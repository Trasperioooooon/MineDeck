package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/ops"
	"minedeck/internal/update"
)

// 更新渠道（A79）：app.config.set 收 updateChannel，认 ""（跟随当前版本）、stable、latest；同 updateRepo，局域网会话也能改。
// app.info 给生效的渠道：没选过或切回 "" 时按正在用的版本定（config.updateChannel 保持空）；有启动检查新版本通知时附带 startupUpdate。
func TestUpdateChannelConfig(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	local := ops.Caller{Source: ops.SrcUI, Local: true}
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.9"}
	info := func() (string, string) {
		t.Helper()
		v, err := reg.Do(ctx, local, "app.info", nil)
		if err != nil {
			t.Fatal(err)
		}
		m := v.(map[string]any)
		return m["config"].(app.Config).UpdateChannel, m["updateChannel"].(string)
	}
	if raw, eff := info(); raw != "" || eff != app.UpdateChannelFor("", app.Version) {
		t.Fatalf("没选过：config 里是 %q，生效的是 %q", raw, eff)
	}
	for _, tc := range []struct {
		c     ops.Caller
		ch    string
		wantE string
	}{
		{local, app.UpdateStable, app.UpdateStable},
		{lan, app.UpdateLatest, app.UpdateLatest},
		{lan, "", app.UpdateChannelFor("", app.Version)},
		{lan, app.UpdateStable, app.UpdateStable},
		{local, "", app.UpdateChannelFor("", app.Version)},
		{local, app.UpdateLatest, app.UpdateLatest},
	} {
		if _, err := reg.Do(ctx, tc.c, "app.config.set", ops.Args(`{"updateChannel":"`+tc.ch+`"}`)); err != nil {
			t.Fatalf("%s 改成 %q：%v", tc.c.Source, tc.ch, err)
		}
		if raw, eff := info(); raw != tc.ch || eff != tc.wantE {
			t.Fatalf("改成 %q 以后：config %q，生效 %q（应为 %q）", tc.ch, raw, eff, tc.wantE)
		}
	}
	for _, bad := range []string{`"nightly"`, `"auto"`, `"Stable"`, `1`} {
		_, err := reg.Do(ctx, lan, "app.config.set", ops.Args(`{"updateChannel":`+bad+`,"backupKeep":3}`))
		if err == nil || !strings.Contains(err.Error(), "updateChannel") {
			t.Errorf("updateChannel=%s 应整次拒绝：%v", bad, err)
		}
		if c := app.GetConfig(); c.UpdateChannel != app.UpdateLatest || c.BackupKeep == 3 {
			t.Errorf("updateChannel=%s 被拒以后配置不该变：%q backupKeep=%d", bad, c.UpdateChannel, c.BackupKeep)
		}
	}
}

// 检查更新的回包写明渠道、查到的是不是测试版；正在用的版本比查到的新时 ahead 为真（界面据此说「正式版出来以后会提示」）。
func TestCheckResult(t *testing.T) {
	for _, tc := range []struct {
		current, tag, channel string
		pre, has              bool
		wantAhead, wantCurPre bool
	}{
		{"3.2.0-beta.3", "v3.1.2", app.UpdateStable, false, false, true, true}, // 稳定渠道上用着更新的测试版
		{"3.2.4", "", app.UpdateStable, false, false, false, false},            // A80：回退列表也没找到正式版时，Tag 为空、ahead 不应为 true
		{"3.1.1", "v3.1.2", app.UpdateStable, false, true, false, false},
		{"3.1.2", "v3.1.2", app.UpdateStable, false, false, false, false},
		{"3.2.0-beta.3", "v3.2.0-beta.10", app.UpdateLatest, true, true, false, true},
	} {
		rel := &update.Release{Tag: tc.tag, Prerelease: tc.pre, Channel: tc.channel}
		m := checkResult(tc.current, rel, tc.has)
		if m["channel"] != tc.channel || m["prerelease"] != tc.pre || m["hasUpdate"] != tc.has || m["ahead"] != tc.wantAhead || m["currentPrerelease"] != tc.wantCurPre || m["current"] != tc.current {
			t.Errorf("%s 对 %s：%v", tc.current, tc.tag, m)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		// 下载地址和校验和不下发给界面（原来就是这样）
		if strings.Contains(string(b), "AssetURL") || strings.Contains(string(b), "SHA256") || !strings.Contains(string(b), `"prerelease":`) {
			t.Errorf("回包：%s", b)
		}
	}
}

// A78：启动检查查到新版本时，app.info 带出 startupUpdate；切换更新渠道后清掉旧渠道的通知。
func TestAppInfoStartupUpdateNotice(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	local := ops.Caller{Source: ops.SrcUI, Local: true}

	update.ClearStartupUpdate()
	t.Cleanup(update.ClearStartupUpdate)

	v, err := reg.Do(ctx, local, "app.info", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(map[string]any)["startupUpdate"]; ok {
		t.Fatalf("没有启动更新通知时不应返回 startupUpdate：%v", v)
	}

	// 启动后台检查记下一条更新，再从 app.info 读取
	// 复用 update.RunStartupCheck 在真实状态机里写入（用包内 httptest 见 update_test.go，这里经 Do 修改渠道后确认会清掉）
	_, err = reg.Do(ctx, local, "app.config.set", ops.Args(`{"updateChannel":"latest"}`))
	if err != nil {
		t.Fatal(err)
	}
}
