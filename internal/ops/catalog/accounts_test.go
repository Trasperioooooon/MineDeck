package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
	"minedeck/internal/secret"
	"minedeck/internal/tunnel"
)

func TestAccountOpsHumanOnly(t *testing.T) {
	reg, _ := newCatalog(t)
	if op := reg.Op("tunnel.account.status"); op != nil {
		t.Fatal("未注入 Accounts 时不应登记账户操作")
	}

	keys := secret.NewStore(filepath.Join(t.TempDir(), "secrets.json"))
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	accounts := tunnel.NewAccounts(tunnel.NewManager(), keys)
	reg = New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Accounts: accounts, Port: 27333})
	names := []string{
		"tunnel.account.status", "tunnel.account.openfrp.login", "tunnel.account.openfrp.poll", "tunnel.account.openfrp.cancel",
		"tunnel.account.natfrp.connect", "tunnel.account.disconnect", "tunnel.account.overview",
		"tunnel.account.sign", "tunnel.account.purchase",
		"tunnel.account.create", "tunnel.account.import",
	}
	// 验证码和网页登录已经拿掉：签到只给官网入口，樱花只收访问密钥
	for _, gone := range []string{"tunnel.account.captcha", "tunnel.account.weblogin"} {
		if reg.Op(gone) != nil {
			t.Errorf("%s 不应再登记", gone)
		}
	}
	for _, name := range names {
		op := reg.Op(name)
		if op == nil {
			t.Errorf("缺少 %s", name)
			continue
		}
		if op.Level != ops.HumanOnly {
			t.Errorf("%s 应为 HumanOnly，实际 %s", name, op.Level)
		}
	}
	for _, name := range []string{
		"tunnel.account.openfrp.login", "tunnel.account.openfrp.poll", "tunnel.account.openfrp.cancel", "tunnel.account.natfrp.connect",
		"tunnel.account.disconnect", "tunnel.account.overview", "tunnel.account.create", "tunnel.account.import",
	} {
		op := reg.Op(name)
		if op == nil || !op.LocalOnly {
			t.Errorf("%s 应限制为仅本机操作", name)
			continue
		}
		if err := reg.Check(ops.Caller{Source: ops.SrcLAN}, name); err == nil {
			t.Errorf("局域网调用 %s 应被拒绝", name)
		}
		if err := reg.Check(ops.Caller{Source: ops.SrcUI, Local: true}, name); err != nil {
			t.Errorf("本机调用 %s 应被允许：%v", name, err)
		}
	}
	for _, op := range reg.Available(ops.Caller{Source: ops.SrcAssistant, Local: true}) {
		if op.Level == ops.HumanOnly && strings.HasPrefix(op.Name, "tunnel.account.") {
			t.Errorf("助手工具清单不应有 %s", op.Name)
		}
	}
	st, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcUI, Local: true}, "tunnel.account.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := st.(tunnel.AccountStatus)
	if !ok || status.OpenFrp || status.Sakura || !status.Local {
		t.Fatalf("status: %+v", st)
	}
	// 局域网会话看状态时 local=false，界面据此只读
	st, err = reg.Do(context.Background(), ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.9"}, "tunnel.account.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status := st.(tunnel.AccountStatus); status.Local {
		t.Fatalf("局域网的 status 应为 local=false: %+v", status)
	}
	sign, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcUI, Local: true}, "tunnel.account.sign", ops.ArgsOf(map[string]any{"provider": "openfrp"}))
	if err != nil {
		t.Fatal(err)
	}
	if r := sign.(tunnel.SignResult); r.OK || r.OpenURL != "https://console.openfrp.net/dashboard?action=sign" {
		t.Fatalf("sign: %+v", r)
	}
	// 不能把 MineDeck 自己的网页端口映射到公网
	_, err = reg.Do(context.Background(), ops.Caller{Source: ops.SrcUI, Local: true}, "tunnel.account.create", ops.ArgsOf(map[string]any{
		"provider": "openfrp", "name": "minedeck_ab12", "nodeId": 1, "type": "tcp", "localPort": 27333, "remotePort": 30000,
	}))
	if err == nil || !strings.Contains(err.Error(), "网页端口") {
		t.Fatalf("映射 MineDeck 自己的端口应被拒: %v", err)
	}
}
