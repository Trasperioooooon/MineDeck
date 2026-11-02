package music

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 结构体布局必须与 C 头文件一致（64 位）。
func TestLayouts(t *testing.T) {
	var pv propVariantBlob
	var ps propVariantStr
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(PROPVARIANT/VT_BLOB)", unsafe.Sizeof(pv), 24},
		{"offsetof(blob.cbSize)", unsafe.Offsetof(pv.cbSize), 8},
		{"offsetof(blob.pBlobData)", unsafe.Offsetof(pv.pBlobData), 16},
		{"sizeof(PROPVARIANT/VT_LPWSTR)", unsafe.Sizeof(ps), 24},
		{"offsetof(pwszVal)", unsafe.Offsetof(ps.str), 8},
		{"sizeof(AUDIOCLIENT_ACTIVATION_PARAMS)", unsafe.Sizeof(activationParams{}), 12},
		{"sizeof(PROPERTYKEY)", unsafe.Sizeof(propertyKey{}), 20},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d，应为 %d", c.name, c.got, c.want)
		}
	}
}

// qi 经回调对象自己的 vtable 调 QueryInterface，走的是系统调用它时的同一条路。
func qi(h *completionHandler, iid *windows.GUID, out *uintptr) hresult {
	r, _, _ := syscall.SyscallN(h.vtbl.QueryInterface, h.ptr(), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out)))
	return hresult(r)
}

func vtblRelease(h *completionHandler) uint32 {
	r, _, _ := syscall.SyscallN(h.vtbl.Release, h.ptr())
	return uint32(r)
}

// 回调对象应答 IUnknown、完成回调接口和 IAgileObject（返回自身并加引用），其余接口一律 E_NOINTERFACE；
// 引用计数归零后从存活表里删除。
func TestHandlerQueryInterface(t *testing.T) {
	h, err := newCompletionHandler(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, iid := range []windows.GUID{iidIUnknown, iidIActivateCompletion, iidIAgileObject} {
		var out uintptr
		if hr := qi(h, &iid, &out); hr != sOK || out != h.ptr() {
			t.Errorf("QueryInterface(%s) = %s，out=%#x，应为 S_OK 并返回自身", iidName(iid), hr, out)
		}
		if n := vtblRelease(h); n != 1 {
			t.Errorf("Release 后引用计数 = %d，应为 1", n)
		}
	}
	for _, iid := range []windows.GUID{iidIMarshal, iidIAudioClient} {
		out := uintptr(1)
		if hr := qi(h, &iid, &out); hr != eNoInterface || out != 0 {
			t.Errorf("QueryInterface(%s) = %s，out=%#x，应为 E_NOINTERFACE 并置空", iidName(iid), hr, out)
		}
	}
	if text := h.queriedText(); !strings.Contains(text, "IAgileObject → 0x00000000 S_OK") || !strings.Contains(text, "IMarshal → 0x80004002") {
		t.Errorf("查询记录不对：%s", text)
	}
	if n := h.release(); n != 0 {
		t.Errorf("最后一次 Release 后引用计数 = %d", n)
	}
	if lookupHandler(h.ptr()) != nil {
		t.Error("引用计数归零后仍在存活表里")
	}
}

// 聚合自由线程封送器后，IMarshal 交给它应答；它通过我们的 AddRef/Release 计数。
func TestHandlerFTM(t *testing.T) {
	err := withCOM(func() error {
		h, err := newCompletionHandler(true)
		if err != nil {
			return err
		}
		var m *iUnknown
		r, _, _ := syscall.SyscallN(h.vtbl.QueryInterface, h.ptr(), uintptr(unsafe.Pointer(&iidIMarshal)), uintptr(unsafe.Pointer(&m)))
		if hr := hresult(r); hr != sOK || m == nil {
			t.Errorf("聚合 FTM 后 QueryInterface(IMarshal) = %s", hr)
		} else {
			if uintptr(unsafe.Pointer(m)) == h.ptr() {
				t.Error("IMarshal 应由封送器对象提供，不是回调对象自身")
			}
			if n := atomic.LoadInt32(&h.refs); n != 2 {
				t.Errorf("取得 IMarshal 后外部引用计数 = %d，应为 2", n)
			}
			m.Release() // 委托给外部对象的 Release
			if n := atomic.LoadInt32(&h.refs); n != 1 {
				t.Errorf("释放 IMarshal 后外部引用计数 = %d，应为 1", n)
			}
		}
		h.release()
		if lookupHandler(h.ptr()) != nil {
			t.Error("引用计数归零后仍在存活表里")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// 系统在自己的工作线程（非 Go 线程）上调 ActivateCompleted：用 CreateThread 以回调地址为线程入口模拟，
// x64 调用约定下线程参数落在第一个参数 this 上。
func TestActivateCompletedFromForeignThread(t *testing.T) {
	h, err := newCompletionHandler(false)
	if err != nil {
		t.Fatal(err)
	}
	defer h.release()
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	r, _, e := syscall.SyscallN(kernel32.NewProc("CreateThread").Addr(), 0, 0, h.vtbl.ActivateCompleted, h.ptr(), 0, 0)
	if r == 0 {
		t.Fatalf("CreateThread 失败：%v", e)
	}
	th := windows.Handle(r)
	defer windows.CloseHandle(th)
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒内没有收到 ActivateCompleted")
	}
	if ev, err := windows.WaitForSingleObject(th, 5000); ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("线程没有结束：%d，%v", ev, err)
	}
	var code uint32
	if r, _, e := syscall.SyscallN(kernel32.NewProc("GetExitCodeThread").Addr(), uintptr(th), uintptr(unsafe.Pointer(&code))); r == 0 || code != uint32(sOK) {
		t.Errorf("回调返回 %#x，%v", code, e)
	}
}

func TestDetect(t *testing.T) {
	c := Detect()
	if c.Major == 0 {
		t.Fatalf("RtlGetVersion 没取到版本号：%+v", c)
	}
	t.Log(c)
}

// waitGoroutines 等 goroutine 数回落到 base（采集线程要退干净）。
func waitGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Errorf("goroutine 没退干净：之前 %d 个，现在 %d 个", base, n)
	}
}

// ListSources 冒烟：wine 下一般没有音频设备，只要不崩溃、错误说得清楚。
func TestListSourcesSmoke(t *testing.T) {
	base := runtime.NumGoroutine()
	list, err := ListSources()
	t.Logf("音源 %d 个，错误：%v", len(list), err)
	for _, s := range list {
		t.Logf("  %+v", s)
	}
	waitGoroutines(t, base)
}

// ListDevices 冒烟：wine 下一般没有播放设备；有的话默认设备排第一个，每个都有 ID 和名字。
func TestListDevicesSmoke(t *testing.T) {
	base := runtime.NumGoroutine()
	list, err := ListDevices()
	t.Logf("播放设备 %d 个，错误：%v", len(list), err)
	for i, d := range list {
		t.Logf("  %+v", d)
		if d.ID == "" || d.Name == "" || (d.Default && i != 0) {
			t.Errorf("设备信息不对：%+v（第 %d 个）", d, i)
		}
	}
	waitGoroutines(t, base)
}

// 整机录音指定了不存在的播放设备：报「找不到指定的播放设备」，采集线程退干净。
func TestStartUnknownDevice(t *testing.T) {
	base := runtime.NumGoroutine()
	_, err := Start(Options{Mode: ModeSystem, Device: "{0.0.0.00000000}.{00000000-0000-0000-0000-000000000000}"})
	var he *HRError
	if err == nil || !strings.Contains(err.Error(), "找不到指定的播放设备") || !errors.As(err, &he) {
		t.Fatalf("应报找不到设备：%v", err)
	}
	waitGoroutines(t, base)
}

// Start 冒烟：录本进程（不出声）。wine 不支持按进程环回，会在激活那一步失败，但完成回调照样会被调用；
// 真 Win11 上应能启动。无论成败，采集线程都要退干净。
func TestStartSmoke(t *testing.T) {
	for _, opts := range []Options{
		{Mode: ModeProcess, PID: uint32(os.Getpid())},
		{Mode: ModeSystem},
	} {
		base := runtime.NumGoroutine()
		opts.Log = func(msg string) { t.Log("  " + msg) }
		t.Logf("—— %v", opts.Mode)
		c, err := Start(opts)
		if err != nil {
			var he *HRError
			t.Logf("启动失败（wine 下预期）：%v；是 HRError：%v", err, errors.As(err, &he))
		} else {
			time.Sleep(300 * time.Millisecond)
			if err := c.Stop(); err != nil {
				t.Errorf("Stop：%v", err)
			}
			if _, ok := <-c.Frames(); ok {
				for range c.Frames() {
				}
			}
			select {
			case <-c.Done():
			default:
				t.Error("Stop 返回后 Done 应已关闭")
			}
			t.Logf("信息 %+v，统计 %+v", c.Info(), c.Stats())
			if err := c.Stop(); err != nil { // 可重复调用
				t.Errorf("再次 Stop：%v", err)
			}
		}
		waitGoroutines(t, base)
	}
	if _, err := Start(Options{Mode: ModeProcess}); err == nil {
		t.Error("没给 PID 应报错")
	}
}

// 整条采集循环（事件、GetNextPacketSize / GetBuffer / ReleaseBuffer、转换、切帧、停止、收尾）：
// wine 不支持环回，改录默认录音设备来跑；三种格式分别走系统转换、原样传回混音格式、本包重采样。
// 没有录音设备时跳过；在 wine 里可用 ALSA 的 null 设备（~/.asoundrc 里写 pcm.!default { type null }）提供一个。
func TestCaptureLoopOnInputDevice(t *testing.T) {
	for _, format := range []string{"", FormatNative, FormatPCM16At44k} {
		base := runtime.NumGoroutine()
		t.Logf("—— 格式 %q", format)
		c, err := Start(Options{Mode: ModeSystem, Format: format, inputDevice: true, Log: func(msg string) { t.Log("  " + msg) }})
		if err != nil {
			t.Skipf("没有可用的录音设备：%v", err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := c.Stop(); err != nil {
			t.Errorf("Stop：%v", err)
		}
		frames := 0
		for f := range c.Frames() {
			if len(f.Samples) != FrameSamples || f.Seq < uint64(frames) {
				t.Fatalf("第 %d 帧：序号 %d，%d 个采样", frames, f.Seq, len(f.Samples))
			}
			frames++
		}
		st := c.Stats()
		t.Logf("信息 %+v；统计 %+v；收到 %d 帧", c.Info(), st, frames)
		if st.Packets == 0 || frames == 0 || uint64(frames) != st.Frames {
			t.Errorf("应收到数据：包 %d，帧 %d，统计帧 %d", st.Packets, frames, st.Frames)
		}
		waitGoroutines(t, base)
	}
}
