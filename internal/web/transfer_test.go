package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
)

// 交接包（.mdpack）三个端点的 HTTP 合同：上传落在缓存目录并回摘要、导入拿任务号、
// 导出在远程请求时忽略主机目录参数。导入任务本身要联网（瘦身的包要重新下载核心），
// 测试环境不许联网，任务会失败——这里只验证 HTTP 层把参数交给操作层时不出错。

// mdpackBytes 造一个内容最小的合法交接包。
func mdpackBytes(full bool) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	inst := `{"name":"A 的服","core":"vanilla","mc":"1.21.1","javaMajor":17,"xmxMb":2048,"xmsMb":1024,"launchTarget":"jar:server.jar","createdAt":"2026-01-01T00:00:00Z"}`
	mf := fmt.Sprintf(`{"formatVersion":1,"mode":"handover","minedeckVersion":%q,"exportedAt":"2026-11-10T00:00:00Z",`+
		`"instance":%s,"core":{"core":"vanilla","mc":"1.21.1","kind":"game"},`+
		`"mods":[],"plugins":[],"worldBytes":5,"excluded":[],"full":%v,"counts":{"files":2,"bytes":30}}`, "3.0.0", inst, full)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(mf))
	w, _ = zw.Create("instance/server.properties")
	w.Write([]byte("server-port=25570\nlevel-name=world\n"))
	w, _ = zw.Create("instance/world/level.dat")
	w.Write([]byte("level"))
	if full {
		w, _ = zw.Create("instance/server.jar")
		w.Write([]byte("fake server"))
		w, _ = zw.Create("instance/eula.txt")
		w.Write([]byte("eula=true\n"))
	}
	zw.Close()
	return buf.Bytes()
}

// uploadForm 发一次 multipart 上传，断言状态码，返回响应体。
func (e *fullEnv) uploadForm(t *testing.T, field, filename string, content []byte, want int) []byte {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/transfer/upload", bytes.NewReader(body.Bytes()))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Errorf("上传 %s：HTTP %d，应为 %d：%s", filename, rec.Code, want, rec.Body)
	}
	return rec.Body.Bytes()
}

func TestTransferRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服

	// 不是 .mdpack / .zip 的文件当场拒绝
	e.uploadForm(t, "file", "pack.rar", []byte("x"), 400)

	// 坏包（不是 zip）：报「打不开」
	bad := e.uploadForm(t, "file", "bad.mdpack", []byte("not a zip"), 400)
	if !strings.Contains(string(bad), "打不开") {
		t.Errorf("坏包的报错：%s", bad)
	}
	// 里面没有 manifest.json 的 zip 也拒绝
	e.uploadForm(t, "file", "plain.zip", mdpacklessZip(), 400)

	// 正经包：上传 → 摘要
	var up struct {
		Data struct {
			Name  string `json:"name"`
			Core  string `json:"core"`
			MC    string `json:"mc"`
			Mode  string `json:"mode"`
			Port  int    `json:"port"`
			Full  bool   `json:"full"`
			Mods  int    `json:"mods"`
			World int64  `json:"worldBytes"`
		} `json:"data"`
	}
	upBody := e.uploadForm(t, "file", "pack.mdpack", mdpackBytes(true), 200)
	if err := json.Unmarshal(upBody, &up); err != nil {
		t.Fatalf("摘要不是 JSON：%s", upBody)
	}
	if up.Data.Name != "A 的服" || up.Data.Core != "vanilla" || up.Data.MC != "1.21.1" || !up.Data.Full || up.Data.Port != 25570 {
		t.Errorf("摘要不对：%s", upBody)
	}
	sumFile := filepath.Join(app.CacheDir, "transfer", "import")
	// 上传的文件确实落在缓存目录（操作层只认这个目录）
	ents, err := os.ReadDir(sumFile)
	if err != nil || len(ents) == 0 {
		t.Errorf("上传的文件没落缓存目录：%v", err)
	}
	var cachedFile string
	for _, en := range ents {
		if strings.HasSuffix(en.Name(), ".mdpack") && !strings.HasPrefix(en.Name(), "bad") && !strings.HasPrefix(en.Name(), "plain") {
			cachedFile = filepath.Join(sumFile, en.Name())
		}
	}
	if cachedFile == "" {
		t.Fatalf("缓存目录里找不到刚上传的包：%v", ents)
	}

	// 导入：重名拒
	dup := e.call(t, "POST", "/api/transfer/import", `{"file":"`+filepath.ToSlash(cachedFile)+`","name":"生存服","eula":true}`, 400)
	if !strings.Contains(dup.Body.String(), "已存在") {
		t.Errorf("重名导入的报错：%s", dup.Body)
	}
	// 导入：文件不在上传目录里拒
	e.call(t, "POST", "/api/transfer/import", `{"file":"C:\\Windows\\evil.mdpack","name":"新服","eula":true}`, 400)
	// 导入：新名字拿任务号（任务联网下核心，测试里不许联网，会失败——HTTP 层给对任务号即可）
	start := e.call(t, "POST", "/api/transfer/import", `{"file":"`+filepath.ToSlash(cachedFile)+`","name":"接手服","eula":true}`, 200)
	var ir struct {
		Data struct {
			TaskID string `json:"taskId"`
			Pack   struct {
				Name string `json:"name"`
				MC   string `json:"mc"`
			} `json:"pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &ir); err != nil || ir.Data.TaskID == "" || ir.Data.Pack.Name != "A 的服" {
		t.Errorf("导入响应：%s (%v)", start.Body, err)
	}
	// 上传的包由导入任务收尾时删除；任务在测试环境里联网不成会失败，等它结束再走，
	// 免得后台还在写测试的临时目录
	deadline := time.Now().Add(45 * time.Second)
	for {
		task := e.call(t, "GET", "/api/tasks/"+ir.Data.TaskID, "", 200)
		if strings.Contains(task.Body.String(), `"ended":true`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("导入任务没在时限内结束：%s", task.Body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(cachedFile); !os.IsNotExist(err) {
		t.Errorf("导入结束后上传的包该删掉")
	}

	// 导出：实例停着就能打包（不联网）；远程请求的 dest 被忽略，落缓存目录
	e.call(t, "POST", ip+"/transfer/export", `{"dest":"C:\\裸奔"}`, 200, fromLAN)
	e.call(t, "POST", ip+"/transfer/export", `{"full":true}`, 200)
	// 不存在的实例报「实例不存在」
	e.call(t, "POST", "/api/instances/nope/transfer/export", `{}`, 400)

}

// mdpacklessZip 一个没有 manifest.json 的普通 zip。
func mdpacklessZip() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("readme.txt")
	w.Write([]byte("只是个 zip"))
	zw.Close()
	return buf.Bytes()
}
