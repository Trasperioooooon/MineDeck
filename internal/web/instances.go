package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
)

// handleImportModpack 接收整合包上传（multipart），存进缓存目录后交给操作 instance.import 解析并启动导入任务。
func (s *Server) handleImportModpack(w http.ResponseWriter, r *http.Request) {
	r, cleanup := beginUpload(w, r)
	defer cleanup()
	c := callerOf(r)
	if err := s.ops.Check(c, "instance.import"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	if err := parseUpload(w, r, modpackUploadLimit, modpackTooLarge); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, fmt.Errorf("缺少整合包文件"))
		return
	}
	defer file.Close()
	port := 25565
	if n, err := strconv.Atoi(r.FormValue("port")); err == nil && n >= 1 && n <= 65535 {
		port = n
	}
	if busy := s.mgr.CheckFormPort(port); busy != nil {
		writePortBusyErr(w, busy)
		return
	}
	dir := filepath.Join(app.CacheDir, "import")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, ops.Wrap(500, err))
		return
	}
	out, err := os.CreateTemp(dir, "*.zip") // 上传名不参与路径，避免并发同名覆盖和 Windows 备用数据流。
	if err != nil {
		writeErr(w, 500, ops.Wrap(500, err))
		return
	}
	dest := out.Name()
	cerr := copyUpload(r, out, file, hdr.Size, modpackTooLarge)
	if werr := out.Close(); cerr == nil {
		cerr = ops.Wrap(500, werr)
	}
	if cerr != nil {
		_ = os.Remove(dest)
		writeErr(w, ops.CodeOf(cerr), fmt.Errorf("保存上传文件失败: %w", cerr))
		return
	}
	a := importFormArgs(r, dest)
	a["fileName"] = hdr.Filename
	res, err := s.ops.Do(r.Context(), c, "instance.import", ops.ArgsOf(a))
	if err != nil {
		_ = os.Remove(dest)
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	writeJSON(w, res)
}

// importFormArgs 导入整合包表单里的各项 → 操作 instance.import 的参数。表单是一项一项抄过去的：
// 表单上加了开关，这里不抄就会被悄悄丢掉、按默认值导入（白名单开关就是这样：不抄的话打开了也写成关）。
func importFormArgs(r *http.Request, dest string) map[string]any {
	atoi := func(s string, def int) int {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return def
	}
	return map[string]any{
		"file":        dest,
		"name":        r.FormValue("name"),
		"xmxMb":       atoi(r.FormValue("xmxMb"), 4096),
		"port":        atoi(r.FormValue("port"), 25565),
		"eula":        r.FormValue("eula") == "true",
		"onlineMode":  r.FormValue("onlineMode") == "true",
		"allowFlight": r.FormValue("allowFlight") == "true",
		"whitelist":   r.FormValue("whitelist") == "true", // 只允许白名单里的玩家进入：整合包自带 server.properties 时也以它为准
		"motd":        r.FormValue("motd"),
		"root":        r.FormValue("root"),        // 远程 / 局域网请求的 root 由操作层忽略
		"localSource": r.FormValue("localSource"), // 本地来源目录（老服务器）：同样由操作层只认本机请求
	}
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

// handleGetInstPending 一台服务器上整合包导入时没装上的文件（按服务器，程序重开以后也在）。
func (s *Server) handleGetInstPending(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.import.pending.get", instArg(r))
}

// handleResolveInstPending 按服务器处置没装上的文件（详情页、任务页用）。仅本机可用。
func (s *Server) handleResolveInstPending(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.import.pending.resolve", func() (ops.Args, error) {
		var body struct {
			Items []inst.PendingAction `json:"items"`
		}
		if err := readJSON(r, &body); err != nil {
			return nil, bodyErr(err)
		}
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "items": body.Items}), nil
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
	s.do(w, r, "instance.list", nil, func(m map[string]any) {
		m["duplicates"] = s.mgr.SkippedDuplicates()
		m["broken"] = s.mgr.BrokenInstances()
		m["local"] = callerOf(r).Local
	})
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if err := s.ops.Check(c, "instance.create"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	defer r.Body.Close()
	var m map[string]any
	if err := decodeJSON(limitedBody(r, maxJSONBody), &m); err != nil {
		err = bodyErr(err)
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	if m == nil {
		m = map[string]any{}
	}
	if !c.Source.AI() {
		nameStr, _ := m["name"].(string)
		coreStr, _ := m["core"].(string)
		if strings.TrimSpace(nameStr) != "" && coreStr != "" && mcsrc.KindOf(mcsrc.Core(coreStr)) != mcsrc.KindProxy {
			port := 25565
			switch pv := m["port"].(type) {
			case json.Number:
				if n, err := pv.Int64(); err == nil && n >= 1 && n <= 65535 {
					port = int(n)
				}
			case float64:
				if int(pv) >= 1 && int(pv) <= 65535 {
					port = int(pv)
				}
			}
			if busy := s.mgr.CheckFormPort(port); busy != nil {
				writePortBusyErr(w, busy)
				return
			}
		}
	}
	res, err := s.ops.Do(r.Context(), c, "instance.create", ops.ArgsOf(m))
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	writeJSON(w, res)
}

func writePortBusyErr(w http.ResponseWriter, busy *inst.FormPortBusy) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       false,
		"error":    busy.Error(),
		"portBusy": busy,
	})
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

// handlePreviewSettings 不落盘地算出「启动与内存」页当前填写内容会存下、会使用的完整参数（A28，A25 保存参数校验）。
func (s *Server) handlePreviewSettings(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if err := s.ops.Check(c, "instance.list"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	var body struct {
		XmxMB           int       `json:"xmxMb"`
		XmsMB           int       `json:"xmsMb"`
		ConsoleEncoding string    `json:"consoleEncoding"`
		ExtraJVM        *[]string `json:"extraJvm"`
	}
	if err := readJSON(r, &body); err != nil {
		err = bodyErr(err)
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	name := r.PathValue("name")
	if _, err := s.mgr.Get(name); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	res, err := s.mgr.PreviewSettings(name, body.XmxMB, body.XmsMB, body.ConsoleEncoding, body.ExtraJVM)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, res)
}
