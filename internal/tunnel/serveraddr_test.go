package tunnel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/secret"
)

// 自定义 frps 的地址会原样写进 frpc 的配置，frpc 读配置时把整份文件当模板渲染：
// 地址里的 {{ .Envs.MINEDECK_FRP_TOKEN }} 会把令牌拼进主机名（v3.1.0 正式版前检查 B-5）。地址只认域名或 IP。
const tplAddr = "{{ .Envs." + frpTokenEnv + " }}.evil.example"

func TestCheckServerAddr(t *testing.T) {
	// 中文域名、带下划线的也放行：这里只挡模板和引号这类字符，域名对不对交给 frpc
	for _, ok := range []string{"frp.example.com", "1.2.3.4", "::1", "[2001:db8::1]", "a", "example.com.", "frp-1.my-host.cn",
		"例子.中国", "xn--fiqs8s.cn", "my_frp.example.com"} {
		if err := checkServerAddr(ok); err != nil {
			t.Errorf("%q 应该能用：%v", ok, err)
		}
	}
	for _, bad := range []string{tplAddr, "{{x}}", "frp.example.com:7000", "http://frp.example.com", "frp.example.com/", "a b", "",
		"a..b", ".a", `frp"x.com`, `frp\x.com`, "frp.example.com\n", "a\tb", "ｆｒｐ｛ｘ｝.cn"} {
		if checkServerAddr(bad) == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
}

// 新增、修改隧道时不许用模板写法的地址；以前存下的，启动时也拒绝，不生成配置。
func TestTemplateAddrRejected(t *testing.T) {
	tempBase(t)
	m := NewManager()
	if _, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: "tok", ServerAddr: tplAddr, ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}); err == nil {
		t.Fatal("模板写法的地址应拒绝")
	}
	c, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: "tok", ServerAddr: " frp.example.com ", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.get(c.ID).ServerAddr; got != "frp.example.com" {
		t.Fatalf("地址应去掉前后空格：%q", got)
	}
	if err := m.Update(c.ID, Tunnel{Provider: "custom", Name: "自建", Credential: "tok", ServerAddr: tplAddr, ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}); err == nil {
		t.Fatal("改成模板写法的地址应拒绝")
	}
	old := &Tunnel{ID: "tun9", Provider: "custom", Credential: "tok", ServerAddr: tplAddr, ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}
	if _, _, err := (customProvider{}).Launch(old); err == nil {
		t.Fatal("以前存下的模板地址，启动时应拒绝")
	}
	// 以前存下的地址带着空格、方括号：写进配置的是整理过的
	legacy := &Tunnel{ID: "tun8", Provider: "custom", ServerAddr: " [2001:db8::1] ", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}
	args, _, err := (customProvider{}).Launch(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := readFile(t, args[len(args)-1]); !strings.Contains(cfg, `serverAddr = "2001:db8::1"`) {
		t.Fatalf("写进配置的地址没整理：\n%s", cfg)
	}
}

// 凭据留空表示不改；但换了服务商，或者 frps 换了地址、端口，要重新填，免得旧令牌被带到新的服务器（B-5）。
// 真正会把令牌带走的是改成自定义 frps 的那几种：改成服务商的，本来就要填凭据（validate 会拦）。
func TestCredentialRebindOnTargetChange(t *testing.T) {
	tempBase(t)
	m := NewManager()
	c, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: "old-token", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	base := Tunnel{Provider: "custom", Name: "自建", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}

	moved := base
	moved.ServerAddr = "attacker.example"
	if err := m.Update(c.ID, moved); err == nil || !strings.Contains(err.Error(), "重新填一次凭据") {
		t.Fatalf("换了地址、凭据留空应要求重填：%v", err)
	}
	port := base
	port.ServerPort = 7001
	if err := m.Update(c.ID, port); err == nil {
		t.Fatal("换了 frps 端口、凭据留空应要求重填")
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "old-token" || m.get(c.ID).ServerAddr != "frp.example.com" {
		t.Fatalf("被拒的修改不该动原来的隧道：%+v", m.get(c.ID))
	}
	// 服务商的隧道改成自定义 frps、凭据留空：服务商的令牌不能被带到填的这台服务器上
	of, err := m.Add(Tunnel{Provider: "openfrp", Name: "服务商", Credential: "openfrp-token", ProxyID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Update(of.ID, moved); err == nil || !strings.Contains(err.Error(), "重新填一次凭据") {
		t.Fatalf("从服务商改成自定义 frps、凭据留空应要求重填：%v", err)
	}
	// 老版本存下的服务商隧道可能还留着地址、端口；改成自定义时照抄这组地址端口，也要重填（只能靠「换了服务商」这一条拦）
	m.get(of.ID).ServerAddr, m.get(of.ID).ServerPort = moved.ServerAddr, moved.ServerPort
	if err := m.Update(of.ID, moved); err == nil || !strings.Contains(err.Error(), "重新填一次凭据") {
		t.Fatalf("换了服务商、地址端口没变，凭据留空也应要求重填：%v", err)
	}
	if got, _ := secret.Unseal(m.get(of.ID).Credential); got != "openfrp-token" || m.get(of.ID).Provider != "openfrp" {
		t.Fatalf("被拒的修改不该动原来的隧道：%+v", m.get(of.ID))
	}

	// 只改远程端口这类：凭据照旧留着
	same := base
	same.RemotePort = 25570
	if err := m.Update(c.ID, same); err != nil {
		t.Fatal(err)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "old-token" {
		t.Fatalf("没换目标时凭据应留着：%q", got)
	}
	// 换地址时填了新凭据：照常
	moved.Credential = "new-token"
	if err := m.Update(c.ID, moved); err != nil {
		t.Fatal(err)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "new-token" || m.get(c.ID).ServerAddr != "attacker.example" {
		t.Fatalf("填了新凭据的修改没生效：%+v", m.get(c.ID))
	}
}

// 自定义 frps 存过的凭据能明说清掉（clearCredential）：tunnels.json 和内存里都不再有，启动时配置里没有 auth.token、
// 也不往 frpc 传令牌；换地址、端口时选清除也放行。凭据留空照旧是「不改」；服务商的隧道不能清。
func TestClearCredential(t *testing.T) {
	tempBase(t)
	m := NewManager()
	c, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: "old-token", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	of, err := m.Add(Tunnel{Provider: "openfrp", Name: "服务商", Credential: "openfrp-token", ProxyID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	has := func(id string) bool {
		for _, st := range m.List() {
			if st.ID == id {
				return st.HasCredential
			}
		}
		t.Fatalf("列表里没有 %s", id)
		return false
	}
	if !has(c.ID) || !has(of.ID) {
		t.Fatal("存着凭据的隧道，列表应标出 hasCredential")
	}
	base := Tunnel{Provider: "custom", Name: "自建", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}
	wipe := UpdateOpts{ClearCredential: true}

	// 清除时又填了新凭据：说不清要哪个，拒绝
	both := base
	both.Credential = "another"
	if err := m.UpdateOpt(c.ID, both, wipe); err == nil || !strings.Contains(err.Error(), "不要同时填新的凭据") {
		t.Fatalf("清除又填新凭据应拒绝：%v", err)
	}
	// 服务商的隧道启动时必须带凭据，不能清
	ofCfg := Tunnel{Provider: "openfrp", Name: "服务商", ProxyID: "42"}
	if err := m.UpdateOpt(of.ID, ofCfg, wipe); err == nil || !strings.Contains(err.Error(), "只有自定义 frps") {
		t.Fatalf("服务商的隧道清除凭据应拒绝：%v", err)
	}
	if got, _ := secret.Unseal(m.get(of.ID).Credential); got != "openfrp-token" {
		t.Fatalf("被拒的清除不该动服务商隧道的凭据：%q", got)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "old-token" {
		t.Fatalf("被拒的清除不该动凭据：%q", got)
	}

	// 清除，同时换了地址和端口：明说了不要凭据，放行
	moved := base
	moved.ServerAddr, moved.ServerPort = "frp2.example.com", 7001
	if err := m.UpdateOpt(c.ID, moved, wipe); err != nil {
		t.Fatal(err)
	}
	if got := m.get(c.ID); got.Credential != "" || got.ServerAddr != "frp2.example.com" || got.ServerPort != 7001 {
		t.Fatalf("清除凭据的修改没生效：%+v", got)
	}
	if has(c.ID) {
		t.Fatal("清掉以后列表不该再标 hasCredential")
	}
	// tunnels.json 里也没了：只剩服务商那一条的密文
	var data struct {
		Tunnels []Tunnel `json:"tunnels"`
	}
	disk := readFile(t, filepath.Join(app.Base, "tunnels.json"))
	if err := json.Unmarshal([]byte(disk), &data); err != nil {
		t.Fatal(err)
	}
	for _, tt := range data.Tunnels {
		if tt.ID == c.ID && tt.Credential != "" {
			t.Fatalf("tunnels.json 里还留着凭据：%s", disk)
		}
	}
	if strings.Count(disk, `"dpapi:`) != 1 {
		t.Fatalf("tunnels.json 里应只剩服务商那一条的密文：%s", disk)
	}
	// 重新读一遍（程序重启）也还是没有
	if m2 := NewManager(); m2.get(c.ID).Credential != "" {
		t.Fatal("重启后凭据又回来了")
	}
	// 启动时：配置里没有 auth.token，也不往 frpc 传令牌
	snap := *m.get(c.ID)
	cred, err := secret.Unseal(snap.Credential)
	if err != nil || cred != "" {
		t.Fatalf("清掉以后解出来应是空的：%q %v", cred, err)
	}
	snap.Credential = cred
	args, env, err := (customProvider{}).Launch(&snap)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := readFile(t, args[len(args)-1]); strings.Contains(cfg, "auth.token") || strings.Contains(cfg, frpTokenEnv) || len(env) != 0 {
		t.Fatalf("没有凭据时不该带令牌：env=%q\n%s", env, cfg)
	}

	// 清掉以后：凭据留空照旧是「不改」，没有凭据可带走，换地址也不用填
	back := base
	if err := m.Update(c.ID, back); err != nil {
		t.Fatal(err)
	}
	if got := m.get(c.ID); got.Credential != "" || got.ServerAddr != "frp.example.com" {
		t.Fatalf("没有凭据时换地址应照常保存：%+v", got)
	}
	// 再填上新的也照常
	back.Credential = "fresh"
	if err := m.Update(c.ID, back); err != nil {
		t.Fatal(err)
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "fresh" || !has(c.ID) {
		t.Fatalf("清掉以后重新填的凭据没存上：%q", got)
	}
}

// 凭据解不开（换了电脑或 Windows 用户）的自定义 frps：清除凭据以后不再标着解不开，可以不带凭据启动。
func TestClearBrokenCredential(t *testing.T) {
	tempBase(t)
	data := `{"seq":1,"tunnels":[{"id":"tun1","provider":"custom","name":"自建","credential":"dpapi:AAAA","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if l := m.List(); len(l) != 1 || !l[0].CredBroken || !l[0].HasCredential {
		t.Fatalf("应标出凭据解不开：%+v", l)
	}
	cfg := Tunnel{Provider: "custom", Name: "自建", ServerAddr: "1.2.3.4", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565}
	if err := m.UpdateOpt("tun1", cfg, UpdateOpts{ClearCredential: true}); err != nil {
		t.Fatal(err)
	}
	if l := m.List(); l[0].CredBroken || l[0].HasCredential || m.get("tun1").Credential != "" {
		t.Fatalf("清除以后不该再标着解不开：%+v", l[0])
	}
}
