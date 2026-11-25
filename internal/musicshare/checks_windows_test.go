package musicshare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/bridge"
)

// voice_host 的备份 .bak 是只读的：Windows 上照旧报错、不覆盖（和以前的 os.WriteFile 一样），voice_host 也不写；
// 报错里带上是哪个文件，但不带本机的完整路径。
func TestVoiceHostReadOnlyBackupRefused(t *testing.T) {
	base := t.TempDir()
	cfg := filepath.Join(base, "server", "config", "voicechat", "voicechat-server.properties")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	const orig = "port=24454\nvoice_host=\n"
	if err := os.WriteFile(cfg, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	const oldBak = "old backup\n"
	bak := cfg + ".bak"
	if err := os.WriteFile(bak, []byte(oldBak), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bak, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bak, 0o644) })
	p, err := bridge.LoadProps(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = writeVoiceHost(cfg, p, "a.example.com")
	if err == nil || !strings.Contains(err.Error(), "voicechat-server.properties.bak") {
		t.Fatalf("只读的 .bak 应报错并说出是哪个文件：%v", err)
	}
	if strings.Contains(err.Error(), base) {
		t.Fatalf("报错里不该有本机的完整路径：%v", err)
	}
	if b, _ := os.ReadFile(bak); string(b) != oldBak {
		t.Fatalf("只读的 .bak 被换掉了：%q", b)
	}
	if st, err := os.Stat(bak); err != nil || st.Mode().Perm()&0o200 != 0 {
		t.Fatalf(".bak 应还是只读的：%v %v", st.Mode(), err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != orig {
		t.Fatalf("备份失败时不该写 voice_host：%q", b)
	}
}
