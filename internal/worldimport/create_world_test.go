package worldimport

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	wt "minedeck/internal/worldimport/worldtest"
)

// 用已有地图新建服务器（placeForCreate）。

// placeNew 按 src 这张地图生成新建用的计划、收下，放进一个新的服务器文件夹（server.properties 是 props）。返回计划和文件夹。
func (e *env) placeNew(src, name, props string) (*Plan, string) {
	e.t.Helper()
	sc, err := e.s.ScanPath(me, src)
	if err != nil {
		e.t.Fatal(err)
	}
	p, err := e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, NewInst: true, Core: "vanilla", MC: "1.20.4"})
	if err != nil || p.Blocked {
		e.t.Fatalf("plan: %v %v", err, codes(p.Issues))
	}
	if err := e.s.CheckCreatePlan(me, p.ID, "vanilla", "1.20.4", true); err != nil {
		e.t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "fresh-"+name)
	write(e.t, dir, map[string]string{"server.properties": props})
	task, ctx := e.m.Tasks.New("test", []string{"x"})
	if err := e.s.placeForCreate(ctx, task, me, p.ID, name, dir); err != nil {
		e.t.Fatal(err)
	}
	return p, dir
}

func propOf(t *testing.T, dir, key string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	return inst.JavaProps(b)[key]
}

// 地图是极限模式的：新建向导的核对里说一句，新服务器的 server.properties 里打开 hardcore（A53）。
// 替换已有服务器的地图不在这里管（那边是由人勾的设置建议）。不是极限模式的地图不动 hardcore。
func TestCreateWithHardcoreMap(t *testing.T) {
	e := newEnv(t, "")
	hard := filepath.Join(e.tmp, "hard")
	lvl := lv("1.20.4", 3700)
	lvl.Hardcore = true
	if err := wt.Legacy(hard, wt.WorldOpts{Level: lvl, Tag: "H", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	if rp := e.plan(hard); slices.Contains(codes(rp.Issues), "hardcore") {
		t.Fatalf("替换已有服务器的计划不该有这一条：%v", codes(rp.Issues))
	}
	p, dir := e.placeNew(hard, "极限服", "server-port=25570\nhardcore=false\n")
	if !slices.Contains(codes(p.Issues), "hardcore") {
		t.Fatalf("新建的核对里应说一句极限模式：%v", codes(p.Issues))
	}
	if got := propOf(t, dir, "hardcore"); got != "true" {
		t.Fatalf("新服务器应打开极限模式：hardcore=%q", got)
	}
	if got := propOf(t, dir, "server-port"); got != "25570" {
		t.Fatalf("别的设置不该动：server-port=%q", got)
	}

	plain := filepath.Join(e.tmp, "plain")
	if err := wt.Legacy(plain, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "P", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p, dir = e.placeNew(plain, "普通服", "server-port=25571\nhardcore=false\n")
	if slices.Contains(codes(p.Issues), "hardcore") || propOf(t, dir, "hardcore") != "false" {
		t.Fatalf("不是极限模式的地图：%v hardcore=%q", codes(p.Issues), propOf(t, dir, "hardcore"))
	}
}

// 放地图的那条记录里记着新服务器的名字（A54）：以前计划是检测地图时生成的，那时还没有名字，记录里是空的。
func TestCreateRecordsServerName(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "m")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "N", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	_, dir := e.placeNew(src, "朋友的新服", "server-port=25570\n")
	js, bad := listJournals(dir)
	if len(bad) != 0 || len(js) != 1 || js[0].State != TxCommitted {
		t.Fatalf("记录：%+v %+v", js, bad)
	}
	if js[0].Inst != "朋友的新服" {
		t.Fatalf("记录里的服务器名：%q", js[0].Inst)
	}
}
