package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/assistant"
	"minedeck/internal/ops"
)

// ---------- 第三阶段：应用内 AI 助手、提案的确认接口、排错和修复用的实例接口 ----------

// fakeLLM 本机的假 AI 服务（OpenAI 兼容，流式）：按顺序交出准备好的回复，记下每次的请求体。
type fakeLLM struct {
	srv     *httptest.Server
	mu      sync.Mutex
	replies [][]string
	bodies  []string
}

func (f *fakeLLM) queue(rs ...[]string) {
	f.mu.Lock()
	f.replies = append(f.replies, rs...)
	f.mu.Unlock()
}

func (f *fakeLLM) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...)
}

func jsStr(v any) string { b, _ := json.Marshal(v); return string(b) }

const llmUsage = `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`

func llmText(text string) []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + jsStr(text) + `},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, llmUsage,
	}
}

func llmTool(id, name, args string) []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":` + jsStr(id) +
			`,"type":"function","function":{"name":` + jsStr(name) + `,"arguments":` + jsStr(args) + `}}]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, llmUsage,
	}
}

// useFakeLLM 把 AI 设成本机的假服务（自定义服务商、OpenAI 兼容协议），只放行它的地址。
func (e *fullEnv) useFakeLLM(t *testing.T) *fakeLLM {
	t.Helper()
	f := &fakeLLM{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(b))
		chunks := llmText("（测试里没有准备更多回复）")
		if len(f.replies) > 0 {
			chunks, f.replies = f.replies[0], f.replies[1:]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(f.srv.Close)
	t.Cleanup(e.idle) // 先于假服务关掉、临时目录清理：等后台的回复跑完
	e.dials.mu.Lock()
	e.dials.allow = strings.TrimPrefix(f.srv.URL, "http://")
	e.dials.mu.Unlock()
	e.call(t, "PUT", "/api/ai/provider", `{"preset":"custom","protocol":"openai","baseUrl":"`+f.srv.URL+`/v1","model":"test-model"}`, 200)
	return f
}

// idle 等所有会话都不忙了。
func (e *fullEnv) idle() {
	local := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		l, err := e.asst.List(local)
		busy := false
		for _, s := range l {
			busy = busy || s.Busy
		}
		if err != nil || !busy {
			return
		}
	}
}

// canceled 请求的上下文先取消：SSE 推完第一份就返回。
var canceled reqOpt = func(r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	*r = *r.WithContext(ctx)
}

type sessResp struct {
	Data assistant.View `json:"data"`
}

func (e *fullEnv) newSession(t *testing.T, inst string, opts ...reqOpt) string {
	t.Helper()
	var v sessResp
	if err := json.Unmarshal(e.call(t, "POST", "/api/assistant/sessions", `{"inst":`+jsStr(inst)+`}`, 200, opts...).Body.Bytes(), &v); err != nil || v.Data.ID == "" {
		t.Fatalf("开会话：%v %+v", err, v)
	}
	return v.Data.ID
}

// settle 等会话的这一轮回复结束，返回会话。
func (e *fullEnv) settle(t *testing.T, id string, opts ...reqOpt) assistant.View {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var v sessResp
		_ = json.Unmarshal(e.call(t, "GET", "/api/assistant/sessions/"+id, "", 200, opts...).Body.Bytes(), &v)
		if !v.Data.Busy {
			return v.Data
		}
		if time.Now().After(deadline) {
			t.Fatal("会话一直在回复")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type propResp struct {
	Data []struct {
		ID     string          `json:"id"`
		Op     string          `json:"op"`
		Args   json.RawMessage `json:"args"`
		Caller ops.Caller      `json:"caller"`
		Plan   struct {
			Diff string `json:"diff"`
		} `json:"plan"`
	} `json:"data"`
}

func (e *fullEnv) proposals(t *testing.T, q string, opts ...reqOpt) propResp {
	t.Helper()
	var p propResp
	if err := json.Unmarshal(e.call(t, "GET", "/api/ops/proposals"+q, "", 200, opts...).Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// lastText 会话里最后一条 AI 回复的文字。
func lastText(v assistant.View) string {
	for k := len(v.Items) - 1; k >= 0; k-- {
		if v.Items[k].Kind == "assistant" && v.Items[k].Text != "" {
			return v.Items[k].Text
		}
	}
	return ""
}

// 第三阶段新增的 27 个接口逐个调一遍：状态码、错误信息、响应结构。会请求 AI、起进程、下载的都挑了在那之前就返回的参数。
// 没配置 AI 时不能开会话，本地排错（横幅的数据来源）照样能用。
func TestAssistantRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服
	var st struct{ Data assistant.Status }
	_ = json.Unmarshal(e.call(t, "GET", "/api/assistant", "", 200).Body.Bytes(), &st)
	if st.Data.Enabled || st.Data.Allowed || !st.Data.Local || !st.Data.LAN || st.Data.Missing == "" {
		t.Errorf("没配置 AI 时的状态：%+v", st.Data)
	}
	e.call(t, "GET", "/api/assistant/sessions", "", 200) // 历史照样能看、能清空，只是不能开会话、发消息
	e.call(t, "POST", "/api/assistant/sessions", `{"inst":"生存服"}`, 400)
	var rep struct {
		Data struct {
			Findings []any `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", ip+"/diag", "", 200).Body.Bytes(), &rep); err != nil {
		t.Errorf("没配置 AI 时本地排错应照样能用：%v", err)
	}

	f := e.useFakeLLM(t)
	sid := e.newSession(t, "生存服")
	sp := "/api/assistant/sessions/" + sid
	cases := []struct {
		method, path, body string
		code               int
		errHas             string
		opts               []reqOpt
	}{
		{"GET", sp, "", 200, "", nil},
		{"GET", sp, "", 404, "会话不存在", []reqOpt{fromLAN}}, // 局域网的设备看不见本机开的会话
		{"GET", sp + "/events", "", 200, "", []reqOpt{canceled}},
		{"POST", sp + "/messages", `{"text":"  "}`, 400, "消息是空的", nil},
		{"POST", sp + "/messages", `{"attach":true}`, 409, "排错资料已经过期", nil},
		{"POST", sp + "/context", "", 200, "", nil},
		{"POST", sp + "/continue", "", 409, "没有要继续的回复", nil},
		{"POST", sp + "/stop", "", 200, "", nil},
		{"GET", sp + "/sent?item=nope", "", 404, "没有这一条", nil},
		{"DELETE", "/api/assistant/sessions/nope", "", 404, "会话不存在", nil},
		{"GET", "/api/ops/proposals", "", 200, "", nil},
		{"POST", "/api/ops/proposals/p_nope/confirm", `{"ack":true}`, 404, "提案不存在", nil},
		{"POST", "/api/ops/proposals/p_nope/reject", `{"reason":"不要"}`, 404, "提案不存在", nil},
		{"PUT", "/api/ai/assistant", `{"lan":false}`, 403, "仅支持在本机修改", []reqOpt{fromLAN}},
		{"PUT", "/api/ai/assistant", `{"lan":true}`, 200, "", nil},
		{"GET", ip + "/console/tail?lines=50&level=warn", "", 200, "", nil},
		{"GET", ip + "/console/tail?lines=abc", "", 400, "参数 lines", nil},
		{"GET", ip + "/logs", "", 404, "还没有 logs/latest.log", nil},
		{"GET", ip + "/crash-reports", "", 200, "", nil},
		{"GET", ip + "/crash-reports?file=..%2Fx.txt", "", 400, "只能读 crash-reports 里的", nil},
		{"GET", ip + "/mods", "", 400, "没有模组或插件目录", nil},
		{"POST", ip + "/mods/toggle", `{"file":"a.jar","enable":false}`, 400, "没有模组或插件目录", nil},
		{"PUT", ip + "/config", `{"path":"../x.toml","mode":"replace","content":"a = 1"}`, 400, "路径不对", nil},
		{"PUT", ip + "/config", `{"path":"config/minedeck.toml","mode":"replace","content":"a = 1\n"}`, 200, "", nil},
		{"GET", ip + "/config", "", 200, "", nil},
		{"GET", ip + "/config?path=config/minedeck.toml", "", 200, "", nil},
		{"PUT", ip + "/java", `{"major":5}`, 400, "参数 major 应在 8 到 30 之间", nil},
		{"POST", ip + "/restart", "", 400, "服务器未在运行", nil},
		{"GET", "/api/ports/25565?proto=tcp", "", 200, "", nil},
		{"GET", "/api/ports/abc", "", 400, "端口不对", nil},
		{"DELETE", sp, "", 200, "", nil},
		{"DELETE", "/api/assistant/sessions", "", 200, "", nil},
	}
	pattern := func(p string) string {
		p = strings.Split(p, "?")[0]
		p = strings.Replace(p, ip, "/api/instances/{name}", 1)
		p = strings.Replace(p, sid, "{id}", 1)
		for _, s := range []string{"/api/assistant/sessions/nope", "/api/ops/proposals/p_nope"} {
			p = strings.Replace(p, s, s[:strings.LastIndex(s, "/")]+"/{id}", 1)
		}
		if strings.HasPrefix(p, "/api/ports/") {
			p = "/api/ports/{port}"
		}
		return p
	}
	seen := map[string]bool{"GET /api/assistant": true, "GET /api/assistant/sessions": true, "GET /api/instances/{name}/diag": true, "POST /api/assistant/sessions": true}
	for _, c := range cases {
		rec := e.call(t, c.method, c.path, c.body, c.code, c.opts...)
		seen[c.method+" "+pattern(c.path)] = true
		if strings.HasSuffix(c.path, "/events") {
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") || !strings.HasPrefix(rec.Body.String(), "data: {") ||
				!strings.Contains(rec.Body.String(), `"type":"session"`) || !strings.Contains(rec.Body.String(), sid) {
				t.Errorf("events 应为 SSE，先推一份完整的会话：%q %s", rec.Header().Get("Content-Type"), rec.Body)
			}
			continue
		}
		var resp struct {
			OK    bool            `json:"ok"`
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Errorf("%s %s：响应不是 JSON：%s", c.method, c.path, rec.Body)
			continue
		}
		if resp.OK != (c.code == 200) || (c.code == 200 && len(resp.Data) == 0) {
			t.Errorf("%s %s：响应结构不对：%s", c.method, c.path, rec.Body)
		}
		if c.errHas != "" && !strings.Contains(resp.Error, c.errHas) {
			t.Errorf("%s %s：错误「%s」里应有「%s」", c.method, c.path, resp.Error, c.errHas)
		}
	}
	if n := len(seen); n != 27 {
		t.Errorf("覆盖了 %d 个接口，应为 27：%v", n, seen)
	}
	if r := f.requests(); len(r) != 0 {
		t.Errorf("这些请求都不该调 AI：%d 次", len(r))
	}
	if b, err := os.ReadFile(filepath.Join(app.ServersDir, "s1", "config", "minedeck.toml")); err != nil || string(b) != "a = 1\n" {
		t.Errorf("本机直接改配置文件（人操作不用确认卡片）：%q %v", b, err)
	}
}

// 本机会话里 AI 先读后写：读的直接执行、不弹确认；写的生成提案，没确认之前文件不动；局域网的设备看不见、也确认不了
// 本机会话的提案；本机确认后按提案里的参数原样执行，结果交还给 AI 接着回复；提出和执行都记进操作记录。
func TestAssistantProposalFlow(t *testing.T) {
	e := newFullEnv(t)
	f := e.useFakeLLM(t)
	conf := filepath.Join(app.ServersDir, "s1", "config", "minedeck.toml")
	args := `{"inst":"生存服","path":"config/minedeck.toml","mode":"replace","content":"view-distance = 8\n"}`
	f.queue(
		llmTool("c1", "instance_props_get", `{"inst":"生存服"}`),
		llmTool("c2", "instance_config_write", args),
		llmText("改好了。"),
	)
	sid := e.newSession(t, "生存服", fromUI)
	e.call(t, "POST", "/api/assistant/sessions/"+sid+"/messages", `{"text":"把视距改成 8"}`, 200, fromUI)
	v := e.settle(t, sid)
	if !v.Waiting || len(f.requests()) != 2 {
		t.Fatalf("应停在确认卡片上：waiting=%v，请求了 %d 次", v.Waiting, len(f.requests()))
	}
	var tools []*assistant.ToolItem
	for _, it := range v.Items {
		tools = append(tools, it.Tools...)
	}
	if len(tools) != 2 || tools[0].Status != "done" || tools[0].Proposal != nil || tools[1].Status != "pending" || tools[1].Proposal == nil {
		t.Fatalf("读的直接执行，写的等确认：%s", jsStr(tools))
	}
	if _, err := os.Stat(conf); err == nil {
		t.Fatal("没确认的写操作执行了")
	}
	p := e.proposals(t, "?session="+sid)
	if len(p.Data) != 1 || p.Data[0].Op != "instance.config.write" || p.Data[0].Caller.Source != ops.SrcAssistant || !strings.Contains(p.Data[0].Plan.Diff, "+ view-distance = 8") {
		t.Fatalf("提案：%+v", p.Data)
	}
	pid := p.Data[0].ID
	if l := e.proposals(t, "", fromLAN); len(l.Data) != 0 {
		t.Errorf("局域网的设备不该看见本机会话的提案：%+v", l.Data)
	}
	if !strings.Contains(e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 403, fromLAN).Body.String(), "只能在主机上处理") {
		t.Error("局域网的设备不能确认本机会话的提案")
	}
	if _, err := os.Stat(conf); err == nil {
		t.Fatal("被拒绝的确认执行了")
	}

	e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 200, fromUI)
	if b, err := os.ReadFile(conf); err != nil || string(b) != "view-distance = 8\n" {
		t.Fatalf("应按提案里的参数原样执行：%q %v", b, err)
	}
	v = e.settle(t, sid)
	if lastText(v) != "改好了。" || v.Waiting || len(f.requests()) != 3 {
		t.Fatalf("确认后应接着回复：%q waiting=%v 请求 %d 次", lastText(v), v.Waiting, len(f.requests()))
	}
	if r := f.requests()[2]; !strings.Contains(r, `"tool_call_id":"c2"`) {
		t.Errorf("第三次请求应带着写入的结果：%s", r)
	}
	if again := e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 404, fromUI); !strings.Contains(again.Body.String(), "已经处理过了") {
		t.Errorf("同一个提案不能确认两次：%s", again.Body)
	}

	var logs struct {
		Data []ops.Record `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/ops/log?inst=%E7%94%9F%E5%AD%98%E6%9C%8D", "", 200).Body.Bytes(), &logs)
	var got []string
	for _, r := range logs.Data {
		if r.Op == "instance.config.write" && r.Src == ops.SrcAssistant {
			got = append(got, fmt.Sprintf("%s|%s|%s|%v", r.Event, r.Proposal, r.ApprovedBy, r.OK))
		}
	}
	if want := []string{"|" + pid + "|本机|true", "propose|" + pid + "||true"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("操作记录：%v，应为 %v", got, want)
	}
}

// 局域网的设备：只看得见局域网开的会话，能处理这些会话的提案；主机关掉「局域网可用 AI 助手」后，局域网的设备
// 不能用助手、看不见也处理不了提案（本机照常）；开关只能在本机改。拒绝的理由交还给 AI，文件不动。
func TestAssistantLAN(t *testing.T) {
	e := newFullEnv(t)
	f := e.useFakeLLM(t)
	f.queue(llmTool("c1", "instance_props_set", `{"inst":"生存服","pairs":[{"key":"max-players","value":"8"}]}`), llmText("好，先不改。"))
	lanSid := e.newSession(t, "生存服", fromLAN)
	localSid := e.newSession(t, "", fromUI)
	count := func(opts ...reqOpt) int {
		var l struct{ Data []assistant.Summary }
		_ = json.Unmarshal(e.call(t, "GET", "/api/assistant/sessions", "", 200, opts...).Body.Bytes(), &l)
		return len(l.Data)
	}
	if n, m := count(fromLAN), count(); n != 1 || m != 2 {
		t.Errorf("局域网看见 %d 个会话（应为 1），本机看见 %d 个（应为 2）", n, m)
	}
	e.call(t, "GET", "/api/assistant/sessions/"+localSid, "", 404, fromLAN)

	e.call(t, "POST", "/api/assistant/sessions/"+lanSid+"/messages", `{"text":"人数改成 8"}`, 200, fromLAN)
	if v := e.settle(t, lanSid, fromLAN); !v.Waiting {
		t.Fatalf("应停在确认卡片上：%+v", v)
	}
	p := e.proposals(t, "", fromLAN)
	if len(p.Data) != 1 || p.Data[0].Op != "instance.props.set" {
		t.Fatalf("局域网的设备应看得见自己会话的提案：%+v", p.Data)
	}
	pid := p.Data[0].ID

	e.call(t, "PUT", "/api/ai/assistant", `{"lan":false}`, 200, fromUI)
	var st struct{ Data assistant.Status }
	_ = json.Unmarshal(e.call(t, "GET", "/api/assistant", "", 200, fromLAN).Body.Bytes(), &st)
	if st.Data.Allowed || st.Data.LAN || !st.Data.Enabled {
		t.Errorf("关掉之后局域网的状态：%+v", st.Data)
	}
	if !strings.Contains(e.call(t, "GET", "/api/assistant/sessions", "", 403, fromLAN).Body.String(), "主机关闭了「局域网可用 AI 助手」") {
		t.Error("关掉之后局域网的设备不能用助手")
	}
	if l := e.proposals(t, "", fromLAN); len(l.Data) != 0 {
		t.Errorf("关掉之后局域网的设备看不见提案：%+v", l.Data)
	}
	e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 403, fromLAN)
	e.call(t, "POST", "/api/ops/proposals/"+pid+"/reject", `{}`, 403, fromLAN)
	e.call(t, "PUT", "/api/ai/assistant", `{"lan":true}`, 403, fromLAN)
	if count() != 2 {
		t.Error("本机照常能用")
	}

	e.call(t, "POST", "/api/ops/proposals/"+pid+"/reject", `{"reason":"人数先别改"}`, 200, fromUI)
	v := e.settle(t, lanSid)
	if lastText(v) != "好，先不改。" || len(f.requests()) != 2 || !strings.Contains(f.requests()[1], "人数先别改") {
		t.Fatalf("拒绝的理由应交还给 AI：%q，请求 %d 次", lastText(v), len(f.requests()))
	}
	if b, _ := os.ReadFile(filepath.Join(app.ServersDir, "s1", "server.properties")); !strings.Contains(string(b), "max-players=20") {
		t.Errorf("拒绝了还改了文件：%s", b)
	}
	e.call(t, "PUT", "/api/ai/assistant", `{"lan":true}`, 200)
	_ = json.Unmarshal(e.call(t, "GET", "/api/assistant", "", 200, fromLAN).Body.Bytes(), &st)
	if !st.Data.Allowed {
		t.Errorf("重新打开后局域网能用：%+v", st.Data)
	}
}
