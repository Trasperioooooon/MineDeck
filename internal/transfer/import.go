package transfer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 交接包导入：把 A 的 .mdpack 解开，走创建流水线的变体（下核心 + 确保 Java），再解包覆盖，
// run.bat / instance.json 按本机的情况重新生成，注册成 B 这边的一台服务器。
// manifest 校验不过、核心下不来、解包越界（zip-slip）都停在中途并把新建目录清理干净。

var importSteps = []string{"校验交接包", "准备 Java 运行时", "下载服务端核心", "安装服务端", "解包服务器文件", "生成启动脚本"}

// ImportReq 一次导入。
type ImportReq struct {
	File  string // 已上传到缓存目录的交接包文件
	Name  string // B 这边给这台服务器起的名字
	XmxMB int    // 0 = 沿用包里 A 分配的内存
	Port  int    // 0 = 沿用包内 server.properties 的端口；给了且和包里不同会写进 server.properties
	EULA  bool   // 包里没同意过 EULA 时必须为 true
	// JavaMajor 用哪个 Java 大版本；0 = 包里原来用的那个，包里没写就按 Minecraft 版本选。
	// 魔改模组可能要比官方元数据更新的 Java，原服主用的那个通常就是对的；接手的人想换也可以在这里先选好。
	JavaMajor int
	Root      string // 自定义存放根（仅本机有效，远程 / 局域网请求被操作层清空）
}

// ImportAsync 校验请求并在后台导入，返回任务编号；任务成功后 result 是实例名。
func ImportAsync(m *inst.Manager, path string, req ImportReq) (string, error) {
	mf, err := ReadManifest(path)
	if err != nil {
		return "", err
	}
	if err := mf.check(); err != nil {
		return "", err
	}
	if req.JavaMajor != 0 && (req.JavaMajor < 8 || req.JavaMajor > 40) {
		return "", fmt.Errorf("Java 版本应在 8 到 40 之间")
	}
	kind := mcsrc.KindOf(mf.Core.Core)
	// EULA 的三种情况：包里带同意过的 eula.txt（以它为准）、包没带但 B 勾了同意（导入时写入）、都没有则拒绝
	eulaOK := req.EULA || packEULAAccepted(path)
	if kind != mcsrc.KindProxy && !eulaOK {
		return "", fmt.Errorf("需要同意 Minecraft EULA 才能开服")
	}
	creq := inst.CreateReq{
		Name: req.Name, Core: mf.Core.Core, MC: mf.Core.MC, Build: mf.Core.Build,
		XmxMB: req.XmxMB, EULA: eulaOK, Root: req.Root,
	}
	// validateCreate 的老路：名字、目录去重、「创建中」占位都在里面
	dir, err := m.ReserveCreate(&creq)
	if err != nil {
		return "", err
	}
	// 往下一律用校验过的名字（ReserveCreate 会修剪前后的空格）：占位、注册、失败回滚要是各用各的写法，
	// 占位放不掉，回滚还可能把另一台名字只差空格的服务器移出列表
	req.Name = creq.Name
	title := mf.Instance.Name
	if strings.TrimSpace(title) == "" {
		title = req.Name
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("导入交接包 %s（%s %s）", title, mf.Core.Core, mf.Core.MC), importSteps)
	t.SetKind("transfer-in")
	go func() {
		defer m.ReleaseCreate(req.Name, dir)
		defer os.Remove(path) // 上传的包在导入结束（成功/失败）后删除：交接包动辄几个 GB，不能留副本攒着
		if err := runImport(ctx, t, m, mf, path, req, dir); err != nil {
			t.Fail(err)
			_ = m.Delete(req.Name, false) // 万一已经注册（收尾失败）：从列表移出
			os.RemoveAll(dir)             // 目录是本程序新建的半成品，整体清掉
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

// runImport 一段一段做完导入。req 是 B 这一侧的原始请求（不是 validateCreate 规范化过的：port=0 表示沿用包里的）。
func runImport(ctx context.Context, t *tasks.Task, m *inst.Manager, mf *Manifest, path string, req ImportReq, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Minute)
	defer cancel()
	kind := mcsrc.KindOf(mf.Core.Core)

	// ---- 步骤 0：校验交接包 ----
	t.StartStep(0)
	t.Logf("交接包：%s（%s %s，%s 导出，%s）", mf.Instance.Name, mf.Core.Core, mf.Core.MC, mf.ExportedAt.Local().Format("2006-01-02 15:04"), ModeName(mf.Mode))
	t.Logf("包内清单：世界 %s、模组 %d 个、插件 %d 个", fmtBytes(mf.WorldBytes), len(mf.Mods), len(mf.Plugins))
	if len(mf.Excluded) > 0 {
		t.Logf("包里没有：%s", strings.Join(mf.Excluded, "、"))
	}
	// 版本可得性：官方清单认得这个版本，才谈得上按版本下载核心（离线时用磁盘缓存的清单）
	known := true
	if kind != mcsrc.KindProxy {
		var err error
		if known, err = mcsrc.KnownVersion(ctx, mf.Core.MC); err != nil {
			t.Logf("没拿到 Minecraft 版本清单（%v），版本相关的事先跳过", err)
			known = false
		} else if !known {
			t.Logf("官方清单里没有版本 %s，核心下载可能失败", mf.Core.MC)
		}
	}
	if mf.Full {
		t.Logf("完整模式：核心启动文件随包带来，不再联网下载")
	}

	// ---- 步骤 1：准备 Java ----
	t.StartStep(1)
	major := mf.Instance.JavaMajor
	if major < 8 || major > 40 {
		major = 0 // 包里写的不像 Java 版本号：当成没写，按 Minecraft 版本选
	}
	if req.JavaMajor != 0 {
		major = req.JavaMajor
	}
	switch {
	case req.JavaMajor != 0:
		t.Logf("按你选的用 Java %d", major)
	case major != 0:
		t.Logf("交接的那台用 Java %d（来自 A 的 instance.json）", major)
	case kind == mcsrc.KindProxy:
		major = mcsrc.FixedJavaOf(mf.Core.Core)
	default:
		if known {
			if meta, err := mcsrc.GetVersionMeta(ctx, mf.Core.MC); err == nil {
				major = meta.JavaMajor
			}
		}
		if major == 0 {
			return fmt.Errorf("包里没写要用的 Java 大版本，官方清单里也没有 %s，没法替它自动选 Java", mf.Core.MC)
		}
		t.Logf("MC %s 需要 Java %d（来自官方元数据）", mf.Core.MC, major)
	}
	javaPath, err := java.Resolve(ctx, major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", major)))
	if err != nil {
		return err
	}
	t.Logf("Java 就绪: %s", javaPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// ---- 步骤 2 / 3：下载核心、安装服务端（完整模式跳过） ----
	launch := mf.Instance.LaunchTarget
	build := mf.Core.Build
	if mf.Full {
		t.StartStep(2)
		t.Logf("核心启动文件随包带来，跳过下载")
		t.StartStep(3)
		t.Logf("依赖库也在包里，跳过安装器")
	} else {
		var b string
		launch, b, err = downloadCore(ctx, t, mf, dir, javaPath)
		if err != nil {
			return err
		}
		if b != "" {
			build = b // Fabric 交接时没带加载器版本的：按清单选了一个
		}
	}

	// ---- 步骤 4：解包服务器文件（instance/ 前缀，严格 zip-slip） ----
	t.StartStep(4)
	res, err := extractPrefix(ctx, path, dir,
		extractOpts{slim: !mf.Full, declared: mf.Counts.Bytes, declaredFiles: mf.Counts.Files, free: func() (uint64, error) {
			n, err := app.DiskFree(dir)
			if err != nil {
				t.Logf("没查到存放位置所在的盘还剩多少空间（%v），这一步检查跳过", err)
			}
			return n, err
		}},
		t.ProgressFn("解包交接包"),
		func(n int, done, total int64) {
			t.ProgressFn(fmt.Sprintf("解包交接包（已 %d 个文件）", n))(done, total)
		})
	if err != nil {
		return err
	}
	t.Logf("解出 %s", fmtBytes(res.bytes))
	if len(res.control) > 0 {
		t.Logf("包里带的 %s 没有采用：这几个文件由 MineDeck 按这台电脑的情况重新生成", strings.Join(res.control, "、"))
	}
	if len(res.kept) > 0 {
		t.Logf("⚠ 这个包没带服务端核心，核心用的是刚下载的那份；包里有 %d 个属于核心的文件（%s）没有采用", len(res.kept), shortList(res.kept, 5))
	}
	// 解包时已经按名字跳过了控制文件，这里再按磁盘上的实际情况清一遍：名字的比较交给 Windows 自己做
	// （大小写、8.3 短名这些别名它都认），不管包里用什么写法，目录里都不会留着别人给的这几样。
	// 瘦身模式下官方安装器生成的 run.bat、user_jvm_args.txt 也在其中，下面收尾时 MineDeck 会重新写。
	inst.ScrubControlFiles(dir)
	// 包里的 server.properties 是原样用的：世界目录的名字指到服务器文件夹外面的包不要
	// （服务器一启动就会去动那个文件夹，备份和还原也跟着找过去）。
	if name, ok := inst.CheckLevelName(dir); !ok {
		return fmt.Errorf("交接包里的 server.properties 把世界目录（level-name）指到了服务器文件夹外面：%s", name)
	}

	// ---- 步骤 5：生成启动脚本 + 注册 ----
	t.StartStep(5)
	if mf.Full && !strings.HasPrefix(launch, "jar:") {
		target, err := inst.LocateLaunchTarget(dir, mf.Core.Core, ctx, mf.Core.MC)
		if err != nil {
			return err
		}
		launch = target
		t.Logf("启动目标: %s", launch)
	}
	if mf.Full {
		// 完整模式带来的启动文件必须在包里（陈旧 instance.json 写着的文件被打包时可能已不在）：早失败，别等启动才炸
		rel, ok := strings.CutPrefix(launch, "jar:")
		if !ok {
			rel, ok = strings.CutPrefix(launch, "args:")
		}
		if ok {
			if !filepath.IsLocal(filepath.FromSlash(rel)) {
				return fmt.Errorf("交接包里写的启动文件不在服务器文件夹里：%s", rel)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return fmt.Errorf("包里缺启动文件 %s（A 那边的实例可能已经不完整）", rel)
			}
		}
	}
	// 包里的 JVM 参数、定时任务、崩溃自动重启不带过来：它们决定这台电脑上执行什么
	// （-javaagent、-XX:OnError 能直接跑程序；定时任务能每天执行一条服务器命令，比如把原服主重新设成管理员），
	// 而导入向导上看不到。写进日志，要用的话接手的人自己在设置里加。
	old := mf.Instance
	if len(old.ExtraJVM) > 0 {
		t.Logf("原来的 JVM 参数没有带过来，需要的话到「内存 / 启动」里自己加：%s", shortList(old.ExtraJVM, 12))
		for _, a := range old.ExtraJVM {
			if strings.HasPrefix(a, "-javaagent:") {
				t.Logf("⚠ 原来的服务器启动时挂了外部程序（-javaagent，外置登录就是这么接的）：对应的 jar 不在交接包里。还要用的话，向原服主要来这个 jar，确认可信以后放进服务器文件夹，再把这条参数加回去；不加的话依赖它的功能（比如外置登录）不起作用")
				break
			}
		}
	}
	if n := len(old.Policies.Schedules); n > 0 || old.Policies.CrashRestart {
		t.Logf("原来的定时任务（%d 条）和崩溃自动重启的设置没有带过来，需要的话在这台服务器的设置里重新加", n)
	}
	// 这台服务器的设置从头填，只从包里取两样：分了多少内存、控制台用什么编码。
	// 不拿包里那份整个改：以后 Settings 加了新字段，不会不知不觉地跟着包进来。
	s := inst.Settings{
		Name: req.Name, Core: mf.Core.Core, MC: mf.Core.MC, Build: build,
		JavaMajor: major, JavaPath: javaPath,
		XmxMB: old.XmxMB, LaunchTarget: launch, CreatedAt: time.Now(),
	}
	if old.ConsoleEncoding == "utf-8" || old.ConsoleEncoding == "gbk" {
		s.ConsoleEncoding = old.ConsoleEncoding
	}
	if req.XmxMB > 0 {
		s.XmxMB = req.XmxMB
	}
	if s.XmxMB < 512 {
		s.XmxMB = 2048
	}
	s.XmsMB = min(1024, s.XmxMB)
	// 端口按 B 的填写改（server.properties 就在包里；代理端没有）。这里和下面关 RCON 都按服务端的读法来
	// （inst.JavaProps / inst.ForceProp）：同一项写了两行时服务端认的是最后一行，只改第一行等于没改。
	propsPath := filepath.Join(dir, "server.properties")
	if req.Port > 0 && req.Port <= 65535 && kind != mcsrc.KindProxy {
		want := fmt.Sprintf("%d", req.Port)
		old := ""
		if b, err := os.ReadFile(propsPath); err == nil {
			old = inst.JavaProps(b)["server-port"]
		}
		if old != want { // 包里没有 server.properties（那台服务器一次都没开过）时新写一份，免得填的端口白填
			if err := inst.ForceProp(propsPath, "server-port", want); err != nil {
				return err
			}
			if old != "" {
				t.Logf("端口按你的填写改成了 %d（包里是 %s）", req.Port, old)
			}
		}
	}
	// RCON：包里的 server.properties 开着远程控制台的话，密码是原服主设的、他知道——和不带过来的定时任务是一回事，先关掉。
	if kind != mcsrc.KindProxy {
		if b, err := os.ReadFile(propsPath); err == nil && strings.EqualFold(strings.TrimSpace(inst.JavaProps(b)["enable-rcon"]), "true") {
			if err := inst.ForceProp(propsPath, "enable-rcon", "false"); err != nil {
				return err
			}
			t.Logf("原来开着 RCON（远程控制台），密码是原服主设的，已经先关掉。要用的话到「全部配置」里换一个密码再打开")
		}
	}
	// EULA：包里带了同意过的 eula.txt 就以它为准；没带时靠请求里的勾选写
	if kind != mcsrc.KindProxy {
		eulaPath := filepath.Join(dir, "eula.txt")
		if b, err := os.ReadFile(eulaPath); err != nil || !eulaAccepted(b) {
			if !req.EULA {
				return fmt.Errorf("需要同意 Minecraft EULA 才能开服")
			}
			if err := os.WriteFile(eulaPath, []byte(eulaText), 0o644); err != nil {
				return err
			}
			t.Logf("已写入 eula.txt（你在上一步同意了 Minecraft EULA）")
		}
	}
	if kind != mcsrc.KindProxy { // log4shell 防护（老版本），和新建、接管时一样按这台电脑重新套一遍
		var err error
		if known {
			err = inst.ApplyLog4jMitigation(ctx, t, &s, dir)
		}
		if !known || err != nil {
			// 判断不了版本（离线、清单里没有）：原服主的 JVM 参数已经整个丢掉了，他原来带着的防护参数也在里面，
			// 所以不能就这么空着。这一条参数对不需要它的新版本没有影响，先加上。
			s.ExtraJVM = append(s.ExtraJVM, inst.Log4jNoLookups)
			t.Logf("⚠ 判断不了这个版本要不要 log4shell 防护（多半是没联网），先加上 formatMsgNoLookups 这一条。它对新版本没有影响；1.17 以前的老版本还需要 Mojang 的日志配置文件，这次没能准备，这样的服先不要开给不认识的人进")
		}
	}
	if _, err := m.FinalizeCreate(s, dir); err != nil {
		return err
	}
	t.Logf("交接完成 ✔ 存档、配置和模组都带过来了（没带过来的东西见上面几行）")
	return nil
}

// downloadCore 瘦身模式：按 manifest 走创建流水线的同一条路把核心拉下来（含源切换与校验）。
// 返回启动方式和（Fabric 特有）定下来的加载器版本。
func downloadCore(ctx context.Context, t *tasks.Task, mf *Manifest, dir, javaPath string) (launch, build string, err error) {
	t.StartStep(2)
	core, mc := mf.Core.Core, mf.Core.MC
	build = mf.Core.Build
	var meta *mcsrc.VersionMeta
	if mcsrc.KindOf(core) != mcsrc.KindProxy {
		if meta, err = mcsrc.GetVersionMeta(ctx, mc); err != nil {
			return "", "", err
		}
	}
	switch core {
	case mcsrc.CoreVanilla:
		if err := inst.FetchArtifact(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+mc, dir, "server.jar"); err != nil {
			return "", "", err
		}
		return "jar:server.jar", "", nil

	case mcsrc.CoreFabric:
		loader := build
		if loader == "" || loader == "latest" {
			loaders, err := mcsrc.FabricLoaders(ctx, mc)
			if err != nil {
				return "", "", err
			}
			loader = loaders[0].ID
			for _, l := range loaders {
				if l.Recommended {
					loader = l.ID
					break
				}
			}
		}
		installerV, err := mcsrc.FabricInstallerVersion(ctx)
		if err != nil {
			return "", "", err
		}
		art := mcsrc.FabricServerArtifact(mc, loader, installerV)
		if err := inst.FetchArtifact(ctx, t, art, "fabric", dir, art.FileName); err != nil {
			return "", "", err
		}
		t.Logf("预下载原版服务端供 Fabric 使用")
		if err := inst.FetchArtifact(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+mc, dir, "server.jar"); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "fabric-server-launcher.properties"), []byte("serverJar=server.jar\n"), 0o644); err != nil {
			return "", "", err
		}
		return "jar:" + art.FileName, loader, nil

	case mcsrc.CoreForge:
		art := mcsrc.ForgeInstallerArtifact(ctx, mc, build)
		if err := inst.FetchArtifact(ctx, t, art, "forge", dir, art.FileName); err != nil {
			return "", "", err
		}
		// 传统 Forge（<=1.16.5）安装器要直连 Mojang 下原版服务端：预放置让它校验后跳过
		if newer117, err := mcsrc.NewerOrEqual(ctx, mc, "1.17"); err == nil && !newer117 {
			t.Logf("预放置原版服务端（加速安装器）")
			if err := inst.FetchArtifact(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+mc, dir, "minecraft_server."+mc+".jar"); err != nil {
				return "", "", err
			}
		}
		return installCore(ctx, t, javaPath, dir, fmt.Sprintf("forge-%s-%s-installer.jar", mc, build), core, mc)

	case mcsrc.CoreNeoForge:
		_, paths, err := mcsrc.NeoBuilds(ctx, mc)
		if err != nil {
			return "", "", err
		}
		art := mcsrc.NeoInstallerArtifact(build, paths[build])
		if err := inst.FetchArtifact(ctx, t, art, "neoforge", dir, art.FileName); err != nil {
			return "", "", err
		}
		t.Logf("预放置原版服务端（加速安装器）")
		preDest := filepath.Join("libraries", "net", "minecraft", "server", mc, "server-"+mc+".jar")
		if err := inst.FetchArtifact(ctx, t, mcsrc.VanillaServerArtifact(meta), "vanilla/"+mc, dir, preDest); err != nil {
			return "", "", err
		}
		return installCore(ctx, t, javaPath, dir, fmt.Sprintf("neoforge-%s-installer.jar", build), core, mc)
	}
	// 通用 jar 型核心（Paper/Purpur/Leaves/Folia/Mohist/Banner/Velocity/Waterfall）
	art, err := mcsrc.GenericArtifact(ctx, core, mc, build)
	if err != nil {
		return "", "", err
	}
	if err := inst.FetchArtifact(ctx, t, art, string(core), dir, "server.jar"); err != nil {
		return "", "", err
	}
	return "jar:server.jar", "", nil
}

// installCore Forge / NeoForge：跑安装器，随后探测启动方式，删掉安装器 jar。
func installCore(ctx context.Context, t *tasks.Task, javaExe, dir, installerJar string, core mcsrc.Core, mc string) (string, string, error) {
	t.StartStep(3)
	t.Logf("运行官方安装器（将从网络下载依赖库，耗时视网络情况数分钟）...")
	if err := inst.RunInstaller(ctx, t, javaExe, dir, installerJar); err != nil {
		return "", "", err
	}
	target, err := inst.LocateLaunchTarget(dir, core, ctx, mc)
	if err != nil {
		return "", "", err
	}
	t.Logf("启动目标: %s", target)
	os.Remove(filepath.Join(dir, installerJar))
	return target, "", nil
}

// eulaText eula.txt 的内容（与创建流水线一致）。
const eulaText = "# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"

// reEULA 和接管已有服务器时用的是同一条：单独一行的 eula=true 才算。
// 原来只看文件里有没有这几个字，"#eula=true" 加 "eula=false" 也算同意过，导入成功以后服务器却起不来。
var reEULA = regexp.MustCompile(`(?im)^\s*eula\s*=\s*true\s*$`)

// eulaAccepted eula.txt 的内容里是不是同意过。
func eulaAccepted(b []byte) bool { return reEULA.Match(b) }

// shortList 把一串名字拼成一句话里放得下的样子：最多列 max 个，多的写「等 N 项」。
func shortList(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, "、")
	}
	return fmt.Sprintf("%s 等 %d 项", strings.Join(items[:max], "、"), len(items))
}

// packEULAAccepted 只读包内 instance/eula.txt（一条小文件），看 A 那边同意过没有。
func packEULAAccepted(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != PayloadPrefix+"eula.txt" || f.UncompressedSize64 > 64<<10 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false
		}
		b, err := io.ReadAll(io.LimitReader(rc, 64<<10))
		rc.Close()
		if err != nil {
			return false
		}
		return eulaAccepted(b)
	}
	return false
}
