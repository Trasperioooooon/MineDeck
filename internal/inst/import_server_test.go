package inst

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
)

// zipOf 造一个服务端压缩包。jar 条目直接写进 zip（内容已经是 jar 字节）。
func zipOf(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func paperJarBytes(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "paper.jar")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"META-INF/MANIFEST.MF":   "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n",
		"META-INF/versions.list": "abc\t1.20.1\t1.20.1/paper-1.20.1.jar\n",
		"version.json":           `{"id":"1.20.1","java_version":17}`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 作者给的服务端 zip：套了一层文件夹，没有整合包清单。解压后用接管已有服务器的识别登记，不重新下载核心。
func TestImportServerArchive(t *testing.T) {
	m, _ := importEnv(t)
	z := zipOf(t, map[string]string{
		"作者服/server.properties":    "motd=hello\nserver-port=25565\nlevel-name=world\n",
		"作者服/mods/jei.jar":         "mod-bytes",
		"作者服/paper-1.20.1-100.jar": paperJarBytes(t),
		"作者服/eula.txt":             "eula=false\n",
		// 别人给的启动脚本和登记文件不能采用，否则下次启动就按包里写的去跑。
		"作者服/instance.json": `{"name":"evil","javaPath":"C:\\evil.exe"}`,
		"作者服/run.bat":       "evil",
	})
	pack, err := modpack.Parse(z)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Format != modpack.FormatServer || pack.Root != "作者服" || pack.Core != mcsrc.CorePaper || pack.MC != "1.20.1" {
		t.Fatalf("解析结果不对: %+v", pack)
	}
	dir := filepath.Join(app.ServersDir, "paper-srv")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "纸服", XmxMB: 2048, Port: 25566, EULA: true, AllowFlight: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, dir); err != nil {
		t.Fatalf("导入失败: %v\n日志: %v", err, tk.Snapshot().Log)
	}
	i, err := m.Get("纸服")
	if err != nil {
		t.Fatal(err)
	}
	if i.Adopted || i.Dir != dir || i.Core != mcsrc.CorePaper || i.MC != "1.20.1" || i.JavaMajor != 17 || i.JavaPath != testJava17 {
		t.Fatalf("登记不对: adopted=%v dir=%s core=%s mc=%s java=%d path=%s launch=%s",
			i.Adopted, i.Dir, i.Core, i.MC, i.JavaMajor, i.JavaPath, i.LaunchTarget)
	}
	if i.LaunchTarget != "jar:paper-1.20.1-100.jar" {
		t.Fatalf("启动文件应是压缩包里的那个 jar: %s", i.LaunchTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, "作者服")); !os.IsNotExist(err) {
		t.Fatal("包装目录不应留在实例里")
	}
	mod, err := os.ReadFile(filepath.Join(dir, "mods", "jei.jar"))
	if err != nil || string(mod) != "mod-bytes" {
		t.Fatalf("模组没解到实例里: %v %q", err, mod)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(props)
	// 压缩包里本来有 server.properties：只改端口，正版验证、允许飞行沿用包里的（这里包里没写，就不写）
	if !strings.Contains(text, "motd=hello") || !strings.Contains(text, "server-port=25566") || strings.Contains(text, "allow-flight") || strings.Contains(text, "online-mode") {
		t.Fatalf("server.properties 应只改端口、别的留着: %s", text)
	}
	eula, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if err != nil || !strings.Contains(string(eula), "eula=true") {
		t.Fatalf("应写上同意的 eula: %v %s", err, eula)
	}
	bat, err := os.ReadFile(filepath.Join(dir, "run.bat"))
	if err != nil || !strings.Contains(string(bat), "paper-1.20.1-100.jar") || strings.TrimSpace(string(bat)) == "evil" {
		t.Fatalf("run.bat 应是 MineDeck 按这个 jar 生成的，不是包里那份: %v %s", err, bat)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil || strings.Contains(string(saved), "evil.exe") || !strings.Contains(string(saved), `"name": "纸服"`) {
		t.Fatalf("instance.json 应是登记时写的，不是压缩包里那份: %v %s", err, saved)
	}
	if _, err := os.Stat(filepath.Join(dir, "paper-1.20.1-100.jar")); err != nil {
		t.Fatal(err)
	}
}

// jarBytesOf 造一个 jar（包内路径 → 内容），返回 jar 的字节。
func jarBytesOf(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// 压缩包里的 server.properties 写着正版验证：导入页上的开关（默认关）不能把它改成离线，
// 不然玩家的 UUID 全变，背包、白名单、管理员都对不上。只改端口，日志写明沿用了什么。
func TestImportServerArchiveKeepsOnlineMode(t *testing.T) {
	m, _ := importEnv(t)
	z := zipOf(t, map[string]string{
		"server.properties":    "online-mode=true\nallow-flight=false\nenforce-secure-profile=true\nserver-port=25565\n",
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"eula.txt":             "eula=true\n",
	})
	pack, err := modpack.Parse(z)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "keep")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "正版服", XmxMB: 2048, Port: 25570, EULA: true, OnlineMode: false, AllowFlight: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, dir); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"online-mode=true", "allow-flight=false", "enforce-secure-profile=true", "server-port=25570"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("应有 %s：%s", want, b)
		}
	}
	if log := strings.Join(tk.Snapshot().Log, "\n"); !strings.Contains(log, "正版验证（开）和允许飞行（关）沿用压缩包里") {
		t.Fatalf("日志应写明沿用了包里的设置：%s", log)
	}
}

// 压缩包里没有 server.properties：按导入页新建；离线时和新建一样关掉 enforce-secure-profile。
func TestImportServerArchiveNewProps(t *testing.T) {
	m, _ := importEnv(t)
	z := zipOf(t, map[string]string{
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"plugins/a.jar":        "plugin",
	})
	pack, err := modpack.Parse(z)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "fresh")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "离线服", XmxMB: 2048, Port: 25572, EULA: true, OnlineMode: false, AllowFlight: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, dir); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"online-mode=false", "allow-flight=true", "enforce-secure-profile=false", "server-port=25572"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("应有 %s：%s", want, b)
		}
	}
}

// 导入失败：任务一显示失败，半成品文件夹就已经删干净了（先清后报失败）。
func TestImportServerArchiveFailCleansUp(t *testing.T) {
	m, _ := importEnv(t)
	files := map[string]string{
		"server.properties":    "level-name=../outside\n",
		"paper-1.20.1-100.jar": paperJarBytes(t),
	}
	for i := 0; i < 300; i++ { // 文件多一点，删得慢一点：先报失败后删的话，这里很容易撞上没删完的时候
		files[fmt.Sprintf("world/region/r.%d.mca", i)] = "x"
	}
	pack, err := modpack.Parse(zipOf(t, files))
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.importServerAsync(pack, CreateReq{Name: "坏服", XmxMB: 2048, Port: 25571, EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		s := m.Tasks.Get(id).Snapshot()
		if s.Ended {
			if s.Err == "" || !strings.Contains(s.Err, "世界文件夹") {
				t.Fatalf("应因为世界文件夹指到外面而失败：%+v", s)
			}
			if s.Kind != "server-in" {
				t.Fatalf("任务的 kind 应是 server-in：%q", s.Kind)
			}
			ents, err := os.ReadDir(app.ServersDir)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(ents) != 0 {
				t.Fatalf("任务显示失败时，半成品文件夹应已删掉：%v", ents)
			}
			if _, err := m.Get("坏服"); err == nil {
				t.Fatal("失败的导入不该留在列表里")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("任务超时未结束：%+v", s)
		}
		time.Sleep(time.Millisecond)
	}
}

// 1.12–1.16.5 的服务端：和新建、接管一样加 log4shell 防护，下载官方的日志配置；压缩包里自带的那份不采用。
func TestImportServerArchiveLog4j(t *testing.T) {
	m, _ := importEnv(t)
	old := fetchLog4jXML
	t.Cleanup(func() { fetchLog4jXML = old })
	fetched := 0
	fetchLog4jXML = func(_ context.Context, dir, _ string) error {
		fetched++
		return os.WriteFile(filepath.Join(dir, log4jXML), []byte("<Configuration/>"), 0o644)
	}
	jar := jarBytesOf(t, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Paperclip\r\n\r\n",
		"patch.properties":     "version=1.16.5\n",
		"version.json":         `{"id":"1.16.5","java_version":17}`, // Java 17 只是为了用上测试里那个假的本机 Java
	})
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"paper-1.16.5-794.jar": jar,
		"server.properties":    "server-port=25565\n",
		"eula.txt":             "eula=true\n",
		log4jXML:               "<Configuration>别人改过的</Configuration>",
	}))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "old")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "老服", XmxMB: 2048, Port: 25573, EULA: true}, dir); err != nil {
		t.Fatalf("导入失败: %v\n%v", err, tk.Snapshot().Log)
	}
	i, err := m.Get("老服")
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 1 || !slices.Contains(i.ExtraJVM, log4jNoLookups) || !slices.Contains(i.ExtraJVM, log4jConfig) {
		t.Fatalf("应下载官方配置并带上两个参数：fetched=%d ExtraJVM=%v", fetched, i.ExtraJVM)
	}
	if b, err := os.ReadFile(filepath.Join(dir, log4jXML)); err != nil || string(b) != "<Configuration/>" {
		t.Fatalf("日志配置应是下载的那份：%v %q", err, b)
	}
	if bat, err := os.ReadFile(filepath.Join(dir, "run.bat")); err != nil || !strings.Contains(string(bat), log4jConfig) {
		t.Fatalf("run.bat 里应带上日志配置：%v %s", err, bat)
	}
	if log := strings.Join(tk.Snapshot().Log, "\n"); strings.Contains(log, "保存一次设置") {
		t.Fatalf("不该再有那句不成立的补救办法：%s", log)
	}
}

// 名字在 Windows 上不能用的文件不解压，日志里列出来；别的照常。设备名不管带不带扩展名都算（CON.txt、com1.log：
// Windows 11 上能建，老一些的 Windows 上是设备），结尾是点或空格的也算——以前 a.txt. 照收，Windows 落盘时去掉那个点，
// 就成了 a.txt，能把同名的另一个文件盖掉（v3.1.0 正式版前检查 C-2e）。
// 断言只看文件夹里列出来的名字，不对这些名字做 Stat、Open：老一些的 Windows 上那样会打开设备或者别的文件。
func TestImportServerArchiveSkipsBadNames(t *testing.T) {
	m, _ := importEnv(t)
	bad := []string{"config/NUL", "config/...", "config/CON.txt", "config/com1.log", "config/LPT¹.cfg", "config/trail.txt.", "config/space.txt "}
	files := map[string]string{
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"server.properties":    "server-port=25565\n",
		"eula.txt":             "eula=true\n",
		"config/ok.txt":        "ok",
	}
	for _, b := range bad {
		files[b] = "x"
	}
	pack, err := modpack.Parse(zipOf(t, files))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "names")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "名字", XmxMB: 2048, Port: 25574, EULA: true}, dir); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "config", "ok.txt")); err != nil || string(b) != "ok" {
		t.Fatalf("正常的文件应解压：%v %q", err, b)
	}
	if got := dirNames(t, filepath.Join(dir, "config")); !slices.Equal(got, []string{"ok.txt"}) {
		t.Errorf("config 里只该有 ok.txt：%q", got)
	}
	log := strings.Join(tk.Snapshot().Log, "\n")
	for _, b := range bad {
		if !strings.Contains(log, b) {
			t.Errorf("日志应列出没解压的 %q：%s", b, log)
		}
	}
}

// dirNames 一个文件夹里的条目名（只列，不打开）。
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// zipOrdered 按给的顺序写一个 zip（同名的条目也照写）。
func zipOrdered(t *testing.T, entries [][2]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// 只差大小写的两个文件在 Windows 上是同一个文件，以前后一个把前一个盖掉，什么也不说（v3.1.0 正式版前检查 C-2e）。
// 现在和名字不能用的一样：留前一个，后面的跳过，日志里列出来；不整包拒绝（服务端包常在 Linux 上打，那边这是两个文件）。
// 写法一模一样出现两次的照旧以后一个为准。
func TestImportServerArchiveCaseDuplicates(t *testing.T) {
	m, _ := importEnv(t)
	pack, err := modpack.Parse(zipOrdered(t, [][2]string{
		{"paper-1.20.1-100.jar", paperJarBytes(t)},
		{"server.properties", "server-port=25565\n"},
		{"eula.txt", "eula=true\n"},
		{"config/Dup.toml", "first"},
		{"config/dup.toml", "second"},
		{"CONFIG/DUP.TOML", "third"},
		{"config/same.toml", "a"},
		{"config/same.toml", "b"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "dups")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "重名", XmxMB: 2048, Port: 25575, EULA: true}, dir); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if got := dirNames(t, filepath.Join(dir, "config")); !slices.Equal(got, []string{"Dup.toml", "same.toml"}) {
		t.Errorf("config 里应是 Dup.toml 和 same.toml：%q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "Dup.toml")); string(b) != "first" {
		t.Errorf("只差大小写的应留前一个：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "same.toml")); string(b) != "b" {
		t.Errorf("写法一样的照旧以后一个为准：%q", b)
	}
	log := strings.Join(tk.Snapshot().Log, "\n")
	if !strings.Contains(log, "只差大小写") || !strings.Contains(log, "config/dup.toml") || !strings.Contains(log, "CONFIG/DUP.TOML") || strings.Contains(log, "config/same.toml") {
		t.Errorf("日志应列出只差大小写、没解压的那两个：%s", log)
	}
}

// 解压现成服务端之前按解开以后的总大小查磁盘余量（A40，同交接包导入）：装不下就一个文件都不写；
// 刚好装得下的照常导入；查不到余量的盘照做，日志里说一声。
func TestImportServerArchiveChecksDiskSpace(t *testing.T) {
	m, _ := importEnv(t)
	z := zipOf(t, map[string]string{
		"paper-1.20.1-100.jar":   paperJarBytes(t),
		"server.properties":      "server-port=25565\n",
		"eula.txt":               "eula=true\n",
		"world/region/r.0.0.mca": strings.Repeat("\x00", 4<<20),
	})
	zr, err := zip.OpenReader(z)
	if err != nil {
		t.Fatal(err)
	}
	need := diskguard.Reserve
	for _, f := range zr.File {
		need += int64(f.UncompressedSize64) + 4096
	}
	zr.Close()
	old := serverImportDiskFree
	t.Cleanup(func() { serverImportDiskFree = old })
	run := func(name string, free func(string) (int64, error)) (string, string, error) {
		pack, err := modpack.Parse(z)
		if err != nil {
			t.Fatal(err)
		}
		serverImportDiskFree = free
		dir := filepath.Join(app.ServersDir, name)
		tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
		err = m.runServerImport(context.Background(), tk, pack, CreateReq{Name: name, XmxMB: 2048, Port: 25576, EULA: true}, dir)
		return dir, strings.Join(tk.Snapshot().Log, "\n"), err
	}

	dir, _, err := run("差一点", func(string) (int64, error) { return need - 1, nil })
	var full *diskguard.NoSpace
	if !errors.As(err, &full) {
		t.Fatalf("差一个字节装不下也该在解压前停下：%v", err)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{CreatingMarker}) {
		t.Fatalf("装不下时一个文件都不该写：%q", got)
	}
	if _, _, err := run("刚好", func(string) (int64, error) { return need, nil }); err != nil {
		t.Fatalf("刚好装得下应照常导入：%v", err)
	}
	_, log, err := run("查不到", func(string) (int64, error) { return 0, errors.New("这块盘查不了") })
	if err != nil || !strings.Contains(log, "没查到存放位置所在的盘还剩多少空间") {
		t.Fatalf("查不到余量的盘照做，日志里说一声：%v\n%s", err, log)
	}
}

// 服务端 jar 里没写版本、压缩包的日志里有：导入用日志里的版本，任务日志和结果里注明是从日志里认的（A44）。
func TestImportServerArchiveVersionFromLog(t *testing.T) {
	m, _ := importEnv(t)
	bare := jarBytesOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n"})
	run := func(name string, files map[string]string) *Instance {
		t.Helper()
		pack, err := modpack.Parse(zipOf(t, files))
		if err != nil {
			t.Fatal(err)
		}
		tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
		if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: name, XmxMB: 2048, Port: 25578, EULA: true}, filepath.Join(app.ServersDir, name)); err != nil {
			t.Fatalf("%s 导入失败：%v\n%v", name, err, tk.Snapshot().Log)
		}
		w := warningOf(tk, "mc-from-log")
		fromLog := strings.Contains(strings.Join(tk.Snapshot().Log, "\n"), "是从压缩包里的 logs/latest.log 认出来的")
		if (w != nil) != fromLog {
			t.Fatalf("%s：日志和结果要一致：%+v %v", name, w, fromLog)
		}
		i, _ := m.Get(name)
		if fromLog != (name == "日志版本") {
			t.Fatalf("%s：从日志里认的说明不对：%v", name, fromLog)
		}
		return i
	}
	if i := run("日志版本", map[string]string{"paper.jar": bare, "eula.txt": "eula=true\n",
		"logs/latest.log": "[12:00:00] [Server thread/INFO]: Starting minecraft server version 1.20.1\n"}); i.MC != "1.20.1" || i.Core != "paper" {
		t.Fatalf("应按日志里的版本登记：%s %s", i.Core, i.MC)
	}
	if i := run("jar 版本", map[string]string{"paper-1.20.1-100.jar": paperJarBytes(t), "eula.txt": "eula=true\n",
		"logs/latest.log": "Starting minecraft server version 1.19.4\n"}); i.MC != "1.20.1" {
		t.Fatalf("jar 里写了版本的以 jar 为准：%s", i.MC)
	}
}
