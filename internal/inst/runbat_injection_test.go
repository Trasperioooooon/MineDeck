package inst

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
)

// 参数怎么写进 run.bat：都加引号；% 写两个；自带的双引号写两个；挨着双引号的反斜杠、结尾的反斜杠写两个
// （java.exe 按 VC 运行库的规矩拆参数，反斜杠挨着双引号才算转义）；别处的反斜杠原样。
func TestBatQuoteArgForJava(t *testing.T) {
	for in, want := range map[string]string{
		`C:\My Server\`: `"C:\My Server\\"`,
		`C:\a\\`:        `"C:\a\\\\"`,
		`a\b`:           `"a\b"`,
		`say "hi"`:      `"say ""hi"""`,
		`a\"b`:          `"a\\""b"`,
		"%PATH%":        `"%%PATH%%"`,
		`a\%b`:          `"a\%%b"`,
		"a\nb":          `"a b"`,
		"":              `""`,
	} {
		if got := batQuoteArg(in); got != want {
			t.Errorf("batQuoteArg(%q) = %s，应为 %s", in, got, want)
		}
	}
}

// 真让 cmd 跑 run.bat、起真的 java，看 JVM 收到的系统属性和 JVM 参数里写的是否一字不差：
// cmd 那一层不能多执行命令，java.exe 那一层不能把参数拆错、串位。找不到 java 就跳过（GitHub 的 Windows 机器上有）。
func TestRunBatArgsReachJava(t *testing.T) {
	java := ""
	if h := os.Getenv("JAVA_HOME"); h != "" {
		if p := filepath.Join(h, "bin", "java.exe"); func() bool { _, err := os.Stat(p); return err == nil }() {
			java = p
		}
	}
	if java == "" {
		p, err := exec.LookPath("java")
		if err != nil {
			t.Skip("找不到 java，跳过")
		}
		java = p
	}
	want := map[string]string{
		"md.slash":      `C:\My Server\`,
		"md.slash2":     `C:\a\\`,
		"md.mid":        `a\b`,
		"md.space":      "a b",
		"md.pct":        "%PATH%",
		"md.amp":        "x & echo AMP_INJECTED",
		"md.quote":      `say "hi"`,
		"md.slashquote": `a\"b`,
	}
	var extra []string
	for k, v := range want {
		extra = append(extra, "-D"+k+"="+v)
	}
	sort.Strings(extra)
	extra = append(extra, "-XshowSettings:properties", "-version") // 打出系统属性和版本就退出
	dir := t.TempDir()
	// 不给启动目标，命令行里就没有 -jar：java 会先去读 -jar 指的那个文件，没有就直接报错，轮不到 -version
	i := &Instance{Settings: Settings{Name: "demo", Core: mcsrc.CorePaper, MC: "1.20.1", JavaPath: java,
		XmxMB: 256, XmsMB: 128, ExtraJVM: extra}, Dir: dir}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd", "/c", filepath.Join(dir, "run.bat"))
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput() // pause 读到空输入就结束
	got := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if k, v, ok := strings.Cut(strings.TrimLeft(line, " "), " = "); ok && strings.HasPrefix(k, "md.") {
			got[k] = v
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s：java 收到 %q，应为 %q", k, got[k], v)
		}
	}
	if t.Failed() {
		bat, _ := os.ReadFile(filepath.Join(dir, "run.bat"))
		t.Logf("run.bat：\n%s\n输出：\n%s", bat, out)
	}
}

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
