package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type inventoryCall struct {
	Path, Method, Header string
	Body                 any
	Form                 Row
	Files                []vendorFile
	Repeated             map[string][]string
}

type inventoryConnectionError struct{}

func (inventoryConnectionError) Error() string {
	return "供应商连接中断，请根据本站任务编号核对上游"
}

func inventoryProviderAuth(provider Row) string {
	cfg := object(provider["config"])
	if strings.EqualFold(str(cfg["vendor"]), "mynewapi") && strings.HasPrefix(str(cfg["sourceModel"]), "nano-banana") {
		return "x-goog-api-key"
	}
	return inventoryAuth(str(cfg["vendor"]))
}

func inventoryEndpoint(base, path string) (string, error) {
	if path == "" {
		return strings.TrimRight(base, "/"), nil
	}
	if !inventoryPathValid(path) {
		return "", fmt.Errorf("供应商接口路径不正确")
	}
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("供应商地址不正确")
	}
	// Inventory bases sometimes already include /v1 or /api/v1. Remove only
	// an exact duplicated path prefix; never let a route replace the host.
	prefix := strings.TrimRight(u.Path, "/")
	for prefix != "" {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
			break
		}
		if next := strings.Index(prefix[1:], "/"); next >= 0 {
			prefix = prefix[next+1:]
		} else {
			break
		}
	}
	return strings.TrimRight(base, "/") + path, nil
}

func inventoryAuth(vendor string) string {
	switch strings.ToLower(vendor) {
	case "midjourney-api":
		return "API-KEY"
	case "secure-skill-sepcial":
		return "X-License-Key"
	}
	return "Authorization"
}

func (a *App) inventoryHTTP(ctx context.Context, p Row, key string, call inventoryCall) (any, []byte, string, error) {
	address, e := inventoryEndpoint(str(p["base_url"]), call.Path)
	if e != nil {
		return nil, nil, "", e
	}
	var input io.Reader
	contentType := "application/json"
	if call.Form != nil {
		body, ct, err := a.vendorFormRepeated(call.Form, call.Files, call.Repeated)
		if err != nil {
			return nil, nil, "", err
		}
		input = body
		contentType = ct
	} else if call.Body != nil {
		input = strings.NewReader(jsonText(call.Body))
	}
	method := call.Method
	if method == "" {
		method = "POST"
	}
	req, e := http.NewRequestWithContext(ctx, method, address, input)
	if e != nil {
		return nil, nil, "", fmt.Errorf("供应商请求构造失败")
	}
	// Even with a deduplication header, a vendor may not implement idempotency.
	// Disable net/http's implicit replay of POST bodies on reused connections.
	if method == "POST" {
		req.GetBody = nil
	}
	header := call.Header
	if header == "" {
		header = inventoryProviderAuth(p)
	}
	if header == "Authorization" {
		req.Header.Set(header, "Bearer "+key)
	} else {
		req.Header.Set(header, key)
	}
	req.Header.Set("Accept", "application/json, audio/mpeg, audio/wav")
	if input != nil {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Idempotency-Key", str(object(p["config"])["jobID"]))
	client := upstreamClient()
	client.Timeout = 90 * time.Second
	res, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, "", ctx.Err()
		}
		return nil, nil, "", inventoryConnectionError{}
	}
	status := res.StatusCode
	mt := strings.Split(res.Header.Get("Content-Type"), ";")[0]
	if status < 200 || status >= 300 {
		cfg := object(p["config"])
		if cfg["vendor"] == "opencrow" && p["type"] == "video" && (status >= 500 || status == 408 || status == 409 || status == 429) {
			raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
			res.Body.Close()
			if readErr == nil {
				if receipt, ok := openCrowAcceptedErrorResponse(raw, str(cfg["upstreamModel"])); ok {
					return receipt, nil, "", nil
				}
			}
			return nil, nil, "", apiError{status, fmt.Sprintf("供应商请求失败（HTTP %d）", status)}
		}
		res.Body.Close()
		return nil, nil, "", apiError{status, fmt.Sprintf("供应商请求失败（HTTP %d）", status)}
	}
	raw, e := boundedBody(res, 32*1024*1024)
	if e != nil {
		var network net.Error
		if errors.Is(e, io.ErrUnexpectedEOF) || errors.As(e, &network) {
			return nil, nil, "", inventoryConnectionError{}
		}
		return nil, nil, "", e
	}
	if strings.HasPrefix(mt, "audio/") {
		return nil, raw, mt, nil
	}
	var result any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil, nil, "", fmt.Errorf("供应商返回的数据不是有效 JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, nil, "", fmt.Errorf("供应商返回了多个 JSON 内容")
	}
	return result, nil, "", nil
}

func inventoryPaths(vendor, kind, model string) (string, string, string) {
	vendor = strings.ToLower(vendor)
	switch vendor {
	case "ali":
		return "/services/aigc/multimodal-generation/generation", "", ""
	case "atlascloud":
		return "/api/v1/model/generateImage", "/api/v1/model/prediction/{id}", "GET"
	case "bytedance":
		if kind == "image" {
			return "/images/generations", "", ""
		}
		return "/contents/generations/tasks", "/contents/generations/tasks/{id}", "GET"
	case "copp":
		return "/openapi/v1/generations", "/openapi/v1/generations/{id}", "GET"
	case "dimensio":
		if kind == "image" {
			return "/v1/images/generations", "", ""
		}
		return "/v1/videos/generations", "/v1/videos/tasks/{id}", "GET"
	case "dsas":
		return "/v1/video/generations", "/v1/video/generations/{id}", "GET"
	case "huanwangai":
		return "/mj/submit/imagine", "/mj/task/{id}/fetch", "GET"
	case "infotoken":
		if kind == "video" {
			return "/v1/contents/generations/tasks", "/v1/tasks/{id}", "GET"
		}
	case "kinovi":
		return "/api/v1/jobs/createTask", "/api/v1/jobs/recordInfo?taskId={id}", "GET"
	case "midjourney-api":
		if strings.Contains(model, "midjourney") {
			return "/midjourney/v1/submit-jobs", "/midjourney/v1/job-status", "POST"
		}
		return "/gpt/v1/generations", "/gpt/v1/job-status", "POST"
	case "mxapi":
		if strings.Contains(model, "sfx") {
			return "/api/v2/music/sound", "/api/v2/music/task?id={id}", "GET"
		}
		return "/api/v2/music/generate", "/api/v2/music/task?id={id}", "GET"
	case "openrouter":
		switch kind {
		case "image":
			return "/v1/images", "", ""
		case "video":
			return "/v1/videos", "/v1/videos/{id}", "GET"
		case "audio":
			if strings.Contains(model, "tts") {
				return "/v1/audio/speech", "", ""
			}
			return "/v1/chat/completions", "", ""
		}
	case "runware":
		return "", "", "POST"
	case "runninghub":
		return "/openapi/v2/rhart-image-g-2-official/text-to-image", "/openapi/v2/query", "POST"
	case "runninghub-upscale":
		return "/openapi/v2/rhart-video/rh-upscale/enhance-frame", "/openapi/v2/query", "POST"
	case "wavespeed":
		return "/api/v3/" + model, "/api/v3/predictions/{id}/result", "GET"
	case "secure-skill-out", "secure-skill-sepcial":
		return "/api/generate-video", "/api/task/{id}", "GET"
	case "secure-skill":
		if kind == "image" {
			return "/v1/images/generations", "", ""
		}
		if strings.HasPrefix(model, "wan3") || strings.Contains(model, "seedance-2.0") || strings.HasPrefix(model, "jimeng-video") {
			return "/v1/videos/generations", "/v1/videos/tasks/{id}", "GET"
		}
		return "/v1/videos", "/v1/videos/{id}", "GET"
	case "ld", "closeai", "zhubo":
		if kind == "image" {
			return "/images/generations", "", ""
		}
		if kind == "video" {
			return "/videos", "/videos/{id}", "GET"
		}
	}
	if kind == "image" {
		return "/v1/images/generations", "", ""
	}
	if kind == "video" {
		return "/v1/videos", "/v1/videos/{id}", "GET"
	}
	return "", "", ""
}
