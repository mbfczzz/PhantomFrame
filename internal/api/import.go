package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ImportSQLite imports an offline snapshot, preserving IDs, integer balances,
// password hashes and AES-GCM provider secrets. Existing targets are refused.
func (a *App) ImportSQLite(path string) (map[string]int, error) {
	var snapshot struct {
		Version int              `json:"version"`
		Tables  map[string][]Row `json:"tables"`
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &snapshot); e != nil {
		return nil, e
	}
	if snapshot.Version != 1 {
		return nil, fmt.Errorf("unsupported export version")
	}
	columns := map[string]string{
		"users":        "id,username,display_name,password,role,balance,status,parent_id,created_at",
		"providers":    "id,name,protocol,base_url,api_key,enabled,config,created_at",
		"models":       "id,name,type,provider_id,upstream_model,description,category,enabled,sort_order,tags,parameters,created_at",
		"prices":       "id,model_id,label,unit,credits,conditions,enabled",
		"jobs":         "id,user_id,model_id,prompt,params,status,cost,outputs,error,source,idempotency_key,upstream_id,provider_snapshot,refunded,created_at,finished_at",
		"transactions": "id,user_id,amount,balance,kind,description,job_id,created_at",
		"assets":       "id,user_id,name,type,mime,file,size,job_id,created_at",
		"projects":     "id,user_id,title,data,version,created_at,updated_at",
		"api_keys":     "id,user_id,name,prefix,token,last_used_at,created_at",
		"redeem_codes": "id,code,credits,used_by,used_at,expires_at,created_at",
		"settings":     "key,value", "migrations": "name,applied_at", "audit_log": "id,user_id,action,detail,created_at",
	}
	order := []string{"users", "providers", "models", "prices", "jobs", "transactions", "assets", "projects", "api_keys", "redeem_codes", "settings", "migrations", "audit_log"}
	for _, p := range snapshot.Tables["providers"] {
		if _, e = a.decrypt(str(p["api_key"])); e != nil {
			return nil, fmt.Errorf("provider encryption key mismatch; preserve the original data/.encryption-key")
		}
	}
	for _, f := range snapshot.Tables["assets"] {
		if _, e = os.Stat(a.filePath(str(f["file"]))); e != nil {
			return nil, fmt.Errorf("asset file missing: %s", str(f["id"]))
		}
	}
	counts := map[string]int{}
	e = try(func() {
		a.Tx(func(s Store) {
			for _, table := range order {
				if integer(s.One("SELECT count(*) n FROM `" + table + "`")["n"]) != 0 {
					fail(409, "目标 MySQL 数据库不是空库，已停止导入以保护现有数据")
				}
			}
			for _, table := range order {
				cols := strings.Split(columns[table], ",")
				quoted := make([]string, len(cols))
				marks := make([]string, len(cols))
				for i, k := range cols {
					quoted[i] = "`" + k + "`"
					marks[i] = "?"
				}
				q := "INSERT INTO `" + table + "` (" + strings.Join(quoted, ",") + ") VALUES(" + strings.Join(marks, ",") + ")"
				for _, row := range snapshot.Tables[table] {
					args := make([]any, len(cols))
					for i, k := range cols {
						v := row[k]
						if table == "users" && k == "parent_id" {
							v = nil
						}
						if strings.HasSuffix(k, "_at") && v != nil {
							t := parseTime(v)
							if t.IsZero() {
								fail(400, "导入时间格式无效："+table+"."+k)
							}
							v = t
						}
						args[i] = v
					}
					s.Exec(q, args...)
				}
				counts[table] = len(snapshot.Tables[table])
			}
			for _, u := range snapshot.Tables["users"] {
				if u["parent_id"] != nil {
					s.Exec("UPDATE users SET parent_id=? WHERE id=?", u["parent_id"], u["id"])
				}
			}
			s.Exec("INSERT INTO migrations(name) VALUES('sqlite-to-mysql-v1')")
		})
	})
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, session := range snapshot.Tables["sessions"] {
		ttl := time.Until(time.UnixMilli(integer(session["expires_at"])))
		if ttl <= 0 {
			continue
		}
		e = a.Redis.Set(ctx, a.Config.RedisPrefix+"session:"+str(session["token"]), jsonText(Row{"userId": session["user_id"], "version": 0}), ttl).Err()
		if e != nil {
			return counts, fmt.Errorf("data imported; session import failed: %w", e)
		}
		counts["sessions"]++
	}
	return counts, nil
}
