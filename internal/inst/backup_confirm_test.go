package inst

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"minedeck/internal/diskguard"
	"minedeck/internal/mcsrc"
)

// 备份排队等写盘名额的时候还没有登记「正在备份」：这时候换地图占得上位；
// 换地图占着位，名额到手的备份登记不上、明说原因；位放掉以后备份照常完成。
//
// 第三轮修复里是先登记再等名额：备份排在一次地图暂存后面时一直登记着，暂存一结束备份开始，
// 导入走到提交那一步看到有人在备份就放弃，复制好的地图白做（2026-11-14 第三轮修复的复审，发现 2）。
func TestBackupQueuesForVolumeBeforeRegistering(t *testing.T) {
	m, _ := tempInstance(t)
	if err := os.MkdirAll(backupDirOf("s"), 0o755); err != nil {
		t.Fatal(err)
	}
	queued := func() (chan error, func()) {
		lease, err := diskguard.Acquire(context.Background(), backupDirOf("s"), diskguard.Options{AllowUnknown: true})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := m.CreateBackup("s", "queued"); done <- err }()
		select {
		case err := <-done:
			lease.Close()
			t.Fatalf("名额被占着时备份不该结束：%v", err)
		case <-time.After(200 * time.Millisecond):
		}
		if who := m.WorldUsers("s"); who != "" {
			lease.Close()
			t.Fatalf("排队等名额时不该已经登记：%s", who)
		}
		return done, func() { lease.Close() }
	}
	// 排队期间换地图占上了位：名额到手以后备份登记不上
	done, free := queued()
	release, err := m.BeginWorldChange("s", "换地图")
	if err != nil {
		free()
		t.Fatalf("备份还在排队，换地图应当占得上位：%v", err)
	}
	free()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "换地图") {
		t.Fatalf("换地图占着位时备份应当明说原因：%v", err)
	}
	release()
	if n := len(backupFiles(t, m)); n != 0 {
		t.Fatalf("没做成的备份不该留下文件：%d", n)
	}
	// 没人占位：名额到手就照常备份
	done, free = queued()
	free()
	if err := <-done; err != nil {
		t.Fatalf("名额放出来以后备份应当完成：%v", err)
	}
	if n := len(backupFiles(t, m)); n != 1 {
		t.Fatalf("应当正好有一份备份：%d", n)
	}
}

// 热备等「保存完了」的确认：老版本的说法也认；等不到不作废，照常备份并在控制台留一句。
//
// 第三轮修复里，等不到「Saved the game」就整份作废：保存得慢的大服务器、确认语不同的 1.12.2 服务器，
// 只要开着就一份备份都拿不到（2026-11-14 第三轮修复的复审，发现 3）。
func TestHotBackupSaveConfirmation(t *testing.T) {
	for _, mode := range []string{"world", "silent"} {
		t.Run(mode, func(t *testing.T) {
			m, dir := tempInstance(t)
			i, _ := m.Get("s")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			ln.Close()
			i.Core, i.MC, i.JavaPath, i.LaunchTarget = mcsrc.CoreVanilla, "1.20.1", exe, "jar:server.jar"
			if err := os.WriteFile(i.PropsPath(), []byte(fmt.Sprintf("server-port=%d\n", port)), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv(fakeServerEnv, "1")
			t.Setenv(fakeSaveEnv, mode)
			if mode == "silent" { // 不回确认的那种要等满时间，调短；world 用默认的 15 秒，认不出来就会等满、下面按用时判
				old := saveConfirmWait
				saveConfirmWait = 300 * time.Millisecond
				t.Cleanup(func() { saveConfirmWait = old })
			}
			if err := m.Start("s"); err != nil {
				t.Fatal(err)
			}
			waitStatus(t, i, "running")
			t.Cleanup(func() {
				if i.Status() != "stopped" {
					_ = m.Kill("s")
					waitStatus(t, i, "stopped")
				}
			})
			started := time.Now()
			if _, err := m.CreateBackup("s", mode); err != nil {
				t.Fatalf("热备应当成功：%v", err)
			}
			if n := len(backupFiles(t, m)); n != 1 {
				t.Fatalf("应当正好有一份备份：%d", n)
			}
			if got := readLevel(t, dir); got != "current" {
				t.Fatalf("原世界不应变化：%s", got)
			}
			log := strings.Join(i.Console.Lines(), "\n")
			noted := strings.Contains(log, "没有等到服务器保存完成的确认")
			switch mode {
			case "world":
				if took := time.Since(started); took > 5*time.Second || noted {
					t.Errorf("「Saved the world」应当被认作保存完成：用时 %v，提示=%v", took, noted)
				}
			case "silent":
				if !noted {
					t.Error("没有等到确认时要在控制台说明一句")
				}
			}
			if !strings.Contains(log, "已恢复自动保存") {
				t.Error("备份完要把自动保存开回来")
			}
		})
	}
}
