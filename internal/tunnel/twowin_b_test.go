package tunnel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 两个窗口同时开着同一条隧道（2026-11-17）：以前「编辑」把整份配置带回去，没刷新的那一边一保存，就把另一边刚存的
// 改回原样。现在只发改了的项（PatchOpt），没给的项沿用存着的那份，公网地址始终留着。

func twStr(s string) *string { return &s }
func twInt(n int) *int       { return &n }
func twBool(b bool) *bool    { return &b }

// twCustom 一条带令牌、绑定了实例、记过公网地址的自定义 frps 语音隧道：哪一项被动了都看得出来。返回编号和存着的密文。
func twCustom(t *testing.T, m *Manager) (string, string) {
	t.Helper()
	added, err := m.Add(Tunnel{Provider: "custom", Name: "语音", Credential: "frps-token", ServerAddr: "frp.example.com", ServerPort: 7000,
		RemotePort: 30001, LocalPort: 24454, Bound: "生存服", Purpose: PurposeVoice, Proto: "udp"})
	if err != nil {
		t.Fatal(err)
	}
	m.setPublicAddr(m.get(added.ID), "frp.example.com:30001")
	m.mu.Lock()
	sealed := m.get(added.ID).Credential
	m.mu.Unlock()
	if sealed == "" {
		t.Fatal("新增时凭据没存上")
	}
	return added.ID, sealed
}

// twSnap 拿锁取一份存着的配置。
func twSnap(m *Manager, id string) Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.get(id)
}

// A 只改跟随启动、B（还是 A 改之前的页面）只改名字：两样都在，别的一项没动。
func TestPatchTwoWindowsKeepBoth(t *testing.T) {
	tempBase(t)
	m := NewManager()
	id, sealed := twCustom(t, m)

	if err := m.PatchOpt(id, Patch{AutoStart: twBool(true)}, UpdateOpts{}); err != nil { // 窗口 A
		t.Fatalf("A 只改跟随启动：%v", err)
	}
	if err := m.PatchOpt(id, Patch{Name: twStr("  改了名  ")}, UpdateOpts{}); err != nil { // 窗口 B
		t.Fatalf("B 只改名字：%v", err)
	}
	want := Tunnel{ID: id, Provider: "custom", Name: "改了名", Credential: sealed, ServerAddr: "frp.example.com", ServerPort: 7000,
		RemotePort: 30001, LocalPort: 24454, Bound: "生存服", AutoStart: true, PublicAddr: "frp.example.com:30001", Purpose: PurposeVoice, Proto: "udp"}
	if got := twSnap(m, id); got != want {
		t.Fatalf("两个窗口先后保存以后：\n得到 %+v\n应为 %+v", got, want)
	}
	// 重启以后还是这样，凭据也还解得开
	st := statusOf(t, NewManager(), id)
	if st.Name != "改了名" || !st.AutoStart || !st.HasCredential || st.CredBroken || st.Bound != "生存服" || st.PublicAddr != "frp.example.com:30001" ||
		st.ServerPort != 7000 || st.RemotePort != 30001 || st.LocalPort != 24454 || st.Purpose != PurposeVoice || st.Proto != "udp" {
		t.Fatalf("重启后：%+v", st)
	}
}

// 只发 clearCredential：凭据没了，别的项都留着；「凭据解不开」的标记一起清掉。
func TestPatchClearCredentialOnly(t *testing.T) {
	tempBase(t)
	m := NewManager()
	id, _ := twCustom(t, m)
	m.mu.Lock()
	m.broken[id] = true
	m.mu.Unlock()
	before := twSnap(m, id)

	if err := m.PatchOpt(id, Patch{}, UpdateOpts{ClearCredential: true}); err != nil {
		t.Fatalf("只发 clearCredential：%v", err)
	}
	want := before
	want.Credential = ""
	if got := twSnap(m, id); got != want {
		t.Fatalf("清凭据以后：\n得到 %+v\n应为 %+v", got, want)
	}
	if st := statusOf(t, m, id); st.HasCredential || st.CredBroken {
		t.Fatalf("凭据应清掉、解不开的标记也清掉：%+v", st)
	}
	if st := statusOf(t, NewManager(), id); st.HasCredential || st.Name != "语音" || st.Bound != "生存服" {
		t.Fatalf("重启后：%+v", st)
	}
}

// 凭据和清凭据的几条规矩照旧：看的是合好以后的整份（只发 clearCredential 时服务商是存着的那个）。
func TestPatchCredentialRules(t *testing.T) {
	tempBase(t)
	m := NewManager()
	id, sealed := twCustom(t, m)
	of, err := m.Add(openfrpTunnel("朋友"))
	if err != nil {
		t.Fatal(err)
	}

	// 改 frps 地址不带凭据：要重填
	if err := m.PatchOpt(id, Patch{ServerAddr: twStr("evil.example.com")}, UpdateOpts{}); err == nil || !strings.Contains(err.Error(), "要重新填一次凭据") {
		t.Errorf("改地址不带凭据应被拒绝：%v", err)
	}
	if err := m.PatchOpt(id, Patch{ServerPort: twInt(7001)}, UpdateOpts{}); err == nil || !strings.Contains(err.Error(), "要重新填一次凭据") {
		t.Errorf("改端口不带凭据应被拒绝：%v", err)
	}
	// 凭据只有空白：当没填（沿用），一样要重填
	if err := m.PatchOpt(id, Patch{ServerAddr: twStr("evil.example.com"), Credential: twStr("   ")}, UpdateOpts{}); err == nil || !strings.Contains(err.Error(), "要重新填一次凭据") {
		t.Errorf("凭据只有空白时改地址应被拒绝：%v", err)
	}
	if got := twSnap(m, id); got.ServerAddr != "frp.example.com" || got.ServerPort != 7000 || got.Credential != sealed {
		t.Fatalf("被拒绝的修改不该存下来：%+v", got)
	}
	// 带上新凭据就能改
	if err := m.PatchOpt(id, Patch{ServerAddr: twStr(" [2001:db8::1] "), Credential: twStr(" new-token ")}, UpdateOpts{}); err != nil {
		t.Fatalf("改地址并重填凭据应能保存：%v", err)
	}
	if got := twSnap(m, id); got.ServerAddr != "2001:db8::1" || got.Credential == sealed || got.Credential == "" || got.Name != "语音" {
		t.Fatalf("改地址重填凭据以后：%+v", got)
	}
	// 服务商的隧道不能清凭据（只发 clearCredential，服务商取存着的 openfrp）
	if err := m.PatchOpt(of.ID, Patch{}, UpdateOpts{ClearCredential: true}); err == nil || !strings.Contains(err.Error(), "只有自定义 frps") {
		t.Errorf("OpenFrp 的隧道清凭据应被拒绝：%v", err)
	}
	// 清凭据又填新的：拒绝
	if err := m.PatchOpt(id, Patch{Credential: twStr("x")}, UpdateOpts{ClearCredential: true}); err == nil || !strings.Contains(err.Error(), "不要同时填新的凭据") {
		t.Errorf("清凭据又填新的应被拒绝：%v", err)
	}
	// 名字改成空白：和整份替换一样拒绝
	if err := m.PatchOpt(of.ID, Patch{Name: twStr("  ")}, UpdateOpts{}); err == nil || !strings.Contains(err.Error(), "隧道名称") {
		t.Errorf("名字空着应被拒绝：%v", err)
	}
	// OpenFrp 的隧道只改名字：凭据沿用（以前的整份替换要靠「留空 = 不改」，这里本来就没给）
	if err := m.PatchOpt(of.ID, Patch{Name: twStr("朋友 2")}, UpdateOpts{}); err != nil {
		t.Fatalf("OpenFrp 只改名字：%v", err)
	}
	if st := statusOf(t, m, of.ID); st.Name != "朋友 2" || !st.HasCredential || st.ProxyID != "42" {
		t.Fatalf("OpenFrp 只改名字以后：%+v", st)
	}
}

// 运行中、正在启动、隧道不存在：PatchOpt 和 UpdateOpt 报一样的错，什么都不改。
func TestPatchRefusedSameAsUpdate(t *testing.T) {
	tempBase(t)
	m := NewManager()
	id, _ := twCustom(t, m)
	full := Tunnel{Provider: "custom", Name: "改了名", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: 24454}
	patch := Patch{Name: twStr("改了名")}
	file := filepath.Join(app.Base, "tunnels.json")
	saved := readFile(t, file)

	same := func(what string, wantSub string) {
		t.Helper()
		ue := m.UpdateOpt(id, full, UpdateOpts{})
		pe := m.PatchOpt(id, patch, UpdateOpts{})
		if ue == nil || pe == nil || ue.Error() != pe.Error() || !strings.Contains(pe.Error(), wantSub) {
			t.Errorf("%s：UpdateOpt 报 %v，PatchOpt 报 %v，应一样、含「%s」", what, ue, pe, wantSub)
		}
	}
	m.mu.Lock()
	m.running[id] = &runState{}
	m.mu.Unlock()
	same("运行中", "请先停止隧道")
	m.mu.Lock()
	delete(m.running, id)
	m.starting[id] = true
	m.mu.Unlock()
	same("正在启动", "正在启动")
	m.mu.Lock()
	delete(m.starting, id)
	m.mu.Unlock()

	ue := m.UpdateOpt("tun999", full, UpdateOpts{})
	pe := m.PatchOpt("tun999", patch, UpdateOpts{})
	if ue == nil || pe == nil || ue.Error() != pe.Error() || pe.Error() != "隧道不存在" {
		t.Errorf("隧道不存在：UpdateOpt 报 %v，PatchOpt 报 %v", ue, pe)
	}
	if got := readFile(t, file); got != saved {
		t.Errorf("被拒绝的修改不该写 tunnels.json：\n之前 %s\n之后 %s", saved, got)
	}
	if got := twSnap(m, id); got.Name != "语音" {
		t.Errorf("被拒绝的修改不该改内存：%+v", got)
	}
}

// 写不进磁盘：报 SaveError，内存里退回原样，「凭据解不开」的标记留着；磁盘上的文件一字不变。
func TestPatchRollsBackWhenSaveFails(t *testing.T) {
	tempBase(t)
	m := NewManager()
	id, _ := twCustom(t, m)
	m.mu.Lock()
	m.broken[id] = true
	m.mu.Unlock()
	snap := twSnap(m, id)
	path := filepath.Join(app.Base, "tunnels.json")
	before := readFile(t, path)

	restore := blockSave(t)
	wantSaveError(t, m.PatchOpt(id, Patch{Name: twStr("改了名"), AutoStart: twBool(true)}, UpdateOpts{}))
	wantSaveError(t, m.PatchOpt(id, Patch{Credential: twStr("new-token")}, UpdateOpts{}))
	wantSaveError(t, m.PatchOpt(id, Patch{}, UpdateOpts{ClearCredential: true}))
	m.mu.Lock()
	now, broken := *m.get(id), m.broken[id]
	m.mu.Unlock()
	restore()
	if now != snap {
		t.Fatalf("写盘失败后隧道被改了：\n前：%+v\n后：%+v", snap, now)
	}
	if !broken {
		t.Fatal("写盘失败不该清掉「凭据解不开」的标记")
	}
	if after := readFile(t, path); after != before {
		t.Fatalf("写盘失败不该动原文件：\n前：%s\n后：%s", before, after)
	}
}

// tunnels.json 没读出来：和 UpdateOpt 一样报读不出来，不报「隧道不存在」。
func TestPatchRefusedWhileLoadFailed(t *testing.T) {
	tempBase(t)
	id, _ := twCustom(t, NewManager())
	path := filepath.Join(app.Base, "tunnels.json")
	if err := os.WriteFile(path, []byte(`{"seq":1,"tunnels":[{坏的`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	err := m.PatchOpt(id, Patch{Name: twStr("改了名")}, UpdateOpts{})
	var le *LoadError
	if !errors.As(err, &le) || !le.Corrupt {
		t.Fatalf("没读出来时应报 LoadError：%v", err)
	}
	if got := readFile(t, path); got != `{"seq":1,"tunnels":[{坏的` {
		t.Fatalf("没读出来时不该写 tunnels.json：%s", got)
	}
}
