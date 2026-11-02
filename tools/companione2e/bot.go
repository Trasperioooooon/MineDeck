//go:build companione2e

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bot 一个假玩家（bot.js 的 node 进程）。
type bot struct {
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	logf  *os.File

	mu   sync.Mutex
	evs  []botEv
	done chan struct{}
}

type botEv struct {
	T      int64   `json:"t"`
	Ev     string  `json:"ev"`
	Text   string  `json:"text"`
	Reason string  `json:"reason"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
}

// startBot node bot.js --port … --name … --version … --mode …
func startBot(node, script, nodePath string, port int, name, version, mode string, logPath string) (*bot, error) {
	cmd := exec.Command(node, script, "--port", strconv.Itoa(port), "--name", name, "--version", version, "--mode", mode)
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	lf, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd.Stderr = lf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &bot{name: name, cmd: cmd, stdin: stdin, logf: lf, done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			fmt.Fprintln(lf, line)
			var e botEv
			if json.Unmarshal([]byte(line), &e) != nil {
				continue
			}
			b.mu.Lock()
			b.evs = append(b.evs, e)
			b.mu.Unlock()
		}
		_ = cmd.Wait()
		close(b.done)
	}()
	return b, nil
}

func (b *bot) send(line string) error {
	_, err := io.WriteString(b.stdin, line+"\n")
	return err
}

// Chat 在聊天栏说一句话。
func (b *bot) Chat(text string) error { return b.send("chat " + text) }

func (b *bot) Events() []botEv {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]botEv(nil), b.evs...)
}

func (b *bot) Mark() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.evs)
}

// Msgs 第 from 个事件以后收到的聊天和系统消息。
func (b *bot) Msgs(from int) []string {
	var out []string
	for i, e := range b.Events() {
		if i >= from && e.Ev == "msg" {
			out = append(out, e.Text)
		}
	}
	return out
}

// WaitMsg 等第 from 个事件以后收到一条满足 pred 的消息。
func (b *bot) WaitMsg(from int, d time.Duration, pred func(string) bool) (string, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, m := range b.Msgs(from) {
			if pred(m) {
				return m, true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", false
}

// WaitEv 等一个某种事件。
func (b *bot) WaitEv(ev string, d time.Duration) (botEv, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, e := range b.Events() {
			if e.Ev == ev {
				return e, true
			}
		}
		select {
		case <-b.done:
			return botEv{}, false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return botEv{}, false
}

// Pos 问一次自己的位置。
func (b *bot) Pos() (x, y, z float64, ok bool) {
	n := b.Mark()
	if b.send("pos") != nil {
		return 0, 0, 0, false
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for i, e := range b.Events() {
			if i >= n && e.Ev == "pos" {
				return e.X, e.Y, e.Z, true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, 0, 0, false
}

// Quit 退出（最多等 10 秒，不行就杀掉）。
func (b *bot) Quit() {
	_ = b.send("quit")
	select {
	case <-b.done:
	case <-time.After(10 * time.Second):
		_ = b.cmd.Process.Kill()
		<-b.done
	}
	b.logf.Close()
}

// Problems 被踢、出错的事件（看进服失败的原因）。
func (b *bot) Problems() string {
	var out []string
	for _, e := range b.Events() {
		switch e.Ev {
		case "kicked", "error", "end":
			out = append(out, e.Ev+":"+e.Text+e.Reason)
		}
	}
	return strings.Join(out, "；")
}
