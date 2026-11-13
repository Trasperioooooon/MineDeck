package ai

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMiMoPresetAndKeyWarning(t *testing.T) {
	p, ok := PresetByID("mimo")
	if !ok || p.Protocol != OpenAI || p.BaseURL != "https://api.xiaomimimo.com/v1" || p.Auth != AuthBoth || p.Echo != EchoOn {
		t.Fatalf("MiMo 预设：%+v %v", p, ok)
	}
	if len(suggestedModels(p)) < 3 || suggestedModels(p)[0].ID != "mimo-v2.6-pro" {
		t.Fatalf("建议模型：%+v", suggestedModels(p))
	}
	ep, ok := p.matchURL("https://token-plan-cn.xiaomimimo.com/anthropic")
	if !ok || ep.Protocol != Anthropic || !ep.NoModels {
		t.Fatalf("Token Plan 的 Claude 地址：%+v %v", ep, ok)
	}
	if w := KeyWarning(p, "https://api.xiaomimimo.com/v1", "sk-abc"); w != "" {
		t.Errorf("按量 Key 配按量地址不该提醒：%s", w)
	}
	if w := KeyWarning(p, "https://token-plan-cn.xiaomimimo.com/v1", "sk-live-0123456789"); w == "" || !strings.Contains(w, "按量") {
		t.Errorf("按量 Key 配 Token Plan 应提醒：%q", w)
	}
	if w := KeyWarning(p, "https://api.xiaomimimo.com/anthropic", "tp-live-0123456789"); w == "" {
		t.Error("Token Plan Key 配按量地址应提醒")
	}
	if w := KeyWarning(p, "https://token-plan-sgp.xiaomimimo.com/v1", "ttp-team-0123456789"); w != "" {
		t.Errorf("团队 Key 配新加坡地址不该提醒：%s", w)
	}
	if w := KeyWarning(p, "https://api.xiaomimimo.com/v1", "not-a-prefixed-key"); w != "" {
		t.Errorf("认不出的前缀不提醒：%s", w)
	}
}

func TestModelsFallback(t *testing.T) {
	f := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
		if strings.Contains(r.Path, "/models") {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"message":"no such endpoint"}}`))
			return
		}
		w.WriteHeader(500)
	})
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: f.srv.Client()})
	ctx := context.Background()
	// Token Plan 文档没有 /models：不发请求
	ml, err := s.Models(ctx, ModelsRequest{Preset: "mimo", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Key: "tp-test-key"})
	if err != nil || ml.Source != "preset" || len(ml.Models) == 0 || len(f.requests()) != 0 {
		t.Fatalf("不该去请求：%v %+v 请求 %d", err, ml, len(f.requests()))
	}
	// 按量地址 404：退回建议，不把 404 当成失败
	_ = s.SetProvider(ProviderPatch{Preset: strp("mimo"), BaseURL: strp(f.srv.URL + "/v1"), Key: strp("sk-test-0123")})
	ml, err = s.Models(ctx, ModelsRequest{Preset: "mimo", BaseURL: f.srv.URL + "/v1"})
	if err != nil || ml.Source != "preset" || ml.Models[0].ID != "mimo-v2.6-pro" || ml.Warning == "" {
		t.Fatalf("404 应退回建议：%v %+v", err, ml)
	}
	if h := f.requests()[0].Header; h.Get("Authorization") == "" || h.Get("api-key") == "" {
		t.Errorf("MiMo 应同时带 Bearer 和 api-key：%v", h)
	}
}

func TestEchoReasoningDowngrade(t *testing.T) {
	f := newFake(t, func(n int, r recorded, w http.ResponseWriter) {
		if n == 0 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"想一想","tool_calls":[{"id":"c1","type":"function","function":{"name":"instance_list","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`))
			return
		}
		if n == 1 {
			if !strings.Contains(string(r.Body), "reasoning_content") {
				t.Errorf("自动策略应先带回思考：%s", r.Body)
			}
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"unknown field reasoning_content is not supported","type":"invalid_request_error"}}`))
			return
		}
		if strings.Contains(string(r.Body), "reasoning_content") {
			t.Errorf("对方拒绝后不应再带：%s", r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2}}`))
	})
	cl, err := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: "sk-test-0123456789", Model: "m", Echo: EchoAuto, RetryDelay: 1})
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := cl.NewConversation("", testTools)
	_ = conv.AddUser("列一下")
	res, err := cl.Chat(context.Background(), conv, nil)
	if err != nil || res.Reasoning != "想一想" || len(res.ToolCalls) != 1 {
		t.Fatalf("第一轮：%v %+v", err, res)
	}
	if err := conv.AddResult(res); err != nil {
		t.Fatal(err)
	}
	if err := conv.AddToolResults([]ToolResult{{ID: res.ToolCalls[0].ID, Content: "[]"}}); err != nil {
		t.Fatal(err)
	}
	res, err = cl.Chat(context.Background(), conv, nil)
	if err != nil || res.Text != "好" {
		t.Fatalf("降级后：%v %+v", err, res)
	}
	if len(f.requests()) != 3 {
		t.Fatalf("应是第一轮 + 被拒 + 去掉再发，实际 %d", len(f.requests()))
	}
}

func TestHintAndScrub(t *testing.T) {
	e := &APIError{Status: 429, Message: "insufficient_quota: sk-live-0123456789abcdef gone"}
	if e.Hint() != "服务商那边的额度用完了" {
		t.Errorf("额度：%s", e.Hint())
	}
	e = &APIError{Status: 429, Message: "rate limit"}
	if e.Hint() != "请求太频繁，等一会儿再试" {
		t.Errorf("频繁：%s", e.Hint())
	}
	e = &APIError{Status: 401, Message: "Incorrect API key provided"}
	if e.Hint() != "Key 不对或已失效" {
		t.Errorf("401：%s", e.Hint())
	}
	e = &APIError{Status: 400, Message: "The reasoning_content must be passed back"}
	if !strings.Contains(e.Hint(), "思考") {
		t.Errorf("400 思考：%s", e.Hint())
	}
	got := scrub("key sk-live-0123456789abcdef leaked", "sk-other-0123456789zzzz")
	if strings.Contains(got, "sk-live") || strings.Contains(got, "0123456789abcdef") {
		t.Errorf("没抹掉另一把 Key：%s", got)
	}
}
