package tunnel

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// 界面上的「编辑」（可用性走查 R12）只改名称、绑定的服务器和跟随启动，其余配置按列表里的原样带回去，凭据留空。
// 以前 validate 先要求 OpenFrp、樱花穿透带凭据，「留空 = 不改」在这两家身上走不到，只要不重填凭据什么都改不了。
// 现在留空就沿用存着的那份；换服务商、换 frps 地址端口照旧要重填；没有存着的凭据可沿用时照旧要填；
// 网页端口、tunnels.json 没读出来、运行中这几道检查都还在。

// editOf 按 List 下发的样子（凭据已经抹掉）把一条隧道带回去，再改名称、绑定和跟随
func editOf(st Status, name, bound string, auto bool) Tunnel {
	t := st.Tunnel
	t.Credential = ""
	t.Name, t.Bound, t.AutoStart = name, bound, auto
	return t
}

func statusOf(t *testing.T, m *Manager, id string) Status {
	t.Helper()
	for _, st := range m.List() {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("列表里没有 %s", id)
	return Status{}
}

func TestEditKeepsStoredCredential(t *testing.T) {
	for _, prov := range []string{"openfrp", "natfrp"} {
		t.Run(prov, func(t *testing.T) {
			tempBase(t)
			m := NewManager()
			added, err := m.Add(Tunnel{Provider: prov, Name: "朋友联机", Credential: "key-" + prov, ProxyID: "42", Bound: "生存服", AutoStart: true})
			if err != nil {
				t.Fatal(err)
			}
			sealed := m.get(added.ID).Credential
			m.setPublicAddr(m.get(added.ID), "cn-sz.example.com:23456") // 上次拿到的地址：编辑以后还在
			st := statusOf(t, m, added.ID)
			if err := m.Update(added.ID, editOf(st, "  改了名  ", "机械动力", false)); err != nil {
				t.Fatalf("凭据留空、只改名称和绑定应能保存：%v", err)
			}
			got := m.get(added.ID)
			if got.Name != "改了名" || got.Bound != "机械动力" || got.AutoStart {
				t.Errorf("改的三样没存上：%+v", *got)
			}
			if got.Credential != sealed || got.ProxyID != "42" || got.Provider != prov || got.PublicAddr != "cn-sz.example.com:23456" {
				t.Errorf("没改的应原样留着：%+v", *got)
			}
			// 重启以后还在，凭据也还解得开
			m2 := NewManager()
			if st := statusOf(t, m2, added.ID); st.Name != "改了名" || !st.HasCredential || st.CredBroken {
				t.Errorf("重启后：%+v", st)
			}
		})
	}
}

func TestEditKeepRules(t *testing.T) {
	tempBase(t)
	m := NewManager()
	nat, err := m.Add(Tunnel{Provider: "natfrp", Name: "樱花", Credential: "k", ProxyID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	bare, err := m.Add(Tunnel{Provider: "custom", Name: "不带令牌", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	// 换服务商：凭据不能带到别家去
	moved := editOf(statusOf(t, m, nat.ID), "樱花", "", false)
	moved.Provider = "openfrp"
	if err := m.Update(nat.ID, moved); err == nil || !strings.Contains(err.Error(), "重新填一次凭据") {
		t.Errorf("换服务商不重填凭据应被拒绝：%v", err)
	}
	// 原来没有凭据，改成要凭据的服务商：和新增一样要填
	need := editOf(statusOf(t, m, bare.ID), "不带令牌", "", false)
	need.Provider, need.ProxyID = "openfrp", "10"
	if err := m.Update(bare.ID, need); !errors.Is(err, errNeedCredential) {
		t.Errorf("没有凭据可沿用时应要求填写：%v", err)
	}
	// 隧道 ID 照旧必填
	noID := editOf(statusOf(t, m, nat.ID), "樱花", "", false)
	noID.ProxyID = " "
	if err := m.Update(nat.ID, noID); !errors.Is(err, errNeedCredential) {
		t.Errorf("隧道 ID 空着应被拒绝：%v", err)
	}
	// 名称空着照旧拒绝
	if err := m.Update(nat.ID, editOf(statusOf(t, m, nat.ID), "  ", "", false)); err == nil || !strings.Contains(err.Error(), "隧道名称") {
		t.Errorf("名称空着应被拒绝：%v", err)
	}
	// 运行中不能改
	m.mu.Lock()
	m.running[nat.ID] = &runState{}
	m.mu.Unlock()
	if err := m.Update(nat.ID, editOf(statusOf(t, m, nat.ID), "运行中改名", "", false)); err == nil || !strings.Contains(err.Error(), "请先停止隧道") {
		t.Errorf("运行中改应被拒绝：%v", err)
	}
	m.mu.Lock()
	delete(m.running, nat.ID)
	m.mu.Unlock()
	if got := m.get(nat.ID); got.Name != "樱花" || got.Provider != "natfrp" {
		t.Errorf("被拒绝的修改不该存下来：%+v", *got)
	}
}

// 以前存下的、指向网页端口的自定义 frps 隧道：只改名称也不放行（编辑守住 PR #20 那道检查）
func TestEditKeepsGuardPort(t *testing.T) {
	tempBase(t)
	const web = 27333
	m := NewManager()
	old, err := m.Add(Tunnel{Provider: "custom", Name: "旧的", Credential: "tok", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: web})
	if err != nil {
		t.Fatal(err)
	}
	m.SetGuardPort(web)
	if err := m.Update(old.ID, editOf(statusOf(t, m, old.ID), "改个名", "", false)); err == nil || !strings.Contains(err.Error(), "MineDeck 自己的网页端口") {
		t.Errorf("指向网页端口的隧道编辑时应被拒绝：%v", err)
	}
	if got := m.get(old.ID); got.Name != "旧的" {
		t.Errorf("被拒绝的修改不该存下来：%q", got.Name)
	}
}

// tunnels.json 在却没读出来：编辑一律拒绝（报 LoadError，不说「隧道不存在」），原文件一字不动
func TestEditBlockedWhileUnreadable(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	seedTunnels(t)
	if err := os.WriteFile(tunnelsPath(), []byte(`{"seq":1,"tunnels":[{"id":"tun1"`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, tunnelsPath())
	m := NewManager()
	edit := Tunnel{Provider: "openfrp", Name: "改名", ProxyID: "42"} // 凭据留空，同界面的「编辑」
	wantLoadError(t, m.Update("tun1", edit), true)
	if after := readFile(t, tunnelsPath()); after != before {
		t.Fatalf("没读成时原文件被改了")
	}
}
