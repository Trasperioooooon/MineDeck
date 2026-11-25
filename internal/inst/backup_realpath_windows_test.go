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

// 还原时只查目标上面的文件夹是不是链接，不查目标本身（A12）：OneDrive 这类云同步的文件在 Go 里也只报 ModeIrregular，
// 和目录联接分不开，查目标本身会让现有的世界文件挡住整次还原。云同步文件在测试里造不出来，这里用一个占在目标位置上、
// 指回服务器文件夹里面的目录联接代替（Lstat 一样只报 ModeIrregular）：真实落点在服务器文件夹里，应当放行。
func TestBackupRestoreDoesNotLinkCheckTargetItself(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "server")
	inner := filepath.Join(dest, "world", "real-region")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	junc := filepath.Join(dest, "world", "region")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junc, inner).CombinedOutput(); err != nil {
		t.Fatalf("建目录联接失败（%v）：%s", err, string(out))
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("world/region/"); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkBackupPaths(zr.File, dest); err != nil {
		t.Fatalf("目标本身占着、真实落点在服务器文件夹里的，不该挡住还原：%v", err)
	}
	lease, err := diskguard.Acquire(context.Background(), dest, diskguard.Options{AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := unzipReaderTo(zr, dest, lease); err != nil {
		t.Fatalf("解压也不该被挡住：%v", err)
	}
}

// 目标本身是服务器文件夹里一个指到外面的目录联接：上面那段只查上一级，这种由 RealPathStrict + InsideReal 拦住，
// 报错要说清是服务器文件夹里的哪一个，不说成备份的毛病。
func TestBackupRestoreRefusesTargetLinkedOutside(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(base, "server")
	if err := os.MkdirAll(filepath.Join(dest, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	junc := filepath.Join(dest, "world", "region")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junc, outside).CombinedOutput(); err != nil {
		t.Fatalf("建目录联接失败（%v）：%s", err, string(out))
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create("world/region")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("escaped"))
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	err = checkBackupPaths(zr.File, dest)
	if err == nil || !strings.Contains(err.Error(), "「world/region」指到了服务器文件夹外面") {
		t.Fatalf("目标本身指到外面的应被拦住，并说清是哪一个：%v", err)
	}
	ents, _ := os.ReadDir(outside)
	if len(ents) != 0 {
		t.Fatalf("外面的文件夹里不该多出东西：%d 项", len(ents))
	}
}
