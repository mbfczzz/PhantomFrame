package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamLogPendingRefund(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	job := object(admin.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "refund diagnostic test"}, 202))
	jid := str(job["id"])
	a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", jid)
	row := a.Store.One("SELECT * FROM jobs WHERE id=?", jid)
	a.beginUpstreamCall(row)
	a.noteUpstreamFailure(row, "生成失败，费用已退还")
	pending := object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))
	want(t, pending["phase"], "refund_pending")
	want(t, pending["refunded"], false)
	if strings.Contains(str(pending["error"]), "已退还") {
		t.Fatal("a pending refund must not be presented as already refunded")
	}
	a.refund(jid, "模拟失败，费用已退还")
	finished := object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))
	want(t, finished["status"], "failed")
	want(t, finished["refunded"], true)
	want(t, finished["phase"], "failed")
	want(t, finished["failurePhase"], "demo")
}
