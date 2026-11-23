package inst

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// A08：加载服务器时单独登记的（接管进来的）优先于存放位置扫描；存放位置里同名的不进列表、进 SkippedDuplicates；
// 存放位置内部两台重名的仍按先到先得，被跳过的同样进 SkippedDuplicates。
func TestAdoptedWinsOverRootDuplicates(t *testing.T) {
	_, user := adoptEnv(t)
	root2 := filepath.Join(user, "root2")
	if _, err := app.UpdateConfig(func(c *app.Config) error {
		c.Roots = []string{root2}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	adoptedDir := filepath.Join(user, "my-adopted-survival")
	inDefaultRoot := filepath.Join(app.ServersDir, "survival-on-usb")
	dupFirst := filepath.Join(app.ServersDir, "creative-a")
	dupSecond := filepath.Join(root2, "creative-b")

	writeTree(t, adoptedDir, map[string]string{
		"instance.json": `{"name":"生存服","core":"paper","mc":"1.21.4","launchTarget":"jar:server.jar","adopted":true}`,
	})
	writeTree(t, inDefaultRoot, map[string]string{
		"instance.json": `{"name":"生存服","core":"vanilla","mc":"1.20.1","launchTarget":"jar:server.jar"}`,
	})
	writeTree(t, dupFirst, map[string]string{
		"instance.json": `{"name":"创造服","core":"fabric","mc":"1.21.1","launchTarget":"jar:server.jar"}`,
	})
	writeTree(t, dupSecond, map[string]string{
		"instance.json": `{"name":"创造服","core":"forge","mc":"1.20.1","launchTarget":"jar:server.jar"}`,
	})
	app.RememberInstanceDir(adoptedDir)

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.ShutdownAll(time.Second) })

	surv, err := m.Get("生存服")
	if err != nil {
		t.Fatalf("列表里应有生存服：%v", err)
	}
	if surv.Dir != adoptedDir || !surv.Adopted || surv.Core != mcsrc.CorePaper {
		t.Fatalf("同名时应保留接管进来的那台（%s），实际拿到 dir=%s adopted=%v core=%s", adoptedDir, surv.Dir, surv.Adopted, surv.Core)
	}

	cre, err := m.Get("创造服")
	if err != nil {
		t.Fatalf("列表里应有创造服：%v", err)
	}
	if cre.Dir != dupFirst {
		t.Fatalf("存放根之间重名应先到先得保留 %s，实际 %s", dupFirst, cre.Dir)
	}

	dups := m.SkippedDuplicates()
	want := []SkippedDuplicate{
		{Name: "生存服", Dir: inDefaultRoot, KeptDir: adoptedDir},
		{Name: "创造服", Dir: dupSecond, KeptDir: dupFirst},
	}
	if !slices.Equal(dups, want) {
		t.Fatalf("SkippedDuplicates = %+v，期望 %+v", dups, want)
	}

	// 用户把重名文件夹里的名字改掉后，提示应自动消失
	writeTree(t, inDefaultRoot, map[string]string{
		"instance.json": `{"name":"生存服 2","core":"vanilla","mc":"1.20.1","launchTarget":"jar:server.jar"}`,
	})
	dupsAfter := m.SkippedDuplicates()
	wantAfter := []SkippedDuplicate{{Name: "创造服", Dir: dupSecond, KeptDir: dupFirst}}
	if !slices.Equal(dupsAfter, wantAfter) {
		t.Fatalf("改掉重名后 SkippedDuplicates = %+v，期望 %+v", dupsAfter, wantAfter)
	}
}

// A09：核实并守住删除与所有启动入口（Start / startInstance / 崩溃自动重启 / 定时重启）之间的互斥：
//  1. 删除占位（BeginWorldChangeInstance("删除")）期间，任何启动入口一律报「正在删除」，起不了进程、也不落盘 start-fail.json；
//  2. 启动准备占位（UseWorldInstance("准备启动")，含放开 procMu 解析域名期间）期间，Delete 一律报「正在准备启动」，不动目录；
//  3. 多轮高并发反复启动 / 强杀 / 崩溃重启与删除交错（覆盖 files=true 和 files=false），删完后进程必不在跑、目录与登记状态一致。
func TestConcurrentStartAndDelete(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	oldDelays := crashRestartDelays
	t.Cleanup(func() { crashRestartDelays = oldDelays })
	crashRestartDelays = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}

	t.Run("删除进行中所有启动入口一律被拦", func(t *testing.T) {
		_, _ = adoptEnv(t)
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ln, port := occupy(t, "127.0.0.1")
		_ = ln.Close()
		dir := filepath.Join(app.ServersDir, "del-window")
		writeTree(t, dir, map[string]string{
			"instance.json":     fmt.Sprintf(`{"name":"删中禁启","core":"vanilla","mc":"1.21.4","javaMajor":21,"javaPath":%q,"xmxMb":64,"xmsMb":64,"launchTarget":"jar:server.jar"}`, exe),
			"server.properties": fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port),
		})
		m, err := NewManager()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.ShutdownAll(time.Second) })
		i, err := m.Get("删中禁启")
		if err != nil {
			t.Fatal(err)
		}
		i.Policies.CrashRestart = true
		i.startedAt = time.Now()

		inDelete := make(chan struct{})
		releaseDelete := make(chan struct{})
		oldHook := deleteHook
		t.Cleanup(func() { deleteHook = oldHook })
		deleteHook = func() {
			close(inDelete)
			<-releaseDelete
		}

		delDone := make(chan error, 1)
		go func() {
			delDone <- m.Delete("删中禁启", true)
		}()

		<-inDelete
		if err := m.Start("删中禁启"); err == nil || !strings.Contains(err.Error(), "正在删除") {
			t.Fatalf("删除进行中 m.Start 应报正在删除，实际：%v", err)
		}
		if err := m.Restart("删中禁启"); err == nil {
			t.Fatalf("删除进行中 m.Restart 应被拦下")
		}
		if err := m.startInstance(i); err == nil || !strings.Contains(err.Error(), "正在删除") {
			t.Fatalf("删除进行中直接调 m.startInstance 应报正在删除，实际：%v", err)
		}
		var autoErrs []error
		var autoMu sync.Mutex
		m.SetAutoHook(func(a AutoAction) {
			autoMu.Lock()
			autoErrs = append(autoErrs, a.Err)
			autoMu.Unlock()
		})
		m.crashRestart(i, 0)
		// 定时重启在服务器停下以前在 policy.go:349 直接返回；即使停服后走到 policy.go:370，调的也是上面测过的 m.Start
		m.execSchedule(i, Schedule{Type: "restart", At: "04:00"})

		close(releaseDelete)
		if err := <-delDone; err != nil {
			t.Fatalf("Delete 应成功：%v", err)
		}
		if i.Status() != "stopped" || i.PID() != 0 {
			t.Fatalf("Delete 完成后不应有进程在跑：status=%s pid=%d", i.Status(), i.PID())
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("files=true 删除完成后目录不应残留：%v", err)
		}
		autoMu.Lock()
		defer autoMu.Unlock()
		if len(autoErrs) == 0 {
			t.Fatalf("崩溃重启应记录失败：%v", autoErrs)
		}
		for _, e := range autoErrs {
			if e == nil || !strings.Contains(e.Error(), "正在删除") {
				t.Errorf("自动重启在删除窗口内应报正在删除，实际：%v", e)
			}
		}
	})

	t.Run("启动准备放开procMu期间Delete一律被拦", func(t *testing.T) {
		_, _ = adoptEnv(t)
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		oldLookup := lookupIPAddr
		t.Cleanup(func() { lookupIPAddr = oldLookup })
		inLookup := make(chan struct{})
		releaseLookup := make(chan struct{})
		var once sync.Once
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			if host == "hold-start.invalid" {
				once.Do(func() { close(inLookup) })
				<-releaseLookup
				return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
			}
			return net.DefaultResolver.LookupIPAddr(ctx, host)
		}

		ln, port := occupy(t, "127.0.0.1")
		_ = ln.Close()
		dir := filepath.Join(app.ServersDir, "start-window")
		writeTree(t, dir, map[string]string{
			"instance.json":     fmt.Sprintf(`{"name":"启中禁删","core":"vanilla","mc":"1.21.4","javaMajor":21,"javaPath":%q,"xmxMb":64,"xmsMb":64,"launchTarget":"jar:server.jar"}`, exe),
			"server.properties": fmt.Sprintf("server-ip=hold-start.invalid\nserver-port=%d\n", port),
		})
		m, err := NewManager()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.ShutdownAll(time.Second) })
		i, _ := m.Get("启中禁删")

		startDone := make(chan error, 1)
		go func() { startDone <- m.Start("启中禁删") }()
		<-inLookup

		for _, files := range []bool{false, true} {
			if err := m.Delete("启中禁删", files); err == nil || !strings.Contains(err.Error(), "准备启动") {
				t.Fatalf("启动准备期间 Delete(files=%v) 应被占位拦住，实际：%v", files, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "instance.json")); err != nil {
			t.Fatalf("被拦下的 Delete 不应动 instance.json：%v", err)
		}

		close(releaseLookup)
		if err := <-startDone; err != nil {
			t.Fatalf("放行后 Start 应成功：%v", err)
		}
		waitStatus(t, i, "running")
		_ = m.Kill("启中禁删")
		waitStatus(t, i, "stopped")
	})

	t.Run("多轮并发启动与删除交错", func(t *testing.T) {
		_, _ = adoptEnv(t)
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		m, err := NewManager()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.ShutdownAll(time.Second) })

		for round := 0; round < 30; round++ {
			files := round%2 == 0
			name := fmt.Sprintf("并发服-%d", round)
			dir := filepath.Join(app.ServersDir, fmt.Sprintf("r-%d", round))
			ln, port := occupy(t, "127.0.0.1")
			_ = ln.Close()
			writeTree(t, dir, map[string]string{
				"instance.json":     fmt.Sprintf(`{"name":%q,"core":"vanilla","mc":"1.21.4","javaMajor":21,"javaPath":%q,"xmxMb":64,"xmsMb":64,"launchTarget":"jar:server.jar"}`, name, exe),
				"server.properties": fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port),
			})
			s, err := readSettings(dir)
			if err != nil {
				t.Fatal(err)
			}
			inst := &Instance{Settings: s, Dir: dir, Console: newConsole()}
			inst.Policies.CrashRestart = true
			m.mu.Lock()
			m.insts[name] = inst
			m.mu.Unlock()

			stopWorkers := make(chan struct{})
			var wg sync.WaitGroup
			for w := 0; w < 3; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for {
						select {
						case <-stopWorkers:
							return
						default:
						}
						_ = m.Start(name)
						if id == 0 {
							_ = m.Kill(name)
						} else if id == 1 {
							_ = m.Command(name, "stop")
						} else {
							m.crashRestart(inst, 0)
						}
					}
				}(w)
			}

			var deleted bool
			raceEnd := time.Now().Add(25 * time.Millisecond)
			for time.Now().Before(raceEnd) {
				if err := m.Delete(name, files); err == nil {
					deleted = true
					break
				}
				_ = m.Kill(name)
				time.Sleep(time.Millisecond)
			}
			close(stopWorkers)
			wg.Wait()
			if !deleted {
				if inst.Status() != "stopped" {
					_ = m.Kill(name)
					waitStatus(t, inst, "stopped")
				}
				if err := m.Delete(name, files); err != nil {
					t.Fatalf("第 %d 轮（files=%v）并发结束后删除失败：%v", round, files, err)
				}
			}
			if inst.Status() != "stopped" || inst.PID() != 0 {
				t.Fatalf("第 %d 轮删除成功后进程仍在运行：status=%s pid=%d", round, inst.Status(), inst.PID())
			}
			if _, err := m.Get(name); err == nil {
				t.Fatalf("第 %d 轮删除成功后仍在列表里", round)
			}
			if err := m.Start(name); err == nil {
				t.Fatalf("第 %d 轮删除成功后 Start 居然成功", round)
			}
			if files {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("第 %d 轮 files=true 删除后目录仍在：%v", round, err)
				}
			} else {
				if _, err := os.Stat(filepath.Join(dir, "instance.json")); !os.IsNotExist(err) {
					t.Fatalf("第 %d 轮移出列表后 instance.json 仍在：%v", round, err)
				}
				if _, err := os.Stat(filepath.Join(dir, settingsBackup)); err != nil {
					t.Fatalf("第 %d 轮移出列表后应有 instance.json.bak：%v", round, err)
				}
			}
		}
	})
}

// A25 第 1 点：迁移下载官方配置期间，用户在实例目录保存了自己的同名日志配置，下载完写入前再看一次文件：
// 已存在且不是官方那份就不覆盖、不加引用，并在控制台说明。
func TestMigrateLog4jDoesNotOverwriteCustomXMLSavedDuringDownload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mc      string
		xml     string
		initial []string
	}{
		{"补配置期间保存自定义112-116", "1.12.2", log4jXML, []string{log4jNoLookups}},
		{"换配置期间保存自定义17-111", "1.8.9", log4jXML17, []string{log4jNoLookups, log4jConfig}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.mc == "1.8.9" {
				officialLog4jXML(t, dir)
			}
			customBody := "<Configuration><!-- 用户在迁移下载那两分钟里自己保存的配置 --></Configuration>"
			oldFetch := fetchLog4jXML
			t.Cleanup(func() { fetchLog4jXML = oldFetch })
			fetchLog4jXML = func(_ context.Context, destDir, name string) error {
				// 模拟下载进行中，用户往实例目录里保存了自己的同名日志配置
				if err := os.WriteFile(filepath.Join(dir, name), []byte(customBody), 0o644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(destDir, name), []byte("<Configuration>official-downloaded</Configuration>"), 0o644)
			}

			i := &Instance{
				Settings: Settings{
					Name: "迁移竞态服", Core: mcsrc.CoreVanilla, MC: tc.mc,
					JavaPath: `jdk\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
					LaunchTarget: "jar:server.jar", ExtraJVM: slices.Clone(tc.initial),
				},
				Dir: dir, Console: newConsole(),
			}
			m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}
			if err := m.saveInstance(i); err != nil {
				t.Fatal(err)
			}
			if err := writeRunBat(i); err != nil {
				t.Fatal(err)
			}

			m.MigrateLog4j(context.Background())

			gotDisk := mustRead(t, filepath.Join(dir, tc.xml))
			if gotDisk != customBody {
				t.Fatalf("用户在下载期间保存的自定义日志配置被覆盖成了：%q", gotDisk)
			}
			if !slices.Equal(i.ExtraJVM, tc.initial) {
				t.Fatalf("不应改写启动参数：got=%q want=%q", i.ExtraJVM, tc.initial)
			}
			saved, err := readSettings(dir)
			if err != nil || !slices.Equal(saved.ExtraJVM, tc.initial) {
				t.Fatalf("instance.json 不应加上官方配置引用：%v %q", err, saved.ExtraJVM)
			}
			con := strings.Join(i.Console.Lines(), "\n")
			if !strings.Contains(con, "没有覆盖它") {
				t.Fatalf("控制台应说明检测到同名自定义文件未覆盖：\n%s", con)
			}
		})
	}
}

// A25 第 2 点 + A28：保存和预览启动参数共用同一个补全函数；只有磁盘上的日志配置文件确实是官方那份时才自动补引用，
// 是自定义内容时不加引用并在结果里说明。
func TestSaveAndPreviewSettingsOnlyReferenceOfficialLog4jXML(t *testing.T) {
	t.Run("同名文件不是官方内容时不强加引用并给出说明", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, log4jXML), []byte("<Configuration>custom</Configuration>"), 0o644); err != nil {
			t.Fatal(err)
		}
		i := &Instance{
			Settings: Settings{
				Name: "自定义日志服", Core: mcsrc.CoreVanilla, MC: "1.16.5",
				JavaPath: `jdk\bin\java.exe`, XmxMB: 2048, XmsMB: 1024,
				LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups},
			},
			Dir: dir, Console: newConsole(),
		}
		m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}

		input := []string{"-XX:+UseG1GC"}
		prev, err := m.PreviewSettings(i.Name, 4096, 0, "", &input)
		if err != nil {
			t.Fatal(err)
		}
		savedRes, err := m.UpdateSettingsWithResult(i.Name, 4096, 0, "", &input)
		if err != nil {
			t.Fatal(err)
		}
		wantJVM := []string{"-XX:+UseG1GC", log4jNoLookups}
		if !slices.Equal(prev.ExtraJVM, wantJVM) || !slices.Equal(savedRes.ExtraJVM, wantJVM) {
			t.Fatalf("非官方 XML 不应强加引用：preview=%q saved=%q want=%q", prev.ExtraJVM, savedRes.ExtraJVM, wantJVM)
		}
		if !slices.Equal(prev.LaunchArgs, savedRes.LaunchArgs) {
			t.Fatalf("预览与保存的完整启动参数应完全一致：\npreview=%q\nsaved=%q", prev.LaunchArgs, savedRes.LaunchArgs)
		}
		if prev.Note == "" || prev.Note != savedRes.Note || !strings.Contains(savedRes.Note, log4jXML) || !strings.Contains(savedRes.Note, "不是 Mojang 官方那份") {
			t.Fatalf("结果里应说明没有自动加上引用：preview=%q saved=%q", prev.Note, savedRes.Note)
		}
		bat := mustRead(t, filepath.Join(dir, "run.bat"))
		if strings.Contains(bat, log4jConfigKey) {
			t.Fatalf("run.bat 里不应出现官方日志配置引用：\n%s", bat)
		}
	})

	t.Run("同名文件是官方内容时预览与保存都自动补齐引用", func(t *testing.T) {
		dir := t.TempDir()
		officialLog4jXML(t, dir)
		i := &Instance{
			Settings: Settings{
				Name: "官方日志服", Core: mcsrc.CoreVanilla, MC: "1.16.5",
				JavaPath: `jdk\bin\java.exe`, XmxMB: 2048, XmsMB: 1024,
				LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups, log4jConfig},
			},
			Dir: dir, Console: newConsole(),
		}
		m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}

		input := []string{"-XX:+UseG1GC"}
		prev, err := m.PreviewSettings(i.Name, 0, 0, "", &input)
		if err != nil {
			t.Fatal(err)
		}
		savedRes, err := m.UpdateSettingsWithResult(i.Name, 0, 0, "", &input)
		if err != nil {
			t.Fatal(err)
		}
		wantJVM := []string{"-XX:+UseG1GC", log4jNoLookups, log4jConfig}
		if !slices.Equal(prev.ExtraJVM, wantJVM) || !slices.Equal(savedRes.ExtraJVM, wantJVM) {
			t.Fatalf("官方 XML 应补回引用：preview=%q saved=%q want=%q", prev.ExtraJVM, savedRes.ExtraJVM, wantJVM)
		}
		if prev.Note != "" || savedRes.Note != "" {
			t.Fatalf("官方 XML 不应有冲突说明：preview=%q saved=%q", prev.Note, savedRes.Note)
		}
		if !slices.Equal(prev.LaunchArgs, savedRes.LaunchArgs) {
			t.Fatalf("预览与保存的完整启动参数不一致：%q vs %q", prev.LaunchArgs, savedRes.LaunchArgs)
		}
	})
}

// A26：接管时文件夹里已有同名、内容不是官方的日志配置，不替换、不下载、不加引用（含旧 instance.json.bak 里带过引用的情况），
// 并在接管结果（告警与日志）和实例控制台里说明「这台服务器自带了自己的日志配置，MineDeck 没有替换，log4shell 防护要靠它自己」；
// 官方那份或没有这个文件的照旧。
func TestAdoptPreservesCustomLog4jXMLWithoutReference(t *testing.T) {
	t.Run("已有非官方日志配置时不替换不加引用并写入说明", func(t *testing.T) {
		fetched := stubLog4jFetch(t, nil)
		m, user := adoptEnv(t)
		dir := filepath.Join(user, "custom-112")
		customXML := "<Configuration><!-- 服主自己的日志配置 --></Configuration>"
		prevBak, _ := json.Marshal(Settings{
			Name: "旧记录", Core: mcsrc.CoreVanilla, MC: "1.12.2", LaunchTarget: "jar:server.jar",
			ExtraJVM: []string{"-XX:+UseG1GC", log4jNoLookups, log4jConfig},
		})
		writeTree(t, dir, map[string]string{
			"server.jar":        vanilla21,
			"eula.txt":          "eula=true\n",
			"server.properties": "server-port=25591\n",
			log4jXML:            customXML,
			settingsBackup:      string(prevBak),
		})

		taskID, err := m.AdoptAsync(AdoptReq{
			Dir: dir, Name: "自带日志的老服", Core: mcsrc.CoreVanilla, MC: "1.12.2",
			LaunchTarget: "jar:server.jar", JavaMajor: 8, JavaPath: fakeJava(t),
		})
		if err != nil {
			t.Fatal(err)
		}
		snap := waitAdoptTask(t, m, taskID)
		if snap.Err != "" {
			t.Fatalf("接管应成功，实际报错：%s", snap.Err)
		}
		if len(*fetched) != 0 {
			t.Fatalf("已有非官方同名文件时不应下载替换：%v", *fetched)
		}
		if got := mustRead(t, filepath.Join(dir, log4jXML)); got != customXML {
			t.Fatalf("用户的日志配置被改了：%q", got)
		}

		i, err := m.Get("自带日志的老服")
		if err != nil {
			t.Fatal(err)
		}
		wantJVM := []string{"-XX:+UseG1GC", log4jNoLookups}
		if !slices.Equal(i.ExtraJVM, wantJVM) {
			t.Fatalf("ExtraJVM 不应带 -Dlog4j.configurationFile 引用：%q（期望 %q）", i.ExtraJVM, wantJVM)
		}
		bat := mustRead(t, filepath.Join(dir, "run.bat"))
		if strings.Contains(bat, log4jConfigKey) {
			t.Fatalf("run.bat 不应带 -Dlog4j.configurationFile 引用：\n%s", bat)
		}

		wantMsg := "这台服务器自带了自己的日志配置，MineDeck 没有替换，log4shell 防护要靠它自己"
		if !strings.Contains(strings.Join(snap.Log, "\n"), wantMsg) {
			t.Errorf("接管任务日志里缺少说明：\n%s", strings.Join(snap.Log, "\n"))
		}
		foundWarn := false
		for _, w := range snap.Warnings {
			if strings.Contains(w.Title, wantMsg) || strings.Contains(w.Note, wantMsg) {
				foundWarn = true
				break
			}
		}
		if !foundWarn {
			t.Errorf("接管任务结果告警里缺少说明：%+v", snap.Warnings)
		}
		con := strings.Join(i.Console.Lines(), "\n")
		if !strings.Contains(con, wantMsg) {
			t.Errorf("实例控制台里缺少说明：\n%s", con)
		}
	})

	t.Run("没有同名文件或本身就是官方那份时照旧加上引用", func(t *testing.T) {
		fetched := stubLog4jFetch(t, nil)
		m, user := adoptEnv(t)
		dirNoFile := filepath.Join(user, "no-xml-112")
		writeTree(t, dirNoFile, map[string]string{
			"server.jar":        vanilla21,
			"eula.txt":          "eula=true\n",
			"server.properties": "server-port=25592\n",
		})
		if out, err := adopt(t, m, AdoptReq{
			Dir: dirNoFile, Name: "缺配置老服", Core: mcsrc.CoreVanilla, MC: "1.12.2",
			LaunchTarget: "jar:server.jar", JavaMajor: 8, JavaPath: fakeJava(t),
		}); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		i1, _ := m.Get("缺配置老服")
		if !slices.Equal(i1.ExtraJVM, []string{log4jNoLookups, log4jConfig}) || !slices.Equal(*fetched, []string{log4jXML}) {
			t.Fatalf("没有同名文件时应照旧下载并加引用：ExtraJVM=%q fetched=%q", i1.ExtraJVM, *fetched)
		}

		dirOfficial := filepath.Join(user, "official-xml-112")
		writeTree(t, dirOfficial, map[string]string{
			"server.jar":        vanilla21,
			"eula.txt":          "eula=true\n",
			"server.properties": "server-port=25593\n",
		})
		officialLog4jXML(t, dirOfficial)
		if out, err := adopt(t, m, AdoptReq{
			Dir: dirOfficial, Name: "官方配置老服", Core: mcsrc.CoreVanilla, MC: "1.12.2",
			LaunchTarget: "jar:server.jar", JavaMajor: 8, JavaPath: fakeJava(t),
		}); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		i2, _ := m.Get("官方配置老服")
		if !slices.Equal(i2.ExtraJVM, []string{log4jNoLookups, log4jConfig}) {
			t.Fatalf("官方那份文件在时应照旧加引用：ExtraJVM=%q", i2.ExtraJVM)
		}
	})
}

func waitAdoptTask(t *testing.T, m *Manager, id string) (snap struct {
	Err      string
	Log      []string
	Warnings []struct{ Kind, Title, Note string }
}) {
	t.Helper()
	tk := m.Tasks.Get(id)
	if tk == nil {
		t.Fatalf("找不到任务 %s", id)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := tk.Snapshot()
		if s.Ended {
			snap.Err = s.Err
			snap.Log = s.Log
			for _, w := range s.Warnings {
				snap.Warnings = append(snap.Warnings, struct{ Kind, Title, Note string }{w.Kind, w.Title, w.Note})
			}
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待任务 %s 结束超时", id)
	return snap
}
