package worldtest

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// --- Independent test-only NBT reader ---

type decodedTag struct {
	Tag   byte
	Value any
}

func readNBT(r io.Reader) (string, map[string]decodedTag, error) {
	var tag [1]byte
	if _, err := io.ReadFull(r, tag[:]); err != nil {
		return "", nil, err
	}
	if tag[0] != TagCompound {
		return "", nil, fmt.Errorf("expected root TagCompound, got %d", tag[0])
	}
	name, err := readModUTF8(r)
	if err != nil {
		return "", nil, err
	}
	val, err := readCompound(r)
	if err != nil {
		return "", nil, err
	}
	return name, val, nil
}

func readCompound(r io.Reader) (map[string]decodedTag, error) {
	out := make(map[string]decodedTag)
	var tag [1]byte
	for {
		if _, err := io.ReadFull(r, tag[:]); err != nil {
			return nil, err
		}
		if tag[0] == TagEnd {
			return out, nil
		}
		name, err := readModUTF8(r)
		if err != nil {
			return nil, err
		}
		v, err := readTagPayload(r, tag[0])
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		out[name] = decodedTag{Tag: tag[0], Value: v}
	}
}

type decodedList struct {
	Elem  byte
	Items []any
}

func readTagPayload(r io.Reader, tag byte) (any, error) {
	switch tag {
	case TagByte:
		var v int8
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case TagShort:
		var v int16
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case TagInt:
		var v int32
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case TagLong:
		var v int64
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case TagFloat:
		var bits uint32
		err := binary.Read(r, binary.BigEndian, &bits)
		return math.Float32frombits(bits), err
	case TagDouble:
		var bits uint64
		err := binary.Read(r, binary.BigEndian, &bits)
		return math.Float64frombits(bits), err
	case TagByteArray:
		var n int32
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		buf := make([]byte, n)
		_, err := io.ReadFull(r, buf)
		return buf, err
	case TagString:
		return readModUTF8(r)
	case TagList:
		var elem [1]byte
		if _, err := io.ReadFull(r, elem[:]); err != nil {
			return nil, err
		}
		var n int32
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		items := make([]any, n)
		for i := range n {
			v, err := readTagPayload(r, elem[0])
			if err != nil {
				return nil, err
			}
			items[i] = v
		}
		return decodedList{Elem: elem[0], Items: items}, nil
	case TagCompound:
		return readCompound(r)
	case TagIntArray:
		var n int32
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		arr := make([]int32, n)
		for i := range n {
			if err := binary.Read(r, binary.BigEndian, &arr[i]); err != nil {
				return nil, err
			}
		}
		return arr, nil
	case TagLongArray:
		var n int32
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		arr := make([]int64, n)
		for i := range n {
			if err := binary.Read(r, binary.BigEndian, &arr[i]); err != nil {
				return nil, err
			}
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unknown tag %d", tag)
	}
}

func readModUTF8(r io.Reader) (string, error) {
	var l uint16
	if err := binary.Read(r, binary.BigEndian, &l); err != nil {
		return "", err
	}
	raw := make([]byte, l)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", err
	}
	var units []uint16
	for i := 0; i < len(raw); {
		b0 := raw[i]
		switch {
		case b0&0x80 == 0:
			units = append(units, uint16(b0))
			i++
		case b0&0xE0 == 0xC0:
			if i+1 >= len(raw) {
				return "", errors.New("truncated 2-byte modified UTF-8")
			}
			units = append(units, uint16(b0&0x1F)<<6|uint16(raw[i+1]&0x3F))
			i += 2
		case b0&0xF0 == 0xE0:
			if i+2 >= len(raw) {
				return "", errors.New("truncated 3-byte modified UTF-8")
			}
			units = append(units, uint16(b0&0x0F)<<12|uint16(raw[i+1]&0x3F)<<6|uint16(raw[i+2]&0x3F))
			i += 3
		default:
			return "", fmt.Errorf("invalid modified UTF-8 lead byte 0x%02x", b0)
		}
	}
	return string(utf16.Decode(units)), nil
}

// --- Tests ---

func TestNBTKnownAnswerAndErrors(t *testing.T) {
	got, err := Marshal("hello world", Compound{{"name", String("Bananrama")}})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	wantHex := "0a000b68656c6c6f20776f726c640800046e616d65000942616e616e72616d6100"
	if hex.EncodeToString(got) != wantHex {
		t.Fatalf("hex mismatch:\n got: %s\nwant: %s", hex.EncodeToString(got), wantHex)
	}

	// Plain Go types must fail.
	for _, bad := range []any{123, "plain", true, []byte{1}, int32(5)} {
		if _, err := Marshal("", Compound{{"bad", bad}}); err == nil {
			t.Fatalf("expected error for plain Go type %T", bad)
		}
	}

	// List element mismatch and TagEnd with items must fail.
	if _, err := Marshal("", Compound{{"l", List{Elem: TagInt, Items: []any{String("wrong")}}}}); err == nil {
		t.Fatal("expected error for list element type mismatch")
	}
	if _, err := Marshal("", Compound{{"l", List{Elem: TagEnd, Items: []any{Int(1)}}}}); err == nil {
		t.Fatal("expected error for TagEnd list with items")
	}
}

func TestNBTModifiedUTF8AndUUID(t *testing.T) {
	// NUL -> C0 80
	b, err := Marshal("", Compound{{"s", String("a\x00b")}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte{0x00, 0x04, 'a', 0xC0, 0x80, 'b'}) {
		t.Fatalf("NUL modified UTF-8 encoding unexpected: %x", b)
	}

	// CJK + Emoji U+1F600 round-trip
	cjk := "空岛生存"
	emoji := "\U0001F600" // 😀 -> surrogate pair D83D DE00 -> ED A0 BD ED B8 80
	raw, err := Marshal("root", Compound{
		{Name: "cjk", Value: String(cjk)},
		{Name: "emoji", Value: String(emoji)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte{0xED, 0xA0, 0xBD, 0xED, 0xB8, 0x80}) {
		t.Fatalf("emoji surrogate pair bytes not found in %x", raw)
	}

	rootName, decoded, err := readNBT(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if rootName != "root" {
		t.Fatalf("rootName = %q", rootName)
	}
	if decoded["cjk"].Value != cjk || decoded["emoji"].Value != emoji {
		t.Fatalf("round-trip mismatch: cjk=%v emoji=%v", decoded["cjk"].Value, decoded["emoji"].Value)
	}

	// Overlong string (> 65535 bytes) must error.
	tooLong := strings.Repeat("a", 65536)
	if _, err := Marshal("", Compound{{"big", String(tooLong)}}); err == nil {
		t.Fatal("expected error for string > 65535 bytes")
	}

	// UUIDInts
	ints, err := UUIDInts("00000000-0000-0000-0000-00000000000a")
	if err != nil {
		t.Fatal(err)
	}
	if len(ints) != 4 || ints[0] != 0 || ints[1] != 0 || ints[2] != 0 || ints[3] != 10 {
		t.Fatalf("unexpected UUIDInts: %v", ints)
	}
	if _, err := UUIDInts("not-a-uuid"); err == nil {
		t.Fatal("expected error for invalid UUID")
	}
}

func TestLevelDatRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "level.dat")
	err := WriteLevelDat(p, LevelOpts{
		VersionName:      "1.20.4",
		DataVersion:      3700,
		LegacyVersion:    19133,
		LevelName:        "SkyBlock",
		ServerBrands:     []string{"vanilla", "paper"},
		WasModded:        true,
		GameType:         0,
		Difficulty:       2,
		Hardcore:         true,
		AllowCommands:    true,
		Seed:             42,
		WorldGen:         true,
		CustomDims:       []string{"mymod:sky"},
		GameRules:        map[string]string{"keepInventory": "true", "doDaylightCycle": "false"},
		EnabledPacks:     []string{"vanilla"},
		DisabledPacks:    []string{"bundle"},
		Player:           true,
		SingleplayerUUID: "11111111-2222-3333-4444-555555555555",
		DataExtra:        Compound{{Name: "CustomData", Value: Int(99)}},
		RootExtra:        Compound{{Name: "fml", Value: Compound{{Name: "LoadingModList", Value: List{Elem: TagEnd}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	rootName, root, err := readNBT(gr)
	if err != nil {
		t.Fatal(err)
	}
	if rootName != "" {
		t.Fatalf("expected empty root name, got %q", rootName)
	}
	if _, ok := root["fml"]; !ok {
		t.Fatal("missing RootExtra fml")
	}
	data := root["Data"].Value.(map[string]decodedTag)

	if dv := data["DataVersion"]; dv.Tag != TagInt || dv.Value.(int32) != 3700 {
		t.Fatalf("DataVersion = %+v", dv)
	}
	ver := data["Version"].Value.(map[string]decodedTag)
	if vn := ver["Name"]; vn.Tag != TagString || vn.Value.(string) != "1.20.4" {
		t.Fatalf("Data.Version.Name = %+v", vn)
	}
	if vs := ver["Series"]; vs.Value.(string) != "main" {
		t.Fatalf("Data.Version.Series = %+v", vs)
	}

	sb := data["ServerBrands"]
	if sb.Tag != TagList {
		t.Fatalf("ServerBrands tag = %d", sb.Tag)
	}
	sbList := sb.Value.(decodedList)
	if sbList.Elem != TagString || len(sbList.Items) != 2 || sbList.Items[0] != "vanilla" {
		t.Fatalf("ServerBrands = %+v", sbList)
	}

	player := data["Player"].Value.(map[string]decodedTag)
	pu := player["UUID"]
	if pu.Tag != TagIntArray || len(pu.Value.([]int32)) != 4 {
		t.Fatalf("Data.Player.UUID = %+v", pu)
	}

	if sp := data["singleplayer_uuid"]; sp.Tag != TagIntArray || len(sp.Value.([]int32)) != 4 {
		t.Fatalf("singleplayer_uuid = %+v", sp)
	}
	if ce := data["CustomData"]; ce.Tag != TagInt || ce.Value.(int32) != 99 {
		t.Fatalf("CustomData = %+v", ce)
	}

	// Pre-15w32a & string singleplayer_uuid variant
	preRoot := LevelDat(LevelOpts{
		LegacyVersion:            19133,
		SingleplayerUUID:         "11111111-2222-3333-4444-555555555555",
		SingleplayerUUIDAsString: true,
	})
	rawPre, err := Marshal("", preRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, preDec, err := readNBT(bytes.NewReader(rawPre))
	if err != nil {
		t.Fatal(err)
	}
	preData := preDec["Data"].Value.(map[string]decodedTag)
	if _, hasVer := preData["Version"]; hasVer {
		t.Fatal("expected Data.Version omitted when VersionName is empty")
	}
	if sp := preData["singleplayer_uuid"]; sp.Tag != TagString {
		t.Fatalf("expected string singleplayer_uuid, got tag %d", sp.Tag)
	}
}

func TestWorldBuilders(t *testing.T) {
	uuid := "12345678-1234-5678-1234-567812345678"
	baseOpts := WorldOpts{
		Level: LevelOpts{
			VersionName: "1.20.4",
			DataVersion: 3700,
			LevelName:   "TestWorld",
		},
		Tag:          "X1",
		Overworld:    true,
		Nether:       true,
		End:          true,
		Entities:     true,
		Players:      []string{uuid},
		Datapacks:    []string{"dp1"},
		ResourcesZip: true,
		Icon:         true,
		SessionLock:  true,
		UIDDat:       true,
		CustomDims:   []string{"mymod:sky"},
		ExtraFiles:   map[string][]byte{"readme.txt": []byte("hello")},
	}

	t.Run("Legacy", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "legacy")
		if err := Legacy(dir, baseOpts); err != nil {
			t.Fatal(err)
		}
		assertMarker(t, filepath.Join(dir, "region", "r.0.0.mca"), "MARK:minecraft:overworld:X1")
		assertMarker(t, filepath.Join(dir, "entities", "r.0.0.mca"), "MARK:minecraft:overworld:X1:entities")
		assertMarker(t, filepath.Join(dir, "poi", "r.0.0.mca"), "MARK:minecraft:overworld:X1:poi")
		assertMarker(t, filepath.Join(dir, "DIM-1", "region", "r.0.0.mca"), "MARK:minecraft:the_nether:X1")
		assertMarker(t, filepath.Join(dir, "DIM1", "region", "r.0.0.mca"), "MARK:minecraft:the_end:X1")
		assertMarker(t, filepath.Join(dir, "dimensions", "mymod", "sky", "region", "r.0.0.mca"), "MARK:mymod:sky:X1")
		assertFilesExist(t, dir,
			"level.dat",
			"data/raids.dat",
			"playerdata/"+uuid+".dat",
			"advancements/"+uuid+".json",
			"stats/"+uuid+".json",
			"datapacks/dp1.zip",
			"resources.zip",
			"icon.png",
			"session.lock",
			"readme.txt",
		)
		// Verify icon.png is a real PNG and session.lock is ☃.
		f, err := os.Open(filepath.Join(dir, "icon.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := png.Decode(f); err != nil {
			t.Fatalf("icon.png invalid PNG: %v", err)
		}
		sl, err := os.ReadFile(filepath.Join(dir, "session.lock"))
		if err != nil || string(sl) != "☃" {
			t.Fatalf("session.lock = %x, err=%v", sl, err)
		}
	})

	t.Run("LegacyBukkit", func(t *testing.T) {
		parent := t.TempDir()
		if err := LegacyBukkit(parent, "survival", baseOpts); err != nil {
			t.Fatal(err)
		}
		mainDir := filepath.Join(parent, "survival")
		netherDir := filepath.Join(parent, "survival_nether")
		endDir := filepath.Join(parent, "survival_the_end")

		assertMarker(t, filepath.Join(mainDir, "region", "r.0.0.mca"), "MARK:minecraft:overworld:X1")
		assertMarker(t, filepath.Join(netherDir, "DIM-1", "region", "r.0.0.mca"), "MARK:minecraft:the_nether:X1")
		assertMarker(t, filepath.Join(endDir, "DIM1", "region", "r.0.0.mca"), "MARK:minecraft:the_end:X1")
		if _, err := os.Stat(filepath.Join(mainDir, "DIM-1")); !os.IsNotExist(err) {
			t.Fatal("main bukkit dir should not have DIM-1")
		}
		assertFilesExist(t, mainDir, "level.dat", "uid.dat", "data/raids.dat")
		assertFilesExist(t, netherDir, "level.dat", "uid.dat")
		assertFilesExist(t, endDir, "level.dat", "uid.dat")
	})

	t.Run("Modern", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "modern")
		if err := Modern(dir, baseOpts); err != nil {
			t.Fatal(err)
		}
		assertMarker(t, filepath.Join(dir, "dimensions", "minecraft", "overworld", "region", "r.0.0.mca"), "MARK:minecraft:overworld:X1")
		assertMarker(t, filepath.Join(dir, "dimensions", "minecraft", "the_nether", "region", "r.0.0.mca"), "MARK:minecraft:the_nether:X1")
		assertMarker(t, filepath.Join(dir, "dimensions", "minecraft", "the_end", "region", "r.0.0.mca"), "MARK:minecraft:the_end:X1")
		assertMarker(t, filepath.Join(dir, "dimensions", "mymod", "sky", "region", "r.0.0.mca"), "MARK:mymod:sky:X1")
		assertFilesExist(t, dir,
			"level.dat",
			"players/data/"+uuid+".dat",
			"players/advancements/"+uuid+".json",
			"players/stats/"+uuid+".json",
			"data/minecraft/game_rules.dat",
			"data/minecraft/scheduled_events.dat",
			"data/minecraft/wandering_trader.dat",
			"data/minecraft/weather.dat",
			"data/minecraft/world_gen_settings.dat",
		)
	})

	t.Run("ModernPaper", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "modern-paper")
		if err := ModernPaper(dir, baseOpts); err != nil {
			t.Fatal(err)
		}
		assertFilesExist(t, dir,
			"level.dat",
			"dimensions/minecraft/overworld/data/minecraft/game_rules.dat",
			"dimensions/minecraft/overworld/data/minecraft/scheduled_events.dat",
			"dimensions/minecraft/overworld/data/minecraft/wandering_trader.dat",
			"dimensions/minecraft/overworld/data/minecraft/weather.dat",
			"dimensions/minecraft/overworld/data/minecraft/world_gen_settings.dat",
			"data/paper/level_overrides.dat",
			"dimensions/minecraft/overworld/paper-world.yml",
			"dimensions/minecraft/the_nether/paper-world.yml",
			"dimensions/minecraft/the_end/paper-world.yml",
			"dimensions/mymod/sky/paper-world.yml",
		)
	})
}

func TestArchivesAndHostile(t *testing.T) {
	tmp := t.TempDir()

	// WriteZip: BadCRC, Encrypted, Symlink, ForceZip64
	zPath := filepath.Join(tmp, "flags.zip")
	err := WriteZip(zPath, []ZipEntry{
		{Name: "badcrc.txt", Body: []byte("hello"), BadCRC: true},
		{Name: "enc.txt", Body: []byte("secret"), Encrypted: true},
		{Name: "sym", Body: []byte("/etc/passwd"), Mode: fs.ModeSymlink | 0o777},
		{Name: "dir/", Dir: true},
	}, ZipOpts{ForceZip64: true})
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(zPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(zr.File))
	}

	for _, f := range zr.File {
		switch f.Name {
		case "badcrc.txt":
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(rc)
			_ = rc.Close()
			if !errors.Is(err, zip.ErrChecksum) {
				t.Fatalf("expected zip.ErrChecksum for BadCRC, got %v", err)
			}
		case "enc.txt":
			if f.Flags&1 == 0 {
				t.Fatal("expected Encrypted flag bit 0 set")
			}
		case "sym":
			if f.Mode()&fs.ModeSymlink == 0 {
				t.Fatalf("expected symlink mode, got %v", f.Mode())
			}
		}
	}

	// BigSparseZip64
	z64Path := filepath.Join(tmp, "sparse64.zip")
	if err := BigSparseZip64(z64Path); err != nil {
		t.Fatal(err)
	}
	z64r, err := zip.OpenReader(z64Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(z64r.File) != 65536 {
		_ = z64r.Close()
		t.Fatalf("expected 65536 entries in BigSparseZip64, got %d", len(z64r.File))
	}
	_ = z64r.Close()

	// ZipTree
	src := filepath.Join(tmp, "src")
	if err := writeFileBytes(filepath.Join(src, "a", "b.txt"), []byte("ab")); err != nil {
		t.Fatal(err)
	}
	treeZip := filepath.Join(tmp, "tree.zip")
	if err := ZipTree(treeZip, src, "Wrap/Inner/", ZipOpts{}); err != nil {
		t.Fatal(err)
	}
	tr, err := zip.OpenReader(treeZip)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.File) != 1 || tr.File[0].Name != "Wrap/Inner/a/b.txt" {
		_ = tr.Close()
		t.Fatalf("unexpected ZipTree entries: %+v", tr.File)
	}
	_ = tr.Close()

	// ManyEntriesZip & BombZip
	manyZip := filepath.Join(tmp, "many.zip")
	if err := ManyEntriesZip(manyZip, 50); err != nil {
		t.Fatal(err)
	}
	mr, err := zip.OpenReader(manyZip)
	if err != nil {
		t.Fatal(err)
	}
	if len(mr.File) != 51 {
		_ = mr.Close()
		t.Fatalf("expected 51 entries in ManyEntriesZip, got %d", len(mr.File))
	}
	_ = mr.Close()

	bombPath := filepath.Join(tmp, "bomb.zip")
	if err := BombZip(bombPath, 1<<20); err != nil {
		t.Fatal(err)
	}
	br, err := zip.OpenReader(bombPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(br.File) != 1 || br.File[0].UncompressedSize64 != 1024 {
		_ = br.Close()
		t.Fatalf("BombZip header mismatch: %+v", br.File)
	}
	_ = br.Close()

	// HostileCases
	cases := HostileCases()
	if len(cases) < 20 {
		t.Fatalf("expected >= 20 hostile cases, got %d", len(cases))
	}
	for _, hc := range cases {
		p := filepath.Join(tmp, "hostile-"+hc.Name+".zip")
		if err := WriteZip(p, hc.Entries, ZipOpts{}); err != nil {
			t.Fatalf("WriteZip hostile %q: %v", hc.Name, err)
		}
		if hc.Why == "" {
			t.Fatalf("hostile case %q missing Why", hc.Name)
		}
	}
}

func TestOtherInputs(t *testing.T) {
	tmp := t.TempDir()

	// Bedrock & BedrockFolder
	mcworld := filepath.Join(tmp, "world.mcworld")
	if err := Bedrock(mcworld); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, mcworld, "level.dat", "levelname.txt", "db/CURRENT", "db/MANIFEST-000001", "db/000003.log")
	bf := filepath.Join(tmp, "bedrock-dir")
	if err := BedrockFolder(bf); err != nil {
		t.Fatal(err)
	}
	assertFilesExist(t, bf, "level.dat", "levelname.txt", "db/CURRENT")
	bdat, err := os.ReadFile(filepath.Join(bf, "level.dat"))
	if err != nil || len(bdat) < 8 || binary.LittleEndian.Uint32(bdat[0:4]) != 10 {
		t.Fatalf("bedrock level.dat invalid header: len=%d err=%v", len(bdat), err)
	}

	// Structures
	structs, err := Structures(filepath.Join(tmp, "structs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(structs) != 4 {
		t.Fatalf("expected 4 structure files, got %d", len(structs))
	}
	for kind, p := range structs {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing structure %s at %s: %v", kind, p, err)
		}
	}

	// ResourcePack, Datapack, CurseForgeModpack, ModrinthPack
	rp := filepath.Join(tmp, "rp.zip")
	if err := ResourcePack(rp); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, rp, "pack.mcmeta", "assets/minecraft/textures/block/stone.png")

	dp := filepath.Join(tmp, "dp.zip")
	if err := Datapack(dp); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, dp, "pack.mcmeta", "data/demo/function/hello.mcfunction")

	cf := filepath.Join(tmp, "cf.zip")
	if err := CurseForgeModpack(cf); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, cf, "manifest.json", "overrides/config/x.toml")

	mr := filepath.Join(tmp, "pack.mrpack")
	if err := ModrinthPack(mr); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, mr, "modrinth.index.json")

	// ServerFolder & ServerZip
	sf := filepath.Join(tmp, "srv")
	if err := ServerFolder(sf); err != nil {
		t.Fatal(err)
	}
	assertFilesExist(t, sf, "server.properties", "eula.txt", "server.jar", "start.bat", "world/level.dat")
	sz := filepath.Join(tmp, "srv.zip")
	if err := ServerZip(sz); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, sz, "MyServer/server.properties", "MyServer/world/level.dat", "MyServer/plugins/", "MyServer/mods/")

	// Mdpack
	md := filepath.Join(tmp, "handover.mdpack")
	if err := Mdpack(md); err != nil {
		t.Fatal(err)
	}
	assertZipHas(t, md, "manifest.json", "instance/world/level.dat", "instance/server.properties")
	mdr, err := zip.OpenReader(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mdr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				_ = rc.Close()
				t.Fatal(err)
			}
			_ = rc.Close()
			if m["mode"] != "handover" || m["formatVersion"] != float64(1) {
				t.Fatalf("unexpected mdpack manifest: %+v", m)
			}
		}
	}
	_ = mdr.Close()

	// Rar, SevenZip, SplitZip
	rarPath := filepath.Join(tmp, "a.rar")
	if err := Rar(rarPath); err != nil {
		t.Fatal(err)
	}
	rb, _ := os.ReadFile(rarPath)
	if !bytes.HasPrefix(rb, []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00}) {
		t.Fatalf("bad RAR5 prefix: %x", rb)
	}

	szPath := filepath.Join(tmp, "a.7z")
	if err := SevenZip(szPath); err != nil {
		t.Fatal(err)
	}
	sb, _ := os.ReadFile(szPath)
	if !bytes.HasPrefix(sb, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}) {
		t.Fatalf("bad 7z prefix: %x", sb)
	}

	splitDir := filepath.Join(tmp, "split")
	first, err := SplitZip(splitDir)
	if err != nil {
		t.Fatal(err)
	}
	z01Bytes, err := os.ReadFile(first)
	if err != nil || !bytes.HasPrefix(z01Bytes, []byte{0x50, 0x4B, 0x07, 0x08}) {
		t.Fatalf("bad split zip z01: %x err=%v", z01Bytes, err)
	}
	assertFilesExist(t, splitDir, "map.zip", "map.z01")

	// Damaged
	dmg, err := Damaged(filepath.Join(tmp, "damaged"))
	if err != nil {
		t.Fatal(err)
	}
	expectedCases := []string{
		"only-level-dat-old",
		"truncated",
		"not-gzip",
		"no-data",
		"only-regions",
		"gzip-bomb",
		"deep-nbt",
		"huge-list",
	}
	for _, k := range expectedCases {
		if _, ok := dmg[k]; !ok {
			t.Fatalf("missing damaged case %q", k)
		}
	}
}

func assertMarker(t *testing.T, path, want string) {
	t.Helper()
	got, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker(%s): %v", path, err)
	}
	if got != want {
		t.Fatalf("ReadMarker(%s) = %q, want %q", path, got, want)
	}
}

func assertFilesExist(t *testing.T, base string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(base, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected file %s to exist: %v", p, err)
		}
	}
}

func assertZipHas(t *testing.T, zipPath string, names ...string) {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("OpenReader(%s): %v", zipPath, err)
	}
	defer zr.Close()
	have := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, want := range names {
		if !have[want] {
			t.Fatalf("zip %s missing entry %q", zipPath, want)
		}
	}
}
