package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/ops"
	"minedeck/internal/transfer"
)

// 服务器打包 / 收包的网页接口（操作是 instance.transfer.*，打包与收包都仅本机）。
//
//	POST /api/transfer/upload  multipart 上传收到的 .mdpack：落进 cache/transfer，返回 {file, summary}
//	POST /api/transfer/export  打包一台实例 → {taskId}
//	POST /api/transfer/import  收包成实例 → {taskId}
func (s *Server) transferRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/transfer/upload", s.handleTransferUpload)
	mux.HandleFunc("POST /api/transfer/export", s.handleTransferExport)
	mux.HandleFunc("POST /api/transfer/import", s.handleTransferImport)
}

// handleTransferUpload 接收 .mdpack 上传（multipart），存进缓存目录后交给操作 instance.transfer.inspect
// 清点。返回的 file 是上传存下的路径，收包时原样带回（instance.transfer.import 只认这个目录里的文件）。
func (s *Server) handleTransferUpload(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if err := s.ops.Check(c, "instance.transfer.inspect"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<30) // 世界存档可能不小：与整合包导入同级的余量，上限 8 GB
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, fmt.Errorf("上传解析失败: %w", err))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, fmt.Errorf("缺少 .mdpack 文件"))
		return
	}
	defer file.Close()
	base := filepath.Base(hdr.Filename)
	if base == "" || base == "." {
		base = "pack" + transfer.Ext
	}
	if !hasMDPackExt(base) {
		writeErr(w, 400, fmt.Errorf("收的包应是 .mdpack 文件（选到的是 %s）", hdr.Filename))
		return
	}
	dest := filepath.Join(app.CacheDir, "transfer", fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		writeErr(w, 500, err)
		return
	}
	out, err := os.Create(dest)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	_, cerr := io.Copy(out, file)
	if werr := out.Close(); cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		os.Remove(dest)
		writeErr(w, 500, fmt.Errorf("保存上传的包失败: %w", cerr))
		return
	}
	res, err := s.ops.Do(r.Context(), c, "instance.transfer.inspect", ops.ArgsOf(map[string]any{"file": dest}))
	if err != nil {
		os.Remove(dest)
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	writeJSON(w, map[string]any{"file": dest, "summary": res})
}

func hasMDPackExt(name string) bool {
	return strings.EqualFold(filepath.Ext(name), transfer.Ext)
}

func (s *Server) handleTransferExport(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.transfer.export", bodyArgs(r))
}

func (s *Server) handleTransferImport(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.transfer.import", bodyArgs(r))
}
