package java

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/diskguard"
)

// 解压 Java 要排队拿这个盘的写入名额。同一个盘上有别的大批量写入占着名额时：任务日志里说一声在等什么，
// 取消任务就停下、原来的文件夹不动；以前等的时候取消不了，也看不出在等。名额放出来以后照常解压。
func TestExtractWaitsCancellablyForDiskSlot(t *testing.T) {
	zipPath := runtimeZip(t)
	dest := filepath.Join(t.TempDir(), "java-21")
	touch(t, filepath.Join(dest, "bin", "old.dll")) // 原来装了一半的：没轮到解压之前不该动它
	extractDiskFree = func(string) (int64, error) { return 10 << 30, nil }
	t.Cleanup(func() { extractDiskFree = nil })

	// 别的操作（一次备份）占着这个盘的名额
	busy, err := diskguard.Acquire(context.Background(), dest, diskguard.Options{Query: extractDiskFree})
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { busy.Close() }) })

	var mu sync.Mutex
	var logs []string
	waiting := make(chan struct{}, 1)
	logf := func(format string, a ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, a...))
		mu.Unlock()
		select {
		case waiting <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- extractZipCtx(ctx, zipPath, dest, logf) }()

	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("名额被占着，不该直接返回：%v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("排队的时候应在日志里说一声")
	}
	mu.Lock()
	said := strings.Join(logs, "\n")
	mu.Unlock()
	if !strings.Contains(said, "等它做完再解压 Java") || !strings.Contains(said, "取消") {
		t.Fatalf("日志要说清在等什么、可以取消：%s", said)
	}
	select {
	case err := <-done:
		t.Fatalf("还在排队，不该返回：%v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消以后应返回取消：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("排队的时候取消不了")
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "old.dll")); err != nil {
		t.Fatalf("没轮到解压就取消了，原来的文件夹不该动：%v", err)
	}

	// 名额放出来以后照常解压，不再说排队
	release.Do(func() { busy.Close() })
	mu.Lock()
	logs = nil
	mu.Unlock()
	if err := extractZipCtx(context.Background(), zipPath, dest, logf); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 0 {
		t.Errorf("不用排队时不该说：%q", logs)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "java.exe")); err != nil {
		t.Errorf("解完以后应当有 bin/java.exe：%v", err)
	}
}

// 解压到一半取消：停下，已经解出来的不留。
func TestExtractCancelledMidwayLeavesNothing(t *testing.T) {
	zipPath := runtimeZip(t)
	dest := filepath.Join(t.TempDir(), "java-21")
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	extractDiskFree = func(string) (int64, error) {
		if calls++; calls == 4 { // 同 TestExtractZip…：第一个文件（bin/java.exe）解出来以后
			cancel()
		}
		return 10 << 30, nil
	}
	t.Cleanup(func() { extractDiskFree = nil })
	if err := extractZipCtx(ctx, zipPath, dest, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消以后应返回取消：%v", err)
	}
	if calls < 4 {
		t.Fatalf("测试自己没摆对：只查了 %d 次余量", calls)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("取消以后不该留下半截运行时：%v", err)
	}
}
