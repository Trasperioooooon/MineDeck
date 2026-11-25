package inst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
)

// A104：instance.json 读不出来、JSON 坏了或缺少 name 的文件夹会出现在 BrokenInstances() 里（仅文件夹名、编号与原因，
// 不含完整磁盘路径），不影响健康服务器正常加载；支持通过编号打开该目录，拒绝子目录与不存在的编号，且修好/删掉后自动消失。
func TestBrokenInstancesLoadOpenAndClear(t *testing.T) {
	_, user := adoptEnv(t)

	badJSONDir := filepath.Join(app.ServersDir, "bad-json")
	noNameDir := filepath.Join(app.ServersDir, "no-name")
	badTypeDir := filepath.Join(app.ServersDir, "bad-type")
	healthyDir := filepath.Join(app.ServersDir, "healthy")
	removedDir := filepath.Join(app.ServersDir, "removed")
	plainDir := filepath.Join(app.ServersDir, "not-an-instance")
	adoptedBrokenDir := filepath.Join(user, "adopted-broken")

	writeTree(t, badJSONDir, map[string]string{
		"instance.json": `{not valid json`,
	})
	writeTree(t, noNameDir, map[string]string{
		"instance.json": `{"core":"paper","mc":"1.21.4"}`,
	})
	writeTree(t, badTypeDir, map[string]string{
		"instance.json": `{"name":"类型错服","xmxMb":"4G"}`,
	})
	writeTree(t, healthyDir, map[string]string{
		"instance.json": `{"name":"健康服","core":"paper","mc":"1.21.4","launchTarget":"jar:server.jar"}`,
	})
	writeTree(t, removedDir, map[string]string{
		"instance.json.bak": `{"name":"已移出"}`,
	})
	writeTree(t, plainDir, map[string]string{
		"readme.txt": "hello",
	})
	writeTree(t, adoptedBrokenDir, map[string]string{
		"instance.json": `{broken`,
	})
	app.RememberInstanceDir(adoptedBrokenDir)

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.ShutdownAll(time.Second) })

	list := m.List()
	if len(list) != 1 || list[0].Name != "健康服" {
		t.Fatalf("健康服务器应正常加载，不应被坏目录连累：%+v", list)
	}

	broken := m.BrokenInstances()
	if len(broken) != 4 {
		t.Fatalf("期望列出 4 个损坏目录，实际 %d 个：%#v", len(broken), broken)
	}

	byFolder := map[string]BrokenInstance{}
	for _, b := range broken {
		if !strings.HasPrefix(b.ID, brokenIDPrefix) || strings.Contains(b.ID, "/") || strings.Contains(b.ID, "\\") {
			t.Fatalf("损坏目录 ID 格式不对：%#v", b)
		}
		if strings.Contains(b.Folder, string(filepath.Separator)) || strings.Contains(b.Folder, "/") {
			t.Fatalf("Folder 不应包含路径分隔符：%#v", b)
		}
		for _, secretPath := range []string{app.Base, app.ServersDir, user} {
			if strings.Contains(b.Reason, secretPath) || strings.Contains(b.ID, secretPath) || strings.Contains(b.Folder, secretPath) {
				t.Fatalf("BrokenInstance 不应暴露磁盘完整路径 %q：%#v", secretPath, b)
			}
		}
		byFolder[b.Folder] = b
	}

	if b, ok := byFolder["bad-json"]; !ok || !strings.Contains(b.Reason, "JSON") {
		t.Errorf("bad-json 原因不对：%#v", byFolder)
	}
	if b, ok := byFolder["no-name"]; !ok || !strings.Contains(b.Reason, "name") {
		t.Errorf("no-name 原因不对：%#v", byFolder)
	}
	if b, ok := byFolder["bad-type"]; !ok || !strings.Contains(b.Reason, "xmxMb") {
		t.Errorf("bad-type 原因不对：%#v", byFolder)
	}
	if _, ok := byFolder["adopted-broken"]; !ok {
		t.Errorf("单独登记的损坏目录也应列出：%#v", byFolder)
	}

	// 损坏目录不能当成已加载服务器获取、启动或删除
	target := byFolder["bad-json"]
	if _, err := m.Get(target.ID); err == nil {
		t.Errorf("损坏目录编号不应被 Get 认成正常实例")
	}
	if err := m.Start(target.ID); err == nil {
		t.Errorf("损坏目录编号不应允许 Start")
	}
	if err := m.Delete(target.ID, false); err == nil {
		t.Errorf("损坏目录编号不应允许移出列表")
	}
	if err := m.Delete(target.ID, true); err == nil {
		t.Errorf("损坏目录编号不应允许删除")
	}

	// OpenDir 只开根目录自身，拒绝 sub 和未知编号，且把真实路径交给 openFolder
	var opened []string
	origOpen := openFolder
	openFolder = func(dir string) error {
		opened = append(opened, dir)
		return nil
	}
	t.Cleanup(func() { openFolder = origOpen })

	if err := m.OpenDir(target.ID, "mods"); err == nil {
		t.Errorf("损坏目录不允许传 sub 打开子目录")
	}
	if err := m.OpenDir("__broken__:nonexistent", ""); err == nil {
		t.Errorf("不存在的损坏编号应返回错误")
	}
	if err := m.OpenDir(target.ID, ""); err != nil {
		t.Errorf("打开损坏目录应成功：%v", err)
	}
	if len(opened) != 1 || filepath.Clean(opened[0]) != filepath.Clean(badJSONDir) {
		t.Errorf("实际打开的路径不对：%v，期望 %s", opened, badJSONDir)
	}

	// 修好一份、删掉配置文件一份、删掉整个目录一份，剩下的也清掉后应立刻从 BrokenInstances 消失
	if err := os.WriteFile(filepath.Join(badJSONDir, "instance.json"), []byte(`{"name":"修好啦","core":"paper"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(noNameDir, "instance.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(badTypeDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(adoptedBrokenDir, "instance.json")); err != nil {
		t.Fatal(err)
	}
	if after := m.BrokenInstances(); len(after) != 0 {
		t.Errorf("全部修好或删除后应不再返回损坏目录：%#v", after)
	}
	if err := m.OpenDir(byFolder["bad-type"].ID, ""); err == nil {
		t.Errorf("整个文件夹被删后再点打开应报错")
	}
}
