package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

// 替换时目标文件只要被谁打开着（哪怕只是查一下文件名、什么权限都不要、允许别人删），
// MoveFileEx 就报拒绝访问；临时文件被打开着、又不许别人删，报共享冲突。
// 这种占用通常一眨眼就放开：本程序自己排队前算锁键（PathKey 会打开目标查真实名字）、
// 不排队的读取、服务端读配置、杀毒和索引都会这样。所以目标不是只读、错误是这两种时，隔一会儿再试，
// 间隔从 1 毫秒翻倍到 125 毫秒封顶，一共最多再试 10 次、加起来约半秒；还不行就照旧报错。
// 重试只再调 MoveFileEx：它本身拒绝覆盖只读文件，所以重试期间目标变成只读也不会被盖掉。
// 2026-11-14 CI 上 TestUpdatePropsKeepsEveryonesChange 偶发过一次 Access is denied；本机在重负载下复现出的触发原因，
// 是排队的保存在进队前算锁键、撞上了正在替换的那一个（单看 CI 那一次，不能排除同时还有别的程序占着）。
var retryDelays = []time.Duration{
	1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond, 16 * time.Millisecond,
	32 * time.Millisecond, 64 * time.Millisecond, 125 * time.Millisecond, 125 * time.Millisecond, 125 * time.Millisecond,
}

// beforeRetry 测试用：第 attempt 次重试（从 1 数）调用 MoveFileEx 之前调用。
var beforeRetry func(attempt int)

// retryable 失败是不是值得再试一次：错误是拒绝访问或共享冲突，而且目标不是只读文件、不是文件夹。
// 原文件原来就是只读的（old 记着）不在这里重试，交给下面的扩展改名，规矩和以前一样。
func retryable(err error, path string, old fs.FileInfo) bool {
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return false
	}
	if old != nil && old.Mode().Perm()&0o200 == 0 {
		return false
	}
	cur, e := os.Lstat(path)
	if errors.Is(e, fs.ErrNotExist) {
		return true // 目标不在，占着的是临时文件
	}
	if e != nil || cur.IsDir() || cur.Mode().Perm()&0o200 == 0 {
		return false
	}
	// 末级是目录联接或目录符号链接时 Lstat 不报 ModeDir，要看属性：改名换不掉文件夹，不用白等
	if a, ok := cur.Sys().(*syscall.Win32FileAttributeData); ok && a.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return false
	}
	return true
}

func replace(tmp, path string, old fs.FileInfo) error {
	err := move(tmp, path)
	retries := 0
	for err != nil && retries < len(retryDelays) && retryable(err, path, old) {
		time.Sleep(retryDelays[retries])
		retries++
		if beforeRetry != nil {
			beforeRetry(retries)
		}
		err = move(tmp, path)
	}
	if err == nil {
		return nil
	}
	if retries > 0 {
		err = fmt.Errorf("重试 %d 次仍不行：%w", retries, err)
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
//
// 不跟随时（算锁键的最后一项）不打开那个目录项本身：上一级照旧用句柄问系统，最后一项的长名到上一级的目录列表里查。
// 以前连最后一项也用句柄打开一次。排队等锁的人都要先算锁键，正好碰上持锁的人在改名替换这个文件时，
// 改名就被拒（Access is denied）：旧式改名替换不了还有句柄开着的文件，哪怕那个句柄只为查路径、允许删除。
// 40 个保存同时排队的测试在 CI 上就是这样偶发失败的（2026-11-15）。
func finalPath(path string, follow bool) (string, bool) {
	if follow {
		return handlePath(path, true)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return handlePath(path, false) // 卷根不会被改名替换，照旧打开
	}
	name := filepath.Base(path)
	// 目录查询把这几个字符当通配符，会匹配到别的文件；正常文件名里不会有它们
	if strings.ContainsAny(name, `*?<>"`) {
		return "", false
	}
	dir, ok := handlePath(parent, true)
	if !ok {
		return "", false
	}
	u, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return "", false
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(u, &fd)
	if err != nil {
		return "", false
	}
	windows.FindClose(h)
	long := windows.UTF16ToString(fd.FileName[:])
	if !strings.EqualFold(long, name) && !strings.EqualFold(windows.UTF16ToString(fd.AlternateFileName[:]), name) {
		return "", false
	}
	return filepath.Join(dir, long), true
}

// handlePath 打开 path 问系统它的最终路径（设备路径形式）。follow 为 false 时不跟随最后一项的链接。
// 打开的句柄会挡住别人改名替换这个文件，所以不拿它查要被原子替换的那个文件（见 finalPath）。
func handlePath(path string, follow bool) (string, bool) {
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
