package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/inst"
)

// 交接包里落到 Windows 上是同一个文件的两条（只差大小写，或者写法一模一样）（v3.1.0 正式版前检查 C-2e）：
// 以前后一条把前一条盖掉，什么也不说。内容不一样的是被改过的包，和可疑路径一样整包不要、一个字节都不落盘；
// 内容一样的是同一个文件打了两遍（3.1.0 及以前导出完整包时，启动 jar 叫 Server.jar 就会和 server.jar 各打一份），照常导入，只解一份。
func TestExtractCaseDuplicates(t *testing.T) {
	for _, pair := range [][2]string{
		{"instance/config/A.toml", "instance/config/a.toml"},
		{"instance/Config/a.toml", "instance/config/A.TOML"},
		{"instance/config/a.toml", "instance/config/a.toml"},
	} {
		pack := rawPack(t, "", [][2]string{{"instance/world/level.dat", "ok"}, {pair[0], "one"}, {pair[1], "two"}})
		dest := filepath.Join(t.TempDir(), "dest")
		_, err := extractPrefix(nil, pack, dest, extractOpts{}, noProgress, noCount)
		if err == nil || !strings.Contains(err.Error(), "同一个文件") {
			t.Errorf("%q 和 %q 内容不一样，应整包拒绝：%v", pair[0], pair[1], err)
		}
		if left := treeOf(t, filepath.Dir(dest)); len(left) != 0 {
			t.Errorf("被拒之前不该落下任何文件：%v", left)
		}
	}
	pack := rawPack(t, "", [][2]string{{"instance/Server.jar", "jar-bytes"}, {"instance/world/level.dat", "ok"}, {"instance/server.jar", "jar-bytes"}})
	dest := filepath.Join(t.TempDir(), "dest")
	res, err := extractPrefix(nil, pack, dest, extractOpts{}, noProgress, noCount)
	if err != nil {
		t.Fatalf("同一个文件打了两遍的包应照常导入：%v", err)
	}
	if got := treeOf(t, dest); !slices.Equal(got, []string{"Server.jar", "world/level.dat"}) || res.bytes != int64(len("jar-bytes")+len("ok")) {
		t.Errorf("只该解一份：%v，%d 字节", got, res.bytes)
	}
}

// 导出时按 Windows 的规矩去重、挑名字（C-2e）：启动 jar 叫 Server.jar 时，它和 server.jar 是同一个文件，只打一份
// （以前打两份，导入那边现在会认出来）；名字在别的 Windows 上写不出原样的（con.txt 这类：Windows 11 上能建，
// 老一些的 Windows 上是设备）不打进包，写进剔除清单和任务日志——导入那边把它当可疑路径整包拒绝。打出来的包照常能导入。
func TestExportLaunchJarCaseAndBadNames(t *testing.T) {
	newEnv(t)
	dir := fakeInstance(t, "A 的服")
	if err := os.Rename(filepath.Join(dir, "server.jar"), filepath.Join(dir, "Server.jar")); err != nil {
		t.Fatal(err)
	}
	var s inst.Settings
	b, _ := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	s.LaunchTarget = "jar:Server.jar"
	b, _ = json.MarshalIndent(s, "", "  ")
	writeFile(t, filepath.Join(dir, "instance.json"), string(b))
	// con.txt 只有这台 Windows 把它当普通文件名时才建得出来（老一些的 Windows 上写它就是写设备）
	badName := filepath.IsLocal("con.txt")
	if badName {
		writeFile(t, filepath.Join(dir, "config", "con.txt"), "x")
	}
	m := manager(t, "A 的服")
	id, err := ExportAsync(m, ExportReq{Inst: "A 的服", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	snap, _, _, _ := waitTask(t, m, id)
	if snap.Err != "" {
		t.Fatalf("导出失败：%s\n日志：%s", snap.Err, strings.Join(snap.Log, "\n"))
	}
	jars := 0
	for _, e := range packEntries(t, snap.Result) {
		if strings.EqualFold(e, "instance/server.jar") {
			jars++
		}
		if strings.EqualFold(e, "instance/config/con.txt") {
			t.Errorf("con.txt 不该进包：%s", e)
		}
	}
	if jars != 1 {
		t.Errorf("Server.jar 和 server.jar 是同一个文件，应只打一份，实际 %d 份", jars)
	}
	if badName {
		mf, err := ReadManifest(snap.Result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(mf.Excluded, "\n"), "config/con.txt") || !strings.Contains(strings.Join(snap.Log, "\n"), "config/con.txt") {
			t.Errorf("没进包的 con.txt 要写进剔除清单和任务日志：%v\n%s", mf.Excluded, strings.Join(snap.Log, "\n"))
		}
	}
	iid, err := ImportAsync(m, snap.Result, ImportReq{Name: "B 接手"})
	if err != nil {
		t.Fatal(err)
	}
	if isnap, _, _, _ := waitTask(t, m, iid); isnap.Err != "" {
		t.Fatalf("导出的包应能导入：%s\n日志：%s", isnap.Err, strings.Join(isnap.Log, "\n"))
	}
}
