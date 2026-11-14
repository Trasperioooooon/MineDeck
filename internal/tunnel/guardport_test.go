package tunnel

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"minedeck/internal/events"
)

// 自定义 frps 的隧道不能指向 MineDeck 自己的网页端口：经 frpc 进来的请求都来自 127.0.0.1，映射出去等于把管理界面摆到公网上。
// 新增、修改时拒绝；以前存下的（或者网页端口这次换了），手动启动和跟随实例启动时也拒绝，frpc 不会被拉起来。
func TestGuardPortBlocksWebPort(t *testing.T) {
	tempBase(t)
	// 放一个假的 frpc，启动也换成只记次数：万一检查漏了，测试不会去下载 frp、也不会真的起进程
	dir := frpcDir("custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frpc.exe"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	var launched atomic.Int32
	oldStart, oldDelay := startFrpc, startDelay
	startFrpc = func(*exec.Cmd) error { launched.Add(1); return errors.New("测试里不起 frpc") }
	startDelay = 10 * time.Millisecond
	t.Cleanup(func() { startFrpc, startDelay = oldStart, oldDelay })

	const web = 27333
	custom := func(name string, port int) Tunnel {
		return Tunnel{Provider: "custom", Name: name, ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: port,
			Bound: "S1", AutoStart: true}
	}
	m := NewManager()
	// 设下检查之前存下的：指向网页端口
	old, err := m.Add(custom("旧的", web))
	if err != nil {
		t.Fatal(err)
	}
	m.SetGuardPort(web)

	isGuard := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "MineDeck 自己的网页端口")
	}
	if _, err := m.Add(custom("新的", web)); !isGuard(err) {
		t.Errorf("新增指向网页端口的隧道应被拒绝：%v", err)
	}
	ok, err := m.Add(custom("MC", 25565))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Update(ok.ID, custom("MC", web)); !isGuard(err) {
		t.Errorf("改成指向网页端口应被拒绝：%v", err)
	}
	if st := m.get(ok.ID); st.LocalPort != 25565 {
		t.Errorf("被拒绝的修改不该存下来：本地端口成了 %d", st.LocalPort)
	}
	// 樱花、OpenFrp 的本地端口在服务商后台，MineDeck 不存，也就不拦
	if _, err := m.Add(Tunnel{Provider: "natfrp", Name: "樱花", Credential: "k", ProxyID: "9", LocalPort: web}); err != nil {
		t.Errorf("樱花隧道不该因为传了没用的本地端口被拒绝：%v", err)
	}

	// 手动启动以前存下的那条
	if err := m.Start(old.ID); !isGuard(err) {
		t.Errorf("启动指向网页端口的隧道应被拒绝：%v", err)
	}
	if le := m.lastError(old.ID); !strings.HasPrefix(le, "启动失败：本地端口 27333 是 MineDeck 自己的网页端口") {
		t.Errorf("卡片上的失败原因：%q", le)
	}

	// 跟随实例启动：S1 上绑着旧的（被拒绝）和 MC（照常去启动，这里换成了假的 frpc）
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })
	m.follow(events.Event{Type: "instance.start", Data: map[string]any{"instance": "S1"}}, func(string) string { return "starting" })
	got := map[string]AutoAction{}
	for len(got) < 2 {
		select {
		case a := <-notes:
			got[a.Args["id"].(string)] = a
		case <-time.After(3 * time.Second):
			t.Fatalf("跟随启动没有记全操作记录：%+v", got)
		}
	}
	if a := got[old.ID]; a.Op != "tunnel.start" || !isGuard(a.Err) {
		t.Errorf("跟随启动指向网页端口的隧道应被拒绝：%+v", a)
	}
	if le := m.lastError(old.ID); !strings.HasPrefix(le, "跟随启动失败：本地端口 27333") {
		t.Errorf("跟随启动被拒绝时卡片上的原因：%q", le)
	}
	if a := got[ok.ID]; isGuard(a.Err) {
		t.Errorf("指向 MC 端口的隧道不该被拦：%+v", a)
	}
	// 只有指向 MC 端口的那条走到了起 frpc 这一步
	if n := launched.Load(); n != 1 {
		t.Errorf("frpc 被拉起 %d 次，应为 1 次（只有指向 MC 端口的那条）", n)
	}
}
