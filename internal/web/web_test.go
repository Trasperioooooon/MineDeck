package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/ops/catalog"
)

// bareServer 不带实例和隧道的路由（和 main 一样：注册表和助手共用一个 AI 接入层），程序目录取当时的 app.Base。
func bareServer() http.Handler {
	aiSvc := catalog.NewAI()
	reg := catalog.New(catalog.Env{AI: aiSvc, Port: 27333})
	return New(nil, nil, reg, catalog.NewAssistant(reg, aiSvc, Token))
}

// 下发配置的响应一律不得含访问密码哈希（回归：PUT /api/config 曾原样回传整个 app.Config，
// 每次保存设置都把 accessPasswordHash 发给前端，局域网登录的手机也不例外）。
func TestConfigResponsesHidePasswordHash(t *testing.T) {
	app.Base = t.TempDir() // config.json 落到临时目录，不污染工作区
	h := bareServer()
	call := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.Header.Set("X-Token", Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: HTTP %d %s", method, path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}

	put := call("PUT", "/api/config", `{"lanPassword":"correct horse","listenLan":true}`)
	hash := app.GetConfig().AccessPasswordHash
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("密码未存为 bcrypt 哈希: %q", hash)
	}
	got := call("GET", "/api/app", "")
	for name, body := range map[string]string{"PUT /api/config": put, "GET /api/app": got} {
		if strings.Contains(body, "accessPasswordHash") || strings.Contains(body, hash) {
			t.Errorf("%s 响应泄露了密码哈希: %s", name, body)
		}
	}

	// 前端仍须拿到保存后的配置，并能经 lanSet 得知已设密码
	var p struct {
		Data app.Config `json:"data"`
	}
	if err := json.Unmarshal([]byte(put), &p); err != nil || !p.Data.ListenLAN {
		t.Errorf("PUT /api/config 未返回保存后的配置: %s", put)
	}
	var a struct {
		Data struct {
			Config app.Config `json:"config"`
			LanSet bool       `json:"lanSet"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &a); err != nil || !a.Data.LanSet || !a.Data.Config.ListenLAN {
		t.Errorf("GET /api/app 未如实告知密码状态: %s", got)
	}
}

// 内嵌字体：按 font/woff2 下发，带 ETag 且允许缓存（每次先问一下）；同一 ETag 再请求回 304，不重下 1.9 MB。
// 页面与脚本仍然 no-store，改了前端立即生效。
func TestFontsServedWithETag(t *testing.T) {
	app.Base = t.TempDir()
	h := bareServer()
	get := func(path, inm string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1"
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, f := range []string{"Geist-latin.woff2", "GeistMono-latin.woff2", "NotoSansSC-gb2312.woff2"} {
		rec := get("/fonts/"+f, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d", f, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
			t.Errorf("%s: Content-Type = %q", f, ct)
		}
		tag := rec.Header().Get("ETag")
		if !strings.HasPrefix(tag, `"`) || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: ETag = %q, Cache-Control = %q", f, tag, rec.Header().Get("Cache-Control"))
		}
		if body := rec.Body.Bytes(); len(body) < 4 || string(body[:4]) != "wOF2" {
			t.Errorf("%s: 不是 WOFF2 文件", f)
		}
		if again := get("/fonts/"+f, tag); again.Code != http.StatusNotModified {
			t.Errorf("%s: 带 If-None-Match 再请求应回 304，实际 %d", f, again.Code)
		}
	}
	if rec := get("/fonts/fonts.css", ""); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("fonts.css: HTTP %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cc := get("/app.css", "").Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("页面应保持 no-store，实际 %q", cc)
	}
}

// chromeColor 只收 #rrggbb（存为小写），格式不对整次拒绝、原值不变；空串清除。
func TestConfigChromeColor(t *testing.T) {
	app.Base = t.TempDir()
	h := bareServer()
	put := func(body string) int {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.Header.Set("X-Token", Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := put(`{"chromeColor":"#EAE5D7"}`); code != http.StatusOK || app.GetConfig().ChromeColor != "#eae5d7" {
		t.Fatalf("合法颜色未保存：HTTP %d，存为 %q", code, app.GetConfig().ChromeColor)
	}
	for _, bad := range []string{`"red"`, `"#fff"`, `"#12345g"`, `"url(x)"`, `" #202020"`} {
		if code := put(`{"chromeColor":` + bad + `,"backupKeep":5}`); code != http.StatusBadRequest {
			t.Errorf("chromeColor=%s 应返回 400，实际 %d", bad, code)
		}
	}
	if c := app.GetConfig(); c.ChromeColor != "#eae5d7" || c.BackupKeep == 5 {
		t.Errorf("被拒绝的请求不应改动配置：chromeColor=%q backupKeep=%d", c.ChromeColor, c.BackupKeep)
	}
	if code := put(`{"chromeColor":""}`); code != http.StatusOK || app.GetConfig().ChromeColor != "" {
		t.Errorf("空串应清除：HTTP %d，仍为 %q", code, app.GetConfig().ChromeColor)
	}
}

// CurseForge API Key 只能写、不能读：保存后任何响应都不含 Key，改由 cfKeySet 告知是否已设置；
// config.json 里也没有明文（加密存在 secrets.json）。传空串仍是清除。
func TestConfigResponsesHideCFKey(t *testing.T) {
	app.Base = t.TempDir()
	h := bareServer()
	call := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.Header.Set("X-Token", Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: HTTP %d %s", method, path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	type appInfo struct {
		Data struct {
			CFKeySet    bool `json:"cfKeySet"`
			CFKeyBroken bool `json:"cfKeyBroken"`
		} `json:"data"`
	}
	state := func() appInfo {
		var a appInfo
		if err := json.Unmarshal([]byte(call("GET", "/api/app", "")), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	if a := state(); a.Data.CFKeySet || a.Data.CFKeyBroken {
		t.Fatalf("初始应为未设置：%+v", a)
	}
	const key = "cf-secret-0123456789abcdef"
	put := call("PUT", "/api/config", `{"cfApiKey":"  `+key+`  ","backupKeep":7}`)
	got := call("GET", "/api/app", "")
	for name, body := range map[string]string{"PUT /api/config": put, "GET /api/app": got} {
		if strings.Contains(body, key) {
			t.Errorf("%s 响应里有 Key：%s", name, body)
		}
	}
	if a := state(); !a.Data.CFKeySet || a.Data.CFKeyBroken {
		t.Errorf("保存后 cfKeySet 应为 true：%+v", a)
	}
	if k := app.CFKey(); k != key {
		t.Errorf("保存的 Key 不对（应去掉首尾空白）：%q", k)
	}
	if b, _ := os.ReadFile(filepath.Join(app.Base, "config.json")); strings.Contains(string(b), key) || !strings.Contains(string(b), `"backupKeep": 7`) {
		t.Errorf("config.json 不对：%s", b)
	}
	call("PUT", "/api/config", `{"backupKeep":8}`) // 不带 cfApiKey：不动 Key
	if app.CFKey() != key {
		t.Error("没传 cfApiKey 却改动了 Key")
	}
	call("PUT", "/api/config", `{"cfApiKey":""}`)
	if a := state(); a.Data.CFKeySet || app.CFKey() != "" {
		t.Errorf("传空串应清除 Key：%+v", a)
	}
}
