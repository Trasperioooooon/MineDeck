package catalog

// beta.3 第一层：可用性走查整改 P0（R01、R02、R03、U30）在服务器列表和新建参数上的样子。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
)

// 服务器列表：代理端的连接端口读它自己的配置（port 仍是旧的 25565）；接管来的服务器带上移出后会留下的文件；没失败过时没有 lastStartError。
func TestSummaryConnectPortAndLeftovers(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"velocity.toml": "bind = \"0.0.0.0:25590\"\n", "run.bat": "x", "run.before-minedeck.bat": "old"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	px := &inst.Instance{Settings: inst.Settings{Name: "代理", Core: mcsrc.CoreVelocity, Adopted: true, LaunchTarget: "jar:server.jar"}, Dir: dir}
	s := summarize(px)
	if s.ConnectPort != 25590 || s.Port != 25565 {
		t.Fatalf("代理端：connectPort=%d port=%d", s.ConnectPort, s.Port)
	}
	if strings.Join(s.MineDeckFiles, ",") != "run.bat,run.before-minedeck.bat" {
		t.Fatalf("接管来的服务器留下的文件：%q", s.MineDeckFiles)
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"connectPort":25590`) || !strings.Contains(string(b), `"mdFiles":["run.bat","run.before-minedeck.bat"]`) || strings.Contains(string(b), "lastStartError") {
		t.Fatalf("JSON：%s", b)
	}
	// 游戏服务器：connectPort 就是 server-port；不是接管来的没有 mdFiles
	gdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gdir, "server.properties"), []byte("server-port=25571\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := summarize(&inst.Instance{Settings: inst.Settings{Name: "游戏服", Core: mcsrc.CorePaper}, Dir: gdir})
	if g.ConnectPort != 25571 || g.Port != 25571 || g.MineDeckFiles != nil {
		t.Fatalf("游戏服务器：%+v", g)
	}
}

// 新建、导入整合包的参数里都有白名单开关（导入整合包的参数表是从新建那份里挑的，漏挑了就没有说明）。
func TestCreateAndImportParamsHaveWhitelist(t *testing.T) {
	if createParams["whitelist"] == nil {
		t.Fatal("新建的参数里应有 whitelist")
	}
	reg, _ := newCatalog(t)
	for _, op := range []string{"instance.create", "instance.import"} {
		if o := reg.Op(op); o == nil || o.Params == nil || o.Params.Properties["whitelist"] == nil {
			t.Fatalf("%s 的参数里应有 whitelist", op)
		}
	}
}
