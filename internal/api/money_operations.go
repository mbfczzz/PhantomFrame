package api

import (
	"sort"
	"strings"
)

// Money mutations share one durable, user-scoped operation namespace. Both the
// receipt and the ledger entries commit together, including after a lost response.
func (a *App) moneyOperation(r *Request, payload Row, fn func(Store) Row) Row {
	key := r.R.Header.Get("Idempotency-Key")
	if len(key) == 0 || len(key) > 128 {
		fail(400, "资金操作必须提供 1–128 字节的 Idempotency-Key，重试请沿用原值")
	}
	fingerprint := hash(r.R.URL.Path + "\n" + jsonText(payload))
	var result Row
	a.Tx(func(s Store) {
		// Lock both parties in a stable order (including opposing admin adjustments).
		parties := []string{r.UID(), r.ID()}
		sort.Strings(parties)
		for _, uid := range parties {
			s.One("SELECT id FROM users WHERE id=? FOR UPDATE", uid)
		}
		actor := s.One("SELECT * FROM users WHERE id=?", r.UID())
		if actor == nil || actor["status"] != "active" || integer(actor["auth_version"]) != integer(r.User["auth_version"]) {
			fail(401, "账户权限已变更，请重新登录")
		}
		if strings.HasPrefix(r.R.URL.Path, "/api/admin/") && actor["role"] != "admin" {
			fail(403, "需要管理员权限")
		}
		if previous := s.One("SELECT * FROM money_operations WHERE user_id=? AND operation_key=?", r.UID(), key); previous != nil {
			if previous["request_hash"] != fingerprint {
				fail(409, "该操作编号已用于不同的资金请求")
			}
			result = object(previous["result"])
			return
		}
		result = fn(s)
		result["operationId"] = key
		s.Exec("INSERT INTO money_operations(user_id,operation_key,request_hash,result) VALUES(?,?,?,?)", r.UID(), key, fingerprint, jsonText(result))
	})
	return result
}
