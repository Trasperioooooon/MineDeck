package modcart

import (
	"os"
	"path/filepath"
	"testing"
)

// cart.json 被别的程序独占着（杀毒、同步盘最常见）时删服：记下来；放开以后下一次用清单就补清。
// 补清那一刻删服记录也被占着：先留着，cart.json 照常用；放开以后下次启动再补。
func TestForgetInstanceWhileLockedSettlesWhenReleased(t *testing.T) {
	c := seedForget(t)
	unlockCart := lockExclusive(t, c.cartPath())
	s := New(c.dir, c.mr.srv.URL)
	wantLoadError(t, s.LoadErr(), false)
	if err := s.ForgetInstance(oldInst); err != nil {
		t.Fatalf("占着时删服应记下来、不报错：%v", err)
	}
	unlockRecord := lockExclusive(t, c.removalsPath())
	unlockCart()
	if err := s.LoadErr(); err != nil {
		t.Fatalf("删服记录被占着不该挡 cart.json：%v", err)
	}
	if got := s.List(oldInst); len(got) != 1 {
		t.Fatalf("删服记录读不出来时先不清：%+v", got)
	}
	unlockRecord()
	s2 := New(c.dir, c.mr.srv.URL)
	if got := s2.List(oldInst); len(got) != 0 || len(s2.Locals(oldInst)) != 0 || s2.DraftInst() != "" {
		t.Fatalf("放开以后重开应补清：%+v %+v %q", got, s2.Locals(oldInst), s2.DraftInst())
	}
	if _, err := os.Stat(filepath.Dir(c.staged)); !os.IsNotExist(err) {
		t.Fatalf("暂存的本地文件应删掉：%v", err)
	}
	if _, err := os.Stat(c.removalsPath()); !os.IsNotExist(err) {
		t.Fatalf("补清以后删服记录应删掉：%v", err)
	}
}
