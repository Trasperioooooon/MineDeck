package companion_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/companion"
)

// 读配置的规则和模组一样：没有文件时是默认值；attack_deny 没写用默认、写成空的表示没有额外限制；
// 禁挖区域两种写法都认（空格或逗号），写错的跳过并说明。
func TestReadConfig(t *testing.T) {
	dir := t.TempDir()
	c, err := companion.ReadConfig(dir)
	if err != nil || c.Exists || c.Enabled || c.Port != companion.DefaultPort || c.MaxCompanions != 4 || !c.DenyDefault ||
		!slices.Equal(c.AttackDeny, companion.DefaultAttackDeny) || len(c.Zones) != 0 || c.Problem() == "" {
		t.Fatalf("没有文件：%+v %v", c, err)
	}
	src := "# 模组生成的模板\r\nenabled=true\r\nport=24481\r\ntoken=" + token + "\r\nmax_companions=99\r\nattack_deny=villager, minecraft:cat,Bad Mob\r\n" +
		"nodig.base=minecraft:overworld -120 -64 -80 -40 320 10\r\nnodig.farm=minecraft:overworld 200,60,200 260,90,260\r\n" +
		"nodig.bad=minecraft:overworld 1 2 3\r\nnodig.坏名字=minecraft:overworld 0 0 0 1 1 1\r\nnodig.far=minecraft:overworld 0 9999 0 1 1 1\r\n"
	writeFile(t, companion.ConfigPath(dir), src)
	c, err = companion.ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Exists || !c.Enabled || c.Port != 24481 || c.Token != token || c.MaxCompanions != 4 || c.DenyDefault ||
		!slices.Equal(c.AttackDeny, []string{"minecraft:villager", "minecraft:cat"}) || c.Problem() != "" {
		t.Errorf("配置：%+v", c)
	}
	if len(c.Zones) != 2 || c.Zones[0] != (companion.Zone{Name: "base", Dim: "minecraft:overworld", Min: [3]int{-120, -64, -80}, Max: [3]int{-40, 320, 10}}) ||
		c.Zones[1].Name != "farm" || c.Zones[1].Max != [3]int{260, 90, 260} {
		t.Errorf("禁挖区域：%+v", c.Zones)
	}
	if p := strings.Join(c.Problems, "|"); !strings.Contains(p, "max_companions=99") || !strings.Contains(p, "Bad") || !strings.Contains(p, "bad") ||
		!strings.Contains(p, "坏名字") || !strings.Contains(p, "far") {
		t.Errorf("写错的条目：%v", c.Problems)
	}
	writeFile(t, companion.ConfigPath(dir), "enabled=true\nport=abc\ntoken=x\nattack_deny=\n")
	c, _ = companion.ReadConfig(dir)
	if c.Port != -1 || len(c.AttackDeny) != 0 || c.DenyDefault || !strings.Contains(c.Problem(), "port") {
		t.Errorf("端口写错、attack_deny 写成空的：%+v %q", c, c.Problem())
	}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// EnsureConfig：没有文件就新建一份带说明的（Windows 换行），打开端口、选一个没被别的实例用的端口、生成令牌；
// 已有的只改这几行，注释、别的键、禁挖区域原样保留；什么都不用改时不写文件。
// Update：只改相关的行；整体替换禁挖区域；恢复 attack_deny 的默认值就是删掉这一行。写的时候先写临时文件再改名。
func TestEnsureAndUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	c, changed, err := companion.EnsureConfig(dir, companion.EnsureOptions{Avoid: func(p int) bool { return p == companion.DefaultPort }})
	if err != nil || !changed || !c.Enabled || c.Port == companion.DefaultPort || c.Port < 1024 || !companion.ValidToken(c.Token) || len(c.Token) != 32 || c.Problem() != "" {
		t.Fatalf("新建：%+v %v %v", c, changed, err)
	}
	b, _ := os.ReadFile(companion.ConfigPath(dir))
	if !strings.Contains(string(b), "\r\n") || !strings.HasPrefix(string(b), "# MineDeck AI 伙伴模组") {
		t.Errorf("新文件：%q", b)
	}
	if _, changed, _ := companion.EnsureConfig(dir, companion.EnsureOptions{}); changed {
		t.Error("没什么要改的时不该写文件")
	}
	tok := c.Token

	// 模组生成的模板（端口关着、令牌太短），带用户写的禁挖区域
	writeFile(t, companion.ConfigPath(dir), "# 模板\nenabled=false\nport=24490\ntoken=abc\nnodig.base=minecraft:overworld 0 0 0 9 9 9\n")
	c, changed, err = companion.EnsureConfig(dir, companion.EnsureOptions{})
	b, _ = os.ReadFile(companion.ConfigPath(dir))
	if err != nil || !changed || !c.Enabled || c.Port != 24490 || c.Token == tok || !companion.ValidToken(c.Token) || len(c.Zones) != 1 ||
		!strings.HasPrefix(string(b), "# 模板\nenabled=true\nport=24490\ntoken=") || !strings.Contains(string(b), "nodig.base=minecraft:overworld 0 0 0 9 9 9\n") {
		t.Errorf("改模板：%+v\n%s", c, b)
	}

	max := 2
	zones := []companion.Zone{{Name: "farm", Dim: "minecraft:overworld", Min: [3]int{200, 60, 200}, Max: [3]int{260, 90, 260}},
		{Name: "nether-base", Dim: "minecraft:the_nether", Min: [3]int{10, 30, 10}, Max: [3]int{-10, 80, -10}}}
	deny := []string{"villager", "minecraft:cat", "villager", " "}
	c, err = companion.Update(dir, companion.Patch{MaxCompanions: &max, AttackDeny: &deny, Zones: &zones})
	b, _ = os.ReadFile(companion.ConfigPath(dir))
	s := string(b)
	if err != nil || c.MaxCompanions != 2 || !slices.Equal(c.AttackDeny, []string{"minecraft:villager", "minecraft:cat"}) || len(c.Zones) != 2 ||
		strings.Contains(s, "nodig.base") || !strings.Contains(s, "nodig.farm=minecraft:overworld 200 60 200 260 90 260\n") ||
		!strings.Contains(s, "nodig.nether-base=minecraft:the_nether 10 30 10 -10 80 -10\n") || !strings.Contains(s, "attack_deny=minecraft:villager,minecraft:cat\n") ||
		!strings.HasPrefix(s, "# 模板\nenabled=true\n") {
		t.Errorf("改设置：%+v %v\n%s", c, err, s)
	}
	// 模组读回来一样（MineDeck 写的就是模组认的格式）
	again, _ := companion.ReadConfig(dir)
	if len(again.Problems) != 0 || again.Zones[1] != zones[1] {
		t.Errorf("读回来：%+v", again)
	}
	none := []string{}
	if c, _ := companion.Update(dir, companion.Patch{AttackDeny: &none}); len(c.AttackDeny) != 0 || c.DenyDefault {
		t.Errorf("没有额外限制：%+v", c)
	}
	if b, _ := os.ReadFile(companion.ConfigPath(dir)); !strings.Contains(string(b), "attack_deny=\n") {
		t.Errorf("应写成 attack_deny=：%s", b)
	}
	if c, _ := companion.Update(dir, companion.Patch{DenyDefault: true}); !c.DenyDefault || !slices.Equal(c.AttackDeny, companion.DefaultAttackDeny) {
		t.Errorf("恢复默认：%+v", c)
	}
	if b, _ := os.ReadFile(companion.ConfigPath(dir)); strings.Contains(string(b), "attack_deny") {
		t.Errorf("恢复默认应删掉这一行：%s", b)
	}
	if ents, _ := os.ReadDir(filepath.Dir(companion.ConfigPath(dir))); len(ents) != 1 {
		t.Errorf("不该留下临时文件：%v", ents)
	}

	// 不合规的改动整次不改
	bad := []struct {
		name string
		p    companion.Patch
	}{
		{"上限 0", companion.Patch{MaxCompanions: ptr(0)}},
		{"上限 17", companion.Patch{MaxCompanions: ptr(17)}},
		{"生物 ID", companion.Patch{AttackDeny: &[]string{"Villager!"}}},
		{"区域名", companion.Patch{Zones: &[]companion.Zone{{Name: "a b", Dim: "minecraft:overworld"}}}},
		{"维度", companion.Patch{Zones: &[]companion.Zone{{Name: "a", Dim: "overworld"}}}},
		{"坐标", companion.Patch{Zones: &[]companion.Zone{{Name: "a", Dim: "minecraft:overworld", Max: [3]int{0, 5000, 0}}}}},
		{"重名", companion.Patch{Zones: &[]companion.Zone{{Name: "a", Dim: "minecraft:overworld"}, {Name: "A", Dim: "minecraft:overworld"}}}},
	}
	before, _ := os.ReadFile(companion.ConfigPath(dir))
	for _, c := range bad {
		if _, err := companion.Update(dir, c.p); err == nil {
			t.Errorf("%s：应报错", c.name)
		}
	}
	if after, _ := os.ReadFile(companion.ConfigPath(dir)); string(after) != string(before) {
		t.Error("不合规的改动不该写文件")
	}

	if changed, err := companion.Disable(dir); err != nil || !changed {
		t.Errorf("关掉：%v %v", changed, err)
	}
	if c, _ := companion.ReadConfig(dir); c.Enabled || c.Token == "" {
		t.Errorf("关掉以后：%+v", c)
	}
	if changed, err := companion.Disable(t.TempDir()); err != nil || changed {
		t.Error("没有文件时关掉什么都不做")
	}
}

func ptr[T any](v T) *T { return &v }

// 伙伴的名字：要能在聊天里用「@名字」叫它。
func TestCheckName(t *testing.T) {
	for _, n := range []string{"小方", "Mine_Bot", "伙伴1", "Ａ"} {
		if err := companion.CheckName(n); err != nil {
			t.Errorf("%s 应该能用：%v", n, err)
		}
	}
	for _, n := range []string{"", "小 方", "@小方", "小/方", "a:b", "a，b", "a\"b", "§c红", strings.Repeat("长", 17)} {
		if err := companion.CheckName(n); err == nil {
			t.Errorf("%q 不该能用", n)
		}
	}
	if !companion.ValidPlayer("Steve.B_1") || companion.ValidPlayer("小明") || companion.ValidPlayer("a b") || companion.ValidPlayer(strings.Repeat("a", 21)) {
		t.Error("玩家名的规则")
	}
}

func TestInRange(t *testing.T) {
	for _, c := range []struct {
		v, r string
		want bool
	}{
		{"1.21.1-1.4.0", "[1.21.1-1.4.0,1.21.1-1.5)", true},
		{"1.21.1-1.4.9", "[1.21.1-1.4.0,1.21.1-1.5)", true},
		{"1.21.1-1.5.0", "[1.21.1-1.4.0,1.21.1-1.5)", false},
		{"1.21.1-1.3.9", "[1.21.1-1.4.0,1.21.1-1.5)", false},
		{"13.0.11", "[13.0.8,)", true},
		{"13.0.6", "[13.0.8,)", false},
		{"21.1.265", "[21.1,)", true},
		{"21.0.110-beta", "[21.1,)", false},
		{"47.4.28", "[47,)", true},
		{"2.0", "(1.0,2.0]", true},
		{"1.0", "(1.0,2.0]", false},
		{"3.5", "[1,2),[3,4)", true},
		{"2.5", "[1,2),[3,4)", false},
		{"1.5", "[1.5]", true},
		{"1.6", "1.5", true},
		{"1.4", "1.5", false},
		{"9", "", true},
	} {
		if got := companion.InRange(c.v, c.r); got != c.want {
			t.Errorf("InRange(%s, %s) = %v", c.v, c.r, got)
		}
	}
}
