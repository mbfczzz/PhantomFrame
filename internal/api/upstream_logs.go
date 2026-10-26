package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type upstreamScopeKey struct{}
type upstreamScope struct {
	app     *App
	jobID   string
	secrets []string
}

func upstreamContext(ctx context.Context, a *App, jobID string, secrets ...string) context.Context {
	return context.WithValue(ctx, upstreamScopeKey{}, &upstreamScope{a, jobID, secrets})
}

var logURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
var logEscapePattern = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

func logText(value string, max int, secrets ...string) string {
	// Diagnostic text may contain embedded URLs, not just a single URL value.
	value = logURLPattern.ReplaceAllStringFunc(value, safeLogURL)
	value = logEscapePattern.ReplaceAllStringFunc(value, strings.ToUpper)
	for _, secret := range secrets {
		if secret != "" {
			for _, encoded := range []string{url.PathEscape(secret), strings.ReplaceAll(url.PathEscape(secret), "%2F", "/"), url.QueryEscape(secret), secret} {
				value = strings.ReplaceAll(value, encoded, "[已隐藏]")
			}
		}
	}
	runes := []rune(value)
	if len(runes) > max {
		value = string(runes[:max-1]) + "…"
	}
	return value
}

func sensitiveLogField(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
	switch key {
	case "key", "apikey", "xapikey", "xgoogapikey", "xtoken", "apitoken", "authorization", "proxyauthorization", "auth", "password", "secret", "secretkey", "privatekey", "token", "securitytoken", "accesstoken", "refreshtoken", "accesskey", "accesskeyid", "accesskeysecret", "clientsecret", "signature", "credentials", "credential", "cookie", "setcookie", "headers":
		return true
	}
	return false
}

func safeLogURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func sanitizeLogValue(value any, depth int, secrets []string) any {
	if depth > 5 {
		return "[内容已省略]"
	}
	switch v := value.(type) {
	case map[string]any:
		result := Row{}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i == 50 {
				result["_truncated"] = true
				break
			}
			if sensitiveLogField(key) {
				result[key] = "[已隐藏]"
			} else {
				result[key] = sanitizeLogValue(v[key], depth+1, secrets)
			}
		}
		return result
	case []any:
		result := make([]any, 0, min(len(v), 50))
		for _, item := range v[:min(len(v), 50)] {
			result = append(result, sanitizeLogValue(item, depth+1, secrets))
		}
		if len(v) > 50 {
			result = append(result, "[更多内容已省略]")
		}
		return result
	case string:
		if strings.HasPrefix(v, "data:") {
			return "[内嵌文件已省略]"
		}
		if u := safeLogURL(v); u != "" {
			v = u
		}
		return logText(v, 1024, secrets...)
	default:
		return value
	}
}

func safeLogParams(params Row, secrets ...string) Row {
	result := object(sanitizeLogValue(params, 0, secrets))
	if len(jsonText(result)) <= 32768 {
		return result
	}
	compact := Row{"_truncated": "参数较长，请结合生成任务查看"}
	for _, key := range []string{"duration", "resolution", "quality", "aspectRatio", "size", "seed", "n"} {
		if value, ok := result[key]; ok {
			compact[key] = value
		}
	}
	return compact
}

func callPhase(status string) string {
	if status == "succeeded" {
		return "completed"
	}
	return status
}

func nullableLogDate(value any) any {
	if str(value) == "" {
		return nil
	}
	return value
}

// No foreign key points to jobs/models/providers: user-visible deletion must not
// remove the operational record needed to investigate an upstream charge.
func (a *App) archiveUpstreamCall(s Store, j Row, legacy bool) {
	p := object(j["provider_snapshot"])
	u := s.One("SELECT username,display_name FROM users WHERE id=?", j["user_id"])
	model := Row{}
	if str(p["modelName"]) == "" || str(p["upstreamModel"]) == "" {
		model = s.One("SELECT name,type,upstream_model,provider_id FROM models WHERE id=?", j["model_id"])
	}
	secret, _ := a.decrypt(str(p["api_key"]))
	phase := callPhase(str(j["status"]))
	if legacy && j["status"] == "running" {
		phase = "legacy"
	}
	columns := "id,user_id,username,display_name,provider_id,provider_name,protocol,model_id,model_name,model_type,upstream_model,source,status,phase,params,cost,refunded,upstream_id,error,legacy,created_at,finished_at"
	args := []any{j["id"], j["user_id"], str(u["username"]), str(u["display_name"]), str(first(p["id"], model["provider_id"])), str(p["name"]), str(p["protocol"]), j["model_id"], str(first(p["modelName"], model["name"])), str(first(p["type"], model["type"])), str(first(p["upstreamModel"], model["upstream_model"])), j["source"], j["status"], phase, jsonText(safeLogParams(object(j["params"]), secret)), j["cost"], truth(j["refunded"]), logText(str(j["upstream_id"]), 255, secret), logText(str(j["error"]), 1000, secret), legacy, j["created_at"], nullableLogDate(j["finished_at"])}
	query := "INSERT INTO upstream_calls(" + columns + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ") ON DUPLICATE KEY UPDATE status=VALUES(status),phase=VALUES(phase),refunded=VALUES(refunded),upstream_id=VALUES(upstream_id),error=VALUES(error),finished_at=VALUES(finished_at),updated_at=UTC_TIMESTAMP(3)"
	s.Exec(query, args...)
}

func (a *App) backfillUpstreamCalls() error {
	return try(func() {
		var after int64
		for {
			rows := a.Store.All("SELECT j.* FROM jobs j LEFT JOIN upstream_calls c ON c.id=j.id WHERE c.id IS NULL AND j.seq>? ORDER BY j.seq LIMIT 100", after)
			if len(rows) == 0 {
				return
			}
			for _, j := range rows {
				a.archiveUpstreamCall(a.Store, j, true)
				after = integer(j["seq"])
			}
		}
	})
}

// Runtime diagnostics are best-effort: a logging failure must not turn an
// accepted provider request into a second submission or change wallet behavior.
func (a *App) updateUpstreamCall(query string, args ...any) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := a.DB.ExecContext(ctx, query, args...); err != nil {
		log.Printf("upstream log update deferred type=%T", err)
	}
}

func (a *App) beginUpstreamCall(j Row) {
	phase := "submitting"
	if object(j["provider_snapshot"])["protocol"] == "demo" {
		phase = "demo"
	}
	a.updateUpstreamCall("UPDATE upstream_calls SET status='running',phase=?,started_at=COALESCE(started_at,UTC_TIMESTAMP(3)),legacy=FALSE,updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='queued'", phase, j["id"])
}

func upstreamPhase(ctx context.Context, phase string) {
	if scope, ok := ctx.Value(upstreamScopeKey{}).(*upstreamScope); ok {
		scope.app.updateUpstreamCall("UPDATE upstream_calls SET phase=?,updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='running'", phase, scope.jobID)
	}
}

func (a *App) setUpstreamTaskID(jobID, upstreamID any) {
	a.Store.Exec("UPDATE jobs SET upstream_id=? WHERE id=?", upstreamID, jobID)
	_ = try(func() {
		j := a.Store.One("SELECT provider_snapshot FROM jobs WHERE id=?", jobID)
		secret, _ := a.decrypt(str(object(j["provider_snapshot"])["api_key"]))
		a.updateUpstreamCall("UPDATE upstream_calls SET upstream_id=?,phase='polling',updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='running'", logText(str(upstreamID), 255, secret), jobID)
	})
}

func (a *App) syncUpstreamCall(jobID string) {
	if err := try(func() {
		j := a.Store.One("SELECT status,refunded,upstream_id,error,finished_at,provider_snapshot FROM jobs WHERE id=?", jobID)
		if j == nil {
			return
		}
		secret, _ := a.decrypt(str(object(j["provider_snapshot"])["api_key"]))
		a.updateUpstreamCall("UPDATE upstream_calls SET status=?,phase=CASE WHEN ? IN ('succeeded','failed','cancelled') THEN ? ELSE phase END,refunded=?,upstream_id=?,error=?,finished_at=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", j["status"], j["status"], callPhase(str(j["status"])), truth(j["refunded"]), logText(str(j["upstream_id"]), 255, secret), logText(str(j["error"]), 1000, secret), nullableLogDate(j["finished_at"]), jobID)
	}); err != nil {
		log.Printf("upstream log synchronization deferred type=%T", err)
	}
}

func (a *App) noteUpstreamFailure(j Row, message string) {
	secret, _ := a.decrypt(str(object(j["provider_snapshot"])["api_key"]))
	message = strings.NewReplacer("费用已退还", "退款处理中", "费用已退回", "退款处理中").Replace(message)
	a.updateUpstreamCall("UPDATE upstream_calls SET failure_phase=CASE WHEN phase!='refund_pending' THEN phase ELSE failure_phase END,phase='refund_pending',error=?,updated_at=UTC_TIMESTAMP(3) WHERE id=? AND status='running'", logText(message, 1000, secret), j["id"])
}

type upstreamTransport struct{ base http.RoundTripper }

func (t upstreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	response, err := t.base.RoundTrip(r)
	if scope, ok := r.Context().Value(upstreamScopeKey{}).(*upstreamScope); ok {
		var status any
		requestID, networkError := "", ""
		secrets := append([]string{}, scope.secrets...)
		for name, values := range r.Header {
			if sensitiveLogField(name) {
				for _, value := range values {
					secrets = append(secrets, strings.TrimPrefix(value, "Bearer "))
				}
			}
		}
		if response != nil {
			status = response.StatusCode
			requestID = str(first(response.Header.Get("X-Request-Id"), response.Header.Get("Request-Id"), response.Header.Get("X-Tt-Logid")))
		}
		if err != nil {
			networkError = "连接失败或请求超时"
			if r.Context().Err() != nil {
				networkError = "请求超时或已取消"
			}
		}
		scope.app.updateUpstreamCall("UPDATE upstream_calls SET request_count=request_count+1,last_http_status=?,last_method=?,last_url=?,last_request_ms=?,last_request_id=?,last_request_error=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", status, r.Method, logText(safeLogURL(r.URL.String()), 700, secrets...), time.Since(start).Milliseconds(), logText(requestID, 255, secrets...), networkError, scope.jobID)
	}
	return response, err
}

func (t upstreamTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func upstreamCallView(row Row, admin bool) Row {
	status := str(first(row["live_status"], row["status"]))
	phase := str(row["phase"])
	if status == "succeeded" || status == "failed" || status == "cancelled" {
		phase = callPhase(status)
	}
	end := time.Now().UTC()
	finished := first(row["live_finished_at"], row["finished_at"])
	knownEnd := status == "queued" || status == "running" || status == "refund_pending"
	if status == "refund_pending" {
		phase = "refund_pending"
	}
	if parsed := parseTime(finished); !parsed.IsZero() {
		end = parsed
		knownEnd = true
	}
	var total, elapsed any
	if created := parseTime(row["created_at"]); !created.IsZero() && knownEnd {
		total = max(int64(0), end.Sub(created).Milliseconds())
	}
	if started := parseTime(row["started_at"]); !started.IsZero() && knownEnd {
		elapsed = max(int64(0), end.Sub(started).Milliseconds())
	}
	refunded := row["refunded"]
	if row["live_refunded"] != nil {
		refunded = row["live_refunded"]
	}
	result := Row{"id": row["id"], "providerName": row["provider_name"], "modelName": row["model_name"], "upstreamModel": row["upstream_model"], "upstreamId": row["upstream_id"], "status": status, "phase": phase, "failurePhase": row["failure_phase"], "source": row["source"], "type": row["model_type"], "demo": row["protocol"] == "demo", "cost": credits(row["cost"]), "refunded": truth(refunded), "createdAt": row["created_at"], "startedAt": row["started_at"], "finishedAt": finished, "totalMs": total, "elapsedMs": elapsed, "requestCount": row["request_count"], "lastHttpStatus": row["last_http_status"], "error": row["error"], "legacy": truth(row["legacy"])}
	if admin {
		for key, column := range map[string]string{"userId": "user_id", "username": "username", "displayName": "display_name", "providerId": "provider_id", "modelId": "model_id", "protocol": "protocol", "lastMethod": "last_method", "lastUrl": "last_url", "lastRequestId": "last_request_id", "lastRequestMs": "last_request_ms", "lastRequestError": "last_request_error", "updatedAt": "updated_at"} {
			result[key] = row[column]
		}
		result["params"] = object(row["params"])
		result["jobAvailable"] = row["live_status"] != nil
	}
	return result
}

const upstreamCallColumns = "SELECT c.*,j.status live_status,j.refunded live_refunded,j.finished_at live_finished_at"
const upstreamCallSelect = upstreamCallColumns + " FROM upstream_calls c LEFT JOIN jobs j ON j.id=c.id"

func (a *App) upstreamCallDetail(r *Request, admin bool) any {
	query, args := upstreamCallSelect+" WHERE c.id=?", []any{r.ID()}
	if !admin {
		query += " AND c.user_id=?"
		args = append(args, r.UID())
	}
	row := a.Store.One(query, args...)
	if row == nil {
		fail(404, "没有找到上游调用日志")
	}
	return upstreamCallView(row, admin)
}

func (a *App) listUpstreamCalls(r *Request) any {
	page, limit := r.Page()
	where, args := []string{"1=1"}, []any{}
	for name, column := range map[string]string{"providerId": "c.provider_id", "userId": "c.user_id", "modelId": "c.model_id"} {
		adminQueryID(r, name, column, &where, &args)
	}
	for name, values := range map[string][]string{"status": {"queued", "running", "refund_pending", "succeeded", "failed", "cancelled"}, "source": {"web", "api"}, "type": {"image", "video", "text", "audio", "upscale"}} {
		if value := adminQueryChoice(r, name, values...); value != "" {
			column := "c." + name
			if name == "status" {
				column = "COALESCE(j.status,c.status)"
			} else if name == "type" {
				column = "c.model_type"
			}
			where, args = append(where, column+"=?"), append(args, value)
		}
	}
	adminQueryDates(r, "c.created_at", &where, &args)
	adminQuerySearch(r, []string{"c.id", "c.upstream_id", "j.upstream_id", "c.provider_name", "c.model_name", "c.upstream_model", "c.username", "c.display_name"}, &where, &args)
	from := " FROM upstream_calls c LEFT JOIN jobs j ON j.id=c.id WHERE " + strings.Join(where, " AND ")
	summary := a.Store.One("SELECT count(*) total,coalesce(sum(COALESCE(j.status,c.status)='succeeded'),0) succeeded,coalesce(sum(COALESCE(j.status,c.status)='failed'),0) failed,coalesce(sum(COALESCE(j.status,c.status) IN ('queued','running','refund_pending')),0) active"+from, args...)
	items := []Row{}
	for _, row := range a.Store.All(upstreamCallColumns+from+" ORDER BY c.created_at DESC,c.seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...) {
		items = append(items, upstreamCallView(row, true))
	}
	return Row{"items": items, "total": summary["total"], "page": page, "limit": limit, "summary": summary}
}

func (a *App) upstreamLogRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/admin/upstream-calls", 3, a.listUpstreamCalls)
	a.route(mux, "GET /api/admin/upstream-calls/options", 3, func(r *Request) any {
		providers := a.Store.All("SELECT id,name FROM providers UNION ALL SELECT c.provider_id id,MAX(c.provider_name) name FROM upstream_calls c WHERE c.provider_id!='' AND NOT EXISTS(SELECT 1 FROM providers p WHERE p.id=c.provider_id) GROUP BY c.provider_id ORDER BY name")
		models := a.Store.All("SELECT id,name FROM models UNION ALL SELECT c.model_id id,MAX(c.model_name) name FROM upstream_calls c WHERE NOT EXISTS(SELECT 1 FROM models m WHERE m.id=c.model_id) GROUP BY c.model_id ORDER BY name")
		return Row{"providers": providers, "models": models}
	})
	a.route(mux, "GET /api/admin/upstream-calls/{id}", 3, func(r *Request) any { return a.upstreamCallDetail(r, true) })
	a.route(mux, "GET /api/jobs/{id}/upstream", 1, func(r *Request) any { return a.upstreamCallDetail(r, false) })
}
