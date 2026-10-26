package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

//go:embed maitoken_catalog.json
var maiTokenCatalogJSON []byte
var maiTokenCatalog = func() Row { var doc Row; must(json.Unmarshal(maiTokenCatalogJSON, &doc)); return object(doc["models"]) }()

func maiTokenDefinition(model string) Row {
	value, ok := maiTokenCatalog[model]
	if !ok {
		return nil
	}
	return object(value)
}
func maiTokenOutputs(result Row) []any {
	if data, ok := result["data"].([]any); ok && len(data) > 0 {
		return data
	}
	for _, path := range []string{"result.url", "result_url", "video_url", "metadata.url", "url", "data.url"} {
		if address := str(vendorValue(result, path)); address != "" {
			return []any{address}
		}
	}
	return nil
}

func (a *App) maiTokenUpstream(ctx context.Context, j, p Row, key string, refs []Row) ([]Row, error) {
	model := str(p["upstreamModel"])
	definition := maiTokenDefinition(model)
	if definition == nil {
		return nil, fmt.Errorf("供应商 Mai-Token 此模型尚未完成协议核对")
	}
	kind := str(definition["kind"])
	if kind != str(p["type"]) {
		return nil, fmt.Errorf("供应商模型类型与接口配置不一致")
	}
	params := object(j["params"])
	body := Row{"contract_version": kind + "-v1", "model": model, "prompt": j["prompt"]}
	copyVendorFields(body, params, "resolution")
	if ratio := first(params["aspectRatio"], params["ratio"]); ratio != nil {
		body["ratio"] = ratio
	}
	path, pollPrefix, contentPrefix := "/videos", "/videos/", "/videos/"
	if kind == "image" {
		path = "/images/generations"
		pollPrefix = "/images/generations/"
		contentPrefix = "/images/"
		body["async"] = true
		body["n"] = 1
		for _, ref := range refs {
			if ref["type"] != "image" {
				return nil, fmt.Errorf("供应商图片模型仅接受参考图片")
			}
		}
	} else {
		body["duration"] = params["duration"]
	}
	frames := params["function_mode"] == "first_last_frames"
	if frames {
		if !truth(definition["frames"]) || len(refs) < 1 || len(refs) > 2 {
			return nil, fmt.Errorf("供应商当前模型不支持此首尾帧组合")
		}
		for _, ref := range refs {
			if ref["type"] != "image" {
				return nil, fmt.Errorf("供应商首尾帧不能混用音视频参考")
			}
		}
	}
	hosted, e := a.hostedReferences(ctx, p, key, refs)
	if e != nil {
		return nil, e
	}
	references := []Row{}
	for _, media := range []string{"image", "video", "audio"} {
		for index, address := range hosted[media] {
			role := "reference_" + media
			if frames && media == "image" {
				role = []string{"first_frame", "last_frame"}[index]
			}
			references = append(references, Row{"type": media, "role": role, "source": address})
		}
	}
	if len(references) > 0 {
		body["references"] = references
	}
	options := Row{}
	for _, name := range array(definition["optionNames"]) {
		if value, ok := params[str(name)]; ok {
			options[str(name)] = value
		}
	}
	if len(options) > 0 {
		body["options"] = options
	}
	base := str(p["base_url"])
	limit := int64(16 * 1024 * 1024)
	if kind == "image" {
		limit = 128 * 1024 * 1024
	}
	result, e := vendorHTTPBounded(ctx, base, key, path, strings.NewReader(jsonText(body)), "application/json", "pf-"+str(j["id"]), limit)
	if e != nil {
		return nil, e
	}
	taskID := str(first(result["task_id"], result["id"], vendorValue(result, "data.task_id"), vendorValue(result, "data.id")))
	state := vendorState("maitoken", result)
	if kind == "image" && taskID == "" && state == "" && result["error"] == nil && len(maiTokenOutputs(result)) > 0 {
		return a.saveProviderOutputs(ctx, j, maiTokenOutputs(result), base, key)
	}
	if taskID == "" || len(taskID) > 255 {
		return nil, fmt.Errorf("供应商未返回有效任务 ID，请核对上游记录")
	}
	a.setUpstreamTaskID(j["id"], taskID)
	interval := time.Duration(num(first(object(p["config"])["pollInterval"], 5000))) * time.Millisecond
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	for {
		switch vendorState("maitoken", result) {
		case "completed", "succeeded", "success":
			outputs := maiTokenOutputs(result)
			if len(outputs) == 0 {
				outputs = []any{strings.TrimRight(base, "/") + contentPrefix + url.PathEscape(taskID) + "/content"}
			}
			return a.saveProviderOutputs(ctx, j, outputs, base, key)
		case "failed", "failure", "error", "cancelled", "canceled", "expired", "refunded":
			return nil, fmt.Errorf("供应商 Mai-Token 任务失败，请核对原任务的错误信息")
		}
		if result["error"] != nil && vendorState("maitoken", result) == "" {
			return nil, fmt.Errorf("供应商 Mai-Token 返回查询错误，请保留任务 ID 核对")
		}
		if e = wait(ctx, interval); e != nil {
			return nil, e
		}
		for attempt := 0; attempt < 3; attempt++ {
			result, e = vendorHTTPBounded(ctx, base, key, pollPrefix+url.PathEscape(taskID), nil, "", "", limit)
			if e == nil {
				break
			}
			if waitErr := wait(ctx, interval); waitErr != nil {
				return nil, waitErr
			}
		}
		if e != nil {
			return nil, e
		}
	}
}
