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

// 定时任务的时刻存成补零的 HH:MM：调度按 "15:04" 逐字比对，经接口存进来的「9:05」原来永远不会触发。
func TestNormHHMM(t *testing.T) {
	for in, want := range map[string]string{"9:05": "09:05", "09:5": "09:05", " 21:30 ": "21:30", "0:0": "00:00", "23:59": "23:59"} {
		if got, ok := normHHMM(in); !ok || got != want {
			t.Errorf("normHHMM(%q) = %q %v，应为 %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "9", "24:00", "12:60", "-1:00", "9:05x", "9:05 x", "aa:bb"} {
		if got, ok := normHHMM(bad); ok {
			t.Errorf("normHHMM(%q) 应报错，得到 %q", bad, got)
		}
	}
	dir := t.TempDir()
	i := &Instance{Settings: Settings{Name: "t"}, Dir: dir, Console: newConsole()}
	m := &Manager{insts: map[string]*Instance{"t": i}, creating: map[string]bool{}}
	if err := m.UpdatePolicies("t", Policies{Schedules: []Schedule{{Type: "restart", At: "9:05"}}}); err != nil {
		t.Fatal(err)
	}
	if at := i.Policies.Schedules[0].At; at != "09:05" {
		t.Errorf("存进去的应是补零的 09:05：%q", at)
	}
}
