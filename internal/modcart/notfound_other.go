//go:build !windows

package modcart

import (
	"errors"
	"io/fs"
)

// fileNotFound 不在。别的系统上「路径某一级不在」和「文件不在」是同一个错误（ENOENT），
// 靠 reallyMissing 从程序文件夹往下逐级看来分（见 notfound_windows.go）。
func fileNotFound(err error) bool { return errors.Is(err, fs.ErrNotExist) }
