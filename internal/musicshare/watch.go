package musicshare

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// 从服务器控制台认出语音的情况：SVC 和桥接模组自己打的日志（英文那几行，不受控制台编码影响）。
// 依据 SVC 1.20.1 / 1.21.1 分支的源码：PluginManager、Server、ServerVoiceEvents。
var (
	reStartMarker   = regexp.MustCompile(`^\[MineDeck\] 启动:`)
	rePluginsLoaded = regexp.MustCompile(`Loaded \d+ plugin\(s\)`)
	reBridgeEvents  = regexp.MustCompile(`Registering events for 'minedeck_voicebridge'`)
	reVoiceStarted  = regexp.MustCompile(`Voice chat server started at (?:port )?(?:\S+:)?(\d+)`)
	reVoiceJoined   = regexp.MustCompile(`Player (\S+) \([0-9a-fA-F-]{32,36}\) successfully connected to voice chat`)
	reVoiceLeft     = regexp.MustCompile(`Disconnecting client (\S+)`)
	reSecret        = regexp.MustCompile(`Received secret request of (\S+) \((-?\d+)\)`)
	reIncompatible  = regexp.MustCompile(`Connected client (\S+) has incompatible voice chat version \(server=(-?\d+), client=(-?\d+)\)`)
)

// IncompatView 一位 SVC 版本不兼容的玩家。
type IncompatView struct {
	Name   string `json:"name"`
	Client int    `json:"client"` // 客户端的兼容号
	Server int    `json:"server"` // 服务端的兼容号
}

// voiceWatch 一次开服期间从控制台认出的情况。
type voiceWatch struct {
	mu            sync.Mutex
	full          bool // 从这次开服的第一行就在看（看不全时有些玩家可能漏掉）
	pluginsLoaded bool // SVC 加载完插件了
	bridgeLoaded  bool // SVC 加载了桥接模组
	voicePort     int  // SVC 实际监听的 UDP 端口
	voice         map[string]bool
	hasSVC        map[string]bool
	incompatible  map[string]IncompatView
	stop          chan struct{}
}

func newWatch() *voiceWatch {
	return &voiceWatch{voice: map[string]bool{}, hasSVC: map[string]bool{}, incompatible: map[string]IncompatView{}, stop: make(chan struct{})}
}

func (w *voiceWatch) handle(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case reBridgeEvents.MatchString(line):
		w.bridgeLoaded = true
	case rePluginsLoaded.MatchString(line):
		w.pluginsLoaded = true
	}
	if m := reVoiceStarted.FindStringSubmatch(line); m != nil {
		w.voicePort, _ = strconv.Atoi(m[1])
	}
	if m := reVoiceJoined.FindStringSubmatch(line); m != nil {
		w.voice[m[1]] = true
		w.hasSVC[m[1]] = true
		delete(w.incompatible, m[1])
	}
	if m := reVoiceLeft.FindStringSubmatch(line); m != nil {
		delete(w.voice, m[1])
	}
	if m := reSecret.FindStringSubmatch(line); m != nil {
		w.hasSVC[m[1]] = true
	}
	if m := reIncompatible.FindStringSubmatch(line); m != nil {
		srv, _ := strconv.Atoi(m[2])
		cli, _ := strconv.Atoi(m[3])
		w.incompatible[m[1]] = IncompatView{Name: m[1], Server: srv, Client: cli}
	}
}

// startWatch 开始看实例的控制台：回放里只看最后一次「[MineDeck] 启动:」之后的行。
func (s *Service) startWatch(name string, restart bool) *voiceWatch {
	s.mu.Lock()
	if w := s.watches[name]; w != nil {
		if !restart {
			s.mu.Unlock()
			return w
		}
		close(w.stop)
		delete(s.watches, name)
	}
	s.mu.Unlock()
	con, err := s.d.Instances.Console(name)
	if err != nil || con == nil {
		return nil
	}
	w := newWatch()
	replay, ch, cancel := con.Subscribe()
	start := -1
	for i := len(replay) - 1; i >= 0; i-- {
		if reStartMarker.MatchString(replay[i]) {
			start = i
			break
		}
	}
	w.full = start >= 0
	for _, l := range replay[start+1:] {
		w.handle(l)
	}
	s.mu.Lock()
	if old := s.watches[name]; old != nil { // 并发的另一次先登记了
		s.mu.Unlock()
		cancel()
		return old
	}
	s.watches[name] = w
	s.mu.Unlock()
	go func() {
		defer cancel()
		for {
			select {
			case <-w.stop:
				return
			case l := <-ch:
				w.handle(l)
			}
		}
	}()
	return w
}

func (s *Service) stopWatch(name string) {
	s.mu.Lock()
	w := s.watches[name]
	delete(s.watches, name)
	s.mu.Unlock()
	if w != nil {
		close(w.stop)
	}
}

// PlayersView 听众的情况。
type PlayersView struct {
	Known        bool           `json:"known"`        // 这次开服以后一直在看控制台（看不全时有些玩家可能漏掉）
	Voice        []string       `json:"voice"`        // 连上语音的在线玩家：会收到音乐
	Excluded     []string       `json:"excluded"`     // 连上了语音，但在排除名单里（广播者自己）
	NoVoice      []string       `json:"noVoice"`      // 在线、没连上语音：多半没装 SVC
	Incompatible []IncompatView `json:"incompatible"` // SVC 版本和服务端不兼容
}

func (w *voiceWatch) players(online []string, exclude []string) PlayersView {
	pv := PlayersView{Voice: []string{}, Excluded: []string{}, NoVoice: []string{}, Incompatible: []IncompatView{}}
	if w == nil {
		pv.NoVoice = append(pv.NoVoice, online...)
		return pv
	}
	ex := map[string]bool{}
	for _, n := range exclude {
		ex[strings.ToLower(n)] = true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	pv.Known = w.full
	for _, p := range online {
		switch inc, bad := w.incompatible[p]; {
		case w.voice[p] && ex[strings.ToLower(p)]:
			pv.Excluded = append(pv.Excluded, p)
		case w.voice[p]:
			pv.Voice = append(pv.Voice, p)
		case bad:
			pv.Incompatible = append(pv.Incompatible, inc)
		default:
			pv.NoVoice = append(pv.NoVoice, p)
		}
	}
	sort.Strings(pv.Voice)
	sort.Strings(pv.NoVoice)
	return pv
}
