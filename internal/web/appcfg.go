package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"minedeck/internal/app"
)

// OnShutdown 由 main 注入：前端「退出程序」按钮触发（浏览器回退模式下唯一的退出途径）。
var OnShutdown func()

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "app.info", nil)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "app.shutdown", nil)
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "app.config.set", bodyArgs(r))
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "app.update.check", nil)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "app.update.apply", nil)
}

// ---------- 壁纸个性化（程序旁 bg/ 目录；只是界面装饰，不走操作层） ----------

var bgExt = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".gif": "image/gif", ".avif": "image/avif", ".bmp": "image/bmp",
}

func bgDir() string { return filepath.Join(app.Base, "bg") }

// handleBGList 列出 bg/ 目录内的图片文件名（前端随机取一张作背景）。
func (s *Server) handleBGList(w http.ResponseWriter, r *http.Request) {
	var imgs []string
	if ents, err := os.ReadDir(bgDir()); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			if _, ok := bgExt[strings.ToLower(filepath.Ext(e.Name()))]; ok {
				imgs = append(imgs, e.Name())
			}
		}
	}
	if imgs == nil {
		imgs = []string{}
	}
	writeJSON(w, map[string]any{"images": imgs})
}

// handleBGImage 提供 bg/ 目录内的单张图片（filepath.Base 去除路径成分防穿越）。
func (s *Server) handleBGImage(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	ct, ok := bgExt[strings.ToLower(filepath.Ext(name))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(bgDir(), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeContent(w, r, name, st.ModTime(), f)
}
