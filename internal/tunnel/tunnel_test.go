package tunnel

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"minedeck/internal/app"
	"minedeck/internal/events"
	"minedeck/internal/secret"
)

// tempBase 把程序目录和运行时目录换成临时的一份，测试结束后还原。
func tempBase(t *testing.T) {
	t.Helper()
	oldBase, oldRT := app.Base, app.RuntimesDir
	t.Cleanup(func() { app.Base, app.RuntimesDir = oldBase, oldRT })
	app.Base = t.TempDir()
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const nasty = `tok+en&"q'<x>\y 中文😀` // 含 html/template 会转义的字符、反斜杠、中文和 emoji

// 新增、修改隧道后 tunnels.json 里只有 DPAPI 密文；列表接口不下发凭据；凭据留空表示不修改。
func TestCredentialsEncryptedAtRest(t *testing.T) {
	tempBase(t)
	m := NewManager()
	a, err := m.Add(Tunnel{Provider: "openfrp", Name: "朋友联机", Credential: "openfrp-user-key-123", ProxyID: "42", LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	if a.Credential != "" {
		t.Fatal("新增的响应不应回显凭据")
	}
	c, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: nasty, ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	disk := readFile(t, filepath.Join(app.Base, "tunnels.json"))
	if strings.Contains(disk, "openfrp-user-key-123") || strings.Contains(disk, "tok+en") || strings.Count(disk, `"dpapi:`) != 2 {
		t.Fatalf("tunnels.json 里有明文凭据：%s", disk)
	}
	for _, st := range m.List() {
		if st.Credential != "" {
			t.Fatalf("列表下发了凭据：%+v", st)
		}
	}
	// 留空不修改；填了就换成新的（同样加密）
	if err := m.Update(c.ID, Tunnel{Provider: "custom", Name: "自建", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25567, LocalPort: 25565}); err != nil {
		t.Fatal(err)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != nasty {
		t.Fatalf("凭据留空修改后凭据变了：%q", got)
	}
	if err := m.Update(c.ID, Tunnel{Provider: "custom", Name: "自建", Credential: "new-token", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25567, LocalPort: 25565}); err != nil {
		t.Fatal(err)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "new-token" {
		t.Fatalf("新凭据没存上：%q", got)
	}
	if disk := readFile(t, filepath.Join(app.Base, "tunnels.json")); strings.Contains(disk, "new-token") {
		t.Fatal("修改后 tunnels.json 里有明文凭据")
	}
}

// 旧版 tunnels.json 的明文凭据：启动时加密写回；旧版留下的、写着明文 token 的 frpc 配置文件一并删掉。
func TestMigratePlaintextTunnels(t *testing.T) {
	tempBase(t)
	legacy := `{"seq":2,"tunnels":[
{"id":"tun1","provider":"natfrp","name":"樱花","credential":"sakura-access-key","proxyId":"9","localPort":25565,"autoStart":false},
{"id":"tun2","provider":"custom","name":"自建","credential":"custom-frps-token","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := frpcDir("custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "serverAddr = \"1.2.3.4\"\nserverPort = 7000\nloginFailExit = false\nauth.token = \"custom-frps-token\"\n"
	sample := "serverAddr = \"127.0.0.1\"\nserverPort = 7000\n" // frp 发行包自带的示例配置，不是我们写的
	for name, body := range map[string]string{"tun2.toml": old, "frpc.toml": sample} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager()
	disk := readFile(t, filepath.Join(app.Base, "tunnels.json"))
	if strings.Contains(disk, "sakura-access-key") || strings.Contains(disk, "custom-frps-token") || strings.Count(disk, `"dpapi:`) != 2 {
		t.Fatalf("迁移后 tunnels.json 仍有明文：%s", disk)
	}
	for id, want := range map[string]string{"tun1": "sakura-access-key", "tun2": "custom-frps-token"} {
		if got, err := secret.Unseal(m.get(id).Credential); got != want || err != nil {
			t.Fatalf("%s 迁移后解出 %q, %v", id, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "tun2.toml")); !os.IsNotExist(err) {
		t.Fatal("旧版带明文 token 的 frpc 配置没有删掉")
	}
	if _, err := os.Stat(filepath.Join(dir, "frpc.toml")); err != nil {
		t.Fatal("不该删 frp 自带的示例配置")
	}
	// 迁移结果再读一遍也一样（不会重复加密）
	if m2 := NewManager(); m2.get("tun2").Credential != m.get("tun2").Credential {
		t.Fatal("重启后凭据又被改写了")
	}
}

// 换了电脑或 Windows 用户：凭据解不开，列表里标出来，启动直接报错（不去下载 frpc、不起进程）。
func TestUnreadableCredential(t *testing.T) {
	tempBase(t)
	data := `{"seq":1,"tunnels":[{"id":"tun1","provider":"custom","name":"自建","credential":"dpapi:AAAA","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if l := m.List(); len(l) != 1 || !l[0].CredBroken {
		t.Fatalf("应标出凭据解不开：%+v", l)
	}
	err := m.Start("tun1")
	if err == nil || !strings.Contains(err.Error(), "读不出来") {
		t.Fatalf("应拒绝启动：%v", err)
	}
	if _, err := os.Stat(frpcDir("custom")); !os.IsNotExist(err) {
		t.Fatal("凭据解不开时不应去准备 frpc")
	}
	// 重填凭据后恢复正常
	if err := m.Update("tun1", Tunnel{Provider: "custom", Name: "自建", Credential: "fresh", ServerAddr: "1.2.3.4", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}); err != nil {
		t.Fatal(err)
	}
	if l := m.List(); l[0].CredBroken {
		t.Fatal("重填后仍标着解不开")
	}
}

var escRe = regexp.MustCompile(`\\u([0-9A-F]{4})|\\U([0-9A-F]{8})`)

// renderLikeFrp 按 frp 0.61.1 读配置的办法处理：先用 html/template 代入环境变量，再还原 TOML 字符串里的 \u 转义，
// 取出 auth.token 的值。
func renderLikeFrp(t *testing.T, cfg string, env []string) (token string, ok bool) {
	t.Helper()
	envs := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		envs[k] = v
	}
	tpl, err := template.New("frp").Parse(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, map[string]any{"Envs": envs}); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^auth\.token = "([^"]*)"$`).FindStringSubmatch(out.String())
	if m == nil {
		return "", false
	}
	return escRe.ReplaceAllStringFunc(m[1], func(s string) string {
		n, _ := strconv.ParseUint(s[2:], 16, 32)
		return string(rune(n))
	}), true
}

// 自定义 frps：配置文件里不再有 token，只有环境变量模板；token 经环境变量传给 frpc，按 frp 的渲染办法能原样还原。
func TestCustomFrpcConfigHasNoToken(t *testing.T) {
	tempBase(t)
	tun := &Tunnel{ID: "tun7", Provider: "custom", Credential: nasty, ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}
	args, env, err := customProvider{}.Launch(tun)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "-c" {
		t.Fatalf("启动参数不对：%v", args)
	}
	for _, a := range args {
		if strings.Contains(a, "tok+en") {
			t.Fatal("token 出现在命令行参数里")
		}
	}
	cfg := readFile(t, args[1])
	if strings.Contains(cfg, "tok+en") || strings.Contains(cfg, "中文") || !strings.HasPrefix(cfg, `auth.token = "{{ .Envs.MINEDECK_FRP_TOKEN }}"`) {
		t.Fatalf("配置文件里有明文 token，或模板不在第一行：\n%s", cfg)
	}
	if len(env) != 1 || !strings.HasPrefix(env[0], "MINEDECK_FRP_TOKEN=") || strings.Contains(env[0], "tok+en") {
		t.Fatalf("环境变量不对：%v", env)
	}
	if got, ok := renderLikeFrp(t, cfg, env); !ok || got != nasty {
		t.Fatalf("按 frp 的办法渲染出的 token = %q（%v），应为 %q", got, ok, nasty)
	}
	// 直接把原文放进环境变量会被 html/template 转义（这就是要先转成 \u 的原因）
	if got, _ := renderLikeFrp(t, cfg, []string{"MINEDECK_FRP_TOKEN=a+b&c"}); got == "a+b&c" {
		t.Fatal("预期 html/template 会转义 + 和 &")
	}
	// frps 没设 token：不写 auth.token，也不传环境变量
	tun.Credential = ""
	args, env, err = customProvider{}.Launch(tun)
	if err != nil || len(env) != 0 || strings.Contains(readFile(t, args[1]), "auth.token") {
		t.Fatalf("没有 token 时：env=%v err=%v\n%s", env, err, readFile(t, args[1]))
	}
}

// tunnels.json 的整体结构不变（老版本读得懂、字段没少）。
func TestTunnelsJSONShape(t *testing.T) {
	tempBase(t)
	m := NewManager()
	if _, err := m.Add(Tunnel{Provider: "openfrp", Name: "a", Credential: "k", ProxyID: "1", LocalPort: 25565}); err != nil {
		t.Fatal(err)
	}
	var data struct {
		Seq     int              `json:"seq"`
		Tunnels []map[string]any `json:"tunnels"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(app.Base, "tunnels.json"))), &data); err != nil {
		t.Fatal(err)
	}
	if data.Seq != 1 || len(data.Tunnels) != 1 || data.Tunnels[0]["id"] != "tun1" || data.Tunnels[0]["proxyId"] != "1" {
		t.Fatalf("tunnels.json 结构变了：%+v", data)
	}
}

// 用途（第五阶段）：老数据没有这一项按「游戏」算；语音隧道照样存取；写错了报错。自定义 frps 可以选 UDP
// （语音隧道用），配置里生成 type = "udp"；樱花、OpenFrp 的协议在服务商后台定，传了也清掉。
func TestTunnelPurposeAndProto(t *testing.T) {
	tempBase(t)
	legacy := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"老隧道","credential":"","proxyId":"9","localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	voice, err := m.Add(Tunnel{Provider: "natfrp", Name: "语音", Credential: "k", ProxyID: "10", Purpose: PurposeVoice, Proto: "udp", Bound: "生存服", AutoStart: true})
	if err != nil || voice.Purpose != PurposeVoice || voice.Proto != "" {
		t.Fatalf("加语音隧道：%+v %v", voice, err)
	}
	udp, err := m.Add(Tunnel{Provider: "custom", Name: "自建语音", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 24454, LocalPort: 24454, Purpose: PurposeVoice, Proto: "udp"})
	if err != nil || udp.Proto != "udp" {
		t.Fatalf("加自定义 UDP 隧道：%+v %v", udp, err)
	}
	tcp, err := m.Add(Tunnel{Provider: "custom", Name: "自建游戏", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil || tcp.Proto != "tcp" || tcp.Purpose != PurposeGame {
		t.Fatalf("自定义隧道默认 TCP、用途游戏：%+v %v", tcp, err)
	}
	got := map[string]string{}
	for _, st := range m.List() {
		got[st.Name] = st.Purpose
	}
	if got["老隧道"] != PurposeGame || got["语音"] != PurposeVoice || got["自建语音"] != PurposeVoice {
		t.Fatalf("列表里的用途：%v", got)
	}
	if _, err := m.Add(Tunnel{Provider: "natfrp", Name: "x", Credential: "k", ProxyID: "1", Purpose: "music"}); err == nil || !strings.Contains(err.Error(), "未知的用途") {
		t.Fatalf("写错用途应报错：%v", err)
	}
	if _, err := m.Add(Tunnel{Provider: "custom", Name: "x", ServerAddr: "a", ServerPort: 1, RemotePort: 2, LocalPort: 3, Proto: "quic"}); err == nil {
		t.Fatal("自定义 frps 的协议写错应报错")
	}
	for tun, want := range map[*Tunnel]string{udp: `type = "udp"`, tcp: `type = "tcp"`} {
		args, _, err := customProvider{}.Launch(tun)
		if err != nil {
			t.Fatal(err)
		}
		if cfg := readFile(t, args[1]); !strings.Contains(cfg, want) || !strings.Contains(cfg, "localPort = "+strconv.Itoa(tun.LocalPort)) {
			t.Fatalf("%s 的配置应有 %s：\n%s", tun.Name, want, cfg)
		}
	}
}

// 拿到公网地址时发 tunnel.up，带上隧道编号（音乐共享按编号找到绑定的实例，把语音隧道的地址写进 voice_host）。
func TestTunnelUpEventHasID(t *testing.T) {
	tempBase(t)
	got := make(chan map[string]any, 4)
	events.Subscribe(func(e events.Event) {
		if e.Type == "tunnel.up" {
			got <- e.Data
		}
	})
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "语音", Credential: "k", ProxyID: "10", Purpose: PurposeVoice})
	if err != nil {
		t.Fatal(err)
	}
	m.handleLine(m.get(tt.ID), &runState{}, "2026/10/28 20:00:00 [I] [语音] UDP 隧道启动成功，使用 frp-abc.natfrp.cloud:12345 来连接")
	select {
	case d := <-got:
		if d["id"] != tt.ID || d["addr"] != "frp-abc.natfrp.cloud:12345" || d["tunnel"] != "语音" {
			t.Fatalf("事件内容：%v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("没有收到 tunnel.up")
	}
	if st := m.List()[0]; st.PublicAddr != "frp-abc.natfrp.cloud:12345" {
		t.Fatalf("公网地址：%+v", st)
	}
}

// 开了跟随启动的隧道跟着实例停（测试报告 C-03）：停服后等一会儿还是停着就停掉；没勾跟随启动的、别的实例的不动；
// 重启（停了马上又开）不来回开关。
func TestFollowInstancesStop(t *testing.T) {
	tempBase(t)
	old := stopGrace
	stopGrace = 50 * time.Millisecond
	t.Cleanup(func() { stopGrace = old })
	m := NewManager()
	add := func(name, bound string, auto bool) string {
		tt, err := m.Add(Tunnel{Provider: "natfrp", Name: name, Credential: "k", ProxyID: "1", Bound: bound, AutoStart: auto})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("ping", "-n", "30", "127.0.0.1") // 替 frpc 占着一个进程
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		m.mu.Lock()
		m.running[tt.ID] = &runState{cmd: cmd}
		m.mu.Unlock()
		return tt.ID
	}
	follow, manual, other := add("跟随", "S1", true), add("手动", "S1", false), add("别的服", "S2", true)
	notes := make(chan AutoAction, 8)
	m.SetAutoHook(func(a AutoAction) { notes <- a })
	status := map[string]string{"S1": "stopped", "S2": "starting"} // S2：重启中，已经又开起来了
	st := func(n string) string { return status[n] }
	m.follow(events.Event{Type: "instance.stop", Data: map[string]any{"instance": "S1"}}, st)
	m.follow(events.Event{Type: "instance.crash", Data: map[string]any{"instance": "S2"}}, st)
	time.Sleep(300 * time.Millisecond)
	stopped := func(id string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.running[id].stopped }
	if !stopped(follow) {
		t.Error("S1 停了，跟随启动的隧道应跟着停")
	}
	if stopped(manual) {
		t.Error("没勾跟随启动的隧道不该停")
	}
	if stopped(other) {
		t.Error("S2 重启中（又开起来了），隧道不该停")
	}
	if !strings.Contains(strings.Join(m.Console(follow).Lines(), "\n"), "服务器「S1」停了，隧道跟着停止") {
		t.Errorf("隧道日志里应说明为什么停：%q", m.Console(follow).Lines())
	}
	// 跟着停的那一条记一条操作记录（tunnel.stop，「跟随实例」，带实例名）；没停的不记
	if len(notes) != 1 {
		t.Fatalf("应只记一条操作记录，实际 %d 条", len(notes))
	}
	if a := <-notes; a.Op != "tunnel.stop" || a.Actor != "跟随实例" || a.Err != nil || a.Args["id"] != follow || a.Args["inst"] != "S1" || a.Args["tunnel"] != "跟随" {
		t.Errorf("跟随停止的操作记录：%+v", a)
	}
}

// 跟随启动失败（首次使用樱花还没放 frpc）：记一条失败的 tunnel.start 操作记录（带错误），失败原因记进 lastError，
// 卡片上直接显示，说明是「跟随启动」失败的；手动启动失败的说法是「启动失败」（测试报告第五轮）。
func TestFollowStartFailureNotedAndShown(t *testing.T) {
	tempBase(t)
	old := startDelay
	startDelay = 10 * time.Millisecond
	t.Cleanup(func() { startDelay = old })
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "游戏", Credential: "k", ProxyID: "10", Bound: "S1", AutoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })
	m.follow(events.Event{Type: "instance.start", Data: map[string]any{"instance": "S1"}}, func(string) string { return "starting" })
	select {
	case a := <-notes:
		if a.Op != "tunnel.start" || a.Actor != "跟随实例" || a.Err == nil || !strings.Contains(a.Err.Error(), "首次使用需手动放置樱花 frpc") ||
			a.Args["id"] != tt.ID || a.Args["inst"] != "S1" || a.Args["tunnel"] != "游戏" {
			t.Fatalf("跟随启动失败的操作记录：%+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("跟随启动失败没有记操作记录")
	}
	le := m.List()[0].LastError
	if !strings.HasPrefix(le, "跟随启动失败：首次使用需手动放置樱花 frpc") || !strings.Contains(le, "目录后重试") || !utf8.ValidString(le) {
		t.Fatalf("卡片上的失败原因：%q", le)
	}
	// 手动启动失败：说法是「启动失败」，同样记下
	if err := m.Start(tt.ID); err == nil {
		t.Fatal("没有 frpc 时应启动失败")
	}
	if le := m.List()[0].LastError; !strings.HasPrefix(le, "启动失败：首次使用需手动放置樱花 frpc") {
		t.Fatalf("手动启动失败的原因：%q", le)
	}
}

// 失败原因按字符截断，不会切在半个汉字上（截断前是按字节截的，下发时末尾成了乱码）。
func TestLastErrorTruncatesByRune(t *testing.T) {
	m := &Manager{lastErr: map[string]string{}}
	m.setLastErr("tun1", strings.Repeat("目录", 200))
	le := m.lastError("tun1")
	if !utf8.ValidString(le) || utf8.RuneCountInString(le) != 301 || !strings.HasSuffix(le, "…") {
		t.Fatalf("截断后：%d 个字，合法 UTF-8：%v", utf8.RuneCountInString(le), utf8.ValidString(le))
	}
}

// frpc 跑起来时清掉上一次的失败原因；没报错就自己退出时，记一条「意外退出」（卡片上看得到，不用点开日志）。
func TestUnexpectedExitShownAsLastError(t *testing.T) {
	tempBase(t)
	ping, err := exec.LookPath("ping") // 替 frpc：参数不对，马上退出
	if err != nil {
		t.Skip("没有 ping")
	}
	b, err := os.ReadFile(ping)
	if err != nil {
		t.Skip(err)
	}
	if err := os.MkdirAll(frpcDir("custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frpcDir("custom"), "frpc.exe"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "custom", Name: "自建", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	m.setLastErr(tt.ID, "上一次的错误")
	if err := m.Start(tt.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := m.List()[0]
		if !st.Running && st.LastError != "" && st.LastError != "上一次的错误" {
			t.Logf("frpc 退出后卡片上的原因：%s", st.LastError) // ping 的输出里没有报错字样时是「frpc 意外退出（退出码 N）…」
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frpc 退出后的状态：%+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// 樱花同时给域名和节点 IP 两种地址时用域名（测试报告第五轮：IP 会随节点变），两行谁先谁后都一样；
// 新的一次运行只给了 IP（换了节点）就用 IP，不卡在上次的域名上。
func TestPublicAddrPrefersDomain(t *testing.T) {
	tempBase(t)
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "游戏", Credential: "k", ProxyID: "10"})
	if err != nil {
		t.Fatal(err)
	}
	tun := m.get(tt.ID)
	const (
		domain = "2026/11/02 15:31:27 [I] [游戏] 使用 >>frp-abc.natfrp.cloud:12345<< 连接你的隧道"
		ip     = "2026/11/02 15:31:27 [I] [游戏] 或使用节点 IP 连接（不推荐）>>203.0.113.7:12345<<"
	)
	addr := func() string { return m.List()[0].PublicAddr }
	for _, order := range [][]string{{domain, ip}, {ip, domain}} {
		rs := &runState{} // 每种顺序算一次新的运行
		for _, l := range order {
			m.handleLine(tun, rs, l)
		}
		if got := addr(); got != "frp-abc.natfrp.cloud:12345" {
			t.Fatalf("两种地址都有时应取域名，实际 %q（顺序 %q）", got, order)
		}
	}
	m.handleLine(tun, &runState{}, strings.Replace(ip, "203.0.113.7", "203.0.113.9", 1))
	if got := addr(); got != "203.0.113.9:12345" {
		t.Fatalf("这次只给了 IP 时应用 IP，实际 %q", got)
	}
	for a, want := range map[string]bool{"203.0.113.7:12345": true, "frp-abc.natfrp.cloud:12345": false, "cn-sz-1.openfrp.top:23456": false} {
		if isIPAddr(a) != want {
			t.Errorf("isIPAddr(%q) 应为 %v", a, want)
		}
	}
}

// PublicAddr 跨次保存着：新一次运行连上之前 Online 为 false（界面据此不拿上次的地址盖住这次的报错），
// 报了成功或拿到地址才算连上；停下以后不再 Online。
func TestOnlineTracksThisRun(t *testing.T) {
	tempBase(t)
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "游戏", Credential: "k", ProxyID: "10"})
	if err != nil {
		t.Fatal(err)
	}
	tun := m.get(tt.ID)
	m.setPublicAddr(tun, "frp-old.natfrp.cloud:12345") // 上一次运行留下的
	rs := &runState{}
	m.mu.Lock()
	m.running[tt.ID] = rs
	m.mu.Unlock()
	m.handleLine(tun, rs, "2026/11/02 15:31:27 [E] [游戏] login to server failed: i/o timeout")
	if st := m.List()[0]; !st.Running || st.Online || st.LastError == "" || st.PublicAddr != "frp-old.natfrp.cloud:12345" {
		t.Fatalf("还没连上就报错：%+v", st)
	}
	m.handleLine(tun, rs, "2026/11/02 15:31:30 [I] [游戏] 使用 >>frp-abc.natfrp.cloud:12345<< 连接你的隧道")
	if st := m.List()[0]; !st.Online || st.PublicAddr != "frp-abc.natfrp.cloud:12345" {
		t.Fatalf("拿到地址后：%+v", st)
	}
	m.mu.Lock()
	delete(m.running, tt.ID)
	m.mu.Unlock()
	if st := m.List()[0]; st.Running || st.Online {
		t.Fatalf("停下以后：%+v", st)
	}
}

// 樱花、OpenFrp 的隧道不存 frps 地址和端口（启动时只传隧道 ID，端口在服务商后台填，存着的数会误导）：
// 新增时清掉；老数据加载时清掉并写回；自定义 frps 的照旧。
func TestUnusedPortsNotStored(t *testing.T) {
	tempBase(t)
	legacy := `{"seq":2,"tunnels":[
{"id":"tun1","provider":"natfrp","name":"语音","credential":"","proxyId":"9","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":true},
{"id":"tun2","provider":"custom","name":"自建","credential":"","serverAddr":"frp.example.com","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if _, err := m.Add(Tunnel{Provider: "openfrp", Name: "新", Credential: "k", ProxyID: "1", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}); err != nil {
		t.Fatal(err)
	}
	var data struct {
		Tunnels []map[string]any `json:"tunnels"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(app.Base, "tunnels.json"))), &data); err != nil {
		t.Fatal(err)
	}
	for _, tn := range data.Tunnels {
		_, lp := tn["localPort"]
		_, rp := tn["remotePort"]
		_, sp := tn["serverPort"]
		if tn["provider"] == "custom" {
			if tn["localPort"] != 25565.0 || tn["remotePort"] != 25566.0 || tn["serverPort"] != 7000.0 {
				t.Errorf("自定义 frps 的端口不该动：%v", tn)
			}
		} else if lp || rp || sp {
			t.Errorf("%s 隧道不该存端口：%v", tn["provider"], tn)
		}
	}
	for _, st := range m.List() {
		if st.Provider != "custom" && (st.LocalPort != 0 || st.RemotePort != 0) {
			t.Errorf("列表里 %s 的端口应为 0：%+v", st.Name, st)
		}
	}
}
