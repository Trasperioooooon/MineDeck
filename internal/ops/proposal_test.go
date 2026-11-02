package ops

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// 只能由人决定的参数：AI 传了也去掉（工具说明里也没有）；要勾选的同意（EULA）只能在确认时由人给出，
// 勾了才设成 true 执行，其余参数和提案完全一致。人直接调用时照常带。
func TestConsentAndHumanParams(t *testing.T) {
	f := newFixture(t)
	f.reg.Register(&Op{Name: "t.create", Title: "新建", Level: Write, HumanParams: []string{"eula", "root"},
		Consent: &Consent{Param: "eula", Text: "我已阅读并同意 Minecraft EULA", URL: "https://aka.ms/MinecraftEULA",
			Need: func(a Args) bool { return !strings.Contains(string(a), `"core":"velocity"`) }},
		Params: Object(map[string]*Schema{
			"name": String("名称"), "core": String("核心"), "eula": Boolean("同意 EULA"), "root": String("存放目录"),
		}, "name", "eula"),
		Run: func(_ context.Context, _ Caller, a Args) (any, error) {
			f.mu.Lock()
			f.runs["t.create"] = append(f.runs["t.create"], string(a))
			f.mu.Unlock()
			return "ok", nil
		}})
	ctx := context.Background()

	// 给 AI 的参数说明里没有 eula、root，必填里也去掉
	op := f.reg.Op("t.create")
	if s := string(op.AISchema()); strings.Contains(s, "eula") || strings.Contains(s, "root") || !strings.Contains(s, `"required":["name"]`) {
		t.Fatalf("AI 的参数说明：%s", s)
	}
	if s := string(f.reg.Op("t.read").AISchema()); s != `{"type":"object","properties":{}}` {
		t.Errorf("没有参数时：%s", s)
	}

	res, err := f.reg.Do(ctx, aiLocal, "t.create", Args(`{"name":"新服","core":"paper","eula":true,"root":"C:\\Windows"}`))
	if err != nil {
		t.Fatal(err)
	}
	p := res.(*Proposal)
	if string(p.Args) != `{"core":"paper","name":"新服"}` {
		t.Fatalf("AI 传的 eula、root 应去掉：%s", p.Args)
	}
	if p.Consent == nil || p.Consent.Param != "eula" || p.Consent.URL == "" {
		t.Fatalf("提案应要求勾选同意：%+v", p.Consent)
	}
	if _, err := f.reg.Confirm(ctx, ui, p.ID, false); CodeOf(err) != 400 || !strings.Contains(err.Error(), "EULA") || f.count("t.create") != 0 {
		t.Fatalf("没勾同意不能执行：%v", err)
	}
	if _, err := f.reg.ConfirmWith(ctx, ui, p.ID, ConfirmInput{Consent: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.lastArgs("t.create"); got != `{"core":"paper","eula":true,"name":"新服"}` {
		t.Fatalf("执行的参数应是提案的参数加上人勾的同意：%s", got)
	}
	if r := f.records(t)[0]; !strings.Contains(string(r.Args), `"eula":true`) || r.ApprovedBy != "本机" {
		t.Errorf("执行的记录：%+v", r)
	}
	// 代理端不用同意 EULA：没有勾选框，直接确认
	res, _ = f.reg.Do(ctx, aiLocal, "t.create", Args(`{"name":"代理","core":"velocity"}`))
	if q := res.(*Proposal); q.Consent != nil {
		t.Errorf("不需要同意时不该有勾选框：%+v", q.Consent)
	} else if _, err := f.reg.Confirm(ctx, ui, q.ID, false); err != nil || f.lastArgs("t.create") != `{"core":"velocity","name":"代理"}` {
		t.Errorf("代理端：%v %s", err, f.lastArgs("t.create"))
	}
	// 人直接调用：eula 照常带（界面上的勾选框就是本人同意）
	if _, err := f.reg.Do(ctx, ui, "t.create", Args(`{"name":"手动","eula":true,"root":"D:\\MC"}`)); err != nil || f.lastArgs("t.create") != `{"eula":true,"name":"手动","root":"D:\\MC"}` {
		t.Errorf("人直接调用：%v %s", err, f.lastArgs("t.create"))
	}
}

// 提案处理完都会通知（执行成功、执行失败、拒绝、过期、作废），助手据此把结果交还给 AI；作废和过期也各记一条。
func TestOnResolve(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var mu sync.Mutex
	var got []string
	f.reg.OnResolve(func(r Resolution) {
		mu.Lock()
		defer mu.Unlock()
		s := string(r.Outcome) + ":" + r.Proposal.Op + ":" + r.By.Actor
		if r.Reason != "" {
			s += ":" + r.Reason
		}
		if r.Err != nil {
			s += ":" + map[bool]string{true: "有错误"}[true]
		}
		got = append(got, s)
	})
	propose := func(op, args string) string {
		t.Helper()
		res, err := f.reg.Do(ctx, aiLocal, op, Args(args))
		if err != nil {
			t.Fatal(err)
		}
		return res.(*Proposal).ID
	}
	id := propose("t.write", `{"inst":"生存服"}`)
	_, _ = f.reg.Confirm(ctx, ui, id, false)
	id = propose("t.fail", `{}`)
	_, _ = f.reg.Confirm(ctx, lan, id, false)
	id = propose("t.write", `{"inst":"生存服"}`)
	_ = f.reg.Reject(lan, id, "不要")
	id = propose("t.write", `{"inst":"生存服"}`)
	f.mu.Lock()
	f.status = "running"
	f.mu.Unlock()
	_, _ = f.reg.Confirm(ctx, ui, id, false)
	id = propose("t.write", `{"inst":"生存服"}`)
	f.clock = f.clock.Add(6 * time.Minute)
	_, _ = f.reg.Confirm(ctx, ui, id, false)
	id = propose("t.write", `{"inst":"生存服"}`)
	f.clock = f.clock.Add(6 * time.Minute)
	if err := f.reg.Reject(ui, id, ""); CodeOf(err) != 410 {
		t.Errorf("拒绝已过期的提案应报 410：%v", err)
	}
	// 资格不够（AI 自己确认、危险操作没勾二次确认）不算处理完
	id = propose("t.danger", `{}`)
	_, _ = f.reg.Confirm(ctx, aiLocal, id, true)
	_, _ = f.reg.Confirm(ctx, ui, id, false)
	want := []string{"done:t.write:本机", "failed:t.fail:192.168.1.23:有错误", "rejected:t.write:192.168.1.23:不要",
		"invalid:t.write:本机:有错误", "expired:t.write:本机:有错误", "expired:t.write:本机:有错误"}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("通知：\n%v\n应为：\n%v", got, want)
	}
	var voids int
	for _, r := range f.records(t) {
		if r.Event == EvVoid {
			voids++
			if r.OK || r.Err == "" {
				t.Errorf("作废的记录应写明原因：%+v", r)
			}
		}
	}
	if voids != 3 {
		t.Errorf("作废、过期应各记一条，共 3 条：%d", voids)
	}
	if f.reg.Proposal(id) == nil || f.reg.Proposal("p_none") != nil {
		t.Error("Proposal 应取得到还没处理的提案")
	}
}

func TestLineDiff(t *testing.T) {
	a := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	b := "a\nb\nc\nd\nE\nf\ng\nh\ni\nj\nk\nl\nm\n"
	want := "@@ 第 3 行 @@\n  c\n  d\n- e\n+ E\n  f\n  g\n@@ 第 11 行 @@\n  k\n  l\n+ m"
	if got := LineDiff(a, b, 2, 0); got != want {
		t.Errorf("逐行对比：\n%s\n应为：\n%s", got, want)
	}
	if LineDiff("x\r\ny\r\n", "x\ny\n", 3, 0) != "" || LineDiff("same", "same", 3, 0) != "" {
		t.Error("只差换行符或完全相同时应为空")
	}
	if got := LineDiff("", "new\n", 3, 0); got != "@@ 第 1 行 @@\n+ new" {
		t.Errorf("新文件：%q", got)
	}
	if got := LineDiff("1\n2\n3\n4\n5\n", "", 3, 2); !strings.HasPrefix(got, "@@ 第 1 行 @@\n- 1\n……（还有 4 行没有显示）") {
		t.Errorf("超出行数要截断：%q", got)
	}
}
