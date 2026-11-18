package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 替换之前要把写好的临时文件重新打开一次来刷盘。这时临时文件被别的程序打开着、又不许别人写（杀毒软件、同步盘
// 扫刚写完的文件时会这样）：以前这一下当场失败，现在和改名那一步一样，隔一会儿再试。

// setReopen 换上测试用的重试间隔，记下重新打开重试了几次；测试结束还原。
func setReopen(t *testing.T, delays []time.Duration, onRetry func(attempt int)) *int {
	t.Helper()
	oldDelays, oldHook := retryDelays, beforeReopen
	t.Cleanup(func() { retryDelays, beforeReopen = oldDelays, oldHook })
	n := new(int)
	retryDelays = delays
	beforeReopen = func(attempt int) {
		*n = attempt
		if onRetry != nil {
			onRetry(attempt)
		}
	}
	return n
}

func tempAndTarget(t *testing.T) (tmp, target string) {
	t.Helper()
	dir := t.TempDir()
	target = filepath.Join(dir, "server.properties")
	tmp = filepath.Join(dir, ".minedeck-download-1.part")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	return tmp, target
}

// 临时文件被占着一小会儿再放开：等到放开就照常换上。
func TestReplaceWaitsForHeldTemp(t *testing.T) {
	for _, hd := range []struct {
		name  string
		share uint32
	}{
		{"读着、不许别人写也不许删", windows.FILE_SHARE_READ},
		{"独占", 0},
	} {
		t.Run(hd.name, func(t *testing.T) {
			tmp, target := tempAndTarget(t)
			o := hold(t, tmp, windows.GENERIC_READ, hd.share, 0)
			n := setReopen(t, ms(10), func(attempt int) {
				if attempt == 3 {
					o.release()
				}
			})
			if err := Replace(tmp, target); err != nil {
				t.Fatalf("放开以后应当换得上：%v", err)
			}
			if *n != 3 {
				t.Errorf("应当在第 3 次重试时打得开，实际重试了 %d 次", *n)
			}
			if got, _ := os.ReadFile(target); string(got) != "new" {
				t.Errorf("内容：%q", got)
			}
			if _, err := os.Stat(tmp); !os.IsNotExist(err) {
				t.Errorf("换上以后临时文件不该还在：%v", err)
			}
		})
	}
}

// 一直占着：试完就照旧报共享冲突，原文件不动，临时文件留给调用方清理。
func TestReplaceGivesUpOnHeldTemp(t *testing.T) {
	tmp, target := tempAndTarget(t)
	o := hold(t, tmp, windows.GENERIC_READ, windows.FILE_SHARE_READ, 0)
	n := setReopen(t, ms(4), nil)
	err := Replace(tmp, target)
	o.release()
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("一直占着应报共享冲突：%v", err)
	}
	if *n != 4 {
		t.Errorf("应当把重试用完（4 次），实际 %d 次", *n)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Errorf("没换成时原文件不该动：%q", got)
	}
	if got, _ := os.ReadFile(tmp); string(got) != "new" {
		t.Errorf("临时文件应原样留着：%q", got)
	}
}

// 没人占着：不等、不重试。临时文件不在这类别的错误也不重试。
func TestReplaceReopenNoRetryWhenFree(t *testing.T) {
	tmp, target := tempAndTarget(t)
	n := setReopen(t, ms(10), nil)
	if err := Replace(tmp, target); err != nil || *n != 0 {
		t.Fatalf("没人占着不该重试：%v（%d 次）", err, *n)
	}
	if err := Replace(tmp, target); !errors.Is(err, os.ErrNotExist) || *n != 0 {
		t.Fatalf("临时文件不在应直接报错、不重试：%v（%d 次）", err, *n)
	}
}
