package api

import (
	"strings"
	"time"
)

func (a *App) listTransactions(r *Request) any {
	page, limit := r.Page()
	q := r.R.URL.Query()
	where, args := []string{"user_id=?"}, []any{r.UID()}
	if kind := q.Get("kind"); kind != "" && kind != "all" {
		enumField(Row{"kind": kind}, "kind", "generation", "refund", "redeem", "welcome", "adjustment", "transfer")
		where = append(where, "kind=?")
		args = append(args, kind)
	}
	if direction := q.Get("direction"); direction != "" && direction != "all" {
		enumField(Row{"direction": direction}, "direction", "in", "out")
		if direction == "in" {
			where = append(where, "amount>0")
		} else {
			where = append(where, "amount<0")
		}
	}
	var from, to time.Time
	if value := q.Get("from"); value != "" {
		var err error
		from, err = time.Parse("2006-01-02", value)
		if err != nil {
			fail(400, "开始日期不正确")
		}
		where = append(where, "created_at>=?")
		args = append(args, from)
	}
	if value := q.Get("to"); value != "" {
		var err error
		to, err = time.Parse("2006-01-02", value)
		if err != nil {
			fail(400, "结束日期不正确")
		}
		where = append(where, "created_at<?")
		args = append(args, to.Add(24*time.Hour))
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		fail(400, "开始日期不能晚于结束日期")
	}
	if search := strings.TrimSpace(q.Get("search")); search != "" {
		if len(search) > 600 {
			fail(400, "搜索内容过长")
		}
		where = append(where, "(description LIKE ? OR id LIKE ? OR job_id LIKE ?)")
		for i := 0; i < 3; i++ {
			args = append(args, "%"+search+"%")
		}
	}
	clause := strings.Join(where, " AND ")
	summary := a.Store.One("SELECT count(*) n,coalesce(sum(CASE WHEN amount>0 THEN amount ELSE 0 END),0) income,coalesce(sum(CASE WHEN amount<0 THEN -amount ELSE 0 END),0) expense FROM transactions WHERE "+clause, args...)
	items := a.Store.All("SELECT * FROM transactions WHERE "+clause+" ORDER BY created_at DESC,seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...)
	for _, item := range items {
		item["amount"] = credits(item["amount"])
		item["balance"] = credits(item["balance"])
	}
	return Row{"items": items, "total": summary["n"], "page": page, "limit": limit, "summary": Row{"balance": credits(r.User["balance"]), "income": credits(summary["income"]), "expense": credits(summary["expense"]), "unit": currencyCode, "currency": currencyCode}}
}
