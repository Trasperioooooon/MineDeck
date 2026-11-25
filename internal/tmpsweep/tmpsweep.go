// Package tmpsweep 清掉 MineDeck 自己留下、已经没人在用的临时文件。
//
// 下载、复制、保存文件、打包备份都是先写到一个名字每次不一样的临时文件里，写完再改成正式名字；正常结束（成功或失败）时
// 临时文件当场就没了。进程被结束、断电、蓝屏时来不及清，就留在原地。以前临时文件用固定的名字，下一次会被盖掉；
// 改成随机名以后没有人再碰它们：下到一半的 Java 包、服务端 jar、半截世界备份各几十上百兆，反复强退会攒起来（2026-11-16 复审记下的）。
//
// 清的规矩往保守里定：
//   - 只认 MineDeck 自己起的那几种名字（见 IsOurs），随机的那一段必须全是数字，别的文件一概不碰；
//     以前版本在备份文件夹里留下的 .backup-<数字>.tmp 只在备份文件夹那一层按同样严格的规则清，别的地方一概不动；
//   - 只清很久没动过的（调用方给的 minAge）：正在写的文件一直在变，刚写完、等着改名的那一下也远小于这个时间；
//   - 只清普通文件，不跟着链接、目录联接走到别处去；
//   - 服务器文件夹往下找时不进世界存档文件夹（带 level.dat 的文件夹），并限制层数；
//   - 删不掉的（Windows 上还开着的文件删不掉）跳过，不重试、不报错。
package tmpsweep

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/linkmode"
)

// instanceMaxDepth 服务器文件夹往下最多找几层（0 是只看服务器根目录这一层）。
// 够覆盖整合包导入和补装可能落到的 config/某模组/...、config/paxi/datapacks/...、kubejs/assets/... 等深层目录。
const instanceMaxDepth = 6

// kinds MineDeck 起的临时文件名：前缀 + 一串数字（os.CreateTemp 填的）+ 后缀。
var kinds = []struct{ prefix, suffix string }{
	{".minedeck-download-", ".part"},     // dl.Fetch
	{".minedeck-download-", ".download"}, // 待装清单、音乐共享下载模组
	{".minedeck-copy-", ".copy"},         // 待装清单把本地文件拷进 mods / plugins
	{".minedeck-write-", ".tmp"},         // atomicfile 保存文件
	{".minedeck-unzip-", ".part"},        // 解压 frpc
	{".minedeck-backup-", ".tmp"},        // 世界备份压缩包
}

// maxTempDigits os.CreateTemp 生成的随机段是 uint32 的十进制（0～4294967295，不补零，见标准库 os/tempfile.go 的 nextRandom），最多 10 位。
// 带前导零的序号（如 007、0001）、大于 4294967295 的 10 位数、11 位及以上的数字串（如 14 位时间戳）MineDeck 自己都起不出来，按保守原则一律不认。
const maxTempDigits = 10

func matchTempDigits(name, prefix, suffix string) bool {
	mid, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	mid, ok = strings.CutSuffix(mid, suffix)
	if !ok || mid == "" || len(mid) > maxTempDigits {
		return false
	}
	if strings.Trim(mid, "0123456789") != "" {
		return false
	}
	if len(mid) > 1 && mid[0] == '0' {
		return false
	}
	_, err := strconv.ParseUint(mid, 10, 32)
	return err == nil
}

// IsOurs name 是不是 MineDeck 起的临时文件名。
func IsOurs(name string) bool {
	for _, k := range kinds {
		if matchTempDigits(name, k.prefix, k.suffix) {
			return true
		}
	}
	return false
}

// isLegacyBackupTemp 以前版本（v3.2.4 及以前）备份留下的 .backup-<全数字>.tmp。
// 名字不是 .minedeck- 开头，只在每台服务器实际用的备份文件夹这一层认，别的地方一概不动。
func isLegacyBackupTemp(name string) bool {
	return matchTempDigits(name, ".backup-", ".tmp")
}

// Instance 一台已登记服务器的文件夹与名称（由调用方从内存传入，这里不读盘上的配置文件）。
type Instance struct {
	Dir  string
	Name string
}

// Result 清了多少。
type Result struct {
	Files int   // 删掉的文件数
	Bytes int64 // 它们一共多大
}

func (r *Result) add(o Result) { r.Files += o.Files; r.Bytes += o.Bytes }

// plainDir p 是一个真的文件夹，不是链接、目录联接。
// 是链接或联接的不清：里面的东西其实在别处（比如服主把 mods 联接到几台服共用的模组库），不归这台服务器管（2026-11-19 Codex 复审）。
func plainDir(p string) bool {
	return linkmode.IsPlainDir(p)
}

// Dir 只清 dir 这一层：名字是 MineDeck 的临时文件、最后一次改动在 minAge 以前的普通文件。dir 不在、读不了都不算错。
// dir 本身是链接或目录联接时不清（见 plainDir）。
func Dir(dir string, minAge time.Duration) Result {
	var res Result
	if !plainDir(dir) {
		return res
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	for _, e := range ents {
		res.add(remove(dir, e, minAge))
	}
	return res
}

// Tree 清 root 这棵树，最多往下 maxDepth 层（0 是只清 root 这一层）。不进链接和目录联接，root 本身是的也不清。
func Tree(root string, maxDepth int, minAge time.Duration) Result {
	var res Result
	if !plainDir(root) {
		return res
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return res
	}
	for _, e := range ents {
		if e.IsDir() { // 链接、目录联接不算真文件夹（IsLink 拦住），云同步真实文件夹放行
			if maxDepth > 0 && !linkmode.IsLink(e.Type()) {
				res.add(Tree(filepath.Join(root, e.Name()), maxDepth-1, minAge))
			}
			continue
		}
		res.add(remove(root, e, minAge))
	}
	return res
}

// isWorldDir dir 里有没有 level.dat（世界存档文件夹）。看不清有没有时也按「有」算，宁可不进去翻。
func isWorldDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, "level.dat"))
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// instanceTree 清服务器文件夹 root，最多往下 maxDepth 层，只认 IsOurs。
// 不进链接和目录联接，也不进世界文件夹（有 level.dat 的文件夹）：世界里文件动辄几万个，且全是用户存档数据。
func instanceTree(root string, maxDepth int, minAge time.Duration) Result {
	var res Result
	if !plainDir(root) {
		return res
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return res
	}
	for _, e := range ents {
		if e.IsDir() {
			if maxDepth > 0 && !linkmode.IsLink(e.Type()) {
				sub := filepath.Join(root, e.Name())
				if !isWorldDir(sub) {
					res.add(instanceTree(sub, maxDepth-1, minAge))
				}
			}
			continue
		}
		res.add(remove(root, e, minAge))
	}
	return res
}

// backupDir 只清备份文件夹 dir 这一层（不进任何子文件夹）：除了 IsOurs 认的 .minedeck-* 临时文件，
// 也把以前版本留下的 .backup-<全数字>.tmp 一并清掉。dir 本身是链接或目录联接时不清。
func backupDir(dir string, minAge time.Duration) Result {
	var res Result
	if !plainDir(dir) {
		return res
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	for _, e := range ents {
		res.add(removeMatch(dir, e, minAge, func(name string) bool {
			return IsOurs(name) || isLegacyBackupTemp(name)
		}))
	}
	return res
}

// Startup 程序启动后清一遍 MineDeck 管着的文件夹：程序文件夹这一层（设置、密钥就存在这里）、data、下载缓存、
// 便携运行时这几棵树，每台服务器文件夹（限制层数、跳过带 level.dat 的世界文件夹），以及每台服务器在 backupRoots 下的备份文件夹这一层。
// backupRoots（如 app.AllBackupRoots()）和 instances 由调用方从内存传入，这里不再读盘上的配置文件。
func Startup(base, cacheDir, runtimesDir string, backupRoots []string, instances []Instance, minAge time.Duration) Result {
	var res Result
	res.add(Dir(base, minAge))
	res.add(Tree(filepath.Join(base, "data"), 4, minAge))
	res.add(Tree(cacheDir, 6, minAge))
	res.add(Tree(runtimesDir, 12, minAge))
	roots := cleanBackupRoots(backupRoots)
	seenBackup := map[string]bool{}
	for _, inst := range instances {
		if inst.Dir != "" {
			res.add(instanceTree(inst.Dir, instanceMaxDepth, minAge))
		}
		sub, ok := backupSubName(inst.Name)
		if !ok {
			continue
		}
		for _, r := range roots {
			if !plainDir(r) {
				continue
			}
			bdir := filepath.Clean(filepath.Join(r, sub))
			bk := pathKey(bdir)
			if seenBackup[bk] {
				continue
			}
			seenBackup[bk] = true
			res.add(backupDir(bdir, minAge))
		}
	}
	return res
}

func pathKey(clean string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func cleanBackupRoots(roots []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			continue
		}
		c := filepath.Clean(r)
		k := pathKey(c)
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}

// backupSubName 保守校验实例名能不能安全当备份根下面的单层子文件夹名用：
// 拒绝空串、.、..、以点或空格结尾、带路径分隔符或冒号的名字，并要求 filepath.IsLocal（Windows 上同时挡住 CON 等保留设备名）。
func backupSubName(name string) (string, bool) {
	sub := filepath.Base(name)
	if name == "" || sub == "" || sub == "." || sub == ".." ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") ||
		strings.ContainsAny(name, `/\:`) || !filepath.IsLocal(sub) {
		return "", false
	}
	return sub, true
}

func remove(dir string, e fs.DirEntry, minAge time.Duration) Result {
	return removeMatch(dir, e, minAge, IsOurs)
}

func removeMatch(dir string, e fs.DirEntry, minAge time.Duration, match func(string) bool) Result {
	if !e.Type().IsRegular() || !match(e.Name()) {
		return Result{}
	}
	p := filepath.Join(dir, e.Name())
	st, err := os.Lstat(p) // 再看一眼：列目录到现在之间可能变了
	if err != nil || !st.Mode().IsRegular() || time.Since(st.ModTime()) < minAge {
		return Result{}
	}
	if os.Remove(p) != nil {
		return Result{}
	}
	return Result{Files: 1, Bytes: st.Size()}
}
