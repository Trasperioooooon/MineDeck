package inst

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"minedeck/internal/atomicfile"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// 整合包导入时没装上的文件（界面上叫「没装上的文件」，代码里叫 pending），作为这台服务器自己的数据存在服务器文件夹里，
// 程序重开以后还读得回来、还能接着处理（可用性走查 R09）。以前只记在内存里的任务上，重开就没了。
//
// 存两份：
//   - .minedeck/import-pending.json：MineDeck 自己读的那份。在控制目录 .minedeck/ 下面（见 control.go），
//     整合包的覆盖目录、交接包进出、模组上传都不收也不带。
//   - 导入待决清单.txt：给人看的那份，在服务器文件夹最外层，内容跟着 json 变（也是控制文件，别人给的不收）。
//
// 读回来的 json 是不可信的输入：服务器文件夹可能是接管来的、从交接包导入的，文件可能被人改过。
// 所以读的时候只把路径、原因、校验值、候选地址、跳过没有当数据，别的（站点名、可用的处理方式）一律重算；
// 处置时更不信它写的落点，每一次都重新走 pendingDestination（九个目录的白名单、真实路径、控制文件、当前世界），
// 信任下载照旧只认 https、公网、带校验值、人确认过的站点（pendingTrustDownload）。

const (
	// PendingListFile 没装上的文件的清单（相对服务器文件夹）
	PendingListFile = ".minedeck/import-pending.json"
	// PendingReportFile 给人看的那份
	PendingReportFile = "导入待决清单.txt"

	pendingFileMax  = 1 << 20 // 清单文件最大 1 MiB：正常的整合包几百项也就几十 KB
	pendingItemsMax = 2000
	pendingPathMax  = 512
	pendingNoteMax  = 400
	pendingURLMax   = 2000
	pendingURLsMax  = 8
)

var (
	pendingIDRe = regexp.MustCompile(`^p[1-9][0-9]{0,5}$`)
	sha1HexRe   = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	// pendingReasons 认得的原因；别的一律当成说不清的原因（只能跳过或用本地文件代替）
	pendingReasons = map[string]bool{modpack.ReasonHost: true, modpack.ReasonNoHash: true, modpack.ReasonBadURL: true, "hash": true, "cf": true}
)

// pendingRecord 清单里的一项：tasks.PendingItem 加上「跳过了没有」。跳过的照样留在清单里（还没装上），以后还能改成别的处理。
type pendingRecord struct {
	tasks.PendingItem
	Skipped bool `json:"skipped,omitempty"`
}

// pendingList 一台服务器的清单文件。
type pendingList struct {
	Version int             `json:"version"`
	Pack    string          `json:"pack,omitempty"` // 哪个整合包导入时留下的（给人看）
	At      time.Time       `json:"at"`             // 导入的时间
	Items   []pendingRecord `json:"items"`
}

// ImportPendingView 一台服务器上整合包导入时没装上的文件（接口 GET /api/instances/{name}/import-pending 的返回）。
// pending 是还没处理的，skipped 是跳过了的（也没装上，还能改成用本地文件代替或信任下载）。
type ImportPendingView struct {
	Inst    string              `json:"inst"`
	Pack    string              `json:"pack,omitempty"`
	At      string              `json:"at,omitempty"`
	Pending []tasks.PendingItem `json:"pending"`
	Skipped []tasks.PendingItem `json:"skipped"`
}

func (l *pendingList) view(name string) ImportPendingView {
	v := ImportPendingView{Inst: name, Pending: []tasks.PendingItem{}, Skipped: []tasks.PendingItem{}}
	if l == nil {
		return v
	}
	v.Pack = l.Pack
	if !l.At.IsZero() {
		v.At = l.At.Format(time.RFC3339)
	}
	for _, r := range l.Items {
		if r.Skipped {
			v.Skipped = append(v.Skipped, r.PendingItem)
		} else {
			v.Pending = append(v.Pending, r.PendingItem)
		}
	}
	return v
}

// open 还没处理（没跳过）的那些，任务上的待决清单（Snapshot.Pending）是它的镜像。
func (l *pendingList) open() []tasks.PendingItem {
	out := []tasks.PendingItem{}
	if l == nil {
		return out
	}
	for _, r := range l.Items {
		if !r.Skipped {
			out = append(out, r.PendingItem)
		}
	}
	return out
}

func (l *pendingList) counts() (open, skipped int) {
	if l == nil {
		return 0, 0
	}
	for _, r := range l.Items {
		if r.Skipped {
			skipped++
		} else {
			open++
		}
	}
	return
}

// clipRunes 截到 n 个字符（按字符不按字节，不切坏汉字）。
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// httpsURLs 只留 https、有主机名的地址（别的地址任何情况下都不会拿来下载，留着只会被界面当链接画出来）。
func httpsURLs(urls []string) []string {
	var out []string
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > pendingURLMax {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			continue
		}
		out = append(out, raw)
		if len(out) == pendingURLsMax {
			break
		}
	}
	return out
}

// pendingOptions 按原因定可用的处理方式（不信清单里写的）：只有 reason=host、有 https 候选地址的才能信任下载。
// 跳过了的不再列「跳过」。
func pendingOptions(it tasks.PendingItem, skipped bool) []string {
	var opts []string
	if it.Reason == modpack.ReasonHost && len(it.URLs) > 0 {
		opts = []string{"local", "skip", "trust"}
	} else {
		opts = []string{"skip", "local"}
	}
	if skipped {
		keep := opts[:0:0]
		for _, o := range opts {
			if o != "skip" {
				keep = append(keep, o)
			}
		}
		opts = keep
	}
	return opts
}

// sanitize 把读回来的清单整理成能放心拿去显示、拿去比对的样子（见文件开头）。编号保持原样（界面拿着编号来处置，
// 读一次变一次就会认错项），不合规的、重复的才另给一个，给法只看文件内容，同一份文件每次读出来都一样。
func (l *pendingList) sanitize() {
	if len(l.Items) > pendingItemsMax {
		l.Items = l.Items[:pendingItemsMax]
	}
	l.Pack = clipRunes(strings.TrimSpace(l.Pack), 120)
	seen := map[string]bool{}
	maxN := 0
	for _, r := range l.Items {
		if pendingIDRe.MatchString(r.ID) && !seen[r.ID] {
			seen[r.ID] = true
			var n int
			fmt.Sscanf(r.ID[1:], "%d", &n)
			if n > maxN {
				maxN = n
			}
		}
	}
	used := map[string]bool{}
	out := l.Items[:0:0]
	for _, r := range l.Items {
		it := r.PendingItem
		it.Path = clipRunes(strings.TrimSpace(it.Path), pendingPathMax)
		if it.Path == "" {
			continue
		}
		if !pendingIDRe.MatchString(it.ID) || used[it.ID] {
			maxN++
			it.ID = fmt.Sprintf("p%d", maxN)
		}
		used[it.ID] = true
		if !pendingReasons[it.Reason] {
			it.Reason = ""
		}
		it.URLs = httpsURLs(it.URLs)
		it.Host = strings.Join(modpack.HostOf(it.URLs), " ")
		if !sha1HexRe.MatchString(it.SHA1) {
			it.SHA1 = ""
		}
		it.SHA1 = strings.ToLower(it.SHA1)
		if it.Size < 0 {
			it.Size = 0
		}
		it.Note = clipRunes(strings.TrimSpace(it.Note), pendingNoteMax)
		if it.Note == "" {
			it.Note = modpack.PendingNote(it.Reason)
		}
		it.Options = pendingOptions(it, r.Skipped)
		out = append(out, pendingRecord{PendingItem: it, Skipped: r.Skipped})
	}
	l.Items = out
	l.Version = 1
}

// pendingListPath 清单文件的位置，并确认它真实的落点在服务器文件夹里面：接管来的文件夹里 .minedeck 可以是指到外面的
// 目录联接，顺着它读写就跑到别处去了。查不清的也不放行。
func pendingListPath(dir string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(PendingListFile))
	root, err := RealPathStrict(dir)
	if err != nil {
		return "", err
	}
	real, err := RealPathStrict(p)
	if err != nil {
		return "", fmt.Errorf("查不清放「没装上的文件」清单的文件夹实际在哪（可能是断掉的目录联接）")
	}
	if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) || !InsideReal(root, real) {
		return "", fmt.Errorf("放「没装上的文件」清单的文件夹（.minedeck）指到了服务器文件夹外面，没有读写")
	}
	return p, nil
}

// readPendingList 读回一台服务器的清单。没有清单文件返回 nil, nil。
func readPendingList(dir string) (*pendingList, error) {
	p, err := pendingListPath(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读不了这台服务器的「没装上的文件」清单：%w", err)
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && !st.Mode().IsRegular() {
		return nil, fmt.Errorf("「没装上的文件」清单不是普通文件，没有读")
	}
	b, err := io.ReadAll(io.LimitReader(f, pendingFileMax+1))
	if err != nil {
		return nil, fmt.Errorf("读不了这台服务器的「没装上的文件」清单：%w", err)
	}
	if len(b) > pendingFileMax {
		return nil, fmt.Errorf("「没装上的文件」清单太大（超过 1 MB），不像是 MineDeck 写的，没有读")
	}
	var l pendingList
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &l); err != nil {
		return nil, fmt.Errorf("「没装上的文件」清单的内容坏了，读不出来（%s）：%v", PendingListFile, err)
	}
	l.sanitize()
	return &l, nil
}

// writePendingList 把清单写回服务器文件夹（先写临时文件再换上去），给人看的 txt 跟着重写；一项都不剩时两份都删掉。
func writePendingList(dir string, l *pendingList) error {
	p, err := pendingListPath(dir)
	if err != nil {
		return err
	}
	if l == nil || len(l.Items) == 0 {
		_ = os.Remove(filepath.Join(dir, PendingReportFile))
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	l.Version = 1
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := atomicfile.Write(p, b, 0o644); err != nil {
		return err
	}
	writePendingReport(dir, l)
	return nil
}

// writePendingReport 给人看的那份：还没处理的、跳过了的分开列。不写「任务面板」「任务日志」这种程序重开以后找不到的地方，
// 指向这台服务器的详情页（可用性走查 R09）。
func writePendingReport(dir string, l *pendingList) {
	open, skipped := l.counts()
	var sb strings.Builder
	if l.Pack != "" {
		sb.WriteString(fmt.Sprintf("这台服务器导入整合包「%s」时，下面这些文件没能自动装上。\r\n", l.Pack))
	} else {
		sb.WriteString("这台服务器导入整合包时，下面这些文件没能自动装上。\r\n")
	}
	sb.WriteString("到 MineDeck 里打开这台服务器的详情页处理：可以用本地已有的文件代替、跳过，或者（下载地址在别的站点的）确认站点后下载。\r\n")
	sb.WriteString("跳过的也还能再处理。这份文件是 MineDeck 写的，处理以后会跟着更新。\r\n")
	write := func(title string, skip bool) {
		sb.WriteString("\r\n" + title + "\r\n")
		for _, r := range l.Items {
			if r.Skipped != skip {
				continue
			}
			mark := ""
			if skip {
				mark = "【已跳过】"
			}
			sb.WriteString(fmt.Sprintf("%s%s（%s）\r\n", mark, r.Path, r.Note))
			for _, u := range r.URLs {
				sb.WriteString("  " + u + "\r\n")
			}
		}
	}
	if open > 0 {
		write(fmt.Sprintf("还没处理的（%d 个）：", open), false)
	}
	if skipped > 0 {
		write(fmt.Sprintf("已跳过的（%d 个，没有装上）：", skipped), true)
	}
	if err := atomicfile.Write(filepath.Join(dir, PendingReportFile), []byte(sb.String()), 0o644); err != nil {
		log.Printf("没写成「%s」：%v", PendingReportFile, err)
	}
}

// pendingWarning 任务结果页上那块「N 个文件待处理」：跟着还没处理的个数变，处理完了就去掉（可用性走查 U08 末尾）。
func pendingWarning(n int) tasks.Warning {
	return tasks.Warning{
		Kind:  "import-pending",
		Title: fmt.Sprintf("%d 个文件待处理", n),
		Note:  "这些文件没法自动下载。逐个处理导入才算补完整：用本地已有的文件代替、跳过，或（地址在别的站点的）确认站点后下载。跳过的以后还能在这台服务器的详情页里再处理。",
	}
}

// loadPending 一台服务器现在的清单。清单文件不在、而这次运行里的导入任务还记着待决项（比如收尾时文件没写成）时，
// 用任务上的那份（这次运行自己生成的，可信）。
func (m *Manager) loadPending(i *Instance) (*pendingList, error) {
	l, err := readPendingList(i.Dir)
	if err != nil || l != nil {
		return l, err
	}
	for _, id := range m.pendingTasksOf(i) {
		t := m.Tasks.Get(id)
		if t == nil {
			continue
		}
		if items := t.PendingItems(); len(items) > 0 {
			l = &pendingList{Version: 1}
			for _, it := range items {
				l.Items = append(l.Items, pendingRecord{PendingItem: it})
			}
			return l, nil
		}
	}
	return nil, nil
}

// ImportPendingFiles 只读服务器文件夹里存的那份清单（导出交接包时说一声缺了哪些用）。没有清单两个列表都是空的。
func (i *Instance) ImportPendingFiles() (ImportPendingView, error) {
	l, err := readPendingList(i.Dir)
	return l.view(i.Name), err
}

// pendingTasksOf 这次运行里，待决清单记在这台服务器上的导入任务。
func (m *Manager) pendingTasksOf(i *Instance) []string {
	m.pendMu.Lock()
	defer m.pendMu.Unlock()
	var ids []string
	for id, ref := range m.pendingInst {
		if ref.name == i.Name && ref.created.Equal(i.CreatedAt) {
			ids = append(ids, id)
		}
	}
	return ids
}

// syncPendingTasks 清单变了以后，这次运行里对应的导入任务跟着变：任务上的待决清单只列还没处理的，
// 「N 个文件待处理」那块告警跟着改个数，都处理完了（跳过的不算没处理）就去掉，任务和服务器的对应也解开。
func (m *Manager) syncPendingTasks(i *Instance, l *pendingList) {
	open := l.open()
	for _, id := range m.pendingTasksOf(i) {
		t := m.Tasks.Get(id)
		if t == nil {
			continue
		}
		t.SetPending(open)
		if len(open) == 0 {
			t.RemoveWarning("import-pending")
			m.forgetPending(id)
			t.Logf("没装上的文件都处理完了")
		} else {
			t.SetWarning(pendingWarning(len(open)))
		}
	}
}

// pendingLog 处置的记录写进这次运行里对应的导入任务的日志（程序重开以后没有任务，就不写了）。
func (m *Manager) pendingLog(i *Instance, format string, a ...any) {
	for _, id := range m.pendingTasksOf(i) {
		if t := m.Tasks.Get(id); t != nil {
			t.Logf(format, a...)
		}
	}
}

// ImportPending 一台服务器上整合包导入时没装上的文件（包括跳过的）。没有的话两个列表都是空的，不报错。
func (m *Manager) ImportPending(name string) (ImportPendingView, error) {
	i, err := m.Get(name)
	if err != nil {
		return ImportPendingView{}, err
	}
	l, err := m.loadPending(i)
	if err != nil {
		return ImportPendingView{}, err
	}
	return l.view(i.Name), nil
}

// ResolveInstancePending 按服务器处置整合包导入时没装上的文件：程序重开以后、没有导入任务了也能处理（可用性走查 R09）。
// 处置的规矩和 ResolveImportPending 一样，返回处置结果和处置以后的清单。权限约束在操作层（仅人工 + 仅本机）。
func (m *Manager) ResolveInstancePending(ctx context.Context, name string, acts []PendingAction) ([]ResolveOutcome, ImportPendingView, error) {
	if len(acts) == 0 {
		return nil, ImportPendingView{}, fmt.Errorf("没有要处理的项")
	}
	if !m.resolveMu.TryLock() {
		return nil, ImportPendingView{}, fmt.Errorf("上一批待处理的文件还在处理（可能在下载），等它完成再试")
	}
	defer m.resolveMu.Unlock()
	i, err := m.Get(name)
	if err != nil {
		return nil, ImportPendingView{}, err
	}
	out, l, err := m.resolvePending(ctx, i, acts)
	return out, l.view(i.Name), err
}
