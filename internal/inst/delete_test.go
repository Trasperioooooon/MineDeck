package inst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
)

// 连同文件删除服务器时，先删其余文件、最后才删 instance.json（A121）：
// 中途碰上被别的程序独占的文件时，删除报错、instance.json 仍在、实例仍留在列表里；放开占用后再删可干净删完。
func TestDeleteKeepsInstanceJSONWhenFileBusy(t *testing.T) {
	_, _ = adoptEnv(t)
	dir := filepath.Join(app.ServersDir, "busy-srv")
	writeTree(t, dir, map[string]string{
		"eula.txt":          "eula=true\n",
		"instance.json":     `{"name":"占着文件的服","core":"vanilla","mc":"1.21.4","javaMajor":21,"launchTarget":"jar:server.jar"}`,
		"server.properties": "server-port=25565\n",
		"world/level.dat":   "world-bytes",
	})
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.ShutdownAll(0) })

	if _, err := m.Get("占着文件的服"); err != nil {
		t.Fatalf("前置条件：服务器应在列表里：%v", err)
	}

	busyFile := filepath.Join(dir, "server.properties")
	unlock := lockExclusive(t, busyFile)
	defer unlock()

	err = m.Delete("占着文件的服", true)
	if err == nil {
		t.Fatal("server.properties 被独占时 Delete(files=true) 应报错")
	}
	if !strings.Contains(err.Error(), "server.properties") {
		t.Fatalf("报错应指明删不掉的文件 server.properties，实际：%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "instance.json")); statErr != nil {
		t.Fatalf("删除中途失败时 instance.json 必须还在：%v", statErr)
	}
	s, readErr := readSettings(dir)
	if readErr != nil || s.Name != "占着文件的服" || s.Core != mcsrc.CoreVanilla {
		t.Fatalf("instance.json 内容应完好可读：err=%v settings=%+v", readErr, s)
	}
	if _, getErr := m.Get("占着文件的服"); getErr != nil {
		t.Fatalf("删除失败后服务器应仍留在列表里：%v", getErr)
	}

	// 放开占用后再删：仍能通过 instance.json 核对并把整个文件夹删干净
	unlock()
	if err := m.Delete("占着文件的服", true); err != nil {
		t.Fatalf("放开句柄后再次 Delete(files=true) 应成功，实际报错：%v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("再次删除后实例文件夹应已不存在，实际 err=%v", statErr)
	}
	if _, getErr := m.Get("占着文件的服"); getErr == nil {
		t.Fatal("再次删除成功后服务器不应仍在列表里")
	}
}
