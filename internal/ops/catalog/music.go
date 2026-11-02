package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/bridge"
	"minedeck/internal/inst"
	"minedeck/internal/musicshare"
	"minedeck/internal/ops"
)

// 音乐共享（设计文档 §8，第五阶段）。
//
// 风险等级：看状态是读操作；装桥接模组、改 voice_host 是普通的写操作（和装模组、改配置一样，AI 提出、人确认）。
// 开始和停止广播、列音源、改「我在游戏里的名字」和音量、确认整机录音都是「仅人工」：AI 永远不能替人开始放歌，
// 也碰不到这台电脑上有哪些程序在出声（§8.6）。开始广播、列音源、确认整机录音还「仅本机」：录的是这台电脑的声音，
// 要人坐在这台电脑前；局域网登录的设备可以看状态、停止广播。

// NewMusic 按程序目录建音乐共享：设置放在 <程序目录>/data/music/。广播自己停下（关服、音乐软件退出、
// 连不上桥接模组、退出 MineDeck）时，经 onAutoStop 记一条操作记录。
func NewMusic(mgr *inst.Manager, tun musicshare.Tunnels, onAutoStop func(inst, reason string, failed bool)) *musicshare.Service {
	return musicshare.New(musicshare.Deps{
		Instances:  musicshare.FromManager(mgr),
		Tunnels:    tun,
		Dir:        filepath.Join(app.Base, "data", "music"),
		OnAutoStop: onAutoStop,
	})
}

// musicErr 带上 musicshare 给的状态码（409：已经在广播、要先确认整机录音、运行中不能替换旧版本）。
func musicErr(err error) error {
	var me *musicshare.Error
	if errors.As(err, &me) {
		return ops.Wrap(me.Code, me.Err)
	}
	return err
}

type musicStartArgs struct {
	Inst   string `json:"inst"`
	Mode   string `json:"mode"`
	PID    uint32 `json:"pid"`
	Exe    string `json:"exe"`
	Device string `json:"device"`
}

type musicSettingsArgs struct {
	Inst    string  `json:"inst"`
	Exclude *string `json:"exclude"`
	Volume  *int    `json:"volume"`
}

type musicInstallArgs struct {
	Inst    string         `json:"inst"`
	Variant bridge.Variant `json:"variant"`
}

type voiceHostArgs struct {
	Inst       string `json:"inst"`
	Value      string `json:"value"`
	FromTunnel bool   `json:"fromTunnel"`
}

// orEmpty 预览里把空串写成「（空）」。
func orEmpty(s string) string {
	if s == "" {
		return "（空）"
	}
	return s
}

func (c *cat) musicOps() {
	const localStart = "音乐广播录的是这台电脑的声音，只能在这台电脑上开始"

	c.reg.Register(&ops.Op{
		Name: "music.status", Title: "查看音乐共享", Level: ops.Read,
		Desc: "实例「音乐共享」页的内容：核心支不支持、SVC 和桥接模组装没装、语音端口、远程语音隧道和 voice_host、" +
			"谁连上了语音（谁没装 SVC、谁的版本不兼容）、这台电脑能怎么录、正在不在广播、上一次广播怎么结束的、发给好友的说明。不改任何东西。",
		Params: withInst(nil),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			st, err := c.Music.Status(p.Inst)
			if err != nil {
				return nil, musicErr(err)
			}
			st.Local = caller.Local
			return st, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.sources", Title: "列出音源", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localStart, NoLog: true,
		Desc: "这台电脑上正在用声音的程序（带当时的电平）和播放设备，开始广播前选音源用。",
		Run:  func(context.Context, ops.Caller, ops.Args) (any, error) { return c.Music.Sources(), nil },
	})

	c.reg.Register(&ops.Op{
		Name: "music.start", Title: "开始音乐广播", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localStart,
		Desc: "录这台电脑上一个程序的声音（Win11）或整机的声音，经桥接模组和 SVC 播给服务器里的玩家。同一时间只能有一个广播；" +
			"整机录音要先确认过提醒（music.system.confirm）。",
		Params: withInst(map[string]*ops.Schema{
			"mode":   ops.String("采集方式：process 只录一个程序（Win11），system 整机录音").OneOf("process", "system"),
			"pid":    ops.Integer("只录一个程序时：它的进程号（见 music.sources）").Range(0, 4294967295),
			"exe":    ops.String("只录一个程序时：程序名（只用来显示，下次默认选它）").Len(0, 260),
			"device": ops.String("整机录音时：播放设备的 ID（见 music.sources）；不填用上次选的，没选过用系统默认").Len(0, 512),
		}, "mode"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[musicStartArgs](a)
			if err != nil {
				return nil, err
			}
			v, err := c.Music.Start(musicshare.StartReq{Inst: p.Inst, Mode: p.Mode, PID: p.PID, Exe: p.Exe, Device: p.Device})
			if err != nil {
				return nil, musicErr(err)
			}
			return v, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.stop", Title: "停止音乐广播", Level: ops.HumanOnly,
		Desc:   "停止给这个实例的广播：通知桥接模组放完缓冲里的一点点就停，停掉采集。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Music.Stop(p.Inst); err != nil {
				return nil, musicErr(err)
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.settings.set", Title: "修改音乐共享设置", Level: ops.HumanOnly,
		Desc: "只改传了的字段。exclude：我在游戏里的名字（几个名字用逗号隔开，空串清掉），写进桥接模组的配置，" +
			"这些人不会再收到一份音乐（自己已经在听原声），2 秒内生效；volume：广播的音量，正在广播时立刻生效。",
		Params: withInst(map[string]*ops.Schema{
			"exclude": ops.String("我在游戏里的名字（不把音乐发给这些人）").Len(0, 400),
			"volume":  ops.Integer("音量（%），100 为原样").Range(0, 200),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[musicSettingsArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Music.SetSettings(p.Inst, p.Exclude, p.Volume); err != nil {
				return nil, musicErr(err)
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.system.confirm", Title: "确认整机录音", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: "整机录音的提醒要在这台电脑上确认",
		Desc:     "确认过「这台电脑只能录整机的声音：游戏声音、语音聊天也会一起播出去」，每台电脑一次。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			if err := c.Music.ConfirmSystem(); err != nil {
				return nil, musicErr(err)
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.bridge.install", Title: "安装音乐桥接模组", Level: ops.Write,
		Desc: "把 MineDeck 内置的桥接模组装进实例的 mods，删掉别的版本，写好它的配置（打开共享、端口、令牌）。" +
			"要先装好 Simple Voice Chat；服务器运行中装的，重启后生效，运行中不能替换已经装着的。",
		Params: withInst(map[string]*ops.Schema{
			"variant": ops.String("加载方式：main 默认；javafml 备用（默认的在服务器上没被 SVC 加载时换这个）").OneOf("", "main", "javafml"),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[musicInstallArgs](a)
			if err != nil {
				return nil, err
			}
			pv, err := c.Music.PreviewInstall(p.Inst, p.Variant)
			if err != nil {
				return nil, musicErr(err)
			}
			plan := &ops.Plan{Pre: map[string]string{
				"实例状态":        pv.Status,
				"mods 里的桥接模组": strings.Join(append([]string{pv.Current.File}, pv.Current.Others...), "、"),
			}}
			if pv.Noop {
				plan.Summary = fmt.Sprintf("「%s」已经装着桥接模组 %s，只检查一遍它的配置", p.Inst, pv.File)
				return plan, nil
			}
			plan.Summary = fmt.Sprintf("给「%s」装桥接模组 %s（MineDeck 内置的 %s 版），写好它的配置", p.Inst, pv.File, bridge.JarVersion)
			if pv.Running {
				plan.Summary += "；服务器正在运行，重启后生效"
			}
			if pv.Current.File != pv.File {
				plan.Changes = append(plan.Changes, ops.Change{What: "mods/" + pv.File, Before: "无", After: "安装"})
			}
			for _, f := range pv.Remove {
				plan.Changes = append(plan.Changes, ops.Change{What: "mods/" + f, Before: "有", After: "删除"})
			}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[musicInstallArgs](a)
			if err != nil {
				return nil, err
			}
			res, err := c.Music.InstallBridge(p.Inst, p.Variant)
			if err != nil {
				return nil, musicErr(err)
			}
			return res, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "music.voicehost.set", Title: "修改语音地址（voice_host）", Level: ops.Write,
		Desc: "改 SVC 配置里的 voice_host（远程好友连语音用的地址），只改这一行，改前备份。fromTunnel 为 true 时用语音隧道的公网地址" +
			"（以后隧道地址变了也会跟着改）；否则用 value（手动填的，MineDeck 不会自动覆盖；空串清掉，只有局域网好友时不需要）。SVC 只在启动时读，运行中改的重启后生效。",
		Params: withInst(map[string]*ops.Schema{
			"value":      ops.String("语音地址，如 frp-xxx.com:12345；fromTunnel 时不用填").Len(0, 300),
			"fromTunnel": ops.Boolean("用语音隧道的公网地址"),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[voiceHostArgs](a)
			if err != nil {
				return nil, err
			}
			ch, sum, err := c.Music.VoiceHostPlan(p.Inst, p.Value, p.FromTunnel)
			if err != nil {
				return nil, musicErr(err)
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			plan := &ops.Plan{Pre: map[string]string{"SVC 的配置文件": sum, "实例状态": i.Status(), "要写的地址": ch.After}}
			if !ch.Changed {
				plan.Summary = fmt.Sprintf("「%s」的 voice_host 已经是「%s」，不用改", p.Inst, orEmpty(ch.After))
				return plan, nil
			}
			plan.Summary = fmt.Sprintf("把「%s」的 voice_host 从「%s」改成「%s」", p.Inst, orEmpty(ch.Before), orEmpty(ch.After))
			if ch.Restart {
				plan.Summary += "；服务器正在运行，重启后生效"
			}
			plan.Changes = []ops.Change{{What: "voice_host", Before: orEmpty(ch.Before), After: orEmpty(ch.After)}}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[voiceHostArgs](a)
			if err != nil {
				return nil, err
			}
			ch, err := c.Music.SetVoiceHost(p.Inst, p.Value, p.FromTunnel)
			if err != nil {
				return nil, musicErr(err)
			}
			return ch, nil
		},
	})
}
