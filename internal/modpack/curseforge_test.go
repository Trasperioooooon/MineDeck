package modpack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCF 一个假的 CurseForge 接口：/v1/mods/files 和 /v1/mods 各回一段写好的 JSON（状态码可以改）。
func fakeCF(t *testing.T, files, mods string, filesCode, modsCode int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "测试用的 Key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string][]int64
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/mods/files":
			if len(body["fileIds"]) == 0 {
				t.Errorf("查文件的请求里没有 fileIds")
			}
			w.WriteHeader(filesCode)
			w.Write([]byte(files))
		case "/v1/mods":
			if len(body["modIds"]) == 0 {
				t.Errorf("查项目的请求里没有 modIds")
			}
			w.WriteHeader(modsCode)
			w.Write([]byte(mods))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	old := cfAPIBase
	cfAPIBase = srv.URL
	t.Cleanup(func() { cfAPIBase = old; srv.Close() })
}

const cfFilesJSON = `{"data":[
 {"id":101,"fileName":"jei-1.20.1.jar","downloadUrl":"https://edge.forgecdn.net/files/1/101/jei-1.20.1.jar","fileLength":10,"hashes":[{"value":"aaaa","algo":1},{"value":"bbbb","algo":2}]},
 {"id":102,"fileName":"Faithful 32x.zip","downloadUrl":"https://edge.forgecdn.net/files/1/102/Faithful.zip","fileLength":20,"hashes":[{"value":"cccc","algo":1}]},
 {"id":103,"fileName":"BSL_v10.zip","downloadUrl":"https://edge.forgecdn.net/files/1/103/BSL.zip","fileLength":30,"hashes":[]},
 {"id":104,"fileName":"recipes_datapack.zip","downloadUrl":"https://edge.forgecdn.net/files/1/104/dp.zip","fileLength":40,"hashes":[]},
 {"id":105,"fileName":"hapi-1.1.3.jar","downloadUrl":null,"fileLength":50,"hashes":[{"value":"dddd","algo":1}]},
 {"id":107,"fileName":"../../instance.json","downloadUrl":"https://edge.forgecdn.net/files/1/107/x.jar","fileLength":60,"hashes":[]},
 {"id":108,"fileName":"by-class.zip","downloadUrl":"https://edge.forgecdn.net/files/1/108/x.zip","fileLength":70,"hashes":[]},
 {"id":109,"fileName":"unknown-kind.zip","downloadUrl":"https://edge.forgecdn.net/files/1/109/x.zip","fileLength":80,"hashes":[]}
]}`

const cfModsJSON = `{"data":[
 {"id":1,"name":"Just Enough Items (JEI)","classId":6,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/jei"}},
 {"id":2,"name":"Faithful 32x","classId":12,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/texture-packs/faithful-32x"}},
 {"id":3,"name":"BSL Shaders","classId":6552,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/shaders/bsl-shaders/"}},
 {"id":4,"name":"Recipe\nFix","classId":6945,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/data-packs/recipe-fix?x=1#top"}},
 {"id":5,"name":"Hybrid API","classId":6,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/hapi"}},
 {"id":6,"name":"已经删掉的文件所在的项目","classId":6,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/gone"}},
 {"id":7,"name":"名字不规矩","classId":6,"links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/evil"}},
 {"id":8,"name":"主页不是 CurseForge 的","classId":12,"links":{"websiteUrl":"https://evil.example.com/minecraft/shaders/x"}},
 {"id":9,"name":"认不出类别","classId":4546,"links":{"websiteUrl":"http://www.curseforge.com/minecraft/customization/x"}}
]}`

func cfRefs() []CFRef {
	var refs []CFRef
	for n := int64(1); n <= 9; n++ {
		refs = append(refs, CFRef{ProjectID: n, FileID: 100 + n, Required: true})
	}
	return refs
}

// 清单里的文件按项目的类别进各自的文件夹；没法自动下载的带着原因、文件名、项目名和下载页。
func TestResolveCFPlacesByClassAndNamesUnresolved(t *testing.T) {
	fakeCF(t, cfFilesJSON, cfModsJSON, 200, 200)
	files, unresolved, err := ResolveCF(context.Background(), cfRefs(), "测试用的 Key")
	if err != nil {
		t.Fatalf("两次查询都成功时不该有错：%v", err)
	}
	got := map[string]File{}
	for _, f := range files {
		got[f.Path] = f
	}
	for _, want := range []string{
		"mods/jei-1.20.1.jar",            // 模组
		"resourcepacks/Faithful 32x.zip", // 主页写着 texture-packs
		"shaderpacks/BSL_v10.zip",        // 主页写着 shaders（地址末尾多一个斜杠也认）
		"datapacks/recipes_datapack.zip", // 主页写着 data-packs（带着查询串也认）
		"resourcepacks/by-class.zip",     // 主页不是 CurseForge 的，不用它；按类别编号 12 认
		"mods/unknown-kind.zip",          // 主页不是 https、类别也认不出：按模组算，和原来一样
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("该有 %s，实际：%v", want, keys(got))
		}
	}
	if len(files) != 6 {
		t.Errorf("可下载的应是 6 个：%v", keys(got))
	}
	if f := got["mods/jei-1.20.1.jar"]; f.SHA1 != "aaaa" || f.Size != 10 || len(f.URLs) != 1 {
		t.Errorf("校验值、大小、地址要照接口给的带上：%+v", f)
	}
	why := map[int64]CFRef{}
	for _, r := range unresolved {
		why[r.FileID] = r
	}
	if len(unresolved) != 3 {
		t.Fatalf("没法自动下载的应是 3 个：%+v", unresolved)
	}
	// 作者不让第三方下载：接口还是给了文件名，要留着；项目名和下载页也带上
	if r := why[105]; r.Why != CFWhyNoDist || r.FileName != "hapi-1.1.3.jar" || r.Name != "Hybrid API" || r.Dir != "mods" ||
		r.URL != "https://www.curseforge.com/minecraft/mc-mods/hapi/files/105" || !r.Required {
		t.Errorf("作者不让下载的那个：%+v", r)
	}
	// 接口里根本没有这个文件
	if r := why[106]; r.Why != CFWhyGone || r.FileName != "" || r.Name != "已经删掉的文件所在的项目" {
		t.Errorf("找不到的那个：%+v", r)
	}
	// 文件名想往外跑：不下载，也不把这个名字往后传
	if r := why[107]; r.Why != CFWhyName || r.FileName != "" {
		t.Errorf("名字不规矩的那个：%+v", r)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "..") || strings.Contains(f.Path, "instance.json") {
			t.Errorf("不该有往外跑的落点：%s", f.Path)
		}
	}
}

// 项目信息查不到（接口报错）不耽误下载：文件都按模组放，和原来一样；err 里说一声。
func TestResolveCFDegradesWithoutProjectInfo(t *testing.T) {
	fakeCF(t, cfFilesJSON, `{"error":"boom"}`, 200, 500)
	files, unresolved, err := ResolveCF(context.Background(), cfRefs(), "测试用的 Key")
	if err == nil || !strings.Contains(err.Error(), "按模组放进了 mods") {
		t.Errorf("类别没查到要在 err 里说明：%v", err)
	}
	if len(files) != 6 || len(unresolved) != 3 {
		t.Fatalf("下载不该受影响：%d 个可下载，%d 个不行", len(files), len(unresolved))
	}
	for _, f := range files {
		if !strings.HasPrefix(f.Path, "mods/") {
			t.Errorf("类别没查到时都按模组放：%s", f.Path)
		}
	}
	for _, r := range unresolved {
		if r.Name != "" || r.URL != "" || r.Dir != "mods" {
			t.Errorf("没查到项目信息时名字和下载页是空的：%+v", r)
		}
		if r.FileID == 105 && (r.Why != CFWhyNoDist || r.FileName != "hapi-1.1.3.jar") {
			t.Errorf("文件名来自查文件的那一次，不受影响：%+v", r)
		}
	}
}

// 没填 Key、Key 不对：全部进待处理，各自带着原因。
func TestResolveCFWithoutKeyOrWithBadKey(t *testing.T) {
	fakeCF(t, cfFilesJSON, cfModsJSON, 200, 200)
	files, unresolved, err := ResolveCF(context.Background(), cfRefs(), "")
	if err == nil || !strings.Contains(err.Error(), "API Key") || len(files) != 0 || len(unresolved) != 9 {
		t.Fatalf("没填 Key：%v，%d 个可下载，%d 个不行", err, len(files), len(unresolved))
	}
	for _, r := range unresolved {
		if r.Why != CFWhyNoKey || r.Dir != "mods" || r.ProjectID == 0 || r.FileID == 0 {
			t.Errorf("没填 Key 的原因要写明，编号要留着：%+v", r)
		}
	}
	files, unresolved, err = ResolveCF(context.Background(), cfRefs(), "不对的 Key")
	if err == nil || len(files) != 0 || len(unresolved) != 9 || unresolved[0].Why != CFWhyAPI {
		t.Fatalf("Key 不对：%v，%d 个可下载，%+v", err, len(files), unresolved)
	}
}

func TestCFSiteAndNameCleaning(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.curseforge.com/minecraft/mc-mods/jei":         "https://www.curseforge.com/minecraft/mc-mods/jei",
		"https://www.curseforge.com/minecraft/mc-mods/jei/?a=1#b":  "https://www.curseforge.com/minecraft/mc-mods/jei",
		"https://curseforge.com/minecraft/shaders/x":               "https://curseforge.com/minecraft/shaders/x",
		"http://www.curseforge.com/minecraft/mc-mods/jei":          "", // 不是 https
		"https://www.curseforge.com.evil.example/minecraft/x":      "", // 冒充的主机名
		"https://user:pw@www.curseforge.com/minecraft/mc-mods/jei": "", // 带着账号
		"javascript:alert(1)":                                      "",
		"":                                                         "",
	} {
		if got := cfSite(raw); got != want {
			t.Errorf("cfSite(%q) = %q，应为 %q", raw, got, want)
		}
	}
	if got := cleanName("  Recipe\r\n\tFix\x00  "); got != "Recipe Fix" {
		t.Errorf("项目名里的换行和控制字符要去掉：%q", got)
	}
	if got := []rune(cleanName(strings.Repeat("长", 200))); len(got) != 81 {
		t.Errorf("太长的项目名要截断：%d 个字", len(got))
	}
}

// 接口给的文件名只认单独一段、在 Windows 上原样写得出来的：绕出去的、只有点的、末尾带点或空格的、
// 带换行的，都不下载，也不拿这个名字往后传。
func TestResolveCFRejectsBadFileNames(t *testing.T) {
	bad := []string{
		"../server.properties", // 化简以后落到实例根目录
		"..\\ops.json",
		"sub/a.jar",
		".",
		"..",
		"...",
		"a.jar.",
		"a.jar ",
		"NUL:",
		"NUL", // Windows 的保留设备名
		"con",
		"con.jar", // 带扩展名的设备名：Windows 11 上能建，老一些的 Windows 上是设备，一律不收（C-2e）
		"COM1.jar",
		"lpt².zip",
		"a\nb.jar",
		"what?.jar",
		"",
	}
	type cfFile struct {
		ID          int64    `json:"id"`
		FileName    string   `json:"fileName"`
		DownloadURL string   `json:"downloadUrl"`
		FileLength  int64    `json:"fileLength"`
		Hashes      []string `json:"hashes"`
	}
	var data []cfFile
	var refs []CFRef
	for n, name := range bad {
		id := int64(200 + n)
		data = append(data, cfFile{ID: id, FileName: name, DownloadURL: "https://edge.forgecdn.net/files/x.jar", FileLength: 1, Hashes: []string{}})
		refs = append(refs, CFRef{ProjectID: 1, FileID: id})
	}
	// 再放一个正常的，确认不是全都被拒
	data = append(data, cfFile{ID: 299, FileName: "Good Mod [1.20] (v2)+build'3.jar", DownloadURL: "https://edge.forgecdn.net/files/g.jar", FileLength: 1, Hashes: []string{}})
	refs = append(refs, CFRef{ProjectID: 1, FileID: 299})
	body, _ := json.Marshal(map[string]any{"data": data})
	fakeCF(t, string(body), cfModsJSON, 200, 200)

	got, unresolved, err := ResolveCF(context.Background(), refs, "测试用的 Key")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "mods/Good Mod [1.20] (v2)+build'3.jar" {
		t.Errorf("只有正常名字的那个该下载：%+v", got)
	}
	if len(unresolved) != len(bad) {
		t.Fatalf("不规矩的名字应有 %d 个进待处理，实际 %d 个：%+v", len(bad), len(unresolved), unresolved)
	}
	for _, r := range unresolved {
		if r.Why != CFWhyName || r.FileName != "" {
			t.Errorf("文件 %d：原因该是名字不能用，名字不往后传：%+v", r.FileID, r)
		}
	}
	// 没有引用时不发请求
	if f, u, err := ResolveCF(context.Background(), nil, "测试用的 Key"); f != nil || u != nil || err != nil {
		t.Errorf("空清单：%v %v %v", f, u, err)
	}
}

func keys(m map[string]File) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
