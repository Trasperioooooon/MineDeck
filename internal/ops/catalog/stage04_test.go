package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"minedeck/internal/chatai"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

func TestGameAIWhitelistDefaultByMCVersion(t *testing.T) {
	e := newGameEnv(t)

	// newGameEnv 默认 MC 是 1.21.1、server.properties 没有 white-list 行：按老版本默认关
	v, err := e.do(t, localUI, "gameai.get", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	if v.(*chatai.View).WhitelistOn {
		t.Fatal("1.21.1 缺 white-list 行时应为 false")
	}

	// 升到 26.3 且 server.properties 仍缺 white-list 行：服务端默认是开，游戏内 AI 也应算作开
	i, err := e.mgr.Get("生存服")
	if err != nil {
		t.Fatal(err)
	}
	i.MC = "26.3"
	v, err = e.do(t, localUI, "gameai.get", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.(*chatai.View).WhitelistOn {
		t.Fatal("26.3 缺 white-list 行时应按服务端默认算作 true")
	}

	// 26.3 里明确写了 white-list=false：按文件里的关算
	if err := os.WriteFile(filepath.Join(e.dir, "server.properties"), []byte("white-list=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = e.do(t, localUI, "gameai.get", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	if v.(*chatai.View).WhitelistOn {
		t.Fatal("26.3 明确写了 white-list=false 时应为 false")
	}
}

func TestDismissStartErrorOp(t *testing.T) {
	e := newGameEnv(t)
	i, err := e.mgr.Get("生存服")
	if err != nil {
		t.Fatal(err)
	}
	i.JavaPath = filepath.Join(t.TempDir(), "missing-java.exe")
	if err := e.mgr.Start("生存服"); err == nil {
		t.Fatal("找不到 Java 时应启动失败")
	}
	if i.LastStartError() == "" {
		t.Fatal("应记下 LastStartError")
	}
	// AI 不许调关闭提示操作（HumanOnly）
	if _, err := e.do(t, ops.Caller{Source: ops.SrcAssistant, Actor: "AI"}, "instance.starterr.clear", map[string]any{"inst": "生存服"}); ops.CodeOf(err) != 403 {
		t.Fatalf("AI 调 instance.starterr.clear 应 403：%v", err)
	}
	if _, err := e.do(t, localUI, "instance.starterr.clear", map[string]any{"inst": "生存服"}); err != nil {
		t.Fatalf("人工关闭启动失败提示应成功：%v", err)
	}
	if i.LastStartError() != "" {
		t.Fatalf("关闭后 LastStartError 应清空：%q", i.LastStartError())
	}
	_ = inst.EffectiveWhitelistOn
}
