package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A23.1：同一个文件夹里同时只许开一份 MineDeck；拿不到时等满时限后提示「这个文件夹里的 MineDeck 已经开着了」，
// 不同文件夹各放一份可同时开，旧进程在等待期内退出则新进程可接着拿到。
func TestSingleInstancePerFolder(t *testing.T) {
	oldName, oldWait, oldAlert := singleInstanceMutexName, singleInstanceWait, singleInstanceAlert
	t.Cleanup(func() {
		singleInstanceMutexName = oldName
		singleInstanceWait = oldWait
		singleInstanceAlert = oldAlert
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

	// 测试里替换互斥量前缀和等待时长，避免和本机可能开着的程序撞名，也不用等满 10 秒。
	prefix := fmt.Sprintf(`Local\MineDeck-Test-%d-`, os.Getpid())
	singleInstanceMutexName = func(dir string) string {
		base := defaultSingleInstanceMutexName(dir)
		return prefix + strings.TrimPrefix(base, `Local\MineDeck-`)
	}
	singleInstanceWait = 50 * time.Millisecond

	var alerts []string
	singleInstanceAlert = func(msg string) {
		alerts = append(alerts, msg)
	}

	// 第一个「进程」占住 dirA
	relA1, ok := ensureSingleInstance(dirA)
	if !ok {
		t.Fatal("第一个实例应当成功拿到互斥量")
	}
	defer func() {
		if relA1 != nil {
			relA1()
		}
	}()

	// 同文件夹第二个「进程」拿不到，等满超时后弹提示并返回 false
	relA2, ok := ensureSingleInstance(dirA)
	if ok {
		if relA2 != nil {
			relA2()
		}
		t.Fatal("同一个文件夹里的第二个实例不应拿到互斥量")
	}
	if len(alerts) != 1 || alerts[0] != "这个文件夹里的 MineDeck 已经开着了" {
		t.Fatalf("提示文案不对：%q", alerts)
	}

	// 不同文件夹里的另一份 MineDeck 可以同时开
	relB, ok := ensureSingleInstance(dirB)
	if !ok {
		t.Fatal("不同文件夹里的 MineDeck 应当能同时启动")
	}
	relB()
	if len(alerts) != 1 {
		t.Fatalf("不同文件夹正常启动不应弹提示：%q", alerts)
	}

	// 模拟应用内更新重启：旧实例在等待窗口内释放互斥量，新实例应顺利接手且不弹提示
	singleInstanceWait = 2 * time.Second
	var released atomic.Bool
	go func() {
		time.Sleep(30 * time.Millisecond)
		released.Store(true)
		relA1()
		relA1 = nil
	}()
	relA3, ok := ensureSingleInstance(dirA)
	if !ok {
		t.Fatal("旧实例在等待期内退出后，新实例应当能顺利启动")
	}
	if !released.Load() {
		t.Fatal("新实例不应在旧实例释放互斥量之前提前拿到")
	}
	relA3()
	if len(alerts) != 1 {
		t.Fatalf("等待期内接手成功不应多弹提示：%q", alerts)
	}
}
