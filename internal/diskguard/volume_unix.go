//go:build !windows

package diskguard

import (
	"math"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Free 路径所在的卷还剩多少字节（当前用户能用的，statfs 的 f_bavail）。
func Free(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	bsize := uint64(st.Bsize)
	if bsize != 0 && uint64(st.Bavail) > math.MaxInt64/bsize {
		return math.MaxInt64, nil
	}
	return int64(uint64(st.Bavail) * bsize), nil
}

// volumeKey 这个已经存在的文件夹在哪个卷上：stat 的设备号（st_dev）。和 Windows 的卷序列号一样，
// 撞号只会让不相干的卷一起排队，不会让同一个卷上的两个写入绕过名额。
func volumeKey(dir string) (uint32, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, &os.PathError{Op: "stat", Path: dir, Err: syscall.ENOTSUP}
	}
	return uint32(st.Dev), nil
}
