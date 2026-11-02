package web

import "net/http"

func (s *Server) handleCores(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "mc.cores", nil)
}

func (s *Server) handleMCVersions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.do(w, r, "mc.versions", args(map[string]any{"core": q.Get("core"), "snapshots": q.Get("snapshots") == "1"}))
}

func (s *Server) handleBuilds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.do(w, r, "mc.builds", args(map[string]any{"core": q.Get("core"), "mc": q.Get("mc")}))
}
