package inst

import "testing"

// Mac 版的 InsideReal（共用的 TestInsideRealIsExact 用的是 Windows 的设备路径，在 Mac 上不适用）。
// 空的根什么都不算在里面，和 Windows 版一样；根目录 / 本身什么都算在里面。
func TestInsideRealDarwin(t *testing.T) {
	cases := []struct {
		root, real string
		want       bool
	}{
		{"", "/Users/a", false},
		{"", "", false},
		{"/", "/Users/a", true},
		{"/Users/a/Foo", "/Users/a/Foo", true},
		{"/Users/a/Foo/", "/Users/a/Foo/config/a.toml", true},
		{"/Users/a/Foo", "/Users/a/foo/config/a.toml", false},
		{"/Users/a/Foo", "/Users/a/FooBar/a.toml", false},
		{"/Users/a/Foo", "/Users/a", false},
	}
	for _, c := range cases {
		if got := InsideReal(c.root, c.real); got != c.want {
			t.Errorf("InsideReal(%q, %q) = %v, want %v", c.root, c.real, got, c.want)
		}
	}
}
