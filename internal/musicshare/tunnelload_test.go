package musicshare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/tunnel"
)

// tunnels.json 在却没读出来：音乐共享页不能说成「没有语音隧道」、引导去添加，状态里带上「隧道配置没读出来」
// （原文和给人看的那句，同隧道列表接口回 503 时的 error、hint）。用真的 tunnel.Manager：取状态时问 LoadErr 会在锁里再读一次，
// 文件修好以后不重启，下一次取状态就照常显示（界面上的「重试」就是再取一次状态）。
func TestRemoteVoiceUnknownWhileTunnelsUnread(t *testing.T) {
	oldBase, oldRT := app.Base, app.RuntimesDir
	t.Cleanup(func() { app.Base, app.RuntimesDir = oldBase, oldRT })
	app.Base = t.TempDir()
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	file := filepath.Join(app.Base, "tunnels.json")
	good := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"语音线","credential":"","proxyId":"9","boundInstance":"` + instName +
		`","purpose":"voice","publicAddr":"203.0.113.7:34567","autoStart":false}]}`
	if err := os.WriteFile(file, []byte(good[:60]), 0o600); err != nil { // 截掉一半：内容坏了
		t.Fatal(err)
	}
	m := tunnel.NewManager()
	e := newEnv(t)
	e.s.d.Tunnels = m // New 不起后台协程，这里换掉还没人在用

	rm := e.status().Remote
	if rm.State != "unknown" || len(rm.Tunnels) != 0 || rm.Addr != "" {
		t.Fatalf("没读出来时应是 unknown，不是「没有语音隧道」：%+v", rm)
	}
	if !strings.Contains(rm.LoadErr, "tunnels.json") || !strings.Contains(rm.LoadHint, "内容坏了") {
		t.Fatalf("应带上后端的原文和那句提示：err=%q hint=%q", rm.LoadErr, rm.LoadHint)
	}
	b, _ := json.Marshal(rm)
	if !strings.Contains(string(b), `"state":"unknown"`) || !strings.Contains(string(b), `"loadErr":`) || !strings.Contains(string(b), `"loadHint":`) {
		t.Fatalf("接口里的字段：%s", b)
	}

	// 修好文件：不重启，下一次取状态就读出来了
	if err := os.WriteFile(file, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	rm = e.status().Remote
	if rm.State == "unknown" || rm.LoadErr != "" || rm.LoadHint != "" || len(rm.Tunnels) != 1 || rm.Tunnels[0].Name != "语音线" {
		t.Fatalf("读出来以后应照常显示语音隧道：%+v", rm)
	}
	if rm.State != "idle" { // 有上次的地址，隧道停着、没勾跟随，voice_host 空着
		t.Fatalf("读出来以后的状态：%q", rm.State)
	}
	if err := m.LoadErr(); err != nil {
		t.Fatalf("隧道管理器那边也该恢复了：%v", err)
	}
	b, _ = json.Marshal(rm)
	if strings.Contains(string(b), "loadErr") || strings.Contains(string(b), "loadHint") {
		t.Fatalf("读出来以后不该再带这两项：%s", b)
	}
}

// 假隧道（只有 List、或者 LoadErr 一直是 nil）按读出来了算，照旧是 none；LoadErr 不是 tunnel.LoadError 时没有提示那句。
func TestRemoteVoiceStateWithFakeTunnels(t *testing.T) {
	e := newEnv(t)
	if rm := e.status().Remote; rm.State != "none" || rm.LoadErr != "" {
		t.Fatalf("没有语音隧道：%+v", rm)
	}
	e.tun.setLoadErr(&tunnel.LoadError{Err: os.ErrPermission})
	rm := e.status().Remote
	if rm.State != "unknown" || !strings.Contains(rm.LoadHint, "可能被别的程序占着") || !strings.Contains(rm.LoadErr, "读不出来") {
		t.Fatalf("读不出来（被占着）：%+v", rm)
	}
	e.tun.setLoadErr(nil)
	if rm := e.status().Remote; rm.State != "none" {
		t.Fatalf("恢复以后：%+v", rm)
	}
}
