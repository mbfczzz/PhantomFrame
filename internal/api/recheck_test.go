package api

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecheckRefundCompletionCopy(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	job := object(c.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "recheck refund"}, 202))
	jid := str(job["id"])
	a.Store.Exec("UPDATE jobs SET status='refund_pending',error='生成失败，退款处理中' WHERE id=?", jid)
	a.retryRefunds()
	row := a.Store.One("SELECT * FROM jobs WHERE id=?", jid)
	want(t, row["status"], "failed")
	want(t, truth(row["refunded"]), true)
	if strings.Contains(str(row["error"]), "退款处理中") {
		t.Fatal("completed refund still claims it is pending")
	}
}

func TestRecheckFailedOutputCleanup(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	job := object(c.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "cleanup failure fixture"}, 202))
	jid := str(job["id"])
	file := "cannot-remove-yet"
	dir := filepath.Join(a.Config.FilesDir, file)
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "blocker"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	assetID := id()
	a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size,job_id) VALUES(?,?,'partial','image','image/png',?,1,?)", assetID, u["id"], file, jid)
	a.refund(jid, "生成失败，费用已退还")
	a.cleanupFailedJob(jid)
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE id=?", assetID)["n"], 1)
	want(t, a.Store.One("SELECT count(*) n FROM job_cleanups WHERE job_id=?", jid)["n"], 1)
	want(t, truth(a.Store.One("SELECT refunded FROM jobs WHERE id=?", jid)["refunded"]), true)
	if e := os.Remove(filepath.Join(dir, "blocker")); e != nil {
		t.Fatal(e)
	}
	a.Store.Exec("UPDATE job_cleanups SET next_attempt_at=CURRENT_TIMESTAMP(3) WHERE job_id=?", jid)
	a.retryCleanups(context.Background())
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE id=?", assetID)["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM job_cleanups WHERE job_id=?", jid)["n"], 0)
}

func TestRecheckEncodedLogSecrets(t *testing.T) {
	secret := "local/key+with spaces"
	text := logText("upstream https://user:password@files.invalid/"+url.PathEscape(secret)+"?signature=private-signature#fragment", 1000, secret)
	for _, hidden := range []string{url.PathEscape(secret), "private-signature", "password"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("log retained secret fragment %q", hidden)
		}
	}
	params := safeLogParams(Row{"headers": Row{"Cookie": "session=private-cookie", "X-Goog-Api-Key": "private-header"}, "private_key": "private-pem", "max_tokens": 123}, secret)
	for _, hidden := range []string{"private-cookie", "private-header", "private-pem"} {
		if strings.Contains(jsonText(params), hidden) {
			t.Fatalf("parameter snapshot retained secret %q", hidden)
		}
	}
	want(t, params["max_tokens"], 123)
}

func TestRecheckStaleGenerationAuthorization(t *testing.T) {
	a := setupIntegration(t)
	u := a.Store.One("SELECT * FROM users WHERE username='test-admin'")
	a.Store.Exec("UPDATE users SET status='disabled',auth_version=auth_version+1 WHERE id=?", u["id"])
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(jsonText(Row{"modelId": "nanobanana", "prompt": "must not debit disabled account"})))
	err := try(func() { a.submit(&Request{A: a, W: httptest.NewRecorder(), R: req, User: u}) })
	if err == nil {
		t.Fatal("generation accepted stale authorization after account disable")
	}
	want(t, a.Store.One("SELECT count(*) n FROM jobs")["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE kind='generation'")["n"], 0)
}

func TestRecheckTaskIDRedactionAcrossLifecycle(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	j := object(c.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "local task id fixture"}, 202))
	jid := str(j["id"])
	secret := "test/credential+with spaces"
	upID := "task/" + url.PathEscape(secret)
	snapshot := object(a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", jid)["provider_snapshot"])
	snapshot["api_key"] = a.encrypt(secret)
	a.Store.Exec("UPDATE jobs SET provider_snapshot=?,status='running' WHERE id=?", jsonText(snapshot), jid)
	a.beginUpstreamCall(a.Store.One("SELECT * FROM jobs WHERE id=?", jid))
	a.setUpstreamTaskID(jid, upID)
	for _, stage := range []string{"task-id", "sync", "backfill"} {
		if stage == "sync" {
			a.syncUpstreamCall(jid)
		}
		if stage == "backfill" {
			a.Store.Exec("DELETE FROM upstream_calls WHERE id=?", jid)
			if err := a.Migrate(); err != nil {
				t.Fatal(err)
			}
		}
		for _, path := range []string{"/jobs/" + jid + "/upstream", "/admin/upstream-calls/" + jid} {
			view := object(c.ok(t, "GET", path, nil, 200))
			if strings.Contains(jsonText(view), secret) || strings.Contains(jsonText(view), url.PathEscape(secret)) {
				t.Fatalf("%s leaked upstream credential", stage)
			}
			if !strings.Contains(str(view["upstreamId"]), "已隐藏") {
				t.Fatalf("%s lost sanitized task ID", stage)
			}
		}
	}
	want(t, a.Store.One("SELECT upstream_id FROM jobs WHERE id=?", jid)["upstream_id"], upID)
}

func TestRecheckAssetDeletionKeepsRetryTarget(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	file := "asset-deletion-blocked"
	dir := filepath.Join(a.Config.FilesDir, file)
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "blocker"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	assetID := id()
	a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size) VALUES(?,?,'delete fixture','image','image/png',?,1)", assetID, u["id"], file)
	c.ok(t, "DELETE", "/assets/"+assetID, nil, 502)
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE id=?", assetID)["n"], 1)
	if e := os.Remove(filepath.Join(dir, "blocker")); e != nil {
		t.Fatal(e)
	}
	c.ok(t, "DELETE", "/assets/"+assetID, nil, 200)
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE id=?", assetID)["n"], 0)
}
