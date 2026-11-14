package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// handleImportModpack 接收整合包上传（multipart），存进缓存目录后交给操作 instance.import 解析并启动导入任务。
func (s *Server) handleImportModpack(w http.ResponseWriter, r *http.Request) {
	r, cleanup := beginUpload(w, r)
	defer cleanup()
	s.do(w, r, "instance.import", func() (ops.Args, error) {
		if err := parseUpload(w, r, modpackUploadLimit, modpackTooLarge); err != nil {
			return nil, err
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少整合包文件")
		}
		defer file.Close()
		dir := filepath.Join(app.CacheDir, "import")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, ops.Wrap(500, err)
		}
		out, err := os.CreateTemp(dir, "*.zip") // 上传名不参与路径，避免并发同名覆盖和 Windows 备用数据流。
		if err != nil {
			return nil, ops.Wrap(500, err)
		}
		dest := out.Name()
		cerr := copyUpload(r, out, file, hdr.Size, modpackTooLarge)
		if werr := out.Close(); cerr == nil {
			cerr = ops.Wrap(500, werr)
		}
		if cerr != nil {
			_ = os.Remove(dest)
			return nil, fmt.Errorf("保存上传文件失败: %w", cerr)
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
			"root":        r.FormValue("root"),        // 远程 / 局域网请求的 root 由操作层忽略
			"localSource": r.FormValue("localSource"), // 本地来源目录（老服务器）：同样由操作层只认本机请求
		}), nil
	})
}

// handleImportPending 逐项处置整合包导入任务的待决文件（任务面板用）。
func (s *Server) handleImportPending(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.import.pending", func() (ops.Args, error) {
		var body struct {
			Items []inst.PendingAction `json:"items"`
		}
		if err := readJSON(r, &body); err != nil {
			return nil, bodyErr(err)
		}
		taskID := r.PathValue("taskId")
		if taskID == "" {
			return nil, ops.Errorf(400, "缺少任务编号")
		}
		return ops.ArgsOf(map[string]any{"taskId": taskID, "items": body.Items}), nil
	})
}

// handleAdoptScan 识别一个已有的服务器文件夹（只读）。仅本机可用。
func (s *Server) handleAdoptScan(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.adopt.scan", bodyArgs(r))
}

// handleAdopt 接管已有的服务器，返回任务编号。仅本机可用。
func (s *Server) handleAdopt(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.adopt", bodyArgs(r))
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.list", nil)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.create", bodyArgs(r))
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "task.list", nil)
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
