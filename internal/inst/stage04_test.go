package inst

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// A05-1：崩溃自动重启遇上端口一直被占，不再只等 waitPortFree 那一次就报「不再自动重试」，
// 而是按崩溃重启已有的退避间隔和 3 次上限接着排，每次控制台说一句在等端口；占满 3 次才放弃。
func TestCrashRestartPortBusyRetriesUpToLimit(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	oldDelays := crashRestartDelays
	oldTimes, oldGap := portWaitTimes, portWaitGap
	t.Cleanup(func() {
		crashRestartDelays = oldDelays
		portWaitTimes, portWaitGap = oldTimes, oldGap
	})
	crashRestartDelays = []time.Duration{20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond}
	portWaitTimes, portWaitGap = 2, 10*time.Millisecond

	t.Run("第二次重试前端口释放则成功拉起", func(t *testing.T) {
		ln, port := occupy(t, "127.0.0.1")
		m, i := portInst(t, "崩重等端口成功", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port), mcsrc.CoreVanilla)
		i.Policies.CrashRestart = true
		i.startedAt = time.Now()

		var mu sync.Mutex
		var attempts []AutoAction
		m.SetAutoHook(func(a AutoAction) {
			mu.Lock()
			attempts = append(attempts, a)
			n := len(attempts)
			mu.Unlock()
			if n == 1 {
				// 第一次自动重启因为端口被占失败后释放端口，等第 2 次退避把它拉起来
				_ = ln.Close()
			}
		})

		m.crashRestart(i, 0)
		t.Cleanup(func() {
			if i.Status() != "stopped" {
				_ = m.Kill(i.Name)
				waitStatus(t, i, "stopped")
			}
		})
		waitStatus(t, i, "running")

		mu.Lock()
		gotAttempts := append([]AutoAction(nil), attempts...)
		mu.Unlock()
		if len(gotAttempts) != 2 || gotAttempts[0].Err == nil || gotAttempts[1].Err != nil {
			t.Fatalf("应在第 1 次端口被占失败后排第 2 次并成功：%+v", gotAttempts)
		}
		con := strings.Join(i.Console.Lines(), "\n")
		if strings.Contains(con, "不再自动重试") {
			t.Errorf("还有重试次数时不应报不再自动重试：\n%s", con)
		}
		if !strings.Contains(con, "仍在等待端口") {
			t.Errorf("控制台应说明仍在等待端口：\n%s", con)
		}
	})

	t.Run("端口一直占着按上限试满3次后放弃", func(t *testing.T) {
		_, port := occupy(t, "127.0.0.1")
		m, i := portInst(t, "崩重等端口放弃", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port), mcsrc.CoreVanilla)
		i.Policies.CrashRestart = true
		i.startedAt = time.Now()

		var mu sync.Mutex
		var attempts []AutoAction
		m.SetAutoHook(func(a AutoAction) {
			mu.Lock()
			attempts = append(attempts, a)
			mu.Unlock()
		})

		m.crashRestart(i, 0)

		mu.Lock()
		gotAttempts := append([]AutoAction(nil), attempts...)
		mu.Unlock()
		if len(gotAttempts) != 3 {
			t.Fatalf("端口一直被占时应按上限试满 3 次即停，实际 %d 次：%+v", len(gotAttempts), gotAttempts)
		}
		for idx, a := range gotAttempts {
			if a.Err == nil || a.Args["attempt"] != idx+1 {
				t.Errorf("第 %d 次记录不对：%+v", idx+1, a)
			}
		}
		con := strings.Join(i.Console.Lines(), "\n")
		if got := strings.Count(con, "仍在等待端口"); got != 2 {
			t.Errorf("前两次失败应各说一句仍在等待端口（共 2 次），实际 %d 次：\n%s", got, con)
		}
		if got := strings.Count(con, "不再自动重试，端口放开以后请手动启动"); got != 1 {
			t.Errorf("第 3 次用完上限后应报一次不再自动重试，实际 %d 次：\n%s", got, con)
		}
	})
}

// A05-2：server-ip 写了解析很慢的域名时，域名解析在 procMu 锁外做，等解析期间读状态等操作不被挡住。
func TestStartSlowServerIPDoesNotHoldProcLock(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	oldLookup := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = oldLookup })

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host == "slow-bind.invalid" {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return net.DefaultResolver.LookupIPAddr(ctx, host)
	}

	ln, port := occupy(t, "127.0.0.1")
	_ = ln.Close()
	m, i := portInst(t, "慢域名测试", fmt.Sprintf("server-ip=slow-bind.invalid\nserver-port=%d\n", port), mcsrc.CoreVanilla)

	startDone := make(chan struct{})
	var startErr error
	go func() {
		defer close(startDone)
		startErr = m.Start(i.Name)
	}()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-startDone
		if i.Status() != "stopped" {
			_ = m.Kill(i.Name)
			waitStatus(t, i, "stopped")
		}
	})

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Start 没有走到域名解析")
	}

	// 此时 Start 正卡在慢域名解析里：读状态、读上次启动错误、读内存快照都不能被 procMu 挡住
	readDone := make(chan string, 1)
	go func() {
		st := i.Status()
		_ = i.LastStartError()
		_, _, _ = i.MemSnapshot()
		readDone <- st
	}()
	select {
	case st := <-readDone:
		if st != "stopped" {
			t.Errorf("解析期间状态应仍是 stopped，得到 %q", st)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("慢域名解析期间 i.Status() 被锁挡住了")
	}

	close(release)
	<-startDone
	if startErr != nil {
		t.Fatalf("放行解析后 Start 应成功：%v", startErr)
	}
	waitStatus(t, i, "running")
}

// A06：serverReady 认整行格式（原版、Paper、Forge、NeoForge、Fabric、Velocity + Mohist、Waterfall），模组提前打印或聊天里的不算。
func TestServerReadyStrictFormats(t *testing.T) {
	positives := []string{
		`[20:15:00] [Server thread/INFO]: Done (7.812s)! For help, type "help"`,
		`[20:15:00] [Server thread/INFO]: Done (8.123s)! For help, type "help"`,
		`[20:14:59 INFO]: Done (6.812s)! For help, type "help"`,
		`[20:14:59] [Server thread/INFO]: Done (6.812s)! For help, type "help" or "?"`,
		`[20:15:05] [Server thread/INFO] [minecraft/DedicatedServer]: Done (12.345s)! For help, type "help"`,
		`[09:31:09] [main/INFO] [minecraft/DedicatedServer]: Done (7.212s)! For help, type "help"`,
		`[02Nov2026 12:34:03.123] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Done (12.345s)! For help, type "help"`,
		`[20:15:00] [Server thread/INFO]: Done (3,456s)! For help, type "help"`,
		`[12:00:00] [Server thread/INFO]: Done (1.0s)!`,
		`[20:15:00 INFO]: Done (0.36s)!`,
		"\x1b[32m[20:15:00 INFO]: Done (0.36s)!\x1b[m",
		`[20:15:00] [main/INFO] [com.velocitypowered.proxy.VelocityServer]: Done (1.23s)!`,
		`[20:15:00 INFO]: Mohist 启动成功`,
		`[20:15:00 INFO]: Mohist started`,
		`20:15:00 [INFO] Listening on /0.0.0.0:25577`,
	}
	for _, line := range positives {
		if !serverReady(line) {
			t.Errorf("正例应识别为启动完成：%q", line)
		}
	}

	negatives := []string{
		`[20:15:00] [Modloading-worker-1/INFO]: Done (loading 3 things)!`,
		`[20:15:00] [Server thread/INFO]: [MyMod] Done (1.234s)!`,
		`[20:15:00] [Server thread/INFO]: ModInit: Done (1.234s)! For help, type "help"`,
		`[20:15:00] [Server thread/INFO] [create/KineticNetwork]: Done (0.123s)!`,
		`[20:15:00] [Server thread/INFO]: <Steve> Done (1.234s)! For help, type "help"`,
		`[20:15:00] [Server thread/INFO]: [Not Secure] <Steve> Done (1.234s)!`,
		`[20:15:00] [Server thread/WARN]: Done (1.234s)! For help, type "help"`,
		`[20:15:00] [Server thread/INFO]: Done (1.234s)! extra trailing words`,
		`Done (1.234s)! For help, type "help"`,
		`[MineDeck] Done (1.234s)!`,
	}
	for _, line := range negatives {
		if serverReady(line) {
			t.Errorf("反例不应被误判为启动完成：%q", line)
		}
	}
}

// A07：上次启动失败原因落盘在 .minedeck/ 下（不改 instance.json），新建 Manager 重加载仍在；手动关闭提示或下次启动成功后删除。
func TestStartFailPersistedAcrossManagerReload(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ln, port := occupy(t, "127.0.0.1")
	dir := filepath.Join(app.ServersDir, "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsBytes, _ := json.MarshalIndent(Settings{
		Name: "落盘测试服", Core: mcsrc.CoreVanilla, MC: "1.21.4", JavaMajor: 21, JavaPath: exe,
		XmxMB: 64, XmsMB: 64, LaunchTarget: "jar:server.jar",
	}, "", "  ")
	instJSONPath := filepath.Join(dir, "instance.json")
	if err := os.WriteFile(instJSONPath, settingsBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port)), 0o644); err != nil {
		t.Fatal(err)
	}

	m1, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.Start("落盘测试服"); err == nil {
		t.Fatal("端口占着时应启动失败")
	}
	afterJSON, err := os.ReadFile(instJSONPath)
	if err != nil || !bytes.Equal(afterJSON, settingsBytes) {
		t.Fatalf("落盘启动失败原因不应改动 instance.json")
	}
	failFile := filepath.Join(dir, ".minedeck", startFailFileName)
	if _, err := os.Stat(failFile); err != nil {
		t.Fatalf("应在 .minedeck 下生成启动失败文件：%v", err)
	}

	// 模拟程序重开：新建 Manager 再加载，错误原因与端口占用详情仍在
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	i2, err := m2.Get("落盘测试服")
	if err != nil {
		t.Fatal(err)
	}
	if got := i2.LastStartError(); !strings.Contains(got, fmt.Sprintf("端口 %d", port)) {
		t.Fatalf("重开后 LastStartError 丢失：%q", got)
	}
	if pb := i2.LastStartPortBusy(); pb == nil || pb.Port != port {
		t.Fatalf("重开后 LastStartPortBusy 丢失：%+v", pb)
	}

	// 用户在界面上关掉提示：内存和磁盘上的文件都清掉
	if err := m2.DismissStartError("落盘测试服"); err != nil {
		t.Fatal(err)
	}
	if i2.LastStartError() != "" || i2.LastStartPortBusy() != nil {
		t.Fatalf("关闭提示后内存中应清空：%q %+v", i2.LastStartError(), i2.LastStartPortBusy())
	}
	if _, err := os.Stat(failFile); !os.IsNotExist(err) {
		t.Fatalf("关闭提示后磁盘文件应删掉：%v", err)
	}

	// 再次启动失败落盘，然后放开端口启动成功：文件同样被清掉
	if err := m2.Start("落盘测试服"); err == nil {
		t.Fatal("端口仍占着应失败")
	}
	_ = ln.Close()
	m3, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := m3.Start("落盘测试服"); err != nil {
		t.Fatalf("端口放开后启动应成功：%v", err)
	}
	i3, _ := m3.Get("落盘测试服")
	t.Cleanup(func() {
		if i3.Status() != "stopped" {
			_ = m3.Kill("落盘测试服")
			waitStatus(t, i3, "stopped")
		}
	})
	waitStatus(t, i3, "running")
	if i3.LastStartError() != "" || i3.LastStartPortBusy() != nil {
		t.Fatalf("启动成功后内存中应清空：%q %+v", i3.LastStartError(), i3.LastStartPortBusy())
	}
	if _, err := os.Stat(failFile); !os.IsNotExist(err) {
		t.Fatalf("启动成功后磁盘文件应删掉：%v", err)
	}
	_ = m3.Kill("落盘测试服")
	waitStatus(t, i3, "stopped")

	// 文件损坏读不出来时当没有，不影响加载实例
	if err := os.MkdirAll(filepath.Dir(failFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failFile, []byte("{broken json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m4, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i4, err := m4.Get("落盘测试服"); err != nil || i4.LastStartError() != "" || i4.LastStartPortBusy() != nil {
		t.Fatalf("损坏的启动失败文件应按没有处理：%v %q", err, i4.LastStartError())
	}
}

// A14：server.properties 缺 white-list 行时，26.3 起按服务端默认「开」算，更早版本按「关」算；写了这一行时以最后一行值为准。
func TestEffectiveWhitelistOnByVersion(t *testing.T) {
	emptyDir := t.TempDir()
	for _, c := range []struct {
		mc   string
		want bool
	}{
		{"", false},
		{"1.20.1", false},
		{"1.21.11", false},
		{"26.2", false},
		{"26.3", true},
		{"26.3-snapshot-1", true},
		{"26.3-pre-2", true},
		{"26.4", true},
		{"27.1", true},
	} {
		if got := EffectiveWhitelistOn(emptyDir, c.mc); got != c.want {
			t.Errorf("缺行时 EffectiveWhitelistOn(mc=%q) = %v，应为 %v", c.mc, got, c.want)
		}
	}

	dirExplicitOff := t.TempDir()
	_ = os.WriteFile(filepath.Join(dirExplicitOff, "server.properties"), []byte("white-list=true\nwhite-list=false\n"), 0o644)
	if EffectiveWhitelistOn(dirExplicitOff, "26.3") {
		t.Error("26.3 明确写了最后一行 white-list=false 时应为 false")
	}

	dirExplicitOn := t.TempDir()
	_ = os.WriteFile(filepath.Join(dirExplicitOn, "server.properties"), []byte("white-list=true\n"), 0o644)
	if !EffectiveWhitelistOn(dirExplicitOn, "1.20.1") {
		t.Error("老版本明确写了 white-list=true 时应为 true")
	}
}
