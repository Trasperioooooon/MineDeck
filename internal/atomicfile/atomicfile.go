// Package atomicfile 把一个文件整份换成新内容：先写到同一个文件夹里的临时文件、刷到盘上，再一步改名盖掉原文件。
// 任何时刻原位上要么是旧的一份、要么是新的一份，不会是半份，也不会是两次保存混在一起的东西。
package atomicfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Hook 测试用：临时文件写好、还没换上去的那一刻调用（参数是要换掉的那个文件的路径）。
var Hook func(path string)

// Write 把 data 写成 path。
//
// 临时文件的名字每次都不一样（.minedeck-write-<随机>.tmp，和原文件在同一个文件夹里，改名才是一步完成的）。
// 以前各处用的是固定的名字（原名加 .tmp）：两个保存同时进行时会打开同一个临时文件、各写各的，
// 最后换上去的内容是两次写入混在一起的（2026-11-14 Codex 第二轮复审阻挡项 3）。
// 用不同的名字以后，同时保存的两份各自完整，后改名的那一份留下。「后保存的盖掉先保存的改动」这件事它不管，
// 要靠调用方把「读、改、写」串起来（见 inst.UpdateProps）。
//
// 原文件是只读的：Windows 用忽略只读属性的扩展改名，不改变原文件属性。
// 文件系统不支持则报错并清理临时文件，绝不先删原文件。
func Write(path string, data []byte, perm fs.FileMode) error {
	return copyChecked(path, bytes.NewReader(data), perm, nil, nil, true)
}

// WriteChecked 检查已经写好的临时文件，通过后才替换原文件。
func WriteChecked(path string, data []byte, perm fs.FileMode, check func(string) error) error {
	return copyChecked(path, bytes.NewReader(data), perm, check, nil, false)
}

// WriteCheckedWith combines validation with a streaming write guard.
func WriteCheckedWith(path string, data []byte, perm fs.FileMode, check func(string) error, wrap func(io.Writer) io.Writer) error {
	return copyChecked(path, bytes.NewReader(data), perm, check, wrap, false)
}

// Copy 流式写完整的一份后原子替换。读改写的调用方自行持有 LockPath，避免重入。
func Copy(path string, r io.Reader, perm fs.FileMode) error {
	return copyChecked(path, r, perm, nil, nil, true)
}

// CopyWith 可给实际临时文件的写入加上磁盘余量等保护；失败时仍保留原文件。
func CopyWith(path string, r io.Reader, perm fs.FileMode, wrap func(io.Writer) io.Writer) error {
	return copyChecked(path, r, perm, nil, wrap, true)
}

// WriteProtected 保留原有只读保护：配置/密钥缓存等以前拒绝覆盖的文件不能因采用原子写就被强行覆盖。
func WriteProtected(path string, data []byte, perm fs.FileMode) error {
	return copyChecked(path, bytes.NewReader(data), perm, nil, nil, false)
}

func copyChecked(path string, r io.Reader, perm fs.FileMode, check func(string) error, wrap func(io.Writer) io.Writer, allowReadOnly bool) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".minedeck-write-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if e := os.Remove(tmp); e != nil && !errors.Is(e, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("清理临时文件失败：%w", e))
		}
	}()
	var w io.Writer = f
	if wrap != nil {
		w = wrap(w)
	}
	_, err = io.Copy(w, r)
	if err == nil {
		err = f.Chmod(perm)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if check != nil {
		if err := check(tmp); err != nil {
			return err
		}
	}
	return replacePrepared(tmp, path, allowReadOnly)
}

// Replace 将已写完的同卷临时文件换到 path。失败时 tmp 留给调用方清理，绝不先删原文件。
// 临时文件先刷盘，再用平台的改名操作发布；不能据此保证故障存储设备的断电行为。
func Replace(tmp, path string) error { return replacePrepared(tmp, path, true) }

func replacePrepared(tmp, path string, allowReadOnly bool) error {
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return fmt.Errorf("刷盘临时文件失败：%w", err)
	}
	var old fs.FileInfo
	if allowReadOnly {
		old, _ = os.Lstat(path)
	}
	if old != nil {
		_ = os.SameFile(old, old)
	} // Windows 延迟取得文件编号；在测试钩子和重试之前固定身份。
	if Hook != nil {
		Hook(path)
	}
	if err := replace(tmp, path, old); err != nil {
		return fmt.Errorf("替换文件失败：%w", err)
	}
	return nil
}
