package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSupportTickets(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	anonymous := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "ticket-owner", "password": "Ticket-pass-2026"}, 200))
	outsider := object(other.ok(t, "POST", "/auth/register", Row{"username": "ticket-other", "password": "Ticket-pass-2026"}, 200))
	anonymous.ok(t, "GET", "/tickets", nil, 401)
	user.ok(t, "GET", "/admin/tickets", nil, 403)
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": "nanobanana", "prompt": "support linked job"}, 202))
	other.ok(t, "POST", "/tickets", Row{"subject": "Invalid job reference", "category": "generation", "content": "Please inspect this job", "jobId": job["id"]}, 400)
	user.ok(t, "POST", "/tickets", Row{"subject": "a", "category": "generation", "content": "short"}, 400)
	ticket := object(user.ok(t, "POST", "/tickets", Row{"subject": "生成任务需要核对", "category": "generation", "content": "生成时出现问题，请核对这次任务。", "jobId": job["id"], "userId": outsider["id"]}, 201))
	ticketID := str(ticket["id"])
	path := "/tickets/" + ticketID
	adminPath := "/admin/tickets/" + ticketID
	want(t, ticket["userId"], owner["id"])
	want(t, ticket["status"], "open")
	want(t, len(array(ticket["messages"])), 1)
	want(t, object(ticket["relatedJob"])["id"], job["id"])
	other.ok(t, "GET", path, nil, 404)
	other.ok(t, "POST", path+"/messages", Row{"content": "Unauthorized"}, 404)
	other.ok(t, "PATCH", path, Row{"status": "closed"}, 404)
	want(t, object(other.ok(t, "GET", "/tickets", nil, 200))["total"], 0)
	user.ok(t, "GET", adminPath, nil, 403)
	key := object(user.ok(t, "POST", "/keys", Row{"name": "cannot access support"}, 201))
	keyClient := &testClient{server: server.URL, key: str(key["token"])}
	keyClient.ok(t, "GET", path, nil, 403)
	keyClient.ok(t, "POST", "/tickets", Row{"subject": "API ticket", "category": "api", "content": "Not allowed with an API key"}, 403)
	staffReply := object(admin.ok(t, "POST", adminPath+"/messages", Row{"content": "已收到，请补充发生时间。"}, 201))
	want(t, staffReply["status"], "replied")
	want(t, object(array(staffReply["messages"])[1])["authorRole"], "staff")
	reply := object(user.ok(t, "POST", path+"/messages", Row{"content": "问题发生在今天下午。"}, 201))
	want(t, reply["status"], "open")
	want(t, len(array(reply["messages"])), 3)
	user.ok(t, "PATCH", path, Row{"status": "resolved"}, 400)
	want(t, object(user.ok(t, "PATCH", path, Row{"status": "closed"}, 200))["status"], "closed")
	user.ok(t, "POST", path+"/messages", Row{"content": "Do not append to closed ticket"}, 409)
	admin.ok(t, "POST", adminPath+"/messages", Row{"content": "Do not append to closed ticket"}, 409)
	want(t, len(array(object(user.ok(t, "GET", path, nil, 200))["messages"])), 3)
	user.ok(t, "PATCH", path, Row{"status": "open"}, 200)
	admin.ok(t, "POST", adminPath+"/messages", Row{"content": "已核对，本次任务可正常处理。"}, 201)
	want(t, object(admin.ok(t, "PATCH", adminPath, Row{"status": "resolved"}, 200))["status"], "resolved")
	second := object(user.ok(t, "POST", "/tickets", Row{"subject": "积分兑换问题", "category": "billing", "content": "需要查询一次充值的到账情况。"}, 201))
	want(t, object(user.ok(t, "GET", "/tickets?status=open", nil, 200))["total"], 1)
	want(t, object(user.ok(t, "GET", "/tickets?category=billing", nil, 200))["total"], 1)
	want(t, object(user.ok(t, "GET", "/tickets?search="+url.QueryEscape(str(ticket["number"])), nil, 200))["total"], 1)
	firstPage := object(admin.ok(t, "GET", "/admin/tickets?limit=1", nil, 200))
	secondPage := object(admin.ok(t, "GET", "/admin/tickets?limit=1&page=2", nil, 200))
	want(t, firstPage["total"], 2)
	if object(array(firstPage["items"])[0])["id"] == object(array(secondPage["items"])[0])["id"] {
		t.Fatal("ticket pagination repeats records")
	}
	user.ok(t, "GET", "/tickets?status=invalid", nil, 400)
	admin.ok(t, "GET", "/admin/tickets?category=invalid", nil, 400)
	user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
	user.ok(t, "DELETE", "/jobs/"+str(job["id"]), nil, 200)
	deletedJobTicket := object(user.ok(t, "GET", path, nil, 200))
	if deletedJobTicket["relatedJob"] != nil {
		t.Fatal("deleted job should not remain in related details")
	}
	want(t, deletedJobTicket["relatedJobId"], job["id"])
	restarted, e := Open(a.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if e = restarted.Migrate(); e != nil {
		t.Fatal(e)
	}
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()
	returning := &testClient{server: restartedServer.URL, cookie: user.cookie}
	persisted := object(returning.ok(t, "GET", path, nil, 200))
	want(t, len(array(persisted["messages"])), 4)
	want(t, persisted["status"], "resolved")
	returning.ok(t, "GET", "/tickets/"+str(second["id"]), nil, 200)
	logs := jsonText(admin.ok(t, "GET", "/admin/audit", nil, 200))
	if !strings.Contains(logs, "ticket.reply") || !strings.Contains(logs, "ticket.status") {
		t.Fatal("ticket administration must be audited")
	}
}

func TestLedgerAndRedemption(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	other := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "ledger-owner", "password": "Ledger-pass-2026"}, 200))
	outsider := object(other.ok(t, "POST", "/auth/register", Row{"username": "ledger-other", "password": "Ledger-pass-2026"}, 200))
	var refundID string
	a.Tx(func(s Store) {
		s.Wallet(str(owner["id"]), -2000, "generation", "Ledger generation", nil)
		refundID = s.Wallet(str(owner["id"]), 500, "refund", "Ledger refund", nil)
		s.Wallet(str(outsider["id"]), 99900, "adjustment", "Other user's private record", nil)
	})
	codes := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 25}, 201))
	receipt := object(user.ok(t, "POST", "/redeem", Row{"code": codes[0]}, 200))
	want(t, receipt["credits"], 25)
	want(t, receipt["balance"], 110)
	if str(receipt["transactionId"]) == "" {
		t.Fatal("redemption receipt needs a transaction ID")
	}
	row := a.Store.One("SELECT * FROM transactions WHERE id=?", receipt["transactionId"])
	want(t, row["user_id"], owner["id"])
	want(t, row["amount"], 2500)
	user.ok(t, "POST", "/redeem", Row{"code": codes[0]}, 400)
	expired := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 9, "expiresAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 201))
	user.ok(t, "POST", "/redeem", Row{"code": expired[0]}, 400)
	all := object(user.ok(t, "GET", "/transactions", nil, 200))
	want(t, all["total"], 4)
	summary := object(all["summary"])
	want(t, summary["balance"], 110)
	want(t, summary["income"], 130)
	want(t, summary["expense"], 20)
	if strings.Contains(jsonText(all), "Other user's private") {
		t.Fatal("ledger leaked another user's records")
	}
	want(t, object(user.ok(t, "GET", "/transactions?user_id="+str(outsider["id"]), nil, 200))["total"], 4)
	deposits := object(user.ok(t, "GET", "/transactions?kind=redeem", nil, 200))
	want(t, deposits["total"], 1)
	want(t, object(deposits["summary"])["income"], 25)
	outgoing := object(user.ok(t, "GET", "/transactions?direction=out", nil, 200))
	want(t, outgoing["total"], 1)
	want(t, object(outgoing["summary"])["expense"], 20)
	want(t, object(user.ok(t, "GET", "/transactions?search="+str(receipt["transactionId"]), nil, 200))["total"], 1)
	a.Store.Exec("UPDATE transactions SET created_at='2026-01-01 12:00:00' WHERE id=?", refundID)
	old := object(user.ok(t, "GET", "/transactions?from=2026-01-01&to=2026-01-01", nil, 200))
	want(t, old["total"], 1)
	want(t, object(old["summary"])["income"], 5)
	firstPage := object(user.ok(t, "GET", "/transactions?limit=1", nil, 200))
	secondPage := object(user.ok(t, "GET", "/transactions?limit=1&page=2", nil, 200))
	if object(array(firstPage["items"])[0])["id"] == object(array(secondPage["items"])[0])["id"] {
		t.Fatal("ledger pagination repeats records")
	}
	for _, query := range []string{"kind=invalid", "direction=invalid", "from=invalid", "from=2026-02-02&to=2026-01-01"} {
		user.ok(t, "GET", "/transactions?"+query, nil, 400)
	}
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 110)
}
