package modpack

import (
	"errors"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
)

// packShape 一种压缩包的样子和应认成什么。hint 不为空时应认不出，报这一句（或者错误原文里有它）。
type packShape struct {
	name   string
	files  map[string]string
	format string
	root   string
	core   mcsrc.Core
	mc     string
	hint   string
}

// 真实包常见的样子各认成什么（A40、A41）。规矩改过几回，这张表钉住：以前认得出的现在照样认得出；
// 故意改了的只有两处（A40）——最外层旁边多一个工具 jar、里面套着服务端的，认里面那台（以前认不出）；
// 网页文件夹里的 manifest.json 不算清单。只有核心加启动脚本、名字不在名单里的 jar 按内容认；Mac 打的 zip 夹带的东西不算数。
// 最外层像服务端却认不出核心时往里看一层：清单照收；服务端只在最外层的旁证只有 eula.txt、libraries/ 时收。
func TestPackShapes(t *testing.T) {
	paper := paperJar(t)
	vanilla := jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.minecraft.bundler.Main\r\n\r\n",
		"version.json":         `{"id":"1.20.1","java_version":17}`,
	})
	oldVanilla := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.minecraft.server.MinecraftServer\r\n\r\n"})
	fabric := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.fabricmc.loader.impl.launch.server.FabricServerLauncher\r\n\r\n"})
	tool := jarOf(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: com.example.Tool\r\n\r\n"})
	fa := "--launchTarget forgeserver --fml.forgeVersion 47.3.0 --fml.mcVersion 1.20.1\n"
	cf := cfManifest("1.20.1", "forge-47.3.0")
	mr := mrpackIndex("")
	web := `{"name":"My PWA","short_name":"pwa","start_url":"/","icons":[]}`
	bat := "@echo off\r\njava -Xmx4G -jar core.jar nogui\r\n"
	fl := "libraries/net/minecraftforge/forge/1.20.1-47.3.0/win_args.txt"
	sp, pj := "server.properties", "paper-1.20.1-100.jar"
	m := func(kv ...string) map[string]string {
		out := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}
	const unknownCore, unrecognized = "看着像现成服务端，但没认出核心和版本", "没认出这个压缩包，既没有整合包清单，也看不出是服务端"
	cfPack := func(name, root string, files map[string]string) packShape {
		return packShape{name: name, files: files, format: FormatCurseForge, root: root, core: mcsrc.CoreForge, mc: "1.20.1"}
	}
	paperAt := func(name, root string, files map[string]string) packShape {
		return packShape{name: name, files: files, format: FormatServer, root: root, core: mcsrc.CorePaper, mc: "1.20.1"}
	}
	shapes := []packShape{
		cfPack("CF 服务端包：最外层 manifest.json 加 mods/", "", m("manifest.json", cf, "mods/a.jar", "m", "config/a.toml", "x")),
		cfPack("CF 客户端包：manifest.json 加 overrides/", "", m("manifest.json", cf, "overrides/config/a.toml", "x", "modlist.html", "<ul></ul>")),
		{name: "mrpack：modrinth.index.json 加 overrides/", files: m("modrinth.index.json", mr, "overrides/config/a.toml", "x"), format: FormatMrpack, core: mcsrc.CoreFabric, mc: "1.20.1"},
		{name: "ServerPackCreator：自己的 manifest.json 加装好的 Forge", files: m("manifest.json", spcManifest, "start.bat", bat, "mods/a.jar", "m", sp, "x=1\n", fl, fa),
			format: FormatServer, core: mcsrc.CoreForge, mc: "1.20.1"},
		{name: "套一层的 Forge 服务端", files: m("MyPack/run.bat", bat, "MyPack/mods/a.jar", "m", "MyPack/"+fl, fa), format: FormatServer, root: "MyPack", core: mcsrc.CoreForge, mc: "1.20.1"},
		paperAt("Paper 在最外层", "", m(pj, paper, "plugins/a.jar", "p", sp, "x=1\n", "eula.txt", "eula=true\n")),
		{name: "只有 server.jar 的原版", files: m("server.jar", vanilla), format: FormatServer, core: mcsrc.CoreVanilla, mc: "1.20.1"},
		{name: "只有 minecraft_server.1.12.2.jar", files: m("minecraft_server.1.12.2.jar", oldVanilla), format: FormatServer, core: mcsrc.CoreVanilla, mc: "1.12.2"},
		{name: "Fabric：fabric-server-launch.jar 加 server.jar 加 mods/", files: m("fabric-server-launch.jar", fabric, "server.jar", vanilla, "mods/a.jar", "m"),
			format: FormatServer, core: mcsrc.CoreFabric},
		paperAt("只有一个名字不在名单里的 jar（里面是 paperclip）加 start.bat", "", m("leaf-1.21.4.jar", paper, "start.bat", bat)),
		paperAt("只有一个改了名的 core.jar 加 start.bat", "", m("core.jar", paper, "start.bat", bat)),
		{name: "只有一个 1.20.1.jar（里面是原版）", files: m("1.20.1.jar", vanilla), format: FormatServer, core: mcsrc.CoreVanilla, mc: "1.20.1"},
		paperAt("只有一个 jar 加 start.bat 加 world/", "", m("arclight-forge-1.20.1.jar", paper, "start.bat", bat, "world/level.dat", "x")),
		paperAt("套一层，里面只有一个改了名的 jar 加 start.bat", "Srv", m("Srv/core.jar", paper, "Srv/start.bat", bat)),
		paperAt("purpur-1.20.1.jar 单独一个", "", m("purpur-1.20.1.jar", paper)),
		paperAt("最外层只多一个 eula.txt，里面套着 Paper", "Srv", m("eula.txt", "eula=true\n", "Srv/"+pj, paper, "Srv/"+sp, "x=1\n")),
		{name: "最外层有个别的 libraries/，里面套着 mrpack 清单", files: m("libraries/some.jar", "x", "Pack/modrinth.index.json", mr, "Pack/overrides/a.txt", "x"),
			format: FormatMrpack, root: "Pack", core: mcsrc.CoreFabric, mc: "1.20.1"},
		paperAt("最外层有个别的 libraries/，里面套着 Paper", "Srv", m("libraries/some.dll", "x", "Srv/"+pj, paper, "Srv/"+sp, "x=1\n")),
		cfPack("最外层只有 eula.txt，里面套着 CF 清单", "Pack", m("eula.txt", "eula=true\n", "Pack/manifest.json", cf, "Pack/overrides/a.txt", "x")),
		paperAt("包装文件夹旁边多一个工具 jar（A40 故意改的：以前认不出）", "MyServer", m("备份工具.jar", tool, "MyServer/"+sp, "x=1\n", "MyServer/"+pj, paper)),
		paperAt("最外层是 Paper，网页文件夹里有 manifest.json", "", m(pj, paper, sp, "x=1\n", "web/manifest.json", web)),
		paperAt("Mac 打的：平铺 Paper 加 __MACOSX 加 .DS_Store", "", m(pj, paper, sp, "x=1\n", ".DS_Store", "x", "__MACOSX/._server.properties", "x")),
		paperAt("Mac 打的：套一层 Paper 加 __MACOSX", "Srv", m("Srv/"+pj, paper, "Srv/"+sp, "x=1\n", "Srv/.DS_Store", "x", "__MACOSX/Srv/._server.properties", "x")),
		paperAt("Mac 打的：套两层 Paper 加 __MACOSX", "A/Srv", m("A/Srv/"+pj, paper, "A/Srv/"+sp, "x=1\n", "__MACOSX/A/Srv/._server.properties", "x")),
		paperAt("Mac 打的：套两层 Paper，中间一层有 .DS_Store", "A/Srv", m("A/Srv/"+pj, paper, "A/Srv/"+sp, "x=1\n", "A/.DS_Store", "x")),
		// ._ 文件和真文件夹并排、不在 __MACOSX/ 里（解压工具把资源分支直接放在旁边）：只靠 ._ 那条规矩剥得进去
		paperAt("Mac 打的：套两层 Paper，中间一层有和文件夹并排的 ._ 文件", "A/Srv", m("A/Srv/"+pj, paper, "A/Srv/"+sp, "x=1\n", "A/._Srv", "x")),
		paperAt("Mac 打的：最外层有和文件夹并排的 ._ 文件，套两层 Paper", "A/Srv", m("._A", "x", "A/Srv/"+pj, paper, "A/Srv/"+sp, "x=1\n")),
		paperAt("Mac 打的：套三层 Paper 加 __MACOSX", "A/B/Srv", m("A/B/Srv/"+pj, paper, "A/B/Srv/"+sp, "x=1\n", "__MACOSX/A/B/Srv/._server.properties", "x")),
		cfPack("Mac 打的：套一层 CF 清单加 __MACOSX", "Pack", m("Pack/manifest.json", cf, "Pack/overrides/a.txt", "x", "__MACOSX/Pack/._manifest.json", "x")),
		cfPack("Mac 打的：套两层 CF 清单加 __MACOSX", "A/Pack", m("A/Pack/manifest.json", cf, "A/Pack/overrides/a.txt", "x", "__MACOSX/A/Pack/._manifest.json", "x")),
		paperAt("套两层 Paper", "A/Srv", m("A/Srv/"+pj, paper, "A/Srv/"+sp, "x=1\n")),
		{name: "只有世界和 server.properties", files: m(sp, "x=1\n", "world/level.dat", "x"), hint: unknownCore},
		cfPack("套一层，CF 清单没写 manifestType", "Pack", m("Pack/manifest.json", cf, "Pack/overrides/a.txt", "x")),
		{name: "套一层，manifest.json 明写类型、版本空着", files: m("Pack/manifest.json", `{"manifestType":"minecraftModpack","minecraft":{"version":"","modLoaders":[]},"files":[]}`, "Pack/overrides/a.txt", "x"),
			hint: "无法识别整合包加载器"},
		cfPack("最外层 mods/ 加套一层 CF 清单", "Pack", m("mods/a.jar", "m", "Pack/manifest.json", cf)),
		// 最外层带着 mods/、plugins/ 的多半就是那台服务端本身，里面的服务端不拿来顶替（外面的模组、配置会丢），同基准
		{name: "最外层 mods/ 里有 fabric-api（名字像服务端 jar），里面套着 Paper", files: m("mods/fabric-api-0.92.2.jar", "x", "Srv/"+pj, paper, "Srv/"+sp, "x=1\n"), hint: unknownCore},
		{name: "最外层 plugins/ 里有 Paper 开头的插件，里面套着 Paper", files: m("plugins/PaperLib-1.0.jar", "x", "Srv/"+pj, paper, "Srv/"+sp, "x=1\n"), hint: unknownCore},
		{name: "没装加载器的服务端包（mods/、config/、eula.txt、安装器），backup/ 里放着一台老 Paper",
			files: m("mods/a.jar", "m", "config/a.toml", "x", "eula.txt", "eula=true\n", "forge-1.20.1-47.2.0-installer.jar", tool, "backup/"+pj, paper, "backup/"+sp, "x=1\n"),
			hint:  unknownCore},
		paperAt("最外层 eula.txt 加 libraries/ 里有名字像服务端的 jar，里面套着 Paper", "Srv",
			m("eula.txt", "eula=true\n", "libraries/paper-1.20.1-100.jar", "x", "Srv/"+pj, paper, "Srv/"+sp, "x=1\n")),
		paperAt("两个顶层文件夹：一个是服务端，一个里面有网页 manifest.json", "Srv", m("Srv/"+pj, paper, "Srv/"+sp, "x=1\n", "site/manifest.json", web)),
		paperAt("spigot-1.20.1.jar 单独一个", "", m("spigot-1.20.1.jar", paper)),
		{name: "老 Forge：forge-1.12.2-….jar 加 minecraft_server.1.12.2.jar 加 libraries/",
			files:  m("forge-1.12.2-14.23.5.2860.jar", tool, "minecraft_server.1.12.2.jar", oldVanilla, "libraries/x.jar", "x"),
			format: FormatServer, core: mcsrc.CoreForge, mc: "1.12.2"},
		paperAt("套一层，里面只有一个改了名的 jar 加 world/", "Srv", m("Srv/core.jar", paper, "Srv/world/level.dat", "x")),
		paperAt("套两层，里面只有改了名的 jar", "A/Srv", m("A/Srv/core.jar", paper, "A/Srv/start.bat", "x")),
		{name: "只有一个认不出的工具 jar", files: m("备份工具.jar", tool), hint: unrecognized},
		{name: "最外层有文件、jar 在文件夹里且名字认不出", files: m("readme.txt", "x", "Srv/core.jar", paper), hint: unrecognized},
	}
	for _, s := range shapes {
		p, err := Parse(mkZip(t, s.files))
		if s.hint != "" {
			var h interface{ Hint() string }
			if err == nil || !(errors.As(err, &h) && h.Hint() == s.hint || strings.Contains(err.Error(), s.hint)) {
				t.Errorf("%s：应认不出、报「%s」：%+v %v", s.name, s.hint, p, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s：应认出来：%v", s.name, err)
			continue
		}
		if p.Format != s.format || p.Root != s.root || p.Core != s.core || p.MC != s.mc {
			t.Errorf("%s：认成了 format=%s root=%q core=%s mc=%s，应为 format=%s root=%q core=%s mc=%s",
				s.name, p.Format, p.Root, p.Core, p.MC, s.format, s.root, s.core, s.mc)
		}
	}
}
