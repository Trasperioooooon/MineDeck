package atomicfile

import (
	"context"
	"path/filepath"
	"sync"
)

var pathLocks = struct {
	sync.Mutex
	m map[string]*pathLock
}{m: make(map[string]*pathLock)}

type pathLock struct {
	ready chan struct{}
	refs  int
}

// PathKey 返回 LockPath 使用的规范化键，仅用于比较和登记，不是可打开的路径。
// 父目录会解开联接和别名，最终文件只规范名称、不跟随文件符号链接：
// 原子替换会把那个链接目录项本身换成普通文件，锁键不能因此变化。
// 无法查询时退回绝对清理路径。
func PathKey(path string) string { return canonicalPath(path) }

// LockPath 将同一规范路径的操作排队；不可重入，要覆盖完整的读、改、写。
func LockPath(path string) func() {
	release, _ := LockPathContext(context.Background(), path)
	return release
}

// LockPathContext 等待别的写入者时可以取消。
func LockPathContext(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return lockKeyContext(ctx, canonicalPath(path))
}

func lockKeyContext(ctx context.Context, key string) (func(), error) {
	pathLocks.Lock()
	l := pathLocks.m[key]
	if l == nil {
		l = &pathLock{ready: make(chan struct{}, 1)}
		l.ready <- struct{}{}
		pathLocks.m[key] = l
	}
	l.refs++
	pathLocks.Unlock()
	drop := func() {
		pathLocks.Lock()
		l.refs--
		if l.refs == 0 {
			delete(pathLocks.m, key)
		}
		pathLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-l.ready:
	}
	var once sync.Once
	return func() { once.Do(func() { l.ready <- struct{}{}; drop() }) }, nil
}

// canonicalEntryPath 只在解析上层目录时跟随最后一项；不存在的后缀保留原名。
// resolve 返回供比较用的真实名称（Windows 会同时规范 8.3 短名）。
func canonicalEntryPath(path string, resolve func(string, bool) (string, bool)) string {
	p, err := filepath.Abs(path)
	if err != nil {
		p = filepath.Clean(path)
	}
	rest := ""
	for cur := p; ; {
		if real, ok := resolve(cur, cur != p); ok {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
