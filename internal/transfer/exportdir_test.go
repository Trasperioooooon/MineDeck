package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 「新建并导出」（可用性走查 R20）：只建最后一级，上一级必须已经在；已经在的文件夹原样用；是文件、名字在 Windows 上
// 落不下来、落在这台服务器的文件夹里面、填的不是完整路径，都不建。
func TestMakeExportDirOnlyLastLevel(t *testing.T) {
	newEnv(t)
	instDir := fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	base := t.TempDir()
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	wantErr := func(dest, want string) {
		t.Helper()
		dir, made, err := MakeExportDir(m, "A 的服", dest)
		if err == nil || !strings.Contains(err.Error(), want) || made || dir != "" {
			t.Errorf("%q 应报「%s」：dir=%q made=%v err=%v", dest, want, dir, made, err)
		}
	}

	wantErr("交接包", "完整的文件夹路径")
	wantErr("  ", "完整的文件夹路径")
	// 上一级不在：一级都不建
	deep := filepath.Join(base, "还没有", "交接包")
	wantErr(deep, "上一级文件夹")
	if exists(filepath.Join(base, "还没有")) {
		t.Error("上一级不在时不该替人建出一串文件夹")
	}
	// 落在这台服务器的文件夹里：不建（下一次导出会把上次的包打进去）
	wantErr(filepath.Join(instDir, "exports"), "不能放在这台服务器的文件夹里面")
	if exists(filepath.Join(instDir, "exports")) {
		t.Error("服务器文件夹里不该建出导出文件夹")
	}
	// 名字在 Windows 上落不下来
	wantErr(filepath.Join(base, "CON"), "不能用作文件夹名")
	wantErr(filepath.Join(base, "交接包."), "不能用作文件夹名")
	// 是文件，不是文件夹
	f := filepath.Join(base, "是个文件")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr(f, "已经是一个文件")
	// 不认识的服务器
	if _, _, err := MakeExportDir(m, "没有这台", filepath.Join(base, "x")); err == nil {
		t.Error("不认识的服务器应报错")
	}

	// 正常：建出最后一级，前后空格不算
	want := filepath.Join(base, "新的交接包")
	dir, made, err := MakeExportDir(m, "A 的服", "  "+want+"  ")
	if err != nil || !made || dir != want {
		t.Fatalf("应新建 %q：dir=%q made=%v err=%v", want, dir, made, err)
	}
	if st, err := os.Stat(want); err != nil || !st.IsDir() {
		t.Fatalf("文件夹没建出来：%v", err)
	}
	// 已经在：原样用，不算新建（导出没成时网页层不会把它删掉）
	if dir, made, err := MakeExportDir(m, "A 的服", want); err != nil || made || dir != want {
		t.Errorf("已经在的文件夹应原样用：dir=%q made=%v err=%v", dir, made, err)
	}
}

// 导出时填的文件夹不在：报错的前半句仍是「导出文件夹不可用」，括号里写明是找不到（界面据此给「新建并导出」）；
// 是文件的照旧只说不可用
func TestExportSaysWhenDirMissing(t *testing.T) {
	newEnv(t)
	fakeInstance(t, "A 的服")
	m := manager(t, "A 的服")
	base := t.TempDir()
	_, err := ExportAsync(m, ExportReq{Inst: "A 的服", Dest: filepath.Join(base, "nope")})
	if err == nil || !strings.HasPrefix(err.Error(), ErrExportDirMissing+": ") || !strings.HasPrefix(err.Error(), "导出文件夹不可用") {
		t.Errorf("不在的文件夹：%v", err)
	}
	f := filepath.Join(base, "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportAsync(m, ExportReq{Inst: "A 的服", Dest: f}); err == nil || strings.Contains(err.Error(), "找不到") || !strings.HasPrefix(err.Error(), "导出文件夹不可用") {
		t.Errorf("是文件不是文件夹：%v", err)
	}
}
