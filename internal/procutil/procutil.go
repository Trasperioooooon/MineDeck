// Package procutil 提供 inst 与 tunnel 共用的进程工具：
// 窗口隐藏、环境净化、Job Object 防孤儿、控制台环形缓冲与订阅。
package procutil

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- 控制台缓冲与订阅 ----------

type Console struct {
	mu    sync.Mutex
	lines []string
	subs  map[chan string]struct{}
}

func NewConsole() *Console {
	return &Console{subs: map[chan string]struct{}{}}
}

func (c *Console) Append(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	if len(c.lines) > 3000 {
		c.lines = c.lines[len(c.lines)-3000:]
	}
	for ch := range c.subs {
		select {
		case ch <- line:
		default: // 订阅者阻塞时丢弃，避免拖死输出
		}
	}
}

// Lines 当前缓冲里的全部行（最多 3000 行）的副本。
func (c *Console) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// Subscribe 返回历史回放与实时通道。
func (c *Console) Subscribe() (replay []string, ch chan string, cancel func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	replay = append(replay, c.lines...)
	ch = make(chan string, 256)
	c.subs[ch] = struct{}{}
	return replay, ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

// ---------- Windows Job Object：确保本程序退出后不留孤儿子进程 ----------

var jobHandle windows.Handle

func InitJob() {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return
	}
	jobHandle = h
}

func AssignToJob(p *os.Process) {
	if jobHandle == 0 || p == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(jobHandle, h)
}

// ---------- 环境隔离与窗口隐藏 ----------

// CleanEnv 移除会污染 Java/frpc 行为的环境变量。
func CleanEnv() []string {
	drop := map[string]bool{
		"JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true,
		"JAVA_OPTS": true, "CLASSPATH": true, "JAVA_HOME": true, "JDK_JAVA_OPTIONS": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}

func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
