// Package linkmode 区分真实文件夹与链接、重解析点。
package linkmode

import (
	"io/fs"
	"os"
)

// IsLink 判断条目的 FileMode 是否为符号链接或目录联接一类的重解析点。
//
// Go 1.23 起，Windows 上云同步按需真实文件夹（例如 OneDrive）同时带
// ModeDir|ModeIrregular，而目录联接只带 ModeIrregular、不带 ModeDir（见 Go 标准库
// os/types_windows.go）。因此带 ModeIrregular 且同时带 ModeDir 的真实文件夹不能当成链接。
func IsLink(m fs.FileMode) bool {
	return m&fs.ModeSymlink != 0 || (m&fs.ModeIrregular != 0 && m&fs.ModeDir == 0)
}

// IsPlainDirInfo 判断 FileInfo 是否是可进入使用的真实文件夹（含云同步真实文件夹，排除符号链接与目录联接）。
func IsPlainDirInfo(st fs.FileInfo) bool {
	return st != nil && st.IsDir() && !IsLink(st.Mode())
}

// lstat 测试里换成返回假 FileInfo 的函数：云同步文件夹（ModeDir|ModeIrregular）在测试里造不出来。
var lstat = os.Lstat

// IsPlainDir 判断路径 p 是否存在且为真实文件夹（不跟随符号链接或目录联接）。
func IsPlainDir(p string) bool {
	st, err := lstat(p)
	return err == nil && IsPlainDirInfo(st)
}

// IsPlainDirEntry 判断目录遍历项 d 是否为真实子文件夹（不跟随符号链接或目录联接）。
func IsPlainDirEntry(d fs.DirEntry) bool {
	return d != nil && d.IsDir() && !IsLink(d.Type())
}
