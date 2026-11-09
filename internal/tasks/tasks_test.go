package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestListKeepsRunningAndEndedTasksNewestFirst(t *testing.T) {
	mg := NewManager()
	if b, err := json.Marshal(mg.List()); err != nil || string(b) != "[]" {
		t.Fatalf("空任务列表：%s, %v", b, err)
	}
	first, _ := mg.New("创建服务器", []string{"下载"})
	first.StartStep(0)
	first.ProgressFn("下载核心")(50, 100)
	second, _ := mg.New("导入整合包", []string{"导入"})
	second.AddWarning(Warning{Title: "需补装模组"})
	second.Finish("测试服")
	third, _ := mg.New("失败任务", []string{"下载"})
	third.Fail(fmt.Errorf("下载失败"))
	for n := 0; n < 9; n++ {
		task, _ := mg.New("并行任务", nil)
		task.Finish("")
	}
	list := mg.List()
	if len(list) != 12 || list[0].ID != "t12" || list[9].ID != third.ID() || list[10].ID != second.ID() || list[11].ID != first.ID() {
		t.Fatalf("未按创建顺序倒序返回：%+v", list)
	}
	if list[9].Err != "下载失败" || !list[10].Ended || list[10].Result != "测试服" || list[10].WarningCount != 1 || list[11].Ended || list[11].Done != 50 {
		t.Fatalf("任务进度或结果丢失：%+v", list[9:])
	}
	// 列表只给摘要：步骤、日志、告警明细留给单个任务的查询，任务再多也不会把几百行日志带进列表
	first.Logf("一行日志")
	b, err := json.Marshal(mg.List())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"steps"`, `"log"`, `"warnings"`, "一行日志", "需补装模组"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("任务列表里不该有 %s：%s", s, b)
		}
	}
	// 以后要是清理旧任务，编号中间缺一个也照常列出其余的
	delete(mg.m, second.ID())
	if list := mg.List(); len(list) != 11 || list[9].ID != third.ID() || list[10].ID != first.ID() {
		t.Fatalf("缺一个任务时没有跳过它：%+v", list)
	}
}

// 下载超时、重试（测试报告 B-01）：记下次数和地址（只留主机名）；下载又有进度时不再报，次数接着数；换步骤清零；任务结束后不报。
func TestNetRetry(t *testing.T) {
	mg := NewManager()
	tk, _ := mg.New("新建", []string{"下载", "安装"})
	tk.StartStep(0)
	if s := tk.Snapshot(); s.Net != nil {
		t.Fatalf("还没出问题：%+v", s.Net)
	}
	b, _ := json.Marshal(tk.Snapshot())
	if strings.Contains(string(b), `"net"`) {
		t.Fatalf("没有问题时不该有 net：%s", b)
	}

	tk.NetRetry("https://bmclapi2.bangbang93.com/maven/x.jar", "下载失败")
	tk.NetRetry("maven.neoforged.net", "连接超时")
	n := tk.Snapshot().Net
	if n == nil || n.Count != 2 || n.Host != "maven.neoforged.net" || n.What != "连接超时" || n.Ago != 0 {
		t.Fatalf("两次以后：%+v", n)
	}

	prog := tk.ProgressFn("下载 x.jar")
	prog(1024, 4096)
	if s := tk.Snapshot(); s.Net != nil {
		t.Fatalf("下载动起来以后不该再报：%+v", s.Net)
	}
	tk.NetRetry("cdn.example.com", "下载失败")
	if n := tk.Snapshot().Net; n == nil || n.Count != 3 || n.Host != "cdn.example.com" {
		t.Fatalf("次数应接着数：%+v", n)
	}

	tk.StartStep(1)
	if s := tk.Snapshot(); s.Net != nil {
		t.Fatalf("换步骤后应清零：%+v", s.Net)
	}
	tk.NetRetry("", "安装器出错退出，第 2 次重试")
	if n := tk.Snapshot().Net; n == nil || n.Count != 1 || n.Host != "" {
		t.Fatalf("换步骤后从 1 数：%+v", n)
	}
	tk.Finish("S1")
	if s := tk.Snapshot(); s.Net != nil {
		t.Fatalf("结束后不该再报：%+v", s.Net)
	}
}

// 待决清单（整合包导入）：快照要带出去给处置面板，处置后清单缩小、全部处理完清空；
// 列表摘要只给条数（pendingCount）。
func TestPendingList(t *testing.T) {
	mg := NewManager()
	tk, _ := mg.New("导入整合包 x", nil)
	if sn := tk.Snapshot(); len(sn.Pending) != 0 {
		t.Fatalf("空清单不该有待决文件: %+v", sn.Pending)
	}
	tk.SetPending([]PendingItem{
		{ID: "p1", Path: "mods/custom.jar", Reason: "host", Note: "白名单外", Host: "files.example.net",
			URLs: []string{"https://files.example.net/a.jar"}, SHA1: "aa", Options: []string{"local", "skip", "trust"}},
		{ID: "p2", Path: "mods/other.jar", Reason: "nohash", Options: []string{"skip", "local"}},
	})
	sn := tk.Snapshot()
	if len(sn.Pending) != 2 || sn.Pending[0].Host != "files.example.net" || sn.Pending[1].ID != "p2" {
		t.Fatalf("快照应逐项带出: %+v", sn.Pending)
	}
	if s := mg.List()[0]; s.PendingCount != 2 {
		t.Fatalf("摘要应给待决条数: %+v", s)
	}
	removed := tk.ResolvePending([]string{"p1", "p9"})
	if len(removed) != 1 || removed[0].ID != "p1" {
		t.Fatalf("只应移走存在的: %+v", removed)
	}
	if left := tk.PendingItems(); len(left) != 1 || left[0].ID != "p2" {
		t.Fatalf("清剩下的不对: %+v", left)
	}
	tk.ResolvePending([]string{"p2"})
	if left := tk.PendingItems(); len(left) != 0 {
		t.Fatalf("应清空: %+v", left)
	}
	if s := mg.List()[0]; s.PendingCount != 0 {
		t.Fatalf("清空后摘要应为 0: %+v", s)
	}
}
