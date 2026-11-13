package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"minedeck/internal/ai"
)

// 思考片段：推出去的拼起来就是最后存下的全文；正文开始之前，攒着的思考已经全部推完（不留尾巴等整轮结束）。
func TestThinkingStreamOrderAndTotal(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	var chunks []string
	var want strings.Builder
	for i := 0; i < 30; i++ {
		s := "第" + string(rune('a'+i%26)) + "步。"
		want.WriteString(s)
		b, _ := json.Marshal(s)
		chunks = append(chunks, `{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":`+string(b)+`},"finish_reason":null}]}`)
	}
	chunks = append(chunks, textReply("答案是 42。")...)
	f.ai.queue(chunks)

	_, ch, cancel, err := f.as.Subscribe(local, id)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := f.as.Send(local, id, "想一想", false); err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	sawDelta := false
	finalThink := ""
	deadline := time.After(5 * time.Second)
	for finalThink == "" {
		select {
		case b, ok := <-ch:
			if !ok {
				t.Fatal("事件流被关了")
			}
			var ev struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Item *Item  `json:"item"`
			}
			if err := json.Unmarshal(b, &ev); err != nil {
				t.Fatal(err)
			}
			switch ev.Type {
			case "thinking":
				if sawDelta {
					t.Errorf("正文开始以后还在推思考：%q", ev.Text)
				}
				streamed.WriteString(ev.Text)
			case "delta":
				sawDelta = true
				if streamed.String() != want.String() {
					t.Errorf("正文开始时思考还没推完：已推 %q", streamed.String())
				}
			case "item":
				if ev.Item != nil && ev.Item.Kind == "assistant" && ev.Item.Text != "" {
					finalThink = ev.Item.Think
				}
			}
		case <-deadline:
			t.Fatalf("等不到回复结束；已推的思考 %q", streamed.String())
		}
	}
	if streamed.String() != want.String() || finalThink != want.String() {
		t.Fatalf("推出去的思考和存下的不一致：\n推 %q\n存 %q\n应 %q", streamed.String(), finalThink, want.String())
	}
}

// 中途订阅：快照里已有的思考，之后不会再作为事件推一遍。
func TestThinkingSnapshotNoDuplicate(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.hold, f.ai.holdAt = make(chan struct{}), 1 // 停在第二块之后：第二块的思考还攒在缓冲里没推
	first := `{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"先看日志。"},"finish_reason":null}]}`
	rest := `{"choices":[{"index":0,"delta":{"reasoning_content":"再看配置。"},"finish_reason":null}]}`
	f.ai.queue(append([]string{first, rest}, textReply("好了。")...))
	if err := f.as.Send(local, id, "看看", false); err != nil {
		t.Fatal(err)
	}
	// 第一块的思考已经推出、第二块还攒着（相隔不到 100ms），这时订阅
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		v, err := f.as.Get(local, id)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(v.Items); n > 0 && v.Items[n-1].Kind == "assistant" && v.Items[n-1].Think != "" {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("等不到第一段思考：%s", dump(v))
		}
	}
	time.Sleep(30 * time.Millisecond) // 让第二块也到（它会攒在缓冲里，因为离上一次推送不到 100ms）
	v, ch, cancel, err := f.as.Subscribe(local, id)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	have := ""
	for _, it := range v.Items {
		if it.Kind == "assistant" {
			have = it.Think
		}
	}
	close(f.ai.hold)
	f.ai.hold = nil
	var later strings.Builder
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case b := <-ch:
			var ev struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Item *Item  `json:"item"`
			}
			_ = json.Unmarshal(b, &ev)
			if ev.Type == "thinking" {
				later.WriteString(ev.Text)
			}
			if ev.Type == "item" && ev.Item != nil && ev.Item.Kind == "assistant" && ev.Item.Text != "" {
				done = true
			}
		case <-deadline:
			t.Fatal("等不到回复结束")
		}
	}
	if have+later.String() != "先看日志。再看配置。" {
		t.Fatalf("快照 %q + 之后的事件 %q 应正好拼成全文（不重复、不缺）", have, later.String())
	}
}

func TestCapThink(t *testing.T) {
	if capThink("短") != "短" {
		t.Fatal("短的不该动")
	}
	long := strings.Repeat("思", maxThink) // 每个字 3 字节，远超上限
	got := capThink(long)
	if len(got) > maxThink+64 || !strings.HasPrefix(got, "……（前面的思考太长") || !strings.HasSuffix(got, "思") {
		t.Fatalf("截断不对：长度 %d，开头 %q", len(got), got[:40])
	}
	if !strings.Contains(got[len("……（前面的思考太长，没有保存）\n"):], "思") || strings.ContainsRune(got, '�') {
		t.Fatal("截断切坏了汉字")
	}
}

// 请求失败：提示的正文只写结论（Hint），带状态码和英文原话的原文放进 Detail（界面上「复制原文」）。
func TestFailNoticeReadable(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue([]string{`!401 {"error":{"message":"Incorrect API key provided: sk-abc***","type":"invalid_request_error"}}`})
	_ = f.as.Send(local, id, "在吗", false)
	v := f.wait(id, func(v *View) bool { return strings.Contains(notices(v), "请求 AI 失败") })
	var n *Item
	for _, it := range v.Items {
		if it.Kind == "notice" {
			n = it
		}
	}
	if n == nil || n.Text != "请求 AI 失败：Key 不对或已失效" || !strings.Contains(n.Detail, "HTTP 401") || strings.Contains(n.Text, "HTTP") {
		t.Fatalf("报错提示：%+v", n)
	}
}

func TestFailText(t *testing.T) {
	for _, c := range []struct {
		err          error
		text, detail string
	}{
		{errors.New("还没选模型"), "请求 AI 失败：还没选模型", ""},
		{errors.New("unexpected EOF"), "请求 AI 失败", "unexpected EOF"},
		{&ai.APIError{Status: 429, Message: "Rate limit reached"}, "请求 AI 失败：请求太频繁，等一会儿再试", "x"},
		// Codex 第二轮：说了一半断开的，提示别丢，也别说成「请求失败」
		{fmt.Errorf("%w：%w", ai.ErrDisconnected, &ai.APIError{Status: 400, Message: "maximum context length is 8192 tokens"}), "回复中途断开了：对话太长，超过了模型能读的长度。开一个新会话再问", "x"},
		{fmt.Errorf("%w：没有收到 message_stop", ai.ErrDisconnected), "回复中途断开了", "x"},
		// 短的中文原文直接显示（Key 只有三个字母，不算英文单词）；夹着英文的放进「复制原文」；网址不算英文
		{errors.New("先填写 Key"), "请求 AI 失败：先填写 Key", ""},
		{errors.New("连不上服务商：dial tcp: connection refused"), "请求 AI 失败", "x"},
		{errors.New("连不上 https://api.example.com/v1，检查地址"), "请求 AI 失败：连不上 https://api.example.com/v1，检查地址", ""},
		{errors.New(strings.Repeat("长", 61)), "请求 AI 失败", "x"},
	} {
		text, detail := failText(c.err, c.err.Error())
		if text != c.text || (c.detail == "" && detail != "") || (c.detail != "" && detail == "") {
			t.Errorf("%v：%q %q", c.err, text, detail)
		}
	}
}

// 只有思考时点停止：这一条撤掉（不留下停在「思考中…」的空消息），提问撤回（界面放回输入框）。前端假后端照这个契约做。
func TestStopDuringThinkingRemovesItem(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.hold = make(chan struct{})
	defer close(f.ai.hold)
	f.ai.queue([]string{`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"先想想……"},"finish_reason":null}]}`, `{"choices":[{"index":0,"delta":{"content":"好"},"finish_reason":"stop"}]}`})
	_ = f.as.Send(local, id, "服务器有点卡", false)
	for end := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		v, _ := f.as.Get(local, id)
		if n := len(v.Items); n > 0 && v.Items[n-1].Think != "" {
			break
		}
		if time.Now().After(end) {
			t.Fatal("等不到思考")
		}
	}
	if err := f.as.Stop(local, id); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return strings.Contains(notices(v), "已停止") })
	for _, it := range v.Items {
		if it.Kind == "assistant" {
			t.Fatalf("只有思考的那一条应撤掉：%s", dump(v))
		}
	}
	if !v.Items[0].Withdrawn {
		t.Errorf("提问应撤回：%s", dump(v))
	}
}
