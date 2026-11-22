package web

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/worldimport/worldtest"
)

func TestGetPropsIncludesWorldGameRules(t *testing.T) {
	e := newFullEnv(t)
	instDir := filepath.Join(app.ServersDir, "s1")
	propsURL := "/api/instances/" + url.PathEscape("生存服") + "/properties"

	// 初始 newFullEnv 里的 level.dat 是损坏的占位内容：读不到时 gamerules 为空，接口照常返回 pairs
	rec := e.call(t, "GET", propsURL, "", 200)
	var r0 struct {
		Data struct {
			Pairs     []any             `json:"pairs"`
			GameRules map[string]string `json:"gamerules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r0); err != nil {
		t.Fatal(err)
	}
	if len(r0.Data.Pairs) == 0 || len(r0.Data.GameRules) != 0 {
		t.Fatalf("损坏的存档不应返回 gamerules：%+v", r0.Data)
	}

	// 按 server.properties 的 level-name 找自定义世界目录（带子目录），读取 26.2 的 game_rules.dat
	if err := os.WriteFile(filepath.Join(instDir, "server.properties"), []byte(
		"motd=hello\nlevel-name=worlds/my_world\npvp=true\nallow-nether=true\nenable-command-block=false\nspawn-monsters=true\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(instDir, "worlds", "my_world")
	if err := worldtest.Modern(worldDir, worldtest.WorldOpts{
		Level: worldtest.LevelOpts{VersionName: "26.2", DataVersion: 4780},
	}); err != nil {
		t.Fatal(err)
	}
	if err := worldtest.WriteFile(filepath.Join(worldDir, "data", "minecraft", "game_rules.dat"), "", worldtest.Compound{
		{Name: "DataVersion", Value: worldtest.Int(4780)},
		{Name: "data", Value: worldtest.Compound{
			{Name: "minecraft:pvp", Value: worldtest.Byte(0)},
			{Name: "minecraft:allow_entering_nether_using_portals", Value: worldtest.Byte(0)},
			{Name: "minecraft:command_blocks_work", Value: worldtest.Byte(1)},
			{Name: "minecraft:spawn_monsters", Value: worldtest.Byte(0)},
		}},
	}, worldtest.Gzip); err != nil {
		t.Fatal(err)
	}

	rec = e.call(t, "GET", propsURL, "", 200)
	var r1 struct {
		Data struct {
			GameRules map[string]string `json:"gamerules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"pvp":                  "false",
		"allow-nether":         "false",
		"enable-command-block": "true",
		"spawn-monsters":       "false",
	}
	if !reflect.DeepEqual(r1.Data.GameRules, want) {
		t.Fatalf("gamerules = %#v，应为 %#v", r1.Data.GameRules, want)
	}

	// level-name 越界指到服务器目录外时：拒绝跟出去读，回退为空
	if err := os.WriteFile(filepath.Join(instDir, "server.properties"), []byte("level-name=../outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = e.call(t, "GET", propsURL, "", 200)
	var r2 struct {
		Data struct {
			GameRules map[string]string `json:"gamerules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if len(r2.Data.GameRules) != 0 {
		t.Fatalf("越界 level-name 不应返回 gamerules：%#v", r2.Data.GameRules)
	}
}
