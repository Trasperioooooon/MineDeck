package tunnel

import (
	"strings"
	"testing"

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
