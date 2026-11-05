package api

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// A claim transaction that fails must not requeue a job that another consumer
// of the same process (same worker_id) claimed right after the rollback.
func TestClaimErrorDoesNotRequeueSiblingClaim(t *testing.T) {
	a := setupIntegration(t)
	jid, _ := slotQueuedJob(t, a)
	const worker = "claim-race-worker"
	// Consumer A's claim UPDATE stalls briefly and then fails (lock wait /
	// connection error stand-in). Sessions with @allow=1 (consumer B) succeed.
	a.Store.Exec("CREATE TRIGGER claim_fail_probe BEFORE UPDATE ON jobs FOR EACH ROW BEGIN IF OLD.status='queued' AND NEW.status='running' AND @allow IS NULL THEN DO SLEEP(1); SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected claim failure'; END IF; END")
	t.Cleanup(func() { _ = try(func() { a.Store.Exec("DROP TRIGGER IF EXISTS claim_fail_probe") }) })

	bDone := make(chan error, 1)
	go func() {
		// Consumer B (same process, same worker id) reaches job X as soon as
		// A's rollback releases it, claims it and starts executing.
		time.Sleep(300 * time.Millisecond)
		ctx := context.Background()
		conn, err := a.DB.Conn(ctx)
		if err != nil {
			bDone <- err
			return
		}
		defer conn.Close()
		if _, err = conn.ExecContext(ctx, "SET @allow=1"); err != nil {
			bDone <- err
			return
		}
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			bDone <- err
			return
		}
		var status string
		if err = tx.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id=? FOR UPDATE", jid).Scan(&status); err != nil {
			tx.Rollback()
			bDone <- err
			return
		}
		if _, err = tx.ExecContext(ctx, "UPDATE jobs SET status='running',worker_id=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 90 SECOND) WHERE id=? AND status='queued'", worker, jid); err != nil {
			tx.Rollback()
			bDone <- err
			return
		}
		time.Sleep(200 * time.Millisecond)
		bDone <- tx.Commit()
	}()

	slots := &workerSlots{perProvider: 4, running: map[string]int{}, executing: map[string]string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
	a.consume(ctx, ctx, worker, slots) // consumer A
	cancel()
	if err := <-bDone; err != nil {
		t.Fatalf("consumer B claim: %v", err)
	}
	status := str(a.Store.One("SELECT status FROM jobs WHERE id=?", jid)["status"])
	t.Logf("job %s status after B claimed it and A's error path ran: %s", jid, status)
	if status != "running" {
		t.Fatalf("consumer A's error-path requeue put job %s back to %q while consumer B is executing it; another consumer will submit it upstream a second time", jid, status)
	}
}

// The leader refunds a job whose lease was renewed between its scan and the
// refund's row lock: refund() never rechecks lease_until.
func TestReclaimRespectsLeaseRenewedAfterScan(t *testing.T) {
	a := setupIntegration(t)
	jid, _ := slotQueuedJob(t, a)
	a.Store.Exec("UPDATE jobs SET status='running',worker_id='alive-worker',lease_until=DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE id=?", jid)

	// The alive worker's renewal is in flight (row locked, not yet committed).
	tx, err := a.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("UPDATE jobs SET lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 90 SECOND) WHERE id=? AND worker_id='alive-worker' AND status='running'", jid); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); a.reclaimExpiredJobs(context.Background()) }()
	time.Sleep(500 * time.Millisecond)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	<-done
	row := a.Store.One("SELECT status,lease_until>CURRENT_TIMESTAMP(3) live FROM jobs WHERE id=?", jid)
	t.Logf("job status=%v leaseLive=%v", row["status"], row["live"])
	if row["status"] != "running" {
		t.Fatalf("job %s with a live (renewed) lease was reclaimed and refunded: status=%v", jid, row["status"])
	}
}
