package web

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/diskguard"
	"minedeck/internal/worldimport"
)

// countReader 记下被读走了多少字节。
type countReader struct {
	r io.Reader
	n int64
}

// 请求体在这一小段之后永久停住：不补足 net/http 默认要排空的 256 KiB。
// 同时验证错误能到客户端、连接真正关闭，以及未完成文件被删除。
func TestWorldUploadPausedClientIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		file  bool
		size  int
		limit int64
		want  int
		hold  bool
	}{
		{"超限即回话", true, 96 << 10, 64 << 10, 413, false},
		{"文件传到一半停住", true, 32 << 10, 8 << 20, 408, false},
		{"非文件字段停住", false, 32, 8 << 20, 408, false},
		{"等待同盘写入时超时", true, 32 << 10, 8 << 20, 408, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newFullEnv(t)
			var lease *diskguard.Lease
			if tc.hold {
				var err error
				lease, err = diskguard.Acquire(context.Background(), worldimport.UploadDir(), diskguard.Options{Query: func(string) (int64, error) { return 1 << 40, nil }})
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
			}
			oldLimit, oldIdle := worldUploadLimit, worldUploadIdle
			worldUploadLimit, worldUploadIdle = tc.limit, 150*time.Millisecond
			t.Cleanup(func() { worldUploadLimit, worldUploadIdle = oldLimit, oldIdle })
			closed := make(chan struct{})
			var once sync.Once
			srv := httptest.NewUnstartedServer(e.h)
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					once.Do(func() { close(closed) })
				}
			}
			srv.Start()
			t.Cleanup(srv.Close)
			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			var part io.Writer
			if tc.file {
				part, err = mw.CreateFormFile("file", "map.zip")
			} else {
				part, err = mw.CreateFormField("note")
			}
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write(make([]byte, tc.size))
			// 故意不写末尾 boundary 和 chunked 的终止块。
			_, err = fmt.Fprintf(conn, "POST /api/world/upload HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Token: %s\r\nContent-Type: %s\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n", Token, mw.FormDataContentType(), body.Len())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(body.Bytes()); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(conn, "\r\n"); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("停传以后仍应收到响应：%v", err)
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != tc.want || !resp.Close {
				t.Fatalf("应当收到 %d 并关闭连接：%d close=%v body=%s err=%v", tc.want, resp.StatusCode, resp.Close, b, err)
			}
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("响应以后连接仍未关闭")
			}
			if ents, err := os.ReadDir(worldimport.UploadDir()); err == nil && len(ents) != 0 {
				t.Fatalf("失败上传残留 %d 个文件", len(ents))
			}
			// 连续失败以后仍可取得名额（不能每次超时都漏一个）。
			if lease != nil {
				_ = lease.Close()
			}
			for i := 0; i < 3; i++ {
				p, err := worldimport.SaveUpload(strings.NewReader("ok"), 100, 2)
				if err != nil {
					t.Fatalf("失败后没有释放上传名额：%v", err)
				}
				_ = os.Remove(p)
			}
		})
	}
}

func TestWorldUploadIdleDeadlineTracksProgress(t *testing.T) {
	e := newFullEnv(t)
	old := worldUploadIdle
	worldUploadIdle = 500 * time.Millisecond
	t.Cleanup(func() { worldUploadIdle = old })
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	mw := multipart.NewWriter(pw)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer pw.Close()
		part, err := mw.CreateFormFile("file", "notes.txt")
		if err != nil {
			return
		}
		for i := 0; i < 8; i++ {
			if _, err := io.WriteString(part, "some map bytes\n"); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = mw.Close()
	}()
	req, err := http.NewRequest("POST", srv.URL+"/api/world/upload", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("持续有进展的上传不应受总时长限制：%d %s %v", resp.StatusCode, b, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("上传写入端未退出")
	}
}

func TestUnauthenticatedUploadDoesNotWaitForBody(t *testing.T) {
	e := newFullEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	for _, path := range []string{"/api/world/upload", "/api/transfer/upload", "/api/import/modpack", "/api/modcart/local"} {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: 127.0.0.1\r\nTransfer-Encoding: chunked\r\nContent-Type: multipart/form-data; boundary=test\r\n\r\n4\r\nbody\r\n", path)
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			conn.Close()
			t.Fatalf("%s 未登录也必须立即回话：%v", path, err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		conn.Close()
		if err != nil || resp.StatusCode != 401 || !resp.Close {
			t.Fatalf("%s：HTTP%d close=%v %s %v", path, resp.StatusCode, resp.Close, b, err)
		}
	}
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
