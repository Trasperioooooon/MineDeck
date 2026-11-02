package musicshare

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/bridge/bridgetest"
	"minedeck/internal/events"
	"minedeck/internal/mcsrc"
	"minedeck/internal/music"
	"minedeck/internal/procutil"
	"minedeck/internal/tunnel"
)

// ---- 假的实例、隧道、采集 ----

type fakeInst struct {
	mu    sync.Mutex
	infos map[string]*InstInfo
	cons  map[string]*procutil.Console
}

func (f *fakeInst) add(info InstInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.infos == nil {
		f.infos, f.cons = map[string]*InstInfo{}, map[string]*procutil.Console{}
	}
	f.infos[info.Name] = &info
	f.cons[info.Name] = procutil.NewConsole()
}

func (f *fakeInst) setStatus(name, st string) {
	f.mu.Lock()
	f.infos[name].Status = st
	f.mu.Unlock()
}

func (f *fakeInst) setOnline(name string, players ...string) {
	f.mu.Lock()
	f.infos[name].Online = players
	f.mu.Unlock()
}

func (f *fakeInst) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.infos {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (f *fakeInst) Info(name string) (InstInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.infos[name]
	if i == nil {
		return InstInfo{}, fmt.Errorf("实例不存在: %s", name)
	}
	c := *i
	c.Online = append([]string(nil), i.Online...)
	return c, nil
}

func (f *fakeInst) Console(name string) (*procutil.Console, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.cons[name]; c != nil {
		return c, nil
	}
	return nil, errors.New("实例不存在")
}

type fakeTunnels struct {
	mu   sync.Mutex
	list []tunnel.Status
}

func (f *fakeTunnels) set(list ...tunnel.Status) {
	f.mu.Lock()
	f.list = list
	f.mu.Unlock()
}

func (f *fakeTunnels) List() []tunnel.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tunnel.Status(nil), f.list...)
}

// fakeCap 假的采集：测试往里推帧；Stop 或 die 之后帧通道关闭。
type fakeCap struct {
	opts   music.Options
	frames chan music.Frame
	mu     sync.Mutex
	closed bool
	err    error
	seq    uint64
}

func (c *fakeCap) Frames() <-chan music.Frame { return c.frames }
func (c *fakeCap) Info() music.StreamInfo {
	if c.opts.Mode == music.ModeSystem {
		return music.StreamInfo{Mode: c.opts.Mode, Device: "扬声器（假的）", Input: "float32 2 声道 48000Hz"}
	}
	return music.StreamInfo{Mode: c.opts.Mode, Input: "float32 2 声道 48000Hz"}
}

func (c *fakeCap) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *fakeCap) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.frames)
	}
	return c.err
}

func (c *fakeCap) stopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// push 推一帧，每个采样都是 v。
func (c *fakeCap) push(v int16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	s := make([]int16, music.FrameSamples)
	for i := range s {
		s[i] = v
	}
	c.frames <- music.Frame{Seq: c.seq, Samples: s}
	c.seq++
}

// die 采集中途出错（比如目标进程退出了）。
func (c *fakeCap) die(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
	if !c.closed {
		c.closed = true
		close(c.frames)
	}
}

type capFactory struct {
	mu   sync.Mutex
	caps []*fakeCap
	fail error
}

func (f *capFactory) start(o music.Options) (Capturer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	c := &fakeCap{opts: o, frames: make(chan music.Frame, 50)}
	f.caps = append(f.caps, c)
	return c, nil
}

func (f *capFactory) setFail(err error) {
	f.mu.Lock()
	f.fail = err
	f.mu.Unlock()
}

func (f *capFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.caps)
}

func eventOf(typ, inst string) events.Event {
	return events.Event{Type: typ, Data: map[string]any{"instance": inst}}
}

func (f *capFactory) last() *fakeCap {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.caps) == 0 {
		return nil
	}
	return f.caps[len(f.caps)-1]
}

var (
	win11 = music.Capability{Major: 10, Build: 26300, Mode: music.ModeProcess}
	win10 = music.Capability{Major: 10, Build: 19045, Mode: music.ModeSystem, Warn: true}
)

type env struct {
	t     *testing.T
	s     *Service
	inst  *fakeInst
	tun   *fakeTunnels
	caps  *capFactory
	dir   string // 实例「生存服」的目录
	data  string // data/music
	auto  chan string
	cap   music.Capability
	capMu sync.Mutex
}

const instName = "生存服"

// newEnv 一个停着的 NeoForge 1.21.1 实例「生存服」，Win11。
func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{t: t, inst: &fakeInst{}, tun: &fakeTunnels{}, caps: &capFactory{}, dir: filepath.Join(root, "srv"),
		data: filepath.Join(root, "data", "music"), auto: make(chan string, 8), cap: win11}
	e.inst.add(InstInfo{Name: instName, Dir: e.dir, Core: mcsrc.CoreNeoForge, MC: "1.21.1", Status: "stopped"})
	e.s = e.newService()
	return e
}

func (e *env) newService() *Service {
	return New(Deps{Instances: e.inst, Tunnels: e.tun, Dir: e.data,
		Detect: func() music.Capability { e.capMu.Lock(); defer e.capMu.Unlock(); return e.cap },
		ListSources: func() ([]music.Source, error) {
			return []music.Source{
				{PID: 4242, Exe: "cloudmusic.exe", Playing: true, Peak: 0.6, Devices: []string{"扬声器（默认）"}},
				{PID: 7, Exe: "javaw.exe", Playing: true, Peak: 0.3},
				{PID: 99, Exe: "QQMusic.exe", Peak: 0},
			}, nil
		},
		ListDevices: func() ([]music.Device, error) {
			return []music.Device{{ID: "{dev-1}", Name: "扬声器", Default: true}, {ID: "{dev-2}", Name: "虚拟声卡"}}, nil
		},
		StartCapture: e.caps.start,
		OnAutoStop:   func(inst, reason string, _ bool) { e.auto <- inst + "：" + reason },
		Subscribe:    func(func(events.Event)) {},
		clientOpts: func(o *bridge.Options) {
			o.RetryDelay = 100 * time.Millisecond
			o.Grace = 1500 * time.Millisecond
			o.BusyWait = 800 * time.Millisecond
		},
		tick: 40 * time.Millisecond, settle: 60 * time.Millisecond,
	})
}

func (e *env) setCap(c music.Capability) {
	e.capMu.Lock()
	e.cap = c
	e.capMu.Unlock()
}

// writeZip 写一个 jar（zip），files 是 名字 → 内容。
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// addSVC 在 mods 里放一个 NeoForge 版的 SVC（只有元数据）。
func (e *env) addSVC() {
	writeZip(e.t, filepath.Join(e.dir, "mods", "voicechat-neoforge-1.21.1-2.6.27.jar"), map[string]string{
		"META-INF/neoforge.mods.toml": "modLoader = \"javafml\"\n[[mods]]\nmodId = \"voicechat\"\nversion = \"1.21.1-2.6.27\"\n[[mods]]\nmodId = \"voicechat_api\"\nversion = \"2.6.27\"\n",
	})
}

// ready 装好 SVC 和桥接模组、写好指向假模组的配置，服务器在运行。返回假模组。
func (e *env) ready() *bridgetest.FakeMod {
	e.t.Helper()
	const token = "0123456789abcdef0123456789abcdef"
	fm, err := bridgetest.Start(0, token)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(fm.Close)
	fm.Listeners.Store(2)
	e.addSVC()
	body := fmt.Sprintf("enabled=true\nport=%d\ntoken=%s\nexclude=房主\n", fm.Port(), token)
	if err := bridge.WriteFileAtomic(bridge.ConfigPath(e.dir), []byte(body)); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.InstallBridge(instName, bridge.VariantMain); err != nil {
		e.t.Fatal(err)
	}
	e.inst.setStatus(instName, "running")
	return fm
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("等了 %v：%s", d, what)
	}
}

func (e *env) status() *Status {
	e.t.Helper()
	st, err := e.s.Status(instName)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func codeOf(err error) int {
	var me *Error
	if errors.As(err, &me) {
		return me.Code
	}
	return 0
}
