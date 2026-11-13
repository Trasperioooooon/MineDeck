package worldtest

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// WorldOpts configures synthetic Minecraft Java world directories.
type WorldOpts struct {
	Level        LevelOpts
	Tag          string            // marker text embedded into every generated region file; default "T"
	Overworld    bool              // create overworld region/r.0.0.mca
	Nether       bool              // create nether region file
	End          bool              // create end region file
	Entities     bool              // also create entities/r.0.0.mca and poi/r.0.0.mca next to each created region dir
	Players      []string          // player UUIDs -> player .dat + advancements json + stats json
	Datapacks    []string          // names -> datapacks/<name>.zip
	ResourcesZip bool              // resources.zip in the world root
	Icon         bool              // icon.png (valid 1x1 PNG)
	SessionLock  bool              // session.lock (E2 98 83)
	UIDDat       bool              // uid.dat (16 deterministic bytes) — Bukkit worlds
	CustomDims   []string          // "ns:path" -> custom dimension region file
	ExtraFiles   map[string][]byte // extra files, slash-separated path relative to world root
}

const regionHeaderSize = 8192

var deterministicUID = []byte{
	0x4d, 0x69, 0x6e, 0x65, 0x44, 0x65, 0x63, 0x6b,
	0x55, 0x49, 0x44, 0x00, 0x01, 0x02, 0x03, 0x04,
}

var sessionLockBytes = []byte{0xE2, 0x98, 0x83}

// Legacy writes a pre-26.1 vanilla Java Edition world to dir.
func Legacy(dir string, o WorldOpts) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := WriteLevelDat(filepath.Join(dir, "level.dat"), o.Level); err != nil {
		return err
	}
	tag := effectiveTag(o.Tag)
	if o.Overworld {
		if err := writeDimRegions(dir, "minecraft:overworld", tag, o.Entities); err != nil {
			return err
		}
	}
	if o.Nether {
		if err := writeDimRegions(filepath.Join(dir, "DIM-1"), "minecraft:the_nether", tag, o.Entities); err != nil {
			return err
		}
	}
	if o.End {
		if err := writeDimRegions(filepath.Join(dir, "DIM1"), "minecraft:the_end", tag, o.Entities); err != nil {
			return err
		}
	}
	if err := writeCustomDims(dir, o.CustomDims, tag, o.Entities, false); err != nil {
		return err
	}
	if err := writePlayers(dir, o.Players, false); err != nil {
		return err
	}
	if err := writeSmallDat(filepath.Join(dir, "data", "raids.dat"), o.Level.DataVersion); err != nil {
		return err
	}
	return writeWorldExtras(dir, o)
}

// LegacyBukkit writes a pre-26.1 CraftBukkit/Spigot/Paper world group under parent:
// <name>/, and optionally <name>_nether/ and <name>_the_end/.
func LegacyBukkit(parent, name string, o WorldOpts) error {
	if name == "" {
		return errors.New("worldtest: empty world name for LegacyBukkit")
	}
	mainDir := filepath.Join(parent, name)
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		return err
	}
	if err := WriteLevelDat(filepath.Join(mainDir, "level.dat"), o.Level); err != nil {
		return err
	}
	tag := effectiveTag(o.Tag)
	if o.Overworld {
		if err := writeDimRegions(mainDir, "minecraft:overworld", tag, o.Entities); err != nil {
			return err
		}
	}
	if err := writeCustomDims(mainDir, o.CustomDims, tag, o.Entities, false); err != nil {
		return err
	}
	if err := writePlayers(mainDir, o.Players, false); err != nil {
		return err
	}
	if err := writeSmallDat(filepath.Join(mainDir, "data", "raids.dat"), o.Level.DataVersion); err != nil {
		return err
	}
	if err := writeWorldExtras(mainDir, o); err != nil {
		return err
	}

	baseLevelName := o.Level.LevelName
	if baseLevelName == "" {
		baseLevelName = name
	}

	if o.Nether {
		netherDir := filepath.Join(parent, name+"_nether")
		if err := os.MkdirAll(netherDir, 0o755); err != nil {
			return err
		}
		nl := o.Level
		nl.LevelName = baseLevelName + "_nether"
		if err := WriteLevelDat(filepath.Join(netherDir, "level.dat"), nl); err != nil {
			return err
		}
		if o.UIDDat {
			if err := writeFileBytes(filepath.Join(netherDir, "uid.dat"), deterministicUID); err != nil {
				return err
			}
		}
		if err := writeDimRegions(filepath.Join(netherDir, "DIM-1"), "minecraft:the_nether", tag, o.Entities); err != nil {
			return err
		}
	}

	if o.End {
		endDir := filepath.Join(parent, name+"_the_end")
		if err := os.MkdirAll(endDir, 0o755); err != nil {
			return err
		}
		el := o.Level
		el.LevelName = baseLevelName + "_the_end"
		if err := WriteLevelDat(filepath.Join(endDir, "level.dat"), el); err != nil {
			return err
		}
		if o.UIDDat {
			if err := writeFileBytes(filepath.Join(endDir, "uid.dat"), deterministicUID); err != nil {
				return err
			}
		}
		if err := writeDimRegions(filepath.Join(endDir, "DIM1"), "minecraft:the_end", tag, o.Entities); err != nil {
			return err
		}
	}

	return nil
}

// Modern writes a 26.1+ vanilla Java Edition world to dir.
func Modern(dir string, o WorldOpts) error {
	return writeModern(dir, o, false)
}

// ModernPaper writes a 26.1+ Paper Java Edition world to dir.
func ModernPaper(dir string, o WorldOpts) error {
	return writeModern(dir, o, true)
}

func writeModern(dir string, o WorldOpts, paper bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := WriteLevelDat(filepath.Join(dir, "level.dat"), o.Level); err != nil {
		return err
	}
	tag := effectiveTag(o.Tag)
	if o.Overworld {
		dimDir := filepath.Join(dir, "dimensions", "minecraft", "overworld")
		if err := writeDimRegions(dimDir, "minecraft:overworld", tag, o.Entities); err != nil {
			return err
		}
		if paper {
			if err := writePaperWorldYML(dimDir); err != nil {
				return err
			}
		}
	}
	if o.Nether {
		dimDir := filepath.Join(dir, "dimensions", "minecraft", "the_nether")
		if err := writeDimRegions(dimDir, "minecraft:the_nether", tag, o.Entities); err != nil {
			return err
		}
		if paper {
			if err := writePaperWorldYML(dimDir); err != nil {
				return err
			}
		}
	}
	if o.End {
		dimDir := filepath.Join(dir, "dimensions", "minecraft", "the_end")
		if err := writeDimRegions(dimDir, "minecraft:the_end", tag, o.Entities); err != nil {
			return err
		}
		if paper {
			if err := writePaperWorldYML(dimDir); err != nil {
				return err
			}
		}
	}
	if err := writeCustomDims(dir, o.CustomDims, tag, o.Entities, paper); err != nil {
		return err
	}
	if err := writePlayers(dir, o.Players, true); err != nil {
		return err
	}

	sharedBase := filepath.Join(dir, "data", "minecraft")
	if paper {
		sharedBase = filepath.Join(dir, "dimensions", "minecraft", "overworld", "data", "minecraft")
	}
	for _, name := range []string{
		"game_rules.dat",
		"scheduled_events.dat",
		"wandering_trader.dat",
		"weather.dat",
		"world_gen_settings.dat",
	} {
		if err := writeSmallDat(filepath.Join(sharedBase, name), o.Level.DataVersion); err != nil {
			return err
		}
	}
	if paper {
		if err := writeSmallDat(filepath.Join(dir, "data", "paper", "level_overrides.dat"), o.Level.DataVersion); err != nil {
			return err
		}
	}

	return writeWorldExtras(dir, o)
}

// ReadMarker reads the MARK:... marker text stored after the 8192-byte region header.
func ReadMarker(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) < regionHeaderSize {
		return "", fmt.Errorf("worldtest: region file %s shorter than %d bytes (%d)", path, regionHeaderSize, len(b))
	}
	marker := string(b[regionHeaderSize:])
	if !strings.HasPrefix(marker, "MARK:") {
		return "", fmt.Errorf("worldtest: region file %s missing MARK: prefix", path)
	}
	return marker, nil
}

func effectiveTag(tag string) string {
	if tag == "" {
		return "T"
	}
	return tag
}

func writeDimRegions(dimDir, dimID, tag string, entities bool) error {
	if err := writeMarkerFile(filepath.Join(dimDir, "region", "r.0.0.mca"), fmt.Sprintf("MARK:%s:%s", dimID, tag)); err != nil {
		return err
	}
	if entities {
		if err := writeMarkerFile(filepath.Join(dimDir, "entities", "r.0.0.mca"), fmt.Sprintf("MARK:%s:%s:entities", dimID, tag)); err != nil {
			return err
		}
		if err := writeMarkerFile(filepath.Join(dimDir, "poi", "r.0.0.mca"), fmt.Sprintf("MARK:%s:%s:poi", dimID, tag)); err != nil {
			return err
		}
	}
	return nil
}

func writeMarkerFile(path, marker string) error {
	buf := make([]byte, regionHeaderSize+len(marker))
	copy(buf[regionHeaderSize:], marker)
	return writeFileBytes(path, buf)
}

func writeCustomDims(worldDir string, dims []string, tag string, entities, paper bool) error {
	for _, cd := range dims {
		ns, dimPath, ok := strings.Cut(cd, ":")
		if !ok || ns == "" || dimPath == "" {
			return fmt.Errorf("worldtest: invalid custom dimension %q (expected ns:path)", cd)
		}
		dimDir := filepath.Join(worldDir, "dimensions", ns, filepath.FromSlash(dimPath))
		if err := writeDimRegions(dimDir, cd, tag, entities); err != nil {
			return err
		}
		if paper {
			if err := writePaperWorldYML(dimDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func writePaperWorldYML(dimDir string) error {
	return writeFileBytes(filepath.Join(dimDir, "paper-world.yml"), []byte("# paper world config\n"))
}

func writePlayers(worldDir string, uuids []string, modern bool) error {
	var dataDir, advDir, statsDir string
	if modern {
		dataDir = filepath.Join(worldDir, "players", "data")
		advDir = filepath.Join(worldDir, "players", "advancements")
		statsDir = filepath.Join(worldDir, "players", "stats")
	} else {
		dataDir = filepath.Join(worldDir, "playerdata")
		advDir = filepath.Join(worldDir, "advancements")
		statsDir = filepath.Join(worldDir, "stats")
	}
	for _, u := range uuids {
		uuidArr, err := UUIDInts(u)
		if err != nil {
			return err
		}
		playerNBT := Compound{
			{Name: "UUID", Value: uuidArr},
			{Name: "XpLevel", Value: Int(30)},
			{Name: "Pos", Value: List{Elem: TagDouble, Items: []any{Double(0.5), Double(64.0), Double(0.5)}}},
		}
		if err := WriteFile(filepath.Join(dataDir, u+".dat"), "", playerNBT, Gzip); err != nil {
			return err
		}
		if err := writeFileBytes(filepath.Join(advDir, u+".json"), []byte(`{"DataVersion":3700}`)); err != nil {
			return err
		}
		if err := writeFileBytes(filepath.Join(statsDir, u+".json"), []byte(`{"stats":{},"DataVersion":3700}`)); err != nil {
			return err
		}
	}
	return nil
}

func writeSmallDat(path string, dataVersion int32) error {
	dv := dataVersion
	if dv == 0 {
		dv = 3700
	}
	return WriteFile(path, "", Compound{
		{Name: "DataVersion", Value: Int(dv)},
		{Name: "data", Value: Compound{}},
	}, Gzip)
}

func writeWorldExtras(dir string, o WorldOpts) error {
	for _, dp := range o.Datapacks {
		if err := Datapack(filepath.Join(dir, "datapacks", dp+".zip")); err != nil {
			return err
		}
	}
	if o.ResourcesZip {
		if err := ResourcePack(filepath.Join(dir, "resources.zip")); err != nil {
			return err
		}
	}
	if o.Icon {
		pngBytes, err := encode1x1PNG()
		if err != nil {
			return err
		}
		if err := writeFileBytes(filepath.Join(dir, "icon.png"), pngBytes); err != nil {
			return err
		}
	}
	if o.SessionLock {
		if err := writeFileBytes(filepath.Join(dir, "session.lock"), sessionLockBytes); err != nil {
			return err
		}
	}
	if o.UIDDat {
		if err := writeFileBytes(filepath.Join(dir, "uid.dat"), deterministicUID); err != nil {
			return err
		}
	}
	for rel, content := range o.ExtraFiles {
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := writeFileBytes(dst, content); err != nil {
			return err
		}
	}
	return nil
}

func encode1x1PNG() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0x3b, G: 0x85, B: 0x26, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
