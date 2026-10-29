package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestProviderInstancesResetOnceAndRetainHistory(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	uid := a.Store.One("SELECT id FROM users WHERE role='admin'")["id"]
	mid := a.Store.One("SELECT id FROM models WHERE provider_id='demo' LIMIT 1")["id"]
	jid := id()
	a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,outputs,provider_snapshot) VALUES(?,?,?,'old fixture','{}','succeeded',25,'[]',?)", jid, uid, mid, jsonText(Row{"api_key": "retired-secret", "name": "old supplier"}))
	balances := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	// Exercise upgrading an old database with the former model foreign key.
	a.Store.Exec("ALTER TABLE jobs ADD CONSTRAINT old_fixture_model_fk FOREIGN KEY(model_id) REFERENCES models(id)")
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Seed()
	for _, table := range []string{"models", "prices", "providers", "provider_models", "inventory_routes"} {
		want(t, a.Store.One("SELECT count(*) n FROM " + table)["n"], 0)
	}
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balances)
	job := a.Store.One("SELECT * FROM jobs WHERE id=?", jid)
	want(t, job["cost"], 25)
	if str(object(job["provider_snapshot"])["modelName"]) == "" {
		t.Fatal("history lost model name")
	}
	if object(job["provider_snapshot"])["api_key"] != nil {
		t.Fatal("completed history retained retired credential")
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	want(t, object(c.ok(t, "GET", "/jobs?type=image", nil, 200))["total"], 1)
	want(t, object(c.ok(t, "GET", "/admin/jobs", nil, 200))["total"], 1)
	c.ok(t, "GET", "/jobs/"+jid, nil, 200)
	c.ok(t, "GET", "/admin/provider-inventory", nil, 404)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "new account", "protocol": "native:apiyi", "base_url": "http://127.0.0.1:9", "apiKey": "new-account-fixture-key"}, 201))
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", p["id"])["n"], 1)
	if _, err := a.ImportProviderInventory(strings.NewReader(inventoryFixtureCSV(t)), true); err == nil {
		t.Fatal("legacy import recreated retired route architecture")
	}
}

func TestProviderInstancesIndependentCatalogAndExecution(t *testing.T) {
	a := setupIntegration(t)
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Seed()
	var mu sync.Mutex
	seen := map[string]int{}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")+" "+r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/models":
			send(w, 200, Row{"data": []Row{{"id": "gpt-5.4-mini", "type": "text"}, {"id": "skip-3d", "type": "3d"}}})
		case "/v1/responses":
			send(w, 200, Row{"output_text": "isolated supplier instance output"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	protocols := array(c.ok(t, "GET", "/admin/provider-protocols", nil, 200))
	native := 0
	for _, raw := range protocols {
		if strings.HasPrefix(str(object(raw)["id"]), "native:") {
			native++
		}
	}
	want(t, native, 34)
	for _, suffix := range []string{"a", "b"} {
		key := "instance-fixture-key-" + suffix
		p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "same supplier", "protocol": "native:apiyi", "base_url": mock.URL + "/v1", "apiKey": key}, 201))
		pid := str(p["id"])
		path := "/admin/providers/" + pid
		c.ok(t, "POST", path+"/sync-models", Row{}, 200)
		c.ok(t, "POST", path+"/sync-models", Row{}, 200)
		catalog := object(c.ok(t, "GET", path+"/models", nil, 200))
		want(t, catalog["total"], 1)
		entry := object(array(catalog["items"])[0])
		want(t, entry["type"], "text")
		published := object(c.ok(t, "POST", path+"/models/"+str(entry["id"])+"/publish", Row{"modelId": "instance-" + suffix, "name": "text " + suffix, "type": "text"}, 201))
		m := object(published["model"])
		m["enabled"] = true
		c.ok(t, "PUT", "/admin/models/instance-"+suffix, m, 200)
		c.ok(t, "POST", "/jobs", Row{"modelId": m["id"], "prompt": "unpriced must stop", "params": Row{}}, 400)
		c.ok(t, "POST", "/admin/prices", Row{"model_id": m["id"], "label": "fixture", "unit": "request", "credits": 1, "conditions": Row{}}, 200)
		j := object(c.ok(t, "POST", "/jobs", Row{"modelId": m["id"], "prompt": "mock only", "params": Row{}}, 202))
		stored := a.Store.One("SELECT * FROM jobs WHERE id=?", j["id"])
		plain, err := a.decrypt(str(object(stored["provider_snapshot"])["api_key"]))
		if err != nil {
			t.Fatal(err)
		}
		want(t, plain, key)
		a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", j["id"])
		stored["status"] = "running"
		a.processJob(context.Background(), stored)
		want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", j["id"])["status"], "succeeded")
		want(t, object(c.ok(t, "POST", path+"/key", Row{}, 200))["apiKey"], key)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers")["n"], 2)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 0)
	mu.Lock()
	defer mu.Unlock()
	for _, suffix := range []string{"a", "b"} {
		want(t, seen["Bearer instance-fixture-key-"+suffix+" /v1/models"], 2)
		want(t, seen["Bearer instance-fixture-key-"+suffix+" /v1/responses"], 1)
	}
}

func TestProviderInstancesManualModelsAndCrossAccountIsolation(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "manual ALI", "protocol": "native:ali", "base_url": "http://127.0.0.1:9", "apiKey": "manual-fixture-key"}, 201))
	path := "/admin/providers/" + str(p["id"])
	c.ok(t, "POST", path+"/sync-models", Row{}, 400)
	entry := object(c.ok(t, "POST", path+"/models", Row{"upstreamModel": "qwen-image-2.1-pro", "name": "manual image", "type": "image", "parameters": []any{}, "useProtocolDefaults": true}, 201))
	if len(array(entry["parameters"])) == 0 {
		t.Fatal("manual model did not receive protocol defaults")
	}
	other := object(c.ok(t, "POST", "/admin/providers", Row{"name": "manual ALI 2", "protocol": "native:ali", "base_url": "http://127.0.0.1:9", "apiKey": "manual-fixture-key-2"}, 201))
	c.ok(t, "POST", "/admin/providers/"+str(other["id"])+"/models/"+str(entry["id"])+"/publish", Row{"modelId": "wrong-account", "name": "wrong", "type": "image"}, 404)
	c.ok(t, "POST", path+"/models/"+str(entry["id"])+"/publish", Row{"modelId": "manual-ali-model", "name": "manual image", "type": "image"}, 201)
	c.ok(t, "POST", path+"/models", Row{"upstreamModel": "invalid-3d", "type": "3d"}, 400)
	c.ok(t, "DELETE", path, nil, 200)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id=?", p["id"])["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id=?", other["id"])["n"], 1)
}

func TestProviderInstancesMigrationKeepsPendingExecutionSnapshot(t *testing.T) {
	a := setupIntegration(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer pending-fixture-key" {
			http.Error(w, "unexpected fixture request", 400)
			return
		}
		send(w, 200, Row{"output_text": "old pending task survived reset"})
	}))
	defer mock.Close()
	mid := seedInventoryTestRoute(a, "APIYI", "gpt-5.4", "text", mock.URL, "pending-fixture-key")
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	j := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "pending fixture", "params": Row{}}, 202))
	balance := a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"]
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM models")["n"], 0)
	stored := a.Store.One("SELECT * FROM jobs WHERE id=?", j["id"])
	key, err := a.decrypt(str(object(stored["provider_snapshot"])["api_key"]))
	if err != nil {
		t.Fatal(err)
	}
	want(t, key, "pending-fixture-key")
	a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", j["id"])
	stored["status"] = "running"
	a.processJob(context.Background(), stored)
	want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", j["id"])["status"], "succeeded")
	want(t, a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"], balance)
}
