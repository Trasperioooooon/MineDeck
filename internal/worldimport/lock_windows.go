//go:build windows

package worldimport

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// sessionLockHeld 世界目录正在不在被一个 Minecraft 进程用：游戏（1.16 起）打开世界时会锁住 session.lock 的内容，
// 这里试着锁一下，锁不上就是有人在用。只看文件在不在是不准的：正常退出以后 session.lock 也留着。
// 更老的版本不锁这个文件，靠后面的整目录改名兜底（有文件被打开时 Windows 不让改名）。
func sessionLockHeld(worldDir string) (bool, error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(worldDir, "session.lock"))
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return true, nil
		}
		return false, err
	}
	defer windows.CloseHandle(h)
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return true, nil
		}
		return false, err
	}
	_ = windows.UnlockFileEx(h, 0, 1, 0, ol)
	return false, nil
}
