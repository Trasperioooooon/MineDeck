package modcart

import (
	"strings"
	"testing"
)

func TestInRange(t *testing.T) {
	for _, c := range []struct {
		v, r string
		want bool
	}{
		// Maven（mods.toml）
		{"47.2.0", "[47,)", true},
		{"46.9", "[47,)", false},
		{"1.21.1", "[1.21.1,1.21.2)", true},
		{"1.21.2", "[1.21.1,1.21.2)", false},
		{"1.0", "(1.0,2.0]", false},
		{"2.0", "(1.0,2.0]", true},
		{"1.5", "[1.0]", false},
		{"1.0", "[1.0]", true},
		{"3.5", "[1,2),[3,4)", true},
		{"2.5", "[1,2),[3,4)", false},
		// Fabric 谓词
		{"0.92.2+1.20.1", ">=0.92.0", true},
		{"0.91.0+1.20.1", ">=0.92.0", false},
		{"1.20.1", "~1.20", true},
		{"1.21", "~1.20", false},
		{"1.20.4", "1.20.x", true},
		{"1.21.0", "1.20.x", false},
		{"2.3.0", "^2.1.0", true},
		{"3.0.0", "^2.1.0", false},
		{"0.5.2", "^0.5.0", true},
		{"0.6.0", "^0.5.0", false},
		{"1.5.0", ">=1.2.0 <2.0.0", true},
		{"2.0.0", ">=1.2.0 <2.0.0", false},
		{"1.0.0", ">=2 || <1.1", true},
		{"1.5.0", ">=2 || <1.1", false},
		{"1.0.0-beta.2", ">=1.0.0", false},
		{"1.0.0", ">1.0.0-beta.2", true},
		{"9.9", "*", true},
		{"9.9", "", true},
		// 不带运算符：按「不低于」
		{"2.0", "1.5", true},
		{"1.0", "1.5", false},
		// 认不出：不报版本不对
		{"1.0", "[1.0", true},
		{"1.0", ">=", true},
		{"1.0", "@@weird", true},
	} {
		if got := InRange(c.v, c.r); got != c.want {
			t.Errorf("InRange(%q, %q) = %v，应为 %v", c.v, c.r, got, c.want)
		}
	}
}

// 本地核对：缺的、版本不对的；服务器本身提供的、只在客户端用的、停用的模组都不算；provides 算已装。
func TestCheckLocal(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, "a.jar", map[string]string{"fabric.mod.json": `{"id":"a","name":"Mod A","version":"1.0",
		"depends":{"minecraft":"1.20.1","fabricloader":">=0.14","b":">=1.2","clib":"*","gone":"*","java":">=17"}}`})
	writeJar(t, dir, "b.jar", map[string]string{"fabric.mod.json": `{"id":"b","name":"Mod B","version":"1.1.0+1.20.1"}`})
	writeJar(t, dir, "c.jar", map[string]string{"fabric.mod.json": `{"id":"c","version":"2.0","provides":["clib"]}`})
	writeJar(t, dir, "n.jar", map[string]string{"META-INF/neoforge.mods.toml": `modLoader = "javafml"
[[mods]]
modId = "n"
displayName = "Neo Mod"
version = "3.0"
[[dependencies.n]]
modId = "clientonly"
type = "required"
side = "CLIENT"
[[dependencies.n]]
modId = "serverlib"
type = "required"
versionRange = "[2,)"
side = "BOTH"
[[dependencies.n]]
modId = "rival"
type = "incompatible"
`})
	writeJar(t, dir, "d.jar.disabled", map[string]string{"fabric.mod.json": `{"id":"gone","version":"1.0","depends":{"zzz":"*"}}`})
	missing, stale, checked, err := CheckLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if checked != 4 {
		t.Errorf("读了 %d 个模组，应为 4（停用的不算）", checked)
	}
	var ms []string
	for _, m := range missing {
		ms = append(ms, m.ID+"←"+m.RequiredBy+"@"+m.File)
	}
	if got := strings.Join(ms, ","); got != "gone←Mod A@a.jar,serverlib←Neo Mod@n.jar" {
		t.Errorf("缺的：%s", got)
	}
	if len(stale) != 1 || stale[0].ID != "b" || stale[0].Have != "1.1.0+1.20.1" || stale[0].Range != ">=1.2" {
		t.Errorf("版本不对的：%+v", stale)
	}
	if m, s, n, err := CheckLocal(dir + "/没有这个"); err != nil || len(m) != 0 || len(s) != 0 || n != 0 || m == nil {
		t.Errorf("目录不存在：%v %v %d %v", m, s, n, err)
	}
}
