package ai

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const (
	commitOldKey = "sk-old-0123456789abcdef"
	commitNewKey = "sk-new-0123456789abcdef"
)

// twoSites 两个假的服务商（端口不同，算两个网站），都把每个请求当成取模型列表来回。
func twoSites(t *testing.T) (oldSrv, newSrv *fakeServer) {
	reply := func(_ int, _ recorded, w http.ResponseWriter) { serve(t, w, "openai/models.json") }
	return newFake(t, reply), newFake(t, reply)
}

// 换服务商时设置和 Key 要么一起生效、要么都不生效（v3.1.0 正式版前检查 B-10）。以前先改内存再写盘：
// 写设置失败，或者设置存好了、新 Key 没存上，后面的请求就把旧 Key 发到新地址；后一种盘上也是这样，重启以后照旧。
// 每一种写盘失败都从「旧地址 + 旧 Key」开始；失败以后用现在的设置真的发一遍请求（助手和游戏内 AI 用的 Client、
// 测试连接、模型列表），再用同一个目录和同一处 Key 重新读一遍设置（重启以后）也发一遍：新地址不能收到旧 Key。
func TestProviderSwitchAllOrNothing(t *testing.T) {
	ctx := context.Background()
	oldSrv, newSrv := twoSites(t)
	oldURL, newURL := oldSrv.srv.URL, newSrv.srv.URL
	dir := t.TempDir()
	keys := &memKeys{m: map[string]string{}}
	open := func() *Service {
		s := NewService(Options{Dir: dir, Keys: keys, HTTP: oldSrv.srv.Client()})
		s.retryDelay = time.Millisecond
		return s
	}
	s := open()
	block := filepath.Join(dir, "settings.json")
	saved := block + ".fault-backup"
	blocked := false
	unblock := func() {
		if !blocked {
			return
		}
		if err := os.Remove(block); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(saved, block); err != nil {
			t.Fatal(err)
		}
		blocked = false
	}
	t.Cleanup(unblock)
	blockSettings := func() { // 阻止替换真正的目标，不依赖临时文件怎样命名；保留原件供解除故障后验证。
		if err := os.Rename(block, saved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(block, 0o755); err != nil {
			t.Fatal(err)
		}
		blocked = true
	}

	// failOn 存这个值的时候失败（存之前先做 then）
	failOn := func(want string, then func()) func(string, string) error {
		return func(_, v string) error {
			if v != want {
				return nil
			}
			if then != nil {
				then()
			}
			return errors.New("写不进去")
		}
	}
	// probe 用这个 Service 现在的设置真的发请求；新地址收到旧 Key 就是配错了
	probe := func(name string, s *Service) {
		t.Helper()
		before := len(newSrv.requests())
		if cl, err := s.Client(false); err == nil {
			_, _ = cl.ListModels(ctx)
		}
		_, _ = s.Test(ctx)
		_, _ = s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: newURL})
		for _, r := range newSrv.requests()[before:] {
			if r.Header.Get("Authorization") == "Bearer "+commitOldKey {
				t.Errorf("%s：旧 Key 发到了新地址", name)
				return
			}
		}
	}
	for _, c := range []struct {
		name    string
		inject  func()
		patch   ProviderPatch
		wantURL string // 失败以后内存里、盘上的地址
		wantKey string // 失败以后存着的 Key
	}{
		{"写设置失败：换到别的网站，没给新 Key", blockSettings, ProviderPatch{BaseURL: strp(newURL)}, oldURL, commitOldKey},
		{"写设置失败：换到别的网站，给了新 Key", blockSettings, ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}, oldURL, commitOldKey},
		{"写设置失败：换成自定义服务商", blockSettings, ProviderPatch{Preset: strp(CustomID), BaseURL: strp(newURL), Model: strp("m")}, oldURL, commitOldKey},
		{"写设置失败：停用", blockSettings, ProviderPatch{Preset: strp("")}, oldURL, commitOldKey},
		{"写设置失败：只改模型（Key 不动）", blockSettings, ProviderPatch{Model: strp("m2")}, oldURL, commitOldKey},
		{"清旧 Key 失败", func() { keys.failSet = failOn("", nil) }, ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}, oldURL, commitOldKey},
		{"设置存好了，存新 Key 失败", func() { keys.failSet = failOn(commitNewKey, nil) }, ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}, oldURL, commitOldKey},
		// 写回旧设置也失败：盘上留着新设置，Key 空着（要重填），不配错
		{"存新 Key 失败，写回旧设置也失败", func() { keys.failSet = failOn(commitNewKey, blockSettings) }, ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}, newURL, ""},
	} {
		keys.failSet = nil
		unblock()
		if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), BaseURL: strp(oldURL), Model: strp("m"), Key: strp(commitOldKey)}); err != nil {
			t.Fatalf("%s：准备旧设置：%v", c.name, err)
		}
		c.inject()
		if err := s.SetProvider(c.patch); err == nil {
			t.Errorf("%s：应报错", c.name)
		}
		keys.failSet = nil
		if v := s.View(); v.Provider.BaseURL != c.wantURL || keys.m[KeyName] != c.wantKey {
			t.Errorf("%s：失败以后应是 %s + %q，实际是 %s + %q", c.name, c.wantURL, c.wantKey, v.Provider.BaseURL, keys.m[KeyName])
		}
		if v := s.View(); c.wantURL == oldURL && (v.Provider.Preset != "deepseek" || v.Provider.Model != "m") {
			t.Errorf("%s：没存上的改动不该生效：%+v", c.name, v.Provider)
		}
		probe(c.name, s)
		unblock()
		re := open() // 重启以后：同一个目录、同一处 Key
		if v := re.View(); v.Provider.BaseURL != c.wantURL {
			t.Errorf("%s：重启以后盘上的地址是 %s，应为 %s", c.name, v.Provider.BaseURL, c.wantURL)
		}
		probe(c.name+"（重启以后）", re)
	}

	// 没有写盘失败时照常切换：新地址配新 Key
	keys.failSet = nil
	if err := s.SetProvider(ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}); err != nil {
		t.Fatal(err)
	}
	if v := s.View(); v.Provider.BaseURL != newURL || keys.m[KeyName] != commitNewKey || !v.Enabled {
		t.Errorf("正常切换：%+v %q", v.Provider, keys.m[KeyName])
	}
	n := len(newSrv.requests())
	if _, err := s.Models(ctx, ModelsRequest{Preset: "deepseek", BaseURL: newURL}); err != nil || len(newSrv.requests()) != n+1 ||
		newSrv.requests()[n].Header.Get("Authorization") != "Bearer "+commitNewKey {
		t.Errorf("正常切换以后新地址应收到新 Key：%v", err)
	}
}

// 读的一方也要一起取设置和 Key：以前先取设置、再取 Key，两步之间正好换完服务商的话，拿到的是旧地址配新 Key。
// 这里把读 Key 的那一步卡住，趁这时去换服务商，看读到的是不是一对。
func TestClientReadsSettingsAndKeyTogether(t *testing.T) {
	oldSrv, newSrv := twoSites(t)
	oldURL, newURL := oldSrv.srv.URL, newSrv.srv.URL
	keys := &memKeys{m: map[string]string{}}
	s := NewService(Options{Dir: t.TempDir(), Keys: keys, HTTP: oldSrv.srv.Client()})
	if err := s.SetProvider(ProviderPatch{Preset: strp("deepseek"), BaseURL: strp(oldURL), Model: strp("m"), Key: strp(commitOldKey)}); err != nil {
		t.Fatal(err)
	}
	inGet, switched := make(chan struct{}), make(chan struct{})
	var fired atomic.Bool
	keys.beforeGet = func() {
		if !fired.CompareAndSwap(false, true) {
			return // 只卡住第一次读 Key（下面那个 Client）
		}
		close(inGet)
		select { // 读的一方拿着锁的话，换服务商要等它读完，所以这里不能一直等
		case <-switched:
		case <-time.After(300 * time.Millisecond):
		}
	}
	type pair struct{ url, key string }
	disabled := pair{"未启用", ""}
	got := make(chan pair, 1)
	go func() {
		cl, err := s.Client(false)
		if errors.Is(err, ErrDisabled) {
			// 读完设置和 Key 以后还要看一眼配没配好：那时旧 Key 正好清掉、新 Key 还没存好的话，当作还没配好，
			// 不发请求，也不会配错
			got <- disabled
			return
		}
		if err != nil {
			got <- pair{"出错：" + err.Error(), ""}
			return
		}
		got <- pair{cl.cfg.BaseURL, cl.cfg.Key}
	}()
	<-inGet
	done := make(chan error, 1)
	go func() { done <- s.SetProvider(ProviderPatch{BaseURL: strp(newURL), Key: strp(commitNewKey)}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("换服务商卡住了")
	}
	close(switched)
	if p := <-got; p != (pair{oldURL, commitOldKey}) && p != (pair{newURL, commitNewKey}) && p != disabled {
		t.Errorf("读到的地址和 Key 不是一对：%+v", p)
	}
	// 换完以后照常可用：新地址配新 Key
	if cl, err := s.Client(false); err != nil || cl.cfg.BaseURL != newURL || cl.cfg.Key != commitNewKey {
		t.Errorf("换完以后应是新地址配新 Key：%v", err)
	}
}
