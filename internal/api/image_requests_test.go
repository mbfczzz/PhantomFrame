package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageRequestPolicy(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	key := object(admin.ok(t, "POST", "/keys", Row{"name": "image policy test"}, 201))
	apiClient := &testClient{server: server.URL, key: str(key["token"])}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want(t, r.URL.Path, "/v1/images/generations")
		var body Row
		must(json.NewDecoder(r.Body).Decode(&body))
		want(t, body["n"], 1)
		outputs := []Row{}
		for i := 0; i < 4; i++ {
			outputs = append(outputs, Row{"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="})
		}
		json.NewEncoder(w).Encode(Row{"data": outputs})
	}))
	defer upstream.Close()
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Image single request", "protocol": "openai-image", "base_url": upstream.URL + "/v1", "apiKey": "local-mock-key"}, 201))
	for index, upstreamID := range []string{"gpt-image-2", "midjourney-v6", "mj-v6"} {
		mid := "image-policy-" + str(index)
		model := object(admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": "Midjourney display alias", "type": "image", "provider_id": provider["id"], "upstream_model": upstreamID, "parameters": []Row{{"name": "n", "label": "生成张数", "type": "number", "default": 4, "min": 1, "max": 10}, {"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image", "max": 16}}}, 201))
		want(t, object(array(model["parameters"])[0])["max"], 1)
		want(t, object(array(model["parameters"])[1])["max"], 16)
		limit := 4
		if index == 0 {
			limit = 1
		}
		want(t, model["maxOutputsPerRequest"], limit)
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "single task", "unit": "request", "credits": .108}, 200)
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "forbidden image price", "unit": "image", "credits": .108}, 400)
		balance := object(admin.ok(t, "GET", "/me", nil, 200))["balance"]
		count := a.Store.One("SELECT count(*) n FROM jobs")["n"]
		for _, invalid := range []any{0, 2, 4, -1, 1.5, "1", nil} {
			apiClient.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{"n": invalid}}, 400)
		}
		apiClient.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "must not submit a batch", "params": Row{"n": 4}}, 400)
		want(t, a.Store.One("SELECT count(*) n FROM jobs")["n"], count)
		want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], balance)
		quote := object(apiClient.ok(t, "POST", "/pricing/quote", Row{"modelId": mid}, 200))
		want(t, quote["unit"], "request")
		want(t, quote["quantity"], 1)
		want(t, quote["total"], .11)
		want(t, object(quote["params"])["n"], 1)
		job := object(apiClient.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "single task", "expectedCost": .11}, 202))
		want(t, job["cost"], .11)
		outputs, err := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
		if err != nil {
			t.Fatal(err)
		}
		want(t, len(outputs), limit)
		want(t, a.Store.One("SELECT count(*) n FROM assets WHERE job_id=?", job["id"])["n"], limit)
		admin.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
		want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], balance)
	}
	for _, name := range []string{"mjollnir-image", "notmidjourney", "image-mjollnir"} {
		want(t, imageOutputLimit(name), 1)
	}
	for _, vendor := range []string{"rolldek", "modelhub"} {
		kind, parameters, _ := vendorCatalogEntry(vendor, Row{"id": "gpt-image-2", "media_type": "image"}, Row{})
		if kind == "image" {
			for _, raw := range parameters {
				if object(raw)["name"] == "n" {
					want(t, object(raw)["max"], 1)
				}
			}
		}
	}
}

func TestImageRequestMigration(t *testing.T) {
	a := setupIntegration(t)
	legacy := []Row{{"name": "n", "label": "生成张数", "type": "select", "default": 4, "options": []int{1, 2, 4}}, {"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image", "max": 30}}
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,tags,parameters,enabled) VALUES('legacy-image','Legacy','image','demo','legacy-gpt-image-test','每次生成 1–10 张，按张计费','[]',?,0)", jsonText(legacy))
	a.Store.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters,api_key) VALUES('legacy-entry','demo','legacy-gpt-image-test','Legacy','image','每次生成 1–10 张，按张计费',?,?)", jsonText(legacy), a.encrypt("preserved-model-key"))
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,billing_extras,conditions,enabled) VALUES('legacy-price','legacy-image','legacy','image',11,108000,?,'{}',1)", jsonText(Row{"imagePrice": .01, "includedImages": 2}))
	beforePrices := a.Store.All("SELECT * FROM prices ORDER BY id")
	beforeUsers := jsonText(a.Store.All("SELECT * FROM users ORDER BY id"))
	beforeLedger := jsonText(a.Store.All("SELECT * FROM transactions ORDER BY id"))
	beforeProvider := jsonText(a.Store.All("SELECT * FROM providers ORDER BY id"))
	modelKey := a.Store.One("SELECT api_key FROM provider_models WHERE id='legacy-entry'")["api_key"]
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	want(t, a.Store.One("SELECT enabled FROM models WHERE id='legacy-image'")["enabled"], 0)
	for _, price := range beforePrices {
		if price["id"] == "legacy-price" {
			price["unit"] = "request"
		}
	}
	want(t, jsonText(a.Store.All("SELECT * FROM prices ORDER BY id")), jsonText(beforePrices))
	for _, table := range []string{"models", "provider_models"} {
		row := a.Store.One("SELECT parameters,description FROM " + table + " WHERE upstream_model='legacy-gpt-image-test'")
		want(t, object(array(row["parameters"])[0])["default"], 1)
		want(t, object(array(row["parameters"])[0])["max"], 1)
		want(t, object(array(row["parameters"])[1])["max"], 30)
		want(t, row["description"], "每次生成 1 张，按次计费")
	}
	want(t, a.Store.One("SELECT api_key FROM provider_models WHERE id='legacy-entry'")["api_key"], modelKey)
	want(t, jsonText(a.Store.All("SELECT * FROM users ORDER BY id")), beforeUsers)
	want(t, jsonText(a.Store.All("SELECT * FROM transactions ORDER BY id")), beforeLedger)
	want(t, jsonText(a.Store.All("SELECT * FROM providers ORDER BY id")), beforeProvider)
	backups, err := filepath.Glob(filepath.Join(a.Config.DataDir, "backups", "image-request-billing-*.encrypted.json"))
	if err != nil {
		t.Fatal(err)
	}
	want(t, len(backups), 1)
	sealed, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "legacy-price") {
		t.Fatal("backup should be encrypted")
	}
	plain, err := a.decrypt(string(sealed))
	if err != nil {
		t.Fatal(err)
	}
	want(t, object(array(object(plain)["prices"])[0])["unit"], "image")
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	backups, _ = filepath.Glob(filepath.Join(a.Config.DataDir, "backups", "image-request-billing-*.encrypted.json"))
	want(t, len(backups), 1)
}
