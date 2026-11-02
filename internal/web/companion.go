package web

import "net/http"

// AI 伙伴（设计文档 §7，第七阶段）：实例的「伙伴」标签。全部走操作层：看状态是读操作；装模组、改设置、刷出、移除、
// 全部停下是写操作（人在界面上点了就做）。伙伴的动作（跟随、采集……）只有游戏内 AI 的工具能用，不在这里。
func (s *Server) companionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/instances/{name}/companion", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.status", instArg(r))
	})
	mux.HandleFunc("POST /api/instances/{name}/companion/install", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.install", instArg(r))
	})
	mux.HandleFunc("PUT /api/instances/{name}/companion/settings", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.settings.set", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/companion/spawn", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.spawn", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/companion/despawn", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.despawn", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/companion/stop", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "companion.stop", bodyArgs(r, "inst", r.PathValue("name")))
	})
}
