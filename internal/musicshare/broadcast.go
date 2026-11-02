package musicshare

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/music"
)

// StartReq 开始广播的参数。
type StartReq struct {
	Inst   string `json:"inst"`
	Mode   string `json:"mode"`   // process：只录一个程序（Win11）；system：整机录音
	PID    uint32 `json:"pid"`    // 只录一个程序时：进程号
	Exe    string `json:"exe"`    // 只录一个程序时：程序名（只用来显示和记住这次的选择）
	Device string `json:"device"` // 整机录音时：播放设备 ID；空用上次选的，没选过用系统默认
}

// BroadcastView 广播的状态（给界面）。
type BroadcastView struct {
	Inst       string  `json:"inst"`
	Phase      string  `json:"phase"` // connecting 连接中 | live 正在播 | retrying 断线重连中
	Since      string  `json:"since"` // 开始的时间
	Mode       string  `json:"mode"`
	Source     string  `json:"source"` // 在录什么
	Volume     int     `json:"volume"`
	Listeners  int     `json:"listeners"` // 听众数（桥接模组报的）；还没连上时为 -1
	Buffered   int     `json:"buffered"`  // 模组缓冲里的帧
	RTTMs      float64 `json:"rttMs"`
	Sent       uint64  `json:"sent"`       // 发给模组的帧
	Dropped    uint64  `json:"dropped"`    // 没连上、或者发不及丢掉的帧
	Silent     uint64  `json:"silent"`     // 静音、没发的帧
	ModDropped uint32  `json:"modDropped"` // 模组缓冲满了丢掉的
	Underruns  uint32  `json:"underruns"`  // 模组缓冲取空的次数
	LastError  string  `json:"lastError,omitempty"`
	Busy       bool    `json:"busy,omitempty"`    // 模组那边已经有别的广播连接
	Warning    string  `json:"warning,omitempty"` // 整机录音的提醒
	Input      string  `json:"input,omitempty"`   // 采集格式
}

// EndView 上一次广播是怎么结束的。
type EndView struct {
	Reason string `json:"reason"`
	At     string `json:"at"`
	Error  bool   `json:"error"` // 出错停下的（连不上桥接模组、采集出错）
}

// 结束的方式。
const (
	endUser = iota // 用户点了停止
	endAuto        // 关服、退出 MineDeck、音乐软件退出
	endFail        // 出错：连不上桥接模组、采集出错
)

type broadcast struct {
	s       *Service
	inst    string
	mode    music.Mode
	source  string
	warning string
	input   string
	since   time.Time
	cap     Capturer
	cli     *bridge.Client
	volume  atomic.Int32
	silent  atomic.Uint64

	stopOnce sync.Once
	stopc    chan struct{}
	pumped   chan struct{} // 采集的帧通道关了（采集停了或出错）
	done     chan struct{} // 收尾完了
}

func modeName(m music.Mode) string {
	if m == music.ModeProcess {
		return "process"
	}
	return "system"
}

// Start 开始广播：检查服务器、桥接模组、SVC；整机录音要先确认过；保证桥接模组的配置能用
// （刚改过就等模组重读一次再连）；开始采集，连上桥接模组推音频。同一时间只能有一个广播。
func (s *Service) Start(req StartReq) (*BroadcastView, error) {
	s.mu.Lock()
	if b := s.cur; b != nil {
		s.mu.Unlock()
		if b.inst == req.Inst {
			return nil, errorf(409, "已经在给「%s」广播了", b.inst)
		}
		return nil, errorf(409, "正在给「%s」广播：同一时间只能有一个广播，先停掉它", b.inst)
	}
	if s.starting {
		s.mu.Unlock()
		return nil, errorf(409, "正在开始广播，请稍候")
	}
	s.starting = true
	confirmed := s.set.SystemConfirmed
	device := s.set.Device
	bridgePending := s.restart[req.Inst][restartBridge]
	vol := s.set.Instances[req.Inst].volume()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
	}()

	info, err := s.d.Instances.Info(req.Inst)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	if ok, why := supportOf(info.Core, info.MC); !ok {
		return nil, errorf(400, "%s", why)
	}
	if info.Status != "running" {
		return nil, errorf(400, "服务器没在运行：先开服，等它启动完成再开始广播")
	}
	jar, err := bridge.InspectJar(modsDir(info.Dir))
	if err != nil {
		return nil, errorf(500, "读 mods 目录失败：%v", err)
	}
	if jar.File == "" {
		return nil, errorf(400, "还没装桥接模组：先在检查清单里安装，然后重启服务器")
	}
	if bridgePending {
		return nil, errorf(400, "桥接模组装好以后还没重启服务器：重启后才能广播")
	}
	if svc := s.detectSVC(info.Dir); !svc.Installed {
		return nil, errorf(400, "mods 里没有 Simple Voice Chat：音乐要靠它推给玩家")
	}

	var opts music.Options
	switch req.Mode {
	case "process":
		if req.PID == 0 {
			return nil, errorf(400, "先在列表里选一个正在出声的程序")
		}
		opts = music.Options{Mode: music.ModeProcess, PID: req.PID}
	case "system":
		if !confirmed {
			return nil, &Error{Code: 409, Err: ErrNeedConfirm}
		}
		if req.Device != "" {
			device = req.Device
		}
		opts = music.Options{Mode: music.ModeSystem, Device: device}
	default:
		return nil, errorf(400, "采集方式只能是 process（只录一个程序）或 system（整机录音）")
	}
	opts.Queue = 50

	_, changed, err := bridge.EnsureConfig(info.Dir, bridge.EnsureOptions{Avoid: s.portsInUse(req.Inst)})
	if err != nil {
		return nil, errorf(500, "写桥接模组的配置失败：%v", err)
	}
	var notBefore time.Time
	if changed { // 模组每 2 秒重读一次配置：等它读到再连，免得碰上旧端口、旧令牌
		notBefore = time.Now().Add(s.d.settle)
	}

	c, err := s.d.StartCapture(opts)
	if err != nil {
		if opts.Mode == music.ModeProcess {
			return nil, errorf(400, "只录这个程序没成功：%v。可以换一个程序试试，或者在高级选项里改用整机录音", err)
		}
		return nil, errorf(400, "整机录音没成功：%v", err)
	}
	si := c.Info()
	b := &broadcast{s: s, inst: req.Inst, mode: opts.Mode, since: time.Now(), cap: c, input: si.Input,
		stopc: make(chan struct{}), pumped: make(chan struct{}), done: make(chan struct{})}
	label := ""
	if opts.Mode == music.ModeProcess {
		exe := req.Exe
		if exe == "" {
			exe = "程序"
		}
		b.source = fmt.Sprintf("%s（PID %d）", exe, req.PID)
		label = exe
	} else {
		dev := si.Device
		if dev == "" {
			dev = "默认播放设备"
		}
		b.source = "整机声音：" + dev
		b.warning = music.SystemWarning
		label = "整机声音"
	}
	b.volume.Store(int32(vol))
	o := bridge.Options{Dir: info.Dir, Name: "MineDeck：" + label, NotBefore: notBefore,
		Logf: func(format string, a ...any) { s.log(req.Inst, format, a...) }}
	if s.d.clientOpts != nil {
		s.d.clientOpts(&o)
	}
	b.cli = bridge.Start(o)

	s.mu.Lock()
	s.cur = b
	delete(s.last, req.Inst)
	is := s.instSet(req.Inst)
	is.Mode = req.Mode
	if req.Exe != "" {
		is.Exe = req.Exe
	}
	if req.Mode == "system" && req.Device != "" {
		s.set.Device = req.Device
	}
	_ = s.saveSettingsLocked()
	s.mu.Unlock()
	s.log(req.Inst, "开始广播：%s", b.source)
	go b.pump()
	go b.supervise()
	return b.view(), nil
}

// Stop 用户停止广播。
func (s *Service) Stop(name string) error {
	s.mu.Lock()
	b := s.cur
	s.mu.Unlock()
	if b == nil || b.inst != name {
		return errorf(400, "没在给「%s」广播", name)
	}
	b.stop("用户停止", endUser)
	return nil
}

// Current 正在广播的实例；没有时为空。
func (s *Service) Current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil {
		return ""
	}
	return s.cur.inst
}

// pump 采集的帧 → 调音量 → 交给客户端（客户端从不阻塞）；数字静音（全是 0，比如音乐暂停了）的帧不发，
// 模组 100 毫秒收不到新帧会让 SVC 收尾，重新有声音时自动接上。
func (b *broadcast) pump() {
	defer close(b.pumped)
	for f := range b.cap.Frames() {
		if silent(f.Samples) {
			b.silent.Add(1)
			continue
		}
		b.cli.Send(applyGain(f.Samples, int(b.volume.Load())))
	}
}

// supervise 盯着这次广播：采集停了、客户端放弃了、服务器不在运行了，就收尾。
func (b *broadcast) supervise() {
	t := time.NewTicker(b.s.d.tick)
	defer t.Stop()
	for {
		select {
		case <-b.stopc:
			return
		case <-b.pumped:
			err := b.cap.Err()
			switch {
			case errors.Is(err, music.ErrProcessExited):
				b.stop("音乐软件退出了", endAuto)
			case err != nil:
				b.stop("采集出错："+err.Error(), endFail)
			default:
				b.stop("采集停了", endFail)
			}
			return
		case <-b.cli.Done():
			b.stop("连不上桥接模组："+errString(b.cli.Err()), endFail)
			return
		case <-t.C:
			info, err := b.s.d.Instances.Info(b.inst)
			switch {
			case err != nil:
				b.stop("实例不见了", endAuto)
				return
			case info.Status == "stopping":
				b.stop("服务器正在停止", endAuto)
				return
			case info.Status != "running":
				b.stop("服务器已停止", endAuto)
				return
			}
		}
	}
}

// stop 收尾：给桥接模组发 END（带上原因）、停采集（释放 COM 对象、退出采集线程），等这些都做完再返回。
// 可以从任何 goroutine 调用、重复调用。
func (b *broadcast) stop(reason string, kind int) {
	b.stopOnce.Do(func() {
		close(b.stopc)
		b.cli.Stop(reason)
		_ = b.cap.Stop()
		<-b.pumped
		s := b.s
		s.mu.Lock()
		if s.cur == b {
			s.cur = nil
		}
		s.last[b.inst] = EndView{Reason: reason, At: time.Now().Format(time.RFC3339), Error: kind == endFail}
		s.mu.Unlock()
		s.log(b.inst, "停止广播（%s）", reason)
		if kind != endUser && s.d.OnAutoStop != nil {
			s.d.OnAutoStop(b.inst, reason, kind == endFail)
		}
		close(b.done)
	})
	<-b.done
}

func (b *broadcast) view() *BroadcastView {
	st := b.cli.State()
	v := &BroadcastView{Inst: b.inst, Phase: string(st.Phase), Since: b.since.Format(time.RFC3339), Mode: modeName(b.mode),
		Source: b.source, Volume: int(b.volume.Load()), Listeners: -1, RTTMs: st.RTTMs, Sent: st.Sent, Dropped: st.Dropped,
		Silent: b.silent.Load(), LastError: st.LastError, Busy: st.Busy, Warning: b.warning, Input: b.input}
	if st.Phase == bridge.PhaseStopped {
		v.Phase = string(bridge.PhaseRetrying) // 收尾中
	}
	if st.Phase == bridge.PhaseLive {
		v.LastError = ""
		if st.Status != nil {
			v.Listeners, v.Buffered, v.ModDropped, v.Underruns = st.Status.Listeners, st.Status.Buffered, st.Status.Dropped, st.Status.Underruns
		}
	}
	return v
}

func silent(s []int16) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

// applyGain 按百分比调音量，削顶到 16 位的范围。
func applyGain(in []int16, pct int) []int16 {
	if pct == 100 {
		return in
	}
	out := make([]int16, len(in))
	for i, v := range in {
		x := int32(v) * int32(pct) / 100
		switch {
		case x > 32767:
			x = 32767
		case x < -32768:
			x = -32768
		}
		out[i] = int16(x)
	}
	return out
}

func errString(err error) string {
	if err == nil {
		return "未知原因"
	}
	return err.Error()
}
