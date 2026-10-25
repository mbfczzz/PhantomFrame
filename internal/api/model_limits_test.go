package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestModelReferenceLimits(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "limit-owner", "password": "Limit-pass-2026"}, 200))
	outsider := object(other.ok(t, "POST", "/auth/register", Row{"username": "limit-other", "password": "Limit-pass-2026"}, 200))
	provider := a.Store.One("SELECT id FROM providers LIMIT 1")
	definitions := []any{
		Row{"name": "images", "label": "参考图片", "type": "assets", "itemType": "image"},
		Row{"name": "videos", "label": "参考视频", "type": "assets", "itemType": "video", "max": nil},
		Row{"name": "audio", "label": "参考音频", "type": "assets", "itemType": "audio", "max": ""},
		Row{"name": "seed", "label": "种子", "type": "number", "default": 0, "min": nil, "max": ""},
		Row{"name": "ratio", "label": "画幅", "type": "select", "default": "5:4", "options": []any{"5:4", "1:1"}},
	}
	config := Row{"id": "reference-limits", "name": "Reference limits", "type": "video", "provider_id": provider["id"], "upstream_model": "demo", "parameters": definitions}
	model := object(admin.ok(t, "POST", "/admin/models", config, 201))
	for _, value := range array(model["parameters"]) {
		if object(value)["max"] != nil {
			t.Fatal("blank limits must not acquire an implicit max")
		}
	}
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": model["id"], "label": "default", "unit": "request", "credits": 1, "conditions": Row{}}, 200)
	assets := map[string][]string{}
	for _, kind := range []string{"image", "video", "audio"} {
		count := 12
		if kind == "image" {
			count = 501
		}
		for i := 0; i < count; i++ {
			assetID := id()
			a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size) VALUES(?,?,?,?,?,?,1)", assetID, owner["id"], fmt.Sprintf("%s-%d", kind, i), kind, kind+"/test", assetID+".test")
			assets[kind] = append(assets[kind], assetID)
		}
	}
	params := Row{"images": assets["image"], "videos": assets["video"], "audio": assets["audio"], "seed": -7}
	result := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": params}, 200))
	want(t, object(result["params"])["seed"], -7)
	want(t, object(result["params"])["ratio"], "5:4")
	want(t, len(array(object(result["params"])["images"])), 501)
	params["seed"] = 20001
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": params}, 200)
	balance := object(user.ok(t, "GET", "/me", nil, 200))["balance"]
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": model["id"], "prompt": "No implicit limit", "params": params}, 202))
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], num(balance)-1)
	user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], balance)
	foreignID := id()
	a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size) VALUES(?,?,'foreign','image','image/test','foreign.test',1)", foreignID, outsider["id"])
	invalid := append([]string{}, assets["image"][:500]...)
	invalid = append(invalid, foreignID)
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": Row{"images": invalid}}, 400)
	invalid[500] = assets["image"][0]
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": Row{"images": invalid}}, 400)
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": Row{"images": assets["video"]}}, 400)

	limited := object(jsonText(config))
	fields := array(limited["parameters"])
	object(fields[0])["max"] = 2
	object(fields[1])["max"] = 1
	object(fields[2])["max"] = 0
	object(fields[3])["min"] = 0
	object(fields[3])["max"] = 10
	limited["parameters"] = fields
	admin.ok(t, "PUT", "/admin/models/reference-limits", limited, 200)
	valid := Row{"images": assets["image"][:2], "videos": assets["video"][:1], "audio": []string{}, "seed": 10}
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": valid}, 200)
	for _, bad := range []Row{{"images": assets["image"][:3]}, {"videos": assets["video"][:2]}, {"audio": assets["audio"][:1]}, {"seed": -1}, {"seed": 11}} {
		user.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": bad}, 400)
		user.ok(t, "POST", "/jobs", Row{"modelId": model["id"], "prompt": "Blocked by configuration", "params": bad}, 400)
	}
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], balance)
	key := object(user.ok(t, "POST", "/keys", Row{"name": "reference-limit-test"}, 201))
	caller := &testClient{server: server.URL, key: str(key["token"])}
	caller.ok(t, "POST", "/jobs", Row{"modelId": model["id"], "prompt": "API also checks limits", "params": Row{"audio": assets["audio"][:1]}}, 400)
	caller.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": valid}, 200)
	for _, bound := range []any{-1, 1.5, "2"} {
		bad := object(jsonText(limited))
		object(array(bad["parameters"])[0])["max"] = bound
		admin.ok(t, "PUT", "/admin/models/reference-limits", bad, 400)
	}
	bad := object(jsonText(limited))
	object(array(bad["parameters"])[2])["required"] = true
	admin.ok(t, "PUT", "/admin/models/reference-limits", bad, 400)
	bad = object(jsonText(limited))
	object(array(bad["parameters"])[0])["min"] = 3
	admin.ok(t, "PUT", "/admin/models/reference-limits", bad, 400)
	// Removing a saved limit restores unlimited references instead of falling back to 9.
	admin.ok(t, "PUT", "/admin/models/reference-limits", config, 200)
	caller.ok(t, "POST", "/pricing/quote", Row{"modelId": model["id"], "params": params}, 200)
}
