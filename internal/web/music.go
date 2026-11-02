package web

import "net/http"

// 音乐共享（设计文档 §8，第五阶段）。实例的「音乐共享」页：检查清单和广播状态、开始 / 停止广播、设置、
// 装桥接模组、改 voice_host；这台电脑的音源列表和「确认整机录音」不属于某个实例，放在 /api/music/ 下。
// 开始广播、列音源、确认整机录音仅本机（录的是这台电脑的声音）；局域网登录的设备可以看状态、停止广播。
func (s *Server) musicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/instances/{name}/music", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.status", instArg(r))
	})
	mux.HandleFunc("POST /api/instances/{name}/music/start", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.start", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/music/stop", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.stop", instArg(r))
	})
	mux.HandleFunc("PUT /api/instances/{name}/music/settings", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.settings.set", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/music/install", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.bridge.install", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("PUT /api/instances/{name}/music/voicehost", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.voicehost.set", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("GET /api/music/sources", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.sources", nil)
	})
	mux.HandleFunc("POST /api/music/confirm-system", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "music.system.confirm", nil)
	})
}
