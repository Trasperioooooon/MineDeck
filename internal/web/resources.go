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
		// 保留「缺少项目 ID」的错误顺序，但不能使用解析失败时填入一半的字段。
		if err := readJSON(r, &body); err != nil {
			if bodyTooLarge(err) {
				return nil, bodyErr(err)
			}
			body.ProjectID = ""
		}
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "projectId": body.ProjectID}), nil
	})
}
