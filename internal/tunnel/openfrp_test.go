package tunnel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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
	client := &http.Client{Transport: openfrpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme = target.Scheme
		clone.URL.Host = target.Host
		clone.Host = ""
		return http.DefaultTransport.RoundTrip(clone)
	})}
	oldDLClient, oldOpenFrpClient := dl.Client, openfrpHTTPClient
	dl.Client, openfrpHTTPClient = client, client
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
}
