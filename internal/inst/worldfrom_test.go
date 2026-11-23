package inst

import (
	"strings"
	"testing"
)

// A51：占过位、但导入没换成（换图前放弃，或者自动退回成功）时，放掉不清「现在的世界从哪份备份来」；没说没换的照旧清。
func TestWorldIntentUnchangedKeepsWorldFrom(t *testing.T) {
	m, dir := tempInstance(t)
	i, _ := m.Get("s")
	want := &WorldFrom{Backup: "20261115-200000-a.zip", At: "2026-11-15 20:00:00"}
	m.setWorldFrom(i, want)
	saved := func() *WorldFrom {
		s, err := readSettings(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s.WorldFrom
	}
	w, err := m.BeginWorldIntentInstance(i, "准备换地图")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Change(); err != nil {
		t.Fatal(err)
	}
	w.WorldUnchanged()
	w.Release()
	if got, disk := i.WorldFromSnapshot(), saved(); got == nil || *got != *want || disk == nil || *disk != *want {
		t.Fatalf("导入没换成，记录应原样留着：内存 %+v，instance.json %+v", got, disk)
	}
	if why := m.WorldBusy("s"); why != "" {
		t.Fatalf("放掉以后占位应没了：%s", why)
	}
	// 同一台服务器的下一次导入换成了：照旧清掉（上一次的「没换」不会留到下一次）
	w, err = m.BeginWorldIntentInstance(i, "准备换地图")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Change(); err != nil {
		t.Fatal(err)
	}
	w.Release()
	if i.WorldFromSnapshot() != nil || saved() != nil {
		t.Fatal("换成了的导入应清掉记录")
	}
}

// A51：换回旧地图这类占位（BeginWorldChangeInstance）做的事确定没换世界时，放掉占位不清记录；标记只管那一次占位。
func TestWorldChangeUnchangedKeepsWorldFrom(t *testing.T) {
	m, dir := tempInstance(t)
	i, _ := m.Get("s")
	want := &WorldFrom{Backup: "20261115-200000-a.zip", At: "2026-11-15 20:00:00"}
	m.setWorldFrom(i, want)
	saved := func() *WorldFrom {
		s, err := readSettings(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s.WorldFrom
	}
	hold := func(unchanged bool) {
		t.Helper()
		release, err := m.BeginWorldChangeInstance(i, "换回旧地图")
		if err != nil {
			t.Fatal(err)
		}
		if unchanged {
			m.WorldUnchanged(i)
		}
		release()
	}
	m.WorldUnchanged(i) // 没占着位时调了不起作用，不会留到下一次占位
	hold(false)
	if i.WorldFromSnapshot() != nil || saved() != nil {
		t.Fatal("没占着位时的标记不该生效：没说没换的占位应清掉记录")
	}
	m.setWorldFrom(i, want)
	hold(true)
	if got, disk := i.WorldFromSnapshot(), saved(); got == nil || *got != *want || disk == nil || *disk != *want {
		t.Fatalf("换回没换成，记录应原样留着：内存 %+v，instance.json %+v", got, disk)
	}
	hold(false)
	if i.WorldFromSnapshot() != nil || saved() != nil {
		t.Fatal("下一次占位没说没换，应照旧清掉记录（上一次的标记不该留下来）")
	}
}

// A52：交接包清单里带来的 WorldFrom 是别人给的，逐项核对；MineDeck 起的备份名按名字重算时间和标签；收下的记上 Carried。
func TestCarriedWorldFrom(t *testing.T) {
	good := WorldFrom{Backup: "20261108-234100.000000000-拆主基地前.zip", Label: "清单里乱写的标签", BackupTime: "1999-01-01 00:00:00", At: "2026-11-12 20:05:31"}
	got := CarriedWorldFrom(&good)
	if got == nil || got.Backup != good.Backup || got.Label != "拆主基地前" || got.BackupTime != "2026-11-08 23:41:00" || got.At != good.At || !got.Carried {
		t.Fatalf("MineDeck 起名的备份：%+v", got)
	}
	own := WorldFrom{Backup: "我的存档.zip", Label: "不认", BackupTime: "2026-10-01 08:00:00", At: "2026-11-12 20:05:31"}
	if got := CarriedWorldFrom(&own); got == nil || got.Label != "" || got.BackupTime != own.BackupTime || !got.Carried {
		t.Fatalf("自己放进去的 zip：%+v", got)
	}
	own.BackupTime = "昨天"
	if got := CarriedWorldFrom(&own); got == nil || got.BackupTime != "" {
		t.Fatalf("备份时间写法不对时只丢这一项：%+v", got)
	}
	for _, bad := range []WorldFrom{
		{Backup: `..\..\evil.zip`, At: good.At},
		{Backup: "../evil.zip", At: good.At},
		{Backup: `C:\evil.zip`, At: good.At},
		{Backup: "CON.zip", At: good.At},
		{Backup: "a<b>.zip", At: good.At},
		{Backup: "notes.txt", At: good.At},
		{Backup: strings.Repeat("长", 80) + ".zip", At: good.At},
		{Backup: good.Backup, At: "2026/11/12"},
		{Backup: good.Backup},
	} {
		if got := CarriedWorldFrom(&bad); got != nil {
			t.Errorf("%+v 应整条不要，得到 %+v", bad, got)
		}
	}
	if CarriedWorldFrom(nil) != nil {
		t.Error("没有记录时应是 nil")
	}
}
