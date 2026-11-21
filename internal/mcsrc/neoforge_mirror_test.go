package mcsrc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minedeck/internal/app"
)

func TestParseSHA1Sidecar(t *testing.T) {
	if got, ok := parseSHA1Sidecar([]byte("0a096663e7e11717b695f8fdab463f37327bfa72\n")); !ok || got != "0a096663e7e11717b695f8fdab463f37327bfa72" {
		t.Fatalf("官方 .sha1：%q %v", got, ok)
	}
	if got, ok := parseSHA1Sidecar([]byte("0A096663E7E11717B695F8FDAB463F37327BFA72  neoforge.jar\n")); !ok || got != "0a096663e7e11717b695f8fdab463f37327bfa72" {
		t.Fatalf("带文件名：%q %v", got, ok)
	}
	for _, bad := range []string{
		"",
		"<html>0a096663e7e11717b695f8fdab463f37327bfa72</html>",
		"0a096663e7e11717b695f8fdab463f37327bfa72\nsecond-line",
		"0a096663e7e11717b695f8fdab463f37327bfa72 extra third",
		"abc",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	} {
		if _, ok := parseSHA1Sidecar([]byte(bad)); ok {
			t.Fatalf("不该认：%q", bad)
		}
	}
}

func TestNeoInstallerArtifactOnlyUsesForgeCDNWithOfficialSHA1(t *testing.T) {
	const wantSHA = "0a096663e7e11717b695f8fdab463f37327bfa72"
	sidecarBody := wantSHA + "\n"
	sidecarStatus := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/21.1.265/neoforge-21.1.265-installer.jar.sha1" && sidecarStatus == http.StatusOK {
			_, _ = w.Write([]byte(sidecarBody))
			return
		}
		http.Error(w, "missing", sidecarStatus)
	}))
	defer srv.Close()

	oldMaven, oldCDN := neoMavenBase, neoForgeCDNBase
	neoMavenBase = srv.URL
	neoForgeCDNBase = "https://neoforged.forgecdn.net/releases/net/neoforged/neoforge"
	t.Cleanup(func() {
		neoMavenBase, neoForgeCDNBase = oldMaven, oldCDN
	})

	oldBase := app.Base
	app.Base = t.TempDir()
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })
		app.Base = oldBase
	})

	// 1. 拿到合法官方 SHA-1 且非 OfficialOnly：forgecdn 排第二（国内没实测过，不抢原来的首选），并带官方 SHA-1
	art := NeoInstallerArtifact(context.Background(), "21.1.265", "")
	if art.SHA1 != wantSHA {
		t.Fatalf("SHA1 = %q，应为 %q", art.SHA1, wantSHA)
	}
	if len(art.URLs) != 3 || !strings.HasPrefix(art.URLs[1], "https://neoforged.forgecdn.net/") ||
		strings.Contains(art.URLs[0], "forgecdn.net") {
		t.Fatalf("有官方校验值时 forgecdn 应排在原来的首选后面：%v", art.URLs)
	}
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "mirror"; return nil })
	artMirror := NeoInstallerArtifact(context.Background(), "21.1.265", "")
	if len(artMirror.URLs) < 2 || !strings.HasPrefix(artMirror.URLs[0], "https://bmclapi2.bangbang93.com/") ||
		!strings.HasPrefix(artMirror.URLs[1], "https://neoforged.forgecdn.net/") {
		t.Fatalf("镜像优先时应先 BMCLAPI、再 forgecdn：%v", artMirror.URLs)
	}
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })

	// 2. OfficialOnly 模式：即使拿到官方 SHA-1 也不加 forgecdn
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "official"; return nil })
	artOff := NeoInstallerArtifact(context.Background(), "21.1.265", "")
	if artOff.SHA1 != wantSHA {
		t.Fatalf("OfficialOnly SHA1 = %q", artOff.SHA1)
	}
	for _, u := range artOff.URLs {
		if strings.Contains(u, "forgecdn.net") {
			t.Fatalf("OfficialOnly 不应含 forgecdn：%v", artOff.URLs)
		}
	}
	_, _ = app.UpdateConfig(func(c *app.Config) error { c.Source = "auto"; return nil })

	// 3. 官方 .sha1 返回 HTML 或 404：坚决不加 forgecdn
	for _, body := range []string{"<html>cookie check</html>", "not-a-sha1"} {
		sidecarBody = body
		a := NeoInstallerArtifact(context.Background(), "21.1.265", "")
		if a.SHA1 != "" {
			t.Fatalf("坏边车不应产出 SHA1：%q", a.SHA1)
		}
		for _, u := range a.URLs {
			if strings.Contains(u, "forgecdn.net") {
				t.Fatalf("没有合格官方校验值时不应带 forgecdn：%v", a.URLs)
			}
		}
	}
	sidecarBody = wantSHA
	sidecarStatus = http.StatusNotFound
	a404 := NeoInstallerArtifact(context.Background(), "21.1.265", "")
	for _, u := range a404.URLs {
		if strings.Contains(u, "forgecdn.net") {
			t.Fatalf("官方 .sha1 404 时不应带 forgecdn：%v", a404.URLs)
		}
	}

	// 4. 非法版本号（.. 或穿越字符）不拼危险文件名，也不拉边车
	sidecarStatus = http.StatusOK
	for _, badVer := range []string{"..", "../evil", "21.1/../../x"} {
		ab := NeoInstallerArtifact(context.Background(), badVer, "")
		if ab.SHA1 != "" || strings.Contains(ab.FileName, "..") || strings.ContainsAny(ab.FileName, `/\`) {
			t.Fatalf("非法版本号 %q 未拦住：%+v", badVer, ab)
		}
	}
}
