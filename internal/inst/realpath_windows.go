package inst

import (
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

// finalPath 问系统一个已有的文件或文件夹的最终路径；打不开返回 false。
func finalPath(p string) (string, bool) {
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
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0) // 0 = 盘符形式、规范化的名字
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
