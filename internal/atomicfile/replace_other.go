//go:build !windows

package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

func replace(tmp, path string, _ fs.FileInfo) error { return os.Rename(tmp, path) }

// openForSync 替换之前把写好的临时文件重新打开，为的是刷盘（Windows 上被占着时会再试，见 replace_windows.go）。
func openForSync(tmp string) (*os.File, error) { return os.OpenFile(tmp, os.O_RDWR, 0) }
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
