// Package bridge 是 MineDeck 和服务端桥接模组（bridge/voicechat，MineDeck Voice Bridge）之间的本机连接：
// 协议第 1 版的编解码、带握手 / 心跳 / 断线重连的客户端、模组配置文件 config/minedeck/voicebridge.properties
// 的读写，以及打进 exe 的模组 jar。协议说明见 bridge/voicechat/README.md 的「协议」一节。
//
// 这个包不引用 Windows API，Linux 上可以直接测试。
package bridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// 协议第 1 版，和模组里的 Protocol.java 一一对应。
// 每帧：类型 1 字节 | 保留 1 字节（填 0，收到时忽略） | 内容长度 2 字节 | 内容。所有整数都是小端。
const (
	ProtoVersion = 1
	SampleRate   = 48000
	FrameSamples = 960 // 每帧 20 毫秒
	HeaderSize   = 4
	MaxPayload   = 4096
	PCMPayload   = 4 + FrameSamples*2 // 序号 4 字节 + 960 个 16 位采样

	TypeHello   = 0x01 // MineDeck → 模组
	TypeWelcome = 0x02 // 模组 → MineDeck
	TypeError   = 0x03 // 模组 → MineDeck，发完就断开
	TypePCM     = 0x10 // MineDeck → 模组
	TypePing    = 0x20 // MineDeck → 模组
	TypePong    = 0x21 // 模组 → MineDeck
	TypeStatus  = 0x30 // 模组 → MineDeck，每 2 秒一次
	TypeEnd     = 0x40 // 双向，发完就断开

	ErrBadToken = 1 // 令牌不对
	ErrVersion  = 2 // 协议版本不支持
	ErrBusy     = 3 // 已经有一个广播连接
	ErrProtocol = 4 // 帧不合规矩
	ErrDisabled = 5 // 音乐共享已关闭

	SourceNone      = 0 // STATUS 里的音源：没有
	SourceBroadcast = 1 // 广播
	SourceSelftest  = 2 // 自测音
)

// Frame 一帧。
type Frame struct {
	Type    byte
	Payload []byte
}

// WriteFrame 写一帧。
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

// ReadFrame 读一帧。对方在帧与帧之间关闭连接时返回 io.EOF。
func ReadFrame(r io.Reader) (Frame, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	n := int(binary.LittleEndian.Uint16(h[2:]))
	if n > MaxPayload {
		return Frame{}, fmt.Errorf("帧太长：%d 字节，最多 %d", n, MaxPayload)
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return Frame{Type: h[0], Payload: p}, nil
}

// HelloPayload HELLO：协议版本 2 字节 | 令牌长度 1 字节 | 令牌 | 广播者名字（剩下的全部，可以为空）。
func HelloPayload(token, name string) ([]byte, error) {
	if len(token) > 255 {
		return nil, errors.New("令牌不能超过 255 字节")
	}
	if 3+len(token)+len(name) > MaxPayload {
		return nil, errors.New("广播者名字太长")
	}
	p := make([]byte, 3, 3+len(token)+len(name))
	binary.LittleEndian.PutUint16(p, ProtoVersion)
	p[2] = byte(len(token))
	p = append(p, token...)
	p = append(p, name...)
	return p, nil
}

// PCMFrame PCM：序号 4 字节 | 960 个 16 位小端采样（48 kHz 单声道）。采样不足 960 个时补静音。
func PCMFrame(seq uint32, samples []int16) []byte {
	p := make([]byte, PCMPayload)
	binary.LittleEndian.PutUint32(p, seq)
	for i := 0; i < FrameSamples && i < len(samples); i++ {
		binary.LittleEndian.PutUint16(p[4+2*i:], uint16(samples[i]))
	}
	return p
}

// Welcome 模组接受连接时的回复。
type Welcome struct {
	Version      int
	SampleRate   int
	FrameSamples int
	Channels     int
	Listeners    int
	ModVersion   string
}

// ParseWelcome WELCOME：协议版本 2 | 采样率 4 | 每帧采样数 2 | 声道数 1 | 当前听众数 2 | 模组版本（剩下的全部）。
func ParseWelcome(p []byte) (Welcome, error) {
	if len(p) < 11 {
		return Welcome{}, fmt.Errorf("WELCOME 太短：%d 字节", len(p))
	}
	le := binary.LittleEndian
	return Welcome{
		Version:      int(le.Uint16(p[0:])),
		SampleRate:   int(le.Uint32(p[2:])),
		FrameSamples: int(le.Uint16(p[6:])),
		Channels:     int(p[8]),
		Listeners:    int(le.Uint16(p[9:])),
		ModVersion:   string(p[11:]),
	}, nil
}

// Status 模组每 2 秒报一次的状态；计数从这次连接开始算。
type Status struct {
	Listeners int    `json:"listeners"` // 听众数
	Buffered  int    `json:"buffered"`  // 抖动缓冲里的帧数
	Source    int    `json:"source"`    // 正在推的音源（Source*）
	Received  uint32 `json:"received"`  // 收到的帧
	Played    uint32 `json:"played"`    // 编码的帧
	Sent      uint32 `json:"sent"`      // 发给听众的帧
	Dropped   uint32 `json:"dropped"`   // 缓冲满了丢掉的帧
	Underruns uint32 `json:"underruns"` // 缓冲取空的次数
}

// ParseStatus STATUS：听众数 2 | 缓冲帧数 2 | 音源 1 | 收到 4 | 编码 4 | 发出 4 | 丢弃 4 | 欠载 4。
func ParseStatus(p []byte) (Status, error) {
	if len(p) < 25 {
		return Status{}, fmt.Errorf("STATUS 太短：%d 字节", len(p))
	}
	le := binary.LittleEndian
	return Status{
		Listeners: int(le.Uint16(p[0:])),
		Buffered:  int(le.Uint16(p[2:])),
		Source:    int(p[4]),
		Received:  le.Uint32(p[5:]),
		Played:    le.Uint32(p[9:]),
		Sent:      le.Uint32(p[13:]),
		Dropped:   le.Uint32(p[17:]),
		Underruns: le.Uint32(p[21:]),
	}, nil
}

// ParseError ERROR：错误码 2 | 说明（UTF-8）。
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
		return fmt.Sprintf("桥接模组拒绝了连接：错误码 %d（%s）", e.Code, ErrorName(e.Code))
	}
	return fmt.Sprintf("桥接模组拒绝了连接：错误码 %d（%s）：%s", e.Code, ErrorName(e.Code), e.Msg)
}

// ErrorName 错误码的中文说明。
func ErrorName(code int) string {
	switch code {
	case ErrBadToken:
		return "令牌不对"
	case ErrVersion:
		return "协议版本不支持"
	case ErrBusy:
		return "已经有一个广播连接"
	case ErrProtocol:
		return "协议错误"
	case ErrDisabled:
		return "音乐共享已关闭"
	}
	return "未知错误"
}

// SourceName 音源的中文名。
func SourceName(s int) string {
	switch s {
	case SourceBroadcast:
		return "广播"
	case SourceSelftest:
		return "自测音"
	}
	return "无"
}
