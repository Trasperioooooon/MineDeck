package inst

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
)

// run.bat 是给用户手动双击的。服务器名字里的换行、JVM 参数里自带的双引号，以前能让 cmd 多执行一行命令
// （v3.1.0 正式版前检查 B-7）。这里真让 cmd 跑一遍生成的 run.bat：Java 路径指向不存在的文件，
// 不会起任何程序；只看输出里有没有冒出被注入的 echo。
func TestRunBatNoInjection(t *testing.T) {
	dir := t.TempDir()
	i := &Instance{Settings: Settings{
		// 绕过 validateName 直接写：模拟以前存下的、或者接管来的 instance.json 里的名字
		Name: "demo\r\necho NAME_INJECTED\r\nrem x", Core: mcsrc.CorePaper, MC: "1.20.1",
		JavaPath: filepath.Join(dir, "no-java", "java.exe"), XmxMB: 1024, XmsMB: 512,
		LaunchTarget: "jar:paper.jar",
		ExtraJVM: []string{
			`-Dprobe=x" & echo QUOTE_INJECTED & rem "`,
			"-Dline=a\r\necho LINE_INJECTED",
			"-Dpct=%PATH%",
		},
	}, Dir: dir}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	bat, err := os.ReadFile(filepath.Join(dir, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(bat), "\r\n"); n != 6 {
		t.Errorf("run.bat 应正好 6 行，实际 %d 行：\n%s", n, bat)
	}
	cmd := exec.Command("cmd", "/c", filepath.Join(dir, "run.bat"))
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput() // 找不到 java，cmd 会报错；pause 读到空输入就结束
	for _, mark := range []string{"NAME_INJECTED", "QUOTE_INJECTED", "LINE_INJECTED"} {
		if strings.Contains(string(out), mark) {
			t.Errorf("cmd 执行了注入的 %s：\n%s\nrun.bat：\n%s", mark, out, bat)
		}
	}
}

// 新建、导入、接管时的名字不许带控制字符。
func TestValidateNameRejectsControl(t *testing.T) {
	for _, n := range []string{"a\nb", "a\rb", "a\tb", "a\x00b", "a\x1bb"} {
		if validateName(n) == nil {
			t.Errorf("%q 应被拒绝", n)
		}
	}
	if err := validateName("生存服 2"); err != nil {
		t.Errorf("正常名字被拒：%v", err)
	}
}

// 保存设置时，JVM 参数里不许有控制字符和双引号；一行一个、带空格的照常。
func TestValidateJVMArgs(t *testing.T) {
	for _, a := range []string{`-Dx="a"`, "-Dx=a\nb", "-Dx=a\rb", "-Dx=\x00"} {
		if validateJVMArgs([]string{"-XX:+UseG1GC", a}) == nil {
			t.Errorf("%q 应被拒绝", a)
		}
	}
	if err := validateJVMArgs([]string{"-XX:+UseG1GC", "-Dpath=C:\\My Server\\x", "-Dpct=%TEMP%"}); err != nil {
		t.Errorf("正常参数被拒：%v", err)
	}
}
