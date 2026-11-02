package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// Feature 用 AI 的功能：用量按它分开记，每日额度也按它分开设（§4.3）。
type Feature string

const (
	FeatAssistant Feature = "assistant" // 应用内助手（局域网登录的人和本机共用这一份额度）
	FeatGameChat  Feature = "gamechat"  // 游戏内聊天 AI
	FeatCompanion Feature = "companion" // AI 伙伴（第七阶段）
)

// QuotaFeatures 可以设每日额度的功能。AI 伙伴（第七阶段）：玩家在聊天里叫着伙伴的名字说话时记在它名下，任务结束的通知不调用 AI。
var QuotaFeatures = []Feature{FeatAssistant, FeatGameChat, FeatCompanion}

func (f Feature) valid() bool { return f == FeatAssistant || f == FeatGameChat || f == FeatCompanion }

// Cost 一次请求（或一段时间合计）的估算费用。币种只有两种：USD（公开价格和自填的美元价）、CNY（自填的人民币价）。
// 不做汇率换算，合计按币种分开。
type Cost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Source   string  `json:"source"` // litellm / custom / local（本机模型，不计费）
}

// Totals 一段时间的合计。Tokens 是额度按的那个数（输入 + 输出 + 缓存命中 + 缓存写入）。
type Totals struct {
	Requests   int64              `json:"requests"`
	Input      int64              `json:"input"`
	Output     int64              `json:"output"`
	CacheRead  int64              `json:"cacheRead"`
	CacheWrite int64              `json:"cacheWrite"`
	Reasoning  int64              `json:"reasoning"`
	Tokens     int64              `json:"tokens"`
	Estimated  int64              `json:"estimated,omitempty"` // 其中几次的用量是按字数估的
	Unpriced   int64              `json:"unpriced,omitempty"`  // 其中几次查不到价格（只记了 token）
	Cost       map[string]float64 `json:"cost,omitempty"`      // 币种 → 金额（估）
}

func (t *Totals) add(u Usage, c *Cost) {
	t.Requests++
	t.Input += u.Input
	t.Output += u.Output
	t.CacheRead += u.CacheRead
	t.CacheWrite += u.CacheWrite
	t.Reasoning += u.Reasoning
	t.Tokens += u.Total()
	if u.Estimated {
		t.Estimated++
	}
	if c != nil && c.Source == "local" {
		return // 本机模型不花钱：不算「查不到价格」，也不记金额
	}
	if c == nil {
		t.Unpriced++
		return
	}
	if t.Cost == nil {
		t.Cost = map[string]float64{}
	}
	t.Cost[c.Currency] += c.Amount
}

func (t *Totals) merge(o *Totals) {
	t.Requests += o.Requests
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Reasoning += o.Reasoning
	t.Tokens += o.Tokens
	t.Estimated += o.Estimated
	t.Unpriced += o.Unpriced
	for k, v := range o.Cost {
		if t.Cost == nil {
			t.Cost = map[string]float64{}
		}
		t.Cost[k] += v
	}
}

// QuotaError 今天的额度用完了：这项功能暂停到第二天（本地时间 0 点）。
type QuotaError struct {
	Feature Feature
	Limit   int64
	Used    int64
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("%s今天的额度已用完（已用 %d / %d token），明天 0 点恢复；可以在「设置 → AI」里调高额度", featureName(e.Feature), e.Used, e.Limit)
}

func featureName(f Feature) string {
	switch f {
	case FeatAssistant:
		return "应用内助手"
	case FeatGameChat:
		return "游戏内 AI"
	case FeatCompanion:
		return "AI 伙伴"
	}
	return string(f)
}

const ledgerKeepDays = 400 // 留一年多，本月和去年同月都查得到

type ledgerFile struct {
	V    int                            `json:"v"`
	Days map[string]map[Feature]*Totals `json:"days"` // "2026-10-28" → 功能 → 合计
}

// Ledger 用量记账：<目录>/usage.json，按天、按功能记。日期按本地时间。
type Ledger struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	loaded bool
	days   map[string]map[Feature]*Totals
}

func newLedger(path string, now func() time.Time) *Ledger {
	return &Ledger{path: path, now: now}
}

func dayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

func (l *Ledger) loadLocked() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.days = map[string]map[Feature]*Totals{}
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	var f ledgerFile
	if json.Unmarshal(b, &f) != nil || f.V != 1 {
		return
	}
	for d, m := range f.Days {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			continue
		}
		clean := map[Feature]*Totals{}
		for k, t := range m {
			if k.valid() && t != nil && t.Tokens >= 0 {
				clean[k] = t
			}
		}
		l.days[d] = clean
	}
}

// Add 记一次请求。c 为 nil 表示查不到价格（只记 token）。
func (l *Ledger) Add(f Feature, u Usage, c *Cost) {
	if c != nil && (math.IsNaN(c.Amount) || c.Amount < 0) {
		c = nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	d := dayKey(l.now())
	if l.days[d] == nil {
		l.days[d] = map[Feature]*Totals{}
	}
	t := l.days[d][f]
	if t == nil {
		t = &Totals{}
		l.days[d][f] = t
	}
	t.add(u, c)
	l.pruneLocked()
	l.saveLocked()
}

func (l *Ledger) pruneLocked() {
	cut := dayKey(l.now().AddDate(0, 0, -ledgerKeepDays))
	for d := range l.days {
		if d < cut {
			delete(l.days, d)
		}
	}
}

func (l *Ledger) saveLocked() {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if enc.Encode(ledgerFile{V: 1, Days: l.days}) == nil {
		_ = writeFileAtomic(l.path, buf.Bytes())
	}
}

// Today 今天这项功能的合计。
func (l *Ledger) Today(f Feature) Totals {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	if t := l.days[dayKey(l.now())][f]; t != nil {
		return clone(t)
	}
	return Totals{}
}

// Month 本月（本地时间）这项功能的合计。
func (l *Ledger) Month(f Feature) Totals {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	prefix := l.now().Local().Format("2006-01-")
	var keys []string
	for d := range l.days {
		if len(d) == 10 && d[:8] == prefix {
			keys = append(keys, d)
		}
	}
	sort.Strings(keys)
	var out Totals
	for _, d := range keys {
		if t := l.days[d][f]; t != nil {
			out.merge(t)
		}
	}
	return out
}

func clone(t *Totals) Totals {
	c := *t
	if t.Cost != nil {
		c.Cost = map[string]float64{}
		for k, v := range t.Cost {
			c.Cost[k] = v
		}
	}
	return c
}

// Check 额度检查：limit 为 0 表示不限；今天已用的 token 达到上限就报 *QuotaError。
func (l *Ledger) Check(f Feature, limit int64) error {
	if limit <= 0 {
		return nil
	}
	if used := l.Today(f).Tokens; used >= limit {
		return &QuotaError{Feature: f, Limit: limit, Used: used}
	}
	return nil
}
