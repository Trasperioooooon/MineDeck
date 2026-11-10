package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modinfo"
	"minedeck/internal/tasks"
)

// 交接包导出：把实例打包成 .mdpack，交给另一台电脑上的 MineDeck 续服。
// 默认「瘦身模式」不带核心 jar（导入时按清单走创建流水线重新下载），不含 logs / crash-reports；
// 「完整模式」（full=true）把启动文件和 libraries 也带上，导入就不用再下载。

var exportSteps = []string{"收集清单", "打包交接包", "收尾"}

// ExportReq 一次导出。
type ExportReq struct {
	Inst string // 实例名
	Full bool   // 完整模式：包含核心启动文件
	Dest string // 导出目录（用户选的）；空 = MineDeck 的缓存目录。非本机请求的这字段被操作层清空
}

// ExportName 生成交接包的文件名。
func ExportName(instance string, at time.Time) string {
	n := fileSafeName(instance)
	if n == "" {
		n = "server"
	}
	return fmt.Sprintf("%s-交接包-%s.mdpack", n, at.Format("20060102-150405"))
}

// ExportAsync 校验请求并在后台打包，返回任务编号。任务成功时 result 是交接包的完整路径。
func ExportAsync(m *inst.Manager, req ExportReq) (string, error) {
	i, err := m.Get(req.Inst)
	if err != nil {
		return "", err
	}
	if i.Status() != "stopped" {
		return "", fmt.Errorf("交接前请先停止服务器（运行中打包会不完整）")
	}
	dest := strings.TrimSpace(req.Dest)
	if dest != "" {
		dest = filepath.Clean(dest)
		if st, err := os.Stat(dest); err != nil || !st.IsDir() {
			return "", fmt.Errorf("导出目录不可用: %s", dest)
		}
		// 目录在本实例的文件夹里的话，下一次导出会把上次的包也打进去
		if rel, err := filepath.Rel(i.Dir, dest); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("导出目录不能放在这台服务器的文件夹里面，请另选一个位置")
		}
	} else {
		dest = filepath.Join(app.CacheDir, "transfer")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return "", fmt.Errorf("导出目录不可用: %w", err)
		}
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("导出交接包 %s（%s %s）", i.Name, i.Core, i.MC), exportSteps)
	t.SetKind("transfer-out")
	go func() {
		path, err := runExport(ctx, t, i, req.Full, dest)
		if err != nil {
			t.Fail(err)
			return
		}
		t.Logf("交接包已生成: %s", path)
		t.Finish(path)
	}()
	return t.ID(), nil
}

// slimFiles 瘦身模式要带上的单文件（存在才带）。代理端没有 server.properties，带上它们自带的配置。
var slimFiles = []string{
	"server.properties", "eula.txt",
	"whitelist.json", "ops.json", "banned-players.json", "banned-ips.json",
	"server-icon.png",
	// Bukkit 一系（Paper / Purpur / Leaves / Folia / Mohist / Banner）放在根目录的配置
	"bukkit.yml", "spigot.yml", "commands.yml", "permissions.yml", "help.yml", "wepif.yml",
	"paper.yml", "purpur.yml", "pufferfish.yml", "leaves.yml",
	// 代理端（Velocity / Waterfall）
	"velocity.toml", "forwarding.secret", "forwarding.toml",
	"config.yml", "waterfall.yml", "modules.yml", "locations.yml",
}

// slimDirs 瘦身模式整目录带上的（存在才带）。世界目录按实例的 level-name 单独找。
// kubejs、scripts（CraftTweaker）、local（FTB 系列的数据）是整合包服务器的一部分，少了配方和任务会变；
// mohist-config 是 Mohist 的配置，modules 是 Waterfall 的模块。
var slimDirs = []string{"mods", "plugins", "config", "defaultconfigs", "kubejs", "scripts", "local", "mohist-config", "modules"}

// regenerable 实例根目录下不用带、也不用提醒的东西：要么会重新生成或重新下载，要么是 MineDeck 自己按本机写的。
// 小写比较。logs、crash-reports、session.lock 在剔除清单里另有说明。
var regenerable = map[string]bool{
	"logs": true, "crash-reports": true, "debug": true, "cache": true, "libraries": true, "versions": true,
	".fabric": true, ".mixin.out": true, ".console_history": true,
	"run.bat": true, "run.sh": true, "instance.json": true, "user_jvm_args.txt": true,
	"usercache.json": true, "version_history.json": true, "session.lock": true,
	"未解析模组清单.txt": true, "导入待决清单.txt": true,
}

// leftOut 实例根目录下没进包、也说不上是可以重新生成的东西（目录名后面带 /）。
// 交接包按清单收文件，清单之外的东西不会自己跟过去：列出来写进任务日志和剔除清单，
// 交的人和接的人都看得见，要的话手动拷一份。
func leftOut(dir string, packed []packFile) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, f := range packed {
		have[strings.ToLower(firstSeg(f.rel))] = true
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		low := strings.ToLower(name)
		if have[low] || regenerable[low] {
			continue
		}
		if !e.IsDir() {
			// 核心 jar 瘦身模式不带（导入时重新下载）；安装器日志这类也不提
			if ext := filepath.Ext(low); ext == ".jar" || ext == ".log" || ext == ".lck" {
				continue
			}
		} else {
			name += "/"
		}
		out = append(out, name)
	}
	return out
}

// runExport 收集清单、打包、落到目标目录，返回交接包的最终路径。
func runExport(ctx context.Context, t *tasks.Task, i *inst.Instance, full bool, destDir string) (string, error) {
	// 交接包要完整一致：打包期间服务器又开起来的话，世界边写边压会不完整
	if i.Status() != "stopped" {
		return "", fmt.Errorf("交接前请先停止服务器（运行中打包会不完整）")
	}
	mf := &Manifest{
		FormatVersion:   FormatVersion,
		Mode:            ModeHandover,
		MineDeckVersion: app.Version,
		ExportedAt:      time.Now(),
		Instance:        i.Settings,
		Core:            CoreRef{Core: i.Core, MC: i.MC, Build: i.Build, Kind: string(mcsrc.KindOf(i.Core))},
		Full:            full,
	}
	// Settings 是会动的外部结构：会变的字段拿加锁的快照逐项盖回来（UpdateSettings / SetJava 在锁内写）
	mf.Instance.ExtraJVM = i.ExtraJVMSnapshot()
	mf.Instance.Policies = i.PoliciesSnapshot()
	mf.Instance.XmxMB, mf.Instance.XmsMB, mf.Instance.ConsoleEncoding = i.MemSnapshot()
	mf.Instance.JavaMajor, _ = i.JavaSnapshot()
	// java.exe 的完整路径不进清单：那是这台电脑上的位置（常带着 Windows 用户名），对接手的人没用，
	// 导入时按 javaMajor 在对方电脑上重新找或下载。
	mf.Instance.JavaPath = ""

	// ---- 步骤 0：收集清单 ----
	t.StartStep(0)
	t.Logf("含世界、配置、模组与名单（logs、崩溃报告与锁定文件不进包）")
	if full {
		t.Logf("完整模式：核心启动文件一并带走")
	}
	var files []packFile
	var worldBytes int64
	add := func(rel, abs string, st os.FileInfo, mod *modinfo.Mod) {
		// 同一个文件不重复打（完整模式 LaunchTarget 与 server.jar 常是同一个）
		for _, f := range files {
			if f.rel == rel {
				return
			}
		}
		files = append(files, packFile{rel: rel, abs: abs, size: st.Size(), mod: mod})
		mf.Counts.Files++
		mf.Counts.Bytes += st.Size()
	}
	walk := func(rootRel string, into *int64, meta map[string]modinfo.Mod) error {
		root := filepath.Join(i.Dir, rootRel)
		return filepath.Walk(root, func(p string, st os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) && p == root {
					return nil // 这个目录本来就没有
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// 只打普通文件：Walk 不跟符号链接，这里再把目录、链接和特殊文件一并排掉，
			// 免得实例目录里植根链接把别的位置吃进包
			if st.IsDir() || !st.Mode().IsRegular() || st.Name() == "session.lock" {
				return nil
			}
			rel, err := filepath.Rel(i.Dir, p)
			if err != nil {
				return err
			}
			var mod *modinfo.Mod
			if meta != nil {
				if mm, ok := meta[st.Name()]; ok {
					mod = &mm
				}
			}
			if into != nil {
				*into += st.Size()
			}
			add(rel, p, st, mod)
			return nil
		})
	}
	for _, w := range i.WorldDirs() {
		if err := walk(w, &worldBytes, nil); err != nil {
			return "", fmt.Errorf("清点世界目录失败: %w", err)
		}
	}
	// level-name 之外的世界（Multiverse 这类多世界插件建的大厅、资源世界）：根目录下带 level.dat 的文件夹都是世界
	if ents, err := os.ReadDir(i.Dir); err == nil {
		known := map[string]bool{}
		for _, w := range i.WorldDirs() {
			known[strings.ToLower(filepath.Clean(w))] = true
		}
		for _, e := range ents {
			if !e.IsDir() || known[strings.ToLower(e.Name())] {
				continue
			}
			if _, err := os.Stat(filepath.Join(i.Dir, e.Name(), "level.dat")); err != nil {
				continue
			}
			if err := walk(e.Name(), &worldBytes, nil); err != nil {
				return "", fmt.Errorf("清点世界目录失败: %w", err)
			}
			t.Logf("另一个世界 %s 也一起带上", e.Name())
		}
	}
	mf.WorldBytes = worldBytes

	// mods / plugins 的 modinfo 元数据（清单的 id / 名称 / 版本 / 加载器）
	modMeta := map[string]map[string]modinfo.Mod{}
	for _, d := range []string{"mods", "plugins"} {
		mods, err := modinfo.ReadDir(filepath.Join(i.Dir, d))
		if err != nil {
			continue
		}
		if len(mods) == 0 {
			continue
		}
		m := map[string]modinfo.Mod{}
		for _, mm := range mods {
			m[mm.File] = mm
		}
		modMeta[d] = m
		if d == "mods" {
			t.Logf("模组 %d 个", len(mods))
		} else {
			t.Logf("插件 %d 个", len(mods))
		}
	}
	for _, d := range slimDirs {
		if err := walk(d, nil, modMeta[d]); err != nil {
			return "", err
		}
	}
	for _, f := range slimFiles {
		p := filepath.Join(i.Dir, f)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			add(f, p, st, nil)
		}
	}
	if full {
		// 启动目标 jar（jar:xxx 这类根目录文件）以及老 Forge 的预置原版 jar
		var carried []string
		if rest, ok := strings.CutPrefix(i.LaunchTarget, "jar:"); ok {
			carried = append(carried, rest)
		}
		carried = append(carried, "server.jar")
		for _, rel := range carried {
			p := filepath.Join(i.Dir, filepath.FromSlash(rel))
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				add(filepath.ToSlash(rel), p, st, nil)
			}
		}
		ms, _ := filepath.Glob(filepath.Join(i.Dir, "minecraft_server*.jar"))
		for _, p := range ms {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				if r, err := filepath.Rel(i.Dir, p); err == nil {
					add(r, p, st, nil)
				}
			}
		}
		if err := walk("libraries", nil, nil); err != nil {
			return "", err
		}
		if st, err := os.Stat(filepath.Join(i.Dir, "fabric-server-launcher.properties")); err == nil && !st.IsDir() {
			add("fabric-server-launcher.properties", filepath.Join(i.Dir, "fabric-server-launcher.properties"), st, nil)
		}
	}
	// 剔除清单（导入向导给接手的人看）
	mf.Excluded = []string{
		"logs（日志可能含玩家 IP，不进包）",
		"crash-reports（崩溃报告）",
		"session.lock（世界锁定文件）",
	}
	if !full {
		mf.Excluded = append(mf.Excluded, "服务端核心（导入时自动重新下载）")
	}
	if left := leftOut(i.Dir, files); len(left) > 0 {
		shown := left
		if len(shown) > 30 {
			shown = append(append([]string{}, left[:30]...), fmt.Sprintf("…等 %d 项", len(left)))
		}
		list := strings.Join(shown, "、")
		mf.Excluded = append(mf.Excluded, "服务器文件夹里的这些不在交接范围内，没有进包："+list)
		t.Logf("⚠ 这些没有进包（交接包只带世界、配置、模组和名单），对方需要的话请另外拷给他：%s", list)
	}
	t.Logf("待打包 %d 个文件、%s", mf.Counts.Files, fmtBytes(mf.Counts.Bytes))

	// ---- 步骤 1：打包（先写缓存目录的 .part，成功再挪到目标目录；失败了清掉，随时可以重来） ----
	t.StartStep(1)
	name := ExportName(i.Name, time.Now())
	// .part 带任务号：同一秒里并发导出同名的两个实例也不会写进同一个文件
	part := filepath.Join(app.CacheDir, "transfer", fmt.Sprintf("%s-%s.part", name, t.ID()))
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return "", err
	}
	defer os.Remove(part)
	// 进度两维：字节为主（progress 每个文件都报），文件数隔一批换一次 label
	if err := writePack(ctx, part, files, mf, t.ProgressFn("打包交接包"),
		func(n int, done, total int64) {
			t.ProgressFn(fmt.Sprintf("打包交接包（已 %d 个文件）", n))(done, total)
		}); err != nil {
		return "", err
	}

	// ---- 步骤 2：收尾，落到目标目录（目标已有同名就不覆盖，换一个名字） ----
	t.StartStep(2)
	final := filepath.Join(destDir, name)
	if err := placeFile(part, final); err != nil {
		if !os.IsExist(err) {
			return "", err
		}
		final = filepath.Join(destDir, fmt.Sprintf("%s-%d.mdpack", strings.TrimSuffix(name, ".mdpack"), time.Now().UnixNano()))
		if err := placeFile(part, final); err != nil {
			return "", err
		}
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", err
	}
	t.Logf("交接包大小：%s（含世界 %s）", fmtBytes(st.Size()), fmtBytes(mf.WorldBytes))
	t.Logf("把整个文件发给接手的人，对方在「新建服务器」页选「交接包」导入即可")
	return final, nil
}

// placeFile 把打包好的 .part 挪到 final（不覆盖已有文件）：同目录改名；跨盘退回
// 独占创建的流式拷贝（目标已存在报 os.IsExist，由调用方换名重试）。
func placeFile(part, final string) error {
	if err := os.Rename(part, final); err == nil {
		return nil
	}
	in, err := os.Open(part)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(final, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if werr := out.Close(); cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		os.Remove(final) // 拷一半的不能留
		return cerr
	}
	return nil
}

// firstSeg 相对路径的第一段目录名（斜杠或反斜杠）。
func firstSeg(rel string) string {
	s := strings.ReplaceAll(rel, "\\", "/")
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

// fmtBytes 人类可读的文件大小（任务日志用，和前端 fmtBytes 一套口径）。
func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
