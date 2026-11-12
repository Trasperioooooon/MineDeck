package inst

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// realPath 一个文件夹的真实路径：解开目录联接（junction）、符号链接、subst 出来的盘符、8.3 短名，大小写按磁盘上的来。
// 只用来比较两个路径是不是同一个文件夹、谁在谁里面，不拿来当实例的目录用（映射的网络盘解开后是 UNC 路径，
// 而 run.bat 里的 cd /d 不认 UNC）。
//
// 路径不存在时，解开还在的那一段上层，后面不存在的几级原样接上：不这样的话同一个位置会有两种写法
// （上层一个写短名、一个写长名），比不出谁在谁里面——列表里的服务器目录被挪走、删掉之后就是这种情况。
// 一级都打不开（盘不在、没权限）就原样返回。
//
// 不用 filepath.EvalSymlinks：Go 1.23 起它在 Windows 上不再解开目录联接。
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

// RealPathStrict 和 realPath 一样解开目录联接、符号链接这些，但查不清时报 ErrRealPath，不原样返回：
// 给「查不清就不让做」的检查用，比如写配置之前看落点还在不在服务器文件夹里（v3.1.0 正式版前检查 B-9）。
//
// 返回的是系统内部的设备路径（\Device\HarddiskVolume3\…），只拿来比较两个路径谁在谁里面，不拿来打开文件。
// 不用盘符形式：没在系统的挂载管理里登记的盘（ImDisk 这类内存盘、一些虚拟盘）查盘符形式会失败，
// 查不清就拒绝的话，放在这种盘上的服务器就一个配置都读写不了；设备路径哪种盘都查得到，比较的两边用同一种就行。
//
// p 还不存在时（要新建的文件），解开还在的那一级上层，后面不存在的几级原样接上。只有这一级确实不存在
// （Lstat 说没有）才往上找；条目在、却打不开的（断掉的目录联接、没有权限），直接报错，不当作不存在放过去。
func RealPathStrict(p string) (string, error) {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, ok := finalPathAs(longPath(cur), volumeNameNT); ok {
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

// longPath 很长的绝对路径加上 \\?\ 前缀，CreateFile 才打得开（os 包自己也是这么做的；
// 不加的话路径一过 MAX_PATH，RealPathStrict 就会把在的文件当成查不清）。
func longPath(p string) string {
	if len(p) < 248 || strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	if filepath.IsAbs(p) {
		return `\\?\` + p
	}
	return p
}

// GetFinalPathNameByHandle 的两种结果：盘符形式（C:\…，0）和系统内部的设备路径（\Device\…）。
const (
	volumeNameDOS = 0x0
	volumeNameNT  = 0x2
)

// finalPath 问系统一个已有的文件或文件夹的最终路径（盘符形式）；打不开返回 false。
func finalPath(p string) (string, bool) { return finalPathAs(p, volumeNameDOS) }

// finalPathAs 同 finalPath，volume 选结果的形式（volumeNameDOS 或 volumeNameNT）。
func finalPathAs(p string, volume uint32) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	// FILE_FLAG_BACKUP_SEMANTICS：打开的是目录时必须带；访问权限给 0，只为查路径
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volume) // FILE_NAME_NORMALIZED 是 0，不用另加
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]uint16, n+1)
	}
	s := windows.UTF16ToString(buf)
	if rest, ok := strings.CutPrefix(s, `\\?\UNC\`); ok {
		s = `\\` + rest
	} else {
		s = strings.TrimPrefix(s, `\\?\`)
	}
	return filepath.Clean(s), true
}
