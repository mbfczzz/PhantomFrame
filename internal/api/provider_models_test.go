package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProviderCatalogPublishing(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	anonymous := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	user.ok(t, "POST", "/auth/register", Row{"username": "catalog-user", "password": "Catalog-pass-2026"}, 200)
	key := object(admin.ok(t, "POST", "/keys", Row{"name": "no catalog administration"}, 201))
	keyClient := &testClient{server: server.URL, key: str(key["token"])}
	original := jsonText(admin.ok(t, "GET", "/admin/models", nil, 200))
	count := a.Store.One("SELECT count(*) n FROM provider_models")["n"]
	if integer(count) == 0 {
		t.Fatal("existing models must be associated with their channels")
	}
	a.backfillProviderModels()
	want(t, a.Store.One("SELECT count(*) n FROM provider_models")["n"], count)
	want(t, jsonText(admin.ok(t, "GET", "/admin/models", nil, 200)), original)
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Catalog demo", "protocol": "demo"}, 201))
	pid := str(provider["id"])
	path := "/admin/providers/" + pid
	for _, endpoint := range []struct{ method, path string }{{"GET", path + "/models"}, {"POST", path + "/models"}, {"PUT", path + "/models/missing"}, {"DELETE", path + "/models/missing"}, {"POST", path + "/models/missing/publish"}, {"POST", path + "/sync-models"}} {
		anonymous.ok(t, endpoint.method, endpoint.path, Row{}, 401)
		user.ok(t, endpoint.method, endpoint.path, Row{}, 403)
		keyClient.ok(t, endpoint.method, endpoint.path, Row{}, 403)
	}
	params := []Row{{"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image"}}
	draft := object(admin.ok(t, "POST", path+"/models", Row{"upstreamModel": "vendor/image", "name": "Draft image", "type": "image", "parameters": params}, 201))
	entryPath := path + "/models/" + str(draft["id"])
	want(t, len(array(draft["publishedModels"])), 0)
	want(t, object(admin.ok(t, "GET", path+"/models?status=unpublished", nil, 200))["total"], 1)
	if strings.Contains(jsonText(admin.ok(t, "GET", "/admin/models", nil, 200)), "vendor/image") {
		t.Fatal("draft appeared in model management before publication")
	}
	admin.ok(t, "POST", path+"/models", Row{"upstreamModel": "vendor/image", "name": "Duplicate", "type": "image"}, 409)
	admin.ok(t, "PUT", "/admin/providers/demo/models/"+str(draft["id"]), Row{"upstreamModel": "vendor/image", "name": "Wrong channel"}, 404)
	payload := Row{"modelId": "published-image", "name": "Published image", "type": "image", "category": "Catalog"}
	results := make(chan int, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &testClient{server: server.URL, cookie: admin.cookie}
			code, _ := client.request(t, "POST", entryPath+"/publish", payload)
			results <- code
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for code := range results {
		if code == 201 {
			created++
		} else {
			want(t, code, 200)
		}
	}
	want(t, created, 1)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id=? AND upstream_model='vendor/image'", pid)["n"], 1)
	want(t, a.Store.One("SELECT count(*) n FROM audit_log WHERE action='provider_model.publish'")["n"], 1)
	published := a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id='published-image'"))
	want(t, published["enabled"], false)
	want(t, len(array(published["parameters"])), 1)
	if strings.Contains(jsonText(user.ok(t, "GET", "/models", nil, 200)), "published-image") {
		t.Fatal("newly published model should remain disabled until configured")
	}
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "published-image", "label": "base", "unit": "request", "credits": 7, "conditions": Row{}}, 200)
	published["enabled"] = true
	published["name"] = "Configured image"
	admin.ok(t, "PUT", "/admin/models/published-image", published, 200)
	payload["modelId"] = "must-not-create"
	payload["name"] = "must-not-overwrite"
	repeated := object(admin.ok(t, "POST", entryPath+"/publish", payload, 200))
	want(t, repeated["alreadyPublished"], true)
	want(t, object(repeated["model"])["name"], "Configured image")
	want(t, object(array(object(repeated["model"])["prices"])[0])["credits"], 7)
	want(t, object(admin.ok(t, "GET", path+"/models?status=published", nil, 200))["total"], 1)
	admin.ok(t, "PUT", entryPath, Row{"upstreamModel": "changed", "name": "changed"}, 409)
	admin.ok(t, "DELETE", entryPath, nil, 409)
	second := object(admin.ok(t, "POST", path+"/models", Row{"upstreamModel": "second", "name": "Second", "type": "video"}, 201))
	secondPath := path + "/models/" + str(second["id"])
	admin.ok(t, "POST", secondPath+"/publish", Row{"modelId": "published-image", "name": "Collision", "type": "video"}, 409)
	want(t, object(admin.ok(t, "GET", path+"/models?status=unpublished", nil, 200))["total"], 1)
	want(t, object(admin.ok(t, "GET", path+"/models?search=SECOND", nil, 200))["total"], 1)
	firstPage := object(admin.ok(t, "GET", path+"/models?limit=1", nil, 200))
	nextPage := object(admin.ok(t, "GET", path+"/models?limit=1&page=2", nil, 200))
	if object(array(firstPage["items"])[0])["id"] == object(array(nextPage["items"])[0])["id"] {
		t.Fatal("catalog pages repeat entries")
	}
	admin.ok(t, "GET", path+"/models?status=invalid", nil, 400)
	provider["enabled"] = false
	admin.ok(t, "PUT", path, provider, 200)
	admin.ok(t, "POST", secondPath+"/publish", Row{"modelId": "disabled-channel-model", "name": "No", "type": "video"}, 400)
	provider["enabled"] = true
	admin.ok(t, "PUT", path, provider, 200)
	admin.ok(t, "DELETE", secondPath, nil, 200)
	admin.ok(t, "DELETE", "/admin/models/published-image", nil, 200)
	want(t, object(admin.ok(t, "GET", path+"/models?status=unpublished", nil, 200))["total"], 1)
	admin.ok(t, "DELETE", path, nil, 200)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", pid)["n"], 0)
}

func TestProviderModelDiscovery(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	var mode atomic.Int32
	var leaked atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true); w.WriteHeader(200) }))
	defer sink.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer discovery-secret" {
			w.WriteHeader(401)
			return
		}
		switch mode.Load() {
		case 1:
			w.WriteHeader(500)
			_, _ = w.Write([]byte("private upstream details"))
			return
		case 2:
			http.Redirect(w, r, sink.URL, http.StatusFound)
			return
		case 3:
			_, _ = w.Write([]byte(`{"data":"invalid"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(Row{"data": []Row{{"id": "upstream-image", "name": "Image", "type": "image"}, {"id": "vendor/video", "name": "Video"}, {"id": "upstream-image"}}})
	}))
	defer upstream.Close()
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Discovery", "protocol": "json-async", "base_url": upstream.URL + "/v1", "apiKey": "discovery-secret"}, 201))
	path := "/admin/providers/" + str(provider["id"])
	result := object(admin.ok(t, "POST", path+"/sync-models", nil, 200))
	want(t, result["received"], 2)
	want(t, result["added"], 2)
	entries := object(admin.ok(t, "GET", path+"/models", nil, 200))
	want(t, entries["total"], 2)
	if strings.Contains(jsonText(entries), "discovery-secret") {
		t.Fatal("channel key leaked into catalog")
	}
	for _, value := range array(entries["items"]) {
		want(t, len(array(object(value)["publishedModels"])), 0)
	}
	entry := object(array(entries["items"])[0])
	body := Row{"upstreamModel": entry["upstreamModel"], "name": "Custom name", "type": "video", "parameters": []Row{{"name": "audio", "label": "参考音频", "type": "assets", "itemType": "audio", "max": 2}}}
	admin.ok(t, "PUT", path+"/models/"+str(entry["id"]), body, 200)
	result = object(admin.ok(t, "POST", path+"/sync-models", nil, 200))
	want(t, result["added"], 0)
	current := a.Store.providerModelView(a.Store.One("SELECT * FROM provider_models WHERE id=?", entry["id"]))
	want(t, current["name"], "Custom name")
	want(t, object(array(current["parameters"])[0])["max"], 2)
	for _, value := range []int32{1, 2, 3} {
		mode.Store(value)
		status, reply := admin.request(t, "POST", path+"/sync-models", nil)
		want(t, status, 502)
		if strings.Contains(jsonText(reply), "private upstream details") {
			t.Fatal("upstream response leaked")
		}
		want(t, object(admin.ok(t, "GET", path+"/models", nil, 200))["total"], 2)
	}
	if leaked.Load() {
		t.Fatal("model discovery followed a redirect")
	}
	admin.ok(t, "POST", "/admin/providers/demo/sync-models", nil, 400)
}
