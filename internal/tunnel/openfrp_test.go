package tunnel

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
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

func TestOpenfrpFrpcURLsFromAPI(t *testing.T) {
	const live = `{
		"flag": true,
		"msg": "OK",
		"data": {
			"latest": "/OF_0.68.0_37f78258_260326/",
			"latest_full": "OF_0.68.0_37f78258_260326",
			"latest_ver": "0.68.0",
			"source": [
				{"label": "CDN/Natayark 静态资源服务", "value": "https://staticassets.naids.com/client"},
				{"label": "CLOUD/OneDrive 镜像源", "value": "https://1dr.zyghit.cn/client/"},
				{"label": "CDN/ZGIT 高速下载源", "value": "https://r.zyghit.cn/download/client"}
			]
		}
	}`
	tests := []struct {
		name    string
		body    string
		want    []string
		wantErr bool
	}{
		{
			name: "接口原文",
			body: live,
			want: []string{
				"https://staticassets.naids.com/client/OF_0.68.0_37f78258_260326/frpc_windows_amd64.zip",
				"https://1dr.zyghit.cn/client/OF_0.68.0_37f78258_260326/frpc_windows_amd64.zip",
				"https://r.zyghit.cn/download/client/OF_0.68.0_37f78258_260326/frpc_windows_amd64.zip",
			},
		},
		{
			name: "latest_full 和下载源两端有斜杠",
			body: `{
				"data": {
					"latest": "/OF_0.68.0_37f78258_260326/",
					"latest_full": " /OF_0.68.0_37f78258_260326/ ",
					"latest_ver": "0.68.0",
					"source": [{"label": "a", "value": " https://staticassets.naids.com/client/ "}]
				}
			}`,
			want: []string{
				"https://staticassets.naids.com/client/OF_0.68.0_37f78258_260326/frpc_windows_amd64.zip",
			},
		},
		{
			name: "latest 和 latest_ver 都不拿来拼路径",
			body: `{
				"data": {
					"latest": "/WRONG/",
					"latest_full": "OF_0.68.0_37f78258_260326",
					"latest_ver": "0.68.0",
					"source": [
						{"value": "https://staticassets.naids.com/client"},
						{"value": "  "}
					]
				}
			}`,
			want: []string{
				"https://staticassets.naids.com/client/OF_0.68.0_37f78258_260326/frpc_windows_amd64.zip",
			},
		},
		{
			name:    "只有 latest_ver",
			body:    `{"data":{"latest":"/OF_0.68.0_37f78258_260326/","latest_ver":"0.68.0","source":[{"value":"https://staticassets.naids.com/client"}]}}`,
			wantErr: true,
		},
		{
			name:    "没有下载源",
			body:    `{"data":{"latest_full":"OF_0.68.0_37f78258_260326","latest_ver":"0.68.0","source":[]}}`,
			wantErr: true,
		},
		{
			name:    "下载源是空的",
			body:    `{"data":{"latest_full":"OF_0.68.0_37f78258_260326","source":[{"value":""},{"value":"/"}]}}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var meta openfrpSoftwareMeta
			if err := json.Unmarshal([]byte(tt.body), &meta); err != nil {
				t.Fatal(err)
			}
			if tt.name == "接口原文" && (meta.Data.Latest != "/OF_0.68.0_37f78258_260326/" || meta.Data.LatestVer != "0.68.0") {
				t.Fatalf("版本字段没有解析出来：latest=%q latest_ver=%q", meta.Data.Latest, meta.Data.LatestVer)
			}
			got, err := openfrpFrpcURLs(meta)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("应失败，得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Fatalf("地址：\n%s\n想要：\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			for _, u := range got {
				if strings.Contains(u, "/0.68.0/") || strings.Contains(u, "/WRONG/") || strings.HasSuffix(u, "/client/frpc_windows_amd64.zip") {
					t.Fatalf("拼进了不该用的路径：%s", u)
				}
			}
		})
	}
}

type openfrpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f openfrpRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestOpenfrpEnsureFrpcDownloadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/commonQuery/get":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"latest_full":"OF_test","source":[{"value":"https://frpc.test/client"}]}}`))
		case "/client/OF_test/frpc_windows_amd64.zip":
			w.WriteHeader(567)
		default:
			t.Errorf("意外请求：%s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// 两个客户端分开，各记下自己经手的路径：frpc 的 zip 必须走 openfrpHTTPClient（只报 TLS 1.3 的那个），
	// 走成默认的 dl.Client 就又会被下载源当成机器人拦掉。
	var viaDefault, viaOpenfrp []string
	redirectTo := func(seen *[]string) *http.Client {
		return &http.Client{Transport: openfrpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			*seen = append(*seen, r.URL.Path)
			clone := r.Clone(r.Context())
			clone.URL.Scheme = target.Scheme
			clone.URL.Host = target.Host
			clone.Host = ""
			return http.DefaultTransport.RoundTrip(clone)
		})}
	}
	oldDLClient, oldOpenFrpClient := dl.Client, openfrpHTTPClient
	dl.Client, openfrpHTTPClient = redirectTo(&viaDefault), redirectTo(&viaOpenfrp)
	t.Cleanup(func() { dl.Client, openfrpHTTPClient = oldDLClient, oldOpenFrpClient })

	oldRuntimes, oldCache := app.RuntimesDir, app.CacheDir
	base := t.TempDir()
	app.RuntimesDir = filepath.Join(base, "runtimes")
	app.CacheDir = filepath.Join(base, "cache")
	t.Cleanup(func() { app.RuntimesDir, app.CacheDir = oldRuntimes, oldCache })

	_, err = (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if err == nil {
		t.Fatal("应该下载失败")
	}
	msg := err.Error()
	for _, want := range []string{
		frpcDir("openfrp"),
		"frpc_windows_amd64.zip",
		"frpc_windows_amd64.exe",
		"HTTP 567（被源站 CDN 的机器人防护拦截）",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误文案缺少 %q：%s", want, msg)
		}
	}
	cause := errors.Unwrap(err)
	if cause == nil || !strings.Contains(cause.Error(), "多次尝试后仍下载失败") {
		t.Fatalf("第一层包装应保留下载汇总错误：%v", err)
	}
	for errors.Unwrap(cause) != nil {
		cause = errors.Unwrap(cause)
	}
	if cause.Error() != "HTTP 567" {
		t.Fatalf("错误链末端应是原始状态码错误：%v", cause)
	}
	if len(viaOpenfrp) == 0 {
		t.Fatal("frpc 的 zip 没有经 openfrpHTTPClient 下载")
	}
	for _, p := range viaOpenfrp {
		if !strings.HasSuffix(p, "/"+openfrpFrpcZip) {
			t.Errorf("openfrpHTTPClient 只该用来下 frpc 的 zip，却请求了 %s", p)
		}
	}
	for _, p := range viaDefault {
		if strings.HasSuffix(p, "/"+openfrpFrpcZip) {
			t.Errorf("frpc 的 zip 走了默认客户端：%s", p)
		}
	}
	// 提示里叫人把 exe 放进这个文件夹，它得已经在那儿
	if fi, err := os.Stat(frpcDir("openfrp")); err != nil || !fi.IsDir() {
		t.Errorf("下载失败后应已建好放 frpc 的文件夹：%v", err)
	}
}

// openfrpHTTPClient 的握手只报 TLS 1.3：这是它过得了下载源机器人防护的全部原因（见 providers.go 里的说明）。
// 起一个本机的 TLS 服务，记下客户端在 ClientHello 里报的版本。证书是测试自签的，握手后面会失败，不影响看这一项。
// 默认的 dl.Client 作对照：它会连 TLS 1.2 一起报。
func TestOpenfrpHTTPClientOffersOnlyTLS13(t *testing.T) {
	var mu sync.Mutex
	var offered [][]uint16
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		offered = append(offered, append([]uint16(nil), h.SupportedVersions...))
		mu.Unlock()
		return nil, nil
	}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // 握手失败是预料之中的，不往测试输出里写
	srv.StartTLS()
	defer srv.Close()

	hello := func(c *http.Client) []uint16 {
		t.Helper()
		mu.Lock()
		offered = nil
		mu.Unlock()
		if resp, err := c.Get(srv.URL); err == nil {
			resp.Body.Close()
			t.Fatal("自签证书应该通不过校验")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(offered) == 0 {
			t.Fatal("服务端没有收到 ClientHello")
		}
		return offered[0]
	}
	if got := hello(openfrpHTTPClient); len(got) != 1 || got[0] != tls.VersionTLS13 {
		t.Errorf("openfrpHTTPClient 应只报 TLS 1.3，实际报了 %#x", got)
	}
	if got := hello(dl.Client); !slices.Contains(got, uint16(tls.VersionTLS12)) {
		t.Errorf("对照用的 dl.Client 应该也报 TLS 1.2（不报的话这条测试就分不出两者了），实际 %#x", got)
	}
}
