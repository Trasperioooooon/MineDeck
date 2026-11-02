// Package catalog 登记 MineDeck 的具体操作。网页接口只做 HTTP 适配（解析请求 → ops.Do → 写回响应），
// 以后的 AI 助手也从这里拿工具清单（设计文档 §5.2）。各操作的处理逻辑原样搬自原来的网页处理函数，
// 响应内容和状态码不变。
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/chatai"
	"minedeck/internal/companion"
	"minedeck/internal/dl"
	"minedeck/internal/inst"
	"minedeck/internal/musicshare"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// Env 操作要用到的东西，由 web.New 注入。
type Env struct {
	Mgr      *inst.Manager
	Tun      *tunnel.Manager
	AI       *ai.Service // nil 时按程序目录建一个（main 自己建好传进来，好在启动时调 Start）
	Port     int         // 实际监听端口
	Shutdown func()      // 退出程序（「退出 MineDeck」和自动更新用）

	// Music 音乐共享（第五阶段）；nil 时按程序目录建一个（Mgr、Tun 都有时）
	Music *musicshare.Service

	// GameChat 游戏内聊天 AI（第四阶段）；nil 时建一个（有 Mgr 时），服务器每输出一行都交给它认聊天。
	// Token 是 MineDeck 这次启动的访问令牌：和应用内助手一样，原样出现在要发给 AI 的内容里就换成 ***。
	GameChat *chatai.Service
	Token    string

	// Companion AI 伙伴（第七阶段）；nil 时建一个（有 Mgr 时）。任务做完或失败时经游戏内 AI 用模板通知发起的玩家。
	Companion *companion.Service
}

// NewAI 按程序目录建 AI 接入层：设置、价格缓存和用量放在 <程序目录>/data/ai/，Key 用 DPAPI 加密存进
// secrets.json（键名 ai.key），价格数据经 GitHub 加速镜像下载。只读设置，不联网。
func NewAI() *ai.Service {
	return ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai"), Keys: app.Secrets(), Mirrors: dl.GHMirrors})
}

type cat struct {
	Env
	reg *ops.Registry
}

// New 建好注册表并登记全部操作；操作记录写在 <程序目录>/logs/ops/。
// 定时任务和崩溃自动重启也经 inst 的回调各记一条（来源 schedule）。
func New(env Env) *ops.Registry {
	reg := ops.NewRegistry(ops.NewLog(filepath.Join(app.Base, "logs", "ops")))
	if env.AI == nil {
		env.AI = NewAI()
	}
	// 音乐共享（第五阶段）：广播自己停下时记一条操作记录（来源 schedule）；开服前由它写好桥接模组的配置和 voice_host
	if env.Music == nil && env.Mgr != nil && env.Tun != nil {
		env.Music = NewMusic(env.Mgr, env.Tun, func(name, reason string, failed bool) {
			var err error
			if failed {
				err = errors.New(reason)
			}
			reg.Record(ops.Caller{Source: ops.SrcSchedule, Actor: "自动停止", Local: true}, "music.stop",
				ops.ArgsOf(map[string]string{"inst": name, "reason": reason}), err, 0)
		})
	}
	var gc *chatai.Service                      // 伙伴的任务通知要经游戏内 AI 发：先建伙伴，等游戏内 AI 建好再接上
	if env.Companion == nil && env.Mgr != nil { // AI 伙伴（第七阶段）
		env.Companion = NewCompanion(env.Mgr, func(f companion.Finished) {
			if gc != nil {
				gc.NotifyTask(f)
			}
		})
	}
	if env.Mgr != nil { // 开服前：音乐共享写好桥接模组的配置和 voice_host，AI 伙伴补好伙伴模组的端口和令牌
		music, comp := env.Music, env.Companion
		env.Mgr.SetStartHook(func(name string) {
			if music != nil {
				music.BeforeStart(name)
			}
			if comp != nil {
				comp.BeforeStart(name)
			}
		})
	}
	if env.GameChat == nil && env.Mgr != nil { // 游戏内 AI（第四阶段）
		var comps chatai.Companions
		if env.Companion != nil {
			comps = env.Companion
		}
		env.GameChat = NewGameChat(env.Mgr, reg, env.AI, env.Token, comps)
	}
	gc = env.GameChat
	c := &cat{Env: env, reg: reg}
	c.appOps()
	c.instanceOps()
	c.backupOps()
	c.playerOps()
	c.resourceOps()
	c.mcOps()
	c.tunnelOps()
	c.logOps()
	c.aiOps()
	c.troubleshootOps()
	c.fileOps()
	c.portOps()
	c.musicOps()
	c.gameChatOps()  // 第四阶段
	c.companionOps() // 第七阶段
	if env.Mgr != nil {
		env.Mgr.SetAutoHook(func(a inst.AutoAction) {
			reg.Record(ops.Caller{Source: ops.SrcSchedule, Actor: a.Actor, Local: true}, a.Op, ops.ArgsOf(a.Args), a.Err, a.Dur)
		})
	}
	return reg
}

// bind 把参数解码进 T；解不开按 400 报（多半是类型不对）。
func bind[T any](a ops.Args) (T, error) {
	var v T
	if err := a.Bind(&v); err != nil {
		return v, ops.Wrap(400, err)
	}
	return v, nil
}

// instArgs 只带实例名的参数。
type instArgs struct {
	Inst string `json:"inst"`
}

var instName = ops.String("实例名称（实例列表里的 name）").Len(1, 100)

// withInst 参数说明：实例名 inst（必填）加上 props。
func withInst(props map[string]*ops.Schema, required ...string) *ops.Schema {
	p := map[string]*ops.Schema{"inst": instName}
	maps.Copy(p, props)
	return ops.Object(p, append([]string{"inst"}, required...)...)
}

// get 取实例；找不到按 404 报（原来直接调 Manager.Get 的接口是 404，其余经 Manager 方法报的是 400）。
func (c *cat) get(name string) (*inst.Instance, error) {
	i, err := c.Mgr.Get(name)
	return i, ops.Wrap(404, err)
}

// statusPre 写操作预览的前置条件：实例当前的状态（确认时状态变了，提案作废）。
func (c *cat) statusPre(name string) (*inst.Instance, map[string]string, error) {
	i, err := c.get(name)
	if err != nil {
		return nil, nil, err
	}
	return i, map[string]string{"实例状态": i.Status()}, nil
}

// fileHash 文件内容的指纹；文件不存在记为「无」。
func fileHash(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "无"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// onlineWarning 会让在线玩家掉线的操作，预览里写明当前在线人数。
func onlineWarning(i *inst.Instance, what string) string {
	if n := i.OnlineCount(); n > 0 {
		return fmt.Sprintf("%s：在线的 %d 人会掉线", what, n)
	}
	return what
}
