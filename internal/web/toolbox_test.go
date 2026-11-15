package web

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/toolbox"
	"minedeck/internal/tunnel"
)

func TestToolboxWebSkinPackAndSecurity(t *testing.T) {
	e := newFullEnv(t)

	// 1. 预演槽位接口
	rec := e.call(t, http.MethodPost, "/api/toolbox/skin-pack/preview",
		`{"mc":"1.21.4","mode":"player","entries":[{"player":"Notch"}]}`, 200)
	if !strings.Contains(rec.Body.String(), "b50ad385-829d-3141-a216-7e7d7539ba7f") {
		t.Fatalf("preview 未返回离线 UUID: %s", rec.Body.String())
	}

	// 准备一张 64×64 测试 PNG
	var pngBuf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	img.SetNRGBA(0, 0, color.NRGBA{R: 120, G: 200, B: 80, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	buildMultipart := func(destDir string) (*bytes.Buffer, string) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		meta, _ := json.Marshal(map[string]any{
			"mc":       "1.21.4",
			"packName": "测试皮肤包",
			"mode":     "player",
			"destDir":  destDir,
			"entries":  []map[string]any{{"player": "Notch", "keep": true}},
		})
		_ = mw.WriteField("meta", string(meta))
		pw, err := mw.CreateFormFile("file_0", `..\..\..\..\evil.png`)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = pw.Write(pngBuf.Bytes())
		_ = mw.Close()
		return &body, mw.FormDataContentType()
	}

	// 2. 局域网会话不能指定主机保存目录（应返回 403）
	loginRec := e.call(t, http.MethodPost, "/api/login", `{"password":"`+lanPass+`"}`, 200, noToken)
	var cookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "amh_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("未拿到会话 Cookie")
	}

	hostDir := t.TempDir()
	lanBody, lanCT := buildMultipart(hostDir)
	req := httptest.NewRequest(http.MethodPost, "/api/toolbox/skin-pack/build", lanBody)
	req.RemoteAddr = "192.168.1.55:12345"
	req.Header.Set("Content-Type", lanCT)
	req.AddCookie(cookie)
	wrec := httptest.NewRecorder()
	e.h.ServeHTTP(wrec, req)
	if wrec.Code != 403 {
		t.Fatalf("局域网请求带 destDir 应 403，实际=%d body=%s", wrec.Code, wrec.Body.String())
	}

	// 3. 本机带 token 请求带上 destDir 正常生成并写盘，且不采用上传名，返回可用 downloadId
	localBody, localCT := buildMultipart(hostDir)
	req = httptest.NewRequest(http.MethodPost, "/api/toolbox/skin-pack/build", localBody)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", localCT)
	req.Header.Set("X-Token", Token)
	wrec = httptest.NewRecorder()
	e.h.ServeHTTP(wrec, req)
	if wrec.Code != 200 {
		t.Fatalf("本机 build 失败: code=%d body=%s", wrec.Code, wrec.Body.String())
	}
	var buildOut struct {
		OK   bool                  `json:"ok"`
		Data toolbox.SkinBuildResp `json:"data"`
	}
	if err := json.Unmarshal(wrec.Body.Bytes(), &buildOut); err != nil || buildOut.Data.DownloadID == "" || buildOut.Data.SavedPath == "" {
		t.Fatalf("build 响应不对: %s", wrec.Body.String())
	}
	if filepath.Dir(buildOut.Data.SavedPath) != hostDir {
		t.Fatalf("保存路径跑出了指定目录: %s", buildOut.Data.SavedPath)
	}

	// 4. 下载接口返回 zip 且头正确，路径穿越/非法编号拒绝，上传文件名不决定压缩包内路径
	dlRec := e.call(t, http.MethodGet, "/api/toolbox/skin-pack/download/"+buildOut.Data.DownloadID, "", 200)
	if !strings.Contains(dlRec.Header().Get("Content-Type"), "application/zip") {
		t.Fatalf("下载 Content-Type 不对: %s", dlRec.Header().Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(dlRec.Body.Bytes()), int64(dlRec.Body.Len()))
	if err != nil {
		t.Fatalf("下载的不是有效 zip: %v", err)
	}
	for _, f := range zr.File {
		if strings.Contains(f.Name, "evil") {
			t.Fatalf("上传文件名污染了 zip 内路径: %s", f.Name)
		}
	}
	e.call(t, http.MethodGet, "/api/toolbox/skin-pack/download/bad-id", "", 400)
}

func TestToolboxWebMrpackRoutesAndAIRestriction(t *testing.T) {
	oldDirs := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = oldDirs[0], oldDirs[1], oldDirs[2], oldDirs[3], oldDirs[4]
	})
	base := t.TempDir()
	app.Base = base
	app.ServersDir = filepath.Join(base, "servers")
	app.BackupsDir = filepath.Join(base, "backups")
	app.CacheDir = filepath.Join(base, "cache")
	app.RuntimesDir = filepath.Join(base, "runtimes")
	_, _ = app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil })

	jarBytes := []byte("PK\x03\x04test-jar-content")
	h1 := sha1.Sum(jarBytes)
	h512 := sha512.Sum512(jarBytes)
	s1Hex, s512Hex := hex.EncodeToString(h1[:]), hex.EncodeToString(h512[:])

	mr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "modA", "slug": "moda", "title": "ModA", "project_type": "mod", "server_side": "required"},
			})
		case "/project/modA/version":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": "v1", "project_id": "modA", "version_number": "1.0", "version_type": "release",
				"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
				"files": []map[string]any{{
					"url": "https://cdn.modrinth.com/data/modA/versions/v1/moda-1.0.jar", "filename": "moda-1.0.jar",
					"primary": true, "size": len(jarBytes), "hashes": map[string]string{"sha1": s1Hex, "sha512": s512Hex},
				}},
			}})
		case "/version_files":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mr.Close()

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cart := modcart.New(filepath.Join(base, "data", "modcart"), mr.URL)
	tun := tunnel.NewManager()
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Port: 27111, ModCart: cart, Token: Token})
	srv := New(mgr, tun, reg, catalog.NewAssistant(reg, catalog.NewAI(), Token))
	e := &fullEnv{h: srv}

	// AI 不能调用仅人工的工具箱生成操作
	if _, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcAssistant, Actor: "ai"}, "toolbox.skinpack.build", ops.Args{}); err == nil {
		t.Fatal("AI 不应能调用仅人工的 toolbox.skinpack.build")
	}
	if _, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcAssistant, Actor: "ai"}, "toolbox.mrpack.build", ops.Args{}); err == nil {
		t.Fatal("AI 不应能调用仅人工的 toolbox.mrpack.build")
	}

	// 保存 / 读取草稿
	e.call(t, http.MethodPut, "/api/toolbox/mrpack/draft",
		`{"name":"我的网页整合包","versionId":"2.0.0","core":"fabric","mc":"1.20.1","loaderVersion":"0.16.9"}`, 200)
	rec := e.call(t, http.MethodGet, "/api/toolbox/mrpack/draft", "", 200)
	if !strings.Contains(rec.Body.String(), "我的网页整合包") {
		t.Fatalf("draft get 不对: %s", rec.Body.String())
	}

	// 加入草稿模组后预览并生成 .mrpack
	// 走网页接口加进工具箱自己的那份清单（cart=toolbox）；新建向导的草稿清单不受影响
	e.call(t, http.MethodPost, "/api/modcart", `{"cart":"toolbox","projectIds":["modA"]}`, 200)
	if len(cart.List(modcart.Draft)) != 0 || len(cart.List(modcart.Toolbox)) != 1 {
		t.Fatalf("工具箱的清单和草稿清单没分开: draft=%v toolbox=%v", cart.List(modcart.Draft), cart.List(modcart.Toolbox))
	}
	e.call(t, http.MethodPost, "/api/modcart", `{"cart":"toolbox","inst":"x","projectIds":["modA"]}`, 400)
	e.call(t, http.MethodPost, "/api/modcart", `{"cart":"bogus","projectIds":["modA"]}`, 400)
	// 不能拿 inst 绕到工具箱那份清单上
	e.call(t, http.MethodPost, "/api/modcart/remove", `{"inst":"<toolbox>","projectId":"modA"}`, 404)
	if len(cart.List(modcart.Toolbox)) != 1 {
		t.Fatal("用 inst=<toolbox> 动到了工具箱的清单")
	}
	rec = e.call(t, http.MethodPost, "/api/toolbox/mrpack/build",
		`{"source":"draft","core":"fabric","mc":"1.20.1","loaderVersion":"0.16.9"}`, 200)
	var out struct {
		Data toolbox.MrpackBuildResp `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Data.DownloadID == "" {
		t.Fatalf("mrpack build 响应不对: %s", rec.Body.String())
	}
	dlRec := e.call(t, http.MethodGet, "/api/toolbox/mrpack/download/"+out.Data.DownloadID, "", 200)
	zr, err := zip.NewReader(bytes.NewReader(dlRec.Body.Bytes()), int64(dlRec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundIdx := false
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			foundIdx = strings.Contains(string(b), fmt.Sprintf(`"sha512": %q`, s512Hex))
		}
	}
	if !foundIdx {
		t.Fatal("下载的 .mrpack 中没有找到预期带 sha512 的 modrinth.index.json")
	}
}
