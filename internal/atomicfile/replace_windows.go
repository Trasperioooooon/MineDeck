package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func longPath(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if len(p) < 248 {
		return p
	}
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

func move(tmp, path string) error {
	from, err := windows.UTF16PtrFromString(longPath(tmp))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows 10/11 的扩展改名可忽略被替换文件的只读属性，不需要先修改任何目标属性。
// 若文件系统不支持，保存直接失败；绝不退回到按路径清除只读、先删原文件的做法。
type renameInfo struct {
	Flags     uint32
	Root      windows.Handle
	NameBytes uint32
	Name      [1]uint16
}

func replace(tmp, path string, old fs.FileInfo) error {
	err := move(tmp, path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || old == nil || !old.Mode().IsRegular() || old.Mode().Perm()&0o200 != 0 {
		return err
	}
	current, e := os.Lstat(path)
	if e != nil || !os.SameFile(old, current) {
		return fmt.Errorf("只读目标已改变，没有覆盖：%w", err)
	}
	from, e := windows.UTF16PtrFromString(longPath(tmp))
	if e != nil {
		return e
	}
	h, e := windows.CreateFile(from, windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(h)
	name, e := windows.UTF16FromString(longPath(path))
	if e != nil {
		return e
	}
	name = name[:len(name)-1]
	var header renameInfo
	offset := int(unsafe.Offsetof(header.Name))
	buf := make([]byte, offset+len(name)*2)
	info := (*renameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS | windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
	info.NameBytes = uint32(len(name) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset])), len(name)), name)
	return windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buf[0], uint32(len(buf)))
}

func canonicalPath(path string) string {
	return strings.ToUpper(canonicalEntryPath(path, finalPath))
}

// finalPath 的最后一项由 follow 决定是否跟随。锁住的是原子改名要替换的目录项，
// 所以最终文件不能跟随符号链接；其父目录里的联接仍需解析。
func finalPath(path string, follow bool) (string, bool) {
	u, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return "", false
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !follow {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 2)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			return filepath.Clean(windows.UTF16ToString(buf[:n])), true
		}
		buf = make([]uint16, n+1)
	}
}
