package inst

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"minedeck/internal/events"
	"minedeck/internal/mcsrc"
	"minedeck/internal/procutil"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Console 与进程工具已提升至 procutil 供 inst/tunnel 共用，此处保留薄别名。

type Console = procutil.Console

func newConsole() *Console { return procutil.NewConsole() }

func assignToJob(p *os.Process) { procutil.AssignToJob(p) }

func cleanEnv() []string { return procutil.CleanEnv() }

func hideWindow(cmd *exec.Cmd) { procutil.HideWindow(cmd) }

// ---------- 服务器进程托管 ----------

type proc struct {
	cmd   *exec.Cmd
	stdin *bufio.Writer
	inMu  sync.Mutex
}

// startBusyMsg 服务器没停下时又点启动的说法，状态的叫法和界面上的状态标签一致。
func startBusyMsg(state string) string {
	switch state {
	case "running":
		return "服务器已经在运行了"
	case "starting":
		return "服务器正在启动，等它启动完成"
	case "stopping":
		return "服务器正在停止，等它停下来再启动"
	}
	return "服务器现在的状态是 " + state + "，停下来以后才能启动"
}

// Start 启动服务器进程。
func (m *Manager) Start(name string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	return m.startInstance(i)
}

func (m *Manager) startInstance(i *Instance) error {
	name := i.Name
	release, err := m.UseWorldInstance(i, "准备启动")
	if err != nil {
		return err
	}
	defer release()
	if m.isPacking(name) { // 打的是停机时的完整一份，启动了世界就边写边压了：打完再启
		return fmt.Errorf("这台服务器正在打包交接包，等打包完成后再启动")
	}
	m.beforeStart(i) // 不能持有 procMu：回调里会查实例状态
	// 本软件别的服务器各用哪个端口、开着没有：端口被占时拿它说是谁占着、改成哪个（可用性走查 R08）。
	// 要拿别的服务器的锁，所以在拿自己的锁之前查
	peers := m.portPeers(i)
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.state != "" && i.state != "stopped" {
		return errors.New(startBusyMsg(i.state))
	}
	// 换地图的占位和封锁在 procMu 里再看一遍：占位也是拿着 procMu 占的，两边不会错开（见 worldhold.go）
	if why := m.WorldBusy(name); why != "" {
		return errors.New(why)
	}
	if m.isPacking(name) {
		return fmt.Errorf("这台服务器正在打包交接包，等打包完成后再启动")
	}
	// 从这里往下的失败是「这台服务器起不来」，不是「现在不能启动」（已经在运行、正在打包、正在换地图）：
	// 原因写进控制台，并记成「上次启动没成功」，卡片和详情页头常驻显示，下次进程起来了才清掉（可用性走查 R03）。
	// 右上角的提示一会儿就没了，回来的人只看到一个没反应的「启动」按钮
	if _, err := os.Stat(i.JavaPath); err != nil {
		// 路径不放进报错：界面上的提示遇到路径只写结论，这句「去哪里改」就看不到了。路径记在控制台里，「设置 → 启动与内存」页也看得到
		i.Console.Append(fmt.Sprintf("[MineDeck] 找不到 Java：%s", i.JavaPath))
		return i.startFailed(errors.New("找不到这台服务器用的 Java，请到这台服务器详情页的「设置 → 启动与内存」里重新选一个 Java。"), false)
	}
	// 端口被占着就直接报错，不先起 Java、等它自己报 BindException（检查报告 D-5）。按服务器真正要绑的地址查
	//（server-ip、server-port）；代理端的端口在它自己的配置文件里，不查（见 startPort）
	if t, ok := startPort(i, i.ExtraJVM); ok { // 拿着 procMu，直接读 ExtraJVM
		if err := checkBindPort(t); err != nil {
			// 占着的是本软件管着的另一台（它没停着、端口一样）：报错点它的名字，再找一个能改成的端口，
			// 详情页头给「改成 N 并启动」（R08）。以前一律说「被别的程序占着」，人去找别的程序找不到
			var busy *portBusyError
			if errors.As(err, &busy) {
				for _, p := range peers {
					if p.live && p.port == t.port {
						busy.owner = p.name
						break
					}
				}
				if busy.owner != "" {
					busy.suggest = freePortFrom(t, peers)
				}
			}
			return i.startFailed(err, true)
		}
	}
	// log4shell 防护和现在的做法对不上的（程序启动时那一遍没做成，或者之后才看得出来）：该摘的引用当场摘掉，
	// 要下载的说一声、后台再试，做成了下次启动生效（见 log4j_migrate.go）
	m.log4jBeforeStartLocked(i)

	args := launchArgs(i)
	cmd := exec.Command(i.JavaPath, args...)
	cmd.Dir = i.Dir
	cmd.Env = cleanEnv()
	hideWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return i.startFailed(err, true)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return i.startFailed(err, true)
	}
	cmd.Stderr = cmd.Stdout

	i.Console.Append(fmt.Sprintf("[MineDeck] 启动: java %s", strings.Join(args, " ")))
	if err := cmd.Start(); err != nil {
		return i.startFailed(fmt.Errorf("启动失败: %w", err), true)
	}
	i.startErr, i.startBusy = "", nil // 进程起来了：上次没启动成功的原因不再算数
	assignToJob(cmd.Process)
	pr := &proc{cmd: cmd, stdin: bufio.NewWriter(stdin)}
	i.proc = pr
	i.state = "starting"
	i.userStop = false
	i.killing = false
	i.startedAt = time.Now()
	i.runGen++
	gen := i.runGen // 本次运行的代号：崩溃重启只在无更新的运行发生时才生效
	i.clearOnline()

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(decodeConsole(sc.Bytes(), i.consoleEnc()), "\r")
			i.Console.Append(line)
			if stoppingLineRe.MatchString(line) && !i.markUserStop(pr) {
				// 游戏里的管理员或 RCON 发了 stop（控制台发的在 CommandEcho 里已经记过）
				i.Console.Append("[MineDeck] 服务器正在关闭（有人执行了 stop）")
			}
			i.trackPlayers(line)
			m.noteLine(i, line) // 游戏内 AI：在线名单更新过了再认聊天
			if serverReady(line) {
				i.procMu.Lock()
				became := i.state == "starting"
				if became {
					i.state = "running"
				}
				mc := i.MC
				i.procMu.Unlock()
				if became {
					if cmd := difficultyCommand(mc, javaPropsOf(i.PropsPath())); cmd != "" {
						i.Console.Append("[MineDeck] 26.1 起游戏只在新建世界时读 server.properties 的难度，按设置执行 " + cmd)
						con := i.Console
						go func() { // 不在读输出的这条线上等写入
							if err := writeLine(pr, cmd); err != nil {
								con.Append("[MineDeck] 没能发出 " + cmd + "（" + err.Error() + "），难度可能还是游戏里原来的")
							}
						}()
					}
				}
			}
		}
		err := cmd.Wait()
		i.procMu.Lock()
		wasUser := i.userStop
		crashPolicy := i.Policies.CrashRestart
		switch {
		case i.killing:
			i.lastExit = "killed"
		case wasUser && err == nil:
			i.lastExit = "clean"
		case wasUser:
			i.lastExit = "killed" // 有人要停、退出码却不是 0：多半是被结束的
		default:
			i.lastExit = "crash"
		}
		i.state = "stopped"
		i.proc = nil
		i.procMu.Unlock()
		i.clearOnline()
		switch {
		case err != nil:
			i.Console.Append(fmt.Sprintf("[MineDeck] 服务器进程已退出（%v）", err))
		case wasUser:
			i.Console.Append("[MineDeck] 服务器已正常关闭")
		default: // 模组加载失败时 Java 也是退出码 0：没人要停服，不能说「正常关闭」（测试报告问题 3）
			i.Console.Append("[MineDeck] 服务器进程已退出（退出码 0，但没人让它停）")
		}
		if wasUser {
			events.Publish("instance.stop", map[string]any{"instance": i.Name})
		} else {
			events.Publish("instance.crash", map[string]any{"instance": i.Name})
		}
		if !wasUser && crashPolicy {
			go m.crashRestart(i, gen)
		}
	}()
	events.Publish("instance.start", map[string]any{"instance": i.Name})
	return nil
}

// startFailed 记下这次没启动起来的原因并原样返回 err。调用方拿着 procMu。
// toConsole：控制台里还没有写过原因的（端口被占、进程起不来）补一行；找不到 Java 的那一行带着路径，调用方已经写过了。
func (i *Instance) startFailed(err error, toConsole bool) error {
	i.startErr = err.Error()
	i.startBusy = nil
	var busy *portBusyError
	if errors.As(err, &busy) {
		i.startBusy = &PortBusy{Port: busy.port, Owner: busy.owner, Suggest: busy.suggest}
	}
	if toConsole {
		i.Console.Append("[MineDeck] 没能启动：" + i.startErr)
	}
	return err
}

// LastStartError 上一次启动没起来的原因（端口被占、找不到 Java、进程起不来）；之后成功起过进程、或者从没失败过，是空串。
// 只在内存里：MineDeck 重开以后就没有了。
func (i *Instance) LastStartError() string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.startErr
}

// LastStartPortBusy 上一次没启动起来是因为端口被占着时，是哪个端口、谁占着、能改成哪个（R08）；别的原因或者没失败过时为 nil。
func (i *Instance) LastStartPortBusy() *PortBusy {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.startBusy == nil {
		return nil
	}
	b := *i.startBusy
	return &b
}

// Stop 优雅停止：发送 stop 命令，超时 30 秒后强杀。
func (m *Manager) Stop(name string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	i.procMu.Lock()
	p := i.proc
	if p == nil || i.state == "stopped" {
		i.procMu.Unlock()
		return fmt.Errorf("服务器未在运行")
	}
	i.state = "stopping"
	i.userStop = true
	i.procMu.Unlock()

	i.Console.Append("[MineDeck] 正在保存世界并停止服务器...")
	_ = writeLine(p, "stop")
	go func() {
		deadline := time.After(30 * time.Second)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-deadline:
				// 仅当仍是本次要停止的那个进程时才强杀——否则可能误杀期间已重启的新进程
				i.procMu.Lock()
				if i.proc == p {
					i.Console.Append("[MineDeck] 停止超时，强制结束进程")
					i.killing = true
					_ = p.cmd.Process.Kill()
				}
				i.procMu.Unlock()
				return
			case <-tick.C:
				i.procMu.Lock()
				gone := i.proc != p // 本次进程已退出（i.proc 置空或已换成新一次运行）
				i.procMu.Unlock()
				if gone {
					return
				}
			}
		}
	}()
	return nil
}

// Kill 立即强制结束进程。
func (m *Manager) Kill(name string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.proc == nil {
		return fmt.Errorf("服务器未在运行")
	}
	i.userStop = true
	i.killing = true
	i.Console.Append("[MineDeck] 强制结束进程")
	return i.proc.cmd.Process.Kill()
}

// Command 向服务器控制台发送命令。命令原样写进服务器的标准输入（Go 的字符串，也就是 UTF-8，不按实例的控制台编码转码），
// 控制台里显示「> 命令」。
func (m *Manager) Command(name, cmd string) error { return m.CommandEcho(name, cmd, "") }

// CommandEcho 同 Command，控制台里显示 echo（空串时照常显示「> 命令」）。游戏内 AI 用它：tellraw 的文字写成了 \uXXXX，
// 原样显示没法看，换成一行「[MineDeck] 游戏内 AI 回复……」。
func (m *Manager) CommandEcho(name, cmd, echo string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	i.procMu.Lock()
	p := i.proc
	i.procMu.Unlock()
	if p == nil {
		return fmt.Errorf("服务器未在运行")
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if echo == "" {
		echo = "> " + cmd
	}
	if stopCommand(i.Core, cmd) {
		i.markUserStop(p) // 发之前就记下：这次退出是用户要停的，不是崩溃（测试报告：控制台 stop 后被崩溃自动重启拉起来）
	}
	i.Console.Append(echo)
	return writeLine(p, cmd)
}

// stoppingLineRe 服务器执行 stop 时打的那一行：控制台「…]: Stopping the server」，
// 游戏里的管理员或 RCON 发的是「…]: [Steve: Stopping the server]」。聊天「<Steve> Stopping the server」不算。
var stoppingLineRe = regexp.MustCompile(`\]: (?:\[[^\[\]<>]+: )?Stopping the server\]?\s*$`)

// stopCommand 控制台发的这条命令是不是停服：服务端是 stop；Velocity 是 shutdown（别名 end、stop），Waterfall 是 end。
func stopCommand(core mcsrc.Core, cmd string) bool {
	f := strings.Fields(strings.ToLower(strings.TrimPrefix(cmd, "/")))
	if len(f) == 0 {
		return false
	}
	switch core {
	case mcsrc.CoreVelocity:
		return f[0] == "shutdown" || f[0] == "end" || f[0] == "stop"
	case mcsrc.CoreWaterfall:
		return f[0] == "end"
	}
	return f[0] == "stop"
}

// markUserStop 用户自己要停这次运行（控制台或游戏里发了 stop）：退出时不算崩溃、不触发崩溃自动重启，
// 状态改成 stopping（伙伴、音乐共享据此知道是正常关服）。p 是发命令时的那次运行，已经换了一次运行就不管。
// 返回之前是不是已经记过（或者那次运行已经结束）。
func (i *Instance) markUserStop(p *proc) (already bool) {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.proc != p || p == nil {
		return true
	}
	already = i.userStop
	i.userStop = true
	if i.state == "starting" || i.state == "running" {
		i.state = "stopping"
	}
	return already
}

func writeLine(p *proc, line string) error {
	p.inMu.Lock()
	defer p.inMu.Unlock()
	if _, err := p.stdin.WriteString(line + "\n"); err != nil {
		return err
	}
	return p.stdin.Flush()
}

// difficultyCommand 开服完成时要不要按 server.properties 补一条 difficulty 命令。26.1 起难度存进了世界数据，
// 服务端只在新建世界时读 server.properties 的 difficulty，已有的世界开服时不再跟着它（2026-11-16 实测：Paper 26.3
// 改成 peaceful 重启后仍是 Hard；Fabric 1.21.11 改了重启就生效）。MineDeck 的设置页写的是 server.properties，
// 标着「重启生效」，所以 26.1 起开好以后补一条命令，和以前的版本一样以设置里的为准。更早的版本、没有这一项、
// 值认不出来（代理端没有 server.properties）时返回空串。极限模式（hardcore=true）也不发：游戏把它锁在困难，
// 命令改不动，却会回一句「已设为某某难度」，反而误导。
func difficultyCommand(mc string, props map[string]string) string {
	// 26.1 的快照、预发布、候选版（26.1-snapshot-6、26.1-pre-1、26.1-rc-1）按 26.1 算：难度存进世界是从快照里开始的，
	// 更早几个快照里多发一条也只是设成同一个难度（Codex 复审 beta.3 第二轮）
	base, _, _ := strings.Cut(mc, "-")
	if base == "" || versionLess(base, "26.1") {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(props["hardcore"]), "true") {
		return ""
	}
	v := strings.ToLower(strings.TrimSpace(props["difficulty"]))
	switch v {
	case "0":
		v = "peaceful"
	case "1":
		v = "easy"
	case "2":
		v = "normal"
	case "3":
		v = "hard"
	}
	switch v {
	case "peaceful", "easy", "normal", "hard":
		return "difficulty " + v
	}
	return ""
}

// serverReady 识别各核心的"启动完成"输出。
func serverReady(line string) bool {
	switch {
	case strings.Contains(line, "Done (") && strings.Contains(line, ")!"): // vanilla/paper/forge/velocity 等
		return true
	case strings.Contains(line, "Mohist 启动成功"), strings.Contains(line, "Mohist started"): // Mohist 中/英
		return true
	case strings.Contains(line, "Listening on /"): // BungeeCord/Waterfall
		return true
	}
	return false
}

// decodeConsole 按实例配置解码服务器输出（解决老 Forge 包 GBK 中文乱码）。
func decodeConsole(b []byte, enc string) string {
	switch enc {
	case "utf-8":
		return string(b)
	case "gbk":
		if d, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
			return string(d)
		}
	default: // auto：优先 UTF-8，非法字节序列则按 GBK 解
		if utf8.Valid(b) {
			return string(b)
		}
		if d, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
			return string(d)
		}
	}
	return string(b)
}

// ShutdownAll 程序退出前优雅停止所有运行中的服务器。
func (m *Manager) ShutdownAll(wait time.Duration) {
	m.closing.Store(true)
	var running []string
	for _, i := range m.List() {
		if i.Status() != "stopped" {
			running = append(running, i.Name)
		}
	}
	for _, n := range running {
		_ = m.Stop(n)
	}
	if len(running) == 0 {
		return
	}
	deadline := time.After(wait)
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			return
		case <-tick.C:
			all := true
			for _, n := range running {
				if i, err := m.Get(n); err == nil && i.Status() != "stopped" {
					all = false
				}
			}
			if all {
				return
			}
		}
	}
}

// OpenDir 在资源管理器中打开实例目录，或其白名单子目录（sub，做穿越防护）。
func (m *Manager) OpenDir(name, sub string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	target := i.Dir
	if sub = strings.TrimSpace(sub); sub != "" {
		clean := filepath.Clean(sub)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("只能打开服务器文件夹里面的文件夹")
		}
		target = filepath.Join(i.Dir, clean)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		return fmt.Errorf("这个文件夹还不存在（服务器可能还没生成它）")
	}
	return exec.Command("explorer.exe", filepath.Clean(target)).Start()
}
