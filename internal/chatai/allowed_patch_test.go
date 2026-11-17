package chatai

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 两个窗口同时开着「游戏内 AI」：名单原来是 Alex、Steve。A 收回了 Steve 并存下；B 还是 A 改之前的样子，
// 只发「授权 Bob」。结果是 Alex、Bob：A 收回的 Steve 没被 B 授权回去，B 加的 Bob 也在；存在实例目录里，读回来一样（2026-11-17）。
func TestAllowedPatchTwoWindows(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Steve", "Alex"}})
	f.set(Patch{RemoveAllowed: []string{"Steve"}}) // 窗口 A
	v := f.set(Patch{AddAllowed: []string{"Bob"}}) // 窗口 B
	want := []string{"Alex", "Bob"}
	if !slices.Equal(v.Settings.Allowed, want) {
		t.Errorf("名单应是 %v：%v", want, v.Settings.Allowed)
	}
	g := New(Options{Backend: f.b, AI: f.svc, Ops: f.reg})
	if s, _ := g.Settings(instName); !slices.Equal(s.Allowed, want) || s.Authorized("Steve") || !s.Authorized("bob") {
		t.Errorf("读回来的名单：%v", s.Allowed)
	}
}

// 收回不分大小写、找不到的不算错；授权已经有的人（大小写不同）不重复加；名字不合格时报错、整次不改（内存和文件都不动）。
func TestAllowedPatchRules(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Alex", "Bob"}})
	if v := f.set(Patch{RemoveAllowed: []string{" ALEX ", "Nobody"}}); !slices.Equal(v.Settings.Allowed, []string{"Bob"}) {
		t.Errorf("收回不分大小写，找不到的不算错：%v", v.Settings.Allowed)
	}
	if v := f.set(Patch{AddAllowed: []string{"bob", " Carol ", "carol"}}); !slices.Equal(v.Settings.Allowed, []string{"Bob", "Carol"}) {
		t.Errorf("已经有的不重复加：%v", v.Settings.Allowed)
	}
	raw, _ := os.ReadFile(filepath.Join(f.b.dir, settingsFile))
	for _, p := range []Patch{
		{RemoveAllowed: []string{"Bob"}, AddAllowed: []string{"Dave", "@a"}},
		{RemoveAllowed: []string{"Bob"}, AddAllowed: []string{"史蒂夫"}},
		{Allowed: &[]string{"Eve"}, AddAllowed: []string{""}},
	} {
		_, err := f.gc.Update(instName, p)
		if err == nil || !strings.Contains(err.Error(), "不能授权") {
			t.Errorf("不合格的名字应报错：%+v %v", p, err)
		}
	}
	if s, _ := f.gc.Settings(instName); !slices.Equal(s.Allowed, []string{"Bob", "Carol"}) {
		t.Errorf("报错的改动不该生效：%v", s.Allowed)
	}
	if after, _ := os.ReadFile(filepath.Join(f.b.dir, settingsFile)); string(after) != string(raw) {
		t.Error("报错的改动不该写文件")
	}
}

// 纯 Apply：先整个替换、再收回、再授权；三样给了任何一样，名单都去重、排序（读进来的文件不保证有序、无重复）；合起来最多 200 人。
func TestAllowedPatchApply(t *testing.T) {
	s, err := Patch{Allowed: &[]string{"X", "Y"}, RemoveAllowed: []string{"x"}, AddAllowed: []string{"Z", "y"}}.Apply(Defaults())
	if err != nil || !slices.Equal(s.Allowed, []string{"Y", "Z"}) {
		t.Errorf("先替换、再收回、再授权：%v %v", s.Allowed, err)
	}
	cur := Defaults()
	cur.Allowed = []string{"steve", "Alex", "Steve"}
	s, err = Patch{AddAllowed: []string{"bob"}}.Apply(cur)
	if err != nil || !slices.Equal(s.Allowed, []string{"Alex", "bob", "steve"}) {
		t.Errorf("只授权一个人，名单也去重、排序：%v %v", s.Allowed, err)
	}
	if !slices.Equal(cur.Allowed, []string{"steve", "Alex", "Steve"}) {
		t.Errorf("Apply 不该改传进来的设置：%v", cur.Allowed)
	}
	var many []string
	for k := range 199 {
		many = append(many, fmt.Sprintf("p%d", k))
	}
	cur.Allowed = many
	if s, err := (Patch{AddAllowed: []string{"q1"}}).Apply(cur); err != nil || len(s.Allowed) != 200 {
		t.Errorf("正好 200 人：%d %v", len(s.Allowed), err)
	}
	if _, err := (Patch{AddAllowed: []string{"q1", "q2"}}).Apply(cur); err == nil || err.Error() != "能执行命令的玩家最多 200 个" {
		t.Errorf("201 人应报错：%v", err)
	}
	if s, err := (Patch{RemoveAllowed: []string{"p0"}, AddAllowed: []string{"q1", "q2"}}).Apply(cur); err != nil || len(s.Allowed) != 200 {
		t.Errorf("先收回一个再授权两个，正好 200 人：%d %v", len(s.Allowed), err)
	}
	// 三样都没给：名单原样，不去重、不排序
	if s, err := (Patch{Enabled: ptr(true)}).Apply(Settings{Allowed: []string{"b", "a", "A"}}); err != nil || !slices.Equal(s.Allowed, []string{"b", "a", "A"}) {
		t.Errorf("没动名单就原样：%v %v", s.Allowed, err)
	}
}
