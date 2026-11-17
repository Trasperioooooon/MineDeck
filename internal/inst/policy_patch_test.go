package inst

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// policyEnv 只有一台停着的服务器「t」的 Manager，存盘写进临时目录。
func policyEnv(t *testing.T) (*Manager, *Instance) {
	t.Helper()
	i := &Instance{Settings: Settings{Name: "t"}, Dir: t.TempDir(), Console: newConsole()}
	return &Manager{insts: map[string]*Instance{"t": i}, creating: map[string]bool{}}, i
}

// savedPolicies 读盘上 instance.json 里存的运维策略。
func savedPolicies(t *testing.T, i *Instance) Policies {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(i.Dir, "instance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s.Policies
}

var (
	schedY = Schedule{Type: "restart", At: "04:00"}
	schedX = Schedule{Type: "backup", At: "03:00"}
	schedZ = Schedule{Type: "command", At: "05:00", Args: "say hi"}
)

// 两个窗口同时开着同一台服务器的定时任务：A 先加了一条 X 并存下；B 还是 A 改之前的样子（只有 Y），
// 删掉 Y、加上 Z、打开崩溃重启，只发这几处改动。A 加的 X 不能被 B 冲掉，B 的改动也都在（2026-11-17）。
func TestPolicyPatchTwoWindows(t *testing.T) {
	m, i := policyEnv(t)
	if err := m.UpdatePolicies("t", Policies{Schedules: []Schedule{schedY}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PatchPolicies("t", PolicyPatch{Add: []Schedule{schedX}}); err != nil { // 窗口 A
		t.Fatal(err)
	}
	on := true
	got, err := m.PatchPolicies("t", PolicyPatch{CrashRestart: &on, Remove: []Schedule{schedY}, Add: []Schedule{schedZ}}) // 窗口 B
	if err != nil {
		t.Fatal(err)
	}
	want := Policies{CrashRestart: true, Schedules: []Schedule{schedX, schedZ}}
	for name, p := range map[string]Policies{"返回的": got, "内存里的": i.PoliciesSnapshot(), "盘上的": savedPolicies(t, i)} {
		if p.CrashRestart != want.CrashRestart || !slices.Equal(p.Schedules, want.Schedules) {
			t.Errorf("%s应是 %+v：%+v", name, want, p)
		}
	}
	// 交出去的和存着的不共用一块内存
	got.Schedules[0].Args = "改了返回值"
	if i.PoliciesSnapshot().Schedules[0].Args != "" {
		t.Error("改返回值动到了存着的任务")
	}
}

// 只拨崩溃重启：任务表一条不动（以前不给 schedules 等于清空）；存着的老任务（没补零、手改坏的）也原样保留，不挡这次保存。
func TestPolicyPatchOnlyCrashRestart(t *testing.T) {
	m, i := policyEnv(t)
	old := []Schedule{{Type: "restart", At: "9:05"}, {Type: "dance", At: "x"}, schedZ}
	i.Policies = Policies{Schedules: slices.Clone(old)}
	on := true
	if _, err := m.PatchPolicies("t", PolicyPatch{CrashRestart: &on}); err != nil {
		t.Fatal(err)
	}
	if p := savedPolicies(t, i); !p.CrashRestart || !slices.Equal(p.Schedules, old) {
		t.Errorf("只改崩溃重启，任务表应原样：%+v", p)
	}
	off := false
	if _, err := m.PatchPolicies("t", PolicyPatch{CrashRestart: &off}); err != nil {
		t.Fatal(err)
	}
	if p := i.PoliciesSnapshot(); p.CrashRestart || !slices.Equal(p.Schedules, old) {
		t.Errorf("关掉崩溃重启：%+v", p)
	}
	// 什么都不给：什么都不改
	if _, err := m.PatchPolicies("t", PolicyPatch{}); err != nil {
		t.Fatal(err)
	}
	if p := i.PoliciesSnapshot(); p.CrashRestart || !slices.Equal(p.Schedules, old) {
		t.Errorf("空的改动：%+v", p)
	}
}

// 按条删：找不到的不算错；两条一样的删一次剩一条；时刻两边都规整成 HH:MM 再比；时刻不合法的删除项当作找不到。
func TestPolicyPatchRemove(t *testing.T) {
	cur := Policies{Schedules: []Schedule{schedY, schedY, {Type: "backup", At: "9:30"}, schedZ}}
	cases := []struct {
		name   string
		remove []Schedule
		want   []Schedule
	}{
		{"删不存在的", []Schedule{{Type: "backup", At: "23:00"}}, cur.Schedules},
		{"命令不一样不算同一条", []Schedule{{Type: "command", At: "05:00", Args: "say bye"}}, cur.Schedules},
		{"两条一样的删一条", []Schedule{schedY}, []Schedule{schedY, {Type: "backup", At: "9:30"}, schedZ}},
		{"同一项给两次删两条", []Schedule{schedY, schedY}, []Schedule{{Type: "backup", At: "9:30"}, schedZ}},
		{"4:00 写法删掉存着的补零的 04:00", []Schedule{{Type: "restart", At: "4:00"}}, []Schedule{schedY, {Type: "backup", At: "9:30"}, schedZ}},
		{"补零的删掉存着的没补零的", []Schedule{{Type: "backup", At: "09:30"}}, []Schedule{schedY, schedY, schedZ}},
		{"时刻不合法的当作找不到", []Schedule{{Type: "restart", At: "25:00"}, {Type: "restart", At: ""}}, cur.Schedules},
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
	if len(cur.Schedules) != 4 || cur.Schedules[0] != schedY {
		t.Errorf("Apply 不该改传进来的现状：%+v", cur)
	}
	// 经 Manager 存一遍：存着的 09:05，界面发来 9:05 也能删掉
	m, i := policyEnv(t)
	if err := m.UpdatePolicies("t", Policies{Schedules: []Schedule{{Type: "restart", At: "9:05"}, schedZ}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PatchPolicies("t", PolicyPatch{Remove: []Schedule{{Type: "restart", At: "9:05"}}}); err != nil {
		t.Fatal(err)
	}
	if p := savedPolicies(t, i); !slices.Equal(p.Schedules, []Schedule{schedZ}) {
		t.Errorf("9:05 的删除项应删掉存着的 09:05：%+v", p.Schedules)
	}
}

// 顺序：整张替换 → 按条删 → 按条加；加的和整张替换一样检查，时刻存成补零的；不合格时整次不改（内存和盘上都不动）。
func TestPolicyPatchOrderAndChecks(t *testing.T) {
	a, b, c := Schedule{Type: "restart", At: "01:00"}, Schedule{Type: "backup", At: "02:00"}, Schedule{Type: "command", At: "3:07", Args: "save-all"}
	add := []Schedule{c}
	got, err := PolicyPatch{Schedules: &[]Schedule{a, b}, Remove: []Schedule{a}, Add: add}.Apply(Policies{Schedules: []Schedule{schedY}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Schedule{b, {Type: "command", At: "03:07", Args: "save-all"}}; !slices.Equal(got.Schedules, want) {
		t.Errorf("先替换、再删、再加：%+v，应为 %+v", got.Schedules, want)
	}
	if add[0].At != "3:07" {
		t.Error("Apply 不该改传进来的切片")
	}

	m, i := policyEnv(t)
	if err := m.UpdatePolicies("t", Policies{CrashRestart: true, Schedules: []Schedule{schedY}}); err != nil {
		t.Fatal(err)
	}
	off := false
	for want, p := range map[string]PolicyPatch{
		"未知任务类型: dance":       {CrashRestart: &off, Remove: []Schedule{schedY}, Add: []Schedule{schedX, {Type: "dance", At: "04:00"}}},
		"时间格式应为 HH:MM: 24:00": {Remove: []Schedule{schedY}, Add: []Schedule{{Type: "backup", At: "24:00"}}},
		"定时命令不能为空":            {Remove: []Schedule{schedY}, Add: []Schedule{{Type: "command", At: "04:00"}}},
		"未知任务类型: nap":         {Schedules: &[]Schedule{{Type: "nap", At: "04:00"}}, Add: []Schedule{schedX}},
	} {
		if _, err := m.PatchPolicies("t", p); err == nil || err.Error() != want {
			t.Errorf("应报「%s」：%v", want, err)
		}
	}
	for name, p := range map[string]Policies{"内存里的": i.PoliciesSnapshot(), "盘上的": savedPolicies(t, i)} {
		if !p.CrashRestart || !slices.Equal(p.Schedules, []Schedule{schedY}) {
			t.Errorf("不合格的改动应整次不改，%s：%+v", name, p)
		}
	}
	// 老写法：两项都给的整张替换照旧，不给 schedules 等于清空
	if err := m.UpdatePolicies("t", Policies{}); err != nil {
		t.Fatal(err)
	}
	if p := savedPolicies(t, i); p.CrashRestart || len(p.Schedules) != 0 {
		t.Errorf("整张替换成空的：%+v", p)
	}
}

// 很多次保存同时到：每次都在 procMu 里读当前的、合并、存盘，各自加的一条全都在。
func TestPolicyPatchConcurrentAdds(t *testing.T) {
	m, i := policyEnv(t)
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for k := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.PatchPolicies("t", PolicyPatch{Add: []Schedule{{Type: "command", At: "06:00", Args: fmt.Sprintf("say %d", k)}}})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, p := range map[string]Policies{"内存里的": i.PoliciesSnapshot(), "盘上的": savedPolicies(t, i)} {
		if len(p.Schedules) != n {
			t.Fatalf("%s应有 %d 条：%d 条", name, n, len(p.Schedules))
		}
		for k := range n {
			if !slices.Contains(p.Schedules, Schedule{Type: "command", At: "06:00", Args: fmt.Sprintf("say %d", k)}) {
				t.Errorf("%s少了 say %d", name, k)
			}
		}
	}
}
