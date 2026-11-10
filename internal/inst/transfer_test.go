package inst

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/tasks"
	"minedeck/internal/transfer"
)

// mkPackSource 造一台停在列表里、目录完整的实例（NeoForge 1.20.1、Java 17——测试环境里有假的
// 本机 Java 17，收包时保真路径能离线解析成功）。
func mkPackSource(t *testing.T, m *Manager, name string) *Instance {
	t.Helper()
	dir := filepath.Join(app.ServersRoot(), slugify(name))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := Settings{
		Name: name, Core: "neoforge", MC: "1.20.1", Build: "47.2.17",
		JavaMajor: 17, XmxMB: 4096, XmsMB: 1024,
		LaunchTarget: "jar:server.jar", CreatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
	}
	writeTree(t, dir, map[string]string{
		"server.properties":        "server-port=25570\nlevel-name=world\nmotd=Summer Server\n",
		"eula.txt":                 "#Agreed by everyone\neula=true\n",
		"server.jar":               vanilla21,
		"run.bat":                  "@echo off\njava -jar server.jar nogui\n",
		"world/level.dat":          " level ",
		"world/playerdata/uid.dat": " progress-for-everyone ",
		"mods/a.jar":               "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\n\r\n",
		"config/common.toml":       "a=1\n",
		"logs/latest.log":          "junk",
		"usercache.json":           "[]",
	})
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	i := &Instance{Settings: s, Dir: dir, Console: newConsole()}
	m.mu.Lock()
	m.insts[name] = i
	m.mu.Unlock()
	return i
}

// waitTaskOf 等 Async 任务结束并返回快照。
func waitTaskOf(t *testing.T, m *Manager, id string) tasks.Snapshot {
	t.Helper()
	for n := 0; n < 500; n++ {
		if tk := m.Tasks.Get(id); tk != nil {
			if s := tk.Snapshot(); s.Ended {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("任务 %s 太久不结束", id)
	return tasks.Snapshot{}
}

func TestExportPackAsyncDistributable(t *testing.T) {
	m, _ := adoptEnv(t)
	mkPackSource(t, m, "生存服")
	outDir := t.TempDir()

	id, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: transfer.ModeDistributable,
		Dest: outDir}) // 给目录：自动起名
	if err != nil {
		t.Fatal(err)
	}
	s := waitTaskOf(t, m, id)
	if s.Err != "" || s.Kind != "pack" || !strings.HasSuffix(s.Result, transfer.Ext) {
		t.Fatalf("打包任务不对: %+v", s)
	}
	sum, err := transfer.Inspect(s.Result)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Instance.Name != "生存服" || sum.Instance.Core != "neoforge" || sum.Instance.MC != "1.20.1" {
		t.Errorf("包里实例摘要不对: %+v", sum.Instance)
	}
	if sum.Port != 25570 || !sum.EULAAgreed {
		t.Errorf("端口 / EULA 不对: %d %v", sum.Port, sum.EULAAgreed)
	}
	if sum.WorldSize <= 0 || sum.ModCount == 0 {
		t.Errorf("世界大小与模组清单应有值: %d %d", sum.WorldSize, sum.ModCount)
	}
	for _, gone := range []string{"logs", "crash-reports", "run.bat", "instance.json", ".minedeck", "usercache.json"} {
		if !slices.Contains(sum.Excluded, gone) {
			t.Errorf("剔除清单应含 %s: %v", gone, sum.Excluded)
		}
	}
	if _, err := os.Stat(s.Result); err != nil {
		t.Errorf("包文件没落盘: %v", err)
	}
}

func TestExportPackRejects(t *testing.T) {
	m, _ := adoptEnv(t)
	i := mkPackSource(t, m, "生存服")
	// 运行中不许打包
	i.procMu.Lock()
	i.state = "running"
	i.procMu.Unlock()
	if _, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Dest: t.TempDir(), Mode: transfer.ModeDistributable}); err == nil || !strings.Contains(err.Error(), "停止") {
		t.Errorf("运行中应拒绝打包: %v", err)
	}
	i.procMu.Lock()
	i.state = "stopped"
	i.procMu.Unlock()
	// 目标放进实例自己的目录里不行
	if _, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Dest: i.Dir, Mode: transfer.ModeDistributable}); err == nil || !strings.Contains(err.Error(), "自己的目录") {
		t.Errorf("导到实例目录里应拒绝: %v", err)
	}
	// 空目标、不认识的实例与模式
	if _, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: transfer.ModeDistributable}); err == nil || !strings.Contains(err.Error(), "放在哪") {
		t.Errorf("空目标应报错: %v", err)
	}
	if _, err := m.ExportPackAsync(ExportReq{Inst: "没有这台", Mode: transfer.ModeDistributable, Dest: t.TempDir()}); err == nil {
		t.Errorf("不存在的实例应报错")
	}
	if _, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: "diy", Dest: t.TempDir()}); err == nil {
		t.Errorf("不认识的模式应报错")
	}
}

func TestImportPackAsyncRoundtrip(t *testing.T) {
	m, _ := adoptEnv(t)
	mkPackSource(t, m, "生存服")
	id, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: transfer.ModeDistributable, Dest: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pack := waitTaskOf(t, m, id).Result
	packBytes, err := os.ReadFile(pack) // 收包成功后缓存里的包会被清掉，先留一份原件试重名
	if err != nil {
		t.Fatal(err)
	}
	pack2 := filepath.Join(t.TempDir(), "again.mdpack")
	if err := os.WriteFile(pack2, packBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	id2, err := m.ImportPackAsync(ImportPackReq{File: pack, Name: "寒假续上", Port: 25599, XmxMB: 2048, EULA: false})
	if err != nil {
		t.Fatal(err)
	}
	s := waitTaskOf(t, m, id2)
	if s.Err != "" || s.Kind != "unpack" || s.Result != "寒假续上" {
		t.Fatalf("收包任务不对: %+v", s)
	}
	i, err := m.Get("寒假续上")
	if err != nil {
		t.Fatal(err)
	}
	if i.MC != "1.20.1" || i.Core != "neoforge" || i.Build != "47.2.17" || i.Adopted {
		t.Errorf("落地实例设置不对: %+v", i.Settings)
	}
	if i.JavaMajor != 17 || i.JavaPath != testJava17 { // 包里写的是 17，保真落地并离线解析成功
		t.Errorf("Java 解析不对: %d %s", i.JavaMajor, i.JavaPath)
	}
	if i.XmxMB != 2048 || i.XmsMB > i.XmxMB {
		t.Errorf("内存应按选择改: %d/%d", i.XmxMB, i.XmsMB)
	}
	if p := i.Port(); p != 25599 {
		t.Errorf("端口应改成 25599，得到 %d", p)
	}
	if b, err := os.ReadFile(filepath.Join(i.Dir, "world", "playerdata", "uid.dat")); err != nil || string(b) != " progress-for-everyone " {
		t.Errorf("世界存档没落好: %v %q", err, string(b))
	}
	bat := mustRead(t, filepath.Join(i.Dir, "run.bat"))
	if !isOurRunBat([]byte(bat)) {
		t.Errorf("run.bat 应重新生成")
	}
	if eula := mustRead(t, filepath.Join(i.Dir, "eula.txt")); !strings.Contains(eula, "eula=true") {
		t.Errorf("包里已同意的 EULA 应回执如初")
	}
	if _, err := os.Stat(filepath.Join(i.Dir, "logs")); !os.IsNotExist(err) {
		t.Errorf("剔除的 logs 不该回来")
	}
	// 落地目录在默认存放根下
	if root, _ := filepath.Rel(app.ServersRoot(), i.Dir); strings.HasPrefix(root, "..") {
		t.Errorf("落地目录应放在默认存放根下: %s", i.Dir)
	}

	// 同名再收一次：报已存在
	if _, err := m.ImportPackAsync(ImportPackReq{File: pack2, Name: "寒假续上"}); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Errorf("重名应报错: %v", err)
	}
	if _, err := os.Stat(pack); !os.IsNotExist(err) {
		t.Errorf("收包成功后缓存里的包应该清掉: %v", err)
	}
}

func TestImportPackEULAAndBadPackage(t *testing.T) {
	m, _ := adoptEnv(t)
	src := mkPackSource(t, m, "生存服")
	// 包里 eula=false：收包要重新同意
	if err := os.WriteFile(filepath.Join(src.Dir, "eula.txt"), []byte("eula=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: transfer.ModeDistributable, Dest: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pack := waitTaskOf(t, m, id).Result
	if _, err := m.ImportPackAsync(ImportPackReq{File: pack, Name: "要勾的"}); err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Errorf("没同意 EULA 应拒绝: %v", err)
	}
	id2, err := m.ImportPackAsync(ImportPackReq{File: pack, Name: "要勾的", EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := waitTaskOf(t, m, id2); s.Err != "" {
		t.Fatalf("勾了 EULA 收包应成功: %s", s.Err)
	}
	i, _ := m.Get("要勾的")
	if eula := mustRead(t, filepath.Join(i.Dir, "eula.txt")); !strings.Contains(eula, "eula=true") {
		t.Errorf("EULA 应写上")
	}

	// 包里写了要启动的文件、解出来却没有：任务失败，实例不留在列表里
	bad := filepath.Join(t.TempDir(), "bad.mdpack")
	manifest := `{"formatVersion":1,"mode":"distributable","exportedAt":"2026-08-01T12:00:00Z","exporterVersion":"MineDeck 3.0.0",` +
		`"instance":{"name":"坏包","core":"paper","mc":"1.20.1","javaMajor":17,"xmxMb":2048,"xmsMb":1024,"launchTarget":"jar:vanished.jar","createdAt":"2026-07-01T10:00:00Z"},` +
		`"mods":[],"worldSize":0,"excluded":[]}`
	f, err := os.Create(bad)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create(transfer.ManifestFile)
	w.Write([]byte(manifest))
	zw.Close()
	f.Close()
	id3, err := m.ImportPackAsync(ImportPackReq{File: bad, Name: "缺启动文件", JavaMajor: 17, EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := waitTaskOf(t, m, id3); s.Err == "" || !strings.Contains(s.Err, "启动文件") {
		t.Fatalf("缺启动文件应失败: %+v", s)
	}
	for _, i := range m.List() {
		if i.Name == "缺启动文件" {
			t.Errorf("失败任务的实例不该留在列表里")
		}
	}
}

func TestPackBusyBlocksStartAndDelete(t *testing.T) {
	m, _ := adoptEnv(t)
	mkPackSource(t, m, "生存服")
	m.markPacking("生存服", true) // 拿占位直接模拟打包任务在跑（免真的开包线程）
	t.Cleanup(func() { m.markPacking("生存服", false) })
	if err := m.Start("生存服"); err == nil || !strings.Contains(err.Error(), "正在打包") {
		t.Errorf("打包中启动应被挡下: %v", err)
	}
	if err := m.Delete("生存服", false); err == nil || !strings.Contains(err.Error(), "正在打包") {
		t.Errorf("打包中删除应被挡下: %v", err)
	}
	if _, err := m.ExportPackAsync(ExportReq{Inst: "生存服", Mode: transfer.ModeDistributable, Dest: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "正在打包") {
		t.Errorf("重复打包应被挡下: %v", err)
	}
	m.markPacking("生存服", false)
	if err := m.Start("生存服"); err == nil || !strings.Contains(err.Error(), "找不到这台服务器用的 Java") {
		t.Errorf("占位释放后启动应回到正常校验（Java 找不到）: %v", err)
	}
}

func TestImportPackProxyNeedsNoEULA(t *testing.T) {
	m, base := adoptEnv(t) // base：放用户原来的文件夹
	vdir := filepath.Join(base, "velocity")
	writeTree(t, vdir, map[string]string{
		"velocity.jar":   "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: proxy.Main\r\n\r\n",
		"velocity.toml":  "[server]\n",
		"plugins/pa.jar": "zip:",
	})
	vs := Settings{Name: "代理", Core: "velocity", MC: "3.4.0", JavaMajor: 17, XmxMB: 512, XmsMB: 512, LaunchTarget: "jar:velocity.jar"}
	vb, _ := json.MarshalIndent(vs, "", "  ")
	if err := os.WriteFile(filepath.Join(vdir, "instance.json"), vb, 0o644); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.insts["代理"] = &Instance{Settings: vs, Dir: vdir, Console: newConsole()}
	m.mu.Unlock()
	vid, err := m.ExportPackAsync(ExportReq{Inst: "代理", Mode: transfer.ModeHandover, Dest: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	vpack := waitTaskOf(t, m, vid).Result
	id, err := m.ImportPackAsync(ImportPackReq{File: vpack, Name: "老代理", JavaMajor: 17, EULA: false})
	if err != nil {
		t.Fatal(err)
	}
	if s := waitTaskOf(t, m, id); s.Err != "" {
		t.Fatalf("代理端收包不需要 EULA: %s", s.Err)
	}
	if i, err := m.Get("老代理"); err != nil || !slices.Contains([]string{"jar:velocity.jar"}, i.LaunchTarget) {
		t.Errorf("代理端落地不对: %v %s", err, i.LaunchTarget)
	}
}
