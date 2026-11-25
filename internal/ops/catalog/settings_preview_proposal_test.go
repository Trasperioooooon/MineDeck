package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// A28 + A25（保存参数校验）：AI 助手提出修改启动参数（instance.settings.set）时，确认卡片里展示的 JVM 参数就是实际会存、会用的参数
// （含自动补上的 log4shell 参数）；遇到同名非官方日志配置文件时不强加引用，并在确认卡片的 warning 里说明。
func TestSettingsProposalPreviewMatchesSavedLog4jArgs(t *testing.T) {
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(app.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mani := `{"latest":{"release":"1.21.4"},"versions":[{"id":"1.21.4","type":"release"},{"id":"1.18.1","type":"release"},{"id":"1.17","type":"release"},{"id":"1.16.5","type":"release"},{"id":"1.12","type":"release"},{"id":"1.7","type":"release"}]}`
	if err := os.WriteFile(filepath.Join(app.CacheDir, "version_manifest_v2.json"), []byte(mani), 0o644); err != nil {
		t.Fatal(err)
	}

	dir17 := filepath.Join(app.ServersDir, "mc117")
	dirCustom := filepath.Join(app.ServersDir, "mc116-custom")
	for _, item := range []struct {
		dir  string
		name string
		mc   string
		jvm  []string
	}{
		{dir17, "老服117", "1.17", []string{"-Dlog4j2.formatMsgNoLookups=true"}},
		{dirCustom, "自定义日志116", "1.16.5", []string{"-Dlog4j2.formatMsgNoLookups=true"}},
	} {
		if err := os.MkdirAll(item.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(inst.Settings{
			Name: item.name, Core: "vanilla", MC: item.mc, JavaMajor: 17, JavaPath: filepath.Join(app.Base, "java.exe"),
			XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar", ExtraJVM: item.jvm,
		})
		if err := os.WriteFile(filepath.Join(item.dir, "instance.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dirCustom, "log4j2_112-116.xml"), []byte("<Configuration>custom</Configuration>"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.ShutdownAll(time.Second) })

	reg := New(Env{Mgr: mgr, Port: 27333})
	ctx := context.Background()
	aiCaller := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s-log4j", Local: true}
	uiCaller := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}

	// 1) AI 只传 ["-XX:+UseG1GC"]：确认卡片里显示的 After 必须包含自动补上的 -Dlog4j2.formatMsgNoLookups=true，与确认执行后保存的参数完全一致
	res, err := reg.Do(ctx, aiCaller, "instance.settings.set", ops.Args(`{"inst":"老服117","extraJvm":["-XX:+UseG1GC"]}`))
	if err != nil {
		t.Fatal(err)
	}
	prop, ok := res.(*ops.Proposal)
	if !ok {
		t.Fatalf("AI 调用写操作应生成提案，实际：%T", res)
	}
	wantAfter := "-XX:+UseG1GC -Dlog4j2.formatMsgNoLookups=true"
	if len(prop.Plan.Changes) != 1 || prop.Plan.Changes[0].What != "JVM 参数" || prop.Plan.Changes[0].After != wantAfter {
		t.Fatalf("确认卡片里的 JVM 参数应显示补齐后的实际值 %q，实际：%+v", wantAfter, prop.Plan.Changes)
	}
	if _, err := reg.Confirm(ctx, uiCaller, prop.ID, false); err != nil {
		t.Fatal(err)
	}
	i17, _ := mgr.Get("老服117")
	if got := strings.Join(i17.ExtraJVMSnapshot(), " "); got != wantAfter {
		t.Fatalf("确认执行后实际保存的参数 %q 与确认卡片显示的 %q 不一致", got, wantAfter)
	}

	// 2) 文件夹里放着非官方的 log4j2_112-116.xml：确认卡片不加 -Dlog4j.configurationFile= 引用，且 Warning 给出说明
	res2, err := reg.Do(ctx, aiCaller, "instance.settings.set", ops.Args(`{"inst":"自定义日志116","extraJvm":["-XX:+UseG1GC"]}`))
	if err != nil {
		t.Fatal(err)
	}
	prop2 := res2.(*ops.Proposal)
	if len(prop2.Plan.Changes) != 1 || prop2.Plan.Changes[0].After != wantAfter {
		t.Fatalf("非官方日志配置不应在确认卡片里强加引用，实际：%+v", prop2.Plan.Changes)
	}
	if !strings.Contains(prop2.Plan.Warning, "等于没防") || !strings.Contains(prop2.Plan.Warning, "-Dlog4j.configurationFile=log4j2_112-116.xml") {
		t.Fatalf("确认卡片 Warning 应说明等于没防及手动加引用的办法，实际：%q", prop2.Plan.Warning)
	}
	decOut, err := reg.Confirm(ctx, uiCaller, prop2.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	iCustom, _ := mgr.Get("自定义日志116")
	if !slices.Equal(iCustom.ExtraJVMSnapshot(), []string{"-XX:+UseG1GC", "-Dlog4j2.formatMsgNoLookups=true"}) {
		t.Fatalf("保存后不应带官方配置文件引用：%q（decide=%+v）", iCustom.ExtraJVMSnapshot(), decOut)
	}
}
