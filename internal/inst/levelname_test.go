package inst

import (
	"runtime"
	"testing"
)

// 2026-11-13 地图导入的跨家族复审（Gemini 4 Argon）查出来的两处：
// level-name 指进 MineDeck 自己的控制目录 .minedeck/ 时也被当成了能用的世界名；
// LevelName 原样返回 ./world、worlds\main 这类写法，换地图的记录按规整的路径核对时就对不上了。

func TestSafeLevelNameControlDir(t *testing.T) {
	if runtime.GOOS == "windows" && SafeLevelName(`.minedeck\world-import\w1\old\world`) {
		t.Errorf("控制目录反斜杠子路径在 Windows 上应被拦截")
	}
	for _, bad := range []string{".minedeck", ".MineDeck", ".minedeck.", ".minedeck ", ".minedeck/world-import", "./.minedeck"} {
		if SafeLevelName(bad) {
			t.Errorf("%q 指进了控制目录，不该被当成世界目录的名字", bad)
		}
	}
	for _, ok := range []string{"worlds/.minedeck", ".minedeck2", "minedeck"} {
		if !SafeLevelName(ok) {
			t.Errorf("%q 不是控制目录，应该认", ok)
		}
	}
}

func TestLevelNameNormalized(t *testing.T) {
	_, dir := tempInstance(t)
	cases := map[string]string{
		"./world":          "world",
		" world ":          "world",
		"worlds//main":     "worlds/main",
		"worlds/./main":    "worlds/main",
		".minedeck/w":      "world",
		"我的世界":             "我的世界",
		"worlds/main/../x": "worlds/x",
	}
	if runtime.GOOS == "windows" {
		cases[`worlds\\main`] = "worlds/main" // 文件里写两个反斜杠，Java 读出来是一个，Windows 上是分隔符
	}
	for raw, want := range cases {
		writeTree(t, dir, map[string]string{"server.properties": "level-name=" + raw + "\n"})
		if got := LevelName(dir); got != want {
			t.Errorf("level-name=%q：LevelName = %q, want %q", raw, got, want)
		}
	}
}
