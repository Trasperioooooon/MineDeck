package dl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
