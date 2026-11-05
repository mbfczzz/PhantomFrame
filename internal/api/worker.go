package api

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Execution and maintenance are separate:
//   - Every instance runs job consumers. MySQL is the claim authority
//     (FOR UPDATE SKIP LOCKED), and each running job carries a lease that its
//     worker renews. Losing Redis leadership never interrupts a job.
//   - One Redis-elected leader runs maintenance: reclaiming jobs whose lease
//     expired (their worker died), refund retries and output cleanup.
//
// Never resubmit an uncertain running request: a reclaimed job is refunded,
// unless the worker proved no provider request was ever started.
const (
	jobLeaseDuration = 90 * time.Second
	jobLeaseRenewal  = 15 * time.Second
	// leaseReclaimDelay is extra time past expiry before the leader reclaims a
	// job, so workers recovering from a database outage renew first.
	leaseReclaimDelay = 30 * time.Second
	leaderTTL         = 30 * time.Second
	leaderRenewal     = 8 * time.Second
)

type workerSlots struct {
	mu          sync.Mutex
	perProvider int
	running     map[string]int
	// executing maps each job this process is actually running to its claim
	// token; only these leases are renewed.
	executing map[string]string
}

func (w *workerSlots) saturated() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []string{}
	for provider, n := range w.running {
		if n >= w.perProvider {
			out = append(out, provider)
		}
	}
	return out
}
func (w *workerSlots) start(job, claim string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.executing == nil {
		w.executing = map[string]string{}
	}
	w.executing[job] = claim
}
func (w *workerSlots) finish(job string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.executing, job)
}

// inFlight returns (job id, claim token) pairs of the jobs being executed.
func (w *workerSlots) inFlight() []any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []any{}
	for job, claim := range w.executing {
		out = append(out, job, claim)
	}
	return out
}

func (w *workerSlots) acquire(provider string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running[provider] >= w.perProvider {
		return false
	}
	w.running[provider]++
	return true
}
func (w *workerSlots) release(provider string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running[provider]--; w.running[provider] <= 0 {
		delete(w.running, provider)
	}
}

func (a *App) workerSettings() (int, int, time.Duration) {
	concurrency := a.Config.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 8
	}
	perProvider := a.Config.WorkerProviderConcurrency
	if perProvider <= 0 {
		// One slow provider may use at most half of the slots, so other
		// providers' image and text jobs keep moving.
		perProvider = max(1, (concurrency+1)/2)
	}
	return concurrency, min(perProvider, concurrency), a.Config.WorkerShutdownGrace
}

// sweepTempDownloads removes provider downloads left behind by a crash or a
// forced kill. Anything older than an hour cannot belong to a live job.
func (a *App) sweepTempDownloads() {
	entries, err := os.ReadDir(a.Config.FilesDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".download-") {
			continue
		}
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(filepath.Join(a.Config.FilesDir, entry.Name()))
		}
	}
}

func (a *App) StartWorker(ctx context.Context) <-chan struct{} {
	a.sweepTempDownloads()
	done := make(chan struct{})
	worker := id()
	concurrency, perProvider, grace := a.workerSettings()
	slots := &workerSlots{perProvider: perProvider, running: map[string]int{}, executing: map[string]string{}}
	// Jobs outlive the shutdown signal for the grace period, then are cancelled.
	jobs, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer close(done)
		defer cancelJobs()
		var consumers sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			consumers.Add(1)
			go func() { defer consumers.Done(); a.consume(ctx, jobs, worker, slots) }()
		}
		leaseDone := make(chan struct{})
		go func() {
			defer close(leaseDone)
			for wait(jobs, jobLeaseRenewal) == nil {
				a.renewJobLeases(slots.inFlight())
			}
		}()
		maintenanceDone := make(chan struct{})
		go func() { defer close(maintenanceDone); a.leaderMaintenance(ctx) }()
		consumersDone := make(chan struct{})
		go func() { consumers.Wait(); close(consumersDone) }()
		<-ctx.Done()
		if grace > 0 {
			timer := time.NewTimer(grace)
			select {
			case <-consumersDone:
			case <-timer.C:
			}
			timer.Stop()
		}
		cancelJobs()
		<-consumersDone
		<-leaseDone
		<-maintenanceDone
	}()
	return done
}

// renewJobLeases extends only the claims this process is executing, matched by
// job id and claim token. A job left "running" without being executed (for
// example after an ambiguous claim commit) lets its lease expire so the leader
// recovers it.
func (a *App) renewJobLeases(pairs []any) {
	if len(pairs) == 0 {
		return
	}
	if err := try(func() {
		marks := strings.TrimSuffix(strings.Repeat("(?,?),", len(pairs)/2), ",")
		args := append([]any{int(jobLeaseDuration / time.Second)}, pairs...)
		a.Store.Exec("UPDATE jobs SET lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE status='running' AND (id,worker_id) IN ("+marks+")", args...)
	}); err != nil {
		log.Printf("job lease renewal deferred type=%T", err)
	}
}

func (a *App) leaderMaintenance(ctx context.Context) {
	key := a.Config.RedisPrefix + "worker:leader"
	for ctx.Err() == nil {
		owner := id()
		ok, e := a.Redis.SetNX(ctx, key, owner, leaderTTL).Result()
		if e != nil || !ok {
			if wait(ctx, 2*time.Second) != nil {
				return
			}
			continue
		}
		leader, cancel := context.WithCancel(ctx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			renewed := time.Now()
			for wait(leader, leaderRenewal) == nil {
				renewCtx, stop := context.WithTimeout(leader, 3*time.Second)
				n, e := a.Redis.Eval(renewCtx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('PEXPIRE',KEYS[1],ARGV[2]) else return 0 end`, []string{key}, owner, leaderTTL.Milliseconds()).Int()
				stop()
				if e == nil && n == 1 {
					renewed = time.Now()
					continue
				}
				// Step down only when the lease is provably gone or could have
				// expired; a single slow Redis call is retried on the next tick.
				if (e == nil && n == 0) || time.Since(renewed) >= leaderTTL-leaderRenewal {
					cancel()
					return
				}
			}
		}()
		var wg sync.WaitGroup
		for _, task := range []func(){
			func() { a.reclaimExpiredJobs(leader) },
			a.retryRefunds,
			func() { a.retryCleanups(leader) },
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for leader.Err() == nil {
					task()
					if wait(leader, 2*time.Second) != nil {
						return
					}
				}
			}()
		}
		wg.Wait()
		cancel()
		<-heartbeatDone
		release, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_ = a.Redis.Eval(release, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`, []string{key}, owner).Err()
		stop()
		if wait(ctx, time.Second) != nil {
			return
		}
	}
}

// reclaimExpiredJobs refunds running jobs whose worker stopped renewing the
// lease (crash, kill, lost database). Jobs from releases without leases have
// no lease_until and are reclaimed the same way.
func (a *App) reclaimExpiredJobs(ctx context.Context) {
	if err := try(func() {
		for _, j := range a.Store.All("SELECT id FROM jobs WHERE status='running' AND (lease_until IS NULL OR lease_until<DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND)) ORDER BY seq LIMIT 100", int(leaseReclaimDelay/time.Second)) {
			if ctx.Err() != nil {
				return
			}
			jid := str(j["id"])
			var refunded bool
			if err := try(func() {
				// Re-checked under the row lock: a renewal after the scan wins.
				refunded = a.refund(jid, "服务重启或调度连接中断，费用已退回，请联系管理员核对上游任务", leaseExpired)
			}); err != nil {
				log.Printf("expired job recovery deferred id=%s type=%T", jid, err)
				continue
			}
			if refunded {
				a.cleanupFailedJob(jid)
			}
		}
	}); err != nil {
		log.Printf("expired job scan deferred type=%T", err)
	}
}

func (a *App) consume(claims, jobs context.Context, worker string, slots *workerSlots) {
	for claims.Err() == nil {
		var j Row
		var provider string
		// Every claim gets its own token in worker_id, so requeueing or
		// refunding by token can never touch another consumer's claim.
		claim := id()
		err := try(func() {
			a.Tx(func(s Store) {
				// Skip providers already at their cap in SQL, so a long backlog
				// for one provider cannot hide other providers' jobs.
				query, args := "SELECT * FROM jobs WHERE status='queued'", []any{}
				if full := slots.saturated(); len(full) > 0 {
					query += " AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(provider_snapshot,'$.id')),JSON_UNQUOTE(JSON_EXTRACT(provider_snapshot,'$.protocol')),'') NOT IN (" + sqlMarks(len(full)) + ")"
					for _, key := range full {
						args = append(args, key)
					}
				}
				for _, candidate := range s.All(query+" ORDER BY seq LIMIT 100 FOR UPDATE SKIP LOCKED", args...) {
					if claims.Err() != nil {
						return
					}
					key := str(first(object(candidate["provider_snapshot"])["id"], object(candidate["provider_snapshot"])["protocol"]))
					if !slots.acquire(key) {
						continue
					}
					claimed := false
					func() {
						// Give the slot back if the UPDATE fails or matches nothing.
						defer func() {
							if !claimed {
								slots.release(key)
							}
						}()
						claimed = s.Exec("UPDATE jobs SET status='running',worker_id=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE id=? AND status='queued'", claim, int(jobLeaseDuration/time.Second), candidate["id"]) > 0
					}()
					if !claimed {
						continue
					}
					j, provider = candidate, key
					j["status"], j["worker_id"] = "running", claim
					return
				}
			})
		})
		if err != nil {
			if provider != "" {
				// The UPDATE succeeded but the commit reported an error, so the
				// claim may or may not be durable. Nothing was sent upstream:
				// undo it if it committed. Matching on this claim's own token
				// makes this a no-op if it rolled back, even if another
				// consumer has claimed the job since.
				slots.release(provider)
				a.requeueJob(str(j["id"]), claim)
				j = nil
			}
			log.Printf("job claim interrupted type=%T", err)
			if wait(claims, time.Second) != nil {
				return
			}
			continue
		}
		if j == nil {
			_, _ = a.Redis.BLPop(claims, time.Second, a.Config.RedisPrefix+"jobs:wake").Result()
			continue
		}
		if claims.Err() != nil {
			// Shutdown began right after the claim: nothing was sent upstream.
			a.requeueJob(str(j["id"]), claim)
			slots.release(provider)
			return
		}
		slots.start(str(j["id"]), claim)
		a.processJob(jobs, j)
		slots.finish(str(j["id"]))
		slots.release(provider)
	}
}

// requeueJob returns a claimed job to the queue. Only valid when no provider
// request was started for it; claim is the token written when it was claimed.
func (a *App) requeueJob(jid, claim string) bool {
	var changed int64
	err := try(func() {
		changed = a.Store.Exec("UPDATE jobs SET status='queued',worker_id=NULL,lease_until=NULL WHERE id=? AND status='running' AND worker_id=?", jid, claim)
		if changed > 0 {
			a.updateUpstreamCall("UPDATE upstream_calls SET status='queued',phase='queued',updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='running'", jid)
		}
	})
	return err == nil && changed > 0
}

func (a *App) processJob(parent context.Context, j Row) {
	a.beginUpstreamCall(j)
	timeout := 12 * time.Minute
	if object(j["provider_snapshot"])["protocol"] == "inventory" || object(j["provider_snapshot"])["protocol"] == "opencrow" {
		timeout = 20 * time.Minute
	}
	if object(j["provider_snapshot"])["protocol"] == "rolldek" || object(j["provider_snapshot"])["protocol"] == "maitoken" {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ctx = upstreamContext(ctx, a, str(j["id"]))
	worker := str(j["worker_id"])
	err := try(func() {
		var out []Row
		var e error
		if object(j["provider_snapshot"])["protocol"] == "demo" {
			out, e = a.demo(ctx, j)
		} else if object(j["provider_snapshot"])["protocol"] == "inventory" || object(j["provider_snapshot"])["protocol"] == "opencrow" {
			out, e = a.inventoryUpstream(ctx, j)
		} else {
			out, e = a.upstream(ctx, j)
		}
		must(e)
		if ctx.Err() != nil {
			panic(ctx.Err())
		}
		// The provider has delivered a paid result. Storing it gets its own
		// budget and retries, so a job that finishes near its deadline, or a
		// transient OSS error, does not throw the result away.
		upstreamPhase(ctx, "storing")
		storeCtx, stopStore := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer stopStore()
		for attempt := 0; ; attempt++ {
			var stored []Row
			if stored, e = a.storeGeneratedOutputs(storeCtx, j, out); e == nil {
				out = stored
				break
			}
			if attempt >= 2 || wait(storeCtx, time.Duration(attempt+1)*2*time.Second) != nil {
				break
			}
		}
		must(e)
		var changed int64
		for attempt := 0; ; attempt++ {
			e = try(func() {
				changed = a.Store.Exec("UPDATE jobs SET status='succeeded',outputs=?,finished_at=CURRENT_TIMESTAMP(3),lease_until=NULL WHERE id=? AND status='running' AND (worker_id=? OR worker_id IS NULL)", jsonText(out), j["id"], worker)
			})
			if e == nil || attempt >= 4 || wait(storeCtx, time.Duration(attempt+1)*time.Second) != nil {
				break
			}
		}
		must(e)
		if changed == 0 {
			panic(apiError{409, "任务已经结束"})
		}
	})
	if err == nil {
		a.syncUpstreamCall(str(j["id"]))
		return
	}
	// Cancelled by shutdown before any provider request started: put it back in
	// the queue instead of failing a job that never ran.
	if parent.Err() != nil && upstreamAttempts(ctx) == 0 && object(j["provider_snapshot"])["protocol"] != "demo" && a.requeueJob(str(j["id"]), worker) {
		return
	}
	message := "生成失败，费用已退还，请联系管理员检查通道配置"
	if _, ok := err.(apiError); ok {
		message = errorMessage(err)
	} else if strings.HasPrefix(err.Error(), "供应商") || strings.HasPrefix(err.Error(), "此视频协议") || strings.HasPrefix(err.Error(), "图片协议") || strings.HasPrefix(err.Error(), "OSS ") {
		message = err.Error()
	}
	if ctx.Err() != nil {
		message = "生成请求超时或服务停止，费用已退还，请核对上游任务"
	}
	a.noteUpstreamFailure(j, message)
	// If the database itself is temporarily offline, keep trying to durably record
	// the obligation. If this worker stops, its lease expires and the leader
	// recovers the job.
	for {
		var refunded bool
		// Only the claim that still owns the job may fail it; if it was
		// re-claimed (lease lost), the new owner decides the outcome.
		e := try(func() { refunded = a.refund(str(j["id"]), message, ownedBy(worker)) })
		if e == nil {
			if refunded {
				a.cleanupFailedJob(str(j["id"]))
			} else {
				log.Printf("job no longer owned by this claim, outcome left to its owner id=%s", str(j["id"]))
			}
			break
		}
		var persisted bool
		_ = try(func() {
			current := a.Store.One("SELECT status FROM jobs WHERE id=?", j["id"])
			persisted = current != nil && current["status"] != "running"
		})
		log.Printf("job refund deferred id=%s type=%T", str(j["id"]), e)
		if persisted || wait(parent, 2*time.Second) != nil {
			return
		}
	}
}

// Called after immediate refunds, retry completion and restart recovery alike.
func (a *App) cleanupFailedJob(jid string) {
	if e := try(func() {
		current := a.Store.One("SELECT status FROM jobs WHERE id=?", jid)
		if current == nil || current["status"] != "failed" {
			return
		}
		for _, f := range a.Store.All("SELECT * FROM assets WHERE job_id=?", jid) {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			cleanupErr := a.removeOSSAsset(cleanup, f)
			stop()
			if cleanupErr != nil {
				log.Printf("OSS cleanup deferred asset=%s", str(f["id"]))
				continue
			}
			if err := os.Remove(a.filePath(str(f["file"]))); err != nil && !os.IsNotExist(err) {
				log.Printf("local output cleanup deferred asset=%s", str(f["id"]))
				continue
			}
			a.Store.Exec("DELETE FROM assets WHERE id=?", f["id"])
		}
		if integer(a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", jid)["n"]) == 0 {
			a.Store.Exec("DELETE FROM job_cleanups WHERE job_id=?", jid)
		} else {
			a.Store.Exec("UPDATE job_cleanups SET attempts=attempts+1,next_attempt_at=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 30 SECOND) WHERE job_id=?", jid)
		}
	}); e != nil {
		log.Printf("job output cleanup deferred id=%s type=%T", jid, e)
	}
}

func (a *App) retryCleanups(ctx context.Context) {
	if err := try(func() {
		for _, row := range a.Store.All("SELECT c.job_id FROM job_cleanups c JOIN jobs j ON j.id=c.job_id WHERE j.status='failed' AND c.next_attempt_at<=CURRENT_TIMESTAMP(3) ORDER BY c.next_attempt_at LIMIT 25") {
			if ctx.Err() != nil {
				return
			}
			a.cleanupFailedJob(str(row["job_id"]))
		}
	}); err != nil {
		log.Printf("output cleanup scan deferred type=%T", err)
	}
}

// Runs independently of generation workers, so long upstream requests cannot
// starve compensation. The locked job and wallet transaction make retries unique.
func (a *App) retryRefunds() {
	if err := try(func() {
		for _, j := range a.Store.All("SELECT id,error,refund_attempts FROM jobs WHERE status='refund_pending' AND (refund_next_at IS NULL OR refund_next_at<=CURRENT_TIMESTAMP(3)) ORDER BY seq LIMIT 100") {
			jid := str(j["id"])
			if err := try(func() { a.refund(jid, str(j["error"])) }); err != nil {
				delay := min(int64(60), 2<<min(integer(j["refund_attempts"]), 5))
				_ = try(func() {
					a.Store.Exec("UPDATE jobs SET refund_attempts=refund_attempts+1,refund_next_at=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE id=? AND status='refund_pending'", delay, jid)
				})
				log.Printf("refund retry deferred id=%s type=%T", jid, err)
			}
		}
	}); err != nil {
		log.Printf("refund scan deferred type=%T", err)
	}
}
