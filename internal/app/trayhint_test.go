package app

import "testing"

// 第一次缩到托盘时提醒一次（可用性走查 R17）：弹出来了才记，记下以后不再弹，重启也不再弹；没弹出来的下次再试。
func TestTrayHintOnce(t *testing.T) {
	tempBase(t)
	calls := 0
	fail := func() bool { calls++; return false }
	ok := func() bool { calls++; return true }

	if TrayHintOnce(fail) || GetConfig().TrayHinted {
		t.Fatal("没弹出来时不该记成提醒过")
	}
	if !TrayHintOnce(ok) || !GetConfig().TrayHinted {
		t.Fatal("第一次弹出来以后应记成提醒过")
	}
	if TrayHintOnce(ok) || calls != 2 {
		t.Fatalf("提醒过以后不该再弹（弹了 %d 次）", calls)
	}
	restart()
	if !GetConfig().TrayHinted || TrayHintOnce(ok) || calls != 2 {
		t.Fatalf("重启以后也不该再弹：hinted=%v calls=%d", GetConfig().TrayHinted, calls)
	}
}
