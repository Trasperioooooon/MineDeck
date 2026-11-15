package tunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/secret"
)

// 这一组测的是 tunnels.json 没读成的时候。以前读不出来、解析不了都当成空列表，之后任何一次保存都把原文件整份盖掉，
// 里面的隧道和凭据就没了。现在分三种：文件不在是新装，照常；读不出来、解析不了记成 LoadError，读成之前什么都不存，
// 原文件不动；读成了（放开了、修好了）自动恢复，不用重启。被别的程序占着的那条在 load_windows_test.go。

// fastLoadRetry 启动时的重读不等那么久，测试跑得快。
func fastLoadRetry(t *testing.T) {
	t.Helper()
	oldWait, oldN := loadRetryWait, loadRetries
	t.Cleanup(func() { loadRetryWait, loadRetries = oldWait, oldN })
	loadRetryWait = time.Millisecond
}

func tunnelsPath() string { return filepath.Join(app.Base, "tunnels.json") }

// seedTunnels 先用正常的 Manager 存一条带凭据的隧道，返回磁盘上的原文。
func seedTunnels(t *testing.T) string {
	t.Helper()
	if _, err := NewManager().Add(openfrpTunnel("一号")); err != nil {
		t.Fatal(err)
	}
	return readFile(t, tunnelsPath())
}

func wantLoadError(t *testing.T, err error, corrupt bool) *LoadError {
	t.Helper()
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("没读成时应报 LoadError，实际：%v", err)
	}
	if le.Corrupt != corrupt {
		t.Fatalf("LoadError.Corrupt=%v，应为 %v：%v", le.Corrupt, corrupt, err)
	}
	if se := (*SaveError)(nil); errors.As(err, &se) {
		t.Fatalf("LoadError 不该包成 SaveError（账户层会把它说成「平台上已经建好」）：%v", err)
	}
	return le
}

// 没读成时：新增、修改、删除、启动都报 LoadError（不说「隧道不存在」），内存里还是空的、编号不动，磁盘上的原文件一字不变。
func assertBlocked(t *testing.T, m *Manager, before string, corrupt bool) {
	t.Helper()
	wantLoadError(t, m.LoadErr(), corrupt)
	if ids := tunnelIDs(m); len(ids) != 0 {
		t.Fatalf("没读成时列表应是空的：%v", ids)
	}
	got, err := m.Add(openfrpTunnel("二号"))
	wantLoadError(t, err, corrupt)
	if got != nil {
		t.Fatalf("没读成时不该加进去：%+v", got)
	}
	if m.seq != 0 || len(m.tunnels) != 0 {
		t.Fatalf("没读成时内存被改了：seq=%d tunnels=%d", m.seq, len(m.tunnels))
	}
	wantLoadError(t, m.Update("tun1", openfrpTunnel("改名")), corrupt)
	wantLoadError(t, m.Delete("tun1"), corrupt)
	wantLoadError(t, m.Start("tun1"), corrupt)
	wantLoadError(t, m.save(), corrupt) // 最后一道闸：哪个保存点漏了检查，也写不下去
	if after := readFile(t, tunnelsPath()); after != before {
		t.Fatalf("没读成时原文件被改了：\n前：%s\n后：%s", before, after)
	}
}

// 读成以后：直接再加一条（不先问 LoadErr，新增自己就要再读一次），编号接着原文件里的往下排，
// 重启后旧的和新的都在，旧隧道的凭据还解得开。
func assertRecovered(t *testing.T, m *Manager) {
	t.Helper()
	a, err := m.Add(openfrpTunnel("二号"))
	if err != nil || a.ID != "tun2" {
		t.Fatalf("恢复以后再加应成功、编号是 tun2：%+v %v", a, err)
	}
	m2 := NewManager()
	if ids := tunnelIDs(m2); !slices.Equal(ids, []string{"tun1", "tun2"}) {
		t.Fatalf("重启后的隧道：%v（原来那条应还在）", ids)
	}
	if cred, err := secret.Unseal(m2.get("tun1").Credential); err != nil || cred != "openfrp-key-一号" {
		t.Fatalf("原来那条的凭据：%q %v", cred, err)
	}
}

// 文件不在：新装，空列表是对的，照常能加、能存。
func TestLoadMissingFileStartsEmpty(t *testing.T) {
	tempBase(t)
	m := NewManager()
	if err := m.LoadErr(); err != nil {
		t.Fatalf("文件不在不算没读成：%v", err)
	}
	if ids := tunnelIDs(m); len(ids) != 0 {
		t.Fatalf("新装应没有隧道：%v", ids)
	}
	if a, err := m.Add(openfrpTunnel("一号")); err != nil || a.ID != "tun1" {
		t.Fatalf("新装时新增：%+v %v", a, err)
	}
	if ids := tunnelIDs(NewManager()); !slices.Equal(ids, []string{"tun1"}) {
		t.Fatalf("重启后：%v", ids)
	}
}

// 坏文件（写到一半的 JSON）：不当空列表，什么都不存，原文件不动；用户修好以后不用重启，下一次操作就读回来。
func TestLoadCorruptFileBlocksSavesUntilFixed(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	good := seedTunnels(t)
	broken := good[:len(good)/2] // 断在半截：凭据的密文也只剩半截
	if err := os.WriteFile(tunnelsPath(), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	assertBlocked(t, m, broken, true)
	le := wantLoadError(t, m.LoadErr(), true)
	if !strings.Contains(le.Error(), "行附近") || !strings.Contains(le.Error(), "内容坏了") {
		t.Fatalf("坏文件的报错要说在第几行附近：%v", le)
	}

	// 用户用记事本修好了（记事本存的带 BOM，也要认）
	if err := os.WriteFile(tunnelsPath(), append([]byte("\xef\xbb\xbf"), good...), 0o600); err != nil {
		t.Fatal(err)
	}
	assertRecovered(t, m)
}

// 坏文件被用户改名挪走：下一次读就是「文件不在」，从空列表开始；挪走的那份原样留着。
func TestLoadCorruptFileMovedAwayStartsEmpty(t *testing.T) {
	tempBase(t)
	if err := os.WriteFile(tunnelsPath(), []byte(`{"seq":1,"tunnels":[{"id":"tun1",`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), true)
	if err := os.Rename(tunnelsPath(), tunnelsPath()+".bad"); err != nil {
		t.Fatal(err)
	}
	if a, err := m.Add(openfrpTunnel("新的")); err != nil || a.ID != "tun1" {
		t.Fatalf("挪走以后应能从空列表开始：%+v %v", a, err)
	}
	if got := readFile(t, tunnelsPath()+".bad"); got != `{"seq":1,"tunnels":[{"id":"tun1",` {
		t.Fatalf("挪走的那份被动了：%s", got)
	}
}

// JSON 格式对、结构不对（比如整个是个数组）也算坏文件。
func TestLoadWrongShapeIsCorrupt(t *testing.T) {
	tempBase(t)
	if err := os.WriteFile(tunnelsPath(), []byte(`[{"id":"tun1"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	assertBlocked(t, m, `[{"id":"tun1"}]`, true)
}

// 读不出来（这里用同名的文件夹顶在那里，各个系统上读它都报错）：不当空列表；挪开、放回原文件以后自动恢复。
// 被别的程序占着的真实情况在 load_windows_test.go。
func TestLoadUnreadableBlocksSavesUntilReadable(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	good := seedTunnels(t)
	aside := tunnelsPath() + ".aside"
	if err := os.Rename(tunnelsPath(), aside); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tunnelsPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), false)
	if _, err := m.Add(openfrpTunnel("二号")); err == nil {
		t.Fatal("读不出来时不该加进去")
	} else {
		wantLoadError(t, err, false)
	}
	if err := os.Remove(tunnelsPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aside, tunnelsPath()); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, tunnelsPath()); got != good {
		t.Fatal("原文件被动了")
	}
	assertRecovered(t, m)
}

// 程序文件夹暂时访问不了（网络盘断了、U 盘拔了）：Windows 上读文件也报「不存在」，但这不是新装。
// 要是当成新装，文件夹一回来，下一次新增就把原文件盖掉（Codex 复审 P1）。这里把文件夹挪开再挪回来模拟。
func TestLoadUnreachableFolderIsNotNewInstall(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	before := seedTunnels(t)
	real := app.Base
	away := real + "-away"
	if err := os.Rename(real, away); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), false)
	if _, err := m.Add(openfrpTunnel("二号")); err == nil {
		t.Fatal("程序文件夹访问不了时不该加进去")
	}
	if err := os.Rename(away, real); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, tunnelsPath()); got != before {
		t.Fatal("原文件被动了")
	}
	assertRecovered(t, m)
}

// 解析得了、却没有 tunnels 这一项（手改时把键名改错了）：隧道和凭据还在别的键底下，按坏文件处理；
// {} 里没有东西，照空列表算；键名大小写不同照常读（encoding/json 本来就不分大小写）。
func TestLoadMissingTunnelsKey(t *testing.T) {
	tempBase(t)
	typo := `{"seq":1,"tunels":[{"id":"tun1","provider":"openfrp","name":"一号","credential":"dpapi:AAAA","proxyId":"42"}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(typo), 0o600); err != nil {
		t.Fatal(err)
	}
	assertBlocked(t, NewManager(), typo, true)

	if err := os.WriteFile(tunnelsPath(), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewManager().LoadErr(); err != nil {
		t.Fatalf("{} 不该挡保存：%v", err)
	}

	upper := `{"Seq":1,"Tunnels":[{"id":"tun1","provider":"custom","name":"自建","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(upper), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if err := m.LoadErr(); err != nil || !slices.Equal(tunnelIDs(m), []string{"tun1"}) {
		t.Fatalf("键名大小写不同应照常读：%v %v", err, tunnelIDs(m))
	}
}

// 修好以后，修改、删除、启动各自就会再读一次、照常做，不用先有人看列表（Codex 复审建议补的）。
func TestEachOperationRecoversOnItsOwn(t *testing.T) {
	good := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"游戏","credential":"k","proxyId":"10"}]}`
	setup := func(t *testing.T) *Manager {
		tempBase(t)
		if err := os.WriteFile(tunnelsPath(), []byte(good[:30]), 0o600); err != nil {
			t.Fatal(err)
		}
		m := NewManager()
		wantLoadError(t, m.LoadErr(), true)
		if err := os.WriteFile(tunnelsPath(), []byte(good), 0o600); err != nil {
			t.Fatal(err)
		}
		return m
	}
	t.Run("修改", func(t *testing.T) {
		m := setup(t)
		if err := m.Update("tun1", Tunnel{Provider: "natfrp", Name: "改了名", Credential: "k2", ProxyID: "10"}); err != nil {
			t.Fatal(err)
		}
		if disk := readFile(t, tunnelsPath()); !strings.Contains(disk, "改了名") || strings.Contains(disk, `"k2"`) {
			t.Fatalf("修改应写进去（凭据照旧是加密的）：%s", disk)
		}
	})
	t.Run("删除", func(t *testing.T) {
		m := setup(t)
		if err := m.Delete("tun1"); err != nil {
			t.Fatal(err)
		}
		if ids := tunnelIDs(NewManager()); len(ids) != 0 {
			t.Fatalf("删除应写进去：%v", ids)
		}
	})
	t.Run("启动", func(t *testing.T) {
		m := setup(t)
		err := m.Start("tun1") // 这里没有樱花的 frpc：走到「首次使用需手动放置」说明已经读回来、找到了这条隧道
		if err == nil || errors.As(err, new(*LoadError)) || !strings.Contains(err.Error(), "首次使用需手动放置樱花 frpc") {
			t.Fatalf("启动应先读回来再去找 frpc：%v", err)
		}
	})
}

// 只有空白的文件（老版本覆盖写时断电留下的）：里面没有能丢的东西，按空列表算，不挡保存。
func TestLoadBlankFileIsEmpty(t *testing.T) {
	tempBase(t)
	if err := os.WriteFile(tunnelsPath(), []byte(" \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if err := m.LoadErr(); err != nil {
		t.Fatalf("空文件不该挡保存：%v", err)
	}
	if _, err := m.Add(openfrpTunnel("一号")); err != nil {
		t.Fatal(err)
	}
}

// 列表里有 null 的项（手改出来的）：跳过，其余照常读；以前读到它会崩。
func TestLoadSkipsNullEntries(t *testing.T) {
	tempBase(t)
	data := `{"seq":1,"tunnels":[null,{"id":"tun1","provider":"custom","name":"自建","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if err := m.LoadErr(); err != nil {
		t.Fatal(err)
	}
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1"}) {
		t.Fatalf("null 项应跳过：%v", ids)
	}
}

// 开机时没读成、之后读成了的那一次，也照常把老版本的明文凭据加密写回（迁移不能因为晚读而漏掉）。
func TestRecoveredLoadStillMigratesPlaintext(t *testing.T) {
	tempBase(t)
	if err := os.WriteFile(tunnelsPath(), []byte(`{"seq":1,"tunnels":[{"id":"tun1",`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), true)
	legacy := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"樱花","credential":"sakura-access-key","proxyId":"9","autoStart":false}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadErr(); err != nil {
		t.Fatal(err)
	}
	if disk := readFile(t, tunnelsPath()); strings.Contains(disk, "sakura-access-key") || !strings.Contains(disk, `"dpapi:`) {
		t.Fatalf("读成以后应把明文凭据加密写回：%s", disk)
	}
}

// 平台账户的创建、导入：没读成时在找平台之前就拦下（不然平台上建好一条、本机存不进去），报的是 LoadError 本身，
// 不是「平台上的隧道已经建好」。
func TestAccountsRefuseWhileTunnelsUnread(t *testing.T) {
	tempBase(t)
	if err := os.WriteFile(tunnelsPath(), []byte(`{"tunnels":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	a := NewAccounts(m, secret.NewStore(filepath.Join(app.Base, "secrets.json")))
	_, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{Provider: "openfrp", Name: "mc", NodeID: 1, LocalPort: 25565, Type: "tcp", RemotePort: 30000})
	wantLoadError(t, err, true)
	if errors.As(err, new(*createdNotSavedError)) {
		t.Fatalf("还没去平台上建，不该说已经建好：%v", err)
	}
	_, err = a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "openfrp", ProxyID: "1001"})
	wantLoadError(t, err, true)
}

// 跟随实例启动：开机时没读成、之后又没人打开隧道页，服务器启动时 StartBound 自己再读一次；
// 还是没读成就什么都不拉，读成了照常去拉跟着这台服务器的隧道（这里没有樱花的 frpc，拉的结果是「首次使用需手动放置」，
// 说明确实去拉了）。
func TestStartBoundRetriesLoad(t *testing.T) {
	tempBase(t)
	old := startDelay
	startDelay = 10 * time.Millisecond
	t.Cleanup(func() { startDelay = old })
	data := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"游戏","credential":"k","proxyId":"10","boundInstance":"S1","autoStart":true}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(data[:20]), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), true)
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })

	m.StartBound("S1")
	select {
	case a := <-notes:
		t.Fatalf("没读成时不该去拉隧道：%+v", a)
	case <-time.After(200 * time.Millisecond):
	}

	if err := os.WriteFile(tunnelsPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m.StartBound("S1")
	select {
	case a := <-notes:
		if a.Op != "tunnel.start" || a.Args["id"] != "tun1" || a.Err == nil || !strings.Contains(a.Err.Error(), "首次使用需手动放置樱花 frpc") {
			t.Fatalf("读成以后应去拉跟随的隧道：%+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("读成以后没有去拉跟随的隧道")
	}
}
