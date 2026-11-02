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
	if err == nil || !strings.Contains(err.Error(), "无法解开") {
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
}
