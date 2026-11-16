package inst

// beta.3 第二层下半：端口冲突（docs/可用性走查-2026-11-14.md 第八节 R08）。
// 事前避开：表单的默认端口跳过本软件别的服务器已经在用的；事后指名带路：启动时端口被本软件的另一台服务器占着，
// 报错点它的名字，并给一个能改成的端口。

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 两台服务器配了同一个端口：第一台开着（占着端口）时启动第二台，报错点第一台的名字，不说「别的程序」；
// 记下的 startPortBusy 有占着它的服务器和一个能改成的端口（往上找、跳过本软件别的服务器配的、现在绑得上）。
func TestStartPortBusyNamesOtherServer(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	ln, port := occupy(t, "127.0.0.1")
	ln.Close() // 先拿到一个空端口：第一台服务器配它
	props := fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port)
	m, a := portInst(t, "生存服", props, mcsrc.CoreVanilla)
	_, b := portInst(t, "第二台", props, mcsrc.CoreVanilla)
	// 第三台停着，配的是 port+1：建议的端口要跳过它
	_, c := portInst(t, "停着的", fmt.Sprintf("server-port=%d\n", port+1), mcsrc.CoreVanilla)
	m.insts[b.Name], m.insts[c.Name] = b, c

	if err := m.Start(a.Name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if a.Status() != "stopped" {
			_ = m.Kill(a.Name)
			waitStatus(t, a, "stopped")
		}
	})
	waitStatus(t, a, "running")
	// 扮演服务器的测试程序自己不绑端口：替它占上
	hold, err := listenOn("127.0.0.1", port)
	if err != nil {
		t.Skipf("端口 %d 刚放开就被别的程序占了：%v", port, err)
	}
	defer hold.Close()

	err = m.Start(b.Name)
	var busy *portBusyError
	if err == nil || !errors.As(err, &busy) {
		t.Fatalf("端口被占着应拒绝启动：%v", err)
	}
	want := fmt.Sprintf("端口 %d 被你的服务器「生存服」占着，先停掉它，或者给这台换个端口", port)
	if err.Error() != want || b.LastStartError() != want {
		t.Fatalf("报错应点出占着端口的服务器：%q（记下的 %q），应为 %q", err, b.LastStartError(), want)
	}
	if strings.Contains(err.Error(), "别的程序") {
		t.Errorf("占着的是本软件的服务器，不该说别的程序：%s", err)
	}
	pb := b.LastStartPortBusy()
	if pb == nil || pb.Port != port || pb.Owner != "生存服" {
		t.Fatalf("startPortBusy：%+v", pb)
	}
	if pb.Suggest <= port || pb.Suggest == port+1 {
		t.Errorf("建议的端口应往上找、跳过别的服务器配的 %d：%d", port+1, pb.Suggest)
	}
	if pb.Suggest > 0 && checkBindPort(bindTarget{"tcp", "127.0.0.1", pb.Suggest}) != nil {
		t.Errorf("建议的端口 %d 现在应绑得上", pb.Suggest)
	}
	if line := "[MineDeck] 没能启动：" + want; !containsLine(b.Console.Lines(), line) {
		t.Errorf("控制台里应有 %q", line)
	}

	// 第一台停下以后，端口还被别的程序占着：照旧说别的程序，不点名、不给建议
	_ = m.Kill(a.Name)
	waitStatus(t, a, "stopped")
	err = m.Start(b.Name)
	if err == nil || err.Error() != fmt.Sprintf("端口 %d 被别的程序占着，换个端口或关掉占用它的程序", port) {
		t.Fatalf("停着的服务器不算占着：%v", err)
	}
	if pb := b.LastStartPortBusy(); pb == nil || pb.Owner != "" || pb.Suggest != 0 || pb.Port != port {
		t.Errorf("别的程序占着时 startPortBusy 只有端口：%+v", pb)
	}

	// 改成建议的端口再启动：起来了，上次的原因和 startPortBusy 一起清掉
	hold.Close()
	if err := m.Start(b.Name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if b.Status() != "stopped" {
			_ = m.Kill(b.Name)
			waitStatus(t, b, "stopped")
		}
	})
	if b.LastStartError() != "" || b.LastStartPortBusy() != nil {
		t.Errorf("起来以后应清掉：%q %+v", b.LastStartError(), b.LastStartPortBusy())
	}
	// 别的原因没起来（找不到 Java）：没有 startPortBusy
	_ = m.Kill(b.Name)
	waitStatus(t, b, "stopped")
	b.JavaPath = filepath.Join(t.TempDir(), "没有", "java.exe")
	if err := m.Start(b.Name); err == nil || b.LastStartPortBusy() != nil {
		t.Errorf("找不到 Java 不该带 startPortBusy：%v %+v", err, b.LastStartPortBusy())
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// 端口默认值：从 25565 起，本软件别的服务器都没用的最小端口；代理端按它自己配置里的端口算，还没生成配置的不算；
// usedBy 是起点被哪台用着。except 那台不算。
func TestDefaultPortSkipsUsed(t *testing.T) {
	mk := func(name string, core mcsrc.Core, files map[string]string) *Instance {
		dir := t.TempDir()
		for n, body := range files {
			if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return &Instance{Settings: Settings{Name: name, Core: core}, Dir: dir, Console: newConsole()}
	}
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}
	for _, i := range []*Instance{
		mk("生存服", mcsrc.CorePaper, nil), // 没有 server.properties：按服务端默认的 25565
		mk("整合包", mcsrc.CoreForge, map[string]string{"server.properties": "server-port=25566\n"}),
		mk("群组", mcsrc.CoreVelocity, map[string]string{"velocity.toml": "bind = \"0.0.0.0:25567\"\n", "server.properties": "server-port=25570\n"}),
		mk("没生成配置的代理", mcsrc.CoreWaterfall, nil),
		mk("远的", mcsrc.CoreVanilla, map[string]string{"server.properties": "server-port=25569\n"}),
	} {
		m.insts[i.Name] = i
	}
	used := m.PortsInUse("")
	var got []string
	for _, u := range used {
		got = append(got, fmt.Sprintf("%d=%s", u.Port, u.Instance))
	}
	if strings.Join(got, ",") != "25565=生存服,25566=整合包,25567=群组,25569=远的" {
		t.Errorf("在用的端口：%v", got)
	}
	if p, by := DefaultPort(used, 25565); p != 25568 || by != "生存服" {
		t.Errorf("默认端口：%d（%s），应为 25568（生存服）", p, by)
	}
	if p, by := DefaultPort(used, 0); p != 25568 || by != "生存服" {
		t.Errorf("没给起点按 25565 算：%d %s", p, by)
	}
	if p, by := DefaultPort(used, 25568); p != 25568 || by != "" {
		t.Errorf("起点本来就空着：%d %q", p, by)
	}
	if p, by := DefaultPort(m.PortsInUse("生存服"), 25565); p != 25565 || by != "" {
		t.Errorf("不算自己时 25565 是空的：%d %q", p, by)
	}
	if p, _ := DefaultPort(nil, 25565); p != 25565 {
		t.Errorf("一台服务器都没有：%d", p)
	}
	// 另外跳过被别的程序占着的（beta.3 之后，回访走查 11-16）：25568 被占着就再往上；usedBy 照旧只说本软件的服务器
	busy := func(p int) bool { return p == 25568 || p == 25570 }
	if p, by := DefaultPortSkipping(used, 25565, busy); p != 25570+1 || by != "生存服" {
		t.Errorf("跳过别的程序占着的：%d（%s），应为 25571（生存服）", p, by)
	}
	if p, by := DefaultPortSkipping(nil, 25568, busy); p != 25569 || by != "" {
		t.Errorf("起点被别的程序占着：%d %q", p, by)
	}
	if p, by := DefaultPortSkipping(used, 25565, nil); p != 25568 || by != "生存服" {
		t.Errorf("busy 为 nil 时同 DefaultPort：%d %q", p, by)
	}
}

// 接管：请求里的端口和文件夹里写的一样（或者没给）就不碰 server.properties；不一样只改 server-port 这一行；
// 接管中途失败，server.properties 和别的文件一起恢复原样。
func TestAdoptPortChange(t *testing.T) {
	const props = "#Minecraft server properties\nmotd=老服务器\nserver-port=25565\nlevel-name=world\n"
	setup := func(t *testing.T) (*Manager, string, *AdoptScan) {
		m, user := adoptEnv(t)
		dir := filepath.Join(user, "old")
		writeTree(t, dir, map[string]string{
			"server.jar":        "zip:" + manifestOf("net.minecraft.bundler.Main") + "|version.json={\"id\":\"1.20.1\",\"java_version\":17}",
			"eula.txt":          "eula=true\n",
			"server.properties": props,
			"world/level.dat":   "存档",
		})
		sc, err := m.ScanAdopt(dir)
		if err != nil {
			t.Fatal(err)
		}
		return m, dir, sc
	}
	req := func(sc *AdoptScan, dir, java string, port int) AdoptReq {
		return AdoptReq{Dir: dir, Name: "老服务器", Core: mcsrc.Core(sc.Core), MC: sc.MC, LaunchTarget: sc.LaunchTarget, XmxMB: 2048,
			JavaMajor: 17, JavaPath: java, Port: port}
	}

	t.Run("端口一样不动", func(t *testing.T) {
		m, dir, sc := setup(t)
		if _, err := adopt(t, m, req(sc, dir, fakeJava(t), 25565)); err != nil {
			t.Fatal(err)
		}
		if got := mustRead(t, filepath.Join(dir, "server.properties")); got != props {
			t.Errorf("端口没变不该动 server.properties：%q", got)
		}
	})
	t.Run("端口不一样只改这一行", func(t *testing.T) {
		m, dir, sc := setup(t)
		log, err := adopt(t, m, req(sc, dir, fakeJava(t), 25601))
		if err != nil {
			t.Fatal(err)
		}
		got := mustRead(t, filepath.Join(dir, "server.properties"))
		if !strings.Contains(got, "server-port=25601") || strings.Contains(got, "server-port=25565") || !strings.Contains(got, "level-name=world") {
			t.Errorf("server.properties：%q", got)
		}
		if i, _ := m.Get("老服务器"); i == nil || i.Port() != 25601 {
			t.Errorf("接管以后的端口应是 25601")
		}
		if !strings.Contains(log, "从 25565 改成了 25601") || !strings.Contains(log, "配置文件只改了端口") {
			t.Errorf("任务日志要写明改了端口：%s", log)
		}
	})
	t.Run("失败时恢复原样", func(t *testing.T) {
		m, dir, sc := setup(t)
		r := req(sc, dir, fakeJava(t), 25601)
		vsc, err := m.validateAdopt(&r)
		if err != nil {
			t.Fatal(err)
		}
		defer m.releaseCreating(r.Name)
		defer m.releaseCreating(vsc.key)
		// 识别以后、写文件之前 eula.txt 被改成没同意：写到 eula 那一步失败，已经改过的端口要一起退回去
		writeTree(t, dir, map[string]string{"eula.txt": "eula=false\n"})
		task, _ := m.Tasks.New("接管", adoptSteps)
		if err := m.runAdopt(offline(), task, r, vsc); err == nil {
			t.Fatal("没同意 EULA 应接管失败")
		}
		if got := mustRead(t, filepath.Join(dir, "server.properties")); got != props {
			t.Errorf("失败以后 server.properties 应恢复原样：%q", got)
		}
	})
	t.Run("代理端不看端口", func(t *testing.T) {
		r := AdoptReq{Name: "代理", Core: mcsrc.CoreVelocity, MC: "3.4.0", Port: 25601}
		m, _ := adoptEnv(t)
		_, _ = m.validateAdopt(&r) // 文件夹是空的，后面会报错；这里只看端口有没有被清掉
		if r.Port != 0 {
			t.Errorf("代理端的端口在它自己的配置里，不该改 server.properties：%d", r.Port)
		}
	})
}

// listenOn 在 host:port 上占住端口（同 occupy，但端口由调用方给）。
func listenOn(host string, port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}
