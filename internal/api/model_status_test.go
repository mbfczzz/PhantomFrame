package api

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestModelStatusStatistics(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	viewer := object(user.ok(t, "POST", "/auth/register", Row{"username": "status-viewer", "password": "Model-status-viewer-2026"}, 200))
	adminID := object(admin.ok(t, "GET", "/me", nil, 200))["id"]
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Status channel", "protocol": "json-async", "base_url": "https://example.invalid/v1", "apiKey": "never-expose-status-secret"}, 201))
	otherProvider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Separate channel", "protocol": "json-async", "base_url": "https://other.example.invalid/v1"}, 201))
	ids := []string{"health-main", "health-empty", "health-failed", "health-old", "health-boundary", "health-other"}
	for _, mid := range ids {
		pid := provider["id"]
		if mid == "health-other" {
			pid = otherProvider["id"]
		}
		admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": mid, "type": "image", "provider_id": pid, "upstream_model": "same-upstream"}, 201)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	insert := func(mid, status, protocol string, ended time.Time, elapsed time.Duration, index int) {
		t.Helper()
		uid, source := adminID, "web"
		if index%2 == 1 {
			uid, source = viewer["id"], "api"
		}
		var finished any = ended
		if ended.IsZero() {
			finished, ended = nil, now
		}
		snapshot := Row{"protocol": protocol, "api_key": "must-not-expose-cipher", "name": "private snapshot"}
		a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,source,outputs,provider_snapshot,created_at,finished_at,error) VALUES(?,?,?,'private prompt','{}',?,0,?,'[]',?,?,?,'private error')", id(), uid, mid, status, source, jsonText(snapshot), ended.Add(-elapsed), finished)
	}
	for i := 0; i < 12; i++ {
		status := "succeeded"
		if i%3 == 0 {
			status = "failed"
		}
		insert("health-main", status, "json-async", now.Add(-time.Duration(12-i)*time.Minute), time.Duration(100+i)*time.Second, i)
	}
	for i, status := range []string{"cancelled", "queued", "running"} {
		insert("health-main", status, "json-async", now.Add(-time.Second), time.Second, i)
	}
	insert("health-main", "succeeded", "demo", now.Add(-time.Second), time.Second, 0)
	insert("health-main", "failed", "demo", now.Add(-time.Second), time.Second, 0)
	insert("health-main", "failed", "json-async", now.Add(time.Hour), time.Second, 0)
	insert("health-main", "failed", "json-async", time.Time{}, time.Second, 0)
	insert("health-failed", "failed", "json-async", now.Add(-time.Minute), 50*time.Second, 0)
	insert("health-failed", "failed", "json-async", now.Add(-time.Minute), time.Second, 1)
	insert("health-old", "succeeded", "json-async", now.Add(-25*time.Hour), time.Second, 0)
	insert("health-boundary", "succeeded", "json-async", now.Add(-24*time.Hour), 5*time.Second, 0)
	insert("health-boundary", "failed", "json-async", now.Add(-24*time.Hour-time.Millisecond), time.Second, 0)
	insert("health-other", "succeeded", "json-async", now.Add(-time.Second), 350*time.Millisecond, 0)
	stats := a.Store.ModelStatus(ids, now)
	main := object(stats["health-main"])
	want(t, main["completed24h"], 12)
	want(t, main["succeeded24h"], 8)
	want(t, main["failed24h"], 4)
	want(t, main["successRate24h"], 66.7)
	want(t, main["minSeconds24h"], 101)
	want(t, main["maxSeconds24h"], 111)
	if !reflect.DeepEqual(main["recent"], []string{"succeeded", "failed", "succeeded", "succeeded", "failed", "succeeded", "succeeded", "failed", "succeeded", "succeeded"}) {
		t.Fatal("recent outcomes must be capped at ten and sorted oldest to newest")
	}
	for _, mid := range []string{"health-empty", "health-old"} {
		want(t, object(stats[mid])["completed24h"], 0)
		want(t, object(stats[mid])["successRate24h"], nil)
		want(t, object(stats[mid])["minSeconds24h"], nil)
	}
	want(t, len(object(stats["health-empty"])["recent"].([]string)), 0)
	want(t, len(object(stats["health-old"])["recent"].([]string)), 1)
	want(t, object(stats["health-failed"])["successRate24h"], 0)
	want(t, object(stats["health-failed"])["minSeconds24h"], nil)
	want(t, object(stats["health-boundary"])["completed24h"], 1)
	want(t, object(stats["health-boundary"])["successRate24h"], 100)
	want(t, object(stats["health-other"])["completed24h"], 1)
	want(t, object(stats["health-other"])["minSeconds24h"], .35)
	want(t, len(a.Store.ModelStatus(nil, now)), 0)
	before := jsonText(admin.ok(t, "GET", "/admin/models", nil, 200))
	anonymous := &testClient{server: server.URL}
	anonymous.ok(t, "GET", "/models/status", nil, 401)
	public := object(user.ok(t, "GET", "/models/status", nil, 200))
	want(t, public["windowHours"], 24)
	want(t, object(object(public["models"])["health-main"])["completed24h"], 12)
	for _, private := range []string{"never-expose-status-secret", "must-not-expose-cipher", "private prompt", "private error", "private snapshot", "status-viewer", str(viewer["id"]), str(adminID), "userId", "jobId", "outputs"} {
		if strings.Contains(jsonText(public), private) {
			t.Fatal("public model statistics contained private job data")
		}
	}
	want(t, jsonText(admin.ok(t, "GET", "/models/status", nil, 200)), jsonText(public))
	// A model or channel being disabled must take effect even when statistics are cached.
	a.Store.Exec("UPDATE models SET enabled=0 WHERE id='health-main'")
	hidden := object(object(user.ok(t, "GET", "/models/status", nil, 200))["models"])
	if _, exists := hidden["health-main"]; exists {
		t.Fatal("disabled model survived the status cache")
	}
	a.Store.Exec("UPDATE models SET enabled=1 WHERE id='health-main'")
	a.Store.Exec("UPDATE providers SET enabled=0 WHERE id=?", provider["id"])
	hidden = object(object(user.ok(t, "GET", "/models/status", nil, 200))["models"])
	for _, mid := range ids[:5] {
		if _, exists := hidden[mid]; exists {
			t.Fatal("disabled channel survived the status cache")
		}
	}
	a.Store.Exec("UPDATE providers SET enabled=1 WHERE id=?", provider["id"])
	want(t, jsonText(admin.ok(t, "GET", "/admin/models", nil, 200)), before)
	// Existing installations receive the index without altering their records.
	count := a.Store.One("SELECT count(*) n FROM jobs")["n"]
	a.Store.Exec("CREATE INDEX jobs_model_legacy ON jobs(model_id)")
	a.Store.Exec("ALTER TABLE jobs DROP INDEX jobs_model_finished")
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM jobs")["n"], count)
}
