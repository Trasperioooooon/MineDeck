package inst

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/diskguard"
	"minedeck/internal/events"
	"minedeck/internal/zipguard"
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

// worldDirsStrict 同 worldDirs，但读不了 server.properties、看不了某个世界目录在不在时返回错误，不当成「没有」。
// 备份和还原用它：还原会按这份清单删旧世界再解压，清单错了删掉的就不是该删的，没删掉的又会和备份里的混在一起。
func (i *Instance) worldDirsStrict() ([]string, error) {
	level, err := LevelNameStrict(i.Dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range []string{level, level + "_nether", level + "_the_end"} {
		st, err := os.Stat(filepath.Join(i.Dir, d))
		switch {
		case err == nil:
			if st.IsDir() {
				out = append(out, d)
			}
		case errors.Is(err, fs.ErrNotExist):
		default:
			return nil, fmt.Errorf("看不了世界文件夹「%s」在不在：%w", d, err)
		}
	}
	return out, nil
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
	// 换地图到一半时世界目录正在被移来移去，打出来的备份是半截的（定时备份也走这里）。
	// 不只是开头看一眼：备份的这段时间一直登记着，换地图要等它做完才占得上位。
	// 还原备份是自己占着位再调下面的 createBackup，所以登记放在这里，不放在 createBackup 里
	i, err := m.Get(name)
	if err != nil {
		return "", err
	}
	// 先排队拿这个盘的写入名额，拿到了再登记「正在备份」。反过来的话，备份排在一次地图暂存后面等名额时
	// 已经登记着：暂存一结束备份开始，导入走到提交那一步看到有人在备份就放弃，那几个 GB 白复制了
	// （2026-11-14 第三轮修复的复审，发现 2）。等名额的时候没有登记，这期间换地图占上了位，
	// 下面的登记会失败、备份报「正在换地图」，和一直以来的先到先得一样
	lease, err := backupLease(name)
	if err != nil {
		return "", err
	}
	defer lease.Close()
	release, err := m.UseWorldInstance(i, "备份")
	if err != nil {
		return "", err
	}
	defer release()
	return m.createBackupWith(i, label, "", lease)
}

// backupLease 排队拿备份文件夹所在的盘的写入名额（压缩是本机上的大批量写盘，和地图暂存、解包一个一个来）。
// 查不到余量照旧备份，和加这道检查之前一样：有的盘就是查不了，不能因此连备份都做不了；查得到又不够时照拦。
func backupLease(name string) (*diskguard.Lease, error) {
	dir := backupDirOf(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return diskguard.Acquire(context.Background(), dir, diskguard.Options{AllowUnknown: true})
}

// saveConfirmWait 热备时发出 save-all flush 以后，等服务端说保存完了等多久（测试里改短）。
var saveConfirmWait = 15 * time.Second

// savedLine 这一行是不是服务端保存完成的确认。1.13 起是「Saved the game」，更早的版本（最低支持的 1.12.2）是「Saved the world」。
func savedLine(line string) bool {
	return strings.Contains(line, "Saved the game") || strings.Contains(line, "Saved the world")
}

// backupHook 测试用：世界目录开始压缩之前调用。
var backupHook func()

// createBackup 同 CreateBackup；spare 非空时滚动清理放过这份备份。还原前的自动备份靠它保住待还原的源备份——
// 份数已满且要还原的正是最旧那份时，源备份本会在解压前被清理掉。
func (m *Manager) createBackup(name, label, spare string) (string, error) {
	i, err := m.Get(name)
	if err != nil {
		return "", err
	}
	lease, err := backupLease(name)
	if err != nil {
		return "", err
	}
	defer lease.Close()
	return m.createBackupWith(i, label, spare, lease)
}

// createBackupWith 调用方已经拿到备份文件夹所在的盘的写入名额（lease），并且已经登记或者占着位。
func (m *Manager) createBackupWith(i *Instance, label, spare string, lease *diskguard.Lease) (string, error) {
	name := i.Name
	i.backupMu.Lock()
	defer i.backupMu.Unlock()
	worlds, err := i.worldDirsStrict()
	if err != nil {
		return "", err
	}
	if len(worlds) == 0 {
		return "", fmt.Errorf("还没有世界存档。服务器可能还没启动过。")
	}
	dir := backupDirOf(name)
	// 名额是排队结束以后才到手的，到这里才暂停存档，避免另一项写盘让服务器长时间停留在 save-off。
	i.procMu.Lock()
	p, gen, state := i.proc, i.runGen, i.state
	i.procMu.Unlock()
	if state != "" && state != "stopped" && state != "running" {
		return "", fmt.Errorf("服务器正在启动或停止，请稍后再备份")
	}
	if state == "running" {
		if p == nil {
			return "", fmt.Errorf("服务器进程状态已变化，请重试备份")
		}
		i.Console.Append("[MineDeck] 开始热备份：暂停自动保存...")
		_, lines, unsubscribe := i.Console.Subscribe()
		defer unsubscribe()
		if err := writeLine(p, "save-off"); err != nil {
			return "", err
		}
		defer func() {
			i.procMu.Lock()
			same := i.proc == p && i.runGen == gen
			i.procMu.Unlock()
			if same && writeLine(p, "save-on") == nil {
				i.Console.Append("[MineDeck] 热备份操作结束，已恢复自动保存")
			}
		}()
		if err := writeLine(p, "save-all flush"); err != nil {
			return "", err
		}
		timer := time.NewTimer(saveConfirmWait)
		defer timer.Stop()
		flushed := false
		for !flushed {
			select {
			case line, ok := <-lines:
				if !ok {
					return "", fmt.Errorf("没有收到服务器保存完成的确认，这次备份已作废")
				}
				flushed = savedLine(line)
			case <-timer.C:
				// 等不到确认不作废，照常备份，和一直以来一样：区块多的大服、慢的盘保存一次就可能超过这个时间，
				// 上一版在这里直接返回错误，这样的服务器只要开着，手动备份和定时备份就一份都拿不到
				// （2026-11-14 第三轮修复的复审，发现 3）。服务器中途重启的那种作废在下面的 verify 里，不受影响
				i.Console.Append("[MineDeck] 没有等到服务器保存完成的确认，照常备份（最后这一小段时间的改动可能没有进这份备份）")
				flushed = true
			}
		}
	}
	base := time.Now().Format("20060102-150405.000000000")
	if label != "" {
		base += "-" + sanitizeLabel(label)
	}
	fname := base + ".zip"
	dest := filepath.Join(dir, fname)
	if backupHook != nil {
		backupHook()
	}
	// 在发布压缩包之前再核对进程代际；热备中重启不能把跨两次运行的内容当成完整备份。
	verify := func() error {
		i.procMu.Lock()
		defer i.procMu.Unlock()
		if i.proc != p || i.runGen != gen {
			return fmt.Errorf("备份期间服务器停止或重新启动，这次备份已作废，请重新备份")
		}
		return nil
	}
	if err := zipDirsWith(i.Dir, worlds, dest, lease, verify); err != nil {
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
	if why := m.WorldBusy(name); why != "" {
		return errors.New(why)
	}
	// 还原要先删旧世界再解压：这期间占上位，不让别人启动服务器、也不让换地图同时进行
	release, err := m.BeginWorldChangeInstance(i, "还原备份")
	if err != nil {
		return err
	}
	defer release()
	copies, err := backupCopies(name, file)
	if err != nil {
		return err
	}
	src := copies[0]
	// 先按「看不了就停下」的读法定下现在的世界目录：这份清单决定下面删哪些文件夹
	worlds, err := i.worldDirsStrict()
	if err != nil {
		return fmt.Errorf("没有还原，什么都没动：%w", err)
	}
	if len(worlds) > 0 {
		if _, err := m.createBackup(name, "还原前自动", filepath.Base(src)); err != nil {
			return fmt.Errorf("还原前自动备份失败，已中止: %w", err)
		}
	}
	// 预检和空间租约都必须先于删旧世界，拒绝损坏或过大的包时旧世界仍在。
	zr, err := zipguard.Open(src, zipguard.Default)
	if err != nil {
		return fmt.Errorf("备份打不开，没有还原：%w", err)
	}
	defer zr.Close()
	if err := checkBackupPaths(zr.File, i.Dir); err != nil {
		return err
	}
	lease, err := diskguard.Acquire(context.Background(), i.Dir, diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	for _, d := range worlds {
		if err := os.RemoveAll(filepath.Join(i.Dir, d)); err != nil {
			return fmt.Errorf("清理旧世界失败: %w", err)
		}
	}
	if err := unzipReaderTo(zr.Reader, i.Dir, lease); err != nil {
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
	lease, err := diskguard.Acquire(context.Background(), filepath.Dir(dest), diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	return zipDirsWith(baseDir, dirs, dest, lease, nil)
}

func zipDirsWith(baseDir string, dirs []string, dest string, lease *diskguard.Lease, verify func() error) error {
	f, err := os.CreateTemp(filepath.Dir(dest), ".backup-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	zw := zip.NewWriter(lease.Writer(f))
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
	if werr != nil {
		return werr
	}
	if verify != nil {
		if err := verify(); err != nil {
			return err
		}
	}
	return atomicfile.Replace(tmp, dest)
}

// unzipTo 解压 zip 至目录（zip-slip 防护）。
func unzipTo(src, destDir string) error {
	zr, err := zipguard.Open(src, zipguard.Default)
	if err != nil {
		return err
	}
	defer zr.Close()
	lease, err := diskguard.Acquire(context.Background(), destDir, diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	if err := checkBackupPaths(zr.File, destDir); err != nil {
		return err
	}
	return unzipReaderTo(zr.Reader, destDir, lease)
}

func checkBackupPaths(files []*zip.File, destDir string) error {
	cleanDest := filepath.Clean(destDir)
	for _, f := range files {
		target := filepath.Join(cleanDest, filepath.FromSlash(strings.ReplaceAll(f.Name, "\\", "/")))
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("备份内含非法路径: %s", f.Name)
		}
	}
	return nil
}

func unzipReaderTo(zr *zip.Reader, destDir string, lease *diskguard.Lease) error {
	cleanDest := filepath.Clean(destDir)
	for _, f := range zr.File {
		if err := lease.Check(0); err != nil {
			return err
		}
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
		_, cerr := io.Copy(lease.Writer(w), rc)
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
