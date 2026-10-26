package api

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func detectMime(b []byte) (string, string) {
	if bytes.HasPrefix(b, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return "image/png", "png"
	}
	if bytes.HasPrefix(b, []byte{255, 216, 255}) {
		return "image/jpeg", "jpg"
	}
	if bytes.HasPrefix(b, []byte("GIF8")) {
		return "image/gif", "gif"
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" {
		if string(b[8:12]) == "WEBP" {
			return "image/webp", "webp"
		}
		if string(b[8:12]) == "WAVE" {
			return "audio/wav", "wav"
		}
	}
	if len(b) >= 8 && string(b[4:8]) == "ftyp" {
		return "video/mp4", "mp4"
	}
	if bytes.HasPrefix(b, []byte{26, 69, 223, 163}) {
		return "video/webm", "webm"
	}
	if bytes.HasPrefix(b, []byte("ID3")) || (len(b) >= 2 && b[0] == 255 && (b[1] == 251 || b[1] == 243 || b[1] == 242)) {
		return "audio/mpeg", "mp3"
	}
	return "", ""
}
func assetView(v Row) Row {
	return Row{"id": v["id"], "name": v["name"], "type": v["type"], "mime": v["mime"], "size": v["size"], "jobId": v["job_id"], "url": "/api/assets/" + str(v["id"]) + "/file", "createdAt": v["created_at"]}
}
func (a *App) storedAssetView(v Row) Row {
	view := assetView(v)
	for key, value := range a.assetLinks(a.Store, v) {
		view[key] = value
	}
	return view
}
func (a *App) filePath(name string) string {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		fail(400, "文件路径不正确")
	}
	return filepath.Join(a.Config.FilesDir, name)
}
func (a *App) saveAsset(uid, jobID, name, mediaType, ext string, data []byte) (Row, error) {
	assetID := id()
	file := assetID + "." + ext
	if name == "" {
		name = file
	}
	path := a.filePath(file)
	if e := os.WriteFile(path, data, 0600); e != nil {
		return nil, e
	}
	var job any
	if jobID != "" {
		job = jobID
	}
	err := try(func() {
		a.Store.Exec("INSERT INTO assets(id,user_id,name,type,mime,file,size,job_id) VALUES(?,?,?,?,?,?,?,?)", assetID, uid, name, strings.Split(mediaType, "/")[0], mediaType, file, len(data), job)
	})
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return Row{"assetId": assetID, "url": "/api/assets/" + assetID + "/file", "type": strings.Split(mediaType, "/")[0], "name": name}, nil
}
func (a *App) assetRoutes(m *http.ServeMux) {
	a.route(m, "POST /api/assets", 1, func(r *Request) any {
		r.R.Body = http.MaxBytesReader(r.W, r.R.Body, 31*1024*1024)
		if e := r.R.ParseMultipartForm(1 << 20); e != nil {
			fail(400, "上传失败，文件不能超过 30MB")
		}
		defer r.R.MultipartForm.RemoveAll()
		f, h, e := r.R.FormFile("file")
		if e != nil {
			fail(400, "请选择文件")
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 30*1024*1024+1))
		must(e)
		if len(b) > 30*1024*1024 {
			fail(400, "文件不能超过 30MB")
		}
		mt, ext := detectMime(b)
		if mt == "" {
			fail(400, "支持 PNG、JPEG、GIF、WebP、MP4、WebM、MP3 和 WAV 文件")
		}
		name := []rune(filepath.Base(h.Filename))
		if len(name) > 240 {
			name = name[:240]
		}
		out, e := a.saveAsset(r.UID(), "", string(name), mt, ext, b)
		must(e)
		r.Status = 201
		return a.storedAssetView(a.Store.One("SELECT * FROM assets WHERE id=?", out["assetId"]))
	})
	a.route(m, "GET /api/assets", 1, func(r *Request) any {
		page, limit := r.Page()
		where, args := []string{"user_id=?"}, []any{r.UID()}
		q := r.R.URL.Query()
		if v := q.Get("type"); v != "" && v != "all" {
			where = append(where, "type=?")
			args = append(args, v)
		}
		if v := q.Get("search"); v != "" {
			where = append(where, "name LIKE ?")
			args = append(args, "%"+v+"%")
		}
		clause := strings.Join(where, " AND ")
		count := a.Store.One("SELECT count(*) n FROM assets WHERE "+clause, args...)["n"]
		items := []Row{}
		for _, v := range a.Store.All("SELECT * FROM assets WHERE "+clause+" ORDER BY created_at DESC,seq DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...) {
			items = append(items, a.storedAssetView(v))
		}
		return Row{"items": items, "total": count, "page": page, "limit": limit}
	})
	a.route(m, "GET /api/assets/{id}/file", 1, func(r *Request) any {
		v := a.Store.One("SELECT * FROM assets WHERE id=? AND user_id=?", r.ID(), r.UID())
		if v == nil {
			fail(404, "素材不存在")
		}
		r.W.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		r.W.Header().Set("Content-Type", str(v["mime"]))
		if r.R.URL.Query().Get("download") != "" {
			r.W.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": str(v["name"])}))
		}
		http.ServeFile(r.W, r.R, a.filePath(str(v["file"])))
		return nil
	})
	a.route(m, "DELETE /api/assets/{id}", 1, func(r *Request) any {
		var v Row
		a.Tx(func(s Store) {
			s.One("SELECT id FROM users WHERE id=? FOR UPDATE", r.UID())
			v = s.One("SELECT * FROM assets WHERE id=? AND user_id=?", r.ID(), r.UID())
			if v == nil {
				fail(404, "素材不存在")
			}
			for _, j := range s.All("SELECT params FROM jobs WHERE user_id=? AND status IN ('queued','running')", r.UID()) {
				if strings.Contains(str(j["params"]), r.ID()) {
					fail(400, "该素材正在生成任务中使用")
				}
			}
			if err := a.removeOSSAsset(r.R.Context(), v); err != nil {
				fail(502, err.Error())
			}
			if err := os.Remove(a.filePath(str(v["file"]))); err != nil && !os.IsNotExist(err) {
				fail(502, "素材文件删除未完成，请稍后重试；删除记录已保留")
			}
			s.Exec("DELETE FROM assets WHERE id=?", r.ID())
		})
		return Row{"ok": true}
	})
}
