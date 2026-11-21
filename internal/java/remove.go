package java

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"minedeck/internal/app"
)

// ErrBusy 正在安装或卸载便携 Java 时再发起卸载返回此错误。
var ErrBusy = errors.New("正在安装或卸载 Java，请稍后再试")

// removeTree 删掉一个运行时目录。测试里换成会失败的函数，用来看删到一半时记号还在。
var removeTree = os.RemoveAll

// IncompletePortable 列出装到一半或删到一半留下的便携 Java 大版本（不算可用运行时，只给设置页列出以便清理）。
func IncompletePortable() []int {
	root := filepath.Clean(app.RuntimesDir)
	if app.RuntimesDir == "" || !filepath.IsAbs(root) || !plainDir(root) {
		return []int{}
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return []int{}
	}
	seen := map[int]bool{}
	for _, e := range ents {
		name := e.Name()
		if rest, ok := strings.CutPrefix(name, "java-"); ok {
			if base, mark := strings.CutSuffix(rest, ".incomplete"); mark {
				if e.Type().IsRegular() {
					if major, err := strconv.Atoi(base); err == nil && major >= 8 && major <= 64 && !IsInstalled(major) {
						seen[major] = true
					}
				}
				continue
			}
			if e.IsDir() && e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
				if major, err := strconv.Atoi(rest); err == nil && major >= 8 && major <= 64 && !IsInstalled(major) {
					seen[major] = true
				}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Ints(out)
	return out
}

// RemovePortable 删除 MineDeck 自己装在 runtimes/java-N 的运行时。
// inUse 在拿住安装锁之后再查一遍：有服务器还指着这个目录，或者 Java 正在安装，都不删。
// 不接受任意路径，也不跟随符号链接或目录联接，因此删不到系统里装的 Java。
func RemovePortable(major int, inUse func(dir string) error) error {
	if major < 8 || major > 64 {
		return fmt.Errorf("没有 Java %d 的便携运行时", major)
	}
	if !ensureMu.TryLock() {
		return ErrBusy
	}
	defer ensureMu.Unlock()

	dir, err := portableRuntimeDir(major)
	if err != nil {
		return err
	}
	root := filepath.Clean(app.RuntimesDir)
	if rst, rerr := os.Lstat(root); rerr != nil {
		if os.IsNotExist(rerr) {
			return fmt.Errorf("没有安装 Java %d 的便携运行时", major)
		}
		return fmt.Errorf("检查运行时目录失败：%w", rerr)
	} else if !rst.IsDir() || rst.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return fmt.Errorf("运行时目录是链接或目录联接，没有删除")
	}

	mark := incompleteMark(major)
	dirSt, dirErr := os.Lstat(dir)
	markSt, markErr := os.Lstat(mark)
	if dirErr != nil && !os.IsNotExist(dirErr) {
		return fmt.Errorf("检查 Java %d 目录失败：%w", major, dirErr)
	}
	if markErr != nil && !os.IsNotExist(markErr) {
		return fmt.Errorf("检查 Java %d 安装记号失败：%w", major, markErr)
	}
	if os.IsNotExist(dirErr) && os.IsNotExist(markErr) {
		return fmt.Errorf("没有安装 Java %d 的便携运行时", major)
	}
	if dirErr == nil && dirSt.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return fmt.Errorf("Java %d 的文件夹是链接或目录联接，没有删除，避免删到别的地方的 Java", major)
	}
	if dirErr == nil && !dirSt.IsDir() {
		return fmt.Errorf("Java %d 的位置不是文件夹，没有删除", major)
	}
	if markErr == nil && !markSt.Mode().IsRegular() {
		return fmt.Errorf("Java %d 的安装记号不是普通文件，没有删除", major)
	}
	if dirErr == nil && hasNestedLink(dir) {
		return fmt.Errorf("Java %d 的文件夹里含有链接或目录联接，没有删除，避免删到别的地方的文件", major)
	}
	if inUse != nil {
		if err := inUse(dir); err != nil {
			return err
		}
	}
	// 先写「没装完」，这样删到一半也不会被当成已经装好。
	if err := os.WriteFile(mark, nil, 0o644); err != nil {
		return fmt.Errorf("没法标记 Java %d 正在卸载：%w", major, err)
	}
	if dirErr == nil {
		if err := removeTree(dir); err != nil {
			return fmt.Errorf("Java %d 未能删干净：%w。它不会再被当成可用版本，可在设置里再删一次", major, err)
		}
	}
	if err := os.Remove(mark); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Java %d 的文件夹已经删掉，但未完成记号还在：%w", major, err)
	}
	return nil
}

func plainDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir() && st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}

func hasNestedLink(root string) bool {
	found := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			found = true
			return fs.SkipAll
		}
		if p != root && d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found || err != nil
}

// portableRuntimeDir 只接受 runtimes 目录下面、名字正好是 java-N 的那一层。
func portableRuntimeDir(major int) (string, error) {
	if strings.TrimSpace(app.RuntimesDir) == "" {
		return "", fmt.Errorf("Java %d 的位置不在 MineDeck 的运行时目录里，没有删除", major)
	}
	root := filepath.Clean(app.RuntimesDir)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("Java %d 的位置不在 MineDeck 的运行时目录里，没有删除", major)
	}
	name := "java-" + strconv.Itoa(major)
	dir := filepath.Join(root, name)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("Java %d 的位置不在 MineDeck 的运行时目录里，没有删除", major)
	}
	return dir, nil
}
