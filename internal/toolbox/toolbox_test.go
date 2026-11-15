package toolbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/modcart"
	"minedeck/internal/modpack"
)

func makeSkinPNGFile(t *testing.T, dir, name string, w, h int, paint func(img *image.NRGBA)) string {
	t.Helper()
	p := filepath.Join(dir, name)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	// 默认铺一点像素（顶面 + 右臂/右腿区分色，方便断言 64×32 扩容镜像）
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 3), B: 180, A: 255})
		}
	}
	if paint != nil {
		paint(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOfflineUUIDHashCodeAndSlot 核实离线 UUID v3、Java UUID.hashCode()（含负哈希）与 FloorMod 在新老版本的默认皮肤槽位。
func TestOfflineUUIDHashCodeAndSlot(t *testing.T) {
	// Notch 离线 UUID 为 Minecraft 玩家社区公认样例：b50ad385-829d-3141-a216-7e7d7539ba7f
	uuidStr, hash, slot, modern, err := ResolvePlayerSlot("Notch", "1.21.4")
	if err != nil {
		t.Fatal(err)
	}
	if uuidStr != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("Notch 离线 UUID 不对: got %s", uuidStr)
	}
	if !modern {
		t.Fatalf("1.21.4 应使用 18 槽位规则")
	}
	// 手工复核 OpenJDK UUID.hashCode() 与 Math.floorMod(hash, 18)
	u := OfflineUUIDBytes("Notch")
	msb := binary.BigEndian.Uint64(u[:8])
	lsb := binary.BigEndian.Uint64(u[8:])
	hilo := msb ^ lsb
	wantHash := int32(hilo>>32) ^ int32(hilo)
	if hash != wantHash {
		t.Fatalf("hashCode 不对: got %d want %d", hash, wantHash)
	}
	wantIdx := int(wantHash) % 18
	if wantIdx < 0 {
		wantIdx += 18
	}
	if slot.Index != wantIdx || !strings.HasPrefix(slot.Path, "assets/minecraft/textures/entity/player/") {
		t.Fatalf("1.21.4 槽位结果不对: %+v", slot)
	}

	// 验证负 hashCode 下 FloorMod 不返回负索引，并与 1.19.2 的 2 槽位（0: steve, 1: alex）一致
	var negPlayer string
	for i := 0; i < 200; i++ {
		cand := fmt.Sprintf("TestNegPlayer_%d", i)
		if UUIDHashCode(OfflineUUIDBytes(cand)) < 0 {
			negPlayer = cand
			break
		}
	}
	if negPlayer == "" {
		t.Fatal("没有找到负 hashCode 的测试玩家名")
	}
	_, negHash, legacySlot, isModern, err := ResolvePlayerSlot(negPlayer, "1.19.2")
	if err != nil || isModern {
		t.Fatalf("1.19.2 应使用 2 槽位规则: err=%v isModern=%v", err, isModern)
	}
	if negHash >= 0 {
		t.Fatalf("期望负 hash: %d", negHash)
	}
	wantLegacyIdx := int(negHash) & 1
	if legacySlot.Index != wantLegacyIdx {
		t.Fatalf("负 hashCode 1.19.2 槽位 %d != %d", legacySlot.Index, wantLegacyIdx)
	}
	if (wantLegacyIdx == 0 && legacySlot.Path != "assets/minecraft/textures/entity/steve.png") ||
		(wantLegacyIdx == 1 && legacySlot.Path != "assets/minecraft/textures/entity/alex.png") {
		t.Fatalf("1.19.2 贴图路径不对: %s", legacySlot.Path)
	}
}

func TestPackFormatAndMcmetaVersions(t *testing.T) {
	cases := []struct {
		mc        string
		wantPF    int
		hasMinMax bool
	}{
		{"1.8.9", 1, false},
		{"1.12.2", 3, false},
		{"1.16.5", 6, false},
		{"1.19.2", 9, false},
		{"1.19.3", 12, false},
		{"1.20.1", 15, false},
		{"1.21.4", 46, false},
		{"1.21.8", 64, false},
		{"1.21.9", 69, true},
		{"1.21.11", 75, true},
		{"26.1", 84, true},
		{"26.1.2", 84, true},
		{"26.2", 88, true},
		{"26.3", 97, true},
		{"26.4", 97, true}, // 比表里新的按最新一档算
	}
	for _, tc := range cases {
		pf, b, minFmt, maxFmt, err := buildPackMcmeta(tc.mc, "测试描述")
		if err != nil {
			t.Fatalf("%s 失败: %v", tc.mc, err)
		}
		if pf != tc.wantPF {
			t.Fatalf("%s pack_format=%d want %d", tc.mc, pf, tc.wantPF)
		}
		// max_format 写整数才覆盖这个大版本的所有小版本（26.3 是 97.1）；写成 [97, 0] 会把 97.1 排除
		want := fmt.Sprintf(`"min_format": %d`, tc.wantPF)
		if tc.hasMinMax && (minFmt != tc.wantPF || maxFmt != tc.wantPF || !bytes.Contains(b, []byte(want)) ||
			!bytes.Contains(b, []byte(fmt.Sprintf(`"max_format": %d`, tc.wantPF)))) {
			t.Fatalf("%s min_format/max_format 不对: min=%v max=%v\n%s", tc.mc, minFmt, maxFmt, b)
		}
		if !tc.hasMinMax && (minFmt != 0 || maxFmt != 0 || bytes.Contains(b, []byte("min_format"))) {
			t.Fatalf("%s 不应生成 min_format/max_format", tc.mc)
		}
		if bytes.Contains(b, []byte("supported_formats")) && pf >= 65 {
			t.Fatalf("%s >=65 不应带 supported_formats", tc.mc)
		}
	}
}

func TestSkinPackBuildValidationCollisionAndLegacyUpgrade(t *testing.T) {
	tmp := t.TempDir()
	svc := New(Options{DataDir: filepath.Join(tmp, "data"), CacheDir: filepath.Join(tmp, "cache")})

	// 找两个在 1.21.4 下一定会撞进同一个 18 槽位的名字
	pA := "Alice_0"
	_, _, slotA, _, _ := ResolvePlayerSlot(pA, "1.21.4")
	var pB string
	for i := 1; i < 500; i++ {
		cand := fmt.Sprintf("Bob_%d", i)
		_, _, sB, _, _ := ResolvePlayerSlot(cand, "1.21.4")
		if sB.Index == slotA.Index {
			pB = cand
			break
		}
	}
	if pB == "" {
		t.Fatal("未能构造同槽位冲突的两个玩家名")
	}

	// 准备一张 64×64、一张老格式 64×32（给右腿外正面标特定颜色方便验证扩展镜像）和几张坏图
	skin64 := makeSkinPNGFile(t, tmp, "../../evil-traversal.png"[6:], 64, 64, nil)
	skin32 := makeSkinPNGFile(t, tmp, "legacy_64x32.png", 64, 32, func(img *image.NRGBA) {
		// 右腿正面左上角 (4, 20) 涂纯红，镜像后应落在左腿正面右上角 (20+3, 52) = (23, 52)
		img.SetNRGBA(4, 20, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		// 右臂正面左上角 (44, 20) 涂纯绿，镜像后应落在左臂正面右上角 (36+3, 52) = (39, 52)
		img.SetNRGBA(44, 20, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
	})

	// 1. 预览应报撞槽冲突
	prev, err := PreviewSkinPack(SkinPreviewReq{
		MC:      "1.21.4",
		Mode:    "player",
		Entries: []SkinEntrySpec{{Player: pA}, {Player: pB}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prev.Conflicts) != 1 || len(prev.Conflicts[0].Players) != 2 {
		t.Fatalf("未检测到预期的撞槽冲突: %+v", prev.Conflicts)
	}

	// 2. 不选二选一直接打包必须被拒
	if _, err := svc.BuildSkinPack(SkinBuildReq{
		MC:            "1.21.4",
		Mode:          "player",
		Entries:       []SkinEntrySpec{{Player: pA}, {Player: pB}},
		UploadedPaths: map[int]string{0: skin64, 1: skin32},
	}); err == nil {
		t.Fatal("撞槽未二选一时不应打包成功")
	}

	// 3. 选保留 pB（64×32 老格式），打包应成功且自动升成 64×64
	saveDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := svc.BuildSkinPack(SkinBuildReq{
		MC:       "1.21.4",
		PackName: "朋友开黑皮肤",
		Mode:     "player",
		Entries: []SkinEntrySpec{
			{Player: pA, Keep: false},
			{Player: pB, Keep: true},
		},
		UploadedPaths: map[int]string{0: skin64, 1: skin32},
		DestDir:       saveDir,
	})
	if err != nil {
		t.Fatalf("选择保留一方后生成失败: %v", err)
	}
	if res.TextureCount != 1 || res.SavedPath == "" || !res.Items[1].UpgradedFrom64x32 {
		t.Fatalf("生成摘要异常: %+v", res)
	}

	zipBytes, err := os.ReadFile(res.SavedPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(zipBytes)
	if hex.EncodeToString(sum[:]) != res.SHA1 {
		t.Fatalf("返回 SHA1 与真实 zip 不一致")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		entries[f.Name] = b
	}
	if _, ok := entries["pack.mcmeta"]; !ok {
		t.Fatal("缺少 pack.mcmeta")
	}
	if _, ok := entries["说明.txt"]; !ok {
		t.Fatal("缺少 说明.txt")
	}
	texBytes, ok := entries[slotA.Path]
	if !ok {
		t.Fatalf("缺少目标槽位贴图 %s，现有：%v", slotA.Path, keysOf(entries))
	}
	decoded, err := png.Decode(bytes.NewReader(texBytes))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatalf("64×32 升级后尺寸不对: %v", decoded.Bounds())
	}
	r, _, _, _ := decoded.At(23, 52).RGBA()
	_, g, _, _ := decoded.At(39, 52).RGBA()
	if r>>8 != 255 || g>>8 != 255 {
		t.Fatalf("64×32 镜像左腿/左臂像素不对: legR=%d armG=%d", r>>8, g>>8)
	}

	// 4. 按顺序模式也应正常工作（不需玩家名）
	seqRes, err := svc.BuildSkinPack(SkinBuildReq{
		MC:            "1.21.4",
		Mode:          "sequential",
		Entries:       []SkinEntrySpec{{Model: "slim", Slot: -1}, {Model: "wide", Slot: -1}},
		UploadedPaths: map[int]string{0: skin64, 1: skin64},
	})
	if err != nil {
		t.Fatalf("顺序模式失败: %v", err)
	}
	if seqRes.TextureCount != 2 || seqRes.Items[0].Slot.Model != "slim" || seqRes.Items[1].Slot.Model != "wide" {
		t.Fatalf("顺序模式模型匹配不对: %+v", seqRes.Items)
	}

	// 4b. 顺序模式：手动指定的槽位先占，自动的挑空着的，不撞；自动的那条被跳过时不分槽位、也不报错
	seqPrev, err := PreviewSkinPack(SkinPreviewReq{MC: "1.21.4", Mode: "sequential",
		Entries: []SkinEntrySpec{{Slot: -1}, {Slot: 0}}})
	if err != nil || len(seqPrev.Conflicts) != 0 || seqPrev.Items[0].Slot.Index == 0 || seqPrev.Items[1].Slot.Index != 0 {
		t.Fatalf("手动槽位应先占、自动的避开: err=%v %+v", err, seqPrev.Items)
	}
	seqSkip, err := svc.BuildSkinPack(SkinBuildReq{
		MC: "1.21.4", Mode: "sequential",
		Entries:       []SkinEntrySpec{{Slot: -1, Skip: true}, {Slot: 0, Keep: true}},
		UploadedPaths: map[int]string{1: skin64},
	})
	if err != nil || seqSkip.TextureCount != 1 || !seqSkip.Items[0].Skipped || seqSkip.Items[0].Slot.Index != -1 {
		t.Fatalf("跳过自动分配的那条后应能生成: err=%v %+v", err, seqSkip)
	}

	// 5. 拒绝坏图片、错误尺寸、超大小、超数量
	badText := filepath.Join(tmp, "fake.png")
	_ = os.WriteFile(badText, []byte("not a png image"), 0o644)
	if _, err := svc.BuildSkinPack(SkinBuildReq{
		MC: "1.21.4", Mode: "sequential", Entries: []SkinEntrySpec{{Slot: -1}}, UploadedPaths: map[int]string{0: badText},
	}); err == nil {
		t.Fatal("假 PNG 未被拒绝")
	}
	wrongDim := makeSkinPNGFile(t, tmp, "wrong.png", 128, 128, nil)
	if _, err := svc.BuildSkinPack(SkinBuildReq{
		MC: "1.21.4", Mode: "sequential", Entries: []SkinEntrySpec{{Slot: -1}}, UploadedPaths: map[int]string{0: wrongDim},
	}); err == nil {
		t.Fatal("128×128 图片未被拒绝")
	}
	tooBig := filepath.Join(tmp, "toobig.png")
	_ = os.WriteFile(tooBig, bytes.Repeat([]byte("A"), MaxSkinBytes+10), 0o644)
	if _, err := svc.BuildSkinPack(SkinBuildReq{
		MC: "1.21.4", Mode: "sequential", Entries: []SkinEntrySpec{{Slot: -1}}, UploadedPaths: map[int]string{0: tooBig},
	}); err == nil {
		t.Fatal("超过 2MB 图片未被拒绝")
	}
	many := make([]SkinEntrySpec, MaxSkins+1)
	if _, err := PreviewSkinPack(SkinPreviewReq{MC: "1.21.4", Mode: "sequential", Entries: many}); err == nil {
		t.Fatal("超过 18 张皮肤未被拒绝")
	}
}

func TestMrpackExportAndRoundTripParse(t *testing.T) {
	oldDirs := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = oldDirs[0], oldDirs[1], oldDirs[2], oldDirs[3], oldDirs[4]
	})
	base := t.TempDir()
	app.Base = base
	app.ServersDir = filepath.Join(base, "servers")
	app.BackupsDir = filepath.Join(base, "backups")
	app.CacheDir = filepath.Join(base, "cache")
	app.RuntimesDir = filepath.Join(base, "runtimes")

	// 准备模拟的实例 mods 字节和它们的 SHA-1 / SHA-512
	jarKnown := validMinimalJar("knownmod", "fabric")
	jarClient := validMinimalJar("minimap", "fabric")
	jarBadHost := validMinimalJar("customsite", "fabric")
	jarLocal := validMinimalJar("localonly", "fabric")

	sha1Hex := func(b []byte) string { s := sha1.Sum(b); return hex.EncodeToString(s[:]) }
	sha512Hex := func(b []byte) string { s := sha512.Sum512(b); return hex.EncodeToString(s[:]) }

	hKnown1, hKnown512 := sha1Hex(jarKnown), sha512Hex(jarKnown)
	hClient1, hClient512 := sha1Hex(jarClient), sha512Hex(jarClient)
	hBad1, hBad512 := sha1Hex(jarBadHost), sha512Hex(jarBadHost)
	hDep1, hDep512 := sha1Hex(jarKnown), sha512Hex(jarKnown) // 复用字节简化前置条目

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/projects":
			reply([]map[string]any{
				{"id": "p-known", "slug": "sodium", "title": "Sodium", "project_type": "mod", "server_side": "optional", "license": map[string]any{"id": "LGPL-3.0"}},
				{"id": "p-client", "slug": "xaeros-minimap", "title": "Xaero's Minimap", "project_type": "mod", "server_side": "unsupported", "client_side": "required", "license": map[string]any{"id": "All-Rights-Reserved"}},
				{"id": "p-badhost", "slug": "badhost-mod", "title": "BadHost Mod", "project_type": "mod", "server_side": "required", "license": map[string]any{"id": "MIT"}},
				{"id": "p-dep", "slug": "fabric-api", "title": "Fabric API", "project_type": "mod", "server_side": "required", "license": map[string]any{"id": "Apache-2.0"}},
			})
		case "/project/p-known/version":
			reply([]map[string]any{{
				"id": "v-known", "project_id": "p-known", "version_number": "0.5.8", "version_type": "release",
				"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
				"files": []map[string]any{{
					"url": "https://cdn.modrinth.com/data/p-known/versions/v-known/sodium-0.5.8.jar", "filename": "sodium-0.5.8.jar", "primary": true, "size": len(jarKnown),
					"hashes": map[string]string{"sha1": hKnown1, "sha512": hKnown512},
				}},
				"dependencies": []map[string]any{{"project_id": "p-dep", "dependency_type": "required"}},
			}})
		case "/project/p-client/version":
			reply([]map[string]any{{
				"id": "v-client", "project_id": "p-client", "version_number": "24.2.0", "version_type": "release",
				"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
				"files": []map[string]any{{
					"url": "https://cdn.modrinth.com/data/p-client/versions/v-client/minimap-24.2.0.jar", "filename": "minimap-24.2.0.jar", "primary": true, "size": len(jarClient),
					"hashes": map[string]string{"sha1": hClient1, "sha512": hClient512},
				}},
				"dependencies": []map[string]any{},
			}})
		case "/project/p-dep/version":
			reply([]map[string]any{{
				"id": "v-dep", "project_id": "p-dep", "version_number": "0.92.0", "version_type": "release",
				"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
				"files": []map[string]any{{
					"url": "https://cdn.modrinth.com/data/p-dep/versions/v-dep/fabric-api-0.92.0.jar", "filename": "fabric-api-0.92.0.jar", "primary": true, "size": len(jarKnown),
					"hashes": map[string]string{"sha1": hDep1, "sha512": hDep512},
				}},
				"dependencies": []map[string]any{},
			}})
		case "/version_files":
			reply(map[string]any{
				hKnown1: map[string]any{
					"id": "v-known", "project_id": "p-known", "version_number": "0.5.8",
					"files": []map[string]any{{"url": "https://cdn.modrinth.com/data/p-known/versions/v-known/sodium-0.5.8.jar", "filename": "sodium-0.5.8.jar", "primary": true, "size": len(jarKnown), "hashes": map[string]string{"sha1": hKnown1, "sha512": hKnown512}}},
				},
				hClient1: map[string]any{
					"id": "v-client", "project_id": "p-client", "version_number": "24.2.0",
					"files": []map[string]any{{"url": "https://cdn.modrinth.com/data/p-client/versions/v-client/minimap-24.2.0.jar", "filename": "minimap-24.2.0.jar", "primary": true, "size": len(jarClient), "hashes": map[string]string{"sha1": hClient1, "sha512": hClient512}}},
				},
				hBad1: map[string]any{
					"id": "v-bad", "project_id": "p-badhost", "version_number": "1.0.0",
					"files": []map[string]any{{"url": "https://untrusted.example.com/badhost-1.0.jar", "filename": "badhost-1.0.jar", "primary": true, "size": len(jarBadHost), "hashes": map[string]string{"sha1": hBad1, "sha512": hBad512}}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// 1. 建立一个停止状态的 Fabric 实例并放入测试 mods 和 config
	instDir := filepath.Join(app.ServersDir, "modded")
	modsDir := filepath.Join(instDir, "mods")
	cfgDir := filepath.Join(instDir, "config")
	_ = os.MkdirAll(modsDir, 0o755)
	_ = os.MkdirAll(cfgDir, 0o755)
	sBytes, _ := json.Marshal(inst.Settings{Name: "测试模组服", Core: "fabric", MC: "1.20.1", Build: "0.16.9", XmxMB: 2048, XmsMB: 1024})
	_ = os.WriteFile(filepath.Join(instDir, "instance.json"), sBytes, 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "sodium.jar"), jarKnown, 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "minimap.jar"), jarClient, 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "badhost.jar"), jarBadHost, 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "localonly.jar"), jarLocal, 0o644)
	_ = os.WriteFile(filepath.Join(modsDir, "disabled.jar.disabled"), jarLocal, 0o644)
	_ = os.WriteFile(filepath.Join(cfgDir, "sodium-options.json"), []byte(`{"v":1}`), 0o644)
	// 像放着令牌、密钥的配置不进包；留空的令牌、开关、纯数字不算
	_ = os.WriteFile(filepath.Join(cfgDir, "plain.toml"), []byte("enableTokens = true\ntoken = \"\"\nmaxTokens = 123456\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cfgDir, "chatbridge.toml"), []byte("[bot]\nbotToken = \"MTIzNDU2Nzg5.abcdefghijkl\"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cfgDir, "ai.json"), []byte(`{"apiKey": "sk-abcdef123456"}`), 0o644)
	_ = os.MkdirAll(filepath.Join(cfgDir, "luckperms"), 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "luckperms", "luckperms.conf"), []byte("storage-method = h2\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cfgDir, "data.db"), []byte("SQLite format 3"), 0o644)

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cart := modcart.New(filepath.Join(base, "data", "modcart"), srv.URL)
	svc := New(Options{Mgr: mgr, Cart: cart, DataDir: filepath.Join(base, "data", "toolbox"), CacheDir: filepath.Join(base, "cache", "toolbox")})

	// 实例预览 + 导出
	outDir := filepath.Join(base, "exports")
	_ = os.MkdirAll(outDir, 0o755)
	res, err := svc.BuildMrpack(context.Background(), MrpackReq{
		Source:        "instance",
		Inst:          "测试模组服",
		Name:          "朋友端整合包",
		VersionID:     "1.2.0",
		Summary:       "测试导出",
		IncludeConfig: true,
		DestDir:       outDir,
	})
	if err != nil {
		t.Fatalf("实例导出失败: %v", err)
	}
	// known + minimap 走 downloads（其中 minimap 是 client-only）；badhost（非白名单域名）+ localonly + config/sodium-options.json 走 overrides
	if res.DownloadCount != 2 || res.ClientOnlyCnt != 1 || res.OverrideCount < 4 || res.ConfigCount != 2 {
		t.Fatalf("实例导出分类数量不对: %+v", res)
	}
	for _, leaked := range []string{"chatbridge.toml", "ai.json", "luckperms/luckperms.conf", "data.db"} {
		if hasZipEntry(t, res.SavedPath, "overrides/config/"+leaked) {
			t.Fatalf("像放着密钥的配置 %s 被打进了包", leaked)
		}
	}
	if !hasZipEntry(t, res.SavedPath, "overrides/config/plain.toml") {
		t.Fatal("没带密钥的 plain.toml 应该打进包")
	}
	if !slices.ContainsFunc(res.Warnings, func(w string) bool {
		return strings.Contains(w, "config/chatbridge.toml") && strings.Contains(w, "config/ai.json") && strings.Contains(w, "config/data.db")
	}) {
		t.Fatalf("跳过的配置要写进提醒: %v", res.Warnings)
	}

	// 用 MineDeck 自己的 modpack.Parse 读回测试！
	pack, err := modpack.Parse(res.SavedPath)
	if err != nil {
		t.Fatalf("modpack.Parse 读回导出包失败: %v", err)
	}
	if pack.Format != modpack.FormatMrpack || pack.Name != "朋友端整合包" || pack.MC != "1.20.1" || pack.Core != "fabric" || pack.Loader != "0.16.9" {
		t.Fatalf("modpack.Parse 解析元数据不对: %+v", pack)
	}
	// 仅客户端的 minimap（env.server: unsupported）被导入服务端时必须自动跳过！
	if len(pack.Files) != 1 || pack.Files[0].Path != "mods/sodium-0.5.8.jar" {
		t.Fatalf("modpack.Parse 过滤 env.server 后 Files 不对: %+v", pack.Files)
	}
	// 校验 modrinth.index.json 含 sha1 + sha512 且非白名单域名的 badhost 进 overrides 而不是 files[]
	idx := readIndexFromZip(t, res.SavedPath)
	if len(idx.Files) != 2 {
		t.Fatalf("index files 数量=%d want 2", len(idx.Files))
	}
	for _, f := range idx.Files {
		if len(f.Hashes.SHA1) != 40 || len(f.Hashes.SHA512) != 128 {
			t.Fatalf("缺少 sha1 或 sha512: %+v", f.Hashes)
		}
		if f.Path == "mods/minimap-24.2.0.jar" && f.Env.Server != "unsupported" {
			t.Fatalf("仅客户端模组 env.server 应为 unsupported，实际=%s", f.Env.Server)
		}
	}
	if hasZipEntry(t, res.SavedPath, "mods/disabled.jar.disabled") || hasZipEntry(t, res.SavedPath, "overrides/mods/disabled.jar.disabled") {
		t.Fatal("已停用的 .disabled 不应被打进整合包")
	}
	if !hasZipEntry(t, res.SavedPath, "overrides/mods/badhost.jar") || !hasZipEntry(t, res.SavedPath, "overrides/mods/localonly.jar") {
		t.Fatal("白名单外链接或本地模组未进 overrides/mods")
	}

	// 2. 草稿清单持久化 + 草稿模式导出（自动补出 Fabric API 必装前置） + local jar 进 overrides + 再次用 modpack.Parse 读回
	if _, err := svc.SaveMrpackDraft(DraftConfig{
		Name: "向导草稿包", VersionID: "0.1.0", Core: "fabric", MC: "1.20.1", LoaderVersion: "0.16.9",
	}); err != nil {
		t.Fatal(err)
	}
	// 重新建一个 Service 实例模拟重启恢复
	svc2 := New(Options{Mgr: mgr, Cart: cart, DataDir: filepath.Join(base, "data", "toolbox"), CacheDir: filepath.Join(base, "cache", "toolbox")})
	gotDraft := svc2.GetMrpackDraft()
	if gotDraft.Name != "向导草稿包" || gotDraft.LoaderVersion != "0.16.9" {
		t.Fatalf("草稿设置重启恢复失败: %+v", gotDraft)
	}
	// 工具箱用自己的那份清单：只在客户端用的模组也能放；草稿清单（新建向导用，建服时会被装进去）不受影响
	if _, _, err := cart.Add(context.Background(), modcart.Toolbox, []string{"p-known", "p-client"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cart.Add(context.Background(), modcart.Draft, []string{"p-client"}); err == nil {
		t.Fatal("草稿清单是要装进服务器的，不该收只在客户端用的模组")
	}
	stageSrc := filepath.Join(base, "staged.jar")
	_ = os.WriteFile(stageSrc, jarLocal, 0o644)
	if _, _, err := cart.AddLocal(modcart.Toolbox, stageSrc, "custom-staged.jar"); err != nil {
		t.Fatal(err)
	}
	if len(cart.List(modcart.Draft)) != 0 || len(cart.Locals(modcart.Draft)) != 0 {
		t.Fatal("工具箱挑的模组跑进了新建向导的草稿清单")
	}

	draftRes, err := svc2.BuildMrpack(context.Background(), MrpackReq{Source: "draft"})
	if err != nil {
		t.Fatalf("草稿导出失败: %v", err)
	}
	if draftRes.DownloadCount != 3 || draftRes.OverrideCount != 1 || draftRes.ClientOnlyCnt != 1 {
		t.Fatalf("草稿导出期望 3 个下载项（含自动补前置 Fabric API 和只在客户端用的小地图）和 1 个 override，实际=%+v", draftRes)
	}
	dlEntry, err := svc2.GetDownload(draftRes.DownloadID)
	if err != nil {
		t.Fatal(err)
	}
	draftPack, err := modpack.Parse(dlEntry.Path)
	if err != nil {
		t.Fatalf("modpack.Parse 读回草稿包失败: %v", err)
	}
	// modpack.Parse 是按装服务器读的：只在客户端用的小地图被跳过，剩两个
	if draftPack.Name != "向导草稿包" || len(draftPack.Files) != 2 {
		t.Fatalf("草稿整合包读回不对: %+v", draftPack)
	}
	if !hasZipEntry(t, dlEntry.Path, "overrides/mods/custom-staged.jar") {
		t.Fatal("草稿里的本地 jar 未进 overrides/mods")
	}
}

func validMinimalJar(modID, loader string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if loader == "fabric" {
		w, _ := zw.Create("fabric.mod.json")
		_, _ = fmt.Fprintf(w, `{"schemaVersion":1,"id":%q,"version":"1.0.0","name":%q}`, modID, strings.ToUpper(modID))
	} else {
		w, _ := zw.Create("plugin.yml")
		_, _ = fmt.Fprintf(w, "name: %s\nversion: 1.0.0\nmain: example.Main\n", modID)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func readIndexFromZip(t *testing.T, zipPath string) modrinthIndex {
	t.Helper()
	b, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			var idx modrinthIndex
			if err := json.NewDecoder(rc).Decode(&idx); err != nil {
				t.Fatal(err)
			}
			return idx
		}
	}
	t.Fatal("zip 里缺少 modrinth.index.json")
	return modrinthIndex{}
}

func hasZipEntry(t *testing.T, zipPath, name string) bool {
	t.Helper()
	b, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
