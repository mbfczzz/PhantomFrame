package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Contract check: every visible, variable scalar control must affect the native
// request or be rejected before billing. Never submit a real upstream request.
func TestInventoryExposedParametersAffectRequest(t *testing.T) {
	routes, _, err := ReadProviderInventory(strings.NewReader(inventoryFixtureCSV(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		t.Run(r.ID, func(t *testing.T) {
			params := Row{}
			defs := inventoryParameters(r)
			for _, raw := range defs {
				p := object(raw)
				params[str(p["name"])] = p["default"]
				if truth(p["required"]) && p["type"] == "text" && str(p["default"]) == "" {
					params[str(p["name"])] = "test-explicit-value"
				}
			}
			provider := Row{"type": r.Modality, "upstreamModel": r.Upstream, "config": clone(r.Config)}
			object(provider["config"])["upstreamModel"] = r.Upstream
			refs := map[string][]string{}
			if r.Modality == "upscale" {
				refs["video"] = []string{"https://reference.invalid/video"}
			}
			base, _, _, e := inventoryPlan(Row{"id": "fixed-test-task", "prompt": "local contract", "params": params}, provider, refs, nil)
			if e != nil {
				t.Fatal(e)
			}
			for _, raw := range defs {
				p := object(raw)
				name := str(p["name"])
				values := []any{}
				switch p["type"] {
				case "select":
					values = array(p["options"])
				case "number":
					values = []any{p["min"], p["max"]}
				case "boolean":
					values = []any{true, false}
				case "text":
					values = []any{"test-custom-value"}
				default:
					continue
				}
				for _, v := range values {
					if option, ok := v.(map[string]any); ok {
						v = option["v"]
					}
					if v == nil || str(v) == str(params[name]) {
						continue
					}
					variant := clone(params)
					variant[name] = v
					call, _, _, e := inventoryPlan(Row{"id": "fixed-test-task", "prompt": "local contract", "params": variant}, provider, refs, nil)
					if e == nil && jsonText(call) == jsonText(base) {
						t.Errorf("%s/%s silently ignores parameter %s=%v", r.Vendor, r.Model, name, v)
					}
				}
			}
		})
	}
}

func TestInventoryFourImageResultAndNativeAuth(t *testing.T) {
	a := setupIntegration(t)
	uid := str(a.Store.One("SELECT id FROM users WHERE role='admin' LIMIT 1")["id"])
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	mid := seedInventoryTestRoute(a, "midjourney-api", "midjourney-v8.2", "image", "http://127.0.0.1:9", "local-test-key")
	a.Store.Exec("UPDATE models SET upstream_model='8.2' WHERE id=?", mid)
	want(t, a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id=?", mid))["maxOutputsPerRequest"], 4)
	p := Row{"type": "image", "upstreamModel": "8.2", "config": Row{"vendor": "midjourney-api", "sourceModel": "midjourney-v8.2"}}
	outputs := []any{Row{"b64_json": encoded}, Row{"b64_json": encoded}, Row{"b64_json": encoded}, Row{"b64_json": encoded}}
	saved, err := a.inventorySave(context.Background(), Row{"id": "", "user_id": uid, "provider_snapshot": p}, outputs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want(t, len(saved), 4)
	png, _ := base64.StdEncoding.DecodeString(encoded)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "protected-image-key" || r.Header.Get("Authorization") != "" {
			t.Error("download used the wrong route authentication")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer mock.Close()
	p = Row{"type": "image", "upstreamModel": "nano-banana-2", "config": Row{"vendor": "MYNEWAPI", "sourceModel": "nano-banana-2"}}
	_, err = a.inventorySave(context.Background(), Row{"id": "", "user_id": uid, "provider_snapshot": p}, []any{Row{"url": mock.URL + "/result"}}, mock.URL, "protected-image-key")
	if err != nil {
		t.Fatal(err)
	}
}

func TestInventorySubmitDoesNotReplayAfterConnectionLoss(t *testing.T) {
	a := setupIntegration(t)
	var submissions atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/warm" {
			_, _ = w.Write([]byte("ready"))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if submissions.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		send(w, 200, Row{"output_text": "must not resubmit"})
	}))
	defer mock.Close()
	// Reuse an established connection, the condition that permits automatic replay.
	res, err := upstreamClient().Get(mock.URL + "/warm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	p := Row{"base_url": mock.URL, "config": Row{"vendor": "APIYI", "jobID": "dedupe-key"}}
	_, _, _, err = a.inventoryHTTP(context.Background(), p, "test-key", inventoryCall{Path: "/submit", Body: Row{"prompt": "local"}})
	if err == nil {
		t.Fatal("uncertain submission unexpectedly succeeded")
	}
	want(t, submissions.Load(), 1)
}

func TestInventoryPollConnectionLossAndForeignFailure(t *testing.T) {
	a := setupIntegration(t)
	uid := str(a.Store.One("SELECT id FROM users WHERE role='admin' LIMIT 1")["id"])
	var submissions, polls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			submissions.Add(1)
			send(w, 200, Row{"id": "my-task", "status": "queued"})
			return
		}
		switch polls.Add(1) {
		case 1:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		case 2:
			send(w, 200, Row{"data": Row{"id": "another-task", "status": "failed", "error": Row{"message": "foreign failure"}}})
		default:
			send(w, 200, Row{"id": "my-task", "status": "succeeded", "data": []Row{{"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="}}})
		}
	}))
	defer mock.Close()
	p := Row{"type": "image", "base_url": mock.URL, "upstreamModel": "gpt-image-2", "api_key": a.encrypt("local-poll-test-key"), "config": Row{"vendor": "secure-skill", "sourceModel": "gpt-image-2", "upstreamModel": "gpt-image-2", "pollInterval": 10}}
	j := Row{"id": id(), "user_id": uid, "prompt": "mock", "params": Row{}, "provider_snapshot": p}
	a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,outputs,provider_snapshot) VALUES(?,?,'nanobanana','mock','{}','running',0,'[]',?)", j["id"], uid, jsonText(p))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outputs, err := a.inventoryUpstream(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	want(t, len(outputs), 1)
	want(t, submissions.Load(), 1)
	if polls.Load() < 3 {
		t.Fatal("polling did not recover the original task")
	}
}

func TestInventoryImportBindingChangesAndLegacyCatalogGuards(t *testing.T) {
	a := setupIntegration(t)
	input := inventoryFixtureCSV(t)
	if _, err := a.ImportProviderInventory(strings.NewReader(input), true); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(input)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	column := map[string]int{}
	for i, key := range records[0] {
		column[key] = i
	}
	var target int
	for i := 1; i < len(records); i++ {
		if records[i][column["provider"]] == "APIYI" {
			target = i
			break
		}
	}
	rid := records[target][column["route_id"]]
	mid := inventoryModelID(rid)
	a.Store.Exec("UPDATE models SET enabled=1 WHERE id=?", mid)
	records[target][column["base_url"]] = "http://127.0.0.1:9"
	encode := func() string { var b bytes.Buffer; w := csv.NewWriter(&b); _ = w.WriteAll(records); return b.String() }
	if _, err := a.ImportProviderInventory(strings.NewReader(encode()), true); err != nil {
		t.Fatal(err)
	}
	want(t, truth(a.Store.One("SELECT enabled FROM models WHERE id=?", mid)["enabled"]), false)
	old := jsonText(a.Store.One("SELECT * FROM inventory_routes WHERE id=?", rid))
	records[target][column["model"]] = "different-source-model"
	if _, err := a.ImportProviderInventory(strings.NewReader(encode()), true); err == nil {
		t.Fatal("route ID reused across different model identity")
	}
	want(t, jsonText(a.Store.One("SELECT * FROM inventory_routes WHERE id=?", rid)), old)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	pid := inventoryProviderID("APIYI")
	c.ok(t, "POST", "/admin/providers/"+pid+"/sync-models", Row{}, 400)
	c.ok(t, "POST", "/admin/providers/"+pid+"/models", Row{"upstreamModel": "rogue", "name": "rogue", "type": "image", "parameters": []any{}}, 400)
	entry := id()
	a.Store.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters) VALUES(?,?,'rogue','legacy entry','image','','[]')", entry, pid)
	c.ok(t, "PUT", "/admin/providers/"+pid+"/models/"+entry+"/key", Row{"apiKey": "must-not-use-legacy-key"}, 400)
	c.ok(t, "POST", "/admin/providers/"+pid+"/models/"+entry+"/publish", Row{"modelId": "rogue", "name": "rogue", "type": "image"}, 400)
}

func TestInventoryOutdatedSchemaCannotChargeAndCanBeRefreshed(t *testing.T) {
	a := setupIntegration(t)
	mid := seedInventoryTestRoute(a, "afrifex", "seedance-2-afti", "video", "http://127.0.0.1:9", "local-config-key")
	m := a.Store.One("SELECT * FROM models WHERE id=?", mid)
	params := array(m["parameters"])
	params = append(params, choice("resolution", "obsolete resolution", []any{"720p", "1080p"}, "1080p"))
	a.Store.Exec("UPDATE models SET parameters=? WHERE id=?", jsonText(params), mid)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	before := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "blocked obsolete schema"}, 400)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), before)
	want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", mid)["n"], 0)
	prices := jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid))
	c.ok(t, "POST", "/admin/provider-inventory/"+str(m["inventory_route_id"])+"/parameters", Row{}, 200)
	want(t, truth(a.Store.One("SELECT enabled FROM models WHERE id=?", mid)["enabled"]), false)
	want(t, jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid)), prices)
	a.Store.Exec("UPDATE models SET enabled=1 WHERE id=?", mid)
	c.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{}}, 200)
}

func TestInventoryActualPromptIsCheckedBeforeDebit(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	mid := seedInventoryTestRoute(a, "midjourney-api", "midjourney-v8.2", "image", "http://127.0.0.1:9", "unused-mock-key")
	a.Store.Exec("UPDATE models SET upstream_model='8.2' WHERE id=?", mid)
	a.Store.Exec("UPDATE inventory_routes SET upstream_model='8.2' WHERE id=(SELECT inventory_route_id FROM models WHERE id=?)", mid)
	before := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	c.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{}}, 200)
	c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "paid prompt --hd"}, 400)
	want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", mid)["n"], 0)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), before)
	kinovi := seedInventoryTestRoute(a, "kinovi", "gpt-image-2", "image", "http://127.0.0.1:9", "unused-kinovi-key")
	c.ok(t, "POST", "/jobs", Row{"modelId": kinovi, "prompt": strings.Repeat("字", int(integer(inventoryKinoviSpec("gpt-image-2", "gpt-image-2")["maxPrompt"]))+1)}, 400)
	want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", kinovi)["n"], 0)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), before)
}

func TestLegacyVendorSubmitDoesNotReplayAfterConnectionLoss(t *testing.T) {
	var submissions atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/warm" {
			_, _ = w.Write([]byte("ready"))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if submissions.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		send(w, 200, Row{"id": "duplicate-task"})
	}))
	defer mock.Close()
	res, err := upstreamClient().Get(mock.URL + "/warm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	_, err = vendorHTTP(context.Background(), mock.URL, "mock-key", "/submit", strings.NewReader(`{"prompt":"fixture"}`), "application/json", "dedupe-fixture")
	if err == nil {
		t.Fatal("uncertain legacy submission unexpectedly succeeded")
	}
	want(t, submissions.Load(), 1)
}
