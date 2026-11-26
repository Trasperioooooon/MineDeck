package inst

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// 用户反馈（2026-11-27）：「每次返回到备份的存档，会把服务器设置重置，正版验证、最大视距这些」。
// 还原备份只换世界：server.properties（正版验证、视距……）和别的不是世界的文件，还原以后和还原前一模一样。

// propsNow 还原前这台服务器现在的设置：和备份里那份（或者默认值）都不一样。
const propsNow = "#Minecraft server properties\nlevel-name=world\nonline-mode=false\nview-distance=20\nmotd=现在的服务器\n"

// nonWorldFiles 实例目录里世界以外的文件（斜杠分隔的相对路径 → 内容）。instance.json 是 MineDeck 自己记账的
// （还原以后要记下现在的世界从哪份备份来），不算服务器的设置，不比。
func nonWorldFiles(t *testing.T, dir string, worlds ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if slices.Contains(worlds, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "instance.json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readRel(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读 %s：%v", rel, err)
	}
	return string(b)
}

// putBackup 在实例 s 的备份文件夹里放一份手工打的备份（entries 是「路径=内容」），返回文件名。
func putBackup(t *testing.T, file string, entries map[string]string) string {
	t.Helper()
	keys := slices.Sorted(maps.Keys(entries))
	spec := make([]string, 0, len(keys))
	for _, k := range keys {
		spec = append(spec, k+"="+entries[k])
	}
	writeTree(t, backupDirOf("s"), map[string]string{file: "zip:" + strings.Join(spec, "|")})
	return file
}

// MineDeck 自己做的备份：包里只有世界。改完正版验证、视距再还原，设置原样、世界回到备份那一刻。
func TestRestoreRealBackupKeepsSettings(t *testing.T) {
	m, dir := tempInstance(t)
	writeTree(t, dir, map[string]string{
		"server.properties":       "level-name=world\nonline-mode=true\nview-distance=10\n",
		"ops.json":                "[]",
		"config/paper-global.yml": "a: 1\n",
	})
	file, err := m.CreateBackup("s", "")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(filepath.Join(backupDirOf("s"), file))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "world/") {
			t.Errorf("备份里应当只有世界，多了 %s", f.Name)
		}
	}
	zr.Close()

	writeTree(t, dir, map[string]string{"server.properties": propsNow, "ops.json": `[{"name":"Steve"}]`})
	writeLevel(t, dir, "之后又玩了一阵")
	before := nonWorldFiles(t, dir, "world")
	if err := m.RestoreBackup("s", file); err != nil {
		t.Fatal(err)
	}
	if got := readLevel(t, dir); got != "current" {
		t.Errorf("世界应回到备份里的 current，实际 %q", got)
	}
	if after := nonWorldFiles(t, dir, "world"); !maps.Equal(before, after) {
		t.Errorf("世界以外的文件被还原动了：\n还原前 %q\n还原后 %q", before, after)
	}
}

// 包里带着 server.properties 等文件的备份（自己整个服务器文件夹打的包放进备份文件夹、别的工具做的）：
// 只还原世界，正版验证、视距、OP 名单这些不盖，包里的模组、配置也不往外放。
func TestRestoreOldFormatBackupKeepsSettings(t *testing.T) {
	m, dir := tempInstance(t)
	writeTree(t, dir, map[string]string{
		"server.properties": propsNow,
		"ops.json":          `[{"name":"现在的OP"}]`,
		"world/stale.dat":   "只有现在的世界里有",
	})
	file := putBackup(t, "20200101-000000.zip", map[string]string{
		"server.properties":          "level-name=world\nonline-mode=true\nview-distance=10\nmotd=备份里的\n",
		"ops.json":                   `[{"name":"以前的OP"}]`,
		"whitelist.json":             "[]",
		"eula.txt":                   "eula=true",
		"mods/old.jar":               "jar",
		"config/x.toml":              "x=1",
		"world/level.dat":            "backup",
		"world/region/r.0.0.mca":     "region",
		"world_nether/level.dat":     "backup nether",
		"world_the_end/DIM1/r.0.0.m": "end",
	})
	before := nonWorldFiles(t, dir, "world", "world_nether", "world_the_end")
	if err := m.RestoreBackup("s", file); err != nil {
		t.Fatal(err)
	}
	if after := nonWorldFiles(t, dir, "world", "world_nether", "world_the_end"); !maps.Equal(before, after) {
		t.Errorf("世界以外的文件被还原动了：\n还原前 %q\n还原后 %q", before, after)
	}
	for rel, want := range map[string]string{
		"world/level.dat":            "backup",
		"world/region/r.0.0.mca":     "region",
		"world_nether/level.dat":     "backup nether",
		"world_the_end/DIM1/r.0.0.m": "end",
	} {
		if got := readRel(t, dir, rel); got != want {
			t.Errorf("%s = %q，应为备份里的 %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "world", "stale.dat")); !os.IsNotExist(err) {
		t.Errorf("旧世界里的文件应随旧世界删掉，不和备份混在一起：%v", err)
	}
}

// 备份时和还原时世界名（level-name）不一样：备份里的世界放进现在的世界名下面（server.properties 不改，
// 服务器开起来读的就是还原出来的这一份），不相干的文件夹不动。
func TestRestoreBackupIntoCurrentLevelName(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"备份时是 world，现在是 survival", "world", "survival"},
		{"备份时带子目录", "worlds/main", "world"},
		{"现在带子目录", "world", "worlds/main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			_ = os.RemoveAll(filepath.Join(dir, "world"))
			props := "level-name=" + tc.to + "\nonline-mode=false\nview-distance=20\n"
			writeTree(t, dir, map[string]string{
				"server.properties":         props,
				tc.to + "/level.dat":        "current",
				tc.to + "/stale.dat":        "只有现在的世界里有",
				tc.to + "_nether/level.dat": "current nether",
				"unrelated/level.dat":       "别的世界，不相干",
			})
			file := putBackup(t, "20200101-000000.zip", map[string]string{
				tc.from + "/level.dat":         "backup",
				tc.from + "/region/r.0.0.mca":  "region",
				tc.from + "_nether/level.dat":  "backup nether",
				tc.from + "_the_end/level.dat": "backup end",
			})
			if err := m.RestoreBackup("s", file); err != nil {
				t.Fatal(err)
			}
			for rel, want := range map[string]string{
				tc.to + "/level.dat":         "backup",
				tc.to + "/region/r.0.0.mca":  "region",
				tc.to + "_nether/level.dat":  "backup nether",
				tc.to + "_the_end/level.dat": "backup end",
				"server.properties":          props,
				"unrelated/level.dat":        "别的世界，不相干",
			} {
				if got := readRel(t, dir, rel); got != want {
					t.Errorf("%s = %q，应为 %q", rel, got, want)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tc.to), "stale.dat")); !os.IsNotExist(err) {
				t.Errorf("旧世界里的文件应随旧世界删掉：%v", err)
			}
			if tc.from != tc.to {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tc.from))); !os.IsNotExist(err) {
					t.Errorf("备份里的世界应放进现在的世界名 %s，不该另起一个 %s：%v", tc.to, tc.from, err)
				}
			}
		})
	}
}

// 包里有好几个世界（多世界插件的服务器整个打的包）：有现在这个世界名的就还原它；没有时按包里 server.properties 的
// level-name 认（只读，不写回去）。别的世界不往外放。
func TestRestorePicksWorldAmongSeveral(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
	}{
		{"包里有现在这个世界名", map[string]string{
			"world/level.dat": "backup", "creative/level.dat": "另一个世界",
		}},
		{"按包里 server.properties 的世界名", map[string]string{
			"server.properties": "level-name=lobby\nonline-mode=true\n",
			"lobby/level.dat":   "backup", "minigame/level.dat": "另一个世界",
		}},
		{"包里 server.properties 的世界名优先于现在的世界名", map[string]string{
			"server.properties": "level-name=lobby\n",
			"lobby/level.dat":   "backup", "world/level.dat": "打包时没在用的世界",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			writeTree(t, dir, map[string]string{"server.properties": propsNow})
			file := putBackup(t, "20200101-000000.zip", tc.entries)
			if err := m.RestoreBackup("s", file); err != nil {
				t.Fatal(err)
			}
			if got := readLevel(t, dir); got != "backup" {
				t.Errorf("世界应还原成备份里的那个，实际 %q", got)
			}
			if got := readRel(t, dir, "server.properties"); got != propsNow {
				t.Errorf("设置被改了：%q", got)
			}
			for _, other := range []string{"creative", "minigame", "lobby"} {
				if _, err := os.Stat(filepath.Join(dir, other)); !os.IsNotExist(err) {
					t.Errorf("别的世界 %s 不该放出来：%v", other, err)
				}
			}
		})
	}
}

// 不能还原的几种：认不出该还原哪个世界（包里没有世界、只有下界末地没有主世界、有好几个又定不下来），
// 包里有一条非法路径（哪怕不在世界里），世界文件夹的位置上是一个普通文件。都是一开始就停下：
// 世界（三个维度）、设置、备份列表都不动，也不先做「还原前自动」。
func TestRestoreRefusesWhenWorldUnclear(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
		files   map[string]string // 服务器文件夹里另外放的
		link    string            // 包里另加一项这个名字的符号链接
		msg     string
	}{
		{name: "包里没有世界", entries: map[string]string{"server.properties": "online-mode=true\n", "ops.json": "[]"}, msg: "没找到世界"},
		{name: "好几个世界定不下来", entries: map[string]string{"a/level.dat": "a", "b/level.dat": "b"}, msg: "好几个世界"},
		{name: "只有下界和末地、没有主世界", entries: map[string]string{"world_nether/level.dat": "n", "world_the_end/level.dat": "e"}, msg: "没有主世界"},
		{name: "只有下界、没有主世界", entries: map[string]string{"world_nether/level.dat": "n", "world_nether/DIM-1/r.0.0.mca": "x"}, msg: "没有主世界"},
		{name: "只有下界、没存过盘", entries: map[string]string{"world_nether/DIM-1/r.0.0.mca": "n"}, msg: "没找到世界"},
		{name: "绕回上一级的路径", entries: map[string]string{"world/level.dat": "backup", "other/../world/region/r.0.0.mca": "x"}, msg: "非法路径"},
		{name: "绝对路径", entries: map[string]string{"world/level.dat": "backup", "/etc/evil": "x"}, msg: "非法路径"},
		{name: "盘符", entries: map[string]string{"world/level.dat": "backup", "C:/evil": "x"}, msg: "非法路径"},
		{name: "世界外面的符号链接", entries: map[string]string{"world/level.dat": "backup"}, link: "plugins/evil", msg: "符号链接"},
		{name: "中间一段带冒号（NTFS 数据流）", entries: map[string]string{"world/level.dat": "backup", "world/level.dat:evil": "x"}, msg: "非法路径"},
		{name: "一段以点结尾", entries: map[string]string{"world/level.dat": "backup", "world./region/r.0.0.mca": "x"}, msg: "非法路径"},
		{name: "一段以空格结尾", entries: map[string]string{"world/level.dat": "backup", "world /region/r.0.0.mca": "x"}, msg: "非法路径"},
		{name: "两个只差大小写的世界", entries: map[string]string{"World/level.dat": "a", "world/level.dat": "b"}, msg: "好几个世界"},
		{name: "末地文件夹的位置上是个文件", entries: map[string]string{"world/level.dat": "backup", "world_the_end/level.dat": "backup end"},
			files: map[string]string{"world_the_end": "一个普通文件"}, msg: "是一个文件"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			writeTree(t, dir, map[string]string{"server.properties": propsNow, "world_nether/level.dat": "current nether"})
			writeTree(t, dir, tc.files)
			file := putBackup(t, "20200101-000000.zip", tc.entries)
			if tc.link != "" {
				addZipSymlink(t, filepath.Join(backupDirOf("s"), file), tc.link)
			}
			list := backupFiles(t, m)
			before := nonWorldFiles(t, dir)
			err := m.RestoreBackup("s", file)
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("应当不还原并说明「%s」：%v", tc.msg, err)
			}
			if got := readLevel(t, dir); got != "current" {
				t.Errorf("没还原，世界不该动：%q", got)
			}
			if after := nonWorldFiles(t, dir); !maps.Equal(before, after) {
				t.Errorf("没还原，文件不该动：\n之前 %q\n之后 %q", before, after)
			}
			if got := backupFiles(t, m); !slices.Equal(got, list) {
				t.Errorf("没还原，也不该先做一份「还原前自动」：%v → %v", list, got)
			}
		})
	}
}

// addZipSymlink 往 zip 文件末尾再加一项符号链接（重写整个包）。
func addZipSymlink(t *testing.T, zipPath, name string) {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	zr.Close()
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	h.SetMode(fs.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("../../outside"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 空文件夹（世界里的 datapacks/ 这类目录条目）照样换名、建出来。
func TestRestoreCreatesEmptyWorldFolders(t *testing.T) {
	m, dir := tempInstance(t)
	writeTree(t, dir, map[string]string{"server.properties": "level-name=survival\n", "survival/level.dat": "current"})
	file := putBackup(t, "20200101-000000.zip", map[string]string{"world/level.dat": "backup", "world/datapacks/": "", "world_nether/": ""})
	if err := m.RestoreBackup("s", file); err != nil {
		t.Fatal(err)
	}
	if got := readRel(t, dir, "survival/level.dat"); got != "backup" {
		t.Errorf("世界应还原成备份里的，实际 %q", got)
	}
	for _, d := range []string{"survival/datapacks", "survival_nether"} {
		if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(d))); err != nil || !st.IsDir() {
			t.Errorf("空文件夹 %s 应建出来：%v", d, err)
		}
	}
}

// 世界名用反斜杠写的（worlds\main）：Windows 上 LevelNameStrict 已经统一成斜杠，planWorldRestore 拿到的就是 worlds/main，
// 不用再转一遍。
func TestRestoreBackslashLevelNameOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只有 Windows 把反斜杠当路径分隔符")
	}
	m, dir := tempInstance(t)
	writeTree(t, dir, map[string]string{"server.properties": "level-name=worlds\\\\main\n", "worlds/main/level.dat": "current"})
	if level, err := LevelNameStrict(dir); err != nil || level != "worlds/main" {
		t.Fatalf("LevelNameStrict = %q %v，应为 worlds/main", level, err)
	}
	file := putBackup(t, "20200101-000000.zip", map[string]string{"world/level.dat": "backup"})
	if err := m.RestoreBackup("s", file); err != nil {
		t.Fatal(err)
	}
	if got := readRel(t, dir, "worlds/main/level.dat"); got != "backup" {
		t.Errorf("世界应还原进 worlds/main，实际 %q", got)
	}
}

// 包里 server.properties 的世界名只读不写；写了却不能用的当它没有，不当成 world。
func TestPropsLevelName(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"写了世界名", "level-name=lobby\n", "lobby"},
		{"带子目录", "level-name=worlds/main\n", "worlds/main"},
		{"没写这一项：服务端默认的 world", "motd=x\n", "world"},
		{"指到外面的不能用", "level-name=../evil\n", ""},
		{"控制目录不能用", "level-name=.minedeck/x\n", ""},
		{"大过 1 MB 的不读", "level-name=lobby\n#" + strings.Repeat("x", 1<<20) + "\n", ""},
		{"留空的：同 LevelNameStrict，按 world 算", "level-name=\n", "world"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			w, err := zw.Create("server.properties")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(tc.content))
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if got := propsLevelName(zr.File[0]); got != tc.want {
				t.Errorf("propsLevelName = %q，应为 %q", got, tc.want)
			}
		})
	}
}

// 「检查备份落点失败」写的是落到服务器文件夹里的哪一级（换过世界名以后的），再写备份里原来叫什么。
// 用 Windows 上不能当文件名的字符让 Lstat 报「不是不存在」的那种错。
func TestBackupTargetCheckErrorNamesTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("靠 Windows 上的非法文件名让 Lstat 出错")
	}
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, "survival"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("world/a<b/level.dat"); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	realDest, err := RealPathStrict(dest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = checkBackupTargetPath(filepath.Clean(dest), realDest, restoreEntry{f: zr.File[0], name: "survival/a<b/level.dat"})
	if err == nil || !strings.Contains(err.Error(), "survival/a<b") || !strings.Contains(err.Error(), "world/a<b/level.dat") {
		t.Fatalf("报错应写落点 survival/a<b 和备份里的原名：%v", err)
	}
}

// 认世界、换名字时的几处边界：前缀要到斜杠为止（world2 不是 world）；Paper 只有下界里有 level.dat 时，那还是 world；
// 世界（或者它的下界、末地）里面再套着的 level.dat 不算另一个世界；不分大小写；空文件夹也换名。
func TestPlanWorldRestoreBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		level   string
		entries []string
		from    string
		want    []string // 解出来落到哪
	}{
		{"前缀到斜杠为止", "world",
			[]string{"world/level.dat", "world2/level.dat", "world2/region/r.0.0.mca", "world_nether/DIM-1/r.mca", "worldX_nether/a"},
			"world", []string{"world/level.dat", "world_nether/DIM-1/r.mca"}},
		{"下界里面套着的 level.dat", "survival",
			[]string{"world/level.dat", "world_nether/level.dat", "world_nether/sub/level.dat"},
			"world", []string{"survival/level.dat", "survival_nether/level.dat", "survival_nether/sub/level.dat"}},
		{"下界里面套着的 level.dat、下界自己没有", "survival",
			[]string{"world/level.dat", "world_nether/sub/level.dat"},
			"world", []string{"survival/level.dat", "survival_nether/sub/level.dat"}},
		{"不分大小写：World 和 world 落进同一个文件夹", "world",
			[]string{"World/level.dat", "world/region/r.0.0.mca", "WORLD_NETHER/DIM-1/r.mca"},
			"world", []string{"world/level.dat", "world/region/r.0.0.mca", "world_nether/DIM-1/r.mca"}},
		{"不分大小写：换世界名", "survival",
			[]string{"World/level.dat", "world/region/r.0.0.mca"},
			"world", []string{"survival/level.dat", "survival/region/r.0.0.mca"}},
		{"空文件夹也换名", "survival",
			[]string{"world/level.dat", "world/datapacks/", "world_the_end/"},
			"world", []string{"survival/level.dat", "survival/datapacks", "survival_the_end"}},
		{"刚建还没存过盘、只有文件夹的世界", "world",
			[]string{"world/", "world/datapacks/"},
			"world", []string{"world", "world/datapacks"}},
		{"只有下界里有 level.dat", "survival",
			[]string{"world/region/r.0.0.mca", "world_nether/level.dat", "server.properties"},
			"world", []string{"survival/region/r.0.0.mca", "survival_nether/level.dat"}},
		{"世界里面套着的 level.dat", "survival",
			[]string{"world/level.dat", "world/backups/old/level.dat"},
			"world", []string{"survival/level.dat", "survival/backups/old/level.dat"}},
		{"没存过盘的世界：照现在的世界名", "world",
			[]string{"world/region/r.0.0.mca", "ops.json"},
			"world", []string{"world/region/r.0.0.mca"}},
		{"整个服务器文件夹连外面一层一起打的包", "world",
			[]string{"MyServer/server.properties", "MyServer/world/level.dat", "MyServer/world_nether/level.dat", "MyServer/mods/a.jar"},
			"MyServer/world", []string{"world/level.dat", "world_nether/level.dat"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, e := range tc.entries {
				if _, err := zw.Create(e); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := planWorldRestore(zr.File, tc.level)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range plan.entries {
				got = append(got, e.name)
			}
			slices.Sort(got)
			slices.Sort(tc.want)
			if !strings.EqualFold(plan.from, tc.from) || !slices.Equal(got, tc.want) {
				t.Errorf("认成 %q，解出 %q；应为 %q，%q（跳过的 %q）", plan.from, got, tc.from, tc.want, plan.skipped)
			}
		})
	}
}

// zipFiles 在内存里打一个包（entries 是「路径 → 内容」，按路径排序），返回里面的条目。
func zipFiles(t *testing.T, entries map[string]string) []*zip.File {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, k := range slices.Sorted(maps.Keys(entries)) {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(entries[k]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr.File
}

// 包里 server.properties 写的世界、现在的世界名，都只在认出来的世界里找（worldCandidates：下界末地并进主世界、
// 套在里面的不算）；认出来的名字用包里的写法。名字正好是一个单独的维度文件夹的，是原版世界起了这个名字，照用。
func TestPickBackupWorldFromCandidates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		level   string
		entries map[string]string
		from    string
	}{
		{"主世界只有 region、level.dat 只在下界里，包里还有别的世界", "world",
			map[string]string{"world/region/r.0.0.mca": "r", "world_nether/level.dat": "n", "lobby/level.dat": "l"}, "world"},
		{"包里 server.properties 写的是下界：不当主世界", "lobby",
			map[string]string{"server.properties": "level-name=world_nether\n", "world/level.dat": "w", "world_nether/level.dat": "n", "lobby/level.dat": "l"}, "lobby"},
		{"现在的世界名是包里的下界：不当主世界", "world_nether",
			map[string]string{"world/level.dat": "w", "world_nether/level.dat": "n"}, "world"},
		{"返回包里的写法", "world", map[string]string{"World/level.dat": "w"}, "World"},
		{"原版世界就叫 xxx_nether", "foo_nether", map[string]string{"foo_nether/level.dat": "f"}, "foo_nether"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planWorldRestore(zipFiles(t, tc.entries), tc.level)
			if err != nil {
				t.Fatal(err)
			}
			if plan.from != tc.from {
				t.Errorf("认成 %q，应为 %q", plan.from, tc.from)
			}
		})
	}
}

// linkDirForTest 建一个指向 target 的文件夹链接：Windows 上用目录联接（不用管理员权限），别处用符号链接。建不了就跳过。
func linkDirForTest(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		hideWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("建不了目录联接（%v）：%s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("建不了符号链接：%v", err)
	}
}

// 某个世界维度在包里只有空的目录条目，服务器文件夹里同名的那个又是指到外面的链接：删旧世界之前就停下（A12），
// 外面那份、现在的世界都不动，也不先做「还原前自动」。
func TestRestoreRefusesLinkedWorldFolder(t *testing.T) {
	m, dir := tempInstance(t)
	writeTree(t, dir, map[string]string{"server.properties": propsNow})
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"keep.dat": "外面的东西"})
	linkDirForTest(t, filepath.Join(dir, "world_the_end"), outside)
	file := putBackup(t, "20200101-000000.zip", map[string]string{"world/level.dat": "backup", "world_the_end/": ""})
	list := backupFiles(t, m)
	err := m.RestoreBackup("s", file)
	if err == nil || !strings.Contains(err.Error(), "「world_the_end」是指向别处的链接") {
		t.Fatalf("落点是指到外面的链接，应当不还原并说清是哪一个：%v", err)
	}
	if got := readLevel(t, dir); got != "current" {
		t.Errorf("没还原，世界不该动：%q", got)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 1 || readRel(t, outside, "keep.dat") != "外面的东西" {
		t.Errorf("外面的文件夹被动了：%v", ents)
	}
	if got := backupFiles(t, m); !slices.Equal(got, list) {
		t.Errorf("没还原，也不该先做一份「还原前自动」：%v → %v", list, got)
	}
}

// checkWorldFolders 用 Lstat 看世界文件夹本身：指到别处的、悬空的链接都拦下（用 Stat 的话，悬空的看着像「不存在」）。
func TestCheckWorldFoldersRefusesLinks(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		dest := t.TempDir()
		target := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		linkDirForTest(t, filepath.Join(dest, "world_the_end"), target)
		if dangling {
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
		}
		err := checkWorldFolders(dest, []string{"world", "world_the_end"})
		if err == nil || !strings.Contains(err.Error(), "「world_the_end」是指向别处的链接") {
			t.Errorf("悬空=%v：世界文件夹是链接，应当拦下：%v", dangling, err)
		}
	}
}

// 还原备份时的目录条目从落点本身查起：落点是链接就不还原，哪怕它指回服务器文件夹里面。
// 整包解压（checkBackupPaths）照旧只从上一级查，同 TestBackupRestoreDoesNotLinkCheckTargetItself。
func TestRestoreDirEntryChecksTargetItself(t *testing.T) {
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{"world/level.dat": "current", "config/a.toml": "a"})
	linkDirForTest(t, filepath.Join(dest, "world_the_end"), filepath.Join(dest, "config"))
	files := zipFiles(t, map[string]string{"world/level.dat": "backup", "world_the_end/": ""})
	plan, err := planWorldRestore(files, "world")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkEntryPaths(plan.entries, dest); err == nil || !strings.Contains(err.Error(), "「world_the_end」是指向别处的链接") {
		t.Errorf("还原时目录条目的落点本身是链接，应当拦下：%v", err)
	}
	if err := checkBackupPaths(files, dest); err != nil {
		t.Errorf("整包解压照旧不查落点本身：%v", err)
	}
}
