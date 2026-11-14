package worldimport

import (
	"os"
	"os/exec"
	"path/filepath"
)

// mkJunction 建一个目录联接（不需要管理员权限）。
func mkJunction(link, target string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}
