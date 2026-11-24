//go:build !windows

package atomicfile

import (
	"os"
	"syscall"
)

// HardlinkCount 返回已打开文件句柄底层文件对象的硬链接数（>= 1）。
func HardlinkCount(f *os.File) (uint32, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && sys.Nlink > 0 {
		return uint32(sys.Nlink), nil
	}
	return 1, nil
}
