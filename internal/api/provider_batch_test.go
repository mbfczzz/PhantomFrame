package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBatchedModelViewsPreservePricesAndRouteLimits(t *testing.T) {
	a := setupIntegration(t)
	seedInventoryTestRoute(a, "APIYI", "gpt-image-2-APIYI", "image", "http://127.0.0.1:9", "batch-fixture-key")
	models := a.Store.All("SELECT * FROM models ORDER BY id")
	models = append(models, models[0]) // Repeated input must not mutate or duplicate price rules.
	expected := []Row{}
	for _, m := range models {
		expected = append(expected, a.Store.ModelView(m))
	}
	q := &countedQueries{Querier: a.DB}
	s := Store{q}
	actual := s.ModelViews(models)
	want(t, jsonText(actual), jsonText(expected))
	if n := q.calls.Load(); n > 3 {
		t.Fatalf("batch performed %d queries for %d models", n, len(models))
	}
	want(t, jsonText(s.ModelViews(nil)), "[]")
	if strings.Contains(jsonText(actual), "batch-fixture-key") {
		t.Fatal("batch exposed provider credentials")
	}
}

func TestCatalogPageBatchKeepsAccountIsolation(t *testing.T) {
	a := setupIntegration(t)
	upstream := a.Store.One("SELECT upstream_model FROM provider_models WHERE provider_id='demo' LIMIT 1")["upstream_model"]
	a.Store.Exec("INSERT INTO providers(id,name,protocol,api_key,config) VALUES('other-batch-account','other','openai-image','','{}')")
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,tags,parameters) VALUES('foreign-batch-model','foreign','image','other-batch-account',?,'','[]','[]')", upstream)
	q := &countedQueries{Querier: a.DB}
	a.Store = Store{q}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	q.calls.Store(0)
	page := object(c.ok(t, "GET", "/admin/providers/demo/models?limit=20", nil, 200))
	want(t, len(array(page["items"])), 20)
	if strings.Contains(jsonText(page), "foreign-batch-model") {
		t.Fatal("catalog batch included another supplier's publication")
	}
	if n := q.calls.Load(); n > 5 {
		t.Fatalf("catalog page performed %d SQL calls", n)
	}
	for _, raw := range array(page["items"]) {
		entry := object(raw)
		if len(array(entry["publishedModels"])) == 0 {
			t.Fatal("published alias was lost")
		}
	}
}

func TestBulkCatalogSyncRollbackAndReviewedData(t *testing.T) {
	a := setupIntegration(t)
	items := []Row{}
	for i := 0; i < 205; i++ {
		items = append(items, Row{"id": fmt.Sprintf("batch-model-%03d", i), "type": "text"})
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		send(w, 200, Row{"data": items})
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "batch account", "protocol": "native:apiyi", "base_url": mock.URL, "apiKey": "batch-account-fixture-key"}, 201))
	pid := str(p["id"])
	path := "/admin/providers/" + pid
	entry := object(c.ok(t, "POST", path+"/models", Row{"upstreamModel": "batch-model-000", "name": "reviewed name", "type": "text", "config": Row{"chatAPI": "responses"}}, 201))
	c.ok(t, "PUT", path+"/models/"+str(entry["id"])+"/key", Row{"apiKey": "reviewed-independent-fixture-key"}, 200)
	before := jsonText(a.Store.One("SELECT * FROM provider_models WHERE id=?", entry["id"]))
	a.Store.Exec("ALTER TABLE provider_models ADD CONSTRAINT reject_batch_fixture CHECK (upstream_model<>'batch-model-101')")
	c.ok(t, "POST", path+"/sync-models", Row{}, 500)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", pid)["n"], 1)
	want(t, jsonText(a.Store.One("SELECT * FROM provider_models WHERE id=?", entry["id"])), before)
	a.Store.Exec("ALTER TABLE provider_models DROP CHECK reject_batch_fixture")
	want(t, object(c.ok(t, "POST", path+"/sync-models", Row{}, 200))["added"], 204)
	want(t, object(c.ok(t, "POST", path+"/sync-models", Row{}, 200))["added"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", pid)["n"], 205)
	want(t, jsonText(a.Store.One("SELECT * FROM provider_models WHERE id=?", entry["id"])), before)
}
