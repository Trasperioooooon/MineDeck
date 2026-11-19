package tmpsweep

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 只认 MineDeck 自己起的那几种名字：前缀、一串数字、后缀都要对上。
func TestIsOurs(t *testing.T) {
	for name, want := range map[string]bool{
		".minedeck-download-123456789.part":             true,
		".minedeck-download-1.download":                 true,
		".minedeck-copy-4081234567.copy":                true,
		".minedeck-write-99.tmp":                        true,
		".minedeck-unzip-123.part":                      true,
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
		".backup-123.tmp":                               false,
		"server.jar":                                    false,
		"world.zip.part":                                false,
		".minedeck-write-1234567890123456789012345.tmp": false, // 长得不像 CreateTemp 填的
		"": false,
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

// 程序启动后那一遍清哪些地方：程序文件夹这一层、data、缓存、运行时，服务器文件夹的根上和 mods、plugins、config；
// 世界文件夹里的、程序文件夹里别的子文件夹里的不翻。
func TestStartup(t *testing.T) {
	base := t.TempDir()
	srv, adopted := filepath.Join(base, "servers", "S1"), filepath.Join(t.TempDir(), "接管来的服")
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
		filepath.Join(adopted, ".minedeck-download-10.part"),
		filepath.Join(adopted, "config", ".minedeck-write-11.tmp"),
	}
	stay := []string{
		filepath.Join(srv, "world", "region", ".minedeck-write-12.tmp"), // 世界里的不翻
		filepath.Join(srv, "mods", "sub", ".minedeck-copy-13.copy"),     // mods 只看一层
		filepath.Join(base, "backups", "S1", ".minedeck-write-14.tmp"),  // 备份文件夹不在范围里
		filepath.Join(base, "servers", ".minedeck-write-15.tmp"),        // 没登记的文件夹不管
		filepath.Join(srv, "server.jar"),
		filepath.Join(base, "config.json"),
	}
	for _, p := range append(append([]string{}, gone...), stay...) {
		put(t, p, old)
	}
	res := Startup(base, filepath.Join(base, "cache"), filepath.Join(base, "runtimes"), []string{srv, adopted}, time.Hour)
	if res.Files != len(gone) {
		t.Errorf("清了 %d 个，应为 %d 个", res.Files, len(gone))
	}
	for _, p := range gone {
		if exists(p) {
			t.Errorf("应清掉：%s", p)
		}
	}
	for _, p := range stay {
		if !exists(p) {
			t.Errorf("不该动：%s", p)
		}
	}
}

// 不跟着目录联接、符号链接走到别的文件夹里去删。
func TestTreeStaysInside(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 的东西")
	}
	base := t.TempDir()
	root, outside := filepath.Join(base, "cache"), filepath.Join(base, "别人的文件夹")
	theirs := filepath.Join(outside, ".minedeck-download-123.part")
	put(t, theirs, 48*time.Hour)
	put(t, filepath.Join(root, "x", ".minedeck-download-456.part"), 48*time.Hour)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "link"), outside).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
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
