package main

import (
	"bytes"
	"os"
	"testing"
)

// 图标由 assets 包内嵌、这里取用（go:embed 够不到上级目录）。这条测试守住这条线没断：
// 取到的内容要和仓库里的 assets/icon.ico 一字节不差，而且确实是 ICO 文件。
func TestIconDataMatchesAssetFile(t *testing.T) {
	want, err := os.ReadFile("../../assets/icon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if len(icoData) == 0 {
		t.Fatal("icoData 是空的：assets 包没有把 icon.ico 内嵌进来")
	}
	if !bytes.Equal(icoData, want) {
		t.Errorf("icoData（%d 字节）和 assets/icon.ico（%d 字节）不一样", len(icoData), len(want))
	}
	if !bytes.HasPrefix(icoData, []byte{0, 0, 1, 0}) {
		t.Errorf("icoData 开头不是 ICO 文件头：% x", icoData[:min(4, len(icoData))])
	}
}
