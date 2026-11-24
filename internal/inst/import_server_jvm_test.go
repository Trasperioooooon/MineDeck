package inst

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
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
	opts, err := scriptJavaOptions([]byte(src), true, "jar:server.jar")
	want := []string{"-Xmx6G", "-Xms2G", "-XX:+UseG1GC", "-XX:MaxGCPauseMillis=200", "-Dmotd=hello world", "-Dfile.encoding=UTF-8", "%JAVA_OPTS%"}
	if err != nil || !slices.Equal(opts, want) {
		t.Fatalf("认出的参数：%q %v，应为 %q", opts, err, want)
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
	opts, err := scriptJavaOptions([]byte(src), false, "jar:server.jar")
	want := []string{"-Xmx4G", "-XX:+UseZGC", "-Dlog.dir=logs", "-Dname=a b", "-Dpath=c d", "$UNKNOWN"}
	if err != nil || !slices.Equal(opts, want) {
		t.Fatalf("认出的参数：%q %v，应为 %q", opts, err, want)
	}
}

// 好几行都在启动 Java 时用点名启动文件的那一行；没有启动 Java 的那一行就认不出（A43）。
func TestScriptJavaOptionsPicksLaunchLine(t *testing.T) {
	src := "java -Xmx1G -jar forge-installer.jar --installServer\r\n" +
		"java @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt %*\r\n"
	opts, err := scriptJavaOptions([]byte(src), true, "args:libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt")
	if err != nil || !slices.Equal(opts, []string{"@user_jvm_args.txt"}) {
		t.Fatalf("应挑启动服务端的那一行：%q %v", opts, err)
	}
	if opts, err := scriptJavaOptions([]byte(src), true, "jar:server.jar"); err != nil || !slices.Equal(opts, []string{"-Xmx1G"}) {
		t.Fatalf("没有点名启动文件的就用第一行：%q %v", opts, err)
	}
	for _, src := range []string{"@echo off\r\necho 先运行安装器\r\npause\r\n", "", "javac Foo.java\n"} {
		if opts, err := scriptJavaOptions([]byte(src), true, "jar:server.jar"); !errors.Is(err, errNoJavaLine) {
			t.Fatalf("没有启动 Java 的那一行，不该认出东西：%q %v", opts, err)
		}
	}
}

// 后面跟一个值的选项（--add-opens X、-cp X）连值算一项，不吞掉后面的 JVM 参数；用 start、if、& 起的 java 认不出（A43）。
func TestScriptJavaOptionsPairsAndUnrecognized(t *testing.T) {
	for src, want := range map[string][]string{
		"java --add-opens java.base/java.lang=ALL-UNNAMED -XX:+UseG1GC -Dfoo=1 -jar server.jar nogui\r\n": {"--add-opens java.base/java.lang=ALL-UNNAMED", "-XX:+UseG1GC", "-Dfoo=1"},
		"java -cp libs/* -XX:+UseG1GC -Dfoo=1 net.minecraft.server.Main nogui\r\n":                        {"-cp libs/*", "-XX:+UseG1GC", "-Dfoo=1"},
		"java -Xss2M --add-exports a/b=c -m my.module/Main\r\n":                                           {"-Xss2M", "--add-exports a/b=c"},
	} {
		if opts, err := scriptJavaOptions([]byte(src), true, "jar:server.jar"); err != nil || !slices.Equal(opts, want) {
			t.Errorf("%q：认出 %q %v，应为 %q", src, opts, err, want)
		}
	}
	sj := filterScriptJVM([]string{"--add-opens java.base/java.lang=ALL-UNNAMED", "-cp libs/*", "-XX:+UseG1GC"})
	if !slices.Equal(sj.carried, []string{"-XX:+UseG1GC"}) || len(sj.dropped) != 2 ||
		sj.dropped[0].arg != "--add-opens java.base/java.lang=ALL-UNNAMED" || sj.dropped[1].why != whyPrefix {
		t.Fatalf("两段式的连值列进没带：%+v", sj)
	}
	for _, src := range []string{
		"start \"Server\" java -XX:+UseG1GC -jar server.jar\r\n",
		"if exist server.jar java -XX:+UseG1GC -jar server.jar\r\n",
		"cd /d %~dp0 & java -XX:+UseG1GC -jar server.jar & pause\r\n",
	} {
		if opts, err := scriptJavaOptions([]byte(src), true, "jar:server.jar"); !errors.Is(err, errNoJavaLine) {
			t.Errorf("%q 应认不出：%q %v", src, opts, err)
		}
	}
}

// 变量每行翻一倍（set A=%A%%A%）：变量的值、展开以后的一行超过上限就整份按认不出办，很快返回（A43）。
func TestScriptJavaOptionsVarBlowup(t *testing.T) {
	var bat, sh strings.Builder
	bat.WriteString("set A=-Dx=0123456789abcdef\r\n")
	sh.WriteString("A=-Dx=0123456789abcdef\n")
	for i := 0; i < 30; i++ {
		bat.WriteString("set A=%A%%A%\r\n")
		sh.WriteString("A=$A$A\n")
	}
	bat.WriteString("java %A% -jar server.jar\r\n")
	sh.WriteString("java $A -jar server.jar\n")
	long := "java " + strings.Repeat("-XX:+UseG1GC ", 3000) + "-jar server.jar\r\n" // 一行本身就超过上限
	for name, c := range map[string]struct {
		src string
		bat bool
	}{"bat 翻倍": {bat.String(), true}, "sh 翻倍": {sh.String(), false}, "一行太长": {long, true}} {
		start := time.Now()
		opts, err := scriptJavaOptions([]byte(c.src), c.bat, "jar:server.jar")
		if !errors.Is(err, errScriptTooLong) || opts != nil {
			t.Errorf("%s：应按认不出办：%d 个参数 %v", name, len(opts), err)
		}
		if took := time.Since(start); took > time.Second {
			t.Errorf("%s：用了 %v", name, took)
		}
	}
	// 不到上限的照常展开
	ok := "set A=-XX:+UseG1GC\r\nset A=%A% %A%\r\njava %A% -jar server.jar\r\n"
	if opts, err := scriptJavaOptions([]byte(ok), true, "jar:server.jar"); err != nil || !slices.Equal(opts, []string{"-XX:+UseG1GC", "-XX:+UseG1GC"}) {
		t.Fatalf("没超上限的照常认：%q %v", opts, err)
	}
}

// allocDuring fn 期间一共申请了多少字节内存（runtime.MemStats.TotalAlloc 的差）。
func allocDuring(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// 单个变量、单行都没超上限，整份加起来很大（A43）：一个一万六千字的变量在一千行启动 Java 的命令里各用两次（脚本才 29 KB），
// 或者两千个变量各存两份它。都要在整份的上限那里停下、按认不出办，一路申请的内存有上限：一千行那份现在四十几 MB
// （停下之前展开的三十来行拆成参数），以前要申请一个多 GB；两千个变量那份现在两三 MB，以前一百多 MB。
func TestScriptJavaOptionsExpandBudget(t *testing.T) {
	half := strings.Repeat("a ", 8190) // 拆开是八千多个参数
	var bat, sh, vars strings.Builder
	bat.WriteString("set A=" + half + "\r\n")
	sh.WriteString("A=\"" + half + "\"\n")
	for i := 0; i < 1000; i++ {
		bat.WriteString("java %A%%A%\r\n")
		sh.WriteString("java $A$A\n")
	}
	vars.WriteString("set A=" + strings.Repeat("a", 16380) + "\r\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&vars, "set V%d=%%A%%%%A%%\r\n", i)
	}
	vars.WriteString("java -XX:+UseG1GC -jar server.jar\r\n")
	for name, c := range map[string]struct {
		src   string
		bat   bool
		limit uint64
	}{
		"bat 一千行": {bat.String(), true, 128 << 20},
		"sh 一千行":  {sh.String(), false, 128 << 20},
		"两千个变量":   {vars.String(), true, 32 << 20},
	} {
		var opts []string
		var err error
		got := allocDuring(func() { opts, err = scriptJavaOptions([]byte(c.src), c.bat, "jar:server.jar") })
		if !errors.Is(err, errScriptTooLong) || opts != nil {
			t.Errorf("%s：整份展开超了上限，应按认不出办：%d 个参数 %v", name, len(opts), err)
		}
		if got > c.limit {
			t.Errorf("%s：申请了 %d MB 内存，应在 %d MB 以内：展开到整份的上限就该停下", name, got>>20, c.limit>>20)
		}
	}
}

// 一行里把一个将近 32 KB 的变量引用两万次：一边换一边量，超过一行的上限立刻停下，不先拼出几百 MB 再看（A43）。
func TestScriptJavaOptionsManyRefsStopEarly(t *testing.T) {
	a := strings.Repeat("a", maxScriptLine-10)
	for name, c := range map[string]struct {
		src string
		bat bool
	}{
		"bat": {"set A=" + a + "\r\njava " + strings.Repeat("%A%", 20000) + " -jar server.jar\r\n", true},
		"sh":  {"A=" + a + "\njava " + strings.Repeat("$A ", 20000) + "-jar server.jar\n", false},
	} {
		var err error
		got := allocDuring(func() { _, err = scriptJavaOptions([]byte(c.src), c.bat, "jar:server.jar") })
		if !errors.Is(err, errScriptTooLong) {
			t.Errorf("%s：应按认不出办：%v", name, err)
		}
		if got > 16<<20 {
			t.Errorf("%s：申请了 %d MB 内存，超过一行的上限就该停下", name, got>>20)
		}
	}
}

// 哪些参数带过来（A43）：每一类都有能带的和不带的，原因写清楚。
func TestFilterScriptJVM(t *testing.T) {
	carry := []string{
		"-XX:+UseG1GC", "-XX:-UseBiasedLocking", "-XX:MaxGCPauseMillis=200", "-XX:G1HeapRegionSize=8M", "-XX:MaxRAMPercentage=75.0",
		"-XX:+HeapDumpOnOutOfMemoryError",
		// 常见调优参数集要用的两个开关照旧带；关掉检查的只拒 - 的那种写法
		"-XX:+UnlockDiagnosticVMOptions", "-XX:+UnlockExperimentalVMOptions", "-XX:+BytecodeVerificationRemote",
		"-Xss2M", "-Xmn512m", "-Xshare:auto", "-Xshare:off", "-Xnoclassgc", "-Xincgc",
		"-Dfml.queryResult=confirm", "-Dpaper.playerconnection.keepalive=60", "-Dterminal.jline=false", "-Daikars.new.flags=true",
		"-Dusing.aikars.flags=https://mcflags.emc.gs", "-Dfoo",
		// user. 只拒 user.dir、user.home；值是相对路径、不是路径的照旧带
		"-Duser.timezone=Asia/Shanghai", "-Duser.language=zh", "-Duser.country=CN",
		"-Dfoo.dir=config/foo", "-Dfoo.range=1..5", "-Dfoo.list=a,b",
	}
	memory := []string{"-Xmx6G", "-Xms2G", "-XX:MaxHeapSize=8g", "-XX:InitialHeapSize=1g", "-XX:MinHeapSize=1g"}
	drop := map[string]string{
		// 另外加载程序、执行命令、再读参数、放开外部挂载
		"-javaagent:authlib-injector.jar=example.com": "另外加载程序", "-agentlib:jdwp=transport=dt_socket": "另外加载程序",
		"-agentpath:/tmp/x.so": "另外加载程序", "-Xbootclasspath/a:evil.jar": "另外加载程序", "-Xrunjdwp:server=y": "另外加载程序",
		"-XX:OnError=cmd /c calc": "执行命令", "-XX:OnOutOfMemoryError=calc": "执行命令",
		"-XX:VMOptionsFile=more.txt": "再读参数", "-XX:Flags=.hotspotrc": "再读参数",
		"-XX:+EnableDynamicAgentLoading": "挂进 Java",
		// 关掉 Java 对程序的检查、让后台的服务器停下等人
		"-XX:-BytecodeVerificationRemote": "关掉 Java 对程序的检查", "-XX:-BytecodeVerificationLocal": "关掉 Java 对程序的检查",
		"-XX:+ShowMessageBoxOnError": "弹窗等人点", "-XX:+PauseAtStartup": "停住等人",
		// -XX: 只带开关和数值
		"-XX:AOTLibrary=./evil.so": "-XX: 只带", "-XX:SharedArchiveFile=x.jsa": "-XX: 只带", "-XX:HeapDumpPath=C:\\x.hprof": "-XX: 只带",
		"-XX:ErrorFile=..\\x.log": "-XX: 只带", "-XX:LogFile=C:\\x.log": "-XX: 只带", "-XX:CompileCommandFile=x": "-XX: 只带", "-XX:": "-XX: 只带",
		// -X 小白名单
		"-Xlog:gc*:file=gc.log": "-X 只带", "-Xloggc:..\\server.properties": "-X 只带", "-Xdebug": "-X 只带", "-Xverify:none": "-X 只带",
		"-Xshare:dump": "-X 只带", "-X": "-X 只带",
		// -D：编码、JDK 自己的名字空间、代理、正版验证、日志配置
		"-Dfile.encoding=UTF-8": "编码由 MineDeck 管", "-Dstdout.encoding=UTF-8": "编码由 MineDeck 管", "-Dstderr.encoding=UTF-8": "编码由 MineDeck 管",
		"-Dsun.stdout.encoding=GBK": "编码由 MineDeck 管", "-Dsun.stderr.encoding=GBK": "编码由 MineDeck 管",
		"-Djava.library.path=C:\\evil": "Java 自己的设置", "-Djavax.net.ssl.trustStore=evil.jks": "Java 自己的设置",
		"-Djdk.attach.allowAttachSelf=true": "Java 自己的设置", "-Dsun.misc.unsafe=x": "Java 自己的设置",
		"-Dcom.sun.management.jmxremote.port=9010": "Java 自己的设置", "-Dcom.sun.jndi.ldap.object.trustURLCodebase=true": "Java 自己的设置",
		"-Dhttps.proxyHost=evil.example": "代理", "-Dhttp.proxyPort=8080": "代理", "-DsocksProxyHost=evil.example": "代理",
		"-Dftp.proxyHost=x": "代理", "-Dhttp.nonProxyHosts=*": "代理",
		"-Dminecraft.api.auth.host=https://evil.example": "正版验证", "-Dlog4j.configurationFile=my.xml": "日志配置",
		"-Dlog4j2.formatMsgNoLookups=true": "日志配置", "-DLog4jContextSelector=x": "日志配置", "-D": "没写名字",
		"-Dorg.apache.logging.log4j.formatMsgNoLookups=false": "日志配置",
		"-Duser.dir=.": "当前文件夹", "-Duser.home=home": "用户文件夹",
		"-Djna.library.path=natives": "另外加载程序", "-Djna.boot.library.path=natives": "另外加载程序",
		"-Dfabric.addMods=extra": "另外加载程序", "-DbundlerRepoDir=libs": "另外加载程序", "-DbundlerMainClass=x.Main": "另外加载程序",
		// -D 的值指到服务器文件夹外面：盘符、\\ 或 / 开头、file: 网址、带 .. 段，一串路径里有一个这样的也算
		"-Dfoo.path=C:\\evil": "外面", "-Dfoo.path=d:/evil": "外面", "-Dfoo.path=\\\\server\\share": "外面", "-Dfoo.path=/etc/x": "外面",
		"-Dfoo.path=../outside": "外面", "-Dfoo.path=config\\..\\..\\x": "外面", "-Dfoo.path=file:///C:/x": "外面", "-Dfoo.paths=libs;C:\\x": "外面",
		// 变量、空格、别的开头、太长
		"%JAVA_OPTS%": "变量", "$OPTS": "变量", "-Dmotd=hello world": "空格",
		"-server": "只带 -XX:、-D、-X", "--add-opens=java.base/java.lang=ALL-UNNAMED": "只带 -XX:、-D、-X", "-ea": "只带 -XX:、-D、-X",
		"-Dlong=" + strings.Repeat("x", maxScriptArg): "太长",
	}
	in := append(append(slices.Clone(carry), memory...), "-XX:+UseG1GC") // 重复的只带一个
	for a := range drop {
		in = append(in, a)
	}
	sj := filterScriptJVM(in)
	if !slices.Equal(sj.carried, carry) {
		t.Fatalf("带过来的：%q，应为 %q", sj.carried, carry)
	}
	if !slices.Equal(sj.memory, memory) {
		t.Fatalf("内存参数不算没带，单独记：%q", sj.memory)
	}
	why := map[string]string{}
	for _, d := range sj.dropped {
		why[d.arg] = d.why
	}
	if len(why) != len(drop) {
		t.Errorf("没带的应正好是这 %d 个：%d 个", len(drop), len(why))
	}
	for arg, part := range drop {
		if got, ok := why[arg]; !ok || !strings.Contains(got, part) {
			t.Errorf("%s 应不带、原因里有「%s」：%q %v", clipArg(arg), part, got, ok)
		}
	}

	// 个数、总长有上限：超了的列进没带
	var many []string
	for i := 0; i < maxCarriedJVM+5; i++ {
		many = append(many, fmt.Sprintf("-Dk%d=v", i))
	}
	if sj := filterScriptJVM(many); len(sj.carried) != maxCarriedJVM || len(sj.dropped) != 5 || sj.dropped[0].why != whyTooMany {
		t.Fatalf("超过 %d 个的列进没带：带 %d 个，没带 %+v", maxCarriedJVM, len(sj.carried), sj.dropped)
	}
	var wide []string
	for i := 0; i < 20; i++ {
		wide = append(wide, fmt.Sprintf("-Dk%d=%s", i, strings.Repeat("v", 900)))
	}
	sj = filterScriptJVM(wide)
	total := 0
	for _, a := range sj.carried {
		total += len(a)
	}
	if total > maxCarriedJVMLen || len(sj.dropped) == 0 {
		t.Fatalf("带过来的加起来不超过 %d：%d，没带 %d 个", maxCarriedJVMLen, total, len(sj.dropped))
	}
}

// 包里是 MineDeck 自己生成的 run.bat：自己的编码参数不带成自定义参数，自己的 -Xms、-Xmx 不列成没带（A43）。
func TestFilterScriptJVMOwnRunBat(t *testing.T) {
	dir := t.TempDir()
	i := &Instance{Settings: Settings{Name: "原来那台", JavaPath: `C:\Java\jdk-17\bin\java.exe`, XmxMB: 4096, XmsMB: 1024,
		LaunchTarget: "jar:paper-1.20.1-100.jar", ExtraJVM: []string{"-XX:+UseG1GC", "-javaagent:authlib.jar=x", "-Dlog4j2.formatMsgNoLookups=true"}}, Dir: dir}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	opts, err := scriptJavaOptions(b, true, "jar:paper-1.20.1-100.jar")
	if err != nil {
		t.Fatal(err)
	}
	sj := filterScriptJVM(opts)
	if !slices.Equal(sj.carried, []string{"-XX:+UseG1GC"}) {
		t.Fatalf("只该带过来原来手写的那个：%q", sj.carried)
	}
	if !slices.Equal(sj.memory, []string{"-Xms1024M", "-Xmx4096M"}) {
		t.Fatalf("内存参数应单独记：%q", sj.memory)
	}
	for _, d := range sj.dropped {
		if strings.HasPrefix(d.arg, "-Xm") {
			t.Errorf("内存不算没带：%+v", d)
		}
		if strings.Contains(d.arg, "encoding") && d.why != whyEncoding {
			t.Errorf("编码参数的原因应是编码由 MineDeck 管：%+v", d)
		}
	}
}

// 导入现成服务端：启动脚本里能带的 JVM 参数进了这台服务器的自定义 JVM 参数，@user_jvm_args.txt 里的也算；
// 带了哪些、没带哪些写进任务日志和结果，内存只在日志里说（A43）。log4shell 防护照旧由 MineDeck 按版本套。
func TestImportServerArchiveCarriesScriptJVM(t *testing.T) {
	m, _ := importEnv(t)
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"Srv/paper-1.20.1-100.jar": paperJarBytes(t),
		"Srv/eula.txt":             "eula=true\n",
		"Srv/server.properties":    "server-port=25565\n",
		"Srv/start.bat":            "@echo off\r\njava -Xmx8G -XX:+UseG1GC -javaagent:authlib-injector.jar=example.com -Dlog4j2.formatMsgNoLookups=true ^\r\n -Dfile.encoding=UTF-8 -Dpaper.playerconnection.keepalive=60 -jar paper-1.20.1-100.jar nogui\r\n",
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
	for _, want := range []string{"-XX:+UseG1GC", "-Dpaper.playerconnection.keepalive=60"} {
		if !slices.Contains(got, want) {
			t.Errorf("应带过来 %s：%q", want, got)
		}
	}
	for _, bad := range []string{"-Xmx8G", "-javaagent:authlib-injector.jar=example.com", "-XX:+UseZGC", "-Dfile.encoding=UTF-8"} {
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
		t.Fatalf("结果里要写明带了哪些、没带哪些（内存不算没带）：%+v", w)
	}
	log := strings.Join(sn.Log, "\n")
	if !strings.Contains(log, "-javaagent:authlib-injector.jar=example.com（会让 Java 另外加载程序") || !strings.Contains(log, "start.bat 里的这些 JVM 参数带到了") ||
		!strings.Contains(log, "内存设置（-Xmx8G）没有用") {
		t.Fatalf("任务日志里要写明：%s", log)
	}

	// Forge 的启动脚本：参数在 @user_jvm_args.txt 里
	forge := zipOf(t, map[string]string{
		"libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt": "--launchTarget forgeserver --fml.forgeVersion 47.3.0 --fml.mcVersion 1.20.1\n",
		"run.bat":           "@echo off\r\njava @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt %*\r\npause\r\n",
		"user_jvm_args.txt": "# Xmx and Xms set the maximum and minimum RAM usage\n-Xmx6G\n-XX:+UseG1GC -Daikars.new.flags=true\n--add-opens java.base/java.lang=ALL-UNNAMED\n",
	})
	fp, err := modpack.Parse(forge)
	if err != nil {
		t.Fatal(err)
	}
	sj, found, err := packScriptJVM(fp, fp.Launch)
	if !found || err != nil || sj.script != "run.bat" || !slices.Equal(sj.carried, []string{"-XX:+UseG1GC", "-Daikars.new.flags=true"}) ||
		!slices.Equal(sj.memory, []string{"-Xmx6G"}) || len(sj.dropped) != 1 || sj.dropped[0].arg != "--add-opens java.base/java.lang=ALL-UNNAMED" {
		t.Fatalf("@user_jvm_args.txt 里的参数也算：%+v %v %v", sj, found, err)
	}

	// 没有启动脚本：什么都不带、不写结果
	plain, err := modpack.Parse(zipOf(t, map[string]string{"paper-1.20.1-100.jar": paperJarBytes(t), "eula.txt": "eula=true\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if sj, found, err := packScriptJVM(plain, plain.Launch); found || err != nil {
		t.Fatalf("没有启动脚本时不该认出东西：%+v %v %v", sj, found, err)
	}
}

// 启动脚本认不出、展开太长：什么都不带，任务日志说一句；参数一大串时结果里只列前面一部分、写一共几个，日志也不会一行几兆（A43）。
func TestImportServerScriptJVMLimits(t *testing.T) {
	run := func(name, script string) (*Instance, []string, *tasks.Warning) {
		t.Helper()
		m, _ := importEnv(t)
		pack, err := modpack.Parse(zipOf(t, map[string]string{
			"paper-1.20.1-100.jar": paperJarBytes(t), "eula.txt": "eula=true\n", "server.properties": "server-port=25565\n", "start.bat": script,
		}))
		if err != nil {
			t.Fatal(err)
		}
		tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
		if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: name, XmxMB: 2048, Port: 25578, EULA: true}, filepath.Join(app.ServersDir, "limits")); err != nil {
			t.Fatalf("%s：导入失败：%v", name, err)
		}
		i, _ := m.Get(name)
		return i, tk.Snapshot().Log, warningOf(tk, "script-jvm")
	}
	i, log, w := run("认不出", "@echo off\r\nstart \"Server\" java -XX:+UseG1GC -jar paper-1.20.1-100.jar\r\n")
	if w != nil || slices.Contains(i.ExtraJVMSnapshot(), "-XX:+UseG1GC") || !strings.Contains(strings.Join(log, "\n"), "认不出启动 Java 的那一行") {
		t.Fatalf("认不出时什么都不带、日志说一句：%+v %q", w, log)
	}

	var bat strings.Builder
	bat.WriteString("set A=-XX:+UseG1GC\r\n")
	for k := 0; k < 25; k++ {
		bat.WriteString("set A=%A% %A%\r\n")
	}
	bat.WriteString("java %A% -jar paper-1.20.1-100.jar\r\n")
	i, log, w = run("翻倍", bat.String())
	if w != nil || slices.Contains(i.ExtraJVMSnapshot(), "-XX:+UseG1GC") || !strings.Contains(strings.Join(log, "\n"), "展开以后太长") {
		t.Fatalf("展开太长时什么都不带、日志说一句：%+v %q", w, log)
	}

	var many strings.Builder
	many.WriteString("java")
	for k := 0; many.Len() < maxScriptLine-100; k++ {
		fmt.Fprintf(&many, " -a%d", k)
	}
	many.WriteString(" -jar paper-1.20.1-100.jar\r\n")
	_, log, w = run("一大串", many.String())
	if w == nil || len(w.Items) != maxWarnItems+1 || !strings.Contains(w.Items[maxWarnItems].Name, "一共") || !strings.Contains(w.Title, "没带过来") {
		t.Fatalf("结果里只列前 %d 个、写一共几个：%v", maxWarnItems, w)
	}
	for _, l := range log {
		if len(l) > 4096 {
			t.Fatalf("任务日志一行不该这么长：%d 字节", len(l))
		}
	}
}
