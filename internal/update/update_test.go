package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		tag, current string
		want         bool
	}{
		{"v2.9.0", "2.6.1", true},
		{"v2.6.1", "2.6.1", false},
		{"v2.6.0", "2.6.1", false},
		{"v2.10.0", "2.9.0", true},
		// 同号的正式版比测试版新，测试版不比同号正式版新
		{"v2.9.0", "2.9.0-beta.1", true},
		{"v2.9.0-beta.1", "2.9.0", false},
		// beta.3 起「最新版」渠道会查到预发布版，测试版之间要分得出新旧（原来只比正式版，这一条是 false）
		{"v2.9.0-beta.2", "2.9.0-beta.1", true},
		{"v2.9.1", "2.9.0-beta.1", true},
		{"v2.8.9", "2.9.0-beta.1", false},
		{"v3.0.0", "2.9.0-beta.2", true}, // 装着测试版的人收得到 3.0.0 正式版
	} {
		if got := newer(tc.tag, tc.current); got != tc.want {
			t.Errorf("newer(%q, %q) = %v，应为 %v", tc.tag, tc.current, got, tc.want)
		}
	}
}

// 按语义化版本的规矩比：先比三段数字；数字相同时正式版比预发布版新；都是预发布时逐段比后缀
// （纯数字的段按数字比，别的按字符串比，纯数字的段比非数字的段旧，段数少的更旧）。
func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		// 三段数字
		{"3.2.0", "3.1.9", 1},
		{"3.10.0", "3.9.9", 1},
		{"2.0.0", "10.0.0", -1},
		{"3.2.1", "3.2.0", 1},
		{"3.2.0", "3.2.0", 0},
		// v 前缀、空白不算
		{"v3.2.0", "3.2.0", 0},
		{" v3.2.0\n", "3.2.0", 0},
		{"V3.2.0-beta.3", "v3.2.0-beta.3", 0},
		{"\tv3.2.0-beta.3 ", "3.2.0-beta.2", 1},
		// 数字相同时正式版比预发布版新
		{"3.2.0", "3.2.0-beta.3", 1},
		{"3.2.0-beta.3", "3.2.0", -1},
		{"3.2.0-rc.1", "3.2.0", -1},
		// 数字不同时先看数字，后缀不管
		{"3.1.2", "3.2.0-beta.1", -1},
		{"3.2.0-beta.1", "3.1.2", 1},
		{"3.2.0-alpha.1", "3.1.99", 1},
		// 后缀逐段比：纯数字按数字比
		{"3.2.0-beta.2", "3.2.0-beta.3", -1},
		{"3.2.0-beta.3", "3.2.0-beta.10", -1},
		{"3.2.0-beta.10", "3.2.0-beta.9", 1},
		{"3.2.0-beta.02", "3.2.0-beta.2", 0}, // 前导零不算
		// 别的按字符串比：alpha < beta < rc
		{"3.2.0-alpha.9", "3.2.0-beta.1", -1},
		{"3.2.0-beta.9", "3.2.0-rc.1", -1},
		{"3.2.0-rc.1", "3.2.0-alpha.5", 1},
		// A84 严格按 SemVer ASCII 序比大小写：'B' < 'b'，所以 Beta.2 < beta.1
		{"3.2.0-Beta.2", "3.2.0-beta.1", -1},
		{"1.0.0-A", "1.0.0-a", -1},
		{"1.0.0-Beta.2", "1.0.0-alpha.1", -1},
		// 段数少的更旧
		{"3.2.0-beta", "3.2.0-beta.1", -1},
		{"3.2.0-beta.1", "3.2.0-beta.1.1", -1},
		// 纯数字的段比非数字的段旧
		{"3.2.0-1", "3.2.0-beta", -1},
		{"3.2.0-beta.1", "3.2.0-beta.x", -1},
		// +构建信息不参与比较
		{"3.2.0+build.5", "3.2.0", 0},
		{"3.2.0-beta.3+abc", "3.2.0-beta.3", 0},
		// 很大的数字不溢出
		{"3.2.0-beta.99999999999999999999", "3.2.0-beta.9", 1},
	} {
		if got := compare(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%q, %q) = %d，应为 %d", tc.a, tc.b, got, tc.want)
		}
		if got := compare(tc.b, tc.a); got != -tc.want {
			t.Errorf("compare(%q, %q) = %d，应为 %d（反过来比）", tc.b, tc.a, got, -tc.want)
		}
	}
	for v, want := range map[string]bool{"3.2.0-beta.3": true, " v3.2.0 ": false, "3.2.0": false, "3.2.0-rc.1+b": true, "3.2.0+b": false} {
		if got := IsPre(v); got != want {
			t.Errorf("IsPre(%q) = %v，应为 %v", v, got, want)
		}
	}
}

// fakeRel 假 GitHub 上的一个发布。
type fakeRel struct {
	tag         string
	pre, draft  bool
	noChecksums bool // 不带 SHA256SUMS
}

// fakeGitHub 起一个假的 GitHub：/repos/o/r/releases（列表，按给的顺序）、/repos/o/r/releases/latest（stable），
// 附件 /dl/<标签>/MineDeck.exe 和 /dl/<标签>/SHA256SUMS（里面的哈希是「hash-<标签>」）。返回列表接口收到的查询串。
func fakeGitHub(t *testing.T, stable string, list []fakeRel) *[]string {
	t.Helper()
	var queries []string
	var base string // 假 GitHub 的地址，起来以后才知道；处理请求时已经填好
	asJSON := func(r fakeRel) map[string]any {
		assets := []map[string]any{{"name": "MineDeck.exe", "browser_download_url": base + "/dl/" + r.tag + "/MineDeck.exe", "size": 12 << 20}}
		if !r.noChecksums {
			assets = append(assets, map[string]any{"name": "SHA256SUMS", "browser_download_url": base + "/dl/" + r.tag + "/SHA256SUMS", "size": 100})
		}
		return map[string]any{"tag_name": r.tag, "body": "说明 " + r.tag, "draft": r.draft, "prerelease": r.pre, "assets": assets}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		out := []map[string]any{}
		for _, x := range list {
			out = append(out, asJSON(x))
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		for _, x := range list {
			if x.tag == stable {
				_ = json.NewEncoder(w).Encode(asJSON(x))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		tag, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/dl/"), "/")
		if file != "SHA256SUMS" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "hash-other *MineDeck-portable.zip\nhash-%s *MineDeck.exe\n", tag)
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
	return &queries
}

// 假 GitHub 上的发布：列表故意乱序，版本号最大的是草稿，还混着一个标签不是版本号的发布。
var fakeList = []fakeRel{
	{tag: "v3.2.0-beta.2", pre: true},
	{tag: "v3.2.0-beta.10", pre: true},
	{tag: "v3.1.2"},
	{tag: "v3.3.0", draft: true}, // 草稿：没发出来，不算
	{tag: "companion-v9.9.9"},    // 不是这个程序的版本号，不比
	{tag: "v99"},                 // 只有一段数字，不是「主.次.修订」，不比（不跳过的话按 99.0.0 算，它最大）
	{tag: "v3.2.0-beta.3", pre: true},
	{tag: "v3.1.1"},
	{tag: "v3.2.0-beta.11", draft: true, pre: true}, // 草稿的预发布也不算
}

func TestCheckChannels(t *testing.T) {
	queries := fakeGitHub(t, "v3.1.2", fakeList)
	ctx := context.Background()
	for _, tc := range []struct {
		name, channel, current string
		tag                    string
		pre, has               bool
	}{
		{"稳定版：用着旧正式版，收到最新正式版", app.UpdateStable, "3.1.1", "v3.1.2", false, true},
		{"稳定版：已是最新正式版", app.UpdateStable, "3.1.2", "v3.1.2", false, false},
		// 不降级：用着比最新正式版还新的测试版，稳定渠道不提示「有更新」
		{"稳定版：用着更新的测试版", app.UpdateStable, "3.2.0-beta.3", "v3.1.2", false, false},
		{"最新版：取版本号最大的预发布版，不看接口顺序", app.UpdateLatest, "3.2.0-beta.3", "v3.2.0-beta.10", true, true},
		{"最新版：正式版用户也收测试版", app.UpdateLatest, "3.1.2", "v3.2.0-beta.10", true, true},
		{"最新版：已是最新测试版", app.UpdateLatest, "3.2.0-beta.10", "v3.2.0-beta.10", true, false},
		// 不降级：用着比列表里都新的版本（自己编的），最新版渠道也不提示
		{"最新版：用着更新的版本", app.UpdateLatest, "3.2.0", "v3.2.0-beta.10", true, false},
		{"认不得的渠道按稳定版", "nightly", "3.1.1", "v3.1.2", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel, has, err := check(ctx, "o/r", tc.channel, tc.current)
			if err != nil {
				t.Fatal(err)
			}
			if rel.Tag != tc.tag || rel.Prerelease != tc.pre || has != tc.has {
				t.Fatalf("查到 %s（预发布 %v），有更新 %v；应为 %s（%v），%v", rel.Tag, rel.Prerelease, has, tc.tag, tc.pre, tc.has)
			}
			wantCh := tc.channel
			if wantCh != app.UpdateLatest {
				wantCh = app.UpdateStable
			}
			if rel.Channel != wantCh {
				t.Errorf("渠道 %q，应为 %q", rel.Channel, wantCh)
			}
			// 附件和校验和的处理两条渠道一样：exe 地址、官方 SHA256SUMS 里对应 MineDeck.exe 的那一行
			if !strings.HasSuffix(rel.AssetURL, "/dl/"+tc.tag+"/MineDeck.exe") || rel.SHA256 != "hash-"+tc.tag || rel.Notes != "说明 "+tc.tag {
				t.Errorf("附件 %q 校验和 %q 说明 %q", rel.AssetURL, rel.SHA256, rel.Notes)
			}
		})
	}
	if len(*queries) == 0 || !strings.Contains((*queries)[0], "per_page=") {
		t.Errorf("列表接口要带 per_page：%q", *queries)
	}
}

// 预发布版不放宽：没有 SHA256SUMS 的测试版查得到，但 Apply 照样拒绝（在动手下载之前）。
func TestLatestChannelStillNeedsChecksums(t *testing.T) {
	fakeGitHub(t, "v3.1.2", []fakeRel{{tag: "v3.1.2"}, {tag: "v3.2.0-beta.4", pre: true, noChecksums: true}})
	rel, has, err := check(context.Background(), "o/r", app.UpdateLatest, "3.2.0-beta.3")
	if err != nil || rel.Tag != "v3.2.0-beta.4" || !has {
		t.Fatalf("%v %v %v", rel, has, err)
	}
	if rel.SHA256 != "" {
		t.Fatalf("没有 SHA256SUMS 时不该有校验和：%q", rel.SHA256)
	}
	err = Apply(context.Background(), rel)
	// Mac 版还不能在程序里换程序，Apply 一进来就拦下（不白下一个 Windows 的 exe），走不到看校验和那一步
	if runtime.GOOS != "windows" {
		if !errors.Is(err, errNoSelfUpdate) {
			t.Fatalf("Mac 版应先拦下「不能在程序里自动更新」：%v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
		t.Fatalf("没有官方校验和的预发布版应拒绝更新：%v", err)
	}
}

// 列表里没有能用的发布（全是草稿、标签不是版本号）：和稳定版一样报「还没有任何发布版本」；仓库没填照旧先报。
func TestLatestChannelNothingUsable(t *testing.T) {
	fakeGitHub(t, "", []fakeRel{{tag: "v3.3.0", draft: true}, {tag: "companion-v1.0.0"}, {tag: ""}})
	if _, _, err := check(context.Background(), "o/r", app.UpdateLatest, "3.2.0"); err == nil || !strings.Contains(err.Error(), "还没有任何发布版本") {
		t.Fatalf("应报还没有发布版本：%v", err)
	}
	if _, _, err := check(context.Background(), " ", app.UpdateLatest, "3.2.0"); err == nil || !strings.Contains(err.Error(), "未配置更新仓库") {
		t.Fatalf("应报未配置更新仓库：%v", err)
	}
}

// Check 按配置里的渠道查；没选过时按正在用的版本定：测试版按最新版，正式版按稳定版。启动时的后台检查也走 Check。
func TestCheckFollowsConfig(t *testing.T) {
	fakeGitHub(t, "v3.1.2", fakeList)
	oldBase, oldVer := app.Base, currentVersion
	oldCfg := app.GetConfig()
	app.Base = t.TempDir() // UpdateConfig 会写 config.json，别写进源码目录
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { *c = oldCfg; return nil })
		app.Base, currentVersion = oldBase, oldVer
	})
	for _, tc := range []struct {
		choice, current, tag string
	}{
		{"", "3.2.0-beta.3", "v3.2.0-beta.10"}, // 没选过、用着测试版：按最新版
		{"", "3.1.1", "v3.1.2"},                // 没选过、用着正式版：按稳定版
		{app.UpdateStable, "3.2.0-beta.3", "v3.1.2"},
		{app.UpdateLatest, "3.1.1", "v3.2.0-beta.10"},
	} {
		currentVersion = tc.current
		if _, err := app.UpdateConfig(func(c *app.Config) error { c.UpdateChannel = tc.choice; return nil }); err != nil {
			t.Fatal(err)
		}
		rel, _, err := Check(context.Background(), "o/r")
		if err != nil {
			t.Fatal(err)
		}
		if rel.Tag != tc.tag {
			t.Errorf("选的 %q、用着 %s：查到 %s，应为 %s", tc.choice, tc.current, rel.Tag, tc.tag)
		}
	}
}

// A84：SemVer 2.0 规范里的递增顺序链，以及本仓库 docs/releases 全部现有标签两两比较严格保持有序。
func TestSemVerSpecAndExistingReleaseTags(t *testing.T) {
	spec := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 0; i < len(spec); i++ {
		for j := 0; j < len(spec); j++ {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := compare(spec[i], spec[j]); got != want {
				t.Errorf("SemVer 规范链 compare(%q, %q) = %d，应为 %d", spec[i], spec[j], got, want)
			}
		}
	}

	// 收集自 docs/releases/*.md 的全部现有标签（从旧到新）。
	existing := []string{
		"v2.9.0-beta.1",
		"v2.9.0-beta.2",
		"v3.0.0",
		"v3.1.0-beta.1",
		"v3.1.0-beta.2",
		"v3.1.0-beta.3",
		"v3.1.0-beta.4",
		"v3.1.0",
		"v3.1.1",
		"v3.1.2",
		"v3.2.0-beta.1",
		"v3.2.0-beta.2",
		"v3.2.0-beta.3",
		"v3.2.0-beta.4",
		"v3.2.0-beta.5",
		"v3.2.0",
		"v3.2.1",
		"v3.2.2",
		"v3.2.3",
		"v3.2.4",
		"v3.2.5",
		"v3.2.6",
	}
	for i := 0; i < len(existing); i++ {
		for j := 0; j < len(existing); j++ {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := compare(existing[i], existing[j]); got != want {
				t.Errorf("现有标签两两比较 compare(%q, %q) = %d，应为 %d", existing[i], existing[j], got, want)
			}
		}
	}
}

// A80：GitHub 的 releases/latest 返回预发布版（标签带 -beta，或者 prerelease=true）时，
// 稳定渠道必须忽略它、回退到第一页发布列表找最新正式版；列表里没有正式版就当没有更新。
func TestStableChannelSkipsPrereleaseLatest(t *testing.T) {
	ctx := context.Background()

	t.Run("latest 是带 -beta 的正式发布：回退列表拿到真正的最新正式版", func(t *testing.T) {
		queries := fakeGitHub(t, "v3.3.0-beta.1", []fakeRel{
			{tag: "v3.3.0-beta.1", pre: false}, // 故意误标为正式版并设为 latest
			{tag: "v3.2.5"},
			{tag: "v3.2.6"},
			{tag: "v3.3.0-beta.2", pre: true},
		})
		rel, has, err := check(ctx, "o/r", app.UpdateStable, "3.2.5")
		if err != nil {
			t.Fatal(err)
		}
		if !has || rel.Tag != "v3.2.6" || rel.Prerelease {
			t.Fatalf("应回退拿到 v3.2.6 正式版并提示更新，实际 %+v has=%v", rel, has)
		}
		if len(*queries) != 1 || (*queries)[0] != "per_page=30" {
			t.Errorf("应只拉第一页发布列表，实际 %v", *queries)
		}
		rel2, has2, err := check(ctx, "o/r", app.UpdateStable, "3.2.6")
		if err != nil || has2 || rel2.Tag != "v3.2.6" {
			t.Fatalf("当前已是 v3.2.6 不应误报更新：%v has=%v err=%v", rel2, has2, err)
		}
	})

	t.Run("latest 标成 prerelease=true：同样回退找正式版", func(t *testing.T) {
		fakeGitHub(t, "v3.3.0", []fakeRel{
			{tag: "v3.3.0", pre: true},
			{tag: "v3.2.6"},
		})
		rel, has, err := check(ctx, "o/r", app.UpdateStable, "3.2.5")
		if err != nil || !has || rel.Tag != "v3.2.6" {
			t.Fatalf("应忽略 prerelease=true 的 latest，拿到 v3.2.6，实际 %+v has=%v err=%v", rel, has, err)
		}
	})

	t.Run("第一页全都是预发布或草稿、找不到正式版：当没有更新（不翻页、不报错）", func(t *testing.T) {
		queries := fakeGitHub(t, "v3.3.0-beta.1", []fakeRel{
			{tag: "v3.3.0-beta.1", pre: false},
			{tag: "v3.2.0-beta.5", pre: true},
			{tag: "v3.2.6", draft: true},
		})
		rel, has, err := check(ctx, "o/r", app.UpdateStable, "3.2.4")
		if err != nil {
			t.Fatalf("找不到正式版应当没有更新而不是报错：%v", err)
		}
		if has || rel.Tag != "" || rel.Channel != app.UpdateStable {
			t.Fatalf("找不到正式版时 hasUpdate 应为 false、Tag 为空：%+v has=%v", rel, has)
		}
		if len(*queries) != 1 {
			t.Errorf("只应请求一页发布列表，实际 %d 次：%v", len(*queries), *queries)
		}
	})
}

// A78：启动时后台检查在日志写明渠道和结果，查到新版本时记入内存通知，已是最新或清渠道时清掉。
func TestRunStartupCheckLogsAndNotice(t *testing.T) {
	ClearStartupUpdate()
	t.Cleanup(ClearStartupUpdate)

	oldBase, oldVer := app.Base, currentVersion
	oldCfg := app.GetConfig()
	app.Base = t.TempDir()
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { *c = oldCfg; return nil })
		app.Base, currentVersion = oldBase, oldVer
	})

	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	fakeGitHub(t, "v3.2.4", []fakeRel{
		{tag: "v3.2.4"},
		{tag: "v3.3.0-beta.1", pre: true},
	})

	// 1. 稳定版渠道已是最新
	currentVersion = "3.2.4"
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.UpdateChannel = ""; return nil })
	RunStartupCheck(context.Background(), "o/r", logf)
	if StartupUpdate() != nil {
		t.Fatalf("已是最新不应记住通知：%+v", StartupUpdate())
	}
	if !slices.ContainsFunc(logs, func(s string) bool { return strings.Contains(s, "稳定版渠道：已是最新 v3.2.4") }) {
		t.Fatalf("日志应写明稳定版渠道和已是最新：%v", logs)
	}

	// 2. 测试版渠道有新版本：记进内存，日志写明渠道与版本
	logs = nil
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.UpdateChannel = app.UpdateLatest; return nil })
	RunStartupCheck(context.Background(), "o/r", logf)
	su := StartupUpdate()
	if su == nil || su.Tag != "v3.3.0-beta.1" || !su.Prerelease || su.Channel != app.UpdateLatest {
		t.Fatalf("启动更新通知不对：%+v", su)
	}
	if !slices.ContainsFunc(logs, func(s string) bool { return strings.Contains(s, "测试版渠道：有新版本 v3.3.0-beta.1") }) {
		t.Fatalf("日志应写明测试版渠道和有新版本：%v", logs)
	}

	// 3. 切到稳定版再查一次已是最新：内存中通知被清掉
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.UpdateChannel = app.UpdateStable; return nil })
	if _, has, err := Check(context.Background(), "o/r"); err != nil || has {
		t.Fatalf("切到稳定版手动检查：has=%v err=%v", has, err)
	}
	if StartupUpdate() != nil {
		t.Fatalf("已无更新后通知应已清除：%+v", StartupUpdate())
	}

	// 4. 失败时日志也要带渠道
	logs = nil
	RunStartupCheck(context.Background(), "invalid-repo", logf)
	if !slices.ContainsFunc(logs, func(s string) bool { return strings.Contains(s, "稳定版渠道：后台检查更新失败") }) {
		t.Fatalf("失败日志应写明渠道：%v", logs)
	}
}
