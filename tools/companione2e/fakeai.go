//go:build companione2e

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"
)

// fakeAI 假的 AI 服务（OpenAI 兼容、流式），按玩家的话决定调哪个伙伴工具：
//   - 「跟着我」→ companion_follow；「给 <玩家> N 个原木」→ companion_give（给别人）；「基岩」→ companion_give（minecraft:bedrock，
//     fetch：背包里没有、也弄不到）；「给我」→ companion_give（minecraft:oak_log ×1）；「去 X Z」→ companion_goto；
//     「招呼」→ companion_say（文字里故意带 § 格式码和换行）；「砍 N 个原木」→ companion_collect（minecraft:oak_log ×N）；
//     「停下」→ companion_stop；「状态」→ companion_status；「打村民」→ companion_attack（minecraft:villager）；别的直接回一句话。
//   - 请求里没有这个工具（没授权、带了 /问）时照样调用，模拟不听话的模型：MineDeck 必须在代码里拦住。
//   - 工具结果交回来以后，按结果回一句话。
//
// 每次回复的用量固定：输入 120、输出 30（共 150 token）。每个请求都记下来，供核对。
type fakeAI struct {
	srv *httptest.Server
	mu  sync.Mutex
	log []aiReq
}

// aiReq 一次请求。
type aiReq struct {
	T         time.Time `json:"t"`
	Tools     []string  `json:"tools"`
	System    string    `json:"-"`
	User      string    `json:"user"`      // 第一条用户消息
	ToolRound bool      `json:"toolRound"` // 最后一条是工具结果
	Results   []string  `json:"results,omitempty"`
	Reply     string    `json:"reply"`     // 回了什么（工具调用写成 tool:名字 参数）
	Companion bool      `json:"companion"` // 叫着伙伴的名字说的（记在 AI 伙伴名下）
}

const tokensPerReply = 150

func newFakeAI() *fakeAI {
	f := &fakeAI{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *fakeAI) URL() string { return f.srv.URL }
func (f *fakeAI) Close()      { f.srv.Close() }

func (f *fakeAI) Requests() []aiReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]aiReq(nil), f.log...)
}

func (f *fakeAI) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.log)
}

type oaMsg struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

func (m oaMsg) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(m.Content, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

var (
	countRe  = regexp.MustCompile(`(\d+)\s*个`)
	gotoRe   = regexp.MustCompile(`去\s*(-?\d+)\s+(-?\d+)`)
	giveToRe = regexp.MustCompile(`给\s*([A-Za-z0-9_]{1,16})\s*(\d+)\s*个原木`)
)

func (f *fakeAI) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []oaMsg `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	rec := aiReq{T: time.Now()}
	for _, t := range req.Tools {
		rec.Tools = append(rec.Tools, t.Function.Name)
	}
	for _, m := range req.Messages {
		switch {
		case m.Role == "system" && rec.System == "":
			rec.System = m.text()
		case m.Role == "user" && rec.User == "":
			rec.User = m.text()
		}
	}
	rec.Companion = strings.Contains(rec.User, "这句话是对伙伴")
	last := req.Messages[len(req.Messages)-1]
	var chunks []string
	if last.Role == "tool" {
		rec.ToolRound = true
		for i := len(req.Messages) - 1; i >= 0 && req.Messages[i].Role == "tool"; i-- {
			rec.Results = append(rec.Results, req.Messages[i].text())
		}
		all := strings.Join(rec.Results, "\n")
		text := "好的。"
		switch {
		case strings.Contains(all, "没有这个工具"):
			text = "这次我指挥不了伙伴：要服主授权才行。"
		case strings.Contains(all, "伙伴模组拒绝了"):
			text = "伙伴做不到这件事。"
		case strings.Contains(all, "开始"):
			text = "好的，伙伴开始干活了。"
		case strings.Contains(all, "已停下"), strings.Contains(all, "没有在做的任务"):
			text = "好的，已经让伙伴停下了。"
		case strings.Contains(all, "没有做"):
			text = "没做成。"
		}
		rec.Reply = text
		chunks = textChunks(text)
	} else {
		q := rec.User
		if _, after, ok := strings.Cut(q, "问题："); ok {
			q = after
		}
		name, args := "", ""
		switch {
		case strings.Contains(q, "跟着我"):
			name, args = "companion_follow", `{}`
		case giveToRe.MatchString(q):
			m := giveToRe.FindStringSubmatch(q)
			name, args = "companion_give", `{"item":"minecraft:oak_log","count":`+m[2]+`,"player":"`+m[1]+`"}`
		case strings.Contains(q, "基岩"):
			name, args = "companion_give", `{"item":"minecraft:bedrock","count":1,"fetch":true}`
		case strings.Contains(q, "给我"):
			name, args = "companion_give", `{"item":"minecraft:oak_log","count":1}`
		case gotoRe.MatchString(q):
			m := gotoRe.FindStringSubmatch(q)
			name, args = "companion_goto", `{"x":`+m[1]+`,"z":`+m[2]+`}`
		case strings.Contains(q, "招呼"):
			name, args = "companion_say", `{"text":"大家好§c，我是小木\n很高兴见到你们"}`
		case strings.Contains(q, "原木"):
			n := "1"
			if m := countRe.FindStringSubmatch(q); m != nil {
				n = m[1]
			}
			name, args = "companion_collect", `{"item":"minecraft:oak_log","count":`+n+`}`
		case strings.Contains(q, "停下"):
			name, args = "companion_stop", `{}`
		case strings.Contains(q, "状态"):
			name, args = "companion_status", `{"inventory":true}`
		case strings.Contains(q, "打村民"):
			name, args = "companion_attack", `{"mob":"minecraft:villager","count":1}`
		}
		if name == "" {
			rec.Reply = "你好！"
			chunks = textChunks(rec.Reply)
		} else {
			rec.Reply = "tool:" + name + " " + args
			chunks = toolChunks(fmt.Sprintf("call_%d", time.Now().UnixNano()), name, args)
		}
	}
	f.mu.Lock()
	f.log = append(f.log, rec)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func jsq(v any) string { b, _ := json.Marshal(v); return string(b) }

const usageChunk = `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`

func textChunks(text string) []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + jsq(text) + `},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		usageChunk,
	}
}

func toolChunks(id, name, args string) []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":` + jsq(id) + `,"type":"function","function":{"name":` + jsq(name) + `,"arguments":` + jsq(args) + `}}]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		usageChunk,
	}
}
