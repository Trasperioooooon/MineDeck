package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

// source 每次给出一帧 960 个采样（48 kHz 单声道）；没有了返回 false。
type source interface {
	next() ([]int16, bool)
}

// toneSource：正弦波；freq 为 0 时播放和模组自测音一样的 C 大调琶音。开头 10 毫秒淡入，避免爆音。
type toneSource struct {
	freq float64
	gain float64
	pos  int64
}

var scaleNotes = []float64{523.25, 659.25, 783.99, 1046.50, 783.99, 659.25}

func (t *toneSource) next() ([]int16, bool) {
	out := make([]int16, frameSamples)
	const noteLen = sampleRate * 300 / 1000
	for i := range out {
		n := t.pos + int64(i)
		var v float64
		if t.freq > 0 {
			v = math.Sin(2 * math.Pi * t.freq * float64(n) / sampleRate)
		} else {
			note := scaleNotes[(n/noteLen)%int64(len(scaleNotes))]
			k := n % noteLen
			env := 1.0
			if k < 240 {
				env = float64(k) / 240
			} else if k > noteLen-2880 {
				env = float64(noteLen-k) / 2880
			}
			v = math.Sin(2*math.Pi*note*float64(k)/sampleRate) * env
		}
		if n < 480 {
			v *= float64(n) / 480
		}
		out[i] = toInt16(v * t.gain)
	}
	t.pos += frameSamples
	return out, true
}

// pcmSource：已经转成 48 kHz 单声道的整段音频（来自 WAV 文件）。
type pcmSource struct {
	data []float32
	gain float64
	pos  int
	loop bool
}

func (s *pcmSource) next() ([]int16, bool) {
	if s.pos >= len(s.data) {
		if !s.loop || len(s.data) == 0 {
			return nil, false
		}
		s.pos = 0
	}
	out := make([]int16, frameSamples)
	for i := range out {
		if s.pos+i < len(s.data) {
			out[i] = toInt16(float64(s.data[s.pos+i]) * s.gain)
		}
	}
	s.pos += frameSamples
	return out, true
}

// limited：最多给出 n 帧（n < 0 表示不限）。
type limited struct {
	src source
	n   int64
}

func (l *limited) next() ([]int16, bool) {
	if l.n == 0 {
		return nil, false
	}
	if l.n > 0 {
		l.n--
	}
	return l.src.next()
}

func toInt16(v float64) int16 {
	v = math.Round(v * 32767)
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// loadWAV 读 WAV 文件，转成 48 kHz 单声道。支持 16/24/32 位整数 PCM 和 32 位浮点，任意声道数（取平均）
// 和采样率（线性插值重采样，够测试用）。
func loadWAV(path string) ([]float32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeWAV(b)
}

func decodeWAV(b []byte) ([]float32, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("不是 WAV 文件")
	}
	le := binary.LittleEndian
	var (
		format, channels, bits int
		rate                   int
		data                   []byte
		haveFmt                bool
	)
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(le.Uint32(b[off+4:]))
		body := off + 8
		end := body + size
		if end > len(b) {
			end = len(b)
		}
		switch id {
		case "fmt ":
			if end-body < 16 {
				return nil, errors.New("WAV 的 fmt 块太短")
			}
			format = int(le.Uint16(b[body:]))
			channels = int(le.Uint16(b[body+2:]))
			rate = int(le.Uint32(b[body+4:]))
			bits = int(le.Uint16(b[body+14:]))
			if format == 0xFFFE && end-body >= 26 {
				format = int(le.Uint16(b[body+24:])) // WAVE_FORMAT_EXTENSIBLE：子格式 GUID 的前两个字节
			}
			haveFmt = true
		case "data":
			data = b[body:end]
		}
		off = body + size + size%2
	}
	if !haveFmt || data == nil {
		return nil, errors.New("WAV 文件缺少 fmt 或 data 块")
	}
	if channels < 1 || rate < 1 {
		return nil, errors.New("WAV 的声道数或采样率不对")
	}
	var sampleBytes int
	switch {
	case format == 1 && (bits == 16 || bits == 24 || bits == 32):
		sampleBytes = bits / 8
	case format == 3 && bits == 32:
		sampleBytes = 4
	default:
		return nil, fmt.Errorf("不支持的 WAV 格式（格式 %d，%d 位），请用 16 位 PCM 或 32 位浮点", format, bits)
	}
	frames := len(data) / (sampleBytes * channels)
	mono := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float64
		for c := 0; c < channels; c++ {
			p := data[(i*channels+c)*sampleBytes:]
			switch {
			case format == 3:
				sum += float64(math.Float32frombits(le.Uint32(p)))
			case bits == 16:
				sum += float64(int16(le.Uint16(p))) / 32768
			case bits == 24:
				v := int32(p[0]) | int32(p[1])<<8 | int32(int8(p[2]))<<16
				sum += float64(v) / 8388608
			case bits == 32:
				sum += float64(int32(le.Uint32(p))) / 2147483648
			}
		}
		mono[i] = float32(sum / float64(channels))
	}
	return resample(mono, rate, sampleRate), nil
}

func resample(in []float32, from, to int) []float32 {
	if from == to || len(in) == 0 {
		return in
	}
	n := int(int64(len(in)) * int64(to) / int64(from))
	out := make([]float32, n)
	step := float64(from) / float64(to)
	for i := range out {
		x := float64(i) * step
		j := int(x)
		frac := float32(x - float64(j))
		a := in[j]
		bv := a
		if j+1 < len(in) {
			bv = in[j+1]
		}
		out[i] = a + (bv-a)*frac
	}
	return out
}
