package tunnel

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/procutil"
)

// 这一组测下载、解压 frpc 出错以后的样子：以前直接往正式文件名里写，解压到一半出错就留下半截的 frpc exe，
// 下次 findExe 找到它照用；OpenFrp 的 zip 缓存只看大小，下坏了的每次都复用；下载地址和跳转不限 https。

type frpEntry struct{ name, body string }

// frpZip 造一个 frp 发行包那样带一层目录的 zip。不压缩：内容原样在包里，breakEntry 才找得到、改得坏。
func frpZip(t *testing.T, files ...frpEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// breakEntry 把包里 body 那段内容改掉一个字节：大小不变、CRC 对不上，读到这个文件的结尾时报错。
func breakEntry(t *testing.T, b []byte, body string) []byte {
	t.Helper()
	i := bytes.Index(b, []byte(body))
	if i < 0 || bytes.Index(b[i+1:], []byte(body)) >= 0 {
		t.Fatalf("包里找不到唯一的一段 %q", body[:min(len(body), 20)])
	}
	out := slices.Clone(b)
	out[i+len(body)/2] ^= 0xff
	return out
}

func writeZip(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "frp.zip")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// dirState 文件夹里有什么：文件名 → 内容。
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		out[e.Name()] = string(b)
	}
	return out
}

func sameDir(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

const (
	frpcBody = "MZ frpc 的内容（假的）"
	frpsBody = "MZ frps 的内容，这一份会被改坏"
)

// frpDir 一个已经有用户自己的文件的 frpc 文件夹：一份配置、一份同名的 LICENSE（包里也有，不该被覆盖）。
func frpDir(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "frpc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, b := range map[string]string{"my.toml": "用户自己的配置", "LICENSE": "旧的 LICENSE"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, dirState(t, dir)
}

// 包里第二个要写的文件坏了（CRC 错）：已经写出来的 frpc 也不留，文件夹回到解压前的样子，下次 findExe 找不到半截的 exe。
func TestExtractZipFlatBadEntryLeavesNothing(t *testing.T) {
	dir, before := frpDir(t)
	good := frpZip(t, frpEntry{"frp_x/frpc.exe", frpcBody}, frpEntry{"frp_x/LICENSE", "新的 LICENSE"}, frpEntry{"frp_x/frps.exe", frpsBody})
	names, err := extractZipFlat(writeZip(t, breakEntry(t, good, frpsBody)), dir)
	if err == nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("坏了的包应报 CRC 错：%v %v", names, err)
	}
	if after := dirState(t, dir); !sameDir(after, before) {
		t.Fatalf("解压失败后文件夹应和解压前一样：\n前：%v\n后：%v", before, after)
	}
	if exe := findExe(dir); exe != "" {
		t.Fatalf("不该留下能被找到的 frpc：%s", exe)
	}
}

// 解压到一半盘满了（写完第一个文件以后余量变成 0）：同样什么都不留。
func TestExtractZipFlatDiskFullLeavesNothing(t *testing.T) {
	dir, before := frpDir(t)
	old := unzipDisk
	t.Cleanup(func() { unzipDisk = old })
	unzipDisk.Query = func(string) (int64, error) { // 有一个临时文件写完了（不是空的）就没地方了
		parts, _ := filepath.Glob(filepath.Join(dir, unzipPart))
		for _, p := range parts {
			if st, err := os.Stat(p); err == nil && st.Size() > 0 {
				return 0, nil
			}
		}
		return 1 << 40, nil
	}
	_, err := extractZipFlat(writeZip(t, frpZip(t, frpEntry{"frp_x/frpc.exe", frpcBody}, frpEntry{"frp_x/frps.exe", frpsBody})), dir)
	var full *diskguard.NoSpace
	if !errors.As(err, &full) {
		t.Fatalf("应报盘满：%v", err)
	}
	if after := dirState(t, dir); !sameDir(after, before) {
		t.Fatalf("盘满以后文件夹应和解压前一样：\n前：%v\n后：%v", before, after)
	}
}

// 都写完了、改正式名字时第二个失败：第一个已经改了名的也删掉。
func TestExtractZipFlatRenameFailureRollsBack(t *testing.T) {
	dir, before := frpDir(t)
	old := renameFile
	t.Cleanup(func() { renameFile = old })
	n := 0
	renameFile = func(from, to string) error {
		if n++; n == 2 {
			return errors.New("改名被拦下了")
		}
		return os.Rename(from, to)
	}
	_, err := extractZipFlat(writeZip(t, frpZip(t, frpEntry{"frp_x/frpc.exe", frpcBody}, frpEntry{"frp_x/frps.exe", frpsBody})), dir)
	if err == nil || n != 2 {
		t.Fatalf("第二次改名失败应报错：%v（改了 %d 次）", err, n)
	}
	if after := dirState(t, dir); !sameDir(after, before) {
		t.Fatalf("改名失败后文件夹应和解压前一样：\n前：%v\n后：%v", before, after)
	}
}

// 正常解压：内容对；解压前就在的同名文件不覆盖；上次被打断留下的临时文件清掉；没有临时文件留下。
func TestExtractZipFlatPlacesFiles(t *testing.T) {
	dir, _ := frpDir(t)
	stale := filepath.Join(dir, ".minedeck-unzip-123.part")
	if err := os.WriteFile(stale, []byte("上次留下的"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := extractZipFlat(writeZip(t, frpZip(t, frpEntry{"frp_x/frpc.exe", frpcBody}, frpEntry{"frp_x/LICENSE", "新的 LICENSE"}, frpEntry{"frp_x/frps.exe", frpsBody})), dir)
	if err != nil || !slices.Equal(names, []string{"frpc.exe", "frps.exe"}) {
		t.Fatalf("解压：%v %v", names, err)
	}
	want := map[string]string{"my.toml": "用户自己的配置", "LICENSE": "旧的 LICENSE", "frpc.exe": frpcBody, "frps.exe": frpsBody}
	if got := dirState(t, dir); !sameDir(got, want) {
		t.Fatalf("解压后的文件夹：%v", got)
	}
}

// 清上次留下的临时文件只看这一个文件夹：文件夹名里带 [ ] 时不能被当成通配符，删到旁边文件夹里的东西（2026-11-18 Codex 复审）。
func TestExtractZipFlatStaleCleanupStaysInFolder(t *testing.T) {
	root := t.TempDir()
	dir, other := filepath.Join(root, "MineDeck[1]", "frpc"), filepath.Join(root, "MineDeck1", "frpc")
	mine, theirs := filepath.Join(dir, ".minedeck-unzip-1.part"), filepath.Join(other, ".minedeck-unzip-2.part")
	for _, p := range []string{mine, theirs} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("上次留下的"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := extractZipFlat(writeZip(t, frpZip(t, frpEntry{"frp_x/frpc.exe", frpcBody})), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Errorf("这个文件夹里上次留下的应清掉：%v", err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Errorf("旁边文件夹里的不该动：%v", err)
	}
}

// frpcFake 假的 OpenFrp 接口和下载源。所有请求经 rewrite 指到它（地址里的主机名留着，方便看请求的是哪儿）。
type frpcFake struct {
	mu      sync.Mutex
	full    string            // 接口返回的 latest_full
	sources []string          // 接口返回的下载源
	files   map[string][]byte // 路径 → 内容
	redir   map[string]string // 路径 → 跳到哪儿（302）
	hits    []string
	srv     *httptest.Server
}

func newFrpcFake(t *testing.T) *frpcFake {
	t.Helper()
	f := &frpcFake{full: "OF_t1", sources: []string{"https://frpc.test/client"}, files: map[string][]byte{}, redir: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits = append(f.hits, r.Host+r.URL.Path)
		if r.URL.Path == "/commonQuery/get" {
			src := make([]map[string]string, 0, len(f.sources))
			for _, s := range f.sources {
				src = append(src, map[string]string{"value": s})
			}
			b, _ := json.Marshal(map[string]any{"data": map[string]any{"latest_full": f.full, "source": src}})
			_, _ = w.Write(b)
			return
		}
		if to, ok := f.redir[r.URL.Path]; ok {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		if b, ok := f.files[r.URL.Path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	target, _ := url.Parse(f.srv.URL)
	rewrite := openfrpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme, clone.URL.Host = target.Scheme, target.Host
		clone.Host = r.URL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
	// 只换传输层：openfrpHTTPClient 自己的跳转规矩（只跟 https）留着
	oldDL, oldOF := dl.Client, openfrpHTTPClient
	of := *openfrpHTTPClient
	of.Transport = rewrite
	dl.Client, openfrpHTTPClient = &http.Client{Transport: rewrite}, &of
	t.Cleanup(func() { dl.Client, openfrpHTTPClient = oldDL, oldOF })
	oldRT, oldCache := app.RuntimesDir, app.CacheDir
	base := t.TempDir()
	app.RuntimesDir, app.CacheDir = filepath.Join(base, "runtimes"), filepath.Join(base, "cache")
	t.Cleanup(func() { app.RuntimesDir, app.CacheDir = oldRT, oldCache })
	return f
}

func (f *frpcFake) set(fn func(f *frpcFake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *frpcFake) hitCount(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, h := range f.hits {
		if strings.Contains(h, sub) {
			n++
		}
	}
	return n
}

// frpPad 凑到 1 MiB 以上的填充（dl.Fetch 对 frpc 的 zip 要求不小于 1 MiB）。
var frpPad = strings.Repeat("填充", 200_000)

// openfrpZip OpenFrp 的发行包：一个 frpc_windows_amd64.exe，后面跟一个填充文件。
func openfrpZip(t *testing.T, exe string) []byte {
	t.Helper()
	return frpZip(t, frpEntry{"frpc_windows_amd64.exe", exe}, frpEntry{"README.txt", frpPad})
}

// OpenFrp 下回来的 zip 里第二个文件坏了：先写出来的 frpc 也不留，frpc 文件夹里什么都没有，缓存的 zip 也删掉；
// 下次启动重新下载，下好的就能用。
func TestOpenfrpBadZipIsDroppedAndRedownloaded(t *testing.T) {
	f := newFrpcFake(t)
	exe := "MZ OpenFrp frpc 一号"
	good := openfrpZip(t, exe)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) { f.files[zipURL] = breakEntry(t, good, frpPad) })
	if _, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole()); err == nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("坏 zip 应解压失败：%v", err)
	}
	dir := frpcDir("openfrp")
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("解压失败后 frpc 文件夹里不该留东西：%v", ents)
	}
	if _, err := os.Stat(filepath.Join(app.CacheDir, "frpc", "openfrp-frpc-OF_t1.zip")); !os.IsNotExist(err) {
		t.Fatalf("解压失败的 zip 应删掉，不然下次照样复用：%v", err)
	}

	f.set(func(f *frpcFake) { f.files[zipURL] = good })
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || got != filepath.Join(dir, "frpc_windows_amd64.exe") || readFile(t, got) != exe {
		t.Fatalf("重新下载以后应能用：%s %v", got, err)
	}
	if n := f.hitCount(zipURL); n != 2 {
		t.Fatalf("应重新下载一次（一共两次），实际 %d 次", n)
	}
}

// 换了版本（latest_full 变了）：缓存的 zip 名字跟着变，删掉 frpc 想升级时下的是新的，不复用旧 zip。
func TestOpenfrpNewVersionDoesNotReuseOldZip(t *testing.T) {
	f := newFrpcFake(t)
	f.set(func(f *frpcFake) { f.files["/client/OF_t1/"+openfrpFrpcZip] = openfrpZip(t, "MZ 旧版") })
	if _, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(frpcDir("openfrp")); err != nil {
		t.Fatal(err)
	}
	f.set(func(f *frpcFake) {
		f.full = "OF_t2"
		f.files["/client/OF_t2/"+openfrpFrpcZip] = openfrpZip(t, "MZ 新版")
	})
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err != nil || readFile(t, got) != "MZ 新版" {
		t.Fatalf("换了版本应下新的：%v", err)
	}
	for _, n := range []string{"openfrp-frpc-OF_t1.zip", "openfrp-frpc-OF_t2.zip"} {
		if _, err := os.Stat(filepath.Join(app.CacheDir, "frpc", n)); err != nil {
			t.Errorf("缓存里应有 %s：%v", n, err)
		}
	}
}

// 下载源只认 https：接口给的源里有 http 的跳过，全是 http 的不下载；latest_full 带奇怪字符的不下载。
func TestOpenfrpFrpcURLsOnlyHTTPS(t *testing.T) {
	meta := func(full string, src ...string) openfrpSoftwareMeta {
		var m openfrpSoftwareMeta
		m.Data.LatestFull = full
		for _, s := range src {
			m.Data.Source = append(m.Data.Source, struct {
				Label string `json:"label"`
				Value string `json:"value"`
			}{Value: s})
		}
		return m
	}
	got, err := openfrpFrpcURLs(meta("OF_1", "http://plain.test/client", "https://ok.test/client", "ftp://x.test/c", "https:///nohost"))
	if err != nil || !slices.Equal(got, []string{"https://ok.test/client/OF_1/" + openfrpFrpcZip}) {
		t.Fatalf("只该留 https 的：%v %v", got, err)
	}
	if _, err := openfrpFrpcURLs(meta("OF_1", "http://plain.test/client", "HTTP://b.test/c")); err == nil || !strings.Contains(err.Error(), "不是 https") {
		t.Fatalf("全是 http 的应拒绝：%v", err)
	}
	for _, full := range []string{"..", ".", "OF/../x", `OF\x`, "OF 1", "OF_1?x=1", "OF%2F..", "版本", "OF:1", strings.Repeat("a", 81)} {
		if _, err := openfrpFrpcURLs(meta(full, "https://ok.test/client")); err == nil || !strings.Contains(err.Error(), "不认识的字符") {
			t.Errorf("latest_full %q 应拒绝：%v", full, err)
		}
	}
	if got, err := openfrpFrpcURLs(meta("OF_0.68.0_37f78258-260326", "https://ok.test/client")); err != nil || len(got) != 1 {
		t.Fatalf("正常的版本号：%v %v", got, err)
	}
}

// 下 frpc 时被跳转到 http 的地址：不跟，下载失败，那个地址一次都没请求；跳到 https 的照常跟。
func TestOpenfrpRefusesRedirectToHTTP(t *testing.T) {
	f := newFrpcFake(t)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) { f.redir[zipURL] = "http://evil.test/frpc.zip" })
	_, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err == nil || !strings.Contains(err.Error(), "重定向到不允许的地址") {
		t.Fatalf("跳到 http 应中断：%v", err)
	}
	if n := f.hitCount("evil.test"); n != 0 {
		t.Fatalf("不该去请求 http 的地址：%d 次", n)
	}
	if ents, _ := os.ReadDir(frpcDir("openfrp")); len(ents) != 0 {
		t.Fatalf("下载失败不该留下文件：%v", ents)
	}

	f.set(func(f *frpcFake) {
		f.redir[zipURL] = "https://cdn.test/real.zip"
		f.files["/real.zip"] = openfrpZip(t, "MZ 跳过去下到的")
	})
	if got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole()); err != nil || readFile(t, got) != "MZ 跳过去下到的" {
		t.Fatalf("跳到 https 的应照常跟：%v", err)
	}
}

// 自定义 frps：下回来的标准版 frp 解压失败，frpc 文件夹同样回到解压前的样子（用户放的东西不动、不留半截的 exe）。
// 这个 zip 按官方 SHA-256 核过，坏的不是它（多半是盘满），所以留着，下次不用重新下载。
func TestCustomFrpcExtractFailureLeavesNothing(t *testing.T) {
	f := newFrpcFake(t)
	good := frpZip(t, frpEntry{"frp_" + frpVersion + "_windows_amd64/frpc.exe", frpcBody}, frpEntry{"frp_x/frps.exe", frpsBody},
		frpEntry{"frp_x/pad", frpPad})
	bad := breakEntry(t, good, frpsBody)
	sum := sha256.Sum256(bad)
	old := frpSHA256
	t.Cleanup(func() { frpSHA256 = old })
	frpSHA256 = hex.EncodeToString(sum[:])
	file := "/fatedier/frp/releases/download/v" + frpVersion + "/frp_" + frpVersion + "_windows_amd64.zip"
	f.set(func(f *frpcFake) {
		f.files[file] = bad
		f.files["/https://github.com"+file] = bad // ghfast.top / gh-proxy.com 是把 GitHub 地址整个接在后面
	})
	dir := frpcDir("custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tun1.toml"), []byte("隧道配置"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := dirState(t, dir)
	if _, err := (customProvider{}).EnsureFrpc(procutil.NewConsole()); err == nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("解压应失败：%v", err)
	}
	if after := dirState(t, dir); !sameDir(after, before) {
		t.Fatalf("解压失败后文件夹应和解压前一样：\n前：%v\n后：%v", before, after)
	}
	if findExe(dir) != "" {
		t.Fatal("不该留下能被找到的 frpc")
	}
}
