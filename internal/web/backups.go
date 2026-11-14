package web

import (
	"fmt"
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
		// 请求体可以省略（空的、不是 JSON 的都按没带备注算）；但超过上限要照实回 413、不往下做：
		// 以前这里把所有错都吞了，超限的请求照样去创建备份（2026-11-14 Codex 第二轮复审）
		if err := readJSON(r, &body); bodyTooLarge(err) {
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
