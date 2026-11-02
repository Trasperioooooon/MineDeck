package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
)

// AI 设置的接口：没配置时没有任何对外连接（一拨号就报错的客户端证明），也不取价格；修改类的仅本机，局域网只能看；
// Key 用 DPAPI 加密存进 secrets.json（键名 ai.key），任何响应、设置文件和操作记录里都没有明文；
// 改了接口地址旧 Key 自动清掉；配好要花钱的服务商之后，才去 GitHub（经加速镜像）取价格。
func TestAISettingsAPI(t *testing.T) {
	e := newFullEnv(t)
	type view struct {
		Data struct {
			Enabled  bool   `json:"enabled"`
			KeySet   bool   `json:"keySet"`
			Local    bool   `json:"local"`
			Missing  string `json:"missing"`
			Presets  []any  `json:"presets"`
			Provider struct {
				Preset  string `json:"preset"`
				BaseURL string `json:"baseUrl"`
				Model   string `json:"model"`
			} `json:"provider"`
			Quota       map[string]int64 `json:"quota"`
			PriceNeeded bool             `json:"priceNeeded"`
		} `json:"data"`
	}
	get := func(opts ...reqOpt) view {
		t.Helper()
		var v view
		if err := json.Unmarshal(e.call(t, "GET", "/api/ai/settings", "", 200, opts...).Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 没配置：只能看、能存额度和自填单价，测试连接和刷新价格报「未启用」，全程不联网
	e.call(t, "GET", "/api/app", "", 200)
	if v := get(); v.Data.Enabled || !v.Data.Local || v.Data.Missing != "选择服务商" || len(v.Data.Presets) != 8 || v.Data.Provider.Preset != "" {
		t.Fatalf("没配置时：%+v", v.Data)
	}
	e.call(t, "PUT", "/api/ai/quota", `{"assistant":500000,"gamechat":20000}`, 200)
	e.call(t, "PUT", "/api/ai/prices", `{"prices":[{"model":"m","currency":"USD","input":1,"output":2,"cacheRead":0.1}]}`, 200)
	e.call(t, "PUT", "/api/ai/prices", `{"prices":[{"model":"m","currency":"EUR","input":1,"output":2}]}`, 400)
	e.call(t, "POST", "/api/ai/test", "", 400)
	e.call(t, "POST", "/api/ai/prices/refresh", "", 400)
	time.Sleep(50 * time.Millisecond)
	if d := e.dials.list(); len(d) != 0 {
		t.Fatalf("没配置 AI 却发出了连接：%v", d)
	}
	// 局域网：能看（local 为 false），不能改，也不能测试连接、取模型列表
	if v := get(fromLAN); v.Data.Local || v.Data.Quota["assistant"] != 500000 {
		t.Errorf("局域网看到的：%+v", v.Data)
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/ai/provider", `{"preset":"deepseek","key":"x"}`}, {"POST", "/api/ai/test", ""},
		{"POST", "/api/ai/models", `{"preset":"deepseek"}`}, {"PUT", "/api/ai/quota", `{"assistant":1}`},
		{"PUT", "/api/ai/prices", `{"prices":[]}`}, {"POST", "/api/ai/prices/refresh", ""},
	} {
		if !strings.Contains(e.call(t, c.method, c.path, c.body, 403, fromLAN).Body.String(), "仅支持在本机修改") {
			t.Errorf("%s %s 应仅本机", c.method, c.path)
		}
	}
	// 本机存 Key：加密进 secrets.json，响应里只有 keySet
	resp := e.call(t, "PUT", "/api/ai/provider", `{"preset":"deepseek","key":"  `+aiKey+`  "}`, 200, fromUI).Body.String()
	if strings.Contains(resp, aiKey) {
		t.Fatalf("响应里有 Key：%s", resp)
	}
	v := get()
	if !v.Data.Enabled || !v.Data.KeySet || v.Data.Provider.BaseURL != "https://api.deepseek.com" || v.Data.Provider.Model != "deepseek-v4-flash" || !v.Data.PriceNeeded {
		t.Fatalf("存好之后：%+v", v.Data)
	}
	var secrets map[string]string
	b, _ := os.ReadFile(filepath.Join(app.Base, "secrets.json"))
	if err := json.Unmarshal(b, &secrets); err != nil || !strings.HasPrefix(secrets["ai.key"], "dpapi:") || strings.Contains(string(b), aiKey) {
		t.Fatalf("secrets.json 里 ai.key 应是 DPAPI 密文：%s", b)
	}
	if settings, _ := os.ReadFile(filepath.Join(app.Base, "data", "ai", "settings.json")); strings.Contains(string(settings), aiKey) || !strings.Contains(string(settings), `"preset": "deepseek"`) {
		t.Errorf("settings.json：%s", settings)
	}
	// 配好要花钱的服务商之后才去取价格：先试两个加速镜像，最后直连 GitHub
	deadline := time.Now().Add(3 * time.Second)
	for len(e.dials.list()) < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if d := e.dials.list(); strings.Join(d, ",") != "ghfast.top:443,gh-proxy.com:443,raw.githubusercontent.com:443" {
		t.Errorf("取价格的连接：%v", d)
	}
	// 操作记录：改 Key 的那一条，Key 写成 ***
	logs, _ := filepath.Glob(filepath.Join(app.Base, "logs", "ops", "*.jsonl"))
	var found bool
	for _, f := range logs {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), aiKey) {
			t.Errorf("操作记录里有 Key")
		}
		found = found || strings.Contains(string(b), `"op":"ai.provider.set","src":"ui","actor":"本机","args":{"key":"***","preset":"deepseek"}`)
	}
	if !found {
		t.Error("改 AI 设置应写操作记录，Key 写成 ***")
	}
	// 改了接口地址：旧 Key 清掉，要重填
	e.call(t, "PUT", "/api/ai/provider", `{"baseUrl":"https://relay.example/v1"}`, 200)
	if v := get(); v.Data.KeySet || v.Data.Enabled || v.Data.Missing != "填写 Key" {
		t.Errorf("改了地址后旧 Key 应清掉：%+v", v.Data)
	}
	b, _ = os.ReadFile(filepath.Join(app.Base, "secrets.json"))
	if strings.Contains(string(b), "ai.key") {
		t.Errorf("secrets.json 里还有 ai.key：%s", b)
	}
	// 停用
	e.call(t, "PUT", "/api/ai/provider", `{"preset":""}`, 200)
	if v := get(); v.Data.Enabled || v.Data.Provider.Preset != "" {
		t.Errorf("停用后：%+v", v.Data)
	}
}
