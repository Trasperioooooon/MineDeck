package web

import (
	"net/http"

	"minedeck/internal/ops"
)

// 自选模组 + 购物车。购物车按实例分开存，inst 不填是向导用的草稿车（实例还没建好），所以车本身放在 /api/modcart 下，
// 装进某台服务器、核对它已装模组的依赖才挂在实例下面。处理逻辑在 ops/catalog/modcart.go。
func (s *Server) modcartRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/modcart/search", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.search", queryArgs(r, "q", "inst", "core", "mc"))
	})
	mux.HandleFunc("GET /api/modcart", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.list", queryArgs(r, "inst"))
	})
	mux.HandleFunc("POST /api/modcart", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.add", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/modcart/remove", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.remove", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/modcart/clear", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.clear", bodyArgs(r))
	})
	mux.HandleFunc("GET /api/modcart/deps", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.deps", queryArgs(r, "inst", "core", "mc"))
	})
	mux.HandleFunc("POST /api/instances/{name}/modcart/install", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.install", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("GET /api/instances/{name}/mods/deps", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.mods.deps", instArg(r))
	})
}

// queryArgs 查询串里的这几项（空的不带：可选参数不填和填空串是一回事）。
func queryArgs(r *http.Request, keys ...string) func() (ops.Args, error) {
	m := map[string]any{}
	for _, k := range keys {
		if v := r.URL.Query().Get(k); v != "" {
			m[k] = v
		}
	}
	return args(m)
}
