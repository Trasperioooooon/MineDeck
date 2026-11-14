package worldtest

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Bedrock writes a synthetic Minecraft Bedrock .mcworld archive to path.
func Bedrock(path string) error {
	entries, err := bedrockEntries()
	if err != nil {
		return err
	}
	return WriteZip(path, entries, ZipOpts{})
}

// BedrockFolder writes a synthetic Minecraft Bedrock world directory to dir.
func BedrockFolder(dir string) error {
	entries, err := bedrockEntries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := writeFileBytes(filepath.Join(dir, filepath.FromSlash(e.Name)), e.Body); err != nil {
			return err
		}
	}
	return nil
}

func bedrockEntries() ([]ZipEntry, error) {
	// Little-endian NBT compound: {StorageVersion: Int(10), LevelName: String("BedrockWorld")}
	var nbt bytes.Buffer
	nbt.WriteByte(TagCompound)
	_ = binary.Write(&nbt, binary.LittleEndian, uint16(0)) // root name ""
	// StorageVersion
	nbt.WriteByte(TagInt)
	_ = binary.Write(&nbt, binary.LittleEndian, uint16(len("StorageVersion")))
	nbt.WriteString("StorageVersion")
	_ = binary.Write(&nbt, binary.LittleEndian, int32(10))
	// LevelName
	nbt.WriteByte(TagString)
	_ = binary.Write(&nbt, binary.LittleEndian, uint16(len("LevelName")))
	nbt.WriteString("LevelName")
	_ = binary.Write(&nbt, binary.LittleEndian, uint16(len("BedrockWorld")))
	nbt.WriteString("BedrockWorld")
	// TagEnd
	nbt.WriteByte(TagEnd)

	var levelDat bytes.Buffer
	_ = binary.Write(&levelDat, binary.LittleEndian, int32(10))
	_ = binary.Write(&levelDat, binary.LittleEndian, int32(nbt.Len()))
	levelDat.Write(nbt.Bytes())

	return []ZipEntry{
		{Name: "level.dat", Body: levelDat.Bytes()},
		{Name: "levelname.txt", Body: []byte("BedrockWorld")},
		{Name: "db/CURRENT", Body: []byte("MANIFEST-000001\n")},
		{Name: "db/MANIFEST-000001", Body: []byte{0x01, 0x02, 0x03, 0x04}},
		{Name: "db/000003.log", Body: []byte("bedrock-db-log")},
	}, nil
}

// Structures writes Sponge .schem, MCEdit .schematic, Litematica .litematic,
// and vanilla structure.nbt files into dir, returning kind -> path.
func Structures(dir string) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	schemPath := filepath.Join(dir, "x.schem")
	if err := WriteFile(schemPath, "Schematic", Compound{
		{Name: "Version", Value: Int(2)},
		{Name: "DataVersion", Value: Int(3700)},
		{Name: "Width", Value: Short(1)},
		{Name: "Height", Value: Short(1)},
		{Name: "Length", Value: Short(1)},
		{Name: "Palette", Value: Compound{
			{Name: "minecraft:stone", Value: Int(0)},
		}},
		{Name: "BlockData", Value: ByteArray{0}},
	}, Gzip); err != nil {
		return nil, err
	}

	schematicPath := filepath.Join(dir, "x.schematic")
	if err := WriteFile(schematicPath, "Schematic", Compound{
		{Name: "Width", Value: Short(1)},
		{Name: "Height", Value: Short(1)},
		{Name: "Length", Value: Short(1)},
		{Name: "Materials", Value: String("Alpha")},
		{Name: "Blocks", Value: ByteArray{1}},
		{Name: "Data", Value: ByteArray{0}},
	}, Gzip); err != nil {
		return nil, err
	}

	litematicPath := filepath.Join(dir, "x.litematic")
	if err := WriteFile(litematicPath, "", Compound{
		{Name: "Version", Value: Int(6)},
		{Name: "MinecraftDataVersion", Value: Int(3700)},
		{Name: "Metadata", Value: Compound{
			{Name: "Name", Value: String("Demo")},
			{Name: "Author", Value: String("MineDeck")},
		}},
		{Name: "Regions", Value: Compound{
			{Name: "Main", Value: Compound{
				{Name: "Size", Value: Compound{
					{Name: "x", Value: Int(1)},
					{Name: "y", Value: Int(1)},
					{Name: "z", Value: Int(1)},
				}},
			}},
		}},
	}, Gzip); err != nil {
		return nil, err
	}

	structurePath := filepath.Join(dir, "structure.nbt")
	if err := WriteFile(structurePath, "", Compound{
		{Name: "DataVersion", Value: Int(3700)},
		{Name: "size", Value: List{Elem: TagInt, Items: []any{Int(1), Int(1), Int(1)}}},
		{Name: "palette", Value: List{Elem: TagCompound, Items: []any{
			Compound{{Name: "Name", Value: String("minecraft:stone")}},
		}}},
		{Name: "blocks", Value: List{Elem: TagCompound, Items: []any{
			Compound{
				{Name: "pos", Value: List{Elem: TagInt, Items: []any{Int(0), Int(0), Int(0)}}},
				{Name: "state", Value: Int(0)},
			},
		}}},
		{Name: "entities", Value: List{Elem: TagEnd}},
	}, Gzip); err != nil {
		return nil, err
	}

	return map[string]string{
		"schem":     schemPath,
		"schematic": schematicPath,
		"litematic": litematicPath,
		"structure": structurePath,
	}, nil
}

// ResourcePack writes a tiny valid Minecraft resource pack zip to path.
func ResourcePack(path string) error {
	pngBytes, err := encode1x1PNG()
	if err != nil {
		return err
	}
	return WriteZip(path, []ZipEntry{
		{Name: "pack.mcmeta", Body: []byte(`{"pack":{"pack_format":34,"description":"x"}}`)},
		{Name: "assets/minecraft/textures/block/stone.png", Body: pngBytes},
	}, ZipOpts{})
}

// Datapack writes a tiny valid Minecraft datapack zip to path.
func Datapack(path string) error {
	return WriteZip(path, []ZipEntry{
		{Name: "pack.mcmeta", Body: []byte(`{"pack":{"pack_format":48,"description":"x"}}`)},
		{Name: "data/demo/function/hello.mcfunction", Body: []byte("say hello\n")},
	}, ZipOpts{})
}

// CurseForgeModpack writes a minimal CurseForge modpack zip to path.
func CurseForgeModpack(path string) error {
	manifest := `{"manifestType":"minecraftModpack","manifestVersion":1,"name":"demo","version":"1.0.0","minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.2.0","primary":true}]},"files":[]}`
	return WriteZip(path, []ZipEntry{
		{Name: "manifest.json", Body: []byte(manifest)},
		{Name: "overrides/config/x.toml", Body: []byte("enabled = true\n")},
	}, ZipOpts{})
}

// ModrinthPack writes a minimal Modrinth .mrpack zip to path.
func ModrinthPack(path string) error {
	idx := `{"formatVersion":1,"game":"minecraft","versionId":"1.0.0","name":"demo","dependencies":{"minecraft":"1.20.1","fabric-loader":"0.15.0"},"files":[]}`
	return WriteZip(path, []ZipEntry{
		{Name: "modrinth.index.json", Body: []byte(idx)},
	}, ZipOpts{})
}

// ServerFolder writes a complete Minecraft server folder with a Legacy world inside world/.
func ServerFolder(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o755); err != nil {
		return err
	}
	if err := writeFileBytes(filepath.Join(dir, "server.properties"), []byte("level-name=world\nserver-port=25565\n")); err != nil {
		return err
	}
	if err := writeFileBytes(filepath.Join(dir, "eula.txt"), []byte("eula=true\n")); err != nil {
		return err
	}
	if err := writeFileBytes(filepath.Join(dir, "server.jar"), []byte("PK\x03\x04fake-server-jar")); err != nil {
		return err
	}
	if err := writeFileBytes(filepath.Join(dir, "start.bat"), []byte("@echo off\r\njava -jar server.jar nogui\r\n")); err != nil {
		return err
	}
	return Legacy(filepath.Join(dir, "world"), WorldOpts{
		Level: LevelOpts{
			VersionName: "1.20.4",
			DataVersion: 3700,
			LevelName:   "world",
			GameType:    0,
			Difficulty:  2,
		},
		Overworld: true,
		Nether:    true,
		End:       true,
	})
}

// ServerZip writes a ServerFolder tree zipped under the "MyServer/" prefix.
func ServerZip(path string) error {
	tmp, err := os.MkdirTemp("", "worldtest-srv-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := ServerFolder(tmp); err != nil {
		return err
	}
	if err := ZipTree(path, tmp, "MyServer/", ZipOpts{}); err != nil {
		return err
	}

	// Ensure empty plugins/ and mods/ directory entries are also present in the archive.
	return appendDirEntries(path, []string{"MyServer/plugins/", "MyServer/mods/"})
}

func appendDirEntries(zipPath string, dirs []string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	var entries []ZipEntry
	seen := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			_ = zr.Close()
			return err
		}
		var body []byte
		if !f.FileInfo().IsDir() {
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				_ = rc.Close()
				_ = zr.Close()
				return err
			}
			body = buf.Bytes()
		}
		_ = rc.Close()
		seen[f.Name] = true
		entries = append(entries, ZipEntry{
			Name:   f.Name,
			Body:   body,
			Method: f.Method,
			Mode:   f.Mode(),
			Dir:    f.FileInfo().IsDir(),
		})
	}
	_ = zr.Close()

	for _, d := range dirs {
		if !seen[d] {
			entries = append(entries, ZipEntry{Name: d, Dir: true, Method: zip.Store})
		}
	}
	return WriteZip(zipPath, entries, ZipOpts{})
}

// Mdpack writes a synthetic MineDeck .mdpack handover archive to path.
func Mdpack(path string) error {
	manifest := map[string]any{
		"formatVersion":   1,
		"mode":            "handover",
		"minedeckVersion": "1.0.0",
		"exportedAt":      time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"instance": map[string]any{
			"name":         "x",
			"core":         "paper",
			"mc":           "1.20.4",
			"build":        "496",
			"javaMajor":    21,
			"xmxMb":        2048,
			"xmsMb":        1024,
			"launchTarget": "jar:server.jar",
		},
		"core": map[string]any{
			"core":  "paper",
			"mc":    "1.20.4",
			"build": "496",
			"kind":  "game",
		},
		"mods":     []any{},
		"plugins":  []any{},
		"counts":   map[string]any{"files": 2, "bytes": 100},
		"excluded": []string{},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	levelBytes, err := gzipCompoundBytes(LevelDat(LevelOpts{
		VersionName: "1.20.4",
		DataVersion: 3700,
		LevelName:   "world",
	}))
	if err != nil {
		return err
	}
	return WriteZip(path, []ZipEntry{
		{Name: "manifest.json", Body: manifestBytes},
		{Name: "instance/world/level.dat", Body: levelBytes},
		{Name: "instance/server.properties", Body: []byte("level-name=world\nserver-port=25565\n")},
	}, ZipOpts{})
}

// Rar writes a file starting with the RAR5 signature followed by junk bytes.
func Rar(path string) error {
	data := []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00, 'j', 'u', 'n', 'k'}
	return writeFileBytes(path, data)
}

// SevenZip writes a file starting with the 7z signature followed by junk bytes.
func SevenZip(path string) error {
	data := []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C, 0x00, 0x04, 'j', 'u', 'n', 'k'}
	return writeFileBytes(path, data)
}

// SplitZip writes a split zip pair ("map.zip" + "map.z01" starting with PK\x07\x08) into dir,
// returning the path to the first volume ("map.z01").
func SplitZip(dir string) (first string, err error) {
	z01Path := filepath.Join(dir, "map.z01")
	z01Data := []byte{0x50, 0x4B, 0x07, 0x08, 0x50, 0x4B, 0x03, 0x04, 's', 'p', 'l', 'i', 't'}
	if err := writeFileBytes(z01Path, z01Data); err != nil {
		return "", err
	}

	zipPath := filepath.Join(dir, "map.zip")
	// Minimal End of Central Directory indicating disk 1 of a multi-disk archive.
	eocd := []byte{
		0x50, 0x4B, 0x05, 0x06, // EOCD signature
		0x01, 0x00, // number of this disk (1)
		0x00, 0x00, // disk where central directory starts (0)
		0x00, 0x00, // entries on this disk
		0x00, 0x00, // total entries
		0x00, 0x00, 0x00, 0x00, // size of central directory
		0x00, 0x00, 0x00, 0x00, // offset of central directory
		0x00, 0x00, // comment length
	}
	if err := writeFileBytes(zipPath, eocd); err != nil {
		return "", err
	}
	return z01Path, nil
}

// Damaged creates damaged Java world directories under parent and returns case -> dir.
func Damaged(parent string) (map[string]string, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	out := make(map[string]string, 8)
	validLevel := LevelOpts{
		VersionName: "1.20.4",
		DataVersion: 3700,
		LevelName:   "DamagedTest",
	}

	// 1. only-level-dat-old
	d := filepath.Join(parent, "only-level-dat-old")
	if err := WriteLevelDat(filepath.Join(d, "level.dat_old"), validLevel); err != nil {
		return nil, err
	}
	if err := writeDimRegions(d, "minecraft:overworld", "T", false); err != nil {
		return nil, err
	}
	out["only-level-dat-old"] = d

	// 2. truncated
	d = filepath.Join(parent, "truncated")
	validGzip, err := gzipCompoundBytes(LevelDat(validLevel))
	if err != nil {
		return nil, err
	}
	if err := writeFileBytes(filepath.Join(d, "level.dat"), validGzip[:len(validGzip)/2]); err != nil {
		return nil, err
	}
	if err := writeDimRegions(d, "minecraft:overworld", "T", false); err != nil {
		return nil, err
	}
	out["truncated"] = d

	// 3. not-gzip
	d = filepath.Join(parent, "not-gzip")
	if err := writeFileBytes(filepath.Join(d, "level.dat"), []byte{0x13, 0x37, 0x99, 0xAB, 0xCD, 0xEF, 0x42, 0x00, 0x7E, 0x11}); err != nil {
		return nil, err
	}
	if err := writeDimRegions(d, "minecraft:overworld", "T", false); err != nil {
		return nil, err
	}
	out["not-gzip"] = d

	// 4. no-data
	d = filepath.Join(parent, "no-data")
	if err := WriteFile(filepath.Join(d, "level.dat"), "", Compound{
		{Name: "NotData", Value: Int(1)},
	}, Gzip); err != nil {
		return nil, err
	}
	if err := writeDimRegions(d, "minecraft:overworld", "T", false); err != nil {
		return nil, err
	}
	out["no-data"] = d

	// 5. only-regions
	d = filepath.Join(parent, "only-regions")
	if err := writeDimRegions(d, "minecraft:overworld", "T", false); err != nil {
		return nil, err
	}
	out["only-regions"] = d

	// 6. gzip-bomb (streamed 64 MiB zeros-prefixed payload)
	d = filepath.Join(parent, "gzip-bomb")
	if err := writeGzipBomb(filepath.Join(d, "level.dat"), 64<<20); err != nil {
		return nil, err
	}
	out["gzip-bomb"] = d

	// 7. deep-nbt (600 compounds deep)
	d = filepath.Join(parent, "deep-nbt")
	deep := Compound{{Name: "leaf", Value: Int(1)}}
	for range 600 {
		deep = Compound{{Name: "n", Value: deep}}
	}
	if err := WriteFile(filepath.Join(d, "level.dat"), "", Compound{{Name: "Data", Value: deep}}, Gzip); err != nil {
		return nil, err
	}
	out["deep-nbt"] = d

	// 8. huge-list (List header declaring 2^31-1 Ints, truncated immediately)
	d = filepath.Join(parent, "huge-list")
	var raw bytes.Buffer
	raw.Write([]byte{TagCompound, 0x00, 0x00})                     // root ""
	raw.Write([]byte{TagCompound, 0x00, 0x04, 'D', 'a', 't', 'a'}) // Data
	raw.Write([]byte{TagList, 0x00, 0x03, 'B', 'i', 'g'})          // Big
	raw.WriteByte(TagInt)                                          // Elem = TagInt
	_ = binary.Write(&raw, binary.BigEndian, int32(0x7FFFFFFF))    // 2^31 - 1
	if err := writeGzipBytes(filepath.Join(d, "level.dat"), raw.Bytes()); err != nil {
		return nil, err
	}
	out["huge-list"] = d

	return out, nil
}

func gzipCompoundBytes(c Compound) ([]byte, error) {
	raw, err := Marshal("", c)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeGzipBytes(path string, raw []byte) error {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		_ = gw.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	return writeFileBytes(path, buf.Bytes())
}

func writeGzipBomb(path string, totalBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		_ = f.Close()
		return err
	}
	var zeros [32 * 1024]byte
	rem := totalBytes
	for rem > 0 {
		chunk := int64(len(zeros))
		if rem < chunk {
			chunk = rem
		}
		if _, err := gw.Write(zeros[:chunk]); err != nil {
			_ = gw.Close()
			_ = f.Close()
			return err
		}
		rem -= chunk
	}
	if err := gw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
