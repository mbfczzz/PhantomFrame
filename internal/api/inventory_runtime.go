package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

func inventoryObject(value any) Row {
	if list, ok := value.([]any); ok && len(list) > 0 {
		return object(list[0])
	}
	return object(value)
}
func inventoryTaskIDs(vendor string, value any) []string {
	root := inventoryObject(value)
	if vendor == "mxapi" {
		ids := array(vendorValue(root, "data.task_ids"))
		if len(ids) > 0 {
			out := []string{}
			for _, v := range ids {
				if id := str(v); id != "" {
					out = append(out, id)
				}
			}
			return out
		}
	}
	for _, path := range []string{"task_id", "taskId", "id", "data.taskId", "data.task_id", "data.id", "data.0.taskUUID", "data.0.taskId", "data.taskUUID", "taskUUID", "result"} {
		if v := vendorValue(root, path); v != nil {
			if s := inventoryID(v); s != "" && !strings.HasPrefix(s, "http") {
				return []string{s}
			}
		}
	}
	return nil
}
func inventoryID(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return string(v)
	}
	return ""
}
func inventoryPollValue(vendor, taskID string, value any) any {
	root := inventoryObject(value)
	if vendor == "midjourney-api" || vendor == "runware" {
		if len(array(root["data"])) == 0 && inventoryEnvelopeError(vendor, root) {
			return root
		}
		for _, raw := range array(root["data"]) {
			item := object(raw)
			if str(first(item["taskId"], item["taskUUID"])) == taskID {
				return item
			}
		}
		return Row{"status": "processing"}
	}
	for _, path := range []string{"id", "taskId", "task_id", "data.id", "data.taskId", "data.task_id"} {
		if returned := inventoryID(vendorValue(root, path)); returned != "" && returned != taskID {
			return Row{"status": "processing"}
		}
	}
	return root
}
func inventoryState(vendor string, value any) string {
	root := inventoryObject(value)
	status := str(first(root["status"], vendorValue(root, "data.status"), root["state"], vendorValue(root, "data.state"), vendorValue(root, "task.status")))
	if vendor == "midjourney-api" {
		if status == "1" {
			return "succeeded"
		}
		if status == "2" {
			return "failed"
		}
	}
	return strings.ToLower(strings.TrimSpace(status))
}
func inventoryFinished(status string) bool {
	return status == "completed" || status == "complete" || status == "succeeded" || status == "success" || status == "done" || status == "finished"
}
func inventoryFailed(status string) bool {
	return status == "fail" || status == "failed" || status == "failure" || status == "error" || status == "cancel" || status == "cancelled" || status == "canceled" || status == "rejected"
}

func inventoryMeaningfulError(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case map[string]any:
		for key, child := range v {
			if key == "code" && (str(child) == "0" || str(child) == "200" || strings.EqualFold(str(child), "success")) {
				continue
			}
			if inventoryMeaningfulError(child) {
				return true
			}
		}
		return false
	case []any:
		for _, child := range v {
			if inventoryMeaningfulError(child) {
				return true
			}
		}
		return false
	default:
		text := strings.TrimSpace(str(value))
		return text != "" && text != "0" && text != "false"
	}
}

func inventoryEnvelopeError(vendor string, value any) bool {
	root := inventoryObject(value)
	if root["success"] == false || len(array(root["errors"])) > 0 {
		return true
	}
	if inventoryMeaningfulError(root["error"]) || inventoryMeaningfulError(vendorValue(root, "data.error")) {
		return true
	}
	if code := root["code"]; code != nil && str(code) != "" {
		s := strings.ToLower(str(code))
		if s == "0" || s == "200" || s == "201" || s == "202" || s == "success" {
			return false
		}
		if vendor == "huanwangai" && (s == "1" || s == "21" || s == "22") {
			return false
		}
		return true
	}
	return false
}

func inventoryOutputs(value any, kinds ...string) []any {
	outputs := []any{}
	kind := ""
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	if kind == "upscale" {
		kind = "video"
	}
	seen := map[string]bool{}
	var visit func(any, int)
	visit = func(v any, depth int) {
		if depth > 8 || len(outputs) >= 10 {
			return
		}
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(x, "https://") || strings.HasPrefix(x, "http://") || strings.HasPrefix(x, "data:image/") || strings.HasPrefix(x, "data:audio/") || strings.HasPrefix(x, "/") && !strings.HasPrefix(x, "//") {
				if !seen[x] {
					outputs = append(outputs, Row{"url": x})
					seen[x] = true
				}
			}
		case []any:
			for _, item := range x {
				visit(item, depth+1)
			}
		case map[string]any:
			mediaType := str(first(x["mediaType"], x["type"]))
			if kind != "" && (strings.HasPrefix(mediaType, "image") || strings.HasPrefix(mediaType, "video") || strings.HasPrefix(mediaType, "audio")) && !strings.HasPrefix(mediaType, kind) {
				return
			}
			if encoded, ok := x["resultJson"].(string); ok && len(encoded) < 2*1024*1024 {
				var decoded any
				if json.Unmarshal([]byte(encoded), &decoded) == nil {
					visit(decoded, depth+1)
				}
			}
			if encoded, ok := x["b64_json"].(string); ok && encoded != "" {
				outputs = append(outputs, Row{"b64_json": encoded})
				return
			}
			if inline := object(first(x["inlineData"], x["inline_data"])); inline["data"] != nil {
				outputs = append(outputs, Row{"b64_json": inline["data"]})
				return
			}
			for _, key := range []string{"url", "video_url", "audio_url", "image_url", "image", "imageUrl", "videoUrl", "audioUrl", "imageURL", "videoURL", "audioURL", "result_url", "output_url", "download_url", "file_url"} {
				if candidate, ok := x[key].(string); ok {
					visit(candidate, depth+1)
				}
			}
			for _, key := range []string{"data", "output", "outputs", "result", "results", "images", "imageUrls", "image_urls", "urls", "resultUrls", "result_urls", "source_urls", "unsigned_urls", "content", "candidates", "parts", "response", "clips", "choices", "message", "audio"} {
				if nested, ok := x[key]; ok {
					visit(nested, depth+1)
				}
			}
		}
	}
	visit(value, 0)
	return outputs
}
func inventoryText(value any) string {
	root := inventoryObject(value)
	if text, ok := root["output_text"].(string); ok {
		return text
	}
	var pieces []string
	for _, item := range array(root["output"]) {
		for _, part := range array(object(item)["content"]) {
			if text, ok := object(part)["text"].(string); ok {
				pieces = append(pieces, text)
			}
		}
	}
	if len(pieces) > 0 {
		return strings.Join(pieces, "\n")
	}
	message := object(vendorValue(root, "choices.0.message"))
	if text, ok := message["content"].(string); ok {
		return text
	}
	for _, part := range array(message["content"]) {
		if text, ok := object(part)["text"].(string); ok {
			pieces = append(pieces, text)
		}
	}
	return strings.Join(pieces, "\n")
}

func (a *App) inventoryUpstream(ctx context.Context, j Row) ([]Row, error) {
	p := object(j["provider_snapshot"])
	cfg := clone(object(p["config"]))
	cfg["jobID"] = j["id"]
	p = clone(p)
	p["config"] = cfg
	key, e := a.decrypt(str(p["api_key"]))
	if e != nil || !inventoryCredentialReady(key) {
		return nil, fmt.Errorf("供应商路由密钥不可用")
	}
	ctx = upstreamContext(ctx, a, str(j["id"]), key)
	vendor, kind := strings.ToLower(str(cfg["vendor"])), str(p["type"])
	files := []Row{}
	refs := map[string][]string{"image": {}, "video": {}, "audio": {}}
	for _, raw := range array(p["parameters"]) {
		field := object(raw)
		if field["type"] != "assets" {
			continue
		}
		for _, assetID := range array(object(j["params"])[str(field["name"])]) {
			asset := a.Store.One("SELECT * FROM assets WHERE id=? AND user_id=?", assetID, j["user_id"])
			if asset == nil {
				return nil, fmt.Errorf("供应商参考素材已不存在")
			}
			files = append(files, asset)
		}
	}
	if len(files) > 0 {
		inline := vendor == "ali" || vendor == "huanwangai" || ((vendor == "ccgo" || vendor == "mynewapi") && strings.HasPrefix(str(cfg["sourceModel"]), "nano-banana"))
		multipart := kind == "image" && inventoryOpenAIImage(vendor, str(cfg["sourceModel"]))
		if inline {
			for _, asset := range files {
				data, e := os.ReadFile(a.filePath(str(asset["file"])))
				if e != nil {
					return nil, e
				}
				refs[str(asset["type"])] = append(refs[str(asset["type"])], "data:"+str(asset["mime"])+";base64,"+base64.StdEncoding.EncodeToString(data))
			}
		} else if !multipart {
			refs, e = a.hostedReferences(ctx, p, key, files)
			if e != nil {
				return nil, e
			}
		}
	}
	call, poll, method, e := inventoryPlan(j, p, refs, files)
	if e != nil {
		return nil, e
	}
	resolved := str(object(call.Body)["model"])
	if resolved == "" && call.Form != nil {
		resolved = str(call.Form["model"])
	}
	if tasks, ok := call.Body.([]Row); ok && len(tasks) > 0 {
		resolved = str(tasks[0]["model"])
	}
	if resolved != "" {
		a.updateUpstreamCall("UPDATE upstream_calls SET upstream_model=? WHERE id=?", logText(resolved, 160, key), j["id"])
	}
	upstreamPhase(ctx, "submitting")
	value, binary, _, e := a.inventoryHTTP(ctx, p, key, call)
	if e != nil {
		return nil, e
	}
	if inventoryEnvelopeError(vendor, value) {
		return nil, fmt.Errorf("供应商拒绝了请求，请核对模型参数与可用额度")
	}
	if kind == "text" {
		text := inventoryText(value)
		if strings.TrimSpace(text) == "" || len(text) > 2*1024*1024 {
			return nil, fmt.Errorf("供应商未返回有效文本结果")
		}
		out, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "生成文本.txt", "text/plain", "txt", []byte(text))
		return []Row{out}, e
	}
	if len(binary) > 0 {
		return a.inventorySave(ctx, j, []any{Row{"bytes": binary}}, str(p["base_url"]), key)
	}
	if kind == "audio" && vendor == "openrouter" {
		root := inventoryObject(value)
		if encoded, ok := vendorValue(root, "choices.0.message.audio.data").(string); ok && encoded != "" {
			return a.inventorySave(ctx, j, []any{Row{"b64_json": encoded}}, str(p["base_url"]), key)
		}
	}
	state := inventoryState(vendor, value)
	if inventoryFailed(state) {
		return nil, fmt.Errorf("供应商生成失败，请联系管理员核对上游任务")
	}
	outputs := inventoryOutputs(value, kind)
	ids := inventoryTaskIDs(vendor, value)
	if len(outputs) > 0 && (state == "" || inventoryFinished(state)) {
		return a.inventorySave(ctx, j, outputs, str(p["base_url"]), key)
	}
	if vendor == "runware" && len(ids) == 0 {
		ids = []string{str(j["id"])}
	}
	if len(ids) == 0 || len(strings.Join(ids, ",")) > 255 {
		return nil, fmt.Errorf("供应商未返回有效任务编号")
	}
	if poll == "" && vendor != "runware" {
		return nil, fmt.Errorf("供应商返回异步任务，但此路由缺少轮询协议")
	}
	a.setUpstreamTaskID(j["id"], strings.Join(ids, ","))
	interval := 5 * time.Second
	if ms := integer(cfg["pollInterval"]); ms > 0 && ms <= 10000 {
		interval = time.Duration(ms) * time.Millisecond
	}
	collected := []any{}
	for _, taskID := range ids {
		for {
			if e := wait(ctx, interval); e != nil {
				return nil, e
			}
			upstreamPhase(ctx, "polling")
			path := strings.ReplaceAll(poll, "{id}", url.PathEscape(taskID))
			if strings.Contains(poll, "?") {
				path = strings.ReplaceAll(poll, "{id}", url.QueryEscape(taskID))
			}
			query := inventoryCall{Path: path, Method: method}
			if method == "POST" {
				query.Body = Row{"taskId": taskID}
				if vendor == "midjourney-api" {
					query.Body = Row{"taskIds": []string{taskID}}
				}
				if vendor == "runware" {
					query.Body = []Row{{"taskType": "getResponse", "taskUUID": taskID}}
				}
			}
			response, _, _, err := a.inventoryHTTP(ctx, p, key, query)
			if err != nil {
				if _, ok := err.(inventoryConnectionError); ok && ctx.Err() == nil {
					continue
				}
				if httpError, ok := err.(apiError); ok && (httpError.Status == 404 || httpError.Status == 429 || httpError.Status >= 500) {
					continue
				}
				return nil, err
			}
			if vendor == "midjourney-api" && str(inventoryObject(response)["code"]) == "1002" {
				continue
			}
			response = inventoryPollValue(vendor, taskID, response)
			if inventoryEnvelopeError(vendor, response) {
				return nil, fmt.Errorf("供应商查询返回任务错误，请核对上游记录")
			}
			state = inventoryState(vendor, response)
			if inventoryFailed(state) {
				return nil, fmt.Errorf("供应商任务失败，费用将退回")
			}
			result := inventoryOutputs(response, kind)
			if inventoryFinished(state) || state == "" && len(result) > 0 {
				if len(result) == 0 && kind == "video" && strings.Contains(poll, "/videos/{id}") {
					contentURL, err := inventoryEndpoint(str(p["base_url"]), strings.ReplaceAll(poll, "{id}", url.PathEscape(taskID))+"/content")
					if err != nil {
						return nil, err
					}
					result = []any{Row{"url": contentURL}}
				}
				if len(result) == 0 {
					return nil, fmt.Errorf("供应商任务完成但未返回可保存的结果")
				}
				collected = append(collected, result...)
				break
			}
		}
	}
	return a.inventorySave(ctx, j, collected, str(p["base_url"]), key)
}

func inventoryOpenAIImage(vendor, model string) bool {
	if (vendor == "ccgo" || vendor == "mynewapi") && strings.HasPrefix(model, "nano-banana") {
		return false
	}
	for _, name := range []string{"apiyi", "ccgo", "closeai", "dlapi", "ld", "mikotopro", "myapi", "mynewapi", "official-openai", "rolldek", "zhubo", "zzshu"} {
		if vendor == name {
			return true
		}
	}
	return false
}

func (a *App) inventorySave(ctx context.Context, j Row, outputs []any, base, key string) ([]Row, error) {
	upstreamPhase(ctx, "receiving")
	saved := []Row{}
	kind := str(object(j["provider_snapshot"])["type"])
	limit := 10
	if kind == "image" {
		limit = inventoryImageOutputLimit(object(j["provider_snapshot"]))
	}
	for i, raw := range outputs {
		if i >= limit {
			break
		}
		output := object(raw)
		var data []byte
		var e error
		if blob, ok := output["bytes"].([]byte); ok {
			data = blob
		} else if encoded, ok := output["b64_json"].(string); ok {
			if len(encoded) > 140*1024*1024 {
				return nil, fmt.Errorf("供应商文件过大")
			}
			if strings.HasPrefix(encoded, "data:") {
				parts := strings.SplitN(encoded, ",", 2)
				if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
					return nil, fmt.Errorf("供应商文件编码无效")
				}
				encoded = parts[1]
			}
			data, e = base64.StdEncoding.DecodeString(encoded)
		} else {
			address := str(output["url"])
			if strings.HasPrefix(address, "data:") {
				parts := strings.SplitN(address, ",", 2)
				if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
					return nil, fmt.Errorf("供应商文件编码无效")
				}
				data, e = base64.StdEncoding.DecodeString(parts[1])
			} else {
				if strings.HasPrefix(address, "/") && !strings.HasPrefix(address, "//") {
					origin, err := url.Parse(base)
					if err != nil {
						return nil, fmt.Errorf("供应商结果地址无效")
					}
					relative, err := url.Parse(address)
					if err != nil {
						return nil, fmt.Errorf("供应商结果地址无效")
					}
					address = origin.ResolveReference(relative).String()
				}
				data, e = downloadVendorOutputAuth(ctx, address, base, key, inventoryProviderAuth(object(j["provider_snapshot"])))
			}
		}
		if e != nil {
			return nil, e
		}
		mt, ext := detectMime(data)
		expected := kind
		if kind == "upscale" {
			expected = "video"
		}
		if mt == "" || !strings.HasPrefix(mt, expected+"/") {
			return nil, fmt.Errorf("供应商结果文件类型与任务不一致")
		}
		asset, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "", mt, ext, data)
		if e != nil {
			return nil, e
		}
		saved = append(saved, asset)
	}
	if len(saved) == 0 {
		return nil, fmt.Errorf("供应商未返回生成结果")
	}
	return saved, nil
}
