package java

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"minedeck/internal/app"
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
		if os.Getenv("CI") == "" && runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skip("这个环境建不了符号链接，真 Windows 上有权限时应拒绝删除链接：" + err.Error())
		}
		t.Fatalf("建符号链接失败：%v", err)
	}
	if st, err := os.Lstat(dir); err != nil || st.Mode()&os.ModeSymlink == 0 {
		if os.Getenv("CI") == "" {
			t.Skip("这个环境的符号链接读不出来（Wine 常见），不把读不到当成已经删掉")
		}
		t.Fatalf("符号链接读不出来：%v %v", st, err)
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

func TestRemovePortableRefusesJunctionAndNestedJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接仅在 Windows 上存在")
	}
	runtimesIn(t)
	if err := os.MkdirAll(app.RuntimesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	keepFile := filepath.Join(outside, "bin", "java.exe")
	if err := os.MkdirAll(filepath.Dir(keepFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepFile, []byte("external-java"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. java-17 本身是指向外部目录的目录联接（mklink /J）
	dir := Dir(17)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dir, outside).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(dir) })
	if err := RemovePortable(17, nil); err == nil || !strings.Contains(err.Error(), "目录联接") {
		t.Fatalf("目录联接应拒绝删除：%v", err)
	}
	if b, err := os.ReadFile(keepFile); err != nil || string(b) != "external-java" {
		t.Fatalf("联接目标被动过：%v %q", err, b)
	}
	_ = os.Remove(dir)

	// 2. java-21 是真文件夹，但里面含指向外部目录的子联接
	dir21 := Dir(21)
	if err := os.MkdirAll(filepath.Join(dir21, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Exe(21), []byte("jvm"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir21, "ext-junc")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", nested, outside).CombinedOutput(); err != nil {
		t.Fatalf("建子联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(nested) })
	if err := RemovePortable(21, nil); err == nil || !strings.Contains(err.Error(), "目录联接") {
		t.Fatalf("含子联接应拒绝删除：%v", err)
	}
	if b, err := os.ReadFile(keepFile); err != nil || string(b) != "external-java" {
		t.Fatalf("子联接目标被动过：%v %q", err, b)
	}
}

func TestRemovePortableKeepsMarkWhenDeleteFailsAndAllowsCleanup(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "未能删干净") {
		t.Fatalf("应报未能删干净：%v", err)
	}
	if IsInstalled(11) {
		t.Fatal("删失败以后不能算已安装")
	}
	if got := IncompletePortable(); !slices.Equal(got, []int{11}) {
		t.Fatalf("IncompletePortable() = %v，应列出 [11] 供再次清理", got)
	}
	if _, statErr := os.Lstat(incompleteMark(11)); statErr != nil {
		t.Fatalf("记号应留下：%v", statErr)
	}
	if _, statErr := os.Stat(Exe(11)); statErr != nil {
		t.Fatalf("没删成的 java.exe 还应在：%v", statErr)
	}

	// 恢复删除函数后再删一次，半截目录与记号应一并清干净
	removeTree = old
	if err := RemovePortable(11, nil); err != nil {
		t.Fatalf("重试清理应成功：%v", err)
	}
	if len(IncompletePortable()) != 0 {
		t.Fatalf("清理完成后不应再有残留：%v", IncompletePortable())
	}
	if _, statErr := os.Lstat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("目录应已删除：%v", statErr)
	}
}
