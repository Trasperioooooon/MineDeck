//go:build companione2e

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"minedeck/internal/chatai"
	"minedeck/internal/companion"
	"minedeck/internal/events"
	"minedeck/internal/mclog"
)

// server 一台真的无头服务器，照 inst.Manager 的做法托管（inst 只能在 Windows 上编译，这里照抄它和伙伴、游戏内 AI 有关的那几处）：
//   - 启动前调 beforeStart（catalog 里交给音乐共享和 AI 伙伴的开服前回调）；JVM 参数照 inst.launchArgs；环境变量照 procutil.CleanEnv；
//   - 标准输出逐行按 inst.decodeConsole 的 auto 规则解码 → 控制台 → 在线名单（mclog）→ 游戏内 AI 的 OnLine；出现「Done (…)!」就算开好了；
//   - 进程起来就发 instance.start，退出后发 instance.stop（用户停的）或 instance.crash（events 总线，companion.Service 订阅它）；
//   - 停服照 inst.Manager.Stop：状态改成 stopping，往标准输入写 stop。
type server struct {
	name  string
	dir   string
	core  string
	mc    string
	build string
	java  string
	args  []string
	clog  *os.File // 控制台的完整记录

	beforeStart func(name string)
	lineHook    func(inst, line string)

	mu       sync.Mutex
	status   string
	online   map[string]bool
	lines    []string
	startIdx int // 最近一次启动在 lines 里的位置
	started  time.Time
	userStop bool
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	exited   chan struct{}
	inMu     sync.Mutex
}

func newServer(name, dir, core, mc, build, java string, args []string, clog *os.File) *server {
	return &server{name: name, dir: dir, core: core, mc: mc, build: build, java: java, args: args, clog: clog, status: "stopped", online: map[string]bool{}}
}

// cleanEnv 同 procutil.CleanEnv：去掉会改 JVM 行为的环境变量。
func cleanEnv() []string {
	drop := map[string]bool{"JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true, "JAVA_OPTS": true, "CLASSPATH": true, "JAVA_HOME": true, "JDK_JAVA_OPTIONS": true}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}

// decodeConsole 同 inst.decodeConsole 的 auto：优先 UTF-8，非法字节序列按 GBK 解。
func decodeConsole(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if d, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
		return string(d)
	}
	return string(b)
}

// serverReady 同 inst.serverReady（只留 NeoForge、Forge 用到的那一条）。
func serverReady(line string) bool {
	return strings.Contains(line, "Done (") && strings.Contains(line, ")!")
}

func (s *server) appendLine(line string) {
	s.mu.Lock()
	s.lines = append(s.lines, line)
	s.mu.Unlock()
	if s.clog != nil {
		fmt.Fprintf(s.clog, "%s %s\n", time.Now().Format("15:04:05.000"), line)
	}
}

// Lines 最近一次启动以来的控制台（含 MineDeck 自己写的行）。
func (s *server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines[s.startIdx:]...)
}

// LinesFrom 从第 n 行起（全部记录的下标）。
func (s *server) LinesFrom(n int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.lines) {
		n = len(s.lines)
	}
	return append([]string(nil), s.lines[n:]...)
}

func (s *server) Mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}

func (s *server) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *server) OnlinePlayers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.online))
	for n := range s.online {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (s *server) trackPlayers(line string) {
	l := mclog.Parse(line)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch l.Kind {
	case mclog.Join:
		s.online[l.Player] = true
	case mclog.Leave:
		delete(s.online, l.Player)
	}
}

// Start 同 inst.Manager.Start。
func (s *server) Start() error {
	if s.beforeStart != nil {
		s.beforeStart(s.name)
	}
	s.mu.Lock()
	if s.status != "stopped" {
		s.mu.Unlock()
		return fmt.Errorf("服务器当前状态为 %s", s.status)
	}
	s.mu.Unlock()
	cmd := exec.Command(s.java, s.args...)
	cmd.Dir = s.dir
	cmd.Env = cleanEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	s.mu.Lock()
	s.startIdx = len(s.lines)
	s.mu.Unlock()
	s.appendLine("[MineDeck] 启动: java " + strings.Join(s.args, " "))
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cmd, s.stdin, s.status, s.userStop, s.started, s.exited = cmd, stdin, "starting", false, time.Now(), make(chan struct{})
	s.online = map[string]bool{}
	exited := s.exited
	s.mu.Unlock()
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(decodeConsole(sc.Bytes()), "\r")
			s.appendLine(line)
			s.trackPlayers(line)
			if s.lineHook != nil {
				s.lineHook(s.name, line)
			}
			if serverReady(line) {
				s.mu.Lock()
				if s.status == "starting" {
					s.status = "running"
				}
				s.mu.Unlock()
			}
		}
		err := cmd.Wait()
		s.mu.Lock()
		wasUser := s.userStop
		s.status, s.cmd, s.stdin = "stopped", nil, nil
		s.online = map[string]bool{}
		s.mu.Unlock()
		if err != nil {
			s.appendLine(fmt.Sprintf("[MineDeck] 服务器进程已退出（%v）", err))
		} else {
			s.appendLine("[MineDeck] 服务器已正常关闭")
		}
		if wasUser {
			events.Publish("instance.stop", map[string]any{"instance": s.name})
		} else {
			events.Publish("instance.crash", map[string]any{"instance": s.name})
		}
		close(exited)
	}()
	events.Publish("instance.start", map[string]any{"instance": s.name})
	return nil
}

// Stop 同 inst.Manager.Stop：发 stop，等进程退出（最多 limit，超时强杀）。返回用了多久、是不是强杀的。
func (s *server) Stop(limit time.Duration) (time.Duration, bool) {
	s.mu.Lock()
	if s.cmd == nil {
		s.mu.Unlock()
		return 0, false
	}
	s.status, s.userStop = "stopping", true
	exited, cmd := s.exited, s.cmd
	s.mu.Unlock()
	t0 := time.Now()
	s.appendLine("[MineDeck] 正在保存世界并停止服务器...")
	_ = s.write("stop")
	select {
	case <-exited:
		return time.Since(t0), false
	case <-time.After(limit):
		s.appendLine("[MineDeck] 停止超时，强制结束进程")
		_ = cmd.Process.Kill()
		<-exited
		return time.Since(t0), true
	}
}

// Kill 强制结束（清理用）。
func (s *server) Kill() {
	s.mu.Lock()
	cmd, exited := s.cmd, s.exited
	s.userStop = true
	s.mu.Unlock()
	if cmd != nil {
		_ = cmd.Process.Kill()
		<-exited
	}
}

func (s *server) write(line string) error {
	s.mu.Lock()
	w := s.stdin
	s.mu.Unlock()
	if w == nil {
		return fmt.Errorf("服务器未在运行")
	}
	s.inMu.Lock()
	defer s.inMu.Unlock()
	_, err := io.WriteString(w, line+"\n")
	return err
}

// Command 同 inst.Manager.CommandEcho。
func (s *server) Command(cmd, echo string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if echo == "" {
		echo = "> " + cmd
	}
	s.appendLine(echo)
	return s.write(cmd)
}

// ---------- companion.Instances（同 catalog.companionInstances）----------

type compInstances struct{ s *server }

func (w compInstances) Names() []string { return []string{w.s.name} }

func (w compInstances) Info(name string) (companion.InstInfo, error) {
	if name != w.s.name {
		return companion.InstInfo{}, fmt.Errorf("实例不存在: %s", name)
	}
	return companion.InstInfo{Name: name, Dir: w.s.dir, Core: w.s.core, MC: w.s.mc, Build: w.s.build, Status: w.s.Status(), Online: w.s.OnlinePlayers()}, nil
}

func (w compInstances) Log(name, line string) {
	if name == w.s.name {
		w.s.appendLine(line)
	}
}

// ---------- chatai.Backend（同 catalog.gameBackend）----------

type chatBackend struct{ s *server }

func (b chatBackend) Dir(name string) (string, error) {
	if name != b.s.name {
		return "", fmt.Errorf("实例不存在: %s", name)
	}
	return b.s.dir, nil
}

func (b chatBackend) Info(name string) (chatai.Info, error) {
	if name != b.s.name {
		return chatai.Info{}, fmt.Errorf("实例不存在: %s", name)
	}
	b.s.mu.Lock()
	started := b.s.started
	b.s.mu.Unlock()
	return chatai.Info{Core: b.s.core, MC: b.s.mc, Status: b.s.Status(), Started: started}, nil
}

func (b chatBackend) Online(string) []string { return b.s.OnlinePlayers() }

func (b chatBackend) Players(string) (chatai.Players, error) {
	return chatai.Players{Online: b.s.OnlinePlayers()}, nil
}

func (b chatBackend) Send(_, cmd, echo string) error { return b.s.Command(cmd, echo) }
