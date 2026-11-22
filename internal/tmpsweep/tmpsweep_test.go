package tmpsweep

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 只认 MineDeck 自己起的那几种名字：前缀、1～10 位纯数字（os.CreateTemp 填的 uint32）、后缀都要对上。
func TestIsOurs(t *testing.T) {
	for name, want := range map[string]bool{
		".minedeck-download-123456789.part":             true,
		".minedeck-download-1.download":                 true,
		".minedeck-copy-4294967295.copy":                true, // 10 位（uint32 上界）
		".minedeck-write-0.tmp":                         true, // 1 位
		".minedeck-write-99.tmp":                        true,
		".minedeck-unzip-123.part":                      true,
		".minedeck-backup-123456.tmp":                   true,
		".minedeck-backup-.tmp":                         false,
		".minedeck-backup-abc.tmp":                      false,
		".minedeck-download-42949672950.part":           false, // 11 位：超过 CreateTemp 可能的位数
		".minedeck-backup-20261122153000.tmp":           false, // 14 位时间戳：不是 CreateTemp 起的
		".minedeck-write-123456789012345678901234.tmp":  false, // 24 位
		".minedeck-write-1234567890123456789012345.tmp": false, // 25 位
		".minedeck-download-.part":                      false, // 中间没有数字
		".minedeck-download-abc.part":                   false,
		".minedeck-download-12a4.part":                  false,
		".minedeck-download-123.part.bak":               false,
		".minedeck-download-123.zip":                    false,
		".minedeck-write-123.tmp.txt":                   false,
		".minedeck-write-123":                           false,
		"minedeck-write-123.tmp":                        false, // 没有开头的点
		"x.minedeck-write-123.tmp":                      false,
		".minedeck-copy-123.part":                       false, // 前后缀不是一对
		".minedeck-companion-install":                   false, // 别处用的名字，不是临时文件
		".minedeck-bridge-operation":                    false,
		".minedeck":                                     false,
		".backup-123.tmp":                               false, // 旧版备份临时名只在备份文件夹那一层认，不在通用名单里
		"server.jar":                                    false,
		"world.zip.part":                                false,
		"":                                              false,
	} {
		if got := IsOurs(name); got != want {
			t.Errorf("IsOurs(%q) = %v，应为 %v", name, got, want)
		}
	}
}

func put(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func shouldSkipJunction(ci string, out []byte) bool {
	if ci != "" {
		return false
	}
	low := strings.ToLower(string(out))
	return strings.Contains(low, "privilege") || strings.Contains(low, "not support") || strings.Contains(low, "权限") || strings.Contains(low, "不支持")
}

func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		if shouldSkipJunction(os.Getenv("CI"), out) {
			t.Skipf("建不了目录联接：%v %s", err, out)
		}
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

// 清一棵树：很久没动过的临时文件删掉；刚动过的（可能有人正在用）、名字不是 MineDeck 起的、名字像临时文件的文件夹都不动；
// 超过层数的不进去。
func TestTree(t *testing.T) {
	root := t.TempDir()
	const day = 24 * time.Hour
	stale := []string{
		".minedeck-download-111.part",
		"java/.minedeck-download-222.part",
		"paper/1.21.1/.minedeck-write-333.tmp",
		"a/b/c/.minedeck-copy-444.copy",
	}
	keep := []string{
		".minedeck-download-555.part",          // 刚动过
		"java/.minedeck-write-666.tmp",         // 刚动过
		"java/jdk-21.zip",                      // 别的文件
		"java/.minedeck-download-abc.part",     // 名字对不上
		"paper/world.zip.part",                 // 别人的 .part
		"a/b/c/d/.minedeck-download-777.part",  // 太深
		".minedeck-write-888.tmp/inside.txt",   // 名字像临时文件的是个文件夹
		"a/.minedeck-download-999.part.keep",   // 多了个后缀
		"a/用户自己的 .minedeck-write-1.tmp 说明.txt", // 名字里带着也不算
	}
	for _, p := range stale {
		put(t, filepath.Join(root, filepath.FromSlash(p)), day)
	}
	for k, p := range keep {
		age := day
		if k < 2 {
			age = time.Minute
		}
		put(t, filepath.Join(root, filepath.FromSlash(p)), age)
	}
	res := Tree(root, 3, time.Hour)
	if res.Files != len(stale) || res.Bytes != int64(10*len(stale)) {
		t.Errorf("清了 %d 个、%d 字节，应为 %d 个、%d 字节", res.Files, res.Bytes, len(stale), 10*len(stale))
	}
	for _, p := range stale {
		if exists(filepath.Join(root, filepath.FromSlash(p))) {
			t.Errorf("很久没动过的临时文件应删掉：%s", p)
		}
	}
	for _, p := range keep {
		if !exists(filepath.Join(root, filepath.FromSlash(p))) {
			t.Errorf("不该动：%s", p)
		}
	}
	// 再清一遍什么都不动
	if res := Tree(root, 3, time.Hour); res.Files != 0 {
		t.Errorf("第二遍不该再删：%+v", res)
	}
	// 文件夹不在也不算错
	if res := Tree(filepath.Join(root, "没有这个文件夹"), 3, time.Hour); res.Files != 0 {
		t.Errorf("%+v", res)
	}
}

// 只清一层：下面文件夹里的不管。
func TestDir(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, ".minedeck-write-1.tmp"), 48*time.Hour)
	put(t, filepath.Join(root, "world", ".minedeck-write-2.tmp"), 48*time.Hour)
	put(t, filepath.Join(root, "instance.json"), 48*time.Hour)
	put(t, filepath.Join(root, ".minedeck-copy-3.copy"), time.Second)
	if res := Dir(root, time.Hour); res.Files != 1 {
		t.Fatalf("只该删这一层里那一个：%+v", res)
	}
	for p, want := range map[string]bool{".minedeck-write-1.tmp": false, "world/.minedeck-write-2.tmp": true, "instance.json": true, ".minedeck-copy-3.copy": true} {
		if exists(filepath.Join(root, filepath.FromSlash(p))) != want {
			t.Errorf("%s：在不在应为 %v", p, want)
		}
	}
}

// 程序启动后那一遍清哪些地方：程序文件夹这一层、data、缓存、运行时，每台服务器实际用的备份文件夹这一层，
// 以及服务器文件夹内限制深度（第 6 层清掉、第 7 层不清）、跳过世界文件夹（含 level.dat 的文件夹）的各层；程序文件夹里别的未登记子文件夹不翻。
func TestStartup(t *testing.T) {
	base := t.TempDir()
	srv, adopted := filepath.Join(base, "servers", "S1"), filepath.Join(t.TempDir(), "接管来的服")
	put(t, filepath.Join(srv, "world", "level.dat"), 48*time.Hour)
	put(t, filepath.Join(srv, "worlds", "skyblock", "level.dat"), 48*time.Hour)

	const old = 48 * time.Hour
	gone := []string{
		filepath.Join(base, ".minedeck-write-1.tmp"),
		filepath.Join(base, "data", "modcart", ".minedeck-write-2.tmp"),
		filepath.Join(base, "cache", "java", ".minedeck-download-3.part"),
		filepath.Join(base, "cache", "frpc", ".minedeck-download-4.part"),
		filepath.Join(base, "runtimes", "java-21", "lib", "security", ".minedeck-download-5.part"),
		filepath.Join(base, "runtimes", "frpc", "openfrp", ".minedeck-unzip-6.part"),
		filepath.Join(srv, ".minedeck-write-7.tmp"),
		filepath.Join(srv, "mods", ".minedeck-copy-8.copy"),
		filepath.Join(srv, "plugins", ".minedeck-download-9.download"),
		filepath.Join(srv, "config", "fancymenu", "assets", ".minedeck-download-16.part"),
		filepath.Join(srv, "kubejs", "server_scripts", ".minedeck-write-17.tmp"),
		filepath.Join(srv, "resourcepacks", ".minedeck-download-18.part"),
		filepath.Join(srv, ".minedeck", ".minedeck-write-19.tmp"),
		filepath.Join(srv, "a", "b", "c", "d", "e", "f", ".minedeck-download-129.part"), // 第 6 层（instanceMaxDepth 边界内）：应清掉
		filepath.Join(adopted, ".minedeck-download-10.part"),
		filepath.Join(adopted, "config", ".minedeck-write-11.tmp"),
		filepath.Join(adopted, "config", "paxi", "datapacks", ".minedeck-write-20.tmp"),
		filepath.Join(base, "backups", "S1", ".minedeck-backup-21.tmp"),
		filepath.Join(base, "backups", "S1", ".backup-22.tmp"),
		filepath.Join(base, "backups", "接管服", ".minedeck-backup-23.tmp"),
	}
	stay := []string{
		filepath.Join(srv, "world", ".minedeck-write-12.tmp"),                               // 世界根（有 level.dat）不进
		filepath.Join(srv, "world", "region", ".minedeck-write-120.tmp"),                    // 世界深层不翻
		filepath.Join(srv, "worlds", "skyblock", "region", ".minedeck-download-121.part"),   // 子目录里的多世界（有 level.dat）也不进
		filepath.Join(srv, "a", "b", "c", "d", "e", "f", "g", ".minedeck-download-13.part"), // 第 7 层（超过深度上限）：不进
		filepath.Join(base, "backups", "未登记服", ".minedeck-backup-14.tmp"),                   // 没登记的服务器的备份文件夹不管
		filepath.Join(base, "backups", "未登记服", ".backup-140.tmp"),                           // 同上
		filepath.Join(base, "servers", ".minedeck-write-15.tmp"),                            // 没登记的文件夹不管
		filepath.Join(srv, ".backup-24.tmp"),                                                // .backup-*.tmp 在备份文件夹外一律不动
		filepath.Join(srv, "config", "fancymenu", ".backup-25.tmp"),                         // 同上
		filepath.Join(base, ".backup-26.tmp"),                                               // 同上
		filepath.Join(srv, "server.jar"),
		filepath.Join(base, "config.json"),
	}
	for _, p := range append(append([]string{}, gone...), stay...) {
		put(t, p, old)
	}
	short := func(p string) string {
		if rel, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		if rel, err := filepath.Rel(adopted, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "接管来的服/" + filepath.ToSlash(rel)
		}
		return filepath.Base(p)
	}
	res := Startup(
		base,
		filepath.Join(base, "cache"),
		filepath.Join(base, "runtimes"),
		[]string{filepath.Join(base, "backups")},
		[]Instance{{Dir: srv, Name: "S1"}, {Dir: adopted, Name: "接管服"}},
		time.Hour,
	)
	if res.Files != len(gone) {
		t.Errorf("清了 %d 个，应为 %d 个", res.Files, len(gone))
	}
	for _, p := range gone {
		if exists(p) {
			t.Errorf("应清掉：%s", short(p))
		}
	}
	for _, p := range stay {
		if !exists(p) {
			t.Errorf("不该动：%s", short(p))
		}
	}
}

// A18.1：备份存放位置改到程序文件夹以外（含曾用旧备份根）、实例目录名与显示名不同（如中文服名）时，
// 只清每台服务器实际用的备份文件夹这一层，且旧版 .backup-<全数字>.tmp 严格按名字、位置和修改时间清。
func TestStartupBackupDirsStrictRules(t *testing.T) {
	base := t.TempDir()
	customRoot := filepath.Join(t.TempDir(), "自定义备份根")
	oldRoot := filepath.Join(t.TempDir(), "旧备份根")
	defRoot := filepath.Join(base, "backups")

	// 中文名服务器的文件夹名是 server-xxx，但备份文件夹按传入的实例名建在「生存服」下
	srv := filepath.Join(base, "servers", "server-20261122-120000")

	curBk := filepath.Join(customRoot, "生存服")
	oldBk := filepath.Join(oldRoot, "生存服")
	defBk := filepath.Join(defRoot, "生存服")

	const stale = 48 * time.Hour
	gone := []string{
		filepath.Join(curBk, ".minedeck-backup-1001.tmp"),
		filepath.Join(curBk, ".backup-1002.tmp"),
		filepath.Join(oldBk, ".backup-1003.tmp"),
		filepath.Join(defBk, ".minedeck-backup-1004.tmp"),
	}
	for _, p := range gone {
		put(t, p, stale)
	}

	// 刚写的不清（< 1 小时）
	fresh := []string{
		filepath.Join(curBk, ".backup-2001.tmp"),
		filepath.Join(curBk, ".minedeck-backup-2002.tmp"),
	}
	for _, p := range fresh {
		put(t, p, time.Minute)
	}

	// 子文件夹里的不清、名字不严格匹配的不清、别处的 .backup-*.tmp 不清、真实备份 zip 不清
	stay := []string{
		filepath.Join(curBk, "sub", ".backup-1.tmp"),                            // 子文件夹里的 .backup-1.tmp 不清
		filepath.Join(curBk, "sub", ".minedeck-backup-1.tmp"),                   // 子文件夹里的 .minedeck-backup-1.tmp 也不清（只看这一层）
		filepath.Join(curBk, ".backup-abc.tmp"),                                 // 非全数字不清
		filepath.Join(curBk, ".backup-.tmp"),                                    // 空随机段不清
		filepath.Join(curBk, ".backup-12a3.tmp"),                                // 混入字母不清
		filepath.Join(curBk, ".backup-123.tmp.bak"),                             // 多后缀不清
		filepath.Join(curBk, "my.backup-123.tmp"),                               // 前面多字不清
		filepath.Join(curBk, "20261122-120000-日常.zip"),                          // 正式备份文件不动
		filepath.Join(customRoot, ".backup-3001.tmp"),                           // 备份根顶层不动
		filepath.Join(customRoot, "server-20261122-120000", ".backup-3002.tmp"), // 按目录名而非实例名的旁路文件夹不动
		filepath.Join(base, "cache", ".backup-3003.tmp"),                        // 缓存目录里的 .backup-*.tmp 不动
		filepath.Join(srv, "mods", ".backup-3004.tmp"),                          // 服务器目录里的 .backup-*.tmp 不动
	}
	for _, p := range stay {
		put(t, p, stale)
	}

	label := func(p string) string {
		for _, pair := range []struct{ root, tag string }{
			{customRoot, "<customRoot>"},
			{oldRoot, "<oldRoot>"},
			{base, "<base>"},
		} {
			if rel, err := filepath.Rel(pair.root, p); err == nil && !strings.HasPrefix(rel, "..") {
				return pair.tag + "/" + filepath.ToSlash(rel)
			}
		}
		return filepath.Base(p)
	}

	res := Startup(
		base,
		filepath.Join(base, "cache"),
		filepath.Join(base, "runtimes"),
		[]string{customRoot, defRoot, oldRoot},
		[]Instance{{Dir: srv, Name: "生存服"}},
		time.Hour,
	)
	if res.Files != len(gone) {
		t.Errorf("清了 %d 个，应为 %d 个", res.Files, len(gone))
	}
	for _, p := range gone {
		if exists(p) {
			t.Errorf("应清掉：%s", label(p))
		}
	}
	for _, p := range append(fresh, stay...) {
		if !exists(p) {
			t.Errorf("不该动：%s", label(p))
		}
	}
}

// R2.1：异常实例名（..、.、a/..、C:、CON、S1::$DATA、空串、a/S1）一律不得算成备份目录，
// 程序文件夹、备份根顶层、备份根的上一层、别的服的备份文件夹里的文件都原封不动。
func TestStartupRejectsBadInstanceNames(t *testing.T) {
	cases := []struct {
		title string
		name  string
	}{
		{"双点越级", ".."},
		{"单点当前目录", "."},
		{"带子目录的双点", "a/.."},
		{"盘符写法", "C:"},
		{"保留设备名", "CON"},
		{"NTFS备用数据流", "S1::$DATA"},
		{"空串", ""},
		{"带分隔符指别的服", "a/S1"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			if runtime.GOOS != "windows" && tc.name == "CON" {
				t.Skip("CON 仅在 Windows 上是保留设备名")
			}
			if _, ok := backupSubName(tc.name); ok {
				t.Errorf("backupSubName(%q) = true，异常名字应拒绝", tc.name)
			}
			outer := t.TempDir()
			base := filepath.Join(outer, "program")
			defRoot := filepath.Join(base, "backups")
			bkParent := filepath.Join(outer, "bk-parent")
			customRoot := filepath.Join(bkParent, "bk-root")
			srv := filepath.Join(base, "servers", "bad")
			if err := os.MkdirAll(srv, 0o755); err != nil {
				t.Fatal(err)
			}

			// 程序文件夹本身由 Dir(base) 清 IsOurs，这里摆 .backup-1.tmp 验证不会被 .. 当成备份目录清掉；
			// 备份根顶层、备份根上一层、别的服的备份文件夹均各摆一份 .backup-<数字>.tmp 和 .minedeck-backup-<数字>.tmp。
			protected := []string{
				filepath.Join(base, ".backup-1.tmp"),
				filepath.Join(defRoot, ".backup-2.tmp"),
				filepath.Join(defRoot, ".minedeck-backup-2.tmp"),
				filepath.Join(customRoot, ".backup-3.tmp"),
				filepath.Join(customRoot, ".minedeck-backup-3.tmp"),
				filepath.Join(bkParent, ".backup-4.tmp"),
				filepath.Join(bkParent, ".minedeck-backup-4.tmp"),
				filepath.Join(defRoot, "S1", ".backup-5.tmp"),
				filepath.Join(defRoot, "S1", ".minedeck-backup-5.tmp"),
				filepath.Join(customRoot, "S1", ".backup-6.tmp"),
				filepath.Join(customRoot, "S1", ".minedeck-backup-6.tmp"),
			}
			for _, p := range protected {
				put(t, p, 48*time.Hour)
			}

			res := Startup(
				base,
				filepath.Join(base, "cache"),
				filepath.Join(base, "runtimes"),
				[]string{defRoot, customRoot},
				[]Instance{{Dir: srv, Name: tc.name}},
				time.Hour,
			)
			if res.Files != 0 {
				t.Errorf("name=%q 时不应删掉任何文件，实际删了 %d 个", tc.name, res.Files)
			}
			for _, p := range protected {
				if !exists(p) {
					rel, _ := filepath.Rel(outer, p)
					t.Errorf("name=%q 时误删了 %s", tc.name, filepath.ToSlash(rel))
				}
			}
		})
	}
}

// R2.2：备份根本身是目录联接时，Startup 不穿过联接去删另一头子文件夹里的文件。
func TestStartupSkipsJunctionBackupRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 的东西")
	}
	outer := t.TempDir()
	base := filepath.Join(outer, "app")
	srv := filepath.Join(base, "servers", "S1")
	if err := os.MkdirAll(srv, 0o755); err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(outer, "真实外部备份根")
	kept := []string{
		filepath.Join(targetRoot, "S1", ".backup-101.tmp"),
		filepath.Join(targetRoot, "S1", ".minedeck-backup-102.tmp"),
	}
	for _, p := range kept {
		put(t, p, 48*time.Hour)
	}
	juncRoot := filepath.Join(outer, "联接备份根")
	makeJunction(t, juncRoot, targetRoot)

	res := Startup(
		base,
		filepath.Join(base, "cache"),
		filepath.Join(base, "runtimes"),
		[]string{juncRoot},
		[]Instance{{Dir: srv, Name: "S1"}},
		time.Hour,
	)
	if res.Files != 0 {
		t.Errorf("备份根是目录联接时不应删文件，实际删了 %d 个", res.Files)
	}
	for _, p := range kept {
		if !exists(p) {
			rel, _ := filepath.Rel(outer, p)
			t.Errorf("穿过联接备份根误删了：%s", filepath.ToSlash(rel))
		}
	}
}

// R3：随机段位数边界（os.CreateTemp 的 uint32 最多 10 位）：1 位和 10 位照清，11 位和 14 位（如时间戳）一律不动，新旧名字各一套。
func TestDigitLengthBounds(t *testing.T) {
	base := t.TempDir()
	srv := filepath.Join(base, "servers", "S1")
	bk := filepath.Join(base, "backups", "S1")
	const stale = 48 * time.Hour

	gone := []string{
		filepath.Join(srv, "mods", ".minedeck-download-0.part"),
		filepath.Join(srv, "mods", ".minedeck-download-4294967295.part"),
		filepath.Join(bk, ".minedeck-backup-7.tmp"),
		filepath.Join(bk, ".minedeck-backup-4294967295.tmp"),
		filepath.Join(bk, ".backup-7.tmp"),
		filepath.Join(bk, ".backup-4294967295.tmp"),
	}
	stay := []string{
		filepath.Join(srv, "mods", ".minedeck-download-42949672950.part"),    // 11 位
		filepath.Join(srv, "mods", ".minedeck-download-20261122153000.part"), // 14 位
		filepath.Join(bk, ".minedeck-backup-42949672950.tmp"),                // 11 位
		filepath.Join(bk, ".minedeck-backup-20261122153000.tmp"),             // 14 位
		filepath.Join(bk, ".backup-42949672950.tmp"),                         // 旧名 11 位
		filepath.Join(bk, ".backup-20261122153000.tmp"),                      // 旧名 14 位
	}
	for _, p := range append(append([]string{}, gone...), stay...) {
		put(t, p, stale)
	}

	res := Startup(
		base,
		filepath.Join(base, "cache"),
		filepath.Join(base, "runtimes"),
		[]string{filepath.Join(base, "backups")},
		[]Instance{{Dir: srv, Name: "S1"}},
		time.Hour,
	)
	if res.Files != len(gone) {
		t.Errorf("清了 %d 个，应为 %d 个", res.Files, len(gone))
	}
	for _, p := range gone {
		if exists(p) {
			rel, _ := filepath.Rel(base, p)
			t.Errorf("1 位与 10 位的临时文件应清掉：%s", filepath.ToSlash(rel))
		}
	}
	for _, p := range stay {
		if !exists(p) {
			rel, _ := filepath.Rel(base, p)
			t.Errorf("超过 10 位的名字不应被清：%s", filepath.ToSlash(rel))
		}
	}
}

// A21：用户自己起 .minedeck- 开头的正常文件名（以及同前后缀但中间不是纯数字的名字）一律不会被清。
func TestUserNamedMinedeckFilesKept(t *testing.T) {
	base := t.TempDir()
	srv := filepath.Join(base, "servers", "S1")
	bk := filepath.Join(base, "backups", "S1")

	userNames := []string{
		".minedeck-我的备份.zip",
		".minedeck-notes.txt",
		".minedeck-backup-20261122.zip",
		".minedeck-write-notes.tmp",
		".minedeck-backup-手动留底.tmp",
		".minedeck-download-modpack.part",
	}
	var allPaths []string
	for _, name := range userNames {
		if IsOurs(name) {
			t.Errorf("IsOurs(%q) = true，用户起的名字不应算临时文件", name)
		}
		for _, dir := range []string{base, srv, filepath.Join(srv, "mods"), filepath.Join(srv, "config", "mymod"), bk} {
			p := filepath.Join(dir, name)
			put(t, p, 48*time.Hour)
			allPaths = append(allPaths, p)
		}
	}

	res := Startup(
		base,
		filepath.Join(base, "cache"),
		filepath.Join(base, "runtimes"),
		[]string{filepath.Join(base, "backups")},
		[]Instance{{Dir: srv, Name: "S1"}},
		time.Hour,
	)
	if res.Files != 0 {
		t.Errorf("用户自己起的 .minedeck-* 文件一个都不该删，实际删了 %d 个", res.Files)
	}
	for _, p := range allPaths {
		if !exists(p) {
			rel, _ := filepath.Rel(base, p)
			t.Errorf("用户文件被误删：%s", filepath.ToSlash(rel))
		}
	}
}

// 不跟着目录联接、符号链接走到别的文件夹里去删；CI 上建不了联接直接判失败，不允许静默跳过。
func TestTreeStaysInside(t *testing.T) {
	if shouldSkipJunction("true", []byte("You do not have sufficient privilege")) {
		t.Fatal("CI 非空时建不了目录联接不应跳过")
	}
	if !shouldSkipJunction("", []byte("You do not have sufficient privilege")) {
		t.Fatal("本机缺少权限时应允许跳过")
	}
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 的东西")
	}
	base := t.TempDir()
	root, outside := filepath.Join(base, "cache"), filepath.Join(base, "别人的文件夹")
	theirs := filepath.Join(outside, ".minedeck-download-123.part")
	put(t, theirs, 48*time.Hour)
	put(t, filepath.Join(root, "x", ".minedeck-download-456.part"), 48*time.Hour)
	link := filepath.Join(root, "link")
	makeJunction(t, link, outside)
	if res := Tree(root, 5, time.Hour); res.Files != 1 {
		t.Fatalf("只该删树里自己的那一个：%+v", res)
	}
	if !exists(theirs) {
		t.Fatal("目录联接指过去的文件夹里的东西不该动")
	}
	// 直接点名一个目录联接（比如服务器的 mods 联接到共用的模组库）也不清：里面的东西不归这台服务器管
	if res := Dir(filepath.Join(root, "link"), time.Hour); res.Files != 0 || !exists(theirs) {
		t.Fatalf("点名的文件夹本身是目录联接时不该清：%+v", res)
	}
	if res := Tree(filepath.Join(root, "link"), 2, time.Hour); res.Files != 0 || !exists(theirs) {
		t.Fatalf("从目录联接开始的树也不该清：%+v", res)
	}

	// 服务器深层子文件夹、服务器的备份文件夹若是目录联接，Startup 也一律不走进去删
	theirsLegacy := filepath.Join(outside, ".backup-789.tmp")
	put(t, theirsLegacy, 48*time.Hour)
	srv := filepath.Join(base, "servers", "S1")
	if err := os.MkdirAll(filepath.Join(srv, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	srvLink := filepath.Join(srv, "config", "shared")
	makeJunction(t, srvLink, outside)
	if err := os.MkdirAll(filepath.Join(base, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	bkLink := filepath.Join(base, "backups", "S1")
	makeJunction(t, bkLink, outside)
	if res := Startup(base, filepath.Join(base, "cache"), filepath.Join(base, "runtimes"), []string{filepath.Join(base, "backups")}, []Instance{{Dir: srv, Name: "S1"}}, time.Hour); res.Files != 0 || !exists(theirs) || !exists(theirsLegacy) {
		t.Fatalf("Startup 不应穿过服务器子目录或备份目录的联接删外部文件：%+v", res)
	}
}

// 还开着的文件删不掉就跳过，不报错（Windows）。
func TestInUseIsSkipped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("靠 Windows 上「打开着的文件删不掉」")
	}
	root := t.TempDir()
	p := filepath.Join(root, ".minedeck-download-123.part")
	put(t, p, 48*time.Hour)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	res := Tree(root, 0, time.Hour)
	f.Close()
	if res.Files != 0 || !exists(p) {
		t.Fatalf("还开着的不该删：%+v", res)
	}
	if res := Tree(root, 0, time.Hour); res.Files != 1 {
		t.Fatalf("放开以后下一遍清掉：%+v", res)
	}
}
