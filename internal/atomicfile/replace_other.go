//go:build !windows

package atomicfile

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

func replace(tmp, path string, _ fs.FileInfo) error { return os.Rename(tmp, path) }

func replaceCtx(_ context.Context, tmp, path string, old fs.FileInfo) error {
	return replace(tmp, path, old)
}

// publishNew 非 Windows 下用 O_EXCL 空文件占住不存在的目标名再改名换上：POSIX 上改名覆盖打开着的文件不会失败，
// 不用 os.Link 是因为 exFAT 等移动盘不支持硬链接（Mac 用户的服务器可能放在外置盘上）。
func publishNew(tmp, path string) error {
	rf, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_ = rf.Close()
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// RetryRemove 非 Windows 下删除文件或空文件夹没有共享冲突，直接调一次。
func RetryRemove(op func() error) error { return op() }

// openForSync 替换之前把写好的临时文件重新打开，为的是刷盘（Windows 上被占着时会再试，见 replace_windows.go）。
func openForSync(tmp string) (*os.File, error) { return os.OpenFile(tmp, os.O_RDWR, 0) }

func openForSyncCtx(_ context.Context, tmp string) (*os.File, error) { return openForSync(tmp) }
func canonicalPath(path string) string {
	return canonicalEntryPath(path, func(cur string, follow bool) (string, bool) {
		if !follow {
			parent, err := filepath.EvalSymlinks(filepath.Dir(cur))
			return filepath.Join(parent, filepath.Base(cur)), err == nil
		}
		resolved, err := filepath.EvalSymlinks(cur)
		return resolved, err == nil
	})
}
