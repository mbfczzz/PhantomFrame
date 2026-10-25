package api

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"path/filepath"
	assets "phantomframe/server"
	"runtime"
	"testing"
	"time"
)

func TestPreciseUnitPricesAndReferenceCharges(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin, user := &testClient{server: server.URL}, &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	u := object(user.ok(t, "POST", "/auth/register", Row{"username": "precise-pricing", "password": "Precise-pass-2026"}, 200))
	uid := str(u["id"])
	model := Row{"id": "precise-reference-price", "name": "Precise reference price", "type": "video", "provider_id": "demo", "upstream_model": "test", "parameters": []Row{{"name": "duration", "label": "时长", "type": "number", "default": 5}, {"name": "seed", "label": "随机种子", "type": "number"}, {"name": "images", "label": "参考图片", "type": "assets", "itemType": "image"}, {"name": "videos", "label": "参考视频", "type": "assets", "itemType": "video"}}}
	admin.ok(t, "POST", "/admin/models", model, 201)
	priceBody := Row{"model_id": model["id"], "label": "precise", "unit": "second", "credits": .123456, "extras": Row{"includedImages": 1, "imagePrice": .06, "videoSecondPrice": .24, "videoRounding": "total"}}
	p := object(admin.ok(t, "POST", "/admin/prices", priceBody, 200))
	want(t, p["credits"], .123456)
	want(t, a.Store.One("SELECT unit_price_micros FROM prices WHERE id=?", p["id"])["unit_price_micros"], 123456)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	images := []any{}
	for i := 0; i < 3; i++ {
		asset, e := a.saveAsset(uid, "", "reference.png", "image/png", "png", png)
		if e != nil {
			t.Fatal(e)
		}
		images = append(images, asset["assetId"])
	}
	videos := []any{}
	for i := 0; i < 2; i++ {
		asset, e := a.saveAsset(uid, "", "reference.webm", "video/webm", "webm", assets.DemoVideo)
		if e != nil {
			t.Fatal(e)
		}
		videos = append(videos, asset["assetId"])
		must(a.Redis.Set(context.Background(), a.Config.RedisPrefix+"media-duration:"+str(asset["assetId"]), 1001, time.Hour).Err())
	}
	params := Row{"images": images, "videos": videos, "seed": ""}
	quote := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": params}, 200))
	want(t, quote["unitPrice"], .123456)
	want(t, quote["total"], 1.46)
	want(t, len(array(quote["breakdown"])), 3)
	want(t, object(array(quote["breakdown"])[2])["quantity"], 3)
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": model["id"], "params": params, "prompt": "exact pricing", "expectedCost": 1.46}, 202))
	want(t, job["cost"], 1.46)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 98.54)
	user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
	priceBody["extras"] = Row{"includedImages": 1, "imagePrice": .06, "videoSecondPrice": .24, "videoRounding": "each"}
	admin.ok(t, "PUT", "/admin/prices/"+str(p["id"]), priceBody, 200)
	quote = object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": params}, 200))
	want(t, quote["total"], 1.7)
	want(t, object(array(quote["breakdown"])[2])["quantity"], 4)
	// Changing only a base price must preserve the supplier's reference charges.
	delete(priceBody, "extras")
	saved := object(admin.ok(t, "PUT", "/admin/prices/"+str(p["id"]), priceBody, 200))
	want(t, object(saved["extras"])["videoSecondPrice"], .24)
	priceBody["credits"] = .1234567
	admin.ok(t, "PUT", "/admin/prices/"+str(p["id"]), priceBody, 400)
	if runtime.GOOS == "windows" {
		_, file, _, _ := runtime.Caller(0)
		t.Setenv("FFPROBE_PATH", filepath.Join(filepath.Dir(file), "../../.tools/ffmpeg/ffprobe.exe"))
		asset := a.Store.One("SELECT * FROM assets WHERE id=?", videos[0])
		must(a.Redis.Del(context.Background(), a.Config.RedisPrefix+"media-duration:"+str(videos[0])).Err())
		want(t, a.referenceDurationMillis(asset), 3000)
	}
	if e := a.Migrate(); e != nil {
		t.Fatal(e)
	}
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", uid)["balance"], 10000)
	want(t, a.Store.One("SELECT unit_price_micros FROM prices WHERE id=?", p["id"])["unit_price_micros"], 123456)
}
