package dl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type atomicRoundTripper func(*http.Request) (*http.Response, error)

func (f atomicRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type gatedDownload struct {
	gate <-chan struct{}
	r    io.Reader
}

func (r *gatedDownload) Read(p []byte) (int, error) { <-r.gate; return r.r.Read(p) }
func (r *gatedDownload) Close() error               { return nil }

func TestSameDestinationDownloadWaitsAndCanCancel(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "replacement data"
	sum := sha256.Sum256([]byte(content))
	started, resume := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	hc := &http.Client{Transport: atomicRoundTripper(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(content)), Body: &gatedDownload{gate: resume, r: strings.NewReader(content)}}, nil
	})}
	req := Request{URLs: []string{"https://example.invalid/file"}, Dest: dest, SHA256: hex.EncodeToString(sum[:]), Client: hc}
	done := make(chan error, 1)
	go func() { done <- Fetch(context.Background(), req, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("下载没有开始")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	err := Fetch(ctx, req, nil)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("等同一目标下载时不能取消：%v", err)
	}
	if calls.Load() != 1 {
		t.Error("同一目标重复下载")
	}
	if b, _ := os.ReadFile(dest); string(b) != "original" {
		t.Error("下载完成前改变了原件")
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != content {
		t.Fatal("没有发布完整下载")
	}
	ents, _ := os.ReadDir(filepath.Dir(dest))
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("临时文件残留：%s", e.Name())
		}
	}
}

func TestFailedDownloadKeepsOriginal(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryWait = old })
	dir := t.TempDir()
	dest := filepath.Join(dir, "data")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Transport: atomicRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 3, Body: io.NopCloser(strings.NewReader("bad"))}, nil
	})}
	sum := sha256.Sum256([]byte("good"))
	err := Fetch(context.Background(), Request{URLs: []string{"https://example.invalid/file"}, Dest: dest, SHA256: hex.EncodeToString(sum[:]), Client: hc}, nil)
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("应是校验错误：%v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "original" {
		t.Fatal("失败后原件丢失")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("失败后残留临时文件：%d", len(ents))
	}
}
