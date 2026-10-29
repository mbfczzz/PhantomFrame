package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Quote struct {
	Model, Provider, Params, Rate Row
	Quantity, Cost                int64
	Demo                          bool
	Breakdown                     []Row
}

func (q Quote) View() Row {
	return Row{"total": credits(q.Cost), "unitPrice": float64(priceMicros(q.Rate)) / 1000000, "currency": currencyCode, "unit": q.Rate["unit"], "quantity": q.Quantity, "label": q.Rate["label"], "demo": q.Demo, "params": q.Params, "breakdown": q.Breakdown}
}
func (a *App) quote(s Store, mid, uid string, raw Row) Quote {
	m := s.One("SELECT * FROM models WHERE id=? AND enabled=1", mid)
	if m == nil {
		fail(404, "模型不存在或已停用")
	}
	provider := s.One("SELECT * FROM providers WHERE id=? AND enabled=1", m["provider_id"])
	if provider == nil || (provider["protocol"] == "demo" && !a.Config.Demo) {
		fail(400, "模型通道未启用")
	}
	validateProviderModelKind(provider, str(m["type"]))
	if nativeVendor(provider["protocol"]) != "" {
		provider = a.nativeProvider(s, m, provider)
	} else if provider["protocol"] == "inventory" {
		provider = a.inventoryProvider(s, m, provider)
	}
	if provider["protocol"] == "opencrow" {
		provider = a.openCrowProvider(s, m, provider)
	}
	defs := imageRequestParameters(str(m["type"]), array(m["parameters"]))
	known := map[string]bool{}
	assetFields := map[string]bool{}
	resolved := clone(raw)
	for _, f := range defs {
		known[str(object(f)["name"])] = true
		assetFields[str(object(f)["name"])] = object(f)["type"] == "assets"
		if resolved[str(object(f)["name"])] == nil {
			resolved[str(object(f)["name"])] = object(f)["default"]
		}
	}
	for k := range raw {
		if m["type"] == "image" && imageCountParameter(k) && !assetFields[k] {
			if number, ok := raw[k].(float64); !ok || number != 1 {
				fail(400, "图片生成每次只提交一个任务，生成数量必须为 1")
			}
		}
		if !known[k] {
			fail(400, "不支持的参数："+k)
		}
	}
	params := Row{}
	for _, f := range defs {
		p := object(f)
		name, label := str(p["name"]), str(p["label"])
		v := raw[name]
		if v == nil {
			v = p["default"]
		}
		if p["type"] == "number" && !truth(p["required"]) && p["default"] == nil && (v == nil || v == "") {
			continue
		}
		switch p["type"] {
		case "assets":
			if v == nil {
				v = []any{}
			}
			items, ok := v.([]any)
			if !ok {
				fail(400, label+"必须是素材列表")
			}
			maximum := p["max"]
			if byResolution := object(p["maxByResolution"])[str(resolved["resolution"])]; byResolution != nil {
				maximum = byResolution
			}
			if maximum != nil && float64(len(items)) > num(maximum) {
				fail(400, label+"数量超出限制")
			}
			if p["min"] != nil && float64(len(items)) < num(p["min"]) {
				fail(400, label+"数量不足")
			}
			seen := map[string]bool{}
			kind := str(p["itemType"])
			if kind == "" {
				kind = "image"
			}
			assetIDs := []any{}
			for _, item := range items {
				assetID, ok := item.(string)
				if !ok || len(assetID) > 100 || seen[assetID] {
					fail(400, label+"中有不可用或重复的素材")
				}
				seen[assetID] = true
				assetIDs = append(assetIDs, assetID)
			}
			// Batch ownership checks without imposing a hidden reference-count limit.
			for start := 0; start < len(assetIDs); start += 500 {
				end := min(start+500, len(assetIDs))
				batch := assetIDs[start:end]
				args := append([]any{uid, kind}, batch...)
				count := s.One("SELECT count(*) n FROM assets WHERE user_id=? AND type=? AND id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+")", args...)
				if integer(count["n"]) != int64(len(batch)) {
					fail(400, label+"中有不可用或重复的素材")
				}
			}
		case "select":
			found := false
			for _, o := range array(p["options"]) {
				option := o
				if m, ok := o.(map[string]any); ok {
					option = m["v"]
				}
				if str(option) == str(v) {
					v = option
					found = true
					break
				}
			}
			if !found {
				fail(400, label+"参数不正确")
			}
		case "boolean":
			if v == nil {
				v = false
			}
			if _, ok := v.(bool); !ok {
				fail(400, label+"必须为布尔值")
			}
		case "number":
			n, e := strconv.ParseFloat(str(v), 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || (p["min"] != nil && n < num(p["min"])) || (p["max"] != nil && n > num(p["max"])) {
				fail(400, label+"超出范围")
			}
			if truth(p["integer"]) && n != math.Trunc(n) {
				fail(400, label+"必须为整数")
			}
			v = n
		default:
			if v == nil {
				v = ""
			}
			textField(Row{"value": v}, "value", 0, 4000)
		}
		if truth(p["required"]) && (v == nil || v == "" || (p["type"] == "assets" && len(array(v)) == 0)) {
			fail(400, "请填写"+label)
		}
		params[name] = v
	}
	refCount := 0
	for _, f := range defs {
		p := object(f)
		if p["type"] == "assets" {
			refCount += len(array(params[str(p["name"])]))
		}
	}
	for _, f := range defs {
		p := object(f)
		if p["type"] == "assets" && p["maxTotal"] != nil && float64(refCount) > num(p["maxTotal"]) {
			fail(400, "参考素材合计最多 "+str(p["maxTotal"])+" 个")
		}
	}
	if provider["protocol"] == "inventory" || provider["protocol"] == "opencrow" {
		a.validateInventoryQuote(s, m, provider, params, uid)
	}
	matches := []Row{}
	for _, p := range s.All("SELECT * FROM prices WHERE model_id=? AND enabled=1", mid) {
		match := true
		for k, v := range object(p["conditions"]) {
			if str(params[k]) != str(v) {
				match = false
			}
		}
		if match {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		fail(400, "当前参数组合尚未配置价格，请联系管理员")
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return len(object(matches[i]["conditions"])) > len(object(matches[j]["conditions"]))
	})
	rate := matches[0]
	for _, p := range matches[1:] {
		if len(object(p["conditions"])) == len(object(rate["conditions"])) && (priceMicros(p) != priceMicros(rate) || p["unit"] != rate["unit"] || jsonText(object(p["billing_extras"])) != jsonText(object(rate["billing_extras"]))) {
			fail(400, "当前参数匹配到多个同优先级价格，请联系管理员明确计费条件")
		}
	}
	quantity := float64(1)
	switch rate["unit"] {
	case "second":
		quantity = num(params["duration"])
	}
	if quantity < 1 || quantity > 300 || quantity != math.Trunc(quantity) {
		fail(400, "计费数量不正确")
	}
	extra, extraItems := a.referenceCharges(s, uid, defs, params, rate)
	cost := (priceMicros(rate)*int64(quantity) + extra + 5000) / 10000
	if cost < 0 || cost > 100000000 {
		fail(400, "价格超出范围")
	}
	breakdown := append([]Row{{"label": rate["label"], "unitPrice": float64(priceMicros(rate)) / 1000000, "quantity": int64(quantity), "unit": rate["unit"]}}, extraItems...)
	return Quote{m, provider, params, rate, int64(quantity), cost, provider["protocol"] == "demo", breakdown}
}
func (s Store) JobView(j Row) Row {
	snapshot := object(j["provider_snapshot"])
	name, kind := snapshot["modelName"], snapshot["type"]
	if name == nil || kind == nil {
		m := s.One("SELECT name,type FROM models WHERE id=?", j["model_id"])
		name, kind = first(m["name"], j["model_id"]), first(m["type"], "image")
	}
	return Row{"id": j["id"], "userId": j["user_id"], "modelId": j["model_id"], "modelName": name, "type": kind, "prompt": j["prompt"], "params": object(j["params"]), "status": j["status"], "cost": credits(j["cost"]), "currency": currencyCode, "outputs": array(j["outputs"]), "error": j["error"], "source": j["source"], "demo": snapshot["protocol"] == "demo", "refunded": truth(j["refunded"]), "createdAt": j["created_at"], "finishedAt": j["finished_at"]}
}
func (a *App) submit(r *Request) any {
	p := r.Body()
	mid := textField(p, "modelId", 1, 100)
	prompt := trimField(p, "prompt", 1, 10000)
	params := mapField(p, "params")
	expected, hasExpected := p["expectedCost"]
	if hasExpected {
		moneyField(p, "expectedCost", 0, 1e9)
	}
	key := r.R.Header.Get("Idempotency-Key")
	if len(key) > 128 {
		fail(400, "Idempotency-Key 不能超过 128 字节")
	}
	var result Row
	fingerprint := hash(jsonText(Row{"modelId": mid, "prompt": prompt, "params": params}))
	a.Tx(func(s Store) {
		u := s.One("SELECT id,status,auth_version FROM users WHERE id=? FOR UPDATE", r.UID())
		if u == nil || u["status"] != "active" || integer(u["auth_version"]) != integer(r.User["auth_version"]) {
			fail(401, "请先登录")
		}
		if key != "" {
			if j := s.One("SELECT * FROM jobs WHERE user_id=? AND idempotency_key=?", r.UID(), key); j != nil {
				if j["request_hash"] != nil && str(j["request_hash"]) != fingerprint {
					fail(409, "该幂等编号已用于不同的生成请求")
				}
				if j["request_hash"] == nil {
					if j["model_id"] != mid || j["prompt"] != prompt {
						fail(409, "该幂等编号已用于不同的生成请求")
					}
					for name, value := range params {
						if jsonText(value) != jsonText(object(j["params"])[name]) {
							fail(409, "该幂等编号已用于不同的生成参数")
						}
					}
				}
				result = a.jobView(s, j)
				return
			}
		}
		if integer(s.One("SELECT count(*) n FROM jobs WHERE user_id=? AND status IN ('queued','running')", r.UID())["n"]) >= 10 {
			fail(429, "同时最多提交 10 个任务")
		}
		s.One("SELECT id FROM models WHERE id=? FOR UPDATE", mid)
		q := a.quote(s, mid, r.UID(), params)
		if q.Provider["protocol"] == "inventory" || q.Provider["protocol"] == "opencrow" {
			a.validateInventoryQuote(s, q.Model, q.Provider, q.Params, r.UID(), prompt)
		}
		storage, storageErr := a.storageConfig(s)
		if storageErr != nil {
			fail(400, storageErr.Error())
		}
		if storage.Enabled {
			if err := validateOSS(storage, true); err != nil {
				fail(400, err.Error())
			}
		}
		if hasExpected && units(expected) != q.Cost {
			fail(409, "价格已更新，请确认最新报价后重新提交")
		}
		jid := id()
		source := "web"
		if r.APIKey {
			source = "api"
		}
		s.Wallet(r.UID(), -q.Cost, "generation", str(q.Model["name"])+" · 生成预扣", jid)
		snapshot := snapshotUploadProvider(s, q.Provider)
		// Capture the effective encrypted credential once. Submissions, polling and
		// downloads keep using this snapshot even if an administrator rotates a key.
		if entry := s.One("SELECT api_key FROM provider_models WHERE provider_id=? AND upstream_model=?", q.Model["provider_id"], q.Model["upstream_model"]); q.Provider["protocol"] != "inventory" && entry != nil && str(entry["api_key"]) != "" {
			snapshot["api_key"] = entry["api_key"]
		}
		snapshot["modelName"] = q.Model["name"]
		snapshot["upstreamModel"] = q.Model["upstream_model"]
		snapshot["type"] = q.Model["type"]
		snapshot["parameters"] = array(q.Model["parameters"])
		if q.Model["type"] == "image" {
			snapshot["maxOutputsPerRequest"] = imageOutputLimit(str(q.Model["upstream_model"]))
			if q.Provider["protocol"] == "inventory" {
				snapshot["maxOutputsPerRequest"] = inventoryImageOutputLimit(snapshot)
			}
		}
		var idem any
		if key != "" {
			idem = key
		}
		s.Exec("INSERT INTO jobs(id,user_id,model_id,prompt,params,cost,source,idempotency_key,provider_snapshot,outputs) VALUES(?,?,?,?,?,?,?,?,?,'[]')", jid, r.UID(), mid, prompt, jsonText(q.Params), q.Cost, source, idem, jsonText(snapshot))
		s.Exec("UPDATE jobs SET request_hash=? WHERE id=?", fingerprint, jid)
		job := s.One("SELECT * FROM jobs WHERE id=?", jid)
		a.archiveUpstreamCall(s, job, false)
		result = a.jobView(s, job)
	})
	a.wake()
	r.Status = 202
	return result
}
func (a *App) wake() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pipe := a.Redis.TxPipeline()
	key := a.Config.RedisPrefix + "jobs:wake"
	pipe.LPush(ctx, key, "1")
	pipe.LTrim(ctx, key, 0, 1)
	pipe.Expire(ctx, key, time.Minute)
	_, _ = pipe.Exec(ctx)
}
func (s Store) refundLocked(j Row, message, status string) {
	if j == nil || (j["status"] != "queued" && j["status"] != "running" && j["status"] != "refund_pending") {
		return
	}
	if !truth(j["refunded"]) {
		s.Wallet(str(j["user_id"]), integer(j["cost"]), "refund", "生成结束 · 退款", j["id"])
	}
	message = strings.ReplaceAll(message, "退款处理中", "费用已退回")
	msg := []rune(message)
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	s.Exec("UPDATE jobs SET status=?,error=?,refunded=1,finished_at=CURRENT_TIMESTAMP(3) WHERE id=?", status, string(msg), j["id"])
}
func (a *App) refund(jid, message string) {
	// Persist the obligation separately: a failed wallet transaction must not erase it.
	pendingMessage := strings.NewReplacer("费用已退还", "退款处理中", "费用已退回", "退款处理中").Replace(message)
	a.Tx(func(s Store) {
		j := s.One("SELECT status FROM jobs WHERE id=? FOR UPDATE", jid)
		if j == nil || (j["status"] != "running" && j["status"] != "queued" && j["status"] != "refund_pending" && j["status"] != "failed") {
			return
		}
		s.Exec("UPDATE jobs SET status='refund_pending',error=?,refund_next_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status IN ('running','queued')", string([]rune(pendingMessage)[:min(len([]rune(pendingMessage)), 1000)]), jid)
		s.Exec("INSERT IGNORE INTO job_cleanups(job_id) VALUES(?)", jid)
	})
	a.Tx(func(s Store) {
		s.refundLocked(s.One("SELECT * FROM jobs WHERE id=? FOR UPDATE", jid), message, "failed")
	})
	a.syncUpstreamCall(jid)
}
func jobSource(r *Request) string {
	source := r.R.URL.Query().Get("source")
	if source == "" || source == "all" {
		return ""
	}
	if source != "api" && source != "web" {
		fail(400, "source 只能为 api、web 或 all")
	}
	return source
}
func (a *App) creationRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/models/status", 1, a.modelStatus)
	a.route(m, "GET /api/models", 1, func(r *Request) any {
		q := "SELECT m.* FROM models m JOIN providers p ON p.id=m.provider_id WHERE m.enabled=1 AND p.enabled=1"
		if !a.Config.Demo {
			q += " AND p.protocol!='demo'"
		}
		return a.Store.ModelViews(a.Store.All(q + " ORDER BY m.sort_order,m.name"))
	})
	a.route(m, "POST /api/pricing/quote", 1, func(r *Request) any {
		p := r.Body()
		return a.quote(a.Store, textField(p, "modelId", 1, 100), r.UID(), mapField(p, "params")).View()
	})
	a.route(m, "POST /api/jobs", 1, a.submit)
	a.route(m, "GET /api/jobs", 1, func(r *Request) any {
		where, args := []string{"j.user_id=?", "j.hidden_at IS NULL"}, []any{r.UID()}
		q := r.R.URL.Query()
		if source := jobSource(r); source != "" {
			where = append(where, "j.source=?")
			args = append(args, source)
		}
		for key, col := range map[string]string{"type": "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(j.provider_snapshot,'$.type')),m.type)", "status": "j.status"} {
			if v := q.Get(key); v != "" && v != "all" {
				where = append(where, col+"=?")
				args = append(args, v)
			}
		}
		if v := q.Get("search"); v != "" {
			where = append(where, "(j.prompt LIKE ? OR COALESCE(JSON_UNQUOTE(JSON_EXTRACT(j.provider_snapshot,'$.modelName')),m.name) LIKE ? OR j.id LIKE ?)")
			for i := 0; i < 3; i++ {
				args = append(args, "%"+v+"%")
			}
		}
		if v := q.Get("from"); v != "" {
			if parseTime(v).IsZero() {
				fail(400, "起始日期不正确")
			}
			where = append(where, "j.created_at>=?")
			args = append(args, parseTime(v))
		}
		if v := q.Get("to"); v != "" {
			date := parseTime(v)
			if date.IsZero() {
				fail(400, "截止日期不正确")
			}
			where = append(where, "j.created_at<?")
			args = append(args, date.Add(24*time.Hour))
		}
		page, limit := r.Page()
		clause := strings.Join(where, " AND ")
		total := a.Store.One("SELECT count(*) n FROM jobs j LEFT JOIN models m ON m.id=j.model_id WHERE "+clause, args...)["n"]
		v := []Row{}
		for _, j := range a.Store.All("SELECT j.* FROM jobs j LEFT JOIN models m ON m.id=j.model_id WHERE "+clause+" ORDER BY j.created_at DESC,j.seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...) {
			v = append(v, a.jobView(a.Store, j))
		}
		return Row{"items": v, "total": total, "page": page, "limit": limit}
	})
	a.route(m, "GET /api/jobs/{id}", 1, func(r *Request) any {
		j := a.Store.One("SELECT * FROM jobs WHERE id=? AND user_id=? AND hidden_at IS NULL", r.ID(), r.UID())
		if j == nil {
			fail(404, "任务不存在")
		}
		return a.jobView(a.Store, j)
	})
	a.route(m, "POST /api/jobs/{id}/cancel", 1, func(r *Request) any {
		a.Tx(func(s Store) {
			j := s.One("SELECT * FROM jobs WHERE id=? AND user_id=? FOR UPDATE", r.ID(), r.UID())
			if j == nil {
				fail(404, "任务不存在")
			}
			if j["status"] != "queued" {
				fail(400, "仅排队中的任务可以取消")
			}
			s.refundLocked(j, "用户取消", "cancelled")
		})
		a.syncUpstreamCall(r.ID())
		return Row{"ok": true}
	})
	a.route(m, "DELETE /api/jobs/{id}", 1, func(r *Request) any {
		a.Tx(func(s Store) {
			j := s.One("SELECT * FROM jobs WHERE id=? AND user_id=? FOR UPDATE", r.ID(), r.UID())
			if j == nil {
				fail(404, "任务不存在")
			}
			if j["status"] == "queued" || j["status"] == "running" || j["status"] == "refund_pending" {
				fail(400, "请等待任务结束后删除")
			}
			a.archiveUpstreamCall(s, j, true)
			s.Exec("UPDATE jobs SET hidden_at=CURRENT_TIMESTAMP(3) WHERE id=?", r.ID())
		})
		return Row{"ok": true}
	})
}
func errorMessage(e error) string {
	if v, ok := e.(apiError); ok {
		return v.Message
	}
	return fmt.Sprint(e)
}
