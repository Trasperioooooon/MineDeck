package bridge

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// 帧格式和 README / Protocol.java 一致：类型、保留 0、小端长度、内容；PCM 帧 1924 字节、采样小端。
func TestFrameLayout(t *testing.T) {
	samples := make([]int16, FrameSamples)
	for i := range samples {
		samples[i] = int16(i*37 - 16000)
	}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypePCM, PCMFrame(7, samples)); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if len(raw) != HeaderSize+PCMPayload || PCMPayload != 1924 {
		t.Fatalf("PCM 帧长度 %d", len(raw))
	}
	if raw[0] != TypePCM || raw[1] != 0 || binary.LittleEndian.Uint16(raw[2:]) != PCMPayload {
		t.Fatalf("帧头不对：% x", raw[:4])
	}
	f, err := ReadFrame(bytes.NewReader(raw))
	if err != nil || f.Type != TypePCM || binary.LittleEndian.Uint32(f.Payload) != 7 {
		t.Fatalf("读回：%v %+v", err, f.Type)
	}
	for i, want := range samples {
		if got := int16(binary.LittleEndian.Uint16(f.Payload[4+2*i:])); got != want {
			t.Fatalf("第 %d 个采样 %d，应为 %d", i, got, want)
		}
	}
	// 不足 960 个采样补静音
	short := PCMFrame(1, []int16{5})
	if len(short) != PCMPayload || binary.LittleEndian.Uint16(short[4:]) != 5 || short[6] != 0 {
		t.Fatal("短帧应补静音")
	}
	if err := WriteFrame(io.Discard, TypePing, make([]byte, MaxPayload+1)); err == nil {
		t.Fatal("超长帧应报错")
	}
	// 读到一半断开 / 声明的长度超限
	if _, err := ReadFrame(bytes.NewReader(raw[:10])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("帧中间断开：%v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("帧之间断开应是 EOF：%v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{TypePing, 0, 0xff, 0xff})); err == nil {
		t.Fatal("长度超限应报错")
	}
}

func TestHelloWelcomeStatusError(t *testing.T) {
	p, err := HelloPayload("0123456789abcdef", "房主")
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(p) != ProtoVersion || p[2] != 16 || string(p[3:19]) != "0123456789abcdef" || string(p[19:]) != "房主" {
		t.Fatalf("HELLO 布局不对：% x", p)
	}
	if _, err := HelloPayload(string(make([]byte, 256)), ""); err == nil {
		t.Fatal("超长令牌应报错")
	}

	w := make([]byte, 11)
	binary.LittleEndian.PutUint16(w[0:], 1)
	binary.LittleEndian.PutUint32(w[2:], 48000)
	binary.LittleEndian.PutUint16(w[6:], 960)
	w[8] = 1
	binary.LittleEndian.PutUint16(w[9:], 3)
	got, err := ParseWelcome(append(w, "0.1.0"...))
	if err != nil || got != (Welcome{1, 48000, 960, 1, 3, "0.1.0"}) {
		t.Fatalf("WELCOME：%+v %v", got, err)
	}
	if _, err := ParseWelcome(w[:10]); err == nil {
		t.Fatal("WELCOME 太短应报错")
	}

	s := make([]byte, 25)
	binary.LittleEndian.PutUint16(s[0:], 2)
	binary.LittleEndian.PutUint16(s[2:], 3)
	s[4] = SourceBroadcast
	for i, v := range []uint32{100, 99, 98, 4, 1} {
		binary.LittleEndian.PutUint32(s[5+4*i:], v)
	}
	st, err := ParseStatus(s)
	if err != nil || st != (Status{2, 3, 1, 100, 99, 98, 4, 1}) {
		t.Fatalf("STATUS：%+v %v", st, err)
	}

	e := append([]byte{ErrBusy, 0}, "已经有一个广播连接"...)
	if code, msg := ParseError(e); code != ErrBusy || msg != "已经有一个广播连接" {
		t.Fatalf("ERROR：%d %q", code, msg)
	}
	if s := (&ModError{Code: ErrBadToken, Msg: "令牌不对"}).Error(); s != "桥接模组拒绝了连接：错误码 1（令牌不对）：令牌不对" {
		t.Fatal(s)
	}
}
