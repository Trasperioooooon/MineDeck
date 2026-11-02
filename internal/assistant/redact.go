package assistant

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"minedeck/internal/ops"
)

// Redactor 发给 AI 之前的脱敏（设计文档 §5.3、§10.1）：路径里的 Windows 用户名、程序目录、局域网 IP、计算机名、
// MineDeck 的令牌和已知的 Key 换掉，长得像 Key 的长串和配置里像密码的值写成 ***。玩家名保留，排错时用得到。
//
// 工具结果、用户的话、打包的排错资料都经它处理；发送预览里看到的就是处理后的样子。
type Redactor struct {
	Base     string          // 程序目录
	Home     string          // 当前 Windows 用户的主目录
	User     string          // Windows 用户名
	Computer string          // 计算机名
	IPs      []string        // 本机各网卡的地址
	Secrets  func() []string // 已知的密钥（MineDeck 令牌、AI 和 CurseForge 的 Key 等），原样出现就换成 ***
}

// 占位符。
const (
	phUser     = "<用户名>"
	phBase     = "<程序目录>"
	phHome     = "<用户目录>"
	phIP       = "<局域网IP>"
	phComputer = "<计算机名>"
)

var (
	// C:\Users\alice\…、C:/Users/alice/…、/home/alice/…：只换用户名那一段
	userPathRe = regexp.MustCompile(`(?i)([A-Z]:[\\/]+(?:Users|Documents and Settings)[\\/]+)([^\\/\s"'<>|:*?]+)`)
	homePathRe = regexp.MustCompile(`(/home/|/Users/)([^/\s"'<>|:*?]+)`)
	// 环境变量和 Java 系统属性里的用户名、计算机名（hs_err、崩溃报告里会有）
	userVarRe = regexp.MustCompile(`(?i)\b(USERNAME|USER|LOGNAME|user\.name)(\s*[=:]\s*)([^\s,;"']+)`)
	compVarRe = regexp.MustCompile(`(?i)\b(COMPUTERNAME|USERDOMAIN|HOSTNAME)(\s*[=:]\s*)([^\s,;"']+)`)
	ipv4Re    = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)
	ipv6Re    = regexp.MustCompile(`(?i)\b[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}(?:%[\w.]+)?`)
	// 长得像 Key 的长串：各家 API Key、bcrypt、GitHub 令牌、JWT、Discord 机器人令牌，以及 40 位以上大小写字母和数字混着的随机串
	keyRes = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:sk|pk|rk|ak)-[A-Za-z0-9_\-]{16,}`),
		regexp.MustCompile(`\$2[aby]?\$\d\d\$[./A-Za-z0-9]{53}`),
		regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`),
		regexp.MustCompile(`\b[A-Fa-f0-9]{32}\.[A-Za-z0-9]{16}\b`),
		regexp.MustCompile(`\b[MNO][A-Za-z\d]{23,27}\.[\w\-]{6}\.[\w\-]{27,40}\b`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	}
	longTokenRe = regexp.MustCompile(`[A-Za-z0-9_\-+=]{40,}`)
	// 配置文本里像密码的那一行：「token = "…"」「"api_key": "…",」「password: …」
	secretLineRe = regexp.MustCompile(`(?im)^(\s*["']?[\w.\-]*(?:password|passwd|passphrase|secret|token|api[_\-]?key|apikey|credential|webhook)[\w.\-]*["']?\s*[=:]\s*)(.*?)(,?)[ \t]*$`)
)

// Text 处理一段文本。
func (r *Redactor) Text(s string) string {
	if r == nil || s == "" {
		return s
	}
	return r.text(s, r.secrets())
}

// secrets 取一次已知的密钥（Secrets 每次都要读 secrets.json、解密：处理一整个 JSON 时只取一次）。
func (r *Redactor) secrets() []string {
	if r == nil || r.Secrets == nil {
		return nil
	}
	return r.Secrets()
}

func (r *Redactor) text(s string, secrets []string) string {
	if s == "" {
		return s
	}
	for _, sec := range secrets {
		if len(sec) >= 6 {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	// 程序目录、主目录（大小写不敏感，两种斜杠都认）；先换长的
	type lit struct{ from, to string }
	var lits []lit
	if r.Base != "" {
		lits = append(lits, lit{r.Base, phBase})
	}
	if r.Home != "" {
		lits = append(lits, lit{r.Home, phHome})
	}
	sort.Slice(lits, func(i, j int) bool { return len(lits[i].from) > len(lits[j].from) })
	for _, l := range lits {
		for _, form := range []string{l.from, strings.ReplaceAll(l.from, `\`, "/"), strings.ReplaceAll(l.from, `\`, `\\`)} {
			s = replaceFold(s, form, l.to)
		}
	}
	s = userPathRe.ReplaceAllString(s, "${1}"+phUser)
	s = homePathRe.ReplaceAllString(s, "${1}"+phUser)
	s = userVarRe.ReplaceAllString(s, "${1}${2}"+phUser)
	s = compVarRe.ReplaceAllString(s, "${1}${2}"+phComputer)
	if len(r.Computer) >= 4 {
		s = replaceFold(s, r.Computer, phComputer)
	}
	s = ipv4Re.ReplaceAllStringFunc(s, func(ip string) string {
		if r.privateIP(ip) {
			return phIP
		}
		return ip
	})
	if strings.Count(s, ":") >= 2 {
		s = ipv6Re.ReplaceAllStringFunc(s, func(cand string) string {
			if r.privateIP(cand) {
				return phIP
			}
			return cand
		})
	}
	for _, re := range keyRes {
		s = re.ReplaceAllString(s, "***")
	}
	s = longTokenRe.ReplaceAllStringFunc(s, func(t string) string {
		if looksRandom(t) {
			return "***"
		}
		return t
	})
	s = secretLineRe.ReplaceAllStringFunc(s, func(line string) string {
		m := secretLineRe.FindStringSubmatch(line)
		v := strings.TrimSpace(m[2])
		if v == "" || v == `""` || v == "''" || v == "true" || v == "false" || isNumber(v) || v == "***" || v == `"***"` {
			return line
		}
		q := ""
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			q = v[:1]
		}
		return m[1] + q + "***" + q + m[3]
	})
	return s
}

// privateIP 局域网地址（私有网段、链路本地、运营商级 NAT、IPv6 的 ULA 和链路本地）或本机网卡的地址。
// 回环地址和 0.0.0.0 保留：排查端口问题时用得到。
func (r *Redactor) privateIP(s string) bool {
	host := s
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, own := range r.IPs {
		if o, err := netip.ParseAddr(own); err == nil && o == a.Unmap() && !a.IsLoopback() && !a.IsUnspecified() {
			return true
		}
	}
	if a.IsLoopback() || a.IsUnspecified() {
		return false
	}
	if a.IsPrivate() || a.IsLinkLocalUnicast() {
		return true
	}
	if a.Is4() {
		b := a.As4()
		return b[0] == 100 && b[1] >= 64 && b[1] <= 127 // 100.64.0.0/10（Tailscale、ZeroTier 一类也在这里）
	}
	return false
}

// looksRandom 长串里大写、小写、数字都有：多半是密钥（Java 类名、路径、十六进制哈希不会三样都有）。
func looksRandom(t string) bool {
	var up, low, dig bool
	for _, c := range t {
		switch {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			low = true
		case c >= '0' && c <= '9':
			dig = true
		}
	}
	return up && low && dig
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// replaceFold 不分大小写地替换（Windows 的路径不分大小写）。
func replaceFold(s, old, new string) string {
	if old == "" {
		return s
	}
	lower, lo := strings.ToLower(s), strings.ToLower(old)
	if len(lower) != len(s) || len(lo) != len(old) { // 大小写转换改变了长度（少见的字符）：只做精确替换
		return strings.ReplaceAll(s, old, new)
	}
	if !strings.Contains(lower, lo) {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(lower[i:], lo)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(new)
		i += j + len(lo)
	}
}

// Value 处理 JSON 解出来的值：先按键名把密钥类的值写成 ***（同操作记录），再处理每个字符串。
func (r *Redactor) Value(v any) any {
	v = ops.Redact(v)
	if r == nil {
		return v
	}
	return r.walk(v, r.secrets())
}

func (r *Redactor) walk(v any, secrets []string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = r.walk(val, secrets)
		}
	case []any:
		for i, val := range x {
			x[i] = r.walk(val, secrets)
		}
	case string:
		return r.text(x, secrets)
	}
	return v
}

// JSON 把任意值编成脱敏后的紧凑 JSON（不转义 < > &）。
func (r *Redactor) JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return r.Text(err.Error())
	}
	var x any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&x) != nil {
		return r.Text(string(b))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(r.Value(x))
	return strings.TrimRight(buf.String(), "\n")
}
