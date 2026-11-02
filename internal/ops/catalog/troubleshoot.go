package catalog

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"minedeck/internal/app"
	"minedeck/internal/diag"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modinfo"
	"minedeck/internal/ops"
)

// troubleshootOps 排错用的读操作（设计文档 §5.2、§5.3）：控制台、日志文件、崩溃报告、本地规则排错。
func (c *cat) troubleshootOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.console.tail", Title: "查看控制台输出", Level: ops.Read,
		Desc: "实例控制台最近的输出（MineDeck 内存里最多保留 3000 行）。可以只看 WARN / ERROR，按关键字过滤，或只看最近一次启动以来的。",
		Params: withInst(map[string]*ops.Schema{
			"lines": ops.Integer("最多返回几行，默认 100（结果超过 16 KB 会被截断）").Range(1, 3000),
			"level": ops.String("只看这一级及更严重的：warn（WARN 和 ERROR）、error（ERROR、异常和堆栈）；空串不过滤").OneOf("", "warn", "error"),
			"q":     ops.String("关键字（不分大小写）").Len(0, 100),
			"since": ops.String("start：只看最近一次启动以来的；空串看全部").OneOf("", "start"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst  string `json:"inst"`
				Lines int    `json:"lines"`
				Level string `json:"level"`
				Q     string `json:"q"`
				Since string `json:"since"`
			}](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			all := i.Console.Lines()
			src := all
			if p.Since == "start" {
				src = diag.SinceStart(all)
			}
			q := strings.ToLower(strings.TrimSpace(p.Q))
			var out []string
			for _, l := range src {
				lv := diag.LineLevel(l)
				switch {
				case p.Level == "warn" && lv == "":
					continue
				case p.Level == "error" && lv != "error":
					continue
				case q != "" && !strings.Contains(strings.ToLower(l), q):
					continue
				}
				out = append(out, l)
			}
			n := p.Lines
			if n <= 0 {
				n = 100
			}
			matched := len(out)
			if len(out) > n {
				out = out[len(out)-n:]
			}
			if out == nil {
				out = []string{}
			}
			return map[string]any{"status": i.Status(), "total": len(all), "matched": matched, "lines": out, "truncated": matched > len(out)}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.logs.read", Title: "查看日志文件", Level: ops.Read,
		Desc: "读实例的 logs/latest.log（本次运行的日志）或 logs/debug.log（Forge / NeoForge 的详细日志）的末尾，或者在里面按关键字搜索。",
		Params: withInst(map[string]*ops.Schema{
			"file":  ops.String("latest 或 debug，默认 latest").OneOf("", "latest", "debug"),
			"lines": ops.Integer("读末尾几行，默认 200").Range(1, 1000),
			"q":     ops.String("关键字（不分大小写）：给了就改成搜索，返回最多 50 处，每处带前后 2 行").Len(0, 100),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst  string `json:"inst"`
				File  string `json:"file"`
				Lines int    `json:"lines"`
				Q     string `json:"q"`
			}](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			name := "latest.log"
			if p.File == "debug" {
				name = "debug.log"
			}
			path := filepath.Join(i.Dir, "logs", name)
			st, err := os.Stat(path)
			if err != nil {
				return nil, ops.Errorf(404, "还没有 logs/%s（服务器可能还没启动过）", name)
			}
			res := map[string]any{"file": "logs/" + name, "size": st.Size(), "modified": st.ModTime().Format("2006-01-02 15:04:05")}
			if q := strings.TrimSpace(p.Q); q != "" {
				matches, scanned, err := searchFile(path, q, 50, 2)
				if err != nil {
					return nil, ops.Wrap(500, err)
				}
				res["q"], res["matches"], res["scannedAll"] = q, matches, scanned
				return res, nil
			}
			n := p.Lines
			if n <= 0 {
				n = 200
			}
			lines, err := tailFile(path, n)
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			res["lines"] = lines
			return res, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.crashes", Title: "查看崩溃报告", Level: ops.Read,
		Desc: "不给 file 时列出 crash-reports/ 下的崩溃报告和实例目录里的 hs_err_pid*.log（Java 虚拟机自己崩溃时写的），新的在前；" +
			"给了 file 就读这一份：默认只给节选（开头、第一个异常、可疑模组、版本和内存），full 为 true 时给原文的前一部分。",
		Params: withInst(map[string]*ops.Schema{
			"file": ops.String("文件名（列表里的 name），如 crash-2026-10-28_13.10.44-server.txt 或 hs_err_pid1234.log").Len(0, 200),
			"full": ops.Boolean("读原文（只给前 16 KB 左右）"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst string `json:"inst"`
				File string `json:"file"`
				Full bool   `json:"full"`
			}](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(p.File) == "" {
				list := crashFiles(i)
				if len(list) > 20 {
					list = list[:20]
				}
				return map[string]any{"reports": list}, nil
			}
			path, kind, err := crashPath(i, p.File)
			if err != nil {
				return nil, err
			}
			b, err := readLimited(path, 4<<20)
			if err != nil {
				return nil, ops.Errorf(404, "读不到 %s：%v", p.File, err)
			}
			text := decodeText(b)
			out := map[string]any{"name": filepath.Base(path), "kind": kind, "size": len(b)}
			switch {
			case p.Full:
				if len(text) > 16000 {
					text = text[:16000] + "\n……（后面还有，没有给出）"
				}
				out["content"] = text
			case kind == "jvm":
				out["summary"] = diag.HsErrSummary(text)
			default:
				out["summary"] = diag.CrashSummary(text)
			}
			return out, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.diag", Title: "本地规则排错", Level: ops.Read,
		Desc: "用 MineDeck 内置的规则检查最近一次启动以来的控制台输出（不联网，也不花钱）：缺少前置、客户端模组、装错加载器、重复模组、" +
			"Mixin 冲突、Java 版本、端口被占、内存不够、看门狗、EULA、好友进不来、配置文件坏了等。返回认出的问题和修复办法。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			rep := diagnose(i)
			return map[string]any{"status": i.Status(), "findings": rep.Findings, "crashed": rep.Crashed, "exit": rep.Exit, "lines": rep.Lines}, nil
		},
	})
}

// diagnose 对实例最近一次启动以来的控制台跑一遍本地规则。
func diagnose(i *inst.Instance) diag.Report {
	xmx, _, _ := i.MemSnapshot()
	major, _ := i.JavaSnapshot()
	in := diag.Input{Lines: diag.SinceStart(i.Console.Lines()), Core: string(i.Core), MC: i.MC, JavaMajor: major, XmxMB: xmx,
		RAMMB: app.TotalRAMMB(), AvailMB: app.AvailRAMMB(), Port: i.Port()}
	if dir := modsDir(i); dir == "mods" {
		in.Mods, _ = modinfo.ReadDir(filepath.Join(i.Dir, dir))
		in.FindClass = func(class string) string { return modinfo.FindClass(filepath.Join(i.Dir, dir), class) }
	}
	return diag.Analyze(in)
}

// modsDir 实例装模组或插件的目录（mods / plugins）；代理端和原版没有，返回空串。
func modsDir(i *inst.Instance) string {
	if _, _, dir, ok := resourceSpec(i.Core); ok {
		return dir
	}
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return "plugins"
	}
	return ""
}

// crashReport 崩溃报告列表的一项。
type crashReport struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // crash：游戏的崩溃报告；jvm：Java 虚拟机崩溃的 hs_err
	Size      int64  `json:"size"`
	Modified  string `json:"modified"`
	ThisStart bool   `json:"thisStart"` // 在最近一次启动之后写的
	mod       time.Time
}

var (
	crashName = regexp.MustCompile(`^crash-[\w.\-]+\.txt$`)
	hsErrName = regexp.MustCompile(`^hs_err_pid\d+\.log$`)
)

func crashFiles(i *inst.Instance) []crashReport {
	since := i.LastStart()
	var out []crashReport
	add := func(dir, kind string, re *regexp.Regexp) {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() || !re.MatchString(e.Name()) {
				continue
			}
			st, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, crashReport{Name: e.Name(), Kind: kind, Size: st.Size(), Modified: st.ModTime().Format("2006-01-02 15:04:05"),
				ThisStart: !since.IsZero() && !st.ModTime().Before(since), mod: st.ModTime()})
		}
	}
	add(filepath.Join(i.Dir, "crash-reports"), "crash", crashName)
	add(i.Dir, "jvm", hsErrName)
	sort.Slice(out, func(a, b int) bool { return out[a].mod.After(out[b].mod) })
	if out == nil {
		out = []crashReport{}
	}
	return out
}

// crashPath 按文件名找崩溃报告：只认 crash-reports/crash-*.txt 和实例目录下的 hs_err_pid*.log。
func crashPath(i *inst.Instance, name string) (string, string, error) {
	name = filepath.Base(strings.TrimSpace(name))
	switch {
	case crashName.MatchString(name):
		return filepath.Join(i.Dir, "crash-reports", name), "crash", nil
	case hsErrName.MatchString(name):
		return filepath.Join(i.Dir, name), "jvm", nil
	}
	return "", "", ops.Errorf(400, "只能读 crash-reports 里的 crash-*.txt 和 hs_err_pid*.log")
}

// readLimited 读文件，最多 max 字节。
func readLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// decodeText 文本解码：合法的 UTF-8 原样用，否则按 GBK 解（老版本 Forge 在中文 Windows 上写的日志）。
func decodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if utf8.Valid(b) {
		return string(b)
	}
	if d, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
		return string(d)
	}
	return strings.ToValidUTF8(string(b), "?")
}

// tailFile 读文件最后 n 行：从末尾按块往前读，最多读 8 MB。
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const chunk = 64 << 10
	end := st.Size()
	var buf []byte
	for end > 0 && bytes.Count(buf, []byte("\n")) <= n && len(buf) < 8<<20 {
		start := max(0, end-chunk)
		part := make([]byte, end-start)
		if _, err := f.ReadAt(part, start); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(part, buf...)
		end = start
	}
	lines := strings.Split(strings.ReplaceAll(decodeText(buf), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if end > 0 && len(lines) > 0 {
		lines = lines[1:] // 第一行可能只读到半截
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, nil
}

// logMatch 搜索日志的一处结果。
type logMatch struct {
	Line   int      `json:"line"` // 行号（从 1 起）
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"` // 前面几行
	After  []string `json:"after,omitempty"`  // 后面几行
}

// searchFile 在文件里按关键字（不分大小写）搜索，最多 limit 处、每处带前后 ctx 行。最多扫 64 MB，scannedAll 表示扫完了。
func searchFile(path, q string, limit, ctx int) (out []logMatch, scannedAll bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	q = strings.ToLower(q)
	sc := bufio.NewScanner(io.LimitReader(f, 64<<20))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var prev []string
	var open []int // 还在收「后面几行」的结果的下标
	n := 0
	for sc.Scan() {
		n++
		l := strings.TrimRight(decodeText(sc.Bytes()), "\r")
		for _, k := range open {
			out[k].After = append(out[k].After, l)
		}
		open = slices.DeleteFunc(open, func(k int) bool { return len(out[k].After) >= ctx })
		if strings.Contains(strings.ToLower(l), q) {
			if len(out) >= limit {
				return out, false, nil
			}
			out = append(out, logMatch{Line: n, Text: l, Before: slices.Clone(prev)})
			open = append(open, len(out)-1)
		}
		prev = append(prev, l)
		if len(prev) > ctx {
			prev = prev[1:]
		}
	}
	if out == nil {
		out = []logMatch{}
	}
	st, _ := f.Stat()
	return out, sc.Err() == nil && (st == nil || st.Size() <= 64<<20), sc.Err()
}

// fmtSize 大小写成人能读的样子。
func fmtSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
