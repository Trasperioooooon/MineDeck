package modpack

import (
	"archive/zip"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawEntry 造 zip 条目用：data 原样写进去（不压缩），declared 是中央目录里写的解开后大小，mode 不为 0 时写进去。
type rawEntry struct {
	name     string
	data     string
	declared uint64
	mode     fs.FileMode
}

// zipRaw 按 rawEntry 原样造 zip：名字里的反斜杠、对不上的大小、符号链接都照写，用来模拟别的工具或恶意的包。
func zipRaw(t *testing.T, ents ...rawEntry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "raw.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range ents {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte(e.data)),
			CompressedSize64: uint64(len(e.data)), UncompressedSize64: e.declared}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

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

// 写的大小和实际的对不上（实际比写的大，或者比写的小）：报错，不会悄悄写出截断的文件。
// 靠的是 zip 库每读一次都查有没有超过写的大小、读到结尾再查够不够；LimitReader 只是兜底。
func TestExtractOverridesSizeMismatch(t *testing.T) {
	for name, e := range map[string]rawEntry{
		"实际比写的大": {name: "overrides/a.txt", data: strings.Repeat("x", 100), declared: 10},
		"实际比写的小": {name: "overrides/a.txt", data: strings.Repeat("x", 10), declared: 100},
	} {
		t.Run(name, func(t *testing.T) {
			p := &Pack{ZipPath: zipRaw(t, e), OverrideDirs: []string{"overrides"}}
			var wrote int64
			err := ExtractOverrides(p, t.TempDir(), func(rel string, r io.Reader) error {
				n, err := io.Copy(io.Discard, r)
				wrote += n
				return err
			})
			if err == nil {
				t.Fatalf("大小对不上应报错，实际写了 %d 字节", wrote)
			}
			if wrote > int64(e.declared) {
				t.Fatalf("写出的比写的大小还多：%d > %d", wrote, e.declared)
			}
		})
	}
}

// 跳过哪些条目和解现成服务端时一样：反斜杠结尾的目录（Windows 工具打的包会这样写）、符号链接。
// 以前反斜杠结尾的目录被当成文件：「overrides\」解出来是空路径，整包报错；「overrides\config\」写成一个叫 config 的文件，
// 后面 config 里的文件就建不了目录。符号链接以前会写成一个内容是链接目标的普通文件。
func TestExtractOverridesSkipsDirsAndLinks(t *testing.T) {
	p := &Pack{ZipPath: zipRaw(t,
		rawEntry{name: `overrides\`},
		rawEntry{name: `overrides\config\`},
		rawEntry{name: `overrides\config\a.toml`, data: "a = 1", declared: 5},
		rawEntry{name: "overrides/link", data: "../../outside", declared: 13, mode: fs.ModeSymlink | 0o777},
	), OverrideDirs: []string{"overrides"}}
	if got := overridesOf(t, p); len(got) != 1 || got["config/a.toml"] != "a = 1" {
		t.Fatalf("只该写出 config/a.toml：%v", got)
	}
}
