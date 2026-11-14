package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/tunnel"
)

// tunnels.json 写不进去时，新增隧道的接口报 400，响应里带一句给人看的 hint（界面的报错提示接这一句），
// error 里是完整原文；列表里没有多出这条。以前写盘的错误被吞掉，接口照样回 200，重启以后隧道没了。
// 用只读的 tunnels.json 制造写不进去：只读属性挡住改名是 Windows 的行为。
func TestAddTunnelReportsSaveFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读属性挡住覆盖是 Windows 的行为")
	}
	e := newFullEnv(t)
	before := len(e.tunnels(t))
	path := filepath.Join(app.Base, "tunnels.json")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	rec := e.call(t, "POST", "/api/tunnels", `{"provider":"openfrp","name":"新隧道","credential":"openfrp-key-xyz","proxyId":"42"}`, 400)
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.OK || !strings.Contains(body.Error, "没能写进磁盘") || body.Hint != (&tunnel.SaveError{}).Hint() {
		t.Fatalf("写盘失败的响应：%s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "openfrp-key-xyz") {
		t.Fatal("报错里带出了凭据")
	}
	if after := len(e.tunnels(t)); after != before {
		t.Fatalf("写盘失败后列表从 %d 条变成 %d 条", before, after)
	}
}
