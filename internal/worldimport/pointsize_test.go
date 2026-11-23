package worldimport

import (
	"sync/atomic"
	"testing"
)

// countSizes 把 countPointBytes 换成会计数的，返回读数的函数。
func countSizes(t *testing.T) func() int64 {
	t.Helper()
	var n atomic.Int64
	old := countPointBytes
	countPointBytes = func(root string) int64 { n.Add(1); return old(root) }
	t.Cleanup(func() { countPointBytes = old })
	return n.Load
}

// 恢复点的大小算一次就缓存起来（A53）：记录没变时再查状态不再数；复制时数到的 0 不会留到提交以后（换回键照样有）；
// 记录一变（重存一次）就重数；删掉的恢复点跟着出去。
func TestStatusCachesPointSizes(t *testing.T) {
	e := newEnv(t, "")
	counted := countSizes(t)
	var duringCopy *WorldStatus
	stagedHook = func() { duringCopy, _ = e.s.Status("生存服") }
	t.Cleanup(func() { stagedHook = nil })
	if _, msg := e.wait(mustExecute(t, e, e.plan(e.bukkitZip()))); msg != "" {
		t.Fatal(msg)
	}
	if duringCopy == nil || len(duringCopy.Points) != 1 || duringCopy.Points[0].HasOld {
		t.Fatalf("测试前提：复制的时候查过一次，那时恢复点里还没有旧世界：%+v", duringCopy)
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) != 1 || !st.Points[0].HasOld || !st.Points[0].CanReturn || st.Points[0].OldBytes == 0 {
		t.Fatalf("提交以后应能换回：%+v %v", st, err)
	}
	n := counted()
	again, _ := e.s.Status("生存服")
	if counted() != n {
		t.Fatalf("记录没变，第二次查状态又数了 %d 次", counted()-n)
	}
	if again.Points[0].OldBytes != st.Points[0].OldBytes || !again.Points[0].CanReturn {
		t.Fatalf("缓存里的数不对：%+v", again.Points[0])
	}
	// 记录重存一次（更新时间变了）：重数
	j, err := loadJournal(e.dir, st.Points[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveJournal(e.dir, j); err != nil {
		t.Fatal(err)
	}
	n = counted()
	if _, err := e.s.Status("生存服"); err != nil || counted() == n {
		t.Fatalf("记录变了应重数：%v", err)
	}
	// 删掉恢复点：列表里没有了，缓存里也不留
	if err := e.s.DeletePoint("生存服", st.Points[0].ID, "delete"); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.s.Status("生存服"); len(st.Points) != 0 {
		t.Fatalf("删掉以后还列着：%+v", st.Points)
	}
	e.s.mu.Lock()
	left := len(e.s.sizes[sizeKey(e.dir)])
	e.s.mu.Unlock()
	if left != 0 {
		t.Fatalf("删掉的恢复点还留在缓存里：%d 条", left)
	}
}

// 作废和数大小撞在一起：作废之前开始数的，数完不存（不然退回以后才删掉的暂存区会一直按删之前的大小显示）。
func TestForgetSizesDuringCount(t *testing.T) {
	e := newEnv(t, "")
	if _, msg := e.wait(mustExecute(t, e, e.plan(e.bukkitZip()))); msg != "" {
		t.Fatal(msg)
	}
	old := countPointBytes
	t.Cleanup(func() { countPointBytes = old })
	once := false
	countPointBytes = func(root string) int64 {
		if !once {
			once = true
			e.s.forgetSizes() // 数到一半，别处做完了一次换地图
		}
		return old(root)
	}
	if _, err := e.s.Status("生存服"); err != nil {
		t.Fatal(err)
	}
	e.s.mu.Lock()
	left := len(e.s.sizes[sizeKey(e.dir)])
	e.s.mu.Unlock()
	if left != 0 {
		t.Fatal("作废以前开始数的结果不该存进缓存")
	}
}
