package web

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/inst"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/worldimport/worldtest"
)

func TestGetPropsIncludesWorldGameRules(t *testing.T) {
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}

	writeInst := func(sub, name, mc string) string {
		d := filepath.Join(app.ServersDir, sub)
		if err := os.MkdirAll(filepath.Join(d, "world"), 0o755); err != nil {
			t.Fatal(err)
		}
		s, _ := json.Marshal(inst.Settings{
			Name: name, Core: "vanilla", MC: mc, JavaMajor: 21,
			JavaPath: filepath.Join(app.Base, "no-java.exe"),
			XmxMB:    2048, XmsMB: 1024, LaunchTarget: "jar:server.jar",
		})
		if err := os.WriteFile(filepath.Join(d, "instance.json"), s, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "server.properties"), []byte("motd=hello\nserver-port=25565\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "world", "level.dat"), []byte("broken"), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}
	oldDir := writeInst("s-old", "老版服", "1.21.8")
	instDir := writeInst("s-new", "新版服", "26.2")

	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	nd := &noDial{}
	aiSvc := ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai"), Keys: app.Secrets(), HTTP: nd.client(), Mirrors: dl.GHMirrors})
	reg := catalog.New(catalog.Env{Mgr: mgr, AI: aiSvc, Port: 27333})
	asst := catalog.NewAssistant(reg, aiSvc, Token)
	e := &fullEnv{h: New(mgr, nil, reg, asst), dials: nd, asst: asst}

	// 1) R5.1：1.21.9 之前的老版本（如 1.21.8）不去读世界存档里的这四条游戏规则
	if err := worldtest.WriteLevelDat(filepath.Join(oldDir, "world", "level.dat"), worldtest.LevelOpts{
		VersionName: "1.21.8",
		DataVersion: 4440,
		GameRules:   map[string]string{"pvp": "false", "spawnMonsters": "false"},
	}); err != nil {
		t.Fatal(err)
	}
	oldURL := "/api/instances/" + url.PathEscape("老版服") + "/properties"
	recOld := e.call(t, "GET", oldURL, "", 200)
	var rOld struct {
		Data struct {
			GameRules map[string]string `json:"gamerules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recOld.Body.Bytes(), &rOld); err != nil {
		t.Fatal(err)
	}
	if len(rOld.Data.GameRules) != 0 {
		t.Fatalf("1.21.9 之前的版本不应去读存档游戏规则：%#v", rOld.Data.GameRules)
	}

	propsURL := "/api/instances/" + url.PathEscape("新版服") + "/properties"

	// 2) 初始新版服里的 level.dat 是损坏的占位内容：读不到时 gamerules 为空，接口照常返回 pairs
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

	// 3) 按 server.properties 的 level-name 找自定义世界目录（带子目录），读取 26.2 的 game_rules.dat
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

	// 4) level-name 越界指到服务器目录外时：拒绝跟出去读，回退为空
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
