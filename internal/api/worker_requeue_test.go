package api

import (
	"context"
	"testing"
	"time"
)

// Consumers of one process used to share a worker id, so a stale requeue from
// one consumer could put a job another consumer was executing back in the
// queue and get it submitted upstream twice. Claims now carry their own token:
// a requeue with any other id must not match, and the job finishes once.
func TestRequeueCannotTouchAnotherClaim(t *testing.T) {
	a := setupIntegration(t)
	jid, _ := slotQueuedJob(t, a)

	ctxB, cancelB := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancelB()
	slotsB := &workerSlots{perProvider: 4, running: map[string]int{}, executing: map[string]string{}}
	doneB := make(chan struct{})
	go func() { defer close(doneB); a.consume(ctxB, ctxB, "proc-1", slotsB) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && str(a.Store.One("SELECT status FROM jobs WHERE id=?", jid)["status"]) != "running" {
		time.Sleep(20 * time.Millisecond)
	}
	claim := str(a.Store.One("SELECT worker_id FROM jobs WHERE id=?", jid)["worker_id"])
	if claim == "" || claim == "proc-1" {
		t.Fatalf("claim token not unique per claim: %q", claim)
	}
	// A sibling consumer's stale requeue (process id, or any other token).
	if a.requeueJob(jid, "proc-1") || a.requeueJob(jid, id()) {
		t.Fatal("requeue matched a claim it does not own")
	}
	<-doneB
	final := a.Store.One("SELECT status,refunded FROM jobs WHERE id=?", jid)
	want(t, final["status"], "succeeded")
	want(t, truth(final["refunded"]), false)
}
