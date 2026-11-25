package web

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// A104：GET /api/instances 返回启动时配置损坏没列出来的目录（仅目录名和原因，不含完整路径）与 local 标记；
// 损坏目录的「打开文件夹」走 opendir 接口，仍受仅本机和禁止子目录的限制，且未登录局域网请求被 401 拦下。
func TestListInstancesIncludesBrokenAndLocal(t *testing.T) {
	e := newFullEnv(t)
	badDir := filepath.Join(app.ServersDir, "broken-json")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "instance.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	noNameDir := filepath.Join(app.ServersDir, "no-name")
	if err := os.MkdirAll(noNameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noNameDir, "instance.json"), []byte(`{"core":"paper"}`), 0o644); err != nil {
		t.Fatal(err)
	}

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

	// 未登录局域网请求不泄露出任何实例或损坏目录信息
	e.call(t, "GET", "/api/instances", "", 401, noToken, fromLAN)

	type listResp struct {
		OK     bool                  `json:"ok"`
		Data   []json.RawMessage     `json:"data"`
		Broken []inst.BrokenInstance `json:"broken"`
		Local  bool                  `json:"local"`
	}

	recLocal := e.call(t, "GET", "/api/instances", "", 200)
	var localGot listResp
	if err := json.Unmarshal(recLocal.Body.Bytes(), &localGot); err != nil {
		t.Fatal(err)
	}
	if !localGot.Local || len(localGot.Data) != 2 || len(localGot.Broken) != 2 {
		t.Fatalf("本机响应异常：%+v body=%s", localGot, recLocal.Body.String())
	}

	var brokenID string
	for _, b := range localGot.Broken {
		if strings.Contains(b.Folder, string(filepath.Separator)) || strings.Contains(b.Reason, app.ServersDir) || strings.Contains(b.ID, "/") {
			t.Fatalf("broken 项不应含磁盘完整路径或斜杠：%#v", b)
		}
		if b.Folder == "broken-json" {
			brokenID = b.ID
		}
	}
	if brokenID == "" {
		t.Fatalf("broken 里未找到 broken-json：%#v", localGot.Broken)
	}

	// 已登录的局域网访问：local=false
	recLAN := e.call(t, "GET", "/api/instances", "", 200, fromLAN)
	var lanGot listResp
	if err := json.Unmarshal(recLAN.Body.Bytes(), &lanGot); err != nil {
		t.Fatal(err)
	}
	if lanGot.Local || len(lanGot.Broken) != 2 {
		t.Fatalf("局域网响应异常：%+v", lanGot)
	}

	// 损坏目录的 opendir 在局域网仍被仅本机策略拒绝；本机传入 sub 也拒绝；未知编号返回错误
	e.call(t, "POST", "/api/instances/"+brokenID+"/opendir", "", 403, fromLAN)
	e.call(t, "POST", "/api/instances/"+brokenID+"/opendir?sub=mods", "", 400)
	e.call(t, "POST", "/api/instances/__broken__:missing/opendir", "", 400)

	// 修好一份、删掉另一份的配置后，列表接口不再继续报它们
	if err := os.WriteFile(filepath.Join(badDir, "instance.json"), []byte(`{"name":"修好了","core":"paper","mc":"1.21.4","launchTarget":"jar:server.jar"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(noNameDir, "instance.json")); err != nil {
		t.Fatal(err)
	}
	recAfter := e.call(t, "GET", "/api/instances", "", 200)
	var afterGot listResp
	if err := json.Unmarshal(recAfter.Body.Bytes(), &afterGot); err != nil {
		t.Fatal(err)
	}
	if len(afterGot.Broken) != 0 {
		t.Fatalf("修复和删除后 broken 应为空：%#v", afterGot.Broken)
	}
}
