package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"minedeck/internal/inst"
	"minedeck/internal/modinfo"
	"minedeck/internal/ops"
)

// fileOps 模组清单、禁用 / 启用模组、读写配置文件（设计文档 §5.2）。
func (c *cat) fileOps() {
	type modsArgs struct {
		Inst   string `json:"inst"`
		Q      string `json:"q"`
		Detail bool   `json:"detail"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.mods.list", Title: "查看模组清单", Level: ops.Read,
		Desc: "解析 mods 目录（插件服是 plugins 目录）里的 jar：文件名、模组 ID、版本、是否已禁用。给了 q 或 detail 为 true 时，" +
			"匹配的那几个再带上依赖（必需 / 不兼容、版本范围、装在哪一端）和 mixin 配置。",
		Params: withInst(map[string]*ops.Schema{
			"q":      ops.String("按文件名、模组 ID 或名称过滤（不分大小写）").Len(0, 100),
			"detail": ops.Boolean("带上依赖和 mixin 配置"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modsArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			dir := modsDir(i)
			if dir == "" {
				return nil, ops.Errorf(400, "「%s」是 %s 核心，没有模组或插件目录", i.Name, i.Core)
			}
			mods, err := modinfo.ReadDir(filepath.Join(i.Dir, dir))
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			q := strings.ToLower(strings.TrimSpace(p.Q))
			detail := p.Detail || q != ""
			disabled := 0
			var list []any
			for _, m := range mods {
				if m.Disabled {
					disabled++
				}
				if q != "" && !strings.Contains(strings.ToLower(m.File+" "+m.ID+" "+m.Name), q) {
					continue
				}
				if detail {
					list = append(list, m)
					continue
				}
				type brief struct {
					File     string `json:"file"`
					ID       string `json:"id,omitempty"`
					Version  string `json:"version,omitempty"`
					Disabled bool   `json:"disabled,omitempty"`
					Err      string `json:"err,omitempty"`
				}
				list = append(list, brief{File: m.File, ID: m.ID, Version: m.Version, Disabled: m.Disabled, Err: m.Err})
			}
			if list == nil {
				list = []any{}
			}
			return map[string]any{"dir": dir, "count": len(mods), "disabled": disabled, "mods": list}, nil
		},
	})

	type toggleArgs struct {
		Inst   string `json:"inst"`
		File   string `json:"file"`
		Enable bool   `json:"enable"`
	}
	// toggle 算出改名前后的路径；state 是现在的状态（前置条件）
	toggle := func(p toggleArgs) (i *inst.Instance, from, to, state string, err error) {
		if i, err = c.get(p.Inst); err != nil {
			return
		}
		dir := modsDir(i)
		if dir == "" {
			err = ops.Errorf(400, "「%s」没有模组或插件目录", i.Name)
			return
		}
		name := filepath.Base(strings.TrimSpace(p.File))
		base := strings.TrimSuffix(name, ".disabled")
		if !strings.HasSuffix(strings.ToLower(base), ".jar") || name != strings.TrimSpace(p.File) {
			err = ops.Errorf(400, "file 应是 %s 目录里的 jar 文件名（如 xxx.jar 或 xxx.jar.disabled）", dir)
			return
		}
		on, off := filepath.Join(i.Dir, dir, base), filepath.Join(i.Dir, dir, base+".disabled")
		_, e1 := os.Stat(on)
		_, e2 := os.Stat(off)
		switch {
		case e1 == nil && e2 == nil:
			state = "两份都在"
		case e1 == nil:
			state = "启用"
		case e2 == nil:
			state = "禁用"
		default:
			err = ops.Errorf(404, "%s 目录里没有 %s", dir, base)
			return
		}
		if p.Enable {
			from, to = off, on
		} else {
			from, to = on, off
		}
		return
	}
	c.reg.Register(&ops.Op{
		Name: "instance.mods.toggle", Title: "禁用或启用模组", Level: ops.Write,
		Desc: "把 mods（或 plugins）目录里的 jar 改名成 .jar.disabled 来禁用，改回来就是启用；不删文件，可以撤销。重启服务器后生效。",
		Params: withInst(map[string]*ops.Schema{
			"file":   ops.String("jar 文件名（见 instance.mods.list 的 file）").Len(1, 200),
			"enable": ops.Boolean("true 启用，false 禁用"),
		}, "file", "enable"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[toggleArgs](a)
			if err != nil {
				return nil, err
			}
			i, from, to, state, err := toggle(p)
			if err != nil {
				return nil, err
			}
			act := map[bool]string{true: "启用", false: "禁用"}[p.Enable]
			what := filepath.Base(strings.TrimSuffix(to, ".disabled"))
			if m := modinfo.Read(from); m.ID != "" {
				what += fmt.Sprintf("（%s %s）", m.ID, m.Version)
			}
			plan := &ops.Plan{Summary: fmt.Sprintf("%s「%s」的模组 %s", act, i.Name, what),
				Changes: []ops.Change{{What: "文件名", Before: filepath.Base(from), After: filepath.Base(to)}},
				Pre:     map[string]string{"模组文件": state}}
			if state == act {
				plan.Changes = nil
				plan.Summary = fmt.Sprintf("模组 %s 已经是%s的了", what, act)
			}
			if i.Status() != "stopped" {
				plan.Warning = "服务器正在运行：重启后才生效；运行中 jar 被占用时改名会失败，可以先停服"
			}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[toggleArgs](a)
			if err != nil {
				return nil, err
			}
			_, from, to, state, err := toggle(p)
			if err != nil {
				return nil, err
			}
			if state == "两份都在" {
				return nil, ops.Errorf(409, "%s 和 %s 都在，请先手动删掉一份", filepath.Base(from), filepath.Base(to))
			}
			if _, err := os.Stat(from); err != nil {
				return map[string]string{"file": filepath.Base(to), "note": "本来就是这个状态"}, nil
			}
			if err := os.Rename(from, to); err != nil {
				return nil, ops.Errorf(500, "改名失败（服务器运行中 jar 可能被占用，先停服再试）：%v", err)
			}
			return map[string]string{"file": filepath.Base(to)}, nil
		},
	})

	type cfgReadArgs struct {
		Inst  string `json:"inst"`
		Path  string `json:"path"`
		Start int    `json:"start"`
		Lines int    `json:"lines"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.config.read", Title: "读取模组配置文件", Level: ops.Read,
		Desc: "读 config/、defaultconfigs/、<世界>/serverconfig/ 和 plugins/<插件>/ 下的配置文件。path 为空或是目录时列出里面的文件；" +
			"是文件时返回内容（从 start 行起最多 lines 行）。",
		Params: withInst(map[string]*ops.Schema{
			"path":  ops.String("相对实例目录的路径，如 config/create-common.toml、world/serverconfig；空串列出可读的目录").Len(0, 300),
			"start": ops.Integer("从第几行起（从 1 起），默认 1").Range(1, 1e7),
			"lines": ops.Integer("最多几行，默认 400").Range(1, 2000),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[cfgReadArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			roots := configRoots(i)
			if strings.Trim(strings.TrimSpace(p.Path), `/\.`) == "" {
				var out []map[string]any
				for _, r := range roots {
					if st, err := os.Stat(filepath.Join(i.Dir, r)); err == nil && st.IsDir() {
						out = append(out, map[string]any{"path": r, "dir": true})
					}
				}
				if out == nil {
					out = []map[string]any{}
				}
				return map[string]any{"roots": out}, nil
			}
			rel, abs, err := configPath(i, p.Path, false)
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(abs)
			if err != nil {
				return nil, ops.Errorf(404, "没有 %s", rel)
			}
			if st.IsDir() {
				return map[string]any{"path": rel, "files": listConfigs(i.Dir, abs, 500)}, nil
			}
			if st.Size() > 1<<20 {
				return nil, ops.Errorf(400, "%s 太大（%s），只读 1 MB 以内的配置文件", rel, fmtSize(st.Size()))
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			lines := strings.Split(strings.ReplaceAll(decodeText(b), "\r\n", "\n"), "\n")
			start, n := max(p.Start, 1), p.Lines
			if n <= 0 {
				n = 400
			}
			end := min(len(lines), start-1+n)
			content := ""
			if start-1 < len(lines) {
				content = strings.Join(lines[start-1:end], "\n")
			}
			return map[string]any{"path": rel, "size": st.Size(), "totalLines": len(lines), "start": start, "content": content, "more": end < len(lines)}, nil
		},
	})

	type cfgEdit struct {
		Find    string `json:"find"`
		Replace string `json:"replace"`
	}
	type cfgWriteArgs struct {
		Inst    string    `json:"inst"`
		Path    string    `json:"path"`
		Mode    string    `json:"mode"`
		Edits   []cfgEdit `json:"edits"`
		Content *string   `json:"content"`
	}
	// cfgPlan 一次修改算出来的结果。old、next 是统一成 UTF-8、\n 换行的文本（和读给 AI 看的一样）；
	// 写回时按原文件的编码和换行还原，备份存原文件的字节，撤销时原样写回备份。
	type cfgPlan struct {
		i         *inst.Instance
		rel, abs  string
		old, next string
		exists    bool
		raw, undo []byte // 原文件、要恢复成的那份备份（mode=undo）
		format    textFormat
	}
	// plan 算出写完之后的内容
	plan := func(p cfgWriteArgs) (pl cfgPlan, err error) {
		if pl.i, err = c.get(p.Inst); err != nil {
			return
		}
		i := pl.i
		if pl.rel, pl.abs, err = configPath(i, p.Path, true); err != nil {
			return
		}
		rel := pl.rel
		b, rerr := os.ReadFile(pl.abs)
		pl.exists = rerr == nil
		if pl.exists {
			pl.raw, pl.format = b, sniffText(b)
			pl.old = pl.format.decode(b)
		}
		exists, old := pl.exists, pl.old
		switch p.Mode {
		case "", "edit":
			if !exists {
				err = ops.Errorf(404, "没有 %s；新建文件请用 mode=replace", rel)
				return
			}
			if len(p.Edits) == 0 {
				err = ops.Errorf(400, "edits 不能为空")
				return
			}
			pl.next = old
			for k, e := range p.Edits {
				switch n := strings.Count(pl.next, e.Find); {
				case e.Find == "":
					err = ops.Errorf(400, "第 %d 处修改的 find 是空的", k+1)
				case n == 0 && placeholders("", e.Find) != "":
					err = ops.Errorf(400, "第 %d 处修改：原文里有 MineDeck 隐藏掉的内容（***、<用户名> 这类），换一段不含它们的原文", k+1)
				case n == 0:
					err = ops.Errorf(400, "第 %d 处修改：文件里找不到要替换的原文（要和文件里一字不差，包括空格和引号）", k+1)
				case n > 1:
					err = ops.Errorf(400, "第 %d 处修改：要替换的原文出现了 %d 次，多带几个字，让它只出现一次", k+1, n)
				}
				if err != nil {
					return
				}
				pl.next = strings.Replace(pl.next, e.Find, e.Replace, 1)
			}
		case "replace":
			if p.Content == nil {
				err = ops.Errorf(400, "mode=replace 时要给 content")
				return
			}
			pl.next = *p.Content
		case "reset":
			if !exists {
				err = ops.Errorf(404, "没有 %s，不用恢复默认", rel)
				return
			}
			pl.next = ""
		case "undo":
			bk := latestBackup(i, rel)
			if bk == "" {
				err = ops.Errorf(404, "%s 没有 MineDeck 留下的备份，撤销不了", rel)
				return
			}
			b, berr := os.ReadFile(bk)
			if berr != nil {
				err = ops.Wrap(500, berr)
				return
			}
			pl.undo, pl.next = b, sniffText(b).decode(b)
		default:
			err = ops.Errorf(400, "mode 只能是 edit、replace、reset、undo")
			return
		}
		if p.Mode != "reset" && p.Mode != "undo" {
			if bad := placeholders(old, pl.next); bad != "" {
				err = ops.Errorf(400, "新内容里有 MineDeck 替换掉的隐私占位符（%s），不能原样写回；只改需要改的那几行（用 edits）", bad)
				return
			}
			_, err = pl.format.encode(pl.next) // GBK 的文件写不进 GBK 表示不了的字：提案阶段就报出来
		}
		return
	}
	c.reg.Register(&ops.Op{
		Name: "instance.config.write", Title: "修改模组配置文件", Level: ops.Write,
		Desc: "改 config/、defaultconfigs/、<世界>/serverconfig/ 或 plugins/<插件>/ 下的配置文件，改之前自动备份原文件，可以撤销。" +
			"mode：edit（默认，按 edits 逐处替换，每处的 find 要和文件里一字不差且只出现一次）、replace（用 content 整体替换或新建）、" +
			"reset（备份后删掉，让模组下次启动时重新生成默认配置）、undo（恢复到上一次修改之前）。",
		Params: withInst(map[string]*ops.Schema{
			"path": ops.String("相对实例目录的路径，如 config/create-common.toml").Len(1, 300),
			"mode": ops.String("edit、replace、reset、undo，默认 edit").OneOf("", "edit", "replace", "reset", "undo"),
			"edits": ops.Array("逐处替换", ops.Object(map[string]*ops.Schema{
				"find":    ops.String("文件里的原文（一字不差，只出现一次）").Len(1, 20000),
				"replace": ops.String("换成的内容").Len(0, 20000),
			}, "find", "replace")).MaxLen(20),
			"content": ops.String("mode=replace 时的完整新内容").Len(0, 500000),
		}, "path"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[cfgWriteArgs](a)
			if err != nil {
				return nil, err
			}
			cp, err := plan(p)
			if err != nil {
				return nil, err
			}
			i, rel, old, next, exists := cp.i, cp.rel, cp.old, cp.next, cp.exists
			pl := &ops.Plan{Pre: map[string]string{rel: textHash(old, exists)}}
			switch p.Mode {
			case "reset":
				pl.Summary = fmt.Sprintf("恢复「%s」的 %s 为默认：先备份，再删掉它，模组下次启动时重新生成", i.Name, rel)
			case "undo":
				pl.Summary = fmt.Sprintf("撤销「%s」的 %s 上一次的修改（恢复到 MineDeck 留下的备份）", i.Name, rel)
				pl.Diff = ops.LineDiff(old, next, 3, 300)
			default:
				pl.Summary = fmt.Sprintf("修改「%s」的 %s（先备份原文件，可以撤销）", i.Name, rel)
				if !exists {
					pl.Summary = fmt.Sprintf("新建「%s」的 %s", i.Name, rel)
				}
				pl.Diff = ops.LineDiff(old, next, 3, 300)
				if pl.Diff == "" {
					pl.Summary += "：内容没有变化"
				}
			}
			if i.Status() != "stopped" {
				pl.Warning = "服务器正在运行：多数模组要重启后才读新配置，有的还会在关服时把旧值写回，最好先停服再改"
			}
			return pl, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[cfgWriteArgs](a)
			if err != nil {
				return nil, err
			}
			cp, err := plan(p)
			if err != nil {
				return nil, err
			}
			i, rel, abs := cp.i, cp.rel, cp.abs
			res := map[string]any{"path": rel, "mode": map[bool]string{true: p.Mode, false: "edit"}[p.Mode != ""]}
			if cp.exists && p.Mode != "undo" {
				bk, err := backupConfig(i, rel, cp.raw)
				if err != nil {
					return nil, ops.Errorf(500, "备份原文件失败，没有修改：%v", err)
				}
				res["backup"] = bk
			}
			switch p.Mode {
			case "reset":
				if err := os.Remove(abs); err != nil {
					return nil, ops.Wrap(500, err)
				}
			default:
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					return nil, ops.Wrap(500, err)
				}
				data := cp.undo // 撤销：原样写回那份备份
				if p.Mode != "undo" {
					if data, err = cp.format.encode(cp.next); err != nil {
						return nil, err
					}
				}
				if err := writeAtomic(abs, data); err != nil {
					return nil, ops.Wrap(500, err)
				}
				if p.Mode == "undo" {
					_ = os.Remove(latestBackup(i, rel)) // 用掉的备份删掉：再撤销一次就回到更早的那份
				}
			}
			return res, nil
		},
	})
}

var configExts = map[string]bool{".toml": true, ".json": true, ".json5": true, ".jsonc": true, ".yml": true, ".yaml": true, ".cfg": true,
	".conf": true, ".properties": true, ".txt": true, ".ini": true, ".snbt": true}

// configRoots 可以读写的配置目录（相对实例目录）。
func configRoots(i *inst.Instance) []string {
	level := "world"
	if p, err := inst.LoadProps(i.PropsPath()); err == nil {
		if v, ok := p.Get("level-name"); ok && strings.TrimSpace(v) != "" && !strings.ContainsAny(v, `/\:`) {
			level = strings.TrimSpace(v)
		}
	}
	return []string{"config", "defaultconfigs", level + "/serverconfig", "plugins"}
}

// configPath 检查并解析配置文件的路径：只能在 configRoots 下面，不能往上跳、不能是绝对路径、不能经符号链接跑出实例目录；
// 文件（file 为 true 时必须是文件）的扩展名要在白名单里。plugins 下只认插件自己的子目录（不碰 jar）。
func configPath(i *inst.Instance, p string, file bool) (rel, abs string, err error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if p == "" || filepath.IsAbs(filepath.FromSlash(p)) || strings.Contains(p, ":") || strings.HasPrefix(p, "/") ||
		clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", ops.Errorf(400, "路径不对：要写相对实例目录的路径，如 config/xxx.toml")
	}
	ok := false
	for _, r := range configRoots(i) {
		if clean == r || strings.HasPrefix(clean, r+"/") {
			ok = true
			// plugins 下只认插件自己的文件夹（plugins/<插件>/…），直接放在 plugins 里的是 jar，不碰
			sub := strings.TrimPrefix(clean, "plugins/")
			if r == "plugins" && clean != "plugins" && !strings.Contains(sub, "/") && (file || filepath.Ext(sub) != "") {
				return "", "", ops.Errorf(400, "plugins 下只能读写插件自己文件夹里的配置，如 plugins/Essentials/config.yml")
			}
		}
	}
	if !ok {
		return "", "", ops.Errorf(400, "只能读写 %s 下的配置文件", strings.Join(configRoots(i), "、"))
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if (file || ext != "") && !configExts[ext] {
		if file || !isDir(filepath.Join(i.Dir, filepath.FromSlash(clean))) {
			return "", "", ops.Errorf(400, "不是配置文件（只认 .toml .json .yml .cfg .properties .txt 等）：%s", clean)
		}
	}
	abs = filepath.Join(i.Dir, filepath.FromSlash(clean))
	// 符号链接：解析出真实路径，必须还在实例目录里
	root, err1 := filepath.EvalSymlinks(i.Dir)
	if real, err2 := filepath.EvalSymlinks(abs); err1 == nil && err2 == nil {
		if r, err := filepath.Rel(root, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", "", ops.Errorf(400, "路径不对：指到了实例目录外面")
		}
	}
	return clean, abs, nil
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// listConfigs 列出目录下（最多四层）的配置文件。
func listConfigs(base, dir string, limit int) []map[string]any {
	out := []map[string]any{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= limit {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if !configExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(base, p)
		out = append(out, map[string]any{"path": filepath.ToSlash(r), "size": st.Size()})
		return nil
	})
	return out
}

// placeholders 新内容里有没有 MineDeck 给 AI 看的脱敏占位符（原文里本来没有的）：有的话说明 AI 把脱敏后的内容原样写回了。
var secretLineRe = regexp.MustCompile(`(?im)^\s*[\w.\-]*(password|passwd|secret|token|api[_-]?key|credential)[\w.\-]*\s*[=:]\s*["']?\*\*\*["']?\s*$`)

func placeholders(old, next string) string {
	var bad []string
	for _, ph := range []string{"<用户名>", "<局域网IP>", "<程序目录>", "<令牌>", "<计算机名>"} {
		if strings.Contains(next, ph) && !strings.Contains(old, ph) {
			bad = append(bad, ph)
		}
	}
	if len(secretLineRe.FindAllString(next, -1)) > len(secretLineRe.FindAllString(old, -1)) {
		bad = append(bad, "***")
	}
	return strings.Join(bad, "、")
}

func textHash(s string, exists bool) string {
	if !exists {
		return "无"
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// backupDir 实例的配置备份目录。
func backupDir(i *inst.Instance) string { return filepath.Join(i.Dir, ".minedeck", "config-backups") }

var unsafeName = regexp.MustCompile(`[^\w.\-]+`)

// backupConfig 备份一份配置文件：<实例>/.minedeck/config-backups/<路径>~<时间>，同一个文件最多留 20 份。返回备份的相对路径。
func backupConfig(i *inst.Instance, rel string, content []byte) (string, error) {
	dir := backupDir(i)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	key := unsafeName.ReplaceAllString(strings.ReplaceAll(rel, "/", "__"), "_")
	name := key + "~" + time.Now().Format("20060102-150405.000")
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		return "", err
	}
	if all := backupsOf(i, rel); len(all) > 20 {
		for _, old := range all[:len(all)-20] {
			_ = os.Remove(old)
		}
	}
	r, _ := filepath.Rel(i.Dir, filepath.Join(dir, name))
	return filepath.ToSlash(r), nil
}

// backupsOf 一个配置文件的全部备份，从旧到新。
func backupsOf(i *inst.Instance, rel string) []string {
	key := unsafeName.ReplaceAllString(strings.ReplaceAll(rel, "/", "__"), "_") + "~"
	ents, _ := os.ReadDir(backupDir(i))
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), key) {
			out = append(out, filepath.Join(backupDir(i), e.Name()))
		}
	}
	sort.Strings(out) // 名字里的时间可以按字典序排
	return out
}

func latestBackup(i *inst.Instance, rel string) string {
	if all := backupsOf(i, rel); len(all) > 0 {
		return all[len(all)-1]
	}
	return ""
}

// textFormat 配置文件原来的编码和换行。读给 AI 看时统一成 UTF-8 和 \n，写回时照原样还原：
// 不把 GBK 的配置改成 UTF-8（老版本 Java 在中文 Windows 上默认按 GBK 读插件配置），不丢 BOM，Windows 换行不变成 \n。
type textFormat struct{ gbk, bom, crlf bool }

var utf8BOM = []byte("\xef\xbb\xbf")

func sniffText(b []byte) textFormat {
	body := bytes.TrimPrefix(b, utf8BOM)
	return textFormat{gbk: !utf8.Valid(body), bom: len(body) < len(b), crlf: bytes.Contains(body, []byte("\r\n"))}
}

func (f textFormat) decode(b []byte) string { return strings.ReplaceAll(decodeText(b), "\r\n", "\n") }

func (f textFormat) encode(s string) ([]byte, error) {
	if f.crlf {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	if f.gbk {
		b, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
		if err != nil {
			return nil, ops.Errorf(400, "原文件是 GBK 编码，新内容里有 GBK 表示不了的字（如表情符号），写不进去")
		}
		return b, nil
	}
	if f.bom {
		return append(append([]byte{}, utf8BOM...), s...), nil
	}
	return []byte(s), nil
}

// writeAtomic 先写临时文件再改名，写到一半断电不会留下半个配置文件。
func writeAtomic(path string, b []byte) error {
	tmp := path + ".minedeck-tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
