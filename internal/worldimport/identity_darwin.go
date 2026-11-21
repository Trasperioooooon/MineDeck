package worldimport

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// dirIdentity 已经确认是普通文件夹的 path 的身份：设备号、inode 号和创建时间（见 directoryIdentity）。
// inode 号会在删掉以后被别的文件夹接着用，带上创建时间，和 Windows 上带 CreationTime 是一个用意。
func dirIdentity(path string) (string, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return "", err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return "", fmt.Errorf("世界目录的类型已改变")
	}
	if st.Ino == 0 {
		return "", fmt.Errorf("磁盘没有提供可靠的目录编号")
	}
	return fmt.Sprintf("%08x:%016x:%x.%09d", uint32(st.Dev), st.Ino, st.Btim.Sec, st.Btim.Nsec), nil
}
