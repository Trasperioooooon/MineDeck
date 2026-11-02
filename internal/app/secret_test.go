package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, c map[string]any) {
	t.Helper()
	b, _ := json.Marshal(c)
	if err := os.WriteFile(configPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileHas(t *testing.T, name, s string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Base, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(b), s)
}

// 旧配置里的明文 cfApiKey：启动时加密挪进 secrets.json，config.json 里不再有它，读出来照旧可用。
func TestMigrateCFKey(t *testing.T) {
	tempBase(t)
	writeConfig(t, map[string]any{"source": "auto", "cfApiKey": "plain-cf-key-123", "webhookUrl": "https://example.com/hook"})
	restart()
	migrateCFKey()
	if fileHas(t, "config.json", "plain-cf-key-123") || fileHas(t, "config.json", "cfApiKey") {
		t.Fatal("迁移后 config.json 里还有明文 Key")
	}
	if fileHas(t, "secrets.json", "plain-cf-key-123") || !fileHas(t, "secrets.json", `"cf.key": "dpapi:`) {
		t.Fatal("secrets.json 里应是 DPAPI 密文")
	}
	if !fileHas(t, "config.json", "https://example.com/hook") {
		t.Fatal("迁移不应动其他配置")
	}
	restart()
	if k := CFKey(); k != "plain-cf-key-123" {
		t.Fatalf("迁移后读不出 Key：%q", k)
	}
	if set, bad := CFKeyState(); !set || bad {
		t.Fatalf("CFKeyState = %v, %v", set, bad)
	}
	migrateCFKey() // 再启动一次：没有明文了，什么都不做
	if k := CFKey(); k != "plain-cf-key-123" {
		t.Fatalf("重复迁移后 Key 变了：%q", k)
	}
}

// 加密失败（这里用「secrets.json 是个目录」模拟写不进去）就保持原样：config.json 留着明文，照旧能用。
func TestMigrateCFKeyFailureKeepsPlaintext(t *testing.T) {
	tempBase(t)
	writeConfig(t, map[string]any{"source": "auto", "cfApiKey": "plain-cf-key-456"})
	if err := os.Mkdir(filepath.Join(Base, "secrets.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	restart()
	migrateCFKey()
	if !fileHas(t, "config.json", "plain-cf-key-456") {
		t.Fatal("迁移失败时不应删掉 config.json 里的 Key")
	}
	if k := CFKey(); k != "plain-cf-key-456" {
		t.Fatalf("迁移失败时应照旧读明文：%q", k)
	}
	if set, bad := CFKeyState(); !set || bad {
		t.Fatalf("CFKeyState = %v, %v", set, bad)
	}
}

// 保存新 Key 加密存放，并顺手清掉残留的旧明文；空串清除；解不开时当作没设置。
func TestSetCFKey(t *testing.T) {
	tempBase(t)
	writeConfig(t, map[string]any{"source": "auto", "cfApiKey": "leftover-plain"})
	restart() // 不迁移，模拟上次迁移失败留下的明文
	if err := SetCFKey("  new-cf-key  "); err != nil {
		t.Fatal(err)
	}
	if fileHas(t, "config.json", "leftover-plain") || CFKey() != "new-cf-key" {
		t.Fatalf("旧明文没清掉，或新 Key 不对：%q", CFKey())
	}
	if err := SetCFKey(""); err != nil {
		t.Fatal(err)
	}
	if set, bad := CFKeyState(); set || bad || CFKey() != "" {
		t.Fatalf("清除后应为未设置：%v, %v, %q", set, bad, CFKey())
	}
	if err := os.WriteFile(filepath.Join(Base, "secrets.json"), []byte(`{"cf.key":"dpapi:AAAA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if set, bad := CFKeyState(); set || !bad || CFKey() != "" {
		t.Fatalf("解不开时应报 unreadable 并当作没设置：%v, %v, %q", set, bad, CFKey())
	}
}
