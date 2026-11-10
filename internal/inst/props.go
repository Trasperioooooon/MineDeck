package inst

import (
	"fmt"
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	os.Remove(path)
	return os.Rename(tmp, path)
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
