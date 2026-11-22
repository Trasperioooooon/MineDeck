package inst

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
	// 父目录故意放一份记号：Windows 下末尾带空格的 ".. " 会被 Win32 去掉尾空格解到父目录，leftoverDir 必须在路径校验层直接拦住。
	parent := filepath.Dir(app.ServersDir)
	if err := os.WriteFile(filepath.Join(parent, CreatingMarker), []byte("parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside-keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, badID := range []string{"../" + filepath.Base(user), "0/.", "0/..", "0/.. ", "0/...", "0/CON.txt", "0/foo:bar"} {
		if err := m.DiscardCreatingLeftover(badID, true); err == nil || !strings.Contains(err.Error(), "没有这一项") {
			t.Fatalf("非法编号 %q 应在路径校验层直接拒绝：%v", badID, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(parent, "outside-keep.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("父目录被动过：%v %q", err, b)
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
		if os.Getenv("CI") == "" && runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skip("这个环境建不了符号链接：" + err.Error())
		}
		t.Fatalf("建符号链接失败：%v", err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		if os.Getenv("CI") == "" {
			t.Skip("这个环境的符号链接读不出来（Wine 常见）")
		}
		t.Fatalf("符号链接读不出来：%v %v", st, err)
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

func TestCreatingLeftoverRefusesJunctionAndNestedJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接仅在 Windows 上存在")
	}
	m, _ := adoptEnv(t)
	root := app.ServersDir
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	writeTree(t, outside, map[string]string{
		CreatingMarker:  "ext\n",
		"instance.json": `{"name":"ext"}`,
		"keep.txt":      "keep",
	})

	// 1. 顶层是目录联接：既不列出，也不穿透去删目标目录里的记号或文件
	junc := filepath.Join(root, "junc")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junc, outside).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junc) })
	if list := m.CreatingLeftovers(); len(list) != 0 {
		t.Fatalf("目录联接不应列入半成品：%+v", list)
	}
	if _, err := os.Lstat(filepath.Join(outside, CreatingMarker)); err != nil {
		t.Fatalf("不应穿透目录联接改动外部目录的记号：%v", err)
	}
	_ = os.Remove(filepath.Join(outside, "instance.json"))
	if err := m.DiscardCreatingLeftover("0/junc", true); err == nil || !strings.Contains(err.Error(), "链接") {
		t.Fatalf("目录联接应拒绝删除：%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("联接目标被动过：%v %q", err, b)
	}
	_ = os.Remove(junc)

	// 2. 半成品目录内部含有指向外面的目录联接：拒绝删除
	half := filepath.Join(root, "half-nested")
	writeTree(t, half, map[string]string{CreatingMarker: "half-nested\n"})
	nested := filepath.Join(half, "mods")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", nested, outside).CombinedOutput(); err != nil {
		t.Fatalf("建子联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(nested) })
	if err := m.DiscardCreatingLeftover("0/half-nested", true); err == nil || !strings.Contains(err.Error(), "目录联接") {
		t.Fatalf("含子目录联接应拒绝删除：%v", err)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("子联接目标被动过：%v %q", err, b)
	}
}

func TestCreatingLeftoverProtectsRemovedBackupAndCustomWorld(t *testing.T) {
	m, _ := adoptEnv(t)
	root := app.ServersDir
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// 移出列表留下的 instance.json.bak（假设当初没清掉创建记号）：绝不能当半成品列出或删掉
	kept := filepath.Join(root, "kept-srv")
	writeTree(t, kept, map[string]string{
		CreatingMarker:    "kept-srv\n",
		settingsBackup:    `{"name":"kept-srv"}`,
		"world/level.dat": "precious",
	})
	// 正在原子落盘的 instance.json.tmp：也不能列出或删掉
	writing := filepath.Join(root, "writing-srv")
	writeTree(t, writing, map[string]string{
		CreatingMarker:      "writing-srv\n",
		"instance.json.tmp": `{"name":"writing-srv"}`,
	})
	// 内存里已登记的实例（即使磁盘上 instance.json 暂时不在）：不能列出或删掉
	inMem := filepath.Join(root, "in-mem")
	writeTree(t, inMem, map[string]string{CreatingMarker: "in-mem\n"})
	m.insts["in-mem"] = &Instance{Settings: Settings{Name: "in-mem"}, Dir: inMem, Console: newConsole()}

	// 自定义 level-name 的半成品：HasWorld 应识别为 true
	custom := filepath.Join(root, "custom-world")
	writeTree(t, custom, map[string]string{
		CreatingMarker:          "custom-world\n",
		"server.properties":     "level-name=worlds/main\n",
		"worlds/main/level.dat": "custom-save",
	})

	list := m.CreatingLeftovers()
	if len(list) != 1 || list[0].Name != "custom-world" || !list[0].HasWorld {
		t.Fatalf("应只列出 custom-world 且认出自定义世界：%+v", list)
	}
	for _, name := range []string{"kept-srv", "writing-srv", "in-mem"} {
		if err := m.DiscardCreatingLeftover("0/"+name, true); err == nil {
			t.Fatalf("%s 不应被允许删除", name)
		}
	}
	if b, err := os.ReadFile(filepath.Join(kept, "world", "level.dat")); err != nil || string(b) != "precious" {
		t.Fatalf("移出列表的服务器文件被动过：%v %q", err, b)
	}
}
