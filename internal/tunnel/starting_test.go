package tunnel

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
)

// 隧道正在准备启动（第一次用要先下载 frpc，可能要一阵子）的时候，修改和删除都拒绝，tunnels.json 不动（2026-11-16 Codex 复审 beta.3）。
// 启动在准备之前就拍下了配置：以前这时候从另一个窗口改了绑定照样保存成功，之后起来的却还是旧的那份，跟随关系和卡片上说的已经变了。
// 准备完了（这里是没起来）就又能改、能删。
func TestEditRefusedWhileStarting(t *testing.T) {
	tempBase(t)
	// 假的 frpc，EnsureFrpc 找得到它就不去下载；起 frpc 那一步换成停在那里等测试放行，这段时间隧道就是「正在启动」
	dir := frpcDir("custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frpc.exe"), fakeFrpcExe(), 0o755); err != nil { // 结构完整的假 exe：随手写几个字的会被当成坏文件删掉
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	free := sync.OnceFunc(func() { close(release) })
	old := startFrpc
	startFrpc = func(*exec.Cmd) error { close(entered); <-release; return errors.New("测试里不起 frpc") }
	t.Cleanup(func() { free(); startFrpc = old })

	m := NewManager()
	tun, err := m.Add(Tunnel{Provider: "custom", Name: "MC", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: 25565,
		Bound: "S1", AutoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Start(tun.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("启动没有走到起 frpc 那一步")
	}

	file := filepath.Join(app.Base, "tunnels.json")
	saved := readFile(t, file)
	edit := Tunnel{Provider: "custom", Name: "改了名", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: 25565, Bound: "S2"}
	if err := m.Update(tun.ID, edit); err == nil || err.Error() != "这条隧道正在启动，等它起来或停掉以后再改" {
		t.Errorf("正在启动时修改应拒绝：%v", err)
	}
	if err := m.Delete(tun.ID); err == nil || err.Error() != "这条隧道正在启动，等它起来或停掉以后再删" {
		t.Errorf("正在启动时删除应拒绝：%v", err)
	}
	if got := readFile(t, file); got != saved {
		t.Errorf("被拒绝的修改、删除不该写 tunnels.json：\n之前 %s\n之后 %s", saved, got)
	}
	if l := m.List(); len(l) != 1 || l[0].Name != "MC" || l[0].Bound != "S1" || !l[0].AutoStart {
		t.Errorf("内存里的配置不该变：%+v", l)
	}

	free()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("测试里的 frpc 起不来，启动应报失败")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("放行以后启动没有结束")
	}
	// 准备完了：能改、能删
	if err := m.Update(tun.ID, edit); err != nil {
		t.Fatalf("启动结束以后应能修改：%v", err)
	}
	if err := m.Delete(tun.ID); err != nil {
		t.Fatalf("启动结束以后应能删除：%v", err)
	}
}

// 登记成运行中之前再核对一遍：隧道还在、配置还是启动时拍下的那份。准备期间改、删已经被拦着，这里兜住以后别的路径改了配置的情况——
// 对不上就把刚起来的 frpc 结束掉、不登记，报一句能直接显示的话（Codex 复审 beta.3 的建议）。
func TestStartDropsFrpcWhenConfigChanged(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("frpc 结构检查只认 Windows PE 程序（Mac 版 frpc 见 D01）")
	}
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
	tun, err := m.Add(Tunnel{Provider: "custom", Name: "MC", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 30001, LocalPort: 25565, Bound: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	old := startFrpc
	t.Cleanup(func() { startFrpc = old })
	for _, c := range []struct {
		name   string
		change func() // 拿着 m.mu 时调：模拟不经过 Update / Delete 改掉的配置
	}{
		{"改了绑定", func() { m.get(tun.ID).Bound = "S2" }},
		{"删掉了", func() { m.tunnels = nil }},
	} {
		var started *exec.Cmd
		startFrpc = func(cmd *exec.Cmd) error {
			m.mu.Lock()
			c.change()
			m.mu.Unlock()
			started = cmd
			return cmd.Start()
		}
		err := m.Start(tun.ID)
		if err == nil || !strings.Contains(err.Error(), "被改过或删掉了") {
			t.Fatalf("%s：启动应报配置变了：%v", c.name, err)
		}
		m.mu.Lock()
		rs, starting := m.running[tun.ID], m.starting[tun.ID]
		m.mu.Unlock()
		if rs != nil || starting {
			t.Fatalf("%s：不该登记成运行中，也不该留着「正在启动」：running=%v starting=%v", c.name, rs != nil, starting)
		}
		if started == nil || started.ProcessState == nil {
			t.Fatalf("%s：刚起来的 frpc 应被结束并等它退出", c.name)
		}
		if le := m.lastError(tun.ID); !strings.Contains(le, "被改过或删掉了") {
			t.Fatalf("%s：卡片上的失败原因：%q", c.name, le)
		}
	}
}
