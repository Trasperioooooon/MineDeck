package music

import (
	"errors"
	"strings"
	"testing"
)

// 版本号判断：≥ 20348 用按进程录音；更低的只能整机录音并要警告。
func TestCapabilityFor(t *testing.T) {
	cases := []struct {
		major, minor, build uint32
		mode                Mode
		warn                bool
		name                string
	}{
		{10, 0, 19045, ModeSystem, true, "Windows 10"}, // Win10 22H2，最后一版
		{10, 0, 20347, ModeSystem, true, "Windows 10"},
		{10, 0, 20348, ModeProcess, false, "Windows Server 2022"},
		{10, 0, 22000, ModeProcess, false, "Windows 11"},
		{10, 0, 26100, ModeProcess, false, "Windows 11"},
		{6, 3, 9600, ModeSystem, true, "Windows 8.1"},
		{11, 0, 100, ModeProcess, false, "新版本"},
	}
	for _, c := range cases {
		got := capabilityFor(c.major, c.minor, c.build)
		if got.Mode != c.mode || got.Warn != c.warn {
			t.Errorf("%d.%d.%d：得到 %v（警告 %v），应为 %v（警告 %v）", c.major, c.minor, c.build, got.Mode, got.Warn, c.mode, c.warn)
		}
		if s := got.String(); !strings.Contains(s, c.name) {
			t.Errorf("%d.%d.%d：String() = %q，应含 %q", c.major, c.minor, c.build, s, c.name)
		}
	}
}

func TestHRText(t *testing.T) {
	cases := map[uint32]string{
		0x00000000: "0x00000000 S_OK",
		0x88890008: "0x88890008 AUDCLNT_E_UNSUPPORTED_FORMAT",
		0x8000000E: "0x8000000E E_ILLEGAL_METHOD_CALL",
		0x80070490: "0x80070490 E_NOTFOUND",
		0x80070020: "0x80070020 Win32 错误 32",
		0x00000002: "0x00000002 成功",
		0x80123456: "0x80123456 未知错误",
	}
	for code, want := range cases {
		if got := HRText(code); !strings.HasPrefix(got, want) {
			t.Errorf("HRText(0x%08X) = %q，应以 %q 开头", code, got, want)
		}
	}
	var err error = &HRError{Step: "IAudioClient::Initialize", Code: 0x88890008}
	if s := err.Error(); s != "IAudioClient::Initialize → 0x88890008 AUDCLNT_E_UNSUPPORTED_FORMAT（不支持该采样格式）" {
		t.Errorf("HRError.Error() = %q", s)
	}
	var he *HRError
	if !errors.As(err, &he) || he.Code != 0x88890008 {
		t.Errorf("errors.As 取不到 HRError")
	}
}

func TestPeakOf(t *testing.T) {
	if p := PeakOf(nil); p != 0 {
		t.Errorf("空采样峰值 = %v", p)
	}
	if p := PeakOf([]int16{3, -16384, 100}); p != 0.5 {
		t.Errorf("峰值 = %v，应为 0.5", p)
	}
	if p := PeakOf([]int16{-32768}); p != 1 {
		t.Errorf("-32768 的峰值 = %v，应为 1", p)
	}
}

func TestAttemptsFor(t *testing.T) {
	names := func(list []initAttempt) string {
		var s []string
		for _, a := range list {
			s = append(s, a.name)
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		mode   Mode
		format string
		want   string
	}{
		{ModeProcess, "", "float,pcm16,pcm16-44k"},
		{ModeSystem, "", "float,native"},
		{ModeProcess, FormatPCM16, "pcm16"},
		{ModeSystem, FormatNative, "native"},
		{ModeSystem, FormatPCM16At44k, "pcm16-44k"},
	}
	for _, c := range cases {
		list, err := attemptsFor(c.mode, c.format)
		if err != nil {
			t.Fatalf("%v/%q：%v", c.mode, c.format, err)
		}
		if got := names(list); got != c.want {
			t.Errorf("%v/%q：得到 %s，应为 %s", c.mode, c.format, got, c.want)
		}
		for _, a := range list {
			if a.native != (a.wave == nil) {
				t.Errorf("%s：native=%v 但 wave 长度 %d", a.name, a.native, len(a.wave))
			}
		}
	}
	if _, err := attemptsFor(ModeProcess, FormatNative); err == nil {
		t.Error("按进程录音不应接受 native")
	}
	if _, err := attemptsFor(ModeSystem, "mp3"); err == nil {
		t.Error("未知格式应报错")
	}
}

func TestValidate(t *testing.T) {
	bad := []Options{
		{},
		{Mode: ModeProcess},
		{Mode: ModeProcess, PID: 1, Format: FormatNative},
		{Mode: ModeSystem, Gain: -1},
		{Mode: 9},
		{Mode: ModeProcess, PID: 1, Device: "{0.0.0.00000000}.{x}"}, // 只有整机录音能指定设备
	}
	for _, o := range bad {
		if err := validate(o); err == nil {
			t.Errorf("%+v 应报错", o)
		}
	}
	good := []Options{
		{Mode: ModeProcess, PID: 1234},
		{Mode: ModeSystem},
		{Mode: ModeSystem, Format: FormatNative, Gain: 2},
		{Mode: ModeSystem, Device: "{0.0.0.00000000}.{x}"},
	}
	for _, o := range good {
		if err := validate(o); err != nil {
			t.Errorf("%+v：%v", o, err)
		}
	}
}

func TestStreamFlagNames(t *testing.T) {
	got := streamFlagNames(streamFlagsLoopback | streamFlagsEventCallback | streamFlagsAutoConvertPCM | streamFlagsSrcDefaultQuality)
	if got != "LOOPBACK|EVENTCALLBACK|AUTOCONVERTPCM|SRC_DEFAULT_QUALITY" {
		t.Errorf("得到 %q", got)
	}
}
