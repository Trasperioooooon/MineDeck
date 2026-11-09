package transfer

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Unpack 把一个 .mdpack 解压到 destDir（新建实例的目录）。manifest 条目不落盘；条目名先做安全校验：
// 拒绝绝对路径、盘符、反斜杠混写和 .. 穿越（zip-slip）。progress 的 total 是所有条目未压缩大小之和。
// 返回解出来的文件条数。
func Unpack(src, destDir string, progress func(done, total int64)) (int, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, errFormat
	}
	defer zr.Close()
	cleanDest := filepath.Clean(destDir)
	if err := os.MkdirAll(cleanDest, 0o755); err != nil {
		return 0, err
	}
	var total int64
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.ToSlash(f.Name), ManifestFile) {
			total += int64(f.UncompressedSize64)
		}
	}
	var done int64
	entries := 0
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.EqualFold(name, ManifestFile) {
			continue // manifest 只作为清单，不落进实例目录
		}
		if err := safeName(name); err != nil {
			return 0, fmt.Errorf("%w：包里的路径不安全（%s）", errFormat, f.Name)
		}
		target := filepath.Join(cleanDest, filepath.FromSlash(name))
		if !withinDir(cleanDest, target) {
			return 0, fmt.Errorf("%w：包里的路径跑到实例目录外面去了（%s）", errFormat, f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		rc, err := f.Open()
		if err != nil {
			return 0, err
		}
		w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return 0, err
		}
		_, cerr := io.Copy(w, rc)
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		rc.Close()
		if cerr != nil {
			return 0, fmt.Errorf("解压 %s 失败: %w", name, cerr)
		}
		_ = os.Chtimes(target, f.Modified, f.Modified) // 尽量保留包里的修改时间
		entries++
		done += int64(f.UncompressedSize64)
		if progress != nil {
			progress(done, total)
		}
	}
	return entries, nil
}

// safeName 包里的条目名必须是不带盘符、不带 .. 的相对路径（正斜杠）。
func safeName(name string) error {
	if name == "" || name == "." {
		return fmt.Errorf("空的路径")
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("用了反斜杠")
	}
	if filepath.IsAbs(name) || strings.Contains(name, ":") {
		return fmt.Errorf("是绝对路径")
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == ".." {
			return fmt.Errorf("有空段或 ..")
		}
	}
	return nil
}

// withinDir target 是不是在 dir 里面（或就是它两个写法不同的别名）。
func withinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
