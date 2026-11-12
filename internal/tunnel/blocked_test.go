package tunnel

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 被杀毒软件拦下（225、226）和 frpc 不见了，各自认得出来，说明里带着 frpc 的文件夹；别的错误不认。
func TestFrpcBlockedHint(t *testing.T) {
	dir := `C:\MineDeck\runtimes\frpc\openfrp`
	exe := filepath.Join(dir, "frpc_windows_amd64.exe")
	cases := []struct {
		name string
		err  error
		head string // 空串：不该认出来
	}{
		{"启动时被拦下", fmt.Errorf("frpc 启动失败: %w", &os.PathError{Op: "fork/exec", Path: exe, Err: errVirusInfected}), "frpc 被 Windows 安全中心拦下了"},
		{"拦下并删除", &os.PathError{Op: "open", Path: exe, Err: errVirusDeleted}, "frpc 被 Windows 安全中心拦下了"},
		{"下载时压缩包被拦下", fmt.Errorf("下载 OpenFrp frpc 失败: %w", fmt.Errorf("多次尝试后仍下载失败: %w",
			&os.LinkError{Op: "rename", Old: "openfrp-frpc.zip.part", New: "openfrp-frpc.zip", Err: errVirusInfected})), "frpc 被 Windows 安全中心拦下了"},
		{"解压后不见了", fmt.Errorf("解压出来的 frpc.exe：%w", errFrpcGone), "frpc 不见了，多半是被 Windows 安全中心或别的杀毒软件删掉了"},
		{"单纯的文件不存在不算", &os.PathError{Op: "open", Path: exe, Err: fs.ErrNotExist}, ""},
		{"别的错误不算", errors.New("dial tcp: i/o timeout"), ""},
	}
	for _, c := range cases {
		hint, ok := frpcBlockedHint(c.err, dir)
		if c.head == "" {
			if ok {
				t.Fatalf("%s：不该认出来，得到 %q", c.name, hint)
			}
			continue
		}
		if !ok || !strings.HasPrefix(hint, c.head) || !strings.Contains(hint, "「保护历史记录」") || !strings.HasSuffix(hint, dir) {
			t.Fatalf("%s：%q", c.name, hint)
		}
	}
}

// 启动隧道时 frpc 被拦下或转眼不见了：返回的错误和卡片上都是中文说明，系统给的原文记在日志里；别的启动错误照旧。
func TestStartShowsAntivirusHint(t *testing.T) {
	tempBase(t)
	old := startFrpc
	t.Cleanup(func() { startFrpc = old })
	dir := frpcDir("natfrp")
	place := func() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "frpc.exe"), []byte("不会真的运行"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager()
	tt, err := m.Add(Tunnel{Provider: "natfrp", Name: "游戏", Credential: "k", ProxyID: "10"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		start func(cmd *exec.Cmd) error
		want  string // 卡片上失败原因的开头
		raw   bool   // 日志里有没有「系统报错原文」
	}{
		{"被拦下", func(cmd *exec.Cmd) error {
			return &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: errVirusInfected}
		}, "启动失败：frpc 被 Windows 安全中心拦下了", true},
		// 真删掉再启动，拿系统真实的「找不到文件」：EnsureFrpc 刚找到它，算被删了
		{"转眼被删", func(cmd *exec.Cmd) error { _ = os.Remove(cmd.Path); return cmd.Start() }, "启动失败：frpc 不见了，多半是", true},
		{"别的错误照旧", func(*exec.Cmd) error { return errors.New("boom") }, "启动失败：frpc 启动失败: boom", false},
	}
	for _, c := range cases {
		place()
		startFrpc = c.start
		before := len(m.Console(tt.ID).Lines())
		err := m.Start(tt.ID)
		le := m.List()[0].LastError
		if err == nil || !strings.HasPrefix(le, c.want) || le != "启动失败："+err.Error() {
			t.Fatalf("%s：返回 %v，卡片上 %q", c.name, err, le)
		}
		if c.raw && !strings.HasSuffix(le, dir) {
			t.Fatalf("%s：说明里没有 frpc 的文件夹：%q", c.name, le)
		}
		lines := strings.Join(m.Console(tt.ID).Lines()[before:], "\n")
		if got := strings.Contains(lines, "[MineDeck] 系统报错原文：frpc 启动失败: "); got != c.raw {
			t.Fatalf("%s：日志里的原文（应有：%v）：\n%s", c.name, c.raw, lines)
		}
		t.Logf("%s：%s", c.name, lines)
	}
}

// 解压完找 frpc：包里有 frpc 却找不到，算不见了；包里本来就没有，照旧报没找到；找到了直接用。
func TestFrpcAfterExtract(t *testing.T) {
	dir := t.TempDir()
	notFound := errors.New("解压后未找到可执行文件")
	if _, err := frpcAfterExtract(dir, []string{"LICENSE", "frpc_windows_amd64.exe"}, notFound); !errors.Is(err, errFrpcGone) {
		t.Fatalf("包里有 frpc 却找不到：%v", err)
	}
	if _, err := frpcAfterExtract(dir, []string{"LICENSE", "frps.exe"}, notFound); err != notFound {
		t.Fatalf("包里本来就没有：%v", err)
	}
	exe := filepath.Join(dir, "frpc.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := frpcAfterExtract(dir, []string{"frpc.exe"}, notFound); err != nil || got != exe {
		t.Fatalf("找到了：%q %v", got, err)
	}
}

// 解压抹平目录层级，返回解压出来的文件名（目录项不算）。
func TestExtractZipFlatNames(t *testing.T) {
	zp := filepath.Join(t.TempDir(), "frp.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, n := range []string{"frp_0.61.1_windows_amd64/", "frp_0.61.1_windows_amd64/frpc.exe", "frp_0.61.1_windows_amd64/LICENSE"} {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(n, "/") {
			_, _ = w.Write([]byte("x"))
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	dest := t.TempDir()
	names, err := extractZipFlat(zp, dest)
	if err != nil || strings.Join(names, ",") != "frpc.exe,LICENSE" {
		t.Fatalf("解压出来的：%q %v", names, err)
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dest, n)); err != nil {
			t.Fatal(err)
		}
	}
}

// frpc 意外退出时 exe 也没了：说明是「不见了」和它的文件夹；exe 还在就不多说。
func TestFrpcGoneHint(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "frpc.exe")
	if h := frpcGoneHint(exe); !strings.HasPrefix(h, "frpc 不见了") || !strings.HasSuffix(h, dir) {
		t.Fatalf("exe 没了：%q", h)
	}
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if h := frpcGoneHint(exe); h != "" {
		t.Fatalf("exe 还在：%q", h)
	}
}
