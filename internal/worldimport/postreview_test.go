package worldimport

import (
	"errors"
	"os"
	"path/filepath"
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
