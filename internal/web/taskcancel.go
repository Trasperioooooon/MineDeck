package web

import "net/http"

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "task.cancel", args(map[string]any{"id": r.PathValue("id")}))
}
