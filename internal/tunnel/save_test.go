package tunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"minedeck/internal/app"
	"minedeck/internal/secret"
)

// 这一组测的是 tunnels.json 写不进去的时候：以前 save 吞掉写盘错误，新增、导入照样报成功，重启以后隧道没了。
// 现在新增、修改、删除写不进去就报错，内存里退回原样；公网地址存不进去时内存照样换、控制台里说一声。

// blockSave 让之后的保存都写不进去：程序目录换成一个普通文件底下的路径，临时文件建不出来。
// m.file() 每次按 app.Base 现算，换掉它就够了。返回还原的函数（tempBase 的清理也会还原）。
func blockSave(t *testing.T) (restore func()) {
	t.Helper()
	good := app.Base
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Base = filepath.Join(blocker, "sub")
	return func() { app.Base = good }
}

func openfrpTunnel(name string) Tunnel {
	return Tunnel{Provider: "openfrp", Name: name, Credential: "openfrp-key-" + name, ProxyID: "42"}
}

func tunnelIDs(m *Manager) []string {
	var ids []string
	for _, st := range m.List() {
		ids = append(ids, st.ID)
	}
	return ids
}

func wantSaveError(t *testing.T, err error) {
	t.Helper()
	var se *SaveError
	if !errors.As(err, &se) {
		t.Fatalf("写不进磁盘应报 SaveError，实际：%v", err)
	}
}

// 新增：报错，内存里没有多出这条，编号也退回去；磁盘上的文件一字不变；恢复以后再加，编号接着原来的用，重启后都在。
func TestAddFailsAndRollsBackWhenSaveFails(t *testing.T) {
	tempBase(t)
	m := NewManager()
	if _, err := m.Add(openfrpTunnel("一号")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(app.Base, "tunnels.json")
	before := readFile(t, path)

	restore := blockSave(t)
	got, err := m.Add(openfrpTunnel("二号"))
	wantSaveError(t, err)
	if got != nil {
		t.Fatalf("失败时不该返回隧道：%+v", got)
	}
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1"}) {
		t.Fatalf("写盘失败后内存里多了隧道：%v", ids)
	}
	if m.seq != 1 {
		t.Fatalf("编号没退回去：seq=%d", m.seq)
	}
	restore()

	if after := readFile(t, path); after != before {
		t.Fatalf("写盘失败不该动原文件：\n前：%s\n后：%s", before, after)
	}
	a, err := m.Add(openfrpTunnel("二号"))
	if err != nil || a.ID != "tun2" {
		t.Fatalf("恢复以后再加应成功、编号是 tun2：%+v %v", a, err)
	}
	if ids := tunnelIDs(NewManager()); !slices.Equal(ids, []string{"tun1", "tun2"}) {
		t.Fatalf("重启后的隧道：%v", ids)
	}
}

// tunnels.json 被设成只读：照旧不覆盖（和以前的 os.WriteFile 一样写不进去），但现在报出来，也不留临时文件。
// 只读属性挡改名是 Windows 的行为；别的系统上改名不看目标文件的权限，跳过。
func TestAddFailsWhenTunnelsJSONReadOnly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读属性挡住覆盖是 Windows 的行为")
	}
	tempBase(t)
	m := NewManager()
	if _, err := m.Add(openfrpTunnel("一号")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(app.Base, "tunnels.json")
	before := readFile(t, path)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := m.Add(openfrpTunnel("二号"))
	wantSaveError(t, err)
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1"}) {
		t.Fatalf("写盘失败后内存里多了隧道：%v", ids)
	}
	if after := readFile(t, path); after != before {
		t.Fatal("只读的 tunnels.json 被改了")
	}
	ents, _ := os.ReadDir(app.Base)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("留下了临时文件：%s", e.Name())
		}
	}
}

// 修改：报错，隧道的内容原样退回；「凭据解不开」的标记也留着（换凭据、清凭据都要写进磁盘以后才算数）。
func TestUpdateFailsAndRollsBackWhenSaveFails(t *testing.T) {
	tempBase(t)
	m := NewManager()
	c, err := m.Add(Tunnel{Provider: "custom", Name: "自建", Credential: "old-token", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25566, LocalPort: 25565})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.broken[c.ID] = true // 假装凭据解不开（换了电脑）
	snap := *m.get(c.ID)
	m.mu.Unlock()

	restore := blockSave(t)
	changed := Tunnel{Provider: "custom", Name: "改了名", Credential: "new-token", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25567, LocalPort: 25565}
	wantSaveError(t, m.Update(c.ID, changed))
	wantSaveError(t, m.UpdateOpt(c.ID, Tunnel{Provider: "custom", Name: "清凭据", ServerAddr: "frp.example.com", ServerPort: 7000, RemotePort: 25567, LocalPort: 25565}, UpdateOpts{ClearCredential: true}))
	m.mu.Lock()
	now, broken := *m.get(c.ID), m.broken[c.ID]
	m.mu.Unlock()
	if now != snap {
		t.Fatalf("写盘失败后隧道被改了：\n前：%+v\n后：%+v", snap, now)
	}
	if !broken {
		t.Fatal("写盘失败不该清掉「凭据解不开」的标记")
	}
	restore()

	if err := m.Update(c.ID, changed); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	name, broken := m.get(c.ID).Name, m.broken[c.ID]
	cred := m.get(c.ID).Credential
	m.mu.Unlock()
	if name != "改了名" || broken {
		t.Fatalf("恢复以后修改应生效、标记清掉：name=%q broken=%v", name, broken)
	}
	if got, _ := secret.Unseal(cred); got != "new-token" {
		t.Fatalf("新凭据没存上：%q", got)
	}
}

// 删除：报错，隧道原样留在原位（删第一条、中间那条都试：原地挪动的写法会把列表弄成 [甲, 丙, 丙]），标记也留着。
func TestDeleteFailsAndRollsBackWhenSaveFails(t *testing.T) {
	tempBase(t)
	m := NewManager()
	for _, n := range []string{"甲", "乙", "丙"} {
		if _, err := m.Add(openfrpTunnel(n)); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	m.broken["tun2"] = true
	m.mu.Unlock()
	all := []string{"tun1", "tun2", "tun3"}

	restore := blockSave(t)
	for _, id := range []string{"tun2", "tun1"} {
		wantSaveError(t, m.Delete(id))
		if ids := tunnelIDs(m); !slices.Equal(ids, all) {
			t.Fatalf("删 %s 写盘失败后列表变了：%v", id, ids)
		}
	}
	m.mu.Lock()
	broken := m.broken["tun2"]
	m.mu.Unlock()
	if !broken {
		t.Fatal("写盘失败不该清掉「凭据解不开」的标记")
	}
	restore()

	if err := m.Delete("tun2"); err != nil {
		t.Fatal(err)
	}
	if ids := tunnelIDs(NewManager()); !slices.Equal(ids, []string{"tun1", "tun3"}) {
		t.Fatalf("恢复以后删除应生效：%v", ids)
	}
	// 删光了存成空列表 []，和以前一样，不是 null
	for _, id := range []string{"tun1", "tun3"} {
		if err := m.Delete(id); err != nil {
			t.Fatal(err)
		}
	}
	if disk := readFile(t, filepath.Join(app.Base, "tunnels.json")); !strings.Contains(disk, `"tunnels": []`) {
		t.Fatalf("删光以后应存成空列表：%s", disk)
	}
}

// 公网地址存不进去：内存里照样是新地址（隧道确实连上了），控制台里说一声，不记成卡片上的红字，也不卡住；
// 之后哪次保存成功，地址一起写进去。
func TestPublicAddrKeptAndReportedWhenSaveFails(t *testing.T) {
	tempBase(t)
	m := NewManager()
	a, err := m.Add(openfrpTunnel("一号"))
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	tun := m.get(a.ID)
	m.mu.Unlock()

	restore := blockSave(t)
	done := make(chan struct{})
	go func() {
		m.setPublicAddr(tun, "frp.example.com:12345")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("公网地址存盘失败时卡住了（锁没放就去拿控制台？）")
	}
	restore()

	var st Status
	for _, s := range m.List() {
		if s.ID == a.ID {
			st = s
		}
	}
	if st.PublicAddr != "frp.example.com:12345" {
		t.Fatalf("内存里应是新地址：%q", st.PublicAddr)
	}
	if st.LastError != "" {
		t.Fatalf("隧道是好的，卡片上不该有红字：%q", st.LastError)
	}
	lines := strings.Join(m.Console(a.ID).Lines(), "\n")
	if !strings.Contains(lines, "公网地址没能存进 tunnels.json") {
		t.Fatalf("控制台里没说存盘失败：%s", lines)
	}
	if strings.Contains(lines, "这次的改动没有保存") {
		t.Fatalf("地址留在内存里了，不该说「改动没有保存」：%s", lines)
	}
	if strings.Contains(readFile(t, filepath.Join(app.Base, "tunnels.json")), "12345") {
		t.Fatal("写不进去的时候文件里不该有新地址")
	}

	// 之后别的保存成功了，地址一起写进去
	if _, err := m.Add(openfrpTunnel("二号")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, filepath.Join(app.Base, "tunnels.json")), "frp.example.com:12345") {
		t.Fatal("下一次保存成功时应把地址一起写进去")
	}
}

// 旧版的明文凭据加密后写回失败：内存里照样是密文、能解开，程序照常起来；文件不动。只读属性挡改名是 Windows 的行为。
func TestLoadMigrationSurvivesSaveFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读属性挡住覆盖是 Windows 的行为")
	}
	tempBase(t)
	path := filepath.Join(app.Base, "tunnels.json")
	legacy := `{"seq":1,"tunnels":[{"id":"tun1","provider":"openfrp","name":"老隧道","credential":"plain-key-123","proxyId":"42","autoStart":false}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	m := NewManager()
	m.mu.Lock()
	cred := m.get("tun1").Credential
	m.mu.Unlock()
	if !secret.IsSealed(cred) {
		t.Fatalf("内存里应是密文：%q", cred)
	}
	if got, err := secret.Unseal(cred); err != nil || got != "plain-key-123" {
		t.Fatalf("密文解不开：%q %v", got, err)
	}
	if readFile(t, path) != legacy {
		t.Fatal("只读的 tunnels.json 被改了")
	}
}

// 报错提示接的那句话要守界面的规矩（app.js 的 errSummary）：60 字以内、有中文、不带「: 」和路径，不然界面不用它。
func TestSaveHintsFitErrorToast(t *testing.T) {
	for _, h := range []string{(&SaveError{Err: os.ErrPermission}).Hint(), (&createdNotSavedError{}).Hint()} {
		if n := utf8.RuneCountInString(h); n == 0 || n > 60 {
			t.Errorf("提示 %d 字，要在 1–60 字之间：%s", n, h)
		}
		if strings.Contains(h, ": ") || strings.ContainsAny(h, `\/`) {
			t.Errorf("提示里有「: 」或路径符号，界面不会用它：%s", h)
		}
	}
}

// 平台账户的导入和创建：写不进磁盘时报失败，本机列表里不多出隧道；创建的报错要说清平台上已经建好、去导入，
// 不然再点一次创建就在平台上多建一条。磁盘恢复以后，导入照常能成（查重没有被失败的那次挡住）。
func TestAccountImportAndCreateReportSaveFailure(t *testing.T) {
	f := newOpenFrpServer(t)
	a, _ := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)
	loginOpenFrp(t, a)
	ctx := context.Background()

	restore := blockSave(t)
	_, err := a.ImportTunnel(ctx, AccountTunnelImport{Provider: "openfrp", ProxyID: "7"})
	wantSaveError(t, err)
	if !strings.Contains(err.Error(), "保存到 MineDeck 失败") {
		t.Fatalf("导入失败的说法：%v", err)
	}
	if n := len(a.tunnels.List()); n != 0 {
		t.Fatalf("导入写盘失败后本机多了 %d 条隧道", n)
	}

	_, err = a.CreateTunnel(ctx, AccountTunnelOptions{
		Provider: "openfrp", Name: "minedeck_ab12", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002,
	})
	wantSaveError(t, err)
	if !f.created {
		t.Fatal("平台上应已建好（测的就是建好以后本机存不上）")
	}
	if !strings.Contains(err.Error(), "平台上的隧道已经建好") || !strings.Contains(err.Error(), "导入") {
		t.Fatalf("创建失败的说法要讲清平台上已建好、去导入：%v", err)
	}
	var h interface{ Hint() string }
	if !errors.As(err, &h) || !strings.Contains(h.Hint(), "导入") {
		t.Fatalf("创建失败的提示要说去导入：%v", err)
	}
	if n := len(a.tunnels.List()); n != 0 {
		t.Fatalf("创建写盘失败后本机多了 %d 条隧道", n)
	}
	restore()

	for _, id := range []string{"7", "8"} { // 8 是刚才在平台上建好、没存上的那条
		st, err := a.ImportTunnel(ctx, AccountTunnelImport{Provider: "openfrp", ProxyID: id})
		if err != nil || st.ProxyID != id {
			t.Fatalf("磁盘恢复以后导入 %s 应成功：%+v %v", id, st, err)
		}
	}
	if n := len(NewManager().List()); n != 2 {
		t.Fatalf("重启后应有 2 条隧道，实际 %d", n)
	}
}

// 樱花同理（创建、导入的前几步和 OpenFrp 不一样，写本机那一步是同一条路）。
func TestSakuraImportAndCreateReportSaveFailure(t *testing.T) {
	srv, lastCreate := sakuraServer(t, nil)
	a, _ := testAccounts(t, srv.URL, srv.URL, srv.URL)
	ctx := context.Background()
	if _, err := a.ConnectSakura(ctx, "sakura-access-key"); err != nil {
		t.Fatal(err)
	}

	restore := blockSave(t)
	_, err := a.ImportTunnel(ctx, AccountTunnelImport{Provider: "natfrp", ProxyID: "9"})
	wantSaveError(t, err)
	_, err = a.CreateTunnel(ctx, AccountTunnelOptions{Provider: "natfrp", Name: "mc", NodeID: 1, Type: "tcp", LocalPort: 25565})
	wantSaveError(t, err)
	if *lastCreate == nil {
		t.Fatal("樱花上应已建好（测的就是建好以后本机存不上）")
	}
	var h interface{ Hint() string }
	if !strings.Contains(err.Error(), "平台上的隧道已经建好") || !errors.As(err, &h) || !strings.Contains(h.Hint(), "导入") {
		t.Fatalf("创建失败的说法要讲清平台上已建好、去导入：%v", err)
	}
	if n := len(a.tunnels.List()); n != 0 {
		t.Fatalf("写盘失败后本机多了 %d 条隧道", n)
	}
	restore()

	if st, err := a.ImportTunnel(ctx, AccountTunnelImport{Provider: "natfrp", ProxyID: "9"}); err != nil || st.ProxyID != "9" {
		t.Fatalf("磁盘恢复以后导入应成功：%+v %v", st, err)
	}
}
