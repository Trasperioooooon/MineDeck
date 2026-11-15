package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		{"3.2.0-Beta.2", "3.2.0-beta.1", 1}, // 大小写不算
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
	if err := Apply(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
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
