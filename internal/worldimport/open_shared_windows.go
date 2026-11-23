//go:build windows

package worldimport

import (
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	openLocalRead = openWindowsSharedRead
}

// openWindowsSharedRead 带 FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE 打开只读文件：
// 服务端保存 level.dat 时会先把旧文件改名再换上新文件，普通 os.Open 不带 FILE_SHARE_DELETE，撞上那几毫秒会让服务端存盘报错。
func openWindowsSharedRead(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}
