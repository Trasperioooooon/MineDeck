package inst

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"minedeck/internal/atomicfile"
)

// ErrHardlinkEscape 目标文件有多个硬链接（可能连向服务器文件夹外的文件），拒绝原地写入以免改穿到外面。
var ErrHardlinkEscape = errors.New("这个文件和服务器文件夹外的文件是同一份（硬链接），为了不改到外面，没有写入")

// openInPlaceNoHardlink 打开 path 准备从头覆盖写入（A11）。硬链接数在打开以后的同一个句柄上查，大于 1 就在截断之前关掉并拒绝，
// 不会改到硬链接另一端的文件。符号链接文件照旧跟过去写：这里只管硬链接。
func openInPlaceNoHardlink(path string, perm fs.FileMode) (*os.File, error) {
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
