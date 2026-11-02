package web

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/assistant"
	"minedeck/internal/dl"
	"minedeck/internal/inst"
	"minedeck/internal/mcp"
	"minedeck/internal/ops"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/tunnel"
)

// fullEnv 一套完整的后端，离线可跑：临时程序目录里两个停止的实例（vanilla 核心：搜索 / 安装模组在联网之前就会被拒；
// 没有 Java：启动在起进程之前就会失败）、一条樱花隧道（启动在下载 frpc 之前就会失败）；
// 设好了 CurseForge Key、访问密码（开着局域网访问）和 Webhook。AI 接入层用一拨号就报错的 HTTP 客户端，
// 记下它想连的地址；Key 用真的 DPAPI 存进 secrets.json。
type fullEnv struct {
	h       http.Handler
	secrets []string // 任何响应里都不能出现的东西
	tunID   string
	dials   *noDial
	asst    *assistant.Service
	mcp     *mcp.Service // MCP 接口（默认关着）
}

// noDial 一拨号就报错，记下想连的地址：用来证明没配置 AI 时没有任何对外连接。
type noDial struct {
	mu    sync.Mutex
	addrs []string
	allow string // 这个地址照常连：本机的假 AI 服务（第三阶段的测试用）
}

func (d *noDial) client() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		d.mu.Lock()
		if d.allow != "" && addr == d.allow {
			d.mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		d.addrs = append(d.addrs, addr)
		d.mu.Unlock()
		return nil, errors.New("测试里不许联网")
	}}}
}

func (d *noDial) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

const (
	aiKey     = "sk-ai-secret-key-8e1b"
	cfKey     = "cf-secret-key-4f8a"
	lanPass   = "lan-secret-pw-9c1d"
	sakuraKey = "sakura-secret-key-7b2e"
	frpsToken = "frps-secret-token-3a6f"
	hookToken = "webhook-secret-token-5d0c"
)

func newFullEnv(t *testing.T) *fullEnv {
	t.Helper()
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
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, dir := range map[string]string{"生存服": "s1", "删除测试": "s2"} {
		d := filepath.Join(app.ServersDir, dir)
		s, _ := json.Marshal(inst.Settings{Name: name, Core: "vanilla", MC: "1.21.1", JavaMajor: 21, JavaPath: filepath.Join(app.Base, "no-java.exe"),
			XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar"})
		write(filepath.Join(d, "instance.json"), string(s))
		write(filepath.Join(d, "server.properties"), "motd=hello\nmax-players=20\nonline-mode=false\nserver-port=25565\n")
		write(filepath.Join(d, "world", "level.dat"), "level")
	}
	write(filepath.Join(app.Base, "bg", "x.png"), "png")
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	tun := tunnel.NewManager()
	tt, err := tun.Add(tunnel.Tunnel{Provider: "natfrp", Name: "樱花", Credential: sakuraKey, ProxyID: "9", LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetCFKey(cfKey); err != nil {
		t.Fatal(err)
	}
	nd := &noDial{}
	aiSvc := ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai"), Keys: app.Secrets(), HTTP: nd.client(), Mirrors: dl.GHMirrors})
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, AI: aiSvc, Port: 27333})
	asst := catalog.NewAssistant(reg, aiSvc, Token)
	mcpSvc := catalog.NewMCP(reg, Token)
	mcpSvc.Start(27333, 27333)
	e := &fullEnv{h: New(mgr, tun, reg, asst, WithMCP(mcpSvc)), tunID: tt.ID, dials: nd, asst: asst, mcp: mcpSvc}
	e.call(t, "PUT", "/api/config", `{"lanPassword":"`+lanPass+`","listenLan":true,"webhookUrl":"https://hook.example/x?access_token=`+hookToken+`"}`, 200)
	e.secrets = []string{cfKey, lanPass, sakuraKey, frpsToken, app.GetConfig().AccessPasswordHash}
	return e
}

type reqOpt func(*http.Request)

var (
	fromUI  reqOpt = func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }
	fromLAN reqOpt = func(r *http.Request) { r.RemoteAddr = "192.168.1.23:52000" }
	noToken reqOpt = func(r *http.Request) { r.Header.Del("X-Token") }
)

// call 发一个请求（默认本机、带令牌），检查状态码，返回响应。
func (e *fullEnv) call(t *testing.T, method, path, body string, want int, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	for _, o := range opts {
		o(req)
	}
	if strings.HasSuffix(path, "/console") { // SSE 一直开着：请求的上下文先取消，回放完就返回
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Errorf("%s %s：HTTP %d，应为 %d：%s", method, path, rec.Code, want, rec.Body)
	}
	return rec
}

// pattern 把具体路径还原成路由表里的写法：实例名换成 {name}，隧道、任务编号换成 {id}，去掉查询串。
func pattern(path string) string {
	parts := strings.Split(strings.Split(path, "?")[0], "/")
	if len(parts) > 3 && parts[1] == "api" {
		switch parts[2] {
		case "instances":
			parts[3] = "{name}"
		case "tunnels", "tasks":
			parts[3] = "{id}"
		}
	}
	if len(parts) == 3 && parts[1] == "bg" {
		parts[2] = "{name}"
	}
	return strings.Join(parts, "/")
}

// 现有 47 个接口的路径、方法和语义不变：逐个调一遍，状态码、错误信息和原来一致，响应仍是 {"ok":…,"data"/"error":…}。
// 会联网或起进程的接口都挑了在那之前就会返回的参数。
func TestRoutesKeepPathsAndSemantics(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服
	cases := []struct {
		method, path, body string
		code               int
		errHas             string // 失败时 error 里应有的文字
		opts               []reqOpt
	}{
		{"GET", "/api/app", "", 200, "", nil},
		{"GET", "/api/bg", "", 200, "", nil},
		{"POST", "/api/pickdir", "", 403, "文件夹选择仅支持在本机操作（远程管理时请手动填写路径）", []reqOpt{fromLAN}},
		{"POST", "/api/openpath", `{"path":"C:\\"}`, 403, "打开目录仅支持在本机操作", []reqOpt{fromLAN}},
		{"POST", "/api/openpath", `{"path":"` + strings.ReplaceAll(filepath.Join(app.Base, "nope"), `\`, `\\`) + `"}`, 400, "目录不存在", nil},
		{"POST", "/api/login", `{"password":"wrong"}`, 401, "密码错误", []reqOpt{fromLAN, noToken}},
		{"POST", "/api/shutdown", "", 200, "", nil},
		{"PUT", "/api/config", `{"backupKeep":5000}`, 200, "", nil},
		{"PUT", "/api/config", `{"serversDir":""}`, 403, "存放目录仅支持在本机修改", []reqOpt{fromLAN}},
		{"PUT", "/api/config", `{"webhookUrl":"ftp://x"}`, 400, "Webhook 地址需以 http(s):// 开头", nil},
		{"PUT", "/api/config", ``, 400, "EOF", nil},
		{"POST", "/api/update/check", "", 502, "未配置更新仓库", nil},
		{"POST", "/api/update/apply", "", 502, "未配置更新仓库", nil},
		{"GET", ip + "/resources/search?q=jei", "", 400, "该核心不支持安装模组/插件", nil},
		{"GET", "/api/instances/nope/resources/search?q=jei", "", 404, "实例不存在", nil},
		{"POST", ip + "/resources/install", `{}`, 400, "缺少项目 ID", nil},
		{"POST", ip + "/resources/install", `{"projectId":"abc"}`, 400, "该核心不支持安装模组/插件", nil},
		{"GET", "/api/cores", "", 200, "", nil},
		{"GET", "/api/mcversions?core=bogus", "", 400, "未知核心: bogus", nil},
		{"GET", "/api/builds?core=paper", "", 400, "参数缺失", nil},
		{"GET", "/api/javas", "", 200, "", nil},
		{"POST", "/api/javas/scan", "", 200, "", nil},
		{"POST", "/api/javas/add", `{"path":"C:\\java.exe"}`, 403, "添加本机 Java 仅支持在本机操作", []reqOpt{fromLAN}},
		{"GET", "/api/instances", "", 200, "", nil},
		{"POST", "/api/instances", `{"name":"","core":"paper","mc":"1.21.4","eula":true}`, 400, "实例名需为 1~40 个字符", nil},
		{"POST", "/api/import/modpack", "", 400, "上传解析失败", nil},
		{"GET", "/api/tasks/t999", "", 404, "任务不存在", nil},
		{"GET", ip + "/backups", "", 200, "", nil},
		{"POST", ip + "/backups", "", 200, "", nil}, // 请求体可省略
		{"POST", ip + "/backups/restore", `{}`, 400, "缺少备份文件名", nil},
		{"DELETE", ip + "/backups", "", 400, "缺少备份文件名", nil},
		{"DELETE", ip + "/backups?file=nope.zip", "", 400, "备份文件不存在", nil},
		{"GET", ip + "/players", "", 200, "", nil},
		{"POST", ip + "/players", `{"action":"whitelist-add","player":"Steve"}`, 200, "", nil},
		{"POST", ip + "/players", `{"action":"fly","player":"Steve"}`, 400, "未知操作: fly", nil},
		{"POST", ip + "/players", `{"action":"op","player":"bad name"}`, 400, "玩家名不合法", nil},
		{"GET", ip + "/policies", "", 200, "", nil},
		{"PUT", ip + "/policies", `{"crashRestart":true,"schedules":[{"type":"backup","at":"04:00"}]}`, 200, "", nil},
		{"PUT", ip + "/policies", `{"schedules":[{"type":"dance","at":"04:00"}]}`, 400, "未知任务类型: dance", nil},
		{"GET", "/api/tunnels", "", 200, "", nil},
		{"POST", "/api/tunnels", `{"provider":"custom","name":"自建","credential":"` + frpsToken + `","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}`, 200, "", nil},
		{"POST", "/api/tunnels", `{"provider":"bogus","name":"x"}`, 400, "未知服务商", nil},
		{"PUT", "/api/tunnels/" + e.tunID, `{"provider":"natfrp","name":"樱花 2","proxyId":"9","credential":"` + sakuraKey + `","localPort":25565}`, 200, "", nil},
		{"DELETE", "/api/tunnels/tun999", "", 400, "隧道不存在", nil},
		{"POST", "/api/tunnels/" + e.tunID + "/start", "", 400, "首次使用需手动放置樱花 frpc", nil},
		{"POST", "/api/tunnels/" + e.tunID + "/stop", "", 400, "隧道未在运行", nil},
		{"GET", "/api/tunnels/" + e.tunID + "/console", "", 200, "", nil},
		{"POST", ip + "/start", "", 400, "Java 运行时缺失", nil},
		{"POST", ip + "/stop", "", 400, "服务器未在运行", nil},
		{"POST", ip + "/kill", "", 400, "服务器未在运行", nil},
		{"POST", ip + "/opendir?sub=mods", "", 403, "打开目录仅支持在本机操作（远程管理时请在主机上操作）", []reqOpt{fromLAN}},
		{"POST", ip + "/command", `{"cmd":"say hi"}`, 400, "服务器未在运行", nil},
		{"GET", ip + "/console", "", 200, "", nil},
		{"GET", ip + "/properties", "", 200, "", nil},
		{"GET", "/api/instances/nope/properties", "", 404, "实例不存在", nil},
		{"PUT", ip + "/properties", `{"pairs":[{"key":"max-players","value":"10"}]}`, 200, "", nil},
		{"PUT", ip + "/settings", `{"xmxMb":3072,"xmsMb":1024,"consoleEncoding":"utf-8"}`, 200, "", nil},
		{"PUT", ip + "/settings", `{"consoleEncoding":"big5"}`, 400, "不支持的控制台编码", nil},
		{"DELETE", "/api/instances/%E5%88%A0%E9%99%A4%E6%B5%8B%E8%AF%95", "", 200, "", nil}, // 删除测试
		{"GET", "/api/ops/log", "", 200, "", nil},
		// AI 设置（第二阶段新增）：修改类的仅本机；没配置时测试连接、刷新价格报「未启用」
		{"GET", "/api/ai/settings", "", 200, "", nil},
		{"POST", "/api/ai/test", "", 400, "AI 未启用", nil},
		{"POST", "/api/ai/prices/refresh", "", 400, "AI 未启用", nil},
		{"POST", "/api/ai/models", `{}`, 400, "先选择服务商", nil},
		{"PUT", "/api/ai/quota", `{"assistant":200000}`, 200, "", nil},
		{"PUT", "/api/ai/prices", `{"prices":[{"model":"deepseek-v4-flash","currency":"CNY","input":2,"output":8}]}`, 200, "", nil},
		{"PUT", "/api/ai/provider", `{"preset":"deepseek"}`, 403, "AI 设置仅支持在本机修改", []reqOpt{fromLAN}},
		{"PUT", "/api/ai/provider", `{"preset":"ollama","model":"qwen3:8b"}`, 200, "", nil},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		rec := e.call(t, c.method, c.path, c.body, c.code, c.opts...)
		seen[c.method+" "+pattern(c.path)] = true
		if strings.HasSuffix(c.path, "/console") {
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
				t.Errorf("%s 应为 SSE：%q", c.path, rec.Header().Get("Content-Type"))
			}
			continue
		}
		var resp struct {
			OK    bool            `json:"ok"`
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Errorf("%s %s：响应不是 JSON：%s", c.method, c.path, rec.Body)
			continue
		}
		if resp.OK != (c.code == 200) || (c.code == 200 && len(resp.Data) == 0) {
			t.Errorf("%s %s：响应结构不对：%s", c.method, c.path, rec.Body)
		}
		if c.errHas != "" && !strings.Contains(resp.Error, c.errHas) {
			t.Errorf("%s %s：错误「%s」里应有「%s」", c.method, c.path, resp.Error, c.errHas)
		}
	}
	// 壁纸图片（免令牌）
	req := httptest.NewRequest("GET", "/bg/x.png", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("GET /bg/x.png：HTTP %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	seen["GET "+pattern("/bg/x.png")] = true
	if n := len(seen); n != 47+1+7 { // 47 个旧接口 + 第一阶段的 /api/ops/log + 第二阶段的 7 个 /api/ai/*
		t.Errorf("覆盖了 %d 个接口，应为 55", n)
	}
	if d := e.dials.list(); len(d) != 0 {
		t.Errorf("只配了 Ollama，不该有任何对外连接：%v", d)
	}

	// 改动确实生效了（只看几处）
	var props struct {
		Data struct {
			Pairs []inst.KV `json:"pairs"`
		} `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", ip+"/properties", "", 200).Body.Bytes(), &props)
	if !strings.Contains(strings.ReplaceAll(func() string { b, _ := json.Marshal(props.Data.Pairs); return string(b) }(), " ", ""), `{"key":"max-players","value":"10"}`) {
		t.Errorf("server.properties 没改上：%+v", props.Data.Pairs)
	}
	if app.GetConfig().BackupKeep != 1000 {
		t.Errorf("backupKeep 超出范围应按 1000 算：%d", app.GetConfig().BackupKeep)
	}
	if b, _ := os.ReadFile(filepath.Join(app.ServersDir, "s1", "whitelist.json")); !strings.Contains(string(b), `"Steve"`) {
		t.Errorf("白名单没写上：%s", b)
	}
}

// 各接口的响应里搜不到 Key：CurseForge Key、AI 的 Key、访问密码（明文和哈希）、穿透凭据；
// config.json、tunnels.json、AI 的设置文件、secrets.json 和操作记录里也搜不到明文（操作记录里 Webhook 的令牌也要写成 ***）。
func TestResponsesNeverLeakSecrets(t *testing.T) {
	e := newFullEnv(t)
	e.secrets = append(e.secrets, aiKey)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D"
	var bodies []string
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/ai/provider", `{"preset":"deepseek","key":"` + aiKey + `"}`}, {"GET", "/api/ai/settings", ""},
		{"PUT", "/api/ai/provider", `{"model":"deepseek-v4-pro"}`}, {"PUT", "/api/ai/quota", `{"gamechat":1000}`},
		{"GET", "/api/app", ""}, {"PUT", "/api/config", `{"cfApiKey":"` + cfKey + `","lanPassword":"` + lanPass + `"}`},
		{"GET", "/api/instances", ""}, {"GET", ip + "/properties", ""}, {"GET", ip + "/players", ""},
		{"GET", ip + "/policies", ""}, {"GET", ip + "/backups", ""}, {"GET", "/api/javas", ""},
		{"GET", "/api/cores", ""}, {"GET", "/api/bg", ""}, {"GET", "/api/tunnels", ""},
		{"POST", "/api/tunnels", `{"provider":"custom","name":"自建","credential":"` + frpsToken + `","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}`},
		{"PUT", "/api/tunnels/" + e.tunID, `{"provider":"natfrp","name":"樱花","proxyId":"9","credential":"` + sakuraKey + `","localPort":25565}`},
		{"GET", "/api/tunnels", ""}, {"GET", "/api/tunnels/" + e.tunID + "/console", ""}, {"GET", "/api/ops/log", ""},
	} {
		bodies = append(bodies, c.method+" "+c.path+" → "+e.call(t, c.method, c.path, c.body, 200).Body.String())
	}
	for _, b := range bodies {
		for _, s := range e.secrets {
			if strings.Contains(b, s) {
				t.Errorf("响应里有密钥 %q：%s", s[:6], b)
			}
		}
	}
	files := []string{filepath.Join(app.Base, "config.json"), filepath.Join(app.Base, "tunnels.json"),
		filepath.Join(app.Base, "data", "ai", "settings.json"), filepath.Join(app.Base, "secrets.json")}
	logs, _ := filepath.Glob(filepath.Join(app.Base, "logs", "ops", "*.jsonl"))
	if len(logs) == 0 {
		t.Fatal("没有写操作记录")
	}
	for _, f := range append(files, logs...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range append(e.secrets, hookToken) {
			if strings.Contains(string(b), s) && !(strings.HasSuffix(f, "config.json") && s == hookToken) {
				t.Errorf("%s 里有密钥 %q", filepath.Base(f), s[:6])
			}
		}
	}
}

// 操作记录接口：记下谁做了什么（本机界面 / 本机程序 / 局域网设备），按实例、来源过滤，新的在前；局域网会话也能看。
func TestOpLogAPI(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D"
	e.call(t, "PUT", ip+"/properties", `{"pairs":[{"key":"motd","value":"from ui"}]}`, 200, fromUI)
	e.call(t, "PUT", ip+"/policies", `{"crashRestart":true}`, 200)
	e.call(t, "POST", ip+"/players", `{"action":"whitelist-add","player":"Alex"}`, 200, fromLAN)
	e.call(t, "GET", ip+"/properties", "", 200) // 读操作不记
	e.call(t, "POST", ip+"/stop", "", 400)      // 失败的也记
	type rec struct {
		Op    string          `json:"op"`
		Src   ops.Source      `json:"src"`
		Actor string          `json:"actor"`
		Inst  string          `json:"inst"`
		Args  json.RawMessage `json:"args"`
		OK    bool            `json:"ok"`
		Err   string          `json:"err"`
	}
	query := func(q string, opts ...reqOpt) []rec {
		t.Helper()
		var resp struct {
			Data []rec `json:"data"`
		}
		if err := json.Unmarshal(e.call(t, "GET", "/api/ops/log"+q, "", 200, opts...).Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Data
	}
	all := query("")
	var got []string
	for _, r := range all {
		got = append(got, r.Op+"|"+string(r.Src)+"|"+r.Actor)
	}
	want := []string{
		"instance.stop|api|本机",
		"instance.whitelist.add|lan|192.168.1.23",
		"instance.policies.set|api|本机",
		"instance.props.set|ui|本机",
		"app.config.set|api|本机", // newFullEnv 里设访问密码那一下
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("操作记录：\n%v\n应为：\n%v", got, want)
	}
	if r := all[0]; r.OK || !strings.Contains(r.Err, "服务器未在运行") || r.Inst != "生存服" {
		t.Errorf("失败的停服记录：%+v", r)
	}
	if r := all[1]; string(r.Args) != `{"player":"Alex"}` || r.Inst != "生存服" {
		t.Errorf("白名单记录：%+v", r)
	}
	if r := all[4]; !strings.Contains(string(r.Args), `"lanPassword":"***"`) || !strings.Contains(string(r.Args), `"webhookUrl":"***"`) {
		t.Errorf("设置的记录应脱敏：%s", r.Args)
	}
	if l := query("?src=lan"); len(l) != 1 || l[0].Op != "instance.whitelist.add" {
		t.Errorf("按来源过滤：%+v", l)
	}
	if l := query("?inst=%E7%94%9F%E5%AD%98%E6%9C%8D&limit=2"); len(l) != 2 || l[0].Op != "instance.stop" {
		t.Errorf("按实例过滤并限两条：%+v", l)
	}
	// 局域网会话（用密码登录拿到的 Cookie，不带令牌）也能看
	login := e.call(t, "POST", "/api/login", `{"password":"`+lanPass+`"}`, 200, fromLAN, noToken)
	cookie := login.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("登录没有下发 Cookie")
	}
	if l := query("", fromLAN, noToken, func(r *http.Request) { r.AddCookie(cookie[0]) }); len(l) != len(all) {
		t.Errorf("局域网会话看到 %d 条，应为 %d", len(l), len(all))
	}
}
