package modpack

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/dl"
)

// mkZip 在临时目录里造一个整合包 zip，files 是 文件名 → 内容。
func mkZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.zip")
	w, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	for name, body := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodSHA1 = "da39a3ee5e6b4b0d3255bfef95601890afd80709" // 空串的 SHA-1

func mrpackIndex(files string) string {
	return `{"formatVersion":1,"name":"测试包","dependencies":{"minecraft":"1.20.1","fabric-loader":"0.16.9"},"files":[` + files + `]}`
}

func TestParseMrpackPerFilePolicy(t *testing.T) {
	idx := mrpackIndex(strings.Join([]string{
		// 正常文件：白名单主机 + SHA-1
		`{"path":"mods/ok.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar","https://cdn.modrinth.com/b.jar"],"fileSize":10}`,
		// 魔改模组常见写法：作者自己的 https 直链，主机不在白名单
		`{"path":"mods/custom-host.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://files.example.net/mods/custom-host.jar"],"fileSize":20}`,
		// 缺 SHA-1：有可下载地址也不能核对
		`{"path":"mods/no-hash.jar","hashes":{},"downloads":["https://cdn.modrinth.com/c.jar"],"fileSize":30}`,
		// 非 https：任何处置都不允许下载
		`{"path":"mods/plain-http.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["http://cdn.modrinth.com/d.jar"],"fileSize":40}`,
		// 混合地址：有一条白名单内的就用它（只保留允许的地址）
		`{"path":"mods/mixed.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/e.jar","https://evil.example.org/e.jar"],"fileSize":50}`,
		// 带端口的自定义主机
		`{"path":"mods/ported.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://files.example.net:8443/f.jar"],"fileSize":60}`,
	}, ","))
	p, err := Parse(mkZip(t, map[string]string{"modrinth.index.json": idx}))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if p.MC != "1.20.1" || p.Loader != "0.16.9" {
		t.Fatalf("识别错误: %+v", p)
	}
	if len(p.Files) != 2 {
		t.Fatalf("可自动下载的文件应为 2 个（ok、mixed），实际 %d: %+v", len(p.Files), p.Files)
	}
	for _, f := range p.Files {
		for _, u := range f.URLs {
			if !strings.HasPrefix(u, "https://cdn.modrinth.com/") {
				t.Errorf("%s 的下载地址应该只保留白名单内的: %s", f.Path, u)
			}
		}
	}
	if len(p.Pending) != 4 {
		t.Fatalf("待决文件应为 4 个，实际 %d: %+v", len(p.Pending), p.Pending)
	}
	byPath := map[string]PendingFile{}
	for _, pf := range p.Pending {
		byPath[pf.Path] = pf
	}
	if pf := byPath["mods/custom-host.jar"]; pf.Reason != ReasonHost || len(pf.URLs) != 1 {
		t.Errorf("自定义主机文件应为 host 待决: %+v", pf)
	}
	if pf := byPath["mods/ported.jar"]; pf.Reason != ReasonHost || len(pf.URLs) != 1 {
		t.Errorf("带端口的自定义主机文件应为 host 待决: %+v", pf)
	}
	if pf := byPath["mods/no-hash.jar"]; pf.Reason != ReasonNoHash {
		t.Errorf("缺哈希应为 nohash 待决: %+v", pf)
	}
	if pf := byPath["mods/plain-http.jar"]; pf.Reason != ReasonBadURL || len(pf.URLs) != 0 {
		t.Errorf("非 https 应为 badurl 待决且不保留地址: %+v", pf)
	}
}

func TestParseMrpackStillRefusesPackLevelProblems(t *testing.T) {
	// 没声明 Minecraft 版本
	_, err := Parse(mkZip(t, map[string]string{"modrinth.index.json": `{"formatVersion":1,"name":"x","dependencies":{},"files":[]}`}))
	if err == nil || !strings.Contains(err.Error(), "未声明 Minecraft 版本") {
		t.Fatalf("缺 Minecraft 版本应拒绝: %v", err)
	}
	// 目录穿越
	idx := mrpackIndex(`{"path":"../../../evil.jar","hashes":{"sha1":"` + goodSHA1 + `"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":1}`)
	if _, err := Parse(mkZip(t, map[string]string{"modrinth.index.json": idx})); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("目录穿越应拒绝: %v", err)
	}
	// Quilt
	_, err = Parse(mkZip(t, map[string]string{"modrinth.index.json": `{"formatVersion":1,"dependencies":{"minecraft":"1.20.1","quilt-loader":"0.1"},"files":[]}`}))
	if err == nil || !strings.Contains(err.Error(), "Quilt") {
		t.Fatalf("Quilt 应拒绝: %v", err)
	}
	// 认不出的 zip
	if _, err := Parse(mkZip(t, map[string]string{"dummy.txt": "x"})); err == nil {
		t.Fatal("无索引的 zip 应拒绝")
	}
}

func TestParseCurseForgeUnchanged(t *testing.T) {
	man := `{"name":"CF包","overrides":"overrides","minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.3.0","primary":true}]},"files":[{"projectID":1,"fileID":11,"required":true}]}`
	p, err := Parse(mkZip(t, map[string]string{"manifest.json": man, "overrides/config/a.txt": "hi"}))
	if err != nil {
		t.Fatalf("CurseForge 解析失败: %v", err)
	}
	if p.Format != "curseforge" || p.Core != "forge" || p.Loader != "47.3.0" || len(p.CFRefs) != 1 {
		t.Fatalf("CurseForge 解析结果不对: %+v", p)
	}
}

func TestHostIsPublic(t *testing.T) {
	pub := []string{"1.1.1.1", "8.8.8.8", "2001:4860:4860::8888", "example.com."}
	for _, h := range pub {
		if !HostIsPublic(h) {
			t.Errorf("%s 应判为公网", h)
		}
	}
	priv := []string{
		"", "127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1", "172.31.255.255",
		"169.254.169.254", "100.64.1.1", "100.127.255.255", "0.0.0.0", "255.255.255.255",
		"fe80::1", "fd00::1", "fc00::1", "224.0.0.1", "ff02::1", "localhost",
	}
	for _, h := range priv {
		if HostIsPublic(h) {
			t.Errorf("%s 应判为内网/不可用", h)
		}
	}
}

func TestAllowedURLAndHostOf(t *testing.T) {
	if _, ok := AllowedURL("https://cdn.modrinth.com/a.jar", nil); !ok {
		t.Error("白名单主机应允许")
	}
	if _, ok := AllowedURL("http://cdn.modrinth.com/a.jar", nil); ok {
		t.Error("http 一律不允许")
	}
	if _, ok := AllowedURL("https://files.example.net/a.jar", nil); ok {
		t.Error("未信任的主机不允许")
	}
	trusted := func(h string) bool { return h == "files.example.net" }
	if _, ok := AllowedURL("https://files.example.net:8443/a.jar", trusted); !ok {
		t.Error("已信任的主机（带端口）应允许")
	}
	if _, ok := AllowedURL("https://CDN.Modrinth.com./a.jar", nil); !ok {
		t.Error("大小写和尾部点应规范化后放行")
	}
	hosts := HostOf([]string{"http://x.com/a", "https://y.net/b", "https://Y.NET/c", "https://z.io/d"})
	if len(hosts) != 2 || hosts[0] != "y.net" || hosts[1] != "z.io" {
		t.Errorf("HostOf 应去重、保序、只留 https: %v", hosts)
	}
}

func TestClassifyURLs(t *testing.T) {
	allowed, pending := classifyURLs([]string{
		"https://cdn.modrinth.com/a", "https://evil.io/b", "http://evil.io/c", "https://github.com/d",
	}, nil)
	if len(allowed) != 2 || allowed[0] != "https://cdn.modrinth.com/a" || allowed[1] != "https://github.com/d" {
		t.Errorf("允许地址过滤不对: %v", allowed)
	}
	if len(pending) != 1 || pending[0] != "https://evil.io/b" {
		t.Errorf("待决候选不对: %v", pending)
	}
}

func TestErrVerifySentinel(t *testing.T) {
	// dl 的校验失败错误要能用 errors.Is 认出来（导入据此把单个文件降级为待决，而不是整包失败）
	if !errors.Is(dl.ErrVerify, dl.ErrVerify) {
		t.Fatal("哨兵自检失败")
	}
	if dl.ErrVerify.Error() != "哈希校验失败" {
		t.Fatalf("哨兵文案不对: %s", dl.ErrVerify.Error())
	}
}
