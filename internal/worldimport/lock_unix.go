//go:build !windows

package worldimport

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// sessionLockHeld 世界目录正在不在被一个 Minecraft 进程用（见 lock_windows.go）。Java 在类 Unix 上用 fcntl 的记录锁
// 锁 session.lock，这里用 F_GETLK 问有没有别的进程锁着它，自己不加锁。
// 注意：Mac 上文件开着也能改名，Windows 上「有文件被打开时不让改名」那道兜底在这里不存在，不锁这个文件的老版本（1.16 以前）认不出来。
func sessionLockHeld(worldDir string) (bool, error) {
	f, err := os.Open(filepath.Join(worldDir, "session.lock"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: io.SeekStart} // Start、Len 为 0：整个文件
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk); err != nil {
		return false, err
	}
	return lk.Type != unix.F_UNLCK, nil
}
