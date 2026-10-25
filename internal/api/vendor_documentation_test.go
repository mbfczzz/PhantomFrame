package api

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentedVendorDefaults(t *testing.T) {
	fixtures := []struct {
		vendor, id          string
		image, video, audio any
	}{
		{"suda", "jy-gpt-image-2", float64(6), nil, nil},
		{"suda", "jy-nano-banana-2.1", float64(14), nil, nil},
		{"suda", "gf8-gpt-image-2.5-flare-4k", float64(16), nil, nil},
		{"suda", "sdas-wd-sd2.5-2-480p", float64(9), float64(0), float64(3)},
		{"modelhub", "vxp-nano-banana-2-lite", float64(14), nil, nil},
		{"openvideo", "30-10-10", float64(30), float64(10), float64(10)},
		{"wxart", "seedance2.0", float64(9), float64(3), float64(3)},
		{"wxart", "seedance2.5", float64(30), float64(10), float64(10)},
	}
	for _, f := range fixtures {
		t.Run(f.vendor+"/"+f.id, func(t *testing.T) {
			kind, params, ok := vendorCatalogEntry(f.vendor, Row{"id": f.id}, Row{})
			want(t, ok, true)
			params = validateParameters(array(jsonText(params)))
			limits := Row{}
			for _, raw := range params {
				p := object(raw)
				if p["type"] == "assets" {
					limits[str(p["itemType"])] = p["max"]
				}
			}
			want(t, limits["image"], f.image)
			want(t, limits["video"], f.video)
			want(t, limits["audio"], f.audio)
			if !strings.Contains(documentedModelDescription(f.vendor, f.id, kind, params), "参考图片最多") {
				t.Fatal("missing reference limits in description")
			}
		})
	}
	_, params, _ := vendorCatalogEntry("catcoder", Row{"id": "k1-gpt-image-2.5", "kind": "image", "capabilities": Row{"max_images": float64(0), "available": true}}, Row{})
	desc := documentedModelDescription("catcoder", "k1-gpt-image-2.5", "image", params)
	if !strings.Contains(desc, "不支持参考图片") {
		t.Fatal("live zero limit must override saved documentation")
	}
	_, params, _ = vendorCatalogEntry("wxart", Row{"id": "veo3.1"}, Row{})
	desc = documentedModelDescription("wxart", "veo3.1", "video", params)
	if !strings.Contains(desc, "数量上限未公开") || strings.Contains(desc, "4–15") {
		t.Fatal("compatibility defaults must not be represented as documented capabilities")
	}
	current := []any{Row{"name": "imageUrls", "type": "assets", "itemType": "image", "max": float64(2)}}
	baseline := []any{Row{"name": "imageUrls", "type": "assets", "itemType": "image"}}
	desired := []any{Row{"name": "imageUrls", "type": "assets", "itemType": "image", "max": float64(9)}}
	merged, manual := mergeDocumentedParameters(current, baseline, desired)
	want(t, object(merged[0])["max"], 2)
	want(t, len(manual), 1)
	merged, _ = mergeDocumentedParameters([]any{}, baseline, desired)
	want(t, len(merged), 0)
	selectA := Row{"name": "quality", "type": "select", "options": []any{"high", "low"}, "default": "high"}
	selectB := Row{"name": "quality", "type": "select", "options": []any{"low", "high"}, "default": "high"}
	merged, manual = mergeDocumentedParameters([]any{selectA}, []any{selectA}, []any{selectB})
	want(t, jsonText(merged), jsonText([]any{selectA}))
	want(t, len(manual), 0)
}

func TestDocumentedReferenceLimitsEnforced(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	uid := str(object(admin.ok(t, "GET", "/me", nil, 200))["id"])
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	image, e := a.saveAsset(uid, "", "reference.png", "image/png", "png", png)
	if e != nil {
		t.Fatal(e)
	}
	video, e := a.saveAsset(uid, "", "reference.webm", "video/webm", "webm", []byte{26, 69, 223, 163})
	if e != nil {
		t.Fatal(e)
	}
	defs := []Row{{"name": "resolution", "label": "分辨率", "type": "select", "default": "720p", "options": []string{"480p", "720p"}}, {"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image", "max": 3, "maxTotal": 1}, {"name": "videoUrls", "label": "参考视频", "type": "assets", "itemType": "video", "max": 3, "maxTotal": 1, "maxByResolution": Row{"720p": 0}}}
	admin.ok(t, "POST", "/admin/models", Row{"id": "documented-limits", "name": "Documented limits", "type": "video", "provider_id": "demo", "upstream_model": "documented", "parameters": defs}, 201)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "documented-limits", "label": "test", "unit": "request", "credits": 1}, 200)
	videoParams := Row{"videoUrls": []any{video["assetId"]}}
	admin.ok(t, "POST", "/pricing/quote", Row{"modelId": "documented-limits", "params": videoParams}, 400)
	videoParams["resolution"] = "480p"
	admin.ok(t, "POST", "/pricing/quote", Row{"modelId": "documented-limits", "params": videoParams}, 200)
	videoParams["imageUrls"] = []any{image["assetId"]}
	before := object(admin.ok(t, "GET", "/me", nil, 200))["balance"]
	admin.ok(t, "POST", "/jobs", Row{"modelId": "documented-limits", "prompt": "combined quota", "params": videoParams}, 400)
	want(t, object(admin.ok(t, "GET", "/me", nil, 200))["balance"], before)
}
