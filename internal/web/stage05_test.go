package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/inst"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/tunnel"
)

// A08：GET /api/instances 保留接管进来的服务器，同时在 duplicates 里返回因重名没列出来的存放根文件夹。
func TestListInstancesIncludesSkippedDuplicates(t *testing.T) {
	e := newFullEnv(t)
	adoptedDir := filepath.Join(t.TempDir(), "adopted-s1")
	if err := os.MkdirAll(adoptedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sBytes, _ := json.Marshal(inst.Settings{
		Name: "生存服", Core: "paper", MC: "1.21.4", JavaMajor: 21, JavaPath: filepath.Join(app.Base, "no-java.exe"),
		XmxMB: 4096, XmsMB: 1024, LaunchTarget: "jar:server.jar", Adopted: true,
	})
	if err := os.WriteFile(filepath.Join(adoptedDir, "instance.json"), sBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	app.RememberInstanceDir(adoptedDir)

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.ShutdownAll(time.Second) })
	tun := tunnel.NewManager()
	aiSvc := ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai2"), Keys: app.Secrets(), HTTP: e.dials.client(), Mirrors: dl.GHMirrors})
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Accounts: tunnel.NewAccounts(tun, app.Secrets()), AI: aiSvc, Port: 27333})
	e.h = New(mgr, tun, reg, catalog.NewAssistant(reg, aiSvc, Token))
	e.mgr = mgr

	rec := e.call(t, "GET", "/api/instances", "", 200)
	var resp struct {
		OK   bool `json:"ok"`
		Data []struct {
			Name    string `json:"name"`
			Core    string `json:"core"`
			Dir     string `json:"dir"`
			Adopted bool   `json:"adopted"`
		} `json:"data"`
		Duplicates []inst.SkippedDuplicate `json:"duplicates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var gotSurv *struct {
		Name    string `json:"name"`
		Core    string `json:"core"`
		Dir     string `json:"dir"`
		Adopted bool   `json:"adopted"`
	}
	for idx := range resp.Data {
		if resp.Data[idx].Name == "生存服" {
			gotSurv = &resp.Data[idx]
			break
		}
	}
	if gotSurv == nil || !gotSurv.Adopted || gotSurv.Dir != adoptedDir || gotSurv.Core != "paper" {
		t.Fatalf("列表里的生存服应是接管进来的那台：%+v", gotSurv)
	}
	wantDup := []inst.SkippedDuplicate{{
		Name:    "生存服",
		Dir:     filepath.Join(app.ServersDir, "s1"),
		KeptDir: adoptedDir,
	}}
	if !slices.Equal(resp.Duplicates, wantDup) {
		t.Fatalf("duplicates = %+v，期望 %+v", resp.Duplicates, wantDup)
	}
}

// A28 + A25（保存参数校验）：启动设置页的预览接口 POST /api/instances/{name}/settings/preview 与保存接口 PUT .../settings
// 返回完全一致的补齐后参数与说明。
func TestSettingsPreviewAndSaveMatchOnLog4j(t *testing.T) {
	e := newFullEnv(t)
	if err := os.MkdirAll(app.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mani := `{"latest":{"release":"1.21.4"},"versions":[{"id":"1.21.4","type":"release"},{"id":"1.18.1","type":"release"},{"id":"1.17","type":"release"},{"id":"1.16.5","type":"release"},{"id":"1.12","type":"release"},{"id":"1.7","type":"release"}]}`
	if err := os.WriteFile(filepath.Join(app.CacheDir, "version_manifest_v2.json"), []byte(mani), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(app.ServersDir, "old116")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sBytes, _ := json.Marshal(inst.Settings{
		Name: "老服116", Core: "vanilla", MC: "1.16.5", JavaMajor: 17, JavaPath: filepath.Join(app.Base, "no-java.exe"),
		XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar", ExtraJVM: []string{"-Dlog4j2.formatMsgNoLookups=true"},
	})
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), sBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "log4j2_112-116.xml"), []byte("<Configuration>my-custom</Configuration>"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.ShutdownAll(time.Second) })
	tun := tunnel.NewManager()
	aiSvc := ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai3"), Keys: app.Secrets(), HTTP: e.dials.client(), Mirrors: dl.GHMirrors})
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Accounts: tunnel.NewAccounts(tun, app.Secrets()), AI: aiSvc, Port: 27333})
	e.h = New(mgr, tun, reg, catalog.NewAssistant(reg, aiSvc, Token))
	e.mgr = mgr

	body := `{"xmxMb":3072,"extraJvm":["-XX:+UseG1GC"]}`
	pRec := e.call(t, "POST", "/api/instances/%E8%80%81%E6%9C%8D116/settings/preview", body, 200)
	sRec := e.call(t, "PUT", "/api/instances/%E8%80%81%E6%9C%8D116/settings", body, 200)

	var pResp, sResp struct {
		Data inst.SettingsSaveResult `json:"data"`
	}
	if err := json.Unmarshal(pRec.Body.Bytes(), &pResp); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sRec.Body.Bytes(), &sResp); err != nil {
		t.Fatal(err)
	}
	wantJVM := []string{"-XX:+UseG1GC", "-Dlog4j2.formatMsgNoLookups=true"}
	if !slices.Equal(pResp.Data.ExtraJVM, wantJVM) || !slices.Equal(sResp.Data.ExtraJVM, wantJVM) {
		t.Fatalf("非官方配置不应加上引用：preview=%q saved=%q", pResp.Data.ExtraJVM, sResp.Data.ExtraJVM)
	}
	if !slices.Equal(pResp.Data.LaunchArgs, sResp.Data.LaunchArgs) {
		t.Fatalf("预览与保存的启动参数不一致：\npreview=%q\nsaved=%q", pResp.Data.LaunchArgs, sResp.Data.LaunchArgs)
	}
	if pResp.Data.Note == "" || pResp.Data.Note != sResp.Data.Note ||
		!strings.Contains(sResp.Data.Note, "等于没防") ||
		!strings.Contains(sResp.Data.Note, "-Dlog4j.configurationFile=log4j2_112-116.xml") {
		t.Fatalf("预览与保存结果都应包含等于没防及加引用说明：preview=%q saved=%q", pResp.Data.Note, sResp.Data.Note)
	}
}
