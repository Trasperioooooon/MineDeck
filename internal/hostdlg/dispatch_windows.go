//go:build windows

package hostdlg

import "golang.org/x/sys/windows"

func currentThreadID() uint32 { return windows.GetCurrentThreadId() }
