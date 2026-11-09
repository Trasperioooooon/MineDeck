package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/ops"
)

// 处置待决文件：只许本机的人操作（要收这台电脑上的文件）；任务号和动作的校验在任务层之前先过一遍。
func TestImportPendingOp(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	args := ops.ArgsOf(map[string]any{
		"taskId": "t-nope",
		"items":  []map[string]any{{"id": "p1", "act": "skip"}},
	})

	// LAN / AI（应用内助手、MCP、游戏内）都不行
	if _, err := reg.Do(ctx, ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.5", Local: false}, "instance.import.pending", args); ops.CodeOf(err) != 403 || !strings.Contains(err.Error(), "仅支持在本机操作") {
		t.Errorf("局域网应拒绝：%v", err)
	}
	for _, src := range []ops.Source{ops.SrcAssistant, ops.SrcMCP, ops.SrcGameChat} {
		caller := ops.Caller{Source: src, Actor: "x", Session: "s", Local: true}
		if _, err := reg.Do(ctx, caller, "instance.import.pending", args); ops.CodeOf(err) != 403 {
			t.Errorf("%s 应拒绝：%v", src, err)
		}
	}

	// 本机调用能走到任务层：任务不存在报 404（待决清单的产生在 internal/inst 的测试里覆盖）
	if _, err := reg.Do(ctx, localUI, "instance.import.pending", args); err == nil || ops.CodeOf(err) != 404 || !strings.Contains(err.Error(), "任务不存在") {
		t.Errorf("任务不存在应 404：%v", err)
	}
	// 空清单 400
	if _, err := reg.Do(ctx, localUI, "instance.import.pending", ops.ArgsOf(map[string]any{"taskId": "t-nope", "items": []map[string]any{}})); err == nil || !strings.Contains(err.Error(), "没有要处理的项") {
		t.Errorf("空清单应报错：%v", err)
	}
	// 动作名不在 skip/local/trust 里：按参数说明拒绝
	bad := ops.ArgsOf(map[string]any{"taskId": "t-nope", "items": []map[string]any{{"id": "p1", "act": "rm -rf"}}})
	if _, err := reg.Do(ctx, localUI, "instance.import.pending", bad); err == nil || ops.CodeOf(err) != 400 {
		t.Errorf("不认识的动作应 400：%v", err)
	}
	// 操作参数说明要能编成 JSON（给 AI / 文档用）
	if op := reg.Op("instance.import.pending"); op == nil || op.Params == nil {
		t.Fatal("应登记参数说明")
	}
}

// mods.list 带 needJava：模组声明了 Java 要求时给出来（界面提示换 Java 用；没声明时不给）。
func TestModsListNeedJava(t *testing.T) {
	reg, props := newCatalog(t)
	ctx := context.Background()
	dir := filepath.Dir(props) // 生存服的实例目录（paper：模组目录是 plugins）
	// 放两个 jar：一个要 Java 25，一个超过不了当前版本
	writeTestJar(t, filepath.Join(dir, "plugins", "modded.jar"), map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"modded","version":"1.0","depends":{"java":">=25"}}`,
	})
	writeTestJar(t, filepath.Join(dir, "plugins", "plain.jar"), map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"plain","version":"1.0","depends":{"java":"nope"}}`,
	})
	res, err := reg.Do(ctx, localUI, "instance.mods.list", ops.ArgsOf(map[string]any{"inst": "生存服"}))
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["needJava"] != 25 {
		t.Fatalf("needJava 应为 25: %v", m)
	}
	// needJava 是全目录扫出来的，不受 q 过滤影响；过滤后 mods 为空
	res, err = reg.Do(ctx, localUI, "instance.mods.list", ops.ArgsOf(map[string]any{"inst": "生存服", "q": "不存在"}))
	if err != nil {
		t.Fatal(err)
	}
	m = res.(map[string]any)
	if m["needJava"] != 25 || len(m["mods"].([]any)) != 0 {
		t.Fatalf("q 过滤后 mods 应为空、needJava 应仍为 25: %v", m)
	}
}
