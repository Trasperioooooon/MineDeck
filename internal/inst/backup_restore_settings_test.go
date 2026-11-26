package inst

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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

// 认不出该还原哪个世界（包里没有世界、有好几个又定不下来）：不还原，世界、设置、备份列表都不动。
func TestRestoreRefusesWhenWorldUnclear(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
		msg     string
	}{
		{"包里没有世界", map[string]string{"server.properties": "online-mode=true\n", "ops.json": "[]"}, "没找到世界"},
		{"好几个世界定不下来", map[string]string{"a/level.dat": "a", "b/level.dat": "b"}, "好几个世界"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			writeTree(t, dir, map[string]string{"server.properties": propsNow})
			file := putBackup(t, "20200101-000000.zip", tc.entries)
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

// 认世界、换名字时的几处边界：前缀要到斜杠为止（world2 不是 world）；绕回上一级的条目
// （world/../server.properties）就是根上的文件，不算世界；Paper 只有下界里有 level.dat 时，那还是 world；
// 世界里面再套着的 level.dat 不算另一个世界。
func TestPlanWorldRestoreBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		level   string
		entries []string
		from    string
		want    []string // 解出来落到哪
	}{
		{"前缀到斜杠为止、绕回上一级的不算", "world",
			[]string{"world/level.dat", "world2/level.dat", "world2/region/r.0.0.mca", "world/../server.properties", "world_nether/DIM-1/r.mca", "worldX_nether/a"},
			"world", []string{"world/level.dat", "world_nether/DIM-1/r.mca"}},
		{"只有下界里有 level.dat", "survival",
			[]string{"world/region/r.0.0.mca", "world_nether/level.dat", "server.properties"},
			"world", []string{"survival/region/r.0.0.mca", "survival_nether/level.dat"}},
		{"世界里面套着的 level.dat", "survival",
			[]string{"world/level.dat", "world/backups/old/level.dat"},
			"world", []string{"survival/level.dat", "survival/backups/old/level.dat"}},
		{"没存过盘的世界：照现在的世界名", "world",
			[]string{"world/region/r.0.0.mca", "ops.json"},
			"world", []string{"world/region/r.0.0.mca"}},
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
			if plan.from != tc.from || !slices.Equal(got, tc.want) {
				t.Errorf("认成 %q，解出 %q；应为 %q，%q（跳过的 %q）", plan.from, got, tc.from, tc.want, plan.skipped)
			}
		})
	}
}
