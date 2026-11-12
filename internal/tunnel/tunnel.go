// Package tunnel 内网穿透：托管各服务商的 frpc 客户端进程，
// 把本地 MC 服务器端口映射到公网。隧道的创建/删除在服务商官网完成，
// 本包负责凭据保存、frpc 下载、一键启动、公网地址提取与状态管理。
// 凭据用 DPAPI 加密后存进 tunnels.json（"dpapi:…"），只在启动 frpc 时解开。
package tunnel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/events"
	"minedeck/internal/procutil"
	"minedeck/internal/secret"
)

// Tunnel 一条穿透隧道的配置。
type Tunnel struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"` // openfrp | natfrp | custom
	Name       string `json:"name"`
	Credential string `json:"credential"`           // openfrp 用户密钥 / natfrp 访问密钥 / custom auth token；存盘和内存里都是 DPAPI 密文
	ProxyID    string `json:"proxyId,omitempty"`    // openfrp 隧道ID / natfrp 隧道ID
	ServerAddr string `json:"serverAddr,omitempty"` // custom: frps 地址
	ServerPort int    `json:"serverPort,omitempty"` // custom: frps 端口
	RemotePort int    `json:"remotePort,omitempty"` // custom: 公网端口
	// LocalPort 本地端口，只有自定义 frps 用（写进 frpc 配置）；樱花、OpenFrp 的本地端口和远程端口都在服务商后台填，
	// 启动时只传隧道 ID，这几项存着反而误导（测试报告第五轮），保存和加载时清掉。
	LocalPort  int    `json:"localPort,omitempty"`
	Bound      string `json:"boundInstance,omitempty"`
	AutoStart  bool   `json:"autoStart"` // 跟随绑定实例启动
	PublicAddr string `json:"publicAddr,omitempty"`
	// Purpose 用途：game（游戏，默认；老数据没有这一项也按游戏算）或 voice（语音：Simple Voice Chat 的 UDP 端口，
	// 公网地址会自动写进绑定实例的 voice_host）。樱花、OpenFrp 的隧道类型在服务商后台定，MineDeck 看不出来，只能让用户标。
	Purpose string `json:"purpose,omitempty"`
	// Proto 自定义 frps 的协议：tcp（默认）或 udp（语音隧道用）。樱花、OpenFrp 的协议在服务商后台定，这一项不用。
	Proto string `json:"proto,omitempty"`
}

const (
	PurposeGame  = "game"
	PurposeVoice = "voice"
)

// IsVoice 是不是语音隧道。
func (t Tunnel) IsVoice() bool { return t.Purpose == PurposeVoice }

type runState struct {
	cmd     *exec.Cmd
	stdin   *bufio.Writer
	stopped bool // 用户主动停止
	// sawDomain 这次运行已经抓到过域名形式的公网地址：之后再出现的 IP 形式不覆盖它（只在读输出的那个 goroutine 里用）
	sawDomain bool
	// online 这次运行报过成功或拿到过公网地址（在 m.mu 下读写）。PublicAddr 跨次保存着，
	// 界面靠它分辨卡片上的地址是不是这次连上的：新一次运行还没连上就报错时，错误不能被上次的地址盖住（Codex 复审发现）
	online bool
}

type Manager struct {
	mu       sync.Mutex
	tunnels  []*Tunnel
	seq      int
	running  map[string]*runState
	starting map[string]bool // 正在启动中的隧道ID（登记进 running 前的占位，防并发/重复 Start 双启 frpc）
	consoles map[string]*procutil.Console
	lastErr  map[string]string // 隧道ID → 最近一次失败原因（启动前的失败、frpc 报的错、意外退出；运行态，不持久化）
	broken   map[string]bool   // 隧道ID → 凭据解不开（换了电脑或 Windows 用户），不能启动

	autoMu   sync.Mutex
	autoHook func(AutoAction) // 跟随实例自动启停之后的回调（操作层用它写操作记录）
}

// AutoAction 跟着实例自动启停隧道的一次操作，交给操作层写一条操作记录（来源 schedule），
// 事后在 logs\ops 里查得到「隧道是谁拉起来、谁停掉的」。和 inst.AutoAction 一个样子。
type AutoAction struct {
	Op    string         // tunnel.start | tunnel.stop
	Actor string         // "跟随实例"
	Args  map[string]any // id、隧道名 tunnel、实例名 inst
	Err   error
	Dur   time.Duration
}

// SetAutoHook 登记自动启停的回调（操作层在启动时注入）。
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

func NewManager() *Manager {
	m := &Manager{running: map[string]*runState{}, starting: map[string]bool{}, consoles: map[string]*procutil.Console{}, lastErr: map[string]string{}, broken: map[string]bool{}}
	m.load()
	scrubCustomConfigs() // 不管 tunnels.json 在不在、读不读得出来都清
	return m
}

func (m *Manager) file() string { return filepath.Join(app.Base, "tunnels.json") }

func (m *Manager) load() {
	b, err := os.ReadFile(m.file())
	if err != nil {
		return
	}
	var data struct {
		Seq     int       `json:"seq"`
		Tunnels []*Tunnel `json:"tunnels"`
	}
	if json.Unmarshal(b, &data) != nil {
		return
	}
	m.tunnels, m.seq = data.Tunnels, data.Seq
	// 旧版存的明文凭据：加密后写回（失败保持原样、只记日志，启动时照旧能用）；解不开的标记出来，不让启动
	migrated := false
	for _, t := range m.tunnels {
		// 老版本给樱花、OpenFrp 隧道存的 25565 / 25566 这类不参与启动的数：只在内存里清，不为它单独改写用户的文件，
		// 下次因为别的原因保存时自然就没了
		clearUnusedPorts(t)
		switch {
		case t.Credential == "":
		case !secret.IsSealed(t.Credential):
			if sealed, err := secret.Seal(t.Credential); err != nil {
				log.Printf("隧道「%s」的凭据加密失败，暂时保留明文: %v", t.Name, err)
			} else {
				t.Credential, migrated = sealed, true
			}
		default:
			if !secret.Readable(t.Credential) {
				m.broken[t.ID] = true
			}
		}
	}
	if migrated {
		m.save()
	}
}

// scrubCustomConfigs 删掉旧版留在 runtimes/frpc/custom 里、写着明文 token 的配置文件。
// 每次启动隧道都会重新生成配置；程序刚启动时上次的 frpc 已随作业对象一起结束，删了没有影响。
func scrubCustomConfigs() {
	dir := frpcDir("custom")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "tun") || !strings.HasSuffix(n, ".toml") {
			continue
		}
		p := filepath.Join(dir, n)
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "auth.token") && !strings.Contains(string(b), ".Envs.") {
			if err := os.Remove(p); err != nil {
				log.Printf("删除带明文 token 的旧 frpc 配置失败（%s）: %v", p, err)
			}
		}
	}
}

func (m *Manager) save() {
	data := struct {
		Seq     int       `json:"seq"`
		Tunnels []*Tunnel `json:"tunnels"`
	}{m.seq, m.tunnels}
	if b, err := json.MarshalIndent(data, "", "  "); err == nil {
		_ = os.WriteFile(m.file(), b, 0o600) // 凭据文件收紧权限
	}
}

// Status 隧道运行状态。
type Status struct {
	Tunnel
	Running    bool   `json:"running"`
	LastError  string `json:"lastError,omitempty"`
	CredBroken bool   `json:"credBroken,omitempty"` // 凭据解不开（换了电脑或 Windows 用户），要重填
	Online     bool   `json:"online,omitempty"`     // 运行中，而且这次运行已经连上（报过成功或拿到过公网地址）
}

func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		rs := m.running[t.ID]
		st := Status{Tunnel: *t, Running: rs != nil, Online: rs != nil && rs.online, LastError: m.lastErr[t.ID], CredBroken: m.broken[t.ID]}
		st.Credential = "" // 凭据绝不下发（前端无需回显）
		if st.Purpose == "" {
			st.Purpose = PurposeGame
		}
		out = append(out, st)
	}
	return out
}

// setLastErr 记录/清除隧道最近一次失败原因（隧道卡片上直接显示）。
func (m *Manager) setLastErr(id, msg string) {
	m.mu.Lock()
	if msg == "" {
		delete(m.lastErr, id)
	} else {
		m.lastErr[id] = clipLastErr(msg)
	}
	m.mu.Unlock()
}

// clipLastErr 失败原因按字符截到 300 字：樱花那条「首次使用需手动放置 frpc」带着目录，中文按字节截会切在半个字上，下发时变成乱码。
func clipLastErr(msg string) string {
	if r := []rune(msg); len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return msg
}

// lastError 隧道最近一次失败原因（没有时为空）。
func (m *Manager) lastError(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr[id]
}

func (m *Manager) get(id string) *Tunnel {
	for _, t := range m.tunnels {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Console 返回隧道的日志缓冲（惰性创建）。
func (m *Manager) Console(id string) *procutil.Console {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consoles[id] == nil {
		m.consoles[id] = procutil.NewConsole()
	}
	return m.consoles[id]
}

// Add 新增隧道配置。
func (m *Manager) Add(t Tunnel) (*Tunnel, error) {
	if err := validate(&t); err != nil {
		return nil, err
	}
	sealed, err := secret.Seal(t.Credential)
	if err != nil {
		return nil, fmt.Errorf("凭据：%w", err)
	}
	t.Credential = sealed
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	t.ID = "tun" + strconv.Itoa(m.seq)
	nt := &t
	m.tunnels = append(m.tunnels, nt)
	m.save()
	resp := *nt
	resp.Credential = "" // 响应不回显凭据
	return &resp, nil
}

// Update 更新隧道配置（运行中不可改）。
func (m *Manager) Update(id string, t Tunnel) error {
	if err := validate(&t); err != nil {
		return err
	}
	sealed, err := secret.Seal(t.Credential)
	if err != nil {
		return fmt.Errorf("凭据：%w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running[id] != nil {
		return fmt.Errorf("请先停止隧道再修改")
	}
	old := m.get(id)
	if old == nil {
		return fmt.Errorf("隧道不存在")
	}
	t.ID = id
	if sealed == "" {
		t.Credential = old.Credential // 凭据留空表示不修改（列表接口不回显凭据）
	} else {
		t.Credential = sealed
		delete(m.broken, id)
	}
	*old = t
	m.save()
	return nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running[id] != nil {
		return fmt.Errorf("请先停止隧道再删除")
	}
	for i, t := range m.tunnels {
		if t.ID == id {
			m.tunnels = append(m.tunnels[:i], m.tunnels[i+1:]...)
			delete(m.broken, id)
			m.save()
			return nil
		}
	}
	return fmt.Errorf("隧道不存在")
}

func validate(t *Tunnel) error {
	t.Name = strings.TrimSpace(t.Name)
	t.Credential = strings.TrimSpace(t.Credential)
	if t.Name == "" {
		return fmt.Errorf("请填写隧道名称")
	}
	switch t.Purpose {
	case "", PurposeGame:
		t.Purpose = PurposeGame
	case PurposeVoice:
	default:
		return fmt.Errorf("未知的用途：%s（可选 game 游戏、voice 语音）", t.Purpose)
	}
	if t.Provider != "custom" {
		t.Proto = ""
	} else {
		switch t.Proto {
		case "", "tcp", "udp":
			if t.Proto == "" {
				t.Proto = "tcp"
			}
		default:
			return fmt.Errorf("自定义 frps 的协议只能是 tcp 或 udp")
		}
	}
	switch t.Provider {
	case "openfrp", "natfrp":
		if t.Credential == "" || strings.TrimSpace(t.ProxyID) == "" {
			return fmt.Errorf("请填写凭据与隧道 ID（在服务商官网创建隧道后获取）")
		}
		clearUnusedPorts(t)
	case "custom":
		if t.ServerAddr == "" || t.ServerPort <= 0 || t.RemotePort <= 0 {
			return fmt.Errorf("自定义 frps 需填写服务器地址、端口与公网远程端口")
		}
		if t.LocalPort <= 0 {
			return fmt.Errorf("请填写本地端口（MC 服务器端口）")
		}
	default:
		return fmt.Errorf("未知服务商: %s", t.Provider)
	}
	return nil
}

// clearUnusedPorts 樱花、OpenFrp 的隧道清掉 frps 地址、端口这几项：启动时只传「凭据 + 隧道 ID」，端口在服务商后台填，
// 存着的数和后台不一致时会让人误判。返回有没有清掉东西。
func clearUnusedPorts(t *Tunnel) bool {
	if t.Provider != "openfrp" && t.Provider != "natfrp" || t.ServerAddr == "" && t.ServerPort == 0 && t.RemotePort == 0 && t.LocalPort == 0 {
		return false
	}
	t.ServerAddr, t.ServerPort, t.RemotePort, t.LocalPort = "", 0, 0, 0
	return true
}

// Start 启动隧道的 frpc 进程。
func (m *Manager) Start(id string) error { return m.start(id, "启动失败：") }

// start 启动隧道；frpc 起来之前就失败的（凭据解不开、没有 frpc、启动不了），原因前面加上 why 记进 lastErr，
// 卡片上直接显示（手动启动和跟随启动的说法不同）。frpc 起来了就清掉上一次的失败原因。
func (m *Manager) start(id, why string) error {
	m.mu.Lock()
	t := m.get(id)
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("隧道不存在")
	}
	if m.running[id] != nil || m.starting[id] {
		m.mu.Unlock()
		return fmt.Errorf("隧道已在运行")
	}
	m.starting[id] = true // 占位：慢速启动（EnsureFrpc 首次可能联网下载 frpc）期间挡住并发/重复 Start，避免双启 frpc 争同一公网端口
	snap := *t            // 启动参数用锁内拍下的副本，凭据在副本上解开，不写回
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.starting, id); m.mu.Unlock() }()

	con := m.Console(id)
	fail := func(err error) error {
		con.Append("[MineDeck] ❌ " + err.Error())
		m.setLastErr(id, why+err.Error())
		return err
	}
	// failFrpc 准备或启动 frpc 失败。认得出是被杀毒软件拦下或删掉的，换成中文说明（卡片上、日志里都是这句），
	// 系统给的原文跟在日志里。dir 是 frpc 所在的文件夹。
	failFrpc := func(err error, dir string) error {
		hint, ok := frpcBlockedHint(err, dir)
		if !ok {
			return fail(err)
		}
		e := fail(errors.New(hint))
		con.Append("[MineDeck] 系统报错原文：" + err.Error())
		return e
	}
	// 先解凭据再准备 frpc：解不开就不必下载 frpc
	cred, err := secret.Unseal(snap.Credential)
	if err != nil {
		return fail(fmt.Errorf("这条隧道的凭据读不出来（可能换了电脑或 Windows 用户）。请重新填写。也可以删掉这条隧道再加一次。"))
	}
	snap.Credential = cred
	p := providerOf(snap.Provider)
	if p == nil {
		return fail(fmt.Errorf("未知服务商: %s", snap.Provider))
	}
	exe, err := p.EnsureFrpc(con)
	if err != nil {
		return failFrpc(err, frpcDir(snap.Provider))
	}
	args, env, err := p.Launch(&snap)
	if err != nil {
		return fail(err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	// frpc 会读取 HTTP_PROXY 等环境变量并把与 frps 的连接送进 HTTP 代理
	//（且不遵守 NO_PROXY），穿透流量必须直连，故剥离代理变量。
	// env 是服务商要额外传给 frpc 的变量（自定义 frps 的 token 走这里，不落盘）。
	cmd.Env = append(dropProxyEnv(procutil.CleanEnv()), env...)
	procutil.HideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	cmd.Stderr = cmd.Stdout
	con.Append(fmt.Sprintf("[MineDeck] 启动 frpc（%s）...", snap.Provider))
	if err := startFrpc(cmd); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// EnsureFrpc 刚找到它，启动时却说文件不存在：转眼被删了
			err = fmt.Errorf("%w（%w）", errFrpcGone, err)
		}
		return failFrpc(fmt.Errorf("frpc 启动失败: %w", err), filepath.Dir(exe))
	}
	procutil.AssignToJob(cmd.Process)
	rs := &runState{cmd: cmd, stdin: bufio.NewWriter(stdin)}
	m.mu.Lock()
	m.running[id] = rs
	m.mu.Unlock()
	m.setLastErr(id, "") // 运行起来了：上一次的失败原因不再显示（frpc 再报错时 handleLine 会重新记）

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			con.Append(line)
			m.handleLine(t, rs, line)
		}
		_ = cmd.Wait()
		exitMsg := "frpc 意外退出"
		if ps := cmd.ProcessState; ps != nil {
			exitMsg += fmt.Sprintf("（退出码 %d）", ps.ExitCode())
		}
		gone := frpcGoneHint(exe) // 跑着被杀毒软件掐掉、隔离了，exe 也跟着没了：卡片上说这个，不让人去查凭据
		m.mu.Lock()
		userStopped := rs.stopped
		name := t.Name              // 在锁内取名，避免 delete 后与并发 Update 的 *old=t 竞争读 t.Name
		mine := m.running[id] == rs // 身份判定：只清自己这次运行的登记，避免旧 goroutine 误删并发新运行（对齐 inst.Stop 的 i.proc==p）
		if mine {
			delete(m.running, id)
			// 「意外退出」和撤登记在同一段锁里记：放到锁外，撤登记之后、记错之前新的一次运行可能已经起来，旧的错会记到它头上（Codex 复审发现）。
			// frpc 自己报过错的留着那条（更具体），没报就记「意外退出」，卡片上看得到
			if !userStopped && m.lastErr[id] == "" {
				if gone != "" {
					m.lastErr[id] = clipLastErr(gone)
				} else {
					m.lastErr[id] = exitMsg + "，检查凭据和隧道 ID，或者点「日志」看 frpc 的输出"
				}
			}
		}
		m.mu.Unlock()
		if userStopped {
			con.Append("[MineDeck] 隧道已停止")
		} else if mine {
			if gone != "" {
				con.Append("[MineDeck] " + exitMsg + "。" + gone)
			} else {
				con.Append("[MineDeck] frpc 意外退出（检查凭据/隧道 ID 是否正确，或查看上方日志）")
			}
			events.Publish("tunnel.down", map[string]any{"tunnel": name})
		}
	}()
	return nil
}

var addrRe = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z0-9.-]+:\d{2,5}|\d{1,3}(?:\.\d{1,3}){3}:\d{2,5})`)

// isIPAddr 「主机:端口」的主机是不是 IP（addrRe 的第一种写法也能匹配 IP，只能拆出来判断）。
func isIPAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && net.ParseIP(host) != nil
}

// handleLine 解析 frpc 输出：提取公网地址、自动应答服务条款确认。
func (m *Manager) handleLine(t *Tunnel, rs *runState, line string) {
	low := strings.ToLower(line)
	// 樱花 frpc 首次运行的服务条款确认
	if strings.Contains(line, "同意") && (strings.Contains(low, "y/n") || strings.Contains(line, "请输入")) {
		rs.stdin.WriteString("y\n")
		rs.stdin.Flush()
		return
	}
	success := strings.Contains(low, "start proxy success") ||
		strings.Contains(line, "启动成功") || strings.Contains(line, "映射启动成功") ||
		strings.Contains(line, "创建成功") || strings.Contains(low, "proxy added")
	hasAddr := addrRe.FindString(line)
	if success {
		m.mu.Lock()
		rs.online = true
		m.mu.Unlock()
	}
	if t.Provider == "custom" && success {
		m.setPublicAddr(t, fmt.Sprintf("%s:%d", t.ServerAddr, t.RemotePort))
		return
	}
	if hasAddr != "" && (success || strings.Contains(line, "连接") || strings.Contains(low, "remote")) {
		// 排除明显的本地/服务器内部地址
		if !strings.HasPrefix(hasAddr, "127.") && !strings.HasPrefix(hasAddr, "0.0.0.0") {
			// 域名优先：樱花同时打「使用 >>域名:端口<< 连接」和「或使用节点 IP 连接（不推荐）>>IP:端口<<」两行，
			// IP 会随节点变，发给好友的要是域名。这次运行见过域名之后，后面的 IP 不再覆盖；先 IP 后域名照常换成域名。
			// 只看这次运行（PublicAddr 跨次保存着，节点换了只给 IP 时不能卡在上次的域名上）。
			m.mu.Lock()
			rs.online = true
			m.mu.Unlock()
			switch {
			case !isIPAddr(hasAddr):
				rs.sawDomain = true
				m.setPublicAddr(t, hasAddr)
			case !rs.sawDomain:
				m.setPublicAddr(t, hasAddr)
			}
		}
	}
	// 失败信息捕获（供隧道行内红字提示）；成功则清除
	if success {
		m.setLastErr(t.ID, "")
	} else if strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(line, "失败") ||
		strings.Contains(low, "shutdown") || strings.Contains(low, "rejected") || strings.Contains(low, "invalid") ||
		strings.Contains(line, "错误") {
		m.setLastErr(t.ID, strings.TrimSpace(line))
	}
}

func (m *Manager) setPublicAddr(t *Tunnel, addr string) {
	m.mu.Lock()
	changed := t.PublicAddr != addr
	t.PublicAddr = addr
	if changed {
		m.save()
	}
	m.mu.Unlock()
	if changed {
		if t.IsVoice() {
			m.Console(t.ID).Append("[MineDeck] ✔ 语音公网地址: " + addr + "（MineDeck 会把它写进绑定实例的 voice_host）")
		} else {
			m.Console(t.ID).Append("[MineDeck] ✔ 公网地址: " + addr + "（把它发给朋友直连）")
		}
		events.Publish("tunnel.up", map[string]any{"tunnel": t.Name, "id": t.ID, "addr": addr})
	}
}

// Stop 停止隧道。
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	rs := m.running[id]
	if rs == nil {
		m.mu.Unlock()
		return fmt.Errorf("隧道未在运行")
	}
	rs.stopped = true
	m.mu.Unlock()
	_ = rs.cmd.Process.Kill() // frpc 无状态，直接结束
	return nil
}

// followActor 跟随实例自动启停时，操作记录里写的「谁做的」。
const followActor = "跟随实例"

// startDelay 实例启动后等多久再拉起跟随的隧道：等服务器进入启动流程。
var startDelay = 2 * time.Second

// startFrpc 启动 frpc 进程。测试里换掉它，模拟被杀毒软件拦下。
var startFrpc = func(cmd *exec.Cmd) error { return cmd.Start() }

// followArgs 跟随启停写进操作记录的参数：带上实例名 inst，按实例查操作记录时查得到。
func (m *Manager) followArgs(id, instance string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := map[string]any{"id": id, "inst": instance}
	if t := m.get(id); t != nil {
		args["tunnel"] = t.Name
	}
	return args
}

// StartBound 启动绑定到某实例且开启了跟随启动的隧道（实例启动后调用）。每拉起（或拉不起）一条记一条操作记录。
func (m *Manager) StartBound(instance string) {
	for _, id := range m.bound(instance) {
		id := id
		go func() {
			time.Sleep(startDelay)
			m.mu.Lock()
			busy := m.running[id] != nil || m.starting[id] // 已经开着（比如重启时没停）就不用再开；等之后再看，免得撞上刚停掉还没收尾的那次
			m.mu.Unlock()
			if busy {
				return
			}
			start := time.Now()
			err := m.start(id, "跟随启动失败：")
			if err != nil {
				m.Console(id).Append("[MineDeck] 跟随启动失败: " + err.Error())
			}
			m.noteAuto(AutoAction{Op: "tunnel.start", Actor: followActor, Args: m.followArgs(id, instance), Err: err, Dur: time.Since(start)})
		}()
	}
}

// StopBound 停掉绑定到某实例、开了跟随启动、正在运行的隧道（实例停了之后调用），返回停了几条。
// 和 StartBound 对称：手动开的、没勾跟随启动的隧道不动。停的每一条记一条操作记录。
func (m *Manager) StopBound(instance string) int {
	n := 0
	for _, id := range m.bound(instance) {
		m.mu.Lock()
		running := m.running[id] != nil
		m.mu.Unlock()
		if !running {
			continue
		}
		m.Console(id).Append(fmt.Sprintf("[MineDeck] 服务器「%s」停了，隧道跟着停止", instance))
		start := time.Now()
		if m.Stop(id) != nil { // Stop 只会报「未在运行」：刚好自己退出了，什么也没做，不记
			continue
		}
		n++
		m.noteAuto(AutoAction{Op: "tunnel.stop", Actor: followActor, Args: m.followArgs(id, instance), Dur: time.Since(start)})
	}
	return n
}

// bound 绑定到某实例、开了跟随启动的隧道。
func (m *Manager) bound(instance string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, t := range m.tunnels {
		if t.Bound == instance && t.AutoStart {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// stopGrace 实例停了之后等多久再停跟随的隧道：重启（先停再开）时不来回开关隧道。
var stopGrace = 5 * time.Second

// FollowInstances 让开了「跟随实例启动」的隧道跟着实例启停（订阅实例事件；main 里调一次）：实例启动时拉起，
// 包括崩溃自动重启那次；实例停了（用户停服、强杀、崩溃）stopGrace 之后还是停着，就停掉——停服后公网端口不再开着
// （测试报告 C-03）。status 查实例现在的状态（stopped | starting | running | stopping）。
func (m *Manager) FollowInstances(status func(instance string) string) {
	events.Subscribe(func(e events.Event) { m.follow(e, status) })
}

func (m *Manager) follow(e events.Event, status func(string) string) {
	name, _ := e.Data["instance"].(string)
	if name == "" {
		return
	}
	switch e.Type {
	case "instance.start":
		m.StartBound(name)
	case "instance.stop", "instance.crash":
		go func() {
			time.Sleep(stopGrace)
			if status(name) == "stopped" {
				m.StopBound(name)
			}
		}()
	}
}

func dropProxyEnv(env []string) []string {
	drop := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	}
	out := env[:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}

// StopAll 程序退出前停止全部 frpc。
func (m *Manager) StopAll() {
	m.mu.Lock()
	var ids []string
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}
