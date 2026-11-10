package transfer

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// 这里放 transfer 自己用的一小段 server.properties / eula.txt 解析。不引用 inst 是刻意的：
// inst 要用这个包（导入导出流水线），反过来引用会成环。规则和 inst 一致：# 或 ! 开头是注释，
// key=value 各一行；BOM 去掉。

var bom = []byte("\xef\xbb\xbf")

// propsValue 从 server.properties 的原文里取一个键的值（去注释、去首尾空白）；没有这个键返回空串。
// 原版服务端按 Java Properties 存盘，非 ASCII 会写成 \uXXXX 转义（中文 level-name 就是这样），
// 这里照 inst 的做法反转义，worldDirs 才找得到真实目录名。
func propsValue(b []byte, key string) string {
	b = bytes.TrimPrefix(b, bom)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "!") {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(unescapeUnicode(v))
		}
	}
	return ""
}

// unescapeUnicode 把 properties 值里的 \uXXXX 换回字符；尾随不完整的转义原样保留。
func unescapeUnicode(s string) string {
	if !strings.Contains(s, "\\u") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+5 < len(s) && s[i+1] == 'u' {
			if v, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
				b.WriteRune(rune(v))
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// parseIntSafe 十进制整数，解析不了或非正返回 0。
func parseIntSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// reEULA eula.txt 里「eula=true」的识别（和 inst 接管那一套同一写法：大小写不敏感，允许空白）。
var reEULA = regexp.MustCompile(`(?im)^\s*eula\s*=\s*true\s*$`)

// eulaAgreed eula.txt 的内容是否已同意。
func eulaAgreed(b []byte) bool { return reEULA.Match(bytes.TrimPrefix(b, bom)) }
