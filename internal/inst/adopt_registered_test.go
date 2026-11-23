package inst

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// 接管进来的服务器是单独登记的（app.InstanceDirs），启动时逐个读。
// 两件事要守住：这个文件夹正好也在某个存放根下面时只算一次，不报「实例名冲突」；
// 文件夹暂时不在（比如移动硬盘没插）时跳过，登记留着，回来了照样认。
func TestRegisteredDirs(t *testing.T) {
	m, user := adoptEnv(t)
	java := fakeJava(t)
	adoptOne := func(dir, name string) {
		t.Helper()
		writeTree(t, dir, map[string]string{"server.jar": vanilla21, "eula.txt": "eula=true\n"})
		req := AdoptReq{Dir: dir, Name: name, Core: mcsrc.CoreVanilla, MC: "1.21.1", LaunchTarget: "jar:server.jar", JavaMajor: 21, JavaPath: java}
		if out, err := adopt(t, m, req); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	// rescan 模拟重启：重新扫描一遍，连同这期间写的日志一起返回。
	rescan := func() (*Manager, string) {
		t.Helper()
		var buf bytes.Buffer
		old := log.Writer()
		log.SetOutput(&buf)
		defer log.SetOutput(old)
		m2, err := NewManager()
		if err != nil {
			t.Fatal(err)
		}
		return m2, buf.String()
	}

	inRoot := filepath.Join(app.ServersDir, "inroot") // 放在内置 servers 目录里：扫根找得到它，单独登记的那条也指着它
	away := filepath.Join(user, "away")
	adoptOne(inRoot, "根里的")
	adoptOne(away, "外面的")
	if got := app.InstanceDirs(); !slices.Equal(got, []string{inRoot, away}) {
		t.Fatalf("登记的文件夹：%v", got)
	}

	m2, logs := rescan()
	if len(m2.List()) != 2 || strings.Contains(logs, "实例名冲突") {
		t.Errorf("同一个文件夹应只算一次：%d 台\n%s", len(m2.List()), logs)
	}

	parked := away + "-unplugged"
	if err := os.Rename(away, parked); err != nil {
		t.Fatal(err)
	}
	m2, logs = rescan()
	if _, err := m2.Get("外面的"); err == nil || len(m2.List()) != 1 || strings.Contains(logs, "读不出来") {
		t.Errorf("文件夹不在时应不声不响地跳过：%d 台\n%s", len(m2.List()), logs)
	}
	if !slices.Contains(app.InstanceDirs(), away) {
		t.Errorf("文件夹只是暂时不在，登记不该去掉：%v", app.InstanceDirs())
	}

	if err := os.Rename(parked, away); err != nil {
		t.Fatal(err)
	}
	m2, _ = rescan()
	if i, err := m2.Get("外面的"); err != nil || i.Dir != away || !i.Adopted {
		t.Errorf("文件夹回来以后应照样认：%v", err)
	}
}

// skipOrFailJunction 同 lifecycle_review_test.go（18bcf89）：mklink /J 只在明确缺权限或文件系统不支持时跳过，其余失败直接判错。
func skipOrFailJunction(t *testing.T, err error, out []byte) {
	t.Helper()
	low := strings.ToLower(string(out))
	if strings.Contains(low, "privilege") || strings.Contains(low, "not support") || strings.Contains(low, "权限") || strings.Contains(low, "不支持") {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Fatalf("建不了目录联接：%v %s", err, out)
}

// 登记的是一个目录联接，真实文件夹又同时在存放根下面（或两条登记指着同一个真实文件夹）：
// A08 起单独登记的先加载，同一个真实文件夹只算一次；移出列表时不管加载时认的是哪条路径，按真实路径都能把登记清干净。
func TestForgetRegisteredAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	_, user := adoptEnv(t)
	real := filepath.Join(app.ServersDir, "srv")
	writeTree(t, real, map[string]string{"instance.json": `{"name":"srv","core":"vanilla","mc":"1.21.1","launchTarget":"jar:server.jar"}`})
	link := filepath.Join(user, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		skipOrFailJunction(t, err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) }) // 只删联接本身
	app.RememberInstanceDir(link)

	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i, err := m2.Get("srv"); err != nil || i.Dir != link || len(m2.List()) != 1 || len(m2.SkippedDuplicates()) != 0 {
		t.Fatalf("单独登记的联接应优先加载且不把同一真实目录算重名：%v dir=%q（%d 台，重名=%v）", err, func() string {
			if i == nil {
				return ""
			}
			return i.Dir
		}(), len(m2.List()), m2.SkippedDuplicates())
	}
	if err := m2.Delete("srv", false); err != nil {
		t.Fatal(err)
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("移出列表后，经目录联接登记的那一条还留着：%v", got)
	}

	// 再测 i.Dir != link 的情况：真实路径和联接都登记在 InstanceDirs 里，加载认到第一条 real，移出列表时联接那条登记也按真实路径一起清掉
	if err := os.Rename(filepath.Join(real, settingsBackup), filepath.Join(real, "instance.json")); err != nil {
		t.Fatal(err)
	}
	app.RememberInstanceDir(real)
	app.RememberInstanceDir(link)
	m3, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i, err := m3.Get("srv"); err != nil || i.Dir != real || len(m3.List()) != 1 {
		t.Fatalf("应认到第一条真实目录且只算一台：%v（%d 台）", err, len(m3.List()))
	}
	if err := m3.Delete("srv", false); err != nil {
		t.Fatal(err)
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("i.Dir 是真实路径时，经目录联接登记的别名也应一起清掉：%v", got)
	}
}
