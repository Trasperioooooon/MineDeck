package web

import "net/http"

// 穿透平台账户（OpenFrp / SakuraFrp）：连接、概览、手动签到、购买跳转、在平台创建/导入隧道。
// 全部经 ops HumanOnly，不给 AI。
func (s *Server) tunnelAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tunnel-accounts", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.status", nil)
	})
	mux.HandleFunc("POST /api/tunnel-accounts/openfrp/login", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.openfrp.login", nil)
	})
	mux.HandleFunc("POST /api/tunnel-accounts/openfrp/login/{id}/poll", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.openfrp.poll", args(map[string]any{"id": r.PathValue("id")}))
	})
	mux.HandleFunc("POST /api/tunnel-accounts/natfrp/connect", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.natfrp.connect", bodyArgs(r))
	})
	mux.HandleFunc("GET /api/tunnel-accounts/{provider}/weblogin", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.weblogin", args(map[string]any{"provider": r.PathValue("provider")}))
	})
	mux.HandleFunc("DELETE /api/tunnel-accounts/{provider}", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.disconnect", args(map[string]any{"provider": r.PathValue("provider")}))
	})
	mux.HandleFunc("GET /api/tunnel-accounts/{provider}/overview", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.overview", args(map[string]any{"provider": r.PathValue("provider")}))
	})
	mux.HandleFunc("GET /api/tunnel-accounts/{provider}/captcha", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.captcha", args(map[string]any{"provider": r.PathValue("provider")}))
	})
	mux.HandleFunc("POST /api/tunnel-accounts/{provider}/sign", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.sign", bodyArgs(r, "provider", r.PathValue("provider")))
	})
	mux.HandleFunc("GET /api/tunnel-accounts/{provider}/purchase", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.purchase", args(map[string]any{"provider": r.PathValue("provider")}))
	})
	mux.HandleFunc("POST /api/tunnel-accounts/{provider}/tunnels", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.create", bodyArgs(r, "provider", r.PathValue("provider")))
	})
	mux.HandleFunc("POST /api/tunnel-accounts/{provider}/import", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "tunnel.account.import", bodyArgs(r, "provider", r.PathValue("provider")))
	})
}
