package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type apiError struct {
	Status  int
	Message string
}

func (e apiError) Error() string      { return e.Message }
func fail(status int, message string) { panic(apiError{status, message}) }

type Request struct {
	A      *App
	W      http.ResponseWriter
	R      *http.Request
	User   Row
	APIKey bool
	Status int
}

func (r *Request) Body() Row {
	r.R.Body = http.MaxBytesReader(r.W, r.R.Body, 3*1024*1024)
	var m Row
	d := json.NewDecoder(r.R.Body)
	if e := d.Decode(&m); e != nil || m == nil {
		fail(400, "请求 JSON 格式不正确")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(400, "请求 JSON 格式不正确")
	}
	return m
}
func (r *Request) ID() string  { return r.R.PathValue("id") }
func (r *Request) UID() string { return str(r.User["id"]) }
func (r *Request) Page() (int, int) {
	page, limit := int(num(r.R.URL.Query().Get("page"))), int(num(r.R.URL.Query().Get("limit")))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	if limit < 1 {
		limit = 24
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
func textField(m Row, k string, min, max int, fallback ...string) string {
	v, ok := m[k]
	if !ok && len(fallback) > 0 {
		return fallback[0]
	}
	s, ok := v.(string)
	if !ok || utf8.RuneCountInString(s) < min || utf8.RuneCountInString(s) > max {
		fail(400, k+" 内容长度或格式不正确")
	}
	return s
}
func trimField(m Row, k string, min, max int, fallback ...string) string {
	s := strings.TrimSpace(textField(m, k, 0, max, fallback...))
	if utf8.RuneCountInString(s) < min {
		fail(400, k+" 不能为空")
	}
	return s
}
func numberField(m Row, k string, min, max float64, fallback ...float64) float64 {
	v, ok := m[k]
	if !ok && len(fallback) > 0 {
		return fallback[0]
	}
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < min || n > max {
		fail(400, k+" 超出范围")
	}
	return n
}
func boolField(m Row, k string, fallback bool) bool {
	v, ok := m[k]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		fail(400, k+" 必须为布尔值")
	}
	return b
}
func mapField(m Row, k string) Row {
	v, ok := m[k]
	if !ok {
		return Row{}
	}
	r, ok := v.(map[string]any)
	if !ok {
		fail(400, k+" 必须为对象")
	}
	return r
}
func listField(m Row, k string, max int) []any {
	v, ok := m[k]
	if !ok {
		return []any{}
	}
	r, ok := v.([]any)
	if !ok || len(r) > max {
		fail(400, k+" 数量超限")
	}
	return r
}
func enumField(m Row, k string, values ...string) string {
	s := str(m[k])
	for _, v := range values {
		if s == v {
			return s
		}
	}
	fail(400, k+" 值不正确")
	return ""
}
func validUsername(m Row) string {
	s := trimField(m, "username", 3, 40)
	if !regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString(s) {
		fail(400, "用户名只能包含字母、数字、点、下划线和横线")
	}
	return s
}
func validPassword(m Row) string {
	s := textField(m, "password", 8, 128)
	if len([]byte(s)) > 72 {
		fail(400, "密码最多 72 字节")
	}
	return s
}
func (a *App) route(mux *http.ServeMux, pattern string, access int, fn func(*Request) any) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if x := recover(); x != nil {
				status, msg := 500, "操作失败，请稍后重试"
				switch e := x.(type) {
				case apiError:
					status, msg = e.Status, e.Message
				case *mysql.MySQLError:
					if e.Number == 1062 {
						status, msg = 409, "记录已存在，请刷新后重试"
					} else if e.Number == 1451 || e.Number == 1452 {
						status, msg = 409, "记录存在关联数据，不能执行该操作"
					} else {
						log.Printf("database error code=%d", e.Number)
					}
				default:
					log.Printf("request error type=%T", x)
				}
				send(w, status, Row{"message": msg})
			}
		}()
		req := &Request{A: a, W: w, R: r, Status: 200}
		if access > 0 {
			a.authenticate(req)
			if access >= 2 && req.APIKey {
				fail(403, "此操作需要网页登录")
			}
			if access == 3 && req.User["role"] != "admin" {
				fail(403, "需要管理员权限")
			}
		}
		if r.Method == "POST" {
			switch r.URL.Path {
			case "/api/auth/login":
				a.limit(r, "login", 30, 15*time.Minute)
			case "/api/redeem":
				a.limitIdentity(r, "redeem", "user:"+req.UID(), 30, 15*time.Minute)
			case "/api/auth/register", "/api/auth/demo":
				a.limit(r, "register", 10, time.Hour)
			case "/api/jobs":
				a.limitIdentity(r, "jobs", "user:"+req.UID(), 30, time.Minute)
			}
		}
		if result := fn(req); result != nil {
			send(w, req.Status, result)
		}
	})
}
func send(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func (a *App) limit(r *http.Request, group string, limit int, window time.Duration) {
	a.limitIdentity(r, group, "ip:"+a.clientIP(r), limit, window)
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	trusted := func(addr netip.Addr) bool {
		for _, value := range a.Config.TrustedProxies {
			if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Contains(addr) {
				return true
			}
			if ip, err := netip.ParseAddr(value); err == nil && ip.Unmap() == addr {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String()
	}
	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" {
		forwarded = r.Header.Get("X-Real-IP")
	}
	if len(forwarded) > 4096 {
		return peer.String()
	}
	chain := strings.Split(forwarded, ",")
	current := peer
	// Walk from the TCP peer towards the client; never trust client-supplied leftmost entries.
	for i := len(chain) - 1; i >= 0 && trusted(current); i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			return peer.String()
		}
		current = addr.Unmap()
	}
	return current.String()
}

func (a *App) limitIdentity(r *http.Request, group, identity string, limit int, window time.Duration) {
	key := a.Config.RedisPrefix + "limit:" + group + ":" + hash(identity)
	script := `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return n`
	n, e := a.Redis.Eval(r.Context(), script, []string{key}, window.Milliseconds()).Int()
	if e != nil {
		fail(503, "会话服务暂不可用，请稍后重试")
	}
	if n > limit {
		fail(429, "操作过于频繁，请稍后重试")
	}
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.upstreamLogRoutes(mux)
	if !a.Config.SupplierInstances {
		a.inventoryRoutes(mux)
	}
	a.protocolRoutes(mux)
	a.authRoutes(mux)
	a.creationRoutes(mux)
	a.accountRoutes(mux)
	a.ticketRoutes(mux)
	a.assetRoutes(mux)
	a.projectRoutes(mux)
	a.adminRoutes(mux)
	a.storageRoutes(mux)
	a.route(mux, "GET /api/health", 0, func(r *Request) any {
		ctx, cancel := context.WithTimeout(r.R.Context(), 2*time.Second)
		defer cancel()
		if a.DB.PingContext(ctx) != nil || a.Redis.Ping(ctx).Err() != nil {
			r.Status = 503
			return Row{"status": "unavailable"}
		}
		return Row{"status": "ok", "backend": "go", "database": "mysql", "cache": "redis"}
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { send(w, 404, Row{"message": "接口不存在"}) })
	root, _ := filepath.Abs(a.Config.FrontendDir)
	fs := http.FileServer(http.Dir(root))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		clean := filepath.Clean(filepath.FromSlash(r.URL.Path))
		path := filepath.Join(root, clean)
		rel, e := filepath.Rel(root, path)
		if e != nil || strings.HasPrefix(rel, "..") {
			http.NotFound(w, r)
			return
		}
		if info, e := os.Stat(path); e == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		if _, e := os.Stat(filepath.Join(root, "index.html")); e != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if strings.HasPrefix(r.URL.Path, "/api") {
			w.Header().Set("Cache-Control", "no-store")
		}
		origin := r.Header.Get("Origin")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && origin != "" {
			allowed := origin == a.Config.AppURL
			if !a.Config.Production {
				allowed = allowed || origin == "http://127.0.0.1:5173" || origin == "http://localhost:5173" || origin == "http://127.0.0.1:"+a.Config.Port
			}
			if !allowed {
				send(w, 403, Row{"message": "不允许的请求来源"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func try(f func()) (err error) {
	defer func() {
		if v := recover(); v != nil {
			switch e := v.(type) {
			case error:
				err = e
			default:
				err = errors.New("internal operation failed")
			}
		}
	}()
	f()
	return nil
}
