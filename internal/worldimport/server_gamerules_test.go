package worldimport

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"minedeck/internal/worldimport/worldtest"
)

func TestReadServerGameRules(t *testing.T) {
	t.Run("1.21.9-1.21.10 旧式 GameRules 字符串值", func(t *testing.T) {
		dir := t.TempDir()
		if err := worldtest.WriteLevelDat(filepath.Join(dir, "level.dat"), worldtest.LevelOpts{
			VersionName: "1.21.10",
			DataVersion: 4556,
			GameRules: map[string]string{
				"pvp":                             "false",
				"allowEnteringNetherUsingPortals": "false",
				"enableCommandBlocks":             "true",
				"spawnMonsters":                   "false",
				"keepInventory":                   "true",
			},
		}); err != nil {
			t.Fatal(err)
		}
		got := ReadServerGameRules(dir)
		want := map[string]string{
			"pvp":                  "false",
			"allow-nether":         "false",
			"enable-command-block": "true",
			"spawn-monsters":       "false",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadServerGameRules = %#v，应为 %#v", got, want)
		}
	})

	t.Run("1.21.11 level.dat 内 game_rules 命名空间与数值", func(t *testing.T) {
		dir := t.TempDir()
		if err := worldtest.WriteLevelDat(filepath.Join(dir, "level.dat"), worldtest.LevelOpts{
			VersionName: "1.21.11",
			DataVersion: 4671,
			DataExtra: worldtest.Compound{
				{Name: "game_rules", Value: worldtest.Compound{
					{Name: "minecraft:pvp", Value: worldtest.Byte(0)},
					{Name: "minecraft:allow_entering_nether_using_portals", Value: worldtest.Byte(1)},
					{Name: "minecraft:command_blocks_work", Value: worldtest.Byte(1)},
					{Name: "minecraft:spawn_monsters", Value: worldtest.Byte(0)},
				}},
			},
		}); err != nil {
			t.Fatal(err)
		}
		got := ReadServerGameRules(dir)
		want := map[string]string{
			"pvp":                  "false",
			"allow-nether":         "true",
			"enable-command-block": "true",
			"spawn-monsters":       "false",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadServerGameRules = %#v，应为 %#v", got, want)
		}
	})

	t.Run("26.1+ 原版 data/minecraft/game_rules.dat", func(t *testing.T) {
		dir := t.TempDir()
		if err := worldtest.Modern(dir, worldtest.WorldOpts{
			Level: worldtest.LevelOpts{VersionName: "26.2", DataVersion: 4780},
		}); err != nil {
			t.Fatal(err)
		}
		if err := worldtest.WriteFile(filepath.Join(dir, "data", "minecraft", "game_rules.dat"), "", worldtest.Compound{
			{Name: "DataVersion", Value: worldtest.Int(4780)},
			{Name: "data", Value: worldtest.Compound{
				{Name: "minecraft:pvp", Value: worldtest.Byte(1)},
				{Name: "minecraft:allow_entering_nether_using_portals", Value: worldtest.Byte(0)},
				{Name: "minecraft:command_blocks_work", Value: worldtest.Byte(1)},
				{Name: "minecraft:spawn_monsters", Value: worldtest.Byte(1)},
			}},
		}, worldtest.Gzip); err != nil {
			t.Fatal(err)
		}
		got := ReadServerGameRules(dir)
		want := map[string]string{
			"pvp":                  "true",
			"allow-nether":         "false",
			"enable-command-block": "true",
			"spawn-monsters":       "true",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadServerGameRules = %#v，应为 %#v", got, want)
		}
	})

	t.Run("26.1+ Paper dimensions/minecraft/overworld/data/minecraft/game_rules.dat", func(t *testing.T) {
		dir := t.TempDir()
		if err := worldtest.ModernPaper(dir, worldtest.WorldOpts{
			Level:     worldtest.LevelOpts{VersionName: "26.3", DataVersion: 4800},
			Overworld: true,
		}); err != nil {
			t.Fatal(err)
		}
		grPath := filepath.Join(dir, "dimensions", "minecraft", "overworld", "data", "minecraft", "game_rules.dat")
		if err := worldtest.WriteFile(grPath, "", worldtest.Compound{
			{Name: "DataVersion", Value: worldtest.Int(4800)},
			{Name: "data", Value: worldtest.Compound{
				{Name: "minecraft:pvp", Value: worldtest.Byte(0)},
				{Name: "minecraft:allow_entering_nether_using_portals", Value: worldtest.Byte(1)},
				{Name: "minecraft:command_blocks_work", Value: worldtest.Byte(0)},
				{Name: "minecraft:spawn_monsters", Value: worldtest.Byte(1)},
			}},
		}, worldtest.Gzip); err != nil {
			t.Fatal(err)
		}
		got := ReadServerGameRules(dir)
		want := map[string]string{
			"pvp":                  "false",
			"allow-nether":         "true",
			"enable-command-block": "false",
			"spawn-monsters":       "true",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadServerGameRules = %#v，应为 %#v", got, want)
		}
	})

	t.Run("世界尚未生成或存档损坏时返回空", func(t *testing.T) {
		if got := ReadServerGameRules(filepath.Join(t.TempDir(), "not-exist")); len(got) != 0 {
			t.Fatalf("不存在的目录应返回空：%#v", got)
		}
		broken := t.TempDir()
		if err := os.WriteFile(filepath.Join(broken, "level.dat"), []byte("not-nbt"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ReadServerGameRules(broken); len(got) != 0 {
			t.Fatalf("损坏的 level.dat 应返回空：%#v", got)
		}
	})
}
