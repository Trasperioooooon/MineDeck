package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func wavBytes(format, channels, rate, bits int, data []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	_ = binary.Write(&b, le, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, le, uint32(16))
	_ = binary.Write(&b, le, uint16(format))
	_ = binary.Write(&b, le, uint16(channels))
	_ = binary.Write(&b, le, uint32(rate))
	_ = binary.Write(&b, le, uint32(rate*channels*bits/8))
	_ = binary.Write(&b, le, uint16(channels*bits/8))
	_ = binary.Write(&b, le, uint16(bits))
	b.WriteString("data")
	_ = binary.Write(&b, le, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func TestDecodeWAVStereo16Resampled(t *testing.T) {
	// 44.1 kHz 立体声：左声道 +0.5，右声道 -0.5 → 混成单声道应该是 0
	const n = 44100
	data := make([]byte, n*4)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(data[i*4:], uint16(int16(16384)))
		binary.LittleEndian.PutUint16(data[i*4+2:], uint16(0xC000)) // -16384
	}
	pcm, err := decodeWAV(wavBytes(1, 2, 44100, 16, data))
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 48000 {
		t.Fatalf("重采样后应为 48000 个采样，实际 %d", len(pcm))
	}
	for i, v := range pcm {
		if math.Abs(float64(v)) > 1e-6 {
			t.Fatalf("第 %d 个采样 %v，应为 0", i, v)
		}
	}
}

func TestDecodeWAVFloatMono(t *testing.T) {
	data := make([]byte, 4*3)
	for i, v := range []float32{0.25, -0.5, 1} {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
	}
	pcm, err := decodeWAV(wavBytes(3, 1, 48000, 32, data))
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 3 || pcm[0] != 0.25 || pcm[1] != -0.5 || pcm[2] != 1 {
		t.Fatalf("解码结果不对：%v", pcm)
	}
	if _, err := decodeWAV(wavBytes(1, 1, 48000, 8, []byte{1, 2})); err == nil {
		t.Fatal("8 位 WAV 应该报不支持")
	}
}

func TestLimitedSource(t *testing.T) {
	src := &limited{src: &toneSource{freq: 440, gain: 0.3}, n: 3}
	for i := 0; i < 3; i++ {
		f, ok := src.next()
		if !ok || len(f) != frameSamples {
			t.Fatalf("第 %d 帧不对", i)
		}
	}
	if _, ok := src.next(); ok {
		t.Fatal("应该只给 3 帧")
	}
}
