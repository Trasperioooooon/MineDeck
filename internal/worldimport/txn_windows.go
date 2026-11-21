package worldimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// moveFile 用 MoveFileEx 改名，带 WRITE_THROUGH：返回时改名已经落盘。不带 COPY_ALLOWED：跨盘就失败，
// 不会退化成「复制一半再删」。replace 只给记录文件用；目录一律不覆盖已有的目标。
func moveFile(from, to string, replace bool) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(f, t, flags); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

// longPath 超过 260 个字符的绝对路径加上 \\?\ 前缀（MoveFileEx 不像 os 包那样自动加）。
func longPath(p string) string {
	if len(p) < 248 || strings.HasPrefix(p, `\\?\`) || !filepath.IsAbs(p) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

// 改名、写盘失败的几类原因（见 explainMove、diskErr）。

// busyErr 有程序开着里面的文件。
func busyErr(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// crossDeviceErr 不在同一个盘上。
func crossDeviceErr(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }

// diskFullErr 磁盘满了。
func diskFullErr(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}

// diskFullErrno 同 diskFullErr，给已经取出来的错误码用。
func diskFullErrno(errno syscall.Errno) bool {
	return errno == syscall.Errno(windows.ERROR_DISK_FULL) || errno == syscall.Errno(windows.ERROR_HANDLE_DISK_FULL)
}
