package modcart

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var fabric = []string{"fabric"}

func titles(fs []File) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Title)
	}
	return out
}

func depTitles(ds []DepInfo) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.Project.Title+"←"+d.DependencyOf)
	}
	return out
}

// 加入时向 Modrinth 核对：ID 和 slug 都认、同一个项目只放一次，查不到的、只在客户端用的不放进去并说明原因；
// 展示用的名称、图标、许可证用 Modrinth 的；存进 cart.json，重开还在。
func TestAddChecksWithModrinth(t *testing.T) {
	mr := newFakeMR(t)
	dir := t.TempDir()
	s := New(dir, mr.srv.URL)
	items, skipped, err := s.Add(context.Background(), Draft, []string{"alpha", "aaa", " client ", "no-such"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProjectID != "aaa" || items[0].Title != "Alpha" || items[0].License != "MIT" || items[0].Kind != "mod" ||
		!strings.Contains(items[0].Icon, "aaa") || items[0].AddedAt.IsZero() {
		t.Fatalf("车里：%+v", items)
	}
	if len(skipped) != 2 || skipped[0].ProjectID != "client" || !strings.Contains(skipped[0].Reason, "客户端") ||
		skipped[1].ProjectID != "no-such" || !strings.Contains(skipped[1].Reason, "找不到") {
		t.Fatalf("没放进去的：%+v", skipped)
	}
	// 再加一次同一个：不重复；加到实例的车里互不影响
	if items, _, _ = s.Add(context.Background(), Draft, []string{"aaa", "bbb"}); len(items) != 2 {
		t.Fatalf("再加以后：%+v", items)
	}
	if items, _, _ = s.Add(context.Background(), "生存服", []string{"fabric-api"}); len(items) != 1 || items[0].ProjectID != "fapi" {
		t.Fatalf("实例的车：%+v", items)
	}
	// 重开读回
	s2 := New(dir, mr.srv.URL)
	if got := s2.List(Draft); len(got) != 2 || got[0].ProjectID != "aaa" || got[1].ProjectID != "bbb" {
		t.Fatalf("重开后的草稿车：%+v", got)
	}
	if got := s2.List("生存服"); len(got) != 1 {
		t.Fatalf("重开后的实例车：%+v", got)
	}
	// 全都放不进去：报错，车不变
	if _, _, err := s.Add(context.Background(), Draft, []string{"no-such"}); err == nil || !IsNotFound(err) {
		t.Fatalf("全都查不到应报 404：%v", err)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"client"}); err == nil || IsNotFound(err) || !strings.Contains(err.Error(), "客户端") {
		t.Fatalf("只有客户端模组应报 400 并说明：%v", err)
	}
	if got := s.List(Draft); len(got) != 2 {
		t.Fatalf("报错后车变了：%+v", got)
	}
}

// 每辆车最多 50 项。
func TestCartLimit(t *testing.T) {
	mr := newFakeMR(t)
	for i := range MaxItems {
		id := fmt.Sprintf("p%02d", i)
		p := mrProject{ID: id, Slug: id, Title: id, ProjectType: "mod", ServerSide: "required"}
		mr.projects[id] = p
	}
	s := New(t.TempDir(), mr.srv.URL)
	var ids []string
	for i := range MaxItems {
		ids = append(ids, fmt.Sprintf("p%02d", i))
	}
	if items, _, err := s.Add(context.Background(), Draft, ids); err != nil || len(items) != MaxItems {
		t.Fatalf("放满 %d 项：%d %v", MaxItems, len(items), err)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"aaa"}); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("第 51 项应报错：%v", err)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"p00"}); err != nil {
		t.Fatalf("车满时加一个已经在车里的不该报错：%v", err)
	}
}

// 拿掉、清空、指纹：指纹只看有哪些项目，不看顺序。
func TestRemoveClearFingerprint(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	a, _, _ := s.Add(context.Background(), Draft, []string{"aaa", "bbb", "fapi"})
	b := []Item{a[2], a[0], a[1]}
	if Fingerprint(a) != Fingerprint(b) || Fingerprint(a) == Fingerprint(a[:2]) {
		t.Fatal("指纹应只看项目集合")
	}
	if got, err := s.Remove(Draft, "beta"); err != nil || len(got) != 2 || slices.ContainsFunc(got, func(it Item) bool { return it.ProjectID == "bbb" }) {
		t.Fatalf("按 slug 拿掉：%+v %v", got, err)
	}
	if got, _ := s.RemoveMany(Draft, []string{"aaa", "nothing"}); len(got) != 1 || got[0].ProjectID != "fapi" {
		t.Fatalf("拿掉几项：%+v", got)
	}
	if err := s.Clear(Draft); err != nil || len(s.List(Draft)) != 0 {
		t.Fatal("清空失败")
	}
	if m := Merge(a[:2], a[1:]); len(m) != 3 {
		t.Fatalf("合并去重：%+v", m)
	}
}

// 前置闭包：必装的往下追 3 层（只写了版本 ID 的也认、绕回购物车的不重复）、内嵌的不装、可选的只列出、
// 不兼容的组合写进冲突、没有能用版本的写进找不到。
func TestClosure(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	items, _, err := s.Add(context.Background(), Draft, []string{"aaa", "bbb", "old"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.Closure(context.Background(), items, Local{}, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(titles(plan.Files), ","); got != "Alpha,Beta,Fabric API,Lib One,Lib Two,Lib Three" {
		t.Fatalf("要下载的：%s", got)
	}
	for _, f := range plan.Files {
		if f.FromDep != (f.Title != "Alpha" && f.Title != "Beta") || f.URL == "" || len(f.SHA1) != 40 || f.FileName == "" {
			t.Fatalf("文件信息不全或前置标记不对：%+v", f)
		}
	}
	if got := strings.Join(depTitles(plan.Missing), ","); got != "Fabric API←Alpha,Lib One←Beta,Lib Two←Lib One,Lib Three←Lib Two" {
		t.Fatalf("缺的前置：%s", got)
	}
	if plan.Missing[0].Project.URL != "" || plan.Missing[0].Project.SHA1 != "" || plan.Missing[0].Project.License != "Apache-2.0" {
		t.Fatalf("Missing 里应去掉下载地址和哈希、带上许可证：%+v", plan.Missing[0].Project)
	}
	if got := strings.Join(depTitles(plan.Optional), ","); got != "Opt One←Alpha" {
		t.Fatalf("可选：%s", got)
	}
	if len(plan.Unresolved) != 1 || plan.Unresolved[0].ProjectID != "old" || !strings.Contains(plan.Unresolved[0].Reason, "fabric 1.20.1") {
		t.Fatalf("找不到：%+v", plan.Unresolved)
	}
	if len(plan.Conflicts) != 0 || len(plan.LocalHits) != 0 {
		t.Fatalf("不该有冲突和已有：%+v %+v", plan.Conflicts, plan.LocalHits)
	}
	if mr.count("/version/v-lib1") == 0 {
		t.Fatal("只写了版本 ID 的依赖应先查版本")
	}

	// 不兼容的放进了同一辆车
	bad, _, _ := s.Add(context.Background(), "x", []string{"aaa", "bad"})
	plan, err = s.Closure(context.Background(), bad, Local{}, fabric, "1.20.1")
	if err != nil || len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], "Alpha 和 Bad Mod 不兼容") {
		t.Fatalf("冲突：%+v %v", plan, err)
	}
}

// 服务器上已经有的不再下载：按 SHA-1 认出来的旧版本、同名文件、modinfo 读出的模组 ID 都算。
func TestClosureLocal(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	items, _, _ := s.Add(context.Background(), Draft, []string{"aaa", "bbb"})
	local := Local{
		Projects: map[string]string{"fapi": "fabric-api-0.90.0.jar"}, // 旧版本：文件名不同，按哈希认出来
		Files:    []string{"bbb-2.0.0.jar"},                          // 购物车里的这一项已经装了同一个文件
		IDs:      []string{"lib-one"},                                // 魔改或手动放进来的：模组 ID 和 slug 一样
	}
	plan, err := s.Closure(context.Background(), items, local, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(titles(plan.Files), ","); got != "Alpha" {
		t.Fatalf("要下载的：%s", got)
	}
	if got := strings.Join(plan.LocalHits, ","); got != "bbb-2.0.0.jar,fabric-api-0.90.0.jar,Lib One" { // Beta 已经装着，也顺着它的依赖查：Lib One 按模组 ID 认出来已有
		t.Fatalf("已有的：%s", got)
	}
	if len(plan.Missing) != 0 {
		t.Fatalf("不该再缺：%+v", plan.Missing)
	}
}

// 挑版本：加载器、MC 版本对得上；正式版优先，同一种里取最新。
func TestPickVersion(t *testing.T) {
	mr := newFakeMR(t)
	if v, ok := pickVersion(mr.versions["rank"], fabric, "1.20.1"); !ok || v.ID != "vr-2" {
		t.Fatalf("应挑最新的正式版：%+v", v)
	}
	if _, ok := pickVersion(mr.versions["rank"], []string{"forge"}, "1.20.1"); ok {
		t.Fatal("加载器对不上不该挑中")
	}
	if _, ok := pickVersion(mr.versions["old"], fabric, "1.20.1"); ok {
		t.Fatal("MC 版本对不上不该挑中")
	}
	if v, ok := pickVersion(mr.versions["old"], fabric, ""); !ok || v.ID != "vold" {
		t.Fatal("不卡版本时（插件）应挑中")
	}
	beta := []mrVersion{mr.versions["rank"][0]}
	if v, ok := pickVersion(beta, fabric, "1.20.1"); !ok || v.VersionType != "beta" {
		t.Fatal("只有测试版时用测试版")
	}
}

// 搜索：只在客户端用的模组不列出来；facets 带上类型、加载器和版本。
func TestSearch(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	res, err := s.Search(context.Background(), SearchQuery{Q: "a", Type: "mod", Loaders: fabric, MC: "1.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	hits := res.Hits
	if len(hits) != 1 || hits[0].ProjectID != "aaa" || hits[0].License != "MIT" || hits[0].ServerSide != "" {
		t.Fatalf("搜索结果：%+v", hits)
	}
	// 让 Modrinth 那边就把只在客户端用的筛掉（每页条数才是齐的）；一页要 20 条
	if !strings.Contains(mr.lastFacets, `["server_side!=unsupported"]`) || !strings.Contains(mr.lastFacets, `["project_type:mod"]`) ||
		!strings.Contains(mr.lastFacets, `["categories:fabric"]`) || !strings.Contains(mr.lastFacets, `["versions:1.20.1"]`) ||
		mr.lastLimit != "20" || mr.lastOffset != "0" {
		t.Fatalf("发给 Modrinth 的：facets=%s offset=%s limit=%s", mr.lastFacets, mr.lastOffset, mr.lastLimit)
	}
	plugins, err := s.Search(context.Background(), SearchQuery{Q: "a", Type: "plugin", Loaders: []string{"paper"}})
	if err != nil || len(plugins.Hits) != 2 {
		t.Fatalf("插件不按客户端筛：%+v %v", plugins, err)
	}
	if strings.Contains(mr.lastFacets, "server_side") || strings.Contains(mr.lastFacets, "versions:") {
		t.Fatalf("插件不带 server_side、不卡版本：%s", mr.lastFacets)
	}
}

// 热门清单：关键词留空、按类别筛、一页 20 条往后翻，最多翻到第 5 页；类别只认白名单里的。
func TestSearchPagesAndCategories(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	ctx := context.Background()
	page := func(offset int) *SearchResult {
		t.Helper()
		res, err := s.Search(ctx, SearchQuery{Q: "bulk", Type: "mod", Loaders: fabric, MC: "1.20.1", Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	p1, p2, p3 := page(0), page(20), page(40)
	if len(p1.Hits) != 20 || !p1.More || p1.Total != 45 || p1.Hits[0].ProjectID != "bulk00" {
		t.Fatalf("第 1 页：%d 条 more=%v total=%d", len(p1.Hits), p1.More, p1.Total)
	}
	if len(p2.Hits) != 20 || !p2.More || p2.Offset != 20 || p2.Hits[0].ProjectID != "bulk20" {
		t.Fatalf("第 2 页：%d 条 more=%v 从 %s 开始", len(p2.Hits), p2.More, p2.Hits[0].ProjectID)
	}
	if len(p3.Hits) != 5 || p3.More {
		t.Fatalf("最后一页：%d 条 more=%v", len(p3.Hits), p3.More)
	}
	// 不是整页的 offset 按页对齐；超过上限的按上限取，到了第 5 页就不再往后
	if got := page(27); got.Offset != 20 || mr.lastOffset != "20" {
		t.Fatalf("offset 27 应对齐到 20：%d（发出去的是 %s）", got.Offset, mr.lastOffset)
	}
	if got := page(500); got.Offset != MaxOffset || got.More || mr.lastOffset != "80" {
		t.Fatalf("offset 最多到 %d：%d more=%v", MaxOffset, got.Offset, got.More)
	}
	if got := page(-5); got.Offset != 0 {
		t.Fatalf("负的 offset 按 0：%d", got.Offset)
	}
	// 关键词留空 + 类别：facets 里多一组 categories:<类别>
	res, err := s.Search(ctx, SearchQuery{Type: "mod", Loaders: fabric, MC: "1.20.1", Category: "optimization"})
	if err != nil || len(res.Hits) != 1 || res.Hits[0].ProjectID != "aaa" || !strings.Contains(mr.lastFacets, `["categories:optimization"]`) {
		t.Fatalf("按类别筛：%+v %v facets=%s", res, err, mr.lastFacets)
	}
	// 白名单外的类别不发给 Modrinth
	before := mr.count("/search")
	for _, q := range []SearchQuery{
		{Type: "mod", Loaders: fabric, MC: "1.20.1", Category: `x"],["project_type:modpack`},
		{Type: "mod", Loaders: fabric, MC: "1.20.1", Category: "economy"}, // 插件才有的类别
		{Type: "plugin", Loaders: []string{"paper"}, Category: "magic"},   // 模组才有的类别
	} {
		if _, err := s.Search(ctx, q); err == nil || err.(*Error).Code != 400 {
			t.Errorf("类别 %q（%s）应报 400：%v", q.Category, q.Type, err)
		}
	}
	if mr.count("/search") != before {
		t.Error("类别不对时不该去问 Modrinth")
	}
	if _, err := s.Search(ctx, SearchQuery{Type: "plugin", Loaders: []string{"paper"}, Category: "economy"}); err != nil {
		t.Errorf("插件的类别：%v", err)
	}
	for _, c := range append(Categories("mod"), Categories("plugin")...) {
		if !validID(c) {
			t.Errorf("类别 %q 不该出现在白名单里", c)
		}
	}
}

// 下载：核对 SHA-1 后才改成正式文件名，同名文件已经在了就跳过；文件名、哈希、（真的 Modrinth 时）地址不对的不下。
func TestDownload(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	items, _, _ := s.Add(context.Background(), Draft, []string{"aaa"})
	plan, err := s.Closure(context.Background(), items, Local{}, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	f := plan.Files[0]
	mods := filepath.Join(t.TempDir(), "mods")
	var last int64
	path, skipped, err := s.Download(context.Background(), f, mods, func(done, _ int64) { last = done })
	if err != nil || skipped || filepath.Base(path) != f.FileName || last == 0 {
		t.Fatalf("下载：%s %v %v %d", path, skipped, err, last)
	}
	if b, _ := os.ReadFile(path); sha1Hex(b) != f.SHA1 {
		t.Fatal("内容不对")
	}
	if _, skipped, err := s.Download(context.Background(), f, mods, nil); err != nil || !skipped {
		t.Fatalf("同名文件已在应跳过：%v %v", skipped, err)
	}
	// 哈希对不上：不留下任何文件
	g := f
	g.FileName, g.SHA1 = "other.jar", strings.Repeat("0", 40)
	if _, _, err := s.Download(context.Background(), g, mods, nil); err == nil {
		t.Fatal("哈希对不上应失败")
	}
	ents, _ := os.ReadDir(mods)
	if len(ents) != 1 {
		t.Fatalf("失败后目录里多了东西：%v", ents)
	}
	// 带扩展名的设备名（nul.jar、COM1.jar）：Windows 11 上 IsLocal 放过，这里不看系统版本一律不收（C-2e）
	for _, name := range []string{"../evil.jar", "a/b.jar", ".hidden.jar", "x.zip", "con:.jar", "", "nul.jar", "COM1.jar", "lpt³.jar"} {
		h := f
		h.FileName = name
		if _, _, err := s.Download(context.Background(), h, mods, nil); err == nil {
			t.Errorf("文件名 %q 应拒绝", name)
		}
	}
	h := f
	h.SHA1 = ""
	if _, _, err := s.Download(context.Background(), h, mods, nil); err == nil {
		t.Error("没有 SHA-1 应拒绝")
	}
	official := New(t.TempDir(), "")
	h = f
	h.FileName = "fresh.jar"
	if _, _, err := official.Download(context.Background(), h, mods, nil); err == nil || !strings.Contains(err.Error(), "地址") {
		t.Errorf("真的 Modrinth 时只认它的 CDN：%v", err)
	}
}

// 看服务器目录里已有的：按 SHA-1 认出项目，停用的不算，modinfo 读出模组 ID。
func TestScanLocal(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	dir := t.TempDir()
	fn := "fapi-0.92.2+1.20.1.jar"
	if err := os.WriteFile(filepath.Join(dir, "renamed-fabric-api.jar"), mr.files[fn], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib4-1.0.jar.disabled"), mr.files["lib4-1.0.jar"], 0o644); err != nil {
		t.Fatal(err)
	}
	writeJar(t, dir, "custom.jar", map[string]string{"fabric.mod.json": `{"id":"custom-lib","version":"1.0","provides":["clib"]}`})
	l := s.ScanLocal(context.Background(), dir)
	if l.Projects["fapi"] != "renamed-fabric-api.jar" || len(l.Projects) != 1 {
		t.Fatalf("按哈希认出的：%+v", l.Projects)
	}
	if !slices.Equal(l.Files, []string{"custom.jar", "renamed-fabric-api.jar"}) {
		t.Fatalf("文件：%v", l.Files)
	}
	if !slices.Contains(l.IDs, "custom-lib") {
		t.Fatalf("模组 ID：%v", l.IDs)
	}
	if got := s.ScanLocal(context.Background(), filepath.Join(dir, "没有")); len(got.Files) != 0 {
		t.Fatal("目录不存在时应为空")
	}
	// 同一个项目有两份 jar（内容一样）：固定认按文件名排在前面的那份
	if err := os.WriteFile(filepath.Join(dir, "a-copy.jar"), mr.files[fn], 0o644); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if got := s.ScanLocal(context.Background(), dir).Projects["fapi"]; got != "a-copy.jar" {
			t.Fatalf("两份 jar 时应固定认 a-copy.jar：%q", got)
		}
	}
}

// 复审补的：加入时只收模组和插件；放不进去时，全是查不到才报 404；写盘失败时内存里的车不变。
func TestAddKindsAndErrors(t *testing.T) {
	mr := newFakeMR(t)
	pack := mrProject{ID: "pack", Slug: "some-pack", Title: "Some Pack", ProjectType: "modpack", ServerSide: "required"}
	mr.projects["pack"] = pack
	s := New(t.TempDir(), mr.srv.URL)
	_, skipped, err := s.Add(context.Background(), Draft, []string{"some-pack"})
	if err == nil || IsNotFound(err) || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "整合包") {
		t.Fatalf("整合包不该放进车，应报 400：%v %+v", err, skipped)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"no-such", "client"}); err == nil || IsNotFound(err) {
		t.Fatalf("有查不到的也有客户端模组，应报 400：%v", err)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"no-such", "gone"}); !IsNotFound(err) {
		t.Fatalf("全都查不到应报 404：%v", err)
	}

	// 车放在一个写不进去的地方（目录名其实是个文件）：报错，内存里也不留
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bs := New(blocked, mr.srv.URL)
	if _, _, err := bs.Add(context.Background(), Draft, []string{"aaa"}); err == nil {
		t.Fatal("写盘失败应报错")
	}
	if got := bs.List(Draft); len(got) != 0 {
		t.Fatalf("写盘失败后内存里不该有：%+v", got)
	}

	// 拼进接口路径的 ID 不许是 . 和 ..
	if _, err := s.versions(context.Background(), "..", fabric, ""); !IsNotFound(err) {
		t.Fatalf("项目 ID .. 应直接拒绝：%v", err)
	}
	if _, err := s.version(context.Background(), "a/b"); !IsNotFound(err) {
		t.Fatalf("版本 ID 带斜杠应直接拒绝：%v", err)
	}
}

// Modrinth 没有回应：操作自己给的时限到了报 502（不是裸的 context deadline exceeded），调用方取消的原样返回取消。
func TestModrinthTimeout(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(hang.Close)
	s := New(t.TempDir(), hang.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := s.Search(ctx, SearchQuery{Q: "x", Type: "mod", Loaders: fabric, MC: "1.20.1"})
	var e *Error
	if !errors.As(err, &e) || e.Code != 502 {
		t.Fatalf("时限到了应报 502：%v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, _, err := s.Add(ctx, Draft, []string{"aaa"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应原样返回：%v", err)
	}
}

// 复审补的闭包边角：
//   - 可选依赖钉的旧版本不能拿来当必装前置的版本
//   - 内嵌（只写了版本 ID）的算满足，别的模组再要它也不装
//   - 和自动补的前置不兼容要报出来
//   - 只写了版本 ID、那个版本已经没有了的必装依赖写进找不到
//   - 文件名不分大小写（Windows）
func TestClosureEdges(t *testing.T) {
	mr := newFakeMR(t)
	for _, id := range []string{"x1", "x2", "x3", "x4", "x5", "x6", "pin"} {
		mr.proj(id, id+"-mod", strings.ToUpper(id), "required", "MIT")
	}
	mr.ver("pin", "vpin-old", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	mr.ver("pin", "vpin-new", "2.0", "release", "2026-02-01T00:00:00Z", "1.20.1")
	mr.ver("x1", "vx1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1",
		mrDep{VersionID: "vpin-old", ProjectID: "pin", DependencyType: "optional"}, mrDep{ProjectID: "pin", DependencyType: "required"})
	mr.ver("x2", "vx2", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{VersionID: "v-lib4", DependencyType: "embedded"})
	mr.ver("x3", "vx3", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{ProjectID: "lib4", DependencyType: "required"})
	mr.ver("x4", "vx4", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{ProjectID: "fapi", DependencyType: "required"})
	mr.ver("x5", "vx5", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{ProjectID: "fapi", DependencyType: "incompatible"})
	mr.ver("x6", "vx6", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{VersionID: "v-gone", DependencyType: "required"})
	s := New(t.TempDir(), mr.srv.URL)
	items, _, err := s.Add(context.Background(), Draft, []string{"x1", "x2", "x3", "x4", "x5", "x6", "aaa"})
	if err != nil {
		t.Fatal(err)
	}
	local := Local{Files: []string{"AAA-1.0.0.JAR"}, Projects: map[string]string{}}
	plan, err := s.Closure(context.Background(), items, local, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	var pinFile string
	for _, f := range plan.Files {
		switch f.ProjectID {
		case "pin":
			pinFile = f.FileName
		case "lib4":
			t.Fatalf("lib4 已经内嵌在 X2 里，不该再装：%+v", f)
		case "aaa":
			t.Fatalf("服务器上已有 AAA-1.0.0.JAR（大小写不同），不该再装：%+v", f)
		}
	}
	if pinFile != "pin-2.0.jar" {
		t.Fatalf("必装的 pin 应挑最新的正式版，不是可选依赖钉的旧版：%q", pinFile)
	}
	if !slices.Contains(plan.LocalHits, "AAA-1.0.0.JAR") {
		t.Fatalf("已有：%v", plan.LocalHits)
	}
	if !slices.ContainsFunc(plan.Conflicts, func(c string) bool { return strings.Contains(c, "X5 和 Fabric API（自动补的前置）不兼容") }) {
		t.Fatalf("冲突：%v", plan.Conflicts)
	}
	if !slices.ContainsFunc(plan.Unresolved, func(u Unresolved) bool { return strings.Contains(u.Reason, "X6 需要的那个版本") }) {
		t.Fatalf("找不到：%+v", plan.Unresolved)
	}
}
