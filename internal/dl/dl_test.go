package dl

import (
	"context"
	"errors"
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
	host := strings.TrimPrefix(srv.URL, "http://")
	if strings.Count(err.Error(), host) != 1 || strings.Count(err.Error(), "HTTP 502") != 1 {
		t.Fatalf("同一主机同一种失败只应出现一次：%s", err)
	}
	if calls != 3*2-1 {
		t.Fatalf("OnRetry 调了 %d 次，应为 5 次（3 轮 × 2 个地址，最后一次不调）", calls)
	}
}

// 每个源的最后一次失败都留在最终错误里（主机名 + 状态），三轮重试不把同一条重复三遍。
// 同一源先 404 再 419 时，摘要里只留最后的 419。
func TestFetchReportsEachSource(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer a.Close()
	var bHits int
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits++
		if bHits == 1 {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		w.WriteHeader(419)
	}))
	defer b.Close()
	err := Fetch(context.Background(), Request{
		URLs:    []string{a.URL + "/frpc_windows_amd64.zip", b.URL + "/frpc_windows_amd64.zip"},
		Dest:    filepath.Join(t.TempDir(), "frpc.zip"),
		MinSize: 1 << 20,
	}, nil)
	if err == nil {
		t.Fatal("应该失败")
	}
	msg := err.Error()
	hostA := strings.TrimPrefix(a.URL, "http://")
	hostB := strings.TrimPrefix(b.URL, "http://")
	wantA := hostA + ": HTTP 404"
	wantB := hostB + ": HTTP 419"
	if !strings.Contains(msg, "多次尝试后仍下载失败（"+wantA+"；"+wantB+"）") {
		t.Fatalf("应列出两个源的最后一次失败：%s", msg)
	}
	if strings.Count(msg, hostA) != 1 || strings.Count(msg, hostB) != 1 || strings.Contains(msg, hostB+": HTTP 404") {
		t.Fatalf("每个源只留最后一次：%s", msg)
	}
}

// 哈希对不上时，汇总后的错误仍能被 errors.Is 认出，调用方好把文件标成待用户处置。
func TestFetchFailureUnwrapsVerify(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	err := Fetch(context.Background(), Request{
		URLs:    []string{srv.URL + "/a"},
		Dest:    filepath.Join(t.TempDir(), "a.jar"),
		MinSize: 1024,
		SHA256:  strings.Repeat("ab", 32),
	}, nil)
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("应仍能认出校验失败：%v", err)
	}
	if !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("摘要里应有校验失败：%v", err)
	}
}

func TestCompactReason(t *testing.T) {
	got := compactReason(errors.New(`Get "https://staticassets.naids.com/client/OF_x/frpc_windows_amd64.zip": dial tcp: i/o timeout`))
	if got != "dial tcp: i/o timeout" {
		t.Fatalf("应去掉 URL：%q", got)
	}
	long := strings.Repeat("错", fetchReasonMax+10)
	got = compactReason(errors.New(long))
	if r := []rune(got); len(r) != fetchReasonMax+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("超长原因应截断：%q（%d）", got, len([]rune(got)))
	}
}
