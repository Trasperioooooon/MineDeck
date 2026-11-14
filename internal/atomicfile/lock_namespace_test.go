package atomicfile

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// exerciseNamespaceWaiters 固定 A、B、C 的顺序：B 已按旧状态取键并进入队列，
// A 替换目录项，B 接手后停住，此时 C 必须仍在同一个队列里。
func exerciseNamespaceWaiters(t *testing.T, key func() string, replaceEntry func()) bool {
	t.Helper()
	aKey := key()
	releaseA, err := lockKeyContext(context.Background(), aKey)
	if err != nil {
		t.Fatal(err)
	}
	held, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	stopB := func() { once.Do(func() { close(finish) }) }
	defer func() {
		releaseA()
		stopB()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("B没有结束")
		}
	}()
	go func() {
		defer close(done)
		release, err := lockKeyContext(context.Background(), key())
		if err != nil {
			t.Error(err)
			return
		}
		defer release()
		close(held)
		<-finish
	}()
	queued := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pathLocks.Lock()
		l := pathLocks.m[aKey]
		queued = l != nil && l.refs == 2
		pathLocks.Unlock()
		if queued {
			break
		}
		runtime.Gosched()
	}
	if !queued {
		t.Fatal("B没有按替换前的键排队")
	}
	replaceEntry()
	releaseA()
	select {
	case <-held:
	case <-time.After(3 * time.Second):
		t.Fatal("B没有取得锁")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	releaseC, err := lockKeyContext(ctx, key())
	cancel()
	overlap := err == nil
	if releaseC != nil {
		releaseC()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	stopB()
	<-done
	return overlap
}

func TestNamespaceReplacementKeepsQueuedWritersTogether(t *testing.T) {
	for _, oldPolicy := range []bool{true, false} {
		name := "最终目录项不跟随"
		if oldPolicy {
			name = "旧跟随策略对照"
		}
		t.Run(name, func(t *testing.T) {
			entry := filepath.Join(t.TempDir(), "server.properties")
			referent := filepath.Join(filepath.Dir(entry), "shared.properties")
			var isLink atomic.Bool
			isLink.Store(true)
			// 这是文件系统接口模型：真实 Windows file symlink 用例另列，不能冒充实盘复现。
			key := func() string {
				return canonicalEntryPath(entry, func(cur string, follow bool) (string, bool) {
					if oldPolicy {
						follow = true
					}
					if cur == entry && isLink.Load() && follow {
						return referent, true
					}
					return cur, true
				})
			}
			overlap := exerciseNamespaceWaiters(t, key, func() { isLink.Store(false) })
			if overlap != oldPolicy {
				t.Fatalf("overlap=%v oldPolicy=%v", overlap, oldPolicy)
			}
		})
	}
}
