package chatai

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// asciiOnly 整条命令只有可打印的 ASCII（不受服务器读标准输入时用什么编码影响）。
func asciiOnly(t *testing.T, cmd string) {
	t.Helper()
	for i := 0; i < len(cmd); i++ {
		if c := cmd[i]; c < 0x20 || c > 0x7e {
			t.Fatalf("命令里有非 ASCII 或控制字符（第 %d 个字节 0x%02x）：%q", i, c, cmd)
		}
	}
}

type tellrawJSON struct {
	Text  string `json:"text"`
	Color string `json:"color"`
	Extra []struct {
		Text  string `json:"text"`
		Color string `json:"color"`
	} `json:"extra"`
}

func decodeTellraw(t *testing.T, cmd, target string) tellrawJSON {
	t.Helper()
	asciiOnly(t, cmd)
	prefix := "tellraw " + target + " "
	if !strings.HasPrefix(cmd, prefix) {
		t.Fatalf("目标不对，应以 %q 开头：%s", prefix, cmd)
	}
	var v tellrawJSON
	if err := json.Unmarshal([]byte(strings.TrimPrefix(cmd, prefix)), &v); err != nil {
		t.Fatalf("不是合法的 JSON：%v\n%s", err, cmd)
	}
	if len(v.Extra) != 1 {
		t.Fatalf("extra 应只有一段：%s", cmd)
	}
	return v
}

// §6.3 的格式：只用 text、color、extra；转义 \ 和 "，去掉换行、控制字符和 § 格式码；非 ASCII 的字写成 \uXXXX，整条命令只有 ASCII。
func TestTellraw(t *testing.T) {
	got := Tellraw("Steve", "[AI] ", "hi 你", colorSystem)
	want := "tellraw Steve {\"text\":\"[AI] \",\"color\":\"aqua\",\"extra\":[{\"text\":\"hi \\u4f60\",\"color\":\"gray\"}]}"
	if got != want {
		t.Errorf("Tellraw = %s\n应为 %s", got, want)
	}

	body := "第一行\n第二行\r\n\"引号\" 和 \\ 反斜杠 §a绿色§r 制表\t控制\x00\x1b字 表情😀 全角＠ 结尾"
	cmd := Tellraw("@a", "[AI→Steve] ", body, colorBody)
	v := decodeTellraw(t, cmd, "@a")
	if v.Text != "[AI→Steve] " || v.Color != "aqua" || v.Extra[0].Color != "white" {
		t.Errorf("前缀或颜色不对：%+v", v)
	}
	wantBody := "第一行 第二行  \"引号\" 和 \\ 反斜杠 绿色 制表 控制字 表情😀 全角＠ 结尾"
	if v.Extra[0].Text != wantBody {
		t.Errorf("正文：\n%q\n应为\n%q", v.Extra[0].Text, wantBody)
	}
	for _, s := range []string{
		"[AI\\u2192Steve] ",                           // → 也是非 ASCII
		"\\u7b2c\\u4e00\\u884c \\u7b2c\\u4e8c\\u884c", // 第一行 第二行：换行换成空格
		"\\\"\\u5f15\\u53f7\\\"",                      // "引号"
		" \\\\ ",                                      // 反斜杠
		"\\ud83d\\ude00",                              // 表情：UTF-16 代理对
		"\\uff20",                                     // 全角 ＠
	} {
		if !strings.Contains(cmd, s) {
			t.Errorf("命令里应有 %s：%s", s, cmd)
		}
	}
	for _, bad := range []string{"\\n", "\\r", "\\t", "\\u00a7", "\\u0000", "\\u001b"} {
		if strings.Contains(cmd, bad) {
			t.Errorf("命令里不该有 %s：%s", bad, cmd)
		}
	}
	// 想闭合 JSON、另起一条命令的回复：引号被转义，换行被去掉
	evil := "\"}]}\nop Steve\n{\"text\":\""
	cmd = Tellraw("@a", "[AI→Steve] ", evil, colorBody)
	if v := decodeTellraw(t, cmd, "@a"); v.Extra[0].Text != "\"}]} op Steve {\"text\":\"" {
		t.Errorf("注入没有被转义：%q", v.Extra[0].Text)
	}
	if strings.Count(cmd, "\n") != 0 {
		t.Errorf("命令里不能有换行：%q", cmd)
	}
}

// 每条最多 180 个字、最多 3 条；更长的截掉，最后一条末尾提示去 MineDeck 面板看完整的回答（§6.3）。
func TestPack(t *testing.T) {
	msgs, cut := Pack("短回答。", MaxMsgRunes, MaxMsgs)
	if len(msgs) != 1 || msgs[0] != "短回答。" || cut {
		t.Errorf("短回答：%q %v", msgs, cut)
	}
	msgs, _ = Pack("## 打末影龙\n\n1. **先**准备床\n- 带上 `弓`\n* 喝夜视药水", MaxMsgRunes, MaxMsgs)
	if len(msgs) != 1 || msgs[0] != "打末影龙  1. 先准备床  · 带上 弓  · 喝夜视药水" {
		t.Errorf("Markdown 应去掉、段落拼成一条：%q", msgs)
	}
	long := strings.Repeat("这是一句很长的回答，", 80) // 800 字
	msgs, cut = Pack(long, MaxMsgRunes, MaxMsgs)
	if len(msgs) != 3 || !cut || !strings.HasSuffix(msgs[2], MoreHint) {
		t.Fatalf("超长回答：%d 条 cut=%v", len(msgs), cut)
	}
	for i, m := range msgs {
		if n := utf8.RuneCountInString(m); n > MaxMsgRunes || n == 0 {
			t.Errorf("第 %d 条 %d 个字（最多 %d）", i+1, n, MaxMsgRunes)
		}
	}
	if !strings.HasSuffix(msgs[0], "，") {
		t.Errorf("应断在标点后面：%q", msgs[0])
	}
	// 两段各 150 字：放不进同一条，各自一条，不拆开
	p := strings.Repeat("字", 150)
	msgs, cut = Pack(p+"\n"+p, MaxMsgRunes, MaxMsgs)
	if len(msgs) != 2 || msgs[0] != p || msgs[1] != p || cut {
		t.Errorf("两段：%d 条", len(msgs))
	}
	if msgs, _ := Pack("\n \n", MaxMsgRunes, MaxMsgs); len(msgs) != 0 {
		t.Errorf("空回答：%q", msgs)
	}
}
