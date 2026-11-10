package transfer

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 测试环境：缓存目录里有版本清单和一个「本机已装」的假 Java 17（java.Resolve 的 Cached 路径命中它，
// 全程不联网），都是 inst 包测试的同一套做法。

const testManifest = `{"latest":{"release":"1.20.1","snapshot":"1.20.1"},"versions":[
{"id":"1.21.1","type":"release"},{"id":"1.20.1","type":"release"},{"id":"1.18.1","type":"release"},
{"id":"1.17","type":"release"},{"id":"1.12.2","type":"release"}]}`

var testJava17 string

func TestMain(m *testing.M) {
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
	setConfig(t, func(c *app.Config) { c.ServersDir, c.BackupsDir, c.Roots, c.BackupRoots = "", "", nil, nil })
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
	// 联网的机器上 mcsrc 的版本清单会先试网络再落磁盘缓存，慢时几秒到几十秒——等足一点
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
	_, err := extractPrefix(nil, pack, out,
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
	if i.ConsoleEncoding != "gbk" || len(i.ExtraJVM) != 1 || i.ExtraJVM[0] != "-XX:+UseG1GC" {
		t.Errorf("A 的 JVM 参数 / 控制台编码没带过来：%+v", i.Settings)
	}
	// 到启动参数这一层：run.bat 与托管启动共用 launchArgs
	got, err := os.ReadFile(filepath.Join(i.Dir, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-Xmx2048M", "-XX:+UseG1GC", "server.jar", "nogui"} {
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
	if _, err := Summarize(makePack("dist.mdpack", `{"formatVersion":1,"mode":"distributable","instance":{"name":"x"},"core":{"core":"paper","mc":"1.20.1"}}`)); err == nil || !strings.Contains(err.Error(), "下发") {
		t.Errorf("下发包该指路：%v", err)
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
	if _, err := extractPrefix(nil, p, dest, func(int64, int64) {}, func(int, int64, int64) {}); err == nil {
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
	if _, err := ImportAsync(m, snap.Result, ImportReq{Name: "A 的服", EULA: true}); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Errorf("重名导入该被拒：%v", err)
	}
}
