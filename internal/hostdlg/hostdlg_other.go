//go:build !windows

package hostdlg

import "errors"

var errNotWindows = errors.New("只有 Windows 上能弹系统对话框")

func pickFolderNative(uintptr, string) (string, error) { return "", errNotWindows }
func pickFolderFallback(string) (string, error)        { return "", errNotWindows }
