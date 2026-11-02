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

// SetJava 给实例换 Java：path 是 java.exe 的路径（调用方保证是已知的 Java），下次启动生效；同时重新生成启动脚本。
func (m *Manager) SetJava(name string, major int, path string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return fmt.Errorf("找不到 Java：%s", path)
	}
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
	if err := m.Stop(name); err != nil {
		return err
	}
	deadline := time.Now().Add(40 * time.Second)
	for i.Status() != "stopped" {
		if time.Now().After(deadline) {
			return fmt.Errorf("等了 40 秒服务器还没停下来，没有重新启动")
		}
		time.Sleep(300 * time.Millisecond)
	}
	return m.Start(name)
}
