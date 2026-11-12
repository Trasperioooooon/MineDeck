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
	for _, ok := range []string{"frp.example.com", "1.2.3.4", "::1", "[2001:db8::1]", "a", "example.com.", "frp-1.my-host.cn"} {
		if err := checkServerAddr(ok); err != nil {
			t.Errorf("%q 应该能用：%v", ok, err)
		}
	}
	for _, bad := range []string{tplAddr, "frp.example.com:7000", "http://frp.example.com", "a b", "", "-bad.com", "bad-.com", "a..b", "例子.中国"} {
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
}

// 凭据留空表示不改；但换了服务商，或者 frps 换了地址、端口，要重新填，免得旧令牌被带到新的服务器（B-5）。
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
	other := base
	other.Provider, other.ProxyID = "openfrp", "42"
	if err := m.Update(c.ID, other); err == nil {
		t.Fatal("换了服务商、凭据留空应要求重填")
	}
	if got, _ := secret.Unseal(m.get(c.ID).Credential); got != "old-token" || m.get(c.ID).ServerAddr != "frp.example.com" {
		t.Fatalf("被拒的修改不该动原来的隧道：%+v", m.get(c.ID))
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
