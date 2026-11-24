package inst

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/modinfo"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
	"minedeck/internal/winname"
)

// modpackHTTP 造导入下载用的 HTTP 客户端：受控客户端（dl.GuardedClient）——每一跳连出去之前都查
// 是不是公网，用户确认过的站点日后 DNS 被解析到内网、或者借跳转指到内网也连不出去。
// 交给它的是「走代理时不按本机的解析结果拦」的站点：内置的名单（不含用户确认过的自定义站点），
// 加上它们的文件实际所在的 CDN——GitHub 的发布文件跳到 release-assets.githubusercontent.com，
// CurseForge 的在 forgecdn.net 上。这些域名在国内常被解析成 0.0.0.0，照本机的结果拦，开着代理也下不了。
// 测试里换成带假 RoundTripper 的版本，不真正联网。
var modpackHTTP = func() *http.Client { return dl.GuardedClient(knownDownloadHost) }

func knownDownloadHost(host string) bool {
	if modpack.IsAllowedHost(host, nil) {
		return true
	}
	for _, suf := range []string{".githubusercontent.com", ".forgecdn.net", ".modrinth.com"} {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

// ImportModpackAsync 从解析好的整合包创建实例（核心/版本/加载器由整合包决定）。
// 现成服务端压缩包不走下载核心那条流水线，解压后用接管已有服务器的识别来登记。
func (m *Manager) ImportModpackAsync(pack *modpack.Pack, req CreateReq, cfKey string) (string, error) {
	if pack.Format == modpack.FormatServer {
		return m.importServerAsync(pack, req)
	}
	req.Core, req.MC, req.Build = pack.Core, pack.MC, pack.Loader
	// 之前逐个确认过的自定义站点直接用（记忆存 app.Config.TrustedHosts，作用域 modpack）：
	// 这次导入再遇到同站点的待决文件就不再列为待决、直接下载。
	pack.Reclassify(app.TrustedHostFunc(app.TrustScopeModpack))
	if src := strings.TrimSpace(req.LocalSource); src != "" {
		if st, err := os.Stat(src); err != nil || !st.IsDir() {
			return "", fmt.Errorf("本地来源文件夹不可用（没法自动下载的文件会从这里按原样拷过去）: %s", src)
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
	t.EnableCancel()
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		if pack.ZipPath != "" {
			defer os.Remove(pack.ZipPath) // 导入结束（成功/失败）后删除缓存 zip 副本，避免每次导入累积 100-500MB
		}
		if err := m.runImport(ctx, t, pack, req, dir, cfKey); err != nil {
			m.forgetPending(t.ID())
			t.SetPending(nil)
			for _, kind := range []string{"import-pending", "cf-unresolved", "import-kept", "java-req", "import-case", "import-skipped"} {
				t.RemoveWarning(kind)
			}
			// 先清后报失败：任务一显示失败，半成品就得已经删干净了（删不干净的带着记号留在「没建完的文件夹」里，见 discardCreatingDir）
			failCreate(t, err, m.discardFailed(err, req.Name, dir))
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
	// 这里不加总时限：建服务器本体那几步在 runCreateWith 里有 90 分钟，整合包的文件下载一直没有
	// （单个文件 90 秒没有新数据会换源重试；几百个文件、慢的线路，总共要多久说不准）
	return m.runCreateWith(ctx, t, req, dir, func(i *Instance) error {
		return m.applyModpack(ctx, t, pack, req, i, cfKey)
	})
}

// applyModpack 只操作尚未发布的实例；下载、覆盖和记账都完成后，由 completeCreate 发布。
func (m *Manager) applyModpack(ctx context.Context, t *tasks.Task, pack *modpack.Pack, req CreateReq, i *Instance, cfKey string) error {
	dir := i.Dir
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
		// 清单里不全是模组：资源包、光影、数据包各进各的文件夹（都放进 mods 的话，在那里什么用都没有）
		if line := cfPlacement(cf); line != "" {
			t.Logf("%s", line)
		}
		if len(unresolved) > 0 {
			writeUnresolvedReport(dir, unresolved)
			t.Logf("⚠ %d 个文件无法自动下载（%s），记进了没装上的文件，导入完成后可以逐个处理", len(unresolved), cfWhySummary(unresolved))
			items := make([]tasks.WarnItem, 0, len(unresolved))
			for _, r := range unresolved {
				pf, wi := cfPendingOf(r)
				job.add(pendingJob{file: pf, origin: "CurseForge", note: pf.Note})
				items = append(items, wi)
			}
			t.AddWarning(tasks.Warning{
				Kind:  "cf-unresolved",
				Title: fmt.Sprintf("%d 个文件需手动补装", len(unresolved)),
				Note:  "这些文件没法自动下载（作者不允许第三方工具下载，或者没有填 CurseForge API Key）：可以点链接到 CurseForge 网站下载，放进名字前面写的那个文件夹，或在下面「没装上的文件」里逐个处理（清单也已保存到服务器文件夹里的「未解析模组清单.txt」）。",
				Items: items,
			})
		}
	}
	// 清单里点名要下载的文件，落点也不能是 MineDeck 自己的文件。这时还没登记（下载和覆盖都在 completeCreate 之前），
	// 登记只按内存里的设置重写 instance.json 和 run.bat：老版本的日志配置、游戏内 AI 的设置这些下载下来就算数了；
	// 带着创建记号的文件夹里也不能出现外来的 instance.json，这时程序没了，下次启动会当成登记写到一半（promoteCreatedRoots）。
	// 覆盖目录那边同理（见 overrideSink.write）。
	var refused []string
	files = slices.DeleteFunc(files, func(f modpack.File) bool {
		if overrideAllowed(f.Path) {
			return false
		}
		refused = append(refused, f.Path)
		return true
	})
	if len(refused) > 0 {
		t.Logf("整合包清单里的这些文件没有下载（由 MineDeck 自己生成，或者名字写不成普通文件）：%s", strings.Join(refused, "、"))
	}
	files, dupDL := dedupeDownloads(files)
	if len(dupDL) > 0 {
		t.Logf("整合包清单里的这些文件和前面的落点只差大小写，在 Windows 上是同一个文件，没有下载（留的是前面那个）：%s", shortNames(dupDL, 8))
	}
	if overrides, err := modpack.OverrideFiles(pack); err == nil { // 读不了的，应用覆盖目录那一步会报出来
		conflicts := modpackCaseConflicts(files, dupDL, overrides)
		addCaseWarning(t, conflicts, len(conflicts))
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
	sink := newOverrideSinkContext(ctx, dir)
	if err := modpack.ExtractOverrides(pack, dir, sink.write); err != nil {
		return fmt.Errorf("应用整合包覆盖文件失败: %w", err)
	}
	if len(sink.notTaken) > 0 {
		t.Logf("整合包覆盖文件夹里的这些文件没有采用（由 MineDeck 自己生成，或者名字写不成普通文件）：%s", shortNames(sink.notTaken, 8))
	}
	if len(sink.dups) > 0 {
		t.Logf("整合包覆盖文件夹里的这些文件和前面的文件只差大小写，在 Windows 上是同一个文件，用的是后面这个：%s", shortNames(sink.dups, 8))
	}
	// 覆盖目录可以带 server.properties：世界目录的名字指到服务器文件夹外面的整合包不要
	// （服务器一启动就会去动那个文件夹，备份和还原也跟着找过去）。
	if name, ok := CheckLevelName(dir); !ok {
		return fmt.Errorf("整合包覆盖文件里的 server.properties 把世界文件夹（level-name）指到了服务器文件夹外面：%s", name)
	}

	// ---- 本地继承：待决文件凡本地已有就算解决 ----
	// 整合包覆盖目录里正好有待决文件（用户把魔改模组随包带上的情形）→ 按原样保留；
	// 再看本地来源目录（用户手里还留着的老服务器目录）：相对路径或文件名对得上的按原样复制。
	pending := job.take()
	var kept []keptFile
	var remain []modpack.PendingFile
	skipped := skippedFiles(pack.ClientOnly, refused, sink.notTaken)
	for _, p := range pending {
		if p.file.Reason != "cf" && !overrideAllowed(p.file.Path) {
			skipped = append(skipped, skippedFile{p.file.Path, notTakenWhy(p.file.Path)})
			continue // 落点是 MineDeck 自己的文件：不继承，也不列进待决清单
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p.file.Path))); err == nil {
			kept = append(kept, keptFile{path: p.file.Path, source: "整合包覆盖文件夹"})
			continue
		}
		if src := findLocalFile(req.LocalSource, p.file.Path); src != "" {
			if err := copyLocalFileContext(ctx, src, filepath.Join(dir, filepath.FromSlash(p.file.Path))); err != nil {
				t.Logf("⚠ 本地继承 %s 失败: %v", p.file.Path, err)
				remain = append(remain, p.file)
				continue
			}
			kept = append(kept, keptFile{path: p.file.Path, source: "本地来源文件夹"})
			continue
		}
		remain = append(remain, p.file)
	}
	addSkippedWarning(t, skipped, len(skipped))
	// 最终控制文件由 completeCreate 重写；这时实例仍不可见。
	restoreLog4jXML(ctx, t, i)
	m.finishImportPack(t, i, pack.Name, remain, kept)
	return nil
}

// caseConflict 一组只差大小写的落点：Windows 上是同一个文件，只留下一份。names 按写的先后，kept 是内容留下来的那个。
type caseConflict struct {
	names []string
	kept  string
	why   string // 为什么留它
}

// modpackCaseConflicts 导入整合包之前找出只差大小写的落点（A13、A47）：清单里下载的之间（dedupeDownloads 只下载前面那个），
// 下载的和覆盖文件夹里的、覆盖文件夹里的之间（先下载、再一层层写覆盖文件夹，后写的盖掉先写的）。留哪个的规矩不变，
// 只是在导入结果里列出来，不再只写在日志里。files 是去过重的下载清单，dupDL 是去掉的那些，overrides 是覆盖文件夹里的（按写的先后）。
func modpackCaseConflicts(files []modpack.File, dupDL, overrides []string) []caseConflict {
	var out []caseConflict
	firstDL := map[string]string{}
	for _, f := range files {
		firstDL[winname.Fold(path.Clean(f.Path))] = f.Path
	}
	for _, d := range dupDL {
		if k := firstDL[winname.Fold(path.Clean(d))]; k != "" && k != d { // 写法一模一样的不算只差大小写
			out = append(out, caseConflict{names: []string{k, d}, kept: k, why: "清单里重名的只下载前面那个"})
		}
	}
	type write struct {
		name     string
		override bool
	}
	groups := map[string][]write{}
	var order []string
	add := func(name string, override bool) {
		k := winname.Fold(path.Clean(name))
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], write{name, override})
	}
	for _, f := range files {
		add(f.Path, false)
	}
	for _, o := range overrides {
		if overrideAllowed(o) { // 不采用的不写，不算
			add(o, true)
		}
	}
	for _, k := range order {
		ws := groups[k]
		var names []string
		fromDL := false
		for _, w := range ws {
			if !slices.Contains(names, w.name) {
				names = append(names, w.name)
			}
			fromDL = fromDL || !w.override
		}
		if len(names) < 2 {
			continue
		}
		why := "覆盖文件夹里后写的盖掉先写的"
		if fromDL {
			why = "覆盖文件夹里的盖掉清单里下载的"
		}
		out = append(out, caseConflict{names: names, kept: ws[len(ws)-1].name, why: why})
	}
	return out
}

// maxWarnItems 一条告警最多列几项，多的说一共几个。
const maxWarnItems = 50

// warnItems 告警里列的项：最多 maxWarnItems 项。total 是一共有几个（names 可能已经只记了前面一部分），
// 比列出来的多时末尾说一共几个，unit 是「个」「组」。
func warnItems(names []string, total int, unit string) []tasks.WarnItem {
	shown := names[:min(len(names), maxWarnItems)]
	items := make([]tasks.WarnItem, 0, len(shown)+1)
	for _, n := range shown {
		items = append(items, tasks.WarnItem{Name: n})
	}
	if total > len(shown) {
		items = append(items, tasks.WarnItem{Name: fmt.Sprintf("……一共 %d %s", total, unit)})
	}
	return items
}

// addCaseWarning 只差大小写的落点写进任务结果（A13、A47）。total 是一共几组（conflicts 可能只记了前面一部分）。
func addCaseWarning(t *tasks.Task, conflicts []caseConflict, total int) {
	if total == 0 {
		return
	}
	names := make([]string, 0, min(len(conflicts), maxWarnItems))
	for _, c := range conflicts[:min(len(conflicts), maxWarnItems)] {
		names = append(names, fmt.Sprintf("%s：留下的是「%s」（%s）", strings.Join(c.names, "、"), c.kept, c.why))
	}
	t.AddWarning(tasks.Warning{Kind: "import-case", Title: fmt.Sprintf("%d 组文件名只差大小写", total),
		Note: "Windows 上文件名不分大小写，每一组都是同一个文件，只留下一份：内容是下面写的那一个，文件名可能还是最先写的那种写法。" +
			"要另一份的话，导入完成以后到服务器文件夹里自己换。",
		Items: warnItems(names, total, "组")})
}

// skippedFile 导入时没装进来的一个文件和原因。
type skippedFile struct{ path, why string }

// skippedFiles 整合包导入时跳过的：清单里写明只给客户端用的、清单里和覆盖文件夹里不采用的（见 overrideAllowed）。
func skippedFiles(clientOnly, refused, notTaken []string) []skippedFile {
	var out []skippedFile
	for _, p := range clientOnly {
		out = append(out, skippedFile{p, "整合包清单里写明只给客户端用"})
	}
	for _, p := range append(slices.Clone(refused), notTaken...) {
		out = append(out, skippedFile{p, notTakenWhy(p)})
	}
	return out
}

// notTakenWhy overrideAllowed 不收的文件为什么不收。
func notTakenWhy(rel string) string {
	if IsControlFile(path.Clean(strings.ReplaceAll(rel, `\`, "/"))) {
		return "是 MineDeck 自己的文件，由 MineDeck 生成"
	}
	return "名字在 Windows 上写不出来"
}

// addSkippedWarning 导入时没装进来的文件写进任务结果（A46）。以前只写在任务日志里，原版服没有插件 / 模组页，导入完了就找不回来。
// total 是一共几个（files 可能只记了前面一部分）。
func addSkippedWarning(t *tasks.Task, files []skippedFile, total int) {
	if total == 0 {
		return
	}
	names := make([]string, 0, min(len(files), maxWarnItems))
	for _, f := range files[:min(len(files), maxWarnItems)] {
		names = append(names, fmt.Sprintf("%s（%s）", f.path, f.why))
	}
	t.AddWarning(tasks.Warning{Kind: "import-skipped", Title: fmt.Sprintf("%d 个文件没有装进来", total),
		Note:  "导入时跳过了这些文件，原因写在每一项后面。只给客户端用的本来就不该装在服务器上；要用别的，导入完成以后自己放进服务器文件夹。",
		Items: warnItems(names, total, "个")})
}

// dedupeDownloads 清单里落点只差大小写的（一模一样的也算）在 Windows 上是同一个文件，而下载是并发的，会同时写它：
// 留第一条，后面的不下载，返回给日志列出来（v3.1.0 正式版前检查 C-2e）。
func dedupeDownloads(files []modpack.File) (kept []modpack.File, dups []string) {
	seen := map[string]bool{}
	for _, f := range files {
		k := winname.Fold(path.Clean(f.Path))
		if seen[k] {
			dups = append(dups, f.Path)
			continue
		}
		seen[k] = true
		kept = append(kept, f)
	}
	return kept, dups
}

// overrideSink 应用覆盖目录时一个一个写文件（ExtractOverrides 的回调，见 runImport），记下没采用的和只差大小写的。
type overrideSink struct {
	ctx            context.Context
	dir            string
	seen           map[string]string // 已经写进来的：不分大小写的路径 → 包里的写法
	notTaken, dups []string
}

func newOverrideSink(dir string) *overrideSink {
	return newOverrideSinkContext(context.Background(), dir)
}

func newOverrideSinkContext(ctx context.Context, dir string) *overrideSink {
	return &overrideSink{ctx: ctx, dir: dir, seen: map[string]string{}}
}

func (s *overrideSink) write(rel string, r io.Reader) error {
	// 覆盖目录是整合包作者给的文件，写进来的时候实例还没登记（登记在最后，completeCreate）：MineDeck 自己的文件
	// （instance.json、run.bat、老版本的日志配置、游戏内 AI 的设置这些）一律不收。登记只重写 instance.json 和 run.bat，
	// 别的收进来就生效了；外来的 instance.json 还会让中途断掉的导入在下次启动时被当成登记写到一半（promoteCreatedRoots）。
	// 名字在 Windows 上写不出原样的（设备名、结尾是点或空格的）也不收。
	if !overrideAllowed(rel) {
		s.notTaken = append(s.notTaken, rel)
		return nil
	}
	// 只差大小写的两个名字在 Windows 上是同一个文件，后写的会把先写的盖掉。照旧以后一个为准：覆盖目录是一层层写的，
	// 后一层（server-overrides）本来就是要盖掉前一层（overrides）的，跨层只差大小写时也该它说了算；同一层里的
	// 只差大小写，包本身就说不清要哪个。不一样的是记下来写进日志，不再悄悄盖掉（v3.1.0 正式版前检查 C-2e）
	k := winname.Fold(rel)
	if first, ok := s.seen[k]; ok && first != rel {
		s.dups = append(s.dups, rel)
	}
	s.seen[k] = rel
	dest := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	lease, err := diskguard.Acquire(s.ctx, filepath.Dir(dest), diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	w, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(lease.Writer(w), r)
	if werr := w.Close(); cerr == nil {
		cerr = werr
	}
	return cerr
}

// shortNames 写进任务日志的文件清单：最多列 max 个，多的说一共几个。
func shortNames(items []string, max int) string {
	return shortNamesOf(items, len(items), max)
}

// shortNamesOf 同 shortNames，total 是一共几个（items 可能只记了前面一部分）。
func shortNamesOf(items []string, total, max int) string {
	shown := items[:min(len(items), max)]
	if total <= len(shown) {
		return strings.Join(shown, "、")
	}
	return strings.Join(shown, "、") + fmt.Sprintf("、…等 %d 个", total)
}

// overrideAllowed 整合包覆盖目录里的这个文件（rel 相对实例目录，斜杠分隔）能不能写进来。规矩见 runImport 里用它的地方。
func overrideAllowed(rel string) bool {
	// 先化简再认：mods/../instance.json 落到磁盘上就是 instance.json。现在的调用方传进来的都是化简过的，
	// 这里不指望这一点。
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	// 每一段都要在 Windows 上原样写得出来（winname.PathOK）：设备名（带不带扩展名都算，CON.txt、COM1.log）、
	// 结尾是点或空格的（Windows 落盘时会去掉它们，"run.bat." 落下来就是 run.bat）都不收。
	// 以前结尾的点和空格是去掉以后再认，名字照收；带扩展名的设备名在 Windows 11 上也照收（v3.1.0 正式版前检查 C-2e）
	return winname.PathOK(rel) && filepath.IsLocal(filepath.FromSlash(rel)) && !IsControlFile(rel)
}

// finishImport 收尾：按原样保留的、待决的、Java 要求的提示与记录文件。
func (m *Manager) finishImport(t *tasks.Task, req CreateReq, dir string, pending []modpack.PendingFile, kept []keptFile) {
	i, err := m.Get(req.Name)
	if err != nil {
		return
	}
	m.finishImportFor(t, i, pending, kept)
}

func (m *Manager) finishImportFor(t *tasks.Task, i *Instance, pending []modpack.PendingFile, kept []keptFile) {
	m.finishImportPack(t, i, "", pending, kept)
}

// finishImportPack 同 finishImportFor，packName 是整合包的名字（记进没装上的文件的清单，给人看）。
func (m *Manager) finishImportPack(t *tasks.Task, i *Instance, packName string, pending []modpack.PendingFile, kept []keptFile) {
	dir := i.Dir
	if len(kept) > 0 {
		items := make([]tasks.WarnItem, 0, len(kept))
		for _, k := range kept {
			items = append(items, tasks.WarnItem{Name: fmt.Sprintf("%s（来自%s，未校验）", k.path, k.source)})
		}
		t.AddWarning(tasks.Warning{
			Kind:  "import-kept",
			Title: fmt.Sprintf("按原样保留 %d 个文件", len(kept)),
			Note:  "这些文件没有从网上下载，用的是整合包覆盖文件夹或本地来源文件夹里已有的那份，内容没有校验（魔改的模组因此能原样保留）。",
			Items: items,
		})
		t.Logf("✔ 按原样保留 %d 个文件（整合包覆盖文件夹 / 本地来源文件夹）", len(kept))
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
		if pf.Note != "" {
			pi.Note = pf.Note // 这一项有自己的说法（比如查到了是哪个项目、为什么下不了）
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
		m.rememberPendingInstance(t.ID(), i)
		t.AddWarning(pendingWarning(len(items)))
		// 清单存进服务器文件夹（.minedeck/import-pending.json 和给人看的 txt）：程序重开以后还能在这台服务器上接着处理（R09）
		l := &pendingList{Pack: packName, At: time.Now()}
		for _, it := range items {
			l.Items = append(l.Items, pendingRecord{PendingItem: it})
		}
		if err := writePendingList(dir, l); err != nil {
			t.Logf("⚠ 没装上的文件的清单没能存进服务器文件夹（%v）：这次运行里还能在这一页处理，程序重开以后就找不到了", err)
		}
	}
	// Java 要求：魔改模组常声明比实例新的 Java（1.20.1 实例是 17，模组可能要 25）。
	// 只提示不阻断：1.20.1 + Java 25 的组合允许存在，到「设置 → 启动与内存」换。
	javaMajor := i.JavaMajor
	if need, mods := javaRequiredBy(dir, javaMajor); need > javaMajor && javaMajor > 0 {
		warn := make([]tasks.WarnItem, 0, len(mods))
		for _, x := range mods {
			warn = append(warn, tasks.WarnItem{Name: x})
		}
		t.AddWarning(tasks.Warning{
			Kind:  "java-req",
			Title: fmt.Sprintf("有模组要求 Java %d，这台服务器现在是 Java %d", need, javaMajor),
			Note:  "这只是提醒，不影响导入结果。装着这样的模组启动可能失败或部分功能不工作；要换的话到「设置 → 启动与内存」里选（MineDeck 可以自动下载便携版），也可以先保持现状。",
			Items: warn,
		})
	}
	keptLine := ""
	if len(kept) > 0 {
		keptLine = fmt.Sprintf("，按原样保留 %d 个", len(kept))
	}
	t.Logf("整合包导入完成 ✔（%d 个文件待处理%s；如需正版验证等设置请到「设置 → 常用」调整）", len(items), keptLine)
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
	return copyLocalFileContext(context.Background(), src, dest)
}

func copyLocalFileContext(ctx context.Context, src, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	stop := context.AfterFunc(ctx, func() { _ = in.Close() })
	defer stop()
	unlock, err := atomicfile.LockPathContext(ctx, dest)
	if err != nil {
		return err
	}
	defer unlock()
	lease, err := diskguard.Acquire(ctx, filepath.Dir(dest), diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	err = atomicfile.CopyWith(dest, in, 0o644, lease.Writer)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
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

// ResolveImportPending 处置整合包导入任务的待决文件（旧接口，按任务编号）。逐项执行后成功的那几项移出待决清单；
// 失败的（源文件不存在、下载校验不符、没确认信任）留在清单里等下次处理。整批不因单项失败而中断。
// 清单以服务器文件夹里存的那份为准（见 pendingstore.go），任务上的待决清单是它的镜像，处置完跟着更新。
// 权限约束在操作层（instance.import.pending：仅人工 + 仅本机）。
func (m *Manager) ResolveImportPending(ctx context.Context, taskID string, acts []PendingAction) ([]ResolveOutcome, []tasks.PendingItem, error) {
	if len(acts) == 0 {
		return nil, nil, fmt.Errorf("没有要处理的项")
	}
	// 一次只处置一批：两个请求同时处置同一项（开了两个窗口）会一起写同一个 .part，落下来的可能是拼坏的文件
	// 下载可能要等上一两分钟，所以拿不到锁就直接告诉对方，不让请求干等着转圈
	if !m.resolveMu.TryLock() {
		return nil, nil, fmt.Errorf("上一批待处理的文件还在处理（可能在下载），等它完成再试")
	}
	defer m.resolveMu.Unlock()
	t := m.Tasks.Get(taskID)
	if t == nil {
		return nil, nil, fmt.Errorf("没有这个任务: %s", taskID)
	}
	inst, err := m.pendingInstance(t.ID())
	if err != nil {
		return nil, nil, err
	}
	out, _, err := m.resolvePending(ctx, inst, acts)
	return out, t.PendingItems(), err
}

// resolvePending 处置一台服务器上没装上的文件，调用方拿着 resolveMu。清单从服务器文件夹里读（不可信，见 pendingstore.go），
// 落点每一次都在写之前重新检查。skip 把项标成「已跳过」留在清单里（还没装上，以后还能改成别的处理）；
// local、trust 成功的项移出清单。处置完把清单写回去，这次运行里对应的导入任务跟着更新。
func (m *Manager) resolvePending(ctx context.Context, inst *Instance, acts []PendingAction) ([]ResolveOutcome, *pendingList, error) {
	release, err := m.UseWorldInstance(inst, "补装整合包文件")
	if err != nil {
		return nil, nil, err
	}
	defer release()
	// 配置锁和 server.properties 的路径锁不整批拿：trust 要联网下载，一批下来可能好几分钟，以前这期间保存设置、
	// 改配置文件的请求都在干等（2026-11-14 合并后复审发现 8）。现在每一项只在真正写进服务器文件夹的那一下拿，
	// 拿到以后把落点的检查在锁里重做一遍，见 placePending。上面的登记（UseWorldInstance）照旧整批占着：
	// 它不让人排队，只让换地图、还原这类搬动整个世界的操作在这期间直接报「正在补装整合包文件」。
	l, err := m.loadPending(inst)
	if err != nil {
		return nil, nil, err
	}
	if l == nil || len(l.Items) == 0 {
		return nil, l, fmt.Errorf("这台服务器没有整合包里没装上的文件")
	}
	byID := map[string]int{}
	for k, r := range l.Items {
		byID[r.ID] = k
	}
	var out []ResolveOutcome
	done := map[string]bool{} // 处置成功、移出清单的
	skip := map[string]bool{} // 这一批里新跳过的
	var ctxErr error
	for _, act := range acts {
		oc := ResolveOutcome{ID: act.ID, Act: act.Act}
		k, ok := byID[act.ID]
		if !ok || done[act.ID] {
			oc.Note = "没有这一项（可能刚处理过）"
			out = append(out, oc)
			continue
		}
		rec := l.Items[k]
		it := rec.PendingItem
		oc.Path = it.Path
		if err := ctx.Err(); err != nil {
			ctxErr = err // 前面处置成的照样记下来，再报错
			break
		}
		// 先查一遍落点：不合规的在读本地文件、发下载请求之前就拒绝。写进去之前在锁里还要再查一遍（placePending）
		if act.Act == "local" || act.Act == "trust" {
			if _, err := pendingDestination(inst, it.Path); err != nil {
				oc.Note = err.Error()
				out = append(out, oc)
				continue
			}
		}
		switch act.Act {
		case "skip":
			oc.OK = true
			if rec.Skipped || skip[it.ID] {
				oc.Note = "之前已经跳过了"
				break
			}
			skip[it.ID] = true
			oc.Note = "已跳过（没有装上，以后还能在这台服务器的详情页里再处理）"
			m.pendingLog(inst, "没装上的文件 %s：选择跳过", it.Path)
		case "local":
			src, note := resolveLocalSource(act.Path, it.Path)
			if src == "" {
				oc.Note = note
				break
			}
			// CurseForge 的项不一定知道文件名：保留哪个文件就叫它本来的名字，放进这一项该在的文件夹
			// （模组进 mods，资源包进 resourcepacks……文件夹是清单里那一项的落点决定的，不是用户给的）
			destRel := it.Path
			if it.Reason == "cf" {
				destRel = path.Dir(it.Path) + "/" + filepath.Base(src)
			}
			destErr, err := m.placePending(ctx, inst, destRel, func(dest string) error { return copyPendingFile(ctx, src, dest) })
			if destErr != nil {
				oc.Note = destErr.Error()
				break
			}
			if err != nil {
				oc.Note = fmt.Sprintf("复制失败: %v", err)
				break
			}
			oc.OK = true
			oc.Note = fmt.Sprintf("已用本地文件原样代替（%s）", src)
			m.pendingLog(inst, "没装上的文件 %s：按本地文件原样保留（%s）", it.Path, src)
		case "trust":
			ok, note := m.pendingTrustDownload(ctx, inst, it, act.URL, act.Ack)
			oc.OK, oc.Note = ok, note
			if ok {
				m.pendingLog(inst, "没装上的文件 %s：确认站点后下载完成（校验通过）", it.Path)
			}
		default:
			oc.Note = "不认识的处理方式（可用：skip、local、trust）"
		}
		if oc.OK && act.Act != "skip" {
			done[it.ID] = true
		}
		out = append(out, oc)
	}
	if len(done) > 0 || len(skip) > 0 {
		keep := l.Items[:0:0]
		for _, r := range l.Items {
			if done[r.ID] {
				continue
			}
			if skip[r.ID] {
				r.Skipped = true
				r.Options = pendingOptions(r.PendingItem, true)
			}
			keep = append(keep, r)
		}
		l.Items = keep
		if err := writePendingList(inst.Dir, l); err != nil {
			// 文件已经放进去了，只是清单没更新：下次读出来这几项还在，再处理一次也不会出错
			log.Printf("没能更新「%s」没装上的文件的清单：%v", inst.Name, err)
			m.pendingLog(inst, "⚠ 没能更新服务器文件夹里没装上的文件的清单：%v", err)
		}
		m.syncPendingTasks(inst, l)
	}
	return out, l, ctxErr
}

// pendingDestination 已发布实例的补装只写普通模组、配置和资源目录。
// 世界、server.properties 和控制文件应走各自的专用入口，不能由旧待决任务覆盖。
// 同时检查真实落点，避免短名、符号链接和目录联接把 config 等别名指向世界。
func pendingDestination(i *Instance, rel string) (string, error) {
	if !overrideAllowed(rel) {
		return "", fmt.Errorf("这个没装上的文件不能写进服务器：%s", rel)
	}
	root, err := RealPathStrict(i.Dir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(i.Dir, filepath.FromSlash(rel))
	real, err := RealPathStrict(dest)
	if err != nil {
		return "", err
	}
	actual, err := filepath.Rel(root, real)
	if err != nil || !filepath.IsLocal(actual) || actual == "." || !InsideReal(root, real) {
		return "", fmt.Errorf("这个文件的实际位置不在服务器文件夹内")
	}
	// 上一级文件夹也要在里面：写的时候临时文件建在上一级（Codex 复审 beta.3 第四轮）
	if parent, err := RealPathStrict(filepath.Dir(dest)); err != nil || !InsideReal(root, parent) {
		return "", fmt.Errorf("这个文件所在的文件夹实际不在服务器文件夹内")
	}
	actual = filepath.ToSlash(actual)
	first, _, nested := strings.Cut(strings.ToLower(actual), "/")
	allowed := nested && (first == "mods" || first == "plugins" || first == "config" || first == "defaultconfigs" || first == "resourcepacks" || first == "datapacks" || first == "shaderpacks" || first == "kubejs" || first == "scripts")
	if !allowed || IsControlFile(actual) {
		return "", fmt.Errorf("没装上的文件只能补进模组、插件、配置和资源文件夹；换世界请用「世界与备份 → 换地图」，server.properties 请到「设置」里改")
	}
	level, err := LevelNameStrict(i.Dir)
	if err != nil {
		return "", err
	}
	for _, world := range []string{level, level + "_nether", level + "_the_end"} {
		worldPath, err := RealPathStrict(filepath.Join(i.Dir, filepath.FromSlash(world)))
		if err != nil {
			return "", err
		}
		if within(real, worldPath) {
			return "", fmt.Errorf("这个文件在当前世界里，换世界请用「世界与备份 → 换地图」：%s", rel)
		}
	}
	return dest, nil
}

// resolveLocalSource 解析 act=local 的源：直接给文件就用；给目录就在里面按待决路径找；都不行说明原因。
func resolveLocalSource(value, relPath string) (src, why string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "给出本地文件或文件夹的路径"
	}
	st, err := os.Stat(value)
	if err != nil {
		return "", "文件或文件夹不存在（给完整路径，如 D:\\old\\mods\\xxx.jar）"
	}
	if st.IsDir() {
		if found := findLocalFile(value, relPath); found != "" {
			return found, ""
		}
		return "", "这个文件夹里没有对得上的文件（按相对路径或 mods / plugins 下的同名文件找）"
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
	if it.SHA1 == "" {
		return false, "这一项没有校验值，下载下来没法核对，不下载"
	}
	known := app.TrustedHostFunc(app.TrustScopeModpack)(host)
	if !known && !ack {
		return false, fmt.Sprintf("站点 %s 没有确认过：读一遍风险说明并确认后才下载", host)
	}
	if _, err := pendingDestination(inst, it.Path); err != nil { // 落点不合规的不发请求
		return false, err.Error()
	}
	// 下载不拿配置锁和设置锁，也不写服务器文件夹：下到缓存文件夹里的临时文件夹，SHA-1 对上了才算下好。
	// 一个文件可能要下好几分钟，这期间保存设置、改配置文件照常（复审发现 8）。写进服务器的那一下才拿锁，
	// 并在锁里把落点重新查一遍（下载期间世界名可能被改了），见 placePending。
	stage, err := pendingStageDir()
	if err != nil {
		return false, fmt.Sprintf("下载失败: %v", err)
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, "download.bin")
	// 受控客户端：即使 grant 时查过公网，连接级（DNS 变动、重定向到内网）也再拦一道
	if err := dl.Fetch(ctx, dl.Request{URLs: []string{match}, Dest: staged, SHA1: it.SHA1, Client: modpackHTTP()}, nil); err != nil {
		return false, fmt.Sprintf("下载失败: %v", err)
	}
	destErr, err := m.placePending(ctx, inst, it.Path, func(dest string) error { return copyPendingFile(ctx, staged, dest) })
	if destErr != nil {
		return false, destErr.Error()
	}
	if err != nil {
		return false, fmt.Sprintf("下载好了，写进服务器时出错: %v", err)
	}
	// 下载下来、校验也过了、写进去了，才把站点记成信任：记下以后同站点的文件不再问，以后导入别的整合包也直接下，
	// 所以不能因为点过一次确认就记——文件没下成（连不上、校验不符、落点不让写）的站点下次照样要确认。
	if !known {
		app.TrustHost(host, app.TrustScopeModpack)
	}
	return true, ""
}

// placePending 把一个待决文件写进服务器文件夹，锁只在这一下拿。顺序是：配置锁（config|实例名）→ server.properties
// 的路径锁 → 在锁里重做落点检查（九个目录的白名单、真实路径、控制文件、当前世界）→ 目标文件的路径锁（copyPendingFile 里）。
// 检查和写入之间没有空档：世界名在 server.properties 里，设置锁拿着它就改不了；配置文件也不会同时有人在写。
// 返回的两个错误分开说：destErr 是落点不合规（照原样给用户看），err 是拿锁或写的时候出错。
func (m *Manager) placePending(ctx context.Context, inst *Instance, rel string, write func(dest string) error) (destErr, err error) {
	unlockConfig, err := LockKeyContext(ctx, "config|"+inst.Name)
	if err != nil {
		return nil, err
	}
	defer unlockConfig()
	unlockProps, err := atomicfile.LockPathContext(ctx, inst.PropsPath())
	if err != nil {
		return nil, err
	}
	defer unlockProps()
	dest, err := pendingDestination(inst, rel)
	if err != nil {
		return err, nil
	}
	return nil, write(dest)
}

// copyPendingFile 把 src 原样写成 dest：写同一文件夹里的临时文件、刷盘、一步换上去，失败时原件还在。
// 磁盘余量只查不排队（diskguard 的 Shared）：这一下拿着配置锁和设置锁，不能排在备份、地图暂存这些大批量写盘后面干等，
// 那样保存设置又会被挡住。补装的是单个模组、配置、资源包，不是整个世界。
// 和 copyLocalFileContext 的差别只在这一点；那个函数导入时还在用，不动它。
func copyPendingFile(ctx context.Context, src, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	stop := context.AfterFunc(ctx, func() { _ = in.Close() })
	defer stop()
	unlock, err := atomicfile.LockPathContext(ctx, dest)
	if err != nil {
		return err
	}
	defer unlock()
	lease, err := diskguard.Acquire(ctx, filepath.Dir(dest), diskguard.Options{Shared: true, AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()
	err = atomicfile.CopyWith(dest, in, 0o644, lease.Writer)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// pendingStageDir 信任下载用的临时文件夹：在 MineDeck 的缓存文件夹里（没有时用系统临时文件夹），不在任何服务器文件夹里。
// 用完由调用方整个删掉；程序中途退出留下的是缓存，不影响服务器。
func pendingStageDir() (string, error) {
	base := os.TempDir()
	if app.CacheDir != "" {
		base = filepath.Join(app.CacheDir, "pending-download")
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, "dl-*")
}

// pendingRef 待决清单对应的实例。光记名字不够：那台实例删掉以后再建一台同名的，
// 回到旧任务页处置，文件会落进新的那台（还会顶掉同名文件），所以连创建时间一起记，处置时核对。
type pendingRef struct {
	name    string
	created time.Time
}

// rememberPending 记下待决清单对应的实例（处置时要按任务找到实例目录）。
func (m *Manager) rememberPending(taskID, instName string) {
	if i, err := m.Get(instName); err == nil {
		m.rememberPendingInstance(taskID, i)
	}
}

func (m *Manager) rememberPendingInstance(taskID string, i *Instance) {
	m.pendMu.Lock()
	if m.pendingInst == nil {
		m.pendingInst = map[string]pendingRef{}
	}
	m.pendingInst[taskID] = pendingRef{name: i.Name, created: i.CreatedAt}
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
	ref, ok := m.pendingInst[taskID]
	m.pendMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("这个任务没有要处理的文件")
	}
	i, err := m.Get(ref.name)
	if err != nil {
		return nil, fmt.Errorf("任务对应的服务器「%s」不在了（可能已被删除）", ref.name)
	}
	if !i.CreatedAt.Equal(ref.created) {
		return nil, fmt.Errorf("任务对应的服务器「%s」已经删掉了，现在这台同名的是后来新建的，这些文件不会放进去", ref.name)
	}
	return i, nil
}

func writeUnresolvedReport(dir string, refs []modpack.CFRef) {
	var sb strings.Builder
	sb.WriteString("以下文件无法自动下载，请从链接手动下载后放进写明的文件夹：\r\n\r\n")
	for _, r := range refs {
		pf, wi := cfPendingOf(r)
		sb.WriteString(fmt.Sprintf("%s\r\n  %s\r\n  %s\r\n\r\n", pf.Path, pf.Note, wi.URL))
	}
	_ = os.WriteFile(filepath.Join(dir, "未解析模组清单.txt"), []byte(sb.String()), 0o644)
}

// cfPendingOf 一个没法自动下载的 CurseForge 文件：待决清单里的一项，和告警列表里可以点的一行。
// 查到了文件名的，落点写真实的（mods/xxx.jar）：覆盖目录或本地来源目录里有同名文件时能自动认上，
// 用户也看得出缺的是哪个。没查到的只能写编号。
func cfPendingOf(r modpack.CFRef) (modpack.PendingFile, tasks.WarnItem) {
	folder := r.Dir
	switch folder {
	case "mods", "resourcepacks", "shaderpacks", "datapacks":
	default:
		folder = "mods"
	}
	ids := fmt.Sprintf("CurseForge 项目 %d，文件 %d", r.ProjectID, r.FileID)
	rel := fmt.Sprintf("%s/（%s）", folder, ids)
	if r.FileName != "" && !strings.ContainsAny(r.FileName, `/\`) {
		// 文件名是接口给的：只认单独一段的名字，再过一遍和覆盖目录一样的检查，
		// 不能是 MineDeck 自己的文件，也不能出实例目录
		if p := folder + "/" + r.FileName; overrideAllowed(p) {
			rel = p
		}
	}
	var why string
	switch r.Why {
	case modpack.CFWhyNoDist:
		why = "作者不允许第三方工具下载这个文件。到 CurseForge 网站下载以后在这里指定，或者直接放进 " + folder + " 文件夹"
	case modpack.CFWhyGone:
		why = "CurseForge 上找不到这个文件了（可能已经被作者删掉）"
	case modpack.CFWhyName:
		why = "这个文件的名字在这台电脑上写不成普通文件"
	case modpack.CFWhyNoKey:
		why = "没有填 CurseForge API Key，不能自动下载。保留本地已有的文件，或者手动放一份"
	default:
		why = "CurseForge 的接口没有查成（检查 API Key 和网络），不能自动下载。保留本地已有的文件，或者手动放一份"
	}
	name := rel
	if r.Name != "" {
		why = "「" + r.Name + "」：" + why
		name = rel + "（" + r.Name + "）"
	}
	link := r.URL
	if link == "" {
		link = fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/projects/%d", r.ProjectID)
	}
	return modpack.PendingFile{Path: rel, Reason: "cf", Note: why}, tasks.WarnItem{Name: name, URL: link}
}

// cfWhySummary 这一批没法自动下载的，各是因为什么（写进任务日志的那一行）。
func cfWhySummary(refs []modpack.CFRef) string {
	count := map[string]int{}
	for _, r := range refs {
		count[r.Why]++
	}
	var parts []string
	for _, c := range []struct{ why, text string }{
		{modpack.CFWhyNoKey, "没有填 API Key"},
		{modpack.CFWhyAPI, "接口没有查成"},
		{modpack.CFWhyNoDist, "作者不允许第三方下载"},
		{modpack.CFWhyGone, "CurseForge 上找不到"},
		{modpack.CFWhyName, "文件名写不成普通文件"},
		{"", "原因不明"},
	} {
		if n := count[c.why]; n == len(refs) {
			return c.text
		} else if n > 0 {
			parts = append(parts, fmt.Sprintf("%s的 %d 个", c.text, n))
		}
	}
	return strings.Join(parts, "，")
}

// cfPlacement 清单里的文件不全是模组时，说一声各放到了哪（全是模组就不说）。
func cfPlacement(files []modpack.File) string {
	count := map[string]int{}
	for _, f := range files {
		top, _, _ := strings.Cut(f.Path, "/")
		count[top]++
	}
	var parts []string
	for _, c := range []struct{ folder, text string }{
		{"resourcepacks", "资源包"}, {"shaderpacks", "光影"}, {"datapacks", "数据包"},
	} {
		if n := count[c.folder]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d 个进 %s", c.text, n, c.folder))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	line := "清单里不全是模组，按 CurseForge 上的类别分开放了：" + strings.Join(parts, "，")
	if count["datapacks"] > 0 {
		line += "。数据包放在 datapacks 里不会自己生效：带数据包的整合包一般自己带着加载它们的模组"
	}
	return line
}
