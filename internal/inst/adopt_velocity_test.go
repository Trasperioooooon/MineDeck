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
	const paperOld = "zip:" + "META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Paperclip\r\n\r\n" +
		"|META-INF/versions.list=abc\t1.20.1\t1.20.1/paper-1.20.1.jar\n"
	toml := "config-version = \"2.7\"\nbind = \"0.0.0.0:25577\"\nonline-mode = true\n"

	t.Run("有 velocity.toml 时不因版本号更小而输给 Paper 或混合端", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "proxy")
		mohist := "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: com.mohistmc.MohistMCStart\r\n\r\n|META-INF/versions.list=abc\t1.20.1\t1.20.1/mohist-1.20.1.jar\n"
		writeTree(t, dir, map[string]string{"a-mohist.jar": mohist, "paper.jar": paper, "velocity.jar": vel, "velocity.toml": toml})
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

	t.Run("同分不同核心夹在中间时仍能选出同核心的新版本", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "sandwich")
		writeTree(t, dir, map[string]string{
			"a-paper-1.20.1.jar": paperOld,
			"b-velocity.jar":     vel,
			"c-paper-1.21.1.jar": paper,
		})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "paper" || sc.LaunchTarget != "jar:c-paper-1.21.1.jar" || sc.MC != "1.21.1" {
			t.Fatalf("应选出新版 Paper 1.21.1，实际：%+v", sc)
		}
	})

	t.Run("启动脚本明确点名 Paper 时，即使有 velocity.toml 也不说按 Velocity 预填", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "scripted")
		writeTree(t, dir, map[string]string{
			"paper.jar":     paper,
			"velocity.jar":  vel,
			"velocity.toml": toml,
			"run.bat":       "java -Xmx4G -jar paper.jar nogui\r\n",
		})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "paper" || sc.LaunchTarget != "jar:paper.jar" {
			t.Fatalf("启动脚本点名应选 Paper：%+v", sc)
		}
		notes := strings.Join(sc.Notes, "\n")
		if strings.Contains(notes, "按 Velocity 预填") {
			t.Fatalf("实际选了 Paper，提示不应说按 Velocity 预填：%s", notes)
		}
	})

	t.Run("普通 Fabric 与配套原版 jar 并存不误报多种服务端", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "fabric-std")
		fab := "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: net.fabricmc.loader.impl.launch.server.FabricServerLauncher\r\n\r\n|install.properties=game-version=1.21.1\nfabric-loader-version=0.16.9\n"
		van := "zip:META-INF/MANIFEST.MF=Manifest-Version: 1.0\r\nMain-Class: net.minecraft.bundler.Main\r\n\r\n|version.json={\"id\":\"1.21.1\",\"java_version\":21}"
		writeTree(t, dir, map[string]string{"fabric-server-launch.jar": fab, "server.jar": van})
		sc, err := scanServerDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Core != "fabric" {
			t.Fatalf("应认成 Fabric：%+v", sc)
		}
		if strings.Contains(strings.Join(sc.Notes, "\n"), "不止一种服务端") {
			t.Fatalf("标准 Fabric + server.jar 不应误报混放：%v", sc.Notes)
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
