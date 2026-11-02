package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 费用估算（§4.4）：MineDeck 自己不写任何价格，运行时下载 LiteLLM 的公开价格文件（MIT 许可，只在运行时下载、
// 不随 exe 分发）。只在配置了 AI 服务商之后才取，只配了 Ollama 的不取；整份下载，不会把「在用哪个模型」发出去。
//
// 下载经 GitHub 加速镜像（dl.GHMirrors），取到的内容当不可信数据：限制大小、只按 JSON 解析、只留对话模型的
// 几个价格字段，负数、非数字、每个 token 超过 1 美元的值一律丢掉，文件里的文字（来源网址、注释）一概不用。

// PriceURL LiteLLM 仓库根目录的价格文件。2026-10-28 在云端核实：直连和经 ghfast.top、gh-proxy.com 转发都取得到，
// 约 2.97 MB（国内能不能连上这两个镜像要实测）。
const PriceURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

const (
	priceMaxBytes   = 10 << 20           // 下载上限
	priceMaxValue   = 1.0                // 每个 token 超过 1 美元的当坏数据丢掉
	priceRefresh    = 7 * 24 * time.Hour // 每 7 天最多自动刷新一次
	priceRetryAfter = 6 * time.Hour      // 自动刷新失败后，至少隔这么久再试
	priceMinEntries = 20                 // 有效条目太少，多半是错误页或被截断，不用它
	priceMaxEntries = 50000
)

// Price 一个模型的单价（美元 / token）。可选的几项为空时按输入价算（缓存）或按输出价算（思考）。
type Price struct {
	Provider   string   `json:"p"`
	Input      float64  `json:"i"`
	Output     float64  `json:"o"`
	CacheRead  *float64 `json:"cr,omitempty"`
	CacheWrite *float64 `json:"cw,omitempty"`
	Reasoning  *float64 `json:"r,omitempty"` // 思考 token 有单独单价时才有
	Tiers      []Tier   `json:"t,omitempty"` // 按输入长度分档，From 从小到大
}

// Tier 输入达到 From 个 token 之后用的单价。
type Tier struct {
	From       int64    `json:"f"`
	Input      float64  `json:"i"`
	Output     float64  `json:"o"`
	CacheRead  *float64 `json:"cr,omitempty"`
	CacheWrite *float64 `json:"cw,omitempty"`
	Reasoning  *float64 `json:"r,omitempty"`
}

// tier 按这次请求的输入长度选档（超出最后一档的上限也按最后一档算）。
func (p Price) tier(prompt int64) Tier {
	t := Tier{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite, Reasoning: p.Reasoning}
	for _, x := range p.Tiers {
		if prompt >= x.From {
			t = x
		}
	}
	return t
}

// Cost 按单价算一次请求的费用：输入、输出、缓存命中、缓存写入各乘各的单价；思考 token 有单独单价才单独算
// （它已经算在输出里，不重复算）。
func (p Price) Cost(u Usage) float64 {
	t := p.tier(u.Prompt())
	or := func(v *float64, d float64) float64 {
		if v == nil {
			return d
		}
		return *v
	}
	c := float64(u.Input)*t.Input + float64(u.CacheRead)*or(t.CacheRead, t.Input) + float64(u.CacheWrite)*or(t.CacheWrite, t.Input)
	if t.Reasoning != nil && u.Reasoning > 0 && u.Reasoning <= u.Output {
		c += float64(u.Output-u.Reasoning)*t.Output + float64(u.Reasoning)**t.Reasoning
	} else {
		c += float64(u.Output) * t.Output
	}
	return c
}

// ---------- 解析与校验 ----------

var (
	priceKeyRe      = regexp.MustCompile(`^[\x21-\x7e]{1,200}$`)
	priceProviderRe = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)
	aboveRe         = regexp.MustCompile(`^(input_cost_per_token|output_cost_per_token|cache_read_input_token_cost|cache_creation_input_token_cost|output_cost_per_reasoning_token)_above_(\d{1,5})k_tokens$`)
)

// priceNum 价格字段：必须是 JSON 数字（不收字符串），有限、不小于 0、每个 token 不超过 1 美元。
func priceNum(raw json.RawMessage) (float64, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || !(s[0] == '-' || (s[0] >= '0' && s[0] <= '9')) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > priceMaxValue {
		return 0, false
	}
	return f, true
}

func optPrice(raw json.RawMessage) *float64 {
	if len(raw) == 0 {
		return nil
	}
	if f, ok := priceNum(raw); ok {
		return &f
	}
	return nil
}

// ParsePrices 解析 LiteLLM 的价格文件，只留对话模型（mode 为 chat 或 responses）的价格字段。坏条目跳过，
// 坏的可选字段丢掉；有效条目太少时整份不用。
func ParsePrices(b []byte) (map[string]Price, error) {
	if len(b) > priceMaxBytes {
		return nil, fmt.Errorf("价格文件太大（%d 字节）", len(b))
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, fmt.Errorf("价格文件不是合法的 JSON：%w", err)
	}
	if len(top) > priceMaxEntries {
		return nil, fmt.Errorf("价格文件的条目太多（%d）", len(top))
	}
	out := map[string]Price{}
	for key, raw := range top {
		if !priceKeyRe.MatchString(key) || len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if mode := rawText(obj["mode"]); mode != "chat" && mode != "responses" {
			continue
		}
		prov := rawText(obj["litellm_provider"])
		if !priceProviderRe.MatchString(prov) {
			continue
		}
		if p, ok := parseEntry(obj); ok {
			p.Provider = prov
			out[key] = p
		}
	}
	if len(out) < priceMinEntries {
		return nil, fmt.Errorf("价格文件里有效的对话模型太少（%d 个）", len(out))
	}
	return out, nil
}

func parseEntry(obj map[string]json.RawMessage) (Price, bool) {
	var p Price
	in, okIn := priceNum(obj["input_cost_per_token"])
	out, okOut := priceNum(obj["output_cost_per_token"])
	cacheRead := optPrice(obj["cache_read_input_token_cost"])
	if cacheRead == nil {
		cacheRead = optPrice(obj["input_cost_per_token_cache_hit"]) // DeepSeek 的另一种写法
	}
	p.CacheRead = cacheRead
	p.CacheWrite = optPrice(obj["cache_creation_input_token_cost"])
	p.Reasoning = optPrice(obj["output_cost_per_reasoning_token"])
	p.Input, p.Output = in, out

	// 分档写法一：tiered_pricing 数组（通义等），顶层没有单价，从 0 起的那一档就是基础价
	if raw, ok := obj["tiered_pricing"]; ok {
		var arr []map[string]json.RawMessage
		if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 || len(arr) > 16 {
			return p, false
		}
		var tiers []Tier
		for _, t := range arr {
			var rng []json.RawMessage
			if json.Unmarshal(t["range"], &rng) != nil || len(rng) != 2 {
				return p, false
			}
			lo, ok1 := rangeNum(rng[0])
			hi, ok2 := rangeNum(rng[1])
			ti, ok3 := priceNum(t["input_cost_per_token"])
			to, ok4 := priceNum(t["output_cost_per_token"])
			if !ok1 || !ok2 || !ok3 || !ok4 || lo >= hi {
				return p, false
			}
			cr := optPrice(t["cache_read_input_token_cost"])
			cw := optPrice(t["cache_creation_input_token_cost"])
			tiers = append(tiers, Tier{From: lo, Input: ti, Output: to, CacheRead: cr, CacheWrite: cw, Reasoning: optPrice(t["output_cost_per_reasoning_token"])})
		}
		sort.Slice(tiers, func(i, j int) bool { return tiers[i].From < tiers[j].From })
		if tiers[0].From != 0 {
			return p, false
		}
		base := tiers[0]
		p.Input, p.Output = base.Input, base.Output
		if base.CacheRead != nil {
			p.CacheRead = base.CacheRead
		}
		if base.CacheWrite != nil {
			p.CacheWrite = base.CacheWrite
		}
		if base.Reasoning != nil {
			p.Reasoning = base.Reasoning
		}
		for _, t := range tiers[1:] {
			if t.CacheRead == nil {
				t.CacheRead = p.CacheRead
			}
			if t.CacheWrite == nil {
				t.CacheWrite = p.CacheWrite
			}
			if t.Reasoning == nil {
				t.Reasoning = p.Reasoning
			}
			p.Tiers = append(p.Tiers, t)
		}
		return p, true
	}
	if !okIn || !okOut {
		return p, false
	}

	// 分档写法二：input_cost_per_token_above_200k_tokens 这类字段（超过 N 千个 token 之后的单价）
	groups := map[int64]map[string]float64{}
	for k, raw := range obj {
		m := aboveRe.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		n, _ := strconv.ParseInt(m[2], 10, 64)
		f, ok := priceNum(raw)
		if !ok || n <= 0 {
			continue
		}
		if groups[n] == nil {
			groups[n] = map[string]float64{}
		}
		groups[n][m[1]] = f
	}
	var ns []int64
	for n := range groups {
		ns = append(ns, n)
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
	prev := Tier{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite, Reasoning: p.Reasoning}
	for _, n := range ns {
		g := groups[n]
		t := prev
		t.From = n*1000 + 1
		if v, ok := g["input_cost_per_token"]; ok {
			t.Input = v
		}
		if v, ok := g["output_cost_per_token"]; ok {
			t.Output = v
		}
		if v, ok := g["cache_read_input_token_cost"]; ok {
			t.CacheRead = &v
		}
		if v, ok := g["cache_creation_input_token_cost"]; ok {
			t.CacheWrite = &v
		}
		if v, ok := g["output_cost_per_reasoning_token"]; ok {
			t.Reasoning = &v
		}
		p.Tiers = append(p.Tiers, t)
		prev = t
	}
	return p, true
}

// rangeNum 分档区间的边界：0 到 10 亿之间的数字。
func rangeNum(raw json.RawMessage) (int64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil || f < 0 || f > 1e9 || math.IsNaN(f) {
		return 0, false
	}
	return int64(f), true
}

// ---------- 价格本：缓存、刷新、对照 ----------

// PriceStatus 价格数据的状态（给设置页看）。
type PriceStatus struct {
	Source   string `json:"source"`            // 固定是「LiteLLM 公开价格」
	Updated  string `json:"updated,omitempty"` // 取到的日期（本地时间）
	Count    int    `json:"count"`             // 有效条目数
	Fetching bool   `json:"fetching,omitempty"`
	Error    string `json:"error,omitempty"` // 上次取的时候出的错（有缓存时照样用缓存）
}

type priceCache struct {
	V       int              `json:"v"`
	Fetched time.Time        `json:"fetched"`
	Entries map[string]Price `json:"entries"`
}

// PriceBook 价格本：读写 <目录>/prices.json，按需从网上刷新。
type PriceBook struct {
	path string
	http *http.Client
	urls []string
	now  func() time.Time

	mu      sync.Mutex
	loaded  bool
	entries map[string]Price
	fetched time.Time
	lastTry time.Time
	lastErr string
	running bool
	wg      sync.WaitGroup
}

func newPriceBook(path string, hc *http.Client, urls []string, now func() time.Time) *PriceBook {
	return &PriceBook{path: path, http: hc, urls: urls, now: now}
}

func (b *PriceBook) loadLocked() {
	if b.loaded {
		return
	}
	b.loaded = true
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return
	}
	var c priceCache
	if json.Unmarshal(raw, &c) != nil || c.V != 1 {
		return
	}
	// 缓存也按不可信数据再筛一遍（文件可能被手改过）
	clean := map[string]Price{}
	for k, p := range c.Entries {
		if priceKeyRe.MatchString(k) && validPrice(p) {
			clean[k] = p
		}
	}
	b.entries, b.fetched = clean, c.Fetched
}

func validPrice(p Price) bool {
	ok := func(f float64) bool { return f >= 0 && f <= priceMaxValue && !math.IsNaN(f) }
	okp := func(f *float64) bool { return f == nil || ok(*f) }
	if !priceProviderRe.MatchString(p.Provider) || !ok(p.Input) || !ok(p.Output) || !okp(p.CacheRead) || !okp(p.CacheWrite) || !okp(p.Reasoning) || len(p.Tiers) > 16 {
		return false
	}
	for _, t := range p.Tiers {
		if t.From < 0 || !ok(t.Input) || !ok(t.Output) || !okp(t.CacheRead) || !okp(t.CacheWrite) || !okp(t.Reasoning) {
			return false
		}
	}
	return true
}

// Status 当前状态。
func (b *PriceBook) Status() PriceStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	s := PriceStatus{Source: "LiteLLM 公开价格", Count: len(b.entries), Fetching: b.running, Error: b.lastErr}
	if !b.fetched.IsZero() {
		s.Updated = b.fetched.Local().Format("2006-01-02")
	}
	return s
}

// MaybeRefresh 缓存没有或超过 7 天时，在后台刷新一次（失败后 6 小时内不再自动重试）。
func (b *PriceBook) MaybeRefresh() {
	b.mu.Lock()
	b.loadLocked()
	now := b.now()
	need := b.entries == nil || now.Sub(b.fetched) >= priceRefresh
	if !need || b.running || (!b.lastTry.IsZero() && now.Sub(b.lastTry) < priceRetryAfter) {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.wg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = b.fetch(ctx)
	}()
}

// Refresh 马上刷新（设置页的「刷新」按钮）。已经在刷新时等它结束。
func (b *PriceBook) Refresh(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		b.wg.Wait()
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.lastErr != "" {
			return errors.New(b.lastErr)
		}
		return nil
	}
	b.running = true
	b.wg.Add(1)
	b.mu.Unlock()
	defer b.wg.Done()
	return b.fetch(ctx)
}

// wait 等后台的刷新结束（测试用）。
func (b *PriceBook) wait() { b.wg.Wait() }

// fetch 依次试各个地址，取到第一份合格的就存下。调用前 running 已置上。
func (b *PriceBook) fetch(ctx context.Context) error {
	var lastErr error
	for _, u := range b.urls {
		raw, err := b.download(ctx, u)
		if err == nil {
			var entries map[string]Price
			if entries, err = ParsePrices(raw); err == nil {
				now := b.now()
				b.mu.Lock()
				b.entries, b.fetched, b.lastTry, b.lastErr, b.running = entries, now, now, "", false
				b.mu.Unlock()
				b.save(entries, now)
				return nil
			}
		}
		lastErr = fmt.Errorf("%s：%w", shortHost(u), err)
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的下载地址")
	}
	b.mu.Lock()
	b.lastTry, b.lastErr, b.running = b.now(), "价格数据取不到："+lastErr.Error(), false
	b.mu.Unlock()
	return lastErr
}

func shortHost(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}

func (b *PriceBook) download(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MineDeck (price estimate)")
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, priceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > priceMaxBytes {
		return nil, errors.New("文件超过 10 MB")
	}
	return raw, nil
}

func (b *PriceBook) save(entries map[string]Price, at time.Time) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(priceCache{V: 1, Fetched: at, Entries: entries}) != nil {
		return
	}
	_ = writeFileAtomic(b.path, buf.Bytes())
}

// Lookup 按服务商名对照到 LiteLLM 的条目（§4.4）：先查「服务商名/模型名」，再查模型名，条目的服务商名也要对得上；
// 自定义地址（provider 为空）先按模型名查，再找以「/模型名」结尾的条目，只有一个时才用，多个就算查不到。
func (b *PriceBook) Lookup(provider, model string) (Price, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	if model == "" || b.entries == nil {
		return Price{}, false
	}
	if provider != "" {
		for _, k := range []string{provider + "/" + model, model} {
			if p, ok := b.entries[k]; ok && p.Provider == provider {
				return p, true
			}
		}
		return Price{}, false
	}
	if p, ok := b.entries[model]; ok {
		return p, true
	}
	var found Price
	n := 0
	for k, p := range b.entries {
		if strings.HasSuffix(k, "/"+model) {
			found = p
			n++
		}
	}
	return found, n == 1
}

// writeFileAtomic 先写临时文件再改名，写到一半断电不会留下半个文件。
func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
