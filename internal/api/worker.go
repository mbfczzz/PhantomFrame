package api

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Redis elects one dispatcher; MySQL is the durable job ledger and claim authority.
// Never resubmit an uncertain running request after losing the dispatcher lease.
func (a *App) StartWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			owner := id()
			key := a.Config.RedisPrefix + "worker:leader"
			ok, e := a.Redis.SetNX(ctx, key, owner, 30*time.Second).Result()
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
				for wait(leader, 8*time.Second) == nil {
					renewCtx, stop := context.WithTimeout(leader, 3*time.Second)
					n, e := a.Redis.Eval(renewCtx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('PEXPIRE',KEYS[1],ARGV[2]) else return 0 end`, []string{key}, owner, 30000).Int()
					stop()
					if e != nil || n != 1 {
						cancel()
						return
					}
				}
			}()
			err := try(func() {
				for _, j := range a.Store.All("SELECT id FROM jobs WHERE status='running'") {
					a.refund(str(j["id"]), "服务重启或调度连接中断，费用已退回，请联系管理员核对上游任务")
				}
				var wg sync.WaitGroup
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); a.consume(leader) }()
				}
				wg.Wait()
			})
			if err != nil {
				log.Printf("worker coordinator interrupted type=%T", err)
			}
			cancel()
			<-heartbeatDone
			release, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_ = a.Redis.Eval(release, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`, []string{key}, owner).Err()
			stop()
			if wait(ctx, time.Second) != nil {
				return
			}
		}
	}()
	return done
}
func (a *App) consume(ctx context.Context) {
	for ctx.Err() == nil {
		var j Row
		err := try(func() {
			a.Tx(func(s Store) {
				j = s.One("SELECT * FROM jobs WHERE status='queued' ORDER BY seq LIMIT 1 FOR UPDATE SKIP LOCKED")
				if j != nil {
					if ctx.Err() != nil {
						return
					}
					s.Exec("UPDATE jobs SET status='running' WHERE id=? AND status='queued'", j["id"])
					j["status"] = "running"
				}
			})
		})
		if err != nil {
			log.Printf("job claim interrupted type=%T", err)
			if wait(ctx, time.Second) != nil {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if j == nil {
			_, _ = a.Redis.BLPop(ctx, time.Second, a.Config.RedisPrefix+"jobs:wake").Result()
			continue
		}
		a.processJob(ctx, j)
	}
}
func (a *App) processJob(parent context.Context, j Row) {
	timeout := 12 * time.Minute
	if object(j["provider_snapshot"])["protocol"] == "rolldek" || object(j["provider_snapshot"])["protocol"] == "maitoken" {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := try(func() {
		var out []Row
		var e error
		if object(j["provider_snapshot"])["protocol"] == "demo" {
			out, e = a.demo(ctx, j)
		} else {
			out, e = a.upstream(ctx, j)
		}
		must(e)
		if ctx.Err() != nil {
			panic(ctx.Err())
		}
		out, e = a.storeGeneratedOutputs(ctx, j, out)
		must(e)
		if ctx.Err() != nil {
			panic(ctx.Err())
		}
		if a.Store.Exec("UPDATE jobs SET status='succeeded',outputs=?,finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'", jsonText(out), j["id"]) == 0 {
			panic(apiError{409, "任务已经结束"})
		}
	})
	if err == nil {
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
	if e := try(func() {
		a.refund(str(j["id"]), message)
		current := a.Store.One("SELECT status FROM jobs WHERE id=?", j["id"])
		if current != nil && current["status"] == "succeeded" {
			return
		}
		for _, f := range a.Store.All("SELECT * FROM assets WHERE job_id=?", j["id"]) {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			cleanupErr := a.removeOSSAsset(cleanup, f)
			stop()
			if cleanupErr != nil {
				log.Printf("OSS cleanup deferred asset=%s", str(f["id"]))
				continue
			}
			_ = os.Remove(a.filePath(str(f["file"])))
			a.Store.Exec("DELETE FROM assets WHERE id=?", f["id"])
		}
	}); e != nil {
		log.Printf("job recovery deferred id=%s type=%T", str(j["id"]), e)
	}
}
