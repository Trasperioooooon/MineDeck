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
	"testing"
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
