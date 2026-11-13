package worldimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"minedeck/internal/inst"
)

// 换地图的事务：一份持久记录（journal.json）+ 一串整目录改名。
//
// 所有切换都是同一个盘上的整目录改名（暂存区、恢复点都在实例目录里的 .minedeck/world-import/<编号>/），
// 不复制、不先删再解压。每一步做没做完不看记录里写到哪，看磁盘：改名 A→B，A 不在、B 在就是做完了。
// 所以退到一半再断电，下次照样能接着退；永远不会因为「以为没做完」而把唯一的那份删掉或盖掉。

// 事务目录里的子目录。
const (
	stageSub   = "new"           // 暂存的新世界（相对实例目录的结构原样放在下面）
	oldSub     = "old"           // 恢复点：被换下来的旧世界
	restoreSub = "after-restore" // 换回旧图时，被换下来的导入世界
)

// txRel 事务目录相对实例目录的路径（斜杠分隔）。
func txRel(id string) string { return inst.WorldTxDir + "/" + id }

func txAbs(dir, id string) string { return filepath.Join(dir, filepath.FromSlash(txRel(id))) }

// faultHook 测试用：在名为 point 的步骤之前返回错误，模拟断电、盘满、被占用。
var faultHook func(point string) error

func fault(point string) error {
	if faultHook != nil {
		return faultHook(point)
	}
	return nil
}

// ---------- 记录的读写 ----------

func journalPath(dir, id string) string { return filepath.Join(txAbs(dir, id), "journal.json") }

// saveJournal 先写临时文件、刷到盘上、再用不覆盖目录的写穿改名换上：写到一半断电，留下的要么是旧的一份、要么是新的一份。
func saveJournal(dir string, j *Journal) error {
	j.Updated = time.Now()
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	p := journalPath(dir, j.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return moveFile(tmp, p, true)
}

func loadJournal(dir, id string) (*Journal, error) {
	b, err := os.ReadFile(journalPath(dir, id))
	if err != nil {
		return nil, err
	}
	var j Journal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("换地图的记录读不出来：%w", err)
	}
	if j.ID != id {
		return nil, fmt.Errorf("换地图的记录对不上（%s）", id)
	}
	if err := j.validate(); err != nil {
		return nil, err
	}
	return &j, nil
}

// validate 记录里的每个路径都只能是这台服务器的世界目录、或者这次事务自己的目录：记录虽然在控制目录里，
// 也不拿它里面的路径去改别处的东西。
func (j *Journal) validate() error {
	switch j.State {
	case TxStaging, TxStaged, TxCommitting, TxCommitted, TxRollingBack, TxRolledBack, TxRestoring, TxRestored, TxStuck:
	default:
		return fmt.Errorf("记录里的状态不认识：%q", j.State)
	}
	if !inst.SafeLevelName(j.LevelName) {
		return fmt.Errorf("记录里的世界名不安全：%q", j.LevelName)
	}
	world := map[string]bool{}
	for _, n := range worldDirNames(j.LevelName) {
		world[strings.ToLower(n)] = true
	}
	tx := strings.ToLower(txRel(j.ID)) + "/"
	ok := func(p string, wantWorld bool) bool {
		if p == "" || !filepath.IsLocal(filepath.FromSlash(p)) || path.Clean(p) != p {
			return false
		}
		low := strings.ToLower(p)
		if wantWorld {
			return world[low]
		}
		return strings.HasPrefix(low, tx)
	}
	for _, s := range j.OldOut {
		if !ok(s.From, true) || !ok(s.To, false) {
			return fmt.Errorf("记录里的路径不对：%s → %s", s.From, s.To)
		}
	}
	for _, s := range j.NewIn {
		if !ok(s.From, false) || !ok(s.To, true) {
			return fmt.Errorf("记录里的路径不对：%s → %s", s.From, s.To)
		}
	}
	for _, s := range j.CurOut {
		if !ok(s.From, true) || !ok(s.To, false) {
			return fmt.Errorf("记录里的路径不对：%s → %s", s.From, s.To)
		}
	}
	for _, s := range j.OldIn {
		if !ok(s.From, false) || !ok(s.To, true) {
			return fmt.Errorf("记录里的路径不对：%s → %s", s.From, s.To)
		}
	}
	return nil
}

// worldDirNames 一个 level-name 对应的全部世界目录：主目录，以及旧 Bukkit 布局的 _nether、_the_end。
func worldDirNames(level string) []string {
	return []string{level, level + "_nether", level + "_the_end"}
}

// ---------- 改名 ----------

// moveFile 用 MoveFileEx 改名，带 WRITE_THROUGH：返回时改名已经落盘。不带 COPY_ALLOWED：跨盘就失败，
// 不会退化成「复制一半再删」。replace 只给记录文件用；目录一律不覆盖已有的目标。
func moveFile(from, to string, replace bool) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(f, t, flags); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

// longPath 超过 260 个字符的绝对路径加上 \\?\ 前缀（MoveFileEx 不像 os 包那样自动加）。
func longPath(p string) string {
	if len(p) < 248 || strings.HasPrefix(p, `\\?\`) || !filepath.IsAbs(p) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// doStep 做一步改名 From → To（相对实例目录）。已经做完的（From 不在、To 在）直接算成功。
func doStep(dir string, s Step) error {
	from := filepath.Join(dir, filepath.FromSlash(s.From))
	to := filepath.Join(dir, filepath.FromSlash(s.To))
	fe, te := exists(from), exists(to)
	switch {
	case !fe && te:
		return nil
	case !fe && !te:
		return fmt.Errorf("要移动的文件夹不见了：%s", s.From)
	case fe && te:
		return fmt.Errorf("目标位置已经有东西了：%s", s.To)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := moveFile(from, to, false); err != nil {
		return explainMove(err, s)
	}
	return nil
}

// explainMove 改名失败时说人话：最常见的是有程序还开着里面的文件（手动启动的服务器、资源管理器预览、杀毒软件）。
func explainMove(err error, s Step) error {
	var le *os.LinkError
	if errors.As(err, &le) {
		switch {
		case errors.Is(le.Err, windows.ERROR_SHARING_VIOLATION), errors.Is(le.Err, windows.ERROR_ACCESS_DENIED), errors.Is(le.Err, windows.ERROR_LOCK_VIOLATION):
			return fmt.Errorf("「%s」正被别的程序占用（可能是手动启动的服务器、打开着的资源管理器或杀毒软件），没法移动：%w", s.From, err)
		case errors.Is(le.Err, windows.ERROR_NOT_SAME_DEVICE):
			return fmt.Errorf("「%s」和目标不在同一个盘上，没法直接移动：%w", s.From, err)
		case errors.Is(le.Err, windows.ERROR_DISK_FULL), errors.Is(le.Err, windows.ERROR_HANDLE_DISK_FULL):
			return fmt.Errorf("磁盘空间不够：%w", err)
		}
	}
	return err
}

// undoStep 撤回一步改名 From → To，按磁盘判断：
//   - From 在：这一步没做过（或者已经撤回了），什么都不做。只有 sameTarget 时 To 也在才算矛盾
//     （见 rollback：新世界的 To 就是旧世界的 From，旧世界还没移走时两个都在是正常的）。
//   - From 不在、To 在：做过了，改回去。
//   - 都不在：东西不见了，交给调用方决定。
func undoStep(dir string, s Step, strict bool) (missing bool, err error) {
	from := filepath.Join(dir, filepath.FromSlash(s.From))
	to := filepath.Join(dir, filepath.FromSlash(s.To))
	fe, te := exists(from), exists(to)
	switch {
	case fe && te && strict:
		return false, fmt.Errorf("「%s」和「%s」两边都有东西，不知道哪个是对的", s.From, s.To)
	case fe:
		return false, nil
	case !te:
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(from), 0o755); err != nil {
		return false, err
	}
	if err := moveFile(to, from, false); err != nil {
		return false, explainMove(err, Step{From: s.To, To: s.From})
	}
	return false, nil
}

// ---------- 提交和退回 ----------

// commitTx 把暂存好的新世界换上去。调用方已经占好维护位、确认服务器停着。
// 返回的错误表示没换成（已经尽力退回旧图）；stuck 为真表示退回也没成，服务器要封锁启动。
func commitTx(dir string, j *Journal, props map[string]string) (stuck bool, err error) {
	j.State = TxCommitting
	if err := saveJournal(dir, j); err != nil {
		return false, fmt.Errorf("没能写下换地图的记录，没有替换任何文件：%w", err)
	}
	fail := func(cause error) (bool, error) {
		j.Err = cause.Error()
		if rerr := rollbackTx(dir, j); rerr != nil {
			return true, fmt.Errorf("%v；自动退回也没完成：%v", cause, rerr)
		}
		return false, cause
	}
	for k, s := range j.OldOut {
		if err := fault(fmt.Sprintf("old-out-%d", k)); err != nil {
			return fail(err)
		}
		if err := doStep(dir, s); err != nil {
			return fail(err)
		}
	}
	for k, s := range j.NewIn {
		if err := fault(fmt.Sprintf("new-in-%d", k)); err != nil {
			return fail(err)
		}
		if err := doStep(dir, s); err != nil {
			return fail(err)
		}
	}
	if len(props) > 0 {
		if err := fault("props"); err != nil {
			return fail(err)
		}
		if err := applyProps(dir, j, props); err != nil {
			return fail(fmt.Errorf("改服务器设置失败：%w", err))
		}
	}
	if err := fault("verify"); err != nil {
		return fail(err)
	}
	if j.NewLevelSHA != "" {
		if got := fileSHA(filepath.Join(dir, filepath.FromSlash(j.LevelName), "level.dat")); got != j.NewLevelSHA {
			return fail(fmt.Errorf("换上去以后核对 level.dat 不对，可能被别的程序改动了"))
		}
	}
	j.State = TxCommitted
	j.Err = ""
	if err := saveJournal(dir, j); err != nil {
		// 新图已经在位，只是「完成」没记下来：下次启动会按没完成处理、退回旧图。宁可退回，也不留一个说不清的状态
		return fail(fmt.Errorf("没能记下换地图已完成：%w", err))
	}
	cleanupEmpty(filepath.Join(txAbs(dir, j.ID), stageSub))
	return false, nil
}

// applyProps 把用户勾选的设置写进 server.properties，原值记进记录（换回旧图时改回去）。
func applyProps(dir string, j *Journal, props map[string]string) error {
	pp := filepath.Join(dir, "server.properties")
	b, err := os.ReadFile(pp)
	if err != nil {
		return err
	}
	cur := inst.JavaProps(b)
	if j.PropsBefore == nil {
		j.PropsBefore, j.PropsAfter = map[string]string{}, map[string]string{}
		for k, v := range props {
			j.PropsBefore[k] = cur[k]
			j.PropsAfter[k] = v
		}
		if err := saveJournal(dir, j); err != nil {
			return err
		}
	}
	for k, v := range props {
		if err := inst.ForceProp(pp, k, v); err != nil {
			return err
		}
	}
	return nil
}

// revertProps 把这次改过的设置改回去：只改现在还是我们写的那个值的（之后用户自己又改过的不动）。
func revertProps(dir string, j *Journal) error {
	if len(j.PropsAfter) == 0 {
		return nil
	}
	pp := filepath.Join(dir, "server.properties")
	b, err := os.ReadFile(pp)
	if err != nil {
		return err
	}
	cur := inst.JavaProps(b)
	for k, after := range j.PropsAfter {
		if cur[k] == after {
			if err := inst.ForceProp(pp, k, j.PropsBefore[k]); err != nil {
				return err
			}
		}
	}
	return nil
}

// rollbackTx 退回旧图（提交没完成时）。可以重复执行：每一步看磁盘。
func rollbackTx(dir string, j *Journal) error {
	j.State = TxRollingBack
	_ = saveJournal(dir, j) // 写不下也照样退：退回只看磁盘
	if err := revertProps(dir, j); err != nil {
		return markStuck(dir, j, fmt.Errorf("没能把服务器设置改回去：%w", err))
	}
	// 先把新世界撤回暂存区（倒序），再把旧世界搬回来。新世界的 To 就是旧世界的 From：
	// 旧世界还没移走时两边都在是正常的，所以这里不按「两边都在」报错
	var warn []string
	for k := len(j.NewIn) - 1; k >= 0; k-- {
		if !arrived(dir, j.NewIn[k], j.OldOut) {
			if !exists(filepath.Join(dir, filepath.FromSlash(j.NewIn[k].From))) {
				warn = append(warn, "暂存的新世界不见了："+j.NewIn[k].From)
			}
			continue
		}
		missing, err := undoStep(dir, j.NewIn[k], false)
		if err != nil {
			return markStuck(dir, j, err)
		}
		if missing {
			warn = append(warn, "暂存的新世界不见了："+j.NewIn[k].From)
		}
	}
	for k := len(j.OldOut) - 1; k >= 0; k-- {
		missing, err := undoStep(dir, j.OldOut[k], true)
		if err != nil {
			return markStuck(dir, j, err)
		}
		if missing {
			return markStuck(dir, j, fmt.Errorf("旧世界「%s」在原位和恢复点里都找不到", j.OldOut[k].From))
		}
	}
	if j.OldLevelSHA != "" {
		if got := fileSHA(filepath.Join(dir, filepath.FromSlash(j.LevelName), "level.dat")); got != j.OldLevelSHA {
			return markStuck(dir, j, fmt.Errorf("退回以后核对旧世界的 level.dat 不对"))
		}
	}
	j.State = TxRolledBack
	j.Stuck = nil
	if len(warn) > 0 {
		j.Err = strings.TrimSpace(j.Err + "；" + strings.Join(warn, "；"))
	}
	if err := saveJournal(dir, j); err != nil {
		return err
	}
	// 暂存的那份是来源的副本，不是谁的唯一一份：退回以后删掉省空间
	_ = os.RemoveAll(filepath.Join(txAbs(dir, j.ID), stageSub))
	cleanupEmpty(filepath.Join(txAbs(dir, j.ID), oldSub))
	return nil
}

// markStuck 退不回去：什么都不删，记下要人看的路径，返回原来的错误。
func markStuck(dir string, j *Journal, cause error) error {
	j.State = TxStuck
	j.Err = cause.Error()
	j.Stuck = nil
	for _, s := range append(append(append(append([]Step{}, j.OldOut...), j.NewIn...), j.CurOut...), j.OldIn...) {
		for _, p := range []string{s.From, s.To} {
			if exists(filepath.Join(dir, filepath.FromSlash(p))) {
				j.Stuck = append(j.Stuck, filepath.Join(dir, filepath.FromSlash(p)))
			}
		}
	}
	_ = saveJournal(dir, j)
	return cause
}

// restoreTx 换回导入前的地图（导入已经提交，服务器可能已经启动过、改写过导入的世界）。
// 当前的世界目录整体移进 after-restore/ 留着，恢复点里的旧世界搬回原位，设置改回去，再核对旧世界的 level.dat。
func restoreTx(dir string, j *Journal) (stuck bool, err error) {
	if j.State != TxCommitted {
		return false, fmt.Errorf("这次导入现在的状态是 %s，不能换回", j.State)
	}
	if len(j.OldOut) == 0 {
		// 导入前没有世界：「换回」只会把现在的世界移走，下次启动生成一张随机的新图
		return false, fmt.Errorf("这次导入之前服务器还没有世界，没有可以换回的旧地图")
	}
	j.CurOut, j.OldIn = nil, nil
	for _, n := range worldDirNames(j.LevelName) {
		if exists(filepath.Join(dir, filepath.FromSlash(n))) {
			j.CurOut = append(j.CurOut, Step{From: n, To: txRel(j.ID) + "/" + restoreSub + "/" + n})
		}
	}
	for _, s := range j.OldOut {
		if !exists(filepath.Join(dir, filepath.FromSlash(s.To))) {
			return false, fmt.Errorf("恢复点里少了「%s」，没法换回（没有替换任何文件）", s.From)
		}
		j.OldIn = append(j.OldIn, Step{From: s.To, To: s.From})
	}
	j.State = TxRestoring
	if err := saveJournal(dir, j); err != nil {
		return false, fmt.Errorf("没能写下记录，没有替换任何文件：%w", err)
	}
	fail := func(cause error) (bool, error) {
		j.Err = cause.Error()
		if rerr := undoRestore(dir, j); rerr != nil {
			return true, fmt.Errorf("%v；自动退回也没完成：%v", cause, rerr)
		}
		return false, cause
	}
	for k, s := range j.CurOut {
		if err := fault(fmt.Sprintf("cur-out-%d", k)); err != nil {
			return fail(err)
		}
		if err := doStep(dir, s); err != nil {
			return fail(err)
		}
	}
	for k, s := range j.OldIn {
		if err := fault(fmt.Sprintf("old-in-%d", k)); err != nil {
			return fail(err)
		}
		if err := doStep(dir, s); err != nil {
			return fail(err)
		}
	}
	if err := revertProps(dir, j); err != nil {
		return fail(fmt.Errorf("没能把服务器设置改回去：%w", err))
	}
	if j.OldLevelSHA != "" {
		if got := fileSHA(filepath.Join(dir, filepath.FromSlash(j.LevelName), "level.dat")); got != j.OldLevelSHA {
			return fail(fmt.Errorf("换回以后核对旧世界的 level.dat 不对"))
		}
	}
	j.State = TxRestored
	j.Err = ""
	if err := saveJournal(dir, j); err != nil {
		return fail(err)
	}
	cleanupEmpty(filepath.Join(txAbs(dir, j.ID), oldSub))
	return false, nil
}

// undoRestore 换回旧图没做完：退回到「导入的地图在位」的状态。
func undoRestore(dir string, j *Journal) error {
	// 设置：换回时改回去的那几项，再改成导入时的值（只改现在还是原值的）
	if len(j.PropsAfter) > 0 {
		pp := filepath.Join(dir, "server.properties")
		if b, err := os.ReadFile(pp); err == nil {
			cur := inst.JavaProps(b)
			for k, after := range j.PropsAfter {
				if cur[k] == j.PropsBefore[k] && after != j.PropsBefore[k] {
					_ = inst.ForceProp(pp, k, after)
				}
			}
		}
	}
	for k := len(j.OldIn) - 1; k >= 0; k-- {
		if !arrived(dir, j.OldIn[k], j.CurOut) {
			continue
		}
		if _, err := undoStep(dir, j.OldIn[k], false); err != nil {
			return markStuck(dir, j, err)
		}
	}
	for k := len(j.CurOut) - 1; k >= 0; k-- {
		missing, err := undoStep(dir, j.CurOut[k], true)
		if err != nil {
			return markStuck(dir, j, err)
		}
		if missing {
			return markStuck(dir, j, fmt.Errorf("导入的世界「%s」在原位和暂放处都找不到", j.CurOut[k].From))
		}
	}
	j.State = TxCommitted
	j.CurOut, j.OldIn = nil, nil
	return saveJournal(dir, j)
}

// recoverTx 程序启动时处理一份没走完的记录：提交到一半的退回旧图，换回到一半的退回到导入的地图，
// 暂存到一半的丢掉暂存。返回处理后的状态；stuck 的保持 stuck（要人看）。
func recoverTx(dir string, j *Journal) error {
	switch j.State {
	case TxCommitting, TxRollingBack:
		return rollbackTx(dir, j)
	case TxRestoring:
		return undoRestore(dir, j)
	case TxStaging, TxStaged:
		_ = os.RemoveAll(filepath.Join(txAbs(dir, j.ID), stageSub))
		j.State = TxRolledBack
		if j.Err == "" {
			j.Err = "暂存没有完成就中断了，没有替换任何文件"
		}
		return saveJournal(dir, j)
	case TxStuck:
		// 再试一次：上次是退回旧图还是换回旧图卡住的，看 CurOut / OldIn 有没有
		if len(j.CurOut) > 0 || len(j.OldIn) > 0 {
			return undoRestore(dir, j)
		}
		return rollbackTx(dir, j)
	}
	return nil
}

// arrived 移进来的这一步（s：别处 → 世界原位）有没有真的发生。原位上有东西不一定是它：
// 原位的旧东西要先移走（out 里 From 等于 s.To 的那一步），旧东西还在原位、没进它该去的地方，原位上的就是旧东西。
// 只有这样才不会在暂存区意外丢失时，把旧世界当成新世界挪进暂存区。
func arrived(dir string, s Step, out []Step) bool {
	if exists(filepath.Join(dir, filepath.FromSlash(s.From))) {
		return false // 还在原处：没移动过
	}
	for _, o := range out {
		if strings.EqualFold(o.From, s.To) && !exists(filepath.Join(dir, filepath.FromSlash(o.To))) {
			return false // 原位的旧东西还没移走：原位上的就是它
		}
	}
	return exists(filepath.Join(dir, filepath.FromSlash(s.To)))
}

// ---------- 小工具 ----------

func fileSHA(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cleanupEmpty 删掉 root 下面只剩空文件夹的部分（有文件就不动）。
func cleanupEmpty(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for k := len(dirs) - 1; k >= 0; k-- {
		_ = os.Remove(dirs[k]) // 不空的删不掉，正好
	}
}

// dirBytes 一个目录下文件的总大小（恢复点占多少地方）。
func dirBytes(root string) int64 {
	var n int64
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
