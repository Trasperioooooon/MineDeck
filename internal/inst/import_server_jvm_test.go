package inst

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/modpack"
)

// 启动脚本里启动 Java 的那一行：cmd 的 ^ 续行、set 过的 %变量%、%~dp0、引号里的空格（A43）。
func TestScriptJavaOptionsBat(t *testing.T) {
	src := "@echo off\r\n" +
		"rem java -XX:+ThisIsAComment -jar server.jar\r\n" +
		"set MEM=-Xmx6G -Xms2G\r\n" +
		"set \"GC=-XX:+UseG1GC -XX:MaxGCPauseMillis=200\"\r\n" +
		"\"%~dp0jre\\bin\\java.exe\" %MEM% %GC% ^\r\n" +
		"  \"-Dmotd=hello world\" -Dfile.encoding=UTF-8 %JAVA_OPTS% ^\r\n" +
		"  -jar \"%~dp0server.jar\" nogui %*\r\n" +
		"pause\r\n"
	opts, ok := scriptJavaOptions([]byte(src), true, "jar:server.jar")
	want := []string{"-Xmx6G", "-Xms2G", "-XX:+UseG1GC", "-XX:MaxGCPauseMillis=200", "-Dmotd=hello world", "-Dfile.encoding=UTF-8", "%JAVA_OPTS%"}
	if !ok || !slices.Equal(opts, want) {
		t.Fatalf("认出的参数：%q %v，应为 %q", opts, ok, want)
	}
}

// sh 的 \ 续行、赋值过的 $变量 和 ${变量}、单双引号、exec（A43）。
func TestScriptJavaOptionsSh(t *testing.T) {
	src := "#!/bin/sh\n" +
		"# java -XX:+Commented -jar server.jar\n" +
		"MEM=\"-Xmx4G\"\n" +
		"export FLAGS='-XX:+UseZGC -Dlog.dir=logs'\n" +
		"cd \"$(dirname \"$0\")\"\n" +
		"exec java $MEM ${FLAGS} \\\n" +
		"  '-Dname=a b' -Dpath=\"c d\" $UNKNOWN \\\n" +
		"  -jar ./server.jar --nogui \"$@\"\n"
	opts, ok := scriptJavaOptions([]byte(src), false, "jar:server.jar")
	want := []string{"-Xmx4G", "-XX:+UseZGC", "-Dlog.dir=logs", "-Dname=a b", "-Dpath=c d", "$UNKNOWN"}
	if !ok || !slices.Equal(opts, want) {
		t.Fatalf("认出的参数：%q %v，应为 %q", opts, ok, want)
	}
}

// 好几行都在启动 Java 时用点名启动文件的那一行；没有启动 Java 的那一行就认不出（A43）。
func TestScriptJavaOptionsPicksLaunchLine(t *testing.T) {
	src := "java -Xmx1G -jar forge-installer.jar --installServer\r\n" +
		"java @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt %*\r\n"
	opts, ok := scriptJavaOptions([]byte(src), true, "args:libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt")
	if !ok || !slices.Equal(opts, []string{"@user_jvm_args.txt"}) {
		t.Fatalf("应挑启动服务端的那一行：%q %v", opts, ok)
	}
	if opts, ok := scriptJavaOptions([]byte(src), true, "jar:server.jar"); !ok || !slices.Equal(opts, []string{"-Xmx1G"}) {
		t.Fatalf("没有点名启动文件的就用第一行：%q %v", opts, ok)
	}
	for _, src := range []string{"@echo off\r\necho 先运行安装器\r\npause\r\n", "", "javac Foo.java\n"} {
		if opts, ok := scriptJavaOptions([]byte(src), true, "jar:server.jar"); ok {
			t.Fatalf("没有启动 Java 的那一行，不该认出东西：%q", opts)
		}
	}
}

// 安全规矩：只带 -XX:、-D、-X 开头的；内存、加载外部程序的、出错执行命令的、再读参数文件的、日志配置、带变量和空格的都不带（A43）。
func TestFilterScriptJVM(t *testing.T) {
	carried, dropped := filterScriptJVM([]string{
		"-XX:+UseG1GC", "-Dfile.encoding=UTF-8", "-Xss2M", "-XX:+UseG1GC", "-Xlog:gc*:file=gc.log",
		"-Xmx6G", "-Xms2G",
		"-javaagent:authlib-injector.jar=example.com", "-agentlib:jdwp=transport=dt_socket", "-agentpath:/tmp/x.so",
		"-Xbootclasspath/a:evil.jar", "-Xrunjdwp:server=y",
		"-XX:OnError=cmd /c calc", "-XX:OnOutOfMemoryError=calc", "-XX:VMOptionsFile=more.txt", "-XX:Flags=.hotspotrc",
		"-Dlog4j.configurationFile=my.xml", "-Dlog4j2.formatMsgNoLookups=true",
		"%JAVA_OPTS%", "$OPTS", "-Dmotd=hello world",
		"-server", "--add-opens=java.base/java.lang=ALL-UNNAMED", "-ea", "-D", "-X", "-XX:",
	})
	if want := []string{"-XX:+UseG1GC", "-Dfile.encoding=UTF-8", "-Xss2M", "-Xlog:gc*:file=gc.log"}; !slices.Equal(carried, want) {
		t.Fatalf("带过来的：%q，应为 %q", carried, want)
	}
	why := map[string]string{}
	for _, d := range dropped {
		why[d.arg] = d.why
	}
	for arg, part := range map[string]string{
		"-Xmx6G": "内存", "-Xms2G": "内存",
		"-javaagent:authlib-injector.jar=example.com": "另外加载程序", "-agentlib:jdwp=transport=dt_socket": "另外加载程序",
		"-agentpath:/tmp/x.so": "另外加载程序", "-Xbootclasspath/a:evil.jar": "另外加载程序", "-Xrunjdwp:server=y": "另外加载程序",
		"-XX:OnError=cmd /c calc": "执行命令", "-XX:OnOutOfMemoryError=calc": "执行命令", "-XX:VMOptionsFile=more.txt": "再读参数", "-XX:Flags=.hotspotrc": "再读参数",
		"-Dlog4j.configurationFile=my.xml": "日志配置", "-Dlog4j2.formatMsgNoLookups=true": "日志配置",
		"%JAVA_OPTS%": "变量", "$OPTS": "变量", "-Dmotd=hello world": "空格",
		"-server": "只带", "--add-opens=java.base/java.lang=ALL-UNNAMED": "只带", "-ea": "只带", "-D": "只带", "-X": "只带", "-XX:": "只带",
	} {
		got, ok := why[arg]
		if !ok || !strings.Contains(got, part) {
			t.Errorf("%s 应不带、原因里有「%s」：%q %v", arg, part, got, ok)
		}
	}
}

// 导入现成服务端：启动脚本里能带的 JVM 参数进了这台服务器的自定义 JVM 参数，@user_jvm_args.txt 里的也算；
// 带了哪些、没带哪些写进任务日志和结果（A43）。log4shell 防护照旧由 MineDeck 按版本套。
func TestImportServerArchiveCarriesScriptJVM(t *testing.T) {
	m, _ := importEnv(t)
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"Srv/paper-1.20.1-100.jar": paperJarBytes(t),
		"Srv/eula.txt":             "eula=true\n",
		"Srv/server.properties":    "server-port=25565\n",
		"Srv/start.bat":            "@echo off\r\njava -Xmx8G -XX:+UseG1GC -javaagent:authlib-injector.jar=example.com -Dlog4j2.formatMsgNoLookups=true ^\r\n -Dfile.encoding=UTF-8 -jar paper-1.20.1-100.jar nogui\r\n",
		"Srv/run.sh":               "#!/bin/sh\njava -XX:+UseZGC -jar paper-1.20.1-100.jar\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "script-jvm")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "脚本参数", XmxMB: 4096, Port: 25577, EULA: true}, dir); err != nil {
		t.Fatalf("导入失败：%v\n%v", err, tk.Snapshot().Log)
	}
	i, _ := m.Get("脚本参数")
	got := i.ExtraJVMSnapshot()
	for _, want := range []string{"-XX:+UseG1GC", "-Dfile.encoding=UTF-8"} {
		if !slices.Contains(got, want) {
			t.Errorf("应带过来 %s：%q", want, got)
		}
	}
	for _, bad := range []string{"-Xmx8G", "-javaagent:authlib-injector.jar=example.com", "-XX:+UseZGC"} {
		if slices.Contains(got, bad) {
			t.Errorf("不该带过来 %s（.bat 说了算，.sh 不看）：%q", bad, got)
		}
	}
	if i.XmxMB != 4096 || !strings.Contains(mustRead(t, filepath.Join(dir, "run.bat")), "-XX:+UseG1GC") {
		t.Errorf("内存按导入页上填的，run.bat 里要有带过来的参数：%d", i.XmxMB)
	}
	sn := tk.Snapshot()
	w := warningOf(tk, "script-jvm")
	if w == nil || !strings.Contains(w.Title, "带过来 2 个，没带 3 个") || len(w.Items) != 3 || !strings.Contains(w.Note, "-XX:+UseG1GC") {
		t.Fatalf("结果里要写明带了哪些、没带哪些：%+v", w)
	}
	log := strings.Join(sn.Log, "\n")
	if !strings.Contains(log, "-javaagent:authlib-injector.jar=example.com（会让 Java 另外加载程序") || !strings.Contains(log, "start.bat 里的这些 JVM 参数带到了") {
		t.Fatalf("任务日志里要写明：%s", log)
	}

	// Forge 的启动脚本：参数在 @user_jvm_args.txt 里
	forge := zipOf(t, map[string]string{
		"libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt": "--launchTarget forgeserver --fml.forgeVersion 47.3.0 --fml.mcVersion 1.20.1\n",
		"run.bat":           "@echo off\r\njava @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt %*\r\npause\r\n",
		"user_jvm_args.txt": "# Xmx and Xms set the maximum and minimum RAM usage\n-Xmx6G\n-XX:+UseG1GC -Daikars.new.flags=true\n",
	})
	fp, err := modpack.Parse(forge)
	if err != nil {
		t.Fatal(err)
	}
	sj, ok := packScriptJVM(fp, fp.Launch)
	if !ok || sj.script != "run.bat" || !slices.Equal(sj.carried, []string{"-XX:+UseG1GC", "-Daikars.new.flags=true"}) || len(sj.dropped) != 1 {
		t.Fatalf("@user_jvm_args.txt 里的参数也算：%+v %v", sj, ok)
	}

	// 没有启动脚本：什么都不带、不写结果
	plain, err := modpack.Parse(zipOf(t, map[string]string{"paper-1.20.1-100.jar": paperJarBytes(t), "eula.txt": "eula=true\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if sj, ok := packScriptJVM(plain, plain.Launch); ok {
		t.Fatalf("没有启动脚本时不该认出东西：%+v", sj)
	}
}
