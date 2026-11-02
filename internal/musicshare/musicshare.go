// Package musicshare 音乐共享的主机广播（设计文档 §8）：选音源，开始和停止广播（采集 → 桥接模组 → SVC），
// 停止广播、关服、退出 MineDeck 时收尾；实例「音乐共享」页的检查清单（核心、SVC、桥接模组、语音端口、
// 远程语音隧道和 voice_host）；装桥接模组；把语音隧道的公网地址自动写进 voice_host。
//
// 同一时间只有一个广播：这台电脑上只有一路采集，桥接模组那边也只接受一个广播连接。
package musicshare

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/events"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/music"
	"minedeck/internal/procutil"
	"minedeck/internal/tunnel"
)

// Error 带状态码的错误（沿用 HTTP 的含义：400 参数或业务错误，404 找不到，409 冲突——已经在广播、需要先确认整机录音）。
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func errorf(code int, format string, a ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, a...)}
}

// ErrNeedConfirm 要整机录音，但这台电脑还没确认过「会把游戏声音、语音聊天一起播出去」。
var ErrNeedConfirm = errors.New("这台电脑只能录整机的声音：游戏声音、语音聊天（好友说的话）也会一起播出去，好友会听到自己说话的回声。确认之后才能开始")

// InstInfo 音乐共享要用到的实例信息。
type InstInfo struct {
	Name   string
	Dir    string
	Core   mcsrc.Core
	MC     string
	Status string   // stopped | starting | running | stopping
	Online []string // 在线的玩家
}

// Instances 实例（inst.Manager 的一层薄包装，测试里换成假的）。
type Instances interface {
	Names() []string
	Info(name string) (InstInfo, error)
	Console(name string) (*procutil.Console, error)
}

// Tunnels 隧道列表（tunnel.Manager）。
type Tunnels interface {
	List() []tunnel.Status
}

// Capturer 一路采集（*music.Capture）。
type Capturer interface {
	Frames() <-chan music.Frame
	Stop() error
	Err() error
	Info() music.StreamInfo
}

// Deps Service 用到的东西。
type Deps struct {
	Instances Instances
	Tunnels   Tunnels
	Dir       string // 设置存在 <Dir>/settings.json（程序目录下的 data/music）

	// 为 nil 时用 music 包的真实现（只有 Windows 能用）；测试里换成假的
	Detect       func() music.Capability
	ListSources  func() ([]music.Source, error)
	ListDevices  func() ([]music.Device, error)
	StartCapture func(music.Options) (Capturer, error)

	// OnAutoStop 广播自己停了（关服、音乐软件退出、连不上桥接模组、退出 MineDeck），给操作记录用；
	// failed 为 true 表示是出错停下的（连不上桥接模组、采集出错）。
	OnAutoStop func(inst, reason string, failed bool)
	// Subscribe 订阅事件（实例启停、隧道拿到公网地址）；nil 用 events.Subscribe。
	Subscribe func(func(events.Event))

	clientOpts func(*bridge.Options) // 测试用：改客户端的参数
	tick       time.Duration         // 测试用：多久检查一次实例状态（默认 1 秒）
	settle     time.Duration         // 测试用：刚改过模组配置时等多久再连（默认 2.5 秒）
}

// Service 音乐共享。
type Service struct {
	d Deps

	mu       sync.Mutex
	cur      *broadcast
	starting bool
	last     map[string]EndView         // 实例 → 上一次广播是怎么结束的
	restart  map[string]map[string]bool // 实例 → 要重启服务器才生效的改动（restartBridge、restartVoiceHost）
	watches  map[string]*voiceWatch
	set      settings
	svcCache map[string]svcEntry
}

// settings 存在 data/music/settings.json 的设置（排除名单在实例的 voicebridge.properties 里，不在这里）。
type settings struct {
	SystemConfirmed bool                     `json:"systemConfirmed,omitempty"` // 确认过「这台电脑只能录整机」（每台电脑一次）
	Device          string                   `json:"device,omitempty"`          // 整机录音指定的播放设备 ID；空为系统默认
	Instances       map[string]*instSettings `json:"instances,omitempty"`
}

type instSettings struct {
	Volume           *int   `json:"volume,omitempty"`           // 音量（%），默认 100
	Mode             string `json:"mode,omitempty"`             // 上次用的采集方式：process / system
	Exe              string `json:"exe,omitempty"`              // 上次选的程序（下次默认选它）
	VoiceHostWritten string `json:"voiceHostWritten,omitempty"` // MineDeck 上次从语音隧道自动写进 voice_host 的值
}

const (
	defaultVolume = 100
	maxVolume     = 200
)

var (
	allMu sync.Mutex
	all   []*Service
)

// New 建好音乐共享，订阅实例启停和隧道事件。
func New(d Deps) *Service {
	if d.Detect == nil {
		d.Detect = music.Detect
	}
	if d.ListSources == nil {
		d.ListSources = music.ListSources
	}
	if d.ListDevices == nil {
		d.ListDevices = music.ListDevices
	}
	if d.StartCapture == nil {
		d.StartCapture = func(o music.Options) (Capturer, error) {
			c, err := music.Start(o)
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	}
	if d.tick <= 0 {
		d.tick = time.Second
	}
	if d.settle <= 0 {
		d.settle = 2500 * time.Millisecond
	}
	s := &Service{d: d, last: map[string]EndView{}, restart: map[string]map[string]bool{}, watches: map[string]*voiceWatch{}, svcCache: map[string]svcEntry{}}
	s.loadSettings()
	sub := d.Subscribe
	if sub == nil {
		sub = events.Subscribe
	}
	sub(s.HandleEvent)
	allMu.Lock()
	all = append(all, s)
	allMu.Unlock()
	return s
}

// ShutdownAll 退出 MineDeck 前调用：停掉所有广播（给桥接模组发 END、停采集），等它们收尾完。
func ShutdownAll(reason string) {
	allMu.Lock()
	list := append([]*Service(nil), all...)
	allMu.Unlock()
	for _, s := range list {
		s.Shutdown(reason)
	}
}

// Shutdown 停掉这个 Service 的广播。
func (s *Service) Shutdown(reason string) {
	s.mu.Lock()
	b := s.cur
	s.mu.Unlock()
	if b != nil {
		b.stop(reason, endAuto)
	}
}

func (s *Service) settingsPath() string { return filepath.Join(s.d.Dir, "settings.json") }

func (s *Service) loadSettings() {
	if s.d.Dir == "" {
		return
	}
	b, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &s.set)
}

// saveSettingsLocked 调用方持有 s.mu。
func (s *Service) saveSettingsLocked() error {
	if s.d.Dir == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.set, "", "  ")
	if err != nil {
		return err
	}
	return bridge.WriteFileAtomic(s.settingsPath(), b)
}

// instSet 取实例的设置（没有就建一份）；调用方持有 s.mu。
func (s *Service) instSet(name string) *instSettings {
	if s.set.Instances == nil {
		s.set.Instances = map[string]*instSettings{}
	}
	is := s.set.Instances[name]
	if is == nil {
		is = &instSettings{}
		s.set.Instances[name] = is
	}
	return is
}

func (is *instSettings) volume() int {
	if is == nil || is.Volume == nil {
		return defaultVolume
	}
	return *is.Volume
}

// HandleEvent 实例启动时开始看它的控制台（谁连上了语音）；停了清掉，正在给它广播的也停掉；
// 语音隧道拿到公网地址时写进绑定实例的 voice_host。
func (s *Service) HandleEvent(e events.Event) {
	switch e.Type {
	case "instance.start":
		if name, _ := e.Data["instance"].(string); name != "" {
			s.startWatch(name, true)
		}
	case "instance.stop", "instance.crash":
		name, _ := e.Data["instance"].(string)
		s.stopWatch(name)
		s.mu.Lock()
		b := s.cur
		s.mu.Unlock()
		if b != nil && b.inst == name {
			b.stop("服务器已停止", endAuto)
		}
	case "tunnel.up":
		id, _ := e.Data["id"].(string)
		for _, t := range s.d.Tunnels.List() {
			if t.ID == id && t.IsVoice() && t.Bound != "" {
				s.syncVoiceHost(t.Bound, false)
			}
		}
	}
}

// BeforeStart 起服务器进程之前（inst 的回调）：清掉「重启后生效」的提示；装了桥接模组就保证它的配置能用
// （打开共享、端口、令牌）；语音隧道已经有公网地址的，先写进 voice_host（SVC 只在启动时读）。
func (s *Service) BeforeStart(name string) {
	s.mu.Lock()
	delete(s.restart, name)
	s.mu.Unlock()
	info, err := s.d.Instances.Info(name)
	if err != nil || !supported(info.Core) {
		return
	}
	if st, err := bridge.InspectJar(modsDir(info.Dir)); err == nil && st.Installed {
		if _, _, err := bridge.EnsureConfig(info.Dir, bridge.EnsureOptions{Avoid: s.portsInUse(name)}); err != nil {
			s.log(name, "写桥接模组的配置失败：%v", err)
		}
	}
	s.syncVoiceHost(name, true)
}

// portsInUse 别的实例的桥接模组已经用了的端口（选端口时避开）。
func (s *Service) portsInUse(self string) func(int) bool {
	used := map[int]bool{}
	for _, n := range s.d.Instances.Names() {
		if n == self {
			continue
		}
		if info, err := s.d.Instances.Info(n); err == nil {
			if c, err := bridge.ReadConfig(info.Dir); err == nil && c.Port > 0 {
				used[c.Port] = true
			}
		}
	}
	return func(p int) bool { return used[p] }
}

// log 往实例的控制台写一行「[MineDeck] 音乐共享：…」。
func (s *Service) log(name, format string, a ...any) {
	if con, err := s.d.Instances.Console(name); err == nil && con != nil {
		con.Append("[MineDeck] 音乐共享：" + fmt.Sprintf(format, a...))
	}
}

func modsDir(dir string) string { return filepath.Join(dir, "mods") }

// 要重启服务器才生效的改动。
const (
	restartBridge    = "bridge"    // 服务器运行中装了桥接模组
	restartVoiceHost = "voicehost" // 服务器运行中改了 voice_host（SVC 只在启动时读）
)

// markRestart 记下要重启才生效的改动；调用方不持有 s.mu。
func (s *Service) markRestart(name, kind string) {
	s.mu.Lock()
	if s.restart[name] == nil {
		s.restart[name] = map[string]bool{}
	}
	s.restart[name][kind] = true
	s.mu.Unlock()
}

// ---- inst.Manager 的包装 ----

type managerInstances struct{ m *inst.Manager }

// FromManager 把 inst.Manager 包成 Instances。
func FromManager(m *inst.Manager) Instances { return managerInstances{m} }

func (w managerInstances) Names() []string {
	var out []string
	for _, i := range w.m.List() {
		out = append(out, i.Name)
	}
	return out
}

func (w managerInstances) Info(name string) (InstInfo, error) {
	i, err := w.m.Get(name)
	if err != nil {
		return InstInfo{}, err
	}
	return InstInfo{Name: i.Name, Dir: i.Dir, Core: i.Core, MC: i.MC, Status: i.Status(), Online: i.OnlinePlayers()}, nil
}

func (w managerInstances) Console(name string) (*procutil.Console, error) {
	i, err := w.m.Get(name)
	if err != nil {
		return nil, err
	}
	return i.Console, nil
}
