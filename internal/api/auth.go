package api

import (
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"strings"
	"time"
)

func (a *App) authenticate(r *Request) {
	bearer := strings.TrimSpace(r.R.Header.Get("X-API-Key"))
	authorization := strings.TrimSpace(r.R.Header.Get("Authorization"))
	hasKeyHeader := len(r.R.Header.Values("Authorization")) > 0 || len(r.R.Header.Values("X-API-Key")) > 0
	if authorization != "" {
		parts := strings.Fields(authorization)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			fail(401, "请使用 Authorization: Bearer API_KEY")
		}
		bearer = parts[1]
	}
	if hasKeyHeader {
		if !strings.HasPrefix(bearer, "pf_") {
			fail(401, "API Key 无效或已撤销")
		}
		k := a.Store.One("SELECT * FROM api_keys WHERE token=?", hash(bearer))
		if k == nil {
			fail(401, "API Key 无效或已撤销")
		}
		r.User = a.Store.One("SELECT * FROM users WHERE id=?", k["user_id"])
		if r.User == nil || r.User["status"] != "active" {
			fail(401, "API Key 所属账户不可用")
		}
		r.APIKey = true
		a.Store.Exec("UPDATE api_keys SET last_used_at=CURRENT_TIMESTAMP(3) WHERE id=?", k["id"])
	} else if c, e := r.R.Cookie("pf_session"); e == nil {
		value, e := a.Redis.Get(r.R.Context(), a.Config.RedisPrefix+"session:"+hash(c.Value)).Result()
		if e != nil && e != redis.Nil {
			fail(503, "会话服务暂不可用，请稍后重试")
		}
		if e == nil {
			s := object(value)
			u := a.Store.One("SELECT * FROM users WHERE id=?", s["userId"])
			if u != nil && integer(u["auth_version"]) == integer(s["version"]) {
				r.User = u
			}
		}
	}
	if r.User == nil || r.User["status"] != "active" {
		fail(401, "请先登录")
	}
}
func (a *App) login(r *Request, u Row) any {
	t := token()
	e := a.Redis.Set(r.R.Context(), a.Config.RedisPrefix+"session:"+hash(t), jsonText(Row{"userId": u["id"], "version": u["auth_version"]}), 7*24*time.Hour).Err()
	if e != nil {
		fail(503, "会话服务暂不可用，请稍后重试")
	}
	http.SetCookie(r.W, &http.Cookie{Name: "pf_session", Value: t, Path: "/", HttpOnly: true, Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 86400})
	return publicUser(u)
}
func (s Store) Settings() Row {
	result := Row{}
	for _, v := range s.All("SELECT `key`,value FROM settings") {
		if str(v["key"]) == "_inventoryStartupFingerprint" {
			continue
		}
		result[str(v["key"])] = v["value"]
	}
	return result
}
func (a *App) authRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/config", 0, func(r *Request) any {
		v := a.Store.Settings()
		v["demoMode"] = a.Config.Demo
		v["currency"] = currencyCode
		v["creditValueInYuan"] = 1
		return v
	})
	a.route(m, "POST /api/auth/login", 0, func(r *Request) any {
		p := r.Body()
		u := a.Store.One("SELECT * FROM users WHERE username=?", textField(p, "username", 1, 40))
		pass := textField(p, "password", 0, 128)
		if u == nil || u["status"] != "active" || bcrypt.CompareHashAndPassword([]byte(str(u["password"])), []byte(pass)) != nil {
			fail(401, "账号或密码不正确")
		}
		return a.login(r, u)
	})
	a.route(m, "POST /api/auth/register", 0, func(r *Request) any {
		if a.Store.Settings()["registrationEnabled"] != "true" {
			fail(403, "当前未开放注册")
		}
		p := r.Body()
		name := validUsername(p)
		pass := passwordHash(validPassword(p))
		display := trimField(p, "displayName", 1, 50, name)
		uid := id()
		a.Tx(func(s Store) {
			s.Exec("INSERT INTO users(id,username,display_name,password) VALUES(?,?,?,?)", uid, name, display, pass)
			welcome := units(s.Settings()["welcomeCredits"])
			if welcome > 0 {
				s.Wallet(uid, welcome, "welcome", "新用户注册赠送", nil)
			}
		})
		return a.login(r, a.Store.One("SELECT * FROM users WHERE id=?", uid))
	})
	a.route(m, "POST /api/auth/demo", 0, func(r *Request) any {
		if !a.Config.Demo {
			fail(404, "演示登录已关闭")
		}
		uid := id()
		a.Tx(func(s Store) {
			s.Exec("INSERT INTO users(id,username,display_name,password) VALUES(?,?,?,?)", uid, "demo-"+randomHex(4), "体验创作者", passwordHash(token()))
			s.Wallet(uid, 100000, "welcome", "演示额度", nil)
		})
		return a.login(r, a.Store.One("SELECT * FROM users WHERE id=?", uid))
	})
	a.route(m, "POST /api/auth/logout", 0, func(r *Request) any {
		if c, e := r.R.Cookie("pf_session"); e == nil {
			if a.Redis.Del(r.R.Context(), a.Config.RedisPrefix+"session:"+hash(c.Value)).Err() != nil {
				fail(503, "会话服务暂不可用，请稍后重试")
			}
		}
		http.SetCookie(r.W, &http.Cookie{Name: "pf_session", Value: "", Path: "/", HttpOnly: true, Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		return Row{"ok": true}
	})
	a.route(m, "GET /api/me", 1, func(r *Request) any { return publicUser(r.User) })
	a.route(m, "PATCH /api/me", 2, func(r *Request) any {
		name := trimField(r.Body(), "displayName", 1, 50)
		a.Store.Exec("UPDATE users SET display_name=? WHERE id=?", name, r.UID())
		return publicUser(a.Store.One("SELECT * FROM users WHERE id=?", r.UID()))
	})
	a.route(m, "POST /api/me/password", 2, func(r *Request) any {
		p := r.Body()
		old := textField(p, "currentPassword", 0, 128)
		pass := validPassword(p)
		if bcrypt.CompareHashAndPassword([]byte(str(r.User["password"])), []byte(old)) != nil {
			fail(400, "原密码不正确")
		}
		a.Store.Exec("UPDATE users SET password=?,auth_version=auth_version+1 WHERE id=?", passwordHash(pass), r.UID())
		return a.login(r, a.Store.One("SELECT * FROM users WHERE id=?", r.UID()))
	})
}
