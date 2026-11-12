package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 局域网登录的人读不到完整的 Webhook 地址（v3.1.0 正式版前检查 B-2）：群机器人的令牌常常就写在地址里。
// 本机照旧看完整的地址；局域网那边保存设置时，原样传回遮掉的地址算「没改」，不能把真地址盖掉；
// 填全新的地址照常能存，清空照常能关，改了一半（还带着 ***）的不收。查询和保存的回包都要遮。
func TestWebhookURLMaskedForLAN(t *testing.T) {
	e := newFullEnv(t)
	full := app.GetConfig().WebhookURL
	if !strings.Contains(full, hookToken) {
		t.Fatalf("准备的 Webhook 地址里应带着令牌：%q", full)
	}
	type appInfo struct {
		Data struct {
			Config struct {
				WebhookURL string `json:"webhookUrl"`
				BackupKeep int    `json:"backupKeep"`
			} `json:"config"`
			WebhookURL string `json:"webhookUrl"` // PUT /api/config 的回包就是配置本身
			Local      *bool  `json:"local"`
		} `json:"data"`
	}
	decode := func(rec *httptest.ResponseRecorder) appInfo {
		t.Helper()
		var v appInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	const masked = "https://hook.example/***"

	// 读：本机看完整的，局域网只看到协议和主机
	if r := e.call(t, "GET", "/api/app", "", 200, fromUI); !strings.Contains(r.Body.String(), hookToken) {
		t.Errorf("本机应看到完整的地址：%s", r.Body)
	} else if v := decode(r); v.Data.Config.WebhookURL != full || v.Data.Local == nil || !*v.Data.Local {
		t.Errorf("本机：%+v", v.Data)
	}
	r := e.call(t, "GET", "/api/app", "", 200, fromLAN)
	if strings.Contains(r.Body.String(), hookToken) || strings.Contains(r.Body.String(), "access_token") {
		t.Errorf("局域网读到了地址里的令牌：%s", r.Body)
	}
	if v := decode(r); v.Data.Config.WebhookURL != masked || v.Data.Local == nil || *v.Data.Local {
		t.Errorf("局域网看到的应是 %q：%+v", masked, v.Data)
	}

	// 局域网原样传回遮掉的地址（同时改了别的设置）：地址不变，别的照改，回包也是遮掉的
	r = e.call(t, "PUT", "/api/config", `{"webhookUrl":"`+masked+`","backupKeep":7}`, 200, fromLAN)
	if got := app.GetConfig(); got.WebhookURL != full || got.BackupKeep != 7 {
		t.Errorf("原样传回遮掉的地址不该改掉真地址：%q backupKeep=%d", got.WebhookURL, got.BackupKeep)
	}
	if strings.Contains(r.Body.String(), hookToken) || decode(r).Data.WebhookURL != masked {
		t.Errorf("保存的回包也要遮：%s", r.Body)
	}
	// 改了一半：还带着 ***，不收，真地址不变
	for _, bad := range []string{masked + "x", "https://other.example/***", "***"} {
		r = e.call(t, "PUT", "/api/config", `{"webhookUrl":"`+bad+`"}`, 400, fromLAN)
		if !strings.Contains(r.Body.String(), "藏起来的部分") || app.GetConfig().WebhookURL != full {
			t.Errorf("%q 应拒绝、不改：%s %q", bad, r.Body, app.GetConfig().WebhookURL)
		}
	}
	// 填全新的地址：存进去；回包里也只有开头
	const fresh = "https://bot.example:8443/send?key=fresh-secret-42"
	r = e.call(t, "PUT", "/api/config", `{"webhookUrl":"`+fresh+`"}`, 200, fromLAN)
	if app.GetConfig().WebhookURL != fresh || decode(r).Data.WebhookURL != "https://bot.example:8443/***" || strings.Contains(r.Body.String(), "fresh-secret") {
		t.Errorf("局域网填新地址：存的是 %q，回包 %s", app.GetConfig().WebhookURL, r.Body)
	}
	e.call(t, "PUT", "/api/config", `{"webhookUrl":"ftp://x"}`, 400, fromLAN) // 格式照旧要查
	// 清空：关掉
	e.call(t, "PUT", "/api/config", `{"webhookUrl":""}`, 200, fromLAN)
	if app.GetConfig().WebhookURL != "" {
		t.Errorf("局域网清空应关掉：%q", app.GetConfig().WebhookURL)
	}

	// 本机照旧：完整地址原样读写，带 *** 的也照存（本机从来拿不到遮掉的地址，填什么就是什么）
	e.call(t, "PUT", "/api/config", `{"webhookUrl":"`+full+`"}`, 200, fromUI)
	if r := e.call(t, "GET", "/api/app", "", 200, fromUI); decode(r).Data.Config.WebhookURL != full {
		t.Errorf("本机写回完整地址：%s", r.Body)
	}
	e.call(t, "PUT", "/api/config", `{"webhookUrl":"https://hook.example/***"}`, 200, fromUI)
	if app.GetConfig().WebhookURL != "https://hook.example/***" {
		t.Errorf("本机填什么就存什么：%q", app.GetConfig().WebhookURL)
	}
}
