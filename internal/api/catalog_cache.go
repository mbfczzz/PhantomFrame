package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// The public model catalog is rebuilt from several tables and parsed JSON
// columns. It only changes through admin writes, which bump a shared version in
// Redis; every instance keeps the encoded response for the current version.
const catalogCacheMaxAge = 30 * time.Second

type catalogCache struct {
	mu      sync.Mutex
	version string
	built   time.Time
	body    json.RawMessage
}

func (a *App) catalogVersionKey() string { return a.Config.RedisPrefix + "catalog:version" }

func (a *App) bumpCatalogVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = a.Redis.Incr(ctx, a.catalogVersionKey()).Err()
	_ = a.Redis.Del(ctx, a.Config.RedisPrefix+"admin:totals").Err()
	a.catalog.mu.Lock()
	a.catalog.body = nil
	a.catalog.mu.Unlock()
}

func (a *App) cachedCatalog(build func() any) any {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	version, err := a.Redis.Get(ctx, a.catalogVersionKey()).Result()
	if err != nil && err != redis.Nil {
		return build()
	}
	a.catalog.mu.Lock()
	if a.catalog.body != nil && a.catalog.version == version && time.Since(a.catalog.built) < catalogCacheMaxAge {
		body := a.catalog.body
		a.catalog.mu.Unlock()
		return body
	}
	a.catalog.mu.Unlock()
	body, err := json.Marshal(build())
	if err != nil {
		panic(err)
	}
	a.catalog.mu.Lock()
	a.catalog.version, a.catalog.built, a.catalog.body = version, time.Now(), body
	a.catalog.mu.Unlock()
	return json.RawMessage(body)
}

const adminTotalsMaxAge = 30 * time.Second

func (a *App) cachedAdminTotals() Row {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	key := a.Config.RedisPrefix + "admin:totals"
	if raw, err := a.Redis.Get(ctx, key).Result(); err == nil {
		if cached := object(raw); len(cached) > 0 {
			return cached
		}
	}
	v := Row{}
	for _, name := range []string{"users", "jobs"} {
		v[name] = a.Store.One("SELECT count(*) n FROM " + name)["n"]
	}
	v["consumed"] = credits(a.Store.One("SELECT coalesce(sum(-amount),0) n FROM transactions WHERE kind IN ('generation','refund')")["n"])
	v["daily"] = a.Store.All("SELECT date(created_at) date,count(*) count,sum(status='succeeded') succeeded,sum(CASE WHEN refunded=0 THEN cost ELSE 0 END)/100.0 credits FROM jobs WHERE created_at>=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 14 DAY) GROUP BY date(created_at)")
	v["totalsAt"] = time.Now().UTC().Format(time.RFC3339)
	_ = a.Redis.Set(ctx, key, jsonText(v), adminTotalsMaxAge).Err()
	return v
}
