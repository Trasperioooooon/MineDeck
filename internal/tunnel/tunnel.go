// Package tunnel 内网穿透：托管各服务商的 frpc 客户端进程，
// 把本地 MC 服务器端口映射到公网。隧道的创建/删除在服务商官网完成，
// 本包负责凭据保存、frpc 下载、一键启动、公网地址提取与状态管理。
// 凭据用 DPAPI 加密后存进 tunnels.json（"dpapi:…"），只在启动 frpc 时解开。
package tunnel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
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
	// guardPort MineDeck 自己的网页端口（0 表示不查）：自定义 frps 的隧道不许指向它，见 SetGuardPort
	guardPort int
	// loadErr tunnels.json 在、却没读成（见 LoadError）。不为空时列表是空的，一律不许保存；
	// 有人要列表、要保存、要启动时再读一次（retryLoad），读成了就清掉
	loadErr *LoadError
	// renum 读进来时给重复编号换了号、还没写进 tunnels.json 的那次换号（A58）。不为空时 save 先在旁边留一份
	// 读进来时的那份的备份再写，写成了才清掉：带重复编号的那份被换掉之前，旁边一定有它的备份
	renum *pendingRenumber

	autoMu   sync.Mutex
	autoHook func(AutoAction)            // 跟随实例自动启停之后的回调（操作层用它写操作记录）
	instSay  func(instance, line string) // 往服务器的控制台里写一句（操作层注入：隧道层够不着服务器）
	pending  []AutoAction                // 排着队、还没交给 autoHook 的操作记录（见 noteSoon）
}

// AutoAction 隧道层自己做的一次操作，交给操作层写一条操作记录（来源 schedule）：跟着实例自动启停隧道，
// 事后在 logs\ops 里查得到「隧道是谁拉起来、谁停掉的」；读 tunnels.json 时给重复的编号换号。和 inst.AutoAction 一个样子。
type AutoAction struct {
	Op    string         // tunnel.start | tunnel.stop | tunnel.renumber
	Actor string         // followActor | renumberActor
	Args  map[string]any // 启停：id、隧道名 tunnel、实例名 inst；换号：renamed、backup、backupSealed
	Err   error
	Dur   time.Duration
}

// SetAutoHook 登记自动启停的回调（操作层在启动时注入）。程序启动时读 tunnels.json 换过编号的话，那时回调还没登记，
// 那条记录在这里补交。
func (m *Manager) SetAutoHook(fn func(AutoAction)) {
	m.autoMu.Lock()
	m.autoHook = fn
	m.autoMu.Unlock()
	m.flushNotes()
}

func (m *Manager) noteAuto(a AutoAction) {
	m.autoMu.Lock()
	fn := m.autoHook
	m.autoMu.Unlock()
	if fn != nil {
		fn(a)
	}
}

// noteSoon 同 noteAuto，但不在调用方手里调回调：读 tunnels.json（load）、保存（save）时可能持着 m.mu，
// 程序启动时（NewManager）操作层又还没登记回调。先排队，回调在就另起一个 goroutine 交出去，不在就等 SetAutoHook 时补交。
func (m *Manager) noteSoon(a AutoAction) {
	m.autoMu.Lock()
	m.pending = append(m.pending, a)
	ready := m.autoHook != nil
	m.autoMu.Unlock()
	if ready {
		go m.flushNotes()
	}
}

// flushNotes 把排着队的操作记录交给回调；回调还没登记时留着不动。
func (m *Manager) flushNotes() {
	m.autoMu.Lock()
	fn, notes := m.autoHook, m.pending
	if fn != nil {
		m.pending = nil
	}
	m.autoMu.Unlock()
	if fn == nil {
		return
	}
	for _, a := range notes {
		fn(a)
	}
}

// SetInstanceConsole 登记往服务器控制台写一句的办法（操作层在启动时注入）。
func (m *Manager) SetInstanceConsole(fn func(instance, line string)) {
	m.autoMu.Lock()
	m.instSay = fn
	m.autoMu.Unlock()
}

func (m *Manager) sayToInstance(instance, line string) {
	m.autoMu.Lock()
	fn := m.instSay
	m.autoMu.Unlock()
	if fn != nil {
		fn(instance, line)
	}
}

// SetGuardPort 告诉隧道层 MineDeck 自己的网页端口：新增、修改、启动（包括跟随实例启动）时，拒绝本地端口是它的自定义
// frps 隧道。经 frpc 转发进来的请求在 MineDeck 看来都来自 127.0.0.1，网页端口映射到公网，就等于把管理界面摆到了公网上。
// 樱花、OpenFrp 的本地端口在服务商后台填，MineDeck 看不到，这里查不了；「仅本机」靠的是本机令牌（web.callerOf），不靠这一条。
func (m *Manager) SetGuardPort(port int) {
	m.mu.Lock()
	m.guardPort = port
	m.mu.Unlock()
}

// checkGuardPort 隧道是不是指向 MineDeck 自己的网页端口 guard。只有自定义 frps 的本地端口由 MineDeck 写进 frpc 配置。
func checkGuardPort(t *Tunnel, guard int) error {
	if guard > 0 && t.Provider == "custom" && t.LocalPort == guard {
		return fmt.Errorf("本地端口 %d 是 MineDeck 自己的网页端口，不能映射到公网：要填的是 Minecraft 服务器的端口", guard)
	}
	return nil
}

// 启动时 tunnels.json 读不出来（多半是杀毒、同步盘这类程序刚好占着）隔多久再读、再读几次。做成变量是为了测试能调小。
// 坏文件再读也是坏的，不等。这几次都没读成也不要紧：之后有人要列表、要保存时还会再读（retryLoad）。
var (
	loadRetryWait = 200 * time.Millisecond
	loadRetries   = 3
)

func NewManager() *Manager {
	m := &Manager{running: map[string]*runState{}, starting: map[string]bool{}, consoles: map[string]*procutil.Console{}, lastErr: map[string]string{}, broken: map[string]bool{}}
	m.load()
	for i := 0; i < loadRetries && m.loadErr != nil && !m.loadErr.Corrupt; i++ {
		time.Sleep(loadRetryWait)
		m.load()
	}
	if m.loadErr != nil {
		log.Printf("隧道配置没读出来，读成之前不许保存（免得盖掉原文件）: %v", m.loadErr)
	}
	scrubCustomConfigs() // 不管 tunnels.json 在不在、读不读得出来都清
	return m
}

func (m *Manager) file() string { return filepath.Join(app.Base, "tunnels.json") }

// LoadError tunnels.json 在，却没读成：读不出来（被别的程序占着、没有读取权限），或者读出来了解析不了（文件坏了）。
//
// 以前这两种都当成空列表悄悄往下走，之后任何一次保存（新增一条隧道、记公网地址）都把原文件整份换掉，
// 里面的隧道和凭据就全没了。现在这期间列表是空的，新增、修改、删除、导入、创建、启动一律拒绝，原文件一个字不动；
// 读成了（占着的程序放开了，或者用户修好、挪走了文件）就恢复正常，不用重启。文件不在（新装）不算，那是真的没有隧道。
type LoadError struct {
	Corrupt bool  // true：读出来了但解析不了；false：读不出来
	Err     error // 系统或 JSON 解析给的原文
}

func (e *LoadError) Error() string {
	if e.Corrupt {
		return "隧道配置文件 tunnels.json 的内容坏了，读不出里面的隧道。为了不盖掉原来的隧道和凭据，修好之前不能添加、修改、删除或启动隧道：" + e.Err.Error()
	}
	return "隧道配置文件 tunnels.json 读不出来（可能被别的程序占着，或者没有读取权限）。为了不盖掉原来的隧道和凭据，读出来之前不能添加、修改、删除或启动隧道：" + e.Err.Error()
}

func (e *LoadError) Unwrap() error { return e.Err }

// Hint 界面报错正文接的那句话，规矩同 SaveError.Hint（60 字以内，不带路径和英文句子）。两种情况下一步不一样，分开说。
func (e *LoadError) Hint() string {
	if e.Corrupt {
		return "MineDeck 文件夹里的 tunnels.json 内容坏了。用记事本修好它，或者改名挪走后从空列表开始"
	}
	return "MineDeck 文件夹里的 tunnels.json 读不出来，可能被别的程序占着。关掉占用它的程序后会自动重读"
}

// load 读 tunnels.json，分三种情况：
//   - 文件不在：新装，空列表是对的，照常保存。要程序文件夹本身看得到才算（见 reallyMissing）；
//   - 文件在但读不出来（被占着、没权限）：记进 m.loadErr；
//   - 读出来了但解析不了，或者没有 tunnels 这一项：记进 m.loadErr（Corrupt）。
//     只有空白的文件、{} 和 null 不算：里面没有能丢的东西，按空列表处理。
//
// m.loadErr 不为空时 m.tunnels、m.seq 不动（还是空的）。调用方持有 m.mu；NewManager 里别人还拿不到 m，不用锁。
// 只有 NewManager 和 retryLoad（m.loadErr 不为空、列表还是空的）调它，读成一次以后不会再调：
// 调的时候一条隧道也没在跑，换编号（renumberDuplicates）不会让哪个运行中的 frpc 对不上号。
func (m *Manager) load() {
	b, err := os.ReadFile(m.file())
	if errors.Is(err, fs.ErrNotExist) {
		if gone := m.reallyMissing(err); gone != nil {
			m.loadErr = &LoadError{Err: gone}
			return
		}
		m.loadErr = nil
		return
	}
	if err != nil {
		m.loadErr = &LoadError{Err: err}
		return
	}
	raw := b // 原样的字节：要给重复编号换号时，一般备份的就是它（见 newPendingRenumber）
	// 记事本存的 UTF-8 可能带 BOM：提示里让人用记事本修，修完不能因为这个又读不出来
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(b)) == 0 {
		// 老版本直接覆盖写，写到一半断电会留下空文件。里面本来就没有东西可丢，挡着不让保存只会让人多跑一趟
		log.Printf("tunnels.json 是空的，按没有隧道处理")
		m.loadErr = nil
		return
	}
	var data struct {
		Seq     int       `json:"seq"`
		Tunnels []*Tunnel `json:"tunnels"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		m.loadErr = &LoadError{Corrupt: true, Err: jsonErrAt(b, err)}
		return
	}
	if lacksTunnelsKey(b) {
		m.loadErr = &LoadError{Corrupt: true, Err: errors.New(`文件里找不到 "tunnels" 这一项（键名是不是改错了）`)}
		return
	}
	m.loadErr = nil
	// 列表里写着 null 的项（手改出来的）跳过：里面没有内容，留着的话下面一读字段就崩
	m.tunnels, m.seq = slices.DeleteFunc(data.Tunnels, func(t *Tunnel) bool { return t == nil }), data.Seq
	// 手改 tunnels.json 把 seq 调小时，把 seq 抬到现有 tun<数字> 编号的最大值，避免新增隧道撞号；
	// 只改内存里的值，不为它单独写回文件，下次保存时自然会写进去。
	for _, t := range m.tunnels {
		s, ok := strings.CutPrefix(t.ID, "tun")
		if !ok || s == "" || strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
			continue
		}
		if n, err := strconv.Atoi(s); err == nil && n > m.seq {
			m.seq = n
		}
	}
	// 换号要在标记凭据解不开之前：m.broken 按编号记，重复的编号会把两条隧道的标记混在一起
	seqRead := m.seq // 换号之前的 seq：有明文凭据要加密时，备份里写的是它（见 newPendingRenumber）
	renamed := m.renumberDuplicates()
	m.broken = map[string]bool{}
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
				also := ""
				if len(renamed) > 0 {
					also = "，换号前留的备份里这一条也是明文（和 tunnels.json 一样）"
				}
				log.Printf("隧道「%s」的凭据加密失败，暂时保留明文%s: %v", t.Name, also, err)
			} else {
				t.Credential, migrated = sealed, true
			}
		default:
			if !secret.Readable(t.Credential) {
				m.broken[t.ID] = true
			}
		}
	}
	if len(renamed) > 0 {
		m.renum = m.newPendingRenumber(raw, seqRead, renamed, migrated)
	}
	m.writeBackLoaded(migrated)
}

// writeBackLoaded 读进来以后要改写文件的两件事合成一次写：旧版明文凭据加密（migrated）、重复编号换号（m.renum）。
// 写不回去不要紧：内存里已经是密文、编号已经不重复，这次运行照常用；没写进去的，下次哪里保存成功时一起写进去
// （m.renum 留着，那时照样先备份），下次启动也会再来一遍。写不回去不算没读成，不碰 m.loadErr。
func (m *Manager) writeBackLoaded(migrated bool) {
	p := m.renum
	if !migrated && p == nil {
		return
	}
	backup, err := m.write()
	if err != nil && migrated {
		log.Printf("隧道凭据加密后写回失败，文件里暂时还是明文，下次保存时再写: %v", saveCause(err))
	}
	if p != nil {
		m.noteRenumbered(p, backup, err)
	}
}

// pendingRenumber 读进来时换了号、还没写进 tunnels.json 的那次换号。
type pendingRenumber struct {
	backup  []byte // 改写之前在旁边留的备份的内容，见 newPendingRenumber
	renamed []renumbered
	sealed  bool // backup 里原来的明文凭据已经加密，不是读进来时逐字节的样子
}

// newPendingRenumber 换了号的那份 tunnels.json 被改写之前要留的备份：一般是读进来时的原样字节（raw，BOM 也在）。
// 原文件里有旧版明文凭据、这次加密了（migrated）的，原样留就是把明文长期放在程序文件夹里；这时备份写成凭据已加密、
// 编号和 seq 还是换号前的那一版，也就是老版本这次启动会写回去的样子。加密失败的那几条还是明文，和 tunnels.json 一样。
// 只拷一份来改，m.tunnels 不动。
func (m *Manager) newPendingRenumber(raw []byte, seq int, renamed []renumbered, migrated bool) *pendingRenumber {
	p := &pendingRenumber{backup: raw, renamed: renamed}
	if !migrated {
		return p
	}
	from := make(map[string]string, len(renamed)) // 新编号 → 换号前的编号（新编号不会和任何一条撞）
	for _, r := range renamed {
		from[r.To] = r.From
	}
	before := make([]*Tunnel, len(m.tunnels))
	for i, t := range m.tunnels {
		c := *t
		if id, ok := from[c.ID]; ok {
			c.ID = id
		}
		before[i] = &c
	}
	// 编不出来时 write 用同一个写法也编不出来，什么都不会写，备份内容是什么无所谓
	if b, err := encodeTunnels(seq, before); err == nil {
		p.backup, p.sealed = b, true
	}
	return p
}

// args 换号那条操作记录的 args：换了哪几条、备份的文件名（只写文件名；没留下备份时没有）、备份里的凭据是不是这次加密的。
func (p *pendingRenumber) args(backup string) map[string]any {
	a := map[string]any{"renamed": p.renamed}
	if backup != "" {
		a["backup"] = backup
		if p.sealed {
			a["backupSealed"] = true
		}
	}
	return a
}

// backupSays 程序日志里说备份在哪的那一句。
func (p *pendingRenumber) backupSays(backup string) string {
	s := "读进来时的那份 tunnels.json 备份在同一文件夹里的 " + backup
	if p.sealed {
		s += "，原文件里的旧版明文凭据在备份里已经加密，备份不是原文件逐字节的样子"
	}
	return s
}

// renumbered 读 tunnels.json 时换了编号的一条隧道（也是操作记录里 renamed 的一项）。
type renumbered struct {
	Tunnel string `json:"tunnel"` // 隧道名
	From   string `json:"from"`
	To     string `json:"to"`
}

// renumberActor 读 tunnels.json 时给重复编号换号，操作记录里写的「谁做的」。
const renumberActor = "读取隧道配置"

// renumberDuplicates 编号和前面某条重复的隧道换一个新编号（A58）。以前手改 tunnels.json 把 seq 调小，新增的隧道会拿到
// 已有的编号；按编号改、删、启动时只认得靠前的那条（get），后面那条改不了、删不掉，两条自定义 frps 的隧道还会写同一份
// frpc 配置。靠前的那条留着原编号：别处记着的编号（以前的操作记录、AI 的提案、网页上开着的卡片）分不清指的是哪一条，
// 一律算它的。新编号照 Add 的办法让 seq 往上加；调用方已经把 seq 抬到现有 tun<数字> 的最大值，再对一遍已有的编号兜底。
// 只在 load 里调：那时还没有隧道在跑（见 load 的说明），没有哪个 frpc 还记着旧编号。
func (m *Manager) renumberDuplicates() []renumbered {
	taken := make(map[string]bool, len(m.tunnels))
	for _, t := range m.tunnels {
		taken[t.ID] = true
	}
	kept := make(map[string]bool, len(m.tunnels))
	var out []renumbered
	for _, t := range m.tunnels {
		if !kept[t.ID] {
			kept[t.ID] = true
			continue
		}
		id := ""
		for id == "" || taken[id] {
			m.seq++
			id = "tun" + strconv.Itoa(m.seq)
		}
		taken[id] = true
		out = append(out, renumbered{Tunnel: t.Name, From: t.ID, To: id})
		t.ID = id
	}
	return out
}

// noteRenumbered 换号的结果在程序日志和操作记录里各记一条：几条、各换成了什么、备份叫什么（只写文件名）。
// werr 是写回 tunnels.json 的结果：没写进去时这次运行照新编号用，之后保存或下次启动时再写。
func (m *Manager) noteRenumbered(p *pendingRenumber, backup string, werr error) {
	parts := make([]string, len(p.renamed))
	for i, r := range p.renamed {
		parts[i] = fmt.Sprintf("「%s」%s→%s", r.Tunnel, r.From, r.To)
	}
	msg := fmt.Sprintf("tunnels.json 里有 %d 条隧道的编号和前面的重复，已换成新编号：%s", len(p.renamed), strings.Join(parts, "、"))
	if backup != "" {
		msg += "。" + p.backupSays(backup)
	}
	var err error
	if werr != nil {
		err = fmt.Errorf("换好的编号没能写回 tunnels.json，这次运行照新编号用，之后保存或下次启动时再写：%w", saveCause(werr))
		msg += "。" + err.Error()
	}
	log.Print(msg)
	m.noteSoon(AutoAction{Op: "tunnel.renumber", Actor: renumberActor, Args: p.args(backup), Err: err})
}

// noteRenumberSaved 启动时没写进去的换号，这次保存写进去了：程序日志和操作记录各补一条成功的，带上备份的文件名。
func (m *Manager) noteRenumberSaved(p *pendingRenumber, backup string) {
	log.Printf("读 tunnels.json 时换过的重复编号这次写进去了，%s", p.backupSays(backup))
	m.noteSoon(AutoAction{Op: "tunnel.renumber", Actor: renumberActor, Args: p.args(backup)})
}

// reallyMissing 读 tunnels.json 报「不存在」（readErr）以后再确认一次，是真的新装才返回 nil，否则返回读不出来的原因。
// Windows 上路径里某一级不在（网络盘断了、U 盘拔了、程序文件夹暂时访问不了）也报 ErrNotExist，
// 当成新装的话，路径一恢复，下一次保存就把原文件盖掉了（2026-11-15 Codex 复审 P1）。
// 所以三样都要对上才算新装：读的时候报的是「文件夹在、文件不在」（fileNotFound），程序文件夹看得到，
// 再看一眼文件还是「文件夹在、文件不在」。最后一步也要分清：Stat 文件夹成功以后路径又失联，报的是路径找不到，
// 不能放行（Codex 第二轮复审）。中间文件冒出来了，按「这次没读成」处理，下次再读。
func (m *Manager) reallyMissing(readErr error) error {
	if !fileNotFound(readErr) {
		return readErr
	}
	dir := filepath.Dir(m.file())
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("程序文件夹访问不了：%w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("程序文件夹的位置上不是文件夹：%s", dir)
	}
	if beforeFileRecheck != nil {
		beforeFileRecheck()
	}
	_, err = os.Lstat(m.file())
	switch {
	case err == nil:
		return errors.New("刚才读的时候说不存在，再看又在了，这次先不读")
	case !fileNotFound(err):
		return err
	}
	return nil
}

// beforeFileRecheck 测试用：reallyMissing 查完程序文件夹、再看文件之前调用，用来在这个空当里让路径失联。
var beforeFileRecheck func()

// lacksTunnelsKey 解析得了、却没有 tunnels 这一项，除了 seq 还有别的东西：多半是手改时把键名改错了（比如 "tunels"），
// 隧道和凭据还在别的键底下，当空列表存一次就没了（2026-11-15 Codex 复审）。MineDeck 从 v2.0.0 起写的文件总有这一项
// （一直是 json:"tunnels"，没有 omitempty）；{}、null、只有 seq 的里面没有能丢的东西，照空列表算。
// 键名按 encoding/json 的规矩不分大小写比。
func lacksTunnelsKey(b []byte) bool {
	var keys map[string]json.RawMessage
	if json.Unmarshal(b, &keys) != nil {
		return false
	}
	other := false
	for k := range keys {
		switch {
		case strings.EqualFold(k, "tunnels"):
			return false
		case !strings.EqualFold(k, "seq"):
			other = true
		}
	}
	return other
}

// jsonErrAt 解析报错带上大概在第几行：提示里让人用记事本修，得知道去哪儿找。
func jsonErrAt(b []byte, err error) error {
	var off int64 = -1
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	}
	if off < 0 || off > int64(len(b)) {
		return err
	}
	return fmt.Errorf("第 %d 行附近：%w", bytes.Count(b[:off], []byte("\n"))+1, err)
}

// retryLoad 上次没读成时再读一次，返回现在的状态（读成了、或者本来就读成了，是 nil）。调用方持有 m.mu。
// 没读成期间列表一直是空的、什么也存不进去，所以读成了直接换上，不会冲掉谁的改动。
// 只读一次、不等：它在锁里，启动时的那几次等待在 NewManager 里。
func (m *Manager) retryLoad() error {
	if m.loadErr == nil {
		return nil
	}
	prev := m.loadErr.Error()
	m.load()
	if m.loadErr == nil {
		log.Printf("隧道配置读出来了（%d 条隧道），恢复正常", len(m.tunnels))
		return nil
	}
	if s := m.loadErr.Error(); s != prev { // 界面每 4 秒问一次：原因没变就不重复记
		log.Printf("隧道配置还是没读出来: %s", s)
	}
	return m.loadErr
}

// LoadErr tunnels.json 在却没读成时返回 *LoadError（先再读一次）；读成了、或者文件本来就不在，返回 nil。
// 操作层查隧道列表前先问它：没读成时报出来，不拿一个空列表冒充「没有隧道」。
func (m *Manager) LoadErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.retryLoad()
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

// SaveError 隧道配置没能写进 tunnels.json。新增、修改、删除隧道遇到它时，内存里的改动已经退回，
// 和磁盘上的那份一致：报了失败就是真的没改，不会出现「报成功、重启以后这条隧道没了」（以前写盘的错误被吞掉，
// 2026-11-14 PR #13 复审顺带发现）。
type SaveError struct{ Err error }

func (e *SaveError) Error() string {
	return "隧道配置没能写进磁盘（tunnels.json），这次的改动没有保存：" + e.Err.Error()
}

func (e *SaveError) Unwrap() error { return e.Err }

// Hint 界面报错正文接的那句话（原文带着路径和系统的英文报错，接不上）。规矩见 app.js 的 errSummary：60 字以内，不带路径和英文句子。
func (e *SaveError) Hint() string {
	return "隧道配置写不进磁盘，这次没有保存。请检查磁盘是否已满、MineDeck 所在的文件夹是否只读"
}

// saveCause 系统给的原因。SaveError 自己的说法是「改动没有保存」，内存里照样留着改动的地方（迁移凭据、记公网地址）
// 用它就说错了，只报原因。
func saveCause(err error) error {
	if se := (*SaveError)(nil); errors.As(err, &se) {
		return se.Err
	}
	return err
}

// save 把隧道配置整份写进 tunnels.json，调用方持有 m.mu。先写同一文件夹里的临时文件再改名换上去，
// 写到一半断电或磁盘满了，原来的 tunnels.json 还是完整的旧版本。用 WriteProtected：用户把 tunnels.json 设成只读时
// 照旧拒绝覆盖（以前的 os.WriteFile 也是写不进去），只是现在会报出来。
//
// tunnels.json 没读成（m.loadErr）时一律不写：内存里是空列表，写下去就把原文件里的隧道和凭据盖掉了。
// 新增、修改、删除在改内存之前就已经用 retryLoad 挡下；这里再挡一道，兜住以后新加的保存点。
// 报的是 LoadError 本身，不包成 SaveError：「这次的改动没有保存、检查磁盘」说的不是这回事。
//
// 读进来时给重复编号换过号、还没写回去（m.renum）的，先在旁边留一份读进来时的那份的备份再写，见 write；
// 写进去了，程序日志和操作记录补一条（启动时那条记的是没写进去）。
func (m *Manager) save() error {
	p := m.renum
	backup, err := m.write()
	if err == nil && p != nil {
		m.noteRenumberSaved(p, backup)
	}
	return err
}

// write 同 save，另外返回为换号前的那份留的备份的文件名（不带文件夹；没有要备份的是空的）。
// 备份留不下就不写：那份被换掉以后，重复编号原来的样子就只在备份里了。备份留下了、写却没写成时，m.renum 留着，
// 下次保存再写；备份一般也留着（keepBackup 认得出已经有一份一模一样的，不会再多一份），只有凭据这次加密过的那种
// 认不出来，刚留的就删掉（见 dropBackup）。
func (m *Manager) write() (backup string, err error) {
	if m.loadErr != nil {
		return "", m.loadErr
	}
	b, err := encodeTunnels(m.seq, m.tunnels)
	if err != nil {
		return "", &SaveError{Err: err}
	}
	fresh := false
	if m.renum != nil {
		if backup, fresh, err = keepBackup(m.file(), m.renum.backup); err != nil {
			return "", &SaveError{Err: fmt.Errorf("改写之前没能先留一份原文件的备份：%w", err)}
		}
	}
	if err := atomicfile.WriteProtected(m.file(), b, 0o600); err != nil { // 凭据文件收紧权限
		if fresh && m.renum.sealed {
			backup = dropBackup(m.file(), backup)
		}
		return backup, &SaveError{Err: err}
	}
	m.renum = nil
	return backup, nil
}

// encodeTunnels tunnels.json 的写法。保存和凭据加密过的那种备份（newPendingRenumber）共用，两边一个样子。
func encodeTunnels(seq int, tunnels []*Tunnel) ([]byte, error) {
	return json.MarshalIndent(struct {
		Seq     int       `json:"seq"`
		Tunnels []*Tunnel `json:"tunnels"`
	}{seq, tunnels}, "", "  ")
}

// dropBackup 写回没成时删掉刚留的、凭据这次加密过的那种备份，返回还在的备份名（删掉了是空的）。DPAPI 每次加密的密文
// 都不一样，这种备份 keepBackup 认不出「内容一样的旧备份」，tunnels.json 只读时不删就每次启动多一份；
// 写回没成，原文件还在，用不着它，下次写之前会再留一份。删不掉就留着，照实报它的名字。
func dropBackup(path, name string) string {
	if err := os.Remove(filepath.Join(filepath.Dir(path), name)); err != nil {
		log.Printf("tunnels.json 没写进去，刚留的备份 %s 也没删掉: %v", name, err)
		return name
	}
	return ""
}

// 备份文件名里的时间取哪个钟（测试里钉住）、同一个名字被占了时最多试几个（-2、-3……）。
var (
	backupNow       = time.Now
	backupNameTries = 100
)

// keepBackup 在 path 旁边留一份内容是 orig 的备份，返回备份的文件名（不带文件夹）和是不是这次新留的。名字是
// 「原名.bak-年月日-时分秒」，.gitignore 里的 /tunnels.json* 管得到。旁边已经有一份内容一模一样的就用那份，不再另存：
// tunnels.json 设成只读时每次启动都写不回去，不能每次都多一份。同一秒里再来一次、内容又不一样，名字就加上 -2、-3；
// 发布用 PublishNew，名字被占了只会报错，绝不盖掉已有的备份。备份里有凭据（旧版明文的已经加密，见 newPendingRenumber；
// 加密失败的还是明文），权限和 tunnels.json 一样收紧。
func keepBackup(path string, orig []byte) (string, bool, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	if name := sameBackup(dir, base, orig); name != "" {
		return name, false, nil
	}
	f, err := os.CreateTemp(dir, ".minedeck-write-*.tmp")
	if err != nil {
		return "", false, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(orig)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", false, err
	}
	stem := base + ".bak-" + backupNow().Format("20060102-150405")
	for i := 1; i <= backupNameTries; i++ {
		name := stem
		if i > 1 {
			name = fmt.Sprintf("%s-%d", stem, i)
		}
		err := atomicfile.PublishNew(tmp, filepath.Join(dir, name))
		if err == nil {
			return name, true, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", false, err
		}
	}
	return "", false, fmt.Errorf("%s 起头的 %d 个名字都被占了", stem, backupNameTries)
}

// sameBackup dir 里已有的、内容和 orig 一模一样的「base.bak-…」备份的文件名，没有时为空。只认普通文件，
// 不顺着链接去读别处的东西；读不出来的当没有，另存一份。
func sameBackup(dir, base string, orig []byte) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), base+".bak-") {
			continue
		}
		if fi, err := e.Info(); err != nil || fi.Size() != int64(len(orig)) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && bytes.Equal(b, orig) {
			return e.Name()
		}
	}
	return ""
}

// Status 隧道运行状态。
type Status struct {
	Tunnel
	Running    bool   `json:"running"`
	LastError  string `json:"lastError,omitempty"`
	CredBroken bool   `json:"credBroken,omitempty"` // 凭据解不开（换了电脑或 Windows 用户），要重填
	Online     bool   `json:"online,omitempty"`     // 运行中，而且这次运行已经连上（报过成功或拿到过公网地址）
	// HasCredential 存着凭据（凭据本身不下发）。界面靠它决定给不给自定义 frps 的隧道显示「清除凭据」
	HasCredential bool `json:"hasCredential,omitempty"`
}

func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		rs := m.running[t.ID]
		st := Status{Tunnel: *t, Running: rs != nil, Online: rs != nil && rs.online, LastError: m.lastErr[t.ID], CredBroken: m.broken[t.ID],
			HasCredential: t.Credential != ""}
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
	if err := checkProxyID(&t, nil); err != nil {
		return nil, err
	}
	sealed, err := secret.Seal(t.Credential)
	if err != nil {
		return nil, fmt.Errorf("凭据：%w", err)
	}
	t.Credential = sealed
	m.mu.Lock()
	defer m.mu.Unlock()
	// tunnels.json 没读成：先于改内存挡下（编号、列表都不动），不然这一条存下去就把原文件盖掉了
	if err := m.retryLoad(); err != nil {
		return nil, err
	}
	if err := checkGuardPort(&t, m.guardPort); err != nil {
		return nil, err
	}
	prev := m.tunnels
	m.seq++
	t.ID = "tun" + strconv.Itoa(m.seq)
	nt := &t
	m.tunnels = append(m.tunnels, nt)
	if err := m.save(); err != nil {
		// 写不进磁盘就当没加过：编号也退回去。这个编号还没交给过任何人（报的是失败），下次新增再用不会撞上
		m.tunnels, m.seq = prev, m.seq-1
		return nil, err
	}
	resp := *nt
	resp.Credential = "" // 响应不回显凭据
	return &resp, nil
}

// UpdateOpts 修改隧道时的附加要求。它不是隧道配置的一部分，不存盘。
type UpdateOpts struct {
	// ClearCredential 删掉存着的凭据，之后启动不带凭据。要明说，不能靠凭据留空来猜：留空表示「不改」。
	// 只有自定义 frps 能这样（frps 可以不设 token）；OpenFrp、樱花穿透启动时必须带凭据，validate 也要求填。
	ClearCredential bool
}

// Patch 修改隧道时只带要改的项：nil 表示不改，沿用存着的那份。
// 两个窗口同时开着同一条隧道时，以前「编辑」把整份配置带回去，后存的一边就把另一边刚存的改动（比如刚打开的跟随启动）
// 改回原样；现在界面只发自己改了的项（2026-11-17）。
// 没有公网地址：那是运行中从 frpc 的输出里记下的，不是配置，修改时始终留着存着的那个。
type Patch struct {
	Provider   *string `json:"provider"`
	Name       *string `json:"name"`
	Credential *string `json:"credential"` // 不给或留空表示不改（列表接口不回显凭据）
	ProxyID    *string `json:"proxyId"`
	ServerAddr *string `json:"serverAddr"`
	ServerPort *int    `json:"serverPort"`
	RemotePort *int    `json:"remotePort"`
	LocalPort  *int    `json:"localPort"`
	Bound      *string `json:"boundInstance"`
	AutoStart  *bool   `json:"autoStart"`
	Purpose    *string `json:"purpose"`
	Proto      *string `json:"proto"`
}

// mergeInto 把给了的项合到 t（存着的那份的副本）上。凭据换成这次填的明文（没填是空，表示沿用），
// 和 UpdateOpt 收到的整份配置一个样子，后面的检查才能共用。
func (p Patch) mergeInto(t Tunnel, cred string) Tunnel {
	set := func(dst, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setInt := func(dst, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	set(&t.Provider, p.Provider)
	set(&t.Name, p.Name)
	set(&t.ProxyID, p.ProxyID)
	set(&t.ServerAddr, p.ServerAddr)
	setInt(&t.ServerPort, p.ServerPort)
	setInt(&t.RemotePort, p.RemotePort)
	setInt(&t.LocalPort, p.LocalPort)
	set(&t.Bound, p.Bound)
	if p.AutoStart != nil {
		t.AutoStart = *p.AutoStart
	}
	set(&t.Purpose, p.Purpose)
	set(&t.Proto, p.Proto)
	t.Credential = cred
	return t
}

// Update 更新隧道配置（运行中不可改）。凭据留空表示不改。
func (m *Manager) Update(id string, t Tunnel) error { return m.UpdateOpt(id, t, UpdateOpts{}) }

// UpdateOpt 同 Update，另带附加要求（比如清除凭据）。整份替换：没带的项按零值存（公网地址也取 t 里的）。
// 界面和操作层现在走 PatchOpt；这里留给整份替换的调用方，检查和写盘与 PatchOpt 共用 checkEdit、replaceLocked。
func (m *Manager) UpdateOpt(id string, t Tunnel, opt UpdateOpts) error {
	// 整份配置在拿锁前就能查完：报错的先后和以前一样（填错了先报填错，再看隧道在不在、是不是在运行）
	if err := checkEdit(&t, opt); err != nil {
		return err
	}
	sealed, err := secret.Seal(t.Credential)
	if err != nil {
		return fmt.Errorf("凭据：%w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.retryLoad(); err != nil { // 没读成时列表是空的，不能报成「隧道不存在」
		return err
	}
	return m.replaceLocked(id, t, sealed, opt)
}

// PatchOpt 只改 p 里给了的项，其余沿用存着的配置（公网地址始终留着）；opt 同 UpdateOpt。
// 合并在 m.mu 里做：拿锁以后再取存着的那份，两个窗口先后保存，后一个合在前一个存下的结果上，不会把它改回去。
// 合好以后的检查和写盘和 UpdateOpt 是同一段（checkEdit、replaceLocked）。
func (m *Manager) PatchOpt(id string, p Patch, opt UpdateOpts) error {
	// 凭据在拿锁前加密（DPAPI 不快，不占着锁）。先去掉前后空白：UpdateOpt 那边是 validateKeep 修剪过以后才加密的
	cred := ""
	if p.Credential != nil {
		cred = strings.TrimSpace(*p.Credential)
	}
	sealed, err := secret.Seal(cred)
	if err != nil {
		return fmt.Errorf("凭据：%w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.retryLoad(); err != nil { // 没读成时列表是空的，不能报成「隧道不存在」
		return err
	}
	old := m.get(id)
	if old == nil {
		return fmt.Errorf("隧道不存在")
	}
	t := p.mergeInto(*old, cred)
	// 「清凭据只许自定义 frps」、validateKeep 这些要看合好以后的整份：只发 {clearCredential:true} 时，服务商是存着的那个
	if err := checkEdit(&t, opt); err != nil {
		return err
	}
	return m.replaceLocked(id, t, sealed, opt)
}

// checkEdit 修改隧道时对新的整份配置的检查（不看 Manager 的状态；顺手规范化 t：去空格、补默认的用途和协议）。
// UpdateOpt 在拿锁前查，PatchOpt 合好以后在锁里查。
func checkEdit(t *Tunnel, opt UpdateOpts) error {
	if opt.ClearCredential {
		// 先于 validate 查：服务商的隧道凭据留空时 validate 只会说「请填写凭据」，说不清为什么不能清
		if t.Provider != "custom" {
			return fmt.Errorf("只有自定义 frps 的隧道能清除凭据，OpenFrp 和樱花穿透启动时必须带凭据")
		}
		if strings.TrimSpace(t.Credential) != "" {
			return fmt.Errorf("要清除凭据，就不要同时填新的凭据")
		}
	}
	// 凭据留空表示不改（列表接口不回显凭据，界面上的「编辑」只改名称、绑定和跟随，手里没有凭据）。
	// OpenFrp、樱花穿透启动时必须带凭据，validate 本来要求填：这里先按「沿用存着的那份」查其余各项，
	// 存着的那份有没有、能不能带过去，拿锁找到旧配置以后再查（以前这两家的隧道只要不重填凭据，什么都改不了）
	keep := !opt.ClearCredential && strings.TrimSpace(t.Credential) == ""
	return validateKeep(t, keep)
}

// replaceLocked 用查过的整份配置 t 换掉存着的那份，调用方持有 m.mu、已经 retryLoad 过。sealed 是这次填的凭据的密文
// （没填是空）。网页端口、运行中、正在启动、凭据能不能沿用这几道在这里查，写不进磁盘就退回。
func (m *Manager) replaceLocked(id string, t Tunnel, sealed string, opt UpdateOpts) error {
	if err := checkGuardPort(&t, m.guardPort); err != nil {
		return err
	}
	if m.running[id] != nil {
		return fmt.Errorf("请先停止隧道再修改")
	}
	if m.starting[id] { // 启动拿的是改之前的那份：这时候改，存下的和实际跑起来的就对不上了（见 errStarting）
		return errStarting("改")
	}
	old := m.get(id)
	if old == nil {
		return fmt.Errorf("隧道不存在")
	}
	if err := checkProxyID(&t, old); err != nil {
		return err
	}
	t.ID = id
	fixed := false // 换掉了存着的凭据：解不开的标记要清掉。等写进磁盘以后再清，写不进去时这条隧道仍是解不开的那份凭据
	if opt.ClearCredential {
		// 明说了不要凭据：换了地址、端口也放行（没有凭据可带走）。存着的密文只在 tunnels.json 和内存里这一份，
		// 下面 save 写回去就没了；frpc 的配置文件里从来只有模板，下次启动时重写，不带 auth.token
		t.Credential = ""
		fixed = true // 解不开的凭据也一起清掉，这条隧道又能启动了
	} else if sealed == "" {
		// 凭据留空表示不修改（列表接口不回显凭据）。但换了服务商，或者自定义 frps 换了地址、端口，要重新填：
		// 不然留着的凭据会被带到新的服务器上去。局域网登录的人读不到凭据，却能改隧道，
		// 改个地址就能把令牌送到自己的服务器（v3.1.0 正式版前检查 B-5）。
		// 以前存下的地址没去空格和方括号，按同样的写法比
		if old.Credential != "" && (t.Provider != old.Provider ||
			(t.Provider == "custom" && (t.ServerAddr != normServerAddr(old.ServerAddr) || t.ServerPort != old.ServerPort))) {
			return fmt.Errorf("换了服务商或 frps 地址、端口，要重新填一次凭据")
		}
		// 没有存着的凭据可沿用（原来是不带 token 的自定义 frps，现在改成了要凭据的服务商）：和新增时一样要填
		if old.Credential == "" && needsCredential(t.Provider) {
			return errNeedCredential
		}
		t.Credential = old.Credential
	} else {
		t.Credential = sealed
		fixed = true
	}
	// 原地改（运行中的 frpc 那边拿的是这个指针），写不进磁盘就把旧内容放回去
	prev := *old
	*old = t
	if err := m.save(); err != nil {
		*old = prev
		return err
	}
	if fixed {
		delete(m.broken, id)
	}
	return nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.retryLoad(); err != nil {
		return err
	}
	if m.running[id] != nil {
		return fmt.Errorf("请先停止隧道再删除")
	}
	if m.starting[id] { // 删了的话，准备好的 frpc 还会照着旧配置起来，列表里却已经没有它，停不掉
		return errStarting("删")
	}
	for i, t := range m.tunnels {
		if t.ID == id {
			// 删掉以后的列表放进新的数组：原地挪（append(s[:i], s[i+1:]...)）会改动旧数组，写不进磁盘时就退不回去了。
			// 先复制再删，删光了也是空列表、存成 []（slices.Concat 拼出空的会是 nil，存成 null）
			prev := m.tunnels
			m.tunnels = slices.Delete(slices.Clone(prev), i, i+1)
			if err := m.save(); err != nil {
				m.tunnels = prev
				return err
			}
			delete(m.broken, id)
			return nil
		}
	}
	return fmt.Errorf("隧道不存在")
}

// errStarting 隧道正在准备启动（m.starting：第一次用要先下载 frpc，可能要一阵子）时拒绝修改、删除的说法，what 是「改」或「删」。
// 启动在准备之前就拍下了配置，准备期间改了的话，存下的（跟随关系、卡片上的内容）和之后实际起来的那份就对不上了（2026-11-16 Codex 复审 beta.3）。
// 界面直接把这句接在「没能保存修改」后面，所以要短、不带英文和冒号（见 app.js 的 errSummary）。
func errStarting(what string) error {
	return fmt.Errorf("这条隧道正在启动，等它起来或停掉以后再%s", what)
}

// sameConfig 两份隧道配置是不是一样（不比公网地址：那是运行中记下的，不是配置）。启动登记进 running 之前拿它核对配置没被改过。
func sameConfig(a, b Tunnel) bool {
	a.PublicAddr, b.PublicAddr = "", ""
	return a == b
}

// reHostname 域名：几段用点隔开，每段是字母（中文这类也算）、数字、连字符、下划线。比 DNS 的规矩宽：
// 这里只为挡住模板的花括号、引号、空白这类字符，域名写得对不对交给 frpc 去解析；中文域名、带下划线的旧配置照常能用。
var reHostname = regexp.MustCompile(`^[\p{L}\p{M}\p{N}_-]+(\.[\p{L}\p{M}\p{N}_-]+)*\.?$`)

// checkServerAddr 自定义 frps 的地址只认域名或 IP。地址会原样写进 frpc 的配置，frpc 读配置时把整份文件当模板渲染：
// 地址里写 {{ .Envs.MINEDECK_FRP_TOKEN }} 这类东西，令牌就被拼进主机名，frpc 一解析这个域名就带出去了
// （v3.1.0 正式版前检查 B-5）。顺带挡住把 http:// 和端口一起填进地址的常见误填。
func checkServerAddr(addr string) error {
	if net.ParseIP(strings.Trim(addr, "[]")) != nil {
		return nil
	}
	if utf8.RuneCountInString(addr) <= 253 && reHostname.MatchString(addr) {
		return nil
	}
	return fmt.Errorf("frps 地址只能填域名或 IP，比如 frp.example.com，不要带 http:// 和端口")
}

// normServerAddr 去掉前后空格和 IPv6 的方括号（frpc 自己会加）。保存时和启动时用同一个写法。
func normServerAddr(addr string) string {
	return strings.Trim(strings.TrimSpace(addr), "[]")
}

// needsCredential 这家服务商启动时必须带凭据（OpenFrp 的用户密钥、樱花的访问密钥）。自定义 frps 可以不设 token。
func needsCredential(provider string) bool { return provider == "openfrp" || provider == "natfrp" }

var errNeedCredential = errors.New("请填写凭据与隧道 ID（在服务商官网创建隧道后获取）")

var errBadProxyID = errors.New("隧道 ID 只能是数字（在服务商官网的隧道列表里看）")

// parseProxyID 平台的隧道编号（OpenFrp、樱花都是正整数）。「007」「+7」都能被 Atoi 认成 7，带正负号的另外拒掉；
// 调用方按 strconv.Itoa(id) 存，同一条隧道只有一种写法，查重才查得出来。
func parseProxyID(s string) (int, bool) {
	s = strings.TrimSpace(s)
	id, err := strconv.Atoi(s)
	return id, err == nil && id > 0 && !strings.ContainsAny(s, "+-")
}

// checkProxyID OpenFrp、樱花的隧道 ID 只收数字，存成十进制形式（同 ImportTunnel）。编号原样交给 frpc（樱花拼成 -f 凭据:编号），
// 以前什么都收：局域网会话沿用存着的凭据时能把它改成「8,9」这种，frpc 自己怎么解析逗号、冒号没查清（A60）。
// old 是改之前存着的那份（新增时为 nil）：编号和服务商都没变的不查，以前存下的不是数字的编号照常能改别的、能启动。
func checkProxyID(t, old *Tunnel) error {
	if !needsCredential(t.Provider) || old != nil && t.ProxyID == old.ProxyID && t.Provider == old.Provider {
		return nil
	}
	id, ok := parseProxyID(t.ProxyID)
	if !ok {
		return errBadProxyID
	}
	t.ProxyID = strconv.Itoa(id)
	return nil
}

func validate(t *Tunnel) error { return validateKeep(t, false) }

// validateKeep 同 validate；keepCred 为 true 时凭据留空不算缺（修改时沿用存着的那份，由 UpdateOpt 拿锁后核对）。
func validateKeep(t *Tunnel, keepCred bool) error {
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
		if (t.Credential == "" && !keepCred) || strings.TrimSpace(t.ProxyID) == "" {
			return errNeedCredential
		}
		clearUnusedPorts(t)
	case "custom":
		t.ServerAddr = normServerAddr(t.ServerAddr)
		if t.ServerAddr == "" || t.ServerPort <= 0 || t.RemotePort <= 0 {
			return fmt.Errorf("自定义 frps 需填写服务器地址、端口与公网远程端口")
		}
		if err := checkServerAddr(t.ServerAddr); err != nil {
			return err
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
	if err := m.retryLoad(); err != nil { // 没读成时说清原因，不报「隧道不存在」；读成了就照常启动
		m.mu.Unlock()
		return err
	}
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
	orig := snap          // 凭据解开之前的那份：登记进 running 之前拿它核对配置没变（见下面登记那一步）
	guard := m.guardPort
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
	// 以前存下的、或者网页端口这次换了（默认端口被占用时随机挑一个）：启动时再查一遍，跟随实例启动也走这里
	if err := checkGuardPort(&snap, guard); err != nil {
		return fail(err)
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
	// 准备期间改、删都被拦着（见 errStarting），这里再核对一遍：隧道还在、还是启动时拍下的那份配置，才登记成运行中。
	// 对不上就把刚起来的 frpc 结束掉——它照着旧配置在跑，登记上的话卡片、跟随关系说的都是新的那份
	if cur := m.get(id); cur != t || !sameConfig(*cur, orig) {
		m.mu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fail(fmt.Errorf("启动的这段时间里这条隧道被改过或删掉了，这次没有启动，请再启动一次"))
	}
	m.running[id] = rs
	m.mu.Unlock()
	m.setLastErr(id, "") // 运行起来了：上一次的失败原因不再显示（frpc 再报错时 handleLine 会重新记）

	mask := credMasker(cred)
	go func() {
		m.pumpOutput(t, rs, con, stdout, mask)
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
			// exe 也没了的，说被杀毒软件删了：这比 frpc 之前自己报的错要紧（不处理它，再启动也起不来；Codex 复审发现）。
			// 否则 frpc 自己报过错的留着那条（更具体），没报就记「意外退出」，卡片上看得到
			if !userStopped {
				if gone != "" {
					m.lastErr[id] = clipLastErr(gone)
				} else if m.lastErr[id] == "" {
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

// addrRe 从一行日志里找「主机:端口」或 IPv4。第一种写法也会配上 IP，是不是 IP 要再拆开看。
// 它不认下划线和斜杠，所以 frpc 日志前缀里的源码位置（client/control_ext.go:37）会被切成 ext.go:37。
// 不能拿 FindString 的第一处就当公网地址：同一行后面往往还有真正的域名:端口。
var addrRe = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z0-9.-]+:\d{2,5}|\d{1,3}(?:\.\d{1,3}){3}:\d{2,5})`)

// ipv6AddrRe 方括号里的 IPv6 加端口，例如 [2001:db8::1]:53981。源码位置是 file.go:数字，对不上这个形状。
var ipv6AddrRe = regexp.MustCompile(`\[(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f:.]*%?[0-9A-Za-z._~-]*\]:\d{2,5}`)

// goCallerRe 日志里的 Go 源码位置，不是可连接的地址。覆盖这几种写法：
//   - 带目录：client/control.go:168、client/control_ext.go:37、D:\src\client\control.go:37
//   - 文件名里有下划线：control_ext.go:37、proxy_manager.go:173（地址正则会从下划线后面再切一截）
//   - 单独放在方括号里的 file.go:行号：[control.go:37]
//
// 没有斜杠、也没有下划线的 name.go:端口（比如 example.go:25565）不在这里面，那是域名。
var goCallerRe = regexp.MustCompile(
	`(?:[A-Za-z]:[\\/])?(?:[A-Za-z0-9_.-]+[\\/])+[A-Za-z0-9_.-]+\.go:\d+` +
		`|[A-Za-z0-9_-]*_[A-Za-z0-9_-]*\.go:\d+` +
		`|\[[A-Za-z0-9_-]+(?:[\\/][A-Za-z0-9_-]+)*\.go:\d+\]`)

// isIPAddr 「主机:端口」的主机是不是 IP（含方括号里的 IPv6）。
func isIPAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && net.ParseIP(host) != nil
}

// publicAddrCandidates 一行里从左到右的公网地址候选。源码位置、粘在更长记号上的半截、
// 端口不在 1–65535 的都去掉。同一处 IPv6 套着的 IPv4 只留外面的完整写法。
func publicAddrCandidates(line string) []string {
	callers := goCallerSpans(line)
	type hit struct {
		start, end int
		addr       string
	}
	var hits []hit
	overlapsCaller := func(s, e int) bool {
		for _, c := range callers {
			if s < c[1] && e > c[0] {
				return true
			}
		}
		return false
	}
	// 前一个字符还是名字的一部分：control_ext.go 里切出来的 ext.go，或 my_node.example.com 里切出来的 node.example.com。
	glued := func(s int) bool {
		if s == 0 {
			return false
		}
		c := line[s-1]
		return c == '_' || (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	// 后一个字符还是数字或字母：端口被正则截短了，[2001:db8::1]:123456 取出来是 :12345，host.example:12345abc 取出来是
	// host.example:12345，都不是真的端口，整段不要。只看 ASCII：中文日志里地址后面直接接汉字（「…:12345连接」）是常见写法
	gluedRight := func(e int) bool {
		if e >= len(line) {
			return false
		}
		c := line[e]
		return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	for _, loc := range addrRe.FindAllStringIndex(line, -1) {
		if overlapsCaller(loc[0], loc[1]) || glued(loc[0]) || gluedRight(loc[1]) {
			continue
		}
		addr := line[loc[0]:loc[1]]
		if !portInRange(addr) {
			continue
		}
		hits = append(hits, hit{loc[0], loc[1], addr})
	}
	for _, loc := range ipv6AddrRe.FindAllStringIndex(line, -1) {
		addr := line[loc[0]:loc[1]]
		if gluedRight(loc[1]) || !isIPAddr(addr) || !portInRange(addr) {
			continue
		}
		hits = append(hits, hit{loc[0], loc[1], addr})
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return (b.end - b.start) - (a.end - a.start)
	})
	var out []string
	var kept []hit
	for _, h := range hits {
		inside := false
		for _, k := range kept {
			if h.start >= k.start && h.end <= k.end && (h.start != k.start || h.end != k.end) {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		kept = append(kept, h)
		out = append(out, h.addr)
	}
	return out
}

func goCallerSpans(line string) [][2]int {
	var spans [][2]int
	for _, loc := range goCallerRe.FindAllStringIndex(line, -1) {
		// 「使用 [example.go:25565]」这种方括号里的单段 .go 域名是连接地址，不是调用方前缀。
		if goCallerIsConnectPayload(line, loc[0], loc[1]) {
			continue
		}
		spans = append(spans, [2]int{loc[0], loc[1]})
	}
	return spans
}

func goCallerIsConnectPayload(line string, start, end int) bool {
	if start >= end || line[start] != '[' || line[end-1] != ']' {
		return false
	}
	inner := line[start+1 : end-1]
	if strings.ContainsAny(inner, "/\\_") {
		return false
	}
	pre := strings.TrimRight(line[:start], " \t")
	return strings.HasSuffix(pre, "使用") || strings.HasSuffix(strings.ToLower(pre), "use")
}

func portInRange(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || len(port) < 2 || len(port) > 5 {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

// ignoredTunnelAddr 本机回环和未指定地址不当成发给朋友的公网地址。
// IPv4 仍只排除 127. 和 0.0.0.0，内网地址（樱花可能打出来）照旧保留。
func ignoredTunnelAddr(addr string) bool {
	if strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "0.0.0.0") {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.To4() != nil && !strings.Contains(host, ":") {
		return false
	}
	return ip.IsLoopback() || ip.IsUnspecified()
}

func usablePublicAddrs(line string, mask func(string) string) []string {
	var out []string
	for _, addr := range publicAddrCandidates(line) {
		if mask(addr) != addr || ignoredTunnelAddr(addr) {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// handleLine 解析 frpc 输出：提取公网地址、自动应答服务条款确认。
func (m *Manager) handleLine(t *Tunnel, rs *runState, line string) {
	m.handleLineMasked(t, rs, line, func(s string) string { return s })
}

// 状态和交互信号从原文识别，但所有会保存或展示的文字先脱敏。
// 短凭据命中的整行可能被隐藏，不能让这改变隧道启动/条款应答行为。
func (m *Manager) handleLineMasked(t *Tunnel, rs *runState, line string, mask func(string) string) {
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
	// 同一行可能先出现源码位置、再出现真正的地址；只认过滤后的候选，不要第一处。
	addrs := usablePublicAddrs(line, mask)
	if success {
		m.mu.Lock()
		rs.online = true
		m.mu.Unlock()
	}
	if t.Provider == "custom" && success {
		m.setPublicAddr(t, fmt.Sprintf("%s:%d", t.ServerAddr, t.RemotePort))
		return
	}
	if len(addrs) > 0 && (success || strings.Contains(line, "连接") || strings.Contains(low, "remote")) {
		// 域名优先：樱花同时打「使用 >>域名:端口<< 连接」和「或使用节点 IP 连接（不推荐）>>IP:端口<<」两行，
		// IP 会随节点变，发给好友的要是域名。这次运行见过域名之后，后面的 IP 不再覆盖；先 IP 后域名照常换成域名。
		// 同一行里有好几处时按从左到右同样处理，后面的域名盖过前面的。
		// 只看这次运行（PublicAddr 跨次保存着，节点换了只给 IP 时不能卡在上次的域名上）。
		m.mu.Lock()
		rs.online = true
		m.mu.Unlock()
		// 先在这一行里挑出最后该用的那个，只写一次：setPublicAddr 每次都存盘、在控制台记一句、发 tunnel.up
		// （webhook 会往外推，语音隧道会去写 voice_host），不能让同一行里排在前面的 IP 先推出去、紧接着又被域名换掉。
		pick := ""
		for _, addr := range addrs {
			switch {
			case !isIPAddr(addr):
				rs.sawDomain = true
				pick = addr
			case !rs.sawDomain:
				pick = addr
			}
		}
		if pick != "" {
			m.setPublicAddr(t, pick)
		}
	}
	// 失败信息捕获（供隧道行内红字提示）；成功则清除
	if success {
		m.setLastErr(t.ID, "")
	} else if strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(line, "失败") ||
		strings.Contains(low, "shutdown") || strings.Contains(low, "rejected") || strings.Contains(low, "invalid") ||
		strings.Contains(line, "错误") {
		m.setLastErr(t.ID, strings.TrimSpace(mask(line)))
	}
}

// setPublicAddr 记下这次运行的公网地址并存盘（下次打开 MineDeck 卡片上还看得到）。
// 存不进去时内存里照样换成新地址：隧道确实连上了，地址是对的；只在日志和这条隧道的控制台里说一声，
// 不记成卡片上的红字（隧道是好的）。之后哪次保存成功，这个地址会一起写进去。
func (m *Manager) setPublicAddr(t *Tunnel, addr string) {
	m.mu.Lock()
	changed := t.PublicAddr != addr
	t.PublicAddr = addr
	var saveErr error
	if changed {
		saveErr = m.save()
	}
	m.mu.Unlock()
	// Console 自己要拿 m.mu，放到锁外
	if saveErr != nil {
		cause := saveCause(saveErr)
		log.Printf("隧道「%s」的公网地址没能存盘: %v", t.Name, cause)
		m.Console(t.ID).Append("[MineDeck] ⚠ 公网地址没能存进 tunnels.json（不影响这次运行；重启 MineDeck 前没存上的话，卡片上会显示上次的地址）。系统报错：" + cause.Error())
	}
	if changed {
		if t.IsVoice() {
			m.Console(t.ID).Append("[MineDeck] ✔ 语音公网地址: " + addr + "（MineDeck 会把它写进绑定服务器的 voice_host）")
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
const followActor = "跟随服务器"

// startDelay 实例启动后等多久再拉起跟随的隧道：等服务器进入启动流程。
var startDelay = 2 * time.Second

// pumpOutput 输出先脱敏再进控制台；原文仅用于状态识别，handleLineMasked 保证保存的文字也脱敏。
// 读到头（frpc 退出）才返回。
func (m *Manager) pumpOutput(t *Tunnel, rs *runState, con *procutil.Console, out io.Reader, mask func(string) string) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		con.Append(mask(line))
		m.handleLineMasked(t, rs, line, mask)
	}
}

// 短凭据可能碰巧出现在地址、时间里；命中的整行省略，避免替换位置反过来提示凭据。
const minMaskLen = 6

// credMasker 返回一个函数，把这次启动用的凭据从一行输出里遮掉。
//
// frpc 的输出原样进控制台缓冲，带「error」「失败」字样的行还会记成隧道卡片上的报错，局域网登录的设备两处都看得到；
// 而 OpenFrp、樱花的凭据是放在命令行参数里交给 frpc 的。frpc 会不会在报错里把它回显出来没有验证过，
// 所以不赌它不会：每一行进缓冲之前先遮掉，后面的控制台、卡片上的报错、事件都跟着干净（2026-11-14 Codex 复审第 6 条）。
// 除了原样的凭据，也遮它写进 frpc 配置时转义过的样子和放进网址时编码过的样子。
func credMasker(cred string) func(string) string {
	cred = strings.TrimSpace(cred)
	if cred == "" {
		return func(s string) string { return s }
	}
	forms := []string{cred}
	for _, f := range []string{tomlEscape(cred), url.QueryEscape(cred), url.PathEscape(cred)} {
		if !slices.Contains(forms, f) {
			forms = append(forms, f)
		}
	}
	// 长的先换：免得短的先换掉一截，长的就认不出来了
	slices.SortFunc(forms, func(a, b string) int { return len(b) - len(a) })
	return func(s string) string {
		if len(cred) < minMaskLen {
			hit := false
			for _, f := range forms {
				hit = hit || strings.Contains(s, f)
			}
			if !hit {
				return s
			}
			// 两种文字互作退路，提示本身也不能包含凭据；保留错误标志供卡片判断。
			low := strings.ToLower(s)
			failed := strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(low, "shutdown") || strings.Contains(low, "rejected") || strings.Contains(low, "invalid") || strings.Contains(s, "错误") || strings.Contains(s, "失败")
			choices := []string{"日志含凭据，已隐藏", "credential output hidden", "***", ""}
			if failed {
				choices = []string{"失败：日志含凭据，已隐藏", "error: credential output hidden", "***", ""}
			}
			for _, msg := range choices {
				clean := true
				for _, f := range forms {
					if strings.Contains(msg, f) {
						clean = false
						break
					}
				}
				if clean {
					return msg
				}
			}
		}
		for _, f := range forms {
			s = strings.ReplaceAll(s, f, "***")
		}
		return s
	}
}

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
	delay := startDelay // 在派发前固定本次等待时间，后台不再读可调整的默认值。
	// tunnels.json 开机时被占着、之后又没人打开隧道页：这里再读一次，读成了照常拉起跟随的隧道
	if err := m.LoadErr(); err != nil {
		m.noteFollowLoadErr(instance, err)
		return
	}
	for _, id := range m.bound(instance) {
		id := id
		go func() {
			time.Sleep(delay)
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

// noteFollowLoadErr 服务器启动了、tunnels.json 却还是没读成：不知道有哪些隧道跟着这台服务器，一条也拉不起来。
// 以前只记一行程序日志，事后在操作记录和服务器控制台里都看不出隧道为什么没起来（A59）。现在操作记录里记一条
// （没有隧道编号，参数只有实例名），服务器的控制台里也说一句。
func (m *Manager) noteFollowLoadErr(instance string, err error) {
	log.Printf("服务器「%s」启动了，但隧道配置没读出来，跟随它启动的隧道这次拉不起来: %v", instance, err)
	m.noteAuto(AutoAction{Op: "tunnel.start", Actor: followActor, Args: map[string]any{"inst": instance},
		Err: fmt.Errorf("隧道配置读不出来，这次没有拉起跟随启动的隧道。%w", err)})
	// 读不出来就不知道这台服务器有没有绑隧道，每台开服的都会收到这句，所以不说成一定有。
	// 原因用 LoadError 给人看的那句（短、不带路径和系统的英文原话，局域网里也看得到控制台），原文在上面的操作记录和程序日志里
	why := "隧道配置文件 tunnels.json 读不出来"
	if le := (*LoadError)(nil); errors.As(err, &le) {
		why = le.Hint()
	}
	m.sayToInstance(instance, "[MineDeck] 隧道配置读不出来，有隧道跟随这台服务器启动的话，这次没有拉起。"+why+
		"。处理好以后到「内网穿透」页手动启动隧道")
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
		grace := stopGrace
		go func() {
			time.Sleep(grace)
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
