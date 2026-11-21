//go:build !windows

package catalog

// symlinkUnprivileged 类 Unix 上建符号链接不要特别的权限，失败都要报出来（见 files_realpath_windows_test.go）。
func symlinkUnprivileged(error) bool { return false }
