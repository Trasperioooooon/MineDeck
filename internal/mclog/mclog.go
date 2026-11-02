// Package mclog 识别 Minecraft 服务端的控制台日志行：拆出时间、线程、级别、记录器和正文，
// 再认出进服、退服和聊天。
//
// 所有规则都锚定行首行尾，而且只对拆出来的正文匹配：玩家在聊天里打一句「Notch joined the game」，
// 这一行的正文是「<Steve> Notch joined the game」，只会被认成 Steve 的聊天，不会被当成 Notch 进服。
//
// 不依赖 Windows，Linux 上可以直接跑测试。
package mclog

import (
	"regexp"
	"strings"
)

// Format 日志行前缀的格式。
type Format string

const (
	FormatNone    Format = ""        // 没有可识别的前缀
	FormatForge   Format = "forge"   // Forge / NeoForge：[时:分:秒] [线程/级别] [记录器]: 正文
	FormatVanilla Format = "vanilla" // 原版 / Fabric / Quilt：[时:分:秒] [线程/级别]: 正文
	FormatPaper   Format = "paper"   // Paper / Purpur / Leaves / Folia：[时:分:秒 级别]: 正文
)

// Kind 日志行的类别。
type Kind int

const (
	Other    Kind = iota // 其他
	Join                 // 进服
	Leave                // 退服
	Chat                 // 聊天
	MineDeck             // MineDeck 自己写进控制台的行（「> 命令」回显、「[MineDeck] …」提示）
)

func (k Kind) String() string {
	return [...]string{"other", "join", "leave", "chat", "minedeck"}[k]
}

// Line 一行日志的识别结果。
type Line struct {
	Text   string // 去掉颜色转义和 § 格式码之后的整行
	Format Format
	Time   string // "20:15:03"
	Thread string // 线程名（Paper 格式没有）
	Level  string // INFO / WARN / ERROR …
	Logger string // Forge 格式方括号里的记录器，如 "minecraft/MinecraftServer"
	Body   string // 前缀之后的正文；没有前缀时为空

	Kind     Kind
	Player   string // 进服、退服、聊天：玩家名
	Message  string // 聊天：消息正文
	Unsigned bool   // 聊天：带「[Not Secure] 」前缀（1.19.1 起服务器记录没有签名的聊天时会加）
}

const trackName = `[^\s<>\[\]]{1,16}` // 进服退服认的名字：1–16 个字符，不含空白和 < > [ ]

var (
	// 前缀格式依次尝试，三种互不重叠（Forge 在线程方括号后面还有一段记录器）。
	// Mohist、Banner 这类混合端的格式没有核实，三种都试，哪个对上算哪个。
	forgeRe   = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\] \[([^\]]+)/([A-Z]+)\] \[([^\]]*)\]: (.*)$`)
	vanillaRe = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\] \[([^\]]+)/([A-Z]+)\]: (.*)$`)
	paperRe   = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2}) ([A-Z]+)\]: (.*)$`)

	// 聊天的发送者只认 1–16 位字母、数字、下划线（正版名字的规则）：游戏内 AI 要把它放进 tellraw，宁可漏认（设计文档 §6.1）。
	chatRe = regexp.MustCompile(`^(\[Not Secure\] )?<([A-Za-z0-9_]{1,16})> (.*)$`)
	// 放宽后（游戏内 AI 的实例设置，§6.1 第 4 步）：1–16 个不含空白和 < > 的字符。用不用得上、哪些名字要丢掉，由调用方决定。
	looseChatRe = regexp.MustCompile(`^(\[Not Secure\] )?<([^\s<>]{1,16})> (.*)$`)
	// 进服退服的名字放宽到「1–16 个字符，不含空白和 < > [ ]」：离线模式（MineDeck 默认）允许 Mr-X 这类名字，
	// Floodgate 的基岩版玩家带 . 前缀，旧版离线服还可能有中文名，都要计入在线人数。放宽不影响防伪造：
	// 聊天行的正文以 < 或「[Not Secure] 」开头，/me、/say、命令回显都带空格，凑不成「整段正文 = 名字 + 固定文字」。
	// 在线名单因此可能出现 @a 这类名字，第四阶段拿它校验聊天发送者时，要先按 §6.1 丢掉以 @ 开头和含 " \ § 的名字。
	joinRe = regexp.MustCompile(`^(` + trackName + `)(?: \(formerly known as ` + trackName + `\))? joined the game$`)
	leftRe = regexp.MustCompile(`^(` + trackName + `) left the game$`)
	// 系统消息「%s joined the game」「%s (formerly known as %s) joined the game」「%s left the game」（原版语言文件 en_us.json）之外，
	// 还认 PlayerList 的「{}[{}] logged in with entity id {} at ({}, {}, {})」和游戏连接断开时的「{} lost connection: {}」：
	// 用的是账号名，不受队伍前缀影响；插件关掉进服、退服提示时也照样有。登录、配置阶段断开的那两条写的是「名字 (/地址)」或整个
	// GameProfile，对不上这里的写法，不会误删在线玩家。
	loginRe = regexp.MustCompile(`^(` + trackName + `)\[[^\]]*\] logged in with entity id \d+ at \(.*\)$`)
	lostRe  = regexp.MustCompile(`^(` + trackName + `) lost connection: .*$`)

	// 聊天只认这两个线程：服务端主线程，和 Paper 的异步聊天线程（推断）
	asyncChatRe = regexp.MustCompile(`^Async Chat Thread - #\d+$`)

	ansiRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-Z\\-_]`) // CSI 序列（颜色等）和两字节的 ESC 序列
	fmtRe  = regexp.MustCompile(`§.?`)
)

// Clean 去掉 ANSI 颜色转义和 § 格式码。经管道读输出时一般已经没有了，以防万一再清一遍。
func Clean(s string) string {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = ansiRe.ReplaceAllString(s, "")
	}
	if strings.Contains(s, "§") {
		s = fmtRe.ReplaceAllString(s, "")
	}
	return s
}

// ValidName 是否合乎正版名字的规则：1–16 位字母、数字、下划线。
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 16 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// ChatOptions 聊天识别的两个放宽项（游戏内 AI 的实例设置，§6.1 第 3、4 步）。进服、退服的识别不受影响。
type ChatOptions struct {
	LooseNames bool // 发送者放宽成 1–16 个不含空白和 < > 的字符（离线模式可能允许更多字符）
	AnyThread  bool // 不看线程名（线程名对不上的服）；Forge 格式仍要求记录器是 MinecraftServer / DedicatedServer
}

// Parse 识别一行控制台输出。
func Parse(raw string) Line { return ParseWith(raw, ChatOptions{}) }

// ParseWith 同 Parse，按 o 放宽聊天的识别。
func ParseWith(raw string, o ChatOptions) Line {
	l := Line{Text: Clean(strings.TrimRight(raw, "\r\n"))}
	if strings.HasPrefix(l.Text, "> ") || strings.HasPrefix(l.Text, "[MineDeck]") {
		l.Kind = MineDeck
		return l
	}
	if m := forgeRe.FindStringSubmatch(l.Text); m != nil {
		l.Format, l.Time, l.Thread, l.Level, l.Logger, l.Body = FormatForge, m[1], m[2], m[3], m[4], m[5]
	} else if m := vanillaRe.FindStringSubmatch(l.Text); m != nil {
		l.Format, l.Time, l.Thread, l.Level, l.Body = FormatVanilla, m[1], m[2], m[3], m[4]
	} else if m := paperRe.FindStringSubmatch(l.Text); m != nil {
		l.Format, l.Time, l.Level, l.Body = FormatPaper, m[1], m[2], m[3]
	} else {
		return l
	}
	if l.Level != "INFO" {
		return l
	}
	if l.chatSource(o.AnyThread) {
		re := chatRe
		if o.LooseNames {
			re = looseChatRe
		}
		if m := re.FindStringSubmatch(l.Body); m != nil {
			l.Kind, l.Unsigned, l.Player, l.Message = Chat, m[1] != "", m[2], m[3]
			return l
		}
	}
	if l.Format == FormatForge && !strings.HasPrefix(l.Logger, "minecraft/") {
		return l // Forge 格式下，进服退服只认游戏本体的记录器，模组打印的同样文字不算
	}
	for _, r := range []struct {
		re   *regexp.Regexp
		kind Kind
	}{{joinRe, Join}, {loginRe, Join}, {leftRe, Leave}, {lostRe, Leave}} {
		if m := r.re.FindStringSubmatch(l.Body); m != nil {
			l.Kind, l.Player = r.kind, m[1]
			return l
		}
	}
	return l
}

// chatSource 这一行是不是由记录聊天的地方打出来的：线程是主线程或异步聊天线程（anyThread 时不看）；
// Forge 格式下记录器还得是 MinecraftServer / DedicatedServer。Paper 格式没有线程和记录器，不查。
func (l *Line) chatSource(anyThread bool) bool {
	if l.Format == FormatPaper {
		return true
	}
	if !anyThread && l.Thread != "Server thread" && !asyncChatRe.MatchString(l.Thread) {
		return false
	}
	return l.Format != FormatForge || l.Logger == "minecraft/MinecraftServer" || l.Logger == "minecraft/DedicatedServer"
}
