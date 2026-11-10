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

// 登记的是一个目录联接，而启动时是从存放根下面先扫到真实的那个文件夹：移出列表时登记也要去掉，
// 不能因为两边路径写法不一样就留下一条清不掉的。
func TestForgetRegisteredAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	_, user := adoptEnv(t)
	real := filepath.Join(app.ServersDir, "srv")
	writeTree(t, real, map[string]string{"instance.json": `{"name":"srv","core":"vanilla","mc":"1.21.1","launchTarget":"jar:server.jar"}`})
	link := filepath.Join(user, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) }) // 只删联接本身
	app.RememberInstanceDir(link)

	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if i, err := m2.Get("srv"); err != nil || i.Dir != real || len(m2.List()) != 1 {
		t.Fatalf("应只认到存放根下面的那一个：%v（%d 台）", err, len(m2.List()))
	}
	if err := m2.Delete("srv", false); err != nil {
		t.Fatal(err)
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("移出列表后，经目录联接登记的那一条还留着：%v", got)
	}
}
