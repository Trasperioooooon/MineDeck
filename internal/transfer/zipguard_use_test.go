package transfer

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
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

// 交接包也走带预检的打开方式：包尾申报的条目数大得离谱时，在标准库动手之前就拒绝
// （2026-11-14 Codex 第二轮复审：main 发版前交接包、整合包也要接上这层保护）。
func TestHandoverPackUsesGuardedZipOpen(t *testing.T) {
	path := hugeDeclaredZip(t)
	if _, err := ReadManifest(path); !errors.Is(err, zipguard.ErrTooLarge) {
		t.Errorf("读交接包的摘要应当按文件太多拒绝：%v", err)
	}
	if packPort(path) != 0 {
		t.Error("读不了的包不该读出端口")
	}
	if packEULAAccepted(path) {
		t.Error("读不了的包不该读出 EULA")
	}
}

func TestPortAndEULAAreGuardedBeforeParsing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "valid.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range map[string]string{"instance/server.properties": "server-port=25571\n", "instance/eula.txt": "eula=true\n"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if packPort(p) != 25571 || !packEULAAccepted(p) {
		t.Fatal("正常对照没有读出端口和 EULA")
	}
	old := zipguard.Default
	t.Cleanup(func() { zipguard.Default = old })
	zipguard.Default.MaxEntries = 1
	if packPort(p) != 0 || packEULAAccepted(p) {
		t.Fatal("超过入口预检限制后仍解析了内容")
	}
}
