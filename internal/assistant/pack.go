package assistant

import (
	"context"
	"fmt"
	"strings"

	"minedeck/internal/diag"
	"minedeck/internal/modinfo"
	"minedeck/internal/ops"
)

// 打包的排错资料各部分的上限（字符），合计约 2 万 token（§5.3）。
const (
	packConsole = 30000
	packCrash   = 12000
	packHsErr   = 4000
	packMods    = 6000
	packTotal   = 60000
	packTail    = 120 // 控制台最后保留几行
)

// pack 「让 AI 分析」的排错资料（§5.3）：实例信息、本地规则的结论、本次启动以来的控制台（精简）、本次启动之后的
// 崩溃报告和 hs_err 节选、相关模组。都经操作层的读操作取（和 AI 自己调工具拿到的一样），脱敏由调用方做。
func (s *Service) pack(ctx context.Context, c ops.Caller, inst string) (string, error) {
	call := func(name string, args map[string]any, out any) error {
		res, err := s.reg.Do(ctx, c, name, ops.ArgsOf(args))
		if err != nil {
			return err
		}
		return decode(res, out)
	}
	var list []struct {
		Name, Core, Kind, MC, Build, Status, JavaSource string
		JavaMajor, XmxMB, XmsMB, Port, OnlineCount      int
		ExtraJVM                                        []string `json:"extraJvm"`
	}
	if err := call("instance.list", nil, &list); err != nil {
		return "", err
	}
	idx := -1
	for k, i := range list {
		if i.Name == inst {
			idx = k
		}
	}
	if idx < 0 {
		return "", ops.Errorf(404, "实例不存在：%s", inst)
	}
	i := list[idx]
	var app struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Ram     int    `json:"ramMb"`
		Avail   int    `json:"availRamMb"`
	}
	_ = call("app.info", nil, &app)
	var props struct {
		Pairs []struct{ Key, Value string }
	}
	_ = call("instance.props.get", map[string]any{"inst": inst}, &props)
	prop := func(k string) string {
		for _, p := range props.Pairs {
			if p.Key == k {
				return p.Value
			}
		}
		return "（未设置）"
	}
	var mods struct {
		Count, Disabled int
	}
	modsErr := call("instance.mods.list", map[string]any{"inst": inst}, &mods)

	var b strings.Builder
	sec := func(title string) { fmt.Fprintf(&b, "\n## %s\n", title) }
	sec("实例信息")
	build := ""
	if i.Build != "" {
		build = " " + i.Build
	}
	fmt.Fprintf(&b, "名称：%s；核心：%s%s（Minecraft %s）；状态：%s\n", i.Name, i.Core, build, i.MC, i.Status)
	src := map[string]string{"portable": "MineDeck 便携版", "local": "本机安装的"}[i.JavaSource]
	fmt.Fprintf(&b, "Java：%d（%s）；内存：最大 %d MB、最小 %d MB；本机内存 %d MB，可用 %d MB\n", i.JavaMajor, src, i.XmxMB, i.XmsMB, app.Ram, app.Avail)
	if len(i.ExtraJVM) > 0 {
		fmt.Fprintf(&b, "JVM 参数：%s\n", strings.Join(i.ExtraJVM, " "))
	}
	fmt.Fprintf(&b, "端口：%d；online-mode：%s", i.Port, prop("online-mode"))
	if modsErr == nil {
		fmt.Fprintf(&b, "；模组 %d 个（禁用 %d 个）", mods.Count, mods.Disabled)
	}
	fmt.Fprintf(&b, "\n系统：%s；MineDeck %s\n", strOr(app.OS, "Windows"), app.Version)

	var rep diag.Report
	_ = call("instance.diag", map[string]any{"inst": inst}, &rep)
	sec("MineDeck 本地规则的结论")
	if len(rep.Findings) == 0 {
		b.WriteString("没有认出已知的问题。\n")
	}
	var related []string
	for k, f := range rep.Findings {
		fmt.Fprintf(&b, "%d. %s：%s\n", k+1, f.Title, f.Summary)
		for _, d := range f.Details {
			fmt.Fprintf(&b, "   - %s\n", d)
		}
		related = append(related, f.Mods...)
	}

	var con struct{ Lines []string }
	if err := call("instance.console.tail", map[string]any{"inst": inst, "since": "start", "lines": 3000}, &con); err == nil && len(con.Lines) > 0 {
		sec("控制台（最近一次启动以来，只留 WARN / ERROR、异常和最后几行）")
		b.WriteString(fit(diag.Condense(con.Lines, packTail), packConsole))
		b.WriteString("\n")
	}

	var crashes struct {
		Reports []struct {
			Name, Kind string
			ThisStart  bool
		}
	}
	if err := call("instance.crashes", map[string]any{"inst": inst}, &crashes); err == nil {
		got := map[string]bool{}
		for _, r := range crashes.Reports {
			if !r.ThisStart || got[r.Kind] {
				continue
			}
			got[r.Kind] = true
			var one struct{ Summary string }
			if call("instance.crashes", map[string]any{"inst": inst, "file": r.Name}, &one) != nil || one.Summary == "" {
				continue
			}
			limit := packCrash
			if r.Kind == "jvm" {
				sec("Java 虚拟机崩溃日志 " + r.Name + "（开头的摘要）")
				limit = packHsErr
			} else {
				sec("崩溃报告 " + r.Name + "（节选）")
			}
			b.WriteString(clip(one.Summary, limit))
			b.WriteString("\n")
		}
	}

	if related = uniqStr(related); len(related) > 0 && modsErr == nil {
		var lines []string
		for _, id := range related {
			if len(lines) >= 8 {
				break
			}
			var r struct{ Mods []modinfo.Mod }
			if call("instance.mods.list", map[string]any{"inst": inst, "q": id, "detail": true}, &r) != nil {
				continue
			}
			found := false
			for _, m := range r.Mods {
				if m.ID != id {
					continue
				}
				found = true
				lines = append(lines, modLine(m))
			}
			if !found {
				lines = append(lines, id+"：mods 目录里没有")
			}
		}
		if len(lines) > 0 {
			sec("相关模组")
			b.WriteString(clip(strings.Join(lines, "\n"), packMods))
			b.WriteString("\n")
		}
	}
	text := strings.TrimSpace(b.String())
	return clip(text, packTotal), nil
}

func modLine(m modinfo.Mod) string {
	var deps []string
	for _, d := range m.Deps {
		switch {
		case d.Kind == "incompatible":
			deps = append(deps, d.ID+" 不兼容")
		case d.Required:
			deps = append(deps, strings.TrimSpace(d.ID+" "+d.Range)+"（必需）")
		}
	}
	s := fmt.Sprintf("%s %s（%s）", m.ID, m.Version, m.File)
	if m.Disabled {
		s += "，已禁用"
	}
	if len(deps) > 0 {
		s += "：依赖 " + strings.Join(deps, "、")
	}
	return s
}

// fit 控制台精简后还太长：保留最后能装下的部分。
func fit(lines []string, limit int) string {
	total := 0
	k := len(lines)
	for k > 0 && total+len(lines[k-1])+1 <= limit {
		k--
		total += len(lines[k]) + 1
	}
	out := strings.Join(lines[k:], "\n")
	if k > 0 {
		out = fmt.Sprintf("……（前面还有 %d 行，太长没有给出）\n", k) + out
	}
	return out
}

// clip 截到 limit 个字节以内（不切断一个字）。
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "\n……（后面太长，没有给出）"
}

func strOr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func uniqStr(ss []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
