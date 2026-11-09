package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// 接管已有服务器：识别和接管都只许人在本机操作（要读主机上任意一个文件夹）；指定的 Java 必须是已知的。
func TestAdoptOps(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "old-server")
	writeTestJar(t, filepath.Join(dir, "server.jar"), map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: net.minecraft.bundler.Main\r\n\r\n",
		"version.json":         `{"id":"1.21.1","java_version":21}`,
	})
	if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanArgs := ops.ArgsOf(map[string]any{"dir": dir})

	res, err := reg.Do(ctx, localUI, "instance.adopt.scan", scanArgs)
	if err != nil {
		t.Fatal(err)
	}
	sc := res.(*inst.AdoptScan)
	if sc.Core != "vanilla" || sc.MC != "1.21.1" || sc.LaunchTarget != "jar:server.jar" || sc.JavaMajor != 21 || !sc.EULA || sc.Name != "old-server" {
		t.Errorf("识别结果：%+v", sc)
	}

	// 别的设备、AI（应用内助手、MCP、游戏里）都不行
	if _, err := reg.Do(ctx, ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.5"}, "instance.adopt.scan", scanArgs); ops.CodeOf(err) != 403 || !strings.Contains(err.Error(), "仅支持在本机操作") {
		t.Errorf("局域网应拒绝：%v", err)
	}
	for _, src := range []ops.Source{ops.SrcAssistant, ops.SrcMCP, ops.SrcGameChat} {
		for _, name := range []string{"instance.adopt.scan", "instance.adopt"} {
			if _, err := reg.Do(ctx, ops.Caller{Source: src, Actor: "x", Session: "s", Local: true}, name, scanArgs); ops.CodeOf(err) != 403 {
				t.Errorf("%s 调 %s 应拒绝：%v", src, name, err)
			}
		}
	}

	// 指定的 java.exe 不在 java.list 里：拒绝，什么都不写（不能借接管让程序去执行任意一个 exe）
	req := map[string]any{"dir": dir, "name": "老服", "core": "vanilla", "mc": "1.21.1", "launchTarget": "jar:server.jar",
		"javaMajor": 21, "javaPath": `C:\evil\java.exe`}
	if _, err := reg.Do(ctx, localUI, "instance.adopt", ops.ArgsOf(req)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "不是已知的 Java") {
		t.Errorf("不认识的 Java 应拒绝：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); err == nil {
		t.Error("被拒绝了还写了 instance.json")
	}
	// 请求不合格在开任务之前就报出来
	req["javaPath"], req["launchTarget"] = "", "jar:nope.jar"
	if _, err := reg.Do(ctx, localUI, "instance.adopt", ops.ArgsOf(req)); err == nil || !strings.Contains(err.Error(), "启动文件不在") {
		t.Errorf("%v", err)
	}
}
