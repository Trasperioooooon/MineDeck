package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/ops/catalog"
)

// 自选模组 + 购物车。购物车按实例分开存，inst 不填是向导用的草稿车（实例还没建好），所以车本身放在 /api/modcart 下，
// 装进某台服务器、核对它已装模组的依赖才挂在实例下面。处理逻辑在 ops/catalog/modcart.go。
func (s *Server) modcartRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/modcart/search", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.search", func() (ops.Args, error) {
			m := queryMap(r, "q", "inst", "core", "mc", "category")
			// offset 在查询串里是字符串，操作的参数说明要的是整数：这里转一下，转不了的按 400 报
			if v := r.URL.Query().Get("offset"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return nil, ops.Errorf(400, "参数 offset 应为整数")
				}
				m["offset"] = n
			}
			return ops.ArgsOf(m), nil
		})
	})
	mux.HandleFunc("GET /api/modcart", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.list", queryArgs(r, "inst", "core", "mc"))
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
	mux.HandleFunc("POST /api/modcart/local", s.handleModcartLocal)
	mux.HandleFunc("POST /api/modcart/local/remove", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "modcart.local.remove", bodyArgs(r))
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

// 本地文件上传的上限（包变量是为了测试能调小）和超过时给人看的话。
var modcartLocalLimit int64 = modcart.MaxLocalSize

const modcartLocalTooLarge = "这个文件超过了 256 MB 的上限，放不进清单。可以等服务器建好以后，自己把它拷进服务器的文件夹。"

// handleModcartLocal 接收一个从电脑上选的 jar（multipart：file，另带 inst / core / mc），存成临时文件后交给操作
// modcart.local.add。和整合包、交接包上传一样，网页层只管把文件存下来：文件名、扩展名、是不是 zip、大小、
// 放不放得进清单都由操作层查（它不信这里给的任何东西，只认上传目录里的文件）。
// 落盘的名字自己起：上传名是对方给的，可以超长、可以带冒号，不拿它拼路径，原样交给操作层去查。
func (s *Server) handleModcartLocal(w http.ResponseWriter, r *http.Request) {
	dest := ""
	// 操作成功时临时文件已经被挪进清单的暂存目录；没成功的（参数不合规、校验没过）在这里删掉，不留在缓存里
	defer func() {
		if dest != "" {
			_ = os.Remove(dest)
		}
	}()
	s.do(w, r, "modcart.local.add", func() (ops.Args, error) {
		if err := parseUpload(w, r, modcartLocalLimit, modcartLocalTooLarge); err != nil {
			return nil, err
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll() // 超过内存上限的部分落在系统临时目录里
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("没有收到文件")
		}
		defer file.Close()
		dir := catalog.LocalUploadDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, ops.Errorf(500, "保存上传文件失败")
		}
		sweepUploads(dir) // 兜底：程序中途退出留下的临时文件
		// 名字交给 CreateTemp 起（随机、独占创建）：几个上传同时到的时候不会撞名
		out, err := os.CreateTemp(dir, "*.upload")
		if err != nil {
			return nil, ops.Errorf(500, "保存上传文件失败")
		}
		path := out.Name()
		dest = path
		_, cerr := io.Copy(out, file)
		if werr := out.Close(); cerr == nil {
			cerr = werr
		}
		if cerr != nil {
			return nil, ops.Errorf(500, "保存上传文件失败")
		}
		m := map[string]any{"file": path, "fileName": hdr.Filename}
		for _, k := range []string{"inst", "core", "mc"} {
			if v := r.FormValue(k); v != "" {
				m[k] = v
			}
		}
		return ops.ArgsOf(m), nil
	})
}

// queryMap 查询串里的这几项（空的不带：可选参数不填和填空串是一回事）。
func queryMap(r *http.Request, keys ...string) map[string]any {
	m := map[string]any{}
	for _, k := range keys {
		if v := r.URL.Query().Get(k); v != "" {
			m[k] = v
		}
	}
	return m
}

// queryArgs 同 queryMap，直接当参数用。
func queryArgs(r *http.Request, keys ...string) func() (ops.Args, error) {
	return args(queryMap(r, keys...))
}
