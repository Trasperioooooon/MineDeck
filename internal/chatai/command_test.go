package chatai

import (
	"strings"
	"testing"
)

func allOn() map[string]bool {
	m := map[string]bool{}
	for _, t := range Templates {
		m[t.ID] = true
	}
	return m
}

func defaultsOn() map[string]bool { return Defaults().enabledTemplates() }

// 模板里允许的写法：「自己」可以省略（程序补上），规范化成去掉 /、命名空间、多余空格的样子。
func TestCommandTemplates(t *testing.T) {
	online := []string{"Steve", "Alex"}
	cases := map[string]string{
		"weather clear":                             "weather clear",
		"/weather clear":                            "weather clear",
		"  WEATHER   Thunder  600 ":                 "weather thunder 600",
		"minecraft:weather rain 6000":               "weather rain 6000",
		"time set day":                              "time set day",
		"time set midnight":                         "time set midnight",
		"time query daytime":                        "time query daytime",
		"tp Alex":                                   "tp Steve Alex",
		"tp steve alex":                             "tp Steve Alex",
		"tp Steve 100 64 -200":                      "tp Steve 100 64 -200",
		"tp ~ ~10 ~":                                "tp Steve ~ ~10 ~",
		"teleport Steve ^ ^ ^5":                     "teleport Steve ^ ^ ^5",
		"effect give Steve minecraft:speed 60 1":    "effect give Steve minecraft:speed 60 1",
		"effect give speed 30":                      "effect give Steve speed 30",
		"effect give Steve night_vision 600 4 true": "effect give Steve night_vision 600 4 true",
		"effect clear Steve":                        "effect clear Steve",
		"effect clear":                              "effect clear Steve",
		"effect clear Steve minecraft:poison":       "effect clear Steve minecraft:poison",
		"effect Steve speed 30 1":                   "effect Steve speed 30 1", // 1.12 的旧写法
		"effect Steve clear":                        "effect Steve clear",
		"spawnpoint":                                "spawnpoint Steve",
		"spawnpoint Steve":                          "spawnpoint Steve",
		"locate structure minecraft:village_plains": "locate structure minecraft:village_plains",
		"locate biome #minecraft:is_ocean":          "locate biome #minecraft:is_ocean",
		"locate Village":                            "locate Village", // 1.18 以前
		"locatebiome minecraft:desert":              "locatebiome minecraft:desert",
		"list":                                      "list",
		"list uuids":                                "list uuids",
		"gamemode creative":                         "gamemode creative Steve",
		"gamemode survival Steve":                   "gamemode survival Steve",
		"give Steve minecraft:diamond 64":           "give Steve minecraft:diamond 64",
		"give create:wrench":                        "give Steve create:wrench", // 模组物品交给服务器校验
		"difficulty hard":                           "difficulty hard",
		"difficulty":                                "difficulty",
		"seed":                                      "seed",
	}
	for in, want := range cases {
		got, err := CheckCommand(in, "Steve", online, allOn())
		if err != nil || got != want {
			t.Errorf("CheckCommand(%q) = %q, %v；应为 %q", in, got, err, want)
		}
	}
}

// 绕过白名单的各种写法一律不执行（§11：execute、命名空间、选择器、大小写、全角字符、超长、换行），带原因。
func TestCommandRejects(t *testing.T) {
	online := []string{"Steve", "Alex"}
	cases := map[string]string{
		"":                                    "空的",
		"op Steve":                            "永远不允许",
		"OP Steve":                            "永远不允许",
		"/op Steve":                           "永远不允许",
		"minecraft:op Steve":                  "永远不允许",
		"deop Alex":                           "永远不允许",
		"execute as Alex run op Steve":        "永远不允许",
		"Execute run weather clear":           "永远不允许",
		"minecraft:execute run time set day":  "永远不允许",
		"ban Alex":                            "永远不允许",
		"stop":                                "永远不允许",
		"kill Alex":                           "永远不允许",
		"whitelist off":                       "永远不允许",
		"gamerule keepInventory true":         "永远不允许",
		"function minecraft:foo":              "永远不允许",
		"forge tps":                           "永远不允许",
		"neoforge tps":                        "永远不允许",
		"say 大家好":                             "中文",
		"say hello":                           "永远不允许",
		"tellraw @a \"hi\"":                   "永远不允许",
		"fill 0 0 0 10 10 10 tnt":             "永远不允许",
		"summon minecraft:wither":             "永远不允许",
		"weather clear\nop Steve":             "换行",
		"weather clear\rop Steve":             "换行",
		"weather §aclear":                     "§",
		"weather\x00clear":                    "控制字符",
		"ｏｐ Steve":                            "全角",
		"weather\U00003000clear":              "全角",
		"weather ｃlear":                       "全角",
		strings.Repeat("a", 201):              "太长",
		"tp @a Steve":                         "选择器",
		"tp Steve @p":                         "选择器",
		"effect give @s speed":                "选择器",
		"tp @e[type=player] Steve":            "选择器",
		"give @r diamond":                     "选择器",
		"tp Alex Steve":                       "只能传送提问的人自己",
		"tp Alex 0 0 0":                       "只能传送提问的人自己",
		"tp Notch":                            "不在线",
		"tp Steve":                            "不能把 Steve 传送到自己身边",
		"tp Steve 1 2":                        "tp 只能",
		"tp Steve ^ 2 ^":                      "tp 只能",
		"tp Steve 1 2 3 90 0":                 "tp 只能",
		"time set 1000":                       "time 只能",
		"time add 100":                        "time 只能",
		"weather clear 6001":                  "1 到 6000",
		"weather clear -5":                    "整数",
		"weather sunny":                       "weather 只能",
		"effect give Alex speed":              "不能对 Alex 用",
		"effect give Steve speed 601":         "1 到 600",
		"effect give Steve speed 60 5":        "等级最多 4",
		"effect give Steve speed 60 1 maybe":  "true 或 false",
		"effect give Steve minecraft:speed{}": "ID 写得不对",
		"spawnpoint Alex":                     "不能对 Alex 用",
		"spawnpoint Steve 0 64 0":             "不能带坐标",
		"locate structure":                    "locate 只能",
		"locate structure ../../x\"":          "ID 写得不对",
		"list all":                            "list 只能",
		"gamemode creative Alex":              "不能改 Alex 的",
		"gamemode god":                        "gamemode 只能",
		"give Alex diamond":                   "不能对 Alex 用",
		"give Steve diamond 65":               "1 到 64",
		"give Steve diamond_sword{Enchantments:[{id:sharpness,lvl:255}]}": "不能带 NBT",
		"give Steve diamond_sword[enchantments={sharpness:255}]":          "不能带 NBT",
		"difficulty insane":       "difficulty 只能",
		"seed 123":                "不带参数",
		"teammsg hi":              "永远不允许",
		"plugins":                 "永远不允许",
		"xp add Steve 100 levels": "不在允许执行的命令里",
		"enchant Steve sharpness": "不在允许执行的命令里",
		"somemod:dosomething":     "不在允许执行的命令里",
	}
	for in, want := range cases {
		got, err := CheckCommand(in, "Steve", online, allOn())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckCommand(%q) = %q, %v；应被拒绝，原因里有「%s」", in, got, err, want)
		}
	}
}

// 默认开关照 §6.6：gamemode（含 creative）、give、difficulty、seed 默认关；关着的模板不执行。
func TestCommandDefaults(t *testing.T) {
	on := defaultsOn()
	for _, id := range []string{"time", "weather", "tp", "effect", "spawnpoint", "locate"} {
		if !on[id] {
			t.Errorf("%s 应默认开", id)
		}
	}
	for _, c := range []string{"gamemode creative", "give Steve diamond", "difficulty hard", "seed"} {
		if _, err := CheckCommand(c, "Steve", []string{"Steve"}, on); err == nil || !strings.Contains(err.Error(), "没有打开") {
			t.Errorf("%q 默认应不执行：%v", c, err)
		}
	}
	if got, err := CheckCommand("weather clear", "Steve", []string{"Steve"}, on); err != nil || got != "weather clear" {
		t.Errorf("weather 默认能执行：%q %v", got, err)
	}
	// 永远禁止的，在任何开关下都不执行
	for _, f := range Forbidden {
		if TemplateByID(f) != nil {
			t.Errorf("禁止清单里的 %s 不能是模板", f)
		}
		for _, tpl := range Templates {
			for _, h := range tpl.heads {
				if h == f {
					t.Errorf("模板 %s 认禁止清单里的命令 %s", tpl.ID, f)
				}
			}
		}
	}
}

// 放宽名字规则以后（§6.1）：以 @ 开头、含 " \ 的名字，以及非 ASCII 的名字，都当不了提问的人。
func TestLooseNames(t *testing.T) {
	for _, n := range []string{"Mr-X", ".Steve", "a.b", "x!y", "Steve"} {
		if why := validSender(n, true); why != "" {
			t.Errorf("放宽后 %q 应能用：%s", n, why)
		}
	}
	for _, n := range []string{"@a", "@p", "@e[type=x]", `St"eve`, `St\eve`, "史蒂夫", "Ste ve", "", "abcdefghijklmnopq", "a<b", "a>b"} {
		if validSender(n, true) == "" {
			t.Errorf("放宽后 %q 也不该能用", n)
		}
	}
	for _, n := range []string{"Mr-X", ".Steve", "@a"} {
		if validSender(n, false) == "" {
			t.Errorf("默认规则下 %q 不该能用", n)
		}
	}
}
