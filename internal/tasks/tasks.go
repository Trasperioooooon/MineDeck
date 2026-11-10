// Package tasks 提供后台任务（如实例创建）的进度跟踪，供前端轮询。
package tasks

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pending | running | done | error
}

// WarnItem 一条可点击的告警条目（如需手动补装的模组）。
type WarnItem struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Warning 任务完成后需在结果页醒目呈现的结构化提示（区别于滚动日志）。
type Warning struct {
	Kind  string     `json:"kind"`  // 如 cf-unresolved
	Title string     `json:"title"` // 如「3 个模组需手动补装」
	Note  string     `json:"note,omitempty"`
	Items []WarnItem `json:"items,omitempty"`
}

// NetRetry 当前这一步里下载超时、失败重试的情况：部署页据此在进度旁提示「下载慢或者在重试」（测试报告 B-01）。
type NetRetry struct {
	Count int    `json:"count"`          // 这一步里一共出过几次
	Host  string `json:"host,omitempty"` // 最近一次出问题的地址（只到主机名）
	What  string `json:"what"`           // 最近一次是什么问题，如「连接超时」「下载失败」
	Ago   int    `json:"ago"`            // 最近一次是几秒前
}

// PendingItem 整合包导入里一个「无法自动下载、等用户处置」的文件（处置见 inst.ResolveImportPending）。
// 可用处置：skip 跳过（记进结果摘要）、local 保留本地已有文件 / 手动指定本地文件、
// trust 信任自定义站点后下载（只有 reason=host 的项有）；处置完成后项从清单里消失，
// 结果记进任务日志和实例目录的报告文件，待决结束后任务仍是已完成。
type PendingItem struct {
	ID      string   `json:"id"`             // 稳定编号（p1、p2…），处置按它认项
	Path    string   `json:"path"`           // 相对实例目录（如 mods/xxx.jar）
	Reason  string   `json:"reason"`         // host | nohash | badurl | hash | cf
	Note    string   `json:"note,omitempty"` // 给用户看的一句话（为什么没法自动下载）
	Host    string   `json:"host,omitempty"` // reason=host 时要确认的站点（规范主机名，多个空格分隔）
	Size    int64    `json:"size,omitempty"` // 整合包声明的文件大小（可能没有）
	URLs    []string `json:"urls,omitempty"` // reason=host：候选地址（信任站点后从中下载）
	SHA1    string   `json:"sha1,omitempty"` // 下载要过的校验值
	Options []string `json:"options"`        // 可用处置：skip、local、trust 的子集
}

type Snapshot struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Kind     string        `json:"kind,omitempty"` // 空 = 新建 / 导入（部署）；adopt = 接管已有服务器；java = 换 Java。界面按它换说法
	Steps    []Step        `json:"steps"`
	Log      []string      `json:"log"`
	Label    string        `json:"label"`
	Done     int64         `json:"done"`
	Total    int64         `json:"total"`
	Ended    bool          `json:"ended"`
	Err      string        `json:"error,omitempty"`
	Result   string        `json:"result,omitempty"`
	Warnings []Warning     `json:"warnings,omitempty"`
	Pending  []PendingItem `json:"pending,omitempty"` // 整合包导入的待决文件（已结束的任务也可能有）：逐项列出、待用户处置
	// Net 最近一次出问题以后还没有新的下载进度时才有（下载又动起来、换了步骤就清掉）
	Net *NetRetry `json:"net,omitempty"`
}

// Summary 任务列表里的一项：只带认出任务、看出状态要用的字段，和 Snapshot 里的同名字段含义相同。
// 步骤、日志、告警明细不在这里：一个任务的日志最多 400 行，任务又不清理，界面每隔几秒就查一次列表，
// AI 助手也能查（task.list），全带上的话列表会越来越大。要看明细再查单个任务。
type Summary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind,omitempty"` // 同 Snapshot.Kind：空 = 部署，adopt = 接管已有服务器，java = 换 Java
	Label        string `json:"label"`
	Done         int64  `json:"done"`
	Total        int64  `json:"total"`
	Ended        bool   `json:"ended"`
	Err          string `json:"error,omitempty"`
	Result       string `json:"result,omitempty"`
	WarningCount int    `json:"warningCount"` // 告警条数，明细在 Snapshot.Warnings
	PendingCount int    `json:"pendingCount"` // 待处置的文件数，明细在 Snapshot.Pending（只有整合包导入的任务会有）
}

type Task struct {
	id       string
	title    string
	kind     string
	mu       sync.Mutex
	steps    []Step
	log      []string
	logCut   int // 日志太长时从中间丢掉了多少行（见 appendLog）
	label    string
	done     int64
	total    int64
	ended    bool
	err      string
	result   string
	warnings []Warning
	pending  []PendingItem
	cancel   context.CancelFunc

	netCount int       // 这一步里出过几次网络问题
	netHost  string    // 最近一次的地址
	netWhat  string    // 最近一次是什么问题
	netAt    time.Time // 最近一次的时间；零值表示之后已经有了新进度（或者还没出过问题）
}

type Manager struct {
	mu  sync.Mutex
	seq int
	m   map[string]*Task
}

func NewManager() *Manager { return &Manager{m: map[string]*Task{}} }

// New 创建任务并返回其生命周期 context。
func (mg *Manager) New(title string, steps []string) (*Task, context.Context) {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	mg.seq++
	t := &Task{id: "t" + strconv.Itoa(mg.seq), title: title}
	for _, s := range steps {
		t.steps = append(t.steps, Step{Name: s, Status: "pending"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	mg.m[t.id] = t
	return t, ctx
}

func (mg *Manager) Get(id string) *Task {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	return mg.m[id]
}

// List 按创建时间从新到旧返回本次运行的任务摘要，切页或刷新后仍能找回进度入口；步骤、日志和告警明细用 Get 查单个任务。
func (mg *Manager) List() []Summary {
	mg.mu.Lock()
	ts := make([]*Task, 0, len(mg.m))
	for n := mg.seq; n > 0; n-- {
		// 现在任务建了就不删；以后要是清理旧任务，编号中间会缺，缺的跳过
		if t := mg.m["t"+strconv.Itoa(n)]; t != nil {
			ts = append(ts, t)
		}
	}
	mg.mu.Unlock()
	out := make([]Summary, 0, len(ts)) // 没有任务时也是 []，不是 null
	for _, t := range ts {
		out = append(out, t.summary())
	}
	return out
}

func (t *Task) summary() Summary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Summary{
		ID: t.id, Title: t.title, Kind: t.kind, Label: t.label,
		Done: t.done, Total: t.total,
		Ended: t.ended, Err: t.err, Result: t.result,
		WarningCount: len(t.warnings),
		PendingCount: len(t.pending),
	}
}

func (t *Task) ID() string { return t.id }

// SetKind 标明这是哪一类任务（见 Snapshot.Kind）；不设就是部署任务。
func (t *Task) SetKind(kind string) {
	t.mu.Lock()
	t.kind = kind
	t.mu.Unlock()
}

// StartStep 将第 i 步标记为运行中（前序步骤自动标记完成）。
func (t *Task) StartStep(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for j := range t.steps {
		if j < i && t.steps[j].Status == "running" {
			t.steps[j].Status = "done"
		}
	}
	if i >= 0 && i < len(t.steps) {
		t.steps[i].Status = "running"
	}
	t.label, t.done, t.total = "", 0, 0
	t.netCount, t.netHost, t.netWhat, t.netAt = 0, "", "", time.Time{}
}

// NetRetry 记一次下载超时或失败（之后会自动重试）：host 是出问题的地址（可以带路径，只留主机名），what 是什么问题。
func (t *Task) NetRetry(host, what string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}
	t.netCount++
	t.netHost, t.netWhat, t.netAt = host, what, time.Now()
}

// 一个任务的日志最多留 logMax 行。超出时不是只留最后的：开头的 logHead 行是任务自己说的话
// （这是什么包、里面有什么、哪些没带过来、用哪个 Java），后面常常跟着官方安装器几百行的输出，
// 只留末尾的话，这些说明就被挤掉了，而收尾那句「没带过来的东西见上面几行」还指着它们。
// 所以开头的留着，从中间往外丢，丢了多少行在快照里标一行。
const (
	logHead = 40
	logMax  = 400
)

func (t *Task) Logf(format string, a ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.appendLog(fmt.Sprintf(format, a...))
}

// appendLog 记一行日志，超出上限时丢掉开头那几十行之后最早的。调用方持有 t.mu。
func (t *Task) appendLog(line string) {
	t.log = append(t.log, line)
	if over := len(t.log) - logMax; over > 0 {
		t.log = append(t.log[:logHead], t.log[logHead+over:]...)
		t.logCut += over
	}
}

// logLines 给快照用的日志：中间丢过行的话，在断开的地方插一行说明。调用方持有 t.mu。
func (t *Task) logLines() []string {
	if t.logCut == 0 {
		return append([]string(nil), t.log...)
	}
	out := make([]string, 0, len(t.log)+1)
	out = append(out, t.log[:logHead]...)
	out = append(out, fmt.Sprintf("……（中间的 %d 行太多，没有保留）……", t.logCut))
	return append(out, t.log[logHead:]...)
}

// AddWarning 追加一条结构化告警，供结果页醒目呈现。
func (t *Task) AddWarning(w Warning) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warnings = append(t.warnings, w)
}

// SetPending 设定任务的待决文件清单（整合包导入收尾时一次设全，之后只经 ResolvePending 减少）。
func (t *Task) SetPending(items []PendingItem) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = items
}

// PendingItems 返回待决清单的副本。
func (t *Task) PendingItems() []PendingItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]PendingItem{}, t.pending...)
}

// ResolvePending 把 ids 里的项从待决清单里移走（处置已定：跳过 / 保留本地 / 信任下载），
// 返回移走的那些。处置失败、重试的机会由调用方自己决定是否把项还回去；这里只管清单本身。
func (t *Task) ResolvePending(ids []string) []PendingItem {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var removed []PendingItem
	keep := t.pending[:0:0]
	for _, it := range t.pending {
		if set[it.ID] {
			removed = append(removed, it)
			delete(set, it.ID)
			continue
		}
		keep = append(keep, it)
	}
	t.pending = keep
	return removed
}

// ProgressFn 返回带标签的进度回调。
func (t *Task) ProgressFn(label string) func(done, total int64) {
	return func(done, total int64) {
		t.mu.Lock()
		t.label, t.done, t.total = label, done, total
		t.netAt = time.Time{} // 下载又动起来了：不再提示「在重试」（次数留着，再出问题接着数）
		t.mu.Unlock()
	}
}

func (t *Task) Finish(result string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for j := range t.steps {
		if t.steps[j].Status == "running" || t.steps[j].Status == "pending" {
			t.steps[j].Status = "done"
		}
	}
	t.ended, t.result = true, result
	t.cancel()
}

func (t *Task) Fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for j := range t.steps {
		if t.steps[j].Status == "running" {
			t.steps[j].Status = "error"
		}
	}
	t.ended = true
	t.err = err.Error()
	t.appendLog("❌ " + err.Error())
	t.cancel()
}

func (t *Task) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		ID: t.id, Title: t.title, Kind: t.kind, Label: t.label,
		Done: t.done, Total: t.total,
		Ended: t.ended, Err: t.err, Result: t.result,
	}
	s.Steps = append(s.Steps, t.steps...)
	s.Log = t.logLines()
	s.Warnings = append(s.Warnings, t.warnings...)
	s.Pending = append(s.Pending, t.pending...)
	if !t.netAt.IsZero() && !t.ended {
		s.Net = &NetRetry{Count: t.netCount, Host: t.netHost, What: t.netWhat, Ago: int(time.Since(t.netAt).Seconds())}
	}
	// 空切片显式化为 []：任务刚创建、尚无日志时 nil 切片会被 JSON 编码成 null，
	// 前端对其做 .map() 会抛错。此处从数据源头兜底，前端另有 (x||[]) 双保险。
	if s.Steps == nil {
		s.Steps = []Step{}
	}
	if s.Log == nil {
		s.Log = []string{}
	}
	return s
}
