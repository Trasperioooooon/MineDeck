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
	if list[9].Err != "下载失败" || !list[10].Ended || list[10].Result != "测试服" || len(list[10].Warnings) != 1 || list[11].Ended || list[11].Done != 50 {
		t.Fatalf("任务进度或结果丢失：%+v", list[9:])
	}
	list[11].Steps[0].Name = "改过的快照"
	if first.Snapshot().Steps[0].Name != "下载" {
		t.Fatal("修改列表快照影响了原任务")
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
