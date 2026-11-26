package inst

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// at 测试里的钟点：2026-11-27 当地时间 h:mi:sec。
func at(h, mi, sec int) time.Time { return time.Date(2026, 11, 27, h, mi, sec, 0, time.Local) }

// 每隔多久按钟点对齐：从每天 0 点起，间隔的整数倍那一分钟执行（每隔 60 分钟就是每个整点）；每天定时的照旧看时刻。
// 存着的任务不再检查，手改出来的坏数据也要有个确定的结果：时刻和间隔都有的按时刻算，间隔越界的按最短、最长算。
func TestDueAt(t *testing.T) {
	cases := []struct {
		name string
		s    Schedule
		now  time.Time
		want bool
	}{
		{"每隔 60 分钟：整点", Schedule{Type: "backup", Every: 60}, at(15, 0, 0), true},
		{"每隔 60 分钟：整点后一分钟", Schedule{Type: "backup", Every: 60}, at(15, 1, 0), false},
		{"每隔 60 分钟：0 点", Schedule{Type: "backup", Every: 60}, at(0, 0, 40), true},
		{"每隔 90 分钟：1:30", Schedule{Type: "command", Every: 90, Args: "say hi"}, at(1, 30, 0), true},
		{"每隔 90 分钟：1:00 不是", Schedule{Type: "command", Every: 90, Args: "say hi"}, at(1, 0, 0), false},
		{"每隔 90 分钟：3:00", Schedule{Type: "command", Every: 90, Args: "say hi"}, at(3, 0, 0), true},
		{"除不尽一天的 50 分钟：23:20", Schedule{Type: "command", Every: 50, Args: "x"}, at(23, 20, 0), true},
		{"除不尽一天的 50 分钟：23:50 不是（0 点重新数）", Schedule{Type: "command", Every: 50, Args: "x"}, at(23, 50, 0), false},
		{"除不尽一天的 50 分钟：0:00", Schedule{Type: "command", Every: 50, Args: "x"}, at(0, 0, 0), true},
		{"每隔 5 分钟", Schedule{Type: "command", Every: 5, Args: "kill @e[type=item]"}, at(4, 35, 0), true},
		{"每天定时", Schedule{Type: "restart", At: "04:00"}, at(4, 0, 59), true},
		{"每天定时：别的时刻", Schedule{Type: "restart", At: "04:00"}, at(5, 0, 0), false},
		{"老配置没补零的 9:05", Schedule{Type: "backup", At: "9:05"}, at(9, 5, 0), true},
		{"时刻和间隔都有（手改的）：按时刻", Schedule{Type: "backup", At: "04:00", Every: 60}, at(4, 0, 0), true},
		{"时刻和间隔都有（手改的）：整点不算", Schedule{Type: "backup", At: "04:00", Every: 60}, at(5, 0, 0), false},
		{"手改的每 1 分钟备份按 30 分钟算：0:30", Schedule{Type: "backup", Every: 1}, at(0, 30, 0), true},
		{"手改的每 1 分钟备份按 30 分钟算：0:01 不备", Schedule{Type: "backup", Every: 1}, at(0, 1, 0), false},
		{"手改的每 1440 分钟按 12 小时算", Schedule{Type: "command", Every: 1440, Args: "x"}, at(12, 0, 0), true},
		{"既没时刻也没间隔", Schedule{Type: "backup"}, at(0, 0, 0), false},
		{"负的间隔当没给", Schedule{Type: "backup", Every: -5}, at(0, 0, 0), false},
	}
	for _, c := range cases {
		if got := dueAt(c.s, c.now); got != c.want {
			t.Errorf("%s：%v 时 dueAt=%v，应为 %v", c.name, c.now.Format("15:04:05"), got, c.want)
		}
	}
}

// tickEnv 一台停着的服务器「t」和一份调度状态；执行换成只记下来（不真的备份、重启）。
type tickEnv struct {
	m  *Manager
	i  *Instance
	st *schedState

	mu      sync.Mutex
	ran     []string                 // 执行过的任务（describe 的写法），按执行先后
	hold    map[string]chan struct{} // 卡住哪些任务：放行前一直「在做」
	blocked int                      // 正卡着的有几个
}

func newTickEnv(t *testing.T, scheds ...Schedule) *tickEnv {
	t.Helper()
	m, i := policyEnv(t)
	i.Policies.Schedules = scheds
	e := &tickEnv{m: m, i: i, hold: map[string]chan struct{}{}}
	e.st = newSchedState(func(_ *Instance, s Schedule) {
		d := describe(s)
		e.mu.Lock()
		e.ran = append(e.ran, d)
		h := e.hold[d]
		if h != nil {
			e.blocked++
		}
		e.mu.Unlock()
		if h != nil {
			<-h
			e.mu.Lock()
			e.blocked--
			e.mu.Unlock()
		}
	})
	return e
}

func bumpGen(i *Instance) {
	i.procMu.Lock()
	i.runGen++
	i.procMu.Unlock()
}

func describe(s Schedule) string {
	w := s.At
	if s.Every > 0 {
		w = "每" + time.Duration(s.Every*int(time.Minute)).String()
	}
	return strings.TrimSpace(s.Type + " " + w + " " + s.Args)
}

// tick 在 now 看一遍，等这一轮交出去的都做完（卡住的除外），返回这一轮执行了哪些（排好序）。
func (e *tickEnv) tick(t *testing.T, now time.Time) []string {
	t.Helper()
	e.mu.Lock()
	before := len(e.ran)
	e.mu.Unlock()
	e.m.schedTick(e.st, now)
	e.settle(t, now)
	e.mu.Lock()
	defer e.mu.Unlock()
	got := slices.Clone(e.ran[before:])
	slices.Sort(got)
	return got
}

// settle 等到还在做的只剩卡住的那些（卡住的都已经记下来了）。
func (e *tickEnv) settle(t *testing.T, now time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.st.busyMu.Lock()
		n := len(e.st.busy)
		e.st.busyMu.Unlock()
		e.mu.Lock()
		b := e.blocked
		e.mu.Unlock()
		if n == b {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 这一轮的任务一直没做完", now.Format("15:04:05"))
		}
		time.Sleep(time.Millisecond)
	}
}

func (e *tickEnv) expect(t *testing.T, now time.Time, want ...string) {
	t.Helper()
	got := e.tick(t, now)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s 应执行 %q，实际 %q", now.Format("15:04:05"), want, got)
	}
}

// 每天定时的和每隔多久的放在一起、放在同一个文件夹里，各按各的点执行；20 秒看一次，同一分钟只执行一次。
func TestSchedTickMixed(t *testing.T) {
	e := newTickEnv(t,
		Schedule{Type: "backup", At: "04:00", Group: "存档"},
		Schedule{Type: "backup", Every: 60, Group: "存档"},
		Schedule{Type: "command", Every: 5, Args: "kill @e[type=item]", Group: "扫地"},
		Schedule{Type: "command", At: "20:00", Args: "say hi"},
	)
	setState(e.i, "running")
	e.expect(t, at(3, 59, 40))
	e.expect(t, at(4, 0, 0), "backup 04:00", "backup 每1h0m0s", "command 每5m0s kill @e[type=item]")
	e.expect(t, at(4, 0, 20)) // 同一分钟再看：不再执行
	e.expect(t, at(4, 0, 40))
	e.expect(t, at(4, 5, 0), "command 每5m0s kill @e[type=item]")
	e.expect(t, at(5, 0, 0), "backup 每1h0m0s", "command 每5m0s kill @e[type=item]")
	e.expect(t, at(20, 0, 0), "backup 每1h0m0s", "command 每5m0s kill @e[type=item]", "command 20:00 say hi")
	// 第二天同一时刻照样执行
	e.expect(t, at(4, 0, 0).AddDate(0, 0, 1), "backup 04:00", "backup 每1h0m0s", "command 每5m0s kill @e[type=item]")
}

// 改了间隔：下一次就是新间隔对得上的下一个钟点（不用等旧的那一轮走完）；两条一模一样的任务各执行各的；
// 这一分钟里删掉前面一条、把一条挪进别的文件夹，剩下的不会因为序号变了再执行一次。
func TestSchedTickEditsWithinMinute(t *testing.T) {
	sweep := Schedule{Type: "command", Every: 60, Args: "kill @e[type=item]"}
	e := newTickEnv(t, Schedule{Type: "restart", At: "06:00"}, sweep, sweep, Schedule{Type: "backup", Every: 60, Group: "存档"})
	setState(e.i, "running")
	e.expect(t, at(6, 0, 0), "restart 06:00", "command 每1h0m0s kill @e[type=item]", "command 每1h0m0s kill @e[type=item]", "backup 每1h0m0s")
	// 同一分钟里：删掉最前面的定时重启、把一条扫地挪进「扫地」文件夹
	moved := sweep
	moved.Group = "扫地"
	if _, err := e.m.PatchPolicies("t", PolicyPatch{Remove: []Schedule{{Type: "restart", At: "06:00"}, sweep}, Add: []Schedule{moved}}); err != nil {
		t.Fatal(err)
	}
	e.expect(t, at(6, 0, 20))
	// 06:10 把每小时的备份改成每 30 分钟：06:30 就执行，不等到 07:00
	if _, err := e.m.PatchPolicies("t", PolicyPatch{
		Remove: []Schedule{{Type: "backup", Every: 60, Group: "存档"}},
		Add:    []Schedule{{Type: "backup", Every: 30, Group: "存档"}},
	}); err != nil {
		t.Fatal(err)
	}
	e.expect(t, at(6, 10, 0))
	e.expect(t, at(6, 30, 0), "backup 每30m0s")
	e.expect(t, at(7, 0, 0), "backup 每30m0s", "command 每1h0m0s kill @e[type=item]", "command 每1h0m0s kill @e[type=item]")
}

// 每隔多久的备份，服务器停着时不备份（世界没变，备出来的一样，还把更早的挤掉）：
// 程序启动以后一直停着的不备；到点时开着的备；上次到点时开着、之后停了的，下一次补一份，再下一次就不备了；
// 两次查看之间开了又停的（只有运行代际变了），也补一份。每天定时的备份照旧，停着也备。
func TestSchedTickIntervalBackupWhileStopped(t *testing.T) {
	e := newTickEnv(t, Schedule{Type: "backup", Every: 60}, Schedule{Type: "backup", At: "06:00"})
	hourly := "backup 每1h0m0s"
	e.expect(t, at(5, 0, 0))                 // 一直停着：不备
	e.expect(t, at(6, 0, 0), "backup 06:00") // 每天定时的照旧
	setState(e.i, "starting")
	bumpGen(e.i)
	e.expect(t, at(6, 10, 0))
	setState(e.i, "running")
	e.expect(t, at(7, 0, 0), hourly) // 开着：备
	e.expect(t, at(7, 20, 0))
	setState(e.i, "stopped")
	e.expect(t, at(7, 40, 0))
	e.expect(t, at(8, 0, 0), hourly) // 7:00 以后开过，7:40 已经停了：补一份
	e.expect(t, at(9, 0, 0))         // 那以后一直停着：不备
	// 9:20 开、9:30 停，中间一次都没看到它开着：运行代际变了，也补一份
	bumpGen(e.i)
	e.expect(t, at(9, 40, 0))
	e.expect(t, at(10, 0, 0), hourly)
	e.expect(t, at(11, 0, 0))
}

// 上一次还没做完（定时备份等服务器稳定下来能等十几分钟）：这一次跳过，在控制台说一声；做完以后下一次照常。
func TestSchedTickSkipsWhileBusy(t *testing.T) {
	e := newTickEnv(t, Schedule{Type: "backup", Every: 30}, Schedule{Type: "command", Every: 30, Args: "say hi"})
	setState(e.i, "running")
	release := make(chan struct{})
	e.hold["backup 每30m0s"] = release
	e.expect(t, at(12, 0, 0), "backup 每30m0s", "command 每30m0s say hi")
	e.expect(t, at(12, 30, 0), "command 每30m0s say hi") // 备份还卡着：跳过，命令照常
	if !consoleHas(e.i, "上一次定时备份还没做完，这一次跳过") {
		t.Errorf("跳过时应在控制台说一声：\n%s", strings.Join(e.i.Console.Lines(), "\n"))
	}
	e.mu.Lock()
	e.hold["backup 每30m0s"] = nil
	e.mu.Unlock()
	close(release)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) { // 等卡着的那次做完
		e.st.busyMu.Lock()
		n := len(e.st.busy)
		e.st.busyMu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("放行以后卡着的备份一直没做完")
		}
	}
	e.expect(t, at(13, 0, 0), "backup 每30m0s", "command 每30m0s say hi")
}

// 每隔多久、文件夹的检查：两样时间只能填一样，间隔有最短、最长，文件夹名去掉首尾空白、不许换行；不合格的整次不改。
func TestPolicyPatchEveryChecks(t *testing.T) {
	m, i := policyEnv(t)
	if err := m.UpdatePolicies("t", Policies{Schedules: []Schedule{schedY}}); err != nil {
		t.Fatal(err)
	}
	for want, s := range map[string]Schedule{
		"每天几点（at）和每隔多久（every）只能填一样":                {Type: "backup", At: "04:00", Every: 60},
		"间隔应为正整数（分钟）: -1":                          {Type: "backup", Every: -1},
		"定时备份最短每隔 30 分钟一次: 10":                     {Type: "backup", Every: 10},
		"定时命令最短每隔 5 分钟一次: 4":                       {Type: "command", Every: 4, Args: "say hi"},
		"定时重启最短每隔 60 分钟一次: 30":                     {Type: "restart", Every: 30},
		"间隔最长 720 分钟（12 小时），更长的请用每天定时: 721":        {Type: "command", Every: 721, Args: "say hi"},
		"定时命令不能为空":                                 {Type: "command", Every: 5},
		"文件夹名最多 32 个字: " + strings.Repeat("档", 33): {Type: "backup", Every: 60, Group: strings.Repeat("档", 33)},
		"文件夹名不能有换行之类的控制字符":                         {Type: "backup", Every: 60, Group: "存\n档"},
	} {
		if _, err := m.PatchPolicies("t", PolicyPatch{Remove: []Schedule{schedY}, Add: []Schedule{s}}); err == nil || err.Error() != want {
			t.Errorf("%+v 应报「%s」：%v", s, want, err)
		}
	}
	if p := savedPolicies(t, i); !slices.Equal(p.Schedules, []Schedule{schedY}) {
		t.Errorf("不合格的改动应整次不改：%+v", p.Schedules)
	}
	// 合格的：最短、最长正好卡在边上的都收；文件夹名去掉首尾空白，32 个字正好
	ok := []Schedule{
		{Type: "backup", Every: 30, Group: " 存档 "},
		{Type: "command", Every: 5, Args: "kill @e[type=item]", Group: "扫地"},
		{Type: "restart", Every: 720},
		{Type: "backup", At: "4:30", Group: strings.Repeat("档", 32)},
	}
	got, err := m.PatchPolicies("t", PolicyPatch{Add: ok})
	if err != nil {
		t.Fatal(err)
	}
	want := []Schedule{schedY,
		{Type: "backup", Every: 30, Group: "存档"},
		{Type: "command", Every: 5, Args: "kill @e[type=item]", Group: "扫地"},
		{Type: "restart", Every: 720},
		{Type: "backup", At: "04:30", Group: strings.Repeat("档", 32)},
	}
	if !slices.Equal(got.Schedules, want) || !slices.Equal(savedPolicies(t, i).Schedules, want) {
		t.Errorf("存进去的：%+v，应为 %+v", got.Schedules, want)
	}
	if ok[0].Group != " 存档 " {
		t.Error("Apply 不该改传进来的切片")
	}
}

// 按条删认的「同一条」把间隔和文件夹也算进去：不带文件夹的删不掉文件夹里的，间隔不一样的不算，
// 挪文件夹 = 删掉原处那条、在新文件夹里加一条。文件夹名两边都去掉首尾空白再比。
func TestPolicyPatchRemoveEveryGroup(t *testing.T) {
	hourly := Schedule{Type: "backup", Every: 60, Group: "存档"}
	sweep := Schedule{Type: "command", Every: 60, Args: "kill @e[type=item]", Group: "扫地"}
	daily := Schedule{Type: "backup", At: "04:00"}
	dailyIn := Schedule{Type: "backup", At: "04:00", Group: "存档"}
	cur := Policies{Schedules: []Schedule{hourly, sweep, daily, dailyIn}}
	cases := []struct {
		name   string
		remove []Schedule
		want   []Schedule
	}{
		{"不带文件夹的删不掉文件夹里的", []Schedule{{Type: "backup", Every: 60}}, cur.Schedules},
		{"间隔不一样的不算", []Schedule{{Type: "backup", Every: 30, Group: "存档"}}, cur.Schedules},
		{"文件夹不一样的不算", []Schedule{{Type: "command", Every: 60, Args: "kill @e[type=item]", Group: "存档"}}, cur.Schedules},
		{"带时刻的不认作每隔多久的", []Schedule{{Type: "backup", At: "04:00", Every: 60, Group: "存档"}}, cur.Schedules},
		{"完全一样的删掉", []Schedule{hourly}, []Schedule{sweep, daily, dailyIn}},
		{"文件夹名带空白也认", []Schedule{{Type: "command", Every: 60, Args: "kill @e[type=item]", Group: " 扫地 "}}, []Schedule{hourly, daily, dailyIn}},
		{"同一时刻、一份在文件夹里一份不在：删不带文件夹的那份", []Schedule{{Type: "backup", At: "4:00"}}, []Schedule{hourly, sweep, dailyIn}},
		{"同一时刻、一份在文件夹里一份不在：删文件夹里的那份", []Schedule{dailyIn}, []Schedule{hourly, sweep, daily}},
	}
	for _, c := range cases {
		got, err := PolicyPatch{Remove: c.remove}.Apply(cur)
		if err != nil {
			t.Errorf("%s：不该报错：%v", c.name, err)
			continue
		}
		if !slices.Equal(got.Schedules, c.want) {
			t.Errorf("%s：%+v，应为 %+v", c.name, got.Schedules, c.want)
		}
	}
	// 两个窗口：A 把每小时存档挪进「备份」文件夹；B 还是旧的样子（在「存档」里），把它删掉——找不到不算错，A 挪过去的那条还在
	m, i := policyEnv(t)
	if err := m.UpdatePolicies("t", cur); err != nil {
		t.Fatal(err)
	}
	moved := hourly
	moved.Group = "备份"
	if _, err := m.PatchPolicies("t", PolicyPatch{Remove: []Schedule{hourly}, Add: []Schedule{moved}}); err != nil { // 窗口 A
		t.Fatal(err)
	}
	if _, err := m.PatchPolicies("t", PolicyPatch{Remove: []Schedule{hourly}}); err != nil { // 窗口 B
		t.Fatal(err)
	}
	if p := savedPolicies(t, i); !slices.Equal(p.Schedules, []Schedule{sweep, daily, dailyIn, moved}) {
		t.Errorf("挪进「备份」的那条应还在：%+v", p.Schedules)
	}
}

// 老配置（只有 at）照旧读得进来、存回去一个字不差；每隔多久的不写 at。
func TestScheduleJSONCompat(t *testing.T) {
	var old Policies
	if err := json.Unmarshal([]byte(`{"crashRestart":true,"schedules":[{"type":"restart","at":"04:00"},{"type":"command","at":"9:05","args":"say hi"}]}`), &old); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(old.Schedules, []Schedule{{Type: "restart", At: "04:00"}, {Type: "command", At: "9:05", Args: "say hi"}}) {
		t.Fatalf("老配置读进来：%+v", old.Schedules)
	}
	for s, want := range map[Schedule]string{
		{Type: "restart", At: "04:00"}:                                        `{"type":"restart","at":"04:00"}`,
		{Type: "command", At: "20:00", Args: "say hi"}:                        `{"type":"command","at":"20:00","args":"say hi"}`,
		{Type: "backup", Every: 60, Group: "存档"}:                              `{"type":"backup","every":60,"group":"存档"}`,
		{Type: "command", Every: 60, Args: "kill @e[type=item]", Group: "扫地"}: `{"type":"command","every":60,"args":"kill @e[type=item]","group":"扫地"}`,
	} {
		b, _ := json.Marshal(s)
		if string(b) != want {
			t.Errorf("%+v 存成 %s，应为 %s", s, b, want)
		}
	}
}

// 每隔多久的任务执行以后，操作记录里写间隔（没有时刻）；每天定时的照旧写时刻（见 TestScheduleNotesAutoAction）。
func TestIntervalScheduleNotesEvery(t *testing.T) {
	m, _ := tempInstance(t)
	var got []AutoAction
	m.SetAutoHook(func(a AutoAction) { got = append(got, a) })
	m.execSchedule(m.insts["s"], Schedule{Type: "backup", Every: 60, Group: "存档"})
	if len(got) != 1 {
		t.Fatalf("应记一条：%+v", got)
	}
	a := got[0]
	if _, hasAt := a.Args["at"]; a.Op != "instance.backup.create" || a.Err != nil || a.Args["every"] != 60 || hasAt || a.Args["inst"] != "s" {
		t.Fatalf("记录不对：%+v", a)
	}
}
