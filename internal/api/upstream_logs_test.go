package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	assets "phantomframe/server"
)

func TestUpstreamCallLogs(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin, user := &testClient{server: server.URL}, &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	u := object(user.ok(t, "POST", "/auth/register", Row{"username": "upstream-log-user", "password": "Upstream-log-pass-2026"}, 200))
	outsider := &testClient{server: server.URL}
	outsider.ok(t, "POST", "/auth/register", Row{"username": "upstream-log-outsider", "password": "Upstream-log-pass-2026"}, 200)
	for _, client := range []*testClient{{server: server.URL}, user} {
		status := 403
		if client.cookie == "" {
			status = 401
		}
		client.ok(t, "GET", "/admin/upstream-calls", nil, status)
	}
	key := object(admin.ok(t, "POST", "/keys", Row{"name": "upstream logs admin key"}, 201))
	(&testClient{server: server.URL, key: str(key["token"])}).ok(t, "GET", "/admin/upstream-calls", nil, 403)

	queued := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "queued log"}, 202))
	queuedLog := object(admin.ok(t, "GET", "/admin/upstream-calls/"+str(queued["id"]), nil, 200))
	want(t, queuedLog["phase"], "queued")
	want(t, queuedLog["requestCount"], 0)
	want(t, queuedLog["username"], "upstream-log-user")
	outsider.ok(t, "GET", "/jobs/"+str(queued["id"])+"/upstream", nil, 404)
	user.ok(t, "POST", "/jobs/"+str(queued["id"])+"/cancel", nil, 200)
	cancelled := object(user.ok(t, "GET", "/jobs/"+str(queued["id"])+"/upstream", nil, 200))
	want(t, cancelled["status"], "cancelled")
	want(t, cancelled["refunded"], true)
	want(t, cancelled["startedAt"], nil)

	const secret = "upstream-provider-secret-never-log"
	var reject atomic.Bool
	var requests atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		w.Header().Set("X-Request-Id", fmt.Sprintf("mock-request-%d", n))
		if reject.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(Row{"message": secret})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/content") {
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write(assets.DemoVideo)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		state := "completed"
		if r.Method == "POST" {
			state = "queued"
		}
		_ = json.NewEncoder(w).Encode(Row{"id": "mock-upstream-task", "status": state})
	}))
	defer up.Close()
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "日志测试渠道", "protocol": "openai-video", "base_url": up.URL, "apiKey": secret, "config": Row{"pollInterval": 20, "submitPath": "/videos?token=" + secret}}, 201))
	admin.ok(t, "POST", "/admin/models", Row{"id": "upstream-log-video", "name": "上游日志视频", "type": "video", "provider_id": provider["id"], "upstream_model": "mock-video", "parameters": []Row{{"name": "duration", "label": "时长", "type": "number", "default": 5}, {"name": "api_key", "label": "测试敏感字段", "type": "text", "default": secret}}}, 201)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "upstream-log-video", "label": "按次", "unit": "request", "credits": 2}, 200)
	ctx, cancel := context.WithCancel(context.Background())
	done := a.StartWorker(ctx)
	defer func() { cancel(); <-done }()
	await := func(id, status string) Row {
		t.Helper()
		for i := 0; i < 160; i++ {
			j := a.Store.One("SELECT * FROM jobs WHERE id=?", id)
			if j["status"] == status {
				for attempt := 0; attempt < 20; attempt++ {
					if a.Store.One("SELECT status FROM upstream_calls WHERE id=?", id)["status"] == status {
						return j
					}
					time.Sleep(10 * time.Millisecond)
				}
				return j
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatalf("job %s did not reach %s", id, status)
		return nil
	}
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": "upstream-log-video", "prompt": "local simulated video"}, 202))
	jid := str(job["id"])
	await(jid, "succeeded")
	entry := object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))
	want(t, entry["upstreamId"], "mock-upstream-task")
	want(t, entry["phase"], "completed")
	want(t, entry["lastHttpStatus"], 200)
	want(t, entry["requestCount"], 3)
	want(t, entry["lastRequestId"], "mock-request-3")
	want(t, entry["refunded"], false)
	if entry["startedAt"] == nil || entry["finishedAt"] == nil || num(entry["elapsedMs"]) < 0 {
		t.Fatal("execution timestamps were not captured")
	}
	if strings.Contains(jsonText(entry), secret) || strings.Contains(str(entry["lastUrl"]), "?") {
		t.Fatal("provider key or signed URL query leaked to the log")
	}
	if strings.Contains(str(a.Store.One("SELECT params FROM upstream_calls WHERE id=?", jid)["params"]), secret) {
		t.Fatal("sensitive parameter was persisted without redaction")
	}
	public := object(user.ok(t, "GET", "/jobs/"+jid+"/upstream", nil, 200))
	for _, field := range []string{"lastUrl", "params", "username", "protocol", "lastRequestId"} {
		if _, present := public[field]; present {
			t.Fatalf("admin diagnostic field %s leaked into the user view", field)
		}
	}
	filtered := object(admin.ok(t, "GET", "/admin/upstream-calls?providerId="+str(provider["id"])+"&status=succeeded&search=mock-upstream-task&limit=1", nil, 200))
	want(t, filtered["total"], 1)
	want(t, len(array(filtered["items"])), 1)
	user.ok(t, "DELETE", "/jobs/"+jid, nil, 200)
	preserved := object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))
	want(t, preserved["status"], "succeeded")
	want(t, preserved["jobAvailable"], true)
	want(t, preserved["upstreamId"], "mock-upstream-task")
	want(t, preserved["requestCount"], 3)

	reject.Store(true)
	failure := object(user.ok(t, "POST", "/jobs", Row{"modelId": "upstream-log-video", "prompt": "simulated provider auth failure"}, 202))
	await(str(failure["id"]), "failed")
	failed := object(admin.ok(t, "GET", "/admin/upstream-calls/"+str(failure["id"]), nil, 200))
	want(t, failed["lastHttpStatus"], 401)
	want(t, failed["failurePhase"], "submitting")
	want(t, failed["refunded"], true)
	want(t, failed["requestCount"], 1)
	if strings.Contains(jsonText(failed), secret) {
		t.Fatal("failed provider response leaked a secret")
	}
	// A repeated migration fills missing history, preserves current diagnostics,
	// and leaves deleted job logs available without manufacturing old timings.
	a.Store.Exec("DELETE FROM upstream_calls WHERE id=?", failure["id"])
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	historical := object(admin.ok(t, "GET", "/admin/upstream-calls/"+str(failure["id"]), nil, 200))
	want(t, historical["legacy"], true)
	want(t, historical["status"], "failed")
	want(t, historical["startedAt"], nil)
	want(t, object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))["requestCount"], 3)
	want(t, a.Store.One("SELECT count(*) n FROM upstream_calls WHERE user_id=?", u["id"])["n"], 3)
	for i := 0; i < 5; i++ {
		a.Store.Exec("INSERT INTO upstream_calls(id,user_id,username,display_name,provider_id,provider_name,protocol,model_id,model_name,model_type,upstream_model,source,status,phase,params,cost,error,created_at,finished_at) VALUES(?,?,?,?,'retired-channel','已删除渠道','openai-video','retired-model','已删除模型','video','old-upstream','api','succeeded','completed','{}',0,'',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))", id(), u["id"], "upstream-log-user", "测试用户")
	}
	firstPage := object(admin.ok(t, "GET", "/admin/upstream-calls?providerId=retired-channel&limit=2&page=1", nil, 200))
	secondPage := object(admin.ok(t, "GET", "/admin/upstream-calls?providerId=retired-channel&limit=2&page=2", nil, 200))
	want(t, firstPage["total"], 5)
	want(t, len(array(firstPage["items"])), 2)
	if object(array(firstPage["items"])[0])["id"] == object(array(secondPage["items"])[0])["id"] {
		t.Fatal("upstream log pagination repeated the same row")
	}
	options := object(admin.ok(t, "GET", "/admin/upstream-calls/options", nil, 200))
	found := false
	for _, raw := range array(options["providers"]) {
		if object(raw)["id"] == "retired-channel" {
			found = true
		}
	}
	if !found {
		t.Fatal("deleted channel history disappeared from filter options")
	}
	admin.ok(t, "GET", "/admin/upstream-calls?status=invalid", nil, 400)
	admin.ok(t, "GET", "/admin/upstream-calls?from=2026-10-27&to=2026-10-26", nil, 400)
}

func TestUpstreamLogRedaction(t *testing.T) {
	params := safeLogParams(Row{"api_key": "secret", "max_tokens": 100, "nested": Row{"Authorization": "Bearer secret", "url": "https://user:pass@files.example/a.png?signature=secret#token"}, "body": "data:image/png;base64,aaaa", "note": "contains real-key-value"}, "real-key-value")
	want(t, params["api_key"], "[已隐藏]")
	want(t, params["max_tokens"], 100)
	want(t, object(params["nested"])["url"], "https://files.example/a.png")
	want(t, params["body"], "[内嵌文件已省略]")
	if strings.Contains(jsonText(params), "secret") || strings.Contains(jsonText(params), "real-key-value") {
		t.Fatal("sensitive log data was not redacted")
	}
	legacy := upstreamCallView(Row{"id": "old-job", "status": "succeeded", "phase": "completed", "created_at": "2026-01-01 00:00:00.000", "legacy": true}, false)
	want(t, legacy["totalMs"], nil)
	want(t, legacy["elapsedMs"], nil)
}
