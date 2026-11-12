package inst

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"minedeck/internal/modpack"
)

// 整合包的覆盖目录（v3.1.0 正式版前检查 C-2e）：名字在 Windows 上写不出原样的不采用（带扩展名的设备名、结尾是点或空格的）；
// 同一层里只差大小写的两个文件留前一个、后一个不写，都记下来给日志。写法一模一样的照旧以后一个为准：
// 那是后一层（server-overrides）有意盖掉前一层（overrides）。跨层只差大小写的也按重名算，留前一个。
func TestOverrideSinkNamesAndCaseDuplicates(t *testing.T) {
	z := zipOrdered(t, [][2]string{
		{"overrides/config/A.toml", "a1"},
		{"overrides/config/a.toml", "a2"},
		{"overrides/config/b.toml", "b1"},
		{"overrides/instance.json", "{}"},
		{"overrides/config/CON.txt", "x"},
		{"overrides/config/trail.toml.", "x"},
		{"server-overrides/config/b.toml", "b2"},
		{"server-overrides/config/A.TOML", "a3"},
	})
	dir := t.TempDir()
	sink := newOverrideSink(dir)
	if err := modpack.ExtractOverrides(&modpack.Pack{ZipPath: z, OverrideDirs: []string{"overrides", "server-overrides"}}, dir, sink.write); err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, filepath.Join(dir, "config")); !slices.Equal(got, []string{"A.toml", "b.toml"}) {
		t.Errorf("config 里应是 A.toml 和 b.toml：%q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "A.toml")); string(b) != "a1" {
		t.Errorf("只差大小写的应留前一个：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "b.toml")); string(b) != "b2" {
		t.Errorf("后一层覆盖目录里写法一样的应盖掉前一层：%q", b)
	}
	if want := []string{"instance.json", "config/CON.txt", "config/trail.toml."}; !slices.Equal(sink.notTaken, want) {
		t.Errorf("没采用的：%q，应为 %q", sink.notTaken, want)
	}
	if want := []string{"config/a.toml", "config/A.TOML"}; !slices.Equal(sink.dups, want) {
		t.Errorf("只差大小写的：%q，应为 %q", sink.dups, want)
	}
}

// 整合包清单里落点只差大小写的（一模一样的也算）只下载第一条：Windows 上是同一个文件，并发下载会同时写它。
func TestDedupeDownloads(t *testing.T) {
	kept, dups := dedupeDownloads([]modpack.File{
		{Path: "mods/A.jar"}, {Path: "mods/b.jar"}, {Path: "mods/a.jar"}, {Path: "MODS/B.JAR"}, {Path: "mods/b.jar"},
		{Path: "mods/sub/../c.jar"}, {Path: "mods/c.jar"},
	})
	var paths []string
	for _, f := range kept {
		paths = append(paths, f.Path)
	}
	if want := []string{"mods/A.jar", "mods/b.jar", "mods/sub/../c.jar"}; !slices.Equal(paths, want) {
		t.Errorf("留下的：%q，应为 %q", paths, want)
	}
	if want := []string{"mods/a.jar", "MODS/B.JAR", "mods/b.jar", "mods/c.jar"}; !slices.Equal(dups, want) {
		t.Errorf("没下载的：%q，应为 %q", dups, want)
	}
}
