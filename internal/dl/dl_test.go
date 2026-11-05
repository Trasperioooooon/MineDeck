package dl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 第一个地址没下成时调 OnRetry（只给主机名和原因），换第二个地址下成；成功的那次不调。
func TestFetchOnRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.Error(w, "坏了", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	var calls []string
	dest := filepath.Join(t.TempDir(), "a.jar")
	err := Fetch(context.Background(), Request{
		URLs: []string{srv.URL + "/bad", srv.URL + "/good"}, Dest: dest, MinSize: 1024,
		OnRetry: func(h string, err error) { calls = append(calls, h+" "+err.Error()) },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != host+" HTTP 502" {
		t.Fatalf("OnRetry：%q", calls)
	}
	if b, err := os.ReadFile(dest); err != nil || len(b) != 2048 {
		t.Fatalf("下载的文件：%d %v", len(b), err)
	}
}

// 每个地址每一轮都没下成：后面还要试的那几次调 OnRetry，最后一个地址的最后一轮不调（不会再试了），失败原因由返回的错误给出。
func TestFetchOnRetryNotOnLastAttempt(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "坏了", http.StatusBadGateway)
	}))
	defer srv.Close()
	calls := 0
	err := Fetch(context.Background(), Request{
		URLs: []string{srv.URL + "/a", srv.URL + "/b"}, Dest: filepath.Join(t.TempDir(), "a.jar"), MinSize: 1024,
		OnRetry: func(string, error) { calls++ },
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "多次尝试后仍下载失败") {
		t.Fatalf("应该失败：%v", err)
	}
	if calls != 3*2-1 {
		t.Fatalf("OnRetry 调了 %d 次，应为 5 次（3 轮 × 2 个地址，最后一次不调）", calls)
	}
}
