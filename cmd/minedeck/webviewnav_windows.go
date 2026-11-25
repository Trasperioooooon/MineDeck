//go:build windows

package main

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

type navAction int

const (
	navAllow navAction = iota
	navOpenExternal
	navCancel
)

// classifyMainFrameNavigation 裁决主窗口顶层导航：
// 同源（与 MineDeck 本地界面同 scheme/host/port、无嵌 userinfo）放行，同源但带 userinfo 的直接取消；
// 其余合法 http/https 外部链接拦下并转交系统默认浏览器；其它协议或畸形地址只拦不开。
func classifyMainFrameNavigation(raw, baseURL string) navAction {
	if raw == "" {
		return navCancel
	}
	base, err := url.Parse(baseURL)
	if err == nil {
		if matched, hasUser := matchHTTPOrigin(raw, base); matched {
			if hasUser {
				return navCancel
			}
			return navAllow
		}
	}
	if _, err := checkExternalURL(raw); err == nil {
		return navOpenExternal
	}
	return navCancel
}

func matchHTTPOrigin(raw string, base *url.URL) (matched, hasUser bool) {
	if base == nil || (!strings.EqualFold(base.Scheme, "http") && !strings.EqualFold(base.Scheme, "https")) {
		return false, false
	}
	if base.Hostname() == "" || strings.ContainsAny(raw, " \t\r\n\\\"<>") {
		return false, false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return false, false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false, false
	}
	same := strings.EqualFold(u.Scheme, base.Scheme) &&
		strings.EqualFold(u.Hostname(), base.Hostname()) &&
		effectiveURLPort(u) == effectiveURLPort(base)
	return same, u.User != nil
}

func effectiveURLPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func currentUIThreadID() uint32 { return windows.GetCurrentThreadId() }

// --- WebView2 ICoreWebView2NavigationStartingEventHandler COM 桥接 ---
// go-webview2 只封装了 NavigationCompleted 等少量事件；这里沿用 webviewsettings_windows.go 的做法，
// 通过反射取其内部 *edge.Chromium -> *edge.ICoreWebView2 及 vtbl.AddNavigationStarting / RemoveNavigationStarting。

const (
	comSOK         uintptr = 0
	comENoIface    uintptr = 0x80004002
	comEPointer    uintptr = 0x80004003
	maxNavHandlerN         = 1 << 20
)

var (
	iidIUnknown = windows.GUID{
		Data1: 0x00000000,
		Data2: 0x0000,
		Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	// {9ADBE429-F36D-432B-9DDC-F8881FBD76E3} (WebView2.h: IID_ICoreWebView2NavigationStartingEventHandler)
	iidICoreWebView2NavigationStartingEventHandler = windows.GUID{
		Data1: 0x9ADBE429,
		Data2: 0xF36D,
		Data3: 0x432B,
		Data4: [8]byte{0x9D, 0xDC, 0xF8, 0x88, 0x1F, 0xBD, 0x76, 0xE3},
	}
)

type navStartingHandlerVtbl struct {
	queryInterface edge.ComProc
	addRef         edge.ComProc
	release        edge.ComProc
	invoke         edge.ComProc
}

type navStartingHandler struct {
	vtbl *navStartingHandlerVtbl
	id   uintptr
	refs atomic.Int32
}

type navStartingEventArgs struct {
	vtbl *navStartingEventArgsVtbl
}

// ICoreWebView2NavigationStartingEventArgs COM 虚函数表顺序（IUnknown 占 0..2）。
type navStartingEventArgsVtbl struct {
	QueryInterface    edge.ComProc
	AddRef            edge.ComProc
	Release           edge.ComProc
	GetUri            edge.ComProc // [3] HRESULT get_Uri(LPWSTR* uri)
	GetIsUserInit     edge.ComProc // [4] get_IsUserInitiated
	GetIsRedirected   edge.ComProc // [5] get_IsRedirected
	GetRequestHeaders edge.ComProc // [6] get_RequestHeaders
	GetCancel         edge.ComProc // [7] get_Cancel
	PutCancel         edge.ComProc // [8] HRESULT put_Cancel(BOOL cancel)
	GetNavigationId   edge.ComProc // [9] get_NavigationId
}

var (
	navHandlersMu sync.RWMutex
	navNextID     uintptr
	navHandlers   = map[uintptr]func(args *navStartingEventArgs){}

	// 全局只生成 4 个 OS 回调跳板，避免测试或多次建窗触发 Go 的全局回调槽位上限。
	navStartingVtbl = navStartingHandlerVtbl{
		queryInterface: edge.NewComProc(navHandlerQueryInterface),
		addRef:         edge.NewComProc(navHandlerAddRef),
		release:        edge.NewComProc(navHandlerRelease),
		invoke:         edge.NewComProc(navHandlerInvoke),
	}
)

func navHandlerQueryInterface(this *navStartingHandler, refiid *windows.GUID, ppvObject *uintptr) uintptr {
	defer func() { _ = recover() }()
	if ppvObject == nil {
		return comEPointer
	}
	if this == nil || refiid == nil {
		*ppvObject = 0
		return comEPointer
	}
	if *refiid == iidIUnknown || *refiid == iidICoreWebView2NavigationStartingEventHandler {
		this.refs.Add(1)
		*ppvObject = uintptr(unsafe.Pointer(this))
		return comSOK
	}
	*ppvObject = 0
	return comENoIface
}

func navHandlerAddRef(this *navStartingHandler) uintptr {
	defer func() { _ = recover() }()
	if this == nil {
		return 0
	}
	return uintptr(this.refs.Add(1))
}

func navHandlerRelease(this *navStartingHandler) uintptr {
	defer func() { _ = recover() }()
	if this == nil {
		return 0
	}
	// 实际内存由调用方用 runtime.Pinner 固定，直到 RemoveNavigationStarting 后再 Unpin。
	if n := this.refs.Add(-1); n > 0 {
		return uintptr(n)
	}
	return 0
}

func navHandlerInvoke(this *navStartingHandler, _ unsafe.Pointer, args *navStartingEventArgs) uintptr {
	defer func() { _ = recover() }()
	if this == nil || args == nil {
		return comSOK
	}
	navHandlersMu.RLock()
	fn := navHandlers[this.id]
	navHandlersMu.RUnlock()
	if fn != nil {
		fn(args)
	}
	return comSOK
}

func readNavURI(args *navStartingEventArgs) (string, bool) {
	if args == nil || args.vtbl == nil || args.vtbl.GetUri == 0 {
		return "", false
	}
	var uriPtr *uint16
	r, _, _ := args.vtbl.GetUri.Call(uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&uriPtr)))
	if int32(r) < 0 || uriPtr == nil {
		return "", false
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(uriPtr))
	return windows.UTF16PtrToString(uriPtr), true
}

func cancelNav(args *navStartingEventArgs) {
	if args == nil || args.vtbl == nil || args.vtbl.PutCancel == 0 {
		return
	}
	_, _, _ = args.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(args)), 1)
}

func extractCoreWebView2(w webview2.WebView) (coreObj uintptr, addProc, remProc edge.ComProc, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("反射读取 WebView2 对象失败：%v", r)
		}
	}()
	if w == nil {
		return 0, 0, 0, errors.New("WebView 实例为空")
	}
	wv := reflect.ValueOf(w)
	if wv.Kind() != reflect.Pointer || wv.IsNil() || wv.Elem().Kind() != reflect.Struct {
		return 0, 0, 0, errors.New("意外的 WebView 实现类型")
	}
	bf := wv.Elem().FieldByName("browser")
	if !bf.IsValid() || bf.Kind() != reflect.Interface || bf.IsNil() {
		return 0, 0, 0, errors.New("找不到 browser 字段")
	}
	chromium := bf.Elem()
	if chromium.Type() != reflect.TypeFor[*edge.Chromium]() || chromium.IsNil() {
		return 0, 0, 0, errors.New("go-webview2 browser 类型不兼容")
	}
	wf := chromium.Elem().FieldByName("webview")
	if !wf.IsValid() || wf.Type() != reflect.TypeFor[*edge.ICoreWebView2]() || wf.IsNil() {
		return 0, 0, 0, errors.New("找不到 *edge.ICoreWebView2 指针")
	}
	vf := wf.Elem().FieldByName("vtbl")
	if !vf.IsValid() || vf.Kind() != reflect.Pointer || vf.IsNil() || vf.Elem().Kind() != reflect.Struct {
		return 0, 0, 0, errors.New("找不到 ICoreWebView2 vtbl")
	}
	vt := vf.Elem()
	addF := vt.FieldByName("AddNavigationStarting")
	remF := vt.FieldByName("RemoveNavigationStarting")
	comProcType := reflect.TypeFor[edge.ComProc]()
	if !addF.IsValid() || !remF.IsValid() || addF.Type() != comProcType || remF.Type() != comProcType {
		return 0, 0, 0, errors.New("go-webview2 vtbl 缺少 NavigationStarting 方法槽")
	}
	coreObj = uintptr(wf.UnsafePointer())
	addProc = edge.ComProc(addF.Uint())
	remProc = edge.ComProc(remF.Uint())
	if coreObj == 0 || addProc == 0 || remProc == 0 {
		return 0, 0, 0, errors.New("ICoreWebView2 或导航事件函数指针为空")
	}
	return coreObj, addProc, remProc, nil
}

func registerNavCallback(fn func(*navStartingEventArgs)) uintptr {
	navHandlersMu.Lock()
	defer navHandlersMu.Unlock()
	for {
		navNextID = (navNextID % maxNavHandlerN) + 1
		if _, used := navHandlers[navNextID]; !used {
			navHandlers[navNextID] = fn
			return navNextID
		}
	}
}

func installWebViewNavHandler(w webview2.WebView, onNavigate func(args *navStartingEventArgs)) (func(), error) {
	coreObj, addProc, remProc, err := extractCoreWebView2(w)
	if err != nil {
		return nil, err
	}
	id := registerNavCallback(onNavigate)
	vtblCopy := new(navStartingHandlerVtbl)
	*vtblCopy = navStartingVtbl
	handler := &navStartingHandler{
		vtbl: vtblCopy,
		id:   id,
	}
	handler.refs.Store(1)

	var pinner runtime.Pinner
	pinner.Pin(vtblCopy)
	pinner.Pin(handler)

	var token int64
	r, _, _ := addProc.Call(coreObj, uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(&token)))
	if int32(r) < 0 {
		navHandlersMu.Lock()
		delete(navHandlers, id)
		navHandlersMu.Unlock()
		pinner.Unpin()
		return nil, fmt.Errorf("AddNavigationStarting 失败：HRESULT=0x%08X", uint32(r))
	}

	return sync.OnceFunc(func() {
		_, _, _ = remProc.Call(coreObj, uintptr(token))
		navHandlersMu.Lock()
		delete(navHandlers, id)
		navHandlersMu.Unlock()
		pinner.Unpin()
	}), nil
}

func installWebViewNavGuard(w webview2.WebView, appURL string, onFail func(error)) (func(), error) {
	return installWebViewNavHandler(w, func(args *navStartingEventArgs) {
		raw, ok := readNavURI(args)
		if !ok {
			cancelNav(args)
			return
		}
		switch classifyMainFrameNavigation(raw, appURL) {
		case navAllow:
			return
		case navOpenExternal:
			cancelNav(args)
			_ = openExternalURL(raw, onFail)
		default:
			cancelNav(args)
		}
	})
}
