package inst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

func TestCreatingLeftoverIgnoresAdoptedAndRegistered(t *testing.T) {
	m, user := adoptEnv(t)
	if err := os.MkdirAll(app.ServersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	half := filepath.Join(app.ServersDir, "half")
	writeTree(t, half, map[string]string{CreatingMarker: "half\n", "world/level.dat": "x", "mods/a.jar": "a"})
	adopted := filepath.Join(user, "已有服")
	writeTree(t, adopted, map[string]string{"server.jar": "x", "world/level.dat": "w"})
	real := filepath.Join(app.ServersDir, "real")
	writeTree(t, real, map[string]string{CreatingMarker: "real\n", "instance.json": `{"name":"real"}`, "world/level.dat": "w"})
	plain := filepath.Join(app.ServersDir, "plain")
	writeTree(t, plain, map[string]string{"server.jar": "x", "world/level.dat": "w"})

	list := m.CreatingLeftovers()
	if len(list) != 1 || list[0].Name != "half" || !list[0].HasWorld {
		t.Fatalf("只该列出没登记的半成品：%+v", list)
	}
	if _, err := os.Lstat(filepath.Join(real, CreatingMarker)); !os.IsNotExist(err) {
		t.Fatalf("已登记的服务器应只去掉记号：%v", err)
	}
	if _, err := os.Lstat(filepath.Join(real, "instance.json")); err != nil {
		t.Fatalf("已登记的服务器被动过：%v", err)
	}
	if _, err := os.Lstat(filepath.Join(plain, "world", "level.dat")); err != nil {
		t.Fatalf("没有记号的目录不应被扫掉：%v", err)
	}
	if _, err := os.Lstat(filepath.Join(adopted, "world", "level.dat")); err != nil {
		t.Fatalf("接管目录不应被扫掉：%v", err)
	}

	if err := m.DiscardCreatingLeftover(list[0].ID, false); err == nil || !strings.Contains(err.Error(), "确认") {
		t.Fatalf("没确认应拒绝：%v", err)
	}
	if _, err := os.Lstat(half); err != nil {
		t.Fatalf("拒绝以后文件夹还应在：%v", err)
	}
	if err := m.DiscardCreatingLeftover(list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(half); !os.IsNotExist(err) {
		t.Fatalf("确认后应删掉半成品：%v", err)
	}
	if _, err := os.Lstat(filepath.Join(plain, "server.jar")); err != nil {
		t.Fatalf("旁边的目录不应被删：%v", err)
	}
	if err := m.DiscardCreatingLeftover("../"+filepath.Base(user), true); err == nil {
		t.Fatal("不应按任意路径删除")
	}
}

func TestCreatingLeftoverSkipsBusyAndLink(t *testing.T) {
	m, _ := adoptEnv(t)
	root := app.ServersDir
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	busy := filepath.Join(root, "busy")
	writeTree(t, busy, map[string]string{CreatingMarker: "busy\n"})
	m.creating[dirKey(busy)] = true
	if list := m.CreatingLeftovers(); len(list) != 0 {
		t.Fatalf("正在创建的不应列出来：%+v", list)
	}
	if err := m.DiscardCreatingLeftover("0/busy", true); err == nil || !strings.Contains(err.Error(), "正在") {
		t.Fatalf("正在创建的应拒绝删除：%v", err)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("这个环境建不了符号链接：" + err.Error())
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Skip("这个环境的符号链接读不出来（Wine 常见）")
	}
	if err := os.WriteFile(filepath.Join(outside, CreatingMarker), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if list := m.CreatingLeftovers(); len(list) != 0 {
		t.Fatalf("链接不应列出来：%+v", list)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("链接目标被动过：%v %q", err, b)
	}
}
