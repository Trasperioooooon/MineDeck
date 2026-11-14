package inst

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
)

// 暂停在核心已准备、整合包覆盖未完成的实际发布边界，所有公开实例操作都必须看不到半成品。
func TestCreatePublishesOnlyAfterModpackFinishes(t *testing.T) {
	m, root := importEnv(t)
	req := CreateReq{Name: "初始化中", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root}
	dir, err := m.validateCreate(&req)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, map[string]string{"server.properties": "level-name=world\n", "server.jar": "core"})
	i := &Instance{Settings: Settings{Name: req.Name, Core: req.Core, MC: req.MC, JavaPath: testJava17, JavaMajor: 17, LaunchTarget: "jar:server.jar", CreatedAt: time.Now()}, Dir: dir, Console: newConsole()}
	task, ctx := m.Tasks.New("导入", append(append([]string{}, createSteps...), "下载", "覆盖"))
	task.EnableCancel()
	pack := &modpack.Pack{ZipPath: zipOrdered(t, [][2]string{{"overrides/world/level.dat", "PACK"}, {"overrides/config/a.toml", "complete"}}), OverrideDirs: []string{"overrides"}, Pending: []modpack.PendingFile{{Path: "mods/later.jar", Reason: modpack.ReasonNoHash}}}
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	t.Cleanup(resume)
	done := make(chan error, 1)
	go func() {
		done <- m.completeCreate(ctx, task, i, func(i *Instance) error {
			close(entered)
			<-proceed
			return m.applyModpack(ctx, task, pack, req, i, "")
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("没有到发布边界")
	}
	if _, err := m.Get(req.Name); err == nil || len(m.List()) != 0 {
		t.Fatal("半成品实例不应可见")
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); !os.IsNotExist(err) {
		t.Fatalf("不应过早写 instance.json: %v", err)
	}
	for name, run := range map[string]func() error{
		"启动": func() error { return m.Start(req.Name) },
		"设置": func() error { return m.UpdateSettings(req.Name, 1024, 512, "", nil) },
		"换图": func() error {
			release, err := m.BeginWorldChange(req.Name, "换图")
			if err == nil {
				release()
			}
			return err
		},
		"删除": func() error { return m.Delete(req.Name, true) },
	} {
		if err := run(); err == nil {
			t.Errorf("%s不应成功", name)
		}
	}
	resume()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, err := m.Get(req.Name); err != nil || got != i {
		t.Fatalf("完成后应发布同一实例: %v", err)
	}
	if got := readLevel(t, dir); got != "PACK" {
		t.Fatal(got)
	}
	if ref, err := m.pendingInstance(task.ID()); err != nil || ref != i {
		t.Fatalf("待决记账未指向完成的实例: %v", err)
	}
}

func TestCreateFailureAndCancellationNeverPublish(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			m, root := importEnv(t)
			req := CreateReq{Name: "未完成", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root}
			dir, err := m.validateCreate(&req)
			if err != nil {
				t.Fatal(err)
			}
			writeTree(t, dir, map[string]string{"config/partial.toml": "partial"})
			i := &Instance{Settings: Settings{Name: req.Name}, Dir: dir}
			task, ctx := m.Tasks.New("创建", nil)
			task.EnableCancel()
			err = m.completeCreate(ctx, task, i, func(*Instance) error {
				if cancelled {
					if !m.Tasks.Cancel(task.ID()) {
						t.Error("取消未被接受")
					}
					return nil
				}
				return errors.New("覆盖失败")
			})
			if err == nil {
				t.Fatal("应失败")
			}
			if _, err := m.Get(req.Name); err == nil {
				t.Fatal("失败实例被发布")
			}
			if _, err := os.Stat(filepath.Join(dir, "instance.json")); !os.IsNotExist(err) {
				t.Fatal("失败实例留下登记文件")
			}
			if err := m.discardCreating(req.Name, dir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("没有清理半成品: %v", err)
			}
		})
	}
	// 目录已经发布时绝不降级为直接 RemoveAll。
	m, root := importEnv(t)
	req := CreateReq{Name: "保留", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root}
	dir, err := m.validateCreate(&req)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, map[string]string{"world/level.dat": "keep"})
	m.insts[req.Name] = &Instance{Settings: Settings{Name: req.Name}, Dir: dir}
	if err := m.discardCreating(req.Name, dir); err == nil {
		t.Fatal("已发布目录不应被清理")
	}
	if readLevel(t, dir) != "keep" {
		t.Fatal("数据被改动")
	}
}

func TestPendingRejectsWorldAndPropertiesForBothActions(t *testing.T) {
	for _, act := range []string{"local", "trust"} {
		t.Run(act, func(t *testing.T) {
			m, dir := importEnv(t)
			task := newImportTask(m, "测试服", dir)
			writeTree(t, dir, map[string]string{"server.properties": "level-name=world\n", "world/level.dat": "old"})
			src := filepath.Join(t.TempDir(), "file.bin")
			if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
				t.Fatal(err)
			}
			pending := []modpack.PendingFile{}
			for _, p := range []string{"server.properties", "world/level.dat", "WORLD_nether/region/r.mca", ".minedeck/private.json"} {
				pending = append(pending, modpack.PendingFile{Path: p, Reason: modpack.ReasonHost, SHA1: sha1Of(t, []byte("new")), URLs: []string{"https://93.184.216.34/a"}})
			}
			m.finishImport(task, CreateReq{Name: "测试服"}, dir, pending, nil)
			old := modpackHTTP
			calls := 0
			modpackHTTP = func() *http.Client {
				calls++
				return dl.GuardedClientWithTransport(fakeTransport{content: []byte("new")})
			}
			t.Cleanup(func() { modpackHTTP = old })
			acts := []PendingAction{}
			for _, it := range task.PendingItems() {
				acts = append(acts, PendingAction{ID: it.ID, Act: act, Path: src, URL: "https://93.184.216.34/a", Ack: true})
			}
			out, left, err := m.ResolveImportPending(context.Background(), task.ID(), acts)
			if err != nil {
				t.Fatal(err)
			}
			if len(left) != len(pending) || calls != 0 {
				t.Fatalf("应在读取/下载前拒绝: left=%d calls=%d", len(left), calls)
			}
			for _, o := range out {
				if o.OK || o.Note == "" {
					t.Fatalf("未明确拒绝: %+v", o)
				}
			}
			if readLevel(t, dir) != "old" {
				t.Fatal("原世界被改动")
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "server.properties")); string(b) != "level-name=world\n" {
				t.Fatal("设置被改动")
			}
		})
	}
}

func TestPendingRespectsWorldHoldAndWorldNamedConfig(t *testing.T) {
	m, dir := importEnv(t)
	task := newImportTask(m, "测试服", dir)
	writeTree(t, dir, map[string]string{"server.properties": "level-name=config\n", "config/level.dat": "world"})
	m.finishImport(task, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonNoHash}, {Path: "config/level.dat", Reason: modpack.ReasonNoHash}}, nil)
	src := filepath.Join(t.TempDir(), "a.jar")
	if err := os.WriteFile(src, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := m.BeginWorldChange("测试服", "换图")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: "local", Path: src}}); err == nil {
		t.Error("换图占位期间不应补装")
	}
	release()
	out, _, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: "local", Path: src}, {ID: "p2", Act: "local", Path: src}})
	if err != nil || len(out) != 2 || !out[0].OK || out[1].OK {
		t.Fatalf("普通jar应可补装，同名config世界不允许: %+v %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "level.dat")); string(b) != "world" {
		t.Fatal("真实世界被覆盖")
	}
}

func TestPendingRejectsJunctionIntoWorld(t *testing.T) {
	m, dir := importEnv(t)
	task := newImportTask(m, "测试服", dir)
	writeTree(t, dir, map[string]string{"server.properties": "level-name=world\n", "world/level.dat": "old", "config/base.toml": "config"})
	link := filepath.Join(dir, "config", "alias")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(dir, "world"))
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接: %v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	i, _ := m.Get("测试服")
	if _, err := pendingDestination(i, "config/alias/level.dat"); err == nil {
		t.Fatal("配置目录别名不能写进世界")
	}
	if len(task.PendingItems()) != 0 {
		t.Fatal("检查本身不应改动清单")
	}
	if readLevel(t, dir) != "old" {
		t.Fatal("原世界被改动")
	}
}

func TestWorldIntentClosesStopWaitGap(t *testing.T) {
	m, _ := tempInstance(t)
	i, _ := m.Get("s")
	use, _ := m.UseWorld("s", "备份")
	if _, err := m.BeginWorldIntentInstance(i, "换图"); err == nil {
		t.Fatal("已有备份时不应停服")
	}
	use()
	i.state = "running"
	w, err := m.BeginWorldIntentInstance(i, "换图")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.UseWorld("s", "备份"); err == nil {
		t.Error("停服等待中不能新建备份")
	}
	if _, ok := m.BeginPacking("s"); ok {
		t.Error("停服等待中不能打包")
	}
	if err := w.Change(); err == nil {
		t.Error("还没停服不能升级")
	}
	i.state = "stopped"
	if err := w.Change(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginWorldChange("s", "另一个换图"); err == nil {
		t.Error("升级后应占位")
	}
	w.Release()
	w.Release()
	next, err := m.BeginWorldIntent("s", "下一次")
	if err != nil {
		t.Fatal(err)
	}
	w.Release()
	if m.WorldBusy("s") == "" {
		t.Error("旧释放清掉了新意图")
	}
	next.Release()
}

func TestDeleteInvalidatesOldReferenceAndBlocksDuringRemoval(t *testing.T) {
	m, _ := tempInstance(t)
	old, _ := m.Get("s")
	called := false
	m.SetStartHook(func(string) { called = true })
	deleteHook = func() {
		if err := m.Start("s"); err == nil {
			t.Error("删除中途不应启动")
		}
		if _, err := m.CreateBackup("s", ""); err == nil {
			t.Error("删除中途不应备份")
		}
		if _, err := m.UseWorldInstance(old, "设置"); err == nil {
			t.Error("删除中途不应登记")
		}
	}
	t.Cleanup(func() { deleteHook = nil })
	if err := m.Delete("s", false); err != nil {
		t.Fatal(err)
	}
	deleteHook = nil
	if called {
		t.Error("删除时启动准备回调不应写文件")
	}
	m.insts["s"] = &Instance{Settings: Settings{Name: "s"}, Dir: t.TempDir(), Console: newConsole()}
	if err := m.startInstance(old); err == nil {
		t.Error("旧对象不应启动")
	}
	if _, err := m.UseWorldInstance(old, "设置"); err == nil {
		t.Error("旧对象不应登记")
	}
	if _, err := m.BeginWorldChangeInstance(old, "换图"); err == nil {
		t.Error("旧对象不应换图")
	}
	if _, err := m.BeginWorldIntentInstance(old, "换图"); err == nil {
		t.Error("旧对象不应准备换图")
	}
	if release, ok := m.BeginPackingInstance(old); ok {
		release()
		t.Error("旧对象不应打包")
	}
}

func TestHotBackupIsDiscardedAfterProcessRestart(t *testing.T) {
	m, dir := tempInstance(t)
	i, _ := m.Get("s")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	i.Core = mcsrc.CoreVanilla
	i.MC = "1.20.1"
	i.JavaPath = exe
	i.LaunchTarget = "jar:server.jar"
	if err := os.WriteFile(i.PropsPath(), []byte(fmt.Sprintf("server-port=%d\n", port)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeServerEnv, "1")
	if err := m.Start("s"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, i, "running")
	t.Cleanup(func() {
		if i.Status() != "stopped" {
			_ = m.Kill("s")
			waitStatus(t, i, "stopped")
		}
	})
	backupHook = func() {
		if err := m.Command("s", "crash"); err != nil {
			t.Error(err)
			return
		}
		waitStatus(t, i, "stopped")
		if err := m.Start("s"); err != nil {
			t.Error(err)
			return
		}
		waitStatus(t, i, "running")
	}
	t.Cleanup(func() { backupHook = nil })
	if _, err := m.CreateBackup("s", "restart"); err == nil || !strings.Contains(err.Error(), "作废") {
		t.Fatalf("跨代备份应作废: %v", err)
	}
	backupHook = nil
	if len(backupFiles(t, m)) != 0 {
		t.Fatal("作废的备份不应发布")
	}
	if got := readLevel(t, dir); got != "current" {
		t.Fatal("原世界不应变化")
	}
	if _, err := m.CreateBackup("s", "normal"); err != nil {
		t.Fatalf("进程稳定时热备应成功: %v", err)
	}
}

func TestBackupWithoutPropertiesAndConcurrentNames(t *testing.T) {
	m, dir := tempInstance(t)
	if _, err := os.Stat(filepath.Join(dir, "server.properties")); !os.IsNotExist(err) {
		t.Fatal("测试需要没有 properties")
	}
	var wg sync.WaitGroup
	names := make(chan string, 2)
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); name, err := m.CreateBackup("s", "same"); names <- name; errs <- err }()
	}
	wg.Wait()
	close(names)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for n := range names {
		if seen[n] {
			t.Fatal("同名备份被覆盖")
		}
		seen[n] = true
	}
	for n := range seen {
		writeLevel(t, dir, "changed")
		if err := m.RestoreBackup("s", n); err != nil {
			t.Fatal(err)
		}
		if readLevel(t, dir) != "current" {
			t.Fatal("默认世界还原失败")
		}
	}
}

func TestCreateReservationRejectsSameDirectoryAlias(t *testing.T) {
	m, root := importEnv(t)
	first := CreateReq{Name: "pack.a", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root}
	dir, err := m.validateCreate(&first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Name = "pack a"
	if _, err := m.validateCreate(&second); err == nil {
		t.Fatal("不同名称不能同时预留同一个slug目录")
	}
	if !m.creating[first.Name] || !m.creating[dirKey(dir)] || m.creating[second.Name] {
		t.Fatal("预留的归属被覆盖")
	}
}

func TestPendingPreservesRootDatapackWorkflow(t *testing.T) {
	m, dir := importEnv(t)
	task := newImportTask(m, "测试服", dir)
	m.finishImport(task, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "datapacks/example.zip", Reason: modpack.ReasonNoHash}}, nil)
	src := filepath.Join(t.TempDir(), "example.zip")
	if err := os.WriteFile(src, []byte("resource"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, left, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: "local", Path: src}})
	if err != nil || len(out) != 1 || !out[0].OK || len(left) != 0 {
		t.Fatalf("正常数据包补装不应被拒: %+v %v", out, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "datapacks", "example.zip")); err != nil || string(b) != "resource" {
		t.Fatalf("文件不完整: %q %v", b, err)
	}
}

func TestPendingKeepsWorldLeaseUntilBothWritesFinish(t *testing.T) {
	for _, act := range []string{"local", "trust"} {
		t.Run(act, func(t *testing.T) {
			m, dir := importEnv(t)
			task := newImportTask(m, "测试服", dir)
			body := []byte("jar")
			m.finishImport(task, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonHost, SHA1: sha1Of(t, body), URLs: []string{"https://93.184.216.34/a"}}}, nil)
			src := filepath.Join(t.TempDir(), "a.jar")
			if err := os.WriteFile(src, body, 0o644); err != nil {
				t.Fatal(err)
			}
			old := modpackHTTP
			modpackHTTP = func() *http.Client { return dl.GuardedClientWithTransport(fakeTransport{content: body}) }
			t.Cleanup(func() { modpackHTTP = old })
			entered, proceed := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			t.Cleanup(resume)
			atomicfile.Hook = func(path string) {
				if strings.EqualFold(path, filepath.Join(dir, "mods", "a.jar")) {
					close(entered)
					<-proceed
				}
			}
			t.Cleanup(func() { atomicfile.Hook = nil })
			done := make(chan error, 1)
			go func() {
				out, _, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: act, Path: src, URL: "https://93.184.216.34/a", Ack: true}})
				if err == nil && (len(out) != 1 || !out[0].OK) {
					err = fmt.Errorf("补装失败: %+v", out)
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("没有进入实际写入")
			}
			if _, err := m.BeginWorldChange("测试服", "换图"); err == nil {
				t.Fatal("写入中仍能换图")
			}
			if err := m.Delete("测试服", true); err == nil {
				t.Fatal("写入中仍能删除")
			}
			resume()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			atomicfile.Hook = nil
			if who := m.WorldUsers("测试服"); who != "" {
				t.Fatalf("写完未释放: %s", who)
			}
		})
	}
}
