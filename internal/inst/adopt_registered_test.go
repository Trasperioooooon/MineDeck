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
// 同一个真实文件夹只算一次；同时在存放根下面时 i.Dir 用存放根下的真实目录，移出列表时按真实路径把联接登记也清干净。
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
	if i, err := m2.Get("srv"); err != nil || i.Dir != real || len(m2.List()) != 1 || len(m2.SkippedDuplicates()) != 0 {
		t.Fatalf("同一个真实文件夹也在存放根下面时应采用根下目录且不算重名：%v dir=%q（%d 台，重名=%v）", err, func() string {
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

	// 再测真实路径和联接都登记在 InstanceDirs 里的情况：加载认到 real，移出列表时联接那条登记也按真实路径一起清掉
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
		t.Fatalf("应认到真实目录且只算一台：%v（%d 台）", err, len(m3.List()))
	}
	if err := m3.Delete("srv", false); err != nil {
		t.Fatal(err)
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Errorf("i.Dir 是真实路径时，经目录联接登记的别名也应一起清掉：%v", got)
	}
}

// 真实目录在 ServersDir 里、又经目录联接登记在 InstanceDirs 里时，「连同文件删除」必须删掉真实目录并清掉登记，
// 重载 Manager 后这台服务器不会从 ServersDir 里复活。
func TestDeleteRemovesRootDirWhenRegisteredViaJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("目录联接是 Windows 上的东西")
	}
	_, user := adoptEnv(t)
	real := filepath.Join(app.ServersDir, "srv-del")
	writeTree(t, real, map[string]string{
		"instance.json":   `{"name":"联接服","core":"vanilla","mc":"1.21.1","launchTarget":"jar:server.jar","adopted":true}`,
		"world/level.dat": "world-data",
	})
	link := filepath.Join(user, "link-del")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		skipOrFailJunction(t, err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	app.RememberInstanceDir(link)

	m1, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m1.ShutdownAll(0) })

	if err := m1.Delete("联接服", true); err != nil {
		t.Fatalf("连同文件删除应成功：%v", err)
	}
	if _, err := os.Stat(real); !os.IsNotExist(err) {
		t.Fatalf("ServersDir 下的真实实例文件夹应已删掉，实际 err=%v", err)
	}
	if got := app.InstanceDirs(); len(got) != 0 {
		t.Fatalf("删除后联接登记应已清掉：%v", got)
	}

	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.ShutdownAll(0) })
	if _, err := m2.Get("联接服"); err == nil || len(m2.List()) != 0 {
		t.Fatalf("重载后已删掉的服务器不应复活：list=%d err=%v", len(m2.List()), err)
	}
}

// 列表里那台保留下来的同名服务器移出列表后，或者原先被跳过的文件夹自己已经被接管进列表后，SkippedDuplicates 不再继续提示它。
func TestSkippedDuplicatesClearedAfterRemovingKept(t *testing.T) {
	_, user := adoptEnv(t)
	root2 := filepath.Join(user, "root2")
	if _, err := app.UpdateConfig(func(c *app.Config) error {
		c.Roots = []string{root2}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	keptDir := filepath.Join(user, "kept-srv")
	dupDir1 := filepath.Join(app.ServersDir, "dup-srv-1")
	dupDir2 := filepath.Join(root2, "dup-srv-2")
	for _, d := range []string{keptDir, dupDir1, dupDir2} {
		writeTree(t, d, map[string]string{
			"instance.json": `{"name":"同名服","core":"vanilla","mc":"1.21.1","launchTarget":"jar:server.jar"}`,
			"server.jar":    vanilla21,
			"eula.txt":      "eula=true\n",
		})
	}
	app.RememberInstanceDir(keptDir)

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.ShutdownAll(0) })

	if got := len(m.SkippedDuplicates()); got != 2 {
		t.Fatalf("初始应有 2 个重名跳过项，实际 %d", got)
	}

	// 把列表里留着的那台移出列表：列表里已经没有「同名服」，不再提示「和列表里的同名服重名」
	if err := m.Delete("同名服", false); err != nil {
		t.Fatal(err)
	}
	if got := m.SkippedDuplicates(); len(got) != 0 {
		t.Fatalf("移出保留实例后不应继续报告重名跳过项，实际：%+v", got)
	}

	// 再把 dupDir1 直接接管成「同名服」：dupDir1 自己进了列表不该再出现在重名提示里，还没进列表的 dupDir2 指向新的保留目录
	if out, err := adopt(t, m, AdoptReq{
		Dir: dupDir1, Name: "同名服", Core: mcsrc.CoreVanilla, MC: "1.21.1",
		LaunchTarget: "jar:server.jar", JavaMajor: 21, JavaPath: fakeJava(t),
	}); err != nil {
		t.Fatalf("接管 dupDir1 失败：%v\n%s", err, out)
	}
	want := []SkippedDuplicate{{Name: "同名服", Dir: dupDir2, KeptDir: dupDir1}}
	if got := m.SkippedDuplicates(); !slices.Equal(got, want) {
		t.Fatalf("接管 dupDir1 后 SkippedDuplicates = %+v，期望 %+v", got, want)
	}
}
