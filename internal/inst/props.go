package inst

import (
	"path/filepath"
	"sync"

	"fmt"
	"minedeck/internal/atomicfile"
	"os"
	"strings"
)

// Props 保序解析/写回 server.properties，保留注释行。
// 值按 Java Properties 规范对非 ASCII 字符做 \uXXXX 转义（MOTD 中文兼容）。
type propLine struct {
	raw  string
	key  string
	val  string
	isKV bool
}

type Props struct {
	lines []propLine
}

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func LoadProps(path string) (*Props, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Props{}, nil
		}
		return nil, err
	}
	return ParseProps(b), nil
}

// ParseProps 解析一段 server.properties 文本（交接包读包里的端口这类「不落盘」的场景用）。
func ParseProps(b []byte) *Props {
	p := &Props{}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			p.lines = append(p.lines, propLine{raw: line})
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			p.lines = append(p.lines, propLine{raw: line})
			continue
		}
		p.lines = append(p.lines, propLine{
			key: strings.TrimSpace(k), val: unescapeUnicode(strings.TrimSpace(v)), isKV: true,
		})
	}
	// 去掉末尾空行
	for len(p.lines) > 0 && !p.lines[len(p.lines)-1].isKV && strings.TrimSpace(p.lines[len(p.lines)-1].raw) == "" {
		p.lines = p.lines[:len(p.lines)-1]
	}
	return p
}

func (p *Props) Get(key string) (string, bool) {
	for _, l := range p.lines {
		if l.isKV && l.key == key {
			return l.val, true
		}
	}
	return "", false
}

func (p *Props) Set(key, val string) {
	for i := range p.lines {
		if p.lines[i].isKV && p.lines[i].key == key {
			p.lines[i].val = val
			return
		}
	}
	p.lines = append(p.lines, propLine{key: key, val: val, isKV: true})
}

func (p *Props) Pairs() []KV {
	var out []KV
	for _, l := range p.lines {
		if l.isKV {
			out = append(out, KV{Key: l.key, Value: l.val})
		}
	}
	return out
}

func (p *Props) Save(path string) error {
	var sb strings.Builder
	for _, l := range p.lines {
		if l.isKV {
			sb.WriteString(l.key)
			sb.WriteString("=")
			sb.WriteString(escapeUnicode(l.val))
		} else {
			sb.WriteString(l.raw)
		}
		sb.WriteString("\n")
	}
	return writeFileAtomic(path, []byte(sb.String()))
}

// writeFileAtomic 把 data 写成 path：写好、刷盘、一步改名盖掉原文件，任何时刻 path 上都是完整的一份。
// 做法和它管不了的事见 atomicfile.Write。
//
// 这个函数以前是「写临时文件、删掉原文件、再改名」：删和改名之间程序退出或者断电，server.properties 就没了，
// 服务器下次按默认设置启动，level-name 是自定义的服务器会另生成一张叫 world 的新地图（2026-11-14 Codex 合并复审第 4 条）。
func writeFileAtomic(path string, data []byte) error {
	return atomicfile.Write(path, data, 0o644)
}

// 同一个文件的「读、改、写」要串起来：两个请求同时改 server.properties，各自读到的都是改之前的内容，
// 后存的那一份会把先存的改动盖掉。临时文件名不同只保证存下去的每一份是完整的，保证不了这个。
// 登记（UseWorld）不管这件事：它故意让几个保存可以同时进行。
var fileLocks sync.Map // 键 → *sync.Mutex。键的数量就是服务器和配置文件的数量，不回收

// LockKey 按键加锁，返回解锁的函数。锁不可重入：拿着同一个键的锁时不能再去拿它（UpdateProps 的回调里不能调 ForceProp）。
func LockKey(key string) (unlock func()) {
	v, _ := fileLocks.LoadOrStore(key, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func propsKey(path string) string { return "props|" + strings.ToLower(filepath.Clean(path)) }

// UpdateProps 读出 path 这份 server.properties，交给 fn 改，再存回去；整个过程和别的 UpdateProps、ForceProp 排队。
// fn 返回错误就不存。
func UpdateProps(path string, fn func(p *Props) error) error {
	defer LockKey(propsKey(path))()
	p, err := LoadProps(path)
	if err != nil {
		return err
	}
	if err := fn(p); err != nil {
		return err
	}
	return p.Save(path)
}

func escapeUnicode(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			sb.WriteRune(r)
		case r > 0xFFFF:
			// Java Properties 的 \uXXXX 只有 4 位，超出 BMP 的字符（如 Emoji）需按 UTF-16 代理对编码
			v := r - 0x10000
			fmt.Fprintf(&sb, "\\u%04x\\u%04x", 0xD800+(v>>10), 0xDC00+(v&0x3FF))
		default:
			fmt.Fprintf(&sb, "\\u%04x", r)
		}
	}
	return sb.String()
}

func unescapeUnicode(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		if c, n, ok := parseUEscape(s, i); ok {
			// 合并 UTF-16 高/低代理对
			if c >= 0xD800 && c <= 0xDBFF {
				if c2, n2, ok2 := parseUEscape(s, i+n); ok2 && c2 >= 0xDC00 && c2 <= 0xDFFF {
					sb.WriteRune(((c-0xD800)<<10 | (c2 - 0xDC00)) + 0x10000)
					i += n + n2
					continue
				}
			}
			sb.WriteRune(c)
			i += n
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func parseUEscape(s string, i int) (rune, int, bool) {
	if i+6 <= len(s) && s[i] == '\\' && (s[i+1] == 'u' || s[i+1] == 'U') {
		var code rune
		if _, err := fmt.Sscanf(s[i+2:i+6], "%04x", &code); err == nil {
			return code, 6, true
		}
	}
	return 0, 0, false
}
