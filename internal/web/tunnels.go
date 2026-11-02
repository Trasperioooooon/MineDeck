package web

import "net/http"

func (s *Server) handleListTunnels(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.list", nil)
}

func (s *Server) handleAddTunnel(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.add", bodyArgs(r))
}

func (s *Server) handleUpdateTunnel(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.update", bodyArgs(r, "id", r.PathValue("id")))
}

func (s *Server) handleDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.delete", args(map[string]any{"id": r.PathValue("id")}))
}

func (s *Server) handleTunnelStart(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.start", args(map[string]any{"id": r.PathValue("id")}))
}

func (s *Server) handleTunnelStop(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "tunnel.stop", args(map[string]any{"id": r.PathValue("id")}))
}
