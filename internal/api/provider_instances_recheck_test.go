package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func instanceUploadFixture(t *testing.T, a *App) (*httptest.Server, Row, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" || r.Header.Get("Authorization") != "Bearer uploader-b-fixture-key" {
			http.Error(w, "wrong isolated uploader", 400)
			return
		}
		send(w, 200, Row{"url": "https://assets.invalid/reference.png"})
	}))
	t.Cleanup(server.Close)
	for _, name := range []string{"a", "b"} {
		a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,config,created_at) VALUES(?,?,'media-upload',?,?,'{}',?)", "uploader-"+name, "uploader "+name, server.URL, a.encrypt("uploader-"+name+"-fixture-key"), "2026-10-"+map[string]string{"a": "01", "b": "02"}[name]+" 00:00:00")
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	uid := str(a.Store.One("SELECT id FROM users WHERE role='admin'")["id"])
	ref, err := a.saveAsset(uid, "", "reference.png", "image/png", "png", png)
	if err != nil {
		t.Fatal(err)
	}
	return server, a.Store.One("SELECT * FROM assets WHERE id=?", ref["assetId"]), uid
}

func TestInstanceRecheckNativeOperationalConfig(t *testing.T) {
	a := setupIntegration(t)
	server, ref, _ := instanceUploadFixture(t, a)
	p := Row{"id": "native-fixture", "protocol": "native:copp", "base_url": server.URL, "api_key": a.encrypt("native-generation-fixture-key"), "config": Row{"uploadProviderId": "uploader-b", "pollInterval": 25}}
	m := Row{"type": "video", "upstream_model": "seedance-2"}
	normalized := a.nativeProvider(a.Store, m, p)
	want(t, object(normalized["config"])["uploadProviderId"], "uploader-b")
	want(t, object(normalized["config"])["pollInterval"], 25)
	refs, err := a.hostedReferences(context.Background(), normalized, "native-generation-fixture-key", []Row{ref})
	if err != nil {
		t.Fatal(err)
	}
	want(t, refs["image"][0], "https://assets.invalid/reference.png")
}

func TestInstanceRecheckMigrationPreservesPendingUploader(t *testing.T) {
	a := setupIntegration(t)
	server, ref, _ := instanceUploadFixture(t, a)
	mid := seedInventoryTestRoute(a, "copp", "seedance-v2", "video", server.URL, "generation-fixture-key")
	route := a.Store.One("SELECT r.* FROM inventory_routes r JOIN models m ON m.inventory_route_id=r.id WHERE m.id=?", mid)
	config := object(route["config"])
	config["uploadProviderId"] = "uploader-b"
	a.Store.Exec("UPDATE inventory_routes SET config=? WHERE id=?", jsonText(config), route["id"])
	apiServer := httptest.NewServer(a.Handler())
	defer apiServer.Close()
	c := &testClient{server: apiServer.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	j := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "pending upload fixture", "params": Row{"imageUrls": []any{ref["id"]}}}, 202))
	// Simulate a job created by the old version, which had no uploader snapshot.
	snapshot := object(a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", j["id"])["provider_snapshot"])
	delete(snapshot, "upload_provider_snapshot")
	a.Store.Exec("UPDATE jobs SET provider_snapshot=? WHERE id=?", jsonText(snapshot), j["id"])
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers")["n"], 0)
	snapshot = object(a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", j["id"])["provider_snapshot"])
	refs, err := a.hostedReferences(context.Background(), snapshot, "generation-fixture-key", []Row{ref})
	if err != nil {
		t.Fatal(err)
	}
	want(t, refs["image"][0], "https://assets.invalid/reference.png")
	if strings.Contains(jsonText(c.ok(t, "GET", "/jobs/"+str(j["id"]), nil, 200)), "uploader-b-fixture-key") {
		t.Fatal("job view exposed upload key")
	}
}

func TestInstanceRecheckProviderConfigNormalization(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "normalized fixture", "protocol": "native:apiyi", "base_url": "https://fixture.invalid/v1", "apiKey": "fixture-provider-key", "config": Row{"chatAPI": " responses ", "sourceModel": " gpt-5.4-mini ", "pollInterval": 25}}, 201))
	want(t, object(p["config"])["chatAPI"], "responses")
	want(t, object(p["config"])["sourceModel"], "gpt-5.4-mini")
	want(t, object(p["config"])["pollInterval"], 25)
}

func TestInstanceRecheckProviderBindingReview(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "review fixture", "protocol": "native:apiyi", "base_url": "https://fixture.invalid/v1", "apiKey": "fixture-provider-key"}, 201))
	mid := id()
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,tags,parameters) VALUES(?,'reviewed model','text',?,'gpt-5.4-mini','','[]','[]')", mid, p["id"])
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,'reviewed price','request',25,'{}')", id(), mid)
	prices := jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid))
	for _, tc := range []struct {
		name    string
		change  Row
		enabled int
	}{
		{"rename", Row{"name": "renamed fixture"}, 1},
		{"key rotation", Row{"apiKey": "rotated-fixture-key"}, 1},
		{"directory only", Row{"config": Row{"modelsPath": "/custom/models"}}, 1},
		{"normalized address", Row{"base_url": "https://fixture.invalid/v1/"}, 1},
		{"new address", Row{"base_url": "https://other.invalid/v1"}, 0},
		{"new generation endpoint", Row{"config": Row{"submit_path_override": "/custom/responses"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.Store.Exec("UPDATE models SET enabled=1 WHERE id=?", mid)
			body := clone(p)
			for k, v := range tc.change {
				body[k] = v
			}
			p = object(c.ok(t, "PUT", "/admin/providers/"+str(p["id"]), body, 200))
			want(t, a.Store.One("SELECT enabled FROM models WHERE id=?", mid)["enabled"], tc.enabled)
			want(t, jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid)), prices)
		})
	}
}

func TestInstanceRecheckUploaderSnapshotSurvivesRotation(t *testing.T) {
	a := setupIntegration(t)
	server, ref, _ := instanceUploadFixture(t, a)
	mid := seedInventoryTestRoute(a, "copp", "seedance-v2", "video", server.URL, "generation-fixture-key")
	pid := inventoryProviderID("copp")
	a.Store.Exec("UPDATE providers SET protocol='native:copp',api_key=?,config=? WHERE id=?", a.encrypt("generation-fixture-key"), jsonText(Row{"uploadProviderId": "uploader-b"}), pid)
	a.Store.Exec("UPDATE models SET inventory_route_id=NULL WHERE id=?", mid)
	apiServer := httptest.NewServer(a.Handler())
	defer apiServer.Close()
	c := &testClient{server: apiServer.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	j := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "uploader snapshot fixture", "params": Row{"imageUrls": []any{ref["id"]}}}, 202))
	snapshot := object(a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", j["id"])["provider_snapshot"])
	want(t, object(snapshot["upload_provider_snapshot"])["id"], "uploader-b")
	a.Store.Exec("UPDATE providers SET api_key=?,enabled=0 WHERE id='uploader-b'", a.encrypt("rotated-uploader-fixture-key"))
	refs, err := a.hostedReferences(context.Background(), snapshot, "generation-fixture-key", []Row{ref})
	if err != nil {
		t.Fatal(err)
	}
	want(t, refs["image"][0], "https://assets.invalid/reference.png")
	missing := snapshotUploadProvider(a.Store, Row{"protocol": "inventory", "config": Row{"uploadProviderId": "missing-uploader"}})
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,config) VALUES('missing-uploader','created later','media-upload',?,?,'{}')", server.URL, a.encrypt("uploader-b-fixture-key"))
	if _, err := a.hostedReferences(context.Background(), missing, "generation-fixture-key", []Row{ref}); err == nil {
		t.Fatal("task borrowed an uploader that did not exist at submission")
	}
}

func TestInstanceRecheckMigrationRollbackAndConcurrentRestart(t *testing.T) {
	a := setupIntegration(t)
	before := jsonText(a.Store.All("SELECT * FROM models ORDER BY id"))
	beforePrices := jsonText(a.Store.All("SELECT * FROM prices ORDER BY id"))
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	a.Store.Exec("CREATE TABLE instance_reset_blocker(provider_id VARCHAR(100) PRIMARY KEY, FOREIGN KEY(provider_id) REFERENCES providers(id)) ENGINE=InnoDB")
	a.Store.Exec("INSERT INTO instance_reset_blocker(provider_id) VALUES('demo')")
	if err := a.migrateProviderInstances(); err == nil {
		t.Fatal("forced reset failure was ignored")
	}
	want(t, jsonText(a.Store.All("SELECT * FROM models ORDER BY id")), before)
	want(t, jsonText(a.Store.All("SELECT * FROM prices ORDER BY id")), beforePrices)
	want(t, a.Store.One("SELECT count(*) n FROM migrations WHERE name=?", providerInstancesMigration)["n"], 0)
	a.Store.Exec("DROP TABLE instance_reset_blocker")
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- a.migrateProviderInstances() }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	want(t, a.Store.One("SELECT count(*) n FROM migrations WHERE name=?", providerInstancesMigration)["n"], 1)
	want(t, a.Store.One("SELECT count(*) n FROM models")["n"], 0)
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,config) VALUES('after-reset','new account','native:apiyi','https://fixture.invalid','','{}')")
	if err := a.migrateProviderInstances(); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE id='after-reset'")["n"], 1)
}
