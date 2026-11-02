package inst

import "testing"

// 定时任务执行后经回调交给操作层记一条（来源 schedule），事后查得到「是谁备份 / 重启的」；没执行的不记。
func TestScheduleNotesAutoAction(t *testing.T) {
	m, _ := tempInstance(t)
	var got []AutoAction
	m.SetAutoHook(func(a AutoAction) { got = append(got, a) })
	m.execSchedule(m.insts["s"], Schedule{Type: "backup", At: "04:00"})
	m.execSchedule(m.insts["s"], Schedule{Type: "command", At: "05:00", Args: "say hi"}) // 没在运行：不发命令，也不记
	m.execSchedule(m.insts["s"], Schedule{Type: "restart", At: "06:00"})                 // 同上
	if len(got) != 1 {
		t.Fatalf("应只记定时备份这一条：%+v", got)
	}
	a := got[0]
	if a.Op != "instance.backup.create" || a.Actor != "定时任务" || a.Err != nil ||
		a.Args["inst"] != "s" || a.Args["at"] != "04:00" || a.Args["label"] != "定时" {
		t.Fatalf("记录不对：%+v", a)
	}
	if len(backupFiles(t, m)) != 1 {
		t.Fatal("定时备份没做")
	}
}
