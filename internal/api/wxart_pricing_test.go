package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWXArtImageSendsPricedResolution(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	uid := str(object(admin.ok(t, "GET", "/me", nil, 200))["id"])
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	png, _ := base64.StdEncoding.DecodeString(encoded)
	asset, e := a.saveAsset(uid, "", "reference.png", "image/png", "png", png)
	if e != nil {
		t.Fatal(e)
	}
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/storage/files" {
			want(t, r.Header.Get("Authorization"), "Bearer storage-key")
			json.NewEncoder(w).Encode(Row{"data": Row{"fileUrl": "https://cdn.example.com/reference.png"}})
			return
		}
		want(t, r.URL.Path, "/v1/images/generations")
		want(t, r.Header.Get("Authorization"), "Bearer vendor-key")
		var body Row
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		want(t, body["model"], "nano-banana2")
		want(t, body["resolution"], "4k")
		want(t, body["aspect_ratio"], "16:9")
		want(t, len(array(body["images_url"])), 1)
		if body["size"] != nil {
			t.Error("native image resolution must not be replaced by a standard size")
		}
		requests++
		json.NewEncoder(w).Encode(Row{"data": []Row{{"b64_json": encoded}}})
	}))
	defer upstream.Close()
	storage := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "pricing-storage", "protocol": "media-upload", "base_url": upstream.URL + "/storage", "apiKey": "storage-key", "config": Row{"uploadPath": "/files"}}, 201))
	provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "pricing-wxart", "protocol": "wxart", "base_url": upstream.URL + "/v1", "apiKey": "vendor-key", "config": Row{"uploadProviderId": storage["id"]}}, 201))
	model := Row{"id": "wxart-pricing-resolution", "name": "WXArt resolution", "type": "image", "provider_id": provider["id"], "upstream_model": "nano-banana2", "parameters": []Row{{"name": "resolution", "label": "分辨率", "type": "select", "default": "4k", "options": []string{"1k", "4k"}}, {"name": "aspectRatio", "label": "比例", "type": "select", "default": "16:9", "options": []string{"16:9"}}, {"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image"}}}
	admin.ok(t, "POST", "/admin/models", model, 201)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": model["id"], "label": "4K", "unit": "request", "credits": 0, "conditions": Row{"resolution": "4k"}}, 200)
	job := object(admin.ok(t, "POST", "/jobs", Row{"modelId": model["id"], "prompt": "test", "params": Row{"imageUrls": []any{asset["assetId"]}}}, 202))
	outputs, e := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
	if e != nil {
		t.Fatal(e)
	}
	want(t, len(outputs), 1)
	want(t, requests, 1)
	admin.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
}
