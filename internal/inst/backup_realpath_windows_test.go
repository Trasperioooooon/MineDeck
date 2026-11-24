//go:build windows

package inst

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/diskguard"
)

func TestBackupRestoreRefusesJunctionEscape(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(base, "server")
	worldDir := filepath.Join(dest, "world")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 在世界目录里放一个指到服务器外面的目录联接 world/region -> outside
	junc := filepath.Join(worldDir, "region")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junc, outside).CombinedOutput(); err != nil {
		t.Fatalf("建目录联接失败（%v）：%s", err, string(out))
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("world/region/escape.txt")
	_, _ = fw.Write([]byte("escaped"))
	_ = zw.Close()

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkBackupPaths(zr.File, dest); err == nil || (!strings.Contains(err.Error(), "链接") && !strings.Contains(err.Error(), "非法路径")) {
		t.Fatalf("checkBackupPaths 应拦住通过目录联接逃出实例目录的路径：%v", err)
	}
	lease, err := diskguard.Acquire(context.Background(), dest, diskguard.Options{AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := unzipReaderTo(zr, dest, lease); err == nil {
		t.Fatalf("unzipReaderTo 也不应写出到目录联接指向的外部目录")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("绝不能在外部目录里建出 escape.txt：%v", err)
	}
}
