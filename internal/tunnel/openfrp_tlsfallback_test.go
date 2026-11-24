package tunnel

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/procutil"
)

// 这一组测 OpenFrp 的 frpc 下载在只报 TLS 1.3 握不上手时的退路（A55）：下载源只会 TLS 1.2 时退回兼容的连接方式能下成；
// 退回的那次跳转照样只跟 https；不是握手的毛病（404）、下载源会 TLS 1.3 的，不退回。
// 测试用的两个客户端都是从产品里的克隆出来、只加了测试证书，TLS 版本、跳转规矩都是原样。

const tlsFallbackNote = "TLS 1.3 连不上，改用兼容的连接方式重试"

// tlsSource 本机的 https 下载源。maxTLS 为 0 时 TLS 1.3 也会。hits 是进到处理函数的请求数：握手没成的进不来。
type tlsSource struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func newTLSSource(t *testing.T, maxTLS uint16, h http.HandlerFunc) *tlsSource {
	t.Helper()
	s := &tlsSource{}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		h(w, r)
	}))
	s.srv.TLS = &tls.Config{MaxVersion: maxTLS}
	s.srv.Config.ErrorLog = log.New(io.Discard, "", 0) // 握手失败是预料之中的，不往测试输出里写
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	return s
}

func (s *tlsSource) zipURL() string { return s.srv.URL + "/client/OF_t1/" + openfrpFrpcZip }

// httptestCert httptest 起的 https 服务器共用的那张自带测试证书。哪天不是同一张了，下面的下载用例都会因为证书对不上失败，不会悄悄通过。
func httptestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	s := httptest.NewTLSServer(http.NotFoundHandler())
	defer s.Close()
	return s.Certificate()
}

// trustingCopy c 的副本：传输层克隆一份，只加上测试证书（一张都不给时不加）。
func trustingCopy(c *http.Client, certs ...*x509.Certificate) *http.Client {
	tr := c.Transport.(*http.Transport).Clone()
	if len(certs) > 0 {
		tr.TLSClientConfig.RootCAs = x509.NewCertPool()
		for _, cert := range certs {
			tr.TLSClientConfig.RootCAs.AddCert(cert)
		}
	}
	cp := *c
	cp.Transport = tr
	return &cp
}

// trustTestCert 两个下载客户端都换成认得 cert 的副本，测完（连同并行的子用例）换回。
func trustTestCert(t *testing.T, cert *x509.Certificate) {
	t.Helper()
	oldOF, oldCompat := openfrpHTTPClient, openfrpCompatHTTPClient
	openfrpHTTPClient, openfrpCompatHTTPClient = trustingCopy(oldOF, cert), trustingCopy(oldCompat, cert)
	t.Cleanup(func() { openfrpHTTPClient, openfrpCompatHTTPClient = oldOF, oldCompat })
}

// installOpenfrpFrom 照 EnsureFrpc 查完版本以后的样子，从这些地址下载、解压 OpenFrp 的 frpc 到一个临时文件夹；
// 交回那个文件夹、结果和控制台里的话。文件夹和 zip 的位置直接给，不经 frpcDir、app.CacheDir：子用例并行跑，不能改包变量。
func installOpenfrpFrom(t *testing.T, zipURLs ...string) (dir, exe, lines string, err error) {
	t.Helper()
	con := procutil.NewConsole()
	base := t.TempDir()
	dir = filepath.Join(base, "runtimes", "frpc", "openfrp")
	exe, err = openfrpInstall(t.Context(), con, zipURLs, filepath.Join(base, "cache", "frpc", "openfrp-frpc-OF_t1.zip"), dir, nil)
	return dir, exe, strings.Join(con.Lines(), "\n"), err
}

// TestOpenfrpZipTLSFallback 退路的几条下载用例。每条都要等 dl.Fetch 整次下载失败一到两次，每次固定等两段轮间间隔（2 秒、4 秒，
// dl 的退避，包外调不了），所以并行跑，一遍的时间是最慢那条（两次都失败的，12 秒左右）。
// 两个下载客户端换成认得测试证书的副本只在这里做一次，子用例里不改任何包变量。
func TestOpenfrpZipTLSFallback(t *testing.T) {
	trustTestCert(t, httptestCert(t))
	for _, c := range []struct {
		name string
		run  func(t *testing.T)
	}{
		{"FallsBackWhenOnlyTLS12", testZipFallsBackWhenOnlyTLS12},
		{"FallsBackWhenAnEarlierSourceFailsHandshake", testZipFallsBackWhenAnEarlierSourceFailsHandshake},
		{"FallbackRefusesRedirectToHTTP", testZipFallbackRefusesRedirectToHTTP},
		{"FallbackFailureNamesBothTries", testZipFallbackFailureNamesBothTries},
		{"NoFallbackWhenTLS13Works", testZipNoFallbackWhenTLS13Works},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.run(t)
		})
	}
}

// 下载源只会 TLS 1.2（网络出口只支持到 1.2 时就是这样）：只报 TLS 1.3 的那次握不上手，退回以后下成，
// 解压、结构检查照常过，frpc 能用；控制台说一句换了连接方式。
func testZipFallsBackWhenOnlyTLS12(t *testing.T) {
	exe := string(fakeFrpcExe()) + "退回以后下到的"
	pkg := openfrpZip(t, exe)
	src := newTLSSource(t, tls.VersionTLS12, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pkg) })

	dir, got, lines, err := installOpenfrpFrom(t, src.zipURL())
	if err != nil || got != filepath.Join(dir, "frpc_windows_amd64.exe") || readFile(t, got) != exe {
		t.Fatalf("退回以后应下成、能用：%q %v\n%s", got, err, lines)
	}
	if !strings.Contains(lines, tlsFallbackNote) {
		t.Errorf("控制台要说换了连接方式：%s", lines)
	}
	if n := src.hits.Load(); n != 1 {
		t.Errorf("只报 TLS 1.3 的那几次握不上手、进不来，退回以后下一次就够：实际进来 %d 次", n)
	}
}

// 握不上手的不是最后一个下载源，也退回：第一个源只会 TLS 1.2、放着好的 zip，第二个会 TLS 1.3、回 404。
// dl 交回的错误只认得出最后一个源的 404；退不退看的是途中有没有握手失败，所以照样退回，从第一个源下成。
func testZipFallsBackWhenAnEarlierSourceFailsHandshake(t *testing.T) {
	exe := string(fakeFrpcExe()) + "第一个源的"
	pkg := openfrpZip(t, exe)
	only12 := newTLSSource(t, tls.VersionTLS12, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pkg) })
	modern := newTLSSource(t, 0, http.NotFound)

	_, got, lines, err := installOpenfrpFrom(t, only12.zipURL(), modern.zipURL())
	if err != nil || readFile(t, got) != exe {
		t.Fatalf("退回以后应从第一个源下成：%q %v\n%s", got, err, lines)
	}
	if !strings.Contains(lines, tlsFallbackNote) {
		t.Errorf("控制台要说换了连接方式：%s", lines)
	}
	if a, b := only12.hits.Load(), modern.hits.Load(); a != 1 || b != 3 {
		t.Errorf("第一个源只有退回那次进得来（1 次），第二个源只有只报 TLS 1.3 的三轮：实际 %d、%d 次", a, b)
	}
}

// 退回的那次跳转照样只跟 https：下载源（只会 TLS 1.2）把 zip 跳到一个 http 的地址，那边真放着一个好的 zip，也不去拿；
// 下载失败，frpc 文件夹里不留 frpc。
func testZipFallbackRefusesRedirectToHTTP(t *testing.T) {
	pkg := openfrpZip(t, string(fakeFrpcExe()))
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write(pkg)
	}))
	t.Cleanup(plain.Close)
	src := newTLSSource(t, tls.VersionTLS12, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/frpc.zip", http.StatusFound)
	})

	dir, got, lines, err := installOpenfrpFrom(t, src.zipURL())
	if err == nil || !strings.Contains(err.Error(), "重定向到不允许的地址") {
		t.Fatalf("退回以后跳到 http 也应中断：%q %v", got, err)
	}
	if !strings.Contains(lines, tlsFallbackNote) || src.hits.Load() == 0 {
		t.Fatalf("应是退回以后才被拒的，不然测不到退回的那个客户端：进来 %d 次\n%s", src.hits.Load(), lines)
	}
	if n := plainHits.Load(); n != 0 {
		t.Fatalf("不该去请求 http 的地址：%d 次", n)
	}
	if exe := findExe(dir); exe != "" {
		t.Fatalf("下载失败不该留下 frpc：%s", exe)
	}
}

// 退回以后还是没下成（直连 EdgeOne 时多半是这样：Go 默认的握手会被当成机器人，回 567）：报错先说手动放 frpc 的办法，
// 再说两种方式都试过了、各自为什么没成。往外传的是退回那次的错误，dl.AnyFailure 逐个看得到它的每一次尝试
// （frpcBlockedHint 靠这个认「写盘时被杀毒软件拦下」）。
func testZipFallbackFailureNamesBothTries(t *testing.T) {
	src := newTLSSource(t, tls.VersionTLS12, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(567) })

	dir, _, _, err := installOpenfrpFrom(t, src.zipURL())
	if err == nil {
		t.Fatal("应该下载失败")
	}
	msg := err.Error()
	guide, both := strings.Index(msg, "frpc_windows_amd64.exe 放入 "+dir), strings.Index(msg, "两种连接方式都试过了")
	if guide < 0 || both < guide {
		t.Fatalf("先说手动放 frpc 的办法，再说两种方式都试过了：%s", msg)
	}
	for _, want := range []string{"只用 TLS 1.3 时：", "protocol version not supported", "改用兼容的连接方式后：", "HTTP 567（被源站 CDN 的机器人防护拦截）"} {
		if !strings.Contains(msg[both:], want) {
			t.Errorf("报错缺少 %q：%s", want, msg)
		}
	}
	if n := src.hits.Load(); n != 3 {
		t.Errorf("只有退回的那次进得来（dl 一次下载试三轮）：实际 %d 次", n)
	}
	if !dl.AnyFailure(err, func(e error) bool { return e.Error() == "HTTP 567" }) {
		t.Errorf("dl.AnyFailure 应看得到退回那次的每一次尝试：%v", err)
	}
}

// 不是握手的毛病不退回：下载源会 TLS 1.3、回 404 时照原样报错，控制台不说换连接方式，只有只报 TLS 1.3 的那三轮请求；
// 下成的时候也没有退回的事。
func testZipNoFallbackWhenTLS13Works(t *testing.T) {
	pkg := openfrpZip(t, string(fakeFrpcExe()))
	var serve atomic.Bool
	src := newTLSSource(t, 0, func(w http.ResponseWriter, r *http.Request) {
		if !serve.Load() {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(pkg)
	})

	_, _, lines, err := installOpenfrpFrom(t, src.zipURL())
	if err == nil || !strings.Contains(err.Error(), "HTTP 404（该源没有这个文件）") || strings.Contains(err.Error(), "两种连接方式") {
		t.Fatalf("404 应照原样报错、不退回：%v", err)
	}
	if strings.Contains(lines, tlsFallbackNote) || src.hits.Load() != 3 {
		t.Fatalf("404 不该退回：进来 %d 次\n%s", src.hits.Load(), lines)
	}

	serve.Store(true)
	_, got, lines, err := installOpenfrpFrom(t, src.zipURL())
	if err != nil || got == "" || strings.Contains(lines, tlsFallbackNote) || src.hits.Load() != 4 {
		t.Fatalf("下成了就没有退回的事：%q %v，进来 %d 次\n%s", got, err, src.hits.Load(), lines)
	}
}

// 哪些握手失败算「换个 TLS 版本可能就好了」：对方只会 TLS 1.2（回告警；或者像不认新扩展的老式服务器、中间设备那样，
// 直接回 TLS 1.2 的 ServerHello）、读完 ClientHello 就掐断连接、回的不是 TLS，都算；握手超时、证书对不上不算；
// 连不上（没走到握手）和握手成功的，什么都不记。
func TestOpenfrpHandshakeFailuresWorthFallback(t *testing.T) {
	ok := newTLSSource(t, 0, http.NotFound)
	only12 := newTLSSource(t, tls.VersionTLS12, http.NotFound)
	cases := []struct {
		name      string
		url       string
		trust     []*x509.Certificate
		hsTimeout time.Duration
		handshake bool // 有没有走到握手
		want      bool // 该不该记下
	}{
		{"只会 TLS 1.2，回告警", only12.srv.URL, []*x509.Certificate{only12.srv.Certificate()}, 0, true, true},
		{"老式服务器直接回 TLS 1.2 的 ServerHello", rawTLSPeer(t, answerTLS12Hello), nil, 0, true, true},
		{"读完 ClientHello 就掐断连接", rawTLSPeer(t, resetAfterHello), nil, 0, true, true},
		{"回的不是 TLS", rawTLSPeer(t, answerPlainHTTP), nil, 0, true, true},
		{"握手超时", rawTLSPeer(t, stayQuiet), nil, 200 * time.Millisecond, true, false},
		{"证书对不上", ok.srv.URL, nil, 0, true, false},
		{"连不上", closedPortURL(t), nil, 0, false, false},
		{"握手成功", ok.srv.URL, []*x509.Certificate{ok.srv.Certificate()}, 0, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := trustingCopy(openfrpHTTPClient, c.trust...)
			if c.hsTimeout > 0 {
				cl.Transport.(*http.Transport).TLSHandshakeTimeout = c.hsTimeout
			}
			var handshook atomic.Bool // 另挂一个 trace 看握手到底走没走到，免得「不该记」的几条白白通过
			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
				TLSHandshakeDone: func(tls.ConnectionState, error) { handshook.Store(true) },
			})
			ctx, handshakeErr := watchTLSHandshake(ctx)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := cl.Do(req)
			if err == nil {
				resp.Body.Close()
			}
			if handshook.Load() != c.handshake {
				t.Fatalf("握手走到没有：%v，应为 %v（请求的结果 %v）", handshook.Load(), c.handshake, err)
			}
			if got := handshakeErr(); (got != nil) != c.want {
				t.Fatalf("记下的握手错误：%v，该不该记：%v（请求的结果 %v）", got, c.want, err)
			}
		})
	}
}

// rawTLSPeer 起一个只收 TCP 连接、按 serve 回话的本机服务器，交回 https 地址。造 Go 的 TLS 服务器造不出来的几种对端。
func rawTLSPeer(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				serve(c)
			}()
		}
	}()
	return "https://" + ln.Addr().String() + "/"
}

// closedPortURL 一个没人听的本机端口的 https 地址。
func closedPortURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "https://" + addr + "/"
}

// readClientHello 读掉客户端发来的第一条 TLS 记录（ClientHello）。
func readClientHello(c net.Conn) {
	var h [5]byte
	if _, err := io.ReadFull(c, h[:]); err == nil {
		_, _ = io.CopyN(io.Discard, c, int64(h[3])<<8|int64(h[4]))
	}
}

// answerTLS12Hello 像不认 supported_versions 扩展的老式服务器那样，照 ClientHello 开头写的 1.2 直接回 TLS 1.2 的 ServerHello。
// Go 的 TLS 服务器认得这个扩展，只会回告警，造不出这一种。
func answerTLS12Hello(c net.Conn) {
	readClientHello(c)
	hello := append([]byte{3, 3}, bytes.Repeat([]byte{1}, 32)...) // 版本 1.2、随机数
	hello = append(hello, 0, 0xc0, 0x2f, 0)                       // 会话 ID 为空、ECDHE-RSA-AES128-GCM-SHA256、不压缩
	msg := append([]byte{2, 0, 0, byte(len(hello))}, hello...)    // 握手消息：ServerHello
	_, _ = c.Write(append([]byte{0x16, 3, 3, 0, byte(len(msg))}, msg...))
	_, _ = io.Copy(io.Discard, c) // 等客户端自己断开
}

// resetAfterHello 读完 ClientHello 就掐断连接（关的时候发 RST），像握手时被中间设备重置。
func resetAfterHello(c net.Conn) {
	readClientHello(c)
	_ = c.(*net.TCPConn).SetLinger(0)
}

// answerPlainHTTP 回的不是 TLS，像拦截设备直接回一张明文的拦截页。
func answerPlainHTTP(c net.Conn) {
	readClientHello(c)
	_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
}

// stayQuiet 收下连接一声不吭，握手等到超时。
func stayQuiet(c net.Conn) { _, _ = io.Copy(io.Discard, c) }
