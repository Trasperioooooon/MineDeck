package catalog

import (
	"context"
	"strings"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// 隧道守住的是这次实际监听的网页端口（Env.Port），不是写死的默认 27333：默认端口被占用时程序会换一个随机端口。
func TestTunnelGuardFollowsActualPort(t *testing.T) {
	newCatalog(t) // 只用它准备好临时程序目录
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	tun := tunnel.NewManager()
	reg := New(Env{Mgr: mgr, Tun: tun, Port: 41234})
	local := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	add := func(port string) error {
		_, err := reg.Do(context.Background(), local, "tunnel.add", ops.Args(
			`{"provider":"custom","name":"自建","serverAddr":"frp.example.com","serverPort":7000,"remotePort":30001,"localPort":`+port+`}`))
		return err
	}
	if err := add("41234"); err == nil || !strings.Contains(err.Error(), "本地端口 41234 是 MineDeck 自己的网页端口") {
		t.Errorf("指向这次的网页端口 41234 应被拒绝：%v", err)
	}
	if err := add("27333"); err != nil {
		t.Errorf("这次网页端口不是 27333，指向它的隧道不该被拦：%v", err)
	}
}
