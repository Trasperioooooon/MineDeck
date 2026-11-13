// worldimport-realtest 用真实的服务端验证地图导入（docs/地图导入-设计.md「验证分几层」第 2 层）。
//
// 它不开 MineDeck 的窗口，直接用 MineDeck 自己的代码：新建流水线下载核心和 Java、进程管理器起停服务器、
// 地图导入服务做导入和换回。每台测试服务器只绑 127.0.0.1（不触发防火墙询问），端口从 -port 起往上排。
// 地图里的「标记」是用控制台命令放的方块：主世界金块、下界钻石块、末地绿宝石块（来源地图），
// 目标服务器原来的世界是铁块 / 青金石块。导入、启动以后用 execute if block 在三个维度分别检查，
// 输出 Test passed 才算「服务器加载的是导入的那张图」。
//
// 用法（在仓库根目录）：
//
//	go run ./tools/worldimport-realtest -base D:\MineDeck-wi-test\run1 -legacy 1.21.4 -modern 26.1
//
// -base 必须是一个新的或专门给测试用的文件夹：里面会生成 servers、runtimes、cache 和 result.md。
// 不碰作者自己的 MineDeck 文件夹和存档。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/procutil"
	"minedeck/internal/worldimport"
)

const owner = "api|本机"

var (
	mgr  *inst.Manager
	svc  *worldimport.Service
	out  *os.File
	port int
	fail int
)

func logf(format string, a ...any) {
	line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, a...)
	fmt.Println(line)
	if out != nil {
		fmt.Fprintln(out, line)
	}
}

func check(ok bool, what string) {
	if ok {
		logf("PASS  %s", what)
		return
	}
	fail++
	logf("FAIL  %s", what)
}

func must(err error, what string) {
	if err != nil {
		logf("ABORT %s: %v", what, err)
		os.Exit(2)
	}
}

func main() {
	base := flag.String("base", `D:\MineDeck-wi-test\run`, "测试用的程序目录（会在里面建 servers、runtimes、cache）")
	legacy := flag.String("legacy", "1.21.4", "26.1 以前的测试版本（空 = 跳过）")
	modern := flag.String("modern", "", "26.1 起的测试版本（空 = 跳过）")
	flag.IntVar(&port, "port", 25611, "第一台测试服务器的端口")
	suites := flag.String("suites", "legacy,modern,upgrade", "要跑哪几组（逗号分隔）：legacy、modern、upgrade（upgrade 要 legacy 和 modern 两个版本都给）")
	flag.Parse()
	want := func(s string) bool { return strings.Contains(","+*suites+",", ","+s+",") }

	abs, err := filepath.Abs(*base)
	must(err, "base")
	if strings.Contains(strings.ToLower(abs), `d:\minedeck\`) || strings.EqualFold(abs, `D:\MineDeck`) {
		must(errors.New("不能用作者自己的 MineDeck 文件夹"), "base")
	}
	app.Base = abs
	app.RuntimesDir = filepath.Join(abs, "runtimes")
	app.CacheDir = filepath.Join(abs, "cache")
	app.ServersDir = filepath.Join(abs, "servers")
	app.BackupsDir = filepath.Join(abs, "backups")
	for _, d := range []string{app.RuntimesDir, app.CacheDir, app.ServersDir, app.BackupsDir} {
		must(os.MkdirAll(d, 0o755), "mkdir")
	}
	_, err = app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil })
	must(err, "config")
	out, err = os.OpenFile(filepath.Join(abs, "result.md"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	must(err, "result.md")
	defer out.Close()
	logf("==== 开始 base=%s legacy=%q modern=%q", abs, *legacy, *modern)

	procutil.InitJob() // 和 MineDeck 一样：这个程序退出（包括中途 ABORT）时，它起的服务端一起结束
	java.Scan()
	mgr, err = inst.NewManager()
	must(err, "NewManager")
	svc = worldimport.NewService(mgr)
	svc.RecoverAll()

	if *legacy != "" && want("legacy") {
		legacySuite(*legacy)
	}
	if *modern != "" && want("modern") {
		modernSuite(*modern)
	}
	if *legacy != "" && *modern != "" && want("upgrade") {
		upgradeSuite(*legacy, *modern)
	}
	logf("==== 结束：%d 项失败", fail)
	if fail > 0 {
		os.Exit(1)
	}
}

// ---------- 服务器 ----------

func create(name string, core mcsrc.Core, mc string) string {
	if i, err := mgr.Get(name); err == nil {
		logf("已有 %s，沿用（%s）", name, i.Dir)
		return i.Dir
	}
	p := port
	port++
	id, err := mgr.CreateAsync(inst.CreateReq{Name: name, Core: core, MC: mc, XmxMB: 2048, Port: p, EULA: true, MOTD: "worldimport test"})
	must(err, "create "+name)
	res, msg := waitTask(id, 30*time.Minute)
	if msg != "" {
		must(errors.New(msg), "create "+name)
	}
	logf("新建 %s（%s %s，端口 %d）：%s", name, core, mc, p, res)
	i, err := mgr.Get(name)
	must(err, "get "+name)
	must(inst.ForceProp(i.PropsPath(), "server-ip", "127.0.0.1"), "server-ip")
	return i.Dir
}

func waitTask(id string, d time.Duration) (string, string) {
	deadline := time.Now().Add(d)
	last := ""
	for time.Now().Before(deadline) {
		t := mgr.Tasks.Get(id)
		if t != nil {
			s := t.Snapshot()
			if s.Label != last && s.Label != "" {
				last = s.Label
			}
			if s.Ended {
				if s.Err != "" {
					tail := s.Log
					if len(tail) > 8 {
						tail = tail[len(tail)-8:]
					}
					return "", s.Err + "\n    " + strings.Join(tail, "\n    ")
				}
				return s.Result, ""
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", "任务超时"
}

func start(name string) {
	i, err := mgr.Get(name)
	must(err, name)
	must(mgr.Start(name), "start "+name)
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		switch i.Status() {
		case "running":
			logf("%s 启动完成", name)
			return
		case "stopped":
			lines := i.Console.Lines()
			if len(lines) > 15 {
				lines = lines[len(lines)-15:]
			}
			must(fmt.Errorf("进程退出了：\n    %s", strings.Join(lines, "\n    ")), "start "+name)
		}
		time.Sleep(500 * time.Millisecond)
	}
	must(errors.New("10 分钟没启动完"), "start "+name)
}

func stop(name string) {
	must(mgr.StopAndWait(name, 3*time.Minute), "stop "+name)
	logf("%s 已停止（%s）", name, mustInst(name).LastExit())
}

func mustInst(name string) *inst.Instance {
	i, err := mgr.Get(name)
	must(err, name)
	return i
}

// run 发一条命令，等第一行含 want 里任一子串的输出。
func run(name, cmd string, want ...string) string {
	i := mustInst(name)
	_, ch, cancel := i.Console.Subscribe()
	defer cancel()
	must(mgr.Command(name, cmd), cmd)
	timeout := time.After(20 * time.Second)
	for {
		select {
		case line := <-ch:
			for _, w := range want {
				if strings.Contains(line, w) {
					return line
				}
			}
		case <-timeout:
			return ""
		}
	}
}

var dims = []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"}

// mark 在三个维度的 (0,100,0) 各放一种方块。
func mark(name string, blocks [3]string) {
	for k, d := range dims {
		run(name, "execute in "+d+" run forceload add 0 0", "Marked", "already", "forceloaded", "chunk")
		// 已经是这种方块时 setblock 会报「Could not set the block」：放之前先清成空气，重跑同一个测试文件夹也能用
		run(name, fmt.Sprintf("execute in %s run setblock 0 100 0 minecraft:air", d), "Changed the block", "Could not set")
		got := run(name, fmt.Sprintf("execute in %s run setblock 0 100 0 %s", d, blocks[k]), "Changed the block", "Could not set")
		if got == "" || strings.Contains(got, "Could not") {
			must(fmt.Errorf("%s：%q", d, got), "mark "+name)
		}
	}
	run(name, "save-all flush", "Saved the game", "saved")
	logf("%s 放好标记 %v", name, blocks)
}

// verify 三个维度的 (0,100,0) 是不是这几种方块。
func verify(name string, blocks [3]string, what string) {
	for k, d := range dims {
		run(name, "execute in "+d+" run forceload add 0 0", "Marked", "already", "forceloaded", "chunk")
		got := run(name, fmt.Sprintf("execute in %s if block 0 100 0 %s", d, blocks[k]), "Test passed", "Test failed", "not loaded")
		check(strings.Contains(got, "Test passed"), fmt.Sprintf("%s：%s 在 %s 找到 %s（%s）", what, name, d, blocks[k], strings.TrimSpace(got)))
	}
}

// ---------- 导入 ----------

// snapshot 来源目录的样子（相对路径 → 大小和修改时间）：导入前后比一遍，证明来源没被动过。
func snapshot(dir string) map[string]string {
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			if !strings.HasPrefix(rel, ".minedeck") && !strings.HasPrefix(rel, "logs") {
				out[rel] = fmt.Sprintf("%d|%d", fi.Size(), fi.ModTime().UnixNano())
			}
		}
		return nil
	})
	return out
}

func sameSnap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func importInto(target, srcPath, prefer string) string {
	before := snapshot(srcPath)
	defer func() {
		check(sameSnap(before, snapshot(srcPath)), fmt.Sprintf("导入以后来源 %s 一个文件都没变（%d 个文件）", srcPath, len(before)))
	}()
	sc, err := svc.ScanPath(owner, srcPath)
	must(err, "scan")
	if len(sc.Candidates) == 0 {
		must(fmt.Errorf("没有候选：%+v", sc.Issues), "scan "+srcPath)
	}
	cand := sc.Candidates[0]
	for _, c := range sc.Candidates {
		if c.FolderName == prefer {
			cand = c
		}
	}
	logf("检测 %s：%d 个候选，用 %s（布局 %s，版本 %s，维度 %d）", srcPath, len(sc.Candidates), cand.Root, cand.Layout, cand.Meta.VersionName, len(cand.Dimensions))
	p, err := svc.MakePlan(context.Background(), owner, worldimport.PlanReq{ScanID: sc.ID, Candidate: cand.ID, Inst: target})
	must(err, "plan")
	for _, is := range p.Issues {
		logf("  [%s] %s", is.Sev, is.Msg)
	}
	if p.Blocked {
		must(errors.New("计划被拦下"), "plan")
	}
	id, err := svc.Execute(owner, worldimport.ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	must(err, "execute")
	res, msg := waitTask(id, 20*time.Minute)
	if msg != "" {
		must(errors.New(msg), "import")
	}
	logf("导入到 %s：%s", target, res)
	st, err := svc.Status(target)
	must(err, "status")
	return st.Points[0].ID
}

func restore(target, txID string) {
	id, err := svc.Restore(target, txID, "restore")
	must(err, "restore")
	res, msg := waitTask(id, 10*time.Minute)
	if msg != "" {
		must(errors.New(msg), "restore")
	}
	logf("换回 %s：%s", target, res)
}

func ls(dir string) string {
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, " ")
}

var (
	srcBlocks  = [3]string{"minecraft:gold_block", "minecraft:diamond_block", "minecraft:emerald_block"}
	oldPaper   = [3]string{"minecraft:iron_block", "minecraft:iron_block", "minecraft:iron_block"}
	oldVanilla = [3]string{"minecraft:lapis_block", "minecraft:lapis_block", "minecraft:lapis_block"}
)

// legacySuite 26.1 以前：原版地图 → Paper（靠 Paper 第一次启动自己迁移下界末地）；Paper 三目录地图 → 原版（导入时并进主目录）。
func legacySuite(mc string) {
	logf("---- 旧布局 %s", mc)
	v := "V-" + mc
	p := "P-" + mc
	v2 := "V2-" + mc
	vdir := create(v, mcsrc.CoreVanilla, mc)
	pdir := create(p, mcsrc.CorePaper, mc)
	create(v2, mcsrc.CoreVanilla, mc)

	start(v)
	mark(v, srcBlocks)
	stop(v)
	start(p)
	mark(p, oldPaper)
	stop(p)
	start(v2)
	mark(v2, oldVanilla)
	stop(v2)

	// 1. 原版世界文件夹 → Paper
	tx := importInto(p, filepath.Join(vdir, "world"), "world")
	start(p)
	verify(p, srcBlocks, "原版地图导入 Paper "+mc)
	stop(p)
	logf("Paper 启动后的世界目录：%s", ls(pdir))
	restore(p, tx)
	start(p)
	verify(p, oldPaper, "Paper "+mc+" 换回导入前的地图")
	stop(p)

	// 2. Paper 服务器文件夹（三目录）→ 原版
	tx = importInto(v2, pdir, "world")
	start(v2)
	verify(v2, oldPaper, "Paper 三目录地图导入原版 "+mc)
	stop(v2)
	restore(v2, tx)
	start(v2)
	verify(v2, oldVanilla, "原版 "+mc+" 换回导入前的地图")
	stop(v2)
}

// modernSuite 26.1 起：原版地图 → Paper；Paper 地图（五个共用文件在主世界目录里）→ 原版。
func modernSuite(mc string) {
	logf("---- 新布局 %s", mc)
	v := "V-" + mc
	p := "P-" + mc
	v2 := "V2-" + mc
	vdir := create(v, mcsrc.CoreVanilla, mc)
	pdir := create(p, mcsrc.CorePaper, mc)
	create(v2, mcsrc.CoreVanilla, mc)

	start(v)
	mark(v, srcBlocks)
	stop(v)
	start(p)
	mark(p, oldPaper)
	stop(p)
	start(v2)
	mark(v2, oldVanilla)
	stop(v2)
	logf("原版 %s 的世界目录：%s / dimensions：%s", mc, ls(filepath.Join(vdir, "world")), ls(filepath.Join(vdir, "world", "dimensions", "minecraft")))
	logf("Paper %s 的世界目录：%s / dimensions：%s", mc, ls(pdir), ls(filepath.Join(pdir, "world", "dimensions", "minecraft")))

	tx := importInto(p, filepath.Join(vdir, "world"), "world")
	start(p)
	verify(p, srcBlocks, "原版地图导入 Paper "+mc)
	stop(p)
	restore(p, tx)
	start(p)
	verify(p, oldPaper, "Paper "+mc+" 换回导入前的地图")
	stop(p)

	tx = importInto(v2, pdir, "world")
	start(v2)
	verify(v2, oldPaper, "Paper 地图导入原版 "+mc)
	stop(v2)
	restore(v2, tx)
	start(v2)
	verify(v2, oldVanilla, "原版 "+mc+" 换回导入前的地图")
	stop(v2)
}

// upgradeSuite 旧版本的地图导入新版本的服务器（26.1 以前 → 26.1 起，要用户确认升级）：
// 原版地图 → 原版、原版地图 → Paper（靠核心自己升级和迁移），Paper 三目录地图 → 原版、→ Paper
// （导入时先按 Paper 文档摆成原版布局，再由核心升级）。要先跑过 legacy 和 modern 两组，服务器和标记都沿用。
func upgradeSuite(oldMC, newMC string) {
	logf("---- 升级 %s → %s", oldMC, newMC)
	vOld := mustInst("V-" + oldMC).Dir
	pOld := mustInst("P-" + oldMC).Dir
	v2 := "V2-" + newMC
	p := "P-" + newMC
	copper := [3]string{"minecraft:copper_block", "minecraft:copper_block", "minecraft:copper_block"}

	tx := importInto(v2, filepath.Join(vOld, "world"), "world")
	start(v2)
	verify(v2, srcBlocks, "原版 "+oldMC+" 地图升级进原版 "+newMC)
	stop(v2)
	restore(v2, tx)

	tx = importInto(v2, pOld, "world")
	start(v2)
	verify(v2, oldPaper, "Paper "+oldMC+" 三目录地图升级进原版 "+newMC)
	stop(v2)
	restore(v2, tx)
	start(v2)
	verify(v2, oldVanilla, "原版 "+newMC+" 两次换回以后还是原来的地图")
	stop(v2)

	start(p)
	mark(p, copper)
	stop(p)
	tx = importInto(p, filepath.Join(vOld, "world"), "world")
	start(p)
	verify(p, srcBlocks, "原版 "+oldMC+" 地图升级进 Paper "+newMC)
	stop(p)
	restore(p, tx)

	tx = importInto(p, pOld, "world")
	start(p)
	verify(p, oldPaper, "Paper "+oldMC+" 三目录地图升级进 Paper "+newMC)
	stop(p)
	restore(p, tx)
	start(p)
	verify(p, copper, "Paper "+newMC+" 两次换回以后还是原来的地图")
	stop(p)
}
