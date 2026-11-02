package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

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
