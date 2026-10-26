package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	assets "phantomframe/server"
)

func inventoryTestWAV() []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+160))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint32(8000))
	_ = binary.Write(&b, binary.LittleEndian, uint32(16000))
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(160))
	b.Write(make([]byte, 160))
	return b.Bytes()
}

func TestInventorySupplierHTTPContracts(t *testing.T) {
	a := setupIntegration(t)
	user := a.Store.One("SELECT * FROM users WHERE username='test-admin'")
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
	type contract struct{ vendor, model, kind, path, shape string }
	cases := []contract{
		{"afrifex", "seedance-2-afti", "video", "/v1/videos", "async"},
		{"ALI", "qwen-image-3.0-pro", "image", "/services/aigc/multimodal-generation/generation", "ali"},
		{"APIYI", "gpt-5.4", "text", "/v1/responses", "responses"}, {"APIYI", "gpt-image-2-APIYI", "image", "/v1/images/generations", "image"},
		{"atlascloud", "midjourney-v8.2", "image", "/api/v1/model/generateImage", "async"},
		{"bytedance", "seedance-2-bytedance", "video", "/contents/generations/tasks", "async"}, {"bytedance", "seedream-5.0-pro-bytedance", "image", "/images/generations", "image"},
		{"ccgo", "gpt-5.4", "text", "/v1/responses", "responses"}, {"ccgo", "gpt-image-2.5", "image", "/v1/images/generations", "image"}, {"ccgo", "nano-banana-2", "image", "/v1beta/models/gemini-3.1-flash-image:generateContent", "gemini"},
		{"closeAI", "gpt-image-2-CA", "image", "/images/generations", "image"},
		{"copp", "seedance-v2", "video", "/openapi/v1/generations", "async"},
		{"dimensio", "gpt_image-2-dimesio", "image", "/v1/images/generations", "image"}, {"dimensio", "wan3.0-video", "video", "/v1/videos/generations", "async"},
		{"dlapi", "gpt-5.4", "text", "/v1/responses", "responses"}, {"dlapi", "gpt-image-2", "image", "/v1/images/generations", "image"},
		{"dsas", "xh-sdas-pro-720p", "video", "/v1/video/generations", "async"},
		{"huanwangai", "midjourney", "image", "/mj/submit/imagine", "mj"},
		{"infotoken", "gpt-image-2", "image", "/v1/images/generations", "image"}, {"infotoken", "seedance-v2", "video", "/v1/contents/generations/tasks", "async"},
		{"kinovi", "midjourney-v8", "image", "/api/v1/jobs/createTask", "kinovi"}, {"kinovi", "seedance-v2", "video", "/api/v1/jobs/createTask", "kinovi"}, {"kinovi", "suno-v5-5-mxapi", "audio", "/api/v1/jobs/createTask", "kinovi"},
		{"LD", "gpt-image-2-LD", "image", "/images/generations", "image"},
		{"midjourney-api", "midjourney-v8", "image", "/midjourney/v1/submit-jobs", "mj-api"}, {"midjourney-api", "gpt-image-2.5", "image", "/gpt/v1/generations", "mj-api"},
		{"MikotoPro", "gpt-6-astra", "text", "/v1/chat/completions", "chat"}, {"MikotoPro", "gpt-image-2-Mk", "image", "/v1/images/generations", "image"}, {"MikotoPro", "wan3.0-video", "video", "/v1/videos", "async"},
		{"mxapi", "suno-v5-5-mxapi", "audio", "/api/v2/music/generate", "mxapi"},
		{"MYAPI", "gpt-5.4", "text", "/v1/responses", "responses"}, {"MYAPI", "gpt-image-2", "image", "/v1/images/generations", "image"},
		{"MYNEWAPI", "DeepSeek-V3.2", "text", "/v1/chat/completions", "chat"}, {"MYNEWAPI", "gpt-image-2", "image", "/v1/images/generations", "image"}, {"MYNEWAPI", "nano-banana-2", "image", "/v1beta/models/nano-banana-2:generateContent", "gemini"},
		{"official-openai", "gpt-image-2", "image", "/v1/images/generations", "image"},
		{"openrouter", "gpt-image-2-APIYI", "image", "/v1/images", "image"}, {"openrouter", "grok-imagine-video", "video", "/v1/videos", "async"}, {"openrouter", "tts-gemini-openrouter", "audio", "/v1/audio/speech", "binary"}, {"openrouter", "lyria-3-openrouter", "audio", "/v1/chat/completions", "music"},
		{"rolldek", "gpt-image-2.5", "image", "/v1/images/generations", "image"}, {"rolldek", "wan3.0-video", "video", "/v1/videos", "async"},
		{"runninghub", "gpt-image-2", "image", "/openapi/v2/rhart-image-g-2-official/text-to-image", "runninghub"},
		{"runninghub-upscale", "rh-upscale", "upscale", "/openapi/v2/rhart-video/rh-upscale/enhance-frame", "runninghub"},
		{"runware", "gpt-image-2-official", "image", "/", "runware"}, {"runware", "seedance-2-runware", "video", "/", "runware"},
		{"secure-skill", "gpt-image-2", "image", "/v1/images/async/generations", "async"}, {"secure-skill", "grok-imagine-video", "video", "/openai/v1/videos", "async"},
		{"secure-skill-out", "jimeng-video-seedance-2.0-vip", "video", "/api/generate-video", "async"}, {"secure-skill-sepcial", "jimeng-video-seedance-2.0-vip", "video", "/api/generate-video", "async"},
		{"uniart", "jimeng-video-seedance-2.0-vip", "video", "/v1/videos", "async"},
		{"wavespeed", "wavespeed-ai/video-upscaler", "upscale", "/api/v3/wavespeed-ai/video-upscaler", "async"},
		{"wxart", "jimeng-video-seedance-2.0-vip", "video", "/v1/videos", "async"},
		{"zhubo", "gpt-image-2.5", "image", "/images/generations", "image"}, {"zhubo", "zhubo_video", "video", "/videos", "async"},
		{"zzshu", "gpt-6-astra", "text", "/v1/chat/completions", "chat"}, {"zzshu", "gpt-image-2", "image", "/v1/images/generations", "image"},
	}
	fixture, _, e := ReadProviderInventory(strings.NewReader(inventoryFixtureCSV(t)))
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range cases {
		t.Run(tc.vendor+"/"+tc.model, func(t *testing.T) {
			var route InventoryRoute
			for _, r := range fixture {
				if r.Vendor == tc.vendor && r.Model == tc.model {
					route = r
					break
				}
			}
			if route.ID == "" {
				t.Fatal("missing real inventory contract")
			}
			key := "local-contract-key-" + route.ID
			var submits, polls atomic.Int32
			var address string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "local-contract-request")
				if r.URL.Path == "/files" {
					send(w, 200, Row{"url": "https://reference.invalid/video.mp4"})
					return
				}
				expectedHeader := inventoryAuth(tc.vendor)
				if tc.vendor == "MYNEWAPI" && tc.shape == "gemini" {
					expectedHeader = "x-goog-api-key"
				}
				expectedKey := key
				if expectedHeader == "Authorization" {
					expectedKey = "Bearer " + key
				}
				if strings.HasPrefix(r.URL.Path, "/result") { // public output URL on the same simulated origin
					media := png
					mt := "image/png"
					if tc.kind == "audio" {
						media = inventoryTestWAV()
						mt = "audio/wav"
					}
					if tc.kind == "video" || tc.kind == "upscale" {
						media = assets.DemoVideo
						mt = "video/webm"
					}
					w.Header().Set("Content-Type", mt)
					_, _ = w.Write(media)
					return
				}
				if r.Header.Get(expectedHeader) != expectedKey {
					t.Errorf("incorrect route-scoped authentication header %s", expectedHeader)
				}
				var body any
				if strings.Contains(r.Header.Get("Content-Type"), "multipart/") {
					if e := r.ParseMultipartForm(1 << 20); e != nil {
						t.Error(e)
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("model") == "" {
						t.Error("form lost model")
					}
				} else if r.Method == "POST" {
					_ = json.NewDecoder(r.Body).Decode(&body)
				}
				isSubmit := r.URL.Path == tc.path && !(tc.shape == "runware" && str(object(array(body)[0])["taskType"]) == "getResponse")
				if isSubmit {
					submits.Add(1)
					if r.Method != "POST" {
						t.Error("submit method must be POST")
					}
					if strings.Contains(jsonText(body), key) {
						t.Error("credential appeared in request body")
					}
					switch tc.shape {
					case "responses":
						want(t, integer(object(body)["max_output_tokens"]), 2048)
						send(w, 200, Row{"output": []Row{{"content": []Row{{"type": "output_text", "text": "本地模拟文本"}}}}})
						return
					case "chat":
						want(t, integer(object(body)["max_tokens"]), 2048)
						send(w, 200, Row{"choices": []Row{{"message": Row{"content": "本地模拟文本"}}}})
						return
					case "binary":
						w.Header().Set("Content-Type", "audio/wav")
						_, _ = w.Write(inventoryTestWAV())
						return
					case "music":
						send(w, 200, Row{"choices": []Row{{"message": Row{"audio": Row{"data": base64.StdEncoding.EncodeToString(inventoryTestWAV())}}}}})
						return
					case "image":
						send(w, 200, Row{"data": []Row{{"b64_json": base64.StdEncoding.EncodeToString(png)}}})
						return
					case "ali":
						send(w, 200, Row{"output": Row{"choices": []Row{{"message": Row{"content": []Row{{"image": address + "/result"}}}}}}})
						return
					case "gemini":
						send(w, 200, Row{"candidates": []Row{{"content": Row{"parts": []Row{{"inlineData": Row{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(png)}}}}}}})
						return
					case "mxapi":
						send(w, 200, Row{"code": 200, "data": Row{"task_ids": []string{"track-a", "track-b"}}})
						return
					case "mj":
						send(w, 200, Row{"code": 1, "result": "native-task"})
						return
					case "mj-api":
						send(w, 200, Row{"code": 0, "data": Row{"taskId": "native-task"}})
						return
					case "kinovi":
						send(w, 200, Row{"code": 200, "data": Row{"taskId": "native-task", "state": "waiting", "error": Row{"code": 0, "message": nil}}})
						return
					case "runninghub":
						send(w, 200, Row{"taskId": "native-task", "status": "QUEUED"})
						return
					case "runware":
						send(w, 200, Row{"data": []Row{{"taskUUID": "native-task", "status": "processing"}}})
						return
					default:
						send(w, 200, Row{"id": "native-task", "status": "queued"})
						return
					}
				}
				polls.Add(1)
				switch tc.shape {
				case "mj-api":
					want(t, r.Method, "POST")
					want(t, str(array(object(body)["taskIds"])[0]), "native-task")
					send(w, 200, Row{"data": []Row{{"taskId": "native-task", "status": 1, "imageUrl": address + "/result"}}})
				case "runninghub":
					want(t, r.Method, "POST")
					want(t, object(body)["taskId"], "native-task")
					send(w, 200, Row{"taskId": "native-task", "status": "SUCCESS", "results": []Row{{"url": address + "/result"}}})
				case "runware":
					want(t, r.Method, "POST")
					want(t, object(array(body)[0])["taskType"], "getResponse")
					send(w, 200, Row{"data": []Row{{"taskUUID": "native-task", "status": "success", "videoURL": address + "/result"}}})
				case "kinovi":
					want(t, r.URL.Query().Get("taskId"), "native-task")
					send(w, 200, Row{"code": 200, "data": Row{"taskId": "native-task", "state": "success", "resultJson": jsonText(Row{"resultUrls": []string{"/result"}})}})
				case "mxapi":
					send(w, 200, Row{"code": 200, "data": Row{"status": "completed", "result": Row{"audio_url": address + "/result/" + r.URL.Query().Get("id")}}})
				default:
					send(w, 200, Row{"id": "native-task", "status": "SUCCESS", "imageUrl": address + "/result"})
				}
			}))
			defer server.Close()
			address = server.URL
			pid, jid := id(), id()
			params := Row{}
			for _, raw := range inventoryParameters(route) {
				p := object(raw)
				params[str(p["name"])] = p["default"]
				if p["type"] == "assets" {
					params[str(p["name"])] = []any{}
				}
			}
			if tc.kind == "upscale" {
				asset, e := a.saveAsset(str(user["id"]), "", "reference.webm", "video/webm", "webm", assets.DemoVideo)
				if e != nil {
					t.Fatal(e)
				}
				params["videoUrls"] = []any{asset["assetId"]}
			}
			a.Store.Exec("DELETE FROM providers WHERE id='inventory-test-upload'")
			a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES('inventory-test-upload','local upload','media-upload',?,?,1,'{}')", address, a.encrypt("local-upload-key"))
			cfg := clone(route.Config)
			cfg["upstreamModel"] = route.Upstream
			cfg["pollInterval"] = 10
			cfg["jobID"] = jid
			snapshot := Row{"id": pid, "name": tc.vendor, "protocol": "inventory", "base_url": address, "api_key": a.encrypt(key), "config": cfg, "modelName": tc.model, "upstreamModel": route.Upstream, "type": tc.kind, "parameters": inventoryParameters(route)}
			job := Row{"id": jid, "user_id": user["id"], "prompt": "local contract test", "params": params, "provider_snapshot": snapshot}
			// A normal durable job is required for task IDs, logs and cleanup ownership.
			a.Store.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,status,cost,outputs,provider_snapshot) VALUES(?,?,'nanobanana','local contract test',?,'running',0,'[]',?)", jid, user["id"], jsonText(params), jsonText(snapshot))
			a.archiveUpstreamCall(a.Store, a.Store.One("SELECT * FROM jobs WHERE id=?", jid), false)
			// Normalize the same JSON boundary used by persisted worker jobs.
			job["params"] = jsonText(params)
			job["provider_snapshot"] = jsonText(snapshot)
			ctx, cancel := context.WithTimeout(context.Background(), 3e9)
			defer cancel()
			outputs, e := a.inventoryUpstream(ctx, job)
			if e != nil {
				t.Fatal(e)
			}
			expected := 1
			if tc.shape == "mxapi" {
				expected = 2
			}
			want(t, len(outputs), expected)
			want(t, submits.Load(), 1)
			if tc.shape == "async" || tc.shape == "mxapi" || tc.shape == "kinovi" || tc.shape == "runninghub" || tc.shape == "runware" || tc.shape == "mj" || tc.shape == "mj-api" {
				if polls.Load() < 1 {
					t.Fatal("asynchronous protocol was not polled")
				}
			}
			for _, output := range outputs {
				stored := a.Store.One("SELECT * FROM assets WHERE id=?", output["assetId"])
				kind := tc.kind
				if kind == "upscale" {
					kind = "video"
				}
				want(t, stored["type"], kind)
			}
			if strings.Contains(jsonText(a.Store.One("SELECT * FROM upstream_calls WHERE id=?", jid)), key) {
				t.Fatal("upstream log leaked route key")
			}
		})
	}
}
