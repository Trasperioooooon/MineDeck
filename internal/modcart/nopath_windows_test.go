package modcart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 把本地文件拷进服务器目录时，暂存的那份读不了（被别的程序独占着）：报错说的是哪个文件、什么原因，不带本机的完整路径。
// 这句话会进任务日志和提醒，局域网里的人、AI 助手都看得到；以前这里把系统给的原文原样返回，里面是完整路径。
func TestInstallLocalErrorHasNoLocalPath(t *testing.T) {
	root := t.TempDir()
	s := New(root, newFakeMR(t).srv.URL)
	addLocal(t, s, "生存服", "custom-1.0.jar", fabricJar(t, "custom", "Custom", nil))
	lf := s.Locals("生存服")[0]
	mods := filepath.Join(t.TempDir(), "mods")
	unlock := lockExclusive(t, s.stagedPath("生存服", lf.SHA1))
	_, err := s.InstallLocal(lf, mods)
	unlock()
	if err == nil || codeOf(err) != 500 || !strings.Contains(err.Error(), "「custom-1.0.jar」读不了") {
		t.Fatalf("读不了应报错、说清是哪个文件：%v", err)
	}
	for _, leak := range []string{root, mods, filepath.Dir(mods), os.TempDir(), `:\`, "local"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("报错里不该带本机路径（%q）：%v", leak, err)
		}
	}
	if ents, _ := os.ReadDir(mods); len(ents) != 0 {
		t.Errorf("没装成不该留下文件：%v", ents)
	}
	// 放开以后照常装上
	if st, err := s.InstallLocal(lf, mods); err != nil || st != LocalCopied {
		t.Fatalf("放开以后应装得上：%s %v", st, err)
	}
}
