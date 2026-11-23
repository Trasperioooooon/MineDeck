package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"minedeck/internal/procutil"
)

// 找到 frpc 以后、用之前，认出明显坏掉的。
//
// v3.2.2 以前解压 frpc 是直接往正式文件名里写的，解压到一半出错（盘满、压缩包坏了、程序被关掉）会留下半截的 exe：
// 文件名对，内容只有前面一段。findExe 只看文件名，照样找到它、照用，隧道一直起不来，只能手动删。
//
// 认法只看 PE 文件的结构，不运行它：开头是不是 MZ、PE 头在不在、节表列出的每一节的数据是不是都在文件里。
// 半截的 exe 后面的节一定缺数据（Windows 加载这样的文件会直接拒绝，报「不是有效的 Win32 应用程序」）；
// 完整的 exe 不会被这条规则误伤。读几百个字节就够，每次启动隧道都查也不费事。
// 没有选「跑一次 frpc -v」：杀毒软件常常拦 frpc，启动被拦、被拖慢时分不清是它坏了还是被拦了，容易把好的删掉；
// 没有选「比大小」：OpenFrp 的 frpc 没有可对的大小和哈希，樱花的是用户自己放的。
// 这样认不出来的：大小没变、中间内容坏了的（那要哈希才行）。

type exeState int

const (
	exeOK        exeState = iota // 看不出毛病
	exeTruncated                 // 半截的：是个 Windows 程序的开头，后面缺了；或者是空文件
	exeNotPE                     // 不是 Windows 程序（网页、压缩包、别的东西）
	exeUnknown                   // 判断不了：读不了（被占着、没权限、被杀毒软件拦下），或者不是普通文件
)

// inspectExe 看一个 exe 是不是明显坏掉的，why 是给人看的原因（exeOK 时为空）。只读文件开头的几个头和节表。
// seen 是查的那个文件当时的样子，从打开着的句柄上取（打不开时为 nil）：要删它之前拿来核对还是不是这个文件（removeIfUnchanged）。
func inspectExe(path string) (state exeState, why string, seen os.FileInfo) {
	if st, err := os.Lstat(path); err != nil {
		return exeUnknown, err.Error(), nil
	} else if !st.Mode().IsRegular() {
		return exeUnknown, "不是普通文件", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return exeUnknown, err.Error(), nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return exeUnknown, err.Error(), nil
	}
	state, why = inspectPE(f, st.Size())
	return state, why, st
}

// inspectPE inspectExe 的结构检查部分：f 是打开着的文件，size 是它的大小。
func inspectPE(f *os.File, size int64) (exeState, string) {
	if size == 0 {
		return exeTruncated, "是个空文件"
	}
	// readAt 读满 n 个字节；文件不够长返回 short=true，别的读错误返回 err
	readAt := func(off int64, n int) (b []byte, short bool, err error) {
		b = make([]byte, n)
		if _, err := f.ReadAt(b, off); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, true, nil
			}
			return nil, false, err
		}
		return b, false, nil
	}
	head, short, err := readAt(0, 2)
	if err != nil {
		return exeUnknown, err.Error()
	}
	if short || head[0] != 'M' || head[1] != 'Z' {
		return exeNotPE, "不是 Windows 程序（开头不是 MZ）"
	}
	truncated := func(what string) (exeState, string) {
		return exeTruncated, fmt.Sprintf("文件不完整，只有 %d 字节，%s", size, what)
	}
	dos, short, err := readAt(0, 64)
	if err != nil {
		return exeUnknown, err.Error()
	}
	if short {
		return truncated("连程序头都不全")
	}
	peOff := int64(binary.LittleEndian.Uint32(dos[60:64]))
	if peOff < 4 {
		return exeNotPE, "不是 Windows 程序（没有 PE 头）"
	}
	coff, short, err := readAt(peOff, 24) // "PE\0\0" 加 20 字节的 COFF 头
	if err != nil {
		return exeUnknown, err.Error()
	}
	if short {
		return truncated("连程序头都不全")
	}
	if string(coff[:4]) != "PE\x00\x00" {
		return exeNotPE, "不是 Windows 程序（没有 PE 头）"
	}
	sections := int(binary.LittleEndian.Uint16(coff[6:8]))
	optSize := int64(binary.LittleEndian.Uint16(coff[20:22]))
	if sections == 0 || sections > 96 { // Windows 最多认 96 节
		return exeNotPE, "不是 Windows 程序（节表不对）"
	}
	table, short, err := readAt(peOff+24+optSize, sections*40)
	if err != nil {
		return exeUnknown, err.Error()
	}
	if short {
		return truncated("连程序头都不全")
	}
	for k := 0; k < sections; k++ {
		sec := table[k*40 : (k+1)*40]
		rawSize := int64(binary.LittleEndian.Uint32(sec[16:20]))
		rawPtr := int64(binary.LittleEndian.Uint32(sec[20:24]))
		if rawSize == 0 || rawPtr == 0 {
			continue // 这一节在文件里不占地方（.bss 这类）
		}
		if rawPtr+rawSize > size {
			return truncated(fmt.Sprintf("程序本身应不少于 %d 字节", rawPtr+rawSize))
		}
	}
	return exeOK, ""
}

// stdFrpcNames frpc 的两个标准文件名：自定义 frps 的 zip 里是 frpc.exe，OpenFrp 的是 frpc_windows_amd64.exe，提示里叫人手动放的也是这两个。
var stdFrpcNames = []string{"frpc.exe", "frpc_windows_amd64.exe"}

func isStdFrpcName(name string) bool {
	return slices.ContainsFunc(stdFrpcNames, func(s string) bool { return strings.EqualFold(s, name) })
}

// frpcCandidates dir 里文件名像 frpc 的 exe，按挑选的先后：frpc.exe、frpc_windows_amd64.exe，然后是别的（findExe 取第一个）。
// 两个标准名直接查，不靠列目录：文件夹没有列目录的权限、却能打开里面的文件时照样找得到（用 Stat：
// 指向的东西已经不在了的链接跳过）。列目录时只跳过直接查已经找到的那几个，别的照旧（比如区分大小写的文件夹里的 FRPC.EXE）。
func frpcCandidates(dir string) []string {
	var out []string
	found := map[string]bool{}
	for _, n := range stdFrpcNames {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
			found[n] = true
		}
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() || !isFrpcExeName(e.Name()) || found[strings.ToLower(e.Name())] {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// plainDir p 是一个真的文件夹，不是链接、目录联接（那样的话里面的东西其实在别处）。
func plainDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir() && st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}

// usableFrpc 在 dir 里找一个能用的 frpc，明显坏掉的先处理掉。返回空串和 nil 表示没有可用的，调用方接着走原来的下载、解压或提示。
//
// managed 说的是这个文件夹里的 frpc 归谁管：
//   - true（OpenFrp、自定义 frps）：frpc 是 MineDeck 自己下载、解压出来的。半截的、不是 Windows 程序的都删掉，接着重新准备。
//   - false（樱花）：frpc 是用户自己下载、手动放进去的，MineDeck 没法替他重新下载。只有半截的（文件的开头是个程序、后面缺了，
//     或者是空文件）才删：这样的文件确实没用了。不是 Windows 程序的（可能是下载成了网页、压缩包没解开，也可能是他有意放的别的东西）
//     不删，报错里说清楚是哪个文件、怎么回事，让人自己处理；文件夹里另有能用的就用那个。
//
// 判断不了的（读不了、被占着）一律当成能用的交回去，和以前一样：启动那一步出了错另有说明（见 blocked.go）。
// 只动 dir 里文件名像 frpc 的 exe（frpcCandidates），别的文件不碰。OpenFrp、自定义 frps 的文件夹里，自动删的只有两个标准名
// （MineDeck 解压出来的、提示里叫人放的就是它们）：别的名字可能是人自己留着的东西，坏了也不用、不删。
// dir 本身是链接或目录联接时一个都不删：里面的东西其实在别处，不归 MineDeck 管（2026-11-19 Codex 复审）。
//
// skip 是判坏了却没删的（OpenFrp、自定义 frps 文件夹里不是标准名的）：接下来解压完挑 frpc 时要跳过它们，
// 不然解压出来的那份不见了时会又挑中它，盖掉本该报的「frpc 不见了」（A56）。
func usableFrpc(dir string, con *procutil.Console, managed bool) (exe string, skip []string, err error) {
	var kept []string // 没删的坏文件：都不能用时报出来
	canDelete := plainDir(dir)
	for _, p := range frpcCandidates(dir) {
		name := filepath.Base(p)
		c, err := checkFrpc(p, func(s exeState) bool {
			// 樱花的只删半截的；OpenFrp、自定义 frps 的只删标准名（别的名字可能是人自己留着的东西）
			return canDelete && (managed || s == exeTruncated) && (!managed || isStdFrpcName(name))
		})
		switch {
		case err != nil:
			return "", skip, fmt.Errorf("frpc 文件夹里的 %s 是坏的（%s），又没能删掉它（%w）。请关掉可能占着它的程序，手动删掉这个文件后重试。它在这个文件夹：%s", name, c.why, err, dir)
		case c.state == exeOK || c.state == exeUnknown:
			return p, skip, nil
		case c.removed && managed:
			con.Append(fmt.Sprintf("[MineDeck] 发现坏掉的 frpc：%s %s（多半是以前解压到一半出了错留下的）。已经删掉，现在重新准备", name, c.why))
		case c.removed:
			con.Append(fmt.Sprintf("[MineDeck] 发现坏掉的 frpc：%s %s（多半是复制或下载到一半断了）。已经删掉，请重新下载一份放进来", name, c.why))
		case managed && canDelete:
			skip = append(skip, p) // 不是 MineDeck 放的名字：不用、不删，接着重新准备（解压出来的是标准名）
		default:
			kept = append(kept, fmt.Sprintf("%s（%s）", name, c.why))
		}
	}
	if len(kept) > 0 {
		return "", skip, fmt.Errorf("frpc 文件夹里的 %s 不能运行，可能是下载成了网页，或者下载的是压缩包、没有解开。MineDeck 没有动它；"+
			"请把它换成从服务商下载的 frpc exe 后重试。它在这个文件夹：%s", strings.Join(kept, "、"), dir)
	}
	return "", skip, nil
}

// frpcCheck checkFrpc 的结果：最后一次查的状态和原因，坏的有没有删掉。
type frpcCheck struct {
	state   exeState
	why     string
	removed bool
}

// inspectFrpc 查 frpc 用的 inspectExe（是变量只为测试：查完以后把文件换掉，造「查完到删之间被换了」）。
var inspectFrpc = inspectExe

// frpcRechecks 查完到删之间文件一再被换掉时，前后一共最多查几次。一直在变说明有程序正往里写，当判断不了。
const frpcRechecks = 3

// checkFrpc 查 exe 坏没坏；坏了、mayDelete 也说可以删的就删掉。查完到删之间文件可能被换掉（比如用户刚放进来一个好的），
// 所以删之前再确认还是刚才查的那个文件（removeIfUnchanged），换了就不删、重新查（A56）。
// 删不掉时返回错误，c 里是查到的原因。
func checkFrpc(exe string, mayDelete func(exeState) bool) (c frpcCheck, err error) {
	var seen os.FileInfo
	c.state, c.why, seen = inspectFrpc(exe)
	for n := 1; (c.state == exeTruncated || c.state == exeNotPE) && mayDelete(c.state); n++ {
		changed, err := removeIfUnchanged(exe, seen)
		if err != nil || !changed {
			c.removed = err == nil
			return c, err
		}
		if n == frpcRechecks {
			return frpcCheck{state: exeUnknown, why: "检查的时候文件一直在变"}, nil
		}
		c.state, c.why, seen = inspectFrpc(exe)
	}
	return c, nil
}

// removeIfUnchanged 删 path，前提是它还是 seen 那个文件：同一个文件（os.SameFile，挡住改名换进来的），大小和修改时间也没变
// （挡住原地改写的）。变了就不删，changed 为 true。已经不在了算删掉了。
// 剩下的空隙是这里的 Lstat 到 os.Remove 之间，只隔一两个系统调用。
func removeIfUnchanged(path string, seen os.FileInfo) (changed bool, err error) {
	now, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if seen == nil || !os.SameFile(seen, now) || seen.Size() != now.Size() || !seen.ModTime().Equal(now.ModTime()) {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return false, nil
}
