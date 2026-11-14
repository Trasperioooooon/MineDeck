package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// 每个预设、两种协议实际发出去的鉴权头。走 Service.config（PR #19 第一版的问题就出在这一层：
// 空的鉴权被补成 bearer，官方 Claude 和 Claude 中转都不再发 x-api-key）。
func TestAuthHeadersPerPreset(t *testing.T) {
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}})
	const key = "sk-test-0123456789"
	type want struct{ bearer, apiKey, xAPIKey bool }
	headers := func(st Settings, k string) http.Header {
		cl := &Client{cfg: s.config(st, k, false)}
		if st.Protocol == Anthropic {
			return cl.claudeHeader(false)
		}
		return cl.openaiHeader()
	}
	check := func(name string, h http.Header, w want) {
		t.Helper()
		got := want{h.Get("Authorization") == "Bearer "+key, h.Get("api-key") == key, h.Get("x-api-key") == key}
		if got != w {
			t.Errorf("%s：Bearer=%v api-key=%v x-api-key=%v，应为 %+v", name, got.bearer, got.apiKey, got.xAPIKey, w)
		}
	}
	for _, p := range presets {
		if p.ID == CustomID {
			continue
		}
		k := key
		if p.NoKey {
			k = ""
		}
		h := headers(Settings{Preset: p.ID, Protocol: p.Protocol, BaseURL: p.BaseURL}, k)
		switch {
		case p.NoKey:
			check(p.ID, h, want{})
		case p.ID == "claude": // 官方地址：只发 x-api-key
			check(p.ID, h, want{xAPIKey: true})
		case p.ID == "mimo":
			check(p.ID, h, want{bearer: true, apiKey: true})
		default:
			check(p.ID, h, want{bearer: true})
		}
		for _, a := range p.AltURLs {
			proto := a.Protocol
			if proto == "" {
				proto = p.Protocol
			}
			h := headers(Settings{Preset: p.ID, Protocol: proto, BaseURL: a.URL}, key)
			switch {
			case p.ID == "mimo" && proto == Anthropic:
				check(p.ID+" "+a.Label, h, want{bearer: true, apiKey: true, xAPIKey: true})
			case p.ID == "mimo":
				check(p.ID+" "+a.Label, h, want{bearer: true, apiKey: true})
			default:
				check(p.ID+" "+a.Label, h, want{bearer: true})
			}
		}
	}
	relay := "https://relay.example.com"
	check("自定义 Claude 中转（默认）", headers(Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: relay}, key), want{bearer: true, xAPIKey: true})
	check("自定义 Claude 官方地址（默认）", headers(Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: "https://api.anthropic.com"}, key), want{xAPIKey: true})
	check("自定义 Claude 只发 Bearer", headers(Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: relay, Auth: "bearer"}, key), want{bearer: true})
	check("自定义 Claude 只发 api-key", headers(Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: relay, Auth: "api-key"}, key), want{apiKey: true})
	check("自定义 Claude 都发", headers(Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: relay, Auth: "both"}, key), want{bearer: true, apiKey: true, xAPIKey: true})
	check("自定义 OpenAI（默认）", headers(Settings{Preset: CustomID, Protocol: OpenAI, BaseURL: relay}, key), want{bearer: true})
	check("自定义 OpenAI 只发 api-key", headers(Settings{Preset: CustomID, Protocol: OpenAI, BaseURL: relay, Auth: "api-key"}, key), want{apiKey: true})
}

// 真的走一次网络：保存为 Claude 中转的设置，拉模型列表时带 x-api-key 和 Bearer。
func TestClaudeRelayHeadersOnTheWire(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-x","display_name":"X"}]}`))
	})
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
	proto := Anthropic
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), Protocol: &proto, BaseURL: strp(f.srv.URL), Model: strp("claude-x"), Key: strp("sk-ant-test-0123456789")}); err != nil {
		t.Fatal(err)
	}
	if st := s.snapshot(); st.Auth != "" {
		t.Errorf("自定义默认的鉴权应留空（按协议的默认），实际 %q", st.Auth)
	}
	if _, err := s.Models(context.Background(), ModelsRequest{Preset: CustomID, Protocol: Anthropic, BaseURL: f.srv.URL}); err != nil {
		t.Fatal(err)
	}
	h := f.requests()[0].Header
	if h.Get("x-api-key") != "sk-ant-test-0123456789" || h.Get("Authorization") != "Bearer sk-ant-test-0123456789" {
		t.Errorf("Claude 中转应带 x-api-key 和 Bearer：%v", h)
	}
}

func TestSetProviderAuthValues(t *testing.T) {
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}})
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), BaseURL: strp("https://relay.example.com/v1"), Auth: strp("api-key")}); err != nil {
		t.Fatal(err)
	}
	if st := s.snapshot(); st.Auth != "api-key" || st.Echo != "auto" {
		t.Fatalf("自定义：%+v", st)
	}
	if err := s.SetProvider(ProviderPatch{Auth: strp("")}); err != nil || s.snapshot().Auth != "" {
		t.Fatalf("空串 = 按协议的默认，应能保存：%v %q", err, s.snapshot().Auth)
	}
	if err := s.SetProvider(ProviderPatch{Auth: strp("x-api-key")}); err == nil {
		t.Error("不认识的鉴权方式应报错")
	}
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek")}); err != nil {
		t.Fatal(err)
	}
	if st := s.snapshot(); st.Auth != "" || st.Echo != "" {
		t.Errorf("离开自定义后鉴权和回传应清掉：%+v", st)
	}
}

// 测试连接的第二轮（工具结果交回之后）被拒：照实报错，不能被当成「不支持工具」去掉工具重试、显示连接正常。
func TestTestConnectionSecondRoundFailure(t *testing.T) {
	ping := `data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"想","tool_calls":[{"index":0,"id":"call_p","type":"function","function":{"name":"minedeck_ping","arguments":"{}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":9}}` + "\n\ndata: [DONE]\n\n"
	for _, c := range []struct{ preset, msg, hint string }{
		{"openai", "An assistant message with 'tool_calls' must be followed by tool messages", "工具结果交回之后被拒：多半是思考内容没有原样传回"},
		{CustomID, "The reasoning_content in the thinking mode must be passed back to the API in multi-turn tool calls", "思考回传"},
	} {
		f := newFake(t, func(n int, _ recorded, w http.ResponseWriter) {
			if n == 0 {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(ping))
				return
			}
			w.WriteHeader(400)
			b, _ := json.Marshal(map[string]any{"error": map[string]string{"message": c.msg, "type": "invalid_request_error"}})
			_, _ = w.Write(b)
		})
		s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
		s.retryDelay = 1
		if err := s.SetProvider(ProviderPatch{Preset: strp(c.preset), BaseURL: strp(f.srv.URL), Model: strp("m"), Key: strp("sk-1")}); err != nil {
			t.Fatal(err)
		}
		r, err := s.Test(context.Background())
		if err != nil || r.OK || r.Tools != "yes" || r.Rounds != 2 || !strings.Contains(r.Hint, c.hint) || !strings.Contains(r.Error, "第二轮") {
			t.Errorf("%s：%v %+v", c.preset, err, r)
		}
		if n := len(f.requests()); n != 2 {
			t.Errorf("%s：第二轮失败后不该再去掉工具重试，实际请求了 %d 次", c.preset, n)
		}
	}
}

// Key 前缀和地址对不上，又真的 401 了：提示直接说对不上，比「Key 不对」更说到点子上。
func TestTestConnection401KeyMismatch(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API Key","type":"authentication_error"}}`))
	})
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
	if err := s.SetProvider(ProviderPatch{Preset: strp("mimo"), BaseURL: strp(f.srv.URL + "/v1"), Key: strp("tp-test-0123456789")}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Test(context.Background())
	if err != nil || r.OK || r.Warn == "" || r.Hint != "Key 和地址对不上：按量付费和套餐的 Key 不能混用" || strings.Contains(r.Error, "tp-test-0123456789") {
		t.Fatalf("%v %+v", err, r)
	}
}

// auto：对方不认 reasoning_content，被拒一次以后记在 Service 上；助手下一轮新建的客户端直接不带，不再先撞 400。
func TestAutoEchoDropRemembered(t *testing.T) {
	rejected := 0
	f := newFake(t, func(n int, r recorded, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(r.Body), "reasoning_content") {
			rejected++
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"'messages.1' : for 'role:assistant' the following must be satisfied[('messages.1' : property 'reasoning_content' is unsupported, did you mean 'reasoning'?)]","type":"invalid_request_error"}}`))
			return
		}
		if n == 0 {
			_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"想一想","tool_calls":[{"id":"c1","type":"function","function":{"name":"instance_list","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"好","reasoning_content":"再想想"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2}}`))
	})
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
	s.retryDelay = 1
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), BaseURL: strp(f.srv.URL + "/v1"), Model: strp("m")}); err != nil {
		t.Fatal(err)
	}
	cl, err := s.Client(false)
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := cl.NewConversation("", testTools)
	_ = conv.AddUser("列一下")
	for round := 0; round < 4; round++ {
		cl, _ = s.Client(false) // 助手每一轮都新建客户端
		res, err := cl.Chat(context.Background(), conv, nil)
		if err != nil {
			t.Fatalf("第 %d 轮：%v", round, err)
		}
		if err := conv.AddResult(res); err != nil {
			t.Fatal(err)
		}
		if len(res.ToolCalls) > 0 {
			_ = conv.AddToolResults([]ToolResult{{ID: res.ToolCalls[0].ID, Content: "[]"}})
		} else {
			_ = conv.AddUser("再说一次")
		}
	}
	if rejected != 1 {
		t.Fatalf("不认这个字段的服务商只该被拒一次，实际 %d 次", rejected)
	}
}

// on（DeepSeek、MiMo）：文档写明必须传回，被拒时不去掉重发（那只会换来另一个 400，还把原话盖掉）。
func TestEchoOnNeverDropped(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"unknown field reasoning_content","type":"invalid_request_error"}}`))
	})
	cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: "sk-test-0123456789", Model: "m", Echo: EchoOn, RetryDelay: 1})
	conv, _ := cl.NewConversation("", testTools)
	_ = conv.AddUser("在吗")
	if _, err := cl.Chat(context.Background(), conv, nil); err == nil || len(f.requests()) != 1 {
		t.Fatalf("on 被拒应直接报错：%v，请求 %d 次", err, len(f.requests()))
	}
}

func TestAdjustReasoning(t *testing.T) {
	plain := `{"role":"assistant","content":"hi"}`
	tool := `{"role":"assistant","content":"","tool_calls":[{"id":"c"}]}`
	withR := `{"role":"assistant","content":"x","reasoning_content":"r"}`
	user := `{"role":"user","content":"q"}`
	has := func(s string) bool { return strings.Contains(s, `"reasoning_content"`) }
	cases := []struct {
		name   string
		in     string
		echo   EchoMode
		drop   bool
		padAll bool
		want   bool
	}{
		{"off 去掉", withR, EchoOff, false, false, false},
		{"auto 收到的带上", withR, EchoAuto, false, false, true},
		{"auto 不补", tool, EchoAuto, false, false, false},
		{"auto 被拒后去掉", withR, EchoAuto, true, false, false},
		{"on 工具轮补空", tool, EchoOn, false, false, true},
		{"on 普通轮不补（MiMo）", plain, EchoOn, false, false, false},
		{"on 普通轮也补（DeepSeek）", plain, EchoOn, false, true, true},
		{"用户的话不动", user, EchoOn, false, true, false},
	}
	for _, c := range cases {
		got := string(adjustReasoning(json.RawMessage(c.in), c.echo, c.drop, c.padAll))
		if has(got) != c.want {
			t.Errorf("%s：%s", c.name, got)
		}
	}
	if got := string(adjustReasoning(json.RawMessage(plain), EchoOn, false, true)); !strings.Contains(got, `"reasoning_content":""`) {
		t.Errorf("补的应是空串：%s", got)
	}
}

func TestReasoningRejected(t *testing.T) {
	for msg, want := range map[string]bool{
		"'messages.1' : for 'role:assistant' the following must be satisfied[('messages.1' : property 'reasoning_content' is unsupported, did you mean 'reasoning'?)]": true,
		"unknown field reasoning_content":                                               true,
		"reasoning_effort is not supported with this model":                             false,
		"the reasoning_content in the thinking mode must be passed back to the api":     false,
		"missing `reasoning_content` field in the assistant message at message index 2": false,
		"unsupported parameter: temperature":                                            false,
	} {
		if got := reasoningRejected(strings.ToLower(msg)); got != want {
			t.Errorf("%q：%v", msg, got)
		}
	}
}

// 关键词表按各家真实报错原文（docs/评审-PR19-助手渲染与兼容服务商.md）。
func TestHintClassification(t *testing.T) {
	for _, c := range []struct {
		status         int
		msg, typ, want string
		retry          bool
	}{
		{400, "This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens.", "invalid_request_error", "对话太长", false},
		{400, "prompt is too long: 210000 tokens > 200000 maximum", "invalid_request_error", "对话太长", false},
		{400, "Input token length too long", "invalid_request_error", "对话太长", false},
		{400, "Prompt 超长", "", "对话太长", false},
		{400, "The reasoning_content in the thinking mode must be passed back to the API", "", "思考内容原样传回", false},
		{400, "reasoning_effort is not supported with this model", "", "模型名不对", false},
		{401, "Incorrect API key provided", "", "Key 不对或已失效", false},
		{429, "您的账户已欠费，请充值后重试", "", "额度用完", false},
		{429, "Your account has exceeded its current quota", "exceeded_current_quota_error", "额度用完", false},
		{429, "Credit balance exhausted", "", "额度用完", false},
		{429, "You exceeded your current quota", "insufficient_quota", "额度用完", false},
		{429, "Rate limit reached for requests", "rate_limit_reached_error", "请求太频繁", true},
		{429, "The engine is currently overloaded", "engine_overloaded_error", "请求太频繁", true},
		{503, "overloaded", "", "服务商暂时出了问题", true},
	} {
		e := &APIError{Status: c.status, Message: c.msg, Type: c.typ}
		if !strings.Contains(e.Hint(), c.want) || e.Retryable() != c.retry {
			t.Errorf("%d %q：提示 %q，可重试 %v", c.status, c.msg, e.Hint(), e.Retryable())
		}
	}
}

func TestScrubWordBoundary(t *testing.T) {
	for in, want := range map[string]string{
		"http-proxy-connection refused":    "http-proxy-connection refused",
		"task-manager-timeout":             "task-manager-timeout",
		"risk-control-blocked here":        "risk-control-blocked here",
		"bad key sk-live-0123456789abcdef": "bad key ***",
		`"tp-abcdefghijkl" invalid`:        `"***" invalid`,
		"key=ttp-team-0123456789":          "key=***",
	} {
		if got := scrub(in, ""); got != want {
			t.Errorf("%q → %q，应为 %q", in, got, want)
		}
	}
}

func TestKeyWarningComparesHostOnly(t *testing.T) {
	p, _ := PresetByID("mimo")
	for u, warn := range map[string]bool{
		"https://token-plan-cn.xiaomimimo.com/v1/chat/completions": false,
		"https://TOKEN-PLAN-SGP.xiaomimimo.com/anthropic/v1":       false,
		"https://api.xiaomimimo.com/v1":                            true,
	} {
		if got := KeyWarning(p, u, "tp-abcdef0123456789") != ""; got != warn {
			t.Errorf("%s：提醒 %v，应为 %v", u, got, warn)
		}
	}
}

// 回传策略是 off 的服务商：收到的思考不存进历史（反正永远不发）；auto 照存。
func TestReasoningStoredOnlyWhenEchoed(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"好","reasoning_content":"想一想"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2}}`))
	})
	for echo, want := range map[EchoMode]bool{EchoOff: false, "": false, EchoAuto: true, EchoOn: true} {
		cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: "k", Model: "m", Echo: echo})
		conv, _ := cl.NewConversation("", nil)
		_ = conv.AddUser("在吗")
		res, err := cl.Chat(context.Background(), conv, nil)
		if err != nil || res.Reasoning != "想一想" {
			t.Fatalf("%q：%v %+v", echo, err, res)
		}
		_ = conv.AddResult(res)
		last := string(conv.msgs[len(conv.msgs)-1])
		if strings.Contains(last, "reasoning_content") != want {
			t.Errorf("回传策略 %q：历史里 %s", echo, last)
		}
	}
}

// 服务商把请求重定向到别的网站：不跟过去（Go 换网站时只去掉 Authorization 和 Cookie，api-key、x-api-key 会被带过去）。
func TestRedirectToOtherSiteNotFollowed(t *testing.T) {
	var leaked []http.Header
	other := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
		leaked = append(leaked, r.Header)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	// 同一台机器的另一个端口也算别的网站
	jump := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
		w.Header().Set("Location", other.srv.URL+r.Path)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	for _, c := range []struct {
		proto Protocol
		auth  AuthMode
	}{{OpenAI, AuthBoth}, {OpenAI, AuthAPIKey}, {Anthropic, ""}} {
		cl, _ := NewClient(Config{Protocol: c.proto, BaseURL: jump.srv.URL + "/v1", Key: "sk-test-0123456789", Model: "m", Auth: c.auth, HTTP: jump.srv.Client()})
		_, err := cl.ListModels(context.Background())
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != http.StatusTemporaryRedirect || !strings.Contains(ae.Hint(), "别的网站") {
			t.Errorf("%s %q：应报转到别的网站：%v", c.proto, c.auth, err)
		}
	}
	if len(leaked) != 0 {
		t.Fatalf("Key 被带到了别的网站：%v", leaked)
	}
	// 同一个网站里的重定向照常跟
	same := newFake(t, func(n int, r recorded, w http.ResponseWriter) {
		if n == 0 {
			w.Header().Set("Location", "/v1/models-moved")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		if r.Header.Get("api-key") != "sk-test-0123456789" {
			t.Errorf("同网站重定向后应照常带 Key：%v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	})
	cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: same.srv.URL + "/v1", Key: "sk-test-0123456789", Model: "m", Auth: AuthAPIKey, HTTP: same.srv.Client()})
	if ms, err := cl.ListModels(context.Background()); err != nil || len(ms) != 1 {
		t.Fatalf("同网站重定向：%v %v", err, ms)
	}
}

// 模型列表：界面上把自定义的鉴权改成「默认」还没保存，就按默认发，不退回已存的 api-key。
func TestModelsExplicitDefaultAuth(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	})
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), BaseURL: strp(f.srv.URL + "/v1"), Auth: strp("api-key"), Key: strp("sk-saved-0123456789")}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Models(ctx, ModelsRequest{Preset: CustomID, Protocol: OpenAI, BaseURL: f.srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Models(ctx, ModelsRequest{Preset: CustomID, Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Auth: strp("")}); err != nil {
		t.Fatal(err)
	}
	rs := f.requests()
	if rs[0].Header.Get("api-key") == "" || rs[0].Header.Get("Authorization") != "" {
		t.Errorf("没传 auth：沿用已存的 api-key：%v", rs[0].Header)
	}
	if rs[1].Header.Get("Authorization") == "" || rs[1].Header.Get("api-key") != "" {
		t.Errorf("传了空串：按默认发 Bearer：%v", rs[1].Header)
	}
}

// 报错里的 Key：错误类型字段里的也抹掉；先抹再截断，截断点切在 Key 中间也不留半截。
func TestScrubTypeAndTruncation(t *testing.T) {
	const key = "sk-mine-0123456789abcdef"
	f := newFake(t, func(n int, _ recorded, w http.ResponseWriter) {
		w.WriteHeader(400)
		if n == 0 {
			_, _ = w.Write([]byte(`{"error":{"type":"bad_key_` + key + `","message":"x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":{"message":"` + strings.Repeat("a", 490) + key + `"}}`))
	})
	cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: key, Model: "m"})
	for i := 0; i < 2; i++ {
		_, err := cl.ListModels(context.Background())
		if err == nil || strings.Contains(err.Error(), "0123456789") || strings.Contains(err.Error(), "sk-mine") {
			t.Errorf("第 %d 次：报错里还有 Key：%v", i, err)
		}
	}
}

// 默认端口写不写都是同一个地址（浏览器的 URL 会规范化掉，后端要和它一致，不然前后端认出的协议不一样）。
func TestSameEndpointDefaultPort(t *testing.T) {
	p, _ := PresetByID("mimo")
	ep, ok := p.matchURL("https://api.xiaomimimo.com:443/anthropic")
	if !ok || ep.Protocol != Anthropic {
		t.Fatalf("带 :443 的 Claude 协议地址：%+v %v", ep, ok)
	}
	if sameEndpoint("https://a.com:8443/v1", "https://a.com/v1") {
		t.Error("非默认端口不是同一个地址")
	}
}

// 重试事件带上给人看的那一句，界面不必显示英文原文。
func TestRetryEventHasHint(t *testing.T) {
	f := newFake(t, func(n int, _ recorded, w http.ResponseWriter) {
		if n == 0 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached for requests","type":"rate_limit_reached_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}]}`))
	})
	cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: "k", Model: "m", RetryDelay: 1})
	conv, _ := cl.NewConversation("", nil)
	_ = conv.AddUser("在吗")
	var hint string
	if _, err := cl.Chat(context.Background(), conv, func(e Event) {
		if e.Type == EvRetry {
			hint = e.Hint
		}
	}); err != nil {
		t.Fatal(err)
	}
	if hint != "请求太频繁，等一会儿再试" || HintOf(&APIError{Status: 401}) != "Key 不对或已失效" {
		t.Fatalf("重试的提示：%q", hint)
	}
}
