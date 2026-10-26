package api

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	assets "phantomframe/server"
)

//go:embed schema.sql
var schema string

type Row = map[string]any
type Querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type Store struct{ Q Querier }
type App struct {
	Config  Config
	DB      *sql.DB
	Store   Store
	Redis   *redis.Client
	OSSHTTP *http.Client // Optional transport override for isolated storage tests.
}

func Open(c Config) (*App, error) {
	cfg := mysql.NewConfig()
	cfg.User = c.MySQLUser
	cfg.Passwd = c.MySQLPassword
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.MySQLHost, c.MySQLPort)
	cfg.DBName = c.MySQLDatabase
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	cfg.Collation = "utf8mb4_bin"
	cfg.ClientFoundRows = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(3 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect MySQL: %w", err)
	}
	rc := redis.NewClient(&redis.Options{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})
	if err = rc.Ping(ctx).Err(); err != nil {
		db.Close()
		rc.Close()
		return nil, fmt.Errorf("connect Redis: %w", err)
	}
	return &App{Config: c, DB: db, Store: Store{db}, Redis: rc}, nil
}
func (a *App) Close() { a.Redis.Close(); a.DB.Close() }
func (a *App) Migrate() error {
	if err := a.checkDatabaseCompatibility(); err != nil {
		return err
	}
	for _, q := range strings.Split(schema, ";") {
		if strings.TrimSpace(q) == "" {
			continue
		}
		if _, err := a.DB.Exec(q); err != nil {
			return err
		}
	}
	var pricePrecisionColumns int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='prices' AND COLUMN_NAME='unit_price_micros'").Scan(&pricePrecisionColumns); err != nil {
		return err
	}
	if pricePrecisionColumns == 0 {
		if _, err := a.DB.Exec("ALTER TABLE prices ADD COLUMN unit_price_micros BIGINT NULL AFTER credits"); err != nil {
			if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1060 {
				return err
			}
		}
	}
	var extraColumns int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='prices' AND COLUMN_NAME='billing_extras'").Scan(&extraColumns); err != nil {
		return err
	}
	if extraColumns == 0 {
		if _, err := a.DB.Exec("ALTER TABLE prices ADD COLUMN billing_extras JSON NULL"); err != nil {
			if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1060 {
				return err
			}
		}
	}
	var modelKeyColumns int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='provider_models' AND COLUMN_NAME='api_key'").Scan(&modelKeyColumns); err != nil {
		return err
	}
	if modelKeyColumns == 0 {
		if _, err := a.DB.Exec("ALTER TABLE provider_models ADD COLUMN api_key TEXT NULL"); err != nil {
			if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1060 {
				return err
			}
		}
	}
	var modelStatusIndexes int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='jobs' AND INDEX_NAME='jobs_model_finished'").Scan(&modelStatusIndexes); err != nil {
		return err
	}
	if modelStatusIndexes == 0 {
		if _, err := a.DB.Exec("ALTER TABLE jobs ADD INDEX jobs_model_finished(model_id,finished_at,seq)"); err != nil {
			if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1061 {
				return err
			}
		}
	}
	var remoteColumns int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='assets' AND COLUMN_NAME='remote_storage'").Scan(&remoteColumns); err != nil {
		return err
	}
	if remoteColumns == 0 {
		if _, err := a.DB.Exec("ALTER TABLE assets ADD COLUMN remote_storage JSON NULL"); err != nil {
			if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1060 {
				return err
			}
		}
	}
	return a.migrateImageRequests()
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
func (s Store) All(q string, args ...any) []Row {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := s.Q.QueryContext(ctx, q, args...)
	must(err)
	defer rows.Close()
	cols, err := rows.Columns()
	must(err)
	types, err := rows.ColumnTypes()
	must(err)
	result := []Row{}
	for rows.Next() {
		v := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range v {
			p[i] = &v[i]
		}
		must(rows.Scan(p...))
		r := Row{}
		for i, k := range cols {
			switch x := v[i].(type) {
			case []byte:
				t := types[i].DatabaseTypeName()
				if t == "DECIMAL" || t == "NEWDECIMAL" {
					n, e := strconv.ParseFloat(string(x), 64)
					must(e)
					r[k] = n
				} else {
					r[k] = string(x)
				}
			case time.Time:
				if types[i].DatabaseTypeName() == "DATE" {
					r[k] = x.UTC().Format("2006-01-02")
				} else {
					r[k] = x.UTC().Format("2006-01-02 15:04:05.000")
				}
			default:
				r[k] = x
			}
		}
		result = append(result, r)
	}
	must(rows.Err())
	return result
}
func (s Store) One(q string, args ...any) Row {
	rows := s.All(q, args...)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
func (s Store) Exec(q string, args ...any) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, e := s.Q.ExecContext(ctx, q, args...)
	must(e)
	n, e := r.RowsAffected()
	must(e)
	return n
}
func (a *App) Tx(f func(Store)) {
	tx, e := a.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	must(e)
	defer tx.Rollback()
	f(Store{tx})
	must(tx.Commit())
}
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case int:
		return float64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		f, _ := strconv.ParseFloat(str(v), 64)
		return f
	}
}
func truth(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return num(v) != 0
}
func integer(v any) int64   { return int64(num(v)) }
func units(v any) int64     { return int64(math.Round(num(v) * 100)) }
func credits(v any) float64 { return num(v) / 100 }
func jsonText(v any) string { b, e := json.Marshal(v); must(e); return string(b) }
func object(v any) Row {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	r := Row{}
	_ = json.Unmarshal([]byte(str(v)), &r)
	if r == nil {
		return Row{}
	}
	return r
}
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	r := []any{}
	_ = json.Unmarshal([]byte(str(v)), &r)
	if r == nil {
		return []any{}
	}
	return r
}
func clone(r Row) Row {
	m := Row{}
	for k, v := range r {
		m[k] = v
	}
	return m
}
func (s Store) Wallet(uid string, amount int64, kind, description string, job any) string {
	if s.Exec("UPDATE users SET balance=balance+? WHERE id=? AND balance+?>=0", amount, uid, amount) == 0 {
		fail(402, "余额不足")
	}
	u := s.One("SELECT balance FROM users WHERE id=?", uid)
	transactionID := id()
	s.Exec("INSERT INTO transactions(id,user_id,amount,balance,kind,description,job_id) VALUES(?,?,?,?,?,?,?)", transactionID, uid, amount, u["balance"], kind, description, job)
	return transactionID
}
func (s Store) Audit(uid, action string, detail any) {
	s.Exec("INSERT INTO audit_log(id,user_id,action,detail) VALUES(?,?,?,?)", id(), uid, action, jsonText(detail))
}
func publicUser(u Row) Row {
	return Row{"id": u["id"], "username": u["username"], "displayName": u["display_name"], "role": u["role"], "balance": credits(u["balance"]), "currency": currencyCode, "status": u["status"], "parentId": u["parent_id"], "createdAt": u["created_at"]}
}
func (s Store) ModelView(m Row) Row {
	p := s.One("SELECT name,protocol FROM providers WHERE id=?", m["provider_id"])
	v := clone(m)
	v["enabled"] = truth(m["enabled"])
	v["tags"] = array(m["tags"])
	v["parameters"] = imageRequestParameters(str(m["type"]), array(m["parameters"]))
	if m["type"] == "image" {
		v["maxOutputsPerRequest"] = imageOutputLimit(str(m["upstream_model"]))
		v["description"] = imageRequestDescription("image", str(m["upstream_model"]), str(m["description"]))
	}
	v["providerName"] = p["name"]
	v["demo"] = p["protocol"] == "demo"
	prices := s.All("SELECT * FROM prices WHERE model_id=? ORDER BY credits", m["id"])
	v["pricingVersion"] = pricingVersion(prices)
	for _, p := range prices {
		priceView(p)
	}
	v["prices"] = prices
	return v
}
func priceView(p Row) Row {
	p["currency"] = currencyCode
	p["credits"] = float64(priceMicros(p)) / 1000000
	delete(p, "unit_price_micros")
	p["extras"] = object(p["billing_extras"])
	delete(p, "billing_extras")
	p["conditions"] = object(p["conditions"])
	p["enabled"] = truth(p["enabled"])
	return p
}
func passwordHash(s string) string {
	b, e := bcrypt.GenerateFromPassword([]byte(s), 12)
	must(e)
	return string(b)
}
func (a *App) Seed() {
	a.Tx(func(s Store) {
		if s.One("SELECT id FROM users WHERE role='admin' LIMIT 1") == nil {
			username := env("ADMIN_USERNAME", "admin")
			pass := env("ADMIN_PASSWORD", "")
			generated := pass == ""
			if generated {
				pass = token()[:24]
			}
			balance := 0
			if a.Config.Demo {
				balance = 100000
			}
			s.Exec("INSERT INTO users(id,username,display_name,password,role,balance) VALUES(?,?,?,?,?,?)", id(), username, "管理员", passwordHash(pass), "admin", balance)
			if generated {
				must(os.WriteFile(".local-credentials.json", []byte(jsonText(Row{"username": username, "password": pass, "note": "本地管理员凭据，请妥善保管"})), 0600))
			}
		}
		welcome := "0"
		if a.Config.Demo {
			welcome = "100"
		}
		for k, v := range map[string]string{"siteName": "PhantomFrame", "subtitle": "让每一个想法，成为独特的一帧。", "registrationEnabled": "true", "welcomeCredits": welcome, "support": "", "announcement": "", "purchaseUrl": ""} {
			s.Exec("INSERT IGNORE INTO settings(`key`,value) VALUES(?,?)", k, v)
		}
		if !a.Config.Demo || s.One("SELECT id FROM providers LIMIT 1") != nil {
			return
		}
		s.Exec("INSERT INTO providers(id,name,protocol,api_key,config,enabled) VALUES('demo',?,'demo','','{}',?)", "本地演示通道", a.Config.Demo)
		var models []Row
		must(json.Unmarshal(assets.Models, &models))
		for _, m := range models {
			params := []any{}
			for _, raw := range array(m["params_schema"]) {
				p := clone(object(raw))
				if p["name"] == "prompt" {
					continue
				}
				switch p["type"] {
				case "[]string":
					p["type"] = "assets"
				case "string":
					p["type"] = "text"
				}
				params = append(params, p)
			}
			for _, pr := range array(m["pricing"]) {
				p := object(pr)
				parts := strings.Split(str(p["size_key"]), ":")
				if len(parts) == 3 {
					found := false
					for _, f := range params {
						if object(f)["name"] == parts[1] {
							found = true
						}
					}
					if !found {
						params = append(params, Row{"name": parts[1], "label": "分辨率", "type": "select", "default": parts[2], "options": []any{Row{"l": parts[2], "v": parts[2]}}})
					}
				}
			}
			category := "默认分组"
			if groups := array(m["groups"]); len(groups) > 0 {
				category = str(object(groups[0])["name"])
			}
			tag := "图片生成"
			if m["type"] == "video" {
				tag = "视频生成"
			}
			s.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,tags,parameters,sort_order) VALUES(?,?,?,'demo',?,?,?,?,?,?)", m["model_id"], m["display_name"], m["type"], m["model_id"], str(m["description"]), category, jsonText([]string{tag}), jsonText(params), integer(m["sort_order"]))
			for _, pr := range array(m["pricing"]) {
				p := object(pr)
				parts := strings.Split(str(p["size_key"]), ":")
				unit, label, conditions := "request", "统一价格", Row{}
				if parts[0] == "per_sec" {
					unit = "second"
				}
				if len(parts) == 3 {
					label = parts[2]
					conditions[parts[1]] = parts[2]
				}
				s.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,?,?,?,?)", id(), m["model_id"], label, unit, units(p["credits"]), jsonText(conditions))
			}
		}
	})
	a.backfillProviderModels()
}
