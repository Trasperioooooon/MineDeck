package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEEventBudgetCountsAllLines(t *testing.T) {
	for _, field := range []string{"data: x\n", "event: x\n", ": keepalive\n", "unknown: x\n"} {
		r := newSSEReader(strings.NewReader(strings.Repeat(field, 100) + "\n"))
		r.eventMax = 32
		if _, err := r.next(); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("%q 多行事件没有受限：%v", field, err)
		}
	}
	r := newSSEReader(strings.NewReader("data: one\ndata: two\n\n"))
	r.eventMax = 20
	ev, err := r.next()
	if err != nil || string(ev.Data) != "one\ntwo" {
		t.Fatalf("正常多行事件：%q %v", ev.Data, err)
	}
}

func TestResponseBudgetExactLimitAndSharedReads(t *testing.T) {
	for _, tc := range []struct {
		data string
		over bool
	}{{"12345", false}, {"123456", true}} {
		r := &responseReader{r: strings.NewReader(tc.data), left: 5}
		b, err := io.ReadAll(r)
		if errors.Is(err, ErrResponseTooLarge) != tc.over {
			t.Fatalf("%q：%q %v", tc.data, b, err)
		}
		if !tc.over && string(b) != tc.data {
			t.Fatal("正好到上限的响应被截断")
		}
	}
	budget := &responseBudget{left: 5}
	if _, err := io.ReadAll(&responseReader{r: strings.NewReader("123"), left: 10, turn: budget}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(&responseReader{r: strings.NewReader("456"), left: 10, turn: budget}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("第二个响应应共用额度：%v", err)
	}
}

func TestResponseBudgetSurvivesConfirmationAndHasSafeBounds(t *testing.T) {
	ctx := ResumeResponseBudget(context.Background(), maxTurnResponseBytes-3)
	ctx = WithResponseBudget(ctx)
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	if _, err := io.ReadAll(&responseReader{r: strings.NewReader("12"), left: 10, turn: b}); err != nil {
		t.Fatal(err)
	}
	received := ResponseBytes(ctx)
	if received != maxTurnResponseBytes-1 {
		t.Fatalf("已接收数量不对：%d", received)
	}
	resumed := ResumeResponseBudget(context.Background(), received)
	b, _ = resumed.Value(responseBudgetKey{}).(*responseBudget)
	if _, err := io.ReadAll(&responseReader{r: strings.NewReader("34"), left: 10, turn: b}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("人工确认后不能重置一轮额度：%v", err)
	}
	if !responseBudgetSpent(ResumeResponseBudget(context.Background(), 1<<62)) {
		t.Fatal("过大的已用数量不能溢出绕过额度")
	}
}

func TestSSEEventCountIsBounded(t *testing.T) {
	r := newSSEReader(strings.NewReader(strings.Repeat("data: x\n\n", maxSSEEvents+1)))
	for i := 0; i < maxSSEEvents; i++ {
		if _, err := r.next(); err != nil {
			t.Fatalf("上限以内第%d个事件：%v", i, err)
		}
	}
	if _, err := r.next(); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("事件数量超限未停止：%v", err)
	}
}

func TestClaudeContentBlockCountIsBounded(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
				if stream {
					_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{}}\n\n")
					for i := 0; i <= maxReplyBlocks; i++ {
						fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"text\",\"text\":\"x\"}}\n\n", i)
					}
				} else {
					blocks := make([]any, maxReplyBlocks+1)
					for i := range blocks {
						blocks[i] = map[string]string{"type": "text", "text": "x"}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"type": "message", "content": blocks})
				}
			})
			c, v := limitClient(t, Anthropic, stream, f)
			if _, err := c.Chat(context.Background(), v, nil); !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("内容块数量超限未停止：%v", err)
			}
		})
	}
}

func limitClient(t *testing.T, protocol Protocol, stream bool, f *fakeServer) (*Client, *Conversation) {
	t.Helper()
	c, err := NewClient(Config{Protocol: protocol, BaseURL: f.srv.URL, Model: "limit-test", Stream: stream, RetryDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	v, err := c.NewConversation("system", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddUser("hello"); err != nil {
		t.Fatal(err)
	}
	return c, v
}

func TestBothProvidersStopGrowingStreamWithoutRetry(t *testing.T) {
	for _, protocol := range []Protocol{OpenAI, Anthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			first := make(chan struct{})
			var once sync.Once
			f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/event-stream")
				if protocol == OpenAI {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"safe\"}}]}\n\n")
				} else {
					_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"model\":\"test\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"safe\"}}\n\n")
				}
				w.(http.Flusher).Flush()
				select {
				case <-first:
				case <-time.After(3 * time.Second):
					return
				}
				for i := 0; i < 100; i++ {
					var data string
					if protocol == OpenAI {
						data = fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", strings.Repeat("x", 100))
					} else {
						data = fmt.Sprintf("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", strings.Repeat("x", 100))
					}
					if _, err := io.WriteString(w, data); err != nil {
						return
					}
				}
			})
			c, v := limitClient(t, protocol, true, f)
			c.responseLimit = 1024
			var emitted int
			res, err := c.Chat(context.Background(), v, func(e Event) {
				if e.Type == EvText {
					emitted += len(e.Text)
					once.Do(func() { close(first) })
				}
			})
			if !errors.Is(err, ErrResponseTooLarge) || res == nil || !res.Partial || !strings.HasPrefix(res.Text, "safe") {
				t.Fatalf("应保留已收文字、标成未完成并停止：%+v %v", res, err)
			}
			if emitted > 1024 || len(f.requests()) != 1 {
				t.Fatalf("超限后不能继续发出内容或重试：字节=%d 请求=%d", emitted, len(f.requests()))
			}
			if err := v.AddResult(res); err == nil {
				t.Fatal("未完成的回复不能进入可执行工具的会话历史")
			}
		})
	}
}

func TestBothProvidersBoundBodiesBeforeAnyOutput(t *testing.T) {
	for _, protocol := range []Protocol{OpenAI, Anthropic} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
					if stream {
						_, _ = io.WriteString(w, strings.Repeat(": keep-alive\n\n", 1000))
						return
					}
					if protocol == OpenAI {
						_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": strings.Repeat("x", 1000)}}}})
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"type": "message", "content": []any{map[string]string{"type": "text", "text": strings.Repeat("x", 1000)}}})
					}
				})
				c, v := limitClient(t, protocol, stream, f)
				c.responseLimit = 128
				emitted := false
				_, err := c.Chat(context.Background(), v, func(e Event) {
					if e.Type == EvText || e.Type == EvReasoning || e.Type == EvToolDone {
						emitted = true
					}
				})
				if !errors.Is(err, ErrResponseTooLarge) || emitted || len(f.requests()) != 1 {
					t.Fatalf("响应整体过大或保活无限增长时应直接停止：err=%v 输出=%v 请求=%d", err, emitted, len(f.requests()))
				}
			})
		}
	}
}

func TestChatSharesBudgetAcrossToolRounds(t *testing.T) {
	const reply = `{"model":"m","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) { _, _ = io.WriteString(w, reply) })
	c, v := limitClient(t, OpenAI, false, f)
	ctx := context.WithValue(context.Background(), responseBudgetKey{}, &responseBudget{left: int64(len(reply)*2 - 1)})
	res, err := c.Chat(ctx, v, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddResult(res); err != nil {
		t.Fatal(err)
	}
	if err := v.AddUser("again"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Chat(ctx, v, nil); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("同一轮累计超限应停止：%v", err)
		}
	}
	if len(f.requests()) != 2 {
		t.Fatalf("预算已耗尽不应再向服务商请求：%d", len(f.requests()))
	}
}

func TestBothProvidersBoundToolAndContentCounts(t *testing.T) {
	for _, protocol := range []Protocol{OpenAI, Anthropic} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						if protocol == Anthropic {
							_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{}}\n\n")
						}
						for i := 0; i <= maxReplyTools; i++ {
							if protocol == OpenAI {
								fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":\"c%d\",\"function\":{\"name\":\"tool\",\"arguments\":\"{}\"}}]}}]}\n\n", i, i)
							} else {
								fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":\"c%d\",\"name\":\"tool\",\"input\":{}}}\n\n", i, i)
							}
						}
					} else {
						var calls []any
						for i := 0; i <= maxReplyTools; i++ {
							if protocol == OpenAI {
								calls = append(calls, map[string]any{"id": fmt.Sprintf("c%d", i), "function": map[string]string{"name": "tool", "arguments": "{}"}})
							} else {
								calls = append(calls, map[string]any{"type": "tool_use", "id": fmt.Sprintf("c%d", i), "name": "tool", "input": map[string]any{}})
							}
						}
						if protocol == OpenAI {
							_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"tool_calls": calls}}}})
						} else {
							_ = json.NewEncoder(w).Encode(map[string]any{"type": "message", "content": calls})
						}
					}
				})
				c, v := limitClient(t, protocol, stream, f)
				_, err := c.Chat(context.Background(), v, nil)
				if !errors.Is(err, ErrResponseTooLarge) || len(f.requests()) != 1 {
					t.Fatalf("工具个数超限必须停止且不重试：%v 请求=%d", err, len(f.requests()))
				}
			})
		}
	}
}
