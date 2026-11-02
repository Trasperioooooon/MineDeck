package bridge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Props 按行读写 Java 的 .properties 文件：只改要改的键，注释、空行和别的键原样保留，换行符和 BOM 也照旧。
// 读的时候按 java.util.Properties.load 的规则：# 和 ! 开头是注释；键和值之间用 =、: 或空白分隔；
// 行尾单个反斜杠表示续行；值里的 \: \= \\ \t \n \uXXXX 等转义会还原。
//
// 桥接模组和 SVC 都用 UTF-8 读写（桥接模组明确按 UTF-8 读，带不带 BOM 都行），所以这里不做 ISO-8859-1 的 \u 转义。
type Props struct {
	lines []string // 自然行，不含换行符
	crlf  bool
	bom   bool
}

// NewProps 一份空的 .properties；crlf 为 true 时用 Windows 换行（用记事本打开也正常）。
func NewProps(crlf bool) *Props { return &Props{crlf: crlf} }

// ParseProps 解析 .properties 的内容。
func ParseProps(data []byte) *Props {
	p := &Props{}
	if bytes.HasPrefix(data, []byte("\uFEFF")) {
		p.bom = true
		data = data[3:]
	}
	s := string(data)
	p.crlf = strings.Contains(s, "\r\n")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s != "" {
		p.lines = strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	return p
}

// LoadProps 读文件；文件不存在时返回的错误满足 os.IsNotExist。
func LoadProps(path string) (*Props, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseProps(b), nil
}

// entry 一个逻辑行（可能由几条自然行续成）。
type entry struct {
	first, last int // 自然行的下标范围（含两端）
	key, val    string
}

// entries 列出全部键值逻辑行。
func (p *Props) entries() []entry {
	var out []entry
	for i := 0; i < len(p.lines); i++ {
		t := strings.TrimLeft(p.lines[i], " \t\f")
		if t == "" || t[0] == '#' || t[0] == '!' {
			continue
		}
		first := i
		logical := t
		for continued(logical) && i+1 < len(p.lines) {
			i++
			logical = logical[:len(logical)-1] + strings.TrimLeft(p.lines[i], " \t\f")
		}
		if continued(logical) {
			logical = logical[:len(logical)-1]
		}
		k, v := splitKV(logical)
		out = append(out, entry{first: first, last: i, key: k, val: v})
	}
	return out
}

// continued 行尾有奇数个反斜杠：下一行接着这一行。
func continued(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// splitKV 按 Properties.load 的规则拆出键和值，并还原转义：键到第一个没转义的 =、: 或空白为止；
// 分隔符是空白时，后面还可以再跟一个 = 或 :；值前面的空白都去掉。
func splitKV(s string) (string, string) {
	ws := func(c byte) bool { return c == ' ' || c == '\t' || c == '\f' }
	i := 0
	for i < len(s) && s[i] != '=' && s[i] != ':' && !ws(s[i]) {
		if s[i] == '\\' {
			i++
		}
		i++
	}
	i = min(i, len(s))
	key, j := s[:i], i
	if j < len(s) {
		if s[j] == '=' || s[j] == ':' {
			j++
		} else {
			for j < len(s) && ws(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '=' || s[j] == ':') {
				j++
			}
		}
	}
	for j < len(s) && ws(s[j]) {
		j++
	}
	return unescapeProp(key), unescapeProp(s[j:])
}

func unescapeProp(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 16); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// escapeProp 写值时的转义：反斜杠、换行这类控制字符，以及开头的空格（否则读回来会被吃掉）。
// = 和 : 在值里不必转义，按原样写，人看着也清楚。
func escapeProp(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\f':
			b.WriteString(`\f`)
		case ' ':
			if i == 0 {
				b.WriteString(`\ `)
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Get 取一个键的值（同一个键出现多次时取最后一个，和 Properties.load 一样）。
func (p *Props) Get(key string) (string, bool) {
	val, ok := "", false
	for _, e := range p.entries() {
		if e.key == key {
			val, ok = e.val, true
		}
	}
	return val, ok
}

// Set 改一个键：已有的换成 key=value（重复出现的只留最后一个的位置），没有就追加在末尾。返回值有没有变。
func (p *Props) Set(key, value string) bool {
	if old, ok := p.Get(key); ok && old == value && p.count(key) == 1 {
		return false
	}
	line := key + "=" + escapeProp(value)
	es := p.entries()
	last := -1
	for i, e := range es {
		if e.key == key {
			last = i
		}
	}
	if last < 0 {
		// 追加在最后一个非空行之后
		end := len(p.lines)
		for end > 0 && strings.TrimSpace(p.lines[end-1]) == "" {
			end--
		}
		p.lines = append(p.lines[:end:end], append([]string{line}, p.lines[end:]...)...)
		return true
	}
	// 从后往前删掉重复的，再替换最后一个
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		if e.key != key {
			continue
		}
		repl := []string{}
		if i == last {
			repl = []string{line}
		}
		p.lines = append(p.lines[:e.first:e.first], append(repl, p.lines[e.last+1:]...)...)
	}
	return true
}

// Keys 全部键，按第一次出现的顺序，重复的只算一个。
func (p *Props) Keys() []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range p.entries() {
		if !seen[e.key] {
			seen[e.key] = true
			out = append(out, e.key)
		}
	}
	return out
}

// Delete 删掉一个键（出现几次都删，续行一起删），注释和别的行原样保留。返回有没有删。
func (p *Props) Delete(key string) bool {
	es := p.entries()
	found := false
	for i := len(es) - 1; i >= 0; i-- {
		if e := es[i]; e.key == key {
			p.lines = append(p.lines[:e.first:e.first], p.lines[e.last+1:]...)
			found = true
		}
	}
	return found
}

func (p *Props) count(key string) int {
	n := 0
	for _, e := range p.entries() {
		if e.key == key {
			n++
		}
	}
	return n
}

// Comment 在末尾追加一行注释（只在新建文件时用）。
func (p *Props) Comment(text string) {
	p.lines = append(p.lines, "# "+text)
}

// Bytes 编码回文件内容。
func (p *Props) Bytes() []byte {
	nl := "\n"
	if p.crlf {
		nl = "\r\n"
	}
	var b bytes.Buffer
	if p.bom {
		b.WriteString("\uFEFF")
	}
	for _, l := range p.lines {
		b.WriteString(l)
		b.WriteString(nl)
	}
	return b.Bytes()
}

// WriteFileAtomic 先写同目录下的临时文件，再改名替换：读的一方（桥接模组每 2 秒读一次）不会读到写了一半的文件。
// Windows 上目标文件偶尔被别的程序（杀毒软件、索引服务）短暂占着，改名失败时稍等重试几次。
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if err := f.Sync(); werr == nil {
		werr = err
	}
	if err := f.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	for i := 0; ; i++ {
		err = os.Rename(tmp, path)
		if err == nil || i == 5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入 %s 失败：%w", filepath.Base(path), err)
	}
	return nil
}
