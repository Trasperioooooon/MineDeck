package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
)

// 「Windows 安全中心排除项」的记录（config.defenderExclusion）只由窗口宿主写（cmd/minedeck/defender_windows.go）。
// 设置页从 GET /api/app 的 config 里读到它；PUT /api/config 带着这个键改不了它（接口收不收这个键都行，记录要原样），
// AI 助手、MCP、局域网登录的手机走的也是这条路，同样改不了。
func TestDefenderExclusionReadOnlyOverAPI(t *testing.T) {
	e := newFullEnv(t)
	at := time.Date(2026, 11, 27, 14, 3, 0, 0, time.UTC)
	set := func(r *app.DefenderExclusion) {
		t.Helper()
		if _, err := app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = r; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	set(&app.DefenderExclusion{Path: app.Base, At: at})
	t.Cleanup(func() { set(nil) })

	var got struct {
		Data struct {
			Base   string `json:"base"`
			Config struct {
				DefenderExclusion *app.DefenderExclusion `json:"defenderExclusion"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", "/api/app", "", 200).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if r := got.Data.Config.DefenderExclusion; r == nil || r.Path != app.Base || !r.At.Equal(at) || got.Data.Base != app.Base {
		t.Fatalf("设置页应从 /api/app 读到记录和程序数据文件夹：%+v，base %q", r, got.Data.Base)
	}

	for _, body := range []string{
		`{"defenderExclusion":null}`,
		`{"defenderExclusion":{"path":"C:\\","at":"2026-01-01T00:00:00Z"}}`,
		`{"backupKeep":7,"defenderExclusion":{"path":"D:\\x"}}`,
	} {
		req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.RemoteAddr = "127.0.0.1:52000"
		req.Header.Set("X-Token", Token)
		e.h.ServeHTTP(httptest.NewRecorder(), req)
		if r := app.GetConfig().DefenderExclusion; r == nil || r.Path != app.Base || !r.At.Equal(at) {
			t.Fatalf("PUT /api/config %s 改动了排除项的记录：%+v", body, r)
		}
	}

	// 撤销以后（宿主清掉记录）配置里就没有这个键：设置页据此显示「没加入」
	set(nil)
	if b := e.call(t, "GET", "/api/app", "", 200).Body.String(); strings.Contains(b, "defenderExclusion") {
		t.Errorf("没有记录时不该带这个键：%s", b)
	}
}
