package inst

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

type CreateReq struct {
	Name        string     `json:"name"`
	Core        mcsrc.Core `json:"core"`
	MC          string     `json:"mc"`
	Build       string     `json:"build"`
	XmxMB       int        `json:"xmxMb"`
	Port        int        `json:"port"`
	EULA        bool       `json:"eula"`
	OnlineMode  bool       `json:"onlineMode"`
	AllowFlight bool       `json:"allowFlight"`
	MOTD        string     `json:"motd"`
	Difficulty  string     `json:"difficulty"` // 创建时写入 server.properties（空=不设，用服务端默认）
	Gamemode    string     `json:"gamemode"`
	// Whitelist 只允许白名单里的玩家进入：新建、导入整合包、用已有地图开服都按它把 white-list 明确写进 server.properties，
	// 不论游戏版本（26.3 起服务端自己的默认值是开）。不传就是关，MineDeck 的默认，表单上写明（可用性走查 R02）
	Whitelist bool `json:"whitelist"`
	// LocalSource 整合包导入的「本地来源目录」：用户手里还留着的老服务器目录（如 D:\old-server）。
	// 待决（无法自动下载）的文件凡在这个目录里有对应的，按原样复制进实例（不校验、要求线上匹配），
	// 用户的魔改模组因此直接保留；目录里没有的留在待决清单交给用户处置。
	// 读的是主机上的任意路径，和 root 一样：仅本机请求有效，远程 / 局域网 / AI 一律忽略。
	LocalSource string `json:"localSource,omitempty"`
	Root        string `json:"root"` // 自定义存放根目录（空=全局默认），仅创建时使用
	// WorldPlan 用已有地图开服：地图导入的计划编号（internal/worldimport）。地图在登记实例之前放好，放不进去整台不建。
	WorldPlan  string `json:"worldPlan,omitempty"`
	WorldAck   bool   `json:"worldAck,omitempty"` // 预览里要确认的项（版本、模组）用户已确认
	WorldOwner string `json:"-"`                  // 发起的人（操作层填），计划只认发起检测的那个人
}

// createSteps 创建流水线的步骤名（整合包导入在其后追加额外步骤）。
var createSteps = []string{"解析版本信息", "准备 Java 运行时", "下载服务端核心", "安装服务端", "写入配置文件", "生成启动脚本"}

// validateCreate 校验创建请求并返回目标目录（供普通创建与整合包导入共用）。
func (m *Manager) validateCreate(req *CreateReq) (string, error) {
	if err := checkInstanceReq(req, true); err != nil {
		return "", err
	}
	return m.reserveInstanceDir(req)
}

// checkInstanceReq 查名字、要不要已经知道核心、EULA。
// needCore 为 false 是现成服务端压缩包：核心要等解压之后，用接管已有服务器那套识别才知道。
func checkInstanceReq(req *CreateReq, needCore bool) error {
	req.Name = strings.TrimSpace(req.Name)
	if err := validateName(req.Name); err != nil {
		return err
	}
	if needCore {
		if !mcsrc.ValidCore(req.Core) {
			return fmt.Errorf("未知核心类型: %s", req.Core)
		}
		if req.MC == "" {
			return fmt.Errorf("请选择版本")
		}
	}
	// 代理端不运行 Minecraft 本体，无需 Mojang EULA。核心还没认出来时按普通服务器算，仍然要同意。
	if !req.EULA && mcsrc.KindOf(req.Core) != mcsrc.KindProxy {
		return fmt.Errorf("需要同意 Minecraft EULA 才能开服")
	}
	return nil
}

// reserveInstanceDir 分配实例目录并占上「创建中」。调用方的 goroutine 结束时要 releaseCreating。
func (m *Manager) reserveInstanceDir(req *CreateReq) (string, error) {
	m.mu.Lock()
	_, exists := m.insts[req.Name]
	m.mu.Unlock()
	if exists {
		return "", fmt.Errorf("已有同名的服务器「%s」", req.Name)
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 25565
	}
	if req.XmxMB < 512 {
		req.XmxMB = 2048
	}
	// 目标根目录：请求指定 > 全局默认
	root := strings.TrimSpace(req.Root)
	if root == "" {
		root = app.ServersRoot()
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("存放文件夹不可用: %w", err)
	}
	// 目录名用 slug（与中文显示名解耦）；同名自动追加序号
	slug := slugify(req.Name)
	dir := filepath.Join(root, slug)
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		if n > 999 {
			return "", fmt.Errorf("无法为新服务器分配文件夹（同名文件夹过多）")
		}
		dir = filepath.Join(root, fmt.Sprintf("%s-%d", slug, n))
	}
	// Mohist/Banner 的 javaagent 自举对非 ASCII 路径不兼容——校验最终路径而非实例名，
	// 因此中文实例名在纯英文路径下也可正常使用。
	if (req.Core == mcsrc.CoreMohist || req.Core == mcsrc.CoreBanner) && !isASCII(dir) {
		return "", fmt.Errorf("Mohist/Banner 与非英文路径不兼容，请把存放文件夹设为纯英文路径（当前将创建于：%s）", dir)
	}
	// 所有校验通过后，原子性登记「创建中」占位。实例要到流水线末尾（Finish）才写入 m.insts，
	// 期间同名请求（一键部署双击 / 重复 POST）此前都能各自通过校验并建目录，完成时相互覆盖 map
	// 造成孤儿实例。此处在返回目录前占位，重复请求会被拒；调用方 goroutine 负责最终 releaseCreating。
	m.mu.Lock()
	_, taken := m.insts[req.Name]
	inflight := m.creating[req.Name]
	dirInflight := m.creating[dirKey(dir)]
	if !taken && !inflight && !dirInflight {
		m.creating[req.Name] = true
		m.creating[dirKey(dir)] = true // 目录也占上：建到一半的目录不能同时被「接管已有服务器」拿走（见 adopt.go）
	}
	m.mu.Unlock()
	if taken {
		return "", fmt.Errorf("已有同名的服务器「%s」", req.Name)
	}
	if inflight {
		return "", fmt.Errorf("「%s」正在创建中，请稍候", req.Name)
	}
	if dirInflight {
		return "", fmt.Errorf("另一个服务器正在创建同名文件夹，请稍后再试")
	}
	return dir, nil
}

// releaseCreating 释放「创建中」占位（实例名，或 dirKey 给出的目录键）；由创建 / 导入 / 接管的 goroutine 在任务结束时（无论成败）调用。
func (m *Manager) releaseCreating(name string) {
	m.mu.Lock()
	delete(m.creating, name)
	m.mu.Unlock()
}

// slugify 把实例显示名转为 ASCII 安全的目录名；全非 ASCII 时回退带时间戳。
func slugify(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-_")
	if s == "" {
		s = "server-" + time.Now().Format("20060102-150405")
	}
	return s
}

// CreateAsync 校验请求并启动后台创建任务，返回任务 ID。
func (m *Manager) CreateAsync(req CreateReq) (string, error) {
	dir, err := m.validateCreate(&req)
	if err != nil {
		return "", err
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("创建服务器 %s（%s %s）", req.Name, req.Core, req.MC), createSteps)
	t.EnableCancel()
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		if req.WorldPlan != "" {
			defer m.releaseWorldPlan(req.WorldOwner, req.WorldPlan)
		}
		if err := m.runCreate(ctx, t, req, dir); err != nil {
			failCreate(t, err, m.discardCreating(req.Name, dir))
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

func (m *Manager) runCreate(ctx context.Context, t *tasks.Task, req CreateReq, dir string) error {
	return m.runCreateWith(ctx, t, req, dir, nil)
}

// runCreateWith 在 beforePublish 完成前不写 instance.json、不把实例开放给其他请求。
func (m *Manager) runCreateWith(ctx context.Context, t *tasks.Task, req CreateReq, dir string, beforePublish func(*Instance) error) error {
	// 90 分钟的时限只管建服务器本体这几步。发布边界（completeCreate）用调用方给的 ctx：
	// 它前面要跑完 beforePublish（整合包的文件下载、覆盖），那一段一直没有总时限，
	// 不能在几百个文件都下完以后被这里的时限判成超时、整台清掉
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, 90*time.Minute)
	defer cancel()

	// ---- 步骤 0：解析版本信息 ----
	t.StartStep(0)
	kind := mcsrc.KindOf(req.Core)
	var meta *mcsrc.VersionMeta
	var javaMajor int
	if kind == mcsrc.KindProxy {
		javaMajor = mcsrc.FixedJavaOf(req.Core)
		t.Logf("代理端 %s 使用 Java %d", req.Core, javaMajor)
	} else {
		var err error
		meta, err = mcsrc.GetVersionMeta(ctx, req.MC)
		if err != nil {
			return err
		}
		javaMajor = meta.JavaMajor
		t.Logf("MC %s 需要 Java %d（来自官方元数据）", req.MC, javaMajor)
	}

	// ---- 步骤 1：准备 Java（本机已装 > 便携 > 下载） ----
	t.StartStep(1)
	javaPath, err := java.Resolve(ctx, javaMajor, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", javaMajor)))
	if err != nil {
		return err
	}
	t.Logf("Java 就绪: %s", javaPath)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 登记成功之前一直留着。进程中途没了，下次可以认出来删；已经有 instance.json 的不靠它清理。
	if err := WriteCreatingMarker(dir, req.Name); err != nil {
		return err
	}

	// ---- 步骤 2：下载核心 ----
	t.StartStep(2)
	inst := &Instance{
		Settings: Settings{
			Name: req.Name, Core: req.Core, MC: req.MC, Build: req.Build,
			JavaMajor: javaMajor, JavaPath: javaPath,
			XmxMB: req.XmxMB, XmsMB: min(1024, req.XmxMB),
			CreatedAt: time.Now(),
		},
		Dir: dir, Console: newConsole(),
	}
	needInstaller := false
	switch req.Core {
	case mcsrc.CoreVanilla:
		if err := fetchToInstance(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+req.MC, dir, "server.jar"); err != nil {
			return err
		}
		inst.LaunchTarget = "jar:server.jar"

	case mcsrc.CoreFabric:
		installerV, err := mcsrc.FabricInstallerVersion(ctx)
		if err != nil {
			return err
		}
		loader := req.Build
		if loader == "" || loader == "latest" {
			loaders, err := mcsrc.FabricLoaders(ctx, req.MC)
			if err != nil {
				return err
			}
			loader = loaders[0].ID
			for _, l := range loaders {
				if l.Recommended {
					loader = l.ID
					break
				}
			}
			inst.Build = loader
		}
		art := mcsrc.FabricServerArtifact(req.MC, loader, installerV)
		if err := fetchToInstance(ctx, t, art, "fabric", dir, art.FileName); err != nil {
			return err
		}
		// 预下载原版 server.jar 并指向之，避免 Fabric 首启时从境外源慢速下载
		t.Logf("预下载原版服务端供 Fabric 使用")
		if err := fetchToInstance(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+req.MC, dir, "server.jar"); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "fabric-server-launcher.properties"),
			[]byte("serverJar=server.jar\n"), 0o644); err != nil {
			return err
		}
		inst.LaunchTarget = "jar:" + art.FileName

	case mcsrc.CoreForge:
		art := mcsrc.ForgeInstallerArtifact(ctx, req.MC, req.Build)
		if err := fetchToInstance(ctx, t, art, "forge", dir, art.FileName); err != nil {
			return err
		}
		// 传统 Forge（<=1.16.5）安装器会直连 Mojang 下载原版服务端，
		// 预放置到其检查路径可让安装器校验哈希后跳过下载。
		if newer117, err := mcsrc.NewerOrEqual(ctx, req.MC, "1.17"); err == nil && !newer117 {
			t.Logf("预放置原版服务端（加速安装器）")
			if err := fetchToInstance(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+req.MC,
				dir, "minecraft_server."+req.MC+".jar"); err != nil {
				return err
			}
		}
		needInstaller = true

	case mcsrc.CoreNeoForge:
		_, paths, err := mcsrc.NeoBuilds(ctx, req.MC)
		if err != nil {
			return err
		}
		art := mcsrc.NeoInstallerArtifact(ctx, req.Build, paths[req.Build])
		if err := fetchToInstance(ctx, t, art, "neoforge", dir, art.FileName); err != nil {
			return err
		}
		// 预放置原版服务端到安装器的校验路径，跳过其直连 Mojang 的大文件下载
		t.Logf("预放置原版服务端（加速安装器）")
		preDest := filepath.Join("libraries", "net", "minecraft", "server", req.MC, "server-"+req.MC+".jar")
		if err := fetchToInstance(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+req.MC, dir, preDest); err != nil {
			return err
		}
		needInstaller = true

	default:
		// 通用 jar 型核心（Paper/Purpur/Leaves/Folia/Mohist/Banner/Velocity/Waterfall）
		art, err := mcsrc.GenericArtifact(ctx, req.Core, req.MC, req.Build)
		if err != nil {
			return err
		}
		if err := fetchToInstance(ctx, t, art, string(req.Core), dir, "server.jar"); err != nil {
			return err
		}
		inst.LaunchTarget = "jar:server.jar"
	}

	// ---- 步骤 3：运行安装器（仅 Forge/NeoForge） ----
	t.StartStep(3)
	if needInstaller {
		installerName := ""
		if req.Core == mcsrc.CoreForge {
			installerName = fmt.Sprintf("forge-%s-%s-installer.jar", req.MC, req.Build)
		} else {
			installerName = fmt.Sprintf("neoforge-%s-installer.jar", req.Build)
		}
		t.Logf("运行官方安装器（将从网络下载依赖库，耗时视网络情况数分钟）...")
		if err := runInstaller(ctx, t, javaPath, dir, installerName); err != nil {
			return err
		}
		target, err := locateLaunchTarget(dir, req.Core, ctx, req.MC)
		if err != nil {
			return err
		}
		inst.LaunchTarget = target
		t.Logf("启动目标: %s", target)
		os.Remove(filepath.Join(dir, installerName))
	} else {
		t.Logf("该核心无需安装步骤，跳过")
	}

	// ---- 步骤 4：写入配置 ----
	t.StartStep(4)
	if kind == mcsrc.KindProxy {
		t.Logf("代理端无需 EULA 与 server.properties，首次启动后在服务器文件夹里编辑它自带的配置文件")
	} else {
		if err := os.WriteFile(filepath.Join(dir, "eula.txt"),
			[]byte("# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
			return err
		}
		props, err := LoadProps(filepath.Join(dir, "server.properties"))
		if err != nil {
			return err
		}
		props.Set("server-port", fmt.Sprintf("%d", req.Port))
		props.Set("online-mode", strconv.FormatBool(req.OnlineMode))
		props.Set("allow-flight", strconv.FormatBool(req.AllowFlight))
		// 离线模式下 1.19+ 显式关闭聊天签名校验，规避代理/插件环境「无法验证安全档案」踢人
		if !req.OnlineMode {
			if ge, err := mcsrc.NewerOrEqual(ctx, req.MC, "1.19"); err == nil && ge {
				props.Set("enforce-secure-profile", "false")
			}
		}
		motd := req.MOTD
		if motd == "" {
			motd = "MineDeck 服务器"
		}
		props.Set("motd", motd)
		if req.Difficulty != "" {
			props.Set("difficulty", req.Difficulty)
		}
		if req.Gamemode != "" {
			props.Set("gamemode", req.Gamemode)
		}
		if err := props.Save(filepath.Join(dir, "server.properties")); err != nil {
			return err
		}
		// log4shell 防护（老版本）
		if err := applyLog4jMitigation(ctx, t, inst); err != nil {
			return err
		}
	}

	// ---- 放地图（用已有地图开服）：在登记实例之前，失败就整台不建 ----
	if req.WorldPlan != "" {
		if m.worldPlacer == nil {
			return fmt.Errorf("地图导入功能没有准备好")
		}
		if err := m.worldPlacer(ctx, t, req.WorldOwner, req.WorldPlan, dir); err != nil {
			return fmt.Errorf("没能把地图放进新服务器：%w", err)
		}
	}

	// ---- 步骤 5：完成初始化后才发布实例 ----
	t.StartStep(5)
	return m.completeCreate(parent, t, inst, withCreateWhitelist(t, req, beforePublish))
}

// withCreateWhitelist 发布之前最后一步：把白名单开关写进 server.properties。放在 beforePublish（整合包的下载和覆盖目录）之后：
// 覆盖目录可以带着整合包自己的 server.properties 整个盖上来，白名单以表单上的开关为准。代理端没有这个文件，不写。
func withCreateWhitelist(t *tasks.Task, req CreateReq, beforePublish func(*Instance) error) func(*Instance) error {
	if mcsrc.KindOf(req.Core) == mcsrc.KindProxy {
		return beforePublish
	}
	return func(i *Instance) error {
		if beforePublish != nil {
			if err := beforePublish(i); err != nil {
				return err
			}
		}
		return writeCreateWhitelist(t, i.Dir, req.Whitelist)
	}
}

// writeCreateWhitelist 把白名单开关明确写进 server.properties，不论游戏版本。26.3 起服务端自己生成配置时默认开白名单，
// 名单又是空的：按默认值开出来的服务器，朋友连进来会被挡住，界面上却一切正常（可用性走查 U02）。
// MineDeck 的默认是关，新建表单上写明「关着时任何知道地址的人都能进」。
// 按服务端的读法写（ForceProp）：同一项写了几行时服务端认最后一行，整合包自带的文件里可能就有。
func writeCreateWhitelist(t *tasks.Task, dir string, on bool) error {
	path := filepath.Join(dir, "server.properties")
	want := strconv.FormatBool(on)
	before, had := javaPropsOf(path)["white-list"]
	if err := ForceProp(path, "white-list", want); err != nil {
		return fmt.Errorf("没能把白名单开关写进 server.properties：%w", err)
	}
	if had && !strings.EqualFold(strings.TrimSpace(before), want) {
		t.Logf("整合包自带的 server.properties 里白名单是「%s」，按导入页上的开关改成了「%s」", onOff(before, false), onOff(want, false))
	} else if on {
		t.Logf("白名单：开（只有名单里的玩家能进，开服后到「玩家」里加名字）")
	} else {
		t.Logf("白名单：关（任何知道地址的人都能进）")
	}
	return nil
}

// completeCreate 是新建与整合包导入共用的发布边界；失败前始终没有可操作的实例。
func (m *Manager) completeCreate(ctx context.Context, t *tasks.Task, i *Instance, beforePublish func(*Instance) error) error {
	if beforePublish != nil {
		if err := beforePublish(i); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.BeginCommit() {
		return context.Canceled
	}
	// 覆盖目录可能使用文件别名：最终控制文件必须来自这份初始化好的内存设置。
	if err := writeRunBat(i); err != nil {
		return err
	}
	if err := m.saveInstance(i); err != nil {
		return err
	}
	// 服务器已经登记。记号删不掉也不失败：下次看到 instance.json 会只去掉记号，不会删服务器。
	if err := ClearCreatingMarker(i.Dir); err != nil {
		t.Logf("服务器已创建。创建记号没能去掉（%v），不影响使用", err)
	}
	app.RememberRoot(filepath.Dir(i.Dir))
	m.mu.Lock()
	m.insts[i.Name] = i
	m.mu.Unlock()
	t.Logf("服务器创建完成 ✔")
	return nil
}

// cancelledOr 任务是用户点了取消才停下的：报「已取消」，不把流水线里冒出来的那句 context canceled 当成失败原因给人看。
func cancelledOr(t *tasks.Task, err error) error {
	if t.Cancelled() {
		return fmt.Errorf("已取消，没有创建服务器")
	}
	return err
}

// failCreate 新建、导入整合包、导入服务端这几种后台任务出错收尾（半成品已经按 cleanupErr 的结果清过）。
// 用户点了取消、半成品也清干净了：任务按「已取消」结束（tasks.EndCancelled），界面显示中性的「已取消」，不是失败；
// 取消以后没清干净的仍是失败，原因里带上清理失败的那句——文件夹里留了东西，要人去看（2026-11-14 复审发现 9）。
func failCreate(t *tasks.Task, err, cleanupErr error) {
	err = cancelledOr(t, err)
	if cleanupErr != nil {
		t.Fail(fmt.Errorf("%w；清理半成品失败：%v", err, cleanupErr))
		return
	}
	if t.Cancelled() {
		t.EndCancelled(err)
		return
	}
	t.Fail(err)
}

// discardCreating 只清理由当前创建任务独占、尚未发布的目录。
func (m *Manager) discardCreating(name, dir string) error {
	m.mu.Lock()
	owned := m.creating[name] && m.creating[dirKey(dir)] && m.insts[name] == nil
	m.mu.Unlock()
	if !owned {
		return fmt.Errorf("服务器目录不再由这次创建任务独占，已保留文件")
	}
	// 创建占位由调用方在清理以后释放；不拿着管理器全局锁等待磁盘删除。
	abs, err := filepath.Abs(dir)
	if err != nil || abs == filepath.Dir(abs) || strings.EqualFold(abs, filepath.Clean(app.Base)) {
		return fmt.Errorf("创建目录异常，已保留文件")
	}
	if st, err := os.Lstat(abs); err == nil && st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("创建目录已变成链接，已保留文件")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(abs)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// fetchToInstance 下载工件到共享缓存，再复制进实例目录。
func fetchToInstance(ctx context.Context, t *tasks.Task, art mcsrc.Artifact, cacheKey, dir, destName string) error {
	cachePath := filepath.Join(app.CacheDir, filepath.FromSlash(cacheKey), art.FileName)
	if err := dl.Fetch(ctx, dl.Request{
		URLs: art.URLs, Dest: cachePath,
		SHA1: art.SHA1, SHA256: art.SHA256, MD5: art.MD5, MinSize: art.MinSize,
		OnRetry: func(host string, err error) {
			t.Logf("从 %s 下载 %s 没成功（%v），换下一个下载源或稍后重试", host, art.FileName, err)
			t.NetRetry(host, "下载失败")
		},
	}, t.ProgressFn("下载 "+art.FileName)); err != nil {
		return err
	}
	return copyFileContext(ctx, cachePath, filepath.Join(dir, destName))
}

func copyFile(src, dst string) error {
	return copyFileContext(context.Background(), src, dst)
}

func copyFileContext(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = in.Close() })
	defer stopClose()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	st, err := in.Stat()
	if err != nil {
		return err
	}
	lease, err := diskguard.Acquire(ctx, dst, diskguard.Options{Need: st.Size(), AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(lease.Writer(out), in)
	if werr := out.Close(); cerr == nil {
		cerr = werr
	}
	return cerr
}

// runInstaller 以匹配的 Java 运行 Forge/NeoForge 官方安装器（--installServer）。
// 加固：镜像参数（--mirror 指向 BMCLAPI）+ 网络抖动自动重试。
func runInstaller(ctx context.Context, t *tasks.Task, javaExe, dir, installerJar string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			t.Logf("安装器第 %d 次重试（多为瞬时网络问题，已下载的库会被复用）...", attempt)
			t.NetRetry("", fmt.Sprintf("安装器出错退出，第 %d 次重试", attempt))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
		lastErr = runInstallerOnce(ctx, t, javaExe, dir, installerJar)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("安装器多次运行失败：%w。请查看服务器文件夹里的 installer.jar.log，或在「设置 → 下载与网络」中切换下载源后重试。", lastErr)
}

func runInstallerOnce(ctx context.Context, t *tasks.Task, javaExe, dir, installerJar string) error {
	cctx, cancel := context.WithTimeout(ctx, 40*time.Minute)
	defer cancel()
	args := []string{"-jar", installerJar, "--installServer", "."}
	if !app.OfficialOnly() {
		args = append(args, "--mirror", "https://bmclapi2.bangbang93.com/maven/")
	}
	cmd := exec.CommandContext(cctx, javaExe, args...)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动安装器: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var nw installerNet
	var said installerSaid
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			t.Logf("[安装器] %s", line)
			said.line(line)
			if what := nw.line(line); what != "" {
				t.NetRetry(nw.host, what)
			}
		}
	}
	// 输出没读完（某一行长得离谱、管道出错）：后面的话没看到，不能拿「没说装成功」下结论。
	// 剩下的输出读掉扔了，免得安装器卡在写不进去的管道上。
	scanErr := sc.Err()
	if scanErr != nil {
		t.Logf("⚠ 安装器的输出没有读完（%v），装没装成只能看它的退出码", scanErr)
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("安装器退出异常: %w", err)
	}
	if scanErr != nil {
		return nil
	}
	if err := said.err(); err != nil {
		return err
	}
	if !said.success {
		t.Logf("⚠ 安装器正常退出了，但没有看到它说安装成功。服务器开不起来的话，删掉这台重新建一次")
	}
	return nil
}

// installerSaid 安装器的输出里和「到底装成没有」有关的两句话。
// 进程以 0 退出不等于装好了：老版本 Forge 的安装器开头会先连一下 files.minecraftforge.net 验证书，
// 这一步失败（网络一抖就会）它打两行提示就以 0 退出，什么都没装。第一次安装时这会在后面找启动文件的
// 地方报出来；可要是发生在重试的那一次，上一次已经把启动文件解出来了，后几步要生成的库却没有，
// 只看退出码就会把这半截安装当成装好的，服务器一开就崩（实测遇到过：Forge 1.16.5 的交接包导入）。
type installerSaid struct {
	success   bool // 说了「The server installed successfully」
	certAbort bool // 说了证书校验没过
}

func (o *installerSaid) line(s string) {
	if strings.Contains(s, "installed successfully") {
		o.success = true
	}
	if strings.Contains(s, "Failed to validate certificates") {
		o.certAbort = true
	}
}

// err 以 0 退出以后还要不要算失败。只认已经见过的那一种：证书校验没过、又没说装成功。
// 别的情况不拦（没法保证每个版本的安装器成功时都说同一句话），调用方只记一行提醒。
func (o installerSaid) err() error {
	if o.certAbort && !o.success {
		return fmt.Errorf("安装器连不上 Forge 的服务器（证书校验没过），什么都没装就退出了")
	}
	return nil
}

// installerNet 从安装器的输出里认出网络问题（测试报告 B-01：NeoForge 安装器直连 maven.neoforged.net，
// 每次连接超时等 30 秒，界面上只看到进度不动）：记下最近一行提到的下载地址；一段异常栈只算一次。
type installerNet struct {
	host    string // 最近提到的下载地址（主机名）
	inTrace bool   // 正在一段已经算过的异常栈里
}

var (
	installerURL    = regexp.MustCompile(`https?://([^/\s"'<>]+)`)
	installerNetErr = regexp.MustCompile(`(?i)SocketTimeoutException|timed out|ConnectException|UnknownHostException|NoRouteToHostException`)
	installerNoHost = regexp.MustCompile(`UnknownHostException:\s*([A-Za-z0-9.-]+)`)
)

// line 看一行输出（已去掉首尾空白）：是一次新的网络问题时返回是什么问题，否则返回空串。
func (w *installerNet) line(s string) string {
	if m := installerURL.FindStringSubmatch(s); m != nil {
		w.host = m[1]
	} else if m := installerNoHost.FindStringSubmatch(s); m != nil {
		w.host = m[1]
	}
	// 异常栈的后续行（at …、Caused by …、... 12 more）还算同一段；别的行另起一段
	cont := strings.HasPrefix(s, "at ") || strings.HasPrefix(s, "Caused by") || strings.HasPrefix(s, "...") || strings.HasPrefix(s, "Suppressed")
	if !cont {
		w.inTrace = false
	}
	if w.inTrace || !installerNetErr.MatchString(s) {
		return ""
	}
	w.inTrace = true
	if strings.Contains(strings.ToLower(s), "timed out") || strings.Contains(s, "SocketTimeoutException") {
		return "连接超时"
	}
	return "连不上"
}

// locateLaunchTarget 在安装完成的目录中定位启动方式。
func locateLaunchTarget(dir string, core mcsrc.Core, ctx context.Context, mc string) (string, error) {
	// 现代 Forge / NeoForge：@参数文件
	for _, pattern := range []string{
		"libraries/net/minecraftforge/forge/*/win_args.txt",
		"libraries/net/neoforged/neoforge/*/win_args.txt",
	} {
		if ms, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern))); len(ms) > 0 {
			rel, err := filepath.Rel(dir, ms[0])
			if err != nil {
				return "", err
			}
			return "args:" + filepath.ToSlash(rel), nil
		}
	}
	// 传统 Forge（<=1.16.5）：直接生成的 forge-*.jar
	if ms, _ := filepath.Glob(filepath.Join(dir, "forge-*.jar")); len(ms) > 0 {
		for _, m := range ms {
			base := filepath.Base(m)
			if !strings.Contains(base, "installer") {
				return "jar:" + base, nil
			}
		}
	}
	return "", fmt.Errorf("安装器未生成可识别的服务端启动文件，请查看服务器文件夹中的 installer.log")
}

// log4shell 缓解参数（Mojang 官方方案，见 minecraft.net 2021-12-10 的公告）：
//   - 1.18.1 起不用加；1.17 ~ 1.18 加 noLookups；
//   - 1.12 ~ 1.16.5 自带的 log4j 是 2.8.1，认不得 noLookups，靠 log4j2_112-116.xml（%msg{nolookups}）；
//   - 1.7 ~ 1.11.2 自带的是 2.0-beta9，%msg{nolookups} 也认不得，要另一份 log4j2_17-111.xml（按正则丢掉带 ${ 的消息）。
//     v3.2.0 以前这段也套 112-116 那份，等于没防（2026-11-17 用 1.8.9 原版服务端实测：照样连到 jndi 地址），换法见 log4j_migrate.go；
//   - 1.7 以前还没用 log4j，什么都不加。
//
// noLookups 对 1.7 ~ 1.16.5 不起作用，没有坏处，和以前一样带着：已经建好的服务器里都有它。
//
// 上面是按 Minecraft 版本分的。要套官方配置的那两段，再看一眼服务端 jar 自带的 log4j：看得出是修过的版本就不套
// （2021 年 12 月以后的 Paper 构建；CraftBukkit 系套上 Mojang 的配置控制台没有输出），判断不了的照旧套，见 log4j_bundled.go。
// 新建、接管、导入、交接包（addLog4jFlags）、保存参数（log4jFlags）、程序启动后和启动服务器时的整理（log4j_migrate.go）都按这一条。
const (
	log4jNoLookups = "-Dlog4j2.formatMsgNoLookups=true"
	log4jXML       = "log4j2_112-116.xml" // 1.12 ~ 1.16.5 的官方配置，下载到实例目录
	log4jConfig    = "-Dlog4j.configurationFile=" + log4jXML
	log4jXML17     = "log4j2_17-111.xml" // 1.7 ~ 1.11.2 的官方配置
	log4jConfig17  = "-Dlog4j.configurationFile=" + log4jXML17
	log4jConfigKey = "-Dlog4j.configurationFile="
)

// log4jXMLSHA1 两份官方配置的 SHA-1，就是 Mojang 下载地址里的那一串。2026-11-17 照 minecraft.net 公告里的链接下载核对过。
var log4jXMLSHA1 = map[string]string{
	log4jXML:   "02937d122c86ce73319ef9975b58896fc1b491d1",
	log4jXML17: "4bb89a97a66f350bc9f73b3ca8509632682aea2e",
}

// log4jEras 各段版本要的防护，新的在前：不老于 from 的落在这一段。from 都是官方清单里有的版本号（清单里有「1.7」这一条）。
var log4jEras = []struct {
	from      string
	noLookups bool
	xml       string // 要哪份官方配置，"" 是不要
}{
	{"1.18.1", false, ""},
	{"1.17", true, ""},
	{"1.12", true, log4jXML},
	{"1.7", true, log4jXML17},
}

// log4jPlan 按 Mojang 的公告给出一个版本要的防护：加不加 noLookups、要哪份官方配置（"" 是不要）。
// newer(b) 回答这个版本是否不老于 b，它出错（判断不了）时原样返回。比 1.7 还老的什么都不要。
func log4jPlan(newer func(b string) (bool, error)) (noLookups bool, xml string, err error) {
	for _, e := range log4jEras {
		ge, err := newer(e.from)
		if err != nil {
			return false, "", err
		}
		if ge {
			return e.noLookups, e.xml, nil
		}
	}
	return false, "", nil
}

// log4jPlanCached 同 log4jPlan，只看本地清单、不联网（mcsrc.NewerOrEqualCached）。本地没有清单、或者清单里没有这个版本时 ok=false。
func log4jPlanCached(mc string) (noLookups bool, xml string, ok bool) {
	noLookups, xml, err := log4jPlan(func(b string) (bool, error) {
		if ge, ok := mcsrc.NewerOrEqualCached(mc, b); ok {
			return ge, nil
		}
		return false, errors.New("本地版本清单判断不了")
	})
	return noLookups, xml, err == nil
}

// applyLog4jMitigation 依据 Mojang 官方公告为老版本添加 log4shell 缓解。
// 官方清单里没有这个版本时和以前一样，按 1.12 ~ 1.16.5 加（接管、导入、交接包都先确认清单里有才调它）。
func applyLog4jMitigation(ctx context.Context, t *tasks.Task, i *Instance) error {
	known, err := mcsrc.KnownVersion(ctx, i.MC)
	if err != nil {
		return err
	}
	noLookups, xml := true, log4jXML
	if known {
		if noLookups, xml, err = log4jPlan(func(b string) (bool, error) { return mcsrc.NewerOrEqual(ctx, i.MC, b) }); err != nil {
			return err
		}
	}
	addLog4jFlags(ctx, t, i, noLookups, xml)
	return nil
}

// addLog4jFlags 往 i.ExtraJVM 里加上该有的参数。要官方配置的先下载，下载不成不加引用：引用一个不存在的配置文件，
// log4j 会退回只输出 ERROR 的默认配置，控制台和「启动完成」的判断都会失效。
// 服务端自带的 log4j 看得出是修过的（见 log4j_bundled.go），不套官方配置：用不着，而且 CraftBukkit 系套上以后控制台没有输出。
func addLog4jFlags(ctx context.Context, t *tasks.Task, i *Instance, noLookups bool, xml string) {
	if noLookups {
		i.ExtraJVM = append(i.ExtraJVM, log4jNoLookups)
	}
	if xml == "" {
		return
	}
	jar := viewLog4jJar(i.Dir, i.LaunchTarget)
	if jar.fixed {
		t.Logf("这个服务端自带的 log4j 是 %s，已经修过 log4shell，不用再套 Mojang 的日志配置（%s）", jar.ver, xml)
		return
	}
	if err := fetchLog4jXML(ctx, i.Dir, xml); err != nil {
		t.Logf("log4j 防护配置没下载成（%v）。服务器照样能开，但 %s 只靠这份配置防 log4shell，现在等于没防，先别让不认识的人进服", err, i.MC)
		return
	}
	i.ExtraJVM = append(i.ExtraJVM, log4jConfigKey+xml)
	t.Logf("已启用 log4shell 漏洞缓解（Mojang 官方方案，%s）", xml)
	if jar.quiet {
		t.Logf("⚠ %s", log4jQuietNote)
	}
}

// log4jObjects 两份官方配置的下载地址前缀，后面接 SHA-1 和文件名（是变量：测试里换成本机的假服务器）。
var log4jObjects = "https://launcher.mojang.com/v1/objects/"

// fetchLog4jXML 把 Mojang 的一份日志配置（log4jXML 或 log4jXML17）放进实例目录。已经在、内容也对得上的不重新下载；
// 内容对不上的（被别的文件顶掉了）会重新下载。这份文件可能经镜像下载，按官方的 SHA-1 校验。
var fetchLog4jXML = func(ctx context.Context, dir, name string) error { // 是变量：测试里换成不联网的
	sum, ok := log4jXMLSHA1[name]
	if !ok {
		return fmt.Errorf("没有 %s 这份官方日志配置", name)
	}
	xmlURL := log4jObjects + sum + "/" + name
	return dl.Fetch(ctx, dl.Request{URLs: mcsrc.URLPair(xmlURL), Dest: filepath.Join(dir, name), MinSize: 100, SHA1: sum}, nil)
}

// restoreLog4jXML 启动参数里引用着官方日志配置的实例，确认那份文件还是官方的（不是就重新下载）。
// 整合包导入收尾时调：覆盖目录和清单里的文件是在它下载好以后才写进来的。
func restoreLog4jXML(ctx context.Context, t *tasks.Task, i *Instance) {
	args := i.ExtraJVMSnapshot()
	for _, name := range []string{log4jXML, log4jXML17} {
		if !slices.Contains(args, log4jConfigKey+name) {
			continue
		}
		if err := fetchLog4jXML(ctx, i.Dir, name); err != nil {
			t.Logf("⚠ 没能确认 log4j 防护配置是官方的那一份（%v）。联网以后可以删掉服务器文件夹里的 %s 再重新导入", err, name)
		}
	}
}

// log4jFlags 给出保存 JVM 参数时须强制保留的 log4shell 缓解参数，判定与创建时的 applyLog4jMitigation 一致；
// 只读本地清单不联网（mcsrc.NewerOrEqualCached），判定不了版本时保守沿用 i.ExtraJVM 中已有的缓解参数。
// 服务端自带的 log4j 看得出是修过的，不强加官方配置的引用（和 addLog4jFlags、log4j_migrate.go 一致，不然这边不加、那边又补回去）。
// 调用方须持 procMu。
func log4jFlags(i *Instance) []string {
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return nil // 代理端不运行 Minecraft 本体，创建时也不加
	}
	var flags []string
	if noLookups, xml, ok := log4jPlanCached(i.MC); !ok {
		for _, a := range i.ExtraJVM {
			if IsLog4jFlag(a) {
				flags = append(flags, a)
			}
		}
	} else {
		if noLookups {
			flags = append(flags, log4jNoLookups)
		}
		if xml != "" && !log4jSelfFixed(i.Dir, i.LaunchTarget) {
			flags = append(flags, log4jConfigKey+xml)
		}
	}
	// 官方 xml 只在创建、接管、导入和换配置（log4j_migrate.go）时下载：文件不在（当初下载失败 / 被删）就不强加引用——
	// 引用不存在的配置文件会令 log4j 退回只输出 ERROR 的默认配置，控制台日志与「启动完成」检测随之失效
	return slices.DeleteFunc(flags, func(a string) bool {
		name, ok := strings.CutPrefix(a, log4jConfigKey)
		if !ok {
			return false
		}
		_, err := os.Stat(filepath.Join(i.Dir, name))
		return err != nil
	})
}

// withJVMFlags 返回 args 的副本，并令 flags 中每个参数恰好出现一次且即为生效值：同名系统属性的其他取值与重复项
// 一律剔除（JVM 对重复的 -D 取最后一个，残留的其他取值可能覆盖缓解），缺失的追加到末尾；其余参数原样保留。
func withJVMFlags(args, flags []string) []string {
	want := make(map[string]string, len(flags)) // 属性名（"=" 之前）→ 须保留的完整参数
	for _, f := range flags {
		k, _, _ := strings.Cut(f, "=")
		want[k] = f
	}
	out := make([]string, 0, len(args)+len(flags))
	kept := make(map[string]bool, len(flags))
	for _, a := range args {
		k, _, _ := strings.Cut(a, "=")
		if f, ok := want[k]; ok {
			if a != f || kept[f] {
				continue
			}
			kept[f] = true
		}
		out = append(out, a)
	}
	for _, f := range flags {
		if !kept[f] {
			out = append(out, f)
			kept[f] = true
		}
	}
	return out
}

// consoleUTF8 让服务器的控制台输出（MineDeck 从管道里读）一律是 UTF-8。Java 17 及以前 System.out 跟着 file.encoding 走；
// Java 18 起 file.encoding 本来就是 UTF-8，System.out 却按 stdout.encoding，没设时用系统编码（中文 Windows 上是 GBK，
// 英文 Windows 上是 cp1252：中文全变成「?」）。GBK 的输出有时恰好也是合法的 UTF-8（比如「@小木 停」），MineDeck 按 UTF-8
// 解出来就是乱码，游戏内 AI 认不出聊天里的伙伴名字。旧版 Java 不认 stdout.encoding，原样忽略。
// 实例的控制台编码手动选了 GBK 时不加这两个：用户认定输出是 GBK（v2.6.1 时 Java 18 以上的服务器在中文 Windows 上就是），
// 再强制成 UTF-8，MineDeck 按 GBK 解反而成了乱码。
var consoleUTF8 = []string{"-Dstdout.encoding=UTF-8", "-Dstderr.encoding=UTF-8"}

// launchArgs 构造 JVM 启动参数（进程托管与 run.bat 共用同一套逻辑）。
func launchArgs(i *Instance) []string {
	args := []string{
		fmt.Sprintf("-Xms%dM", i.XmsMB),
		fmt.Sprintf("-Xmx%dM", i.XmxMB),
		"-Dfile.encoding=UTF-8",
	}
	if i.ConsoleEncoding != "gbk" {
		args = append(args, consoleUTF8...)
	}
	args = append(args, i.ExtraJVM...)
	if rest, ok := strings.CutPrefix(i.LaunchTarget, "args:"); ok {
		args = append(args, "@"+filepath.FromSlash(rest), "nogui")
	} else if rest, ok := strings.CutPrefix(i.LaunchTarget, "jar:"); ok {
		args = append(args, "-jar", rest, "nogui")
	}
	return args
}

// batEscapeTitle 转义 title 行中的 cmd 元字符（title 命令无法用引号包裹，只能逐字符转义）。
// 换行这类控制字符换成空格：换行会让 title 那一行断开，后半截成了另一条命令。新建时名字已经不许带控制字符，
// 这里兜住以前存下的、或者接管来的 instance.json 里的名字（v3.1.0 正式版前检查 B-7）。
func batEscapeTitle(s string) string {
	return strings.NewReplacer(
		"%", "%%", "^", "^^", "&", "^&", "|", "^|",
		"<", "^<", ">", "^>", "(", "^(", ")", "^)",
	).Replace(batNoControl(s))
}

// batQuoteArg 把一个参数写成 run.bat 里的一段：cmd 读这一行时不会多执行命令，java.exe 拆参数时拿回原样的一个参数。
// 两边的规矩不一样，要同时照顾到：
//   - cmd：参数一律加引号，& | < > 这类符号在引号里不起作用；% 写成两个，不然批处理会把它当成变量。
//   - 参数里自带的双引号写成两个：cmd 看到的是「关上、马上又打开」，中间没有露在引号外的字；java.exe 在引号里
//     看到两个双引号，收一个字面的双引号。不能写成 \"：cmd 不认反斜杠，会以为引号到这里就关上了。
//     以前自带的引号原样写进去，会把外面的引号提前关掉，后面的 & echo … 就成了命令（v3.1.0 正式版前检查 B-7）。
//   - java.exe 拆参数的规矩和 VC 运行库一样：反斜杠挨着双引号时才算转义。所以双引号前面的反斜杠、参数结尾的
//     反斜杠都要写成两个，不然收尾的引号被转义掉，后面的参数全都串位（-Ddir=C:\My Server\ 这种就会）。
//     这一条是复审时发现的，以前就有。
//
// 控制字符换成空格。保存设置时 JVM 参数里已经不许有双引号和控制字符，这里兜住以前存下的、接管来的。
func batQuoteArg(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0 // 还没写出去的连续反斜杠：后面跟的是什么，决定写一倍还是两倍
	for _, r := range batNoControl(s) {
		switch r {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes))
			b.WriteString(`""`)
		case '%':
			b.WriteString(strings.Repeat(`\`, slashes))
			b.WriteString("%%")
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			b.WriteRune(r)
		}
		slashes = 0
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}

// batNoControl 把换行、制表符这类控制字符换成空格，免得一个值在 run.bat 里断成好几行。
func batNoControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// writeRunBat 生成可手动双击的启动脚本（与 GUI 内启动等效）。
// 开头关掉延迟展开：有人在注册表里给 cmd 全局打开了它的话，名字和参数里成对的感叹号之间的字会被当成变量换掉
// （v3.1.0 正式版前检查「可以放到以后的」里那条）。
func writeRunBat(i *Instance) error {
	var sb strings.Builder
	sb.WriteString("@echo off\r\n")
	sb.WriteString("setlocal DisableDelayedExpansion\r\n")
	sb.WriteString("chcp 65001 >nul\r\n")
	sb.WriteString(fmt.Sprintf("title %s - MineDeck\r\n", batEscapeTitle(i.Name)))
	sb.WriteString("cd /d \"%~dp0\"\r\n")
	sb.WriteString(batQuoteArg(i.JavaPath))
	for _, a := range launchArgs(i) {
		sb.WriteString(" " + batQuoteArg(a))
	}
	sb.WriteString("\r\npause\r\n")
	// 现代 Forge/NeoForge 同步维护 user_jvm_args.txt，便于手动使用官方 run.bat 的用户
	if strings.HasPrefix(i.LaunchTarget, "args:") {
		content := fmt.Sprintf("# MineDeck 生成\n-Xms%dM\n-Xmx%dM\n", i.XmsMB, i.XmxMB)
		_ = os.WriteFile(filepath.Join(i.Dir, "user_jvm_args.txt"), []byte(content), 0o644)
	}
	return os.WriteFile(filepath.Join(i.Dir, "run.bat"), []byte(sb.String()), 0o644)
}
