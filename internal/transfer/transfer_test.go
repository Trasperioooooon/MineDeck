package transfer

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/dl/dltest"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 测试环境：缓存目录里有版本清单和一个「本机已装」的假 Java 17（java.Resolve 的 Cached 路径命中它），
// 都是 inst 包测试的同一套做法。
//
// 光这样并不是「全程不联网」：取版本清单是先试网络、取不到才用缓存，有网的机器上导入的每条用例都会真的去连
// 镜像站，取到的是真清单，还会把缓存里这份测试用的盖掉（2026-11-16 用记拨号的办法查出来的：单跑时有五条用例
// 各连一次 bmclapi2.bangbang93.com）。所以 TestMain 一开头就把外网挡掉（dltest.NoInternet），取清单马上失败、
// 落回缓存里这一份，有没有网结果都一样。

const testManifest = `{"latest":{"release":"1.20.1","snapshot":"1.20.1"},"versions":[
{"id":"1.21.1","type":"release"},{"id":"1.20.1","type":"release"},{"id":"1.18.1","type":"release"},
{"id":"1.17","type":"release"},{"id":"1.12.2","type":"release"}]}`

var testJava17 string

func TestMain(m *testing.M) {
	dltest.NoInternet()
	dir, err := os.MkdirTemp("", "md-transfer-test")
	if err != nil {
		panic(err)
	}
	app.CacheDir = dir
	if err := os.WriteFile(filepath.Join(dir, "version_manifest_v2.json"), []byte(testManifest), 0o644); err != nil {
		panic(err)
	}
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

type env struct {
	servers string // B 这边的默认存放根
}

// newEnv 每个用例一份干净的程序目录与配置；不建 Manager——要扫描的服务器目录先写好再用 scan 建。
func newEnv(t *testing.T) *env {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir}
	t.Cleanup(func() { app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir = old[0], old[1], old[2], old[3] })
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	setConfig(t, func(c *app.Config) {
		c.ServersDir, c.BackupsDir, c.Roots, c.BackupRoots, c.CreatingRoots = "", "", nil, nil, nil
	})
	for _, d := range []string{app.ServersDir, app.BackupsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &env{servers: app.ServersDir}
}

func setConfig(t *testing.T, mutate func(*app.Config)) {
	t.Helper()
	if _, err := app.UpdateConfig(func(c *app.Config) error { mutate(c); return nil }); err != nil {
		t.Fatal(err)
	}
}

// writeFile 落一个文件（连同父目录）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeJar 写一个带元数据的最小 jar（fabric.mod.json），字段随便但形状对得上 modinfo。
func writeJar(t *testing.T, path, modID, name, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf(`{"id":%q,"name":%q,"version":%q,"environment":"*","schemaVersion":1}`, modID, name, version)
	_, _ = w.Write([]byte(meta))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// fakeInstance 造一台 A 的服务器目录并写好 instance.json（返回目录）。
// 内容：世界、配置、两个模组（一个正常、一个魔改——不是 zip）、名单、eula、logs、run.bat。
func fakeInstance(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(app.ServersDir, "server-a")
	writeFile(t, filepath.Join(dir, "world", "level.dat"), "level")
	writeFile(t, filepath.Join(dir, "world", "region", "r.0.0.mca"), strings.Repeat("M", 4096))
	writeFile(t, filepath.Join(dir, "world", "session.lock"), "lock") // 不进包
	writeFile(t, filepath.Join(dir, "world_nether", "level.dat"), "nether")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25570\nlevel-name=world\nmotd=A 的服\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")
	writeFile(t, filepath.Join(dir, "whitelist.json"), `[{"name":"Steve","uuid":"069a79f4-44e9-4726-a5be-fca90e38aaf5"}]`)
	writeFile(t, filepath.Join(dir, "ops.json"), `[{"name":"Notch","uuid":"853c80ef3c7c32dbc015cc2434c73665","level":4,"bypassesPlayerLimit":false}]`)
	writeFile(t, filepath.Join(dir, "config", "testmod", "common.toml"), "answer=42\n")
	writeJar(t, filepath.Join(dir, "mods", "normalmod-1.0.jar"), "normalmod", "普通模组", "1.0")
	writeFile(t, filepath.Join(dir, "mods", "魔改-private.jar"), "不是 zip 的内容")      // 线上无对应的魔改 jar
	writeFile(t, filepath.Join(dir, "logs", "latest.log"), "log")                 // 不进包
	writeFile(t, filepath.Join(dir, "run.bat"), "@echo off\r\n")                  // 不进包（B 重新生成）
	writeFile(t, filepath.Join(dir, "server.jar"), "fake paper server jar bytes") // 完整模式会带走
	s := inst.Settings{
		Name: name, Core: mcsrc.CorePaper, MC: "1.20.1", Build: "100",
		JavaMajor: 17, JavaPath: testJava17, XmxMB: 2048, XmsMB: 1024,
		LaunchTarget:    "jar:server.jar",
		ExtraJVM:        []string{"-XX:+UseG1GC"},
		ConsoleEncoding: "gbk",
		CreatedAt:       time.Unix(1700000000, 0),
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "instance.json"), string(b))
	return dir
}

// manager 扫描根建 Manager，并确认 name 在里面。
func manager(t *testing.T, name string) *inst.Manager {
	t.Helper()
	m, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(name); err != nil {
		t.Fatalf("扫描根没认出这台服务器：%v", err)
	}
	return m
}

// waitTask 等任务结束；顺带记录任务运行中看到过的最大进度（后面的步骤开始时 done/total 会清零，
// 完成后只看快照拿不到当时的进度）。
func waitTask(t *testing.T, mgr *inst.Manager, id string) (snap tasks.Snapshot, maxDone, maxTotal int64, sawFiles bool) {
	t.Helper()
	// 等足一点：原来联网的机器上取版本清单要先试网络，慢时几秒到几十秒；现在外网挡掉了（见 TestMain），留着余量给慢机器
	deadline := time.Now().Add(60 * time.Second)
	for {
		task := mgr.Tasks.Get(id)
		if task == nil {
			t.Fatalf("任务不存在: %s", id)
		}
		s := task.Snapshot()
		if s.Done > maxDone {
			maxDone, maxTotal = s.Done, s.Total
		}
		if strings.Contains(s.Label, "个文件") {
			sawFiles = true
		}
		if s.Ended {
			return s, maxDone, maxTotal, sawFiles
		}
		if time.Now().After(deadline) {
			t.Fatalf("任务超时未结束：%+v", s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestZipProgressCallbacks 直接验证打包 / 解包的进度回调按字节单调到总量、fileCount 带实时的数——
// 端到端轮询是尽力而为（任务太短时采不到），契约在这里确定性验证。
func TestZipProgressCallbacks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	n := 64
	var files []packFile
	for i := 0; i < n; i++ {
		p := filepath.Join(src, fmt.Sprintf("f%02d.dat", i))
		writeFile(t, p, strings.Repeat("x", 100+i))
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, packFile{rel: filepath.Base(p), abs: p, size: st.Size()})
	}
	mf := &Manifest{Counts: PackCounts{Files: int64(n), Bytes: 0}}
	for _, f := range files {
		mf.Counts.Bytes += f.size
	}
	var lastDone, lastTotal int64
	var lastN int
	pack := filepath.Join(dir, "out.mdpack")
	mf2 := &Manifest{Mode: ModeHandover, FormatVersion: 1, Counts: PackCounts{Bytes: mf.Counts.Bytes}}
	if err := writePack(nil, pack, files, mf2,
		func(done, total int64) {
			if done < lastDone || total != mf2.Counts.Bytes || done > total {
				t.Errorf("进度不单调或总量不对：%d/%d（上一次 %d）", done, total, lastDone)
			}
			lastDone, lastTotal = done, total
		},
		func(k int, done, total int64) {
			if k < lastN {
				t.Errorf("文件数回调不单调")
			}
			lastN = k
		}); err != nil {
		t.Fatal(err)
	}
	if lastDone != mf2.Counts.Bytes || lastTotal != mf2.Counts.Bytes {
		t.Errorf("打包完进度应到总量：%d/%d", lastDone, lastTotal)
	}
	if lastN != n {
		t.Errorf("最后一批文件数是 %d，想要 %d", lastN, n)
	}
	// 解包侧
	out := filepath.Join(dir, "out")
	_, err := extractPrefix(nil, pack, out, extractOpts{declared: mf2.Counts.Bytes},
		func(done, total int64) {
			if total != mf2.Counts.Bytes || done > total {
				t.Errorf("解包进度不对：%d/%d", done, total)
			}
			lastDone, lastTotal = done, total
		},
		func(k int, done, total int64) { lastN = k })
	if err != nil {
		t.Fatal(err)
	}
	if lastDone != lastTotal || lastTotal != mf2.Counts.Bytes {
		t.Errorf("解包完进度不对：%d/%d", lastDone, lastTotal)
	}
	if lastN != n {
		t.Errorf("解包文件数不对：%d", lastN)
	}
}

// packEntries 列出 zip 的全部条目名。
func packEntries(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

// 核心回归：导出（完整模式）→ 导入 → 实例进列表、launchArgs 到位、世界 / 配置 / 模组逐字节保留。
func TestExportImportRoundtripFull(t *testing.T) {
	newEnv(t)
	aDir := fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	aProps, _ := os.ReadFile(filepath.Join(aDir, "server.properties"))

	// 导出（完整模式，落默认缓存目录）
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, maxDone, maxTotal, sawFiles := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s\n日志：%s", snap.Err, strings.Join(snap.Log, "\n"))
	}
	if snap.Kind != "transfer-out" {
		t.Errorf("任务 kind = %q，想要 transfer-out", snap.Kind)
	}
	path := snap.Result
	if path == "" || !strings.HasSuffix(path, ".mdpack") {
		t.Fatalf("任务结果不是交接包路径：%q", path)
	}
	// 双维度进度：字节（done/total）主维度 + label 里带文件数。采样到中逧行即可——末帧和回调取值的
	// 合同由 TestZipProgressCallbacks 确定性验证；这里只保证字段确实进了任务快照。
	if maxTotal > 0 && (maxDone <= 0 || maxDone > maxTotal) {
		t.Errorf("导出中采样到的进度不合理：%d/%d", maxDone, maxTotal)
	}
	if sawFiles && maxTotal <= 0 {
		t.Errorf("采样到文件数维度却没有字节维度")
	}

	// 摘要（导入向导给 B 看的）
	sum, err := Summarize(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Name != "A 的服" || sum.Core != "paper" || sum.MC != "1.20.1" || sum.Build != "100" {
		t.Errorf("摘要核心信息不对：%+v", sum)
	}
	if sum.Port != 25570 || sum.Mods != 2 || !sum.Full || sum.WorldBytes < 4096 || sum.XmxMB != 2048 || sum.JavaMajor != 17 {
		t.Errorf("摘要内容不对：%+v", sum)
	}
	if sum.MineDeck != app.Version || sum.ExportedAt == "" {
		t.Errorf("摘要的版本/时间不对：%+v", sum)
	}

	// 包里的清单
	mf, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if mf.FormatVersion != 1 || mf.Mode != "handover" || !mf.Full || mf.MineDeckVersion != app.Version {
		t.Errorf("manifest 头不对：%+v", mf)
	}
	if mf.Instance.Name != "A 的服" || mf.Instance.Core != mcsrc.CorePaper || mf.Instance.LaunchTarget != "jar:server.jar" {
		t.Errorf("manifest 的 instance.json 原文不对：%+v", mf.Instance)
	}
	if len(mf.Mods) != 2 {
		t.Fatalf("模组清单应有 2 项：%+v", mf.Mods)
	}
	var normal, modded bool
	for _, e := range mf.Mods {
		switch e.File {
		case "normalmod-1.0.jar":
			normal = e.ID == "normalmod" && e.Loader == "fabric" && e.Version == "1.0" && len(e.SHA1) == 40
		case "魔改-private.jar":
			// 魔改 jar：读不出元数据，id 留空，但 sha1/size 在——线上无对应也照保留
			modded = e.ID == "" && len(e.SHA1) == 40 && e.Size == int64(len("不是 zip 的内容"))
		}
	}
	if !normal || !modded {
		t.Errorf("模组清单不对：%+v", mf.Mods)
	}
	if mf.WorldBytes < 4096 {
		t.Errorf("世界大小不对：%d", mf.WorldBytes)
	}
	ents := packEntries(t, path)
	has := func(n string) bool {
		for _, e := range ents {
			if e == n {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"manifest.json", "instance/server.properties", "instance/eula.txt",
		"instance/world/level.dat", "instance/world/region/r.0.0.mca", "instance/world_nether/level.dat",
		"instance/mods/normalmod-1.0.jar", "instance/mods/魔改-private.jar", "instance/config/testmod/common.toml",
		"instance/whitelist.json", "instance/ops.json", "instance/server.jar"} {
		if !has(want) {
			t.Errorf("包里少了 %s；实际：%v", want, ents)
		}
	}
	for _, banned := range []string{"instance/world/session.lock", "instance/logs/latest.log", "instance/run.bat", "instance/instance.json"} {
		if has(banned) {
			t.Errorf("包里不该有 %s", banned)
		}
	}

	// 导入
	iid, err := ImportAsync(m, path, ImportReq{Name: "B 接手", EULA: false}) // 包里同意过 eula，不用再勾
	if err != nil {
		t.Fatal(err)
	}
	isnap, iDone, iTotal, _ := waitTask(t, m, iid)
	if isnap.Err != "" {
		t.Fatalf("导入失败：%s\n日志：%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
	if isnap.Kind != "transfer-in" || isnap.Result != "B 接手" {
		t.Errorf("导入任务不对：%+v", isnap)
	}
	if iTotal > 0 && (iDone <= 0 || iDone > iTotal) {
		t.Errorf("导入中采样到的进度不合理：%d/%d", iDone, iTotal)
	}

	i, err := m.Get("B 接手")
	if err != nil {
		t.Fatalf("导入后实例不在列表里：%v", err)
	}
	if i.MC != "1.20.1" || i.Core != mcsrc.CorePaper || i.Build != "100" {
		t.Errorf("导入后的版本不对：%+v", i.Settings)
	}
	if i.JavaMajor != 17 || i.JavaPath != testJava17 {
		t.Errorf("Java 没按本机解析：%d %s", i.JavaMajor, i.JavaPath)
	}
	if i.XmxMB != 2048 || i.XmsMB != 1024 {
		t.Errorf("内存沿用不对：%d/%d", i.XmxMB, i.XmsMB)
	}
	if i.LaunchTarget != "jar:server.jar" {
		t.Errorf("启动方式不对：%s", i.LaunchTarget)
	}
	if i.ConsoleEncoding != "gbk" {
		t.Errorf("A 的控制台编码没带过来：%+v", i.Settings)
	}
	// JVM 参数不带过来（决定这台电脑上执行什么，向导上又看不到），但要在日志里告诉接手的人原来有哪些
	if len(i.ExtraJVM) != 0 {
		t.Errorf("A 的 JVM 参数不该自动采用：%v", i.ExtraJVM)
	}
	if log := strings.Join(isnap.Log, "\n"); !strings.Contains(log, "-XX:+UseG1GC") {
		t.Errorf("日志里该写明原来的 JVM 参数没带过来：\n%s", log)
	}
	// 到启动参数这一层：run.bat 与托管启动共用 launchArgs
	got, err := os.ReadFile(filepath.Join(i.Dir, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-Xmx2048M", "server.jar", "nogui"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("run.bat 里没有 %s：\n%s", want, got)
		}
	}
	args := inst.LaunchArgsOf(i)
	if len(args) < 4 || args[len(args)-1] != "nogui" || args[len(args)-2] != "server.jar" {
		t.Errorf("launchArgs 不对：%v", args)
	}
	// 世界 / 配置 / 名单 / 模组逐字节保留；session.lock 不带过来
	bProps, err := os.ReadFile(filepath.Join(i.Dir, "server.properties"))
	if err != nil || string(bProps) != string(aProps) {
		t.Errorf("server.properties 没原样保留：%q\n原：%q", bProps, aProps)
	}
	for _, f := range []string{"world/level.dat", "world/region/r.0.0.mca", "world_nether/level.dat",
		"config/testmod/common.toml", "mods/normalmod-1.0.jar", "mods/魔改-private.jar", "whitelist.json", "ops.json", "eula.txt", "server.jar"} {
		a, err := os.ReadFile(filepath.Join(aDir, filepath.FromSlash(f)))
		if err != nil {
			t.Fatalf("A 侧读不了 %s：%v", f, err)
		}
		b, err := os.ReadFile(filepath.Join(i.Dir, filepath.FromSlash(f)))
		if err != nil {
			t.Fatalf("B 侧少了 %s：%v", f, err)
		}
		if string(a) != string(b) {
			t.Errorf("%s 导过来变了", f)
		}
	}
	if _, err := os.Stat(filepath.Join(i.Dir, "world", "session.lock")); !os.IsNotExist(err) {
		t.Errorf("session.lock 不该带过来")
	}
	// 上传的包导入完就删
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("导入后上传的包该删掉")
	}
	// A 一侧没被动过
	aProps2, _ := os.ReadFile(filepath.Join(aDir, "server.properties"))
	if string(aProps2) != string(aProps) {
		t.Errorf("导出不该改动 A 的文件")
	}
}

// 瘦身模式：不带核心启动文件，剔除清单里说明，其余全带。
func TestExportSlim(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: false})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s\n日志：%s", snap.Err, strings.Join(snap.Log, "\n"))
	}
	mf, err := ReadManifest(snap.Result)
	if err != nil {
		t.Fatal(err)
	}
	if mf.Full {
		t.Errorf("瘦身模式 manifest 不该标 full")
	}
	if len(mf.Excluded) == 0 || !strings.Contains(strings.Join(mf.Excluded, "\n"), "服务端核心") {
		t.Errorf("剔除清单没说明核心： %+v", mf.Excluded)
	}
	for _, e := range packEntries(t, snap.Result) {
		if e == "instance/server.jar" || strings.HasPrefix(e, "instance/libraries/") {
			t.Errorf("瘦身模式不该带核心：%s", e)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(snap.Result), filepath.Base(snap.Result))); err != nil {
		t.Errorf("交接包没落位：%v", err)
	}
}

// TestExportCarriesCoreConfigs 各家核心放在根目录的配置（spigot.yml 这些）和整合包的脚本目录（kubejs）要进包；
// 清单之外的东西不会跟过去，要写进任务日志和剔除清单；清单里不带导出者电脑上的 java.exe 路径。
// 2026-11-10 真机试出来的：原来 Paper 服的 spigot.yml、bukkit.yml 悄悄丢了，对方开服时按默认值重新生成。
func TestExportCarriesCoreConfigs(t *testing.T) {
	newEnv(t)
	dir := fakeInstance(t, "A 的服")
	writeFile(t, filepath.Join(dir, "spigot.yml"), "settings:\n  bungeecord: true\n")
	writeFile(t, filepath.Join(dir, "bukkit.yml"), "spawn-limits:\n  monsters: 50\n")
	writeFile(t, filepath.Join(dir, "kubejs", "server_scripts", "recipes.js"), "// 配方")
	writeFile(t, filepath.Join(dir, "lobby", "level.dat"), "多世界插件建的大厅") // level-name 之外的世界
	writeFile(t, filepath.Join(dir, "lobby", "region", "r.0.0.mca"), "大厅的区块")
	writeFile(t, filepath.Join(dir, "dynmap", "web", "tiles", "0.png"), "地图瓦片") // 清单之外的目录
	writeFile(t, filepath.Join(dir, "我的笔记.txt"), "自己记的东西")                      // 清单之外的文件
	writeFile(t, filepath.Join(dir, "cache", "mojang_1.20.1.jar"), "会重新下载")     // 可以重新生成：不带也不提
	writeFile(t, filepath.Join(dir, "usercache.json"), "[]")
	// MineDeck 自己的文件不出这台电脑：和自家模组之间的令牌、游戏内 AI 的设置、老版本的日志配置、配置备份
	writeFile(t, filepath.Join(dir, "config", "minedeck", "companion.properties"), "token=这台电脑上的令牌\n")
	writeFile(t, filepath.Join(dir, "gameai.json"), "{}")
	writeFile(t, filepath.Join(dir, "log4j2_112-116.xml"), "<Configuration/>")
	writeFile(t, filepath.Join(dir, ".minedeck", "config-backups", "server.properties.1"), "x")
	// 空文件夹：Forge 装好就有空的 defaultconfigs；清单之外的空文件夹（里面只有空的子文件夹）同理，没有东西可带
	for _, empty := range []string{"defaultconfigs", filepath.Join("空文件夹", "里面也是空的")} {
		if err := os.MkdirAll(filepath.Join(dir, empty), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := manager(t, "A 的服")
	for _, full := range []bool{false, true} {
		id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: full})
		if err != nil {
			t.Fatal(err)
		}
		snap, _, _, _ := waitTask(t, m, id)
		if snap.Err != "" {
			t.Fatalf("导出失败：%s\n日志：%s", snap.Err, strings.Join(snap.Log, "\n"))
		}
		ents := strings.Join(packEntries(t, snap.Result), "\n")
		for _, want := range []string{"instance/spigot.yml", "instance/bukkit.yml", "instance/kubejs/server_scripts/recipes.js",
			"instance/lobby/level.dat", "instance/lobby/region/r.0.0.mca"} {
			if !strings.Contains(ents, want) {
				t.Errorf("full=%v：%s 应该进包", full, want)
			}
		}
		for _, not := range []string{"instance/dynmap/", "instance/我的笔记.txt", "instance/cache/", "instance/usercache.json", "instance/logs/", "instance/run.bat", "instance/instance.json",
			"instance/config/minedeck/", "instance/gameai.json", "instance/log4j2_112-116.xml", "instance/.minedeck/"} {
			if strings.Contains(ents, not) {
				t.Errorf("full=%v：%s 不该进包", full, not)
			}
		}
		mf, err := ReadManifest(snap.Result)
		if err != nil {
			t.Fatal(err)
		}
		if mf.Instance.JavaPath != "" {
			t.Errorf("清单不该带导出者电脑上的 java.exe 路径：%q", mf.Instance.JavaPath)
		}
		if mf.Instance.JavaMajor != 17 {
			t.Errorf("Java 大版本要留着，导入时靠它选 Java：%d", mf.Instance.JavaMajor)
		}
		excluded := strings.Join(mf.Excluded, "\n")
		log := strings.Join(snap.Log, "\n")
		for _, want := range []string{"dynmap/", "我的笔记.txt"} {
			if !strings.Contains(excluded, want) || !strings.Contains(log, want) {
				t.Errorf("full=%v：没进包的 %s 要写进剔除清单和任务日志\n剔除清单：%s\n日志：%s", full, want, excluded, log)
			}
		}
		for _, not := range []string{"cache", "usercache.json", "run.bat", "instance.json", "server.jar", "gameai.json", "log4j2", ".minedeck"} {
			if strings.Contains(excluded, not) {
				t.Errorf("full=%v：%s 会重新生成或重新下载，不用列进「没进包」：%s", full, not, excluded)
			}
		}
		for _, not := range []string{"defaultconfigs", "空文件夹"} {
			if strings.Contains(excluded, not) || strings.Contains(log, not) {
				t.Errorf("full=%v：%s 是空的，没有东西可带，不该提醒「没有进包」\n剔除清单：%s\n日志：%s", full, not, excluded, log)
			}
		}
		os.Remove(snap.Result)
	}
}

// 摘要错误：不是交接包、版本太新、下发包。
func TestManifestValidation(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.mdpack")
	if err := os.WriteFile(bare, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Summarize(bare); err == nil {
		t.Errorf("打不开的包不该有摘要")
	}
	makePack := func(name string, body string) string {
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, _ := zw.Create(manifestName)
		w.Write([]byte(body))
		zw.Close()
		f.Close()
		return p
	}
	if _, err := Summarize(makePack("v2.mdpack", `{"formatVersion":2,"mode":"handover","instance":{"name":"x"},"core":{"core":"paper","mc":"1.20.1"}}`)); err == nil || !strings.Contains(err.Error(), "升级") {
		t.Errorf("更新版本的包该报升级： %v", err)
	}
	// 「发给朋友」的包和交接的包是同一种格式，只是 mode 不同：两种都认（原来把它当成另一种包拒掉）
	if sum, err := Summarize(makePack("dist.mdpack", `{"formatVersion":1,"mode":"distributable","instance":{"name":"x"},"core":{"core":"paper","mc":"1.20.1"}}`)); err != nil || sum.Mode != ModeDistributable {
		t.Errorf("发给朋友的包应照常读出摘要：%v %+v", err, sum)
	}
	if _, err := Summarize(makePack("odd.mdpack", `{"formatVersion":1,"mode":"别的什么","instance":{"name":"x"},"core":{"core":"paper","mc":"1.20.1"}}`)); err == nil || !strings.Contains(err.Error(), "不认识") {
		t.Errorf("不认识的 mode 该报错：%v", err)
	}
	if _, err := Summarize(makePack("noCore.mdpack", `{"formatVersion":1,"mode":"handover","instance":{"name":"x"},"core":{"core":"什么鬼","mc":"1.20.1"}}`)); err == nil {
		t.Errorf("认不得的核心该报错")
	}
	if _, err := Summarize(makePack("plain.zip", `{"formatVersion":0,"mode":"","instance":{},"core":{}}`)); err == nil {
		t.Errorf("普通 zip（没有版本和模式）该报错")
	}
}

// zip-slip：包里带 .. 的条目必须被拒。
func TestZipSlipRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "evil.mdpack")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"instance/world/level.dat", "instance/../evil.txt", "instance/a/../../evil2.txt"} {
		w, _ := zw.Create(name)
		w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()
	dest := filepath.Join(dir, "dest")
	if _, err := extractPrefix(nil, p, dest, extractOpts{}, func(int64, int64) {}, func(int, int64, int64) {}); err == nil {
		t.Errorf("带越界路径的包必须被拒")
	} else if !strings.Contains(err.Error(), "越界") && !strings.Contains(err.Error(), "可疑") {
		t.Errorf("报错该指明问题：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); !os.IsNotExist(err) {
		t.Errorf("越界文件被解出来了")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil2.txt")); !os.IsNotExist(err) {
		t.Errorf("越界文件被解出来了")
	}
}

// EULA 与端口：包里没同意过 eula 时必须勾；勾了写进去；填了端口就改包里的端口。
func TestImportEULAAndPortOverride(t *testing.T) {
	newEnv(t)
	aDir := fakeInstance(t, "A 的服")
	os.Remove(filepath.Join(aDir, "eula.txt"))
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	if _, err := ImportAsync(m, snap.Result, ImportReq{Name: "B1", EULA: false}); err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Errorf("没勾 EULA 该被拒：%v", err)
	}
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: "B1", EULA: true, Port: 25599})
	if err != nil {
		t.Fatal(err)
	}
	isnap, _, _, _ := waitTask(t, m, iid)
	if isnap.Err != "" {
		t.Fatalf("导入失败：%s\n%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
	i, _ := m.Get("B1")
	b, err := os.ReadFile(filepath.Join(i.Dir, "eula.txt"))
	if err != nil || !eulaAccepted(b) {
		t.Errorf("eula.txt 没写进去：%q %v", b, err)
	}
	props, err := inst.LoadProps(filepath.Join(i.Dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := props.Get("server-port"); v != "25599" {
		t.Errorf("端口该按请求改：got %q", v)
	}
}

// 交接包导入到一个新的存放位置：登记时把这个位置记成了扫描根，重开以后这台还在列表里（A38）。
func TestImportIntoNewRootListedAfterRestart(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	root := filepath.Join(t.TempDir(), "新存放位置")
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: "B 的服", EULA: true, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if isnap, _, _, _ := waitTask(t, m, iid); isnap.Err != "" {
		t.Fatalf("导入失败：%s\n%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
	m2 := manager(t, "B 的服") // 重开，确认这台在列表里
	b, err := m2.Get("B 的服")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(b.Dir) != root {
		t.Fatalf("这台应在新的存放位置里：%s", b.Dir)
	}
}

// 导入交接包走到一半（解完包、正在按填的端口改 server.properties）程序没了：文件夹带着创建记号，重开以后在「没建完的文件夹」里；
// 接着走完导入，记号去掉、不再列出来（A48）。
func TestImportInterruptedLeavesListedLeftover(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	root := filepath.Join(t.TempDir(), "新存放位置")
	reached, gate := make(chan string, 1), make(chan struct{})
	var once sync.Once
	old := atomicfile.Hook
	t.Cleanup(func() { atomicfile.Hook = old })
	atomicfile.Hook = func(p string) {
		if filepath.Base(p) != "server.properties" || !strings.HasPrefix(p, root) {
			return
		}
		once.Do(func() { reached <- filepath.Dir(p); <-gate })
	}
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: "B 的服", EULA: true, Root: root, Port: 25599})
	if err != nil {
		t.Fatal(err)
	}
	var dir string
	select {
	case dir = <-reached:
	case <-time.After(30 * time.Second):
		close(gate)
		t.Fatal("导入没走到改端口那一步")
	}
	listed := func() bool {
		m2, err := inst.NewManager() // 这时重开：只看盘上的东西
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(m2.CreatingLeftovers(), func(it inst.CreatingLeftover) bool { return it.Name == filepath.Base(dir) })
	}
	if _, err := os.Stat(filepath.Join(dir, inst.CreatingMarker)); err != nil || !listed() {
		close(gate)
		t.Fatalf("解完包的文件夹应带着创建记号，重开以后列为没建完的：%v", err)
	}
	close(gate)
	if isnap, _, _, _ := waitTask(t, m, iid); isnap.Err != "" {
		t.Fatalf("放行以后导入应完成：%s\n%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
	if _, err := os.Stat(filepath.Join(dir, inst.CreatingMarker)); !os.IsNotExist(err) || listed() {
		t.Fatalf("导入成功以后不该再有创建记号，也不该列为没建完的：%v", err)
	}
}

// 重名导入被拒。
func TestImportDuplicateName(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	if _, err := ImportAsync(m, snap.Result, ImportReq{Name: "A 的服", EULA: true}); err == nil || !strings.Contains(err.Error(), "已有同名的服务器") {
		t.Errorf("重名导入该被拒：%v", err)
	}
}

// 导入交接包在收尾时失败，半成品又没删干净（有文件被别的程序占着）：失败原因照实说没清干净，剩下的文件夹带着创建记号，
// 列在「没建完的文件夹」里，文件放开以后能在那里删掉（A48）。
func TestImportCleanupFailureKeepsMarker(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只有 Windows 上打开着的文件删不掉")
	}
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	root := filepath.Join(t.TempDir(), "新存放位置")
	var dir string
	var held *os.File
	var mu sync.Mutex
	old := atomicfile.Hook
	t.Cleanup(func() {
		atomicfile.Hook = old
		mu.Lock()
		defer mu.Unlock()
		if held != nil {
			held.Close()
		}
	})
	atomicfile.Hook = func(p string) {
		mu.Lock()
		defer mu.Unlock()
		if filepath.Base(p) != "server.properties" || !strings.HasPrefix(p, root) || held != nil {
			return
		}
		// 改端口的时候：别的程序占着世界里的一个文件，收尾写 run.bat 又会失败（那个位置上放一个文件夹）
		dir = filepath.Dir(p)
		held, _ = os.Open(filepath.Join(dir, "world", "level.dat"))
		_ = os.Mkdir(filepath.Join(dir, "run.bat"), 0o755)
	}
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: "B 的服", EULA: true, Root: root, Port: 25599})
	if err != nil {
		t.Fatal(err)
	}
	isnap, _, _, _ := waitTask(t, m, iid)
	mu.Lock()
	got := dir
	mu.Unlock()
	if got == "" || isnap.Err == "" {
		t.Fatalf("应在收尾时失败：%+v", isnap)
	}
	if _, err := os.Stat(got); err != nil {
		t.Skip("这台电脑上打开着的文件也删得掉，碰不上删不干净的情形")
	}
	if !strings.Contains(isnap.Err, "清理半成品失败") || !isnap.CleanupFailed {
		t.Fatalf("失败原因里要照实说半成品没清干净：%q", isnap.Err)
	}
	listedID := func() string {
		m2, err := inst.NewManager() // 这时重开：只看盘上的东西
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range m2.CreatingLeftovers() {
			if it.Name == filepath.Base(got) {
				return it.ID
			}
		}
		return ""
	}
	leftover := listedID()
	if _, err := os.Stat(filepath.Join(got, inst.CreatingMarker)); err != nil || leftover == "" {
		t.Fatalf("没删干净的半成品应带着创建记号，列在「没建完的文件夹」里：%v", err)
	}
	mu.Lock()
	held.Close()
	held = nil
	mu.Unlock()
	m3, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := m3.DiscardCreatingLeftover(leftover, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("文件放开以后应能在「没建完的文件夹」那里删掉：%v", err)
	}
}

// 预留以后、建文件夹之前，同一个位置上冒出了别人的文件夹（空的、有东西的各一种）：导入在建文件夹那一步失败，说清那里已经有
// 文件夹、没有动它；失败时的清理（DiscardCreate）不碰它，不放创建记号（A48）。建文件夹之前没有能停下来放文件夹的地方，
// 这里照 ImportAsync 的先后直接走：预留、放文件夹、runImport、清理。
func TestImportDirAppearedAfterReserveLeftAlone(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s", snap.Err)
	}
	mf, err := ReadManifest(snap.Result)
	if err != nil {
		t.Fatal(err)
	}
	for kind, files := range map[string]map[string]string{
		"空文件夹": {},
		"有东西":  {"world/level.dat": "别人的世界", "notes.txt": "别人的笔记"},
	} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "存放位置")
			req := ImportReq{Name: "B 的服（" + kind + "）", EULA: true, Root: root}
			creq := inst.CreateReq{Name: req.Name, Core: mf.Core.Core, MC: mf.Core.MC, Build: mf.Core.Build, EULA: true, Root: root}
			dir, err := m.ReserveCreate(&creq)
			if err != nil {
				t.Fatal(err)
			}
			defer m.ReleaseCreate(creq.Name, dir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range files {
				writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), body)
			}
			tk, ctx := m.Tasks.New("导入交接包", importSteps)
			err = runImport(ctx, tk, m, mf, snap.Result, req, dir)
			if err == nil || !strings.Contains(err.Error(), "这个位置已经有一个同名文件夹（可能是刚放进来的），没有动它") {
				t.Fatalf("应在建文件夹那一步失败、说清那里已经有文件夹：%v", err)
			}
			if cerr := m.DiscardCreate(err, req.Name, dir); cerr != nil {
				t.Fatalf("别人的文件夹不进清理，不该报清理失败：%v", cerr)
			}
			entries, rerr := os.ReadDir(dir)
			if want := map[bool]int{true: 0, false: 2}[len(files) == 0]; rerr != nil || len(entries) != want {
				t.Fatalf("别人的文件夹应原样留着、不多不少：%v %v", entries, rerr)
			}
			for name, body := range files {
				if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); err != nil || string(b) != body {
					t.Errorf("别人的 %s 被动过：%q %v", name, b, err)
				}
			}
			if _, err := m.Get(req.Name); err == nil {
				t.Error("不该登记成服务器")
			}
		})
	}
}
