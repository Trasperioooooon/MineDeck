package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// A79：PUT /api/config 允许传空串 updateChannel 回到「跟随当前版本」，之后再传 stable/latest 仍可正常切换。
func TestConfigUpdateChannelResetHTTP(t *testing.T) {
	app.Base = t.TempDir()
	h := bareServer()
	call := func(method, path, body string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.RemoteAddr = "127.0.0.1:52000"
		req.Header.Set("X-Token", Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: HTTP %d（应为 %d）%s", method, path, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	call("PUT", "/api/config", `{"updateChannel":"latest"}`, http.StatusOK)
	if got := app.GetConfig().UpdateChannel; got != app.UpdateLatest {
		t.Fatalf("设成 latest 后应为 %q，实际 %q", app.UpdateLatest, got)
	}
	call("PUT", "/api/config", `{"updateChannel":""}`, http.StatusOK)
	if got := app.GetConfig().UpdateChannel; got != "" {
		t.Fatalf("设回空串后应为 \"\"，实际 %q", got)
	}
	var resp struct {
		Data struct {
			UpdateChannel string `json:"updateChannel"`
			Config        struct {
				UpdateChannel string `json:"updateChannel"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(call("GET", "/api/app", "", http.StatusOK)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Config.UpdateChannel != "" || resp.Data.UpdateChannel != app.UpdateChannelFor("", app.Version) {
		t.Fatalf("回退后 /api/app 不对：%+v", resp.Data)
	}
}
