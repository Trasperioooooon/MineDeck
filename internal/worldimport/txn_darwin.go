package worldimport

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// moveFile 改名（Mac 版，对应 txn_windows.go）。replace 为假时用 renamex_np 的 RENAME_EXCL：目标已经在就失败，
// 不会像 rename(2) 那样悄悄盖掉文件或空文件夹。rename 本来就不跨卷（跨卷报 EXDEV），不会退化成「复制一半再删」。
// Windows 上带 WRITE_THROUGH，这里改完把两边的上级目录刷一次盘（刷盘失败不算改名失败：改名已经做了）。
// replace 为真（只有记录文件）就是普通的 rename(2)，本来就要原样盖掉旧的。
func moveFile(from, to string, replace bool) error {
	var err error
	if replace {
		err = unix.Rename(from, to)
	} else {
		err = unix.RenamexNp(from, to, unix.RENAME_EXCL)
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) {
			// 有的文件系统（exFAT 的 U 盘、网络盘）不认 RENAME_EXCL：先看目标在不在，再普通改名。
			// 用 present 不用 exists：看不清目标在不在（没权限、盘出错）就停下，不冒险去改名盖掉它（Gemini 4 Argon 复审 PR #32）
			if there, perr := present(to); perr != nil {
				err = perr
			} else if there {
				err = unix.EEXIST
			} else {
				err = unix.Rename(from, to)
			}
		}
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	syncDir(filepath.Dir(to))
	if d := filepath.Dir(from); d != filepath.Dir(to) {
		syncDir(d)
	}
	return nil
}

func syncDir(dir string) {
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync() // Go 在 Mac 上的 Sync 用 F_FULLFSYNC
		f.Close()
	}
}

// 改名、写盘失败的几类原因（见 explainMove、diskErr）。

// busyErr 有程序占着。Mac 上开着的文件不挡改名，能碰上的只有挂载点这类 EBUSY。
func busyErr(err error) bool { return errors.Is(err, unix.EBUSY) }

// crossDeviceErr 不在同一个卷上。
func crossDeviceErr(err error) bool { return errors.Is(err, unix.EXDEV) }

// diskFullErr 磁盘满了（或者超了配额）。
func diskFullErr(err error) bool { return errors.Is(err, unix.ENOSPC) || errors.Is(err, unix.EDQUOT) }

// diskFullErrno 同 diskFullErr，给已经取出来的错误码用。
func diskFullErrno(errno syscall.Errno) bool {
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT
}
