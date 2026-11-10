package inst

import (
	"os"
	"path/filepath"
	"testing"
)

// 2026-11-10 最后一轮复审查出来的：MineDeck 读 server.properties 的办法和 Minecraft 服务端（java.util.Properties）
// 不一样，拿 MineDeck 读到的值做「世界目录不能指到外面」的检查，换个写法就骗过去了。
// 这里的用例守的是：安全判断用的是服务端实际会读到的值。

// TestJavaProps 照 java.util.Properties.load 的规矩读。
func TestJavaProps(t *testing.T) {
	for _, c := range []struct {
		name, text, key, want string
	}{
		{"普通的一行", "level-name=world\n", "level-name", "world"},
		{"同一个键写两行：后面的盖掉前面的", "level-name=world\nlevel-name=../other\n", "level-name", "../other"},
		{"冒号当分隔", "level-name:../x\n", "level-name", "../x"},
		{"空白当分隔", "level-name ../x\n", "level-name", "../x"},
		{"空白后面再跟等号", "level-name = ../x\n", "level-name", "../x"},
		{"值前面的空白不算，后面的算", "level-name=   w  \n", "level-name", "w  "},
		{"键里的转义", "level\\-name=../x\n", "level-name", "../x"},
		{"值里的转义", "level-name=.\\.\\/x\n", "level-name", "../x"},
		{"Unicode 转义", "level-name=\\u002e\\u002e/x\n", "level-name", "../x"},
		{"行尾反斜杠接下一行，下一行开头的空白不算", "level-name=w\\\n   /../../x\n", "level-name", "w/../../x"},
		{"两个反斜杠是一个真的反斜杠，不续行", "level-name=w\\\\\nother=1\n", "level-name", "w\\"},
		{"注释和空行跳过", "# level-name=../x\n! level-name=../y\n\nlevel-name=ok\n", "level-name", "ok"},
		{"行首的空白不算", "   \tlevel-name=ok\n", "level-name", "ok"},
		{"CRLF 和单独的 CR 都算换行", "a=1\r\nlevel-name=one\rlevel-name=two\r\n", "level-name", "two"},
		{"转义的等号属于键", "a\\=b=c\n", "a=b", "c"},
		{"\\t \\n 这些是控制字符", "k=a\\tb\\nc\n", "k", "a\tb\nc"},
		{"文件开头的 BOM 会粘在第一个键上（服务端也读不到这一项）", string(rune(0xFEFF)) + "level-name=x\nserver-port=1\n", "level-name", ""},
		{"服务端存盘时给感叹号加的反斜杠", "level-name=world\\!\n", "level-name", "world!"},
		{"没写", "server-port=25565\n", "level-name", ""},
	} {
		if got := JavaProps([]byte(c.text))[c.key]; got != c.want {
			t.Errorf("%s：%q 读成了 %q，应为 %q", c.name, c.key, got, c.want)
		}
	}
}

// TestCheckLevelNameJavaView 这些写法 MineDeck 自己读出来都是「没问题」（world、或者没写），
// 服务端读到的却是指到外面的路径：都要拦下来。
func TestCheckLevelNameJavaView(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{
		"同一项写两行":       "level-name=world\nlevel-name=../other-server/world\n",
		"先越界后正常也有歧义":   "level-name=../other-server\nlevel-name=world\n",
		"冒号分隔":         "level-name:../x\n",
		"空白分隔":         "level-name ../x\n",
		"键里带转义":        "level\\-name=../x\n",
		"值里带转义":        "level-name=.\\.\\/x\n",
		"续行":           "level-name=w\\\n/../../x\n",
		"CR 换行藏着第二行":   "level-name=world\rlevel-name=../x\n",
		"全是点的名字":       "level-name=...\n",
		"点和空格":         "level-name=. .\n",
		"直接写越界的（原来就拦）": "level-name=../other-server\n",
	} {
		write(text)
		if got, ok := CheckLevelName(dir); ok {
			t.Errorf("%s：应该拦下来，却当成没问题（读到 %q）", name, got)
		}
		if got := LevelName(dir); got != "world" && !SafeLevelName(got) {
			t.Errorf("%s：备份和还原用的世界目录名不该是 %q", name, got)
		}
	}
	// 正常的写法照常：普通名字、带子目录、中文（转义写法和直接写）、带感叹号（服务端存盘时会加反斜杠）、没写
	for name, c := range map[string][2]string{
		"普通":      {"server-port=25565\nlevel-name=world\n", "world"},
		"子目录":     {"level-name=worlds/main\n", "worlds/main"},
		"中文直接写":   {"level-name=我的世界\n", "我的世界"},
		"中文转义":    {"level-name=\\u6211\\u7684\\u4e16\\u754c\n", "我的世界"},
		"带感叹号":    {"level-name=world\\!\n", "world!"},
		"同一项两行相同": {"level-name=world\nlevel-name=world\n", "world"},
	} {
		write(c[0])
		if got, ok := CheckLevelName(dir); !ok || got != c[1] {
			t.Errorf("%s：正常的写法不该被拦（%q %v）", name, got, ok)
		}
		if got := LevelName(dir); got != c[1] {
			t.Errorf("%s：世界目录名应是 %q，实际 %q", name, c[1], got)
		}
	}
	write("server-port=25565\n")
	if _, ok := CheckLevelName(dir); !ok || LevelName(dir) != "world" {
		t.Error("没写 level-name 不算问题，世界目录是 world")
	}
}

// TestSafeLevelNameDots 全是点和空格的名字，Windows 会解成所在的目录。
func TestSafeLevelNameDots(t *testing.T) {
	for _, bad := range []string{"...", "....", ". .", "... ", "..  .", "world/...", ".../world"} {
		if SafeLevelName(bad) {
			t.Errorf("%q 不该被当成世界目录的名字", bad)
		}
	}
	for _, ok := range []string{"world.", "world.backup", ".hidden", "a.b/c.d"} {
		if !SafeLevelName(ok) {
			t.Errorf("%q 是正常的名字", ok)
		}
	}
}

// TestForceProp 改完以后服务端读到的必须是新值：同一项有好几行、用了别的写法时，只改第一行等于没改。
func TestForceProp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.properties")
	for name, text := range map[string]string{
		"普通":         "enable-rcon=true\nserver-port=1\n",
		"同一项两行":      "enable-rcon=false\nserver-port=1\nenable-rcon=true\n",
		"冒号写法":       "enable-rcon:true\n",
		"末尾没有换行":     "a=1\nenable-rcon=true",
		"最后一行以反斜杠结尾": "enable-rcon=true\nmotd=hi\\\n",
		"原来没有这一项":    "server-port=1\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ForceProp(path, "enable-rcon", "false"); err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if got := javaPropsOf(path)["enable-rcon"]; got != "false" {
			b, _ := os.ReadFile(path)
			t.Errorf("%s：服务端读到的还是 %q\n%s", name, got, b)
		}
	}
	// 文件不存在时新建
	fresh := filepath.Join(t.TempDir(), "server.properties")
	if err := ForceProp(fresh, "server-port", "25570"); err != nil {
		t.Fatal(err)
	}
	if got := javaPropsOf(fresh)["server-port"]; got != "25570" {
		t.Errorf("新建的文件里应有这一项：%q", got)
	}
}
