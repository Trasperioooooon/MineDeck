package modcart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这一组测的是 cart.json 没读成的时候（照 tunnel 包的 load_test.go）。以前读不出来就从空清单开始，启动时按空清单把
// 暂存的本地文件清掉，解析不了的改名成 .bad，之后随便存一次就把原文件盖掉。现在读不出来、解析不了都记成 LoadError：
// 读成之前什么都不存、不删，cart.json 和暂存的文件原样；读成了（放开了、修好了）自动恢复，不用重启。
// 被别的程序占着的那条在 load_windows_test.go。

// fastLoadRetry 启动时的重读不等那么久，测试跑得快。
func fastLoadRetry(t *testing.T) {
	t.Helper()
	oldWait, oldN := loadRetryWait, loadRetries
	t.Cleanup(func() { loadRetryWait, loadRetries = oldWait, oldN })
	loadRetryWait = time.Millisecond
}

// seededCart 一份有东西的清单：草稿车里一个 Modrinth 项目（aaa）、一个本地文件，放在 <base>/data/modcart 下面。
type seededCart struct {
	mr     *fakeMR
	base   string
	dir    string
	json   string // cart.json 的原文
	local  LocalFile
	staged string // 那个本地文件暂存在哪
}

func (c seededCart) path() string { return filepath.Join(c.dir, "cart.json") }

func seedCart(t *testing.T) seededCart {
	t.Helper()
	fastLoadRetry(t)
	mr := newFakeMR(t)
	base := t.TempDir()
	dir := filepath.Join(base, "data", "modcart")
	s := New(dir, mr.srv.URL)
	if _, _, err := s.Add(context.Background(), Draft, []string{"aaa"}); err != nil {
		t.Fatal(err)
	}
	lf := addLocal(t, s, Draft, "Custom-1.0.jar", fabricJar(t, "custom", "Custom Mod", nil))
	c := seededCart{mr: mr, base: base, dir: dir, local: lf, staged: s.stagedPath(Draft, lf.SHA1)}
	c.json = readText(t, c.path())
	return c
}

func readText(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantLoadError(t *testing.T, err error, corrupt bool) *LoadError {
	t.Helper()
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("没读成时应报 LoadError，实际：%v", err)
	}
	if le.Corrupt != corrupt {
		t.Fatalf("LoadError.Corrupt=%v，应为 %v：%v", le.Corrupt, corrupt, err)
	}
	return le
}

// assertBlocked 没读成时：清单是空的；加入、移除、清空、添加和移除本地文件都报 LoadError，写盘的最后一道闸也挡着；
// 暂存的本地文件一个不少、也没多出来。cart.json 原文由调用方比（被独占时读不了，要先放开）。
func assertBlocked(t *testing.T, s *Service, c seededCart, corrupt bool) {
	t.Helper()
	wantLoadError(t, s.LoadErr(), corrupt)
	if len(s.List(Draft)) != 0 || len(s.Locals(Draft)) != 0 {
		t.Fatalf("没读成时清单应是空的：%v %v", s.List(Draft), s.Locals(Draft))
	}
	_, _, err := s.Add(context.Background(), Draft, []string{"bbb"})
	wantLoadError(t, err, corrupt)
	_, err = s.Remove(Draft, "aaa")
	wantLoadError(t, err, corrupt)
	wantLoadError(t, s.Clear(Draft), corrupt)
	wantLoadError(t, s.ClearAll(Draft), corrupt)
	src := upload(t, fabricJar(t, "other", "Other", nil))
	_, _, err = s.AddLocal(Draft, src, "Other.jar")
	wantLoadError(t, err, corrupt)
	if _, err := os.Stat(src); err != nil {
		t.Error("没放进去的临时文件应留在原处（调用方删）")
	}
	_, err = s.RemoveLocal(Draft, c.local.SHA1)
	wantLoadError(t, err, corrupt)
	s.mu.Lock()
	err = s.writeLocked(map[string][]Item{}, nil, draftTarget{})
	s.mu.Unlock()
	wantLoadError(t, err, corrupt)
	if len(s.List(Draft)) != 0 || len(s.Locals(Draft)) != 0 {
		t.Fatal("没读成时内存里的清单被改了")
	}
	if c.mr.count("/projects") != 1 {
		t.Errorf("没读成时加入不该先去问 Modrinth（只有种清单时那一次）：%d", c.mr.count("/projects"))
	}
	assertStagedIntact(t, c)
}

// assertStagedIntact 暂存的本地文件还在，暂存目录里也没多出别的文件。
func assertStagedIntact(t *testing.T, c seededCart) {
	t.Helper()
	if _, err := os.Stat(c.staged); err != nil {
		t.Fatalf("暂存的本地文件被删了：%v", err)
	}
	if ents, _ := os.ReadDir(filepath.Dir(c.staged)); len(ents) != 1 {
		t.Fatalf("暂存目录里应只有原来那 1 个文件：%v", ents)
	}
}

// assertRecovered 读成以后：直接再加一项（不先问 LoadErr，加入自己就要再读一次），原来的项目和本地文件都还在；
// 重开以后清单、本地文件、暂存的文件都在，也没有 .bad 之类的副本。
func assertRecovered(t *testing.T, s *Service, c seededCart) {
	t.Helper()
	items, _, err := s.Add(context.Background(), Draft, []string{"bbb"})
	if err != nil || len(items) != 2 || items[0].ProjectID != "aaa" || items[1].ProjectID != "bbb" {
		t.Fatalf("恢复以后再加应成功、原来那项还在：%+v %v", items, err)
	}
	s2 := New(c.dir, c.mr.srv.URL)
	if got := s2.List(Draft); len(got) != 2 {
		t.Fatalf("重开以后的清单：%+v", got)
	}
	if got := s2.Locals(Draft); len(got) != 1 || got[0].SHA1 != c.local.SHA1 {
		t.Fatalf("重开以后的本地文件：%+v", got)
	}
	assertStagedIntact(t, c)
	if ents, _ := os.ReadDir(c.dir); len(ents) != 2 {
		t.Fatalf("数据目录里应只有 cart.json 和 local：%v", ents)
	}
}

// 坏文件（写到一半的 JSON）：不当空清单、不另存 .bad，什么都不存不删；用户修好以后不用重启，下一次操作就读回来。
func TestCartCorruptFileBlocksUntilFixed(t *testing.T) {
	c := seedCart(t)
	broken := c.json[:len(c.json)/2]
	if err := os.WriteFile(c.path(), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(c.dir, c.mr.srv.URL)
	assertBlocked(t, s, c, true)
	le := wantLoadError(t, s.LoadErr(), true)
	if !strings.Contains(le.Error(), "行附近") || !strings.Contains(le.Error(), "内容坏了") || strings.Contains(le.Error(), c.base) {
		t.Fatalf("坏文件的报错要说在第几行附近、不带路径：%v", le)
	}
	if readText(t, c.path()) != broken {
		t.Fatal("没读成时 cart.json 被改了")
	}
	// 用户用记事本修好了（记事本存的带 BOM，也要认）
	if err := os.WriteFile(c.path(), append([]byte("\xef\xbb\xbf"), c.json...), 0o644); err != nil {
		t.Fatal(err)
	}
	assertRecovered(t, s, c)
}

// 空文件：里面没有登记，可暂存的本地文件还在；当空清单的话启动时就把它们清掉了，所以也挡着。
// 没有 carts 这一项（键名改错了、整个是 null 或 {}）同样按坏文件处理：MineDeck 写的文件总带着它。
func TestCartEmptyOrShapelessFileBlocks(t *testing.T) {
	for name, body := range map[string]string{"空文件": "", "只有空白": " \r\n", "null": "null", "{}": "{}",
		"键名改错": `{"version":1,"cartz":{"":[{"projectId":"aaa"}]}}`, "数组": `[1,2]`} {
		t.Run(name, func(t *testing.T) {
			c := seedCart(t)
			if err := os.WriteFile(c.path(), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			s := New(c.dir, c.mr.srv.URL)
			assertBlocked(t, s, c, true)
			if readText(t, c.path()) != body {
				t.Fatal("没读成时 cart.json 被改了")
			}
			if err := os.WriteFile(c.path(), []byte(c.json), 0o644); err != nil {
				t.Fatal(err)
			}
			assertRecovered(t, s, c)
		})
	}
}

// 读不出来（这里用同名的文件夹顶在那里，各个系统上读它都报错）：不当空清单；挪开、放回原文件以后自动恢复。
func TestCartFolderInTheWayBlocksUntilReadable(t *testing.T) {
	c := seedCart(t)
	aside := c.path() + ".aside"
	if err := os.Rename(c.path(), aside); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c.path(), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(c.dir, c.mr.srv.URL)
	assertBlocked(t, s, c, false)
	if err := os.Remove(c.path()); err != nil {
		t.Fatalf("顶着的文件夹应原样还在（没往里写东西）：%v", err)
	}
	if err := os.Rename(aside, c.path()); err != nil {
		t.Fatal(err)
	}
	if readText(t, c.path()) != c.json {
		t.Fatal("原文件被动了")
	}
	assertRecovered(t, s, c)
}

// 程序文件夹暂时访问不了（网络盘断了、U 盘拔了）：读 cart.json 也报「不存在」，但这不是新装。
// 当成新装的话，启动时就按空清单清暂存目录，文件夹一回来，下一次加入就把原文件盖掉。这里把程序文件夹挪开再挪回来模拟。
// 没读成期间也不能把程序文件夹建回来（写盘、暂存本地文件之前都会建文件夹）。
func TestCartUnreachableProgramFolderIsNotNewInstall(t *testing.T) {
	c := seedCart(t)
	away := c.base + "-away"
	if err := os.Rename(c.base, away); err != nil {
		t.Fatal(err)
	}
	s := New(c.dir, c.mr.srv.URL)
	le := wantLoadError(t, s.LoadErr(), false)
	if !strings.Contains(le.Error(), "程序文件夹访问不了") || strings.Contains(le.Error(), c.base) {
		t.Fatalf("要说清是程序文件夹访问不了、不带路径：%v", le)
	}
	if _, _, err := s.Add(context.Background(), Draft, []string{"bbb"}); err == nil {
		t.Fatal("程序文件夹访问不了时不该加进去")
	}
	if _, _, err := s.AddLocal(Draft, upload(t, fabricJar(t, "other", "Other", nil)), "Other.jar"); err == nil {
		t.Fatal("程序文件夹访问不了时不该收本地文件")
	}
	wantLoadError(t, s.ClearAll(Draft), false)
	if _, err := os.Stat(c.base); !os.IsNotExist(err) {
		t.Fatalf("没读成时把程序文件夹建回来了：%v", err)
	}
	if err := os.Rename(away, c.base); err != nil {
		t.Fatal(err)
	}
	assertStagedIntact(t, c)
	if readText(t, c.path()) != c.json {
		t.Fatal("原文件被动了")
	}
	assertRecovered(t, s, c)
}

// 文件不在、程序文件夹看得到：新装，空清单是对的，照常能加能存。data、data\modcart 第一次存清单时才建，不在也算新装。
// 暂存目录里没登记的文件照旧清掉（新装时本来就不该有）。
func TestCartMissingFileStartsEmpty(t *testing.T) {
	mr := newFakeMR(t)
	for name, prep := range map[string]func(base, dir string){
		"data 都还没有":            func(string, string) {},
		"data\\modcart 在、没有清单": func(_, dir string) { _ = os.MkdirAll(dir, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "data", "modcart")
			prep(base, dir)
			s := New(dir, mr.srv.URL)
			if err := s.LoadErr(); err != nil {
				t.Fatalf("文件不在不算没读成：%v", err)
			}
			if items, _, err := s.Add(context.Background(), Draft, []string{"aaa"}); err != nil || len(items) != 1 {
				t.Fatalf("新装时加入：%+v %v", items, err)
			}
			if got := New(dir, mr.srv.URL).List(Draft); len(got) != 1 {
				t.Fatalf("重开：%+v", got)
			}
		})
	}
	// 清单删掉了、暂存目录里还留着没登记的文件：按新装，启动时清掉
	base := t.TempDir()
	dir := filepath.Join(base, "data", "modcart")
	s := New(dir, mr.srv.URL)
	lf := addLocal(t, s, Draft, "a.jar", fabricJar(t, "a", "A", nil))
	if err := os.Remove(filepath.Join(dir, "cart.json")); err != nil {
		t.Fatal(err)
	}
	if New(dir, mr.srv.URL).LoadErr() != nil {
		t.Fatal("清单删掉了算新装")
	}
	if _, err := os.Stat(s.stagedPath(Draft, lf.SHA1)); !os.IsNotExist(err) {
		t.Fatalf("新装时没登记的暂存文件应清掉：%v", err)
	}
}
