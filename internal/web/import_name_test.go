package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func bareServerZip(t *testing.T, prefix string) []byte {
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
	add := func(name string, data []byte) {
		w, err := zw.Create(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	add("server.jar", jarBuf.Bytes())
	// 让解压后的 CheckLevelName 在本地立刻报错收尾，不走到探测 Java / 联网，也防止清理竞态
	add("server.properties", []byte("server-port=25580\nlevel-name=../outside\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func waitTaskEnded(t *testing.T, e *fullEnv, taskID string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := e.call(t, "GET", "/api/tasks/"+taskID, "", 200)
		var resp struct {
			Data struct {
				Title string `json:"title"`
				Ended bool   `json:"ended"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Data.Ended {
			return resp.Data.Title
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("任务 %s 超时未结束", taskID)
	return ""
}

func uploadImportPack(t *testing.T, e *fullEnv, instName, fileName string, zipBytes []byte) (taskID, packName string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("name", instName)
	_ = mw.WriteField("eula", "true")
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(zipBytes); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/import/modpack", bytes.NewReader(body.Bytes()))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST /api/import/modpack HTTP %d：%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			TaskID string `json:"taskId"`
			Pack   struct {
				Name string `json:"name"`
			} `json:"pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.TaskID, resp.Data.Pack.Name
}

// 上传最外层没有包装文件夹的现成服务端压缩包时，返回的 pack.name 和任务标题都要用整理后的上传文件名，
// 而不是缓存里随机数字临时文件名；有包装文件夹时仍用文件夹名。
func TestImportBareServerZipUsesUploadDisplayName(t *testing.T) {
	e := newFullEnv(t)

	taskID, packName := uploadImportPack(t, e, "测试新服一", "我的服务器.zip", bareServerZip(t, ""))
	if packName != "我的服务器" {
		t.Fatalf("pack.name = %q，期望 %q", packName, "我的服务器")
	}
	title := waitTaskEnded(t, e, taskID)
	if !strings.Contains(title, "我的服务器") || title != "导入服务端 我的服务器" {
		t.Fatalf("任务标题 = %q，期望 %q", title, "导入服务端 我的服务器")
	}

	// 有包装文件夹的现成服务端：仍用文件夹名，不用上传文件名
	taskID2, packName2 := uploadImportPack(t, e, "测试新服二", "上传名.zip", bareServerZip(t, "包装文件夹/"))
	if packName2 != "包装文件夹" {
		t.Fatalf("有包装文件夹时 pack.name = %q，期望 %q", packName2, "包装文件夹")
	}
	_ = waitTaskEnded(t, e, taskID2)
}
