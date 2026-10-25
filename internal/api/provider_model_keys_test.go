package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	assets "phantomframe/server"
)

func TestProviderModelCredentials(t *testing.T) {
	a := setupIntegration(t)
	local := httptest.NewServer(a.Handler())
	defer local.Close()
	admin := &testClient{server: local.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	user := &testClient{server: local.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "model-key-user", "password": "Test-model-key-2026"}, 200)
	anonymous := &testClient{server: local.URL}
	apiKey := object(admin.ok(t, "POST", "/keys", Row{"name": "no model key management"}, 201))
	apiClient := &testClient{server: local.URL, key: str(apiKey["token"])}
	requests := make(chan Row, 50)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if r.URL.Path == "/v1/models" {
			want(t, auth, "Bearer channel-default")
			json.NewEncoder(w).Encode(Row{"data": []Row{{"id": "shared-video", "type": "video"}}})
			return
		}
		requests <- Row{"path": r.URL.Path, "auth": auth}
		if auth == "Bearer rejected-model-key" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/videos":
			json.NewEncoder(w).Encode(Row{"id": "key-task", "status": "queued"})
		case r.URL.Path == "/v1/videos/key-task":
			json.NewEncoder(w).Encode(Row{"id": "key-task", "status": "completed"})
		case r.URL.Path == "/v1/videos/key-task/content":
			w.Header().Set("Content-Type", "video/webm")
			w.Write(assets.DemoVideo)
		default:
			t.Error("unexpected upstream request")
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Key channel", "protocol": "openai-video", "base_url": upstream.URL + "/v1", "apiKey": "channel-default", "config": Row{"pollInterval": 20}}, 201))
	other := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Other key channel", "protocol": "openai-video", "base_url": upstream.URL + "/v1", "apiKey": "other-channel-default", "config": Row{"pollInterval": 20}}, 201))
	pid := str(provider["id"])
	defaultCipher := a.Store.One("SELECT api_key FROM providers WHERE id=?", pid)["api_key"]
	base := "/admin/providers/" + pid
	entry := object(admin.ok(t, "POST", base+"/models", Row{"upstreamModel": "shared-video", "name": "Shared video", "type": "video"}, 201))
	keyPath := base + "/models/" + str(entry["id"]) + "/key"
	for _, client := range []*testClient{anonymous, user, apiClient} {
		status := 403
		if client == anonymous {
			status = 401
		}
		client.ok(t, "PUT", keyPath, Row{"apiKey": "never-save"}, status)
	}
	admin.ok(t, "PUT", "/admin/providers/"+str(other["id"])+"/models/"+str(entry["id"])+"/key", Row{"apiKey": "never-save"}, 404)
	for _, payload := range []Row{{}, {"apiKey": "  "}, {"apiKey": "line\r\nbreak"}, {"apiKey": strings.Repeat("x", 4097)}, {"useDefault": "true"}, {"apiKey": "conflict", "useDefault": true}} {
		admin.ok(t, "PUT", keyPath, payload, 400)
	}
	setKey := func(key string) string {
		t.Helper()
		response := object(admin.ok(t, "PUT", keyPath, Row{"apiKey": key}, 200))
		want(t, response["hasKey"], true)
		cipher := str(a.Store.One("SELECT api_key FROM provider_models WHERE id=?", entry["id"])["api_key"])
		plain, err := a.decrypt(cipher)
		if err != nil || plain != key || cipher == key {
			t.Fatal("model key must be encrypted at rest")
		}
		if strings.Contains(jsonText(response), key) || strings.Contains(jsonText(response), cipher) {
			t.Fatal("model key leaked in response")
		}
		return cipher
	}
	cipher := setKey("model-key-original")
	// A draft key survives publication, catalog backfills, metadata edits and sync.
	admin.ok(t, "PUT", base+"/models/"+str(entry["id"]), Row{"upstreamModel": "shared-video", "name": "Renamed video", "type": "video"}, 200)
	admin.ok(t, "POST", base+"/models/"+str(entry["id"])+"/publish", Row{"modelId": "key-primary", "name": "Key primary", "type": "video"}, 201)
	primary := a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id='key-primary'"))
	primary["enabled"] = true
	admin.ok(t, "PUT", "/admin/models/key-primary", primary, 200)
	for _, model := range []Row{
		{"id": "key-alias", "provider_id": pid, "upstream_model": "shared-video"},
		{"id": "key-sibling", "provider_id": pid, "upstream_model": "sibling-video"},
		{"id": "key-other-channel", "provider_id": other["id"], "upstream_model": "shared-video"},
	} {
		model["name"], model["type"] = model["id"], "video"
		admin.ok(t, "POST", "/admin/models", model, 201)
	}
	for _, mid := range []string{"key-primary", "key-alias", "key-sibling", "key-other-channel"} {
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "test", "credits": 0, "unit": "request"}, 200)
	}
	a.backfillProviderModels()
	admin.ok(t, "POST", base+"/sync-models", nil, 200)
	want(t, a.Store.One("SELECT api_key FROM provider_models WHERE id=?", entry["id"])["api_key"], cipher)
	makeJob := func(mid string, client *testClient) Row {
		t.Helper()
		return object(client.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "credential routing"}, 202))
	}
	runJob := func(job Row, key string) {
		t.Helper()
		row := a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"])
		plain, err := a.decrypt(str(object(row["provider_snapshot"])["api_key"]))
		if err != nil || plain != key {
			t.Fatal("effective credential not captured in the job snapshot")
		}
		out, err := a.upstream(context.Background(), row)
		if err != nil || len(out) != 1 {
			t.Fatalf("mock generation failed: %v", err)
		}
		for _, path := range []string{"/v1/videos", "/v1/videos/key-task", "/v1/videos/key-task/content"} {
			select {
			case request := <-requests:
				want(t, request["path"], path)
				want(t, request["auth"], "Bearer "+key)
			default:
				t.Fatal("missing authenticated upstream request")
			}
		}
		admin.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
	}
	oldJob := makeJob("key-primary", admin)
	rotated := setKey("model-key-rotated")
	runJob(oldJob, "model-key-original")
	runJob(makeJob("key-primary", apiClient), "model-key-rotated")
	runJob(makeJob("key-alias", admin), "model-key-rotated")
	runJob(makeJob("key-sibling", admin), "channel-default")
	runJob(makeJob("key-other-channel", admin), "other-channel-default")
	queued := makeJob("key-primary", admin)
	response := object(admin.ok(t, "PUT", keyPath, Row{"useDefault": true}, 200))
	want(t, response["hasKey"], false)
	want(t, str(a.Store.One("SELECT api_key FROM provider_models WHERE id=?", entry["id"])["api_key"]), "")
	runJob(queued, "model-key-rotated")
	runJob(makeJob("key-primary", admin), "channel-default")
	// An invalid independent key must fail rather than silently billing another key.
	setKey("rejected-model-key")
	rejected := makeJob("key-primary", admin)
	if _, err := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", rejected["id"])); err == nil {
		t.Fatal("an upstream credential error should not fall back to the channel key")
	}
	want(t, len(requests), 1)
	want(t, (<-requests)["auth"], "Bearer rejected-model-key")
	admin.ok(t, "POST", "/jobs/"+str(rejected["id"])+"/cancel", nil, 200)
	for _, path := range []string{base + "/models", "/admin/providers", "/admin/models", "/models", "/jobs", "/admin/jobs", "/admin/audit"} {
		body := jsonText(admin.ok(t, "GET", path, nil, 200))
		for _, secret := range []string{"model-key-original", "model-key-rotated", "rejected-model-key", cipher, rotated} {
			if strings.Contains(body, secret) {
				t.Fatalf("credential leaked from %s", path)
			}
		}
	}
	want(t, a.Store.One("SELECT api_key FROM providers WHERE id=?", pid)["api_key"], defaultCipher)
}

func TestProviderModelKeyMigration(t *testing.T) {
	a := setupIntegration(t)
	count := a.Store.One("SELECT count(*) n FROM provider_models")["n"]
	a.Store.Exec("ALTER TABLE provider_models DROP COLUMN api_key")
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM provider_models")["n"], count)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE api_key IS NOT NULL")["n"], 0)
}
