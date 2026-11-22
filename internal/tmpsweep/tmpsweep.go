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
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"minedeck/internal/app"
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

func matchTempDigits(name, prefix, suffix string) bool {
	mid, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	mid, ok = strings.CutSuffix(mid, suffix)
	if !ok || mid == "" || len(mid) > 24 {
		return false
	}
	return strings.Trim(mid, "0123456789") == ""
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

// Result 清了多少。
type Result struct {
	Files int   // 删掉的文件数
	Bytes int64 // 它们一共多大
}

func (r *Result) add(o Result) { r.Files += o.Files; r.Bytes += o.Bytes }

// plainDir p 是一个真的文件夹，不是链接、目录联接。
// 是链接或联接的不清：里面的东西其实在别处（比如服主把 mods 联接到几台服共用的模组库），不归这台服务器管（2026-11-19 Codex 复审）。
func plainDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir() && st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
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
		if e.IsDir() { // 链接、目录联接不算文件夹（Type 里带着别的标记），不会走进去
			if maxDepth > 0 && e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
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
			if maxDepth > 0 && e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
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
// 便携运行时这几棵树，每台服务器文件夹（限制层数、跳过带 level.dat 的世界文件夹），以及每台服务器实际用的备份文件夹这一层。
func Startup(base, cacheDir, runtimesDir string, instanceDirs []string, minAge time.Duration) Result {
	var res Result
	res.add(Dir(base, minAge))
	res.add(Tree(filepath.Join(base, "data"), 4, minAge))
	res.add(Tree(cacheDir, 6, minAge))
	res.add(Tree(runtimesDir, 12, minAge))
	backupRoots := backupRootsOf(base)
	seenBackup := map[string]bool{}
	for _, d := range instanceDirs {
		res.add(instanceTree(d, instanceMaxDepth, minAge))
		sub, ok := instanceBackupSub(d)
		if !ok {
			continue
		}
		for _, r := range backupRoots {
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

type backupConfig struct {
	BackupsDir  string   `json:"backupsDir"`
	BackupRoots []string `json:"backupRoots"`
}

func readBackupConfig(path string) (backupConfig, bool) {
	var cfg backupConfig
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return cfg, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, false
	}
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &cfg); err != nil {
		return cfg, false
	}
	return cfg, true
}

func backupRootsOf(base string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		c := filepath.Clean(d)
		k := pathKey(c)
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	if app.Base != "" && base != "" && pathKey(filepath.Clean(app.Base)) == pathKey(filepath.Clean(base)) {
		for _, r := range app.AllBackupRoots() {
			add(r)
		}
	}
	if base != "" {
		if plainDir(base) {
			if cfg, ok := readBackupConfig(filepath.Join(base, "config.json")); ok {
				add(cfg.BackupsDir)
				add(filepath.Join(base, "backups"))
				for _, r := range cfg.BackupRoots {
					add(r)
				}
			}
		}
		add(filepath.Join(base, "backups"))
	}
	return out
}

// instanceBackupSub 从服务器文件夹里的 instance.json 读出实例名，按 inst.backupDirOf 的同一规矩算出备份子文件夹名。
// 不能直接取 filepath.Base(dir)：中文名服务器的目录名是 server-时间戳，接管来的也是原文件夹名，备份都按 instance.json 的 name 存放。
func instanceBackupSub(dir string) (string, bool) {
	p := filepath.Join(dir, "instance.json")
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	var s struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &s); err != nil || s.Name == "" {
		return "", false
	}
	sub := filepath.Base(s.Name)
	if sub == "" || sub == "." || sub == ".." || strings.ContainsAny(sub, `/\:`) || !filepath.IsLocal(sub) {
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
