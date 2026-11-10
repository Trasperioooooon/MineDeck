package tunnel

import (
	"encoding/json"
	"strings"
	"testing"
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
