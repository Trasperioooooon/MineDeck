package transfer

import (
	"path/filepath"
	"strings"
	"testing"
)

// 整合包导入时没装上的文件的清单（.minedeck/import-pending.json 和那份 txt）是这台电脑上的记录，交接包不带；
// 缺了哪些写进任务日志和剔除清单，交的人、接的人都看得见（beta.3 R09）。
func TestExportLeavesPendingListOutButSaysWhatIsMissing(t *testing.T) {
	newEnv(t)
	dir := fakeInstance(t, "A 的服")
	writeFile(t, filepath.Join(dir, ".minedeck", "import-pending.json"),
		`{"version":1,"pack":"测试包","items":[{"id":"p1","path":"mods/missing.jar","reason":"nohash"},{"id":"p2","path":"config/x.toml","reason":"badurl","skipped":true}]}`)
	writeFile(t, filepath.Join(dir, "导入待决清单.txt"), "这台服务器导入整合包时……")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: false})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	for _, e := range packEntries(t, snap.Result) {
		if strings.Contains(e, "import-pending") || strings.Contains(e, "导入待决清单") || strings.Contains(e, ".minedeck") {
			t.Errorf("清单不该进包：%s", e)
		}
	}
	mf, err := ReadManifest(snap.Result)
	if err != nil {
		t.Fatal(err)
	}
	ex := strings.Join(mf.Excluded, "\n")
	if !strings.Contains(ex, "没装上的 2 个文件") || !strings.Contains(ex, "mods/missing.jar") || !strings.Contains(ex, "config/x.toml") {
		t.Errorf("剔除清单应写明缺了哪些：%s", ex)
	}
	if log := strings.Join(snap.Log, "\n"); !strings.Contains(log, "没装上") || !strings.Contains(log, "详情页") {
		t.Errorf("任务日志应说一声：%s", log)
	}
}
