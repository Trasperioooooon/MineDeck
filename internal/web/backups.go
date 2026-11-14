package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"minedeck/internal/ops"
)

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.backups.list", instArg(r))
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.backup.create", func() (ops.Args, error) {
		var body struct {
			Label string `json:"label"`
		}
		// 请求体可以省略，但只有空的请求体算省略（没带备注）。超过上限回 413，写坏了的 JSON 回 400，都不往下做：
		// 以前这里把所有错都吞了，超限的、写坏的请求照样去创建备份（2026-11-14 Codex 第二轮复审）
		if err := readJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			return nil, bodyErr(err)
		}
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "label": body.Label}), nil
	})
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.backup.restore", func() (ops.Args, error) {
		var body struct {
			File string `json:"file"`
		}
		err := readJSON(r, &body)
		if bodyTooLarge(err) {
			return nil, bodyErr(err)
		}
		if err != nil || body.File == "" {
			return nil, fmt.Errorf("缺少备份文件名")
		}
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "file": body.File}), nil
	})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.backup.delete", func() (ops.Args, error) {
		f := r.URL.Query().Get("file")
		if f == "" {
			return nil, fmt.Errorf("缺少备份文件名")
		}
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "file": f}), nil
	})
}
