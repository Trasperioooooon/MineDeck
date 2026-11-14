package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// memKeys 内存里的 Key 存放处（Windows 上的 DPAPI 加密由 web 包的测试在 wine 里验证）。
type memKeys struct {
	mu     sync.Mutex
	m      map[string]string
	broken bool
	// failSet 模拟写盘失败：返回非 nil 时这次 Set 什么都不改，原样报这个错
	failSet func(name, v string) error
	// beforeGet 每次 Get 读值之前调（不拿着 mu），测试用它卡住读 Key 的那一方
	beforeGet func()
}

func (k *memKeys) Get(n string) (string, error) {
	if k.beforeGet != nil {
		k.beforeGet()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.broken && k.m[n] != "" {
		return "", errors.New("解不开")
	}
	return k.m[n], nil
}

func (k *memKeys) Set(n, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.failSet != nil {
		if err := k.failSet(n, v); err != nil {
			return err
		}
	}
	if v == "" {
		delete(k.m, n)
	} else {
		k.m[n] = v
	}
	return nil
}

func (k *memKeys) State(n string) (bool, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m[n] == "" {
		return false, false
	}
	if k.broken {
		return false, true
	}
	return true, false
}

// NoDial 一拨号就报错的 HTTP 客户端：记下想连的地址。
type noDial struct {
	mu    sync.Mutex
	addrs []string
}

func (d *noDial) client() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
		d.mu.Lock()
		d.addrs = append(d.addrs, addr)
		d.mu.Unlock()
		return nil, errors.New("测试里不许联网")
	}}}
}

func (d *noDial) dials() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

func strp(s string) *string { return &s }

// 没配置 AI 时：不发出任何对外连接，也不取价格；只配了 Ollama 时也不取价格。配了要花钱的服务商才会去取。
func TestNoNetworkUntilConfigured(t *testing.T) {
	nd := &noDial{}
	keys := &memKeys{m: map[string]string{}}
	mirrors := func(u string) []string { return []string{"https://mirror.example/" + u, u} }
	s := NewService(Options{Dir: t.TempDir(), Keys: keys, HTTP: nd.client(), Mirrors: mirrors})
	s.Start()
	v := s.View()
	if v.Enabled || v.Missing != "选择服务商" || v.PriceNeeded || len(v.Presets) != 9 {
		t.Fatalf("没配置时的状态：%+v", v)
	}
	if err := s.SetQuota(map[Feature]int64{FeatAssistant: 200000}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrices([]CustomPrice{{Model: "deepseek-v4-flash", Currency: "CNY", Input: 1, Output: 2}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, err := range map[string]error{
		"Begin": s.Begin(FeatAssistant), "RefreshPrices": s.RefreshPrices(ctx),
	} {
		if !errors.Is(err, ErrDisabled) {
			t.Errorf("%s 应返回「未启用」：%v", name, err)
		}
	}
	if _, err := s.Test(ctx); !errors.Is(err, ErrDisabled) {
		t.Errorf("Test：%v", err)
	}
	if _, err := s.Client(true); !errors.Is(err, ErrDisabled) {
		t.Errorf("Client：%v", err)
	}
	s.Record(FeatGameChat, &Result{Usage: Usage{Input: 1}}) // 记账本身不联网
	time.Sleep(50 * time.Millisecond)
	s.prices.wait()
	if d := nd.dials(); len(d) != 0 {
		t.Fatalf("没配置 AI 却发出了连接：%v", d)
	}

	// 只配了 Ollama（本机，不用 Key）：启用了，但不取价格
	if err := s.SetProvider(ProviderPatch{Preset: strp("ollama"), Model: strp("qwen3:8b")}); err != nil {
		t.Fatal(err)
	}
	s.Start()
	s.Record(FeatAssistant, &Result{Usage: Usage{Input: 10, Output: 5}})
	if err := s.RefreshPrices(ctx); err == nil || errors.Is(err, ErrDisabled) {
		t.Errorf("本机模型不需要价格数据：%v", err)
	}
	time.Sleep(50 * time.Millisecond)
	s.prices.wait()
	if d := nd.dials(); len(d) != 0 {
		t.Fatalf("只配了 Ollama 却发出了连接：%v", d)
	}
	v = s.View()
	if !v.Enabled || v.PriceNeeded || v.ModelPrice == nil || !v.ModelPrice.Local {
		t.Errorf("Ollama 的状态：%+v", v)
	}
	if tt := v.Today[FeatAssistant]; tt.Tokens != 15 || tt.Unpriced != 0 || len(tt.Cost) != 0 {
		t.Errorf("本机模型记 token、不记钱：%+v", tt)
	}

	// 配了要花钱的服务商：这时才去取价格（只会连到价格文件的镜像和 GitHub）
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), Key: strp("sk-deepseek-123456")}); err != nil {
		t.Fatal(err)
	}
	s.prices.wait()
	d := nd.dials()
	if len(d) != 2 || d[0] != "mirror.example:443" || d[1] != "raw.githubusercontent.com:443" {
		t.Errorf("配好之后应依次试镜像和直连去取价格：%v", d)
	}
}

// Key 只写不读：存进 KeyStore（不进 settings.json）；换了服务商、或者地址换到了别的网站，旧 Key 自动清掉；
// 同一个网站只改路径，Key 照旧（测试报告问题 1）。
func TestProviderSettingsAndKeyRules(t *testing.T) {
	dir := t.TempDir()
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: dir, Keys: keys, HTTP: (&noDial{}).client()})
	if err := s.SetProvider(ProviderPatch{Key: strp("sk-x")}); err == nil {
		t.Error("没选服务商就存 Key")
	}
	const key = "sk-deepseek-secret-0123456789"
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), Key: strp("  " + key + "  ")}); err != nil {
		t.Fatal(err)
	}
	v := s.View()
	if !v.Enabled || !v.KeySet || v.Provider.BaseURL != "https://api.deepseek.com" || v.Provider.Model != "deepseek-v4-flash" || v.Provider.Protocol != OpenAI {
		t.Fatalf("选了预设应填好地址和建议模型：%+v", v)
	}
	if keys.m[KeyName] != key {
		t.Errorf("Key 应去掉首尾空白后存进 KeyStore：%q", keys.m[KeyName])
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	vb, _ := json.Marshal(v)
	if strings.Contains(string(b), key) || strings.Contains(string(vb), key) {
		t.Error("settings.json 或给界面的状态里有 Key")
	}
	// 只改模型：Key 不动
	if err := s.SetProvider(ProviderPatch{Model: strp("deepseek-v4-pro")}); err != nil || keys.m[KeyName] != key {
		t.Fatalf("只改模型不该动 Key：%v", err)
	}
	// 同一个网站只改路径（大小写、默认端口不同也算同一个）：Key 不动
	for _, u := range []string{"https://api.deepseek.com/nonexistent-path", "https://API.deepseek.com:443/v1"} {
		if err := s.SetProvider(ProviderPatch{BaseURL: strp(u)}); err != nil || keys.m[KeyName] != key || !s.View().KeySet {
			t.Fatalf("改成同一网站的 %s 不该动 Key：%v", u, err)
		}
	}
	// 改到别的网站：旧 Key 清掉，要重填
	if err := s.SetProvider(ProviderPatch{BaseURL: strp("https://evil.example/v1")}); err != nil {
		t.Fatal(err)
	}
	if v := s.View(); v.KeySet || v.Enabled || v.Missing != "填写 Key" || keys.m[KeyName] != "" {
		t.Errorf("改了地址，旧 Key 应清掉：%+v", v)
	}
	// 同一次请求里带了新 Key：用新的
	if err := s.SetProvider(ProviderPatch{Preset: strp("kimi"), Key: strp("sk-kimi-1")}); err != nil || keys.m[KeyName] != "sk-kimi-1" {
		t.Fatalf("换预设同时给了新 Key：%v %q", err, keys.m[KeyName])
	}
	if v := s.View(); v.Provider.BaseURL != "https://api.moonshot.cn/v1" || v.Provider.Model != "" || v.Missing != "选择模型" {
		t.Errorf("Kimi 没有建议模型，要从列表里选：%+v", v)
	}
	// 换预设：旧 Key 清掉
	if err := s.SetProvider(ProviderPatch{Preset: strp("claude"), Effort: strp("high")}); err != nil || keys.m[KeyName] != "" {
		t.Fatalf("换预设应清掉旧 Key：%v", err)
	}
	if v := s.View(); v.Provider.Protocol != Anthropic || v.Provider.Effort != "high" || !v.Provider.Eager || !v.Provider.Fallback {
		t.Errorf("Claude 官方地址默认开 eager 和服务端回退：%+v", v.Provider)
	}
	// 自定义：协议二选一，中转默认不开 Claude 的两个可选功能
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), Protocol: (*Protocol)(strp("anthropic")), BaseURL: strp("https://relay.example"), Model: strp("claude-opus-5-5"), Key: strp("k-relay")}); err != nil {
		t.Fatal(err)
	}
	if v := s.View(); !v.Enabled || v.Provider.Eager || v.Provider.Fallback {
		t.Errorf("中转默认不开 eager 和服务端回退：%+v", v.Provider)
	}
	for name, pt := range map[string]ProviderPatch{
		"未知预设":    {Preset: strp("nope")},
		"地址不对":    {BaseURL: strp("ftp://x")},
		"地址带查询串":  {BaseURL: strp("https://x.example/v1?key=1")},
		"Key 有空格": {Key: strp("sk a")},
		"思考深度不对":  {Effort: strp("huge")},
	} {
		if err := s.SetProvider(pt); err == nil {
			t.Errorf("%s 应报错", name)
		}
	}
	// 停用：清掉设置和 Key
	if err := s.SetProvider(ProviderPatch{Preset: strp("")}); err != nil {
		t.Fatal(err)
	}
	if v := s.View(); v.Enabled || v.Provider.Preset != "" || keys.m[KeyName] != "" {
		t.Errorf("停用后：%+v", v)
	}
	// Key 解不开（换了电脑或 Windows 用户）：当作没设置，提示重填
	_ = s.SetProvider(ProviderPatch{Preset: strp("openai"), Model: strp("gpt-5"), Key: strp("sk-openai-1")})
	keys.broken = true
	if v := s.View(); v.Enabled || !v.KeyBroken || !strings.Contains(v.Missing, "解不开") {
		t.Errorf("Key 解不开时：%+v", v)
	}
	// 手改坏的设置文件：当作没配置
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"preset":"claude","protocol":"openai","baseUrl":"https://api.anthropic.com","model":"x"}`), 0o644)
	if v := NewService(Options{Dir: dir, Keys: keys}).View(); v.Provider.Preset != "" {
		t.Errorf("协议和预设对不上的设置应当作没配置：%+v", v.Provider)
	}
}

// 模型列表：可以用还没保存的服务商和地址；不传 Key 时只有服务商没换、地址还是同一个网站才用已存的 Key。
func TestModelsKeyRules(t *testing.T) {
	f := newFake(t, func(_ int, r recorded, w http.ResponseWriter) { serve(t, w, "openai/models.json") })
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: t.TempDir(), Keys: keys, HTTP: f.srv.Client()})
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), BaseURL: strp(f.srv.URL), Key: strp("sk-saved")}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ml, err := s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: f.srv.URL})
	if err != nil || ml.Source != "live" || len(ml.Models) != 3 || f.requests()[0].Header.Get("Authorization") != "Bearer sk-saved" {
		t.Fatalf("同一个地址用已存的 Key：%v %+v", err, ml)
	}
	if _, err := s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: f.srv.URL + "/other"}); err != nil || f.requests()[1].Header.Get("Authorization") != "Bearer sk-saved" {
		t.Fatalf("同一个网站换了路径，照样用已存的 Key：%v", err)
	}
	other := strings.Replace(f.srv.URL, "127.0.0.1", "localhost", 1) // 同一个服务器，换个名字就算别的网站
	if _, err := s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: other}); err == nil || len(f.requests()) != 2 {
		t.Errorf("换了网站又没给 Key，不能把已存的 Key 发过去：%v", err)
	}
	if _, err := s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: other, Key: "sk-new"}); err != nil {
		t.Fatal(err)
	}
	if h := f.requests()[2].Header.Get("Authorization"); h != "Bearer sk-new" {
		t.Errorf("用请求里给的 Key：%q", h)
	}
}

// 测试连接：发一个带测试工具的流式请求，报告延迟、能不能调用工具；服务商说不支持工具时去掉工具再试。
// Claude 中转：先打开 eager 和服务端回退试，不认的关掉，结果存进设置。
func TestServiceTestConnection(t *testing.T) {
	ping := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_p","type":"function","function":{"name":"minedeck_ping","arguments":"{\"ok\":true}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":9}}` + "\n\ndata: [DONE]\n\n"
	t.Run("能调用工具", func(t *testing.T) {
		f := newFake(t, func(n int, r recorded, w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			if n == 0 {
				_, _ = w.Write([]byte(ping))
				return
			}
			if !strings.Contains(string(r.Body), `"role":"tool"`) || !strings.Contains(string(r.Body), `call_p`) || !strings.Contains(string(r.Body), `ok`) {
				t.Errorf("第二轮应带上工具结果：%s", r.Body)
			}
			_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"连接正常\"},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"))
		})
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
		_ = s.SetProvider(ProviderPatch{Preset: strp("openai"), BaseURL: strp(f.srv.URL), Model: strp("gpt-5"), Key: strp("sk-1")})
		r, err := s.Test(context.Background())
		if err != nil || !r.OK || r.Tools != "yes" || r.Rounds != 2 || r.Usage.Input != 140 || r.Usage.Output != 13 || r.Reply != "连接正常" || r.Model != "gpt-5" {
			t.Fatalf("%v %+v", err, r)
		}
		if len(f.requests()) != 2 {
			t.Fatalf("应是两轮，实际 %d", len(f.requests()))
		}
		if body := string(f.requests()[0].Body); !strings.Contains(body, "minedeck_ping") || !strings.Contains(body, `"stream":true`) {
			t.Errorf("测试请求：%s", body)
		}
		if tt := s.View().Today[FeatAssistant]; tt.Requests != 0 {
			t.Error("测试连接不计入用量")
		}
	})
	t.Run("不支持工具", func(t *testing.T) {
		f := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
			if strings.Contains(string(r.Body), `"tools"`) {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools"}}`))
				return
			}
			serve(t, w, "ollama/text.sse")
		})
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
		_ = s.SetProvider(ProviderPatch{Preset: strp("ollama"), BaseURL: strp(f.srv.URL + "/v1/"), Model: strp("gemma:2b")})
		r, err := s.Test(context.Background())
		if err != nil || !r.OK || r.Tools != "no" || r.Reply == "" {
			t.Fatalf("%v %+v", err, r)
		}
		if r.Cost == nil || r.Cost.Source != "local" {
			t.Errorf("本机模型不计费：%+v", r.Cost)
		}
	})
	t.Run("连不上", func(t *testing.T) {
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: (&noDial{}).client()})
		s.retryDelay = time.Millisecond
		_ = s.SetProvider(ProviderPatch{Preset: strp("openai"), Model: strp("gpt-5"), Key: strp("sk-1")})
		r, err := s.Test(context.Background())
		if err != nil || r.OK || !strings.Contains(r.Error, "连不上服务商") || r.Hint != "连不上服务商，检查网络、代理和接口地址" {
			t.Fatalf("%v %+v", err, r)
		}
	})
	// 原文带着状态码和服务商的原话，另给一句人话（Hint），界面的报错正文接它
	t.Run("Key 不对", func(t *testing.T) {
		f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`))
		})
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
		_ = s.SetProvider(ProviderPatch{Preset: strp("openai"), BaseURL: strp(f.srv.URL), Model: strp("gpt-5"), Key: strp("sk-1")})
		r, err := s.Test(context.Background())
		if err != nil || r.OK || !strings.Contains(r.Error, "HTTP 401") || !strings.Contains(r.Error, "Incorrect API key provided") || r.Hint != "Key 不对或已失效" {
			t.Fatalf("%v %+v", err, r)
		}
	})
	t.Run("Claude 中转不认 eager", func(t *testing.T) {
		f := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
			if strings.Contains(string(r.Body), "eager_input_streaming") {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools.0.custom.eager_input_streaming: Extra inputs are not permitted"}}`))
				return
			}
			serve(t, w, "claude/tools.sse")
		})
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
		_ = s.SetProvider(ProviderPatch{Preset: strp(CustomID), Protocol: (*Protocol)(strp("anthropic")), BaseURL: strp(f.srv.URL), Model: strp("claude-opus-5-5"), Key: strp("k")})
		r, err := s.Test(context.Background())
		if err != nil || !r.OK || r.Eager == nil || *r.Eager || r.Fallback == nil || !*r.Fallback {
			t.Fatalf("%v %+v", err, r)
		}
		if p := s.View().Provider; p.Eager || !p.Fallback {
			t.Errorf("测试结果应存进设置：%+v", p)
		}
		if r.Tools != "unknown" { // 样本里调的是 instance_props_get，不是测试工具
			t.Errorf("没调用测试工具：%s", r.Tools)
		}
	})
}

// 记账和额度：按功能、按天记；额度用完这项功能暂停到第二天；本月合计；重启后还在。
// 费用：自填单价优先（可以是人民币），公开价格兜底，查不到的只记 token。
func TestLedgerQuotaAndCost(t *testing.T) {
	ps := &priceServer{body: samplePrices(t)}
	f := ps.serve(t)
	dir := t.TempDir()
	now := time.Date(2026, 10, 30, 23, 0, 0, 0, time.Local)
	clock := func() time.Time { return now }
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: dir, Keys: keys, HTTP: f.srv.Client(), Now: clock, PriceURL: f.srv.URL + "/m2/litellm.json"})
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), Key: strp("sk-1")}); err != nil {
		t.Fatal(err)
	}
	s.prices.wait() // 配好之后自动取了一次价格
	if s.View().Prices.Count != 24 {
		t.Fatalf("价格没取到：%+v", s.View().Prices)
	}
	if err := s.SetQuota(map[Feature]int64{FeatAssistant: 3000, FeatGameChat: 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetQuota(map[Feature]int64{"music": 1}); err == nil {
		t.Error("只能给助手、游戏内 AI 和 AI 伙伴设额度")
	}
	if err := s.SetQuota(map[Feature]int64{FeatCompanion: 100}); err != nil || s.View().Quota[FeatCompanion] != 100 {
		t.Errorf("AI 伙伴的额度（第七阶段）：%v %v", err, s.View().Quota)
	}
	s.Record(FeatCompanion, &Result{Usage: Usage{Input: 80, Output: 40}, Model: "deepseek-v4-flash"})
	var cq *QuotaError
	if err := s.Begin(FeatCompanion); !errors.As(err, &cq) || cq.Feature != FeatCompanion || !strings.Contains(err.Error(), "AI 伙伴今天的额度已用完") {
		t.Errorf("AI 伙伴的额度用完了：%v", err)
	}
	if err := s.Begin(FeatGameChat); err != nil {
		t.Errorf("AI 伙伴的额度和游戏内 AI 分开算：%v", err)
	}
	if err := s.SetQuota(map[Feature]int64{FeatCompanion: 0}); err != nil || s.Begin(FeatCompanion) != nil {
		t.Errorf("清掉 AI 伙伴的额度：%v", err)
	}
	if err := s.Begin(FeatAssistant); err != nil {
		t.Fatal(err)
	}
	// 公开价格：deepseek-v4-flash 输入 0.3、缓存命中 0.006、输出 1.2 美元每百万
	c := s.Record(FeatAssistant, &Result{Usage: Usage{Input: 1000, CacheRead: 1000, Output: 500}, Model: "deepseek-v4-flash"})
	if c == nil || c.Currency != "USD" || c.Source != "litellm" || !near(c.Amount, 1000*3e-7+1000*6e-9+500*1.2e-6) {
		t.Errorf("公开价格的估算：%+v", c)
	}
	// 自填单价优先：人民币
	if err := s.SetPrices([]CustomPrice{{Model: "deepseek-v4-flash", Currency: "CNY", Input: 2, Output: 8}}); err != nil {
		t.Fatal(err)
	}
	c = s.Record(FeatAssistant, &Result{Usage: Usage{Input: 1000, Output: 500, Estimated: true}})
	if c == nil || c.Currency != "CNY" || c.Source != "custom" || !near(c.Amount, 1000*2e-6+500*8e-6) {
		t.Errorf("自填单价：%+v", c)
	}
	if mp := s.View().ModelPrice; mp == nil || mp.Source != "custom" || mp.Currency != "CNY" || mp.Input != 2 {
		t.Errorf("当前模型的单价：%+v", mp)
	}
	// 查不到价格的模型：只记 token
	_ = s.SetProvider(ProviderPatch{Model: strp("deepseek-unknown")})
	if c = s.Record(FeatAssistant, &Result{Usage: Usage{Input: 100}}); c != nil {
		t.Errorf("查不到价格应返回 nil：%+v", c)
	}
	today := s.View().Today[FeatAssistant]
	if today.Requests != 3 || today.Tokens != 4100 || today.Estimated != 1 || today.Unpriced != 1 ||
		!near(today.Cost["USD"], 1000*3e-7+1000*6e-9+500*1.2e-6) || !near(today.Cost["CNY"], 0.006) {
		t.Errorf("今天的合计：%+v", today)
	}
	// 额度用完：暂停到第二天
	var qe *QuotaError
	if err := s.Begin(FeatAssistant); !errors.As(err, &qe) || qe.Used != 4100 || qe.Limit != 3000 || !strings.Contains(err.Error(), "明天 0 点恢复") {
		t.Fatalf("额度用完应报错：%v", err)
	}
	if err := s.Begin(FeatGameChat); err != nil {
		t.Errorf("游戏内 AI 的额度另算：%v", err)
	}
	now = now.Add(2 * time.Hour) // 第二天（10 月 31 日）
	if err := s.Begin(FeatAssistant); err != nil {
		t.Errorf("第二天应恢复：%v", err)
	}
	s.Record(FeatAssistant, &Result{Usage: Usage{Input: 50}})
	month := s.View().Month[FeatAssistant]
	if month.Requests != 4 || month.Tokens != 4150 {
		t.Errorf("本月合计：%+v", month)
	}
	now = now.Add(24 * time.Hour) // 11 月 1 日：本月从零算
	if m := s.View().Month[FeatAssistant]; m.Requests != 0 {
		t.Errorf("换月后本月合计应从零开始：%+v", m)
	}
	// 重启后记账还在
	s2 := NewService(Options{Dir: dir, Keys: keys, HTTP: f.srv.Client(), Now: func() time.Time { return now.Add(-24 * time.Hour) }})
	if tt := s2.View().Today[FeatAssistant]; tt.Requests != 1 || tt.Tokens != 50 {
		t.Errorf("重启后 10 月 31 日的记账：%+v", tt)
	}
}

// Claude 发生服务端回退时，回退那部分按回退模型的价格算（§4.4）。
func TestCostWithFallbackAttempts(t *testing.T) {
	ps := &priceServer{body: samplePrices(t)}
	f := ps.serve(t)
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client(), PriceURL: f.srv.URL + "/m2/litellm.json"})
	_ = s.SetProvider(ProviderPatch{Preset: strp("claude"), Key: strp("k")})
	s.prices.wait()
	r := &Result{Model: "claude-opus-5", Usage: Usage{Input: 947, Output: 264},
		Attempts: []Attempt{{Model: "claude-opus-5-5", Usage: Usage{Input: 535}}, {Model: "claude-opus-5", Fallback: true, Usage: Usage{Input: 412, Output: 264}}}}
	c := s.Record(FeatAssistant, r)
	if c == nil || !near(c.Amount, 535*4e-6+412*5e-6+264*2.5e-5) {
		t.Errorf("回退的费用：%+v", c)
	}
	if mp := s.View().ModelPrice; mp == nil || mp.Source != "litellm" || mp.Input != 4 || mp.Note != "" {
		t.Errorf("Claude 的单价不带国内提示：%+v", mp)
	}
	_ = s.SetProvider(ProviderPatch{Preset: strp("qwen"), Key: strp("k")})
	if mp := s.View().ModelPrice; mp == nil || mp.Note != DomesticNote {
		t.Errorf("国内的服务商要注明按国际站美元价估算：%+v", mp)
	}
}

// 「局域网可用 AI 助手」默认开；关掉后存进设置文件，重启后还在，停用 AI 也不影响它。
// Current 只在配置好时给出服务商和模型（会话据此判断设置改过没有）。
func TestLANAssistantSetting(t *testing.T) {
	dir := t.TempDir()
	nd := &noDial{}
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: dir, Keys: keys, HTTP: nd.client()})
	if !s.LANAssistant() || !s.View().LANAssistant {
		t.Fatal("默认应开着")
	}
	if c := s.Current(); c != (Current{}) {
		t.Errorf("没配置时：%+v", c)
	}
	if err := s.SetLANAssistant(false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProvider(ProviderPatch{Preset: strp("ollama"), Model: strp("qwen3:8b")}); err != nil {
		t.Fatal(err)
	}
	if c := s.Current(); c.Preset != "ollama" || c.Model != "qwen3:8b" || c.Protocol != OpenAI || c.BaseURL == "" {
		t.Errorf("配置好之后：%+v", c)
	}
	if err := s.SetProvider(ProviderPatch{Preset: strp("")}); err != nil {
		t.Fatal(err)
	}
	s2 := NewService(Options{Dir: dir, Keys: keys, HTTP: nd.client()})
	if s2.LANAssistant() || s2.View().LANAssistant {
		t.Error("关掉之后重启应还是关着")
	}
	if used, limit := s2.Usage(FeatAssistant); used.Tokens != 0 || limit != 0 {
		t.Errorf("用量：%+v %d", used, limit)
	}
	if d := nd.dials(); len(d) != 0 {
		t.Errorf("不该联网：%v", d)
	}
}
