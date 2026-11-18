package worldimport

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 系统错误里带着本机的完整路径；检测的结果局域网里的人也看得到，报错只留原因。原因还包在里面，errors.Is 照旧认得出。
func TestBarePathErr(t *testing.T) {
	inner := fs.ErrPermission
	p := filepath.Join(t.TempDir(), "cache", "uploads", "1.zip")
	for name, err := range map[string]error{
		"PathError":       &fs.PathError{Op: "open", Path: p, Err: inner},
		"包了一层的 PathError": errors.Join(errors.New("外层"), &fs.PathError{Op: "remove", Path: p, Err: inner}),
		"LinkError":       &os.LinkError{Op: "rename", Old: p, New: p + ".new", Err: inner},
	} {
		got := barePathErr("上传的文件读不了", err)
		if got == nil || strings.Contains(got.Error(), filepath.Dir(p)) || !strings.HasPrefix(got.Error(), "上传的文件读不了：") || !errors.Is(got, inner) {
			t.Errorf("%s：%v", name, got)
		}
	}
	if barePathErr("x", nil) != nil {
		t.Error("nil 还是 nil")
	}
	if got := barePathErr("x", context.Canceled); got != context.Canceled {
		t.Errorf("不带路径的错误原样返回：%v", got)
	}
}

// 上传的文件在检测开始之前不见了（被清理、被杀毒软件删了）：报错不带 MineDeck 缓存文件夹的路径。
func TestScanUploadMissingFileErrorHasNoLocalPath(t *testing.T) {
	e := newEnv(t, "")
	if err := os.MkdirAll(UploadDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := e.s.ScanUpload(me, filepath.Join(UploadDir(), "不见了.zip"), "map.zip")
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("文件不在应报错：%v", err)
	}
	for _, leak := range []string{UploadDir(), app.CacheDir, "不见了.zip", `:\`} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("报错里不该带本机路径（%q）：%v", leak, err)
		}
	}
	if len(e.s.scanSlots) != 0 {
		t.Error("检测的名额没放掉")
	}
}

// 选的是单个 NBT 文件、却读不了：同样只留原因。
func TestScanSingleNBTUnreadableErrorHasNoLocalPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "house.schem")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock := lockFileExclusive(t, p) // 被别的程序独占着：打不开
	r := &scanResult{}
	_, err := r.singleNBT(p, "house.schem", ".schem")
	unlock()
	if err == nil || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), `:\`) || !strings.HasPrefix(err.Error(), "这个文件读不了：") {
		t.Fatalf("读不了应报错，报错里不该带本机路径：%v", err)
	}
}
