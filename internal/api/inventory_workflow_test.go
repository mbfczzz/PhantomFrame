package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	assets "phantomframe/server"
)

func seedInventoryTestRoute(a *App, vendor, model, kind, base, key string) string {
	rid := id()
	pid := inventoryProviderID(vendor)
	mid := inventoryModelID(rid)
	route := InventoryRoute{ID: rid, Vendor: vendor, Model: model, Upstream: inventoryDefaultModel(vendor, model, Row{}), Modality: kind, Base: base, Key: key, Enabled: true, Config: Row{"vendor": vendor, "sourceModel": model, "pollInterval": 10}}
	if kind == "text" {
		route.Config["chatAPI"] = "responses"
	}
	a.Store.Exec("INSERT IGNORE INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,?,'inventory',?,'',1,'{}')", pid, vendor, base)
	a.Store.Exec("INSERT INTO inventory_routes(id,provider_id,vendor,model,upstream_model,modality,base_url,api_key,source_enabled,is_primary,priority,weight,config) VALUES(?,?,?,?,?,?,?,?,1,1,1,1,?)", rid, pid, vendor, model, route.Upstream, kind, base, a.encrypt(key), jsonText(route.Config))
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters,inventory_route_id) VALUES(?,?,?,?,?,'local workflow fixture','test',1,'[]',?,?)", mid, model, kind, pid, route.Upstream, jsonText(inventoryParameters(route)), rid)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions) VALUES(?,?,'local fixture','request',25,250000,'{}')", id(), mid)
	return mid
}

func TestInventoryEndToEndBillingAndOwnership(t *testing.T) {
	a := setupIntegration(t)
	var mockBase string
	var submitted atomic.Int32
	var reject atomic.Bool
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			send(w, 200, Row{"url": "https://reference.invalid/source.webm"})
			return
		}
		if r.URL.Path == "/video" {
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write(assets.DemoVideo)
			return
		}
		submitted.Add(1)
		if reject.Load() {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"message":"mock credential must never escape"}`))
			return
		}
		switch r.URL.Path {
		case "/v1/responses":
			send(w, 200, Row{"output_text": "<script>unsafe()</script> 本地模拟文本，不应作为 HTML 执行"})
		case "/v1/audio/speech":
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(inventoryTestWAV())
		case "/v1/images/generations":
			send(w, 200, Row{"data": []Row{{"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="}}})
		default:
			send(w, 200, Row{"status": "succeeded", "video_url": mockBase + "/video"})
		}
	}))
	defer mock.Close()
	mockBase = mock.URL
	apiServer := httptest.NewServer(a.Handler())
	defer apiServer.Close()
	user := &testClient{server: apiServer.URL}
	u := object(user.ok(t, "POST", "/auth/register", Row{"username": "inventory-workflow-user", "password": "Inventory-test-password"}, 200))
	uid := str(u["id"])
	admin := &testClient{server: apiServer.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	outsider := &testClient{server: apiServer.URL}
	outsider.ok(t, "POST", "/auth/register", Row{"username": "inventory-workflow-other", "password": "Inventory-test-password"}, 200)
	user.ok(t, "GET", "/admin/provider-inventory", nil, 403)
	video, e := a.saveAsset(uid, "", "source.webm", "video/webm", "webm", assets.DemoVideo)
	if e != nil {
		t.Fatal(e)
	}
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES('inventory-upload','mock upload','media-upload',?,?,1,'{}')", mock.URL, a.encrypt("upload-local-key"))
	ctx, cancel := context.WithCancel(context.Background())
	done := a.StartWorker(ctx)
	defer func() { cancel(); <-done }()
	await := func(jid string) Row {
		t.Helper()
		for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
			j := object(user.ok(t, "GET", "/jobs/"+jid, nil, 200))
			if j["status"] == "succeeded" || j["status"] == "failed" {
				return j
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("job did not finish")
		return nil
	}
	types := []struct{ vendor, model, kind string }{{"APIYI", "gpt-5.4", "text"}, {"APIYI", "gpt-image-2-APIYI", "image"}, {"copp", "seedance-v2", "video"}, {"openrouter", "tts-gemini-openrouter", "audio"}, {"wavespeed", "wavespeed-ai/video-upscaler", "upscale"}}
	mids := map[string]string{}
	for _, tc := range types {
		mid := seedInventoryTestRoute(a, tc.vendor, tc.model, tc.kind, mock.URL, "workflow-key-"+tc.kind)
		mids[tc.kind] = mid
		params := Row{}
		if tc.kind == "upscale" {
			params["videoUrls"] = []any{video["assetId"]}
			outsider.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "no cross-owner references", "params": params}, 400)
		}
		body := Row{"modelId": mid, "prompt": "local synthetic request", "params": params, "expectedCost": 0.25}
		header := map[string]string{"Idempotency-Key": "inventory-flow-" + tc.kind}
		status, value := user.request(t, "POST", "/jobs", body, header)
		want(t, status, 202)
		jid := str(object(value)["id"])
		job := await(jid)
		want(t, job["status"], "succeeded")
		output := object(array(job["outputs"])[0])
		expected := tc.kind
		if expected == "upscale" {
			expected = "video"
		}
		want(t, output["type"], expected)
		outsider.ok(t, "GET", "/jobs/"+jid+"/upstream", nil, 404)
		before := submitted.Load()
		status, value = user.request(t, "POST", "/jobs", body, header)
		want(t, status, 202)
		want(t, object(value)["id"], jid)
		want(t, submitted.Load(), before)
		user.ok(t, "DELETE", "/jobs/"+jid, nil, 200)
		status, value = user.request(t, "POST", "/jobs", body, header)
		want(t, status, 202)
		want(t, object(value)["id"], jid)
		log := object(admin.ok(t, "GET", "/admin/upstream-calls/"+jid, nil, 200))
		want(t, log["type"], tc.kind)
		if strings.Contains(jsonText(log), "workflow-key-") {
			t.Fatal("new modality leaked a route credential")
		}
	}
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], num(u["balance"])-1.25)
	reject.Store(true)
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": mids["text"], "prompt": "local upstream failure"}, 202))
	failed := await(str(job["id"]))
	want(t, failed["refunded"], true)
	want(t, failed["status"], "failed")
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], num(u["balance"])-1.25)
	rid := a.Store.One("SELECT inventory_route_id FROM models WHERE id=?", mids["text"])["inventory_route_id"]
	user.ok(t, "PATCH", "/admin/provider-inventory/"+str(rid), Row{"sourceEnabled": false}, 403)
	admin.ok(t, "PATCH", "/admin/provider-inventory/"+str(rid), Row{"sourceEnabled": false}, 200)
	before := submitted.Load()
	user.ok(t, "POST", "/jobs", Row{"modelId": mids["text"], "prompt": "disabled route"}, 404)
	want(t, submitted.Load(), before)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE user_id=? AND kind='generation'", uid)["n"], 6)
	want(t, a.Store.One("SELECT count(*) n FROM transactions WHERE user_id=? AND kind='refund'", uid)["n"], 1)
}

func TestInventoryRejectsResultTypeAndPreservesSnapshot(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	var mid, base string
	var submissions, polls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer snapshot-original-key" {
			t.Error("in-flight call switched credentials")
		}
		if r.Method == "POST" {
			submissions.Add(1)
			rid := a.Store.One("SELECT inventory_route_id FROM models WHERE id=?", mid)["inventory_route_id"]
			a.Store.Exec("UPDATE inventory_routes SET api_key=?,base_url='https://must-not-contact.invalid' WHERE id=?", a.encrypt("snapshot-new-key"), rid)
			send(w, 200, Row{"id": "original-task", "status": "queued"})
			return
		}
		if r.URL.Path == "/result" {
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write(assets.DemoVideo)
			return
		}
		if polls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		if polls.Load() == 2 {
			send(w, 200, Row{"id": "another-users-task", "status": "succeeded", "video_url": base + "/wrong-result"})
			return
		}
		send(w, 200, Row{"id": "original-task", "status": "succeeded", "video_url": base + "/result"})
	}))
	defer mock.Close()
	base = mock.URL
	mid = seedInventoryTestRoute(a, "copp", "seedance-v2", "video", base, "snapshot-original-key")
	j := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "immutable route snapshot"}, 202))
	a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", j["id"])
	a.processJob(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", j["id"]))
	want(t, a.Store.One("SELECT status FROM jobs WHERE id=?", j["id"])["status"], "succeeded")
	want(t, submissions.Load(), 1)
	want(t, polls.Load(), 3)
	// A declared image result cannot be accepted as audio just because HTTP returned 200.
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	_, e := a.inventorySave(context.Background(), Row{"id": id(), "user_id": j["userId"], "provider_snapshot": Row{"type": "audio"}}, []any{Row{"bytes": png}}, "", "")
	if e == nil {
		t.Fatal("audio task accepted image bytes")
	}
}

func TestInventoryMissingConfigurationNeverDebitsOrBorrowsKeys(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	balance := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	mid := seedInventoryTestRoute(a, "APIYI", "gpt-5.4", "text", "http://127.0.0.1:9", "valid-route-test-key")
	rid := a.Store.One("SELECT inventory_route_id FROM models WHERE id=?", mid)["inventory_route_id"]
	pid := inventoryProviderID("APIYI")
	// Shared/provider-model credentials must not rescue a broken route binding.
	a.Store.Exec("UPDATE providers SET api_key=? WHERE id=?", a.encrypt("must-never-borrow-parent-key"), pid)
	a.Store.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters,api_key) VALUES(?,?,'gpt-5.4','fallback','text','','[]',?)", id(), pid, a.encrypt("must-never-borrow-model-key"))
	for _, scenario := range []string{"missing_key", "placeholder_key", "missing_url", "source_disabled", "provider_disabled", "unpriced", "disabled_price"} {
		t.Run(scenario, func(t *testing.T) {
			a.Store.Exec("UPDATE inventory_routes SET api_key=?,base_url='http://127.0.0.1:9',source_enabled=1 WHERE id=?", a.encrypt("valid-route-test-key"), rid)
			a.Store.Exec("UPDATE providers SET enabled=1 WHERE id=?", pid)
			switch scenario {
			case "missing_key":
				a.Store.Exec("UPDATE inventory_routes SET api_key='' WHERE id=?", rid)
			case "placeholder_key":
				a.Store.Exec("UPDATE inventory_routes SET api_key=? WHERE id=?", a.encrypt("placeholder"), rid)
			case "missing_url":
				a.Store.Exec("UPDATE inventory_routes SET base_url='' WHERE id=?", rid)
			case "source_disabled":
				a.Store.Exec("UPDATE inventory_routes SET source_enabled=0 WHERE id=?", rid)
			case "provider_disabled":
				a.Store.Exec("UPDATE providers SET enabled=0 WHERE id=?", pid)
			case "unpriced":
				a.Store.Exec("DELETE FROM prices WHERE model_id=?", mid)
			case "disabled_price":
				a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions,enabled) VALUES(?,?,'disabled','request',25,250000,'{}',0)", id(), mid)
			}
			c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "must not submit"}, 400)
			want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balance)
			want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", mid)["n"], 0)
		})
	}
	_ = u
}
