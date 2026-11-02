package web

import (
	"net/http"

	"minedeck/internal/ops"
)

func (s *Server) handleResourceSearch(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.resources.search", args(map[string]any{"inst": r.PathValue("name"), "q": r.URL.Query().Get("q")}))
}

func (s *Server) handleResourceInstall(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.resources.install", func() (ops.Args, error) {
		var body struct {
			ProjectID string `json:"projectId"`
		}
		_ = readJSON(r, &body) // 解析失败按「缺少项目 ID」报，且排在「实例不存在」之后（同原来的顺序）
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "projectId": body.ProjectID}), nil
	})
}
