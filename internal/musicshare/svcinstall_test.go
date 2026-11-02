package musicshare

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"minedeck/internal/bridge"
	"minedeck/internal/mcsrc"
)

// ---- 假的 Modrinth：版本列表和文件都由本机的 httptest 提供，测试里不联网 ----

type fakeMR struct {
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte // 路径 → 内容
	versions []map[string]any
	queries  []string // 收到的版本查询（loaders、game_versions）
}

func newFakeMR(t *testing.T) *fakeMR {
	f := &fakeMR{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/v2/project/"+svcProject+"/version" {
			f.queries = append(f.queries, r.URL.Query().Get("loaders")+" "+r.URL.Query().Get("game_versions"))
			_ = json.NewEncoder(w).Encode(f.versions)
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

// version 加一个版本，文件内容是 jar。
func (f *fakeMR) version(number, typ, date string, loaders, games []string, fname string, jar []byte) {
	sum := sha1.Sum(jar)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files["/files/"+fname] = jar
	f.versions = append(f.versions, map[string]any{
		"version_number": number, "version_type": typ, "date_published": date, "loaders": loaders, "game_versions": games,
		"files": []map[string]any{{"url": f.srv.URL + "/files/" + fname, "filename": fname, "primary": true, "size": len(jar),
			"hashes": map[string]string{"sha1": hex.EncodeToString(sum[:])}}},
	})
}

// modJarBytes 一个只有元数据的模组 jar（toml 是 META-INF/neoforge.mods.toml 或 META-INF/mods.toml），带点填充让它大于 1 KB。
func modJarBytes(t *testing.T, toml, modID, version string) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.jar")
	var pad strings.Builder // 压不小的填充（重复的文字一压就没了，凑不够 1 KB）
	for i := 0; i < 40; i++ {
		s := sha1.Sum([]byte(modID + version + string(rune('a'+i))))
		pad.WriteString(hex.EncodeToString(s[:]))
	}
	writeZip(t, p, map[string]string{
		toml:      "modLoader = \"javafml\"\n[[mods]]\nmodId = \"" + modID + "\"\nversion = \"" + version + "\"\n",
		"pad.txt": pad.String(),
	})
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) useModrinth(f *fakeMR) { e.s.d.ModrinthAPI = f.srv.URL + "/v2" }

func modsFiles(t *testing.T, dir string) []string {
	ents, _ := os.ReadDir(filepath.Join(dir, "mods"))
	var out []string
	for _, x := range ents {
		out = append(out, x.Name())
	}
	return out
}

// 按加载器和 MC 版本查，取最新的正式版（不取更新的测试版、别的加载器的版本），核对后装进 mods；再装报「已经有」。
func TestInstallSVC(t *testing.T) {
	e := newEnv(t)
	mr := newFakeMR(t)
	e.useModrinth(mr)
	nf, games := []string{"neoforge"}, []string{"1.21.1"}
	mr.version("neoforge-1.21.1-2.6.20", "release", "2026-08-01T10:00:00Z", nf, games, "voicechat-neoforge-1.21.1-2.6.20.jar",
		modJarBytes(t, "META-INF/neoforge.mods.toml", "voicechat", "1.21.1-2.6.20"))
	mr.version("neoforge-1.21.1-2.6.30-beta", "beta", "2026-10-20T10:00:00Z", nf, games, "voicechat-neoforge-1.21.1-2.6.30-beta.jar",
		modJarBytes(t, "META-INF/neoforge.mods.toml", "voicechat", "1.21.1-2.6.30-beta"))
	mr.version("neoforge-1.21.1-2.6.27", "release", "2026-10-01T10:00:00Z", nf, games, "voicechat-neoforge-1.21.1-2.6.27.jar",
		modJarBytes(t, "META-INF/neoforge.mods.toml", "voicechat", "1.21.1-2.6.27"))
	mr.version("fabric-1.21.1-2.6.28", "release", "2026-10-10T10:00:00Z", []string{"fabric"}, games, "voicechat-fabric-1.21.1-2.6.28.jar",
		modJarBytes(t, "META-INF/neoforge.mods.toml", "voicechat", "1.21.1-2.6.28"))

	pv, err := e.s.PreviewSVC(instName)
	if err != nil || pv.Loader != "neoforge" || pv.MC != "1.21.1" || pv.Running {
		t.Fatalf("预览：%+v %v", pv, err)
	}
	res, err := e.s.InstallSVC(context.Background(), instName)
	if err != nil {
		t.Fatal(err)
	}
	if res.File != "voicechat-neoforge-1.21.1-2.6.27.jar" || res.Version != "1.21.1-2.6.27" || res.Name != "neoforge-1.21.1-2.6.27" || res.Type != "" || res.Restart {
		t.Fatalf("安装结果：%+v", res)
	}
	if q := mr.queries; len(q) != 1 || q[0] != `["neoforge"] ["1.21.1"]` {
		t.Fatalf("查询：%q", q)
	}
	if got := modsFiles(t, e.dir); !slices.Equal(got, []string{res.File}) {
		t.Fatalf("mods 里：%q（不该留下临时文件）", got)
	}
	st := e.status()
	if !st.SVC.Installed || st.SVC.Version != "1.21.1-2.6.27" || len(st.Restart) != 0 {
		t.Fatalf("装好后的状态：%+v %v", st.SVC, st.Restart)
	}
	if _, err := e.s.InstallSVC(context.Background(), instName); codeOf(err) != 400 || !strings.Contains(err.Error(), "已经有") {
		t.Fatalf("再装一次：%v", err)
	}
	if _, err := e.s.PreviewSVC(instName); codeOf(err) != 400 {
		t.Fatalf("装好后预览：%v", err)
	}
}

// 服务器运行中装的：记下「重启后生效」（检查清单顶上提示、不能开始广播），控制台写一行；开服前清掉。
// Forge 服按 forge 查。
func TestInstallSVCWhileRunning(t *testing.T) {
	e := newEnv(t)
	mr := newFakeMR(t)
	e.useModrinth(mr)
	dir := t.TempDir()
	e.inst.add(InstInfo{Name: "模组服", Dir: dir, Core: mcsrc.CoreForge, MC: "1.20.1", Status: "running"})
	mr.version("forge-1.20.1-2.6.27", "release", "2026-10-01T10:00:00Z", []string{"forge"}, []string{"1.20.1"}, "voicechat-forge-1.20.1-2.6.27.jar",
		modJarBytes(t, "META-INF/mods.toml", "voicechat", "1.20.1-2.6.27"))
	res, err := e.s.InstallSVC(context.Background(), "模组服")
	if err != nil || !res.Restart || res.File != "voicechat-forge-1.20.1-2.6.27.jar" {
		t.Fatalf("运行中安装：%+v %v", res, err)
	}
	if q := mr.queries; len(q) != 1 || q[0] != `["forge"] ["1.20.1"]` {
		t.Fatalf("Forge 服的查询：%q", q)
	}
	st, err := e.s.Status("模组服")
	if err != nil || !slices.Equal(st.Restart, []string{"Simple Voice Chat"}) {
		t.Fatalf("要重启的改动：%+v %v", st, err)
	}
	if con, _ := e.inst.Console("模组服"); !strings.Contains(strings.Join(con.Lines(), "\n"), "重启服务器后生效") {
		t.Fatalf("控制台：%q", con.Lines())
	}
	if _, err := e.s.InstallBridge("模组服", bridge.VariantMain); err != nil { // 装好 SVC 就能装桥接模组了
		t.Fatal(err)
	}
	if _, err := e.s.Start(StartReq{Inst: "模组服", Mode: "process", PID: 4242}); codeOf(err) != 400 || !strings.Contains(err.Error(), "Simple Voice Chat 装好以后还没重启") {
		t.Fatalf("SVC 装好没重启时不能开始：%v", err)
	}
	e.s.BeforeStart("模组服")
	if st, _ := e.s.Status("模组服"); len(st.Restart) != 0 {
		t.Fatalf("开服前应清掉：%v", st.Restart)
	}
}

// 下载的不是 SVC、加载器不对、Modrinth 上没有这个版本、核心不支持：都不装，mods 里不留东西。
func TestInstallSVCRejects(t *testing.T) {
	e := newEnv(t)
	mr := newFakeMR(t)
	e.useModrinth(mr)
	nf, games := []string{"neoforge"}, []string{"1.21.1"}

	if _, err := e.s.InstallSVC(context.Background(), instName); codeOf(err) != 404 || !strings.Contains(err.Error(), "没有 neoforge 1.21.1 版") {
		t.Fatalf("没有版本：%v", err)
	}
	mr.version("neoforge-1.21.1-1.0", "release", "2026-10-01T10:00:00Z", nf, games, "talkingheads-1.0.jar",
		modJarBytes(t, "META-INF/neoforge.mods.toml", "talkingheads", "1.0"))
	if _, err := e.s.InstallSVC(context.Background(), instName); codeOf(err) != 502 || !strings.Contains(err.Error(), "没有 Simple Voice Chat") {
		t.Fatalf("不是 SVC：%v", err)
	}
	if got := modsFiles(t, e.dir); len(got) != 0 {
		t.Fatalf("不是 SVC 时 mods 里留下了：%q", got)
	}
	mr.versions = nil
	mr.version("neoforge-1.21.1-2.6.27", "release", "2026-10-02T10:00:00Z", nf, games, "voicechat-forge-1.21.1-2.6.27.jar",
		modJarBytes(t, "META-INF/mods.toml", "voicechat", "1.21.1-2.6.27"))
	if _, err := e.s.InstallSVC(context.Background(), instName); codeOf(err) != 502 || !strings.Contains(err.Error(), "是给 forge 的") {
		t.Fatalf("加载器不对：%v", err)
	}
	if got := modsFiles(t, e.dir); len(got) != 0 {
		t.Fatalf("加载器不对时 mods 里留下了：%q", got)
	}
	e.inst.add(InstInfo{Name: "插件服", Dir: t.TempDir(), Core: mcsrc.CorePaper, MC: "1.21.1"})
	if _, err := e.s.InstallSVC(context.Background(), "插件服"); codeOf(err) != 400 {
		t.Fatalf("Paper：%v", err)
	}
	if _, err := e.s.InstallSVC(context.Background(), "没有这个服"); codeOf(err) != 404 {
		t.Fatalf("实例不存在：%v", err)
	}
}

// 挑版本：正式版优先，同一种里取发布最晚的；加载器、MC 版本不对或没有文件的不要。
func TestPickSVC(t *testing.T) {
	f := []mrFile{{Filename: "a.jar"}}
	vs := []mrVersion{
		{VersionNumber: "b", VersionType: "beta", DatePublished: "2026-10-03T00:00:00Z", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}, Files: f},
		{VersionNumber: "r1", VersionType: "release", DatePublished: "2026-10-01T00:00:00Z", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}, Files: f},
		{VersionNumber: "r2", VersionType: "release", DatePublished: "2026-10-02T00:00:00Z", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}, Files: f},
		{VersionNumber: "nofile", VersionType: "release", DatePublished: "2026-10-05T00:00:00Z", Loaders: []string{"forge"}, GameVersions: []string{"1.20.1"}},
		{VersionNumber: "other", VersionType: "release", DatePublished: "2026-10-05T00:00:00Z", Loaders: []string{"forge"}, GameVersions: []string{"1.20.4"}, Files: f},
	}
	if v, ok := pickSVC(vs, "forge", "1.20.1"); !ok || v.VersionNumber != "r2" {
		t.Fatalf("应取 r2：%+v %v", v, ok)
	}
	if v, ok := pickSVC(vs[:1], "forge", "1.20.1"); !ok || v.VersionNumber != "b" {
		t.Fatalf("只有测试版时取测试版：%+v %v", v, ok)
	}
	if _, ok := pickSVC(vs, "neoforge", "1.20.1"); ok {
		t.Fatal("加载器不对不该选中")
	}
}
