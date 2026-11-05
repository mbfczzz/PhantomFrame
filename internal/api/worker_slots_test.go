package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression tests for per-provider worker slots: a failed claim must give its
// slot back, and a provider at its cap must not hide other providers' jobs.

func slotQueuedJob(t *testing.T, a *App) (string, string) {
	t.Helper()
	server := httptest.NewServer(a.Handler())
	t.Cleanup(server.Close)
	user := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "slot-" + randomHex(4), "password": "Slot-owner-pass-2026"}, 200)
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "slot regression"}, 202))
	jid := str(job["id"])
	snap := object(a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", jid)["provider_snapshot"])
	return jid, str(first(snap["id"], snap["protocol"]))
}

// A DB error on the claim UPDATE must release the provider slot, so the job is
// claimed once the database recovers.
func TestWorkerClaimErrorReleasesProviderSlot(t *testing.T) {
	a := setupIntegration(t)
	jid, key := slotQueuedJob(t, a)
	a.Store.Exec("CREATE TRIGGER slot_fail_claim BEFORE UPDATE ON jobs FOR EACH ROW BEGIN IF OLD.status='queued' AND NEW.status='running' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected claim failure'; END IF; END")
	t.Cleanup(func() { _ = try(func() { a.Store.Exec("DROP TRIGGER IF EXISTS slot_fail_claim") }) })
	slots := &workerSlots{perProvider: 2, running: map[string]int{}}

	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	a.consume(ctx, ctx, "slot-test-worker", slots)
	cancel()
	a.Store.Exec("DROP TRIGGER slot_fail_claim") // database "recovers"

	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	a.consume(ctx, ctx, "slot-test-worker", slots)
	cancel()

	slots.mu.Lock()
	leaked := slots.running[key]
	slots.mu.Unlock()
	status := str(a.Store.One("SELECT status FROM jobs WHERE id=?", jid)["status"])
	t.Logf("provider=%s leaked slots=%d job status after recovery=%s", key, leaked, status)
	if leaked != 0 || status == "queued" {
		t.Fatalf("%d slot(s) leaked for provider %q with nothing running; job %s is stuck %q", leaked, key, jid, status)
	}
}

// With 100+ queued jobs of a provider at its cap ahead of it, another
// provider's job must still be claimed.
func TestWorkerCappedProviderDoesNotStarveOthers(t *testing.T) {
	slotStarvation(t, 100)
}

// The same setup with fewer capped jobs than the claim window.
func TestWorkerCappedProviderWithinClaimWindow(t *testing.T) {
	slotStarvation(t, 50)
}

func slotStarvation(t *testing.T, ahead int) {
	a := setupIntegration(t)
	template, key := slotQueuedJob(t, a)
	for i := 0; i < ahead; i++ {
		a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,cost,source,provider_snapshot,outputs,status) SELECT ?,user_id,model_id,prompt,params,cost,source,provider_snapshot,outputs,'queued' FROM jobs WHERE id=?", fmt.Sprintf("slow-%03d", i), template)
	}
	a.Store.Exec("UPDATE jobs SET status='cancelled' WHERE id=?", template)
	other := "fast-provider-job"
	a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,cost,source,provider_snapshot,outputs,status) SELECT ?,user_id,model_id,prompt,params,cost,source,JSON_SET(provider_snapshot,'$.id','other-provider'),outputs,'queued' FROM jobs WHERE id=?", other, template)
	slots := &workerSlots{perProvider: 1, running: map[string]int{key: 1}} // slow provider already at cap

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	a.consume(ctx, ctx, "slot-test-worker", slots)
	cancel()

	status := str(a.Store.One("SELECT status FROM jobs WHERE id=?", other)["status"])
	t.Logf("other provider job status=%s", status)
	if status == "queued" {
		t.Fatalf("job for an idle provider was not claimed behind %d capped jobs", ahead)
	}
}
