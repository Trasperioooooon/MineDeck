package java

import (
	"archive/zip"
	"errors"
	"minedeck/internal/zipguard"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeZipUsesPreflight(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "runtime.zip")
	dest := filepath.Join(dir, "unpacked")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"runtime/bin/runtime.exe", "runtime/readme.txt"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte("fixture")); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	old := zipguard.Default
	t.Cleanup(func() { zipguard.Default = old })
	zipguard.Default.MaxEntries = 1
	err = extractZip(p, dest)
	if !errors.Is(err, zipguard.ErrTooLarge) {
		t.Fatalf("没走ZIP预检：%v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("拒绝前已创建解压目录")
	}
	zipguard.Default = old
	err = extractZip(p, dest)
	if err != nil {
		t.Fatalf("正常ZIP无法解压：%v", err)
	}
}
