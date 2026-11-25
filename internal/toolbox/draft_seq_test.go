package toolbox

import "testing"

// A65：同一编辑页发的草稿补丁带 clientId 与递增 seq；旧序号比服务端已存序号小的字段必须忽略，
// 不同字段之间互不影响，不让乱序到达的重试或 pagehide 补发把新值改回旧值。
func TestDraftPatchOutOfOrderIgnored(t *testing.T) {
	svc, _ := twbService(t)
	if _, err := svc.SaveMrpackDraft(DraftConfig{Name: "初始名", Summary: "初始简介"}); err != nil {
		t.Fatal(err)
	}

	// 客户端 A 依次发出 seq=1（临时名）与 seq=2（新名），模拟网络乱序：seq=2 先到，seq=1 后到。
	got2, err := svc.PatchMrpackDraft(DraftPatch{ClientID: "win-a", Seq: 2, Name: twbStr("新名")})
	if err != nil {
		t.Fatalf("seq=2 保存失败：%v", err)
	}
	if got2.Name != "新名" {
		t.Fatalf("seq=2 后 Name = %q，应为 %q", got2.Name, "新名")
	}

	got1, err := svc.PatchMrpackDraft(DraftPatch{ClientID: "win-a", Seq: 1, Name: twbStr("旧名"), Summary: twbStr("顺带改了简介")})
	if err != nil {
		t.Fatalf("乱序旧请求不应该报错：%v", err)
	}
	if got1.Name != "新名" {
		t.Fatalf("乱序到达的 seq=1 不该把 Name 改回旧值，得到 %q", got1.Name)
	}
	// Summary 在 win-a 上之前没有更高 seq：seq=1 的 Summary 仍应生效
	if got1.Summary != "顺带改了简介" {
		t.Fatalf("Summary 之前没有更新的 seq，应保存 %q，得到 %q", "顺带改了简介", got1.Summary)
	}

	// 另一个窗口 B 用自己的 clientId（或较低 seq）修改同一字段不受窗口 A 序号影响
	gotB, err := svc.PatchMrpackDraft(DraftPatch{ClientID: "win-b", Seq: 1, Name: twbStr("窗口B的新名")})
	if err != nil || gotB.Name != "窗口B的新名" {
		t.Fatalf("不同窗口应各自计序号：got=%+v err=%v", gotB, err)
	}
}
