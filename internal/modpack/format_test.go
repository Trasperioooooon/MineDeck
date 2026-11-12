package modpack

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
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
	n, err := ExtractServer(&Pack{Format: FormatServer, ZipPath: z, Root: "Srv"}, func(rel string, r io.Reader) error {
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
	if _, err := ExtractServer(p, func(rel string, r io.Reader) error {
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
		"两处清单":  {"A/modrinth.index.json": "{}", "B/manifest.json": "{}"},
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
