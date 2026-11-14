package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
