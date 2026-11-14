package worldimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 事务的测试不需要真的 NBT：只看目录搬得对不对、level.dat 的内容（哈希）对不对。

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// setup 一台有旧世界（三个目录）的服务器，暂存区里放好新世界（只有主目录），返回实例目录和记录。
func setup(t *testing.T) (string, *Journal) {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"server.properties":               "level-name=world\ndifficulty=easy\n",
		"world/level.dat":                 "OLD",
		"world/region/r.0.0.mca":          "old-ow",
		"world_nether/DIM-1/region/r.mca": "old-nether",
		"world_the_end/DIM1/region/r.mca": "old-end",
		"other_world/level.dat":           "OTHER",
	})
	id := "wtest"
	stage := filepath.Join(txAbs(dir, id), stageSub)
	write(t, stage, map[string]string{
		"world/level.dat":           "NEW",
		"world/region/r.0.0.mca":    "new-ow",
		"world/DIM-1/region/r.mca":  "new-nether",
		"world/datapacks/a.zip":     "pack",
		"world/players/data/u.dat":  "p",
		"world/playerdata/u.dat":    "p",
		"world/advancements/u.json": "{}",
	})
	j := &Journal{ID: id, Inst: "s", State: TxStaged, LevelName: "world",
		OldLevelSHA: fileSHA(filepath.Join(dir, "world", "level.dat")),
		NewLevelSHA: fileSHA(filepath.Join(stage, "world", "level.dat"))}
	for _, n := range worldDirNames("world") {
		if exists(filepath.Join(dir, n)) {
			j.OldOut = append(j.OldOut, Step{From: n, To: txRel(id) + "/" + oldSub + "/" + n})
		}
		if exists(filepath.Join(stage, n)) {
			j.NewIn = append(j.NewIn, Step{From: txRel(id) + "/" + stageSub + "/" + n, To: n})
		}
	}
	var stagedNames []string
	for _, step := range j.NewIn {
		stagedNames = append(stagedNames, step.To)
	}
	rememberStagedDirs(stage, stagedNames, j)
	if err := saveJournal(dir, j); err != nil {
		t.Fatal(err)
	}
	return dir, j
}

func assertOld(t *testing.T, dir string) {
	t.Helper()
	for rel, want := range map[string]string{
		"world/level.dat":                 "OLD",
		"world/region/r.0.0.mca":          "old-ow",
		"world_nether/DIM-1/region/r.mca": "old-nether",
		"world_the_end/DIM1/region/r.mca": "old-end",
		"other_world/level.dat":           "OTHER",
	} {
		if got := read(t, filepath.Join(dir, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	if exists(filepath.Join(dir, "world", "DIM-1")) {
		t.Error("旧世界里混进了新世界的 DIM-1")
	}
}

func assertNew(t *testing.T, dir string) {
	t.Helper()
	for rel, want := range map[string]string{
		"world/level.dat":          "NEW",
		"world/region/r.0.0.mca":   "new-ow",
		"world/DIM-1/region/r.mca": "new-nether",
		"other_world/level.dat":    "OTHER",
	} {
		if got := read(t, filepath.Join(dir, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	// 新图没带 _nether、_the_end：旧的必须移走，不能留着被重新加载
	if exists(filepath.Join(dir, "world_nether")) || exists(filepath.Join(dir, "world_the_end")) {
		t.Error("旧图的 _nether / _the_end 还留在原位")
	}
}

func TestCommitHappy(t *testing.T) {
	dir, j := setup(t)
	stuck, err := commitTx(dir, j, map[string]string{"difficulty": "hard"})
	if err != nil || stuck {
		t.Fatalf("commit: %v stuck=%v", err, stuck)
	}
	assertNew(t, dir)
	if got := read(t, filepath.Join(txAbs(dir, j.ID), oldSub, "world_nether", "DIM-1", "region", "r.mca")); got != "old-nether" {
		t.Fatalf("恢复点里的旧下界 = %q", got)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "server.properties")), "difficulty=hard") {
		t.Fatal("设置没改")
	}
	j2, err := loadJournal(dir, j.ID)
	if err != nil || j2.State != TxCommitted || j2.PropsBefore["difficulty"] != "easy" {
		t.Fatalf("journal = %+v %v", j2, err)
	}
	// 换回旧图：先模拟游戏启动后改写了导入的世界、生成了 _nether
	write(t, dir, map[string]string{"world/region/r.0.0.mca": "played", "world_nether/level.dat": "GEN"})
	stuck, err = restoreTx(dir, j2)
	if err != nil || stuck {
		t.Fatalf("restore: %v stuck=%v", err, stuck)
	}
	assertOld(t, dir)
	if got := read(t, filepath.Join(txAbs(dir, j.ID), restoreSub, "world", "region", "r.0.0.mca")); got != "played" {
		t.Fatalf("换回时留在旁边的导入世界 = %q", got)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "server.properties")), "difficulty=easy") {
		t.Fatal("设置没改回去")
	}
}

func TestCommitFaults(t *testing.T) {
	for _, point := range []string{"old-out-0", "old-out-1", "old-out-2", "new-in-0", "props", "verify"} {
		t.Run(point, func(t *testing.T) {
			dir, j := setup(t)
			faultHook = func(p string) error {
				if p == point {
					return errors.New("模拟故障 " + p)
				}
				return nil
			}
			defer func() { faultHook = nil }()
			stuck, err := commitTx(dir, j, map[string]string{"difficulty": "hard"})
			if err == nil || stuck {
				t.Fatalf("err=%v stuck=%v", err, stuck)
			}
			assertOld(t, dir)
			if !strings.Contains(read(t, filepath.Join(dir, "server.properties")), "difficulty=easy") {
				t.Fatal("设置没退回")
			}
			j2, _ := loadJournal(dir, j.ID)
			if j2.State != TxRolledBack {
				t.Fatalf("state = %s", j2.State)
			}
			// 再恢复一次也不会出事（可以重复执行）
			if err := recoverTx(dir, j2); err != nil {
				t.Fatal(err)
			}
			assertOld(t, dir)
		})
	}
}

// 模拟换到一半断电：记录停在 committing，磁盘上已经做了前几步。启动时恢复，恢复两遍结果一样。
func TestCrashMidCommit(t *testing.T) {
	for cut := 0; cut <= 4; cut++ {
		t.Run(string(rune('0'+cut)), func(t *testing.T) {
			dir, j := setup(t)
			j.State = TxCommitting
			if err := saveJournal(dir, j); err != nil {
				t.Fatal(err)
			}
			steps := append(append([]Step{}, j.OldOut...), j.NewIn...)
			for k := 0; k < cut && k < len(steps); k++ {
				if err := doStep(dir, steps[k]); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				j2, err := loadJournal(dir, j.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := recoverTx(dir, j2); err != nil {
					t.Fatal(err)
				}
				assertOld(t, dir)
			}
		})
	}
}

// 暂存区在换的过程中意外丢了：退回时不能把原位的旧世界当成新世界挪走。
func TestStagingVanished(t *testing.T) {
	dir, j := setup(t)
	j.State = TxCommitting
	_ = saveJournal(dir, j)
	if err := os.RemoveAll(filepath.Join(txAbs(dir, j.ID), stageSub)); err != nil {
		t.Fatal(err)
	}
	if err := recoverTx(dir, j); err != nil {
		t.Fatal(err)
	}
	assertOld(t, dir)
}

// 旧世界被移走以后，原位又冒出一个世界（比如在 MineDeck 外面手动启动生成了新世界）：不猜，标成卡住，什么都不删。
func TestStuckKeepsEverything(t *testing.T) {
	dir, j := setup(t)
	j.State = TxCommitting
	_ = saveJournal(dir, j)
	if err := doStep(dir, j.OldOut[0]); err != nil {
		t.Fatal(err)
	}
	write(t, dir, map[string]string{"world/level.dat": "RANDOM"})
	err := recoverTx(dir, j)
	if err == nil || j.State != TxStuck {
		t.Fatalf("err=%v state=%s", err, j.State)
	}
	if got := read(t, filepath.Join(txAbs(dir, j.ID), oldSub, "world", "level.dat")); got != "OLD" {
		t.Fatalf("恢复点里的旧世界 = %q", got)
	}
	if got := read(t, filepath.Join(dir, "world", "level.dat")); got != "RANDOM" {
		t.Fatalf("原位 = %q", got)
	}
	if len(j.Stuck) == 0 {
		t.Fatal("没记下要人看的路径")
	}
	// 被移走的那个随机世界挪开以后，重试就能恢复
	if err := os.RemoveAll(filepath.Join(dir, "world")); err != nil {
		t.Fatal(err)
	}
	if err := recoverTx(dir, j); err != nil {
		t.Fatal(err)
	}
	assertOld(t, dir)
}

func TestRestoreFaultKeepsImported(t *testing.T) {
	for _, point := range []string{"cur-out-0", "cur-out-1", "old-in-0", "old-in-2"} {
		t.Run(point, func(t *testing.T) {
			dir, j := setup(t)
			if _, err := commitTx(dir, j, nil); err != nil {
				t.Fatal(err)
			}
			write(t, dir, map[string]string{"world_nether/level.dat": "GEN"})
			faultHook = func(p string) error {
				if p == point {
					return errors.New("模拟故障")
				}
				return nil
			}
			defer func() { faultHook = nil }()
			stuck, err := restoreTx(dir, j)
			if err == nil || stuck {
				t.Fatalf("err=%v stuck=%v", err, stuck)
			}
			if got := read(t, filepath.Join(dir, "world", "level.dat")); got != "NEW" {
				t.Fatalf("导入的世界应该还在原位，level.dat = %q", got)
			}
			if got := read(t, filepath.Join(dir, "world_nether", "level.dat")); got != "GEN" {
				t.Fatalf("游戏生成的 _nether 应该还在，= %q", got)
			}
			if j.State != TxCommitted {
				t.Fatalf("state = %s", j.State)
			}
			faultHook = nil
			if _, err := restoreTx(dir, j); err != nil {
				t.Fatal(err)
			}
			assertOld(t, dir)
		})
	}
}

func TestJournalValidate(t *testing.T) {
	bad := []Journal{
		{ID: "x", State: TxCommitting, LevelName: "../evil"},
		{ID: "x", State: TxCommitting, LevelName: "world", OldOut: []Step{{From: "plugins", To: txRel("x") + "/old/plugins"}}},
		{ID: "x", State: TxCommitting, LevelName: "world", OldOut: []Step{{From: "world", To: "../elsewhere"}}},
		{ID: "x", State: TxCommitting, LevelName: "world", NewIn: []Step{{From: txRel("y") + "/new/world", To: "world"}}},
		{ID: "x", State: TxCommitting, LevelName: "world", NewIn: []Step{{From: txRel("x") + "/new/../../../world", To: "world"}}},
		{ID: "x", State: TxCommitting, LevelName: "world", NewIn: []Step{{From: `C:/Windows`, To: "world"}}},
		{ID: "x", State: "done", LevelName: "world"},
		{ID: "x", LevelName: "world"},
	}
	for k, j := range bad {
		if err := j.validate(); err == nil {
			t.Errorf("case %d 应该被拒绝", k)
		}
	}
	good := Journal{ID: "x", State: TxCommitting, LevelName: "worlds/main", OldOut: []Step{{From: "worlds/main", To: txRel("x") + "/old/worlds/main"}}}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
}
