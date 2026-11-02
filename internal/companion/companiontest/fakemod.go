// Package companiontest 测试用的假伙伴模组：在 127.0.0.1 上监听，按协议 v1（companion/README.md）应答：
// 握手和版本协商、令牌、同一时间只接受一个控制连接、心跳（10 秒没收到任何帧就发 END）、每 2 秒一次 STATUS、
// 指令按顺序执行（指令引起的事件先发、回复后发）。内置一个很小的「世界」：刷出、移除、查状态、
// 五种任务（开始时发 task_started，测试用 Finish 让它结束）、停下、说话。给 companion 和别的包写测试用。
package companiontest

import (
	"bufio"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/companion"
)

// Options 假模组的参数。
type Options struct {
	Token       string
	MaxVersion  int           // 模组支持的最高协议版本，默认 1
	StatusEvery time.Duration // STATUS 的间隔，默认 2 秒；负数不发
	IdleTimeout time.Duration // 多久没收到任何帧就发 END（心跳超时），默认 10 秒
	TokenDelay  time.Duration // 令牌不对时等多久再回 ERROR 1，默认 0.3 秒
	PongDelay   time.Duration // 收到 PING 后等多久再回 PONG（Windows 的时钟一跳 0.5～15.6 毫秒，本机往返太快会量成 0）
	// Handle 不为 nil 时由它处理指令：返回要先发的事件和回复（回复里的 id 由这里补上）。返回 nil 的回复表示交给内置的处理。
	Handle func(req map[string]any) (events []any, resp map[string]any)
	// Welcome 改 WELCOME 的 JSON（在内置的基础上）。
	Welcome func(w map[string]any)
	Session string // WELCOME 的 session，默认 "s1"
}

// Hello 收到的一次 HELLO。
type Hello struct {
	Version int
	Token   string
	Client  string
}

// FakeMod 假的伙伴模组。
type FakeMod struct {
	o  Options
	ln net.Listener

	mu       sync.Mutex
	ctl      *conn // 控制连接（同一时间只有一个）
	reject   int   // 非 0 时用这个错误码拒绝新连接
	silent   bool  // 不发 STATUS、不回 PONG（测 MineDeck 那边的心跳超时）
	hellos   []Hello
	requests []map[string]any
	ends     []string
	pings    int
	accepts  int
	closed   bool
	session  string
	nextTask int64
	world    map[int]map[string]any // 编号 → 伙伴对象
	tasks    map[int64]map[string]any
	config   map[string]any
	extraOps []string
}

type conn struct {
	c   net.Conn
	wmu sync.Mutex
}

func (c *conn) write(typ byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return companion.WriteFrame(c.c, typ, payload)
}

// Start 在 127.0.0.1:port 上监听（port 为 0 时随便挑一个空端口）。
func Start(port int, o Options) (*FakeMod, error) {
	if o.MaxVersion == 0 {
		o.MaxVersion = 1
	}
	if o.StatusEvery == 0 {
		o.StatusEvery = 2 * time.Second
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = 10 * time.Second
	}
	if o.TokenDelay == 0 {
		o.TokenDelay = 300 * time.Millisecond
	}
	if o.Session == "" {
		o.Session = "s1"
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	m := &FakeMod{o: o, ln: ln, session: o.Session, world: map[int]map[string]any{}, tasks: map[int64]map[string]any{},
		config: map[string]any{"maxCompanions": 4, "attackDeny": companion.DefaultAttackDeny, "zones": []any{}, "problems": []any{}}}
	go m.serve()
	return m, nil
}

// Port 监听的端口。
func (m *FakeMod) Port() int { return m.ln.Addr().(*net.TCPAddr).Port }

// Close 停止监听并断开所有连接。
func (m *FakeMod) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.ln.Close()
	m.Kick()
}

// Kick 断开控制连接（模拟模组崩溃、网络断开），不发 END。
func (m *FakeMod) Kick() {
	m.mu.Lock()
	c := m.ctl
	m.ctl = nil
	m.mu.Unlock()
	if c != nil {
		c.c.Close()
	}
}

// Restart 模拟服务器重启：断开连接，换一个 session，任务编号从 1 重新数（伙伴还在：重启后被接管）。
func (m *FakeMod) Restart(session string) {
	m.Kick()
	m.mu.Lock()
	m.session, m.nextTask = session, 0
	m.tasks = map[int64]map[string]any{}
	for _, c := range m.world {
		c["task"], c["state"] = nil, companion.StateIdle
	}
	m.mu.Unlock()
}

// SetReject 非 0 时用这个错误码拒绝新连接（0 恢复正常）。
func (m *FakeMod) SetReject(code int) {
	m.mu.Lock()
	m.reject = code
	m.mu.Unlock()
}

// SetSilent 不再发 STATUS、不回 PONG（让 MineDeck 那边 10 秒收不到任何帧）。
func (m *FakeMod) SetSilent(on bool) {
	m.mu.Lock()
	m.silent = on
	m.mu.Unlock()
}

// SetConfig 改模组「实际在用的配置」（WELCOME 和 status 里的 config），并发 config_applied。
func (m *FakeMod) SetConfig(cfg map[string]any) {
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
	_ = m.SendEvent(map[string]any{"event": companion.EvConfigApplied, "config": cfg})
}

// AddOps WELCOME 的 ops 里多报几个指令（测兼容规则）。
func (m *FakeMod) AddOps(ops ...string) {
	m.mu.Lock()
	m.extraOps = append(m.extraOps, ops...)
	m.mu.Unlock()
}

// Connected 现在有没有控制连接。
func (m *FakeMod) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ctl != nil
}

// Accepts 接受过几次控制连接（握手成功的）。
func (m *FakeMod) Accepts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accepts
}

// Hellos 收到的 HELLO。
func (m *FakeMod) Hellos() []Hello {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Hello(nil), m.hellos...)
}

// Requests 收到的指令（原样解出的 JSON）。
func (m *FakeMod) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.requests...)
}

// Ends 收到的 END 的原因。
func (m *FakeMod) Ends() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ends...)
}

// Pings 收到过几个 PING。
func (m *FakeMod) Pings() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pings
}

// Companions 现在世界里的伙伴（按编号）。
func (m *FakeMod) Companions() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []int
	for id := range m.world {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, clone(m.world[id]))
	}
	return out
}

// SendFrame 在控制连接上发任意一帧（测不认识的帧类型）。
func (m *FakeMod) SendFrame(typ byte, payload []byte) error {
	m.mu.Lock()
	c := m.ctl
	m.mu.Unlock()
	if c == nil {
		return fmt.Errorf("没有控制连接")
	}
	return c.write(typ, payload)
}

// SendEvent 发一个事件（任意 JSON）。
func (m *FakeMod) SendEvent(ev any) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return m.SendFrame(companion.TypeEvent, b)
}

// SendEnd 发 END 再断开（模拟关服、改成 enabled=false）。
func (m *FakeMod) SendEnd(reason string) {
	m.mu.Lock()
	c := m.ctl
	m.ctl = nil
	m.mu.Unlock()
	if c != nil {
		_ = c.write(companion.TypeEnd, []byte(reason))
		c.c.Close()
	}
}

// Finish 让一个任务结束：发 task_finished（extra 里是 have、gained、want、detail……），伙伴回到空闲，记下 lastResult。
func (m *FakeMod) Finish(taskID int64, result string, extra map[string]any) error {
	return m.finish(taskID, result, extra, true)
}

// FinishLost 同 Finish，但 task_finished 丢了：只改伙伴的状态和 lastResult（之后的 STATUS、WELCOME 里看得到）。
// 真模组的 WELCOME 读的是每 tick 末尾才刷新的快照，握手正好落在任务结束的那个 tick 里时就是这样（两半联调时实测到过同样的接管事件）。
func (m *FakeMod) FinishLost(taskID int64, result string, extra map[string]any) error {
	return m.finish(taskID, result, extra, false)
}

func (m *FakeMod) finish(taskID int64, result string, extra map[string]any, send bool) error {
	m.mu.Lock()
	t := m.tasks[taskID]
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("没有任务 %d", taskID)
	}
	delete(m.tasks, taskID)
	cid := int(toInt(t["companion"]))
	ev := map[string]any{"event": companion.EvTaskFinished, "companion": cid, "task": t, "result": result, "seconds": 10.4,
		"pos": map[string]any{"dim": "minecraft:overworld", "x": 1.5, "y": 64.0, "z": -3.5}}
	for k, v := range extra {
		ev[k] = v
	}
	if c := m.world[cid]; c != nil {
		lr := clone(ev)
		delete(lr, "event")
		delete(lr, "companion")
		lr["at"] = time.Now().UnixMilli()
		c["task"], c["state"], c["lastResult"] = nil, companion.StateIdle, lr
	}
	m.mu.Unlock()
	if !send {
		return nil
	}
	return m.SendEvent(ev)
}

func (m *FakeMod) serve() {
	for {
		c, err := m.ln.Accept()
		if err != nil {
			return
		}
		go m.handle(c)
	}
}

func (m *FakeMod) handle(raw net.Conn) {
	c := &conn{c: raw}
	defer raw.Close()
	br := bufio.NewReader(raw)
	_ = raw.SetReadDeadline(time.Now().Add(5 * time.Second)) // 连上后 5 秒内要发 HELLO
	f, err := companion.ReadFrame(br)
	if err != nil {
		return
	}
	fail := func(code int, msg string) {
		p := make([]byte, 2, 2+len(msg))
		binary.LittleEndian.PutUint16(p, uint16(code))
		_ = c.write(companion.TypeError, append(p, msg...))
	}
	if f.Type != companion.TypeHello || len(f.Payload) < 3 || len(f.Payload) < 3+int(f.Payload[2]) {
		fail(companion.ErrProtocol, "第一帧不是 HELLO")
		return
	}
	ver := int(binary.LittleEndian.Uint16(f.Payload))
	tl := int(f.Payload[2])
	token := string(f.Payload[3 : 3+tl])
	m.mu.Lock()
	m.hellos = append(m.hellos, Hello{Version: ver, Token: token, Client: string(f.Payload[3+tl:])})
	reject, busy := m.reject, m.ctl != nil
	m.mu.Unlock()
	switch {
	case reject != 0:
		fail(reject, "测试里拒绝")
		return
	case subtle.ConstantTimeCompare([]byte(token), []byte(m.o.Token)) != 1:
		time.Sleep(m.o.TokenDelay)
		fail(companion.ErrBadToken, "令牌不对")
		return
	case ver == 0:
		fail(companion.ErrVersion, "协议版本 0 不支持")
		return
	case busy:
		fail(companion.ErrBusy, "已经有一个控制连接")
		return
	}
	use := min(ver, m.o.MaxVersion)
	m.mu.Lock()
	if m.ctl != nil { // 并发的另一个先连上了
		m.mu.Unlock()
		fail(companion.ErrBusy, "已经有一个控制连接")
		return
	}
	m.ctl = c
	m.accepts++
	w := m.welcomeLocked(use)
	m.mu.Unlock()
	if m.o.Welcome != nil {
		m.o.Welcome(w)
	}
	js, _ := json.Marshal(w)
	p := make([]byte, 2, 2+len(js))
	binary.LittleEndian.PutUint16(p, uint16(use))
	if err := c.write(companion.TypeWelcome, append(p, js...)); err != nil {
		m.drop(c)
		return
	}
	stop := make(chan struct{})
	defer close(stop)
	if m.o.StatusEvery > 0 {
		go m.statusLoop(c, stop)
	}
	for {
		_ = raw.SetReadDeadline(time.Now().Add(m.o.IdleTimeout))
		f, err := companion.ReadFrame(br)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				_ = c.write(companion.TypeEnd, []byte("心跳超时"))
			}
			m.drop(c)
			return
		}
		switch f.Type {
		case companion.TypePing:
			m.mu.Lock()
			m.pings++
			silent := m.silent
			m.mu.Unlock()
			switch {
			case silent:
			case m.o.PongDelay > 0:
				time.AfterFunc(m.o.PongDelay, func() { _ = c.write(companion.TypePong, f.Payload) })
			default:
				_ = c.write(companion.TypePong, f.Payload)
			}
		case companion.TypeRequest:
			m.request(c, f.Payload)
		case companion.TypeEnd:
			m.mu.Lock()
			m.ends = append(m.ends, string(f.Payload))
			m.mu.Unlock()
			m.drop(c)
			return
		default: // 不认识的帧类型：忽略
		}
	}
}

func (m *FakeMod) drop(c *conn) {
	m.mu.Lock()
	if m.ctl == c {
		m.ctl = nil
	}
	m.mu.Unlock()
}

func (m *FakeMod) statusLoop(c *conn, stop chan struct{}) {
	t := time.NewTicker(m.o.StatusEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.mu.Lock()
			silent := m.silent
			list := m.listLocked(false)
			m.mu.Unlock()
			if silent {
				continue
			}
			b, _ := json.Marshal(map[string]any{"companions": list})
			if c.write(companion.TypeStatus, b) != nil {
				return
			}
		}
	}
}

func (m *FakeMod) welcomeLocked(ver int) map[string]any {
	ops := append([]string{"spawn", "despawn", "status", "collect", "follow", "goto", "attack", "give", "stop", "say"}, m.extraOps...)
	return map[string]any{
		"protocol": ver, "session": m.session, "mod": "0.2.0", "loader": "neoforge", "minecraft": "1.21.1", "playerEngine": "1.21.1-1.4.0",
		"ops":      ops,
		"events":   []string{"spawned", "adopted", "removed", "died", "task_started", "task_finished", "stuck", "config_applied"},
		"features": []string{"nodig", "adopt"}, "limits": map[string]any{"maxCount": 2304, "maxText": 256, "maxName": 32, "maxZones": 64},
		"config": clone(m.config), "companions": m.listLocked(false),
	}
}

func (m *FakeMod) listLocked(inv bool) []map[string]any {
	var ids []int
	for id := range m.world {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := []map[string]any{}
	for _, id := range ids {
		c := clone(m.world[id])
		if !inv {
			delete(c, "inventory")
			delete(c, "selectedSlot")
		}
		out = append(out, c)
	}
	return out
}

// request 处理一条指令：先发它引起的事件，再发回复（按收到的顺序，一条一条来）。
func (m *FakeMod) request(c *conn, p []byte) {
	var req map[string]any
	if err := json.Unmarshal(p, &req); err != nil {
		b, _ := json.Marshal(map[string]any{"id": nil, "ok": false, "code": "bad_json", "error": "不是 JSON 对象"})
		_ = c.write(companion.TypeResponse, b)
		return
	}
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()
	var events []any
	var resp map[string]any
	if m.o.Handle != nil {
		events, resp = m.o.Handle(req)
	}
	if resp == nil {
		events, resp = m.builtin(req)
	}
	for _, e := range events {
		b, _ := json.Marshal(e)
		_ = c.write(companion.TypeEvent, b)
	}
	resp["id"] = req["id"]
	b, _ := json.Marshal(resp)
	_ = c.write(companion.TypeResponse, b)
}

func fail(code, msg string) map[string]any {
	return map[string]any{"ok": false, "code": code, "error": msg}
}

// builtin 内置的处理：一个很小的世界。
func (m *FakeMod) builtin(req map[string]any) ([]any, map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, _ := req["op"].(string)
	pick := func() (map[string]any, map[string]any) {
		if v, ok := req["companion"]; ok && v != nil {
			c := m.world[int(toInt(v))]
			if c == nil {
				return nil, fail("no_companion", fmt.Sprintf("没有编号 %v 的伙伴", v))
			}
			return c, nil
		}
		switch len(m.world) {
		case 0:
			return nil, fail("no_companion", "一个伙伴都没有")
		case 1:
			for _, c := range m.world {
				return c, nil
			}
		}
		return nil, fail("ambiguous", "有好几个伙伴，要写 companion")
	}
	switch op {
	case "spawn":
		name, _ := req["name"].(string)
		if name == "" {
			name = "伙伴"
		}
		for _, c := range m.world {
			if strings.EqualFold(c["name"].(string), name) {
				return nil, fail("name_taken", "已经有一个叫「"+name+"」的伙伴")
			}
		}
		if max := int(toInt(m.config["maxCompanions"])); max > 0 && len(m.world) >= max {
			r := fail("limit", "伙伴数量到上限了")
			r["limit"] = max
			return nil, r
		}
		id := 1
		for m.world[id] != nil {
			id++
		}
		info := map[string]any{"id": id, "uuid": fmt.Sprintf("00000000-0000-0000-0000-%012d", id), "name": name,
			"owner": req["owner"], "ownerUuid": nil, "tag": req["tag"], "dim": "minecraft:overworld", "x": 32.5, "y": 64.0, "z": -383.5,
			"health": 20.0, "maxHealth": 20.0, "state": companion.StateIdle, "task": nil, "lastResult": nil, "engineTask": "",
			"inventory": []any{map[string]any{"slot": 0, "item": "minecraft:oak_log", "count": 3}}, "selectedSlot": 0}
		m.world[id] = info
		pub := clone(info)
		delete(pub, "inventory")
		delete(pub, "selectedSlot")
		return []any{map[string]any{"event": "spawned", "companion": id, "info": pub}}, map[string]any{"ok": true, "companion": id, "info": pub}
	case "despawn":
		var ids []int
		if all, _ := req["all"].(bool); all {
			for id := range m.world {
				ids = append(ids, id)
			}
			sort.Ints(ids)
		} else {
			c, bad := pick()
			if bad != nil {
				return nil, bad
			}
			ids = []int{int(toInt(c["id"]))}
		}
		var evs []any
		for _, id := range ids {
			c := m.world[id]
			if t, ok := c["task"].(map[string]any); ok {
				tid := toInt(t["id"])
				delete(m.tasks, tid)
				evs = append(evs, map[string]any{"event": "task_finished", "companion": id, "task": t, "result": "cancelled", "seconds": 1.0})
			}
			delete(m.world, id)
			evs = append(evs, map[string]any{"event": "removed", "companion": id, "uuid": c["uuid"], "name": c["name"], "reason": "despawn"})
		}
		if ids == nil {
			ids = []int{}
		}
		return evs, map[string]any{"ok": true, "removed": ids}
	case "status":
		inv, _ := req["inventory"].(bool)
		list := m.listLocked(inv)
		if v, ok := req["companion"]; ok && v != nil {
			c := m.world[int(toInt(v))]
			if c == nil {
				return nil, fail("no_companion", "没有这个伙伴")
			}
			one := clone(c)
			if !inv {
				delete(one, "inventory")
				delete(one, "selectedSlot")
			}
			list = []map[string]any{one}
		}
		return nil, map[string]any{"ok": true, "companions": list, "config": clone(m.config)}
	case "collect", "follow", "goto", "attack", "give":
		c, bad := pick()
		if bad != nil {
			return nil, bad
		}
		if op == "give" {
			if p, _ := req["player"].(string); p == "Nobody" {
				return nil, fail("no_player", "玩家不在线：Nobody")
			}
		}
		if op == "attack" {
			if mob, _ := req["mob"].(string); strings.Contains(mob, "villager") {
				return nil, fail("forbidden", "不许打村民")
			}
		}
		var evs []any
		if old, ok := c["task"].(map[string]any); ok { // 新任务把旧任务顶掉
			delete(m.tasks, toInt(old["id"]))
			evs = append(evs, map[string]any{"event": "task_finished", "companion": c["id"], "task": old, "result": "superseded", "seconds": 2.0})
		}
		m.nextTask++
		args := map[string]any{}
		for k, v := range req {
			if k != "id" && k != "op" && k != "companion" {
				args[k] = v
			}
		}
		t := map[string]any{"id": m.nextTask, "companion": c["id"], "op": op, "args": args, "timeout": 600, "startedAt": time.Now().UnixMilli(),
			"command": op}
		m.tasks[m.nextTask] = t
		c["task"], c["state"] = t, companion.StateBusy
		evs = append(evs, map[string]any{"event": "task_started", "companion": c["id"], "task": t})
		return evs, map[string]any{"ok": true, "task": t}
	case "stop":
		var targets []map[string]any
		if all, _ := req["all"].(bool); all {
			for _, c := range m.world {
				targets = append(targets, c)
			}
		} else {
			c, bad := pick()
			if bad != nil {
				return nil, bad
			}
			targets = []map[string]any{c}
		}
		stopped := []int64{}
		var evs []any
		for _, c := range targets {
			if t, ok := c["task"].(map[string]any); ok {
				tid := toInt(t["id"])
				delete(m.tasks, tid)
				stopped = append(stopped, tid)
				c["task"], c["state"] = nil, companion.StateIdle
				evs = append(evs, map[string]any{"event": "task_finished", "companion": c["id"], "task": t, "result": "cancelled", "seconds": 3.0})
			}
		}
		sort.Slice(stopped, func(i, j int) bool { return stopped[i] < stopped[j] })
		return evs, map[string]any{"ok": true, "stopped": stopped}
	case "say":
		if _, bad := pick(); bad != nil {
			return nil, bad
		}
		if t, _ := req["text"].(string); t == "" {
			return nil, fail("bad_args", "text 不能是空的")
		}
		return nil, map[string]any{"ok": true}
	}
	return nil, fail("unknown_op", "不认识的指令："+op)
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}

func clone(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
