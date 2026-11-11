package modcart

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// zipBytes 一个真的 jar（zip）：entries 是包内路径 → 内容。
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for k := range entries {
			if !yield(k) {
				return
			}
		}
	}) {
		w, _ := zw.Create(p)
		_, _ = w.Write([]byte(entries[p]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fabricJar 只有 fabric.mod.json 的 jar。depends 是「模组 ID → 版本范围」。
func fabricJar(t *testing.T, id, name string, depends map[string]string) []byte {
	t.Helper()
	meta, _ := json.Marshal(map[string]any{"id": id, "name": name, "version": "1.0.0", "depends": depends})
	return zipBytes(t, map[string]string{"fabric.mod.json": string(meta)})
}

// upload 把内容存成「网页层刚收到的临时文件」，返回路径。
func upload(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.upload")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func addLocal(t *testing.T, s *Service, inst, name string, body []byte) LocalFile {
	t.Helper()
	lf, _, err := s.AddLocal(inst, upload(t, body), name)
	if err != nil {
		t.Fatalf("添加 %s：%v", name, err)
	}
	return lf
}

func codeOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// verZip 在假 Modrinth 上登记一个版本，文件是一个真的 jar：本地文件按 SHA-1 能认出它来。
func (f *fakeMR) verZip(pid, vid, num, mc string, body []byte, deps ...mrDep) {
	fn := pid + "-" + num + ".jar"
	f.files[fn] = body
	v := mrVersion{ID: vid, ProjectID: pid, VersionNumber: num, VersionType: "release", DatePublished: "2026-01-01T00:00:00Z",
		Loaders: []string{"fabric"}, GameVersions: []string{mc}, Dependencies: deps}
	mf := mrFile{URL: f.srv.URL + "/files/" + fn, Filename: fn, Primary: true, Size: int64(len(body))}
	mf.Hashes.SHA1 = sha1Hex(body)
	v.Files = []mrFile{mf}
	f.versions[pid] = append(f.versions[pid], v)
}

// 文件名：必须是单独一段、以 .jar 结尾，不带路径、控制字符、看不见的排版字符和 Windows 不许的字符，长度有上限。
func TestCheckLocalName(t *testing.T) {
	for _, ok := range []string{"a.jar", "【优化】钠 Sodium-0.5.8+mc1.20.1.JAR", "my mod (1).jar", strings.Repeat("长", 60) + ".jar"} {
		if err := CheckLocalName(ok); err != nil {
			t.Errorf("%q 应该能用：%v", ok, err)
		}
	}
	bad := []string{"", ".", "..", "a.zip", "a.jar.disabled", "jar", "../a.jar", "a/b.jar", `a\b.jar`, `..\a.jar`, "C:a.jar", "a:b.jar",
		".hidden.jar", "a\x00.jar", "a\n.jar", "a\x7f.jar", "a\u0085.jar", "evil\u202egnp.jar", "a\u200b.jar", "a*.jar", "a?.jar", `a".jar`, "a<b>.jar", "a|b.jar",
		"NUL.jar", "com1.jar", "a.jar ", "a.jar.", strings.Repeat("a", 197) + ".jar", strings.Repeat("长", 97) + ".jar", "a\xff.jar"}
	for _, name := range bad {
		if err := CheckLocalName(name); codeOf(err) != 400 {
			t.Errorf("%q 应拒绝（400）：%v", name, err)
		}
	}
}

// 添加本地文件：读出模组信息，按内容的 SHA-1 暂存在这辆车自己的文件夹里；同一个文件不重复存，同名不同内容拒绝；
// 不是 zip 的、空的、太大的不收；重开还在；结果里没有路径。
func TestAddLocal(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, newFakeMR(t).srv.URL)
	body := fabricJar(t, "custom", "Custom Mod", map[string]string{"ghost": "*"})
	src := upload(t, body)
	lf, dup, err := s.AddLocal(Draft, src, "Custom-1.0.jar")
	if err != nil || dup {
		t.Fatalf("添加：%v dup=%v", err, dup)
	}
	if lf.SHA1 != sha1Hex(body) || lf.FileName != "Custom-1.0.jar" || lf.Size != int64(len(body)) || lf.Title != "Custom Mod" ||
		lf.ModID != "custom" || lf.Version != "1.0.0" || lf.Loader != "fabric" || lf.AddedAt.IsZero() {
		t.Fatalf("登记的：%+v", lf)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("放进清单以后临时文件应已挪走")
	}
	staged := filepath.Join(dir, "local", "draft", lf.SHA1+".jar")
	if b, err := os.ReadFile(staged); err != nil || !bytes.Equal(b, body) {
		t.Fatalf("暂存的文件不对：%v", err)
	}
	if b, _ := json.Marshal(s.Locals(Draft)); strings.Contains(string(b), "local") && strings.Contains(string(b), filepath.Base(dir)) || strings.Contains(string(b), `\\`) {
		t.Errorf("给外面的结果里不该有路径：%s", b)
	}

	// 同一个文件再加一次（换了名字也一样）：不报错、不重复存；临时文件留给调用方删
	src2 := upload(t, body)
	again, dup, err := s.AddLocal(Draft, src2, "another-name.jar")
	if err != nil || !dup || again.FileName != "Custom-1.0.jar" || len(s.Locals(Draft)) != 1 {
		t.Fatalf("同一个文件再加：%+v dup=%v %v", again, dup, err)
	}
	if _, err := os.Stat(src2); err != nil {
		t.Error("没放进去的临时文件应留在原处（调用方删）")
	}
	// 文件名相同（不分大小写）内容不同：拒绝并说清楚
	other := fabricJar(t, "custom", "Custom Mod", map[string]string{"ghost": ">=2"})
	if _, _, err := s.AddLocal(Draft, upload(t, other), "custom-1.0.JAR"); codeOf(err) != 409 || !strings.Contains(err.Error(), "内容和这个不一样") {
		t.Fatalf("同名不同内容应报 409：%v", err)
	}
	// 别的车里可以有同名文件：每辆车各存各的
	if _, _, err := s.AddLocal("生存服", upload(t, other), "Custom-1.0.jar"); err != nil {
		t.Fatalf("别的车：%v", err)
	}

	for name, c := range map[string]struct {
		body []byte
		code int
		say  string
	}{
		"not-zip.jar": {[]byte("这不是 zip"), 400, "打不开"},
		"empty.jar":   {nil, 400, "空文件"},
		"x.zip":       {body, 400, ".jar"},
	} {
		if _, _, err := s.AddLocal(Draft, upload(t, c.body), name); codeOf(err) != c.code || !strings.Contains(err.Error(), c.say) {
			t.Errorf("%s 应报 %d「%s」：%v", name, c.code, c.say, err)
		}
	}
	if _, _, err := s.AddLocal(Draft, filepath.Join(t.TempDir(), "没有这个文件"), "gone.jar"); codeOf(err) != 400 {
		t.Errorf("临时文件不存在应报 400：%v", err)
	}
	maxLocalSize = int64(len(body)) + 10
	big := zipBytes(t, map[string]string{"fabric.mod.json": `{"id":"big"}`, "pad.txt": strings.Repeat("x", 4096) + fmt.Sprint(os.Getpid())})
	_, _, err = s.AddLocal(Draft, upload(t, big), "big.jar")
	maxLocalSize = MaxLocalSize
	if codeOf(err) != 413 || !strings.Contains(err.Error(), "256 MB") {
		t.Errorf("太大的应报 413：%v", err)
	}
	// 没收下的不留下任何东西
	ents, _ := os.ReadDir(filepath.Join(dir, "local", "draft"))
	if len(ents) != 1 {
		t.Errorf("暂存目录里应只有 1 个文件：%v", ents)
	}
	// 报错里没有路径
	if _, _, err := s.AddLocal(Draft, upload(t, []byte("x")), "bad.jar"); err == nil || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), os.TempDir()) {
		t.Errorf("报错里不该有路径：%v", err)
	}

	// 读不出信息的 jar：名称用文件名，加载器是 unknown
	plain := addLocal(t, s, Draft, "plain.jar", zipBytes(t, map[string]string{"a.txt": "hi"}))
	if plain.Title != "plain.jar" || plain.Loader != "unknown" || plain.ModID != "" {
		t.Errorf("读不出信息的：%+v", plain)
	}

	// 重开：登记和文件都在；暂存文件丢了的那条不要了；没登记的文件、不属于任何车的文件夹清掉
	if err := os.Remove(filepath.Join(dir, "local", "draft", plain.SHA1+".jar")); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "local", "draft", strings.Repeat("0", 40)+".jar")
	_ = os.WriteFile(stray, []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "local", "i-deadbeef"), 0o755)
	s2 := New(dir, "")
	if got := s2.Locals(Draft); len(got) != 1 || got[0].SHA1 != lf.SHA1 {
		t.Fatalf("重开以后的草稿清单：%+v", got)
	}
	if got := s2.Locals("生存服"); len(got) != 1 {
		t.Fatalf("重开以后实例的清单：%+v", got)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("没登记的暂存文件应清掉")
	}
	if _, err := os.Stat(filepath.Join(dir, "local", "i-deadbeef")); !os.IsNotExist(err) {
		t.Error("不属于任何车的文件夹应清掉")
	}
	if _, err := os.Stat(staged); err != nil {
		t.Error("登记着的暂存文件不能被清掉")
	}
}

// 每辆车一个暂存文件夹，实例名不直接拼进路径：名字里带 ..、斜杠、冒号的实例，文件照样落在 local/ 里面。
func TestLocalBucketsStayInside(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, newFakeMR(t).srv.URL)
	root := filepath.Join(dir, "local")
	seen := map[string]bool{}
	for i, inst := range []string{Draft, "生存服", `..\..\evil`, "../../evil", "a/b", "C:\\x", "con", "x:y", strings.Repeat("长", 100)} {
		lf := addLocal(t, s, inst, "m.jar", fabricJar(t, fmt.Sprintf("m%d", i), "M", nil))
		p := s.stagedPath(inst, lf.SHA1)
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") || strings.Count(filepath.ToSlash(rel), "/") != 1 {
			t.Errorf("实例 %q 的暂存文件落在了 %s", inst, rel)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("实例 %q 的暂存文件不在：%v", inst, err)
		}
		b := filepath.Dir(rel)
		if seen[b] {
			t.Errorf("实例 %q 和别的车共用了文件夹 %s", inst, b)
		}
		seen[b] = true
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.Name() != "local" && e.Name() != "cart.json" {
			t.Errorf("数据目录里多了 %s", e.Name())
		}
	}
}

// 本地文件和 Modrinth 项目合计不超过 MaxItems；移除、清空：Clear 只清 Modrinth 项目，ClearAll 连本地文件和暂存目录一起清。
func TestLocalLimitRemoveClear(t *testing.T) {
	mr := newFakeMR(t)
	dir := t.TempDir()
	s := New(dir, mr.srv.URL)
	ctx := context.Background()
	if _, _, err := s.Add(ctx, Draft, []string{"aaa", "bbb"}); err != nil {
		t.Fatal(err)
	}
	var last LocalFile
	for i := range MaxItems - 2 {
		last = addLocal(t, s, Draft, fmt.Sprintf("m%02d.jar", i), fabricJar(t, fmt.Sprintf("m%02d", i), "M", nil))
	}
	if _, _, err := s.AddLocal(Draft, upload(t, fabricJar(t, "over", "Over", nil)), "over.jar"); codeOf(err) != 400 || !strings.Contains(err.Error(), "最多放 50 项") {
		t.Fatalf("合计超过 50 项应报 400：%v", err)
	}
	if _, _, err := s.Add(ctx, Draft, []string{"fapi"}); codeOf(err) != 400 || !strings.Contains(err.Error(), "已经有 50 项") {
		t.Fatalf("本地文件占满以后再加 Modrinth 项目应报 400：%v", err)
	}
	// 移除一个：登记和暂存文件都没了；清单里没有的不报错
	left, err := s.RemoveLocal(Draft, last.SHA1)
	if err != nil || len(left) != MaxItems-3 {
		t.Fatalf("移除：%d %v", len(left), err)
	}
	if _, err := os.Stat(s.stagedPath(Draft, last.SHA1)); !os.IsNotExist(err) {
		t.Error("移除以后暂存文件应删掉")
	}
	if _, err := s.RemoveLocal(Draft, strings.Repeat("f", 40)); err != nil {
		t.Errorf("清单里没有的不报错：%v", err)
	}
	if _, err := s.RemoveLocal(Draft, `..\..\cart`); err != nil {
		t.Errorf("乱写的编号不报错也不动别的文件：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cart.json")); err != nil {
		t.Fatal("cart.json 不该被动到")
	}
	// Clear（AI 也能调）：只清 Modrinth 项目
	if err := s.Clear(Draft); err != nil || len(s.List(Draft)) != 0 || len(s.Locals(Draft)) != MaxItems-3 {
		t.Fatalf("Clear 之后：%d 项、%d 个本地文件 %v", len(s.List(Draft)), len(s.Locals(Draft)), err)
	}
	// ClearAll：都清，暂存目录也没了；别的车不受影响
	keep := addLocal(t, s, "别的服", "keep.jar", fabricJar(t, "keep", "Keep", nil))
	if err := s.ClearAll(Draft); err != nil || len(s.Locals(Draft)) != 0 {
		t.Fatalf("ClearAll：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "local", "draft")); !os.IsNotExist(err) {
		t.Error("ClearAll 以后这辆车的暂存目录应删掉")
	}
	if _, err := os.Stat(s.stagedPath("别的服", keep.SHA1)); err != nil {
		t.Error("别的车的暂存文件不该动")
	}
	if got := New(dir, "").Locals(Draft); len(got) != 0 {
		t.Errorf("清空以后重开：%+v", got)
	}
	// 指纹：按内容、不看顺序
	a, b := LocalFile{SHA1: "aa"}, LocalFile{SHA1: "bb"}
	if LocalFingerprint([]LocalFile{a, b}) != LocalFingerprint([]LocalFile{b, a}) || LocalFingerprint([]LocalFile{a}) == LocalFingerprint([]LocalFile{a, b}) ||
		LocalFingerprint(nil) != LocalFingerprint([]LocalFile{}) {
		t.Error("本地文件的指纹不对")
	}
	if got := MergeLocal([]LocalFile{a}, []LocalFile{a, b}); len(got) != 2 {
		t.Errorf("合并：%+v", got)
	}
}

// 拷进服务器目录：用登记的文件名；已有同名文件时内容一样算装好、不一样不覆盖；不留临时文件；暂存的内容被换过就不装。
func TestInstallLocal(t *testing.T) {
	s := New(t.TempDir(), newFakeMR(t).srv.URL)
	body := fabricJar(t, "custom", "Custom", nil)
	lf := addLocal(t, s, "生存服", "custom-1.0.jar", body)
	lf = s.Locals("生存服")[0] // 带着它在哪辆车里
	mods := filepath.Join(t.TempDir(), "mods")
	st, err := s.InstallLocal(lf, mods)
	if err != nil || st != LocalCopied {
		t.Fatalf("拷进去：%s %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(mods, "custom-1.0.jar")); !bytes.Equal(b, body) {
		t.Fatal("拷过去的内容不对")
	}
	if st, err := s.InstallLocal(lf, mods); err != nil || st != LocalSame {
		t.Fatalf("同名同内容应算装好：%s %v", st, err)
	}
	if err := os.WriteFile(filepath.Join(mods, "custom-1.0.jar"), []byte("服务器上原来的另一份"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, err := s.InstallLocal(lf, mods); err != nil || st != LocalDiffers {
		t.Fatalf("同名不同内容不覆盖：%s %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(mods, "custom-1.0.jar")); string(b) != "服务器上原来的另一份" {
		t.Fatal("同名不同内容的文件被覆盖了")
	}
	ents, _ := os.ReadDir(mods)
	if len(ents) != 1 {
		t.Fatalf("目录里多了东西：%v", ents)
	}
	// 暂存的那份被换过：不装
	other := filepath.Join(t.TempDir(), "plugins")
	if err := os.WriteFile(s.stagedPath("生存服", lf.SHA1), fabricJar(t, "evil", "Evil", nil), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallLocal(lf, other); err == nil || !strings.Contains(err.Error(), "内容变了") {
		t.Fatalf("暂存内容和登记的对不上应拒绝：%v", err)
	}
	if ents, _ := os.ReadDir(other); len(ents) != 0 {
		t.Fatalf("没装成不该留下文件：%v", ents)
	}
	// 登记的名字不对（cart.json 被人改过）：不拷
	bad := lf
	bad.FileName = `..\run.bat`
	if _, err := s.InstallLocal(bad, other); codeOf(err) != 400 {
		t.Fatalf("名字不对应拒绝：%v", err)
	}
	// 装好以后拿掉：登记和暂存文件都没了
	if err := s.DropLocal(lf); err != nil || len(s.Locals("生存服")) != 0 {
		t.Fatalf("拿掉：%v", err)
	}
	if _, err := s.InstallLocal(lf, other); codeOf(err) != 404 {
		t.Fatalf("暂存文件没了应报 404：%v", err)
	}
}

// 加载器对不上只提醒：按 jar 自己的描述文件看。
func TestLoaderWarn(t *testing.T) {
	paper := []string{"paper", "spigot", "bukkit"}
	for _, c := range []struct {
		mod     string
		loaders []string
		mc      string
		want    string // 提醒里应有的字；空串 = 不提醒
	}{
		{"fabric", fabric, "1.20.1", ""},
		{"forge", fabric, "1.20.1", "给 Forge 用的"},
		{"fabric", []string{"forge"}, "1.20.1", "给 Fabric 用的"},
		{"neoforge+forge", []string{"forge"}, "1.20.1", ""},
		{"neoforge", []string{"forge"}, "1.20.1", "给 NeoForge 用的"},
		{"forge", []string{"neoforge"}, "1.20.1", ""},      // 1.20.5 之前 NeoForge 读的也是 mods.toml
		{"forge", []string{"neoforge"}, "1.21.1", "Forge"}, // 之后只认 neoforge.mods.toml
		{"neoforge", []string{"neoforge"}, "1.21.1", ""},
		{"quilt", fabric, "1.20.1", "Quilt"},
		{"plugin", paper, "", ""},
		{"plugin", fabric, "1.20.1", "这是插件"},
		{"fabric", paper, "", "这台服务器是插件服"},
		{"forge", []string{"velocity"}, "", "Velocity 代理端"},
		{"unknown", fabric, "1.20.1", "没读出"},
		{"unknown", paper, "", "没读出"},
		{"fabric", nil, "", ""},
	} {
		got := LoaderWarn(c.mod, c.loaders, c.mc)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("LoaderWarn(%s, %v, %s) = %q，应%s", c.mod, c.loaders, c.mc, got, map[bool]string{true: "不提醒", false: "含「" + c.want + "」"}[c.want == ""])
		}
	}
}

// 前置检查里的本地文件——在 Modrinth 上认得出来的：它的必装前置并进闭包；别的项目依赖它时算已满足；
// 车里正好也有这个项目时只装本地那一份；加载器、MC 版本对不上只提醒。
func TestClosureRecognizedLocal(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	ctx := context.Background()
	mr.proj("loc", "local-mod", "Local Mod", "required", "MIT")
	locJar := fabricJar(t, "localmod", "Local Mod", map[string]string{"fabric-api": "*"})
	mr.verZip("loc", "v-loc", "3.1", "1.20.1", locJar, mrDep{ProjectID: "fapi", DependencyType: "required"},
		mrDep{ProjectID: "bad", DependencyType: "incompatible"})
	fapiJar := fabricJar(t, "fabric-api", "Fabric API", nil)
	mr.verZip("fapi", "v-fapi-zip", "0.90.0", "1.20.1", fapiJar)

	// 只有一个认得出来的本地文件：缺的前置照常列出来、会自动一起下载
	addLocal(t, s, Draft, "local-mod-3.1.jar", locJar)
	plan, err := s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Local) != 1 || !plan.Local[0].Recognized || plan.Local[0].ProjectID != "loc" || plan.Local[0].Version != "3.1" || plan.Local[0].Warn != "" {
		t.Fatalf("认出来的本地文件：%+v", plan.Local)
	}
	if got := strings.Join(depTitles(plan.Missing), ","); got != "Fabric API←Local Mod" {
		t.Fatalf("它的必装前置应列进缺的：%s", got)
	}
	if len(plan.Files) != 1 || !plan.Files[0].FromDep || plan.Files[0].ProjectID != "fapi" || len(plan.Manual) != 0 {
		t.Fatalf("要下载的应只有自动补的 Fabric API：%+v 手动 %+v", titles(plan.Files), plan.Manual)
	}
	// 它声明的不兼容：车里有 bad 时写进冲突
	bad, _, _ := s.Add(ctx, Draft, []string{"bad"})
	plan, _ = s.ClosureWith(ctx, bad, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], "Local Mod") || !strings.Contains(plan.Conflicts[0], "Bad Mod") {
		t.Fatalf("本地文件声明的不兼容：%v", plan.Conflicts)
	}
	_ = s.ClearAll(Draft)

	// 车里的 Alpha 需要 Fabric API，Fabric API 是本地文件：算已满足，不再下载一份，也不说「服务器里已经有」
	items, _, _ := s.Add(ctx, Draft, []string{"aaa"})
	addLocal(t, s, Draft, "my-fabric-api.jar", fapiJar)
	plan, err = s.ClosureWith(ctx, items, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(titles(plan.Files), ","); got != "Alpha" || len(plan.Missing) != 0 || len(plan.LocalHits) != 0 {
		t.Fatalf("本地文件顶上了前置：要下载 %s，缺 %v，已有 %v", got, depTitles(plan.Missing), plan.LocalHits)
	}
	// 车里正好也有 Fabric API 这个项目：只装本地那一份
	items, _, _ = s.Add(ctx, Draft, []string{"fapi"})
	plan, _ = s.ClosureWith(ctx, items, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if got := strings.Join(titles(plan.Files), ","); got != "Alpha" || plan.Local[0].Covers != "Fabric API" || len(plan.Unresolved) != 0 {
		t.Fatalf("同一个项目只装本地那一份：要下载 %s，Covers=%q", got, plan.Local[0].Covers)
	}
	// 本地文件声明和车里的 Alpha 不兼容的情况在上面测过；这里看反过来：Alpha 声明和 bad 不兼容，bad 是本地文件
	_ = s.ClearAll(Draft)
	badJar := fabricJar(t, "bad-mod", "Bad Mod", nil)
	mr.verZip("bad", "v-bad-zip", "9.9", "1.20.1", badJar)
	items, _, _ = s.Add(ctx, Draft, []string{"aaa"})
	addLocal(t, s, Draft, "bad.jar", badJar)
	plan, _ = s.ClosureWith(ctx, items, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], "本地文件 bad.jar") {
		t.Fatalf("和本地文件不兼容：%v", plan.Conflicts)
	}

	// 加载器对不上（按 Modrinth 上写的）：提醒，不追它的前置，不拦
	_ = s.ClearAll(Draft)
	addLocal(t, s, Draft, "local-mod-3.1.jar", locJar)
	plan, err = s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, []string{"forge"}, "1.20.1")
	if err != nil || !strings.Contains(plan.Local[0].Warn, "给 Fabric 用的") || !strings.Contains(plan.Local[0].Warn, "Forge") || len(plan.Files) != 0 || len(plan.Unresolved) != 0 {
		t.Fatalf("加载器对不上：%+v %v 要下载 %v", plan.Local, err, titles(plan.Files))
	}
	// MC 版本对不上：提醒
	plan, _ = s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, fabric, "1.21.1")
	if !strings.Contains(plan.Local[0].Warn, "Minecraft 1.20.1") || !strings.Contains(plan.Local[0].Warn, "1.21.1") {
		t.Fatalf("MC 版本对不上：%q", plan.Local[0].Warn)
	}
	// 这次连不上 Modrinth 的 /version_files：按认不出处理，并说明
	mr.noFiles = true
	plan, err = s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, fabric, "1.20.1")
	mr.noFiles = false
	if err != nil || plan.Local[0].Recognized || plan.LocalNote == "" || len(plan.Manual) != 1 || plan.Manual[0].ID != "fabric-api" {
		t.Fatalf("认不了的时候按 jar 自己写的查：%+v %v note=%q manual=%+v", plan.Local, err, plan.LocalNote, plan.Manual)
	}
}

// 前置检查里的本地文件——Modrinth 上认不出来的：按 jar 自己声明的依赖，对照车里别的本地文件、服务器里已有的、这次要下载的；
// 还缺的单独列进 Manual（只知道模组 ID，没法代为下载）。
func TestClosureUnknownLocal(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	ctx := context.Background()
	// needs：要 ghost（哪儿都没有）、helper（车里另一个本地文件）、onserver（服务器里已有）、fabric-api（这次要下载的）、
	// 还有服务器本身提供的 minecraft / fabricloader / java
	needs := fabricJar(t, "needs", "Needs Things", map[string]string{"ghost": ">=2", "helper": "*", "onserver": "*", "fabric-api": "*",
		"minecraft": "1.20.1", "fabricloader": "*", "java": ">=17"})
	addLocal(t, s, Draft, "needs.jar", needs)
	addLocal(t, s, Draft, "helper.jar", zipBytes(t, map[string]string{"fabric.mod.json": `{"id":"helper-impl","version":"1","provides":["helper"]}`}))
	server := t.TempDir()
	writeJar(t, server, "onserver.jar", map[string]string{"fabric.mod.json": `{"id":"onserver","version":"1"}`})
	items, _, _ := s.Add(ctx, Draft, []string{"fapi"})
	plan, err := s.ClosureWith(ctx, items, s.Locals(Draft), s.ScanLocal(ctx, server), fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Manual) != 1 || plan.Manual[0].ID != "ghost" || plan.Manual[0].Range != ">=2" || plan.Manual[0].RequiredBy != "Needs Things" || plan.Manual[0].File != "needs.jar" {
		t.Fatalf("要自己去找的前置应只有 ghost：%+v", plan.Manual)
	}
	if plan.Local[0].Recognized || plan.Local[0].Warn != "" || len(plan.Missing) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("认不出来的本地文件：%+v 缺 %v 冲突 %v", plan.Local[0], depTitles(plan.Missing), plan.Conflicts)
	}
	b, _ := json.Marshal(plan)
	for _, key := range []string{`"local":[`, `"manual":[`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("JSON 里 %s 应是数组：%s", key, b)
		}
	}
	if empty, _ := s.Closure(ctx, nil, Local{}, fabric, "1.20.1"); empty.Local == nil || empty.Manual == nil {
		t.Error("没有本地文件时 Local、Manual 也不能是 nil（JSON 里要是 []）")
	}

	// 加载器不对（Forge 的 jar 放进 Fabric 服）、读不出信息：提醒，不拦
	_ = s.ClearAll(Draft)
	addLocal(t, s, Draft, "forge-only.jar", zipBytes(t, map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId = \"forgey\"\ndisplayName = \"Forgey\"\nversion = \"1\"\n"}))
	addLocal(t, s, Draft, "plain.jar", zipBytes(t, map[string]string{"a.txt": "x"}))
	plan, err = s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, fabric, "1.20.1")
	if err != nil || !strings.Contains(plan.Local[0].Warn, "给 Forge 用的") || !strings.Contains(plan.Local[1].Warn, "没读出") {
		t.Fatalf("提醒：%+v %v", plan.Local, err)
	}

	// 插件：plugin.yml 的 depend 是必需的，softdepend 不算
	_ = s.ClearAll(Draft)
	addLocal(t, s, Draft, "shop.jar", zipBytes(t, map[string]string{"plugin.yml": "name: Shop\nversion: 1.0\ndepend: [Vault]\nsoftdepend: [Essentials]\n"}))
	plan, _ = s.ClosureWith(ctx, nil, s.Locals(Draft), Local{}, []string{"paper", "spigot", "bukkit"}, "")
	if len(plan.Manual) != 1 || plan.Manual[0].ID != "Vault" || plan.Local[0].Warn != "" {
		t.Fatalf("插件的必需前置：%+v %+v", plan.Manual, plan.Local)
	}
}

// 前置检查里的本地文件——对照服务器目录：一模一样的文件安装时跳过；同一个模组的另一份、同名不同内容的文件写进冲突提醒；
// 车里两个本地文件是同一个模组、本地文件和要下载的是同一个模组，也提醒。不自动删任何东西。
func TestClosureLocalConflicts(t *testing.T) {
	mr := newFakeMR(t)
	s := New(t.TempDir(), mr.srv.URL)
	ctx := context.Background()
	server := t.TempDir()
	same := fabricJar(t, "same", "Same", nil)
	if err := os.WriteFile(filepath.Join(server, "already-here.jar"), same, 0o644); err != nil {
		t.Fatal(err)
	}
	writeJar(t, server, "sodium-old.jar", map[string]string{"fabric.mod.json": `{"id":"sodium","version":"0.4"}`})
	writeJar(t, server, "taken.jar", map[string]string{"fabric.mod.json": `{"id":"taken-old","version":"1"}`})
	writeJar(t, server, "off.jar.disabled", map[string]string{"fabric.mod.json": `{"id":"offmod","version":"1"}`})

	addLocal(t, s, Draft, "same-renamed.jar", same)                                                                  // 0：服务器里有一模一样的
	addLocal(t, s, Draft, "sodium-new.jar", fabricJar(t, "sodium", "Sodium", nil))                                   // 1：同一个模组的另一份
	addLocal(t, s, Draft, "Taken.jar", fabricJar(t, "taken-new", "Taken", nil))                                      // 2：同名不同内容
	addLocal(t, s, Draft, "off-new.jar", fabricJar(t, "offmod", "Off", nil))                                         // 3：服务器里那份停用了，不算冲突
	addLocal(t, s, Draft, "sodium-extra.jar", fabricJar(t, "Sodium", "Sodium Extra", map[string]string{"x": "*"}))   // 4：和 1 是同一个模组
	addLocal(t, s, Draft, "fabric_api-mine.jar", fabricJar(t, "fabric_api", "My FAPI", map[string]string{"y": "*"})) // 5：和要下载的 Fabric API 像同一个
	before, _ := os.ReadDir(server)
	items, _, _ := s.Add(ctx, Draft, []string{"fapi"})
	plan, err := s.ClosureWith(ctx, items, s.Locals(Draft), s.ScanLocal(ctx, server), fabric, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Local[0].Skip != "already-here.jar" || !slices.Contains(plan.LocalHits, "already-here.jar") {
		t.Errorf("一模一样的文件应标成跳过：%+v 已有 %v", plan.Local[0], plan.LocalHits)
	}
	if !plan.Local[2].NameTaken || plan.Local[1].NameTaken || plan.Local[1].Skip != "" {
		t.Errorf("同名不同内容：%+v", plan.Local)
	}
	all := strings.Join(plan.Conflicts, "\n")
	for _, want := range []string{
		"本地文件 sodium-new.jar 和服务器里已有的 sodium-old.jar 是同一个模组",
		"服务器里已经有一个也叫 taken.jar 的文件",
		"本地文件 sodium-new.jar 和 sodium-extra.jar 是同一个模组的两份",
		"本地文件 fabric_api-mine.jar 和要下载的 Fabric API 看起来是同一个模组",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("冲突提醒里应有「%s」：\n%s", want, all)
		}
	}
	if strings.Contains(all, "off") || strings.Contains(all, "same-renamed") || len(plan.Conflicts) != 5 {
		// 5 条：上面 4 条 + sodium-extra 和服务器里的 sodium-old 也是同一个模组
		t.Errorf("冲突提醒多了或少了（%d 条）：\n%s", len(plan.Conflicts), all)
	}
	after, _ := os.ReadDir(server)
	if len(after) != len(before) {
		t.Error("检查前置不该动服务器目录里的任何文件")
	}
	if len(s.Locals(Draft)) != 6 {
		t.Error("检查前置不该动清单")
	}
}
