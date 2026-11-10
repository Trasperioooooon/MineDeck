package transfer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Root  string // 自定义存放根（仅本机有效，远程 / 局域网请求被操作层清空）
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
	t.Logf("交接包：%s（%s %s，%s 导出）", mf.Instance.Name, mf.Core.Core, mf.Core.MC, mf.ExportedAt.Local().Format("2006-01-02 15:04"))
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
	switch {
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
	s := mf.Instance // A 的设置原样带来，下面逐项换成 B 这边的
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
	packBytes, err := extractPrefix(ctx, path, dir, t.ProgressFn("解包交接包"),
		func(n int, done, total int64) {
			t.ProgressFn(fmt.Sprintf("解包交接包（已 %d 个文件）", n))(done, total)
		})
	if err != nil {
		return err
	}
	t.Logf("解出 %s", fmtBytes(packBytes))

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
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return fmt.Errorf("包里缺启动文件 %s（A 那边的实例可能已经不完整）", rel)
			}
		}
	}
	s.Name = req.Name
	s.Core, s.MC, s.Build = mf.Core.Core, mf.Core.MC, build
	s.JavaMajor, s.JavaPath = major, javaPath
	if req.XmxMB > 0 {
		s.XmxMB = req.XmxMB
	}
	if s.XmxMB < 512 {
		s.XmxMB = 2048
	}
	s.XmsMB = min(1024, s.XmxMB)
	s.LaunchTarget = launch
	s.CreatedAt = time.Now()
	s.Adopted = false
	// 端口按 B 的填写改（服务器.properties 就在包里；代理端没有）
	if req.Port > 0 && req.Port <= 65535 && kind != mcsrc.KindProxy {
		props, err := inst.LoadProps(filepath.Join(dir, "server.properties"))
		if err == nil {
			if old, _ := props.Get("server-port"); old != fmt.Sprintf("%d", req.Port) {
				props.Set("server-port", fmt.Sprintf("%d", req.Port))
				if err := props.Save(filepath.Join(dir, "server.properties")); err != nil {
					return err
				}
				if old != "" {
					t.Logf("端口按你的填写改成了 %d（包里是 %s）", req.Port, old)
				}
			}
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
	if _, err := m.FinalizeCreate(s, dir); err != nil {
		return err
	}
	t.Logf("交接完成 ✔ 存档、配置和模组都是原来的")
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

// eulaAccepted eula.txt 的内容里是不是同意过。
func eulaAccepted(b []byte) bool {
	s := strings.ToLower(string(b))
	return strings.Contains(s, "eula=true") || strings.Contains(s, "eula = true")
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
