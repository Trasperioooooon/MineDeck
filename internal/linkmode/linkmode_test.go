package linkmode

import (
	"io/fs"
	"testing"
)

func TestFileModeClassification(t *testing.T) {
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
		if got := IsLink(tc.mode); got != tc.wantLink {
			t.Errorf("%s: IsLink = %v, want %v (mode=%v)", tc.name, got, tc.wantLink, tc.mode)
		}
		isDir := tc.mode&fs.ModeDir != 0
		gotPlain := isDir && !IsLink(tc.mode)
		if gotPlain != tc.wantPlainDir {
			t.Errorf("%s: plainDir = %v, want %v", tc.name, gotPlain, tc.wantPlainDir)
		}
	}
}
