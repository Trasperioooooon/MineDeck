package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNew(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "fresh.txt")
	if err := WriteNew(dest, []byte("hello"), 0o644); err != nil {
		t.Fatalf("目标不存在时 WriteNew 应成功：%v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "hello" {
		t.Fatalf("写入内容不对：%q err=%v", got, err)
	}

	err := WriteNew(dest, []byte("overwrite"), 0o644)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("目标已存在时 WriteNew 应返回 fs.ErrExist，实际：%v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "hello" {
		t.Fatalf("目标已存在时原内容不应被改写：%q err=%v", got, err)
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".minedeck-write-") {
			t.Fatalf("WriteNew 返回后不应残留临时文件：%s", e.Name())
		}
	}
}
