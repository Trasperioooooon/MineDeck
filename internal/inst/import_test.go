package inst

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// importEnv 和 adoptEnv 同款：程序目录换成临时的一份（测试不联网），返回空的 Manager。
func importEnv(t *testing.T) (*Manager, string) {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.RuntimesDir = old[0], old[1], old[2], old[3]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	// 配置复位（TrustedHosts 清空），避免用例之间互相污染
	setConfig(t, func(c *app.Config) {
		c.TrustedHosts = nil
		c.ServersDir, c.BackupsDir, c.Roots, c.BackupRoots = "", "", nil, nil
	})
	return &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}, t.TempDir()
}

// newImportTask 建一个导入收尾用的任务和实例（不走 runCreate：那里要联网下载核心）。
func newImportTask(m *Manager, instName, dir string) *tasks.Task {
	m.insts[instName] = &Instance{Settings: Settings{Name: instName, JavaMajor: 17}, Dir: dir}
	t, _ := m.Tasks.New("导入整合包 "+instName, nil)
	return t
}

// TestFinishImportPending 验证收尾记账：待决清单（编号、原因、处置选项、SHA-1）、保留清单告警与记录文件。
func TestFinishImportPending(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	pending := []modpack.PendingFile{
		{Path: "mods/custom-host.jar", SHA1: "aa", URLs: []string{"https://93.184.216.34/a.jar"}, Size: 10, Reason: modpack.ReasonHost},
		{Path: "mods/no-hash.jar", Size: 20, Reason: modpack.ReasonNoHash},
		{Path: "mods/plain-http.jar", Size: 30, Reason: modpack.ReasonBadURL},
	}
	kept := []keptFile{{path: "mods/inherited.jar", source: "本地来源目录"}}
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, pending, kept)

	sn := tk.Snapshot()
	if len(sn.Pending) != 3 {
		t.Fatalf("待决清单应为 3 项: %+v", sn.Pending)
	}
	p1 := sn.Pending[0]
	if p1.ID != "p1" || p1.Reason != "host" || p1.Host != "93.184.216.34" || len(p1.URLs) != 1 || p1.SHA1 != "aa" {
		t.Errorf("host 待决项字段不对: %+v", p1)
	}
	if got := strings.Join(p1.Options, ","); got != "local,skip,trust" {
		t.Errorf("host 待决项应可信任下载: %s", got)
	}
	if p2 := sn.Pending[1]; p2.Reason != "nohash" || strings.Join(p2.Options, ",") != "skip,local" || len(p2.URLs) != 0 {
		t.Errorf("nohash 待决项不对: %+v", p2)
	}
	kinds := map[string]bool{}
	for _, w := range sn.Warnings {
		kinds[w.Kind] = true
	}
	for _, want := range []string{"import-kept", "import-pending"} {
		if !kinds[want] {
			t.Errorf("缺少 %s 告警: %+v", want, sn.Warnings)
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "导入待决清单.txt"))
	if err != nil || !strings.Contains(string(body), "custom-host.jar") {
		t.Errorf("应写入待决记录文件: %v", err)
	}
	if i, err := m.pendingInstance(tk.ID()); err != nil || i.Name != "测试服" {
		t.Fatalf("待决任务应能找回实例: %v %v", i, err)
	}
}

// TestFinishImportJavaWarn 魔改模组声明 Java 25、实例是 17：给提示但不阻断（1.20.1 + Java 25 允许存在）。
func TestFinishImportJavaWarn(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	// fabric.mod.json 声明 "java": ">=25" 的魔改 jar；再放一个不超过要求的普通 jar
	writeTree(t, dir, map[string]string{
		"mods/modded-fabric.jar": `zip:fabric.mod.json={"schemaVersion":1,"id":"modded","version":"1.0","depends":{"java":">=25"}}`,
		"mods/ok.jar":            `zip:fabric.mod.json={"schemaVersion":1,"id":"ok","version":"1.0","depends":{"java":">=17"}}`,
	})
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, nil, nil)
	sn := tk.Snapshot()
	var found *tasks.Warning
	for k := range sn.Warnings {
		if sn.Warnings[k].Kind == "java-req" {
			found = &sn.Warnings[k]
		}
	}
	if found == nil {
		t.Fatalf("应提示 Java 要求: %+v", sn.Warnings)
	}
	if !strings.Contains(found.Title, "Java 25") || !strings.Contains(found.Title, "Java 17") {
		t.Errorf("标题应写明两边版本: %s", found.Title)
	}
	if len(found.Items) != 1 || !strings.Contains(found.Items[0].Name, "modded-fabric.jar") {
		t.Errorf("应只点出要 Java 25 的那个模组: %+v", found.Items)
	}
}

// TestFindLocalFile 本地来源目录的匹配规则：相对路径一致 > mods/plugins 下的同名 > 根目录同名。
func TestFindLocalFile(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"mods/same-name.jar":      "内容A",
		"mods/exact/relative.jar": "内容B",
		"plugins/plugin-name.jar": "内容C",
		"root-name.jar":           "内容D",
	})
	cases := []struct {
		rel  string
		want string
	}{
		{"mods/exact/relative.jar", "mods/exact/relative.jar"},
		{"mods/same-name.jar", "mods/same-name.jar"},
		{"other-dir/plugin-name.jar", "plugins/plugin-name.jar"},
		{"anywhere/root-name.jar", "root-name.jar"},
		{"mods/missing.jar", ""},
	}
	for _, c := range cases {
		got := findLocalFile(src, c.rel)
		if c.want == "" {
			if got != "" {
				t.Errorf("%s 不该找到，找到 %s", c.rel, got)
			}
			continue
		}
		if !strings.HasSuffix(filepath.ToSlash(got), c.want) {
			t.Errorf("%s 应匹配到 %s，实际 %s", c.rel, c.want, got)
		}
	}
	if findLocalFile("不存在的目录", "mods/x.jar") != "" {
		t.Error("来源目录不存在时应返回空")
	}
}

// TestResolveImportPending 逐项处置：跳过、本地目录匹配、失败留清单；一批里成败混着各自结算。
func TestResolveImportPending(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	pending := []modpack.PendingFile{
		{Path: "mods/a-custom-host.jar", SHA1: "aa", URLs: []string{"https://93.184.216.34/a.jar"}, Reason: modpack.ReasonHost},
		{Path: "mods/b-nohash.jar", Reason: modpack.ReasonNoHash},
		{Path: "mods/c-keep.jar", Reason: modpack.ReasonNoHash},
	}
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, pending, nil)

	local := t.TempDir()
	writeTree(t, local, map[string]string{"mods/c-keep.jar": "我的魔改模组"})
	out, remain, err := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "trust", URL: "https://93.184.216.34/a.jar"}, // 没确认信任：留在清单
		{ID: "p2", Act: "skip"},
		{ID: "p3", Act: "local", Path: local},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("应有 3 个结果: %+v", out)
	}
	if out[0].OK {
		t.Errorf("未确认信任不该下载: %+v", out[0])
	}
	if !out[1].OK || !out[2].OK {
		t.Errorf("跳过和本地保留应成功: %+v", out)
	}
	if len(remain) != 1 || remain[0].Path != "mods/a-custom-host.jar" {
		t.Fatalf("没确认信任的应留在待决清单: %+v", remain)
	}
	b, err := os.ReadFile(filepath.Join(dir, "mods", "c-keep.jar"))
	if err != nil || string(b) != "我的魔改模组" {
		t.Fatalf("本地文件应复制进实例: %v %q", err, string(b))
	}
	if len(tk.Snapshot().Warnings) == 0 { // 只是确保没把任务状态改坏
		t.Fatal("告警不该被处置动作清掉")
	}

	// 手动指定替代文件也行；不存在的编号单项失败不影响整批
	out, _, _ = m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "?p", Act: "skip"},
		{ID: "p1", Act: "local", Path: filepath.Join(local, "mods", "c-keep.jar")},
	})
	if len(out) != 2 || !out[1].OK {
		t.Fatalf("处置结果不对: %+v", out)
	}
	if left := tk.PendingItems(); len(left) != 0 {
		t.Fatalf("全部处理完应清空待决清单: %+v", left)
	}
}

// TestResolveTrustDownload 确认信任后真的下载并过 SHA-1 校验（用注入的假 HTTP 客户端，不联网；
// 站点是公网 IP 字面量，不过 DNS）。同一站点再遇到（另一个文件）不再要求确认：记忆生效。
func TestResolveTrustDownload(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	body := "魔改模组的真实内容"
	// SHA-1 在测试里现算，走 dl 的真实校验逻辑
	sum := sha1Of(t, []byte(body))
	pending := []modpack.PendingFile{
		{Path: "mods/a-custom-host.jar", SHA1: sum, URLs: []string{"https://93.184.216.34/a.jar"}, Reason: modpack.ReasonHost},
		{Path: "mods/b-custom-host.jar", SHA1: sum, URLs: []string{"https://93.184.216.34/b.jar"}, Reason: modpack.ReasonHost},
	}
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, pending, nil)
	// 下载不真联网：导入的受控客户端换成带假 RoundTripper 的版本（拨号级检查不会发生，
	// 重定向与 SHA-1 校验照常走）
	oldHTTP := modpackHTTP
	modpackHTTP = func() *http.Client {
		return dl.GuardedClientWithTransport(func(string) bool { return true }, fakeTransport{content: []byte(body)})
	}
	t.Cleanup(func() { modpackHTTP = oldHTTP })

	// 没确认（ack=false）→ 留清单
	out, _, _ := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "trust", URL: "https://93.184.216.34/a.jar"},
	})
	if out[0].OK {
		t.Fatalf("没有确认不该下载: %+v", out)
	}
	// 确认 → 下载并校验通过；信任记进全局配置（作用域 modpack）
	out, _, _ = m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "trust", URL: "https://93.184.216.34/a.jar", Ack: true},
	})
	if !out[0].OK {
		t.Fatalf("确认信任后应下载成功: %+v", out)
	}
	if !app.TrustedHostFunc(app.TrustScopeModpack)("93.184.216.34") {
		t.Fatal("信任应记进配置")
	}
	b, err := os.ReadFile(filepath.Join(dir, "mods", "a-custom-host.jar"))
	if err != nil || string(b) != body {
		t.Fatalf("下载的文件内容不对: %v %q", err, string(b))
	}
	// 站点已信任：同一站点第二个文件不再要求 ack
	out, _, _ = m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p2", Act: "trust", URL: "https://93.184.216.34/b.jar"},
	})
	if !out[0].OK {
		t.Fatalf("已确认过的站点不该再要求确认: %+v", out)
	}
	if len(tk.PendingItems()) != 0 {
		t.Fatalf("待决清单应清空: %+v", tk.PendingItems())
	}
	if _, err := m.pendingInstance(tk.ID()); err == nil {
		t.Fatal("清单清空后应解除任务与实例的关联")
	}
}

// TestResolveTrustSecurity 信任下载的安全门全关着的时候逐个都不放行（SSRF 防线不动）：
// 候选之外的地址、非 https、内网 IP、没确认。任何一步都不下载、不记信任。
func TestResolveTrustSecurity(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	pending := []modpack.PendingFile{
		{Path: "mods/a.jar", SHA1: "aa", URLs: []string{"https://93.184.216.34/a.jar"}, Reason: modpack.ReasonHost},
		{Path: "mods/no-trust-option.jar", Reason: modpack.ReasonBadURL},
	}
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, pending, nil)
	acts := []PendingAction{
		{ID: "p1", Act: "trust", URL: "https://evil.example.org/a.jar", Ack: true}, // 不在候选里
		{ID: "p1", Act: "trust", URL: "http://93.184.216.34/a.jar", Ack: true},     // 非 https
		{ID: "p1", Act: "trust", URL: "https://127.0.0.1/a.jar", Ack: true},        // 不是候选且是内网
		{ID: "p1", Act: "trust", URL: "https://93.184.216.34/a.jar"},               // 没确认
		{ID: "p2", Act: "trust", URL: "https://93.184.216.34/a.jar", Ack: true},    // 这一项根本没有 trust 选项
		{ID: "p1", Act: "local", Path: ""},                                         // local 没给路径
	}
	out, _, err := m.ResolveImportPending(context.Background(), tk.ID(), acts)
	if err != nil {
		t.Fatal(err)
	}
	for k, oc := range out {
		if oc.OK {
			t.Errorf("第 %d 个动作 %s 不应成功: %+v", k, oc.Act, oc)
		}
	}
	if len(tk.PendingItems()) != 2 {
		t.Fatalf("失败都该留在清单: %+v", tk.PendingItems())
	}
	if len(app.GetConfig().TrustedHosts) != 0 {
		t.Fatalf("一道门都没过去就不该记信任: %+v", app.GetConfig().TrustedHosts)
	}
}

// ---------- 测试工具 ----------

func sha1Of(t *testing.T, b []byte) string {
	t.Helper()
	h := sha1.New()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// fakeTransport 不联网：任何请求都回固定的内容。
type fakeTransport struct{ content []byte }

func (f fakeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(f.content)),
		Header:     http.Header{"Content-Length": []string{strconv.Itoa(len(f.content))}},
	}, nil
}
