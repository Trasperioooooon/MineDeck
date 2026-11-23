package inst

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/winname"
)

// ErrCreatingConflict 半成品目录当前正在创建或已是登记服务器，拒绝删除时包装此错误（操作层映射为 409）。
var ErrCreatingConflict = errors.New("creating leftover conflict")

// WriteCreatingMarker 在还没登记的新目录里写下「这次创建还没完成」。新建、导入整合包、导入现成服务端和交接包都经这里。
// 只写我们自己的记号文件，不碰世界、配置。已经登记的服务器不靠这个文件判断。
// 写之前先把 dir 所在的存放位置记进「正在建的位置」（A38）：第一次往一个新位置里建服、记号放下了还没建完程序就崩了，
// 下次启动找没建完的文件夹时也扫得到那里。不记成扫描根：建不成的话，那个位置里别的带 instance.json 的文件夹
// 不该因此进服务器列表。建成了由登记处记成扫描根（completeCreate、FinalizeCreate）。
func WriteCreatingMarker(dir, name string) error {
	app.RememberCreatingRoot(filepath.Dir(dir))
	return os.WriteFile(filepath.Join(dir, CreatingMarker), []byte(strings.TrimSpace(name)+"\n"), 0o644)
}

// leftoverRoots 找没建完的文件夹要扫的位置：扫描根（app.InstanceRoots）在前，后面接正在建的位置（app.CreatingRoots）里
// 还不在前面的，顺序固定。半成品的编号是「在这份列表里的序号/文件夹名」，列和删都经这里取，序号才对得上。
func leftoverRoots() []string {
	roots := app.InstanceRoots()
	for _, r := range app.CreatingRoots() {
		if !hasPathFold(roots, r) {
			roots = append(roots, filepath.Clean(r))
		}
	}
	return roots
}

// pruneCreatingRoots 启动时整理「正在建的位置」：已经成了扫描根的，和底下没有带创建记号的文件夹的，去掉。
// 只在启动时做，那时没有正在建的任务。位置本身现在读不到（比如在没插上的移动硬盘里）的留着，回来了照样找得到半成品。
func pruneCreatingRoots() {
	scan := app.InstanceRoots()
	var drop []string
	for _, r := range app.CreatingRoots() {
		if hasPathFold(scan, r) {
			drop = append(drop, r)
		} else if has, readable := hasCreatingChild(r); readable && !has {
			drop = append(drop, r)
		}
	}
	if len(drop) > 0 {
		app.ForgetCreatingRoots(drop...)
	}
}

// hasCreatingChild root 下面还有没有找半成品时会看的、带创建记号的文件夹（条件同 scanCreatingLeftovers）。
// root 读不出来时 readable 为 false；root 是链接或目录联接时找半成品本来就不扫它，算读得出、没有。
func hasCreatingChild(root string) (has, readable bool) {
	st, err := os.Lstat(root)
	if err != nil {
		return false, false
	}
	if !st.IsDir() || st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return false, true
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return false, false
	}
	for _, e := range ents {
		if e.IsDir() && e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 && winname.OK(e.Name()) &&
			creatingMarkerOwned(filepath.Join(root, e.Name())) {
			return true, true
		}
	}
	return false, true
}

// hasPathFold list 里有没有和 p 写的是同一个位置的（不分大小写，只按字面比）。
func hasPathFold(list []string, p string) bool {
	p = filepath.Clean(p)
	return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(filepath.Clean(x), p) })
}

// ClearCreatingMarker 登记成功以后去掉记号。文件本来就没有不算失败。
func ClearCreatingMarker(dir string) error {
	err := os.Remove(filepath.Join(dir, CreatingMarker))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CreatingLeftover 一次创建没走完、进程重启以后还留在磁盘上的目录。
// 只包括「在某个服务器根目录的直接子目录里、有创建记号、没有 instance.json（及 .bak/.tmp）」的真文件夹。
// 接管进来的目录、用户自己放的目录没有这个记号，不会出现在这里。
type CreatingLeftover struct {
	ID       string `json:"id"`   // 交给删除接口的编号，不是任意路径
	Name     string `json:"name"` // 文件夹名
	RootName string `json:"root"` // 它所在的服务器根的文件夹名，给人看
	HasWorld bool   `json:"hasWorld"`
}

// CreatingLeftovers 列出可以手动删的半成品。只读，不改盘上的东西：有 instance.json（或移出列表留下的 .bak）的不列入，
// 它里面残留的记号留到下次启动时去掉（clearStaleCreatingMarkers），或者有人点删的时候去掉（DiscardCreatingLeftover）。
func (m *Manager) CreatingLeftovers() []CreatingLeftover {
	return m.scanCreatingLeftovers(nil)
}

// clearStaleCreatingMarkers 启动时（NewManager）去掉已经登记成服务器的文件夹里残留的创建记号：登记成功以后去记号那一下失败了
// （被占着），或者程序正好崩在那中间。留着的话，以后这台的 instance.json 要是没了，它会被当成半成品列出来、能被删掉。
// 启动时还没有正在创建的任务，去掉的只会是残留的。
func (m *Manager) clearStaleCreatingMarkers() {
	m.scanCreatingLeftovers(func(dir string) {
		if err := ClearCreatingMarker(dir); err != nil {
			log.Printf("已登记的服务器文件夹里残留的创建记号去不掉，下次启动再试：%s（%v）", dir, err)
		}
	})
}

// scanCreatingLeftovers 扫各个存放位置（leftoverRoots）下带创建记号的文件夹，列出没登记的半成品。
// 已经登记成服务器的不列，交给 registered（可以是 nil）。
func (m *Manager) scanCreatingLeftovers(registered func(dir string)) []CreatingLeftover {
	var out []CreatingLeftover
	for i, root := range leftoverRoots() {
		if !plainCreatingDir(root) {
			continue
		}
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() || e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				continue
			}
			name := e.Name()
			if !winname.OK(name) {
				continue
			}
			dir := filepath.Join(root, name)
			if !creatingMarkerOwned(dir) {
				continue
			}
			isServer, checkErr := checkNoInstanceFiles(dir)
			if isServer {
				if registered != nil {
					registered(dir)
				}
				continue
			}
			if checkErr != nil {
				continue // 读不清是不是已有服务器时不列出来删
			}
			if m.creatingOrRegistered(dir) {
				continue // 正在创建，或已在内存/接管登记里
			}
			out = append(out, CreatingLeftover{
				ID:       strconv.Itoa(i) + "/" + name,
				Name:     name,
				RootName: filepath.Base(root),
				HasWorld: dirHasWorld(dir),
			})
		}
	}
	if out == nil {
		out = []CreatingLeftover{}
	}
	return out
}

// DiscardCreatingLeftover 删掉 id 对应的半成品目录。confirm 必须为 true。
// 占住目录的「创建中」锁后再查一遍记号、instance.json（含 .bak/.tmp）、符号链接与目录联接。任何一条不满足都不删。
func (m *Manager) DiscardCreatingLeftover(id string, confirm bool) error {
	if !confirm {
		return fmt.Errorf("删除没建完的文件夹需要明确确认")
	}
	dir, err := m.leftoverDir(id)
	if err != nil {
		return err
	}
	release, err := m.acquireLeftoverDelete(dir)
	if err != nil {
		return err
	}
	defer release()

	st, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("找不到这个文件夹")
		}
		return fmt.Errorf("检查这个文件夹失败：%w", err)
	}
	if !st.IsDir() || st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return fmt.Errorf("这个文件夹是指向别处的链接，没有删除")
	}
	if !creatingMarkerOwned(dir) {
		return fmt.Errorf("这个文件夹没有创建记号，没有删除")
	}
	registered, checkErr := checkNoInstanceFiles(dir)
	if registered {
		_ = ClearCreatingMarker(dir)
		return fmt.Errorf("%w：这个文件夹已经是服务器，没有删除", ErrCreatingConflict)
	}
	if checkErr != nil {
		return checkErr
	}
	if hasNestedCreatingLink(dir) {
		return fmt.Errorf("这个文件夹里面含有链接或目录联接，没有删除")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("未能删干净：%w。其他服务器和文件夹没有动，可稍后再试", err)
	}
	return nil
}

func (m *Manager) leftoverDir(id string) (string, error) {
	idxText, name, ok := strings.Cut(id, "/")
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || !winname.OK(name) {
		return "", fmt.Errorf("没有这一项")
	}
	idx, err := strconv.Atoi(idxText)
	roots := leftoverRoots()
	if err != nil || idx < 0 || idx >= len(roots) {
		return "", fmt.Errorf("没有这一项")
	}
	root := filepath.Clean(roots[idx])
	if !plainCreatingDir(root) {
		return "", fmt.Errorf("没有这一项")
	}
	clean := filepath.Clean(filepath.Join(root, name))
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("没有这一项")
	}
	return clean, nil
}

func plainCreatingDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir() && st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}

// creatingMarkerOwned 记号是普通文件，目录本身是真文件夹（不是符号链接或目录联接）。
func creatingMarkerOwned(dir string) bool {
	if !plainCreatingDir(dir) {
		return false
	}
	st, err := os.Lstat(filepath.Join(dir, CreatingMarker))
	return err == nil && st.Mode().IsRegular()
}

// checkNoInstanceFiles 确认目录里没有 instance.json、移出列表留底的 instance.json.bak 或正在落盘的 instance.json.tmp。
// 遇到权限不足、共享冲突等非「不存在」错误时返回 checkErr，绝不当成「没有配置文件」放行删除。
func checkNoInstanceFiles(dir string) (registered bool, checkErr error) {
	for _, name := range []string{"instance.json", settingsBackup} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("没法确认这个文件夹是不是已有服务器（%w），没有删除", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "instance.json.tmp")); err == nil {
		return false, fmt.Errorf("%w：这个文件夹正在写入服务器配置，暂时不能删除", ErrCreatingConflict)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("没法确认这个文件夹是不是已有服务器（%w），没有删除", err)
	}
	return false, nil
}

func hasNestedCreatingLink(root string) bool {
	found := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			found = true
			return fs.SkipAll
		}
		if p != root && d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found || err != nil
}

func (m *Manager) creatingOrRegistered(dir string) bool {
	real := realPath(dir)
	key := dirKeyPrefix + strings.ToLower(real)
	m.mu.Lock()
	if m.creating[key] {
		m.mu.Unlock()
		return true
	}
	for _, inst := range m.insts {
		ir := realPath(inst.Dir)
		if sameDir(real, ir) || within(real, ir) || within(ir, real) {
			m.mu.Unlock()
			return true
		}
	}
	m.mu.Unlock()
	for _, d := range app.InstanceDirs() {
		dr := realPath(d)
		if sameDir(real, dr) || within(real, dr) || within(dr, real) {
			return true
		}
	}
	return false
}

func (m *Manager) acquireLeftoverDelete(dir string) (func(), error) {
	real := realPath(dir)
	key := dirKeyPrefix + strings.ToLower(real)
	m.mu.Lock()
	if m.creating[key] {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w：这个文件夹正在创建服务器，暂时不能删除", ErrCreatingConflict)
	}
	for _, inst := range m.insts {
		ir := realPath(inst.Dir)
		if sameDir(real, ir) || within(real, ir) || within(ir, real) {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w：这个文件夹已经是列表里的服务器，没有删除", ErrCreatingConflict)
		}
	}
	if m.creating == nil {
		m.creating = map[string]bool{}
	}
	m.creating[key] = true
	m.mu.Unlock()

	for _, d := range app.InstanceDirs() {
		dr := realPath(d)
		if sameDir(real, dr) || within(real, dr) || within(dr, real) {
			m.releaseCreating(key)
			return nil, fmt.Errorf("%w：这个文件夹已经是接管登记的服务器，没有删除", ErrCreatingConflict)
		}
	}
	return func() { m.releaseCreating(key) }, nil
}

func dirHasWorld(dir string) bool {
	names := []string{"world", "world_nether", "world_the_end"}
	if lv := LevelName(dir); lv != "" && lv != "world" {
		names = append(names, lv, lv+"_nether", lv+"_the_end")
	}
	for _, name := range names {
		if st, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name), "level.dat")); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}
