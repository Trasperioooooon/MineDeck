package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"minedeck/internal/inst"
	"minedeck/internal/linkmode"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/worldimport"
)

func (s *Server) handleGetProps(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if err := s.ops.Check(c, "instance.props.get"); err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	name := r.PathValue("name")
	res, err := s.ops.Do(r.Context(), c, "instance.props.get", ops.ArgsOf(map[string]any{"inst": name}))
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	if m, ok := res.(map[string]any); ok {
		if rules := s.readInstanceGameRules(name); len(rules) > 0 {
			m["gamerules"] = rules
		}
	}
	writeJSON(w, res)
}

// readInstanceGameRules 按实例的 server.properties 里写的 level-name 找到世界存档目录，读出四项迁移为游戏规则的开关当前值。
// 世界还没生成、level-name 越界或有歧义、中间目录含链接、存档损坏时都返回 nil，界面退回 server.properties。
func (s *Server) readInstanceGameRules(name string) map[string]string {
	i, err := s.mgr.Get(name)
	if err != nil || mcsrc.KindOf(i.Core) == mcsrc.KindProxy || !inst.UsesServerGameRules(i.MC) || !filepath.IsAbs(i.Dir) {
		return nil
	}
	if !isPlainDir(i.Dir) {
		return nil
	}
	if _, ok := inst.CheckLevelName(i.Dir); !ok {
		return nil
	}
	lvl, err := inst.LevelNameStrict(i.Dir)
	if err != nil || lvl == "" {
		return nil
	}
	cur := i.Dir
	for _, seg := range strings.Split(lvl, "/") {
		cur = filepath.Join(cur, seg)
		if !isPlainDir(cur) {
			return nil
		}
	}
	return worldimport.ReadServerGameRules(cur)
}

func isPlainDir(p string) bool {
	return linkmode.IsPlainDir(p)
}

func (s *Server) handleSetProps(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.props.set", bodyArgs(r, "inst", r.PathValue("name")))
}
