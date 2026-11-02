package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"minedeck/internal/assistant"
	"minedeck/internal/ops"
)

func toolNames(t *testing.T, r reply2) []string {
	t.Helper()
	var out []string
	tools, _ := r.result()["tools"].([]any)
	for _, x := range tools {
		out = append(out, x.(map[string]any)["name"].(string))
	}
	return out
}

func toolByName(r reply2, name string) map[string]any {
	tools, _ := r.result()["tools"].([]any)
	for _, x := range tools {
		if m := x.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

// 工具清单：和应用内助手给 AI 的操作一样（仅人工的不给，检查更新、game.info 这类也不给），再加 proposal_status；
// 写操作和危险操作的说明写明要在 MineDeck 里确认；EULA 和存放目录不在参数里；旧版本不带新字段。
func TestToolsList(t *testing.T) {
	f := newFixture(t)
	f.on()
	r := f.modern(1, "tools/list", nil)
	got := toolNames(t, r)
	var want []string
	for _, op := range assistant.ToolOps(f.reg, ops.Caller{Source: ops.SrcMCP, Local: true}) {
		want = append(want, ops.ToolName(op.Name))
	}
	want = append(want, StatusTool)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("工具清单：\n%v\n应为\n%v", got, want)
	}
	for _, bad := range []string{"instance_delete", "app_update_check", "game_info"} {
		if toolByName(r, bad) != nil {
			t.Errorf("不该给外部 AI：%s", bad)
		}
	}
	res := r.result()
	if res["resultType"] != "complete" || res["cacheScope"] != "private" || res["ttlMs"] == nil {
		t.Errorf("2026-07-28 的 tools/list：%v", res)
	}
	w := toolByName(r, "instance_props_set")
	if !strings.Contains(w["description"].(string), "在 MineDeck 的界面上生成一张确认卡片") || w["title"] != "修改 server.properties" ||
		js(w["annotations"]) != `{"destructiveHint":false,"readOnlyHint":false}` {
		t.Errorf("写操作：%v", w)
	}
	if d := toolByName(r, "instance_restart"); !strings.Contains(d["description"].(string), "还要再勾选一次") || js(d["annotations"]) != `{"destructiveHint":true,"readOnlyHint":false}` {
		t.Errorf("危险操作：%v", d)
	}
	if rd := toolByName(r, "app_info"); js(rd["annotations"]) != `{"readOnlyHint":true}` || strings.Contains(rd["description"].(string), "确认卡片") {
		t.Errorf("读操作：%v", rd)
	}
	if c := js(toolByName(r, "instance_create")["inputSchema"]); strings.Contains(c, "eula") || strings.Contains(c, "root") || !strings.Contains(c, `"name"`) {
		t.Errorf("新建实例的参数里不能有 eula 和 root：%s", c)
	}
	if st := toolByName(r, StatusTool); st == nil || got[len(got)-1] != StatusTool || !strings.Contains(js(st["inputSchema"]), `"wait"`) {
		t.Errorf("proposal_status：%v", st)
	}
	// 两次的清单逐字节一样（顺序固定）
	if again := f.modern(2, "tools/list", nil); js(again.result()["tools"]) != js(res["tools"]) {
		t.Error("工具清单应该每次都一样")
	}
	// 旧版本：2025-03-26 没有 title、有 annotations；2024-11-05 两样都没有；都不带 resultType
	_, sid := f.initialize("2025-03-26", "Old")
	old := f.post(legacyReq(3, "tools/list", nil), "Mcp-Session-Id", sid)
	if strings.Contains(old.raw, `"title"`) || !strings.Contains(old.raw, `"annotations"`) || strings.Contains(old.raw, "resultType") || strings.Contains(old.raw, "ttlMs") {
		t.Errorf("2025-03-26 的工具清单：%.300s", old.raw)
	}
	_, sid = f.initialize("2024-11-05", "Older")
	older := f.post(legacyReq(4, "tools/list", nil), "Mcp-Session-Id", sid)
	if strings.Contains(older.raw, `"annotations"`) || strings.Contains(older.raw, `"title"`) || len(toolNames(t, older)) != len(got) {
		t.Errorf("2024-11-05 的工具清单：%.300s", older.raw)
	}
}

// 读操作直接执行，结果和助手一样脱敏（路径里的用户名、局域网 IP、Key、像密码的值）、加数据边界、超过 16 KB 截断。
func TestReadToolRedaction(t *testing.T) {
	f := newFixture(t)
	tok := f.on()
	_, sid := f.initialize("2025-06-18", "Claude Desktop")
	r := f.call(sid, "app_info", map[string]any{})
	text, isErr := r.text()
	if isErr || !strings.HasPrefix(text, "【数据开始") || !strings.HasSuffix(text, "【数据结束】") {
		t.Fatalf("读操作：%s", r.raw)
	}
	for _, leak := range []string{winUser, lanIP, aiSecret, "hook-secret-77", tok} {
		if strings.Contains(text, leak) {
			t.Errorf("结果里不能有 %q：%s", leak, text)
		}
	}
	if !strings.Contains(text, "<程序目录>") || !strings.Contains(text, "<局域网IP>") || strings.Contains(text, "10.0.0.5") {
		t.Errorf("应换成占位符：%s", text)
	}
	if r := f.call(sid, "instance_props_get", map[string]any{"inst": "生存服"}); strings.Contains(r.raw, "hunter2") {
		t.Errorf("rcon.password 要写成 ***：%s", r.raw)
	}
	// 超过 16 KB：带 lines 的从最旧的删起
	for i := 0; i < 400; i++ {
		f.big = append(f.big, fmt.Sprintf("[12:00:%02d INFO] 第 %d 行 %s", i%60, i, strings.Repeat("x", 80)))
	}
	text, _ = f.call(sid, "instance_console_tail", map[string]any{"inst": "生存服"}).text()
	if len(text) > 17<<10 || !strings.Contains(text, "前面") || !strings.Contains(text, "第 399 行") || strings.Contains(text, "第 0 行 ") {
		t.Errorf("截断：%d 字节，%.200s", len(text), text)
	}
	// 参数不对、实例不存在这类：工具的错误（isError），外部 AI 能看到原因再改
	text, isErr = f.call(sid, "instance_props_get", map[string]any{}).text()
	if !isErr || !strings.Contains(text, "缺少参数 inst") {
		t.Errorf("缺参数：%v %s", isErr, text)
	}
	// 读操作不记操作记录
	if len(f.records()) != 0 {
		t.Errorf("读操作不该记录：%v", f.records())
	}
}

// 写操作：不执行，生成提案；结果告诉外部 AI 还没执行、要做什么、要用户在 MineDeck 里确认、怎么查。人确认后按存下的参数执行，
// 操作记录里来源是 mcp、actor 是客户端报的名字、approvedBy 是确认的人；proposal_status 查得到执行结果。
func TestWriteToolProposal(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-06-18", "Claude Desktop")
	args := map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "max-players", "value": "10"}}}
	text, isErr := f.call(sid, "instance_props_set", args).text()
	if isErr || !strings.Contains(text, "已生成提案 p_") || !strings.Contains(text, "还没有执行") || !strings.Contains(text, "max-players 从 20 改成 10") ||
		!strings.Contains(text, "MineDeck 的界面上确认") || !strings.Contains(text, "5 分钟内有效") || !strings.Contains(text, StatusTool) {
		t.Fatalf("提案的结果：%s", text)
	}
	if strings.Contains(text, winUser) || !strings.Contains(text, "<程序目录>") {
		t.Errorf("提案的说明也要脱敏：%s", text)
	}
	if f.count("instance.props.set") != 0 {
		t.Fatal("没确认的写操作不能执行")
	}
	pid := proposalID(t, text)
	p := f.reg.Proposal(pid)
	if p == nil || p.Caller.Source != ops.SrcMCP || p.Caller.Actor != "Claude Desktop" || !p.Caller.Local {
		t.Fatalf("提案的提出者：%+v", p)
	}
	pend := f.svc.Pending()
	if len(pend) != 1 || pend[0].ID != pid || pend[0].Actor != "Claude Desktop" || pend[0].Level != "W" || pend[0].Plan == nil {
		t.Fatalf("界面上等确认的：%+v", pend)
	}
	if st, _ := f.call(sid, StatusTool, map[string]any{"id": pid}).text(); !strings.Contains(st, "还在等用户在 MineDeck 里确认") {
		t.Errorf("等确认时：%s", st)
	}
	// 人在 MineDeck 的界面上确认
	if _, err := f.reg.ConfirmWith(context.Background(), local, pid, ops.ConfirmInput{}); err != nil {
		t.Fatal(err)
	}
	if f.count("instance.props.set") != 1 || f.lastArgs("instance.props.set") != `{"inst":"生存服","pairs":[{"key":"max-players","value":"10"}]}` {
		t.Fatalf("确认后应按提案的参数执行：%s", f.lastArgs("instance.props.set"))
	}
	recs := f.records()
	if len(recs) != 2 || recs[0].Src != ops.SrcMCP || recs[0].Actor != "Claude Desktop" || recs[0].ApprovedBy != "本机" || recs[0].Proposal != pid || recs[0].Event != "" ||
		recs[1].Event != ops.EvPropose || recs[1].Src != ops.SrcMCP {
		t.Fatalf("操作记录：%+v", recs)
	}
	st, isErr := f.call(sid, StatusTool, map[string]any{"id": pid}).text()
	if isErr || !strings.Contains(st, "用户确认了，已经执行") || !strings.Contains(st, `"result":"ok"`) || strings.Contains(st, winUser) {
		t.Errorf("执行完：%s", st)
	}
	if len(f.svc.Pending()) != 0 {
		t.Error("处理完的不再等确认")
	}
}

// 危险操作要多勾一次；EULA 只能由人在确认时勾（外部 AI 传了 eula 也被去掉）。
func TestDangerAndConsent(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-11-25", "Cursor")
	text, _ := f.call(sid, "instance_restart", map[string]any{"inst": "生存服"}).text()
	if !strings.Contains(text, "这是危险操作") || !strings.Contains(text, "在线的 2 人会掉线") {
		t.Fatalf("危险操作的提案：%s", text)
	}
	pid := proposalID(t, text)
	if _, err := f.reg.ConfirmWith(context.Background(), local, pid, ops.ConfirmInput{}); ops.CodeOf(err) != 400 || f.count("instance.restart") != 0 {
		t.Fatalf("不勾第二次确认应失败：%v", err)
	}
	if _, err := f.reg.ConfirmWith(context.Background(), local, pid, ops.ConfirmInput{Ack: true}); err != nil || f.count("instance.restart") != 1 {
		t.Fatalf("勾了第二次确认：%v", err)
	}
	if _, err := f.reg.ConfirmWith(context.Background(), local, pid, ops.ConfirmInput{Ack: true}); ops.CodeOf(err) != 404 {
		t.Fatalf("提案只能用一次：%v", err)
	}
	text, _ = f.call(sid, "instance_create", map[string]any{"name": "新服", "core": "paper", "eula": true, "root": `D:\x`}).text()
	pid = proposalID(t, text)
	p := f.reg.Proposal(pid)
	if strings.Contains(string(p.Args), "eula") || strings.Contains(string(p.Args), "root") || p.Consent == nil || !strings.Contains(text, "你不能替用户同意") {
		t.Fatalf("外部 AI 不能替用户同意 EULA：%s / %s", p.Args, text)
	}
	if _, err := f.reg.ConfirmWith(context.Background(), local, pid, ops.ConfirmInput{Consent: true}); err != nil ||
		f.lastArgs("instance.create") != `{"core":"paper","eula":true,"name":"新服"}` {
		t.Fatalf("人勾了同意之后：%v %s", err, f.lastArgs("instance.create"))
	}
}

// proposal_status：没有的编号是工具错误；wait 等到提案处理完马上返回；不填 id 列出最近的；拒绝的理由交给外部 AI。
func TestProposalStatus(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-06-18", "Claude Desktop")
	if text, isErr := f.call(sid, StatusTool, map[string]any{"id": "p_000000000000"}).text(); !isErr || !strings.Contains(text, "没有这个提案") {
		t.Errorf("没有的编号：%v %s", isErr, text)
	}
	if text, _ := f.call(sid, StatusTool, map[string]any{}).text(); !strings.Contains(text, "最近一小时没有") {
		t.Errorf("没有提案时列出：%s", text)
	}
	if text, isErr := f.call(sid, StatusTool, map[string]any{"wait": 99}).text(); !isErr || !strings.Contains(text, "0 到 30") {
		t.Errorf("wait 超出范围：%s", text)
	}
	text, _ := f.call(sid, "instance_props_set", map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "pvp", "value": "false"}}}).text()
	pid := proposalID(t, text)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = f.reg.Reject(local, pid, "周末人多，先别改")
	}()
	start := time.Now()
	st, isErr := f.call(sid, StatusTool, map[string]any{"id": pid, "wait": 10}).text()
	if isErr || !strings.Contains(st, "用户拒绝了，没有执行") || !strings.Contains(st, "周末人多") || time.Since(start) > 5*time.Second {
		t.Fatalf("wait 等到拒绝：%v %s（%v）", isErr, st, time.Since(start))
	}
	if list, _ := f.call(sid, StatusTool, map[string]any{}).text(); !strings.Contains(list, pid) || !strings.Contains(list, "被拒绝") {
		t.Errorf("列出最近的提案：%s", list)
	}
	// 过期：没人处理，状态写明过期
	text, _ = f.call(sid, "instance_props_set", map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "pvp", "value": "true"}}}).text()
	pid2 := proposalID(t, text)
	f.svc.mu.Lock()
	f.svc.props[pid2].expires = time.Now().Add(-time.Second)
	f.svc.mu.Unlock()
	if st, _ := f.call(sid, StatusTool, map[string]any{"id": pid2}).text(); !strings.Contains(st, "已经过期") {
		t.Errorf("过期：%s", st)
	}
}

// 同时等确认的 MCP 提案最多 10 个；关掉 MCP 接口时没处理的作废（记一条拒绝）；外部 AI 的提案只能在本机处理。
func TestPendingCapDisableAndDecide(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-06-18", "Cherry Studio")
	var last string
	for i := 0; i < maxPendingMCP; i++ {
		text, isErr := f.call(sid, "instance_props_set", map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "motd", "value": fmt.Sprint(i)}}}).text()
		if isErr {
			t.Fatalf("第 %d 个：%s", i, text)
		}
		last = proposalID(t, text)
	}
	if text, isErr := f.call(sid, "instance_props_set", map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "motd", "value": "x"}}}).text(); !isErr || !strings.Contains(text, "已经有 10 个提案") {
		t.Fatalf("超过上限：%s", text)
	}
	if _, isErr := f.call(sid, "app_info", map[string]any{}).text(); isErr {
		t.Fatal("读操作不受提案上限影响")
	}
	p := f.reg.Proposal(last)
	if err := CanDecide(lan, p); ops.CodeOf(err) != 403 {
		t.Errorf("局域网的设备不能处理外部 AI 的提案：%v", err)
	}
	if CanDecide(local, p) != nil || CanDecide(lan, &ops.Proposal{Caller: ops.Caller{Source: ops.SrcAssistant}}) != nil {
		t.Error("本机能处理；别的提案不归它管")
	}
	ch, cancel := f.svc.Subscribe()
	defer cancel()
	if err := f.svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if len(f.svc.Pending()) != 0 || f.reg.Proposal(last) != nil {
		t.Fatal("关掉后没处理的提案应作废")
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error("关掉时应通知界面")
	}
	rejects := 0
	for _, r := range f.records() {
		if r.Event == ops.EvReject && r.Src == ops.SrcMCP && strings.Contains(r.Reason, "关闭了 MineDeck 的 MCP 接口") {
			rejects++
		}
	}
	if rejects != maxPendingMCP {
		t.Errorf("作废的提案应各记一条拒绝：%d", rejects)
	}
	f.on()
	if st, _ := f.call(sid, StatusTool, map[string]any{"id": last}).text(); !strings.Contains(st, "用户拒绝了") || !strings.Contains(st, "MCP 接口") {
		t.Errorf("作废后查状态：%s", st)
	}
}

// 客户端报的名字：去掉换行和零宽字符、限长，记进连过的客户端和操作记录；会话认不出时（MineDeck 重启过）用桥带来的名字，
// 再没有就写「MCP 客户端」。
func TestClientIdentity(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-06-18", "Claude\nDesk\u200btop "+strings.Repeat("长", 60))
	text, _ := f.call(sid, "instance_restart", map[string]any{"inst": "生存服"}).text()
	actor := f.reg.Proposal(proposalID(t, text)).Caller.Actor
	if strings.ContainsAny(actor, "\n\u200b") || len([]rune(actor)) > 41 || !strings.HasPrefix(actor, "Claude Desk top 长") || !strings.HasSuffix(actor, "…") {
		t.Errorf("清理后的名字：%q", actor)
	}
	text, _ = f.call("unknown-session", "instance_restart", map[string]any{"inst": "生存服"}, HeaderClient, "Claude%20Desktop", HeaderBridge, "9.9.9").text()
	if a := f.reg.Proposal(proposalID(t, text)).Caller.Actor; a != "Claude Desktop" {
		t.Errorf("认不出会话时用桥带来的名字：%q", a)
	}
	text, _ = f.call("", "instance_restart", map[string]any{"inst": "生存服"}).text()
	if a := f.reg.Proposal(proposalID(t, text)).Caller.Actor; a != "MCP 客户端" {
		t.Errorf("不知道名字：%q", a)
	}
	f.modern(9, "tools/list", map[string]any{"_meta": modernMeta("VS Code")})
	cl := f.svc.Status(true).Clients
	if len(cl) != 3 || cl[0].Name != "VS Code" || cl[0].Protocol != Modern || cl[0].Transport != "http" {
		t.Fatalf("连过的客户端：%+v", cl)
	}
	var stdio *Client
	for i := range cl {
		if cl[i].Name == "Claude Desktop" {
			stdio = &cl[i]
		}
	}
	if stdio == nil || stdio.Transport != "stdio" || stdio.Calls != 1 {
		t.Errorf("桥转来的客户端：%+v", cl)
	}
}

// 界面的订阅：有新提案、处理完都通知（信号会合并）。
func TestSubscribe(t *testing.T) {
	f := newFixture(t)
	f.on()
	ch, cancel := f.svc.Subscribe()
	defer cancel()
	_, sid := f.initialize("2025-06-18", "Claude Desktop")
	text, _ := f.call(sid, "instance_restart", map[string]any{"inst": "生存服"}).text()
	wait := func(what string) {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("%s时没有通知", what)
		}
	}
	wait("新提案")
	_ = f.reg.Reject(local, proposalID(t, text), "")
	wait("拒绝")
}

// 复制配置：打开之后才有；Claude Desktop 用 stdio（exe 的路径加 -mcp，不带令牌），其余用 HTTP（地址加令牌）。
func TestConfigSnippets(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Config(); err == nil {
		t.Fatal("没打开时不给配置")
	}
	tok := f.on()
	c, err := f.svc.Config()
	if err != nil || c.Token != tok || c.URL != f.svc.URL() {
		t.Fatalf("配置：%v %+v", err, c)
	}
	byID := map[string]Snippet{}
	for _, s := range c.Snippets {
		byID[s.ID] = s
	}
	var claude struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(byID["claude"].Text), &claude); err != nil || claude.MCPServers["minedeck"].Command != baseDir+`\MineDeck.exe` ||
		js(claude.MCPServers["minedeck"].Args) != `["-mcp"]` || strings.Contains(byID["claude"].Text, tok) {
		t.Errorf("Claude Desktop：%v %s", err, byID["claude"].Text)
	}
	check := func(id, key, typ string) {
		var v map[string]map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal([]byte(byID[id].Text), &v); err != nil {
			t.Errorf("%s 不是 JSON：%v", id, err)
			return
		}
		e := v[key]["minedeck"]
		if e.Type != typ || e.URL != c.URL || e.Headers["Authorization"] != "Bearer "+tok {
			t.Errorf("%s：%+v", id, e)
		}
	}
	check("cherry", "mcpServers", "streamableHttp")
	check("cursor", "mcpServers", "")
	check("vscode", "servers", "http")
	if !strings.Contains(byID["http"].Text, c.URL) || !strings.Contains(byID["stdio"].Text, "-mcp") {
		t.Errorf("通用的配置：%v", byID)
	}
	for _, s := range c.Snippets {
		if s.Where == "" || s.Name == "" {
			t.Errorf("%s 要写明粘贴到哪儿", s.ID)
		}
	}
}

// 连过的客户端最多记 8 个：挤掉最久没来过的，新来的留下。
func TestClientsLimit(t *testing.T) {
	f := newFixture(t)
	f.on()
	for i := 0; i < maxClients+3; i++ {
		f.initialize("2025-06-18", fmt.Sprintf("客户端 %d", i))
		time.Sleep(2 * time.Millisecond) // Last 要分得出先后
	}
	cl := f.svc.Status(true).Clients
	if len(cl) != maxClients || cl[0].Name != fmt.Sprintf("客户端 %d", maxClients+2) || cl[len(cl)-1].Name != "客户端 3" {
		t.Fatalf("连过的客户端：%+v", cl)
	}
}

// 写操作的预览失败（比如停一台没在运行的服务器）：不生成提案，按工具的错误交回原因（以前会因为 nil 的提案出错）。
func TestPreviewFailure(t *testing.T) {
	f := newFixture(t)
	f.on()
	_, sid := f.initialize("2025-06-18", "Cherry Studio")
	text, isErr := f.call(sid, "instance_stop", map[string]any{"inst": "生存服"}).text()
	if !isErr || !strings.Contains(text, "没有在运行") || len(f.svc.Pending()) != 0 || f.count("instance.stop") != 0 {
		t.Fatalf("预览失败：%v %s", isErr, text)
	}
}

// Windows 上 Go 的时钟一跳 0.5～15.6 毫秒，前后脚来的客户端 Last 一样（GitHub 的 Windows 机器上实测）：
// 先后按来访的序号排，最近来过的在前；超过上限时挤掉的是最久没来过的，不是刚来的。
func TestClientOrderSameTick(t *testing.T) {
	f := newFixture(t)
	tick := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	f.svc.now = func() time.Time { return tick } // 时钟停在同一跳
	for i := 1; i <= maxClients+1; i++ {
		f.svc.seen(identity{Name: fmt.Sprintf("工具%d", i)}, "http", Modern, false)
	}
	f.svc.seen(identity{Name: "工具5"}, "http", Modern, true) // 老客户又来了：排到最前
	cl := f.svc.Status(true).Clients
	want := []string{"工具5", "工具9", "工具8", "工具7", "工具6", "工具4", "工具3", "工具2"}
	if len(cl) != len(want) {
		t.Fatalf("最多记 %d 个：%+v", maxClients, cl)
	}
	for i, c := range cl {
		if c.Name != want[i] {
			t.Fatalf("第 %d 个应是 %s：%+v", i+1, want[i], cl)
		}
	}
}
