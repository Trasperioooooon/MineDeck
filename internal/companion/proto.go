// Package companion 是 MineDeck 这一侧的 AI 伙伴（设计文档 §7.2「第二步」）：和伙伴模组之间的本机连接（协议 v1，
// 见 companion/README.md 的「协议 v1」，以它为准）、模组配置文件 config/minedeck/companion.properties 的读写、
// 伙伴模组和它依赖的 PlayerEngine、Architectury API 的安装（后两个按清单从 Modrinth 下载，tested-first，不自动追新），
// 以及每个实例一个的管理器：服务器在运行、又启用了伙伴时自动连上模组，跟踪伙伴和任务，处理事件，关服和退出时收尾。
//
// 所有 LLM 调用都不在这里：游戏内 AI（internal/chatai）把伙伴的动作当成工具，经 Service.Act 交给模组执行；
// 任务做完或失败时，这里只按模板写好一句话交回去（Deps.OnFinish），不再调用 AI。
//
// 内置的 jar 和清单在子包 internal/companion/jar（companionjar），这里只读不改。这个包不引用 Windows API，Linux 上可以直接测试。
package companion

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 协议 v1 的帧（README「帧」一节）：类型 1 字节 | 保留 1 字节（填 0，收到时忽略）| 内容长度 2 字节（小端）| 内容。
const (
	ProtoVersion = 1 // MineDeck 支持的最高版本（HELLO 里写它）
	HeaderSize   = 4
	MaxPayload   = 65535

	TypeHello    = 0x01 // MineDeck → 模组：协议版本 u16、令牌长度 u8、令牌、客户端名字
	TypeWelcome  = 0x02 // 模组 → MineDeck：协议版本 u16 + JSON
	TypeError    = 0x03 // 模组 → MineDeck：错误码 u16 + 说明，发完就断开，只在握手时用
	TypeRequest  = 0x10 // MineDeck → 模组：JSON 指令
	TypeResponse = 0x11 // 模组 → MineDeck：JSON 回复，每条指令正好一条
	TypeEvent    = 0x12 // 模组 → MineDeck：JSON 事件
	TypePing     = 0x20 // MineDeck → 模组：最多 64 字节
	TypePong     = 0x21 // 模组 → MineDeck：原样带回
	TypeStatus   = 0x30 // 模组 → MineDeck：JSON，每 2 秒一次
	TypeEnd      = 0x40 // 双向：原因（UTF-8），发完就断开

	MaxPing = 64

	// ERROR 的错误码
	ErrBadToken = 1 // 令牌不对
	ErrVersion  = 2 // 协议版本不支持
	ErrBusy     = 3 // 已经有一个控制连接
	ErrProtocol = 4 // 协议错误（第一帧不是 HELLO、HELLO 格式不对、帧太长）
	ErrClosing  = 5 // 控制端口正在关闭（配置里刚改成 enabled=false，或者服务器正在关闭）

	MinTokenLength = 16
	MaxTokenLength = 255
)

// Frame 一帧。
type Frame struct {
	Type    byte
	Payload []byte
}

// WriteFrame 写一帧（一次 Write：同一个连接上几个协程交替写时帧不会交错，调用方仍要自己加锁）。
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("帧太长：%d 字节，最多 %d", len(payload), MaxPayload)
	}
	buf := make([]byte, HeaderSize+len(payload))
	buf[0] = typ
	binary.LittleEndian.PutUint16(buf[2:], uint16(len(payload)))
	copy(buf[HeaderSize:], payload)
	_, err := w.Write(buf)
	return err
}

// ReadFrame 读一帧。对方在帧与帧之间关闭连接时返回 io.EOF。保留字节不看。
func ReadFrame(r io.Reader) (Frame, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	p := make([]byte, int(binary.LittleEndian.Uint16(h[2:])))
	if _, err := io.ReadFull(r, p); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return Frame{Type: h[0], Payload: p}, nil
}

// ValidToken 令牌是不是 16–255 个可见 ASCII 字符（模组的要求）。
func ValidToken(t string) bool {
	if len(t) < MinTokenLength || len(t) > MaxTokenLength {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] < 0x21 || t[i] > 0x7e {
			return false
		}
	}
	return true
}

// HelloPayload HELLO：协议版本 u16（MineDeck 支持的最高版本）| 令牌长度 u8 | 令牌 | 客户端名字（剩下的全部，只用来记日志）。
func HelloPayload(version int, token, client string) ([]byte, error) {
	if !ValidToken(token) {
		return nil, fmt.Errorf("令牌要是 %d–%d 个可见的 ASCII 字符", MinTokenLength, MaxTokenLength)
	}
	if version < 0 || version > 0xffff {
		return nil, fmt.Errorf("协议版本不对：%d", version)
	}
	if 3+len(token)+len(client) > MaxPayload {
		return nil, errors.New("客户端名字太长")
	}
	p := make([]byte, 3, 3+len(token)+len(client))
	binary.LittleEndian.PutUint16(p, uint16(version))
	p[2] = byte(len(token))
	p = append(p, token...)
	return append(p, client...), nil
}

// ParseWelcome WELCOME：协议版本 u16（这次连接用的版本），后面是 JSON。
func ParseWelcome(p []byte) (int, []byte, error) {
	if len(p) < 2 {
		return 0, nil, fmt.Errorf("WELCOME 太短：%d 字节", len(p))
	}
	return int(binary.LittleEndian.Uint16(p)), p[2:], nil
}

// ParseError ERROR：错误码 u16 | 说明（UTF-8）。
func ParseError(p []byte) (int, string) {
	if len(p) < 2 {
		return 0, ""
	}
	return int(binary.LittleEndian.Uint16(p)), string(p[2:])
}

// ModError 模组用 ERROR 帧拒绝了连接。
type ModError struct {
	Code int
	Msg  string
}

func (e *ModError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("伙伴模组拒绝了连接：错误码 %d（%s）", e.Code, ErrorName(e.Code))
	}
	return fmt.Sprintf("伙伴模组拒绝了连接：错误码 %d（%s）：%s", e.Code, ErrorName(e.Code), e.Msg)
}

// ErrorName ERROR 错误码的中文说明。
func ErrorName(code int) string {
	switch code {
	case ErrBadToken:
		return "令牌不对"
	case ErrVersion:
		return "协议版本不支持"
	case ErrBusy:
		return "已经有一个控制连接"
	case ErrProtocol:
		return "协议错误"
	case ErrClosing:
		return "控制端口正在关闭"
	}
	return "未知错误"
}

// ---------- JSON ----------

// Welcome WELCOME 的 JSON（README「WELCOME 的 JSON」）。不认识的字段一律忽略（兼容规则 1）。
type Welcome struct {
	Protocol     int         `json:"protocol"`
	Session      string      `json:"session"` // 每次开服随机生成：变了说明服务器重启过（任务编号从 1 重新数）
	Mod          string      `json:"mod"`     // 模组版本
	Loader       string      `json:"loader"`
	Minecraft    string      `json:"minecraft"`
	PlayerEngine string      `json:"playerEngine"`
	Ops          []string    `json:"ops"`      // 支持的指令
	Events       []string    `json:"events"`   // 会发的事件
	Features     []string    `json:"features"` // 可选能力：nodig、adopt
	Limits       Limits      `json:"limits"`
	Config       ModConfig   `json:"config"`     // 模组现在实际在用的配置
	Companions   []Companion `json:"companions"` // 现有的伙伴（不带背包）
}

// Limits 模组的几个上限。
type Limits struct {
	MaxCount int `json:"maxCount"`
	MaxText  int `json:"maxText"`
	MaxName  int `json:"maxName"`
	MaxZones int `json:"maxZones"`
}

// ModConfig 模组现在实际在用的配置（WELCOME、status 回复和 config_applied 事件里的 config）。
type ModConfig struct {
	MaxCompanions int      `json:"maxCompanions"`
	AttackDeny    []string `json:"attackDeny"`
	Zones         []Zone   `json:"zones"`
	Problems      Texts    `json:"problems"` // 配置文件里写错、没生效的条目
}

// Zone 一个禁挖区域：维度和两个角（方块坐标，包含两端）。模组报来的是 min / max；MineDeck 自己读配置文件时照写的顺序。
type Zone struct {
	Name string `json:"name"`
	Dim  string `json:"dim"`
	Min  [3]int `json:"min"`
	Max  [3]int `json:"max"`
}

// Texts 一组说明文字。协议只说是数组；元素不是字符串时（以后可能改成对象）原样编成一行 JSON，不当成错误。
type Texts []string

func (t *Texts) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
			*t = nil
			return nil
		}
		return err
	}
	out := make(Texts, 0, len(raw))
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
			continue
		}
		var m map[string]any
		if json.Unmarshal(r, &m) == nil {
			for _, k := range []string{"message", "error", "detail", "text"} {
				if v, ok := m[k].(string); ok && v != "" {
					s = v
					break
				}
			}
		}
		if s == "" {
			s = string(r)
		}
		out = append(out, s)
	}
	*t = out
	return nil
}

// Companion 伙伴对象（README「伙伴对象」）。
type Companion struct {
	ID         int        `json:"id"`
	UUID       string     `json:"uuid"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner,omitempty"` // null 时为空串
	OwnerUUID  string     `json:"ownerUuid,omitempty"`
	Tag        string     `json:"tag,omitempty"`
	Dim        string     `json:"dim"`
	X          float64    `json:"x"`
	Y          float64    `json:"y"`
	Z          float64    `json:"z"`
	Health     float64    `json:"health"`
	MaxHealth  float64    `json:"maxHealth"`
	State      string     `json:"state"` // starting / idle / busy；不认识的当成 busy（兼容规则 4，见 KnownState）
	Task       *Task      `json:"task"`
	LastResult *Result    `json:"lastResult"`
	EngineTask string     `json:"engineTask,omitempty"`
	Inventory  []ItemSlot `json:"inventory,omitempty"`
	Selected   *int       `json:"selectedSlot,omitempty"`
}

// 伙伴的状态。
const (
	StateStarting = "starting"
	StateIdle     = "idle"
	StateBusy     = "busy"
)

// KnownState 不认识的 state 当成 busy（兼容规则 4）。
func KnownState(s string) string {
	if s == StateStarting || s == StateIdle || s == StateBusy {
		return s
	}
	return StateBusy
}

// ItemSlot 背包的一格（status 带 inventory 时才有）。
type ItemSlot struct {
	Slot  int    `json:"slot"`
	Item  string `json:"item"`
	Count int    `json:"count"`
}

// Task 任务对象。
type Task struct {
	ID        int64           `json:"id"`
	Companion int             `json:"companion"`
	Op        string          `json:"op"`
	Args      json.RawMessage `json:"args,omitempty"`
	Timeout   int             `json:"timeout"`
	StartedAt int64           `json:"startedAt"` // 毫秒时间戳
	Command   string          `json:"command,omitempty"`
}

// TaskRef task 字段：任务对象，或者只有任务编号（stuck 事件里是编号）。
type TaskRef struct{ Task }

func (t *TaskRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		return json.Unmarshal(b, &t.Task)
	}
	return json.Unmarshal(b, &t.ID)
}

func (t TaskRef) MarshalJSON() ([]byte, error) { return json.Marshal(t.Task) }

// Pos 伙伴的位置。
type Pos struct {
	Dim string  `json:"dim"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Z   float64 `json:"z"`
}

// Result 一个任务的结果：task_finished 事件和伙伴对象的 lastResult 共用（后者多一个 at）。
type Result struct {
	Task    *TaskRef `json:"task,omitempty"`
	Result  string   `json:"result"` // done / incomplete / failed / cancelled / superseded / timeout
	Detail  string   `json:"detail,omitempty"`
	Seconds float64  `json:"seconds"`
	Pos     *Pos     `json:"pos,omitempty"`
	Have    *int     `json:"have,omitempty"`
	Gained  *int     `json:"gained,omitempty"`
	Want    *int     `json:"want,omitempty"`
	Killed  *int     `json:"killed,omitempty"`
	Given   *int     `json:"given,omitempty"`
	At      int64    `json:"at,omitempty"` // lastResult：结束时的毫秒时间戳
}

// 任务的结果。
const (
	ResultDone       = "done"
	ResultIncomplete = "incomplete"
	ResultFailed     = "failed"
	ResultCancelled  = "cancelled"
	ResultSuperseded = "superseded"
	ResultTimeout    = "timeout"
)

// KnownResult 不认识的 result 当成 failed（兼容规则 4）。
func KnownResult(r string) string {
	switch r {
	case ResultDone, ResultIncomplete, ResultFailed, ResultCancelled, ResultSuperseded, ResultTimeout:
		return r
	}
	return ResultFailed
}

// Event 一个 EVENT（各种事件的字段并在一起，没有的就是零值）。
type Event struct {
	Event     string     `json:"event"`
	Companion int        `json:"companion,omitempty"`
	Info      *Companion `json:"info,omitempty"`   // spawned、adopted
	Task      *TaskRef   `json:"task,omitempty"`   // task_started、task_finished（对象）；stuck（编号，没有任务时为 null）
	Result    string     `json:"result,omitempty"` // task_finished
	Detail    string     `json:"detail,omitempty"`
	Seconds   float64    `json:"seconds,omitempty"`
	Pos       *Pos       `json:"pos,omitempty"`
	Have      *int       `json:"have,omitempty"`
	Gained    *int       `json:"gained,omitempty"`
	Want      *int       `json:"want,omitempty"`
	Killed    *int       `json:"killed,omitempty"`
	Given     *int       `json:"given,omitempty"`
	Reason    string     `json:"reason,omitempty"` // stuck：no_progress / nodig；removed：despawn / died / discarded / lost
	UUID      string     `json:"uuid,omitempty"`   // died、removed
	Name      string     `json:"name,omitempty"`
	Cause     string     `json:"cause,omitempty"`  // died：死亡信息
	Config    *ModConfig `json:"config,omitempty"` // config_applied
}

// 事件名。
const (
	EvSpawned       = "spawned"
	EvAdopted       = "adopted"
	EvRemoved       = "removed"
	EvDied          = "died"
	EvTaskStarted   = "task_started"
	EvTaskFinished  = "task_finished"
	EvStuck         = "stuck"
	EvConfigApplied = "config_applied"
)

// AsResult task_finished 事件里的结果部分。
func (e Event) AsResult() Result {
	return Result{Task: e.Task, Result: e.Result, Detail: e.Detail, Seconds: e.Seconds, Pos: e.Pos, Have: e.Have, Gained: e.Gained,
		Want: e.Want, Killed: e.Killed, Given: e.Given}
}

// Status STATUS 帧：{"companions": [伙伴对象…]}，不带背包。
type Status struct {
	Companions []Companion `json:"companions"`
}

// Response 一条 RESPONSE。成功 {"id":1,"ok":true,...}；失败 {"id":1,"ok":false,"code":"no_player","error":"…"}。
// Raw 是整条回复的原文，各指令自己的字段（companion、info、task、removed、stopped、companions、config……）从里面取。
type Response struct {
	ID    json.RawMessage `json:"id"` // 取不到时是 null
	OK    bool            `json:"ok"`
	Code  string          `json:"code,omitempty"`
	Error string          `json:"error,omitempty"`
	Raw   json.RawMessage `json:"-"`
}

// Decode 把回复解进 v（取各指令自己的字段）。
func (r *Response) Decode(v any) error { return json.Unmarshal(r.Raw, v) }

// RESPONSE 的错误码（code）。不认识的当成一般失败（兼容规则 4）。
const (
	CodeBadJSON     = "bad_json"
	CodeUnknownOp   = "unknown_op"
	CodeBadArgs     = "bad_args"
	CodeNoCompanion = "no_companion"
	CodeAmbiguous   = "ambiguous"
	CodeNotReady    = "not_ready"
	CodeLimit       = "limit"
	CodeNameTaken   = "name_taken"
	CodeNoPlayer    = "no_player"
	CodeNotEnough   = "not_enough"
	CodeForbidden   = "forbidden"
	CodeRejected    = "rejected"
	CodeSpawnFailed = "spawn_failed"
	CodeInternal    = "internal"
)

// OpError 模组回了 ok=false。
type OpError struct {
	Op    string
	Code  string
	Msg   string
	Reply *Response
}

func (e *OpError) Error() string {
	msg := strings.TrimSpace(e.Msg)
	if msg == "" {
		msg = codeName(e.Code)
	}
	return msg
}

// codeName 错误码的中文说明（模组的 error 是空的时候用）。
func codeName(code string) string {
	switch code {
	case CodeBadJSON:
		return "指令不是合法的 JSON"
	case CodeUnknownOp:
		return "伙伴模组不认识这个指令（可能要更新模组）"
	case CodeBadArgs:
		return "参数不对"
	case CodeNoCompanion:
		return "没有这个伙伴"
	case CodeAmbiguous:
		return "有好几个伙伴，要说清楚是哪一个"
	case CodeNotReady:
		return "伙伴刚刷出来，还没准备好，稍后再试"
	case CodeLimit:
		return "伙伴数量到上限了"
	case CodeNameTaken:
		return "已经有一个同名的伙伴"
	case CodeNoPlayer:
		return "玩家不在线"
	case CodeNotEnough:
		return "背包里不够"
	case CodeForbidden:
		return "不许这样做"
	case CodeRejected:
		return "PlayerEngine 不接受这条指令"
	case CodeSpawnFailed:
		return "伙伴没能加进世界"
	case CodeInternal:
		return "伙伴模组内部出错（看服务器日志）"
	}
	return "伙伴模组拒绝了这条指令（" + code + "）"
}
