package tunnel

import (
	"bufio"
	"bytes"
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
	for _, cred := range []string{"1", "abc", "error", "失", "*", "…"} {
		mask := credMasker(cred)
		if got := mask("failed: token=" + cred); strings.Contains(got, cred) {
			t.Errorf("短凭据没有隐藏：%q => %q", cred, got)
		}
		plain := "正常连接已经建立"
		if !strings.Contains(plain, cred) && mask(plain) != plain {
			t.Error("无凭据的行应当保留")
		}
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

func TestShortCredentialMaskPreservesControlSignals(t *testing.T) {
	tempBase(t)
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "short", Credential: "1", ProxyID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	tun := m.get(tt.ID)
	con := m.Console(tt.ID)
	var replies bytes.Buffer
	rs := &runState{stdin: bufio.NewWriter(&replies)}
	lines := "2026/11/14 同意服务条款，请输入 y/n\n2026/11/14 start proxy success token=1\n2026/11/14 invalid token=1\n"
	m.pumpOutput(tun, rs, con, strings.NewReader(lines), credMasker("1"))
	if replies.String() != "y\n" || !rs.online {
		t.Fatalf("脱敏改变了控制信号：reply=%q online=%v", replies.String(), rs.online)
	}
	if got := strings.Join(con.Lines(), "\n"); strings.Contains(got, "1") {
		t.Fatalf("控制台含短凭据：%q", got)
	}
	if err := m.lastError(tt.ID); err == "" || strings.Contains(err, "1") {
		t.Fatalf("错误文字未脱敏：%q", err)
	}
}
