//go:build windows

package modcart

import (
	"errors"
	"syscall"
)

// fileNotFound 只认「文件夹在、里面没有这个文件或文件夹」（ERROR_FILE_NOT_FOUND）。路径里某一级不在（ERROR_PATH_NOT_FOUND）、
// 网络路径找不到（ERROR_BAD_NETPATH）这些，os 也归进 fs.ErrNotExist，但那是路径暂时失联，不是新装（同 tunnel 包的 fileNotFound）。
func fileNotFound(err error) bool { return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) }
