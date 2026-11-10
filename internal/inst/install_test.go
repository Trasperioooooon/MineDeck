package inst

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// 测试用版本清单（新版本在前），供 mcsrc.NewerOrEqualCached 从磁盘缓存离线读取。
const testManifest = `{"latest":{"release":"1.20.1","snapshot":"1.20.1"},"versions":[
{"id":"1.21.1","type":"release"},
{"id":"1.20.1","type":"release"},{"id":"1.18.1","type":"release"},{"id":"1.18","type":"release"},
{"id":"1.17.1","type":"release"},{"id":"1.17","type":"release"},{"id":"1.16.5","type":"release"},
{"id":"1.12.2","type":"release"}]}`

var testJava17 string // 假的本机 Java 17 的路径，TestMain 里准备

func TestMain(m *testing.M) {
	if os.Getenv(fakeServerEnv) == "1" { // proc_test.go：测试程序自己扮演服务器进程
		os.Exit(fakeServer())
	}
	dir, err := os.MkdirTemp("", "amh-inst-test")
	if err != nil {
		panic(err)
	}
	app.CacheDir = dir
	if err := os.WriteFile(filepath.Join(dir, "version_manifest_v2.json"), []byte(testManifest), 0o644); err != nil {
		panic(err)
	}
	// adopt_test.go：一个「本机已安装」的假 Java 17（java.Cached 从缓存目录的 javas.json 读）
	testJava17 = filepath.Join(dir, "jdk17", "bin", "java.exe")
	javas, _ := json.Marshal([]map[string]any{{"path": testJava17, "major": 17, "version": "17.0.1"}})
	if err := os.MkdirAll(filepath.Dir(testJava17), 0o755); err != nil {
		panic(err)
	}
	for p, b := range map[string][]byte{testJava17: nil, filepath.Join(dir, "javas.json"): javas} {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// 控制台输出一律 UTF-8：Java 18 起 System.out 不跟 file.encoding、跟 stdout.encoding（没设时是系统编码，中文 Windows 上是 GBK，
// 「@小木 停」的 GBK 恰好是合法的 UTF-8，会被解成乱码）。三个参数都在内存参数之后、实例自己的 JVM 参数和启动目标之前，run.bat 里也有。
func TestLaunchArgsConsoleUTF8(t *testing.T) {
	dir := t.TempDir()
	i := &Instance{Settings: Settings{Name: "t", Core: mcsrc.CoreNeoForge, MC: "1.21.1", JavaPath: `C:\java\bin\java.exe`, XmxMB: 4096, XmsMB: 1024,
		LaunchTarget: "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt", ExtraJVM: []string{"-XX:+UseG1GC"}}, Dir: dir}
	got := launchArgs(i)
	want := []string{"-Xms1024M", "-Xmx4096M", "-Dfile.encoding=UTF-8", "-Dstdout.encoding=UTF-8", "-Dstderr.encoding=UTF-8", "-XX:+UseG1GC",
		"@" + filepath.FromSlash("libraries/net/neoforged/neoforge/21.1.265/win_args.txt"), "nogui"}
	if !slices.Equal(got, want) {
		t.Fatalf("launchArgs = %q\nwant %q", got, want)
	}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	bat, err := os.ReadFile(filepath.Join(dir, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range consoleUTF8 {
		if !strings.Contains(string(bat), batQuoteArg(f)) {
			t.Errorf("run.bat 里没有 %s：\n%s", f, bat)
		}
	}

	// 控制台编码手动选了 GBK：只留 file.encoding，输出还是系统编码，按 GBK 解才对得上
	i.ConsoleEncoding = "gbk"
	got = launchArgs(i)
	want = []string{"-Xms1024M", "-Xmx4096M", "-Dfile.encoding=UTF-8", "-XX:+UseG1GC",
		"@" + filepath.FromSlash("libraries/net/neoforged/neoforge/21.1.265/win_args.txt"), "nogui"}
	if !slices.Equal(got, want) {
		t.Fatalf("GBK 时 launchArgs = %q\nwant %q", got, want)
	}
}

// 保存 JVM 参数（套用预设 / 清空 / 手改）后，老版本的 log4shell 缓解参数必须仍在 instance.json、run.bat 与托管启动参数里；
// 新版本与代理端不受影响。
func TestUpdateSettingsKeepsLog4jFix(t *testing.T) {
	preset := []string{"-XX:+UseG1GC", "-XX:MaxGCPauseMillis=200"}
	both := []string{log4jNoLookups, log4jConfig}
	cases := []struct {
		name    string
		core    mcsrc.Core
		mc      string
		xml     bool // 实例目录中是否有官方 xml（创建时下载成功）
		initial []string
		submit  []string
		want    []string
	}{
		{"1.16.5 套用预设", mcsrc.CorePaper, "1.16.5", true, both, preset, append(slices.Clone(preset), both...)},
		{"1.16.5 清空", mcsrc.CorePaper, "1.16.5", true, both, nil, both},
		{"1.16.5 前端已保留则原样", mcsrc.CorePaper, "1.16.5", true, both, append(slices.Clone(both), preset...), append(slices.Clone(both), preset...)},
		{"1.12.2 Forge 清空", mcsrc.CoreForge, "1.12.2", true, both, nil, both},
		{"1.16.5 xml 下载失败不强加引用", mcsrc.CorePaper, "1.16.5", false, []string{log4jNoLookups}, nil, []string{log4jNoLookups}},
		{"1.18 套用预设", mcsrc.CoreVanilla, "1.18", false, []string{log4jNoLookups}, preset, append(slices.Clone(preset), log4jNoLookups)},
		{"1.17.1 清空", mcsrc.CoreFabric, "1.17.1", false, []string{log4jNoLookups}, nil, []string{log4jNoLookups}},
		{"已丢失的实例保存时补回", mcsrc.CorePaper, "1.16.5", true, preset, preset, append(slices.Clone(preset), both...)},
		{"同名其他取值被剔除", mcsrc.CorePaper, "1.18", false, nil,
			[]string{log4jNoLookups, "-Xss1M", "-Dlog4j2.formatMsgNoLookups=false", "-Dlog4j2.formatMsgNoLookups"},
			[]string{log4jNoLookups, "-Xss1M"}},
		{"1.18.1 不受影响", mcsrc.CorePaper, "1.18.1", false, nil, preset, preset},
		{"1.20.1 手填可自由删除", mcsrc.CorePaper, "1.20.1", false, []string{log4jNoLookups}, nil, nil},
		{"1.20.1 手填原样保留", mcsrc.CorePaper, "1.20.1", false, nil, []string{"-Dlog4j2.formatMsgNoLookups=false"}, []string{"-Dlog4j2.formatMsgNoLookups=false"}},
		{"代理端不受影响", mcsrc.CoreVelocity, "3.4.0-SNAPSHOT", false, nil, nil, nil},
		{"版本无法判定时沿用已有缓解", mcsrc.CorePaper, "9.9-unknown", true, append(slices.Clone(preset), both...), nil, both},
		{"版本无法判定且原本没有则不加", mcsrc.CorePaper, "9.9-unknown", true, preset, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.xml {
				if err := os.WriteFile(filepath.Join(dir, log4jXML), []byte("<Configuration/>"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			i := &Instance{Settings: Settings{
				Name: "t", Core: c.core, MC: c.mc, JavaPath: `C:\java\bin\java.exe`,
				XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar", ExtraJVM: c.initial,
			}, Dir: dir}
			m := &Manager{insts: map[string]*Instance{"t": i}, creating: map[string]bool{}}
			submit := slices.Clone(c.submit)
			if err := m.UpdateSettings("t", 4096, 0, "", &submit); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(i.ExtraJVM, c.want) {
				t.Fatalf("ExtraJVM = %q, want %q", i.ExtraJVM, c.want)
			}
			if !slices.Equal(submit, c.submit) {
				t.Fatalf("调用方传入的切片被改写: %q", submit)
			}

			// 持久化：instance.json
			var saved Settings
			b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(saved.ExtraJVM, c.want) {
				t.Fatalf("instance.json extraJvm = %q, want %q", saved.ExtraJVM, c.want)
			}

			// run.bat 与托管启动同源（launchArgs）：缓解参数要么都在、要么都不在
			bat, err := os.ReadFile(filepath.Join(dir, "run.bat"))
			if err != nil {
				t.Fatal(err)
			}
			args := launchArgs(i)
			for _, f := range both {
				want := slices.Contains(c.want, f)
				if got := slices.Contains(args, f); got != want {
					t.Errorf("launchArgs 含 %s = %v, want %v（%q）", f, got, want, args)
				}
				if got := strings.Contains(string(bat), batQuoteArg(f)); got != want {
					t.Errorf("run.bat 含 %s = %v, want %v\n%s", f, got, want, bat)
				}
			}
		})
	}
}

// 只改内存 / 编码（extraJVM 为 nil）时不动 JVM 参数。
func TestUpdateSettingsNilExtraJVMUntouched(t *testing.T) {
	i := &Instance{Settings: Settings{
		Name: "t", Core: mcsrc.CorePaper, MC: "1.16.5", JavaPath: `C:\java\bin\java.exe`,
		XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar", ExtraJVM: []string{"-Xss1M"},
	}, Dir: t.TempDir()}
	m := &Manager{insts: map[string]*Instance{"t": i}, creating: map[string]bool{}}
	if err := m.UpdateSettings("t", 0, 0, "gbk", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(i.ExtraJVM, []string{"-Xss1M"}) || i.ConsoleEncoding != "gbk" {
		t.Fatalf("ExtraJVM = %q, enc = %q", i.ExtraJVM, i.ConsoleEncoding)
	}
}

func TestWithJVMFlags(t *testing.T) {
	in := []string{"-Xss1M", log4jNoLookups, log4jNoLookups}
	got := withJVMFlags(in, []string{log4jNoLookups, log4jNoLookups})
	if !slices.Equal(got, []string{"-Xss1M", log4jNoLookups}) {
		t.Fatalf("重复项未合并: %q", got)
	}
	out := withJVMFlags(in, nil)
	if !slices.Equal(out, in) {
		t.Fatalf("无须保留参数时应原样返回: %q", out)
	}
	out[0] = "changed"
	if in[0] != "-Xss1M" {
		t.Fatal("返回值与入参共享底层数组")
	}
}

// 安装器输出里的网络问题（测试报告 B-01）：一段异常栈只算一次（包括 Caused by 里才出现超时的），记下最近的下载地址；
// 正常的输出不算。
func TestInstallerNetWatch(t *testing.T) {
	lines := []string{
		"Considering library net.neoforged:coremods:7.0.3",
		"  Downloading library from https://maven.neoforged.net/releases/net/neoforged/coremods/7.0.3/coremods-7.0.3.jar",
		"java.net.SocketTimeoutException: Connect timed out",
		"at java.base/sun.nio.ch.NioSocketImpl.timedFinishConnect(NioSocketImpl.java:546)",
		"at net.minecraftforge.installer.DownloadUtils.downloadLibrary(DownloadUtils.java:149)",
		"Considering library net.neoforged:bus:8.0.2",
		"  Downloading library from https://maven.neoforged.net/releases/net/neoforged/bus/8.0.2/bus-8.0.2.jar",
		"java.io.IOException: 下载失败",
		"at net.minecraftforge.installer.DownloadUtils.download(DownloadUtils.java:200)",
		"Caused by: java.net.SocketTimeoutException: Connect timed out",
		"... 12 more",
		"java.net.UnknownHostException: libraries.minecraft.net",
		"Downloaded library from https://bmclapi2.bangbang93.com/maven/foo.jar",
		"The server installed successfully",
	}
	var w installerNet
	var got []string
	for _, l := range lines {
		if what := w.line(strings.TrimSpace(l)); what != "" {
			got = append(got, w.host+" "+what)
		}
	}
	want := []string{"maven.neoforged.net 连接超时", "maven.neoforged.net 连接超时", "libraries.minecraft.net 连不上"}
	if !slices.Equal(got, want) {
		t.Fatalf("认出的网络问题：%q，应为 %q", got, want)
	}
	if w.host != "bmclapi2.bangbang93.com" {
		t.Fatalf("最近的下载地址：%q", w.host)
	}
}

// 安装器以 0 退出不等于装好了：老版本 Forge 的安装器证书预检失败时，打两行提示就以 0 退出，什么都没装。
// 这几段输出是实测里原样留下来的。
func TestInstallerSaid(t *testing.T) {
	feed := func(lines ...string) installerSaid {
		var s installerSaid
		for _, ln := range lines {
			s.line(ln)
		}
		return s
	}
	// 证书预检失败、什么都没装就退出（Forge 1.16.5，重试的那一次）
	aborted := feed(
		"JVM info: Temurin - 1.8.0_512 - 25.512-b09",
		"Host: files.minecraftforge.net [198.18.1.87]",
		"Error checking https://files.minecraftforge.net/: Remote host terminated the handshake",
		"Failed to validate certificates for https://files.minecraftforge.net/ this typically means you have an outdated java.",
		"If instalation fails try updating your java!",
	)
	if err := aborted.err(); err == nil || !strings.Contains(err.Error(), "什么都没装") {
		t.Errorf("证书预检失败又没说装成功，要算这一次失败（好让外面重试）：%v", err)
	}
	// 正常装完（Forge、NeoForge 的安装器结尾都是这两行）
	ok := feed("Target Directory: .", "The server installed successfully", "You can delete this installer file now if you wish")
	if err := ok.err(); err != nil || !ok.success {
		t.Errorf("正常装完不该算失败：%v %+v", err, ok)
	}
	// 证书那两行出现过，但后面照常装完了：以装完为准
	if err := feed("Failed to validate certificates for https://x/ this typically means you have an outdated java.", "The server installed successfully").err(); err != nil {
		t.Errorf("提示过证书问题但最后装成了，不算失败：%v", err)
	}
	// 没见过成功那句话、也没有证书问题（不认识的安装器版本）：不拦，只由调用方提醒一句
	unknown := feed("Target Directory: .", "Done.")
	if err := unknown.err(); err != nil || unknown.success {
		t.Errorf("不认识的输出不该拦：%v %+v", err, unknown)
	}
}
