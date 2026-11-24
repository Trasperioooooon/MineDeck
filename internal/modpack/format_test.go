package modpack

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"minedeck/internal/mcsrc"
)

func jarOf(t *testing.T, files map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "jar-*.jar")
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func cfManifest(ver, loader string) string {
	return `{"name":"CF包","overrides":"overrides","minecraft":{"version":"` + ver +
		`","modLoaders":[{"id":"` + loader + `","primary":true}]},"files":[{"projectID":1,"fileID":11,"required":true}]}`
}

func paperJar(t *testing.T) string {
	t.Helper()
	return jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF":   "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n",
		"META-INF/versions.list": "abc\t1.20.1\t1.20.1/paper-1.20.1.jar\n",
		"version.json":           `{"id":"1.20.1","java_version":17}`,
	})
}

func overridesOf(t *testing.T, p *Pack) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := ExtractOverrides(p, t.TempDir(), func(rel string, r io.Reader) error {
		b, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		got[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseNestedMrpack(t *testing.T) {
	idx := mrpackIndex(`{"path":"mods/ok.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":1}`)
	p, err := Parse(mkZip(t, map[string]string{
		"机械动力/modrinth.index.json":       idx,
		"机械动力/overrides/config/a.txt":    "hi",
		"机械动力/server-overrides/ops.json": `["Steve"]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatMrpack || p.Root != "机械动力" || p.MC != "1.20.1" || p.Core != mcsrc.CoreFabric || p.Loader != "0.16.9" {
		t.Fatalf("套了一层的 mrpack 没认对: %+v", p)
	}
	if len(p.OverrideDirs) != 2 || p.OverrideDirs[0] != "机械动力/overrides" || p.OverrideDirs[1] != "机械动力/server-overrides" {
		t.Fatalf("覆盖目录应带上外面那一层: %v", p.OverrideDirs)
	}
	if len(p.Files) != 1 || p.Files[0].Path != "mods/ok.jar" {
		t.Fatalf("清单里的文件路径不应带上包装目录: %+v", p.Files)
	}
	got := overridesOf(t, p)
	if got["config/a.txt"] != "hi" || got["ops.json"] != `["Steve"]` {
		t.Fatalf("覆盖文件落点不对: %v", got)
	}
}

func TestParseNestedCurseForge(t *testing.T) {
	p, err := Parse(mkZip(t, map[string]string{
		"整合包/manifest.json":          cfManifest("1.20.1", "forge-47.3.0"),
		"整合包/overrides/config/a.txt": "hi",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatCurseForge || p.Root != "整合包" || p.MC != "1.20.1" || p.Core != mcsrc.CoreForge || p.Loader != "47.3.0" || len(p.CFRefs) != 1 {
		t.Fatalf("套了一层的 CurseForge 没认对: %+v", p)
	}
	if len(p.OverrideDirs) != 1 || p.OverrideDirs[0] != "整合包/overrides" {
		t.Fatalf("覆盖目录应带上外面那一层: %v", p.OverrideDirs)
	}
	if got := overridesOf(t, p); got["config/a.txt"] != "hi" {
		t.Fatalf("覆盖文件落点不对: %v", got)
	}
}

func TestParseRootManifestWins(t *testing.T) {
	p, err := Parse(mkZip(t, map[string]string{
		"manifest.json":     cfManifest("1.20.1", "forge-47.3.0"),
		"另一份/manifest.json": cfManifest("9.9.9", "forge-1.0"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != "" || p.MC != "1.20.1" {
		t.Fatalf("最外层有清单时不应去猜里面那份: %+v", p)
	}
}

func TestParseAmbiguousIndexes(t *testing.T) {
	_, err := Parse(mkZip(t, map[string]string{
		"甲/manifest.json": cfManifest("1.20.1", "forge-47.3.0"),
		"乙/manifest.json": cfManifest("1.21.1", "neoforge-21.1.1"),
	}))
	if err == nil || !strings.Contains(err.Error(), "不止一处") {
		t.Fatalf("两处清单应拒绝而不是猜: %v", err)
	}
	_, err = Parse(mkZip(t, map[string]string{
		"包/modrinth.index.json": mrpackIndex(""),
		"包/manifest.json":       cfManifest("1.20.1", "forge-47.3.0"),
	}))
	if err == nil || !strings.Contains(err.Error(), "同时有") {
		t.Fatalf("同一层两种清单应拒绝: %v", err)
	}
}

func TestParseBackslashNestedManifest(t *testing.T) {
	// 有的压缩工具用反斜杠。认的时候要当成同一层目录。
	p, err := Parse(mkZip(t, map[string]string{
		`包\manifest.json`: cfManifest("1.20.1", "forge-47.3.0"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatCurseForge || p.Root != "包" || p.MC != "1.20.1" {
		t.Fatalf("反斜杠套层没认出来: %+v", p)
	}
}

func TestDeepManifestIsNotCurseForge(t *testing.T) {
	man := cfManifest("9.9.9", "forge-1.0")
	// mods 里面的 manifest.json，哪怕长得像 CurseForge 清单，也不能当成整合包。
	_, err := Parse(mkZip(t, map[string]string{
		"mods/jei/manifest.json": man,
		"mods/jei.jar":           "not-a-server",
	}))
	if err == nil || strings.Contains(err.Error(), "9.9.9") || strings.Contains(err.Error(), "无法识别整合包加载器") {
		t.Fatalf("mods 深处的 manifest.json 被当成了整合包: %v", err)
	}
	if !strings.Contains(err.Error(), "接管已有服务器") {
		t.Fatalf("认不出核心时应告诉用户去接管: %v", err)
	}

	// 旁边有真正的服务端时，用服务端，不用 mods 里那份假清单。
	// 放在 mods/manifest.json（刚好一层、目录名又是 mods）同样不能当成包装目录。
	p, err := Parse(mkZip(t, map[string]string{
		"mods/manifest.json":     man,
		"mods/jei/manifest.json": man,
		"server.properties":      "motd=hello\n",
		"paper-1.20.1-100.jar":   paperJar(t),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatServer || p.Core != mcsrc.CorePaper || p.MC != "1.20.1" || p.MC == "9.9.9" {
		t.Fatalf("应认成 Paper 服务端，而不是 mods 里的清单: %+v", p)
	}
}

func TestParseServerArchives(t *testing.T) {
	paper := paperJar(t)
	forgeArgs := "--launchTarget forgeserver --fml.forgeVersion 47.3.0 --fml.mcVersion 1.20.1\n"
	neoArgs := "--launchTarget forgeserver --fml.neoForgeVersion 21.1.265 --fml.mcVersion 1.21.1\n"
	fabric := jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.fabricmc.loader.impl.launch.server.FabricServerLauncher\r\n\r\n",
		"install.properties":   "game-version=1.20.1\nfabric-loader-version=0.16.9\n",
	})
	vanilla := jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF":   "Manifest-Version: 1.0\r\nMain-Class: net.minecraft.bundler.Main\r\n\r\n",
		"version.json":           `{"id":"1.21.1","java_version":21}`,
		"META-INF/versions.list": "abc\t1.21.1\t1.21.1/server-1.21.1.jar\n",
	})

	cases := []struct {
		name   string
		files  map[string]string
		root   string
		core   mcsrc.Core
		mc     string
		loader string
		launch string
	}{
		{
			name: "套一层的 Paper",
			files: map[string]string{
				"MyServer/server.properties":    "server-port=25565\n",
				"MyServer/mods/jei.jar":         "mod",
				"MyServer/paper-1.20.1-100.jar": paper,
			},
			root: "MyServer", core: mcsrc.CorePaper, mc: "1.20.1", launch: "jar:paper-1.20.1-100.jar",
		},
		{
			name: "最外层就是服务端",
			files: map[string]string{
				"server.properties":    "server-port=25565\n",
				"plugins/x.jar":        "plugin",
				"paper-1.20.1-100.jar": paper,
			},
			core: mcsrc.CorePaper, mc: "1.20.1", launch: "jar:paper-1.20.1-100.jar",
		},
		{
			name: "套一层的 Forge",
			files: map[string]string{
				"Srv/mods/a.jar": "mod",
				"Srv/libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt": forgeArgs,
			},
			root: "Srv", core: mcsrc.CoreForge, mc: "1.20.1", loader: "47.3.0",
			launch: "args:libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt",
		},
		{
			name: "NeoForge",
			files: map[string]string{
				"libraries/net/neoforged/neoforge/21.1.265/win_args.txt": neoArgs,
				"server.properties": "x",
			},
			core: mcsrc.CoreNeoForge, mc: "1.21.1", loader: "21.1.265",
			launch: "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt",
		},
		{
			name: "Fabric",
			files: map[string]string{
				"fabric-server-launch.jar": fabric,
				"mods/a.jar":               "mod",
			},
			core: mcsrc.CoreFabric, mc: "1.20.1", loader: "0.16.9", launch: "jar:fabric-server-launch.jar",
		},
		{
			name: "原版",
			files: map[string]string{
				"server.jar":        vanilla,
				"server.properties": "x",
			},
			core: mcsrc.CoreVanilla, mc: "1.21.1", launch: "jar:server.jar",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Parse(mkZip(t, c.files))
			if err != nil {
				t.Fatal(err)
			}
			if p.Format != FormatServer || p.Root != c.root || p.Core != c.core || p.MC != c.mc || p.Loader != c.loader || p.Launch != c.launch {
				t.Fatalf("认成了 format=%s root=%q core=%s mc=%s loader=%s launch=%s", p.Format, p.Root, p.Core, p.MC, p.Loader, p.Launch)
			}
		})
	}
}

func TestParseQuiltServerRejected(t *testing.T) {
	q := jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: org.quiltmc.loader.impl.launch.server.QuiltServerLauncher\r\n\r\n",
	})
	_, err := Parse(mkZip(t, map[string]string{
		"quilt-server-launch.jar": q,
		"mods/a.jar":              "mod",
	}))
	if err == nil || !strings.Contains(err.Error(), "Quilt") {
		t.Fatalf("Quilt 服务端应拒绝: %v", err)
	}
}

func TestParseAmbiguousServerDirs(t *testing.T) {
	_, err := Parse(mkZip(t, map[string]string{
		"A/server.properties": "x",
		"A/mods/a.jar":        "mod",
		"B/server.properties": "x",
		"B/plugins/b.jar":     "plugin",
	}))
	if err == nil || !strings.Contains(err.Error(), "好几个") {
		t.Fatalf("两个服务端文件夹应拒绝: %v", err)
	}
}

func TestParseNestedMrpackRejectsBadPaths(t *testing.T) {
	trav := mrpackIndex(`{"path":"../../../evil.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":1}`)
	if _, err := Parse(mkZip(t, map[string]string{"包/modrinth.index.json": trav})); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("套层之后的 ../ 仍应拒绝: %v", err)
	}
	abs := mrpackIndex(`{"path":"/etc/passwd","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":1}`)
	if _, err := Parse(mkZip(t, map[string]string{"包/modrinth.index.json": abs})); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("绝对路径应拒绝: %v", err)
	}
	win := mrpackIndex(`{"path":"C:/Windows/evil.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":1}`)
	if _, err := Parse(mkZip(t, map[string]string{"包/modrinth.index.json": win})); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("盘符路径应拒绝: %v", err)
	}
}

func TestMaliciousZipPaths(t *testing.T) {
	// 清单不能靠 ../、绝对路径、盘符伪装到最外层。
	for _, name := range []string{"../manifest.json", "/manifest.json", "C:/manifest.json"} {
		_, err := Parse(mkZip(t, map[string]string{name: cfManifest("1.20.1", "forge-47.3.0"), "readme.txt": "x"}))
		if err == nil {
			t.Fatalf("%s 不应被当成清单", name)
		}
	}

	// 已经像服务端了，里面再夹一条能穿越出去的路径：整包拒绝，一个文件都不写。
	z := mkZip(t, map[string]string{
		"Srv/server.properties": "x",
		"Srv/mods/a.jar":        "mod",
		"Srv/../../evil.txt":    "pwn",
		"Srv/C:/Windows/x.txt":  "pwn",
	})
	if _, err := Parse(z); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("服务端压缩包里的非法路径应拒绝: %v", err)
	}
	n, err := ExtractServer(&Pack{Format: FormatServer, ZipPath: z, Root: "Srv"}, nil, func(rel string, r io.Reader) error {
		t.Fatalf("不应写出 %s", rel)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "非法路径") || n != 0 {
		t.Fatalf("解压也应拦住非法路径: n=%d err=%v", n, err)
	}
}

func TestExtractServerStripsWrapper(t *testing.T) {
	p, err := Parse(mkZip(t, map[string]string{
		"MyServer/server.properties":    "motd=hello\n",
		"MyServer/mods/jei.jar":         "mod",
		"MyServer/paper-1.20.1-100.jar": paperJar(t),
		"MyServer/readme.txt":           "hi",
		"别的说明.txt":                      "skip",
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	if _, err := ExtractServer(p, nil, func(rel string, r io.Reader) error {
		b, err := io.ReadAll(r)
		got[rel] = string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got["server.properties"] != "motd=hello\n" || got["mods/jei.jar"] != "mod" || got["readme.txt"] != "hi" {
		t.Fatalf("剥掉包装目录之后的路径不对: %v", got)
	}
	if _, ok := got["别的说明.txt"]; ok {
		t.Fatal("包装目录外面的文件不应解进来")
	}
	for rel := range got {
		if strings.HasPrefix(rel, "MyServer/") {
			t.Fatalf("还带着包装目录: %s", rel)
		}
	}
}

// 解析阶段的长说明另给一句 60 字以内的人话（Hint）：界面的提示正文接这一句，长说明进「详情」。
func TestParseErrorsHaveHint(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"认不出":   {"readme.txt": "hi"},
		"两台服务端": {"A/server.properties": "x", "A/mods/a.jar": "mod", "B/server.properties": "x", "B/plugins/b.jar": "plugin"},
		"两处清单":  {"A/modrinth.index.json": "{}", "B/manifest.json": cfManifest("1.20.1", "forge-47.3.0")}, // 空的 {} 不算 CurseForge 清单，见 isIndexFile
		"认不出核心": {"server.properties": "x", "plugins/a.jar": "plugin"},
	} {
		_, err := Parse(mkZip(t, files))
		var h interface{ Hint() string }
		if err == nil || !errors.As(err, &h) {
			t.Fatalf("%s：应带一句人话：%v", name, err)
		}
		if hint := h.Hint(); hint == "" || utf8.RuneCountInString(hint) > 60 || len(err.Error()) <= len(hint) {
			t.Fatalf("%s：人话 %q 应在 60 字以内、比原文短：%q", name, hint, err.Error())
		}
	}
}

// 打得开、Main-Class 认不出的 jar 不按文件名猜核心：PaperLib-1.20.1.jar 是库，不是 Paper。
func TestLibraryJarNotGuessedByName(t *testing.T) {
	lib := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\n\r\n"})
	pack, err := Parse(mkZip(t, map[string]string{
		"server.properties":   "x",
		"plugins/a.jar":       "plugin",
		"PaperLib-1.20.1.jar": lib,
	}))
	if err == nil {
		t.Fatalf("库 jar 不该被当成 Paper 服务端：%+v", pack)
	}
}

// 八千个顶层文件夹的压缩包：找服务端在哪一层只扫一遍条目建好各层的样子（zipLayout），serverRoot 只查它、碰不到条目本身。
// 以前对每个顶层文件夹都把全部条目扫一遍，这样的包要一秒多（A40）；上限放得很宽，只挡回到那种做法。
func TestServerRootManyTopLevelDirs(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8000; i++ {
		add(fmt.Sprintf("d%04d/readme.txt", i), "x")
		add(fmt.Sprintf("d%04d/sub/a.txt", i), "x")
	}
	add("Srv/server.properties", "server-port=25565\n")
	add("Srv/paper-1.20.1-100.jar", paperJar(t))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	p, err := parseFiles("内存里的.zip", zr.File)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("认这个包用了 %v", took)
	}
	if err != nil || p.Format != FormatServer || p.Root != "Srv" || p.Core != mcsrc.CorePaper {
		t.Fatalf("应认出 Srv 里那台 Paper：%+v %v", p, err)
	}
}

// 「像服务端」要看到服务端的特征，名字认不出的 jar 单独一个不算（A40）：套了一层的服务端旁边多放一个这样的 jar，
// 以前最外层就算像服务端，里面那台认不出来了。
func TestUnknownJarAloneIsNotServer(t *testing.T) {
	tool := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: com.example.Tool\r\n\r\n"})
	p, err := Parse(mkZip(t, map[string]string{
		"备份工具.jar":                      tool,
		"MyServer/server.properties":    "server-port=25565\n",
		"MyServer/paper-1.20.1-100.jar": paperJar(t),
	}))
	if err != nil || p.Root != "MyServer" || p.Core != mcsrc.CorePaper || p.MC != "1.20.1" {
		t.Fatalf("旁边多一个认不出的 jar，里面那台照样该认出来：%+v %v", p, err)
	}
	var h interface{ Hint() string }
	if _, err := Parse(mkZip(t, map[string]string{"备份工具.jar": tool})); !errors.As(err, &h) || !strings.HasPrefix(h.Hint(), "没认出这个压缩包") {
		t.Fatalf("只有一个认不出的 jar，不该算服务端：%v", err)
	}
	// 最外层有 server.properties 或名字认得出的服务端 jar：它就是服务端本身，按最外层认，认不出核心也不往文件夹里找
	for _, trait := range []string{"server.properties", "server.jar", "paper.jar", "spigot-1.20.1.jar"} {
		_, err := Parse(mkZip(t, map[string]string{
			trait:                           "x",
			"MyServer/server.properties":    "server-port=25565\n",
			"MyServer/paper-1.20.1-100.jar": paperJar(t),
		}))
		if !errors.As(err, &h) || h.Hint() != "看着像现成服务端，但没认出核心和版本" {
			t.Fatalf("最外层有 %s 时应按最外层认（认不出核心）：%v", trait, err)
		}
	}
	// 最外层只多一个 eula.txt、mods/、plugins/、libraries/ 这类旁证：最外层认不出核心时再看里面一层，认出里面那台
	for _, trait := range []string{"eula.txt", "mods/a.jar", "plugins/a.jar", "libraries/x/y.jar"} {
		p, err := Parse(mkZip(t, map[string]string{
			trait:                           "x",
			"MyServer/server.properties":    "server-port=25565\n",
			"MyServer/paper-1.20.1-100.jar": paperJar(t),
		}))
		if err != nil || p.Root != "MyServer" || p.Core != mcsrc.CorePaper {
			t.Fatalf("最外层只多一个 %s 时应认出里面那台：%+v %v", trait, p, err)
		}
	}
	// 里面也认不出的，照旧说最外层认不出核心
	if _, err := Parse(mkZip(t, map[string]string{"eula.txt": "x", "Other/readme.txt": "x"})); !errors.As(err, &h) || h.Hint() != "看着像现成服务端，但没认出核心和版本" {
		t.Fatalf("里面也认不出时应照旧报最外层认不出核心：%v", err)
	}
}

func TestKnownServerJar(t *testing.T) {
	for name, want := range map[string]bool{
		"server.jar": true, "minecraft_server.1.12.2.jar": true, "paper-1.20.1-100.jar": true, "purpur.jar": true,
		"fabric-server-launch.jar": true, "forge-1.12.2-14.23.5.2860.jar": true, "spigot-1.20.1.jar": true,
		"craftbukkit.jar": true, "BungeeCord.jar": true, "velocity-3.3.0.jar": true, "quilt-server-launch.jar": true,
		"Mohist-1.20.1.jar": true, "taiyitist-1.21.1.jar": true,
		// 名字像 Paper 的库：算特征，认核心时打开 jar 看主类才知道不是（见 TestLibraryJarNotGuessedByName）
		"PaperLib-1.20.1.jar":                true,
		"forge-1.20.1-47.2.20-installer.jar": false, "BuildTools.jar": false, "备份工具.jar": false,
		"server.properties": false, "serverstarter-2.4.0.jar": false,
	} {
		if got := knownServerJar(name); got != want {
			t.Errorf("knownServerJar(%q) = %v，应为 %v", name, got, want)
		}
	}
}

// 文件夹里的 manifest.json 要真像 CurseForge 清单才算（A40）：最外层已经像服务端时，里面的一概不算；
// 不然也要读得出来、minecraft 那一段写着版本。最外层的那份照旧：读不出来也当成清单，把原因报出来。
func TestNestedManifestMustLookLikeCurseForge(t *testing.T) {
	paper := paperJar(t)
	for name, manifest := range map[string]string{
		"网页的清单":   `{"name":"控制面板","icons":[{"src":"a.png"}],"start_url":"/"}`,
		"不是 JSON": "<html>",
		"像 CF 的":  cfManifest("9.9.9", "forge-1.0"),
	} {
		t.Run("最外层是服务端时，"+name, func(t *testing.T) {
			p, err := Parse(mkZip(t, map[string]string{
				"server.properties":      "server-port=25565\n",
				"paper-1.20.1-100.jar":   paper,
				"plugins/web/index.html": "<html>",
				"web/manifest.json":      manifest,
			}))
			if err != nil || p.Format != FormatServer || p.Root != "" || p.MC != "1.20.1" {
				t.Fatalf("应按最外层的服务端认：%+v %v", p, err)
			}
		})
	}
	for name, manifest := range map[string]string{
		"网页的清单":                       `{"name":"控制面板","icons":[{"src":"a.png"}]}`,
		"不是 JSON":                     "<html>",
		"没写 manifestType、minecraft 空": `{"minecraft":{}}`,
	} {
		t.Run("套一层时，"+name, func(t *testing.T) {
			p, err := Parse(mkZip(t, map[string]string{
				"包/manifest.json":        manifest,
				"包/server.properties":    "server-port=25565\n",
				"包/paper-1.20.1-100.jar": paper,
			}))
			if err != nil || p.Format != FormatServer || p.Root != "包" {
				t.Fatalf("不像 CurseForge 清单，应按里面的服务端认：%+v %v", p, err)
			}
		})
	}
	if p, err := Parse(mkZip(t, map[string]string{"包/manifest.json": cfManifest("1.20.1", "forge-47.3.0")})); err != nil || p.Format != FormatCurseForge {
		t.Fatalf("套一层的 CurseForge 清单照旧认：%+v %v", p, err)
	}
	// 明写 manifestType: minecraftModpack 的就是 CurseForge 清单，版本空着也算，由解析报出具体缺什么，不说笼统的「没认出」
	emptyMC := `{"manifestType":"minecraftModpack","minecraft":{"version":"","modLoaders":[]},"files":[]}`
	if _, err := Parse(mkZip(t, map[string]string{"包/manifest.json": emptyMC, "包/overrides/a.txt": "x"})); err == nil || !strings.Contains(err.Error(), "无法识别整合包加载器") {
		t.Fatalf("明写了 manifestType 的应按清单解析、报具体原因：%v", err)
	}
}

// 包装文件夹最多剥三层，每一层都只有一个文件夹、没有别的文件时才往里剥（A41）。四层的照旧认不出。
func TestWrapperFoldersUpToThree(t *testing.T) {
	server := func(prefix string) map[string]string {
		return map[string]string{
			prefix + "server.properties":    "server-port=25565\n",
			prefix + "mods/jei.jar":         "mod",
			prefix + "paper-1.20.1-100.jar": paperJar(t),
		}
	}
	for _, c := range []struct{ prefix, root, name string }{
		{"外/里/", "外/里", "里"},
		{"外/中/里/", "外/中/里", "里"},
	} {
		p, err := Parse(mkZip(t, server(c.prefix)))
		if err != nil || p.Format != FormatServer || p.Root != c.root || p.Name != c.name || p.Core != mcsrc.CorePaper {
			t.Fatalf("%s：应认成套着的服务端：%+v %v", c.prefix, p, err)
		}
		got := map[string]string{}
		if _, err := ExtractServer(p, nil, func(rel string, r io.Reader) error {
			b, err := io.ReadAll(r)
			got[rel] = string(b)
			return err
		}); err != nil || got["mods/jei.jar"] != "mod" || got["server.properties"] == "" {
			t.Fatalf("%s：解出来应剥掉外面几层：%v %v", c.prefix, got, err)
		}
	}
	unrecognized := func(what string, files map[string]string) {
		t.Helper()
		var h interface{ Hint() string }
		if _, err := Parse(mkZip(t, files)); !errors.As(err, &h) || h.Hint() != "没认出这个压缩包，既没有整合包清单，也看不出是服务端" {
			t.Fatalf("%s：应照旧认不出：%v", what, err)
		}
	}
	unrecognized("套四层", server("一/二/三/四/"))
	withNote := server("外/里/")
	withNote["外/说明.txt"] = "这一层还有别的文件"
	unrecognized("中间一层有别的文件", withNote)
	withSibling := server("外/里/")
	withSibling["外/另一个/a.txt"] = "这一层有两个文件夹"
	unrecognized("中间一层有两个文件夹", withSibling)

	cf := cfManifest("1.20.1", "forge-47.3.0")
	if p, err := Parse(mkZip(t, map[string]string{"外/中/里/manifest.json": cf, "外/中/里/overrides/config/a.txt": "hi"})); err != nil ||
		p.Format != FormatCurseForge || p.Root != "外/中/里" || overridesOf(t, p)["config/a.txt"] != "hi" {
		t.Fatalf("套三层的 CurseForge 整合包应认出来、覆盖目录带上外面几层：%+v %v", p, err)
	}
	if p, err := Parse(mkZip(t, map[string]string{"外/里/modrinth.index.json": mrpackIndex("")})); err != nil || p.Format != FormatMrpack || p.Root != "外/里" {
		t.Fatalf("套两层的 Modrinth 整合包应认出来：%+v %v", p, err)
	}
	unrecognized("清单套四层", map[string]string{"一/二/三/四/manifest.json": cf})
	unrecognized("剥的路上有内容目录", map[string]string{"外/config/manifest.json": cf}) // config 这种不算包装文件夹
}

// 服务端 jar 里没写版本时，最后看压缩包里的 logs/latest.log（A44）；jar 里写了的以 jar 为准，代理端不看。
func TestServerVersionFromLog(t *testing.T) {
	bare := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n"})
	logLine := "[12:00:00] [Server thread/INFO]: Starting minecraft server version 1.20.1\n"
	p, err := Parse(mkZip(t, map[string]string{"Srv/paper.jar": bare, "Srv/eula.txt": "eula=true\n", "Srv/logs/latest.log": logLine}))
	if err != nil || p.Core != mcsrc.CorePaper || p.MC != "1.20.1" || !p.MCFromLog {
		t.Fatalf("jar 里没写版本，应从日志里认出 1.20.1：%+v %v", p, err)
	}
	for name, files := range map[string]map[string]string{
		"没有日志":      {"paper.jar": bare, "eula.txt": "eula=true\n"},
		"日志里没有那一行":  {"paper.jar": bare, "eula.txt": "eula=true\n", "logs/latest.log": "[12:00:00] Loading libraries\n"},
		"版本号不像样":    {"paper.jar": bare, "eula.txt": "eula=true\n", "logs/latest.log": "Starting minecraft server version <script>\n"},
		"日志在别的文件夹里": {"paper.jar": bare, "eula.txt": "eula=true\n", "backup/logs/latest.log": logLine},
	} {
		if p, err := Parse(mkZip(t, files)); err != nil || p.MC != "" || p.MCFromLog {
			t.Errorf("%s：不该认出版本：%+v %v", name, p, err)
		}
	}
	if p, err := Parse(mkZip(t, map[string]string{"paper-1.20.1-100.jar": paperJar(t), "logs/latest.log": "Starting minecraft server version 1.19.4\n"})); err != nil ||
		p.MC != "1.20.1" || p.MCFromLog {
		t.Fatalf("jar 里写了版本的以 jar 为准：%+v %v", p, err)
	}
	// 代理端不跑 Minecraft 本体，日志里就算有这一行也不是它的版本
	velocity := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: com.velocitypowered.proxy.Velocity\r\n\r\n"})
	if p, err := Parse(mkZip(t, map[string]string{"velocity.jar": velocity, "logs/latest.log": logLine})); err != nil ||
		p.Core != mcsrc.CoreVelocity || p.MC != "" || p.MCFromLog {
		t.Fatalf("代理端不从日志认版本：%+v %v", p, err)
	}
	// 只看日志的开头 512 KB：这一行在开服的头几秒就写了，再往后的不看
	late := strings.Repeat("[12:00:00] [Server thread/INFO]: 填充\n", (512<<10)/40+100) + logLine
	if p, err := Parse(mkZip(t, map[string]string{"paper.jar": bare, "eula.txt": "eula=true\n", "logs/latest.log": late})); err != nil || p.MC != "" || p.MCFromLog {
		t.Fatalf("512 KB 以后才出现的那一行不认：%+v %v", p, err)
	}
	if p, err := Parse(mkZip(t, map[string]string{"paper.jar": bare, "eula.txt": "eula=true\n", "logs/latest.log": logLine + late})); err != nil || p.MC != "1.20.1" {
		t.Fatalf("开头就有的照常认，后面再长也不碍事：%+v %v", p, err)
	}
}
