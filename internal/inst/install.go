package inst

import (
	"bufio"
	"context"
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
	// LocalSource 整合包导入的「本地来源目录」：用户手里还留着的老服务器目录（如 D:\old-server）。
	// 待决（无法自动下载）的文件凡在这个目录里有对应的，按原样复制进实例（不校验、要求线上匹配），
	// 用户的魔改模组因此直接保留；目录里没有的留在待决清单交给用户处置。
	// 读的是主机上的任意路径，和 root 一样：仅本机请求有效，远程 / 局域网 / AI 一律忽略。
	LocalSource string `json:"localSource,omitempty"`
	Root        string `json:"root"` // 自定义存放根目录（空=全局默认），仅创建时使用
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
	if !taken && !inflight {
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
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		if err := m.runCreate(ctx, t, req, dir); err != nil {
			os.RemoveAll(dir) // 先清理半成品目录再报失败（缓存保留，重试无需重新下载）
			t.Fail(err)
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
		t.Logf("代理端无需 EULA 与 server.properties，首次启动后在服务器文件夹里编辑它自带的配置文件")
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
	t.Logf("服务器创建完成 ✔")
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
		OnRetry: func(host string, err error) {
			t.Logf("从 %s 下载 %s 没成功（%v），换下一个下载源或稍后重试", host, art.FileName, err)
			t.NetRetry(host, "下载失败")
		},
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
		if err := fetchLog4jXML(ctx, i.Dir); err != nil {
			t.Logf("log4j 防护配置下载失败（不影响开服，仅安全性降低）: %v", err)
			return nil
		}
		i.ExtraJVM = append(i.ExtraJVM, log4jConfig)
		t.Logf("已启用 log4shell 漏洞缓解（Mojang 官方方案）")
	}
	return nil
}

// fetchLog4jXML 把 Mojang 给 1.12–1.16.5 的日志配置放进实例目录。已经在、内容也对得上的不重新下载；
// 内容对不上的（被别的文件顶掉了）会重新下载。这份文件可能经镜像下载，按官方的 SHA-1 校验
// （就是下载地址里那一串；2026-11-10 下载核对过）。
var fetchLog4jXML = func(ctx context.Context, dir string) error { // 是变量：测试里换成不联网的
	const sum = "02937d122c86ce73319ef9975b58896fc1b491d1"
	const xmlURL = "https://launcher.mojang.com/v1/objects/" + sum + "/" + log4jXML
	return dl.Fetch(ctx, dl.Request{URLs: mcsrc.URLPair(xmlURL), Dest: filepath.Join(dir, log4jXML), MinSize: 100, SHA1: sum}, nil)
}

// restoreLog4jXML 启动参数里引用着官方日志配置的实例，确认那份文件还是官方的（不是就重新下载）。
// 整合包导入收尾时调：覆盖目录和清单里的文件是在它下载好以后才写进来的。
func restoreLog4jXML(ctx context.Context, t *tasks.Task, i *Instance) {
	if !slices.Contains(i.ExtraJVMSnapshot(), log4jConfig) {
		return
	}
	if err := fetchLog4jXML(ctx, i.Dir); err != nil {
		t.Logf("⚠ 没能确认 log4j 防护配置是官方的那一份（%v）。联网以后可以删掉服务器文件夹里的 %s 再重新导入", err, log4jXML)
	}
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
// 换行这类控制字符换成空格：换行会让 title 那一行断开，后半截成了另一条命令。新建时名字已经不许带控制字符，
// 这里兜住以前存下的、或者接管来的 instance.json 里的名字（v3.1.0 正式版前检查 B-7）。
func batEscapeTitle(s string) string {
	return strings.NewReplacer(
		"%", "%%", "^", "^^", "&", "^&", "|", "^|",
		"<", "^<", ">", "^>", "(", "^(", ")", "^)",
	).Replace(batNoControl(s))
}

// batQuoteArg 参数一律加引号并把 % 翻倍，防实例名 / JVM 参数含 cmd 元字符时用户双击 run.bat 触发命令注入。
// 参数里自带的双引号写成两个：cmd 看到的是「关上、马上又打开」，中间没有露在引号外面的字，& 这类符号始终在引号里；
// Java 收到的仍是一个双引号。以前原样写进去，自带的引号会把外面的引号提前关掉，后面的 & echo … 就成了命令。
// 控制字符同样换成空格（B-7）。
func batQuoteArg(s string) string {
	return `"` + strings.NewReplacer("%", "%%", `"`, `""`).Replace(batNoControl(s)) + `"`
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
