package inst

import (
	"fmt"
	"time"
)

// Schedule 定时任务：每天 At（HH:MM）执行。
type Schedule struct {
	Type string `json:"type"` // restart | command | backup
	At   string `json:"at"`   // "HH:MM"
	Args string `json:"args,omitempty"`
}

// Policies 实例运维策略。
type Policies struct {
	CrashRestart bool       `json:"crashRestart"`
	Schedules    []Schedule `json:"schedules,omitempty"`
}

// AutoAction 程序自己执行的一次操作（定时任务、崩溃自动重启），交给操作层写一条操作记录（来源 schedule），
// 事后能查到「服务器是谁重启的」。
type AutoAction struct {
	Op    string         // 对应的操作名，如 instance.restart
	Actor string         // "定时任务"、"崩溃自动重启"
	Args  map[string]any // 含实例名 inst
	Err   error
	Dur   time.Duration
}

// SetAutoHook 登记自动操作的回调（操作层在启动时注入）。
func (m *Manager) SetAutoHook(fn func(AutoAction)) {
	m.autoMu.Lock()
	m.autoHook = fn
	m.autoMu.Unlock()
}

func (m *Manager) noteAuto(a AutoAction) {
	m.autoMu.Lock()
	fn := m.autoHook
	m.autoMu.Unlock()
	if fn != nil {
		fn(a)
	}
}

// UpdatePolicies 更新实例策略。
func (m *Manager) UpdatePolicies(name string, p Policies) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	for _, s := range p.Schedules {
		if s.Type != "restart" && s.Type != "command" && s.Type != "backup" {
			return fmt.Errorf("未知任务类型: %s", s.Type)
		}
		var h, mi int
		if _, err := fmt.Sscanf(s.At, "%d:%d", &h, &mi); err != nil || h < 0 || h > 23 || mi < 0 || mi > 59 {
			return fmt.Errorf("时间格式应为 HH:MM: %s", s.At)
		}
		if s.Type == "command" && s.Args == "" {
			return fmt.Errorf("定时命令不能为空")
		}
	}
	i.procMu.Lock()
	defer i.procMu.Unlock()
	i.Policies = p
	return m.saveInstance(i) // 持锁落盘，避免与并发写竞争地读写 i.Settings
}

// crashRestart 崩溃后按退避策略自动重启（10s/30s/60s，连续 3 次放弃；
// 正常运行超过 10 分钟后计数清零）。gen 为崩溃那次运行的代号：只要期间又发生过任何
// 新的运行（用户手动启动/停止、或上一次退避重启），本次重启即作废，避免把用户主动
// 停掉的服务器又拉起来。
func (m *Manager) crashRestart(i *Instance, gen int64) {
	i.procMu.Lock()
	if i.runGen != gen {
		// 期间已有新的运行发生（用户手动启停/上次退避重启），本次作废：
		// 在自增计数与清零之前就返回，避免污染当前运行的连崩计数与清零时基。
		i.procMu.Unlock()
		return
	}
	if time.Since(i.startedAt) > 10*time.Minute {
		i.crashCount = 0
	}
	i.crashCount++
	n := i.crashCount
	i.procMu.Unlock()
	if n > 3 {
		i.Console.Append("[MineDeck] ⚠ 连续崩溃 3 次，已停止自动重启。请检查上方日志排查原因。")
		return
	}
	delay := []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second}[n-1]
	i.Console.Append(fmt.Sprintf("[MineDeck] 检测到服务器异常退出，%v 后自动重启（第 %d/3 次）...", delay, n))
	time.Sleep(delay)
	i.procMu.Lock()
	abort := i.runGen != gen || (i.state != "" && i.state != "stopped")
	i.procMu.Unlock()
	if abort {
		return // 用户已手动处理（重启过或正在运行）
	}
	start := time.Now()
	err := m.Start(i.Name)
	if err != nil {
		i.Console.Append("[MineDeck] 自动重启失败: " + err.Error())
	}
	m.noteAuto(AutoAction{Op: "instance.start", Actor: "崩溃自动重启", Args: map[string]any{"inst": i.Name, "attempt": n}, Err: err, Dur: time.Since(start)})
}

// startScheduler 全局定时任务调度（每 20 秒检查一次到点任务）。
func (m *Manager) startScheduler() {
	go func() {
		fired := map[string]string{}
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for range tick.C {
			now := time.Now()
			hhmm := now.Format("15:04")
			stamp := now.Format("2006-01-02 ") + hhmm
			for _, i := range m.List() {
				i.procMu.Lock()
				scheds := append([]Schedule{}, i.Policies.Schedules...)
				i.procMu.Unlock()
				for idx, s := range scheds {
					if s.At != hhmm {
						continue
					}
					key := fmt.Sprintf("%s|%d|%s", i.Name, idx, s.Type)
					if fired[key] == stamp {
						continue
					}
					fired[key] = stamp
					go m.execSchedule(i, s)
				}
			}
		}
	}()
}

func (m *Manager) execSchedule(i *Instance, s Schedule) {
	start := time.Now()
	note := func(op string, args map[string]any, err error) {
		args["inst"], args["at"] = i.Name, s.At
		m.noteAuto(AutoAction{Op: op, Actor: "定时任务", Args: args, Err: err, Dur: time.Since(start)})
	}
	switch s.Type {
	case "command":
		if i.Status() == "running" {
			err := m.Command(i.Name, s.Args)
			note("instance.command", map[string]any{"cmd": s.Args}, err)
		}
	case "backup":
		_, err := m.CreateBackup(i.Name, "定时")
		if err != nil {
			i.Console.Append("[MineDeck] 定时备份失败: " + err.Error())
		} else {
			i.Console.Append("[MineDeck] 定时备份完成")
		}
		note("instance.backup.create", map[string]any{"label": "定时"}, err)
	case "restart":
		if i.Status() != "running" {
			return
		}
		i.Console.Append("[MineDeck] 定时重启：60 秒后重启服务器")
		_ = m.Command(i.Name, "say §c服务器将在 60 秒后定时重启，请注意安全下线！")
		time.Sleep(50 * time.Second)
		_ = m.Command(i.Name, "say §c10 秒后重启！")
		time.Sleep(10 * time.Second)
		if err := m.Stop(i.Name); err != nil {
			note("instance.restart", map[string]any{}, err)
			return
		}
		for k := 0; k < 80; k++ { // 最多等 40 秒
			if i.Status() == "stopped" {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		err := fmt.Errorf("等了 40 秒服务器还没停下来，没有重新启动")
		if i.Status() == "stopped" {
			if err = m.Start(i.Name); err != nil {
				i.Console.Append("[MineDeck] 定时重启失败: " + err.Error())
			}
		}
		note("instance.restart", map[string]any{}, err)
	}
}
