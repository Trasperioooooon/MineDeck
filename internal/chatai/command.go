package chatai

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// 命令白名单（设计文档 §6.6）：AI 只能执行用户在界面上勾选的命令模板，每个参数按模板校验；
// 禁止清单写死在代码里，界面也打不开。模型输出什么都没用：不在白名单里的一律不执行。

// Template 一个命令模板。
type Template struct {
	ID      string `json:"id"`
	Title   string `json:"title"`   // 界面上的名字
	Usage   string `json:"usage"`   // 写法（界面和给 AI 的说明都用它；<自己> 是提问的人）
	Limit   string `json:"limit"`   // 限制
	Default bool   `json:"default"` // 默认开不开
	Does    string `json:"-"`       // /帮助 里「能让 AI 做的事」用的短语

	heads []string                                                      // 认哪些命令名
	check func(c *cmdCtx, head string, args []string) ([]string, error) // 校验参数，返回规范化后的参数（「自己」已换成名字）
}

// Templates 全部命令模板，按界面上的顺序。默认开关照 §6.6（gamemode、give 默认关，已定）。
var Templates = []*Template{
	{ID: "time", Title: "改时间、查时间", Usage: "time set day|night|noon|midnight；time query daytime|gametime|day", Limit: "只允许这些值", Default: true,
		Does: "改时间", heads: []string{"time"}, check: checkTime},
	{ID: "weather", Title: "改天气", Usage: "weather clear|rain|thunder [秒]", Limit: "秒数 ≤ 6000", Default: true,
		Does: "改天气", heads: []string{"weather"}, check: checkWeather},
	{ID: "tp", Title: "传送自己", Usage: "tp <自己> <在线玩家>；tp <自己> x y z", Limit: "只能传送自己", Default: true,
		Does: "把你传送到别人身边或某个坐标", heads: []string{"tp", "teleport"}, check: checkTP},
	{ID: "effect", Title: "给自己加状态效果", Usage: "effect give <自己> <效果> [秒] [等级]；effect clear <自己>", Limit: "秒 ≤ 600，等级 ≤ 4", Default: true,
		Does: "给你加或清除状态效果", heads: []string{"effect"}, check: checkEffect},
	{ID: "spawnpoint", Title: "设出生点", Usage: "spawnpoint <自己>", Limit: "只能设自己的，设在现在站的地方", Default: true,
		Does: "把你的出生点设在你现在的位置", heads: []string{"spawnpoint"}, check: checkSpawnpoint},
	{ID: "locate", Title: "找结构和生物群系、看谁在线", Usage: "locate structure|biome <ID>；list", Limit: "只读", Default: true,
		Does: "找最近的结构和生物群系、看谁在线", heads: []string{"locate", "locatebiome", "list"}, check: checkLocate},
	{ID: "gamemode", Title: "改自己的游戏模式", Usage: "gamemode survival|creative|adventure|spectator <自己>", Limit: "只能改自己的（含创造模式）", Default: false,
		Does: "改你的游戏模式", heads: []string{"gamemode"}, check: checkGamemode},
	{ID: "give", Title: "给自己物品", Usage: "give <自己> <物品> [数量]", Limit: "数量 ≤ 64；模组物品的 ID 没法提前校验，交给服务器报错", Default: false,
		Does: "给你物品", heads: []string{"give"}, check: checkGive},
	{ID: "difficulty", Title: "改难度", Usage: "difficulty peaceful|easy|normal|hard", Limit: "影响所有人", Default: false,
		Does: "改难度（影响所有人）", heads: []string{"difficulty"}, check: checkDifficulty},
	{ID: "seed", Title: "看种子", Usage: "seed", Limit: "只读", Default: false,
		Does: "看世界种子", heads: []string{"seed"}, check: checkSeed},
}

// TemplateByID 按编号取模板。
func TemplateByID(id string) *Template {
	for _, t := range Templates {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Forbidden 永远禁止的命令（§6.6，写死在代码里，界面也打不开）：比设计稿多列了几条同样危险的（放置结构、改生物群系、
// 给战利品、造成伤害、改世界出生点、默认游戏模式），以及会冒充别人说话的（tellraw、say、msg 这些），还有各家加载器和插件端的命令。
// 不在模板里的命令本来就一律不执行，这张表让拒绝的原因说得更清楚，也防止以后加模板时误放。
var Forbidden = []string{
	"op", "deop", "ban", "ban-ip", "pardon", "pardon-ip", "kick", "stop", "restart", "reload", "whitelist",
	"execute", "function", "schedule", "datapack", "forceload", "save-off", "save-on", "save-all", "setidletimeout",
	"transfer", "tick", "debug", "perf", "jfr", "publish", "kill", "clear", "fill", "clone", "setblock", "summon",
	"data", "item", "attribute", "scoreboard", "team", "gamerule", "worldborder", "spreadplayers", "ride",
	// 设计稿以外、同样危险的
	"place", "fillbiome", "loot", "damage", "setworldspawn", "defaultgamemode", "return",
	"tellraw", "title", "say", "me", "msg", "tell", "w", "teammsg", "tm", "bossbar", "trigger",
	// 加载器、插件端的命令
	"forge", "neoforge", "fabric", "quilt", "mohist", "banner", "bukkit", "spigot", "paper", "purpur", "folia", "leaves",
	"sponge", "velocity", "plugins", "pl", "version", "ver", "about", "timings", "icanhasbukkit",
}

// MaxCommandLen 一条命令最长多少个字符（§6.6 校验第 1 步）。
const MaxCommandLen = 200

// cmdCtx 校验一条命令时要知道的事。
type cmdCtx struct {
	self    string            // 提问的人（「自己」）
	online  map[string]string // 在线的玩家：小写 → 原样
	enabled map[string]bool   // 启用了哪些模板
}

func newCmdCtx(self string, online []string, enabled map[string]bool) *cmdCtx {
	c := &cmdCtx{self: self, online: map[string]string{}, enabled: enabled}
	for _, p := range online {
		c.online[strings.ToLower(p)] = p
	}
	return c
}

func (c *cmdCtx) isSelf(s string) bool { return strings.EqualFold(s, c.self) }

// player 在线的玩家（不分大小写），返回原样的名字。
func (c *cmdCtx) player(s string) (string, bool) {
	p, ok := c.online[strings.ToLower(s)]
	return p, ok
}

// CheckCommand 按白名单校验 AI 要执行的命令（§6.6 的校验步骤），通过时返回要写进服务器的那条命令（去掉开头的 /、
// 去掉命名空间、「自己」换成提问的人的名字、参数之间只留一个空格）。不通过时返回给 AI 看的中文原因。
func CheckCommand(cmd, self string, online []string, enabled map[string]bool) (string, error) {
	return checkCommand(cmd, newCmdCtx(self, online, enabled))
}

func checkCommand(cmd string, c *cmdCtx) (string, error) {
	cmd = strings.TrimSpace(cmd)
	cmd = strings.TrimPrefix(cmd, "/")
	if cmd == "" {
		return "", fmt.Errorf("命令是空的")
	}
	if len([]rune(cmd)) > MaxCommandLen {
		return "", fmt.Errorf("命令太长了（最多 %d 个字符）", MaxCommandLen)
	}
	for _, r := range cmd {
		switch {
		case r == '\n' || r == '\r':
			return "", fmt.Errorf("命令里不能有换行，一次只能执行一条")
		case r == '§':
			return "", fmt.Errorf("命令里不能有 § 格式码")
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("命令里不能有控制字符")
		case r > 0x7e:
			return "", fmt.Errorf("命令里只能有英文字母、数字和半角符号（不能有中文和全角字符）")
		}
	}
	args := strings.Fields(cmd)
	head := strings.ToLower(args[0])
	if i := strings.LastIndexByte(head, ':'); i >= 0 { // minecraft:give → give
		head = head[i+1:]
	}
	args = args[1:]
	if slices.Contains(Forbidden, head) {
		return "", fmt.Errorf("%s 命令永远不允许游戏内 AI 执行", head)
	}
	for _, a := range args {
		if strings.Contains(a, "@") {
			return "", fmt.Errorf("不能用目标选择器（%s）：要指定玩家就直接写名字，「自己」写提问的人的名字", a)
		}
	}
	var t *Template
	for _, x := range Templates {
		if slices.Contains(x.heads, head) {
			t = x
			break
		}
	}
	if t == nil {
		return "", fmt.Errorf("%s 不在允许执行的命令里", head)
	}
	if !c.enabled[t.ID] {
		return "", fmt.Errorf("服主没有打开「%s」（%s）这类命令", t.Title, head)
	}
	out, err := t.check(c, head, args)
	if err != nil {
		return "", err
	}
	return strings.Join(append([]string{head}, out...), " "), nil
}

func oneOf(s string, vals ...string) bool { return slices.Contains(vals, strings.ToLower(s)) }

var (
	intRe   = regexp.MustCompile(`^\d{1,9}$`)
	coordRe = regexp.MustCompile(`^(?:[~^]|[~^]?-?\d{1,8}(?:\.\d{1,6})?)$`)
	idRe    = regexp.MustCompile(`^[a-z0-9_.\-]+(?::[a-z0-9_./\-]+)?$`)
	tagRe   = regexp.MustCompile(`^#?[a-z0-9_.\-]+(?::[a-z0-9_./\-]+)?$`)
	boolRe  = regexp.MustCompile(`^(?:true|false)$`)
)

// num 不大于 max 的正整数。
func num(s string, max int, what string) (string, error) {
	if !intRe.MatchString(s) {
		return "", fmt.Errorf("%s要写成整数（%s）", what, s)
	}
	n, _ := strconv.Atoi(s)
	if n < 1 || n > max {
		return "", fmt.Errorf("%s要在 1 到 %d 之间（%s）", what, max, s)
	}
	return strconv.Itoa(n), nil
}

func checkTime(_ *cmdCtx, _ string, a []string) ([]string, error) {
	switch {
	case len(a) == 2 && strings.EqualFold(a[0], "set") && oneOf(a[1], "day", "night", "noon", "midnight"):
	case len(a) == 2 && strings.EqualFold(a[0], "query") && oneOf(a[1], "daytime", "gametime", "day"):
	default:
		return nil, fmt.Errorf("time 只能是 time set day|night|noon|midnight 或 time query daytime|gametime|day")
	}
	return []string{strings.ToLower(a[0]), strings.ToLower(a[1])}, nil
}

func checkWeather(_ *cmdCtx, _ string, a []string) ([]string, error) {
	if len(a) < 1 || len(a) > 2 || !oneOf(a[0], "clear", "rain", "thunder") {
		return nil, fmt.Errorf("weather 只能是 weather clear|rain|thunder [秒]")
	}
	out := []string{strings.ToLower(a[0])}
	if len(a) == 2 {
		s, err := num(a[1], 6000, "持续时间（秒）")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func coords(a []string) bool {
	if len(a) != 3 {
		return false
	}
	local := 0
	for _, s := range a {
		if !coordRe.MatchString(s) {
			return false
		}
		if strings.HasPrefix(s, "^") {
			local++
		}
	}
	return local == 0 || local == 3 // ^ 局部坐标三个要一起用
}

// checkTP 只能传送自己：tp <自己> <在线玩家>、tp <自己> x y z；省略「自己」时程序补上。
func checkTP(c *cmdCtx, _ string, a []string) ([]string, error) {
	bad := fmt.Errorf("tp 只能把提问的人（%s）传送到某个在线玩家身边或某个坐标：tp %s <在线玩家> 或 tp %s x y z", c.self, c.self, c.self)
	if len(a) == 2 || len(a) == 4 {
		if !c.isSelf(a[0]) {
			if _, ok := c.player(a[0]); ok {
				return nil, fmt.Errorf("只能传送提问的人自己（%s），不能传送 %s", c.self, a[0])
			}
			return nil, bad
		}
		a = a[1:]
	}
	switch {
	case len(a) == 1:
		p, ok := c.player(a[0])
		if !ok {
			return nil, fmt.Errorf("%s 不在线，传送不过去", a[0])
		}
		if c.isSelf(p) {
			return nil, fmt.Errorf("不能把 %s 传送到自己身边", c.self)
		}
		return []string{c.self, p}, nil
	case coords(a):
		return append([]string{c.self}, a...), nil
	}
	return nil, bad
}

// target 第一个参数是不是「自己」：是就去掉；是别的在线玩家就报错；都不是（多半省略了）返回原样。
func (c *cmdCtx) target(a []string, what string) ([]string, error) {
	if len(a) == 0 {
		return a, nil
	}
	if c.isSelf(a[0]) {
		return a[1:], nil
	}
	if _, ok := c.player(a[0]); ok {
		return nil, fmt.Errorf("%s只能对提问的人自己（%s）用，不能对 %s 用", what, c.self, a[0])
	}
	return a, nil
}

// checkEffect effect give <自己> <效果> [秒] [等级] [隐藏粒子]、effect clear <自己> [效果]；
// 也认 1.12 的旧写法 effect <自己> <效果> [秒] [等级]、effect <自己> clear（版本不对时服务器会报错）。
func checkEffect(c *cmdCtx, _ string, a []string) ([]string, error) {
	bad := fmt.Errorf("effect 只能是 effect give %s <效果> [秒] [等级] 或 effect clear %s [效果]", c.self, c.self)
	if len(a) == 0 {
		return nil, bad
	}
	sub := strings.ToLower(a[0])
	if sub != "give" && sub != "clear" { // 1.12 的旧写法：effect <自己> …
		if !c.isSelf(a[0]) {
			return nil, bad
		}
		rest := a[1:]
		if len(rest) == 1 && strings.EqualFold(rest[0], "clear") {
			return []string{c.self, "clear"}, nil
		}
		out, err := effectArgs(rest)
		if err != nil {
			return nil, err
		}
		return append([]string{c.self}, out...), nil
	}
	rest, err := c.target(a[1:], "状态效果")
	if err != nil {
		return nil, err
	}
	if sub == "clear" {
		switch len(rest) {
		case 0:
			return []string{"clear", c.self}, nil
		case 1:
			if !idRe.MatchString(strings.ToLower(rest[0])) {
				return nil, fmt.Errorf("效果的 ID 写得不对（%s）", rest[0])
			}
			return []string{"clear", c.self, strings.ToLower(rest[0])}, nil
		}
		return nil, bad
	}
	out, err := effectArgs(rest)
	if err != nil {
		return nil, err
	}
	return append([]string{"give", c.self}, out...), nil
}

// effectArgs <效果> [秒] [等级] [隐藏粒子]：秒 ≤ 600，等级 ≤ 4。
func effectArgs(a []string) ([]string, error) {
	if len(a) < 1 || len(a) > 4 {
		return nil, fmt.Errorf("effect 的写法：<效果> [秒] [等级]")
	}
	id := strings.ToLower(a[0])
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("效果的 ID 写得不对（%s）", a[0])
	}
	out := []string{id}
	if len(a) >= 2 {
		s, err := num(a[1], 600, "持续时间（秒）")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if len(a) >= 3 {
		if !intRe.MatchString(a[2]) {
			return nil, fmt.Errorf("等级要写成整数（%s）", a[2])
		}
		n, _ := strconv.Atoi(a[2])
		if n > 4 {
			return nil, fmt.Errorf("等级最多 4（%s）", a[2])
		}
		out = append(out, strconv.Itoa(n))
	}
	if len(a) == 4 {
		if !boolRe.MatchString(strings.ToLower(a[3])) {
			return nil, fmt.Errorf("最后一个参数只能是 true 或 false（%s）", a[3])
		}
		out = append(out, strings.ToLower(a[3]))
	}
	return out, nil
}

func checkSpawnpoint(c *cmdCtx, _ string, a []string) ([]string, error) {
	rest, err := c.target(a, "设出生点")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("spawnpoint 只能是 spawnpoint %s（设在现在站的地方），不能带坐标", c.self)
	}
	return []string{c.self}, nil
}

// checkLocate locate structure|biome <ID>、list；也认 1.18 以前的 locate <结构>、locatebiome <生物群系>。
func checkLocate(_ *cmdCtx, head string, a []string) ([]string, error) {
	id := func(s string) (string, error) {
		if !tagRe.MatchString(strings.ToLower(s)) {
			return "", fmt.Errorf("ID 写得不对（%s），应形如 minecraft:village_plains", s)
		}
		return s, nil
	}
	switch head {
	case "list":
		if len(a) == 0 {
			return nil, nil
		}
		if len(a) == 1 && strings.EqualFold(a[0], "uuids") {
			return []string{"uuids"}, nil
		}
		return nil, fmt.Errorf("list 只能是 list 或 list uuids")
	case "locatebiome":
		if len(a) != 1 {
			return nil, fmt.Errorf("locatebiome 只能带一个生物群系 ID")
		}
		s, err := id(a[0])
		return []string{s}, err
	}
	switch {
	case len(a) == 2 && oneOf(a[0], "structure", "biome"):
		s, err := id(a[1])
		return []string{strings.ToLower(a[0]), s}, err
	case len(a) == 1 && !oneOf(a[0], "structure", "biome", "poi"):
		s, err := id(a[0])
		return []string{s}, err
	}
	return nil, fmt.Errorf("locate 只能是 locate structure <ID> 或 locate biome <ID>")
}

func checkGamemode(c *cmdCtx, _ string, a []string) ([]string, error) {
	if len(a) < 1 || len(a) > 2 || !oneOf(a[0], "survival", "creative", "adventure", "spectator") {
		return nil, fmt.Errorf("gamemode 只能是 gamemode survival|creative|adventure|spectator %s", c.self)
	}
	if len(a) == 2 && !c.isSelf(a[1]) {
		return nil, fmt.Errorf("只能改提问的人自己（%s）的游戏模式，不能改 %s 的", c.self, a[1])
	}
	return []string{strings.ToLower(a[0]), c.self}, nil
}

func checkGive(c *cmdCtx, _ string, a []string) ([]string, error) {
	rest, err := c.target(a, "给物品")
	if err != nil {
		return nil, err
	}
	if len(rest) < 1 || len(rest) > 2 {
		return nil, fmt.Errorf("give 只能是 give %s <物品> [数量]", c.self)
	}
	item := strings.ToLower(rest[0])
	if !idRe.MatchString(item) {
		return nil, fmt.Errorf("物品的 ID 写得不对（%s），只能是 minecraft:diamond 这样的 ID，不能带 NBT 或组件", rest[0])
	}
	out := []string{c.self, item}
	if len(rest) == 2 {
		s, err := num(rest[1], 64, "数量")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func checkDifficulty(_ *cmdCtx, _ string, a []string) ([]string, error) {
	switch {
	case len(a) == 0:
		return nil, nil
	case len(a) == 1 && oneOf(a[0], "peaceful", "easy", "normal", "hard"):
		return []string{strings.ToLower(a[0])}, nil
	}
	return nil, fmt.Errorf("difficulty 只能是 difficulty peaceful|easy|normal|hard")
}

func checkSeed(_ *cmdCtx, _ string, a []string) ([]string, error) {
	if len(a) != 0 {
		return nil, fmt.Errorf("seed 不带参数")
	}
	return nil, nil
}
