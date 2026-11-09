package inst

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"minedeck/internal/dl"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// ImportModpackAsync 从解析好的整合包创建实例（核心/版本/加载器由整合包决定）。
func (m *Manager) ImportModpackAsync(pack *modpack.Pack, req CreateReq, cfKey string) (string, error) {
	req.Core, req.MC, req.Build = pack.Core, pack.MC, pack.Loader
	dir, err := m.validateCreate(&req)
	if err != nil {
		return "", err
	}
	steps := append(append([]string{}, createSteps...), "下载整合包文件", "应用整合包覆盖")
	title := pack.Name
	if title == "" {
		title = req.Name
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("导入整合包 %s（%s %s）", title, req.Core, req.MC), steps)
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		if pack.ZipPath != "" {
			defer os.Remove(pack.ZipPath) // 导入结束（成功/失败）后删除缓存 zip 副本，避免每次导入累积 100-500MB
		}
		if err := m.runImport(ctx, t, pack, req, dir, cfKey); err != nil {
			t.Fail(err)
			_ = m.Delete(req.Name, false) // 从列表移除（可能已注册）
			os.RemoveAll(dir)
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

func (m *Manager) runImport(ctx context.Context, t *tasks.Task, pack *modpack.Pack, req CreateReq, dir, cfKey string) error {
	// 先走完整的核心创建流水线（步骤 0~5）
	if err := m.runCreate(ctx, t, req, dir); err != nil {
		return err
	}

	// ---- 步骤 6：下载整合包文件 ----
	t.StartStep(6)
	files := pack.Files
	if len(pack.CFRefs) > 0 {
		t.Logf("通过 CurseForge API 解析 %d 个模组引用...", len(pack.CFRefs))
		cf, unresolved, err := modpack.ResolveCF(ctx, pack.CFRefs, cfKey)
		if err != nil {
			t.Logf("⚠ CurseForge 解析受限: %v", err)
		}
		files = append(files, cf...)
		if len(unresolved) > 0 {
			writeUnresolvedReport(dir, unresolved)
			t.Logf("⚠ %d 个模组无法自动下载（未配 API Key 或作者禁止分发），清单已写入实例目录「未解析模组清单.txt」，需手动放入 mods 目录", len(unresolved))
			items := make([]tasks.WarnItem, 0, len(unresolved))
			for _, r := range unresolved {
				items = append(items, tasks.WarnItem{
					Name: fmt.Sprintf("CurseForge 项目 #%d（文件 %d）", r.ProjectID, r.FileID),
					URL:  fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/projects/%d", r.ProjectID),
				})
			}
			t.AddWarning(tasks.Warning{
				Kind:  "cf-unresolved",
				Title: fmt.Sprintf("%d 个模组需手动补装", len(unresolved)),
				Note:  "以下模组因作者禁止第三方分发或未配置 CurseForge API Key 而无法自动下载，请从链接手动下载对应文件后放入实例的 mods 目录（清单也已保存到实例目录「未解析模组清单.txt」）。",
				Items: items,
			})
		}
	}
	var total int64
	for _, f := range files {
		total = total + f.Size
	}
	prog := t.ProgressFn("下载整合包文件")
	var done atomic.Int64
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	// 首个错误用互斥保护而非 atomic.Value：并发存入异构 error 具体类型会令 atomic.Value panic（连带整个进程）
	var errMu sync.Mutex
	var firstErr error
	setErr := func(e error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = e
		}
		errMu.Unlock()
	}
	getErr := func() error { errMu.Lock(); defer errMu.Unlock(); return firstErr }
	for _, f := range files {
		if getErr() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f modpack.File) {
			defer wg.Done()
			defer func() { <-sem }()
			dest := filepath.Join(dir, filepath.FromSlash(f.Path))
			if err := dl.Fetch(ctx, dl.Request{URLs: f.URLs, Dest: dest, SHA1: f.SHA1}, nil); err != nil {
				setErr(fmt.Errorf("下载 %s 失败: %w", f.Path, err))
				return
			}
			prog(done.Add(f.Size), total)
		}(f)
	}
	wg.Wait()
	if e := getErr(); e != nil {
		return e
	}
	t.Logf("整合包文件下载完成（%d 个）", len(files))

	// ---- 步骤 7：应用覆盖目录 ----
	t.StartStep(7)
	err := modpack.ExtractOverrides(pack, dir, func(rel string, r io.Reader) error {
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		w, err := os.Create(dest)
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
		return fmt.Errorf("应用整合包覆盖文件失败: %w", err)
	}
	t.Logf("整合包导入完成 ✔（如需正版验证等设置请到「常用设置」调整）")
	return nil
}

func writeUnresolvedReport(dir string, refs []modpack.CFRef) {
	var sb strings.Builder
	sb.WriteString("以下模组无法自动下载，请从链接手动下载后放入 mods 目录：\r\n\r\n")
	for _, r := range refs {
		sb.WriteString(fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/projects/%d （文件 ID: %d）\r\n", r.ProjectID, r.FileID))
	}
	_ = os.WriteFile(filepath.Join(dir, "未解析模组清单.txt"), []byte(sb.String()), 0o644)
}
