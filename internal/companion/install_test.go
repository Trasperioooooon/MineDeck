package companion_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"minedeck/internal/companion"
	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/events"
)

// ---------- 假的实例 ----------

type fakeInsts struct {
	mu    sync.Mutex
	insts map[string]*companion.InstInfo
	logs  []string
}

func newInsts() *fakeInsts { return &fakeInsts{insts: map[string]*companion.InstInfo{}} }

func (f *fakeInsts) add(t *testing.T, name, core, mc, build string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.insts[name] = &companion.InstInfo{Name: name, Dir: dir, Core: core, MC: mc, Build: build, Status: "stopped"}
	f.mu.Unlock()
	return dir
}

func (f *fakeInsts) setStatus(name, st string) {
	f.mu.Lock()
	f.insts[name].Status = st
	f.mu.Unlock()
}

func (f *fakeInsts) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.insts {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func (f *fakeInsts) Info(name string) (companion.InstInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.insts[name]
	if i == nil {
		return companion.InstInfo{}, fmt.Errorf("实例不存在: %s", name)
	}
	return *i, nil
}

func (f *fakeInsts) Log(_, line string) {
	f.mu.Lock()
	f.logs = append(f.logs, line)
	f.mu.Unlock()
}

// ---------- 假的 Modrinth（API 和 CDN）----------

type fakeModrinth struct {
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte           // 路径 → 内容
	versions map[string][]map[string]any // 项目 ID → 版本列表
	hits     []string                    // 原本要访问的「主机/路径」
}

func newModrinth(t *testing.T) *fakeModrinth {
	f := &fakeModrinth{files: map[string][]byte{}, versions: map[string][]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/v2/project/") && strings.HasSuffix(r.URL.Path, "/version") {
			pid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/project/"), "/version")
			var loaders, games []string
			_ = json.Unmarshal([]byte(r.URL.Query().Get("loaders")), &loaders)
			_ = json.Unmarshal([]byte(r.URL.Query().Get("game_versions")), &games)
			if len(loaders) == 0 || len(games) == 0 {
				http.Error(w, "缺 loaders、game_versions", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(f.versions[pid])
			return
		}
		b, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// client 把所有请求转给假服务，记下原本要去的地址（测试里不访问真实网络）。
func (f *fakeModrinth) client() *http.Client {
	target, _ := url.Parse(f.srv.URL)
	return &http.Client{Transport: rt(func(req *http.Request) (*http.Response, error) {
		f.mu.Lock()
		f.hits = append(f.hits, req.URL.Host+req.URL.Path)
		f.mu.Unlock()
		r := req.Clone(req.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (f *fakeModrinth) serve(rawURL string, b []byte) {
	u, _ := url.Parse(rawURL)
	f.mu.Lock()
	f.files[u.Path] = b
	f.mu.Unlock()
}

func (f *fakeModrinth) hitList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// modJar 一个只有元数据的模组 jar。
func modJar(t *testing.T, modID, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("META-INF/neoforge.mods.toml")
	fmt.Fprintf(w, "modLoader=\"javafml\"\nloaderVersion=\"[1,)\"\nlicense=\"LGPL-3.0\"\n[[mods]]\nmodId=\"%s\"\nversion=\"%s\"\ndisplayName=\"%s\"\n", modID, version, modID)
	p, _ := zw.Create("pad.txt")
	fmt.Fprintf(p, "%s %s", modID, version)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha512hex(b []byte) string { s := sha512.Sum512(b); return hex.EncodeToString(s[:]) }

// testManifest 内置的清单，只把 NeoForge 1.21.1 的两个依赖的实测文件换成测试里造的 jar（地址不变，由假 Modrinth 提供）。
type depJars struct{ pe, arch []byte }

func testManifest(t *testing.T) (*companionjar.Manifest, depJars) {
	t.Helper()
	m, err := companionjar.Load()
	if err != nil {
		t.Fatal(err)
	}
	cp := *m
	cp.Jars = slices.Clone(m.Jars)
	jars := depJars{pe: modJar(t, "playerengine", "1.21.1-1.4.0"), arch: modJar(t, "architectury", "13.0.11")}
	for i := range cp.Jars {
		if cp.Jars[i].Loader != companionjar.LoaderNeoForge {
			continue
		}
		cp.Jars[i].Dependencies = slices.Clone(cp.Jars[i].Dependencies)
		for k := range cp.Jars[i].Dependencies {
			d := &cp.Jars[i].Dependencies[k]
			b := jars.pe
			if d.ModID == "architectury" {
				b = jars.arch
			}
			d.Modrinth.Tested.Size, d.Modrinth.Tested.SHA512 = int64(len(b)), sha512hex(b)
		}
	}
	return &cp, jars
}

func depOf(m *companionjar.Manifest, id string) companionjar.Dependency {
	j, _ := m.Find(companionjar.LoaderNeoForge, "1.21.1")
	for _, d := range j.Dependencies {
		if d.ModID == id {
			return d
		}
	}
	panic(id)
}

type installEnv struct {
	s    *companion.Service
	in   *fakeInsts
	mr   *fakeModrinth
	man  *companionjar.Manifest
	jars depJars
	dir  string
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	e := &installEnv{in: newInsts(), mr: newModrinth(t)}
	e.man, e.jars = testManifest(t)
	e.dir = e.in.add(t, "模组服", "neoforge", "1.21.1", "21.1.265")
	e.s = companion.New(companion.Deps{Instances: e.in, HTTP: e.mr.client(), Manifest: e.man, Subscribe: func(func(events.Event)) {}})
	return e
}

func (e *installEnv) serveTested() {
	e.mr.serve(depOf(e.man, "playerengine").Modrinth.Tested.URL, e.jars.pe)
	e.mr.serve(depOf(e.man, "architectury").Modrinth.Tested.URL, e.jars.arch)
}

func (e *installEnv) put(t *testing.T, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, "mods", name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func modsList(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, "mods"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func states(v companion.InstallView) string {
	var s []string
	for _, m := range v.Mods {
		s = append(s, m.ID+"="+m.State)
	}
	return strings.Join(s, ",")
}

// 先装实测过的版本：伙伴模组从 exe 里写出来，PlayerEngine 和 Architectury 从 Modrinth 的 CDN 下载、核对大小和 sha512；
// 实测过的能下载时根本不去查 API（不追新）。装完再装一次什么都不做。好友的客户端要装什么按清单的 side 来。
func TestInstallTestedFirst(t *testing.T) {
	e := newInstallEnv(t)
	e.serveTested()
	v, err := e.s.Status("模组服")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Install.Supported || states(v.Install) != "minedeck_companion=missing,playerengine=missing,architectury=missing" || !v.Install.Needed ||
		v.Install.Ready || v.Install.Download != int64(len(e.jars.pe)+len(e.jars.arch)) || v.Install.Java != 21 {
		t.Fatalf("装之前：%+v", v.Install)
	}
	// 依赖的客户端要不要装看清单的 side（0.2.0 起两个依赖都只装服务端，好友只装伙伴模组）
	pe, arch := depOf(e.man, "playerengine"), depOf(e.man, "architectury")
	if len(v.Client) != 3 || !v.Client[0].Required || v.Client[0].Page != "" ||
		v.Client[1].Required != (pe.Side == companionjar.SideBoth) || v.Client[2].Required != (arch.Side == companionjar.SideBoth) ||
		v.Client[1].Page != "https://modrinth.com/mod/playerengine/version/9cN9nlTk" || v.Client[2].Version != "13.0.11" {
		t.Errorf("好友要装的：%+v", v.Client)
	}
	p, err := e.s.PreviewInstall("模组服")
	if err != nil || len(p.Write) != 1 || len(p.Download) != 2 || len(p.Remove) != 0 || p.Running || p.Noop {
		t.Fatalf("预览：%+v %v", p, err)
	}
	if len(e.mr.hitList()) != 0 {
		t.Fatalf("预览不该联网：%v", e.mr.hitList())
	}
	res, err := e.s.Install(context.Background(), "模组服")
	if err != nil {
		t.Fatal(err)
	}
	j, _ := e.man.Find("neoforge", "1.21.1")
	if !slices.Equal(res.Installed, []string{j.InstallName, "playerengine-neoforge-1.21.1-1.4.0.jar", "architectury-13.0.11-neoforge.jar"}) ||
		len(res.Removed) != 0 || res.Restart || len(res.Fallback) != 0 || !res.Install.Ready {
		t.Errorf("装完：%+v", res)
	}
	ours, _ := j.Bytes()
	for name, want := range map[string][]byte{j.InstallName: ours, "playerengine-neoforge-1.21.1-1.4.0.jar": e.jars.pe, "architectury-13.0.11-neoforge.jar": e.jars.arch} {
		if b, err := os.ReadFile(filepath.Join(e.dir, "mods", name)); err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s 的内容不对：%v", name, err)
		}
	}
	if got := modsList(t, e.dir); len(got) != 3 {
		t.Errorf("mods 里只该有这三个（没有临时文件）：%v", got)
	}
	hits := e.mr.hitList()
	if len(hits) != 2 || !strings.HasPrefix(hits[0], "cdn.modrinth.com/data/nUOXthgN/versions/9cN9nlTk/") ||
		!strings.HasPrefix(hits[1], "cdn.modrinth.com/data/lhGA9TYQ/versions/1IiqEQGl/") {
		t.Errorf("只该下载两个实测过的文件，不查 API：%v", hits)
	}
	v, _ = e.s.Status("模组服")
	if states(v.Install) != "minedeck_companion=ok,playerengine=ok,architectury=ok" || !v.Install.Ready || v.Install.Needed {
		t.Errorf("装完以后：%+v", v.Install)
	}
	if p, err := e.s.PreviewInstall("模组服"); err != nil || !p.Noop {
		t.Errorf("都装好了：%+v %v", p, err)
	}
	if res, err := e.s.Install(context.Background(), "模组服"); err != nil || len(res.Installed) != 0 || len(e.mr.hitList()) != 2 {
		t.Errorf("再装一次什么都不做：%+v %v", res, err)
	}

	// 清单里依赖的 side 改成 server（以后客户端不用装了）：界面照清单显示
	for i := range e.man.Jars {
		for k := range e.man.Jars[i].Dependencies {
			e.man.Jars[i].Dependencies[k].Side = companionjar.SideServer
		}
	}
	v, _ = e.s.Status("模组服")
	if v.Client[1].Required || v.Client[2].Required || !v.Client[0].Required {
		t.Errorf("side=server 的依赖好友不用装：%+v", v.Client)
	}
}

// 下载下来的大小或 sha512 对不上：不装，不留下临时文件；不会因此去 Modrinth 上找别的版本。
func TestInstallVerifiesSHA512(t *testing.T) {
	e := newInstallEnv(t)
	e.serveTested()
	bad := bytes.Clone(e.jars.pe)
	bad[len(bad)-1] ^= 1 // 大小一样、内容不一样
	e.mr.serve(depOf(e.man, "playerengine").Modrinth.Tested.URL, bad)
	_, err := e.s.Install(context.Background(), "模组服")
	var ce *companion.Error
	if !errors.As(err, &ce) || ce.Code != 502 || !strings.Contains(err.Error(), "sha512 对不上") {
		t.Fatalf("sha512 对不上应报错：%v", err)
	}
	for _, f := range modsList(t, e.dir) {
		if strings.Contains(f, "playerengine") || strings.HasSuffix(f, ".part") {
			t.Errorf("不该留下 %s", f)
		}
	}
	for _, h := range e.mr.hitList() {
		if strings.HasPrefix(h, "api.modrinth.com") {
			t.Errorf("校验失败不该回退去查 API：%v", e.mr.hitList())
		}
	}
	e.mr.serve(depOf(e.man, "playerengine").Modrinth.Tested.URL, e.jars.pe[:len(e.jars.pe)-10])
	if _, err := e.s.Install(context.Background(), "模组服"); err == nil || !strings.Contains(err.Error(), "大小") {
		t.Errorf("大小不对应报错：%v", err)
	}
}

func version(number, typ, date, file string, loaders []string, b []byte, fileURL string) map[string]any {
	return map[string]any{"id": "v" + number, "project_id": "nUOXthgN", "version_number": number, "version_type": typ, "date_published": date,
		"loaders": loaders, "game_versions": []string{"1.21.1"},
		"files": []any{map[string]any{"url": fileURL, "filename": file, "primary": true, "size": len(b), "hashes": map[string]any{"sha512": sha512hex(b), "sha1": "x"}}}}
}

// 实测过的版本下载不到（404，作者删了）：才去 Modrinth 上找 release、version_number 匹配规则、发布最晚的那个，
// 下载后核对 Modrinth 给的 sha512，再确认 mods.toml 里的版本在范围内。beta、不匹配的、别的加载器的都不要。
func TestInstallFallback(t *testing.T) {
	e := newInstallEnv(t)
	e.mr.serve(depOf(e.man, "architectury").Modrinth.Tested.URL, e.jars.arch) // PlayerEngine 实测的那个不提供：404
	pe143, pe142, pe145, pe150 := modJar(t, "playerengine", "1.21.1-1.4.3"), modJar(t, "playerengine", "1.21.1-1.4.2"),
		modJar(t, "playerengine", "1.21.1-1.4.5"), modJar(t, "playerengine", "1.21.1-1.5.0")
	cdn := "https://cdn.modrinth.com/data/nUOXthgN/versions/"
	e.mr.versions["nUOXthgN"] = []map[string]any{
		version("1.21.1-1.4.2-NeoForge", "release", "2026-02-01T00:00:00Z", "pe-142.jar", []string{"neoforge"}, pe142, cdn+"b/pe-142.jar"),
		version("1.21.1-1.4.3-NeoForge", "release", "2026-03-01T00:00:00.123Z", "pe-143.jar", []string{"neoforge"}, pe143, cdn+"c/pe-143.jar"),
		version("1.21.1-1.4.5-NeoForge", "beta", "2026-04-01T00:00:00Z", "pe-145.jar", []string{"neoforge"}, pe145, cdn+"d/pe-145.jar"),
		version("1.21.1-1.5.0-NeoForge", "release", "2026-05-01T00:00:00Z", "pe-150.jar", []string{"neoforge"}, pe150, cdn+"e/pe-150.jar"),
		version("1.21.1-1.4.9-NeoForge", "release", "2026-06-01T00:00:00Z", "pe-149.jar", []string{"fabric"}, pe150, cdn+"f/pe-149.jar"),
	}
	for p, b := range map[string][]byte{"b/pe-142.jar": pe142, "c/pe-143.jar": pe143, "d/pe-145.jar": pe145, "e/pe-150.jar": pe150} {
		e.mr.serve(cdn+p, b)
	}
	res, err := e.s.Install(context.Background(), "模组服")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Installed, "pe-143.jar") || len(res.Fallback) != 1 || !strings.Contains(res.Fallback[0], "1.21.1-1.4.3-NeoForge") ||
		!strings.Contains(res.Fallback[0], "没有实测过") {
		t.Errorf("回退的结果：%+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "mods", "pe-143.jar")); !bytes.Equal(b, pe143) {
		t.Error("装的不是 1.4.3")
	}
	hits := strings.Join(e.mr.hitList(), "\n")
	if !strings.Contains(hits, "api.modrinth.com/v2/project/nUOXthgN/version") || strings.Contains(hits, "pe-145") || strings.Contains(hits, "pe-150") {
		t.Errorf("访问过的：\n%s", hits)
	}
	v, _ := e.s.Status("模组服")
	if states(v.Install) != "minedeck_companion=ok,playerengine=inrange,architectury=ok" || !v.Install.Ready {
		t.Errorf("装完以后：%+v", v.Install)
	}
}

// 回退时 mods.toml 的版本不在范围内、或者 Modrinth 给的 sha512 对不上、或者一个能用的都没有：不装。
func TestInstallFallbackRejects(t *testing.T) {
	cdn := "https://cdn.modrinth.com/data/nUOXthgN/versions/"
	cases := map[string]struct {
		versions func(t *testing.T, e *installEnv) []map[string]any
		errHas   string
	}{
		"版本不在范围内": {func(t *testing.T, e *installEnv) []map[string]any {
			b := modJar(t, "playerengine", "1.21.1-1.5.0") // version_number 匹配规则，jar 里写的却是 1.5.0
			e.mr.serve(cdn+"x/pe.jar", b)
			return []map[string]any{version("1.21.1-1.4.7-NeoForge", "release", "2026-03-01T00:00:00Z", "pe.jar", []string{"neoforge"}, b, cdn+"x/pe.jar")}
		}, "不是范围"},
		"模组 ID 不对": {func(t *testing.T, e *installEnv) []map[string]any {
			b := modJar(t, "notpe", "1.21.1-1.4.7")
			e.mr.serve(cdn+"x/pe.jar", b)
			return []map[string]any{version("1.21.1-1.4.7-NeoForge", "release", "2026-03-01T00:00:00Z", "pe.jar", []string{"neoforge"}, b, cdn+"x/pe.jar")}
		}, "模组 ID"},
		"sha512 对不上": {func(t *testing.T, e *installEnv) []map[string]any {
			b := modJar(t, "playerengine", "1.21.1-1.4.7")
			v := version("1.21.1-1.4.7-NeoForge", "release", "2026-03-01T00:00:00Z", "pe.jar", []string{"neoforge"}, b, cdn+"x/pe.jar")
			e.mr.serve(cdn+"x/pe.jar", append(bytes.Clone(b[:len(b)-1]), b[len(b)-1]^1))
			return []map[string]any{v}
		}, "sha512"},
		"不在 Modrinth 的 CDN 上": {func(t *testing.T, e *installEnv) []map[string]any {
			b := modJar(t, "playerengine", "1.21.1-1.4.7")
			return []map[string]any{version("1.21.1-1.4.7-NeoForge", "release", "2026-03-01T00:00:00Z", "pe.jar", []string{"neoforge"}, b, "https://evil.example/pe.jar")}
		}, "文件信息不对"},
		"一个能用的都没有": {func(t *testing.T, e *installEnv) []map[string]any {
			b := modJar(t, "playerengine", "1.21.1-1.6.0")
			return []map[string]any{version("1.21.1-1.6.0-NeoForge", "release", "2026-03-01T00:00:00Z", "pe.jar", []string{"neoforge"}, b, cdn+"x/pe.jar")}
		}, "没有符合"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newInstallEnv(t)
			e.mr.serve(depOf(e.man, "architectury").Modrinth.Tested.URL, e.jars.arch)
			e.mr.versions["nUOXthgN"] = c.versions(t, e)
			_, err := e.s.Install(context.Background(), "模组服")
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Fatalf("应报错（%s）：%v", c.errHas, err)
			}
			for _, f := range modsList(t, e.dir) {
				if strings.Contains(f, "pe") && !strings.Contains(f, "companion") {
					t.Errorf("不该装上 %s", f)
				}
			}
		})
	}
}

// 不自动追新：已经装着、版本在范围内的依赖不动（哪怕不是实测过的那个），不联网。
func TestNoAutoUpgrade(t *testing.T) {
	e := newInstallEnv(t)
	e.put(t, "playerengine-1.4.1.jar", modJar(t, "playerengine", "1.21.1-1.4.1"))
	e.put(t, "architectury-13.0.12.jar", modJar(t, "architectury", "13.0.12"))
	v, _ := e.s.Status("模组服")
	if states(v.Install) != "minedeck_companion=missing,playerengine=inrange,architectury=inrange" || v.Install.Download != 0 {
		t.Fatalf("状态：%+v", v.Install)
	}
	res, err := e.s.Install(context.Background(), "模组服")
	if err != nil || len(res.Installed) != 1 || len(res.Removed) != 0 {
		t.Fatalf("只该装伙伴模组：%+v %v", res, err)
	}
	if h := e.mr.hitList(); len(h) != 0 {
		t.Errorf("依赖都在范围内，不该联网：%v", h)
	}
	if got := modsList(t, e.dir); !slices.Contains(got, "playerengine-1.4.1.jar") || !slices.Contains(got, "architectury-13.0.12.jar") {
		t.Errorf("范围内的依赖应留着：%v", got)
	}
}

// 换掉旧版本：我们的模组按文件名前缀和模组 ID 找（改过名的也认），依赖按模组 ID 找、不在范围内的换掉，
// 同一个依赖启用了两份只留一份；停用的不动。服务器运行中要删旧文件时不装（文件被占用），只是新加文件的可以装、重启后生效。
func TestInstallReplacesOld(t *testing.T) {
	e := newInstallEnv(t)
	e.serveTested()
	e.put(t, "minedeck-companion-neoforge-1.21.1-0.0.9.jar", modJar(t, "minedeck_companion", "0.0.9"))
	e.put(t, "AI伙伴.jar", modJar(t, "minedeck_companion", "0.0.8"))
	e.put(t, "minedeck-companion-neoforge-1.21.1-0.0.7.jar.disabled", modJar(t, "minedeck_companion", "0.0.7"))
	e.put(t, "playerengine-old.jar", modJar(t, "playerengine", "1.21.1-1.3.0"))
	e.put(t, "architectury-13.0.11-neoforge.jar", e.jars.arch)
	e.put(t, "architectury-copy.jar", modJar(t, "architectury", "13.0.9"))
	v, _ := e.s.Status("模组服")
	if states(v.Install) != "minedeck_companion=outdated,playerengine=outdated,architectury=ok" {
		t.Fatalf("状态：%+v", v.Install)
	}
	e.in.setStatus("模组服", "running")
	if _, err := e.s.PreviewInstall("模组服"); !errors.Is(err, companion.ErrRunningReplace) {
		t.Fatalf("运行中要替换旧版本应报错：%v", err)
	}
	e.in.setStatus("模组服", "stopped")
	res, err := e.s.Install(context.Background(), "模组服")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(res.Removed)
	if !slices.Equal(res.Removed, []string{"AI伙伴.jar", "architectury-copy.jar", "minedeck-companion-neoforge-1.21.1-0.0.9.jar", "playerengine-old.jar"}) {
		t.Errorf("删掉的：%v", res.Removed)
	}
	j, _ := e.man.Find("neoforge", "1.21.1")
	got := modsList(t, e.dir)
	slices.Sort(got)
	want := []string{"architectury-13.0.11-neoforge.jar", j.InstallName, "minedeck-companion-neoforge-1.21.1-0.0.7.jar.disabled", "playerengine-neoforge-1.21.1-1.4.0.jar"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("mods：%v", got)
	}
	if h := e.mr.hitList(); len(h) != 1 {
		t.Errorf("Architectury 已经是实测过的版本，只该下载 PlayerEngine：%v", h)
	}

	// 运行中、只是新加文件：可以装，重启后生效
	e2 := newInstallEnv(t)
	e2.serveTested()
	e2.in.setStatus("模组服", "running")
	res, err = e2.s.Install(context.Background(), "模组服")
	if err != nil || !res.Restart || len(res.Installed) != 3 {
		t.Fatalf("运行中装：%+v %v", res, err)
	}
	if v, _ := e2.s.Status("模组服"); !v.Restart {
		t.Error("应提示重启后生效")
	}
}

// 不支持的加载器和 MC 版本：说清楚支持哪些（按清单列）；加载器版本不在范围内时提醒。
func TestInstallUnsupported(t *testing.T) {
	e := newInstallEnv(t)
	for _, c := range []struct{ core, mc string }{{"fabric", "1.21.1"}, {"forge", "1.21.1"}, {"neoforge", "1.20.1"}, {"paper", "1.21.4"}, {"mohist", "1.20.1"}, {"vanilla", "1.21.1"}} {
		name := c.core + c.mc
		e.in.add(t, name, c.core, c.mc, "")
		_, err := e.s.PreviewInstall(name)
		var ce *companion.Error
		if !errors.As(err, &ce) || ce.Code != 400 || !strings.Contains(err.Error(), "只支持 NeoForge 1.21.1、Forge 1.20.1") {
			t.Errorf("%s：%v", name, err)
		}
		v, err := e.s.Status(name)
		if err != nil || v.Install.Supported || v.Install.Reason == "" || len(v.Client) != 0 {
			t.Errorf("%s 的状态：%+v %v", name, v, err)
		}
		if _, err := e.s.SetSettings(name, companion.SettingsPatch{Enabled: ptr(true)}); err == nil {
			t.Errorf("%s 不该能打开伙伴", name)
		}
	}
	e.in.add(t, "旧加载器", "neoforge", "1.21.1", "21.0.167")
	if v, _ := e.s.Status("旧加载器"); !v.Install.Supported || !strings.Contains(v.Install.LoaderWarn, "21.0.167") {
		t.Errorf("加载器版本不在范围内：%+v", v.Install)
	}
	if _, err := e.s.Status("没有这个"); err == nil {
		t.Error("实例不存在应报错")
	}
}
