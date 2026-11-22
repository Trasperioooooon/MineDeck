package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
)

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

	// 2) 新建表单提交时：默认端口或自动换出的端口在表单打开后被别的程序占上、或落在保留段里，一律拒绝创建并回传改端口提示
	restore2 := inst.SetPortProbesForTest(
		map[int]string{25568: "java.exe"},
		[][2]int{{25570, 25572}},
	)
	defer restore2()

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
