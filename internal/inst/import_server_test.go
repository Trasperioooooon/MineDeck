package inst

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
)

// zipOf 造一个服务端压缩包。jar 条目直接写进 zip（内容已经是 jar 字节）。
func zipOf(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func paperJarBytes(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "paper.jar")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"META-INF/MANIFEST.MF":   "Manifest-Version: 1.0\r\nMain-Class: io.papermc.paperclip.Main\r\n\r\n",
		"META-INF/versions.list": "abc\t1.20.1\t1.20.1/paper-1.20.1.jar\n",
		"version.json":           `{"id":"1.20.1","java_version":17}`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 作者给的服务端 zip：套了一层文件夹，没有整合包清单。解压后用接管已有服务器的识别登记，不重新下载核心。
func TestImportServerArchive(t *testing.T) {
	m, _ := importEnv(t)
	z := zipOf(t, map[string]string{
		"作者服/server.properties":    "motd=hello\nserver-port=25565\nlevel-name=world\n",
		"作者服/mods/jei.jar":         "mod-bytes",
		"作者服/paper-1.20.1-100.jar": paperJarBytes(t),
		"作者服/eula.txt":             "eula=false\n",
		// 别人给的启动脚本和登记文件不能采用，否则下次启动就按包里写的去跑。
		"作者服/instance.json": `{"name":"evil","javaPath":"C:\\evil.exe"}`,
		"作者服/run.bat":       "evil",
	})
	pack, err := modpack.Parse(z)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Format != modpack.FormatServer || pack.Root != "作者服" || pack.Core != mcsrc.CorePaper || pack.MC != "1.20.1" {
		t.Fatalf("解析结果不对: %+v", pack)
	}
	dir := filepath.Join(app.ServersDir, "paper-srv")
	tk, _ := m.Tasks.New("导入服务端", serverImportSteps)
	req := CreateReq{Name: "纸服", XmxMB: 2048, Port: 25566, EULA: true, AllowFlight: true}
	if err := m.runServerImport(context.Background(), tk, pack, req, dir); err != nil {
		t.Fatalf("导入失败: %v\n日志: %v", err, tk.Snapshot().Log)
	}
	i, err := m.Get("纸服")
	if err != nil {
		t.Fatal(err)
	}
	if i.Adopted || i.Dir != dir || i.Core != mcsrc.CorePaper || i.MC != "1.20.1" || i.JavaMajor != 17 || i.JavaPath != testJava17 {
		t.Fatalf("登记不对: adopted=%v dir=%s core=%s mc=%s java=%d path=%s launch=%s",
			i.Adopted, i.Dir, i.Core, i.MC, i.JavaMajor, i.JavaPath, i.LaunchTarget)
	}
	if i.LaunchTarget != "jar:paper-1.20.1-100.jar" {
		t.Fatalf("启动文件应是压缩包里的那个 jar: %s", i.LaunchTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, "作者服")); !os.IsNotExist(err) {
		t.Fatal("包装目录不应留在实例里")
	}
	mod, err := os.ReadFile(filepath.Join(dir, "mods", "jei.jar"))
	if err != nil || string(mod) != "mod-bytes" {
		t.Fatalf("模组没解到实例里: %v %q", err, mod)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(props)
	if !strings.Contains(text, "motd=hello") || !strings.Contains(text, "server-port=25566") || !strings.Contains(text, "allow-flight=true") {
		t.Fatalf("server.properties 应改端口和飞行、留着 MOTD: %s", text)
	}
	eula, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if err != nil || !strings.Contains(string(eula), "eula=true") {
		t.Fatalf("应写上同意的 eula: %v %s", err, eula)
	}
	bat, err := os.ReadFile(filepath.Join(dir, "run.bat"))
	if err != nil || !strings.Contains(string(bat), "paper-1.20.1-100.jar") || strings.TrimSpace(string(bat)) == "evil" {
		t.Fatalf("run.bat 应是 MineDeck 按这个 jar 生成的，不是包里那份: %v %s", err, bat)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil || strings.Contains(string(saved), "evil.exe") || !strings.Contains(string(saved), `"name": "纸服"`) {
		t.Fatalf("instance.json 应是登记时写的，不是压缩包里那份: %v %s", err, saved)
	}
	if _, err := os.Stat(filepath.Join(dir, "paper-1.20.1-100.jar")); err != nil {
		t.Fatal(err)
	}
}
