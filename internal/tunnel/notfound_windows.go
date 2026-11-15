//go:build windows

package tunnel

import (
	"errors"
	"syscall"
)

// fileNotFound 只认「文件夹在、里面没有这个文件」（ERROR_FILE_NOT_FOUND）。
// 路径里某一级不在（ERROR_PATH_NOT_FOUND）、网络路径找不到（ERROR_BAD_NETPATH）这些，os 也归进 fs.ErrNotExist，
// 但那是路径暂时失联，不是新装：当成新装的话路径一恢复，下一次保存就把原文件盖掉（2026-11-15 Codex 两轮复审的 P1）。
func fileNotFound(err error) bool { return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) }
