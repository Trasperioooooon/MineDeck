package inst

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
			// 失败了就没有这台服务器，关于它的文件、参数、版本的结果不留（同导入整合包）
			for _, kind := range []string{"import-skipped", "import-case", "script-jvm", "mc-from-log"} {
				t.RemoveWarning(kind)
			}
			// 先清后报失败：任务一显示失败，半成品就得已经删干净了（失败框上写着可以重试；交接包导入同样的顺序）
			failCreate(t, err, m.discardCreating(req.Name, dir))
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
	if err := WriteCreatingMarker(dir, req.Name); err != nil {
		return err
	}
	where := "压缩包最外层"
	if pack.Root != "" {
		where = "「" + pack.Root + "」这一层"
	}
	t.Logf("按现成服务端解压（文件在%s）", where)
	res, err := extractServerFiles(ctx, t, pack, dir)
	if err != nil {
		return fmt.Errorf("解压服务端失败: %w", err)
	}
	ScrubControlFiles(dir)
	res.report(t)
	if name, ok := CheckLevelName(dir); !ok {
		return fmt.Errorf("服务端的 server.properties 把世界文件夹（level-name）指到了服务器文件夹外面：%s", name)
	}

	t.StartStep(1)
	srv, err := detectImportedServer(dir, pack)
	if err != nil {
		return err
	}
	core, mc, build, launch, major := srv.core, srv.mc, srv.build, srv.launch, srv.javaMajor
	if (core == mcsrc.CoreMohist || core == mcsrc.CoreBanner) && !isASCII(dir) {
		return fmt.Errorf("Mohist / Taiyitist（原 Banner）与非英文路径不兼容，请把存放文件夹设为纯英文路径（当前将创建于：%s）", dir)
	}
	kind := mcsrc.KindOf(core)
	t.Logf("按接管已有服务器的办法认出：%s %s，启动文件 %s", core, mc, strings.TrimPrefix(strings.TrimPrefix(launch, "jar:"), "args:"))
	if srv.mcFromLog {
		noteMCFromLog(t, mc)
	}

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
	applyScriptJVM(t, inst, pack)
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

	t.Logf("服务端导入完成 ✔ 世界、模组、插件和配置用的是压缩包里的那份，没有重新下载核心")
	return nil
}

// serverExtract 解压现成服务端的结果。名单只记前 maxWarnItems 个（告警最多列这么多），一共几个另外数。
type serverExtract struct {
	n        int            // 解出来的文件个数
	skipped  []string       // 名字在 Windows 上不能用、没有解压的
	nSkipped int            // 一共几个
	dups     []caseConflict // 和前面的文件只差大小写、没有解压的
	nDups    int            // 一共几组
}

// report 写进任务日志，跳过的和只差大小写的也写进任务结果（A46、A47）。
func (res serverExtract) report(t *tasks.Task) {
	t.Logf("解压了 %d 个文件", res.n)
	if res.nSkipped > 0 {
		t.Logf("这些文件的名字在 Windows 上不能用，没有解压：%s", shortNamesOf(res.skipped, res.nSkipped, 20))
		files := make([]skippedFile, 0, len(res.skipped))
		for _, p := range res.skipped {
			files = append(files, skippedFile{p, "名字在 Windows 上写不出来"})
		}
		addSkippedWarning(t, files, res.nSkipped)
	}
	if res.nDups > 0 {
		names := make([]string, 0, len(res.dups))
		for _, c := range res.dups {
			names = append(names, c.names[len(c.names)-1])
		}
		t.Logf("这些文件和压缩包里前面的文件只差大小写，在 Windows 上是同一个文件，没有解压（留的是前面那个）：%s", shortNamesOf(names, res.nDups, 20))
		addCaseWarning(t, res.dups, res.nDups)
	}
}

// extractServerFiles 把现成服务端压缩包解进 dir。整个解压拿着一份磁盘名额，开头按解开以后的总大小查余量（serverExtractLease）。
func extractServerFiles(ctx context.Context, t *tasks.Task, pack *modpack.Pack, dir string) (serverExtract, error) {
	var res serverExtract
	var lease *diskguard.Lease
	defer func() {
		if lease != nil {
			lease.Close()
		}
	}()
	cleanDest := filepath.Clean(dir)
	seen := map[string]string{} // 已经解压的文件：不分大小写的路径 → 包里的写法
	n, err := modpack.ExtractServer(pack, func(files []modpack.ServerFile) (err error) {
		lease, err = serverExtractLease(ctx, t, dir, files)
		return err
	}, func(rel string, r io.Reader) error {
		if err := lease.Check(0); err != nil { // 也查取消
			return err
		}
		// 和交接包一样：别人给的 instance.json、run.bat 不能落下来，不然下次启动就按包里写的去跑。
		// 名字在 Windows 上不能用的（CON、CON.txt、COM1.log 这类设备名，结尾是点或空格的）也和整合包覆盖目录一样跳过。
		if !overrideAllowed(rel) {
			if !IsControlFile(rel) {
				if res.nSkipped++; len(res.skipped) < maxWarnItems {
					res.skipped = append(res.skipped, rel)
				}
			}
			return nil
		}
		// 只差大小写的两个名字在 Windows 上是同一个文件，后一个会把前一个盖掉（v3.1.0 正式版前检查 C-2e）：
		// 和名字不能用的一样跳过、日志里列出来，留前一个。不整包拒绝：服务端包常在 Linux 上打，那边这是两个文件，
		// 拒了整包就导不进来了。写法一模一样出现两次的照旧以后一个为准
		k := winname.Fold(rel)
		if first, ok := seen[k]; ok && first != rel {
			if res.nDups++; len(res.dups) < maxWarnItems {
				res.dups = append(res.dups, caseConflict{names: []string{first, rel}, kept: first, why: "压缩包里只差大小写的只解压前面那个"})
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
	res.n = n
	return res, err
}

// serverImportDiskFree 查磁盘余量的函数；nil 用系统的。测试里换成余量不够、查不到的。
var serverImportDiskFree func(string) (int64, error)

// serverExtractLease 解压现成服务端之前按解开以后的总大小要磁盘余量（A40），做法同交接包导入（transfer 的 extractPrefix）：
// 总大小加上每个文件 4 KiB 的零头，装不下就在写第一个文件之前停下，免得把盘写满、连累同一块盘上别的服务器。
// 只算真要写的：控制文件（logs/ 这些）和名字写不出来的不落盘，不算（见 extractServerFiles 里的 overrideAllowed）。
// 查不到余量的盘照做（有的盘就是查不了），日志里说一声。写的过程中 diskguard 照常每隔一段再查一次。
func serverExtractLease(ctx context.Context, t *tasks.Task, dir string, files []modpack.ServerFile) (*diskguard.Lease, error) {
	query := serverImportDiskFree
	if query == nil {
		query = diskguard.Free
	}
	warned := false
	logged := func(p string) (int64, error) {
		n, err := query(p)
		if err != nil && !warned {
			warned = true
			t.Logf("没查到存放位置所在的盘还剩多少空间（%v），这一步检查跳过", err)
		}
		return n, err
	}
	// 文件数、总大小在 ExtractServer 里已经过了上限（20 万个、8 GB），加起来不会溢出
	var need int64
	for _, f := range files {
		if overrideAllowed(f.Rel) {
			need += f.Size + 4096
		}
	}
	return diskguard.Acquire(ctx, dir, diskguard.Options{Need: need, Query: logged, AllowUnknown: true})
}

// importedServer 解压以后认出来的服务端。
type importedServer struct {
	core      mcsrc.Core
	mc, build string
	launch    string
	javaMajor int
	mcFromLog bool // 版本号用的是解析压缩包时从 logs/latest.log 里认的（A44）
}

// detectImportedServer 先用接管已有服务器的识别（看 jar、参数文件；日志不解压，那套看不到）。
// 那套没认出来时，再用解析压缩包时看到的结果兜底。
func detectImportedServer(dir string, pack *modpack.Pack) (importedServer, error) {
	if sc, scanErr := scanServerDir(dir); scanErr == nil {
		if s, ok := importedFromScan(sc, pack); ok {
			return s, nil
		}
	}
	if mcsrc.ValidCore(pack.Core) && pack.MC != "" && pack.Launch != "" {
		if _, statErr := os.Stat(filepath.Join(dir, launchFile(pack.Launch))); statErr == nil {
			return importedServer{core: pack.Core, mc: pack.MC, build: pack.Loader, launch: pack.Launch, mcFromLog: pack.MCFromLog}, nil
		}
	}
	return importedServer{}, modpack.ServerUnknownError()
}

// importedFromScan 接管那套识别的结果 sc 认全了就用它。认出了启动文件、核心或版本却是空的：解析阶段看的是同一个启动文件、
// 而且认出了核心和版本，才拿解析的补上；两边说的不是同一个 jar 时不拼凑，不然核心、版本、启动文件可能出自两个 jar。
// 版本用的是 sc 认出来的（jar 里写着的），就不算从日志认的（A44）。
func importedFromScan(sc *AdoptScan, pack *modpack.Pack) (importedServer, bool) {
	if mcsrc.ValidCore(mcsrc.Core(sc.Core)) && sc.MC != "" && sc.LaunchTarget != "" {
		return importedServer{core: mcsrc.Core(sc.Core), mc: sc.MC, build: sc.Build, launch: sc.LaunchTarget, javaMajor: sc.JavaMajor}, true
	}
	if sc.LaunchTarget == "" || sc.LaunchTarget != pack.Launch || !mcsrc.ValidCore(pack.Core) || pack.MC == "" {
		return importedServer{}, false
	}
	s := importedServer{core: pack.Core, mc: pack.MC, build: pack.Loader, launch: sc.LaunchTarget, javaMajor: sc.JavaMajor, mcFromLog: pack.MCFromLog}
	if sc.Build != "" {
		s.build = sc.Build
	}
	if sc.MC != "" {
		s.mc, s.mcFromLog = sc.MC, false
	}
	return s, true
}

// noteMCFromLog 版本号是从压缩包里的日志认出来的（A44）：任务日志和结果里都说明，它是上一次开服时的版本，未必是现在的。
func noteMCFromLog(t *tasks.Task, mc string) {
	t.Logf("版本号 %s 是从压缩包里的 logs/latest.log 认出来的（服务端 jar 里没写）", mc)
	t.AddWarning(tasks.Warning{Kind: "mc-from-log", Title: "版本号是从日志里认出来的",
		Note: fmt.Sprintf("服务端 jar 里没写 Minecraft 版本，%s 是从压缩包里的 logs/latest.log 认出来的，是这台服务器上一次开服时的版本。"+
			"版本号决定用哪个 Java、要不要加 log4shell 防护；要是后来换过服务端，这里就不对了，可以先解压，用「新建服务器 → 接管已有服务器」自己填版本。", mc)})
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
	// 白名单和正版验证不一样：开关它不会让谁的存档对不上，导入页上又写明了，所以压缩包里写着什么都以导入页的开关为准（可用性走查 R02）。
	// 按服务端的读法写：同一项写了几行时服务端认最后一行
	wl := strconv.FormatBool(req.Whitelist)
	wlBefore, wlHad := javaPropsOf(path)["white-list"]
	wlNote := "白名单" + onOff(wl, false)
	if err := ForceProp(path, "white-list", wl); err != nil {
		t.Logf("没能把白名单开关写进 server.properties（%v）", err)
		wlNote = "白名单没能写进去，到「玩家」里看一眼"
	} else if wlHad && !strings.EqualFold(strings.TrimSpace(wlBefore), wl) {
		wlNote = fmt.Sprintf("白名单按导入页的开关从「%s」改成了「%s」", onOff(wlBefore, false), onOff(wl, false))
	}
	if had {
		online, _ := props.Get("online-mode")
		flight, _ := props.Get("allow-flight")
		t.Logf("端口已设为 %d，%s。正版验证（%s）和允许飞行（%s）沿用压缩包里 server.properties 的设置，导入页上的这两项没有用上，要改到「设置 → 常用」里改",
			req.Port, wlNote, onOff(online, true), onOff(flight, false))
		return
	}
	t.Logf("压缩包里没有 server.properties，按导入页新建：端口 %d，正版验证%s，允许飞行%s，%s",
		req.Port, onOff(fmt.Sprint(req.OnlineMode), false), onOff(fmt.Sprint(req.AllowFlight), false), wlNote)
}

// applyImportLog4j 老版本的 log4shell 防护，和新建、接管的 applyLog4jMitigation 一样按版本分段（见 log4jEras），
// 要官方日志配置的下载官方那份（压缩包里自带的是控制文件，不采用）。
// 版本新旧先只看本地清单：getManifest 冷启动时先联网刷新，网络不通能等上两分钟，文件和 Java 都已经就绪了，
// 不为这一步干等（Codex 复审提醒）。本地清单不认识这个版本时才联网问；官方清单也不认识的分不出新旧，不加（和接管一样）。
func applyImportLog4j(ctx context.Context, t *tasks.Task, inst *Instance) {
	noLookups, xml, ok := log4jPlanCached(inst.MC)
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
	addLog4jFlags(ctx, t, inst, noLookups, xml)
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
