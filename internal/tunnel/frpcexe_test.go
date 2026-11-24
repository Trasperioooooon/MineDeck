package tunnel

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/procutil"
)

// 这一组测「找到 frpc 以后、用之前认出明显坏掉的」，以及 OpenFrp 查版本那一步只跟 https 的跳转（v3.2.2 之后的三块问题，第二块）。

// fakeFrpcExe 一个结构完整的假 exe（1024 字节）：DOS 头、PE 头、一节 512 字节的数据。不能真的运行，只为过结构检查。
// 用到「一个放好了的 frpc」的测试都拿它当假 frpc：以前随手写几个字就行，现在那样的会被当成坏文件。
func fakeFrpcExe() []byte {
	b := make([]byte, 1024)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[60:], 0x40) // PE 头在哪
	pe := b[0x40:]
	copy(pe, "PE\x00\x00")
	binary.LittleEndian.PutUint16(pe[4:], 0x8664) // amd64
	binary.LittleEndian.PutUint16(pe[6:], 1)      // 一节
	binary.LittleEndian.PutUint16(pe[20:], 240)   // 可选头的大小
	binary.LittleEndian.PutUint16(pe[22:], 0x22)  // 可执行、支持大地址
	binary.LittleEndian.PutUint16(pe[24:], 0x20b) // PE32+
	sec := pe[24+240:]
	copy(sec, ".text")
	binary.LittleEndian.PutUint32(sec[8:], 512)     // 内存里的大小
	binary.LittleEndian.PutUint32(sec[12:], 0x1000) // 内存里的位置
	binary.LittleEndian.PutUint32(sec[16:], 512)    // 文件里的大小
	binary.LittleEndian.PutUint32(sec[20:], 512)    // 文件里的位置
	for k := 512; k < 1024; k++ {
		b[k] = 0xCC
	}
	return b
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// 认坏 exe：完整的不误伤；截断的（空的、头都不全的、节的数据缺了的）算半截；网页、压缩包这些算「不是 Windows 程序」；读不了的判断不了。
func TestInspectExe(t *testing.T) {
	good := fakeFrpcExe()
	noPE := slices.Clone(good)
	copy(noPE[0x40:], "XX\x00\x00")
	farPE := slices.Clone(good[:64])
	binary.LittleEndian.PutUint32(farPE[60:], 4096) // PE 头的位置在文件外面：开头是程序，后面没了
	noSections := slices.Clone(good)
	binary.LittleEndian.PutUint16(noSections[0x40+6:], 0)
	bss := slices.Clone(good) // 文件里不占地方的节不看
	binary.LittleEndian.PutUint32(bss[0x40+24+240+16:], 0)
	binary.LittleEndian.PutUint32(bss[0x40+24+240+20:], 0)
	for _, c := range []struct {
		name string
		body []byte
		want exeState
	}{
		{"结构完整", good, exeOK},
		{"后面还跟着别的数据（签名、附加段）", append(slices.Clone(good), make([]byte, 300)...), exeOK},
		{"不占文件的节", bss[:512], exeOK},
		{"空文件", nil, exeTruncated},
		{"只有 MZ", []byte("MZ"), exeTruncated},
		{"DOS 头都不全", good[:40], exeTruncated},
		{"PE 头不全", good[:0x40+10], exeTruncated},
		{"节表不全", good[:0x40+24+240+20], exeTruncated},
		{"节的数据缺了一半", good[:768], exeTruncated},
		{"只少最后一个字节", good[:1023], exeTruncated},
		{"PE 头的位置在文件外面", farPE, exeTruncated},
		{"网页", []byte("<!DOCTYPE html><html><body>404 Not Found</body></html>"), exeNotPE},
		{"压缩包", []byte("PK\x03\x04 这是个 zip"), exeNotPE},
		{"随手写的几个字", []byte("fake"), exeNotPE},
		{"一个字节", []byte("M"), exeNotPE},
		{"开头是 MZ、后面不是 PE 头", noPE, exeNotPE},
		{"一节都没有", noSections, exeNotPE},
	} {
		p := filepath.Join(t.TempDir(), "frpc.exe")
		writeFile(t, p, c.body)
		if got, why, _ := inspectExe(p); got != c.want || (got == exeOK) != (why == "") {
			t.Errorf("%s：inspectExe = %d %q，应为 %d", c.name, got, why, c.want)
		}
	}
	if got, _, _ := inspectExe(filepath.Join(t.TempDir(), "没有这个文件.exe")); got != exeUnknown {
		t.Errorf("文件不在：%d，应判断不了", got)
	}
	dir := filepath.Join(t.TempDir(), "frpc.exe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := inspectExe(dir); got != exeUnknown {
		t.Errorf("是个文件夹：%d，应判断不了", got)
	}
}

// frpcTempDirs 把 runtimes、cache 指到临时文件夹。
func frpcTempDirs(t *testing.T) {
	t.Helper()
	oldRT, oldCache := app.RuntimesDir, app.CacheDir
	base := t.TempDir()
	app.RuntimesDir, app.CacheDir = filepath.Join(base, "runtimes"), filepath.Join(base, "cache")
	t.Cleanup(func() { app.RuntimesDir, app.CacheDir = oldRT, oldCache })
}

// 自己下载的 frpc（OpenFrp）：文件夹里留着半截的、不是程序的 exe 时，认出来、删掉、重新下载解压，用新的那份；
// 文件夹里别的文件不动。好的 frpc 直接用，不删、不联网。
func TestOpenfrpBrokenExeIsReplaced(t *testing.T) {
	good := string(fakeFrpcExe())
	for _, c := range []struct {
		name string
		old  []byte
	}{
		{"截断的", fakeFrpcExe()[:700]},
		{"空文件", nil},
		{"不是 PE 的", []byte("<html>下载站的报错页</html>")},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFrpcFake(t)
			zipURL := "/client/OF_t1/" + openfrpFrpcZip
			f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, good) })
			dir := frpcDir("openfrp")
			exe := filepath.Join(dir, "frpc_windows_amd64.exe")
			writeFile(t, exe, c.old)
			writeFile(t, filepath.Join(dir, "frpc.ini"), []byte("用户自己的文件，名字里也有 frpc"))
			writeFile(t, filepath.Join(dir, "notes.txt"), []byte("别的文件"))
			con := procutil.NewConsole()
			got, err := (openfrpProvider{}).EnsureFrpc(con)
			if err != nil || got != exe || readFile(t, got) != good {
				t.Fatalf("坏的应删掉、重新准备：%s %v", got, err)
			}
			if n := f.hitCount(zipURL); n != 1 {
				t.Fatalf("应重新下载一次，实际 %d 次", n)
			}
			if lines := strings.Join(con.Lines(), "\n"); !strings.Contains(lines, "发现坏掉的 frpc：frpc_windows_amd64.exe") || !strings.Contains(lines, "已经删掉") {
				t.Fatalf("控制台要说清楚：%s", lines)
			}
			if readFile(t, filepath.Join(dir, "frpc.ini")) != "用户自己的文件，名字里也有 frpc" || readFile(t, filepath.Join(dir, "notes.txt")) != "别的文件" {
				t.Fatal("别的文件不该动")
			}
			// 再来一次：现在是好的，直接用，不再下载
			if again, err := (openfrpProvider{}).EnsureFrpc(con); err != nil || again != exe || f.hitCount(zipURL) != 1 || f.hitCount("commonQuery") != 1 {
				t.Fatalf("好的 frpc 应直接用：%s %v（下载 %d 次）", again, err, f.hitCount(zipURL))
			}
		})
	}
}

// OpenFrp 的文件夹里，不是标准名的坏 exe（可能是人自己留着的东西）不用也不删，照样重新准备标准名的那份（2026-11-19 Codex 复审）。
func TestOpenfrpKeepsNonStandardBrokenExe(t *testing.T) {
	good := string(fakeFrpcExe())
	f := newFrpcFake(t)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, good) })
	dir := frpcDir("openfrp")
	mine := filepath.Join(dir, "old-frpc-tools.exe")
	writeFile(t, mine, []byte("<html>人自己留着的东西</html>"))
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || got != filepath.Join(dir, "frpc_windows_amd64.exe") || readFile(t, got) != good {
		t.Fatalf("应重新准备标准名的那份：%s %v", got, err)
	}
	if readFile(t, mine) != "<html>人自己留着的东西</html>" {
		t.Fatal("不是标准名的文件不该删")
	}
}

// 判坏了、不是标准名的 frpc 只是不删，这一轮解压完挑 frpc 时也不能又挑中它（A56）。
// 包里的 frpc 也不是标准名、排在它后面时，以前列目录挑到的是这个坏的。
func TestOpenfrpSkipsJudgedBrokenExeAfterExtract(t *testing.T) {
	good := string(fakeFrpcExe()) + "包里的"
	f := newFrpcFake(t)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) {
		f.files[zipURL] = frpZip(t, frpEntry{"frpc_b.exe", good}, frpEntry{"README.txt", frpPad})
	})
	dir := frpcDir("openfrp")
	old := filepath.Join(dir, "frpc_a_old.exe")
	writeFile(t, old, fakeFrpcExe()[:700])
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || got != filepath.Join(dir, "frpc_b.exe") || readFile(t, got) != good {
		t.Fatalf("应挑解压出来的那份，不是刚判坏的：%s %v", got, err)
	}
	if readFile(t, old) != string(fakeFrpcExe()[:700]) {
		t.Fatal("不是标准名的坏文件照旧不删")
	}
}

// 解压出来的 frpc 转眼不见了（被杀毒软件删了）：报「frpc 不见了」，不拿文件夹里判坏的那个顶上（A56）。
func TestOpenfrpGoneAfterExtractNotReplacedByJudgedBroken(t *testing.T) {
	f := newFrpcFake(t)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, string(fakeFrpcExe())) })
	dir := frpcDir("openfrp")
	old := filepath.Join(dir, "old-frpc.exe")
	writeFile(t, old, fakeFrpcExe()[:700])
	oldRename := renameFile
	t.Cleanup(func() { renameFile = oldRename })
	renameFile = func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if isFrpcExeName(filepath.Base(to)) {
			return os.Remove(to) // 改好名字的那一下就被删了
		}
		return nil
	}
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if got != "" || !errors.Is(err, errFrpcGone) {
		t.Fatalf("应报 frpc 不见了：%q %v", got, err)
	}
}

// 查出 frpc 坏了、到删它之间，文件被换成了好的（比如用户刚放进来一个）：不删，重新查，用换进来的这份，也不去下载（A56）。
// 原地改写（大小变了）和改名换进来（大小、修改时间都和原来一样，只有文件本身不同）两种都要认出来。
// 删前确认比的三样各有一条只靠它认得出的：改名换进来只靠「是不是同一个文件」，原地改写成一样长的只靠修改时间，
// 原地改写成不一样长、修改时间又被改回原样的只靠大小。
func TestBrokenFrpcReplacedBeforeDeleteIsKept(t *testing.T) {
	notPE := fakeFrpcExe()
	copy(notPE[0x40:], "XX\x00\x00") // 和好的一样长，开头是 MZ、后面不是 PE 头
	stat := func(t *testing.T, p string) os.FileInfo {
		t.Helper()
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	for _, c := range []struct {
		name    string
		old     []byte
		replace func(t *testing.T, path string) []byte
	}{
		{"原地改写成大小不同的", fakeFrpcExe()[:700], func(t *testing.T, path string) []byte {
			b := append(fakeFrpcExe(), "用户刚放的"...)
			writeFile(t, path, b)
			return b
		}},
		{"改名换进来、大小和修改时间都一样", notPE, func(t *testing.T, path string) []byte {
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			b := fakeFrpcExe()
			tmp := filepath.Join(filepath.Dir(path), "incoming.tmp")
			writeFile(t, tmp, b)
			if err := os.Chtimes(tmp, st.ModTime(), st.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}
			return b
		}},
		{"原地改写、大小一样、修改时间不同", notPE, func(t *testing.T, path string) []byte {
			before := stat(t, path)
			b := fakeFrpcExe()
			writeFile(t, path, b)
			later := before.ModTime().Add(2 * time.Second) // 明着设，不靠两次写之间隔了多久
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
			if now := stat(t, path); !os.SameFile(before, now) || now.Size() != before.Size() || now.ModTime().Equal(before.ModTime()) {
				t.Fatal("前提：同一个文件、大小没变、修改时间变了")
			}
			return b
		}},
		{"原地改写成大小不同、修改时间改回原样", fakeFrpcExe()[:700], func(t *testing.T, path string) []byte {
			before := stat(t, path)
			b := append(fakeFrpcExe(), "用户刚放的"...)
			writeFile(t, path, b)
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			if now := stat(t, path); !os.SameFile(before, now) || now.Size() == before.Size() || !now.ModTime().Equal(before.ModTime()) {
				t.Fatal("前提：同一个文件、修改时间没变、大小变了")
			}
			return b
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFrpcFake(t)
			zipURL := "/client/OF_t1/" + openfrpFrpcZip
			f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, string(fakeFrpcExe())+"下载的") })
			exe := filepath.Join(frpcDir("openfrp"), "frpc_windows_amd64.exe")
			writeFile(t, exe, c.old)
			var want []byte
			old := inspectFrpc
			t.Cleanup(func() { inspectFrpc = old })
			inspectFrpc = func(p string) (exeState, string, os.FileInfo) {
				state, why, seen := inspectExe(p)
				if want == nil {
					want = c.replace(t, p) // 查完、还没删的这会儿被换掉
				}
				return state, why, seen
			}
			got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
			if err != nil || got != exe || readFile(t, exe) != string(want) {
				t.Fatalf("换进来的好文件不该删，应直接用：%s %v", got, err)
			}
			if n := f.hitCount(".zip"); n != 0 {
				t.Fatalf("有好的就不该去下载：%d 次", n)
			}
		})
	}
}

// 一直在变的 frpc（有程序正往里写）：不删，当判断不了交回去，和读不了的一样由启动那一步报（A56）。
func TestBrokenFrpcKeepsChangingIsNotDeleted(t *testing.T) {
	frpcTempDirs(t)
	exe := filepath.Join(frpcDir("openfrp"), "frpc.exe")
	writeFile(t, exe, fakeFrpcExe()[:300])
	n := 0
	old := inspectFrpc
	t.Cleanup(func() { inspectFrpc = old })
	inspectFrpc = func(p string) (exeState, string, os.FileInfo) {
		state, why, seen := inspectExe(p)
		n++
		writeFile(t, p, fakeFrpcExe()[:300+n]) // 每查一次就又长了一点，还是半截的
		return state, why, seen
	}
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || got != exe || n != frpcRechecks {
		t.Fatalf("一直在变的应当判断不了、照旧交回去：%s %v（查了 %d 次）", got, err, n)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("不该删：%v", err)
	}
}

// 自定义 frps 的 frpc 也是自己下载的：坏的同样删掉、重新准备；frpc.exe 坏了、旁边另有一份好的，用好的那份。
func TestCustomBrokenExeIsReplaced(t *testing.T) {
	f := newFrpcFake(t)
	good := string(fakeFrpcExe())
	pkg := frpZip(t, frpEntry{"frp_" + frpVersion + "_windows_amd64/frpc.exe", good}, frpEntry{"frp_x/pad", frpPad})
	oldSum := frpSHA256
	t.Cleanup(func() { frpSHA256 = oldSum })
	frpSHA256 = sha256Hex(pkg)
	file := "/fatedier/frp/releases/download/v" + frpVersion + "/frp_" + frpVersion + "_windows_amd64.zip"
	f.set(func(f *frpcFake) {
		f.files[file] = pkg
		f.files["/https://github.com"+file] = pkg
	})
	dir := frpcDir("custom")
	exe := filepath.Join(dir, "frpc.exe")
	writeFile(t, exe, fakeFrpcExe()[:300])
	writeFile(t, filepath.Join(dir, "tun1.toml"), []byte("隧道配置"))
	con := procutil.NewConsole()
	got, err := (customProvider{}).EnsureFrpc(con)
	if err != nil || got != exe || readFile(t, got) != good || readFile(t, filepath.Join(dir, "tun1.toml")) != "隧道配置" {
		t.Fatalf("坏的应删掉、重新准备：%s %v", got, err)
	}
	if !strings.Contains(strings.Join(con.Lines(), "\n"), "发现坏掉的 frpc：frpc.exe") {
		t.Fatalf("控制台要说清楚：%q", con.Lines())
	}

	// frpc.exe 又坏了，旁边另有一份好的：删掉坏的，用好的，不下载
	hits := f.hitCount(".zip")
	writeFile(t, exe, []byte("坏了"))
	other := filepath.Join(dir, "frpc_windows_amd64.exe")
	writeFile(t, other, fakeFrpcExe())
	if got, err := (customProvider{}).EnsureFrpc(con); err != nil || got != other || f.hitCount(".zip") != hits {
		t.Fatalf("应改用旁边那份好的：%s %v", got, err)
	}
	if _, err := os.Stat(exe); !os.IsNotExist(err) {
		t.Fatalf("坏的那份应删掉：%v", err)
	}
}

// 樱花的 frpc 是用户自己放的。半截的确实没用了，删掉、请人重新放；拿不准的（不是 Windows 程序的东西、读不了的）不删：
// 前一种报出来让人自己处理，后一种照旧交给启动那一步。好的不动。
func TestNatfrpBrokenExe(t *testing.T) {
	frpcTempDirs(t)
	dir := frpcDir("natfrp")
	exe := filepath.Join(dir, "frpc.exe")

	// 好的：直接用
	writeFile(t, exe, fakeFrpcExe())
	con := procutil.NewConsole()
	if got, err := (natfrpProvider{}).EnsureFrpc(con); err != nil || got != exe || len(con.Lines()) != 0 {
		t.Fatalf("好的直接用：%s %v %q", got, err, con.Lines())
	}

	// 半截的：删掉，回到「请手动放置」
	writeFile(t, exe, fakeFrpcExe()[:900])
	got, err := (natfrpProvider{}).EnsureFrpc(con)
	if got != "" || err == nil || !strings.Contains(err.Error(), "手动放置樱花 frpc") {
		t.Fatalf("半截的删掉以后应请人重新放：%s %v", got, err)
	}
	if _, serr := os.Stat(exe); !os.IsNotExist(serr) {
		t.Fatalf("半截的应删掉：%v", serr)
	}
	if lines := strings.Join(con.Lines(), "\n"); !strings.Contains(lines, "发现坏掉的 frpc：frpc.exe") || !strings.Contains(lines, "请重新下载一份放进来") {
		t.Fatalf("控制台要说清楚：%s", lines)
	}

	// 拿不准的一：不是 Windows 程序的东西（下载成了压缩包）。不删，报出来
	zipLike := []byte("PK\x03\x04 用户把下载的压缩包直接改了名")
	writeFile(t, exe, zipLike)
	got, err = (natfrpProvider{}).EnsureFrpc(con)
	if got != "" || err == nil || !strings.Contains(err.Error(), "frpc.exe") || !strings.Contains(err.Error(), "MineDeck 没有动它") || !strings.HasSuffix(err.Error(), dir) {
		t.Fatalf("不是程序的应报出来：%s %v", got, err)
	}
	if readFile(t, exe) != string(zipLike) {
		t.Fatal("拿不准的不该删、不该改")
	}
	// 旁边另放了一份好的：用好的，那个拿不准的照旧留着
	other := filepath.Join(dir, "SakuraFrp_frpc_windows_amd64.exe")
	writeFile(t, other, fakeFrpcExe())
	if got, err := (natfrpProvider{}).EnsureFrpc(con); err != nil || got != other || readFile(t, exe) != string(zipLike) {
		t.Fatalf("旁边有好的就用好的：%s %v", got, err)
	}
	// 拿不准的二（读不了的）在 frpcexe_windows_test.go 里：要靠 Windows 的独占打开
}

// OpenFrp 查版本那一步被跳转到 http 的地址：不跟，报错，那个 http 地址一次都没请求，也没有去下载 frpc；跳到 https 的照常跟。
func TestOpenfrpVersionQueryRefusesRedirectToHTTP(t *testing.T) {
	frpcTempDirs(t)
	var mu sync.Mutex
	var hits []string
	redirect := "http://evil.test/software.json"
	meta, _ := json.Marshal(map[string]any{"data": map[string]any{"latest_full": "OF_t9",
		"source": []map[string]string{{"value": "https://frpc.test/client"}}}})
	pkg := openfrpZip(t, string(fakeFrpcExe()))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Host+r.URL.Path)
		to := redirect
		mu.Unlock()
		switch {
		case r.Host == "api.openfrp.net" && r.URL.Path == "/commonQuery/get":
			http.Redirect(w, r, to, http.StatusFound)
		case r.URL.Path == "/software.json":
			_, _ = w.Write(meta)
		case r.URL.Path == "/client/OF_t9/"+openfrpFrpcZip:
			_, _ = w.Write(pkg)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	rewrite := openfrpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme, clone.URL.Host = target.Scheme, target.Host
		clone.Host = r.URL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
	oldDL, oldOF := dl.Client, openfrpHTTPClient
	of := *openfrpHTTPClient
	of.Transport = rewrite
	dl.Client, openfrpHTTPClient = &http.Client{Transport: rewrite}, &of // dl.Client 自己不限跳转：限制是查版本那一步自己加的
	t.Cleanup(func() { dl.Client, openfrpHTTPClient = oldDL, oldOF })
	count := func(sub string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, h := range hits {
			if strings.Contains(h, sub) {
				n++
			}
		}
		return n
	}

	_, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err == nil || !strings.Contains(err.Error(), "获取 OpenFrp 客户端版本失败") || !strings.Contains(err.Error(), "重定向到不允许的地址") {
		t.Fatalf("查版本被跳到 http 应中断：%v", err)
	}
	if count("evil.test") != 0 || count(".zip") != 0 {
		t.Fatalf("http 的地址一次都不该请求，也不该去下载：%q", hits)
	}
	if ents, _ := os.ReadDir(frpcDir("openfrp")); len(ents) != 0 {
		t.Fatalf("不该留下文件：%v", ents)
	}

	mu.Lock()
	redirect = "https://cdn.openfrp.test/software.json"
	mu.Unlock()
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || readFile(t, got) != string(fakeFrpcExe()) || count("cdn.openfrp.test/software.json") != 1 {
		t.Fatalf("跳到 https 的应照常跟：%s %v %q", got, err, hits)
	}
}

// 樱花的文件夹里 frpc.exe 是个指向的东西已经不在了的链接、旁边另有一份好的 frpc_windows_amd64.exe：跳过链接、用好的那份，
// 链接不动（同 v3.2.2 的 findExe，2026-11-19 Codex 复审第二轮）。
func TestNatfrpSkipsDanglingLink(t *testing.T) {
	frpcTempDirs(t)
	dir := frpcDir("natfrp")
	good := filepath.Join(dir, "frpc_windows_amd64.exe")
	writeFile(t, good, fakeFrpcExe())
	link := filepath.Join(dir, "frpc.exe")
	if err := os.Symlink(filepath.Join(dir, "已经不在了.exe"), link); err != nil {
		if os.Getenv("CI") == "" && runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("建不了符号链接（Windows 上要开发者模式或管理员）：%v", err)
		}
		t.Fatalf("建符号链接失败：%v", err)
	}
	got, err := (natfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || got != good {
		t.Fatalf("应跳过失效的链接、用好的那份：%s %v", got, err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("链接不该被动：%v", err)
	}
}
