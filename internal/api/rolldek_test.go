package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	assets "phantomframe/server"
	"strings"
	"testing"
)

func TestRolldekCatalog(t *testing.T) {
	for id, raw := range rolldekCatalog {
		t.Run(id, func(t *testing.T) {
			kind, params, ok := vendorCatalogEntry("rolldek", Row{"id": id}, Row{})
			want(t, ok, true)
			want(t, kind, object(raw)["kind"])
			validateParameters(array(jsonText(params)))
			if documentedModelDescription("rolldek", id, kind, params) == "" {
				t.Fatal("missing model description")
			}
		})
	}
	for _, id := range []string{"claude-fable-5", "gpt-5", "gemini-3-pro", "sd-2.5-ch8", "undocumented-image-model"} {
		_, _, ok := vendorCatalogEntry("rolldek", Row{"id": id}, Row{})
		want(t, ok, false)
	}
}

func TestRolldekAdapters(t *testing.T) {
	a := setupIntegration(t)
	local := httptest.NewServer(a.Handler())
	defer local.Close()
	admin := &testClient{server: local.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	uid := str(object(admin.ok(t, "GET", "/me", nil, 200))["id"])
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
	png, _ := base64.StdEncoding.DecodeString(encoded)
	images := []any{}
	for i := 0; i < 2; i++ {
		asset, e := a.saveAsset(uid, "", "image.png", "image/png", "png", png)
		if e != nil {
			t.Fatal(e)
		}
		images = append(images, asset["assetId"])
	}
	video, e := a.saveAsset(uid, "", "video.webm", "video/webm", "webm", assets.DemoVideo)
	if e != nil {
		t.Fatal(e)
	}
	audio, e := a.saveAsset(uid, "", "audio.wav", "audio/wav", "wav", []byte("RIFF0000WAVE"))
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name, model                              string
		params                                   Row
		frames, outputs                          int
		multipart, gemini, ark, fallback, failed bool
	}{
		{name: "one image per request", model: "gpt-image-2", params: Row{"n": 1, "size": "1024x1536"}, outputs: 1},
		{name: "image edits", model: "gpt-image-2-high", params: Row{"imageUrls": images}, frames: 2, outputs: 1, multipart: true},
		{name: "image 2.5 fixed pixels", model: "gpt-image-2.5-sunburst", params: Row{"resolution": "4K", "aspectRatio": "1:1", "quality": "max", "imageUrls": images[:1]}, frames: 1, outputs: 1, multipart: true},
		{name: "image 2.5 no n", model: "gpt-image-2.5", params: Row{"aspectRatio": "16:9"}, outputs: 1},
		{name: "gemini native", model: "gemini-nano-banana-2.1", params: Row{"resolution": "4K", "thinkingLevel": "high", "imageUrls": images}, frames: 2, outputs: 1, gemini: true},
		{name: "seedream references", model: "seedream-5.0", params: Row{"resolution": "2k", "aspectRatio": "4:3", "n": 1, "imageUrls": images[:1]}, frames: 1, outputs: 1},
		{name: "sd multimodal", model: "sd-2.0-ch1", params: Row{"imageUrls": images[:1], "videoUrls": []any{video["assetId"]}, "audioUrls": []any{audio["assetId"]}}, frames: 3, outputs: 1},
		{name: "sd first last", model: "sd-2.5-ch2", params: Row{"function_mode": "first_last_frames", "imageUrls": images}, frames: 2, outputs: 1},
		{name: "wan reference objects", model: "wan3.0-video-720p", params: Row{"imageUrls": images[:1], "videoUrls": []any{video["assetId"]}, "audioUrls": []any{audio["assetId"]}}, frames: 3, outputs: 1},
		{name: "ark native", model: "doubao-seedance-2-5-260628", params: Row{"imageUrls": images[:1]}, frames: 1, outputs: 1, ark: true},
		{name: "grok content fallback", model: "grok-imagine-video-1.5", params: Row{"imageUrls": images[:1]}, frames: 1, outputs: 1, fallback: true},
		{name: "terminal failure", model: "veo-3.1-fast", params: Row{}, failed: true},
	}
	for index, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			var body Row
			submitted, polled, uploaded := 0, 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/storage/") {
					want(t, r.Header.Get("Authorization"), "Bearer storage-key")
					uploaded++
					json.NewEncoder(w).Encode(Row{"data": Row{"fileUrl": "https://cdn.example.com/ref-" + str(uploaded) + ".png"}})
					return
				}
				want(t, r.Header.Get("Authorization"), "Bearer roll-key")
				if strings.HasSuffix(r.URL.Path, "/content") {
					w.Header().Set("Content-Type", "video/webm")
					w.Write(assets.DemoVideo)
					return
				}
				if r.Method == "POST" {
					submitted++
					if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "pf-") {
						t.Error("missing stable idempotency key")
					}
					body = Row{}
					if scenario.multipart {
						if e := r.ParseMultipartForm(5 << 20); e != nil {
							t.Fatal(e)
						}
						defer r.MultipartForm.RemoveAll()
						for k, v := range r.MultipartForm.Value {
							body[k] = v[0]
						}
						want(t, len(r.MultipartForm.File["image[]"]), scenario.frames)
						want(t, r.URL.Path, "/v1/images/edits")
					} else {
						if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
							t.Fatal(e)
						}
					}
					if scenario.gemini {
						want(t, r.URL.Path, "/v1beta/models/"+scenario.model+":generateContent")
						want(t, vendorValue(body, "generationConfig.responseFormat.image.imageSize"), "4K")
						want(t, vendorValue(body, "generationConfig.thinkingConfig.thinkingLevel"), "high")
						want(t, len(array(vendorValue(body, "contents.0.parts"))), 3)
						json.NewEncoder(w).Encode(Row{"candidates": []Row{{"content": Row{"parts": []Row{{"thought": true, "inlineData": Row{"data": "invalid preview"}}, {"inlineData": Row{"data": encoded, "mimeType": "image/png"}}}}}}})
						return
					}
					kind := rolldekDefinition(scenario.model)["kind"]
					if kind == "image" {
						if !scenario.multipart {
							want(t, r.URL.Path, "/v1/images/generations")
						}
						outputs := []Row{}
						for i := 0; i < scenario.outputs; i++ {
							outputs = append(outputs, Row{"b64_json": encoded})
						}
						json.NewEncoder(w).Encode(Row{"data": outputs})
						return
					}
					if scenario.ark {
						want(t, r.URL.Path, "/api/v3/contents/generations/tasks")
						want(t, vendorValue(body, "content.1.type"), "image_url")
						want(t, body["ratio"], "16:9")
						if body["prompt"] != nil || body["aspect_ratio"] != nil {
							t.Error("mixed native Ark and generic fields")
						}
					} else {
						want(t, r.URL.Path, "/v1/videos")
					}
					json.NewEncoder(w).Encode(Row{"id": "local-task", "status": "queued"})
					return
				}
				polled++
				if polled == 1 {
					w.WriteHeader(503)
					io.WriteString(w, "temporarily unavailable")
					return
				}
				result := Row{"id": "local-task", "status": "completed"}
				if scenario.failed {
					result["status"] = "failed"
				} else if scenario.ark {
					result["status"] = "succeeded"
					result["content"] = Row{"video_url": "/v1/videos/local-task/content"}
				} else if !scenario.fallback {
					result["metadata"] = Row{"url": "/v1/videos/local-task/content"}
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer upstream.Close()
			storage := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "rolldek-upload-" + str(index), "protocol": "media-upload", "base_url": upstream.URL + "/storage", "apiKey": "storage-key", "config": Row{"uploadPath": "/files"}}, 201))
			provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "rolldek-" + str(index), "protocol": "rolldek", "base_url": upstream.URL + "/v1", "apiKey": "roll-key", "config": Row{"uploadProviderId": storage["id"], "pollInterval": 20}}, 201))
			kind, parameters, _ := vendorCatalogEntry("rolldek", Row{"id": scenario.model}, Row{})
			mid := "rolldek-test-" + str(index)
			admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": mid, "type": kind, "provider_id": provider["id"], "upstream_model": scenario.model, "parameters": parameters}, 201)
			admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "test", "unit": "request", "credits": 0}, 200)
			job := object(admin.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "参考图片1", "params": scenario.params}, 202))
			outputs, e := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
			if scenario.failed {
				if e == nil {
					t.Fatal("terminal failure was accepted")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				want(t, len(outputs), scenario.outputs)
			}
			want(t, submitted, 1)
			if kind == "video" {
				want(t, uploaded, scenario.frames)
				if polled < 2 {
					t.Fatal("query retries were not exercised")
				}
			} else {
				want(t, uploaded, 0)
			}
			switch scenario.model {
			case "gpt-image-2":
				want(t, body["n"], 1)
				want(t, body["quality"], "medium")
			case "gpt-image-2.5", "gpt-image-2.5-sunburst":
				if body["n"] != nil || body["response_format"] != nil {
					t.Error("unsupported image 2.5 fields")
				}
				if scenario.model == "gpt-image-2.5" {
					want(t, body["size"], "1280x720")
					want(t, body["quality"], nil)
				} else {
					want(t, body["size"], "2880x2880")
				}
			case "seedream-5.0":
				want(t, body["size"], "4:3")
				want(t, body["resolution"], "2k")
				if !strings.HasPrefix(str(array(body["images"])[0]), "data:image/png;base64,") {
					t.Error("missing reference data URL")
				}
			case "sd-2.0-ch1":
				want(t, len(array(body["image_refs"])), 1)
				want(t, len(array(body["video_refs"])), 1)
				want(t, len(array(body["audio_refs"])), 1)
				if !strings.Contains(str(body["prompt"]), "@Image1") {
					t.Error("reference prompt mapping missing")
				}
			case "sd-2.5-ch2":
				if body["first_image"] == nil || body["last_image"] == nil || body["image_refs"] != nil {
					t.Error("invalid first/last mapping")
				}
			case "wan3.0-video-720p":
				want(t, body["seconds"], "10")
				want(t, body["resolution"], "720P")
				want(t, body["duration"], nil)
				if vendorValue(body, "reference_images.0.url") == nil {
					t.Error("missing WAN reference objects")
				}
			}
			admin.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
		})
	}
}
