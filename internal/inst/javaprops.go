package inst

import (
	"minedeck/internal/atomicfile"
	"os"
	"strconv"
	"strings"
)

// 按 Minecraft 服务端的读法读 server.properties。
//
// 服务端用 java.util.Properties 读这个文件，规矩和 MineDeck 自己的 Props（保序、保注释，方便改一行再写回）不一样：
// 同一个键写了几行的，Java 认最后一行，Props 认第一行；键和值之间除了 = 还可以用 : 或空白隔开；
// 反斜杠是转义（\. 就是 .），行尾的反斜杠把下一行接上来。平时这点差别无所谓，可 server.properties 可以是别人给的
// （交接包、整合包的覆盖目录）：照 Props 读是 level-name=world，服务端读到的却是另一行的 ../别的服务器，
// 「世界目录不能指到服务器文件夹外面」这道检查就白查了。所以凡是拿来做安全判断的值，都按这里的读法取。

// JavaProps 按 java.util.Properties.load 的规矩解析，返回每个键最终生效的值（后面的盖掉前面的）。
// 写坏了的 \uXXXX 在 Java 里会让整个文件读不进来；这里把那一个转义原样留着，不影响别的行。
func JavaProps(data []byte) map[string]string {
	out := map[string]string{}
	lines := splitNaturalLines(string(data))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue // 空行和注释行。注释只认逻辑行的开头：接上来的续行以 # 开头也算内容
		}
		for endsWithOddBackslashes(line) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimLeft(lines[i], " \t\f")
		}
		key, val := splitJavaKV(line)
		out[unescapeJava(key)] = unescapeJava(val)
	}
	return out
}

// splitNaturalLines 按 \n、\r、\r\n 三种行尾切开。
func splitNaturalLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func endsWithOddBackslashes(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// splitJavaKV 把一条逻辑行切成键和值（都还没解转义）：键到第一个没被反斜杠转义的 =、: 或空白为止；
// 之后跳过空白，再跳过至多一个 = 或 : 和它后面的空白，剩下的是值。
func splitJavaKV(line string) (key, val string) {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\f' }
	keyLen, valStart, hasSep, escaped := 0, len(line), false, false
	for keyLen < len(line) {
		c := line[keyLen]
		if (c == '=' || c == ':') && !escaped {
			valStart, hasSep = keyLen+1, true
			break
		}
		if isSpace(c) && !escaped {
			valStart = keyLen + 1
			break
		}
		escaped = c == '\\' && !escaped
		keyLen++
	}
	for valStart < len(line) {
		c := line[valStart]
		if !isSpace(c) {
			if !hasSep && (c == '=' || c == ':') {
				hasSep = true
			} else {
				break
			}
		}
		valStart++
	}
	return line[:keyLen], line[valStart:]
}

// unescapeJava 解 Properties 的转义：\t \n \r \f、\uXXXX，别的 \x 就是 x 本身。
func unescapeJava(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			if c != '\\' { // 落单在末尾的反斜杠：丢掉
				sb.WriteByte(c)
			}
			continue
		}
		i++
		switch s[i] {
		case 't':
			sb.WriteByte('\t')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 'f':
			sb.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if code, err := strconv.ParseUint(s[i+1:i+5], 16, 16); err == nil {
					sb.WriteRune(rune(code))
					i += 4
					continue
				}
			}
			sb.WriteString(`\u`)
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// javaPropsOf 读 path 这个 properties 文件在 Java 眼里的样子；文件不存在或读不了返回 nil。
func javaPropsOf(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return JavaProps(b)
}

// ForceProp 把 path 里的 key 改成 value，并且保证服务端读到的就是它：先照常改（改的是第一处），
// 再按 Java 的读法读回来看一眼——同一个键还有别的行、或者用了别的写法，服务端认的不是这一行时，
// 在文件末尾再补一行（Java 认最后一行）。文件不存在时新建。
func ForceProp(path, key, value string) error {
	defer atomicfile.LockPath(path)() // 和 UpdateProps、别的 ForceProp 排队：这里也是读、改、写
	p, err := LoadProps(path)
	if err != nil {
		return err
	}
	p.Set(key, value)
	if err := p.Save(path); err != nil {
		return err
	}
	if javaPropsOf(path)[key] == value {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	// 上一行要是以反斜杠结尾，会把补的这一行接成它的续行：先垫一个空行隔开
	s += "\n" + key + "=" + escapeUnicode(value) + "\n"
	return writeFileAtomic(path, []byte(s))
}
