package inst

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/events"
	"minedeck/internal/mcsrc"
)

// fakeServerEnv 设了它，测试程序就扮演服务器进程（TestMain 里先看）：打出 Done，然后按标准输入里的命令行事。
const fakeServerEnv = "MINEDECK_FAKE_SERVER"

// fakeSaveEnv 仿真服务端收到 save-all flush 怎么回：world = 回「Saved the world」，silent = 不回，别的 = 回「Saved the game」。
const fakeSaveEnv = "MINEDECK_FAKE_SAVE"

// fakeServer stop：像原版那样打「Stopping the server」再退出；opstop：游戏里的管理员发的 stop；
// crash：什么都不说直接退出码 0（模组加载失败时 Forge 就是这样）。
func fakeServer() int {
	fmt.Println(`[12:00:00] [Server thread/INFO]: Done (1.0s)! For help, type "help"`)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "stop":
			fmt.Println("[12:00:01] [Server thread/INFO]: Stopping the server")
			return 0
		case "opstop":
			fmt.Println("[12:00:01] [Server thread/INFO]: [Steve: Stopping the server]")
			return 0
		case "crash":
			return 0
		case "save-all flush":
			switch os.Getenv(fakeSaveEnv) {
			case "world": // 1.12.2 及更早的说法
				fmt.Println("[12:00:01] [Server thread/INFO]: Saved the world")
			case "silent": // 保存得慢，迟迟不回确认
			default:
				fmt.Println("[12:00:01] [Server thread/INFO]: Saved the game")
			}
		case "save-on":
			fmt.Println("[12:00:01] [Server thread/INFO]: Automatic saving enabled")
		}
	}
	return 0
}

// 控制台发 stop、游戏里的管理员发 stop 都是用户自己要停：退出时发 instance.stop、不走崩溃自动重启；
// 没人要停、进程自己以退出码 0 退了，仍算崩溃，而且不说「正常关闭」（测试报告：控制台 stop 后被崩溃自动重启拉起来）。
func TestConsoleStopIsUserStop(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeServerEnv, "1")
	var mu sync.Mutex
	got := map[string][]string{} // 实例名 → 事件
	events.Subscribe(func(e events.Event) {
		if n, _ := e.Data["instance"].(string); strings.HasPrefix(n, "停服测试") {
			mu.Lock()
			got[n] = append(got[n], e.Type)
			mu.Unlock()
		}
	})
	for _, c := range []struct {
		cmd, event, line string
		crash            bool
	}{
		{"stop", "instance.stop", "[MineDeck] 服务器已正常关闭", false},
		{"/STOP", "", "", false}, // 大小写、前面的 / 也认（假服务器不认这条，进程不会退；只看状态）
		{"opstop", "instance.stop", "[MineDeck] 服务器正在关闭（有人执行了 stop）", false},
		{"crash", "instance.crash", "[MineDeck] 服务器进程已退出（退出码 0，但没人让它停）", true},
	} {
		t.Run(c.cmd, func(t *testing.T) {
			name := "停服测试-" + c.cmd
			dir := t.TempDir()
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			ln.Close()
			_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte(fmt.Sprintf("server-port=%d\n", port)), 0o644)
			i := &Instance{Settings: Settings{Name: name, Core: mcsrc.CoreVanilla, JavaPath: exe, XmxMB: 64, XmsMB: 64,
				LaunchTarget: "jar:server.jar", Policies: Policies{CrashRestart: true}}, Dir: dir, Console: newConsole()}
			m := &Manager{insts: map[string]*Instance{name: i}, creating: map[string]bool{}}
			if err := m.Start(name); err != nil {
				t.Fatal(err)
			}
			waitStatus(t, i, "running")
			if err := m.Command(name, c.cmd); err != nil {
				t.Fatal(err)
			}
			if c.event == "" {
				if st := i.Status(); st != "stopping" {
					t.Errorf("发了 %s，状态应是 stopping：%s", c.cmd, st)
				}
				_ = m.Kill(name)
				waitStatus(t, i, "stopped")
				return
			}
			waitStatus(t, i, "stopped")
			deadline := time.Now().Add(3 * time.Second)
			for {
				mu.Lock()
				evs := slices.Clone(got[name])
				mu.Unlock()
				if slices.Contains(evs, c.event) || time.Now().After(deadline) {
					if !slices.Contains(evs, c.event) {
						t.Errorf("事件：%v，应有 %s", evs, c.event)
					}
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond) // 崩溃自动重启一开始就往控制台写「检测到服务器异常退出」
			lines := i.Console.Lines()
			if !slices.Contains(lines, c.line) {
				t.Errorf("控制台里应有 %q：%q", c.line, lines)
			}
			restarting := slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "检测到服务器异常退出") })
			if restarting != c.crash {
				t.Errorf("崩溃自动重启：%v，应为 %v：%q", restarting, c.crash, lines)
			}
			if slices.Contains(lines, "[MineDeck] 服务器已正常关闭") && c.crash {
				t.Error("没人要停服，不能说正常关闭")
			}
			i.procMu.Lock()
			i.runGen++ // 让退避中的那次崩溃重启作废，不再真的拉起进程
			i.procMu.Unlock()
		})
	}
}

func waitStatus(t *testing.T, i *Instance, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for i.Status() != want {
		if time.Now().After(deadline) {
			t.Fatalf("等 %s 超时，现在是 %s：%q", want, i.Status(), i.Console.Lines())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 停服那一行：控制台和游戏里管理员发的都认，聊天里有人打这句话不算。
func TestStoppingLine(t *testing.T) {
	for line, want := range map[string]bool{
		"[12:34:03] [Server thread/INFO]: Stopping the server":                                                       true,
		"[02Nov2026 12:34:03.123] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Stopping the server": true,
		"[12:34:03] [Server thread/INFO]: [Steve: Stopping the server]":                                              true,
		"[12:34:03] [Server thread/INFO]: [Rcon: Stopping the server]":                                               true,
		"[12:34:03] [Server thread/INFO]: <Steve> Stopping the server":                                               false,
		"[12:34:03] [Server thread/INFO]: [Not Secure] <Steve> Stopping the server":                                  false,
		"[12:34:03] [Server thread/INFO]: Stopping server":                                                           false,
	} {
		if got := stoppingLineRe.MatchString(line); got != want {
			t.Errorf("%q：%v，应为 %v", line, got, want)
		}
	}
	for _, c := range []struct {
		core mcsrc.Core
		cmd  string
		want bool
	}{
		{mcsrc.CorePaper, "stop", true}, {mcsrc.CorePaper, "/stop", true}, {mcsrc.CorePaper, "STOP", true},
		{mcsrc.CorePaper, "stopsound @a", false}, {mcsrc.CorePaper, "say stop", false}, {mcsrc.CorePaper, "end", false},
		{mcsrc.CoreVelocity, "shutdown", true}, {mcsrc.CoreVelocity, "end", true},
		{mcsrc.CoreWaterfall, "end", true}, {mcsrc.CoreWaterfall, "stop", false},
	} {
		if got := stopCommand(c.core, c.cmd); got != c.want {
			t.Errorf("stopCommand(%s, %q) = %v，应为 %v", c.core, c.cmd, got, c.want)
		}
	}
}

// instance.json 开头有 UTF-8 BOM（记事本「UTF-8 带 BOM」、PowerShell 5.1 写的）照样读得出来，不让服务器从列表里消失；
// 保存时先写临时文件再改名，不留临时文件。
func TestReadSettingsWithBOM(t *testing.T) {
	dir := t.TempDir()
	body := `{"name":"S1","core":"neoforge","mc":"1.21.1","xmxMb":4096}`
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), append([]byte("\xef\xbb\xbf"), body...), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := readSettings(dir)
	if err != nil || s.Name != "S1" || s.XmxMB != 4096 {
		t.Fatalf("带 BOM 的 instance.json：%+v %v", s, err)
	}
	i := &Instance{Settings: s, Dir: dir}
	i.XmxMB = 5120
	if err := (&Manager{}).saveInstance(i); err != nil {
		t.Fatal(err)
	}
	if s, err := readSettings(dir); err != nil || s.XmxMB != 5120 {
		t.Fatalf("保存后：%+v %v", s, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("不该留下临时文件：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), []byte(`{"core":"paper"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSettings(dir); err == nil {
		t.Error("没有 name 的 instance.json 应报错")
	}
	if _, err := readSettings(t.TempDir()); !os.IsNotExist(err) {
		t.Errorf("没有 instance.json 的目录：%v", err)
	}
}
