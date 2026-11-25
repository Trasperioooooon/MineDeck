package catalog

// beta.3 第二层，服务器详情页（docs/测试与评审/可用性走查-2026-11-14.md 第八节 R10、R11、R15、R21）在操作层上的样子。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// R10：删除插件或模组只许人在界面上点（AI 不能调用，也不能提出提案）；删的是 plugins 文件夹里的那个 jar，移进 .minedeck/已删除/。
func TestModsDeleteHumanOnly(t *testing.T) {
	reg, _ := newCatalog(t)
	if op := reg.Op("instance.mods.delete"); op == nil || op.Level != ops.HumanOnly {
		t.Fatal("instance.mods.delete 应是仅人工")
	}
	dir := filepath.Join(app.ServersDir, "s1", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Chunky.jar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := do(t, reg, bot, "instance.mods.delete", `{"inst":"生存服","file":"Chunky.jar"}`, nil); err == nil || ops.CodeOf(err) != 403 {
		t.Fatalf("AI 调用应被拒：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Chunky.jar")); err != nil {
		t.Fatal("AI 被拒时文件应还在")
	}
	var out struct{ File, MovedTo, Dir string }
	if err := do(t, reg, human, "instance.mods.delete", `{"inst":"生存服","file":"Chunky.jar"}`, &out); err != nil {
		t.Fatal(err)
	}
	if out.MovedTo != filepath.Join(".minedeck", "已删除", "plugins", "Chunky.jar") || out.Dir != filepath.Join(".minedeck", "已删除", "plugins") {
		t.Fatalf("结果：%+v", out)
	}
	if _, err := os.Stat(filepath.Join(app.ServersDir, "s1", out.MovedTo)); err != nil {
		t.Fatal("移走的文件应在 .minedeck/已删除/plugins 里")
	}
}

// R03 留给第二层的：「让 AI 分析」打包的排错资料（经操作层的 instance.diag 取结论）带上「上次没能启动」的原因，排在最前面；
// 界面上调 instance.diag 不带（详情页头的提示区已经常驻写着）。
// R21：服务器列表带上 javaMissing；R15：带上这台服务器的备份文件夹。
func TestDiagStartFailedForAIAndListFields(t *testing.T) {
	reg, _ := newCatalog(t)
	// 测试里的服务器没配 Java：点启动就是「找不到 Java」，记成上次没能启动
	if err := do(t, reg, human, "instance.start", `{"inst":"生存服"}`, nil); err == nil {
		t.Fatal("没有 Java 应启动失败")
	}
	type rep struct {
		Findings []struct{ Rule, Title, Summary string }
	}
	var ai, ui rep
	if err := do(t, reg, bot, "instance.diag", `{"inst":"生存服"}`, &ai); err != nil {
		t.Fatal(err)
	}
	if len(ai.Findings) == 0 || ai.Findings[0].Rule != "start-failed" || !strings.Contains(ai.Findings[0].Summary, "找不到这台服务器用的 Java") ||
		!strings.Contains(ai.Findings[0].Summary, "「设置 → 启动与内存」") {
		t.Fatalf("给 AI 的结论：%+v", ai.Findings)
	}
	if err := do(t, reg, human, "instance.diag", `{"inst":"生存服"}`, &ui); err != nil {
		t.Fatal(err)
	}
	for _, f := range ui.Findings {
		if f.Rule == "start-failed" {
			t.Fatalf("界面上的结论不该再带这一条：%+v", ui.Findings)
		}
	}
	var list []struct {
		Name        string
		JavaMissing bool   `json:"javaMissing"`
		BackupDir   string `json:"backupDir"`
	}
	if err := do(t, reg, human, "instance.list", `{}`, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].JavaMissing || list[0].BackupDir != filepath.Join(app.BackupsRoot(), "生存服") {
		t.Fatalf("列表：%+v", list)
	}
}

// 第一层留下的：游戏内 AI 读白名单开关按服务端的读法，同一项写了几行认最后一行（原来 Props.Get 读的是第一行）；没写这一项按关。
func TestGameChatWhitelistOnLastLineWins(t *testing.T) {
	for props, want := range map[string]bool{
		"white-list=false\nwhite-list=true\n": true,
		"white-list=true\nwhite-list=false\n": false,
		"white-list=TRUE\n":                   true,
		"motd=x\n":                            false,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := whitelistOn(&inst.Instance{Dir: dir}); got != want {
			t.Errorf("%q：读成 %v，应为 %v", props, got, want)
		}
	}
}
