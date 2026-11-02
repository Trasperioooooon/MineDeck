package inst

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/app"
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
	Root        string     `json:"root"` // 自定义存放根目录（空=全局默认），仅创建时使用
}

// createSteps 创建流水线的步骤名（整合包导入在其后追加额外步骤）。
var createSteps = []string{"解析版本信息", "准备 Java 运行时", "下载服务端核心", "安装服务端", "写入配置文件", "生成启动脚本"}

// validateCreate 校验创建请求并返回目标目录（供普通创建与整合包导入共用）。
func (m *Manager) validateCreate(req *CreateReq) (string, error) {
	req.Name = strings.TrimSpace(req.Name)
	if err := validateName(req.Name); err != nil {
		return "", err
	}
	if !mcsrc.ValidCore(req.Core) {
		return "", fmt.Errorf("未知核心类型: %s", req.Core)
	}
	if req.MC == "" {
		return "", fmt.Errorf("请选择版本")
	}
	// 代理端不运行 Minecraft 本体，无需 Mojang EULA
	if !req.EULA && mcsrc.KindOf(req.Core) != mcsrc.KindProxy {
		return "", fmt.Errorf("需要同意 Minecraft EULA 才能开服")
	}
	m.mu.Lock()
	_, exists := m.insts[req.Name]
	m.mu.Unlock()
	if exists {
		return "", fmt.Errorf("实例名已存在: %s", req.Name)
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
		return "", fmt.Errorf("存放目录不可用: %w", err)
	}
	// 目录名用 slug（与中文显示名解耦）；同名自动追加序号
	slug := slugify(req.Name)
	dir := filepath.Join(root, slug)
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		if n > 999 {
			return "", fmt.Errorf("无法为实例分配目录（同名目录过多）")
		}
		dir = filepath.Join(root, fmt.Sprintf("%s-%d", slug, n))
	}
	// Mohist/Banner 的 javaagent 自举对非 ASCII 路径不兼容——校验最终路径而非实例名，
	// 因此中文实例名在纯英文路径下也可正常使用。
	if (req.Core == mcsrc.CoreMohist || req.Core == mcsrc.CoreBanner) && !isASCII(dir) {
		return "", fmt.Errorf("Mohist/Banner 与非英文路径不兼容，请把存放目录设为纯英文路径（当前将创建于：%s）", dir)
	}
	// 所有校验通过后，原子性登记「创建中」占位。实例要到流水线末尾（Finish）才写入 m.insts，
	// 期间同名请求（一键部署双击 / 重复 POST）此前都能各自通过校验并建目录，完成时相互覆盖 map
	// 造成孤儿实例。此处在返回目录前占位，重复请求会被拒；调用方 goroutine 负责最终 releaseCreating。
	m.mu.Lock()
	_, taken := m.insts[req.Name]
	inflight := m.creating[req.Name]
	if !taken && !inflight {
		m.creating[req.Name] = true
	}
	m.mu.Unlock()
	if taken {
		return "", fmt.Errorf("实例名已存在: %s", req.Name)
	}
	if inflight {
		return "", fmt.Errorf("「%s」正在创建中，请稍候", req.Name)
	}
	return dir, nil
}

// releaseCreating 释放实例名的「创建中」占位；由创建/导入 goroutine 在任务结束时（无论成败）调用。
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
	t, ctx := m.Tasks.New(fmt.Sprintf("创建实例 %s（%s %s）", req.Name, req.Core, req.MC), createSteps)
	go func() {
		defer m.releaseCreating(req.Name)
		if err := m.runCreate(ctx, t, req, dir); err != nil {
			t.Fail(err)
			os.RemoveAll(dir) // 清理半成品目录（缓存保留，重试无需重新下载）
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

func (m *Manager) runCreate(ctx context.Context, t *tasks.Task, req CreateReq, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Minute)
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
		art := mcsrc.NeoInstallerArtifact(req.Build, paths[req.Build])
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
		t.Logf("代理端无需 EULA 与 server.properties，首次启动后在实例目录编辑其自带配置文件")
	} else {
		if err := os.WriteFile(filepath.Join(dir, "eula.txt"),
			[]byte("# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
			return err
		}
		props, _ := LoadProps(filepath.Join(dir, "server.properties"))
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

	// ---- 步骤 5：启动脚本 + 注册实例 ----
	t.StartStep(5)
	if err := writeRunBat(inst); err != nil {
		return err
	}
	if err := m.saveInstance(inst); err != nil {
		return err
	}
	app.RememberRoot(filepath.Dir(dir)) // 记录自定义根，重启后仍能扫描到
	m.mu.Lock()
	m.insts[req.Name] = inst
	m.mu.Unlock()
	t.Logf("实例创建完成 ✔")
	return nil
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
	}, t.ProgressFn("下载 "+art.FileName)); err != nil {
		return err
	}
	return copyFile(cachePath, filepath.Join(dir, destName))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
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
	return fmt.Errorf("安装器多次运行失败: %w（可查看实例目录 installer.jar.log；也可在全局设置切换下载源后重试）", lastErr)
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
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			t.Logf("[安装器] %s", line)
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("安装器退出异常: %w", err)
	}
	return nil
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
	return "", fmt.Errorf("安装器未生成可识别的服务端启动文件，请查看实例目录中的 installer.log")
}

// log4shell 缓解参数（Mojang 官方方案）：<1.18.1 加 noLookups，1.12.2 ~ 1.16.5 另引用官方 xml 日志配置。
const (
	log4jNoLookups = "-Dlog4j2.formatMsgNoLookups=true"
	log4jXML       = "log4j2_112-116.xml" // 创建时下载到实例目录
	log4jConfig    = "-Dlog4j.configurationFile=" + log4jXML
)

// applyLog4jMitigation 依据 Mojang 官方公告为老版本添加 log4shell 缓解。
func applyLog4jMitigation(ctx context.Context, t *tasks.Task, i *Instance) error {
	newer1181, err := mcsrc.NewerOrEqual(ctx, i.MC, "1.18.1")
	if err != nil || newer1181 {
		return err
	}
	i.ExtraJVM = append(i.ExtraJVM, log4jNoLookups)
	newer117, err := mcsrc.NewerOrEqual(ctx, i.MC, "1.17")
	if err != nil {
		return err
	}
	if !newer117 { // 1.12.2 ~ 1.16.5：官方 xml 配置
		const xmlURL = "https://launcher.mojang.com/v1/objects/02937d122c86ce73319ef9975b58896fc1b491d1/log4j2_112-116.xml"
		dest := filepath.Join(i.Dir, log4jXML)
		if err := dl.Fetch(ctx, dl.Request{URLs: mcsrc.URLPair(xmlURL), Dest: dest, MinSize: 100}, nil); err != nil {
			t.Logf("log4j 防护配置下载失败（不影响开服，仅安全性降低）: %v", err)
			return nil
		}
		i.ExtraJVM = append(i.ExtraJVM, log4jConfig)
		t.Logf("已启用 log4shell 漏洞缓解（Mojang 官方方案）")
	}
	return nil
}

// log4jFlags 给出保存 JVM 参数时须强制保留的 log4shell 缓解参数，判定与创建时的 applyLog4jMitigation 一致；
// 只读本地清单不联网（mcsrc.NewerOrEqualCached），判定不了版本时保守沿用 i.ExtraJVM 中已有的缓解参数。
// 调用方须持 procMu。
func log4jFlags(i *Instance) []string {
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return nil // 代理端不运行 Minecraft 本体，创建时也不加
	}
	var flags []string
	if newer1181, ok := mcsrc.NewerOrEqualCached(i.MC, "1.18.1"); !ok {
		for _, a := range i.ExtraJVM {
			if a == log4jNoLookups || a == log4jConfig {
				flags = append(flags, a)
			}
		}
	} else if !newer1181 {
		flags = append(flags, log4jNoLookups)
		if newer117, _ := mcsrc.NewerOrEqualCached(i.MC, "1.17"); !newer117 { // 同一清单里 1.17 必可判定
			flags = append(flags, log4jConfig)
		}
	}
	// 官方 xml 只在创建时下载：文件不在（当初下载失败 / 被删）就不强加引用——引用不存在的配置文件会令 log4j
	// 退回只输出 ERROR 的默认配置，控制台日志与「启动完成」检测随之失效
	if _, err := os.Stat(filepath.Join(i.Dir, log4jXML)); err != nil {
		flags = slices.DeleteFunc(flags, func(a string) bool { return a == log4jConfig })
	}
	return flags
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
func batEscapeTitle(s string) string {
	return strings.NewReplacer(
		"%", "%%", "^", "^^", "&", "^&", "|", "^|",
		"<", "^<", ">", "^>", "(", "^(", ")", "^)",
	).Replace(s)
}

// batQuoteArg 参数一律加引号并把 % 翻倍，防实例名 / JVM 参数含 cmd 元字符时用户双击 run.bat 触发命令注入。
func batQuoteArg(s string) string {
	return `"` + strings.ReplaceAll(s, "%", "%%") + `"`
}

// writeRunBat 生成可手动双击的启动脚本（与 GUI 内启动等效）。
func writeRunBat(i *Instance) error {
	var sb strings.Builder
	sb.WriteString("@echo off\r\n")
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
