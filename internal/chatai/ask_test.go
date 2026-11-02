package chatai

import (
	"slices"
	"strings"
	"testing"
)

var defPrefixes = []string{"@AI"}

type askWant struct {
	act      Action
	private  bool
	noExec   bool
	question string
	opts     []Opt
	unknown  string
}

func checkAsk(t *testing.T, msg string, prefixes []string, want *askWant) {
	t.Helper()
	a := ParseAsk(msg, prefixes)
	if want == nil {
		if a != nil {
			t.Errorf("%q 不该触发，实际 %+v", msg, a)
		}
		return
	}
	if a == nil {
		t.Errorf("%q 应触发", msg)
		return
	}
	if a.Action != want.act || a.Private != want.private || a.NoExec != want.noExec || a.Question != want.question {
		t.Errorf("%q → %s 私=%v 问=%v 问题=%q，应为 %s 私=%v 问=%v 问题=%q", msg, a.Action, a.Private, a.NoExec, a.Question,
			want.act, want.private, want.noExec, want.question)
	}
	if want.opts != nil && !slices.Equal(a.Opts, want.opts) {
		t.Errorf("%q 的选项 %v，应为 %v", msg, a.Opts, want.opts)
	}
	if a.Unknown != want.unknown {
		t.Errorf("%q 的未知选项 %q，应为 %q", msg, a.Unknown, want.unknown)
	}
}

// 设计文档 §6.2「识别测试的样例」全部 20 条（以 Steve 在线、前缀 @AI 为例），含后补的 /问、/额度。
func TestAskSamples(t *testing.T) {
	q := "今晚怎么打末影龙"
	ask := func(question string) *askWant { return &askWant{act: ActAsk, question: question} }
	priv := func(question string) *askWant { return &askWant{act: ActAsk, private: true, question: question} }
	help := &askWant{act: ActHelp}
	cases := []struct {
		msg  string
		want *askWant
	}{
		{"@AI 今晚怎么打末影龙", ask(q)},     // 提问，全服可见
		{"@AI /私 今晚怎么打末影龙", priv(q)}, // 只回 Steve
		{"@ai /P 今晚怎么打末影龙", priv(q)}, // 别名不分大小写
		{"＠AI ／私，今晚怎么打末影龙", priv(q)}, // 全角标记，逗号分隔
		{"@AI/私 今晚怎么打末影龙", priv(q)},  // 前缀后面直接跟选项
		{"@AI 、私 今晚怎么打末影龙", priv(q)}, // 顿号当标记
		{"@AI /私 /私 今晚怎么打末影龙", &askWant{act: ActAsk, private: true, question: q, opts: []Opt{OptPrivate}}}, // 重复的算一个
		{"@AI /帮助", help}, {"@AI /h", help}, // 私发帮助，不调用 AI
		{"@AI", help}, {"@AI /私", &askWant{act: ActHelp, private: true}}, // 没写问题，同 /帮助
		{"@AI /帮助 今晚怎么打末影龙", &askWant{act: ActHelp, question: q}},                  // 只回帮助
		{"@AI /tp 怎么用", &askWant{act: ActUnknown, question: "怎么用", unknown: "tp"}}, // 未知选项
		{"@AI /私今晚怎么打末影龙", &askWant{act: ActNoSpace, unknown: "私今晚怎么打末影龙"}},        // 提示「选项后面空一格」
		{"@AI 怎么用 /tp", ask("怎么用 /tp")},                                            // 问题里的 / 只是普通文字
		{"@AI 1/2 加 1/3 等于几", ask("1/2 加 1/3 等于几")},
		{"@AI 今晚怎么打末影龙 /私", &askWant{act: ActMisplaced, question: q + " /私"}}, // 选项要写在问题前面
		{"@AI /问 /私 怎么做附魔台", &askWant{act: ActAsk, private: true, noExec: true, question: "怎么做附魔台", opts: []Opt{OptAsk, OptPrivate}}},
		{"@AIbot /私 你好", nil}, // 不触发
		{"@AI /问 把天气改晴", &askWant{act: ActAsk, noExec: true, question: "把天气改晴"}}, // 全服可见、不给执行工具
		{"@AI /额度", &askWant{act: ActQuota}}, {"@AI /quota", &askWant{act: ActQuota}},
		{"@AI /额度 今晚怎么打末影龙", &askWant{act: ActQuota, question: q}}, // 只回额度
	}
	for _, c := range cases {
		checkAsk(t, c.msg, defPrefixes, c.want)
	}
}

// §6.1 第 5 步的触发规则：去掉首尾空白，前缀不分大小写、认全角 ＠，前缀后面要是分隔符、选项标记或者结束；可以加别名。
func TestTrigger(t *testing.T) {
	cases := []struct {
		msg  string
		want *askWant
	}{
		{"@AI 几点了", &askWant{act: ActAsk, question: "几点了"}},
		{"＠ai，帮我把天气改晴", &askWant{act: ActAsk, question: "帮我把天气改晴"}},
		{"@AI hello", &askWant{act: ActAsk, question: "hello"}},
		{"@Ai: hi", &askWant{act: ActAsk, question: "hi"}},
		{"@AI：你好", &askWant{act: ActAsk, question: "你好"}},
		{"@AI\U00003000你好", &askWant{act: ActAsk, question: "你好"}}, // 全角空格
		{"  @AI   你好  ", &askWant{act: ActAsk, question: "你好"}},
		{"<Alex> @AI op me", nil}, // 正文不以 @AI 开头
		{"@AIbot 不该触发", nil},
		{"AI 你好", nil},
		{"@A", nil},
		{"你好 @AI", nil},
		{"@AI /HELP", &askWant{act: ActHelp}},
		{"@AI /Ask 天气", &askWant{act: ActAsk, noExec: true, question: "天气"}},
		{"@AI /QUOTA", &askWant{act: ActQuota}},
		{"@AI /私:今晚", &askWant{act: ActAsk, private: true, question: "今晚"}}, // 选项名到分隔符为止
		{"@AI /帮助一下", &askWant{act: ActNoSpace, unknown: "帮助一下"}},
		{"@AI /help今晚", &askWant{act: ActUnknown, unknown: "help今晚"}}, // 英文别名后面接字：普通的未知选项
		{"@AI /tp /帮助", &askWant{act: ActHelp}},                       // 带了 /帮助 就只回帮助
		{"@AI /额度 /tp", &askWant{act: ActUnknown, unknown: "tp"}},
		{"@AI /一个非常非常非常非常非常非常长的选项名字 你好", &askWant{act: ActUnknown, question: "你好", unknown: "一个非常非常非常非常非常非常长的"}}, // 提示里最多 16 个字
		{"@AI 今晚 /P", &askWant{act: ActMisplaced, question: "今晚 /P"}},
		{"@AI 今晚 ／私", &askWant{act: ActMisplaced, question: "今晚 ／私"}},
		{"@AI 今晚/私", &askWant{act: ActAsk, question: "今晚/私"}},   // 没有空格：最后一个词不是 /私
		{"@AI 今晚 /问", &askWant{act: ActAsk, question: "今晚 /问"}}, // 只有 /私 放错位置才拦（怕本想私聊的回答被全服看到）
	}
	for _, c := range cases {
		checkAsk(t, c.msg, defPrefixes, c.want)
	}
	// 别名：和主前缀一样对待；先试长的
	ps := []string{"@AI", "@小助手", "@AIbot"}
	checkAsk(t, "@小助手 你好", ps, &askWant{act: ActAsk, question: "你好"})
	checkAsk(t, "@AIbot /私 你好", ps, &askWant{act: ActAsk, private: true, question: "你好"})
	checkAsk(t, "@AIbot2 你好", ps, nil)
	if a := ParseAsk("@ai 你好", ps); a == nil || a.Prefix != "@ai" {
		t.Errorf("认出的前缀应是原文：%+v", a)
	}
}

// 不调用 AI 的几种提示（§6.2）：没有执行权限的人不显示 /问；未知选项提示里的名字最多 16 个字。
func TestHintText(t *testing.T) {
	a := ParseAsk("@AI /tp 怎么用", defPrefixes)
	h := hintText("@AI", a, false)
	if !strings.Contains(h, "不认识 /tp") || !strings.Contains(h, "可用：/私（只回你）、/额度、/帮助") || strings.Contains(h, "/问") ||
		!strings.Contains(h, "@AI 怎么用 /tp") {
		t.Errorf("未知选项（没有执行权限）：%s", h)
	}
	if h := hintText("@AI", a, true); !strings.Contains(h, "/问（只回答、不执行命令）") {
		t.Errorf("有执行权限的人应看到 /问：%s", h)
	}
	if h := hintText("@AI", ParseAsk("@AI /私今晚怎么打末影龙", defPrefixes), false); h != "选项和问题之间要空一格，比如 @AI /私 今晚怎么打末影龙" {
		t.Errorf("选项后面没空格：%s", h)
	}
	if h := hintText("@AI", ParseAsk("@AI 今晚怎么打末影龙 /私", defPrefixes), false); h != "选项要写在问题前面，比如 @AI /私 今晚怎么打末影龙" {
		t.Errorf("选项放错位置：%s", h)
	}
	long := ParseAsk("@AI /abcdefghijklmnopqrstuvwxyz 你好", defPrefixes)
	if h := hintText("@AI", long, false); !strings.Contains(h, "不认识 /abcdefghijklmnop。") {
		t.Errorf("提示里的选项名最多 16 个字：%s", h)
	}
	// 举例时只在选项名后面补空格，后文原样带上
	for msg, want := range map[string]string{
		"@AI /私今晚 /问 你好": "选项和问题之间要空一格，比如 @AI /私 今晚 /问 你好",
		"@AI /私/问 你好":    "选项之间要空一格，比如 @AI /私 /问 你好",
	} {
		if h := hintText("@AI", ParseAsk(msg, defPrefixes), false); h != want {
			t.Errorf("%s：%s", msg, h)
		}
	}
	// 只有一个 /；不认识的不像命令名时，「怎么用 /…」拿 tp 举例
	if h := hintText("@AI", ParseAsk("@AI / 你好", defPrefixes), false); !strings.HasPrefix(h, "/ 后面要紧跟选项名，比如 @AI /私 今晚怎么打末影龙。") {
		t.Errorf("只有一个 /：%s", h)
	}
	if h := hintText("@AI", ParseAsk("@AI 、天气 晴", defPrefixes), false); !strings.Contains(h, "不认识 /天气。") || !strings.HasSuffix(h, "比如 @AI 怎么用 /tp") {
		t.Errorf("不认识的中文选项：%s", h)
	}
}
