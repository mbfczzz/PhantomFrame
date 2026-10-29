package api

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestInventoryProviderDeleteAndExplicitRestore(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	pid := inventoryProviderID("zzshu")
	path := "/admin/providers/" + pid
	routes := a.Store.All("SELECT * FROM inventory_routes WHERE provider_id=?", pid)
	want(t, len(routes), 3)
	mid := inventoryModelID(str(routes[0]["id"]))
	a.Store.Exec("UPDATE inventory_routes SET api_key=? WHERE provider_id=?", a.encrypt("deleted-provider-fixture-key"), pid)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,'delete fixture','request',25,'{}')", id(), mid)
	a.Store.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,description,parameters,api_key) VALUES(?,?,'catalog-fixture','catalog fixture','','[]',?)", id(), pid, a.encrypt("deleted-catalog-fixture-key"))
	otherRoutes := jsonText(a.Store.All("SELECT * FROM inventory_routes WHERE provider_id<>? ORDER BY id", pid))
	balances := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "DELETE", path, nil, 401)
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	c.ok(t, "DELETE", path, nil, 200)
	for _, query := range []string{
		"SELECT count(*) n FROM providers WHERE id=?",
		"SELECT count(*) n FROM inventory_routes WHERE provider_id=?",
		"SELECT count(*) n FROM models WHERE provider_id=?",
		"SELECT count(*) n FROM provider_models WHERE provider_id=?",
	} {
		want(t, a.Store.One(query, pid)["n"], 0)
	}
	want(t, a.Store.One("SELECT count(*) n FROM prices WHERE model_id=?", mid)["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_deleted_providers WHERE provider_id=?", pid)["n"], 1)
	want(t, jsonText(a.Store.All("SELECT * FROM inventory_routes WHERE provider_id<>? ORDER BY id", pid)), otherRoutes)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balances)
	if strings.Contains(jsonText(c.ok(t, "GET", "/admin/audit", nil, 200)), "deleted-provider-fixture-key") {
		t.Fatal("deletion audit leaked a credential")
	}
	c.ok(t, "DELETE", path, nil, 200)
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Config.ProviderPresets = true
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", pid)["n"], 0)
	input := inventoryFixtureCSV(t)
	for _, csv := range []string{input, strings.Replace(input, "test-only-key-", "changed-fixture-key-", 1)} {
		report, err := a.importProviderInventory(strings.NewReader(csv), true, true)
		if err != nil {
			t.Fatal(err)
		}
		want(t, report["skippedDeletedRoutes"], 3)
		want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", pid)["n"], 0)
	}
	if _, err := a.ImportProviderInventory(strings.NewReader(input), false); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", pid)["n"], 0)
	if _, err := a.ImportProviderInventory(strings.NewReader(input), true); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", pid)["n"], 1)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_deleted_providers WHERE provider_id=?", pid)["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id=? AND enabled=1", pid)["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM prices WHERE model_id=?", mid)["n"], 0)
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes WHERE provider_id=?", pid)["n"], 3)
}

func TestInventoryProviderDeleteProtectsJobs(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	pid := inventoryProviderID("zzshu")
	mid := a.Store.One("SELECT id FROM models WHERE provider_id=? LIMIT 1", pid)["id"]
	uid := a.Store.One("SELECT id FROM users WHERE role='admin'")["id"]
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,'history fixture','request',25,'{}')", id(), mid)
	before := jsonText(a.Store.All("SELECT * FROM inventory_routes WHERE provider_id=? ORDER BY id", pid))
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	user := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "delete-check-user", "password": "Test-user-password-2026"}, 200)
	user.ok(t, "DELETE", "/admin/providers/"+pid, nil, 403)
	for _, status := range []string{"queued", "running", "succeeded", "failed", "refund_pending"} {
		t.Run(status, func(t *testing.T) {
			jid := id()
			a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,outputs,provider_snapshot,hidden_at) VALUES(?,?,?,'history fixture','{}',?,25,'[]','{}',UTC_TIMESTAMP(3))", jid, uid, mid, status)
			job := jsonText(a.Store.One("SELECT * FROM jobs WHERE id=?", jid))
			c.ok(t, "DELETE", "/admin/providers/"+pid, nil, 409)
			want(t, jsonText(a.Store.All("SELECT * FROM inventory_routes WHERE provider_id=? ORDER BY id", pid)), before)
			want(t, jsonText(a.Store.One("SELECT * FROM jobs WHERE id=?", jid)), job)
			want(t, a.Store.One("SELECT count(*) n FROM prices WHERE model_id=?", mid)["n"], 1)
			want(t, a.Store.One("SELECT count(*) n FROM inventory_deleted_providers WHERE provider_id=?", pid)["n"], 0)
			a.Store.Exec("DELETE FROM jobs WHERE id=?", jid)
		})
	}
}

func TestInventoryProviderDeletionSerializesWithPresets(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	pid := inventoryProviderID("zzshu")
	handler := a.Handler()
	server := httptest.NewServer(handler)
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	var wg sync.WaitGroup
	errors := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- try(a.ensureProviderPresets) }()
	}
	c.ok(t, "DELETE", "/admin/providers/"+pid, nil, 200)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	a.ensureProviderPresets()
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", pid)["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes WHERE provider_id=?", pid)["n"], 0)
}
