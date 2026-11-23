package web

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/java"
)

var (
	fakeVanillaOnce sync.Once
	fakeVanillaURL  string
)

func ensureFakeVanillaServer() string {
	fakeVanillaOnce.Do(func() {
		fakeJar := bytes.Repeat([]byte("J"), 1<<20)
		jarSum := sha1.Sum(fakeJar)
		jarSHA1 := hex.EncodeToString(jarSum[:])
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/1.21.4.json":
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"javaVersion":{"majorVersion":21},"downloads":{"server":{"sha1":%q,"url":%q}}}`, jarSHA1, srv.URL+"/server.jar")
			case "/server.jar":
				_, _ = w.Write(fakeJar)
			default:
				http.NotFound(w, r)
			}
		}))
		fakeVanillaURL = srv.URL
	})
	return fakeVanillaURL
}

func runnableServerZip(t *testing.T) []byte {
	t.Helper()
	var jarBuf bytes.Buffer
	jw := zip.NewWriter(&jarBuf)
	for name, body := range map[string]string{
		"META-INF/MANIFEST.MF":   "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n",
		"META-INF/versions.list": "abc\t1.20.1\t1.20.1/paper-1.20.1.jar\n",
		"version.json":           `{"id":"1.20.1","java_version":17}`,
	} {
		w, err := jw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := jw.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{
		"server.jar":        jarBuf.Bytes(),
		"server.properties": []byte("server-port=25580\n"),
		"eula.txt":          []byte("eula=true\n"),
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPortSuggestAndSubmitRecheck(t *testing.T) {
	e := newFullEnv(t)

	// 1) GET /api/ports/suggest 识别系统保留段并跳过
	restore := inst.SetPortProbesForTest(
		map[int]string{},
		[][2]int{{25565, 25567}},
	)
	defer restore()

	rec := e.call(t, "GET", "/api/ports/suggest?from=25567", "", 200)
	var sug struct {
		Data struct {
			Port         int  `json:"port"`
			From         int  `json:"from"`
			FromBusy     bool `json:"fromBusy"`
			FromReserved bool `json:"fromReserved"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sug); err != nil {
		t.Fatal(err)
	}
	if sug.Data.Port != 25568 || !sug.Data.FromBusy || !sug.Data.FromReserved {
		t.Fatalf("保留段里的端口预查不对：%+v", sug.Data)
	}

	// 2) 新建表单提交时：默认端口或自动换出的端口在表单打开后被别的程序占上、或落在保留段里，一律拒绝创建并回传改端口提示；
	//    但本软件自己的服务器（25565 上的「生存服」，真开着、监听表里也占着 25565）占着端口时，提交不拦（R3）。
	t.Setenv("MINEDECK_FAKE_SERVER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, major := range []int{17, 21} {
		if err := os.MkdirAll(filepath.Dir(java.Exe(major)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(java.Exe(major), []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dlBase := ensureFakeVanillaServer()
	if err := os.MkdirAll(app.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	maniJSON := fmt.Sprintf(`{"latest":{"release":"1.21.4"},"versions":[`+
		`{"id":"1.21.4","type":"release","url":%q},`+
		`{"id":"1.21.1","type":"release"},{"id":"1.20.1","type":"release"},`+
		`{"id":"1.18.1","type":"release"},{"id":"1.17","type":"release"},`+
		`{"id":"1.16.5","type":"release"},{"id":"1.12.2","type":"release"},`+
		`{"id":"1.12","type":"release"},{"id":"1.8.9","type":"release"},{"id":"1.7","type":"release"}]}`,
		dlBase+"/1.21.4.json")
	if err := os.WriteFile(filepath.Join(app.CacheDir, "version_manifest_v2.json"), []byte(maniJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	surv, err := e.mgr.Get("生存服")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(surv.PropsPath(), []byte("server-ip=127.0.0.1\nserver-port=25565\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.SetJava("生存服", 21, exe); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Start("生存服"); err != nil {
		t.Fatalf("启动生存服失败：%v", err)
	}
	t.Cleanup(func() {
		if surv.Status() != "stopped" {
			_ = e.mgr.Kill("生存服")
			deadline := time.Now().Add(5 * time.Second)
			for surv.Status() != "stopped" && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for surv.Status() != "running" {
		if time.Now().After(deadline) {
			t.Fatalf("等待生存服进入 running 超时，当前状态 %s", surv.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}

	restore2 := inst.SetPortProbesForTest(
		map[int]string{25565: "java.exe", 25568: "java.exe"},
		[][2]int{{25570, 25572}},
	)
	defer restore2()

	recOwn := e.call(t, "POST", "/api/instances",
		`{"name":"共用自家端口服","core":"vanilla","mc":"1.21.4","port":25565,"eula":true}`,
		http.StatusOK, fromUI)
	var ownTask struct {
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recOwn.Body.Bytes(), &ownTask); err != nil {
		t.Fatal(err)
	}
	if ownTask.Data.TaskID == "" {
		t.Fatalf("另一台真开着时提交同端口应成功返回创建任务号：%s", recOwn.Body.String())
	}
	_ = waitTaskEnded(t, e, ownTask.Data.TaskID)
	if _, err := e.mgr.Get("共用自家端口服"); err != nil {
		t.Fatalf("新建同端口服务器应真正建成实例：%v", err)
	}

	rec = e.call(t, "POST", "/api/instances",
		`{"name":"占端口新服","core":"paper","mc":"1.21.4","port":25568,"eula":true}`,
		http.StatusConflict, fromUI)
	var busyResp struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		PortBusy struct {
			Port     int    `json:"port"`
			Process  string `json:"process"`
			Reserved bool   `json:"reserved"`
			Suggest  int    `json:"suggest"`
		} `json:"portBusy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &busyResp); err != nil {
		t.Fatal(err)
	}
	if busyResp.OK || busyResp.PortBusy.Port != 25568 || busyResp.PortBusy.Process != "java.exe" || busyResp.PortBusy.Suggest != 25566 {
		t.Fatalf("被别的程序占着时应回传改端口信息：%+v", busyResp)
	}
	if r := e.call(t, "GET", "/api/instances", "", 200); strings.Contains(r.Body.String(), "占端口新服") {
		t.Fatalf("端口被占时不应创建服务器：%s", r.Body.String())
	}

	// 落在系统保留段里同样拦下，并标明 reserved
	rec = e.call(t, "POST", "/api/instances",
		`{"name":"保留段新服","core":"paper","mc":"1.21.4","port":25571,"eula":true}`,
		http.StatusConflict, fromUI)
	busyResp = struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		PortBusy struct {
			Port     int    `json:"port"`
			Process  string `json:"process"`
			Reserved bool   `json:"reserved"`
			Suggest  int    `json:"suggest"`
		} `json:"portBusy"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &busyResp); err != nil {
		t.Fatal(err)
	}
	if !busyResp.PortBusy.Reserved || busyResp.PortBusy.Port != 25571 || !strings.Contains(busyResp.Error, "系统保留") {
		t.Fatalf("系统保留段应拦下并说明原因：%+v", busyResp)
	}

	// 3) 导入整合包提交时：同样复查端口，被占就不建服、不留缓存压缩包
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("name", "占端口导入服")
	_ = mw.WriteField("port", "25568")
	_ = mw.WriteField("eula", "true")
	fw, err := mw.CreateFormFile("file", "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(bareServerZip(t, ""))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/import/modpack", &body)
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	recImp := httptest.NewRecorder()
	e.h.ServeHTTP(recImp, req)
	if recImp.Code != http.StatusConflict {
		t.Fatalf("导入整合包端口被占应回 409，实际 %d：%s", recImp.Code, recImp.Body.String())
	}
	if ents, err := os.ReadDir(filepath.Join(app.CacheDir, "import")); err == nil && len(ents) > 0 {
		t.Fatalf("端口被占拦下导入时不应在 cache/import 留下上传文件：%v", ents)
	}

	// 导入整合包若用正在运行的自家服务器同端口（25565），同样只提示不拦（200），且能真正导入建成实例
	{
		if surv.Status() != "running" {
			t.Fatalf("测试前提：生存服应仍在运行，实际 %s", surv.Status())
		}
		var ownBody bytes.Buffer
		ownMW := multipart.NewWriter(&ownBody)
		_ = ownMW.WriteField("name", "共用自家端口导入服")
		_ = ownMW.WriteField("port", "25565")
		_ = ownMW.WriteField("eula", "true")
		ownFW, err := ownMW.CreateFormFile("file", "pack.zip")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = ownFW.Write(runnableServerZip(t))
		_ = ownMW.Close()
		ownReq := httptest.NewRequest("POST", "/api/import/modpack", &ownBody)
		ownReq.Host = "127.0.0.1"
		ownReq.RemoteAddr = "127.0.0.1:52000"
		ownReq.Header.Set("X-Token", Token)
		ownReq.Header.Set("Sec-Fetch-Site", "same-origin")
		ownReq.Header.Set("Content-Type", ownMW.FormDataContentType())
		ownRec := httptest.NewRecorder()
		e.h.ServeHTTP(ownRec, ownReq)
		if ownRec.Code != http.StatusOK {
			t.Fatalf("导入整合包用自家运行中服务器同端口不应被 409 拦下，实际 %d：%s", ownRec.Code, ownRec.Body.String())
		}
		var ownImpTask struct {
			Data struct {
				TaskID string `json:"taskId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(ownRec.Body.Bytes(), &ownImpTask); err != nil {
			t.Fatal(err)
		}
		_ = waitTaskEnded(t, e, ownImpTask.Data.TaskID)
		if _, err := e.mgr.Get("共用自家端口导入服"); err != nil {
			t.Fatalf("导入同端口服务器应真正建成实例：%v", err)
		}
	}

	// 4) 真正在回环地址上占住一个端口，走系统真实监听表也能拦下
	restoreReal := inst.UseRealPortProbesForTest()
	defer restoreReal()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	livePort := ln.Addr().(*net.TCPAddr).Port
	recLive := e.call(t, "POST", "/api/instances",
		fmt.Sprintf(`{"name":"真实监听服","core":"paper","mc":"1.21.4","port":%d,"eula":true}`, livePort),
		http.StatusConflict, fromUI)
	if !strings.Contains(recLive.Body.String(), fmt.Sprintf(`"port":%d`, livePort)) {
		t.Fatalf("真实监听端口 %d 应被拦下：%s", livePort, recLive.Body.String())
	}
}
