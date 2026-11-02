package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BridgeOptions 桥的依赖。
type BridgeOptions struct {
	Dir     string                 // MCP 的数据目录（DataDir(程序目录)）：在这里找 runtime.json
	Base    string                 // 程序目录：runtime.json 里的进程必须是这个目录下的程序
	In      io.Reader              // MCP 客户端写来的消息（标准输入），一行一条
	Out     io.Writer              // 回给客户端的消息（标准输出），一行一条，只写合法的 MCP 消息
	Err     io.Writer              // 日志（标准错误）
	Token   func() (string, error) // 读 MCP 令牌（Windows 上从 secrets.json 用 DPAPI 解出来：同一个 Windows 用户才解得开）
	HTTP    *http.Client           // nil 用默认的（不走代理，连本机）
	Alive   func(pid int, base string) error
	Version string // MineDeck 的版本号（X-MineDeck-Bridge 请求头）
}

const (
	msgNotRunning = "没有找到运行着的 MineDeck：先打开 MineDeck（就是这个 MineDeck.exe），在「设置 → AI（可选）→ MCP 接口」里打开 MCP 接口，再让 AI 重试。"
	msgNoSecret   = "读不出 MineDeck 的 MCP 令牌：MineDeck 和这个 AI 工具要是同一个 Windows 用户在用；也可以在 MineDeck 的「设置 → AI（可选）→ MCP 接口」里重置令牌。"
)

// bridge 一次运行的桥：stdio 一行一条 ↔ 本机 HTTP 一个请求一条。请求可以同时在路上（各开一个协程），回复谁先到先写。
// 旧版握手时记下 Mcp-Session-Id 和谈好的版本，之后的请求都带上；2026-07-28 的请求按请求体补上要求的镜像头。
type bridge struct {
	o    BridgeOptions
	http *http.Client

	outMu sync.Mutex // 一次写一整行

	mu       sync.Mutex
	session  string // 旧版的 Mcp-Session-Id
	protocol string // 旧版谈好的版本
	client   string // 客户端在 initialize 里报的名字
	inflight map[string]context.CancelFunc
	lastErr  string // 上一次写到标准错误的提示（同样的不重复写）
}

// Bridge 运行 stdio 桥，直到标准输入关闭（或 ctx 取消）。MineDeck 没有运行、MCP 接口没打开、令牌不对时，
// 每个请求都回一个 JSON-RPC 错误（说明怎么办），同时写到标准错误；MineDeck 之后打开了，下一个请求就能通。
func Bridge(ctx context.Context, o BridgeOptions) error {
	b := &bridge{o: o, http: o.HTTP, inflight: map[string]context.CancelFunc{}}
	if b.http == nil {
		b.http = &http.Client{Transport: &http.Transport{
			Proxy:                 nil, // 只连本机，不走代理
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			ResponseHeaderTimeout: 5 * time.Minute,
			MaxIdleConnsPerHost:   4,
		}}
	}
	if b.o.Alive == nil {
		b.o.Alive = processAlive
	}
	if b.o.Err == nil {
		b.o.Err = io.Discard
	}
	fmt.Fprintf(b.o.Err, "MineDeck MCP：把 stdio 转给 %s 下运行着的 MineDeck\n", b.o.Base)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReaderSize(b.o.In, 64<<10)
		for {
			line, err := r.ReadBytes('\n')
			if l := bytes.TrimSpace(line); len(l) > 0 {
				select {
				case lines <- l:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()
	for {
		select {
		case l := <-lines:
			b.dispatch(ctx, l, &wg)
			continue
		case err := <-readErr:
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(b.o.Err, "MineDeck MCP：读标准输入失败：", err)
			}
		case <-ctx.Done():
		}
		break
	}
	// 标准输入关了（客户端要结束）：正在处理的请求最多再等 10 秒
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
	}
	return nil
}

// dispatch 处理一行：stdio 上的取消通知自己处理（断开那个请求）；其余的转给 MineDeck。
func (b *bridge) dispatch(ctx context.Context, line []byte, wg *sync.WaitGroup) {
	if line[0] == '[' { // 旧版的批量请求：整个转过去
		wg.Add(1)
		go func() { defer wg.Done(); b.forward(ctx, line, nil, "", nil) }()
		return
	}
	var m struct {
		ID     json.RawMessage            `json:"id"`
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		b.write(encode(errReply(nil, codeParse, "不是合法的 JSON："+err.Error(), nil)))
		return
	}
	if m.Method == "notifications/cancelled" {
		var id json.RawMessage
		_ = json.Unmarshal(m.Params["requestId"], &id)
		b.cancel(idKey(id))
		return
	}
	if m.Method == "initialize" {
		if id := parseIdentity(m.Params["clientInfo"]); id.known() {
			b.mu.Lock()
			b.client = id.Name
			b.mu.Unlock()
		}
	}
	isReq := m.Method != "" && validID(m.ID)
	rctx, cancel := context.WithCancel(ctx)
	key := idKey(m.ID)
	if isReq {
		b.mu.Lock()
		if old := b.inflight[key]; old != nil { // 同一个 id 又来了（客户端不该这样）：旧的不再回
			old()
		}
		b.inflight[key] = cancel
		b.mu.Unlock()
	}
	var id json.RawMessage
	if isReq {
		id = m.ID
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		b.forward(rctx, line, id, m.Method, m.Params)
		if isReq {
			b.mu.Lock()
			delete(b.inflight, key)
			b.mu.Unlock()
		}
	}()
}

func idKey(id json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, id) != nil {
		return string(id)
	}
	return buf.String()
}

func (b *bridge) cancel(key string) {
	b.mu.Lock()
	c := b.inflight[key]
	delete(b.inflight, key)
	b.mu.Unlock()
	if c != nil {
		c()
	}
}

// forward 把一条消息 POST 给 MineDeck，把回复写回标准输出。id 为 nil 表示不用回（通知、批量里没有请求时）。
func (b *bridge) forward(ctx context.Context, line, id json.RawMessage, method string, params map[string]json.RawMessage) {
	rt, err := b.runtime()
	if err != nil {
		b.fail(ctx, id, CodeNotRunning, msgNotRunning, err.Error())
		return
	}
	tok, err := b.o.Token()
	if err != nil || tok == "" {
		detail := "没有令牌"
		if err != nil {
			detail = err.Error()
		}
		b.fail(ctx, id, CodeAuth, msgNoSecret, detail)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/mcp", rt.Port), bytes.NewReader(line))
	if err != nil {
		b.fail(ctx, id, codeInternal, "转发失败："+err.Error(), "")
		return
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/event-stream")
	h.Set("Authorization", "Bearer "+tok)
	ver := b.o.Version
	if ver == "" {
		ver = "1"
	}
	h.Set(HeaderBridge, ver)
	b.mu.Lock()
	session, protocol, client := b.session, b.protocol, b.client
	b.mu.Unlock()
	if client != "" {
		h.Set(HeaderClient, url.QueryEscape(client))
	}
	// 2026-07-28 的请求：按请求体补上镜像头；旧版：带上握手时谈好的版本和会话
	modern := ""
	if raw := params["_meta"]; len(raw) > 0 {
		var meta map[string]json.RawMessage
		if json.Unmarshal(raw, &meta) == nil {
			_ = json.Unmarshal(meta["io.modelcontextprotocol/protocolVersion"], &modern)
		}
	}
	switch {
	case modern != "":
		h.Set("MCP-Protocol-Version", modern)
	case method != "initialize" && protocol != "":
		h.Set("MCP-Protocol-Version", protocol)
	}
	if method != "" {
		h.Set("Mcp-Method", method)
	}
	field := map[string]string{"tools/call": "name", "prompts/get": "name", "resources/read": "uri"}[method]
	if field != "" {
		var name string
		if json.Unmarshal(params[field], &name) == nil {
			h.Set("Mcp-Name", EncodeHeader(name))
		}
	}
	if session != "" && modern == "" && method != "initialize" {
		h.Set("Mcp-Session-Id", session)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		b.fail(ctx, id, CodeNotRunning, msgNotRunning, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		b.relaySSE(ctx, resp.Body, id)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if ctx.Err() != nil {
		return // 客户端取消了：不再回
	}
	if err != nil {
		b.fail(ctx, id, CodeNotRunning, msgNotRunning, err.Error())
		return
	}
	if method == "initialize" && resp.StatusCode == http.StatusOK {
		var r struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		_ = json.Unmarshal(body, &r)
		b.mu.Lock()
		b.session, b.protocol = resp.Header.Get("Mcp-Session-Id"), r.Result.ProtocolVersion
		b.mu.Unlock()
	}
	msg, rerr := withID(body, id)
	if rerr != "" {
		b.note(rerr)
	}
	if msg == nil {
		if id != nil {
			b.fail(ctx, id, codeInternal, fmt.Sprintf("MineDeck 返回了看不懂的内容（HTTP %d）", resp.StatusCode), strings.TrimSpace(string(body)))
		}
		return
	}
	if id != nil || bytes.HasPrefix(msg, []byte("[")) {
		b.write(msg)
	}
}

// withID 整理 MineDeck 回来的 JSON：压成一行；请求体解析之前就拒绝的错误没有 id，补上原来请求的 id。
// 第二个返回值是要写到标准错误的提示（MCP 接口没开、令牌不对这类）。
func withID(body, id json.RawMessage) ([]byte, string) {
	var buf bytes.Buffer
	if json.Compact(&buf, body) != nil {
		return nil, ""
	}
	out := buf.Bytes()
	if len(out) == 0 || out[0] != '{' {
		return out, ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(out, &m) != nil {
		return nil, ""
	}
	note := ""
	if e := m["error"]; e != nil {
		var re rpcError
		if json.Unmarshal(e, &re) == nil && (re.Code == CodeDisabled || re.Code == CodeAuth || re.Code == CodeForbidden) {
			note = re.Message
		}
		if (m["id"] == nil || string(m["id"]) == "null") && id != nil {
			m["id"] = id
			out = encode(m)
		}
	}
	return out, note
}

// relaySSE MineDeck 回的是 SSE 流时（现在不会这样，留着以防以后流式回复）：每个事件的 data 是一条消息，逐条写回。
func (b *bridge) relaySSE(ctx context.Context, body io.Reader, id json.RawMessage) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 64<<20)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		raw := []byte(strings.Join(data, "\n"))
		data = nil
		if msg, _ := withID(raw, id); msg != nil && ctx.Err() == nil {
			b.write(msg)
		}
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
}

// runtime 读运行时文件，确认里面的进程还在、是这个目录下的程序（文件可能是上次崩溃留下的，端口可能已经被别的程序用了：
// 不能把令牌发给它）。
func (b *bridge) runtime() (*Runtime, error) {
	raw, err := os.ReadFile(filepath.Join(b.o.Dir, RuntimeFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("没有运行时文件（MineDeck 没打开，或者没打开 MCP 接口）")
		}
		return nil, err
	}
	var rt Runtime
	if json.Unmarshal(raw, &rt) != nil || rt.V != 1 || rt.Port <= 0 || rt.Port > 65535 || rt.PID <= 0 {
		return nil, errors.New("运行时文件的格式不对")
	}
	if err := b.o.Alive(rt.PID, b.o.Base); err != nil {
		return nil, fmt.Errorf("运行时文件是上次留下的：%v", err)
	}
	return &rt, nil
}

// fail 回一个 JSON-RPC 错误（是请求才回），提示同时写到标准错误。
func (b *bridge) fail(ctx context.Context, id json.RawMessage, code int, msg, detail string) {
	note := msg
	if detail != "" {
		note += "（" + detail + "）"
	}
	b.note(note)
	if id == nil || ctx.Err() != nil {
		return
	}
	b.write(encode(errReply(id, code, msg, nil)))
}

// note 写一行提示到标准错误；和上一次一样就不重复写。
func (b *bridge) note(s string) {
	b.mu.Lock()
	same := s == b.lastErr
	b.lastErr = s
	b.mu.Unlock()
	if !same {
		fmt.Fprintln(b.o.Err, "MineDeck MCP：", s)
	}
}

func (b *bridge) write(msg []byte) {
	b.outMu.Lock()
	defer b.outMu.Unlock()
	_, _ = b.o.Out.Write(append(bytes.TrimRight(msg, "\n"), '\n'))
}
