package modcart

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// 假的 Modrinth：字段名和形状照 2026-11 的真实返回（见 modrinth.go 开头）。
//
//	alpha (aaa)  必装 fabric-api、内嵌 embed、可选 opt1、和 bad 不兼容
//	beta  (bbb)  必装 lib1（只写了版本 ID）；lib1 → lib2 → lib3 → lib4 一层层必装，lib2 还要 beta（绕回购物车）
//	client       只在客户端用
//	old          只有 1.19.2 的版本
//	rank         有一个更新的 beta 和两个正式版
type fakeMR struct {
	srv      *httptest.Server
	projects map[string]mrProject
	versions map[string][]mrVersion // 项目 ID → 版本
	files    map[string][]byte      // 文件名 → 内容
	mu       sync.Mutex
	hits     map[string]int // 路径 → 请求次数
	// 搜索：记下最近一次请求带的 facets / offset / limit；noFiles 为真时 /version_files 报 500（认不了本地文件）
	lastFacets, lastOffset, lastLimit string
	noFiles                           bool
}

func jarBody(name string) []byte {
	return bytes.Repeat([]byte(name+"\n"), 2048/len(name)+1) // ≥1KB：下载器的完整性下限
}

func sha1Hex(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

func newFakeMR(t *testing.T) *fakeMR {
	t.Helper()
	f := &fakeMR{projects: map[string]mrProject{}, versions: map[string][]mrVersion{}, files: map[string][]byte{}, hits: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	proj, ver := f.proj, f.ver
	req := func(pid string) mrDep { return mrDep{ProjectID: pid, DependencyType: "required"} }
	proj("aaa", "alpha", "Alpha", "required", "MIT")
	proj("bbb", "beta", "Beta", "optional", "LGPL-3.0-only")
	proj("fapi", "fabric-api", "Fabric API", "optional", "Apache-2.0")
	proj("embed", "embedded-lib", "Embedded Lib", "required", "MIT")
	proj("opt1", "opt-one", "Opt One", "optional", "MIT")
	proj("bad", "bad-mod", "Bad Mod", "required", "MIT")
	proj("lib1", "lib-one", "Lib One", "required", "MIT")
	proj("lib2", "lib-two", "Lib Two", "required", "MIT")
	proj("lib3", "lib-three", "Lib Three", "required", "MIT")
	proj("lib4", "lib-four", "Lib Four", "required", "MIT")
	proj("client", "client-only", "Client Only", "unsupported", "MIT")
	proj("old", "old-mod", "Old Mod", "required", "MIT")
	proj("rank", "rank-mod", "Rank Mod", "required", "MIT")

	ver("aaa", "va1", "1.0.0", "release", "2026-01-01T00:00:00Z", "1.20.1", req("fapi"),
		mrDep{ProjectID: "embed", DependencyType: "embedded"}, mrDep{ProjectID: "opt1", DependencyType: "optional"},
		mrDep{ProjectID: "bad", DependencyType: "incompatible"})
	ver("bbb", "vb1", "2.0.0", "release", "2026-01-01T00:00:00Z", "1.20.1", mrDep{VersionID: "v-lib1", DependencyType: "required"})
	ver("fapi", "vf1", "0.92.2+1.20.1", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("embed", "ve1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("opt1", "vo1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("bad", "vbad", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("lib1", "v-lib1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", req("lib2"))
	ver("lib2", "v-lib2", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", req("lib3"), req("bbb"))
	ver("lib3", "v-lib3", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1", req("lib4"))
	ver("lib4", "v-lib4", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("client", "vc1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("old", "vold", "1.0", "release", "2023-01-01T00:00:00Z", "1.19.2")
	ver("rank", "vr-beta", "3.0-beta", "beta", "2026-03-01T00:00:00Z", "1.20.1")
	ver("rank", "vr-1", "1.0", "release", "2026-01-01T00:00:00Z", "1.20.1")
	ver("rank", "vr-2", "2.0", "release", "2026-02-01T00:00:00Z", "1.20.1")
	return f
}

// proj 加一个模组项目。
func (f *fakeMR) proj(id, slug, title, side, license string) {
	p := mrProject{ID: id, Slug: slug, Title: title, ProjectType: "mod", ServerSide: side, IconURL: "https://cdn.modrinth.com/data/" + id + "/icon.png"}
	p.License.ID = license
	f.projects[id] = p
}

// ver 给项目加一个 fabric 版本（文件名 <项目>-<版本号>.jar）。
func (f *fakeMR) ver(pid, vid, num, typ, date, mc string, deps ...mrDep) {
	fn := pid + "-" + num + ".jar"
	body := jarBody(fn)
	f.files[fn] = body
	v := mrVersion{ID: vid, ProjectID: pid, VersionNumber: num, VersionType: typ, DatePublished: date,
		Loaders: []string{"fabric"}, GameVersions: []string{mc}, Dependencies: deps}
	mf := mrFile{URL: f.srv.URL + "/files/" + fn, Filename: fn, Primary: true, Size: int64(len(body))}
	mf.Hashes.SHA1 = sha1Hex(body)
	v.Files = []mrFile{mf}
	f.versions[pid] = append(f.versions[pid], v)
}

func (f *fakeMR) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeMR) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits[r.URL.Path]++
	f.mu.Unlock()
	if r.Header.Get("User-Agent") == "" {
		http.Error(w, "Modrinth 要求带 User-Agent", http.StatusBadRequest)
		return
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch p := r.URL.Path; {
	case p == "/projects":
		var ids []string
		if json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids) != nil {
			http.Error(w, "ids 不是 JSON 数组", http.StatusBadRequest)
			return
		}
		out := []mrProject{}
		for _, id := range ids {
			for _, pr := range f.projects {
				if pr.ID == id || pr.Slug == id {
					out = append(out, pr)
				}
			}
		}
		reply(out)
	case strings.HasPrefix(p, "/project/") && strings.HasSuffix(p, "/version"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/project/"), "/version")
		for _, pr := range f.projects {
			if pr.Slug == id {
				id = pr.ID
			}
		}
		vs, ok := f.versions[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var loaders, games []string
		_ = json.Unmarshal([]byte(r.URL.Query().Get("loaders")), &loaders)
		_ = json.Unmarshal([]byte(r.URL.Query().Get("game_versions")), &games)
		out := []mrVersion{}
		for _, v := range vs { // 同真的 Modrinth：服务端先按加载器和版本筛一遍
			if (len(loaders) == 0 || slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(loaders, l) })) &&
				(len(games) == 0 || slices.ContainsFunc(v.GameVersions, func(g string) bool { return slices.Contains(games, g) })) {
				out = append(out, v)
			}
		}
		reply(out)
	case strings.HasPrefix(p, "/version/"):
		id := strings.TrimPrefix(p, "/version/")
		for _, vs := range f.versions {
			for _, v := range vs {
				if v.ID == id {
					reply(v)
					return
				}
			}
		}
		http.NotFound(w, r)
	case p == "/version_files" && r.Method == http.MethodPost:
		f.mu.Lock()
		down := f.noFiles
		f.mu.Unlock()
		if down {
			http.Error(w, "坏了", http.StatusInternalServerError)
			return
		}
		var body struct {
			Hashes    []string `json:"hashes"`
			Algorithm string   `json:"algorithm"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		out := map[string]mrVersion{}
		for _, vs := range f.versions {
			for _, v := range vs {
				for _, fl := range v.Files {
					if body.Algorithm == "sha1" && slices.Contains(body.Hashes, fl.Hashes.SHA1) {
						out[fl.Hashes.SHA1] = v
					}
				}
			}
		}
		reply(out)
	case p == "/search":
		// 形状同真的 Modrinth：hits + total_hits，认 offset / limit；facets 里的 categories:<类别> 也认（只对带了 categories 的条目）。
		// server_side!=unsupported 这条故意不认：回来以后的兜底筛选也要测到。查询词是 bulk 时回 45 条，测翻页用。
		q := r.URL.Query()
		f.mu.Lock()
		f.lastFacets, f.lastOffset, f.lastLimit = q.Get("facets"), q.Get("offset"), q.Get("limit")
		f.mu.Unlock()
		all := []map[string]any{
			{"project_id": "aaa", "slug": "alpha", "title": "Alpha", "description": "d", "downloads": 10, "icon_url": "", "license": "MIT", "server_side": "required", "categories": []string{"fabric", "optimization"}},
			{"project_id": "client", "slug": "client-only", "title": "Client Only", "description": "d", "downloads": 5, "icon_url": "", "license": "MIT", "server_side": "unsupported", "categories": []string{"fabric", "utility"}},
		}
		if q.Get("query") == "bulk" {
			all = nil
			for i := range 45 {
				id := fmt.Sprintf("bulk%02d", i)
				all = append(all, map[string]any{"project_id": id, "slug": id, "title": id, "description": "d", "downloads": 1000 - i, "icon_url": "", "license": "MIT", "server_side": "required", "categories": []string{"fabric"}})
			}
		}
		var groups [][]string
		_ = json.Unmarshal([]byte(q.Get("facets")), &groups)
		for _, g := range groups {
			for _, cond := range g {
				if cat, ok := strings.CutPrefix(cond, "categories:"); ok && slices.Contains(AllCategories(), cat) {
					all = slices.DeleteFunc(all, func(h map[string]any) bool { return !slices.Contains(h["categories"].([]string), cat) })
				}
			}
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		page := all[min(offset, len(all)):]
		page = page[:min(limit, len(page))]
		reply(map[string]any{"hits": page, "offset": offset, "limit": limit, "total_hits": len(all)})
	case strings.HasPrefix(p, "/files/"):
		b, ok := f.files[strings.TrimPrefix(p, "/files/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

// writeJar 在 dir 里写一个只有元数据的 jar。entries：包内路径 → 内容。
func writeJar(t *testing.T, dir, name string, entries map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for p, body := range entries {
		w, _ := zw.Create(p)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
