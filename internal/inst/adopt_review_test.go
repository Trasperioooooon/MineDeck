package inst

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// 这个文件里的用例来自 Gemini 4 Argon 的只读复审（2026-11-09）：每条都是它指出、核对后成立的问题。

const (
	paperOf  = "|META-INF/versions.list=abc\t%s\t%s/paper-%s.jar\n"
	forgeMF  = "net.minecraftforge.fml.relauncher.ServerLaunchWrapper"
	shimMF   = "net.minecraftforge.bootstrap.shim.Main"
	vanillaO = "net.minecraft.server.MinecraftServer"
)

func paperJar(mc string) string {
	return "zip:" + manifestOf("io.papermc.paperclip.Main") + strings.ReplaceAll(paperOf, "%s", mc)
}

// 启动脚本和目录内容里的各种坑：注释掉的旧命令、Forge 安装器留下的 run.sh、%~dp0、新旧 jar 并存、shim.jar。
func TestScanPicksTheRightTarget(t *testing.T) {
	forge := func(build string) string { return strings.Replace(forgeArgs, "%s", build, 1) }
	cases := []struct {
		name    string
		files   map[string]string
		target  string
		core    string
		mc      string
		xmx     int
		targets int // 候选个数，0 = 不查
	}{
		{name: "注释掉的旧命令不算：rem 那行的 jar 和内存都不当真",
			files: map[string]string{
				"old_paper.jar": paperJar("1.19.4"), "paper-1.20.4.jar": paperJar("1.20.4"),
				"start.bat": "@echo off\r\nREM java -Xmx16G -jar old_paper.jar nogui\r\n:: java -Xmx32G -jar old_paper.jar\r\njava -Xmx4G -jar paper-1.20.4.jar nogui\r\n",
			},
			target: "jar:paper-1.20.4.jar", core: "paper", mc: "1.20.4", xmx: 4096},
		{name: "从 Forge 换成了 Paper，Forge 安装器留下的 run.sh 没人管：以改过的 run.bat 为准",
			files: map[string]string{
				"libraries/net/minecraftforge/forge/1.20.1-47.2.0/win_args.txt": forge("47.2.0"),
				"run.sh":           "#!/usr/bin/env sh\njava @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.2.0/unix_args.txt \"$@\"\n",
				"run.bat":          "java -Xmx3G -jar paper-1.20.1.jar nogui\r\npause\r\n",
				"paper-1.20.1.jar": paperJar("1.20.1"),
			},
			target: "jar:paper-1.20.1.jar", core: "paper", mc: "1.20.1", xmx: 3072},
		{name: "只有 .sh 时才看 .sh",
			files: map[string]string{
				"libraries/net/minecraftforge/forge/1.20.1-47.2.17/win_args.txt": forge("47.2.17"),
				"libraries/net/minecraftforge/forge/1.20.1-47.4.20/win_args.txt": forge("47.4.20"),
				"run.sh": "java -Xmx5G @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.2.17/unix_args.txt \"$@\"\n",
			},
			target: "args:libraries/net/minecraftforge/forge/1.20.1-47.2.17/win_args.txt", core: "forge", mc: "1.20.1", xmx: 5120},
		{name: "升级后新旧两个 jar 都留着，脚本里用的是变量：选版本新的",
			files: map[string]string{
				"paper-1.20.1-196.jar": paperJar("1.20.1"), "paper-1.20.4-496.jar": paperJar("1.20.4"),
				"start.bat": "set JAR=paper-1.20.4-496.jar\r\njava -Xmx2G -jar %JAR% nogui\r\n",
			},
			target: "jar:paper-1.20.4-496.jar", core: "paper", mc: "1.20.4", xmx: 2048},
		{name: "脚本里写的是 \"%~dp0xxx.jar\"",
			files: map[string]string{
				"forge-1.16.5-36.2.39.jar":    "zip:" + manifestOf(forgeMF),
				"minecraft_server.1.16.5.jar": "zip:" + manifestOf(vanillaO),
				"启动.bat":                      "java -Xmx6G -jar \"%~dp0forge-1.16.5-36.2.39.jar\" nogui\r\n",
			},
			target: "jar:forge-1.16.5-36.2.39.jar", core: "forge", mc: "1.16.5", xmx: 6144},
		{name: "老版本的 Purpur 改名成 purpur.jar（里面没有 versions.list）",
			files:  map[string]string{"purpur.jar": "zip:" + manifestOf("io.papermc.paperclip.Paperclip") + "|patch.properties=version=1.16.5\n"},
			target: "jar:purpur.jar", core: "purpur", mc: "1.16.5"},
		{name: "老版本的 Paper 改名成 server.jar：是 Paper，不说它是「分支」",
			files:  map[string]string{"server.jar": "zip:" + manifestOf("io.papermc.paperclip.Paperclip") + "|patch.properties=version=1.16.5\n"},
			target: "jar:server.jar", core: "paper", mc: "1.16.5"},
		{name: "新版 Forge：根目录的 shim.jar 和参数文件是一回事，只列参数文件；脚本点名 shim.jar 也算",
			files: map[string]string{
				"libraries/net/minecraftforge/forge/1.20.6-50.1.0/win_args.txt": strings.Replace(forge("50.1.0"), "1.20.1", "1.20.6", 1),
				"forge-1.20.6-50.1.0-shim.jar":                                  "zip:" + manifestOf(shimMF),
				"start.bat":                                                     "java -Xmx7G -jar forge-1.20.6-50.1.0-shim.jar nogui\r\n",
			},
			target: "args:libraries/net/minecraftforge/forge/1.20.6-50.1.0/win_args.txt", core: "forge", mc: "1.20.6", xmx: 7168, targets: 1},
		{name: "只有 shim.jar、没有参数文件：就用它启动",
			files:  map[string]string{"forge-1.20.6-50.1.0-shim.jar": "zip:" + manifestOf(shimMF)},
			target: "jar:forge-1.20.6-50.1.0-shim.jar", core: "forge", mc: "1.20.6", targets: 1},
		{name: "认不出的混合服和原版 jar 放在一起：真正的服务端是认不出的那个",
			files: map[string]string{
				"arclight-forge-1.20.1-1.0.5.jar": "zip:" + manifestOf("io.izzel.arclight.server.Launcher"),
				"server.jar":                      "zip:" + manifestOf("net.minecraft.bundler.Main") + "|version.json={\"id\":\"1.20.1\",\"java_version\":17}",
			},
			target: "jar:arclight-forge-1.20.1-1.0.5.jar", core: "", mc: "1.20.1", targets: 2},
		{name: "脚本里先跑了个小工具：内存看点名服务端的那一行",
			files: map[string]string{
				"server.jar": vanilla21,
				"start.bat":  "java -Xmx128M -jar updater.jar\r\njava -Xms1G -Xmx6G -jar server.jar nogui\r\n",
			},
			target: "jar:server.jar", core: "vanilla", mc: "1.21.1", xmx: 6144},
		{name: "安装器装的 Fabric：原版 jar 在 .fabric/server 下面，版本从那里看",
			files: map[string]string{
				"fabric-server-launch.jar":         "zip:" + manifestOf("net.fabricmc.loader.impl.launch.server.FabricServerLauncher"),
				".fabric/server/1.20.4-server.jar": "不用打开",
			},
			target: "jar:fabric-server-launch.jar", core: "fabric", mc: "1.20.4"},
		{name: "日志比旁边留着的旧原版 jar 更能说明现在的版本",
			files: map[string]string{
				"fabric-server-launch.jar": "zip:" + manifestOf("net.fabricmc.loader.impl.launch.server.FabricServerLauncher"),
				"old-vanilla.jar":          "zip:" + manifestOf("net.minecraft.bundler.Main") + "|version.json={\"id\":\"1.18.2\",\"java_version\":17}",
				"logs/latest.log":          "[10:00:00] [Server thread/INFO]: Starting minecraft server version 1.20.4\n",
			},
			target: "jar:fabric-server-launch.jar", core: "fabric", mc: "1.20.4"},
		{name: "改了名的 Mohist：版本看 jar 里面写的",
			files:  map[string]string{"server.jar": "zip:" + manifestOf("com.mohistmc.MohistMCStart") + "|META-INF/versions.list=abc\t1.20.1\t1.20.1/mohist-1.20.1.jar\n"},
			target: "jar:server.jar", core: "mohist", mc: "1.20.1"},
		{name: "BungeeCord：按 Waterfall 管，版本从清单里取",
			files:  map[string]string{"BungeeCord.jar": "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: net.md_5.bungee.Bootstrap\r\nImplementation-Version: git:BungeeCord-Bootstrap:1.21-R0.1-SNAPSHOT:abc:1890\r\n\r\n"},
			target: "jar:BungeeCord.jar", core: "waterfall", mc: "1.21"},
		{name: "Forge 跨 Minecraft 版本升级过：按 Forge 自己的版本号比新旧，不被目录名带偏",
			files: map[string]string{
				"libraries/net/minecraftforge/forge/1.20-46.0.14/win_args.txt":   strings.Replace(forge("46.0.14"), "1.20.1", "1.20", 1),
				"libraries/net/minecraftforge/forge/1.20.1-47.2.20/win_args.txt": forge("47.2.20"),
			},
			target: "args:libraries/net/minecraftforge/forge/1.20.1-47.2.20/win_args.txt", core: "forge", mc: "1.20.1", targets: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, c.files)
			sc, err := scanServerDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if sc.LaunchTarget != c.target || sc.Core != c.core || sc.MC != c.mc || sc.XmxMB != c.xmx {
				t.Errorf("认成了 target=%q core=%q mc=%q xmx=%d\n候选：%+v\n提醒：%v", sc.LaunchTarget, sc.Core, sc.MC, sc.XmxMB, sc.Targets, sc.Notes)
			}
			if c.targets != 0 && len(sc.Targets) != c.targets {
				t.Errorf("候选应有 %d 个：%+v", c.targets, sc.Targets)
			}
			if strings.Contains(c.name, "不说它是「分支」") && strings.Contains(strings.Join(sc.Notes, ""), "分支") {
				t.Errorf("%v", sc.Notes)
			}
			if c.core == "waterfall" && !strings.Contains(strings.Join(sc.Notes, ""), "BungeeCord") {
				t.Errorf("%v", sc.Notes)
			}
		})
	}
}

// user_jvm_args.txt 是 Java 的参数文件：行尾的 # 注释不是参数，引号里的空白不拆；带空格的参数带不过来，要数出来提醒。
func TestReadUserJVMArgs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "user_jvm_args.txt")
	body := "\xef\xbb\xbf# Xmx and Xms set the maximum and minimum RAM usage\r\n" +
		"-Xmx4G # 分配 4G 内存\r\n" +
		"-Xms1G -XX:+UseG1GC   -Dfoo=a#b\r\n" +
		"  # 整行注释 -Dnope=1\r\n" +
		"-XX:HeapDumpPath=\"D:/MC Server/dumps\"\r\n" +
		"'-Dquoted=1' -Xmx6G\r\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	xmx, extra, skipped := readUserJVMArgs(p)
	if xmx != 6144 || skipped != 1 || !slices.Equal(extra, []string{"-XX:+UseG1GC", "-Dfoo=a#b", "-Dquoted=1"}) {
		t.Errorf("xmx=%d skipped=%d extra=%q", xmx, skipped, extra)
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs, "user_jvm_args.txt": body})
	sc, err := scanServerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if notes := strings.Join(sc.Notes, ""); sc.XmxMB != 6144 || !strings.Contains(notes, "3 个 JVM 参数会带过来") || !strings.Contains(notes, "1 个带空格的 JVM 参数带不过来") {
		t.Errorf("xmx=%d %v", sc.XmxMB, sc.Notes)
	}
}

func TestVersionLess(t *testing.T) {
	for _, c := range []struct{ older, newer string }{
		{"47.2.17", "47.4.20"}, {"1.20", "1.20.1"}, {"1.9.4", "1.10"}, {"1.21.1", "26.1"}, {"46.0.14", "47.2.20"},
		{"20.4.190-beta", "20.4.190"}, {"21.0.0-beta", "21.0.1-beta"}, {"26.3-snapshot-1", "26.3-snapshot-2"}, {"26.3-snapshot-2", "26.3"},
	} {
		if !versionLess(c.older, c.newer) || versionLess(c.newer, c.older) {
			t.Errorf("%s 应比 %s 旧", c.older, c.newer)
		}
	}
	if versionLess("1.20.1", "1.20.1") {
		t.Error("一样的版本不算旧")
	}
}

func TestXmxOf(t *testing.T) {
	for in, want := range map[string]int{
		"java -Xmx4G -jar a.jar": 4096, "-Xmx6144M": 6144, "-xmx2g": 2048, "-Xmx1048576K": 1024,
		"-Xmx128M": 0, "-Xmx18014398509481985G": 0, "-Xmx2048G": 0, "no flags": 0,
		"-Xmx2G -Xmx8G": 8192, "-Xmx8G -Xmx64M": 8192,
	} {
		if got := xmxOf([]byte(in)); got != want {
			t.Errorf("%q → %d，应为 %d", in, got, want)
		}
	}
}

// MineDeck 管过的目录、启动文件没变：核心和版本信原来记的，候选的说明跟着改，猜错的提醒不出现。
func TestScanTrustsPreviousSettings(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"custom.jar":   "zip:" + manifestOf("io.papermc.paperclip.Main") + "|META-INF/versions.list=abc\t1.20.4\t1.20.4/pufferfish-1.20.4.jar\n",
		settingsBackup: `{"name":"老服","core":"purpur","mc":"1.20.4","build":"2176","launchTarget":"jar:custom.jar","xmxMb":3072}`,
	})
	sc, err := scanServerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Core != "purpur" || sc.Build != "2176" || sc.Targets[0].Core != "purpur" || !strings.Contains(sc.Targets[0].Label, "Purpur") || len(sc.Notes) != 0 {
		t.Errorf("%+v", sc)
	}
}

// 接管中途失败：动过的文件全部恢复原样，文件夹和接管之前一模一样。
func TestAdoptRollsBackOnFailure(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "pack")
	before := map[string]string{
		"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs,
		"run.bat":           "@echo off\r\njava @user_jvm_args.txt @libraries/net/neoforged/neoforge/21.1.265/win_args.txt %*\r\n",
		"user_jvm_args.txt": "-Xmx6G\r\n-Dfoo=bar\r\n",
		"eula.txt":          "#By changing the setting below to TRUE you are indicating your agreement to our EULA.\neula=false\n",
		"world/level.dat":   "存档",
	}
	writeTree(t, dir, before)
	// run.bat 设成只读：备份、改写 user_jvm_args.txt、写 eula.txt 都成功之后，写 run.bat 这一步失败
	bat := filepath.Join(dir, "run.bat")
	if err := os.Chmod(bat, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bat, 0o644) })

	log, err := adopt(t, m, AdoptReq{Dir: dir, Name: "整合包", Core: mcsrc.CoreNeoForge, MC: "1.21.1", Build: "21.1.265",
		LaunchTarget: "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt", XmxMB: 4096, JavaMajor: 21, JavaPath: fakeJava(t), EULA: true})
	if err == nil {
		t.Fatalf("run.bat 只读时应失败\n%s", log)
	}
	for name, body := range before {
		if got := mustRead(t, filepath.Join(dir, filepath.FromSlash(name))); got != body {
			t.Errorf("%s 没恢复原样：%q", name, got)
		}
	}
	for _, name := range []string{"instance.json", "instance.json.tmp", runBatBackup, jvmArgsBackup, log4jXML} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("失败后留下了 %s", name)
		}
	}
	if _, err := m.Get("整合包"); err == nil {
		t.Error("失败了还登记了")
	}
	if len(m.creating) != 0 {
		t.Errorf("占位没放：%v", m.creating)
	}
}

// 移出列表之后再接管，这回选了 Forge 的参数文件启动：user_jvm_args.txt 是用户自己写的，里面的参数要并进来再改写它。
func TestAdoptBackKeepsUserJVMArgs(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "pack")
	writeTree(t, dir, map[string]string{
		"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs,
		"user_jvm_args.txt": "-Xmx6G\r\n-XX:+UseZGC\r\n-Dfoo=bar\r\n",
		"eula.txt":          "eula=true\n",
		settingsBackup:      `{"name":"整合包","core":"neoforge","mc":"1.21.1","build":"21.1.265","launchTarget":"jar:gone.jar","xmxMb":3072,"extraJvm":["-XX:+UseZGC"]}`,
	})
	sc, err := m.ScanAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-XX:+UseZGC", "-Dfoo=bar"}; !slices.Equal(sc.ExtraJVM, want) || sc.XmxMB != 3072 {
		t.Errorf("识别：%q xmx=%d", sc.ExtraJVM, sc.XmxMB)
	}
	if log, err := adopt(t, m, AdoptReq{Dir: dir, Name: sc.Name, Core: mcsrc.CoreNeoForge, MC: sc.MC, Build: sc.Build,
		LaunchTarget: sc.LaunchTarget, XmxMB: sc.XmxMB, JavaMajor: 21, JavaPath: fakeJava(t)}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	i, _ := m.Get("整合包")
	if !slices.Equal(i.ExtraJVM, []string{"-XX:+UseZGC", "-Dfoo=bar"}) {
		t.Errorf("用户写在 user_jvm_args.txt 里的参数丢了：%q", i.ExtraJVM)
	}
	if got := mustRead(t, filepath.Join(dir, jvmArgsBackup)); !strings.Contains(got, "-Dfoo=bar") {
		t.Errorf("原来的 user_jvm_args.txt 没备份：%q", got)
	}
}

// 经目录联接（junction）绕一圈进来的同一个文件夹，要认得出来；备份目录在里面的文件夹不让接管。
func TestCheckAdoptDirRealPaths(t *testing.T) {
	m, user := adoptEnv(t)
	real := filepath.Join(user, "srv")
	writeTree(t, real, map[string]string{"server.jar": vanilla21})
	m.insts["srv"] = &Instance{Settings: Settings{Name: "srv"}, Dir: real, Console: newConsole()}

	bk := filepath.Join(user, "with-backups", "bk")
	writeTree(t, filepath.Join(user, "with-backups"), map[string]string{"server.jar": vanilla21, "bk/x.zip": "x"})
	setConfig(t, func(c *app.Config) { c.BackupsDir = bk })
	if _, err := m.checkAdoptDir(filepath.Join(user, "with-backups")); err == nil || !strings.Contains(err.Error(), "备份目录") {
		t.Errorf("里面放着备份目录的文件夹应拒绝：%v", err)
	}
	setConfig(t, func(c *app.Config) { c.BackupsDir, c.BackupRoots = "", nil })

	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	link := filepath.Join(user, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) }) // 只删联接本身，不动它指的文件夹
	if got := realPath(link); !strings.EqualFold(got, realPath(real)) {
		t.Fatalf("目录联接没解开：%q，应为 %q", got, realPath(real))
	}
	if _, err := m.checkAdoptDir(link); err == nil || !strings.Contains(err.Error(), "已经在列表里了，就是「srv」") {
		t.Errorf("经目录联接进来的同一个文件夹应拒绝：%v", err)
	}
	if _, err := m.checkAdoptDir(filepath.Join(link, "sub")); err == nil || !strings.Contains(err.Error(), "找不到") {
		t.Errorf("%v", err)
	}
	// 没登记过的文件夹经联接进来可以接管，登记的目录是用户选的那个路径
	other := filepath.Join(user, "other")
	writeTree(t, other, map[string]string{"server.jar": vanilla21})
	link2 := filepath.Join(user, "link2")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link2, other).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link2) })
	if got, err := m.checkAdoptDir(link2); err != nil || got != link2 {
		t.Errorf("%q %v", got, err)
	}
}

// 一个文件夹正在接管（或正在新建）时，它自己、它的上层和下层都不能再接管。
func TestValidateAdoptOverlapInFlight(t *testing.T) {
	m, user := adoptEnv(t)
	parent := filepath.Join(user, "parent")
	child := filepath.Join(parent, "child")
	writeTree(t, parent, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n", "child/server.jar": vanilla21, "child/eula.txt": "eula=true\n"})
	req := func(dir, name string) AdoptReq {
		return AdoptReq{Dir: dir, Name: name, Core: mcsrc.CoreVanilla, MC: "1.21.1", LaunchTarget: "jar:server.jar"}
	}
	a := req(child, "下层")
	if _, err := m.validateAdopt(&a); err != nil {
		t.Fatal(err)
	}
	for _, r := range []AdoptReq{req(parent, "上层"), req(child, "同一个")} {
		if _, err := m.validateAdopt(&r); err == nil || !strings.Contains(err.Error(), "正在新建或接管中") {
			t.Errorf("%s 应拒绝：%v", r.Name, err)
		}
	}
	// 接管完成（登记进列表）之后，上层照样不行，这回是因为里面放着一台服务器
	m.insts["下层"] = &Instance{Settings: Settings{Name: "下层"}, Dir: child, Console: newConsole()}
	m.releaseCreating("下层")
	m.releaseCreating(dirKey(child))
	b := req(parent, "上层")
	if _, err := m.validateAdopt(&b); err == nil || !strings.Contains(err.Error(), "里面还放着服务器「下层」") {
		t.Errorf("%v", err)
	}
	// 新建到一半的目录（新建流水线在 validateCreate 里占的位）也不能接管
	half := filepath.Join(user, "half")
	writeTree(t, half, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n"})
	m.creating[dirKey(half)] = true
	c := req(half, "半成品")
	if _, err := m.validateAdopt(&c); err == nil || !strings.Contains(err.Error(), "正在新建或接管中") {
		t.Errorf("%v", err)
	}
}

// 新建服务器时目录也占位，任务结束后放掉。
func TestValidateCreateReservesDir(t *testing.T) {
	m, _ := adoptEnv(t)
	req := CreateReq{Name: "新服", Core: mcsrc.CorePaper, MC: "1.21.1", EULA: true}
	dir, err := m.validateCreate(&req)
	if err != nil {
		t.Fatal(err)
	}
	if !m.creating["新服"] || !m.creating[dirKey(dir)] {
		t.Errorf("名字和目录都应占位：%v", m.creating)
	}
	m.releaseCreating("新服")
	m.releaseCreating(dirKey(dir))
	if len(m.creating) != 0 {
		t.Errorf("%v", m.creating)
	}
}

// 移出列表：instance.json 被人改过名字、改坏了也照样改名留底，不然下次启动这个目录又被认成一台服务器。
func TestReleaseRenamesEvenIfEdited(t *testing.T) {
	for _, body := range []string{`{"name":"被人改了名字","core":"paper","mc":"1.21.1"}`, `{ 这不是 JSON`} {
		m, user := adoptEnv(t)
		dir := filepath.Join(user, "srv")
		writeTree(t, dir, map[string]string{"instance.json": body, "world/level.dat": "存档"})
		m.insts["原名"] = &Instance{Settings: Settings{Name: "原名"}, Dir: dir, Console: newConsole()}
		if err := m.Delete("原名", false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "instance.json")); err == nil {
			t.Errorf("instance.json 还在（内容：%s）", body)
		}
		if got := mustRead(t, filepath.Join(dir, settingsBackup)); got != body {
			t.Errorf("留底的内容变了：%q", got)
		}
		if got := mustRead(t, filepath.Join(dir, "world", "level.dat")); got != "存档" {
			t.Error("存档被动了")
		}
	}
}

// shortPath 一个已有路径的 8.3 短名写法；这个盘没开短名时原样返回。
func shortPath(p string) string {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) >= len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}

// 不存在的目录也按真实路径比：上层写成 8.3 短名的，和写长名的算同一个地方。
// GitHub 的测试机临时目录就是短名（RUNNER~1）：列表里一台服务器的目录已经不在了，选它的上层接管时没认出来，
// 合入 main 后的第一次 CI 挂在这里，本地的临时目录是长名所以没发现。
func TestRealPathOfMissingDir(t *testing.T) {
	m, user := adoptEnv(t)
	long := filepath.Join(user, "a rather long folder name")
	writeTree(t, long, map[string]string{"server.jar": vanilla21})
	want := filepath.Join(realPath(long), "missing", "deeper")
	if got := realPath(filepath.Join(long, "missing", "deeper")); !strings.EqualFold(got, want) {
		t.Errorf("不存在的目录：%q，应为 %q", got, want)
	}
	if got := realPath(`Q:\no\such\drive`); got != `Q:\no\such\drive` {
		t.Errorf("一级都打不开时原样返回：%q", got)
	}
	short := shortPath(long)
	if strings.EqualFold(short, long) {
		t.Skip("这个盘没开 8.3 短名，短名那一半测不了")
	}
	if got := realPath(filepath.Join(short, "missing", "deeper")); !strings.EqualFold(got, want) {
		t.Errorf("上层写成短名的不存在的目录：%q，应为 %q", got, want)
	}
	// 列表里一台服务器的目录写的是短名、而且已经不在了：选它的上层，照样认得出里面有它
	m.insts["inner"] = &Instance{Settings: Settings{Name: "inner"}, Dir: filepath.Join(short, "inner"), Console: newConsole()}
	if _, err := m.checkAdoptDir(long); err == nil || !strings.Contains(err.Error(), "里面还放着服务器「inner」") {
		t.Errorf("%v", err)
	}
	if _, err := m.checkAdoptDir(short); err == nil || !strings.Contains(err.Error(), "里面还放着服务器「inner」") {
		t.Errorf("%v", err)
	}
}
