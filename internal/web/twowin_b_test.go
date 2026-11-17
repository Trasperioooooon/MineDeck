package web

import (
	"encoding/json"
	"testing"
)

// twbTunnelRow 隧道列表里这组测试要看的几项。
type twbTunnelRow struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	ProxyID       string `json:"proxyId"`
	Bound         string `json:"boundInstance"`
	AutoStart     bool   `json:"autoStart"`
	PublicAddr    string `json:"publicAddr"`
	Purpose       string `json:"purpose"`
	HasCredential bool   `json:"hasCredential"`
}

func (e *fullEnv) twbTunnel(t *testing.T, id string) twbTunnelRow {
	t.Helper()
	var v struct {
		Data []twbTunnelRow `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", "/api/tunnels", "", 200).Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, r := range v.Data {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("列表里没有 %s", id)
	return twbTunnelRow{}
}

// 两个窗口同时开着同一条隧道（2026-11-17）：一边先存了绑定和跟随启动，另一边（还是旧页面）只发改了的名字，
// 先存的那两样还在；服务商、隧道 ID、凭据都没动。请求里带的 publicAddr 不收（那是运行中记下的）。
func TestTunnelUpdateOnlyGivenFields(t *testing.T) {
	e := newFullEnv(t)
	e.call(t, "PUT", "/api/tunnels/"+e.tunID, `{"boundInstance":"生存服","autoStart":true}`, 200)       // 窗口 A
	e.call(t, "PUT", "/api/tunnels/"+e.tunID, `{"name":"x","publicAddr":"evil.example.com:1"}`, 200) // 窗口 B
	got := e.twbTunnel(t, e.tunID)
	want := twbTunnelRow{ID: e.tunID, Provider: "natfrp", Name: "x", ProxyID: "9", Bound: "生存服", AutoStart: true, Purpose: "game", HasCredential: true}
	if got != want {
		t.Fatalf("只发名字以后：\n得到 %+v\n应为 %+v", got, want)
	}
	// 只发 clearCredential：服务商取存着的樱花，照旧不许清；什么都没改
	e.call(t, "PUT", "/api/tunnels/"+e.tunID, `{"clearCredential":true}`, 400)
	if got := e.twbTunnel(t, e.tunID); got != want {
		t.Fatalf("被拒绝的清凭据改了东西：%+v", got)
	}
}
