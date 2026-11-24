package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/tasks"
)

// 卸载便携 Java 的途中（还在查有没有服务器用着它）把一台服务器换成这套：换 Java 回 409「正在卸载」，
// 那台服的 Java 设置不变；卸完以后这套已经没了（A34）。
func TestJavaSetRefusedWhilePortableRemoving(t *testing.T) {
	reg, mgr := newCatalogMgr(t)
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	if err := os.MkdirAll(filepath.Dir(java.Exe(21)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(java.Exe(21), []byte("jvm21"), 0o644); err != nil {
		t.Fatal(err)
	}
	i, err := mgr.Get("生存服")
	if err != nil {
		t.Fatal(err)
	}
	beforeMajor, beforePath := i.JavaSnapshot()

	checking, release, removed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		removed <- java.RemovePortable(21, func(string) error { close(checking); <-release; return nil })
	}()
	<-checking
	_, err = reg.Do(context.Background(), ui, "instance.java.set", ops.Args(`{"inst":"生存服","major":21}`))
	close(release)
	if rerr := <-removed; rerr != nil {
		t.Fatalf("卸载应照常做完：%v", rerr)
	}
	if ops.CodeOf(err) != 409 || !strings.Contains(err.Error(), "正在卸载") {
		t.Fatalf("卸载途中换成这套应回 409「正在卸载」：%v", err)
	}
	if m, p := i.JavaSnapshot(); m != beforeMajor || p != beforePath {
		t.Fatalf("被拒时 Java 设置不该变：Java %d %s", m, p)
	}
	if java.IsInstalled(21) {
		t.Fatal("这套 Java 应已卸掉")
	}
}

// 下载便携 Java 还没完时同一台服务器再保存一次：返回同一个任务编号，任务列表里只有一个（A36）。
// 别的服务器要同一个版本照样有自己的任务（下好了要换上的是它自己）；下完以后再保存是新的一次。
func TestJavaSetReusesRunningDownload(t *testing.T) {
	reg, mgr := newCatalogMgr(t)
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	srvDir := filepath.Join(app.ServersDir, "s2")
	if err := os.MkdirAll(srvDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.FinalizeCreate(inst.Settings{Name: "创造服", Core: mcsrc.CorePaper, MC: "1.21.4", XmxMB: 1024, XmsMB: 512, LaunchTarget: "jar:server.jar"}, srvDir); err != nil {
		t.Fatal(err)
	}
	// 下好的 Java 放在运行时目录外面：knownJava 认不出它，下完以后再保存还是要下载，看得出是不是新的一次
	exe := filepath.Join(t.TempDir(), "jdk25", "bin", "java.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("jvm25"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	old := ensureJava
	ensureJava = func(ctx context.Context, _ int, _ func(string, ...any), _ dl.Progress) (string, error) {
		select {
		case <-release:
			return exe, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	t.Cleanup(func() { unblock(); waitJavaTasks(t, mgr); ensureJava = old })

	save := func(name string) string {
		t.Helper()
		res, err := reg.Do(context.Background(), ui, "instance.java.set", ops.Args(`{"inst":"`+name+`","major":25}`))
		if err != nil {
			t.Fatal(err)
		}
		m, _ := res.(map[string]string)
		id := m["taskId"]
		if id == "" {
			t.Fatalf("要下载时应返回任务编号：%+v", res)
		}
		return id
	}
	first := save("生存服")
	if again := save("生存服"); again != first {
		t.Fatalf("同一台、同一个版本的下载还在跑，应返回同一个任务：%s 和 %s", first, again)
	}
	if n := len(javaTasks(mgr)); n != 1 {
		t.Fatalf("任务列表里应只有一个换 Java 的任务，实际 %d 个", n)
	}
	if other := save("创造服"); other == first {
		t.Fatal("别的服务器要同一个版本应有自己的任务")
	}
	unblock()
	waitJavaTasks(t, mgr)
	if i, _ := mgr.Get("生存服"); func() bool { m, p := i.JavaSnapshot(); return m != 25 || p != exe }() {
		t.Fatal("下好以后应换上")
	}
	if next := save("生存服"); next == first {
		t.Fatal("上一次的任务结束了，再保存应是新的一次")
	}
}

// javaTasks 任务列表里换 Java 的那些。
func javaTasks(mgr *inst.Manager) []tasks.Summary {
	var out []tasks.Summary
	for _, s := range mgr.Tasks.List() {
		if s.Kind == "java" {
			out = append(out, s)
		}
	}
	return out
}

// waitJavaTasks 等换 Java 的任务都结束（最多 5 秒）。
func waitJavaTasks(t *testing.T, mgr *inst.Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		busy := false
		for _, s := range javaTasks(mgr) {
			busy = busy || !s.Ended
		}
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("换 Java 的任务 5 秒还没结束")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
