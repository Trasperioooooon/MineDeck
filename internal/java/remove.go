package java

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"minedeck/internal/app"
)

// removeTree 删掉一个运行时目录。测试里换成会失败的函数，用来看删到一半时记号还在。
var removeTree = os.RemoveAll

// RemovePortable 删除 MineDeck 自己装在 runtimes/java-N 的运行时。
// inUse 在拿住安装锁之后再查一遍：有服务器还指着这个目录，或者 Java 正在安装，都不删。
// 不接受任意路径，也不跟随链接，因此删不到系统里装的 Java。
func RemovePortable(major int, inUse func(dir string) error) error {
	if major < 8 || major > 64 {
		return fmt.Errorf("没有 Java %d 的便携运行时", major)
	}
	if !ensureMu.TryLock() {
		return fmt.Errorf("正在安装或卸载 Java，等它结束再试")
	}
	defer ensureMu.Unlock()

	dir, err := portableRuntimeDir(major)
	if err != nil {
		return err
	}
	mark := incompleteMark(major)
	dirSt, dirErr := os.Lstat(dir)
	markSt, markErr := os.Lstat(mark)
	if dirErr != nil && os.IsNotExist(dirErr) && (markErr != nil || !markSt.Mode().IsRegular()) {
		return fmt.Errorf("没有安装 Java %d 的便携运行时", major)
	}
	if dirErr == nil && dirSt.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Java %d 的文件夹是链接，没有删除，避免删到别的地方的 Java", major)
	}
	if dirErr == nil && !dirSt.IsDir() {
		return fmt.Errorf("Java %d 的位置不是文件夹，没有删除", major)
	}
	if markErr == nil && !markSt.Mode().IsRegular() {
		return fmt.Errorf("Java %d 的安装记号不是普通文件，没有删除", major)
	}
	if inUse != nil {
		if err := inUse(dir); err != nil {
			return err
		}
	}
	// 先写「没装完」，这样删到一半也不会被当成已经装好。
	if err := os.MkdirAll(app.RuntimesDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(mark, nil, 0o644); err != nil {
		return fmt.Errorf("没法标记 Java %d 正在卸载：%w", major, err)
	}
	if dirErr == nil {
		if err := removeTree(dir); err != nil {
			return fmt.Errorf("Java %d 删到一半：%w。记号还在，不会把它当成已装好的，可以再删一次", major, err)
		}
	}
	if err := os.Remove(mark); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Java %d 的文件夹已经删掉，但「没装完」的记号还在：%w", major, err)
	}
	return nil
}

// portableRuntimeDir 只接受 runtimes 目录下面、名字正好是 java-N 的那一层。
func portableRuntimeDir(major int) (string, error) {
	root := filepath.Clean(app.RuntimesDir)
	name := "java-" + strconv.Itoa(major)
	dir := filepath.Join(root, name)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("Java %d 的位置不在 MineDeck 的运行时目录里，没有删除", major)
	}
	return dir, nil
}
