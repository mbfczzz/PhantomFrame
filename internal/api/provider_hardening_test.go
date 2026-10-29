package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderKindValidationAcrossCatalogAndPublishing(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	for _, protocol := range []string{"openai-image", "openai-video", "json-async", "media-upload"} {
		t.Run(protocol, func(t *testing.T) {
			p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "kind fixture", "protocol": protocol, "base_url": "https://fixture.invalid/v1", "apiKey": "kind-fixture-key"}, 201))
			pid := str(p["id"])
			path := "/admin/providers/" + pid
			c.ok(t, "POST", path+"/models", Row{"upstreamModel": "unsupported-text", "type": "text"}, 400)
			entryID := id()
			a.Store.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters) VALUES(?,?,'old-unsupported','legacy item','text','','[]')", entryID, pid)
			c.ok(t, "POST", path+"/models/"+entryID+"/publish", Row{"modelId": "unsupported-" + protocol, "name": "invalid", "type": "text"}, 400)
			want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id=?", pid)["n"], 0)
		})
	}
}

func TestProviderCredentialsValidatedBeforeSave(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "credential fixture", "protocol": "native:apiyi", "base_url": "https://fixture.invalid/v1", "apiKey": "  valid-fixture-key\n"}, 201))
	pid := str(p["id"])
	plain, err := a.decrypt(str(a.Store.One("SELECT api_key FROM providers WHERE id=?", pid)["api_key"]))
	if err != nil {
		t.Fatal(err)
	}
	want(t, plain, "valid-fixture-key")
	before := a.Store.One("SELECT api_key FROM providers WHERE id=?", pid)["api_key"]
	for _, key := range []string{"short", "placeholder", "broken\r\nheader", "key with spaces"} {
		body := clone(p)
		body["apiKey"] = key
		c.ok(t, "PUT", "/admin/providers/"+pid, body, 400)
		want(t, a.Store.One("SELECT api_key FROM providers WHERE id=?", pid)["api_key"], before)
	}
	entry := object(c.ok(t, "POST", "/admin/providers/"+pid+"/models", Row{"upstreamModel": "fixture-text", "type": "text"}, 201))
	c.ok(t, "PUT", "/admin/providers/"+pid+"/models/"+str(entry["id"])+"/key", Row{"apiKey": "short"}, 400)
}

func TestProviderBackfillSkipsReviewedCatalog(t *testing.T) {
	a := setupIntegration(t)
	reviewed := a.Store.One("SELECT * FROM provider_models WHERE provider_id='demo' ORDER BY id LIMIT 1")
	a.Store.Exec("UPDATE provider_models SET name='reviewed name',api_key=? WHERE id=?", a.encrypt("backfill-fixture-key"), reviewed["id"])
	before := jsonText(a.Store.One("SELECT * FROM provider_models WHERE id=?", reviewed["id"]))
	q := &countedQueries{Querier: a.DB}
	s := Store{q}
	s.backfillProviderModels()
	want(t, q.calls.Load(), int64(1))
	missing := a.Store.One("SELECT id FROM provider_models WHERE provider_id='demo' AND id<>? LIMIT 1", reviewed["id"])
	a.Store.Exec("DELETE FROM provider_models WHERE id=?", missing["id"])
	q.calls.Store(0)
	s.backfillProviderModels()
	want(t, q.calls.Load(), int64(2))
	want(t, jsonText(a.Store.One("SELECT * FROM provider_models WHERE id=?", reviewed["id"])), before)
}

func TestProviderSyncSingleFlightAndLockRelease(t *testing.T) {
	a := setupIntegration(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		send(w, 200, Row{"data": []Row{{"id": "single-flight-model", "type": "text"}}})
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	defer unblock()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "sync fixture", "protocol": "native:apiyi", "base_url": mock.URL, "apiKey": "sync-fixture-key"}, 201))
	path := "/admin/providers/" + str(p["id"]) + "/sync-models"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/api"+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", c.cookie)
	done := make(chan error, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if res != nil {
			res.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first sync did not reach mock")
	}
	c.ok(t, "POST", path, Row{}, 409)
	want(t, calls.Load(), int32(1))
	cancel()
	<-done
	unblock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, _ := c.request(t, "POST", path, Row{})
		if status == 200 {
			break
		}
		if status != 409 || time.Now().After(deadline) {
			t.Fatalf("sync lock was not released: %d", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", p["id"])["n"], 1)
}

func TestProviderSyncNormalizesKindsAndKeepsValidDuplicate(t *testing.T) {
	a := setupIntegration(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, Row{"data": []Row{
			{"id": "shared-id", "type": " 3D "}, {"id": "shared-id", "type": " TEXT "}, {"id": "unknown-kind"},
		}})
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "kind normalization", "protocol": "native:apiyi", "base_url": mock.URL, "apiKey": "kind-normalization-fixture"}, 201))
	path := "/admin/providers/" + str(p["id"])
	want(t, object(c.ok(t, "POST", path+"/sync-models", Row{}, 200))["added"], 2)
	entry := a.Store.One("SELECT * FROM provider_models WHERE provider_id=? AND upstream_model='shared-id'", p["id"])
	want(t, entry["type"], "text")
	if len(array(entry["parameters"])) == 0 {
		t.Fatal("normalized type lost its parameter schema")
	}
	want(t, a.Store.One("SELECT type FROM provider_models WHERE provider_id=? AND upstream_model='unknown-kind'", p["id"])["type"], "")
}

func TestProviderSyncDifferentAccountsStayParallel(t *testing.T) {
	a := setupIntegration(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		send(w, 200, Row{"data": []string{"parallel-model"}})
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	defer unblock()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "parallel account", "protocol": "native:apiyi", "base_url": mock.URL, "apiKey": "parallel-fixture-key"}, 201))
		req, err := http.NewRequest("POST", server.URL+"/api/admin/providers/"+str(p["id"])+"/sync-models", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", c.cookie)
		go func() {
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- 0
				return
			}
			res.Body.Close()
			results <- res.StatusCode
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("one supplier blocked another's directory request")
		}
	}
	unblock()
	for i := 0; i < 2; i++ {
		want(t, <-results, 200)
	}
}

func TestCatalogTransactionCancellationRollsBack(t *testing.T) {
	a := setupIntegration(t)
	blocker, err := a.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.Exec("UPDATE providers SET name=name WHERE id='demo'"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- try(func() {
			a.TxContext(ctx, func(s Store) {
				s.Exec("INSERT INTO providers(id,name,protocol,api_key,config) VALUES('cancelled-catalog-fixture','temporary','demo','','{}')")
				s.Exec("UPDATE providers SET name='should not commit' WHERE id='demo'")
			})
		})
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled catalog transaction committed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("catalog transaction ignored cancellation while waiting for a row lock")
	}
	blocker.Rollback()
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id='cancelled-catalog-fixture'")["n"], 0)
	if a.Store.One("SELECT name FROM providers WHERE id='demo'")["name"] == "should not commit" {
		t.Fatal("cancelled transaction changed existing data")
	}
}

func TestProviderSyncBodyTimeoutReleasesLock(t *testing.T) {
	a := setupIntegration(t)
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[`))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		send(w, 200, Row{"data": []string{"retry-model"}})
	}))
	defer mock.Close()
	handler := a.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync-models") {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "body timeout", "protocol": "native:apiyi", "base_url": mock.URL, "apiKey": "body-timeout-fixture"}, 201))
	path := "/admin/providers/" + str(p["id"]) + "/sync-models"
	c.ok(t, "POST", path, Row{}, 504)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", p["id"])["n"], 0)
	want(t, object(c.ok(t, "POST", path, Row{}, 200))["added"], 1)
	want(t, calls.Load(), int32(2))
}
