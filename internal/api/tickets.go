package api

import (
	"net/http"
	"strings"
)

func ticketCategory(p Row) string {
	return enumField(p, "category", "generation", "billing", "api", "account", "other")
}
func (s Store) ticket(id, uid string, staff, lock bool) Row {
	query, args := "SELECT * FROM support_tickets WHERE id=?", []any{id}
	if !staff {
		query += " AND user_id=?"
		args = append(args, uid)
	}
	if lock {
		query += " FOR UPDATE"
	}
	ticket := s.One(query, args...)
	if ticket == nil {
		fail(404, "工单不存在")
	}
	return ticket
}
func (s Store) ticketView(ticket Row) Row {
	owner := s.One("SELECT username,display_name FROM users WHERE id=?", ticket["user_id"])
	number := str(ticket["id"])
	if len(number) > 8 {
		number = number[:8]
	}
	return Row{"id": ticket["id"], "number": "TK-" + strings.ToUpper(number), "userId": ticket["user_id"], "username": owner["username"], "displayName": owner["display_name"], "subject": ticket["subject"], "category": ticket["category"], "status": ticket["status"], "relatedJobId": ticket["related_job_id"], "createdAt": ticket["created_at"], "updatedAt": ticket["updated_at"]}
}
func (s Store) ticketDetail(ticket Row) Row {
	view := s.ticketView(ticket)
	messages := []Row{}
	for _, message := range s.All("SELECT m.*,u.display_name FROM support_messages m JOIN users u ON u.id=m.author_id WHERE m.ticket_id=? ORDER BY m.seq", ticket["id"]) {
		messages = append(messages, Row{"id": message["id"], "authorId": message["author_id"], "authorName": message["display_name"], "authorRole": message["author_role"], "content": message["content"], "createdAt": message["created_at"]})
	}
	view["messages"] = messages
	view["relatedJob"] = nil
	if ticket["related_job_id"] != nil {
		if job := s.One("SELECT * FROM jobs WHERE id=? AND user_id=?", ticket["related_job_id"], ticket["user_id"]); job != nil {
			summary := s.JobView(job)
			view["relatedJob"] = Row{"id": summary["id"], "modelName": summary["modelName"], "status": summary["status"], "cost": summary["cost"], "refunded": summary["refunded"]}
		}
	}
	return view
}
func (a *App) listTickets(r *Request, staff bool) any {
	where, args := []string{"1=1"}, []any{}
	if !staff {
		where = append(where, "t.user_id=?")
		args = append(args, r.UID())
	}
	query := r.R.URL.Query()
	if status := query.Get("status"); status != "" && status != "all" {
		enumField(Row{"status": status}, "status", "open", "replied", "resolved", "closed")
		where = append(where, "t.status=?")
		args = append(args, status)
	}
	if category := query.Get("category"); category != "" && category != "all" {
		ticketCategory(Row{"category": category})
		where = append(where, "t.category=?")
		args = append(args, category)
	}
	search := strings.TrimSpace(query.Get("search"))
	if len(search) > 600 {
		fail(400, "搜索内容过长")
	}
	if search != "" {
		where = append(where, "(LOWER(t.subject) LIKE ? OR LOWER(t.id) LIKE ?)")
		args = append(args, "%"+strings.ToLower(search)+"%", "%"+strings.TrimPrefix(strings.ToLower(search), "tk-")+"%")
	}
	page, limit := r.Page()
	clause := strings.Join(where, " AND ")
	total := a.Store.One("SELECT count(*) n FROM support_tickets t WHERE "+clause, args...)["n"]
	items := []Row{}
	for _, ticket := range a.Store.All("SELECT t.* FROM support_tickets t WHERE "+clause+" ORDER BY t.updated_at DESC,t.seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...) {
		items = append(items, a.Store.ticketView(ticket))
	}
	return Row{"items": items, "total": total, "page": page, "limit": limit}
}
func (a *App) createTicket(r *Request) any {
	p := r.Body()
	subject := trimField(p, "subject", 3, 120)
	content := trimField(p, "content", 5, 6000)
	category := ticketCategory(p)
	jobID := trimField(p, "jobId", 0, 100, "")
	ticketID := id()
	a.Tx(func(s Store) {
		s.One("SELECT id FROM users WHERE id=? FOR UPDATE", r.UID())
		if integer(s.One("SELECT count(*) n FROM support_tickets WHERE user_id=? AND status IN ('open','replied')", r.UID())["n"]) >= 50 {
			fail(429, "最多同时保留 50 个未结束的工单")
		}
		var related any
		if jobID != "" {
			if s.One("SELECT id FROM jobs WHERE id=? AND user_id=?", jobID, r.UID()) == nil {
				fail(400, "关联任务不存在或不属于当前账户")
			}
			related = jobID
		}
		s.Exec("INSERT INTO support_tickets(id,user_id,subject,category,related_job_id) VALUES(?,?,?,?,?)", ticketID, r.UID(), subject, category, related)
		s.Exec("INSERT INTO support_messages(id,ticket_id,author_id,author_role,content) VALUES(?,?,?,'user',?)", id(), ticketID, r.UID(), content)
	})
	r.Status = 201
	return a.Store.ticketDetail(a.Store.ticket(ticketID, r.UID(), false, false))
}
func (a *App) replyTicket(r *Request, staff bool) any {
	content := trimField(r.Body(), "content", 1, 6000)
	a.Tx(func(s Store) {
		ticket := s.ticket(r.ID(), r.UID(), staff, true)
		if ticket["status"] == "closed" || ticket["status"] == "resolved" {
			fail(409, "工单已结束，请先重新打开再回复")
		}
		role, status := "user", "open"
		if staff {
			role, status = "staff", "replied"
		}
		s.Exec("INSERT INTO support_messages(id,ticket_id,author_id,author_role,content) VALUES(?,?,?,?,?)", id(), r.ID(), r.UID(), role, content)
		s.Exec("UPDATE support_tickets SET status=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", status, r.ID())
		if staff {
			s.Audit(r.UID(), "ticket.reply", Row{"id": r.ID(), "status": status})
		}
	})
	r.Status = 201
	return a.Store.ticketDetail(a.Store.ticket(r.ID(), r.UID(), staff, false))
}
func (a *App) updateTicket(r *Request, staff bool) any {
	p := r.Body()
	var status string
	if staff {
		status = enumField(p, "status", "open", "resolved", "closed")
	} else {
		status = enumField(p, "status", "open", "closed")
	}
	a.Tx(func(s Store) {
		ticket := s.ticket(r.ID(), r.UID(), staff, true)
		if ticket["status"] == status {
			return
		}
		s.Exec("UPDATE support_tickets SET status=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", status, r.ID())
		s.Audit(r.UID(), "ticket.status", Row{"id": r.ID(), "status": status, "staff": staff})
	})
	return a.Store.ticketDetail(a.Store.ticket(r.ID(), r.UID(), staff, false))
}
func (a *App) ticketRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/tickets", 2, func(r *Request) any { return a.listTickets(r, false) })
	a.route(m, "POST /api/tickets", 2, a.createTicket)
	a.route(m, "GET /api/tickets/{id}", 2, func(r *Request) any { return a.Store.ticketDetail(a.Store.ticket(r.ID(), r.UID(), false, false)) })
	a.route(m, "POST /api/tickets/{id}/messages", 2, func(r *Request) any { return a.replyTicket(r, false) })
	a.route(m, "PATCH /api/tickets/{id}", 2, func(r *Request) any { return a.updateTicket(r, false) })
	a.route(m, "GET /api/admin/tickets", 3, func(r *Request) any { return a.listTickets(r, true) })
	a.route(m, "GET /api/admin/tickets/{id}", 3, func(r *Request) any { return a.Store.ticketDetail(a.Store.ticket(r.ID(), r.UID(), true, false)) })
	a.route(m, "POST /api/admin/tickets/{id}/messages", 3, func(r *Request) any { return a.replyTicket(r, true) })
	a.route(m, "PATCH /api/admin/tickets/{id}", 3, func(r *Request) any { return a.updateTicket(r, true) })
}
