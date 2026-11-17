package catalog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/companion"
	"minedeck/internal/ops"
	"minedeck/internal/toolbox"
)

// 两个窗口同时开着同一页（2026-11-17）：只发改了的字段、列表按条增删，没刷新的那一边保存时不把另一边刚存的改回去。

// 伙伴的禁挖区域：AI 提出的预览按合并以后的名字列；按条加、删经 companion.settings.set 落到文件上；
// 和现有的重名报 400（原话给人看），不是「写配置失败」的 500。
func TestCompanionZonesAddRemoveOps(t *testing.T) {
	reg, _, dir := newMusicCatalog(t)
	ctx := context.Background()
	path := companion.ConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("enabled=false\nnodig.base=minecraft:overworld 0 0 0 9 9 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zone := func(name string) string {
		return `{"name":"` + name + `","dim":"minecraft:overworld","min":[100,0,100],"max":[109,9,109]}`
	}
	names := func() []string {
		t.Helper()
		c, err := companion.ReadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, z := range c.Zones {
			out = append(out, z.Name)
		}
		return out
	}

	// AI 提出「删 base、加 farm」：预览里禁挖区域改完以后是合并的结果
	ai := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	res, err := reg.Do(ctx, ai, "companion.settings.set", ops.Args(`{"inst":"模组服","removeZones":["BASE"],"addZones":[`+zone("farm")+`]}`))
	p, ok := res.(*ops.Proposal)
	if err != nil || !ok {
		t.Fatalf("应生成提案：%+v %v", res, err)
	}
	if len(p.Plan.Changes) != 1 || p.Plan.Changes[0].What != "禁挖区域" || p.Plan.Changes[0].Before != "base" || p.Plan.Changes[0].After != "farm" {
		t.Fatalf("预览的改动：%+v", p.Plan.Changes)
	}
	if err := reg.Reject(ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}, p.ID, "不改"); err != nil {
		t.Fatal(err)
	}
	// 预览时就和现有的重名：400
	if _, err := reg.Do(ctx, ai, "companion.settings.set", ops.Args(`{"inst":"模组服","addZones":[`+zone("Base")+`]}`)); ops.CodeOf(err) != 400 ||
		!strings.Contains(err.Error(), "已经有一个叫「base」的禁挖区域了") {
		t.Fatalf("预览重名：%v", err)
	}

	// 两个窗口：A 加 farm，B（没刷新）删 base、加 mine → [farm, mine]
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.5"}
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","addZones":[`+zone("farm")+`]}`)); err != nil {
		t.Fatalf("A 加 farm：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","removeZones":["base"],"addZones":[`+zone("mine")+`]}`)); err != nil {
		t.Fatalf("B 删 base、加 mine：%v", err)
	}
	if got := names(); !slices.Equal(got, []string{"farm", "mine"}) {
		t.Fatalf("两个窗口先后保存以后：%v", got)
	}
	// 再加一个 Farm：400，原话，不是「写伙伴模组的配置失败」；文件不动
	before, _ := os.ReadFile(path)
	_, err = reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","addZones":[`+zone("Farm")+`]}`))
	if ops.CodeOf(err) != 400 || err.Error() != "已经有一个叫「farm」的禁挖区域了（可能是刚在别的窗口加的），换个名字" {
		t.Fatalf("重名：%v（%d）", err, ops.CodeOf(err))
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("重名时不该写文件")
	}
	// addZones 里的坐标少写一个：400，和 zones 一样
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","addZones":[{"name":"x","dim":"minecraft:overworld","min":[0,0],"max":[9,9,9]}]}`)); ops.CodeOf(err) != 400 ||
		!strings.Contains(err.Error(), "三个坐标") {
		t.Fatalf("坐标少写：%v", err)
	}
	// 只删：删掉一个、删一个没有的不算错
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","removeZones":["MINE","nope"]}`)); err != nil {
		t.Fatalf("只删：%v", err)
	}
	if got := names(); !slices.Equal(got, []string{"farm"}) {
		t.Fatalf("删 mine 以后：%v", got)
	}
}

// 工具箱整合包草稿：toolbox.mrpack.draft.save 只改传了的字段；存好的草稿读不出来时报 500、不写。
func TestToolboxDraftSaveOnlyGivenFields(t *testing.T) {
	reg, _, _ := newMusicCatalog(t)
	ctx := context.Background()
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	if _, err := reg.Do(ctx, ui, "toolbox.mrpack.draft.save", ops.Args(`{"name":"A 的名字","loaderVersion":"0.16.9"}`)); err != nil { // 窗口 A
		t.Fatalf("A 改名字：%v", err)
	}
	out, err := reg.Do(ctx, ui, "toolbox.mrpack.draft.save", ops.Args(`{"summary":"B 的简介"}`)) // 窗口 B
	if err != nil {
		t.Fatalf("B 改简介：%v", err)
	}
	want := toolbox.DraftConfig{Name: "A 的名字", VersionID: "1.0.0", Summary: "B 的简介", Core: "fabric", MC: "1.20.1", LoaderVersion: "0.16.9"}
	if got, ok := out.(toolbox.DraftConfig); !ok || got != want {
		t.Fatalf("返回的整份：%+v", out)
	}
	if got, err := reg.Do(ctx, ui, "toolbox.mrpack.draft.get", ops.Args(`{}`)); err != nil || got.(toolbox.DraftConfig) != want {
		t.Fatalf("读回来：%+v %v", got, err)
	}
	// 存好的草稿读不出来（同名的是个文件夹）：500，不写
	path := filepath.Join(app.Base, "data", "toolbox", "mrpack-draft.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Do(ctx, ui, "toolbox.mrpack.draft.save", ops.Args(`{"name":"x"}`)); ops.CodeOf(err) != 500 {
		t.Fatalf("读不出来时应报 500：%v（%d）", err, ops.CodeOf(err))
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Fatalf("读不出来时不该写：%v", err)
	}
}
