package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func imageCountParameter(name string) bool {
	switch name {
	case "n", "image_count", "num_images", "numImages", "batch_size", "imageCount", "num_outputs", "count":
		return true
	}
	return false
}

func imageRequestParameters(kind string, parameters []any) []any {
	if kind != "image" {
		return parameters
	}
	result := make([]any, 0, len(parameters))
	for _, raw := range parameters {
		p := object(raw)
		if p["type"] != "assets" && imageCountParameter(str(p["name"])) {
			p = clone(p)
			p["type"], p["default"], p["min"], p["max"], p["integer"] = "number", float64(1), float64(1), float64(1), true
			delete(p, "options")
		}
		result = append(result, p)
	}
	return result
}

var midjourneyModelID = regexp.MustCompile(`(^|[/:._-])(midjourney|mj)($|[/:._-])`)
var imageOutputRange = regexp.MustCompile(`每次生成\s*\d+\s*[–～~-]\s*\d+\s*张`)

func imageOutputLimit(upstream string) int {
	if midjourneyModelID.MatchString(strings.ToLower(upstream)) {
		return 4
	}
	return 1
}

func imageRequestDescription(kind, upstream, description string) string {
	if kind != "image" {
		return description
	}
	description = strings.ReplaceAll(description, "按张计费", "按次计费")
	if imageOutputLimit(upstream) == 1 {
		description = imageOutputRange.ReplaceAllString(description, "每次生成 1 张")
	}
	return description
}

// Preserve amounts, specification conditions, enabled flags and reference limits.
// Only legacy image units and batch-output parameters are migrated.
func (a *App) migrateImageRequests() error {
	return try(func() {
		a.Tx(func(s Store) {
			prices := s.All("SELECT p.* FROM prices p JOIN models m ON m.id=p.model_id WHERE m.type='image' AND p.unit='image' FOR UPDATE")
			models, catalog := []Row{}, []Row{}
			for _, spec := range []struct {
				table  string
				target *[]Row
			}{{"models", &models}, {"provider_models", &catalog}} {
				for _, row := range s.All("SELECT id,upstream_model,parameters,description FROM " + spec.table + " WHERE type='image' FOR UPDATE") {
					params := jsonText(imageRequestParameters("image", array(row["parameters"])))
					desc := imageRequestDescription("image", str(row["upstream_model"]), str(row["description"]))
					if params != jsonText(array(row["parameters"])) || desc != row["description"] {
						*spec.target = append(*spec.target, row)
					}
				}
			}
			if len(prices)+len(models)+len(catalog) == 0 {
				return
			}
			if integer(s.One("SELECT count(*) n FROM jobs WHERE status IN ('queued','running')")["n"]) > 0 {
				fail(409, "图片计费升级需等待进行中的任务结束")
			}
			directory := filepath.Join(a.Config.DataDir, "backups")
			must(os.MkdirAll(directory, 0700))
			backup := filepath.Join(directory, "image-request-billing-"+id()+".encrypted.json")
			must(os.WriteFile(backup, []byte(a.encrypt(jsonText(Row{"prices": prices, "models": models, "providerModels": catalog}))), 0600))
			for _, price := range prices {
				s.Exec("UPDATE prices SET unit='request' WHERE id=?", price["id"])
			}
			for _, spec := range []struct {
				table string
				rows  []Row
			}{{"models", models}, {"provider_models", catalog}} {
				for _, row := range spec.rows {
					s.Exec("UPDATE "+spec.table+" SET parameters=?,description=? WHERE id=?", jsonText(imageRequestParameters("image", array(row["parameters"]))), imageRequestDescription("image", str(row["upstream_model"]), str(row["description"])), row["id"])
				}
			}
			if admin := s.One("SELECT id FROM users WHERE role='admin' ORDER BY created_at LIMIT 1"); admin != nil {
				s.Audit(str(admin["id"]), "maintenance.image_request_billing", Row{"prices": len(prices), "models": len(models), "providerModels": len(catalog), "backup": backup})
			}
		})
	})
}
