package transfer

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkInst 在临时目录里造一个能代表真实实例的目录树。
func mkInst(t *testing.T) (dir string, instance json.RawMessage) {
	t.Helper()
	dir = t.TempDir()
	w := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("instance.json", `{"name":"生存服","core":"neoforge","mc":"1.21.1","build":"21.1.265","javaMajor":21,"xmxMb":6144,"xmsMb":1024,"createdAt":"2026-07-01T10:00:00Z"}`)
	w("server.properties", "server-port=25570\nlevel-name=world\nmotd=A Minecraft Server")
	w("eula.txt", "#Tue Jul 01\neula=true\n")
	w("run.bat", "@echo off\nserver run\n") // MineDeck 簿记：打包应剔除
	w("world/level.dat", " LevelData ")
	w("world/playerdata/uuid1.dat", " player1-progress ") // 玩家进度必须在包里
	w("world/session.lock", "lock")                       // 任何深度的 session.lock 都剔除
	w("world_nether/level.dat", " nether ")
	w("mods/a.jar", "PK-a-not-a-realjar") // 内容无所谓，modinfo 读不了也不报错
	w("mods/b.jar", "PK-b")
	w("config/foo.toml", "key = 1\n")
	w("logs/latest.log", "Sticky server log") // distributable 剔除
	w("crash-reports/crash-1.txt", " crash ") // distributable 剔除
	w(".minedeck/config-backups/server.properties.bak", " old props ")
	w("usercache.json", "cache") // distributable 剔除
	b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil {
		t.Fatal(err)
	}
	return dir, json.RawMessage(b)
}

func TestPackInspectUnpackRoundtrip(t *testing.T) {
	srcDir, instance := mkInst(t)
	dest := filepath.Join(t.TempDir(), "包.mdpack")
	when := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	calls, last := 0, int64(-1)
	m, err := Pack(dest, PackOptions{
		Dir: srcDir, Instance: instance, Mode: ModeDistributable,
		ModDir: "mods", Exporter: "MineDeck 3.0.0", Now: when,
		Progress: func(done, total int64) { calls++; last = done },
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.FormatVersion != 1 || m.Mode != ModeDistributable || m.ExporterVersion != "MineDeck 3.0.0" {
		t.Errorf("manifest 不对: %+v", m)
	}
	if !m.ExportedAt.Equal(when) {
		t.Errorf("导出时间应使用注入值，得到 %v", m.ExportedAt)
	}
	if !jsonEqual(m.Instance, instance) {
		t.Errorf("instance.json 原文应原样进 manifest")
	}
	if m.WorldSize <= 0 {
		t.Errorf("世界大小应含 level.dat 和 playerdata，得到 %d", m.WorldSize)
	}
	if calls < 5 || last <= 0 {
		t.Errorf("进度回调应逐文件推进（calls=%d done=%d）", calls, last)
	}
	for _, e := range []string{"logs", "crash-reports", ".minedeck", "usercache.json"} {
		if !containsFold(m.Excluded, e) {
			t.Errorf("剔除清单应含 %s: %v", e, m.Excluded)
		}
	}
	if len(m.Mods) != 2 {
		t.Errorf("模组清单应列 2 个 jar（读不了的也列），得到 %d", len(m.Mods))
	}

	sum, err := Inspect(dest)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Instance.Name != "生存服" || sum.Instance.Core != "neoforge" || sum.Instance.MC != "1.21.1" || sum.Instance.JavaMajor != 21 {
		t.Errorf("清点摘要不对: %+v", sum.Instance)
	}
	if sum.Port != 25570 || !sum.EULAAgreed {
		t.Errorf("端口 / EULA 应从包内文件读出: port=%d eula=%v", sum.Port, sum.EULAAgreed)
	}
	if sum.WorldSize != m.WorldSize || sum.ModCount != 2 {
		t.Errorf("摘要应照 manifest: world=%d mods=%d", sum.WorldSize, sum.ModCount)
	}
	if sum.Mode != ModeDistributable || sum.Exporter != "MineDeck 3.0.0" || !sum.ExportedAt.Equal(when) {
		t.Errorf("清点出的模式/版本/时间不对: %+v", sum)
	}

	// unpack 到另一个目录，剔除的不在，其余逐字节一致
	outDir := filepath.Join(t.TempDir(), "inst")
	n, err := Unpack(dest, outDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertFile := func(rel, want string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("缺文件 %s: %v", rel, err)
			return
		}
		if string(b) != want {
			t.Errorf("%s 内容不对", rel)
		}
	}
	assertFile("server.properties", "server-port=25570\nlevel-name=world\nmotd=A Minecraft Server")
	assertFile("eula.txt", "#Tue Jul 01\neula=true\n")
	assertFile("world/playerdata/uuid1.dat", " player1-progress ")
	assertFile("config/foo.toml", "key = 1\n")
	assertFile("mods/a.jar", "PK-a-not-a-realjar")
	for _, gone := range []string{"instance.json", "run.bat", "logs/latest.log", "crash-reports/crash-1.txt", ".minedeck/config-backups/server.properties.bak", "usercache.json", "world/session.lock", "mdpack.json"} {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Errorf("剔除的 %s 不该出现在解包结果里", gone)
		}
	}
	if n != 8 { // server.properties, eula.txt, world×2, world_nether, mods×2, config/foo.toml
		t.Errorf("应解出 8 个文件，得到 %d", n)
	}
}

func TestPackHandoverKeepsRecords(t *testing.T) {
	srcDir, instance := mkInst(t)
	dest := filepath.Join(t.TempDir(), "h.mdpack")
	m, err := Pack(dest, PackOptions{Dir: srcDir, Instance: instance, Mode: ModeHandover, ModDir: "mods"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"logs", "crash-reports", "usercache.json"} {
		if containsFold(m.Excluded, e) {
			t.Errorf("交接模式应保留 %s", e)
		}
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"logs/latest.log", "crash-reports/crash-1.txt", "usercache.json"} {
		if !names[want] {
			t.Errorf("交接包应含 %s", want)
		}
	}
	if names[".minedeck/config-backups/server.properties.bak"] {
		t.Errorf("交接包也不该带 MineDeck 的簿记目录")
	}
}

func TestExtraExcludesAndBadExcludes(t *testing.T) {
	srcDir, instance := mkInst(t)
	dest := filepath.Join(t.TempDir(), "x.mdpack")
	m, err := Pack(dest, PackOptions{Dir: srcDir, Instance: instance, Mode: ModeDistributable,
		ModDir: "mods", Excludes: []string{"config", "world_nether"}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsFold(m.Excluded, "config") || !containsFold(m.Excluded, "world_nether") {
		t.Errorf("额外剔除应原样进清单: %v", m.Excluded)
	}
	_, err = Unpack(dest, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// 路径分隔符不一样的写法也该认
	if !Excluded([]string{"a/b"}, "a/b", false) || Excluded([]string{"a/b"}, "a/bc", false) {
		t.Errorf("带斜杠的剔除应按整段匹配")
	}
	if !Excluded([]string{"logs"}, "world/logs/latest.log", true) {
		t.Errorf("不带斜杠的剔除应任何深度同名")
	}
	// 危险写法拒绝
	if _, err := Pack(filepath.Join(t.TempDir(), "bad.mdpack"), PackOptions{Dir: srcDir, Instance: instance,
		Mode: ModeDistributable, ModDir: "mods", Excludes: []string{"../别的服"}}); err == nil {
		t.Errorf("剔除清单带 .. 应被拒绝")
	}
}

func TestInspectRejects(t *testing.T) {
	tmp := t.TempDir()
	// 不是 zip
	p := filepath.Join(tmp, "empty.mdpack")
	os.WriteFile(p, []byte("not a zip"), 0o644)
	if _, err := Inspect(p); err == nil {
		t.Fatalf("非 zip 应报错")
	} else if !strings.Contains(err.Error(), "有效的 .mdpack") {
		t.Errorf("错误文案不像包格式错误: %v", err)
	}
	// 是 zip 但没有 manifest
	p2 := filepath.Join(tmp, "nomanifest.mdpack")
	newZip(t, p2, map[string]string{"server.properties": "server-port=25565"})
	if _, err := Inspect(p2); err == nil {
		t.Errorf("没有 mdpack.json 应报错")
	}
	// manifest 版本不对
	p3 := filepath.Join(tmp, "future.mdpack")
	newZip(t, p3, map[string]string{"mdpack.json": `{"formatVersion":99,"mode":"distributable","instance":{},"exportedAt":"2026-01-01T00:00:00Z","worldSize":1,"mods":[],"excluded":[]}`})
	if _, err := Inspect(p3); err == nil || !strings.Contains(err.Error(), "版本") {
		t.Errorf("不认识的格式版本应报错: %v", err)
	}
	// mode 不认识
	p4 := filepath.Join(tmp, "badmode.mdpack")
	newZip(t, p4, map[string]string{"mdpack.json": `{"formatVersion":1,"mode":"weird","instance":{},"exportedAt":"2026-01-01T00:00:00Z"}`})
	if _, err := Inspect(p4); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Errorf("不认识的 mode 应报错: %v", err)
	}
	// instance 原文缺失
	p5 := filepath.Join(tmp, "noinstance.mdpack")
	newZip(t, p5, map[string]string{"mdpack.json": `{"formatVersion":1,"mode":"handover","exportedAt":"2026-01-01T00:00:00Z"}`})
	if _, err := Inspect(p5); err == nil || !strings.Contains(err.Error(), "instance") {
		t.Errorf("缺 instance 原文应报错: %v", err)
	}
	// 不存在的文件
	if _, err := Inspect(filepath.Join(tmp, "nothere.mdpack")); err == nil {
		t.Errorf("不存在的文件应报错")
	}
}

func TestUnpackZipSlip(t *testing.T) {
	// 手造带穿越条目的 zip
	p := filepath.Join(t.TempDir(), "evil.mdpack")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"../evil.txt", "C:/evil.txt", "a/../../evil.txt", "ok.txt"} {
		w, _ := zw.Create(name)
		w.Write([]byte("evil"))
	}
	zw.Close()
	f.Close()
	out := filepath.Join(t.TempDir(), "out")
	_, err = Unpack(p, out, nil)
	if err == nil || !strings.Contains(err.Error(), "不安全") {
		t.Fatalf("穿越条目应报错: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(filepath.Dir(out), "evil.txt")); serr == nil {
		t.Errorf("zip-slip：外面的 evil.txt 被写出来了")
	}
}

func TestUnpackProgressAndDirs(t *testing.T) {
	srcDir, instance := mkInst(t)
	dest := filepath.Join(t.TempDir(), "withdir.mdpack")
	if _, err := Pack(dest, PackOptions{Dir: srcDir, Instance: instance, Mode: ModeHandover, ModDir: "mods"}); err != nil {
		t.Fatal(err)
	}
	var gotDone, gotTotal int64
	n, err := Unpack(dest, filepath.Join(t.TempDir(), "o"), func(done, total int64) { gotDone, gotTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	if n < 8 || gotDone != gotTotal || gotTotal <= 0 {
		t.Errorf("进度没走全: n=%d done=%d total=%d", n, gotDone, gotTotal)
	}
}

func TestPropsValue(t *testing.T) {
	b := []byte("ï»¿#MineDeck\nserver-port=25570\r\nlevel-name = 我的存档\n")
	if v := propsValue(b, "server-port"); v != "25570" {
		t.Errorf("server-port 应为 25570，得到 %q", v)
	}
	if v := propsValue(b, "level-name"); v != "我的存档" {
		t.Errorf("等号两侧的空白应去掉，得到 %q", v)
	}
	if v := propsValue(b, "none"); v != "" {
		t.Errorf("不存在的键应为空")
	}
	if eulaAgreed([]byte("eula=true")) != true || eulaAgreed([]byte("eula=false")) {
		t.Errorf("eula 识别不对")
	}
}

func TestExcludedSegments(t *testing.T) {
	ex := []string{".minedeck", "instance.json", "run.bat", "session.lock", "logs", "crash-reports", "usercache.json"}
	cases := []struct {
		rel  string
		dir  bool
		want bool
	}{
		{"instance.json", false, true},
		{"INSTANCE.JSON", false, true}, // 大小写不敏感
		{"run.bat", false, true},
		{"world/session.lock", false, true},
		{"logs", true, true},
		{"logs/latest.log", false, true},
		{"world", true, false},
		{"world/level.dat", false, false},
		{"logsdir/x", false, false}, // 前缀必须整段
		{"mods", true, false},
	}
	for _, c := range cases {
		if got := Excluded(ex, c.rel, c.dir); got != c.want {
			t.Errorf("Excluded(%q, dir=%v) = %v，应为 %v", c.rel, c.dir, got, c.want)
		}
	}
}

// ---- 小工具 ----

func newZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	xb, _ := json.Marshal(y)
	return string(xa) == string(xb)
}

func containsFold(list []string, needle string) bool {
	for _, e := range list {
		if strings.EqualFold(e, needle) {
			return true
		}
	}
	return false
}
