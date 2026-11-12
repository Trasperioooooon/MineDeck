package modpack

import (
	"errors"
	"strings"
	"testing"
)

func TestUninstalledServerPackGuidance(t *testing.T) {
	installer := jarOf(t, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.minecraftforge.installer.SimpleInstaller\r\n\r\n",
	})
	for name, files := range map[string]map[string]string{
		"只有启动脚本": {
			"startserver.bat":  "@echo off\r\nrem install the loader first\r\n",
			"mods/example.jar": "placeholder",
		},
		"只有安装器": {
			"forge-1.20.1-47.2.20-installer.jar": installer,
			"mods/example.jar":                   "placeholder",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(mkZip(t, files))
			var hint interface{ Hint() string }
			if err == nil || !errors.As(err, &hint) || hint.Hint() != "看着像现成服务端，但没认出核心和版本" {
				t.Fatalf("未安装加载器的包应拒绝并保留原有提示：%v", err)
			}
			detail := err.Error()
			previous := -1
			for _, step := range []string{"先解压", "启动脚本或安装器", "把加载器装好", "接管已有服务器"} {
				position := strings.Index(detail, step)
				if position <= previous {
					t.Fatalf("详情应依次说明解压、安装加载器、再接管；缺少或顺序不对：%q；原文：%s", step, detail)
				}
				previous = position
			}
		})
	}
}
