package modpack

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDisplayNameFromUpload(t *testing.T) {
	long85 := strings.Repeat("甲", 85)
	want80 := strings.Repeat("甲", 80)

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "普通中文压缩包", input: "我的服务器.zip", want: "我的服务器"},
		{name: "反斜杠路径与大写扩展名", input: `a\b\pack.ZIP`, want: "pack"},
		{name: "相对路径前缀", input: "../x/server.zip", want: "server"},
		{name: "去掉扩展名只剩空格", input: "  .zip", want: ""},
		{name: "去掉控制字符", input: "a\u0007b.zip", want: "ab"},
		{name: "超长按rune截断到80", input: long85 + ".zip", want: want80},
		{name: "没有扩展名", input: "abc", want: "abc"},
		{name: "多个点只去最后扩展名", input: "a.b.zip", want: "a.b"},
		{name: "去掉首尾空格和点", input: " . 我的服 . .zip", want: "我的服"},
		{name: "空输入", input: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DisplayNameFromUpload(tc.input)
			if got != tc.want {
				t.Fatalf("DisplayNameFromUpload(%q) = %q，期望 %q", tc.input, got, tc.want)
			}
			if utf8.RuneCountInString(got) > 80 {
				t.Fatalf("长度超过 80 个字符：%d", utf8.RuneCountInString(got))
			}
		})
	}
}
