// Package app 管理应用基础目录与全局配置。
package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"minedeck/internal/secret"
)

const Version = "2.9.0-beta.2"

var (
	Base        string // 程序所在目录（数据自包含于此，不写注册表、不动环境变量）
	RuntimesDir string // 便携式 Java 运行时
	CacheDir    string // 下载缓存
	ServersDir  string // 服务器实例
	BackupsDir  string // 世界备份（独立于实例目录，删实例后仍保留）
)

type Config struct {
	Source             string `json:"source"`                       // auto | mirror | official
	CFApiKey           string `json:"cfApiKey,omitempty"`           // 旧版明文 CurseForge API Key：启动时加密挪进 secrets.json 后清空，只有迁移失败时才留着（读写走 CFKey / SetCFKey）
	WebhookURL         string `json:"webhookUrl,omitempty"`         // 事件推送地址（选填）
	UpdateRepo         string `json:"updateRepo,omitempty"`         // GitHub 更新仓库，如 user/MineDeck
	CheckUpdateOnStart bool   `json:"checkUpdateOnStart,omitempty"` // 启动时后台静默检查更新
	ListenLAN          bool   `json:"listenLan,omitempty"`          // 允许局域网访问（需设置密码，重启生效）
	AccessPasswordHash string `json:"accessPasswordHash,omitempty"` // 远程访问密码哈希（bcrypt；兼容旧版 SHA-256 hex，登录成功自动升级）
	MinimizeToTray     bool   `json:"minimizeToTray,omitempty"`     // 关闭窗口时最小化到托盘（服务器不停），默认关

	ServersDir  string   `json:"serversDir,omitempty"`  // 自定义实例存放根目录（空=内置 servers/），仅对新建生效
	BackupsDir  string   `json:"backupsDir,omitempty"`  // 自定义备份根目录（空=内置 backups/）
	Roots       []string `json:"roots,omitempty"`       // 曾放置实例或曾设为默认的根目录（多根扫描用，内置根除外）
	BackupKeep  int      `json:"backupKeep,omitempty"`  // 每实例保留的备份份数（0=默认 10）
	BackupRoots []string `json:"backupRoots,omitempty"` // 曾设为备份根的目录（旧备份照常列出、还原，内置根除外）
	Onboarded   bool     `json:"onboarded,omitempty"`   // 首次运行引导卡是否已完成
	TosAccepted bool     `json:"tosAccepted,omitempty"` // 用户协议与免责声明是否已同意（未同意则每次启动重新弹引导卡）

	// ChromeColor 界面标题栏的底色（#rrggbb，小写），由原生窗口里的前端在外观变化时上报；
	// 下次启动时作为 WebView2 画出页面之前的默认底色，避免冷启动白闪。空 = 按系统浅 / 深色取默认值。
	ChromeColor string `json:"chromeColor,omitempty"`
}

// ParseHexColor 校验并规范化 #rrggbb 形式的颜色，返回小写形式；其它写法（#rgb、颜色名、rgb() 等）一律不认。
func ParseHexColor(s string) (string, bool) {
	if len(s) != 7 || s[0] != '#' {
		return "", false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return "", false
		}
	}
	return strings.ToLower(s), true
}

// DefaultBackupKeep 未配置时每实例保留的备份份数。
const DefaultBackupKeep = 10

// DefaultUpdateRepo 内置自动更新源：用户未在「设置 → 更新」另填时使用，
// 让 GitHub 自动更新开箱即用（仍需用户手动点检查或开启启动时检查，不主动联网）。
const DefaultUpdateRepo = "Trasperioooooon/MineDeck"

var (
	cfgMu sync.RWMutex
	cfg   = Config{Source: "auto", UpdateRepo: DefaultUpdateRepo}
)

func Init() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	base := filepath.Dir(exe)
	// go run 场景下可执行文件在临时目录，退回工作目录
	if strings.Contains(base, "go-build") || strings.HasPrefix(base, os.TempDir()) {
		if wd, werr := os.Getwd(); werr == nil {
			base = wd
		}
	}
	Base = base
	RuntimesDir = filepath.Join(base, "runtimes")
	CacheDir = filepath.Join(base, "cache")
	ServersDir = filepath.Join(base, "servers")
	BackupsDir = filepath.Join(base, "backups")
	for _, d := range []string{RuntimesDir, CacheDir, ServersDir, BackupsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	loadConfig()
	migrateCFKey()
	return nil
}

func configPath() string { return filepath.Join(Base, "config.json") }

func loadConfig() {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	var c Config
	if json.Unmarshal(b, &c) == nil && c.Source != "" {
		if c.UpdateRepo == "" {
			c.UpdateRepo = DefaultUpdateRepo // 老配置未填过更新仓库时补上内置源
		}
		cfgMu.Lock()
		cfg = c
		cfgMu.Unlock()
	}
}

func GetConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

// UpdateConfig 在同一把锁内完成配置的读-改-写并持久化，避免并发处理器各自
// GetConfig→改→整体覆盖时互相丢更新。mutate 返回错误则放弃本次修改（配置不变、不落盘）。
func UpdateConfig(mutate func(*Config) error) (Config, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := cfg
	if err := mutate(&c); err != nil {
		return cfg, err
	}
	if c.Source != "mirror" && c.Source != "official" {
		c.Source = "auto"
	}
	// 默认存放根被改掉（含恢复默认）时旧根并入 Roots：其中的实例重启后仍被扫描，原地可用
	if c.ServersDir != cfg.ServersDir {
		addRoot(&c, cfg.ServersDir)
	}
	// 备份根同理并入 BackupRoots：其中的备份照常列出、可还原，并计入保留份数
	if c.BackupsDir != cfg.BackupsDir {
		addBackupRoot(&c, cfg.BackupsDir)
	}
	cfg = c
	saveLocked()
	return c, nil
}

// saveLocked 持久化当前配置，调用方须已持有 cfgMu。
func saveLocked() {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(configPath(), b, 0o644)
}

// ---------- 密钥（secrets.json，DPAPI 加密） ----------

const cfKeyName = "cf.key" // secrets.json 里 CurseForge API Key 的键名（AI 的 Key 叫 ai.key，见 internal/ai）

// Secrets 程序目录下的 secrets.json（DPAPI 加密的密钥都放这里）。
func Secrets() *secret.Store { return secret.NewStore(filepath.Join(Base, "secrets.json")) }

// CFKey 返回 CurseForge API Key 的明文；没设置或解不开时返回空串。
// config.json 里的旧明文只在迁移没成功时才会还在，这时照旧用它。
func CFKey() string {
	if k := GetConfig().CFApiKey; k != "" {
		return k
	}
	k, _ := Secrets().Get(cfKeyName)
	return k
}

// CFKeyState 返回 CurseForge API Key 是否已设置，以及是否解不开（换了电脑或 Windows 用户，需要重填）。
func CFKeyState() (set, unreadable bool) {
	if GetConfig().CFApiKey != "" {
		return true, false
	}
	return Secrets().State(cfKeyName)
}

// SetCFKey 加密保存 CurseForge API Key（空串表示清除），并清掉 config.json 里可能残留的旧明文。
func SetCFKey(k string) error {
	if err := Secrets().Set(cfKeyName, strings.TrimSpace(k)); err != nil {
		return err
	}
	if GetConfig().CFApiKey != "" {
		_, err := UpdateConfig(func(c *Config) error { c.CFApiKey = ""; return nil })
		return err
	}
	return nil
}

// migrateCFKey 启动时把 config.json 里的明文 cfApiKey 加密挪进 secrets.json，再从 config.json 里删掉。
// 加密失败就保持原样（CFKey 照旧读明文），只记日志。
func migrateCFKey() {
	k := GetConfig().CFApiKey
	if k == "" {
		return
	}
	if err := Secrets().Set(cfKeyName, k); err != nil {
		log.Println("CurseForge API Key 加密失败，暂时保留 config.json 里的明文:", err)
		return
	}
	_, _ = UpdateConfig(func(c *Config) error { c.CFApiKey = ""; return nil })
	log.Println("已把 config.json 里的 CurseForge API Key 加密挪进 secrets.json")
}

func MirrorFirst() bool  { return GetConfig().Source != "official" }
func OfficialOnly() bool { return GetConfig().Source == "official" }
func MirrorOnly() bool   { return GetConfig().Source == "mirror" }

// ServersRoot 返回新实例的默认存放根目录（配置优先，否则程序内置 servers/）。
func ServersRoot() string {
	if d := GetConfig().ServersDir; d != "" {
		return filepath.Clean(d)
	}
	return ServersDir
}

// BackupKeep 返回每实例保留的备份份数（配置优先，缺省 DefaultBackupKeep，范围 1~1000）。
func BackupKeep() int {
	k := GetConfig().BackupKeep
	if k <= 0 {
		return DefaultBackupKeep
	}
	if k > 1000 {
		return 1000
	}
	return k
}

// BackupsRoot 返回备份根目录（配置优先，否则程序内置 backups/），新备份都写在这里。
func BackupsRoot() string {
	if d := GetConfig().BackupsDir; d != "" {
		return filepath.Clean(d)
	}
	return BackupsDir
}

// AllBackupRoots 返回查找已有备份的全部根目录（去重、保持顺序）：当前备份根在前，
// 其后是内置根与改目录前用过的旧根。
func AllBackupRoots() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d == "" {
			return
		}
		c := filepath.Clean(d)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	c := GetConfig()
	add(c.BackupsDir) // 配置的备份根（空则下一行的内置根就是当前根）
	add(BackupsDir)   // 内置默认根
	for _, r := range c.BackupRoots {
		add(r) // 改目录前用过的旧根
	}
	return out
}

// addBackupRoot 把 dir 并入 c.BackupRoots（filepath.Clean 后去重，空串与内置备份根跳过：
// 内置根总在 AllBackupRoots 里）。调用方须已持有 cfgMu。
func addBackupRoot(c *Config, dir string) {
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	if dir == filepath.Clean(BackupsDir) {
		return
	}
	for _, r := range c.BackupRoots {
		if filepath.Clean(r) == dir {
			return
		}
	}
	c.BackupRoots = append(c.BackupRoots, dir)
}

// InstanceRoots 返回所有需要扫描实例的根目录（去重、保持顺序）。
func InstanceRoots() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d == "" {
			return
		}
		c := filepath.Clean(d)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	c := GetConfig()
	add(ServersDir)   // 内置默认根
	add(c.ServersDir) // 配置的默认根
	for _, r := range c.Roots {
		add(r) // 历史自定义根
	}
	return out
}

// RememberRoot 记录一个曾放置实例的根，便于下次启动扫描发现。
// 只有内置默认根始终被扫描、无需记录；当前配置根也要记，之后改目录或恢复默认时它就不再被扫描。
func RememberRoot(dir string) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if addRoot(&cfg, dir) {
		saveLocked()
	}
}

// addRoot 把 dir 并入 c.Roots（filepath.Clean 后去重，空串与内置默认根跳过），返回是否新增。
// 调用方须已持有 cfgMu。
func addRoot(c *Config, dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	if dir == filepath.Clean(ServersDir) {
		return false
	}
	for _, r := range c.Roots {
		if filepath.Clean(r) == dir {
			return false
		}
	}
	c.Roots = append(c.Roots, dir)
	return true
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// TotalRAMMB 返回物理内存总量（MB），失败时返回保守值。
func TotalRAMMB() int {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 8192
	}
	return int(m.TotalPhys / (1024 * 1024))
}

// AvailRAMMB 返回当前可用物理内存（MB）；调用失败时返回 0（表示未知，
// 与 TotalRAMMB 的保守兜底 8192 区分，便于前端据 0 走降级逻辑）。
func AvailRAMMB() int {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0
	}
	return int(m.AvailPhys / (1024 * 1024))
}

// OSVersion Windows 的版本号，如「Windows 10.0.26300」（Win11 的主版本号也是 10，按内部版本号区分：22000 起是 Win11）。
func OSVersion() string {
	v := windows.RtlGetVersion()
	name := "Windows"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	} else if v.MajorVersion == 10 {
		name = "Windows 10"
	}
	return fmt.Sprintf("%s（%d.%d.%d）", name, v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
