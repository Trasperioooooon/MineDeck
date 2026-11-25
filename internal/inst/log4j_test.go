package inst

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// stubLog4jFetch 把 fetchLog4jXML 换成不联网的：fail 为 nil 时写一份假的配置进去，记下下载过哪些文件。
func stubLog4jFetch(t *testing.T, fail func() error) *[]string {
	t.Helper()
	old := fetchLog4jXML
	t.Cleanup(func() { fetchLog4jXML = old })
	var got []string
	fetchLog4jXML = func(_ context.Context, dir, name string) error {
		got = append(got, name)
		if fail != nil {
			if err := fail(); err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(dir, name), []byte("<Configuration/>"), 0o644)
	}
	return &got
}

// officialLog4jXMLFile 在 dir 里放一份指定名字（112-116 或 17-111）的官方日志配置，并把它的 SHA-1 临时当成官方的（测试里拿不到真文件）。
func officialLog4jXMLFile(t *testing.T, dir, name string) {
	t.Helper()
	body := []byte("<Configuration>" + name + "</Configuration>")
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(body)
	old := log4jXMLSHA1[name]
	t.Cleanup(func() { log4jXMLSHA1[name] = old })
	log4jXMLSHA1[name] = hex.EncodeToString(sum[:])
}

// officialLog4jXML 在 dir 里放一份 112-116，并把它的 SHA-1 临时当成官方的（测试里拿不到真文件）。
func officialLog4jXML(t *testing.T, dir string) {
	t.Helper()
	officialLog4jXMLFile(t, dir, log4jXML)
}

// 按 Mojang 的公告分段：1.18.1 起不加；1.17 ~ 1.18 只加参数；1.12 ~ 1.16.5 用 112-116；1.7 ~ 1.11.2 用 17-111；1.7 以前什么都不加。
// 清单里没有的版本判断不了。
func TestLog4jPlanEras(t *testing.T) {
	for _, c := range []struct {
		mc        string
		noLookups bool
		xml       string
		ok        bool
	}{
		{"1.21.1", false, "", true},
		{"1.18.1", false, "", true},
		{"1.18", true, "", true},
		{"1.17", true, "", true},
		{"1.16.5", true, log4jXML, true},
		{"1.12", true, log4jXML, true},
		{"1.11.2", true, log4jXML17, true},
		{"1.8.9", true, log4jXML17, true},
		{"1.7", true, log4jXML17, true},
		{"1.6.4", false, "", true},
		{"9.9-unknown", false, "", false},
	} {
		noLookups, xml, ok := log4jPlanCached(c.mc)
		if noLookups != c.noLookups || xml != c.xml || ok != c.ok {
			t.Errorf("%s：noLookups=%v xml=%q ok=%v，应为 %v %q %v", c.mc, noLookups, xml, ok, c.noLookups, c.xml, c.ok)
		}
	}
}

// 导入现成服务端（新建、接管、交接包走同一张分段表）：每段加什么参数、下哪份文件；下载不成不加引用，日志里说清楚。
func TestApplyImportLog4jEras(t *testing.T) {
	for _, c := range []struct {
		mc      string
		fail    bool
		want    []string
		fetched []string
	}{
		{"1.20.1", false, nil, nil},
		{"1.17.1", false, []string{log4jNoLookups}, nil},
		{"1.16.5", false, []string{log4jNoLookups, log4jConfig}, []string{log4jXML}},
		{"1.12.2", false, []string{log4jNoLookups, log4jConfig}, []string{log4jXML}},
		{"1.11.2", false, []string{log4jNoLookups, log4jConfig17}, []string{log4jXML17}},
		{"1.8.9", false, []string{log4jNoLookups, log4jConfig17}, []string{log4jXML17}},
		{"1.7.10", false, []string{log4jNoLookups, log4jConfig17}, []string{log4jXML17}},
		{"1.6.4", false, nil, nil},
		{"1.8.9", true, []string{log4jNoLookups}, []string{log4jXML17}},
	} {
		t.Run(fmt.Sprintf("%s-fail=%v", c.mc, c.fail), func(t *testing.T) {
			got := stubLog4jFetch(t, func() error {
				if c.fail {
					return errors.New("没联网")
				}
				return nil
			})
			i := &Instance{Settings: Settings{Name: "t", Core: mcsrc.CoreVanilla, MC: c.mc}, Dir: t.TempDir()}
			tk, _ := tasks.NewManager().New("导入", []string{"导入"})
			applyImportLog4j(context.Background(), tk, i)
			if !slices.Equal(i.ExtraJVM, c.want) || !slices.Equal(*got, c.fetched) {
				t.Fatalf("ExtraJVM=%q 下载了 %q，应为 %q、%q", i.ExtraJVM, *got, c.want, c.fetched)
			}
			log := strings.Join(tk.Snapshot().Log, "\n")
			if c.fail && !strings.Contains(log, "没下载成") {
				t.Errorf("下载不成要在任务日志里说：%s", log)
			}
		})
	}
}

// 保存 JVM 参数时强制保留的那几条也按新的分段：1.7 ~ 1.11.2 留 17-111（文件在时），1.7 以前不强加；判断不了版本时沿用已有的。
func TestLog4jFlagsEras(t *testing.T) {
	for _, c := range []struct {
		name    string
		mc      string
		files   []string
		initial []string
		want    []string
	}{
		{"1.8.9 文件在", "1.8.9", []string{log4jXML17}, nil, []string{log4jNoLookups, log4jConfig17}},
		{"1.8.9 文件不在不强加引用", "1.8.9", nil, nil, []string{log4jNoLookups}},
		{"1.8.9 只有旧文件也不强加旧的", "1.8.9", []string{log4jXML}, nil, []string{log4jNoLookups}},
		{"1.6.4 什么都不强加", "1.6.4", []string{log4jXML}, []string{log4jNoLookups, log4jConfig}, nil},
		{"判断不了版本时沿用已有的", "9.9-unknown", []string{log4jXML17}, []string{"-Xss1M", log4jNoLookups, log4jConfig17}, []string{log4jNoLookups, log4jConfig17}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range c.files {
				officialLog4jXMLFile(t, dir, f)
			}
			i := &Instance{Settings: Settings{Name: "t", Core: mcsrc.CorePaper, MC: c.mc, ExtraJVM: c.initial}, Dir: dir}
			if got, _ := log4jFlagsFor(i.Settings, i.Dir); !slices.Equal(got, c.want) {
				t.Fatalf("log4jFlagsFor = %q，应为 %q", got, c.want)
			}
		})
	}
}

// 已经建好的 1.7 ~ 1.11.2 服务器：引用着 112-116 那份的换成 17-111；别的参数原样、位置不变；instance.json、run.bat 跟着换；
// 换下来的旧文件留着不删（用户自己的启动脚本可能还引用着它，见 log4j_migrate.go 开头）。
func TestMigrateLog4j(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	dir := t.TempDir()
	officialLog4jXML(t, dir)
	i := &Instance{Settings: Settings{Name: "老服", Core: mcsrc.CoreVanilla, MC: "1.8.9", JavaPath: `C:\java\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
		LaunchTarget: "jar:server.jar", ExtraJVM: []string{"-Xss1M", log4jNoLookups, log4jConfig, "-XX:+UseG1GC"}}, Dir: dir, Console: newConsole()}
	m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	if note := log4jStartNote(i); !strings.Contains(note, log4jXML17) {
		t.Fatalf("还没换的，启动时要在控制台说：%q", note)
	}

	m.MigrateLog4j(context.Background())
	want := []string{"-Xss1M", log4jNoLookups, log4jConfig17, "-XX:+UseG1GC"}
	if !slices.Equal(i.ExtraJVM, want) || !slices.Equal(*fetched, []string{log4jXML17}) {
		t.Fatalf("ExtraJVM=%q 下载了 %q", i.ExtraJVM, *fetched)
	}
	saved, err := readSettings(dir)
	if err != nil || !slices.Equal(saved.ExtraJVM, want) {
		t.Fatalf("instance.json：%v %q", err, saved.ExtraJVM)
	}
	bat := mustRead(t, filepath.Join(dir, "run.bat"))
	if !strings.Contains(bat, log4jXML17) || strings.Contains(bat, log4jXML) {
		t.Fatalf("run.bat 要跟着换：\n%s", bat)
	}
	if _, err := os.Stat(filepath.Join(dir, log4jXML)); err != nil {
		t.Fatalf("换下来的旧文件应留着：%v", err)
	}
	lines := strings.Join(i.Console.Lines(), "\n")
	if !strings.Contains(lines, "换成了 Mojang 给 1.7 ~ 1.11.2 的日志配置") || strings.Contains(lines, "删掉") {
		t.Fatalf("控制台：%s", lines)
	}
	if log4jStartNote(i) != "" {
		t.Fatal("换过以后启动时不该再提示")
	}

	// 再过一遍什么都不动
	m.MigrateLog4j(context.Background())
	if len(*fetched) != 1 || !slices.Equal(i.ExtraJVM, want) {
		t.Fatalf("换过的不该再下载、再改：%q %q", *fetched, i.ExtraJVM)
	}
}

// 没联网：什么都不动（instance.json、run.bat、旧文件都原样），控制台说清楚；联网以后再过一遍就换上。
func TestMigrateLog4jOffline(t *testing.T) {
	offline := true
	stubLog4jFetch(t, func() error {
		if offline {
			return errors.New("dial tcp: 没联网")
		}
		return nil
	})
	dir := t.TempDir()
	officialLog4jXML(t, dir)
	args := []string{log4jNoLookups, log4jConfig, "-Dmy.flag=1"}
	i := &Instance{Settings: Settings{Name: "老服", Core: mcsrc.CoreForge, MC: "1.7.10", JavaPath: `C:\java\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
		LaunchTarget: "jar:forge-1.7.10.jar", ExtraJVM: slices.Clone(args)}, Dir: dir, Console: newConsole()}
	m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}
	if err := m.saveInstance(i); err != nil {
		t.Fatal(err)
	}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	beforeJSON := mustRead(t, filepath.Join(dir, "instance.json"))
	beforeBat := mustRead(t, filepath.Join(dir, "run.bat"))

	m.MigrateLog4j(context.Background())
	if !slices.Equal(i.ExtraJVM, args) || mustRead(t, filepath.Join(dir, "instance.json")) != beforeJSON || mustRead(t, filepath.Join(dir, "run.bat")) != beforeBat {
		t.Fatalf("没下载成不该动参数和文件：%q", i.ExtraJVM)
	}
	for _, f := range []string{log4jXML} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("旧文件还在用，不能删：%v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, log4jXML17)); !os.IsNotExist(err) {
		t.Fatalf("没下载成不该留下新文件：%v", err)
	}
	if lines := strings.Join(i.Console.Lines(), "\n"); !strings.Contains(lines, "没下载成") || !strings.Contains(lines, "没联网") {
		t.Fatalf("控制台要说清楚：%s", lines)
	}
	if log4jStartNote(i) == "" {
		t.Fatal("还没换成，启动时要提示")
	}

	offline = false
	m.MigrateLog4j(context.Background())
	if want := []string{log4jNoLookups, log4jConfig17, "-Dmy.flag=1"}; !slices.Equal(i.ExtraJVM, want) {
		t.Fatalf("联网以后应换上：%q", i.ExtraJVM)
	}
}

// 不该换的不动：1.12 ~ 1.16.5 本来就该用 112-116；1.7 以前和判断不了版本的不动参数；代理端不管。
// 旧文件一律留着：内容改过的、原来的启动脚本还引用着的、用户自己的 start.bat 引用着官方那份的（2026-11-18 Codex 复审的例子）。
func TestMigrateLog4jLeavesOthers(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}}
	add := func(name string, core mcsrc.Core, mc string, extra []string) *Instance {
		dir := t.TempDir()
		officialLog4jXML(t, dir)
		i := &Instance{Settings: Settings{Name: name, Core: core, MC: mc, LaunchTarget: "jar:server.jar", ExtraJVM: extra}, Dir: dir, Console: newConsole()}
		m.insts[name] = i
		return i
	}
	both := []string{log4jNoLookups, log4jConfig}
	keep := []*Instance{
		add("1.16.5", mcsrc.CorePaper, "1.16.5", slices.Clone(both)),
		add("1.12", mcsrc.CoreVanilla, "1.12", slices.Clone(both)),
		add("1.6.4", mcsrc.CoreVanilla, "1.6.4", slices.Clone(both)),
		add("不认识", mcsrc.CorePaper, "9.9-unknown", slices.Clone(both)),
	}
	// 1.8.9 已经是新配置了，旧文件被改过：不删
	edited := add("改过的", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig17})
	if err := os.WriteFile(filepath.Join(edited.Dir, log4jXML), []byte("<Configuration>自己改的</Configuration>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 1.8.9 已经是新配置了，接管前的启动脚本还引用着旧文件：不删
	referenced := add("脚本引用着", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig17})
	if err := os.WriteFile(filepath.Join(referenced.Dir, runBatBackup), []byte("java -Dlog4j.configurationFile=LOG4J2_112-116.XML -jar server.jar\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 接管来的 1.8.9 已经是新配置了，用户自己的 start.bat 还引用着官方那份旧文件：不删
	ownScript := add("自己的脚本", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig17})
	if err := os.WriteFile(filepath.Join(ownScript.Dir, "start.bat"), []byte("java -Dlog4j.configurationFile=log4j2_112-116.xml -jar server.jar\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m.MigrateLog4j(context.Background())
	if len(*fetched) != 0 {
		t.Fatalf("都不该下载：%q", *fetched)
	}
	for _, i := range append(keep, edited, referenced, ownScript) {
		if _, err := os.Stat(filepath.Join(i.Dir, log4jXML)); err != nil {
			t.Errorf("%s：旧文件不该删（%v）", i.Name, err)
		}
	}
	for _, i := range keep {
		if !slices.Equal(i.ExtraJVM, both) {
			t.Errorf("%s：参数不该动：%q", i.Name, i.ExtraJVM)
		}
		if _, err := os.Stat(filepath.Join(i.Dir, "instance.json")); !os.IsNotExist(err) {
			t.Errorf("%s：不该写 instance.json", i.Name)
		}
	}
}

// 启动时还没换成：控制台说一声、照旧用旧配置开（不等下载），后台再试；旧文件开着时、停了以后再过一遍都留着。
func TestMigrateLog4jOnStart(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeServerEnv, "1")
	release := make(chan struct{})
	stubLog4jFetch(t, func() error { <-release; return nil })
	dir := t.TempDir()
	officialLog4jXML(t, dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte(fmt.Sprintf("server-port=%d\n", port)), 0o644)
	i := &Instance{Settings: Settings{Name: "开着换", Core: mcsrc.CoreVanilla, MC: "1.8.9", JavaPath: exe, XmxMB: 64, XmsMB: 64,
		LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups, log4jConfig}}, Dir: dir, Console: newConsole()}
	m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}
	if err := m.Start(i.Name); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, i, "running") // 下载还卡着，启动照样完成
	lines := i.Console.Lines()
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) ||
		!slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") && strings.Contains(l, log4jConfig) }) {
		t.Fatalf("这次照旧用旧配置开，控制台要说一声：%q", lines)
	}
	close(release)
	waitConsole(t, i, "重启以后生效")
	if !slices.Contains(i.ExtraJVMSnapshot(), log4jConfig17) {
		t.Fatalf("后台应换上：%q", i.ExtraJVMSnapshot())
	}
	if _, err := os.Stat(filepath.Join(dir, log4jXML)); err != nil {
		t.Fatalf("服务器开着，旧文件留着：%v", err)
	}
	if err := m.Command(i.Name, "stop"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, i, "stopped")
	m.MigrateLog4j(context.Background())
	if _, err := os.Stat(filepath.Join(dir, log4jXML)); err != nil {
		t.Fatalf("停了以后再过一遍，旧文件也留着：%v", err)
	}
}

func waitConsole(t *testing.T, i *Instance, sub string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !slices.ContainsFunc(i.Console.Lines(), func(l string) bool { return strings.Contains(l, sub) }) {
		if time.Now().After(deadline) {
			t.Fatalf("控制台里等不到 %q：%q", sub, i.Console.Lines())
		}
		time.Sleep(20 * time.Millisecond)
	}
	for { // 等后台那一次整个做完（解开占位）
		if _, busy := log4jMigrating.Load(i); !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("后台换配置一直没结束")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 整合包导入收尾：引用着 17-111 的也要确认那份还是官方的。
func TestRestoreLog4jXML17(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	i := &Instance{Settings: Settings{Name: "t", MC: "1.8.9", ExtraJVM: []string{log4jNoLookups, log4jConfig17}}, Dir: t.TempDir()}
	tk, _ := tasks.NewManager().New("导入", []string{"导入"})
	restoreLog4jXML(context.Background(), tk, i)
	if !slices.Equal(*fetched, []string{log4jXML17}) {
		t.Fatalf("下载了 %q", *fetched)
	}
}

// 真的下载那一段（本机假服务器）：按地址里的 SHA-1 校验；文件对得上不重新下载，被换掉了重新下载；
// 服务器给的内容对不上时报错，原来的文件不动。
func TestFetchLog4jXMLVerifies(t *testing.T) {
	body := []byte("<Configuration>17-111</Configuration>" + strings.Repeat(" ", 100))
	sum := sha1.Sum(body)
	hexSum := hex.EncodeToString(sum[:])
	old := log4jXMLSHA1[log4jXML17]
	t.Cleanup(func() { log4jXMLSHA1[log4jXML17] = old })
	log4jXMLSHA1[log4jXML17] = hexSum
	var hits atomic.Int32
	var serve atomic.Pointer[[]byte]
	serve.Store(&body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/"+hexSum+"/"+log4jXML17 {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(*serve.Load())
	}))
	defer srv.Close()
	oldBase := log4jObjects
	t.Cleanup(func() { log4jObjects = oldBase })
	log4jObjects = srv.URL + "/"

	dir := t.TempDir()
	path := filepath.Join(dir, log4jXML17)
	ctx := context.Background()
	if err := fetchLog4jXML(ctx, dir, log4jXML17); err != nil || mustRead(t, path) != string(body) || hits.Load() != 1 {
		t.Fatalf("第一次下载：%v hits=%d", err, hits.Load())
	}
	if err := fetchLog4jXML(ctx, dir, log4jXML17); err != nil || hits.Load() != 1 {
		t.Fatalf("对得上不该重新下载：%v hits=%d", err, hits.Load())
	}
	if err := os.WriteFile(path, []byte("<Configuration>别人换的</Configuration>"+strings.Repeat(" ", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fetchLog4jXML(ctx, dir, log4jXML17); err != nil || mustRead(t, path) != string(body) || hits.Load() != 2 {
		t.Fatalf("被换掉了应重新下载：%v hits=%d", err, hits.Load())
	}

	bad := []byte("<Configuration>坏的</Configuration>" + strings.Repeat(" ", 100))
	serve.Store(&bad)
	if err := os.WriteFile(path, []byte("原来的"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fetchLog4jXML(ctx, dir, log4jXML17); err == nil || mustRead(t, path) != "原来的" {
		t.Fatalf("内容对不上应报错、不动原来的文件：%v %q", err, mustRead(t, path))
	}
}

// 实例目录里已经有官方那份日志配置时，启动迁移（补引用和换引用两条路）直接复用盘上文件，不发网络请求；
// 即使离线（下载源全回 404）也能把缺失或错配的引用修好。
func TestMigrateLog4jSkipsDownloadWhenOfficialOnDisk(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	oldBase := log4jObjects
	t.Cleanup(func() { log4jObjects = oldBase })
	log4jObjects = srv.URL + "/"

	dirAdd := t.TempDir()
	officialLog4jXMLFile(t, dirAdd, log4jXML)
	instAdd := &Instance{
		Settings: Settings{
			Name: "离线补引用", Core: mcsrc.CoreVanilla, MC: "1.12.2",
			JavaPath: `jdk\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
			LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups},
		},
		Dir: dirAdd, Console: newConsole(),
	}

	dirSwap := t.TempDir()
	officialLog4jXMLFile(t, dirSwap, log4jXML)
	officialLog4jXMLFile(t, dirSwap, log4jXML17)
	instSwap := &Instance{
		Settings: Settings{
			Name: "离线换引用", Core: mcsrc.CoreVanilla, MC: "1.8.9",
			JavaPath: `jdk\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
			LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups, log4jConfig},
		},
		Dir: dirSwap, Console: newConsole(),
	}

	m := &Manager{
		insts:    map[string]*Instance{instAdd.Name: instAdd, instSwap.Name: instSwap},
		creating: map[string]bool{},
	}
	for _, i := range []*Instance{instAdd, instSwap} {
		if err := m.saveInstance(i); err != nil {
			t.Fatal(err)
		}
		if err := writeRunBat(i); err != nil {
			t.Fatal(err)
		}
	}

	m.MigrateLog4j(context.Background())

	if got := hits.Load(); got != 0 {
		t.Fatalf("官方配置文件已在盘上时不应发 HTTP 请求，实际请求了 %d 次", got)
	}
	if want := []string{log4jNoLookups, log4jConfig}; !slices.Equal(instAdd.ExtraJVM, want) {
		t.Fatalf("1.12.2 应补上引用：got=%q want=%q（console=%s）", instAdd.ExtraJVM, want, strings.Join(instAdd.Console.Lines(), "\n"))
	}
	if want := []string{log4jNoLookups, log4jConfig17}; !slices.Equal(instSwap.ExtraJVM, want) {
		t.Fatalf("1.8.9 应换上 17-111 引用：got=%q want=%q（console=%s）", instSwap.ExtraJVM, want, strings.Join(instSwap.Console.Lines(), "\n"))
	}
	for _, tc := range []struct {
		inst *Instance
		want string
	}{
		{instAdd, log4jConfig},
		{instSwap, log4jConfig17},
	} {
		saved, err := readSettings(tc.inst.Dir)
		if err != nil || !slices.Contains(saved.ExtraJVM, tc.want) {
			t.Fatalf("%s 的 instance.json 未写入 %s：%v %q", tc.inst.Name, tc.want, err, saved.ExtraJVM)
		}
		if bat := mustRead(t, filepath.Join(tc.inst.Dir, "run.bat")); !strings.Contains(bat, tc.want) {
			t.Fatalf("%s 的 run.bat 未写入 %s：\n%s", tc.inst.Name, tc.want, bat)
		}
		if con := strings.Join(tc.inst.Console.Lines(), "\n"); strings.Contains(con, "⚠") {
			t.Fatalf("%s 控制台不应报迁移告警：\n%s", tc.inst.Name, con)
		}
	}
}
