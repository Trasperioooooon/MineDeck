package modinfo

import (
	"strings"
	"testing"
	"time"
)

// TestJavaMajorOf 覆盖 fabric.mod.json 的 depends.java 和 mods.toml 里 modId=java 的 versionRange
// 两种来源、Fabric semver 与 Maven 区间两种写法（导入结果和实例详情的「建议换 Java」提示用）。
func TestJavaMajorOf(t *testing.T) {
	cases := []struct {
		name string
		mod  Mod
		want int
	}{
		{"Fabric 大于等于写法", Mod{Deps: []Dep{{ID: "java", Range: ">=25", Required: true}}}, 25},
		{"Fabric 大于等于带空格", Mod{Deps: []Dep{{ID: "java", Range: ">= 17"}}}, 17},
		{"Fabric 合取（下界操作符算要求）", Mod{Deps: []Dep{{ID: "java", Range: ">=17 <21"}}}, 17},
		{"Fabric 合取只给上界不算", Mod{Deps: []Dep{{ID: "java", Range: "<21"}}}, 0},
		{"Fabric 数组候选满足其一即可，取最低", Mod{Deps: []Dep{{ID: "java", Range: ">=17 || >=25"}}}, 17},
		{"候选里有一个没有下界就等于没要求", Mod{Deps: []Dep{{ID: "java", Range: "<21 || >=25"}}}, 0},
		{"Fabric 的 breaks 不算要求", Mod{Deps: []Dep{{ID: "java", Range: ">=25", Kind: "incompatible"}}}, 0},
		{"NeoForge 的 discouraged 不算要求", Mod{Deps: []Dep{{ID: "java", Range: "[25,)", Kind: "discouraged"}}}, 0},
		{"breaks 和 depends 并存只看 depends", Mod{Deps: []Dep{{ID: "java", Range: ">=17", Required: true}, {ID: "java", Range: ">=25", Kind: "incompatible"}}}, 17},
		{"Maven 区间下界", Mod{Deps: []Dep{{ID: "java", Range: "[25,)"}}}, 25},
		{"Maven 闭区间下界", Mod{Deps: []Dep{{ID: "java", Range: "[17,25)"}}}, 17},
		{"Maven 无逗号区间", Mod{Deps: []Dep{{ID: "java", Range: "[26)"}}}, 26},
		{"裸版本号", Mod{Deps: []Dep{{ID: "java", Range: "21"}}}, 21},
		{"带小版本", Mod{Deps: []Dep{{ID: "java", Range: ">=25.0.1"}}}, 25},
		{"大手笔波浪号", Mod{Deps: []Dep{{ID: "java", Range: "~21"}}}, 21},
		{"deps 里没有 java", Mod{Deps: []Dep{{ID: "fabricloader", Range: ">=0.16"}}}, 0},
		{"ID 大小写不敏感", Mod{Deps: []Dep{{ID: "Java", Range: ">=17"}}}, 17},
		{"MC 版本号不当 Java", Mod{Deps: []Dep{{ID: "java", Range: "1.20.1"}}}, 0},
		{"离谱的版本不当 Java", Mod{Deps: []Dep{{ID: "java", Range: ">=500"}}}, 0},
		{"多个模组声明取最高", Mod{Deps: []Dep{{ID: "minecraft", Range: "1.20.1"}, {ID: "java", Range: ">=17"}, {ID: "java", Range: ">=25"}}}, 25},
	}
	for _, c := range cases {
		if got := JavaMajorOf(c.mod); got != c.want {
			t.Errorf("%s：JavaMajorOf 应为 %d，实际 %d", c.name, c.want, got)
		}
	}
}

// TestJavaMajorOfHugeInput 版本号那一段来自别人打的 jar，可以有几 MB 长：解析要立刻返回。
// 原来逐字节拼字符串，一百万位数字要一分多钟，整合包里放一个这样的 jar 就能把导入卡在收尾那一步。
func TestJavaMajorOfHugeInput(t *testing.T) {
	huge := strings.Repeat("1", 4<<20)
	start := time.Now()
	for _, r := range []string{huge, ">=" + huge, "[" + huge + ",)", huge + " || " + huge} {
		if got := JavaMajorOf(Mod{Deps: []Dep{{ID: "java", Range: r}}}); got != 0 {
			t.Errorf("超长的数字不该当成 Java 版本，实际 %d", got)
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("解析 4 MB 的版本号用了 %v，应该立刻返回", d)
	}
}
