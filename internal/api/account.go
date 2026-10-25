package api

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) accountRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/transactions", 1, a.listTransactions)
	a.route(m, "GET /api/usage", 1, func(r *Request) any {
		where, args := "user_id=?", []any{r.UID()}
		if source := jobSource(r); source != "" {
			where += " AND source=?"
			args = append(args, source)
		}
		v := a.Store.One("SELECT count(*) total,coalesce(sum(status='succeeded'),0) succeeded,coalesce(sum(status='failed'),0) failed,coalesce(sum(CASE WHEN refunded=0 THEN cost ELSE 0 END),0) spent FROM jobs WHERE "+where, args...)
		v["spent"] = credits(v["spent"])
		v["balance"] = credits(r.User["balance"])
		v["currency"] = currencyCode
		v["redeemed"] = credits(a.Store.One("SELECT coalesce(sum(amount),0) n FROM transactions WHERE user_id=? AND kind='redeem'", r.UID())["n"])
		v["daily"] = a.Store.All("SELECT date(created_at) date,count(*) count,sum(CASE WHEN refunded=0 THEN cost ELSE 0 END)/100.0 credits FROM jobs WHERE "+where+" AND created_at>=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 7 DAY) GROUP BY date(created_at) ORDER BY date(created_at)", args...)
		return v
	})
	a.route(m, "POST /api/redeem", 2, func(r *Request) any {
		code := strings.ToUpper(trimField(r.Body(), "code", 1, 100))
		amount := float64(0)
		var balance float64
		var transactionID string
		a.Tx(func(s Store) {
			c := s.One("SELECT * FROM redeem_codes WHERE code=? FOR UPDATE", code)
			if c == nil || c["used_by"] != nil || (str(c["expires_at"]) != "" && parseTime(c["expires_at"]).Before(time.Now())) {
				fail(400, "兑换码无效、已使用或已过期")
			}
			if s.Exec("UPDATE redeem_codes SET used_by=?,used_at=CURRENT_TIMESTAMP(3) WHERE id=? AND used_by IS NULL", r.UID(), c["id"]) == 0 {
				fail(409, "兑换码已使用")
			}
			transactionID = s.Wallet(r.UID(), integer(c["credits"]), "redeem", "兑换码充值", nil)
			amount = credits(c["credits"])
			balance = credits(s.One("SELECT balance FROM users WHERE id=?", r.UID())["balance"])
		})
		return Row{"credits": amount, "balance": balance, "transactionId": transactionID, "currency": currencyCode}
	})
	a.route(m, "GET /api/keys", 2, func(r *Request) any {
		return a.Store.All("SELECT id,name,prefix,created_at,last_used_at FROM api_keys WHERE user_id=? ORDER BY created_at DESC", r.UID())
	})
	a.route(m, "POST /api/keys", 2, func(r *Request) any {
		name := trimField(r.Body(), "name", 1, 80)
		keyID, t := id(), "pf_"+token()
		a.Tx(func(s Store) {
			s.One("SELECT id FROM users WHERE id=? FOR UPDATE", r.UID())
			if integer(s.One("SELECT count(*) n FROM api_keys WHERE user_id=?", r.UID())["n"]) >= 20 {
				fail(400, "最多创建 20 个 API Key")
			}
			s.Exec("INSERT INTO api_keys(id,user_id,name,prefix,token) VALUES(?,?,?,?,?)", keyID, r.UID(), name, t[:12], hash(t))
		})
		r.Status = 201
		return Row{"id": keyID, "name": name, "token": t}
	})
	a.route(m, "DELETE /api/keys/{id}", 2, func(r *Request) any {
		a.Store.Exec("DELETE FROM api_keys WHERE id=? AND user_id=?", r.ID(), r.UID())
		return Row{"ok": true}
	})
	a.route(m, "GET /api/sub-users", 2, func(r *Request) any {
		items := []Row{}
		for _, u := range a.Store.All("SELECT * FROM users WHERE parent_id=?", r.UID()) {
			items = append(items, publicUser(u))
		}
		return items
	})
	a.route(m, "POST /api/sub-users", 2, func(r *Request) any {
		if r.User["parent_id"] != nil {
			fail(403, "子账户不能创建子账户")
		}
		p := r.Body()
		name, pass, uid := validUsername(p), passwordHash(validPassword(p)), id()
		a.Store.Exec("INSERT INTO users(id,username,display_name,password,parent_id) VALUES(?,?,?,?,?)", uid, name, name, pass, r.UID())
		r.Status = 201
		return publicUser(a.Store.One("SELECT * FROM users WHERE id=?", uid))
	})
	a.route(m, "POST /api/sub-users/{id}/transfer", 2, func(r *Request) any {
		amount := units(moneyField(r.Body(), "credits", 0.01, 1000000))
		a.Tx(func(s Store) {
			s.One("SELECT id FROM users WHERE id=? FOR UPDATE", r.UID())
			u := s.One("SELECT id FROM users WHERE id=? AND parent_id=? AND status='active' FOR UPDATE", r.ID(), r.UID())
			if u == nil {
				fail(404, "子账户不存在")
			}
			s.Wallet(r.UID(), -amount, "transfer", "向子账户转账", nil)
			s.Wallet(r.ID(), amount, "transfer", "主账户转入", nil)
		})
		return Row{"ok": true}
	})
	a.route(m, "DELETE /api/sub-users/{id}", 2, func(r *Request) any {
		a.Store.Exec("UPDATE users SET parent_id=NULL WHERE id=? AND parent_id=?", r.ID(), r.UID())
		return Row{"ok": true}
	})
}
func parseTime(v any) time.Time {
	s := str(v)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999", "2006-01-02"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t
		}
	}
	return time.Time{}
}
