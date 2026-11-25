package linkmode

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeInfo 按给定的 FileMode 造一个 FileInfo：云同步文件夹这类组合在测试里造不出真的。
// IsDir 照 Mode 算，和 os.Lstat 的结果一致（目录联接、符号链接不带 ModeDir 时 IsDir 为假）。
type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "x" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// 按 Windows 上 os.Lstat、DirEntry.Type() 报的 FileMode 组合（Go 1.23 起），直接测四个函数：
// 云同步的真文件夹算普通文件夹，符号链接、目录联接不算。
func TestFileModeClassification(t *testing.T) {
	t.Cleanup(func() { lstat = os.Lstat })
	cases := []struct {
		name         string
		mode         fs.FileMode
		wantLink     bool
		wantPlainDir bool
	}{
		{"普通文件夹", fs.ModeDir | 0o755, false, true},
		{"云同步真实文件夹（ModeDir|ModeIrregular）", fs.ModeDir | fs.ModeIrregular | 0o755, false, true},
		{"目录联接（仅 ModeIrregular）", fs.ModeIrregular, true, false},
		{"符号链接（ModeSymlink）", fs.ModeSymlink, true, false},
		{"带目录位的符号链接（ModeDir|ModeSymlink）", fs.ModeDir | fs.ModeSymlink, true, false},
		{"普通文件", 0o644, false, false},
	}
	for _, tc := range cases {
		info := fakeInfo{tc.mode}
		if got := IsLink(tc.mode); got != tc.wantLink {
			t.Errorf("%s: IsLink = %v, want %v (mode=%v)", tc.name, got, tc.wantLink, tc.mode)
		}
		if got := IsPlainDirInfo(info); got != tc.wantPlainDir {
			t.Errorf("%s: IsPlainDirInfo = %v, want %v", tc.name, got, tc.wantPlainDir)
		}
		if got := IsPlainDirEntry(fs.FileInfoToDirEntry(info)); got != tc.wantPlainDir {
			t.Errorf("%s: IsPlainDirEntry = %v, want %v", tc.name, got, tc.wantPlainDir)
		}
		lstat = func(string) (fs.FileInfo, error) { return info, nil }
		if got := IsPlainDir("x"); got != tc.wantPlainDir {
			t.Errorf("%s: IsPlainDir = %v, want %v", tc.name, got, tc.wantPlainDir)
		}
	}

	// 没有东西、读不了的都不算
	if IsPlainDirInfo(nil) || IsPlainDirEntry(nil) {
		t.Error("nil 不该算普通文件夹")
	}
	lstat = func(string) (fs.FileInfo, error) { return nil, errors.New("读不了") }
	if IsPlainDir("x") {
		t.Error("Lstat 出错时不该算普通文件夹")
	}
}

// IsPlainDir 默认用的是真的 os.Lstat：真文件夹算，普通文件和不存在的不算。
func TestIsPlainDirOnDisk(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsPlainDir(dir) {
		t.Errorf("真文件夹应算普通文件夹：%s", dir)
	}
	if IsPlainDir(file) {
		t.Error("普通文件不该算普通文件夹")
	}
	if IsPlainDir(filepath.Join(dir, "missing")) {
		t.Error("不存在的不该算普通文件夹")
	}
}
