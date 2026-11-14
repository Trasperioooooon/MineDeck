package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
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
	r, cleanup := beginUpload(w, r)
	defer cleanup()
	// 存文件的这半步留在网页层（和整合包上传一样不占单次操作记录）；摘要走 instance.transfer.scan
	s.do(w, r, "instance.transfer.scan", func() (ops.Args, error) {
		if err := parseUpload(w, r, transferUploadLimit, transferTooLarge); err != nil {
			return nil, err
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少交接包文件")
		}
		defer file.Close()
		// 只拿上传名的扩展名做判断，落盘的名字自己起：上传名是对方给的，可以超长、可以带冒号
		// （写下来是备用数据流）、扩展名可以比整条名字的上限还长。
		ext := strings.ToLower(filepath.Ext(filepath.Base(hdr.Filename)))
		if ext != ".mdpack" && ext != ".zip" {
			shown := []rune(filepath.Base(hdr.Filename))
			if len(shown) > 40 {
				shown = append(shown[:40], '…')
			}
			return nil, fmt.Errorf("只支持 .mdpack 交接包（收到 %s）", string(shown))
		}
		dir := filepath.Join(app.CacheDir, "transfer", "import")
		dest := filepath.Join(dir, fmt.Sprintf("%d.mdpack", time.Now().UnixNano()))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, ops.Wrap(500, err)
		}
		sweepUploads(dir) // 顺手清掉很早以前传了没导入的：交接包动辄几个 GB，不能攒着
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return nil, ops.Wrap(500, err)
		}
		cerr := copyUpload(r, out, file, hdr.Size, transferTooLarge)
		if werr := out.Close(); cerr == nil {
			cerr = ops.Wrap(500, werr)
		}
		if cerr != nil {
			os.Remove(dest) // 传了一半的不留：GB 级的半截文件最占地方
			return nil, fmt.Errorf("保存上传文件失败: %w", cerr)
		}
		// 落盘了就顺手校验：不是交接包、版本太新、类型不对的当场告诉用户并删掉，别等导入时才发现，也别留在缓存里占地方
		if _, err := transfer.Summarize(dest); err != nil {
			os.Remove(dest)
			return nil, err
		}
		return ops.ArgsOf(map[string]any{"file": dest}), nil
	})
}

// 上传的上限。包变量是为了测试能调小。
var (
	transferUploadLimit int64 = 8 << 30 // 交接包常以 GB 计
	modpackUploadLimit  int64 = 4 << 30
)

// 超过上限时给用户看的话。说清楚是太大了、上限是多少、还有什么别的办法：原来报的是
// 「上传解析失败: http: request body too large」，用户多半以为文件坏了，一遍遍重试。
const (
	transferTooLarge = "这个交接包超过了 8 GB 的上限，导不进来。存档这么大的服务器，把整个服务器文件夹拷到这台电脑上，再到「新建服务器」页选「接管已有服务器」。"
	modpackTooLarge  = "这个整合包超过了 4 GB 的上限，导不进来。"
)

// parseUpload 解析 multipart 上传。超过 limit 时返回 tooLarge 这句话：请求头里写明了大小的，不等传完就拒掉；
// 没写或者写小了的，读到上限时拒掉。
func parseUpload(w http.ResponseWriter, r *http.Request, limit int64, tooLarge string) (err error) {
	defer func() { err = uploadError(r, err, tooLarge) }()
	// 请求体比文件本身多出 multipart 的分隔线和别的表单项。界面是按文件大小拦的，这里留 1 MiB 余量，
	// 免得一个刚好卡在上限以内的文件，界面放行、传完了又被这里拒掉。
	limit += 1 << 20
	if r.ContentLength > limit {
		return ops.Errorf(http.StatusRequestEntityTooLarge, "%s", tooLarge)
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	// ParseMultipartForm 超出内存额度会写系统临时目录，所以对临时目录所在的盘同样查余量（开头一次、每收一段一次）。
	// 只查余量，不占那个盘的独占名额：请求体是按对方的网速进来的，占着名额会让同盘的其他写盘操作等它传完，
	// 自己排队等名额的时间又会被算成「上传没有进展」
	lease, err := diskguard.Acquire(r.Context(), os.TempDir(), diskguard.Options{Shared: true, Need: max(r.ContentLength, 0), Query: uploadDiskFree})
	if err != nil {
		return err
	}
	defer lease.Close()
	r.Body = &readerCloser{Reader: lease.Reader(r.Body), Closer: r.Body}
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) || strings.Contains(err.Error(), "request body too large") {
			return ops.Errorf(http.StatusRequestEntityTooLarge, "%s", tooLarge)
		}
		return fmt.Errorf("上传解析失败: %w", err)
	}
	// multipart 的结束边界后还可能有请求尾部；在同一限长 reader 上读完，
	// 让 HTTP 能感知客户端断开，再撤销网络空闲期限，进入本机复制阶段。
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return err
	}
	if b := uploadOf(r); b != nil {
		if b.stop() || r.Context().Err() != nil {
			return r.Context().Err()
		}
	}
	return nil
}

var multipartMemory int64 = 64 << 20 // 测试调小，以实际触发标准库的临时文件落盘。
var uploadDiskFree = diskguard.Free  // 测试注入查询失败和余量不足，生产使用真实卷查询。

type readerCloser struct {
	io.Reader
	io.Closer
}

// copyUpload 仅覆盖系统临时文件／内存到缓存的复制，不持锁解析包或启动后续导入。
// 同样只查余量、不排队：文件已经收完了，这时候再让请求挂着等别的操作，对方只会看到传到头却不结束。
func copyUpload(r *http.Request, out *os.File, src io.Reader, size int64, tooLarge string) error {
	lease, err := diskguard.Acquire(r.Context(), filepath.Dir(out.Name()), diskguard.Options{Shared: true, Need: max(size, 0), Query: uploadDiskFree})
	if err != nil {
		return uploadError(r, ops.Wrap(500, err), tooLarge)
	}
	defer lease.Close()
	_, err = io.Copy(lease.Writer(out), src)
	if err == nil {
		err = r.Context().Err()
	}
	return uploadError(r, ops.Wrap(500, err), tooLarge)
}

// sweepUploads 清掉上传目录里 24 小时还没导入的包（导入任务自己会删用完的；这是给传完没用的兜底）。
func sweepUploads(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if st, err := e.Info(); err == nil && st.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
