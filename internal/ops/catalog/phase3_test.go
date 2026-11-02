package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// 第三阶段新增的读写能力（设计文档 §5.2）。

// newModCatalog 临时程序目录里放一个停止的 NeoForge 实例「整合包」：mods 里两个模组、几份配置、日志和崩溃报告。
func newModCatalog(t *testing.T) (*ops.Registry, *inst.Manager, string) {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "pack")
	java17 := filepath.Join(app.Base, "jdk17", "bin", "java.exe")
	s, _ := json.Marshal(inst.Settings{Name: "整合包", Core: "neoforge", MC: "1.21.1", JavaMajor: 17, JavaPath: java17, XmxMB: 6144, XmsMB: 1024})
	files := map[string]string{
		"instance.json":                                      string(s),
		"server.properties":                                  "motd=hi\nserver-port=25570\nlevel-name=world\n",
		"config/create-common.toml":                          "[trains]\n\tmaxSpeed = 40\n\tenabled = true\n",
		"config/discord/bot.toml":                            "token = \"real-bot-token-123\"\nchannel = \"general\"\n",
		"world/serverconfig/create-server.toml":              "[kinetics]\n\tmaxRotationSpeed = 256\n",
		"logs/latest.log":                                    "[12:00:00] [main/INFO] [x/]: 第一行\n[12:00:01] [main/WARN] [x/]: 有点问题\n[12:00:02] [main/ERROR] [x/]: 出错了 boom\n[12:00:03] [main/INFO] [x/]: 最后一行\n",
		"crash-reports/crash-2026-10-28_13.10.44-server.txt": "---- Minecraft Crash Report ----\nDescription: Watching Server\n\njava.lang.Error: Watchdog\n\tat a.B.c(B.java:1)\n\nA detailed walkthrough of the error\n-- System Details --\n\tJava Version: 17.0.12\n",
		"hs_err_pid4242.log":                                 "#\n# A fatal error has been detected by the Java Runtime Environment:\n#\n#  EXCEPTION_ACCESS_VIOLATION\n#\n\nUSERNAME=alice\n",
		"jdk17/bin/java.exe":                                 "",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if name == "jdk17/bin/java.exe" {
			p = java17
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// GBK 写的 debug.log（老 Forge 在中文 Windows 上会这样）
	gbk, _ := simplifiedchinese.GBK.NewEncoder().String("[12:00:00] [main/DEBUG] [x/]: 调试信息：找到存档\n")
	_ = os.WriteFile(filepath.Join(dir, "logs", "debug.log"), []byte(gbk), 0o644)
	writeTestJar(t, filepath.Join(dir, "mods", "create-1.21.1-6.0.4.jar"), map[string]string{"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"create\"\nversion=\"6.0.4\"\n[[mixins]]\nconfig=\"create.mixins.json\"\n[[dependencies.create]]\nmodId=\"geckolib\"\ntype=\"required\"\nversionRange=\"[4.4,)\"\n"})
	writeTestJar(t, filepath.Join(dir, "mods", "xaerominimap.jar"), map[string]string{"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"xaerominimap\"\nversion=\"24.5\"\n", "xaero/minimap/XaeroMinimap.class": "x"})
	// 本机已有的 Java 21（MineDeck 的便携版目录）
	j21 := filepath.Join(app.RuntimesDir, "java-21", "bin", "java.exe")
	_ = os.MkdirAll(filepath.Dir(j21), 0o755)
	_ = os.WriteFile(j21, nil, 0o644)
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Port: 27333}), mgr, dir
}

func writeTestJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for n, b := range files {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(b))
	}
	_ = zw.Close()
	_ = f.Close()
}

var (
	human = ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	bot   = ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
)

// do 调用操作并把结果按 JSON 解码进 out。
func do(t *testing.T, reg *ops.Registry, c ops.Caller, name, args string, out any) error {
	t.Helper()
	res, err := reg.Do(context.Background(), c, name, ops.Args(args))
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

func TestTroubleshootReads(t *testing.T) {
	reg, mgr, _ := newModCatalog(t)
	i, _ := mgr.Get("整合包")
	for _, l := range []string{"[MineDeck] 启动: java -jar old.jar", "旧的一行 ERROR", "[MineDeck] 启动: java -Xmx6144M @args nogui",
		"[09:30:03] [main/ERROR] [ne.ne.fm.lo.ModSorter/LOADING]: Missing or unsupported mandatory dependencies:",
		"\tMod ID: 'geckolib', Requested by: 'create', Expected range: '[4.4,)', Actual version: '[MISSING]'",
		"[09:30:04] [main/WARN] [x/]: 警告 Boom", "[09:30:05] [main/INFO] [x/]: 普通", "[MineDeck] 服务器进程已退出（exit status 1）"} {
		i.Console.Append(l)
	}
	// 控制台：按级别、关键字、本次启动过滤
	var con struct {
		Lines   []string `json:"lines"`
		Total   int      `json:"total"`
		Matched int      `json:"matched"`
	}
	if err := do(t, reg, bot, "instance.console.tail", `{"inst":"整合包","level":"warn","since":"start"}`, &con); err != nil {
		t.Fatal(err)
	}
	if con.Total != 8 || len(con.Lines) != 2 || !strings.Contains(con.Lines[0], "ModSorter") || !strings.Contains(con.Lines[1], "警告") {
		t.Errorf("控制台按 WARN 过滤：%+v", con)
	}
	_ = do(t, reg, bot, "instance.console.tail", `{"inst":"整合包","q":"boom","lines":1}`, &con)
	if con.Matched != 1 || len(con.Lines) != 1 {
		t.Errorf("控制台按关键字：%+v", con)
	}
	// 本地规则排错：认出缺少前置，本次启动之前的不算；有模组清单
	var dg struct {
		Findings []struct {
			Rule  string   `json:"rule"`
			Files []string `json:"files"`
		} `json:"findings"`
		Crashed bool `json:"crashed"`
	}
	if err := do(t, reg, bot, "instance.diag", `{"inst":"整合包"}`, &dg); err != nil {
		t.Fatal(err)
	}
	if len(dg.Findings) != 1 || dg.Findings[0].Rule != "missing-deps" || !dg.Crashed || dg.Findings[0].Files[0] != "create-1.21.1-6.0.4.jar" {
		t.Errorf("本地规则排错：%+v", dg)
	}
	// 日志：读末尾；搜索带上下文；GBK 的 debug.log 也能读
	var lg struct {
		Lines   []string `json:"lines"`
		Matches []struct {
			Line   int      `json:"line"`
			Before []string `json:"before"`
			After  []string `json:"after"`
		} `json:"matches"`
	}
	if err := do(t, reg, bot, "instance.logs.read", `{"inst":"整合包","lines":2}`, &lg); err != nil || len(lg.Lines) != 2 || !strings.Contains(lg.Lines[1], "最后一行") {
		t.Errorf("日志末尾：%v %+v", err, lg)
	}
	if err := do(t, reg, bot, "instance.logs.read", `{"inst":"整合包","q":"BOOM"}`, &lg); err != nil || len(lg.Matches) != 1 || lg.Matches[0].Line != 3 ||
		len(lg.Matches[0].Before) != 2 || len(lg.Matches[0].After) != 1 {
		t.Errorf("日志搜索：%v %+v", err, lg)
	}
	if err := do(t, reg, bot, "instance.logs.read", `{"inst":"整合包","file":"debug"}`, &lg); err != nil || !strings.Contains(strings.Join(lg.Lines, ""), "调试信息：找到存档") {
		t.Errorf("GBK 的日志：%v %+v", err, lg)
	}
	// 崩溃报告：列表、节选、hs_err 摘要；只认规定的文件名
	var cr struct {
		Reports []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"reports"`
		Summary string `json:"summary"`
	}
	if err := do(t, reg, bot, "instance.crashes", `{"inst":"整合包"}`, &cr); err != nil || len(cr.Reports) != 2 {
		t.Fatalf("崩溃报告列表：%v %+v", err, cr)
	}
	if err := do(t, reg, bot, "instance.crashes", `{"inst":"整合包","file":"crash-2026-10-28_13.10.44-server.txt"}`, &cr); err != nil ||
		!strings.Contains(cr.Summary, "Watchdog") || !strings.Contains(cr.Summary, "Java Version: 17.0.12") {
		t.Errorf("崩溃报告节选：%v %q", err, cr.Summary)
	}
	if err := do(t, reg, bot, "instance.crashes", `{"inst":"整合包","file":"hs_err_pid4242.log"}`, &cr); err != nil ||
		!strings.Contains(cr.Summary, "EXCEPTION_ACCESS_VIOLATION") || strings.Contains(cr.Summary, "alice") {
		t.Errorf("hs_err 摘要：%v %q", err, cr.Summary)
	}
	for _, bad := range []string{"../server.properties", "server.properties", "crash-reports/../../x.txt"} {
		if err := do(t, reg, bot, "instance.crashes", `{"inst":"整合包","file":"`+bad+`"}`, nil); ops.CodeOf(err) != 400 {
			t.Errorf("读 %s 应拒绝：%v", bad, err)
		}
	}
	// 模组清单：简表；按关键字带依赖
	var ml struct {
		Count int `json:"count"`
		Mods  []struct {
			File string `json:"file"`
			ID   string `json:"id"`
			Deps []any  `json:"deps"`
		} `json:"mods"`
	}
	if err := do(t, reg, bot, "instance.mods.list", `{"inst":"整合包"}`, &ml); err != nil || ml.Count != 2 || ml.Mods[0].ID != "create" || ml.Mods[0].Deps != nil {
		t.Errorf("模组清单：%v %+v", err, ml)
	}
	if err := do(t, reg, bot, "instance.mods.list", `{"inst":"整合包","q":"CREATE"}`, &ml); err != nil || len(ml.Mods) != 1 || len(ml.Mods[0].Deps) != 1 {
		t.Errorf("按关键字带依赖：%v %+v", err, ml)
	}
	// 端口：占着的端口查得出来
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	var pc struct {
		TCP struct {
			Free   bool `json:"free"`
			Owners []struct {
				PID      int  `json:"pid"`
				MineDeck bool `json:"minedeck"`
			} `json:"owners"`
		} `json:"tcp"`
	}
	if err := do(t, reg, bot, "port.check", `{"port":`+itoa(port)+`,"proto":"tcp"}`, &pc); err != nil || pc.TCP.Free {
		t.Errorf("占着的端口：%v %+v", err, pc)
	}
	for _, o := range pc.TCP.Owners { // wine 里不一定查得到进程
		if o.PID != os.Getpid() || !o.MineDeck {
			t.Errorf("占端口的应是本进程：%+v", o)
		}
	}
	// 只占 IPv4 的 0.0.0.0（测试报告 E-09 的做法）：Windows 上再绑一次 :端口（IPv6 双栈）照样成功，也得认出被占
	ln4, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln4.Close()
	pc.TCP.Owners = nil
	if err := do(t, reg, bot, "port.check", `{"port":`+itoa(ln4.Addr().(*net.TCPAddr).Port)+`,"proto":"tcp"}`, &pc); err != nil || pc.TCP.Free || len(pc.TCP.Owners) == 0 {
		t.Errorf("只占 0.0.0.0 的端口：%v %+v", err, pc)
	}
	var pc2 struct {
		Instances []map[string]string `json:"instances"`
	}
	if err := do(t, reg, bot, "port.check", `{"port":25570}`, &pc2); err != nil || len(pc2.Instances) != 1 || pc2.Instances[0]["instance"] != "整合包" {
		t.Errorf("端口上配置的实例：%v %+v", err, pc2)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// 配置文件：列目录、读内容；路径只能在规定的几个目录里，不能跳出去，不能读 jar。
func TestConfigRead(t *testing.T) {
	reg, _, _ := newModCatalog(t)
	var roots struct {
		Roots []struct{ Path string } `json:"roots"`
	}
	if err := do(t, reg, bot, "instance.config.read", `{"inst":"整合包"}`, &roots); err != nil || len(roots.Roots) != 2 ||
		roots.Roots[0].Path != "config" || roots.Roots[1].Path != "world/serverconfig" {
		t.Errorf("可读的目录：%v %+v", err, roots)
	}
	var ls struct {
		Files []struct{ Path string } `json:"files"`
	}
	if err := do(t, reg, bot, "instance.config.read", `{"inst":"整合包","path":"config"}`, &ls); err != nil || len(ls.Files) != 2 {
		t.Errorf("列目录：%v %+v", err, ls)
	}
	var f struct {
		Content    string `json:"content"`
		TotalLines int    `json:"totalLines"`
		More       bool   `json:"more"`
	}
	if err := do(t, reg, bot, "instance.config.read", `{"inst":"整合包","path":"config\\create-common.toml","start":2,"lines":1}`, &f); err != nil ||
		f.Content != "\tmaxSpeed = 40" || !f.More {
		t.Errorf("读一行：%v %+v", err, f)
	}
	for _, bad := range []string{"../instance.json", "config/../../x.toml", "C:\\Windows\\win.ini", "/etc/passwd", "mods/create-1.21.1-6.0.4.jar",
		"server.properties", "plugins/x.yml", "config/create.exe"} {
		if err := do(t, reg, bot, "instance.config.read", `{"inst":"整合包","path":`+jsonStr(bad)+`}`, nil); ops.CodeOf(err) != 400 {
			t.Errorf("读 %s 应拒绝：%v", bad, err)
		}
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

// 写配置：AI 提出的修改先预览（逐行对比），确认后备份原文件再写；可以撤销；文件在预览之后变了，提案作废；
// 把脱敏占位符原样写回会被拒绝；恢复默认是备份后删掉。
func TestConfigWrite(t *testing.T) {
	reg, _, dir := newModCatalog(t)
	ctx := context.Background()
	path := filepath.Join(dir, "config", "create-common.toml")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	propose := func(args string) *ops.Proposal {
		t.Helper()
		res, err := reg.Do(ctx, bot, "instance.config.write", ops.Args(args))
		if err != nil {
			t.Fatal(err)
		}
		return res.(*ops.Proposal)
	}
	p := propose(`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"maxSpeed = 40","replace":"maxSpeed = 60"}]}`)
	if !strings.Contains(p.Plan.Diff, "- \tmaxSpeed = 40\n+ \tmaxSpeed = 60") || read() != "[trains]\n\tmaxSpeed = 40\n\tenabled = true\n" {
		t.Fatalf("预览：%q", p.Plan.Diff)
	}
	if _, err := reg.Confirm(ctx, human, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if read() != "[trains]\n\tmaxSpeed = 60\n\tenabled = true\n" {
		t.Fatalf("没按提案改：%q", read())
	}
	bks, _ := filepath.Glob(filepath.Join(dir, ".minedeck", "config-backups", "*"))
	if len(bks) != 1 {
		t.Fatalf("应留一份备份：%v", bks)
	}
	// 预览之后文件被别人改了：作废
	p = propose(`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"enabled = true","replace":"enabled = false"}]}`)
	_ = os.WriteFile(path, []byte(read()+"# 手改\n"), 0o644)
	if _, err := reg.Confirm(ctx, human, p.ID, false); ops.CodeOf(err) != 409 || strings.Contains(read(), "enabled = false") {
		t.Fatalf("文件变了应作废：%v", err)
	}
	// 撤销：回到上一次修改之前
	p = propose(`{"inst":"整合包","path":"config/create-common.toml","mode":"undo"}`)
	if _, err := reg.Confirm(ctx, human, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if read() != "[trains]\n\tmaxSpeed = 40\n\tenabled = true\n" {
		t.Fatalf("撤销后：%q", read())
	}
	// 找不到原文、出现多次、占位符原样写回：都不生成提案
	for args, want := range map[string]string{
		`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"nope","replace":"x"}]}`:                                       "找不到",
		`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"=","replace":":"}]}`:                                          "出现了 2 次",
		`{"inst":"整合包","path":"config/discord/bot.toml","edits":[{"find":"token = \"***\"","replace":"token = \"x\""}]}`:                  "隐藏掉的内容",
		`{"inst":"整合包","path":"config/discord/bot.toml","mode":"replace","content":"token = \"***\"\nchannel = \"general\"\n"}`:           "占位符",
		`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"maxSpeed = 40","replace":"dir = \"C:\\\\Users\\\\<用户名>\""}]}`: "占位符",
		`{"inst":"整合包","path":"mods/x.toml","mode":"replace","content":"x"}`:                                                              "只能读写",
	} {
		if _, err := reg.Do(ctx, bot, "instance.config.write", ops.Args(args)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：应报「%s」：%v", args, want, err)
		}
	}
	// 恢复默认：备份后删掉；serverconfig 也能改
	p = propose(`{"inst":"整合包","path":"world/serverconfig/create-server.toml","mode":"reset"}`)
	if _, err := reg.Confirm(ctx, human, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "world", "serverconfig", "create-server.toml")); !os.IsNotExist(err) {
		t.Error("恢复默认应删掉文件")
	}
	if bks, _ := filepath.Glob(filepath.Join(dir, ".minedeck", "config-backups", "world__serverconfig__create-server.toml~*")); len(bks) != 1 {
		t.Errorf("恢复默认前应备份：%v", bks)
	}
}

// 写回时保持原文件的编码和换行：GBK + Windows 换行的插件配置改完还是 GBK + \r\n（多行的 find 照样找得到），
// 带 BOM 的 UTF-8 不丢 BOM；备份存的是原文件的字节，撤销原样恢复；GBK 表示不了的字在提案阶段就报错。
func TestConfigWriteKeepsFormat(t *testing.T) {
	reg, _, dir := newModCatalog(t)
	ctx := context.Background()
	gbk := func(s string) []byte { b, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s)); return b }
	yml := filepath.Join(dir, "plugins", "Essentials", "config.yml")
	orig := gbk("欢迎: 你好\r\nmotd: 测试\r\n")
	_ = os.MkdirAll(filepath.Dir(yml), 0o755)
	_ = os.WriteFile(yml, orig, 0o644)
	bom := filepath.Join(dir, "config", "bom.toml")
	_ = os.WriteFile(bom, []byte("\xef\xbb\xbfa = 1\n"), 0o644)
	run := func(args string) error {
		t.Helper()
		res, err := reg.Do(ctx, bot, "instance.config.write", ops.Args(args))
		if err != nil {
			return err
		}
		_, err = reg.Confirm(ctx, human, res.(*ops.Proposal).ID, false)
		return err
	}
	if err := run(`{"inst":"整合包","path":"plugins/Essentials/config.yml","edits":[{"find":"欢迎: 你好\nmotd: 测试","replace":"欢迎: 大家好\nmotd: 测试"}]}`); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(yml); !bytes.Equal(b, gbk("欢迎: 大家好\r\nmotd: 测试\r\n")) {
		t.Errorf("GBK + \\r\\n 应保持：%q", b)
	}
	bks, _ := filepath.Glob(filepath.Join(dir, ".minedeck", "config-backups", "plugins__Essentials__config.yml~*"))
	if b, _ := os.ReadFile(bks[0]); len(bks) != 1 || !bytes.Equal(b, orig) {
		t.Errorf("备份应是原文件的字节：%q", b)
	}
	if _, err := reg.Do(ctx, bot, "instance.config.write", ops.Args(`{"inst":"整合包","path":"plugins/Essentials/config.yml","edits":[{"find":"大家好","replace":"🎉"}]}`)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "GBK") {
		t.Errorf("GBK 表示不了的字应在提案阶段报错：%v", err)
	}
	if err := run(`{"inst":"整合包","path":"plugins/Essentials/config.yml","mode":"undo"}`); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(yml); !bytes.Equal(b, orig) {
		t.Errorf("撤销应原样恢复：%q", b)
	}
	if err := run(`{"inst":"整合包","path":"config/bom.toml","edits":[{"find":"a = 1","replace":"a = 2"}]}`); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bom); string(b) != "\xef\xbb\xbfa = 2\n" {
		t.Errorf("BOM 应保留：%q", b)
	}
}

// 禁用 / 启用模组、换 Java、重启、新建实例的 EULA。
func TestWriteOps(t *testing.T) {
	reg, mgr, dir := newModCatalog(t)
	ctx := context.Background()
	jar := filepath.Join(dir, "mods", "xaerominimap.jar")
	res, err := reg.Do(ctx, bot, "instance.mods.toggle", ops.Args(`{"inst":"整合包","file":"xaerominimap.jar","enable":false}`))
	if err != nil {
		t.Fatal(err)
	}
	p := res.(*ops.Proposal)
	if !strings.Contains(p.Plan.Summary, "xaerominimap 24.5") || p.Plan.Changes[0].After != "xaerominimap.jar.disabled" {
		t.Errorf("预览：%+v", p.Plan)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Fatal("没确认就改名了")
	}
	if _, err := reg.Confirm(ctx, human, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jar + ".disabled"); err != nil {
		t.Fatal("没有禁用")
	}
	// 启用回来；预览之后有人手动改回去了：作废
	res, _ = reg.Do(ctx, bot, "instance.mods.toggle", ops.Args(`{"inst":"整合包","file":"xaerominimap.jar.disabled","enable":true}`))
	_ = os.Rename(jar+".disabled", jar)
	if _, err := reg.Confirm(ctx, human, res.(*ops.Proposal).ID, false); ops.CodeOf(err) != 409 {
		t.Errorf("状态变了应作废：%v", err)
	}
	for _, bad := range []string{`"../server.properties"`, `"nope.jar"`, `"config/x.jar"`} {
		if _, err := reg.Do(ctx, bot, "instance.mods.toggle", ops.Args(`{"inst":"整合包","file":`+bad+`,"enable":false}`)); err == nil {
			t.Errorf("%s 应拒绝", bad)
		}
	}

	// 换 Java：已有 Java 21 就直接换（下次启动生效）；指定不认识的路径拒绝
	res, err = reg.Do(ctx, bot, "instance.java.set", ops.Args(`{"inst":"整合包","major":21}`))
	if err != nil {
		t.Fatal(err)
	}
	p = res.(*ops.Proposal)
	if c := p.Plan.Changes[0]; !strings.HasPrefix(c.Before, "Java 17") || !strings.Contains(c.After, filepath.Join("java-21", "bin", "java.exe")) {
		t.Errorf("换 Java 的预览：%+v", c)
	}
	if _, err := reg.Confirm(ctx, human, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if i, _ := mgr.Get("整合包"); func() bool { m, jp := i.JavaSnapshot(); return m != 21 || !strings.Contains(jp, "java-21") }() {
		t.Error("没换成 Java 21")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "run.bat")); !strings.Contains(string(b), "java-21") {
		t.Error("启动脚本没重新生成")
	}
	if _, err := reg.Do(ctx, bot, "instance.java.set", ops.Args(`{"inst":"整合包","major":21,"path":"C:\\evil\\java.exe"}`)); ops.CodeOf(err) != 400 {
		t.Errorf("不认识的 Java 应拒绝：%v", err)
	}

	// 重启：没在运行时连提案都不生成；是危险操作
	if _, err := reg.Do(ctx, bot, "instance.restart", ops.Args(`{"inst":"整合包"}`)); ops.CodeOf(err) != 400 {
		t.Errorf("没在运行时不能重启：%v", err)
	}
	if reg.Op("instance.restart").Level != ops.Danger {
		t.Error("重启应是危险操作")
	}

	// 新建实例：AI 传的 eula 去掉；确认时不勾同意不执行，勾了才带上 eula（这里实例名已占用，校验过了 EULA 才报重名）
	res, err = reg.Do(ctx, bot, "instance.create", ops.Args(`{"name":"整合包","core":"paper","mc":"1.21.4","eula":true,"root":"C:\\x"}`))
	if err != nil {
		t.Fatal(err)
	}
	p = res.(*ops.Proposal)
	if strings.Contains(string(p.Args), "eula") || strings.Contains(string(p.Args), "root") || p.Consent == nil || p.Consent.URL != "https://aka.ms/MinecraftEULA" {
		t.Fatalf("新建实例的提案：%s %+v", p.Args, p.Consent)
	}
	if _, err := reg.ConfirmWith(ctx, human, p.ID, ops.ConfirmInput{}); err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Fatalf("不勾同意不能执行：%v", err)
	}
	if _, err := reg.ConfirmWith(ctx, human, p.ID, ops.ConfirmInput{Consent: true}); err == nil || !strings.Contains(err.Error(), "实例名已存在") {
		t.Fatalf("勾了同意应带上 eula 执行（校验到实例名才失败）：%v", err)
	}
	if strings.Contains(string(reg.Op("instance.create").AISchema()), "eula") {
		t.Error("AI 的参数说明里不该有 eula")
	}
	// 代理端不用同意
	res, _ = reg.Do(ctx, bot, "instance.create", ops.Args(`{"name":"代理","core":"velocity","mc":"3.4.0"}`))
	if res.(*ops.Proposal).Consent != nil {
		t.Error("代理端不用同意 EULA")
	}
}
