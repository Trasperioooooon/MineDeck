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

// transferRoutes 实例交接包（.mdpack）的三个 HTTP 适配：导出、上传 + 摘要、导入。
// 上传路径和整合包导入（/api/import/modpack）对称：multipart 里的 file 存进缓存目录的独立子目录，
// 再把缓存路径交给操作层——操作层只认这个子目录。
func (s *Server) transferRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/instances/{name}/transfer/export", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.transfer.export", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/transfer/upload", s.handleTransferUpload)
	mux.HandleFunc("POST /api/transfer/import", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.transfer.import", bodyArgs(r))
	})
}

// handleTransferUpload 接收 .mdpack 上传（multipart），存进缓存目录后正好读个摘要给向导展示。
func (s *Server) handleTransferUpload(w http.ResponseWriter, r *http.Request) {
	// 存文件的这半步留在网页层（和整合包上传一样不占单次操作记录）；摘要走 instance.transfer.scan
	s.do(w, r, "instance.transfer.scan", func() (ops.Args, error) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<30) // 交接包常以 GB 计，给 8 GiB 上限
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil, fmt.Errorf("上传解析失败: %w", err)
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少交接包文件")
		}
		defer file.Close()
		base := filepath.Base(hdr.Filename)
		if base == "" || base == "." {
			base = "pack.mdpack"
		}
		if ext := strings.ToLower(filepath.Ext(base)); ext != ".mdpack" && ext != ".zip" {
			return nil, fmt.Errorf("只支持 .mdpack 交接包（收到 %s）", base)
		}
		dest := filepath.Join(app.CacheDir, "transfer", "import", fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base))
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
		// 落盘了就顺手校验得上读不出，坏了当场告诉用户，别等导入时才发现
		if _, err := transfer.ReadManifest(dest); err != nil {
			os.Remove(dest)
			return nil, err
		}
		return ops.ArgsOf(map[string]any{"file": dest}), nil
	})
}
