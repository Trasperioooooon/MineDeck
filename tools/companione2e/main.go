//go:build companione2e

// Command companione2e 是 AI 伙伴两半联调的端到端实测：MineDeck 这一半的真代码（internal/companion 的安装、协议 v1 客户端、
// 管理器，internal/chatai 的游戏内指挥，internal/diag 的排错规则）对真的无头服务器和真的伙伴模组（内置的 jar）。
// 依赖从真的 Modrinth 下载；玩家是 mineflayer 机器人（bot.js）；AI 是本机的假服务（fakeai.go）。
//
// 只在 Linux 上跑（要 java、node、iptables），用 build tag 和 go test ./... 隔开：
//
//	go run -tags companione2e ./tools/companione2e -loader neo -work <目录> -template <装好加载器的服务端目录> ...
//
// 用法和每一步测什么见同目录的 README.md。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// preset 一个加载器。
type preset struct {
	core, mc, build string
	argsFile        string // 相对实例目录的启动参数文件（安装器生成的 unix_args.txt）
	botVersion      string
	botMode         string
	gamePort        int
	ctlPort         int
}

var presets = map[string]preset{
	"neo": {core: "neoforge", mc: "1.21.1", build: "21.1.265", argsFile: "libraries/net/neoforged/neoforge/21.1.265/unix_args.txt",
		botVersion: "1.21.1", botMode: "neoforge", gamePort: 25631, ctlPort: 25641},
	"forge": {core: "forge", mc: "1.20.1", build: "47.4.28", argsFile: "libraries/net/minecraftforge/forge/1.20.1-47.4.28/unix_args.txt",
		botVersion: "1.20.1", botMode: "forge", gamePort: 25632, ctlPort: 25643},
}

type options struct {
	loader, work, template, java, node, nodeModules, repo, oldRev, steps, modJar string
	xmx                                                                          int
	extraJVM                                                                     string
	gamePort, ctlPort                                                            int
}

func main() {
	var o options
	flag.StringVar(&o.loader, "loader", "", "neo（NeoForge 1.21.1）或 forge（Forge 1.20.1）")
	flag.StringVar(&o.work, "work", "", "工作目录：实例、日志、证据都放在 <work>/<loader>/ 下（不要放进仓库）")
	flag.StringVar(&o.template, "template", "", "装好加载器的服务端目录（用加载器的安装器 --installServer 装的）：只取它的 libraries/")
	flag.StringVar(&o.java, "java", "java", "Java（NeoForge 1.21.1 要 21，Forge 1.20.1 要 17）")
	flag.StringVar(&o.node, "node", "node", "node")
	flag.StringVar(&o.nodeModules, "node-modules", "", "装了 mineflayer 的 node_modules 目录")
	flag.StringVar(&o.repo, "repo", ".", "仓库根目录（从 git 历史里取旧版伙伴模组）")
	flag.StringVar(&o.oldRev, "old-rev", "9944d36", "旧版伙伴模组（原型 0.1.0）所在的提交")
	flag.StringVar(&o.steps, "steps", "all", "要跑的步骤，逗号隔开：install,connect,manage,chat,water,reconnect,portchange,diag,exit,stop,restart；all 是全部")
	flag.StringVar(&o.modJar, "mod-jar", "", "装好以后用这个 jar 换掉内置的伙伴模组（只为和别的版本对比，比如旧版 0.2.0；安装那几项照常按内置的查）")
	flag.IntVar(&o.xmx, "xmx", 2048, "服务端 -Xmx（MB）")
	flag.StringVar(&o.extraJVM, "jvm", "", "额外的 JVM 参数（空格隔开），加在 inst.launchArgs 那几个后面")
	flag.IntVar(&o.gamePort, "game-port", 0, "游戏端口（默认 neo 25631、forge 25632）")
	flag.IntVar(&o.ctlPort, "ctl-port", 0, "伙伴模组的控制端口（默认 neo 25641、forge 25643；改端口那一步用它 +1）")
	flag.Parse()
	p, ok := presets[o.loader]
	if !ok || o.work == "" || o.template == "" || o.nodeModules == "" {
		fmt.Fprintln(os.Stderr, "要给 -loader neo|forge、-work、-template、-node-modules（见 README.md）")
		os.Exit(2)
	}
	if o.gamePort > 0 {
		p.gamePort = o.gamePort
	}
	if o.ctlPort > 0 {
		p.ctlPort = o.ctlPort
	}
	dir := filepath.Join(o.work, o.loader)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rep, err := newReporter(filepath.Join(dir, "evidence.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e := &env{o: o, p: p, dir: dir, rep: rep, name: "联调-" + o.loader, steps: map[string]bool{}}
	for _, s := range strings.Split(o.steps, ",") {
		e.steps[strings.TrimSpace(s)] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	e.ctx = ctx
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { // 中途打断：照样关服、退出玩家、去掉 iptables 规则
		<-sig
		e.rep.logf("收到中断信号，清理后退出")
		e.cleanup()
		e.rep.summary()
		os.Exit(130)
	}()
	failed := e.run()
	os.Exit(map[bool]int{true: 1, false: 0}[failed > 0])
}

// ---------- 结果和证据 ----------

type result struct{ ID, Name, Status, Note string }

type reporter struct {
	mu  sync.Mutex
	f   *os.File
	res []result
	t0  time.Time
}

func newReporter(path string) (*reporter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &reporter{f: f, t0: time.Now()}, nil
}

func (r *reporter) logf(format string, a ...any) {
	line := fmt.Sprintf("[%6.1fs] ", time.Since(r.t0).Seconds()) + fmt.Sprintf(format, a...)
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintln(r.f, line)
	fmt.Println(line)
}

// check 记一项结果；note 是证据要点。
func (r *reporter) check(id, name string, ok bool, note string, a ...any) bool {
	st := "PASS"
	if !ok {
		st = "FAIL"
	}
	n := fmt.Sprintf(note, a...)
	r.mu.Lock()
	r.res = append(r.res, result{id, name, st, n})
	r.mu.Unlock()
	r.logf("%s %s %s：%s", st, id, name, n)
	return ok
}

func (r *reporter) skip(id, name, note string) {
	r.mu.Lock()
	r.res = append(r.res, result{id, name, "SKIP", note})
	r.mu.Unlock()
	r.logf("SKIP %s %s：%s", id, name, note)
}

func (r *reporter) summary() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	failed := 0
	fmt.Fprintln(r.f, "\n==== 汇总 ====")
	fmt.Println("\n==== 汇总 ====")
	for _, x := range r.res {
		line := fmt.Sprintf("%-4s %-6s %s：%s", x.Status, x.ID, x.Name, x.Note)
		fmt.Fprintln(r.f, line)
		fmt.Println(line)
		if x.Status == "FAIL" {
			failed++
		}
	}
	fmt.Fprintf(r.f, "共 %d 项，失败 %d 项，用时 %.0f 秒\n", len(r.res), failed, time.Since(r.t0).Seconds())
	fmt.Printf("共 %d 项，失败 %d 项，用时 %.0f 秒\n", len(r.res), failed, time.Since(r.t0).Seconds())
	return failed
}

// waitFor 每 200 毫秒看一次，d 之内 f 为 true 就返回 true。
func waitFor(d time.Duration, f func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if f() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
