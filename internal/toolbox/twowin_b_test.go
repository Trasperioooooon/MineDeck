package toolbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// 两个窗口同时开着工具箱的「导出 Modrinth 整合包」（2026-11-17）：以前每次保存都把整页的草稿写回去，没刷新的那一边
// 就把另一边刚改的字段改回原样。现在只发改了的字段（PatchMrpackDraft），在 draftMu 里读-合-写。

func twbStr(s string) *string { return &s }
func twbBool(b bool) *bool    { return &b }

func twbService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	return New(Options{DataDir: dir, CacheDir: filepath.Join(dir, "cache")}), dir
}

// A 只改名字、B（还是 A 改之前的页面）只改简介：两样都在，别的字段是原来存的。重开程序以后还是这样。
func TestDraftPatchTwoWindowsKeepBoth(t *testing.T) {
	svc, dir := twbService(t)
	orig := DraftConfig{Name: "原名", VersionID: "2.0.0", Core: "forge", MC: "1.20.1", LoaderVersion: "47.1.0", IncludeConfig: true}
	if _, err := svc.SaveMrpackDraft(orig); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PatchMrpackDraft(DraftPatch{Name: twbStr("  A 的名字 ")}); err != nil { // 窗口 A
		t.Fatalf("A 只改名字：%v", err)
	}
	got, err := svc.PatchMrpackDraft(DraftPatch{Summary: twbStr("B 的简介")}) // 窗口 B
	if err != nil {
		t.Fatalf("B 只改简介：%v", err)
	}
	want := orig
	want.Name, want.Summary = "A 的名字", "B 的简介"
	if got != want {
		t.Fatalf("返回的整份：\n得到 %+v\n应为 %+v", got, want)
	}
	again, err := New(Options{DataDir: dir, CacheDir: filepath.Join(dir, "cache")}).ReadMrpackDraft()
	if err != nil || again != want {
		t.Fatalf("重开以后：%+v %v", again, err)
	}
}

// 还没存过草稿：没给的字段是默认值；没有数据目录时不写盘，只返回合好的。
func TestDraftPatchDefaults(t *testing.T) {
	svc, dir := twbService(t)
	got, err := svc.PatchMrpackDraft(DraftPatch{Summary: twbStr("简介"), IncludeConfig: twbBool(true)})
	want := DraftConfig{Name: "我的整合包", VersionID: "1.0.0", Summary: "简介", Core: "fabric", MC: "1.20.1", IncludeConfig: true}
	if err != nil || got != want {
		t.Fatalf("没存过时：%+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, draftFile)); err != nil {
		t.Fatalf("应写下草稿：%v", err)
	}
	mem := &Service{}
	if got, err := mem.PatchMrpackDraft(DraftPatch{Name: twbStr("内存里")}); err != nil || got.Name != "内存里" || got.VersionID != "1.0.0" {
		t.Fatalf("没有数据目录：%+v %v", got, err)
	}
}

// 太长：400，存好的不动。
func TestDraftPatchTooLong(t *testing.T) {
	svc, dir := twbService(t)
	if _, err := svc.PatchMrpackDraft(DraftPatch{Name: twbStr("存好的")}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, draftFile))
	_, err := svc.PatchMrpackDraft(DraftPatch{Summary: twbStr(strings.Repeat("字", 301))})
	var te *Error
	if !errors.As(err, &te) || te.Code != 400 || !strings.Contains(err.Error(), "太长") {
		t.Fatalf("简介太长应报 400：%v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, draftFile)); string(after) != string(before) {
		t.Fatal("太长时不该写盘")
	}
}

// 存好的草稿文件在却读不出来（这里是同名的文件夹）：报 500、不写；内容坏了照旧按默认值合。
func TestDraftPatchUnreadableRefused(t *testing.T) {
	svc, dir := twbService(t)
	path := filepath.Join(dir, draftFile)
	if err := os.WriteFile(path, []byte("{坏的"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.PatchMrpackDraft(DraftPatch{MC: twbStr("1.21.1")}); err != nil || got.Name != "我的整合包" || got.MC != "1.21.1" {
		t.Fatalf("内容坏了按默认值合：%+v %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := svc.PatchMrpackDraft(DraftPatch{Name: twbStr("不该写下")})
	var te *Error
	if !errors.As(err, &te) || te.Code != 500 || !strings.Contains(err.Error(), "读不出存好的整合包草稿") {
		t.Fatalf("读不出来应报 500：%v", err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Fatalf("读不出来时不该写：%v %v", fi, err)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("留下了临时文件：%s", e.Name())
		}
	}
}

// 七个窗口同时各改一个字段：全在 draftMu 里一个一个合，最后七样都在。
func TestDraftPatchConcurrent(t *testing.T) {
	svc, _ := twbService(t)
	patches := []DraftPatch{
		{Name: twbStr("名字")}, {VersionID: twbStr("3.0.0")}, {Summary: twbStr("简介")}, {Core: twbStr("neoforge")},
		{MC: twbStr("1.21.1")}, {LoaderVersion: twbStr("21.1.0")}, {IncludeConfig: twbBool(true)},
	}
	for round := range 20 {
		if _, err := svc.SaveMrpackDraft(DraftConfig{}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, len(patches))
		var wg sync.WaitGroup
		for _, p := range patches {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := svc.PatchMrpackDraft(p)
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("第 %d 轮同时改报错：%v", round, err)
			}
		}
		want := DraftConfig{Name: "名字", VersionID: "3.0.0", Summary: "简介", Core: "neoforge", MC: "1.21.1", LoaderVersion: "21.1.0", IncludeConfig: true}
		if got, err := svc.ReadMrpackDraft(); err != nil || got != want {
			t.Fatalf("第 %d 轮同时改以后：\n得到 %+v\n应为 %+v（%v）", round, got, want, err)
		}
	}
}
