package main

// 窗口版里的外部链接交给系统默认浏览器（3.1.1 起）。
//
// 界面上的外部链接（Minecraft EULA、申请 CurseForge Key、「设置 → 更新与关于」里的 openfrp.net、AI 回答里的链接……）
// 以前在窗口版里点开，弹出来的是 WebView2 自带的简易窗口：宿主没有接管 WebView2 的「开新窗口」请求，它就自己开一个，
// 用不了用户自己浏览器里的密码管理器、扩展和登录状态。现在前端（app.js 的 initExternalLinks）拦下这些点击和 window.open，
// 调宿主函数 hostOpenURL（main.go 里 Bind），由这里交给系统默认浏览器。
//
// 宿主函数对页面里的任何脚本都开放（go-webview2 不查消息来自哪个页面），所以地址在这里自己把关：
// 只认 http / https 的绝对地址，不能借它在本机打开程序、文件、系统设置或别的协议。

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// maxExternalURLLen 交给浏览器的地址最长多少字节。界面上的链接远用不到这么长；
// 再长的多半不是正常链接，也不往浏览器的启动命令里塞（Windows 的命令行最长 32767 个字符）。
const maxExternalURLLen = 8 << 10

// checkExternalURL 检查页面交来的地址能不能交给默认浏览器，能就原样返回。
//
// 只认 http / https 开头、带主机名的绝对地址；相对地址，还有 file:、javascript:、ms-settings: 这类
// 会在本机打开文件、程序或系统设置的协议，一律不认。地址里只能有可打印的 ASCII 字符，空格、双引号、
// 控制字符和非 ASCII 字符都不收：前端交来的是浏览器整理过的地址（a.href、new URL().href），中文和空格早已转成 %XX，
// 正常链接不会被拦；而双引号和空白是地址拼进浏览器启动命令时拆开参数的口子，看不见的字符能把地址伪装成别的样子。
func checkExternalURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("地址是空的")
	}
	if len(raw) > maxExternalURLLen {
		return "", errors.New("地址太长")
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c <= ' ' || c > '~' || c == '"' {
			return "", errors.New("地址里有空格、引号或别的不该出现的字符")
		}
	}
	u, err := url.Parse(escapeStrayPercent(raw))
	if err != nil {
		return "", errors.New("地址的格式不对")
	}
	if u.Scheme != "http" && u.Scheme != "https" { // url.Parse 已经把协议名转成小写
		return "", errors.New("只能打开 http 或 https 开头的网址")
	}
	// 「http:example.com」这种不带「//」的写法 Opaque 不为空；「http://:80/」的 Host 是「:80」，主机名却是空的
	if u.Opaque != "" || u.Hostname() == "" {
		return "", errors.New("网址里没有主机名")
	}
	return raw, nil
}

// escapeStrayPercent 把后面没跟两位十六进制数的「%」写成 %25，只给 url.Parse 检查用。
// 浏览器整理地址时不管路径、# 后面零散的「%」（比如 /50%off），a.href 里原样留着；url.Parse 却当成写错的转义整个拒收，
// 这种链接就打不开了。协议、主机名照样由 url.Parse 查，交给浏览器的还是原样的地址。
func escapeStrayPercent(s string) string {
	isHex := func(c byte) bool { return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && !(i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2])) {
			b.WriteString("%25")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// launchURL 是真正把地址交给系统的那一步。测试里换成记账的假函数，跑测试时不会真去开浏览器。
var launchURL = shellOpenURL

// openExternalURL 先检查地址：检查不过直接返回错误，页面那边的 Promise 拿到这句话；
// 检查过了就在后台交给默认浏览器，马上返回。打开失败（很少见，比如系统里没有能打开网址的程序）时写日志、调 onFail。
//
// 不在调用方的线程里打开：宿主函数是在 WebView2 处理页面消息的回调里、在界面线程上被调用的，
// ShellExecute 可能要等一会儿，还可能在里面转消息循环，放在那里会卡住窗口，WebView2 也不支持在回调里再进消息循环。
func openExternalURL(raw string, onFail func(error)) error {
	target, err := checkExternalURL(raw)
	if err != nil {
		log.Printf("不打开页面交来的地址 %.200q：%v", raw, err)
		return err
	}
	go func() {
		if err := launchURL(target); err != nil {
			err = fmt.Errorf("用默认浏览器打开 %s 失败：%w", target, err)
			log.Println(err)
			if onFail != nil {
				onFail(err)
			}
		}
	}()
	return nil
}

// shellOpenURL 用 ShellExecuteEx 的 open 把网址交给系统，系统按 http / https 的关联（默认浏览器）打开。
// 网址作为一个整体交过去，不经过 cmd、rundll32 这类会重新拆命令行的中间程序，也不按 PATH 找程序。
// 微软的文档要求调用的线程先初始化 COM（单线程套间）：它可能经 COM 交给外壳扩展去执行。
func shellOpenURL(target string) error {
	runtime.LockOSThread() // COM 的初始化跟着线程走，这段时间把 goroutine 固定在同一个线程上
	defer runtime.UnlockOSThread()
	// 这个线程以前初始化过 COM 时返回 S_FALSE（x/sys 把它当错误返回），同样要配一次 CoUninitialize；
	// 已经按别的方式初始化过（RPC_E_CHANGED_MODE）就不动它，照样能打开
	if hr := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); hr == nil || hr == syscall.Errno(windows.S_FALSE) {
		defer windows.CoUninitialize()
	}
	return shellOpen(target)
}

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo 对应 Windows 的 SHELLEXECUTEINFOW（x/sys 里没有）。字段的顺序和类型照 C 的定义写，
// 中间的对齐 Go 会照样补上；hIcon 和 hMonitor 在 C 里共用一个位置。
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

const (
	// seeMaskNoAsync 等交给外壳扩展的活儿（DDE、COM 激活）做完再返回。我们在后台线程上调、调完就撤掉 COM，
	// 不带它的话，异步的那一半可能还没走完就断了，浏览器没打开，返回的却是成功（微软的文档就是这么要求的）。
	seeMaskNoAsync = 0x00000100
	// seeMaskFlagNoUI 出错时不弹系统的对话框（没有主窗口，它会跑到别的窗口后面去），由页面上的报错提示来说。
	seeMaskFlagNoUI = 0x00000400
)

// shellOpen 用 ShellExecuteEx 的 open 打开 file，调用的线程要已经初始化好 COM（见 shellOpenURL）。
func shellOpen(file string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	f, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{fMask: seeMaskNoAsync | seeMaskFlagNoUI, lpVerb: verb, lpFile: f, nShow: windows.SW_SHOWNORMAL}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(verb)
	runtime.KeepAlive(f)
	if r != 0 {
		return nil
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return errors.New("系统没能打开它（ShellExecuteEx 失败）")
}

// openURLFailedJS 拼出告诉页面「没打开」的脚本，app.js 的 window.__mdOpenURLFailed 接着弹报错提示。
// 报错原文用 JSON 转成 JS 的字符串字面量（引号、换行、U+2028 都转义了），拼不坏脚本。
func openURLFailedJS(err error) string {
	msg, _ := json.Marshal(err.Error())
	return "window.__mdOpenURLFailed && window.__mdOpenURLFailed(" + string(msg) + ")"
}
