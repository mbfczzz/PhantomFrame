package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	assets "phantomframe/server"
	"testing"
)

func TestMaiTokenCatalog(t *testing.T) {
	for id, raw := range maiTokenCatalog {
		kind, params, ok := vendorCatalogEntry("maitoken", Row{"id": id}, Row{})
		want(t, ok, true)
		want(t, kind, object(raw)["kind"])
		validateParameters(array(jsonText(params)))
	}
	for _, id := range []string{"deepseek-v3.2", "unknown-video", "gpt-image-2"} {
		_, _, ok := vendorCatalogEntry("maitoken", Row{"id": id}, Row{})
		want(t, ok, false)
	}
	for _, resolution := range []string{"480p", "720p", "1080p"} {
		d := maiTokenDefinition("sdg-2.5-" + resolution)
		for _, raw := range array(d["parameters"]) {
			p := object(raw)
			if p["name"] == "duration" {
				want(t, p["min"], 4)
				want(t, p["max"], 30)
				want(t, p["integer"], true)
			}
			if p["itemType"] == "image" {
				want(t, p["max"], 30)
			}
		}
	}
}

func TestMaiTokenContractsAndReferencePricing(t *testing.T) {
	a := setupIntegration(t)
	local := httptest.NewServer(a.Handler())
	defer local.Close()
	admin := &testClient{server: local.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	uid := str(object(admin.ok(t, "GET", "/me", nil, 200))["id"])
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	png, _ := base64.StdEncoding.DecodeString(encoded)
	image, e := a.saveAsset(uid, "", "ref.png", "image/png", "png", png)
	if e != nil {
		t.Fatal(e)
	}
	video, e := a.saveAsset(uid, "", "ref.webm", "video/webm", "webm", assets.DemoVideo)
	if e != nil {
		t.Fatal(e)
	}
	// This tariff ignores input duration, so probing the reference video must not be required.
	t.Setenv("FFPROBE_PATH", "nonexistent-maitoken-test-probe")
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			submitted, polled, uploaded := 0, 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/storage/files" {
					want(t, r.Header.Get("Authorization"), "Bearer storage-key")
					uploaded++
					json.NewEncoder(w).Encode(Row{"data": Row{"fileUrl": "https://cdn.example.com/reference-" + str(uploaded)}})
					return
				}
				want(t, r.Header.Get("Authorization"), "Bearer mai-key")
				if r.Method == "POST" {
					submitted++
					var body Row
					must(json.NewDecoder(r.Body).Decode(&body))
					want(t, body["contract_version"], kind+"-v1")
					want(t, body["ratio"], "16:9")
					if r.Header.Get("Idempotency-Key") == "" {
						t.Error("missing idempotency key")
					}
					if kind == "video" {
						want(t, r.URL.Path, "/v1/videos")
						want(t, body["duration"], 4)
						want(t, vendorValue(body, "references.1.type"), "video")
						want(t, vendorValue(body, "references.1.role"), "reference_video")
					} else {
						want(t, r.URL.Path, "/v1/images/generations")
						want(t, body["async"], true)
						want(t, body["n"], 1)
					}
					want(t, vendorValue(body, "references.0.role"), "reference_image")
					json.NewEncoder(w).Encode(Row{"id": "unrelated-upstream-id", "task_id": "local-task", "status": "queued"})
					return
				}
				if r.URL.Path == "/v1/videos/local-task/content" {
					w.Header().Set("Content-Type", "video/webm")
					w.Write(assets.DemoVideo)
					return
				}
				polled++
				if polled == 1 {
					w.WriteHeader(503)
					return
				}
				result := Row{"task_id": "local-task", "status": "completed"}
				if kind == "image" {
					want(t, r.URL.Path, "/v1/images/generations/local-task")
					result["data"] = []Row{{"b64_json": encoded}}
				} else {
					want(t, r.URL.Path, "/v1/videos/local-task")
					result["result"] = Row{"url": "/v1/videos/local-task/content"}
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer upstream.Close()
			storage := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "mai-storage-" + kind, "protocol": "media-upload", "base_url": upstream.URL + "/storage", "apiKey": "storage-key", "config": Row{"uploadPath": "/files"}}, 201))
			provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "mai-" + kind, "protocol": "maitoken", "base_url": upstream.URL + "/v1", "apiKey": "mai-key", "config": Row{"uploadProviderId": storage["id"], "pollInterval": 20}}, 201))
			upModel := "nano-banana-pro"
			if kind == "video" {
				upModel = "sdg-2.5-480p"
			}
			mid := "maitoken-test-" + kind
			_, params, _ := vendorCatalogEntry("maitoken", Row{"id": upModel}, Row{})
			admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": mid, "type": kind, "provider_id": provider["id"], "upstream_model": upModel, "parameters": params}, 201)
			entry := a.Store.One("SELECT id FROM provider_models WHERE provider_id=? AND upstream_model=?", provider["id"], upModel)
			admin.ok(t, "PUT", "/admin/providers/"+str(provider["id"])+"/models/"+str(entry["id"])+"/key", Row{"apiKey": "mai-key"}, 200)
			a.Store.Exec("UPDATE providers SET api_key=? WHERE id=?", a.encrypt("unused-mai-default"), provider["id"])
			price := Row{"model_id": mid, "label": "test", "unit": "request", "credits": 0}
			if kind == "video" {
				price["unit"] = "second"
				price["credits"] = .768
				price["extras"] = Row{"videoOutputSecondPrice": 1.08}
			}
			admin.ok(t, "POST", "/admin/prices", price, 200)
			values := Row{"imageUrls": []any{image["assetId"]}, "aspectRatio": "16:9"}
			if kind == "video" {
				q := object(admin.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": values}, 200))
				want(t, q["total"], 3.07)
				values["videoUrls"] = []any{video["assetId"]}
				q = object(admin.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": values}, 200))
				want(t, q["total"], 7.39)
				want(t, object(array(q["breakdown"])[1])["quantity"], 4)
				admin.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{"duration": 4.5}}, 400)
			}
			balance := num(object(admin.ok(t, "GET", "/me", nil, 200))["balance"])
			job := object(admin.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "参考图片1", "params": values}, 202))
			out, e := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
			if e != nil {
				t.Fatal(e)
			}
			want(t, len(out), 1)
			want(t, submitted, 1)
			want(t, polled, 2)
			want(t, a.Store.One("SELECT upstream_id FROM jobs WHERE id=?", job["id"])["upstream_id"], "local-task")
			if kind == "video" {
				want(t, uploaded, 2)
				want(t, job["cost"], 7.39)
			} else {
				want(t, uploaded, 1)
			}
			admin.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
			want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], balance)
		})
	}
}
