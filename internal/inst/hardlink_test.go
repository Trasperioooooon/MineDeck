package inst

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"minedeck/internal/atomicfile"
	"minedeck/internal/diskguard"
	"minedeck/internal/tasks"
)

func TestInPlaceWriteRejectsHardlinkAndAtomicWriteBreaksIt(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside-secret.txt")
	const origOutside = `[{"name":"KeepOutside","uuid":"0000"}]`
	if err := os.WriteFile(outside, []byte(origOutside), 0o644); err != nil {
		t.Fatal(err)
	}

	srvDir := filepath.Join(base, "servers", "s1")
	if err := os.MkdirAll(srvDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. 原地写（writePlayerRaw / PlayerAction / writeFileNoHardlink）遇到硬链接时必须拒绝，且外面文件原封不动。
	whitelistPath := filepath.Join(srvDir, "whitelist.json")
	if err := os.Link(outside, whitelistPath); err != nil {
		t.Fatalf("建硬链接失败（请在允许建硬链接的环境运行）：%v", err)
	}
	m := newTestManagerWithInstance(t, "s1", srvDir)
	err := m.PlayerAction("s1", "whitelist-add", "Attacker")
	if err == nil || !errors.Is(err, ErrHardlinkEscape) {
		t.Fatalf("原地写硬链接文件应被 ErrHardlinkEscape 拒绝，实际 %v", err)
	}
	if b, rerr := os.ReadFile(outside); rerr != nil || string(b) != origOutside {
		t.Fatalf("外部文件被修改或丢失：%q %v", b, rerr)
	}

	// 2. 解压还原时如果目标文件已存在硬链接，也必须拒绝且不改穿外面。
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	fw, _ := zw.Create("world/level.dat")
	_, _ = fw.Write([]byte("new-world-data"))
	_ = zw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(zbuf.Bytes()), int64(zbuf.Len()))
	destDir := filepath.Join(base, "restore-target")
	if err := os.MkdirAll(filepath.Join(destDir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedDat := filepath.Join(destDir, "world", "level.dat")
	if err := os.Link(outside, linkedDat); err != nil {
		t.Fatal(err)
	}
	lease, err := diskguard.Acquire(context.Background(), destDir, diskguard.Options{AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := unzipReaderTo(zr, destDir, lease); err == nil || !errors.Is(err, ErrHardlinkEscape) {
		t.Fatalf("还原解压遇到已存在的硬链接应拒绝，实际 %v", err)
	}
	if b, rerr := os.ReadFile(outside); rerr != nil || string(b) != origOutside {
		t.Fatalf("还原解压拒写后外部文件不应变化：%q %v", b, rerr)
	}

	// 3. 经替换写（UpdateProps → Props.Save → atomicfile.Write）：放行、外面原样、里面独立成链接数 1 的新文件。
	propsPath := filepath.Join(srvDir, "server.properties")
	propsOut := filepath.Join(base, "outside-props.properties")
	if err := os.WriteFile(propsOut, []byte("server-port=25565\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(propsOut, propsPath); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProps(propsPath, func(p *Props) error {
		p.Set("server-port", "25599")
		return nil
	}); err != nil {
		t.Fatalf("原子替换硬链接上的 server.properties 失败：%v", err)
	}
	if b, rerr := os.ReadFile(propsOut); rerr != nil || string(b) != "server-port=25565\n" {
		t.Fatalf("外部属性文件不应被改动：%q %v", b, rerr)
	}
	f, err := os.Open(propsPath)
	if err != nil {
		t.Fatal(err)
	}
	n, herr := atomicfile.HardlinkCount(f)
	_ = f.Close()
	if herr != nil || n != 1 {
		t.Fatalf("替换后服务器里的文件应变成独立文件（链接数=1），got n=%d err=%v", n, herr)
	}
}

func newTestManagerWithInstance(t *testing.T, name, dir string) *Manager {
	t.Helper()
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}
	inst := &Instance{
		Settings: Settings{Name: name, Core: "vanilla", MC: "1.20.4", XmxMB: 1024},
		Dir:      dir,
		Console:  newConsole(),
	}
	m.insts[name] = inst
	return m
}
