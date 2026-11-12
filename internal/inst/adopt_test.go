package inst

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// adoptEnv 把程序目录换成临时的一份（缓存目录沿用 TestMain 准备的那份：里面有版本清单和一个假的 Java 17），
// 返回空的 Manager 和一个放「用户原来的服务器」的目录（在程序目录外面）。
func adoptEnv(t *testing.T) (*Manager, string) {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir}
	t.Cleanup(func() { app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir = old[0], old[1], old[2], old[3] })
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	setConfig(t, func(c *app.Config) {
		c.ServersDir, c.BackupsDir, c.Roots, c.BackupRoots, c.InstanceDirs = "", "", nil, nil, nil
	})
	return &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}, t.TempDir()
}

// writeTree 在 dir 下按相对路径写一批文件；.jar 结尾、内容以 "zip:" 开头的写成 jar（后面是「包内路径=内容」，用 | 分隔）。
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if spec, ok := strings.CutPrefix(body, "zip:"); ok {
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			for _, ent := range strings.Split(spec, "|") {
				k, v, _ := strings.Cut(ent, "=")
				w, err := zw.Create(k)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte(v))
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			continue
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func manifestOf(mainClass string) string {
	return "META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: " + mainClass + "\r\n\r\n"
}

const (
	neoArgs   = "-p libraries/a.jar;libraries/b.jar --add-modules ALL-MODULE-PATH\n--launchTarget forgeserver --fml.neoForgeVersion 21.1.265 --fml.fmlVersion 4.0.45 --fml.mcVersion 1.21.1 --fml.neoFormVersion 20240808.144430\n"
	forgeArgs = "-p libraries/a.jar --launchTarget forgeserver --fml.forgeVersion %s --fml.mcVersion 1.20.1 --fml.forgeGroup net.minecraftforge --fml.mcpVersion 20230612.114412\n"
	vanilla21 = "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: net.minecraft.bundler.Main\r\nBundler-Format: 1.0\r\n\r\n" +
		"|version.json={\"id\":\"1.21.1\",\"name\":\"1.21.1\",\"java_version\":21}" +
		"|META-INF/versions.list=c301de10\t1.21.1\t1.21.1/server-1.21.1.jar\n"
)

// 各种服务端：认出核心、版本、启动文件、内存。
func TestScanServerDir(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		target string
		core   string
		mc     string
		build  string
		java   int
		xmx    int
		check  func(t *testing.T, sc *AdoptScan)
	}{
		{name: "NeoForge：参数文件、user_jvm_args.txt 里的内存和 JVM 参数、已经同意的 EULA",
			files: map[string]string{
				"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs,
				"user_jvm_args.txt": "# Xmx and Xms set the maximum and minimum RAM usage\r\n-Xmx6G -Xms2G\r\n-XX:+UseG1GC\r\n",
				"run.bat":           "@echo off\r\njava @user_jvm_args.txt @libraries/net/neoforged/neoforge/21.1.265/win_args.txt %*\r\npause\r\n",
				"eula.txt":          "#By changing the setting below to TRUE you are indicating your agreement\neula = TRUE\n",
				"server.properties": "server-port=25570\nlevel-name=world\n", "world/level.dat": "x",
				"neoforge-21.1.265-installer.jar": "不是 zip，也不该被当成服务端",
			},
			target: "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt", core: "neoforge", mc: "1.21.1", build: "21.1.265", xmx: 6144,
			check: func(t *testing.T, sc *AdoptScan) {
				if !slices.Equal(sc.ExtraJVM, []string{"-XX:+UseG1GC"}) || !sc.EULA || sc.Port != 25570 || !sc.HasWorld || sc.Managed || len(sc.Targets) != 1 {
					t.Errorf("%+v", sc)
				}
			}},
		{name: "Forge 装过两个版本：启动脚本点名的那个优先，否则新的优先",
			files: map[string]string{
				"libraries/net/minecraftforge/forge/1.20.1-47.2.17/win_args.txt": strings.Replace(forgeArgs, "%s", "47.2.17", 1),
				"libraries/net/minecraftforge/forge/1.20.1-47.4.20/win_args.txt": strings.Replace(forgeArgs, "%s", "47.4.20", 1),
				"start.bat": "java -Xmx8192M @user_jvm_args.txt @libraries\\net\\minecraftforge\\forge\\1.20.1-47.2.17\\win_args.txt nogui\r\n",
			},
			target: "args:libraries/net/minecraftforge/forge/1.20.1-47.2.17/win_args.txt", core: "forge", mc: "1.20.1", build: "47.2.17", xmx: 8192,
			check: func(t *testing.T, sc *AdoptScan) {
				if len(sc.Targets) != 2 || sc.Targets[1].Build != "47.4.20" || sc.EULA {
					t.Errorf("%+v", sc)
				}
			}},
		{name: "Paper：versions.list 里的版本，启动脚本里的内存",
			files: map[string]string{
				"paper-1.21.1-132.jar": "zip:" + manifestOf("io.papermc.paperclip.Main") + "|META-INF/versions.list=abc\t1.21.1\t1.21.1/paper-1.21.1.jar\n",
				"start.bat":            "@echo off\r\njava -Xms2G -Xmx4G -XX:+UseG1GC -jar paper-1.21.1-132.jar nogui\r\n",
			},
			target: "jar:paper-1.21.1-132.jar", core: "paper", mc: "1.21.1", xmx: 4096,
			check: func(t *testing.T, sc *AdoptScan) {
				if len(sc.ExtraJVM) != 0 || !strings.Contains(strings.Join(sc.Notes, ""), "别的 JVM 参数") {
					t.Errorf("启动脚本里别的 JVM 参数不带过来，但要提醒：%+v", sc)
				}
			}},
		{name: "Purpur：paperclip 启动，versions.list 的文件名是 purpur",
			files:  map[string]string{"server.jar": "zip:" + manifestOf("io.papermc.paperclip.Main") + "|META-INF/versions.list=abc\t1.20.4\t1.20.4/purpur-1.20.4.jar\n"},
			target: "jar:server.jar", core: "purpur", mc: "1.20.4"},
		{name: "老 Paper（没有 versions.list）：看 patch.properties 和文件名",
			files:  map[string]string{"paper-1.16.5-794.jar": "zip:" + manifestOf("io.papermc.paperclip.Paperclip") + "|patch.properties=originalHash=aa\nversion=1.16.5\n"},
			target: "jar:paper-1.16.5-794.jar", core: "paper", mc: "1.16.5"},
		{name: "Paper 的分支按 Paper 管，并说明",
			files:  map[string]string{"pufferfish.jar": "zip:" + manifestOf("io.papermc.paperclip.Main") + "|META-INF/versions.list=abc\t1.20.4\t1.20.4/pufferfish-1.20.4.jar\n"},
			target: "jar:pufferfish.jar", core: "paper", mc: "1.20.4",
			check: func(t *testing.T, sc *AdoptScan) {
				if !strings.Contains(strings.Join(sc.Notes, ""), "Paper 的分支") {
					t.Errorf("%v", sc.Notes)
				}
			}},
		{name: "Fabric 启动器旁边放着原版 server.jar：启动的是 Fabric",
			files: map[string]string{
				"fabric-server-mc.1.21.1-loader.0.16.5-launcher.1.0.1.jar": "zip:" + manifestOf("net.fabricmc.installer.ServerLauncher") + "|install.properties=fabric-loader-version=0.16.5\ngame-version=1.21.1\n",
				"server.jar": vanilla21,
			},
			target: "jar:fabric-server-mc.1.21.1-loader.0.16.5-launcher.1.0.1.jar", core: "fabric", mc: "1.21.1", build: "0.16.5", java: 21,
			check: func(t *testing.T, sc *AdoptScan) {
				if len(sc.Targets) != 2 || sc.Targets[1].Core != "vanilla" {
					t.Errorf("%+v", sc.Targets)
				}
			}},
		{name: "安装器生成的 fabric-server-launch.jar 自己不带版本：从旁边的原版 jar 取",
			files: map[string]string{
				"fabric-server-launch.jar": "zip:" + manifestOf("net.fabricmc.loader.impl.launch.server.FabricServerLauncher"),
				"server.jar":               vanilla21,
			},
			target: "jar:fabric-server-launch.jar", core: "fabric", mc: "1.21.1", java: 21},
		{name: "原版", files: map[string]string{"server.jar": vanilla21}, target: "jar:server.jar", core: "vanilla", mc: "1.21.1", java: 21,
			check: func(t *testing.T, sc *AdoptScan) {
				if sc.HasWorld {
					t.Error("没有存档目录")
				}
			}},
		{name: "还没有 server.properties、但已经有 world 目录：算有存档；改了 level-name 的看那个目录",
			files:  map[string]string{"server.jar": vanilla21, "world/level.dat": "x"},
			target: "jar:server.jar", core: "vanilla", mc: "1.21.1", java: 21,
			check: func(t *testing.T, sc *AdoptScan) {
				if !sc.HasWorld {
					t.Error("有 world 目录应算有存档")
				}
			}},
		{name: "level-name 指到别的目录、那个目录不在：没有存档",
			files:  map[string]string{"server.jar": vanilla21, "world/level.dat": "x", "server.properties": "level-name=survival\n"},
			target: "jar:server.jar", core: "vanilla", mc: "1.21.1", java: 21,
			check: func(t *testing.T, sc *AdoptScan) {
				if sc.HasWorld {
					t.Error("level-name 指的目录不在")
				}
			}},
		{name: "老 Forge：forge-<mc>-<forge>.jar 旁边是 minecraft_server 的 jar",
			files: map[string]string{
				"forge-1.12.2-14.23.5.2860.jar": "zip:" + manifestOf("net.minecraftforge.fml.relauncher.ServerLaunchWrapper"),
				"minecraft_server.1.12.2.jar":   "zip:" + manifestOf("net.minecraft.server.MinecraftServer"),
			},
			target: "jar:forge-1.12.2-14.23.5.2860.jar", core: "forge", mc: "1.12.2", build: "14.23.5.2860"},
		{name: "Velocity：版本在清单的 Implementation-Version 里",
			files:  map[string]string{"velocity.jar": "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: com.velocitypowered.proxy.Velocity\r\nImplementation-Version: 3.4.0-SNAPSHOT (git-1234abcd-b449)\r\n\r\n"},
			target: "jar:velocity.jar", core: "velocity", mc: "3.4.0-SNAPSHOT"},
		{name: "Spigot 按 Paper 管，并说明",
			files:  map[string]string{"spigot-1.20.4.jar": "zip:" + manifestOf("org.bukkit.craftbukkit.bootstrap.Main") + "|META-INF/versions.list=abc\t1.20.4\t1.20.4/spigot-1.20.4.jar\n"},
			target: "jar:spigot-1.20.4.jar", core: "paper", mc: "1.20.4",
			check: func(t *testing.T, sc *AdoptScan) {
				if !strings.Contains(strings.Join(sc.Notes, ""), "Spigot") {
					t.Errorf("%v", sc.Notes)
				}
			}},
		{name: "认不出的服务端：核心留空让用户选，版本从日志里找",
			files: map[string]string{
				"arclight.jar":    "zip:" + manifestOf("io.izzel.arclight.server.Launcher"),
				"logs/latest.log": "[12:00:00] [main/INFO]: Loading\n[12:00:03] [Server thread/INFO]: Starting minecraft server version 1.20.1\n",
			},
			target: "jar:arclight.jar", core: "", mc: "1.20.1",
			check: func(t *testing.T, sc *AdoptScan) {
				if !strings.Contains(strings.Join(sc.Notes, ""), "没认出这是哪种服务端") {
					t.Errorf("%v", sc.Notes)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "My Server")
			writeTree(t, dir, c.files)
			sc, err := scanServerDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if sc.LaunchTarget != c.target || sc.Core != c.core || sc.MC != c.mc || sc.Build != c.build || sc.JavaMajor != c.java || sc.XmxMB != c.xmx {
				t.Errorf("认成了 target=%q core=%q mc=%q build=%q java=%d xmx=%d", sc.LaunchTarget, sc.Core, sc.MC, sc.Build, sc.JavaMajor, sc.XmxMB)
			}
			if sc.Name != "My Server" {
				t.Errorf("建议的名称应是文件夹名：%q", sc.Name)
			}
			if c.check != nil {
				c.check(t, sc)
			}
		})
	}
}

// 没有服务端的文件夹报错；选到了上一层时点出下面哪个像服务器。
func TestScanNoServer(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"notes.txt": "x", "生存服/server.properties": "motd=x\n", "生存服/server.jar": vanilla21, "照片/a.png": "x",
		"代理/velocity.jar": "只放了一个 jar、还没启动过的也算"})
	_, err := scanServerDir(dir)
	if err == nil || !strings.Contains(err.Error(), "「生存服」") || !strings.Contains(err.Error(), "「代理」") || strings.Contains(err.Error(), "照片") {
		t.Errorf("应点出下面的「生存服」「代理」：%v", err)
	}
	if _, err := scanServerDir(filepath.Join(dir, "照片")); err == nil || !strings.Contains(err.Error(), "没找到服务端") {
		t.Errorf("%v", err)
	}
}

// 哪些文件夹不许接管：整个磁盘、MineDeck 自己的目录、存放服务器的总目录、已经在列表里的和它里外的。
func TestCheckAdoptDir(t *testing.T) {
	m, user := adoptEnv(t)
	mine := filepath.Join(app.ServersDir, "mine")
	writeTree(t, mine, map[string]string{"server.jar": vanilla21, "world/level.dat": "x"})
	m.insts["mine"] = &Instance{Settings: Settings{Name: "mine"}, Dir: mine, Console: newConsole()}
	orphan := filepath.Join(app.ServersDir, "orphan") // 自己拷进 servers 目录、但没有 instance.json 的：可以接管
	writeTree(t, orphan, map[string]string{"server.jar": vanilla21})
	writeTree(t, app.CacheDir, map[string]string{"vanilla/1.21.1/server.jar": vanilla21})
	writeTree(t, user, map[string]string{"server.jar": vanilla21})

	for dir, want := range map[string]string{
		"":                              "请先选择",
		"relative\\dir":                 "完整的路径",
		filepath.Join(user, "nope"):     "找不到这个文件夹",
		filepath.VolumeName(user) + `\`: "整个磁盘",
		app.Base:                        "程序目录",
		filepath.Dir(app.Base):          "程序目录",
		app.ServersDir:                  "总目录",
		filepath.Join(app.CacheDir, "vanilla", "1.21.1"): "MineDeck 自己用的",
		mine:                         "已经在列表里了，就是「mine」",
		strings.ToUpper(mine):        "已经在列表里了",
		filepath.Join(mine, "world"): "在服务器「mine」的目录里面",
	} {
		if _, err := m.checkAdoptDir(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q 应拒绝（%s）：%v", dir, want, err)
		}
	}
	for _, dir := range []string{user, orphan, user + `\`} {
		if got, err := m.checkAdoptDir(dir); err != nil || got != filepath.Clean(dir) {
			t.Errorf("%q 应可以接管：%q %v", dir, got, err)
		}
	}
	// 文件夹里面还放着一台已经在列表里的服务器
	nested := filepath.Join(user, "inner")
	m.insts["inner"] = &Instance{Settings: Settings{Name: "inner"}, Dir: nested, Console: newConsole()}
	if _, err := m.checkAdoptDir(user); err == nil || !strings.Contains(err.Error(), "里面还放着服务器「inner」") {
		t.Errorf("%v", err)
	}
}

// offline 一个已经取消的 context：下载一律立刻失败，版本清单退回磁盘缓存——测试不联网。
func offline() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// adopt 校验并接管（不联网），返回任务的日志。
func adopt(t *testing.T, m *Manager, req AdoptReq) (string, error) {
	t.Helper()
	sc, err := m.validateAdopt(&req)
	if err != nil {
		return "", err
	}
	defer m.releaseCreating(req.Name)
	defer m.releaseCreating(sc.key)
	task, _ := m.Tasks.New("接管", adoptSteps)
	err = m.runAdopt(offline(), task, req, sc)
	return strings.Join(task.Snapshot().Log, "\n"), err
}

func fakeJava(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "jdk", "bin", "java.exe")
	writeTree(t, filepath.Dir(p), map[string]string{"java.exe": ""})
	return p
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 接管一台 NeoForge 服务器：只写 MineDeck 自己的文件，原来的启动脚本和 user_jvm_args.txt 另存，存档和配置原样不动。
func TestAdoptNeoForge(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "create-pack")
	const userBat = "@echo off\r\njava @user_jvm_args.txt @libraries/net/neoforged/neoforge/21.1.265/win_args.txt %*\r\npause\r\n"
	const userArgs = "-Xmx6G\r\n-XX:+UseG1GC\r\n-Dfoo=bar\r\n"
	untouched := map[string]string{
		"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs,
		"eula.txt":                  "eula=true\n",
		"server.properties":         "server-port=25570\nmotd=old server\n",
		"world/level.dat":           "存档",
		"mods/create.jar":           "模组",
		"config/create-common.toml": "x = 1\n",
		"ops.json":                  "[]",
	}
	writeTree(t, dir, untouched)
	writeTree(t, dir, map[string]string{"run.bat": userBat, "user_jvm_args.txt": userArgs})
	// 同一层还有一台 MineDeck 管过的服务器（带 instance.json），这次没有接管它
	writeTree(t, filepath.Join(user, "neighbor"), map[string]string{
		"instance.json": `{"name":"邻居","core":"vanilla","mc":"1.21.1","launchTarget":"jar:server.jar"}`,
	})
	java := fakeJava(t)

	sc, err := m.ScanAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	log, err := adopt(t, m, AdoptReq{Dir: dir, Name: sc.Name, Core: mcsrc.Core(sc.Core), MC: sc.MC, Build: sc.Build,
		LaunchTarget: sc.LaunchTarget, XmxMB: sc.XmxMB, JavaMajor: 21, JavaPath: java})
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}

	i, err := m.Get("create-pack")
	if err != nil {
		t.Fatal(err)
	}
	if i.Dir != dir || !i.Adopted || i.Core != mcsrc.CoreNeoForge || i.MC != "1.21.1" || i.Build != "21.1.265" || i.JavaMajor != 21 || i.JavaPath != java ||
		i.XmxMB != 6144 || i.XmsMB != 1024 || i.LaunchTarget != "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt" ||
		!slices.Equal(i.ExtraJVM, []string{"-XX:+UseG1GC", "-Dfoo=bar"}) || i.Port() != 25570 {
		t.Errorf("登记的设置不对：%+v", i.Settings)
	}
	for name, body := range untouched {
		if got := mustRead(t, filepath.Join(dir, filepath.FromSlash(name))); got != body {
			t.Errorf("%s 被改了：%q", name, got)
		}
	}
	if got := mustRead(t, filepath.Join(dir, runBatBackup)); got != userBat {
		t.Errorf("原来的 run.bat 没原样另存：%q", got)
	}
	if got := mustRead(t, filepath.Join(dir, jvmArgsBackup)); got != userArgs {
		t.Errorf("原来的 user_jvm_args.txt 没原样另存：%q", got)
	}
	bat := mustRead(t, filepath.Join(dir, "run.bat"))
	if !isOurRunBat([]byte(bat)) || !strings.Contains(bat, java) || !strings.Contains(bat, `"-Xmx6144M"`) || !strings.Contains(bat, `"-Dfoo=bar"`) ||
		!strings.Contains(bat, `"@libraries\net\neoforged\neoforge\21.1.265\win_args.txt"`) {
		t.Errorf("生成的 run.bat：%s", bat)
	}
	if s, err := readSettings(dir); err != nil || s.Name != "create-pack" || !s.Adopted {
		t.Errorf("instance.json：%+v %v", s, err)
	}
	// 只单独登记被接管的这一个文件夹，不把它的上一层记成扫描根
	if c := app.GetConfig(); !slices.Equal(c.InstanceDirs, []string{dir}) || len(c.Roots) != 0 {
		t.Errorf("应只登记被接管的文件夹：instanceDirs=%v roots=%v", c.InstanceDirs, c.Roots)
	}

	// 重启后（重新扫描）还在；旁边那台没接管过的不会跟着出现；同一个文件夹不能再接管一次
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i2, err := m2.Get("create-pack"); err != nil || i2.Dir != dir || !i2.Adopted {
		t.Errorf("重新扫描后没找到：%v", err)
	}
	if _, err := m2.Get("邻居"); err == nil || len(m2.List()) != 1 {
		t.Errorf("旁边那台没接管过的服务器，重启后自己出现在了列表里（现在 %d 台）", len(m2.List()))
	}
	if _, err := m.ScanAdopt(dir); err == nil || !strings.Contains(err.Error(), "已经在列表里了") {
		t.Errorf("%v", err)
	}

	// 再接管一次别的名字也不行；备份只留最早那份（用户的原件），不会被 MineDeck 生成的覆盖
	if _, err := backupOnce(dir, "run.bat", runBatBackup); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, runBatBackup)); got != userBat {
		t.Errorf("备份被覆盖了：%q", got)
	}
}

// 没指定 Java：官方元数据拿不到（离线）时用服务端自己写明的版本；本机有这个版本就直接用。老版本带上 log4shell 缓解。
func TestAdoptPicksJava(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "vanilla")
	writeTree(t, dir, map[string]string{
		"server.jar": "zip:" + manifestOf("net.minecraft.bundler.Main") + "|version.json={\"id\":\"1.20.1\",\"java_version\":17}",
	})
	req := AdoptReq{Dir: dir, Name: "原版", Core: mcsrc.CoreVanilla, MC: "1.20.1", LaunchTarget: "jar:server.jar"}
	if _, err := adopt(t, m, req); err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Fatalf("没同意 EULA 应拒绝：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); err == nil {
		t.Fatal("被拒绝了还写了 instance.json")
	}
	req.EULA = true
	log, err := adopt(t, m, req)
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	i, _ := m.Get("原版")
	if i.JavaMajor != 17 || i.JavaPath != testJava17 || i.XmxMB != 2048 || !strings.Contains(log, "服务端自己写明需要 Java 17") {
		t.Errorf("Java %d %s xmx=%d\n%s", i.JavaMajor, i.JavaPath, i.XmxMB, log)
	}
	if got := mustRead(t, filepath.Join(dir, "eula.txt")); !strings.Contains(got, "eula=true") {
		t.Errorf("eula.txt：%q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, runBatBackup)); err == nil {
		t.Error("原来没有 run.bat，不该有备份")
	}

	// 版本清单里没有、jar 里也没写：说明要手动选 Java，什么都不写
	odd := filepath.Join(user, "odd")
	writeTree(t, odd, map[string]string{"custom.jar": "zip:" + manifestOf("com.example.Main"), "eula.txt": "eula=true\n"})
	_, err = adopt(t, m, AdoptReq{Dir: odd, Name: "奇怪的", Core: mcsrc.CorePaper, MC: "9.9.9", LaunchTarget: "jar:custom.jar"})
	if err == nil || !strings.Contains(err.Error(), "手动选一个版本") {
		t.Errorf("%v", err)
	}
	if ents, _ := os.ReadDir(odd); len(ents) != 2 {
		t.Errorf("接管失败不该留下任何文件：%v", ents)
	}
	if _, err := m.Get("奇怪的"); err == nil {
		t.Error("接管失败不该登记")
	}
	// 手动选了 Java 就行；清单里没有的版本不加 log4shell 缓解（不知道它是新是旧）
	java := fakeJava(t)
	if log, err := adopt(t, m, AdoptReq{Dir: odd, Name: "奇怪的", Core: mcsrc.CorePaper, MC: "9.9.9", LaunchTarget: "jar:custom.jar", JavaMajor: 21, JavaPath: java}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if i, _ := m.Get("奇怪的"); len(i.ExtraJVM) != 0 {
		t.Errorf("%v", i.ExtraJVM)
	}

	// 1.16.5：和新建时一样带上 log4shell 缓解参数
	old := filepath.Join(user, "old")
	writeTree(t, old, map[string]string{"paper-1.16.5-794.jar": "zip:" + manifestOf("io.papermc.paperclip.Paperclip"), "eula.txt": "eula=true\n"})
	if log, err := adopt(t, m, AdoptReq{Dir: old, Name: "老服", Core: mcsrc.CorePaper, MC: "1.16.5", LaunchTarget: "jar:paper-1.16.5-794.jar", JavaMajor: 8, JavaPath: java}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if i, _ := m.Get("老服"); !slices.Contains(i.ExtraJVM, log4jNoLookups) {
		t.Errorf("老版本应带 log4shell 缓解：%v", i.ExtraJVM)
	}
}

// 请求里的东西不能信：启动文件必须是识别出来的候选，名字不能重，同一个文件夹不能同时接管两次。
func TestValidateAdopt(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "srv")
	writeTree(t, dir, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n", "other/evil.jar": vanilla21})
	m.insts["已有的"] = &Instance{Settings: Settings{Name: "已有的"}, Dir: filepath.Join(user, "x"), Console: newConsole()}
	ok := AdoptReq{Dir: dir, Name: "新的", Core: mcsrc.CoreVanilla, MC: "1.21.1", LaunchTarget: "jar:server.jar"}
	for want, mutate := range map[string]func(*AdoptReq){
		"已有同名的服务器":   func(r *AdoptReq) { r.Name = "已有的" },
		"服务器名称不能包含":  func(r *AdoptReq) { r.Name = "a:b" },
		"服务端的类型":     func(r *AdoptReq) { r.Core = "arclight" },
		"版本号":        func(r *AdoptReq) { r.MC = " " },
		"启动文件不在":     func(r *AdoptReq) { r.LaunchTarget = "jar:other/evil.jar" },
		"重新识别":       func(r *AdoptReq) { r.LaunchTarget = `jar:..\x\server.jar` },
		"8 到 30 之间":  func(r *AdoptReq) { r.JavaMajor = 99 },
		"还要写明它是哪个版本": func(r *AdoptReq) { r.JavaPath = `C:\java.exe` },
		"Mohist":     func(r *AdoptReq) { r.Core = mcsrc.CoreMohist; r.Dir = filepath.Join(user, "中文") },
	} {
		req := ok
		mutate(&req)
		if req.Core == mcsrc.CoreMohist {
			writeTree(t, req.Dir, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n"})
		}
		if _, err := m.validateAdopt(&req); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("应拒绝（%s）：%v", want, err)
		}
	}
	if len(m.creating) != 0 {
		t.Errorf("被拒绝的请求不该留下占位：%v", m.creating)
	}
	req := ok
	if _, err := m.validateAdopt(&req); err != nil {
		t.Fatal(err)
	}
	again := ok
	again.Name = "另一个名字"
	if _, err := m.validateAdopt(&again); err == nil || !strings.Contains(err.Error(), "正在新建或接管中") {
		t.Errorf("同一个文件夹同时接管两次应拒绝：%v", err)
	}
}

// 移出列表（不删文件）之后不会在下次启动时又冒出来；再接管回来时名字、定时任务、JVM 参数都还在。
func TestReleaseThenAdoptBack(t *testing.T) {
	m, user := adoptEnv(t)
	dir := filepath.Join(user, "srv")
	writeTree(t, dir, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n", "world/level.dat": "存档"})
	java := fakeJava(t)
	if log, err := adopt(t, m, AdoptReq{Dir: dir, Name: "朋友服", Core: mcsrc.CoreVanilla, MC: "1.21.1", LaunchTarget: "jar:server.jar", XmxMB: 3072, JavaMajor: 21, JavaPath: java}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	pol := Policies{CrashRestart: true, Schedules: []Schedule{{Type: "restart", At: "04:00"}}}
	if err := m.UpdatePolicies("朋友服", pol); err != nil {
		t.Fatal(err)
	}
	jvm := []string{"-XX:+UseZGC"}
	if err := m.UpdateSettings("朋友服", 0, 0, "gbk", &jvm); err != nil {
		t.Fatal(err)
	}

	if err := m.Delete("朋友服", false); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, "world", "level.dat")); got != "存档" {
		t.Error("移出列表动了存档")
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); err == nil {
		t.Error("instance.json 还在：下次启动又会被扫出来")
	}
	if m2, _ := NewManager(); len(m2.List()) != 0 {
		t.Errorf("移出列表后重新扫描又出现了：%d 台", len(m2.List()))
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("移出列表后单独登记还留着：%v", got)
	}

	sc, err := m.ScanAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Managed || sc.Name != "朋友服" || sc.XmxMB != 3072 || sc.Core != "vanilla" || !slices.Equal(sc.ExtraJVM, jvm) {
		t.Errorf("没从留底的设置里带回来：%+v", sc)
	}
	if log, err := adopt(t, m, AdoptReq{Dir: dir, Name: sc.Name, Core: mcsrc.Core(sc.Core), MC: sc.MC, LaunchTarget: sc.LaunchTarget, XmxMB: sc.XmxMB, JavaMajor: 21, JavaPath: java}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	i, _ := m.Get("朋友服")
	if got := i.PoliciesSnapshot(); !got.CrashRestart || len(got.Schedules) != 1 || got.Schedules[0].At != "04:00" {
		t.Errorf("定时任务没带回来：%+v", got)
	}
	if i.ConsoleEncoding != "gbk" || !slices.Equal(i.ExtraJVM, jvm) {
		t.Errorf("控制台编码、JVM 参数没带回来：%q %v", i.ConsoleEncoding, i.ExtraJVM)
	}
	if _, err := os.Stat(filepath.Join(dir, settingsBackup)); err == nil {
		t.Error("接管回来后留底的 .bak 应该清掉")
	}
	if _, err := os.Stat(filepath.Join(dir, runBatBackup)); err == nil {
		t.Error("MineDeck 自己生成的 run.bat 不用备份")
	}
	if got := app.InstanceDirs(); !slices.Equal(got, []string{dir}) {
		t.Errorf("接管回来后应重新登记这个文件夹：%v", got)
	}

	// 连文件一起删照旧，登记也去掉
	if err := m.Delete("朋友服", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("目录还在")
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("删掉以后单独登记还留着：%v", got)
	}
}
