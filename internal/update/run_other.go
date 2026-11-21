//go:build !windows

package update

// startSwap 换程序：只有 Windows 版有（见 run_windows.go），别的平台在 Apply 开头就拦下了。
func startSwap(string, string, string, int) error { return errNoSelfUpdate }
