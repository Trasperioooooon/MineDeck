package chatai

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"minedeck/internal/mclog"
)

// Rules 认聊天、认触发前缀的规则（实例设置里的几项，§6.1）。
type Rules struct {
	Prefixes   []string       // 触发前缀：主前缀和别名
	LooseNames bool           // 名字放宽（见 validSender）
	AnyThread  bool           // 不看线程名
	Pattern    *regexp.Regexp // 自定义正则：命名分组 name 和 msg；nil 用内置的五类核心的规则
}

// Hit 一行日志的识别结果。识别测试原样显示它；运行时只看 Player、Ask。
type Hit struct {
	Format  string `json:"format"`            // forge / vanilla / paper / custom；空 = 认不出前缀
	Thread  string `json:"thread,omitempty"`  // 线程名（Paper 格式和自定义正则没有）
	Chat    bool   `json:"chat"`              // 认出是一句聊天
	Player  string `json:"player,omitempty"`  // 说话的人
	Message string `json:"message,omitempty"` // 聊天的正文
	Ask     *Ask   `json:"ask,omitempty"`     // 触发了游戏内 AI：解析出的选项和问题
	Reason  string `json:"reason,omitempty"`  // 没触发的原因（识别测试里显示）
}

// Trigger 是不是触发了游戏内 AI（名字合规、正文以前缀开头）。在不在线另外查。
func (h Hit) Trigger() bool { return h.Ask != nil }

// Recognize 识别一行控制台输出（§6.1 的第 1–5 步）。不查说话的人在不在线：那一步要看实例当前的在线名单。
func Recognize(line string, r Rules) Hit {
	return recognizeParsed(mclog.ParseWith(line, mclog.ChatOptions{LooseNames: r.LooseNames, AnyThread: r.AnyThread}), r)
}

// recognizeParsed 同 Recognize，用已经拆好的一行（l 要按 r 的两个放宽项拆）。
func recognizeParsed(l mclog.Line, r Rules) Hit {
	h := Hit{Format: string(l.Format), Thread: l.Thread}
	if l.Kind == mclog.MineDeck {
		h.Reason = "MineDeck 自己写进控制台的行（命令回显、提示），不看"
		return h
	}
	switch {
	case r.Pattern != nil:
		m := r.Pattern.FindStringSubmatch(l.Text)
		if m == nil {
			h.Format, h.Thread, h.Reason = "custom", "", "自定义正则没有匹配上这一行"
			return h
		}
		h.Format, h.Thread = "custom", ""
		h.Player, h.Message = m[r.Pattern.SubexpIndex("name")], m[r.Pattern.SubexpIndex("msg")]
	case l.Kind == mclog.Chat:
		h.Player, h.Message = l.Player, l.Message
	default:
		h.Reason = notChat(l, r)
		return h
	}
	h.Chat = true
	if why := validSender(h.Player, r.LooseNames); why != "" {
		h.Reason = why
		return h
	}
	if h.Ask = ParseAsk(h.Message, r.Prefixes); h.Ask == nil {
		h.Reason = fmt.Sprintf("正文不是以触发前缀（%s）开头的，或者前缀后面直接跟着别的字", strings.Join(r.Prefixes, "、"))
	}
	return h
}

// notChat 认不出聊天的原因（识别测试里给人看）。
func notChat(l mclog.Line, r Rules) string {
	switch {
	case l.Format == mclog.FormatNone:
		return "认不出这一行的日志格式（Forge / NeoForge、原版 / Fabric、Paper 三种都对不上）；装了改聊天格式的插件或模组的服，可以填自定义正则"
	case l.Level != "INFO":
		return "不是 INFO 级别的日志，不是聊天"
	case l.Format != mclog.FormatPaper && !r.AnyThread && l.Thread != "Server thread" && !strings.HasPrefix(l.Thread, "Async Chat Thread - #"):
		return fmt.Sprintf("线程「%s」不是记录聊天的线程；线程名对不上的服，可以打开「不看线程名」", l.Thread)
	case l.Format == mclog.FormatForge && l.Logger != "minecraft/MinecraftServer" && l.Logger != "minecraft/DedicatedServer":
		return fmt.Sprintf("记录器「%s」不是记录聊天的 MinecraftServer", l.Logger)
	case strings.HasPrefix(l.Body, "<") || strings.HasPrefix(l.Body, "[Not Secure] <"):
		if r.LooseNames {
			return "名字超过 16 个字或含空白，不认"
		}
		return "名字只认 1–16 位字母、数字、下划线；离线服的名字带别的字符时，可以打开「放宽名字规则」"
	}
	return "不是玩家的聊天（聊天的正文形如 <名字> 内容）"
}

// validSender 说话的人能不能当提问者：名字会原样写进 tellraw 的目标和命令模板里的「自己」（§6.1 第 4 步）。
// 默认只认正版名字的规则（1–16 位字母、数字、下划线）。放宽后认 1–16 个可打印的 ASCII 字符（不含空格和 < >），
// 但以 @ 开头（名字本身要是 @a，就等于发给所有人、对所有人执行）、含 " 或 \ 的一律丢掉；
// 含中文等非 ASCII 字符的也丢掉：写进服务器标准输入的命令要保证整条只有 ASCII（见 tellraw.go）。
func validSender(name string, loose bool) string {
	if !loose {
		if !mclog.ValidName(name) {
			return "名字只认 1–16 位字母、数字、下划线；离线服的名字带别的字符时，可以打开「放宽名字规则」"
		}
		return ""
	}
	if n := utf8.RuneCountInString(name); n == 0 || n > 16 {
		return "名字要 1–16 个字"
	}
	if strings.HasPrefix(name, "@") {
		return "以 @ 开头的名字不认（写进命令会变成目标选择器）"
	}
	for _, c := range name {
		switch {
		case c == '"' || c == '\\':
			return `含 " 或 \ 的名字不认（会破坏命令）`
		case c < 0x21 || c > 0x7e || c == '<' || c == '>':
			return "名字里有中文、空格或其他非 ASCII 字符：没法安全地写进命令，不认"
		}
	}
	return ""
}

// compilePattern 编译自定义正则：要有命名分组 name 和 msg。空串返回 nil。
func compilePattern(s string) (*regexp.Regexp, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	re, err := regexp.Compile(s)
	if err != nil {
		return nil, fmt.Errorf("自定义正则写错了：%v", err)
	}
	if re.SubexpIndex("name") < 0 || re.SubexpIndex("msg") < 0 {
		return nil, fmt.Errorf("自定义正则要有两个命名分组：(?P<name>…) 是名字，(?P<msg>…) 是聊天内容")
	}
	return re, nil
}
