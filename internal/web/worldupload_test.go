package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"minedeck/internal/worldimport"
)

// countReader 记下被读走了多少字节。
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// 上传被拒（太大、同时上传的太多、盘快满）的时候，不把请求体剩下的部分读完再回话。
// 以前拒绝之前先关掉这一段，而关它就是把剩下的内容全读一遍：几个 GB 的上传要读到头，才回得了一句「不收」
// （2026-11-14 Codex 第二轮复审）。这里把上限调小，拿「超过上限」这一种来测，另外两种走的是同一段代码。
func TestWorldUploadRejectionDoesNotDrainBody(t *testing.T) {
	e := newFullEnv(t)
	old := worldUploadLimit
	t.Cleanup(func() { worldUploadLimit = old })
	worldUploadLimit = 64 << 10

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "big.zip")
	if err != nil {
		t.Fatal(err)
	}
	const size = 900 << 10 // 比上限大得多，又没到请求体总上限（上限 + 1 MiB），所以以前会被从头读到尾
	if _, err := fw.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	cr := &countReader{r: bytes.NewReader(body.Bytes())}
	req := httptest.NewRequest("POST", "/api/world/upload", cr) // 不是 bytes.Reader：长度未知，和分块上传一样
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超过上限应当是 413：HTTP %d %s", rec.Code, rec.Body)
	}
	if cr.n > 256<<10 {
		t.Errorf("拒绝之前不该把剩下的请求体读完：一共 %d 字节，读走了 %d 字节", body.Len(), cr.n)
	}
	// 没收下的文件不留在缓存目录里
	if ents, _ := os.ReadDir(worldimport.UploadDir()); len(ents) != 0 {
		t.Errorf("被拒的上传不该留下文件：%d 个", len(ents))
	}
}

// 同一件事走一条真的连接：对方还在往上传，服务端不等它传完就回了 413，对方收得到这句话。
// 这里的对方是 Go 自带的 HTTP 客户端；浏览器和窗口里的 WebView 碰到「请求体没传完连接就被关掉」会怎么显示，
// 这条测试说明不了，没有在真浏览器里验证过。
func TestWorldUploadRejectionReachesClientMidUpload(t *testing.T) {
	e := newFullEnv(t)
	old := worldUploadLimit
	t.Cleanup(func() { worldUploadLimit = old })
	worldUploadLimit = 64 << 10
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	sent := make(chan int64, 1)
	go func() { // 一小块一小块地传 4 MiB；服务端不收了以后写会出错，到此为止
		var n int64
		defer func() { pw.Close(); sent <- n }()
		fw, err := mw.CreateFormFile("file", "big.zip")
		if err != nil {
			return
		}
		chunk := make([]byte, 16<<10)
		for n < 4<<20 {
			w, err := fw.Write(chunk)
			n += int64(w)
			if err != nil {
				return
			}
		}
		mw.Close()
	}()
	req, err := http.NewRequest("POST", srv.URL+"/api/world/upload", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("对方应当收到服务端的回话，而不是连接出错：%v", err)
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(text), "超过了 8 GB 的上限") {
		t.Fatalf("应当收到 413 和那句说明：HTTP %d %s", resp.StatusCode, text)
	}
	pr.CloseWithError(io.ErrClosedPipe) // 让还在写的那一头停下
	// 传了多少不在这里看：本机回环的缓冲区可能把剩下的全吃进去。服务端没有去读，由上一条测试看住
	select {
	case <-sent:
	case <-time.After(10 * time.Second):
		t.Error("上传那一头没有停下")
	}
}
