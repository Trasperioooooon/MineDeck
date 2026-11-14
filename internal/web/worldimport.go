package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minedeck/internal/diskguard"
	"minedeck/internal/ops"
	"minedeck/internal/worldimport"
)

// worldRoutes 地图导入的 HTTP 适配（逻辑在 internal/worldimport，权限在操作层 world.*）。
//
// 本机的人可以直接给路径（world.import.scanpath，仅本机）；别的设备只能上传 ZIP。上传是流式写进缓存目录的：
// 不经 ParseMultipartForm，多 GB 的地图不会先在系统临时目录里落一份再拷一遍。
func (s *Server) worldRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/world/scan", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.scanpath", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/world/upload", s.handleWorldUpload)
	mux.HandleFunc("POST /api/world/readme", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.readme", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/world/plan", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.plan", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/world/import", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.run", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/world/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.cancel", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/world/discard", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.import.discard", bodyArgs(r))
	})
	mux.HandleFunc("GET /api/instances/{name}/world", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.status", instArg(r))
	})
	mux.HandleFunc("POST /api/instances/{name}/world/restore", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.restore", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/world/delete", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.point.delete", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/world/retry", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "world.recover.retry", instArg(r))
	})
}

// 地图上传的上限（包变量是为了测试能调小）。更大的地图在这台电脑上直接选文件夹，不走上传。
var worldUploadLimit int64 = 8 << 30

// 每次真正读请求体时续期；一直有进展的大地图不受总耗时限制。
var worldUploadIdle = 30 * time.Second

const worldUploadFormLimit int64 = 1 << 20

type uploadBody struct {
	io.ReadCloser
	control          *http.ResponseController
	idle             time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	timer            *time.Timer
	stopCancel       func() bool
	expired, stopped bool
}

type uploadBodyKey struct{}

// beginUpload 为所有上传入口设置可取消的读取期限，并负责 HTTP 连接和 multipart 临时文件的收尾。
func beginUpload(w http.ResponseWriter, r *http.Request) (*http.Request, func()) {
	if uploadOf(r) != nil {
		return r, func() {}
	}
	control := http.NewResponseController(w)
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
	original := r.Body
	body := newUploadBody(r.Context(), original, control)
	r = r.WithContext(context.WithValue(body.ctx, uploadBodyKey{}, body))
	r.Body = body
	return r, func() {
		body.stop()
		body.cancel()
		_ = control.SetReadDeadline(time.Now())
		_ = original.Close()
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}
}

// 上传的拒绝也包括鉴权失败：这一层必须在 secure 前面，不能让未登录的
// 客户端通过不再发送请求体，把拒绝响应和连接收尾一直拖住。
func guardUploads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/world/upload", "/api/transfer/upload", "/api/import/modpack", "/api/modcart/local":
				var cleanup func()
				r, cleanup = beginUpload(w, r)
				defer cleanup()
			}
		}
		next.ServeHTTP(w, r)
	})
}

func uploadOf(r *http.Request) *uploadBody {
	b, _ := r.Context().Value(uploadBodyKey{}).(*uploadBody)
	return b
}

func newUploadBody(parent context.Context, body io.ReadCloser, control *http.ResponseController) *uploadBody {
	ctx, cancel := context.WithCancel(parent)
	b := &uploadBody{ReadCloser: body, control: control, idle: worldUploadIdle, ctx: ctx, cancel: cancel}
	_ = control.SetReadDeadline(time.Now().Add(b.idle))
	b.timer = time.AfterFunc(b.idle, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.stopped {
			b.expired = true
			b.cancel() // 等磁盘名额、没有正在读网络时也能退出
			_ = b.control.SetReadDeadline(time.Now())
		}
	})
	b.stopCancel = context.AfterFunc(ctx, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.stopped {
			_ = b.control.SetReadDeadline(time.Now())
		}
	})
	return b
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.mu.Lock()
		if !b.stopped && !b.expired {
			b.timer.Reset(b.idle)
			_ = b.control.SetReadDeadline(time.Now().Add(b.idle))
		}
		b.mu.Unlock()
	}
	return n, err
}

func (b *uploadBody) stop() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	b.timer.Stop()
	b.stopCancel()
	_ = b.control.SetReadDeadline(time.Time{})
	return b.expired
}

func (b *uploadBody) timedOut() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.expired
}

func uploadReadError(err error) error {
	return uploadError(nil, err, worldTooLarge)
}

func uploadError(r *http.Request, err error, tooLarge string) error {
	if err == nil {
		return nil
	}
	if r != nil {
		if b := uploadOf(r); b != nil && b.timedOut() {
			return ops.Errorf(http.StatusRequestTimeout, "上传长时间没有新数据，已停止接收，请重新上传")
		}
	}
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		return ops.Errorf(http.StatusRequestEntityTooLarge, "%s", tooLarge)
	}
	var space *diskguard.NoSpace
	if errors.As(err, &space) {
		return ops.Errorf(http.StatusInsufficientStorage, "接收上传文件的磁盘空间不足，请先腾出空间：%v", space)
	}
	if errors.Is(err, diskguard.ErrUnknown) {
		return ops.Errorf(http.StatusServiceUnavailable, "暂时查不到接收上传文件所在磁盘的剩余空间，没有接收文件。请稍后重试")
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return ops.Errorf(http.StatusRequestTimeout, "上传长时间没有新数据，已停止接收，请重新上传")
	}
	return err
}

const worldTooLarge = "这个压缩包超过了 8 GB 的上限。这么大的地图，请在运行 MineDeck 的那台电脑上直接选择地图文件夹导入。"

// handleWorldUpload 接收地图上传（multipart，字段名 file），流式写进缓存目录，再交给 world.import.scan 认。
func (s *Server) handleWorldUpload(w http.ResponseWriter, r *http.Request) {
	// HTTP/1 默认会在发响应前排空一部分请求体；对方停传时这一步也会卡住。
	// 上传独占这一条连接，拒绝时不复用；返回前令后续 Close 的读取立即结束。
	r, cleanup := beginUpload(w, r)
	defer cleanup()
	body := uploadOf(r)
	s.do(w, r, "world.import.scan", func() (a ops.Args, err error) {
		defer func() {
			if body.timedOut() {
				a, err = nil, ops.Errorf(http.StatusRequestTimeout, "上传长时间没有新数据，已停止接收，请重新上传")
			}
		}()
		if r.ContentLength > worldUploadLimit+worldUploadFormLimit {
			return nil, ops.Errorf(http.StatusRequestEntityTooLarge, "%s", worldTooLarge)
		}
		r.Body = http.MaxBytesReader(w, r.Body, worldUploadLimit+worldUploadFormLimit)
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, fmt.Errorf("上传解析失败：%w", err)
		}
		var formBytes int64
		for parts := 0; parts < 16; parts++ {
			part, err := mr.NextPart()
			if err != nil {
				var big *http.MaxBytesError
				if errors.As(err, &big) {
					return nil, ops.Errorf(http.StatusRequestEntityTooLarge, "%s", worldTooLarge)
				}
				if errors.Is(err, io.EOF) {
					return nil, fmt.Errorf("缺少地图文件")
				}
				return nil, uploadReadError(fmt.Errorf("读取上传表单失败：%w", err))
			}
			if part.FormName() != "file" || part.FileName() == "" {
				n, err := io.Copy(io.Discard, io.LimitReader(part, worldUploadFormLimit-formBytes+1))
				formBytes += n
				if err != nil {
					return nil, uploadReadError(err)
				}
				if formBytes > worldUploadFormLimit {
					return nil, ops.Errorf(413, "上传附带的表单内容太大，没有接收地图")
				}
				continue
			}
			// 落盘的名字自己起：上传名是对方给的，只拿来显示和认扩展名
			name := filepath.Base(part.FileName())
			dest, err := worldimport.SaveUploadCtx(r.Context(), part, worldUploadLimit, r.ContentLength)
			// 被拒的时候不关这一段：关它会把这一段剩下的内容读完，几个 GB 的上传要读很久才回得了 429、507。
			// 不读完，连接由服务端在回完以后关掉
			if err != nil {
				var big *http.MaxBytesError
				switch {
				case errors.Is(err, worldimport.ErrUploadTooLarge) || errors.As(err, &big):
					return nil, ops.Errorf(http.StatusRequestEntityTooLarge, "%s", worldTooLarge)
				case errors.Is(err, worldimport.ErrUploadBusy):
					return nil, ops.Errorf(http.StatusTooManyRequests, "%v", err)
				case errors.Is(err, worldimport.ErrUploadNoSpace):
					return nil, ops.Errorf(http.StatusInsufficientStorage, "运行 MineDeck 的电脑上%v，没有收下这个文件。请先腾出空间，或者在那台电脑上直接选地图文件夹导入", err)
				case errors.Is(err, diskguard.ErrUnknown):
					return nil, ops.Errorf(http.StatusServiceUnavailable, "暂时查不到上传缓存所在磁盘的剩余空间，没有接收文件。请稍后重试，或在这台电脑上直接选择地图文件夹")
				}
				return nil, uploadReadError(ops.Errorf(500, "保存上传的文件失败：%w", err))
			}
			// 文件已经读到边界，再有界读完请求尾部。否则扫描期间请求体还开着，
			// 读期限会误伤正常扫描，客户端断开也可能迟迟不能传到扫描的 ctx。
			n, err := io.Copy(io.Discard, io.LimitReader(r.Body, worldUploadFormLimit-formBytes+1))
			if err != nil || n+formBytes > worldUploadFormLimit {
				_ = os.Remove(dest)
				if err != nil {
					return nil, uploadReadError(err)
				}
				return nil, ops.Errorf(413, "上传附带的内容太大，没有接收地图")
			}
			if body.stop() || body.ctx.Err() != nil {
				_ = os.Remove(dest)
				return nil, body.ctx.Err()
			}
			if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
				os.Remove(dest)
				return nil, fmt.Errorf("上传的文件是空的")
			}
			return ops.ArgsOf(map[string]any{"file": dest, "name": name}), nil
		}
		return nil, ops.Errorf(400, "上传表单的字段太多，请只选择一个地图文件")
	})
}
