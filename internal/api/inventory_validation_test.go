package api

import (
	"encoding/json"
	"testing"
)

func TestInventoryExplicitProviderErrors(t *testing.T) {
	for _, body := range []Row{{"errors": []any{Row{"code": "invalidInput"}}}, {"success": false}, {"error": Row{"message": "private upstream detail"}}, {"code": json.Number("1007")}, {"code": "InvalidParameter"}} {
		if !inventoryEnvelopeError("runware", body) {
			t.Fatal("explicit supplier error accepted as pending")
		}
	}
	if inventoryEnvelopeError("huanwangai", Row{"code": json.Number("1"), "result": "task"}) {
		t.Fatal("Midjourney accepted response rejected")
	}
	for _, body := range []Row{{"error": Row{}}, {"error": Row{"code": json.Number("0"), "message": nil}}, {"data": Row{"error": Row{"code": "200", "message": ""}}}} {
		if inventoryEnvelopeError("kinovi", body) {
			t.Fatal("empty error envelope rejected normal pending task")
		}
	}
	if !inventoryEnvelopeError("kinovi", Row{"data": Row{"error": Row{"message": "failure"}}}) {
		t.Fatal("nested error missed")
	}
}

func TestInventoryNestedResultsAndTaskIsolation(t *testing.T) {
	value := object(`{"data":{"resultJson":"{\"resultUrls\":[\"/result\",\"https://output.invalid/video\"]}"}}`)
	want(t, len(inventoryOutputs(value, "video")), 2)
	value = object(`{"output":[{"mediaType":"image","url":"https://output.invalid/cover"},{"mediaType":"video","url":"https://output.invalid/video"}]}`)
	want(t, len(inventoryOutputs(value, "video")), 1)
	want(t, object(inventoryOutputs(value, "video")[0])["url"], "https://output.invalid/video")
	want(t, object(inventoryPollValue("kinovi", "mine", object(`{"data":{"id":"other","status":"success"}}`)))["status"], "processing")
	want(t, inventoryFailed("fail"), true)
	want(t, inventoryFailed("cancel"), true)
}

func TestInventoryNativeParametersReachSupplier(t *testing.T) {
	for _, c := range []struct {
		vendor, source, model, kind string
		params                      Row
	}{
		{"runware", "gpt-image-2-official", "openai:gpt-image@2", "image", Row{"resolution": "4k", "aspectRatio": "1:1", "quality": "high"}},
		{"runware", "seedance-2-fast-runware", "bytedance:seedance@2.0-fast", "video", Row{"resolution": "480p", "aspectRatio": "1:1", "duration": 5}},
		{"atlascloud", "midjourney-v8.2", "midjourney-v8.2", "image", Row{"resolution": "2k", "aspectRatio": "1:1"}},
		{"midjourney-api", "midjourney-v8.2", "8.2", "image", Row{"resolution": "2k", "aspectRatio": "16:9"}},
	} {
		p := Row{"type": c.kind, "config": Row{"vendor": c.vendor, "sourceModel": c.source, "upstreamModel": c.model}}
		refs := map[string][]string{}
		if c.vendor == "runware" && c.kind == "image" {
			refs["image"] = []string{"https://reference.invalid/image"}
		}
		call, _, _, err := inventoryPlan(Row{"id": "local", "prompt": "test", "params": c.params}, p, refs, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.vendor == "runware" {
			task := call.Body.([]Row)[0]
			if c.kind == "image" {
				want(t, task["width"], 2880)
				want(t, object(object(task["providerSettings"])["openai"])["quality"], "high")
				want(t, len(object(task["inputs"])["referenceImages"].([]string)), 1)
			} else {
				want(t, task["width"], 640)
				want(t, task["height"], 640)
			}
		}
		if c.vendor == "atlascloud" {
			want(t, object(call.Body)["hd"], true)
		}
		if c.vendor == "midjourney-api" {
			want(t, object(call.Body)["prompt"], "test --ar 16:9 --v 8.2 --hd")
			_, _, _, err = inventoryPlan(Row{"prompt": "test --hd", "params": c.params}, p, nil, nil)
			if err == nil {
				t.Fatal("prompt bypassed selected retail parameters")
			}
		}
	}
}

func TestInventoryNativeModelsAndSpecialFields(t *testing.T) {
	for _, c := range []struct{ vendor, source, want string }{
		{"APIYI", "gpt-image-2-vip-APIYI", "gpt-image-2-vip"},
		{"dimensio", "gpt_image-2-dimesio", "vxp-gpt-image-2"},
		{"dimensio", "jimeng-video-seedance-2.0-vip-Dimensio", "jimeng-video-seedance-2.0-vip"},
		{"ccgo", "nano-banana-2", "gemini-3.1-flash-image"},
		{"MYNEWAPI", "nano-banana-2", "nano-banana-2"},
		{"openrouter", "grok-imagine-video-1.5-preview", "x-ai/grok-imagine-video-1.5"},
	} {
		want(t, inventoryDefaultModel(c.vendor, c.source, Row{}), c.want)
	}
	p := Row{"type": "image", "config": Row{"vendor": "APIYI", "sourceModel": "gpt-image-2-vip-APIYI", "upstreamModel": "gpt-image-2-vip"}}
	call, _, _, e := inventoryPlan(Row{"id": "local", "prompt": "fixture", "params": Row{"quality": "high", "resolution": "1k", "aspectRatio": "1:1"}}, p, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := object(call.Body)["quality"]; ok {
		t.Fatal("APIYI VIP must not receive quality")
	}
	p = Row{"type": "upscale", "config": Row{"vendor": "runninghub-upscale", "sourceModel": "rh-upscale", "upstreamModel": "rh-upscale"}}
	call, _, _, e = inventoryPlan(Row{"id": "local", "prompt": "fixture", "params": Row{"fps": "2x", "resolution": "1080p"}}, p, map[string][]string{"video": {"https://reference.invalid/video.webm"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	want(t, object(call.Body)["model"], "max")
	want(t, object(call.Body)["outputFps"], "2x")
}
