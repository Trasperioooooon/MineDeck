package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"minedeck/internal/ops"
)

// world.status、world.import.cancel 接上了 worldimport 的任务登记（A53）：没有正在跑的换地图时不带 task；
// 取消不了时报错里是 worldimport 给的原因。有任务时的样子在 worldimport 的用例里测（那边能停在复制、换地图的那一刻）。
func TestWorldStatusAndCancelWiring(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	res, err := reg.Do(ctx, ui, "world.status", ops.Args(`{"inst":"生存服"}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), `"task"`) {
		t.Fatalf("没有正在跑的换地图时不该带 task：%s", b)
	}
	if _, err := reg.Do(ctx, ui, "world.import.cancel", ops.Args(`{"taskId":"t-none"}`)); err == nil || !strings.Contains(err.Error(), "这个任务已经不能取消了") {
		t.Fatalf("没有这个任务时：%v", err)
	}
}
