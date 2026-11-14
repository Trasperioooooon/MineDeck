package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 键值对和字段名两条规则各管各的：{"key","value"} 的 value 按 key 判断，同一层别的字段照常按自己的名字判断。
// 以前一认出是键值对就不再看同层的字段名，{"key":"motd","value":"x","token":"…"} 里的 token 原样进了操作记录。
func TestRedactPairAndSiblingFields(t *testing.T) {
	in := `{
		"a":{"key":"motd","value":"hello","token":"tok-1"},
		"b":{"key":"rcon.password","value":"pw-2","credential":"cred-3","note":"留着"},
		"c":{"key":"server-name","value":{"apiKey":"k-4","port":25565}},
		"d":{"authorization":"Bearer auth-5","Authorization":"Bearer auth-6","headers":{"X-Authorization-Token":"auth-7"}},
		"e":{"key":"solo-key-8"},
		"f":[{"key":"motd","value":"x","password":"pw-9"}],
		"g":{"key":"motd","value":{"key":"rcon.password","value":"pw-10","token":"tok-11"}}
	}`
	var v any
	dec := json.NewDecoder(strings.NewReader(in))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	out := encode(Redact(v))
	for i, s := range []string{"tok-1", "pw-2", "cred-3", "k-4", "auth-5", "auth-6", "auth-7", "solo-key-8", "pw-9", "pw-10", "tok-11"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("第 %d 个密钥 %s 没遮住：%s", i+1, s, out)
		}
	}
	want := `{"a":{"key":"motd","token":"***","value":"hello"},` +
		`"b":{"credential":"***","key":"rcon.password","note":"留着","value":"***"},` +
		`"c":{"key":"server-name","value":{"apiKey":"***","port":25565}},` +
		`"d":{"Authorization":"***","authorization":"***","headers":{"X-Authorization-Token":"***"}},` +
		`"e":{"key":"***"},` +
		`"f":[{"key":"motd","password":"***","value":"x"}],` +
		`"g":{"key":"motd","value":{"key":"rcon.password","token":"***","value":"***"}}}`
	if string(out) != want {
		t.Errorf("脱敏结果：\n%s\n应为：\n%s", out, want)
	}
	if !SecretName("Authorization") || !SecretName("proxyAuthorization") {
		t.Error("Authorization 请求头应算密钥")
	}
}

// 操作参数最外层的 key 是 AI 设置的 API Key 这类真密钥：多传一个 value，也不能被当成键值对的键名原样记下
// （Codex 复审 8de04b2 时指出，main 原有）。清除用的空串照旧留着；pairs 里的键名照旧留着。
func TestOpLogTopLevelKeyAlwaysMasked(t *testing.T) {
	f := newFixture(t)
	for _, args := range []string{
		`{"preset":"deepseek","key":"sk-real-12","value":null}`,
		`{"key":"sk-real-13","value":"x"}`,
		`{"key":"","value":"x","pairs":[{"key":"max-players","value":"10"},{"key":"rcon.password","value":"pw-14"}]}`,
	} {
		if _, err := f.reg.Do(context.Background(), ui, "t.cfg", Args(args)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, r := range f.records(t) {
		got = append(got, string(r.Args))
	}
	all := strings.Join(got, "\n")
	for _, s := range []string{"sk-real-12", "sk-real-13", "pw-14"} {
		if strings.Contains(all, s) {
			t.Errorf("操作记录里有 %s：\n%s", s, all)
		}
	}
	want := []string{ // 新的在前
		`{"key":"","pairs":[{"key":"max-players","value":"10"},{"key":"rcon.password","value":"***"}],"value":"x"}`,
		`{"key":"***","value":"x"}`,
		`{"key":"***","preset":"deepseek","value":null}`,
	}
	if strings.Join(want, "\n") != all {
		t.Errorf("脱敏结果：\n%s\n应为：\n%s", all, strings.Join(want, "\n"))
	}
}
