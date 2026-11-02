package music

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	bufferDuration  = 10_000_000 // 采集缓冲 1 秒（单位 100 纳秒）：只决定能扛多久不取数据，不增加延迟
	waitTimeoutMs   = 100        // 等事件的超时；超时也取一次数据，兼容不发事件的老系统（Win10 1703 以前的环回）
	activateTimeout = 10 * time.Second
	noDataNotice    = 3 * time.Second
)

// Detect 用 RtlGetVersion 取系统版本号（不受程序清单兼容性的影响），选采集方式。
func Detect() Capability {
	v := windows.RtlGetVersion()
	return capabilityFor(v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// Start 开始采集。返回时音频流已经启动；失败时采集线程已清理退出，错误里带出错那一步的 HRESULT。
func Start(opts Options) (*Capture, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errors.New("音频采集只支持 64 位 Windows")
	}
	if err := validate(opts); err != nil {
		return nil, err
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil) // 停止事件，手动复位
	if err != nil {
		return nil, fmt.Errorf("CreateEvent 失败：%w", err)
	}
	stop := &stopSignal{ev: ev}
	q := opts.Queue
	if q <= 0 {
		q = 50
	}
	c := &Capture{frames: make(chan Frame, q), done: make(chan struct{}), stop: stop.set}
	ready := make(chan error, 1)
	go run(c, opts, stop, ready)
	if err := <-ready; err != nil {
		<-c.done
		return nil, err
	}
	return c, nil
}

// stopSignal 是停止事件：Stop 置位它；采集线程退出时关闭它，之后再 Stop 不会碰已关闭的句柄。
type stopSignal struct {
	mu sync.Mutex
	ev windows.Handle
}

func (s *stopSignal) set() {
	s.mu.Lock()
	if s.ev != 0 {
		windows.SetEvent(s.ev)
	}
	s.mu.Unlock()
}

func (s *stopSignal) close() {
	s.mu.Lock()
	if s.ev != 0 {
		windows.CloseHandle(s.ev)
		s.ev = 0
	}
	s.mu.Unlock()
}

// run 是采集线程：初始化，通知 Start，采集到停止或出错，最后释放全部资源。
func run(c *Capture, opts Options, stop *stopSignal, ready chan<- error) {
	// COM 按线程初始化，所以锁定系统线程；不解锁：goroutine 结束时 Go 连同线程一起结束，
	// 不把初始化过 COM、登记过 MMCSS 的线程还给调度器。
	runtime.LockOSThread()
	s := &session{opts: opts, c: c, stopEv: stop.ev}
	s.emitFn = s.emit
	defer close(c.done)
	defer close(c.frames)
	defer stop.close()
	defer s.close()
	if err := s.open(); err != nil {
		ready <- err
		return
	}
	c.info = s.info
	ready <- nil
	c.setErr(s.loop())
}

// session 持有采集线程上的 COM 对象和句柄，只在采集线程上使用。
type session struct {
	opts   Options
	c      *Capture
	stopEv windows.Handle

	comInit  bool
	mmcss    uintptr
	sampleEv windows.Handle // 采样事件：有数据时音频引擎置位
	proc     windows.Handle // 按进程录音的目标进程，用来发现它退出
	enum     *iMMDeviceEnumerator
	device   *iMMDevice
	client   *iAudioClient
	capture  *iAudioCaptureClient
	started  bool

	attempt initAttempt
	conv    *converter
	info    StreamInfo
	emitFn  func(Frame)
	packets uint64
	delta   Stats // 本轮取数据的统计，取完并入 Capture
}

func (s *session) logf(format string, args ...any) {
	if s.opts.Log != nil {
		s.opts.Log(fmt.Sprintf(format, args...))
	}
}

func (s *session) open() error {
	hr := coInitializeEx()
	s.logf("CoInitializeEx(COINIT_MULTITHREADED) → %s", hr)
	if hr.failed() {
		return hr.err("CoInitializeEx")
	}
	s.comInit = true
	if h, err := avSetMmThreadCharacteristics("Audio"); err != nil {
		s.logf("AvSetMmThreadCharacteristics(Audio) 失败（不影响采集）：%v", err)
	} else {
		s.mmcss = h
	}
	ev, err := windows.CreateEvent(nil, 0, 0, nil) // 采样事件，自动复位
	if err != nil {
		return fmt.Errorf("CreateEvent 失败：%w", err)
	}
	s.sampleEv = ev
	if s.opts.Mode == ModeProcess {
		err = s.openProcess()
	} else {
		err = s.openSystem()
	}
	if err != nil {
		return err
	}
	return s.startStream()
}

// openProcess 打开按进程环回：每种格式都重新激活一个 IAudioClient（初始化失败过的不能再用）。
func (s *session) openProcess() error {
	pid := s.opts.PID
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	switch {
	case err == nil:
		s.proc = h
		s.logf("目标进程 PID %d：%s", pid, orUnknown(processPath(h)))
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return fmt.Errorf("进程 %d 不存在（OpenProcess：%v）", pid, err)
	default:
		s.logf("OpenProcess(PID %d) 失败：%v；照常录音，但发现不了它退出", pid, err)
	}
	attempts, err := attemptsFor(ModeProcess, s.opts.Format)
	if err != nil {
		return err
	}
	var tried []string
	var last hresult
	for _, a := range attempts {
		client, err := s.activateProcessLoopback(pid)
		if err != nil {
			return err
		}
		hr := s.initialize(client, a)
		if !hr.failed() {
			s.client = client
			return nil
		}
		release(client)
		tried, last = append(tried, a.name), hr
	}
	return &HRError{Step: "IAudioClient::Initialize（试过 " + strings.Join(tried, "、") + "）", Code: uint32(last)}
}

// activateProcessLoopback 调 ActivateAudioInterfaceAsync 激活按进程环回（含子进程）的 IAudioClient，等完成回调。
func (s *session) activateProcessLoopback(pid uint32) (*iAudioClient, error) {
	if err := procActivateAudioInterfaceAsync.Find(); err != nil {
		return nil, fmt.Errorf("系统没有 ActivateAudioInterfaceAsync（mmdevapi.dll）：%v", err)
	}
	h, err := newCompletionHandler(s.opts.FTM)
	if err != nil {
		return nil, err
	}
	defer h.release() // 我们自己持有的那份引用；系统持有的那份由它自己释放
	h.params = activationParams{activationType: activationTypeProcessLoopback, targetPID: pid, loopbackMode: loopbackModeIncludeTree}
	h.pv = propVariantBlob{vt: vtBlob, cbSize: uint32(unsafe.Sizeof(h.params)), pBlobData: unsafe.Pointer(&h.params)}
	path, _ := windows.UTF16PtrFromString(virtualAudioDeviceProcessLoopback)

	var op *iActivateAsyncOperation
	begin := time.Now()
	r, _, _ := syscall.SyscallN(procActivateAudioInterfaceAsync.Addr(),
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&iidIAudioClient)),
		uintptr(unsafe.Pointer(&h.pv)),
		uintptr(unsafe.Pointer(h)),
		uintptr(unsafe.Pointer(&op)))
	hr := hresult(r)
	ftm := ""
	if s.opts.FTM {
		ftm = "，回调对象聚合了自由线程封送器"
	}
	s.logf("ActivateAudioInterfaceAsync(%s，PID %d，含子进程%s) → %s", virtualAudioDeviceProcessLoopback, pid, ftm, hr)
	if hr.failed() {
		return nil, hr.err("ActivateAudioInterfaceAsync")
	}
	if op == nil {
		return nil, errors.New("ActivateAudioInterfaceAsync 成功了，但没有返回异步操作对象")
	}
	defer release(op)

	select {
	case <-h.done:
	case <-time.After(activateTimeout):
		s.logf("等了 %v 还没收到 ActivateCompleted 回调；%s", activateTimeout, h.queriedText())
		return nil, fmt.Errorf("等了 %v 还没收到 ActivateCompleted 回调", activateTimeout)
	}
	s.logf("收到 ActivateCompleted 回调，用时 %v；%s", time.Since(begin).Round(time.Millisecond), h.queriedText())

	var activateHR hresult
	var unk *iUnknown
	hr = op.GetActivateResult(&activateHR, &unk)
	s.logf("IActivateAudioInterfaceAsyncOperation::GetActivateResult → %s，激活结果 %s", hr, activateHR)
	if hr.failed() {
		release(unk)
		return nil, hr.err("GetActivateResult")
	}
	if activateHR.failed() {
		release(unk)
		return nil, activateHR.err("按进程环回激活")
	}
	if unk == nil {
		return nil, errors.New("激活成功了，但没有返回接口")
	}
	defer release(unk)
	var client *iAudioClient
	hr = unk.QueryInterface(&iidIAudioClient, unsafe.Pointer(&client))
	if hr.failed() || client == nil {
		return nil, hr.err("QueryInterface(IAudioClient)")
	}
	return client, nil
}

// openSystem 打开默认播放设备的整机环回。先请求 48k float 让系统转换，不行再用混音格式由本包转换。
func (s *session) openSystem() error {
	hr := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator, unsafe.Pointer(&s.enum))
	s.logf("CoCreateInstance(MMDeviceEnumerator) → %s", hr)
	if hr.failed() {
		return hr.err("CoCreateInstance(MMDeviceEnumerator)")
	}
	if s.opts.Device != "" {
		hr = s.enum.GetDevice(s.opts.Device, &s.device)
		s.logf("IMMDeviceEnumerator::GetDevice(%s) → %s", s.opts.Device, hr)
		if hr.failed() || s.device == nil {
			return fmt.Errorf("找不到指定的播放设备（可能被拔掉或禁用了，请重新选择）：%w", hr.err("IMMDeviceEnumerator::GetDevice"))
		}
	} else {
		flow, flowName := uint32(eRender), "eRender"
		if s.opts.inputDevice {
			flow, flowName = eCapture, "eCapture"
		}
		hr = s.enum.GetDefaultAudioEndpoint(flow, eConsole, &s.device)
		s.logf("IMMDeviceEnumerator::GetDefaultAudioEndpoint(%s, eConsole) → %s", flowName, hr)
		if hr.failed() {
			return hr.err("GetDefaultAudioEndpoint")
		}
	}
	s.info.Device = s.device.FriendlyName()
	switch {
	case s.opts.inputDevice:
		s.logf("默认录音设备（测试用）：%s", orUnknown(s.info.Device))
	case s.opts.Device != "":
		s.logf("指定的播放设备：%s", orUnknown(s.info.Device))
	default:
		s.logf("默认播放设备：%s", orUnknown(s.info.Device))
	}
	attempts, err := attemptsFor(ModeSystem, s.opts.Format)
	if err != nil {
		return err
	}
	var tried []string
	var last hresult
	for i, a := range attempts {
		var client *iAudioClient
		hr := s.device.Activate(&iidIAudioClient, unsafe.Pointer(&client))
		s.logf("IMMDevice::Activate(IAudioClient) → %s", hr)
		if hr.failed() || client == nil {
			return hr.err("IMMDevice::Activate(IAudioClient)")
		}
		if i == 0 || a.native { // 第一次总记下混音格式，便于判断系统转换做了什么
			mix, hr := client.GetMixFormat()
			f, perr := parseWaveFormat(mix)
			switch {
			case hr.failed():
				s.logf("IAudioClient::GetMixFormat → %s", hr)
			case perr != nil:
				s.logf("IAudioClient::GetMixFormat → %s，无法处理：%v", hr, perr)
			default:
				s.logf("IAudioClient::GetMixFormat → %s，混音格式 %s，声道掩码 0x%X", hr, f, f.ChannelMask)
			}
			if a.native {
				if err := errors.Join(hr.err("IAudioClient::GetMixFormat"), perr); err != nil {
					release(client)
					return err
				}
				a.format, a.wave = f, mix
			}
		}
		hr = s.initialize(client, a)
		if !hr.failed() {
			s.client = client
			return nil
		}
		release(client)
		tried, last = append(tried, a.name), hr
	}
	return &HRError{Step: "IAudioClient::Initialize（试过 " + strings.Join(tried, "、") + "）", Code: uint32(last)}
}

// initialize 用一种格式初始化 IAudioClient（共享模式、环回、事件驱动）。
func (s *session) initialize(client *iAudioClient, a initAttempt) hresult {
	flags := uint32(streamFlagsLoopback | streamFlagsEventCallback)
	if s.opts.inputDevice {
		flags &^= streamFlagsLoopback
	}
	if a.convert {
		flags |= streamFlagsAutoConvertPCM | streamFlagsSrcDefaultQuality
	}
	hr := client.Initialize(flags, bufferDuration, a.wave)
	s.logf("IAudioClient::Initialize(%s：%s，%s) → %s", a.name, a.format, streamFlagNames(flags), hr)
	if !hr.failed() {
		s.attempt = a
	}
	return hr
}

func (s *session) startStream() error {
	var n uint32
	hr := s.client.GetBufferSize(&n)
	s.logf("IAudioClient::GetBufferSize → %s，%d 帧", hr, n)
	hr = s.client.GetService(&iidIAudioCaptureClient, unsafe.Pointer(&s.capture))
	s.logf("IAudioClient::GetService(IAudioCaptureClient) → %s", hr)
	if hr.failed() || s.capture == nil {
		return hr.err("IAudioClient::GetService(IAudioCaptureClient)")
	}
	hr = s.client.SetEventHandle(s.sampleEv)
	s.logf("IAudioClient::SetEventHandle → %s", hr)
	if hr.failed() {
		return hr.err("IAudioClient::SetEventHandle")
	}
	conv, err := newConverter(s.attempt.format, s.opts.Gain)
	if err != nil {
		return err
	}
	s.conv = conv
	hr = s.client.Start()
	s.logf("IAudioClient::Start → %s", hr)
	if hr.failed() {
		return hr.err("IAudioClient::Start")
	}
	s.started = true
	s.info.Mode = s.opts.Mode
	s.info.Format = s.attempt.name
	s.info.Input = s.attempt.format.String()
	s.info.Convert = conv.describe()
	s.info.SystemConvert = s.attempt.convert
	s.info.BufferFrames = n
	if s.opts.Mode == ModeSystem {
		s.info.Warning = SystemWarning
	}
	return nil
}

// loop 是事件驱动的采集循环：等停止事件、采样事件或目标进程退出。
func (s *session) loop() error {
	handles := []windows.Handle{s.stopEv, s.sampleEv}
	if s.proc != 0 {
		handles = append(handles, s.proc)
	}
	begin := time.Now()
	noticed := false
	for {
		ev, err := windows.WaitForMultipleObjects(handles, false, waitTimeoutMs)
		switch {
		case err != nil:
			return fmt.Errorf("WaitForMultipleObjects 失败：%w", err)
		case ev == windows.WAIT_OBJECT_0:
			return nil
		case ev == windows.WAIT_OBJECT_0+2:
			s.logf("目标进程（PID %d）已退出", s.opts.PID)
			return fmt.Errorf("%w（PID %d）", ErrProcessExited, s.opts.PID)
		}
		err = s.drain() // 采样事件或超时
		s.c.addStats(s.delta)
		s.delta = Stats{}
		if err != nil {
			s.logf("采集中断：%v", err)
			return err
		}
		if !noticed && s.packets == 0 && time.Since(begin) > noDataNotice {
			noticed = true
			s.logf("开始 %v 了还没收到数据包：目标程序可能没在出声（整机环回在没有任何声音时不送数据）", noDataNotice)
		}
	}
}

// drain 取完当前所有的包。静音包（AUDCLNT_BUFFERFLAGS_SILENT）不读数据，按静音补零。
func (s *session) drain() error {
	ba := s.attempt.format.blockAlign()
	for {
		var n uint32
		if hr := s.capture.GetNextPacketSize(&n); hr.failed() {
			return hr.err("IAudioCaptureClient::GetNextPacketSize")
		}
		if n == 0 {
			return nil
		}
		var data *byte
		var frames, flags uint32
		var devPos, qpcPos uint64
		hr := s.capture.GetBuffer(&data, &frames, &flags, &devPos, &qpcPos)
		if hr == audclntSBufferEmpty {
			return nil
		}
		if hr.failed() {
			return hr.err("IAudioCaptureClient::GetBuffer")
		}
		if s.packets == 0 {
			s.logf("第一个数据包：%d 帧，标志 0x%X", frames, flags)
		}
		s.packets++
		s.delta.Packets++
		s.delta.InFrames += uint64(frames)
		if flags&bufferFlagsSilent != 0 {
			s.delta.SilentPackets++
		}
		if flags&bufferFlagsDataDiscontinuity != 0 {
			s.delta.Discontinuities++
		}
		silent := flags&bufferFlagsSilent != 0 || data == nil
		var b []byte
		if !silent {
			b = unsafe.Slice(data, int(frames)*ba) // 系统的缓冲区，ReleaseBuffer 之前有效；push 当场处理完
		}
		s.conv.push(b, int(frames), silent, s.emitFn)
		if hr := s.capture.ReleaseBuffer(frames); hr.failed() {
			return hr.err("IAudioCaptureClient::ReleaseBuffer")
		}
	}
}

// emit 交出一帧；通道满了就丢掉，绝不阻塞采集线程（阻塞会让系统缓冲区溢出）。
func (s *session) emit(f Frame) {
	select {
	case s.c.frames <- f:
		s.delta.Frames++
	default:
		s.delta.Dropped++
	}
}

// close 停流并释放 COM 对象、事件句柄，最后 CoUninitialize。打开到一半失败时也调用。
func (s *session) close() {
	if s.started {
		s.logf("IAudioClient::Stop → %s", s.client.Stop())
	}
	release(s.capture)
	release(s.client)
	release(s.device)
	release(s.enum)
	s.capture, s.client, s.device, s.enum = nil, nil, nil, nil
	if s.sampleEv != 0 {
		windows.CloseHandle(s.sampleEv)
	}
	if s.proc != 0 {
		windows.CloseHandle(s.proc)
	}
	if s.mmcss != 0 {
		avRevertMmThreadCharacteristics(s.mmcss)
	}
	if s.comInit {
		coUninitialize()
		s.logf("已释放 COM 对象和句柄，CoUninitialize 完成")
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "（取不到）"
	}
	return s
}
