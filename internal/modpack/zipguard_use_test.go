package modpack

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/zipguard"
)

// hugeDeclaredZip 一个 8 MiB 的空洞文件，包尾（ZIP64）申报着三百万个条目，清单其实是空的。
func hugeDeclaredZip(t *testing.T) string {
	t.Helper()
	const size = 8 << 20
	path := filepath.Join(t.TempDir(), "huge.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, 98)
	binary.LittleEndian.PutUint32(tail, 0x06064b50) // ZIP64 结束记录
	binary.LittleEndian.PutUint64(tail[4:], 44)
	binary.LittleEndian.PutUint64(tail[24:], 3_000_000)
	binary.LittleEndian.PutUint64(tail[32:], 3_000_000)
	binary.LittleEndian.PutUint32(tail[56:], 0x07064b50) // ZIP64 定位符
	binary.LittleEndian.PutUint64(tail[64:], size-98)
	binary.LittleEndian.PutUint32(tail[72:], 1)
	copy(tail[76:], []byte{'P', 'K', 0x05, 0x06}) // 结束记录
	binary.LittleEndian.PutUint16(tail[84:], 0xffff)
	binary.LittleEndian.PutUint16(tail[86:], 0xffff)
	binary.LittleEndian.PutUint32(tail[88:], 0xffffffff)
	binary.LittleEndian.PutUint32(tail[92:], 0xffffffff)
	if _, err := f.WriteAt(tail, size-98); err != nil {
		t.Fatal(err)
	}
	return path
}

// 整合包也走带预检的打开方式：包尾申报的条目数大得离谱时，在标准库动手之前就拒绝。
func TestModpackUsesGuardedZipOpen(t *testing.T) {
	if _, err := Parse(hugeDeclaredZip(t)); !errors.Is(err, zipguard.ErrTooLarge) {
		t.Errorf("解析整合包应当按文件太多拒绝：%v", err)
	}
}

func TestNestedServerJarUsesGuard(t *testing.T) {
	nested := paperJar(t)
	outer := jarOf(t, map[string]string{"server.jar": nested})
	zr, err := zip.NewReader(strings.NewReader(outer), int64(len(outer)))
	if err != nil {
		t.Fatal(err)
	}
	plain, read := candFromJar(zr.File[0], "server.jar")
	if !read || plain.core == "" {
		t.Fatalf("正常内嵌jar未识别：%+v", plain)
	}
	old := zipguard.Default
	t.Cleanup(func() { zipguard.Default = old })
	zipguard.Default.MaxEntries = 1
	got, read := candFromJar(zr.File[0], "server.jar")
	if !read || got.core != "" {
		t.Fatalf("超过内嵌ZIP预检上限仍被识别：%+v", got)
	}
}
