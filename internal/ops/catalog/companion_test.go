package catalog

// AI 伙伴的操作（第七阶段）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/chatai"
	"minedeck/internal/companion"
	"minedeck/internal/companion/companiontest"
	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/ops"
)

// 权限：看状态是读操作（AI 助手直接看）；装模组、改设置、刷出、移除、全部停下是写操作：人点了就做，AI 助手只能提出、人确认；
// 游戏内（游戏内 AI、AI 伙伴自己）一个都调不了——伙伴的动作只走游戏内 AI 的工具。都不是「仅本机」，局域网的人也能管。
func TestCompanionOpsPermissions(t *testing.T) {
	reg, _, _ := newMusicCatalog(t)
	ctx := context.Background()
	avail := map[string]bool{}
	for _, op := range reg.Available(ops.Caller{Source: ops.SrcAssistant, Local: true}) {
		avail[op.Name] = true
	}
	for _, name := range []string{"companion.status", "companion.install", "companion.settings.set", "companion.spawn", "companion.despawn", "companion.stop"} {
		if !avail[name] {
			t.Errorf("助手应能用（或提出）%s", name)
		}
		for _, src := range []ops.Source{ops.SrcGameChat, ops.SrcCompanion} {
			if _, err := reg.Do(ctx, ops.Caller{Source: src, Actor: "Steve"}, name, ops.Args(`{"inst":"模组服","all":true,"name":"小方"}`)); ops.CodeOf(err) != 403 {
				t.Errorf("游戏内（%s）调 %s 应 403：%v", src, name, err)
			}
		}
		if reg.Op(name).LocalOnly {
			t.Errorf("%s 不该仅本机", name)
		}
	}
	// 助手看状态：直接执行
	ai := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	out, err := reg.Do(ctx, ai, "companion.status", ops.Args(`{"inst":"模组服"}`))
	if err != nil {
		t.Fatal(err)
	}
	v := out.(companionView)
	if !v.Install.Supported || v.Install.Ready || len(v.Install.Mods) != 3 || v.Link.State != "off" || len(v.Client) != 3 || v.GameAI.Prefix != "@AI" {
		t.Errorf("状态：%+v", v)
	}
	// 助手提出装模组：只生成提案（不下载），预览写明要下载、要装什么
	res, err := reg.Do(ctx, ai, "companion.install", ops.Args(`{"inst":"模组服"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := res.(*ops.Proposal)
	if !ok || !strings.Contains(p.Plan.Summary, "要从 Modrinth 下载") || len(p.Plan.Changes) != 3 || p.Plan.Pre["实例状态"] != "stopped" {
		t.Fatalf("装模组应生成提案：%+v", res)
	}
	if err := reg.Reject(ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}, p.ID, "不装"); err != nil {
		t.Fatal(err)
	}
	// 助手提出改设置：预览列出改动
	res, err = reg.Do(ctx, ai, "companion.settings.set", ops.Args(`{"inst":"模组服","maxCompanions":2,"zones":[{"name":"base","dim":"minecraft:overworld","min":[0,0,0],"max":[9,9,9]}]}`))
	if p, ok := res.(*ops.Proposal); err != nil || !ok || len(p.Plan.Changes) != 2 || p.Plan.Changes[0].What != "伙伴上限" || p.Plan.Changes[1].After != "base" {
		t.Errorf("改设置的提案：%+v %v", res, err)
	}
	// 区域的坐标少写了一个：400
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.5"}
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","zones":[{"name":"base","dim":"minecraft:overworld","min":[0,0],"max":[9,9,9]}]}`)); ops.CodeOf(err) != 400 ||
		!strings.Contains(err.Error(), "三个坐标") {
		t.Errorf("坐标少写：%v", err)
	}
	// 没装伙伴模组时不能打开；伙伴模组没连着时刷出报 409；不支持的核心说清楚
	if _, err := reg.Do(ctx, lan, "companion.settings.set", ops.Args(`{"inst":"模组服","enabled":true}`)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "先装伙伴模组") {
		t.Errorf("没装时打开：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "companion.spawn", ops.Args(`{"inst":"模组服","name":"小方"}`)); ops.CodeOf(err) != 409 {
		t.Errorf("没连着时刷出：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "companion.spawn", ops.Args(`{"inst":"模组服","name":"AI"}`)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "撞了") {
		t.Errorf("名字和前缀撞了：%v", err)
	}
	out, err = reg.Do(ctx, lan, "companion.status", ops.Args(`{"inst":"生存服"}`))
	if err != nil || out.(companionView).Install.Supported || !strings.Contains(out.(companionView).Install.Reason, "只支持") {
		t.Errorf("Paper 不支持伙伴：%+v %v", out, err)
	}
	if _, err := reg.Do(ctx, lan, "companion.install", ops.Args(`{"inst":"生存服"}`)); ops.CodeOf(err) != 400 {
		t.Errorf("Paper 装伙伴：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "companion.status", ops.Args(`{"inst":"没有这个"}`)); ops.CodeOf(err) != 404 {
		t.Errorf("实例不存在：%v", err)
	}
}

// 端到端：真的 inst.Manager 起一个假的服务器进程，假的伙伴模组。打开伙伴 → 开服后 MineDeck 自动连上 → 刷出「小方」 →
// Steve 在游戏里说「@小方 砍 3 个木头」→ 假的 AI 调 companion_collect → 伙伴模组收到 collect → 回复以小方的名义发出 →
// 伙伴模组报任务做完 → 用模板私下告诉 Steve（不再调用 AI）→ 关服时给伙伴模组发 END。
func TestCompanionEndToEnd(t *testing.T) {
	e := newGameEnv(t)
	man, err := companionjar.Load()
	if err != nil {
		t.Fatal(err)
	}
	j, _ := man.Find("neoforge", "1.21.1")
	b, _ := j.Bytes()
	if err := os.WriteFile(filepath.Join(e.dir, "mods", j.InstallName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	const tok = "0123456789abcdef0123456789abcdef"
	mod, err := companiontest.Start(0, companiontest.Options{Token: tok})
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if _, err := e.do(t, localUI, "companion.settings.set", map[string]any{"inst": "生存服", "enabled": true}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := companion.ReadConfig(e.dir)
	path := companion.ConfigPath(e.dir)
	src, _ := os.ReadFile(path)
	s := strings.Replace(strings.Replace(string(src), fmt.Sprintf("port=%d", cfg.Port), fmt.Sprintf("port=%d", mod.Port()), 1), "token="+cfg.Token, "token="+tok, 1)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.do(t, localUI, "gameai.set", map[string]any{"inst": "生存服", "enabled": true, "allowed": []string{"Steve"}}); err != nil {
		t.Fatal(err)
	}
	collect := `{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"companion_collect","arguments":` +
		jsq(`{"item":"minecraft:oak_log","count":3}`) + `}}]},"finish_reason":"tool_calls"}]}`
	e.ai.mu.Lock()
	e.ai.replies = [][]string{{collect, usageChunk}, aiText("好，我这就去砍。")}
	e.ai.mu.Unlock()
	if err := e.mgr.Start("生存服"); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		_ = e.mgr.Stop("生存服")
		i, _ := e.mgr.Get("生存服")
		deadline := time.Now().Add(15 * time.Second)
		for i.Status() != "stopped" && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if i.Status() != "stopped" {
			_ = e.mgr.Kill("生存服")
		}
	})
	status := func() companionView {
		out, err := e.do(t, localUI, "companion.status", map[string]any{"inst": "生存服"})
		if err != nil {
			t.Fatal(err)
		}
		return out.(companionView)
	}
	waitFor(t, "开服后连上伙伴模组", 20*time.Second, func() bool { return status().Link.State == "live" })
	if h := mod.Hellos(); !strings.HasPrefix(h[len(h)-1].Client, "MineDeck ") {
		t.Errorf("HELLO 的名字：%+v", h)
	}
	out, err := e.do(t, localUI, "companion.spawn", map[string]any{"inst": "生存服", "name": "小方", "owner": "Steve"})
	if err != nil || out.(*companion.Companion).Name != "小方" {
		t.Fatalf("刷出：%+v %v", out, err)
	}
	i, _ := e.mgr.Get("生存服")
	waitFor(t, "Steve 进服", 10*time.Second, func() bool { return len(i.OnlinePlayers()) == 1 })
	if err := e.mgr.Command("生存服", "fake-chat Steve @小方 砍 3 个木头"); err != nil {
		t.Fatal(err)
	}
	reply := chatai.Tellraw("@a", "[小方→Steve] ", "好，我这就去砍。", "white") // 整条只有 ASCII：中文写成 \uXXXX
	deadline := time.Now().Add(20 * time.Second)
	for found := false; !found; {
		for _, l := range e.stdinLines() {
			found = found || l == reply
		}
		if !found && time.Now().After(deadline) {
			t.Fatalf("等了 20 秒还没有以小方的名义回复。标准输入：\n%s\n控制台：\n%s\nAI 请求 %d 次", strings.Join(e.stdinLines(), "\n"),
				strings.Join(i.Console.Lines(), "\n"), len(e.ai.requests()))
		}
		time.Sleep(20 * time.Millisecond)
	}
	var req map[string]any
	for _, r := range mod.Requests() {
		if r["op"] == "collect" {
			req = r
		}
	}
	if req == nil || req["companion"] != float64(1) || req["item"] != "minecraft:oak_log" || req["count"] != float64(3) {
		t.Fatalf("伙伴模组收到的：%v", mod.Requests())
	}
	reqs := len(e.ai.requests())
	if reqs != 2 || !strings.Contains(string(e.ai.requests()[0]), `"companion_collect"`) || !strings.Contains(string(e.ai.requests()[0]), "这句话是对伙伴「小方」说的") {
		t.Errorf("AI 的请求：%d", reqs)
	}
	// 任务做完：模板通知，只发给 Steve，不调用 AI
	var taskID int64
	for _, c := range mod.Companions() {
		if task, ok := c["task"].(map[string]any); ok {
			taskID = int64(task["id"].(float64))
		}
	}
	if err := mod.Finish(taskID, "done", map[string]any{"gained": 3, "want": 3, "have": 3}); err != nil {
		t.Fatal(err)
	}
	notice, _, _ := strings.Cut(chatai.Tellraw("Steve", "[小方] ", "x", "white"), `"extra"`)
	waitFor(t, "任务通知", 10*time.Second, func() bool {
		for _, l := range e.stdinLines() {
			if strings.HasPrefix(l, notice) {
				return true
			}
		}
		return false
	})
	if n := len(e.ai.requests()); n != reqs {
		t.Errorf("任务通知不该调用 AI：%d → %d", reqs, n)
	}
	con := strings.Join(i.Console.Lines(), "\n")
	if !strings.Contains(con, "[MineDeck] 伙伴「小方」的任务通知（只发给 Steve）：采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 3 个，用时 10 秒。") {
		t.Errorf("控制台：\n%s", con)
	}
	// 操作记录：刷出（来源 ui）、动作（来源 companion）、问答（来源 gamechat，带伙伴的名字）
	recs, _ := e.reg.Log.Query(ops.Query{})
	var got []string
	for _, r := range recs {
		if strings.HasPrefix(r.Op, "companion.") || r.Op == "gameai.ask" {
			got = append(got, fmt.Sprintf("%s|%s|%s|%v", r.Op, r.Src, r.Actor, r.OK))
		}
	}
	if strings.Join(got, ",") != "gameai.ask|gamechat|Steve|true,companion.action|companion|Steve|true,companion.spawn|ui|本机|true,companion.settings.set|ui|本机|true" { // 新的在前
		t.Errorf("操作记录：%v", got)
	}
	h, _ := e.do(t, localUI, "gameai.log", map[string]any{"inst": "生存服"})
	if rs := h.([]chatai.Record); len(rs) != 2 || rs[0].Kind != "task" || rs[1].Companion != "小方" || len(rs[1].Actions) != 1 || !rs[1].Actions[0].OK {
		t.Errorf("最近的问答：%+v", rs)
	}
	v := status()
	if len(v.Companions) != 1 || v.Companions[0].LastResult == nil || v.Companions[0].LastResult.Result != "done" || len(v.Events) == 0 {
		t.Errorf("伙伴的状态：%+v", v.Companions)
	}
	// 全部停下、移除（界面上的按钮）
	if _, err := e.do(t, localUI, "companion.stop", map[string]any{"inst": "生存服", "all": true}); err != nil {
		t.Error(err)
	}
	if out, err := e.do(t, localUI, "companion.despawn", map[string]any{"inst": "生存服", "companion": 1, "drop": true}); err != nil ||
		fmt.Sprint(out.(map[string]any)["removed"]) != "[1]" {
		t.Errorf("移除：%+v %v", out, err)
	}
	// 关服：给伙伴模组发 END
	_ = e.mgr.Stop("生存服")
	waitFor(t, "关服", 15*time.Second, func() bool { return i.Status() == "stopped" })
	stopped = true
	waitFor(t, "伙伴模组收到 END", 10*time.Second, func() bool { return len(mod.Ends()) == 1 })
	if mod.Ends()[0] != "服务器已停止" {
		t.Errorf("END：%v", mod.Ends())
	}
}
