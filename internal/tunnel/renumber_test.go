package tunnel

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/secret"
)

// 这一组测的是 tunnels.json 里已经撞了号（A58）：以前手改把 seq 调小，新增的隧道拿到了已有的编号，文件里就有两条
// 一样编号的隧道，按编号改、删、启动时只认得靠前的那条。现在读进来时靠前的那条留着原编号，后面的换新编号，
// 先在旁边留一份原文件的备份再写回；凭据原样搬过去。测试里的凭据都是假的：dpapi: 开头的是编出来的乱码（解不开，
// 会被标成「凭据解不开」），能解开的是现场加密的假话，明文的是一眼看得出是假的字串。

// 假的密文：带 base64 里的 +、/、=，看 JSON 写回去以后一个字节都没变
const (
	fakeSealedA = "dpapi:QUFBQS1mYWtlLXR3bw=="
	fakeSealedB = "dpapi:RkFLRS1ub3QtYS1yZWFsLWNyZWQ+Lz0="
	fakeSealedC = "dpapi:Wm1GclpTMTBhSEpsWlE9PQ=="
)

// pinBackupClock 备份文件名里的时间钉在 2026-11-24 15:30:00，返回那时候该叫的名字。
func pinBackupClock(t *testing.T) string {
	t.Helper()
	old := backupNow
	t.Cleanup(func() { backupNow = old })
	backupNow = func() time.Time { return time.Date(2026, 11, 24, 15, 30, 0, 0, time.Local) }
	return "tunnels.json.bak-20261124-153000"
}

// backupNames 程序文件夹里 tunnels.json 的备份，按名字排好。
func backupNames(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(app.Base)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "tunnels.json.bak-") {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

func jsonStr(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// dupFixture 一份撞了号的 tunnels.json（带 BOM、压成一行、seq 比现有编号小）。第一条和换号的「第三个一号」的凭据是
// 现场加密的假话（解得开：换号时要是解开再加密，密文就变了，看得出来），别的是编出来的乱码。每条写的字段和保存时
// 写出来的一样（没有省掉的、也没有多出来的），解开来比得出「除了编号什么都没变」。
// 返回原文和五条隧道按顺序的凭据。读进来以后编号应该是 tun1…tun5、seq 是 5：
// 「语音」tun1→tun3、「自建」tun2→tun4、「第三个一号」tun1→tun5。
func dupFixture(t *testing.T) (orig string, creds []string) {
	t.Helper()
	sealed, err := secret.Seal("fake-cred-一号-not-real")
	if err != nil {
		t.Fatal(err)
	}
	sealed5, err := secret.Seal("fake-cred-五号-not-real")
	if err != nil {
		t.Fatal(err)
	}
	creds = []string{sealed, fakeSealedA, fakeSealedB, "", sealed5}
	orig = "\xef\xbb\xbf" + `{"seq":1,"tunnels":[` +
		`{"id":"tun1","provider":"openfrp","name":"游戏一","credential":` + jsonStr(t, sealed) + `,"proxyId":"101","boundInstance":"S1","autoStart":true},` +
		`{"id":"tun2","provider":"natfrp","name":"游戏二","credential":"` + fakeSealedA + `","proxyId":"102","autoStart":false},` +
		`{"id":"tun1","provider":"natfrp","name":"语音","credential":"` + fakeSealedB + `","proxyId":"103","boundInstance":"S1","autoStart":true,"publicAddr":"voice.example.com:20001","purpose":"voice"},` +
		`{"id":"tun2","provider":"custom","name":"自建","credential":"","serverAddr":"frp.example.com","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false,"proto":"tcp"},` +
		`{"id":"tun1","provider":"openfrp","name":"第三个一号","credential":` + jsonStr(t, sealed5) + `,"proxyId":"104","autoStart":false}` +
		`]}`
	return orig, creds
}

var dupWantIDs = []string{"tun1", "tun2", "tun3", "tun4", "tun5"}

type diskTunnels struct {
	Seq     int `json:"seq"`
	Tunnels []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Credential string `json:"credential"`
		Bound      string `json:"boundInstance"`
		Purpose    string `json:"purpose"`
	} `json:"tunnels"`
}

func readDisk(t *testing.T) (diskTunnels, string) {
	t.Helper()
	s := readFile(t, tunnelsPath())
	var d diskTunnels
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatalf("写回的 tunnels.json 解析不了：%v\n%s", err, s)
	}
	return d, s
}

func diskIDs(d diskTunnels) []string {
	var ids []string
	for _, t := range d.Tunnels {
		ids = append(ids, t.ID)
	}
	return ids
}

// 撞了号的文件读进来：编号不再重复、靠前的那条编号没变、seq 不小于任何编号；写回的文件里凭据逐字节相同；
// 旁边有一份和原文件一字不差（连 BOM）的备份；操作记录在操作层登记回调时补上（启动时回调还没登记）。
// 再启动一次不再写、不再多备份；之后新增接着往下排。
func TestLoadRenumbersDuplicateIDs(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, creds := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	if err := m.LoadErr(); err != nil {
		t.Fatalf("有重复编号不算没读成：%v", err)
	}
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("编号应不再重复、靠前的不变：%v", ids)
	}
	if m.seq < 5 {
		t.Fatalf("seq=%d，比换出来的编号小，下次新增会撞号", m.seq)
	}
	if st := statusOf(t, m, "tun1"); st.Name != "游戏一" || st.CredBroken {
		t.Fatalf("tun1 应还是靠前的那条、凭据解得开：%+v", st)
	}
	if st := statusOf(t, m, "tun3"); st.Name != "语音" || !st.CredBroken || st.Bound != "S1" || !st.IsVoice() {
		t.Fatalf("换成 tun3 的应是「语音」，其余配置照旧，它的凭据解不开的标记跟着它：%+v", st)
	}
	if cred, err := secret.Unseal(m.get("tun1").Credential); err != nil || cred != "fake-cred-一号-not-real" {
		t.Fatalf("靠前那条的凭据：%q %v", cred, err)
	}

	d, written := readDisk(t)
	if !slices.Equal(diskIDs(d), dupWantIDs) || d.Seq != m.seq {
		t.Fatalf("写回的文件：编号 %v、seq %d（内存里 %d）", diskIDs(d), d.Seq, m.seq)
	}
	for i, tt := range d.Tunnels {
		if !bytes.Equal([]byte(tt.Credential), []byte(creds[i])) {
			t.Fatalf("第 %d 条的凭据变了：%q → %q", i+1, creds[i], tt.Credential)
		}
		if creds[i] != "" && !strings.Contains(written, jsonStr(t, creds[i])) {
			t.Fatalf("第 %d 条的凭据在文件里不是原样的字节：%s", i+1, written)
		}
	}
	if d.Tunnels[2].Name != "语音" || d.Tunnels[2].Purpose != "voice" || d.Tunnels[2].Bound != "S1" {
		t.Fatalf("换号的那条其余内容应照旧：%+v", d.Tunnels[2])
	}

	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("旁边应有一份备份 %s：%v", bakName, got)
	}
	if b := readFile(t, filepath.Join(app.Base, bakName)); b != orig {
		t.Fatalf("备份和原文件不一样：\n原：%q\n备：%q", orig, b)
	}

	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 {
		t.Fatalf("登记回调时应补上一条换号的操作记录：%+v", notes)
	}
	a := notes[0]
	want := []renumbered{{"语音", "tun1", "tun3"}, {"自建", "tun2", "tun4"}, {"第三个一号", "tun1", "tun5"}}
	if got, _ := a.Args["renamed"].([]renumbered); a.Op != "tunnel.renumber" || a.Actor != renumberActor || a.Err != nil ||
		a.Args["backup"] != bakName || a.Args["backupSealed"] != nil || !slices.Equal(got, want) {
		t.Fatalf("换号的操作记录：%+v", a)
	}

	// 再启动一次：没有重复了，不写、不多留备份
	m2 := NewManager()
	if ids := tunnelIDs(m2); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("重启后的编号：%v", ids)
	}
	if again := readFile(t, tunnelsPath()); again != written {
		t.Fatal("没有重复编号时不该再写 tunnels.json")
	}
	if got := backupNames(t); len(got) != 1 {
		t.Fatalf("没有重复编号时不该再留备份：%v", got)
	}
	added, err := m2.Add(openfrpTunnel("新加的"))
	if err != nil || added.ID != "tun6" {
		t.Fatalf("新增应接着排到 tun6：%+v %v", added, err)
	}
}

// 没有重复编号的文件读进来：一个字节也不改，也不留备份，不记操作记录。原文件压成一行，哪怕重写一遍也看得出来。
func TestLoadWithoutDuplicatesWritesNothing(t *testing.T) {
	tempBase(t)
	pinBackupClock(t)
	orig := `{"seq":2,"tunnels":[{"id":"tun1","provider":"natfrp","name":"甲","credential":"` + fakeSealedA +
		`","proxyId":"1","autoStart":false},{"id":"tun2","provider":"natfrp","name":"乙","credential":"` + fakeSealedB +
		`","proxyId":"2","autoStart":false},{"id":"custom","provider":"natfrp","name":"丙","credential":"` + fakeSealedC + `","proxyId":"3","autoStart":false}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1", "tun2", "custom"}) {
		t.Fatalf("编号：%v", ids)
	}
	if got := readFile(t, tunnelsPath()); got != orig {
		t.Fatalf("没有重复编号时 tunnels.json 被改写了：\n前：%s\n后：%s", orig, got)
	}
	if got := backupNames(t); len(got) != 0 {
		t.Fatalf("没有重复编号时不该留备份：%v", got)
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 0 {
		t.Fatalf("没有重复编号时不该记操作记录：%+v", notes)
	}
}

// 不是 tun<数字> 的编号撞了（手改出来的）也换，新编号从 seq 往上排。
func TestRenumberNonNumericIDs(t *testing.T) {
	tempBase(t)
	pinBackupClock(t)
	orig := `{"seq":0,"tunnels":[{"id":"abc","provider":"natfrp","name":"甲","credential":"` + fakeSealedA +
		`","proxyId":"1","autoStart":false},{"id":"abc","provider":"natfrp","name":"乙","credential":"` + fakeSealedB + `","proxyId":"2","autoStart":false}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"abc", "tun1"}) || m.seq != 1 {
		t.Fatalf("编号：%v seq=%d", ids, m.seq)
	}
}

// 备份的名字：同一秒里已经有一份同名、内容不一样的（比如上次也是这一秒）就加 -2，原来那份一个字不动；
// 已经有一份内容一模一样的就用它，不再另存。
func TestRenumberBackupNeverOverwrites(t *testing.T) {
	t.Run("同一秒已有同名的", func(t *testing.T) {
		tempBase(t)
		bakName := pinBackupClock(t)
		orig, _ := dupFixture(t)
		if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(app.Base, bakName)
		if err := os.WriteFile(other, []byte("别的东西"), 0o600); err != nil {
			t.Fatal(err)
		}
		m := NewManager()
		if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
			t.Fatalf("编号：%v", ids)
		}
		if got := readFile(t, other); got != "别的东西" {
			t.Fatalf("已有的同名备份被盖掉了：%q", got)
		}
		if got := backupNames(t); !slices.Equal(got, []string{bakName, bakName + "-2"}) {
			t.Fatalf("应另起一个 -2 的名字：%v", got)
		}
		if got := readFile(t, filepath.Join(app.Base, bakName+"-2")); got != orig {
			t.Fatal("-2 的备份应是原文件")
		}
	})
	t.Run("已有一模一样的", func(t *testing.T) {
		tempBase(t)
		pinBackupClock(t)
		orig, _ := dupFixture(t)
		if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		old := "tunnels.json.bak-20261101-080000"
		if err := os.WriteFile(filepath.Join(app.Base, old), []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		m := NewManager()
		if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
			t.Fatalf("编号：%v", ids)
		}
		if got := backupNames(t); !slices.Equal(got, []string{old}) {
			t.Fatalf("已有一模一样的备份就不该再另存：%v", got)
		}
		var notes []AutoAction
		m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
		if len(notes) != 1 || notes[0].Args["backup"] != old {
			t.Fatalf("操作记录里的备份应是已有的那份：%+v", notes)
		}
	})
}

// 备份留不下（这里让唯一能用的名字被占着）：不写回，原文件一个字不动；内存里编号照样不重复，操作记录说没写回去。
// 之后哪次保存时备份留得下了，先留备份、再把换好的编号和这次的改动一起写进去。
func TestRenumberNoBackupNoWrite(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Base, bakName), []byte("别的东西"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTries := backupNameTries
	t.Cleanup(func() { backupNameTries = oldTries })
	backupNameTries = 1

	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("写不回去时内存里的编号也不该重复：%v", ids)
	}
	if err := m.LoadErr(); err != nil {
		t.Fatalf("写不回去不算没读成：%v", err)
	}
	if got := readFile(t, tunnelsPath()); got != orig {
		t.Fatal("备份留不下时不该改写 tunnels.json")
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 || notes[0].Err == nil || !strings.Contains(notes[0].Err.Error(), "没能先留一份原文件的备份") ||
		notes[0].Args["backup"] != nil {
		t.Fatalf("操作记录应说没写回去、没有备份：%+v", notes)
	}

	backupNameTries = oldTries
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName, bakName + "-2"}) {
		t.Fatalf("这次保存前应先留下备份：%v", got)
	}
	if got := readFile(t, filepath.Join(app.Base, bakName+"-2")); got != orig {
		t.Fatal("备份应是原文件")
	}
	d, _ := readDisk(t)
	if ids := diskIDs(d); !slices.Equal(ids, append(slices.Clone(dupWantIDs), "tun6")) {
		t.Fatalf("写进去的编号：%v", ids)
	}
}

// tunnels.json 是只读的：写不回去，内存里照新编号用；备份留着（之后哪次保存成功前它必须在），
// 再启动一次认得出已有一模一样的备份，不会每次启动都多一份。去掉只读以后，下一次保存把换好的编号写进去。
func TestRenumberReadOnlyFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读属性挡住覆盖是 Windows 的行为")
	}
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tunnelsPath(), 0o644) })

	for i := 0; i < 2; i++ {
		m := NewManager()
		if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
			t.Fatalf("第 %d 次启动：写不回去时内存里的编号也不该重复：%v", i+1, ids)
		}
		if got := readFile(t, tunnelsPath()); got != orig {
			t.Fatalf("第 %d 次启动：只读的 tunnels.json 被改了", i+1)
		}
		if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
			t.Fatalf("第 %d 次启动：备份应始终只有一份：%v", i+1, got)
		}
		var notes []AutoAction
		m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
		if len(notes) != 1 || notes[0].Err == nil || notes[0].Args["backup"] != bakName {
			t.Fatalf("第 %d 次启动：操作记录应说没写回去、备份在哪：%+v", i+1, notes)
		}
	}

	m := NewManager()
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })
	<-notes // 启动时那条：没写回去
	if err := os.Chmod(tunnelsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	d, _ := readDisk(t)
	if ids := diskIDs(d); !slices.Equal(ids, append(slices.Clone(dupWantIDs), "tun6")) {
		t.Fatalf("去掉只读后保存，换好的编号应一起写进去：%v", ids)
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("备份应还是那一份：%v", got)
	}
	a := nextNote(t, notes, "换好的编号写进去了，应补一条成功的换号操作记录")
	if got, _ := a.Args["renamed"].([]renumbered); a.Op != "tunnel.renumber" || a.Err != nil || a.Args["backup"] != bakName || len(got) != 3 {
		t.Fatalf("补的那条换号操作记录：%+v", a)
	}
}

// 启动时没读成、之后才读成（这时操作层的回调已经登记）：换号照样做，操作记录照样记（另起 goroutine 交出去，
// 不在持着 m.mu 的时候调回调）。
func TestRenumberAfterRetryLoadNotes(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig[:30]), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), true)
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })

	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadErr(); err != nil {
		t.Fatal(err)
	}
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("编号：%v", ids)
	}
	select {
	case a := <-notes:
		if a.Op != "tunnel.renumber" || a.Err != nil || a.Args["backup"] != bakName {
			t.Fatalf("换号的操作记录：%+v", a)
		}
	case <-time.After(time.Second):
		t.Fatal("读成以后换了号，应记一条操作记录")
	}
}

// nextNote 等回调收到下一条操作记录（保存时那条是另起 goroutine 交出去的），一秒内没等到算失败。
func nextNote(t *testing.T, notes <-chan AutoAction, why string) AutoAction {
	t.Helper()
	select {
	case a := <-notes:
		return a
	case <-time.After(time.Second):
		t.Fatal(why)
	}
	return AutoAction{}
}

// captureLog 程序日志接到一个缓冲区里，测试结束还原。
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

// sameButIDs 两份 tunnels.json 解开来比：每条隧道去掉 id 以后一模一样，顶层除了 seq 也一模一样。
func sameButIDs(t *testing.T, a, b string) {
	t.Helper()
	strip := func(s string) map[string]any {
		var v map[string]any
		if err := json.Unmarshal(bytes.TrimPrefix([]byte(s), []byte("\xef\xbb\xbf")), &v); err != nil {
			t.Fatalf("解析不了：%v\n%s", err, s)
		}
		list, _ := v["tunnels"].([]any)
		for _, it := range list {
			delete(it.(map[string]any), "id")
		}
		delete(v, "seq")
		return v
	}
	if x, y := strip(a), strip(b); !reflect.DeepEqual(x, y) {
		t.Fatalf("除了编号和 seq 还有别的不一样：\n%v\n%v", x, y)
	}
}

// 换号只动编号：备份（读进来时的那份）和写回的文件解开来比，每条去掉 id 以后一模一样（跟随启动、公网地址、隧道 ID、
// 凭据都没变），顶层只有 seq 不一样。
func TestRenumberChangesOnlyIDs(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	NewManager()
	sameButIDs(t, readFile(t, filepath.Join(app.Base, bakName)), readFile(t, tunnelsPath()))
}

// 没有重复编号的文件读进来以后再保存：不留备份，不记换号的操作记录。
func TestNoDuplicatesThenSaveLeavesNoBackup(t *testing.T) {
	tempBase(t)
	pinBackupClock(t)
	orig := `{"seq":1,"tunnels":[{"id":"tun1","provider":"natfrp","name":"甲","credential":"` + fakeSealedA + `","proxyId":"1","autoStart":false}]}`
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	if got := backupNames(t); len(got) != 0 {
		t.Fatalf("没有重复编号，保存时不该留备份：%v", got)
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) }) // 排着队的在这里同步交出来
	if len(notes) != 0 {
		t.Fatalf("没有重复编号时不该记换号的操作记录：%+v", notes)
	}
}

// 换好的编号写进去以后就不再管备份：把备份删掉再保存，不会再生成一份，也不再说「这次写进去了」、不再补记操作记录。
func TestRenumberBackupOnlyUntilWritten(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	m := NewManager()
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("启动时应留一份备份：%v", got)
	}
	if err := os.Remove(filepath.Join(app.Base, bakName)); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	if got := backupNames(t); len(got) != 0 {
		t.Fatalf("已经写进去了，之后保存不该再留备份：%v", got)
	}
	if strings.Contains(logs.String(), "这次写进去了") {
		t.Fatalf("已经写进去了，之后保存不该再说一遍：%s", logs.String())
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 || notes[0].Err != nil {
		t.Fatalf("只该有启动时那一条换号的操作记录：%+v", notes)
	}
}

// within f 在 d 之内做完，做不完算卡死。
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s过了 %v 还没返回：多半是持着 m.mu 调了回调，回调里再拿锁就卡死了", what, d)
	}
}

// 换号的操作记录不在持着 m.mu 的时候交给回调：回调里再调要拿锁的方法（这里是 List）不会卡死。两条持着锁的路：
// 启动时没读成、之后才读成（retryLoad），启动时没写进去、之后保存才写进去（save）。
func TestRenumberNoteOutsideLock(t *testing.T) {
	listing := func(m *Manager, got chan<- []string) func(AutoAction) {
		return func(a AutoAction) {
			if a.Op == "tunnel.renumber" {
				got <- tunnelIDs(m)
			}
		}
	}
	t.Run("之后才读成", func(t *testing.T) {
		tempBase(t)
		fastLoadRetry(t)
		pinBackupClock(t)
		orig, _ := dupFixture(t)
		if err := os.WriteFile(tunnelsPath(), []byte(orig[:30]), 0o600); err != nil {
			t.Fatal(err)
		}
		m := NewManager()
		got := make(chan []string, 4)
		m.SetAutoHook(listing(m, got))
		if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		within(t, 3*time.Second, "读成以后换号的那次 LoadErr ", func() { err = m.LoadErr() })
		if err != nil {
			t.Fatal(err)
		}
		select {
		case ids := <-got:
			if !slices.Equal(ids, dupWantIDs) {
				t.Fatalf("回调里拿到的列表：%v", ids)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("回调没收到换号的操作记录，或者回调里的 List 卡住了")
		}
	})
	t.Run("之后保存才写进去", func(t *testing.T) {
		tempBase(t)
		bakName := pinBackupClock(t)
		orig, _ := dupFixture(t)
		if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		// 唯一能用的备份名被占着：启动时备份留不下，不写回
		if err := os.WriteFile(filepath.Join(app.Base, bakName), []byte("别的东西"), 0o600); err != nil {
			t.Fatal(err)
		}
		oldTries := backupNameTries
		t.Cleanup(func() { backupNameTries = oldTries })
		backupNameTries = 1
		m := NewManager()
		got := make(chan []string, 4)
		m.SetAutoHook(listing(m, got))
		<-got // 启动时那条（没写回去）在登记回调时交出来，那时没持着锁
		backupNameTries = oldTries
		var err error
		within(t, 3*time.Second, "写进换好的编号的那次保存（Add）", func() { _, err = m.Add(openfrpTunnel("新加的")) })
		if err != nil {
			t.Fatal(err)
		}
		select {
		case ids := <-got:
			if len(ids) != 6 {
				t.Fatalf("回调里拿到的列表：%v", ids)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("回调没收到补的那条换号操作记录，或者回调里的 List 卡住了")
		}
	})
}

// 旧版明文凭据加撞号的假数据：两条都是 tun1，凭据都是明文。seq 手改成了 0：读进来时先抬到 1，换号后是 2，
// 加密版备份里该是抬过、没换号的 1（老版本这次启动写回去的那个），和文件里的 0、换号后的 2 都分得开。
const (
	plainCredA = "PLAIN-fake-key-AAAA-not-real"
	plainCredB = "PLAIN-fake-key-BBBB-not-real"
	plainDup   = `{"seq":0,"tunnels":[{"id":"tun1","provider":"natfrp","name":"甲","credential":"` + plainCredA +
		`","proxyId":"1","autoStart":false},{"id":"tun1","provider":"natfrp","name":"乙","credential":"` + plainCredB + `","proxyId":"2","autoStart":true}]}`
)

// noPlaintext 文件里不该有明文凭据。
func noPlaintext(t *testing.T, what, s string) {
	t.Helper()
	if strings.Contains(s, plainCredA) || strings.Contains(s, plainCredB) {
		t.Fatalf("%s里有明文凭据：%s", what, s)
	}
}

// sealedBackupOK 凭据加密过的那种备份：编号和 seq 是换号前的，凭据解得开、是原来的明文。
func sealedBackupOK(t *testing.T, bak string) {
	t.Helper()
	noPlaintext(t, "备份", bak)
	var d diskTunnels
	if err := json.Unmarshal([]byte(bak), &d); err != nil {
		t.Fatalf("备份解析不了：%v\n%s", err, bak)
	}
	if ids := diskIDs(d); !slices.Equal(ids, []string{"tun1", "tun1"}) || d.Seq != 1 {
		t.Fatalf("备份里的编号和 seq 应是换号前的：%v seq=%d", ids, d.Seq)
	}
	for i, want := range []string{plainCredA, plainCredB} {
		if got, err := secret.Unseal(d.Tunnels[i].Credential); err != nil || got != want {
			t.Fatalf("备份里第 %d 条的凭据解不开或不对：%q %v", i+1, got, err)
		}
	}
}

// 原文件里有旧版明文凭据、又撞了号：备份里不留明文，写成凭据加密后、编号和 seq 还是换号前的那一版（老版本这次启动会
// 写回去的样子），凭据解得开；和写回的文件比只差编号和 seq，凭据是同一份密文（只加密了一次）。程序日志和操作记录都说明
// 备份里的凭据加密过、不是原文件逐字节的样子。
func TestRenumberPlaintextNotKeptInBackup(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	if err := os.WriteFile(tunnelsPath(), []byte(plainDup), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1", "tun2"}) {
		t.Fatalf("编号：%v", ids)
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("应留一份备份：%v", got)
	}
	bak, written := readFile(t, filepath.Join(app.Base, bakName)), readFile(t, tunnelsPath())
	sealedBackupOK(t, bak)
	noPlaintext(t, "写回的 tunnels.json ", written)
	sameButIDs(t, bak, written)
	if !strings.Contains(logs.String(), "备份不是原文件逐字节的样子") {
		t.Fatalf("程序日志应说明备份里的凭据加密过：%s", logs.String())
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 || notes[0].Err != nil || notes[0].Args["backup"] != bakName || notes[0].Args["backupSealed"] != true {
		t.Fatalf("操作记录应带备份名，并说明备份里的凭据加密过：%+v", notes)
	}
}

// 明文加撞号、tunnels.json 只读：写不回去。这种备份每次加密的密文都不一样，认不出旧的那份，刚留的就删掉：启动几次、
// 期间保存失败几次，都不多出备份；操作记录说没写回去、不带备份名。去掉只读以后保存：先留一份（不含明文、编号是换号前的）
// 再写进去，补一条成功的换号操作记录；之后再保存不再多留。
func TestRenumberPlaintextReadOnlyLeavesNoBackup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读属性挡住覆盖是 Windows 的行为")
	}
	tempBase(t)
	bakName := pinBackupClock(t)
	if err := os.WriteFile(tunnelsPath(), []byte(plainDup), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tunnelsPath(), 0o644) })

	for i := 0; i < 2; i++ {
		m := NewManager()
		if ids := tunnelIDs(m); !slices.Equal(ids, []string{"tun1", "tun2"}) {
			t.Fatalf("第 %d 次启动：编号：%v", i+1, ids)
		}
		if got := backupNames(t); len(got) != 0 {
			t.Fatalf("第 %d 次启动：写不回去时不该留下备份：%v", i+1, got)
		}
		var notes []AutoAction
		m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
		if len(notes) != 1 || notes[0].Err == nil || notes[0].Args["backup"] != nil {
			t.Fatalf("第 %d 次启动：操作记录应说没写回去、不带备份名：%+v", i+1, notes)
		}
	}
	if readFile(t, tunnelsPath()) != plainDup {
		t.Fatal("只读的 tunnels.json 被改了")
	}

	m := NewManager()
	notes := make(chan AutoAction, 4)
	m.SetAutoHook(func(a AutoAction) { notes <- a })
	<-notes // 启动时那条：没写回去
	if _, err := m.Add(openfrpTunnel("还是只读")); err == nil {
		t.Fatal("还是只读，保存应失败")
	}
	if got := backupNames(t); len(got) != 0 {
		t.Fatalf("保存失败时也不该留下备份：%v", got)
	}

	if err := os.Chmod(tunnelsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	if s := logs.String(); !strings.Contains(s, "读 tunnels.json 时换过的重复编号这次写进去了") || !strings.Contains(s, bakName) ||
		!strings.Contains(s, "备份不是原文件逐字节的样子") {
		t.Fatalf("程序日志应说换好的编号这次写进去了、备份在哪、备份里的凭据加密过：%s", s)
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("写进去之前应先留一份备份：%v", got)
	}
	sealedBackupOK(t, readFile(t, filepath.Join(app.Base, bakName)))
	noPlaintext(t, "写回的 tunnels.json ", readFile(t, tunnelsPath()))
	d, _ := readDisk(t)
	if ids := diskIDs(d); !slices.Equal(ids, []string{"tun1", "tun2", "tun3"}) {
		t.Fatalf("写进去的编号：%v", ids)
	}
	a := nextNote(t, notes, "换好的编号写进去了，应补一条成功的换号操作记录")
	if a.Op != "tunnel.renumber" || a.Err != nil || a.Args["backup"] != bakName || a.Args["backupSealed"] != true {
		t.Fatalf("补的那条换号操作记录：%+v", a)
	}

	if _, err := m.Add(openfrpTunnel("再加一条")); err != nil {
		t.Fatal(err)
	}
	if got := backupNames(t); len(got) != 1 {
		t.Fatalf("写进去以后不该再留备份：%v", got)
	}
	select {
	case a := <-notes:
		t.Fatalf("写进去以后不该再记换号：%+v", a)
	case <-time.After(100 * time.Millisecond):
	}
}
