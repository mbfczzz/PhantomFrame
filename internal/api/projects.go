package api

import "net/http"

func validateProject(p Row) Row {
	nodes, ok := p["nodes"].([]any)
	if !ok || len(nodes) > 500 {
		fail(400, "画布节点格式不正确或超过 500 个")
	}
	edges, ok := p["edges"].([]any)
	if !ok || len(edges) > 1000 {
		fail(400, "画布连线格式不正确或超过 1000 个")
	}
	for _, n := range nodes {
		v, ok := n.(map[string]any)
		if !ok {
			fail(400, "节点格式不正确")
		}
		textField(v, "id", 1, 100)
		pos := mapField(v, "position")
		numberField(pos, "x", -1e10, 1e10)
		numberField(pos, "y", -1e10, 1e10)
		mapField(v, "data")
		if _, ok := v["type"]; ok {
			textField(v, "type", 0, 50)
		}
	}
	for _, e := range edges {
		v := object(e)
		textField(v, "id", 1, 200)
		textField(v, "source", 1, 100)
		textField(v, "target", 1, 100)
	}
	if _, ok := p["viewport"]; ok {
		v := mapField(p, "viewport")
		numberField(v, "x", -1e10, 1e10)
		numberField(v, "y", -1e10, 1e10)
		numberField(v, "zoom", 0.00001, 1e5)
	}
	return p
}
func projectView(p Row, full bool) Row {
	data := object(p["data"])
	v := Row{"id": p["id"], "title": p["title"], "version": p["version"], "nodeCount": len(array(data["nodes"])), "edgeCount": len(array(data["edges"])), "createdAt": p["created_at"], "updatedAt": p["updated_at"]}
	if full {
		v["data"] = data
	}
	return v
}
func (a *App) projectRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/projects", 1, func(r *Request) any {
		v := []Row{}
		for _, p := range a.Store.All("SELECT * FROM projects WHERE user_id=? ORDER BY updated_at DESC", r.UID()) {
			v = append(v, projectView(p, false))
		}
		return v
	})
	a.route(m, "POST /api/projects", 1, func(r *Request) any {
		p := r.Body()
		title := trimField(p, "title", 1, 100, "未命名画布")
		data := Row{"nodes": []any{}, "edges": []any{}}
		if _, ok := p["data"]; ok {
			data = validateProject(mapField(p, "data"))
		}
		pid := id()
		a.Store.Exec("INSERT INTO projects(id,user_id,title,data) VALUES(?,?,?,?)", pid, r.UID(), title, jsonText(data))
		r.Status = 201
		return projectView(a.Store.One("SELECT * FROM projects WHERE id=?", pid), true)
	})
	a.route(m, "GET /api/projects/{id}", 1, func(r *Request) any {
		p := a.Store.One("SELECT * FROM projects WHERE id=? AND user_id=?", r.ID(), r.UID())
		if p == nil {
			fail(404, "画布不存在")
		}
		return projectView(p, true)
	})
	a.route(m, "PATCH /api/projects/{id}", 1, func(r *Request) any {
		input := r.Body()
		version := numberField(input, "version", 1, 1e12)
		var result Row
		a.Tx(func(s Store) {
			p := s.One("SELECT * FROM projects WHERE id=? AND user_id=? FOR UPDATE", r.ID(), r.UID())
			if p == nil {
				fail(404, "画布不存在")
			}
			title := trimField(input, "title", 1, 100, str(p["title"]))
			data := str(p["data"])
			if _, ok := input["data"]; ok {
				data = jsonText(validateProject(mapField(input, "data")))
			}
			if s.Exec("UPDATE projects SET title=?,data=?,version=version+1,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND version=?", title, data, r.ID(), version) == 0 {
				fail(409, "画布已在其他窗口更新，请刷新后重试")
			}
			result = projectView(s.One("SELECT * FROM projects WHERE id=?", r.ID()), true)
		})
		return result
	})
	a.route(m, "DELETE /api/projects/{id}", 1, func(r *Request) any {
		a.Store.Exec("DELETE FROM projects WHERE id=? AND user_id=?", r.ID(), r.UID())
		return Row{"ok": true}
	})
}
