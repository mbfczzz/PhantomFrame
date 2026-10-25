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

func TestVendorAdapters(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "vendor-user", "password": "Vendor-pass-2026"}, 200))
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	reference, err := a.saveAsset(str(owner["id"]), "", "ref.png", "image/png", "png", png)
	if err != nil {
		t.Fatal(err)
	}
	for _, vendor := range []string{"openvideo", "suda", "modelhub", "gway", "catcoder", "wxart"} {
		for _, kind := range []string{"image", "video"} {
			t.Run(vendor+"_"+kind, func(t *testing.T) {
				submitted, polled, uploaded := 0, 0, 0
				var base string
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					key := "Bearer vendor-key"
					if strings.HasPrefix(r.URL.Path, "/storage/") {
						key = "Bearer storage-key"
					}
					if r.Header.Get("Authorization") != key {
						t.Error("credential crossed the supplier boundary")
						w.WriteHeader(401)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/uploads/" || r.URL.Path == "/v1/media/upload" || r.URL.Path == "/storage/files" {
						uploaded++
						if e := r.ParseMultipartForm(2 << 20); e != nil {
							t.Error(e)
						}
						if len(r.MultipartForm.File["file"]) != 1 {
							t.Error("missing upload file")
						}
						_ = json.NewEncoder(w).Encode(Row{"success": true, "url": "https://files.example/ref.png", "data": Row{"fileUrl": "https://files.example/ref.png", "url": "https://files.example/ref.png"}})
						return
					}
					if r.URL.Path == "/v1/result" {
						if kind == "video" {
							w.Header().Set("Content-Type", "video/webm")
							_, _ = w.Write(assets.DemoVideo)
						} else {
							w.Header().Set("Content-Type", "image/png")
							_, _ = w.Write(png)
						}
						return
					}
					if r.Method == "POST" {
						submitted++
						if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "pf-") {
							t.Error("missing stable idempotency key")
						}
						body := Row{}
						if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
							if e := r.ParseMultipartForm(2 << 20); e != nil {
								t.Error(e)
							}
							for name, values := range r.MultipartForm.Value {
								body[name] = values[0]
							}
							field := "image_file_1"
							if kind == "image" {
								field = "images"
								if vendor == "catcoder" {
									field = "image"
								}
								if vendor == "wxart" {
									field = "image[]"
								}
							}
							if len(r.MultipartForm.File[field]) != 1 {
								t.Errorf("missing reference multipart field %s", field)
							}
						} else {
							if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
								t.Error(e)
							}
						}
						want(t, body["model"], "upstream-model")
						switch vendor {
						case "openvideo":
							want(t, object(str(body["face"]))["enabled"], false)
							if kind == "video" {
								want(t, body["function_mode"], "omni_reference")
								want(t, body["video_resolution"], "720p")
							}
						case "modelhub":
							if kind == "video" {
								want(t, body["functionMode"], "omni_reference")
							}
						case "suda":
							payload, ok := object(body["metadata"])["payload"].(string)
							if !ok {
								t.Error("metadata.payload must be a JSON string")
							}
							want(t, len(array(object(payload)["imageUrls"])), 1)
							if kind == "video" {
								want(t, object(payload)["aspectRatio"], "16:9")
								if body["resolution"] != nil {
									t.Error("Suda resolution must not be outside payload")
								}
							} else {
								want(t, object(payload)["ratio"], "16:9")
								if body["duration"] != nil {
									t.Error("image request inherited video duration")
								}
							}
						case "gway":
							want(t, len(array(body["images"])), 1)
						case "catcoder":
							if kind == "video" {
								want(t, len(array(body["images"])), 1)
								want(t, body["aspect_ratio"], "16:9")
							}
						case "wxart":
							if kind == "video" {
								want(t, body["seconds"], 5)
								want(t, len(array(body["references"])), 1)
							}
						}
						if kind == "image" && (vendor == "modelhub" || vendor == "wxart") {
							_ = json.NewEncoder(w).Encode(Row{"data": []Row{{"b64_json": base64.StdEncoding.EncodeToString(png)}}})
							return
						}
						_ = json.NewEncoder(w).Encode(Row{"task_id": "local-task", "id": "upstream-id", "status": "queued"})
						return
					}
					polled++
					if vendor == "suda" && polled == 1 {
						_ = json.NewEncoder(w).Encode(Row{"code": "success", "data": Row{"status": "NOT_START", "data": Row{"state": "processing", "images": []string{"https://files.example/ref.png"}}}})
						return
					}
					result := Row{"status": "completed"}
					url := base + "/v1/result"
					switch vendor {
					case "openvideo":
						result = Row{"status": "success", "source_urls": []string{url}, "result_urls": []string{"https://preview.example/low.mp4"}}
					case "suda":
						result = Row{"code": "success", "data": Row{"status": "SUCCESS", "result_url": url}}
					case "modelhub":
						result["result"] = Row{"url": url}
					case "catcoder":
						result["status"] = "succeeded"
						result["download_url"] = "/v1/result"
					case "gway":
						result["result_urls"] = []string{url}
					case "wxart":
						result["video_url"] = url
					}
					if vendor == "wxart" && !strings.HasSuffix(r.URL.Path, "/local-task") {
						t.Error("must query local task_id instead of upstream id")
					}
					_ = json.NewEncoder(w).Encode(result)
				}))
				defer upstream.Close()
				base = upstream.URL
				storage := object(admin.ok(t, "POST", "/admin/providers", Row{"name": "Storage " + vendor + kind, "protocol": "media-upload", "base_url": base + "/storage", "apiKey": "storage-key", "config": Row{"uploadPath": "/files"}}, 201))
				provider := object(admin.ok(t, "POST", "/admin/providers", Row{"name": vendor + kind, "protocol": vendor, "base_url": base + "/v1", "apiKey": "vendor-key", "config": Row{"uploadProviderId": storage["id"], "uploadBaseURL": base + "/uploads", "pollInterval": 20}}, 201))
				mid := "vendor-" + vendor + "-" + kind
				defs := []Row{{"name": "imageUrls", "label": "参考图片", "type": "assets", "itemType": "image"}, {"name": "aspectRatio", "label": "比例", "type": "select", "default": "16:9", "options": []string{"16:9"}}, {"name": "resolution", "label": "分辨率", "type": "select", "default": "720p", "options": []string{"720p"}}}
				if kind == "video" {
					defs = append(defs, Row{"name": "duration", "label": "时长", "type": "number", "default": 5})
				}
				admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": mid, "type": kind, "provider_id": provider["id"], "upstream_model": "upstream-model", "parameters": defs}, 201)
				if kind == "video" {
					entry := a.Store.One("SELECT id FROM provider_models WHERE provider_id=? AND upstream_model='upstream-model'", provider["id"])
					admin.ok(t, "PUT", "/admin/providers/"+str(provider["id"])+"/models/"+str(entry["id"])+"/key", Row{"apiKey": "vendor-key"}, 200)
					a.Store.Exec("UPDATE providers SET api_key=? WHERE id=?", a.encrypt("unused-channel-default"), provider["id"])
				}
				admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "test", "credits": 0, "unit": "request"}, 200)
				job := object(user.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "参考图片1", "params": Row{"imageUrls": []string{str(reference["assetId"])}}}, 202))
				outputs, e := a.upstream(context.Background(), a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
				if e != nil {
					t.Fatal(e)
				}
				want(t, len(outputs), 1)
				want(t, outputs[0]["type"], kind)
				want(t, submitted, 1)
				if !(kind == "image" && (vendor == "modelhub" || vendor == "wxart")) && polled == 0 {
					t.Fatal("async tasks must be polled")
				}
				if vendor == "suda" || vendor == "gway" || kind == "video" && (vendor == "catcoder" || vendor == "wxart") {
					want(t, uploaded, 1)
				}
				user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
			})
		}
	}
}
func TestVendorOutputCredentials(t *testing.T) {
	var received bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Header.Get("Authorization") != "" {
			t.Error("credential leaked")
		}
		_, _ = io.WriteString(w, "not media")
	}))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want(t, r.Header.Get("Authorization"), "Bearer local-key")
		http.Redirect(w, r, foreign.URL, http.StatusFound)
	}))
	defer origin.Close()
	_, err := downloadVendorOutput(context.Background(), origin.URL, origin.URL, "local-key")
	if err == nil {
		t.Fatal("redirect to private third-party host should be blocked")
	}
	if received {
		t.Fatal("private redirect was followed")
	}
}
func TestVendorCatalogTemplates(t *testing.T) {
	fixtures := map[string]Row{
		"openvideo": {"id": "fast", "type": "video", "caps": Row{"ratios": []any{"16:9"}, "res": []any{"720p"}, "dur": Row{"min": 4, "max": 12}, "modes": []any{"omni_reference"}}},
		"suda":      {"id": "sd-api-2-fast-720p"}, "gway": {"id": "seedance-2.5"}, "wxart": {"id": "seedance2.0"},
		"catcoder": {"id": "image-model", "kind": "image", "capabilities": Row{"max_images": 5, "max_videos": 0, "available": true}, "image_prices": Row{"1K/high": 1}},
		"modelhub": {"id": "seedance-2.0", "media_type": "video", "parameters": []any{Row{"name": "duration", "type": "number", "default": "5", "accepted_values": []any{"4", "5"}}}},
	}
	for vendor, row := range fixtures {
		t.Run(vendor, func(t *testing.T) {
			kind, params, ok := vendorCatalogEntry(vendor, row, Row{})
			if !ok || kind == "" {
				t.Fatal("missing model template")
			}
			validateParameters(array(jsonText(params)))
			if vendor == "suda" {
				for _, p := range params {
					if object(p)["name"] == "resolution" {
						t.Fatal("video resolution is encoded in Suda model id")
					}
				}
			}
		})
	}
}
