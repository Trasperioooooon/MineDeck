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
	"sync/atomic"

	"minedeck/internal/app"
	"minedeck/internal/linkmode"
)

// ErrBusy 正在安装或卸载便携 Java、或者有服务器正在换成这套便携 Java 时再发起卸载，返回此错误。
var ErrBusy = errors.New("正在安装或卸载 Java，或者有服务器正在换成这套 Java，请稍后再试")

// ErrRemoving 这套便携 Java 正在卸载时把服务器换成它，返回此错误（见 UsePortable）。
var ErrRemoving = errors.New("这套 Java 正在卸载，没有换上。等卸载完再换，或者换成别的 Java")

// ErrNotInstalled 要换上的便携 Java 已经卸载了、卸到一半，或者没装完整（见 UsePortable）。
var ErrNotInstalled = errors.New("这套 Java 已经卸载了或者没装完整，没有换上。请换成别的 Java，或者重新下载这一版")

// removeTree 删掉一个运行时目录。测试里换成会失败的函数，用来看删到一半时记号还在。
var removeTree = os.RemoveAll

// afterSwitchMarked、afterRemoveMarked 两边「登记了自己、还没看对方」的那一刻，默认什么都不做。
// 测试里换成会停住的函数，钉住两边的先后（见下面互斥那段）。
var afterSwitchMarked, afterRemoveMarked = func() {}, func() {}

// 卸载和「把服务器换成某套便携 Java」互斥（A34）：以前卸载查完没人用、还没删完的那一会儿，另一个页面能把服务器换成这套，
// 那台服下次开服就找不到 Java。两边各自先登记自己、再看对方：removing 是正在卸载的大版本（0 是没有，卸载拿着 ensureMu，
// 同一时间只有一个），switching 按大版本记有几台服务器正在换成它。原子操作有统一的先后，两边不会都没看到对方。
// 没用锁：换 Java 要是去等 ensureMu，别的大版本下载的那几分钟里连换一套现成的 Java 都得干等。
var (
	removing  atomic.Int32
	switching [maxPortableMajor + 1]atomic.Int32
)

const maxPortableMajor = 64

// UsePortable 把服务器换成便携 Java major 时，用它包住真正去换的 set：set 做完之前这套 Java 不会被卸载
// （RemovePortable 报 ErrBusy）；这套 Java 正在卸载时不调 set，返回 ErrRemoving；已经卸了、卸到一半（失败后留着
// java.exe 和「没装完」记号）或者没装完整时也不调，返回 ErrNotInstalled：调用方先前查过「装着」，到这里可能已经变了。
func UsePortable(major int, set func() error) error {
	if major < 8 || major > maxPortableMajor {
		return set() // 不是便携 Java 的大版本，RemovePortable 也不会删它
	}
	switching[major].Add(1)
	defer switching[major].Add(-1)
	afterSwitchMarked()
	if removing.Load() == int32(major) {
		return ErrRemoving
	}
	if !IsInstalled(major) {
		return ErrNotInstalled
	}
	return set()
}

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
					if major, err := strconv.Atoi(base); err == nil && major >= 8 && major <= maxPortableMajor && !IsInstalled(major) {
						seen[major] = true
					}
				}
				continue
			}
			if linkmode.IsPlainDirEntry(e) {
				if major, err := strconv.Atoi(rest); err == nil && major >= 8 && major <= maxPortableMajor && !IsInstalled(major) {
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
// 从 inUse 查起到删完，这套 Java 记成正在卸载，这期间换不上它；有服务器正在换成它时报 ErrBusy（见 UsePortable）。
// 不接受任意路径，也不跟随符号链接或目录联接，因此删不到系统里装的 Java。
func RemovePortable(major int, inUse func(dir string) error) error {
	if major < 8 || major > maxPortableMajor {
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
	} else if !linkmode.IsPlainDirInfo(rst) {
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
	if dirErr == nil && linkmode.IsLink(dirSt.Mode()) {
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
	removing.Store(int32(major)) // 先记上自己、再看有没有人正在换成它（顺序见 UsePortable）
	defer removing.Store(0)
	afterRemoveMarked()
	if switching[major].Load() > 0 {
		return ErrBusy
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
	return linkmode.IsPlainDir(p)
}

func hasNestedLink(root string) bool {
	found := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			found = true
			return fs.SkipAll
		}
		if p != root && linkmode.IsLink(d.Type()) {
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
