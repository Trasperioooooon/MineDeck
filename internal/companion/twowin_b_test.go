package companion_test

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"minedeck/internal/companion"
)

// 两个窗口同时开着「伙伴」页（2026-11-17）：以前加、删一个禁挖区域都要把整张表发回来，没刷新的那一边一保存，
// 就把另一边刚加的区域删掉了。现在按条删、按条加（removeZones、addZones），配置文件的读-改-写在一把锁里。

func twZone(name string, n int) companion.Zone {
	return companion.Zone{Name: name, Dim: "minecraft:overworld", Min: [3]int{n, 0, n}, Max: [3]int{n + 9, 9, n + 9}}
}

func twNames(zs []companion.Zone) []string {
	out := []string{}
	for _, z := range zs {
		out = append(out, z.Name)
	}
	return out
}

func twRead(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(companion.ConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// twZoneNames 重新读一遍文件里的区域名（按名字排好的）。
func twZoneNames(t *testing.T, dir string) []string {
	t.Helper()
	c, err := companion.ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return twNames(c.Zones)
}

const twBaseFile = "# 手写的说明\nenabled=false\nnodig.base=minecraft:overworld 0 0 0 9 9 9\n"

// 原有 [base]；A 加 farm；B（还是 A 加之前的页面）删 base、加 mine：结果是 [farm, mine]，别的行原样。
func TestZonesTwoWindowsKeepBoth(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, companion.ConfigPath(dir), twBaseFile)
	if _, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone("farm", 100)}}); err != nil { // 窗口 A
		t.Fatalf("A 加 farm：%v", err)
	}
	c, err := companion.Update(dir, companion.Patch{RemoveZones: []string{"base"}, AddZones: []companion.Zone{twZone("mine", 200)}}) // 窗口 B
	if err != nil {
		t.Fatalf("B 删 base、加 mine：%v", err)
	}
	if got := twNames(c.Zones); !slices.Equal(got, []string{"farm", "mine"}) {
		t.Fatalf("Update 返回的区域：%v", got)
	}
	if got := twZoneNames(t, dir); !slices.Equal(got, []string{"farm", "mine"}) {
		t.Fatalf("文件里的区域：%v", got)
	}
	s := twRead(t, dir)
	if !strings.HasPrefix(s, "# 手写的说明\nenabled=false\n") || strings.Contains(s, "nodig.base") ||
		!strings.Contains(s, "nodig.farm=minecraft:overworld 100 0 100 109 9 109\n") || !strings.Contains(s, "nodig.mine=minecraft:overworld 200 0 200 209 9 209\n") {
		t.Fatalf("文件：\n%s", s)
	}
}

// 加 Farm（大写）时 farm 已经在了：400，说清楚是重名，文件一字不变。
func TestAddZoneDuplicateRefused(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, companion.ConfigPath(dir), twBaseFile+"nodig.farm=minecraft:overworld 100 0 100 109 9 109\n")
	before := twRead(t, dir)
	_, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone("Farm", 300)}, MaxCompanions: ptr(3)})
	var ce *companion.Error
	if !errors.As(err, &ce) || ce.Code != 400 || err.Error() != "已经有一个叫「farm」的禁挖区域了（可能是刚在别的窗口加的），换个名字" {
		t.Fatalf("重名应报 400：%v", err)
	}
	if after := twRead(t, dir); after != before {
		t.Fatalf("重名时文件不该动（连同一次给的伙伴上限）：\n前 %s\n后 %s", before, after)
	}
	// 同一次要加的里面自己重名：Check 就拒绝
	if _, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone("x", 0), twZone("X", 1)}}); err == nil || !strings.Contains(err.Error(), "两个都叫") {
		t.Fatalf("要加的里面重名应拒绝：%v", err)
	}
	if after := twRead(t, dir); after != before {
		t.Fatal("被拒绝的改动不该写文件")
	}
}

// 编辑 = 删旧名字 + 加新的（只改了大小写也行）；删一个没有的名字不算错，也不写文件。
func TestEditZoneAndRemoveMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, companion.ConfigPath(dir), twBaseFile)
	edited := twZone(" Base ", 500) // 名字带空格：Check 去掉，而且不改调用方手里的那份
	add := []companion.Zone{edited}
	c, err := companion.Update(dir, companion.Patch{RemoveZones: []string{" base "}, AddZones: add})
	if err != nil {
		t.Fatalf("删 base、加 Base：%v", err)
	}
	if len(c.Zones) != 1 || c.Zones[0].Name != "Base" || c.Zones[0].Min != [3]int{500, 0, 500} {
		t.Fatalf("编辑以后：%+v", c.Zones)
	}
	if add[0].Name != " Base " {
		t.Fatalf("Check 改了调用方的切片：%q", add[0].Name)
	}
	if s := twRead(t, dir); strings.Contains(s, "nodig.base=") || !strings.Contains(s, "nodig.Base=minecraft:overworld 500 0 500 509 9 509\n") {
		t.Fatalf("文件：\n%s", s)
	}
	before := twRead(t, dir)
	c, err = companion.Update(dir, companion.Patch{RemoveZones: []string{"nope"}})
	if err != nil || !slices.Equal(twNames(c.Zones), []string{"Base"}) {
		t.Fatalf("删一个没有的名字：%v %v", twNames(c.Zones), err)
	}
	if after := twRead(t, dir); after != before {
		t.Fatal("删一个没有的名字不该改文件")
	}
}

// 合完超过 64 个：400，文件不动；先删一个再加一个正好 64 个就行。
func TestZonesLimitAfterMerge(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := range companion.MaxZones {
		fmt.Fprintf(&b, "nodig.z%02d=minecraft:overworld %d 0 0 %d 9 9\n", i, i*10, i*10+9)
	}
	writeFile(t, companion.ConfigPath(dir), b.String())
	before := twRead(t, dir)
	_, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone("extra", 5000)}})
	var ce *companion.Error
	if !errors.As(err, &ce) || ce.Code != 400 || !strings.Contains(err.Error(), "最多 64 个") {
		t.Fatalf("超过上限应报 400：%v", err)
	}
	if after := twRead(t, dir); after != before {
		t.Fatal("超过上限时不该写文件")
	}
	c, err := companion.Update(dir, companion.Patch{RemoveZones: []string{"Z00"}, AddZones: []companion.Zone{twZone("extra", 5000)}})
	if err != nil || len(c.Zones) != companion.MaxZones || slices.Contains(twNames(c.Zones), "z00") || !slices.Contains(twNames(c.Zones), "extra") {
		t.Fatalf("删一个加一个：%d 个 %v", len(c.Zones), err)
	}
}

// 文件里手写的、写错的区域行（模组会跳过）：只用 addZones、removeZones 时原样留着；老写法 zones 整体替换照旧全删。
func TestBadZoneLinesKeptByAddRemove(t *testing.T) {
	dir := t.TempDir()
	const broken = "nodig.broken=minecraft:overworld 1 2\n"
	const wrongDim = "nodig.nether = overworld 0 0 0 1 1 1\n"
	writeFile(t, companion.ConfigPath(dir), "# 说明\n"+broken+twBaseFile[len("# 手写的说明\n"):]+wrongDim)
	c, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone("farm", 100)}})
	if err != nil || !slices.Equal(twNames(c.Zones), []string{"base", "farm"}) || len(c.Problems) != 2 {
		t.Fatalf("加 farm：%+v %v", c, err)
	}
	if s := twRead(t, dir); !strings.Contains(s, broken) || !strings.Contains(s, wrongDim) || !strings.HasPrefix(s, "# 说明\n"+broken) {
		t.Fatalf("加区域时写错的行应原样留着：\n%s", s)
	}
	if _, err := companion.Update(dir, companion.Patch{RemoveZones: []string{"base"}}); err != nil {
		t.Fatal(err)
	}
	if s := twRead(t, dir); !strings.Contains(s, broken) || !strings.Contains(s, wrongDim) || strings.Contains(s, "nodig.base") {
		t.Fatalf("删区域时写错的行应原样留着：\n%s", s)
	}
	// 老写法（整张替换）和以前一样：nodig.* 全删了再写
	if _, err := companion.Update(dir, companion.Patch{Zones: &[]companion.Zone{twZone("only", 0)}}); err != nil {
		t.Fatal(err)
	}
	if s := twRead(t, dir); strings.Contains(s, "broken") || strings.Contains(s, "nether") || !strings.Contains(s, "nodig.only=") {
		t.Fatalf("整张替换：\n%s", s)
	}
}

// 几样一起给：先 zones 整体替换，再删，再加；结果按名字排好。纯函数，不碰文件。
func TestMergeZonesOrder(t *testing.T) {
	cur := []companion.Zone{twZone("old", 0)}
	pt, err := companion.Patch{Zones: &[]companion.Zone{twZone("b", 1), twZone("a", 2)}, RemoveZones: []string{"A"}, AddZones: []companion.Zone{twZone("c", 3)}}.Check()
	if err != nil {
		t.Fatal(err)
	}
	got, err := pt.MergeZones(cur)
	if err != nil || !slices.Equal(twNames(got), []string{"b", "c"}) {
		t.Fatalf("合并：%v %v", twNames(got), err)
	}
	if got, err := (companion.Patch{}).MergeZones(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("什么都没给：%v %v", got, err)
	}
}

// 很多个窗口同时各加一个不同名字的区域：读-改-写在锁里一个一个来，全部都在，谁的也没丢。
func TestZonesConcurrentAdds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, companion.ConfigPath(dir), twBaseFile)
	const n = 40
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := companion.Update(dir, companion.Patch{AddZones: []companion.Zone{twZone(fmt.Sprintf("w%02d", i), 100+i*20)}})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("同时加区域报错：%v", err)
		}
	}
	got := twZoneNames(t, dir)
	want := []string{"base"}
	for i := range n {
		want = append(want, fmt.Sprintf("w%02d", i))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		missing := []string{}
		for _, w := range want {
			if !slices.Contains(got, w) {
				missing = append(missing, w)
			}
		}
		t.Fatalf("同时加了 %d 个，文件里有 %d 个，丢了：%v", n, len(got)-1, missing)
	}
}
