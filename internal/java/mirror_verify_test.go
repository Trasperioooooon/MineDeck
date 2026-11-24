package java

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
)

func makeJREZipBytes(t *testing.T, marker string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range []struct{ name, body string }{
		{"jdk-17.0.14+7-jre/bin/" + exeName, "jvm-" + marker}, // 程序名跟着平台走：Windows 是 java.exe，Mac 是 java
		{"jdk-17.0.14+7-jre/lib/modules", strings.Repeat(marker, 1024)},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestFromTUNARequiresOfficialAdoptiumSHA256AndPicksNewest(t *testing.T) {
	runtimesIn(t)
	oldCache, oldBase := app.CacheDir, app.Base
	app.CacheDir = filepath.Join(t.TempDir(), "cache")
	app.Base = t.TempDir()
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })
		app.CacheDir, app.Base = oldCache, oldBase
	})

	oldMin := javaMinZipSize
	javaMinZipSize = 64
	t.Cleanup(func() { javaMinZipSize = oldMin })

	goodZip, goodSHA := makeJREZipBytes(t, "good")
	badZip, fakeMirrorSHA := makeJREZipBytes(t, "tampered")

	const oldName = "OpenJDK17U-jre_x64_windows_hotspot_17.0.9_9.zip"
	const newName = "OpenJDK17U-jre_x64_windows_hotspot_17.0.14_7.zip"

	serveZip := goodZip
	var adoptiumName, adoptiumSHA string
	var requestedZip string

	tunaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/17/jre/x64/windows/":
			// 字母序里 17.0.9_9 排在 17.0.14_7 后面；并故意放一份伪造的 .sha256.txt 和非法穿越 href
			_, _ = w.Write([]byte(
				`<a href="OpenJDK17U-jre_x64_windows_hotspot_17.0.14_7.zip">14</a>` +
					`<a href="OpenJDK17U-jre_x64_windows_hotspot_17.0.9_9.zip">9</a>` +
					`<a href="OpenJDK17U-jre_../evil.zip">evil</a>`))
		case "/17/jre/x64/windows/" + newName + ".sha256.txt":
			_, _ = w.Write([]byte(fakeMirrorSHA + "  " + newName + "\n"))
		case "/17/jre/x64/windows/" + newName:
			requestedZip = newName
			_, _ = w.Write(serveZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer tunaSrv.Close()

	adoptSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/latest/17/") {
			if adoptiumName == "" {
				_ = json.NewEncoder(w).Encode([]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"binary": map[string]any{
					"package": map[string]any{"name": adoptiumName, "checksum": adoptiumSHA},
				},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer adoptSrv.Close()

	oldTUNA, oldAdopt := tunaAdoptiumBase, adoptiumAPIBase
	tunaAdoptiumBase, adoptiumAPIBase = tunaSrv.URL, adoptSrv.URL
	t.Cleanup(func() { tunaAdoptiumBase, adoptiumAPIBase = oldTUNA, oldAdopt })

	// 1. Adoptium 没有给出匹配文件名的官方 SHA-256：即使清华镜像自己有 .sha256.txt 也必须拒绝
	if err := fromTUNA(context.Background(), 17, func(string, ...any) {}, nil); err == nil {
		t.Fatal("缺少官方 SHA-256 时不应采用镜像自证的压缩包")
	}
	if requestedZip != "" {
		t.Fatalf("没有官方校验值时连 zip 都不应去下载，实际请求了 %q", requestedZip)
	}

	// 2. Adoptium 给出了官方 SHA-256，但镜像返回被篡改的内容：校验失败，不留残留文件和目录
	adoptiumName, adoptiumSHA = newName, goodSHA
	serveZip = badZip
	err := fromTUNA(context.Background(), 17, func(string, ...any) {}, nil)
	if !errors.Is(err, dl.ErrVerify) {
		t.Fatalf("内容被篡改应报哈希校验失败：%v", err)
	}
	if requestedZip != newName {
		t.Fatalf("应按版本号选最新的 17.0.14 而非字典序末尾的 17.0.9，实际选了 %q", requestedZip)
	}
	if _, statErr := os.Lstat(filepath.Join(app.CacheDir, "java", newName)); !os.IsNotExist(statErr) {
		t.Fatalf("校验失败后不应留下缓存 zip：%v", statErr)
	}
	ents, _ := os.ReadDir(filepath.Join(app.CacheDir, "java"))
	if len(ents) != 0 {
		t.Fatalf("校验失败后缓存目录不应有 .part 残留：%v", ents)
	}

	// 3. 镜像内容与 Adoptium 官方 SHA-256 一致：成功解压部署
	serveZip = goodZip
	if err := fromTUNA(context.Background(), 17, func(string, ...any) {}, nil); err != nil {
		t.Fatalf("官方校验通过后应成功：%v", err)
	}
	if got, rerr := os.ReadFile(Exe(17)); rerr != nil || string(got) != "jvm-good" {
		t.Fatalf("解压结果不对：%v %q", rerr, got)
	}
}

func TestFromAdoptiumUsesImageTypeInMirrorPathAndHonorsOfficialOnly(t *testing.T) {
	runtimesIn(t)
	oldCache, oldBase := app.CacheDir, app.Base
	app.CacheDir = filepath.Join(t.TempDir(), "cache")
	app.Base = t.TempDir()
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })
		app.CacheDir, app.Base = oldCache, oldBase
	})

	oldMin := javaMinZipSize
	javaMinZipSize = 64
	t.Cleanup(func() { javaMinZipSize = oldMin })

	jdkZip, jdkSHA := makeJREZipBytes(t, "jdk21")
	const jdkName = "OpenJDK21U-jdk_x64_windows_hotspot_21.0.5_11.zip"

	var tunaPaths []string
	officialHits := 0
	tunaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tunaPaths = append(tunaPaths, r.URL.Path)
		if r.URL.Path == "/21/jdk/x64/windows/"+jdkName {
			_, _ = w.Write(jdkZip)
			return
		}
		http.NotFound(w, r)
	}))
	defer tunaSrv.Close()

	adoptSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.RawQuery, "image_type=jre"):
			_ = json.NewEncoder(w).Encode([]any{}) // 没有 jre，回退到 jdk
		case strings.Contains(r.URL.RawQuery, "image_type=jdk"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"binary": map[string]any{
					"package": map[string]any{
						"name":     jdkName,
						"checksum": jdkSHA,
						"link":     r.Host, // 稍后改写为完整 URL
					},
				},
			}})
		case r.URL.Path == "/official/"+jdkName:
			officialHits++
			_, _ = w.Write(jdkZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer adoptSrv.Close()

	// 重新包一层让 link 指向 adoptSrv.URL + "/official/" + jdkName
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "image_type=jre") {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"binary": map[string]any{
				"package": map[string]any{
					"name":     jdkName,
					"checksum": jdkSHA,
					"link":     adoptSrv.URL + "/official/" + jdkName,
				},
			},
		}})
	}))
	defer apiSrv.Close()

	oldTUNA, oldAdopt := tunaAdoptiumBase, adoptiumAPIBase
	tunaAdoptiumBase, adoptiumAPIBase = tunaSrv.URL, apiSrv.URL
	t.Cleanup(func() { tunaAdoptiumBase, adoptiumAPIBase = oldTUNA, oldAdopt })

	// 默认模式：jdk 回退时应请求 /21/jdk/...（不是写死的 /jre/），命中清华镜像即可完成
	if err := fromAdoptium(context.Background(), 21, func(string, ...any) {}, nil); err != nil {
		t.Fatalf("fromAdoptium(auto) 失败：%v", err)
	}
	if len(tunaPaths) != 1 || tunaPaths[0] != "/21/jdk/x64/windows/"+jdkName {
		t.Fatalf("清华镜像路径不对（不应写死 /jre/）：%v", tunaPaths)
	}
	if officialHits != 0 {
		t.Fatalf("清华镜像校验通过后不应再下官方源：%d", officialHits)
	}

	// OfficialOnly 模式：完全不请求清华镜像，直接走官方地址
	tunaPaths = nil
	_ = os.RemoveAll(app.CacheDir)
	_ = os.RemoveAll(app.RuntimesDir)
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "official"; return nil })
	if _, err := Ensure(context.Background(), 21, func(string, ...any) {}, nil); err != nil {
		t.Fatalf("Ensure(official) 失败：%v", err)
	}
	if len(tunaPaths) != 0 || officialHits != 1 {
		t.Fatalf("OfficialOnly 不应访问清华镜像：tuna=%v official=%d", tunaPaths, officialHits)
	}
}

func TestJavaZipLessComparesVersionThenBuild(t *testing.T) {
	const p = "OpenJDK17U-jre_x64_windows_hotspot_"
	cases := []struct{ older, newer string }{
		{p + "17.0.9_9.zip", p + "17.0.14_7.zip"},
		{p + "17.0.21_9.zip", p + "17.0.21.1_2.zip"}, // 第四段的 .1 比构建号 9 要紧
		{"OpenJDK21U-jre_x64_windows_hotspot_21_35.zip", "OpenJDK21U-jre_x64_windows_hotspot_21.0.5_11.zip"},
		{p + "17.0.14_7.zip", p + "17.0.14_9.zip"},
		{"OpenJDK8U-jre_x64_windows_hotspot_8u92b14.zip", "OpenJDK8U-jre_x64_windows_hotspot_8u442b06.zip"},
		{"OpenJDK8U-jre_x64_windows_hotspot_8u442b06.zip", "OpenJDK8U-jre_x64_windows_hotspot_8u442b07.zip"},
		{"bellsoft-jre17.0.2.zip", "bellsoft-jre17.0.14.zip"}, // 没有 hotspot_ 的名字不能切在前缀的 b 上
	}
	for _, c := range cases {
		if !javaZipLess(c.older, c.newer) || javaZipLess(c.newer, c.older) {
			t.Errorf("应判 %s 比 %s 旧", c.older, c.newer)
		}
	}
}

// dialLog offlineNet 记下的拨号：拨过哪些主机（本机的 httptest 服务不算）。
type dialLog struct {
	mu    sync.Mutex
	hosts []string
}

func (d *dialLog) add(host string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hosts = append(d.hosts, host)
}

func (d *dialLog) count(host string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, h := range d.hosts {
		if h == host {
			n++
		}
	}
	return n
}

// offlineNet 把 dl.Client 换成拨号可控的客户端：本机的 httptest 服务照常连；hang 里的主机像被墙的地址一样挂住，
// 等到这一问的时限；别的主机一律当连接被拒。测试里不会连到外面去。
func offlineNet(t *testing.T, hang ...string) *dialLog {
	t.Helper()
	log := &dialLog{}
	stop := make(chan struct{})
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host == "127.0.0.1" {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		log.add(host)
		if slices.Contains(hang, host) {
			select {
			case <-ctx.Done():
			case <-stop:
			}
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
	}
	old := dl.Client
	dl.Client = &http.Client{Transport: &http.Transport{DialContext: dial}}
	t.Cleanup(func() { close(stop); dl.Client = old })
	return log
}

// sourcesIn 下载源设成 source（auto / official），缓存放进临时目录；用完改回 auto。
func sourcesIn(t *testing.T, source string) {
	t.Helper()
	runtimesIn(t)
	oldCache, oldBase := app.CacheDir, app.Base
	app.CacheDir = filepath.Join(t.TempDir(), "cache")
	app.Base = t.TempDir() // 改下载源会存配置：存进临时目录
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.Source = source; return nil }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })
		app.CacheDir, app.Base = oldCache, oldBase
	})
}

// 清华镜像和 Adoptium 接口都连不上（或者下载源选「官方」而 Adoptium 接口连不上）时：第一次问 Adoptium 的接口就确认连不上，
// 同一个主机的下一问（jdk）不再问，很快走到 Mojang 运行时，任务日志里说明（A35）。原来这一步按默认时限等，两问各两轮三十秒。
func TestEnsureSkipsAdoptiumWhenUnreachable(t *testing.T) {
	const tunaHost, apiHost = "mirror.tuna.example", "api.adoptium.example"
	for name, source := range map[string]string{"两个源都连不上": "auto", "下载源选官方": "official"} {
		t.Run(name, func(t *testing.T) {
			sourcesIn(t, source)
			dials := offlineNet(t, apiHost)
			oldTUNA, oldAdopt, oldWait := tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait
			tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait = "https://"+tunaHost+"/Adoptium", "https://"+apiHost+"/v3/assets", 300*time.Millisecond
			t.Cleanup(func() { tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait = oldTUNA, oldAdopt, oldWait })

			var logMu sync.Mutex
			var logs []string
			logf := func(f string, a ...any) { logMu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); logMu.Unlock() }
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			start := time.Now()
			_, err := Ensure(ctx, 17, logf, nil)
			if err == nil {
				t.Fatal("哪个源都连不上，不该装成")
			}
			if d := time.Since(start); d > 3*time.Second {
				t.Fatalf("Adoptium 接口连不上时等了 %v，应很快改用 Mojang 运行时", d)
			}
			if n := dials.count(apiHost); n != 1 {
				t.Fatalf("Adoptium 的接口应只问一次（jdk 那一问跳过），实际拨了 %d 次", n)
			}
			if dials.count("launchermeta.mojang.com") == 0 {
				t.Fatalf("应走到 Mojang 运行时那一步，拨过的主机：%v", dials.hosts)
			}
			if source == "official" && dials.count(tunaHost) != 0 {
				t.Fatal("下载源选官方时不该问清华镜像")
			}
			if all := strings.Join(logs, "\n"); !strings.Contains(all, "Adoptium 的接口连不上，直接改用 Mojang") {
				t.Fatalf("任务日志里应说明直接改用 Mojang 运行时：\n%s", all)
			}
		})
	}
}

// Adoptium 的接口回了话、只是没有（404）：不算连不上，清华那条路要不到校验值以后，Adoptium 官方源那条路照常试，jre 没有再问 jdk（A35）。
func TestEnsureKeepsAdoptiumWhenItReplies(t *testing.T) {
	sourcesIn(t, "auto")
	offlineNet(t)
	const name = "OpenJDK17U-jre_x64_windows_hotspot_17.0.14_7.zip"
	tunaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/17/jre/x64/windows/" {
			_, _ = w.Write([]byte(`<a href="` + name + `">z</a>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer tunaSrv.Close()
	var mu sync.Mutex
	var asked []string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer apiSrv.Close()
	oldTUNA, oldAdopt := tunaAdoptiumBase, adoptiumAPIBase
	tunaAdoptiumBase, adoptiumAPIBase = tunaSrv.URL, apiSrv.URL
	t.Cleanup(func() { tunaAdoptiumBase, adoptiumAPIBase = oldTUNA, oldAdopt })

	if _, err := Ensure(context.Background(), 17, func(string, ...any) {}, nil); err == nil {
		t.Fatal("哪个源都没有，不该装成")
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(asked, "\n")
	if !strings.Contains(all, "feature_releases") || !strings.Contains(all, "image_type=jdk") {
		t.Fatalf("接口回了 404 不算连不上：校验值的两问和官方源的 jre、jdk 都该问到，实际：\n%s", all)
	}
}

// Adoptium 的接口连不上（请求挂住不回）时，清华这条路要在短时限内放弃，并报成「连不上接口」，
// 让 Ensure 不再走同一个接口的 Adoptium 官方源空等；也不去下清华上的压缩包。
func TestFromTUNAGivesUpQuicklyWhenAdoptiumUnreachable(t *testing.T) {
	runtimesIn(t)
	oldCache := app.CacheDir
	app.CacheDir = filepath.Join(t.TempDir(), "cache")
	t.Cleanup(func() { app.CacheDir = oldCache })

	const name = "OpenJDK17U-jre_x64_windows_hotspot_17.0.14_7.zip"
	zipHits := 0
	tunaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/17/jre/x64/windows/":
			_, _ = w.Write([]byte(`<a href="` + name + `">z</a>`))
		case "/17/jre/x64/windows/" + name:
			zipHits++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer tunaSrv.Close()
	release := make(chan struct{})
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // 像被墙的地址一样不回
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer apiSrv.Close()
	defer close(release)

	oldTUNA, oldAdopt, oldWait := tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait
	tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait = tunaSrv.URL, apiSrv.URL, 300*time.Millisecond
	t.Cleanup(func() { tunaAdoptiumBase, adoptiumAPIBase, adoptiumIndexWait = oldTUNA, oldAdopt, oldWait })

	start := time.Now()
	err := fromTUNA(context.Background(), 17, func(string, ...any) {}, nil)
	if !errors.Is(err, errAdoptiumUnreachable) {
		t.Fatalf("接口连不上应报 errAdoptiumUnreachable，实际：%v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("接口连不上时等了 %v，应在两个短时限内放弃", d)
	}
	if zipHits != 0 {
		t.Fatalf("没有官方校验值时不应去下清华上的压缩包，请求了 %d 次", zipHits)
	}
}
