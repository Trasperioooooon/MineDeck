package music

// COM 接口的手工绑定：每个接口指针指向一个「首字段为 vtable 指针」的对象，方法按 vtable 下标调用。
// vtable 顺序照 Windows SDK 头文件（mmdeviceapi.h、audioclient.h、audiopolicy.h、endpointvolume.h、propsys.h）。
//
// 所有调用都直接写在 syscall.SyscallN 的参数里做 uintptr(unsafe.Pointer(…)) 转换，
// 不经过中间函数转手 uintptr，否则 Go 栈搬迁后传给系统的地址会失效。
// 只支持 64 位：REFERENCE_TIME（int64）按一个参数槽传递。

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modole32                          = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx                = modole32.NewProc("CoInitializeEx")
	procCoUninitialize                = modole32.NewProc("CoUninitialize")
	procCoCreateInstance              = modole32.NewProc("CoCreateInstance")
	procCoCreateFreeThreadedMarshaler = modole32.NewProc("CoCreateFreeThreadedMarshaler")
	procPropVariantClear              = modole32.NewProc("PropVariantClear")

	modmmdevapi                     = windows.NewLazySystemDLL("mmdevapi.dll")
	procActivateAudioInterfaceAsync = modmmdevapi.NewProc("ActivateAudioInterfaceAsync")

	modavrt                             = windows.NewLazySystemDLL("avrt.dll")
	procAvSetMmThreadCharacteristicsW   = modavrt.NewProc("AvSetMmThreadCharacteristicsW")
	procAvRevertMmThreadCharacteristics = modavrt.NewProc("AvRevertMmThreadCharacteristics")
)

// hresult 是 COM 调用的返回值；最高位为 1 表示失败。
type hresult uint32

const (
	sOK                 hresult = 0x00000000
	eNoInterface        hresult = 0x80004002
	ePointer            hresult = 0x80004003
	eInvalidArg         hresult = 0x80070057
	audclntSBufferEmpty hresult = 0x08890001
)

func (h hresult) failed() bool   { return h&0x80000000 != 0 }
func (h hresult) String() string { return HRText(uint32(h)) }

// err 在失败时返回带步骤名的 *HRError，成功时返回 nil。
func (h hresult) err(step string) error {
	if h.failed() {
		return &HRError{Step: step, Code: uint32(h)}
	}
	return nil
}

const (
	coinitMultithreaded = 0x0
	clsctxAll           = 0x17 // CLSCTX_INPROC_SERVER | INPROC_HANDLER | LOCAL_SERVER | REMOTE_SERVER
	eRender             = 0
	eCapture            = 1
	eConsole            = 0
	deviceStateActive   = 0x1
	stgmRead            = 0x0
	shareModeShared     = 0
	vtLPWSTR            = 31
	vtBlob              = 65

	audioSessionStateActive  = 1
	audioSessionStateExpired = 2

	activationTypeProcessLoopback = 1 // AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK
	loopbackModeIncludeTree       = 0 // PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE

	// virtualAudioDeviceProcessLoopback 是按进程环回的虚拟设备路径（mmdeviceapi.h 的 VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK）。
	virtualAudioDeviceProcessLoopback = `VAD\Process_Loopback`
)

func guid(d1 uint32, d2, d3 uint16, d4 ...byte) windows.GUID {
	return windows.GUID{Data1: d1, Data2: d2, Data3: d3, Data4: [8]byte(d4)}
}

var (
	clsidMMDeviceEnumerator   = guid(0xBCDE0395, 0xE52F, 0x467C, 0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E)
	iidIUnknown               = guid(0x00000000, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46)
	iidIMarshal               = guid(0x00000003, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46)
	iidIAgileObject           = guid(0x94EA2B94, 0xE9CC, 0x49E0, 0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90)
	iidIMMDeviceEnumerator    = guid(0xA95664D2, 0x9614, 0x4F35, 0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6)
	iidIAudioClient           = guid(0x1CB9AD4C, 0xDBFA, 0x4C32, 0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2)
	iidIAudioCaptureClient    = guid(0xC8ADBD64, 0xE71E, 0x48A0, 0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17)
	iidIAudioSessionManager2  = guid(0x77AA99A0, 0x1BD6, 0x484F, 0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F)
	iidIAudioSessionControl2  = guid(0xBFB7FF88, 0x7239, 0x4FC9, 0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D)
	iidIAudioMeterInformation = guid(0xC02216F6, 0x8C67, 0x4B5B, 0x9D, 0x00, 0xD0, 0x08, 0xE7, 0x3E, 0x00, 0x64)
	iidIActivateCompletion    = guid(0x41D949AB, 0x9862, 0x444A, 0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB) // IActivateAudioInterfaceCompletionHandler

	pkeyDeviceFriendlyName = propertyKey{guid(0xA45C254E, 0xDF1C, 0x4EFD, 0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0), 14}
)

// iidNames 用于记录系统向回调对象查询了哪些接口。
var iidNames = map[windows.GUID]string{
	iidIUnknown:            "IUnknown",
	iidIActivateCompletion: "IActivateAudioInterfaceCompletionHandler",
	iidIAgileObject:        "IAgileObject",
	iidIMarshal:            "IMarshal",
	guid(0xECC8691B, 0xC1DB, 0x4DC0, 0x85, 0x5E, 0x65, 0xF6, 0xC5, 0x51, 0xAF, 0x49): "INoMarshal",
	guid(0x00000018, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46): "IStdMarshalInfo",
	guid(0x00000019, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46): "IExternalConnection",
	guid(0x00000038, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46): "IWeakReferenceSource",
	guid(0xAF86E2E0, 0xB12D, 0x4C6A, 0x9C, 0x5A, 0xD7, 0xAA, 0x65, 0x10, 0x1E, 0x90): "IInspectable",
}

func iidName(g windows.GUID) string {
	if n, ok := iidNames[g]; ok {
		return n
	}
	return g.String()
}

// propertyKey 即 PROPERTYKEY。
type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// propVariantBlob 是 vt = VT_BLOB 的 PROPVARIANT（64 位下 24 字节，pBlobData 在偏移 16）。
type propVariantBlob struct {
	vt        uint16
	reserved  [3]uint16
	cbSize    uint32
	pBlobData unsafe.Pointer
}

// propVariantStr 是 vt = VT_LPWSTR 的 PROPVARIANT。只用来读设备名，用完要 PropVariantClear。
type propVariantStr struct {
	vt       uint16
	reserved [3]uint16
	str      *uint16
	_        uintptr
}

// activationParams 即 AUDIOCLIENT_ACTIVATION_PARAMS（12 字节）。
type activationParams struct {
	activationType uint32
	targetPID      uint32
	loopbackMode   uint32
}

// coInitializeEx 以多线程套间（MTA）初始化当前线程的 COM；S_FALSE（已初始化过）也算成功，同样要配对 CoUninitialize。
func coInitializeEx() hresult {
	r, _, _ := syscall.SyscallN(procCoInitializeEx.Addr(), 0, coinitMultithreaded)
	return hresult(r)
}

func coUninitialize() { syscall.SyscallN(procCoUninitialize.Addr()) }

func coCreateInstance(clsid, iid *windows.GUID, out unsafe.Pointer) hresult {
	r, _, _ := syscall.SyscallN(procCoCreateInstance.Addr(),
		uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(out))
	return hresult(r)
}

// coTaskString 读出系统用 CoTaskMemAlloc 分配的宽字符串并释放它。
func coTaskString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// withCOM 在一个新的、锁定的系统线程上初始化 COM（MTA）后运行 fn。
// 不解锁线程：goroutine 结束时 Go 连同线程一起结束，不把初始化过 COM 的线程还给调度器。
func withCOM(fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if hr := coInitializeEx(); hr.failed() {
			errc <- hr.err("CoInitializeEx")
			return
		}
		err := fn()
		coUninitialize()
		errc <- err
	}()
	return <-errc
}

// ---- IUnknown ----

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type iUnknown struct{ vtbl *iUnknownVtbl }

func (u *iUnknown) QueryInterface(iid *windows.GUID, out unsafe.Pointer) hresult {
	r, _, _ := syscall.SyscallN(u.vtbl.QueryInterface, uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(iid)), uintptr(out))
	return hresult(r)
}

func (u *iUnknown) Release() uint32 {
	r, _, _ := syscall.SyscallN(u.vtbl.Release, uintptr(unsafe.Pointer(u)))
	return uint32(r)
}

// release 释放任意 COM 接口指针（nil 跳过）。每个接口的 vtable 都以 IUnknown 的三个方法开头。
func release[T any](p *T) {
	if p != nil {
		(*iUnknown)(unsafe.Pointer(p)).Release()
	}
}

// queryInterface 在任意 COM 接口指针上调用 QueryInterface。
func queryInterface[T any](p *T, iid *windows.GUID, out unsafe.Pointer) hresult {
	return (*iUnknown)(unsafe.Pointer(p)).QueryInterface(iid, out)
}

// ---- IMMDeviceEnumerator / IMMDeviceCollection / IMMDevice / IPropertyStore ----

type iMMDeviceEnumerator struct {
	vtbl *struct {
		iUnknownVtbl
		EnumAudioEndpoints                     uintptr
		GetDefaultAudioEndpoint                uintptr
		GetDevice                              uintptr
		RegisterEndpointNotificationCallback   uintptr
		UnregisterEndpointNotificationCallback uintptr
	}
}

func (e *iMMDeviceEnumerator) EnumAudioEndpoints(flow, stateMask uint32, out **iMMDeviceCollection) hresult {
	r, _, _ := syscall.SyscallN(e.vtbl.EnumAudioEndpoints, uintptr(unsafe.Pointer(e)), uintptr(flow), uintptr(stateMask), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

func (e *iMMDeviceEnumerator) GetDefaultAudioEndpoint(flow, role uint32, out **iMMDevice) hresult {
	r, _, _ := syscall.SyscallN(e.vtbl.GetDefaultAudioEndpoint, uintptr(unsafe.Pointer(e)), uintptr(flow), uintptr(role), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

// GetDevice 按设备 ID 打开设备。
func (e *iMMDeviceEnumerator) GetDevice(id string, out **iMMDevice) hresult {
	p, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return eInvalidArg
	}
	r, _, _ := syscall.SyscallN(e.vtbl.GetDevice, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(out)))
	runtime.KeepAlive(p)
	return hresult(r)
}

type iMMDeviceCollection struct {
	vtbl *struct {
		iUnknownVtbl
		GetCount uintptr
		Item     uintptr
	}
}

func (c *iMMDeviceCollection) GetCount(n *uint32) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.GetCount, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(n)))
	return hresult(r)
}

func (c *iMMDeviceCollection) Item(i uint32, out **iMMDevice) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.Item, uintptr(unsafe.Pointer(c)), uintptr(i), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

type iMMDevice struct {
	vtbl *struct {
		iUnknownVtbl
		Activate          uintptr
		OpenPropertyStore uintptr
		GetId             uintptr
		GetState          uintptr
	}
}

func (d *iMMDevice) Activate(iid *windows.GUID, out unsafe.Pointer) hresult {
	r, _, _ := syscall.SyscallN(d.vtbl.Activate, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(iid)), clsctxAll, 0, uintptr(out))
	return hresult(r)
}

func (d *iMMDevice) OpenPropertyStore(access uint32, out **iPropertyStore) hresult {
	r, _, _ := syscall.SyscallN(d.vtbl.OpenPropertyStore, uintptr(unsafe.Pointer(d)), uintptr(access), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

// ID 返回设备 ID 字符串；取不到时为空。
func (d *iMMDevice) ID() string {
	var p *uint16
	r, _, _ := syscall.SyscallN(d.vtbl.GetId, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&p)))
	if hresult(r).failed() {
		return ""
	}
	return coTaskString(p)
}

// FriendlyName 返回设备在声音设置里显示的名字；取不到时为空。
func (d *iMMDevice) FriendlyName() string {
	var store *iPropertyStore
	if hr := d.OpenPropertyStore(stgmRead, &store); hr.failed() || store == nil {
		return ""
	}
	defer release(store)
	pv := new(propVariantStr)
	if hr := store.GetValue(&pkeyDeviceFriendlyName, pv); hr.failed() {
		return ""
	}
	name := ""
	if pv.vt == vtLPWSTR && pv.str != nil {
		name = windows.UTF16PtrToString(pv.str)
	}
	syscall.SyscallN(procPropVariantClear.Addr(), uintptr(unsafe.Pointer(pv)))
	return name
}

type iPropertyStore struct {
	vtbl *struct {
		iUnknownVtbl
		GetCount uintptr
		GetAt    uintptr
		GetValue uintptr
		SetValue uintptr
		Commit   uintptr
	}
}

func (s *iPropertyStore) GetValue(key *propertyKey, pv *propVariantStr) hresult {
	r, _, _ := syscall.SyscallN(s.vtbl.GetValue, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(pv)))
	return hresult(r)
}

// ---- IAudioClient / IAudioCaptureClient ----

type iAudioClient struct {
	vtbl *struct {
		iUnknownVtbl
		Initialize        uintptr
		GetBufferSize     uintptr
		GetStreamLatency  uintptr
		GetCurrentPadding uintptr
		IsFormatSupported uintptr
		GetMixFormat      uintptr
		GetDevicePeriod   uintptr
		Start             uintptr
		Stop              uintptr
		Reset             uintptr
		SetEventHandle    uintptr
		GetService        uintptr
	}
}

// Initialize 以共享模式初始化；bufferDuration 以 100 纳秒为单位，事件驱动时 periodicity 必须为 0。
func (c *iAudioClient) Initialize(flags uint32, bufferDuration int64, format []byte) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.Initialize, uintptr(unsafe.Pointer(c)),
		shareModeShared, uintptr(flags), uintptr(bufferDuration), 0, uintptr(unsafe.Pointer(&format[0])), 0)
	return hresult(r)
}

func (c *iAudioClient) GetBufferSize(n *uint32) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.GetBufferSize, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(n)))
	return hresult(r)
}

// GetMixFormat 返回设备混音格式的字节（复制后释放系统分配的内存）。WAVEFORMATEXTENSIBLE 只保留标准的
// 40 字节并把 cbSize 改成 22，之后原样传回 Initialize 时系统不会读到缓冲区外。
func (c *iAudioClient) GetMixFormat() ([]byte, hresult) {
	var p *byte
	r, _, _ := syscall.SyscallN(c.vtbl.GetMixFormat, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&p)))
	hr := hresult(r)
	if hr.failed() || p == nil {
		return nil, hr
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	head := unsafe.Slice(p, 18)
	n := 18
	if binary.LittleEndian.Uint16(head) == waveFormatExtensible && binary.LittleEndian.Uint16(head[16:]) >= 22 {
		n = 40
	}
	b := append([]byte(nil), unsafe.Slice(p, n)...)
	binary.LittleEndian.PutUint16(b[16:], uint16(n-18))
	return b, hr
}

func (c *iAudioClient) Start() hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.Start, uintptr(unsafe.Pointer(c)))
	return hresult(r)
}

func (c *iAudioClient) Stop() hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.Stop, uintptr(unsafe.Pointer(c)))
	return hresult(r)
}

func (c *iAudioClient) SetEventHandle(h windows.Handle) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.SetEventHandle, uintptr(unsafe.Pointer(c)), uintptr(h))
	return hresult(r)
}

func (c *iAudioClient) GetService(iid *windows.GUID, out unsafe.Pointer) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.GetService, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(iid)), uintptr(out))
	return hresult(r)
}

type iAudioCaptureClient struct {
	vtbl *struct {
		iUnknownVtbl
		GetBuffer         uintptr
		ReleaseBuffer     uintptr
		GetNextPacketSize uintptr
	}
}

func (c *iAudioCaptureClient) GetBuffer(data **byte, frames, flags *uint32, devPos, qpcPos *uint64) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.GetBuffer, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(frames)), uintptr(unsafe.Pointer(flags)), uintptr(unsafe.Pointer(devPos)), uintptr(unsafe.Pointer(qpcPos)))
	return hresult(r)
}

func (c *iAudioCaptureClient) ReleaseBuffer(frames uint32) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.ReleaseBuffer, uintptr(unsafe.Pointer(c)), uintptr(frames))
	return hresult(r)
}

func (c *iAudioCaptureClient) GetNextPacketSize(n *uint32) hresult {
	r, _, _ := syscall.SyscallN(c.vtbl.GetNextPacketSize, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(n)))
	return hresult(r)
}

// ---- 音频会话：IAudioSessionManager2 / IAudioSessionEnumerator / IAudioSessionControl2 / IAudioMeterInformation ----

type iAudioSessionManager2 struct {
	vtbl *struct {
		iUnknownVtbl
		GetAudioSessionControl        uintptr
		GetSimpleAudioVolume          uintptr
		GetSessionEnumerator          uintptr
		RegisterSessionNotification   uintptr
		UnregisterSessionNotification uintptr
		RegisterDuckNotification      uintptr
		UnregisterDuckNotification    uintptr
	}
}

func (m *iAudioSessionManager2) GetSessionEnumerator(out **iAudioSessionEnumerator) hresult {
	r, _, _ := syscall.SyscallN(m.vtbl.GetSessionEnumerator, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

type iAudioSessionEnumerator struct {
	vtbl *struct {
		iUnknownVtbl
		GetCount   uintptr
		GetSession uintptr
	}
}

func (e *iAudioSessionEnumerator) GetCount(n *int32) hresult {
	r, _, _ := syscall.SyscallN(e.vtbl.GetCount, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(n)))
	return hresult(r)
}

// GetSession 返回的是 IAudioSessionControl，要再 QueryInterface 成 IAudioSessionControl2。
func (e *iAudioSessionEnumerator) GetSession(i int32, out **iUnknown) hresult {
	r, _, _ := syscall.SyscallN(e.vtbl.GetSession, uintptr(unsafe.Pointer(e)), uintptr(i), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

type iAudioSessionControl2 struct {
	vtbl *struct {
		iUnknownVtbl
		// IAudioSessionControl
		GetState                           uintptr
		GetDisplayName                     uintptr
		SetDisplayName                     uintptr
		GetIconPath                        uintptr
		SetIconPath                        uintptr
		GetGroupingParam                   uintptr
		SetGroupingParam                   uintptr
		RegisterAudioSessionNotification   uintptr
		UnregisterAudioSessionNotification uintptr
		// IAudioSessionControl2
		GetSessionIdentifier         uintptr
		GetSessionInstanceIdentifier uintptr
		GetProcessId                 uintptr
		IsSystemSoundsSession        uintptr
		SetDuckingPreference         uintptr
	}
}

func (s *iAudioSessionControl2) GetState(state *uint32) hresult {
	r, _, _ := syscall.SyscallN(s.vtbl.GetState, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(state)))
	return hresult(r)
}

func (s *iAudioSessionControl2) DisplayName() string {
	var p *uint16
	r, _, _ := syscall.SyscallN(s.vtbl.GetDisplayName, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&p)))
	if hresult(r).failed() {
		return ""
	}
	return coTaskString(p)
}

// GetProcessId 在会话跨多个进程时返回 AUDCLNT_S_NO_SINGLE_PROCESS（成功码），pid 为发起会话的进程。
func (s *iAudioSessionControl2) GetProcessId(pid *uint32) hresult {
	r, _, _ := syscall.SyscallN(s.vtbl.GetProcessId, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(pid)))
	return hresult(r)
}

// IsSystemSoundsSession 对系统声音会话返回 S_OK，其余返回 S_FALSE。
func (s *iAudioSessionControl2) IsSystemSoundsSession() hresult {
	r, _, _ := syscall.SyscallN(s.vtbl.IsSystemSoundsSession, uintptr(unsafe.Pointer(s)))
	return hresult(r)
}

type iAudioMeterInformation struct {
	vtbl *struct {
		iUnknownVtbl
		GetPeakValue uintptr
		// 其后还有 GetMeteringChannelCount、GetChannelsPeakValues、QueryHardwareSupport，用不到
	}
}

func (m *iAudioMeterInformation) GetPeakValue(peak *float32) hresult {
	r, _, _ := syscall.SyscallN(m.vtbl.GetPeakValue, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(peak)))
	return hresult(r)
}

// ---- ActivateAudioInterfaceAsync 与完成回调对象 ----

type iActivateAsyncOperation struct {
	vtbl *struct {
		iUnknownVtbl
		GetActivateResult uintptr
	}
}

func (o *iActivateAsyncOperation) GetActivateResult(activateHR *hresult, out **iUnknown) hresult {
	r, _, _ := syscall.SyscallN(o.vtbl.GetActivateResult, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(activateHR)), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

// completionHandler 是传给 ActivateAudioInterfaceAsync 的完成回调对象（IActivateAudioInterfaceCompletionHandler），
// 用 Go 搭的 COM 对象：首字段是 vtable 指针，四个方法由 syscall.NewCallback 生成。
//
// 系统在自己的 MTA 工作线程上调用 ActivateCompleted（Go 支持在非 Go 线程上进入回调）。函数文档说回调对象
// 必须是敏捷对象（实现 IAgileObject），否则会报 E_ILLEGAL_METHOD_CALL（原文写的是 Win10 以前的系统）。
// 微软示例和 OBS 用 WRL 的 FtmBase（应答 IAgileObject，并聚合自由线程封送器应答 IMarshal），
// miniaudio 只应答 IAgileObject。这里默认应答 IUnknown / 回调接口 / IAgileObject；
// useFTM 时再聚合自由线程封送器（CoCreateFreeThreadedMarshaler）应答 IMarshal，作为备用写法。
// 对象的方法都可在任意线程并发调用（引用计数用原子操作），声明敏捷是如实的。
type completionHandler struct {
	vtbl *completionHandlerVtbl // 必须是第一个字段：COM 指针就是这个对象的地址
	refs int32
	done chan struct{} // ActivateCompleted 被调用后关闭
	once sync.Once
	ftm  *iUnknown // 聚合的自由线程封送器（内部 IUnknown），仅 useFTM 时有

	// ActivateAudioInterfaceAsync 的参数放在对象里，保证异步操作期间一直有效
	params activationParams
	pv     propVariantBlob

	mu      sync.Mutex
	queried []qiRecord // 系统查询过的接口，排查用
}

type completionHandlerVtbl struct {
	iUnknownVtbl
	ActivateCompleted uintptr
}

type qiRecord struct {
	iid windows.GUID
	hr  hresult
}

var (
	handlerVtblOnce sync.Once
	handlerVtbl     *completionHandlerVtbl
	// liveHandlers 让系统还持有引用的回调对象一直可达，GC 不会回收；引用计数归零时删除。
	// 回调里也靠它从 this 指针找回 Go 对象，免得把 uintptr 转回指针。
	liveHandlers sync.Map // uintptr → *completionHandler
)

func newCompletionHandler(useFTM bool) (*completionHandler, error) {
	handlerVtblOnce.Do(func() {
		// NewCallback 生成的回调永不释放、总数有上限，所以只建一次，所有对象共用一张 vtable
		handlerVtbl = &completionHandlerVtbl{
			iUnknownVtbl: iUnknownVtbl{
				QueryInterface: syscall.NewCallback(handlerQueryInterface),
				AddRef:         syscall.NewCallback(handlerAddRef),
				Release:        syscall.NewCallback(handlerRelease),
			},
			ActivateCompleted: syscall.NewCallback(handlerActivateCompleted),
		}
	})
	h := &completionHandler{vtbl: handlerVtbl, refs: 1, done: make(chan struct{})}
	liveHandlers.Store(h.ptr(), h)
	if useFTM {
		r, _, _ := syscall.SyscallN(procCoCreateFreeThreadedMarshaler.Addr(), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&h.ftm)))
		if hr := hresult(r); hr.failed() {
			h.release()
			return nil, hr.err("CoCreateFreeThreadedMarshaler")
		}
	}
	return h, nil
}

func (h *completionHandler) ptr() uintptr { return uintptr(unsafe.Pointer(h)) }

func (h *completionHandler) addRef() uint32 { return uint32(atomic.AddInt32(&h.refs, 1)) }

func (h *completionHandler) release() uint32 {
	n := atomic.AddInt32(&h.refs, -1)
	if n == 0 {
		if h.ftm != nil {
			h.ftm.Release() // 聚合规则：释放内部对象不会回调外部的 Release
		}
		liveHandlers.Delete(h.ptr())
	}
	return uint32(n)
}

// queriedText 说明系统向回调对象查询过哪些接口、我们怎么答的。
func (h *completionHandler) queriedText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queried) == 0 {
		return "系统没有向回调对象查询接口"
	}
	parts := make([]string, len(h.queried))
	for i, q := range h.queried {
		parts[i] = fmt.Sprintf("%s → %s", iidName(q.iid), q.hr)
	}
	return "系统向回调对象查询了：" + strings.Join(parts, "；")
}

func lookupHandler(this uintptr) *completionHandler {
	if v, ok := liveHandlers.Load(this); ok {
		return v.(*completionHandler)
	}
	return nil
}

func handlerQueryInterface(this uintptr, iid *windows.GUID, out *uintptr) uintptr {
	if out == nil {
		return uintptr(ePointer)
	}
	*out = 0
	h := lookupHandler(this)
	if h == nil || iid == nil {
		return uintptr(eNoInterface)
	}
	hr := eNoInterface
	switch *iid {
	case iidIUnknown, iidIActivateCompletion, iidIAgileObject:
		h.addRef()
		*out = this
		hr = sOK
	case iidIMarshal:
		if h.ftm != nil { // 交给聚合的自由线程封送器；它会通过我们的 AddRef 增加引用
			hr = h.ftm.QueryInterface(iid, unsafe.Pointer(out))
		}
	}
	h.mu.Lock()
	h.queried = append(h.queried, qiRecord{*iid, hr})
	h.mu.Unlock()
	return uintptr(hr)
}

func handlerAddRef(this uintptr) uintptr {
	if h := lookupHandler(this); h != nil {
		return uintptr(h.addRef())
	}
	return 1
}

func handlerRelease(this uintptr) uintptr {
	if h := lookupHandler(this); h != nil {
		return uintptr(h.release())
	}
	return 0
}

// handlerActivateCompleted 只通知等待方；激活结果由等待方在自己的线程上用 GetActivateResult 取（同 miniaudio）。
func handlerActivateCompleted(this, op uintptr) uintptr {
	if h := lookupHandler(this); h != nil {
		h.once.Do(func() { close(h.done) })
	}
	return uintptr(sOK)
}

// ---- MMCSS ----

// avSetMmThreadCharacteristics 把当前线程登记为「Audio」任务，提高调度优先级，减少爆音；失败不影响采集。
func avSetMmThreadCharacteristics(task string) (uintptr, error) {
	if err := procAvSetMmThreadCharacteristicsW.Find(); err != nil {
		return 0, err
	}
	name, err := windows.UTF16PtrFromString(task)
	if err != nil {
		return 0, err
	}
	var index uint32
	r, _, e := syscall.SyscallN(procAvSetMmThreadCharacteristicsW.Addr(), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&index)))
	if r == 0 {
		return 0, e
	}
	return r, nil
}

func avRevertMmThreadCharacteristics(h uintptr) {
	if procAvRevertMmThreadCharacteristics.Find() == nil {
		syscall.SyscallN(procAvRevertMmThreadCharacteristics.Addr(), h)
	}
}
