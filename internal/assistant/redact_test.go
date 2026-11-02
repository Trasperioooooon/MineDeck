package assistant

import (
	"strconv"
	"strings"
	"testing"
)

func TestRedactText(t *testing.T) {
	r := &Redactor{Base: `D:\Games\MineDeck`, Home: `C:\Users\Alice`, User: "Alice", Computer: "DESKTOP-7QK2",
		IPs: []string{"192.168.1.23", "2408:8207:1234::5", "58.33.1.2"}, Secrets: func() []string { return []string{"md-token-0011223344", "x"} }}
	cases := []struct{ in, want string }{
		// 程序目录、主目录、别的用户的路径（两种斜杠、JSON 里转义过的反斜杠、大小写）
		{`D:\Games\MineDeck\servers\s1\logs`, `<程序目录>\servers\s1\logs`},
		{`d:/games/minedeck/runtimes/java-21`, `<程序目录>/runtimes/java-21`},
		{`C:\Users\Alice\AppData\Roaming`, `<用户目录>\AppData\Roaming`},
		{`file:/C:/Users/bob/.minecraft/mods`, `file:/C:/Users/<用户名>/.minecraft/mods`},
		{`"path":"C:\\Users\\carol\\Desktop"`, `"path":"C:\\Users\\<用户名>\\Desktop"`},
		{`/home/dave/server`, `/home/<用户名>/server`},
		{`USERNAME=Alice COMPUTERNAME=DESKTOP-7QK2 user.name=alice`, `USERNAME=<用户名> COMPUTERNAME=<计算机名> user.name=<用户名>`},
		{`连接来自 desktop-7qk2`, `连接来自 <计算机名>`},
		// 局域网 IP 换掉，公网 IP、回环和 0.0.0.0 保留；本机网卡的地址（哪怕是公网的）也换掉
		{`bind 192.168.1.23:25565 / 10.1.2.3 / 172.20.0.9 / 100.101.1.1 / 169.254.3.4`, `bind <局域网IP>:25565 / <局域网IP> / <局域网IP> / <局域网IP> / <局域网IP>`},
		{`8.8.8.8 127.0.0.1 0.0.0.0 172.32.0.1 58.33.1.2`, `8.8.8.8 127.0.0.1 0.0.0.0 172.32.0.1 <局域网IP>`},
		{`fe80::1c2:3%eth0 fd12:3456::1 2408:8207:1234::5 ::1 2001:4860::8888`, `<局域网IP> <局域网IP> <局域网IP> ::1 2001:4860::8888`},
		{`[20:15:03] time 20:15:03 mac 00:1a:2b:3c:4d:5e`, `[20:15:03] time 20:15:03 mac 00:1a:2b:3c:4d:5e`},
		// 已知的密钥、长得像 Key 的长串
		{`token md-token-0011223344 here`, `token *** here`},
		{`key sk-abcdefghijklmnop1234 and sk-ant-api03-AbCdEfGhIjKlMnOpQrSt`, `key *** and ***`},
		{`$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy`, `***`},
		{`ghp_0123456789abcdefghijABCDEFGHIJ`, `***`},
		{`id 0123456789abcdef0123456789abcdef.AbCdEfGh12345678`, `id ***`},
		{`aGVsbG8gd29ybGQgdGhpcyBpcyBhIHNlY3JldCBrZXkxMjM0NTY3ODk=`, `***`},
		// 类名、哈希、UUID、版本号不动
		{`net.minecraftforge.fml.loading.moddiscovery.ModFileParser`, `net.minecraftforge.fml.loading.moddiscovery.ModFileParser`},
		{`sha1 da39a3ee5e6b4b0d3255bfef95601890afd80709 uuid 069a79f4-44e9-4726-a5be-fca90e38aaf5`, `sha1 da39a3ee5e6b4b0d3255bfef95601890afd80709 uuid 069a79f4-44e9-4726-a5be-fca90e38aaf5`},
		{`AbstractContainerScreenMixinAccessorImplementationForTheWin`, `AbstractContainerScreenMixinAccessorImplementationForTheWin`},
		// 配置里像密码的值
		{"token = \"discord-bot-xyz\"\nchannel = \"general\"", "token = \"***\"\nchannel = \"general\""},
		{"  \"apiKey\": \"abc123\",\n  \"enabled\": true", "  \"apiKey\": \"***\",\n  \"enabled\": true"},
		{"rcon.password=hunter2\nmax-players=20", "rcon.password=***\nmax-players=20"},
		{"requireToken = false\ntoken = \"\"\nsecretLength = 32", "requireToken = false\ntoken = \"\"\nsecretLength = 32"},
	}
	for _, c := range cases {
		if got := r.Text(c.in); got != c.want {
			t.Errorf("\n输入 %q\n得到 %q\n应为 %q", c.in, got, c.want)
		}
	}
}

// JSON 值：按键名把密钥类的值写成 ***（同操作记录的规则），每个字符串再做文本脱敏。
func TestRedactJSON(t *testing.T) {
	r := &Redactor{Base: `C:\MineDeck`, IPs: []string{"192.168.0.8"}}
	got := r.JSON(map[string]any{
		"base":   `C:\MineDeck`,
		"ips":    []string{"192.168.0.8"},
		"config": map[string]any{"webhookUrl": "https://hook/x?t=1", "cfApiKey": "", "source": "auto"},
		"pairs":  []map[string]string{{"key": "rcon.password", "value": "hunter2"}, {"key": "motd", "value": "<b>hi</b> & bye"}},
	})
	want := `{"base":"<程序目录>","config":{"cfApiKey":"","source":"auto","webhookUrl":"***"},"ips":["<局域网IP>"],` +
		`"pairs":[{"key":"rcon.password","value":"***"},{"key":"motd","value":"<b>hi</b> & bye"}]}`
	if got != want {
		t.Errorf("\n得到 %s\n应为 %s", got, want)
	}
	if s := r.JSON("纯文本"); s != `"纯文本"` {
		t.Errorf("字符串：%s", s)
	}
	var nilR *Redactor
	if nilR.Text("x") != "x" || strings.Contains((&Redactor{}).Text("192.168.1.1"), "192.168") {
		t.Error("零值的 Redactor 也要处理通用规则")
	}
}

// 交还给 AI 的结果超过 16 KB：带 lines 的从最旧的行删起，其余的截掉后面并注明。
func TestContentTruncation(t *testing.T) {
	s := &Service{redact: &Redactor{}}
	var lines []string
	for i := 0; i < 2000; i++ {
		lines = append(lines, strings.Repeat("x", 20)+" 第"+strconv.Itoa(i)+"行")
	}
	got := s.content(map[string]any{"lines": lines, "status": "running"})
	if len(got) > maxResult+200 || !strings.Contains(got, "第1999行") || strings.Contains(got, " 第0行") || strings.Contains(got, " 第500行") || !strings.Contains(got, "前面") {
		t.Errorf("行太多时应留下最新的：%d 字节，%s…", len(got), got[:200])
	}
	big := strings.Repeat("配置", 10000)
	got = s.content(map[string]any{"content": big})
	if !strings.Contains(got, "已截断") || !strings.HasPrefix(got, dataBegin) || !strings.HasSuffix(got, dataEnd) || len(got) > maxResult+400 {
		t.Errorf("其余的截断并注明：%d 字节", len(got))
	}
}
