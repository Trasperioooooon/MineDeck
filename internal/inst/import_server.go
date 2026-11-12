package inst

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
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
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		defer os.Remove(pack.ZipPath)
		if err := m.runServerImport(ctx, t, pack, req, dir); err != nil {
			t.Fail(err)
			_ = m.Delete(req.Name, false)
			os.RemoveAll(dir)
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
	n, err := modpack.ExtractServer(pack, func(rel string, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 和交接包一样：别人给的 instance.json、run.bat 不能落下来，不然下次启动就按包里写的去跑。
		if IsControlFile(rel) {
			return nil
		}
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
		_, cerr := io.Copy(w, r)
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
	if name, ok := CheckLevelName(dir); !ok {
		return fmt.Errorf("服务端的 server.properties 把世界目录（level-name）指到了服务器文件夹外面：%s", name)
	}

	t.StartStep(1)
	core, mc, build, launch, major, err := detectImportedServer(dir, pack)
	if err != nil {
		return err
	}
	if (core == mcsrc.CoreMohist || core == mcsrc.CoreBanner) && !isASCII(dir) {
		return fmt.Errorf("Mohist/Banner 与非英文路径不兼容，请把存放目录设为纯英文路径（当前将创建于：%s）", dir)
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
		applyServerProps(t, dir, req)
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
		applyCachedLog4j(t, inst)
	}
	if err := writeRunBat(inst); err != nil {
		return err
	}
	if err := m.saveInstance(inst); err != nil {
		return err
	}
	app.RememberRoot(filepath.Dir(dir))
	m.mu.Lock()
	m.insts[req.Name] = inst
	m.mu.Unlock()
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
		// 认出了启动文件但核心或版本是空的：解析阶段若已经有，就补上，启动文件仍用扫描的。
		if sc.LaunchTarget != "" && mcsrc.ValidCore(pack.Core) && pack.MC != "" {
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

// applyServerProps 端口、正版验证、允许飞行按导入页上填的改；server.properties 里的其他项（世界名、MOTD）留着。
func applyServerProps(t *tasks.Task, dir string, req CreateReq) {
	path := filepath.Join(dir, "server.properties")
	props, err := LoadProps(path)
	if err != nil {
		t.Logf("没改 server.properties（%v）", err)
		return
	}
	props.Set("server-port", fmt.Sprintf("%d", req.Port))
	props.Set("online-mode", fmt.Sprintf("%t", req.OnlineMode))
	props.Set("allow-flight", fmt.Sprintf("%t", req.AllowFlight))
	if err := props.Save(path); err != nil {
		t.Logf("没改 server.properties（%v）", err)
		return
	}
	t.Logf("端口已设为 %d（正版验证 %t，允许飞行 %t）；server.properties 里的其他项保留", req.Port, req.OnlineMode, req.AllowFlight)
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

// applyCachedLog4j 老版本补 log4shell 缓解。只看本地版本清单，不在导入这一步上网。
func applyCachedLog4j(t *tasks.Task, inst *Instance) {
	newer, ok := mcsrc.NewerOrEqualCached(inst.MC, "1.18.1")
	if !ok || newer {
		return
	}
	inst.ExtraJVM = append(inst.ExtraJVM, log4jNoLookups)
	if newer117, ok := mcsrc.NewerOrEqualCached(inst.MC, "1.17"); ok && !newer117 {
		t.Logf("这个版本需要 log4shell 防护配置。导入时没有下载它；保存一次设置，或重新导入并联网后会补上。")
	}
}
