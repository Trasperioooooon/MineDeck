package worldimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/diskguard"
	"minedeck/internal/mcsrc"
)

func TestWorldRootConfigMustBeReadable(t *testing.T) {
	for _, kind := range []string{"bukkit", "args"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t, "")
			i, _ := e.m.Get("生存服")
			rel, content := "bukkit.yml", "settings:\n  world-container: .\n"
			if kind == "bukkit" {
				i.Core = mcsrc.CorePaper
			} else {
				rel, content = "libraries/test/win_args.txt", "-Xmx2G\n"
				i.LaunchTarget = "args:" + rel
			}
			write(t, e.dir, map[string]string{rel: content})
			src := e.bukkitZip()
			if p := e.plan(src); p.Blocked {
				t.Fatalf("可读的默认世界位置应当允许导入：%v", codes(p.Issues))
			}
			unlock := lockFileExclusive(t, filepath.Join(e.dir, filepath.FromSlash(rel)))
			p := e.plan(src)
			if !p.Blocked || !strings.Contains(strings.Join(codes(p.Issues), ","), "world-config-unreadable") {
				t.Fatalf("读不了决定世界位置的配置时必须阻断：%v", codes(p.Issues))
			}
			if _, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true}); err == nil {
				t.Fatal("被阻断的计划不能执行")
			}
			unlock()
			if p := e.plan(src); p.Blocked {
				t.Fatalf("文件释放后正常输入仍可用：%v", codes(p.Issues))
			}
		})
	}
}

func TestWorldRootConfigMissingDefaults(t *testing.T) {
	e := newEnv(t, "")
	i, _ := e.m.Get("生存服")
	i.Core = mcsrc.CorePaper
	src := e.bukkitZip()
	if p := e.plan(src); p.Blocked {
		t.Fatalf("尚未生成 bukkit.yml 应按默认世界位置处理：%v", codes(p.Issues))
	}
	i.LaunchTarget = "args:libraries/missing/win_args.txt"
	if p := e.plan(src); !p.Blocked || !strings.Contains(strings.Join(codes(p.Issues), ","), "world-config-unreadable") {
		t.Fatalf("明确引用但丢失的参数文件不能猜：%v", codes(p.Issues))
	}
}

func TestWorldRootConfigRecheckedBeforeCommit(t *testing.T) {
	for _, kind := range []string{"bukkit", "args", "safe-change"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t, "")
			i, _ := e.m.Get("生存服")
			rel, content, changed := "bukkit.yml", "settings:\n  world-container: .\n", "settings:\n  world-container: elsewhere\n"
			i.Core = mcsrc.CorePaper
			if kind == "args" {
				rel, content, changed = "win_args.txt", "-Xmx2G\n", "--universe elsewhere\n"
				i.LaunchTarget = "args:" + rel
			} else if kind == "safe-change" {
				changed = content + "# changed since preview\n"
			}
			write(t, e.dir, map[string]string{rel: content})
			p := e.plan(e.bukkitZip())
			if p.Blocked {
				t.Fatalf("准备计划被阻断：%v", codes(p.Issues))
			}
			var hookErr error
			stagedHook = func() { hookErr = os.WriteFile(filepath.Join(e.dir, filepath.FromSlash(rel)), []byte(changed), 0o644) }
			t.Cleanup(func() { stagedHook = nil })
			id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
			if err != nil {
				t.Fatal(err)
			}
			_, msg := e.wait(id)
			if hookErr != nil {
				t.Fatal(hookErr)
			}
			if msg == "" {
				t.Fatal("暂存后目标配置变动必须拒绝提交")
			}
			if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
				t.Fatalf("原世界必须未被替换：%s", got)
			}
			if who := e.m.WorldBusy("生存服"); who != "" {
				t.Fatalf("失败后不能遗留占位：%s", who)
			}
		})
	}
}

func TestWorldRootConfigFingerprintChanges(t *testing.T) {
	e := newEnv(t, "")
	i, _ := e.m.Get("生存服")
	i.Core = mcsrc.CorePaper
	read := func() string {
		target, _, err := e.s.targetOf("生存服")
		if err != nil {
			t.Fatal(err)
		}
		worldRootChecks(target)
		return targetFP(target)
	}
	before := read()
	write(t, e.dir, map[string]string{"bukkit.yml": "settings:\n  world-container: .\n"})
	if after := read(); before == after {
		t.Fatal("目标指纹必须包括决定世界位置的配置")
	}
}

func TestCancelledDuplicateScanKeepsSharedUpload(t *testing.T) {
	e := newEnv(t, "")
	b, err := os.ReadFile(mkZip(t, map[string]string{"world/level.dat": rawNBT(8)}))
	if err != nil {
		t.Fatal(err)
	}
	p, err := SaveUpload(bytes.NewReader(b), 1<<20, int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.s.ScanUpload(me, p, "map.zip")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.s.ScanUploadCtx(ctx, me, p, "map.zip"); err == nil {
		t.Fatal("取消的第二次检测必须失败")
	}
	if !exists(p) {
		t.Fatal("不能删除第一份有效结果仍使用的上传")
	}
	second, err := e.s.ScanUpload(me, p, "map.zip")
	if err != nil {
		t.Fatal(err)
	}
	e.s.Discard(me, first.ID)
	if !exists(p) {
		t.Fatal("另一个结果还引用上传，不能删")
	}
	e.s.Discard(me, second.ID)
	if exists(p) {
		t.Fatal("最后一个结果释放后应删除上传")
	}
}

func TestCancelledFormatRejectionDoesNotLeaveUpload(t *testing.T) {
	e := newEnv(t, "")
	p, err := SaveUpload(strings.NewReader("not a zip"), 1024, 9)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.s.ScanUploadCtx(ctx, me, p, "invalid.zip"); err == nil {
		t.Fatal("已经取消不能发布格式拒绝结果")
	}
	if exists(p) || len(e.s.scans) != 0 || len(e.s.scanSlots) != 0 {
		t.Fatal("取消后不得遗留结果、文件或名额")
	}
}

func TestScanCancellationStopsActualContentReads(t *testing.T) {
	e := newEnv(t, "")
	files := map[string]string{}
	for n := 0; n < 30; n++ {
		files[fmt.Sprintf("w%02d/level.dat", n)] = rawNBT(8)
	}
	p := mkZip(t, files)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	sourceOpenHook = func(rel string) {
		if strings.HasSuffix(rel, "level.dat") {
			reads++
			if reads == 3 {
				cancel()
			}
		}
	}
	t.Cleanup(func() { sourceOpenHook = nil })
	if _, err := e.s.ScanPathCtx(ctx, me, p); err == nil {
		t.Fatal("扫描应当响应真实context取消")
	}
	if reads != 3 {
		t.Fatalf("应在第三次内容读取前停止，实际 %d", reads)
	}
}

func TestProbeAccountsActualBytesAfterSourceChanges(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"manifest.json": "x"})
	src, err := openFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	write(t, dir, map[string]string{"manifest.json": strings.Repeat("x", 8192)})
	old := maxProbeBytes
	maxProbeBytes = 1024
	t.Cleanup(func() { maxProbeBytes = old })
	r := &scanResult{src: src, ctx: context.Background()}
	_ = r.probe(src.find("manifest.json"), 8<<20)
	if r.probeBytes != 1024 {
		t.Fatalf("应按实际读取扣账且不能越过预算：%d", r.probeBytes)
	}
	_ = r.probe(src.find("manifest.json"), 8<<20)
	if r.probeReads != 1 {
		t.Fatal("字节额度耗尽后不能再打开文件")
	}
}

// 地图暂存（或备份）占着同一个盘的独占写入名额时，接收上传不排在它后面：上传是按对方的网速进来的，
// 让它排队，等久了会被当成没有进展掐掉；让它占名额，暂存和备份又得等它传完。取消仍然管用，名额和文件都不留。
func TestUploadDoesNotQueueBehindStaging(t *testing.T) {
	e := newEnv(t, "")
	lease, err := diskguard.Acquire(context.Background(), e.tmp, diskguard.Options{Query: func(string) (int64, error) { return 1 << 40, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	old := diskFree
	diskFree = func(string) (int64, error) { return 1 << 40, nil }
	t.Cleanup(func() { diskFree = old })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dest, err := SaveUploadCtx(ctx, strings.NewReader("data"), 1024, 4)
	if err != nil {
		t.Fatalf("名额被暂存占着时上传应当照常收下：%v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "data" {
		t.Fatalf("收下的内容不对：%q", b)
	}
	_ = os.Remove(dest)
	// 余量不够照旧拦，不因为不排队就不查了
	diskFree = func(string) (int64, error) { return uploadReserve / 2, nil }
	if _, err := SaveUploadCtx(ctx, strings.NewReader("data"), 1024, 4); !errors.Is(err, ErrUploadNoSpace) {
		t.Fatalf("余量不够应当拒绝：%v", err)
	}
	diskFree = func(string) (int64, error) { return 1 << 40, nil }
	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := SaveUploadCtx(done, strings.NewReader("data"), 1024, 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("已经取消的上传不该收：%v", err)
	}
	if len(uploadSlots) != 0 {
		t.Fatal("上传结束以后必须归还上传名额")
	}
	if ents, _ := os.ReadDir(UploadDir()); len(ents) != 0 {
		t.Fatalf("没收下的上传不得留下文件：%v", ents)
	}
}

func TestCapacityBecomesUnknownDuringCopyStopsUnlessConfirmed(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(confirmed), func(t *testing.T) {
			oldFree, oldEvery := diskFree, stageCheckEvery
			t.Cleanup(func() { diskFree, stageCheckEvery = oldFree, oldEvery })
			stageCheckEvery = 1 << 20
			src, err := openZip(mkZip(t, map[string]string{"world/big.bin": strings.Repeat("x", 4<<20)}))
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			files := []stagedFile{{e: &src.files[0], to: "world/big.bin"}}
			calls := 0
			diskFree = func(string) (int64, error) {
				calls++
				if calls > 3 {
					return 0, errors.New("capacity unavailable")
				}
				return 1 << 40, nil
			}
			ctx := context.Background()
			if confirmed {
				ctx = withSpaceConfirmation(ctx, &Plan{Issues: []Issue{{Code: "disk-unknown", Sev: SevConfirm}}})
			}
			err = stageFiles(ctx, src, files, filepath.Join(t.TempDir(), "stage"), nil, nil)
			if confirmed && err != nil {
				t.Fatalf("明确确认的降级应允许继续：%v", err)
			}
			if !confirmed && err != nil && strings.Contains(err.Error(), "压缩包") {
				t.Fatalf("余量查询失败不该说压缩包坏了：%v", err)
			}
			if !confirmed && !errors.Is(err, diskguard.ErrUnknown) {
				t.Fatalf("新出现的查询失败必须停止：%v", err)
			}
		})
	}
}

func TestStageIdentityRejectsSameHashAndSizeDifferentDirectory(t *testing.T) {
	dir, j := setup(t)
	stage := filepath.Join(txAbs(dir, j.ID), stageSub)
	original := filepath.Join(stage, "world")
	if err := os.Rename(original, filepath.Join(dir, "kept-copy")); err != nil {
		t.Fatal(err)
	}
	write(t, stage, map[string]string{"world/level.dat": "NEW", "world/region/r.0.0.mca": "new-ow"})
	j.NewBytes = dirBytes(stage)
	if stageIsOurs(stage, j) {
		t.Fatal("相同level.dat和字节数不能替代目录身份")
	}
	if err := rollbackTx(dir, j); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(original, "region", "r.0.0.mca")) {
		t.Fatal("未知身份的地图必须保留")
	}
}

func TestStageIdentityKeepsLegacyAndUnverifiedSiblings(t *testing.T) {
	dir, j := setup(t)
	stage := filepath.Join(txAbs(dir, j.ID), stageSub)
	j.NewDirIDs = nil
	if stageIsOurs(stage, j) {
		t.Fatal("旧记录没有目录身份时保留数据")
	}
	if err := os.RemoveAll(filepath.Join(stage, "world")); err != nil {
		t.Fatal(err)
	}
	write(t, stage, map[string]string{"world_nether/DIM-1/region/r.mca": "unique-world"})
	if stageIsOurs(stage, j) {
		t.Fatal("主世界缺失不能授权删除未知维度")
	}
}

func TestEveryStrictStagingReadFailureStopsBeforeMovingWorlds(t *testing.T) {
	for _, name := range []string{"existing-new-hash", "existing-old-hash", "existing-size", "create-new-hash", "create-size"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "")
			src := e.bukkitZip()
			creating := strings.HasPrefix(name, "create-")
			var p *Plan
			if creating {
				sc, err := e.s.ScanPath(me, src)
				if err != nil {
					t.Fatal(err)
				}
				p, err = e.s.MakePlan(context.Background(), me, PlanReq{ScanID: sc.ID, Candidate: sc.Candidates[0].ID, NewInst: true, Core: "vanilla", MC: "1.20.4"})
				if err != nil || p.Blocked {
					t.Fatalf("新建计划：%v", err)
				}
				if err := e.s.CheckCreatePlan(me, p.ID, "vanilla", "1.20.4", true); err != nil {
					t.Fatal(err)
				}
				defer e.s.ReleaseCreate(me, p.ID)
			} else {
				p = e.plan(src)
			}
			hit := false
			faultHook = func(point string) error {
				low := strings.ToLower(filepath.ToSlash(point))
				isSize := strings.HasPrefix(low, "size:") && strings.HasSuffix(low, "/new")
				isNewHash := strings.HasPrefix(low, "hash:") && strings.Contains(low, "/new/world/level.dat")
				isOldHash := point == "hash:"+filepath.Join(e.dir, "world", "level.dat")
				if strings.HasSuffix(name, "-size") && isSize || strings.HasSuffix(name, "-new-hash") && isNewHash || strings.HasSuffix(name, "-old-hash") && isOldHash {
					hit = true
					return errors.New("injected strict read failure")
				}
				return nil
			}
			t.Cleanup(func() { faultHook = nil })
			if creating {
				dir := filepath.Join(e.tmp, "fresh")
				write(t, dir, map[string]string{"server.properties": "level-name=world\n"})
				task, ctx := e.m.Tasks.New("test", []string{"x"})
				err := e.s.placeForCreate(ctx, task, me, p.ID, dir)
				if err == nil || !strings.Contains(err.Error(), "injected strict read failure") {
					t.Fatalf("新建必须保留读取错误：%v", err)
				}
				if exists(filepath.Join(dir, "world")) {
					t.Fatal("读取失败前不能搬入新建实例")
				}
			} else {
				id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
				if err != nil {
					t.Fatal(err)
				}
				_, msg := e.wait(id)
				if !strings.Contains(msg, "injected strict read failure") {
					t.Fatalf("必须保留读取错误：%s", msg)
				}
				if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
					t.Fatalf("原世界被修改：%s", got)
				}
			}
			if !hit {
				t.Fatal("测试没有走到指定的严格读取调用")
			}
		})
	}
}
