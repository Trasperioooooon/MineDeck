package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
func inspectExe(path string) (state exeState, why string) {
	if st, err := os.Lstat(path); err != nil {
		return exeUnknown, err.Error()
	} else if !st.Mode().IsRegular() {
		return exeUnknown, "不是普通文件"
	}
	f, err := os.Open(path)
	if err != nil {
		return exeUnknown, err.Error()
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return exeUnknown, err.Error()
	}
	size := st.Size()
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

// frpcCandidates dir 里文件名像 frpc 的 exe，按 findExe 的先后：frpc.exe、frpc_windows_amd64.exe，然后是别的。
func frpcCandidates(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var first, second, rest []string
	for _, e := range ents {
		if e.IsDir() || !isFrpcExeName(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		switch strings.ToLower(e.Name()) {
		case "frpc.exe":
			first = append(first, p)
		case "frpc_windows_amd64.exe":
			second = append(second, p)
		default:
			rest = append(rest, p)
		}
	}
	return append(append(first, second...), rest...)
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
// 只动 dir 里文件名像 frpc 的 exe（frpcCandidates），别的文件不碰。
func usableFrpc(dir string, con *procutil.Console, managed bool) (string, error) {
	var kept []string // 没删的坏文件（樱花）：都不能用时报出来
	for _, exe := range frpcCandidates(dir) {
		state, why := inspectExe(exe)
		if state == exeOK || state == exeUnknown {
			return exe, nil
		}
		name := filepath.Base(exe)
		if state == exeNotPE && !managed {
			kept = append(kept, fmt.Sprintf("%s（%s）", name, why))
			continue
		}
		if err := os.Remove(exe); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("frpc 文件夹里的 %s 是坏的（%s），又没能删掉它（%w）。请关掉可能占着它的程序，手动删掉这个文件后重试。它在这个文件夹：%s", name, why, err, dir)
		}
		if managed {
			con.Append(fmt.Sprintf("[MineDeck] 发现坏掉的 frpc：%s %s（多半是以前解压到一半出了错留下的）。已经删掉，现在重新准备", name, why))
		} else {
			con.Append(fmt.Sprintf("[MineDeck] 发现坏掉的 frpc：%s %s（多半是复制或下载到一半断了）。已经删掉，请重新下载一份放进来", name, why))
		}
	}
	if len(kept) > 0 {
		return "", fmt.Errorf("frpc 文件夹里的 %s 不能运行，可能是下载成了网页，或者下载的是压缩包、没有解开。MineDeck 没有动它；"+
			"请把它换成从服务商下载的 frpc exe 后重试。它在这个文件夹：%s", strings.Join(kept, "、"), dir)
	}
	return "", nil
}
