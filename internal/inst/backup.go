package inst

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/diskguard"
	"minedeck/internal/errhint"
	"minedeck/internal/events"
	"minedeck/internal/linkmode"
	"minedeck/internal/zipguard"
)

type BackupInfo struct {
	File   string  `json:"file"`
	SizeMB float64 `json:"sizeMb"`
	Time   string  `json:"time"`
	// Label 做备份时填的标签（文件名里时间后面的那段，如「更新模组前」「还原前自动」）；没填的没有这一项。
	// 界面上列表优先显示它（可用性走查 R11）
	Label string `json:"label,omitempty"`
	// Dir 这份备份所在的文件夹（某个备份根下面这台服务器的子文件夹）。
	Dir string `json:"dir"`
	// Elsewhere 这份备份不在现在的备份存放文件夹里：是改存放位置以前做的，还留在原来的文件夹（照常列出、能还原能删）。
	// 界面据此提醒「还在原来的文件夹」，不然「备份放在」写的是新文件夹、列出来的却是别处的，点「打开」看到的是空文件夹（回访走查 11-16）
	Elsewhere bool `json:"elsewhere,omitempty"`
}

// backupNameRe 备份文件名：时间（20060102-150405，后面可能带 .纳秒）加可选的「-标签」。
var backupNameRe = regexp.MustCompile(`^(\d{8}-\d{6})(?:\.\d+)?(?:-(.+))?\.zip$`)

// parseBackupName 从备份文件名里拆出做备份的时间和标签；不是 MineDeck 起的名字（自己放进来的 zip）时 ok 为 false。
func parseBackupName(file string) (t time.Time, label string, ok bool) {
	m := backupNameRe.FindStringSubmatch(file)
	if m == nil {
		return time.Time{}, "", false
	}
	t, err := time.ParseInLocation("20060102-150405", m[1], time.Local)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, m[2], true
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
	_, out, err := i.levelAndWorldDirsStrict()
	return out, err
}

// levelAndWorldDirsStrict 同 worldDirsStrict，另外返回世界名（level-name）：还原要照它把备份里的世界放回去，
// 现在一个世界文件夹都还没有时也要知道放到哪。
func (i *Instance) levelAndWorldDirsStrict() (string, []string, error) {
	level, err := LevelNameStrict(i.Dir)
	if err != nil {
		return "", nil, err
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
			return "", nil, fmt.Errorf("看不了世界文件夹「%s」在不在：%w", d, err)
		}
	}
	return level, out, nil
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
			_, label, _ := parseBackupName(e.Name())
			out = append(out, BackupInfo{
				File:      e.Name(),
				SizeMB:    float64(info.Size()) / (1 << 20),
				Time:      info.ModTime().Format("2006-01-02 15:04:05"),
				Label:     label,
				Dir:       dir,
				Elsewhere: k > 0,
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

// backupNow 给备份文件起名用的时钟（测试里换成不走的）；backupNameTries 名字撞上已有的备份时最多等几毫秒。
var (
	backupNow       = time.Now
	backupNameTries = 500
)

// errBackupUnsettled 这次备份没做成，是因为服务器正在启动、停止，或者备份期间停止、重启过。
// 等它稳定下来再备份就行：定时备份据此等一等再试（见 execSchedule），手动备份照原样把这句话报给用户。
type errBackupUnsettled struct{ msg string }

func (e *errBackupUnsettled) Error() string { return e.msg }

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
		return "", fmt.Errorf("还没有世界。服务器可能还没启动过。")
	}
	dir := backupDirOf(name)
	// 名额是排队结束以后才到手的，到这里才暂停存档，避免另一项写盘让服务器长时间停留在 save-off。
	i.procMu.Lock()
	p, gen, state := i.proc, i.runGen, i.state
	i.procMu.Unlock()
	if state != "" && state != "stopped" && state != "running" {
		return "", &errBackupUnsettled{"服务器正在启动或停止，请稍后再备份"}
	}
	if state == "running" {
		if p == nil {
			return "", &errBackupUnsettled{"服务器进程状态已变化，请重试备份"}
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
					return "", &errBackupUnsettled{"没有收到服务器保存完成的确认，这次备份已作废"}
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
	// 文件名里的第一个候选时间在压缩开始前取，避免大世界压了几分钟后文件名时间比开始备份晚一大截；
	// 发布时若这个名字已被占住，再在 zipDirsExclusive 里按当时的时钟重取。
	startedAt := backupNow()
	if backupHook != nil {
		backupHook()
	}
	// 在发布压缩包之前再核对进程代际；热备中重启不能把跨两次运行的内容当成完整备份。
	// 状态也要和开始时一样：热备压到一半有人点了停止，进程还没退出（代际没变）压缩就结束了的那种，
	// 后半截是服务器一边停服存盘一边压的，同样不算完整。
	verify := func() error {
		i.procMu.Lock()
		defer i.procMu.Unlock()
		// 空串和 stopped 是一回事（Status 也这么算）
		same := i.state == state || ((i.state == "" || i.state == "stopped") && (state == "" || state == "stopped"))
		if i.proc != p || i.runGen != gen || !same {
			return &errBackupUnsettled{"备份期间服务器停止或重新启动，这次备份已作废，请重新备份"}
		}
		return nil
	}
	fname, err := zipDirsExclusive(i.Dir, worlds, dir, label, startedAt, lease, verify)
	if err != nil {
		// 压到一半读文件出错，多半是服务器这时正在停服或重启：存盘时会写临时文件再改名，列出来的文件转眼就不在了。
		// 这种也按「服务器停止或重启过」报，定时备份会等它稳定下来再试；核对通过的才是真的读不了
		if verr := verify(); verr != nil {
			return "", verr
		}
		var dup *errBackupNameDup
		if errors.As(err, &dup) {
			return "", dup
		}
		return "", fmt.Errorf("压缩备份失败: %w", err)
	}
	m.pruneBackups(name, spare)
	events.Publish("backup.done", map[string]any{"instance": name, "file": fname})
	return fname, nil
}

// RestoreBackup 还原备份（仅停止状态；还原前自动再备份一次当前世界）。
//
// 只换世界：从备份里认出世界的那几个文件夹（见 planWorldRestore），放进现在的世界名下面；包里别的文件
// （server.properties、ops.json、模组、配置……）一概不解。MineDeck 自己做的备份里本来只有世界，可备份列表
// 认得任何放进备份文件夹的 zip（自己把整个服务器文件夹打的包、别的工具做的备份），以前这里把整个包解进服务器文件夹，
// 这种包一还原，正版验证、视距这些设置就回到了打包那时候。2026-11-27 有用户反馈还原以后设置被重置，改成只解世界。
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
	// 先按「看不了就停下」的读法定下现在的世界名和世界目录：目录清单决定下面删哪些文件夹，世界名决定备份里的世界放到哪
	level, worlds, err := i.levelAndWorldDirsStrict()
	if err != nil {
		return fmt.Errorf("没有还原，什么都没动：%w", err)
	}
	// 打开备份、认出里面的世界、查落点，都放在还原前自动备份之前：包坏了、认不出世界的，什么都不动就停下，
	// 也不多做一份「还原前自动」（份数满了时多这一份会挤掉一份最旧的备份）。
	zr, err := zipguard.Open(src, zipguard.Default)
	if err != nil {
		wrapErr := fmt.Errorf("备份打不开，没有还原：%w", err)
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, zipguard.ErrTooLarge) {
			return errhint.Wrap(wrapErr, "备份压缩包打不开或已损坏，请检查备份文件是否完整")
		}
		return wrapErr
	}
	defer zr.Close()
	plan, err := planWorldRestore(zr.File, level)
	if err != nil {
		return err
	}
	if err := checkEntryPaths(plan.entries, i.Dir); err != nil {
		return err
	}
	if len(worlds) > 0 {
		if _, err := m.createBackup(name, "还原前自动", filepath.Base(src)); err != nil {
			return fmt.Errorf("还原前自动备份失败，已中止: %w", err)
		}
	}
	// 空间租约必须先于删旧世界，拒绝过大的包时旧世界仍在；要排在还原前自动备份后面：两边多半在同一个盘上，
	// 先拿着这一份再去做备份，备份就排在自己后面等不到名额了。
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
	if err := unzipEntriesTo(plan.entries, i.Dir, lease); err != nil {
		wrapErr := fmt.Errorf("解压备份失败: %w", err)
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errhint.Wrap(wrapErr, "这份备份可能损坏或没写完整，请检查备份文件")
		}
		return wrapErr
	}
	i.Console.Append("[MineDeck] 世界已还原至备份 " + filepath.Base(file))
	if plan.from != level {
		i.Console.Append(fmt.Sprintf("[MineDeck] 备份里的世界叫「%s」，照现在的世界名放进了「%s」（server.properties 没改）", plan.from, level))
	}
	if len(plan.skipped) > 0 {
		i.Console.Append(fmt.Sprintf("[MineDeck] 备份里另有 %d 个不是世界的文件（%s），没有还原，服务器设置保持原样", len(plan.skipped), skippedSample(plan.skipped)))
	}
	// 记下现在的世界是从哪份备份来的（R11）：详情页「世界与备份」页顶照着写，重开程序后仍在
	wf := &WorldFrom{Backup: filepath.Base(src), At: time.Now().Format("2006-01-02 15:04:05")}
	if t, label, ok := parseBackupName(wf.Backup); ok {
		wf.BackupTime, wf.Label = t.Format("2006-01-02 15:04:05"), label
	} else if st, err := os.Stat(src); err == nil {
		wf.BackupTime = st.ModTime().Format("2006-01-02 15:04:05")
	}
	m.setWorldFrom(i, wf)
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

type errBackupNameDup struct{ fname string }

func (e *errBackupNameDup) Error() string {
	return fmt.Sprintf("备份文件的名字和已有的一份撞上了（%s），这次没有备份，请再试一次", e.fname)
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

func formatBackupName(t time.Time, label string) string {
	base := t.Format("20060102-150405.000000000")
	if label != "" {
		base += "-" + sanitizeLabel(label)
	}
	return base + ".zip"
}

// zipDirsExclusive 先把世界压缩进临时文件并过完 verify，再用 atomicfile.PublishNew 只在目标不存在时一步发布上去：
// 不在备份文件夹里预先建 0 字节占位 .zip（避免 Windows 上杀毒或资源管理器打开占位导致替换拒绝访问），
// 同一进程同跳时钟、预先存在的同名文件，或跨进程在压缩期间同时备份同一台服时，都不会盖掉已有的备份。
func zipDirsExclusive(baseDir string, dirs []string, outDir, label string, startedAt time.Time, lease *diskguard.Lease, verify func() error) (string, error) {
	tmp, err := writeBackupTemp(baseDir, dirs, outDir, lease, verify)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	for tries := 0; ; tries++ {
		ts := startedAt
		if tries > 0 {
			ts = backupNow()
		}
		fname := formatBackupName(ts, label)
		dest := filepath.Join(outDir, fname)
		err := atomicfile.PublishNew(tmp, dest)
		if err == nil {
			return fname, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if tries >= backupNameTries {
			return "", &errBackupNameDup{fname: fname}
		}
		time.Sleep(time.Millisecond)
	}
}

func zipDirsWith(baseDir string, dirs []string, dest string, lease *diskguard.Lease, verify func() error) error {
	tmp, err := writeBackupTemp(baseDir, dirs, filepath.Dir(dest), lease, verify)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	return atomicfile.Replace(tmp, dest)
}

func writeBackupTemp(baseDir string, dirs []string, outDir string, lease *diskguard.Lease, verify func() error) (string, error) {
	f, err := os.CreateTemp(outDir, ".minedeck-backup-*.tmp")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
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
		return "", werr
	}
	if verify != nil {
		if err := verify(); err != nil {
			return "", err
		}
	}
	ok = true
	return tmp, nil
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

// restoreEntry 备份里要解出来的一项，以及它落到服务器文件夹里的哪儿（相对路径，斜杠分隔）。
// 整包解压时 name 就是条目自己的名字；还原备份时是换成现在的世界名以后的路径，见 planWorldRestore。
type restoreEntry struct {
	f    *zip.File
	name string
}

func allEntries(files []*zip.File) []restoreEntry {
	out := make([]restoreEntry, len(files))
	for k, f := range files {
		out[k] = restoreEntry{f: f, name: f.Name}
	}
	return out
}

// worldRestorePlan 还原备份时从包里解哪些、放到哪：from 是备份里世界的名字（打包那时候的 level-name），
// entries 是这个世界的三个文件夹（from、from_nether、from_the_end）里的文件，已经换成现在的世界名下面的路径；
// skipped 是包里不是世界、不还原的文件（只用来在控制台说一声）。
type worldRestorePlan struct {
	from    string
	entries []restoreEntry
	skipped []string
}

// worldSuffixes 一个世界的三个文件夹：Paper、Spigot 把下界和末地放在世界名后面加这两个后缀的文件夹里，原版放在世界里面。
var worldSuffixes = []string{"", "_nether", "_the_end"}

// planWorldRestore 从备份里认出世界、算出放到哪（level 是现在的世界名）。只读不写，认不出就报错，调用方这时还什么都没动。
//
// 世界在包里叫什么，依次看：
//  1. 包里有现在这个世界名、带着 level.dat 的：备份时和现在世界名一样，最常见；
//  2. 包里根上带着 server.properties 的（自己打的整包、别的工具做的备份）：只读出它的 level-name（读法同 LevelNameStrict），
//     那个世界在包里就用它——不写回去，现在的设置不动；
//  3. 都不是：按带 level.dat 的文件夹认（Paper 的 world_nether、world_the_end 里也有 level.dat，算 world 的一部分；
//     世界里面再套着的 level.dat 不算另一个世界），正好认出一个才用，没有或者不止一个就不还原；
//  4. 包里一个 level.dat 都没有（世界从没存过盘）、但有现在这个世界名的文件夹：照现在的世界名还原。
//
// 世界名不一样时（备份以后改过 level-name），备份里的世界放进现在的世界名下面：server.properties 不改，
// 开服读的就是还原出来的这一份。原来照包里的名字解，现在的世界删了、备份里的那个服务器又不读，开服就生成一张新地图。
func planWorldRestore(files []*zip.File, level string) (*worldRestorePlan, error) {
	names := make([]string, len(files)) // 条目统一成斜杠分隔、去掉 ./ 和 ..（world/../server.properties 就是 server.properties）
	has := map[string]bool{}            // 包里出现过的文件夹（每一级都记）
	levelDirs := map[string]bool{}      // 直接装着 level.dat 的文件夹
	var props *zip.File
	for k, f := range files {
		n := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		names[k] = n
		if f.FileInfo().IsDir() || !fs.ValidPath(n) || n == "." {
			continue
		}
		for d := path.Dir(n); d != "."; d = path.Dir(d) {
			has[d] = true
		}
		switch {
		case path.Base(n) == "level.dat" && path.Dir(n) != ".":
			levelDirs[path.Dir(n)] = true
		case strings.EqualFold(n, "server.properties"):
			props = f
		}
	}
	packed := "" // 包里 server.properties 写的世界名；没带这个文件、读不出来时为空
	if props != nil {
		packed = propsLevelName(props)
	}
	from := ""
	switch {
	case levelDirs[level]:
		from = level
	case packed != "" && levelDirs[packed]:
		from = packed
	default:
		cands := worldCandidates(levelDirs, has)
		switch {
		case len(cands) == 1:
			from = cands[0]
		case len(cands) > 1:
			if len(cands) > 5 {
				cands = append(cands[:5], "……")
			}
			return nil, fmt.Errorf("这份备份里有好几个世界（%s），认不出该还原哪一个，没有还原，什么都没动", strings.Join(cands, "、"))
		case has[level] || has[level+"_nether"] || has[level+"_the_end"]:
			from = level
		default:
			return nil, fmt.Errorf("这份备份里没找到世界（没有装着 level.dat 的文件夹），没有还原，什么都没动")
		}
	}
	// 从包里认出来的名字也要过一遍：指到外面、指回服务器文件夹自己、落进 .minedeck 的都不要
	if !SafeLevelName(from) {
		return nil, fmt.Errorf("这份备份里的世界文件夹「%s」不能用，没有还原，什么都没动", from)
	}
	plan := &worldRestorePlan{from: from}
	for k, f := range files {
		n := names[k]
		if f.FileInfo().IsDir() {
			continue
		}
		target, ok := "", false
		for _, s := range worldSuffixes {
			if rest, cut := strings.CutPrefix(n, from+s+"/"); cut && rest != "" {
				target, ok = level+s+"/"+rest, true
				break
			}
		}
		if ok {
			plan.entries = append(plan.entries, restoreEntry{f: f, name: target})
		} else {
			plan.skipped = append(plan.skipped, f.Name)
		}
	}
	if len(plan.entries) == 0 {
		return nil, fmt.Errorf("这份备份里的世界「%s」是空的，没有还原，什么都没动", from)
	}
	return plan, nil
}

// propsLevelName 包里那份 server.properties 写的世界名（读法同 LevelNameStrict：没写、不能用的按 world 算）。只读不写；
// 读不出来返回空串，调用方当它没有。太大的不读：正常的 server.properties 只有几 KB。
func propsLevelName(f *zip.File) string {
	if f.UncompressedSize64 > 1<<20 {
		return ""
	}
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return ""
	}
	if v, ok := JavaProps(b)["level-name"]; ok && SafeLevelName(v) {
		return filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(v))))
	}
	return "world"
}

// worldCandidates 按带 level.dat 的文件夹认出包里的世界（排好序）。X_nether、X_the_end 在 X 也在包里时算 X 的一部分；
// 套在另一个世界里面的（模组在世界里放的副本这类）不算另一个世界。
func worldCandidates(levelDirs, has map[string]bool) []string {
	set := map[string]bool{}
	for d := range levelDirs {
		for _, s := range worldSuffixes[1:] {
			if base, ok := strings.CutSuffix(d, s); ok && base != "" && (levelDirs[base] || has[base]) {
				d = base
				break
			}
		}
		set[d] = true
	}
	var out []string
	for d := range set {
		nested := false
		for e := range set {
			if e != d && strings.HasPrefix(d, e+"/") {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// skippedSample 控制台里举几个没还原的文件当例子：server.properties 在的话排第一个，用户关心的就是它。
func skippedSample(skipped []string) string {
	var out []string
	for _, s := range skipped {
		if strings.EqualFold(path.Clean(strings.ReplaceAll(s, "\\", "/")), "server.properties") {
			out = append(out, s)
			break
		}
	}
	for _, s := range skipped {
		if len(out) >= 3 {
			break
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(skipped) > len(out) {
		return strings.Join(out, "、") + " 等"
	}
	return strings.Join(out, "、")
}

func checkBackupPaths(files []*zip.File, destDir string) error {
	return checkEntryPaths(allEntries(files), destDir)
}

// checkEntryPaths 同 checkBackupPaths，按每一项要落到的路径查（还原备份时是换成现在的世界名以后的路径）。
func checkEntryPaths(ents []restoreEntry, destDir string) error {
	cleanDest := filepath.Clean(destDir)
	realDest, err := RealPathStrict(cleanDest)
	if err != nil {
		return fmt.Errorf("备份还原目标文件夹不可用: %w", err)
	}
	for _, e := range ents {
		if _, err := checkBackupTargetPath(cleanDest, realDest, e); err != nil {
			return err
		}
	}
	return nil
}

// checkBackupTargetPath 校验备份条目不会借由 ..、符号链接或目录联接落到 destDir 之外（A12）。落点按 e.name 算。
func checkBackupTargetPath(cleanDest, realDest string, e restoreEntry) (string, error) {
	if e.f.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("备份内含符号链接: %s", e.f.Name)
	}
	rel := filepath.FromSlash(strings.ReplaceAll(e.name, "\\", "/"))
	target := filepath.Join(cleanDest, rel)
	if target == cleanDest || !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
		return "", fmt.Errorf("备份内含非法路径: %s", e.f.Name)
	}
	// 从上一级文件夹查起，逐级看已存在的部分是不是符号链接、目录联接。不查目标本身：OneDrive 这类云同步的文件
	// 在 Go 里可能也只报 ModeIrregular，和联接分不开，查它会让现有的世界文件挡住整次还原；目标本身指到外面的，
	// 由下面的 RealPathStrict + InsideReal 拦住。
	for cur := filepath.Dir(target); cur != cleanDest; {
		if st, err := os.Lstat(cur); err == nil {
			if linkmode.IsLink(st.Mode()) {
				rel, _ := filepath.Rel(cleanDest, cur)
				return "", fmt.Errorf("服务器文件夹里的「%s」是指向别处的链接，没有还原", filepath.ToSlash(rel))
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("检查备份落点失败: %s（%w）", e.f.Name, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	realTarget, err := RealPathStrict(target)
	if err != nil || !InsideReal(realDest, realTarget) {
		// 上面几级都不是链接，走到这里多半是服务器文件夹里已有的目标本身指到了外面
		rel, _ := filepath.Rel(cleanDest, target)
		return "", fmt.Errorf("服务器文件夹里的「%s」指到了服务器文件夹外面，或者查不清实际在哪里，没有还原", filepath.ToSlash(rel))
	}
	return target, nil
}

func unzipReaderTo(zr *zip.Reader, destDir string, lease *diskguard.Lease) error {
	return unzipEntriesTo(allEntries(zr.File), destDir, lease)
}

// unzipEntriesTo 同 unzipReaderTo，只解 ents 里的这几项，各自落到 e.name。
func unzipEntriesTo(ents []restoreEntry, destDir string, lease *diskguard.Lease) error {
	cleanDest := filepath.Clean(destDir)
	realDest, err := RealPathStrict(cleanDest)
	if err != nil {
		return fmt.Errorf("备份还原目标文件夹不可用: %w", err)
	}
	for _, e := range ents {
		f := e.f
		if lease != nil {
			if err := lease.Check(0); err != nil {
				return err
			}
		}
		target, err := checkBackupTargetPath(cleanDest, realDest, e)
		if err != nil {
			return err
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
		w, err := openInPlaceNoHardlink(target, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		var dst io.Writer = w
		if lease != nil {
			dst = lease.Writer(w)
		}
		_, cerr := io.Copy(dst, rc)
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
