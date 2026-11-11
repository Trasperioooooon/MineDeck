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
		"tunnel.account.status", "tunnel.account.openfrp.login", "tunnel.account.openfrp.poll",
		"tunnel.account.natfrp.connect", "tunnel.account.weblogin", "tunnel.account.disconnect", "tunnel.account.overview",
		"tunnel.account.captcha", "tunnel.account.sign", "tunnel.account.purchase",
		"tunnel.account.create", "tunnel.account.import",
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
		"tunnel.account.openfrp.login", "tunnel.account.openfrp.poll", "tunnel.account.natfrp.connect",
		"tunnel.account.disconnect", "tunnel.account.sign", "tunnel.account.create", "tunnel.account.import",
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
	if !ok || status.OpenFrp || status.Sakura {
		t.Fatalf("status: %+v", st)
	}
	cap, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcUI, Local: true}, "tunnel.account.captcha", ops.ArgsOf(map[string]any{"provider": "openfrp"}))
	if err != nil {
		t.Fatal(err)
	}
	info := cap.(tunnel.CaptchaInfo)
	if info.Mode != "token" || info.AppID == "" || info.Vendor != "naids" || info.OpenURL == "" {
		t.Fatalf("captcha: %+v", info)
	}
}
