package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A23.1：同一个文件夹里同时只许开一份 MineDeck，默认等 3 秒；
// 普通启动拿不到时写标准错误并弹系统提示框，带 -nogui 或 -minimized（quiet=true）时只写标准错误不弹框；
// 不同文件夹各放一份可同时开，旧进程在等待期内退出则新进程可接着拿到。
func TestSingleInstancePerFolder(t *testing.T) {
	if singleInstanceWait != 3*time.Second {
		t.Fatalf("默认等待时长应为 3 秒，实际：%v", singleInstanceWait)
	}

	oldName, oldWait := singleInstanceMutexName, singleInstanceWait
	oldAlert, oldStderr := singleInstanceAlert, singleInstanceStderr
	t.Cleanup(func() {
		singleInstanceMutexName = oldName
		singleInstanceWait = oldWait
		singleInstanceAlert = oldAlert
		singleInstanceStderr = oldStderr
	})

	dirA := filepath.Join(t.TempDir(), "MineDeck-Folder-A")
	dirB := filepath.Join(t.TempDir(), "MineDeck-Folder-B")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 核对默认互斥量名：不暴露原始路径或文件夹名，同文件夹大小写/尾斜杠一致，不同文件夹不同。
	nameA := defaultSingleInstanceMutexName(dirA)
	nameB := defaultSingleInstanceMutexName(dirB)
	if strings.Contains(strings.ToLower(nameA), "minedeck-folder-a") || strings.Contains(nameA, ":") {
		t.Fatalf("互斥量名不应直接包含文件夹路径：%q", nameA)
	}
	if got := defaultSingleInstanceMutexName(strings.ToLower(dirA) + `\`); got != nameA {
		t.Fatalf("同一文件夹不同大小写或尾斜杠应得到相同互斥量名：%q vs %q", got, nameA)
	}
	if nameA == nameB {
		t.Fatalf("不同文件夹不应共用同一个互斥量名：%q", nameA)
	}

	// 测试里替换互斥量前缀和等待时长，避免和本机可能开着的程序撞名，也不用等满 3 秒。
	prefix := fmt.Sprintf(`Local\MineDeck-Test-%d-`, os.Getpid())
	singleInstanceMutexName = func(dir string) string {
		base := defaultSingleInstanceMutexName(dir)
		return prefix + strings.TrimPrefix(base, `Local\MineDeck-`)
	}
	singleInstanceWait = 40 * time.Millisecond

	var alerts []string
	var stderrBuf bytes.Buffer
	singleInstanceAlert = func(msg string) {
		alerts = append(alerts, msg)
	}
	singleInstanceStderr = &stderrBuf

	const wantMsg = "这个文件夹里的 MineDeck 已经开着了。如果刚关掉它，等几秒再打开。"

	// 第一个「进程」占住 dirA
	relA1, ok := ensureSingleInstance(dirA, false)
	if !ok {
		t.Fatal("第一个实例应当成功拿到互斥量")
	}
	defer relA1() // relA1 内部由 sync.Once 保护，重复调用安全

	// 同文件夹第二个「进程」（普通启动 quiet=false）：拿不到，写标准错误并弹提示框
	relA2, ok := ensureSingleInstance(dirA, false)
	if ok {
		if relA2 != nil {
			relA2()
		}
		t.Fatal("同一个文件夹里的第二个实例不应拿到互斥量")
	}
	if len(alerts) != 1 || alerts[0] != wantMsg {
		t.Fatalf("普通启动被拦时提示框文案不对：%q", alerts)
	}
	if strings.TrimSpace(stderrBuf.String()) != wantMsg {
		t.Fatalf("普通启动被拦时标准错误输出不对：%q", stderrBuf.String())
	}

	// 带 -nogui 或 -minimized（quiet=true）：拿不到时只写标准错误，绝不弹系统提示框
	for _, flagCase := range []struct {
		name             string
		nogui, minimized bool
	}{
		{"-nogui", true, false},
		{"-minimized", false, true},
	} {
		stderrBuf.Reset()
		relQ, qok := ensureSingleInstance(dirA, flagCase.nogui || flagCase.minimized)
		if qok {
			if relQ != nil {
				relQ()
			}
			t.Fatalf("%s 模式下同文件夹第二个实例不应拿到互斥量", flagCase.name)
		}
		if len(alerts) != 1 {
			t.Fatalf("%s 模式下被拦不应弹系统提示框：alerts=%q", flagCase.name, alerts)
		}
		if strings.TrimSpace(stderrBuf.String()) != wantMsg {
			t.Fatalf("%s 模式下被拦应写标准错误：%q", flagCase.name, stderrBuf.String())
		}
	}

	// 不同文件夹里的另一份 MineDeck 可以同时开
	stderrBuf.Reset()
	relB, ok := ensureSingleInstance(dirB, false)
	if !ok {
		t.Fatal("不同文件夹里的 MineDeck 应当能同时启动")
	}
	relB()
	if len(alerts) != 1 || stderrBuf.Len() != 0 {
		t.Fatalf("不同文件夹正常启动不应弹提示或写错误：alerts=%q stderr=%q", alerts, stderrBuf.String())
	}

	// 模拟刚关掉马上重开：旧实例在等待窗口内释放互斥量，新实例应顺利接手且不弹提示（用 channel 等后台协程退出，无数据竞争）
	singleInstanceWait = 2 * time.Second
	var released atomic.Bool
	relDone := make(chan struct{})
	go func() {
		defer close(relDone)
		time.Sleep(30 * time.Millisecond)
		released.Store(true)
		relA1()
	}()
	relA3, ok := ensureSingleInstance(dirA, false)
	<-relDone
	if !ok {
		t.Fatal("旧实例在等待期内退出后，新实例应当能顺利启动")
	}
	if !released.Load() {
		t.Fatal("新实例不应在旧实例释放互斥量之前提前拿到")
	}
	relA3()
	if len(alerts) != 1 || stderrBuf.Len() != 0 {
		t.Fatalf("等待期内接手成功不应多弹提示或写错误：alerts=%q stderr=%q", alerts, stderrBuf.String())
	}
}
