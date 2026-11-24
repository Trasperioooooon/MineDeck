//go:build windows

package atomicfile

import (
	"os"

	"golang.org/x/sys/windows"
)

// HardlinkCount 返回已打开文件句柄底层文件对象的硬链接数（>= 1）。
func HardlinkCount(f *os.File) (uint32, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, err
	}
	if info.NumberOfLinks == 0 {
		return 1, nil
	}
	return info.NumberOfLinks, nil
}
