package inst

// beta.3 第二层（新建流程、任务页、没装上的文件的后端）：R09 的清单落盘和按服务器处置、取消显示成「已取消」。

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// 取消、半成品也清干净了：任务按「已取消」结束，error 里是中文说明、没有 context canceled，步骤不画红叉；
// 取消以后半成品没清干净的照旧是失败；没取消过的失败照旧是失败（2026-11-14 复审发现 9）。
func TestFailCreateEndsAsCancelled(t *testing.T) {
	mg := tasks.NewManager()
	tk, ctx := mg.New("创建服务器 测试", []string{"下载", "配置"})
	tk.EnableCancel()
	tk.StartStep(0)
	if !mg.Cancel(tk.ID()) {
		t.Fatal("应收下取消")
	}
	if sn := tk.Snapshot(); !sn.Cancelling || sn.Cancelled || sn.Ended {
		t.Fatalf("清理期间应是「正在取消」：%+v", sn)
	}
	failCreate(tk, ctx.Err(), nil)
	sn := tk.Snapshot()
	if !sn.Ended || !sn.Cancelled || sn.Cancelling || sn.Err != "已取消，没有创建服务器" {
		t.Fatalf("应按「已取消」结束：%+v", sn)
	}
	if sn.Steps[0].Status != "cancelled" || sn.Steps[1].Status != "pending" {
		t.Fatalf("正在做的那一步应标成 cancelled，不是 error：%+v", sn.Steps)
	}
	for _, line := range sn.Log {
		if strings.Contains(line, "context canceled") || strings.Contains(line, "❌") {
			t.Fatalf("日志里不该有英文的取消原因、也不该像失败：%q", line)
		}
	}
	if s := mg.List()[0]; !s.Cancelled || s.Err == "" {
		t.Fatalf("列表摘要也要带上「已取消」，error 照旧有那句中文：%+v", s)
	}

	// 取消了、但半成品没清干净：是失败
	tk2, ctx2 := mg.New("创建服务器 测试2", []string{"下载"})
	tk2.EnableCancel()
	mg.Cancel(tk2.ID())
	failCreate(tk2, ctx2.Err(), errors.New("文件被占用"))
	if sn := tk2.Snapshot(); sn.Cancelled || !strings.Contains(sn.Err, "已取消") || !strings.Contains(sn.Err, "清理半成品失败") {
		t.Fatalf("没清干净的应是失败、写明清理失败：%+v", sn)
	}

	// 没取消过：照旧失败，原因原样
	tk3, _ := mg.New("创建服务器 测试3", []string{"下载"})
	tk3.EnableCancel()
	failCreate(tk3, errors.New("下载失败"), nil)
	if sn := tk3.Snapshot(); sn.Cancelled || sn.Err != "下载失败" {
		t.Fatalf("普通失败：%+v", sn)
	}
	// 没收下过取消请求的任务调 EndCancelled 也只算失败
	tk4, _ := mg.New("别的任务", nil)
	tk4.EndCancelled(errors.New("已取消"))
	if sn := tk4.Snapshot(); sn.Cancelled || !sn.Ended {
		t.Fatalf("没取消过的任务不能记成已取消：%+v", sn)
	}
}

// restartEnv 一台登记在 app.ServersDir 里的服务器（真的写了 instance.json），newMgr 每调一次就是「程序重开一次」。
func restartEnv(t *testing.T) (dir string, newMgr func() *Manager) {
	t.Helper()
	importEnv(t)
	dir = filepath.Join(app.ServersDir, "pack-server")
	s, _ := json.Marshal(Settings{Name: "整合包服", Core: "forge", MC: "1.20.1", XmxMB: 4096, CreatedAt: time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)})
	writeTree(t, dir, map[string]string{"instance.json": string(s), "server.properties": "level-name=world\n", "world/level.dat": "world"})
	return dir, func() *Manager {
		m, err := NewManager()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Get("整合包服"); err != nil {
			t.Fatalf("重开以后没认出这台服务器：%v", err)
		}
		return m
	}
}

// 程序重开（新建一个 Manager 读同一个文件夹）以后，清单读得回来（跳过的也在），按服务器能接着处置；
// txt 跟着变，不提「任务面板」「任务日志」，指向详情页；都处理完两份文件都删掉。
func TestPendingListSurvivesRestart(t *testing.T) {
	dir, newMgr := restartEnv(t)
	m1 := newMgr()
	i1, _ := m1.Get("整合包服")
	tk, _ := m1.Tasks.New("导入整合包 测试包", nil)
	data := []byte("trusted-jar")
	m1.finishImportPack(tk, i1, "测试包", []modpack.PendingFile{
		{Path: "mods/a.jar", Reason: modpack.ReasonNoHash},
		{Path: "mods/b.jar", Reason: modpack.ReasonHost, SHA1: sha1Of(t, data), URLs: []string{"https://93.184.216.34/b.jar"}},
		{Path: "config/c.toml", Reason: modpack.ReasonBadURL},
	}, nil)
	for _, f := range []string{PendingListFile, PendingReportFile} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Fatalf("导入收尾应把清单存进服务器文件夹：%s %v", f, err)
		}
	}
	// 这次运行里跳过一项：任务上的清单和「N 个文件待处理」跟着变
	out, v, err := m1.ResolveInstancePending(context.Background(), "整合包服", []PendingAction{{ID: "p3", Act: "skip"}})
	if err != nil || len(out) != 1 || !out[0].OK || len(v.Pending) != 2 || len(v.Skipped) != 1 {
		t.Fatalf("跳过：%+v %+v %v", out, v, err)
	}
	if left := tk.PendingItems(); len(left) != 2 {
		t.Fatalf("任务上的清单是镜像，跳过的那项应移走：%+v", left)
	}
	if w := warningOf(tk, "import-pending"); w == nil || w.Title != "2 个文件待处理" {
		t.Fatalf("「N 个文件待处理」应跟着变：%+v", w)
	}
	txt := readFile(t, filepath.Join(dir, PendingReportFile))
	for _, want := range []string{"测试包", "详情页", "【已跳过】config/c.toml", "mods/a.jar"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("txt 里应有「%s」：\n%s", want, txt)
		}
	}
	for _, bad := range []string{"任务面板", "任务日志"} {
		if strings.Contains(txt, bad) {
			t.Fatalf("txt 里不该再有「%s」：\n%s", bad, txt)
		}
	}

	// 程序重开：没有任务了，清单照样读得回来
	m2 := newMgr()
	v, err = m2.ImportPending("整合包服")
	if err != nil {
		t.Fatal(err)
	}
	if v.Pack != "测试包" || len(v.Pending) != 2 || len(v.Skipped) != 1 || v.Skipped[0].ID != "p3" {
		t.Fatalf("重开以后读回来的清单：%+v", v)
	}
	if p2 := v.Pending[1]; p2.ID != "p2" || p2.Host != "93.184.216.34" || strings.Join(p2.Options, ",") != "local,skip,trust" {
		t.Fatalf("站点名、可用处理方式应重算出来：%+v", p2)
	}
	if strings.Join(v.Skipped[0].Options, ",") != "local" {
		t.Fatalf("跳过的不再列「跳过」：%+v", v.Skipped[0])
	}
	// 重开以后用本地文件代替 p1、信任下载 p2、把跳过的 p3 改成用本地文件代替
	src := filepath.Join(t.TempDir(), "a.jar")
	if err := os.WriteFile(src, []byte("local-jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := modpackHTTP
	modpackHTTP = func() *http.Client { return dl.GuardedClientWithTransport(fakeTransport{content: data}) }
	t.Cleanup(func() { modpackHTTP = old })
	out, v, err = m2.ResolveInstancePending(context.Background(), "整合包服", []PendingAction{
		{ID: "p1", Act: "local", Path: src},
		{ID: "p2", Act: "trust", URL: "https://93.184.216.34/b.jar", Ack: true},
	})
	if err != nil || len(out) != 2 || !out[0].OK || !out[1].OK {
		t.Fatalf("重开以后处置：%+v %v", out, err)
	}
	if readFile(t, filepath.Join(dir, "mods", "a.jar")) != "local-jar" || readFile(t, filepath.Join(dir, "mods", "b.jar")) != "trusted-jar" {
		t.Fatal("文件没放进去")
	}
	if len(v.Pending) != 0 || len(v.Skipped) != 1 {
		t.Fatalf("处置以后的清单：%+v", v)
	}
	// 只剩跳过的一项：txt 还在（那个文件也没装上）
	if txt := readFile(t, filepath.Join(dir, PendingReportFile)); !strings.Contains(txt, "已跳过的（1 个") || strings.Contains(txt, "还没处理的") {
		t.Fatalf("只剩跳过的时 txt：\n%s", txt)
	}
	cfg := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(cfg, []byte("k=v"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, v, err = newMgr().ResolveInstancePending(context.Background(), "整合包服", []PendingAction{{ID: "p3", Act: "local", Path: cfg}})
	if err != nil || !out[0].OK || len(v.Pending)+len(v.Skipped) != 0 {
		t.Fatalf("跳过的也能再处理：%+v %+v %v", out, v, err)
	}
	for _, f := range []string{PendingListFile, PendingReportFile} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("都处理完了，%s 应删掉：%v", f, err)
		}
	}
	if _, _, err := newMgr().ResolveInstancePending(context.Background(), "整合包服", []PendingAction{{ID: "p1", Act: "skip"}}); err == nil {
		t.Fatal("没有清单时处置应报错")
	}
}

// 旧的按任务编号的接口照样能用，和按服务器的那个看的是同一份清单；都处理完，任务上那块「N 个文件待处理」去掉。
func TestPendingOldTaskAPIStaysInSync(t *testing.T) {
	dir, newMgr := restartEnv(t)
	m := newMgr()
	i, _ := m.Get("整合包服")
	tk, _ := m.Tasks.New("导入整合包 测试包", nil)
	m.finishImportPack(tk, i, "测试包", []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonNoHash}, {Path: "mods/b.jar", Reason: modpack.ReasonNoHash}}, nil)
	out, left, err := m.ResolveImportPending(context.Background(), tk.ID(), []PendingAction{{ID: "p1", Act: "skip"}})
	if err != nil || !out[0].OK || len(left) != 1 || left[0].ID != "p2" {
		t.Fatalf("旧接口跳过：%+v %+v %v", out, left, err)
	}
	if v, _ := m.ImportPending("整合包服"); len(v.Pending) != 1 || len(v.Skipped) != 1 {
		t.Fatalf("按服务器看到的应是同一份：%+v", v)
	}
	src := filepath.Join(t.TempDir(), "b.jar")
	if err := os.WriteFile(src, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, err := m.ResolveInstancePending(context.Background(), "整合包服", []PendingAction{{ID: "p2", Act: "local", Path: src}}); err != nil || !out[0].OK {
		t.Fatalf("按服务器处置：%+v %v", out, err)
	}
	if len(tk.PendingItems()) != 0 || warningOf(tk, "import-pending") != nil {
		t.Fatalf("都处理完（跳过的不算没处理），任务上的清单和告警应清掉：%+v %+v", tk.PendingItems(), tk.Snapshot().Warnings)
	}
	if readFile(t, filepath.Join(dir, "mods", "b.jar")) != "b" {
		t.Fatal("文件没放进去")
	}
}

// 清单文件被人改过：落点改成九个目录以外、控制文件、带 .. 的，local 和 trust 都拒绝，服务器文件夹和外面一个字节都不写；
// 非 https 的地址丢掉（不能被当链接画出来）、站点名按地址重算（不能冒充 modrinth）、重复和乱写的编号另给；跳过的照样读回来。
func TestPendingTamperedListIsRejected(t *testing.T) {
	dir, newMgr := restartEnv(t)
	outside := filepath.Join(filepath.Dir(dir), "outside")
	writeTree(t, outside, map[string]string{"keep.txt": "outside"})
	writeTree(t, dir, map[string]string{"mods/ok.jar": "ok", "config/a.toml": "a"})
	sha := sha1Of(t, []byte("payload"))
	bad := []string{
		"../outside/evil.jar",        // 带 ..，出服务器文件夹
		"config/../../outside/x.jar", // 带 ..，绕一圈出去
		"mods/../instance.json",      // 带 ..，落到控制文件
		"instance.json",              // 控制文件
		"run.bat",                    // 控制文件
		PendingReportFile,            // 控制文件（给人看的清单自己）
		".minedeck/import-pending.json",
		"server.properties",   // 九个目录以外
		"notes/readme.txt",    // 九个目录以外
		"world/level.dat",     // 世界
		"C:/Windows/evil.dll", // 绝对路径
	}
	type rec struct {
		ID      string   `json:"id"`
		Path    string   `json:"path"`
		Reason  string   `json:"reason"`
		Host    string   `json:"host,omitempty"`
		URLs    []string `json:"urls,omitempty"`
		SHA1    string   `json:"sha1,omitempty"`
		Options []string `json:"options"`
		Skipped bool     `json:"skipped,omitempty"`
	}
	var items []rec
	for k, p := range bad {
		items = append(items, rec{ID: "p" + strings.Repeat("1", 1) + string(rune('0'+k%10)), Path: p, Reason: "host", Host: "cdn.modrinth.com",
			URLs: []string{"https://93.184.216.34/x.jar"}, SHA1: sha, Options: []string{"local", "skip", "trust"}})
	}
	// 非 https 的地址、冒充的站点名、乱写的处理方式、重复的编号、跳过的项
	items = append(items,
		rec{ID: "p50", Path: "mods/link.jar", Reason: "host", Host: "cdn.modrinth.com", URLs: []string{"javascript:alert(1)", "http://93.184.216.34/a.jar", "https://93.184.216.34/a.jar"}, Options: []string{"trust"}},
		rec{ID: "p50", Path: "mods/dup.jar", Reason: "nohash", Options: []string{"trust"}},
		rec{ID: "../x", Path: "mods/weird-id.jar", Reason: "什么", Options: []string{"trust"}},
		rec{ID: "p60", Path: "config/skipped.toml", Reason: "badurl", Skipped: true},
	)
	b, _ := json.Marshal(map[string]any{"version": 1, "pack": "被改过的包", "items": items})
	writeTree(t, dir, map[string]string{PendingListFile: string(b)})
	before := treeOf(t, filepath.Dir(dir))

	m := newMgr()
	v, err := m.ImportPending("整合包服")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Pending) != len(bad)+3 || len(v.Skipped) != 1 || v.Skipped[0].Path != "config/skipped.toml" {
		t.Fatalf("读回来的项数：%d 个没处理、%+v 跳过", len(v.Pending), v.Skipped)
	}
	ids := map[string]bool{}
	for _, it := range append(v.Pending, v.Skipped...) {
		if ids[it.ID] {
			t.Fatalf("编号重复：%s", it.ID)
		}
		ids[it.ID] = true
		for _, u := range it.URLs {
			if !strings.HasPrefix(u, "https://") {
				t.Fatalf("非 https 的地址应丢掉：%+v", it)
			}
		}
		if it.Host != "" && it.Host != "93.184.216.34" {
			t.Fatalf("站点名应按地址重算，不能照抄文件里的：%+v", it)
		}
		if it.Reason != modpack.ReasonHost && slices.Contains(it.Options, "trust") {
			t.Fatalf("不是 host 的项不能信任下载：%+v", it)
		}
	}
	link := v.Pending[len(bad)]
	if len(link.URLs) != 1 || strings.Join(link.Options, ",") != "local,skip,trust" {
		t.Fatalf("混着坏地址的那一项：%+v", link)
	}
	// 两遍读出来的编号一样（界面拿着编号来处置）
	if v2, _ := m.ImportPending("整合包服"); v2.Pending[len(bad)+1].ID != v.Pending[len(bad)+1].ID || v2.Pending[len(bad)+2].ID != v.Pending[len(bad)+2].ID {
		t.Fatalf("另给的编号每次读都应一样：%+v / %+v", v.Pending, v2.Pending)
	}

	src := filepath.Join(t.TempDir(), "payload.jar")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	old := modpackHTTP
	modpackHTTP = func() *http.Client {
		calls++
		return dl.GuardedClientWithTransport(fakeTransport{content: []byte("payload")})
	}
	t.Cleanup(func() { modpackHTTP = old })
	for _, act := range []string{"local", "trust"} {
		var acts []PendingAction
		for _, it := range v.Pending[:len(bad)] {
			acts = append(acts, PendingAction{ID: it.ID, Act: act, Path: src, URL: "https://93.184.216.34/x.jar", Ack: true})
		}
		out, after, err := m.ResolveInstancePending(context.Background(), "整合包服", acts)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range out {
			if o.OK || o.Note == "" {
				t.Fatalf("%s：被改过的落点应拒绝并说明原因：%+v", act, o)
			}
		}
		if len(after.Pending) != len(v.Pending) {
			t.Fatalf("%s：被拒的项应留在清单里", act)
		}
	}
	if calls != 0 {
		t.Fatalf("落点不合规的不该发下载请求：%d 次", calls)
	}
	if got := treeOf(t, filepath.Dir(dir)); !maps2Equal(before, got) {
		t.Fatalf("处置被拒时什么都不该写：\n之前 %v\n之后 %v", before, got)
	}
}

// 清单文件被人改过、落点本身合规：trust 也只认重算过的那份记录。原因不是 host 的（哪怕文件里写了 https 地址和校验值、
// 自称能信任下载）、校验值不合规或干脆没有的，都不发下载请求、不写文件，用户点过确认也一样。
func TestPendingTamperedTrustNeedsHostAndHash(t *testing.T) {
	dir, newMgr := restartEnv(t)
	sha := sha1Of(t, []byte("payload"))
	url := "https://93.184.216.34/x.jar"
	list := `{"version":1,"items":[` +
		`{"id":"p1","path":"mods/nohash.jar","reason":"nohash","host":"cdn.modrinth.com","urls":["` + url + `"],"sha1":"` + sha + `","options":["trust"]},` +
		`{"id":"p2","path":"mods/badsha.jar","reason":"host","urls":["` + url + `"],"sha1":"zz` + sha[2:] + `","options":["trust"]},` +
		`{"id":"p3","path":"mods/nosha.jar","reason":"host","urls":["` + url + `"],"options":["trust"]}]}`
	writeTree(t, dir, map[string]string{PendingListFile: list})
	before := treeOf(t, filepath.Dir(dir))

	calls := 0
	old := modpackHTTP
	modpackHTTP = func() *http.Client {
		calls++
		return dl.GuardedClientWithTransport(fakeTransport{content: []byte("payload")})
	}
	t.Cleanup(func() { modpackHTTP = old })

	m := newMgr()
	v, err := m.ImportPending("整合包服")
	if err != nil || len(v.Pending) != 3 {
		t.Fatalf("读回来：%+v %v", v, err)
	}
	for _, it := range v.Pending {
		if it.ID == "p1" && slices.Contains(it.Options, "trust") {
			t.Fatalf("不是 host 的项读回来不该有 trust：%+v", it)
		}
		if it.ID != "p1" && it.SHA1 != "" {
			t.Fatalf("不合规的校验值应丢掉：%+v", it)
		}
	}
	var acts []PendingAction
	for _, id := range []string{"p1", "p2", "p3"} {
		acts = append(acts, PendingAction{ID: id, Act: "trust", URL: url, Ack: true})
	}
	out, after, err := m.ResolveInstancePending(context.Background(), "整合包服", acts)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"p1": "没有可确认下载的自定义站点", "p2": "没有校验值", "p3": "没有校验值"}
	for _, o := range out {
		if o.OK || !strings.Contains(o.Note, want[o.ID]) {
			t.Fatalf("%s 应拒绝并写明「%s」：%+v", o.ID, want[o.ID], o)
		}
	}
	if len(out) != 3 || len(after.Pending) != 3 {
		t.Fatalf("三项都该留在清单里：%+v / %+v", out, after.Pending)
	}
	if calls != 0 {
		t.Fatalf("不该发下载请求：%d 次", calls)
	}
	if got := treeOf(t, filepath.Dir(dir)); !maps2Equal(before, got) {
		t.Fatalf("什么都不该写：\n之前 %v\n之后 %v", before, got)
	}
}

// 接管来的文件夹里 .minedeck 是指到外面的目录联接：清单不读也不写，不顺着联接跑出去。
func TestPendingListRefusesJunctionedControlDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	dir, newMgr := restartEnv(t)
	outside := filepath.Join(filepath.Dir(dir), "elsewhere")
	writeTree(t, outside, map[string]string{"import-pending.json": `{"version":1,"items":[{"id":"p1","path":"mods/a.jar","reason":"nohash"}]}`})
	cmd := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dir, ".minedeck"), outside)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		skipOrFailJunction(t, err, out)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(dir, ".minedeck")) })
	m := newMgr()
	if _, err := m.ImportPending("整合包服"); err == nil || !strings.Contains(err.Error(), "外面") {
		t.Fatalf("联接到外面的清单不该读：%v", err)
	}
	if err := writePendingList(dir, &pendingList{Items: []pendingRecord{{PendingItem: tasks.PendingItem{ID: "p1", Path: "mods/b.jar"}}}}); err == nil {
		t.Fatal("联接到外面的清单不该写")
	}
	if readFile(t, filepath.Join(outside, "import-pending.json")) == "" {
		t.Fatal("外面的文件被动了")
	}
}

// 清单文件本身是控制文件：整合包覆盖目录、交接包都不收。
func TestPendingFilesAreControlFiles(t *testing.T) {
	for _, p := range []string{PendingListFile, PendingReportFile, ".MineDeck/IMPORT-PENDING.JSON"} {
		if !IsControlFile(p) || overrideAllowed(p) {
			t.Errorf("%s 应是控制文件", p)
		}
	}
}

func warningOf(t *tasks.Task, kind string) *tasks.Warning {
	for _, w := range t.Snapshot().Warnings {
		if w.Kind == kind {
			return &w
		}
	}
	return nil
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s：%v", p, err)
	}
	return string(b)
}

// treeOf 一个文件夹下所有文件的相对路径 → 内容。
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func maps2Equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
