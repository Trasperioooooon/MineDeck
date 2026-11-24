package inst

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
)

// 整合包的覆盖目录（v3.1.0 正式版前检查 C-2e）：名字在 Windows 上写不出原样的不采用（带扩展名的设备名、结尾是点或空格的）；
// 只差大小写的两个文件在 Windows 上是同一个，照旧以后写的为准，但都记下来给日志：后一层（server-overrides）本来就要盖掉
// 前一层（overrides），跨层只差大小写时也该它说了算。写法一模一样的不算重名。
func TestOverrideSinkNamesAndCaseDuplicates(t *testing.T) {
	z := zipOrdered(t, [][2]string{
		{"overrides/config/A.toml", "a1"},
		{"overrides/config/a.toml", "a2"},
		{"overrides/config/b.toml", "b1"},
		{"overrides/instance.json", "{}"},
		{"overrides/config/CON.txt", "x"},
		{"overrides/config/trail.toml.", "x"},
		{"server-overrides/config/b.toml", "b2"},
		{"server-overrides/config/A.TOML", "a3"},
	})
	dir := t.TempDir()
	sink := newOverrideSink(dir)
	if err := modpack.ExtractOverrides(&modpack.Pack{ZipPath: z, OverrideDirs: []string{"overrides", "server-overrides"}}, dir, sink.write); err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, filepath.Join(dir, "config")); !slices.Equal(got, []string{"A.toml", "b.toml"}) {
		t.Errorf("config 里应是 A.toml 和 b.toml：%q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "A.toml")); string(b) != "a3" {
		t.Errorf("只差大小写的以后写的为准，跨层时 server-overrides 说了算：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config", "b.toml")); string(b) != "b2" {
		t.Errorf("后一层覆盖目录里写法一样的应盖掉前一层：%q", b)
	}
	if want := []string{"instance.json", "config/CON.txt", "config/trail.toml."}; !slices.Equal(sink.notTaken, want) {
		t.Errorf("没采用的：%q，应为 %q", sink.notTaken, want)
	}
	if want := []string{"config/a.toml", "config/A.TOML"}; !slices.Equal(sink.dups, want) {
		t.Errorf("只差大小写的：%q，应为 %q", sink.dups, want)
	}
}

// 整合包清单里落点只差大小写的（一模一样的也算）只下载第一条：Windows 上是同一个文件，并发下载会同时写它。
func TestDedupeDownloads(t *testing.T) {
	kept, dups := dedupeDownloads([]modpack.File{
		{Path: "mods/A.jar"}, {Path: "mods/b.jar"}, {Path: "mods/a.jar"}, {Path: "MODS/B.JAR"}, {Path: "mods/b.jar"},
		{Path: "mods/sub/../c.jar"}, {Path: "mods/c.jar"},
	})
	var paths []string
	for _, f := range kept {
		paths = append(paths, f.Path)
	}
	if want := []string{"mods/A.jar", "mods/b.jar", "mods/sub/../c.jar"}; !slices.Equal(paths, want) {
		t.Errorf("留下的：%q，应为 %q", paths, want)
	}
	if want := []string{"mods/a.jar", "MODS/B.JAR", "mods/b.jar", "mods/c.jar"}; !slices.Equal(dups, want) {
		t.Errorf("没下载的：%q，应为 %q", dups, want)
	}
}

// 导入整合包：只差大小写的落点导入前就找出来，写进任务结果（A13、A47）——清单里下载的之间留前面那个，下载的和覆盖文件夹里的
// 以覆盖文件夹为准，覆盖文件夹里的之间后写的为准；导入时没装进来的文件也写进任务结果（A46），原版服导入完也看得到。
func TestModpackImportListsCaseConflictsAndSkipped(t *testing.T) {
	m, root := importEnv(t)
	req := CreateReq{Name: "大小写", Core: mcsrc.CoreVanilla, MC: "1.20.1", EULA: true, Root: root}
	dir, err := m.validateCreate(&req)
	if err != nil {
		t.Fatal(err)
	}
	defer m.releaseCreating(req.Name)
	defer m.releaseCreating(dirKey(dir))
	writeTree(t, dir, map[string]string{"server.jar": "core"})
	data := []byte("下载的内容")
	old := modpackHTTP
	modpackHTTP = func() *http.Client { return dl.GuardedClientWithTransport(fakeTransport{content: data}) }
	t.Cleanup(func() { modpackHTTP = old })
	dlFile := func(p string) modpack.File {
		return modpack.File{Path: p, SHA1: sha1Of(t, data), URLs: []string{"https://93.184.216.34/" + path.Base(p)}, Size: int64(len(data))}
	}
	pack := &modpack.Pack{
		Format: modpack.FormatMrpack, Name: "大小写包",
		Files:      []modpack.File{dlFile("mods/A.jar"), dlFile("config/X.toml"), dlFile("mods/a.jar"), dlFile("instance.json")},
		Pending:    []modpack.PendingFile{{Path: "gameai.json", Reason: modpack.ReasonNoHash}},
		ClientOnly: []string{"resourcepacks/光影.zip"},
		ZipPath: zipOrdered(t, [][2]string{
			{"overrides/CONFIG/x.toml", "覆盖的"},
			{"overrides/config/b.txt", "b1"},
			{"overrides/run.bat", "evil"},
			{"overrides/config/CON.txt", "x"},
			{"server-overrides/config/B.txt", "b2"},
		}),
		OverrideDirs: []string{"overrides", "server-overrides"},
	}
	i := &Instance{Settings: Settings{Name: req.Name, Core: req.Core, MC: req.MC, JavaPath: testJava17, JavaMajor: 17, LaunchTarget: "jar:server.jar", CreatedAt: time.Now()}, Dir: dir, Console: newConsole()}
	task, ctx := m.Tasks.New("导入", append(append([]string{}, createSteps...), "下载", "覆盖"))
	if err := m.completeCreate(ctx, task, i, func(i *Instance) error { return m.applyModpack(ctx, task, pack, req, i, "") }); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(task.Snapshot().Log, "\n"))
	}

	w := warningOf(task, "import-case")
	if w == nil || w.Title != "3 组文件名只差大小写" {
		t.Fatalf("只差大小写的应写进结果：%+v", w)
	}
	for k, want := range []string{
		"mods/A.jar、mods/a.jar：留下的是「mods/A.jar」（清单里重名的只下载前面那个）",
		"config/X.toml、CONFIG/x.toml：留下的是「CONFIG/x.toml」（覆盖文件夹里的盖掉清单里下载的）",
		"config/b.txt、config/B.txt：留下的是「config/B.txt」（覆盖文件夹里后写的盖掉先写的）",
	} {
		if w.Items[k].Name != want {
			t.Errorf("第 %d 组：%q，应为 %q", k+1, w.Items[k].Name, want)
		}
	}
	if runtime.GOOS == "windows" {
		if b := mustRead(t, filepath.Join(dir, "config", "X.toml")); b != "覆盖的" {
			t.Errorf("留下的内容应和结果里说的一样，是覆盖文件夹里那份：%q", b)
		}
	}

	s := warningOf(task, "import-skipped")
	if s == nil || s.Title != "5 个文件没有装进来" {
		t.Fatalf("没装进来的应写进结果：%+v", s)
	}
	var got []string
	for _, it := range s.Items {
		got = append(got, it.Name)
	}
	for _, want := range []string{
		"resourcepacks/光影.zip（整合包清单里写明只给客户端用）",
		"instance.json（是 MineDeck 自己的文件，由 MineDeck 生成）",
		"run.bat（是 MineDeck 自己的文件，由 MineDeck 生成）",
		"config/CON.txt（名字在 Windows 上写不出来）",
		"gameai.json（是 MineDeck 自己的文件，由 MineDeck 生成）",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("没装进来的里面应有 %q：%q", want, got)
		}
	}
}

// 导入现成服务端：名字在 Windows 上写不出来的、和前面只差大小写的，也写进任务结果（A46、A47）。
func TestServerImportListsSkippedAndCaseConflicts(t *testing.T) {
	m, _ := importEnv(t)
	pack, err := modpack.Parse(zipOrdered(t, [][2]string{
		{"paper-1.20.1-100.jar", paperJarBytes(t)},
		{"eula.txt", "eula=true\n"},
		{"config/Dup.toml", "first"},
		{"config/dup.toml", "second"},
		{"config/CON.txt", "x"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "跳过的", XmxMB: 2048, Port: 25579, EULA: true}, filepath.Join(app.ServersDir, "skipped")); err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if w := warningOf(tk, "import-case"); w == nil || len(w.Items) != 1 || w.Items[0].Name != "config/Dup.toml、config/dup.toml：留下的是「config/Dup.toml」（压缩包里只差大小写的只解压前面那个）" {
		t.Fatalf("只差大小写的应写进结果：%+v", w)
	}
	if w := warningOf(tk, "import-skipped"); w == nil || len(w.Items) != 1 || w.Items[0].Name != "config/CON.txt（名字在 Windows 上写不出来）" {
		t.Fatalf("名字写不出来的应写进结果：%+v", w)
	}
}

// 现成服务端里写不出名字的、只差大小写的比告警能列的多：标题和日志写一共几个，列表只列前面一部分（A46、A47）。
func TestServerImportCountsAllSkippedAndCaseConflicts(t *testing.T) {
	m, _ := importEnv(t)
	entries := [][2]string{{"paper-1.20.1-100.jar", paperJarBytes(t)}, {"eula.txt", "eula=true\n"}}
	for k := 0; k < maxWarnItems+10; k++ {
		entries = append(entries, [2]string{fmt.Sprintf("config/CON.%02d.txt", k), "x"})
	}
	for k := 0; k < maxWarnItems+5; k++ {
		entries = append(entries, [2]string{fmt.Sprintf("data/f%02d.txt", k), "a"}, [2]string{fmt.Sprintf("data/F%02d.txt", k), "b"})
	}
	pack, err := modpack.Parse(zipOrdered(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	if err := m.runServerImport(context.Background(), tk, pack, CreateReq{Name: "好多", XmxMB: 2048, Port: 25581, EULA: true}, filepath.Join(app.ServersDir, "many")); err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	for kind, want := range map[string]struct {
		title string
		total int
	}{"import-skipped": {fmt.Sprintf("%d 个文件没有装进来", maxWarnItems+10), maxWarnItems + 10}, "import-case": {fmt.Sprintf("%d 组文件名只差大小写", maxWarnItems+5), maxWarnItems + 5}} {
		w := warningOf(tk, kind)
		if w == nil || w.Title != want.title || len(w.Items) != maxWarnItems+1 || !strings.Contains(w.Items[maxWarnItems].Name, fmt.Sprintf("一共 %d", want.total)) {
			t.Errorf("%s：标题写一共几个、只列前 %d 项：%+v", kind, maxWarnItems, w)
		}
	}
	log := strings.Join(tk.Snapshot().Log, "\n")
	if !strings.Contains(log, fmt.Sprintf("…等 %d 个", maxWarnItems+10)) || !strings.Contains(log, fmt.Sprintf("…等 %d 个", maxWarnItems+5)) {
		t.Fatalf("日志里也写一共几个：%s", log)
	}
}
