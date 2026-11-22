package inst

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"minedeck/internal/atomicfile"
	"minedeck/internal/mcsrc"
)

// A22：两台服务器通过硬链接或符号链接共用同一份 run.bat 时，给其中一台重写 run.bat 只应替换它自己的目录项，
// 不能把另一台共用的那份文件内容一起改掉。
func TestWriteRunBatDoesNotModifyLinkedTarget(t *testing.T) {
	const sharedContent = "@echo off\r\ntitle Shared - MineDeck\r\npause\r\n"

	t.Run("硬链接共用", func(t *testing.T) {
		otherDir := t.TempDir()
		sharedBat := filepath.Join(otherDir, "run.bat")
		if err := os.WriteFile(sharedBat, []byte(sharedContent), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		ownBat := filepath.Join(dir, "run.bat")
		if err := os.Link(sharedBat, ownBat); err != nil {
			t.Fatalf("创建硬链接失败：%v", err)
		}
		inst := &Instance{
			Settings: Settings{
				Name: "这一台自己的服", Core: mcsrc.CorePaper, MC: "1.20.1",
				JavaPath: `C:\java\bin\java.exe`, XmxMB: 4096, XmsMB: 1024,
				LaunchTarget: "jar:server.jar",
			},
			Dir: dir,
		}
		if err := writeRunBat(inst); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(sharedBat); err != nil || string(got) != sharedContent {
			t.Fatalf("另一台硬链接共用的 run.bat 被改了：%q（err=%v）", got, err)
		}
		if got, err := os.ReadFile(ownBat); err != nil || !strings.Contains(string(got), "这一台自己的服") {
			t.Fatalf("本服自己的 run.bat 内容不对：%q（err=%v）", got, err)
		}
	})

	t.Run("符号链接共用", func(t *testing.T) {
		otherDir := t.TempDir()
		sharedBat := filepath.Join(otherDir, "run.bat")
		if err := os.WriteFile(sharedBat, []byte(sharedContent), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		ownBat := filepath.Join(dir, "run.bat")
		if err := os.Symlink(sharedBat, ownBat); err != nil {
			if os.Getenv("CI") == "" && errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
				t.Skipf("账户没有创建符号链接权限，跳过：%v", err)
			}
			t.Fatal(err)
		}
		inst := &Instance{
			Settings: Settings{
				Name: "符号链接这一台", Core: mcsrc.CorePaper, MC: "1.20.1",
				JavaPath: `C:\java\bin\java.exe`, XmxMB: 4096, XmsMB: 1024,
				LaunchTarget: "jar:server.jar",
			},
			Dir: dir,
		}
		if err := writeRunBat(inst); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(sharedBat); err != nil || string(got) != sharedContent {
			t.Fatalf("另一台符号链接指向的 run.bat 被改了：%q（err=%v）", got, err)
		}
		st, err := os.Lstat(ownBat)
		if err != nil || !st.Mode().IsRegular() {
			t.Fatalf("替换后本服的 run.bat 应为独立普通文件：mode=%v err=%v", st.Mode(), err)
		}
		if got, err := os.ReadFile(ownBat); err != nil || !strings.Contains(string(got), "符号链接这一台") {
			t.Fatalf("本服自己的 run.bat 内容不对：%q（err=%v）", got, err)
		}
	})

	t.Run("经临时文件原子替换不留半截", func(t *testing.T) {
		dir := t.TempDir()
		ownBat := filepath.Join(dir, "run.bat")
		if err := os.WriteFile(ownBat, []byte(sharedContent), 0o644); err != nil {
			t.Fatal(err)
		}
		var sawOriginalBeforeSwap bool
		oldHook := atomicfile.Hook
		t.Cleanup(func() { atomicfile.Hook = oldHook })
		atomicfile.Hook = func(p string) {
			if p == ownBat {
				b, err := os.ReadFile(ownBat)
				sawOriginalBeforeSwap = err == nil && string(b) == sharedContent
			}
		}
		inst := &Instance{
			Settings: Settings{
				Name: "原子写入服", Core: mcsrc.CorePaper, MC: "1.20.1",
				JavaPath: `C:\java\bin\java.exe`, XmxMB: 2048, XmsMB: 1024,
				LaunchTarget: "jar:server.jar",
			},
			Dir: dir,
		}
		if err := writeRunBat(inst); err != nil {
			t.Fatal(err)
		}
		atomicfile.Hook = oldHook
		if !sawOriginalBeforeSwap {
			t.Fatal("writeRunBat 没有先写临时文件再原子替换原位上的 run.bat")
		}
	})
}
