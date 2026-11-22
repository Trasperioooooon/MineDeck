package catalog

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// noListeners 推荐端口的用例不看这台电脑上真有哪些程序在监听（机器上碰巧有程序占着 25565、25566 时结果会变）
func noListeners(t *testing.T, fake map[int]int) {
	t.Helper()
	old := listenPorts
	listenPorts = func() map[int]int { return fake }
	t.Cleanup(func() { listenPorts = old })
}

// beta.3 R08：表单的默认端口从这里拿。生存服没写 server-port（服务端默认 25565），所以推荐 25566，并说 25565 被谁用着；
// 给自己换端口（except）时不算自己。只给界面的表单用，AI 调不了。
func TestPortSuggest(t *testing.T) {
	reg, _ := newCatalog(t)
	noListeners(t, map[int]int{})
	var r struct {
		Port   int    `json:"port"`
		From   int    `json:"from"`
		UsedBy string `json:"usedBy"`
		Used   []struct {
			Port     int    `json:"port"`
			Instance string `json:"instance"`
			Status   string `json:"status"`
		} `json:"used"`
	}
	if err := do(t, reg, human, "port.suggest", `{}`, &r); err != nil {
		t.Fatal(err)
	}
	if r.Port != 25566 || r.From != 25565 || r.UsedBy != "生存服" || len(r.Used) != 1 || r.Used[0].Port != 25565 || r.Used[0].Instance != "生存服" || r.Used[0].Status != "stopped" {
		t.Errorf("推荐的端口：%+v", r)
	}
	r.UsedBy = ""
	if err := do(t, reg, human, "port.suggest", `{"from":25565,"except":"生存服"}`, &r); err != nil || r.Port != 25565 || r.UsedBy != "" {
		t.Errorf("不算自己时 25565 是空的：%v %+v", err, r)
	}
	if err := do(t, reg, human, "port.suggest", `{"from":30000}`, &r); err != nil || r.Port != 30000 || r.From != 30000 {
		t.Errorf("从 30000 起：%v %+v", err, r)
	}
	if err := do(t, reg, bot, "port.suggest", `{}`, nil); err == nil {
		t.Error("AI 不该调得到 port.suggest")
	}
	// 表单每打开一次就问一次端口，详情页每换一次标签就查一次没装上的文件：都是只读，不该记进操作记录
	if err := do(t, reg, human, "instance.import.pending.get", `{"inst":"生存服"}`, nil); err != nil {
		t.Fatal(err)
	}
	var log []map[string]any
	if err := do(t, reg, human, "ops.log", `{}`, &log); err != nil {
		t.Fatal(err)
	}
	for _, e := range log {
		if e["op"] == "port.suggest" || e["op"] == "instance.import.pending.get" {
			t.Errorf("只读的查询不该记进操作记录：%v", e)
		}
	}
}

// beta.3 之后（回访走查 11-16）：默认端口也跳过被别的程序占着的，并说 from 被哪个程序占着；本软件自己那台开着的服务器
// 也在监听表里，那种只说 usedBy，不算别的程序
func TestPortSuggestSkipsOtherPrograms(t *testing.T) {
	reg, _ := newCatalog(t)
	self := os.Getpid()
	type res struct {
		Port        int    `json:"port"`
		From        int    `json:"from"`
		UsedBy      string `json:"usedBy"`
		FromBusy    bool   `json:"fromBusy"`
		FromProcess string `json:"fromProcess"`
	}
	// 25566、25567 被别的程序占着：生存服用着 25565，推荐 25568；from 是 25565，被生存服用着，不算别的程序
	noListeners(t, map[int]int{25565: self, 25566: self, 25567: self})
	var r res
	if err := do(t, reg, human, "port.suggest", `{}`, &r); err != nil || r.Port != 25568 || r.UsedBy != "生存服" || r.FromBusy {
		t.Errorf("跳过别的程序占着的：%v %+v", err, r)
	}
	// from 本身被别的程序占着：fromBusy，带上程序名（这里是测试程序自己）
	r = res{}
	if err := do(t, reg, human, "port.suggest", `{"from":25566}`, &r); err != nil || r.Port != 25568 || !r.FromBusy || r.UsedBy != "" ||
		!strings.EqualFold(r.FromProcess, filepath.Base(os.Args[0])) {
		t.Errorf("from 被别的程序占着：%v %+v（应为 %s）", err, r, filepath.Base(os.Args[0]))
	}
	// 没人占着：照旧
	r = res{}
	if err := do(t, reg, human, "port.suggest", `{"from":30000}`, &r); err != nil || r.Port != 30000 || r.FromBusy || r.FromProcess != "" {
		t.Errorf("空着的：%v %+v", err, r)
	}
	// 从起点往上到 65535 全被占满时（兜底分支）：不能越界，照原样返回起点并标出 fromBusy
	noListeners(t, map[int]int{65534: self, 65535: self})
	r = res{}
	if err := do(t, reg, human, "port.suggest", `{"from":65534}`, &r); err != nil || r.Port != 65534 || r.From != 65534 || !r.FromBusy {
		t.Errorf("从 65534 往上全满时应兜底返回起点 65534：%v %+v", err, r)
	}
}

// 真读一次系统的监听表：自己在非临时高端口段（30000..40000）监听一个回环端口，只断言自己绑住的这个端口能被识别并跳过，
// 不二次扫描整张监听表跟别的进程抢端口，避免并发跑测试时偶发误报。
func TestPortSuggestReadsRealListenTable(t *testing.T) {
	reg, _ := newCatalog(t)
	var (
		ln   net.Listener
		busy int
		err  error
	)
	for p := 30000; p <= 40000; p++ {
		ln, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			busy = p
			break
		}
	}
	if ln == nil {
		t.Fatalf("30000..40000 没有可绑定的回环端口：%v", err)
	}
	defer ln.Close()
	if pid, ok := tcpListeners()[busy]; !ok || pid != os.Getpid() {
		t.Fatalf("监听表里没有自己占着的 %d：%v %v", busy, pid, ok)
	}
	var r struct {
		Port        int    `json:"port"`
		From        int    `json:"from"`
		FromBusy    bool   `json:"fromBusy"`
		FromProcess string `json:"fromProcess"`
	}
	if err := do(t, reg, human, "port.suggest", `{"from":`+strconv.Itoa(busy)+`}`, &r); err != nil ||
		r.From != busy || !r.FromBusy || !strings.EqualFold(r.FromProcess, filepath.Base(os.Args[0])) || r.Port <= busy || r.Port > 65535 {
		t.Errorf("应识别并跳过自己占着的 %d：%v %+v", busy, err, r)
	}
}
