package inst

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Mac 版的真实路径（对应 realpath_windows.go，函数的用法和约定一样，这里只写和 Windows 不同的地方）。
// 按打开以后的文件问系统它在哪（fcntl 的 F_GETPATH）：符号链接全部解开，/var 这类系统链接解成 /private/var，
// 每一级的大小写按磁盘上的来（Mac 默认的文件系统不分大小写，Foo 和 foo 是同一个文件夹）。

// realPath 一个文件夹的真实路径；不存在的那几级原样接在还在的上层后面，一级都打不开就原样返回。
func realPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, ok := finalPath(cur); ok {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// ErrRealPath 查不清一个路径的真实落点（见 RealPathStrict）。
var ErrRealPath = errors.New("查不清这个路径实际指到哪里")

// RealPathStrict 和 realPath 一样，但查不清时报 ErrRealPath：只有这一级确实不存在才往上找，
// 条目在、却打不开的（断掉的符号链接、没有权限）直接报错。
func RealPathStrict(p string) (string, error) {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, ok := finalPath(cur); ok {
			return filepath.Join(r, rest), nil
		}
		if _, err := os.Lstat(cur); !errors.Is(err, fs.ErrNotExist) {
			return "", ErrRealPath
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ErrRealPath
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// finalPath 问系统一个已有的文件或文件夹的真实路径；打不开返回 false。
// O_EVTONLY：只为查路径，不要读权限，也不挡着卷被推出；O_NONBLOCK：碰上命名管道不卡住。
func finalPath(p string) (string, bool) {
	var fd int
	err := ignoringEINTR(func() (err error) {
		fd, err = unix.Open(p, unix.O_EVTONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		return err
	})
	if err != nil {
		return "", false
	}
	defer unix.Close(fd)
	return fdPath(uintptr(fd))
}

// ignoringEINTR 系统调用被信号打断（EINTR）就重做，直到不是 EINTR 为止，和 os 包内部的做法一样：网络盘（SMB、NFS）上 open 会被
// Go 运行时的抢占信号打断，不重做的话在的文件会被当成打不开，RealPathStrict 就把它报成查不清（Gemini 4 Argon 复审 PR #32）。
func ignoringEINTR(fn func() error) error {
	for {
		if err := fn(); !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// pathBufLen F_GETPATH 的缓冲区大小。写成变量：make 的长度不是常量，缓冲区一定分配在堆上，
// 下面把地址当整数交给系统调用时不会因为栈扩张被搬走。
var pathBufLen = unix.PathMax

// fdPath 问系统一个打开着的文件的路径。
func fdPath(fd uintptr) (string, bool) {
	buf := make([]byte, pathBufLen)
	err := ignoringEINTR(func() error {
		_, err := unix.FcntlInt(fd, unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0]))))
		return err
	})
	runtime.KeepAlive(buf)
	if err != nil {
		return "", false
	}
	n := bytes.IndexByte(buf, 0)
	if n <= 0 {
		return "", false
	}
	return filepath.Clean(string(buf[:n])), true
}

// OpenFileInside 打开 p 来读，并按打开以后的文件核对它实际落在 dir 里面（见 realpath_windows.go）。
func OpenFileInside(p, dir string) (*os.File, error) {
	root, err := RealPathStrict(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	real, ok := fdPath(f.Fd())
	if !ok {
		f.Close()
		return nil, ErrRealPath
	}
	if !InsideReal(root, real) {
		f.Close()
		return nil, ErrOutside
	}
	return f, nil
}

// InsideReal real 是不是 root 本身或者在它里面。两个都必须是 RealPathStrict 或者按打开的文件问出来的路径，逐字比较。
// 没给根（空串）时和 Windows 版一样什么都不算在里面；先判断再去掉末尾的斜杠，不然空串会被当成根目录 /。
func InsideReal(root, real string) bool {
	if root == "" {
		return false
	}
	root = strings.TrimRight(root, "/")
	if root == "" { // 根目录 / 本身：什么都在它里面
		return strings.HasPrefix(real, "/")
	}
	return real == root || strings.HasPrefix(real, root+"/")
}

// ErrOutside 文件实际落在要求的文件夹外面（见 OpenFileInside）。
var ErrOutside = errors.New("这个文件实际在服务器文件夹外面")
