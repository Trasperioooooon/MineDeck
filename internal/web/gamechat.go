package web

import (
	"net/http"
	"strconv"

	"minedeck/internal/ops"
)

// 游戏内聊天 AI（设计文档 §6、§9 第 4 条，第四阶段）：实例的「游戏内 AI」标签。全部走操作层（gameai.*，「仅人工」）：
// 改设置、一键开启白名单仅本机；局域网登录的设备能看设置和最近的问答，能做识别测试。
func (s *Server) gameChatRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/instances/{name}/gameai", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "gameai.get", instArg(r))
	})
	mux.HandleFunc("PUT /api/instances/{name}/gameai", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "gameai.set", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/gameai/test", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "gameai.test", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("GET /api/instances/{name}/gameai/log", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "gameai.log", func() (ops.Args, error) {
			m := map[string]any{"inst": r.PathValue("name")}
			if v := r.URL.Query().Get("limit"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return nil, ops.Errorf(400, "limit 要是整数")
				}
				m["limit"] = n
			}
			return ops.ArgsOf(m), nil
		})
	})
	mux.HandleFunc("POST /api/instances/{name}/gameai/whitelist", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "gameai.whitelist", bodyArgs(r, "inst", r.PathValue("name")))
	})
}
