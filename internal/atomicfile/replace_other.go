//go:build !windows

package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

func replace(tmp, path string, _ fs.FileInfo) error { return os.Rename(tmp, path) }
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
