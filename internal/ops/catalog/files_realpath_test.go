package catalog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// junction 在 link 建一个指向 target 的目录联接（mklink /J 不用管理员权限），测试结束时只删联接本身，不动它指的文件夹。
// 要在 t.TempDir() 之后调：清理按注册的倒序做，联接先删，临时目录后删。
func junction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

// refused err 是不是按 400 拒绝、话里有 want（ops.CodeOf(nil) 也是 400，所以先看 err 是不是 nil）。
func refused(err error, want string) bool {
	return err != nil && ops.CodeOf(err) == 400 && strings.Contains(err.Error(), want)
}

// names 一个文件夹（含子文件夹）里的全部条目，相对路径、排好序。
func names(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// 服务器文件夹里有指到外面的目录联接时，读写配置都不能顺着它跑出去（v3.1.0 正式版前检查 B-9）。
// 以前解析真实路径这一步出错就跳过检查：经过目录联接的路径解析必然出错（Go 1.23 起 EvalSymlinks 不解开联接），
// 新建的文件还不存在也必然出错，于是照常写到了外面。人在界面上直接改、AI 提案两条路都要拦住。
func TestConfigPathRejectsJunctionOutside(t *testing.T) {
	reg, _, dir := newModCatalog(t)
	ctx := context.Background()
	outside := t.TempDir() // 服务器文件夹外面的一个文件夹，里面放一份「别人的」配置
	victim := filepath.Join(outside, "victim.toml")
	if err := os.WriteFile(victim, []byte("a = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dir, "config", "linked"), outside) // config 下面的一个联接
	junction(t, filepath.Join(dir, "defaultconfigs"), outside)   // 整个配置根就是联接
	before := names(t, outside)

	for _, args := range []string{
		`{"inst":"整合包","path":"config/linked/new.toml","mode":"replace","content":"x = 1\n"}`,     // 新建：文件还不存在，上层是联接
		`{"inst":"整合包","path":"config/linked/sub/new.toml","mode":"replace","content":"x = 1\n"}`, // 联接下面再带一层不存在的子目录
		`{"inst":"整合包","path":"defaultconfigs/new.toml","mode":"replace","content":"x = 1\n"}`,
		`{"inst":"整合包","path":"config/linked/victim.toml","edits":[{"find":"a = 1","replace":"a = 2"}]}`,
		`{"inst":"整合包","path":"defaultconfigs/victim.toml","mode":"replace","content":"a = 3\n"}`,
		`{"inst":"整合包","path":"config/linked/victim.toml","mode":"reset"}`,
		`{"inst":"整合包","path":"config/linked/victim.toml","mode":"undo"}`,
	} {
		for _, c := range []ops.Caller{human, bot} { // human 直接执行；bot 先生成提案，生成时就该被拒
			if _, err := reg.Do(ctx, c, "instance.config.write", ops.Args(args)); !refused(err, "外面") {
				t.Errorf("%s 经目录联接写到外面（%s）应拒绝：%v", args, c.Source, err)
			}
		}
	}
	for _, p := range []string{"config/linked/victim.toml", "config/linked", "defaultconfigs/victim.toml", "defaultconfigs"} {
		if _, err := reg.Do(ctx, bot, "instance.config.read", ops.Args(`{"inst":"整合包","path":`+jsonStr(p)+`}`)); !refused(err, "外面") {
			t.Errorf("经目录联接读 %s 应拒绝：%v", p, err)
		}
	}
	if after := names(t, outside); !slices.Equal(before, after) {
		t.Errorf("外面的文件夹被动过：%v → %v", before, after)
	}
	if b, _ := os.ReadFile(victim); string(b) != "a = 1\n" {
		t.Errorf("外面的文件被改了：%q", b)
	}

	// 提案生成时路径还是好的，确认之前被换成了指到外面的联接：确认时也要拦住
	res, err := reg.Do(ctx, bot, "instance.config.write", ops.Args(`{"inst":"整合包","path":"config/discord/bot.toml","edits":[{"find":"general","replace":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "config", "discord")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "bot.toml"), []byte("token = \"t\"\nchannel = \"general\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dir, "config", "discord"), outside)
	if _, err := reg.Confirm(ctx, human, res.(*ops.Proposal).ID, false); err == nil {
		t.Error("确认前路径被换成指到外面的联接，确认时应拒绝")
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "bot.toml")); !strings.Contains(string(b), "general") {
		t.Errorf("外面的文件被改了：%q", b)
	}
}

// 放备份的 .minedeck 被换成指到外面的联接：改配置之前要先备份，备份会写到外面，清理旧备份、撤销还会删外面的文件，也要拦住。
func TestConfigWriteRejectsBackupDirOutside(t *testing.T) {
	reg, _, dir := newModCatalog(t)
	outside := t.TempDir()
	junction(t, filepath.Join(dir, ".minedeck"), outside)
	path := filepath.Join(dir, "config", "create-common.toml")
	orig, _ := os.ReadFile(path)
	for _, c := range []ops.Caller{human, bot} {
		_, err := reg.Do(context.Background(), c, "instance.config.write", ops.Args(`{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"maxSpeed = 40","replace":"maxSpeed = 60"}]}`))
		if !refused(err, "备份") {
			t.Errorf("备份文件夹指到外面时应拒绝（%s）：%v", c.Source, err)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != string(orig) {
		t.Errorf("配置不该改：%q", b)
	}
	if got := names(t, outside); len(got) != 0 {
		t.Errorf("备份写到了外面：%v", got)
	}
}

// 正常的读写不受影响：指回服务器文件夹里面的联接照常读写；服务器文件夹本身经联接登记（存放目录是个联接）也照常；
// 路径超过 MAX_PATH 的照常（解析真实路径时打不开长路径的话，就会把在的文件当成查不清）；读不存在的文件还是 404。
func TestConfigPathJunctionsInsideStillWork(t *testing.T) {
	reg, _, dir := newModCatalog(t)
	ctx := context.Background()
	run := func(reg *ops.Registry, args string) error {
		t.Helper()
		_, err := reg.Do(ctx, human, "instance.config.write", ops.Args(args))
		return err
	}
	junction(t, filepath.Join(dir, "config", "alias"), filepath.Join(dir, "config", "discord"))
	if err := run(reg, `{"inst":"整合包","path":"config/alias/bot.toml","edits":[{"find":"general","replace":"ops"}]}`); err != nil {
		t.Fatalf("指回服务器文件夹里面的联接应照常写：%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "discord", "bot.toml")); !strings.Contains(string(b), `channel = "ops"`) {
		t.Errorf("没写进去：%q", b)
	}
	if err := run(reg, `{"inst":"整合包","path":"config/alias/new.toml","mode":"replace","content":"n = 1\n"}`); err != nil {
		t.Fatalf("经里面的联接新建应照常：%v", err)
	}
	if _, err := reg.Do(ctx, bot, "instance.config.read", ops.Args(`{"inst":"整合包","path":"config/nope.toml"}`)); err == nil || ops.CodeOf(err) != 404 {
		t.Errorf("读不存在的文件应是 404：%v", err)
	}

	// 很长的路径（超过 MAX_PATH）：照常新建、修改（备份的文件名也还在 255 个字以内）
	seg := func(c string) string { return strings.Repeat(c, 70) }
	long := "config/" + seg("a") + "/" + seg("b") + "/" + seg("c") + "/x.toml"
	if n := len(filepath.Join(dir, filepath.FromSlash(long))); n <= 260 {
		t.Fatalf("路径只有 %d 个字符，没超过 MAX_PATH，测不到长路径", n)
	}
	if err := run(reg, `{"inst":"整合包","path":"`+long+`","mode":"replace","content":"v = 1\n"}`); err != nil {
		t.Fatalf("长路径新建：%v", err)
	}
	if err := run(reg, `{"inst":"整合包","path":"`+long+`","edits":[{"find":"v = 1","replace":"v = 2"}]}`); err != nil {
		t.Fatalf("长路径修改：%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(long))); string(b) != "v = 2\n" {
		t.Errorf("长路径的内容：%q", b)
	}

	// 存放目录是个联接：服务器文件夹按联接的路径登记，真实路径在别处，读写照常
	link := filepath.Join(app.Base, "servers-link")
	junction(t, link, app.ServersDir)
	old := app.ServersDir
	app.ServersDir = link
	t.Cleanup(func() { app.ServersDir = old })
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i, err := mgr.Get("整合包"); err != nil || !strings.HasPrefix(i.Dir, link) {
		t.Fatalf("服务器文件夹应按联接的路径登记：%v", err)
	}
	reg2 := New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Port: 27333})
	if err := run(reg2, `{"inst":"整合包","path":"config/create-common.toml","edits":[{"find":"maxSpeed = 40","replace":"maxSpeed = 50"}]}`); err != nil {
		t.Fatalf("经联接登记的服务器文件夹应照常写：%v", err)
	}
	if err := run(reg2, `{"inst":"整合包","path":"config/brand-new.toml","mode":"replace","content":"b = 1\n"}`); err != nil {
		t.Fatalf("经联接登记的服务器文件夹应照常新建：%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "create-common.toml")); !strings.Contains(string(b), "maxSpeed = 50") {
		t.Errorf("没写进去：%q", b)
	}
}
