package main

import "testing"

func TestMinTrackSize(t *testing.T) {
	cases := []struct {
		name         string
		dpi          uint32
		workW, workH int32
		wantW, wantH int32
	}{
		{"100% 缩放", 96, 1920, 1040, 1024, 576},
		{"125% 缩放", 120, 1920, 1040, 1280, 720},
		{"150% 缩放", 144, 2560, 1400, 1536, 864},
		{"200% 缩放", 192, 3840, 2100, 2048, 1152},
		{"取不到 DPI 按 96 算", 0, 1920, 1040, 1024, 576},
		{"取不到工作区就不收", 144, 0, 0, 1536, 864},
		{"1366×768 的笔记本配 150%：收到工作区以内", 144, 1366, 728, 1366, 728},
		{"只有高度不够", 120, 1600, 680, 1280, 680},
	}
	for _, c := range cases {
		w, h := minTrackSize(c.dpi, c.workW, c.workH)
		if w != c.wantW || h != c.wantH {
			t.Errorf("%s：minTrackSize(%d, %d, %d) = %d×%d，应为 %d×%d", c.name, c.dpi, c.workW, c.workH, w, h, c.wantW, c.wantH)
		}
	}
	// 没被工作区收小的时候是 16:9
	if w, h := minTrackSize(96, 0, 0); w*9 != h*16 {
		t.Errorf("最小窗口应为 16:9，实际 %d×%d", w, h)
	}
}
