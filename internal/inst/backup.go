package inst

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/events"
)

type BackupInfo struct {
	File   string  `json:"file"`
	SizeMB float64 `json:"sizeMb"`
	Time   string  `json:"time"`
}

// backupDirOf 返回实例在当前备份根下的备份目录（新备份只写这里）。filepath.Base 兜底：即便某个 instance.json
// 携带带路径分隔符的异常 name（多根扫描可能拾取到），也不会让备份读写逃出备份根。
func backupDirOf(name string) string { return filepath.Join(app.BackupsRoot(), filepath.Base(name)) }

// backupDirsOf 返回实例在全部已知备份根（app.AllBackupRoots，当前根在前）下的备份目录：改过备份目录或
// 恢复默认后，旧根里的备份照常列出、还原、删除，并计入保留份数。同样只取 filepath.Base(name)。
func backupDirsOf(name string) []string {
	roots := app.AllBackupRoots()
	dirs := make([]string, len(roots))
	for i, r := range roots {
		dirs[i] = filepath.Join(r, filepath.Base(name))
	}
	return dirs
}

// backupCopies 返回实例各备份目录里名为 file 的备份的完整路径（当前根在前）。防越界：file 只取 filepath.Base
// （去掉目录与盘符），须为 .zip 且 filepath.IsLocal（拒 NTFS 流名、设备名），目录又只来自已知备份根，客户端给的名字
// 因此落不到这些目录之外。列表按文件名去重、同名即同一份备份：还原取第一个，删除与滚动清理全删。
func backupCopies(name, file string) ([]string, error) {
	f := filepath.Base(file)
	if !strings.HasSuffix(f, ".zip") || !filepath.IsLocal(f) {
		return nil, fmt.Errorf("非法备份文件名")
	}
	var out []string
	for _, dir := range backupDirsOf(name) {
		p := filepath.Join(dir, f)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("备份文件不存在: %s", file)
	}
	return out, nil
}

// worldDirs 返回实例的世界存档目录（vanilla 单目录 / bukkit 三目录）。
// level-name 指到实例目录外面、或者指回实例目录自己的不认，按 world 算：备份会把这些目录打包，
// 还原之前还会把它们删掉，而 server.properties 可以是别人给的（交接包、整合包的覆盖目录），也可以是手改的。
// 原来照单全收，level-name=../别的服务器 再点一次「还原备份」，删掉的就是隔壁那台服务器。
func (i *Instance) worldDirs() []string {
	level := LevelName(i.Dir)
	var out []string
	for _, d := range []string{level, level + "_nether", level + "_the_end"} {
		full := filepath.Join(i.Dir, d)
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// ListBackups 列出实例在全部已知备份根下的备份（新在前）；同名文件只列一份（当前根优先）。
func (m *Manager) ListBackups(name string) ([]BackupInfo, error) {
	if _, err := m.Get(name); err != nil {
		return nil, err
	}
	out := []BackupInfo{} // 显式非 nil：空目录时避免 JSON 编成 null 致前端备份页 .map 崩溃
	seen := map[string]bool{}
	for k, dir := range backupDirsOf(name) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if k == 0 && !os.IsNotExist(err) {
				return nil, err // 当前根读不了照旧报错；旧根可能已删或在拔掉的盘上，跳过
			}
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") || seen[e.Name()] {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			seen[e.Name()] = true
			out = append(out, BackupInfo{
				File:   e.Name(),
				SizeMB: float64(info.Size()) / (1 << 20),
				Time:   info.ModTime().Format("2006-01-02 15:04:05"),
			})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].File > out[b].File })
	return out, nil
}

// CreateBackup 备份世界存档；运行中执行热备（save-off → save-all → 压缩 → save-on）。
func (m *Manager) CreateBackup(name, label string) (string, error) {
	return m.createBackup(name, label, "")
}

// createBackup 同 CreateBackup；spare 非空时滚动清理放过这份备份。还原前的自动备份靠它保住待还原的源备份——
// 份数已满且要还原的正是最旧那份时，源备份本会在解压前被清理掉。
func (m *Manager) createBackup(name, label, spare string) (string, error) {
	i, err := m.Get(name)
	if err != nil {
		return "", err
	}
	worlds := i.worldDirs()
	if len(worlds) == 0 {
		return "", fmt.Errorf("还没有世界存档。服务器可能还没启动过。")
	}
	if i.Status() == "running" {
		i.Console.Append("[MineDeck] 开始热备份：暂停自动保存...")
		_ = m.Command(name, "save-off")
		_ = m.Command(name, "save-all flush")
		i.waitConsole("Saved the game", 15*time.Second)
		defer func() {
			_ = m.Command(name, "save-on")
			i.Console.Append("[MineDeck] 热备份完成，已恢复自动保存")
		}()
	}
	fname := time.Now().Format("20060102-150405")
	if label != "" {
		fname += "-" + sanitizeLabel(label)
	}
	fname += ".zip"
	dest := filepath.Join(backupDirOf(name), fname)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := zipDirs(i.Dir, worlds, dest); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("压缩备份失败: %w", err)
	}
	m.pruneBackups(name, spare)
	events.Publish("backup.done", map[string]any{"instance": name, "file": fname})
	return fname, nil
}

// RestoreBackup 还原备份（仅停止状态；还原前自动再备份一次当前世界）。
func (m *Manager) RestoreBackup(name, file string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if i.Status() != "stopped" {
		return fmt.Errorf("请先停止服务器再还原备份")
	}
	copies, err := backupCopies(name, file)
	if err != nil {
		return err
	}
	src := copies[0]
	if len(i.worldDirs()) > 0 {
		if _, err := m.createBackup(name, "还原前自动", filepath.Base(src)); err != nil {
			return fmt.Errorf("还原前自动备份失败，已中止: %w", err)
		}
	}
	for _, d := range i.worldDirs() {
		if err := os.RemoveAll(filepath.Join(i.Dir, d)); err != nil {
			return fmt.Errorf("清理旧世界失败: %w", err)
		}
	}
	if err := unzipTo(src, i.Dir); err != nil {
		return fmt.Errorf("解压备份失败: %w", err)
	}
	i.Console.Append("[MineDeck] 世界已还原至备份 " + filepath.Base(file))
	return nil
}

// DeleteBackup 删除一份备份（各备份根里的同名文件一并删，免得删完又从旧根冒出来）。
func (m *Manager) DeleteBackup(name, file string) error {
	if _, err := m.Get(name); err != nil {
		return err
	}
	copies, err := backupCopies(name, file)
	if err != nil {
		return err
	}
	for _, p := range copies {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) { // 两个根可能是同一目录的不同写法
			return err
		}
	}
	return nil
}

// pruneBackups 按保留份数滚动清理：与列表一致，跨全部已知备份根计数，超出的最旧备份不论在哪个根都删。
func (m *Manager) pruneBackups(name, spare string) {
	list, err := m.ListBackups(name)
	if err != nil {
		return
	}
	for i := app.BackupKeep(); i < len(list); i++ {
		// 自动清理最旧的（保留手动标签“还原前自动”同样计入总数）；spare 见 createBackup
		if list[i].File == spare {
			continue
		}
		copies, _ := backupCopies(name, list[i].File)
		for _, p := range copies {
			_ = os.Remove(p)
		}
	}
}

func sanitizeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>| `, r) {
			return '_'
		}
		return r
	}, s)
	if len([]rune(s)) > 20 {
		s = string([]rune(s)[:20])
	}
	return s
}

// waitConsole 等待控制台出现指定子串（热备时等待 "Saved the game"）。
func (i *Instance) waitConsole(substr string, timeout time.Duration) bool {
	_, ch, cancel := i.Console.Subscribe()
	defer cancel()
	deadline := time.After(timeout)
	for {
		select {
		case line := <-ch:
			if strings.Contains(line, substr) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// zipDirs 将 baseDir 下的若干子目录打包为 zip（跳过 session.lock）。
func zipDirs(baseDir string, dirs []string, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	var werr error
	for _, d := range dirs {
		root := filepath.Join(baseDir, d)
		werr = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || info.Name() == "session.lock" {
				return nil
			}
			rel, err := filepath.Rel(baseDir, p)
			if err != nil {
				return err
			}
			w, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			r, err := os.Open(p)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(w, r)
			r.Close()
			return cerr
		})
		if werr != nil {
			break
		}
	}
	if cerr := zw.Close(); werr == nil {
		werr = cerr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// unzipTo 解压 zip 至目录（zip-slip 防护）。
func unzipTo(src, destDir string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	cleanDest := filepath.Clean(destDir)
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		target := filepath.Join(cleanDest, filepath.FromSlash(name))
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("备份内含非法路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(w, rc)
		rc.Close()
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
