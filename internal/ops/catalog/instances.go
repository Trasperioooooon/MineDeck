package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
	"minedeck/internal/ops"
)

type instSummary struct {
	Name            string   `json:"name"`
	Core            string   `json:"core"`
	Kind            string   `json:"kind"`
	MC              string   `json:"mc"`
	Build           string   `json:"build"`
	JavaMajor       int      `json:"javaMajor"`
	JavaSource      string   `json:"javaSource"` // portable：MineDeck 的便携版；local：本机装的 Java
	JavaPath        string   `json:"javaPath"`   // 现在用的 java.exe（「设置 → 启动与内存」里换 Java 时标出当前这一个）
	Adopted         bool     `json:"adopted"`    // 接管进来的：目录是用户原来的文件夹（删除时默认不删文件）
	XmxMB           int      `json:"xmxMb"`
	XmsMB           int      `json:"xmsMb"`
	Port            int      `json:"port"`
	Status          string   `json:"status"`
	MOTD            string   `json:"motd"`
	Dir             string   `json:"dir"`
	CreatedAt       string   `json:"createdAt"`
	ConsoleEncoding string   `json:"consoleEncoding"`
	OnlineCount     int      `json:"onlineCount"`
	UptimeSec       int      `json:"uptimeSec"`
	MaxPlayers      int      `json:"maxPlayers"`
	ExtraJVM        []string `json:"extraJvm"`
	// ConnectPort 朋友在游戏里连这台服务器用的端口（界面上的「连接地址」）：游戏服务器同 port；
	// 代理端是它自己配置文件里的监听端口（port 对代理端恒为 25565，不是它监听的），配置文件还没生成时为 0
	ConnectPort int `json:"connectPort"`
	// LastStartError 上一次启动没起来的原因（端口被占、找不到 Java……）；下次成功起了进程就清掉，只在内存里
	LastStartError string `json:"lastStartError,omitempty"`
	// StartPortBusy 上一次没启动起来是因为端口被占着（R08）：哪个端口、占着的是不是本软件的另一台服务器、能改成哪个。
	// 界面按它给「改成 N 并启动」或「改端口」，不去解析 lastStartError 那句话。跟着 lastStartError 一起清
	StartPortBusy *inst.PortBusy `json:"startPortBusy,omitempty"`
	// MineDeckFiles 接管来的服务器：文件夹里 MineDeck 写的、移出列表以后会留下的文件（相对路径）。别的服务器没有这一项
	MineDeckFiles []string `json:"mdFiles,omitempty"`
	// JavaMissing 配的 java.exe 现在找不到了（卸载了、挪走了）：卡片和详情页头常驻警告，带路去「设置 → 启动与内存」（可用性走查 R21）
	JavaMissing bool `json:"javaMissing,omitempty"`
	// BackupDir 这台服务器的新备份放在哪个文件夹（当前备份根下面，还没备份过时可能还不存在）
	BackupDir string `json:"backupDir"`
	// WorldFrom 现在的世界是从哪份备份还原来的（R11）；没还原过、还原以后换过地图时没有这一项
	WorldFrom *inst.WorldFrom `json:"worldFrom,omitempty"`
}

func summarize(i *inst.Instance) instSummary {
	motd := ""
	maxP := 20
	if p, err := inst.LoadProps(i.PropsPath()); err == nil {
		motd, _ = p.Get("motd")
		if v, ok := p.Get("max-players"); ok {
			if n, e := strconv.Atoi(strings.TrimSpace(v)); e == nil && n > 0 {
				maxP = n
			}
		}
	}
	xmx, xms, enc := i.MemSnapshot() // 加锁快照，避免与并发的 UpdateSettings 写入构成数据竞争
	javaMajor, javaPath := i.JavaSnapshot()
	javaSource := "local"
	if rel, err := filepath.Rel(app.RuntimesDir, javaPath); err == nil && !strings.HasPrefix(rel, "..") {
		javaSource = "portable"
	}
	if enc == "" {
		enc = "auto"
	}
	return instSummary{
		Name: i.Name, Core: string(i.Core), Kind: string(mcsrc.KindOf(i.Core)),
		MC: i.MC, Build: i.Build,
		JavaMajor: javaMajor, JavaSource: javaSource, JavaPath: javaPath, Adopted: i.Adopted, XmxMB: xmx, XmsMB: xms,
		Port: i.Port(), Status: i.Status(), MOTD: motd, Dir: i.Dir,
		CreatedAt:       i.CreatedAt.Format("2006-01-02 15:04"),
		ConsoleEncoding: enc,
		OnlineCount:     i.OnlineCount(), UptimeSec: i.UptimeSec(), MaxPlayers: maxP,
		ExtraJVM:    i.ExtraJVMSnapshot(),
		ConnectPort: i.ConnectPort(), LastStartError: i.LastStartError(), StartPortBusy: i.LastStartPortBusy(), MineDeckFiles: i.MineDeckFiles(),
		JavaMissing: i.JavaMissing(), BackupDir: i.BackupDir(), WorldFrom: i.WorldFromSnapshot(),
	}
}

// createParams 新建实例、导入整合包共用的参数说明。
var createParams = map[string]*ops.Schema{
	"name":        ops.String("实例名称，1–40 个字，不能含 \\ / : * ? \" < > |"),
	"core":        ops.String("核心 id（见 mc.cores），如 paper、neoforge、forge、fabric、vanilla"),
	"mc":          ops.String("Minecraft 版本（见 mc.versions）"),
	"build":       ops.String("构建号或加载器版本（见 mc.builds）；空则取推荐的"),
	"xmxMb":       ops.Integer("最大内存（MB），低于 512 按 2048 算"),
	"port":        ops.Integer("端口，不在 1–65535 内按 25565 算"),
	"eula":        ops.Boolean("是否同意 Minecraft EULA（必须由用户本人同意）"),
	"onlineMode":  ops.Boolean("正版验证"),
	"allowFlight": ops.Boolean("允许飞行"),
	"motd":        ops.String("服务器列表里显示的简介"),
	"difficulty":  ops.String("难度：peaceful、easy、normal、hard；空则用服务端默认"),
	"gamemode":    ops.String("默认游戏模式：survival、creative、adventure、spectator；空则用服务端默认"),
	"whitelist":   ops.Boolean("只允许白名单里的玩家进入（写进 server.properties 的 white-list，不论版本）；不传为关，任何知道地址的人都能进"),
	"root":        ops.String("存放目录（仅本机生效；空则用全局默认）"),
	"worldPlan":   ops.String("用已有地图开服：地图导入的计划编号（world.import.plan 生成，只能由人选择）"),
	"worldAck":    ops.Boolean("地图预览里要确认的项已确认"),
}

func (c *cat) instanceOps() {
	c.taskCancelOps()
	c.reg.Register(&ops.Op{
		Name: "instance.list", Title: "查看服务器列表", Level: ops.Read,
		Desc: "所有实例：核心、版本、Java、内存、端口、状态、在线人数、运行时长、MOTD；connectPort 是玩家连进来用的端口（代理端以它为准），" +
			"lastStartError 是上一次没启动起来的原因（端口被占、找不到 Java 等，下次启动成功后清空），端口被占时 startPortBusy 写着端口、" +
			"占着它的是哪台服务器（owner，空是别的程序）、可以改成的端口（suggest）；javaMissing 为 true 表示配的 java.exe 现在找不到了；" +
			"worldFrom 是现在的世界从哪份备份还原来的（没有就是没还原过，或者还原以后换过地图）。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			out := []instSummary{}
			for _, i := range c.Mgr.List() {
				out = append(out, summarize(i))
			}
			return out, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.create", Title: "新建服务器", Level: ops.Write,
		Desc: "在后台下载核心与 Java 并生成配置，返回任务编号（进度见 task.get）。Minecraft EULA 由用户在确认卡片上自己勾选同意，" +
			"不用也不能替用户传；存放目录用默认的。",
		Params: ops.Object(createParams),
		// EULA 只能由人同意（§5.1）、存放目录只能由人指定：AI 提出时这两项去掉，确认卡片上由用户自己勾选同意
		HumanParams: []string{"eula", "root", "worldPlan", "worldAck"},
		Consent: &ops.Consent{Param: "eula", Text: "我已阅读并同意 Minecraft EULA", URL: "https://aka.ms/MinecraftEULA",
			Need: func(a ops.Args) bool {
				req, _ := bind[inst.CreateReq](a)
				return mcsrc.KindOf(req.Core) != mcsrc.KindProxy // 代理端不运行 Minecraft 本体，不用同意 EULA
			}},
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			req, err := bind[inst.CreateReq](a)
			if err != nil {
				return nil, err
			}
			taken := "未占用"
			if _, err := c.Mgr.Get(strings.TrimSpace(req.Name)); err == nil {
				taken = "已占用"
			}
			return &ops.Plan{Summary: fmt.Sprintf("新建服务器「%s」（%s %s）", req.Name, req.Core, req.MC),
				Pre: map[string]string{"服务器名称": taken}}, nil
		},
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			req, err := bind[inst.CreateReq](a)
			if err != nil {
				return nil, err
			}
			if !caller.Local || caller.Source.AI() {
				req.Root = "" // 远程 / 局域网请求和 AI 都不得指定主机任意路径，强制落到默认根，防越界写入
			}
			if req.WorldPlan != "" { // 用已有地图开服：计划只认发起检测的那个人，核心和版本要对得上
				if err := c.worldCreateCheck(caller, req.WorldPlan, req.Core, req.MC, req.WorldAck); err != nil {
					return nil, err
				}
				req.WorldOwner = string(caller.Source) + "|" + caller.Actor
			}
			id, err := c.Mgr.CreateAsync(req)
			if err != nil {
				if req.WorldPlan != "" { // 建服没能开始（重名之类）：放掉刚收下的导入计划（没过期会放回去，改一改还能接着用）
					c.World.ReleaseCreate(req.WorldOwner, req.WorldPlan)
				}
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})

	importParams := map[string]*ops.Schema{
		"file":     ops.String("已上传到缓存目录的整合包文件"),
		"fileName": ops.String("上传时的原文件名（只用来显示，不参与任何路径）"),
	}
	for k, v := range createParams {
		if k == "name" || k == "xmxMb" || k == "port" || k == "eula" || k == "onlineMode" || k == "allowFlight" || k == "motd" || k == "root" || k == "whitelist" {
			importParams[k] = v
		}
	}
	importParams["localSource"] = ops.String("本地来源目录（可选）：用户还留着的老服务器目录，待决文件凡在这里有的按原样保留，不要求线上匹配（仅本机生效）")
	c.reg.Register(&ops.Op{
		Name: "instance.import", Title: "导入整合包", Level: ops.HumanOnly,
		Desc: "从已上传的 Modrinth / CurseForge 整合包创建实例（核心、版本、加载器由整合包决定），返回任务编号。" +
			"没有清单、但是 mods、plugins、server.properties 或服务端 jar 的压缩包，按现成服务端解压，核心和版本用接管已有服务器的同一套识别。",
		Params: ops.Object(importParams, "file"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				File     string `json:"file"`
				FileName string `json:"fileName"`
				inst.CreateReq
			}](a)
			if err != nil {
				return nil, err
			}
			// 只认网页接口刚存进缓存目录的上传文件，不能拿它去读主机上别的文件
			if filepath.Dir(filepath.Clean(p.File)) != filepath.Join(app.CacheDir, "import") {
				return nil, fmt.Errorf("整合包文件不在上传文件夹里")
			}
			pack, err := modpack.Parse(p.File)
			if err != nil {
				os.Remove(p.File)
				return nil, err
			}
			if pack.Format == modpack.FormatServer && pack.Root == "" {
				if name := modpack.DisplayNameFromUpload(p.FileName); name != "" {
					pack.Name = name
				}
			}
			req := p.CreateReq
			if !caller.Local {
				// 同 instance.create：远程 / 局域网请求不得指定主机任意路径（存放根、本地来源目录）
				req.Root, req.LocalSource = "", ""
			}
			id, err := c.Mgr.ImportModpackAsync(pack, req, app.CFKey())
			if err != nil {
				os.Remove(p.File)
				return nil, err
			}
			return map[string]any{
				"taskId": id,
				"pack": map[string]any{
					"name": pack.Name, "mc": pack.MC, "core": pack.Core, "loader": pack.Loader,
					"format": pack.Format, "files": len(pack.Files) + len(pack.CFRefs), "pending": len(pack.Pending),
				},
			}, nil
		},
	})

	// pendingResolveMax 一次「处理没装上的文件」最多带多少项。整合包里白名单外的文件可以有几百个，
	// 50 会让界面一次提交就报「参数 items 最多 50 项」。500 接受、501 拒绝。界面按这个数分批提交。
	// 不因此把 forgecdn 放进全局白名单：每一项仍要用户选 trust，并按原来的规则确认站点、校验 SHA-1。
	const pendingResolveMax = 500

	// 逐项处置导入的待决文件：跳过、保留本地已有 / 手动指定的文件、确认信任自定义站点后下载。
	// 触到主机上的任意路径（local 的路径、来源目录），和接管一样只许本机的人操作。
	const importPendingLocalMsg = "处理没装上的文件仅支持在本机操作（要读、要收这台电脑上的文件）"
	c.reg.Register(&ops.Op{
		Name: "instance.import.pending", Title: "处理导入任务里没装上的文件", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: importPendingLocalMsg,
		Desc: "整合包导入完成后的待决文件（白名单外的下载地址、缺校验值、下载后校验不符、CurseForge 未解析）逐个处置：" +
			"skip 跳过并记录、local 用本地已有的文件或手动指定的文件原样代替（不校验）、trust 确认信任自定义站点后下载（必须 https、公网，下载后过 SHA-1 校验）。" +
			"处置成功的那几项移出清单，失败的留在清单里；一批里单项失败不影响其他项。",
		Params: ops.Object(map[string]*ops.Schema{
			"taskId": ops.String("导入任务编号（GET /api/tasks/{id} 的 pending 字段列有待决文件）"),
			"items": ops.Array("一批处置动作", ops.Object(map[string]*ops.Schema{
				"id":   ops.String("待决文件编号（p1、p2…）"),
				"act":  ops.String("处理方式：skip 跳过、local 用本地文件代替、trust 信任站点后下载").OneOf("skip", "local", "trust"),
				"path": ops.String("act=local：本地文件路径，或目录路径（按待决文件的相对路径和 mods 下的同名文件找）").Len(0, 300),
				"url":  ops.String("act=trust：从候选地址的哪一条下载（要和待决记录里列出的一致）").Len(0, 2000),
				"ack":  ops.Boolean("act=trust：用户已读过风险说明、确认信任这个站点（每个站点第一次必须确认）"),
			}, "id", "act")).MaxLen(pendingResolveMax),
		}, "taskId", "items"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				TaskID string               `json:"taskId"`
				Items  []inst.PendingAction `json:"items"`
			}](a)
			if err != nil {
				return nil, err
			}
			resolved, pending, err := c.Mgr.ResolveImportPending(ctx, p.TaskID, p.Items)
			if err != nil {
				if strings.HasPrefix(err.Error(), "没有这个任务") {
					return nil, ops.Errorf(404, "任务不存在")
				}
				return nil, err
			}
			return map[string]any{"resolved": resolved, "pending": pending}, nil
		},
	})

	// 按服务器查、处置整合包导入时没装上的文件（可用性走查 R09）：清单存在服务器文件夹里，程序重开以后也在，
	// 不再只挂在这次运行的导入任务上。清单是从服务器文件夹里读的不可信文字，不给 AI 看（仅人工）；
	// 查看不限本机（局域网打开的管理页也能看到缺了什么），处置和上面按任务的那个一样只许本机。
	c.reg.Register(&ops.Op{
		// NoLog：只读。详情页每换一次标签都查一次（页头那一条「N 个整合包文件没装上」，R09），记进操作记录只会把真正的修改淹掉
		Name: "instance.import.pending.get", Title: "查看整合包里没装上的文件", Level: ops.HumanOnly, NoLog: true,
		Desc: "一台服务器导入整合包时没能自动装上的文件：pending 是还没处理的，skipped 是跳过了的（也没装上，还能改成别的处理）。" +
			"每一项的 options 是现在能用的处理方式。没有的话两个列表都是空的。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst string `json:"inst"`
			}](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			return c.Mgr.ImportPending(p.Inst)
		},
	})
	c.reg.Register(&ops.Op{
		Name: "instance.import.pending.resolve", Title: "处理整合包里没装上的文件", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: importPendingLocalMsg,
		Desc: "按服务器处置导入整合包时没装上的文件，程序重开以后也能用（不需要导入任务还在）。处理方式同 instance.import.pending：" +
			"skip 跳过（留在清单里标成已跳过，以后还能改成别的处理）、local 用本地文件原样代替、trust 确认信任站点后下载。" +
			"落点每一次都重新检查（只许 mods、plugins、config 这类九个文件夹，不许世界、server.properties 和 MineDeck 自己的文件），清单里写的不作数。" +
			"返回处置结果和处置以后的清单。",
		Params: withInst(map[string]*ops.Schema{
			"items": ops.Array("一批处置动作", ops.Object(map[string]*ops.Schema{
				"id":   ops.String("文件编号（p1、p2…，见 instance.import.pending.get）"),
				"act":  ops.String("处理方式：skip 跳过、local 用本地文件代替、trust 信任站点后下载").OneOf("skip", "local", "trust"),
				"path": ops.String("act=local：本地文件路径，或目录路径（按文件的相对路径和 mods 下的同名文件找）").Len(0, 300),
				"url":  ops.String("act=trust：从候选地址的哪一条下载（要和清单里列出的一致）").Len(0, 2000),
				"ack":  ops.Boolean("act=trust：用户已读过风险说明、确认信任这个站点（每个站点第一次必须确认）"),
			}, "id", "act")).MaxLen(pendingResolveMax),
		}, "items"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst  string               `json:"inst"`
				Items []inst.PendingAction `json:"items"`
			}](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			resolved, view, err := c.Mgr.ResolveInstancePending(ctx, p.Inst, p.Items)
			if err != nil {
				return nil, err
			}
			return map[string]any{"resolved": resolved, "pending": view.Pending, "skipped": view.Skipped}, nil
		},
	})

	c.adoptOps()

	c.reg.Register(&ops.Op{
		Name: "task.list", Title: "查看后台任务", Level: ops.Read,
		Desc: "本次运行的新建实例、整合包导入、接管已有服务器、Java 下载、交接包导入导出任务，按创建时间从新到旧排列，包含已完成、失败和用户取消的任务。" +
			"每个任务只给摘要（标题、是否结束、当前进度、出错原因、告警条数），步骤、日志和告警明细用 task.get 查。" +
			"cancelled 为 true 的是用户取消的（error 里是一句说明，不是出错），cancelling 为 true 的正在取消、清理半成品。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			return c.Mgr.Tasks.List(), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "task.get", Title: "查看任务进度", Level: ops.Read,
		Desc:   "新建实例、导入整合包、接管已有服务器、换 Java、交接包导入导出的后台任务：各步骤状态、下载进度、日志、告警。",
		Params: ops.Object(map[string]*ops.Schema{"id": ops.String("任务编号")}, "id"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID string `json:"id"`
			}](a)
			if err != nil {
				return nil, err
			}
			t := c.Mgr.Tasks.Get(p.ID)
			if t == nil {
				return nil, ops.Errorf(404, "任务不存在")
			}
			return t.Snapshot(), nil
		},
	})

	// 启停：预览以实例状态为前置条件，停服、强杀写明在线人数
	lifecycle := []struct {
		name, title string
		level       ops.Level
		desc        string
		run         func(name string) error
		plan        func(i *inst.Instance) (summary, warning string)
	}{
		{"instance.start", "启动服务器", ops.Write, "启动实例；绑定在它上面、开了跟随启动的隧道会跟着拉起。",
			func(name string) error { return c.Mgr.Start(name) }, // 隧道跟随启停在 tunnel.FollowInstances 里（崩溃自动重启也算）
			func(i *inst.Instance) (string, string) { return fmt.Sprintf("启动「%s」", i.Name), "" }},
		{"instance.stop", "停止服务器", ops.Danger, "发送 stop 命令，保存世界后停止；30 秒没停下就强制结束。开了跟随启动的隧道随后跟着停。",
			func(name string) error { return c.Mgr.Stop(name) },
			func(i *inst.Instance) (string, string) {
				return fmt.Sprintf("停止「%s」（先保存世界）", i.Name), onlineWarning(i, "停服")
			}},
		{"instance.kill", "强制结束服务器", ops.Danger, "立即结束进程，不保存世界。",
			func(name string) error { return c.Mgr.Kill(name) },
			func(i *inst.Instance) (string, string) {
				return fmt.Sprintf("强制结束「%s」", i.Name), onlineWarning(i, "不会保存世界，可能丢失最近的进度")
			}},
	}
	for _, l := range lifecycle {
		l := l
		c.reg.Register(&ops.Op{
			Name: l.name, Title: l.title, Level: l.level, Desc: l.desc, Params: withInst(nil),
			Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
				p, err := bind[instArgs](a)
				if err != nil {
					return nil, err
				}
				i, pre, err := c.statusPre(p.Inst)
				if err != nil {
					return nil, err
				}
				summary, warning := l.plan(i)
				return &ops.Plan{Summary: summary, Warning: warning, Pre: pre}, nil
			},
			Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
				p, err := bind[instArgs](a)
				if err != nil {
					return nil, err
				}
				if err := l.run(p.Inst); err != nil {
					return nil, err
				}
				return "ok", nil
			},
		})
	}

	c.reg.Register(&ops.Op{
		Name: "instance.restart", Title: "重启服务器", Level: ops.Danger, Params: withInst(nil),
		Desc: "先保存世界并停止（最多等 40 秒），停下来之后再启动。改了配置、换了 Java、禁用了模组之后，用它让改动生效。服务器要在运行。",
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			i, pre, err := c.statusPre(p.Inst)
			if err != nil {
				return nil, err
			}
			if st := i.Status(); st != "running" && st != "starting" {
				return nil, ops.Errorf(400, "「%s」没在运行，不用重启；要启动请用 instance.start", i.Name)
			}
			return &ops.Plan{Summary: fmt.Sprintf("重启「%s」（先保存世界）", i.Name), Warning: onlineWarning(i, "重启"), Pre: pre}, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.Restart(p.Inst); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	type cmdArgs struct {
		Inst string `json:"inst"`
		Cmd  string `json:"cmd"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.command", Title: "执行控制台命令", Level: ops.Danger,
		Desc:   "向服务器控制台发送一条命令（不带开头的 /）。控制台命令能做任何事。",
		Params: withInst(map[string]*ops.Schema{"cmd": ops.String("命令，如 say hi")}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[cmdArgs](a)
			if err != nil {
				return nil, err
			}
			_, pre, err := c.statusPre(p.Inst)
			if err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: fmt.Sprintf("在「%s」的控制台执行：%s", p.Inst, strings.TrimSpace(p.Cmd)),
				Warning: "控制台命令可以做任何事，请确认命令无误", Pre: pre}, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[cmdArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.Command(p.Inst, p.Cmd); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.delete", Title: "删除服务器", Level: ops.HumanOnly,
		Desc:   "从列表移除实例；files 为 true 时连实例目录一起删掉。服务器必须已停止。",
		Params: withInst(map[string]*ops.Schema{"files": ops.Boolean("同时删除实例目录")}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst  string `json:"inst"`
				Files bool   `json:"files"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.Delete(p.Inst, p.Files); err != nil {
				return nil, err
			}
			// 这台服务器的购物车跟着清掉：购物车按实例名存，不清的话以后新建一台同名的，
			// 旧车里剩下的东西会被当成新服务器的。人从电脑上添加、暂存着的本地文件也一起删。
			if c.ModCart != nil {
				_ = c.ModCart.ClearAll(p.Inst)
			}
			return "ok", nil
		},
	})

	// 与 host.pickdir / host.openpath 同理：资源管理器窗口开在主机桌面上，对远程用户无意义，故仅限本机
	c.reg.Register(&ops.Op{
		Name: "instance.opendir", Title: "打开服务器文件夹", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "打开文件夹仅支持在本机操作（远程管理时请在主机上操作）",
		Desc:     "在主机的资源管理器里打开实例目录，或它下面的 mods、plugins、world、logs、crash-reports。",
		Params:   withInst(map[string]*ops.Schema{"sub": ops.String("子目录；空则打开实例目录本身")}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst string `json:"inst"`
				Sub  string `json:"sub"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.OpenDir(p.Inst, p.Sub); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	type settingsArgs struct {
		Inst            string    `json:"inst"`
		XmxMB           int       `json:"xmxMb"`
		XmsMB           int       `json:"xmsMb"`
		ConsoleEncoding string    `json:"consoleEncoding"`
		ExtraJVM        *[]string `json:"extraJvm"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.settings.set", Title: "修改内存与启动参数", Level: ops.Write,
		Desc: "改最大 / 最小内存、控制台编码、自定义 JVM 参数，并重新生成启动脚本；下次启动生效。",
		Params: withInst(map[string]*ops.Schema{
			"xmxMb":           ops.Integer("最大内存（MB），不低于 512；0 表示不改"),
			"xmsMb":           ops.Integer("最小内存（MB）；不合理时按 min(1024, 最大内存) 算"),
			"consoleEncoding": ops.String("控制台编码：auto、utf-8、gbk；空表示不改"),
			"extraJvm":        ops.Array("自定义 JVM 参数，一行一个，传了就整体替换（老版本的 log4shell 缓解参数始终保留）", ops.String("一个参数")),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[settingsArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			xmx, xms, enc := i.MemSnapshot()
			jvm := i.ExtraJVMSnapshot()
			plan := &ops.Plan{Summary: fmt.Sprintf("修改「%s」的运行参数（下次启动生效）", p.Inst),
				Pre: map[string]string{"运行参数": fmt.Sprintf("%d/%d/%s/%q", xmx, xms, enc, jvm)}}
			if p.XmxMB > 0 && p.XmxMB != xmx {
				plan.Changes = append(plan.Changes, ops.Change{What: "最大内存（MB）", Before: strconv.Itoa(xmx), After: strconv.Itoa(p.XmxMB)})
			}
			if p.ConsoleEncoding != "" && p.ConsoleEncoding != enc {
				plan.Changes = append(plan.Changes, ops.Change{What: "控制台编码", Before: enc, After: p.ConsoleEncoding})
			}
			if p.ExtraJVM != nil && !slices.Equal(*p.ExtraJVM, jvm) {
				plan.Changes = append(plan.Changes, ops.Change{What: "JVM 参数", Before: strings.Join(jvm, " "), After: strings.Join(*p.ExtraJVM, " ")})
			}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[settingsArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.UpdateSettings(p.Inst, p.XmxMB, p.XmsMB, p.ConsoleEncoding, p.ExtraJVM); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.props.get", Title: "读取 server.properties", Level: ops.Read,
		Desc:   "server.properties 的全部键值（按文件里的顺序），以及服务器是否在运行（运行中改的要重启才生效）。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			props, err := inst.LoadProps(i.PropsPath())
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			pairs := props.Pairs()
			if pairs == nil {
				pairs = []inst.KV{}
			}
			return map[string]any{"pairs": pairs, "running": i.Status() != "stopped"}, nil
		},
	})

	type propsArgs struct {
		Inst  string    `json:"inst"`
		Pairs []inst.KV `json:"pairs"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.props.set", Title: "修改 server.properties", Level: ops.Write,
		Desc: "改 server.properties 里的若干项（其余保持原样）；服务器运行中改的要重启才生效。",
		Params: withInst(map[string]*ops.Schema{
			"pairs": ops.Array("要改的键值对", ops.Object(map[string]*ops.Schema{
				"key":   ops.String("键名，如 max-players"),
				"value": ops.String("新的值（一律写成字符串）"),
			}, "key", "value")),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[propsArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			props, err := inst.LoadProps(i.PropsPath())
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			plan := &ops.Plan{Summary: fmt.Sprintf("修改「%s」的 server.properties", p.Inst),
				Pre: map[string]string{"server.properties": fileHash(i.PropsPath())}}
			for _, kv := range p.Pairs {
				key, val := cleanProp(kv)
				if key == "" {
					continue
				}
				if old, ok := props.Get(key); !ok || old != val {
					plan.Changes = append(plan.Changes, ops.Change{What: key, Before: old, After: val})
				}
			}
			if i.Status() != "stopped" {
				plan.Warning = "服务器正在运行，改动要重启后才生效"
			}
			return plan, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[propsArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			// 换地图到一半，世界和设置都可能在动。读、改、存的这段时间登记着：换地图也会改这个文件，不能穿插进来
			release, err := c.Mgr.UseWorldInstance(i, "保存设置")
			if err != nil {
				return nil, err
			}
			defer release()
			// 读、改、存和别的保存排队：两个窗口同时改，后存的不会把先存的那几项盖回去
			err = inst.UpdatePropsContext(ctx, i.PropsPath(), func(props *inst.Props) error {
				for _, kv := range p.Pairs {
					if key, val := cleanProp(kv); key != "" {
						props.Set(key, val)
					}
				}
				return nil
			})
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.policies.get", Title: "查看崩溃重启与定时任务", Level: ops.Read,
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			pol := i.PoliciesSnapshot()
			if pol.Schedules == nil {
				pol.Schedules = []inst.Schedule{}
			}
			return pol, nil
		},
	})

	// 两个窗口同时开着「定时任务」时，界面只发改了的：拨开关发 crashRestart，加一条发 addSchedules，删一条发 removeSchedules，
	// 不再整页发回来把另一边刚存的改回去（2026-11-17）。schedules 整张替换是老写法，照旧能用。
	schedItem := func() *ops.Schema {
		return ops.Object(map[string]*ops.Schema{
			"type": ops.String("restart（重启）、command（执行命令）、backup（备份）"),
			"at":   ops.String("每天几点，HH:MM"),
			"args": ops.String("type 为 command 时要执行的命令"),
		})
	}
	type policiesArgs struct {
		Inst string `json:"inst"`
		inst.PolicyPatch
	}
	c.reg.Register(&ops.Op{
		Name: "instance.policies.set", Title: "修改崩溃重启与定时任务", Level: ops.Write,
		Desc: "只改传了的：crashRestart 给了才改；schedules 给了就整张替换定时任务（重启、命令、备份）；" +
			"removeSchedules、addSchedules 按条删、加（删的是类型、时刻、命令都一样的那一条，找不到不算错）。先整张替换，再删，再加。",
		Params: withInst(map[string]*ops.Schema{
			"crashRestart":    ops.Boolean("崩溃后自动重启（10 / 30 / 60 秒退避，连续 3 次放弃）"),
			"schedules":       ops.Array("定时任务（给了就整张替换）", schedItem()),
			"removeSchedules": ops.Array("要删掉的定时任务，每项删一条完全相同的", schedItem()),
			"addSchedules":    ops.Array("要加上的定时任务", schedItem()),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[policiesArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			cur := i.PoliciesSnapshot()
			// 和真正保存用同一个合并：预览里列的「改成」就是确认以后存进去的
			next, err := p.Apply(cur)
			if err != nil {
				return nil, err
			}
			fp, _ := json.Marshal(cur)
			plan := &ops.Plan{Summary: fmt.Sprintf("修改「%s」的崩溃重启与定时任务", p.Inst), Pre: map[string]string{"运维策略": string(fp)}}
			if cur.CrashRestart != next.CrashRestart {
				plan.Changes = append(plan.Changes, ops.Change{What: "崩溃自动重启", Before: onOff(cur.CrashRestart), After: onOff(next.CrashRestart)})
			}
			if before, after := schedText(cur.Schedules), schedText(next.Schedules); before != after {
				plan.Changes = append(plan.Changes, ops.Change{What: "定时任务", Before: before, After: after})
			}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[policiesArgs](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.Mgr.PatchPolicies(p.Inst, p.PolicyPatch); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})
}

// adoptOps 接管已有的服务器：把用户自己以前开的服务器文件夹就地登记成实例（inst/adopt.go）。
// 两个操作都要读主机上任意一个文件夹，和 host.pickdir、java.add 一样只许人在本机操作，AI 和别的设备都不行。
func (c *cat) adoptOps() {
	const localMsg = "接管已有服务器仅支持在本机操作（要读这台电脑上的文件夹）"
	c.reg.Register(&ops.Op{
		Name: "instance.adopt.scan", Title: "识别已有的服务器文件夹", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: localMsg,
		Desc:     "只读地看一遍文件夹，认出服务端的类型、版本、启动文件和原来分的内存，给接管的表单预填。",
		Params:   ops.Object(map[string]*ops.Schema{"dir": ops.String("服务器所在的文件夹")}, "dir"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Dir string `json:"dir"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Mgr.ScanAdopt(p.Dir)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.adopt", Title: "接管已有服务器", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: localMsg,
		Desc: "把一个已有的服务器文件夹就地登记成实例（不搬文件，不改存档和配置，只有给了不一样的 port 时改 server.properties 里的端口；原来的 run.bat 先备份），返回任务编号。" +
			"没有合适的 Java 时在后台下载便携版。",
		Params: ops.Object(map[string]*ops.Schema{
			"dir":          ops.String("服务器所在的文件夹"),
			"name":         createParams["name"],
			"core":         createParams["core"],
			"mc":           ops.String("Minecraft 版本（代理端填它自己的版本）"),
			"build":        ops.String("构建号或加载器版本，可以不填"),
			"launchTarget": ops.String("启动方式：instance.adopt.scan 给出的候选之一"),
			"xmxMb":        createParams["xmxMb"],
			"javaMajor":    ops.Integer("Java 大版本；0 表示按 Minecraft 版本自动选"),
			"javaPath":     ops.String("用 java.list 里的哪一个 java.exe；空串自动选"),
			"eula":         createParams["eula"],
			"port":         ops.Integer("端口；0 或者和文件夹里 server.properties 写的一样就不改，不一样就把 server-port 改成它（代理端不看）").Range(0, 65535),
		}, "dir", "name", "core", "mc", "launchTarget"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[inst.AdoptReq](a)
			if err != nil {
				return nil, err
			}
			if req.JavaPath != "" { // 只认已知的 Java，不能借这里让程序去执行任意一个 exe
				jp, ok := knownJava(req.JavaMajor, req.JavaPath)
				if !ok {
					return nil, ops.Errorf(400, "%s 不是已知的 Java %d（见 java.list）", req.JavaPath, req.JavaMajor)
				}
				req.JavaPath = jp
			}
			id, err := c.Mgr.AdoptAsync(req)
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})
}

// cleanProp 规整一项 server.properties 的修改：键名为空或含 = 和换行的丢弃；
// 值内换行→空格，防拆行注入额外 key=value（如 enable-rcon）。
func cleanProp(kv inst.KV) (key, val string) {
	key = strings.TrimSpace(kv.Key)
	if key == "" || strings.ContainsAny(key, "=\n\r") {
		return "", ""
	}
	return key, strings.ReplaceAll(strings.ReplaceAll(kv.Value, "\r", " "), "\n", " ")
}

func onOff(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

func schedText(ss []inst.Schedule) string {
	if len(ss) == 0 {
		return "无"
	}
	var parts []string
	for _, s := range ss {
		t := s.At + " " + s.Type
		if s.Args != "" {
			t += "：" + s.Args
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, "；")
}
