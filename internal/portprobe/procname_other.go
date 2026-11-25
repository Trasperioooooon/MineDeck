//go:build !windows && !darwin

package portprobe

// ProcessName 非 Windows/macOS 平台默认不解析进程名。
func ProcessName(int) string { return "" }
