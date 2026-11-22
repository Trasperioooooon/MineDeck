package catalog

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// symlinkUnprivileged 建符号链接失败是不是因为账户没有这个权限（没开开发者模式、也不是管理员）：只有这一种算「这台机器测不了」，
// 用例跳过；别的失败（路径不对、文件夹没建、同名已经有了）是用例自己的问题，要报出来，不能也记成跳过（Codex 复审 beta.3 第五轮）。
func symlinkUnprivileged(err error) bool { return errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) }

// 跳过的条件只认「本机没权限」：路径不对这类失败要报出来，CI 环境变量非空时哪怕报没权限也必须直接失败不跳过
func TestSymlinkSkipOnlyWithoutPrivilege(t *testing.T) {
	wrap := func(e error) error { return &os.LinkError{Op: "symlink", Old: "a", New: "b", Err: e} }
	t.Setenv("CI", "")
	if !canSkipSymlink(wrap(windows.ERROR_PRIVILEGE_NOT_HELD)) {
		t.Error("本机没权限时该跳过")
	}
	for _, e := range []error{windows.ERROR_PATH_NOT_FOUND, windows.ERROR_ALREADY_EXISTS, windows.ERROR_ACCESS_DENIED, errors.New("别的错")} {
		if canSkipSymlink(wrap(e)) {
			t.Errorf("%v 不该当成没权限跳过", e)
		}
	}
	t.Setenv("CI", "true")
	if canSkipSymlink(wrap(windows.ERROR_PRIVILEGE_NOT_HELD)) {
		t.Error("CI 上建不出符号链接应直接失败，不能静默跳过")
	}
}
