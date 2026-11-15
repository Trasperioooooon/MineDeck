package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
)

// 凭据短于 6 个字符（自定义 frps 的令牌本来可以很短）：不拒绝，界面上添加时只给一句提示（beta.3 主会话定的，见复审发现 10）。
// 已经存好的短凭据隧道照常读出来、照常启动，令牌原样交给 frpc；日志里含它的行整行隐藏这一点不变（见 credmask_test.go）。
func TestShortCredentialTunnelStillLoadsAndStarts(t *testing.T) {
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
	// 一条是老版本存的明文短令牌，一条是这一版加密存的短令牌
	legacy := `{"seq":1,"tunnels":[{"id":"tun1","provider":"custom","name":"老的","credential":"abc","serverAddr":"frp.example.com","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false}]}`
	if err := os.WriteFile(filepath.Join(app.Base, "tunnels.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	first := NewManager()
	added, err := first.Add(Tunnel{Provider: "custom", Name: "新的", Credential: "k1", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25567, LocalPort: 25565})
	if err != nil {
		t.Fatalf("短凭据不该被拒：%v", err)
	}

	m := NewManager() // 重新读一遍：已经存好的
	if err := m.LoadErr(); err != nil {
		t.Fatal(err)
	}
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("应读出两条隧道：%+v", list)
	}
	for _, st := range list {
		if !st.HasCredential || st.CredBroken {
			t.Fatalf("短凭据的隧道应照常可用：%+v", st)
		}
	}

	old := startFrpc
	t.Cleanup(func() { startFrpc = old })
	var mu sync.Mutex
	seen := map[string][2][]string{} // 配置文件路径 → 参数、环境变量
	startFrpc = func(cmd *exec.Cmd) error {
		mu.Lock()
		seen[cmd.Args[len(cmd.Args)-1]] = [2][]string{cmd.Args, cmd.Env}
		mu.Unlock()
		return cmd.Start()
	}
	for id, want := range map[string]string{"tun1": "abc", added.ID: "k1"} {
		if err := m.Start(id); err != nil {
			t.Fatalf("%s：短凭据的隧道应能启动：%v", id, err)
		}
		cfgPath := filepath.Join(frpcDir("custom"), id+".toml")
		mu.Lock()
		got, ok := seen[cfgPath]
		mu.Unlock()
		if !ok {
			t.Fatalf("%s：没有按 %s 启动 frpc：%v", id, cfgPath, seen)
		}
		if tok, ok := renderLikeFrp(t, readFile(t, cfgPath), got[1]); !ok || tok != want {
			t.Fatalf("%s：交给 frpc 的令牌是 %q（%v），应为 %q", id, tok, ok, want)
		}
		for _, a := range got[0] {
			if strings.Contains(a, want) && a != cfgPath {
				t.Fatalf("%s：令牌出现在命令行参数里：%v", id, got[0])
			}
		}
	}
	// 等两个替身 frpc 退出，免得临时目录删不掉
	deadline := time.Now().Add(10 * time.Second)
	for {
		running := 0
		for _, st := range m.List() {
			if st.Running {
				running++
			}
		}
		if running == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("替身 frpc 没有退出：%+v", m.List())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
