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
	"time"

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
		return dl.GuardedClientWithTransport(fakeTransport{content: []byte(body)})
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

// TestResolveTrustOnlyAfterVerified 点了确认但文件没下成（内容对不上校验值）：不能把站点记成信任。
// 记下以后同站点的文件不再问、以后导入别的整合包也直接下，所以要等下载下来、校验也过了才记。
func TestResolveTrustOnlyAfterVerified(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{
		{Path: "mods/a.jar", SHA1: sha1Of(t, []byte("整合包说的内容")), URLs: []string{"https://93.184.216.34/a.jar"}, Reason: modpack.ReasonHost},
	}, nil)
	oldHTTP := modpackHTTP
	modpackHTTP = func() *http.Client {
		return dl.GuardedClientWithTransport(fakeTransport{content: []byte("站点给的是别的东西")})
	}
	t.Cleanup(func() { modpackHTTP = oldHTTP })

	out, remain, err := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "trust", URL: "https://93.184.216.34/a.jar", Ack: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].OK || len(remain) != 1 {
		t.Fatalf("校验不符应失败并留在清单: %+v %+v", out, remain)
	}
	if hosts := app.GetConfig().TrustedHosts; len(hosts) != 0 {
		t.Fatalf("文件没下成就不该记信任: %+v", hosts)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "a.jar")); err == nil {
		t.Fatal("校验不符的文件不该留在实例里")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "a.jar.part")); err == nil {
		t.Fatal("不该留下 .part 半成品")
	}
}

// TestResolvePendingInstanceReplaced 待决清单认的是导入出来的那一台：删掉以后再建一台同名的，
// 回到旧任务处置时要拒绝，不能把文件放进新的那台。
func TestResolvePendingInstanceReplaced(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	m.insts["测试服"].CreatedAt = time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonNoHash}}, nil)

	local := t.TempDir()
	writeTree(t, local, map[string]string{"a.jar": "旧任务要放的文件"})
	// 删掉，再建一台同名的（目录是新的，创建时间也是新的）
	newDir := t.TempDir()
	m.insts["测试服"] = &Instance{Settings: Settings{Name: "测试服", JavaMajor: 17, CreatedAt: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}, Dir: newDir}

	_, _, err := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "local", Path: filepath.Join(local, "a.jar")},
	})
	if err == nil || !strings.Contains(err.Error(), "后来新建的") {
		t.Fatalf("同名的新实例应被认出来并拒绝: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "mods", "a.jar")); err == nil {
		t.Fatal("文件不该落进后来新建的同名实例")
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

// 没法自动下载的 CurseForge 文件：查到了文件名的，待决清单里写真实的落点和项目名、原因；
// 接口给的文件名不规矩（是 MineDeck 自己的文件、是 Windows 的保留名）时退回只写编号，不拿它当落点。
func TestCFPendingCarriesNamesAndPlacement(t *testing.T) {
	pf, wi := cfPendingOf(modpack.CFRef{ProjectID: 5, FileID: 105, FileName: "hapi-1.1.3.jar", Name: "Hybrid API",
		Dir: "mods", Why: modpack.CFWhyNoDist, URL: "https://www.curseforge.com/minecraft/mc-mods/hapi/files/105"})
	if pf.Path != "mods/hapi-1.1.3.jar" || pf.Reason != "cf" || !strings.Contains(pf.Note, "「Hybrid API」") || !strings.Contains(pf.Note, "作者不允许") {
		t.Errorf("作者不让下载的：%+v", pf)
	}
	if wi.Name != "mods/hapi-1.1.3.jar（Hybrid API）" || wi.URL != "https://www.curseforge.com/minecraft/mc-mods/hapi/files/105" {
		t.Errorf("告警里的一行：%+v", wi)
	}
	// 资源包进 resourcepacks
	if pf, _ := cfPendingOf(modpack.CFRef{ProjectID: 2, FileID: 102, FileName: "Faithful.zip", Dir: "resourcepacks", Why: modpack.CFWhyNoDist}); pf.Path != "resourcepacks/Faithful.zip" {
		t.Errorf("资源包的落点：%s", pf.Path)
	}
	// 没查到文件名（没填 Key）：只有编号，链接退回按项目编号拼的那个
	pf, wi = cfPendingOf(modpack.CFRef{ProjectID: 7, FileID: 107, Why: modpack.CFWhyNoKey})
	if pf.Path != "mods/（CurseForge 项目 7，文件 107）" || !strings.Contains(pf.Note, "API Key") || !strings.Contains(wi.URL, "/projects/7") {
		t.Errorf("没查到文件名的：%+v %+v", pf, wi)
	}
	// 不规矩的文件名、认不得的文件夹：不拿来当落点
	for _, r := range []modpack.CFRef{
		{ProjectID: 8, FileID: 108, FileName: "NUL", Dir: "mods"},                     // Windows 的保留名
		{ProjectID: 8, FileID: 108, FileName: "x.jar", Dir: "config/minedeck"},        // 文件夹不是那四个之一
		{ProjectID: 8, FileID: 108, FileName: "x.jar", Dir: ".."},                     // 想往外跑
		{ProjectID: 8, FileID: 108, FileName: "sub/../../instance.json", Dir: "mods"}, // 接口那边按理已经挡掉，这里再挡一次
	} {
		pf, _ := cfPendingOf(r)
		if !strings.HasPrefix(pf.Path, "mods/") || strings.Contains(pf.Path, "..") || strings.Contains(pf.Path, "minedeck") ||
			pf.Path == "mods/NUL" || !overrideAllowed(pf.Path) {
			t.Errorf("%+v 的落点不该是 %q", r, pf.Path)
		}
	}

	// 落点的检查要按化简以后的样子来认：绕一圈回到 MineDeck 自己的文件、整段只有点和空格的，都不行
	for _, bad := range []string{"mods/../instance.json", "mods/sub/../../instance.json", "config/x/../minedeck/companion.properties",
		"mods/../../outside.jar", "..", "mods/.../x.jar", "mods/ . /x.jar", `mods\..\instance.json`} {
		if overrideAllowed(bad) {
			t.Errorf("%q 不该算能写的落点", bad)
		}
	}
	for _, ok := range []string{"mods/a.jar", "mods/sub/../b.jar", "resourcepacks/Faithful 32x.zip", "config/x.toml"} {
		if !overrideAllowed(ok) {
			t.Errorf("%q 是正常的落点", ok)
		}
	}

	if got := cfWhySummary([]modpack.CFRef{{Why: modpack.CFWhyNoKey}, {Why: modpack.CFWhyNoKey}}); got != "没有填 API Key" {
		t.Errorf("全是一个原因时只说原因：%q", got)
	}
	if got := cfWhySummary([]modpack.CFRef{{Why: modpack.CFWhyNoDist}, {Why: modpack.CFWhyNoDist}, {Why: modpack.CFWhyGone}}); got != "作者不允许第三方下载的 2 个，CurseForge 上找不到的 1 个" {
		t.Errorf("几个原因时各说几个：%q", got)
	}
	if got := cfPlacement([]modpack.File{{Path: "mods/a.jar"}, {Path: "mods/b.jar"}}); got != "" {
		t.Errorf("全是模组时不用说：%q", got)
	}
	got := cfPlacement([]modpack.File{{Path: "mods/a.jar"}, {Path: "resourcepacks/a.zip"}, {Path: "resourcepacks/b.zip"}, {Path: "datapacks/c.zip"}})
	if !strings.Contains(got, "资源包 2 个进 resourcepacks") || !strings.Contains(got, "数据包 1 个进 datapacks") || strings.Contains(got, "光影") || !strings.Contains(got, "不会自己生效") {
		t.Errorf("分开放的说明：%q", got)
	}
}

// 待决清单里 CurseForge 的项：说明用它自己的那句；用本地文件代替时放进这一项该在的文件夹，文件叫它本来的名字。
func TestCFPendingNoteAndLocalPlacement(t *testing.T) {
	m, dir := importEnv(t)
	tk := newImportTask(m, "测试服", dir)
	p1, _ := cfPendingOf(modpack.CFRef{ProjectID: 2, FileID: 102, FileName: "Faithful 32x.zip", Name: "Faithful", Dir: "resourcepacks", Why: modpack.CFWhyNoDist})
	p2, _ := cfPendingOf(modpack.CFRef{ProjectID: 5, FileID: 105, Why: modpack.CFWhyNoKey})
	m.finishImport(tk, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{p1, p2}, nil)
	sn := tk.Snapshot()
	if len(sn.Pending) != 2 || !strings.Contains(sn.Pending[0].Note, "「Faithful」") || !strings.Contains(sn.Pending[1].Note, "API Key") {
		t.Fatalf("待决清单里的说明：%+v", sn.Pending)
	}
	local := t.TempDir()
	writeTree(t, local, map[string]string{"我下载的资源包.zip": "资源包", "我下载的模组.jar": "模组"})
	out, remain, err := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{
		{ID: "p1", Act: "local", Path: filepath.Join(local, "我下载的资源包.zip")},
		{ID: "p2", Act: "local", Path: filepath.Join(local, "我下载的模组.jar")},
	})
	if err != nil || len(out) != 2 || !out[0].OK || !out[1].OK || len(remain) != 0 {
		t.Fatalf("两项都该处理成功：%v %+v %+v", err, out, remain)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "resourcepacks", "我下载的资源包.zip")); err != nil || string(b) != "资源包" {
		t.Errorf("资源包该进 resourcepacks：%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "mods", "我下载的模组.jar")); err != nil || string(b) != "模组" {
		t.Errorf("没查到文件名的按模组放进 mods：%v", err)
	}
}
