package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 价格样本是从 LiteLLM 1.106.5 安装包里的 model_prices_and_context_window_backup.json 裁出来的一小份
// （MIT 许可，原文见 testdata/litellm/LICENSE），里面有 sample_spec、嵌入和画图模型这些要跳过的条目。
func samplePrices(t *testing.T) []byte { return sample(t, "litellm/model_prices_sample.json") }

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-12+1e-9*math.Abs(b) }

func TestParsePricesSample(t *testing.T) {
	ps, err := ParsePrices(samplePrices(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, skip := range []string{"sample_spec", "text-embedding-3-small", "gpt-image-2"} {
		if _, ok := ps[skip]; ok {
			t.Errorf("%s 不是对话模型，应跳过", skip)
		}
	}
	if len(ps) != 24 {
		t.Errorf("有效条目 %d 个，应为 24", len(ps))
	}
	ds := ps["deepseek/deepseek-v4-flash"]
	if ds.Provider != "deepseek" || !near(ds.Input, 3e-7) || !near(ds.Output, 1.2e-6) || ds.CacheRead == nil || !near(*ds.CacheRead, 6e-9) {
		t.Errorf("deepseek-v4-flash：%+v", ds)
	}
	// tiered_pricing：顶层没有单价，从 0 起的那一档是基础价
	q := ps["dashscope/qwen-plus-latest"]
	if !near(q.Input, 4e-7) || q.Reasoning == nil || !near(*q.Reasoning, 4e-6) || len(q.Tiers) != 1 || q.Tiers[0].From != 256000 || !near(q.Tiers[0].Input, 1.2e-6) {
		t.Errorf("qwen-plus-latest 的分档：%+v", q)
	}
	if m := ps["dashscope/qwen3-max"]; len(m.Tiers) != 2 || m.Tiers[1].From != 128000 || !near(m.Tiers[1].Output, 1.5e-5) {
		t.Errorf("qwen3-max 的分档：%+v", m)
	}
	// _above_272k_tokens 这类字段
	g := ps["gpt-5.5"]
	if len(g.Tiers) != 1 || g.Tiers[0].From != 272001 || !near(g.Tiers[0].Input, 1e-5) || !near(g.Tiers[0].Output, 4.5e-5) || !near(*g.Tiers[0].CacheRead, 1e-6) {
		t.Errorf("gpt-5.5 的分档：%+v", g)
	}
	// 缓存写入的 1 小时价（_above_1hr）不是按长度分档，不能当成分档
	h := ps["claude-haiku-5-5"]
	if len(h.Tiers) != 1 || h.Tiers[0].From != 100001 || !near(*h.CacheWrite, 1.25e-7) || !near(*h.Tiers[0].CacheWrite, 6.25e-7) {
		t.Errorf("claude-haiku-5-5：%+v", h)
	}
	if o := ps["claude-opus-5-5"]; o.Provider != "anthropic" || !near(o.Input, 4e-6) || !near(o.Output, 2e-5) || len(o.Tiers) != 0 {
		t.Errorf("claude-opus-5-5：%+v", o)
	}
}

// 下载的内容当不可信数据：负数、字符串、超过每 token 1 美元的值、奇怪的键名和服务商名一律丢掉，
// 坏的可选字段只丢那一项；不是 JSON、太大、有效条目太少的整份不用。
func TestParsePricesRejectsBadData(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(samplePrices(t), &top); err != nil {
		t.Fatal(err)
	}
	add := func(k, v string) { top[k] = json.RawMessage(v) }
	add("bad/negative", `{"mode":"chat","litellm_provider":"x","input_cost_per_token":-1e-6,"output_cost_per_token":1e-6}`)
	add("bad/string", `{"mode":"chat","litellm_provider":"x","input_cost_per_token":"0.000001","output_cost_per_token":1e-6}`)
	add("bad/huge", `{"mode":"chat","litellm_provider":"x","input_cost_per_token":1e-6,"output_cost_per_token":5}`)
	add("bad key", `{"mode":"chat","litellm_provider":"x","input_cost_per_token":1e-6,"output_cost_per_token":1e-6}`)
	add(strings.Repeat("k", 201), `{"mode":"chat","litellm_provider":"x","input_cost_per_token":1e-6,"output_cost_per_token":1e-6}`)
	add("bad/provider", `{"mode":"chat","litellm_provider":"Evil <b>Provider</b>","input_cost_per_token":1e-6,"output_cost_per_token":1e-6}`)
	add("bad/notobject", `"just a string"`)
	add("bad/array", `[1,2]`)
	add("bad/tier", `{"mode":"chat","litellm_provider":"x","tiered_pricing":[{"range":[100,0],"input_cost_per_token":1e-6,"output_cost_per_token":1e-6}]}`)
	add("ok/badcache", `{"mode":"chat","litellm_provider":"x","input_cost_per_token":1e-6,"output_cost_per_token":2e-6,"cache_read_input_token_cost":"cheap","source":"<script>alert(1)</script>"}`)
	b, _ := json.Marshal(top)
	ps, err := ParsePrices(b)
	if err != nil {
		t.Fatal(err)
	}
	for k := range ps {
		if strings.HasPrefix(k, "bad") || len(k) > 200 {
			t.Errorf("坏条目 %q 应被丢掉", k)
		}
	}
	if p, ok := ps["ok/badcache"]; !ok || p.CacheRead != nil || !near(p.Output, 2e-6) {
		t.Errorf("坏的可选字段只丢那一项：%+v %v", p, ok)
	}
	if len(ps) != 25 {
		t.Errorf("有效条目 %d 个，应为 24 + 1", len(ps))
	}
	for name, raw := range map[string][]byte{
		"不是 JSON":  []byte("<html>502 Bad Gateway</html>"),
		"不是对象":     []byte(`[1,2,3]`),
		"条目太少":     []byte(`{"gpt-5":{"mode":"chat","litellm_provider":"openai","input_cost_per_token":1e-6,"output_cost_per_token":1e-6}}`),
		"超过 10 MB": append([]byte(`{"x":"`), append([]byte(strings.Repeat("a", priceMaxBytes)), '"', '}')...),
	} {
		if _, err := ParsePrices(raw); err == nil {
			t.Errorf("%s：应整份不用", name)
		}
	}
}

// 怎么算：输入、输出、缓存命中、缓存写入各乘各的单价；思考有单独单价才单独算；分档按这次的输入长度选。
func TestPriceCost(t *testing.T) {
	ps, _ := ParsePrices(samplePrices(t))
	cases := []struct {
		key  string
		u    Usage
		want float64
	}{
		// 1000 输入 + 2000 缓存命中 + 500 缓存写入 + 300 输出（Claude Opus 5.5：4 / 0.2 / 5 / 20 美元每百万）
		{"claude-opus-5-5", Usage{Input: 1000, CacheRead: 2000, CacheWrite: 500, Output: 300}, 1000*4e-6 + 2000*2e-7 + 500*5e-6 + 300*2e-5},
		// 思考 token 有单独单价（qwen-turbo：输出 0.2、思考 0.5 美元每百万），不重复算
		{"dashscope/qwen-turbo", Usage{Input: 100, Output: 1000, Reasoning: 600}, 100*5e-8 + 400*2e-7 + 600*5e-7},
		// 没有单独单价的思考按输出价（已含在输出里）
		{"gpt-5", Usage{Input: 100, Output: 1000, Reasoning: 600}, 100*1.25e-6 + 1000*1e-5},
		// 分档：输入 30 万超过 25.6 万，整次按第二档算
		{"dashscope/qwen-plus-latest", Usage{Input: 300000, Output: 1000}, 300000*1.2e-6 + 1000*3.6e-6},
		{"dashscope/qwen-plus-latest", Usage{Input: 1000, Output: 1000}, 1000*4e-7 + 1000*1.2e-6},
		// 超出最后一档的上限也按最后一档
		{"dashscope/qwen3-max", Usage{Input: 400000, Output: 10}, 400000*3e-6 + 10*1.5e-5},
		// 输入长度含缓存：20 万缓存命中 + 8 万输入超过 27.2 万，按高档
		{"gpt-5.5", Usage{Input: 80000, CacheRead: 200000, Output: 100}, 80000*1e-5 + 200000*1e-6 + 100*4.5e-5},
		// 没有缓存价的按输入价算
		{"dashscope/qwen-plus", Usage{Input: 10, CacheRead: 90, Output: 5}, 100*4e-7 + 5*1.2e-6},
	}
	for _, c := range cases {
		if got := ps[c.key].Cost(c.u); !near(got, c.want) {
			t.Errorf("%s %+v：%.10f，应为 %.10f", c.key, c.u, got, c.want)
		}
	}
}

// 对照 LiteLLM 的条目：先查「服务商名/模型名」再查模型名，服务商名要对得上；
// 自定义地址按模型名查，再找以「/模型名」结尾的唯一条目，有多个就算查不到。
func TestPriceLookup(t *testing.T) {
	b := newPriceBook(filepath.Join(t.TempDir(), "prices.json"), nil, nil, time.Now)
	ps, _ := ParsePrices(samplePrices(t))
	b.entries, b.loaded = ps, true
	cases := []struct {
		provider, model string
		found           bool
		input           float64
	}{
		{"deepseek", "deepseek-v4-pro", true, 1.32e-6},
		{"deepseek", "deepseek-v4-flash", true, 3e-7},
		{"dashscope", "qwen-plus", true, 4e-7},
		{"moonshot", "kimi-k2.6", true, 9.5e-7},
		{"zai", "glm-5.3", true, 1.4e-6},
		{"openai", "gpt-5", true, 1.25e-6},
		{"anthropic", "claude-sonnet-5-5", true, 2e-6},
		{"openai", "deepseek-chat", false, 0}, // 条目的服务商对不上
		{"openai", "qwen-plus", false, 0},     // 只有 dashscope/qwen-plus
		{"", "gpt-4o-mini", true, 1.5e-7},     // 自定义：按模型名
		{"", "llama3.1", true, 0},             // 自定义：以 /llama3.1 结尾的只有一个
		{"", "glm-4.5", false, 0},             // 自定义：以 /glm-4.5 结尾的有两个，算查不到
		{"anthropic", "claude-no-such", false, 0},
	}
	for _, c := range cases {
		p, ok := b.Lookup(c.provider, c.model)
		if ok != c.found || (ok && !near(p.Input, c.input)) {
			t.Errorf("Lookup(%q, %q) = %+v %v", c.provider, c.model, p, ok)
		}
	}
}

// priceServer 本地假服务：/m1 是坏掉的镜像，/m2 交出价格文件（body 可换）。
type priceServer struct {
	mu   sync.Mutex
	body []byte
	hits map[string]int
}

func (p *priceServer) serve(t *testing.T) *fakeServer {
	return newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
		p.mu.Lock()
		if p.hits == nil {
			p.hits = map[string]int{}
		}
		p.hits[r.Path]++
		body := p.body
		p.mu.Unlock()
		if r.Path == "/m1/litellm.json" {
			w.WriteHeader(502)
			return
		}
		_, _ = w.Write(body)
	})
}

func (p *priceServer) count(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits[path]
}

// 缓存与刷新：取到后存进 prices.json，重启后直接用缓存；7 天内不再下载，过了 7 天自动刷新；
// 坏掉的镜像跳过；取到坏数据时保留旧缓存，6 小时内不再自动重试。
func TestPriceBookCacheAndRefresh(t *testing.T) {
	ps := &priceServer{body: samplePrices(t)}
	f := ps.serve(t)
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	clock := func() time.Time { return now }
	urls := []string{f.srv.URL + "/m1/litellm.json", f.srv.URL + "/m2/litellm.json"}
	b := newPriceBook(filepath.Join(dir, "prices.json"), f.srv.Client(), urls, clock)
	if _, ok := b.Lookup("openai", "gpt-5"); ok {
		t.Fatal("还没取就查到了")
	}
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ps.count("/m1/litellm.json") != 1 || ps.count("/m2/litellm.json") != 1 {
		t.Errorf("应先试坏镜像再换下一个：%v", ps.hits)
	}
	st := b.Status()
	if st.Count != 24 || st.Updated != "2026-10-28" || st.Error != "" || st.Source != "LiteLLM 公开价格" {
		t.Errorf("状态：%+v", st)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "prices.json"))
	if len(raw) == 0 || strings.Contains(string(raw), "source") || strings.Contains(string(raw), "api-docs.deepseek.com") {
		t.Errorf("缓存里只留价格字段，不留文字：%.200s", raw)
	}
	// 重启：直接读缓存，不联网
	b2 := newPriceBook(filepath.Join(dir, "prices.json"), f.srv.Client(), urls, clock)
	if _, ok := b2.Lookup("openai", "gpt-5"); !ok || len(f.requests()) != 2 {
		t.Fatalf("重启后应直接用缓存：请求了 %d 次", len(f.requests()))
	}
	b2.MaybeRefresh()
	b2.wait()
	if len(f.requests()) != 2 {
		t.Error("7 天之内不该再下载")
	}
	now = now.Add(8 * 24 * time.Hour)
	b2.MaybeRefresh()
	b2.wait()
	if ps.count("/m2/litellm.json") != 2 || b2.Status().Updated != "2026-11-05" {
		t.Errorf("过了 7 天应自动刷新：%v %+v", ps.hits, b2.Status())
	}
	// 镜像交出坏数据：保留旧缓存，记下错误；6 小时内不再自动重试
	ps.mu.Lock()
	ps.body = []byte("<html>blocked</html>")
	ps.mu.Unlock()
	now = now.Add(8 * 24 * time.Hour)
	b2.MaybeRefresh()
	b2.wait()
	if _, ok := b2.Lookup("openai", "gpt-5"); !ok || b2.Status().Error == "" || b2.Status().Updated != "2026-11-05" {
		t.Errorf("坏数据不该顶掉旧缓存：%+v", b2.Status())
	}
	n := len(f.requests())
	now = now.Add(time.Hour)
	b2.MaybeRefresh()
	b2.wait()
	if len(f.requests()) != n {
		t.Error("失败后 6 小时内不该自动重试")
	}
}
