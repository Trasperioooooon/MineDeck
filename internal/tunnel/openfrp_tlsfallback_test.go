package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/procutil"
)

// 这一组测 OpenFrp 的 frpc 下载在只报 TLS 1.3 握不上手时的退路（A55）：下载源只会 TLS 1.2 时退回兼容的连接方式能下成；
// 退回的那次跳转照样只跟 https；不是握手的毛病（404）、下载源会 TLS 1.3 的、被取消的、写盘时被杀毒软件拦下的，不退回。
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
func installOpenfrpFrom(t *testing.T, ctx context.Context, zipURLs ...string) (dir, exe, lines string, err error) {
	t.Helper()
	con := procutil.NewConsole()
	base := t.TempDir()
	dir = filepath.Join(base, "runtimes", "frpc", "openfrp")
	exe, err = openfrpInstall(ctx, con, zipURLs, filepath.Join(base, "cache", "frpc", "openfrp-frpc-OF_t1.zip"), dir, nil)
	return dir, exe, strings.Join(con.Lines(), "\n"), err
}

// TestOpenfrpZipTLSFallback 退路的几条下载用例。大多要等 dl.Fetch 整次下载失败一到两次，每次固定等两段轮间间隔（2 秒、4 秒，
// dl 的退避，包外调不了），所以并行跑，一遍的时间是最慢那几条（两次都失败的，12 秒左右）。
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
		{"FallsBackOnceWhenBothHandshakesFail", testZipFallsBackOnceWhenBothHandshakesFail},
		{"NoFallbackWhenTLS13Works", testZipNoFallbackWhenTLS13Works},
		{"NoFallbackWhenCanceled", testZipNoFallbackWhenCanceled},
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

	dir, got, lines, err := installOpenfrpFrom(t, t.Context(), src.zipURL())
	if err != nil || got != filepath.Join(dir, "frpc_windows_amd64.exe") || readFile(t, got) != exe {
		t.Fatalf("退回以后应下成、能用：%q %v\n%s", got, err, lines)
	}
	if n := strings.Count(lines, tlsFallbackNote); n != 1 {
		t.Errorf("控制台要说一次换了连接方式，实际 %d 次：%s", n, lines)
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

	_, got, lines, err := installOpenfrpFrom(t, t.Context(), only12.zipURL(), modern.zipURL())
	if err != nil || readFile(t, got) != exe {
		t.Fatalf("退回以后应从第一个源下成：%q %v\n%s", got, err, lines)
	}
	if n := strings.Count(lines, tlsFallbackNote); n != 1 {
		t.Errorf("控制台要说一次换了连接方式，实际 %d 次：%s", n, lines)
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

	dir, got, lines, err := installOpenfrpFrom(t, t.Context(), src.zipURL())
	if err == nil || !strings.Contains(err.Error(), "重定向到不允许的地址") {
		t.Fatalf("退回以后跳到 http 也应中断：%q %v", got, err)
	}
	if strings.Count(lines, tlsFallbackNote) != 1 || src.hits.Load() == 0 {
		t.Fatalf("应是退回一次以后才被拒的，不然测不到退回的那个客户端：进来 %d 次\n%s", src.hits.Load(), lines)
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

	dir, _, lines, err := installOpenfrpFrom(t, t.Context(), src.zipURL())
	if err == nil {
		t.Fatal("应该下载失败")
	}
	if n := strings.Count(lines, tlsFallbackNote); n != 1 {
		t.Errorf("只退一次，控制台那句只说一次，实际 %d 次：%s", n, lines)
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

// 退回以后握手还是没成（对端读完 ClientHello 就掐断连接，两种方式都一样）：只退一次，控制台那句只说一次，
// 报错说两种方式都试过了；两次下载各试三轮，一共六个连接。
func testZipFallsBackOnceWhenBothHandshakesFail(t *testing.T) {
	var conns atomic.Int32
	peer := rawTLSPeer(t, func(c net.Conn) {
		conns.Add(1)
		resetAfterHello(c)
	})

	_, _, lines, err := installOpenfrpFrom(t, t.Context(), peer+"client/OF_t1/"+openfrpFrpcZip)
	if err == nil || strings.Count(err.Error(), "两种连接方式都试过了") != 1 {
		t.Fatalf("应报两种方式都试过了：%v", err)
	}
	if n := strings.Count(lines, tlsFallbackNote); n != 1 {
		t.Errorf("只退一次，控制台那句只说一次，实际 %d 次：%s", n, lines)
	}
	if n := conns.Load(); n != 6 {
		t.Errorf("只报 TLS 1.3 的三轮加退回的三轮，应是 6 个连接，实际 %d 个", n)
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

	_, _, lines, err := installOpenfrpFrom(t, t.Context(), src.zipURL())
	if err == nil || !strings.Contains(err.Error(), "HTTP 404（该源没有这个文件）") || strings.Contains(err.Error(), "两种连接方式") {
		t.Fatalf("404 应照原样报错、不退回：%v", err)
	}
	if strings.Contains(lines, tlsFallbackNote) || src.hits.Load() != 3 {
		t.Fatalf("404 不该退回：进来 %d 次\n%s", src.hits.Load(), lines)
	}

	serve.Store(true)
	_, got, lines, err := installOpenfrpFrom(t, t.Context(), src.zipURL())
	if err != nil || got == "" || strings.Contains(lines, tlsFallbackNote) || src.hits.Load() != 4 {
		t.Fatalf("下成了就没有退回的事：%q %v，进来 %d 次\n%s", got, err, src.hits.Load(), lines)
	}
}

// 下载途中被取消（总时限到了也一样）的不退回：第一个源只会 TLS 1.2（只报 1.3 的那次握不上手，记下了握手失败），
// 第二个源收到请求时把整次下载取消。交回取消的错误，不等 dl 的轮间间隔；控制台不说换连接方式，第一个源一次也没进来。
func testZipNoFallbackWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	only12 := newTLSSource(t, tls.VersionTLS12, http.NotFound)
	stopper := newTLSSource(t, 0, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		select { // 等客户端因为取消断开；不让服务端关不掉
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	start := time.Now()
	_, _, lines, err := installOpenfrpFrom(t, ctx, only12.zipURL(), stopper.zipURL())
	took := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应交回取消的错误：%v", err)
	}
	if strings.Contains(lines, tlsFallbackNote) || only12.hits.Load() != 0 {
		t.Fatalf("取消了就不该退回：第一个源进来 %d 次\n%s", only12.hits.Load(), lines)
	}
	if took > 3*time.Second {
		t.Errorf("取消以后应立刻返回，不该等 dl 的轮间间隔：用了 %v", took)
	}
	t.Logf("取消以后 %v 返回", took)
}

// 第一次下载没成以后退不退（tlsFallbackWorth）。写盘时被杀毒软件拦下的，真下载时造不出 Windows 的那两个错误码，
// 在这里拿包着 errVirusInfected、errVirusDeleted 的错误直接测（dl.AnyFailure 先看错误本身的包装链）。
func TestTLSFallbackWorth(t *testing.T) {
	hsErr := errors.New("remote error: tls: protocol version not supported")
	notFound := errors.New("多次尝试后仍下载失败（frpc.test: HTTP 404（该源没有这个文件））")
	blocked := fmt.Errorf("多次尝试后仍下载失败：%w", &os.PathError{Op: "write", Path: ".minedeck-download-1.part", Err: errVirusInfected})
	deleted := fmt.Errorf("多次尝试后仍下载失败：%w", &os.LinkError{Op: "rename", Old: ".minedeck-download-1.part", New: "openfrp-frpc.zip", Err: errVirusDeleted})
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, c := range []struct {
		name       string
		ctx        context.Context
		err, hsErr error
		want       bool
	}{
		{"途中有握手失败、没下成", t.Context(), notFound, hsErr, true},
		{"下成了", t.Context(), nil, hsErr, false},
		{"途中没有握手失败", t.Context(), notFound, nil, false},
		{"被取消或总时限到了", canceled, notFound, hsErr, false},
		{"写盘时被杀毒软件拦下", t.Context(), blocked, hsErr, false},
		{"被杀毒软件拦下并删掉", t.Context(), deleted, hsErr, false},
	} {
		if got := tlsFallbackWorth(c.ctx, c.err, c.hsErr); got != c.want {
			t.Errorf("%s：退不退 %v，应为 %v", c.name, got, c.want)
		}
	}
}

// 哪些握手失败算「换个 TLS 版本可能就好了」：对方只会 TLS 1.2（回告警；或者像不认新扩展的老式服务器、中间设备那样，
// 直接回 TLS 1.2 的 ServerHello）、读完 ClientHello 就掐断连接、回的不是 TLS，都算；握手超时、证书对不上不算；
// 连不上（没走到握手）和握手成功的，什么都不记。
func TestOpenfrpHandshakeFailuresWorthFallback(t *testing.T) {
	ok := newTLSSource(t, 0, http.NotFound)
	only12 := newTLSSource(t, tls.VersionTLS12, http.NotFound)
	at := func(u string) func(*testing.T) string { return func(*testing.T) string { return u } }
	cases := []struct {
		name      string
		url       func(t *testing.T) string // 发请求前才取：连不上那行的端口放掉以后要马上拨
		trust     []*x509.Certificate
		hsTimeout time.Duration
		handshake bool // 有没有走到握手
		want      bool // 该不该记下
	}{
		{"只会 TLS 1.2，回告警", at(only12.srv.URL), []*x509.Certificate{only12.srv.Certificate()}, 0, true, true},
		{"老式服务器直接回 TLS 1.2 的 ServerHello", at(rawTLSPeer(t, answerTLS12Hello)), nil, 0, true, true},
		{"读完 ClientHello 就掐断连接", at(rawTLSPeer(t, resetAfterHello)), nil, 0, true, true},
		{"回的不是 TLS", at(rawTLSPeer(t, answerPlainHTTP)), nil, 0, true, true},
		{"握手超时", at(rawTLSPeer(t, stayQuiet)), nil, 200 * time.Millisecond, true, false},
		{"证书对不上", at(ok.srv.URL), nil, 0, true, false},
		{"连不上", closedPortURL, nil, 0, false, false},
		{"握手成功", at(ok.srv.URL), []*x509.Certificate{ok.srv.Certificate()}, 0, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := trustingCopy(openfrpHTTPClient, c.trust...)
			if c.hsTimeout > 0 {
				cl.Transport.(*http.Transport).TLSHandshakeTimeout = c.hsTimeout
			}
			o := watchOneRequest(t, cl, t.Context(), c.url(t), nil)
			if o.handshook != c.handshake {
				t.Fatalf("握手走到没有：%v，应为 %v（请求的结果 %v）", o.handshook, c.handshake, o.doErr)
			}
			if (o.recorded != nil) != c.want {
				t.Fatalf("记下的握手错误：%v，该不该记：%v（请求的结果 %v）", o.recorded, c.want, o.doErr)
			}
		})
	}
}

// 请求在握手途中被打断（对端读完 ClientHello 就不吭声）都不记。按 Go 实际交给 TLSHandshakeDone 的错误写：
// net/http 拨号不跟着请求取消（连接留给以后的请求用），请求被取消或到期时立刻带着 context 的错误返回，
// 握手却在后台接着等到传输层自己的握手超时，TLSHandshakeDone 拿到的是那个超时（tlsVersionMayHelp 的 Timeout 那一支）；
// 只有拨号本身也被取消（请求取消以后 CloseIdleConnections）时，拿到的才是 context.Canceled（取消那一支）。
// 下载里被取消的不退回，真正挡着的是 openfrpDownloadZip 里看 ctx 的那一条（见 testZipNoFallbackWhenCanceled）。
func TestOpenfrpHandshakeFailuresWorthFallbackInterrupted(t *testing.T) {
	isTimeout := func(err error) bool {
		var ne net.Error
		return errors.As(err, &ne) && ne.Timeout() && !errors.Is(err, context.DeadlineExceeded)
	}
	for _, c := range []struct {
		name      string
		deadline  time.Duration // 请求多久到期；0 是对端读完 ClientHello 时 cancel()
		closeIdle bool          // 请求被取消以后，拨号也取消
		doErr     error         // 请求交回的
		hookErr   func(error) bool
	}{
		{"请求在握手途中被取消", 0, false, context.Canceled, isTimeout},
		{"请求在握手途中到期", 50 * time.Millisecond, false, context.DeadlineExceeded, isTimeout},
		{"请求和拨号都在握手途中被取消", 0, true, context.Canceled, func(err error) bool { return errors.Is(err, context.Canceled) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			hello := make(chan struct{}, 1)
			peer := rawTLSPeer(t, func(conn net.Conn) {
				readClientHello(conn)
				select {
				case hello <- struct{}{}:
				default:
				}
				stayQuiet(conn)
			})
			cl := trustingCopy(openfrpHTTPClient)
			cl.Transport.(*http.Transport).TLSHandshakeTimeout = 200 * time.Millisecond // 后台的握手不用等 15 秒
			var ctx context.Context
			var cancel context.CancelFunc
			if c.deadline > 0 {
				ctx, cancel = context.WithTimeout(t.Context(), c.deadline)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
				go func() {
					select {
					case <-hello:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			defer cancel()
			afterDo := func() {
				if c.closeIdle {
					cl.CloseIdleConnections()
				}
			}
			o := watchOneRequest(t, cl, ctx, peer, afterDo)
			if !errors.Is(o.doErr, c.doErr) {
				t.Fatalf("请求应带着 %v 返回，实际 %v", c.doErr, o.doErr)
			}
			if !o.handshook || !c.hookErr(o.hookErr) {
				t.Fatalf("TLSHandshakeDone 拿到的不是预想的那种错误：走到握手 %v，错误 %v", o.handshook, o.hookErr)
			}
			if o.recorded != nil {
				t.Fatalf("不该记下：%v", o.recorded)
			}
		})
	}
}

// handshakeObservation 一次请求里握手的情况。
type handshakeObservation struct {
	handshook bool  // 走到了握手
	hookErr   error // TLSHandshakeDone 拿到的错误
	recorded  error // watchTLSHandshake 记下的
	doErr     error // 请求交回的
}

// watchOneRequest 挂上 watchTLSHandshake 发一次请求。另挂一个 trace 看握手走没走到、TLSHandshakeDone 拿到了什么，
// 免得「不该记」的几条白白通过。请求被打断时握手会比请求晚结束（拨号在后台接着握手），走到了握手的等它结束再看；
// afterDo 在请求返回以后、等握手结束之前调。
func watchOneRequest(t *testing.T, cl *http.Client, ctx context.Context, url string, afterDo func()) handshakeObservation {
	t.Helper()
	var started atomic.Bool
	done := make(chan error, 1)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		TLSHandshakeStart: func() { started.Store(true) },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { done <- err },
	})
	ctx, handshakeErr := watchTLSHandshake(ctx) // 后挂的先调：test 这边收到时，它已经记过了
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	var o handshakeObservation
	resp, err := cl.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	o.doErr = err
	if afterDo != nil {
		afterDo()
	}
	if started.Load() {
		select {
		case o.hookErr = <-done:
			o.handshook = true
		case <-time.After(5 * time.Second):
			t.Fatalf("握手开始了，5 秒内没结束（请求的结果 %v）", err)
		}
	}
	o.recorded = handshakeErr()
	return o
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
