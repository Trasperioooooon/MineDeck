package inst

import (
	"fmt"
	"os"
	"time"
)

// LastStart 最近一次启动的时间；没启动过为零值。服务器停了也保留（排错时拿它判断崩溃报告是不是这次的）。
func (i *Instance) LastStart() time.Time {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.startedAt
}

// PID 服务器进程的编号；没在运行返回 0。
func (i *Instance) PID() int {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.proc == nil || i.proc.cmd.Process == nil {
		return 0
	}
	return i.proc.cmd.Process.Pid
}

// JavaSnapshot 加锁读取实例用的 Java（大版本和 java.exe 路径）。
func (i *Instance) JavaSnapshot() (major int, path string) {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.JavaMajor, i.JavaPath
}

// RunningJavaPath 当前正在运行（含启动中、停止中）的服务器进程实际执行的 java.exe 路径；未运行时为空。
// 服务器运行中改过 Java 设置（下次启动才生效）时，i.JavaPath 已变成新的，但进程占着的仍是旧的。
func (i *Instance) RunningJavaPath() string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.proc == nil || i.proc.cmd == nil || i.state == "" || i.state == "stopped" {
		return ""
	}
	return i.proc.cmd.Path
}

// HasCreating 当前是否有正在进行的新建、导入或接管任务。
func (m *Manager) HasCreating() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.creating) > 0
}

// SetJava 给实例换 Java：path 是 java.exe 的路径（调用方保证是已知的 Java），下次启动生效；同时重新生成启动脚本。
func (m *Manager) SetJava(name string, major int, path string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return fmt.Errorf("找不到 Java：%s", path)
	}
	release, err := m.UseWorldInstance(i, "保存实例设置")
	if err != nil {
		return err
	}
	defer release()
	i.procMu.Lock()
	defer i.procMu.Unlock()
	i.JavaMajor, i.JavaPath = major, path
	if err := m.saveInstance(i); err != nil {
		return err
	}
	return writeRunBat(i)
}

// Restart 重启：先保存世界并停止（最多等 40 秒），停下来之后再启动。
func (m *Manager) Restart(name string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if err := m.stopWait(i, "没有重新启动"); err != nil {
		return err
	}
	m.waitPortFree(i) // 进程刚退出，端口万一还没放开就等几秒
	return m.Start(name)
}

// StopWait 先保存世界并停止，等它真的停下来（最多 40 秒）再返回；已经停着就直接返回。
// 「停止并还原」用它：停下来以后接着还原（可用性走查 R11），一次确认做完，不用人等着停好再点一次。
// then 说的是停不下来时接下来那件事没做，写进报错（「没有还原」）。
func (m *Manager) StopWait(name, then string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if i.Status() == "stopped" {
		return nil
	}
	return m.stopWait(i, then)
}

func (m *Manager) stopWait(i *Instance, then string) error {
	if err := m.Stop(i.Name); err != nil {
		return err
	}
	deadline := time.Now().Add(stopWaitMax)
	for i.Status() != "stopped" {
		if time.Now().After(deadline) {
			return fmt.Errorf("等了 %d 秒服务器还没停下来，%s", int(stopWaitMax/time.Second), then)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil
}

// stopWaitMax 重启、停止并还原时最多等服务器停下来多久。
var stopWaitMax = 40 * time.Second
