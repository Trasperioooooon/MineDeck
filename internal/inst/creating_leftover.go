package inst

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minedeck/internal/app"
)

// WriteCreatingMarker 在还没登记的新目录里写下「这次创建还没完成」。
// 只写我们自己的记号文件，不碰世界、配置。已经登记的服务器不靠这个文件判断。
func WriteCreatingMarker(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, CreatingMarker), []byte(strings.TrimSpace(name)+"\n"), 0o644)
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
// 只包括「在某个服务器根目录的直接子目录里、有创建记号、没有 instance.json」的。
// 接管进来的目录、用户自己放的目录没有这个记号，不会出现在这里。
type CreatingLeftover struct {
	ID       string `json:"id"`   // 交给删除接口的编号，不是任意路径
	Name     string `json:"name"` // 文件夹名
	RootName string `json:"root"` // 它所在的服务器根的文件夹名，给人看
	HasWorld bool   `json:"hasWorld"`
}

// CreatingLeftovers 列出可以手动删的半成品。有 instance.json 的只去掉记号，不列入、不删目录。
func (m *Manager) CreatingLeftovers() []CreatingLeftover {
	var out []CreatingLeftover
	for i, root := range app.InstanceRoots() {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if !creatingMarkerOwned(dir) {
				continue
			}
			if _, err := os.Lstat(filepath.Join(dir, "instance.json")); err == nil {
				_ = ClearCreatingMarker(dir)
				continue
			}
			if m.creatingBusy(dir) {
				continue // 这次创建还在进行，不是上次留下的
			}
			out = append(out, CreatingLeftover{
				ID:       strconv.Itoa(i) + "/" + e.Name(),
				Name:     e.Name(),
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
// 再查一遍记号、instance.json、是不是链接、是不是正在创建。任何一条不满足都不删。
func (m *Manager) DiscardCreatingLeftover(id string, confirm bool) error {
	if !confirm {
		return fmt.Errorf("删除没建完的文件夹需要明确确认")
	}
	dir, err := m.leftoverDir(id)
	if err != nil {
		return err
	}
	if m.creatingBusy(dir) {
		return fmt.Errorf("这个文件夹正在被一次创建使用，没有删除")
	}
	if !creatingMarkerOwned(dir) {
		return fmt.Errorf("这个文件夹没有创建记号，没有删除")
	}
	if _, err := os.Lstat(filepath.Join(dir, "instance.json")); err == nil {
		_ = ClearCreatingMarker(dir)
		return fmt.Errorf("这个文件夹已经是登记过的服务器，没有删除")
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("找不到这个文件夹")
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return fmt.Errorf("这个文件夹是链接，没有删除")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("没能删干净：%w。服务器和别的文件夹没有动，可以再试一次", err)
	}
	return nil
}

func (m *Manager) leftoverDir(id string) (string, error) {
	idxText, name, ok := strings.Cut(id, "/")
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("没有这一项")
	}
	idx, err := strconv.Atoi(idxText)
	roots := app.InstanceRoots()
	if err != nil || idx < 0 || idx >= len(roots) {
		return "", fmt.Errorf("没有这一项")
	}
	dir := filepath.Join(roots[idx], name)
	root := filepath.Clean(roots[idx])
	clean := filepath.Clean(dir)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("没有这一项")
	}
	return clean, nil
}

// creatingMarkerOwned 记号是普通文件，目录本身不是链接。
func creatingMarkerOwned(dir string) bool {
	dst, err := os.Lstat(dir)
	if err != nil || dst.Mode()&os.ModeSymlink != 0 || !dst.IsDir() {
		return false
	}
	st, err := os.Lstat(filepath.Join(dir, CreatingMarker))
	return err == nil && st.Mode().IsRegular()
}

func (m *Manager) creatingBusy(dir string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creating[dirKey(dir)]
}

func dirHasWorld(dir string) bool {
	for _, name := range []string{"world", "world_nether", "world_the_end"} {
		if st, err := os.Lstat(filepath.Join(dir, name, "level.dat")); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}
