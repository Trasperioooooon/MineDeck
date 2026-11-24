package inst

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"minedeck/internal/atomicfile"
	"minedeck/internal/linkmode"
)

// ErrHardlinkEscape 目标文件有多个硬链接（可能连向服务器文件夹外的文件），拒绝原地写入以免改穿到外面。
var ErrHardlinkEscape = errors.New("这个文件和服务器文件夹外的文件是同一份（硬链接），为了不改到外面，没有写入")

// openInPlaceNoHardlink 打开 path 准备从头覆盖写入；如果 path 是符号链接/特殊项，或打开的句柄硬链接数 > 1，
// 在 Truncate 之前直接关闭并拒绝，绝不改动硬链接另一端的文件。
func openInPlaceNoHardlink(path string, perm fs.FileMode) (*os.File, error) {
	if st, err := os.Lstat(path); err == nil {
		if linkmode.IsLink(st.Mode()) || !st.Mode().IsRegular() {
			return nil, fmt.Errorf("目标不是普通文件（可能是链接或目录）: %s", filepath.Base(path))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// 先不带 O_TRUNC 打开，查完硬链接数 <= 1 后再截断，避免检测前先把外面的文件清空。
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}
	n, err := atomicfile.HardlinkCount(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if n > 1 {
		_ = f.Close()
		return nil, fmt.Errorf("%w：%s", ErrHardlinkEscape, filepath.Base(path))
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// writeFileNoHardlink 是拒绝覆盖硬链接的原地写文件助手（替 os.WriteFile）。
func writeFileNoHardlink(path string, data []byte, perm fs.FileMode) error {
	f, err := openInPlaceNoHardlink(path, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
