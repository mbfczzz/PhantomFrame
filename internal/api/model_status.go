package api

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

// ModelStatus contains anonymous aggregate results only. Job payloads, identities,
// outputs and provider credentials must never be included in this public view.
func (s Store) ModelStatus(ids []string, now time.Time) Row {
	result := Row{}
	for _, mid := range ids {
		result[mid] = Row{"completed24h": 0, "succeeded24h": 0, "failed24h": 0, "successRate24h": nil, "minSeconds24h": nil, "maxSeconds24h": nil, "recent": []string{}}
	}
	if len(ids) == 0 {
		return result
	}
	args := []any{}
	for _, mid := range ids {
		args = append(args, mid)
	}
	// Completion time defines the rolling window, including work submitted earlier.
	eligible := "status IN ('succeeded','failed') AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(provider_snapshot,'$.protocol')),'')!='demo'"
	duration := "CASE WHEN status='succeeded' AND finished_at>=created_at THEN TIMESTAMPDIFF(MICROSECOND,created_at,finished_at)/1000000 END"
	q := "SELECT model_id,COUNT(*) total,SUM(status='succeeded') succeeded,MIN(" + duration + ") min_seconds,MAX(" + duration + ") max_seconds FROM jobs WHERE model_id IN (" + strings.TrimRight(strings.Repeat("?,", len(ids)), ",") + ") AND finished_at>=? AND finished_at<=? AND " + eligible + " GROUP BY model_id"
	args = append(args, now.Add(-24*time.Hour), now)
	for _, row := range s.All(q, args...) {
		stats := object(result[str(row["model_id"])])
		total, succeeded := integer(row["total"]), integer(row["succeeded"])
		stats["completed24h"], stats["succeeded24h"], stats["failed24h"] = total, succeeded, total-succeeded
		stats["successRate24h"] = math.Round(float64(succeeded)/float64(total)*1000) / 10
		stats["minSeconds24h"], stats["maxSeconds24h"] = row["min_seconds"], row["max_seconds"]
	}
	// Each indexed branch reads at most ten matching outcomes. This avoids ranking
	// the entire job history when only a short strip is needed for every model.
	parts, recentArgs := []string{}, []any{}
	for _, mid := range ids {
		parts = append(parts, "(SELECT model_id,status,finished_at,seq FROM jobs WHERE model_id=? AND finished_at<=? AND "+eligible+" ORDER BY finished_at DESC,seq DESC LIMIT 10)")
		recentArgs = append(recentArgs, mid, now)
	}
	for _, row := range s.All("SELECT model_id,status FROM ("+strings.Join(parts, " UNION ALL ")+") recent ORDER BY model_id,finished_at,seq", recentArgs...) {
		stats := object(result[str(row["model_id"])])
		stats["recent"] = append(stats["recent"].([]string), str(row["status"]))
	}
	return result
}

func (a *App) modelStatus(r *Request) any {
	query := "SELECT m.id FROM models m JOIN providers p ON p.id=m.provider_id WHERE m.enabled=1 AND p.enabled=1"
	if !a.Config.Demo {
		query += " AND p.protocol!='demo'"
	}
	ids := []string{}
	for _, row := range a.Store.All(query + " ORDER BY m.id") {
		ids = append(ids, str(row["id"]))
	}
	// Include currently visible IDs in the cache identity so disabled models never
	// survive in a cached response after an admin changes model visibility.
	cacheKey := a.Config.RedisPrefix + "model-status:" + hash(jsonText(ids))
	if cached, err := a.Redis.Get(r.R.Context(), cacheKey).Bytes(); err == nil {
		var result Row
		if json.Unmarshal(cached, &result) == nil && result["models"] != nil {
			return result
		}
	}
	now := time.Now().UTC()
	result := Row{"models": a.Store.ModelStatus(ids, now), "updatedAt": now.Format(time.RFC3339), "windowHours": 24}
	_ = a.Redis.Set(r.R.Context(), cacheKey, jsonText(result), 15*time.Second).Err()
	return result
}
