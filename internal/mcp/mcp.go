// Package mcp MCP 接口（设计文档 §15）：让同一台电脑上的其他 AI 工具（Claude Desktop、Cherry Studio、Cursor、VS Code 等）
// 经 MCP（Model Context Protocol）调用 MineDeck 的操作层。
//
// 两种连法：
//   - Streamable HTTP：MineDeck 本地 HTTP 服务上的 /mcp（ServeHTTP）。只收本机的连接，校验 Origin，
//     要带 Authorization: Bearer <MCP 令牌>。令牌在第一次打开时生成，DPAPI 加密存进 secrets.json，和每次启动的网页令牌无关。
//   - stdio：MineDeck.exe -mcp 是 stdio ↔ HTTP 的桥（Bridge），由 MCP 客户端当子进程启动，把消息转给同一目录下正在运行的
//     MineDeck。运行着的 MineDeck 把端口写进 <程序目录>/data/mcp/runtime.json；令牌由桥自己从 secrets.json 解出来
//     （同一个 Windows 用户才解得开）。
//
// 协议按 2026-07-28 实现（无状态，每个请求的 _meta 带协议版本和客户端能力，有 server/discover），同时认 initialize 握手的
// 旧版本 2025-11-25、2025-06-18、2025-03-26、2024-11-05。只做工具：读操作直接执行；写操作和危险操作和应用内助手一样只生成提案，
// 要人在 MineDeck 的界面上确认（危险操作多勾一次，EULA 只能人勾）；仅人工的操作不给。工具结果和助手一样先脱敏、截断再交出去。
//
// 默认关闭。不引用 Windows API（桥检查进程的那一个函数分平台写），Linux 上能直接测试。
package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"minedeck/internal/assistant"
	"minedeck/internal/ops"
)

// 协议版本。
const (
	// Modern 2026-07-28：无状态，每个请求在 _meta 里带协议版本和客户端的能力，没有 initialize 握手。
	Modern = "2026-07-28"
	// latestLegacy 旧版客户端在 initialize 里要的版本不认识时回这个（握手里能谈的最新版本）。
	latestLegacy = "2025-11-25"
	// defaultLegacy 旧版的请求既没带 MCP-Protocol-Version 头、也认不出会话时，按规范当作 2025-03-26。
	defaultLegacy = "2025-03-26"
)

// Versions 支持的协议版本，新的在前。2026-07-28 之外都是 initialize 握手的旧版本。
var Versions = []string{Modern, "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

func legacyVersion(v string) bool { return v != Modern && contains(Versions, v) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// KeyName MCP 令牌在 secrets.json 里的名字。
const KeyName = "mcp.token"

// KeyStore 令牌的存放处（Windows 上是 secret.Store：DPAPI 加密存进 secrets.json）。
type KeyStore interface {
	Get(name string) (string, error)
	Set(name, plain string) error
	State(name string) (set, unreadable bool)
}

// DataDir MCP 接口的数据目录：<程序目录>/data/mcp（settings.json、runtime.json）。运行着的 MineDeck 和桥都按它找文件。
func DataDir(base string) string { return filepath.Join(base, "data", "mcp") }

// Options Service 的依赖。
type Options struct {
	Ops     *ops.Registry       // 和网页接口、应用内助手共用的注册表（提案存在里面）
	Dir     string              // 数据目录，见 DataDir
	Keys    KeyStore            // 令牌的存放处
	Redact  *assistant.Redactor // 交给外部 AI 之前的脱敏（和应用内助手共用）
	Version string              // MineDeck 的版本号（serverInfo）
	Exe     string              // MineDeck.exe 的完整路径（stdio 方式的配置用）
	Now     func() time.Time
}

// Service MCP 接口。
type Service struct {
	reg     *ops.Registry
	dir     string
	keys    KeyStore
	redact  *assistant.Redactor
	version string
	exe     string
	now     func() time.Time

	mu       sync.Mutex
	enabled  bool
	port     int    // 实际监听的端口（Start 之后才有）
	wanted   int    // 想用的端口（被占用时 MineDeck 会换一个）
	sum      []byte // 令牌的 SHA-256：内存里不留明文；nil 表示没有可用的令牌
	broken   bool   // 存着的令牌解不开（换了电脑或 Windows 用户）
	clients  []*Client
	seq      uint64 // 递增的序号：客户端来访（Client.seq）和提出提案（record.seq）共用
	sessions map[string]*session
	props    map[string]*record // MCP 提出的提案（处理结果留一个小时，给 proposal_status 查）
	changed  chan struct{}      // 提案有变化时关闭、再换一个新的（proposal_status 的 wait 等它）
	subs     map[chan struct{}]struct{}
}

type settings struct {
	Enabled bool `json:"enabled"`
}

// New 读设置和令牌，登记提案处理完的回调。不监听、不联网；端口要等 Start。
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Redact == nil {
		o.Redact = &assistant.Redactor{}
	}
	s := &Service{reg: o.Ops, dir: o.Dir, keys: o.Keys, redact: o.Redact, version: o.Version, exe: o.Exe, now: o.Now,
		sessions: map[string]*session{}, props: map[string]*record{}, changed: make(chan struct{}), subs: map[chan struct{}]struct{}{}}
	if b, err := os.ReadFile(s.settingsPath()); err == nil {
		var st settings
		if json.Unmarshal(b, &st) == nil {
			s.enabled = st.Enabled
		}
	}
	s.mu.Lock()
	s.loadTokenLocked()
	if s.enabled && s.sum == nil && !s.broken { // 开着却没有令牌（比如有人删了 secrets.json 里的这一项）：补一个
		if err := s.newTokenLocked(); err != nil {
			log.Println("生成 MCP 令牌失败:", err)
		}
	}
	s.mu.Unlock()
	s.reg.OnResolve(s.onResolve)
	return s
}

func (s *Service) settingsPath() string { return filepath.Join(s.dir, "settings.json") }

func (s *Service) saveLocked() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(settings{Enabled: s.enabled}, "", "  ")
	return writeAtomic(s.settingsPath(), b)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ---------- 令牌 ----------

func (s *Service) loadTokenLocked() {
	s.sum, s.broken = nil, false
	if s.keys == nil {
		return
	}
	tok, err := s.keys.Get(KeyName)
	switch {
	case err != nil:
		s.broken = true
	case tok != "":
		sum := sha256.Sum256([]byte(tok))
		s.sum = sum[:]
	}
}

// newTokenLocked 生成新令牌（32 字节随机数的十六进制），加密存好，内存里只留它的 SHA-256。
func (s *Service) newTokenLocked() error {
	if s.keys == nil {
		return errors.New("没有存放令牌的地方")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	tok := hex.EncodeToString(b)
	if err := s.keys.Set(KeyName, tok); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(tok))
	s.sum, s.broken = sum[:], false
	return nil
}

type authResult int

const (
	authOK authResult = iota
	authMissing
	authWrong
	authBroken
)

// auth 核对 Authorization: Bearer <令牌>（比较 SHA-256，用定长比较）。
func (s *Service) auth(header string) authResult {
	s.mu.Lock()
	sum, broken := s.sum, s.broken
	s.mu.Unlock()
	if broken || sum == nil {
		return authBroken
	}
	tok, ok := bearer(header)
	if !ok {
		return authMissing
	}
	got := sha256.Sum256([]byte(tok))
	if subtle.ConstantTimeCompare(got[:], sum) != 1 {
		return authWrong
	}
	return authOK
}

func bearer(h string) (string, bool) {
	h = strings.TrimSpace(h)
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return "", false
	}
	tok := strings.TrimSpace(h[7:])
	return tok, tok != ""
}

// Token 令牌的明文（只给本机的界面复制配置用）。没设置返回空串，解不开返回错误。
func (s *Service) Token() (string, error) {
	if s.keys == nil {
		return "", errors.New("没有存放令牌的地方")
	}
	return s.keys.Get(KeyName)
}

// ResetToken 换一个新令牌：HTTP 方式连的客户端要换成新令牌，stdio 方式（桥每次自己读）不用改。
func (s *Service) ResetToken() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.newTokenLocked(); err != nil {
		return fmt.Errorf("重置 MCP 令牌失败：%w", err)
	}
	return nil
}

// ---------- 开关、端口、运行时文件 ----------

// Enabled MCP 接口是否打开。
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SetEnabled 打开或关闭 MCP 接口。第一次打开（或令牌解不开）时生成令牌；打开时写运行时文件，关闭时删掉它，
// 还没处理的 MCP 提案作废（经操作层拒绝，记一条操作记录）。
func (s *Service) SetEnabled(on bool) error {
	s.mu.Lock()
	if on && (s.sum == nil || s.broken) {
		if err := s.newTokenLocked(); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("生成 MCP 令牌失败：%w", err)
		}
	}
	prev := s.enabled
	s.enabled = on
	if err := s.saveLocked(); err != nil {
		s.enabled = prev
		s.mu.Unlock()
		return fmt.Errorf("保存 MCP 设置失败：%w", err)
	}
	s.mu.Unlock()
	if on {
		s.writeRuntime()
	} else {
		s.removeRuntime()
		s.voidPending("用户关闭了 MineDeck 的 MCP 接口")
	}
	s.publish()
	return nil
}

// Runtime 运行时文件 runtime.json：运行着的 MineDeck 写，桥读（打开了 MCP 接口时才有）。
type Runtime struct {
	V       int    `json:"v"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Exe     string `json:"exe,omitempty"`
	Version string `json:"version,omitempty"`
	Started string `json:"started"`
}

// RuntimeFile 运行时文件的名字（在 DataDir 里）。
const RuntimeFile = "runtime.json"

func (s *Service) runtimePath() string { return filepath.Join(s.dir, RuntimeFile) }

// Start HTTP 服务开始监听之后调：记下端口（wanted 是想用的端口，被占用时 MineDeck 会换一个），打开着就写运行时文件。
func (s *Service) Start(port, wanted int) {
	s.mu.Lock()
	s.port, s.wanted = port, wanted
	on := s.enabled
	s.mu.Unlock()
	if on {
		s.writeRuntime()
	}
}

// Stop 退出时调：删掉自己写的运行时文件。
func (s *Service) Stop() { s.removeRuntime() }

func (s *Service) writeRuntime() {
	s.mu.Lock()
	port := s.port
	s.mu.Unlock()
	if port == 0 {
		return
	}
	b, _ := json.MarshalIndent(Runtime{V: 1, PID: os.Getpid(), Port: port, Exe: s.exe, Version: s.version, Started: s.now().Format(time.RFC3339)}, "", "  ")
	err := os.MkdirAll(s.dir, 0o755)
	if err == nil {
		err = writeAtomic(s.runtimePath(), b)
	}
	if err != nil {
		log.Println("写 MCP 运行时文件失败:", err)
	}
}

// removeRuntime 只删自己写的（同一目录下开了两份 MineDeck 时，不删别人的）。
func (s *Service) removeRuntime() {
	b, err := os.ReadFile(s.runtimePath())
	if err != nil {
		return
	}
	var rt Runtime
	if json.Unmarshal(b, &rt) == nil && rt.PID != os.Getpid() {
		return
	}
	_ = os.Remove(s.runtimePath())
}

// URL HTTP 方式的接口地址。
func (s *Service) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urlLocked()
}

func (s *Service) urlLocked() string {
	port := s.port
	if port == 0 {
		port = s.wanted
	}
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
}

// ---------- 给界面看的状态 ----------

// View 「设置 → AI → MCP 接口」显示的内容（GET /api/mcp）。不含令牌。
type View struct {
	Enabled     bool     `json:"enabled"`
	Local       bool     `json:"local"` // 请求来自本机（局域网的设备只能看）
	URL         string   `json:"url"`
	Port        int      `json:"port"`
	PortChanged bool     `json:"portChanged,omitempty"` // 想用的端口被占用，这次换了一个：HTTP 方式的配置要跟着改
	TokenSet    bool     `json:"tokenSet"`
	TokenBroken bool     `json:"tokenBroken,omitempty"` // 令牌解不开（换了电脑或 Windows 用户），要重置
	Clients     []Client `json:"clients"`               // 本次启动以来连过的客户端，最近的在前
	Pending     int      `json:"pending"`               // 还在等确认的 MCP 提案
	Tools       int      `json:"tools"`                 // 给外部 AI 的工具数（含 proposal_status）
	Versions    []string `json:"versions"`              // 支持的协议版本
}

// Status 见 View。
func (s *Service) Status(local bool) View {
	s.mu.Lock()
	v := View{Enabled: s.enabled, Local: local, URL: s.urlLocked(), Port: s.port, PortChanged: s.wanted != 0 && s.port != 0 && s.port != s.wanted,
		TokenSet: s.sum != nil, TokenBroken: s.broken, Clients: []Client{}, Versions: append([]string(nil), Versions...)}
	for _, c := range s.clients {
		v.Clients = append(v.Clients, *c)
	}
	s.mu.Unlock()
	newestFirst(v.Clients)
	v.Pending = len(s.Pending())
	v.Tools = len(s.toolDefs(Modern))
	return v
}

// ---------- 连过的客户端、旧版的会话 ----------

// Client 连过的一个客户端（客户端自己报的名字和版本，不能当真，只用来显示和记操作记录）。
type Client struct {
	Name      string    `json:"name"`
	Title     string    `json:"title,omitempty"`
	Version   string    `json:"version,omitempty"`
	Transport string    `json:"transport"` // http / stdio
	Protocol  string    `json:"protocol"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
	Calls     int       `json:"calls"` // 调用工具的次数
	seq       uint64    // 第几个来过的：按它排先后。Windows 上 Go 的时钟一跳 0.5～15.6 毫秒，前后脚来的两个 Last 常常一样
}

// newestFirst 最近来过的在前。
func newestFirst(cs []Client) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].seq > cs[j].seq })
}

const maxClients = 8

// identity 客户端报的身份（initialize 的 clientInfo，或者 2026-07-28 每个请求的 _meta）。
type identity struct {
	Name, Title, Version string
}

func (id identity) known() bool { return id.Name != "" }

// actor 操作记录里的 actor：客户端报的名字（清理过、限长）；不知道就写「MCP 客户端」。
func (id identity) actor() string {
	if id.Name == "" {
		return "MCP 客户端"
	}
	return id.Name
}

// parseIdentity 从 clientInfo（Implementation）里取名字、标题和版本，去掉控制字符、限长。
func parseIdentity(raw json.RawMessage) identity {
	var v struct {
		Name    any `json:"name"`
		Title   any `json:"title"`
		Version any `json:"version"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return identity{}
	}
	str := func(x any) string { s, _ := x.(string); return s }
	return identity{Name: cleanName(str(v.Name), 40), Title: cleanName(str(v.Title), 40), Version: cleanName(str(v.Version), 24)}
}

// cleanName 去掉控制字符和格式字符（换行、零宽字符这些），连续的空白合成一个空格，最长 max 个字。
func cleanName(s string, max int) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		if unicode.IsSpace(r) {
			if space {
				continue
			}
			space = true
			r = ' '
		} else {
			space = false
		}
		if n == max {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// seen 记下一个客户端来过（tool 为 true 时算一次工具调用）。
func (s *Service) seen(id identity, transport, protocol string, tool bool) {
	if !id.known() {
		return
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var c *Client
	for _, x := range s.clients {
		if x.Name == id.Name && x.Transport == transport {
			c = x
			break
		}
	}
	if c == nil {
		c = &Client{Name: id.Name, Transport: transport, First: now, Last: now}
		s.clients = append(s.clients, c)
	}
	s.seq++
	c.Title, c.Version, c.Protocol, c.Last, c.seq = id.Title, id.Version, protocol, now, s.seq
	if len(s.clients) > maxClients { // 挤掉最久没来过的（新来的这个序号最大，不会被挤掉）
		sort.SliceStable(s.clients, func(i, j int) bool { return s.clients[i].seq > s.clients[j].seq })
		s.clients = s.clients[:maxClients]
	}
	if tool {
		c.Calls++
	}
}

// session 旧版（initialize 握手）的 HTTP 会话：只用来记住客户端报的名字和谈好的版本。认不出的会话照常处理，
// 当作不知道名字（MineDeck 重启之后，客户端带着旧的会话编号来，不让它出错）。
type session struct {
	id        identity
	protocol  string
	transport string
	last      time.Time
}

const (
	maxSessions = 64
	sessionIdle = 24 * time.Hour
)

func (s *Service) newSession(id identity, protocol, transport string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	sid := hex.EncodeToString(b)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.sessions {
		if now.Sub(x.last) > sessionIdle {
			delete(s.sessions, k)
		}
	}
	for len(s.sessions) >= maxSessions { // 挤掉最久没用的
		oldest := ""
		for k, x := range s.sessions {
			if oldest == "" || x.last.Before(s.sessions[oldest].last) {
				oldest = k
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[sid] = &session{id: id, protocol: protocol, transport: transport, last: now}
	return sid
}

func (s *Service) session(sid string) *session {
	if sid == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[sid]
	if x != nil {
		x.last = s.now()
		c := *x
		return &c
	}
	return nil
}
