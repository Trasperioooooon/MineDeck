package inst

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
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
	// 斜杠前用这个位置真实的编号：前缀对不上任何位置的话，名字校验拿掉了也照样回「没有这一项」，这几条就白测了。
	key, _, _ := strings.Cut(list[0].ID, "/")
	for _, badID := range []string{"../" + filepath.Base(user), key + "/.", key + "/..", key + "/.. ", key + "/...", key + "/CON.txt", key + "/foo:bar"} {
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

// 往新的存放位置里建服，登记写完 instance.json、还没把位置记成扫描根程序就没了：重开以后这台在列表里，位置成了扫描根，
// 「正在建的位置」里去掉了，残留的记号也去掉了。只有移出列表留下的 instance.json.bak 的不算建成，那个位置不升格（A38）。
func TestCreatedRootRememberedAtStartupAfterCrash(t *testing.T) {
	adoptEnv(t)
	base := t.TempDir()
	root := filepath.Join(base, "新存放位置")
	dir := filepath.Join(root, "建成的服")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreatingMarker(dir, "建成的服"); err != nil {
		t.Fatal(err)
	}
	// 登记写好了 instance.json，程序在记扫描根之前没了
	writeTree(t, dir, map[string]string{
		"instance.json":   `{"name":"建成的服","core":"paper","mc":"1.20.1","xmxMb":1024,"xmsMb":512}`,
		"world/level.dat": "x",
	})
	bakRoot := filepath.Join(base, "只有bak的位置")
	bakDir := filepath.Join(bakRoot, "移出的服")
	if err := os.MkdirAll(bakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreatingMarker(bakDir, "移出的服"); err != nil {
		t.Fatal(err)
	}
	writeTree(t, bakDir, map[string]string{settingsBackup: `{"name":"移出的服","core":"paper","mc":"1.20.1"}`})

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("建成的服"); err != nil {
		t.Fatalf("重开以后这台应在列表里：%v", err)
	}
	if roots := app.InstanceRoots(); !slices.Contains(roots, root) || slices.Contains(roots, bakRoot) {
		t.Fatalf("有建成的服的位置应补记成扫描根，只有 .bak 的不该：%v", roots)
	}
	if got := app.CreatingRoots(); slices.Contains(got, root) {
		t.Fatalf("成了扫描根以后应从「正在建的位置」里去掉：%v", got)
	}
	if pathExists(filepath.Join(dir, CreatingMarker)) {
		t.Fatal("这台残留的创建记号应在启动时去掉")
	}
	if list := m.CreatingLeftovers(); len(list) != 0 {
		t.Fatalf("建成的服和移出列表的服都不该当成没建完的：%+v", list)
	}
}

// 登记的先后（markCreated）：记成扫描根的那一刻，instance.json 已经写好、创建记号还在；两步之间程序没了，
// 下次启动才认得出来（TestCreatedRootRememberedAtStartupAfterCrash）。新建和导入走的 completeCreate、交接包走的 FinalizeCreate 各走一遍（A38）。
func TestRegistrationRemembersRootBeforeClearingMarker(t *testing.T) {
	m, _ := importEnv(t)
	var seen []string
	afterRootRemembered = func(dir string) {
		seen = append(seen, filepath.Base(dir))
		if !pathExists(filepath.Join(dir, "instance.json")) {
			t.Errorf("%s：记扫描根之前 instance.json 应已写好", dir)
		}
		if !slices.Contains(app.InstanceRoots(), filepath.Dir(dir)) {
			t.Errorf("%s：这时位置应已记成扫描根：%v", dir, app.InstanceRoots())
		}
		if !pathExists(filepath.Join(dir, CreatingMarker)) {
			t.Errorf("%s：记扫描根之前不该先去掉创建记号", dir)
		}
	}
	t.Cleanup(func() { afterRootRemembered = func(string) {} })
	base := t.TempDir()

	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"eula.txt":             "eula=true\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "导入的服", XmxMB: 2048, Port: 25573, EULA: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, filepath.Join(base, "位置1", "imported")); err != nil {
		t.Fatalf("导入失败: %v\n日志: %v", err, tk.Snapshot().Log)
	}

	dir := filepath.Join(base, "位置2", "handover")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreatingMarker(dir, "交接的服"); err != nil {
		t.Fatal(err)
	}
	s := Settings{Name: "交接的服", Core: mcsrc.CorePaper, MC: "1.20.1", JavaPath: fakeJava(t), LaunchTarget: "jar:server.jar", XmxMB: 1024, XmsMB: 512}
	if _, err := m.FinalizeCreate(s, dir); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(seen, []string{"imported", "handover"}) {
		t.Fatalf("两处登记都该经过 markCreated：%v", seen)
	}
	for _, d := range []string{filepath.Join(base, "位置1", "imported"), dir} {
		if pathExists(filepath.Join(d, CreatingMarker)) {
			t.Errorf("%s：登记完记号应去掉", d)
		}
	}
}

// 新位置里留下一个没建完的文件夹，人把它接管进来（接管写 instance.json、单独登记这个文件夹，记号还在）。
// 重开以后这个位置不该因此升成扫描根：接管只认它自己，位置里别的带 instance.json 的文件夹不进列表（A38）。
func TestAdoptedLeftoverDoesNotPromoteItsRoot(t *testing.T) {
	m, _ := adoptEnv(t)
	root := filepath.Join(t.TempDir(), "新存放位置")
	writeTree(t, filepath.Join(root, "old-srv"), map[string]string{
		"instance.json": `{"name":"别处留下的服","core":"paper","mc":"1.20.1","xmxMb":1024,"xmsMb":512}`,
	})
	half := newCreatingLeftover(t, root, "half")
	writeTree(t, half, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n"})
	if log, err := adopt(t, m, AdoptReq{Dir: half, Name: "接管的服", Core: mcsrc.CoreVanilla, MC: "1.21.1",
		LaunchTarget: "jar:server.jar", XmxMB: 2048, JavaMajor: 21, JavaPath: fakeJava(t)}); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}

	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Get("接管的服"); err != nil {
		t.Fatalf("接管进来的应在列表里：%v", err)
	}
	if _, err := m2.Get("别处留下的服"); err == nil {
		t.Fatal("这个位置里别的服务器不该因为接管了一个文件夹而进列表")
	}
	if roots := app.InstanceRoots(); slices.Contains(roots, root) {
		t.Fatalf("接管的文件夹所在的位置不该升成扫描根：%v", roots)
	}
}

// 接管一个没建完的文件夹：写文件之前去掉创建记号，接管完成以后盘上不会有「记号加 instance.json」（A38）；
// 接管中途失败时，记号和别的动过的文件一起放回去，它照旧算没建完的。
func TestAdoptLeftoverClearsMarkerAndRestoresOnFailure(t *testing.T) {
	m, _ := adoptEnv(t)
	root := filepath.Join(t.TempDir(), "新存放位置")
	req := func(dir, name string) AdoptReq {
		return AdoptReq{Dir: dir, Name: name, Core: mcsrc.CoreVanilla, MC: "1.21.1",
			LaunchTarget: "jar:server.jar", XmxMB: 2048, JavaMajor: 21, JavaPath: fakeJava(t)}
	}

	ok := newCreatingLeftover(t, root, "half-ok")
	writeTree(t, ok, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n"})
	if log, err := adopt(t, m, req(ok, "接管成的")); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if pathExists(filepath.Join(ok, CreatingMarker)) {
		t.Fatal("接管完成以后创建记号应去掉")
	}

	bad := newCreatingLeftover(t, root, "half-bad")
	writeTree(t, bad, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n", "run.bat": "@echo off\r\njava -jar server.jar\r\n"})
	before := mustRead(t, filepath.Join(bad, CreatingMarker))
	// run.bat 设成只读：去掉记号以后、写 run.bat 这一步失败
	bat := filepath.Join(bad, "run.bat")
	if err := os.Chmod(bat, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bat, 0o644) })
	if log, err := adopt(t, m, req(bad, "接管不成的")); err == nil {
		t.Fatalf("run.bat 只读时应失败\n%s", log)
	}
	if got := mustRead(t, filepath.Join(bad, CreatingMarker)); got != before {
		t.Fatalf("接管失败以后创建记号应原样放回去：%q", got)
	}
	if list := m.CreatingLeftovers(); !slices.ContainsFunc(list, func(it CreatingLeftover) bool { return it.Name == "half-bad" }) {
		t.Fatalf("接管失败的那个应照旧算没建完的：%+v", list)
	}
}

// 两个位置的短哈希撞了：两边各有一个同名的半成品，编号一模一样，猜不出指的是哪个，删哪个都回「没有这一项」、文件都在（A38）。
func TestLeftoverIDHashCollisionRefused(t *testing.T) {
	adoptEnv(t)
	a, b := collidingRoots(t, t.TempDir())
	dirA, dirB := newCreatingLeftover(t, a, "生存服"), newCreatingLeftover(t, b, "生存服")
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	list := m.CreatingLeftovers()
	if len(list) != 2 || list[0].ID != list[1].ID {
		t.Fatalf("撞了哈希的两个位置里的同名半成品应列两行、编号一样：%+v", list)
	}
	for _, it := range list {
		if err := m.DiscardCreatingLeftover(it.ID, true); err == nil || !strings.Contains(err.Error(), "没有这一项") {
			t.Fatalf("编号对上两个位置时不该去猜：%v", err)
		}
	}
	for _, d := range []string{dirA, dirB} {
		if !pathExists(filepath.Join(d, "world", "level.dat")) {
			t.Fatalf("%s 不该被删", d)
		}
	}
}

// collidingRoots 在 base 下找两个短哈希（leftoverRootKey）相同的位置。FNV-32a 只有 32 位，按生日问题十来万个以内就能撞上。
func collidingRoots(t *testing.T, base string) (string, string) {
	t.Helper()
	seen := map[string]string{}
	for n := 0; n < 3_000_000; n++ {
		p := filepath.Join(base, "r"+strconv.Itoa(n))
		k := leftoverRootKey(p)
		if q, ok := seen[k]; ok {
			return q, p
		}
		seen[k] = p
	}
	t.Fatal("没找到短哈希相同的两个位置")
	return "", ""
}

// 只在 Windows 上把只差大小写的两个位置当成同一个：区分大小写的卷上它们是两个文件夹，各列各的、编号不同，删一个不动另一个。
func TestLeftoverRootsCaseFoldOnlyOnWindows(t *testing.T) {
	base := t.TempDir()
	upper, lower := filepath.Join(base, "PosA"), filepath.Join(base, "posa")
	if same := foldRoot(upper) == foldRoot(lower); same != (runtime.GOOS == "windows") {
		t.Fatalf("foldRoot 只该在 Windows 上不分大小写：%q %q 一样=%v", upper, lower, same)
	}
	if runtime.GOOS == "windows" {
		return
	}
	adoptEnv(t)
	dirU := newCreatingLeftover(t, upper, "half")
	if pathExists(filepath.Join(lower, "half")) {
		t.Skip("这个卷不区分大小写（比如 Mac 默认的 APFS），只差大小写的两个位置是同一个文件夹")
	}
	dirL := newCreatingLeftover(t, lower, "half")
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	list := m.CreatingLeftovers()
	if len(list) != 2 || list[0].ID == list[1].ID {
		t.Fatalf("区分大小写的卷上应列两行、编号不同：%+v", list)
	}
	if err := m.DiscardCreatingLeftover(leftoverIDIn(t, list, "posa"), true); err != nil {
		t.Fatal(err)
	}
	if pathExists(dirL) || !pathExists(dirU) {
		t.Fatalf("应只删掉 posa 里的那个：posa 的还在=%v，PosA 的还在=%v", pathExists(dirL), pathExists(dirU))
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

// newCreatingLeftover 照建服流程在 root 里放一个没建完的文件夹（WriteCreatingMarker 会把 root 记进「正在建的位置」），
// 里面带世界文件。
func newCreatingLeftover(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreatingMarker(dir, name); err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, map[string]string{"world/level.dat": "x"})
	return dir
}

// leftoverIDIn 列表里位于 rootName 的那一行的编号。
func leftoverIDIn(t *testing.T, list []CreatingLeftover, rootName string) string {
	t.Helper()
	for _, it := range list {
		if it.RootName == rootName {
			return it.ID
		}
	}
	t.Fatalf("列表里没有位于「%s」的一行：%+v", rootName, list)
	return ""
}

// restoreServersDirAfter 用例要改默认存放位置时先调：结束时改回默认，再清掉 roots（改回去那一下会把改过的位置并进 roots），
// 不留给后面的用例。
func restoreServersDirAfter(t *testing.T) {
	t.Cleanup(func() {
		setConfig(t, func(c *app.Config) { c.ServersDir = "" })
		setConfig(t, func(c *app.Config) { c.Roots = nil })
	})
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// 两个位置里各有一个同名的半成品。界面列出来以后，其中一个位置因为建成了一台而升成扫描根，位置的先后变了：
// 界面上停着的旧编号还是指原来那一行，另一个位置里的同名文件夹不动（A38）。
func TestLeftoverIDKeepsItsRootAfterPromotion(t *testing.T) {
	importEnv(t)
	base := t.TempDir()
	rootA, rootX := filepath.Join(base, "位置A"), filepath.Join(base, "位置X")
	dirA := newCreatingLeftover(t, rootA, "生存服")
	dirX := newCreatingLeftover(t, rootX, "生存服")
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	idA := leftoverIDIn(t, m.CreatingLeftovers(), "位置A")
	before := app.InstanceRoots()

	// 在位置X 里建成一台（真实的导入现成服务端流水线）：位置X 升成扫描根
	pack, err := modpack.Parse(zipOf(t, map[string]string{
		"paper-1.20.1-100.jar": paperJarBytes(t),
		"eula.txt":             "eula=true\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "另一台", XmxMB: 2048, Port: 25573, EULA: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, filepath.Join(rootX, "另一台")); err != nil {
		t.Fatalf("导入失败: %v\n日志: %v", err, tk.Snapshot().Log)
	}
	if after := app.InstanceRoots(); slices.Contains(before, rootX) || !slices.Contains(after, rootX) {
		t.Fatalf("位置X 应在建成以后才成为扫描根：之前 %v，之后 %v", before, after)
	}

	if err := m.DiscardCreatingLeftover(idA, true); err != nil {
		t.Fatalf("拿列表里位置A 那一行的编号去删：%v", err)
	}
	if pathExists(dirA) {
		t.Fatal("位置A 里的半成品应删掉")
	}
	if !pathExists(filepath.Join(dirX, "world", "level.dat")) {
		t.Fatal("位置X 里的同名半成品不该被动过")
	}
}

// 改了默认存放位置以后（旧的并进 roots），位置的先后也会变：拿改之前列出来的编号去删，删的还是原来那个位置里的（A38）。
func TestLeftoverIDKeepsItsRootAfterDefaultDirChange(t *testing.T) {
	adoptEnv(t)
	base := t.TempDir()
	d1, d2 := filepath.Join(base, "默认1"), filepath.Join(base, "默认2")
	restoreServersDirAfter(t)
	setConfig(t, func(c *app.Config) { c.ServersDir, c.Roots = d1, []string{d2} })
	dir1 := filepath.Join(d1, "生存服")
	dir2 := filepath.Join(d2, "生存服")
	for _, d := range []string{dir1, dir2} {
		writeTree(t, d, map[string]string{CreatingMarker: "生存服\n", "world/level.dat": "x"})
	}
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	id1 := leftoverIDIn(t, m.CreatingLeftovers(), "默认1")
	setConfig(t, func(c *app.Config) { c.ServersDir = d2 })
	if roots := app.InstanceRoots(); slices.Index(roots, d1) < slices.Index(roots, d2) {
		t.Fatalf("改了默认存放位置以后两个位置的先后应该变了：%v", roots)
	}

	if err := m.DiscardCreatingLeftover(id1, true); err != nil {
		t.Fatalf("拿改之前默认1 那一行的编号去删：%v", err)
	}
	if pathExists(dir1) {
		t.Fatal("默认1 里的半成品应删掉")
	}
	if !pathExists(filepath.Join(dir2, "world", "level.dat")) {
		t.Fatal("默认2 里的同名半成品不该被动过")
	}
}

// 扫描根里有两条只差大小写的（Windows 上是同一个文件夹）：里面的半成品只列一次，用列出来的编号删得掉。
func TestLeftoverRootsDifferingOnlyInCaseListedOnce(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只有 Windows 上只差大小写的两个路径一定是同一个文件夹")
	}
	adoptEnv(t)
	restoreServersDirAfter(t)
	setConfig(t, func(c *app.Config) { c.ServersDir = strings.ToUpper(app.ServersDir) })
	if roots := app.InstanceRoots(); !slices.Contains(roots, app.ServersDir) || !slices.Contains(roots, strings.ToUpper(app.ServersDir)) {
		t.Fatalf("配置里应有两条只差大小写的扫描根：%v", roots)
	}
	half := filepath.Join(app.ServersDir, "half")
	writeTree(t, half, map[string]string{CreatingMarker: "half\n"})
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	list := m.CreatingLeftovers()
	if len(list) != 1 {
		t.Fatalf("同一个文件夹只该列一次：%+v", list)
	}
	if err := m.DiscardCreatingLeftover(list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if pathExists(half) {
		t.Fatal("半成品应删掉")
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
	if err := m.DiscardCreatingLeftover(leftoverID(root, "busy"), true); err == nil || !strings.Contains(err.Error(), "正在") {
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
	if err := m.DiscardCreatingLeftover(leftoverID(root, "junc"), true); err == nil || !strings.Contains(err.Error(), "链接") {
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
	if err := m.DiscardCreatingLeftover(leftoverID(root, "half-nested"), true); err == nil || !strings.Contains(err.Error(), "目录联接") {
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
		if err := m.DiscardCreatingLeftover(leftoverID(root, name), true); err == nil || strings.Contains(err.Error(), "没有这一项") {
			t.Fatalf("%s 不应被允许删除，而且要是因为它本身不能删、不是编号不对：%v", name, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(kept, "world", "level.dat")); err != nil || string(b) != "precious" {
		t.Fatalf("移出列表的服务器文件被动过：%v %q", err, b)
	}
}
