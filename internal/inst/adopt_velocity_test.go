package inst

import (
	"path/filepath"
	"strings"
	"testing"
)

// 官方 Velocity 3.4.0-566 与 4.3.1-45 的清单是读 zip 看到的，没有运行 jar。
// Main-Class 都是 com.velocitypowered.proxy.Velocity，bind 仍是一行字符串。
// 1.1.9 只用来证明不能拿代理端版本号和游戏版本比大小。
func TestScanVelocityJarAndToml(t *testing.T) {
	const vel = "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nImplementation-Version: 1.1.9\r\nImplementation-Title: Velocity\r\nMain-Class: com.velocitypowered.proxy.Velocity\r\n\r\n"
	const paper = "zip:" + "META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Paperclip\r\n\r\n" +
		"|META-INF/versions.list=abc\t1.21.1\t1.21.1/paper-1.21.1.jar\n"
	toml := "config-version = \"2.7\"\nbind = \"0.0.0.0:25577\"\nonline-mode = true\n"

	t.Run("有 velocity.toml 时不因版本号更小而输给 Paper", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "proxy")
		writeTree(t, dir, map[string]string{"paper.jar": paper, "velocity.jar": vel, "velocity.toml": toml})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "velocity" || sc.LaunchTarget != "jar:velocity.jar" || sc.MC != "1.1.9" {
			t.Fatalf("认成了 %+v", sc)
		}
		if !strings.Contains(strings.Join(sc.Notes, "\n"), "velocity.toml") {
			t.Fatalf("应说明为什么选 Velocity：%v", sc.Notes)
		}
		if got := mustRead(t, filepath.Join(dir, "velocity.toml")); got != toml {
			t.Fatalf("配置被改了：%q", got)
		}
	})

	t.Run("没有 toml 时不拿版本号把 Paper 比下去", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mix")
		writeTree(t, dir, map[string]string{"paper.jar": paper, "velocity.jar": vel})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "paper" || sc.LaunchTarget != "jar:paper.jar" {
			t.Fatalf("没有 toml 时按文件名应先是 Paper：%+v", sc)
		}
		if !strings.Contains(strings.Join(sc.Notes, "\n"), "不止一种") {
			t.Fatalf("应说明有歧义：%v", sc.Notes)
		}
	})

	t.Run("非标准文件名靠清单", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "named")
		body := "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nImplementation-Version: 3.4.0\r\nImplementation-Title: Velocity\r\nMain-Class: com.velocitypowered.proxy.Velocity\r\n\r\n"
		writeTree(t, dir, map[string]string{"proxy.jar": body})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "velocity" || sc.LaunchTarget != "jar:proxy.jar" || sc.MC != "3.4.0" {
			t.Fatalf("%+v", sc)
		}
	})

	t.Run("只有 toml 没有 jar", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "empty")
		writeTree(t, dir, map[string]string{"velocity.toml": toml})
		_, err := scanServerDir(dir)
		if err == nil || !strings.Contains(err.Error(), "velocity.toml") {
			t.Fatalf("应说明有 toml 但没有 jar：%v", err)
		}
		if got := mustRead(t, filepath.Join(dir, "velocity.toml")); got != toml {
			t.Fatalf("配置被改了：%q", got)
		}
	})
}

func TestScanTaiyitistAsBanner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ty")
	// Teneted/Taiyitist 1.21.1 分支 build.gradle 写的主类。没有运行 jar。
	body := "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: com.taiyitistmc.TaiyitistLauncher\r\nImplementation-Title: Taiyitist\r\n\r\n"
	writeTree(t, dir, map[string]string{"taiyitist-launcher.jar": body})
	sc, err := scanServerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Core != "banner" || sc.LaunchTarget != "jar:taiyitist-launcher.jar" {
		t.Fatalf("%+v", sc)
	}
	if !strings.Contains(strings.Join(sc.Notes, "\n"), "Taiyitist") {
		t.Fatalf("应说明这是改名后的 Banner：%v", sc.Notes)
	}
}
