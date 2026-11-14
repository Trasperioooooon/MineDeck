package inst

import (
	"strings"
	"testing"
	"time"
)

// 换地图和备份、打包、删除、保存设置之间的互斥（2026-11-14 Codex 合并复审第 2 条）。
// 以前这些操作只在开头看一眼有没有在换地图，自己不登记；这里测的都是「对方先开始」的那个方向。

// 备份压到一半时，换地图占不上位；备份做完以后才占得上。备份不拦启动。
func TestWorldChangeWaitsForRunningBackup(t *testing.T) {
	m, _ := tempInstance(t)
	started, proceed := make(chan struct{}), make(chan struct{})
	backupHook = func() { close(started); <-proceed }
	t.Cleanup(func() { backupHook = nil })
	done := make(chan error, 1)
	go func() {
		_, err := m.CreateBackup("s", "定时")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("备份没有走到压缩那一步")
	}

	if got := m.WorldUsers("s"); got != "备份" {
		t.Errorf("备份的时候应当登记着：%q", got)
	}
	if _, err := m.BeginWorldChange("s", "换地图"); err == nil || !strings.Contains(err.Error(), "备份") {
		t.Errorf("备份压到一半时换地图不该占得上位，原因要说是备份：%v", err)
	}
	if _, err := m.BeginWorldRecovery("s", "恢复地图"); err == nil {
		t.Error("恢复那条路也一样占不上")
	}
	if err := m.Delete("s", false); err == nil || !strings.Contains(err.Error(), "备份") {
		t.Errorf("备份压到一半时不该删得掉：%v", err)
	}
	// 备份不算「不能动世界」：热备份的时候服务器开着，崩了要能自动重启
	if why := m.WorldBusy("s"); why != "" {
		t.Errorf("备份的时候 WorldBusy 应当是空的（启动查的是它）：%q", why)
	}
	// 备份的时候照常能保存设置
	release, err := m.UseWorld("s", "保存设置")
	if err != nil {
		t.Fatalf("备份的时候应当还能保存设置：%v", err)
	}
	if got := m.WorldUsers("s"); got != "保存设置、备份" && got != "备份、保存设置" {
		t.Errorf("两样都登记着：%q", got)
	}
	release()

	close(proceed)
	if err := <-done; err != nil {
		t.Fatalf("备份应当照常做完：%v", err)
	}
	backupHook = nil
	if got := m.WorldUsers("s"); got != "" {
		t.Errorf("备份做完登记应当撤掉：%q", got)
	}
	hold, err := m.BeginWorldChange("s", "换地图")
	if err != nil {
		t.Fatalf("备份做完以后换地图应当占得上位：%v", err)
	}

	// 反过来：换地图占着位的时候，备份、保存设置、打包都开始不了
	if _, err := m.CreateBackup("s", "x"); err == nil || !strings.Contains(err.Error(), "换地图") {
		t.Errorf("换地图的时候不该备份：%v", err)
	}
	if _, err := m.UseWorld("s", "保存设置"); err == nil {
		t.Error("换地图的时候不该保存设置")
	}
	if _, ok := m.BeginPacking("s"); ok {
		t.Error("换地图的时候不该打包")
	}
	if err := m.Delete("s", false); err == nil {
		t.Error("换地图的时候不该删除")
	}
	hold()
	hold() // 放两次也只算一次

	// 被封锁（有没走完的换地图记录）的时候同理
	m.SetWorldBlock("s", "上次换地图没有完成（测试）")
	if _, err := m.UseWorld("s", "备份"); err == nil || !strings.Contains(err.Error(), "上次换地图没有完成") {
		t.Errorf("被封锁时不该登记得上：%v", err)
	}
	if _, ok := m.BeginPacking("s"); ok {
		t.Error("被封锁时不该打包")
	}
	m.SetWorldBlock("s", "")

	// 登记可以叠：同一样有两个在做，撤掉一个还算在用
	a, _ := m.UseWorld("s", "备份")
	b, _ := m.UseWorld("s", "备份")
	a()
	a()
	if got := m.WorldUsers("s"); got != "备份" {
		t.Errorf("还有一个备份没做完：%q", got)
	}
	b()
	if got := m.WorldUsers("s"); got != "" {
		t.Errorf("都做完了：%q", got)
	}
}

// 删除的时候占着位：删到一半，备份、换地图、打包都进不来。
func TestDeleteHoldsTheInstance(t *testing.T) {
	m, _ := tempInstance(t)
	// 打包中不能删（原有的行为）
	release, ok := m.BeginPacking("s")
	if !ok {
		t.Fatal("应当能开始打包")
	}
	if err := m.Delete("s", false); err == nil || !strings.Contains(err.Error(), "打包") {
		t.Errorf("打包的时候不该删得掉：%v", err)
	}
	release()
	// 正常删除照常，删完登记都清掉
	if err := m.Delete("s", false); err != nil {
		t.Fatalf("删除：%v", err)
	}
	m.mu.Lock()
	held := m.worldHold["s"]
	m.mu.Unlock()
	if held != "" {
		t.Errorf("删完以后占位应当放掉：%q", held)
	}
}
