package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/inst"
)

// 交接包带着「现在的世界是从哪份备份还原来的」（inst.WorldFrom）：世界确实是包里带的那份时，导入的那台沿用它（A52）。

// withWorldFrom 改 A 那台的 instance.json，记上 wf。
func withWorldFrom(t *testing.T, dir string, wf *inst.WorldFrom) {
	t.Helper()
	p := filepath.Join(dir, "instance.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var s inst.Settings
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	s.WorldFrom = wf
	if b, err = json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, string(b))
}

// handOver 导出 A 的服、再导入成 name，返回导入的那台。
func handOver(t *testing.T, name string) (*inst.Manager, *inst.Instance) {
	t.Helper()
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: name, EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	if isnap, _, _, _ := waitTask(t, m, iid); isnap.Err != "" {
		t.Fatalf("导入失败：%s\n%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
	b, err := m.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return m, b
}

func TestHandoverCarriesWorldFrom(t *testing.T) {
	newEnv(t)
	aDir := fakeInstance(t, "A 的服")
	want := inst.WorldFrom{Backup: "20261108-234100.000000000-拆主基地前.zip", Label: "拆主基地前", BackupTime: "2026-11-08 23:41:00", At: "2026-11-12 20:05:31"}
	withWorldFrom(t, aDir, &want)
	_, b := handOver(t, "B 接手")
	want.Carried = true // 随交接包带来的记上（A52）：B 的备份页页顶照实说那份备份在原来那台电脑上
	if got := b.WorldFromSnapshot(); got == nil || *got != want {
		t.Fatalf("导入的那台应沿用这条记录：%+v", got)
	}
	// 记进了 B 的 instance.json：重开程序以后还在
	m2 := manager(t, "B 接手")
	b2, _ := m2.Get("B 接手")
	if got := b2.WorldFromSnapshot(); got == nil || *got != want {
		t.Fatalf("重开以后：%+v", got)
	}
}

// 清单里的记录写法不对（往外走的路径、不是 zip、时间乱写）：整条不要，导入照常完成。
func TestHandoverDropsBadWorldFrom(t *testing.T) {
	newEnv(t)
	aDir := fakeInstance(t, "A 的服")
	withWorldFrom(t, aDir, &inst.WorldFrom{Backup: `..\..\evil.zip`, At: "2026-11-12 20:05:31"})
	if _, b := handOver(t, "B 接手"); b.WorldFromSnapshot() != nil {
		t.Fatalf("写法不对的记录不该收：%+v", b.WorldFromSnapshot())
	}
}

// 包里没带世界（A 那台还没生成过世界、或者世界被删了）：B 的世界是第一次启动时新生成的，不沿用这条记录。
func TestHandoverWithoutWorldDropsWorldFrom(t *testing.T) {
	newEnv(t)
	aDir := fakeInstance(t, "A 的服")
	withWorldFrom(t, aDir, &inst.WorldFrom{Backup: "20261108-234100.000000000-拆主基地前.zip", At: "2026-11-12 20:05:31"})
	for _, d := range []string{"world", "world_nether"} {
		if err := os.RemoveAll(filepath.Join(aDir, d)); err != nil {
			t.Fatal(err)
		}
	}
	if _, b := handOver(t, "B 接手"); b.WorldFromSnapshot() != nil {
		t.Fatalf("包里没有世界时不该沿用：%+v", b.WorldFromSnapshot())
	}
}
