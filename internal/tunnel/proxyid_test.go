package tunnel

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// proxyIDOf 存着的那份隧道 ID。
func proxyIDOf(t *testing.T, m *Manager, id string) string {
	t.Helper()
	for _, st := range m.List() {
		if st.ID == id {
			return st.ProxyID
		}
	}
	t.Fatalf("没有隧道 %s", id)
	return ""
}

// OpenFrp、樱花的隧道 ID 只收数字（A60）：新增和修改时规范成十进制，不是数字的回「隧道 ID 只能是数字」，存着的不动。
// 自定义 frps 不用隧道 ID，不查。
func TestProxyIDOnlyDigits(t *testing.T) {
	tempBase(t)
	m := NewManager()
	for _, bad := range []string{"8,9", "abc", "+7", "-7", "0", "00", "7 8", "9:1", "99999999999999999999", "１２３"} { // 超过 int 的、全角数字也拒
		if _, err := m.Add(Tunnel{Provider: "natfrp", Name: "樱花", Credential: "fake-sakura-key", ProxyID: bad}); !errors.Is(err, errBadProxyID) {
			t.Errorf("新增时隧道 ID %q 应被拒：%v", bad, err)
		}
	}
	if n := len(m.List()); n != 0 {
		t.Fatalf("被拒的不该加进去：%d 条", n)
	}
	added, err := m.Add(Tunnel{Provider: "openfrp", Name: "OpenFrp", Credential: "fake-openfrp-key", ProxyID: " 007 "})
	if err != nil || added.ProxyID != "7" {
		t.Fatalf("「 007 」应存成 7：%+v %v", added, err)
	}
	custom, err := m.Add(Tunnel{Provider: "custom", Name: "自建", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565, ProxyID: "随便"})
	if err != nil {
		t.Fatalf("自定义 frps 不用隧道 ID，不该查：%v", err)
	}
	// 自定义 frps 改成樱花：隧道 ID 从这时起要用上，得是数字
	if err := m.PatchOpt(custom.ID, Patch{Provider: twStr("natfrp"), Credential: twStr("fake-sakura-key")}, UpdateOpts{}); !errors.Is(err, errBadProxyID) {
		t.Fatalf("自定义 frps 改成樱花时隧道 ID 也要是数字：%v", err)
	}
	if got := proxyIDOf(t, m, custom.ID); got != "随便" {
		t.Fatalf("被拒的修改不该动存着的：%q", got)
	}

	if err := m.PatchOpt(added.ID, Patch{ProxyID: twStr("8,9")}, UpdateOpts{}); !errors.Is(err, errBadProxyID) {
		t.Fatalf("修改成不是数字的应被拒：%v", err)
	}
	if got := proxyIDOf(t, m, added.ID); got != "7" {
		t.Fatalf("被拒的修改不该动存着的：%q", got)
	}
	if err := m.PatchOpt(added.ID, Patch{ProxyID: twStr("0010")}, UpdateOpts{}); err != nil || proxyIDOf(t, m, added.ID) != "10" {
		t.Fatalf("「0010」应存成 10：%q %v", proxyIDOf(t, m, added.ID), err)
	}
}

// 以前存下的不是数字的隧道 ID（那时什么都收）：不改它时照常改名、改跟随，照常去启动；换服务商时要过一遍（A60）。
func TestLegacyNonNumericProxyIDKeptWhenUnchanged(t *testing.T) {
	tempBase(t)
	legacy := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"旧的","credential":"fake-sakura-key","proxyId":"abc","autoStart":false}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if err := m.PatchOpt("tun1", Patch{Name: twStr("改了名"), AutoStart: twBool(true)}, UpdateOpts{}); err != nil {
		t.Fatalf("没改隧道 ID 时应照常保存：%v", err)
	}
	m = NewManager() // 从磁盘读回来
	if got := proxyIDOf(t, m, "tun1"); got != "abc" || m.List()[0].Name != "改了名" {
		t.Fatalf("存下的应是改了名、隧道 ID 原样：%+v", m.List())
	}
	// 这里没有樱花的 frpc，启动的结果是「首次使用需手动放置」：说明没被隧道 ID 挡下
	if err := m.Start("tun1"); errors.Is(err, errBadProxyID) || err == nil || !strings.Contains(err.Error(), "首次使用需手动放置樱花 frpc") {
		t.Fatalf("旧数据照常去启动：%v", err)
	}
	if err := m.PatchOpt("tun1", Patch{Provider: twStr("openfrp"), Credential: twStr("fake-openfrp-key")}, UpdateOpts{}); !errors.Is(err, errBadProxyID) {
		t.Fatalf("换了服务商时隧道 ID 也要是数字：%v", err)
	}
}
