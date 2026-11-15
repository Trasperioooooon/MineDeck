package inst

// beta.3 第二层，服务器详情页（docs/可用性走查-2026-11-14.md 第八节 R10、R11、R21）。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
)

// R11：备份文件名里的标签拆得出来，列表里带上（界面优先显示它）；没填标签、不是 MineDeck 起名的没有。
func TestBackupLabelParsed(t *testing.T) {
	for file, want := range map[string]string{
		"20261115-203000.123456789-更新模组前.zip": "更新模组前",
		"20261115-203000.123456789.zip":       "",
		"20261115-203000-还原前自动.zip":           "还原前自动",
		"自己放进来的.zip":                          "",
	} {
		_, label, _ := parseBackupName(file)
		if label != want {
			t.Errorf("%s 的标签：%q，应为 %q", file, label, want)
		}
	}
	m, _ := tempInstance(t)
	f, err := m.CreateBackup("s", "打龙前")
	if err != nil {
		t.Fatal(err)
	}
	list, err := m.ListBackups("s")
	if err != nil || len(list) != 1 || list[0].File != f || list[0].Label != "打龙前" {
		t.Fatalf("列表：%+v %v", list, err)
	}
}

// R11：还原以后记下现在的世界是从哪份备份来的，写进 instance.json（重开程序后仍在）；再还原一份就换成那一份。
func TestRestoreRecordsWorldFrom(t *testing.T) {
	m, dir := tempInstance(t)
	first, err := m.CreateBackup("s", "第一份")
	if err != nil {
		t.Fatal(err)
	}
	writeLevel(t, dir, "changed")
	if err := m.RestoreBackup("s", first); err != nil {
		t.Fatal(err)
	}
	i, _ := m.Get("s")
	wf := i.WorldFromSnapshot()
	if wf == nil || wf.Backup != first || wf.Label != "第一份" || wf.BackupTime == "" || wf.At == "" {
		t.Fatalf("还原以后的记录：%+v", wf)
	}
	s, err := readSettings(dir)
	if err != nil || s.WorldFrom == nil || s.WorldFrom.Backup != first {
		t.Fatalf("instance.json 里应记着：%+v %v", s.WorldFrom, err)
	}
	// 再还原另一份（还原前自动备份的那一份）：记录换成它
	list, _ := m.ListBackups("s")
	var auto string
	for _, b := range list {
		if b.Label == "还原前自动" {
			auto = b.File
		}
	}
	if auto == "" {
		t.Fatalf("应有还原前自动备份：%+v", list)
	}
	if err := m.RestoreBackup("s", auto); err != nil {
		t.Fatal(err)
	}
	if wf := i.WorldFromSnapshot(); wf == nil || wf.Backup != auto || wf.Label != "还原前自动" {
		t.Fatalf("第二次还原以后的记录：%+v", wf)
	}
}

// R11：换地图（导入占位、换回旧地图、恢复地图）以后不再说世界是从备份来的；删除恢复点、删除插件这类不动世界的占位不清。
func TestWorldChangeClearsWorldFrom(t *testing.T) {
	m, dir := tempInstance(t)
	i, _ := m.Get("s")
	set := func() { m.setWorldFrom(i, &WorldFrom{Backup: "a.zip", At: "2026-11-15 20:00:00"}) }
	saved := func() *WorldFrom {
		s, err := readSettings(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s.WorldFrom
	}
	for _, c := range []struct {
		why   string
		clear bool
	}{{"换回旧地图", true}, {"恢复地图", true}, {"删除恢复点", false}, {modTrashWhy, false}} {
		set()
		release, err := m.BeginWorldChangeInstance(i, c.why)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if got := i.WorldFromSnapshot(); (got == nil) != c.clear || (saved() == nil) != c.clear {
			t.Fatalf("%s 以后：内存 %+v，instance.json %+v，应%s", c.why, got, saved(), map[bool]string{true: "清掉", false: "留着"}[c.clear])
		}
	}
	// 导入地图：占上位（Change）以后才算换过；只登记了意图、没占上位就放弃的不清
	set()
	w, err := m.BeginWorldIntentInstance(i, "准备换地图")
	if err != nil {
		t.Fatal(err)
	}
	w.Release()
	if i.WorldFromSnapshot() == nil {
		t.Fatal("没占上位就放弃的导入不该清掉记录")
	}
	w, err = m.BeginWorldIntentInstance(i, "准备换地图")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Change(); err != nil {
		t.Fatal(err)
	}
	w.Release()
	if i.WorldFromSnapshot() != nil || saved() != nil {
		t.Fatal("导入地图以后应清掉记录")
	}
}

// R11 换地图收尾（2026-11-16 Codex 复审 beta.3）：清掉 WorldFrom、存 instance.json 要在占位还在的时候做完，最后才放掉占位。
// 以前先放占位再存：中间「移出列表」能进来，把 instance.json 改名成 .bak，晚到的保存又写回一份 instance.json，
// 下次启动扫描存放目录，已经移出去的服务器又被认回来。worldReleaseHook 停在存盘之前（不拿任何锁），这时候去移出列表：
// 要么被占位挡下（服务器还在、记录清掉并存好了），要么真的移出去了、instance.json 不能再冒出来。换回旧地图的占位和导入地图的意图两条路都看。
func TestWorldReleaseSavesBeforeDroppingHold(t *testing.T) {
	for _, c := range []struct {
		name string
		hold func(t *testing.T, m *Manager, i *Instance) (release func())
	}{
		{"换回旧地图", func(t *testing.T, m *Manager, i *Instance) func() {
			release, err := m.BeginWorldChangeInstance(i, "换回旧地图")
			if err != nil {
				t.Fatal(err)
			}
			return release
		}},
		{"导入地图", func(t *testing.T, m *Manager, i *Instance) func() {
			w, err := m.BeginWorldIntentInstance(i, "准备换地图")
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Change(); err != nil {
				t.Fatal(err)
			}
			return w.Release
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			i, _ := m.Get("s")
			m.setWorldFrom(i, &WorldFrom{Backup: "a.zip", At: "2026-11-15 20:00:00"}) // 还原过备份：收尾时要清掉、存一次盘
			release := c.hold(t, m, i)
			old := worldReleaseHook
			t.Cleanup(func() { worldReleaseHook = old })
			fired, delErr := false, error(nil)
			worldReleaseHook = func(name string) {
				if fired || name != "s" { // 移出列表自己的占位收尾也会走到这里：只在第一次（换地图收尾）去移
					return
				}
				fired = true
				delErr = m.Delete(name, false)
			}
			release()
			if !fired {
				t.Fatal("收尾时没走到存盘之前那一步")
			}
			cfg := filepath.Join(dir, "instance.json")
			if delErr == nil {
				if _, err := os.Stat(cfg); !os.IsNotExist(err) {
					t.Fatalf("移出列表以后 instance.json 又被写回来了，下次启动会被重新扫出来：%v", err)
				}
				return
			}
			if !strings.Contains(delErr.Error(), "请等它完成") {
				t.Fatalf("收尾还没做完时移出列表应被占位挡下：%v", delErr)
			}
			if _, err := m.Get("s"); err != nil {
				t.Fatalf("被挡下的话服务器应还在列表里：%v", err)
			}
			if s, err := readSettings(dir); err != nil || s.WorldFrom != nil {
				t.Fatalf("记录应已清掉并存进 instance.json：%+v %v", s.WorldFrom, err)
			}
			if why := m.WorldBusy("s"); why != "" {
				t.Fatalf("收尾以后占位应放掉：%s", why)
			}
			// 收尾做完以后照常能移出列表，instance.json 改名成 .bak、不再回来
			worldReleaseHook = old
			if err := m.Delete("s", false); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(cfg); !os.IsNotExist(err) {
				t.Fatalf("移出列表以后不该还有 instance.json：%v", err)
			}
		})
	}
}

// R21：配的 java.exe 不在了就是 JavaMissing；路径是空的也算。
func TestJavaMissing(t *testing.T) {
	exe, _ := os.Executable()
	i := &Instance{Settings: Settings{JavaPath: exe}}
	if i.JavaMissing() {
		t.Fatal("存在的 java 不该算找不到")
	}
	for _, p := range []string{filepath.Join(t.TempDir(), "没有", "java.exe"), "", t.TempDir()} {
		i.JavaPath = p
		if !i.JavaMissing() {
			t.Fatalf("%q 应算找不到", p)
		}
	}
}

// R10：删除插件或模组只是移进 .minedeck/已删除/<文件夹>/，能找回；同名的不覆盖，加编号；
// 服务器开着、文件名带路径、不是 jar、不是普通文件的一律不动。
func TestTrashMod(t *testing.T) {
	m, i := portInst(t, "删插件", "", mcsrc.CorePaper)
	plugins := filepath.Join(i.Dir, "plugins")
	if err := os.MkdirAll(filepath.Join(plugins, "Chunky"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(plugins, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Chunky.jar", "v1")
	to, err := m.TrashMod(i.Name, "plugins", "Chunky.jar")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(".minedeck", "已删除", "plugins", "Chunky.jar"); to != want {
		t.Fatalf("移到了 %q，应为 %q", to, want)
	}
	if _, err := os.Stat(filepath.Join(plugins, "Chunky.jar")); !os.IsNotExist(err) {
		t.Fatal("原来的文件应不在了")
	}
	if b, _ := os.ReadFile(filepath.Join(i.Dir, to)); string(b) != "v1" {
		t.Fatalf("移走的内容：%q", b)
	}
	// 再删一次同名的（停用的也能删）：不覆盖第一次的，加编号
	write("Chunky.jar", "v2")
	if to, err = m.TrashMod(i.Name, "plugins", "Chunky.jar"); err != nil || !strings.HasSuffix(to, "Chunky (2).jar") {
		t.Fatalf("第二次：%q %v", to, err)
	}
	write("Old.jar.disabled", "off")
	if to, err = m.TrashMod(i.Name, "plugins", "Old.jar.disabled"); err != nil || !strings.HasSuffix(to, "Old.jar.disabled") {
		t.Fatalf("停用的：%q %v", to, err)
	}
	if b, _ := os.ReadFile(filepath.Join(i.Dir, ".minedeck", "已删除", "plugins", "Chunky.jar")); string(b) != "v1" {
		t.Fatalf("第一次删掉的被覆盖了：%q", b)
	}
	// 不认的：带路径、往外跳、不是 jar、文件夹、不是 mods / plugins、没有这个文件
	write("readme.txt", "x")
	for _, c := range [][2]string{{"plugins", "../server.properties"}, {"plugins", `sub\a.jar`}, {"plugins", "a.jar:x"}, {"plugins", "a.jar."},
		{"plugins", "readme.txt"}, {"plugins", "Chunky"}, {"world", "a.jar"}, {"plugins", "没有.jar"}, {"plugins", " Chunky.jar"}} {
		if _, err := m.TrashMod(i.Name, c[0], c[1]); err == nil {
			t.Errorf("%s/%s 应拒绝", c[0], c[1])
		}
	}
	if _, err := os.Stat(filepath.Join(plugins, "readme.txt")); err != nil {
		t.Fatal("拒绝的不该动文件")
	}
	// 服务器开着：不动
	write("Run.jar", "x")
	i.procMu.Lock()
	i.state = "running"
	i.procMu.Unlock()
	if _, err := m.TrashMod(i.Name, "plugins", "Run.jar"); err == nil || !strings.Contains(err.Error(), "先停止服务器") {
		t.Fatalf("运行中应拒绝：%v", err)
	}
	i.procMu.Lock()
	i.state = ""
	i.procMu.Unlock()
	if _, err := os.Stat(filepath.Join(plugins, "Run.jar")); err != nil {
		t.Fatal("运行中拒绝时文件应还在")
	}
	// 删的时候占着位：结束以后能照常启动（占位放掉了）
	if why := m.WorldBusy(i.Name); why != "" {
		t.Fatalf("删完应放掉占位：%s", why)
	}
}

// R10 删除插件或模组（2026-11-16 Codex 复审 beta.3）：接管来的服务器里 plugins、.minedeck 或它下面的「已删除」是指到外面的目录联接时，
// 只看末级是普通文件拦不住——顺着联接会把外面的文件挪进来，或者在外面建文件夹、放进移走的文件。这几种都要拒绝，外面一个字节都不动。
// 联接指到服务器文件夹里面的照常能删。
func TestTrashModRefusesJunctionsOutside(t *testing.T) {
	junction := func(link, target string) {
		t.Helper()
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		hideWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("建不了目录联接: %v %s", err, out)
		}
		t.Cleanup(func() { _ = os.Remove(link) }) // 只删联接本身；在临时目录之后登记，先于临时目录删
	}
	trash := func(m *Manager, i *Instance, file string) error {
		_, err := m.TrashMod(i.Name, "plugins", file)
		return err
	}

	// plugins 整个是指到外面的联接：外面那个文件夹里的 jar 不能被挪进来
	m, i := portInst(t, "联接插件", "", mcsrc.CorePaper)
	outside := t.TempDir()
	writeTree(t, filepath.Join(outside, "plugins"), map[string]string{"Victim.jar": "外面的"})
	junction(filepath.Join(i.Dir, "plugins"), filepath.Join(outside, "plugins"))
	before := treeOf(t, outside)
	if err := trash(m, i, "Victim.jar"); err == nil || !strings.Contains(err.Error(), "plugins 文件夹指到了服务器文件夹外面") {
		t.Fatalf("plugins 指到外面时应拒绝删除：%v", err)
	}
	if got := treeOf(t, outside); !maps2Equal(before, got) {
		t.Fatalf("外面的文件被动了：\n之前 %v\n之后 %v", before, got)
	}
	if _, err := os.Lstat(filepath.Join(i.Dir, ".minedeck")); !os.IsNotExist(err) {
		t.Fatalf("拒绝了就不该建放删除文件的文件夹：%v", err)
	}

	// .minedeck，或者它下面的「已删除」、「已删除/plugins」指到外面：不在外面建文件夹、放文件，服务器里的 jar 原地不动
	for _, rel := range []string{".minedeck", ".minedeck/已删除", ".minedeck/已删除/plugins"} {
		m, i := portInst(t, "联接回收", "", mcsrc.CorePaper)
		outside := t.TempDir()
		writeTree(t, filepath.Join(i.Dir, "plugins"), map[string]string{"Mine.jar": "自己的"})
		link := filepath.Join(i.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		junction(link, outside)
		if err := trash(m, i, "Mine.jar"); err == nil || !strings.Contains(err.Error(), ".minedeck 文件夹指到了服务器文件夹外面") {
			t.Fatalf("%s 指到外面时应拒绝删除：%v", rel, err)
		}
		if got := treeOf(t, outside); len(got) != 0 {
			t.Fatalf("%s 指到外面时，外面多出了文件：%v", rel, got)
		}
		if ents, _ := os.ReadDir(outside); len(ents) != 0 {
			t.Fatalf("%s 指到外面时，外面多出了文件夹：%v", rel, ents)
		}
		if b, err := os.ReadFile(filepath.Join(i.Dir, "plugins", "Mine.jar")); err != nil || string(b) != "自己的" {
			t.Fatalf("%s：拒绝了 jar 应原地不动：%q %v", rel, b, err)
		}
	}

	// 联接指到服务器文件夹里面（比如 plugins → 服务器里另一个文件夹）：照常删，移进 .minedeck/已删除/plugins
	m, i = portInst(t, "联接在里面", "", mcsrc.CorePaper)
	writeTree(t, filepath.Join(i.Dir, "插件本体"), map[string]string{"Inner.jar": "里面的"})
	junction(filepath.Join(i.Dir, "plugins"), filepath.Join(i.Dir, "插件本体"))
	to, err := m.TrashMod(i.Name, "plugins", "Inner.jar")
	if err != nil {
		t.Fatalf("联接指到服务器文件夹里面的应照常删除：%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(i.Dir, to)); err != nil || string(b) != "里面的" {
		t.Fatalf("移到 %s 的内容：%q %v", to, b, err)
	}
}

// R11：instance.json 里的 worldFrom 读得回来（重开程序后仍在）。
func TestWorldFromJSON(t *testing.T) {
	s := Settings{Name: "x", WorldFrom: &WorldFrom{Backup: "b.zip", At: "t"}}
	b, _ := json.Marshal(s)
	var back Settings
	if err := json.Unmarshal(b, &back); err != nil || back.WorldFrom == nil || back.WorldFrom.Backup != "b.zip" {
		t.Fatalf("%s → %+v %v", b, back.WorldFrom, err)
	}
	b, _ = json.Marshal(Settings{Name: "x"})
	if strings.Contains(string(b), "worldFrom") {
		t.Fatalf("没有记录时不写这一项：%s", b)
	}
}

// 26.1 起难度存在世界里，开服完成时按 server.properties 补一条命令；更早的版本服务端自己会读，不补。
func TestDifficultyCommand(t *testing.T) {
	cases := []struct {
		mc, val, want string
	}{
		{"1.21.11", "hard", ""},
		{"1.20.1", "hard", ""},
		{"26.1", "hard", "difficulty hard"},
		{"26.3", "Peaceful", "difficulty peaceful"},
		{"26.3-snapshot-2", "normal", "difficulty normal"},
		{"26.3", "3", "difficulty hard"},
		{"26.3", "", ""},
		{"26.3", "extreme", ""},
		{"", "hard", ""},
	}
	for _, c := range cases {
		props := map[string]string{}
		if c.val != "" {
			props["difficulty"] = c.val
		}
		if got := difficultyCommand(c.mc, props); got != c.want {
			t.Errorf("mc=%q difficulty=%q: got %q want %q", c.mc, c.val, got, c.want)
		}
	}
	// 极限模式不发；hardcore=false 照常发
	if got := difficultyCommand("26.3", map[string]string{"difficulty": "easy", "hardcore": "true"}); got != "" {
		t.Errorf("hardcore=true: got %q, want no command", got)
	}
	if got := difficultyCommand("26.3", map[string]string{"difficulty": "easy", "hardcore": "false"}); got != "difficulty easy" {
		t.Errorf("hardcore=false: got %q", got)
	}
}
