package catalog

import "testing"

// beta.3 R08：表单的默认端口从这里拿。生存服没写 server-port（服务端默认 25565），所以推荐 25566，并说 25565 被谁用着；
// 给自己换端口（except）时不算自己。只给界面的表单用，AI 调不了。
func TestPortSuggest(t *testing.T) {
	reg, _ := newCatalog(t)
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
