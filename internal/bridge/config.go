package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ConfigRel 桥接模组的配置文件，相对实例目录（服务端的工作目录，和 SVC 的 config/voicechat/ 同一层）。
const ConfigRel = "config/minedeck/voicebridge.properties"

const (
	DefaultPort    = 24470 // 模组的默认端口
	MinTokenLength = 16    // 模组要求令牌至少这么长
	maxExclude     = 512
)

// Config 模组的配置（README「配置文件」一节）。令牌不下发给前端。
type Config struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"` // 写错（不是数字）时为 -1
	Token    string `json:"-"`
	Exclude  string `json:"exclude"` // 不推送的玩家：名字或 UUID，逗号隔开
	Selftest bool   `json:"selftest"`
	Bypass   bool   `json:"bypassGroupIsolation"`
}

// ConfigPath 配置文件的完整路径。
func ConfigPath(instDir string) string {
	return filepath.Join(instDir, filepath.FromSlash(ConfigRel))
}

// ReadConfig 读模组的配置；文件不存在时返回的错误满足 os.IsNotExist。规则和模组的 BridgeConfig.load 一样。
func ReadConfig(instDir string) (Config, error) {
	p, err := LoadProps(ConfigPath(instDir))
	if err != nil {
		return Config{}, err
	}
	return configFrom(p), nil
}

func configFrom(p *Props) Config {
	get := func(k string) string { v, _ := p.Get(k); return strings.TrimSpace(v) }
	c := Config{
		Enabled:  parseBool(get("enabled"), false),
		Port:     DefaultPort,
		Token:    get("token"),
		Exclude:  get("exclude"),
		Selftest: parseBool(get("selftest"), false),
		Bypass:   parseBool(get("bypass_group_isolation"), true),
	}
	if s := get("port"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			c.Port = n
		} else {
			c.Port = -1
		}
	}
	return c
}

func parseBool(s string, def bool) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return def
}

// Problem 模组打不开端口的原因（和模组的判断一样）；没有问题时为空。
func (c Config) Problem() string {
	switch {
	case !c.Enabled:
		return "配置里 enabled 不是 true"
	case c.Port < 1024 || c.Port > 65535:
		return "port 必须是 1024 到 65535 之间的数字"
	case len(c.Token) < MinTokenLength:
		return fmt.Sprintf("token 至少要 %d 个字符", MinTokenLength)
	}
	return ""
}

// ExcludeNames 拆开排除名单（和模组一样，中英文逗号、分号、空白都算分隔）。
func ExcludeNames(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || unicode.IsSpace(r)
	})
}

// NormalizeExclude 校验并规范化排除名单：去掉多余的分隔符，用英文逗号连起来。
// 名字里不能有反斜杠、= # 这类会让配置文件出错的字符（离线模式的名字五花八门，其余字符都放行）。
func NormalizeExclude(s string) (string, error) {
	names := ExcludeNames(s)
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if utf8.RuneCountInString(n) > 36 { // 36 = 带横线的 UUID
			return "", fmt.Errorf("「%s」太长了：玩家名最多 16 个字符，UUID 36 个", n)
		}
		for _, r := range n {
			if r == '\\' || r == '=' || r == '#' || r == '!' || r == ':' || unicode.IsControl(r) {
				return "", fmt.Errorf("「%s」里有不能用的字符 %q", n, r)
			}
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	joined := strings.Join(out, ",")
	if len(joined) > maxExclude {
		return "", errors.New("排除名单太长了")
	}
	return joined, nil
}

// EnsureOptions EnsureConfig 的参数。
type EnsureOptions struct {
	Exclude *string        // 非 nil 时同时改排除名单（已规范化）
	Avoid   func(int) bool // 选端口时要避开的（别的实例已经用了的）；nil 不避
	canBind func(int) bool // 测试用：端口现在能不能绑定
}

// EnsureConfig 保证模组会打开端口：enabled=true、端口在 1024–65535、令牌够长，缺的补上（端口优先从 24470 起
// 找一个现在空着、别的实例也没用的，令牌随机生成）。文件不存在就新建一份带说明的；已有的只改这几行，
// 其余原样保留。返回最终的配置和文件有没有改动。
func EnsureConfig(instDir string, o EnsureOptions) (Config, bool, error) {
	path := ConfigPath(instDir)
	p, err := LoadProps(path)
	fresh := false
	switch {
	case os.IsNotExist(err):
		p, fresh = newConfigProps(), true
	case err != nil:
		return Config{}, false, err
	}
	c := configFrom(p)
	changed := fresh
	set := func(k, v string) {
		if p.Set(k, v) {
			changed = true
		}
	}
	if !c.Enabled {
		set("enabled", "true")
	}
	// 没写端口、写错了，或者和别的实例撞了（模组生成的模板都是 24470），换一个
	if _, ok := p.Get("port"); !ok || c.Port < 1024 || c.Port > 65535 || (o.Avoid != nil && o.Avoid(c.Port)) {
		set("port", strconv.Itoa(pickPort(o)))
	}
	if len(c.Token) < MinTokenLength {
		set("token", RandomToken())
	}
	switch {
	case o.Exclude != nil:
		set("exclude", *o.Exclude)
	case fresh:
		set("exclude", "")
	}
	if fresh {
		set("selftest", "false")
		set("bypass_group_isolation", "true")
	}
	if changed {
		if err := WriteFileAtomic(path, p.Bytes()); err != nil {
			return Config{}, false, err
		}
	}
	return configFrom(p), changed, nil
}

// SetExclude 只改排除名单（文件不存在时不新建，返回 os.ErrNotExist）。返回有没有改动。
func SetExclude(instDir, exclude string) (bool, error) {
	path := ConfigPath(instDir)
	p, err := LoadProps(path)
	if err != nil {
		return false, err
	}
	if !p.Set("exclude", exclude) {
		return false, nil
	}
	return true, WriteFileAtomic(path, p.Bytes())
}

func newConfigProps() *Props {
	p := &Props{crlf: true} // Windows 上用记事本打开也正常
	p.Comment("MineDeck 音乐共享桥接模组（minedeck_voicebridge）的配置，由 MineDeck 写入。")
	p.Comment("改了以后约 2 秒内生效，不用重启服务器。各项的说明见 MineDeck 仓库的 bridge/voicechat/README.md。")
	p.Comment("enabled：是否在 127.0.0.1 上打开音乐共享端口；port：端口；token：共享令牌（只给 MineDeck 用，不要外传）。")
	p.Comment("exclude：不推送音乐的玩家，填名字或 UUID，逗号隔开。广播的人自己也在游戏里时填自己，免得听两遍。")
	return p
}

// RandomToken 32 位十六进制的随机令牌。
func RandomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // 系统随机数不可用，没法继续
	}
	return hex.EncodeToString(b)
}

// pickPort 从 24470 起找一个能绑定、别的实例也没用的端口；都不行就让系统给一个。
// 不直接用系统随机给的：Windows 的动态端口（49152 起）随时会被别的出站连接占走，模组下次开服就绑不上了。
func pickPort(o EnsureOptions) int {
	canBind := o.canBind
	if canBind == nil {
		canBind = canBindLocal
	}
	for p := DefaultPort; p < DefaultPort+100; p++ {
		if (o.Avoid == nil || !o.Avoid(p)) && canBind(p) {
			return p
		}
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		defer ln.Close()
		return ln.Addr().(*net.TCPAddr).Port
	}
	return DefaultPort
}

func canBindLocal(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
