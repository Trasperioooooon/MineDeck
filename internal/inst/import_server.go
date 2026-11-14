package inst

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/diskguard"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
	"minedeck/internal/winname"
)

// 作者给的现成服务端压缩包：没有 modrinth.index.json / manifest.json，
// 但是 mods、plugins、server.properties 或服务端 jar 都在（有时外面还套了一层文件夹）。
// 不重新下载核心。解压到新建的实例目录后，用接管已有服务器的 scanServerDir 认核心和版本，再登记。
// 目录是 MineDeck 建的，不算「接管」：删服务器时文件一起删。压缩包里的 run.bat、instance.json
// 这些 MineDeck 自己的文件不采用，启动脚本按识别结果重新写。

var serverImportSteps = []string{"解压服务端文件", "识别核心和版本", "准备 Java", "登记到 MineDeck"}

func (m *Manager) importServerAsync(pack *modpack.Pack, req CreateReq) (string, error) {
	if pack == nil || pack.ZipPath == "" {
		return "", fmt.Errorf("服务端压缩包的路径丢了")
	}
	// 解析时已经认出来的核心先填上：Mohist 的路径检查、代理端免 EULA 用得上。
	// 解压之后以 scanServerDir 的结果为准，这里认错了还会改。
	if mcsrc.ValidCore(pack.Core) {
		req.Core = pack.Core
	}
	if pack.MC != "" {
		req.MC = pack.MC
	}
	if pack.Loader != "" {
		req.Build = pack.Loader
	}
	if err := checkInstanceReq(&req, false); err != nil {
		return "", err
	}
	dir, err := m.reserveInstanceDir(&req)
	if err != nil {
		return "", err
	}
	title := pack.Name
	if title == "" {
		title = req.Name
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("导入服务端 %s", title), serverImportSteps)
	t.EnableCancel()
	t.SetKind("server-in") // 任务页按它换说法：这条路不下载服务端
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		defer os.Remove(pack.ZipPath)
		if err := m.runServerImport(ctx, t, pack, req, dir); err != nil {
			err = cancelledOr(t, err)
			// 先清后报失败：任务一显示失败，半成品就得已经删干净了（失败框上写着可以重试；交接包导入同样的顺序）
			if cleanupErr := m.discardCreating(req.Name, dir); cleanupErr != nil {
				err = fmt.Errorf("%w；清理半成品失败：%v", err, cleanupErr)
			}
			t.Fail(err)
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

func (m *Manager) runServerImport(ctx context.Context, t *tasks.Task, pack *modpack.Pack, req CreateReq, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()

	t.StartStep(0)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	where := "压缩包最外层"
	if pack.Root != "" {
		where = "「" + pack.Root + "」这一层"
	}
	t.Logf("按现成服务端解压（文件在%s）", where)
	cleanDest := filepath.Clean(dir)
	var skipped []string        // 名字在 Windows 上不能用、没有解压的文件（最多记 20 个）
	var dups []string           // 和前面的文件只差大小写、没有解压的（最多记 20 个）
	seen := map[string]string{} // 已经解压的文件：不分大小写的路径 → 包里的写法
	n, err := modpack.ExtractServer(pack, func(rel string, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 和交接包一样：别人给的 instance.json、run.bat 不能落下来，不然下次启动就按包里写的去跑。
		// 名字在 Windows 上不能用的（CON、CON.txt、COM1.log 这类设备名，结尾是点或空格的）也和整合包覆盖目录一样跳过。
		if !overrideAllowed(rel) {
			if !IsControlFile(rel) && len(skipped) < 20 {
				skipped = append(skipped, rel)
			}
			return nil
		}
		// 只差大小写的两个名字在 Windows 上是同一个文件，后一个会把前一个盖掉（v3.1.0 正式版前检查 C-2e）：
		// 和名字不能用的一样跳过、日志里列出来，留前一个。不整包拒绝：服务端包常在 Linux 上打，那边这是两个文件，
		// 拒了整包就导不进来了。写法一模一样出现两次的照旧以后一个为准
		k := winname.Fold(rel)
		if first, ok := seen[k]; ok && first != rel {
			if len(dups) < 20 {
				dups = append(dups, rel)
			}
			return nil
		}
		seen[k] = rel
		target := filepath.Join(cleanDest, filepath.FromSlash(rel))
		if target == cleanDest || !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("整合包内含非法路径: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		lease, err := diskguard.Acquire(ctx, filepath.Dir(target), diskguard.Options{AllowUnknown: true})
		if err != nil {
			return err
		}
		defer lease.Close()
		w, err := os.Create(target)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(lease.Writer(w), r)
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		return cerr
	})
	if err != nil {
		return fmt.Errorf("解压服务端失败: %w", err)
	}
	ScrubControlFiles(dir)
	t.Logf("解压了 %d 个文件", n)
	if len(skipped) > 0 {
		t.Logf("这些文件的名字在 Windows 上不能用，没有解压：%s", strings.Join(skipped, "、"))
	}
	if len(dups) > 0 {
		t.Logf("这些文件和压缩包里前面的文件只差大小写，在 Windows 上是同一个文件，没有解压（留的是前面那个）：%s", strings.Join(dups, "、"))
	}
	if name, ok := CheckLevelName(dir); !ok {
		return fmt.Errorf("服务端的 server.properties 把世界文件夹（level-name）指到了服务器文件夹外面：%s", name)
	}

	t.StartStep(1)
	core, mc, build, launch, major, err := detectImportedServer(dir, pack)
	if err != nil {
		return err
	}
	if (core == mcsrc.CoreMohist || core == mcsrc.CoreBanner) && !isASCII(dir) {
		return fmt.Errorf("Mohist/Banner 与非英文路径不兼容，请把存放文件夹设为纯英文路径（当前将创建于：%s）", dir)
	}
	kind := mcsrc.KindOf(core)
	t.Logf("按接管已有服务器的办法认出：%s %s，启动文件 %s", core, mc, strings.TrimPrefix(strings.TrimPrefix(launch, "jar:"), "args:"))

	t.StartStep(2)
	if major == 0 && kind == mcsrc.KindProxy {
		major = mcsrc.FixedJavaOf(core)
	}
	if major == 0 {
		major = javaFromCachedManifest(mc)
	}
	if major == 0 {
		// 本地版本清单里没有这个版本时才上网问官方。问不到就让用户走接管，自己选 Java。
		if meta, merr := mcsrc.GetVersionMeta(ctx, mc); merr == nil {
			major = meta.JavaMajor
		}
	}
	if major < 8 {
		return fmt.Errorf("认出了 %s %s，但没法确定该用哪个 Java（服务端没写，本地也没有版本清单）。请先解压，用「新建服务器 → 接管已有服务器」自己选 Java。", core, mc)
	}
	t.Logf("使用 Java %d", major)
	javaPath, err := java.Resolve(ctx, major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", major)))
	if err != nil {
		return err
	}
	t.Logf("Java 就绪: %s", javaPath)

	t.StartStep(3)
	inst := &Instance{
		Settings: Settings{
			Name: req.Name, Core: core, MC: mc, Build: build,
			JavaMajor: major, JavaPath: javaPath,
			XmxMB: req.XmxMB, XmsMB: min(1024, req.XmxMB),
			LaunchTarget: launch,
			CreatedAt:    time.Now(),
		},
		Dir: dir, Console: newConsole(),
	}
	if kind != mcsrc.KindProxy {
		applyServerProps(ctx, t, dir, req, mc)
		if b, err := os.ReadFile(filepath.Join(dir, "eula.txt")); err != nil || !reEULA.Match(b) {
			if !req.EULA {
				return fmt.Errorf("需要同意 Minecraft EULA 才能开服")
			}
			if err := os.WriteFile(filepath.Join(dir, "eula.txt"),
				[]byte("# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
				return err
			}
			t.Logf("已写入 eula.txt")
		}
		applyImportLog4j(ctx, t, inst)
	}
	if err := m.completeCreate(ctx, t, inst, nil); err != nil {
		return err
	}

	t.Logf("服务端导入完成 ✔ 存档、模组和配置用的是压缩包里的那份，没有重新下载核心")
	return nil
}

// detectImportedServer 先用接管已有服务器的识别（看 jar、参数文件、日志）。
// 那套没认出来时，再用解析压缩包时看到的结果兜底。
func detectImportedServer(dir string, pack *modpack.Pack) (core mcsrc.Core, mc, build, launch string, javaMajor int, err error) {
	if sc, scanErr := scanServerDir(dir); scanErr == nil {
		if mcsrc.ValidCore(mcsrc.Core(sc.Core)) && sc.MC != "" && sc.LaunchTarget != "" {
			return mcsrc.Core(sc.Core), sc.MC, sc.Build, sc.LaunchTarget, sc.JavaMajor, nil
		}
		// 认出了启动文件但核心或版本是空的：解析阶段看的是同一个启动文件、而且认出了核心和版本，才拿它补上。
		// 两边说的不是同一个 jar 时不拼凑，不然核心、版本、启动文件可能出自两个 jar。
		if sc.LaunchTarget != "" && sc.LaunchTarget == pack.Launch && mcsrc.ValidCore(pack.Core) && pack.MC != "" {
			build = pack.Loader
			if sc.Build != "" {
				build = sc.Build
			}
			mc = pack.MC
			if sc.MC != "" {
				mc = sc.MC
			}
			return pack.Core, mc, build, sc.LaunchTarget, sc.JavaMajor, nil
		}
	}
	if mcsrc.ValidCore(pack.Core) && pack.MC != "" && pack.Launch != "" {
		if _, statErr := os.Stat(filepath.Join(dir, launchFile(pack.Launch))); statErr == nil {
			return pack.Core, pack.MC, pack.Loader, pack.Launch, 0, nil
		}
	}
	return "", "", "", "", 0, modpack.ServerUnknownError()
}

// applyServerProps 端口按导入页上填的改。压缩包里本来就有 server.properties 时，正版验证和允许飞行保留原样：
// 正版和离线下同一个名字的 UUID 不同，改了以后玩家的背包、进度、白名单、管理员都对不上；接管、导入交接包也都不动这两项。
// 压缩包里没有 server.properties 时，三项都按导入页写，离线的 1.19 起和新建一样关掉 enforce-secure-profile。
func applyServerProps(ctx context.Context, t *tasks.Task, dir string, req CreateReq, mc string) {
	path := filepath.Join(dir, "server.properties")
	_, statErr := os.Stat(path)
	had := statErr == nil
	props, err := LoadProps(path)
	if err != nil {
		t.Logf("没改 server.properties（%v）", err)
		return
	}
	props.Set("server-port", fmt.Sprintf("%d", req.Port))
	if !had {
		props.Set("online-mode", fmt.Sprintf("%t", req.OnlineMode))
		props.Set("allow-flight", fmt.Sprintf("%t", req.AllowFlight))
		if !req.OnlineMode && newerOrEqualLocalFirst(ctx, mc, "1.19") {
			props.Set("enforce-secure-profile", "false")
		}
	}
	if err := props.Save(path); err != nil {
		t.Logf("没改 server.properties（%v）", err)
		return
	}
	if had {
		online, _ := props.Get("online-mode")
		flight, _ := props.Get("allow-flight")
		t.Logf("端口已设为 %d。正版验证（%s）和允许飞行（%s）沿用压缩包里 server.properties 的设置，导入页上的这两项没有用上，要改到「常用设置」里改",
			req.Port, onOff(online, true), onOff(flight, false))
		return
	}
	t.Logf("压缩包里没有 server.properties，按导入页新建：端口 %d，正版验证%s，允许飞行%s",
		req.Port, onOff(fmt.Sprint(req.OnlineMode), false), onOff(fmt.Sprint(req.AllowFlight), false))
}

// applyImportLog4j 老版本的 log4shell 防护，和新建、接管的 applyLog4jMitigation 一样：1.18.1 以前加参数，
// 1.17 以前再下载官方的日志配置（压缩包里自带的那份是控制文件，不采用）。
// 版本新旧先只看本地清单：getManifest 冷启动时先联网刷新，网络不通能等上两分钟，文件和 Java 都已经就绪了，
// 不为这一步干等（Codex 复审提醒）。本地清单不认识这个版本时才联网问；官方清单也不认识的分不出新旧，不加（和接管一样）。
func applyImportLog4j(ctx context.Context, t *tasks.Task, inst *Instance) {
	newer1181, ok := mcsrc.NewerOrEqualCached(inst.MC, "1.18.1")
	if !ok {
		known, err := mcsrc.KnownVersion(ctx, inst.MC)
		if err != nil {
			t.Logf("没拿到 Minecraft 版本清单（%v），log4shell 防护先跳过", err)
			return
		}
		if known {
			if err := applyLog4jMitigation(ctx, t, inst); err != nil {
				t.Logf("没能判断要不要加 log4shell 防护（%v），先跳过", err)
			}
		}
		return
	}
	if newer1181 {
		return
	}
	inst.ExtraJVM = append(inst.ExtraJVM, log4jNoLookups)
	if newer117, _ := mcsrc.NewerOrEqualCached(inst.MC, "1.17"); newer117 {
		return
	}
	if err := fetchLog4jXML(ctx, inst.Dir); err != nil {
		t.Logf("log4j 防护配置下载失败（不影响开服，仅安全性降低）: %v", err)
		return
	}
	inst.ExtraJVM = append(inst.ExtraJVM, log4jConfig)
	t.Logf("已启用 log4shell 漏洞缓解（Mojang 官方方案）")
}

// newerOrEqualLocalFirst 版本 a 是否不老于 b：先只看本地清单，两个版本它都认识就不联网；不认识再问官方清单。
func newerOrEqualLocalFirst(ctx context.Context, a, b string) bool {
	if newer, ok := mcsrc.NewerOrEqualCached(a, b); ok {
		return newer
	}
	newer, err := mcsrc.NewerOrEqual(ctx, a, b)
	return err == nil && newer
}

// onOff 把 server.properties 里的布尔值说成开、关；没写时按游戏的默认值说。
func onOff(v string, def bool) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true":
		return "开"
	case "false":
		return "关"
	}
	if def {
		return "没写，默认开"
	}
	return "没写，默认关"
}

// javaFromCachedManifest 用本地已有的版本清单估 Java，不上网。清单里没有这个版本就返回 0。
func javaFromCachedManifest(mc string) int {
	if newer, ok := mcsrc.NewerOrEqualCached(mc, "26.1"); ok && newer {
		return 25
	}
	if newer, ok := mcsrc.NewerOrEqualCached(mc, "1.20.5"); ok && newer {
		return 21
	}
	if newer, ok := mcsrc.NewerOrEqualCached(mc, "1.18"); ok && newer {
		return 17
	}
	if newer, ok := mcsrc.NewerOrEqualCached(mc, "1.17"); ok && newer {
		return 16
	}
	if _, ok := mcsrc.NewerOrEqualCached(mc, "1.12.2"); ok {
		return 8
	}
	return 0
}
