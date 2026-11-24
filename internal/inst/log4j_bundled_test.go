package inst

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 这一组测「服务端 jar 自带的 log4j 修没修过」怎么认，以及各个入口、程序启动后的整理怎么按它办（v3.2.2 之后的三块问题，第一块）。

// writeJar 现造一个小 jar。
func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// log4jPom Maven 打包 log4j-core 时留下的版本记录（照真 jar 里的样子写）。
func log4jPom(ver string) string {
	return "#Created by Apache Maven 3.6.3\r\nversion=" + ver + "\r\ngroupId=org.apache.logging.log4j\r\nartifactId=log4j-core\r\n"
}

const craftMain = "Manifest-Version: 1.0\r\nMain-Class: org.bukkit.craftbukkit.Main\r\n\r\n"

// plainServerJar 在 dir 里放一个自己带着 log4j 的 server.jar（Spigot 那种）。ver 为空时不带版本记录（原版、老构建）。
func plainServerJar(t *testing.T, dir, ver string) {
	t.Helper()
	files := map[string]string{"META-INF/MANIFEST.MF": craftMain, "org/apache/logging/log4j/core/LoggerContext.class": "x"}
	if ver != "" {
		files[log4jCorePom] = log4jPom(ver)
	}
	writeJar(t, filepath.Join(dir, "server.jar"), files)
}

// paperclipServer 在 dir 里放一个 paperclip 的 server.jar 和它在 cache 里生成的服务端 jar（里面的 log4j 是 ver，空串是不带版本记录）。
// legacy 为 true 时用 2016–2019 年那种 patch.json。返回 cache 里那个 jar 的路径。
func paperclipServer(t *testing.T, dir, mc, ver string, legacy bool) string {
	t.Helper()
	inner := map[string]string{"META-INF/MANIFEST.MF": craftMain, "org/apache/logging/log4j/core/LoggerContext.class": "x"}
	if ver != "" {
		inner[log4jCorePom] = log4jPom(ver)
	}
	patched := filepath.Join(dir, "cache", "patched_"+mc+".jar")
	writeJar(t, patched, inner)
	b, err := os.ReadFile(patched)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	hash := strings.ToUpper(hex.EncodeToString(sum[:])) // paperclip 里记的是大写
	clip := map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Paperclip\r\n\r\n", "paperMC.patch": "补丁"}
	if legacy {
		clip["patch.json"] = fmt.Sprintf(`{"patch":"paperMC.patch","sourceUrl":"https://example.invalid/server.jar","originalHash":"AA","patchedHash":"%s","version":"%s"}`, hash, mc)
	} else {
		clip["patch.properties"] = "#Default Paperclip launch values.\r\n#Sat Dec 18 12:57:17 CST 2021\r\nversion=" + mc + "\r\npatchedHash=" + hash +
			"\r\nsourceUrl=https\\://example.invalid/server.jar\r\noriginalHash=AA\r\npatch=paperMC.patch\r\n"
	}
	writeJar(t, filepath.Join(dir, "server.jar"), clip)
	return patched
}

// 版本号怎么分：2.16.0 起、回补版 2.12.2 / 2.3.1 起算修过的；2.15.0、带后缀的不算；读不懂的判断不了。
func TestClassifyLog4jCore(t *testing.T) {
	for ver, want := range map[string]log4jBundle{
		"2.17.0": log4jBundleFixed, "2.17.1": log4jBundleFixed, "2.16.0": log4jBundleFixed, "2.25.3": log4jBundleFixed, "2.17": log4jBundleFixed,
		"3.0.0": log4jBundleFixed, "2.12.2": log4jBundleFixed, "2.12.4": log4jBundleFixed, "2.3.1": log4jBundleFixed, "2.3.2": log4jBundleFixed,
		" 2.17.0\r": log4jBundleFixed,
		"2.15.0":    log4jBundleOld, "2.14.1": log4jBundleOld, "2.8.1": log4jBundleOld, "2.0-beta9": log4jBundleOld, "2.12.1": log4jBundleOld,
		"2.3": log4jBundleOld, "2.3.0": log4jBundleOld, "2.13.3": log4jBundleOld, "2.11.2": log4jBundleOld, "2.0": log4jBundleOld,
		"2.17.1-SNAPSHOT": log4jBundleOld, "2.17.0-rc1": log4jBundleOld, "3.0.0-beta2": log4jBundleOld, "2.17.0.1": log4jBundleOld,
		"": log4jBundleUnknown, "2": log4jBundleUnknown, "2.": log4jBundleUnknown, "abc": log4jBundleUnknown, "x.17.0": log4jBundleUnknown,
		"1.2.17": log4jBundleUnknown, "${project.version}": log4jBundleUnknown, "2.x.1": log4jBundleUnknown, "2.17.0.0.1": log4jBundleUnknown,
	} {
		if got := classifyLog4jCore(ver); got != want {
			t.Errorf("classifyLog4jCore(%q) = %d，应为 %d", ver, got, want)
		}
	}
}

// 认 jar 里的 log4j：自己带着版本记录的直接看；paperclip 的看 cache 里生成的那个，还要和它记的哈希对得上；别的都判断不了。
func TestBundledLog4j(t *testing.T) {
	for _, c := range []struct {
		name   string
		target string
		setup  func(t *testing.T, dir string)
		want   log4jBundle
		ver    string
	}{
		{"Spigot 那种，修过的版本", "jar:server.jar", func(t *testing.T, dir string) { plainServerJar(t, dir, "2.17.0") }, log4jBundleFixed, "2.17.0"},
		{"Spigot 那种，没修过的版本", "jar:server.jar", func(t *testing.T, dir string) { plainServerJar(t, dir, "2.8.1") }, log4jBundleOld, "2.8.1"},
		{"1.8 自带的 beta9", "jar:server.jar", func(t *testing.T, dir string) { plainServerJar(t, dir, "2.0-beta9") }, log4jBundleOld, "2.0-beta9"},
		{"有 log4j 的类、没有版本记录（原版、老构建）", "jar:server.jar", func(t *testing.T, dir string) { plainServerJar(t, dir, "") }, log4jBundleUnknown, ""},
		{"jar 里根本没有 log4j（Forge 的启动 jar）", "jar:server.jar", func(t *testing.T, dir string) {
			writeJar(t, filepath.Join(dir, "server.jar"), map[string]string{"META-INF/MANIFEST.MF": craftMain})
		}, log4jBundleUnknown, ""},
		{"版本记录里没写版本", "jar:server.jar", func(t *testing.T, dir string) {
			writeJar(t, filepath.Join(dir, "server.jar"), map[string]string{log4jCorePom: "groupId=org.apache.logging.log4j\n"})
		}, log4jBundleUnknown, ""},
		{"版本记录读不懂", "jar:server.jar", func(t *testing.T, dir string) {
			writeJar(t, filepath.Join(dir, "server.jar"), map[string]string{log4jCorePom: "version=${project.version}\n"})
		}, log4jBundleUnknown, "${project.version}"},
		{"读不出来：不是 zip", "jar:server.jar", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "server.jar"), []byte("这不是一个 jar"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, log4jBundleUnknown, ""},
		{"读不出来：文件不在", "jar:server.jar", func(*testing.T, string) {}, log4jBundleUnknown, ""},
		{"读不出来：是个文件夹", "jar:server.jar", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "server.jar"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, log4jBundleUnknown, ""},
		{"用参数文件启动的不看", "args:libraries/x/win_args.txt", func(t *testing.T, dir string) { plainServerJar(t, dir, "2.17.0") }, log4jBundleUnknown, ""},
		{"启动目标是空的", "", func(t *testing.T, dir string) { plainServerJar(t, dir, "2.17.0") }, log4jBundleUnknown, ""},
		{"启动目标指到文件夹外面", "jar:../server.jar", func(t *testing.T, dir string) {
			plainServerJar(t, filepath.Dir(dir), "2.17.0")
			t.Cleanup(func() { _ = os.Remove(filepath.Join(filepath.Dir(dir), "server.jar")) })
		}, log4jBundleUnknown, ""},
		{"paperclip，生成的服务端是修过的", "jar:server.jar", func(t *testing.T, dir string) { paperclipServer(t, dir, "1.8.8", "2.17.0", false) }, log4jBundleFixed, "2.17.0"},
		{"paperclip，生成的服务端没修过", "jar:server.jar", func(t *testing.T, dir string) { paperclipServer(t, dir, "1.12.2", "2.8.1", true) }, log4jBundleOld, "2.8.1"},
		{"老 paperclip（patch.json），修过的", "jar:server.jar", func(t *testing.T, dir string) { paperclipServer(t, dir, "1.11.2", "2.17.0", true) }, log4jBundleFixed, "2.17.0"},
		{"2016 年的 paperclip：生成的叫 patched.jar", "jar:server.jar", func(t *testing.T, dir string) {
			p := paperclipServer(t, dir, "1.8.8", "2.17.0", true)
			if err := os.Rename(p, filepath.Join(dir, "cache", "patched.jar")); err != nil {
				t.Fatal(err)
			}
		}, log4jBundleFixed, "2.17.0"},
		{"paperclip，生成的服务端没有版本记录（2017 年的构建）", "jar:server.jar", func(t *testing.T, dir string) { paperclipServer(t, dir, "1.11.2", "", true) }, log4jBundleUnknown, ""},
		{"paperclip 还没启动过，cache 里什么都没有", "jar:server.jar", func(t *testing.T, dir string) {
			if err := os.Remove(paperclipServer(t, dir, "1.8.8", "2.17.0", false)); err != nil {
				t.Fatal(err)
			}
		}, log4jBundleUnknown, ""},
		{"paperclip，cache 里的是别的构建生成的（哈希对不上）", "jar:server.jar", func(t *testing.T, dir string) {
			p := paperclipServer(t, dir, "1.8.8", "2.8.1", false)                           // 现在的 server.jar 会生成没修过的
			writeJar(t, p, map[string]string{log4jCorePom: log4jPom("2.17.0"), "别的": "内容"}) // cache 里留着的却是另一个修过的
		}, log4jBundleUnknown, ""},
		{"paperclip，文件夹里有 paperclip.properties（能改掉补丁说明）", "jar:server.jar", func(t *testing.T, dir string) {
			paperclipServer(t, dir, "1.8.8", "2.17.0", false)
			if err := os.WriteFile(filepath.Join(dir, "paperclip.properties"), []byte("patch=other.patch\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, log4jBundleUnknown, ""},
		{"paperclip，版本那一项带着路径", "jar:server.jar", func(t *testing.T, dir string) {
			p := paperclipServer(t, dir, "1.8.8", "2.17.0", false)
			b, _ := os.ReadFile(p)
			sum := sha256.Sum256(b)
			writeJar(t, filepath.Join(dir, "server.jar"), map[string]string{"patch.properties": "version=../../x\npatchedHash=" + hex.EncodeToString(sum[:]) + "\n"})
			if err := os.Rename(p, filepath.Join(dir, "cache", "patched_../../x.jar")); err != nil { // 不查版本那一项的话，拼出来的就是这个文件
				t.Fatal(err)
			}
		}, log4jBundleUnknown, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "srv")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			c.setup(t, dir)
			got, ver := bundledLog4j(dir, c.target)
			if got != c.want || (c.want != log4jBundleUnknown && ver != c.ver) {
				t.Fatalf("bundledLog4j = %d %q，应为 %d %q", got, ver, c.want, c.ver)
			}
			if fixed := log4jSelfFixed(dir, c.target); fixed != (c.want == log4jBundleFixed) {
				t.Fatalf("log4jSelfFixed = %v", fixed)
			}
		})
	}
}

// bundleKinds 三种服务端：自带的 log4j 修过了、没修过、判断不了。
var bundleKinds = []struct {
	name  string
	fixed bool
	setup func(t *testing.T, dir, mc string)
}{
	{"修过的 paperclip", true, func(t *testing.T, dir, mc string) { paperclipServer(t, dir, mc, "2.17.0", false) }},
	{"修过的单个 jar", true, func(t *testing.T, dir, mc string) { plainServerJar(t, dir, "2.17.1") }},
	{"没修过的 paperclip", false, func(t *testing.T, dir, mc string) { paperclipServer(t, dir, mc, "2.8.1", true) }},
	{"判断不了", false, func(t *testing.T, dir, mc string) { plainServerJar(t, dir, "") }},
	{"paperclip 还没启动过", false, func(t *testing.T, dir, mc string) {
		if err := os.Remove(paperclipServer(t, dir, mc, "2.17.0", false)); err != nil {
			t.Fatal(err)
		}
	}},
}

// 各个入口加什么参数：导入现成服务端（applyImportLog4j）、新建和接管（applyLog4jMitigation）、交接包（ApplyLog4jMitigation）、
// 保存参数时强制保留的（log4jFlags）。自带的 log4j 修过了的不套 Mojang 的配置、也不下载；没修过的、判断不了的照旧。
func TestLog4jEntryPointsFollowBundled(t *testing.T) {
	for _, era := range []struct{ mc, xml string }{{"1.8.9", log4jXML17}, {"1.11.2", log4jXML17}, {"1.12.2", log4jXML}, {"1.16.5", log4jXML}} {
		for _, k := range bundleKinds {
			want, wantFetch := []string{log4jNoLookups, log4jConfigKey + era.xml}, []string{era.xml}
			if k.fixed {
				want, wantFetch = []string{log4jNoLookups}, nil
			}
			newInst := func(t *testing.T) *Instance {
				dir := t.TempDir()
				k.setup(t, dir, era.mc)
				return &Instance{Settings: Settings{Name: "t", Core: mcsrc.CorePaper, MC: era.mc, LaunchTarget: "jar:server.jar"}, Dir: dir}
			}
			check := func(t *testing.T, got, fetched []string, log string) {
				t.Helper()
				if !slices.Equal(got, want) || !slices.Equal(fetched, wantFetch) {
					t.Fatalf("ExtraJVM=%q 下载了 %q，应为 %q、%q", got, fetched, want, wantFetch)
				}
				if k.fixed != strings.Contains(log, "已经修过 log4shell") {
					t.Fatalf("任务日志：%s", log)
				}
			}
			name := era.mc + "/" + k.name
			t.Run(name+"/导入", func(t *testing.T) {
				fetched := stubLog4jFetch(t, nil)
				i := newInst(t)
				tk, _ := tasks.NewManager().New("导入", []string{"导入"})
				applyImportLog4j(context.Background(), tk, i)
				check(t, i.ExtraJVM, *fetched, strings.Join(tk.Snapshot().Log, "\n"))
			})
			t.Run(name+"/新建和接管", func(t *testing.T) {
				fetched := stubLog4jFetch(t, nil)
				i := newInst(t)
				tk, _ := tasks.NewManager().New("接管", []string{"接管"})
				if err := applyLog4jMitigation(offline(), tk, i); err != nil {
					t.Fatal(err)
				}
				check(t, i.ExtraJVM, *fetched, strings.Join(tk.Snapshot().Log, "\n"))
			})
			t.Run(name+"/交接包", func(t *testing.T) {
				fetched := stubLog4jFetch(t, nil)
				i := newInst(t)
				s := i.Settings
				s.ExtraJVM = []string{log4jConfigKey + "别人包里带来的.xml"} // 同名参数以这里的为准
				tk, _ := tasks.NewManager().New("交接", []string{"交接"})
				if err := ApplyLog4jMitigation(offline(), tk, &s, i.Dir); err != nil {
					t.Fatal(err)
				}
				got := s.ExtraJVM
				if k.fixed { // 没有要强加的同名参数，原来那条不归这里管
					got = slices.DeleteFunc(slices.Clone(got), func(a string) bool { return a == log4jConfigKey+"别人包里带来的.xml" })
				}
				check(t, got, *fetched, strings.Join(tk.Snapshot().Log, "\n"))
			})
			t.Run(name+"/保存参数", func(t *testing.T) {
				i := newInst(t)
				for _, f := range []string{log4jXML, log4jXML17} { // 两份官方配置文件都在：加不加只看该不该套
					officialLog4jXMLFile(t, i.Dir, f)
				}
				if got := log4jFlags(i); !slices.Equal(got, want) {
					t.Fatalf("log4jFlags = %q，应为 %q", got, want)
				}
			})
		}
	}
}

// 接管整条路走一遍（不联网）：修过的 Paper 1.8 不套配置、不下载，instance.json 和 run.bat 里都没有引用；没修过的照旧套 17-111。
func TestAdoptFollowsBundledLog4j(t *testing.T) {
	for _, c := range []struct {
		name  string
		ver   string
		fixed bool
	}{{"修过的", "2.17.0", true}, {"没修过的", "2.0-beta9", false}} {
		t.Run(c.name, func(t *testing.T) {
			fetched := stubLog4jFetch(t, nil)
			m, user := adoptEnv(t)
			dir := filepath.Join(user, "paper18")
			paperclipServer(t, dir, "1.8.9", c.ver, false)
			writeTree(t, dir, map[string]string{"eula.txt": "eula=true\n", "server.properties": "server-port=25590\n"})
			if log, err := adopt(t, m, AdoptReq{Dir: dir, Name: "老 Paper", Core: mcsrc.CorePaper, MC: "1.8.9", LaunchTarget: "jar:server.jar", JavaMajor: 8, JavaPath: fakeJava(t)}); err != nil {
				t.Fatalf("%v\n%s", err, log)
			}
			i, _ := m.Get("老 Paper")
			bat := mustRead(t, filepath.Join(dir, "run.bat"))
			saved, err := readSettings(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.fixed {
				if !slices.Equal(i.ExtraJVM, []string{log4jNoLookups}) || len(*fetched) != 0 || strings.Contains(bat, "log4j.configurationFile") || !slices.Equal(saved.ExtraJVM, i.ExtraJVM) {
					t.Fatalf("修过的不该套配置：ExtraJVM=%q 下载了 %q\n%s", i.ExtraJVM, *fetched, bat)
				}
				if _, err := os.Stat(filepath.Join(dir, log4jXML17)); !os.IsNotExist(err) {
					t.Fatalf("不该下载配置文件：%v", err)
				}
				return
			}
			if !slices.Equal(i.ExtraJVM, []string{log4jNoLookups, log4jConfig17}) || !slices.Equal(*fetched, []string{log4jXML17}) || !strings.Contains(bat, log4jXML17) {
				t.Fatalf("没修过的照旧套 17-111：ExtraJVM=%q 下载了 %q\n%s", i.ExtraJVM, *fetched, bat)
			}
		})
	}
}

// 保存 JVM 参数（整条路）：自带的 log4j 修过了的，清空、套用预设以后不会把 Mojang 配置的引用补回来；没修过的照旧补回。
func TestUpdateSettingsFollowsBundledLog4j(t *testing.T) {
	preset := []string{"-XX:+UseG1GC"}
	for _, c := range []struct {
		name  string
		ver   string
		start []string
		want  []string
	}{
		{"修过的，清空以后只剩 noLookups", "2.17.0", []string{log4jNoLookups}, append(slices.Clone(preset), log4jNoLookups)},
		{"没修过的，引用补回来", "2.8.1", []string{log4jNoLookups}, append(slices.Clone(preset), log4jNoLookups, log4jConfig)},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			paperclipServer(t, dir, "1.16.5", c.ver, false)
			officialLog4jXMLFile(t, dir, log4jXML)
			i := &Instance{Settings: Settings{Name: "存参数", Core: mcsrc.CorePaper, MC: "1.16.5", JavaPath: `jdk\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
				LaunchTarget: "jar:server.jar", ExtraJVM: c.start}, Dir: dir, Console: newConsole()}
			m := &Manager{insts: map[string]*Instance{i.Name: i}, creating: map[string]bool{}}
			if err := m.UpdateSettings(i.Name, 0, 0, "", &preset); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(i.ExtraJVM, c.want) {
				t.Fatalf("ExtraJVM = %q，应为 %q", i.ExtraJVM, c.want)
			}
		})
	}
}

// migrateInst 一台摆好的旧服：instance.json 和 run.bat 都按 extra 写好。
func migrateInst(t *testing.T, m *Manager, name string, core mcsrc.Core, mc string, extra []string) *Instance {
	t.Helper()
	i := &Instance{Settings: Settings{Name: name, Core: core, MC: mc, JavaPath: `jdk\bin\java.exe`, XmxMB: 1024, XmsMB: 512,
		LaunchTarget: "jar:server.jar", ExtraJVM: slices.Clone(extra)}, Dir: t.TempDir(), Console: newConsole()}
	if err := m.saveInstance(i); err != nil {
		t.Fatal(err)
	}
	if err := writeRunBat(i); err != nil {
		t.Fatal(err)
	}
	m.insts[name] = i
	return i
}

func newMigrateManager() *Manager {
	return &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}}
}

// 问题 A 的整理：服务端自带的 log4j 修过了、却还引用着 Mojang 配置的，把引用摘掉。别的参数原样、位置不变；
// instance.json、run.bat 跟着改；不下载；配置文件留着不删。
func TestMigrateLog4jDropsWhenSelfFixed(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	paper18 := migrateInst(t, m, "Paper 1.8", mcsrc.CorePaper, "1.8.9", []string{"-Xss1M", log4jNoLookups, log4jConfig17, "-XX:+UseG1GC"})
	paperclipServer(t, paper18.Dir, "1.8.9", "2.17.0", false)
	oldRef := migrateInst(t, m, "还引用着旧的", mcsrc.CorePaper, "1.11.2", []string{log4jNoLookups, log4jConfig, log4jConfig17})
	paperclipServer(t, oldRef.Dir, "1.11.2", "2.17.0", true)
	spigot116 := migrateInst(t, m, "修过的 1.16.5", mcsrc.CorePaper, "1.16.5", []string{log4jNoLookups, log4jConfig})
	plainServerJar(t, spigot116.Dir, "2.17.1")
	for _, i := range []*Instance{paper18, oldRef, spigot116} {
		for _, f := range []string{log4jXML, log4jXML17} {
			if err := os.WriteFile(filepath.Join(i.Dir, f), []byte("<Configuration>原来的</Configuration>"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	m.MigrateLog4j(context.Background())
	for i, want := range map[*Instance][]string{
		paper18:   {"-Xss1M", log4jNoLookups, "-XX:+UseG1GC"},
		oldRef:    {log4jNoLookups},
		spigot116: {log4jNoLookups},
	} {
		if !slices.Equal(i.ExtraJVM, want) {
			t.Errorf("%s：ExtraJVM=%q，应为 %q", i.Name, i.ExtraJVM, want)
		}
		if saved, err := readSettings(i.Dir); err != nil || !slices.Equal(saved.ExtraJVM, want) {
			t.Errorf("%s：instance.json：%v %q", i.Name, err, saved.ExtraJVM)
		}
		if bat := mustRead(t, filepath.Join(i.Dir, "run.bat")); strings.Contains(bat, "log4j.configurationFile") || !strings.Contains(bat, log4jNoLookups) {
			t.Errorf("%s：run.bat 要跟着摘掉引用、别的不动：\n%s", i.Name, bat)
		}
		for _, f := range []string{log4jXML, log4jXML17} {
			if got := mustRead(t, filepath.Join(i.Dir, f)); got != "<Configuration>原来的</Configuration>" {
				t.Errorf("%s：%s 不该动：%q", i.Name, f, got)
			}
		}
		if lines := strings.Join(i.Console.Lines(), "\n"); !strings.Contains(lines, "摘掉") || !strings.Contains(lines, "2.17") {
			t.Errorf("%s：控制台要说清楚：%s", i.Name, lines)
		}
		if log4jStartNote(i) != "" {
			t.Errorf("%s：摘过以后启动时不该再提示", i.Name)
		}
	}
	if len(*fetched) != 0 {
		t.Fatalf("摘引用不用下载：%q", *fetched)
	}
	// 再过一遍什么都不动
	before := mustRead(t, filepath.Join(paper18.Dir, "instance.json"))
	m.MigrateLog4j(context.Background())
	if mustRead(t, filepath.Join(paper18.Dir, "instance.json")) != before || len(*fetched) != 0 {
		t.Fatal("摘过的不该再动")
	}
}

// 问题 B 的整理：1.7 ~ 1.16.5 的游戏服，启动参数里没有任何 -Dlog4j.configurationFile= 的，按版本下载对应的官方配置、补上引用；
// 别的参数原样；instance.json、run.bat 跟着改。
func TestMigrateLog4jAddsMissingConfig(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	v189 := migrateInst(t, m, "1.8.9", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, "-Dmy.flag=1"})
	v1122 := migrateInst(t, m, "1.12.2", mcsrc.CoreVanilla, "1.12.2", []string{"-Xss1M", log4jNoLookups})
	forge := migrateInst(t, m, "1.7.10", mcsrc.CoreForge, "1.7.10", []string{log4jNoLookups})
	bare := migrateInst(t, m, "什么都没有", mcsrc.CorePaper, "1.16.5", nil)
	oldPaper := migrateInst(t, m, "没修过的 Paper", mcsrc.CorePaper, "1.12.2", []string{log4jNoLookups})
	paperclipServer(t, oldPaper.Dir, "1.12.2", "2.8.1", true)

	m.MigrateLog4j(context.Background())
	// 没修过的老 Paper（CraftBukkit 系、不带 TerminalConsoleAppender）不补：套上以后控制台没有输出、定时任务跟着失效，
	// 照 v3.2.2 不动，启动时提醒（2026-11-19 Codex 复审）
	if !slices.Equal(oldPaper.ExtraJVM, []string{log4jNoLookups}) {
		t.Errorf("没修过的老 Paper 不该补：%q", oldPaper.ExtraJVM)
	}
	if _, err := os.Stat(filepath.Join(oldPaper.Dir, log4jXML)); err == nil {
		t.Error("没修过的老 Paper 不该去下载配置")
	}
	if note := log4jStartNote(oldPaper); note != "" {
		t.Errorf("不补的就不该说「后台正在下载」：%q", note)
	}
	for i, want := range map[*Instance][]string{
		v189:  {log4jNoLookups, "-Dmy.flag=1", log4jConfig17},
		v1122: {"-Xss1M", log4jNoLookups, log4jConfig},
		forge: {log4jNoLookups, log4jConfig17},
		bare:  {log4jConfig},
	} {
		xml := strings.TrimPrefix(want[len(want)-1], log4jConfigKey)
		if !slices.Equal(i.ExtraJVM, want) {
			t.Errorf("%s：ExtraJVM=%q，应为 %q", i.Name, i.ExtraJVM, want)
		}
		if saved, err := readSettings(i.Dir); err != nil || !slices.Equal(saved.ExtraJVM, want) {
			t.Errorf("%s：instance.json：%v %q", i.Name, err, saved.ExtraJVM)
		}
		if bat := mustRead(t, filepath.Join(i.Dir, "run.bat")); !strings.Contains(bat, log4jConfigKey+xml) {
			t.Errorf("%s：run.bat 要跟着加上引用：\n%s", i.Name, bat)
		}
		if _, err := os.Stat(filepath.Join(i.Dir, xml)); err != nil {
			t.Errorf("%s：配置文件要在：%v", i.Name, err)
		}
		if lines := strings.Join(i.Console.Lines(), "\n"); !strings.Contains(lines, "补上了 Mojang 的日志配置") || !strings.Contains(lines, xml) {
			t.Errorf("%s：控制台要说清楚：%s", i.Name, lines)
		}
		if log4jStartNote(i) != "" {
			t.Errorf("%s：补上以后启动时不该再提示", i.Name)
		}
	}
	want := []string{log4jXML17, log4jXML, log4jXML17, log4jXML}
	got := slices.Clone(*fetched)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("下载了 %q，应为 %q", got, want)
	}
	// 再过一遍什么都不动
	m.MigrateLog4j(context.Background())
	if len(*fetched) != 4 {
		t.Fatalf("补过的不该再下载：%q", *fetched)
	}
}

// B，没联网：什么都不动（参数、instance.json、run.bat 原样，不留下配置文件），控制台说清楚、启动时再提示；联网以后再过一遍就补上。
func TestMigrateLog4jAddOffline(t *testing.T) {
	offlineNow := true
	stubLog4jFetch(t, func() error {
		if offlineNow {
			return errors.New("dial tcp: 没联网")
		}
		return nil
	})
	m := newMigrateManager()
	args := []string{log4jNoLookups, "-Dmy.flag=1"}
	i := migrateInst(t, m, "老服", mcsrc.CoreVanilla, "1.12.2", args)
	beforeJSON := mustRead(t, filepath.Join(i.Dir, "instance.json"))
	beforeBat := mustRead(t, filepath.Join(i.Dir, "run.bat"))

	m.MigrateLog4j(context.Background())
	if !slices.Equal(i.ExtraJVM, args) || mustRead(t, filepath.Join(i.Dir, "instance.json")) != beforeJSON || mustRead(t, filepath.Join(i.Dir, "run.bat")) != beforeBat {
		t.Fatalf("没下载成不该动参数和文件：%q", i.ExtraJVM)
	}
	if _, err := os.Stat(filepath.Join(i.Dir, log4jXML)); !os.IsNotExist(err) {
		t.Fatalf("没下载成不该留下配置文件：%v", err)
	}
	if lines := strings.Join(i.Console.Lines(), "\n"); !strings.Contains(lines, "没下载成") || !strings.Contains(lines, "没联网") || !strings.Contains(lines, "照样能开") {
		t.Fatalf("控制台要说清楚：%s", lines)
	}
	if note := log4jStartNote(i); !strings.Contains(note, log4jXML) || !strings.Contains(note, "后台正在下载") {
		t.Fatalf("还没补上，启动时要提示：%q", note)
	}

	offlineNow = false
	m.MigrateLog4j(context.Background())
	if want := []string{log4jNoLookups, "-Dmy.flag=1", log4jConfig}; !slices.Equal(i.ExtraJVM, want) {
		t.Fatalf("联网以后应补上：%q", i.ExtraJVM)
	}
}

// B 不该动的：用户自己引用了别的日志配置的；服务端自带的 log4j 修过了的（不该套的不套）；用不着配置的版本；
// 判断不了版本的；代理端。都不下载，参数、instance.json 一个不动。
func TestMigrateLog4jAddLeavesOthers(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	type keep struct {
		i    *Instance
		args []string
		json string
	}
	var keeps []keep
	add := func(name string, core mcsrc.Core, mc string, extra []string, setup func(dir string)) {
		i := migrateInst(t, m, name, core, mc, extra)
		if setup != nil {
			setup(i.Dir)
		}
		keeps = append(keeps, keep{i, slices.Clone(extra), mustRead(t, filepath.Join(i.Dir, "instance.json"))})
	}
	add("自己的配置", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfigKey + "my-log4j2.xml"}, nil)
	add("自己的配置，在别的文件夹里", mcsrc.CoreForge, "1.12.2", []string{log4jConfigKey + `..\shared\log.xml`, log4jNoLookups}, nil)
	add("自己的配置，没有 noLookups", mcsrc.CorePaper, "1.16.5", []string{log4jConfigKey + "custom.xml"}, nil)
	add("修过的 Paper 1.8，只有 noLookups", mcsrc.CorePaper, "1.8.9", []string{log4jNoLookups}, func(dir string) { paperclipServer(t, dir, "1.8.9", "2.17.0", false) })
	add("修过的 1.16.5，什么都没有", mcsrc.CorePaper, "1.16.5", nil, func(dir string) { plainServerJar(t, dir, "2.17.0") })
	add("1.17.1 用不着配置", mcsrc.CoreVanilla, "1.17.1", []string{log4jNoLookups}, nil)
	add("1.20.1", mcsrc.CorePaper, "1.20.1", nil, nil)
	add("1.6.4 还没用 log4j", mcsrc.CoreVanilla, "1.6.4", []string{log4jNoLookups}, nil)
	add("不认识的版本", mcsrc.CorePaper, "9.9-unknown", []string{log4jNoLookups}, nil)
	add("代理端", mcsrc.CoreVelocity, "1.16.5", []string{log4jNoLookups}, nil)
	add("已经套好的", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups, log4jConfig}, nil)

	m.MigrateLog4j(context.Background())
	if len(*fetched) != 0 {
		t.Fatalf("都不该下载：%q", *fetched)
	}
	for _, k := range keeps {
		if !slices.Equal(k.i.ExtraJVM, k.args) {
			t.Errorf("%s：参数不该动：%q", k.i.Name, k.i.ExtraJVM)
		}
		if got := mustRead(t, filepath.Join(k.i.Dir, "instance.json")); got != k.json {
			t.Errorf("%s：instance.json 不该动", k.i.Name)
		}
		for _, f := range []string{log4jXML, log4jXML17} {
			if _, err := os.Stat(filepath.Join(k.i.Dir, f)); !os.IsNotExist(err) {
				t.Errorf("%s：不该下载 %s（%v）", k.i.Name, f, err)
			}
		}
		if log4jStartNote(k.i) != "" {
			t.Errorf("%s：启动时不该提示", k.i.Name)
		}
	}
}

// 判断不了的照旧：paperclip 的 cache 里留着的是别的构建生成的（哈希对不上），不能凭它把引用摘掉。
func TestMigrateLog4jKeepsWhenBundleUnverified(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	args := []string{log4jNoLookups, log4jConfig17}
	i := migrateInst(t, m, "换过 jar", mcsrc.CorePaper, "1.8.9", args)
	p := paperclipServer(t, i.Dir, "1.8.9", "2.0-beta9", false)
	writeJar(t, p, map[string]string{log4jCorePom: log4jPom("2.17.0")})
	m.MigrateLog4j(context.Background())
	if !slices.Equal(i.ExtraJVM, args) || len(*fetched) != 0 {
		t.Fatalf("判断不了的不该动：%q %q", i.ExtraJVM, *fetched)
	}
}

// startFake 用测试程序自己扮演服务器，把 i 启动起来。
func startFake(t *testing.T, m *Manager, i *Instance) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeServerEnv, "1")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_ = os.WriteFile(filepath.Join(i.Dir, "server.properties"), []byte(fmt.Sprintf("server-port=%d\n", port)), 0o644)
	i.procMu.Lock()
	i.JavaPath, i.XmxMB, i.XmsMB = exe, 64, 64
	i.procMu.Unlock()
	if err := m.Start(i.Name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.Kill(i.Name)
		waitStatus(t, i, "stopped")
	})
	waitStatus(t, i, "running")
}

// 启动服务器时：该摘的引用当场摘掉，这一次启动就不带它（程序启动后那一遍做的时候 paperclip 还没生成过服务端、
// 之后开过一次才看得出来的，就走这里）。
func TestStartDropsLog4jConfigWhenSelfFixed(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "开服时摘", mcsrc.CorePaper, "1.8.9", []string{log4jNoLookups, log4jConfig17, "-Dkeep=1"})
	patched := paperclipServer(t, i.Dir, "1.8.9", "2.17.0", false)
	hidden := patched + ".away"
	if err := os.Rename(patched, hidden); err != nil { // 程序启动后那一遍：还没生成过服务端，判断不了
		t.Fatal(err)
	}
	m.MigrateLog4j(context.Background())
	if !slices.Contains(i.ExtraJVMSnapshot(), log4jConfig17) {
		t.Fatalf("判断不了的时候不该摘：%q", i.ExtraJVMSnapshot())
	}
	if err := os.Rename(hidden, patched); err != nil { // 开过一次，生成出来了
		t.Fatal(err)
	}
	startFake(t, m, i)
	lines := i.Console.Lines()
	launch := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || strings.Contains(lines[launch], "log4j.configurationFile") || !strings.Contains(lines[launch], "-Dkeep=1") || !strings.Contains(lines[launch], log4jNoLookups) {
		t.Fatalf("这一次启动就不该带着配置的引用，别的参数不动：%q", lines)
	}
	if !slices.ContainsFunc(lines[:launch], func(l string) bool {
		return strings.Contains(l, "摘掉") && strings.Contains(l, "这次启动就生效")
	}) {
		t.Fatalf("控制台要在启动之前说清楚：%q", lines)
	}
	want := []string{log4jNoLookups, "-Dkeep=1"}
	if saved, err := readSettings(i.Dir); err != nil || !slices.Equal(saved.ExtraJVM, want) || !slices.Equal(i.ExtraJVMSnapshot(), want) {
		t.Fatalf("instance.json：%v %q", err, saved.ExtraJVM)
	}
	if len(*fetched) != 0 {
		t.Fatalf("不用下载：%q", *fetched)
	}
}

// 套上 Mojang 的配置就没有控制台输出的那一类（CraftBukkit 系、不带 TerminalConsoleAppender）认得出来；
// 带着这份配置启动时先在控制台里说明，补上配置的那一刻也说；Paper 1.12 起那种（带 TerminalConsoleAppender）和原版不说。
func TestLog4jQuietConsoleNote(t *testing.T) {
	const vanillaMain = "Manifest-Version: 1.0\r\nMain-Class: net.minecraft.server.MinecraftServer\r\n\r\n"
	jars := map[string]map[string]string{
		"Spigot":     {"META-INF/MANIFEST.MF": craftMain, log4jCorePom: log4jPom("2.8.1")},
		"Paper 1.12": {"META-INF/MANIFEST.MF": craftMain, log4jCorePom: log4jPom("2.8.1"), termConsoleClass: "x"},
		"原版":         {"META-INF/MANIFEST.MF": vanillaMain},
	}
	for name, want := range map[string]bool{"Spigot": true, "Paper 1.12": false, "原版": false} {
		dir := t.TempDir()
		writeJar(t, filepath.Join(dir, "server.jar"), jars[name])
		if got := viewLog4jJar(dir, "jar:server.jar"); got.quiet != want || got.fixed {
			t.Errorf("%s：%+v，quiet 应为 %v", name, got, want)
		}
	}
	old := t.TempDir()
	paperclipServer(t, old, "1.8.9", "", true) // 老构建的 Paper：生成的服务端主类是 CraftBukkit 的，没有版本记录
	if got := viewLog4jJar(old, "jar:server.jar"); !got.quiet || got.fixed {
		t.Errorf("老构建的 Paper：%+v", got)
	}
	if err := os.Remove(filepath.Join(old, "cache", "patched_1.8.9.jar")); err != nil {
		t.Fatal(err)
	}
	if got := viewLog4jJar(old, "jar:server.jar"); got.quiet || got.fixed {
		t.Errorf("还没生成过服务端的 paperclip 判断不了：%+v", got)
	}

	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	quiet := func(lines []string) bool {
		return slices.ContainsFunc(lines, func(l string) bool {
			return strings.Contains(l, "控制台不会有输出") && strings.Contains(l, "latest.log")
		})
	}
	// 带着配置启动的 Spigot：启动那一行之前先说明，参数不动
	spigot := migrateInst(t, m, "Spigot 1.8", mcsrc.CorePaper, "1.8.9", []string{log4jNoLookups, log4jConfig17})
	writeJar(t, filepath.Join(spigot.Dir, "server.jar"), jars["Spigot"])
	startFake(t, m, spigot)
	lines := spigot.Console.Lines()
	launch := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || !quiet(lines[:launch]) || !strings.Contains(lines[launch], log4jConfig17) {
		t.Fatalf("带着配置启动的 Spigot 要先说明、照旧带着配置：%q", lines)
	}
	// 带 TerminalConsoleAppender 的、原版：不说
	for _, name := range []string{"Paper 1.12", "原版"} {
		i := migrateInst(t, m, name, mcsrc.CorePaper, "1.12.2", []string{log4jNoLookups, log4jConfig})
		writeJar(t, filepath.Join(i.Dir, "server.jar"), jars[name])
		startFake(t, m, i)
		if quiet(i.Console.Lines()) {
			t.Errorf("%s：控制台照常，不该说没有输出：%q", name, i.Console.Lines())
		}
	}
	// 没套上配置的 Spigot：不替它补（套上以后控制台没有输出、定时任务跟着失效），启动时提醒没套、为什么没补，这次照旧不带配置启动
	bare := migrateInst(t, m, "没套上的 Spigot", mcsrc.CorePaper, "1.12.2", []string{log4jNoLookups})
	writeJar(t, filepath.Join(bare.Dir, "server.jar"), jars["Spigot"])
	m.migrateLog4j(context.Background(), bare)
	if !slices.Equal(bare.ExtraJVM, []string{log4jNoLookups}) || len(*fetched) != 0 {
		t.Fatalf("没套上配置的 Spigot 不该补：%q，下载了 %q", bare.ExtraJVM, *fetched)
	}
	startFake(t, m, bare)
	lines = bare.Console.Lines()
	launch = slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || !slices.Contains(lines[:launch], log4jQuietBareNote) || strings.Contains(lines[launch], "log4j.configurationFile") ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) || len(*fetched) != 0 {
		t.Fatalf("启动时要提醒没套上、照旧不带配置启动、也不去下载：%q", lines)
	}
}

// 要补的配置文件名已经被一份不是官方的文件占着（比如自己改过的 112-116）：不覆盖、不引用，程序启动后那一遍和开服时都说清楚。
// 文件夹里已经有官方的那一份（比如换回了没修过的构建，引用以前被摘掉过）：开服时当场补上，这一次启动就生效（2026-11-19 Codex 复审）。
func TestLog4jAddRespectsExistingXML(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	mine := migrateInst(t, m, "自己改过的", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups})
	custom := []byte("<Configuration>我自己改过的</Configuration>")
	if err := os.WriteFile(filepath.Join(mine.Dir, log4jXML), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	m.migrateLog4j(context.Background(), mine)
	if b, _ := os.ReadFile(filepath.Join(mine.Dir, log4jXML)); string(b) != string(custom) {
		t.Fatalf("自己改过的配置被覆盖了：%s", b)
	}
	if !slices.Equal(mine.ExtraJVM, []string{log4jNoLookups}) || len(*fetched) != 0 {
		t.Fatalf("不该引用、不该下载：%q %q", mine.ExtraJVM, *fetched)
	}
	if !slices.ContainsFunc(mine.Console.Lines(), func(l string) bool { return strings.Contains(l, "没有覆盖它") }) {
		t.Fatalf("要说清楚为什么没补：%q", mine.Console.Lines())
	}
	startFake(t, m, mine)
	lines := mine.Console.Lines()
	launch := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || strings.Contains(lines[launch], "log4j.configurationFile") || len(*fetched) != 0 ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) {
		t.Fatalf("开服时也不该引用、不该下载：%q", lines)
	}
	if b, _ := os.ReadFile(filepath.Join(mine.Dir, log4jXML)); string(b) != string(custom) {
		t.Fatalf("开服时自己改过的配置被覆盖了：%s", b)
	}

	back := migrateInst(t, m, "换回了老构建", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups})
	officialLog4jXML(t, back.Dir)
	startFake(t, m, back)
	lines = back.Console.Lines()
	launch = slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || !strings.Contains(lines[launch], log4jConfig) || len(*fetched) != 0 {
		t.Fatalf("文件夹里有官方的那一份：这次启动就该带上引用、不用下载：%q %q", lines, *fetched)
	}
	if saved, err := readSettings(back.Dir); err != nil || !slices.Equal(saved.ExtraJVM, []string{log4jNoLookups, log4jConfig}) {
		t.Fatalf("instance.json 要跟着补上：%v %q", err, saved.ExtraJVM)
	}
}

// 文件夹里有官方的那一份、开服时要当场补上引用，可 instance.json 写不进去：照实说是设置文件写不进去，不说成在后台下载，
// 也不去下载；这次照旧启动，参数不动（A50）。
func TestLog4jAddReportsSettingsWriteFailure(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "设置写不进", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups})
	officialLog4jXML(t, i.Dir)
	if err := os.Mkdir(filepath.Join(i.Dir, "instance.json.tmp"), 0o755); err != nil { // 存 instance.json 先写的临时文件写不了
		t.Fatal(err)
	}
	startFake(t, m, i)
	lines := i.Console.Lines()
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "设置文件（instance.json）写不进去") && strings.Contains(l, log4jXML)
	}) ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) {
		t.Fatalf("应照实说设置文件写不进去、不说在下载：%q", lines)
	}
	launch := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || strings.Contains(lines[launch], "log4j.configurationFile") || len(*fetched) != 0 || !slices.Equal(i.ExtraJVMSnapshot(), []string{log4jNoLookups}) {
		t.Fatalf("这次照旧启动、参数不动、不下载：%q %q %q", lines, *fetched, i.ExtraJVMSnapshot())
	}
}

// 启动服务器时还没补上配置的（B）：控制台说一声、这次照旧启动（不等下载），后台去补，补上了下次启动生效。
func TestStartAddsLog4jConfigInBackground(t *testing.T) {
	release := make(chan struct{})
	stubLog4jFetch(t, func() error { <-release; return nil })
	m := newMigrateManager()
	i := migrateInst(t, m, "开服时补", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups})
	startFake(t, m, i) // 下载还卡着，启动照样完成
	lines := i.Console.Lines()
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") && strings.Contains(l, log4jXML) }) ||
		!slices.ContainsFunc(lines, func(l string) bool {
			return strings.Contains(l, "启动: java") && !strings.Contains(l, "log4j.configurationFile")
		}) {
		t.Fatalf("这次照旧启动，控制台要说一声：%q", lines)
	}
	close(release)
	waitConsole(t, i, "重启以后生效")
	if want := []string{log4jNoLookups, log4jConfig}; !slices.Equal(i.ExtraJVMSnapshot(), want) {
		t.Fatalf("后台应补上：%q", i.ExtraJVMSnapshot())
	}
}

// 只有 log4j-core 的版本记录、jar 里却没有它的类的（别的启动器重新打包依赖时留下的记录，真正的 log4j 从 libraries 另外加载）：
// 版本记录说明不了实际运行的是哪一版，不算修过，照旧套配置（2026-11-19 Codex 复审）。
func TestLog4jPomWithoutCoreIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "server.jar"), map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: com.example.Launcher\r\n\r\n",
		log4jCorePom:           log4jPom("2.17.1"),
	})
	if got := viewLog4jJar(dir, "jar:server.jar"); got.fixed {
		t.Fatalf("没有 log4j-core 的类，不该算修过：%+v", got)
	}
	if b, _ := bundledLog4j(dir, "jar:server.jar"); b != log4jBundleUnknown {
		t.Fatalf("应判断不了：%v", b)
	}
	plainServerJar(t, dir, "2.17.1") // 带着类的
	if got := viewLog4jJar(dir, "jar:server.jar"); !got.fixed {
		t.Fatalf("带着 log4j-core 的类、版本是修过的，应算修过：%+v", got)
	}
}

// 换配置（1.7 ~ 1.11.2 引用着 112-116 的）时，17-111 这个文件名已经被一份不是官方的文件占着：不覆盖、不换引用，说清楚
// （2026-11-19 Codex 复审第二轮）。
func TestLog4jSwapRespectsExistingXML(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "自己放了 17-111", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig})
	custom := []byte("<Configuration>我自己改过的 17-111</Configuration>")
	if err := os.WriteFile(filepath.Join(i.Dir, log4jXML17), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	m.migrateLog4j(context.Background(), i)
	if b, _ := os.ReadFile(filepath.Join(i.Dir, log4jXML17)); string(b) != string(custom) {
		t.Fatalf("自己改过的 17-111 被覆盖了：%s", b)
	}
	if !slices.Equal(i.ExtraJVM, []string{log4jNoLookups, log4jConfig}) || len(*fetched) != 0 {
		t.Fatalf("不该换引用、不该下载：%q %q", i.ExtraJVM, *fetched)
	}
	if !slices.ContainsFunc(i.Console.Lines(), func(l string) bool {
		return strings.Contains(l, "没有覆盖它") && strings.Contains(l, "换上引用")
	}) {
		t.Fatalf("要说清楚为什么没换：%q", i.Console.Lines())
	}
	startFake(t, m, i)
	if b, _ := os.ReadFile(filepath.Join(i.Dir, log4jXML17)); string(b) != string(custom) || len(*fetched) != 0 {
		t.Fatalf("开服时也不该覆盖、不该下载：%s %q", b, *fetched)
	}
}

// 启动的是 paperclip、它生成的服务端还核对不了（cache 里没有）：看不出是不是套上配置就没输出的那一类，先不补，
// 开服时也不说「后台正在下载」（2026-11-19 Codex 复审第二轮）。
func TestLog4jAddWaitsForPaperclipCache(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "缓存清掉了的老 Paper", mcsrc.CorePaper, "1.8.9", []string{log4jNoLookups})
	patched := paperclipServer(t, i.Dir, "1.8.9", "", true)
	if err := os.Remove(patched); err != nil {
		t.Fatal(err)
	}
	if v := viewLog4jJar(i.Dir, "jar:server.jar"); !v.clipPending || v.quiet || v.fixed {
		t.Fatalf("应认成还核对不了的 paperclip：%+v", v)
	}
	m.migrateLog4j(context.Background(), i)
	if !slices.Equal(i.ExtraJVM, []string{log4jNoLookups}) || len(*fetched) != 0 {
		t.Fatalf("不该补、不该下载：%q %q", i.ExtraJVM, *fetched)
	}
	startFake(t, m, i)
	if slices.ContainsFunc(i.Console.Lines(), func(l string) bool { return strings.Contains(l, "后台正在下载") }) || len(*fetched) != 0 {
		t.Fatalf("开服时也不该去补：%q %q", i.Console.Lines(), *fetched)
	}
}

// writeJarEntries 按给的先后造一个 jar，同名的条目可以出现几次。
func writeJarEntries(t *testing.T, path string, entries [][2]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeJarUnderstated 造一个 jar，里面 name 这一条申报的大小写成 10 字节、实际是 body。
func writeJarUnderstated(t *testing.T, path string, entries [][2]string, name, body string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, _ := zw.Create(e[0])
		_, _ = w.Write([]byte(e[1]))
	}
	w, err := zw.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte(body)),
		CompressedSize64: uint64(len(body)), UncompressedSize64: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// jar 里的元数据靠不住时按「判断不了」办，照旧套防护（A50）：看的条目同名的出现两次、元数据超过 64 KB（申报的或实际读到的）、
// pom.properties 里 version 写了两次又不一样。造的 jar 里另一份、后半截都是没修过的 2.14.1，以前会被判成修过了。
func TestLog4jUnreliableJarMetadataIsUnknown(t *testing.T) {
	pad := strings.Repeat("# 填充\n", 12000) // 七万多字节
	core := [2]string{log4jCoreClass, "x"}
	mf := [2]string{"META-INF/MANIFEST.MF", craftMain}
	pom := func(body string) [2]string { return [2]string{log4jCorePom, body} }
	clip := func(hash string) [2]string {
		return [2]string{"patch.properties", "version=1.16.5\npatchedHash=" + hash + "\n"}
	}
	cases := map[string][][2]string{
		"同名条目两份：前 2.14.1 后 2.17.1":        {mf, core, pom(log4jPom("2.14.1")), pom(log4jPom("2.17.1"))},
		"同名条目两份：前 2.17.1 后 2.14.1":        {mf, core, pom(log4jPom("2.17.1")), pom(log4jPom("2.14.1"))},
		"超长：前 64 KB 里写 2.17.1、后面写 2.14.1": {mf, core, pom(log4jPom("2.17.1") + pad + "version=2.14.1\n")},
		"超长：version 只在 64 KB 以后":          {mf, core, pom(pad + "version=2.17.1\n")},
		"同一个键两次：先 2.17.1 后 2.14.1":        {mf, core, pom("version=2.17.1\nversion=2.14.1\n")},
		"同一个键两次，另一种分隔：先 = 后 :":            {mf, core, pom("version=2.17.1\r\nversion: 2.14.1\r\n")},
		"log4j 的类两份（两个版本并在一个 jar 里）":      {mf, core, core, pom(log4jPom("2.17.1"))},
		"控制台的类两份":                         {mf, core, {termConsoleClass, "a"}, {termConsoleClass, "b"}, pom(log4jPom("2.17.1"))},
		"MANIFEST.MF 两份":                  {mf, mf, core, pom(log4jPom("2.17.1"))},
		"补丁说明两份":                          {mf, core, pom(log4jPom("2.17.1")), clip("AA"), clip("BB")},
	}
	settings := Settings{Core: mcsrc.CorePaper, MC: "1.16.5", LaunchTarget: "jar:server.jar", ExtraJVM: []string{log4jNoLookups, log4jConfig}}
	for name, entries := range cases {
		dir := t.TempDir()
		writeJarEntries(t, filepath.Join(dir, "server.jar"), entries)
		if _, err := readServerJar(filepath.Join(dir, "server.jar")); !errors.Is(err, errJarMetaUnsure) {
			t.Errorf("%s：应认成元数据靠不住：%v", name, err)
		}
		if v := viewLog4jJar(dir, "jar:server.jar"); v.fixed {
			t.Errorf("%s：不该判成修过了：%+v", name, v)
		}
		if b, _ := bundledLog4j(dir, "jar:server.jar"); b != log4jBundleUnknown {
			t.Errorf("%s：应判断不了：%v", name, b)
		}
		if fix, _, _ := log4jFixFor(settings, dir); fix == log4jFixDrop {
			t.Errorf("%s：判断不了时照旧套着防护，不该摘掉", name)
		}
	}

	dir := t.TempDir()
	writeJarUnderstated(t, filepath.Join(dir, "server.jar"), [][2]string{mf, core}, log4jCorePom, log4jPom("2.17.1")+pad)
	if _, err := readServerJar(filepath.Join(dir, "server.jar")); !errors.Is(err, errJarMetaUnsure) {
		t.Errorf("申报写小、实际超过 64 KB 的也算靠不住：%v", err)
	}

	// 对照：只有一份、键写一次（或者写两次一样）的照常认
	for name, body := range map[string]string{"只有一份": log4jPom("2.17.1"), "同一个键两次、写的一样": "version=2.17.1\nversion=2.17.1\n"} {
		dir := t.TempDir()
		writeJarEntries(t, filepath.Join(dir, "server.jar"), [][2]string{mf, core, pom(body)})
		if v := viewLog4jJar(dir, "jar:server.jar"); !v.fixed || v.ver != "2.17.1" {
			t.Errorf("%s：应照常认成修过的 2.17.1：%+v", name, v)
		}
	}
}

// 换配置（1.7 ~ 1.11.2 引用着 112-116 的）时，官方的 17-111 已经在文件夹里：开服时当场换上，这一次启动就生效、不下载；
// instance.json 写不进去时照实说，不说成在后台下载（A50，同「补」那一支）。
func TestLog4jSwapUsesOfficialXMLOnDisk(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "17-111 已在", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig})
	officialLog4jXMLFile(t, i.Dir, log4jXML17)
	startFake(t, m, i)
	lines := i.Console.Lines()
	launch := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "启动: java") })
	if launch < 0 || !strings.Contains(lines[launch], log4jConfig17) || strings.Contains(lines[launch], log4jConfig+" ") || len(*fetched) != 0 ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) ||
		!slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "已经换上，这次启动就生效") }) {
		t.Fatalf("17-111 已在：这次启动就该换上、不下载、不说在后台下载：%q %q", lines, *fetched)
	}
	if saved, err := readSettings(i.Dir); err != nil || !slices.Equal(saved.ExtraJVM, []string{log4jNoLookups, log4jConfig17}) {
		t.Fatalf("instance.json 要跟着换：%v %q", err, saved.ExtraJVM)
	}

	stuck := migrateInst(t, m, "设置写不进", mcsrc.CoreVanilla, "1.8.9", []string{log4jNoLookups, log4jConfig})
	officialLog4jXMLFile(t, stuck.Dir, log4jXML17)
	if err := os.Mkdir(filepath.Join(stuck.Dir, "instance.json.tmp"), 0o755); err != nil { // 存 instance.json 先写的临时文件写不了
		t.Fatal(err)
	}
	startFake(t, m, stuck)
	lines = stuck.Console.Lines()
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "设置文件（instance.json）写不进去") && strings.Contains(l, log4jXML17) && strings.Contains(l, "要换上")
	}) || slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "后台正在下载") }) || len(*fetched) != 0 {
		t.Fatalf("应照实说设置文件写不进去、不说在下载：%q", lines)
	}
	if !slices.Equal(stuck.ExtraJVMSnapshot(), []string{log4jNoLookups, log4jConfig}) {
		t.Fatalf("这次照旧带着原来的启动：%q", stuck.ExtraJVMSnapshot())
	}
}

// 开服前看着官方配置在文件夹里，当场换上、补上时它又没了（不存在）：不说成设置文件写不进去，交回去照旧在后台下载（A50）。
func TestLog4jFixOnDiskFallsBackWhenXMLGone(t *testing.T) {
	m := newMigrateManager()
	for _, c := range []struct {
		fix   log4jFix
		mc    string
		extra []string
		xml   string
	}{
		{log4jFixAdd, "1.12.2", []string{log4jNoLookups}, log4jXML},
		{log4jFixSwap, "1.8.9", []string{log4jNoLookups, log4jConfig}, log4jXML17},
	} {
		i := migrateInst(t, m, fmt.Sprintf("配置没了-%d", c.fix), mcsrc.CoreVanilla, c.mc, c.extra)
		i.procMu.Lock()
		done := m.log4jFixOnDiskLocked(i, c.fix, c.xml, false)
		i.procMu.Unlock()
		if done || slices.ContainsFunc(i.Console.Lines(), func(l string) bool { return strings.Contains(l, "写不进去") }) {
			t.Fatalf("配置文件没了：应交回去下载，不说写不进去：%v %q", done, i.Console.Lines())
		}
		if !slices.Equal(i.ExtraJVMSnapshot(), c.extra) {
			t.Fatalf("参数不该动：%q", i.ExtraJVMSnapshot())
		}
	}
}

// 后台整理时官方配置本来就在文件夹里（没有下载），却没能存进 instance.json：不说成「下载好了」。
func TestMigrateLog4jSettingsFailureNotSaidDownloaded(t *testing.T) {
	fetched := stubLog4jFetch(t, nil)
	m := newMigrateManager()
	i := migrateInst(t, m, "后台写不进", mcsrc.CoreVanilla, "1.12.2", []string{log4jNoLookups})
	officialLog4jXML(t, i.Dir)
	if err := os.Mkdir(filepath.Join(i.Dir, "instance.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.migrateLog4j(context.Background(), i)
	lines := i.Console.Lines()
	if len(*fetched) != 0 || !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "没能把它的引用存进 instance.json") }) ||
		slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "下载好了") }) {
		t.Fatalf("没下载就不该说下载好了：%q %q", lines, *fetched)
	}
}
