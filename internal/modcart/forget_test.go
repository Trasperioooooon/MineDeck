package modcart

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这一组测的是删掉一台服务器时清它的清单，尤其是 cart.json 读不出来的那段时间删的（A30）：记进 cart-pending-removals.json，
// 读出来以后补清；同名新建的那台后来加的东西不跟着清；这个小文件写不进、读不出来都不影响 cart.json。

const oldInst = "旧服" // 要删掉的那台

// forgetCart 一份有东西的清单：旧服里一个 Modrinth 项目（aaa）、一个本地文件；别的服里一个项目（bbb）；
// 草稿清单里一个项目（fapi），记的目标服务器是旧服。放在 <base>/data/modcart 下面。
type forgetCart struct {
	mr     *fakeMR
	base   string
	dir    string
	json   string    // cart.json 的原文
	staged string    // 旧服那个本地文件暂存在哪
	seeded time.Time // 种完清单的时间：清单里的东西都是这之前加的
}

func (c forgetCart) cartPath() string     { return filepath.Join(c.dir, "cart.json") }
func (c forgetCart) removalsPath() string { return filepath.Join(c.dir, removalsName) }

func seedForget(t *testing.T) forgetCart {
	t.Helper()
	fastLoadRetry(t)
	mr := newFakeMR(t)
	base := t.TempDir()
	dir := filepath.Join(base, "data", "modcart")
	s := New(dir, mr.srv.URL)
	ctx := context.Background()
	for inst, id := range map[string]string{oldInst: "aaa", "别的服": "bbb", Draft: "fapi"} {
		if _, _, err := s.Add(ctx, inst, []string{id}); err != nil {
			t.Fatal(err)
		}
	}
	lf := addLocal(t, s, oldInst, "Old-1.0.jar", fabricJar(t, "old", "Old Mod", nil))
	if err := s.SetDraftInst(oldInst); err != nil {
		t.Fatal(err)
	}
	c := forgetCart{mr: mr, base: base, dir: dir, staged: s.stagedPath(oldInst, lf.SHA1), seeded: time.Now().UTC()}
	c.json = readText(t, c.cartPath())
	return c
}

// unreadable 把 cart.json 换成写到一半的，按这个样子重开：cart.json 没读成。
func (c forgetCart) unreadable(t *testing.T) *Service {
	t.Helper()
	if err := os.WriteFile(c.cartPath(), []byte(c.json[:len(c.json)/2]), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(c.dir, c.mr.srv.URL)
	wantLoadError(t, s.LoadErr(), true)
	return s
}

// fix 用户修好了 cart.json（放回原文）。
func (c forgetCart) fix(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(c.cartPath(), []byte(c.json), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readRemovalsFile 读删服记录的原样内容。
func readRemovalsFile(t *testing.T, c forgetCart) removalsFile {
	t.Helper()
	var f removalsFile
	if err := json.Unmarshal([]byte(readText(t, c.removalsPath())), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// assertForgotten 旧服的本地文件、暂存的文件和指向它的记号都清掉了（wantDraftInst 是这时该记着的目标），
// 别的服和草稿清单原样，删服记录删掉了。旧服的 Modrinth 项目由调用方查（补清以后可能又加了）。
func assertForgotten(t *testing.T, s *Service, c forgetCart, wantDraftInst string) {
	t.Helper()
	if locals := s.Locals(oldInst); len(locals) != 0 {
		t.Fatalf("删掉的那台的本地文件应清掉：%+v", locals)
	}
	if _, err := os.Stat(filepath.Dir(c.staged)); !os.IsNotExist(err) {
		t.Fatalf("删掉的那台暂存的本地文件应删掉：%v", err)
	}
	if got := s.DraftInst(); got != wantDraftInst {
		t.Fatalf("草稿清单记的目标服务器：%q，应为 %q", got, wantDraftInst)
	}
	if got := s.List("别的服"); len(got) != 1 || got[0].ProjectID != "bbb" {
		t.Fatalf("别的服的清单不该动：%+v", got)
	}
	if got := s.List(Draft); len(got) != 1 || got[0].ProjectID != "fapi" {
		t.Fatalf("草稿清单本身不该动：%+v", got)
	}
	if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
		t.Fatalf("补清以后删服记录应删掉：%v", err)
	}
}

// waitPast 等到墙上时间越过 at：后面加进清单的东西，加入时间要比删除的时间晚（Windows 的时钟有时十几毫秒才走一格）。
func waitPast(at time.Time) {
	for !time.Now().UTC().After(at) {
		time.Sleep(time.Millisecond)
	}
}

// 读不出来时删服：清不了，记进删服记录；cart.json 和暂存的文件一个不动。修好以后不重启，下一次用清单的请求先补清，再办它自己的事。
// 两种请求各测一遍，都能看出补清是不是在它之前：同名新建的那台记目标服务器（补清在后的话，刚记上的记号会被清掉），
// 往同名的那台里加上一台就有的项目（补清在后的话，「已经在清单里」不重复加，随后又被补清清掉）。
func TestForgetInstanceWhileUnreadableSettlesOnRecovery(t *testing.T) {
	for name, tc := range map[string]struct {
		trigger func(*Service) error
		draft   string // 补清以后草稿清单该记着的目标
		items   int    // 补清以后旧服清单里该有几项（只能是这次请求加的）
	}{
		"同名新建的那台记目标服务器": {func(s *Service) error { return s.SetDraftInst(oldInst) }, oldInst, 0},
		"往同名的那台里加上一台就有的项目": {func(s *Service) error {
			_, _, err := s.Add(context.Background(), oldInst, []string{"aaa"})
			return err
		}, "", 1},
	} {
		t.Run(name, func(t *testing.T) {
			c := seedForget(t)
			s := c.unreadable(t)
			broken := readText(t, c.cartPath())
			if err := s.ForgetInstance(oldInst); err != nil {
				t.Fatalf("读不出来时删服应记下来、不报错：%v", err)
			}
			f := readRemovalsFile(t, c)
			if len(f.Removals) != 1 || f.Removals[0].Inst != oldInst || f.Removals[0].At.Before(c.seeded) {
				t.Fatalf("删服记录：%+v", f)
			}
			if readText(t, c.cartPath()) != broken {
				t.Fatal("读不出来时 cart.json 被改了")
			}
			if _, err := os.Stat(c.staged); err != nil {
				t.Fatalf("读不出来时暂存的本地文件不能删：%v", err)
			}

			c.fix(t)
			waitPast(f.Removals[0].At)
			if err := tc.trigger(s); err != nil {
				t.Fatal(err)
			}
			for _, s := range []*Service{s, New(c.dir, c.mr.srv.URL)} { // 重开一遍：补清的结果存进 cart.json 了
				got := s.List(oldInst)
				if len(got) != tc.items || (tc.items == 1 && !got[0].AddedAt.After(f.Removals[0].At)) {
					t.Fatalf("旧服清单里应只有这次请求加的 %d 项：%+v", tc.items, got)
				}
				assertForgotten(t, s, c, tc.draft)
			}
		})
	}
}

// 读不出来时删服，修好以后重开 MineDeck：启动时读成就补清。同一台删了两次（中间又建了一台同名的）只记一条、时间取后一次；
// 几台一起记。
func TestForgetInstanceSettlesAtStartup(t *testing.T) {
	c := seedForget(t)
	s := c.unreadable(t)
	for _, inst := range []string{oldInst, "别的服", oldInst} {
		if err := s.ForgetInstance(inst); err != nil {
			t.Fatal(err)
		}
		waitPast(time.Now().UTC()) // 三次删除的时间各不相同
	}
	f := readRemovalsFile(t, c)
	if len(f.Removals) != 2 || f.Removals[0].Inst != oldInst || f.Removals[1].Inst != "别的服" || !f.Removals[0].At.After(f.Removals[1].At) {
		t.Fatalf("同一台只记一条、时间取后一次：%+v", f)
	}
	c.fix(t)
	s2 := New(c.dir, c.mr.srv.URL)
	if err := s2.LoadErr(); err != nil {
		t.Fatal(err)
	}
	if got := s2.List("别的服"); len(got) != 0 {
		t.Fatalf("一起记下的别的服也应清掉：%+v", got)
	}
	if got := s2.List(oldInst); len(got) != 0 || len(s2.Locals(oldInst)) != 0 || s2.DraftInst() != "" {
		t.Fatalf("旧服的清单和记号应清掉：%+v %+v %q", got, s2.Locals(oldInst), s2.DraftInst())
	}
	if _, err := os.Stat(filepath.Dir(c.staged)); !os.IsNotExist(err) {
		t.Fatalf("旧服暂存的本地文件应删掉：%v", err)
	}
	if got := s2.List(Draft); len(got) != 1 {
		t.Fatalf("草稿清单本身不该动：%+v", got)
	}
	if ents, _ := os.ReadDir(c.dir); len(ents) != 2 {
		t.Fatalf("数据目录里应只剩 cart.json 和 local：%v", ents)
	}
}

// 补清要等到删服记录读得出来：读成 cart.json 那一刻它坏着，就先留着，旧服的东西这时还在。这期间同名新建的那台加了
// 东西（Modrinth 项目和本地文件），等记录修好、下次启动补清时，只清删服之前就在的，新加的留着。
func TestForgetInstanceKeepsWhatSameNameAddedLater(t *testing.T) {
	c := seedForget(t)
	s := c.unreadable(t)
	if err := s.ForgetInstance(oldInst); err != nil {
		t.Fatal(err)
	}
	record := readText(t, c.removalsPath())
	deletedAt := readRemovalsFile(t, c).Removals[0].At
	if err := os.WriteFile(c.removalsPath(), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	c.fix(t)
	if err := s.LoadErr(); err != nil {
		t.Fatalf("删服记录坏了不该挡 cart.json：%v", err)
	}
	if got := s.List(oldInst); len(got) != 1 || got[0].ProjectID != "aaa" {
		t.Fatalf("删服记录读不出来时先不清：%+v", got)
	}
	if readText(t, c.removalsPath()) != "{" {
		t.Fatal("读不出来的删服记录应原样留着")
	}

	waitPast(deletedAt)
	if _, _, err := s.Add(context.Background(), oldInst, []string{"bbb"}); err != nil {
		t.Fatal(err)
	}
	fresh := addLocal(t, s, oldInst, "New-1.0.jar", fabricJar(t, "new", "New Mod", nil))

	if err := os.WriteFile(c.removalsPath(), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := New(c.dir, c.mr.srv.URL)
	if got := s2.List(oldInst); len(got) != 1 || got[0].ProjectID != "bbb" {
		t.Fatalf("只清删服之前的 aaa，删服以后加的 bbb 留着：%+v", got)
	}
	if got := s2.Locals(oldInst); len(got) != 1 || got[0].SHA1 != fresh.SHA1 {
		t.Fatalf("删服以后加的本地文件留着：%+v", got)
	}
	if _, err := os.Stat(c.staged); !os.IsNotExist(err) {
		t.Fatalf("删服之前那个本地文件暂存的文件应删掉：%v", err)
	}
	if _, err := os.Stat(s2.stagedPath(oldInst, fresh.SHA1)); err != nil {
		t.Fatalf("删服以后加的本地文件暂存的文件应留着：%v", err)
	}
	if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
		t.Fatalf("补清以后删服记录应删掉：%v", err)
	}
}

// 删服记录写不进：报错（由删服的结果说明），什么都不建、不删。程序文件夹暂时访问不了时不能把它建回来（见 reallyMissing）；
// 记录的位置上顶着一个文件夹时不往里写，文件夹原样留着。报错里不带路径。
func TestForgetInstanceRecordUnwritable(t *testing.T) {
	t.Run("程序文件夹访问不了", func(t *testing.T) {
		c := seedForget(t)
		away := c.base + "-away"
		if err := os.Rename(c.base, away); err != nil {
			t.Fatal(err)
		}
		s := New(c.dir, c.mr.srv.URL)
		err := s.ForgetInstance(oldInst)
		if err == nil || !strings.Contains(err.Error(), "没能记下来") || strings.Contains(err.Error(), c.base) {
			t.Fatalf("记不下来时应报错、不带路径：%v", err)
		}
		if _, err := os.Stat(c.base); !os.IsNotExist(err) {
			t.Fatalf("记不下来时把程序文件夹建回来了：%v", err)
		}
		if err := os.Rename(away, c.base); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
			t.Fatalf("不该留下删服记录：%v", err)
		}
		if got := New(c.dir, c.mr.srv.URL).List(oldInst); len(got) != 1 {
			t.Fatalf("没记下来就清不了，清单原样：%+v", got)
		}
	})
	t.Run("记录的位置上顶着文件夹", func(t *testing.T) {
		c := seedForget(t)
		if err := os.Mkdir(c.removalsPath(), 0o755); err != nil {
			t.Fatal(err)
		}
		s := c.unreadable(t)
		err := s.ForgetInstance(oldInst)
		if err == nil || !strings.Contains(err.Error(), "没能记下来") || strings.Contains(err.Error(), c.base) {
			t.Fatalf("记不下来时应报错、不带路径：%v", err)
		}
		if ents, err := os.ReadDir(c.removalsPath()); err != nil || len(ents) != 0 {
			t.Fatalf("顶着的文件夹应原样留着、没往里写：%v %v", ents, err)
		}
	})
}

// 删服记录坏了（写到一半、没有 removals 这一项）：启动时 cart.json 照常读、照常存，记录原样留着，
// 程序日志里说一句；不当成没有记录删掉它。
func TestBrokenRemovalsFileLeftAlone(t *testing.T) {
	for name, body := range map[string]string{"写到一半": `{"version":1,"removals":[{"inst":"旧服"`, "没有 removals": `{"version":1}`} {
		t.Run(name, func(t *testing.T) {
			c := seedForget(t)
			if err := os.WriteFile(c.removalsPath(), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logs)
			s := New(c.dir, c.mr.srv.URL)
			log.SetOutput(prev)
			if err := s.LoadErr(); err != nil {
				t.Fatalf("删服记录坏了不该挡 cart.json：%v", err)
			}
			if !strings.Contains(logs.String(), removalsName) || !strings.Contains(logs.String(), "下次再试") {
				t.Fatalf("程序日志里应说删服记录读不出来：%q", logs.String())
			}
			if got := s.List(oldInst); len(got) != 1 {
				t.Fatalf("清单照常读出来：%+v", got)
			}
			if _, _, err := s.Add(context.Background(), "别的服", []string{"aaa"}); err != nil {
				t.Fatalf("清单照常能存：%v", err)
			}
			if readText(t, c.removalsPath()) != body {
				t.Fatal("坏的删服记录应原样留着")
			}
		})
	}
}

// 删服记录里键是草稿清单、工具箱清单的（坏记录、手改的）不理：不能把草稿清单和工具箱的清单清了。
func TestRemovalsIgnoreDraftAndToolbox(t *testing.T) {
	c := seedForget(t)
	s := New(c.dir, c.mr.srv.URL)
	if _, _, err := s.Add(context.Background(), Toolbox, []string{"aaa"}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	b, _ := json.Marshal(removalsFile{Version: 1, Removals: []removal{{Inst: Draft, At: future}, {Inst: Toolbox, At: future}}})
	if err := os.WriteFile(c.removalsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := New(c.dir, c.mr.srv.URL)
	if len(s2.List(Draft)) != 1 || len(s2.List(Toolbox)) != 1 || s2.DraftInst() != oldInst {
		t.Fatalf("草稿清单、工具箱清单和记号都不该动：%+v %+v %q", s2.List(Draft), s2.List(Toolbox), s2.DraftInst())
	}
	if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
		t.Fatalf("处理过的删服记录应删掉：%v", err)
	}
}

// 读得出来时删服：当场清掉，不留删服记录；记号指着别的服务器时不动。
func TestForgetInstanceWhenReadable(t *testing.T) {
	c := seedForget(t)
	s := New(c.dir, c.mr.srv.URL)
	if err := s.ForgetInstance("别的服"); err != nil {
		t.Fatal(err)
	}
	if len(s.List("别的服")) != 0 || s.DraftInst() != oldInst {
		t.Fatalf("删别的服：清它的清单、不动指着旧服的记号：%+v %q", s.List("别的服"), s.DraftInst())
	}
	if err := s.ForgetInstance(oldInst); err != nil {
		t.Fatal(err)
	}
	if len(s.List(oldInst)) != 0 || len(s.Locals(oldInst)) != 0 || s.DraftInst() != "" || len(s.List(Draft)) != 1 {
		t.Fatalf("删旧服：清它的清单和记号、草稿清单留着：%+v %+v %q %+v", s.List(oldInst), s.Locals(oldInst), s.DraftInst(), s.List(Draft))
	}
	if _, err := os.Stat(filepath.Dir(c.staged)); !os.IsNotExist(err) {
		t.Fatalf("暂存的本地文件应删掉：%v", err)
	}
	if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
		t.Fatalf("读得出来时不该留删服记录：%v", err)
	}
}
