package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

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

const worldTooLarge = "这个压缩包超过了 8 GB 的上限。这么大的地图，请在运行 MineDeck 的那台电脑上直接选择地图文件夹导入。"

// handleWorldUpload 接收地图上传（multipart，字段名 file），流式写进缓存目录，再交给 world.import.scan 认。
func (s *Server) handleWorldUpload(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "world.import.scan", func() (ops.Args, error) {
		if r.ContentLength > worldUploadLimit+(1<<20) {
			return nil, ops.Errorf(http.StatusRequestEntityTooLarge, "%s", worldTooLarge)
		}
		r.Body = http.MaxBytesReader(w, r.Body, worldUploadLimit+(1<<20))
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, fmt.Errorf("上传解析失败：%w", err)
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				var big *http.MaxBytesError
				if errors.As(err, &big) {
					return nil, ops.Errorf(http.StatusRequestEntityTooLarge, "%s", worldTooLarge)
				}
				return nil, fmt.Errorf("缺少地图文件")
			}
			if part.FormName() != "file" || part.FileName() == "" {
				part.Close()
				continue
			}
			// 落盘的名字自己起：上传名是对方给的，只拿来显示和认扩展名
			name := filepath.Base(part.FileName())
			dest, err := worldimport.SaveUpload(part, worldUploadLimit, r.ContentLength)
			if err == nil {
				part.Close()
			}
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
				}
				return nil, ops.Errorf(500, "保存上传的文件失败：%w", err)
			}
			if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
				os.Remove(dest)
				return nil, fmt.Errorf("上传的文件是空的")
			}
			return ops.ArgsOf(map[string]any{"file": dest, "name": name}), nil
		}
	})
}
