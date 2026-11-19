package tunnel

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
		if got, why := inspectExe(p); got != c.want || (got == exeOK) != (why == "") {
			t.Errorf("%s：inspectExe = %d %q，应为 %d", c.name, got, why, c.want)
		}
	}
	if got, _ := inspectExe(filepath.Join(t.TempDir(), "没有这个文件.exe")); got != exeUnknown {
		t.Errorf("文件不在：%d，应判断不了", got)
	}
	dir := filepath.Join(t.TempDir(), "frpc.exe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := inspectExe(dir); got != exeUnknown {
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
