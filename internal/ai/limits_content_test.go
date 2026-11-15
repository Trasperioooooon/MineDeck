package ai

import (
	"bufio"
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

// 复审发现 7（2026-11-14）：回复的上限以前按事件流的原始字节算。OpenAI 兼容接口一个词元一个事件、
// 每个事件两三百字节的外壳，8 MiB 只够两三万个词元。现在内容按解码后的正文、思考、工具参数算，
// 原始字节另有宽松得多的兜底。下面几条都用默认的上限，不调小。

// longTokens 一个词元一个事件时轮流推的词元：中文、英文、标点都有。
var longTokens = []string{"我", "们", " the", " server", "，", "MC", "的", " config"}

func TestLongTokenByTokenStreamCompletes(t *testing.T) {
	const perPart = 40000 // 思考、正文各四万个词元，一共八万个事件
	var wantThink, wantText strings.Builder
	for i := 0; i < perPart; i++ {
		wantThink.WriteString(longTokens[i%len(longTokens)])
		wantText.WriteString(longTokens[(i+3)%len(longTokens)])
	}
	for _, protocol := range []Protocol{OpenAI, Anthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			// 整条流先在内存里拼好，数清原始字节和事件数，再交给假服务原样发出去
			var body strings.Builder
			events := 0
			ev := func(s string) { events++; body.WriteString(s) }
			{
				if protocol == OpenAI {
					// 照 DeepSeek / OpenAI 流式块的样子：每个块都带编号、时间、模型、指纹这些外壳
					const head = `data: {"id":"chatcmpl-B8xQf4mJ2kV9nL3pR7sT1uW5yZ0aC","object":"chat.completion.chunk","created":1763180000,"model":"deepseek-reasoner","system_fingerprint":"fp_8802369eaa_prod0623_fp8_kvcache","choices":[{"index":0,"delta":`
					const tail = `,"logprobs":null,"finish_reason":null}]}` + "\n\n"
					for i := 0; i < perPart; i++ {
						ev(head + fmt.Sprintf(`{"content":null,"reasoning_content":%q}`, longTokens[i%len(longTokens)]) + tail)
					}
					for i := 0; i < perPart; i++ {
						ev(head + fmt.Sprintf(`{"content":%q}`, longTokens[(i+3)%len(longTokens)]) + tail)
					}
					ev(`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":80000}}` + "\n\n")
					ev("data: [DONE]\n\n")
				} else {
					ev("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_long\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-long\",\"content\":[],\"usage\":{\"input_tokens\":20,\"output_tokens\":1}}}\n\n")
					ev("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n")
					for i := 0; i < perPart; i++ {
						ev(fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}\n\n", longTokens[i%len(longTokens)]))
					}
					ev("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"c2lnbmF0dXJl\"}}\n\n")
					ev("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
					ev("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
					for i := 0; i < perPart; i++ {
						ev(fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", longTokens[(i+3)%len(longTokens)]))
					}
					ev("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
					ev("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":80000}}\n\n")
					ev("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				}
			}
			stream := body.String()
			raw := len(stream)
			f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, stream)
			})
			c, v := limitClient(t, protocol, true, f)
			texts, thinks := 0, 0
			res, err := c.Chat(context.Background(), v, func(e Event) {
				switch e.Type {
				case EvText:
					texts++
				case EvReasoning:
					thinks++
				}
			})
			if err != nil {
				t.Fatalf("逐词元推送的长回复应能收完：%v（原始 %d 字节、%d 个事件）", err, raw, events)
			}
			// 这条流按旧的算法必定超限：原始字节超过 8 MiB，事件数超过 65536。不是的话这条测试就没测到要测的东西
			if raw <= 8<<20 || events <= 65536 {
				t.Fatalf("测试流太小：原始 %d 字节、%d 个事件", raw, events)
			}
			if res.Partial || res.Stop != StopEnd || res.Text != wantText.String() || res.Reasoning != wantThink.String() {
				t.Fatalf("回复不完整：partial=%v stop=%v 正文 %d/%d 字节 思考 %d/%d 字节", res.Partial, res.Stop,
					len(res.Text), wantText.Len(), len(res.Reasoning), wantThink.Len())
			}
			if texts != perPart || thinks != perPart || len(f.requests()) != 1 {
				t.Fatalf("事件数：正文 %d 思考 %d 请求 %d", texts, thinks, len(f.requests()))
			}
			if err := v.AddResult(res); err != nil {
				t.Fatalf("收完的长回复应能进会话历史：%v", err)
			}
			t.Logf("%s：原始 %.1f MiB、%d 个事件，内容 %.2f MiB", protocol, float64(raw)/(1<<20), events, float64(len(res.Text)+len(res.Reasoning))/(1<<20))
		})
	}
}

// 内容真超过 8 MiB：和以前一样停下、不重试，开始输出以后交回已收的部分（标成未完成，不能进可执行工具的历史），
// 报错里是「回复内容过多」；不流式的整段超限什么都不交出去。
func TestContentOverLimitStopsAsBefore(t *testing.T) {
	const chunk = 4096
	const chunks = maxResponseBytes/chunk + 256 // 比 8 MiB 多 1 MiB
	piece := strings.Repeat("内容", chunk/len("内容")+1)[:chunk/6*6]
	for _, protocol := range []Protocol{OpenAI, Anthropic} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
					bw := bufio.NewWriterSize(w, 64<<10)
					defer bw.Flush()
					switch {
					case !stream && protocol == OpenAI:
						_ = json.NewEncoder(bw).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": strings.Repeat(piece, chunks)}, "finish_reason": "stop"}}})
					case !stream:
						_ = json.NewEncoder(bw).Encode(map[string]any{"type": "message", "content": []any{map[string]string{"type": "text", "text": strings.Repeat(piece, chunks)}}, "stop_reason": "end_turn"})
					case protocol == OpenAI:
						for i := 0; i < chunks; i++ {
							fmt.Fprintf(bw, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", piece)
						}
						_, _ = bw.WriteString("data: [DONE]\n\n")
					default:
						_, _ = bw.WriteString("data: {\"type\":\"message_start\",\"message\":{\"model\":\"m\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
						for i := 0; i < chunks; i++ {
							fmt.Fprintf(bw, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", piece)
						}
						_, _ = bw.WriteString("data: {\"type\":\"message_stop\"}\n\n")
					}
				})
				c, v := limitClient(t, protocol, stream, f)
				ctx := WithResponseBudget(context.Background())
				emitted := 0
				res, err := c.Chat(ctx, v, func(e Event) {
					if e.Type == EvText {
						emitted += len(e.Text)
					}
				})
				if !errors.Is(err, ErrResponseTooLarge) || !strings.Contains(err.Error(), "回复内容过多") || len(f.requests()) != 1 {
					t.Fatalf("内容超限应停止、提示「回复内容过多」、不重试：%v 请求=%d", err, len(f.requests()))
				}
				if emitted > maxResponseBytes {
					t.Fatalf("超限的内容交给了回调：%d 字节", emitted)
				}
				if !stream {
					// 同原始字节超限：整段读完才知道，什么都没交出去
					if res != nil || emitted != 0 || errors.Is(err, ErrDisconnected) {
						t.Fatalf("不流式超限：res=%v 已交出 %d 字节 %v", res != nil, emitted, err)
					}
					return
				}
				// 同原始字节超限：已经开始输出，算中途断开，交回收到的那部分
				if !errors.Is(err, ErrDisconnected) || res == nil || !res.Partial || len(res.Text) == 0 || len(res.Text) > maxResponseBytes || emitted != len(res.Text) {
					t.Fatalf("流式超限：%v partial=%v 正文 %d 字节 已交出 %d 字节", err, res != nil && res.Partial, len(res.Text), emitted)
				}
				if err := v.AddResult(res); err == nil {
					t.Fatal("未完成的回复不能进入可执行工具的会话历史")
				}
				// 一轮记下的是收下的内容（Claude 另有块的骨架几十字节），超限的那一块没有记进去
				if got := ResponseBytes(ctx); got < int64(len(res.Text)) || got > int64(len(res.Text))+64 {
					t.Fatalf("一轮记下的内容字节 %d，实际收下 %d", got, len(res.Text))
				}
			})
		}
	}
}

// 原始字节的兜底：内容不涨、保活一直来（流式）也会停。已经有输出的，和以前一样交回已收的部分。
func TestRawLimitStopsKeepaliveAfterText(t *testing.T) {
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
				// 等客户端交出了第一段正文再灌保活：超限的那一块整块不交给解析器，不然「safe」会和保活一起被丢掉
				w.(http.Flusher).Flush()
				select {
				case <-first:
				case <-time.After(3 * time.Second):
					return
				}
				for i := 0; i < 2000; i++ {
					if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
						return
					}
				}
			})
			c, v := limitClient(t, protocol, true, f)
			c.rawLimit = 4096
			res, err := c.Chat(context.Background(), v, func(e Event) {
				if e.Type == EvText {
					once.Do(func() { close(first) })
				}
			})
			if !errors.Is(err, ErrResponseTooLarge) || !errors.Is(err, ErrDisconnected) || res == nil || !res.Partial || res.Text != "safe" || len(f.requests()) != 1 {
				t.Fatalf("保活无限增长应按原始字节停下并交回已收的部分：%v %+v 请求=%d", err, res, len(f.requests()))
			}
		})
	}
}

// 一轮的额度只算内容：外壳很大、正文很小的两次回复都能收；内容合计超了，第三次不再发请求。
func TestTurnBudgetCountsContentNotEnvelope(t *testing.T) {
	pad := strings.Repeat("p", 2000)
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		fmt.Fprintf(w, "data: {\"id\":%q,\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":%q,\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", pad, pad)
	})
	c, v := limitClient(t, OpenAI, true, f)
	ctx := context.WithValue(context.Background(), responseBudgetKey{}, &responseBudget{left: 4})
	for i := 0; i < 2; i++ {
		res, err := c.Chat(ctx, v, nil)
		if err != nil {
			t.Fatalf("第 %d 次：外壳不该算进一轮的额度：%v", i+1, err)
		}
		if err := v.AddResult(res); err != nil {
			t.Fatal(err)
		}
		if err := v.AddUser("again"); err != nil {
			t.Fatal(err)
		}
	}
	// 4 - 2 - 2 = 0：正好用完，同一轮不再请求
	if _, err := c.Chat(ctx, v, nil); !errors.Is(err, ErrResponseTooLarge) || len(f.requests()) != 2 {
		t.Fatalf("额度用完以后不该再请求：%v 请求=%d", err, len(f.requests()))
	}
}
