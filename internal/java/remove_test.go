package java

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
		low := strings.ToLower(string(out))
		if strings.Contains(low, "privilege") || strings.Contains(low, "not support") || strings.Contains(low, "权限") || strings.Contains(low, "不支持") {
			t.Skipf("建不了目录联接：%v %s", err, out)
		}
		t.Fatalf("建不了目录联接：%v %s", err, out)
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

// 卸载停在「查完有没有人用、还没删」那一刻时把服务器换成这套：换不上（ErrRemoving，真正去换的那一步不调），
// 别的大版本照常换；卸完以后不再挡：重新装上就能换（A34）。
func TestUsePortableRefusedWhileRemoving(t *testing.T) {
	runtimesIn(t)
	touch(t, Exe(17))
	touch(t, Exe(21))
	reached, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	old := removeTree
	removeTree = func(dir string) error { close(reached); <-release; return old(dir) }
	var removeErr error
	finished := make(chan struct{})
	t.Cleanup(func() { unblock(); <-finished; removeTree = old }) // 中途失败也让卸载走完，不留着「正在卸载」
	go func() { removeErr = RemovePortable(17, func(string) error { return nil }); close(finished) }()
	<-reached

	called := false
	if err := UsePortable(17, func() error { called = true; return nil }); !errors.Is(err, ErrRemoving) || called {
		t.Fatalf("卸载途中换成这套应被拒、不去换：err=%v 换了=%v", err, called)
	}
	if err := UsePortable(21, func() error { return nil }); err != nil {
		t.Fatalf("别的大版本不受影响：%v", err)
	}
	unblock()
	<-finished
	if removeErr != nil {
		t.Fatal(removeErr)
	}
	touch(t, Exe(17)) // 卸完以后重新装上
	if err := UsePortable(17, func() error { return nil }); err != nil {
		t.Fatalf("卸完以后不该再挡：%v", err)
	}
}

// 换 Java 刚登记了自己、还没看有没有在卸载的那一刻卸载这套：卸载看得到它，报 ErrBusy，什么都不删；
// 换 Java 随后照常换上（A34）。钉住换 Java 这边「先登记自己、再看对方」的先后。
func TestRemovePortableBusyWhenSwitchMarkedFirst(t *testing.T) {
	runtimesIn(t)
	touch(t, Exe(17))
	paused, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	old := afterSwitchMarked
	afterSwitchMarked = func() { close(paused); <-resume }
	var setErr error
	called := false
	done := make(chan struct{})
	t.Cleanup(func() { release(); <-done; afterSwitchMarked = old }) // 中途失败也让换 Java 走完，不留着「正在换」
	go func() { setErr = UsePortable(17, func() error { called = true; return nil }); close(done) }()
	<-paused

	inUseCalled := false
	if err := RemovePortable(17, func(string) error { inUseCalled = true; return nil }); !errors.Is(err, ErrBusy) || inUseCalled {
		t.Fatalf("换 Java 已经登记时卸载应报 ErrBusy、不往下查：err=%v 查了有没有人用=%v", err, inUseCalled)
	}
	if !IsInstalled(17) {
		t.Fatal("报忙时不该动这套 Java")
	}
	release()
	<-done
	if setErr != nil || !called {
		t.Fatalf("卸载报忙以后，换 Java 应照常换上：err=%v 换了=%v", setErr, called)
	}
}

// 卸载刚登记了自己、还没看有没有人在换的那一刻换成这套：换 Java 看得到它，被拒（ErrRemoving），真正去换的那一步不调；
// 卸载随后照常卸掉（A34）。钉住卸载这边「先登记自己、再看对方」的先后。
func TestUsePortableRefusedWhenRemoveMarkedFirst(t *testing.T) {
	runtimesIn(t)
	touch(t, Exe(17))
	paused, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	old := afterRemoveMarked
	afterRemoveMarked = func() { close(paused); <-resume }
	var removeErr error
	done := make(chan struct{})
	t.Cleanup(func() { release(); <-done; afterRemoveMarked = old }) // 中途失败也让卸载走完，不留着「正在卸载」和安装锁
	go func() { removeErr = RemovePortable(17, func(string) error { return nil }); close(done) }()
	<-paused

	called := false
	if err := UsePortable(17, func() error { called = true; return nil }); !errors.Is(err, ErrRemoving) || called {
		t.Fatalf("卸载已经登记时换成这套应被拒、不去换：err=%v 换了=%v", err, called)
	}
	release()
	<-done
	if removeErr != nil || IsInstalled(17) {
		t.Fatalf("换 Java 被拒以后，卸载应照常卸掉：err=%v 还装着=%v", removeErr, IsInstalled(17))
	}
}

// 卸到一半失败（java.exe 删不掉、留着「没装完」记号）或者已经卸干净的便携 Java 不让换上：返回 ErrNotInstalled，
// 真正去换的那一步不调（A34）。调用方查「装着」和换之间，卸载可能已经走完了。
func TestUsePortableRefusesRemovedOrHalfRemoved(t *testing.T) {
	runtimesIn(t)
	touch(t, Exe(17))
	old := removeTree
	removeTree = func(string) error { return errors.New("被别的程序占着，删不动") }
	t.Cleanup(func() { removeTree = old })
	if err := RemovePortable(17, nil); err == nil {
		t.Fatal("这次卸载应该失败")
	}
	if _, err := os.Stat(Exe(17)); err != nil {
		t.Fatalf("卸到一半，java.exe 应还在：%v", err)
	}
	called := false
	if err := UsePortable(17, func() error { called = true; return nil }); !errors.Is(err, ErrNotInstalled) || called {
		t.Fatalf("卸到一半的这套不该换上：err=%v 换了=%v", err, called)
	}

	removeTree = old
	if err := RemovePortable(17, nil); err != nil {
		t.Fatal(err)
	}
	if err := UsePortable(17, func() error { called = true; return nil }); !errors.Is(err, ErrNotInstalled) || called {
		t.Fatalf("卸干净的这套不该换上：err=%v 换了=%v", err, called)
	}
}

// 有服务器正在换成这套便携 Java（换的那一步还没做完）时卸载：报 ErrBusy，什么都不删；换完以后照常能卸（A34）。
func TestRemovePortableBusyWhileSwitching(t *testing.T) {
	runtimesIn(t)
	touch(t, Exe(21))
	inSet, finish, switched := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(finish) })
	t.Cleanup(func() { release(); <-switched }) // 中途失败也让换 Java 走完，不留着「正在换」
	go func() { _ = UsePortable(21, func() error { close(inSet); <-finish; return nil }); close(switched) }()
	<-inSet

	if err := RemovePortable(21, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("换 Java 途中卸载应报 ErrBusy：%v", err)
	}
	if !IsInstalled(21) {
		t.Fatal("报忙时不该动这套 Java")
	}
	release()
	<-switched
	if err := RemovePortable(21, nil); err != nil {
		t.Fatalf("换完以后应能卸：%v", err)
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
