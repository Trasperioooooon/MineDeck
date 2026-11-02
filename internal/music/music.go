// Package music 为「音乐共享」采集主机上的声音。
//
// Win11（系统版本号 ≥ 20348）用按进程环回，只录指定程序及其子进程；
// Win10 退回整机环回（默认播放设备上的全部声音），会把游戏声音、语音聊天一起录进去，要提醒用户。
// 输出统一为 48kHz、16 位、单声道 PCM，每 20ms 一帧（960 个采样），即 Simple Voice Chat 的帧格式。
//
// 纯 Go，不用 cgo：COM 接口按 vtable 手工调用；ActivateAudioInterfaceAsync 的完成回调对象
// 用 syscall.NewCallback 搭 vtable（见 com_windows.go）。用法：
//
//	c, err := music.Start(music.Options{Mode: music.ModeProcess, PID: pid})
//	for f := range c.Frames() { … } // 每帧 960 个采样；采集结束后通道关闭
//	err = c.Stop()                  // 等采集线程释放资源并退出
package music

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	SampleRate    = 48000                 // 输出采样率（Hz）
	FrameSamples  = 960                   // 每帧采样数
	FrameDuration = 20 * time.Millisecond // 每帧时长

	// ProcessLoopbackMinBuild 是按进程环回的最低系统版本号。微软示例和 AUDIOCLIENT_ACTIVATION_PARAMS
	// 文档写 20348（函数文档有一处写 20438）；Win10 最后的 22H2 是 19045，所以实际上只有 Win11 能用。
	// 版本号只用来选默认方式，能不能录以运行时调用成功为准。
	ProcessLoopbackMinBuild = 20348
)

// Mode 是采集方式。
type Mode int

const (
	ModeProcess Mode = 1 // 按进程环回：只录目标进程及其子进程
	ModeSystem  Mode = 2 // 整机环回：默认播放设备上的全部声音
)

func (m Mode) String() string {
	switch m {
	case ModeProcess:
		return "按进程录音"
	case ModeSystem:
		return "整机录音"
	}
	return fmt.Sprintf("未知方式(%d)", int(m))
}

// SystemWarning 是整机录音时要给用户看的提醒。
const SystemWarning = "整机录音会把游戏声音、语音聊天等一起录进去；主机自己也在这台电脑上玩时，别人说话会被再广播一遍，形成回声。"

// Capability 是能力检测的结果。
type Capability struct {
	Major, Minor, Build uint32
	Mode                Mode // 推荐的采集方式
	Warn                bool // 只能整机录音，需要提醒用户（SystemWarning）
}

// capabilityFor 按系统版本号选采集方式。
func capabilityFor(major, minor, build uint32) Capability {
	c := Capability{Major: major, Minor: minor, Build: build, Mode: ModeSystem, Warn: true}
	if major > 10 || major == 10 && build >= ProcessLoopbackMinBuild {
		c.Mode, c.Warn = ModeProcess, false
	}
	return c
}

func (c Capability) String() string {
	return fmt.Sprintf("%s（%d.%d.%d）→ %s", osName(c.Major, c.Build), c.Major, c.Minor, c.Build, c.Mode)
}

func osName(major, build uint32) string {
	switch {
	case major == 0:
		return "未知系统"
	case major > 10:
		return "Windows（新版本）"
	case major == 10 && build >= 22000:
		return "Windows 11"
	case major == 10 && build >= ProcessLoopbackMinBuild:
		return "Windows Server 2022 / Win10 预览版"
	case major == 10:
		return "Windows 10"
	}
	return "Windows 8.1 或更早"
}

// Source 是一个可选的音源：在播放设备上开了音频会话的进程（同一进程的多个会话已合并）。
type Source struct {
	PID     uint32
	Exe     string   // exe 文件名；取不到（如权限不足）时为空
	Path    string   // exe 完整路径；取不到时为空
	Name    string   // 会话显示名，多数程序不设置
	Playing bool     // 会话处于活动状态：开着音频流（正在出声，或暂停了但没关流）
	Peak    float32  // 列举时刻的峰值电平，0–1
	Devices []string // 会话所在的播放设备，默认设备带「（默认）」
}

// Device 是一个播放设备（整机录音时可以指定录哪一个）。
type Device struct {
	ID      string `json:"id"`      // 设备 ID（IMMDevice::GetId），传给 Options.Device
	Name    string `json:"name"`    // 声音设置里显示的名字
	Default bool   `json:"default"` // 是不是系统默认的播放设备
}

// Frame 是一帧输出：48kHz、16 位、单声道，FrameSamples 个采样。
type Frame struct {
	Seq     uint64 // 帧序号，从 0 开始
	Samples []int16
}

// PeakOf 返回采样的峰值电平（0–1）。
func PeakOf(samples []int16) float64 {
	m := 0
	for _, v := range samples {
		a := int(v)
		if a < 0 {
			a = -a
		}
		if a > m {
			m = a
		}
	}
	return float64(m) / 32768
}

// IAudioClient::Initialize 用的格式（Options.Format），测试时可指定其一；空字符串为自动依次尝试。
const (
	FormatFloat      = "float"     // float32 双声道 48kHz，WAVEFORMATEXTENSIBLE（同 OBS），加系统转换
	FormatPCM16      = "pcm16"     // int16 双声道 48kHz，WAVEFORMATEX（同微软示例，只改采样率），加系统转换
	FormatPCM16At44k = "pcm16-44k" // int16 双声道 44.1kHz，与微软示例完全相同，由本包重采样到 48kHz
	FormatNative     = "native"    // 仅整机录音：用设备混音格式，不让系统转换，由本包下混、重采样
)

// Options 是采集参数。
type Options struct {
	Mode   Mode
	PID    uint32  // 按进程录音的目标进程（含其子进程）
	Device string  // 整机录音时录哪个播放设备（设备 ID，见 ListDevices）；空为系统默认的播放设备
	Format string  // 见 Format* 常量；空为自动
	Gain   float64 // 音量增益，0 视为 1
	FTM    bool    // 备用写法：完成回调对象再聚合自由线程封送器（应答 IMarshal），与 WRL 的 FtmBase 一致
	Queue  int     // 帧通道容量，0 为 50（1 秒）；消费跟不上时丢弃新帧，不阻塞采集线程
	// Log 接收诊断日志：每一步做了什么、返回的 HRESULT。在采集线程上调用，要能并发调用；nil 则不输出。
	Log func(msg string)

	// inputDevice 仅供测试：整机方式改录默认录音设备（不加 LOOPBACK），在不支持环回的 wine 里也能跑通采集循环。
	inputDevice bool
}

// StreamInfo 是采集流协商出的结果。
type StreamInfo struct {
	Mode          Mode
	Device        string // 整机录音时的设备名
	Format        string // 生效的初始化格式（Format* 之一）
	Input         string // 采集流的格式，如「float32 2 声道 48000Hz」
	Convert       string // 本包做的处理，如「下混 2→1，重采样 44100→48000Hz」
	SystemConvert bool   // 是否让系统转换格式（AUTOCONVERTPCM | SRC_DEFAULT_QUALITY）
	BufferFrames  uint32 // IAudioClient::GetBufferSize 的结果
	Warning       string // 非空时要提醒用户
}

// Stats 是采集统计。
type Stats struct {
	Packets         uint64 // 收到的包
	InFrames        uint64 // 收到的采样帧（同一时刻的所有声道算一帧）
	SilentPackets   uint64 // 带 AUDCLNT_BUFFERFLAGS_SILENT 的包（已按静音补零）
	Discontinuities uint64 // 带 AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY 的包（采集有断档）
	Frames          uint64 // 交出的 20ms 帧
	Dropped         uint64 // 帧通道满了丢掉的帧
}

// ErrProcessExited 表示按进程录音的目标进程已经退出。
var ErrProcessExited = errors.New("目标进程已退出")

// Capture 是一路正在进行的采集。帧从 Frames 取；采集结束（Stop 或出错）后通道关闭，Err 给出原因。
type Capture struct {
	info   StreamInfo
	frames chan Frame
	done   chan struct{}
	stop   func() // 通知采集线程停止（平台相关）

	stopOnce sync.Once
	mu       sync.Mutex
	err      error
	stats    Stats
}

// Info 返回协商出的采集格式。
func (c *Capture) Info() StreamInfo { return c.info }

// Frames 返回帧通道；采集结束后关闭。
func (c *Capture) Frames() <-chan Frame { return c.frames }

// Done 在采集线程释放完资源、退出后关闭。
func (c *Capture) Done() <-chan struct{} { return c.done }

// Err 返回采集中途出的错（目标进程退出、设备失效等）；主动 Stop 的为 nil。
func (c *Capture) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stats 返回当前统计。
func (c *Capture) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Stop 停止采集，等采集线程停流、释放 COM 对象和事件句柄并退出后返回 Err()。可重复调用。
func (c *Capture) Stop() error {
	c.stopOnce.Do(c.stop)
	<-c.done
	return c.Err()
}

func (c *Capture) setErr(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

func (c *Capture) addStats(d Stats) {
	c.mu.Lock()
	s := &c.stats
	s.Packets += d.Packets
	s.InFrames += d.InFrames
	s.SilentPackets += d.SilentPackets
	s.Discontinuities += d.Discontinuities
	s.Frames += d.Frames
	s.Dropped += d.Dropped
	c.mu.Unlock()
}

// initAttempt 是一次 IAudioClient::Initialize 尝试。
type initAttempt struct {
	name    string       // Format* 之一
	format  sampleFormat // 请求的格式；native 时由 GetMixFormat 填
	wave    []byte       // 传给 Initialize 的 WAVEFORMATEX 字节
	convert bool         // 是否加 AUTOCONVERTPCM | SRC_DEFAULT_QUALITY
	native  bool         // 用设备混音格式
}

var (
	stereoFloat48 = sampleFormat{Channels: 2, Rate: 48000, Bits: 32, Float: true, ChannelMask: speakerFrontLeft | speakerFrontRight}
	stereoPCM48   = sampleFormat{Channels: 2, Rate: 48000, Bits: 16}
	stereoPCM44   = sampleFormat{Channels: 2, Rate: 44100, Bits: 16}
)

// attemptsFor 返回要依次尝试的初始化格式。按进程环回不支持 GetMixFormat（miniaudio 源码注释、
// 微软示例均自己指定格式），所以没有 native；整机先请求 48k float 让系统转换，失败再用混音格式自己转。
func attemptsFor(mode Mode, format string) ([]initAttempt, error) {
	all := map[string]initAttempt{
		FormatFloat:      {name: FormatFloat, format: stereoFloat48, convert: true},
		FormatPCM16:      {name: FormatPCM16, format: stereoPCM48, convert: true},
		FormatPCM16At44k: {name: FormatPCM16At44k, format: stereoPCM44, convert: true},
		FormatNative:     {name: FormatNative, native: true},
	}
	var order []string
	switch {
	case format != "":
		order = []string{format}
	case mode == ModeProcess:
		order = []string{FormatFloat, FormatPCM16, FormatPCM16At44k}
	default:
		order = []string{FormatFloat, FormatNative}
	}
	var list []initAttempt
	for _, name := range order {
		a, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("未知的初始化格式 %q（可选 float / pcm16 / pcm16-44k / native）", name)
		}
		if a.native && mode == ModeProcess {
			return nil, errors.New("按进程录音不支持 GetMixFormat，不能用 native 格式")
		}
		if !a.native {
			a.wave = waveFormatBytes(a.format)
		}
		list = append(list, a)
	}
	return list, nil
}

// validate 检查采集参数。
func validate(opts Options) error {
	switch opts.Mode {
	case ModeProcess:
		if opts.PID == 0 {
			return errors.New("按进程录音要指定进程 ID")
		}
	case ModeSystem:
	default:
		return fmt.Errorf("未知的采集方式 %d", int(opts.Mode))
	}
	if opts.Device != "" && opts.Mode != ModeSystem {
		return errors.New("只有整机录音才能指定播放设备")
	}
	if opts.Gain < 0 {
		return fmt.Errorf("音量增益不能为负：%v", opts.Gain)
	}
	_, err := attemptsFor(opts.Mode, opts.Format)
	return err
}

// streamFlagNames 把流标志写成「LOOPBACK|EVENTCALLBACK|…」，用于日志。
func streamFlagNames(flags uint32) string {
	names := []struct {
		bit  uint32
		name string
	}{
		{streamFlagsLoopback, "LOOPBACK"},
		{streamFlagsEventCallback, "EVENTCALLBACK"},
		{streamFlagsAutoConvertPCM, "AUTOCONVERTPCM"},
		{streamFlagsSrcDefaultQuality, "SRC_DEFAULT_QUALITY"},
	}
	s := ""
	for _, n := range names {
		if flags&n.bit != 0 {
			if s != "" {
				s += "|"
			}
			s += n.name
		}
	}
	return s
}

// IAudioClient::Initialize 的流标志（audiosessiontypes.h）与 GetBuffer 的包标志（audioclient.h）。
const (
	streamFlagsLoopback          = 0x00020000
	streamFlagsEventCallback     = 0x00040000
	streamFlagsSrcDefaultQuality = 0x08000000
	streamFlagsAutoConvertPCM    = 0x80000000

	bufferFlagsDataDiscontinuity = 0x1
	bufferFlagsSilent            = 0x2
)
