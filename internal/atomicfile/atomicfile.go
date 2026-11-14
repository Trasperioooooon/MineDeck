// Package atomicfile 把一个文件整份换成新内容：先写到同一个文件夹里的临时文件、刷到盘上，再一步改名盖掉原文件。
// 任何时刻原位上要么是旧的一份、要么是新的一份，不会是半份，也不会是两次保存混在一起的东西。
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Hook 测试用：临时文件写好、还没换上去的那一刻调用（参数是要换掉的那个文件的路径）。
var Hook func(path string)

// Write 把 data 写成 path。
//
// 临时文件的名字每次都不一样（<原名>.<随机>.tmp，和原文件在同一个文件夹里，改名才是一步完成的）。
// 以前各处用的是固定的名字（原名加 .tmp）：两个保存同时进行时会打开同一个临时文件、各写各的，
// 最后换上去的内容是两次写入混在一起的（2026-11-14 Codex 第二轮复审阻挡项 3）。
// 用不同的名字以后，同时保存的两份各自完整，后改名的那一份留下。「后保存的盖掉先保存的改动」这件事它不管，
// 要靠调用方把「读、改、写」串起来（见 inst.UpdateProps）。
//
// 原文件是只读的：改名盖不掉。这时去掉只读再试一次（以前有的地方是先删原文件再改名，删的时候系统会顺带去掉只读，
// 行为保持一样）。失败时删掉临时文件，原文件不动。
func Write(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm) // CreateTemp 建的是 0600；Windows 上只分只读不只读，这里主要是别让新文件带着只读
	}
	if err == nil {
		if Hook != nil {
			Hook(path)
		}
		err = os.Rename(tmp, path)
		if err != nil {
			if fi, serr := os.Stat(path); serr == nil && fi.Mode().Perm()&0o200 == 0 && os.Chmod(path, 0o644) == nil {
				err = os.Rename(tmp, path)
			}
		}
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
