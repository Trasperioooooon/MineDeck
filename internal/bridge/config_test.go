package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .properties 的读法和 java.util.Properties.load 一样；改一个键时其余行（注释、空行、别的键）原样保留，
// 换行符和 BOM 照旧，重复的键只留一个。
func TestPropsReadWrite(t *testing.T) {
	src := "\uFEFF# 注释\r\n! 也是注释\r\n\r\nport = 24454\r\nvoice_host=old.example.com\\:24454\r\n" +
		"key\\ with\\ space:value\r\nmulti=a\\\r\n    b\r\nempty=\r\nuni=\\u4e2d\\u6587\r\nport=24455\r\n"
	p := ParseProps([]byte(src))
	for k, want := range map[string]string{
		"port": "24455", "voice_host": "old.example.com:24454", "key with space": "value", "multi": "ab", "empty": "", "uni": "中文",
	} {
		if got, ok := p.Get(k); !ok || got != want {
			t.Errorf("%s = %q（%v），应为 %q", k, got, ok, want)
		}
	}
	if _, ok := p.Get("nope"); ok {
		t.Error("不存在的键")
	}
	if p.Set("voice_host", "old.example.com:24454") {
		t.Error("值没变不该算改动")
	}
	if !p.Set("voice_host", "frp-abc.natfrp.cloud:12345") || !p.Set("port", "24456") || !p.Set("multi", "c") || !p.Set("added", " lead\\x") {
		t.Fatal("应算改动")
	}
	out := string(p.Bytes())
	want := "\uFEFF# 注释\r\n! 也是注释\r\n\r\nvoice_host=frp-abc.natfrp.cloud:12345\r\n" +
		"key\\ with\\ space:value\r\nmulti=c\r\nempty=\r\nuni=\\u4e2d\\u6587\r\nport=24456\r\nadded=\\ lead\\\\x\r\n"
	if out != want {
		t.Fatalf("写回：\n%q\n应为：\n%q", out, want)
	}
	again := ParseProps([]byte(out))
	if v, _ := again.Get("added"); v != " lead\\x" {
		t.Errorf("转义读回：%q", v)
	}
	// 列出键、删掉键（续行一起删，注释和别的行不动）
	if keys := strings.Join(again.Keys(), ","); keys != "voice_host,key with space,multi,empty,uni,port,added" {
		t.Errorf("Keys：%s", keys)
	}
	r := ParseProps([]byte("# c\nz=1\nlong=a\\\n  b\nz=2\nkeep=x\n"))
	if !r.Delete("z") || !r.Delete("long") || r.Delete("nope") {
		t.Error("Delete 的返回值不对")
	}
	if string(r.Bytes()) != "# c\nkeep=x\n" {
		t.Errorf("删掉以后：%q", r.Bytes())
	}
	// 空文件、只有 LF 的文件
	q := ParseProps(nil)
	q.Set("a", "1")
	if string(q.Bytes()) != "a=1\n" {
		t.Errorf("空文件：%q", q.Bytes())
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "x.properties")
	for _, body := range []string{"a=1\n", "a=2\n"} {
		if err := WriteFileAtomic(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Fatalf("内容 %q", b)
		}
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Fatalf("留下了临时文件：%v", ents)
	}
}

// voicebridge.properties：读法和模组的 BridgeConfig 一致；EnsureConfig 只补缺的（打开共享、端口、令牌），
// 别的行和用户加的内容原样保留；新建时带说明、端口避开别的实例。
func TestEnsureConfig(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadConfig(dir); !os.IsNotExist(err) {
		t.Fatalf("没有文件时应报不存在：%v", err)
	}
	if _, err := SetExclude(dir, "Steve"); !os.IsNotExist(err) {
		t.Fatalf("没有文件时 SetExclude 不新建：%v", err)
	}
	used := map[int]bool{24470: true, 24471: true}
	busyPort := 24472
	o := EnsureOptions{Avoid: func(p int) bool { return used[p] }, canBind: func(p int) bool { return p != busyPort }}
	c, changed, err := EnsureConfig(dir, o)
	if err != nil || !changed {
		t.Fatalf("新建：%v %v", changed, err)
	}
	if !c.Enabled || c.Port != 24473 || len(c.Token) != 32 || c.Problem() != "" || c.Exclude != "" || c.Selftest || !c.Bypass {
		t.Fatalf("新建的配置：%+v（%s）", c, c.Problem())
	}
	body, _ := os.ReadFile(ConfigPath(dir))
	if !strings.Contains(string(body), "\r\n# exclude：") && !strings.Contains(string(body), "# exclude：") {
		t.Errorf("新建的文件应带说明：%s", body)
	}
	// 再来一次：什么都不缺，不写文件
	before, _ := os.Stat(ConfigPath(dir))
	c2, changed, err := EnsureConfig(dir, o)
	if err != nil || changed || c2 != c {
		t.Fatalf("没缺东西时不该改：%v %v %+v", changed, err, c2)
	}
	after, _ := os.Stat(ConfigPath(dir))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("没改动却写了文件")
	}

	// 模组生成的模板（共享关闭、默认端口和别的实例撞了）+ 用户加的一行：打开共享、换端口，别的保留
	tpl := "# 模组的模板\nenabled=false\nport=24470\ntoken=0123456789abcdef0123\nexclude=房主\nselftest=true\nmy_note=keep\n"
	if err := os.WriteFile(ConfigPath(dir), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := "Alex,Steve"
	c3, changed, err := EnsureConfig(dir, EnsureOptions{Exclude: &ex, Avoid: o.Avoid, canBind: o.canBind})
	if err != nil || !changed || !c3.Enabled || c3.Port != 24473 || c3.Token != "0123456789abcdef0123" || c3.Exclude != "Alex,Steve" || !c3.Selftest {
		t.Fatalf("改模板：%v %v %+v", changed, err, c3)
	}
	got, _ := os.ReadFile(ConfigPath(dir))
	if string(got) != "# 模组的模板\nenabled=true\nport=24473\ntoken=0123456789abcdef0123\nexclude=Alex,Steve\nselftest=true\nmy_note=keep\n" {
		t.Fatalf("改模板后的文件：\n%s", got)
	}
	if ch, err := SetExclude(dir, "Alex,Steve"); err != nil || ch {
		t.Fatalf("排除名单没变：%v %v", ch, err)
	}
	if ch, err := SetExclude(dir, ""); err != nil || !ch {
		t.Fatalf("清空排除名单：%v %v", ch, err)
	}
	if c4, _ := ReadConfig(dir); c4.Exclude != "" {
		t.Fatalf("清空后：%+v", c4)
	}

	// 端口写错、令牌太短：补上
	if err := os.WriteFile(ConfigPath(dir), []byte("enabled=yes\nport=abc\ntoken=short\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c5, _ := ReadConfig(dir); c5.Port != -1 || !c5.Enabled || c5.Problem() == "" {
		t.Fatalf("写错的配置：%+v", c5)
	}
	c6, changed, err := EnsureConfig(dir, o)
	if err != nil || !changed || c6.Port != 24473 || len(c6.Token) != 32 || c6.Problem() != "" {
		t.Fatalf("修好：%+v %v", c6, err)
	}
}

func TestNormalizeExclude(t *testing.T) {
	for in, want := range map[string]string{
		"":                                 "",
		" Steve ":                          "Steve",
		"Steve，房主; alex  Steve":            "Steve,房主,alex",
		"069a79f444e94726a5befca90e38aaf5": "069a79f444e94726a5befca90e38aaf5",
	} {
		got, err := NormalizeExclude(in)
		if err != nil || got != want {
			t.Errorf("%q → %q（%v），应为 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`a\b`, "a=b", "#x", "名字超过三十六个字符的名字名字名字名字名字名字名字名字名字名字名字名字名字名字", "a:b"} {
		if _, err := NormalizeExclude(bad); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
}
