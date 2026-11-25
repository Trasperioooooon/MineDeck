package secret

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"minedeck/internal/errhint"
	"minedeck/internal/errhint/hintcheck"
)

func TestStoreCorruptJSONAndUnreadableHints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	// 1. 坏 JSON：读时报错带「密钥存储文件坏了」提示，但 Set 会自动重建并写回。
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewStore(path)
	_, err := st.Get("cf.key")
	if err == nil {
		t.Fatal("坏 JSON 应报错")
	}
	hint := errhint.FromError(err)
	if !strings.Contains(hint, "密钥存储文件坏了") {
		t.Fatalf("坏 JSON 提示不符：%q", hint)
	}
	if strings.Contains(hint, "换了电脑") {
		t.Fatalf("坏 JSON 不应误报换了电脑：%q", hint)
	}
	if herr := hintcheck.CheckUIHint(hint); herr != nil {
		t.Fatal(herr)
	}

	if runtime.GOOS == "windows" {
		if err := st.Set("cf.key", "recovered-secret"); err != nil {
			t.Fatalf("Set 遇到坏 JSON 应能自动重建：%v", err)
		}
		if got, err := st.Get("cf.key"); err != nil || got != "recovered-secret" {
			t.Fatalf("重建后读回不一致：got=%q err=%v", got, err)
		}
	}

	// 2. DPAPI 密文解不开：Get 返回 ErrUnreadable，附带换电脑或账户提示。
	if err := os.WriteFile(path, []byte(`{"cf.key":"dpapi:not-valid-base64!!!"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = st.Get("cf.key")
	if err == nil {
		t.Fatal("解不开的密文应报错")
	}
	hint = errhint.FromError(err)
	if !strings.Contains(hint, "可能换了电脑或系统账户") {
		t.Fatalf("解不开的提示不符：%q", hint)
	}
	if herr := hintcheck.CheckUIHint(hint); herr != nil {
		t.Fatal(herr)
	}
}
