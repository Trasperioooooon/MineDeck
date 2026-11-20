package java

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovePortableOnlyOwnRuntime(t *testing.T) {
	runtimesIn(t)
	dir := Dir(21)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Exe(21), []byte("not a real jvm"), 0o644); err != nil {
		t.Fatal(err)
	}
	system := filepath.Join(t.TempDir(), "Program Files", "Java", "bin", "java.exe")
	if err := os.MkdirAll(filepath.Dir(system), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte("system"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemovePortable(21, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("便携目录应删掉：%v", err)
	}
	if _, err := os.Lstat(incompleteMark(21)); !os.IsNotExist(err) {
		t.Fatalf("记号应一起去掉：%v", err)
	}
	if b, err := os.ReadFile(system); err != nil || string(b) != "system" {
		t.Fatalf("系统 Java 被动过：%v %q", err, b)
	}
	if IsInstalled(21) {
		t.Fatal("删掉以后不应算已安装")
	}
}

func TestRemovePortableRefusesInUseAndSymlink(t *testing.T) {
	runtimesIn(t)
	dir := Dir(17)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Exe(17), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RemovePortable(17, func(got string) error {
		if got != dir {
			t.Fatalf("回调拿到的目录：%s", got)
		}
		return errors.New("生存服还配着它")
	})
	if err == nil || !strings.Contains(err.Error(), "生存服") {
		t.Fatalf("被占用应拒绝：%v", err)
	}
	if _, statErr := os.Stat(Exe(17)); statErr != nil {
		t.Fatalf("拒绝以后文件还应在：%v", statErr)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, dir); err != nil {
		t.Skip("这个环境建不了符号链接，真 Windows 上有权限时应拒绝删除链接：" + err.Error())
	}
	if st, err := os.Lstat(dir); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Skip("这个环境的符号链接读不出来（Wine 常见），不把读不到当成已经删掉")
	}
	target := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemovePortable(17, nil); err == nil || !strings.Contains(err.Error(), "链接") {
		t.Fatalf("链接应拒绝：%v", err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "keep" {
		t.Fatalf("链接目标被动过：%v %q", err, b)
	}
}

func TestRemovePortableKeepsMarkWhenDeleteFails(t *testing.T) {
	runtimesIn(t)
	dir := Dir(11)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Exe(11), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := removeTree
	removeTree = func(string) error { return errors.New("磁盘忙") }
	t.Cleanup(func() { removeTree = old })
	err := RemovePortable(11, nil)
	if err == nil || !strings.Contains(err.Error(), "删到一半") {
		t.Fatalf("应报删到一半：%v", err)
	}
	if IsInstalled(11) {
		t.Fatal("删失败以后不能算已安装")
	}
	if _, statErr := os.Lstat(incompleteMark(11)); statErr != nil {
		t.Fatalf("记号应留下：%v", statErr)
	}
	if _, statErr := os.Stat(Exe(11)); statErr != nil {
		t.Fatalf("没删成的 java.exe 还应在：%v", statErr)
	}
}
