package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/ops"
)

// 2026-11-14 Codex 第二轮复审（对修复的复审）查出来的几处，在操作这一层的回归测试。
// 报告原文和逐条核实在 docs/评审-合并后复审-2026-11-14.md 第七、八节。

// 阻挡项 1：改配置文件的操作，从读原文件到写回，整个过程都登记着，换地图插不进来。
// 上一版是读完、算完才登记：读和写之间换地图可以整个做完，按旧地图的配置算出来的内容就写进了新地图。
func TestConfigWriteHoldsFromReadToWrite(t *testing.T) {
	reg, mgr, dir := newModCatalog(t)
	path := filepath.Join(dir, "world", "serverconfig", "create-server.toml")
	args := ops.Args(`{"inst":"整合包","path":"world/serverconfig/create-server.toml","edits":[{"find":"maxRotationSpeed = 256","replace":"maxRotationSpeed = 512"}]}`)

	// 读完原文件、算好新内容、还没写回的那一刻：换地图必须占不上位
	asked := false
	var holdErr error
	cfgPlannedHook = func() {
		asked = true
		release, err := mgr.BeginWorldChange("整合包", "换地图")
		holdErr = err
		if err == nil {
			// 没拦住：就在这里把世界换掉，看旧配置会不会被写进新地图
			_ = os.WriteFile(path, []byte("[kinetics]\n\tmaxRotationSpeed = 1\n\tnewWorldOnly = true\n"), 0o644)
			release()
		}
	}
	t.Cleanup(func() { cfgPlannedHook = nil })
	if _, err := reg.Do(context.Background(), human, "instance.config.write", args); err != nil {
		t.Fatalf("改配置文件：%v", err)
	}
	cfgPlannedHook = nil
	if !asked {
		t.Fatal("测试自己没摆对：没走到读完还没写的那一步")
	}
	if holdErr == nil || !strings.Contains(holdErr.Error(), "修改配置文件") {
		b, _ := os.ReadFile(path)
		t.Errorf("读完还没写的时候换地图不该占得上位：%v。最后文件里是：%q", holdErr, b)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "maxRotationSpeed = 512") || strings.Contains(string(b), "newWorldOnly") {
		t.Errorf("改完以后的内容：%q", b)
	}
	if who := mgr.WorldUsers("整合包"); who != "" {
		t.Errorf("改完以后登记应当撤掉：%q", who)
	}

	// 反过来：换地图占着位的时候，这个操作在读文件之前就被拒绝（连路径不对这类错都轮不到报），文件一个字不动
	before, _ := os.ReadFile(path)
	release, err := mgr.BeginWorldChange("整合包", "换地图")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{string(args), `{"inst":"整合包","path":"world/serverconfig/没有这个文件.toml","edits":[{"find":"a","replace":"b"}]}`} {
		if _, err := reg.Do(context.Background(), human, "instance.config.write", ops.Args(a)); err == nil || !strings.Contains(err.Error(), "换地图") {
			t.Errorf("换地图的时候改配置文件应当被拒绝，原因要说是换地图：%v", err)
		}
	}
	release()
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("被拒绝的修改不该动文件：%q", after)
	}
}

// 开白名单的操作要改 server.properties：一开头就登记，正在换地图时什么都不做。
// 不能是「人已经加进白名单了，才发现登记不上」，那样会留下加了人、白名单却没开的半截结果。
func TestWhitelistDoesNothingWhileWorldIsChanging(t *testing.T) {
	e := newGameEnv(t)
	props := filepath.Join(e.dir, "server.properties")
	wl := filepath.Join(e.dir, "whitelist.json")
	before, _ := os.ReadFile(props)
	release, err := e.mgr.BeginWorldChange("生存服", "换地图")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.do(t, localUI, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Steve", "Alex"}})
	release()
	if err == nil || !strings.Contains(err.Error(), "换地图") {
		t.Fatalf("换地图的时候开白名单应当被拒绝：%v", err)
	}
	if after, _ := os.ReadFile(props); string(after) != string(before) {
		t.Errorf("被拒绝以后 server.properties 不该变：%q", after)
	}
	if b, err := os.ReadFile(wl); err == nil && (strings.Contains(string(b), "Steve") || strings.Contains(string(b), "Alex")) {
		t.Errorf("被拒绝以后白名单里不该多出人：%q", b)
	}
	// 放开以后照常
	if _, err := e.do(t, localUI, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Steve"}}); err != nil {
		t.Fatalf("换完地图以后开白名单：%v", err)
	}
	if after, _ := os.ReadFile(props); !strings.Contains(string(after), "white-list=true") {
		t.Errorf("开了白名单以后 server.properties：%q", after)
	}
	if b, _ := os.ReadFile(wl); !strings.Contains(string(b), "Steve") {
		t.Errorf("白名单里应当有 Steve：%q", b)
	}
	if who := e.mgr.WorldUsers("生存服"); who != "" {
		t.Errorf("做完以后登记应当撤掉：%q", who)
	}
}
