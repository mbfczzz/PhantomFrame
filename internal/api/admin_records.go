package api

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func adminQueryChoice(r *Request, name string, values ...string) string {
	value := r.R.URL.Query().Get(name)
	if value == "" || value == "all" {
		return ""
	}
	return enumField(Row{name: value}, name, values...)
}
func adminQueryDates(r *Request, column string, where *[]string, args *[]any) {
	var start, end time.Time
	for _, field := range []string{"from", "to"} {
		value := r.R.URL.Query().Get(field)
		if value == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			fail(400, "日期格式不正确")
		}
		if field == "from" {
			start = parsed
			*where = append(*where, column+">=?")
			*args = append(*args, parsed)
		} else {
			end = parsed
			*where = append(*where, column+"<?")
			*args = append(*args, parsed.AddDate(0, 0, 1))
		}
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		fail(400, "开始日期不能晚于结束日期")
	}
}
func adminQuerySearch(r *Request, columns []string, where *[]string, args *[]any) {
	value := strings.TrimSpace(r.R.URL.Query().Get("search"))
	if value == "" {
		return
	}
	if len(value) > 600 {
		fail(400, "搜索内容过长")
	}
	parts := []string{}
	for _, column := range columns {
		parts = append(parts, "LOCATE(?,LOWER("+column+"))>0")
		*args = append(*args, strings.ToLower(value))
	}
	*where = append(*where, "("+strings.Join(parts, " OR ")+")")
}
func adminQueryID(r *Request, name, column string, where *[]string, args *[]any) {
	if value := r.R.URL.Query().Get(name); value != "" && value != "all" {
		if len(value) > 100 {
			fail(400, "筛选条件过长")
		}
		*where = append(*where, column+"=?")
		*args = append(*args, value)
	}
}
func (a *App) adminJobView(j Row) Row {
	item := a.Store.JobView(j)
	item["username"], item["displayName"] = j["username"], j["display_name"]
	snapshot := object(j["provider_snapshot"])
	item["providerName"], item["upstreamId"] = snapshot["name"], j["upstream_id"]
	// Only expose the named metadata above; snapshots contain encrypted provider credentials.
	outputs := []Row{}
	for _, value := range array(item["outputs"]) {
		output := clone(object(value))
		output["url"] = "/api/admin/jobs/" + url.PathEscape(str(j["id"])) + "/outputs/" + url.PathEscape(str(output["assetId"]))
		outputs = append(outputs, output)
	}
	item["outputs"] = outputs
	return item
}
func (a *App) listAdminJobs(r *Request) any {
	page, limit := r.Page()
	where, args := []string{"1=1"}, []any{}
	for name, column := range map[string]string{"status": "j.status", "source": "j.source", "type": "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(j.provider_snapshot,'$.type')),m.type)"} {
		allowed := map[string][]string{"status": {"queued", "running", "succeeded", "failed", "cancelled"}, "source": {"web", "api"}, "type": {"image", "video"}}
		if value := adminQueryChoice(r, name, allowed[name]...); value != "" {
			where = append(where, column+"=?")
			args = append(args, value)
		}
	}
	adminQueryID(r, "userId", "j.user_id", &where, &args)
	adminQueryID(r, "modelId", "j.model_id", &where, &args)
	adminQueryDates(r, "j.created_at", &where, &args)
	adminQuerySearch(r, []string{"j.id", "u.username", "u.display_name", "m.name", "j.model_id", "j.prompt", "JSON_UNQUOTE(JSON_EXTRACT(j.provider_snapshot,'$.modelName'))"}, &where, &args)
	from := " FROM jobs j JOIN users u ON u.id=j.user_id JOIN models m ON m.id=j.model_id WHERE " + strings.Join(where, " AND ")
	summary := a.Store.One("SELECT count(*) total,coalesce(sum(j.status='succeeded'),0) succeeded,coalesce(sum(j.status='failed'),0) failed,coalesce(sum(j.status IN ('queued','running')),0) active,coalesce(sum(CASE WHEN j.refunded=0 THEN j.cost ELSE 0 END),0) spent"+from, args...)
	items := []Row{}
	for _, job := range a.Store.All("SELECT j.*,u.username,u.display_name"+from+" ORDER BY j.created_at DESC,j.seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...) {
		items = append(items, a.adminJobView(job))
	}
	summary["spent"] = credits(summary["spent"])
	summary["currency"] = currencyCode
	return Row{"items": items, "total": summary["total"], "page": page, "limit": limit, "summary": summary}
}
func (a *App) adminJobDetail(r *Request) any {
	job := a.Store.One("SELECT j.*,u.username,u.display_name FROM jobs j JOIN users u ON u.id=j.user_id WHERE j.id=?", r.ID())
	if job == nil {
		fail(404, "任务不存在")
	}
	return a.adminJobView(job)
}
func (a *App) adminJobOutput(r *Request) any {
	asset := a.Store.One("SELECT a.* FROM assets a JOIN jobs j ON j.id=a.job_id AND j.user_id=a.user_id WHERE j.id=? AND a.id=?", r.ID(), r.R.PathValue("assetID"))
	if asset == nil {
		fail(404, "生成文件不存在或已删除")
	}
	r.W.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	r.W.Header().Set("Content-Type", str(asset["mime"]))
	if r.R.URL.Query().Get("download") != "" {
		r.W.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": str(asset["name"])}))
	}
	http.ServeFile(r.W, r.R, a.filePath(str(asset["file"])))
	return nil
}
func (a *App) listAdminCodes(r *Request) any {
	page, limit := r.Page()
	where, args := []string{"1=1"}, []any{}
	switch adminQueryChoice(r, "status", "unused", "used", "expired") {
	case "unused":
		where = append(where, "c.used_by IS NULL AND (c.expires_at IS NULL OR c.expires_at>UTC_TIMESTAMP(3))")
	case "used":
		where = append(where, "c.used_by IS NOT NULL")
	case "expired":
		where = append(where, "c.used_by IS NULL AND c.expires_at<=UTC_TIMESTAMP(3)")
	}
	adminQuerySearch(r, []string{"c.code", "u.username", "u.display_name"}, &where, &args)
	from := " FROM redeem_codes c LEFT JOIN users u ON u.id=c.used_by WHERE " + strings.Join(where, " AND ")
	total := a.Store.One("SELECT count(*) n"+from, args...)["n"]
	items := a.Store.All("SELECT c.*,u.username,CASE WHEN c.used_by IS NOT NULL THEN 'used' WHEN c.expires_at<=UTC_TIMESTAMP(3) THEN 'expired' ELSE 'unused' END status"+from+" ORDER BY c.created_at DESC,c.id DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...)
	for _, item := range items {
		item["credits"] = credits(item["credits"])
		item["currency"] = currencyCode
	}
	return Row{"items": items, "total": total, "page": page, "limit": limit}
}
func (a *App) listAdminTransactions(r *Request) any {
	page, limit := r.Page()
	where, args := []string{"1=1"}, []any{}
	if kind := adminQueryChoice(r, "kind", "generation", "refund", "redeem", "welcome", "adjustment", "transfer"); kind != "" {
		where = append(where, "t.kind=?")
		args = append(args, kind)
	}
	adminQueryID(r, "userId", "t.user_id", &where, &args)
	adminQueryDates(r, "t.created_at", &where, &args)
	adminQuerySearch(r, []string{"t.id", "t.description", "t.job_id", "u.username", "u.display_name"}, &where, &args)
	from := " FROM transactions t JOIN users u ON u.id=t.user_id WHERE " + strings.Join(where, " AND ")
	total := a.Store.One("SELECT count(*) n"+from, args...)["n"]
	items := a.Store.All("SELECT t.*,u.username"+from+" ORDER BY t.created_at DESC,t.seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...)
	for _, item := range items {
		item["amount"] = credits(item["amount"])
		item["balance"] = credits(item["balance"])
		item["currency"] = currencyCode
	}
	return Row{"items": items, "total": total, "page": page, "limit": limit}
}
func (a *App) listAdminAudit(r *Request) any {
	page, limit := r.Page()
	where, args := []string{"1=1"}, []any{}
	adminQueryDates(r, "a.created_at", &where, &args)
	adminQuerySearch(r, []string{"a.action", "a.detail", "u.username"}, &where, &args)
	from := " FROM audit_log a LEFT JOIN users u ON u.id=a.user_id WHERE " + strings.Join(where, " AND ")
	total := a.Store.One("SELECT count(*) n"+from, args...)["n"]
	items := a.Store.All("SELECT a.*,u.username"+from+" ORDER BY a.created_at DESC,a.id DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...)
	for _, item := range items {
		item["detail"] = object(item["detail"])
	}
	return Row{"items": items, "total": total, "page": page, "limit": limit}
}
