package inst

import (
	"testing"

	"golang.org/x/sys/unix"
)

// makeUnreplaceable 让这个文件换不掉，模拟用户把它保护了起来（对应 unreplaceable_windows_test.go）。
// Mac 上改名替换不看文件自己的权限位（chmod 0444 挡不住，看的是所在文件夹能不能写），挡得住的是「不可更改」标记
// （chflags uchg，Finder 简介里勾「已锁定」就是它）。只加标记、不动权限位，文件照样读得出来。
// 测试结束时去掉标记，临时目录才删得掉。
func makeUnreplaceable(t *testing.T, path string) {
	t.Helper()
	if err := unix.Chflags(path, unix.UF_IMMUTABLE); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Chflags(path, 0) })
}
