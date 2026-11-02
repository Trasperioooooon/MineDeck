package companion

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"minedeck/internal/bridge"
)

// ConfigRel 伙伴模组的配置文件，相对实例目录（服务端的工作目录，和音乐桥接的 voicebridge.properties 同一层）。
const ConfigRel = "config/minedeck/companion.properties"

// 配置文件的默认值和范围（README「配置文件」一节）。
const (
	DefaultPort          = 24480
	DefaultMaxCompanions = 4
	MaxCompanionsLimit   = 16
	MaxZones             = 64
	MaxAttackDeny        = 64
	zonePrefix           = "nodig."
)

// DefaultAttackDeny attack 默认不许打的生物种类（配置里没写 attack_deny 时）。
var DefaultAttackDeny = []string{"minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:allay"}

// Config companion.properties 的内容。令牌不下发给前端。
type Config struct {
	Exists        bool     `json:"exists"` // 文件存在（模组第一次启动时生成一份端口关闭的模板）
	Enabled       bool     `json:"enabled"`
	Port          int      `json:"port"` // 写错（不是数字）时为 -1
	Token         string   `json:"-"`
	MaxCompanions int      `json:"maxCompanions"`
	AttackDeny    []string `json:"attackDeny"`         // 实际生效的：没写这一行时是默认的那几种
	DenyDefault   bool     `json:"attackDenyDefault"`  // 没写 attack_deny（用默认）
	Zones         []Zone   `json:"zones"`              // 按名字排序
	Problems      []string `json:"problems,omitempty"` // MineDeck 读的时候发现写错、模组会跳过的条目
}

// ConfigPath 配置文件的完整路径。
func ConfigPath(instDir string) string { return filepath.Join(instDir, filepath.FromSlash(ConfigRel)) }

// ReadConfig 读配置；文件不存在时返回默认值（Exists 为 false），不算错误。规则和模组读的时候一样：
// enabled、port、token 写错时模组不开端口（见 Problem）；其余的键写错只是那一项不生效。
func ReadConfig(instDir string) (Config, error) {
	p, err := bridge.LoadProps(ConfigPath(instDir))
	if os.IsNotExist(err) {
		return configFrom(nil), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := configFrom(p)
	c.Exists = true
	return c, nil
}

func configFrom(p *bridge.Props) Config {
	c := Config{Port: DefaultPort, MaxCompanions: DefaultMaxCompanions, AttackDeny: slices.Clone(DefaultAttackDeny), DenyDefault: true, Zones: []Zone{}}
	if p == nil {
		return c
	}
	get := func(k string) (string, bool) { v, ok := p.Get(k); return strings.TrimSpace(v), ok }
	if v, _ := get("enabled"); strings.EqualFold(v, "true") {
		c.Enabled = true
	}
	if v, ok := get("port"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Port = n
		} else {
			c.Port = -1
		}
	}
	c.Token, _ = get("token")
	if v, ok := get("max_companions"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= MaxCompanionsLimit {
			c.MaxCompanions = n
		} else {
			c.Problems = append(c.Problems, fmt.Sprintf("max_companions=%s 不对（要是 1–%d），按默认的 %d 个", v, MaxCompanionsLimit, DefaultMaxCompanions))
		}
	}
	if v, ok := get("attack_deny"); ok {
		c.DenyDefault = false
		c.AttackDeny = []string{}
		for _, id := range splitList(v) {
			n, err := NormalizeEntityID(id)
			if err != nil {
				c.Problems = append(c.Problems, "attack_deny 里的 "+id+" 不对，跳过")
				continue
			}
			if !slices.Contains(c.AttackDeny, n) {
				c.AttackDeny = append(c.AttackDeny, n)
			}
		}
	}
	for _, k := range p.Keys() {
		name, ok := strings.CutPrefix(k, zonePrefix)
		if !ok {
			continue
		}
		v, _ := get(k)
		z, err := ParseZone(name, v)
		if err != nil {
			c.Problems = append(c.Problems, fmt.Sprintf("禁挖区域 %s 不对，模组会跳过它：%v", name, err))
			continue
		}
		if len(c.Zones) >= MaxZones {
			c.Problems = append(c.Problems, fmt.Sprintf("禁挖区域最多 %d 个，%s 被跳过", MaxZones, name))
			continue
		}
		c.Zones = append(c.Zones, z)
	}
	sort.Slice(c.Zones, func(i, j int) bool { return c.Zones[i].Name < c.Zones[j].Name })
	return c
}

// Problem 模组打不开控制端口的原因（和模组的判断一样）；没有问题时为空。
func (c Config) Problem() string {
	switch {
	case !c.Exists:
		return "还没有伙伴模组的配置文件 " + ConfigRel
	case !c.Enabled:
		return "配置里 enabled 不是 true"
	case c.Port < 1024 || c.Port > 65535:
		return "port 要是 1024 到 65535 之间的数字"
	case !ValidToken(c.Token):
		return fmt.Sprintf("token 要是 %d–%d 个可见的 ASCII 字符", MinTokenLength, MaxTokenLength)
	}
	return ""
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\t' })
}

var (
	zoneNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	dimRe      = regexp.MustCompile(`^[a-z0-9_.-]+:[a-z0-9_./-]+$`)
	entityRe   = regexp.MustCompile(`^(?:[a-z0-9_.-]+:)?[a-z0-9_./-]+$`)
)

// 坐标的范围（同模组对 goto 的检查）。
const (
	maxXZ = 30000000
	minY  = -2048
	maxY  = 2048
)

// ValidZoneName 禁挖区域的名字：1–32 个字母、数字、_、-。
func ValidZoneName(n string) bool { return zoneNameRe.MatchString(n) }

// NormalizeEntityID 生物种类 ID：小写字母、数字和 _ . - / 加命名空间；原版的可以省 minecraft:。
func NormalizeEntityID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if len(id) > 100 || !entityRe.MatchString(id) {
		return "", fmt.Errorf("「%s」不是生物种类的 ID（如 minecraft:villager）", id)
	}
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	return id, nil
}

// ParseZone 解析一行 nodig.<名字>=<维度> x1 y1 z1 x2 y2 z2（空格或逗号隔开）。两个角照写的顺序存。
func ParseZone(name, v string) (Zone, error) {
	if !ValidZoneName(name) {
		return Zone{}, errors.New("名字只能是 1–32 个字母、数字、_、-")
	}
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(f) != 7 {
		return Zone{}, errors.New("要写成「维度 x1 y1 z1 x2 y2 z2」")
	}
	z := Zone{Name: name, Dim: f[0]}
	for i := 0; i < 6; i++ {
		n, err := strconv.Atoi(f[i+1])
		if err != nil {
			return Zone{}, fmt.Errorf("坐标 %q 不是整数", f[i+1])
		}
		if i < 3 {
			z.Min[i] = n
		} else {
			z.Max[i-3] = n
		}
	}
	return z, z.Check()
}

// Check 检查一个区域：名字、维度 ID、坐标范围。
func (z Zone) Check() error {
	switch {
	case !ValidZoneName(z.Name):
		return fmt.Errorf("区域名「%s」只能是 1–32 个字母、数字、_、-", z.Name)
	case !dimRe.MatchString(z.Dim) || len(z.Dim) > 100:
		return fmt.Errorf("维度「%s」要写完整的 ID，比如 minecraft:overworld", z.Dim)
	}
	for _, c := range [][3]int{z.Min, z.Max} {
		if c[0] < -maxXZ || c[0] > maxXZ || c[2] < -maxXZ || c[2] > maxXZ {
			return fmt.Errorf("区域「%s」的 x、z 要在 ±%d 以内", z.Name, maxXZ)
		}
		if c[1] < minY || c[1] > maxY {
			return fmt.Errorf("区域「%s」的 y 要在 %d 到 %d 之间", z.Name, minY, maxY)
		}
	}
	return nil
}

func (z Zone) value() string {
	return fmt.Sprintf("%s %d %d %d %d %d %d", z.Dim, z.Min[0], z.Min[1], z.Min[2], z.Max[0], z.Max[1], z.Max[2])
}

// EnsureOptions EnsureConfig 的参数。
type EnsureOptions struct {
	Avoid   func(int) bool // 选端口时要避开的（别的实例已经用了的）；nil 不避
	canBind func(int) bool // 测试用：端口现在能不能绑定
}

// EnsureConfig 保证模组会打开控制端口：enabled=true、端口在 1024–65535、令牌合规，缺的补上（端口优先从 24480 起找一个
// 现在空着、别的实例也没用的；令牌随机生成 32 位十六进制）。文件不存在就新建一份带说明的；已有的只改这几行，其余原样保留。
// 先写临时文件再改名（模组每 2 秒读一次，不会读到写了一半的文件）。返回最终的配置和文件有没有改动。
func EnsureConfig(instDir string, o EnsureOptions) (Config, bool, error) {
	return edit(instDir, func(p *bridge.Props, c Config, fresh bool) error {
		if !c.Enabled {
			p.Set("enabled", "true")
		}
		if _, ok := p.Get("port"); !ok || c.Port < 1024 || c.Port > 65535 || (o.Avoid != nil && o.Avoid(c.Port)) {
			p.Set("port", strconv.Itoa(pickPort(o)))
		}
		if !ValidToken(c.Token) {
			p.Set("token", bridge.RandomToken())
		}
		return nil
	})
}

// Disable 把 enabled 改成 false（模组断开当前连接、关掉端口）。文件不存在时什么都不做。
func Disable(instDir string) (bool, error) {
	if _, err := os.Stat(ConfigPath(instDir)); os.IsNotExist(err) {
		return false, nil
	}
	_, changed, err := edit(instDir, func(p *bridge.Props, c Config, _ bool) error {
		if c.Enabled {
			p.Set("enabled", "false")
		}
		return nil
	})
	return changed, err
}

// Patch 改设置：只改传了的字段。
type Patch struct {
	MaxCompanions *int      `json:"maxCompanions"`
	AttackDeny    *[]string `json:"attackDeny"`  // 整体替换；空数组表示没有额外限制（写成 attack_deny=）
	DenyDefault   bool      `json:"denyDefault"` // 恢复默认（删掉 attack_deny 这一行）；和 AttackDeny 一起传时以它为准
	Zones         *[]Zone   `json:"zones"`       // 整体替换
}

// Check 检查改动，返回规范化以后的（attack_deny 补上命名空间、去重；区域按名字排序、名字不能重复）。
func (pt Patch) Check() (Patch, error) {
	if pt.MaxCompanions != nil && (*pt.MaxCompanions < 1 || *pt.MaxCompanions > MaxCompanionsLimit) {
		return pt, fmt.Errorf("伙伴上限要在 1 到 %d 之间", MaxCompanionsLimit)
	}
	if pt.AttackDeny != nil && !pt.DenyDefault {
		var out []string
		for _, id := range *pt.AttackDeny {
			if strings.TrimSpace(id) == "" {
				continue
			}
			n, err := NormalizeEntityID(id)
			if err != nil {
				return pt, err
			}
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
		if len(out) > MaxAttackDeny {
			return pt, fmt.Errorf("不许打的生物最多 %d 种", MaxAttackDeny)
		}
		if out == nil {
			out = []string{}
		}
		pt.AttackDeny = &out
	}
	if pt.Zones != nil {
		zs := slices.Clone(*pt.Zones)
		if len(zs) > MaxZones {
			return pt, fmt.Errorf("禁挖区域最多 %d 个", MaxZones)
		}
		seen := map[string]bool{}
		for i := range zs {
			zs[i].Name, zs[i].Dim = strings.TrimSpace(zs[i].Name), strings.TrimSpace(zs[i].Dim)
			if err := zs[i].Check(); err != nil {
				return pt, err
			}
			k := strings.ToLower(zs[i].Name)
			if seen[k] {
				return pt, fmt.Errorf("有两个禁挖区域都叫「%s」", zs[i].Name)
			}
			seen[k] = true
		}
		sort.Slice(zs, func(i, j int) bool { return zs[i].Name < zs[j].Name })
		if zs == nil {
			zs = []Zone{}
		}
		pt.Zones = &zs
	}
	return pt, nil
}

// Update 按 Patch 改配置文件（只改相关的行，注释和别的键原样保留；文件不存在就新建，端口照旧关着）。
func Update(instDir string, pt Patch) (Config, error) {
	pt, err := pt.Check()
	if err != nil {
		return Config{}, err
	}
	c, _, err := edit(instDir, func(p *bridge.Props, _ Config, _ bool) error {
		if pt.MaxCompanions != nil {
			p.Set("max_companions", strconv.Itoa(*pt.MaxCompanions))
		}
		switch {
		case pt.DenyDefault:
			p.Delete("attack_deny")
		case pt.AttackDeny != nil:
			p.Set("attack_deny", strings.Join(*pt.AttackDeny, ","))
		}
		if pt.Zones != nil {
			for _, k := range p.Keys() {
				if strings.HasPrefix(k, zonePrefix) {
					p.Delete(k)
				}
			}
			for _, z := range *pt.Zones {
				p.Set(zonePrefix+z.Name, z.value())
			}
		}
		return nil
	})
	return c, err
}

// edit 读配置文件（不存在就建一份带说明的），交给 f 改，变了才写回（先写临时文件再改名）。
func edit(instDir string, f func(p *bridge.Props, c Config, fresh bool) error) (Config, bool, error) {
	path := ConfigPath(instDir)
	before, err := os.ReadFile(path)
	fresh := os.IsNotExist(err)
	if err != nil && !fresh {
		return Config{}, false, err
	}
	p := bridge.ParseProps(before)
	if fresh {
		p = newConfigProps()
	}
	cur := configFrom(p)
	cur.Exists = !fresh
	if err := f(p, cur, fresh); err != nil {
		return Config{}, false, err
	}
	out := p.Bytes()
	changed := fresh || string(out) != string(before)
	if changed {
		if err := bridge.WriteFileAtomic(path, out); err != nil {
			return Config{}, false, err
		}
	}
	c := configFrom(p)
	c.Exists = true
	return c, changed, nil
}

func newConfigProps() *bridge.Props {
	p := bridge.NewProps(true) // Windows 换行：用记事本打开也正常
	p.Comment("MineDeck AI 伙伴模组（minedeck_companion）的配置，由 MineDeck 写入。改了以后约 2 秒内生效，不用重启服务器。")
	p.Comment("各项的说明见 MineDeck 仓库 companion/README.md 的「配置文件」。")
	p.Comment("enabled：是否在 127.0.0.1 上打开控制端口；port：端口；token：控制令牌（只给 MineDeck 用，不要外传）。")
	p.Comment("max_companions：同时最多几个伙伴；attack_deny：attack 不许打的生物；nodig.<名字>：禁挖区域（维度 x1 y1 z1 x2 y2 z2）。")
	return p
}

// pickPort 从 24480 起找一个能绑定、别的实例也没用的端口；都不行就让系统给一个。
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
