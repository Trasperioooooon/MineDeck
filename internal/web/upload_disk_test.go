package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/ops/catalog"
)

func multipartRequest(t *testing.T, url, filename string, content []byte) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("before", "保留前面的字段")
	f, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("after", "保留后面的字段")
	_ = mw.Close()
	r, err := http.NewRequest("POST", url, bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Token", Token)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

// 别的操作（地图暂存、备份）占着同一个盘的独占写入名额时，上传不排在它后面：照常收、照常回话，
// 不会因为「等名额」被当成没有进展掐掉。上一版里接收上传也占独占名额，排队超过空闲期限就回 408
// 「上传长时间没有新数据」，而对方其实一直在传（2026-11-14 第三轮修复的复审，发现 2）。
func TestMultipartUploadsDoNotQueueBehindVolumeLease(t *testing.T) {
	e := newFullEnv(t)
	oldIdle, oldMemory := worldUploadIdle, multipartMemory
	worldUploadIdle, multipartMemory = 150*time.Millisecond, 16
	t.Cleanup(func() { worldUploadIdle, multipartMemory = oldIdle, oldMemory })
	spool := t.TempDir()
	t.Setenv("TMP", spool)
	t.Setenv("TEMP", spool)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	lease, err := diskguard.Acquire(context.Background(), spool, diskguard.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	// 名额一直被别人占着。等的时间比空闲期限长得多：要是上传在排队，这里就会超时
	started := time.Now()
	for _, endpoint := range []string{"/api/transfer/upload", "/api/import/modpack", "/api/modcart/local"} {
		resp, err := client.Do(multipartRequest(t, srv.URL+endpoint, "pack.zip", mdpackBytes(true)))
		if err != nil {
			t.Fatalf("名额被占着时上传也必须回话：%s %v", endpoint, err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 后两个入口收到的不是它们要的那种文件，回什么由各自的校验决定；要守的只是不能回「没有进展」
		if err != nil || resp.StatusCode == 408 || strings.Contains(string(b), "长时间没有新数据") {
			t.Fatalf("%s：不该被当成没有进展：HTTP %d %s %v", endpoint, resp.StatusCode, b, err)
		}
		if endpoint == "/api/transfer/upload" && resp.StatusCode != 200 {
			t.Fatalf("%s：名额被占着时应当照常收下：HTTP %d %s", endpoint, resp.StatusCode, b)
		}
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("三次上传一共用了 %v，像是在等名额", took)
	}
	worldUploadIdle = 2 * time.Second
	if files, _ := os.ReadDir(spool); len(files) != 0 {
		t.Fatalf("回完话以后临时文件应当已经清掉：%v", files)
	}
	// 再走一遍真正的上传摘要入口，核对内容：16 字节的内存额度迫使 multipart 落盘，
	// 先解析再复制都在同一个盘上，名额仍被别人占着。
	want := mdpackBytes(true)
	resp, err := client.Do(multipartRequest(t, srv.URL+"/api/transfer/upload", "normal.mdpack", want))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("正常上传：HTTP %d %s %v", resp.StatusCode, b, err)
	}
	var result struct {
		Data struct {
			File string `json:"file"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(result.Data.File)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("缓存副本必须完整：%v", err)
	}
	if files, _ := os.ReadDir(spool); len(files) != 0 {
		t.Fatalf("正常上传后临时文件未清：%v", files)
	}
	// 上传没有碰那个名额：占着的人放掉以后，别人马上拿得到。
	_ = lease.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	l, err := diskguard.Acquire(ctx, filepath.Dir(result.Data.File), diskguard.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}

func TestMultipartUploadsRejectUnknownAndFullVolumes(t *testing.T) {
	e := newFullEnv(t)
	old := uploadDiskFree
	t.Cleanup(func() { uploadDiskFree = old })
	for _, tc := range []struct {
		name string
		free int64
		err  error
		code int
	}{{"查不到", 0, errors.New("查询失败"), 503}, {"余量不足", 1, nil, 507}} {
		for _, cachePhase := range []bool{false, true} {
			for _, endpoint := range []string{"/api/transfer/upload", "/api/import/modpack", "/api/modcart/local"} {
				uploadDiskFree = func(p string) (int64, error) {
					if cachePhase && !strings.HasPrefix(strings.ToLower(filepath.Clean(p)), strings.ToLower(filepath.Clean(app.CacheDir))+string(filepath.Separator)) {
						return 1 << 40, nil
					}
					return tc.free, tc.err
				}
				req := multipartRequest(t, endpoint, "pack.zip", mdpackBytes(true))
				req.Host = "127.0.0.1"
				req.RemoteAddr = "127.0.0.1:52000"
				res := httptest.NewRecorder()
				e.h.ServeHTTP(res, req)
				if res.Code != tc.code {
					t.Fatalf("%s %s 缓存阶段=%v：HTTP %d %s", tc.name, endpoint, cachePhase, res.Code, res.Body)
				}
			}
		}
	}
	for _, dir := range []string{filepath.Join(app.CacheDir, "import"), filepath.Join(app.CacheDir, "transfer", "import"), catalog.LocalUploadDir()} {
		if entries, _ := os.ReadDir(dir); len(entries) > 0 {
			t.Fatalf("被拒上传留下缓存：%s %v", dir, entries)
		}
	}
}

func TestMultipartFieldsSurviveDiskSpooling(t *testing.T) {
	old := multipartMemory
	multipartMemory = 16
	t.Cleanup(func() { multipartMemory = old })
	r := multipartRequest(t, "/upload", "data.zip", bytes.Repeat([]byte("x"), 1000))
	w := httptest.NewRecorder()
	r, cleanup := beginUpload(w, r)
	defer cleanup()
	if err := parseUpload(w, r, 1<<20, "too large"); err != nil {
		t.Fatal(err)
	}
	if r.FormValue("before") != "保留前面的字段" || r.FormValue("after") != "保留后面的字段" {
		t.Fatal("文件前后的表单值未保留")
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil || len(b) != 1000 {
		t.Fatalf("multipart文件：%d %v", len(b), err)
	}
}
