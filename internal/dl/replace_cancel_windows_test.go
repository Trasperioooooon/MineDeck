package dl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"minedeck/internal/atomicfile"
)

// A19.2：下载完成后的落盘替换若因目标被占用而进入重试等待（约半秒/次），取消 ctx 后应立即中断等待返回，不再干等整轮。
func TestFetchCancelStopsReplaceWait(t *testing.T) {
	payload := strings.Repeat("d", 2048)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "mod.jar")
	if err := os.WriteFile(dest, []byte("in-use"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		t.Fatal(err)
	}
	// 用独占句柄一直占着目标文件，迫使落盘替换进入约半秒的重试等待
	h, err := windows.CreateFile(u, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { _ = windows.CloseHandle(h) }) }
	t.Cleanup(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inReplace := make(chan struct{})
	var hookOnce sync.Once
	oldHook := atomicfile.Hook
	t.Cleanup(func() { atomicfile.Hook = oldHook })
	atomicfile.Hook = func(p string) {
		if p == dest {
			hookOnce.Do(func() { close(inReplace) })
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- Fetch(ctx, Request{
			URLs:    []string{srv.URL + "/1", srv.URL + "/2", srv.URL + "/3"},
			Dest:    dest,
			MinSize: 1024,
		}, nil)
	}()

	select {
	case <-inReplace:
	case <-time.After(5 * time.Second):
		t.Fatal("没有走到落盘替换")
	}
	// 等落盘进入重试睡眠（后面还剩约 480ms 以上的睡眠）后取消 ctx
	time.Sleep(25 * time.Millisecond)
	cancelAt := time.Now()
	cancel()

	err = <-done
	elapsed := time.Since(cancelAt)
	release()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消 ctx 后应返回 context.Canceled，实际：%v", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("取消 ctx 后落盘等待过了 %v 才返回，没有响应取消", elapsed)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("第一次落盘等待被取消后不应继续请求后续地址，实际请求了 %d 次", got)
	}
	if b, _ := os.ReadFile(dest); string(b) != "in-use" {
		t.Fatalf("取消后原目标文件被改动：%q", b)
	}
}
