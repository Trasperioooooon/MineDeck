package worldimport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	wt "minedeck/internal/worldimport/worldtest"
)

// env 临时程序目录里放一台停着的原版服务器「生存服」（1.20.4），旧世界三个维度各有标记 OLD。
type env struct {
	t   *testing.T
	m   *inst.Manager
	s   *Service
	dir string
	tmp string
}

const me = "ui|本机"

func newEnv(t *testing.T, core string) *env {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "s1")
	s, _ := json.Marshal(inst.Settings{Name: "生存服", Core: "vanilla", MC: "1.20.4", XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar", JavaPath: "C:\\nope\\java.exe"})
	if core != "" {
		s, _ = json.Marshal(inst.Settings{Name: "生存服", Core: "velocity", MC: "3.3.0", XmxMB: 512, XmsMB: 256})
	}
	write(t, dir, map[string]string{"instance.json": string(s), "server.properties": "level-name=world\ndifficulty=easy\nonline-mode=true\n"})
	if err := wt.Legacy(filepath.Join(dir, "world"), wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "OLD", Overworld: true, Nether: true, End: true}); err != nil {
		t.Fatal(err)
	}
	m, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	sv := NewService(m)
	sv.order = fakeOrder
	return &env{t: t, m: m, s: sv, dir: dir, tmp: t.TempDir()}
}

func (e *env) marker(rel string) string {
	got, err := wt.ReadMarker(filepath.Join(e.dir, filepath.FromSlash(rel)))
	if err != nil {
		return "<missing>"
	}
	return got
}

func (e *env) wait(id string) (result, errMsg string) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if t := e.m.Tasks.Get(id); t != nil {
			if s := t.Snapshot(); s.Ended {
				return s.Result, s.Err
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("任务没结束")
	return "", ""
}

// bukkitZip 一张旧 Paper 三目录的地图（1.20.4，标记 NEW），套一层目录打成 ZIP。
func (e *env) bukkitZip() string {
	src := filepath.Join(e.tmp, "src")
	if err := wt.LegacyBukkit(src, "MapWorld", wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "NEW", Overworld: true, Nether: true, End: true, UIDDat: true,
		Players: []string{"069a79f4-44e9-4726-a1a5-95721e2c6d2a"}}); err != nil {
		e.t.Fatal(err)
	}
	z := filepath.Join(e.tmp, "map.zip")
	if err := wt.ZipTree(z, src, "冒险地图 v1/", wt.ZipOpts{}); err != nil {
		e.t.Fatal(err)
	}
	return z
}

func (e *env) plan(path string) *Plan {
	e.t.Helper()
	sc, err := e.s.ScanPath(me, path)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(sc.Candidates) != 1 {
		e.t.Fatalf("候选 %d 个：%v", len(sc.Candidates), codes(sc.Issues))
	}
	p, err := e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, Inst: "生存服"})
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestImportReplaceAndRestore(t *testing.T) {
	e := newEnv(t, "")
	p := e.plan(e.bukkitZip())
	if p.Blocked || p.Relation != RelSame || len(p.Replace) != 1 || p.Replace[0] != "world" {
		t.Fatalf("plan: blocked=%v rel=%s replace=%v issues=%v", p.Blocked, p.Relation, p.Replace, codes(p.Issues))
	}
	// 后端自己核对确认，不靠前端
	if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID}); err == nil {
		t.Fatal("没带 confirm 也执行了")
	}
	if _, err := e.s.Execute("lan|192.168.1.5", ExecReq{PlanID: p.ID, Confirm: "replace"}); err == nil {
		t.Fatal("别人拿到计划编号也能执行")
	}
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	res, msg := e.wait(id)
	if msg != "" || !strings.Contains(res, "尚未启动检查") {
		t.Fatalf("result=%q err=%q", res, msg)
	}
	// Bukkit 的 _nether/DIM-1、_the_end/DIM1 并进了原版的 world/DIM-1、world/DIM1
	for rel, want := range map[string]string{
		"world/region/r.0.0.mca":       "MARK:minecraft:overworld:NEW",
		"world/DIM-1/region/r.0.0.mca": "MARK:minecraft:the_nether:NEW",
		"world/DIM1/region/r.0.0.mca":  "MARK:minecraft:the_end:NEW",
	} {
		if got := e.marker(rel); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	if exists(filepath.Join(e.dir, "world_nether")) || exists(filepath.Join(e.dir, "world", "session.lock")) {
		t.Error("不该有 world_nether 或 session.lock")
	}
	// 同一份计划不能用第二次
	if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true}); err == nil {
		t.Fatal("计划用了两次")
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) != 1 || !st.Points[0].CanReturn || !st.Points[0].HasOld {
		t.Fatalf("status = %+v %v", st, err)
	}
	// 换回旧图
	if _, err := e.s.Restore("生存服", st.Points[0].ID, "no"); err == nil {
		t.Fatal("没带 confirm 也换回了")
	}
	id, err = e.s.Restore("生存服", st.Points[0].ID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); msg != "" {
		t.Fatal(msg)
	}
	for rel, want := range map[string]string{
		"world/region/r.0.0.mca":       "MARK:minecraft:overworld:OLD",
		"world/DIM-1/region/r.0.0.mca": "MARK:minecraft:the_nether:OLD",
		"world/DIM1/region/r.0.0.mca":  "MARK:minecraft:the_end:OLD",
	} {
		if got := e.marker(rel); got != want {
			t.Errorf("换回后 %s = %q, want %q", rel, got, want)
		}
	}
	// 删恢复点要明确确认
	if err := e.s.DeletePoint("生存服", st.Points[0].ID, ""); err == nil {
		t.Fatal("没确认也删了")
	}
	if err := e.s.DeletePoint("生存服", st.Points[0].ID, "delete"); err != nil {
		t.Fatal(err)
	}
}

// 预览以后来源变了：不替换任何文件。
func TestStalePlan(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "folder")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "NEW", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	time.Sleep(20 * time.Millisecond)
	write(t, src, map[string]string{"region/r.1.0.mca": "changed"})
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, msg := e.wait(id); !strings.Contains(msg, "变过了") {
		t.Fatalf("err = %q", msg)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("旧世界被动了：%s", got)
	}
}

// 选了这台服务器自己的世界当来源：拦下。暂存到一半退出的，下次启动清掉暂存区、不封锁。
func TestSourceIsTargetAndStagingLeftover(t *testing.T) {
	e := newEnv(t, "")
	if p := e.plan(filepath.Join(e.dir, "world")); !p.Blocked || !strings.Contains(strings.Join(codes(p.Issues), ","), "source-is-target") {
		t.Fatalf("issues = %v", codes(p.Issues))
	}
	id := "wleft"
	write(t, filepath.Join(txAbs(e.dir, id), stageSub), map[string]string{"world/level.dat": "half"})
	if err := saveJournal(e.dir, &Journal{ID: id, State: TxStaging, LevelName: "world"}); err != nil {
		t.Fatal(err)
	}
	e.s.RecoverAll()
	if exists(filepath.Join(txAbs(e.dir, id), stageSub)) {
		t.Fatal("暂存区没清掉")
	}
	if j, _ := loadJournal(e.dir, id); j.State != TxRolledBack {
		t.Fatalf("state = %s", j.State)
	}
	if why := e.m.WorldBusy("生存服"); why != "" {
		t.Fatalf("不该封锁：%s", why)
	}
}

// 版本更新的地图拦下；代理端直接拒绝。
func TestBlocks(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "new")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.21.4", 4189), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	if !p.Blocked || p.Relation != RelNewer {
		t.Fatalf("blocked=%v rel=%s", p.Blocked, p.Relation)
	}
	if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true}); err == nil {
		t.Fatal("被拦下的计划也执行了")
	}
	px := newEnv(t, "velocity")
	sc, _ := px.s.ScanPath(me, src)
	if _, err := px.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, Inst: "生存服"}); err == nil || !strings.Contains(err.Error(), "代理端") {
		t.Fatalf("proxy: %v", err)
	}
}

// 换地图占着位时启动不了；程序在换到一半时退出，下次启动先封住启动，恢复完再放开，可以重复恢复。
func TestHoldAndRecovery(t *testing.T) {
	e := newEnv(t, "")
	release, err := e.m.BeginWorldChange("生存服", "换地图")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.Start("生存服"); err == nil || !strings.Contains(err.Error(), "换地图") {
		t.Fatalf("占位时启动：%v", err)
	}
	if _, err := e.m.CreateBackup("生存服", ""); err == nil {
		t.Fatal("占位时还能备份")
	}
	if err := e.m.Delete("生存服", true); err == nil {
		t.Fatal("占位时还能删除")
	}
	if _, ok := e.m.BeginPacking("生存服"); ok {
		t.Fatal("占位时还能打包")
	}
	if _, err := e.m.BeginWorldChange("生存服", "换地图"); err == nil {
		t.Fatal("占了两次")
	}
	release()

	// 模拟断电：旧世界已经移进恢复点，新世界还在暂存区，记录停在 committing
	id := "wcrash"
	stage := filepath.Join(txAbs(e.dir, id), stageSub)
	if err := wt.Legacy(filepath.Join(stage, "world"), wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "NEW", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	j := &Journal{ID: id, Inst: "生存服", State: TxCommitting, LevelName: "world",
		OldOut:      []Step{{From: "world", To: txRel(id) + "/old/world"}},
		NewIn:       []Step{{From: txRel(id) + "/new/world", To: "world"}},
		OldLevelSHA: fileSHA(filepath.Join(e.dir, "world", "level.dat"))}
	if err := saveJournal(e.dir, j); err != nil {
		t.Fatal(err)
	}
	if err := doStep(e.dir, j.OldOut[0]); err != nil {
		t.Fatal(err)
	}
	m2, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.Start("生存服"); err == nil || !strings.Contains(err.Error(), "没有完成") {
		t.Fatalf("没恢复就能启动：%v", err)
	}
	s2 := NewService(m2)
	s2.RecoverAll()
	s2.RecoverAll()
	if why := m2.WorldBusy("生存服"); why != "" {
		t.Fatalf("恢复后还封着：%s", why)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("恢复后的世界 = %s", got)
	}
}

// 新建服务器时放地图：放进 world，失败时整台不建由流水线负责（这里只看放得对）。
func TestPlaceForCreate(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "m")
	if err := wt.Modern(src, wt.WorldOpts{Level: lv("26.1", 4800), Tag: "NEW", Overworld: true, Nether: true}); err != nil {
		t.Fatal(err)
	}
	sc, err := e.s.ScanPath(me, src)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, NewInst: true, Core: "paper", MC: "26.1"})
	if err != nil || p.Blocked {
		t.Fatalf("plan: %v %v", err, codes(p.Issues))
	}
	if err := e.s.CheckCreatePlan(me, p.ID, "paper", "26.2", true); err == nil {
		t.Fatal("版本对不上也放行了")
	}
	if err := e.s.CheckCreatePlan(me, p.ID, "paper", "26.1", true); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(app.ServersDir, "fresh")
	write(t, newDir, map[string]string{"server.properties": "server-port=25570\n"})
	task, ctx := e.m.Tasks.New("test", []string{"x"})
	if err := e.s.placeForCreate(ctx, task, me, p.ID, newDir); err != nil {
		t.Fatal(err)
	}
	got, _ := wt.ReadMarker(filepath.Join(newDir, "world", "dimensions", "minecraft", "the_nether", "region", "r.0.0.mca"))
	if got != "MARK:minecraft:the_nether:NEW" {
		t.Fatalf("nether = %q", got)
	}
	if exists(filepath.Join(newDir, "world", "session.lock")) {
		t.Fatal("session.lock 不该带过去")
	}
	_ = os.RemoveAll(newDir)
}
