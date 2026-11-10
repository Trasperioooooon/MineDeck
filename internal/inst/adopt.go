package inst

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 接管已有的服务器：把用户自己以前开的服务器文件夹就地登记成实例，交给 MineDeck 启停和管理。
//
// 不搬文件、不改存档和配置。往文件夹里写的只有：
//   - instance.json：MineDeck 自己的记录
//   - run.bat：按这里的设置生成的启动脚本。原来有 run.bat 的，先另存为 run.before-minedeck.bat
//   - user_jvm_args.txt（只有 Forge / NeoForge）：内存由 MineDeck 管，原来的另存为 user_jvm_args.before-minedeck.txt，
//     里面别的 JVM 参数带到自定义 JVM 参数里
//   - eula.txt：原来没同意过、这次用户勾了同意时才写
//   - log4j2_112-116.xml：1.12 ~ 1.16.5 的 log4shell 缓解配置，和新建时一样
//
// 接管失败不删任何东西（这个文件夹是用户的，不是半成品），并且把这次动过的那几个文件恢复原样：
// 失败之后文件夹和接管之前一模一样。

// AdoptReq 接管请求。除了 Dir，其余是识别结果经用户确认、修改之后的值。
type AdoptReq struct {
	Dir          string     `json:"dir"`
	Name         string     `json:"name"`
	Core         mcsrc.Core `json:"core"`
	MC           string     `json:"mc"`
	Build        string     `json:"build"`
	LaunchTarget string     `json:"launchTarget"` // 必须是识别出来的候选之一
	XmxMB        int        `json:"xmxMb"`
	JavaMajor    int        `json:"javaMajor"` // 0 = 按 Minecraft 版本自动选
	JavaPath     string     `json:"javaPath"`  // 指定用哪一个 java.exe（调用方保证是已知的 Java）；空 = 本机找，没有就下载
	EULA         bool       `json:"eula"`
}

var adoptSteps = []string{"检查服务器文件夹", "准备 Java 运行时", "登记到 MineDeck"}

const (
	runBatBackup  = "run.before-minedeck.bat"
	jvmArgsBackup = "user_jvm_args.before-minedeck.txt"
)

// ScanAdopt 识别一个已有的服务器文件夹（只读），给界面预填。
func (m *Manager) ScanAdopt(dir string) (*AdoptScan, error) {
	dir, err := m.checkAdoptDir(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scanServerDir(dir)
	if err != nil {
		return nil, err
	}
	sc.Name = m.freeName(sc.Name)
	return sc, nil
}

// checkAdoptDir 这个文件夹能不能接管，返回清理后的绝对路径。
func (m *Manager) checkAdoptDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("请先选择服务器所在的文件夹")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("请填完整的路径（如 D:\\MyServer）")
	}
	dir = filepath.Clean(dir)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("找不到这个文件夹：%s", dir)
	}
	// 后面的比较都用真实路径：经目录联接（junction）、符号链接、subst 的盘符绕一圈进来的同一个文件夹，要认得出是同一个。
	// 登记成实例的目录仍然是用户选的那个路径
	real := realPath(dir)
	if dir == filepath.Dir(dir) || real == filepath.Dir(real) {
		return "", fmt.Errorf("不能选整个磁盘，请选服务器所在的那个文件夹")
	}
	if within(realPath(app.Base), real) {
		return "", fmt.Errorf("不能选 MineDeck 自己的程序目录（或它的上层）。服务器和 MineDeck.exe 放在同一个文件夹里的话，请先把 MineDeck.exe 挪到一个单独的文件夹")
	}
	for _, r := range []string{app.RuntimesDir, app.CacheDir} {
		if r != "" && within(real, realPath(r)) {
			return "", fmt.Errorf("这个文件夹是 MineDeck 自己用的（%s），不能当服务器接管", r)
		}
	}
	// 备份目录：在它里面的不行；把它包在里面的也不行——那样连文件夹一起删服务器时会把备份一起删掉
	for _, r := range app.AllBackupRoots() {
		if r == "" {
			continue
		}
		if rr := realPath(r); within(real, rr) || within(rr, real) {
			return "", fmt.Errorf("这个文件夹和 MineDeck 的备份目录（%s）叠在一起了，不能当服务器接管", r)
		}
	}
	// 新建的服务器都放在这两个目录下面：把它们（或它们的上层）当成一台服务器，以后新建的就建到这台服务器里面去了
	for _, r := range []string{app.ServersDir, app.ServersRoot()} {
		if r != "" && within(realPath(r), real) {
			return "", fmt.Errorf("这是存放服务器的总目录，请选到里面具体那一台服务器的文件夹")
		}
	}
	for _, i := range m.List() {
		if err := overlapErr(real, realPath(i.Dir), i.Name); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// overlapErr dir 和服务器 name 的目录 other 是同一个、或者一个套着另一个时，给出对应的报错（两个都传真实路径）。
func overlapErr(dir, other, name string) error {
	switch {
	case sameDir(other, dir):
		return fmt.Errorf("这个文件夹已经在列表里了，就是「%s」", name)
	case within(dir, other):
		return fmt.Errorf("这个文件夹在服务器「%s」的目录里面，不能单独接管", name)
	case within(other, dir):
		return fmt.Errorf("这个文件夹里面还放着服务器「%s」，请选到具体那一台服务器的文件夹", name)
	}
	return nil
}

// within child 是不是在 parent 里面（或者就是它）。Windows 上不分大小写。
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func sameDir(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}

// freeName 名称被占用时加序号，给出一个没人用的。
func (m *Manager) freeName(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	taken := func(n string) bool { _, ok := m.insts[n]; return ok || m.creating[n] }
	if !taken(name) {
		return name
	}
	for n := 2; n < 100; n++ {
		if c := fmt.Sprintf("%s %d", name, n); !taken(c) {
			return c
		}
	}
	return name
}

// dirKey 「正在新建 / 接管」占位用的键，记的是目录：同一个文件夹（和它的上层、下层）不能同时被两个任务用。
// 实例名里不能有冒号，不会和名字的占位撞上。
const dirKeyPrefix = "dir:"

func dirKey(dir string) string { return dirKeyPrefix + strings.ToLower(realPath(dir)) }

// AdoptAsync 校验请求并在后台接管，返回任务编号。
func (m *Manager) AdoptAsync(req AdoptReq) (string, error) {
	sc, err := m.validateAdopt(&req)
	if err != nil {
		return "", err
	}
	t, ctx := m.Tasks.New(fmt.Sprintf("接管服务器 %s（%s %s）", req.Name, req.Core, req.MC), adoptSteps)
	t.SetKind("adopt")
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(sc.key)
		if err := m.runAdopt(ctx, t, req, sc); err != nil {
			t.Fail(err) // 不清理目录：这是用户原来的文件夹（动过的文件 runAdopt 自己已经恢复原样）
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

// validateAdopt 校验接管请求（重新识别一遍文件夹，不信请求里带来的事实），通过后登记「创建中」占位。
func (m *Manager) validateAdopt(req *AdoptReq) (*AdoptScan, error) {
	req.Name = strings.TrimSpace(req.Name)
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if !mcsrc.ValidCore(req.Core) {
		return nil, fmt.Errorf("请选择服务端的类型")
	}
	req.MC, req.Build = strings.TrimSpace(req.MC), strings.TrimSpace(req.Build)
	if req.MC == "" || len(req.MC) > 40 || strings.ContainsAny(req.MC, "\r\n\t\"") {
		return nil, fmt.Errorf("请填写版本号（如 1.20.1）")
	}
	if len(req.Build) > 60 || strings.ContainsAny(req.Build, "\r\n\t\"") {
		return nil, fmt.Errorf("构建版本不对")
	}
	if req.JavaMajor != 0 && (req.JavaMajor < 8 || req.JavaMajor > 30) {
		return nil, fmt.Errorf("Java 版本应在 8 到 30 之间")
	}
	if req.JavaPath != "" {
		if req.JavaMajor == 0 {
			return nil, fmt.Errorf("指定了 Java 的路径，还要写明它是哪个版本")
		}
		// 是不是已知的 Java 由操作层查；这里再守一道：只能是 java.exe，路径里不能有会写坏 run.bat 的字符
		if base := strings.ToLower(filepath.Base(req.JavaPath)); (base != "java.exe" && base != "javaw.exe") || strings.ContainsAny(req.JavaPath, "\"\r\n") {
			return nil, fmt.Errorf("Java 的路径不对：%s", req.JavaPath)
		}
		if st, err := os.Stat(req.JavaPath); err != nil || st.IsDir() {
			return nil, fmt.Errorf("找不到 Java：%s", req.JavaPath)
		}
	}
	dir, err := m.checkAdoptDir(req.Dir)
	if err != nil {
		return nil, err
	}
	req.Dir = dir
	sc, err := scanServerDir(dir)
	if err != nil {
		return nil, err
	}
	ok := false
	for _, c := range sc.Targets {
		if c.Target == req.LaunchTarget {
			ok = true
		}
	}
	if !ok {
		return nil, fmt.Errorf("选的启动文件不在这个文件夹里，请重新识别一次")
	}
	kind := mcsrc.KindOf(req.Core)
	if kind != mcsrc.KindProxy && !sc.EULA && !req.EULA {
		return nil, fmt.Errorf("需要同意 Minecraft EULA 才能开服")
	}
	if (req.Core == mcsrc.CoreMohist || req.Core == mcsrc.CoreBanner) && !isASCII(dir) {
		return nil, fmt.Errorf("Mohist / Banner 不能放在带中文的路径下（现在是 %s）。请把文件夹挪到纯英文路径下再接管", dir)
	}
	if req.XmxMB < 512 {
		req.XmxMB = 2048
	}
	real := realPath(dir) // 在锁外算好：要问系统
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.insts[req.Name]; taken {
		return nil, fmt.Errorf("实例名已存在: %s", req.Name)
	}
	if m.creating[req.Name] {
		return nil, fmt.Errorf("「%s」正在创建中，请稍候", req.Name)
	}
	// 识别文件夹花了时间，这期间别的请求可能已经登记了同一个文件夹、或者它的上层、下层：拿着锁再查一遍。
	// 正在新建、正在接管的也算（它们的目录记在 creating 里，键是 dirKey）
	for _, i := range m.insts {
		if err := overlapErr(real, realPath(i.Dir), i.Name); err != nil {
			return nil, err
		}
	}
	for k := range m.creating {
		if other, ok := strings.CutPrefix(k, dirKeyPrefix); ok && (within(real, other) || within(other, real)) {
			return nil, fmt.Errorf("这个文件夹（或它的上层、下层）正在新建或接管中，请稍候")
		}
	}
	sc.key = dirKeyPrefix + strings.ToLower(real)
	m.creating[req.Name], m.creating[sc.key] = true, true
	return sc, nil
}

func (m *Manager) runAdopt(ctx context.Context, t *tasks.Task, req AdoptReq, sc *AdoptScan) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	dir := req.Dir
	kind := mcsrc.KindOf(req.Core)

	// ---- 步骤 0：检查文件夹，定下用哪个 Java ----
	t.StartStep(0)
	t.Logf("服务器文件夹：%s", dir)
	t.Logf("启动方式：%s", strings.TrimPrefix(strings.TrimPrefix(req.LaunchTarget, "jar:"), "args:"))
	known := false // 官方清单里有这个版本：有才能按版本选 Java、判断要不要加 log4shell 缓解
	if kind != mcsrc.KindProxy {
		var err error
		if known, err = mcsrc.KnownVersion(ctx, req.MC); err != nil {
			t.Logf("没拿到 Minecraft 版本清单（%v），按版本自动判断的事先跳过", err)
		}
	}
	major := req.JavaMajor
	switch {
	case major != 0:
		t.Logf("按你选的用 Java %d", major)
	case kind == mcsrc.KindProxy:
		major = mcsrc.FixedJavaOf(req.Core)
		t.Logf("代理端 %s 使用 Java %d", req.Core, major)
	default:
		if known {
			if meta, err := mcsrc.GetVersionMeta(ctx, req.MC); err == nil {
				major = meta.JavaMajor
				t.Logf("MC %s 需要 Java %d（来自官方元数据）", req.MC, major)
			} else {
				t.Logf("没拿到 MC %s 的官方元数据（%v）", req.MC, err)
			}
		}
		if major == 0 {
			for _, c := range sc.Targets {
				if c.Target == req.LaunchTarget && c.MC == req.MC && c.JavaMajor > 0 {
					major = c.JavaMajor
					t.Logf("服务端自己写明需要 Java %d", major)
				}
			}
		}
	}
	if major < 8 { // 哪条路都没定下来
		return fmt.Errorf("没法按版本「%s」自动选 Java（官方清单里没有这个版本，或者现在连不上网）。请返回上一步，在「Java」里手动选一个版本再试", req.MC)
	}

	// ---- 步骤 1：准备 Java（指定的 > 本机已装 > 便携 > 下载） ----
	t.StartStep(1)
	javaPath := req.JavaPath
	if javaPath != "" {
		if st, err := os.Stat(javaPath); err != nil || st.IsDir() {
			return fmt.Errorf("找不到 Java：%s", javaPath)
		}
	} else {
		var err error
		if javaPath, err = java.Resolve(ctx, major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", major))); err != nil {
			return err
		}
	}
	t.Logf("Java 就绪: %s", javaPath)

	// ---- 步骤 2：写入 MineDeck 的文件并登记 ----
	t.StartStep(2)
	// 文件夹在准备 Java 的这段时间里可能变了（被别的程序挪走、启动文件被删）：再确认一次
	if _, err := os.Stat(filepath.Join(dir, launchFile(req.LaunchTarget))); err != nil {
		return fmt.Errorf("启动文件不见了：%s", launchFile(req.LaunchTarget))
	}
	isArgs := strings.HasPrefix(req.LaunchTarget, "args:")
	inst := &Instance{
		Settings: Settings{
			Name: req.Name, Core: req.Core, MC: req.MC, Build: req.Build,
			JavaMajor: major, JavaPath: javaPath,
			XmxMB: req.XmxMB, XmsMB: min(1024, req.XmxMB),
			LaunchTarget: req.LaunchTarget,
			CreatedAt:    time.Now(),
			Adopted:      true,
		},
		Dir: dir, Console: newConsole(),
	}
	if p := sc.prev; p != nil { // MineDeck 管过的目录：JVM 参数、定时任务、崩溃重启、控制台编码照旧
		inst.ExtraJVM = append([]string{}, p.ExtraJVM...)
		inst.Policies, inst.ConsoleEncoding = p.Policies, p.ConsoleEncoding
		if !p.CreatedAt.IsZero() {
			inst.CreatedAt = p.CreatedAt
		}
	}
	if isArgs { // 按用户最后选的启动方式来：user_jvm_args.txt 只有 Forge / NeoForge 的启动脚本会读。它马上要被改写，里面的参数先带过来
		_, extra, _ := readUserJVMArgs(filepath.Join(dir, "user_jvm_args.txt"))
		had := len(inst.ExtraJVM)
		inst.ExtraJVM = mergeArgs(inst.ExtraJVM, extra)
		if added := len(inst.ExtraJVM) - had; added > 0 {
			t.Logf("user_jvm_args.txt 里的 %d 个 JVM 参数已带到「自定义 JVM 参数」", added)
		}
	}

	// 下面开始动文件夹里的文件。会改到的几个先记下原样：中途哪一步失败，就全部恢复，文件夹和接管之前一模一样。
	touched := []string{"run.bat", runBatBackup, "instance.json", "instance.json.tmp"}
	if isArgs {
		touched = append(touched, "user_jvm_args.txt", jvmArgsBackup)
	}
	if kind != mcsrc.KindProxy {
		touched = append(touched, "eula.txt", log4jXML)
	}
	restore, err := snapshotFiles(dir, touched)
	if err != nil {
		return err
	}
	if err := m.writeAdoptFiles(ctx, t, inst, req, kind, known); err != nil {
		restore()
		return err
	}
	_ = os.Remove(filepath.Join(dir, settingsBackup)) // 以前移出列表时留的那份已经并进新的 instance.json
	// 下次启动还要认得它：只登记这一个文件夹。不把上一层记成扫描根——那样上一层里别的带 instance.json 的文件夹
	// （比如老版本 MineDeck 的 servers 目录里的另外几台）会不经接管就自己出现在列表里
	app.RememberInstanceDir(dir)
	m.mu.Lock()
	m.insts[req.Name] = inst
	m.mu.Unlock()
	t.Logf("接管完成 ✔ 存档、模组、插件和配置文件都没有动")
	return nil
}

// writeAdoptFiles 接管时往文件夹里写的全部东西：eula.txt、log4shell 缓解、两份备份、run.bat、instance.json。
// 出错就返回，由调用方把动过的文件恢复原样。
func (m *Manager) writeAdoptFiles(ctx context.Context, t *tasks.Task, inst *Instance, req AdoptReq, kind mcsrc.CoreKind, known bool) error {
	dir := inst.Dir
	if kind != mcsrc.KindProxy {
		// EULA 以现在文件里的为准（准备 Java 的这段时间里可能变了），没同意过又没勾同意的不写
		if b, err := readCapped(filepath.Join(dir, "eula.txt"), 64<<10); err != nil || !reEULA.Match(b) {
			if !req.EULA {
				return fmt.Errorf("需要同意 Minecraft EULA 才能开服")
			}
			if err := os.WriteFile(filepath.Join(dir, "eula.txt"),
				[]byte("# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
				return err
			}
			t.Logf("已写入 eula.txt（你在上一步同意了 Minecraft EULA）")
		}
		if known { // log4shell 防护（老版本），和新建时一样；已有的同名参数以这里的为准
			own := len(inst.ExtraJVM)
			if err := applyLog4jMitigation(ctx, t, inst); err != nil {
				t.Logf("没能判断要不要加 log4shell 防护（%v），先跳过", err)
			}
			inst.ExtraJVM = withJVMFlags(inst.ExtraJVM[:own:own], inst.ExtraJVM[own:])
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "run.bat")); err == nil && !isOurRunBat(b) {
		if saved, err := backupOnce(dir, "run.bat", runBatBackup); err != nil {
			return fmt.Errorf("备份原来的 run.bat 失败: %w", err)
		} else if saved {
			t.Logf("原来的 run.bat 另存为 %s", runBatBackup)
		}
	}
	if strings.HasPrefix(inst.LaunchTarget, "args:") {
		if b, err := os.ReadFile(filepath.Join(dir, "user_jvm_args.txt")); err == nil && !isOurJVMArgs(b) {
			if saved, err := backupOnce(dir, "user_jvm_args.txt", jvmArgsBackup); err != nil {
				return fmt.Errorf("备份原来的 user_jvm_args.txt 失败: %w", err)
			} else if saved {
				t.Logf("原来的 user_jvm_args.txt 另存为 %s（里面的内存设置改由 MineDeck 管）", jvmArgsBackup)
			}
		}
	}
	if err := writeRunBat(inst); err != nil {
		return err
	}
	return m.saveInstance(inst)
}

// snapshotFiles 记下 dir 里这几个文件现在的样子（内容，或者「没有」），返回一个把它们恢复原样的函数。
// 都是很小的文本文件；哪个大得反常（超过 4 MB）就报错不接管，免得把它读进内存。
func snapshotFiles(dir string, names []string) (restore func(), err error) {
	type snap struct {
		path    string
		data    []byte
		existed bool
	}
	var snaps []snap
	for _, n := range names {
		p := filepath.Join(dir, n)
		st, err := os.Stat(p)
		if err != nil {
			snaps = append(snaps, snap{path: p})
			continue
		}
		if st.IsDir() || st.Size() > 4<<20 {
			return nil, fmt.Errorf("文件夹里的 %s 不是正常的文件（是个目录，或者大得反常），没有接管", n)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("读不了 %s：%w", n, err)
		}
		snaps = append(snaps, snap{path: p, data: b, existed: true})
	}
	return func() {
		for _, s := range snaps {
			if s.existed {
				_ = os.WriteFile(s.path, s.data, 0o644)
			} else {
				_ = os.Remove(s.path)
			}
		}
	}, nil
}

// mergeArgs 把 extra 里还没有的参数接到 args 后面（顺序不变，不重复）。
func mergeArgs(args, extra []string) []string {
	out := append([]string{}, args...)
	for _, a := range extra {
		dup := false
		for _, b := range out {
			if a == b {
				dup = true
			}
		}
		if !dup {
			out = append(out, a)
		}
	}
	return out
}

// launchFile LaunchTarget 指的那个文件（相对实例目录）。
func launchFile(target string) string {
	if rest, ok := strings.CutPrefix(target, "args:"); ok {
		return filepath.FromSlash(rest)
	}
	return strings.TrimPrefix(target, "jar:")
}

// isOurJVMArgs 这个 user_jvm_args.txt 是不是 MineDeck（或改名前的 AutoMCHUB）生成的。
func isOurJVMArgs(b []byte) bool {
	return strings.HasPrefix(string(b), "# MineDeck 生成") || strings.HasPrefix(string(b), "# AutoMCHUB 生成")
}

// backupOnce 把 dir 里的 name 复制一份成 backup。备份已经有了就不再覆盖：留着的是最早那份，也就是用户自己的原件。
func backupOnce(dir, name, backup string) (saved bool, err error) {
	dst := filepath.Join(dir, backup)
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	return true, copyFile(filepath.Join(dir, name), dst)
}
