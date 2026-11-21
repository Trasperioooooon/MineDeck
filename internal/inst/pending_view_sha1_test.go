package inst

import (
	"encoding/json"
	"strings"
	"testing"

	"minedeck/internal/tasks"
)

// 界面上「全选可下载项」只选带校验值的行，靠的是 GET import-pending 的每一项里带着 sha1（PR #31 复审）。
// 这里锁住这个约定：有校验值的项序列化出去带 sha1，没有的不带；跳过的那一栏也一样。
func TestPendingViewCarriesSHA1ForSelectAll(t *testing.T) {
	const sum = "0123456789abcdef0123456789abcdef01234567"
	l := &pendingList{Items: []pendingRecord{
		{PendingItem: tasks.PendingItem{ID: "p1", Path: "mods/a.jar", Reason: "host", URLs: []string{"https://files.example.net/a.jar"}, SHA1: sum, Options: []string{"local", "skip", "trust"}}},
		{PendingItem: tasks.PendingItem{ID: "p2", Path: "mods/b.jar", Reason: "host", URLs: []string{"https://files.example.net/b.jar"}, Options: []string{"local", "skip", "trust"}}},
		{PendingItem: tasks.PendingItem{ID: "p3", Path: "mods/c.jar", Reason: "host", URLs: []string{"https://files.example.net/c.jar"}, SHA1: sum, Options: []string{"local", "trust"}}, Skipped: true},
	}}
	raw, err := json.Marshal(l.view("服"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Pending []map[string]any `json:"pending"`
		Skipped []map[string]any `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 2 || len(got.Skipped) != 1 {
		t.Fatalf("分栏不对：%s", raw)
	}
	if got.Pending[0]["sha1"] != sum || got.Skipped[0]["sha1"] != sum {
		t.Fatalf("有校验值的项应带 sha1 给界面：%s", raw)
	}
	if _, ok := got.Pending[1]["sha1"]; ok || strings.Contains(string(raw), `"sha1":""`) {
		t.Fatalf("没有校验值的项不应带 sha1：%s", raw)
	}
}
