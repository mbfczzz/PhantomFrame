package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joho/godotenv"
	assets "phantomframe/server"
)

type testClient struct {
	cookie, key string
	server      string
}

func (c *testClient) request(t *testing.T, method, path string, body any, headers ...map[string]string) (int, any) {
	t.Helper()
	var b io.Reader
	if body != nil {
		b = strings.NewReader(jsonText(body))
	}
	r, e := http.NewRequest(method, c.server+"/api"+path, b)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	if method == "POST" && (strings.HasSuffix(path, "/transfer") || strings.HasSuffix(path, "/credits")) {
		r.Header.Set("Idempotency-Key", id())
	}
	if c.cookie != "" {
		r.Header.Set("Cookie", c.cookie)
	}
	if c.key != "" {
		r.Header.Set("Authorization", "Bearer "+c.key)
	}
	for _, h := range headers {
		for k, v := range h {
			r.Header.Set(k, v)
		}
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	for _, cookie := range res.Cookies() {
		if cookie.Name == "pf_session" {
			c.cookie = cookie.Name + "=" + cookie.Value
		}
	}
	var v any
	if e = json.NewDecoder(res.Body).Decode(&v); e != nil {
		t.Fatalf("%s %s: invalid JSON status=%d: %v", method, path, res.StatusCode, e)
	}
	return res.StatusCode, v
}
func want(t *testing.T, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}
func (c *testClient) ok(t *testing.T, method, path string, body any, status int) any {
	t.Helper()
	code, v := c.request(t, method, path, body)
	if code != status {
		t.Fatalf("%s %s status=%d want=%d data=%v", method, path, code, status, v)
	}
	return v
}
func setupIntegration(t testing.TB) *App {
	t.Helper()
	if os.Getenv("PF_INTEGRATION") != "1" {
		t.Skip("set PF_INTEGRATION=1 to run against isolated MySQL and Redis data")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	_ = godotenv.Load(filepath.Join(root, ".env"))
	c, e := LoadConfig()
	if e != nil {
		t.Fatal(e)
	}
	c.Production = false
	c.OSS = defaultOSSConfig(false)
	c.OSS.Enabled = false
	c.OSS.AccessKey, c.OSS.SecretKey = "", ""
	c.Demo = true
	// General API fixtures isolate the built-in CSV templates. Dedicated startup
	// tests exercise ProviderPresets=true with the full production seed path.
	c.ProviderPresets = false
	c.SupplierInstances = false
	c.CookieSecure = false
	c.DataDir = t.TempDir()
	c.FilesDir = filepath.Join(c.DataDir, "files")
	_ = os.MkdirAll(c.FilesDir, 0700)
	c.Key = bytes.Repeat([]byte{0x61}, 32)
	c.RedisPrefix = "phantomframe:test:" + randomHex(8) + ":"
	c.MySQLDatabase = ""
	admin, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	name := "phantomframe_test_" + randomHex(8)
	admin.Store.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin")
	c.MySQLDatabase = name
	a, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		a.Close()
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, e := admin.Redis.Scan(ctx, cursor, c.RedisPrefix+"*", 100).Result()
			if e != nil {
				break
			}
			if len(keys) > 0 {
				_ = admin.Redis.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		admin.Store.Exec("DROP DATABASE `" + name + "`")
		admin.Close()
	})
	if e = a.Migrate(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("ADMIN_USERNAME", "test-admin")
	t.Setenv("ADMIN_PASSWORD", "Test-admin-password-2026")
	a.Seed()
	return a
}
func TestIntegration(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	var uid, otherID string
	var stop context.CancelFunc
	var done <-chan struct{}
	var standby <-chan struct{}
	defer func() {
		if stop != nil {
			stop()
			<-done
			if standby != nil {
				<-standby
			}
		}
	}()
	t.Run("cookie authentication stores sessions in Redis", func(t *testing.T) {
		admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "wrong"}, 401)
		admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
		uid = str(object(user.ok(t, "POST", "/auth/register", Row{"username": "creator-one", "password": "Creator-pass-2026"}, 200))["id"])
		otherID = str(object(other.ok(t, "POST", "/auth/register", Row{"username": "creator-two", "password": "Creator-pass-2026"}, 200))["id"])
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
		anonymous := &testClient{server: server.URL}
		anonymous.ok(t, "GET", "/models", nil, 401)
		tokenValue := strings.TrimPrefix(user.cookie, "pf_session=")
		if a.Redis.Exists(context.Background(), a.Config.RedisPrefix+"session:"+hash(tokenValue)).Val() != 1 {
			t.Fatal("session missing from Redis")
		}
	})
	t.Run("catalog and exact parameter pricing", func(t *testing.T) {
		models := array(user.ok(t, "GET", "/models", nil, 200))
		want(t, len(models), 22)
		q := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": "sd2.0-933-op", "params": Row{"resolution": "720p"}}, 200))
		want(t, q["total"], 38)
		q = object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": "sd2.0-933-qd", "params": Row{"resolution": "720p", "duration": 15}}, 200))
		want(t, q["total"], 67.5)
		user.ok(t, "POST", "/pricing/quote", Row{"modelId": "nanobanana", "params": Row{"cost": 0}}, 400)
		user.ok(t, "POST", "/pricing/quote", Row{"modelId": "sd2.0-933-op", "params": Row{"duration": -1}}, 400)
	})
	t.Run("authorization and cross-origin writes", func(t *testing.T) {
		user.ok(t, "GET", "/admin/models", nil, 403)
		code, _ := user.request(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "x"}, map[string]string{"Origin": "https://foreign.invalid"})
		want(t, code, 403)
	})
	var jobID string
	t.Run("concurrent duplicate submissions charge once in MySQL", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make(chan string, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := &testClient{server: server.URL, cookie: user.cookie}
				code, v := c.request(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "idempotency", "params": Row{}}, map[string]string{"Idempotency-Key": "same-job"})
				if code != 202 {
					t.Errorf("submit status=%d %v", code, v)
				}
				ids <- str(object(v)["id"])
			}()
		}
		wg.Wait()
		close(ids)
		for v := range ids {
			if jobID == "" {
				jobID = v
			}
			want(t, v, jobID)
		}
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 99)
		want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=?", jobID)["n"], 1)
	})
	t.Run("owner-only cancellation and exactly-once refund", func(t *testing.T) {
		other.ok(t, "GET", "/jobs/"+jobID, nil, 404)
		other.ok(t, "POST", "/jobs/"+jobID+"/cancel", nil, 404)
		user.ok(t, "POST", "/jobs/"+jobID+"/cancel", nil, 200)
		user.ok(t, "POST", "/jobs/"+jobID+"/cancel", nil, 400)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
		want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='refund'", jobID)["n"], 1)
	})
	t.Run("concurrent spending cannot overdraw", func(t *testing.T) {
		var wg sync.WaitGroup
		codes := make(chan int, 3)
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := &testClient{server: server.URL, cookie: user.cookie}
				code, _ := c.request(t, "POST", "/jobs", Row{"modelId": "sd2.0-933-qd", "prompt": "overspend", "params": Row{"resolution": "720p", "duration": 15}})
				codes <- code
			}()
		}
		wg.Wait()
		close(codes)
		success := 0
		for code := range codes {
			if code == 202 {
				success++
			} else {
				want(t, code, 402)
			}
		}
		want(t, success, 1)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 32.5)
		for _, j := range a.Store.All("SELECT id FROM jobs WHERE user_id=? AND status='queued'", uid) {
			user.ok(t, "POST", "/jobs/"+str(j["id"])+"/cancel", nil, 200)
		}
	})
	t.Run("stale quotes fail without a debit", func(t *testing.T) {
		user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "stale", "expectedCost": 500}, 409)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
	})
	t.Run("API keys are hashed and cannot manage sessions or admin", func(t *testing.T) {
		v := object(user.ok(t, "POST", "/keys", Row{"name": "test key"}, 201))
		key := str(v["token"])
		c := &testClient{server: server.URL, key: key}
		c.ok(t, "GET", "/models", nil, 200)
		c.ok(t, "GET", "/keys", nil, 403)
		c.ok(t, "GET", "/admin/models", nil, 403)
		anonymous := &testClient{server: server.URL}
		code, _ := anonymous.request(t, "GET", "/models", nil, map[string]string{"X-API-Key": key})
		want(t, code, 200)
		code, _ = user.request(t, "GET", "/models", nil, map[string]string{"Authorization": "Bearer invalid-key"})
		want(t, code, 401)
		code, _ = user.request(t, "GET", "/models", nil, map[string]string{"X-API-Key": "invalid-key"})
		want(t, code, 401)
		want(t, a.Store.One("SELECT token FROM api_keys WHERE id=?", v["id"])["token"], hash(key))
		user.ok(t, "DELETE", "/keys/"+str(v["id"]), nil, 200)
		c.ok(t, "GET", "/models", nil, 401)
	})
	t.Run("redeem codes credit once under concurrent redemption", func(t *testing.T) {
		codes := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 25}, 201))
		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := &testClient{server: server.URL, cookie: user.cookie}
				code, _ := c.request(t, "POST", "/redeem", Row{"code": codes[0]})
				statuses <- code
			}()
		}
		wg.Wait()
		close(statuses)
		success := 0
		for code := range statuses {
			if code == 200 {
				success++
			} else {
				want(t, code, 400)
			}
		}
		want(t, success, 1)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 125)
	})
	t.Run("subaccounts transfer transactionally", func(t *testing.T) {
		u := object(user.ok(t, "POST", "/sub-users", Row{"username": "sub-user", "password": "Sub-password-2026"}, 201))
		user.ok(t, "POST", "/sub-users/"+str(u["id"])+"/transfer", Row{"credits": 10}, 200)
		want(t, a.Store.One("SELECT balance FROM users WHERE id=?", u["id"])["balance"], 1000)
		other.ok(t, "POST", "/sub-users/"+str(u["id"])+"/transfer", Row{"credits": 1}, 404)
	})
	t.Run("canvas persistence and optimistic conflicts", func(t *testing.T) {
		p := object(user.ok(t, "POST", "/projects", Row{"title": "test canvas"}, 201))
		pid := str(p["id"])
		user.ok(t, "PATCH", "/projects/"+pid, Row{"version": 1, "data": Row{"nodes": []any{Row{"id": "n", "position": Row{"x": 0, "y": 0}, "data": Row{"text": "hello"}}}, "edges": []any{}}}, 200)
		user.ok(t, "PATCH", "/projects/"+pid, Row{"version": 1, "title": "stale"}, 409)
		other.ok(t, "GET", "/projects/"+pid, nil, 404)
		want(t, object(user.ok(t, "GET", "/projects/"+pid, nil, 200))["nodeCount"], 1)
	})
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	var referenceID string
	t.Run("binary uploads preserve ownership and reject disguised HTML", func(t *testing.T) {
		for _, valid := range []bool{true, false} {
			buf := new(bytes.Buffer)
			form := multipart.NewWriter(buf)
			f, _ := form.CreateFormFile("file", "reference.png")
			b, _ := base64.StdEncoding.DecodeString(png)
			if !valid {
				b = []byte("<script>alert(1)</script>")
			}
			_, _ = f.Write(b)
			form.Close()
			req, _ := http.NewRequest("POST", server.URL+"/api/assets", buf)
			req.Header.Set("Cookie", user.cookie)
			req.Header.Set("Content-Type", form.FormDataContentType())
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			var v Row
			_ = json.NewDecoder(res.Body).Decode(&v)
			res.Body.Close()
			if valid {
				want(t, res.StatusCode, 201)
				referenceID = str(v["id"])
				other.ok(t, "GET", "/assets/"+referenceID+"/file", nil, 404)
			} else {
				want(t, res.StatusCode, 400)
			}
		}
		user.ok(t, "POST", "/pricing/quote", Row{"modelId": "nanobanana", "params": Row{"imageUrls": []string{referenceID}}}, 200)
	})
	t.Run("provider keys are encrypted and compatible with old AES format", func(t *testing.T) {
		v := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Encrypted", "protocol": "openai-image", "base_url": "https://example.com/v1", "apiKey": "secret-test-key"}, 201))
		want(t, v["hasKey"], true)
		stored := str(a.Store.One("SELECT api_key FROM providers WHERE id=?", v["id"])["api_key"])
		if strings.Contains(stored, "secret-test-key") || len(strings.Split(stored, ".")) != 3 {
			t.Fatal("secret not encrypted")
		}
		clear, e := a.decrypt(stored)
		if e != nil || clear != "secret-test-key" {
			t.Fatal("secret did not roundtrip")
		}
		if strings.Contains(jsonText(v), "secret-test-key") {
			t.Fatal("secret leaked")
		}
	})
	waitJob := func(t *testing.T, id string) Row {
		t.Helper()
		for i := 0; i < 100; i++ {
			j := object(user.ok(t, "GET", "/jobs/"+id, nil, 200))
			if j["status"] != "queued" && j["status"] != "running" && j["status"] != "refund_pending" {
				return j
			}
			time.Sleep(80 * time.Millisecond)
		}
		t.Fatal("job timeout")
		return nil
	}
	t.Run("two worker instances elect one dispatcher and process demo media", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		stop = cancel
		done = a.StartWorker(ctx)
		standby = a.StartWorker(ctx)
		for _, mid := range []string{"nanobanana", "sd2.0-933-op"} {
			j := object(user.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "demo", "params": Row{}}, 202))
			finished := waitJob(t, str(j["id"]))
			want(t, finished["status"], "succeeded")
			want(t, finished["demo"], true)
			out := object(array(finished["outputs"])[0])
			req, _ := http.NewRequest("GET", server.URL+str(out["url"]), nil)
			req.Header.Set("Cookie", user.cookie)
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			want(t, res.StatusCode, 200)
			if mid == "nanobanana" && !bytes.Contains(b, []byte("非 AI 模型生成")) {
				t.Fatal("demo disclosure missing")
			}
			want(t, a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", j["id"])["n"], 1)
		}
	})
	t.Run("all provider protocols submit and poll through Go", func(t *testing.T) {
		var calls atomic.Int32
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Header.Get("Authorization") != "Bearer mock-secret" {
				t.Error("missing upstream auth")
			}
			switch r.URL.Path {
			case "/v1/images/generations":
				var body Row
				_ = json.NewDecoder(r.Body).Decode(&body)
				want(t, body["model"], "upstream-model")
				send(w, 200, Row{"data": []Row{{"b64_json": png}}})
			case "/v1/images/edits":
				_ = r.ParseMultipartForm(1 << 20)
				defer r.MultipartForm.RemoveAll()
				want(t, r.FormValue("model"), "upstream-model")
				if len(r.MultipartForm.File["image[]"]) != 1 {
					t.Error("image edit reference missing")
				}
				send(w, 200, Row{"data": []Row{{"b64_json": png}}})
			case "/v1/generate/image":
				send(w, 200, Row{"data": Row{"task_id": "task", "state": "queued"}})
			case "/v1/tasks/task":
				send(w, 200, Row{"data": Row{"state": "completed", "results": []Row{{"b64_json": png}}}})
			case "/v1/videos":
				_ = r.ParseMultipartForm(1 << 20)
				defer r.MultipartForm.RemoveAll()
				want(t, r.FormValue("model"), "upstream-model")
				send(w, 200, Row{"id": "video-task", "status": "queued"})
			case "/v1/videos/video-task":
				send(w, 200, Row{"status": "completed"})
			case "/v1/videos/video-task/content":
				w.Header().Set("Content-Type", "video/webm")
				_, _ = w.Write(assets.DemoVideo)
			default:
				t.Errorf("unexpected upstream route %s", r.URL.Path)
				w.WriteHeader(404)
			}
		}))
		defer up.Close()
		for _, protocol := range []string{"openai-image", "json-async", "openai-video"} {
			config := Row{"pollInterval": 20}
			kind := "image"
			if protocol == "json-async" {
				config["idPath"] = "data.task_id"
				config["statusField"] = "data.state"
				config["outputPath"] = "data.results"
			}
			if protocol == "openai-video" {
				kind = "video"
			}
			p := object(admin.ok(t, "POST", "/admin/providers", Row{"name": protocol, "protocol": protocol, "base_url": up.URL + "/v1", "apiKey": "mock-secret", "config": config}, 201))
			mid := "adapter-" + protocol
			model := Row{"id": mid, "name": mid, "type": kind, "provider_id": p["id"], "upstream_model": "upstream-model", "parameters": []Row{{"name": "images", "label": "Images", "type": "assets"}}}
			admin.ok(t, "POST", "/admin/models", model, 201)
			admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "flat", "unit": "request", "credits": 1}, 200)
			params := Row{}
			if protocol == "openai-image" {
				params["images"] = []string{referenceID}
			}
			j := object(user.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "provider test", "params": params}, 202))
			finished := waitJob(t, str(j["id"]))
			want(t, finished["status"], "succeeded")
			want(t, finished["demo"], false)
			model["name"] = "Renamed later"
			admin.ok(t, "PUT", "/admin/models/"+mid, model, 200)
			want(t, object(user.ok(t, "GET", "/jobs/"+str(j["id"]), nil, 200))["modelName"], mid)
		}
		if calls.Load() < 6 {
			t.Fatal("upstream adapters were not called")
		}
	})
	t.Run("provider failure refunds once", func(t *testing.T) {
		p := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "missing key", "protocol": "openai-image", "base_url": "https://example.com/v1"}, 201))
		admin.ok(t, "POST", "/admin/models", Row{"id": "failure", "name": "Failure", "type": "image", "provider_id": p["id"], "upstream_model": "image"}, 201)
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": "failure", "label": "flat", "unit": "request", "credits": 3}, 200)
		before := object(user.ok(t, "GET", "/me", nil, 200))["balance"]
		j := object(user.ok(t, "POST", "/jobs", Row{"modelId": "failure", "prompt": "fail"}, 202))
		finished := waitJob(t, str(j["id"]))
		want(t, finished["status"], "failed")
		want(t, finished["refunded"], true)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], before)
		a.refund(str(j["id"]), "retry")
		want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='refund'", j["id"])["n"], 1)
	})
	t.Run("price conflicts rejected and zero cost jobs supported", func(t *testing.T) {
		admin.ok(t, "POST", "/admin/models", Row{"id": "free", "name": "Free", "type": "image", "provider_id": "demo", "upstream_model": "free", "parameters": []Row{{"name": "quality", "label": "Quality", "type": "select", "options": []string{"hd", "sd"}, "default": "hd"}}}, 201)
		p := object(admin.ok(t, "POST", "/admin/prices", Row{"model_id": "free", "label": "free", "unit": "request", "credits": 0}, 200))
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": "free", "label": "duplicate", "unit": "request", "credits": 1}, 400)
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": "free", "label": "mixed", "unit": "second", "credits": 1, "conditions": Row{"quality": "hd"}}, 400)
		j := object(user.ok(t, "POST", "/jobs", Row{"modelId": "free", "prompt": "free"}, 202))
		want(t, j["cost"], 0)
		want(t, waitJob(t, str(j["id"]))["status"], "succeeded")
		admin.ok(t, "DELETE", "/admin/prices/"+str(p["id"]), nil, 200)
	})
	t.Run("restart recovers queued jobs and refunds interrupted jobs without resubmission", func(t *testing.T) {
		stop()
		<-done
		<-standby
		standby = nil
		stop = nil
		before := object(user.ok(t, "GET", "/me", nil, 200))["balance"]
		interrupted := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "interrupted"}, 202))
		a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", interrupted["id"])
		queued := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "queued"}, 202))
		ctx, cancel := context.WithCancel(context.Background())
		stop = cancel
		done = a.StartWorker(ctx)
		want(t, waitJob(t, str(interrupted["id"]))["status"], "failed")
		want(t, waitJob(t, str(queued["id"]))["status"], "succeeded")
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], num(before)-1)
		want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE job_id=? AND kind='refund'", interrupted["id"])["n"], 1)
		second := httptest.NewServer(a.Handler())
		defer second.Close()
		client := &testClient{server: second.URL, cookie: user.cookie}
		client.ok(t, "GET", "/me", nil, 200)
	})
	t.Run("API key generates image and video with isolated usage, pagination and private downloads", func(t *testing.T) {
		createdKey := object(user.ok(t, "POST", "/keys", Row{"name": "external application"}, 201))
		client := &testClient{server: server.URL, key: str(createdKey["token"])}
		before := object(user.ok(t, "GET", "/me", nil, 200))["balance"]
		want(t, object(client.ok(t, "GET", "/usage?source=api", nil, 200))["total"], 0)
		for _, modelID := range []string{"nanobanana", "sd2.0-933-op"} {
			body := Row{"modelId": modelID, "prompt": "API integration " + modelID, "params": Row{}}
			quoted := object(client.ok(t, "POST", "/pricing/quote", Row{"modelId": modelID, "params": Row{}}, 200))
			body["expectedCost"] = quoted["total"]
			status, raw := client.request(t, "POST", "/jobs", body, map[string]string{"Idempotency-Key": "api-" + modelID})
			want(t, status, 202)
			job := object(raw)
			_, replay := client.request(t, "POST", "/jobs", body, map[string]string{"Idempotency-Key": "api-" + modelID})
			want(t, object(replay)["id"], job["id"])
			finished := waitJob(t, str(job["id"]))
			want(t, finished["status"], "succeeded")
			want(t, finished["source"], "api")
			client.ok(t, "GET", "/jobs/"+str(job["id"]), nil, 200)
			other.ok(t, "GET", "/jobs/"+str(job["id"]), nil, 404)
			output := object(array(finished["outputs"])[0])
			request, _ := http.NewRequest("GET", server.URL+str(output["url"]), nil)
			request.Header.Set("X-API-Key", client.key)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			want(t, response.StatusCode, 200)
			if len(data) == 0 {
				t.Fatal("empty API download")
			}
			anonymous := &testClient{server: server.URL}
			anonymous.ok(t, "GET", strings.TrimPrefix(str(output["url"]), "/api"), nil, 401)
		}
		usage := object(client.ok(t, "GET", "/usage?source=api", nil, 200))
		want(t, usage["total"], 2)
		want(t, usage["succeeded"], 2)
		want(t, usage["spent"], 39)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], num(before)-39)
		daily := array(usage["daily"])
		if len(daily) == 0 {
			t.Fatal("API daily usage missing")
		}
		dailyTotal := float64(0)
		for _, entry := range daily {
			day := object(entry)
			if _, err := time.Parse("2006-01-02", str(day["date"])); err != nil {
				t.Fatalf("DATE must serialize as YYYY-MM-DD: %v", err)
			}
			dailyTotal += num(day["credits"])
		}
		want(t, dailyTotal, 39)
		first := object(client.ok(t, "GET", "/jobs?source=api&limit=1", nil, 200))
		want(t, first["total"], 2)
		want(t, len(array(first["items"])), 1)
		second := object(client.ok(t, "GET", "/jobs?source=api&limit=1&page=2", nil, 200))
		want(t, len(array(second["items"])), 1)
		if object(array(first["items"])[0])["id"] == object(array(second["items"])[0])["id"] {
			t.Fatal("pagination returned the same job")
		}
		for _, item := range array(object(client.ok(t, "GET", "/jobs?source=web", nil, 200))["items"]) {
			want(t, object(item)["source"], "web")
		}
		allUsage := object(client.ok(t, "GET", "/usage", nil, 200))
		webUsage := object(client.ok(t, "GET", "/usage?source=web", nil, 200))
		want(t, num(allUsage["total"]), num(webUsage["total"])+2)
		client.ok(t, "GET", "/usage?source=invalid", nil, 400)
		client.ok(t, "GET", "/jobs?source=invalid", nil, 400)
		keys := array(user.ok(t, "GET", "/keys", nil, 200))
		if strings.Contains(jsonText(keys), client.key) {
			t.Fatal("API secret leaked in key listing")
		}
		user.ok(t, "DELETE", "/keys/"+str(createdKey["id"]), nil, 200)
		client.ok(t, "GET", "/models", nil, 401)
	})
	t.Run("disabled users and password changes revoke sessions", func(t *testing.T) {
		admin.ok(t, "PATCH", "/admin/users/"+otherID, Row{"status": "disabled"}, 200)
		other.ok(t, "GET", "/me", nil, 401)
		old := &testClient{server: server.URL, cookie: user.cookie}
		user.ok(t, "POST", "/me/password", Row{"currentPassword": "Creator-pass-2026", "password": "Changed-pass-2026"}, 200)
		old.ok(t, "GET", "/me", nil, 401)
		user.ok(t, "GET", "/me", nil, 200)
	})
	t.Run("audit and readiness expose no credentials", func(t *testing.T) {
		v := admin.ok(t, "GET", "/admin/audit?limit=100", nil, 200)
		if !strings.Contains(jsonText(v), "price.save") || strings.Contains(jsonText(v), "mock-secret") {
			t.Fatal("invalid audit data")
		}
		want(t, object(user.ok(t, "GET", "/health", nil, 200))["backend"], "go")
	})
}
