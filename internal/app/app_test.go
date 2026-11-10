package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tempBase 把程序目录与内存配置换成临时的一份，测试结束后还原。
func tempBase(t *testing.T) {
	t.Helper()
	oldBase, oldServers := Base, ServersDir
	cfgMu.Lock()
	oldCfg := cfg
	cfgMu.Unlock()
	Base = t.TempDir()
	ServersDir = filepath.Join(Base, "servers")
	restart()
	t.Cleanup(func() {
		Base, ServersDir = oldBase, oldServers
		cfgMu.Lock()
		cfg = oldCfg
		cfgMu.Unlock()
	})
}

// restart 模拟重启：丢弃内存配置，按 config.json 重新加载。
func restart() {
	cfgMu.Lock()
	cfg = Config{Source: "auto", UpdateRepo: DefaultUpdateRepo}
	cfgMu.Unlock()
	loadConfig()
}

func setServersDir(t *testing.T, dir string) {
	t.Helper()
	if _, err := UpdateConfig(func(c *Config) error { c.ServersDir = dir; return nil }); err != nil {
		t.Fatal(err)
	}
}

// 回归：配置根 D 下的实例（含旧版本建的、从未进 Roots 的），改目录到 E 或恢复默认后重启仍要被扫描。
func TestServersDirChangeKeepsOldRoot(t *testing.T) {
	for _, tc := range []struct{ name, next string }{{"changeDir", "E"}, {"resetDefault", ""}} { // 改目录 / 恢复默认
		t.Run(tc.name, func(t *testing.T) {
			tempBase(t)
			d := filepath.Join(Base, "D", "MC")
			setServersDir(t, d)
			next := ""
			if tc.next != "" {
				next = filepath.Join(Base, tc.next, "MC")
			}
			setServersDir(t, next)
			restart()
			if roots := InstanceRoots(); !slices.Contains(roots, d) {
				t.Fatalf("重启后不再扫描旧根 %s：%v", d, roots)
			}
		})
	}
}

// 回归：RememberRoot 曾跳过当前配置根；现在只跳过内置根，并按 filepath.Clean 去重。
func TestRememberRoot(t *testing.T) {
	tempBase(t)
	d := filepath.Join(Base, "D", "MC")
	setServersDir(t, d)
	RememberRoot(d)
	RememberRoot(d + string(filepath.Separator))
	RememberRoot(ServersDir)
	restart()
	if got := GetConfig().Roots; !slices.Equal(got, []string{d}) {
		t.Fatalf("Roots = %v，应为 [%s]", got, d)
	}
}

// 单独登记的服务器文件夹（接管进来的）：去重不看大小写和末尾的分隔符；重启后还在；去掉以后不再有；
// 不会连带把它的上一层记成扫描根。
func TestRememberInstanceDir(t *testing.T) {
	tempBase(t)
	a, b := filepath.Join(Base, "old", "A"), filepath.Join(Base, "old", "B")
	RememberInstanceDir(a)
	RememberInstanceDir(a + string(filepath.Separator))
	RememberInstanceDir(strings.ToUpper(a))
	RememberInstanceDir(b)
	RememberInstanceDir("")
	restart()
	if got := InstanceDirs(); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("InstanceDirs = %v，应为 [%s %s]", got, a, b)
	}
	if roots := InstanceRoots(); slices.Contains(roots, filepath.Join(Base, "old")) {
		t.Fatalf("上一层不该成为扫描根：%v", roots)
	}

	held := GetConfig().InstanceDirs // 别处早先拿到的一份配置
	// 去掉再加，让 cfg 里的切片后面留出空位：这时再登记不能写进别处手里那块数组
	c := filepath.Join(Base, "old", "C")
	RememberInstanceDir(c)
	ForgetInstanceDir(c)
	spare := GetConfig().InstanceDirs
	RememberInstanceDir(c)
	if got := spare[:cap(spare)]; len(got) > len(spare) && got[len(spare)] == c {
		t.Errorf("登记时写进了别处手里那份配置的底层数组：%v", got)
	}
	ForgetInstanceDir(c)
	ForgetInstanceDir(filepath.Join(Base, "old", "没登记过的"))
	ForgetInstanceDir(strings.ToUpper(a) + string(filepath.Separator))
	if !slices.Equal(held, []string{a, b}) {
		t.Errorf("去掉登记时改到了别处手里的那份配置：%v", held)
	}
	restart()
	if got := InstanceDirs(); !slices.Equal(got, []string{b}) {
		t.Fatalf("去掉 A 之后 InstanceDirs = %v，应为 [%s]", got, b)
	}
}

func TestParseHexColor(t *testing.T) {
	for in, want := range map[string]string{"#202020": "#202020", "#EAE5D7": "#eae5d7", "#0e1210": "#0e1210"} {
		if got, ok := ParseHexColor(in); !ok || got != want {
			t.Errorf("ParseHexColor(%q) = %q, %v；应为 %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "#fff", "202020", "#2020200", "#20202g", "red", "rgb(1,2,3)", " #202020", "#202020 ", "＃202020"} {
		if got, ok := ParseHexColor(bad); ok {
			t.Errorf("ParseHexColor(%q) 应拒绝，却得到 %q", bad, got)
		}
	}
}
