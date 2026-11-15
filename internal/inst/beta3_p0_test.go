package inst

// beta.3 第一层：可用性走查整改 P0（docs/可用性走查-2026-11-14.md 第八节 R01–R05，外加 U30）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// R03：端口被占时没启动起来，原因写进控制台（和找不到 Java 那一行一样留痕），并记成「上次启动没成功」；
// 端口放开以后再启动、进程起来了，这条就清掉。服务器已经在运行时又点启动，不算启动失败。
func TestStartFailureIsRecordedAndClearedOnNextStart(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	ln, port := occupy(t, "127.0.0.1")
	m, i := portInst(t, "启动失败留痕", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port), mcsrc.CoreVanilla)
	err := m.Start(i.Name)
	if err == nil {
		t.Fatal("端口被占着应启动失败")
	}
	want := fmt.Sprintf("端口 %d 被别的程序占着，换个端口或关掉占用它的程序", port)
	if got := i.LastStartError(); got != want {
		t.Fatalf("上次启动失败的原因：%q，应为 %q", got, want)
	}
	if line := "[MineDeck] 没能启动：" + want; !slices.Contains(i.Console.Lines(), line) {
		t.Fatalf("控制台里应有 %q：%q", line, i.Console.Lines())
	}

	ln.Close() // 端口放开了：这次能起来，上次的原因不再算数
	if err := m.Start(i.Name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // 中途失败也把扮演服务器的进程收掉，不然临时目录删不掉
		if i.Status() != "stopped" {
			_ = m.Kill(i.Name)
			waitStatus(t, i, "stopped")
		}
	})
	if got := i.LastStartError(); got != "" {
		t.Fatalf("进程起来以后应清掉上次的原因：%q", got)
	}
	waitStatus(t, i, "running")
	// 已经在运行时又点启动：这是「现在不能启动」，不是服务器起不来，不该记成启动失败
	if err := m.Start(i.Name); err == nil {
		t.Fatal("运行中再启动应报错")
	}
	if got := i.LastStartError(); got != "" {
		t.Fatalf("运行中再点启动不该记成启动失败：%q", got)
	}
	_ = m.Kill(i.Name)
	waitStatus(t, i, "stopped")
	if got := i.LastStartError(); got != "" {
		t.Fatalf("停下来也不该冒出启动失败：%q", got)
	}
}

// R03：找不到 Java 也记下来（控制台那一行带路径，原来就有）。
func TestStartFailureMissingJava(t *testing.T) {
	m, i := portInst(t, "没有 Java", "", mcsrc.CoreVanilla)
	i.JavaPath = filepath.Join(t.TempDir(), "没有这个", "java.exe")
	if err := m.Start(i.Name); err == nil {
		t.Fatal("找不到 Java 应启动失败")
	}
	if got := i.LastStartError(); !strings.Contains(got, "找不到这台服务器用的 Java") {
		t.Fatalf("上次启动失败的原因：%q", got)
	}
	if !slices.ContainsFunc(i.Console.Lines(), func(l string) bool { return strings.HasPrefix(l, "[MineDeck] 找不到 Java：") }) {
		t.Fatalf("控制台里应有找不到 Java 的那一行：%q", i.Console.Lines())
	}
}

// R02：新建时按开关把 white-list 明确写进 server.properties（不论版本），默认关；代理端没有这个文件，不写。
func TestCreateWritesWhitelist(t *testing.T) {
	for _, on := range []bool{false, true} {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"server.properties": "server-port=25565\nmotd=MineDeck 服务器\n"})
		tk, _ := tasks.NewManager().New("新建", nil)
		pub := withCreateWhitelist(tk, CreateReq{Core: mcsrc.CorePaper, MC: "26.3", Whitelist: on}, nil)
		if err := pub(&Instance{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		if got := javaPropsOf(filepath.Join(dir, "server.properties"))["white-list"]; got != fmt.Sprint(on) {
			t.Fatalf("开关 %v：white-list=%q", on, got)
		}
	}
	// Forge / NeoForge 这类第一次启动前还没有 server.properties 的：照样写上
	dir := t.TempDir()
	tk, _ := tasks.NewManager().New("新建", nil)
	if err := withCreateWhitelist(tk, CreateReq{Core: mcsrc.CoreNeoForge}, nil)(&Instance{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if got := javaPropsOf(filepath.Join(dir, "server.properties"))["white-list"]; got != "false" {
		t.Fatalf("没有 server.properties 时也要写：%q", got)
	}
	// 代理端：不写，也不新建 server.properties
	px := t.TempDir()
	if pub := withCreateWhitelist(tk, CreateReq{Core: mcsrc.CoreVelocity, Whitelist: true}, nil); pub != nil {
		if err := pub(&Instance{Dir: px}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(px, "server.properties")); !os.IsNotExist(err) {
		t.Fatalf("代理端不该有 server.properties：%v", err)
	}
}

// R02：导入整合包，覆盖目录自带 server.properties（还把 white-list 写了两行）时，以导入页的开关为准，按服务端的读法写对。
func TestModpackOverrideWhitelistFollowsForm(t *testing.T) {
	m, root := importEnv(t)
	req := CreateReq{Name: "整合包白名单", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root, Whitelist: false}
	dir, err := m.validateCreate(&req)
	if err != nil {
		t.Fatal(err)
	}
	defer m.releaseCreating(req.Name)
	defer m.releaseCreating(dirKey(dir))
	writeTree(t, dir, map[string]string{"server.properties": "server-port=25565\n", "server.jar": "core"})
	i := &Instance{Settings: Settings{Name: req.Name, Core: req.Core, MC: req.MC, JavaPath: testJava17, JavaMajor: 17, LaunchTarget: "jar:server.jar", CreatedAt: time.Now()}, Dir: dir, Console: newConsole()}
	task, ctx := m.Tasks.New("导入", append(append([]string{}, createSteps...), "下载", "覆盖"))
	pack := &modpack.Pack{ZipPath: zipOrdered(t, [][2]string{{"overrides/server.properties", "white-list=true\nmotd=pack\nwhite-list=true\n"}}), OverrideDirs: []string{"overrides"}}
	if err := m.completeCreate(ctx, task, i, withCreateWhitelist(task, req, func(i *Instance) error {
		return m.applyModpack(ctx, task, pack, req, i, "")
	})); err != nil {
		t.Fatal(err)
	}
	props := javaPropsOf(filepath.Join(dir, "server.properties"))
	if props["white-list"] != "false" || props["motd"] != "pack" {
		t.Fatalf("应以导入页的开关为准、别的沿用整合包：%v", props)
	}
	if log := strings.Join(task.Snapshot().Log, "\n"); !strings.Contains(log, "整合包自带的 server.properties 里白名单是「开」，按导入页上的开关改成了「关」") {
		t.Fatalf("日志应写明改了整合包的白名单：%s", log)
	}
}

// R02：导入现成服务端压缩包：包里写着白名单开，导入页没开，以导入页为准；包里没有 server.properties 时按导入页写开。
func TestServerImportWhitelistFollowsForm(t *testing.T) {
	m, _ := importEnv(t)
	for _, c := range []struct {
		name, props string
		on          bool
		want        string
	}{
		{"包里开着", "white-list=true\nserver-port=25565\n", false, "false"},
		{"包里没有", "", true, "true"},
	} {
		files := map[string]string{"paper-1.20.1-100.jar": paperJarBytes(t)}
		if c.props != "" {
			files["server.properties"] = c.props
		}
		pack, err := modpack.Parse(zipOf(t, files))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(app.ServersDir, "wl-"+fmt.Sprint(c.on))
		tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
		req := CreateReq{Name: c.name, XmxMB: 2048, Port: 25576, EULA: true, Whitelist: c.on}
		if err := m.runServerImport(context.Background(), tk, pack, req, dir); err != nil {
			t.Fatalf("%s：导入失败 %v", c.name, err)
		}
		if got := javaPropsOf(filepath.Join(dir, "server.properties"))["white-list"]; got != c.want {
			t.Fatalf("%s：white-list=%q，应为 %s", c.name, got, c.want)
		}
	}
}

// R02：读白名单开关按服务端的读法（后一行盖前一行）；没写这一项是「没写」，不是「关」（26.3 起服务端默认开）。接管的识别结果带上它。
func TestWhitelistOnReadsLikeServer(t *testing.T) {
	dir := t.TempDir()
	if WhitelistOn(dir) != nil {
		t.Fatal("没有 server.properties 时应为 nil")
	}
	for body, want := range map[string]string{
		"motd=x\n":                          "nil",
		"white-list=false\nwhite-list=true\n": "true", // 服务端认最后一行；MineDeck 自己的 Props 认第一行，不能用它判断
		"white-list : TRUE\n":                 "true",
		"white-list=false\n":                  "false",
	} {
		writeTree(t, dir, map[string]string{"server.properties": body})
		got := "nil"
		if p := WhitelistOn(dir); p != nil {
			got = fmt.Sprint(*p)
		}
		if got != want {
			t.Errorf("%q：%s，应为 %s", body, got, want)
		}
	}
	writeTree(t, dir, map[string]string{"server.properties": "white-list=false\nwhite-list=true\n",
		"server.jar": "zip:" + manifestOf("net.minecraft.bundler.Main") + "|version.json={\"id\":\"1.21.4\",\"java_version\":21}"})
	sc, err := scanServerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.WhitelistOn == nil || !*sc.WhitelistOn {
		t.Fatalf("接管识别应按服务端的读法给出白名单开着：%v", sc.WhitelistOn)
	}
}

// R01：代理端的连接端口读它自己的配置文件；配置还没生成时是 0（不猜）；游戏服务器就是 server-port。
func TestConnectPort(t *testing.T) {
	dir := t.TempDir()
	vel := &Instance{Settings: Settings{Core: mcsrc.CoreVelocity}, Dir: dir}
	if p := vel.ConnectPort(); p != 0 {
		t.Fatalf("还没生成 velocity.toml 时应为 0：%d", p)
	}
	writeTree(t, dir, map[string]string{"velocity.toml": "# 注释里的 bind = \"x:1\" 不算\nconfig-version = \"2.7\"\nbind = \"0.0.0.0:25588\"\nmotd = \"hi\"\n"})
	if p := vel.ConnectPort(); p != 25588 {
		t.Fatalf("Velocity：%d", p)
	}
	wdir := t.TempDir()
	writeTree(t, wdir, map[string]string{"config.yml": "servers:\n  lobby:\n    address: localhost:25565\nlisteners:\n- query_port: 25577\n  motd: '&1Another Bungee server'\n  host: 0.0.0.0:25599\n  max_players: 1\n"})
	wf := &Instance{Settings: Settings{Core: mcsrc.CoreWaterfall}, Dir: wdir}
	if p := wf.ConnectPort(); p != 25599 {
		t.Fatalf("Waterfall：%d", p)
	}
	gdir := t.TempDir()
	writeTree(t, gdir, map[string]string{"server.properties": "server-port=25570\n", "velocity.toml": "bind = \"0.0.0.0:1\"\n"})
	if p := (&Instance{Settings: Settings{Core: mcsrc.CorePaper}, Dir: gdir}).ConnectPort(); p != 25570 {
		t.Fatalf("游戏服务器应是 server-port：%d", p)
	}
}

// U30：接管来的服务器，列出文件夹里 MineDeck 写的、移出列表后会留下的文件（存在的才列）；自己建的服务器不列。
func TestMineDeckFilesOnlyForAdopted(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"run.bat": "x", runBatBackup: "old", "user_jvm_args.txt": "# MineDeck 生成\n", jvmArgsBackup: "old",
		".minedeck/config-backups/a~1": "x", "server.properties": "", "world/level.dat": "x"})
	i := &Instance{Settings: Settings{Adopted: true, LaunchTarget: "args:libraries/net/neoforged/neoforge/21.1.1/win_args.txt"}, Dir: dir}
	want := []string{"run.bat", runBatBackup, "user_jvm_args.txt", jvmArgsBackup, ".minedeck"}
	if got := i.MineDeckFiles(); !slices.Equal(got, want) {
		t.Fatalf("接管来的：%q，应为 %q", got, want)
	}
	// 直接跑 jar 的：user_jvm_args.txt 不是 MineDeck 写的，不列
	i.LaunchTarget = "jar:server.jar"
	if got := i.MineDeckFiles(); slices.Contains(got, "user_jvm_args.txt") {
		t.Fatalf("不是 Forge 的参数文件启动时不该列 user_jvm_args.txt：%q", got)
	}
	i.Adopted = false
	if got := i.MineDeckFiles(); got != nil {
		t.Fatalf("自己建的服务器不列：%q", got)
	}
}
