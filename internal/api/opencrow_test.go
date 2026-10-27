package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	assets "phantomframe/server"
)

func openCrowFixture(model string) (Row, Row, map[string][]string, []Row) {
	params := Row{}
	refs := map[string][]string{}
	files := []Row{}
	for _, raw := range openCrowParameters(model) {
		p := object(raw)
		name := str(p["name"])
		params[name] = p["default"]
		if p["type"] == "assets" {
			params[name] = []any{}
			if truth(p["required"]) {
				refs[str(p["itemType"])] = []string{"https://reference.invalid/" + str(p["itemType"])}
				params[name] = []any{"owned-asset"}
			}
		} else if truth(p["required"]) && str(p["default"]) == "" {
			params[name] = "test-voice"
		}
	}
	p := Row{"type": openCrowDefinition(model)["kind"], "upstreamModel": model, "config": Row{"vendor": "opencrow", "sourceModel": model, "upstreamModel": model}}
	return params, p, refs, files
}

func TestOpenCrowCatalogAndParameterContracts(t *testing.T) {
	want(t, len(openCrowCatalog), 155)
	for model, spec := range openCrowCatalog {
		t.Run(model, func(t *testing.T) {
			kind, defs, ok := vendorCatalogEntry("opencrow", Row{"id": model}, Row{})
			want(t, ok, true)
			want(t, kind, spec["kind"])
			if err := try(func() { validateParameters(defs) }); err != nil {
				t.Fatal(err)
			}
			params, p, refs, files := openCrowFixture(model)
			base, poll, method, err := inventoryPlan(Row{"id": "stable", "prompt": "local", "params": params}, p, refs, files)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "video" {
				want(t, poll, "/v1/videos/generations/{id}")
				want(t, method, "GET")
				want(t, object(base.Body)["nonce"], "stable")
			}
			for _, raw := range defs {
				f := object(raw)
				name := str(f["name"])
				values := array(f["options"])
				if f["type"] == "number" {
					values = []any{f["min"], f["max"]}
				}
				if f["type"] == "text" {
					values = []any{"alternate-test-text"}
				}
				for _, v := range values {
					if o, ok := v.(map[string]any); ok {
						v = o["v"]
					}
					if v == nil || str(v) == str(params[name]) {
						continue
					}
					variant := clone(params)
					variant[name] = v
					call, _, _, err := inventoryPlan(Row{"id": "stable", "prompt": "local", "params": variant}, p, refs, files)
					if err == nil && jsonText(call) == jsonText(base) {
						t.Errorf("ignored %s=%v", name, v)
					}
				}
			}
		})
	}
	for _, model := range []string{"text-embedding-3-large", "unknown-future-model", "3d-model"} {
		_, _, ok := vendorCatalogEntry("opencrow", Row{"id": model}, Row{})
		want(t, ok, false)
	}
}

func TestOpenCrowNativeRequestAndResponseShapes(t *testing.T) {
	for _, model := range []string{"wan3.0-video", "wan2.7-i2v", "happyhorse-1.0-video-edit", "minimax-video-h3-max", "kling-video-v3-omni", "kling-video-v3-turbo", "gemini-3.1-flash-image-preview", "qwen-image-plus", "grok-imagine-image", "gpt-5.4"} {
		t.Run(model, func(t *testing.T) {
			params, p, refs, files := openCrowFixture(model)
			if strings.HasPrefix(model, "gemini-") {
				refs["image"] = []string{"data:image/png;base64,dGVzdA=="}
			}
			if model == "minimax-video-h3-max" {
				refs["image"] = []string{"https://reference.invalid/frame"}
			}
			call, _, _, err := inventoryPlan(Row{"id": "local", "prompt": "fixture", "params": params}, p, refs, files)
			if err != nil {
				t.Fatal(err)
			}
			body := object(call.Body)
			switch model {
			case "wan3.0-video":
				want(t, object(body["parameters"])["resolution"], "480P")
				want(t, object(body["input"])["prompt"], "fixture")
			case "wan2.7-i2v":
				want(t, object(body["input"])["img_url"], refs["image"][0])
			case "happyhorse-1.0-video-edit":
				want(t, object(body["input"])["video_url"], refs["video"][0])
				want(t, body["duration"], int64(5))
			case "minimax-video-h3-max":
				content := body["content"].([]Row)
				want(t, content[1]["role"], "first_frame")
				want(t, body["resolution"], "768P")
				params["resolution"] = "2K"
				if _, _, _, err = inventoryPlan(Row{"params": params}, p, refs, files); err == nil {
					t.Fatal("invalid Max resolution accepted")
				}
			case "kling-video-v3-omni":
				want(t, body["contents"].([]Row)[0]["type"], "prompt")
			case "kling-video-v3-turbo":
				if _, exists := object(body["settings"])["audio"]; exists {
					t.Fatal("turbo received unsupported audio")
				}
			case "gemini-3.1-flash-image-preview":
				want(t, body["input"].([]Row)[1]["data"], "dGVzdA==")
				want(t, object(body["response_format"])["image_size"], "1K")
			case "qwen-image-plus":
				want(t, object(body["input"])["prompt"], "fixture")
			case "grok-imagine-image":
				want(t, body["size"], "1024x1024")
			case "gpt-5.4":
				want(t, body["max_completion_tokens"], int64(2048))
				if body["max_tokens"] != nil {
					t.Fatal("reasoning model received deprecated token field")
				}
			}
		})
	}
	result := object(`{"content":[{"type":"image","mime_type":"image/png","data":"cover-must-not-be-video"},{"type":"last_frame_url","last_frame_url":"https://cdn.invalid/cover"},{"type":"video_url","video_url":{"url":"https://cdn.invalid/result","duration":5}}]}`)
	outputs := inventoryOutputs(result, "video")
	want(t, len(outputs), 1)
	want(t, object(outputs[0])["url"], "https://cdn.invalid/result")
	result = object(`{"steps":[{"type":"thought","content":[{"type":"image","mime_type":"image/png","data":"do-not-save"}]},{"type":"model_output","content":[{"type":"image","mime_type":"image/png","data":"test-result"}]}]}`)
	outputs = inventoryOutputs(result, "image")
	want(t, len(outputs), 1)
	want(t, object(outputs[0])["b64_json"], "test-result")
	want(t, inventoryFailed("timeout"), true)
}

func TestOpenCrowSyncPublishAndBilling(t *testing.T) {
	a := setupIntegration(t)
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	var base string
	var submitted atomic.Int32
	var reject atomic.Bool
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			send(w, 200, Row{"url": "https://reference.invalid/hosted"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer opencrow-test-only-key" {
			t.Error("route auth missing or changed")
		}
		if r.URL.Path == "/result" {
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write(assets.DemoVideo)
			return
		}
		if r.URL.Path == "/v1/models" {
			list := []Row{}
			for model := range openCrowCatalog {
				list = append(list, Row{"id": model})
			}
			list = append(list, Row{"id": "text-embedding-3-large"})
			send(w, 200, Row{"data": list})
			return
		}
		if r.Method == "GET" {
			send(w, 200, Row{"id": "task", "status": "succeeded", "content": []Row{{"type": "video_url", "video_url": Row{"url": base + "/result"}}}})
			return
		}
		submitted.Add(1)
		if reject.Load() {
			w.WriteHeader(503)
			return
		}
		var body Row
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			defer r.MultipartForm.RemoveAll()
			want(t, len(r.MultipartForm.File["image"]), 1)
			body = Row{"model": r.FormValue("model")}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			send(w, 200, Row{"choices": []Row{{"message": Row{"content": "local OpenCrow text"}}}})
		case "/v1/audio/speech":
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(inventoryTestWAV())
		case "/v1/videos/generations":
			want(t, str(body["nonce"]) != "", true)
			send(w, 200, Row{"id": "task", "status": "submitted"})
		default:
			if strings.HasPrefix(str(body["model"]), "gemini-") {
				send(w, 200, Row{"status": "completed", "steps": []Row{{"type": "model_output", "content": []Row{{"type": "image", "mime_type": "image/png", "data": png}}}}})
			} else {
				send(w, 200, Row{"data": []Row{{"b64_json": png}}})
			}
		}
	}))
	defer mock.Close()
	base = mock.URL
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	uid := str(u["id"])
	provider := object(c.ok(t, "POST", "/admin/providers", Row{"name": "OpenCrow mock", "protocol": "opencrow", "base_url": base, "apiKey": "opencrow-test-only-key", "config": Row{"pollInterval": 10}}, 201))
	pid := str(provider["id"])
	c.ok(t, "POST", "/admin/providers/"+pid+"/sync-models", Row{}, 200)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", pid)["n"], 155)
	c.ok(t, "POST", "/admin/providers/"+pid+"/sync-models", Row{}, 200)
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", pid)["n"], 155)
	// Non-demo exact-name prices are inherited only when the native schema fits.
	seedInventoryTestRoute(a, "APIYI", "gpt-5.4", "text", base, "source-fixture-key")
	models := []string{"gpt-5.4", "gpt-image-2", "gemini-3.1-flash-image-preview", "qwen-image-plus", "qwen-image-edit", "dola-seedream-5-0-pro-260628", "grok-imagine-image", "wan3.0-video", "dreamina-seedance-2-0-260128", "wan2.7-i2v", "wan2.6-r2v", "happyhorse-1.0-video-edit", "minimax-video-h3-max", "kling-video-v3-omni", "kling-video-v3-turbo", "qwen3-tts-flash"}
	imgBytes, _ := base64.StdEncoding.DecodeString(png)
	img, err := a.saveAsset(uid, "", "reference.png", "image/png", "png", imgBytes)
	if err != nil {
		t.Fatal(err)
	}
	vid, err := a.saveAsset(uid, "", "reference.webm", "video/webm", "webm", assets.DemoVideo)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := a.saveAsset(uid, "", "reference.wav", "audio/wav", "wav", inventoryTestWAV())
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES('opencrow-upload','mock upload','media-upload',?,?,1,'{}')", base, a.encrypt("mock-upload-key"))
	startBalance := num(u["balance"])
	for i, model := range models {
		t.Run(model, func(t *testing.T) {
			entry := a.Store.One("SELECT id FROM provider_models WHERE provider_id=? AND upstream_model=?", pid, model)
			mid := fmt.Sprintf("opencrow-model-%d", i)
			kind := str(openCrowDefinition(model)["kind"])
			published := object(c.ok(t, "POST", "/admin/providers/"+pid+"/models/"+str(entry["id"])+"/publish", Row{"modelId": mid, "name": model, "type": kind}, 201))
			m := object(published["model"])
			want(t, truth(m["enabled"]), false)
			if model == "gpt-5.4" {
				want(t, len(array(m["prices"])), 1)
			} else {
				want(t, len(array(m["prices"])), 0)
			}
			m["enabled"] = true
			c.ok(t, "PUT", "/admin/models/"+mid, m, 200)
			if model != "gpt-5.4" {
				c.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{}}, 400)
				c.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "mock price", "unit": "request", "credits": 0.25}, 200)
			}
			params := Row{}
			for _, raw := range openCrowParameters(model) {
				p := object(raw)
				if p["type"] == "assets" {
					aid := img["assetId"]
					if p["itemType"] == "video" {
						aid = vid["assetId"]
					}
					if p["itemType"] == "audio" {
						aid = audio["assetId"]
					}
					params[str(p["name"])] = []any{aid}
				}
			}
			job := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "mock request", "params": params, "expectedCost": 0.25}, 202))
			a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", job["id"])
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			a.processJob(ctx, a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
			finished := a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"])
			if finished["status"] != "succeeded" {
				t.Fatalf("job failed: %v", finished["error"])
			}
			want(t, len(array(finished["outputs"])), 1)
		})
	}
	want(t, num(object(c.ok(t, "GET", "/me", nil, 200))["balance"]), startBalance-float64(len(models))*.25)
	reject.Store(true)
	job := object(c.ok(t, "POST", "/jobs", Row{"modelId": "opencrow-model-0", "prompt": "refund failure"}, 202))
	a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", job["id"])
	a.processJob(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
	want(t, truth(a.Store.One("SELECT refunded FROM jobs WHERE id=?", job["id"])["refunded"]), true)
	want(t, num(object(c.ok(t, "GET", "/me", nil, 200))["balance"]), startBalance-float64(len(models))*.25)
	if strings.Contains(jsonText(c.ok(t, "GET", "/admin/upstream-calls", nil, 200)), "opencrow-test-only-key") {
		t.Fatal("credential leaked in logs")
	}
	want(t, submitted.Load(), int32(len(models)+1))
}
