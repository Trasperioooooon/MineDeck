package inst

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/modpack"
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
	if _, err := os.Lstat(filepath.Join(real, CreatingMarker)); err != nil {
		t.Fatalf("列半成品只读，已登记的服务器里残留的记号留到下次启动再去掉：%v", err)
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

// 列半成品的接口只读，盘上的东西一样不动；已经登记成服务器（instance.json，或移出列表留下的 .bak）的文件夹里残留的
// 创建记号在下次启动时去掉，没登记的半成品的记号留着（A38）。
func TestStaleCreatingMarkerClearedAtStartup(t *testing.T) {
	m, _ := adoptEnv(t)
	real := filepath.Join(app.ServersDir, "real")
	writeTree(t, real, map[string]string{CreatingMarker: "real\n", "instance.json": `{"name":"real"}`})
	kept := filepath.Join(app.ServersDir, "kept")
	writeTree(t, kept, map[string]string{CreatingMarker: "kept\n", settingsBackup: `{"name":"kept"}`})
	half := filepath.Join(app.ServersDir, "half")
	writeTree(t, half, map[string]string{CreatingMarker: "half\n"})
	marker := func(dir string) bool {
		_, err := os.Lstat(filepath.Join(dir, CreatingMarker))
		return err == nil
	}

	if list := m.CreatingLeftovers(); len(list) != 1 || list[0].Name != "half" {
		t.Fatalf("只该列出没登记的半成品：%+v", list)
	}
	if !marker(real) || !marker(kept) || !marker(half) {
		t.Fatal("列半成品不该去掉任何记号")
	}
	if _, err := NewManager(); err != nil {
		t.Fatal(err)
	}
	if marker(real) || marker(kept) {
		t.Fatal("启动时应去掉已登记的服务器里残留的记号")
	}
	if !marker(half) {
		t.Fatal("没登记的半成品的记号要留着，下次还能列出来")
	}
	if got := mustRead(t, filepath.Join(real, "instance.json")); got != `{"name":"real"}` {
		t.Fatalf("去记号不该动服务器的配置：%q", got)
	}
}

// 第一次往一个新的存放位置里建服，记号放下以后、建完以前程序崩了：下次启动照样扫得到那里的半成品，能删；
// 删掉以后再重开，这个位置从「正在建的位置」里去掉（A38）。这个位置自始至终不是扫描根。
func TestCreatingLeftoverInNewRootFoundAfterCrash(t *testing.T) {
	adoptEnv(t)
	root := filepath.Join(t.TempDir(), "新存放位置")
	dir := filepath.Join(root, "half-new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(app.InstanceRoots(), root) {
		t.Fatal("这个存放位置一开始不该在配置里")
	}
	if err := WriteCreatingMarker(dir, "新位置的服"); err != nil {
		t.Fatal(err)
	}
	// 程序在这里没了：不走登记那一步，直接重开（重开两次：还有半成品的位置启动时不能被整理掉）
	if _, err := NewManager(); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	list := m2.CreatingLeftovers()
	if len(list) != 1 || list[0].Name != "half-new" || list[0].RootName != "新存放位置" {
		t.Fatalf("下次启动应扫得到新存放位置里的半成品：%+v", list)
	}
	if slices.Contains(app.InstanceRoots(), root) {
		t.Fatalf("没建成的位置不该成为扫描根：%v", app.InstanceRoots())
	}
	if err := m2.DiscardCreatingLeftover(list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("半成品应删掉：%v", err)
	}
	if _, err := NewManager(); err != nil {
		t.Fatal(err)
	}
	if got := app.CreatingRoots(); slices.Contains(got, root) {
		t.Fatalf("半成品删掉以后重开，这个位置应从「正在建的位置」里去掉：%v", got)
	}
}

// 人挑了一个新的存放位置，里面本来就有一个带 instance.json 的文件夹（别处留下的服务器）。导入现成服务端在放了记号以后失败、
// 半成品清掉了：重开以后那台不进列表，这个位置不是扫描根，也从「正在建的位置」里去掉了（A38）。
func TestFailedImportInNewRootKeepsNeighbourOut(t *testing.T) {
	m, _ := importEnv(t)
	root := filepath.Join(t.TempDir(), "人挑的存放位置")
	neighbour := filepath.Join(root, "old-srv")
	writeTree(t, neighbour, map[string]string{
		"instance.json": `{"name":"别处留下的服","core":"paper","mc":"1.20.1","xmxMb":1024,"xmsMb":512}`,
	})
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"server.properties":    "level-name=../../outside\n", // 解压以后才查出来：导入在放了记号以后失败
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"eula.txt":             "eula=true\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.importServerAsync(pack, CreateReq{Name: "新服", XmxMB: 2048, EULA: true, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if s := waitImportTask(t, m, id); !strings.Contains(s.Err, "世界文件夹") {
		t.Fatalf("应因为世界文件夹指到外面而失败：%+v", s)
	}
	if ents, _ := os.ReadDir(root); len(ents) != 1 {
		t.Fatalf("失败以后这个位置里只该剩原来那个文件夹：%v", ents)
	}
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Get("别处留下的服"); err == nil {
		t.Fatal("导入失败以后，这个位置里别处留下的服务器不该进列表")
	}
	if roots := app.InstanceRoots(); slices.Contains(roots, root) {
		t.Fatalf("导入失败的位置不该成为扫描根：%v", roots)
	}
	if got := app.CreatingRoots(); slices.Contains(got, root) {
		t.Fatalf("没有半成品了，重开以后应从「正在建的位置」里去掉：%v", got)
	}
	if b := mustRead(t, filepath.Join(neighbour, "instance.json")); !strings.Contains(b, "别处留下的服") {
		t.Fatalf("旁边那台的配置不该被动过：%s", b)
	}
}

// 往新的存放位置里建成了：这个位置成了扫描根，重开能列出这台，「正在建的位置」里随之去掉（A38）。
func TestCreatedInNewRootBecomesScanRoot(t *testing.T) {
	m, _ := importEnv(t)
	root := filepath.Join(t.TempDir(), "新存放位置")
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"eula.txt":             "eula=true\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "新位置的服", XmxMB: 2048, Port: 25573, EULA: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, filepath.Join(root, "srv")); err != nil {
		t.Fatalf("导入失败: %v\n日志: %v", err, tk.Snapshot().Log)
	}
	if roots := app.InstanceRoots(); !slices.Contains(roots, root) {
		t.Fatalf("建成以后这个位置应成为扫描根：%v", roots)
	}
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Get("新位置的服"); err != nil {
		t.Fatalf("重开以后应列出建成的服务器：%v", err)
	}
	if got := app.CreatingRoots(); slices.Contains(got, root) {
		t.Fatalf("已经是扫描根了，重开以后应从「正在建的位置」里去掉：%v", got)
	}
}

// 「正在建的位置」现在读不到（比如在没插上的移动硬盘里）时启动不整理掉它：回来了照样找得到里面的半成品（A38）。
func TestCreatingRootKeptWhileUnreadable(t *testing.T) {
	adoptEnv(t)
	root := filepath.Join(t.TempDir(), "移动硬盘")
	dir := filepath.Join(root, "half")
	app.RememberCreatingRoot(root) // 记过以后这个位置就读不到了
	if _, err := NewManager(); err != nil {
		t.Fatal(err)
	}
	if got := app.CreatingRoots(); !slices.Contains(got, root) {
		t.Fatalf("读不到的位置不该被整理掉：%v", got)
	}
	writeTree(t, dir, map[string]string{CreatingMarker: "half\n"})
	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if list := m2.CreatingLeftovers(); len(list) != 1 || list[0].Name != "half" {
		t.Fatalf("位置回来以后应找得到里面的半成品：%+v", list)
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
		skipOrFailJunction(t, err, out)
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
