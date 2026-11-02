package web

import (
	"net/http"
	"strconv"
)

// handleOpLog GET /api/ops/log?inst=&src=&op=&limit= 查操作记录（新的在前），局域网会话也能看。
func (s *Server) handleOpLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	s.do(w, r, "ops.log", args(map[string]any{"inst": q.Get("inst"), "src": q.Get("src"), "op": q.Get("op"), "limit": limit}))
}
