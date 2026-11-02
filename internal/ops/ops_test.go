package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 测试用的一组操作：记下每次执行收到的参数原文；t.write 的预览把「实例状态」当前置条件。
type fixture struct {
	reg    *Registry
	dir    string
	clock  time.Time
	mu     sync.Mutex
	runs   map[string][]string // 操作名 → 每次执行收到的参数原文
	status string              // t.write 预览时读的「实例状态」
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dir: filepath.Join(t.TempDir(), "logs", "ops"), runs: map[string][]string{}, status: "stopped",
		clock: time.Date(2026, 11, 2, 20, 15, 3, 0, time.FixedZone("CST", 8*3600))}
	f.reg = NewRegistry(NewLog(f.dir))
	f.reg.now = func() time.Time { return f.clock }
	run := func(name string, err error) func(context.Context, Caller, Args) (any, error) {
		return func(_ context.Context, _ Caller, a Args) (any, error) {
			f.mu.Lock()
			f.runs[name] = append(f.runs[name], string(a))
			f.mu.Unlock()
			return "ok", err
		}
	}
	instArgs := Object(map[string]*Schema{
		"inst":  String("实例名称").Len(1, 40),
		"value": Integer("一个数").Range(1, 100),
	}, "inst")
	f.reg.Register(&Op{Name: "t.read", Title: "读", Level: Read, Run: run("t.read", nil)})
	f.reg.Register(&Op{Name: "t.ingame", Title: "游戏内可读", Level: Read, InGame: true, Run: run("t.ingame", nil)})
	f.reg.Register(&Op{Name: "t.write", Title: "写", Level: Write, Params: instArgs, Run: run("t.write", nil),
		Preview: func(_ context.Context, _ Caller, a Args) (*Plan, error) {
			var p struct{ Inst string }
			_ = a.Bind(&p)
			if p.Inst == "坏的" {
				return nil, Errorf(404, "实例不存在: %s", p.Inst)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			return &Plan{Summary: "改 " + p.Inst, Pre: map[string]string{"实例状态": f.status}}, nil
		}})
	f.reg.Register(&Op{Name: "t.danger", Title: "危险", Level: Danger, Run: run("t.danger", nil)})
	f.reg.Register(&Op{Name: "t.human", Title: "仅人工", Level: HumanOnly, Run: run("t.human", nil)})
	f.reg.Register(&Op{Name: "t.local", Title: "打开目录", Level: HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "打开目录仅支持在本机操作", Run: run("t.local", nil)})
	f.reg.Register(&Op{Name: "t.localw", Title: "本机写", Level: Write, LocalOnly: true, Run: run("t.localw", nil)})
	f.reg.Register(&Op{Name: "t.fail", Title: "会失败", Level: Write, Run: run("t.fail", Errorf(502, "上游挂了"))})
	f.reg.Register(&Op{Name: "t.cfg", Title: "改配置", Level: HumanOnly, Run: run("t.cfg", nil)})
	return f
}

func (f *fixture) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs[name])
}

func (f *fixture) lastArgs(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[name]
	if len(r) == 0 {
		return ""
	}
	return r[len(r)-1]
}

func (f *fixture) records(t *testing.T) []Record {
	t.Helper()
	recs, err := f.reg.Log.Query(Query{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

var (
	ui        = Caller{Source: SrcUI, Actor: "本机", Local: true}
	api       = Caller{Source: SrcAPI, Actor: "本机", Local: true}
	lan       = Caller{Source: SrcLAN, Actor: "192.168.1.23"}
	aiLocal   = Caller{Source: SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	aiLAN     = Caller{Source: SrcAssistant, Actor: "192.168.1.23", Session: "s2"}
	gamechat  = Caller{Source: SrcGameChat, Actor: "Steve"}
	companion = Caller{Source: SrcCompanion, Actor: "伙伴"}
	schedule  = Caller{Source: SrcSchedule, Actor: "定时任务", Local: true}
	mcp       = Caller{Source: SrcMCP, Actor: "Claude Desktop", Local: true} // MCP 接口只收本机的连接
)

// 权限矩阵：调用来源 × 风险等级 × 仅本机。
func TestPermissionMatrix(t *testing.T) {
	f := newFixture(t)
	const run, prop, deny = "执行", "提案", "拒绝"
	callers := []struct {
		name string
		c    Caller
	}{{"ui", ui}, {"api", api}, {"lan", lan}, {"ai本机", aiLocal}, {"ai局域网", aiLAN}, {"游戏内", gamechat}, {"伙伴", companion}, {"定时", schedule}, {"mcp", mcp}}
	want := map[string][]string{
		//            ui   api  lan   ai本机 ai局域网 游戏内 伙伴  定时  mcp
		"t.read":   {run, run, run, run, run, deny, deny, run, run},
		"t.ingame": {run, run, run, run, run, run, run, run, run},
		"t.write":  {run, run, run, prop, prop, deny, deny, run, prop},
		"t.danger": {run, run, run, prop, prop, deny, deny, run, prop},
		"t.human":  {run, run, run, deny, deny, deny, deny, deny, deny},
		"t.local":  {run, run, deny, deny, deny, deny, deny, deny, deny},
		"t.localw": {run, run, deny, prop, deny, deny, deny, run, prop},
	}
	args := map[string]Args{"t.write": Args(`{"inst":"生存服"}`)}
	for op, row := range want {
		for i, cl := range callers {
			before := f.count(op)
			res, err := f.reg.Do(context.Background(), cl.c, op, args[op])
			got := run
			switch {
			case err != nil:
				got = deny
				if CodeOf(err) != 403 {
					t.Errorf("%s × %s：拒绝应为 403，实际 %d %v", op, cl.name, CodeOf(err), err)
				}
			case func() bool { _, ok := res.(*Proposal); return ok }():
				got = prop
			}
			if got != row[i] {
				t.Errorf("%s × %s：%s，应为%s（%v）", op, cl.name, got, row[i], err)
			}
			if ran := f.count(op) > before; ran != (got == run) {
				t.Errorf("%s × %s：结果是%s，却%s执行", op, cl.name, got, map[bool]string{true: "", false: "没有"}[ran])
			}
			if err := f.reg.Check(cl.c, op); (err != nil) != (got == deny) {
				t.Errorf("Check(%s, %s) 与 Do 不一致：%v", cl.name, op, err)
			}
		}
	}
	if _, err := f.reg.Do(context.Background(), lan, "t.local", nil); err == nil || err.Error() != "打开目录仅支持在本机操作" {
		t.Errorf("仅本机的提示应用操作自己的说法：%v", err)
	}
	if _, err := f.reg.Do(context.Background(), ui, "t.none", nil); CodeOf(err) != 404 {
		t.Errorf("未知操作应为 404：%v", err)
	}
	if _, err := f.reg.Do(context.Background(), Caller{Source: "robot"}, "t.read", nil); CodeOf(err) != 403 {
		t.Errorf("未知来源应拒绝：%v", err)
	}
}

// AI 的工具清单：仅人工的不出现；游戏内只有明确开放的读操作。
func TestAvailable(t *testing.T) {
	f := newFixture(t)
	names := func(c Caller) string {
		var s []string
		for _, op := range f.reg.Available(c) {
			s = append(s, op.Name)
		}
		return strings.Join(s, ",")
	}
	if got := names(aiLocal); got != "t.read,t.ingame,t.write,t.danger,t.localw,t.fail" {
		t.Errorf("本机助手的工具：%s", got)
	}
	if got := names(aiLAN); got != "t.read,t.ingame,t.write,t.danger,t.fail" {
		t.Errorf("局域网助手的工具：%s", got)
	}
	if got := names(gamechat); got != "t.ingame" {
		t.Errorf("游戏内 AI 的工具：%s", got)
	}
	if got := names(mcp); got != "t.read,t.ingame,t.write,t.danger,t.localw,t.fail" {
		t.Errorf("MCP 接口的工具（同本机助手）：%s", got)
	}
	if !SrcMCP.AI() || SrcMCP.Human() {
		t.Error("MCP 接口算 AI 发起，不算人")
	}
	if ToolName("instance.props.set") != "instance_props_set" {
		t.Error("ToolName 不对")
	}
}

// 参数按说明校验：类型、必填、范围、长度；不合格就不执行。
func TestValidation(t *testing.T) {
	f := newFixture(t)
	cases := map[string]string{
		`{}`:                         "缺少参数 inst",
		`null`:                       "缺少参数 inst",
		`{"inst":5}`:                 "参数 inst 应为字符串",
		`{"inst":""}`:                "参数 inst 应为 1 到 40 个字",
		`{"inst":"a","value":"7"}`:   "参数 value 应为整数",
		`{"inst":"a","value":7.5}`:   "参数 value 应为整数",
		`{"inst":"a","value":101}`:   "参数 value 应在 1 到 100 之间",
		`[1,2]`:                      "参数应为 JSON 对象",
		`{"inst":"a"`:                "参数不是合法的 JSON",
		`{"inst":"a"} {"inst":"b"}`:  "多余内容",
		`{"inst":null,"value":null}`: "缺少参数 inst",
	}
	for in, want := range cases {
		_, err := f.reg.Do(context.Background(), ui, "t.write", Args(in))
		if err == nil || CodeOf(err) != 400 || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：%v，应为 400「%s」", in, err, want)
		}
	}
	if n := f.count("t.write"); n != 0 {
		t.Fatalf("参数不合格却执行了 %d 次", n)
	}
	// 嵌套：数组、枚举、正则
	f.reg.Register(&Op{Name: "t.nested", Title: "嵌套", Level: Write, Run: func(context.Context, Caller, Args) (any, error) { return nil, nil },
		Params: Object(map[string]*Schema{
			"mode":  String("模式").OneOf("a", "b"),
			"pairs": Array("键值对", Object(map[string]*Schema{"key": String("键").Match(`[a-z.-]+`), "value": String("值")}, "key", "value")).MaxLen(2),
			"on":    Boolean("开关"),
		})})
	for in, want := range map[string]string{
		`{"mode":"c"}`:                        "参数 mode 只能是 a、b 之一",
		`{"pairs":[{"key":"A","value":"1"}]}`: "参数 pairs[0].key 格式不对",
		`{"pairs":[{"key":"a"}]}`:             "缺少参数 pairs[0].value",
		`{"pairs":[null]}`:                    "参数 pairs[0] 不能为 null",
		`{"pairs":[{},{},{}]}`:                "参数 pairs 最多 2 项",
		`{"on":"true"}`:                       "参数 on 应为布尔值",
		`{"pairs":{"key":"a","value":"1"}}`:   "参数 pairs 应为数组",
	} {
		if _, err := f.reg.Do(context.Background(), ui, "t.nested", Args(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：%v，应含「%s」", in, err, want)
		}
	}
	if _, err := f.reg.Do(context.Background(), ui, "t.nested", Args(`{"mode":"a","pairs":[{"key":"rcon.port","value":"1"}],"on":false,"extra":1}`)); err != nil {
		t.Errorf("合格的参数被拒：%v", err)
	}
}

// 执行时拿到的是规范化后的参数：键排序、去掉空白、不转义 < > &。
func TestArgsCanonical(t *testing.T) {
	f := newFixture(t)
	if _, err := f.reg.Do(context.Background(), ui, "t.write", Args(" {\n \"value\" : 7 ,\"inst\":\"a<b>&c\" } ")); err != nil {
		t.Fatal(err)
	}
	if got := f.lastArgs("t.write"); got != `{"inst":"a<b>&c","value":7}` {
		t.Errorf("参数没有规范化：%s", got)
	}
	var s struct {
		Inst  string
		Value int
	}
	if err := Args(f.lastArgs("t.write")).Bind(&s); err != nil || s.Inst != "a<b>&c" || s.Value != 7 {
		t.Errorf("Bind = %+v, %v", s, err)
	}
	if b, _ := json.Marshal(struct{ A Args }{Args(`{"x":1}`)}); string(b) != `{"A":{"x":1}}` {
		t.Errorf("Args 编码成 JSON 时应原样嵌入：%s", b)
	}
}

// 提案的一生：AI 发起的写操作不执行，只生成提案；只有人能确认；确认后按存下的参数原样执行一次；
// 危险操作要二次确认；过期、前置条件变了都作废；拒绝后不执行。
func TestProposalLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	propose := func(op, args string) *Proposal {
		t.Helper()
		res, err := f.reg.Do(ctx, aiLocal, op, Args(args))
		if err != nil {
			t.Fatal(err)
		}
		p, ok := res.(*Proposal)
		if !ok {
			t.Fatalf("应返回提案，实际 %T", res)
		}
		return p
	}

	p := propose("t.write", `{"value":42,"inst":"生存服"}`)
	if f.count("t.write") != 0 {
		t.Fatal("没确认就执行了")
	}
	if !strings.HasPrefix(p.ID, "p_") || p.Level != Write || p.Plan.Summary != "改 生存服" || string(p.Args) != `{"inst":"生存服","value":42}` ||
		!p.Expires.Equal(f.clock.Add(5*time.Minute)) || p.Caller.Session != "s1" {
		t.Fatalf("提案内容不对：%+v", p)
	}
	if b, _ := json.Marshal(p); bytes.Contains(b, []byte("实例状态")) || !bytes.Contains(b, []byte(`"level":"W"`)) {
		t.Errorf("提案的 JSON 不应带前置条件指纹：%s", b)
	}
	if l := f.reg.Pending("s1"); len(l) != 1 || l[0].ID != p.ID {
		t.Fatalf("Pending(s1) = %v", l)
	}
	if l := f.reg.Pending("s2"); len(l) != 0 {
		t.Fatalf("别的会话不该看到：%v", l)
	}
	// AI 自己确认不行，提案还在
	if _, err := f.reg.Confirm(ctx, aiLocal, p.ID, true); CodeOf(err) != 403 || len(f.reg.Pending("")) != 1 {
		t.Fatalf("AI 不能确认提案：%v", err)
	}
	// 篡改返回给调用方的提案，不影响存着的那份
	copy(p.Args, `{"inst":"别的服","value":99}`)
	res, err := f.reg.Confirm(ctx, ui, p.ID, false)
	if err != nil || res != "ok" {
		t.Fatalf("确认失败：%v", err)
	}
	if f.count("t.write") != 1 || f.lastArgs("t.write") != `{"inst":"生存服","value":42}` {
		t.Fatalf("执行的参数和提案不一致：%v", f.runs["t.write"])
	}
	if _, err := f.reg.Confirm(ctx, ui, p.ID, false); CodeOf(err) != 404 || f.count("t.write") != 1 {
		t.Fatalf("提案只能用一次：%v", err)
	}
	// 记两条：AI 提出（event 为 propose），确认后执行（带确认人）
	recs := f.records(t)
	if len(recs) != 2 || recs[0].Proposal != p.ID || recs[0].ApprovedBy != "本机" || recs[0].Src != SrcAssistant || recs[0].Event != "" ||
		recs[0].Inst != "生存服" || string(recs[0].Args) != `{"value":42}` || !recs[0].OK {
		t.Fatalf("操作记录不对：%+v", recs)
	}
	if r := recs[1]; r.Event != EvPropose || r.Proposal != p.ID || r.Src != SrcAssistant || r.Session != "s1" || r.ApprovedBy != "" || string(r.Args) != `{"value":42}` {
		t.Fatalf("提出提案也要记一条：%+v", r)
	}

	// 危险操作：不勾二次确认不执行，提案还留着；勾了才执行
	d := propose("t.danger", `{}`)
	if _, err := f.reg.Confirm(ctx, lan, d.ID, false); CodeOf(err) != 400 || f.count("t.danger") != 0 || len(f.reg.Pending("")) != 1 {
		t.Fatalf("危险操作没有二次确认就执行了：%v", err)
	}
	if _, err := f.reg.Confirm(ctx, lan, d.ID, true); err != nil || f.count("t.danger") != 1 {
		t.Fatalf("二次确认后应执行：%v", err)
	}
	if r := f.records(t)[0]; r.ApprovedBy != "192.168.1.23" || r.Op != "t.danger" {
		t.Errorf("确认人应是局域网设备：%+v", r)
	}

	// 过期
	e := propose("t.write", `{"inst":"生存服"}`)
	f.clock = f.clock.Add(5*time.Minute + time.Second)
	if l := f.reg.Pending(""); len(l) != 0 {
		t.Errorf("过期的提案不该再列出来：%v", l)
	}
	if _, err := f.reg.Confirm(ctx, ui, e.ID, false); CodeOf(err) != 410 || f.count("t.write") != 1 {
		t.Fatalf("过期的提案应作废（410）：%v", err)
	}

	// 前置条件变了：预览之后实例被启动了
	c := propose("t.write", `{"inst":"生存服"}`)
	f.mu.Lock()
	f.status = "running"
	f.mu.Unlock()
	if _, err := f.reg.Confirm(ctx, ui, c.ID, false); CodeOf(err) != 409 || !strings.Contains(err.Error(), "实例状态") || f.count("t.write") != 1 {
		t.Fatalf("前置条件变了应作废（409，说明哪一项变了）：%v", err)
	}
	if _, err := f.reg.Confirm(ctx, ui, c.ID, false); CodeOf(err) != 404 {
		t.Fatalf("作废的提案不能再确认：%v", err)
	}
	// 状态没变就照常执行
	c2 := propose("t.write", `{"inst":"生存服"}`)
	if _, err := f.reg.Confirm(ctx, ui, c2.ID, false); err != nil || f.count("t.write") != 2 {
		t.Fatalf("前置条件没变应执行：%v", err)
	}
	// 预览时实例就不在了：不生成提案
	if _, err := f.reg.Do(ctx, aiLocal, "t.write", Args(`{"inst":"坏的"}`)); CodeOf(err) != 404 {
		t.Fatalf("预览失败应原样报错：%v", err)
	}

	// 拒绝
	r := propose("t.write", `{"inst":"生存服"}`)
	if err := f.reg.Reject(aiLocal, r.ID, ""); CodeOf(err) != 403 {
		t.Fatalf("AI 不能替人拒绝：%v", err)
	}
	if err := f.reg.Reject(ui, r.ID, "  先别改  "); err != nil {
		t.Fatal(err)
	}
	if rec := f.records(t)[0]; rec.Event != EvReject || rec.Proposal != r.ID || rec.ApprovedBy != "本机" || rec.Reason != "先别改" || !rec.OK || rec.Src != SrcAssistant {
		t.Fatalf("拒绝要记一条，带上理由：%+v", rec)
	}
	if _, err := f.reg.Confirm(ctx, ui, r.ID, false); CodeOf(err) != 404 || f.count("t.write") != 2 {
		t.Fatalf("拒绝后不应执行：%v", err)
	}
	if err := f.reg.Reject(ui, r.ID, ""); CodeOf(err) != 404 {
		t.Fatalf("重复拒绝应为 404：%v", err)
	}

	// 仅本机的写操作：局域网的人不能确认
	lw := propose("t.localw", `{}`)
	if _, err := f.reg.Confirm(ctx, lan, lw.ID, false); CodeOf(err) != 403 || f.count("t.localw") != 0 {
		t.Fatalf("局域网设备不能确认仅本机的操作：%v", err)
	}
	if _, err := f.reg.Confirm(ctx, ui, lw.ID, false); err != nil || f.count("t.localw") != 1 {
		t.Fatalf("本机确认后应执行：%v", err)
	}
	// 仅人工的操作：AI 连提案都提不出来
	if _, err := f.reg.Do(ctx, aiLocal, "t.human", nil); CodeOf(err) != 403 || len(f.reg.Pending("")) != 0 {
		t.Fatalf("仅人工的操作不能有提案：%v", err)
	}
}

// 等确认的提案有上限，超出时挤掉最早的。
func TestProposalLimit(t *testing.T) {
	f := newFixture(t)
	var first string
	for i := 0; i < maxPending+5; i++ {
		f.clock = f.clock.Add(time.Millisecond)
		res, err := f.reg.Do(context.Background(), aiLocal, "t.danger", nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = res.(*Proposal).ID
		}
	}
	if n := len(f.reg.Pending("")); n != maxPending {
		t.Fatalf("等确认的提案有 %d 个，上限 %d", n, maxPending)
	}
	if _, err := f.reg.Confirm(context.Background(), ui, first, true); CodeOf(err) != 404 {
		t.Fatalf("最早的提案应被挤掉：%v", err)
	}
}

// 操作记录：写操作、危险操作、仅人工的操作都记；读操作和 NoLog 的不记；失败也记；按月写 JSONL。
func TestOpLogRecords(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, call := range []struct {
		c    Caller
		op   string
		args string
	}{
		{ui, "t.read", ""}, {ui, "t.local", ""}, {ui, "t.write", `{"inst":"生存服","value":5}`},
		{lan, "t.fail", ""}, {ui, "t.human", `{"inst":"机械动力"}`},
	} {
		_, _ = f.reg.Do(ctx, call.c, call.op, Args(call.args))
	}
	f.reg.Record(schedule, "instance.restart", ArgsOf(map[string]any{"inst": "生存服", "at": "04:00"}), nil, 3*time.Second)
	recs := f.records(t)
	var ops []string
	for _, r := range recs {
		ops = append(ops, r.Op)
	}
	if strings.Join(ops, ",") != "instance.restart,t.human,t.fail,t.write" {
		t.Fatalf("记下的操作（新的在前）：%v", ops)
	}
	w := recs[3]
	if w.T != "2026-11-02T20:15:03+08:00" || w.Src != SrcUI || w.Actor != "本机" || w.Inst != "生存服" || string(w.Args) != `{"value":5}` || !w.OK {
		t.Errorf("写操作的记录：%+v", w)
	}
	if fl := recs[2]; fl.OK || fl.Err != "上游挂了" || fl.Src != SrcLAN || fl.Actor != "192.168.1.23" {
		t.Errorf("失败的操作也要记：%+v", fl)
	}
	if s := recs[0]; s.Src != SrcSchedule || s.Actor != "定时任务" || s.MS != 3000 || string(s.Args) != `{"at":"04:00"}` {
		t.Errorf("定时任务的记录：%+v", s)
	}
	b, err := os.ReadFile(filepath.Join(f.dir, "2026-11.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 4 {
		t.Fatalf("应有 4 行：%s", b)
	}
	if !strings.Contains(string(b), `{"t":"2026-11-02T20:15:03+08:00","op":"t.write","src":"ui","actor":"本机","inst":"生存服","args":{"value":5},"ok":true,"ms":`) {
		t.Errorf("JSONL 的写法和设计文档 §3.4 的样例不一致：%s", b)
	}
}

// Key、密码、凭据写成 ***：键名像密钥的字段，以及 server.properties 里像密码的键值对；空值保留（看得出是清除）。
func TestOpLogRedaction(t *testing.T) {
	f := newFixture(t)
	args := `{"cfApiKey":"cf-secret-1","lanPassword":"pw-secret-2","webhookUrl":"https://hook.example/x?access_token=tk-3",
		"credential":"cred-4","key":"ai-key-5","backupKeep":5,"source":"mirror","cleared":{"cfApiKey":""},
		"pairs":[{"key":"rcon.password","value":"hunter2"},{"key":"max-players","value":"10"},{"key":"management-server-secret","value":"ms-6"}],
		"tunnels":[{"name":"自建","token":"frps-7"}]}`
	if _, err := f.reg.Do(context.Background(), ui, "t.cfg", Args(args)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(f.dir, "2026-11.jsonl"))
	for _, secret := range []string{"cf-secret-1", "pw-secret-2", "tk-3", "cred-4", "ai-key-5", "hunter2", "ms-6", "frps-7"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("操作记录里有 %s：%s", secret, b)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(f.records(t)[0].Args, &got); err != nil {
		t.Fatal(err)
	}
	want := `{"backupKeep":5,"cfApiKey":"***","cleared":{"cfApiKey":""},"credential":"***","key":"***","lanPassword":"***",` +
		`"pairs":[{"key":"rcon.password","value":"***"},{"key":"max-players","value":"10"},{"key":"management-server-secret","value":"***"}],` +
		`"source":"mirror","tunnels":[{"name":"自建","token":"***"}],"webhookUrl":"***"}`
	if s := string(encode(got)); s != want {
		t.Errorf("脱敏结果：\n%s\n应为：\n%s", s, want)
	}
}

// 查询：按实例、来源、操作过滤，新的在前，跨月份，限制条数。
func TestOpLogQuery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.clock = time.Date(2026, 10, 31, 23, 59, 0, 0, time.Local)
	_, _ = f.reg.Do(ctx, ui, "t.write", Args(`{"inst":"A","value":1}`))
	_, _ = f.reg.Do(ctx, lan, "t.write", Args(`{"inst":"B","value":2}`))
	f.clock = time.Date(2026, 11, 1, 0, 1, 0, 0, time.Local)
	_, _ = f.reg.Do(ctx, ui, "t.write", Args(`{"inst":"A","value":3}`))
	_, _ = f.reg.Do(ctx, ui, "t.danger", nil)
	if _, err := os.Stat(filepath.Join(f.dir, "2026-10.jsonl")); err != nil {
		t.Fatal("10 月的记录应写在 2026-10.jsonl")
	}
	val := func(r Record) string { return string(r.Args) }
	q := func(q Query) string {
		recs, err := f.reg.Log.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, r := range recs {
			s = append(s, r.Op+val(r))
		}
		return strings.Join(s, " ")
	}
	if got := q(Query{}); got != `t.danger t.write{"value":3} t.write{"value":2} t.write{"value":1}` {
		t.Errorf("全部：%s", got)
	}
	if got := q(Query{Inst: "A"}); got != `t.write{"value":3} t.write{"value":1}` {
		t.Errorf("实例 A：%s", got)
	}
	if got := q(Query{Src: SrcLAN}); got != `t.write{"value":2}` {
		t.Errorf("来源 lan：%s", got)
	}
	if got := q(Query{Op: "t.d"}); got != `t.danger` {
		t.Errorf("操作前缀：%s", got)
	}
	if got := q(Query{Limit: 2}); got != `t.danger t.write{"value":3}` {
		t.Errorf("限两条：%s", got)
	}
	// 写到一半的坏行跳过
	fh, _ := os.OpenFile(filepath.Join(f.dir, "2026-11.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(`{"t":"2026-11-01T00:02:00`)
	fh.Close()
	if got := q(Query{Limit: 1}); got != `t.danger` {
		t.Errorf("坏行应跳过：%s", got)
	}
	if recs, _ := NewLog(filepath.Join(t.TempDir(), "none")).Query(Query{}); recs == nil || len(recs) != 0 {
		t.Errorf("没有记录时应返回空数组：%#v", recs)
	}
}

// 保留：最多 12 个月；总共超过上限时从最早的月份删起；当月的不删。
func TestOpLogRetention(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	for m := 1; m <= 13; m++ { // 2025-11 … 2026-11 共 13 个月
		y, mm := 2025, 10+m
		if mm > 12 {
			y, mm = 2026, mm-12
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d-%02d.jsonl", y, mm)), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("别的文件不动"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.Write(Record{T: "2026-12-01T08:00:00+08:00", Op: "x", OK: true})
	names := l.files()
	if len(names) != 12 || names[0] != "2026-01.jsonl" || names[11] != "2026-12.jsonl" {
		t.Fatalf("应只留最近 12 个月：%v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("不该删别的文件")
	}
	// 总量超限：从最早的删起，当月的哪怕自己就超了也不删
	l.MaxBytes = 4000
	for _, n := range names[:11] {
		_ = os.WriteFile(filepath.Join(dir, n), bytes.Repeat([]byte("x"), 1000), 0o600)
	}
	l.Write(Record{T: "2027-01-02T08:00:00+08:00", Op: "x", OK: true})
	// 11 个 1000 字节的旧文件加两个小文件：删掉最早的 8 个后降到 4000 字节以内
	if names := l.files(); len(names) != 5 || names[0] != "2026-09.jsonl" || names[4] != "2027-01.jsonl" {
		t.Fatalf("总量超限后应只剩最近的几个月：%v", names)
	}
	l.MaxBytes = 10
	l.Write(Record{T: "2027-02-02T08:00:00+08:00", Op: "x", OK: true})
	if names := l.files(); len(names) != 1 || names[0] != "2027-02.jsonl" {
		t.Fatalf("当月的文件不删：%v", names)
	}
}

func TestErrorCodes(t *testing.T) {
	if CodeOf(errors.New("x")) != 400 || CodeOf(Errorf(404, "x")) != 404 || CodeOf(fmt.Errorf("包一层：%w", Errorf(502, "x"))) != 502 {
		t.Error("CodeOf 不对")
	}
	if Wrap(500, nil) != nil || CodeOf(Wrap(500, errors.New("x"))) != 500 {
		t.Error("Wrap 不对")
	}
}

// 参数说明能直接编成 JSON Schema 交给 AI，键的顺序固定（前缀缓存才能命中）。
func TestSchemaJSON(t *testing.T) {
	s := Object(map[string]*Schema{
		"inst": String("实例名称").Len(1, 40),
		"mode": String("模式").OneOf("a", "b"),
		"n":    Integer("数量").Range(0, 10),
	}, "inst")
	s.compile()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{"inst":{"type":"string","description":"实例名称","minLength":1,"maxLength":40},` +
		`"mode":{"type":"string","description":"模式","enum":["a","b"]},"n":{"type":"integer","description":"数量","minimum":0,"maximum":10}},"required":["inst"]}`
	if string(b) != want {
		t.Errorf("JSON Schema：\n%s\n应为：\n%s", b, want)
	}
}

func TestRegisterRejectsBadOps(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&Op{Name: "a", Level: Read, Run: func(context.Context, Caller, Args) (any, error) { return nil, nil }})
	for name, op := range map[string]*Op{
		"重名":   {Name: "a", Level: Read, Run: func(context.Context, Caller, Args) (any, error) { return nil, nil }},
		"缺执行":  {Name: "b", Level: Read},
		"缺等级":  {Name: "c", Run: func(context.Context, Caller, Args) (any, error) { return nil, nil }},
		"正则不对": {Name: "d", Level: Read, Run: func(context.Context, Caller, Args) (any, error) { return nil, nil }, Params: Object(map[string]*Schema{"x": String("x").Match("(")})},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s：应 panic", name)
				}
			}()
			r.Register(op)
		}()
	}
	// 没有操作记录时照常执行
	if _, err := r.Do(context.Background(), ui, "a", nil); err != nil {
		t.Error(err)
	}
}

// AI 发起的写操作预览失败（比如重启一台没在运行的服务器）：Do 返回 nil 和错误，不能返回装着 nil *Proposal 的 any，
// 否则调用方的类型断言会把它当成提案。
func TestProposeErrorIsUntypedNil(t *testing.T) {
	f := newFixture(t)
	f.reg.Register(&Op{Name: "t.badpreview", Title: "预览会失败", Level: Danger,
		Preview: func(context.Context, Caller, Args) (*Plan, error) {
			return nil, Errorf(400, "服务器没有在运行")
		},
		Run: func(context.Context, Caller, Args) (any, error) { return "ran", nil }})
	for _, c := range []Caller{aiLocal, mcp} {
		res, err := f.reg.Do(context.Background(), c, "t.badpreview", nil)
		if res != nil || err == nil || err.Error() != "服务器没有在运行" {
			t.Errorf("%s：res=%#v err=%v", c.Source, res, err)
		}
		if _, ok := res.(*Proposal); ok {
			t.Errorf("%s：预览失败时不能断言出提案", c.Source)
		}
	}
	if n := len(f.reg.Pending("")); n != 0 {
		t.Errorf("预览失败不留提案：%d", n)
	}
}
