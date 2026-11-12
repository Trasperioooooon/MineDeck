package modpack

import (
	"errors"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
)

// ServerPackCreator 打的服务端包，最外层有一份它自己的 manifest.json。2026-11 正式版前检查时，
// 作者拿来的三份真包（BMC4、BMC5 Neo、Prominence II）都是这样，以前被当成 CurseForge 清单去解析，
// 报的是「解析 manifest.json 失败: json: cannot unmarshal string into ...」，没有人话。
const spcManifest = `{"files":["mods/a.jar","config/b.toml"],"minecraftVersion":"1.20.1","modloader":"Forge",` +
	`"modloaderVersion":"47.4.20","serverPackCreatorVersion":"8.1.2"}`

// 只带启动脚本、没装加载器的 ServerPackCreator 包：按服务端去认，认不出核心，报那句写好的提示。
func TestServerPackCreatorManifestIsNotModpack(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"最外层": {
			"manifest.json":     spcManifest,
			"variables.txt":     "MINECRAFT_VERSION=1.20.1\nMODLOADER=Forge\n",
			"start.bat":         "@echo off\r\n",
			"mods/a.jar":        "mod",
			"config/b.toml":     "x = 1\n",
			"server.properties": "server-port=25565\n",
		},
		"套一层": {
			"BMC4_ServerPack/manifest.json": spcManifest,
			"BMC4_ServerPack/start.bat":     "@echo off\r\n",
			"BMC4_ServerPack/mods/a.jar":    "mod",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(mkZip(t, files))
			var h interface{ Hint() string }
			if err == nil || !errors.As(err, &h) || h.Hint() != "看着像现成服务端，但没认出核心和版本" {
				t.Fatalf("应按服务端去认、报认不出核心：%v", err)
			}
			if strings.Contains(err.Error(), "解析 manifest.json 失败") {
				t.Fatalf("不该再当成 CurseForge 清单解析：%v", err)
			}
		})
	}
}

// 装好了加载器的 ServerPackCreator 包：按现成服务端导入。
func TestServerPackCreatorWithInstalledLoader(t *testing.T) {
	p, err := Parse(mkZip(t, map[string]string{
		"manifest.json": spcManifest,
		"mods/a.jar":    "mod",
		"libraries/net/minecraftforge/forge/1.20.1-47.4.20/win_args.txt": "--launchTarget forgeserver --fml.forgeVersion 47.4.20 --fml.mcVersion 1.20.1\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatServer || p.Core != mcsrc.CoreForge || p.MC != "1.20.1" || p.Loader != "47.4.20" {
		t.Fatalf("认成了 format=%s core=%s mc=%s loader=%s", p.Format, p.Core, p.MC, p.Loader)
	}
}

// 哪些 manifest.json 算 CurseForge 清单。
func TestCurseForgeManifestDetection(t *testing.T) {
	withType := `{"manifestType":"minecraftModpack","manifestVersion":1,"name":"带类型","overrides":"overrides",` +
		`"minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.3.0","primary":true}]},"files":[]}`
	p, err := Parse(mkZip(t, map[string]string{"manifest.json": withType}))
	if err != nil || p.Format != FormatCurseForge {
		t.Fatalf("写了 manifestType 的应按 CurseForge 解析：%+v %v", p, err)
	}
	// 没写 manifestType、但有 minecraft 那一段：老的或别的工具导出的，仍按 CurseForge
	p, err = Parse(mkZip(t, map[string]string{"manifest.json": cfManifest("1.20.1", "forge-47.3.0")}))
	if err != nil || p.Format != FormatCurseForge {
		t.Fatalf("有 minecraft 段的应按 CurseForge 解析：%+v %v", p, err)
	}
	// manifestType 写的是别的：不算整合包清单；这里也不像服务端，报认不出
	other := `{"manifestType":"somethingElse","minecraft":{"version":"1.20.1"}}`
	if _, err = Parse(mkZip(t, map[string]string{"manifest.json": other, "readme.txt": "x"})); err == nil || strings.Contains(err.Error(), "解析 manifest.json 失败") {
		t.Fatalf("manifestType 不是 minecraftModpack 的不该当 CurseForge 清单：%v", err)
	}
	// 不是 JSON：仍当成清单，让用户看到解析失败的原因
	if _, err = Parse(mkZip(t, map[string]string{"manifest.json": "不是 JSON"})); err == nil || !strings.Contains(err.Error(), "解析 manifest.json 失败") {
		t.Fatalf("坏掉的 manifest.json 应报解析失败：%v", err)
	}
}
