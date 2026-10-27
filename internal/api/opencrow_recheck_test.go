package api

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Synthetic black frames generated locally, without any AI service.
//
//go:embed testdata/opencrow-motion-valid.mp4
var openCrowMotionVideo []byte

//go:embed testdata/opencrow-motion-small.mp4
var openCrowMotionSmallVideo []byte

//go:embed testdata/opencrow-motion-long.mp4
var openCrowMotionLongVideo []byte

func seedOpenCrowRecheck(a *App, model, base string) string {
	pid, mid := id(), id()
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,'local opencrow','opencrow',?,?,1,?)", pid, base, a.encrypt("opencrow-recheck-test-key"), jsonText(Row{"pollInterval": 10}))
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters) VALUES(?,?,?,?,?,'local fixture','test',1,'[]',?)", mid, model, openCrowDefinition(model)["kind"], pid, model, jsonText(openCrowParameters(model)))
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions) VALUES(?,?,'local fixture','request',25,250000,'{}')", id(), mid)
	return mid
}

func TestOpenCrowStaleSchemaAndReferenceTypesBlockBeforeDebit(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	uid := str(u["id"])
	video, err := a.saveAsset(uid, "", "valid.mp4", "video/mp4", "mp4", openCrowMotionVideo)
	if err != nil {
		t.Fatal(err)
	}
	before := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	for _, scenario := range []string{"missing-resolution", "missing-duration", "wrong-image-type", "blank-voice"} {
		t.Run(scenario, func(t *testing.T) {
			model := "wan3.0-video"
			if scenario == "wrong-image-type" {
				model = "gpt-image-2"
			}
			if scenario == "blank-voice" {
				model = "qwen3-tts-flash"
			}
			mid := seedOpenCrowRecheck(a, model, "http://127.0.0.1:9")
			defs := []any{}
			params := Row{}
			for _, raw := range openCrowParameters(model) {
				f := clone(object(raw))
				name := str(f["name"])
				if scenario == "missing-resolution" && name == "resolution" || scenario == "missing-duration" && name == "duration" {
					continue
				}
				if scenario == "wrong-image-type" && name == "imageUrls" {
					f["itemType"] = "video"
					params[name] = []any{video["assetId"]}
				}
				defs = append(defs, f)
			}
			if scenario == "blank-voice" {
				params["voice"] = " \t "
			}
			a.Store.Exec("UPDATE models SET parameters=? WHERE id=?", jsonText(defs), mid)
			c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "must reject before billing", "params": params}, 400)
			want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", mid)["n"], 0)
			want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), before)
		})
	}
}

func TestOpenCrowMotionMediaConstraints(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	uid := str(u["id"])
	save := func(name, mime, ext string, data []byte) any {
		v, err := a.saveAsset(uid, "", name, mime, ext, data)
		if err != nil {
			t.Fatal(err)
		}
		return v["assetId"]
	}
	makePNG := func(width, height int) []byte {
		var out bytes.Buffer
		if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	goodImage := save("valid.png", "image/png", "png", makePNG(360, 360))
	smallImage := save("small.png", "image/png", "png", makePNG(200, 200))
	wideImage := save("wide.png", "image/png", "png", makePNG(900, 300))
	wrongImage := save("wrong.gif", "image/gif", "gif", []byte("GIF89a"))
	goodVideo := save("valid.mp4", "video/mp4", "mp4", openCrowMotionVideo)
	smallVideo := save("small.mp4", "video/mp4", "mp4", openCrowMotionSmallVideo)
	longVideo := save("long.mp4", "video/mp4", "mp4", openCrowMotionLongVideo)
	wrongVideo := save("wrong.webm", "video/webm", "webm", openCrowMotionVideo)
	mid := seedOpenCrowRecheck(a, "kling-video-motion-control-v3.0", "http://127.0.0.1:9")
	before := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	for _, tc := range []struct {
		name        string
		img, vid    any
		orientation string
		status      int
	}{
		{"valid", goodImage, goodVideo, "image", 200}, {"small-image", smallImage, goodVideo, "image", 400}, {"wide-image", wideImage, goodVideo, "image", 400}, {"wrong-image-format", wrongImage, goodVideo, "image", 400}, {"small-video", goodImage, smallVideo, "image", 400}, {"wrong-video-format", goodImage, wrongVideo, "image", 400}, {"long-image-orientation", goodImage, longVideo, "image", 400}, {"long-video-orientation", goodImage, longVideo, "video", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := Row{"imageUrls": []any{tc.img}, "videoUrls": []any{tc.vid}, "characterOrientation": tc.orientation}
			c.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": params}, tc.status)
			if tc.status == 400 {
				c.ok(t, "POST", "/jobs", Row{"modelId": mid, "params": params, "prompt": "must reject invalid media"}, 400)
			}
		})
	}
	want(t, a.Store.One("SELECT count(*) n FROM jobs WHERE model_id=?", mid)["n"], 0)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), before)
}

func TestOpenCrowAuthoritativeTaskReceipts(t *testing.T) {
	for _, raw := range []string{
		`{"id":"task","status":"submitted"}`, `{"id":"task","status":"processing","model":"wan3.0-video"}`,
	} {
		_, ok := openCrowAcceptedErrorResponse([]byte(raw), "wan3.0-video")
		want(t, ok, true)
	}
	for _, raw := range []string{`{"id":"task"}`, `{"id":"task","status":"submitted","model":"other-model"}`, `{"id":"../escape","status":"submitted"}`, `{"id":"task","status":"submitted","error":"denied"}`, `{"id":"task","status":"submitted"} {"second":true}`} {
		_, ok := openCrowAcceptedErrorResponse([]byte(raw), "wan3.0-video")
		want(t, ok, false)
	}
	want(t, inventoryTaskIDs("opencrow", Row{"id": "public-id", "task_id": "native-id"})[0], "public-id")
	root := object(`{"id":"public-id","status":"succeeded","data":{"id":"native-id","outputs":[{"type":"video","url":"https://result.invalid/video"}]}}`)
	want(t, len(inventoryOutputs(openCrowPollValue("public-id", root), "video")), 1)
	wrong := object(`{"status":"succeeded","data":[{"id":"foreign","status":"failed","outputs":[{"url":"https://result.invalid/foreign"}]}]}`)
	want(t, openCrowPollValue("public-id", wrong)["status"], "processing")
	combined := object(`{"data":[{"id":"foreign","outputs":[{"url":"https://result.invalid/foreign"}]},{"id":"public-id","status":"succeeded","outputs":[{"url":"https://result.invalid/mine"}]}]}`)
	filtered := openCrowPollValue("public-id", combined)
	want(t, filtered["status"], "succeeded")
	want(t, len(inventoryOutputs(filtered, "video")), 1)
}

func TestOpenCrowAcceptedHTTPErrorKeepsOriginalTask(t *testing.T) {
	a := setupIntegration(t)
	var base string
	var submits, polls, downloads atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer opencrow-recheck-test-key" {
			t.Error("wrong route key")
		}
		if r.Method == "POST" {
			submits.Add(1)
			send(w, 503, Row{"id": "accepted-task", "task_id": "native-task", "model": "wan3.0-video", "status": "submitted"})
			return
		}
		if r.URL.Path == "/result" {
			downloads.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(openCrowMotionVideo)
			return
		}
		if r.URL.Path == "/foreign" {
			t.Error("foreign result downloaded")
			w.WriteHeader(500)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/accepted-task") {
			t.Error("polled the native ID instead of public receipt ID")
		}
		if polls.Add(1) == 1 {
			send(w, 200, Row{"status": "succeeded", "data": []Row{{"id": "foreign", "status": "succeeded", "outputs": []Row{{"type": "video", "url": base + "/foreign"}}}}})
			return
		}
		send(w, 200, Row{"data": []Row{{"id": "accepted-task", "status": "succeeded", "outputs": []Row{{"type": "video", "url": base + "/result"}}}}})
	}))
	defer mock.Close()
	base = mock.URL
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	u := object(c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200))
	mid := seedOpenCrowRecheck(a, "wan3.0-video", base)
	job := object(c.ok(t, "POST", "/jobs", Row{"modelId": mid, "prompt": "local accepted receipt"}, 202))
	a.Store.Exec("UPDATE jobs SET status='running' WHERE id=?", job["id"])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.processJob(ctx, a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"]))
	finished := a.Store.One("SELECT * FROM jobs WHERE id=?", job["id"])
	want(t, finished["status"], "succeeded")
	want(t, truth(finished["refunded"]), false)
	want(t, submits.Load(), 1)
	want(t, polls.Load(), 2)
	want(t, downloads.Load(), 1)
	want(t, num(object(c.ok(t, "GET", "/me", nil, 200))["balance"]), num(u["balance"])-.25)
}
