package java

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl/dltest"
)

// 这个包的测试不连外网（见 dltest.NoInternet）：Ensure 的三个下载来源一律马上失败。
func TestMain(m *testing.M) {
	dltest.NoInternet()
	os.Exit(m.Run())
}

// runtimesIn 把运行时目录换到临时文件夹里。
func runtimesIn(t *testing.T) {
	t.Helper()
	old := app.RuntimesDir
	app.RuntimesDir = filepath.Join(t.TempDir(), "runtimes")
	t.Cleanup(func() { app.RuntimesDir = old })
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runtimeZip 仿照真的 JRE 压缩包的顺序：bin/java.exe 排在前面，大头 lib/modules 排在后面。
func runtimeZip(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "jre.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"jdk-21+9-jre/bin/java.exe", "jdk-21+9-jre/bin/server/jvm.dll", "jdk-21+9-jre/lib/modules", "jdk-21+9-jre/release"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(strings.Repeat("j", 4096))); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// 解压到一半磁盘余量不够：已经解出来的不留。
// 2026-11-14 的 c1007f1 给解压加了余量检查，每个文件之前查一次，不够就中途返回，解出来的半截原地留着；
// bin\java.exe 在压缩包里排得很靠前，于是「java.exe 在、lib\modules 不在」，下次被当成装好了。
func TestExtractFailureLeavesNoHalfRuntime(t *testing.T) {
	zipPath := runtimeZip(t)
	dest := filepath.Join(t.TempDir(), "java-21")
	// 旧的半截：解压前会被整个删掉
	touch(t, filepath.Join(dest, "bin", "old.dll"))
	calls := 0
	extractDiskFree = func(string) (int64, error) {
		calls++
		// 查余量的次序：开头一次；第一个文件（bin/java.exe）开始之前一次、开始写的时候一次；第二个文件开始之前……
		if calls <= 3 { // 前三次：够，java.exe 解出来了
			return 10 << 30, nil
		}
		return 1 << 20, nil // 第二个文件开始之前：别的程序把盘写满了
	}
	t.Cleanup(func() { extractDiskFree = nil })
	err := extractZip(zipPath, dest)
	var space *diskguard.NoSpace
	if !errors.As(err, &space) {
		t.Fatalf("余量中途不够应当报空间不够：%v", err)
	}
	if calls < 4 {
		t.Fatalf("测试自己没摆对：应当是解出 java.exe 以后才不够的，只查了 %d 次", calls)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		var left []string
		_ = filepath.WalkDir(dest, func(p string, d os.DirEntry, _ error) error {
			if d != nil && !d.IsDir() {
				left = append(left, strings.TrimPrefix(p, dest))
			}
			return nil
		})
		t.Fatalf("解压失败以后不该留下半截运行时：%v", left)
	}

	// 一开头就不够放下整包：直接拒绝，原来的文件夹不动
	touch(t, filepath.Join(dest, "bin", "old.dll"))
	extractDiskFree = func(string) (int64, error) { return diskguard.Reserve + 8192, nil } // 保留量以外只多 8 KiB，包解开是 16 KiB
	if err := extractZip(zipPath, dest); !errors.As(err, &space) {
		t.Fatalf("放不下整包应当一开头就拒绝：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "old.dll")); err != nil {
		t.Errorf("一开头就拒绝的，原来的文件夹不该动：%v", err)
	}

	// 余量算上马上要删掉的旧文件夹：重装一个装了一半、已经占着 12 KiB 的运行时，只要再多 4 KiB，
	// 保留量以外剩 8 KiB 够了，不该拒绝
	if err := os.WriteFile(filepath.Join(dest, "bin", "old.dll"), make([]byte, 12<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("旧文件夹删掉以后放得下，不该拒绝：%v", err)
	}

	// 余量够：照常解完
	extractDiskFree = func(string) (int64, error) { return 10 << 30, nil }
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin/java.exe", "bin/server/jvm.dll", "lib/modules", "release"} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(name))); err != nil {
			t.Errorf("解完以后应当有 %s：%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "old.dll")); !os.IsNotExist(err) {
		t.Error("解压前旧文件夹应当整个删掉")
	}
}

// 「还没装完」的记号还在：bin\java.exe 在也不算装好，Ensure 不把它交出去，而是重装；整套装完才拿掉记号。
// 程序在逐文件下载、解压中途被结束，或者失败以后半截删不干净，留下的就是这种状态。
func TestHalfInstalledRuntimeIsNotUsed(t *testing.T) {
	runtimesIn(t)
	const major = 21
	// 这道记号出现之前装好的运行时没有记号：照旧算装好
	touch(t, Exe(major))
	if !IsInstalled(major) || len(Installed()) != 1 {
		t.Fatalf("没有记号的应当算装好：%v %v", IsInstalled(major), Installed())
	}
	// 装到一半留下的：java.exe 在，记号也在
	touch(t, incompleteMark(major))
	if IsInstalled(major) || len(Installed()) != 0 {
		t.Fatalf("记号还在的不算装好：%v %v", IsInstalled(major), Installed())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 三个下载来源都马上失败
	exe, err := Ensure(ctx, major, func(string, ...any) {}, nil)
	if err == nil || !strings.Contains(err.Error(), "全部下载源均失败") {
		t.Fatalf("半截的运行时不该交出去用，应当去重装（这里装不成）：%q %v", exe, err)
	}
	if IsInstalled(major) {
		t.Error("重装没成，仍然不算装好")
	}
	// 整套装完：拿掉记号，算装好
	if exe, err := finalize(major); err != nil || exe != Exe(major) {
		t.Fatalf("finalize：%q %v", exe, err)
	}
	if _, err := os.Lstat(incompleteMark(major)); !os.IsNotExist(err) {
		t.Errorf("装完以后记号应当拿掉：%v", err)
	}
	if !IsInstalled(major) {
		t.Error("装完以后应当算装好")
	}

	// 从没装过的：Ensure 动手之前先放记号，装不成记号留着
	const fresh = 17
	if _, err := Ensure(ctx, fresh, func(string, ...any) {}, nil); err == nil {
		t.Fatal("没有网应当装不成")
	}
	if _, err := os.Lstat(incompleteMark(fresh)); err != nil {
		t.Errorf("动手装之前应当先放记号：%v", err)
	}
	// 这时逐文件下载留下了 java.exe（下到一半断了）：不算装好
	touch(t, Exe(fresh))
	if IsInstalled(fresh) {
		t.Error("下到一半留下的 java.exe 不算装好")
	}
}
