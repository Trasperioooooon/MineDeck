package web

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"minedeck/internal/ops"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/toolbox"
)

// draftPatchArgs 解析工具箱草稿保存请求的 JSON 体，并补充请求头里可选的 X-Draft-Client / X-Draft-Seq（防乱序覆盖，A65）。
func draftPatchArgs(r *http.Request) func() (ops.Args, error) {
	base := bodyArgs(r)
	cid := strings.TrimSpace(r.Header.Get("X-Draft-Client"))
	seqStr := strings.TrimSpace(r.Header.Get("X-Draft-Seq"))
	return func() (ops.Args, error) {
		a, err := base()
		if err != nil {
			return nil, err
		}
		if cid == "" && seqStr == "" {
			return a, nil
		}
		var raw map[string]any
		if len(a) > 0 {
			if uerr := json.Unmarshal(a, &raw); uerr != nil {
				// JSON 语法不对或不是对象：交回给 ops 按原样报错
				return a, nil
			}
		}
		if raw == nil {
			raw = map[string]any{}
		}
		changed := false
		if cid != "" && raw["clientId"] == nil {
			raw["clientId"] = cid
			changed = true
		}
		if seqStr != "" && raw["seq"] == nil {
			if seq, perr := strconv.ParseInt(seqStr, 10, 64); perr == nil && seq > 0 {
				raw["seq"] = seq
				changed = true
			}
		}
		if !changed {
			return a, nil
		}
		out, merr := json.Marshal(raw)
		if merr != nil {
			return a, nil
		}
		return ops.Args(out), nil
	}
}

func (s *Server) toolboxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/toolbox/skin-pack/preview", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "toolbox.skinpack.preview", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/toolbox/skin-pack/build", s.handleSkinPackBuild)
	mux.HandleFunc("GET /api/toolbox/skin-pack/download/{id}", s.handleToolboxDownload)

	mux.HandleFunc("GET /api/toolbox/mrpack/draft", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "toolbox.mrpack.draft.get", nil)
	})
	mux.HandleFunc("PUT /api/toolbox/mrpack/draft", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "toolbox.mrpack.draft.save", draftPatchArgs(r))
	})
	mux.HandleFunc("POST /api/toolbox/mrpack/preview", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "toolbox.mrpack.preview", bodyArgs(r))
	})
	mux.HandleFunc("POST /api/toolbox/mrpack/build", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "toolbox.mrpack.build", bodyArgs(r))
	})
	mux.HandleFunc("GET /api/toolbox/mrpack/download/{id}", s.handleToolboxDownload)
}

var skinPackUploadLimit int64 = toolbox.MaxSkinUploadBytes

const skinPackTooLarge = "上传的皮肤图片总大小超过了 40 MB 上限（单张皮肤不超过 2 MB，最多 18 张）"

// handleSkinPackBuild 接收 multipart 皮肤包构建请求：meta（JSON）+ file_0..file_17（PNG 文件），
// 先将上传文件安全落盘为缓存区临时文件并严格清理，具体 PNG 解码与路径校验由操作层和 toolbox 完成。
func (s *Server) handleSkinPackBuild(w http.ResponseWriter, r *http.Request) {
	r, cleanup := beginUpload(w, r)
	defer cleanup()

	var temps []string
	defer func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}()

	s.do(w, r, "toolbox.skinpack.build", func() (ops.Args, error) {
		if err := parseUpload(w, r, skinPackUploadLimit, skinPackTooLarge); err != nil {
			return nil, err
		}
		metaRaw := strings.TrimSpace(r.FormValue("meta"))
		if metaRaw == "" {
			return nil, ops.Errorf(400, "缺少皮肤包配置内容（meta）")
		}
		var argsMap map[string]any
		if err := json.Unmarshal([]byte(metaRaw), &argsMap); err != nil || argsMap == nil {
			return nil, ops.Errorf(400, "皮肤包配置格式不对")
		}
		if r.MultipartForm == nil {
			return nil, ops.Errorf(400, "没有收到上传文件")
		}
		dir := catalog.SkinUploadDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, ops.Errorf(500, "准备皮肤上传目录失败")
		}
		sweepUploads(dir)

		filesMap := map[string]string{}
		for fieldName, headers := range r.MultipartForm.File {
			if len(headers) == 0 {
				continue
			}
			idxStr, ok := strings.CutPrefix(fieldName, "file_")
			if !ok {
				continue
			}
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 0 || idx >= toolbox.MaxSkins {
				return nil, ops.Errorf(400, "皮肤上传字段编号不对：%s", fieldName)
			}
			hdr := headers[0]
			if hdr.Size > toolbox.MaxSkinBytes {
				return nil, ops.Errorf(400, "第 %d 行的皮肤超过了 2 MB 上限", idx+1)
			}
			f, err := hdr.Open()
			if err != nil {
				return nil, fmt.Errorf("打开第 %d 行上传的皮肤失败", idx+1)
			}
			tmp, err := os.CreateTemp(dir, "*.skin")
			if err != nil {
				f.Close()
				return nil, ops.Errorf(500, "保存皮肤临时文件失败")
			}
			tmpPath := tmp.Name()
			temps = append(temps, tmpPath)
			cerr := copyUpload(r, tmp, f, hdr.Size, skinPackTooLarge)
			_ = f.Close()
			if werr := tmp.Close(); cerr == nil {
				cerr = ops.Wrap(500, werr)
			}
			if cerr != nil {
				return nil, cerr
			}
			filesMap[idxStr] = tmpPath
		}
		argsMap["files"] = filesMap
		return ops.ArgsOf(argsMap), nil
	})
}

func (s *Server) handleToolboxDownload(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if err := s.ops.Check(c, "toolbox.download"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	res, err := s.ops.Do(r.Context(), c, "toolbox.download", ops.ArgsOf(map[string]any{"id": r.PathValue("id")}))
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	entry, ok := res.(toolbox.DownloadEntry)
	if !ok {
		writeErr(w, 500, fmt.Errorf("下载结果格式不对"))
		return
	}
	f, err := os.Open(entry.Path)
	if err != nil {
		writeErr(w, 404, fmt.Errorf("下载文件已经不在了，请重新生成"))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		writeErr(w, 404, fmt.Errorf("下载文件不可读"))
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": entry.FileName}))
	http.ServeContent(w, r, entry.FileName, entry.At, f)
}
