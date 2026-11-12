package dl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 多源都没下成：errors.Is 只认最后一个地址（ErrVerify 的老语义不变），AnyFailure 逐个看每个地址。
// 例子是下载 frpc 时第一个源写盘被杀毒软件拦下（Windows 225 号错误），后面的源只是 404。
func TestAnyFailure(t *testing.T) {
	virus := syscall.Errno(225)
	isVirus := func(e error) bool { return errors.Is(e, virus) }
	err := fmt.Errorf("下载 OpenFrp frpc 失败: %w", newFetchFail([]urlFail{
		{url: "https://a.example/x.zip", err: &os.LinkError{Op: "rename", Old: "x.zip.part", New: "x.zip", Err: virus}},
		{url: "https://b.example/x.zip", err: errors.New("HTTP 404")},
	}, nil))
	if errors.Is(err, virus) {
		t.Fatal("errors.Is 只该认最后一个地址")
	}
	if !AnyFailure(err, isVirus) {
		t.Fatal("AnyFailure 应认出第一个地址的错误")
	}
	if AnyFailure(err, func(e error) bool { return errors.Is(e, ErrVerify) }) {
		t.Fatal("没有哈希校验失败，不该认出来")
	}
	if !AnyFailure(fmt.Errorf("启动失败: %w", virus), isVirus) || AnyFailure(errors.New("boom"), isVirus) {
		t.Fatal("不是多源汇总的错误时，看它自己的包装链")
	}
}

// 同一个源前一轮的失败被后一轮盖掉了（摘要里只留最后一次），AnyFailure 仍要认得出前一轮：
// 真实情形是前一轮写盘被杀毒软件拦下、后一轮超时，这里用先 404 后 419 代替。
func TestAnyFailureSeesEarlierRounds(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		w.WriteHeader(419)
	}))
	defer srv.Close()
	err := Fetch(context.Background(), Request{
		URLs:    []string{srv.URL + "/frpc_windows_amd64.zip"},
		Dest:    filepath.Join(t.TempDir(), "frpc.zip"),
		MinSize: 1 << 20,
	}, nil)
	if err == nil {
		t.Fatal("应该失败")
	}
	if strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("摘要只留最后一次：%s", err)
	}
	if !AnyFailure(err, func(e error) bool { return e.Error() == "HTTP 404" }) {
		t.Fatal("AnyFailure 应认出第一轮的 404")
	}
}

// 前一次失败以后，重试拖到了整体超时：报错还是超时（说法和 errors.Is 都不变），AnyFailure 仍认得出前一次的失败。
func TestAnyFailureAfterDeadline(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		<-r.Context().Done() // 第二次起一直不回，等到客户端超时断开
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := Fetch(ctx, Request{
		URLs:    []string{srv.URL + "/frpc_windows_amd64.zip"},
		Dest:    filepath.Join(t.TempDir(), "frpc.zip"),
		MinSize: 1 << 20,
	}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || err.Error() != context.DeadlineExceeded.Error() {
		t.Fatalf("应该还是超时的错误：%v", err)
	}
	if !AnyFailure(err, func(e error) bool { return e.Error() == "HTTP 404" }) {
		t.Fatal("AnyFailure 应认出超时之前那次 404")
	}
}

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
// 多个地址时认的是最后一个地址最后一次的错误，和汇总之前一样。
func TestFetchFailureUnwrapsVerify(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer bad.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	for _, urls := range [][]string{
		{srv.URL + "/a"},
		{bad.URL + "/a", srv.URL + "/a"},
	} {
		err := Fetch(context.Background(), Request{
			URLs:    urls,
			Dest:    filepath.Join(t.TempDir(), "a.jar"),
			MinSize: 1024,
			SHA256:  strings.Repeat("ab", 32),
		}, nil)
		if !errors.Is(err, ErrVerify) {
			t.Fatalf("%d 个地址：应仍能认出校验失败：%v", len(urls), err)
		}
		if !strings.Contains(err.Error(), "SHA-256") {
			t.Fatalf("摘要里应有校验失败：%v", err)
		}
	}
}

// 连不上的源：摘要里只有主机名和底层原因，不带完整地址和查询串。
func TestFetchFailureHidesURL(t *testing.T) {
	old := retryWait
	retryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { retryWait = old }()
	err := Fetch(context.Background(), Request{
		URLs:    []string{"http://127.0.0.1:0/client/frpc_windows_amd64.zip?sig=secret"},
		Dest:    filepath.Join(t.TempDir(), "frpc.zip"),
		MinSize: 1 << 20,
	}, nil)
	if err == nil {
		t.Fatal("应该失败")
	}
	msg := err.Error()
	if !strings.Contains(msg, "（127.0.0.1:0: ") || strings.Contains(msg, "sig=secret") || strings.Contains(msg, "frpc_windows_amd64.zip") {
		t.Fatalf("摘要应只有主机名和原因：%s", msg)
	}
}

// 连接类错误去掉地址。本机写文件出错时报错是「操作 路径: 原因」，原因在最后，要原样保留，不能截掉。
func TestCompactReason(t *testing.T) {
	ue := &url.Error{Op: "Get", URL: "https://staticassets.naids.com/client/OF_x/frpc_windows_amd64.zip?sig=abc", Err: errors.New("dial tcp: i/o timeout")}
	if got := compactReason(ue); got != "dial tcp: i/o timeout" {
		t.Fatalf("应去掉 URL：%q", got)
	}
	part := `C:\Users\张三\Desktop\MineDeck\cache\frpc\openfrp-frpc.zip.part`
	for _, e := range []error{
		&os.LinkError{Op: "rename", Old: part, New: strings.TrimSuffix(part, ".part"), Err: errors.New("The process cannot access the file because it is being used by another process.")},
		&os.PathError{Op: "write", Path: part, Err: errors.New("There is not enough space on the disk.")},
	} {
		if got := compactReason(e); got != e.Error() {
			t.Fatalf("本机文件报错应原样保留：\n得到 %q\n原文 %q", got, e.Error())
		}
	}
	if got := compactReason(errors.New("a\n  b\tc")); got != "a b c" {
		t.Fatalf("换行和连续空白应压成一个空格：%q", got)
	}
}
