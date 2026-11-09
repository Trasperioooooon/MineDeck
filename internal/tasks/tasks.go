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

type Snapshot struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Steps    []Step    `json:"steps"`
	Log      []string  `json:"log"`
	Label    string    `json:"label"`
	Done     int64     `json:"done"`
	Total    int64     `json:"total"`
	Ended    bool      `json:"ended"`
	Err      string    `json:"error,omitempty"`
	Result   string    `json:"result,omitempty"`
	Warnings []Warning `json:"warnings,omitempty"`
	// Net 最近一次出问题以后还没有新的下载进度时才有（下载又动起来、换了步骤就清掉）
	Net *NetRetry `json:"net,omitempty"`
}

// Summary 任务列表里的一项：只带认出任务、看出状态要用的字段，和 Snapshot 里的同名字段含义相同。
// 步骤、日志、告警明细不在这里：一个任务的日志最多 400 行，任务又不清理，界面每隔几秒就查一次列表，
// AI 助手也能查（task.list），全带上的话列表会越来越大。要看明细再查单个任务。
type Summary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Label        string `json:"label"`
	Done         int64  `json:"done"`
	Total        int64  `json:"total"`
	Ended        bool   `json:"ended"`
	Err          string `json:"error,omitempty"`
	Result       string `json:"result,omitempty"`
	WarningCount int    `json:"warningCount"` // 告警条数，明细在 Snapshot.Warnings
}

type Task struct {
	id       string
	title    string
	mu       sync.Mutex
	steps    []Step
	log      []string
	label    string
	done     int64
	total    int64
	ended    bool
	err      string
	result   string
	warnings []Warning
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
		ID: t.id, Title: t.title, Label: t.label,
		Done: t.done, Total: t.total,
		Ended: t.ended, Err: t.err, Result: t.result,
		WarningCount: len(t.warnings),
	}
}

func (t *Task) ID() string { return t.id }

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

func (t *Task) Logf(format string, a ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.log = append(t.log, fmt.Sprintf(format, a...))
	if len(t.log) > 400 {
		t.log = t.log[len(t.log)-400:]
	}
}

// AddWarning 追加一条结构化告警，供结果页醒目呈现。
func (t *Task) AddWarning(w Warning) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warnings = append(t.warnings, w)
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
	t.log = append(t.log, "❌ "+err.Error())
	t.cancel()
}

func (t *Task) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		ID: t.id, Title: t.title, Label: t.label,
		Done: t.done, Total: t.total,
		Ended: t.ended, Err: t.err, Result: t.result,
	}
	s.Steps = append(s.Steps, t.steps...)
	s.Log = append(s.Log, t.log...)
	s.Warnings = append(s.Warnings, t.warnings...)
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
