package modcart

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"minedeck/internal/atomicfile"
)

// ---- 删掉一台服务器时清它的清单 ----
//
// 清单按实例名存，删服务器时要把这台的清单清掉，不然以后新建、接管或导入一台同名的，会看到上一台留下的东西。
// cart.json 在却没读成的那段时间（见 LoadError）清不了：那时一律不写 cart.json。所以把「这台要清」连同删除的时间
// 记进 cart.json 旁边的 cart-pending-removals.json，cart.json 读成的那一刻（启动时，或者没读成以后再读成）先补做，
// 再办触发这次重读的请求（A30）。
//
// 补做的规矩：
//   - 只清删除之前就在清单里的。Modrinth 项目和本地文件都按加入时间比，删除以后才加进来的（同名新建的那台加的）留着；
//   - 草稿清单记的目标服务器还是这个名字、又是删除之前记的（老文件没有记的时间，按删除之前算），记号清掉，
//     草稿清单本身留着；删除以后记的是同名新建的那台，留着；
//   - 这个文件读不出来、内容坏了：留着不动，记一句程序日志，下次读成 cart.json（下次启动）时再试，不挡 cart.json 的读写；
//   - cart.json 存成了才删这个文件。没删掉的话下次再补一遍：清单里的东西和记号都按时间比，删除以后加的、记的不动，结果一样。

const removalsName = "cart-pending-removals.json"

type removalsFile struct {
	Version  int       `json:"version"`
	Removals []removal `json:"removals"`
}

// removal 一台删掉了、清单还没清的服务器。
type removal struct {
	Inst string    `json:"inst"` // 车的键：实例名
	At   time.Time `json:"at"`   // 删除的时间（UTC）
}

func (s *Service) removalsPath() string { return filepath.Join(s.dir, removalsName) }

// ForgetInstance 删掉一台服务器时调：这台的清单整个清掉（Modrinth 项目、本地文件的登记和暂存的文件），草稿清单记的目标
// 是这台的，记号也清掉（草稿清单本身留着，A31）。cart.json 没读成时清不了，记进删服记录等读成以后补做；
// 记也记不下来、或者 cart.json 存不进去时返回错误。删服本身不受影响，由调用方照实说明。
func (s *Service) ForgetInstance(inst string) error {
	if inst == Draft || inst == Toolbox {
		return nil // 不是服务器的清单
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retryLoadLocked() != nil {
		if err := s.recordRemovalLocked(removal{Inst: inst, At: time.Now().UTC()}); err != nil {
			log.Printf("删服务器「%s」时待装清单读不出来，也没能记下来以后再清: %v", inst, err)
			return errorf(500, "待装清单读不出来，也没能记下来等读出来以后再清（%v）", err)
		}
		log.Printf("删服务器「%s」时待装清单读不出来，记下了，读出来以后再清这台的清单", inst)
		return nil
	}
	if err := s.clearAllLocked(inst, true); err != nil {
		log.Printf("删服务器「%s」时待装清单没存进去: %v", inst, err)
		return err
	}
	return nil
}

// readRemovals 读删服记录。文件不在时 found 为 false。
func (s *Service) readRemovals() (list []removal, found bool, err error) {
	b, err := os.ReadFile(s.removalsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, noPath(err)
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var f removalsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, true, jsonErrAt(b, err)
	}
	if f.Removals == nil {
		return nil, true, errors.New(`文件里找不到 "removals" 这一项`)
	}
	return f.Removals, true, nil
}

// recordRemovalLocked 把 r 加进删服记录；同一台记过的，时间取后一次（清到后一次删除为止，前一次的也就清了）。调用方持有 s.mu。
// 已有的记录读不出来时不覆盖它，报错：覆盖就把上次记下的丢了。不建文件夹：cart.json 读不出来可能是程序文件夹暂时访问不了，
// 这时把文件夹建出来，下次就会被当成新装（见 reallyMissing）。
func (s *Service) recordRemovalLocked(r removal) error {
	list, _, err := s.readRemovals()
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(list, func(x removal) bool { return x.Inst == r.Inst }); i >= 0 {
		if r.At.After(list[i].At) {
			list[i].At = r.At
		}
	} else {
		list = append(list, r)
	}
	b, _ := json.MarshalIndent(removalsFile{Version: 1, Removals: list}, "", "  ")
	if err := atomicfile.Write(s.removalsPath(), b, 0o644); err != nil {
		return noPath(err)
	}
	return nil
}

// settleRemovalsLocked cart.json 读成以后补做删服时没清掉的（见文件开头的规矩）。不改 s.loadErr、不报错：
// 出了问题记一句程序日志，记录留着下次再试。调用方持有 s.mu（New 里别人还拿不到 s，不用锁）。
func (s *Service) settleRemovalsLocked() {
	list, found, err := s.readRemovals()
	if err != nil {
		log.Printf("待装清单的删服记录 %s 读不出来，先留着，下次再试: %v", removalsName, err)
		return
	}
	if !found {
		return
	}
	p := s.planRemovals(list)
	if len(p.names) > 0 {
		if err := s.writeLocked(p.carts, p.locals, p.draft); err != nil {
			log.Printf("待装清单补清删掉的服务器时没存进去，删服记录先留着，下次再试: %v", err)
			return
		}
		log.Printf("待装清单读出来了，补清了删掉的服务器留下的清单：%s", strings.Join(p.names, "、"))
	}
	for _, path := range p.staged { // 登记先清，文件后删（同 ClearAll）
		_ = os.RemoveAll(path)
	}
	if err := atomicfile.RetryRemove(func() error { return os.Remove(s.removalsPath()) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("待装清单的删服记录 %s 删不掉，下次读成清单时会再补一遍（只清删服之前加的东西和记的目标）: %v", removalsName, noPath(err))
	}
}

// removalPlan 补清要做的事：换上去的清单和记号、要删的暂存文件或文件夹、补清了哪几台。
type removalPlan struct {
	carts  map[string][]Item
	locals map[string][]LocalFile
	draft  draftTarget
	staged []string // 这台的本地文件一个不剩时是它的暂存文件夹，还剩着的时候是拿掉的那几个暂存文件
	names  []string
}

// planRemovals 按删服记录算出补清以后的清单，不动内存里的（写盘成功才换上去）。调用方持有 s.mu。
func (s *Service) planRemovals(list []removal) removalPlan {
	p := removalPlan{carts: s.carts, locals: s.locals, draft: s.draft}
	for _, r := range list {
		if r.Inst == Draft || r.Inst == Toolbox {
			continue // 不是服务器的清单：坏记录不能把草稿清单、工具箱的清单清了
		}
		keepItems, dropItems := splitAddedAfter(p.carts[r.Inst], r.At, func(it Item) time.Time { return it.AddedAt })
		keepLocals, dropLocals := splitAddedAfter(p.locals[r.Inst], r.At, func(lf LocalFile) time.Time { return lf.AddedAt })
		if len(dropItems) > 0 {
			p.carts = withEntry(p.carts, r.Inst, keepItems)
		}
		if len(dropLocals) > 0 {
			p.locals = withEntry(p.locals, r.Inst, keepLocals)
		}
		if len(keepLocals) == 0 {
			p.staged = append(p.staged, s.stagedDir(r.Inst))
		} else {
			for _, lf := range dropLocals {
				p.staged = append(p.staged, s.stagedPath(r.Inst, lf.SHA1))
			}
		}
		markCleared := p.draft.Inst == r.Inst && !p.draft.At.After(r.At)
		if markCleared {
			p.draft = draftTarget{}
		}
		if len(dropItems)+len(dropLocals) > 0 || markCleared {
			p.names = append(p.names, r.Inst)
		}
	}
	return p
}

// splitAddedAfter 按加入时间分开：at 之后加的留着（keep），at 和 at 之前的拿掉（drop）。
func splitAddedAfter[T any](list []T, at time.Time, addedAt func(T) time.Time) (keep, drop []T) {
	for _, x := range list {
		if addedAt(x).After(at) {
			keep = append(keep, x)
		} else {
			drop = append(drop, x)
		}
	}
	return keep, drop
}
