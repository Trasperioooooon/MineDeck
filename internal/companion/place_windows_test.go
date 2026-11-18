package companion

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

func lockFileExclusive(t *testing.T, path string) func() {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	unlock := func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

func TestPlaceJarCtxErrorsAndSuccess(t *testing.T) {
	t.Run("目标jar被独占时提示停服后再试且保留底层系统错误", func(t *testing.T) {
		mods := t.TempDir()
		target := filepath.Join(mods, "companion.jar")
		if err := os.WriteFile(target, []byte("old-jar"), 0o644); err != nil {
			t.Fatal(err)
		}
		unlock := lockFileExclusive(t, target)
		defer unlock()

		err := placeJarCtx(context.Background(), mods, "companion.jar", []byte("new-jar"), nil)
		if err == nil {
			t.Fatal("目标被独占时应报错")
		}
		if !strings.Contains(err.Error(), "停服后再试") {
			t.Fatalf("报错应提示停服后再试：%v", err)
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatalf("应可用 errors.Is 认出原来的系统占用错误：%v", err)
		}
	})

	t.Run("内容检查回调报错时原样返回不加停服提示", func(t *testing.T) {
		mods := t.TempDir()
		want := errors.New("下载的依赖版本不对，没有装")
		err := placeJarCtx(context.Background(), mods, "dep.jar", []byte("jar-bytes"), func(string) error {
			return want
		})
		if !errors.Is(err, want) || err.Error() != want.Error() {
			t.Fatalf("check 报错应原样返回：%v", err)
		}
		if strings.Contains(err.Error(), "停服后再试") {
			t.Fatalf("内容检查没过不应带停服提示：%v", err)
		}
	})

	t.Run("取消上下文时不加停服提示", func(t *testing.T) {
		mods := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := placeJarCtx(ctx, mods, "dep.jar", []byte("jar-bytes"), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("应返回取消错误：%v", err)
		}
		if strings.Contains(err.Error(), "停服后再试") {
			t.Fatalf("取消错误不应带停服提示：%v", err)
		}
	})

	t.Run("写入正常时照常成功", func(t *testing.T) {
		mods := t.TempDir()
		want := []byte("valid-companion-jar")
		if err := placeJarCtx(context.Background(), mods, "companion.jar", want, nil); err != nil {
			t.Fatalf("正常写入失败：%v", err)
		}
		got, err := os.ReadFile(filepath.Join(mods, "companion.jar"))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("写入内容不对：%v %q", err, got)
		}
	})
}
