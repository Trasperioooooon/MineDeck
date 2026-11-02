package ops

import (
	"fmt"
	"strings"
)

// LineDiff 逐行对比两段文本，给写配置文件这类操作的预览用：没变的行最多保留前后 ctx 行作上下文，
// 删掉的行以「- 」开头，加上的行以「+ 」开头，上下文以「  」开头，隔开的两段之间是「@@ 第 N 行 @@」。
// 输出最多 maxLines 行，超出时注明省略了多少。两段相同时返回空串。
func LineDiff(a, b string, ctx, maxLines int) string {
	if a == b {
		return ""
	}
	x, y := splitLines(a), splitLines(b)
	// 先去掉相同的开头和结尾，中间那段再求最长公共子序列（改动一般只有几行，中间很短）
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	type line struct {
		op   byte // ' ' 不变、'-' 删、'+' 加
		text string
		no   int // 在旧文本里的行号（从 1 起；加的行记下一行的行号）
	}
	var ls []line
	for i := 0; i < pre; i++ {
		ls = append(ls, line{' ', x[i], i + 1})
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]
	if len(mx)*len(my) > 4_000_000 { // 太大就不求公共子序列：整段删了再加
		for i, s := range mx {
			ls = append(ls, line{'-', s, pre + i + 1})
		}
		for _, s := range my {
			ls = append(ls, line{'+', s, pre + len(mx) + 1})
		}
	} else {
		// dp[i][j]：mx[i:] 和 my[j:] 的最长公共子序列长度
		dp := make([][]int32, len(mx)+1)
		for i := range dp {
			dp[i] = make([]int32, len(my)+1)
		}
		for i := len(mx) - 1; i >= 0; i-- {
			for j := len(my) - 1; j >= 0; j-- {
				if mx[i] == my[j] {
					dp[i][j] = dp[i+1][j+1] + 1
				} else {
					dp[i][j] = max(dp[i+1][j], dp[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(mx) || j < len(my) {
			switch {
			case i < len(mx) && j < len(my) && mx[i] == my[j]:
				ls = append(ls, line{' ', mx[i], pre + i + 1})
				i++
				j++
			case i < len(mx) && (j == len(my) || dp[i+1][j] >= dp[i][j+1]): // 同样长时先删后加
				ls = append(ls, line{'-', mx[i], pre + i + 1})
				i++
			default:
				ls = append(ls, line{'+', my[j], pre + i + 1})
				j++
			}
		}
	}
	for i := len(x) - suf; i < len(x); i++ {
		ls = append(ls, line{' ', x[i], i + 1})
	}
	// 只留改动和前后 ctx 行上下文
	keep := make([]bool, len(ls))
	for i, l := range ls {
		if l.op != ' ' {
			for k := max(0, i-ctx); k <= min(len(ls)-1, i+ctx); k++ {
				keep[k] = true
			}
		}
	}
	var out []string
	omitted := 0
	gap := true
	for i, l := range ls {
		if !keep[i] {
			gap = true
			continue
		}
		if gap {
			out = append(out, fmt.Sprintf("@@ 第 %d 行 @@", l.no))
			gap = false
		}
		out = append(out, string(l.op)+" "+l.text)
	}
	if maxLines > 0 && len(out) > maxLines {
		omitted = len(out) - maxLines
		out = append(out[:maxLines], fmt.Sprintf("……（还有 %d 行没有显示）", omitted))
	}
	return strings.Join(out, "\n")
}

// splitLines 按行拆开（\r\n 和 \n 都认；末尾的换行不算多出一行）。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
