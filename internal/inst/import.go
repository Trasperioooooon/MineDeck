package inst

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/modinfo"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// modpackHTTP 造导入下载用的 HTTP 客户端：受控客户端（dl.GuardedClient）——拨号级与
// 重定向级都查公网和允许列表，用户确认过的站点日后 DNS 被解析到内网也连不出去。
// 测试里换成带假 RoundTripper 的版本，不真正联网。
var modpackHTTP = func() *http.Client {
	trusted := app.TrustedHostFunc(app.TrustScopeModpack)
	return dl.GuardedClient(func(host string) bool { return modpack.IsAllowedHost(host, trusted) })
}

// ImportModpackAsync 从解析好的整合包创建实例（核心/版本/加载器由整合包决定）。
func (m *Manager) ImportModpackAsync(pack *modpack.Pack, req CreateReq, cfKey string) (string, error) {
	req.Core, req.MC, req.Build = pack.Core, pack.MC, pack.Loader
	// 之前逐个确认过的自定义站点直接用（记忆存 app.Config.TrustedHosts，作用域 modpack）：
	// 这次导入再遇到同站点的待决文件就不再列为待决、直接下载。
	pack.Reclassify(app.TrustedHostFunc(app.TrustScopeModpack))
	if src := strings.TrimSpace(req.LocalSource); src != "" {
		if st, err := os.Stat(src); err != nil || !st.IsDir() {
			return "", fmt.Errorf("本地来源目录不可用（待决文件会从这里按原样保留）: %s", src)
		}
	}
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

// pendingJob 收尾前还在攒的待决文件（并发下载里也会产生：哈希校验不符）。
type pendingJob struct {
	file   modpack.PendingFile
	origin string // 待决来源：解析清单 | 哈希校验 | CurseForge
	note   string // 给用户看的一句话（没写用 modpack.PendingNote 按原因给）
}

// importJob 一次导入里对待决文件的记账（下载协程并发往里加，收尾前一次性取走）。
type importJob struct {
	mu      sync.Mutex
	pending []pendingJob
}

func (j *importJob) add(p pendingJob) {
	j.mu.Lock()
	j.pending = append(j.pending, p)
	j.mu.Unlock()
}

func (j *importJob) take() []pendingJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.pending
	j.pending = nil
	return out
}

// keptFile 一个「按原样保留」的文件：没有走线上，从整合包覆盖目录或本地来源目录拿的现成文件。
type keptFile struct {
	path   string // 相对实例目录
	source string // 从哪保留的：整合包覆盖目录 | 本地来源目录
}

// runImport 和之前一样先建好服务器本体，再下载整合包文件、应用覆盖目录；
// 区别在「无法自动解决」的文件不再让整个导入失败：解析期白名单外 / 缺哈希、
// 下载后哈希校验不符、CurseForge 未解析的文件逐项进待决清单；整合包覆盖目录或
// 本地来源目录里已有的按原样保留（魔改模组因此不再要求线上匹配）；剩下的交
// 处置面板（任务面板 / POST /api/import/pending/{任务}）由用户逐个决定。
func (m *Manager) runImport(ctx context.Context, t *tasks.Task, pack *modpack.Pack, req CreateReq, dir, cfKey string) error {
	// 先走完整的核心创建流水线（步骤 0~5）
	if err := m.runCreate(ctx, t, req, dir); err != nil {
		return err
	}
	job := &importJob{}

	// ---- 步骤 6：下载整合包文件 ----
	t.StartStep(6)
	files := pack.Files
	for _, pf := range pack.Pending {
		job.add(pendingJob{file: pf, origin: "解析清单"})
	}
	if len(pack.CFRefs) > 0 {
		t.Logf("通过 CurseForge API 解析 %d 个模组引用...", len(pack.CFRefs))
		cf, unresolved, err := modpack.ResolveCF(ctx, pack.CFRefs, cfKey)
		if err != nil {
			t.Logf("⚠ CurseForge 解析受限: %v", err)
		}
		files = append(files, cf...)
		if len(unresolved) > 0 {
			writeUnresolvedReport(dir, unresolved)
			t.Logf("⚠ %d 个模组无法自动下载（未配 API Key 或作者禁止分发），已列入待决清单，可在任务面板逐个处理", len(unresolved))
			items := make([]tasks.WarnItem, 0, len(unresolved))
			for _, r := range unresolved {
				job.add(pendingJob{
					file: modpack.PendingFile{
						Path:   fmt.Sprintf("mods/（CurseForge 项目 %d，文件 %d）", r.ProjectID, r.FileID),
						Reason: "cf",
					},
					origin: "CurseForge",
					note:   "CurseForge 上解析不到这个模组（未配 API Key 或作者禁止第三方分发），不能自动下载",
				})
				items = append(items, tasks.WarnItem{
					Name: fmt.Sprintf("CurseForge 项目 #%d（文件 %d）", r.ProjectID, r.FileID),
					URL:  fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/projects/%d", r.ProjectID),
				})
			}
			t.AddWarning(tasks.Warning{
				Kind:  "cf-unresolved",
				Title: fmt.Sprintf("%d 个模组需手动补装", len(unresolved)),
				Note:  "以下模组因作者禁止第三方分发或未配置 CurseForge API Key 而无法自动下载：可从链接手动下载后放入实例的 mods 目录，或在下面的「待决文件」里逐个处理（清单也已保存到实例目录「未解析模组清单.txt」）。",
				Items: items,
			})
		}
	}
	var total int64
	for _, f := range files {
		total = total + f.Size
	}
	client := modpackHTTP()
	prog := t.ProgressFn("下载整合包文件")
	var done atomic.Int64
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	// 首个错误用互斥保护而非 atomic.Value：并发存入异构 error 具体类型会令 atomic.Value panic（连带整个进程）
	var errMu sync.Mutex
	var firstErr error
	// 哈希校验不符（dl.ErrVerify）重试也不会好：转成待决，由用户决定保留本地文件 / 手动指定 / 跳过。
	// 下载不下来（网络问题）仍算整次失败，和之前一样：重试整次导入很快（已下载的文件有缓存）。
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
			if err := dl.Fetch(ctx, dl.Request{URLs: f.URLs, Dest: dest, SHA1: f.SHA1, Client: client}, nil); err != nil {
				if errors.Is(err, dl.ErrVerify) {
					_ = os.Remove(dest + ".part")
					job.add(pendingJob{
						file:   modpack.PendingFile{Path: f.Path, SHA1: f.SHA1, Size: f.Size, Reason: "hash"},
						origin: "哈希校验",
						note:   "下载下来的内容对不上整合包给的校验值，没有采用",
					})
					prog(done.Add(f.Size), total)
					return
				}
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

	// ---- 本地继承：待决文件凡本地已有就算解决 ----
	// 整合包覆盖目录里正好有待决文件（用户把魔改模组随包带上的情形）→ 按原样保留；
	// 再看本地来源目录（用户手里还留着的老服务器目录）：相对路径或文件名对得上的按原样复制。
	pending := job.take()
	var kept []keptFile
	var remain []modpack.PendingFile
	for _, p := range pending {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p.file.Path))); err == nil {
			kept = append(kept, keptFile{path: p.file.Path, source: "整合包覆盖目录"})
			continue
		}
		if src := findLocalFile(req.LocalSource, p.file.Path); src != "" {
			if err := copyLocalFile(src, filepath.Join(dir, filepath.FromSlash(p.file.Path))); err != nil {
				t.Logf("⚠ 本地继承 %s 失败: %v", p.file.Path, err)
				remain = append(remain, p.file)
				continue
			}
			kept = append(kept, keptFile{path: p.file.Path, source: "本地来源目录"})
			continue
		}
		remain = append(remain, p.file)
	}
	m.finishImport(t, req, dir, remain, kept)
	return nil
}

// finishImport 收尾：按原样保留的、待决的、Java 要求的提示与记录文件。
func (m *Manager) finishImport(t *tasks.Task, req CreateReq, dir string, pending []modpack.PendingFile, kept []keptFile) {
	if len(kept) > 0 {
		items := make([]tasks.WarnItem, 0, len(kept))
		for _, k := range kept {
			items = append(items, tasks.WarnItem{Name: fmt.Sprintf("%s（来自%s，未校验）", k.path, k.source)})
		}
		t.AddWarning(tasks.Warning{
			Kind:  "import-kept",
			Title: fmt.Sprintf("按原样保留 %d 个文件", len(kept)),
			Note:  "这些文件没有从网上下载，用的是整合包覆盖目录或本地来源目录里已有的那份，内容没有校验（魔改的模组因此能原样保留）。还有没法自动下载的文件在待决清单里。",
			Items: items,
		})
		t.Logf("✔ 按原样保留 %d 个文件（整合包覆盖目录 / 本地来源目录）", len(kept))
	}
	items := make([]tasks.PendingItem, 0, len(pending))
	for _, pf := range pending {
		pi := tasks.PendingItem{
			Path:    pf.Path,
			Reason:  pf.Reason,
			Note:    modpack.PendingNote(pf.Reason),
			Size:    pf.Size,
			Options: []string{"skip", "local"},
		}
		if pf.Reason == "cf" {
			pi.Note = "CurseForge 上解析不到（未配 API Key 或作者禁止分发），保留本地已有文件或手动放一份"
		}
		if pf.Reason == "hash" {
			pi.Note = "下载的内容对不上整合包给的校验值，没有采用；可以保留本地已有文件或手动指定一份"
		}
		if pf.Reason == modpack.ReasonHost {
			pi.Options = []string{"local", "skip", "trust"}
			pi.Host = strings.Join(modpack.HostOf(pf.URLs), " ")
			pi.URLs = pf.URLs // 信任站点后从这些候选里下载
			pi.SHA1 = pf.SHA1 // 下载必须过这道 SHA-1 校验
		}
		items = append(items, pi)
	}
	if len(items) > 0 {
		for i := range items {
			items[i].ID = fmt.Sprintf("p%d", i+1)
		}
		t.SetPending(items)
		m.rememberPending(t.ID(), req.Name)
		t.AddWarning(tasks.Warning{
			Kind:  "import-pending",
			Title: fmt.Sprintf("%d 个文件待处理", len(items)),
			Note:  "这些文件没法自动下载。逐个处理导入才算补完整：保留本地已有的文件、跳过（记录进结果摘要），或（地址在自定义站点的）确认站点后下载。",
		})
		writePendingReport(dir, t.PendingItems())
	}
	// Java 要求：魔改模组常声明比实例新的 Java（1.20.1 实例是 17，模组可能要 25）。
	// 只提示不阻断：1.20.1 + Java 25 的组合允许存在，到「内存 / 启动」换。
	javaMajor := 0
	if i, err := m.Get(req.Name); err == nil {
		javaMajor = i.JavaMajor
	}
	if need, mods := javaRequiredBy(dir, javaMajor); need > javaMajor && javaMajor > 0 {
		warn := make([]tasks.WarnItem, 0, len(mods))
		for _, x := range mods {
			warn = append(warn, tasks.WarnItem{Name: x})
		}
		t.AddWarning(tasks.Warning{
			Kind:  "java-req",
			Title: fmt.Sprintf("有模组要求 Java %d，这台实例现在是 Java %d", need, javaMajor),
			Note:  "这只是提醒，不影响导入结果。装着这样的模组启动可能失败或部分功能不工作；要换的话到「内存 / 启动」里选（MineDeck 可以自动下载便携版），也可以先保持现状。",
			Items: warn,
		})
	}
	keptLine := ""
	if len(kept) > 0 {
		keptLine = fmt.Sprintf("，按原样保留 %d 个", len(kept))
	}
	t.Logf("整合包导入完成 ✔（%d 个文件待处理%s；如需正版验证等设置请到「常用设置」调整）", len(items), keptLine)
}

// findLocalFile 在本地来源目录里找整合包清单说的这个文件：先按相对路径找，
// 再按文件名找（mods/xxx.jar 的名字在来源目录的 mods、plugins 或根目录下）。找不到返回空串。
func findLocalFile(source, relPath string) string {
	source = strings.TrimSpace(source)
	if source == "" || relPath == "" {
		return ""
	}
	if st, err := os.Stat(source); err != nil || !st.IsDir() {
		return ""
	}
	name := path.Base(filepath.ToSlash(relPath))
	for _, cand := range []string{
		filepath.Join(source, filepath.FromSlash(relPath)), // 相对路径一致（mods/xxx.jar）
		filepath.Join(source, "mods", name),                // 魔改 jar 最常见的摆法：只对得上名字
		filepath.Join(source, "plugins", name),
		filepath.Join(source, name),
	} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

// copyLocalFile 把本地文件复制进实例目录（按原样保留：内容不校验——用户自己的文件自己负责）。
func copyLocalFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if werr := out.Close(); cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	_ = os.Remove(dest)
	return os.Rename(tmp, dest)
}

// javaRequiredBy 扫实例目录里声明的 Java 要求，返回要求的最高大版本和不达标的模组说明。
// mods 与 plugins 两个目录都看（不同核心放的位置不同）。
func javaRequiredBy(dir string, javaMajor int) (int, []string) {
	best := 0
	var mods []string
	for _, sub := range []string{"mods", "plugins"} {
		list, err := modinfo.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, mi := range list {
			need := modinfo.JavaMajorOf(mi)
			if need <= javaMajor {
				continue
			}
			if mi.ID != "" {
				mods = append(mods, fmt.Sprintf("%s（%s，要 Java %d）", mi.File, mi.ID, need))
			} else {
				mods = append(mods, fmt.Sprintf("%s（要 Java %d）", mi.File, need))
			}
			if need > best {
				best = need
			}
		}
	}
	return best, mods
}

// writePendingReport 把待决清单也写进实例目录（面板之外留一份记录，和「未解析模组清单.txt」同理）。
func writePendingReport(dir string, items []tasks.PendingItem) {
	if len(items) == 0 {
		_ = os.Remove(filepath.Join(dir, "导入待决清单.txt"))
		return
	}
	var sb strings.Builder
	sb.WriteString("以下文件没法自动下载，已在任务面板逐项列出，可逐个处理：\r\n\r\n")
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("%s（%s）\r\n", it.Path, it.Note))
		for _, u := range it.URLs {
			sb.WriteString("  " + u + "\r\n")
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "导入待决清单.txt"), []byte(sb.String()), 0o644)
}

// PendingAction 一次处置动作：act 取 skip（跳过）、local（保留本地已有文件或手动指定的本地文件）、
// trust（确认信任自定义站点后下载）。
type PendingAction struct {
	ID   string `json:"id"`
	Act  string `json:"act"`
	Path string `json:"path,omitempty"` // act=local：本地文件路径，或本地目录（按文件名匹配）
	URL  string `json:"url,omitempty"`  // act=trust：从哪条候选地址下载（要和待决记录里列出的一致）
	Ack  bool   `json:"ack,omitempty"`  // act=trust：用户读过风险说明，确认信任这个站点
}

// ResolveOutcome 一项处置的结果（ok=false 的项留在待决清单里，note 写明原因）。
type ResolveOutcome struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Act  string `json:"act"`
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

// ResolveImportPending 处置整合包导入任务的待决文件。逐项执行后成功的那几项移出待决清单；
// 失败的（源文件不存在、下载校验不符、没确认信任）留在清单里等下次处理。整批不因单项失败而中断。
// 权限约束在操作层（instance.import.pending：仅人工 + 仅本机）。
func (m *Manager) ResolveImportPending(ctx context.Context, taskID string, acts []PendingAction) ([]ResolveOutcome, []tasks.PendingItem, error) {
	if len(acts) == 0 {
		return nil, nil, fmt.Errorf("没有要处理的项")
	}
	t := m.Tasks.Get(taskID)
	if t == nil {
		return nil, nil, fmt.Errorf("没有这个任务: %s", taskID)
	}
	inst, err := m.pendingInstance(t.ID())
	if err != nil {
		return nil, nil, err
	}
	pending := t.PendingItems()
	byID := map[string]tasks.PendingItem{}
	for _, it := range pending {
		byID[it.ID] = it
	}
	var out []ResolveOutcome
	var doneIDs []string
	for _, act := range acts {
		oc := ResolveOutcome{ID: act.ID, Act: act.Act}
		it, ok := byID[act.ID]
		if !ok {
			oc.Note = "没有这一项（可能刚处理过）"
			out = append(out, oc)
			continue
		}
		oc.Path = it.Path
		switch act.Act {
		case "skip":
			oc.OK = true
			oc.Note = "已跳过，已记录进任务日志"
			t.Logf("待决文件 %s：用户选择跳过", it.Path)
		case "local":
			src, note := resolveLocalSource(act.Path, it.Path)
			if src == "" {
				oc.Note = note
				break
			}
			// CurseForge 的项只知道项目号、不知道文件名：保留哪个文件就叫它本来的名字
			destRel := it.Path
			if it.Reason == "cf" {
				destRel = "mods/" + filepath.Base(src)
			}
			dest := filepath.Join(inst.Dir, filepath.FromSlash(destRel))
			if err := copyLocalFile(src, dest); err != nil {
				oc.Note = fmt.Sprintf("复制失败: %v", err)
				break
			}
			oc.OK = true
			oc.Note = fmt.Sprintf("已用本地文件原样代替（%s）", src)
			t.Logf("待决文件 %s：按本地文件原样保留（%s）", it.Path, src)
		case "trust":
			ok, note := m.pendingTrustDownload(ctx, inst, it, act.URL, act.Ack)
			oc.OK, oc.Note = ok, note
			if ok {
				t.Logf("待决文件 %s：确认站点后下载完成（校验通过）", it.Path)
			}
		default:
			oc.Note = "不认识的处理方式（可用：skip、local、trust）"
		}
		if oc.OK {
			doneIDs = append(doneIDs, it.ID)
		}
		out = append(out, oc)
	}
	if len(doneIDs) > 0 {
		t.ResolvePending(doneIDs)
		writePendingReport(inst.Dir, t.PendingItems())
		if len(t.PendingItems()) == 0 {
			m.forgetPending(t.ID())
			t.Logf("待决清单已全部处理完")
		}
	}
	return out, t.PendingItems(), nil
}

// resolveLocalSource 解析 act=local 的源：直接给文件就用；给目录就在里面按待决路径找；都不行说明原因。
func resolveLocalSource(value, relPath string) (src, why string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "给出本地文件或目录的路径"
	}
	st, err := os.Stat(value)
	if err != nil {
		return "", "文件或目录不存在（给完整路径，如 D:\\old\\mods\\xxx.jar）"
	}
	if st.IsDir() {
		if found := findLocalFile(value, relPath); found != "" {
			return found, ""
		}
		return "", "这个目录里没有对得上的文件（按相对路径或 mods / plugins 下的同名文件找）"
	}
	return value, ""
}

// pendingTrustDownload 信任自定义站点后下载一个待决文件。安全边界与原下载链路一致，不作任何放宽：
// 地址必须 https 且在待决记录列出的候选里、主机必须公网（授予信任时和这次下载各查一遍，防 DNS 变动）、
// 下载内容必须过整合包声明的 SHA-1 校验；任何一关不过都不下载。
func (m *Manager) pendingTrustDownload(ctx context.Context, inst *Instance, it tasks.PendingItem, rawURL string, ack bool) (bool, string) {
	if it.Reason != modpack.ReasonHost {
		return false, "这一项没有可确认下载的自定义站点"
	}
	rawURL = strings.TrimSpace(rawURL)
	match := ""
	for _, u := range it.URLs {
		if strings.TrimSpace(u) == rawURL {
			match = rawURL
			break
		}
	}
	if match == "" {
		return false, "地址要选这一项列出的候选地址"
	}
	host, _ := modpack.AllowedURL(match, nil)
	if host == "" {
		return false, "地址不是 https，任何情况下都不下载"
	}
	if !modpack.HostIsPublic(host) {
		return false, fmt.Sprintf("站点 %s 解析到内网或本机地址，不下载", host)
	}
	if !app.TrustedHostFunc(app.TrustScopeModpack)(host) {
		if !ack {
			return false, fmt.Sprintf("站点 %s 没有确认过：读一遍风险说明并确认后才下载", host)
		}
		app.TrustHost(host, app.TrustScopeModpack)
	}
	dest := filepath.Join(inst.Dir, filepath.FromSlash(it.Path))
	// 受控客户端：即使 grant 时查过公网，连接级（DNS 变动、重定向到内网）也再拦一道
	if err := dl.Fetch(ctx, dl.Request{URLs: []string{match}, Dest: dest, SHA1: it.SHA1, Client: modpackHTTP()}, nil); err != nil {
		_ = os.Remove(dest + ".part")
		return false, fmt.Sprintf("下载失败: %v", err)
	}
	return true, ""
}

// rememberPending 记下待决清单对应的实例（处置时要按任务找到实例目录）。
func (m *Manager) rememberPending(taskID, instName string) {
	m.pendMu.Lock()
	if m.pendingInst == nil {
		m.pendingInst = map[string]string{}
	}
	m.pendingInst[taskID] = instName
	m.pendMu.Unlock()
}

// forgetPending 待决清空后解除关联。
func (m *Manager) forgetPending(taskID string) {
	m.pendMu.Lock()
	delete(m.pendingInst, taskID)
	m.pendMu.Unlock()
}

// pendingInstance 按任务取回对应实例。
func (m *Manager) pendingInstance(taskID string) (*Instance, error) {
	m.pendMu.Lock()
	name, ok := m.pendingInst[taskID]
	m.pendMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("这个任务没有待决的文件")
	}
	i, err := m.Get(name)
	if err != nil {
		return nil, fmt.Errorf("任务对应的实例「%s」不在了（可能已被删除）", name)
	}
	return i, nil
}

func writeUnresolvedReport(dir string, refs []modpack.CFRef) {
	var sb strings.Builder
	sb.WriteString("以下模组无法自动下载，请从链接手动下载后放入 mods 目录：\r\n\r\n")
	for _, r := range refs {
		sb.WriteString(fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/projects/%d （文件 ID: %d）\r\n", r.ProjectID, r.FileID))
	}
	_ = os.WriteFile(filepath.Join(dir, "未解析模组清单.txt"), []byte(sb.String()), 0o644)
}
