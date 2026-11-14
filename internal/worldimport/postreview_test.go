package worldimport

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

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

// 打开 ZIP 时读文件清单的字节数有预算：Go 的 zip 包会把清单里的每一条都读进内存、不看包里申报的条数，
// 条目数要等它读完才比得了。超了预算就停，按「文件太多」拦下，不等读完。
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

	// 预算内读到的字节数不超过预算（外加末尾找清单位置的那一小段），没有把整份清单读完
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	counted := &countingReaderAt{r: f}
	b := &budgetReaderAt{r: counted}
	b.left.Store(maxZipDirBytes)
	b.limited.Store(true)
	_, zerr := zip.NewReader(b, st.Size())
	f.Close()
	if zerr == nil || !b.over.Load() {
		t.Fatalf("超过预算应当打不开：%v", zerr)
	}
	if counted.n > maxZipDirBytes {
		t.Errorf("预算是 %d 字节，实际读了 %d 字节", maxZipDirBytes, counted.n)
	}

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

type countingReaderAt struct {
	r io.ReaderAt
	n int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	return n, err
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
