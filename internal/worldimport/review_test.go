package worldimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	wt "minedeck/internal/worldimport/worldtest"
)

// 2026-11-13 跨家族复审（Gemini 4 Argon，对 9e824c5）查出来的问题，每条一个回归测试。

// 第 1 条：记录读不出来（文件坏了，或者内容通不过核对）时，启动时的恢复不能把启动封锁解掉，
// 「地图 / 世界」页也要把它列出来。
// 复审第二轮（对 4a0ae14）补的：写着「已提交」的坏记录也要封锁，封锁的说明要说记录读不出来，不是「正在恢复」。
func TestUnreadableJournalKeepsBlock(t *testing.T) {
	for _, c := range []struct {
		name, body string
		loadBlocks bool // 加载实例时就封上（步骤路径要 worldimport 才查得出来，那种在启动恢复时封）
	}{
		{"坏的 JSON", `{"id":"wbad","state":"committ`, true},
		{"路径对不上", `{"id":"wbad","state":"committing","levelName":"world","oldOut":[{"from":"world","to":"../../elsewhere"}]}`, true},
		{"编号对不上", `{"id":"wother","state":"committing","levelName":"world"}`, true},
		{"世界名在控制目录里", `{"id":"wbad","state":"rolling-back","levelName":".minedeck"}`, true},
		{"写着已提交但编号对不上", `{"id":"wother","state":"committed","levelName":"world"}`, true},
		{"写着暂存但世界名不安全", `{"id":"wbad","state":"staged","levelName":"../other"}`, true},
		{"状态不认识", `{"id":"wbad","state":"","levelName":"world"}`, true},
		{"写着已提交但路径对不上", `{"id":"wbad","state":"committed","levelName":"world","oldOut":[{"from":"world","to":"../../elsewhere"}]}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, "")
			write(t, txAbs(e.dir, "wbad"), map[string]string{"journal.json": c.body, "old/world/level.dat": "旧世界的一部分"})
			m2, err := inst.NewManager()
			if err != nil {
				t.Fatal(err)
			}
			if c.loadBlocks && m2.WorldBusy("生存服") == "" {
				t.Fatal("加载时没有封锁")
			}
			s2 := NewService(m2)
			s2.RecoverAll()
			if why := m2.WorldBusy("生存服"); !strings.Contains(why, "读不出来") {
				t.Fatalf("读不出来的记录还在，封锁是 %q", why)
			}
			if err := m2.Start("生存服"); err == nil {
				t.Fatal("封锁时还能启动")
			}
			st, err := s2.Status("生存服")
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Points) != 1 || st.Points[0].State != TxUnreadable || st.Points[0].CanReturn || len(st.Points[0].Stuck) != 1 {
				t.Fatalf("points = %+v", st.Points)
			}
			if err := s2.Retry("生存服"); err == nil || !strings.Contains(err.Error(), "读不出来") {
				t.Fatalf("重试以后也不该解除：%v", err)
			}
			if !exists(filepath.Join(txAbs(e.dir, "wbad"), "old", "world", "level.dat")) {
				t.Fatal("恢复点里的文件被动了")
			}
		})
	}
}

// 第 3 条：复制完重新读来源时读不了（文件夹没了），要当成来源变了，不能拿旧清单比。
func TestRescanFailsWhenSourceGone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "map")
	if err := wt.Legacy(dir, wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	src, err := openFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := &candInfo{roots: []string{""}}
	if _, err := rescan(src, info); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if files, err := rescan(src, info); err == nil {
		t.Fatalf("来源没了，rescan 还给了 %d 个文件", len(files))
	}
}

// importFolder 把一张标记为 tag 的 1.20.4 地图导入「生存服」，等它做完，返回这次的记录编号。
func (e *env) importFolder(tag string) string {
	e.t.Helper()
	src := filepath.Join(e.tmp, tag)
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: tag, Overworld: true}); err != nil {
		e.t.Fatal(err)
	}
	p := e.plan(src)
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, msg := e.wait(id); msg != "" {
		e.t.Fatal(msg)
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) == 0 {
		e.t.Fatalf("status: %v", err)
	}
	return st.Points[0].ID
}

func (e *env) restore(txID string) error {
	e.t.Helper()
	_, err := e.restoreRes(txID)
	return err
}

// restoreRes 换回并返回任务结果（给人看的那句话）。
func (e *env) restoreRes(txID string) (string, error) {
	e.t.Helper()
	id, err := e.s.Restore("生存服", txID, "restore")
	if err != nil {
		return "", err
	}
	res, msg := e.wait(id)
	if msg != "" {
		e.t.Fatal(msg)
	}
	return res, nil
}

func canReturn(st *WorldStatus) map[string]bool {
	out := map[string]bool{}
	for _, p := range st.Points {
		out[p.ID] = p.CanReturn
	}
	return out
}

// 第 8 条：导入过两次，只能先换回最近那次，再往前换；乱序换回会把中间那张图困在恢复点里。
// 第 9 条：换回的提示只在真的核对过 level.dat 时才说核对一致。
func TestRestoreLIFO(t *testing.T) {
	e := newEnv(t, "")
	w1 := e.importFolder("A")
	time.Sleep(5 * time.Millisecond)
	w2 := e.importFolder("B")
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:B" {
		t.Fatalf("两次导入以后 = %s", got)
	}
	st, _ := e.s.Status("生存服")
	if cr := canReturn(st); !cr[w2] || cr[w1] {
		t.Fatalf("canReturn = %v（应只有最近的 %s）", cr, w2)
	}
	if _, err := e.s.Restore("生存服", w1, "restore"); err == nil || !strings.Contains(err.Error(), "最近") {
		t.Fatalf("乱序换回：%v", err)
	}
	res, err := e.restoreRes(w2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "核对一致") { // 旧世界有 level.dat，核对过
		t.Fatalf("换回的提示 = %q", res)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:A" {
		t.Fatalf("换回最近一次以后 = %s", got)
	}
	st, _ = e.s.Status("生存服")
	if cr := canReturn(st); !cr[w1] || cr[w2] {
		t.Fatalf("换回一次以后 canReturn = %v", cr)
	}
	if err := e.restore(w1); err != nil {
		t.Fatal(err)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("换回两次以后 = %s", got)
	}
}

// 第 4 条：导入前没有世界（新建服务器时放的地图）的记录不能「换回」：那只会把现在的世界移走。
func TestRestoreNeedsOldWorld(t *testing.T) {
	e := newEnv(t, "")
	id := "wnew"
	if err := saveJournal(e.dir, &Journal{ID: id, Inst: "生存服", State: TxCommitted, Created: time.Now(), LevelName: "world",
		NewIn: []Step{{From: txRel(id) + "/new/world", To: "world"}}}); err != nil {
		t.Fatal(err)
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) != 1 || st.Points[0].HasOld || st.Points[0].CanReturn {
		t.Fatalf("status = %+v %v", st, err)
	}
	if _, err := e.s.Restore("生存服", id, "restore"); err == nil {
		t.Fatal("没有旧世界也能换回")
	}
	j, _ := loadJournal(e.dir, id)
	if _, err := restoreTx(e.dir, j); err == nil {
		t.Fatal("restoreTx 没拦")
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("现在的世界被动了：%s", got)
	}
}

// 第 6 条：建服请求收下计划以后，计划过期、检测结果过期、用户放弃检测，都不影响之后放地图；
// 同一份计划不能给两个建服请求用；建服结束以后才删上传的文件。
func TestCreateClaimPinsUpload(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "m")
	if err := wt.Modern(src, wt.WorldOpts{Level: lv("26.1", 4800), Tag: "NEW", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(UploadDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(UploadDir(), "u1.upload")
	if err := wt.ZipTree(up, src, "地图/", wt.ZipOpts{}); err != nil {
		t.Fatal(err)
	}
	sc, err := e.s.ScanUpload(me, up, "map.zip")
	if err != nil || len(sc.Candidates) != 1 {
		t.Fatalf("scan: %v", err)
	}
	newPlan := func() *Plan {
		p, err := e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, NewInst: true, Core: "paper", MC: "26.1"})
		if err != nil || p.Blocked {
			t.Fatalf("plan: %v", err)
		}
		return p
	}

	// 建服没能开始（重名之类）：放掉以后计划还能用
	p0 := newPlan()
	if err := e.s.CheckCreatePlan(me, p0.ID, "paper", "26.1", true); err != nil {
		t.Fatal(err)
	}
	e.s.ReleaseCreate(me, p0.ID)
	if err := e.s.CheckCreatePlan(me, p0.ID, "paper", "26.1", true); err != nil {
		t.Fatalf("放掉以后再用：%v", err)
	}
	if err := e.s.CheckCreatePlan(me, p0.ID, "paper", "26.1", true); err == nil {
		t.Fatal("同一份计划给了两个建服请求")
	}
	e.s.ReleaseCreate("lan|192.168.1.5", p0.ID) // 别人放不掉
	if err := e.s.CheckCreatePlan(me, p0.ID, "paper", "26.1", true); err == nil {
		t.Fatal("别人把计划放掉了")
	}

	// 下载核心、跑安装器花了很久，期间用户关了向导（放弃检测），计划和检测结果都过期了
	e.s.Discard(me, sc.ID)
	base := time.Now()
	e.s.now = func() time.Time { return base.Add(150 * time.Minute) }
	e.s.sweep()
	if !exists(up) {
		t.Fatal("建服还没结束，上传的文件被删了")
	}
	newDir := filepath.Join(e.tmp, "fresh")
	write(t, newDir, map[string]string{"server.properties": "server-port=25570\n"})
	task, ctx := e.m.Tasks.New("test", []string{"x"})
	if err := e.s.placeForCreate(ctx, task, me, p0.ID, newDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := wt.ReadMarker(filepath.Join(newDir, "world", "dimensions", "minecraft", "overworld", "region", "r.0.0.mca")); got != "MARK:minecraft:overworld:NEW" {
		t.Fatalf("放进去的世界 = %q", got)
	}
	e.s.ReleaseCreate(me, p0.ID)
	if exists(up) {
		t.Fatal("建服结束、检测也放弃了，上传的文件应该删掉")
	}
	if err := e.s.CheckCreatePlan(me, p0.ID, "paper", "26.1", true); err == nil {
		t.Fatal("放过地图的计划又能用了")
	}
}

// 第 10 条：只有发起导入的人能取消。
func TestCancelOwner(t *testing.T) {
	e := newEnv(t, "")
	called := false
	e.s.mu.Lock()
	e.s.cancels["t1"] = cancelEntry{owner: me, fn: func() { called = true }}
	e.s.mu.Unlock()
	if e.s.Cancel("lan|192.168.1.5", "t1") || called {
		t.Fatal("别人取消了导入")
	}
	if !e.s.Cancel(me, "t1") || !called {
		t.Fatal("发起的人取消不了")
	}
}

// 第 7 条：level-name 写成 ./world 时，导入留下的记录要能读回来、能换回。
func TestImportWithUncleanLevelName(t *testing.T) {
	e := newEnv(t, "")
	write(t, e.dir, map[string]string{"server.properties": "level-name=./world\ndifficulty=easy\nonline-mode=true\n"})
	w := e.importFolder("A")
	st, _ := e.s.Status("生存服")
	if len(st.Points) != 1 || st.Points[0].State != TxCommitted || !st.Points[0].CanReturn || st.LevelName != "world" {
		t.Fatalf("status = %+v", st)
	}
	if err := e.restore(w); err != nil {
		t.Fatal(err)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("换回以后 = %s", got)
	}
}

// 第 1 条的补充：记录文件在却读不了（这里用同名文件夹模拟被占用、没权限），也要封锁；
// 给人看的说明里不带本机路径。
func TestJournalReadErrorKeepsBlock(t *testing.T) {
	e := newEnv(t, "")
	if err := os.MkdirAll(filepath.Join(txAbs(e.dir, "wlock"), "journal.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	m2, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if m2.WorldBusy("生存服") == "" {
		t.Fatal("加载时没有封锁")
	}
	s2 := NewService(m2)
	s2.RecoverAll()
	if m2.WorldBusy("生存服") == "" {
		t.Fatal("读不了的记录还在，封锁却被解除了")
	}
	st, _ := s2.Status("生存服")
	if len(st.Points) != 1 || st.Points[0].State != TxUnreadable {
		t.Fatalf("points = %+v", st.Points)
	}
	if strings.Contains(strings.ToLower(st.Points[0].Err), strings.ToLower(filepath.Base(e.dir))+string(filepath.Separator)) ||
		strings.Contains(st.Points[0].Err, app.Base) {
		t.Fatalf("说明里带了本机路径：%s", st.Points[0].Err)
	}
}

// 第 5 条在同一台服务器上的情况：一个导入还在复制（暂存在占位之外进行）时点「重试恢复」，
// 不能把它的暂存区当成上次留下的清掉。
func TestRetrySkipsActiveStaging(t *testing.T) {
	e := newEnv(t, "")
	id := "wlive"
	stage := filepath.Join(txAbs(e.dir, id), stageSub)
	write(t, stage, map[string]string{"world/level.dat": "复制到一半"})
	if err := saveJournal(e.dir, &Journal{ID: id, Inst: "生存服", State: TxStaging, Created: time.Now(), LevelName: "world"}); err != nil {
		t.Fatal(err)
	}
	done := e.s.setActive(txAbs(e.dir, id))
	if err := e.s.Retry("生存服"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(stage, "world", "level.dat")) {
		t.Fatal("正在进行的导入的暂存区被清掉了")
	}
	if j, _ := loadJournal(e.dir, id); j.State != TxStaging {
		t.Fatalf("state = %s", j.State)
	}
	done()
	// 导入任务已经不在了（比如程序在复制时崩了又没重启）：照常清掉
	if err := e.s.Retry("生存服"); err != nil {
		t.Fatal(err)
	}
	if exists(stage) {
		t.Fatal("没人用的暂存区没清掉")
	}
}

// 第 9 条后半：导入前的旧世界没有 level.dat（没核对可核），换回的提示不能说核对一致。
func TestRestoreMessageWithoutLevelDat(t *testing.T) {
	e := newEnv(t, "")
	if err := os.Remove(filepath.Join(e.dir, "world", "level.dat")); err != nil {
		t.Fatal(err)
	}
	w := e.importFolder("A")
	res, err := e.restoreRes(w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res, "核对") || !strings.Contains(res, "已经换回") {
		t.Fatalf("换回的提示 = %q", res)
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Fatalf("换回以后 = %s", got)
	}
}

// 复审第二轮 3.1：替换地图的任务钉着检测结果时用户放弃了检测，它过期很久、文件也放了很久，
// 都不能删上传的文件；任务结束再删。计划不能比它用的检测结果活得久。
func TestPinnedUploadSurvivesSweep(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "m")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "NEW", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(UploadDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(UploadDir(), "u2.upload")
	if err := wt.ZipTree(up, src, "", wt.ZipOpts{}); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	e.s.now = func() time.Time { return base }
	sc, err := e.s.ScanUpload(me, up, "map.zip")
	if err != nil || len(sc.Candidates) != 1 {
		t.Fatalf("scan: %v", err)
	}

	// 快过期时做的计划会把检测结果的有效期顺延到计划过期
	e.s.now = func() time.Time { return base.Add(scanTTL - 10*time.Minute) }
	p, err := e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, Inst: "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	e.s.now = func() time.Time { return base.Add(scanTTL + 10*time.Minute) }
	if _, err := e.s.getScan(me, sc.ID); err != nil {
		t.Fatalf("计划还没过期，检测结果先过期了：%v", err)
	}
	_ = p

	// 模拟一个正在跑的替换任务钉着它（Execute 里的 r.pins++），然后用户放弃检测
	e.s.mu.Lock()
	r := e.s.scans[sc.ID]
	r.pins++
	e.s.mu.Unlock()
	e.s.Discard(me, sc.ID)
	if _, err := e.s.getScan(me, sc.ID); err == nil {
		t.Fatal("放弃以后还能用这次检测")
	}
	old := base.Add(-5 * time.Hour)
	if err := os.Chtimes(up, old, old); err != nil {
		t.Fatal(err)
	}
	e.s.now = func() time.Time { return base.Add(5 * time.Hour) }
	e.s.sweep()
	if !exists(up) {
		t.Fatal("任务还在用，上传的文件被删了")
	}
	e.s.unpin(r)
	if exists(up) {
		t.Fatal("任务结束、检测已放弃，上传的文件应该删掉")
	}
	e.s.mu.Lock()
	_, still := e.s.scans[sc.ID]
	e.s.mu.Unlock()
	if still {
		t.Fatal("检测结果没从缓存里拿掉")
	}
}

// 界面复审（Grok 4.7）带出来的两处后端：同一台服务器一次只导入一张（复制阶段还没占位，别的设备能再发起一次）；
// 世界状态分开说「启动被封锁」（重试恢复有用）和一般的忙。
func TestOneImportAtATimeAndBlocked(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "A")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "A", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	e.s.mu.Lock()
	e.s.importing["生存服"] = true // 另一个导入还在复制
	e.s.mu.Unlock()
	st, _ := e.s.Status("生存服")
	if st.Busy == "" || st.Blocked != "" {
		t.Fatalf("复制阶段：busy=%q blocked=%q", st.Busy, st.Blocked)
	}
	if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true}); err == nil || !strings.Contains(err.Error(), "另一张") {
		t.Fatalf("同一台服务器同时导入两张：%v", err)
	}
	e.s.mu.Lock()
	delete(e.s.importing, "生存服")
	e.s.mu.Unlock()
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatalf("前一个做完以后计划应该还能用：%v", err)
	}
	if _, msg := e.wait(id); msg != "" {
		t.Fatal(msg)
	}
	e.s.mu.Lock()
	left := e.s.importing["生存服"]
	e.s.mu.Unlock()
	if left {
		t.Fatal("导入做完了还登记着")
	}
	e.m.SetWorldBlock("生存服", "上次换地图没能自动恢复")
	if st, _ := e.s.Status("生存服"); st.Blocked == "" || st.Busy == "" {
		t.Fatalf("封锁时：busy=%q blocked=%q", st.Busy, st.Blocked)
	}
}
