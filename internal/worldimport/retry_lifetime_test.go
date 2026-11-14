package worldimport

import (
	"errors"
	"testing"
	"time"
)

func TestRetryAlwaysReleasesItsHold(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			e := newEnv(t, "")
			j := &Journal{ID: "wretrylifetime", Inst: "生存服", State: TxCommitting, LevelName: "world", Created: time.Now()}
			if err := saveJournal(e.dir, j); err != nil {
				t.Fatal(err)
			}
			e.m.SetWorldBlock("生存服", "待恢复")
			faultHook = func(point string) error {
				if point != "rollback" {
					return nil
				}
				if outcome == "panic" {
					panic("恢复异常")
				}
				if outcome == "failure" {
					return errors.New("恢复失败")
				}
				return nil
			}
			t.Cleanup(func() { faultHook = nil })
			var err error
			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				err = e.s.Retry("生存服")
			}()
			faultHook = nil
			if outcome == "success" && err != nil {
				t.Fatal(err)
			}
			if outcome == "failure" && err == nil {
				t.Fatal("失败不应报告成功")
			}
			if panicked != (outcome == "panic") {
				t.Fatalf("panic=%v", panicked)
			}
			// Recovery 允许启动封锁，但不允许被上一轮尚未释放的 hold 穿过。
			release, err := e.m.BeginWorldRecovery("生存服", "下一次恢复")
			if err != nil {
				t.Fatalf("Retry 留下了占位: %v", err)
			}
			release()
		})
	}
}
