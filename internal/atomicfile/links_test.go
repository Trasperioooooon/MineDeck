package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardlinkCountAndAtomicReplaceBreaksLink(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "outer.txt")
	if err := os.WriteFile(outer, []byte("original-outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	f0, err := os.Open(outer)
	if err != nil {
		t.Fatal(err)
	}
	n0, err := HardlinkCount(f0)
	_ = f0.Close()
	if err != nil || n0 != 1 {
		t.Fatalf("单独文件硬链接数应为 1，got n=%d err=%v", n0, err)
	}

	inner := filepath.Join(t.TempDir(), "inner.txt")
	if err := os.Link(outer, inner); err != nil {
		t.Skipf("当前环境不允许建硬链接：%v", err)
	}
	f1, err := os.Open(inner)
	if err != nil {
		t.Fatal(err)
	}
	n1, err := HardlinkCount(f1)
	_ = f1.Close()
	if err != nil || n1 != 2 {
		t.Fatalf("建硬链接后链接数应为 2，got n=%d err=%v", n1, err)
	}

	// 经 atomicfile.Write 改名覆盖后，会断开硬链接：外面的文件不变，里面变成独立文件（链接数回到 1）。
	if err := Write(inner, []byte("updated-inside"), 0o644); err != nil {
		t.Fatalf("atomicfile.Write 应放行并替换硬链接目标：%v", err)
	}
	gotOut, err := os.ReadFile(outer)
	if err != nil || string(gotOut) != "original-outside" {
		t.Fatalf("外部文件不应被原子替换改动：%q %v", gotOut, err)
	}
	gotIn, err := os.ReadFile(inner)
	if err != nil || string(gotIn) != "updated-inside" {
		t.Fatalf("内部替换后内容不对：%q %v", gotIn, err)
	}
	f2, err := os.Open(inner)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := HardlinkCount(f2)
	_ = f2.Close()
	if err != nil || n2 != 1 {
		t.Fatalf("替换后服务器里的副本应断开硬链接（链接数=1），got n=%d err=%v", n2, err)
	}
}
