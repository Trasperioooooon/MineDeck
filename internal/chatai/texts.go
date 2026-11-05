package chatai

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"minedeck/internal/ai"
)

// 不调用 AI 的几种回复（§6.2）：帮助、额度、未知选项、选项放错位置。都私发给提问的人。
// 系统提示一句一行（\n 分开），发的时候按聊天框的宽度拆成几条（见 Fit），每条一行显示得下，游戏里不会在奇怪的地方换行。

// helpText /帮助：由选项表生成，只列这个服实际能用的选项。有执行权限的人多一行「能让 AI 做的事」（按打开了的命令模板生成）；
// 没有执行权限的人不显示 /问，因为对他们来说本来就只回答。
func (r *room) helpText(set Settings, authorized bool, comp string) string {
	p := set.Prefix
	opts := []string{"/私（只回你，但你的提问别人照样看得到）"}
	if authorized {
		opts = append(opts, "/问（只回答、不执行命令）")
	}
	opts = append(opts, "/额度（看今天还能问几次）", "/帮助")
	text := fmt.Sprintf("用法：%s 你的问题，比如 %s 今晚怎么打末影龙。\n问题前可加：%s。\n想问命令怎么用，可以说 %s /tp 怎么用。",
		p, p, strings.Join(opts, "、"), p)
	if authorized {
		var does []string
		for _, t := range Templates {
			if set.Templates[t.ID] {
				does = append(does, t.Does)
			}
		}
		if len(does) > 0 {
			text += "\n你能让 AI 帮你：" + strings.Join(does, "、") + "。AI 执行的命令，效果大家都看得到。"
		}
	}
	if list, _, ok := r.s.companionsLive(r.name); ok { // AI 伙伴（第七阶段）
		text += companionHelp(p, list, authorized)
	}
	if comp != "" {
		text = "我是伙伴「" + comp + "」。" + text
	}
	if !r.s.ai.Enabled() {
		text += "\nMineDeck 还没配置 AI，现在问不了问题。"
	}
	return text
}

// quotaText /额度：自己今天还能问几次、冷却还剩几秒；全服当天的次数或 token 快用完时也一并说明。
// 叫着伙伴的名字问的（「@小方 /额度」）说的是叫伙伴的次数；问 AI 和叫伙伴分开算，有伙伴时顺带说一句另一种还剩几次。
func (r *room) quotaText(set Settings, player string, now time.Time, feat ai.Feature) string {
	comp := feat == ai.FeatCompanion
	r.mu.Lock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	used, other, count, tokens := c.used(k, comp), c.used(k, !comp), c.Count, c.Tokens
	var cd time.Duration
	if last, ok := r.lastAsk[k]; ok && set.Cooldown > 0 {
		cd = time.Duration(set.Cooldown)*time.Second - now.Sub(last)
	}
	r.mu.Unlock()
	var parts []string
	switch {
	case set.PerPlayer > 0 && comp:
		parts = append(parts, fmt.Sprintf("你今天还能叫伙伴 %d 次（每天 %d 次）", max(0, set.PerPlayer-used), set.PerPlayer))
	case set.PerPlayer > 0:
		parts = append(parts, fmt.Sprintf("你今天还能问 %d 次（每天 %d 次）", max(0, set.PerPlayer-used), set.PerPlayer))
	case comp:
		parts = append(parts, fmt.Sprintf("你今天叫了伙伴 %d 次，没有次数限制", used))
	default:
		parts = append(parts, fmt.Sprintf("你今天问了 %d 次，没有次数限制", used))
	}
	if cd > 0 {
		parts = append(parts, "还要等 "+secs(cd)+" 秒才能再问")
	}
	text := strings.Join(parts, "。") + "。"
	if _, _, live := r.s.companionsLive(r.name); live && set.PerPlayer > 0 {
		if comp {
			text += fmt.Sprintf("\n问 AI 另算，今天还能问 %d 次。", max(0, set.PerPlayer-other))
		} else {
			text += fmt.Sprintf("\n叫伙伴另算，今天还能叫 %d 次。", max(0, set.PerPlayer-other))
		}
	}
	if set.PerServer > 0 {
		if left := set.PerServer - count; left <= 0 {
			text += "\n全服今天的次数已经用完了。"
		} else if left*5 <= set.PerServer {
			text += fmt.Sprintf("\n全服今天只剩 %d 次了。", left)
		}
	}
	if set.Budget > 0 {
		if tokens >= set.Budget {
			text += "\n全服今天的 AI 额度已经用完了。"
		} else if tokens*5 >= set.Budget*4 {
			text += fmt.Sprintf("\n全服今天的 AI 额度已经用了 %d%%。", tokens*100/set.Budget)
		}
	}
	what := "游戏内 AI"
	if comp {
		what = "AI 伙伴"
	}
	if used, limit := r.s.ai.Usage(feat); limit > 0 {
		if used.Tokens >= limit {
			text += "\n" + what + "今天的总额度已经用完了。"
		} else if used.Tokens*5 >= limit*4 {
			text += fmt.Sprintf("\n%s今天的总额度已经用了 %d%%。", what, used.Tokens*100/limit)
		}
	}
	return text
}

// silenceText 静默时私下说的话，带上还要等多久。
func silenceText(left time.Duration) string {
	return "你问得太快了，" + waitText(left) + "后再问。"
}

// waitText 还要等多久：满 1 分钟按分钟说（向上取整），不满按秒。
func waitText(d time.Duration) string {
	if d >= time.Minute {
		return itoa(int((d+time.Minute-1)/time.Minute)) + " 分钟"
	}
	return secs(d) + " 秒"
}

// hintText 未知选项、选项后面没空格、选项放错位置的提示。提示里的选项名最多 16 个字（按 §6.3 转义后才放进 tellraw）。
func hintText(prefix string, a *Ask, authorized bool) string {
	ex := a.Example
	if ex == "" {
		ex = "今晚怎么打末影龙"
	}
	switch a.Action {
	case ActNoSpace:
		what := "选项和问题之间"
		if r, _ := utf8.DecodeRuneInString(ex); isMark(r) {
			what = "选项之间" // @AI /私/问 你好
		}
		return fmt.Sprintf("%s要空一格，比如 %s /%s %s", what, prefix, optPrefixOf(a.Unknown), ex)
	case ActMisplaced:
		return fmt.Sprintf("选项要写在问题前面，比如 %s /私 %s", prefix, ex)
	}
	opts := "/私（只回你）、"
	if authorized {
		opts += "/问（只回答、不执行命令）、"
	}
	if a.Unknown == "" { // 只有一个 /（或 ／、、），后面没跟选项名
		return fmt.Sprintf("/ 后面要紧跟选项名，比如 %s /私 今晚怎么打末影龙。\n可用：%s/额度、/帮助。", prefix, opts)
	}
	name := a.Unknown // 举例「/tp 怎么用」：不认识的像个命令名才拿它举例
	if !cmdNameRe.MatchString(name) {
		name = "tp"
	}
	return fmt.Sprintf("不认识 /%s。可用：%s/额度、/帮助。\n想问命令怎么用，可以说 %s /%s 怎么用。", a.Unknown, opts, prefix, name)
}

// cmdNameRe 像个命令名：字母开头，字母、数字、_、-、:（minecraft:tp）。
var cmdNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:-]{0,15}$`)

// providerName 隐私提示里的服务商：预设的名字；自定义的写接口地址的主机名；本机模型写明不出本机。
// 西文名字前后各空一格（「发给 DeepSeek 处理」）。
func providerName(svc *ai.Service) string {
	cur := svc.Current()
	p, ok := ai.PresetByID(cur.Preset)
	switch {
	case !ok:
		return "AI 服务商"
	case p.Local:
		return "开服那台电脑上的模型（" + p.Name + "，不会发到别处）" // 这句是发给玩家看的：「这台电脑」在玩家眼里是他自己的电脑
	case p.ID == ai.CustomID:
		if u, err := url.Parse(cur.BaseURL); err == nil && u.Hostname() != "" {
			return spaced(u.Hostname())
		}
		return "自定义的 AI 服务"
	}
	return spaced(p.Name)
}

// spaced 西文开头、结尾的名字前后各加一个空格，夹在中文里好读。
func spaced(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c < 0x80 {
		s = " " + s
	}
	if c := s[len(s)-1]; c < 0x80 {
		s += " "
	}
	return s
}
