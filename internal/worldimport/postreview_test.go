package worldimport

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"minedeck/internal/atomicfile"
	"minedeck/internal/inst"
	wt "minedeck/internal/worldimport/worldtest"
)

// 2026-11-14 四个 PR 合进 main 以后，Codex 做访问控制复审查出来的几处，各自的回归测试。
// 报告原文和逐条核实在 docs/评审-合并后复审-2026-11-14.md。

// 第 4 条：换回、删恢复点时，记录编号是请求里给的。编号会拼进路径，拼之前要先确认它是单独一段名字，
// 不然请求就能决定主机去读哪个位置的 journal.json。
func TestJournalIDMustBeOneSegment(t *testing.T) {
	dir := t.TempDir()
	// 在事务目录外面放一份内容通得过核对的记录，编号正好写成指到它自己的相对路径：
	// 不在入口挡住的话，loadJournal 会把它当成一份正常的记录读出来
	evil := "../../world/x"
	write(t, dir, map[string]string{
		"world/x/journal.json": `{"id":"../../world/x","inst":"s","state":"committed","levelName":"world"}`,
	})
	if _, err := os.Stat(journalPath(dir, evil)); err != nil {
		t.Fatalf("测试自己没摆对：编号拼出来的路径上应该有那份文件：%v", err)
	}
	for _, id := range []string{evil, `..\..\world\x`, "..", ".", "", "a/b", `a\b`, "C:x", "w1.", "w1 ", "CON", "w\x00"} {
		j, err := loadJournal(dir, id)
		if err == nil {
			t.Errorf("编号 %q 不该读出记录：%+v", id, j)
			continue
		}
		// 按「没有这份记录」算，列记录时才不会把这种名字的文件夹当成读不出来的记录去封锁启动
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("编号 %q 应当按没有这份记录算：%v", id, err)
		}
	}

	// 正常的记录照常读得出来，编号的格式不写死（以前留下的、不是 w 加十六进制的也认）
	for _, id := range []string{"w0123456789abcdef01", "wtest", "old-style_1"} {
		if err := saveJournal(dir, &Journal{ID: id, Inst: "s", State: TxCommitted, LevelName: "world"}); err != nil {
			t.Fatal(err)
		}
		if j, err := loadJournal(dir, id); err != nil || j.ID != id {
			t.Errorf("编号 %q 的记录应当读得出来：%v", id, err)
		}
	}
	js, bad := listJournals(dir)
	if len(js) != 3 || len(bad) != 0 {
		t.Errorf("应当列出 3 份记录、没有读不出来的，得到 %d 份、%d 份读不出来", len(js), len(bad))
	}

	// 大得离谱的记录不读进内存，按读不出来算
	big := filepath.Join(txAbs(dir, "wbig"), "journal.json")
	write(t, dir, map[string]string{txRel("wbig") + "/journal.json": `{"id":"wbig","pad":"` + strings.Repeat("x", maxJournalBytes) + `"}`})
	if _, err := loadJournal(dir, "wbig"); err == nil || !strings.Contains(err.Error(), "大得不正常") {
		t.Errorf("超过上限的记录应当报大得不正常（%s）：%v", big, err)
	}
}

// 第 4 条，从操作的入口再走一遍：换回和删除恢复点带着越界的编号，都说找不到，磁盘上什么也不动。
func TestRestoreAndDeleteRejectTraversalID(t *testing.T) {
	e := newEnv(t, "")
	evil := "../../world/x"
	write(t, e.dir, map[string]string{
		"world/x/journal.json": `{"id":"../../world/x","inst":"生存服","state":"committed","levelName":"world"}`,
		"world/x/keep.txt":     "keep",
	})
	if _, err := e.s.Restore("生存服", evil, "restore"); err == nil || !strings.Contains(err.Error(), "找不到这次导入的记录") {
		t.Errorf("越界的编号换回时应当说找不到：%v", err)
	}
	for _, id := range []string{evil, "..", "."} {
		if err := e.s.DeletePoint("生存服", id, "delete"); err == nil || !strings.Contains(err.Error(), "找不到这次导入的记录") {
			t.Errorf("编号 %q 删恢复点时应当说找不到：%v", id, err)
		}
	}
	if read(t, filepath.Join(e.dir, "world", "x", "keep.txt")) != "keep" || !exists(filepath.Join(e.dir, "world", "level.dat")) {
		t.Error("越界的编号不该动到磁盘上的东西")
	}
}

// ---------- 第 1 条：一次检测的内存和工作量有上限 ----------

// mkZip 在临时目录里写一个 ZIP，files 是「路径 → 内容」。
func mkZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "map.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// rawNBT 一份不压缩的 NBT：根复合标签里放一个 pad 字节长的字节数组。
func rawNBT(pad int) string {
	b := []byte{0x0a, 0, 0, 0x07, 0, 1, 'a', byte(pad >> 24), byte(pad >> 16), byte(pad >> 8), byte(pad)}
	b = append(b, make([]byte, pad)...)
	return string(append(b, 0))
}

func scanOf(t *testing.T, p string) *scanResult {
	t.Helper()
	r, err := scanPath(p, filepath.Base(p), true)
	if err != nil {
		t.Fatal(err)
	}
	if r.src != nil {
		t.Cleanup(func() { r.src.Close() })
	}
	return r
}

func issueCodes(is []Issue) string {
	var c []string
	for _, i := range is {
		c = append(c, i.Code)
	}
	return strings.Join(c, ",")
}

// 压缩包的文件清单超过上限：不打开，按「文件太多」拦下，不是「压缩包坏了」。
// 怎么在标准库动手之前拦住、分配量有没有守住，测试在 internal/zipguard。
func TestZipDirectoryBudget(t *testing.T) {
	files := map[string]string{"world/level.dat": rawNBT(8)}
	for i := 0; i < 3000; i++ {
		files[fmt.Sprintf("world/region/r.%d.0.mca", i)] = ""
	}
	p := mkZip(t, files)

	// 默认的预算下照常打开
	src, err := openZip(p)
	if err != nil {
		t.Fatalf("正常的包应当打得开：%v", err)
	}
	if len(src.files) != 3001 {
		t.Errorf("应当读出 3001 个文件，得到 %d", len(src.files))
	}
	src.Close()

	old := maxZipDirBytes
	t.Cleanup(func() { maxZipDirBytes = old })
	maxZipDirBytes = 32 << 10 // 这个包的清单有十几万字节

	// 从检测的入口走：整份拦下，说文件太多，不是「压缩包坏了」
	if _, err := openZip(p); err == nil {
		t.Fatal("超过预算的包不该打得开")
	}
	r := scanOf(t, p)
	if r.Kind != KindUnknown || issueCodes(r.Issues) != "too-many" || len(r.Candidates) != 0 {
		t.Errorf("超过预算应当按文件太多拦下：kind=%s issues=%s 候选 %d 个", r.Kind, issueCodes(r.Issues), len(r.Candidates))
	}
	if !strings.Contains(r.Issues[0].Msg, "文件太多") {
		t.Errorf("提示语：%s", r.Issues[0].Msg)
	}
}

// 一份来源里的世界太多时整份拦下、说清楚，不往下逐个认，也不悄悄截掉一部分。
func TestTooManyWorldsIsRejectedNotTruncated(t *testing.T) {
	old := maxWorlds
	t.Cleanup(func() { maxWorlds = old })
	maxWorlds = 5

	build := func(n, oldOnly int) string {
		files := map[string]string{}
		for i := 0; i < n; i++ {
			files[fmt.Sprintf("saves/w%02d/level.dat", i)] = rawNBT(8)
			files[fmt.Sprintf("saves/w%02d/region/r.0.0.mca", i)] = "x"
		}
		for i := 0; i < oldOnly; i++ {
			files[fmt.Sprintf("saves/o%02d/level.dat_old", i)] = rawNBT(8)
		}
		return mkZip(t, files)
	}
	// 正好到上限：全认出来
	if r := scanOf(t, build(5, 0)); len(r.Candidates) != 5 {
		t.Errorf("5 个世界应当都认出来，得到 %d 个（%s）", len(r.Candidates), issueCodes(r.Issues))
	}
	// 超过一个：整份拦下，提示里有实际的个数和上限
	for _, c := range [][2]int{{6, 0}, {3, 3}} {
		r := scanOf(t, build(c[0], c[1]))
		if r.Kind != KindUnknown || len(r.Candidates) != 0 || len(r.cands) != 0 || issueCodes(r.Issues) != "too-many-worlds" {
			t.Errorf("%d 个世界加 %d 个只有 level.dat_old 的：应当整份拦下，得到 kind=%s 候选 %d 个 issues=%s",
				c[0], c[1], r.Kind, len(r.Candidates), issueCodes(r.Issues))
			continue
		}
		if msg := r.Issues[0].Msg; !strings.Contains(msg, "6 个") || !strings.Contains(msg, "最多 5 个") || !strings.Contains(msg, "level.dat") {
			t.Errorf("提示语应当说有几个、最多几个、该怎么办：%s", msg)
		}
	}
}

// 去重改成按上级目录查以后，认出来的世界和以前一样：嵌在别的世界里面的 level.dat 不单独算，
// 来源的根本身是世界时别的都算它的一部分，只差大小写的上级目录也认。
func TestNestedWorldsFoldIntoParent(t *testing.T) {
	roots := func(r *scanResult) string {
		var s []string
		for _, c := range r.Candidates {
			s = append(s, c.Root)
		}
		sort.Strings(s)
		return strings.Join(s, "|")
	}
	lv := rawNBT(8)
	r := scanOf(t, mkZip(t, map[string]string{
		"a/level.dat": lv, "a/DIM-1/level.dat": lv, "a/x/y/level.dat": lv,
		"b/level.dat": lv, "B/sub/level.dat": lv,
		"c/d/level.dat": lv,
		"ab/level.dat":  lv, // 名字以 a 开头，但不在 a 里面
	}))
	if got := roots(r); got != "a|ab|b|c/d" {
		t.Errorf("认出来的世界应当是 a、ab、b、c/d，得到 %s", got)
	}
	r = scanOf(t, mkZip(t, map[string]string{"level.dat": lv, "x/level.dat": lv, "x/y/level.dat": lv}))
	if got := roots(r); len(r.Candidates) != 1 || got != "" {
		t.Errorf("来源的根就是世界时只该有一个候选，得到 %d 个：%q", len(r.Candidates), got)
	}
}

// 一次检测里所有 NBT 合起来有上限：每个文件各自不超，加起来超了就整份拦下。
func TestScanNBTBudget(t *testing.T) {
	oldB, oldN := maxScanNBTBytes, maxScanNBTNodes
	t.Cleanup(func() { maxScanNBTBytes, maxScanNBTNodes = oldB, oldN })
	files := map[string]string{}
	for i := 0; i < 4; i++ {
		files[fmt.Sprintf("w%d/level.dat", i)] = rawNBT(1000)
	}
	p := mkZip(t, files)

	maxScanNBTBytes = 1 << 20
	if r := scanOf(t, p); len(r.Candidates) != 4 || strings.Contains(issueCodes(r.Issues), "too-much-data") {
		t.Errorf("额度够的时候四个世界都该认出来：%d 个，%s", len(r.Candidates), issueCodes(r.Issues))
	}
	maxScanNBTBytes = 2500 // 一份展开后一千出头：读得完两份，第三份读不完
	r := scanOf(t, p)
	if r.Kind != KindUnknown || len(r.Candidates) != 0 || issueCodes(r.Issues) != "too-much-data" {
		t.Errorf("额度用完应当整份拦下：kind=%s 候选 %d 个 issues=%s", r.Kind, len(r.Candidates), issueCodes(r.Issues))
	}
	// 值的个数同理：每份 2 个值（根和那个字节数组）
	maxScanNBTBytes, maxScanNBTNodes = 1<<20, 5
	if r := scanOf(t, p); issueCodes(r.Issues) != "too-much-data" {
		t.Errorf("值的个数超了也应当整份拦下：%s", issueCodes(r.Issues))
	}
}

// 同时在跑的检测有名额，检测完（不管成没成）名额都还回去。
func TestScanSlotsAreReleased(t *testing.T) {
	e := newEnv(t, "")
	if cap(e.s.scanSlots) != maxConcurrentScans {
		t.Fatalf("名额应当是 %d 个，得到 %d", maxConcurrentScans, cap(e.s.scanSlots))
	}
	good := mkZip(t, map[string]string{"w/level.dat": rawNBT(8)})
	for i := 0; i < maxConcurrentScans+2; i++ {
		if _, err := e.s.ScanPath(me, good); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.ScanPath(me, filepath.Join(e.tmp, "没有这个文件.zip")); err == nil {
			t.Fatal("不存在的文件应当报错")
		}
	}
	if n := len(e.s.scanSlots); n != 0 {
		t.Errorf("检测都结束了，名额应当全还回去，还占着 %d 个", n)
	}
}

// ---------- 合并复审第 1 条：封锁以后不能再换回更早的恢复点 ----------

// findWorldTag 在服务器文件夹里（原位、各个恢复点、暂存区）找带着某个标记的世界还在不在。
func findWorldTag(t *testing.T, dir, tag string) []string {
	t.Helper()
	var at []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "r.0.0.mca" {
			return nil
		}
		if got, err := wt.ReadMarker(p); err == nil && strings.HasSuffix(got, ":overworld:"+tag) {
			rel, _ := filepath.Rel(dir, p)
			at = append(at, filepath.ToSlash(rel))
		}
		return nil
	})
	return at
}

// stuckSecondImport 第一次导入成功（OLD → B），第二次导入（B → C）在新地图已经就位以后出错，退回也出错：
// 服务器被封锁，C 在原位，B 在第二次的恢复点里，OLD 在第一次的恢复点里。返回第一次导入的记录编号。
func stuckSecondImport(t *testing.T, e *env) (first string) {
	t.Helper()
	first = e.importFolder("B")
	time.Sleep(5 * time.Millisecond)
	src := filepath.Join(e.tmp, "C")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "C", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	faultHook = func(point string) error {
		if point == "verify" || point == "rollback" {
			return errors.New("模拟：" + point + " 出错")
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); !strings.Contains(msg, "恢复未完成") {
		t.Fatalf("第二次导入应当卡住：%q", msg)
	}
	faultHook = nil
	if e.s.M.WorldBusy("生存服") == "" {
		t.Fatal("卡住以后服务器应当被封锁")
	}
	for _, tag := range []string{"OLD", "B", "C"} {
		if len(findWorldTag(t, e.dir, tag)) != 1 {
			t.Fatalf("准备阶段：%s 应当正好有一份，实际在 %v", tag, findWorldTag(t, e.dir, tag))
		}
	}
	return first
}

// 第二次导入卡在半路、服务器被封锁时，别的页面上早先打开的确认框不能再把第一次导入换回去。
// 换得回去的话，之后重试恢复第二次导入，会把换回来的那张最早的地图当成第二次的新地图移走、清掉，它就一份都不剩了。
func TestBlockedServerCannotRestoreEarlierPoint(t *testing.T) {
	e := newEnv(t, "")
	first := stuckSecondImport(t, e)

	if _, err := e.s.Restore("生存服", first, "restore"); err == nil {
		// 没拦住：等换回的任务跑完，再重试恢复，看最早的那张地图还在不在
		time.Sleep(300 * time.Millisecond)
		_ = e.s.Retry("生存服")
		t.Errorf("封锁期间不该换得回更早的恢复点。换回再重试恢复以后：OLD 在 %v，B 在 %v，C 在 %v",
			findWorldTag(t, e.dir, "OLD"), findWorldTag(t, e.dir, "B"), findWorldTag(t, e.dir, "C"))
	} else if !strings.Contains(err.Error(), "换地图") && !strings.Contains(err.Error(), "暂停启动") {
		t.Errorf("拒绝的原因应当说清是换地图没走完：%v", err)
	}
	// 删恢复点同样不行
	if err := e.s.DeletePoint("生存服", first, "delete"); err == nil {
		t.Error("封锁期间不该删得掉恢复点")
	}
	// 三张地图都还在，各一份
	for _, tag := range []string{"OLD", "B", "C"} {
		if at := findWorldTag(t, e.dir, tag); len(at) != 1 {
			t.Errorf("%s 应当还在、正好一份，实际在 %v", tag, at)
		}
	}
	// 重试恢复这条路照常能走：退回第二次导入，B 回到原位，解除封锁
	if err := e.s.Retry("生存服"); err != nil {
		t.Fatalf("重试恢复：%v", err)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:B" {
		t.Errorf("重试恢复以后原位应当是 B：%s", got)
	}
	if len(findWorldTag(t, e.dir, "OLD")) != 1 {
		t.Errorf("重试恢复以后 OLD 应当还在第一次的恢复点里：%v", findWorldTag(t, e.dir, "OLD"))
	}
	// 解除以后，最近一次已提交的导入照常能换回
	if err := e.restore(first); err != nil {
		t.Fatalf("解除封锁以后换回第一次导入：%v", err)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("换回以后原位应当是 OLD：%s", got)
	}
}

// 占位时查封锁：被封锁的服务器只有「处理没走完的记录」这条路占得上位，别的都占不上。
func TestWorldHoldChecksBlock(t *testing.T) {
	e := newEnv(t, "")
	m := e.s.M
	m.SetWorldBlock("生存服", "上次换地图没有完成（测试）")
	if _, err := m.BeginWorldChange("生存服", "换地图"); err == nil || !strings.Contains(err.Error(), "上次换地图没有完成") {
		t.Errorf("被封锁时不该占得上位，原因要原样说出来：%v", err)
	}
	release, err := m.BeginWorldRecovery("生存服", "恢复地图")
	if err != nil {
		t.Fatalf("处理没走完的记录应当占得上位：%v", err)
	}
	// 恢复占着的时候，第二个恢复也占不上：占位照查
	if _, err := m.BeginWorldRecovery("生存服", "恢复地图"); err == nil {
		t.Error("已经有人占着位，再占应当失败")
	}
	release()
	m.SetWorldBlock("生存服", "")
	release, err = m.BeginWorldChange("生存服", "换地图")
	if err != nil {
		t.Fatalf("解除封锁以后应当占得上：%v", err)
	}
	release()
}

// 假设真有别的路把更早的地图换到了原位（封锁检查本该拦住）：重试恢复退回第二次导入时，
// 从原位搬回暂存区的不是它自己的新地图，核对 level.dat 对不上，就留着不删。这是丢地图那条链的第二道保险。
func TestRollbackKeepsWorldThatIsNotItsStagedCopy(t *testing.T) {
	e := newEnv(t, "")
	first := stuckSecondImport(t, e)
	// 绕过所有检查，直接把第一次导入换回去（改之前的代码走得到这一步）
	j, err := loadJournal(e.dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if stuck, err := restoreTx(e.dir, j); err != nil || stuck {
		t.Fatalf("restoreTx: %v stuck=%v", err, stuck)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("准备阶段：原位应当是 OLD：%s", got)
	}
	if err := e.s.Retry("生存服"); err != nil {
		t.Fatalf("重试恢复：%v", err)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:B" {
		t.Errorf("重试恢复以后原位应当是 B：%s", got)
	}
	for _, tag := range []string{"OLD", "B", "C"} {
		if at := findWorldTag(t, e.dir, tag); len(at) != 1 {
			t.Errorf("%s 应当还在、正好一份，实际在 %v", tag, at)
		}
	}
	if at := findWorldTag(t, e.dir, "OLD"); len(at) == 1 && !strings.Contains(at[0], "/"+stageSub+"/") {
		t.Errorf("OLD 应当留在第二次导入的暂存区里没被删：%v", at)
	}
}

// ---------- 合并复审第 3 条：看不了的时候不能当成没有 ----------

// lockDirExclusive 像别的程序独占着一个文件夹那样把它打开（不共享），返回放开它的函数。占着的时候别人读不了它。
func lockDirExclusive(t *testing.T, dir string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("没能独占打开 %s：%v", dir, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

// 放换地图记录的文件夹在、却读不了：不能当成「没有记录」。这里像别的程序那样把它独占打开来造出读不了。
// （顺带确认过：那个位置上要是一个同名的文件，系统报的是「找不到路径」，算不存在，里面也确实不可能有记录。）
func TestUnreadableTxFolderIsNotTreatedAsEmpty(t *testing.T) {
	e := newEnv(t, "")
	first := e.importFolder("B")
	txRoot := filepath.Join(e.dir, filepath.FromSlash(inst.WorldTxDir))
	if inst.PendingWorldTx(e.dir) != "" {
		t.Fatal("准备阶段：正常的记录不该封锁")
	}
	// 没有这个文件夹：确实没有记录
	empty := t.TempDir()
	if inst.PendingWorldTx(empty) != "" {
		t.Error("没有放记录的文件夹时不该封锁")
	}
	if js, bad := listJournals(empty); len(js) != 0 || len(bad) != 0 {
		t.Errorf("没有放记录的文件夹时应当什么都没有：%d、%d", len(js), len(bad))
	}

	unlock := lockDirExclusive(t, txRoot)
	if _, err := os.ReadDir(txRoot); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("测试自己没摆对：这里应当读不了、又不是「不存在」：%v", err)
	}
	if why := inst.PendingWorldTx(e.dir); why != inst.WorldTxUnreadable {
		t.Errorf("读不了应当封锁启动：%q", why)
	}
	js, bad := listJournals(e.dir)
	if len(js) != 0 || len(bad) != 1 || !bad[0].root {
		t.Fatalf("读不了应当算一份读不出来的记录：%d 份记录、%d 份读不出来", len(js), len(bad))
	}
	if bad[0].path(e.dir) != txRoot {
		t.Errorf("给人看的位置应当是放记录的文件夹：%s", bad[0].path(e.dir))
	}
	// 重试恢复不能把封锁解掉
	e.s.M.SetWorldBlock("生存服", inst.WorldTxUnreadable)
	if err := e.s.Retry("生存服"); err == nil {
		t.Error("读不了的时候重试恢复不该报成功")
	}
	if e.s.M.WorldBusy("生存服") == "" {
		t.Error("读不了的时候重试恢复不该解除封锁")
	}
	// 「地图」页把它列成读不出来的一项
	st, err := e.s.Status("生存服")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Points) != 1 || st.Points[0].State != TxUnreadable || len(st.Points[0].Stuck) != 1 {
		t.Errorf("世界状态里应当有一项读不出来的记录：%+v", st.Points)
	}
	// 放开以后一切照常
	unlock()
	if err := e.s.Retry("生存服"); err != nil {
		t.Fatalf("文件夹恢复正常以后重试恢复应当解除封锁：%v", err)
	}
	if err := e.restore(first); err != nil {
		t.Fatalf("之后照常能换回：%v", err)
	}
}

// 决定搬不搬东西的那几步，看不了原位或目标在不在的时候停下，不当成「不在」。
// 以前「原位看不了、目标有东西」会被认成这一步已经做完。
func TestStepsStopWhenTheyCannotSee(t *testing.T) {
	dir, j := setup(t)
	from := filepath.Join(dir, "world")
	blind := func(p string) (os.FileInfo, error) {
		if strings.EqualFold(filepath.Clean(p), from) {
			return nil, &os.PathError{Op: "lstat", Path: p, Err: errors.New("Access is denied.")}
		}
		return os.Lstat(p)
	}
	// 目标位置先放上东西：原位看不了的时候，旧写法会认成「已经搬过去了」
	step := j.OldOut[0]
	write(t, dir, map[string]string{step.To + "/level.dat": "SOMETHING"})

	statFn = blind
	t.Cleanup(func() { statFn = os.Lstat })
	if err := doStep(dir, step); err == nil || !strings.Contains(err.Error(), "看不了") {
		t.Errorf("doStep 看不了原位时应当停下：%v", err)
	}
	if _, err := undoStep(dir, step, true); err == nil || !strings.Contains(err.Error(), "看不了") {
		t.Errorf("undoStep 看不了原位时应当停下：%v", err)
	}
	if _, err := arrived(dir, Step{From: "world", To: "x"}, nil); err == nil {
		t.Error("arrived 看不了的时候应当报错，不是说没到")
	}
	// 整份记录的恢复：看不了就标成卡住，什么都不搬、不删
	j.State = TxCommitting
	_ = saveJournal(dir, j)
	if err := recoverTx(dir, j); err == nil || j.State != TxStuck {
		t.Errorf("恢复时看不了应当卡住：err=%v state=%s", err, j.State)
	}
	statFn = os.Lstat
	if read(t, filepath.Join(dir, "world", "level.dat")) != "OLD" || read(t, filepath.Join(dir, filepath.FromSlash(step.To), "level.dat")) != "SOMETHING" {
		t.Error("看不了的时候不该动任何东西")
	}
}

// ---------- 合并复审第 4 条：撤销换回时，设置改不回去不能当成恢复完成 ----------

// lockFileExclusive 像别的程序独占着一个文件那样把它打开（不共享），返回放开它的函数。
func lockFileExclusive(t *testing.T, path string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("没能独占打开 %s：%v", path, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

// 导入时改过难度；换回旧地图走到最后核对时出错，要撤销换回：世界搬回去了，设置也得改回导入时的值。
// 这时候 server.properties 正好被别的程序占着：以前是读不了就跳过、照样记成「已提交」；现在卡住，放开以后重试才算完。
func TestUndoRestoreDoesNotSwallowPropsFailure(t *testing.T) {
	dir, j := setup(t)
	if stuck, err := commitTx(dir, j, map[string]string{"difficulty": "hard"}); err != nil || stuck {
		t.Fatalf("commit: %v stuck=%v", err, stuck)
	}
	props := filepath.Join(dir, "server.properties")
	diff := func() string {
		b, err := os.ReadFile(props)
		if err != nil {
			return "<读不了>"
		}
		return inst.JavaProps(b)["difficulty"]
	}
	if diff() != "hard" {
		t.Fatalf("准备阶段：导入以后难度应当是 hard：%s", diff())
	}
	j2, err := loadJournal(dir, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var unlock func()
	faultHook = func(point string) error {
		if point == "restore-verify" { // 这时候世界已经换回去了，难度也已经改回 easy
			unlock = lockFileExclusive(t, props)
			return errors.New("模拟：核对出错")
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	stuck, err := restoreTx(dir, j2)
	faultHook = nil
	if err == nil || !stuck || j2.State != TxStuck {
		t.Fatalf("设置改不回去时应当卡住：err=%v stuck=%v state=%s", err, stuck, j2.State)
	}
	if !strings.Contains(j2.Err, "服务器设置") {
		t.Errorf("卡住的原因应当说是设置：%s", j2.Err)
	}
	if len(j2.CurOut) == 0 || len(j2.OldIn) == 0 {
		t.Error("卡住时要留着换回的步骤，重试才知道该走哪条路")
	}
	// 世界已经搬回「导入的地图在位」的样子
	if got := read(t, filepath.Join(dir, "world", "level.dat")); got != "NEW" {
		t.Errorf("撤销换回以后原位应当是导入的地图：%q", got)
	}
	if why := inst.PendingWorldTx(dir); why == "" {
		t.Error("卡住的记录应当封锁启动")
	}

	// 放开设置文件，重试：这回走得完
	unlock()
	if diff() != "easy" {
		t.Fatalf("重试之前难度应当还停在 easy：%s", diff())
	}
	j3, err := loadJournal(dir, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := recoverTx(dir, j3); err != nil || j3.State != TxCommitted {
		t.Fatalf("放开以后重试应当恢复完成：err=%v state=%s", err, j3.State)
	}
	if diff() != "hard" {
		t.Errorf("恢复完成以后难度应当改回导入时的 hard：%s", diff())
	}
	if len(j3.CurOut) != 0 || len(j3.OldIn) != 0 || len(j3.Stuck) != 0 {
		t.Error("恢复完成以后换回的步骤和卡住的路径都该清掉")
	}
	if inst.PendingWorldTx(dir) != "" {
		t.Error("恢复完成以后不该再封锁")
	}
	// 之后照常能换回
	if stuck, err := restoreTx(dir, j3); err != nil || stuck {
		t.Fatalf("之后照常能换回：%v stuck=%v", err, stuck)
	}
	if got := read(t, filepath.Join(dir, "world", "level.dat")); got != "OLD" || diff() != "easy" {
		t.Errorf("换回以后原位 = %q，难度 = %s", got, diff())
	}
}

// ---------- 合并复审第 5 条：取消返回成功，地图就一定没动 ----------

// 最后一个文件刚复制完、还在核对的时候点取消：取消返回成功，导入必须放弃，原来的世界一点没动。
// 以前这一段不再看有没有被取消，接着就停服、换图。
func TestCancelAfterCopyStillCancels(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	var cancelled bool
	stagedHook = func() { cancelled = e.s.Cancel(me, <-idCh) }
	t.Cleanup(func() { stagedHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	_, msg := e.wait(id)
	if !cancelled {
		t.Fatal("复制刚完成时取消应当返回成功")
	}
	if !strings.Contains(msg, "已取消") {
		t.Errorf("取消返回了成功，导入就该放弃：任务的结果是 %q", msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("取消以后原来的世界不该动：%s", got)
	}
	if at := findWorldTag(t, e.dir, "B"); len(at) != 0 {
		t.Errorf("取消以后暂存的那份应当清掉：%v", at)
	}
	if e.s.Cancel(me, id) {
		t.Error("任务结束以后再取消应当返回不能取消")
	}
}

// 已经开始换地图以后再点取消：返回「不能取消」，导入照常做完。不会出现「说取消了、其实换了」。
func TestCancelAfterPointOfNoReturnIsRefused(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	accepted, asked := false, false
	faultHook = func(point string) error {
		if point == "old-out-0" && !asked { // 换地图的第一步之前
			asked = true
			accepted = e.s.Cancel(me, <-idCh)
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	res, msg := e.wait(id)
	if !asked {
		t.Fatal("测试自己没摆对：没走到换地图那一步")
	}
	if accepted {
		t.Error("已经开始换地图以后，取消应当返回不能取消")
	}
	if msg != "" || !strings.Contains(res, "尚未启动检查") {
		t.Errorf("导入应当照常做完：result=%q err=%q", res, msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:B" {
		t.Errorf("导入做完以后原位应当是新地图：%s", got)
	}
}

// 别人取消不了：登记还在的时候也一样。
func TestCancelOnlyByOwnerBeforeBoundary(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	var other bool
	stagedHook = func() { other = e.s.Cancel("lan|192.168.1.5", <-idCh) }
	t.Cleanup(func() { stagedHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	if _, msg := e.wait(id); msg != "" || other {
		t.Errorf("别人的取消不该生效：返回 %v，任务 %q", other, msg)
	}
}

// ---------- 合并复审第 6 条、访问控制第 2 条：空间的算术不溢出，暂存和上传看磁盘余量 ----------

// lyingZip 一张正常的地图，外加一个申报得巨大的文件（实际内容只有几个字节）。
func lyingZip(t *testing.T, e *env, declared int64) string {
	t.Helper()
	src := filepath.Join(e.tmp, "lying-src")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "L", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	var entries []wt.ZipEntry
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		body, err := os.ReadFile(p)
		entries = append(entries, wt.ZipEntry{Name: "map/" + filepath.ToSlash(rel), Body: body})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, wt.ZipEntry{Name: "map/region/r.9.9.mca", Body: []byte("tiny"), DeclaredSize: declared})
	z := filepath.Join(e.tmp, fmt.Sprintf("lying-%d.zip", declared))
	if err := wt.WriteZip(z, entries, wt.ZipOpts{}); err != nil {
		t.Fatal(err)
	}
	return z
}

func TestNeedWithReserveDoesNotOverflow(t *testing.T) {
	for _, c := range []struct{ n, reserve, want int64 }{
		{0, 256 << 20, 256 << 20},
		{10 << 30, 256 << 20, 10<<30 + 256<<20},
		{math.MaxInt64, 256 << 20, math.MaxInt64},
		{math.MaxInt64 - 100, 256 << 20, math.MaxInt64},
		{math.MaxInt64 - 256<<20, 256 << 20, math.MaxInt64},
		{-1, 256 << 20, math.MaxInt64}, // 负数本身就不对，按放不下算
	} {
		if got := needWithReserve(c.n, c.reserve); got != c.want || got < 0 {
			t.Errorf("needWithReserve(%d, %d) = %d，应为 %d", c.n, c.reserve, got, c.want)
		}
	}
}

// 压缩包申报的大小加起来接近 MaxInt64 时，「要多少空间」不能溢出成负数把空间检查绕过去。
func TestHugeDeclaredSizeCannotBypassSpaceCheck(t *testing.T) {
	e := newEnv(t, "")
	// 默认：申报的合计超过上限，检测时就整份拦下
	r := scanOf(t, lyingZip(t, e, math.MaxInt64-(1<<20)))
	if r.Kind != KindUnknown || issueCodes(r.Issues) != "bad-size" || len(r.Candidates) != 0 {
		t.Errorf("申报大得离谱的包应当在检测时拦下：kind=%s issues=%s", r.Kind, issueCodes(r.Issues))
	}
	// 就算没有那道上限（这里把它放开来测），做计划时算出来的需要空间也不能是负数，空间不够照样拦
	old := maxZipDeclaredBytes
	t.Cleanup(func() { maxZipDeclaredBytes = old })
	maxZipDeclaredBytes = math.MaxInt64
	p := e.plan(lyingZip(t, e, math.MaxInt64-(8<<20)))
	if p.NeedBytes <= 0 {
		t.Errorf("需要的空间溢出了：%d", p.NeedBytes)
	}
	if !p.Blocked || !strings.Contains(strings.Join(codes(p.Issues), ","), "disk-full") {
		t.Errorf("空间不够应当拦下：blocked=%v issues=%v need=%d free=%d", p.Blocked, codes(p.Issues), p.NeedBytes, p.FreeBytes)
	}
	if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true}); err == nil {
		t.Error("被拦下的计划不该执行得了")
	}
}

// 做计划时空间够、点确认时不够了（隔了很久，或者别的导入占掉了）：开始复制之前再查一遍，不够就不动。
func TestStagingRechecksFreeSpace(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	low := false
	old := diskFree
	diskFree = func(string) (int64, error) {
		if low {
			return stageReserve / 2, nil
		}
		return 1 << 40, nil
	}
	t.Cleanup(func() { diskFree = old })
	p := e.plan(src)
	if p.Blocked {
		t.Fatalf("准备阶段：空间够的时候计划不该被拦：%v", codes(p.Issues))
	}
	low = true
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); !strings.Contains(msg, "磁盘空间不够") || !strings.Contains(msg, "没有替换任何文件") {
		t.Errorf("开始复制前空间不够应当放弃：%q", msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("放弃以后原来的世界不该动：%s", got)
	}
	if at := findWorldTag(t, e.dir, "B"); len(at) != 0 {
		t.Errorf("放弃以后不该留下暂存的文件：%v", at)
	}
	// 查不到剩余空间的盘不拦，但做计划时要说一声
	low = false
	diskFree = func(string) (int64, error) { return 0, errors.New("这个盘查不了") }
	p = e.plan(src)
	if p.Blocked || !strings.Contains(strings.Join(codes(p.Issues), ","), "disk-unknown") {
		t.Errorf("查不到剩余空间时不拦、但要提醒：blocked=%v issues=%v", p.Blocked, codes(p.Issues))
	}
}

// 上传：同时传的有名额，写之前和写的过程中都看放缓存的盘还剩多少。
func TestUploadGuards(t *testing.T) {
	newEnv(t, "")
	old := diskFree
	t.Cleanup(func() { diskFree = old })
	uploads := func() int {
		ents, _ := os.ReadDir(UploadDir())
		return len(ents)
	}

	// 盘的余量放不下对方说的大小再多出留底的：一个字节都不收
	diskFree = func(string) (int64, error) { return uploadReserve + 100, nil }
	if _, err := SaveUpload(strings.NewReader("data"), 1<<20, 4096); !errors.Is(err, ErrUploadNoSpace) {
		t.Errorf("余量不够应当拒收：%v", err)
	}
	if uploads() != 0 {
		t.Error("拒收以后不该留下文件")
	}
	// 够的时候照常收下
	diskFree = func(string) (int64, error) { return 1 << 40, nil }
	dest, err := SaveUpload(strings.NewReader("data"), 1<<20, 4)
	if err != nil || read(t, dest) != "data" {
		t.Fatalf("余量够的时候应当收下：%v", err)
	}
	// 超过单个文件的上限照旧
	if _, err := SaveUpload(strings.NewReader("123456"), 5, 6); !errors.Is(err, ErrUploadTooLarge) {
		t.Errorf("超过上限：%v", err)
	}
	// 名额占满时直接拒绝，放掉以后又能传
	for i := 0; i < cap(uploadSlots); i++ {
		uploadSlots <- struct{}{}
	}
	_, err = SaveUpload(strings.NewReader("data"), 1<<20, 4)
	for i := 0; i < cap(uploadSlots); i++ {
		<-uploadSlots
	}
	if !errors.Is(err, ErrUploadBusy) {
		t.Errorf("名额占满应当拒绝：%v", err)
	}
	if _, err := SaveUpload(strings.NewReader("data"), 1<<20, 4); err != nil {
		t.Errorf("名额放掉以后应当能传：%v", err)
	}
	if len(uploadSlots) != 0 {
		t.Errorf("传完名额应当都还回去：还占着 %d 个", len(uploadSlots))
	}

	// 写的过程中余量掉到留底以下就停：对方说的大小可以是假的
	free := int64(1 << 40)
	diskFree = func(string) (int64, error) { return free, nil }
	sw := &spaceWriter{w: io.Discard, dir: UploadDir()}
	chunk := make([]byte, 16<<20)
	var werr error
	for i := 0; i < 12 && werr == nil; i++ {
		if i == 5 {
			free = uploadReserve - 1
		}
		_, werr = sw.Write(chunk)
	}
	if !errors.Is(werr, ErrUploadNoSpace) {
		t.Errorf("写的过程中余量不够应当停下：%v", werr)
	}
}

// 上传文件的清理：还有效的检测结果用着的文件，放得再久也不删；放弃了就删；程序启动时清空上传目录。
func TestUploadCleanupFollowsScanLifetime(t *testing.T) {
	e := newEnv(t, "")
	zipPath := mkZip(t, map[string]string{"w/level.dat": rawNBT(8), "w/region/r.0.0.mca": "x"})
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	dest, err := SaveUpload(bytes.NewReader(body), 1<<20, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.s.ScanUpload(me, dest, "map.zip")
	if err != nil {
		t.Fatal(err)
	}
	// 文件是五个小时以前传的，检测结果还有效（做计划会把它的有效期往后顺延）
	longAgo := time.Now().Add(-5 * time.Hour)
	if err := os.Chtimes(dest, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(UploadDir(), "u-orphan.upload")
	if err := os.WriteFile(orphan, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(orphan, longAgo, longAgo)
	e.s.sweep()
	if !exists(dest) {
		t.Error("检测结果还有效，它用着的上传文件不该被清掉")
	}
	if exists(orphan) {
		t.Error("没人认领、又放了很久的上传文件应当清掉")
	}
	e.s.Discard(me, sc.ID)
	if exists(dest) {
		t.Error("放弃检测结果以后，上传的文件应当删掉")
	}
	// 程序启动时：上传目录里剩下的都没人认领了，清空
	left := filepath.Join(UploadDir(), "u-left.upload")
	if err := os.WriteFile(left, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.s.clearUploads()
	if exists(left) {
		t.Error("启动时应当清空上传目录")
	}
}

// ---------- 合并复审第 2 条：有备份在跑的时候，导入不停服、直接放弃 ----------

// 导入复制完、要停服换图了，这时候有备份正在压这台服务器的世界：导入放弃（服务器不停，什么都不换），
// 等备份做完再导入就照常。占位那一步本来也占不上，这里要的是在停服之前就看出来。
func TestImportGivesUpWhileBackupIsRunning(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	done, err := e.m.UseWorld("生存服", "备份")
	if err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true, StopFirst: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); !strings.Contains(msg, "正在备份") || !strings.Contains(msg, "没有被停") {
		t.Errorf("有备份在跑时导入应当放弃，并说清服务器没被停：%q", msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("放弃以后原来的世界不该动：%s", got)
	}
	if at := findWorldTag(t, e.dir, "B"); len(at) != 0 {
		t.Errorf("放弃以后暂存的那份应当清掉：%v", at)
	}
	done()
	if got := e.importFolder("B2"); got == "" {
		t.Error("备份做完以后导入应当照常")
	}
	// 换地图占着位的时候，保存设置、改配置文件这类登记不上
	release, err := e.m.BeginWorldChange("生存服", "换地图")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.UseWorld("生存服", "保存设置"); err == nil || !strings.Contains(err.Error(), "换地图") {
		t.Errorf("换地图的时候不该登记得上：%v", err)
	}
	release()
}

// ---------- 第二轮阻挡项 2：「看不了就停下」按类补全 ----------

// accessDenied 让 present（经 statFn）看不了名字以 suffix 结尾的路径，别的照常。返回恢复的函数。
func blindTo(t *testing.T, suffix string) (restore func()) {
	t.Helper()
	suffix = strings.ToLower(filepath.FromSlash(suffix))
	statFn = func(p string) (os.FileInfo, error) {
		if strings.HasSuffix(strings.ToLower(filepath.Clean(p)), suffix) {
			return nil, &os.PathError{Op: "lstat", Path: p, Err: errors.New("Access is denied.")}
		}
		return os.Lstat(p)
	}
	restore = func() { statFn = os.Lstat }
	t.Cleanup(restore)
	return restore
}

// 复制完以后搬哪些目录，从「要复制的文件清单」推出来，再逐个确认真的在：看不了、少了、多了都报错。
func TestStagedDirsComeFromTheCopyList(t *testing.T) {
	stage := t.TempDir()
	write(t, stage, map[string]string{"world/level.dat": "L", "world_nether/DIM-1/region/r.mca": "n"})
	files := []stagedFile{{to: "world/level.dat"}, {to: "world_nether/DIM-1/region/r.mca"}}
	got, err := stagedDirs(stage, "world", files)
	if err != nil || strings.Join(got, ",") != "world,world_nether" {
		t.Fatalf("应当是 world、world_nether：%v %v", got, err)
	}
	// 维度目录一时看不了：报错，不是悄悄不搬它
	restore := blindTo(t, "/world_nether")
	if _, err := stagedDirs(stage, "world", files); err == nil || !strings.Contains(err.Error(), "看不了") {
		t.Errorf("看不了维度目录时应当报错：%v", err)
	}
	restore()
	// 该有的不在
	if err := os.RemoveAll(filepath.Join(stage, "world_nether")); err != nil {
		t.Fatal(err)
	}
	if _, err := stagedDirs(stage, "world", files); err == nil || !strings.Contains(err.Error(), "少了「world_nether」") {
		t.Errorf("该有的目录不在应当报错：%v", err)
	}
	// 不该有的在
	write(t, stage, map[string]string{"world_nether/x": "n", "world_the_end/DIM1/r.mca": "e"})
	if _, err := stagedDirs(stage, "world", files); err == nil || !strings.Contains(err.Error(), "多出了「world_the_end」") {
		t.Errorf("多出来的目录应当报错：%v", err)
	}
	// 要复制的文件不在任何一个世界目录下面：它复制了也不会被搬进去
	if _, err := stagedDirs(stage, "world", append(files, stagedFile{to: "worldx/a"})); err == nil || !strings.Contains(err.Error(), "不在任何一个世界文件夹下面") {
		t.Errorf("落在世界目录外面的文件应当报错：%v", err)
	}
	// 世界名带子目录、大小写不同也认
	stage2 := t.TempDir()
	write(t, stage2, map[string]string{"worlds/Main/level.dat": "L"})
	if got, err := stagedDirs(stage2, "worlds/main", []stagedFile{{to: "worlds/main/level.dat"}}); err != nil || len(got) != 1 {
		t.Errorf("带子目录的世界名：%v %v", got, err)
	}
}

// 目标是 Paper 这类把下界、末地放在旁边两个文件夹里的服务器：复制完以后下界那个文件夹正好看不了。
// 以前它被当成「没有」，不进搬入的清单，主世界照样换上去、核对也通过，下界悄悄没装。现在整次导入放弃。
func TestImportStopsWhenStagedDimensionCannotBeSeen(t *testing.T) {
	e := newEnv(t, "")
	i, err := e.m.Get("生存服")
	if err != nil {
		t.Fatal(err)
	}
	i.Core = "paper"
	// 来源是旧 Bukkit 布局的三个文件夹（原版布局的地图导进 Paper 时不拆，由 Paper 第一次启动自己搬）
	src := filepath.Join(e.tmp, "bukkit-src")
	if err := wt.LegacyBukkit(src, "MapWorld", wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true, Nether: true, End: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	if p.Blocked {
		t.Fatalf("准备阶段：计划不该被拦：%v", codes(p.Issues))
	}
	sawNether := false
	stagedHook = func() {
		// 复制完的那一刻，暂存区里确实有下界的文件夹；从这时起它看不了
		sawNether = len(findDirs(e.dir, "world_nether")) > 0
		blindTo(t, "/"+stageSub+"/world_nether")
	}
	t.Cleanup(func() { stagedHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	res, msg := e.wait(id)
	statFn = os.Lstat
	if !sawNether {
		t.Fatal("测试自己没摆对：这个目标应当把下界暂存成单独的 world_nether 文件夹")
	}
	if !strings.Contains(msg, "看不了") || !strings.Contains(msg, "没有替换任何文件") {
		t.Errorf("下界的暂存目录看不了时，导入应当放弃：result=%q err=%q", res, msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("放弃以后原来的世界不该动：%s", got)
	}
	if at := findWorldTag(t, e.dir, "B"); len(at) != 0 {
		t.Errorf("放弃以后暂存的那份应当清掉：%v", at)
	}
}

// findDirs root 下面名字是 name 的文件夹。
func findDirs(root, name string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == name {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// 读不了 server.properties 时，不能把世界名默认成 world 接着往下做：做计划、换回都停下。
func TestUnreadablePropsStopsInsteadOfDefaultingToWorld(t *testing.T) {
	e := newEnv(t, "")
	first := e.importFolder("B")
	props := filepath.Join(e.dir, "server.properties")
	if name, err := inst.LevelNameStrict(e.dir); err != nil || name != "world" {
		t.Fatalf("准备阶段：%q %v", name, err)
	}
	// 没有这个文件：就是默认的 world，不算错
	if name, err := inst.LevelNameStrict(t.TempDir()); err != nil || name != "world" {
		t.Errorf("没有 server.properties 时应当是 world：%q %v", name, err)
	}
	unlock := lockFileExclusive(t, props)
	if _, err := inst.LevelNameStrict(e.dir); err == nil {
		t.Error("读不了时应当报错")
	}
	if inst.LevelName(e.dir) != "world" {
		t.Error("只拿来显示的读法照旧给默认值")
	}
	if name, ok := inst.CheckLevelName(e.dir); ok {
		t.Errorf("读不了的 server.properties 不该算通过：%q", name)
	}
	if _, _, err := e.s.targetOf("生存服"); err == nil || !strings.Contains(err.Error(), "server.properties") {
		t.Errorf("读不了时不该定得出目标：%v", err)
	}
	if _, err := e.s.Restore("生存服", first, "restore"); err == nil || !strings.Contains(err.Error(), "server.properties") {
		t.Errorf("读不了时不该换得回：%v", err)
	}
	if _, err := e.m.CreateBackup("生存服", "x"); err == nil || !strings.Contains(err.Error(), "server.properties") {
		t.Errorf("读不了时不该备份（备份哪些文件夹是按世界名定的）：%v", err)
	}
	unlock()
	if err := e.restore(first); err != nil {
		t.Fatalf("放开以后照常能换回：%v", err)
	}
}

// 清理暂存区之前的判断：看不了暂存的世界在不在，就留着不删（以前当成不在、照删）。
func TestStageIsKeptWhenItCannotBeSeen(t *testing.T) {
	dir, j := setup(t)
	stage := filepath.Join(txAbs(dir, j.ID), stageSub)
	if !stageIsOurs(stage, j) {
		t.Fatal("准备阶段：暂存区里就是这次导入的那一份")
	}
	restore := blindTo(t, "/"+stageSub+"/world")
	if stageIsOurs(stage, j) {
		t.Error("看不了的时候不能算是自己的那一份（那样会删）")
	}
	restore()
	// level.dat 读不了：同样不算
	unlock := lockFileExclusive(t, filepath.Join(stage, "world", "level.dat"))
	if stageIsOurs(stage, j) {
		t.Error("level.dat 读不了的时候不能算是自己的那一份")
	}
	unlock()
	// 暂存区里没有世界：只剩空壳，可以清
	if err := os.RemoveAll(filepath.Join(stage, "world")); err != nil {
		t.Fatal(err)
	}
	if !stageIsOurs(stage, j) {
		t.Error("暂存的世界确实不在时可以清")
	}
}

// ---------- 撤销换回的另外三种失败（Codex 第二轮点名要补的） ----------

// lockFileNoWrite 像别的程序开着一个文件那样：别人还能读它，但改不了、换不掉。返回放开的函数。
func lockFileNoWrite(t *testing.T, path string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("没能打开 %s：%v", path, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

func TestUndoRestoreOtherFailures(t *testing.T) {
	propOf := func(dir, key string) string {
		b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
		if err != nil {
			return "<读不了>"
		}
		return inst.JavaProps(b)[key]
	}
	// 走到「世界已经换回去、设置也已经改回去、最后核对出错」，由 atVerify 决定这时候造出哪种失败
	failRestore := func(t *testing.T, props map[string]string, atVerify func(dir string, j *Journal)) (string, *Journal, bool, error) {
		t.Helper()
		dir, j := setup(t)
		if stuck, err := commitTx(dir, j, props); err != nil || stuck {
			t.Fatalf("commit: %v stuck=%v", err, stuck)
		}
		j2, err := loadJournal(dir, j.ID)
		if err != nil {
			t.Fatal(err)
		}
		faultHook = func(point string) error {
			if point == "restore-verify" {
				atVerify(dir, j2)
				return errors.New("模拟：核对出错")
			}
			return nil
		}
		t.Cleanup(func() { faultHook = nil; atomicfile.Hook = nil })
		stuck, err := restoreTx(dir, j2)
		faultHook, atomicfile.Hook = nil, nil
		return dir, j2, stuck, err
	}
	// 放开以后重新从盘上读记录、再恢复：必须回到「导入的地图在位、设置是导入时的值」
	recoverAndCheck := func(t *testing.T, dir, id string, want map[string]string) {
		t.Helper()
		j, err := loadJournal(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == TxCommitted {
			t.Fatal("盘上的记录不该已经是「已提交」")
		}
		if err := recoverTx(dir, j); err != nil || j.State != TxCommitted {
			t.Fatalf("放开以后重试应当恢复完成：err=%v state=%s", err, j.State)
		}
		if got := read(t, filepath.Join(dir, "world", "level.dat")); got != "NEW" {
			t.Errorf("恢复完成以后原位应当是导入的地图：%q", got)
		}
		for k, v := range want {
			if got := propOf(dir, k); got != v {
				t.Errorf("恢复完成以后 %s 应当是 %s：%s", k, v, got)
			}
		}
		if inst.PendingWorldTx(dir) != "" {
			t.Error("恢复完成以后不该再封锁")
		}
	}

	t.Run("读得了、写不进", func(t *testing.T) {
		var unlock func()
		dir, j, stuck, err := failRestore(t, map[string]string{"difficulty": "hard"}, func(dir string, _ *Journal) {
			unlock = lockFileNoWrite(t, filepath.Join(dir, "server.properties"))
		})
		if err == nil || !stuck || j.State != TxStuck || !strings.Contains(j.Err, "服务器设置") {
			t.Fatalf("设置写不进去时应当卡住：err=%v stuck=%v state=%s（%s）", err, stuck, j.State, j.Err)
		}
		if propOf(dir, "difficulty") != "easy" {
			t.Fatalf("这时候难度应当还停在 easy：%s", propOf(dir, "difficulty"))
		}
		unlock()
		recoverAndCheck(t, dir, j.ID, map[string]string{"difficulty": "hard"})
	})

	t.Run("两项设置只改回一项", func(t *testing.T) {
		var unlock func()
		dir, j, stuck, err := failRestore(t, map[string]string{"difficulty": "hard", "gamemode": "creative"}, func(dir string, _ *Journal) {
			n := 0
			atomicfile.Hook = func(path string) {
				if n++; n == 2 && unlock == nil { // 第一项存进去了，存第二项的时候文件被别的程序开着
					unlock = lockFileNoWrite(t, path)
				}
			}
		})
		if err == nil || !stuck || j.State != TxStuck {
			t.Fatalf("第二项写不进去时应当卡住：err=%v stuck=%v state=%s", err, stuck, j.State)
		}
		done := 0
		if propOf(dir, "difficulty") == "hard" {
			done++
		}
		if propOf(dir, "gamemode") == "creative" {
			done++
		}
		if done != 1 {
			t.Fatalf("这时候应当正好只有一项改回了导入时的值：difficulty=%s gamemode=%s", propOf(dir, "difficulty"), propOf(dir, "gamemode"))
		}
		unlock()
		recoverAndCheck(t, dir, j.ID, map[string]string{"difficulty": "hard", "gamemode": "creative"})
	})

	t.Run("最后一次存记录失败", func(t *testing.T) {
		var unlock func()
		dir, j, stuck, err := failRestore(t, map[string]string{"difficulty": "hard"}, func(dir string, j *Journal) {
			atomicfile.Hook = func(string) { // 设置正在改回去的时候，记录文件被别的程序开着：设置改得成，记录存不下
				if unlock == nil {
					unlock = lockFileNoWrite(t, journalPath(dir, j.ID))
				}
			}
		})
		if err == nil || !stuck {
			t.Fatalf("记录存不下时不能算恢复完成：err=%v stuck=%v", err, stuck)
		}
		if why := inst.PendingWorldTx(dir); why == "" {
			t.Error("盘上的记录还写着没走完，应当封锁启动")
		}
		unlock()
		recoverAndCheck(t, dir, j.ID, map[string]string{"difficulty": "hard"})
	})
}
