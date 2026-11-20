package modcart

import "testing"

func TestProjectPageURL(t *testing.T) {
	if got := ProjectPageURL("mod", "sodium"); got != "https://modrinth.com/mod/sodium" {
		t.Fatalf("模组：%s", got)
	}
	if got := ProjectPageURL("plugin", "essentialsx"); got != "https://modrinth.com/plugin/essentialsx" {
		t.Fatalf("插件：%s", got)
	}
	for _, pair := range [][2]string{
		{"", "sodium"},
		{"mod", ""},
		{"modpack", "my pack"},
		{"website", "sodium"},
		{"mod", ".."},
		{"mod", "javascript:alert(1)"},
		{"mod", "a/b"},
	} {
		if got := ProjectPageURL(pair[0], pair[1]); got != "" {
			t.Fatalf("%q %q 不该有网址：%s", pair[0], pair[1], got)
		}
	}
}
