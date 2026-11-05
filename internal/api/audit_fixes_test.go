package api

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression tests for the 2026-11 audit: signed-link reuse, download address
// filtering, per-provider worker slots, credential revocation, sub-account
// limits and lease-based job recovery.

func TestSignedOSSLinksAreReusedWithinHalfTheirLifetime(t *testing.T) {
	a := &App{}
	c := OSSConfig{Bucket: "bucket-name", Region: "cn-shanghai", Endpoint: "https://oss-cn-shanghai.aliyuncs.com", URLTTL: 3600, AccessKey: "ak", SecretKey: "sk"}
	first, expires, err := a.cachedOSSURL(c, "phantomframe/a.png")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	second, again, err := a.cachedOSSURL(c, "phantomframe/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !expires.Equal(again) {
		t.Fatal("polling must not re-sign (and force a re-download of) the same object")
	}
	other, _, _ := a.cachedOSSURL(c, "phantomframe/b.png")
	if other == first {
		t.Fatal("links are cached per object")
	}
	c.SecretKey = "rotated"
	rotated, _, _ := a.cachedOSSURL(c, "phantomframe/a.png")
	if rotated == first {
		t.Fatal("rotating credentials must produce a new signature")
	}
}

func TestOutputDownloadsRejectNonPublicAddresses(t *testing.T) {
	for _, address := range []string{"100.100.100.200", "100.64.0.1", "10.0.0.1", "127.0.0.1", "169.254.169.254", "0.1.2.3", "198.18.0.1", "::1", "fd00::1"} {
		if publicOutputIP(net.ParseIP(address)) {
			t.Fatalf("%s must be blocked", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "47.100.1.1", "2606:4700::1111"} {
		if !publicOutputIP(net.ParseIP(address)) {
			t.Fatalf("%s must be allowed", address)
		}
	}
}

func TestWorkerSlotsCapOneProvider(t *testing.T) {
	slots := &workerSlots{perProvider: 2, running: map[string]int{}}
	if !slots.acquire("slow") || !slots.acquire("slow") {
		t.Fatal("slots below the cap must be granted")
	}
	if slots.acquire("slow") {
		t.Fatal("one provider must not take every worker slot")
	}
	if !slots.acquire("fast") {
		t.Fatal("other providers keep their slots")
	}
	slots.release("slow")
	if !slots.acquire("slow") {
		t.Fatal("released slots are reusable")
	}
	a := &App{Config: Config{WorkerConcurrency: 8}}
	if concurrency, perProvider, _ := a.workerSettings(); concurrency != 8 || perProvider != 4 {
		t.Fatalf("default provider cap = %d of %d", perProvider, concurrency)
	}
}

func TestCredentialChangesRevokeAPIKeysAndLimitSubAccounts(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "key-owner", "password": "Key-owner-pass-2026"}, 200))
	key := object(user.ok(t, "POST", "/keys", Row{"name": "integration"}, 201))
	keyClient := &testClient{server: server.URL, key: str(key["token"])}
	keyClient.ok(t, "GET", "/me", nil, 200)
	keyClient.ok(t, "GET", "/me", nil, 200)
	user.ok(t, "POST", "/me/password", Row{"currentPassword": "Key-owner-pass-2026", "password": "Key-owner-pass-2027"}, 200)
	keyClient.ok(t, "GET", "/me", nil, 401)
	fresh := object(user.ok(t, "POST", "/keys", Row{"name": "after rotation"}, 201))
	(&testClient{server: server.URL, key: str(fresh["token"])}).ok(t, "GET", "/me", nil, 200)

	// Sub-accounts stop working while their parent is disabled.
	child := &testClient{server: server.URL}
	user.ok(t, "POST", "/sub-users", Row{"username": "key-owner-child", "password": "Child-pass-2026"}, 201)
	child.ok(t, "POST", "/auth/login", Row{"username": "key-owner-child", "password": "Child-pass-2026"}, 200)
	child.ok(t, "GET", "/me", nil, 200)
	admin.ok(t, "PATCH", "/admin/users/"+str(owner["id"]), Row{"status": "disabled"}, 200)
	child.ok(t, "GET", "/me", nil, 401)
	admin.ok(t, "PATCH", "/admin/users/"+str(owner["id"]), Row{"status": "active"}, 200)
	child.ok(t, "GET", "/me", nil, 200)

	// The per-parent cap holds even without hitting the rate limit.
	parent := &testClient{server: server.URL}
	parentRow := object(parent.ok(t, "POST", "/auth/register", Row{"username": "many-children", "password": "Parent-pass-2026"}, 200))
	for i := 0; i < maxSubAccounts; i++ {
		a.Store.Exec("INSERT INTO users(id,username,display_name,password,parent_id) VALUES(?,?,?,?,?)", id(), "child-"+randomHex(6), "child", "x", parentRow["id"])
	}
	parent.ok(t, "POST", "/sub-users", Row{"username": "one-too-many", "password": "Child-pass-2026"}, 400)
}

func TestLeaseRecoveryOnlyReclaimsOrphanedJobs(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	user := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "lease-owner", "password": "Lease-owner-pass-2026"}, 200)
	alive := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "still running elsewhere"}, 202))
	orphan := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "worker died"}, 202))
	a.Store.Exec("UPDATE jobs SET status='running',worker_id='other-instance',lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE id=?", alive["id"])
	a.Store.Exec("UPDATE jobs SET status='running',worker_id='dead-instance',lease_until=DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE id=?", orphan["id"])
	a.reclaimExpiredJobs(context.Background())
	want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", alive["id"])["status"], "running")
	reclaimed := a.Store.One("SELECT status,refunded FROM jobs WHERE id=?", orphan["id"])
	want(t, reclaimed["status"], "failed")
	want(t, truth(reclaimed["refunded"]), true)

	// A claimed job that never reached a provider goes back to the queue.
	a.Store.Exec("UPDATE jobs SET worker_id='this-instance' WHERE id=?", alive["id"])
	if !a.requeueJob(str(alive["id"]), "this-instance") {
		t.Fatal("requeue failed")
	}
	want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", alive["id"])["status"], "queued")
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='refund'", alive["id"])["n"], 0)
}

func TestJobListBatchesOutputs(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "batch-owner", "password": "Batch-owner-pass-2026"}, 200)
	otherRow := object(other.ok(t, "POST", "/auth/register", Row{"username": "batch-other", "password": "Batch-other-pass-2026"}, 200))
	ids := []string{}
	for i := 0; i < 3; i++ {
		ids = append(ids, str(object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "batch"}, 202))["id"]))
	}
	mine, _ := a.saveAsset(str(a.Store.One("SELECT user_id FROM jobs WHERE id=?", ids[0])["user_id"]), ids[0], "", "image/png", "png", []byte("\x89PNG\r\n\x1a\nmine"))
	foreign, _ := a.saveAsset(str(otherRow["id"]), "", "", "image/png", "png", []byte("\x89PNG\r\n\x1a\nforeign"))
	a.Store.Exec("UPDATE jobs SET status='succeeded',outputs=? WHERE id=?", jsonText([]Row{{"assetId": mine["assetId"], "type": "image"}, {"assetId": foreign["assetId"], "type": "image"}}), ids[0])
	list := object(user.ok(t, "GET", "/jobs?limit=10", nil, 200))
	var job Row
	for _, item := range array(list["items"]) {
		if object(item)["id"] == ids[0] {
			job = object(item)
		}
	}
	outputs := array(job["outputs"])
	want(t, len(outputs), 2)
	want(t, object(outputs[0])["url"], "/api/assets/"+str(mine["assetId"])+"/file")
	// Another user's asset is never resolved through someone else's job.
	if object(outputs[1])["storage"] != nil {
		t.Fatal("foreign asset must not be linked")
	}
}
