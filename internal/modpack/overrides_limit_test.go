package modpack

import (
	"archive/zip"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipDeclaring 造一个只有一个文件的 zip：实际 1 个字节，中央目录里写的大小是 declared。
// 用来模拟「压缩后很小、申报解开后很大」的整合包，不用真的生成几个 GB 的数据。
func zipDeclaring(t *testing.T, name string, declared uint64) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "declared.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store,
		CRC32: crc32.ChecksumIEEE([]byte("x")), CompressedSize64: 1, UncompressedSize64: declared})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

// 覆盖目录解开后的大小和个数有上限：超了一个文件都不写（v3.1.0 正式版前检查 B-4）。
func TestExtractOverridesLimits(t *testing.T) {
	write := func(p *Pack) (int, error) {
		n := 0
		err := ExtractOverrides(p, t.TempDir(), func(rel string, r io.Reader) error {
			n++
			_, err := io.Copy(io.Discard, r)
			return err
		})
		return n, err
	}

	// 申报 9 GB：超过 8 GB 的上限
	p := &Pack{ZipPath: zipDeclaring(t, "overrides/config/big.bin", 9<<30), OverrideDirs: []string{"overrides"}}
	if n, err := write(p); err == nil || !strings.Contains(err.Error(), "超过了一次能导入的上限") || n != 0 {
		t.Fatalf("申报 9 GB 应整包拒绝、一个不写：写了 %d 个，%v", n, err)
	}

	// 个数超过上限（把上限临时调小）
	old := overrideMaxFiles
	overrideMaxFiles = 3
	defer func() { overrideMaxFiles = old }()
	p = &Pack{ZipPath: mkZip(t, map[string]string{
		"overrides/a.txt": "1", "overrides/b.txt": "2", "server-overrides/c.txt": "3", "server-overrides/d.txt": "4",
	}), OverrideDirs: []string{"overrides", "server-overrides"}}
	if n, err := write(p); err == nil || !strings.Contains(err.Error(), "4 个") || n != 0 {
		t.Fatalf("两个覆盖目录合起来 4 个、上限 3 个，应拒绝且一个不写：写了 %d 个，%v", n, err)
	}
	overrideMaxFiles = old

	// 没超的照常写，server-overrides 在 overrides 后面写，同名的以它为准
	p = &Pack{ZipPath: mkZip(t, map[string]string{
		"overrides/config/a.txt": "旧", "server-overrides/config/a.txt": "新", "overrides/b.txt": "b",
	}), OverrideDirs: []string{"overrides", "server-overrides"}}
	got := overridesOf(t, p)
	if got["config/a.txt"] != "新" || got["b.txt"] != "b" || len(got) != 2 {
		t.Fatalf("正常的覆盖目录没写对：%v", got)
	}
}
