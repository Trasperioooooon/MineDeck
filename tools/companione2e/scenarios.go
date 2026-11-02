//go:build companione2e

package main

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/chatai"
	"minedeck/internal/companion"
	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/diag"
	"minedeck/internal/dl"
	"minedeck/internal/events"
	"minedeck/internal/modinfo"
	"minedeck/internal/ops"
)

// env 一次联调：一个加载器、一个干净的实例目录、MineDeck 这一半的真服务（companion.Service、chatai.Service、ai.Service）。
type env struct {
	o     options
	p     preset
	dir   string
	inst  string
	name  string
	rep   *reporter
	steps map[string]bool
	ctx   context.Context

	man  *companionjar.Manifest
	jar  companionjar.Jar
	srv  *server
	comp *companion.Service
	gc   *chatai.Service
	aisv *ai.Service
	fake *fakeAI
	reg  *ops.Registry
	bots map[string]*bot

	blocked   bool
	session   string
	muID      int // 关服前小木的编号（重启后接管要一样）
	cleanOnce sync.Once

	mu       sync.Mutex
	asked    map[string][]time.Time // 每个玩家触发游戏内 AI 的时间（防刷屏：1 分钟最多 5 次）
	base     [3]int                 // 游戏内指挥用的场地中心
	compLogs []string               // companion.Service 的诊断日志
}

func (e *env) want(step string) bool { return e.steps["all"] || e.steps[step] }

func (e *env) run() int {
	defer e.cleanup()
	if err := e.setup(); err != nil {
		e.rep.check("setup", "准备实例和服务", false, "%v", err)
		return e.rep.summary()
	}
	e.stepInstall()
	if e.o.modJar != "" {
		if err := e.swapModJar(e.o.modJar); err != nil {
			e.rep.check("swap", "换成指定的伙伴模组", false, "%v", err)
			return e.rep.summary()
		}
	}
	later := []string{"connect", "manage", "chat", "water", "reconnect", "portchange", "diag", "exit", "stop", "restart"}
	if slices.ContainsFunc(later, e.want) {
		if e.stepConnect() {
			if e.want("manage") {
				e.stepManage()
			}
			if e.want("chat") {
				e.stepChat()
			}
			if e.want("water") {
				e.stepWater()
			}
			if e.want("reconnect") {
				e.stepReconnect()
			}
			if e.want("portchange") {
				e.stepPortChange()
			}
			if e.want("diag") {
				e.stepDiag()
			}
			if e.want("exit") {
				e.stepExit()
			}
			e.stepStop()
			if e.want("restart") {
				e.stepRestart()
			}
		}
	}
	return e.rep.summary()
}

func (e *env) cleanup() { e.cleanOnce.Do(e.doCleanup) }

func (e *env) doCleanup() {
	if e.blocked {
		e.unblock()
	}
	for _, b := range e.bots {
		b.Quit()
	}
	if e.srv != nil && e.srv.Status() != "stopped" {
		if _, killed := e.srv.Stop(90 * time.Second); killed {
			e.rep.logf("清理：服务器没自己退出，强制结束了")
		}
	}
	if e.fake != nil {
		e.fake.Close()
	}
}

// ---------- 准备 ----------

type memKeys struct{}

func (memKeys) Get(string) (string, error) { return "", nil }
func (memKeys) Set(string, string) error   { return nil }
func (memKeys) State(string) (bool, bool)  { return false, false }

func (e *env) setup() error {
	e.inst = filepath.Join(e.dir, "inst")
	if err := os.RemoveAll(e.inst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(e.inst, "mods"), 0o755); err != nil {
		return err
	}
	// 干净的实例：只从装好加载器的服务端目录取 libraries/（硬链接，不占地方）；mods 是空的、新世界
	if out, err := exec.Command("cp", "-al", filepath.Join(e.o.template, "libraries"), filepath.Join(e.inst, "libraries")).CombinedOutput(); err != nil {
		if out2, err2 := exec.Command("cp", "-a", filepath.Join(e.o.template, "libraries"), filepath.Join(e.inst, "libraries")).CombinedOutput(); err2 != nil {
			return fmt.Errorf("复制 libraries 失败：%s %s", out, out2)
		}
	}
	if _, err := os.Stat(filepath.Join(e.inst, e.p.argsFile)); err != nil {
		return fmt.Errorf("模板里没有启动参数文件 %s", e.p.argsFile)
	}
	props := fmt.Sprintf("online-mode=false\nserver-ip=127.0.0.1\nserver-port=%d\nlevel-seed=minedeck-p7\nmotd=MineDeck companion e2e\n"+
		"max-players=5\nspawn-protection=0\nview-distance=6\nsimulation-distance=6\ndifficulty=peaceful\nsync-chunk-writes=false\nwhite-list=false\n", e.p.gamePort)
	if err := os.WriteFile(filepath.Join(e.inst, "server.properties"), []byte(props), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.inst, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		return err
	}
	var err error
	if e.man, err = companionjar.Load(); err != nil {
		return err
	}
	var ok bool
	if e.jar, ok = e.man.Find(e.p.core, e.p.mc); !ok {
		return fmt.Errorf("清单里没有 %s %s", e.p.core, e.p.mc)
	}
	clog, err := os.Create(filepath.Join(e.dir, "console.log"))
	if err != nil {
		return err
	}
	// JVM 参数照 inst.launchArgs（-Xms、-Xmx、-Dfile.encoding=UTF-8，再加实例的额外参数，最后 @参数文件 nogui）
	args := []string{"-Xms512M", fmt.Sprintf("-Xmx%dM", e.o.xmx)}
	args = append(args, launchJVMArgs...)
	args = append(args, strings.Fields(e.o.extraJVM)...)
	args = append(args, "@"+e.p.argsFile, "nogui")
	e.srv = newServer(e.name, e.inst, e.p.core, e.p.mc, e.p.build, e.o.java, args, clog)

	// 假的 AI 服务 + 真的 AI 接入层（自定义服务商，OpenAI 兼容）
	e.fake = newFakeAI()
	aiDir := filepath.Join(e.dir, "ai")
	_ = os.RemoveAll(aiDir)
	e.aisv = ai.NewService(ai.Options{Dir: aiDir, Keys: memKeys{}, PriceURL: e.fake.URL() + "/prices.json"})
	s := func(v string) *string { return &v }
	proto := ai.OpenAI
	if err := e.aisv.SetProvider(ai.ProviderPatch{Preset: s("custom"), Protocol: &proto, BaseURL: s(e.fake.URL() + "/v1"), Model: s("fake-model")}); err != nil {
		return err
	}
	opDir := filepath.Join(e.dir, "oplog")
	_ = os.RemoveAll(opDir)
	e.reg = ops.NewRegistry(ops.NewLog(opDir))
	e.reg.Register(&ops.Op{Name: "game.info", Title: "游戏内 AI 查看服务器信息", Level: ops.Read, InGame: true,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例")}, "inst"),
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			return map[string]any{"core": e.p.core, "mc": e.p.mc, "online": e.srv.OnlinePlayers()}, nil
		}})
	// 照 catalog.New 接起来：先建伙伴（任务通知经游戏内 AI 发），再建游戏内 AI；服务器每行输出交给游戏内 AI；开服前回调交给伙伴
	var gc *chatai.Service
	e.comp = companion.New(companion.Deps{Instances: compInstances{e.srv}, ClientName: "MineDeck companione2e",
		OnFinish: func(f companion.Finished) {
			if gc != nil {
				gc.NotifyTask(f)
			}
		},
		Logf: func(f string, a ...any) {
			e.mu.Lock()
			e.compLogs = append(e.compLogs, fmt.Sprintf(f, a...))
			e.mu.Unlock()
			e.rep.logf("  [companion] "+f, a...)
		}})
	gc = chatai.New(chatai.Options{Backend: chatBackend{e.srv}, AI: e.aisv, Ops: e.reg, Companions: e.comp})
	e.gc = gc
	e.srv.lineHook = gc.OnLine
	e.srv.beforeStart = e.comp.BeforeStart
	e.bots = map[string]*bot{}
	e.rep.logf("实例 %s：%s；JVM 参数 %s", e.name, e.inst, strings.Join(args, " "))
	return nil
}

// launchJVMArgs inst.launchArgs 里 -Xms、-Xmx 之后的固定参数（控制台编码不是手动选的 GBK 时；改那里时这里跟着改）。
// 两半联调前只有 -Dfile.encoding=UTF-8：Java 21 的控制台输出按系统编码，中文变成「?」，游戏内 AI 认不出「@小木」。
var launchJVMArgs = []string{"-Dfile.encoding=UTF-8", "-Dstdout.encoding=UTF-8", "-Dstderr.encoding=UTF-8"}

// ---------- 小工具 ----------

func (e *env) mods() string { return filepath.Join(e.inst, "mods") }

func (e *env) status() *companion.View {
	v, err := e.comp.Status(e.name)
	if err != nil {
		e.rep.logf("Status 出错：%v", err)
		return &companion.View{}
	}
	return v
}

func (e *env) compByName(n string) (companion.Companion, bool) {
	for _, c := range e.status().Companions {
		if c.Name == n {
			return c, true
		}
	}
	return companion.Companion{}, false
}

func (e *env) waitLive(d time.Duration) bool {
	return waitFor(d, func() bool { return e.status().Link.State == "live" })
}

// latest.log（UTF-8；控制台输出的编码要看 JVM 参数）
func (e *env) logPath() string { return filepath.Join(e.inst, "logs", "latest.log") }

func (e *env) logSize() int64 {
	st, err := os.Stat(e.logPath())
	if err != nil {
		return 0
	}
	return st.Size()
}

func (e *env) logSince(off int64) string {
	b, err := os.ReadFile(e.logPath())
	if err != nil || off > int64(len(b)) {
		return ""
	}
	return string(b[off:])
}

func (e *env) waitLog(off int64, d time.Duration, re *regexp.Regexp) (string, bool) {
	var hit string
	ok := waitFor(d, func() bool {
		for _, l := range strings.Split(e.logSince(off), "\n") {
			if re.MatchString(l) {
				hit = strings.TrimSpace(l)
				return true
			}
		}
		return false
	})
	return hit, ok
}

func grepLines(text string, re *regexp.Regexp) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if re.MatchString(l) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// cmdExpect 往控制台发一条命令，等输出里出现匹配 re 的一行。
func (e *env) cmdExpect(cmd string, re *regexp.Regexp, d time.Duration) (string, bool) {
	mark := e.srv.Mark()
	if err := e.srv.Command(cmd, ""); err != nil {
		return err.Error(), false
	}
	var hit string
	ok := waitFor(d, func() bool {
		for _, l := range e.srv.LinesFrom(mark) {
			if !strings.HasPrefix(l, "> ") && re.MatchString(l) { // 「> 命令」是 MineDeck 自己的回显
				hit = l
				return true
			}
		}
		return false
	})
	return hit, ok
}

func (e *env) cmd(c string) { _ = e.srv.Command(c, "") }

func eventsOf(v *companion.View, kind string) []companion.EventView {
	var out []companion.EventView
	for _, ev := range v.Events {
		if ev.Event == kind {
			out = append(out, ev)
		}
	}
	return out
}

func hasEventText(v *companion.View, sub string) (string, bool) {
	for _, ev := range v.Events {
		if strings.Contains(ev.Text, sub) {
			return ev.Text, true
		}
	}
	return "", false
}

func fileSHA(path string) (size int64, s256, s512 string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, "", "", err
	}
	a := sha256.Sum256(b)
	c := sha512.Sum512(b)
	return int64(len(b)), hex.EncodeToString(a[:]), hex.EncodeToString(c[:]), nil
}

func listMods(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, en := range ents {
		if !en.IsDir() {
			out = append(out, en.Name())
		}
	}
	sort.Strings(out)
	return out
}

func depOf(j companionjar.Jar, id string) companionjar.Dependency {
	for _, d := range j.Dependencies {
		if d.ModID == id {
			return d
		}
	}
	return companionjar.Dependency{}
}

func sameStrings(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}

// ---------- 1 安装 ----------

func (e *env) stepInstall() {
	pe, arch := depOf(e.jar, "playerengine"), depOf(e.jar, "architectury")
	pv, err := e.comp.PreviewInstall(e.name)
	ok := err == nil && slices.Equal(pv.Write, []string{e.jar.InstallName}) && sameStrings(pv.Download, []string{pe.Modrinth.Tested.File, arch.Modrinth.Tested.File}) &&
		len(pv.Remove) == 0 && pv.Bytes == pe.Modrinth.Tested.Size+arch.Modrinth.Tested.Size && !pv.Noop && !pv.Running
	if err != nil {
		e.rep.check("I1a", "安装预览（干净的实例）", false, "%v", err)
	} else {
		e.rep.check("I1a", "安装预览（干净的实例）", ok, "写 %v，下载 %v（%d 字节），删 %v", pv.Write, pv.Download, pv.Bytes, pv.Remove)
	}
	t0 := time.Now()
	res, err := e.comp.Install(e.ctx, e.name)
	dt := time.Since(t0)
	if err != nil {
		e.rep.check("I1b", "安装（伙伴模组写出来，两个依赖从真 Modrinth 下载）", false, "%v", err)
		return
	}
	e.rep.check("I1b", "安装（伙伴模组写出来，两个依赖从真 Modrinth 下载）", len(res.Installed) == 3 && len(res.Removed) == 0 && len(res.Fallback) == 0 &&
		res.Install.Ready && !res.Install.Needed && !res.Restart, "装了 %v，用时 %.1f 秒，回退 %v", res.Installed, dt.Seconds(), res.Fallback)
	// 逐个核对：伙伴模组和内置的逐字节相同（sha256），依赖的大小、sha512、mods.toml 版本和清单里实测过的一样
	var notes []string
	good := true
	if size, s256, _, err := fileSHA(filepath.Join(e.mods(), e.jar.InstallName)); err != nil || size != e.jar.Size || s256 != e.jar.SHA256 {
		good = false
		notes = append(notes, fmt.Sprintf("伙伴模组不对：%d %s %v", size, s256, err))
	} else {
		notes = append(notes, fmt.Sprintf("%s %d 字节 sha256 %s…", e.jar.InstallName, size, s256[:12]))
	}
	for _, d := range []companionjar.Dependency{pe, arch} {
		t := d.Modrinth.Tested
		p := filepath.Join(e.mods(), t.File)
		size, _, s512, err := fileSHA(p)
		mi := modinfo.Read(p)
		if err != nil || size != t.Size || s512 != t.SHA512 || mi.Version != t.ModVersion {
			good = false
			notes = append(notes, fmt.Sprintf("%s 不对：%d %s… %s %v", t.File, size, s512[:min(12, len(s512))], mi.Version, err))
			continue
		}
		notes = append(notes, fmt.Sprintf("%s %d 字节 sha512 %s… mods.toml %s", t.File, size, s512[:12], mi.Version))
	}
	e.rep.check("I1c", "装好的文件和清单逐项核对", good, "%s；mods：%v", strings.Join(notes, "；"), listMods(e.mods()))

	// 再装一次：什么都不做
	mt := map[string]time.Time{}
	for _, f := range listMods(e.mods()) {
		if st, err := os.Stat(filepath.Join(e.mods(), f)); err == nil {
			mt[f] = st.ModTime()
		}
	}
	pv2, err := e.comp.PreviewInstall(e.name)
	res2, err2 := e.comp.Install(e.ctx, e.name)
	if err != nil || err2 != nil {
		e.rep.check("I2", "再装一次什么都不做", false, "%v %v", err, err2)
		return
	}
	same := true
	for f, t := range mt {
		if st, err := os.Stat(filepath.Join(e.mods(), f)); err != nil || !st.ModTime().Equal(t) {
			same = false
		}
	}
	e.rep.check("I2", "再装一次什么都不做", err == nil && err2 == nil && pv2.Noop && len(res2.Installed) == 0 && len(res2.Removed) == 0 && same &&
		len(listMods(e.mods())) == 3, "Noop=%v，装了 %v，删了 %v，文件的修改时间不变=%v", pv2 != nil && pv2.Noop, res2.Installed, res2.Removed, same)

	// 旧版伙伴模组（原型 0.1.0，取自 git 历史）：换掉
	oldName, oldBytes, err := e.oldCompanion()
	if err != nil {
		e.rep.check("I3", "替换旧版伙伴模组", false, "取不到旧版：%v", err)
	} else {
		_ = os.Remove(filepath.Join(e.mods(), e.jar.InstallName))
		_ = os.WriteFile(filepath.Join(e.mods(), oldName), oldBytes, 0o644)
		iv := e.status().Install
		st := ""
		if len(iv.Mods) > 0 {
			st = iv.Mods[0].State + " " + iv.Mods[0].Version
		}
		pv3, err := e.comp.PreviewInstall(e.name)
		res3, err2 := e.comp.Install(e.ctx, e.name)
		if err != nil || err2 != nil {
			e.rep.check("I3", "替换旧版伙伴模组", false, "%v %v", err, err2)
			return
		}
		_, err3 := os.Stat(filepath.Join(e.mods(), oldName))
		size, s256, _, _ := fileSHA(filepath.Join(e.mods(), e.jar.InstallName))
		e.rep.check("I3", "替换旧版伙伴模组", err == nil && err2 == nil && st == "outdated 0.1.0" && slices.Equal(pv3.Write, []string{e.jar.InstallName}) &&
			slices.Equal(pv3.Remove, []string{oldName}) && slices.Equal(res3.Removed, []string{oldName}) && os.IsNotExist(err3) &&
			size == e.jar.Size && s256 == e.jar.SHA256, "装之前 %s；预览写 %v 删 %v；装完删了 %v，mods：%v", st, pv3.Write, pv3.Remove, res3.Removed, listMods(e.mods()))
	}
	e.stepInstallInRange(arch)
}

// swapModJar 用指定的 jar 换掉装好的伙伴模组（-mod-jar：拿别的版本对比，比如修好之前的 0.2.0）。
func (e *env) swapModJar(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, f := range listMods(e.mods()) {
		if strings.HasPrefix(f, companionjar.FilePrefix) {
			_ = os.Remove(filepath.Join(e.mods(), f))
		}
	}
	if err := os.WriteFile(filepath.Join(e.mods(), filepath.Base(path)), b, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	e.rep.logf("用 %s（%d 字节，sha256 %s…）换掉了内置的伙伴模组，只为对比；mods：%v", filepath.Base(path), len(b), hex.EncodeToString(sum[:])[:12], listMods(e.mods()))
	return nil
}

// oldCompanion 从 git 历史里取原型 0.1.0 的 jar（同一个加载器、MC 版本）。
func (e *env) oldCompanion() (string, []byte, error) {
	raw, err := exec.Command("git", "-C", e.o.repo, "show", e.o.oldRev+":internal/companion/jar/manifest.json").Output()
	if err != nil {
		return "", nil, err
	}
	var m companionjar.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, err
	}
	j, ok := m.Find(e.p.core, e.p.mc)
	if !ok {
		return "", nil, errors.New("旧清单里没有这个加载器")
	}
	b, err := exec.Command("git", "-C", e.o.repo, "show", e.o.oldRev+":internal/companion/jar/"+j.File).Output()
	if err != nil {
		return "", nil, err
	}
	return j.InstallName, b, nil
}

// stepInstallInRange 依赖已经装着别的、范围内的版本（整合包里自带 Architectury 的情况）：不动它；同时启用了两份时只留实测过的。
func (e *env) stepInstallInRange(d companionjar.Dependency) {
	name, data, ver, err := e.fetchOtherVersion(d)
	if err != nil {
		e.rep.skip("I4", "依赖装着范围内的别的版本", err.Error())
		return
	}
	tested := d.Modrinth.Tested.File
	// 两份都在：留实测过的，删掉另一份
	_ = os.WriteFile(filepath.Join(e.mods(), name), data, 0o644)
	pv, err := e.comp.PreviewInstall(e.name)
	res, err2 := e.comp.Install(e.ctx, e.name)
	if err != nil || err2 != nil {
		e.rep.check("I4", "同一个依赖启用了两份：只留实测过的", false, "%v %v", err, err2)
		return
	}
	_, gone := os.Stat(filepath.Join(e.mods(), name))
	e.rep.check("I4", "同一个依赖启用了两份：只留实测过的", err == nil && err2 == nil && slices.Equal(pv.Remove, []string{name}) && len(pv.Download) == 0 &&
		slices.Equal(res.Removed, []string{name}) && os.IsNotExist(gone), "多放了 %s（%s）；预览删 %v；装完 mods：%v", name, ver, pv.Remove, listMods(e.mods()))
	// 只有范围内的别的版本：不动它（不自动追新，也不换回实测过的）
	_ = os.Remove(filepath.Join(e.mods(), tested))
	_ = os.WriteFile(filepath.Join(e.mods(), name), data, 0o644)
	iv := e.status().Install
	st := ""
	for _, m := range iv.Mods {
		if m.ID == d.ModID {
			st = m.State + " " + m.Version
		}
	}
	pv2, err := e.comp.PreviewInstall(e.name)
	e.rep.check("I5", "依赖装着范围内的别的版本：不动它", err == nil && pv2.Noop && iv.Ready && st == "inrange "+ver, "状态 %s，Ready=%v，Noop=%v %v", st, iv.Ready, pv2 != nil && pv2.Noop, err)
	// 换回实测过的（再从 Modrinth 下一次）
	_ = os.Remove(filepath.Join(e.mods(), name))
	res3, err := e.comp.Install(e.ctx, e.name)
	if err != nil {
		e.rep.check("I6", "删掉以后重新安装实测过的版本", false, "%v", err)
		return
	}
	e.rep.check("I6", "删掉以后重新安装实测过的版本", slices.Equal(res3.Installed, []string{tested}) && len(listMods(e.mods())) == 3,
		"装了 %v，mods：%v", res3.Installed, listMods(e.mods()))
}

// fetchOtherVersion 测试夹具：在 Modrinth 上找这个依赖的另一个 release（符合 versionPattern、mods.toml 版本在范围内、不是实测过的那个），下载并核对 sha512。
func (e *env) fetchOtherVersion(d companionjar.Dependency) (file string, data []byte, ver string, err error) {
	r := d.Modrinth
	lj, _ := json.Marshal(r.Loaders)
	gj, _ := json.Marshal(r.GameVersions)
	u := fmt.Sprintf("%s/project/%s/version?loaders=%s&game_versions=%s", companion.ModrinthAPI, r.ProjectID, url.QueryEscape(string(lj)), url.QueryEscape(string(gj)))
	body, err := httpGet(e.ctx, u)
	if err != nil {
		return "", nil, "", err
	}
	var vs []struct {
		VersionNumber string `json:"version_number"`
		VersionType   string `json:"version_type"`
		Files         []struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
			Primary  bool   `json:"primary"`
			Hashes   struct {
				SHA512 string `json:"sha512"`
			} `json:"hashes"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &vs); err != nil {
		return "", nil, "", err
	}
	re := regexp.MustCompile(r.VersionPattern)
	for _, v := range vs {
		if v.VersionType != "release" || v.VersionNumber == r.Tested.VersionNumber || !re.MatchString(v.VersionNumber) || len(v.Files) == 0 {
			continue
		}
		f := v.Files[0]
		for _, x := range v.Files {
			if x.Primary {
				f = x
			}
		}
		b, err := httpGet(e.ctx, f.URL)
		if err != nil {
			continue
		}
		s := sha512.Sum512(b)
		if hex.EncodeToString(s[:]) != f.Hashes.SHA512 {
			continue
		}
		tmp := filepath.Join(e.dir, "other-"+f.Filename)
		_ = os.WriteFile(tmp, b, 0o644)
		mi := modinfo.Read(tmp)
		if mi.ID != d.ModID || !companion.InRange(mi.Version, d.VersionRange) {
			continue
		}
		return f.Filename, b, mi.Version, nil
	}
	return "", nil, "", fmt.Errorf("Modrinth 上没有 %s 的别的、范围（%s）内的正式版", d.Name, d.VersionRange)
}

func httpGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", dl.UA)
	resp, err := dl.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// ---------- 2 连接 ----------

func (e *env) stepConnect() bool {
	// 端口先写好（测试只用 25600–25699）；其余的由「打开伙伴」补上：EnsureConfig 保留合规的端口，补 enabled 和令牌
	cfgPath := companion.ConfigPath(e.inst)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	_ = os.WriteFile(cfgPath, []byte(fmt.Sprintf("port=%d\n", e.p.ctlPort)), 0o644)
	on := true
	_, err := e.comp.SetSettings(e.name, companion.SettingsPatch{Enabled: &on})
	cfg, _ := companion.ReadConfig(e.inst)
	e.rep.check("C1", "打开伙伴：写好配置（端口、令牌）", err == nil && cfg.Enabled && cfg.Port == e.p.ctlPort && companion.ValidToken(cfg.Token) && len(cfg.Token) == 32,
		"enabled=%v port=%d 令牌 %d 位；err=%v", cfg.Enabled, cfg.Port, len(cfg.Token), err)
	zero, steve := 0, []string{"Steve"}
	if _, err := e.gc.Update(e.name, chatai.Patch{Enabled: &on, Allowed: &steve, Cooldown: &zero}); err != nil {
		e.rep.check("C1b", "打开游戏内 AI、授权 Steve", false, "%v", err)
		return false
	}

	t0 := time.Now()
	if err := e.srv.Start(); err != nil {
		e.rep.check("C2", "开服", false, "%v", err)
		return false
	}
	if !waitFor(8*time.Minute, func() bool { return e.srv.Status() == "running" || e.srv.Status() == "stopped" }) || e.srv.Status() != "running" {
		e.rep.check("C2", "开服", false, "等不到 Done（状态 %s），看 console.log", e.srv.Status())
		return false
	}
	tDone := time.Since(t0)
	live := e.waitLive(60 * time.Second)
	tLive := time.Since(t0) - tDone
	v := e.status()
	e.rep.check("C2", "开服到 Done 后自动连上伙伴模组", live, "开服 %.1f 秒；Done 之后 %.1f 秒连上（%s，状态 %s %s）", tDone.Seconds(), tLive.Seconds(), v.Link.Addr, v.Link.State, v.Link.Error)
	if !live {
		return false
	}
	pe := depOf(e.jar, "playerengine")
	need := append([]string{"spawn", "despawn"}, companion.ActionOps...)
	opsOK := true
	for _, op := range need {
		opsOK = opsOK && slices.Contains(v.Link.Ops, op)
	}
	conn, _ := hasEventText(v, "连上了伙伴模组")
	e.rep.check("C3", "状态里有模组和 PlayerEngine 的版本", v.Link.Mod == e.man.Mod.Version && v.Link.PlayerEngine == pe.Modrinth.Tested.ModVersion && v.Link.Protocol == 1 &&
		opsOK && slices.Contains(v.Link.Features, "nodig") && slices.Contains(v.Link.Features, "adopt") && v.Settings.Applied != nil && !v.Settings.Pending &&
		v.Install.Ready, "模组 %s，PlayerEngine %s，协议 %d，session %s，features %v；事件「%s」", v.Link.Mod, v.Link.PlayerEngine, v.Link.Protocol, v.Link.Session, v.Link.Features, conn)
	e.session = v.Link.Session
	hit, ok := e.waitLog(0, 5*time.Second, regexp.MustCompile(`控制连接已建立：「?MineDeck companione2e`))
	e.rep.check("C4", "模组日志里有这次的控制连接", ok, "%s", clip(hit, 160))

	// 两个玩家：Steve（授权了）、Alex（没授权）
	for _, n := range []string{"Steve", "Alex"} {
		b, err := startBot(e.o.node, filepath.Join(e.o.repo, "tools", "companione2e", "bot.js"), e.o.nodeModules, e.p.gamePort, n, e.p.botVersion, e.p.botMode,
			filepath.Join(e.dir, "bot-"+n+".log"))
		if err != nil {
			e.rep.check("C5", "玩家进服", false, "起不了 %s：%v", n, err)
			return false
		}
		e.bots[n] = b
	}
	var spawned []string
	for n, b := range e.bots {
		if _, ok := b.WaitEv("spawn", 90*time.Second); ok {
			spawned = append(spawned, n)
		} else {
			e.rep.logf("%s 没进服：%s", n, b.Problems())
		}
	}
	online := waitFor(20*time.Second, func() bool { return len(e.srv.OnlinePlayers()) == 2 })
	e.rep.check("C5", fmt.Sprintf("玩家进服（%s 模式）", e.p.botMode), len(spawned) == 2 && online, "进服 %v，在线 %v", spawned, e.srv.OnlinePlayers())
	if len(spawned) < 2 {
		return true
	}
	for _, c := range []string{"time set day", "gamerule doDaylightCycle false", "gamerule doWeatherCycle false", "weather clear"} {
		e.cmd(c)
	}
	return true
}

// ---------- 3 管理（界面上的按钮：companion.Service 的接口）----------

func (e *env) stepManage() {
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)
	defer cancel()
	var ids []int
	for _, r := range []companion.SpawnReq{{Name: "小木", Owner: "Steve", Near: "Steve"}, {Name: "小石", Owner: "Alex", Near: "Alex"}, {Name: "小风"}} {
		c, err := e.comp.Spawn(ctx, e.name, r)
		if err != nil {
			e.rep.check("M1", "刷出伙伴", false, "刷出 %s：%v", r.Name, err)
			return
		}
		ids = append(ids, c.ID)
	}
	waitFor(5*time.Second, func() bool { return len(e.status().Companions) == 3 })
	v := e.status()
	var names []string
	for _, c := range v.Companions {
		names = append(names, fmt.Sprintf("%d:%s(主人 %s，%s，血量 %.0f，%s %.1f %.1f %.1f)", c.ID, c.Name, c.Owner, c.State, c.Health, strings.TrimPrefix(c.Dim, "minecraft:"), c.X, c.Y, c.Z))
	}
	ev, _ := hasEventText(v, "刷出了伙伴「小木」")
	e.rep.check("M1", "刷出 3 个伙伴", slices.Equal(ids, []int{1, 2, 3}) && len(v.Companions) == 3 && ev != "", "%s；事件「%s」", strings.Join(names, "；"), ev)
	_, err := e.comp.Spawn(ctx, e.name, companion.SpawnReq{Name: "小木"})
	var oe *companion.OpError
	e.rep.check("M2", "重名被模组拒绝", errors.As(err, &oe) && oe.Code == companion.CodeNameTaken, "%v", err)

	// 停下一个（小石刚刷出来，可能还没 tick 过）
	a, err := e.actReady(ctx, "follow", map[string]any{"companion": "小石", "player": "Alex"})
	if err != nil || !a.OK || a.Task == nil {
		e.rep.check("M3", "停下一个伙伴", false, "follow 没开始：%v %+v", err, a)
		return
	}
	time.Sleep(time.Second)
	stopped, err := e.comp.StopTasks(ctx, e.name, companion.StopReq{Companion: ids[1]})
	e.rep.check("M3", "停下一个伙伴", err == nil && slices.Equal(stopped, []int64{a.Task.ID}), "小石的任务 %d，停下了 %v %v", a.Task.ID, stopped, err)
	// 全部停下
	a2, _ := e.comp.Act(ctx, e.name, "follow", map[string]any{"companion": "小石", "player": "Alex"}, companion.Origin{})
	a3, _ := e.comp.Act(ctx, e.name, "follow", map[string]any{"companion": "小风", "player": "Alex"}, companion.Origin{})
	time.Sleep(time.Second)
	stopped, err = e.comp.StopTasks(ctx, e.name, companion.StopReq{All: true})
	var want []int64
	for _, x := range []*companion.ActResult{a2, a3} {
		if x != nil && x.Task != nil {
			want = append(want, x.Task.ID)
		}
	}
	idle := waitFor(8*time.Second, func() bool {
		for _, c := range e.status().Companions {
			if c.Task != nil {
				return false
			}
		}
		return true
	})
	cancelled, _ := hasEventText(e.status(), "停下了")
	sort.Slice(stopped, func(i, j int) bool { return stopped[i] < stopped[j] })
	e.rep.check("M4", "全部停下", err == nil && len(want) == 2 && slices.Equal(stopped, want) && idle, "停下了 %v（应为 %v），都空闲了=%v；事件「%s」", stopped, want, idle, cancelled)

	// 改设置：上限、attack_deny、禁挖区域，模组回 config_applied
	applied := func(d time.Duration, f func(companion.ModConfig) bool) (time.Duration, bool) {
		t0 := time.Now()
		ok := waitFor(d, func() bool {
			v := e.status()
			return v.Settings.Applied != nil && f(*v.Settings.Applied) && !v.Settings.Pending
		})
		return time.Since(t0), ok
	}
	two := 2
	_, err = e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{MaxCompanions: &two}})
	dt, ok := applied(10*time.Second, func(c companion.ModConfig) bool { return c.MaxCompanions == 2 })
	ev, _ = hasEventText(e.status(), "伙伴模组读到了新的配置")
	_, err2 := e.comp.Spawn(ctx, e.name, companion.SpawnReq{Name: "小四"})
	e.rep.check("M5", "改伙伴上限 → config_applied，超上限被拒", err == nil && ok && ev != "" && errors.As(err2, &oe) && oe.Code == companion.CodeLimit,
		"%.1f 秒后模组在用 2；事件「%s」；刷第 4 个：%v", dt.Seconds(), ev, err2)
	deny := []string{"minecraft:villager", "cow"}
	_, err = e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{AttackDeny: &deny}})
	dt, ok = applied(10*time.Second, func(c companion.ModConfig) bool {
		return sameStrings(c.AttackDeny, []string{"minecraft:villager", "minecraft:cow"})
	})
	at, aerr := e.comp.Act(ctx, e.name, "attack", map[string]any{"companion": "小木", "mob": "minecraft:cow"}, companion.Origin{})
	forbidden := aerr == nil && !at.OK && at.Code == companion.CodeForbidden
	_, err3 := e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{DenyDefault: true}})
	dt2, ok2 := applied(10*time.Second, func(c companion.ModConfig) bool { return sameStrings(c.AttackDeny, companion.DefaultAttackDeny) })
	atxt := ""
	if at != nil {
		atxt = at.Code + " " + at.Error
	}
	e.rep.check("M6", "改 attack_deny → config_applied，打牛被拒；恢复默认", err == nil && err3 == nil && ok && forbidden && ok2,
		"%.1f 秒后模组在用 villager、cow；打牛：%s；恢复默认 %.1f 秒", dt.Seconds(), atxt, dt2.Seconds())
	c1, _ := e.compByName("小木")
	x, z := int(math.Floor(c1.X)), int(math.Floor(c1.Z))
	zones := []companion.Zone{{Name: "base", Dim: "minecraft:overworld", Min: [3]int{x + 40, 100, z + 40}, Max: [3]int{x + 30, 40, z + 30}}}
	_, err = e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{Zones: &zones}})
	dt, ok = applied(10*time.Second, func(c companion.ModConfig) bool {
		return len(c.Zones) == 1 && c.Zones[0].Name == "base" && c.Zones[0].Min == [3]int{x + 30, 40, z + 30} && c.Zones[0].Max == [3]int{x + 40, 100, z + 40}
	})
	var got string
	if a := e.status().Settings.Applied; a != nil && len(a.Zones) > 0 {
		got = fmt.Sprintf("%s %v %v", a.Zones[0].Name, a.Zones[0].Min, a.Zones[0].Max)
	}
	none := []companion.Zone{}
	_, err3 = e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{Zones: &none}})
	_, ok2 = applied(10*time.Second, func(c companion.ModConfig) bool { return len(c.Zones) == 0 })
	e.rep.check("M7", "改禁挖区域（两个角反着写）→ config_applied，模组整理成 min/max；再清空", err == nil && err3 == nil && ok && ok2,
		"%.1f 秒后模组在用 %s（文件里写的是 %v %v），不再显示「还没读到」", dt.Seconds(), got, zones[0].Min, zones[0].Max)
	four := 4
	_, _ = e.comp.SetSettings(e.name, companion.SettingsPatch{Patch: companion.Patch{MaxCompanions: &four}})
	applied(10*time.Second, func(c companion.ModConfig) bool { return c.MaxCompanions == 4 })

	// 移除：小石带着 3 颗钻石、勾上「掉落背包」；小风带着 2 颗绿宝石、不勾
	c2, _ := e.compByName("小石")
	c3, _ := e.compByName("小风")
	e.cmd("item replace entity " + c2.UUID + " weapon.mainhand with minecraft:diamond 3")
	e.cmd("item replace entity " + c3.UUID + " weapon.mainhand with minecraft:emerald 2")
	time.Sleep(1500 * time.Millisecond)
	c2, _ = e.compByName("小石")
	c3, _ = e.compByName("小风")
	r1, err := e.comp.Despawn(ctx, e.name, companion.DespawnReq{Companion: ids[1], Drop: true})
	r2, err2 := e.comp.Despawn(ctx, e.name, companion.DespawnReq{Companion: ids[2]})
	time.Sleep(2 * time.Second)
	where := func(c companion.Companion, item string) string { // 东西在哪：地上（伙伴原地 6 格内），或者被旁边的玩家捡走了
		re := regexp.MustCompile(`Test passed|Test failed|Found \d+ matching|No items were found`)
		if hit, _ := e.cmdExpect(fmt.Sprintf("execute positioned %.1f %.1f %.1f if entity @e[type=minecraft:item,distance=..6,nbt={Item:{id:\"%s\"}}]", c.X, c.Y, c.Z, item), re, 5*time.Second); strings.Contains(hit, "passed") {
			return "地上"
		}
		for _, p := range []string{"Alex", "Steve"} {
			if hit, _ := e.cmdExpect("clear "+p+" "+item+" 0", re, 5*time.Second); strings.Contains(hit, "Found") {
				return p + " 捡走了"
			}
		}
		return "没有"
	}
	dia, eme := where(c2, "minecraft:diamond"), where(c3, "minecraft:emerald")
	waitFor(5*time.Second, func() bool { return len(e.status().Companions) == 1 })
	gone, _ := hasEventText(e.status(), "伙伴「小风」没了（MineDeck 移除的）")
	e.rep.check("M8", "移除（掉落背包 / 不掉落）", err == nil && err2 == nil && slices.Equal(r1, []int{ids[1]}) && slices.Equal(r2, []int{ids[2]}) && dia != "没有" && eme == "没有" &&
		len(e.status().Companions) == 1 && gone != "", "移除了 %v、%v；小石（掉落）的钻石：%s；小风（不掉落）的绿宝石：%s；事件「%s」", r1, r2, dia, eme, gone)
}

// ---------- 4 游戏内指挥 ----------

// fieldY 场地的高度：悬空，免得旁边有水（伙伴站在水里会淹死，模组已知的限制）、坡、别的树。
const fieldY = 100

// field 在 (x, z) 上空 fieldY 铺一块 29×29 的草地、上面清空 12 格，四周围 4 格高的屏障、草地下面再垫一层屏障（地形每次都一样；
// 屏障拆不掉，伙伴走不出去也掉不下去：PlayerEngine 的方块扫描没赶上看到新放的树时，会去找地面上远处的树，以前从玻璃墙那儿
// 下去过），两个玩家和伙伴都放到上面，放好两棵树，等 5 秒让 PlayerEngine 扫到。之后 goto 的目标、水箱也都在这块地里。
func (e *env) field(x, z int) {
	y := fieldY
	e.base = [3]int{x, y, z}
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:barrier", x-15, y-2, z-15, x+15, y-2, z+15))
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:grass_block", x-15, y-1, z-15, x+15, y-1, z+15))
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:air", x-15, y, z-15, x+15, y+12, z+15))
	for _, w := range [][4]int{{x - 15, z - 15, x + 15, z - 15}, {x - 15, z + 15, x + 15, z + 15}, {x - 15, z - 15, x - 15, z + 15}, {x + 15, z - 15, x + 15, z + 15}} {
		e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:barrier", w[0], y, w[1], w[2], y+3, w[3]))
	}
	e.cmd(fmt.Sprintf("tp Steve %d.5 %d %d.5", x-2, y, z+2))
	e.cmd(fmt.Sprintf("tp Alex %d.5 %d %d.5", x-4, y, z-4))
	e.home()
	e.rep.logf("场地中心 %v；放树：%s；%s", e.base, e.tree(0), e.tree(9))
	time.Sleep(5 * time.Second)
}

// home 把小木放回这块地的中心。
func (e *env) home() {
	if c, ok := e.compByName("小木"); ok {
		e.cmd(fmt.Sprintf("tp %s %d.5 %d %d.5", c.UUID, e.base[0], e.base[1], e.base[2]))
	}
}

// tree 在场地中心东边 6 格、南北偏 dz 格放一棵橡树（先铺草、清空）。
func (e *env) tree(dz int) string {
	x, y, z := e.base[0], e.base[1], e.base[2]+dz
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:grass_block", x+3, y-1, z-3, x+9, y-1, z+3))
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:air", x+3, y, z-3, x+9, y+10, z+3))
	hit, _ := e.cmdExpect(fmt.Sprintf("place feature minecraft:oak %d %d %d", x+6, y, z), regexp.MustCompile(`Placed|Failed to place`), 5*time.Second)
	return strings.TrimSpace(hit)
}

func (e *env) dist(c companion.Companion, b *bot) float64 {
	x, _, z, ok := b.Pos()
	if !ok {
		return -1
	}
	return math.Hypot(c.X-x, c.Z-z)
}

// pace 游戏内 AI 防刷屏（§6.7）：同一个人 1 分钟内触发超过 5 次就静默 10 分钟。测试连着问时等一等，不去碰它。
func (e *env) pace(who string) {
	e.mu.Lock()
	if e.asked == nil {
		e.asked = map[string][]time.Time{}
	}
	var keep []time.Time
	for _, t := range e.asked[who] {
		if time.Since(t) < 62*time.Second {
			keep = append(keep, t)
		}
	}
	var wait time.Duration
	if len(keep) >= 5 {
		wait = 62*time.Second - time.Since(keep[len(keep)-5])
	}
	e.mu.Unlock()
	if wait > 0 {
		e.rep.logf("（%s 一分钟内已经问了 5 次，等 %.0f 秒，免得被防刷屏静默）", who, wait.Seconds())
		time.Sleep(wait)
	}
	e.mu.Lock()
	e.asked[who] = append(keep, time.Now())
	e.mu.Unlock()
}

// ask 某人在聊天栏说一句话，等他收到以 head 开头的回复。返回回复、这次 AI 收到的请求。
func (e *env) ask(who, text, head string, d time.Duration) (string, []aiReq, bool) {
	e.pace(who)
	b := e.bots[who]
	mark, n := b.Mark(), e.fake.Count()
	if err := b.Chat(text); err != nil {
		return err.Error(), nil, false
	}
	reply, ok := b.WaitMsg(mark, d, func(m string) bool { return strings.HasPrefix(m, head) })
	time.Sleep(300 * time.Millisecond)
	return reply, e.fake.Requests()[n:], ok
}

func taskOf(c companion.Companion) string {
	if c.Task == nil {
		return "（没有任务）"
	}
	return fmt.Sprintf("#%d %s %s", c.Task.ID, c.Task.Op, string(c.Task.Args))
}

func (e *env) stepChat() {
	steve, alex := e.bots["Steve"], e.bots["Alex"]
	if steve == nil || alex == nil {
		e.rep.skip("H", "游戏内指挥", "玩家没进服")
		return
	}
	c, ok := e.compByName("小木")
	if !ok {
		if _, err := e.comp.Spawn(e.ctx, e.name, companion.SpawnReq{Name: "小木", Owner: "Steve", Near: "Steve"}); err != nil {
			e.rep.check("H0", "刷出小木", false, "%v", err)
			return
		}
		waitFor(5*time.Second, func() bool { _, ok := e.compByName("小木"); return ok })
		c, _ = e.compByName("小木")
	}
	e.field(int(math.Floor(c.X)), int(math.Floor(c.Z)))

	// 1 「@小木 跟着我」
	sx, sy, sz, _ := steve.Pos()
	e.cmd(fmt.Sprintf("tp Steve %.1f %.1f %.1f", sx-10, sy, sz))
	time.Sleep(1500 * time.Millisecond)
	c, _ = e.compByName("小木")
	d0 := e.dist(c, steve)
	amark := alex.Mark()
	reply, reqs, ok := e.ask("Steve", "@小木 跟着我", "[小木→Steve] ", 30*time.Second)
	var first aiReq
	if len(reqs) > 0 {
		first = reqs[0]
	}
	alexSaw := len(alex.Msgs(amark)) > 0
	waitFor(5*time.Second, func() bool { c, _ = e.compByName("小木"); return c.Task != nil && c.Task.Op == "follow" })
	args := companion.TaskArgs(c.Task)
	e.rep.check("H1a", "「@小木 跟着我」：以小木的名义回复，伙伴开始跟随", ok && slices.Contains(first.Tools, "companion_follow") && strings.Contains(first.User, "这句话是对伙伴「小木」说的") &&
		c.Task != nil && c.Task.Op == "follow" && args["player"] == "Steve" && alexSaw,
		"回复「%s」（全服可见，Alex 也收到=%v）；AI 第一轮的工具 %v；伙伴的任务 %s", reply, alexSaw, first.Tools, taskOf(c))
	t0 := time.Now()
	near := waitFor(40*time.Second, func() bool {
		c, _ = e.compByName("小木")
		d := e.dist(c, steve)
		return d >= 0 && d < 4
	})
	e.rep.check("H1b", "伙伴真的走到了 Steve 旁边", near, "距离 %.1f → %.1f 格，用了 %.1f 秒（PlayerEngine 任务 %s）", d0, e.dist(c, steve), time.Since(t0).Seconds(), c.EngineTask)

	// 2 「@小木 砍 3 个原木」：做完后私下通知 Steve（不调用 AI）
	smark, amark := steve.Mark(), alex.Mark()
	reply, reqs, ok = e.ask("Steve", "@小木 砍 3 个原木", "[小木→Steve] ", 30*time.Second)
	var collectArgs string
	if len(reqs) > 0 {
		collectArgs = reqs[0].Reply
	}
	waitFor(5*time.Second, func() bool { c, _ = e.compByName("小木"); return c.Task != nil && c.Task.Op == "collect" })
	task := taskOf(c)
	nAI := e.fake.Count()
	t0 = time.Now()
	note, got := steve.WaitMsg(smark, 4*time.Minute, func(m string) bool { return strings.HasPrefix(m, "[小木] 采集 oak_log ×3") })
	dt := time.Since(t0)
	time.Sleep(time.Second)
	alexNote := false
	for _, m := range alex.Msgs(amark) {
		alexNote = alexNote || strings.HasPrefix(m, "[小木] ")
	}
	followNote := false
	for _, m := range steve.Msgs(smark) {
		followNote = followNote || strings.HasPrefix(m, "[小木] 跟着")
	}
	e.rep.check("H2a", "「@小木 砍 3 个原木」：伙伴去砍，做完后 Steve 私下收到模板通知", ok && strings.Contains(collectArgs, `"minecraft:oak_log","count":3`) && got &&
		strings.Contains(note, "做完了") && !alexNote && !followNote,
		"回复「%s」；AI 调 %s；伙伴的任务 %s；%.1f 秒后通知「%s」；Alex 没收到=%v；被顶掉的跟随没通知=%v", reply, collectArgs, task, dt.Seconds(), note, !alexNote, !followNote)
	inv := e.inventory("小木")
	hist, _ := e.gc.History(e.name, 5)
	histOK := len(hist) > 0 && hist[0].Kind == "task" && hist[0].Companion == "小木" && hist[0].Usage == nil
	e.rep.check("H2b", "通知不调用 AI，不算次数；伙伴背包里有原木", e.fake.Count() == nAI && histOK && inv["minecraft:oak_log"] >= 3,
		"通知前后 AI 请求数 %d → %d；最近的问答第一条 kind=%s；背包 %v", nAI, e.fake.Count(), kindOf(hist), inv)

	// 3 「@AI 让伙伴停下」
	_, _, _ = e.ask("Steve", "@小木 跟着我", "[小木→Steve] ", 30*time.Second)
	waitFor(5*time.Second, func() bool { c, _ = e.compByName("小木"); return c.Task != nil && c.Task.Op == "follow" })
	followTask := taskOf(c)
	smark = steve.Mark()
	reply, reqs, ok = e.ask("Steve", "@AI 让伙伴停下", "[AI→Steve] ", 30*time.Second)
	stoppedOK := waitFor(6*time.Second, func() bool { c, _ = e.compByName("小木"); return c.Task == nil })
	time.Sleep(3 * time.Second)
	cancelNote := false
	for _, m := range steve.Msgs(smark) {
		cancelNote = cancelNote || strings.HasPrefix(m, "[小木] ")
	}
	var stopTools []string
	asComp := true
	if len(reqs) > 0 {
		stopTools, asComp = reqs[0].Tools, reqs[0].Companion
	}
	e.rep.check("H3", "「@AI 让伙伴停下」：伙伴停下，被停下的不通知", ok && slices.Contains(stopTools, "companion_stop") && !asComp && stoppedOK && !cancelNote,
		"停之前 %s；回复「%s」；停下后没有任务=%v；没收到通知=%v", followTask, reply, stoppedOK, !cancelNote)

	// 4 没授权的 Alex；5 带 /问
	for _, tc := range []struct{ id, who, text, head, title string }{
		{"H4", "Alex", "@小木 跟着我", "[小木→Alex] ", "没授权的玩家叫伙伴干活：不执行"},
		{"H5", "Steve", "@AI /问 让小木跟着我", "[AI→Steve] ", "带 /问 的指令：不执行"},
	} {
		started := len(eventsOf(e.status(), companion.EvTaskStarted))
		off := e.logSize()
		reply, reqs, ok := e.ask(tc.who, tc.text, tc.head, 30*time.Second)
		time.Sleep(3 * time.Second)
		c, _ = e.compByName("小木")
		var tools []string
		refused := false
		for i, r := range reqs {
			if i == 0 {
				tools = r.Tools
			}
			for _, res := range r.Results {
				refused = refused || strings.Contains(res, "没有这个工具：companion_follow")
			}
		}
		noTool := !slices.ContainsFunc(tools, func(t string) bool { return strings.HasPrefix(t, "companion_") || t == "run_command" })
		modTasks := grepLines(e.logSince(off), regexp.MustCompile(`的任务 #\d+ 开始`))
		e.rep.check(tc.id, tc.title, ok && noTool && refused && len(eventsOf(e.status(), companion.EvTaskStarted)) == started && len(modTasks) == 0 && c.Task == nil,
			"回复「%s」；请求里的工具只有 %v；模型硬调 companion_follow 被拦（「没有这个工具」）=%v；模组没开始任务=%v", reply, tools, refused, len(modTasks) == 0)
	}

	// 6 记账
	var compReqs, gameReqs int64
	for _, r := range e.fake.Requests() {
		if r.Companion {
			compReqs++
		} else {
			gameReqs++
		}
	}
	cu, _ := e.aisv.Usage(ai.FeatCompanion)
	gu, _ := e.aisv.Usage(ai.FeatGameChat)
	e.rep.check("H6a", "用量记账：叫着伙伴说的记在 AI 伙伴名下，@AI 的记在游戏内 AI 名下", cu.Requests == compReqs && cu.Tokens == compReqs*tokensPerReply &&
		gu.Requests == gameReqs && gu.Tokens == gameReqs*tokensPerReply, "AI 伙伴 %d 次请求 %d token，游戏内 AI %d 次 %d token（假 AI 收到 %d + %d 次）", cu.Requests, cu.Tokens, gu.Requests, gu.Tokens, compReqs, gameReqs)
	view, err := e.gc.View(e.name)
	if err == nil {
		e.rep.check("H6b", "游戏内 AI 的次数：受理一次算一次，通知不算", view.Today.Players["steve"] == 5 && view.Today.Players["alex"] == 1 && view.Today.Count == 6 &&
			view.Today.Tokens == (compReqs+gameReqs)*tokensPerReply, "今天 %d 次（%v），%d token", view.Today.Count, view.Today.Players, view.Today.Tokens)
	}
	recs, _ := e.reg.Log.Query(ops.Query{Op: "companion.action"})
	var acts []string
	for _, r := range recs {
		acts = append(acts, fmt.Sprintf("%s/%s/%v", r.Src, r.Actor, r.OK))
	}
	e.rep.check("H6c", "操作记录：每个伙伴动作一条 companion.action（来源 companion）", len(recs) == 4 && !slices.ContainsFunc(recs, func(r ops.Record) bool { return r.Src != ops.SrcCompanion || r.Actor != "Steve" }),
		"%d 条：%v", len(recs), acts)

	// 7 额度：AI 伙伴的每日额度用完；每人每天的次数用完
	_ = e.aisv.SetQuota(map[ai.Feature]int64{ai.FeatCompanion: cu.Tokens})
	n := e.fake.Count()
	reply, _, ok = e.ask("Steve", "@小木 你好", "[AI] ", 15*time.Second)
	blockedComp := ok && strings.Contains(reply, "AI 伙伴今天的额度用完了") && e.fake.Count() == n
	reply2, _, ok2 := e.ask("Steve", "@AI 你好", "[AI→Steve] ", 15*time.Second)
	_ = e.aisv.SetQuota(map[ai.Feature]int64{ai.FeatCompanion: 0})
	e.rep.check("H7a", "AI 伙伴的每日额度用完：叫伙伴不调用 AI，@AI 不受影响", blockedComp && ok2 && e.fake.Count() > n, "叫小木：「%s」；@AI：「%s」", reply, reply2)
	one, thirty := 1, 30
	_, _ = e.gc.Update(e.name, chatai.Patch{PerPlayer: &one})
	n = e.fake.Count()
	reply, _, ok = e.ask("Alex", "@小木 你好", "[AI] ", 15*time.Second)
	_, _ = e.gc.Update(e.name, chatai.Patch{PerPlayer: &thirty})
	e.rep.check("H7b", "每人每天的次数用完：不调用 AI", ok && strings.Contains(reply, "次已经用完了") && e.fake.Count() == n, "Alex：「%s」", reply)

	// 8 「@小木 给我 1 个原木」：player 不写就是提问的人；做完后通知
	smark = steve.Mark()
	before := e.countItem("Steve", "minecraft:oak_log")
	reply, _, ok = e.ask("Steve", "@小木 给我 1 个原木", "[小木→Steve] ", 30*time.Second)
	note, got = steve.WaitMsg(smark, 90*time.Second, func(m string) bool { return strings.HasPrefix(m, "[小木] 交出 oak_log ×1") })
	time.Sleep(2 * time.Second) // 扔过去的东西落地、被捡起来
	after := e.countItem("Steve", "minecraft:oak_log")
	e.rep.check("H8", "「@小木 给我 1 个原木」：伙伴把原木交给 Steve，做完后通知", ok && got && strings.Contains(note, "做完了") && after > before,
		"回复「%s」；通知「%s」；Steve 背包里的原木 %d → %d", reply, note, before, after)

	// 9 「@小木 去 X Z」
	gx, gz := e.base[0]-6, e.base[2]-6 // 在场地里
	smark = steve.Mark()
	reply, _, ok = e.ask("Steve", fmt.Sprintf("@小木 去 %d %d", gx, gz), "[小木→Steve] ", 30*time.Second)
	note, got = steve.WaitMsg(smark, 90*time.Second, func(m string) bool { return strings.HasPrefix(m, fmt.Sprintf("[小木] 去 (%d, %d)", gx, gz)) })
	c = e.fresh("小木") // STATUS 每 2 秒一次，可能还是走之前的位置：直接问模组
	e.rep.check("H9", "「@小木 去 X Z」：伙伴走过去，到了以后通知", ok && got && strings.Contains(note, "做完了") && math.Hypot(c.X-float64(gx)-0.5, c.Z-float64(gz)-0.5) < 2,
		"回复「%s」；通知「%s」；伙伴现在在 %.1f, %.1f", reply, note, c.X, c.Z)

	// 10 「@小木 跟大家打个招呼」：伙伴以自己的名字说话（§ 和换行去掉），全服都看得到
	amark = alex.Mark()
	reply, _, ok = e.ask("Steve", "@小木 跟大家打个招呼", "[小木→Steve] ", 30*time.Second)
	said, saw := alex.WaitMsg(amark, 10*time.Second, func(m string) bool { return strings.Contains(m, "大家好") })
	e.rep.check("H10", "「@小木 跟大家打个招呼」：伙伴在聊天栏说话，§ 格式码和换行去掉了", ok && saw && strings.Contains(said, "小木") && !strings.ContainsAny(said, "§\n"),
		"回复「%s」；Alex 看到「%s」", reply, said)

	// 11 「@小木 去打村民」：模组拒绝（forbidden），AI 照实转告，伙伴不动
	started := len(eventsOf(e.status(), companion.EvTaskStarted))
	reply, reqs, ok = e.ask("Steve", "@小木 去打村民", "[小木→Steve] ", 30*time.Second)
	forbidden := false
	for _, r := range reqs {
		for _, res := range r.Results {
			forbidden = forbidden || strings.Contains(res, "forbidden")
		}
	}
	e.rep.check("H11", "「@小木 去打村民」：模组拒绝（forbidden），AI 照实转告，没开始任务", ok && forbidden && len(eventsOf(e.status(), companion.EvTaskStarted)) == started,
		"回复「%s」；工具结果里有 forbidden=%v", reply, forbidden)

	// 12 give 一个都没交出去（模组 0.2.1 起报 failed，不再报 done）：接东西的 Alex 在下界，PlayerEngine 找不到他
	ax, ay, az, _ := alex.Pos()
	e.cmd("execute in minecraft:the_nether run tp Alex 0.5 129 0.5") // 下界顶上的基岩外面：安全，没有岩浆
	time.Sleep(3 * time.Second)
	smark = steve.Mark()
	reply, _, ok = e.ask("Steve", "@小木 给 Alex 1 个原木", "[小木→Steve] ", 30*time.Second)
	note, got = steve.WaitMsg(smark, 60*time.Second, func(m string) bool { return strings.HasPrefix(m, "[小木] 交出 oak_log ×1 给 Alex") })
	e.cmd(fmt.Sprintf("execute in minecraft:overworld run tp Alex %.1f %.1f %.1f", ax, ay, az))
	e.rep.check("H12", "give 一个都没交出去（Alex 在下界）：报失败，通知说清楚原因", ok && got && strings.Contains(note, "失败了") && strings.Contains(note, "交出了 0 个（要 1 个）") &&
		strings.Contains(note, "Alex 不在伙伴所在的维度"), "回复「%s」；Steve 收到「%s」", reply, note)

	// 13 give 背包里没有、也弄不到（fetch，PlayerEngine 不会弄基岩）：同样报失败
	smark = steve.Mark()
	reply, _, ok = e.ask("Steve", "@小木 给我 1 个基岩", "[小木→Steve] ", 30*time.Second)
	note, got = steve.WaitMsg(smark, 60*time.Second, func(m string) bool { return strings.HasPrefix(m, "[小木] 交出 bedrock ×1") })
	e.rep.check("H13", "give 背包里没有、也弄不到（fetch）：报失败，通知说清楚原因", ok && got && strings.Contains(note, "失败了") && strings.Contains(note, "交出了 0 个（要 1 个）") &&
		strings.Contains(note, "原来一个都没有，也没弄到"), "回复「%s」；Steve 收到「%s」", reply, note)
}

// countItem 玩家背包里有几个某物（clear <玩家> <物品> 0 只数不清）。
func (e *env) countItem(player, item string) int {
	hit, _ := e.cmdExpect("clear "+player+" "+item+" 0", regexp.MustCompile(`Found (\d+) matching|No items were found`), 5*time.Second)
	if m := regexp.MustCompile(`Found (\d+) matching`).FindStringSubmatch(hit); m != nil {
		var n int
		fmt.Sscan(m[1], &n)
		return n
	}
	return 0
}

func kindOf(h []chatai.Record) string {
	if len(h) == 0 {
		return ""
	}
	return h[0].Kind
}

// fresh 经伙伴模组的 status 指令取伙伴现在的样子（不等下一个 STATUS）。
// actReady 和 Act 一样，只是模组回 not_ready（伙伴刚刷出来、还没 tick 过，PlayerEngine 控制器还没建）时
// 隔 100 毫秒再试，最多 5 秒。服务器不忙时，刷出和紧跟着的指令会落在同一个 tick 间隙里（两个版本都这样）。
func (e *env) actReady(ctx context.Context, op string, args map[string]any) (*companion.ActResult, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := e.comp.Act(ctx, e.name, op, args, companion.Origin{})
		if err != nil || a == nil || a.OK || a.Code != companion.CodeNotReady || time.Now().After(deadline) {
			return a, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *env) fresh(name string) companion.Companion {
	res, err := e.comp.Act(e.ctx, e.name, "status", map[string]any{"companion": name}, companion.Origin{})
	if err != nil || !res.OK {
		return companion.Companion{}
	}
	var body struct {
		Companions []companion.Companion `json:"companions"`
	}
	_ = res.Reply.Decode(&body)
	if len(body.Companions) == 0 {
		return companion.Companion{}
	}
	return body.Companions[0]
}

// inventory 经伙伴模组的 status 取背包（物品 → 数量）。
func (e *env) inventory(name string) map[string]int {
	res, err := e.comp.Act(e.ctx, e.name, "status", map[string]any{"companion": name, "inventory": true}, companion.Origin{})
	out := map[string]int{}
	if err != nil || !res.OK {
		return out
	}
	var body struct {
		Companions []companion.Companion `json:"companions"`
	}
	_ = res.Reply.Decode(&body)
	for _, c := range body.Companions {
		for _, s := range c.Inventory {
			out[s.Item] += s.Count
		}
	}
	return out
}

// ---------- 空闲时在水里 ----------

// stepWater 停下来的伙伴放进 3 格深的水里（PlayerEngine 停用了 TaskRunner，它的防淹水不跑；两半联调时就是这样淹死的）：
// 模组 0.2.1 起空闲的伙伴自己浮上来。看 65 秒：还活着、不掉血、浮在水面、没离开水箱、没开始任务。
func (e *env) stepWater() {
	c, ok := e.compByName("小木")
	if !ok {
		e.rep.skip("W1", "空闲时在水里", "没有伙伴")
		return
	}
	if e.base == [3]int{} {
		e.field(int(math.Floor(c.X)), int(math.Floor(c.Z)))
	}
	x0, y, z0 := e.base[0]-10, e.base[1], e.base[2]+8
	// 玻璃水箱：里面 3×3、水深 3 格，上面敞着
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:glass", x0-1, y, z0-1, x0+3, y+3, z0+3))
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:air", x0, y, z0, x0+2, y+3, z0+2))
	e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:water", x0, y, z0, x0+2, y+2, z0+2))
	if _, err := e.comp.StopTasks(e.ctx, e.name, companion.StopReq{Companion: c.ID}); err != nil {
		e.rep.check("W1", "空闲时在水里", false, "停不下来：%v", err)
		return
	}
	time.Sleep(time.Second)
	start := e.fresh("小木")
	t0 := time.Now()
	e.cmd(fmt.Sprintf("tp %s %d.5 %d %d.5", c.UUID, x0+1, y, z0+1)) // 水底
	var ys []string
	alive := true
	var cur companion.Companion
	for time.Since(t0) < 65*time.Second {
		time.Sleep(5 * time.Second)
		cur = e.fresh("小木")
		if cur.ID == 0 {
			alive = false
			break
		}
		ys = append(ys, fmt.Sprintf("%.1f", cur.Y-float64(y)))
	}
	var died string
	for _, ev := range e.status().Events {
		if ev.T.After(t0) && (ev.Event == companion.EvDied || ev.Event == companion.EvRemoved) {
			died = ev.Text
		}
	}
	inTank := math.Abs(cur.X-float64(x0)-1.5) < 1.6 && math.Abs(cur.Z-float64(z0)-1.5) < 1.6
	e.rep.check("W1", "停下来的伙伴放进 3 格深的水里 65 秒：浮在水面上，不掉血，不乱走，没开始任务", alive && died == "" && cur.Health >= start.Health && cur.Y-float64(y) >= 1.5 &&
		inTank && cur.Task == nil, "血量 %.0f → %.0f；离水底的高度（每 5 秒）%v；还在水箱里=%v；任务 %s；死亡事件「%s」", start.Health, cur.Health, ys, inTank, taskOf(cur), died)
	// 拆掉水箱，清掉里面的掉落物：淹死时掉出来的背包会把后面去采集的伙伴引过去（它会去捡）
	for i := 0; i < 2; i++ {
		e.cmd(fmt.Sprintf("kill @e[type=minecraft:item,x=%d,y=%d,z=%d,dx=6,dy=6,dz=6]", x0-2, y-1, z0-2))
		if i == 0 {
			e.cmd(fmt.Sprintf("fill %d %d %d %d %d %d minecraft:air", x0-1, y, z0-1, x0+3, y+3, z0+3))
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !alive {
		// 淹死了（比如换成 0.2.0 对比时）：重新刷一个小木，后面几步照样有意义
		_, err := e.comp.Spawn(e.ctx, e.name, companion.SpawnReq{Name: "小木", Owner: "Steve", Near: "Steve"})
		waitFor(5*time.Second, func() bool { _, ok := e.compByName("小木"); return ok })
		c, _ = e.compByName("小木")
		e.rep.logf("小木淹死了，重新刷了一个（%d 号）给后面几步用：%v", c.ID, err)
		time.Sleep(time.Second)
	}
	e.home()
	time.Sleep(time.Second)
}

// ---------- 5 断线重连 ----------

func (e *env) iptables(op string) error {
	for _, dir := range []string{"--dport", "--sport"} {
		if out, err := exec.Command("iptables", op, "INPUT", "-i", "lo", "-p", "tcp", dir, fmt.Sprint(e.p.ctlPort), "-j", "DROP").CombinedOutput(); err != nil {
			return fmt.Errorf("iptables %s %s：%v %s", op, dir, err, out)
		}
	}
	return nil
}

func (e *env) block() error {
	if err := e.iptables("-I"); err != nil {
		return err
	}
	e.blocked = true
	return nil
}

func (e *env) unblock() {
	if err := e.iptables("-D"); err != nil {
		e.rep.logf("去掉 iptables 规则失败：%v", err)
	}
	e.blocked = false
}

func (e *env) stepReconnect() {
	steve := e.bots["Steve"]
	c, ok := e.compByName("小木")
	if steve == nil || !ok {
		e.rep.skip("R", "断线重连", "没有玩家或伙伴")
		return
	}
	e.home()
	time.Sleep(time.Second)
	e.rep.logf("小木回到场地中心 %v；放树：%s", e.base, e.tree(9))
	smark := steve.Mark()
	reply, _, ok := e.ask("Steve", "@小木 砍 3 个原木", "[小木→Steve] ", 30*time.Second)
	waitFor(5*time.Second, func() bool { c, _ = e.compByName("小木"); return c.Task != nil && c.Task.Op == "collect" })
	if !ok || c.Task == nil {
		e.rep.check("R1", "断线重连", false, "采集没开始：「%s」%s", reply, taskOf(c))
		return
	}
	taskID := c.Task.ID
	off := e.logSize()
	if err := e.block(); err != nil {
		e.rep.check("R1", "断线（iptables 丢掉控制端口的包）", false, "%v", err)
		return
	}
	t0 := time.Now()
	e.rep.logf("任务 #%d 开始后断开控制连接（iptables DROP 127.0.0.1:%d 两个方向）", taskID, e.p.ctlPort)
	down := waitFor(25*time.Second, func() bool { return e.status().Link.State == "retrying" })
	tDown := time.Since(t0)
	dtext := ""
	if evs := eventsOf(e.status(), "disconnected"); len(evs) > 0 {
		dtext = evs[0].Text // 新的在前
	}
	e.rep.check("R1", "断线后 MineDeck 发现并开始重连", down && strings.Contains(dtext, "心跳超时"), "%.1f 秒后状态 retrying；事件「%s」", tDown.Seconds(), dtext)
	fin, finOK := e.waitLog(off, 3*time.Minute, regexp.MustCompile(fmt.Sprintf(`的任务 #%d 结束`, taskID)))
	modDown, _ := e.waitLog(off, 30*time.Second, regexp.MustCompile(`控制连接已断开`))
	waitFor(20*time.Second, func() bool { return time.Since(t0) > 20*time.Second })
	early := false
	for _, m := range steve.Msgs(smark) {
		early = early || strings.HasPrefix(m, "[小木] 采集")
	}
	e.rep.check("R2", "断开期间任务在游戏里做完了（MineDeck 收不到 task_finished）", finOK && !early, "模组日志「%s」；「%s」；断开期间 Steve 没收到通知=%v",
		clip(fin, 120), clip(modDown, 120), !early)
	e.unblock()
	t1 := time.Now()
	back := e.waitLive(40 * time.Second)
	tBack := time.Since(t1)
	note, got := steve.WaitMsg(smark, 20*time.Second, func(m string) bool { return strings.HasPrefix(m, "[小木] 采集 oak_log ×3") })
	ev, _ := hasEventText(e.status(), "（断开期间结束的）")
	e.rep.check("R3", "恢复后自动重连，断开期间做完的任务照样通知 Steve", back && got && ev != "", "恢复网络后 %.1f 秒连上；通知「%s」；事件「%s」", tBack.Seconds(), note, ev)
}

// ---------- 改端口重连 ----------

func (e *env) setPort(p int) error {
	path := companion.ConfigPath(e.inst)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := regexp.MustCompile(`(?m)^port=\d+`).ReplaceAllString(string(b), fmt.Sprintf("port=%d", p))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (e *env) stepPortChange() {
	var notes []string
	good := true
	for _, p := range []int{e.p.ctlPort + 1, e.p.ctlPort} {
		n := len(eventsOf(e.status(), "connected"))
		t0 := time.Now()
		if err := e.setPort(p); err != nil {
			good = false
			notes = append(notes, err.Error())
			break
		}
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		ok := waitFor(30*time.Second, func() bool {
			v := e.status()
			return v.Link.State == "live" && v.Link.Addr == addr && len(eventsOf(v, "connected")) > n
		})
		dtext := ""
		if evs := eventsOf(e.status(), "disconnected"); len(evs) > 0 {
			dtext = evs[0].Text // 新的在前
		}
		good = good && ok && strings.Contains(dtext, "端口改了")
		notes = append(notes, fmt.Sprintf("改成 %d：%.1f 秒后在新端口上连上=%v（「%s」）", p, time.Since(t0).Seconds(), ok, dtext))
	}
	e.rep.check("P1", "改配置文件里的端口再改回来：自动重连", good, "%s", strings.Join(notes, "；"))
}

// ---------- 6 排错规则 ----------

func (e *env) stepDiag() {
	lines := e.srv.Lines()
	mods, _ := modinfo.ReadDir(e.mods())
	r := diag.Analyze(diag.Input{Lines: lines, Core: e.p.core, MC: e.p.mc, Mods: mods, Port: e.p.gamePort})
	dist := regexp.MustCompile(`Attempted to load class (\S+) for invalid dist DEDICATED_SERVER`)
	var distLines []string
	for _, l := range lines {
		if m := dist.FindStringSubmatch(l); m != nil {
			distLines = append(distLines, m[1])
		}
	}
	var rules []string
	clientOnly := false
	for _, f := range r.Findings {
		rules = append(rules, f.Rule+"（"+f.Title+"）")
		clientOnly = clientOnly || f.Rule == "client-only"
	}
	e.rep.check("D1", "装了 PlayerEngine 的服正常启动后，排错不报「客户端模组放进了服务端」", !clientOnly && len(distLines) > 0,
		"控制台 %d 行，PlayerEngine 的 dist 报错 %d 行（%v）；结论：%v", len(lines), len(distLines), distLines, rules)
}

// ---------- 退出 MineDeck、关服、重启 ----------

func (e *env) stepExit() {
	off := e.logSize()
	companion.ShutdownAll("MineDeck 已退出")
	hit, ok := e.waitLog(off, 10*time.Second, regexp.MustCompile(`MineDeck 结束了连接（MineDeck 已退出）`))
	e.rep.check("X1", "退出 MineDeck：模组收到 END", ok, "模组日志「%s」", clip(hit, 160))
	e.rawGive()
	// 重新连上（像重新打开 MineDeck 一样），后面关服要用
	e.comp.HandleEvent(events.Event{Type: "instance.start", Data: map[string]any{"instance": e.name}})
	e.rep.check("X2", "重新打开后再连上", e.waitLive(20*time.Second), "状态 %s", e.status().Link.State)

	// 服务器开着时在「伙伴」标签里关掉伙伴：先发 END，再改 enabled=false，模组关掉端口；再打开，自动连上
	off = e.logSize()
	no, yes := false, true
	v, err := e.comp.SetSettings(e.name, companion.SettingsPatch{Enabled: &no})
	end, ok1 := e.waitLog(off, 10*time.Second, regexp.MustCompile(`MineDeck 结束了连接（MineDeck 关掉了伙伴）`))
	closed, ok2 := e.waitLog(off, 10*time.Second, regexp.MustCompile(`伙伴控制端口已关闭`))
	state := ""
	if v != nil {
		state = v.Link.State
	}
	e.rep.check("X3", "服务器开着时关掉伙伴：模组收到 END、关掉端口", err == nil && ok1 && ok2 && state == "off", "模组日志「%s」「%s」；连接状态 %s", clip(end, 100), clip(closed, 100), state)
	t0 := time.Now()
	_, err = e.comp.SetSettings(e.name, companion.SettingsPatch{Enabled: &yes})
	e.rep.check("X4", "再打开：模组重新打开端口，自动连上", err == nil && e.waitLive(30*time.Second), "%.1f 秒后连上", time.Since(t0).Seconds())
}

// rawGive 绕开 MineDeck（它发 give 前会去掉 minecraft:），直接按协议发 give minecraft:oak_log：模组 0.2.1 自己也去掉，
// 交给 PlayerEngine 的是 give Steve oak_log 1，真的交出去了。MineDeck 的连接这时已经断开（只能有一个控制连接）。
func (e *env) rawGive() {
	var mu sync.Mutex
	var fin *companion.Event
	cl := companion.Start(companion.Options{Dir: e.inst, Name: "companione2e 原始协议",
		OnEvent: func(ev companion.Event) {
			if ev.Event == companion.EvTaskFinished {
				mu.Lock()
				x := ev
				fin = &x
				mu.Unlock()
			}
		}})
	defer cl.Stop("测完了")
	if !waitFor(15*time.Second, cl.Live) {
		e.rep.check("X1b", "按协议直接发 give minecraft:oak_log", false, "连不上：%s", cl.State().LastError)
		return
	}
	id := 0 // MineDeck 这时没连着，它的伙伴列表是空的：看这个连接自己的 WELCOME
	if w := cl.State().Welcome; w != nil {
		for _, c := range w.Companions {
			if c.Name == "小木" {
				id = c.ID
			}
		}
	}
	before := e.countItem("Steve", "minecraft:oak_log")
	resp, err := cl.Request(e.ctx, "give", map[string]any{"companion": id, "item": "minecraft:oak_log", "count": 1, "player": "Steve"})
	if err != nil || !resp.OK {
		e.rep.check("X1b", "按协议直接发 give minecraft:oak_log", false, "%v %+v", err, resp)
		return
	}
	var body struct {
		Task companion.Task `json:"task"`
	}
	_ = resp.Decode(&body)
	var f companion.Event
	done := waitFor(60*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		if fin != nil && fin.Task != nil && fin.Task.ID == body.Task.ID {
			f = *fin
			return true
		}
		return false
	})
	time.Sleep(2 * time.Second) // 扔过去的东西落地、被捡起来
	after := e.countItem("Steve", "minecraft:oak_log")
	iv := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	e.rep.check("X1b", "按协议直接发 give minecraft:oak_log：模组交给 PlayerEngine 的是 oak_log，真的交出去了", done && body.Task.Command == "give Steve oak_log 1" &&
		f.Result == companion.ResultDone && iv(f.Given) == 1 && iv(f.Want) == 1 && after > before,
		"任务的 PlayerEngine 指令「%s」；结果 %s，given %d、want %d、have %d；Steve 背包里的原木 %d → %d", body.Task.Command, f.Result, iv(f.Given), iv(f.Want), iv(f.Have), before, after)
}

func (e *env) stepStop() {
	for n, b := range e.bots {
		b.Quit()
		delete(e.bots, n)
	}
	if c, ok := e.compByName("小木"); ok {
		e.muID = c.ID
	}
	off := e.logSize()
	evN := len(e.status().Events)
	e.mu.Lock()
	logN := len(e.compLogs)
	e.mu.Unlock()
	dt, killed := e.srv.Stop(90 * time.Second)
	e.rep.check("S1", "关服：进程自己退出", !killed, "用时 %.1f 秒", dt.Seconds())
	waitFor(10*time.Second, func() bool { return e.status().Link.State == "waiting" })
	v := e.status()
	var evs []string
	for i := 0; i < len(v.Events)-evN && i < len(v.Events); i++ { // 新的在前
		evs = append(evs, v.Events[i].Event+"「"+v.Events[i].Text+"」")
	}
	modLines := grepLines(e.logSince(off), regexp.MustCompile(`控制连接已断开|MineDeck 结束了连接|伙伴控制端口`))
	modEnd := slices.ContainsFunc(modLines, func(l string) bool { return strings.Contains(l, "原因：服务器正在关闭") })
	warned := slices.ContainsFunc(v.Events[:max(0, len(v.Events)-evN)], func(ev companion.EventView) bool { return ev.Level == "warn" })
	e.mu.Lock()
	retried := 0
	for _, l := range e.compLogs[logN:] {
		if strings.Contains(l, "后重连") && !strings.Contains(l, "服务器正在关闭；") {
			retried++
		}
	}
	e.mu.Unlock()
	e.rep.check("S2", "关服：模组先发 END（服务器正在关闭），MineDeck 当成正常关服：不报「会自动重连」、不再重连", modEnd && !warned && retried == 0 && v.Link.State == "waiting",
		"模组日志：%s；MineDeck 的事件：%s；关服期间重连 %d 次；之后连接状态 %s", strings.Join(modLines, " / "), strings.Join(evs, "，"), retried, v.Link.State)
}

func (e *env) stepRestart() {
	prev := e.session
	t0 := time.Now()
	if err := e.srv.Start(); err != nil {
		e.rep.check("T1", "重启后接管伙伴", false, "%v", err)
		return
	}
	ok := waitFor(6*time.Minute, func() bool { return e.srv.Status() == "running" }) && e.waitLive(60*time.Second)
	welcomeHad := len(e.status().Companions)
	// 接管马上就看得到（模组 0.2.1 在主线程上生成 WELCOME）：要么在 WELCOME 里（「现有 1 个伙伴」），要么紧跟着收到 adopted 事件。
	// 只看事件，不看伙伴列表（列表也会被 STATUS 补上，那就分不出来了）
	var how string
	immediate := waitFor(5*time.Second, func() bool {
		for _, ev := range e.status().Events {
			if !ev.T.After(t0) {
				continue
			}
			switch {
			case ev.Event == "connected" && strings.Contains(ev.Text, "现有 1 个伙伴"):
				how = "WELCOME 里就有：「" + ev.Text + "」"
				return true
			case ev.Event == companion.EvAdopted:
				how = "WELCOME 之后收到 adopted：「" + ev.Text + "」"
				return true
			}
		}
		return false
	})
	if !immediate {
		for _, ev := range e.status().Events {
			if ev.T.After(t0) && ev.Event == "connected" {
				how = "只有「" + ev.Text + "」，没有 adopted 事件"
			}
		}
	}
	e.rep.check("T0", "重启后接管马上就看得到：在 WELCOME 里，或者紧跟着收到 adopted（不等 STATUS）", ok && immediate, "%s", how)
	var c companion.Companion
	var have bool
	waitFor(6*time.Second, func() bool { c, have = e.compByName("小木"); return have }) // WELCOME 读的快照可能比接管晚一个 tick：下一个 STATUS 就有了
	v := e.status()
	conn, _ := hasEventText(v, "现有 1 个伙伴")
	e.rep.check("T1", "重启后：重新连上，存档里的小木被接管（编号、主人不变），session 变了", ok && have && e.muID != 0 && c.ID == e.muID && c.Owner == "Steve" && v.Link.Session != prev && v.Link.Session != "",
		"session %s → %s；小木 %d 号（关服前 %d 号），主人 %s（刚连上时列表里 %d 个）；事件「%s」", prev, v.Link.Session, c.ID, e.muID, c.Owner, welcomeHad, conn)
	dt, killed := e.srv.Stop(90 * time.Second)
	e.rep.check("T2", "再关服", !killed, "用时 %.1f 秒", dt.Seconds())
}
