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

// 不调用 AI 的几种回复（§6.2）：帮助、额度、未知选项、选项放错位置。都私发给提问的人，最多 3 条。

// helpText /帮助：由选项表生成，只列这个服实际能用的选项。有执行权限的人多一行「能让 AI 做的事」（按打开了的命令模板生成）；
// 没有执行权限的人不显示 /问，因为对他们来说本来就只回答。
func (r *room) helpText(set Settings, authorized bool, comp string) string {
	p := set.Prefix
	opts := []string{"/私（只回你；你的提问别人照样看得到）"}
	if authorized {
		opts = append(opts, "/问（只回答、不执行命令）")
	}
	opts = append(opts, "/额度（看今天还能问几次）", "/帮助")
	text := fmt.Sprintf("用法：%s 你的问题，比如 %s 今晚怎么打末影龙。问题前可加：%s。", p, p, strings.Join(opts, "、"))
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
		text += "\n主机的 MineDeck 还没配置 AI，现在问不了问题。"
	}
	return text
}

// quotaText /额度：自己今天还能问几次、冷却还剩几秒；全服当天的次数或 token 快用完时也一并说明。
func (r *room) quotaText(set Settings, player string, now time.Time, feat ai.Feature) string {
	r.mu.Lock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	used, count, tokens := c.Players[k], c.Count, c.Tokens
	var cd time.Duration
	if last, ok := r.lastAsk[k]; ok && set.Cooldown > 0 {
		cd = time.Duration(set.Cooldown)*time.Second - now.Sub(last)
	}
	r.mu.Unlock()
	var parts []string
	if set.PerPlayer > 0 {
		left := max(0, set.PerPlayer-used)
		parts = append(parts, fmt.Sprintf("你今天还能问 %d 次（每天 %d 次）", left, set.PerPlayer))
	} else {
		parts = append(parts, fmt.Sprintf("你今天问了 %d 次，没有次数限制", used))
	}
	if cd > 0 {
		parts = append(parts, "冷却还剩 "+secs(cd)+" 秒")
	}
	text := strings.Join(parts, "，") + "。"
	if set.PerServer > 0 {
		if left := set.PerServer - count; left <= 0 {
			text += "全服今天的次数已经用完了。"
		} else if left*5 <= set.PerServer {
			text += fmt.Sprintf("全服今天只剩 %d 次了。", left)
		}
	}
	if set.Budget > 0 {
		if tokens >= set.Budget {
			text += "全服今天的 AI 额度已经用完了。"
		} else if tokens*5 >= set.Budget*4 {
			text += fmt.Sprintf("全服今天的 AI 额度已经用了 %d%%。", tokens*100/set.Budget)
		}
	}
	what := "游戏内 AI"
	if feat == ai.FeatCompanion {
		what = "AI 伙伴"
	}
	if used, limit := r.s.ai.Usage(feat); limit > 0 {
		if used.Tokens >= limit {
			text += what + "今天的总额度已经用完了。"
		} else if used.Tokens*5 >= limit*4 {
			text += fmt.Sprintf("%s今天的总额度已经用了 %d%%。", what, used.Tokens*100/limit)
		}
	}
	return text
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
		return fmt.Sprintf("/ 后面要紧跟选项名，比如 %s /私 今晚怎么打末影龙。可用：%s/额度、/帮助", prefix, opts)
	}
	name := a.Unknown // 举例「怎么用 /tp」：不认识的像个命令名才拿它举例
	if !cmdNameRe.MatchString(name) {
		name = "tp"
	}
	return fmt.Sprintf("不认识 /%s。可用：%s/额度、/帮助。想问命令的用法，把命令写在问题中间，比如 %s 怎么用 /%s", a.Unknown, opts, prefix, name)
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
		return "主机上的本机模型（" + p.Name + "，不出这台电脑）"
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
