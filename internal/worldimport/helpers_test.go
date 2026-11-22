package worldimport

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mkJunction 建一个目录联接（不需要管理员权限）；只在非 Windows 或明确缺权限/文件系统不支持时跳过，其余失败直接判错。
func mkJunction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "privilege") || strings.Contains(low, "not support") || strings.Contains(low, "权限") || strings.Contains(low, "不支持") {
			t.Skipf("建不了目录联接：%v %s", err, out)
		}
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}
