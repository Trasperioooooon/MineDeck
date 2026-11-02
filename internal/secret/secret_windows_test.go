package secret

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DPAPI 加密再解密要原样回来，密文里看不到明文。
func TestSealRoundTrip(t *testing.T) {
	for _, plain := range []string{"cf-key-123", `a+b&c"d'e<f>g\h 中文 😀`, strings.Repeat("x", 4096)} {
		s, err := Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		if !IsSealed(s) || strings.Contains(s, plain) {
			t.Fatalf("密文不对：%.60q", s)
		}
		got, err := Unseal(s)
		if err != nil || got != plain {
			t.Fatalf("Unseal = %.40q, %v；应为 %.40q", got, err, plain)
		}
	}
	if s, err := Seal(""); s != "" || err != nil {
		t.Errorf("空串应原样返回：%q, %v", s, err)
	}
	if got, err := Unseal("旧版明文"); got != "旧版明文" || err != nil {
		t.Errorf("没有 dpapi: 前缀的应按明文原样返回：%q, %v", got, err)
	}
}

// 密文被改坏、或者不带本程序的附加口令（相当于别的程序来解）都解不开，报 ErrUnreadable。
func TestUnsealFailures(t *testing.T) {
	s, err := Seal("secret-value")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	// 改最后一个字节（DPAPI 的签名）一定解不开。前面的字段不一定在签名里：真 Windows 上
	// 版本号和提供者 GUID 改了照样解得开，wine 的密文格式又不一样
	raw[len(raw)-1] ^= 0x55
	for name, bad := range map[string]string{
		"不是 base64": "dpapi:@@@",
		"截断":        s[:len(s)-12],
		"改了签名的一个字节": prefix + base64.StdEncoding.EncodeToString(raw),
	} {
		if _, err := Unseal(bad); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s：应返回 ErrUnreadable，实际 %v", name, err)
		}
	}
	old := entropy
	entropy = []byte("someone-else")
	_, err = Unseal(s)
	entropy = old
	if !errors.Is(err, ErrUnreadable) {
		t.Errorf("附加口令不对也解开了：%v", err)
	}
}

// Store：存进文件的是密文；空串删除；解不开的一项报「解不开」而不是当成有值。
func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	st := NewStore(path)
	if v, err := st.Get("cf.key"); v != "" || err != nil {
		t.Fatalf("文件不存在时应为空：%q, %v", v, err)
	}
	if set, bad := st.State("cf.key"); set || bad {
		t.Fatalf("State = %v, %v", set, bad)
	}
	if err := st.Set("cf.key", "my-curseforge-key"); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("ai.key", "my-ai-key"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "my-curseforge-key") || strings.Contains(string(b), "my-ai-key") || !strings.Contains(string(b), `"cf.key": "dpapi:`) {
		t.Fatalf("secrets.json 里有明文或格式不对：%s", b)
	}
	if v, err := NewStore(path).Get("cf.key"); v != "my-curseforge-key" || err != nil {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if set, bad := st.State("cf.key"); !set || bad {
		t.Fatalf("State = %v, %v", set, bad)
	}
	if err := st.Set("cf.key", ""); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.Get("cf.key"); v != "" {
		t.Fatalf("空串应删除这一项，仍为 %q", v)
	}
	if v, _ := st.Get("ai.key"); v != "my-ai-key" {
		t.Fatalf("删一项不应影响别的项：%q", v)
	}
	if err := os.WriteFile(path, []byte(`{"cf.key":"dpapi:AAAA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if set, bad := st.State("cf.key"); set || !bad {
		t.Errorf("解不开的一项应报 unreadable：%v, %v", set, bad)
	}
	if _, err := st.Get("cf.key"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("Get 应返回 ErrUnreadable：%v", err)
	}
	// 文件坏了：按「解不开」报；存新密钥时从空文件重新开始，不会一直存不进去
	if err := os.WriteFile(path, []byte(`{"cf.key":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if set, bad := st.State("cf.key"); set || !bad {
		t.Errorf("文件坏了应报 unreadable：%v, %v", set, bad)
	}
	if err := st.Set("cf.key", "fresh-key"); err != nil {
		t.Fatalf("文件坏了时应能重新存：%v", err)
	}
	if v, err := st.Get("cf.key"); v != "fresh-key" || err != nil {
		t.Errorf("重新存后 Get = %q, %v", v, err)
	}
}

// 查状态不解出明文字符串，结果按密文缓存；换一段密文会重新判断。
func TestReadableCache(t *testing.T) {
	good, err := Seal("k")
	if err != nil {
		t.Fatal(err)
	}
	if !Readable(good) || !Readable(good) || Readable("dpapi:AAAA") || Readable("dpapi:@@") || !Readable("旧版明文") {
		t.Error("Readable 判断不对")
	}
	readableMu.Lock()
	cached := readable[good]
	readableMu.Unlock()
	if !cached {
		t.Error("能解开的结果应缓存")
	}
}
