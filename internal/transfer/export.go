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
	"velocity.toml", "config.yml", "forwarding.toml", "server-icon.png",
}

// slimDirs 瘦身模式整目录带上的（存在才带）。世界目录按实例的 level-name 单独找。
var slimDirs = []string{"mods", "plugins", "config", "defaultconfigs"}

// runExport 收集清单、打包、落到目标目录，返回交接包的最终路径。
func runExport(ctx context.Context, t *tasks.Task, i *inst.Instance, full bool, destDir string) (string, error) {
	mf := &Manifest{
		FormatVersion:   FormatVersion,
		Mode:            ModeHandover,
		MineDeckVersion: app.Version,
		ExportedAt:      time.Now(),
		Instance:        i.Settings,
		Core:            CoreRef{Core: i.Core, MC: i.MC, Build: i.Build, Kind: string(mcsrc.KindOf(i.Core))},
		Full:            full,
	}
	// Settings 是会动的外部结构：拿加锁的快照，导出过程中别的线程改动不进来
	mf.Instance.ExtraJVM = i.ExtraJVMSnapshot()
	mf.Instance.Policies = i.PoliciesSnapshot()

	// ---- 步骤 0：收集清单 ----
	t.StartStep(0)
	t.Logf("含世界、配置、模组与名单（logs、崩溃报告与锁定文件不进包）")
	if full {
		t.Logf("完整模式：核心启动文件一并带走")
	}
	var files []packFile
	var worldBytes int64
	add := func(rel, abs string, st os.FileInfo, mod *modinfo.Mod) {
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
			if st.IsDir() || st.Name() == "session.lock" {
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
	t.Logf("待打包 %d 个文件、%s", mf.Counts.Files, fmtBytes(mf.Counts.Bytes))

	// ---- 步骤 1：打包（先写缓存目录的 .part，成功再挪到目标目录；失败了清掉，随时可以重来） ----
	t.StartStep(1)
	name := ExportName(i.Name, time.Now())
	part := filepath.Join(app.CacheDir, "transfer", name+".part")
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

	// ---- 步骤 2：收尾，落到目标目录 ----
	t.StartStep(2)
	final := filepath.Join(destDir, name)
	if _, err := os.Stat(final); err == nil { // 同名（同一秒内重复导出）：加个时间尾
		final = filepath.Join(destDir, fmt.Sprintf("%s-%d.mdpack", strings.TrimSuffix(name, ".mdpack"), time.Now().Unix()))
	}
	if err := os.Rename(part, final); err != nil { // 跨盘改名不行，退回拷贝流
		if err := copyStream(part, final); err != nil {
			return "", err
		}
		os.Remove(part)
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", err
	}
	t.Logf("交接包大小：%s（含世界 %s）", fmtBytes(st.Size()), fmtBytes(mf.WorldBytes))
	t.Logf("把整个文件发给接手的人，对方在「新建服务器」页选「交接包」导入即可")
	return final, nil
}

// copyStream 流式拷贝（跨盘落位用），不整读进内存。
func copyStream(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
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
