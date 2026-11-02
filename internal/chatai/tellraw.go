package chatai

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 回复（设计文档 §6.3）：tellraw <目标> {"text":"[AI] ","color":"aqua","extra":[{"text":"回复内容","color":"white"}]}。
// 只用 text、color、extra 这三个最老的字段。
//
// 中文不乱码：命令经服务器的标准输入送进去。MineDeck 写标准输入时不转码（Go 的字符串原样写出，也就是 UTF-8，
// 见 inst 包的 writeLine；实例的「控制台编码」只管怎么读输出），服务器按什么编码读标准输入却取决于核心和 Java 的设置。
// 所以 JSON 文本里所有非 ASCII 的字都写成 \uXXXX（超出 BMP 的写成代理对），整条命令只有 ASCII，
// 在任何编码下读出来都一样（这一条是用户定的，替换了 §6.3 原来「不用 \u 转义」的写法）。
// JSON（1.21.4 及以前用 Gson 解析）认 \uXXXX；1.21.5 起文本组件改用 SNBT，SNBT 的带引号字符串也认 \uXXXX（推断，要实测）。
// 除此之外只转义 \ 和 "；换行、控制字符和 § 去掉，不用 \n 这类转义（§2.3：1.21.5 起 \n 在 tellraw 里可能报错）。

// 每条回复最多多少个字、最多几条（§6.3）。
const (
	MaxMsgRunes = 180
	MaxMsgs     = 3
)

// MoreHint 回答太长、截掉了后面时，最后一条末尾的提示。
const MoreHint = "……（完整回答请在 MineDeck 面板查看）"

// 颜色：前缀 aqua；回答正文 white；系统提示（帮助、额度、冷却……）gray。
const (
	colorHead   = "aqua"
	colorBody   = "white"
	colorSystem = "gray"
)

// Tellraw 拼出一条 tellraw 命令（整条只有 ASCII）。target 是 @a 或者提问的人的名字（已经过名字规则，见 validSender）。
func Tellraw(target, head, body, color string) string {
	return fmt.Sprintf(`tellraw %s {"text":"%s","color":"%s","extra":[{"text":"%s","color":"%s"}]}`,
		target, jsonText(head), colorHead, jsonText(body), color)
}

// cleanText 去掉换行、控制字符和 § 格式码（§ 连同后面那个字一起去掉，同 mclog.Clean；换行换成空格）。
func cleanText(s string) string {
	var b strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case skip:
			skip = false
		case r == '§':
			skip = true
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r == utf8.RuneError || unicode.IsControl(r) || r == '\U00002028' || r == '\U00002029':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// jsonText 先 cleanText，再按 JSON 字符串转义：\ 和 " 加反斜杠，非 ASCII 的字写成 \uXXXX。
func jsonText(s string) string {
	s = cleanText(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x80:
			b.WriteRune(r)
		case r > 0xffff:
			r -= 0x10000
			fmt.Fprintf(&b, "\\u%04x\\u%04x", 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			fmt.Fprintf(&b, "\\u%04x", r)
		}
	}
	return b.String()
}

// mdLine 去掉 Markdown 的几种常见写法（标题的 #、加粗的 **、代码的 `），列表的 - / * 换成 ·：游戏里不渲染 Markdown。
func mdLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#")
	s = strings.TrimSpace(s)
	for _, p := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(s, p) {
			s = "· " + strings.TrimSpace(s[len(p):])
			break
		}
	}
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return s
}

// Pack 把回答拆成最多 n 条、每条最多 max 个字：按换行分段，短的段落拼在一起，太长的在标点处断开；
// 放不下的截掉，最后一条末尾加上 MoreHint（cut 为 true）。
func Pack(text string, max, n int) (msgs []string, cut bool) {
	var paras []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(cleanText(mdLine(l))); l != "" {
			paras = append(paras, l)
		}
	}
	var cur string
	flush := func() {
		if cur != "" {
			msgs = append(msgs, cur)
			cur = ""
		}
	}
	for _, p := range paras {
		for p != "" {
			room := max - utf8.RuneCountInString(cur)
			if cur != "" {
				room -= 2 // 两段之间空两格
			}
			if utf8.RuneCountInString(p) <= room {
				if cur != "" {
					cur += "  "
				}
				cur += p
				p = ""
				continue
			}
			if cur != "" && utf8.RuneCountInString(p) <= max { // 这一段整段放得进下一条：不拆开
				flush()
				continue
			}
			if room < max/4 { // 剩下的地方太少，换一条再拆
				flush()
				continue
			}
			head, tail := breakAt(p, room)
			if cur != "" {
				cur += "  "
			}
			cur += head
			flush()
			p = tail
		}
	}
	flush()
	if len(msgs) > n {
		msgs, cut = msgs[:n], true
		last := []rune(msgs[n-1])
		keep := max - utf8.RuneCountInString(MoreHint)
		if len(last) > keep {
			last = last[:keep]
		}
		msgs[n-1] = strings.TrimRightFunc(string(last), unicode.IsSpace) + MoreHint
	}
	return msgs, cut
}

// breakAt 在最多 n 个字的地方断开：优先断在后 40% 里最后一个标点或空格之后。
func breakAt(s string, n int) (head, tail string) {
	r := []rune(s)
	if len(r) <= n {
		return s, ""
	}
	cut := n
	for i := n; i > n*6/10; i-- {
		if strings.ContainsRune("。！？；，、.!?;, ）)", r[i-1]) {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(r[:cut])), strings.TrimSpace(string(r[cut:]))
}
