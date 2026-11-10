package modinfo

import "testing"

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
		{"Fabric 数组候选取最高", Mod{Deps: []Dep{{ID: "java", Range: ">=17 || >=25"}}}, 25},
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
