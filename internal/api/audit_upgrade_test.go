package api

import (
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAuditUpgradePreservesExistingJobs(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	body := Row{"modelId": "nanobanana", "prompt": "legacy submission"}
	header := map[string]string{"Idempotency-Key": "legacy-replay"}
	code, value := c.request(t, "POST", "/jobs", body, header)
	want(t, code, 202)
	jid := str(object(value)["id"])
	a.Store.Exec("ALTER TABLE jobs DROP COLUMN hidden_at,DROP COLUMN request_hash,DROP COLUMN refund_attempts,DROP COLUMN refund_next_at")
	for i := 0; i < 2; i++ {
		if err := a.Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	code, value = c.request(t, "POST", "/jobs", body, header)
	want(t, code, 202)
	want(t, object(value)["id"], jid)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='generation'", jid)["n"], 1)
	body["prompt"] = "changed legacy request"
	code, _ = c.request(t, "POST", "/jobs", body, header)
	want(t, code, 409)
	want(t, a.Store.One("SELECT count(*) n FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='jobs' AND COLUMN_NAME IN ('hidden_at','request_hash','refund_attempts','refund_next_at')")["n"], 4)
}

func TestAuditOpposingAdjustmentsAndStaleAuthority(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	first := &testClient{server: server.URL}
	u := object(first.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	second := &testClient{server: server.URL}
	v := object(second.ok(t, "POST", "/auth/register", Row{"username": "audit-money-admin", "password": "Test-money-admin"}, 200))
	first.ok(t, "PATCH", "/admin/users/"+str(v["id"]), Row{"role": "admin"}, 200)
	second.ok(t, "POST", "/auth/login", Row{"username": "audit-money-admin", "password": "Test-money-admin"}, 200)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		for index, c := range []*testClient{first, second} {
			target := v
			if index == 1 {
				target = u
			}
			wg.Add(1)
			go func(c *testClient, target Row) {
				defer wg.Done()
				c.ok(t, "POST", "/admin/users/"+str(target["id"])+"/credits", Row{"amount": 0.01, "reason": "concurrent adjustment"}, 200)
			}(c, target)
		}
	}
	wg.Wait()
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE kind='adjustment'")["n"], 16)
	stale := a.Store.One("SELECT * FROM users WHERE id=?", v["id"])
	first.ok(t, "PATCH", "/admin/users/"+str(v["id"]), Row{"role": "user"}, 200)
	req := httptest.NewRequest("POST", "/api/admin/users/"+str(u["id"])+"/credits", nil)
	req.SetPathValue("id", str(u["id"]))
	req.Header.Set("Idempotency-Key", "stale-authority")
	invoked := false
	err := try(func() {
		a.moneyOperation(&Request{A: a, R: req, User: stale}, Row{"amount": 1}, func(s Store) Row { invoked = true; return Row{"ok": true} })
	})
	if err == nil || invoked {
		t.Fatal("stale administrator must not mutate money after losing permission")
	}
}
