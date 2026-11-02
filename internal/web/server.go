// Package web 提供本地 HTTP API 与内嵌 GUI 静态资源。
// 安全措施：仅绑定 127.0.0.1、Host 头校验、API 需随机 Token（防跨站请求）。
// 接口只做 HTTP 适配：解析请求 → ops.Do（操作层判断权限、校验参数、写操作记录）→ 写回响应；
// 具体的处理逻辑在 internal/ops/catalog。SSE 控制台流、整合包上传、壁纸和登录不走操作层（或只部分走）。
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/assistant"
	"minedeck/internal/inst"
	"minedeck/internal/mcp"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

//go:embed ui
var uiFS embed.FS

type Server struct {
	mgr  *inst.Manager
	tun  *tunnel.Manager
	ops  *ops.Registry
	asst *assistant.Service
	mcp  *mcp.Service // MCP 接口；nil 时没有 /mcp 和 /api/mcp*
}

// Option New 的可选项。
type Option func(*Server)

// WithMCP 接上 MCP 接口（设计文档 §15）：/mcp 和 /api/mcp*，见 mcp.go。
func WithMCP(m *mcp.Service) Option { return func(s *Server) { s.mcp = m } }

// New 建好路由。reg 是操作层的注册表，asst 是应用内助手，都在 main 里建好：两者共用同一个注册表（提案存在里面）。
func New(mgr *inst.Manager, tun *tunnel.Manager, reg *ops.Registry, asst *assistant.Service, opts ...Option) http.Handler {
	s := &Server{mgr: mgr, tun: tun, ops: reg, asst: asst}
	for _, o := range opts {
		o(s)
	}
	mux := http.NewServeMux()

	sub, _ := fs.Sub(uiFS, "ui")
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("GET /", noCache(fileServer))
	// 内嵌字体约 1.9 MB：带 ETag 可缓存，局域网手机再次打开只需一次 304
	mux.Handle("GET /fonts/", revalidate(sub, fileServer))
	mux.HandleFunc("GET /bg/{name}", s.handleBGImage) // 用户壁纸（程序旁 bg/ 目录），免 token 供 CSS url() 引用

	mux.HandleFunc("GET /api/app", s.handleApp)
	mux.HandleFunc("GET /api/bg", s.handleBGList)
	mux.HandleFunc("POST /api/pickdir", s.handlePickDir)
	mux.HandleFunc("POST /api/openpath", s.handleOpenPath)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)
	mux.HandleFunc("PUT /api/config", s.handleSetConfig)
	mux.HandleFunc("POST /api/update/check", s.handleUpdateCheck)
	mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)
	mux.HandleFunc("GET /api/instances/{name}/resources/search", s.handleResourceSearch)
	mux.HandleFunc("POST /api/instances/{name}/resources/install", s.handleResourceInstall)
	mux.HandleFunc("GET /api/cores", s.handleCores)
	mux.HandleFunc("GET /api/mcversions", s.handleMCVersions)
	mux.HandleFunc("GET /api/builds", s.handleBuilds)
	mux.HandleFunc("GET /api/javas", s.handleJavas)
	mux.HandleFunc("POST /api/javas/scan", s.handleJavaScan)
	mux.HandleFunc("POST /api/javas/add", s.handleJavaAdd)

	mux.HandleFunc("GET /api/instances", s.handleListInstances)
	mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	mux.HandleFunc("POST /api/import/modpack", s.handleImportModpack)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTask)

	mux.HandleFunc("GET /api/instances/{name}/backups", s.handleListBackups)
	mux.HandleFunc("POST /api/instances/{name}/backups", s.handleCreateBackup)
	mux.HandleFunc("POST /api/instances/{name}/backups/restore", s.handleRestoreBackup)
	mux.HandleFunc("DELETE /api/instances/{name}/backups", s.handleDeleteBackup)
	mux.HandleFunc("GET /api/instances/{name}/players", s.handleGetPlayers)
	mux.HandleFunc("POST /api/instances/{name}/players", s.handlePlayerAction)
	mux.HandleFunc("GET /api/instances/{name}/policies", s.handleGetPolicies)
	mux.HandleFunc("PUT /api/instances/{name}/policies", s.handleSetPolicies)

	mux.HandleFunc("GET /api/tunnels", s.handleListTunnels)
	mux.HandleFunc("POST /api/tunnels", s.handleAddTunnel)
	mux.HandleFunc("PUT /api/tunnels/{id}", s.handleUpdateTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.handleDeleteTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/start", s.handleTunnelStart)
	mux.HandleFunc("POST /api/tunnels/{id}/stop", s.handleTunnelStop)
	mux.HandleFunc("GET /api/tunnels/{id}/console", s.handleTunnelConsole)

	s.musicRoutes(mux)     // 音乐共享（第五阶段），见 music.go
	s.gameChatRoutes(mux)  // 游戏内 AI（第四阶段），见 gamechat.go
	s.companionRoutes(mux) // AI 伙伴（第七阶段），见 companion.go

	mux.HandleFunc("POST /api/instances/{name}/start", s.handleInstStart)
	mux.HandleFunc("POST /api/instances/{name}/stop", s.instOp("instance.stop"))
	mux.HandleFunc("POST /api/instances/{name}/kill", s.instOp("instance.kill"))
	mux.HandleFunc("POST /api/instances/{name}/opendir", s.handleOpenDir)
	mux.HandleFunc("POST /api/instances/{name}/command", s.handleCommand)
	mux.HandleFunc("DELETE /api/instances/{name}", s.handleDelete)
	mux.HandleFunc("GET /api/instances/{name}/console", s.handleConsole)
	mux.HandleFunc("GET /api/instances/{name}/properties", s.handleGetProps)
	mux.HandleFunc("PUT /api/instances/{name}/properties", s.handleSetProps)
	mux.HandleFunc("PUT /api/instances/{name}/settings", s.handleSetSettings)

	mux.HandleFunc("GET /api/ops/log", s.handleOpLog)

	mux.HandleFunc("GET /api/ai/settings", s.handleAISettings)
	mux.HandleFunc("PUT /api/ai/provider", s.handleAIProvider)
	mux.HandleFunc("POST /api/ai/models", s.handleAIModels)
	mux.HandleFunc("POST /api/ai/test", s.handleAITest)
	mux.HandleFunc("PUT /api/ai/quota", s.handleAIQuota)
	mux.HandleFunc("PUT /api/ai/prices", s.handleAIPrices)
	mux.HandleFunc("POST /api/ai/prices/refresh", s.handleAIPricesRefresh)

	// 第三阶段：应用内 AI 助手、提案的确认接口、排错和修复用的实例接口（见 assistant.go）
	s.assistantRoutes(mux)
	s.troubleshootRoutes(mux)
	s.mcpRoutes(mux) // MCP 接口，见 mcp.go

	return secure(mux)
}

// secure 校验 Host、API Token 或局域网会话 Cookie。
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lan := app.GetConfig().ListenLAN && app.GetConfig().AccessPasswordHash != ""
		if !lan {
			host := r.Host
			if h, _, err := splitHostPort(host); err == nil {
				host = h
			}
			if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
				http.Error(w, "forbidden host", http.StatusForbidden)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/login" {
			tok := r.Header.Get("X-Token")
			if tok == "" {
				tok = r.URL.Query().Get("token")
			}
			if tok != Token && !validSession(r) {
				writeErr(w, http.StatusUnauthorized, fmt.Errorf("需要登录"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// splitHostPort 拆分 Host 头，正确处理裸 IPv6（如 ::1 / [::1]，有无端口皆可），
// 避免自写 LastIndex(":") 把 IPv6 环回地址切错导致 secure() 误判 403。
func splitHostPort(hp string) (string, string, error) {
	if h, p, err := net.SplitHostPort(hp); err == nil {
		return h, p, nil
	}
	return strings.Trim(hp, "[]"), "", nil // 无端口：裸主机名或裸 IPv6，去方括号
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// Go 内置的扩展名表没有 .woff2（Windows 上还会去查注册表，结果因机器而异），显式登记。
func init() { _ = mime.AddExtensionType(".woff2", "font/woff2") }

// revalidate 给体积较大的内嵌静态文件（字体）加强 ETag（内容的 SHA-256）与 Cache-Control: no-cache：
// 浏览器可以缓存，但每次使用前先带 If-None-Match 询问，内容没变就回 304，不重新下载；
// 换了版本 ETag 随内容变化，不会用到旧字体。304 由 http.FileServer 按已设置的 ETag 头自动处理。
func revalidate(fsys fs.FS, next http.Handler) http.Handler {
	etags := map[string]string{}
	_ = fs.WalkDir(fsys, "fonts", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := fs.ReadFile(fsys, p); err == nil {
			sum := sha256.Sum256(b)
			etags["/"+p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		}
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, ok := etags[r.URL.Path]; ok {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": v})
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ---------- 接到操作层 ----------

// callerOf 网页请求的调用方：本机来的算本机（浏览器和 WebView2 里的界面发请求时带 Sec-Fetch-Site: same-origin，
// 记作 ui；其余本机程序记作 api）；别的设备一律算局域网，记下它的地址。
func callerOf(r *http.Request) ops.Caller {
	if isLoopbackReq(r) {
		src := ops.SrcAPI
		if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
			src = ops.SrcUI
		}
		return ops.Caller{Source: src, Actor: "本机", Local: true}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return ops.Caller{Source: ops.SrcLAN, Actor: host}
}

// do 解析请求 → ops.Do → 写回响应。先判权限，再调 parse 取参数（取不到按它带的状态码报，默认 400），最后执行。
// 成功时响应仍是 {"ok":true,"data":…}；失败时状态码取自操作层的错误。
func (s *Server) do(w http.ResponseWriter, r *http.Request, name string, parse func() (ops.Args, error)) {
	c := callerOf(r)
	if err := s.ops.Check(c, name); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	var a ops.Args
	if parse != nil {
		var err error
		if a, err = parse(); err != nil {
			writeErr(w, ops.CodeOf(err), err)
			return
		}
	}
	res, err := s.ops.Do(r.Context(), c, name, a)
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	writeJSON(w, res)
}

// args 固定的参数。
func args(m map[string]any) func() (ops.Args, error) {
	return func() (ops.Args, error) { return ops.ArgsOf(m), nil }
}

// instArg 只有实例名（路径里的 {name}）的参数。
func instArg(r *http.Request) func() (ops.Args, error) {
	return args(map[string]any{"inst": r.PathValue("name")})
}

// bodyArgs 请求体（JSON 对象）加上路径参数 kv（名, 值, 名, 值…）。请求体解析失败按 400 报，和原来的 readJSON 一样。
func bodyArgs(r *http.Request, kv ...string) func() (ops.Args, error) {
	return func() (ops.Args, error) {
		defer r.Body.Close()
		var m map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber() // 数字原样转交，不经 float64
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		if m == nil {
			m = map[string]any{}
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return ops.ArgsOf(m), nil
	}
}
