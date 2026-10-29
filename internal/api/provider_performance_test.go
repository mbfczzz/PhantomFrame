package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countedQueries struct {
	Querier
	calls atomic.Int64
}

func (q *countedQueries) QueryContext(ctx context.Context, sqlText string, args ...any) (*sql.Rows, error) {
	q.calls.Add(1)
	return q.Querier.QueryContext(ctx, sqlText, args...)
}
func (q *countedQueries) ExecContext(ctx context.Context, sqlText string, args ...any) (sql.Result, error) {
	q.calls.Add(1)
	return q.Querier.ExecContext(ctx, sqlText, args...)
}

// Explicit diagnostic only: -run '^$' -bench '^BenchmarkProviderAPI$' -benchtime=1x.
// It owns an isolated database, cancels submit-latency probes, then processes
// six mock-only text tasks to measure dispatcher waiting with two workers.
func BenchmarkProviderAPI(b *testing.B) {
	if b.N != 1 {
		b.Fatal("run this diagnostic with -benchtime=1x")
	}
	b.StopTimer()
	a := setupIntegration(b)
	a.Config.SupplierInstances = true
	a.Config.Demo = false
	if err := a.Migrate(); err != nil {
		b.Fatal(err)
	}
	a.Seed()
	var upstreamCalls atomic.Int64
	var generationCalls atomic.Int64
	var startsMu sync.Mutex
	starts := map[string]time.Time{}
	directory := []Row{}
	for i := 0; i < 1000; i++ {
		directory = append(directory, Row{"id": fmt.Sprintf("model-%04d", i), "type": "text"})
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
			generationCalls.Add(1)
			startsMu.Lock()
			starts[r.Header.Get("Idempotency-Key")] = time.Now()
			startsMu.Unlock()
			time.Sleep(100 * time.Millisecond)
			send(w, 200, Row{"choices": []Row{{"message": Row{"content": "local performance fixture"}}}})
			return
		}
		upstreamCalls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			http.Error(w, "unexpected paid endpoint", 500)
			return
		}
		time.Sleep(50 * time.Millisecond)
		send(w, 200, Row{"data": directory})
	}))
	defer mock.Close()
	params := jsonText(inventoryParameters(InventoryRoute{Vendor: "apiyi", Model: "perf-text", Upstream: "perf-text", Modality: "text", Config: Row{}}))
	a.Tx(func(s Store) {
		for p := 0; p < 20; p++ {
			pid := fmt.Sprintf("perf-provider-%02d", p)
			s.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,config) VALUES(?,?,'native:apiyi',?,?,'{}')", pid, pid, mock.URL, a.encrypt("performance-fixture-key"))
			for m := 0; m < 50; m++ {
				mid, up := fmt.Sprintf("perf-model-%02d-%04d", p, m), fmt.Sprintf("model-%04d", m)
				s.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,parameters,tags) VALUES(?,?,'text',?,?,'',?,'[]')", mid, mid, pid, up, params)
				s.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters) VALUES(?,?,?,?,'text','',?)", id(), pid, up, up, params)
				s.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,'base','request',25,'{}'),(?,?,'conditional','request',50,'{\"maxOutputTokens\":1024}')", id(), mid, id(), mid)
			}
		}
	})
	counts := &countedQueries{Querier: a.DB}
	a.Store = Store{counts}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	client := &http.Client{Timeout: 45 * time.Second}
	cookie := ""
	request := func(method, path string, payload any) (int, []byte, float64, error) {
		body := ""
		if payload != nil {
			body = jsonText(payload)
		}
		req, err := http.NewRequest(method, server.URL+"/api"+path, strings.NewReader(body))
		if err != nil {
			return 0, nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		started := time.Now()
		res, err := client.Do(req)
		if err != nil {
			return 0, nil, 0, err
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		if path == "/auth/login" {
			for _, c := range res.Cookies() {
				if c.Name == "pf_session" {
					cookie = c.Name + "=" + c.Value
				}
			}
		}
		return res.StatusCode, data, elapsed, err
	}
	if status, _, _, err := request("POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}); err != nil || status != 200 {
		b.Fatalf("login failed: %d %v", status, err)
	}
	type spec struct {
		name, method, path string
		payload            any
		status, n, workers int
		cancel             bool
	}
	results := []Row{}
	measure := func(s spec) {
		var mu sync.Mutex
		times := []float64{}
		failures := []string{}
		sizes := 0
		var firstQueries int64
		call := func(record bool) {
			before := counts.calls.Load()
			status, body, ms, err := request(s.method, s.path, s.payload)
			queries := counts.calls.Load() - before
			mu.Lock()
			if err != nil || status != s.status {
				failures = append(failures, fmt.Sprintf("status=%d err=%v", status, err))
			}
			if record {
				times = append(times, ms)
				sizes += len(body)
				if len(times) == 1 {
					firstQueries = queries
				}
			}
			mu.Unlock()
			if s.cancel && status == 202 {
				var j Row
				if json.Unmarshal(body, &j) == nil {
					status, _, _, err := request("POST", "/jobs/"+str(j["id"])+"/cancel", Row{})
					if err != nil || status != 200 {
						mu.Lock()
						failures = append(failures, "cancel failed")
						mu.Unlock()
					}
				}
			}
		}
		if s.n > 1 && s.workers == 1 {
			call(false)
		}
		work := make(chan int, s.n)
		for i := 0; i < s.n; i++ {
			work <- i
		}
		close(work)
		var wg sync.WaitGroup
		for i := 0; i < s.workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range work {
					call(true)
				}
			}()
		}
		wg.Wait()
		if len(failures) > 0 {
			b.Fatalf("%s: %v", s.name, failures)
		}
		sort.Float64s(times)
		percentile := func(p float64) float64 { index := int(float64(len(times)-1)*p + 0.5); return times[index] }
		row := Row{"name": s.name, "samples": s.n, "workers": s.workers, "p50_ms": percentile(.5), "p95_ms": percentile(.95), "max_ms": times[len(times)-1], "response_bytes": sizes / s.n}
		if s.workers == 1 && (s.method == "GET" || s.name == "quote") {
			row["sql_calls_first_sample"] = firstQueries
		}
		results = append(results, row)
	}
	b.StartTimer()
	for _, s := range []spec{
		{"health", "GET", "/health", nil, 200, 12, 1, false},
		{"protocols", "GET", "/admin/provider-protocols", nil, 200, 12, 1, false},
		{"suppliers", "GET", "/admin/providers", nil, 200, 12, 1, false},
		{"catalog_page20", "GET", "/admin/providers/perf-provider-00/models?limit=20", nil, 200, 12, 1, false},
		{"admin_models_1000", "GET", "/admin/models", nil, 200, 12, 1, false},
		{"public_models_1000", "GET", "/models", nil, 200, 12, 1, false},
		{"quote", "POST", "/pricing/quote", Row{"modelId": "perf-model-00-0000", "params": Row{}}, 200, 12, 1, false},
		{"submit_queue", "POST", "/jobs", Row{"modelId": "perf-model-00-0000", "prompt": "performance fixture", "params": Row{}}, 202, 8, 1, true},
		{"models_concurrency4", "GET", "/models", nil, 200, 20, 4, false},
		{"submit_concurrency4", "POST", "/jobs", Row{"modelId": "perf-model-00-0000", "prompt": "performance fixture", "params": Row{}}, 202, 12, 4, true},
		{"sync_first_1000_50ms_upstream", "POST", "/admin/providers/perf-provider-00/sync-models", Row{}, 200, 1, 1, false},
		{"sync_repeat_1000_50ms_upstream", "POST", "/admin/providers/perf-provider-00/sync-models", Row{}, 200, 5, 1, false},
	} {
		measure(s)
	}
	// The timing probes above were all cancelled before any worker was started.
	if balance := integer(a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"]); balance != 100000 {
		b.Fatalf("cancelled probes changed balance: %d", balance)
	}
	a.Redis.Del(context.Background(), a.Config.RedisPrefix+"jobs:wake")
	ctx, stopWorker := context.WithCancel(context.Background())
	workerDone := a.StartWorker(ctx)
	defer func() { stopWorker(); <-workerDone }()
	submitted := map[string]time.Time{}
	for i := 0; i < 6; i++ {
		started := time.Now()
		status, data, _, err := request("POST", "/jobs", Row{"modelId": "perf-model-00-0000", "prompt": "mock queue timing", "params": Row{}})
		if err != nil || status != 202 {
			b.Fatalf("queue probe rejected: %d %v", status, err)
		}
		var j Row
		if err := json.Unmarshal(data, &j); err != nil {
			b.Fatal(err)
		}
		submitted[str(j["id"])] = started
	}
	delays, totals := []float64{}, []float64{}
	for deadline := time.Now().Add(10 * time.Second); len(submitted) > 0 && time.Now().Before(deadline); {
		for jid, started := range submitted {
			j := a.Store.One("SELECT status,finished_at FROM jobs WHERE id=?", jid)
			if j["status"] == "failed" || j["status"] == "refund_pending" {
				b.Fatalf("mock queue task failed: %s", jid)
			}
			if j["status"] == "succeeded" {
				startsMu.Lock()
				firstRequest := starts[jid]
				startsMu.Unlock()
				if firstRequest.IsZero() {
					b.Fatal("mock job has no upstream request")
				}
				delays = append(delays, float64(firstRequest.Sub(started).Microseconds())/1000)
				totals = append(totals, float64(parseTime(str(j["finished_at"])).Sub(started).Microseconds())/1000)
				delete(submitted, jid)
			}
		}
		if len(submitted) > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(submitted) != 0 || generationCalls.Load() != 6 {
		b.Fatal("mock queue did not finish exactly six calls")
	}
	if balance := integer(a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"]); balance != 99850 {
		b.Fatalf("mock jobs did not charge exactly once: %d", balance)
	}
	sort.Float64s(delays)
	sort.Float64s(totals)
	queue := Row{"jobs": 6, "workers": 2, "mock_generation_delay_ms": 100, "first_dispatch_ms": delays[0], "last_dispatch_ms": delays[5], "median_completion_ms": totals[3], "last_completion_ms": totals[5]}
	b.StopTimer()
	report := Row{"dataset": Row{"suppliers": 20, "published_models": 1000, "model_type": "text", "price_rules": 2000, "initial_catalog_entries": 1000, "sync_payload_models": 1000, "mock_directory_delay_ms": 50}, "results": results, "queue": queue, "mock_generation_calls": generationCalls.Load(), "mock_directory_calls": upstreamCalls.Load(), "real_generation_calls": 0, "real_payment_calls": 0}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		b.Fatal(err)
	}
	if path := os.Getenv("PF_PERFORMANCE_REPORT"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			b.Fatal(err)
		}
	}
	b.Log("PERFORMANCE " + jsonText(report))
}
