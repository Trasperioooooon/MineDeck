package catalog

import (
	"strings"
	"testing"
)

// 遮掉的 Webhook 地址只留协议和主机（含端口）：路径、查询串、片段、地址里的用户名和密码一律不留（v3.1.0 正式版前检查 B-2）。
func TestMaskWebhook(t *testing.T) {
	for in, want := range map[string]string{
		"": "",
		"https://hook.example/x?access_token=tk-1":            "https://hook.example/***",
		"https://hook.example":                                "https://hook.example/***",
		"http://10.0.0.5:8080/send#frag-secret":               "http://10.0.0.5:8080/***",
		"https://user:pass-secret@hook.example:8443/a/b?c=d":  "https://hook.example:8443/***",
		"https://[::1]:9000/robot/send?access_token=tk-2":     "https://[::1]:9000/***",
		"https://oapi.dingtalk.com/robot/send?access_token=x": "https://oapi.dingtalk.com/***",
		"https://exa mple.com/x?token=tk-3":                   "***", // 解析不了的整个遮掉
		"hook.example/x?token=tk-4":                           "***", // 没写协议（手改过的配置）
	} {
		got := maskWebhook(in)
		if got != want {
			t.Errorf("maskWebhook(%q) = %q，应为 %q", in, got, want)
		}
		for _, secret := range []string{"tk-", "pass-secret", "frag-secret", "user:"} {
			if strings.Contains(got, secret) {
				t.Errorf("maskWebhook(%q) 里还有 %q", in, secret)
			}
		}
	}
}
