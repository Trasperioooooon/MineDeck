package musicshare

import (
	"fmt"
	"sort"
	"strings"

	"minedeck/internal/bridge"
)

// BridgeView 桥接模组的情况。
type BridgeView struct {
	bridge.JarStatus
	Config *bridge.Config `json:"config,omitempty"` // 它的配置（令牌不下发）；还没有配置文件时为空
	// Loaded 服务器运行中时，控制台里有没有 SVC 加载它的日志：true 加载了；false SVC 加载完插件了却没有它
	// （加载方式不认，可以换备用加载方式试试）；没有这一项表示还不知道（没在运行、或者 SVC 还没加载完）。
	Loaded *bool `json:"loaded,omitempty"`
}

// SettingsView 这个实例的音乐共享设置。
type SettingsView struct {
	Exclude string `json:"exclude"` // 我在游戏里的名字（排除名单，可以有好几个）
	Volume  int    `json:"volume"`
	Mode    string `json:"mode,omitempty"` // 上次用的采集方式
	Exe     string `json:"exe,omitempty"`  // 上次选的程序
}

// Status 实例「音乐共享」页的全部内容。
type Status struct {
	Inst            string         `json:"inst"`
	Supported       bool           `json:"supported"`
	Reason          string         `json:"reason,omitempty"` // 不支持的原因
	Note            string         `json:"note,omitempty"`
	ServerStatus    string         `json:"serverStatus"`
	SVC             SVCInfo        `json:"svc"`
	Bridge          BridgeView     `json:"bridge"`
	Voice           VoiceConfig    `json:"voice"`
	VoicePort       int            `json:"voicePort,omitempty"` // 这次开服 SVC 实际监听的 UDP 端口（从控制台认出来的）
	Remote          RemoteView     `json:"remote"`
	Players         PlayersView    `json:"players"`
	Capability      CapView        `json:"capability"`
	SystemConfirmed bool           `json:"systemConfirmed"`
	Settings        SettingsView   `json:"settings"`
	Broadcast       *BroadcastView `json:"broadcast,omitempty"` // 正在给这个实例广播
	Elsewhere       string         `json:"elsewhere,omitempty"` // 正在给别的实例广播：那个实例的名字
	Last            *EndView       `json:"last,omitempty"`      // 上一次广播是怎么结束的
	Restart         []string       `json:"restart,omitempty"`   // 要重启服务器才生效的改动
	Guide           string         `json:"guide"`               // 发给好友的说明
	// Local 看的人在不在这台电脑前（操作层按调用方填）：不在时界面上不列音源、不给开始广播
	Local bool `json:"local"`
}

// Status 汇总实例「音乐共享」页要显示的东西：检查清单、广播状态、听众。只读，不改任何文件。
func (s *Service) Status(name string) (*Status, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	st := &Status{Inst: name, ServerStatus: info.Status}
	st.Supported, st.Reason = supportOf(info.Core, info.MC)
	if st.Supported && info.Core == "mohist" {
		st.Note = "Mohist 上还没测过音乐共享"
	}
	st.SVC = s.detectSVC(info.Dir)
	jar, _ := bridge.InspectJar(modsDir(info.Dir))
	st.Bridge = BridgeView{JarStatus: jar}
	if c, err := bridge.ReadConfig(info.Dir); err == nil {
		st.Bridge.Config = &c
		st.Settings.Exclude = c.Exclude
	}
	st.Voice = readVoiceConfig(info.Dir)
	st.Remote = s.remoteView(name, st.Voice)
	st.Capability = s.capability()

	var w *voiceWatch
	if info.Status == "running" || info.Status == "starting" {
		w = s.startWatch(name, false)
	}
	if w != nil {
		w.mu.Lock()
		st.VoicePort = w.voicePort
		if w.bridgeLoaded {
			st.Bridge.Loaded = ptr(true)
		} else if w.pluginsLoaded && w.full && jar.Installed {
			st.Bridge.Loaded = ptr(false)
		}
		w.mu.Unlock()
	}
	st.Players = w.players(info.Online, bridge.ExcludeNames(st.Settings.Exclude))

	s.mu.Lock()
	st.SystemConfirmed = s.set.SystemConfirmed
	is := s.set.Instances[name]
	st.Settings.Volume = is.volume()
	if is != nil {
		st.Settings.Mode, st.Settings.Exe = is.Mode, is.Exe
	}
	b := s.cur
	if last, ok := s.last[name]; ok {
		st.Last = &last
	}
	for k := range s.restart[name] {
		st.Restart = append(st.Restart, map[string]string{restartBridge: "桥接模组", restartVoiceHost: "voice_host"}[k])
	}
	s.mu.Unlock()
	sort.Strings(st.Restart)
	if b != nil {
		if b.inst == name {
			st.Broadcast = b.view()
		} else {
			st.Elsewhere = b.inst
		}
	}
	st.Guide = guide(st)
	return st, nil
}

func ptr[T any](v T) *T { return &v }

// guide 一段可以直接发给好友的说明：装哪个版本的 SVC、在哪里单独静音、远程好友连哪个地址。
func guide(st *Status) string {
	var b strings.Builder
	b.WriteString("服务器开了「MineDeck 音乐共享」：房主电脑上放的歌，会通过 Simple Voice Chat（简单语音聊天）播给大家。\n")
	if st.SVC.Installed && st.SVC.Version != "" {
		fmt.Fprintf(&b, "1. 客户端装 Simple Voice Chat，和服务器同一个版本最稳妥：服务器装的是 %s。\n", st.SVC.Version)
	} else {
		b.WriteString("1. 客户端装 Simple Voice Chat，版本和服务器的一样（问房主要）。\n")
	}
	b.WriteString("2. 进服后按 V 打开语音菜单，点「调整音量」，找到「MineDeck 音乐共享」：拉到 0 就只静音音乐，不影响别人说话。\n")
	if st.Voice.VoiceHost != "" {
		fmt.Fprintf(&b, "3. 远程连服的朋友：语音走 %s，不用另外设置；连不上语音时告诉房主。\n", st.Voice.VoiceHost)
	}
	return strings.TrimRight(b.String(), "\n")
}
