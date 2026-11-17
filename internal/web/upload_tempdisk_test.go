package web

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 临时盘余量不够、但存放缓存的目标盘够用时：
//   - 不超过 multipartMemory（64 MiB）的上传整段留在内存、不写临时盘，应当照常成功；
//   - 超过 multipartMemory 或长度未知（ContentLength == -1）的上传可能写临时盘，
//     应当被拒（HTTP 507），且报错写明是系统临时文件夹所在磁盘及盘符。
func TestUploadChecksTempDiskOnlyWhenSpoolingPossible(t *testing.T) {
	e := newFullEnv(t)
	oldFree := uploadDiskFree
	t.Cleanup(func() { uploadDiskFree = oldFree })

	under := func(parent, target string) bool {
		p := strings.ToLower(filepath.Clean(parent))
		c := strings.ToLower(filepath.Clean(target))
		return c == p || strings.HasPrefix(c, p+string(filepath.Separator))
	}
	// 先判更具体的缓存目录，再判临时目录（测试环境里缓存目录常建在系统临时目录下）。
	uploadDiskFree = func(p string) (int64, error) {
		if under(app.CacheDir, p) {
			return 1 << 40, nil
		}
		if under(os.TempDir(), p) {
			return 1, nil
		}
		return 1, nil
	}

	// 1. 小于 64 MiB 的上传不碰临时盘余量检查，应当成功
	smallReq := multipartRequest(t, "/api/transfer/upload", "pack.mdpack", mdpackBytes(true))
	smallReq.Host = "127.0.0.1"
	smallReq.RemoteAddr = "127.0.0.1:52000"
	smallRes := httptest.NewRecorder()
	e.h.ServeHTTP(smallRes, smallReq)
	if smallRes.Code != 200 {
		t.Fatalf("小文件上传不应被临时盘余量拦截：HTTP %d %s", smallRes.Code, smallRes.Body.String())
	}

	// 2. 大于 multipartMemory 的上传会被临时盘余量拦截，报 507 并写明「临时」和盘符
	oldMem := multipartMemory
	multipartMemory = 16
	t.Cleanup(func() { multipartMemory = oldMem })

	bigReq := multipartRequest(t, "/api/transfer/upload", "pack.mdpack", mdpackBytes(true))
	bigReq.Host = "127.0.0.1"
	bigReq.RemoteAddr = "127.0.0.1:52000"
	bigRes := httptest.NewRecorder()
	e.h.ServeHTTP(bigRes, bigReq)
	if bigRes.Code != 507 {
		t.Fatalf("超过 multipartMemory 的上传应返回 507：HTTP %d %s", bigRes.Code, bigRes.Body.String())
	}
	msg := bigRes.Body.String()
	if !strings.Contains(msg, "临时") {
		t.Fatalf("报错应写明是临时文件夹所在磁盘：%s", msg)
	}
	if vol := filepath.VolumeName(os.TempDir()); vol != "" && !strings.Contains(msg, vol) {
		t.Fatalf("报错应带上盘符 %q：%s", vol, msg)
	}

	// 3. 恢复默认 64 MiB 后，长度未知（ContentLength == -1）的上传也仍按临时盘检查
	multipartMemory = oldMem
	unkReq := multipartRequest(t, "/api/transfer/upload", "pack.mdpack", mdpackBytes(true))
	rawBody := new(bytes.Buffer)
	_, _ = rawBody.ReadFrom(unkReq.Body)
	unkReq2 := httptest.NewRequest("POST", "/api/transfer/upload", struct{ *bytes.Buffer }{rawBody})
	unkReq2.Header = unkReq.Header.Clone()
	unkReq2.Host = "127.0.0.1"
	unkReq2.RemoteAddr = "127.0.0.1:52000"
	unkReq2.ContentLength = -1
	unkRes := httptest.NewRecorder()
	e.h.ServeHTTP(unkRes, unkReq2)
	if unkRes.Code != 507 || !strings.Contains(unkRes.Body.String(), "临时") {
		t.Fatalf("长度未知的上传应按临时盘检查报 507 并写明「临时」：HTTP %d %s", unkRes.Code, unkRes.Body.String())
	}
}
