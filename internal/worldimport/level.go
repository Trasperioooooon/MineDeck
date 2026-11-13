package worldimport

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"minedeck/internal/worldimport/nbt"
)

// parseLevel 从 level.dat 的根标签里读出导入要用的信息。缺的字段就是缺，不补值；
// 字段在不同版本里的位置不一样的（游戏规则、生成设置 26.1 起搬出了 level.dat），只读读得到的地方。
func parseLevel(root nbt.Compound) (LevelMeta, error) {
	var m LevelMeta
	d, ok := root.Compound("Data")
	if !ok {
		return m, fmt.Errorf("level.dat 里没有 Data 这一节")
	}
	if v, ok := d.Compound("Version"); ok {
		m.HasVersion = true
		if s, ok := v.String("Name"); ok {
			m.VersionName = cleanText(s, 64)
		}
		if n, ok := v.Int("Id"); ok {
			m.VersionID = int32(n)
		}
		m.Snapshot, _ = v.Bool("Snapshot")
		if s, ok := v.String("Series"); ok {
			m.Series = cleanText(s, 32)
		}
	}
	if n, ok := d.Int("DataVersion"); ok {
		m.DataVersion = int32(n)
	}
	if n, ok := d.Int("version"); ok {
		m.LegacyNBT = int32(n)
	}
	if s, ok := d.String("LevelName"); ok {
		m.LevelName = cleanText(s, 80)
	}
	if b, ok := d.Strings("ServerBrands"); ok {
		for _, s := range b {
			if s = cleanText(s, 40); s != "" && len(m.ServerBrands) < 16 {
				m.ServerBrands = append(m.ServerBrands, s)
			}
		}
	}
	m.WasModded, _ = d.Bool("WasModded")
	if n, ok := d.Int("GameType"); ok {
		m.GameType, m.HasGameType = int(n), true
	}
	m.Hardcore, _ = d.Bool("hardcore")
	if n, ok := d.Int("Difficulty"); ok {
		m.Difficulty, m.HasDifficulty = int(n), true
	}
	m.AllowCommands, _ = d.Bool("allowCommands")
	if n, ok := d.Int("WorldGenSettings", "seed"); ok {
		m.Seed = fmt.Sprint(n)
	} else if n, ok := d.Int("RandomSeed"); ok {
		m.Seed = fmt.Sprint(n)
	}

	if p, ok := d.Compound("Player"); ok {
		m.HasPlayerTag = true
		m.PlayerUUID = entityUUID(p)
	}
	if v, ok := d.Get("singleplayer_uuid"); ok {
		m.SingleplayerUUID, _ = nbt.UUID(v)
	} else if v, ok := root.Get("singleplayer_uuid"); ok { // 位置没有核实以前，根上的也认
		m.SingleplayerUUID, _ = nbt.UUID(v)
	}

	if l, ok := d.List("version_history"); ok {
		m.VersionHistory = historyStrings(l)
	} else if l, ok := root.List("version_history"); ok {
		m.VersionHistory = historyStrings(l)
	}

	if s, ok := d.Strings("DataPacks", "Enabled"); ok {
		m.EnabledPacks = capStrings(s, 200)
	}
	if s, ok := d.Strings("DataPacks", "Disabled"); ok {
		m.DisabledPacks = capStrings(s, 200)
	}
	if g, ok := d.Compound("GameRules"); ok {
		m.GameRules = map[string]string{}
		for k, v := range g {
			if len(m.GameRules) >= 200 {
				break
			}
			switch x := v.(type) {
			case string:
				m.GameRules[cleanText(k, 64)] = cleanText(x, 64)
			case int8, int16, int32, int64:
				m.GameRules[cleanText(k, 64)] = fmt.Sprint(x)
			}
		}
	}
	if dims, ok := d.Compound("WorldGenSettings", "dimensions"); ok {
		for k := range dims {
			if len(m.Dimensions) < 64 {
				m.Dimensions = append(m.Dimensions, cleanText(k, 128))
			}
		}
		sort.Strings(m.Dimensions)
	}
	// 加载器留在 level.dat 根上或 Data 里的标记（Forge 1.12–1.16 的 fml / FML，以及别的加载器的同类键）
	for _, k := range []string{"fml", "FML", "forge", "Forge", "neoforge", "fabric", "quilt"} {
		if _, ok := root.Get(k); ok {
			m.ModMarkers = append(m.ModMarkers, k)
		} else if _, ok := d.Get(k); ok {
			m.ModMarkers = append(m.ModMarkers, "Data."+k)
		}
	}
	return m, nil
}

// entityUUID 实体（玩家）的 UUID：1.16 起是 4 个整数的 UUID 数组，更早是 UUIDMost / UUIDLeast。
func entityUUID(c nbt.Compound) string {
	if v, ok := c.Get("UUID"); ok {
		if s, ok := nbt.UUID(v); ok {
			return s
		}
	}
	most, ok1 := c.Int("UUIDMost")
	least, ok2 := c.Int("UUIDLeast")
	if ok1 && ok2 {
		return nbt.UUIDMostLeast(most, least)
	}
	return ""
}

// historyStrings 把 version_history 的每一项写成一行字（结构没有核实以前，字符串原样取，复合里取 Name / name / id）。
func historyStrings(l nbt.List) []string {
	var out []string
	for _, it := range l.Items {
		if len(out) >= 32 {
			break
		}
		switch x := it.(type) {
		case string:
			out = append(out, cleanText(x, 64))
		case nbt.Compound:
			for _, k := range []string{"Name", "name", "version", "id"} {
				if s, ok := x.String(k); ok {
					out = append(out, cleanText(s, 64))
					break
				}
			}
		}
	}
	return out
}

func capStrings(s []string, n int) []string {
	out := make([]string, 0, min(len(s), n))
	for _, v := range s {
		if len(out) >= n {
			break
		}
		out = append(out, cleanText(v, 128))
	}
	return out
}

// cleanText 别人给的文字（地图名、版本名、说明书）拿来显示之前：去掉控制字符和 Minecraft 的 § 格式码，限长。
func cleanText(s string, max int) string {
	var b strings.Builder
	skip := false
	n := 0
	for _, r := range s {
		if skip {
			skip = false
			continue
		}
		if r == '§' {
			skip = true
			continue
		}
		if r == '\n' || r == '\t' {
			r = ' '
		}
		if unicode.IsControl(r) || r == '‮' || r == '‭' || r == '⁦' || r == '⁧' || r == '⁨' || r == '⁩' {
			continue
		}
		if n >= max {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
