package atomicfile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWriteReadOnlyAndBusyTarget(t *testing.T) {
	p := filepath.Join(t.TempDir(), "readonly.txt")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	u, _ := windows.UTF16PtrFromString(p)
	h, err := windows.CreateFile(u, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = Write(p, []byte("blocked"), 0o644)
	windows.CloseHandle(h)
	if err == nil {
		t.Fatal("占用中的只读目标不应被换掉")
	}
	before, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	if string(before) != "old" || st.Mode().Perm()&0o200 != 0 {
		t.Fatalf("失败改变了旧内容或只读属性：%q %v", before, st.Mode())
	}
	if err := Write(p, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if string(after) != "new" {
		t.Fatalf("没有保存新内容：%q", after)
	}
	assertNoTemps(t, filepath.Dir(p))
}

func TestReadOnlyRetryDoesNotChangeReplacement(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "target.txt")
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(p, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Hook = nil; os.Chmod(p, 0o644); os.Chmod(original, 0o644) })
	Hook = func(string) {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(p, original); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("other"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := Write(p, []byte("new"), 0o644); err == nil {
		t.Fatal("不应重试覆盖被其他程序换上的只读文件")
	}
	Hook = nil
	got, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	if string(got) != "other" || st.Mode().Perm()&0o200 != 0 {
		t.Fatalf("改变了替换目标：%q %v", got, st.Mode())
	}
	assertNoTemps(t, dir)
}

type brokenReader struct{ read bool }

func (r *brokenReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, "half"), nil
	}
	return 0, io.ErrUnexpectedEOF
}
func TestCopyAndValidationFailureKeepOriginal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Copy(p, &brokenReader{}, 0o644); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if err := WriteChecked(p, []byte("invalid"), 0o644, func(string) error { return os.ErrInvalid }); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "old" {
		t.Fatalf("失败覆盖了原文件：%q", got)
	}
	if err := Copy(p, bytes.NewReader([]byte("good")), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(p)
	if string(got) != "good" {
		t.Fatal("正常复制没有生效")
	}
	assertNoTemps(t, dir)
}

func TestPathLocksNormalizeAndCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "target")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(p)
	for _, alias := range []string{strings.ToUpper(abs), filepath.Join(dir, "unused", "..", "target")} {
		release := LockPath(p)
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		got, err := LockPathContext(ctx, alias)
		cancel()
		release()
		if got != nil {
			got()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("别名取得另一把锁：%q %v", alias, err)
		}
	}
	release := LockPath(p)
	release()
	release()
	pathLocks.Lock()
	left := len(pathLocks.m)
	pathLocks.Unlock()
	if left != 0 {
		t.Fatalf("锁没有回收：%d", left)
	}
}

func TestPathLocksResolveDirectoryAlias(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	alias := filepath.Join(dir, "alias")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	// 目录联接不需要创建符号链接的管理员权限，正是实例路径常用的别名。
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", alias, real).CombinedOutput(); err != nil {
		t.Fatalf("创建目录联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(alias) })
	if PathKey(alias) == PathKey(real) {
		t.Fatal("最终联接目录项被跟随，替换目录项会改变锁键")
	}
	p := filepath.Join(real, "not-created-yet")
	release := LockPath(p)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	got, err := LockPathContext(ctx, filepath.Join(alias, "not-created-yet"))
	if got != nil {
		got()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("目录别名取得另一把锁：%v", err)
	}
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("残留临时文件：%s", e.Name())
		}
	}
}

func TestLongComponentAndProtectedReadOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, strings.Repeat("x", 240)+".txt")
	if err := Write(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	if err := WriteProtected(p, []byte("new"), 0o644); err == nil {
		t.Fatal("只读保护被覆盖")
	}
	got, _ := os.ReadFile(p)
	if string(got) != "original" {
		t.Fatal("只读目标改变")
	}
	assertNoTemps(t, dir)
}

func TestFileSymlinkReplacementKeepsQueuedWritersTogether(t *testing.T) {
	dir := t.TempDir()
	referent := filepath.Join(dir, "shared.properties")
	entry := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(referent, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, entry); err != nil {
		if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
			t.Skipf("无法执行真实文件symlink用例：账户没有创建符号链接权限：%v", err)
		}
		t.Fatal(err)
	}
	before := PathKey(entry)
	overlap := exerciseNamespaceWaiters(t, func() string { return PathKey(entry) }, func() {
		if err := Write(entry, []byte("replacement"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if overlap || PathKey(entry) != before {
		t.Fatal("文件符号链接被替换后锁键分裂")
	}
	if got, _ := os.ReadFile(referent); string(got) != "original" {
		t.Fatal("替换修改了引用目标")
	}
	if got, _ := os.ReadFile(entry); string(got) != "replacement" {
		t.Fatal("没有替换链接目录项")
	}
}

func TestPathKeyStableAcrossAtomicReplaceAndShortName(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "server-properties-with-a-long-name.properties")
	if err := Write(entry, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := PathKey(entry)
	checkShort := func() {
		t.Helper()
		p, err := windows.UTF16PtrFromString(longPath(entry))
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]uint16, 32768)
		n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
		if err != nil {
			t.Fatal(err)
		}
		if int(n) >= len(buf) {
			t.Fatal("短路径长度异常")
		}
		short := windows.UTF16ToString(buf[:n])
		if strings.EqualFold(short, entry) {
			t.Log("这个卷未提供不同的8.3短名，普通路径控制照常执行")
		}
		if PathKey(short) != before {
			t.Fatalf("短名取得不同锁：%q", short)
		}
	}
	checkShort()
	release := LockPath(entry)
	if err := Write(entry, []byte("new"), 0o644); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if PathKey(entry) != before {
		t.Fatal("普通原子替换改变了锁键")
	}
	checkShort()
}
