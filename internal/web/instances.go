package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/ops"
)

// handleImportModpack 接收整合包上传（multipart），存进缓存目录后交给操作 instance.import 解析并启动导入任务。
func (s *Server) handleImportModpack(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.import", func() (ops.Args, error) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil, fmt.Errorf("上传解析失败: %w", err)
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少整合包文件")
		}
		defer file.Close()
		base := filepath.Base(hdr.Filename)
		if base == "" || base == "." {
			base = "modpack.zip"
		}
		dest := filepath.Join(app.CacheDir, "import", fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, ops.Wrap(500, err)
		}
		out, err := os.Create(dest)
		if err != nil {
			return nil, ops.Wrap(500, err)
		}
		_, cerr := io.Copy(out, file)
		if werr := out.Close(); cerr == nil {
			cerr = werr
		}
		if cerr != nil {
			return nil, ops.Errorf(500, "保存上传文件失败: %w", cerr)
		}
		atoi := func(s string, def int) int {
			if n, err := strconv.Atoi(s); err == nil {
				return n
			}
			return def
		}
		return ops.ArgsOf(map[string]any{
			"file":        dest,
			"name":        r.FormValue("name"),
			"xmxMb":       atoi(r.FormValue("xmxMb"), 4096),
			"port":        atoi(r.FormValue("port"), 25565),
			"eula":        r.FormValue("eula") == "true",
			"onlineMode":  r.FormValue("onlineMode") == "true",
			"allowFlight": r.FormValue("allowFlight") == "true",
			"motd":        r.FormValue("motd"),
			"root":        r.FormValue("root"), // 远程 / 局域网请求的 root 由操作层忽略
		}), nil
	})
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.list", nil)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.create", bodyArgs(r))
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "task.get", args(map[string]any{"id": r.PathValue("id")}))
}

// instOp 只带实例名的操作（停止、强杀）。
func (s *Server) instOp(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { s.do(w, r, name, instArg(r)) }
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.command", bodyArgs(r, "inst", r.PathValue("name")))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.delete", args(map[string]any{"inst": r.PathValue("name"), "files": r.URL.Query().Get("files") == "1"}))
}

// handleInstStart 启动实例，成功后拉起绑定的跟随隧道。
func (s *Server) handleInstStart(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.start", instArg(r))
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.settings.set", bodyArgs(r, "inst", r.PathValue("name")))
}
