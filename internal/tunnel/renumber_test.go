package tunnel

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
// 会被标成「凭据解不开」），能解开的那条是现场加密的一句假话。

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

// dupFixture 一份撞了号的 tunnels.json（带 BOM、压成一行、seq 比现有编号小），第一条的凭据是现场加密的假话。
// 返回原文和五条隧道按顺序的凭据。读进来以后编号应该是 tun1…tun5、seq 是 5：
// 「语音」tun1→tun3、「自建」tun2→tun4、「第三个一号」tun1→tun5。
func dupFixture(t *testing.T) (orig string, creds []string) {
	t.Helper()
	sealed, err := secret.Seal("fake-cred-一号-not-real")
	if err != nil {
		t.Fatal(err)
	}
	creds = []string{sealed, fakeSealedA, fakeSealedB, "", fakeSealedC}
	orig = "\xef\xbb\xbf" + `{"seq":1,"tunnels":[` +
		`{"id":"tun1","provider":"openfrp","name":"游戏一","credential":` + jsonStr(t, sealed) + `,"proxyId":"101","boundInstance":"S1","autoStart":true},` +
		`{"id":"tun2","provider":"natfrp","name":"游戏二","credential":"` + fakeSealedA + `","proxyId":"102","autoStart":false},` +
		`{"id":"tun1","provider":"natfrp","name":"语音","credential":"` + fakeSealedB + `","proxyId":"103","boundInstance":"S1","autoStart":true,"purpose":"voice"},` +
		`{"id":"tun2","provider":"custom","name":"自建","serverAddr":"frp.example.com","serverPort":7000,"remotePort":25566,"localPort":25565,"autoStart":false,"proto":"tcp"},` +
		`{"id":"tun1","provider":"openfrp","name":"第三个一号","credential":"` + fakeSealedC + `","proxyId":"104","autoStart":false}` +
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
		a.Args["backup"] != bakName || !slices.Equal(got, want) {
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
