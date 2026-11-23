//go:build windows

package inst

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
)

// 实例文件夹本身被别的进程当作工作目录时，里面的文件和 instance.json 都能删掉，只剩最后删空文件夹失败（A121）：
// Delete 照常把服务器移出列表并记日志，重开后也不会再出现在列表里；子进程退出后空文件夹可以清掉。
func TestDeleteRemovesInstanceWhenWorkingDirHeld(t *testing.T) {
	_, ext := adoptEnv(t)
	dir := filepath.Join(ext, "held-cwd-srv")
	writeTree(t, dir, map[string]string{
		"eula.txt":          "eula=true\n",
		"instance.json":     `{"name":"占着目录的服","core":"vanilla","mc":"1.21.4","javaMajor":21,"launchTarget":"jar:server.jar","adopted":true}`,
		"server.properties": "server-port=25565\n",
		"world/level.dat":   "world-bytes",
	})
	app.RememberInstanceDir(dir)
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.ShutdownAll(0) })

	if _, err := m.Get("占着目录的服"); err != nil {
		t.Fatalf("前置条件：服务器应在列表里：%v", err)
	}

	releaseChild := holdWorkingDir(t, dir)

	prevOut := log.Writer()
	var logBuf bytes.Buffer
	log.SetOutput(io.MultiWriter(prevOut, &logBuf))
	defer log.SetOutput(prevOut)

	if err := m.Delete("占着目录的服", true); err != nil {
		t.Fatalf("只剩空文件夹被占作工作目录时 Delete(files=true) 不应报错，实际：%v", err)
	}
	if !strings.Contains(logBuf.String(), "空文件夹") || !strings.Contains(logBuf.String(), dir) {
		t.Errorf("日志应记录留下空文件夹及路径，实际日志：%q", logBuf.String())
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("被占作工作目录的空文件夹此时应仍可读：%v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("实例文件夹里的内容应已全部删空，剩余：%v", ents)
	}
	if _, err := m.Get("占着目录的服"); err == nil {
		t.Fatal("删除后服务器不应仍在当前列表里")
	}
	if len(app.InstanceDirs()) != 0 {
		t.Fatalf("删除后独立登记应已移除，实际：%v", app.InstanceDirs())
	}

	m2, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.ShutdownAll(0) })
	if _, err := m2.Get("占着目录的服"); err == nil {
		t.Fatal("重新加载后服务器不应再出现在列表里")
	}

	releaseChild()
	if err := atomicfile.RetryRemove(func() error { return os.Remove(dir) }); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("结束占用子进程后空文件夹应能删掉：%v", err)
	}
}

// holdWorkingDir 起一个工作目录设在 dir 的子进程（测试程序自己扮演服务器进程，见 proc_test.go 的 fakeServerEnv），
// 等它打出第一行再返回；返回的函数关掉它的标准输入、等它退出，测试结束时也会自动调。建不了子进程时，CI 上直接失败，不许跳过。
func holdWorkingDir(t *testing.T, dir string) (release func()) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI 环境下获取测试可执行文件失败：%v", err)
		}
		t.Skipf("无法获取测试可执行文件：%v", err)
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fakeServerEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI 环境下启动占着工作目录的子进程失败：%v", err)
		}
		t.Skipf("无法启动子进程：%v", err)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		_ = stdin.Close() // 假服务器读到标准输入结束就退出
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(release)

	lineCh := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(stdout).ReadString('\n')
		lineCh <- s
	}()
	select {
	case s := <-lineCh:
		if !strings.Contains(s, "Done") {
			t.Fatalf("子进程没有正常就绪：%q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等子进程进入工作目录超时")
	}
	return release
}

// 删到最后文件夹没删掉时，日志按文件夹里实际还剩什么说：空的说留了空文件夹；不空的（删的同时有程序写进了文件）
// 写清还剩几项、是哪些、文件夹在哪，不能也说成「留了一个空文件夹」。
func TestLogLeftInstanceDirSaysWhatIsLeft(t *testing.T) {
	capture := func(dir string) string {
		prevOut := log.Writer()
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(prevOut)
		logLeftInstanceDir("留下文件夹的服", dir, errors.New("占用"))
		return buf.String()
	}

	empty := t.TempDir()
	if got := capture(empty); !strings.Contains(got, "留了一个空文件夹") || !strings.Contains(got, empty) {
		t.Errorf("空文件夹：%q", got)
	}

	full := t.TempDir()
	writeTree(t, full, map[string]string{"a.log": "x", "b.log": "x", "c.log": "x", "d.log": "x"})
	got := capture(full)
	if strings.Contains(got, "空文件夹") || !strings.Contains(got, "还剩 4 项") || !strings.Contains(got, "a.log") ||
		!strings.Contains(got, "…") || !strings.Contains(got, full) {
		t.Errorf("文件夹不空时应写清还剩几项和路径：%q", got)
	}
}
