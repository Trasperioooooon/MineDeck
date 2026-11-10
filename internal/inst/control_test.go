package inst

import (
	"os"
	"path/filepath"
	"testing"

	"minedeck/internal/app"
)

// 2026-11-10 合并前复审查出来的：server.properties 可以是别人给的（交接包、整合包的覆盖目录），
// level-name 写成 ../别的服务器 以后，备份会去打包那个文件夹，「还原备份」会先把它删掉。

func TestSafeLevelName(t *testing.T) {
	for _, ok := range []string{"world", "我的世界", "worlds/main", `worlds\main`, " world ", "world.backup", "a/../b"} {
		if !SafeLevelName(ok) {
			t.Errorf("%q 是实例目录里面的名字，应该认", ok)
		}
	}
	for _, bad := range []string{
		"", " ", ".", "./", "a/..", "world/..", // 空的，或者指回实例目录自己：还原时会把整台服务器删掉
		"..", "../other-server", `..\other-server`, "world/../..", "a/../../b", // 指到外面
		"/abs", `\abs`, "C:/Windows", `C:\Windows`, "C:world", `\\host\share\w`, // 绝对路径、盘符、网络路径
		"world:stream", "NUL", "con/x", // 备用数据流、保留设备名
	} {
		if SafeLevelName(bad) {
			t.Errorf("%q 不该被当成世界目录的名字", bad)
		}
	}
}

// TestWorldDirsIgnoresOutsideLevelName level-name 指到外面时按 world 算：备份只打实例自己的世界，
// 还原只删实例自己的世界，隔壁那台服务器的文件夹原样还在。
func TestWorldDirsIgnoresOutsideLevelName(t *testing.T) {
	m, dir := tempInstance(t)
	neighbour := filepath.Join(app.ServersDir, "other-server")
	writeTree(t, neighbour, map[string]string{"level.dat": "隔壁的世界", "precious.txt": "隔壁的东西"})
	for _, bad := range []string{"../other-server", "..", ".", "world/.."} {
		writeTree(t, dir, map[string]string{"server.properties": "level-name=" + bad + "\n"})
		i := m.insts["s"]
		if got := i.worldDirs(); len(got) != 1 || got[0] != "world" {
			t.Fatalf("level-name=%q 时世界目录应回到 world：%v", bad, got)
		}
		file, err := m.CreateBackup("s", "测试")
		if err != nil {
			t.Fatalf("level-name=%q 时备份失败：%v", bad, err)
		}
		writeLevel(t, dir, "之后又玩了一阵")
		if err := m.RestoreBackup("s", file); err != nil {
			t.Fatalf("level-name=%q 时还原失败：%v", bad, err)
		}
		if got := readLevel(t, dir); got != "current" {
			t.Errorf("level-name=%q：还原的应是实例自己的世界，实际 %q", bad, got)
		}
		for name, want := range map[string]string{"level.dat": "隔壁的世界", "precious.txt": "隔壁的东西"} {
			if b, err := os.ReadFile(filepath.Join(neighbour, name)); err != nil || string(b) != want {
				t.Fatalf("level-name=%q：隔壁服务器的 %s 被动了（%v %q）", bad, name, err, b)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "server.properties")); err != nil {
			t.Fatalf("level-name=%q：实例自己的文件不该被删：%v", bad, err)
		}
	}
	// 正常的名字照旧：带子目录的世界也认
	writeTree(t, dir, map[string]string{"server.properties": "level-name=worlds/main\n", "worlds/main/level.dat": "x"})
	if got := m.insts["s"].worldDirs(); len(got) != 1 || got[0] != "worlds/main" {
		t.Errorf("带子目录的世界名应照用：%v", got)
	}
}

func TestCheckLevelName(t *testing.T) {
	dir := t.TempDir()
	if _, ok := CheckLevelName(dir); !ok {
		t.Error("没有 server.properties 不算问题")
	}
	writeTree(t, dir, map[string]string{"server.properties": "server-port=25565\n"})
	if _, ok := CheckLevelName(dir); !ok {
		t.Error("没写 level-name 不算问题")
	}
	writeTree(t, dir, map[string]string{"server.properties": "level-name=world\n"})
	if _, ok := CheckLevelName(dir); !ok {
		t.Error("正常的 level-name 不算问题")
	}
	writeTree(t, dir, map[string]string{"server.properties": "level-name=../other-server\n"})
	if name, ok := CheckLevelName(dir); ok || name != "../other-server" {
		t.Errorf("指到外面的 level-name 要报出来：%q %v", name, ok)
	}
}

// TestIsControlFile MineDeck 自己读写的文件不能由别人给（交接包、整合包的覆盖目录）。
func TestIsControlFile(t *testing.T) {
	for _, c := range []string{
		"instance.json", "Instance.JSON", "instance.json.bak", "instance.json.tmp", "run.bat", "RUN.BAT",
		"run.before-minedeck.bat", "user_jvm_args.txt", "log4j2_112-116.xml", "gameai.json", "gameai-state.json",
		"config/minedeck/companion.properties", `Config\MineDeck\voicebridge.properties`, ".minedeck/config-backups/x",
		"logs/latest.log", "crash-reports/crash.txt",
	} {
		if !IsControlFile(c) {
			t.Errorf("%q 应算控制文件", c)
		}
	}
	for _, ok := range []string{
		"server.properties", "eula.txt", "world/level.dat", "mods/a.jar", "config/instance.json", "config/minedeck.toml",
		"config/voicechat/voicechat-server.properties", "plugins/logs/x.txt", "kubejs/server_scripts/run.bat",
	} {
		if IsControlFile(ok) {
			t.Errorf("%q 是普通的服务器文件，不该算控制文件", ok)
		}
	}
}

// TestOverrideAllowed 整合包的覆盖目录是在实例登记之后才写进来的：带一份 instance.json 会顶掉刚登记的那份，
// 重启 MineDeck 以后用哪个 Java、带什么参数、有哪些定时任务就都由整合包说了算。
func TestOverrideAllowed(t *testing.T) {
	for _, bad := range []string{
		"instance.json", "INSTANCE.JSON", "instance.json.", "instance.json ", "run.bat", "run.bat.",
		"user_jvm_args.txt", "log4j2_112-116.xml", "gameai.json", "config/minedeck/companion.properties",
		"config ./minedeck/x", ".minedeck/config-backups/a", "logs/latest.log",
		"NUL", "world/COM1", "a:b", // 写不成普通文件的名字
	} {
		if overrideAllowed(bad) {
			t.Errorf("覆盖目录里的 %q 不该写进实例", bad)
		}
	}
	for _, ok := range []string{
		"server.properties", "eula.txt", "config/create-common.toml", "mods/custom.jar", "kubejs/server_scripts/a.js",
		"defaultconfigs/x.toml", "world/datapacks/pack.zip", "server-icon.png", "config/instance.json",
	} {
		if !overrideAllowed(ok) {
			t.Errorf("覆盖目录里的 %q 是整合包正常会带的文件，应该照写", ok)
		}
	}
}

// TestKnownDownloadHost 走代理时不按本机解析结果拦的站点：内置名单，加上它们的文件实际所在的 CDN。
func TestKnownDownloadHost(t *testing.T) {
	for _, h := range []string{"cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "release-assets.githubusercontent.com",
		"objects.githubusercontent.com", "mediafilez.forgecdn.net", "edge.forgecdn.net", "gitlab.com"} {
		if !knownDownloadHost(h) {
			t.Errorf("%s 应算已知的下载站点", h)
		}
	}
	for _, h := range []string{"files.example.net", "githubusercontent.com.evil.example", "evilforgecdn.net", "modrinth.com.evil.example", "repo1.maven.org"} {
		if knownDownloadHost(h) {
			t.Errorf("%s 不该算已知的下载站点", h)
		}
	}
}
