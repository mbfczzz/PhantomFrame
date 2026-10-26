package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type storageTransport func(*http.Request) (*http.Response, error)

func (f storageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOSSStoragePipeline(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin, user := &testClient{server: server.URL}, &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	uid := str(object(admin.ok(t, "GET", "/me", nil, 200))["id"])
	user.ok(t, "POST", "/auth/register", Row{"username": "oss-outsider", "password": "OSS-test-password-2026"}, 200)
	apiKey := object(admin.ok(t, "POST", "/keys", Row{"name": "oss test access"}, 201))
	apiClient := &testClient{server: server.URL, key: str(apiKey["token"])}
	for _, route := range []struct{ method, path string }{{"GET", "/admin/storage"}, {"PUT", "/admin/storage"}, {"POST", "/admin/storage/test"}} {
		(&testClient{server: server.URL}).ok(t, route.method, route.path, Row{}, 401)
		user.ok(t, route.method, route.path, Row{}, 403)
		apiClient.ok(t, route.method, route.path, Row{}, 403)
	}
	config := object(admin.ok(t, "GET", "/admin/storage", nil, 200))
	want(t, config["bucket"], "flowlinght")
	want(t, config["prefix"], "phantomframe/uploads/")
	want(t, config["hasCredentials"], false)
	for _, invalid := range []Row{{"enabled": true}, {"endpoint": "http://invalid.example"}, {"prefix": "../bad/"}, {"cdnDomain": "https://cdn.example/x?secret=yes"}, {"urlTTL": 0}, {"accessKeyId": "partial-key"}} {
		admin.ok(t, "PUT", "/admin/storage", invalid, 400)
	}
	var mu sync.Mutex
	files := map[string][]byte{}
	var failUpload atomic.Bool
	requests := []Row{}
	a.OSSHTTP = &http.Client{Transport: storageTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, Row{"method": r.Method, "host": r.URL.Host, "path": r.URL.Path})
		status := 200
		body := []byte{}
		switch r.Method {
		case "PUT":
			want(t, r.URL.Host, "flowlinght.oss-accelerate.aliyuncs.com")
			if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") {
				t.Error("missing OSS V4 authentication")
			}
			if r.Header.Get("x-oss-object-acl") != "" {
				t.Error("must inherit the bucket's ACL")
			}
			if !strings.HasPrefix(r.URL.Path, "/phantomframe/uploads/") {
				t.Error("object escaped the configured prefix")
			}
			if failUpload.Load() {
				status = 403
				body = []byte(`<Error><Code>AccessDenied</Code><Message>private-storage-secret</Message></Error>`)
			} else {
				var err error
				files[r.URL.Path], err = io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
			}
		case "GET":
			body = files[r.URL.Path]
			if body == nil {
				status = 404
			}
		case "DELETE":
			delete(files, r.URL.Path)
			status = 204
		default:
			t.Errorf("unexpected storage operation %s", r.Method)
			status = 405
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	config["enabled"], config["cdnDomain"] = true, "https://cdn.example.test"
	config["accessKeyId"], config["accessKeySecret"] = "local-storage-access-id", "private-storage-secret"
	saved := object(admin.ok(t, "PUT", "/admin/storage", config, 200))
	want(t, saved["hasCredentials"], true)
	sealed := str(a.Store.One("SELECT credentials FROM object_storage WHERE id=1")["credentials"])
	if strings.Contains(sealed, "private-storage-secret") {
		t.Fatal("storage credentials must be encrypted")
	}
	plain, err := a.decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}
	want(t, object(plain)["accessKeySecret"], "private-storage-secret")
	for _, route := range []string{"/config", "/admin/settings", "/admin/storage", "/admin/audit"} {
		body := jsonText(admin.ok(t, "GET", route, nil, 200))
		if strings.Contains(body, "private-storage-secret") || strings.Contains(body, "local-storage-access-id") || strings.Contains(body, sealed) {
			t.Fatalf("storage credential leaked through %s", route)
		}
	}
	admin.ok(t, "POST", "/admin/storage/test", saved, 200)
	want(t, len(files), 0)
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	png, _ := base64.StdEncoding.DecodeString(encoded)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(Row{"data": []Row{{"b64_json": encoded}}})
	}))
	defer upstream.Close()
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "OSS output supplier", "protocol": "openai-image", "base_url": upstream.URL, "apiKey": "supplier-local-test"}, 201))
	admin.ok(t, "POST", "/admin/models", Row{"id": "oss-image", "name": "OSS image", "type": "image", "provider_id": provider["id"], "upstream_model": "image"}, 201)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "oss-image", "label": "request", "unit": "request", "credits": .3}, 200)
	process := func() Row {
		t.Helper()
		job := object(apiClient.ok(t, "POST", "/jobs", Row{"modelId": "oss-image", "prompt": "mock OSS generation"}, 202))
		a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", job["id"])
		a.processJob(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
		return object(apiClient.ok(t, "GET", "/jobs/"+str(job["id"]), nil, 200))
	}
	job := process()
	want(t, job["status"], "succeeded")
	want(t, calls.Load(), 1)
	output := object(array(job["outputs"])[0])
	want(t, output["storage"], "oss")
	address, err := url.Parse(str(output["url"]))
	if err != nil {
		t.Fatal(err)
	}
	want(t, address.Host, "cdn.example.test")
	if address.Query().Get("x-oss-signature") == "" {
		t.Fatal("private URL must be signed")
	}
	if strings.Contains(str(output["url"]), upstream.URL) || strings.Contains(jsonText(job), "private-storage-secret") {
		t.Fatal("upstream URL or secret leaked")
	}
	want(t, len(files), 1)
	want(t, string(files[address.Path]), string(png))
	asset := a.Store.One("SELECT * FROM assets WHERE id=?", output["assetId"])
	want(t, object(asset["remote_storage"])["provider"], "oss")
	items := object(admin.ok(t, "GET", "/assets", nil, 200))
	want(t, object(array(items["items"])[0])["storage"], "oss")
	user.ok(t, "GET", str(output["downloadUrl"])[4:], nil, 404)
	// CDN and link policy changes are reflected in old records without rewriting outputs.
	saved["cdnDomain"], saved["publicRead"] = "https://new-cdn.example.test", true
	admin.ok(t, "PUT", "/admin/storage", saved, 200)
	job = object(admin.ok(t, "GET", "/jobs/"+str(job["id"]), nil, 200))
	output = object(array(job["outputs"])[0])
	address, _ = url.Parse(str(output["url"]))
	want(t, address.Host, "new-cdn.example.test")
	want(t, address.RawQuery, "")
	ref, err := a.saveAsset(uid, "", "reference.png", "image/png", "png", png)
	if err != nil {
		t.Fatal(err)
	}
	links, err := a.hostedReferences(context.Background(), Row{"protocol": "wxart"}, "not-an-oss-key", []Row{a.Store.One("SELECT * FROM assets WHERE id=?", ref["assetId"])})
	if err != nil {
		t.Fatal(err)
	}
	referenceURL, _ := url.Parse(links["image"][0])
	want(t, referenceURL.Host, "flowlinght.oss-accelerate.aliyuncs.com")
	admin.ok(t, "DELETE", "/assets/"+str(ref["assetId"]), nil, 200)
	admin.ok(t, "DELETE", "/assets/"+str(output["assetId"]), nil, 200)
	want(t, len(files), 0)
	failUpload.Store(true)
	balance := object(admin.ok(t, "GET", "/me", nil, 200))["balance"]
	failed := process()
	want(t, failed["status"], "failed")
	want(t, failed["refunded"], true)
	want(t, len(array(failed["outputs"])), 0)
	if !strings.Contains(str(failed["error"]), "OSS") || strings.Contains(str(failed["error"]), "private-storage-secret") {
		t.Fatal("storage failure must be visible without leaking service details")
	}
	want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], balance)
	want(t, a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", failed["id"])["n"], 0)
	want(t, calls.Load(), 2)
	// An unconfigured production destination is rejected before charging or contacting a supplier.
	a.Store.Exec("DELETE FROM object_storage WHERE id=1")
	a.Config.OSS.Enabled = true
	apiClient.ok(t, "POST", "/jobs", Row{"modelId": "oss-image", "prompt": "must not charge"}, 400)
	want(t, calls.Load(), 2)
	want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], balance)
}
