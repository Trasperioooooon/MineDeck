package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/modcart"
	"minedeck/internal/ops"
)

// 2026-11-10 合并前审计查出来的：往购物车里加东西是读操作，AI 助手不用确认就能做；安装又是照「执行那一刻」的车来装，
// 不管人当时看到的是什么。两样叠起来，写操作要人确认这一层就空了。这里的用例守的是：装的必须是人看过、确认过的那些。

func cartIDs(cart *modcart.Service, inst string) []string {
	var ids []string
	for _, it := range cart.List(inst) {
		ids = append(ids, it.ProjectID)
	}
	return ids
}

func modsIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	ents, _ := os.ReadDir(filepath.Join(dir, "mods"))
	for _, e := range ents {
		if e.Name() != "needy.jar" { // 测试环境里原本就有的那个
			out = append(out, e.Name())
		}
	}
	return out
}

// TestModCartInstallBoundToSeen 人在界面上点「安装」：请求里带着他当时看到的项目，和现在的车对不上就不装。
func TestModCartInstallBoundToSeen(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"fapi"}}); err != nil {
		t.Fatal(err)
	}
	seen := cartIDs(cart, "模组服") // 人打开购物车标签时看到的：只有 Fabric API
	// 之后 AI 又往车里加了一项（读操作，直接执行，不出卡片）
	if _, err := cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	_, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": seen})
	if ops.CodeOf(err) != 409 || !strings.Contains(err.Error(), "不一样") {
		t.Fatalf("车里多了人没看过的东西，安装应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
	if len(cart.List("模组服")) != 2 {
		t.Errorf("被拒以后车不该动：%v", cartIDs(cart, "模组服"))
	}
	// 被拒以后占位要放掉：看过现在的车再点，照常装
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": cartIDs(cart, "模组服")})
	if err != nil {
		t.Fatalf("看到的和车里的一致，应照常安装：%v", err)
	}
	if s := waitTask(t, mgr, m["taskId"].(string)); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "alpha-1.0.jar fabric-api-1.0.jar" {
		t.Errorf("装上的文件不对：%s", got)
	}
	// 顺序不同、少一项、多一项、重复的：只有同一批才算一致
	for _, c := range []struct {
		items []string
		want  []string
		same  bool
	}{
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "a"}, false},
		{nil, []string{}, true},
	} {
		var items []modcart.Item
		for _, id := range c.items {
			items = append(items, modcart.Item{ProjectID: id})
		}
		if got := sameProjects(items, c.want); got != c.same {
			t.Errorf("sameProjects(%v, %v) = %v，应为 %v", c.items, c.want, got, c.same)
		}
	}
}

// TestModCartDraftBoundToSeen 新建向导：建服完成后带着草稿车一起装。草稿车里多出人没看过的东西，
// 或者同名的旧购物车里还剩着东西（以前删掉的同名服务器留下的），都不能悄悄装进新服务器。
func TestModCartDraftBoundToSeen(t *testing.T) {
	reg, _, dir, cart := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{"fapi"}, "core": "fabric", "mc": "1.20.1"}); err != nil {
		t.Fatal(err)
	}
	seen := cartIDs(cart, modcart.Draft) // 向导「选模组」那一步上显示的
	// 实例名对应的那辆车里有东西（AI 加的，或者以前同名服务器留下的）：人在向导上一项都没看到
	if _, err := cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "includeDraft": true, "expect": seen}); ops.CodeOf(err) != 409 {
		t.Fatalf("实例的车里有向导上没显示的东西，应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
}

// TestInstanceDeleteClearsCart 删掉服务器时把它的购物车一起清掉：购物车按实例名存，
// 不清的话以后新建一台同名的，旧车里剩下的东西会被当成新服务器的。
func TestInstanceDeleteClearsCart(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Do(context.Background(), cartUI, "instance.delete", ops.ArgsOf(map[string]any{"inst": "模组服", "files": false})); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if left := cart.List("模组服"); len(left) != 0 {
		t.Errorf("删掉服务器以后它的购物车应清空：%v", cartIDs(cart, "模组服"))
	}
}

// TestModCartProposalBoundAtRun AI 提的安装、人在卡片上确认：确认时会重新预览一遍并比对，但比对和执行之间还隔着一段
// （预览要联网，几秒到几十秒）。这段时间里车变了，或者 Modrinth 上发了新版本，执行时要自己认出来，不装。
func TestModCartProposalBoundAtRun(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"fapi"}}); err != nil {
		t.Fatal(err)
	}
	op := reg.Op("modcart.install")
	if op == nil {
		t.Fatal("没有 modcart.install")
	}
	args := ops.ArgsOf(map[string]any{"inst": "模组服"})
	plan, err := op.Preview(context.Background(), cartUI, args)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := plan.Pre // 人在卡片上确认的那一份
	// 车在确认之后变了（AI 又加了一项）：执行时读到的车和确认的对不上
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Run(ops.WithConfirmedPre(context.Background(), confirmed), cartUI, args); ops.CodeOf(err) != 409 {
		t.Fatalf("确认之后车变了，执行应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("不该落下任何文件：%v", got)
	}
	// 车没变，但要装的文件和卡片上列的不一样了（比如发了新版本）：任务失败，一个文件都不下载
	if _, err := cart.Remove("模组服", "aaa"); err != nil {
		t.Fatal(err)
	}
	stale := map[string]string{"购物车": confirmed["购物车"], "要装的文件": "和卡片上不一样的指纹"}
	res, err := op.Run(ops.WithConfirmedPre(context.Background(), stale), cartUI, args)
	if err != nil {
		t.Fatalf("车对得上时应先建任务：%v", err)
	}
	s := waitTask(t, mgr, res.(map[string]string)["taskId"])
	if !strings.Contains(s.Err, "不一样") {
		t.Fatalf("文件清单对不上，任务应失败：%+v", s)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("文件清单对不上时一个文件都不该下载：%v", got)
	}
	// 都对得上：照常装。任务结束后占位已放掉，这里顺便验证了
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err = op.Run(ops.WithConfirmedPre(context.Background(), confirmed), cartUI, args)
		if err == nil || ops.CodeOf(err) != 409 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond) // 上一个任务刚失败，占位可能还没放掉
	}
	if err != nil {
		t.Fatalf("确认的内容和现在一致，应照常安装：%v", err)
	}
	if s := waitTask(t, mgr, res.(map[string]string)["taskId"]); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "fabric-api-1.0.jar" {
		t.Errorf("装上的应正好是卡片上那一个文件：%s", got)
	}
}
