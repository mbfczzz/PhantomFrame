package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrustedProxyBoundaries(t *testing.T) {
	a := &App{Config: Config{TrustedProxies: []string{"127.0.0.1/32", "::1/128", "10.0.0.0/8"}}}
	for _, tc := range []struct{ peer, forwarded, real, want string }{
		{"127.0.0.1:123", "198.51.100.1", "", "198.51.100.1"},
		{"127.0.0.1:123", "forged, 198.51.100.1, 10.1.2.3", "", "198.51.100.1"},
		{"198.51.100.2:123", "198.51.100.3", "198.51.100.4", "198.51.100.2"},
		{"[::1]:123", "2001:db8::1", "", "2001:db8::1"},
		{"127.0.0.1:123", "", "198.51.100.1", "198.51.100.1"},
		{"127.0.0.1:123", "garbage", "198.51.100.1", "127.0.0.1"},
		{"127.0.0.1:123", "::ffff:198.51.100.1", "", "198.51.100.1"},
	} {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		r.Header.Set("X-Real-IP", tc.real)
		want(t, a.clientIP(r), tc.want)
	}
	a.Config.TrustedProxies = nil
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:123"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	want(t, a.clientIP(r), "127.0.0.1")
}

func TestAuditRateLimitIsolation(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	for i := 0; i < 31; i++ {
		code, _ := c.request(t, "POST", "/auth/login", Row{"username": "not-found", "password": "bad"}, map[string]string{"X-Forwarded-For": "198.51.100.1"})
		expected := 401
		if i == 30 {
			expected = 429
		}
		want(t, code, expected)
	}
	code, _ := c.request(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, map[string]string{"X-Forwarded-For": "198.51.100.2"})
	want(t, code, 200)
	// Authenticated buckets use account identity, independent of IP and login abuse.
	c.ok(t, "POST", "/redeem", Row{"code": "invalid"}, 400)
	user := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "audit-rate-user", "password": "Test-password-2026"}, 200)
	for i := 0; i < 31; i++ {
		code, _ := c.request(t, "POST", "/jobs", Row{})
		if i == 30 {
			want(t, code, 429)
		}
	}
	user.ok(t, "POST", "/jobs", Row{}, 400)
}

func TestAuditHiddenJobRetainsFinancialFacts(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	key := map[string]string{"Idempotency-Key": "hidden-request"}
	body := Row{"modelId": "nanobanana", "prompt": "locally completed fixture"}
	code, value := c.request(t, "POST", "/jobs", body, key)
	want(t, code, 202)
	job := object(value)
	jid := str(job["id"])
	a.Store.Exec("UPDATE jobs SET status='succeeded',finished_at=CURRENT_TIMESTAMP(3) WHERE id=?", jid)
	before := object(c.ok(t, "GET", "/usage", nil, 200))
	balance := a.Store.One("SELECT balance FROM users WHERE id=?", u["id"])["balance"]
	c.ok(t, "DELETE", "/jobs/"+jid, nil, 200)
	c.ok(t, "DELETE", "/jobs/"+jid, nil, 200)
	c.ok(t, "GET", "/jobs/"+jid, nil, 404)
	want(t, object(c.ok(t, "GET", "/jobs", nil, 200))["total"], 0)
	want(t, object(c.ok(t, "GET", "/usage", nil, 200))["spent"], before["spent"])
	c.ok(t, "GET", "/admin/jobs/"+jid, nil, 200)
	code, value = c.request(t, "POST", "/jobs", body, key)
	want(t, code, 202)
	want(t, object(value)["id"], jid)
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", u["id"])["balance"], balance)
	body["prompt"] = "different intent"
	code, _ = c.request(t, "POST", "/jobs", body, key)
	want(t, code, 409)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='generation'", jid)["n"], 1)
}

func TestAuditMoneyReplayAndConcurrency(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	child := object(c.ok(t, "POST", "/sub-users", Row{"username": "audit-child", "password": "Test-child-password"}, 201))
	path := "/sub-users/" + str(child["id"]) + "/transfer"
	body := Row{"credits": 5.01}
	key := map[string]string{"Idempotency-Key": "transfer-replay"}
	var wg sync.WaitGroup
	results := make(chan any, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, value := c.request(t, "POST", path, body, key)
			if code != 200 {
				t.Errorf("replay returned %d", code)
			}
			results <- value
		}()
	}
	wg.Wait()
	close(results)
	for value := range results {
		want(t, object(value)["operationId"], "transfer-replay")
	}
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", child["id"])["balance"], 501)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE kind='transfer'")["n"], 2)
	code, _ := c.request(t, "POST", path, Row{"credits": 6}, key)
	want(t, code, 409)
	code, _ = c.request(t, "POST", path, body, map[string]string{"Idempotency-Key": ""})
	want(t, code, 400)
	// Retrying a committed operation still succeeds after the target is unlinked.
	c.ok(t, "DELETE", "/sub-users/"+str(child["id"]), nil, 200)
	code, _ = c.request(t, "POST", path, body, key)
	want(t, code, 200)
	c.ok(t, "GET", "/money-operations/transfer-replay", nil, 200)
	outsider := &testClient{server: server.URL}
	outsider.ok(t, "POST", "/auth/login", Row{"username": "audit-child", "password": "Test-child-password"}, 200)
	outsider.ok(t, "GET", "/money-operations/transfer-replay", nil, 404)
	// The same guarantee applies to manual adjustments, including negative amounts.
	adjustment := "/admin/users/" + str(u["id"]) + "/credits"
	adjustKey := map[string]string{"Idempotency-Key": "adjust-replay"}
	for i := 0; i < 2; i++ {
		code, _ = c.request(t, "POST", adjustment, Row{"amount": -0.11, "reason": "audit adjustment"}, adjustKey)
		want(t, code, 200)
	}
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE kind='adjustment'")["n"], 1)
	code, _ = c.request(t, "POST", adjustment, Row{"amount": 1, "reason": "another request"}, key)
	want(t, code, 409)
}

func TestAuditRefundRecoveryWithoutRestart(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"simulated"}`))
	}))
	defer mock.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	job := object(c.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "local failure"}, 202))
	jid := str(job["id"])
	a.Store.Exec("UPDATE jobs SET status='running',provider_snapshot=? WHERE id=?", jsonText(Row{"protocol": "openai-image", "base_url": mock.URL, "upstreamModel": "mock", "type": "image", "config": Row{}}), jid)
	a.Store.Exec("ALTER TABLE transactions ADD CONSTRAINT audit_refund_failure CHECK (kind <> 'refund')")
	a.processJob(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", jid))
	// A partially stored output must also be cleaned after deferred compensation.
	partialFile := "audit-partial.png"
	if err := os.WriteFile(filepath.Join(a.Config.FilesDir, partialFile), []byte("local fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size,job_id) VALUES(?,?,'partial','image','image/png',?,13,?)", id(), u["id"], partialFile, jid)
	current := a.Store.One("SELECT * FROM jobs WHERE id=?", jid)
	want(t, current["status"], "refund_pending")
	want(t, truth(current["refunded"]), false)
	if strings.Contains(str(current["error"]), "已退还") {
		t.Fatal("pending refund falsely presented as completed")
	}
	c.ok(t, "DELETE", "/jobs/"+jid, nil, 400)
	ctx, cancel := context.WithCancel(context.Background())
	done := a.StartWorker(ctx)
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(8 * time.Second)
	for integer(a.Store.One("SELECT refund_attempts n FROM jobs WHERE id=?", jid)["n"]) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if integer(a.Store.One("SELECT refund_attempts n FROM jobs WHERE id=?", jid)["n"]) == 0 {
		t.Fatal("background refund scanner did not retry")
	}
	a.Store.Exec("ALTER TABLE transactions DROP CHECK audit_refund_failure")
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if a.Store.One("SELECT status FROM jobs WHERE id=?", jid)["status"] == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", jid)["status"], "failed")
	for i := 0; i < 4; i++ {
		a.refund(jid, "replayed refund")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && integer(a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", jid)["n"]) != 0 {
		time.Sleep(25 * time.Millisecond)
	}
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", jid)["n"], 0)
	if _, err := os.Stat(filepath.Join(a.Config.FilesDir, partialFile)); !os.IsNotExist(err) {
		t.Fatal("partial output was not removed")
	}
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='refund'", jid)["n"], 1)
	want(t, credits(a.Store.One("SELECT balance FROM users WHERE id=?", u["id"])["balance"]), u["balance"])
}

func TestAuditAdminLifecycleAndBootstrap(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	first := &testClient{server: server.URL}
	u := object(first.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	second := &testClient{server: server.URL}
	v := object(second.ok(t, "POST", "/auth/register", Row{"username": "audit-admin-two", "password": "Test-second-admin"}, 200))
	first.ok(t, "PATCH", "/admin/users/"+str(v["id"]), Row{"role": "admin"}, 200)
	second.ok(t, "POST", "/auth/login", Row{"username": "audit-admin-two", "password": "Test-second-admin"}, 200)
	var wg sync.WaitGroup
	for i, c := range []*testClient{first, second} {
		target := v
		if i == 1 {
			target = u
		}
		wg.Add(1)
		go func(c *testClient, target Row) {
			defer wg.Done()
			code, _ := c.request(t, "PATCH", "/admin/users/"+str(target["id"]), Row{"role": "user", "status": "disabled"})
			if code != 200 && code != 401 && code != 403 && code != 409 {
				t.Errorf("unexpected code %d", code)
			}
		}(c, target)
	}
	wg.Wait()
	want(t, a.Store.One("SELECT count(*) n FROM users WHERE role='admin' AND status='active'")["n"], 1)
	balance := a.Store.One("SELECT balance FROM users WHERE id=?", u["id"])["balance"]
	a.Store.Exec("UPDATE users SET role='user',status='disabled'")
	// A real restart must recover the existing bootstrap name without duplicate insertion.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.Seed() }()
	}
	wg.Wait()
	recovered := a.Store.One("SELECT * FROM users WHERE username='test-admin'")
	want(t, recovered["role"], "admin")
	want(t, recovered["status"], "active")
	want(t, recovered["balance"], balance)
	want(t, a.Store.One("SELECT count(*) n FROM users WHERE username='test-admin'")["n"], 1)
	first.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
}

func TestAuditTicketReopenCapConcurrent(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/register", Row{"username": "audit-ticket-user", "password": "Test-ticket-password"}, 200))
	for i := 0; i < 49; i++ {
		a.Store.Exec("INSERT INTO support_tickets(id,user_id,subject,category,status) VALUES(?,?,'audit fixture','other','open')", id(), u["id"])
	}
	closed := id()
	a.Store.Exec("INSERT INTO support_tickets(id,user_id,subject,category,status) VALUES(?,?,'closed fixture','other','closed')", closed, u["id"])
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		code, _ := c.request(t, "POST", "/tickets", Row{"subject": "audit creation", "content": "audited local content", "category": "other"})
		if code != 201 && code != 429 {
			t.Errorf("create: %d", code)
		}
	}()
	go func() {
		defer wg.Done()
		code, _ := admin.request(t, "PATCH", "/admin/tickets/"+closed, Row{"status": "open"})
		if code != 200 && code != 429 {
			t.Errorf("reopen: %d", code)
		}
	}()
	wg.Wait()
	want(t, a.Store.One("SELECT count(*) n FROM support_tickets WHERE user_id=? AND status IN ('open','replied')", u["id"])["n"], 50)
	another := id()
	a.Store.Exec("INSERT INTO support_tickets(id,user_id,subject,category,status) VALUES(?,?,'resolved fixture','other','resolved')", another, u["id"])
	c.ok(t, "PATCH", "/tickets/"+another, Row{"status": "open"}, 429)
	admin.ok(t, "PATCH", "/admin/tickets/"+another, Row{"status": "open"}, 429)
	open := a.Store.One("SELECT id FROM support_tickets WHERE user_id=? AND status='open' LIMIT 1", u["id"])
	c.ok(t, "PATCH", "/tickets/"+str(open["id"]), Row{"status": "closed"}, 200)
	c.ok(t, "PATCH", "/tickets/"+another, Row{"status": "open"}, 200)
	c.ok(t, "PATCH", "/tickets/"+another, Row{"status": "open"}, 200)
}
