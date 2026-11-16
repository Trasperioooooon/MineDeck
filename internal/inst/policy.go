package inst

import (
	"errors"
	"fmt"
	"strings"
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

// normHHMM 把「9:05」「09:5」这类时刻规整成补零的 HH:MM；格式不对或越界时 ok 为假。
func normHHMM(s string) (string, bool) {
	var h, mi int
	var rest string
	if n, _ := fmt.Sscanf(strings.TrimSpace(s)+" .", "%d:%d %s", &h, &mi, &rest); n != 3 || rest != "." || h < 0 || h > 23 || mi < 0 || mi > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, mi), true
}

// UpdatePolicies 更新实例策略。
func (m *Manager) UpdatePolicies(name string, p Policies) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	for k, s := range p.Schedules {
		if s.Type != "restart" && s.Type != "command" && s.Type != "backup" {
			return fmt.Errorf("未知任务类型: %s", s.Type)
		}
		at, ok := normHHMM(s.At)
		if !ok {
			return fmt.Errorf("时间格式应为 HH:MM: %s", s.At)
		}
		p.Schedules[k].At = at // 存成补零的 HH:MM：调度按 "15:04" 逐字比对，「9:05」原来永远不会触发
		if s.Type == "command" && s.Args == "" {
			return fmt.Errorf("定时命令不能为空")
		}
	}
	release, err := m.UseWorldInstance(i, "保存实例设置")
	if err != nil {
		return err
	}
	defer release()
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
	handled := func() bool { // 用户已手动处理（重启过或正在运行）
		i.procMu.Lock()
		defer i.procMu.Unlock()
		return i.runGen != gen || (i.state != "" && i.state != "stopped")
	}
	if handled() {
		return
	}
	m.waitPortFree(i) // 端口一般早放开了；还占着就再等几秒，不让一下子的状态把这次重启取消掉
	if handled() {
		return
	}
	start := time.Now()
	err := m.Start(i.Name)
	if err != nil {
		i.Console.Append("[MineDeck] 自动重启失败: " + err.Error() + restartGaveUp(err))
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
					if at, _ := normHHMM(s.At); at != hhmm { // 老配置里可能存着没补零的「9:05」
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

// 定时备份撞上启动、停止时，最多等这么久让服务器稳定下来；每隔 backupSettlePoll 看一次。测试里改短。
var (
	backupSettleWait = 15 * time.Minute
	backupSettlePoll = 2 * time.Second
)

// waitSettled 等到服务器不在启动、也不在停止（运行中或已停止），最多等 max。等到了返回 true。
func (i *Instance) waitSettled(max time.Duration) bool {
	deadline := time.Now().Add(max)
	for {
		if st := i.Status(); st == "running" || st == "stopped" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(backupSettlePoll)
	}
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
		// 撞上服务器启动、停止或重启的，等它稳定下来再试，最多再试两次。
		// 不试的话：备份和重启定在同一分钟（世界大到一分钟压不完，压到一半服务器被重启，这份作废），
		// 或者备份排在重启后一两分钟（服务器还在启动，直接被拒），这一天就没有备份，而且天天如此，
		// 只在控制台留一行字。这两道闸是 2026-11-14 加的，之前这两种情况都照常出一份备份。
		// 再试两次：第一次重试可能正好落在重启中间「已停止」的那一下，随后服务器启动，又被作废一次。
		var unsettled *errBackupUnsettled
		for try := 0; try < 2 && errors.As(err, &unsettled); try++ {
			i.Console.Append("[MineDeck] 定时备份这一次没有做成（" + unsettled.msg + "），等服务器启动或停止完再试")
			if !i.waitSettled(backupSettleWait) {
				break
			}
			_, err = m.CreateBackup(i.Name, "定时")
		}
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
			m.waitPortFree(i) // 进程刚退出，端口万一还没放开就等几秒
			if err = m.Start(i.Name); err != nil {
				i.Console.Append("[MineDeck] 定时重启失败: " + err.Error() + restartGaveUp(err))
			}
		}
		note("instance.restart", map[string]any{}, err)
	}
}
