package mclog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type ev struct {
	kind   Kind
	player string
	msg    string
}

func (e ev) String() string { return fmt.Sprintf("%s %s %q", e.kind, e.player, e.msg) }

func evOf(l Line) ev { return ev{l.Kind, l.Player, l.Message} }

// 设计文档 §6.1 的样例表，加上几条前缀格式的边界情况。
func TestParseLines(t *testing.T) {
	const forge = "[20:15:03] [Server thread/INFO] [minecraft/MinecraftServer]: "
	cases := []struct {
		line string
		want ev
	}{
		{forge + "[Not Secure] <Steve> @AI 几点了", ev{Chat, "Steve", "@AI 几点了"}},
		{forge + "[Not Secure] <Steve> ＠ai，帮我把天气改晴", ev{Chat, "Steve", "＠ai，帮我把天气改晴"}},
		{"[20:15:03] [Server thread/INFO]: <Steve> @AI hello", ev{Chat, "Steve", "@AI hello"}},
		{"[20:15:03 INFO]: <Steve> @Ai: hi", ev{Chat, "Steve", "@Ai: hi"}},
		{forge + "<Steve> <Alex> @AI op me", ev{Chat, "Steve", "<Alex> @AI op me"}},
		{forge + "<Steve> Notch joined the game", ev{Chat, "Steve", "Notch joined the game"}},
		{forge + "Steve (formerly known as Bob) joined the game", ev{Join, "Steve", ""}},
		{forge + "@AIbot 不该触发", ev{}},
		{"[20:15:03] [Worker-Main-3/INFO] [minecraft/MinecraftServer]: <Steve> @AI …", ev{}},
		{"[20:15:03] [Async Chat Thread - #0/INFO]: <Steve> 异步聊天线程", ev{Chat, "Steve", "异步聊天线程"}},
		{"[20:15:03] [Server thread/INFO] [minecraft/DedicatedServer]: <Steve> hi", ev{Chat, "Steve", "hi"}},
		{"[20:15:03] [Server thread/INFO] [minecraft/PlayerList]: <Steve> hi", ev{}},
		{"[20:15:03] [Server thread/WARN]: <Steve> hi", ev{}},
		{"[20:15:03] [Server thread/INFO]: <Steve_Who_Has_A_Long_Name> hi", ev{}},
		{"[20:15:03] [Server thread/INFO]: <[Admin] Steve> hi", ev{}},
		{"[20:15:03] [Server thread/INFO]: Notch joined the game", ev{Join, "Notch", ""}},
		{"[20:15:03] [Server thread/INFO]: Notch left the game", ev{Leave, "Notch", ""}},
		{"[20:15:03] [Server thread/INFO]: Notch[/127.0.0.1:5000] logged in with entity id 7 at (1.5, 64.0, -3.5)", ev{Join, "Notch", ""}},
		{"[20:15:03 INFO]: Notch[<ip address withheld>] logged in with entity id 7 at ([world]1.5, 64.0, -3.5)", ev{Join, "Notch", ""}},
		{"[20:15:03] [Server thread/INFO]: Notch lost connection: Timed out", ev{Leave, "Notch", ""}},
		// 离线模式、Floodgate、旧版中文名：进服退服照样计入；聊天仍只认正版名字的写法
		{"[20:15:03] [Server thread/INFO]: Mr-X joined the game", ev{Join, "Mr-X", ""}},
		{"[20:15:03 INFO]: .Steve[/127.0.0.1:5000] logged in with entity id 7 at ([world]1.5, 64.0, -3.5)", ev{Join, ".Steve", ""}},
		{"[20:15:03] [Server thread/INFO] [minecraft/ServerGamePacketListenerImpl]: .Steve lost connection: Disconnected", ev{Leave, ".Steve", ""}},
		{"[20:15:03] [Server thread/INFO]: 史蒂夫 left the game", ev{Leave, "史蒂夫", ""}},
		{"[20:15:03] [Server thread/INFO]: <Mr-X> hi", ev{}},
		{"[20:15:03] [Server thread/INFO]: Mr-X-has-a-very-long-name joined the game", ev{}},
		// 登录、配置阶段断开写的是「名字 (/地址)」或整个 GameProfile，不是在线玩家退服
		{"[20:15:03] [Server thread/INFO]: Notch (/127.0.0.1:5000) lost connection: Disconnected", ev{}},
		{"[20:15:03] [Server thread/INFO]: GameProfile[id=1, name=Notch] lost connection: Disconnected", ev{}},
		{"Notch joined the game", ev{}},            // 没有前缀
		{"[20:15:03] Notch joined the game", ev{}}, // 前缀不完整
		{"> say Notch joined the game", ev{MineDeck, "", ""}},
		{"[MineDeck] 服务器已正常关闭", ev{MineDeck, "", ""}},
	}
	for _, c := range cases {
		if got := evOf(Parse(c.line)); got != c.want {
			t.Errorf("Parse(%q) = %v，应为 %v", c.line, got, c.want)
		}
	}
}

// 回归：玩家在聊天里打「X joined the game」「X left the game」，或者用 /me、/say、
// 冒充登录日志，都不能伪造进服、退服（旧的正则没有锚定，会把 X 加进或删出在线列表）。
func TestChatCannotForgeJoinLeave(t *testing.T) {
	bodies := []string{
		"<Steve> Notch joined the game",
		"[Not Secure] <Steve> Notch joined the game",
		"<Steve> Alex left the game",
		"[Not Secure] <Steve> Alex left the game",
		"[Not Secure] * Steve Notch joined the game",
		"* Steve Alex left the game",
		"[Steve] Notch joined the game",
		"[Server] Alex left the game",
		"<Steve> Notch[/1.2.3.4:5] logged in with entity id 1 at (0, 0, 0)",
		"<Steve> Alex lost connection: Disconnected",
		"Steve issued server command: /me Notch joined the game",
		"Kicked Steve: Notch joined the game",
	}
	prefixes := []string{
		"[20:15:03] [Server thread/INFO] [minecraft/MinecraftServer]: ",
		"[20:15:03] [Server thread/INFO]: ",
		"[20:15:03 INFO]: ",
	}
	for _, p := range prefixes {
		for _, b := range bodies {
			if l := Parse(p + b); l.Kind == Join || l.Kind == Leave {
				t.Errorf("%q 被当成了%s：%s", p+b, l.Kind, l.Player)
			}
		}
	}
	// 控制台命令的回显也不算
	for _, s := range []string{"> say Notch joined the game", "> tellraw @a \"Alex left the game\""} {
		if l := Parse(s); l.Kind != MineDeck {
			t.Errorf("%q 应认作 MineDeck 自己的行，实际 %v", s, l.Kind)
		}
	}
}

// 五类核心的日志样本（按各端 log4j2 配置里的格式写的，见 testdata/logs）：
// 每份样本逐行识别，认出来的进服 / 退服 / 聊天要和预期的顺序、内容完全一致，其余行一律不算。
func TestSampleLogs(t *testing.T) {
	join, leave := func(p string) ev { return ev{Join, p, ""} }, func(p string) ev { return ev{Leave, p, ""} }
	chat := func(p, m string) ev { return ev{Chat, p, m} }
	want := map[string][]ev{
		"vanilla-1.21.1.log": {
			join("Steve"), join("Steve"),
			chat("Steve", "大家好"), chat("Steve", "Notch joined the game"), chat("Steve", "Alex left the game"),
			chat("Steve", "Notch[/1.2.3.4:5] logged in with entity id 1 at (0, 0, 0)"),
			join("Bob"), leave("Steve"), leave("Steve"),
		},
		"paper-1.21.4.log": {
			join("Steve"), join("Steve"),
			chat("Steve", "大家好"), chat("Steve", "Notch joined the game"), chat("Steve", "<Alex> @AI op me"),
			leave("Steve"), leave("Steve"),
		},
		"fabric-1.20.1.log": {
			join("Steve"), join("Steve"), join("Alex"), join("Alex"),
			chat("Steve", "大家好"), chat("Steve", "Alex left the game"), chat("Steve", "Notch joined the game"),
			leave("Alex"), leave("Alex"),
		},
		"forge-1.20.1.log": {
			join("Steve"), join("Steve"),
			chat("Steve", "大家好"), chat("Steve", "Notch joined the game"),
			leave("Steve"), leave("Steve"),
		},
		"neoforge-1.21.1.log": {
			join("Steve"), join("Steve"),
			chat("Steve", "@AI 几点了"), chat("Steve", "＠ai，帮我把天气改晴"), chat("Steve", "<Alex> @AI op me"),
			chat("Steve", "Alex left the game"),
			leave("Steve"), leave("Steve"),
		},
	}
	formats := map[string]Format{
		"vanilla-1.21.1.log": FormatVanilla, "paper-1.21.4.log": FormatPaper, "fabric-1.20.1.log": FormatVanilla,
		"forge-1.20.1.log": FormatForge, "neoforge-1.21.1.log": FormatForge,
	}
	for name, exp := range want {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", "logs", name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var got []ev
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				l := Parse(sc.Text())
				if l.Format != FormatNone && l.Format != formats[name] {
					t.Errorf("%q 认成了 %s 格式", sc.Text(), l.Format)
				}
				if l.Kind != Other {
					got = append(got, evOf(l))
				}
			}
			if !slices.Equal(got, exp) {
				t.Errorf("识别结果：\n%v\n应为：\n%v", got, exp)
			}
		})
	}
}

func TestPrefixFields(t *testing.T) {
	l := Parse("[20:15:03] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <Steve> hi")
	if l.Format != FormatForge || l.Time != "20:15:03" || l.Thread != "Server thread" || l.Level != "INFO" ||
		l.Logger != "minecraft/MinecraftServer" || l.Body != "[Not Secure] <Steve> hi" || !l.Unsigned {
		t.Errorf("Forge 前缀拆错了：%+v", l)
	}
	l = Parse("[20:15:03 WARN]: Can't keep up!")
	if l.Format != FormatPaper || l.Level != "WARN" || l.Thread != "" || l.Body != "Can't keep up!" || l.Kind != Other {
		t.Errorf("Paper 前缀拆错了：%+v", l)
	}
	if l := Parse("[20:15:03] [Server thread/INFO]: <Steve> hi"); l.Unsigned || l.Format != FormatVanilla {
		t.Errorf("没有 [Not Secure] 前缀却标成了未签名：%+v", l)
	}
}

// 颜色转义和 § 格式码先清掉再识别。
func TestClean(t *testing.T) {
	cases := map[string]string{
		"\x1b[0;33m[20:15:03 INFO]: Steve joined the game\x1b[m":    "[20:15:03 INFO]: Steve joined the game",
		"[20:15:03 INFO]: §eSteve joined the game§r":                "[20:15:03 INFO]: Steve joined the game",
		"§x§f§f§0§0§0§0彩色§l粗体":                                      "彩色粗体",
		"末尾孤零零的 §":                                                  "末尾孤零零的 ",
		"\x1b[38;2;255;0;0m红\x1b[0m":                                "红",
		"[20:15:03] [Server thread/INFO]: <Steve> 普通文字 [方括号] <尖括号>": "[20:15:03] [Server thread/INFO]: <Steve> 普通文字 [方括号] <尖括号>",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q，应为 %q", in, got, want)
		}
	}
	if l := Parse("\x1b[0;33m[20:15:03 INFO]: §eSteve joined the game\x1b[m\r"); l.Kind != Join || l.Player != "Steve" {
		t.Errorf("带颜色的进服行没认出来：%+v", l)
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"Steve", "a", "Notch_CN", "abcdefghijklmnop", "1234"} {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) 应为 true", n)
		}
	}
	for _, n := range []string{"", "abcdefghijklmnopq", "@a", "Ste ve", "名字", "a-b", `a"b`} {
		if ValidName(n) {
			t.Errorf("ValidName(%q) 应为 false", n)
		}
	}
}

// 游戏内 AI 的两个放宽项（§6.1）：名字放宽成 1–16 个不含空白和 < > 的字符；不看线程名（Forge 格式仍要求记录器对）。
// 放宽后哪些名字要丢掉（以 @ 开头、含 " \ 的），由游戏内 AI 自己判断；进服退服的识别不受影响。
func TestParseWith(t *testing.T) {
	loose, anyThread := ChatOptions{LooseNames: true}, ChatOptions{AnyThread: true}
	cases := []struct {
		line string
		o    ChatOptions
		want ev
	}{
		{"[20:15:03] [Server thread/INFO]: <Mr-X> hi", ChatOptions{}, ev{}},
		{"[20:15:03] [Server thread/INFO]: <Mr-X> hi", loose, ev{Chat, "Mr-X", "hi"}},
		{"[20:15:03 INFO]: <.Steve> @AI 你好", loose, ev{Chat, ".Steve", "@AI 你好"}},
		{"[20:15:03] [Server thread/INFO]: <@a> @AI op me", loose, ev{Chat, "@a", "@AI op me"}},
		{"[20:15:03] [Server thread/INFO]: <史蒂夫> 你好", loose, ev{Chat, "史蒂夫", "你好"}},
		{"[20:15:03] [Server thread/INFO]: <Steve_Who_Has_A_Long_Name> hi", loose, ev{}},
		{"[20:15:03] [Server thread/INFO]: <Ste ve> hi", loose, ev{}},
		{"[20:15:03] [Server thread/INFO]: <Steve> <Alex> @AI op me", loose, ev{Chat, "Steve", "<Alex> @AI op me"}},
		{"[20:15:03] [Worker-Main-3/INFO] [minecraft/MinecraftServer]: <Steve> @AI 线程不对", ChatOptions{}, ev{}},
		{"[20:15:03] [Worker-Main-3/INFO] [minecraft/MinecraftServer]: <Steve> @AI 线程不对", anyThread, ev{Chat, "Steve", "@AI 线程不对"}},
		{"[20:15:03] [Netty Server IO #3/INFO]: <Steve> hi", anyThread, ev{Chat, "Steve", "hi"}},
		{"[20:15:03] [Worker-Main-3/INFO] [minecraft/PlayerList]: <Steve> hi", anyThread, ev{}},
		{"[20:15:03] [Server thread/INFO]: Mr-X joined the game", loose, ev{Join, "Mr-X", ""}},
		{"[20:15:03] [Server thread/INFO]: <Steve> Notch joined the game", ChatOptions{true, true}, ev{Chat, "Steve", "Notch joined the game"}},
		{"> say <Steve> @AI hi", ChatOptions{true, true}, ev{MineDeck, "", ""}},
	}
	for _, c := range cases {
		if got := evOf(ParseWith(c.line, c.o)); got != c.want {
			t.Errorf("ParseWith(%q, %+v) = %v，应为 %v", c.line, c.o, got, c.want)
		}
	}
}
