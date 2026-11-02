package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/modinfo"
)

func load(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// input 按样本文件名（<规则>@<核心>-….log）组一份输入；本机 16 GB、可用 9 GB，最大内存按启动参数取。
func input(t *testing.T, path string) Input {
	t.Helper()
	lines := load(t, path)
	base := filepath.Base(path)
	core := base
	if _, after, ok := strings.Cut(base, "@"); ok {
		core = after
	}
	core = strings.SplitN(strings.TrimSuffix(core, ".log"), "-", 2)[0]
	in := Input{Lines: lines, Core: core, RAMMB: 16384, AvailMB: 9216, XmxMB: 6144, Port: 25565}
	if strings.Contains(lines[0], "-Xmx4096M") {
		in.XmxMB = 4096
	}
	if strings.Contains(lines[0], "-Xmx16384M") {
		in.XmxMB = 16384
	}
	return in
}

func rulesOf(r Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Rule)
	}
	return out
}

func find(r Report, rule string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Rule == rule {
			return &r.Findings[i]
		}
	}
	return nil
}

// 13 类样本全部认出来，而且只认出这一类（不串到别的规则）；设计文档 §5.3 的 13 类都有样本。
func TestPositiveSamples(t *testing.T) {
	files, _ := filepath.Glob("testdata/positive/*.log")
	if len(files) < 13 {
		t.Fatalf("样本太少：%d", len(files))
	}
	covered := map[string]bool{}
	for _, f := range files {
		want, _, _ := strings.Cut(filepath.Base(f), "@")
		rep := Analyze(input(t, f))
		got := rulesOf(rep)
		if !slices.Equal(got, []string{want}) {
			t.Errorf("%s：认出 %v，应只有 %s", filepath.Base(f), got, want)
			continue
		}
		fd := rep.Findings[0]
		if fd.Title == "" || fd.Summary == "" || len(fd.Fixes) == 0 || len(fd.Evidence) == 0 || (fd.Severity != "error" && fd.Severity != "warn") {
			t.Errorf("%s：结论不全：%+v", filepath.Base(f), fd)
		}
		covered[want] = true
	}
	for _, r := range []string{"missing-deps", "client-only", "wrong-loader", "duplicate-mods", "incompatible-mods", "mixin",
		"java-version", "jvm-option", "port-in-use", "out-of-memory", "watchdog", "eula", "client-mismatch", "broken-config"} {
		if !covered[r] {
			t.Errorf("规则 %s 没有样本", r)
		}
	}
}

// 正常启动的日志不误报：常见的 WARN（refmap、minVersion、配置自动修正、Can't keep up、离线模式）、
// 聊天里打出报错原文、/say 广播、插件运行时的普通异常、用户自己停服，都不算。
func TestNegativeSamples(t *testing.T) {
	files, _ := filepath.Glob("testdata/negative/*.log")
	if len(files) < 5 {
		t.Fatalf("反例太少：%d", len(files))
	}
	for _, f := range files {
		rep := Analyze(input(t, f))
		if len(rep.Findings) != 0 || rep.Crashed {
			t.Errorf("%s 误报：%v crashed=%v %+v", filepath.Base(f), rulesOf(rep), rep.Crashed, rep.Findings)
		}
	}
}

// 各规则取出的细节。
func TestFindingDetails(t *testing.T) {
	get := func(name, rule string, mods ...modinfo.Mod) *Finding {
		t.Helper()
		in := input(t, "testdata/positive/"+name)
		in.Mods = mods
		f := find(Analyze(in), rule)
		if f == nil {
			t.Fatalf("%s 没认出 %s", name, rule)
		}
		return f
	}
	f := get("missing-deps@forge-1.20.1.log", "missing-deps", modinfo.Mod{File: "mowziesmobs-1.6.4.jar", ID: "mowziesmobs"})
	if len(f.Details) != 2 || !strings.Contains(f.Details[0], "mowziesmobs 需要 geckolib（版本 [4.2,)），但没有装") ||
		!strings.Contains(f.Details[1], "装的是 0.5.0.i，版本不对") || !slices.Contains(f.Files, "mowziesmobs-1.6.4.jar") || !slices.Contains(f.Mods, "geckolib") {
		t.Errorf("缺少前置（Forge）：%+v", f)
	}
	f = get("missing-deps@neoforge-1.21.1.log", "missing-deps")
	if len(f.Details) != 2 || !strings.Contains(f.Details[1], "farmersdelight 需要 neoforge（版本 21.1.90 or above），装的是 21.1.77") {
		t.Errorf("缺少前置（NeoForge，ModSorter 和汇总里的同一条只算一次）：%+v", f.Details)
	}
	f = get("missing-deps@fabric-1.21.1.log", "missing-deps")
	if len(f.Details) != 2 || !strings.Contains(f.Details[0], "sodium-extra 需要 fabric-api（版本 任意版本）") || !strings.Contains(f.Details[1], "create 需要 flywheel（版本 0.6.10 or later）") {
		t.Errorf("缺少前置（Fabric）：%+v", f.Details)
	}
	// Connector 转过来的 Fabric 模组：版本范围是空的，前置装了但加载器说不满足（测试报告 E-06，S2 实测日志）
	f = get("missing-deps@forge-1.20.1-connector.log", "missing-deps")
	if len(f.Details) != 2 || !strings.Contains(f.Details[0], "continuity 需要 fabric_api，装的是 0.92.6+1.11.14+1.20.1，加载器认为版本不对") ||
		!strings.Contains(f.Details[1], "continuity 需要 connectormod，装的是 1.0.0-beta.48+1.20.1") {
		t.Errorf("缺少前置（版本范围空着）：%+v", f.Details)
	}
	f = get("client-only@forge-1.20.1.log", "client-only")
	if !slices.Equal(f.Files, []string{"Xaeros_Minimap_24.5.0_Forge_1.20.jar"}) || !slices.Contains(f.Mods, "xaerominimap") || !strings.Contains(f.Summary, "net.minecraft.client.gui.screens.Screen") {
		t.Errorf("客户端模组（堆栈里带 jar 名）：%+v", f)
	}
	f = get("client-only@neoforge-1.21.1.log", "client-only")
	if !slices.Contains(f.Files, "journeymap-neoforge-1.21.1-6.0.0-beta.33.jar") || !slices.Contains(f.Mods, "journeymap") {
		t.Errorf("客户端模组（NeoForge）：%+v", f)
	}
	f = get("wrong-loader@neoforge-1.21.1.log", "wrong-loader")
	if len(f.Files) != 3 || !strings.Contains(strings.Join(f.Details, "|"), "EssentialsX-2.21.0.jar 是插件") {
		t.Errorf("装错加载器：%+v", f)
	}
	if f = get("wrong-loader@paper-1.21.4.log", "wrong-loader"); !slices.Equal(f.Files, []string{"create-1.21.1-6.0.4.jar"}) {
		t.Errorf("插件服里放了模组：%+v", f)
	}
	f = get("duplicate-mods@forge-1.20.1.log", "duplicate-mods")
	if !slices.Equal(f.Mods, []string{"jei"}) || !slices.Equal(f.Files, []string{"jei-1.20.1-forge-15.2.0.27.jar", "jei-1.20.1-forge-15.3.0.4.jar"}) {
		t.Errorf("重复模组：%+v", f)
	}
	f = get("incompatible-mods@neoforge-1.21.1.log", "incompatible-mods")
	if !slices.Equal(f.Mods, []string{"embeddium", "optifine"}) {
		t.Errorf("不兼容：%+v", f)
	}
	// mixin：有模组清单时按清单把配置对到 jar；没有时按配置名猜
	f = get("mixin@forge-1.20.1.log", "mixin", modinfo.Mod{File: "create-1.20.1-0.5.1.f.jar", ID: "create", Mixins: []string{"create.mixins.json"}})
	if !slices.Equal(f.Mods, []string{"create"}) || !slices.Equal(f.Files, []string{"create-1.20.1-0.5.1.f.jar"}) {
		t.Errorf("mixin：%+v", f)
	}
	if f = get("mixin@neoforge-1.21.1.log", "mixin"); !slices.Equal(f.Mods, []string{"sodiumextra"}) {
		t.Errorf("mixin（按配置名猜）：%+v", f)
	}
	if f = get("java-version@neoforge-1.21.1.log", "java-version"); !strings.Contains(f.Summary, "要 Java 21 或更新，现在用的是 Java 17") || !strings.Contains(f.Fixes[0], "Java 21") {
		t.Errorf("Java 版本：%+v", f)
	}
	if f = get("java-version@forge-1.16.5.log", "java-version"); !strings.Contains(f.Fixes[0], "Java 8") {
		t.Errorf("老 Forge：%+v", f)
	}
	if f = get("java-version@neoforge-feature.log", "java-version"); !slices.Equal(f.Mods, []string{"examplemod"}) || !strings.Contains(f.Fixes[0], "javaVersion 21") {
		t.Errorf("NeoForge 声明了 Java 要求：%+v", f)
	}
	if f = get("jvm-option@paper-1.21.4.log", "jvm-option"); !strings.Contains(f.Summary, "ZGenerational") {
		t.Errorf("JVM 参数：%+v", f)
	}
	if f = get("port-in-use@forge-1.20.1.log", "port-in-use"); !strings.Contains(f.Summary, "端口 25566") {
		t.Errorf("端口（取自 Starting Minecraft server on）：%+v", f)
	}
	if f = get("out-of-memory@forge-runtime.log", "out-of-memory"); !strings.Contains(f.Fixes[0], "调到 9216 MB") {
		t.Errorf("运行中内存不够（6 GB → 9 GB，本机 16 GB）：%+v", f.Fixes)
	}
	if f = get("out-of-memory@vanilla-startup.log", "out-of-memory"); !strings.Contains(f.Fixes[0], "降到 8192 MB") {
		t.Errorf("一启动就内存不够（可用 9 GB）：%+v", f.Fixes)
	}
	if f = get("watchdog@forge-1.20.1.log", "watchdog"); !strings.Contains(f.Summary, "60.00 秒") || !strings.Contains(strings.Join(f.Fixes, ""), "max-tick-time") {
		t.Errorf("看门狗：%+v", f)
	}
	if f = get("eula@vanilla-1.21.1.log", "eula"); !strings.Contains(strings.Join(f.Fixes, ""), "AI 不能替你同意") {
		t.Errorf("EULA：%+v", f)
	}
	f = get("client-mismatch@neoforge-1.21.1.log", "client-mismatch")
	if f.Severity != "warn" || !strings.Contains(f.Summary, "Steve、Bob") || !slices.Equal(f.Mods, []string{"create", "voicechat"}) {
		t.Errorf("好友进不来：%+v", f)
	}
	if f = get("broken-config@forge-1.20.1.log", "broken-config"); !slices.Equal(f.Details, []string{"create-common.toml"}) || !slices.Equal(f.Mods, []string{"create"}) {
		t.Errorf("配置坏了（Forge）：%+v", f)
	}
	if f = get("broken-config@neoforge-1.21.1.log", "broken-config"); !slices.Equal(f.Details, []string{"config/farmersdelight-common.toml"}) {
		t.Errorf("配置坏了（NeoForge）：%+v", f)
	}
}

// 退出状态：用户停服、强杀、在控制台打 stop 都不算崩溃；自己退出、规则又没认出原因时给一条「异常退出」。
// 只看最近一次启动之后的输出。
func TestExitState(t *testing.T) {
	start := StartMark + " java -jar server.jar nogui"
	cases := []struct {
		lines   []string
		crashed bool
	}{
		{[]string{start, "[12:00:00] [Server thread/INFO]: Done (1.0s)!"}, false},
		{[]string{start, "[MineDeck] 正在保存世界并停止服务器...", "[MineDeck] 服务器已正常关闭"}, false},
		{[]string{start, "[MineDeck] 强制结束进程", "[MineDeck] 服务器进程已退出（exit status 1）"}, false},
		{[]string{start, "> stop", "[MineDeck] 服务器已正常关闭"}, false},
		{[]string{start, "> /STOP", "[MineDeck] 服务器已正常关闭"}, false},
		{[]string{start, "[12:00:00] [Server thread/INFO]: [Steve: Stopping the server]", "[MineDeck] 服务器正在关闭（有人执行了 stop）", "[MineDeck] 服务器已正常关闭"}, false},
		{[]string{start, "> stopsound @a", "[MineDeck] 服务器进程已退出（退出码 0，但没人让它停）"}, true},
		{[]string{start, "[MineDeck] 服务器进程已退出（exit status -1）"}, true},
		{[]string{start, "[MineDeck] 服务器进程已退出（exit status 1）", start, "[12:00:00] [Server thread/INFO]: Done (1.0s)!"}, false},
	}
	for i, c := range cases {
		rep := Analyze(Input{Lines: c.lines})
		if rep.Crashed != c.crashed {
			t.Errorf("第 %d 个：crashed=%v", i, rep.Crashed)
		}
		if c.crashed && (len(rep.Findings) != 1 || rep.Findings[0].Rule != "crash" || rep.Findings[0].Evidence[0] != rep.Exit) {
			t.Errorf("第 %d 个：异常退出应给一条通用结论：%+v", i, rep)
		}
	}
	if got := SinceStart([]string{"a", start, "b", start, "c"}); len(got) != 2 || got[1] != "c" {
		t.Errorf("SinceStart：%v", got)
	}
}

// 给 AI 的控制台精简：WARN / ERROR、异常块（最多 15 行 at）、最后几行；重复的行合并成 ×N。
func TestCondense(t *testing.T) {
	var lines []string
	lines = append(lines, StartMark+" java -jar server.jar nogui")
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("[12:00:%02d] [Server thread/INFO]: 普通信息 %d", i%60, i))
	}
	for i := 0; i < 37; i++ {
		lines = append(lines, fmt.Sprintf("[12:01:%02d] [Server thread/WARN]: Can't keep up!", i%60))
	}
	lines = append(lines, "[12:02:00] [Server thread/ERROR]: Encountered an unexpected exception", "java.lang.IllegalStateException: boom")
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("\tat a.b.C.m%d(C.java:%d)", i, i))
	}
	lines = append(lines, "Caused by: java.lang.NullPointerException", "\tat x.Y.z(Y.java:1)", "\t... 30 more")
	for i := 0; i < 10; i++ {
		lines = append(lines, fmt.Sprintf("[12:03:%02d] [Server thread/INFO]: 后面的信息 %d", i, i))
	}
	out := Condense(lines, 5)
	text := strings.Join(out, "\n")
	for _, want := range []string{StartMark, "Can't keep up!  ×37", "java.lang.IllegalStateException: boom", "\tat a.b.C.m14(C.java:14)",
		"Caused by: java.lang.NullPointerException", "\tat x.Y.z(Y.java:1)", "后面的信息 9", "……（省略 50 行）"} {
		if !strings.Contains(text, want) {
			t.Errorf("精简结果里应有 %q：\n%s", want, text)
		}
	}
	for _, bad := range []string{"\tat a.b.C.m15(", "普通信息 3", "后面的信息 4"} {
		if strings.Contains(text, bad) {
			t.Errorf("精简结果里不该有 %q", bad)
		}
	}
}

func TestCrashSummary(t *testing.T) {
	var b strings.Builder
	b.WriteString("---- Minecraft Crash Report ----\n// Uh... Did I do that?\n\nTime: 2026-10-28 13:10:44\nDescription: Watching Server\n\njava.lang.Error: Watchdog\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\tat a.b.C.m%d(C.java:%d)\n", i, i)
	}
	b.WriteString("\n\nA detailed walkthrough of the error, its code path and all known details is as follows:\n---------------------------------------------------------------------------------------\n\n-- Head --\nThread: Server Watchdog\nSuspected Mods: \n\tCreate (create), Version: 6.0.4\n\t\tIssue tracker URL: https://github.com/Creators-of-Create/Create/issues\n")
	b.WriteString("Stacktrace:\n\tat x.y.Z(Z.java:1)\n\n-- System Details --\nDetails:\n\tMinecraft Version: 1.21.1\n\tOperating System: Windows 11 (amd64) version 10.0\n\tJava Version: 21.0.4, Eclipse Adoptium\n\tMemory: 1234567 bytes (1 MiB) / 4294967296 bytes (4096 MiB) up to 6442450944 bytes (6144 MiB)\n\tJVM Flags: 3 total; -Xms1024M -Xmx6144M -Dfile.encoding=UTF-8\n\tModLauncher: 11.0.4+main.d2e20e43\n\tMod List: \n\t\tcreate-1.21.1-6.0.4.jar |Create |create |6.0.4 |Manifest: NOSIGNATURE\n")
	s := CrashSummary(b.String())
	for _, want := range []string{"Description: Watching Server", "java.lang.Error: Watchdog", "\tat a.b.C.m14(", "Suspected Mods:", "Minecraft Version: 1.21.1", "Java Version: 21.0.4", "JVM Flags: 3 total", "Memory:"} {
		if !strings.Contains(s, want) {
			t.Errorf("崩溃报告节选里应有 %q：\n%s", want, s)
		}
	}
	if strings.Contains(s, "m15(") || strings.Contains(s, "create-1.21.1-6.0.4.jar |Create") {
		t.Errorf("节选太长：\n%s", s)
	}
	hs := HsErrSummary("#\n# A fatal error has been detected by the Java Runtime Environment:\n#\n#  EXCEPTION_ACCESS_VIOLATION (0xc0000005) at pc=0x00007ff8, pid=1234, tid=5678\n#\n# Problematic frame:\n# C  [lwjgl.dll+0x1234]\n#\n\n---------------  S U M M A R Y ------------\n\nCommand Line: -Xmx4G\nUSERNAME=alice\n")
	if !strings.Contains(hs, "EXCEPTION_ACCESS_VIOLATION") || !strings.Contains(hs, "lwjgl.dll") || strings.Contains(hs, "USERNAME") {
		t.Errorf("hs_err 摘要：%q", hs)
	}
}

func TestLineLevel(t *testing.T) {
	for line, want := range map[string]string{
		"[12:00:00] [Server thread/WARN]: Can't keep up!":                         "warn",
		"[12:00:00] [main/ERROR] [mixin/]: Mixin apply failed":                    "error",
		"[12:00:00 FATAL]: boom":                                                  "error",
		"java.lang.NullPointerException: x":                                       "error",
		"\tat a.b.C.d(C.java:1)":                                                  "error",
		"Caused by: java.io.IOException":                                          "error",
		"[12:00:00] [Server thread/INFO]: Done (1.0s)!":                           "",
		"[12:00:00] [Server thread/INFO] [minecraft/MinecraftServer]: <Steve> hi": "",
	} {
		if got := LineLevel(line); got != want {
			t.Errorf("%q：%q，应为 %q", line, got, want)
		}
	}
}

// PlayerEngine（AI 伙伴的身体）在专用服务器上启动时打的那一行 dist 报错不算「客户端模组放进了服务端」：例外只认
// net/minecraft/client/ 下的类、紧跟着 playerengine.mixins.json 对同一个类的「was not found」。真正的客户端模组照样认出来，
// 和 PlayerEngine 那几行同时出现时，结论里也只有那个客户端模组。
func TestPlayerEngineDistException(t *testing.T) {
	f := find(Analyze(input(t, "testdata/positive/client-only@neoforge-1.21.1-playerengine.log")), "client-only")
	if f == nil || !slices.Equal(f.Files, []string{"journeymap-neoforge-1.21.1-6.0.0-beta.33.jar"}) || !slices.Equal(f.Mods, []string{"journeymap"}) ||
		strings.Contains(strings.Join(f.Evidence, "\n"), "MultiPlayerGameMode") || !strings.Contains(f.Summary, "net.minecraft.client.KeyMapping") {
		t.Fatalf("同时有 PlayerEngine 的那几行和真正的客户端模组：%+v", f)
	}
	head := "[MineDeck] 启动: java -Xmx6144M @libraries/net/neoforged/neoforge/21.1.265/win_args.txt nogui\n"
	dist := "[11:51:05.506] [main/ERROR] [common.asm.RuntimeDistCleaner/DISTXFORM]: Attempted to load class net/minecraft/client/multiplayer/MultiPlayerGameMode for invalid dist DEDICATED_SERVER\n"
	warn := "[11:51:05.523] [main/WARN] [mixin/]: Error loading class: net/minecraft/client/multiplayer/MultiPlayerGameMode (java.lang.RuntimeException: Attempted to load class net/minecraft/client/multiplayer/MultiPlayerGameMode for invalid dist DEDICATED_SERVER)\n"
	miss := func(class, cfg string) string {
		return "[11:51:05.524] [main/WARN] [mixin/]: @Mixin target " + class + " was not found " + cfg + ":ClientBlockBreakMixin from mod playerengine\n"
	}
	run := func(log string) []string {
		return rulesOf(Analyze(Input{Lines: strings.Split(strings.TrimRight(head+log, "\n"), "\n"), Core: "neoforge"}))
	}
	cases := []struct {
		name string
		log  string
		want []string
	}{
		{"PlayerEngine 原样的三行", dist + warn + miss("net.minecraft.client.multiplayer.MultiPlayerGameMode", "playerengine.mixins.json"), nil},
		{"中间隔了几行也认", dist + "[11:51:05.510] [main/INFO] [mixin/]: x\n" + warn + miss("net.minecraft.client.multiplayer.MultiPlayerGameMode", "playerengine.mixins.json"), nil},
		{"没有 was not found 那一行", dist + warn, []string{"client-only"}},
		{"别的模组的 mixin", dist + warn + miss("net.minecraft.client.multiplayer.MultiPlayerGameMode", "xaerominimap.mixins.json"), []string{"client-only"}},
		{"was not found 说的是别的类", dist + warn + miss("net.minecraft.client.gui.screens.Screen", "playerengine.mixins.json"), []string{"client-only"}},
		{"隔得太远", dist + strings.Repeat("[11:51:05.510] [main/INFO] [mixin/]: x\n", 6) + miss("net.minecraft.client.multiplayer.MultiPlayerGameMode", "playerengine.mixins.json"), []string{"client-only"}},
		{"不是 net/minecraft/client 下的类", "[11:51:05.506] [main/ERROR] [common.asm.RuntimeDistCleaner/DISTXFORM]: Attempted to load class com/example/ClientOnly for invalid dist DEDICATED_SERVER\n" +
			miss("com.example.ClientOnly", "playerengine.mixins.json"), []string{"client-only"}},
	}
	for _, c := range cases {
		if got := run(c.log); !slices.Equal(got, c.want) {
			t.Errorf("%s：认出 %v，应为 %v", c.name, got, c.want)
		}
	}
}
