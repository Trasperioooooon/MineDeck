package worldimport

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/inst"
	wt "minedeck/internal/worldimport/worldtest"
)

// 换地图和「现在的世界从哪份备份来」（inst.WorldFrom，备份页页顶那一句）的关系（A51）。

// restoredEnv 一台从备份还原过的服务器：WorldFrom 记着那份备份。返回记录，以及一张能拿来导入的地图。
func restoredEnv(t *testing.T) (*env, *inst.WorldFrom, string) {
	t.Helper()
	e := newEnv(t, "")
	file, err := e.m.CreateBackup("生存服", "拆家前")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.RestoreBackup("生存服", file); err != nil {
		t.Fatal(err)
	}
	i, _ := e.m.Get("生存服")
	wf := i.WorldFromSnapshot()
	if wf == nil || wf.Backup != file {
		t.Fatalf("测试前提：还原以后应记着这份备份：%+v", wf)
	}
	src := filepath.Join(e.tmp, "map")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "NEW", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	return e, wf, src
}

// worldFromOnDisk instance.json 里记着的那份。
func (e *env) worldFromOnDisk() *inst.WorldFrom {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, "instance.json"))
	if err != nil {
		e.t.Fatal(err)
	}
	var s inst.Settings
	if err := json.Unmarshal(b, &s); err != nil {
		e.t.Fatal(err)
	}
	return s.WorldFrom
}

func (e *env) importWith(src string, fault func(point string) error) (result, errMsg string) {
	e.t.Helper()
	faultHook = fault
	e.t.Cleanup(func() { faultHook = nil })
	p := e.plan(src)
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		e.t.Fatal(err)
	}
	return e.wait(id)
}

// 换地图换到一半失败、自动退回了原来的世界：记录还是原来那一份（内存和 instance.json 都是），页顶照旧说得出来源。
func TestFailedImportKeepsWorldFrom(t *testing.T) {
	for _, point := range []string{"old-out-0", "new-in-0", "verify"} {
		t.Run(point, func(t *testing.T) {
			e, want, src := restoredEnv(t)
			_, msg := e.importWith(src, func(p string) error {
				if p == point {
					return errors.New("模拟：文件被别的程序占着")
				}
				return nil
			})
			if !strings.Contains(msg, "已恢复到导入前的世界") {
				t.Fatalf("测试前提：导入应失败并退回：%q", msg)
			}
			if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
				t.Fatalf("测试前提：世界应是原来那个：%s", got)
			}
			i, _ := e.m.Get("生存服")
			if got, disk := i.WorldFromSnapshot(), e.worldFromOnDisk(); got == nil || *got != *want || disk == nil || *disk != *want {
				t.Fatalf("退回以后记录应原样留着：内存 %+v，instance.json %+v，原来 %+v", got, disk, want)
			}
		})
	}
}

// 换回导入前的地图换到一半失败、自动退回了（世界还是换回之前那个）：记录还是原来那一份。
// 情形：导入以后又从备份还原过一次（记录指着那份备份），再点「恢复导入前的地图」。
func TestFailedRestoreKeepsWorldFrom(t *testing.T) {
	e := newEnv(t, "")
	if _, msg := e.wait(mustExecute(t, e, e.plan(e.bukkitZip()))); msg != "" {
		t.Fatal(msg)
	}
	file, err := e.m.CreateBackup("生存服", "导入以后")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.RestoreBackup("生存服", file); err != nil {
		t.Fatal(err)
	}
	i, _ := e.m.Get("生存服")
	want := i.WorldFromSnapshot()
	if want == nil {
		t.Fatal("测试前提：还原以后应记着这份备份")
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) != 1 || !st.Points[0].CanReturn {
		t.Fatalf("测试前提：%+v %v", st, err)
	}
	faultHook = func(p string) error {
		if p == "old-in-0" {
			return errors.New("模拟：文件被别的程序占着")
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	id, err := e.s.Restore("生存服", st.Points[0].ID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); !strings.Contains(msg, "没能换回旧地图") {
		t.Fatalf("测试前提：换回应失败并退回：%q", msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:NEW" {
		t.Fatalf("测试前提：世界应还是换回之前那个：%s", got)
	}
	if got, disk := i.WorldFromSnapshot(), e.worldFromOnDisk(); got == nil || *got != *want || disk == nil || *disk != *want {
		t.Fatalf("换回退回以后记录应原样留着：内存 %+v，instance.json %+v，原来 %+v", got, disk, want)
	}
	// 换回成功：世界换了，照旧清掉
	faultHook = nil
	id, err = e.s.Restore("生存服", st.Points[0].ID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); msg != "" {
		t.Fatal(msg)
	}
	if i.WorldFromSnapshot() != nil || e.worldFromOnDisk() != nil {
		t.Fatal("换回成功以后应清掉记录")
	}
}

// 导入成功：世界换了，照旧清掉记录。退回也失败（卡住、要人处理）的拿不准世界是哪一份，也清掉。
func TestChangedOrStuckImportClearsWorldFrom(t *testing.T) {
	e, _, src := restoredEnv(t)
	if _, msg := e.importWith(src, nil); msg != "" {
		t.Fatal(msg)
	}
	i, _ := e.m.Get("生存服")
	if i.WorldFromSnapshot() != nil || e.worldFromOnDisk() != nil {
		t.Fatal("换成了的导入应清掉记录")
	}

	e, _, src = restoredEnv(t)
	_, msg := e.importWith(src, func(p string) error {
		if p == "new-in-0" || p == "rollback" {
			return errors.New("模拟：文件被别的程序占着")
		}
		return nil
	})
	if !strings.Contains(msg, "恢复未完成") {
		t.Fatalf("测试前提：退回也应失败：%q", msg)
	}
	i, _ = e.m.Get("生存服")
	if i.WorldFromSnapshot() != nil || e.worldFromOnDisk() != nil {
		t.Fatal("卡住时拿不准世界是哪一份，应清掉记录")
	}
}
