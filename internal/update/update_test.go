package update

import "testing"

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		tag, current string
		want         bool
	}{
		{"v2.9.0", "2.6.1", true},
		{"v2.6.1", "2.6.1", false},
		{"v2.6.0", "2.6.1", false},
		{"v2.10.0", "2.9.0", true},
		// 同号的正式版比测试版新，测试版不比同号正式版新
		{"v2.9.0", "2.9.0-beta.1", true},
		{"v2.9.0-beta.1", "2.9.0", false},
		{"v2.9.0-beta.2", "2.9.0-beta.1", false}, // 只比正式版（检查更新取的是 latest，不含预发布）
		{"v2.9.1", "2.9.0-beta.1", true},
		{"v2.8.9", "2.9.0-beta.1", false},
	} {
		if got := newer(tc.tag, tc.current); got != tc.want {
			t.Errorf("newer(%q, %q) = %v，应为 %v", tc.tag, tc.current, got, tc.want)
		}
	}
}
