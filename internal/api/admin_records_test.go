package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAdminRecordsAndPermissions(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	anonymous := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "records-owner", "password": "Records-pass-2026", "role": "admin"}, 200))
	outsider := object(other.ok(t, "POST", "/auth/register", Row{"username": "records-other", "password": "Records-pass-2026"}, 200))
	want(t, owner["role"], "user")
	want(t, object(user.ok(t, "PATCH", "/me", Row{"displayName": "Record owner", "role": "admin"}, 200))["role"], "user")
	key := object(admin.ok(t, "POST", "/keys", Row{"name": "admin key remains generation-only"}, 201))
	keyClient := &testClient{server: server.URL, key: str(key["token"])}
	endpoints := []struct{ method, path string }{
		{"GET", "/admin/stats"}, {"GET", "/admin/models"}, {"POST", "/admin/models"}, {"PUT", "/admin/models/nanobanana"}, {"DELETE", "/admin/models/nanobanana"},
		{"GET", "/admin/prices"}, {"POST", "/admin/prices"}, {"GET", "/admin/providers"}, {"POST", "/admin/providers"},
		{"GET", "/admin/users"}, {"PATCH", "/admin/users/" + str(owner["id"])}, {"POST", "/admin/users/" + str(owner["id"]) + "/credits"},
		{"GET", "/admin/jobs"}, {"GET", "/admin/jobs/missing"}, {"GET", "/admin/jobs/missing/outputs/missing"},
		{"GET", "/admin/codes"}, {"POST", "/admin/codes"}, {"DELETE", "/admin/codes/missing"},
		{"GET", "/admin/transactions"}, {"GET", "/admin/audit"}, {"GET", "/admin/settings"}, {"PUT", "/admin/settings"}, {"GET", "/admin/tickets"},
	}
	for _, endpoint := range endpoints {
		anonymous.ok(t, endpoint.method, endpoint.path, Row{}, 401)
		user.ok(t, endpoint.method, endpoint.path, Row{}, 403)
		keyClient.ok(t, endpoint.method, endpoint.path, Row{}, 403)
	}
	createJob := func(jid, uid, model, source, status, created string, cost int64, refunded bool) {
		kind := "image"
		if model != "nanobanana" {
			kind = "video"
		}
		snapshot := Row{"modelName": "Historical " + kind, "type": kind, "name": "Records test channel", "protocol": "demo", "api_key": "snapshot-secret-do-not-expose"}
		a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,refunded,source,outputs,provider_snapshot,created_at) VALUES(?,?,?,?,?,?,?,?,?,'[]',?,?)", jid, uid, model, "audit record "+jid, "{}", status, cost, refunded, source, jsonText(snapshot), created)
	}
	createJob("oldest-record", str(owner["id"]), "nanobanana", "web", "cancelled", "2024-01-01 00:00:00", 200, true)
	createJob("image-api-record", str(owner["id"]), "nanobanana", "api", "succeeded", "2025-01-02 12:00:00", 125, false)
	createJob("video-web-record", str(outsider["id"]), "sd2.0-933-op", "web", "failed", "2025-01-03 23:59:59.999", 3800, true)
	for i := 0; i < 505; i++ {
		createJob(fmt.Sprintf("batch-record-%04d", i), str(outsider["id"]), "nanobanana", "web", "succeeded", "2026-01-01 00:00:00", 0, false)
	}
	all := object(admin.ok(t, "GET", "/admin/jobs?limit=20", nil, 200))
	want(t, all["total"], 508)
	want(t, len(array(all["items"])), 20)
	want(t, object(all["summary"])["spent"], 1.25)
	last := object(admin.ok(t, "GET", "/admin/jobs?page=26&limit=20", nil, 200))
	want(t, len(array(last["items"])), 8)
	if !strings.Contains(jsonText(last), "oldest-record") {
		t.Fatal("older records must remain accessible beyond 500 rows")
	}
	for query, count := range map[string]int{"source=api": 1, "status=failed": 1, "type=video": 1, "userId=" + str(owner["id"]): 2, "modelId=sd2.0-933-op": 1, "from=2025-01-02&to=2025-01-03": 2, "search=RECORDS-OWNER": 2, "search=Historical%20video": 1, "search=%25": 0} {
		result := object(admin.ok(t, "GET", "/admin/jobs?"+query, nil, 200))
		want(t, result["total"], count)
	}
	for _, query := range []string{"source=invalid", "status=invalid", "type=invalid", "from=2025-02-30", "from=2025-03-03&to=2025-03-01", "search=" + strings.Repeat("a", 601)} {
		admin.ok(t, "GET", "/admin/jobs?"+query, nil, 400)
	}
	want(t, object(user.ok(t, "GET", "/jobs", nil, 200))["total"], 2)
	detail := object(admin.ok(t, "GET", "/admin/jobs/video-web-record", nil, 200))
	want(t, detail["username"], "records-other")
	want(t, detail["refunded"], true)
	want(t, detail["providerName"], "Records test channel")
	if strings.Contains(jsonText(detail), "snapshot-secret") || strings.Contains(jsonText(all), "snapshot-secret") {
		t.Fatal("provider credentials leaked into admin records")
	}
	user.ok(t, "GET", "/jobs/video-web-record", nil, 404)
	admin.ok(t, "GET", "/admin/jobs/missing", nil, 404)
	output, err := a.saveAsset(str(outsider["id"]), "video-web-record", "test-result.svg", "image/svg+xml", ".svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Exec("UPDATE jobs SET outputs=? WHERE id='video-web-record'", jsonText([]any{output}))
	detail = object(admin.ok(t, "GET", "/admin/jobs/video-web-record", nil, 200))
	fileURL := str(object(array(detail["outputs"])[0])["url"])
	request, _ := http.NewRequest("GET", server.URL+fileURL, nil)
	request.Header.Set("Cookie", admin.cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	want(t, response.StatusCode, 200)
	content, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(content), "<svg") {
		t.Fatal("admin output preview did not return generated file")
	}
	user.ok(t, "GET", strings.TrimPrefix(fileURL, "/api"), nil, 403)
	keyClient.ok(t, "GET", strings.TrimPrefix(fileURL, "/api"), nil, 403)
	admin.ok(t, "GET", "/admin/jobs/image-api-record/outputs/"+str(output["assetId"]), nil, 404)
	// The ordinary asset endpoint continues enforcing ownership even for administrators.
	admin.ok(t, "GET", "/assets/"+str(output["assetId"])+"/file", nil, 404)

	codes := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 2, "credits": 25.5}, 201))
	user.ok(t, "POST", "/redeem", Row{"code": codes[0]}, 200)
	expired := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 1, "expiresAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 201))
	for i := 0; i < 505; i++ {
		a.Store.Exec("INSERT INTO redeem_codes(id,code,credits,created_at) VALUES(?,?,100,'2024-01-01')", id(), fmt.Sprintf("BULK-CODE-%04d", i))
	}
	want(t, object(admin.ok(t, "GET", "/admin/codes", nil, 200))["total"], 508)
	for status, total := range map[string]int{"unused": 506, "used": 1, "expired": 1} {
		want(t, object(admin.ok(t, "GET", "/admin/codes?status="+status, nil, 200))["total"], total)
	}
	want(t, len(array(object(admin.ok(t, "GET", "/admin/codes?limit=20&page=26", nil, 200))["items"])), 8)
	used := object(array(object(admin.ok(t, "GET", "/admin/codes?search=records-owner", nil, 200))["items"])[0])
	want(t, used["code"], codes[0])
	want(t, used["credits"], 25.5)
	want(t, used["status"], "used")
	if used["used_at"] == nil {
		t.Fatal("redemption timestamp missing")
	}
	admin.ok(t, "DELETE", "/admin/codes/"+str(used["id"]), nil, 400)
	expiredRow := object(array(object(admin.ok(t, "GET", "/admin/codes?search="+url.QueryEscape(str(expired[0])), nil, 200))["items"])[0])
	admin.ok(t, "DELETE", "/admin/codes/"+str(expiredRow["id"]), nil, 200)
	admin.ok(t, "GET", "/admin/codes?status=invalid", nil, 400)
	logs := object(admin.ok(t, "GET", "/admin/audit?search=codes.delete", nil, 200))
	want(t, logs["total"], 1)
	if strings.Contains(jsonText(logs), str(expired[0])) {
		t.Fatal("full redeem code should not be copied into the audit log")
	}
	txs := object(admin.ok(t, "GET", "/admin/transactions?kind=redeem&userId="+str(owner["id"]), nil, 200))
	want(t, txs["total"], 1)
	want(t, object(array(txs["items"])[0])["amount"], 25.5)
	want(t, object(admin.ok(t, "GET", "/admin/transactions?kind=redeem&search=records-other", nil, 200))["total"], 0)
	firstTx := object(array(object(admin.ok(t, "GET", "/admin/transactions?limit=1", nil, 200))["items"])[0])
	secondTx := object(array(object(admin.ok(t, "GET", "/admin/transactions?limit=1&page=2", nil, 200))["items"])[0])
	if firstTx["id"] == secondTx["id"] {
		t.Fatal("transaction pages repeat rows")
	}
	admin.ok(t, "GET", "/admin/transactions?kind=invalid", nil, 400)
	admin.ok(t, "GET", "/admin/audit?from=2025-03-02&to=2025-01-01", nil, 400)
}
