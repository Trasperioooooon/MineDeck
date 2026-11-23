package worldimport

import (
	"path/filepath"
	"strings"
	"time"
)

// 恢复点大小的缓存（A53）。「换地图」页每隔几秒查一次世界状态（Status），以前每次都把每个恢复点的文件夹数一遍大小，
// 恢复点一多、世界一大就慢，界面只好放慢轮询。现在同一份记录（编号、更新时间、状态都没变）只数一次。
//
// 只缓存落定了的记录（已导入、没有生效、已换回）：之后它们的文件夹只随记录一起变，而每次 saveJournal 都会换更新时间。
// 正在进行的导入、正在换、卡住的每次都重数。记录落盘以后还有收尾（退回以后才删暂存区），所以导入、换回、删恢复点、
// 启动恢复做完时整个作废（forgetSizes）；作废的同时有人在数的，数完不存（sizeGen）。

// pointSize 一份记录的三样大小，连同数的时候记录的更新时间和状态（对不上就重数）。
type pointSize struct {
	updated           time.Time
	state             string
	old, kept, staged int64
}

// countPointBytes 数一个文件夹里文件的总大小。测试里换成会计数的，看缓存有没有生效。
var countPointBytes = dirBytes

// sizeSettled 这种状态的记录落定了，大小可以缓存。
func sizeSettled(state string) bool {
	return state == TxCommitted || state == TxRolledBack || state == TxRestored
}

// pointSizes 一台服务器各份记录的大小（记录编号 → 大小）：缓存里对得上的直接用，其余的现数，落定了的存起来。
// 数的时候不拿 s.mu（取消、closeCancel 也要这把锁）。缓存里只留这次列出来的记录，删掉的恢复点跟着出去。
func (s *Service) pointSizes(dir string, js []*Journal) map[string]pointSize {
	key := sizeKey(dir)
	s.mu.Lock()
	gen, cached := s.sizeGen, s.sizes[key]
	s.mu.Unlock()
	out := make(map[string]pointSize, len(js))
	keep := make(map[string]pointSize, len(js))
	for _, j := range js {
		if c, ok := cached[j.ID]; ok && c.updated.Equal(j.Updated) && c.state == j.State {
			out[j.ID], keep[j.ID] = c, c
			continue
		}
		root := txAbs(dir, j.ID)
		ps := pointSize{updated: j.Updated, state: j.State,
			old:    countPointBytes(filepath.Join(root, oldSub)),
			kept:   countPointBytes(filepath.Join(root, restoreSub)),
			staged: countPointBytes(filepath.Join(root, stageSub))}
		out[j.ID] = ps
		if sizeSettled(j.State) && !s.isActive(root) {
			keep[j.ID] = ps
		}
	}
	s.mu.Lock()
	if s.sizeGen == gen {
		s.sizes[key] = keep
	}
	s.mu.Unlock()
	return out
}

// sizeKey 缓存里认实例目录用的键（Windows 上路径不分大小写）。
func sizeKey(dir string) string { return strings.ToLower(filepath.Clean(dir)) }

// forgetSizes 缓存的大小全部作废。导入、换回旧地图、删恢复点、启动恢复做完时调。
func (s *Service) forgetSizes() {
	s.mu.Lock()
	s.sizeGen++
	s.sizes = map[string]map[string]pointSize{}
	s.mu.Unlock()
}
