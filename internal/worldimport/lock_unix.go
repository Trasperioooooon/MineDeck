//go:build !windows

package worldimport

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// sessionLockHeld 世界目录正在不在被一个 Minecraft 进程用（见 lock_windows.go）。Java 在类 Unix 上用 fcntl 的记录锁
// 锁 session.lock，这里用 F_GETLK 问有没有别的进程锁着它，自己不加锁。
// 打开时带 O_NONBLOCK 并用 Fstat 检查普通文件，防止 session.lock 被换成命名管道时阻塞；打开阶段遇到 EINTR 重试。
// 注意：Mac 上文件开着也能改名，Windows 上「有文件被打开时不让改名」那道兜底在这里不存在，不锁这个文件的老版本（1.16 以前）认不出来。
func sessionLockHeld(worldDir string) (bool, error) {
	p := filepath.Join(worldDir, "session.lock")
	var fd int
	var err error
	for {
		fd, err = unix.Open(p, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ENOENT) {
			return false, nil
		}
		return false, &os.PathError{Op: "open", Path: p, Err: err}
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	for {
		err = unix.Fstat(fd, &st)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return false, &os.PathError{Op: "stat", Path: p, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return false, fmt.Errorf("session.lock 不是普通文件")
	}
	lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: io.SeekStart} // Start、Len 为 0：整个文件
	if err := unix.FcntlFlock(uintptr(fd), unix.F_GETLK, &lk); err != nil {
		return false, err
	}
	return lk.Type != unix.F_UNLCK, nil
}
