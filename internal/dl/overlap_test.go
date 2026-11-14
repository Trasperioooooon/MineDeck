package dl

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 缓存命中（目标文件已经在、哈希对得上）以后，目标文件必须还在、内容不变。
// 第三轮修复里 verifyFile 多了两行，校验完会去删目标文件；当时没出事只是因为 Windows 不让删还开着的文件
// （2026-11-14 第三轮修复的复审，发现 4）。
func TestCacheHitKeepsFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cached.jar")
	data := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(data)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte("别的内容")) }))
	defer srv.Close()
	if err := Fetch(context.Background(), Request{URLs: []string{srv.URL}, Dest: dest, SHA1: hex.EncodeToString(sum[:]), Client: srv.Client()}, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("缓存命中不该再去下载：请求了 %d 次", calls)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("缓存命中以后目标文件应当原样还在：%v", err)
	}
}

// 落在同一个盘上的两个下载必须能同时进行：服务端等两个请求都到了才回内容。
// 第三轮修复里下载从发请求起就占着整个盘的独占写入名额，第二个下载要等第一个整个传完才发请求，
// 整合包的 6 路并发下载变成了一个一个来（2026-11-14 第三轮修复的复审，发现 2）。
func TestDownloadsToOneVolumeOverlap(t *testing.T) {
	var mu sync.Mutex
	inflight, peak := 0, 0
	both := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inflight++
		peak = max(peak, inflight)
		if inflight == 2 {
			once.Do(func() { close(both) })
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(3 * time.Second): // 等不到第二个请求：放行，让测试报错而不是卡住
		}
		w.Write(bytes.Repeat([]byte("x"), 2048))
		mu.Lock()
		inflight--
		mu.Unlock()
	}))
	defer srv.Close()
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Fetch(context.Background(), Request{URLs: []string{fmt.Sprintf("%s/%d", srv.URL, i)},
				Dest: filepath.Join(dir, fmt.Sprintf("f%d.bin", i)), Client: srv.Client()}, nil)
			if err != nil {
				t.Errorf("下载 %d 失败：%v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if peak < 2 {
		t.Fatalf("两个下载没有同时进行（同时在途的请求最多 %d 个）", peak)
	}
	for i := 0; i < 2; i++ {
		if st, err := os.Stat(filepath.Join(dir, fmt.Sprintf("f%d.bin", i))); err != nil || st.Size() != 2048 {
			t.Errorf("下载 %d 的文件不对：%v", i, err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".minedeck-download-*")); len(left) != 0 {
		t.Errorf("不该留下临时文件：%v", left)
	}
}
