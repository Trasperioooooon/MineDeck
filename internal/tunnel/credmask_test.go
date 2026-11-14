package tunnel

import (
	"bufio"
	"io"
	"net/url"
	"strings"
	"testing"
)

// frpc 的输出进控制台缓冲和卡片上的报错之前，先遮掉这次启动用的凭据（2026-11-14 Codex 复审第 6 条）。
// 这两处局域网登录的设备都看得到；OpenFrp、樱花的凭据是放在命令行参数里交给 frpc 的。
func TestFrpcOutputMasksCredential(t *testing.T) {
	tempBase(t)
	const cred = "sk-Secret+Token/123=中"
	m := NewManager()
	added, err := m.Add(Tunnel{Provider: "natfrp", Name: "樱花", Credential: cred, ProxyID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	tun := m.get(added.ID)
	if tun == nil {
		t.Fatal("找不到刚加的隧道")
	}
	con := m.Console(added.ID)
	rs := &runState{stdin: bufio.NewWriter(io.Discard)}

	// 假装 frpc 在各种地方把凭据打了出来：原样的、和编号拼在一起的、转义过的、放进网址里的
	out := strings.Join([]string{
		"2026/11/14 10:00:00 [I] start frpc -f " + cred + ":9",
		"2026/11/14 10:00:01 [E] login to server failed: invalid token " + cred,
		"[W] config token = \"" + tomlEscape(cred) + "\"",
		"[E] GET https://api.example/auth?token=" + url.QueryEscape(cred) + " failed",
		"[I] 普通的一行，不带凭据",
	}, "\r\n")
	m.pumpOutput(tun, rs, con, strings.NewReader(out), credMasker(cred))

	lines := con.Lines()
	if len(lines) != 5 {
		t.Fatalf("应当有 5 行进了控制台，得到 %d 行：%q", len(lines), lines)
	}
	all := strings.Join(lines, "\n")
	for _, leak := range []string{cred, "Secret+Token", tomlEscape(cred), url.QueryEscape(cred), "Secret%2BToken"} {
		if strings.Contains(all, leak) {
			t.Errorf("控制台里还有凭据（%q）：\n%s", leak, all)
		}
	}
	if !strings.Contains(lines[0], "start frpc -f ***:9") || lines[4] != "[I] 普通的一行，不带凭据" {
		t.Errorf("遮完以后别的内容应当原样留着：%q", lines)
	}
	// 带 error / failed 的行会记成卡片上的报错：同样不带凭据，但报错本身还在
	le := m.lastError(added.ID)
	if le == "" || strings.Contains(le, "Secret") || strings.Contains(le, "%2BToken") {
		t.Errorf("卡片上的报错应当留着、但不带凭据：%q", le)
	}
}

func TestCredMasker(t *testing.T) {
	// 太短的凭据不遮：日志里到处都会碰巧出现，遮了日志没法看，而且哪几个字符被换掉了一眼看得出来，藏不住。
	// 这是有意的取舍，不是这道边界已经关上了：Codex 第二轮复审建议全遮，没有照做，交作者定
	short := credMasker("abc")
	if got := short("abc abcdef"); got != "abc abcdef" {
		t.Errorf("短凭据不该遮：%q", got)
	}
	if got := credMasker("")("anything"); got != "anything" {
		t.Errorf("没有凭据时原样返回：%q", got)
	}
	// 前后的空白不算凭据的一部分（保存时就去掉了）
	if got := credMasker("  tokentoken  ")("x tokentoken y"); got != "x *** y" {
		t.Errorf("得到 %q", got)
	}
	// 一行里出现几次都遮
	if got := credMasker("tokentoken")("tokentoken/tokentoken"); got != "***/***" {
		t.Errorf("得到 %q", got)
	}
}
