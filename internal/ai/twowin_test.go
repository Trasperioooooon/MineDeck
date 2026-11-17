package ai

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 服务商设置的指纹只看存着的七项（预设、协议、地址、模型、思考深度、鉴权、思考回传）；
// 输出上限、Eager、Fallback、额度、单价、局域网助手开关变了，指纹不变。
func TestProviderRevFields(t *testing.T) {
	base := Settings{Preset: CustomID, Protocol: Anthropic, BaseURL: "https://relay.example", Model: "m", Effort: "high", Auth: "bearer", Echo: "auto"}
	r0 := providerRev(base)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(r0) || providerRev(base) != r0 {
		t.Fatalf("指纹应是固定的 16 个十六进制字符：%q", r0)
	}
	for name, mut := range map[string]func(*Settings){
		"预设": func(s *Settings) { s.Preset = "openai" }, "协议": func(s *Settings) { s.Protocol = OpenAI },
		"地址": func(s *Settings) { s.BaseURL += "/v1" }, "模型": func(s *Settings) { s.Model = "m2" },
		"思考深度": func(s *Settings) { s.Effort = "max" }, "鉴权": func(s *Settings) { s.Auth = "both" },
		"思考回传": func(s *Settings) { s.Echo = "on" },
		// 拼接方式不能让两项挪一个字就撞上同一个指纹
		"模型和思考深度之间挪一个字": func(s *Settings) { s.Model, s.Effort = "mh", "igh" },
	} {
		s := base
		mut(&s)
		if providerRev(s) == r0 {
			t.Errorf("改了%s，指纹应变", name)
		}
	}
	on, off := true, false
	for name, mut := range map[string]func(*Settings){
		"输出上限":     func(s *Settings) { s.MaxOutput = 8000 },
		"Eager":    func(s *Settings) { s.Eager = &on },
		"Fallback": func(s *Settings) { s.Fallback = &off },
		"额度":       func(s *Settings) { s.Quota = map[Feature]int64{FeatAssistant: 1} },
		"单价":       func(s *Settings) { s.Prices = []CustomPrice{{Model: "m", Currency: "USD"}} },
		"局域网助手":    func(s *Settings) { s.LANAssistant = &off },
	} {
		s := base
		mut(&s)
		if providerRev(s) != r0 {
			t.Errorf("改了%s，指纹不该变", name)
		}
	}
}

// 两个窗口同时开着「设置 → AI」：都读到同一个 rev；A 先换了模型存下，B 拿着旧 rev 换模型、顺带填了新 Key，
// 被拒（ErrStale），A 存的模型和原来的 Key 都没被动。别处改额度、单价、局域网开关、输出上限，
// 测试连接记下 Eager/Fallback，都不让还开着的页面自己跟自己冲突（2026-11-17）。
func TestProviderRevTwoWindows(t *testing.T) {
	f := newFake(t, func(_ int, r recorded, w http.ResponseWriter) {
		if strings.Contains(string(r.Body), "eager_input_streaming") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools.0.custom.eager_input_streaming: Extra inputs are not permitted"}}`))
			return
		}
		serve(t, w, "claude/tools.sse")
	})
	dir := t.TempDir()
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: dir, Keys: keys, HTTP: f.srv.Client()})
	if err := s.SetProvider(ProviderPatch{Preset: strp(CustomID), Protocol: (*Protocol)(strp("anthropic")), BaseURL: strp(f.srv.URL), Model: strp("claude-opus-5-5"), Key: strp("k-relay")}); err != nil {
		t.Fatal(err)
	}
	rev := s.View().Provider.Rev
	if rev == "" || s.View().Provider.Rev != rev {
		t.Fatalf("读两次应是同一个 rev：%q", rev)
	}

	// 别处改的这些不进指纹
	if err := s.SetQuota(map[Feature]int64{FeatAssistant: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrices([]CustomPrice{{Model: "m0", Currency: "USD", Input: 1, Output: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLANAssistant(false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProvider(ProviderPatch{MaxOutput: ptrTo(8000)}); err != nil {
		t.Fatal(err)
	}
	if before := s.View().Provider; before.Fallback || before.MaxOutput != 8000 {
		t.Fatalf("中转默认不开服务端回退，输出上限应存下：%+v", before)
	}
	if r, err := s.Test(context.Background()); err != nil || !r.OK {
		t.Fatalf("测试连接：%v %+v", err, r)
	}
	if p := s.View().Provider; !p.Fallback || p.Eager {
		t.Fatalf("测试连接应记下 Eager 关、Fallback 开：%+v", p)
	}
	if got := s.View().Provider.Rev; got != rev {
		t.Fatalf("额度、单价、局域网开关、输出上限、测试连接都不该改 rev：%q → %q", rev, got)
	}

	// 窗口 A 先存
	if err := s.SetProvider(ProviderPatch{Model: strp("claude-sonnet-5"), Rev: &rev}); err != nil {
		t.Fatalf("拿着当前的 rev 应能存：%v", err)
	}
	revA := s.View().Provider.Rev
	if revA == rev {
		t.Fatal("换了模型，rev 应变")
	}
	settingsBefore, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	// 窗口 B 拿着旧 rev，换模型还填了新 Key
	err := s.SetProvider(ProviderPatch{Model: strp("claude-haiku-5"), Key: strp("k-new"), Rev: &rev})
	if !errors.Is(err, ErrStale) || err.Error() != "AI 设置刚在别的窗口改过，这一页显示的还是旧的。重新读取以后再改" {
		t.Fatalf("拿旧 rev 保存应返回 ErrStale：%v", err)
	}
	if p := s.View().Provider; p.Model != "claude-sonnet-5" || p.Rev != revA {
		t.Errorf("被拒的保存不该改设置：%+v", p)
	}
	if keys.m[KeyName] != "k-relay" {
		t.Errorf("被拒的保存不该动 Key：%q", keys.m[KeyName])
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(after) != string(settingsBefore) {
		t.Errorf("被拒的保存不该写设置文件：\n%s\n→\n%s", settingsBefore, after)
	}
	// 停用也一样先比 rev：旧的不让停
	if err := s.SetProvider(ProviderPatch{Preset: strp(""), Rev: &rev}); !errors.Is(err, ErrStale) || !s.View().Enabled {
		t.Errorf("拿旧 rev 停用应被拒：%v", err)
	}
	// B 重新读取以后再存：成
	if err := s.SetProvider(ProviderPatch{Model: strp("claude-haiku-5"), Rev: &revA}); err != nil || s.View().Provider.Model != "claude-haiku-5" {
		t.Fatalf("重新读取以后应能存：%v", err)
	}
	// 不带 rev 照旧（AI 设置的老写法、清 Key、停用）
	if err := s.SetProvider(ProviderPatch{Key: strp("")}); err != nil || keys.m[KeyName] != "" {
		t.Errorf("不带 rev 清 Key：%v", err)
	}
}

func ptrTo[T any](v T) *T { return &v }
