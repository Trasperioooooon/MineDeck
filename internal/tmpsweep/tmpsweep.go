// Package tmpsweep 清掉 MineDeck 自己留下、已经没人在用的临时文件。
//
// 下载、复制、保存文件都是先写到一个名字每次不一样的临时文件里，写完再改成正式名字；正常结束（成功或失败）时
// 临时文件当场就没了。进程被结束、断电、蓝屏时来不及清，就留在原地。以前临时文件用固定的名字，下一次会被盖掉；
// 改成随机名以后没有人再碰它们：下到一半的 Java 包、服务端 jar 各几十上百兆，反复强退会攒起来（2026-11-16 复审记下的）。
//
// 清的规矩往保守里定：
//   - 只认 MineDeck 自己起的那几种名字（见 IsOurs），随机的那一段必须全是数字，别的文件一概不碰；
//   - 只清很久没动过的（调用方给的 minAge）：正在写的文件一直在变，刚写完、等着改名的那一下也远小于这个时间；
//   - 只清普通文件，不跟着链接、目录联接走到别处去；
//   - 删不掉的（Windows 上还开着的文件删不掉）跳过，不重试、不报错。
package tmpsweep

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// kinds MineDeck 起的临时文件名：前缀 + 一串数字（os.CreateTemp 填的）+ 后缀。
var kinds = []struct{ prefix, suffix string }{
	{".minedeck-download-", ".part"},     // dl.Fetch
	{".minedeck-download-", ".download"}, // 待装清单、音乐共享下载模组
	{".minedeck-copy-", ".copy"},         // 待装清单把本地文件拷进 mods / plugins
	{".minedeck-write-", ".tmp"},         // atomicfile 保存文件
	{".minedeck-unzip-", ".part"},        // 解压 frpc
}

// IsOurs name 是不是 MineDeck 起的临时文件名。
func IsOurs(name string) bool {
	for _, k := range kinds {
		mid, ok := strings.CutPrefix(name, k.prefix)
		if !ok {
			continue
		}
		mid, ok = strings.CutSuffix(mid, k.suffix)
		if !ok || mid == "" || len(mid) > 24 {
			continue
		}
		if strings.Trim(mid, "0123456789") == "" {
			return true
		}
	}
	return false
}

// Result 清了多少。
type Result struct {
	Files int   // 删掉的文件数
	Bytes int64 // 它们一共多大
}

func (r *Result) add(o Result) { r.Files += o.Files; r.Bytes += o.Bytes }

// Dir 只清 dir 这一层：名字是 MineDeck 的临时文件、最后一次改动在 minAge 以前的普通文件。dir 不在、读不了都不算错。
func Dir(dir string, minAge time.Duration) Result {
	var res Result
	ents, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	for _, e := range ents {
		res.add(remove(dir, e, minAge))
	}
	return res
}

// Tree 清 root 这棵树，最多往下 maxDepth 层（0 是只清 root 这一层）。不进链接和目录联接。
func Tree(root string, maxDepth int, minAge time.Duration) Result {
	var res Result
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

// Startup 程序启动后清一遍 MineDeck 管着的文件夹：程序文件夹这一层（设置、密钥就存在这里）、data、下载缓存、
// 便携运行时这几棵树，以及每台服务器文件夹的根上和 mods、plugins、config 这几层。
// 服务器文件夹不往深处翻：世界里的文件动辄几万个，MineDeck 的临时文件也不会写到那里面去。
// 整合包导入时下到更深的文件夹里（比如 config 的子文件夹）没写完的，这里清不到。
func Startup(base, cacheDir, runtimesDir string, instanceDirs []string, minAge time.Duration) Result {
	var res Result
	res.add(Dir(base, minAge))
	res.add(Tree(filepath.Join(base, "data"), 4, minAge))
	res.add(Tree(cacheDir, 6, minAge))
	res.add(Tree(runtimesDir, 12, minAge))
	for _, d := range instanceDirs {
		res.add(Dir(d, minAge))
		for _, sub := range []string{"mods", "plugins", "config"} {
			res.add(Dir(filepath.Join(d, sub), minAge))
		}
	}
	return res
}

func remove(dir string, e fs.DirEntry, minAge time.Duration) Result {
	if !e.Type().IsRegular() || !IsOurs(e.Name()) {
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
