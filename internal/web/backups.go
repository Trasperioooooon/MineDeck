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
		_ = readJSON(r, &body) // 请求体可省略
		return ops.ArgsOf(map[string]any{"inst": r.PathValue("name"), "label": body.Label}), nil
	})
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.backup.restore", func() (ops.Args, error) {
		var body struct {
			File string `json:"file"`
		}
		if err := readJSON(r, &body); err != nil || body.File == "" {
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
