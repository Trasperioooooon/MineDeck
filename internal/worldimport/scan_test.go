package worldimport

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	wt "minedeck/internal/worldimport/worldtest"
)

// fakeOrder 测试用的版本先后（旧 → 新），不联网。
var fakeVersions = []string{"1.8.9", "1.9", "1.12.2", "1.16.5", "1.20.1", "1.20.4", "1.21.1", "1.21.4", "26.1-snapshot-6", "26.1", "26.2", "26.3"}

func fakeOrder(_ context.Context, a, b string) (int, bool) {
	ia, ib := slices.Index(fakeVersions, a), slices.Index(fakeVersions, b)
	if ia < 0 || ib < 0 {
		return 0, false
	}
	switch {
	case ia > ib:
		return 1, true
	case ia < ib:
		return -1, true
	}
	return 0, true
}

func lv(name string, dv int32) wt.LevelOpts {
	return wt.LevelOpts{VersionName: name, DataVersion: dv, LevelName: "测试 地图", ServerBrands: []string{"vanilla"}, WorldGen: true}
}

func mustScan(t *testing.T, p, display string) *scanResult {
	t.Helper()
	r, err := scanPath(p, display, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.src != nil {
		t.Cleanup(func() { r.src.Close() })
	}
	return r
}

func codes(is []Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.Code)
	}
	return out
}

func dimIDs(c Candidate) []string {
	var out []string
	for _, d := range c.Dimensions {
		out = append(out, d.ID)
	}
	slices.Sort(out)
	return out
}

func TestScanLegacyZipShapes(t *testing.T) {
	tmp := t.TempDir()
	world := filepath.Join(tmp, "src", "空岛 Skyblock")
	if err := wt.Legacy(world, wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true, Nether: true, End: true,
		Players: []string{"069a79f4-44e9-4726-a1a5-95721e2c6d2a"}, Datapacks: []string{"game"}, ResourcesZip: true, Icon: true, SessionLock: true}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, prefix string }{{"root", ""}, {"wrapped", "Download/Map v2/空岛 Skyblock/"}} {
		t.Run(c.name, func(t *testing.T) {
			z := filepath.Join(tmp, c.name+".zip")
			if err := wt.ZipTree(z, world, c.prefix, wt.ZipOpts{}); err != nil {
				t.Fatal(err)
			}
			r := mustScan(t, z, c.name+".zip")
			if r.Kind != KindJavaWorld || len(r.Candidates) != 1 {
				t.Fatalf("kind=%s cands=%d issues=%v", r.Kind, len(r.Candidates), codes(r.Issues))
			}
			cd := r.Candidates[0]
			if cd.Root != strings.TrimSuffix(c.prefix, "/") || cd.Layout != LayoutLegacy {
				t.Fatalf("root=%q layout=%s", cd.Root, cd.Layout)
			}
			if got := strings.Join(dimIDs(cd), ","); got != "minecraft:overworld,minecraft:the_end,minecraft:the_nether" {
				t.Fatalf("dims = %s", got)
			}
			if cd.Meta.VersionName != "1.20.4" || cd.Meta.DataVersion != 3700 || cd.Meta.LevelName != "测试 地图" {
				t.Fatalf("meta = %+v", cd.Meta)
			}
			if cd.PlayerFiles != 1 || !cd.ResourcePak || !cd.HasIcon || len(cd.Datapacks) != 1 {
				t.Fatalf("players=%d res=%v icon=%v packs=%v", cd.PlayerFiles, cd.ResourcePak, cd.HasIcon, cd.Datapacks)
			}
		})
	}
	// 世界旁边放着说明书和资源包、外面还有 mods：说明书认出来，mods 只作提示
	pkg := filepath.Join(tmp, "pkg")
	if err := wt.Legacy(filepath.Join(pkg, "MyMap", "world"), wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	write(t, pkg, map[string]string{"MyMap/README.txt": "Play on 1.20.4\x07 with §cfriends", "MyMap/mods/a.jar": "x", "MyMap/Resource Pack.zip": "PK"})
	z := filepath.Join(tmp, "pkg.zip")
	if err := wt.ZipTree(z, pkg, "", wt.ZipOpts{}); err != nil {
		t.Fatal(err)
	}
	r := mustScan(t, z, "pkg.zip")
	if len(r.Candidates) != 1 || len(r.Candidates[0].Readme) != 1 || len(r.Candidates[0].SourceHints) == 0 {
		t.Fatalf("cands=%d readme=%v hints=%v", len(r.Candidates), r.Candidates[0].Readme, r.Candidates[0].SourceHints)
	}
}

func TestScanMultipleAndBukkit(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "two")
	if err := wt.Legacy(filepath.Join(src, "A"), wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	if err := wt.LegacyBukkit(src, "survival", wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true, Nether: true, End: true, UIDDat: true}); err != nil {
		t.Fatal(err)
	}
	r := mustScan(t, src, "two")
	if len(r.Candidates) != 2 || !slices.Contains(codes(r.Issues), "multi") {
		t.Fatalf("cands=%d issues=%v", len(r.Candidates), codes(r.Issues))
	}
	var bk *Candidate
	for k := range r.Candidates {
		if r.Candidates[k].FolderName == "survival" {
			bk = &r.Candidates[k]
		}
	}
	if bk == nil || bk.Layout != LayoutLegacyBukkit || len(bk.Group) != 2 {
		t.Fatalf("bukkit candidate = %+v", bk)
	}
	if got := strings.Join(dimIDs(*bk), ","); got != "minecraft:overworld,minecraft:the_end,minecraft:the_nether" {
		t.Fatalf("dims = %s", got)
	}
}

func TestScanModernAndBlank(t *testing.T) {
	tmp := t.TempDir()
	m := filepath.Join(tmp, "m")
	if err := wt.Modern(m, wt.WorldOpts{Level: lv("26.1", 4800), Overworld: true, Nether: true, Players: []string{"069a79f4-44e9-4726-a1a5-95721e2c6d2a"},
		CustomDims: []string{"mypack:sky"}}); err != nil {
		t.Fatal(err)
	}
	r := mustScan(t, m, "m")
	c := r.Candidates[0]
	if c.Layout != LayoutModern || c.PlayerFiles != 1 || !slices.Contains(dimIDs(c), "mypack:sky") || slices.Contains(dimIDs(c), "minecraft:the_end") {
		t.Fatalf("layout=%s players=%d dims=%v", c.Layout, c.PlayerFiles, dimIDs(c))
	}
	p := filepath.Join(tmp, "p")
	if err := wt.ModernPaper(p, wt.WorldOpts{Level: lv("26.1", 4800), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	if r := mustScan(t, p, "p"); r.Candidates[0].Layout != LayoutModernPaper {
		t.Fatalf("paper layout = %s", r.Candidates[0].Layout)
	}
	// 没生成过区块的空白世界：只有 level.dat，不能因为没有 region 就拒绝
	b := filepath.Join(tmp, "blank")
	if err := wt.Legacy(b, wt.WorldOpts{Level: lv("1.20.4", 3700)}); err != nil {
		t.Fatal(err)
	}
	r = mustScan(t, b, "blank")
	if r.Kind != KindJavaWorld || hasBlock(r.Candidates[0].Issues) {
		t.Fatalf("blank world: kind=%s issues=%v", r.Kind, codes(r.Candidates[0].Issues))
	}
}

func TestScanSinglePlayerMeta(t *testing.T) {
	tmp := t.TempDir()
	old := lv("1.20.4", 3700)
	old.Player, old.PlayerUUID = true, "069a79f4-44e9-4726-a1a5-95721e2c6d2a"
	old.GameRules = map[string]string{"keepInventory": "true"}
	if err := wt.Legacy(filepath.Join(tmp, "a"), wt.WorldOpts{Level: old}); err != nil {
		t.Fatal(err)
	}
	m := mustScan(t, filepath.Join(tmp, "a"), "a").Candidates[0].Meta
	if !m.HasPlayerTag || m.PlayerUUID != old.PlayerUUID || m.GameRules["keepInventory"] != "true" {
		t.Fatalf("meta = %+v", m)
	}
	for _, asString := range []bool{false, true} {
		nw := lv("26.1", 4800)
		nw.SingleplayerUUID, nw.SingleplayerUUIDAsString = old.PlayerUUID, asString
		d := filepath.Join(tmp, "b"+map[bool]string{true: "s", false: "i"}[asString])
		if err := wt.Modern(d, wt.WorldOpts{Level: nw}); err != nil {
			t.Fatal(err)
		}
		if m := mustScan(t, d, "b").Candidates[0].Meta; m.SingleplayerUUID != old.PlayerUUID || m.HasPlayerTag {
			t.Fatalf("asString=%v meta = %+v", asString, m)
		}
	}
}

func TestScanOtherInputs(t *testing.T) {
	tmp := t.TempDir()
	type tc struct {
		name string
		make func(p string) error
		file string
		kind InputKind
	}
	cases := []tc{
		{"bedrock", wt.Bedrock, "w.mcworld", KindBedrock},
		{"bedrock-folder", wt.BedrockFolder, "", KindBedrock},
		{"resource-pack", wt.ResourcePack, "rp.zip", KindResourcePack},
		{"datapack", wt.Datapack, "dp.zip", KindDatapack},
		{"curseforge", wt.CurseForgeModpack, "cf.zip", KindModpack},
		{"modrinth", wt.ModrinthPack, "x.mrpack", KindModpack},
		{"mdpack", wt.Mdpack, "x.mdpack", KindMdpack},
		{"rar", wt.Rar, "x.rar", KindArchive},
		{"7z", wt.SevenZip, "x.7z", KindArchive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(tmp, c.name)
			if c.file != "" {
				p = filepath.Join(tmp, c.file)
			}
			if err := c.make(p); err != nil {
				t.Fatal(err)
			}
			r := mustScan(t, p, filepath.Base(p))
			if r.Kind != c.kind || len(r.Candidates) != 0 {
				t.Fatalf("kind=%s cands=%d issues=%v", r.Kind, len(r.Candidates), codes(r.Issues))
			}
		})
	}
	// 建筑蓝图：单个文件
	st, err := wt.Structures(filepath.Join(tmp, "st"))
	if err != nil {
		t.Fatal(err)
	}
	for k, p := range st {
		if r := mustScan(t, p, filepath.Base(p)); r.Kind != KindStructure {
			t.Errorf("%s: kind=%s", k, r.Kind)
		}
	}
	// 分卷
	first, err := wt.SplitZip(filepath.Join(tmp, "split"))
	if err != nil {
		t.Fatal(err)
	}
	if r := mustScan(t, first, filepath.Base(first)); r.Kind != KindArchive {
		t.Errorf("split: kind=%s", r.Kind)
	}
	// 整台服务端：世界照样列出来（只导入用户选的那张），并提示可以接管
	sv := filepath.Join(tmp, "server")
	if err := wt.ServerFolder(sv); err != nil {
		t.Fatal(err)
	}
	if r := mustScan(t, sv, "server"); r.Kind != KindJavaWorld || !slices.Contains(codes(r.Issues), "server-folder") {
		t.Errorf("server folder: kind=%s issues=%v", r.Kind, codes(r.Issues))
	}
	// MineDeck 服务器文件夹里的恢复点不算地图
	if err := wt.Legacy(filepath.Join(sv, ".minedeck", "world-import", "w1", "old", "world"), wt.WorldOpts{Level: lv("1.20.4", 3700)}); err != nil {
		t.Fatal(err)
	}
	if r := mustScan(t, sv, "server"); len(r.Candidates) != 1 {
		t.Errorf("恢复点被当成了地图：%d 个候选", len(r.Candidates))
	}
}

func TestScanDamaged(t *testing.T) {
	tmp := t.TempDir()
	dirs, err := wt.Damaged(tmp)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"only-level-dat-old": "only-level-dat-old", "truncated": "level-dat-broken", "not-gzip": "level-dat-broken",
		"no-data": "level-dat-broken", "gzip-bomb": "level-dat-broken", "deep-nbt": "level-dat-broken", "huge-list": "level-dat-broken",
	}
	for name, d := range dirs {
		t.Run(name, func(t *testing.T) {
			r := mustScan(t, d, name)
			if name == "only-regions" {
				if r.Kind != KindDamagedWorld || !slices.Contains(codes(r.Issues), "only-regions") {
					t.Fatalf("kind=%s issues=%v", r.Kind, codes(r.Issues))
				}
				return
			}
			if r.Kind != KindDamagedWorld || len(r.Candidates) != 1 || !slices.Contains(codes(r.Candidates[0].Issues), want[name]) {
				t.Fatalf("kind=%s cands=%d issues=%v", r.Kind, len(r.Candidates), codes(r.Issues))
			}
		})
	}
}

// 恶意压缩包：可疑的一律整包拒收；CRC 不对、申报大小说谎的在扫描时看不出来，复制到暂存区时必须失败。
func TestHostileArchives(t *testing.T) {
	tmp := t.TempDir()
	for _, hc := range wt.HostileCases() {
		t.Run(hc.Name, func(t *testing.T) {
			z := filepath.Join(tmp, hc.Name+".zip")
			if err := wt.WriteZip(z, hc.Entries, wt.ZipOpts{}); err != nil {
				t.Skipf("生成不了（%v）", err)
			}
			r, err := scanPath(z, hc.Name+".zip", false)
			if err != nil {
				return // 打不开也算拒收
			}
			if r.src != nil {
				defer r.src.Close()
			}
			if r.Kind != KindJavaWorld {
				if !hasBlock(r.Issues) {
					t.Fatalf("没认成世界，但也没说为什么拒收：kind=%s", r.Kind)
				}
				return
			}
			switch hc.Name {
			case "bad-crc", "size-lie":
				info := r.cands[r.Candidates[0].ID]
				files, _, err := resolveMoves(info, mapping{moves: []Move{{From: info.c.Root, To: "world"}}})
				if err != nil {
					t.Fatal(err)
				}
				err = stageFiles(context.Background(), r.src, files, filepath.Join(tmp, hc.Name+"-stage"), nil, nil)
				if err == nil {
					t.Fatal("暂存应该失败")
				}
			case "duplicate-entry":
				t.Fatalf("同名不同内容的条目应该整包拒收：%v", codes(r.Issues))
			default:
				t.Fatalf("应该拒收，却认成了 %d 张地图：%v", len(r.Candidates), codes(r.Issues))
			}
		})
	}
	// 压缩炸弹：申报 1 KB，实际 64 MB
	bomb := filepath.Join(tmp, "bomb.zip")
	if err := wt.BombZip(bomb, 64<<20); err != nil {
		t.Fatal(err)
	}
	r := mustScan(t, bomb, "bomb.zip")
	if r.Kind == KindJavaWorld {
		info := r.cands[r.Candidates[0].ID]
		files, _, _ := resolveMoves(info, mapping{moves: []Move{{From: info.c.Root, To: "world"}}})
		if err := stageFiles(context.Background(), r.src, files, filepath.Join(tmp, "bomb-stage"), nil, nil); err == nil {
			t.Fatal("炸弹应该在暂存时失败")
		}
		var total int64
		filepath.Walk(filepath.Join(tmp, "bomb-stage"), func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				total += fi.Size()
			}
			return nil
		})
		if total > 2<<20 {
			t.Fatalf("炸弹写出了 %d 字节", total)
		}
	}
	// ZIP64 能读
	z64 := filepath.Join(tmp, "z64.zip")
	if err := wt.BigSparseZip64(z64); err != nil {
		t.Fatal(err)
	}
	if r := mustScan(t, z64, "z64.zip"); r.Kind == KindArchive || slices.Contains(codes(r.Issues), "zip-broken") {
		t.Fatalf("zip64: kind=%s issues=%v", r.Kind, codes(r.Issues))
	}
}

// 文件夹来源里的目录联接：不跟进去，拦下。
func TestFolderJunction(t *testing.T) {
	tmp := t.TempDir()
	w := filepath.Join(tmp, "w")
	if err := wt.Legacy(w, wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside")
	write(t, outside, map[string]string{"secret.txt": "x"})
	if err := mkJunction(filepath.Join(w, "data", "link"), outside); err != nil {
		t.Skipf("建不了目录联接：%v", err)
	}
	r := mustScan(t, w, "w")
	if !slices.Contains(codes(r.Issues), "links") {
		t.Fatalf("issues = %v", codes(r.Issues))
	}
	for _, e := range r.src.files {
		if strings.Contains(e.rel, "secret") {
			t.Fatal("跟进了目录联接")
		}
	}
}

func TestMapping(t *testing.T) {
	tmp := t.TempDir()
	// 旧 Bukkit → 原版类目标：DIM-1 / DIM1 并进主目录，_nether 里的服务器文件不带
	src := filepath.Join(tmp, "bk")
	if err := wt.LegacyBukkit(src, "survival", wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true, Nether: true, End: true, UIDDat: true}); err != nil {
		t.Fatal(err)
	}
	r := mustScan(t, src, "bk")
	info := r.cands[r.Candidates[0].ID]
	mp, is := planMapping(info.c, "vanilla", false, "world")
	if len(is) > 0 {
		t.Fatal(codes(is))
	}
	files, dropped, err := resolveMoves(info, mp)
	if err != nil {
		t.Fatal(err)
	}
	to := map[string]string{}
	for _, f := range files {
		to[strings.ToLower(f.to)] = f.e.rel
	}
	for _, want := range []string{"world/level.dat", "world/region/r.0.0.mca", "world/dim-1/region/r.0.0.mca", "world/dim1/region/r.0.0.mca"} {
		if _, ok := to[want]; !ok {
			t.Errorf("没有 %s：%v", want, to)
		}
	}
	for k := range to {
		if strings.Contains(k, "_nether") || strings.Contains(k, "_the_end") || strings.HasSuffix(k, "session.lock") {
			t.Errorf("不该有 %s", k)
		}
	}
	if len(dropped) == 0 {
		t.Error("_nether 里的 level.dat、uid.dat 应该列为不带")
	}
	// 同一张 Bukkit 地图 → Paper（26.1 以前）：三目录原样，按目标的 level-name 改名
	mp, _ = planMapping(info.c, "bukkit", false, "lobby")
	files, _, _ = resolveMoves(info, mp)
	to = map[string]string{}
	for _, f := range files {
		to[strings.ToLower(f.to)] = f.e.rel
	}
	for _, want := range []string{"lobby/level.dat", "lobby_nether/dim-1/region/r.0.0.mca", "lobby_the_end/dim1/region/r.0.0.mca", "lobby_nether/uid.dat"} {
		if _, ok := to[want]; !ok {
			t.Errorf("没有 %s", want)
		}
	}
	// Paper 26.1 → 原版：五个共用数据文件挪回 data/minecraft/
	pp := filepath.Join(tmp, "pp")
	if err := wt.ModernPaper(pp, wt.WorldOpts{Level: lv("26.1", 4800), Overworld: true}); err != nil {
		t.Fatal(err)
	}
	r = mustScan(t, pp, "pp")
	info = r.cands[r.Candidates[0].ID]
	mp, _ = planMapping(info.c, "fabric", true, "world")
	files, _, err = resolveMoves(info, mp)
	if err != nil {
		t.Fatal(err)
	}
	to = map[string]string{}
	for _, f := range files {
		to[strings.ToLower(f.to)] = f.e.rel
	}
	for _, f := range sharedDataFiles {
		if _, ok := to["world/data/minecraft/"+f]; !ok {
			t.Errorf("%s 没挪到 data/minecraft", f)
		}
		if _, ok := to["world/dimensions/minecraft/overworld/data/minecraft/"+f]; ok {
			t.Errorf("%s 还在主世界目录里", f)
		}
	}
	// 新布局放进 26.1 以前的服务器：拦下
	if _, is := planMapping(info.c, "vanilla", false, "world"); !slices.Contains(codes(is), "modern-to-old") {
		t.Error("新布局进旧版本应该被拦下")
	}
}

func TestRelation(t *testing.T) {
	ctx := context.Background()
	tg := &Target{MC: "1.20.4"}
	for _, c := range []struct {
		m    LevelMeta
		want Relation
		code string
	}{
		{LevelMeta{HasVersion: true, VersionName: "1.20.4", DataVersion: 3700}, RelSame, ""},
		{LevelMeta{HasVersion: true, VersionName: "1.21.1"}, RelNewer, "newer"},
		{LevelMeta{HasVersion: true, VersionName: "1.16.5"}, RelOlder, "older"},
		{LevelMeta{HasVersion: true, VersionName: "my-fork-1.0"}, RelUnknown, "version-unknown"},
		{LevelMeta{}, RelOlder, "very-old"},
		{LevelMeta{DataVersion: 100}, RelUnknown, "version-unknown"},
	} {
		rel, is := relation(ctx, fakeOrder, c.m, tg)
		if rel != c.want || c.code != "" && !slices.Contains(codes(is), c.code) {
			t.Errorf("%+v → %s %v", c.m, rel, codes(is))
		}
	}
	// 版本名认不出来，但目标 jar 里有数据版本号：用它判断
	tg.jarWV = 3700
	if rel, _ := relation(ctx, fakeOrder, LevelMeta{HasVersion: true, VersionName: "fork", DataVersion: 3800}, tg); rel != RelNewer {
		t.Errorf("rel = %s", rel)
	}
	if m, ok := isModern(ctx, fakeOrder, "26.2"); !m || !ok {
		t.Error("26.2 应该是新布局")
	}
	if m, ok := isModern(ctx, fakeOrder, "1.21.4"); m || !ok {
		t.Error("1.21.4 不是新布局")
	}
}

func TestModChecks(t *testing.T) {
	c := &Candidate{Meta: LevelMeta{ServerBrands: []string{"vanilla", "forge"}, WasModded: true},
		Dimensions: []Dimension{{ID: "minecraft:overworld"}, {ID: "twilightforest:twilight_forest"}}}
	if is := modChecks(c, "vanilla"); !slices.Contains(codes(is), "mods-mismatch") || is[0].Sev != SevConfirm {
		t.Fatalf("vanilla target: %v", codes(is))
	}
	if is := modChecks(c, "forge"); !slices.Contains(codes(is), "mods-needed") {
		t.Fatalf("forge target: %v", codes(is))
	}
	if is := modChecks(c, "neoforge"); !slices.Contains(codes(is), "mods-mismatch") {
		t.Fatalf("neoforge target: %v", codes(is))
	}
	// Paper 也会把 WasModded 记成 1（实服 1.21.11、26.3 的 Paper 存档都是这样）：不能当成模组世界
	plain := &Candidate{Meta: LevelMeta{ServerBrands: []string{"vanilla", "Paper"}, WasModded: true}}
	if is := modChecks(plain, "vanilla"); len(is) != 0 {
		t.Fatalf("paper brand is not a mod: %v", codes(is))
	}
	if is := modChecks(&Candidate{Meta: LevelMeta{WasModded: true}}, "vanilla"); len(is) != 1 || is[0].Code != "was-modded" || is[0].Sev != SevWarn {
		t.Fatalf("WasModded without brands: %v", codes(is))
	}
}

// 新版本 level.dat 的写法（按实服 1.21.11、26.3 生成的存档核对过）：难度在 difficulty_settings，
// 游戏规则是 game_rules（带命名空间、值是数字），version_history 是数据版本号的列表。
func TestNewLevelFields(t *testing.T) {
	tmp := t.TempDir()
	o := lv("26.3", 5023)
	o.DataExtra = wt.Compound{
		{Name: "difficulty_settings", Value: wt.Compound{{Name: "difficulty", Value: wt.String("hard")}, {Name: "hardcore", Value: wt.Byte(1)}}},
		{Name: "game_rules", Value: wt.Compound{{Name: "minecraft:command_blocks_work", Value: wt.Byte(0)}, {Name: "minecraft:keep_inventory", Value: wt.Byte(1)}}},
		{Name: "version_history", Value: wt.List{Elem: wt.TagInt, Items: []any{wt.Int(4671), wt.Int(5023)}}},
	}
	d := filepath.Join(tmp, "w")
	if err := wt.Modern(d, wt.WorldOpts{Level: o, Overworld: true}); err != nil {
		t.Fatal(err)
	}
	c := mustScan(t, d, "w").Candidates[0]
	m := c.Meta
	if !m.HasDifficulty || m.Difficulty != 3 || !m.Hardcore || strings.Join(m.VersionHistory, ",") != "4671,5023" {
		t.Fatalf("meta = %+v", m)
	}
	if v, ok := m.gameRule("keepInventory", "keep_inventory"); !ok || v != "1" {
		t.Fatalf("keep_inventory = %q %v", v, ok)
	}
	if !slices.Contains(codes(playNotes(&c)), "command-blocks-off") {
		t.Fatalf("notes = %v", codes(playNotes(&c)))
	}
	sug := settingSuggestions(m, map[string]string{"difficulty": "easy", "hardcore": "false", "gamemode": "survival"})
	var keys []string
	for _, s := range sug {
		keys = append(keys, s.Key+"="+s.After)
	}
	if strings.Join(keys, ",") != "difficulty=hard,hardcore=true" {
		t.Fatalf("suggestions = %v", keys)
	}
}

// 复审 #2：名字里有大小写换算以后字节数会变的字符（土耳其语的 ı、ſ、ȿ），按字节切会切错位置甚至越界。
func TestFoldByteLength(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"kıta", "ıııııııııı", "ſky ȿ", "ɐɐ"} {
		src := filepath.Join(tmp, "src-"+name)
		if err := wt.Legacy(filepath.Join(src, name), wt.WorldOpts{Level: lv("1.20.4", 3700), Overworld: true, Nether: true,
			Players: []string{"069a79f4-44e9-4726-a1a5-95721e2c6d2a"}, Datapacks: []string{"p"}}); err != nil {
			t.Fatal(err)
		}
		z := filepath.Join(tmp, "z-"+name+".zip")
		if err := wt.ZipTree(z, src, "包/", wt.ZipOpts{}); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{src, z} {
			r := mustScan(t, p, name)
			if len(r.Candidates) != 1 {
				t.Fatalf("%s: %d 个候选", name, len(r.Candidates))
			}
			c := r.Candidates[0]
			if c.Dimensions[0].Regions != 1 || c.PlayerFiles != 1 || len(c.Datapacks) != 1 {
				t.Fatalf("%s: regions=%d players=%d packs=%v", name, c.Dimensions[0].Regions, c.PlayerFiles, c.Datapacks)
			}
			info := r.cands[c.ID]
			mp, _ := planMapping(info.c, "vanilla", false, "world")
			files, _, err := resolveMoves(info, mp)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if !strings.HasPrefix(f.to, "world/") || strings.Contains(f.to, name) {
					t.Fatalf("%s: 落点不对 %s ← %s", name, f.to, f.e.rel)
				}
			}
		}
	}
}
