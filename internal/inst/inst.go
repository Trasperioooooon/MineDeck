// Package inst 管理服务器实例：创建流水线、配置读写、进程启停与控制台。
package inst

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

type Settings struct {
	Name            string     `json:"name"`
	Core            mcsrc.Core `json:"core"`
	MC              string     `json:"mc"`
	Build           string     `json:"build"`
	JavaMajor       int        `json:"javaMajor"`
	JavaPath        string     `json:"javaPath"`
	XmxMB           int        `json:"xmxMb"`
	XmsMB           int        `json:"xmsMb"`
	LaunchTarget    string     `json:"launchTarget"` // "jar:<文件>" 或 "args:<win_args.txt 相对路径>"
	ExtraJVM        []string   `json:"extraJvm,omitempty"`
	ConsoleEncoding string     `json:"consoleEncoding,omitempty"` // auto | utf-8 | gbk
	Policies        Policies   `json:"policies"`
	CreatedAt       time.Time  `json:"createdAt"`
	// Adopted 这台服务器是「接管」进来的：目录是用户自己原来的文件夹，不是 MineDeck 新建的（见 adopt.go）。
	// 界面删除时拿它决定默认只移出列表、不删文件。
	Adopted bool `json:"adopted,omitempty"`
	// WorldFrom 现在的世界是从哪份备份还原来的，见 WorldFrom；没还原过、或者还原以后换过地图时没有
	WorldFrom *WorldFrom `json:"worldFrom,omitempty"`
}

// WorldFrom 现在的世界是从哪份备份还原来的（可用性走查 R11）：详情页「世界与备份」页顶据此写「当前世界：从 某某时间 的备份还原」。
// 记在 instance.json 里，重开程序后仍在；下一次还原换成那一份，换地图（导入、换回、恢复）时清掉，见 clearWorldFrom。
type WorldFrom struct {
	Backup     string `json:"backup"`               // 备份文件名
	Label      string `json:"label,omitempty"`      // 备份的标签（文件名里时间后面那段）
	BackupTime string `json:"backupTime,omitempty"` // 这份备份是什么时候做的（2006-01-02 15:04:05）；文件名里认不出时用文件的修改时间
	At         string `json:"at"`                   // 什么时候还原的（2006-01-02 15:04:05）
}

// WorldFromSnapshot 加锁读取 WorldFrom 的一份拷贝；没有时为 nil。
func (i *Instance) WorldFromSnapshot() *WorldFrom {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.WorldFrom == nil {
		return nil
	}
	wf := *i.WorldFrom
	return &wf
}

// setWorldFrom 记下（wf 为 nil 时清掉）现在的世界从哪来，并存进 instance.json。存不进去不算还原失败：只是页顶少一句说明，
// 原因写进控制台。没变化时不写盘。
func (m *Manager) setWorldFrom(i *Instance, wf *WorldFrom) {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	m.setWorldFromLocked(i, wf)
}

// setWorldFromLocked 同 setWorldFrom，调用方已经拿着 i.procMu（换地图收尾时在占位还在的时候存，见 clearWorldFromHeld）。
func (m *Manager) setWorldFromLocked(i *Instance, wf *WorldFrom) {
	if i.WorldFrom == nil && wf == nil {
		return
	}
	i.WorldFrom = wf
	if err := m.saveInstance(i); err != nil {
		i.Console.Append("[MineDeck] 没能把「当前世界从哪份备份来」记进 instance.json：" + err.Error())
	}
}

// keepWorldFrom 这几种占位做完以后，现在的世界仍是原来那个（或者服务器已经没了），不用清掉 WorldFrom：
// 还原备份自己会记新的那一份；删除服务器、删除恢复点、删除插件或模组都不动世界。其余的占位（换回旧地图、恢复地图、导入地图）
// 都可能换掉世界，做完就清掉：宁可少一句说明，也不能把换过的地图说成是从备份来的。
var keepWorldFrom = map[string]bool{"还原备份": true, "删除": true, "删除恢复点": true, modTrashWhy: true}

type Instance struct {
	Settings
	Dir     string
	Console *Console

	backupMu   sync.Mutex // 同实例备份串行，热备 save-off/save-on 不互相穿插
	procMu     sync.Mutex
	proc       *proc
	state      string // stopped | starting | running | stopping
	userStop   bool   // 本次退出是否用户主动触发（区分崩溃）
	startedAt  time.Time
	crashCount int
	runGen     int64     // 每次 Start 自增，用于让崩溃重启只作用于其对应的那次运行
	killing    bool      // 这次运行是被强制结束的（Kill、停止超时）
	lastExit   string    // 上一次进程怎么结束的，见 LastExit
	startErr   string    // 上一次启动没起来的原因，见 LastStartError；下一次进程起来了就清掉
	startBusy  *PortBusy // 上一次没起来是因为端口被占着：哪个端口、谁占着、能改成哪个（见 LastStartPortBusy），跟着 startErr 一起清

	onlineMu sync.Mutex
	online   map[string]bool
}

func (i *Instance) Status() string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.state == "" {
		return "stopped"
	}
	return i.state
}

func (i *Instance) PropsPath() string { return filepath.Join(i.Dir, "server.properties") }

// SettingsSnapshot 一次取得同一时刻的全部设置，切片不与正在保存的实例共享。
func (i *Instance) SettingsSnapshot() Settings {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	s := i.Settings
	s.ExtraJVM = append([]string(nil), s.ExtraJVM...)
	s.Policies.Schedules = append([]Schedule(nil), s.Policies.Schedules...)
	return s
}

// Port 读取当前 server.properties 中的端口：服务端实际会监听的那个（按 Java 的读法取，同一项写了两行时认最后一行）。
func (i *Instance) Port() int {
	if v, ok := javaPropsOf(i.PropsPath())["server-port"]; ok {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 25565
}

type Manager struct {
	mu       sync.Mutex
	insts    map[string]*Instance
	creating map[string]bool // 正在创建中的实例名（注册进 insts 前的占位，防并发/重复创建同名实例）
	packing  map[string]bool // 正在打包交接包的实例名：这期间不能启动、删除，免得打出来的包里世界是写了一半的
	// worldHold 正在换地图 / 还原备份的实例名 → 在做什么；worldBlock 有没走完的换地图记录的实例名 → 原因（见 worldhold.go）
	worldIntent map[string]*WorldIntent
	worldHold   map[string]string
	worldBlock  map[string]string
	// worldUse 正在用这台服务器的世界和设置的操作（备份、保存设置）：实例名 → 在做什么 → 有几个。见 UseWorld
	worldUse     map[string]map[string]int
	worldPlacer  WorldPlacer
	worldRelease func(owner, planID string)
	Tasks        *tasks.Manager

	autoMu    sync.Mutex
	autoHook  func(AutoAction)     // 定时任务、崩溃自动重启执行后的回调（操作层用它写操作记录）
	startHook func(string)         // 起服务器进程之前的回调（音乐共享在这里写好桥接模组的配置、voice_host），见 hooks.go
	lineHook  func(string, string) // 服务器每输出一行的回调（游戏内 AI 认聊天），见 hooks.go

	pendMu      sync.Mutex // 整合包导入待决的任务 → 实例（处置时按任务找实例目录）
	pendingInst map[string]pendingRef
	resolveMu   sync.Mutex // 待决文件的处置一次只做一批，见 ResolveImportPending

	skippedDups []SkippedDuplicate // 启动加载时因为重名没列出来的文件夹（A08）

	closing atomic.Bool // ShutdownAll 开始以后为真：程序正在退出，等着重试的定时备份不再动手
}

// SkippedDuplicate 启动加载服务器时，因为名字和已加载的另一台撞了而没有列出来的文件夹（A08）。
type SkippedDuplicate struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	KeptDir string `json:"keptDir"` // 列表里留着的那台服务器所在的文件夹
}

func NewManager() (*Manager, error) {
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}
	seen := map[string]bool{}          // 已经登记过的目录（真实路径）：同一个文件夹从两条路都能找到时只算一次
	fromDirs := map[string]*Instance{} // 从 InstanceDirs 加载的实例（按真实路径记）：后面若在存放根下面扫到同一个真实文件夹，把 Dir 改成存放根下的路径
	load := func(dir string, fromRoot bool) {
		s, err := readSettings(dir)
		if os.IsNotExist(err) {
			return // 不是实例目录，或者这个文件夹现在不在
		}
		if err != nil {
			log.Printf("实例配置读不出来，这个目录没有列出来：%s（%v）", dir, err)
			return
		}
		key := strings.ToLower(realPath(dir))
		if seen[key] {
			// 同一个真实文件夹既经目录联接登记在 InstanceDirs 里，又在某个存放根下面被扫到：
			// i.Dir 用存放根下面那条路径，免得「连同文件删除」只删掉联接而留下真目录在下次启动复活；
			// 「登记的优先」只管两个不同文件夹撞名的情形（A08）。
			if fromRoot {
				if inst, ok := fromDirs[key]; ok {
					delete(fromDirs, key)
					inst.Dir = dir
				}
			}
			return
		}
		seen[key] = true
		if kept, dup := m.insts[s.Name]; dup {
			log.Printf("实例名冲突，已跳过重复目录：%s（%s）", s.Name, dir)
			m.skippedDups = append(m.skippedDups, SkippedDuplicate{Name: s.Name, Dir: dir, KeptDir: kept.Dir})
			return
		}
		startErr, startBusy := loadStartFail(dir)
		inst := &Instance{Settings: s, Dir: dir, Console: newConsole(), startErr: startErr, startBusy: startBusy}
		m.insts[s.Name] = inst
		if !fromRoot {
			fromDirs[key] = inst
		}
		// 上次换地图没走完：先封住启动，等 worldimport 按记录处理完再放开（比调度器和任何启动入口都早）
		if why := PendingWorldTx(dir); why != "" {
			if m.worldBlock == nil {
				m.worldBlock = map[string]string{}
			}
			m.worldBlock[s.Name] = why
		}
	}
	// 单独登记的文件夹（接管进来的服务器）先读（A08）：只读它自己，不看它的上一层。
	// 先读它，后插上的移动硬盘或存放位置里后冒出来的同名服务器才不会顶掉接管进来的那台。
	// 文件夹现在不在（比如在没插上的移动硬盘里）就跳过，登记留着，回来了照样认；
	// 登记只在这台服务器移出列表或被删掉时去掉（见 Delete）。
	for _, dir := range app.InstanceDirs() {
		load(dir, false)
	}
	// 多根扫描：内置默认根 + 配置默认根 + 历史自定义根（支持实例散落于不同盘符/目录）；存放位置内部重名的仍按先到先得
	for _, root := range app.InstanceRoots() {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue // 根目录不存在/不可读则跳过，不再因默认根缺失整体失败
		}
		for _, e := range ents {
			if e.IsDir() {
				load(filepath.Join(root, e.Name()), true)
			}
		}
	}
	m.clearStaleCreatingMarkers() // 列半成品的接口只读，残留的创建记号在这里去掉（A38）
	m.startScheduler()
	return m, nil
}

// SkippedDuplicates 返回启动加载时因为重名没列出来的文件夹（A08）。
// 若列表里已经没有这台同名服务器、或者被跳过的文件夹自己已进入列表、或者磁盘上已挪走/删掉/改了名，不再继续提示。
func (m *Manager) SkippedDuplicates() []SkippedDuplicate {
	m.mu.Lock()
	dups := append([]SkippedDuplicate(nil), m.skippedDups...)
	var instDirs []string
	keptByName := make(map[string]string, len(dups))
	if len(dups) > 0 {
		instDirs = make([]string, 0, len(m.insts))
		for _, inst := range m.insts {
			instDirs = append(instDirs, inst.Dir)
		}
		for _, d := range dups {
			if k, ok := m.insts[d.Name]; ok {
				keptByName[d.Name] = k.Dir
			}
		}
	}
	m.mu.Unlock()
	if len(dups) == 0 {
		return []SkippedDuplicate{}
	}
	loaded := make(map[string]bool, len(instDirs))
	for _, dir := range instDirs {
		loaded[strings.ToLower(realPath(dir))] = true
	}
	out := make([]SkippedDuplicate, 0, len(dups))
	for _, d := range dups {
		kd, ok := keptByName[d.Name]
		if !ok {
			continue
		}
		if loaded[strings.ToLower(realPath(d.Dir))] {
			continue
		}
		s, err := readSettings(d.Dir)
		if err != nil || s.Name != d.Name {
			continue
		}
		d.KeptDir = kd
		out = append(out, d)
	}
	return out
}

// readSettings 读实例目录里的 instance.json。开头的 UTF-8 BOM 去掉再解析：记事本存成「UTF-8 带 BOM」、
// PowerShell 5.1 的 Set-Content 写的都有，不去掉这台服务器会从列表里消失（测试报告发现 10）。
func readSettings(dir string) (Settings, error) {
	return readSettingsFile(filepath.Join(dir, "instance.json"))
}

// readSettingsFile 读一份实例配置文件（instance.json，或移出列表时留底的 instance.json.bak）。
func readSettingsFile(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &s); err != nil {
		return s, err
	}
	if s.Name == "" {
		return s, fmt.Errorf("没有 name")
	}
	return s, nil
}

func (m *Manager) saveInstance(i *Instance) error {
	b, err := json.MarshalIndent(i.Settings, "", "  ")
	if err != nil {
		return err
	}
	// 先写临时文件再改名：写到一半断电也不会留下半截的 instance.json（那样这台服务器下次就列不出来了）
	path := filepath.Join(i.Dir, "instance.json")
	if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (m *Manager) List() []*Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Instance, 0, len(m.insts))
	for _, i := range m.insts {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	return out
}

func (m *Manager) Get(name string) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[name]
	if !ok {
		return nil, fmt.Errorf("服务器「%s」不存在", name)
	}
	return i, nil
}

// Delete 删除实例；files=true 时同时删除实例目录。
// files=false 只是移出列表：文件全都留着，instance.json 改名成 instance.json.bak——不改名的话下次启动扫描目录
// 又会把它认回来。以后想加回来，用「接管已有服务器」选这个文件夹，原来的设置（内存、定时任务等）会从 .bak 里带回来。
func (m *Manager) Delete(name string, files bool) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if m.isPacking(name) { // 正在打包：读文件的在压成包，删掉目录会让它打出一个坏包
		return fmt.Errorf("这台服务器正在打包，等打包完成后再删除或移出列表")
	}
	if why := m.WorldBusy(name); why != "" { // 换地图到一半：删掉目录会连恢复点一起删掉
		return errors.New(why)
	}
	if i.Status() != "stopped" {
		return fmt.Errorf("请先停止服务器再删除")
	}
	// 上面三样是为了把原因说清楚，看完到真的动手之间还有空档。这里占上位再删：占位和启动、换地图、备份、打包、
	// 保存设置用的是同一套登记，删的过程中它们都进不来（2026-11-14 Codex 合并复审第 2 条）。
	// 占上以后不能再调 WorldBusy：会看到自己占的位
	release, err := m.BeginWorldChangeInstance(i, "删除")
	if err != nil {
		return err
	}
	defer release()
	if deleteHook != nil {
		deleteHook()
	}
	// 这台服务器的单独登记（接管进来的才有）。登记的写法可能和 i.Dir 不一样——比如登记的是目录联接，
	// 而真实文件夹也在某个存放根下面（加载时 i.Dir 用的是存放根下的路径），或者同一文件夹登记过别名——
	// 所以按真实路径找；要赶在删文件夹之前找，删了就解不出真实路径了
	var registered []string
	if dirs := app.InstanceDirs(); len(dirs) > 0 {
		real := realPath(i.Dir)
		for _, d := range dirs {
			if strings.EqualFold(realPath(d), real) {
				registered = append(registered, d)
			}
		}
	}
	if files {
		if err := safeRemoveInstanceDir(i); err != nil {
			return err
		}
	} else if err := releaseInstanceDir(i); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.insts, name)
	m.mu.Unlock()
	for _, d := range registered { // 不是接管进来的本来就没登记，这里什么都不做
		app.ForgetInstanceDir(d)
	}
	return nil
}

// deleteHook 测试用：取得删除占位以后、动文件以前。
var deleteHook func()

// settingsBackup 移出列表时 instance.json 改成的名字。
const settingsBackup = "instance.json.bak"

// releaseInstanceDir 移出列表时把目录里的 instance.json 改名留底。文件已经不在（目录被用户挪走、删掉）不算错。
// 不看里面的内容：这台服务器就是从这个目录读出来的，即使文件后来被人改坏、改了名字，也照样改名——
// 留着它，下次启动这个目录又会被认成一台服务器。
func releaseInstanceDir(i *Instance) error {
	src := filepath.Join(i.Dir, "instance.json")
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		return nil
	}
	if err := os.Rename(src, filepath.Join(i.Dir, settingsBackup)); err != nil {
		return fmt.Errorf("没能把这台服务器移出列表（%w）。可以关掉占用这个文件夹的程序再试。", err)
	}
	return nil
}

// safeRemoveInstanceDir 仅当目录内的 instance.json 确属本实例时才整目录删除，
// 从而支持任意自定义位置（不再依赖是否位于默认 servers 根下）。
// 先删其余文件、最后才删 instance.json（A121）：中途遇上被占用的文件失败时 instance.json 仍在，下次还能再删。
func safeRemoveInstanceDir(i *Instance) error {
	dir := filepath.Clean(i.Dir)
	if dir == "" || dir == filepath.Dir(dir) || dir == filepath.Clean(app.Base) {
		return fmt.Errorf("服务器文件夹异常，已拒绝删除：%s", dir)
	}
	s, err := readSettings(dir)
	if os.IsNotExist(err) {
		return fmt.Errorf("找不到 instance.json，已拒绝删除: %s", dir)
	}
	if err != nil || s.Name != i.Name {
		return fmt.Errorf("这个文件夹对不上这台服务器（instance.json 读不出来，或里面的名字不一致），已拒绝删除: %s", dir)
	}
	emptyDirErr, err := removeInstanceDirMarkerLast(dir)
	if err != nil {
		return fmt.Errorf("删除文件失败: %w", err)
	}
	if emptyDirErr != nil {
		logLeftInstanceDir(i.Name, dir, emptyDirErr)
	}
	return nil
}

// logLeftInstanceDir 删到最后只剩文件夹本身没删掉时（照常移出列表）记一笔，按文件夹里实际还剩什么说：
// 多半是文件夹被别的程序当成工作目录占着、里面是空的；也可能删的同时有程序往里写了文件，文件夹其实不空。
func logLeftInstanceDir(name, dir string, cause error) {
	ents, err := os.ReadDir(dir)
	switch {
	case err != nil:
		log.Printf("服务器「%s」已移出列表，但文件夹本身没删掉，也读不出里面还剩什么：%s（%v；%v）", name, dir, cause, err)
	case len(ents) == 0:
		log.Printf("服务器「%s」的文件已删空，但文件夹本身正被别的程序占用，留了一个空文件夹：%s（%v）", name, dir, cause)
	default:
		names := make([]string, 0, 3)
		for _, e := range ents[:min(len(ents), 3)] {
			names = append(names, e.Name())
		}
		if len(ents) > len(names) {
			names = append(names, "…")
		}
		log.Printf("服务器「%s」已移出列表，但文件夹没有删掉，里面还剩 %d 项（%s）：%s（%v）。"+
			"可能是删的同时有程序往里写了文件，或者还有程序开着里面的文件", name, len(ents), strings.Join(names, "、"), dir, cause)
	}
}

// walkInstanceDir 顶层路径是往里逐项删（true），还是只删这一个目录项本身（false）。
// 只看 ModeDir，不排除 ModeIrregular：Go 1.23 起符号链接和目录联接都不带 ModeDir，
// 而 OneDrive 按需文件这类云同步真文件夹报 ModeDir|ModeIrregular，必须往里删。
func walkInstanceDir(mode fs.FileMode) bool {
	return mode.IsDir()
}

// removeInstanceDirMarkerLast 不跟顶层链接；普通目录先删 instance.json 以外的条目，最后删 instance.json 与空目录。
// instance.json 已经删掉、只剩删空文件夹失败时返回 emptyDirErr（err 为 nil），由调用方记日志并照常移出列表。
func removeInstanceDirMarkerLast(dir string) (emptyDirErr error, err error) {
	st, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !walkInstanceDir(st.Mode()) {
		return nil, atomicfile.RetryRemove(func() error { return os.Remove(dir) })
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var markers []string
	for _, e := range ents {
		if strings.EqualFold(e.Name(), "instance.json") {
			markers = append(markers, e.Name())
			continue
		}
		child := filepath.Join(dir, e.Name())
		if err := atomicfile.RetryRemove(func() error { return os.RemoveAll(child) }); err != nil {
			return nil, err
		}
	}
	for _, name := range markers {
		p := filepath.Join(dir, name)
		if err := atomicfile.RetryRemove(func() error { return os.Remove(p) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if err := atomicfile.RetryRemove(func() error { return os.Remove(dir) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err, nil
	}
	return nil, nil
}

// 打包任务的占位：打包时读着实例目录里的世界和配置，这期间启动、删除都会打出坏包。
// map 惰性初始化，测试里手工构造的 Manager 也能用。
func (m *Manager) isPacking(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.packing[name]
}

// tryMarkPacking 占上打包的位；已经有人占着，或者这台服务器正在换地图、有没走完的换地图记录时返回 false。
func (m *Manager) tryMarkPacking(name string) bool {
	i, err := m.Get(name)
	if err != nil {
		return false
	}
	return m.tryMarkPackingInstance(i)
}

func (m *Manager) tryMarkPackingInstance(i *Instance) bool {
	name := i.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insts[name] != i || m.worldIntent[name] != nil || m.packing[name] || m.worldHold[name] != "" || m.worldBlock[name] != "" {
		return false
	}
	if m.packing == nil {
		m.packing = map[string]bool{}
	}
	m.packing[name] = true
	return true
}

func (m *Manager) unmarkPacking(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.packing, name)
}

// SettingsSaveResult 保存或预览运行参数的结果：给出补齐 log4shell 防护后的实际参数、完整启动参数，以及有非官方同名日志配置时的说明（A28，A25 保存参数校验）。
type SettingsSaveResult struct {
	XmxMB      int      `json:"xmxMb"`
	XmsMB      int      `json:"xmsMb"`
	ExtraJVM   []string `json:"extraJvm"`
	LaunchArgs []string `json:"launchArgs"`
	Note       string   `json:"note,omitempty"`
}

// computeSettingsUpdate 按同一套规则校验并算出更新后的运行参数（PreviewSettings 与 UpdateSettingsWithResult 共用，A28）。
// 只读 cur 值副本与磁盘上的服务端 jar / 日志配置文件，不碰 Instance 锁。
func computeSettingsUpdate(cur Settings, dir string, xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) (Settings, SettingsSaveResult, error) {
	switch consoleEnc {
	case "auto", "utf-8", "gbk", "":
	default:
		return Settings{}, SettingsSaveResult{}, fmt.Errorf("不支持的控制台编码: %s", consoleEnc)
	}
	if extraJVM != nil {
		if err := validateJVMArgs(*extraJVM, cur.ExtraJVM); err != nil {
			return Settings{}, SettingsSaveResult{}, err
		}
	}
	next := cur
	next.ExtraJVM = append([]string(nil), cur.ExtraJVM...)
	if xmxMB > 0 {
		if xmxMB < 512 {
			return Settings{}, SettingsSaveResult{}, fmt.Errorf("最大内存不能低于 512MB")
		}
		if xmsMB < 128 || xmsMB > xmxMB {
			xmsMB = min(1024, xmxMB)
		}
		next.XmxMB, next.XmsMB = xmxMB, xmsMB
	}
	if consoleEnc != "" {
		next.ConsoleEncoding = consoleEnc
	}
	var note string
	if extraJVM != nil {
		next.ExtraJVM, note = completeExtraJVM(next, dir, *extraJVM)
	}
	tmp := Instance{Settings: next, Dir: dir}
	return next, SettingsSaveResult{
		XmxMB:      next.XmxMB,
		XmsMB:      next.XmsMB,
		ExtraJVM:   append([]string(nil), next.ExtraJVM...),
		LaunchArgs: launchArgs(&tmp),
		Note:       note,
	}, nil
}

// PreviewSettings 不落盘地算出修改运行参数后实际会存、会用的参数（AI 确认卡片和界面「启动与内存」预览共用，A28）。
// 只在取当前设置快照时短暂持 procMu，开包检查服务端 jar 与读日志配置文件都在锁外完成，不阻塞 Status 等状态查询。
func (i *Instance) PreviewSettings(xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) (SettingsSaveResult, error) {
	cur := i.SettingsSnapshot()
	_, res, err := computeSettingsUpdate(cur, i.Dir, xmxMB, xmsMB, consoleEnc, extraJVM)
	return res, err
}

// PreviewSettings 按实例名预览运行参数（见 Instance.PreviewSettings）。
func (m *Manager) PreviewSettings(name string, xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) (SettingsSaveResult, error) {
	i, err := m.Get(name)
	if err != nil {
		return SettingsSaveResult{}, err
	}
	return i.PreviewSettings(xmxMB, xmsMB, consoleEnc, extraJVM)
}

// UpdateSettings 调整实例运行参数（内存、控制台编码、自定义 JVM 参数）并重新生成启动脚本。
// xmxMB<=0 表示不修改内存；consoleEnc 为空表示不修改编码；extraJVM 为 nil 表示不修改 JVM 参数。
func (m *Manager) UpdateSettings(name string, xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) error {
	_, err := m.UpdateSettingsWithResult(name, xmxMB, xmsMB, consoleEnc, extraJVM)
	return err
}

// UpdateSettingsWithResult 同 UpdateSettings，并返回补齐后的实际参数与说明（A28，A25 保存参数校验）。
func (m *Manager) UpdateSettingsWithResult(name string, xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) (SettingsSaveResult, error) {
	i, err := m.Get(name)
	if err != nil {
		return SettingsSaveResult{}, err
	}
	release, err := m.UseWorldInstance(i, "保存启动设置")
	if err != nil {
		return SettingsSaveResult{}, err
	}
	defer release()
	// 运行中控制台 goroutine 会并发读这些字段，写入必须持锁；
	// 落盘（saveInstance 序列化 i.Settings、writeRunBat 读取 i 字段）也在锁内完成，
	// 避免与并发的 UpdateSettings 竞争地读写 i.Settings
	i.procMu.Lock()
	defer i.procMu.Unlock()
	next, res, err := computeSettingsUpdate(i.Settings, i.Dir, xmxMB, xmsMB, consoleEnc, extraJVM)
	if err != nil {
		return SettingsSaveResult{}, err
	}
	old := i.Settings
	i.Settings = next
	if err := m.saveInstance(i); err != nil {
		i.Settings = old
		return SettingsSaveResult{}, err
	}
	if err := writeRunBat(i); err != nil {
		return SettingsSaveResult{}, err
	}
	return res, nil
}

// consoleEnc 加锁读取控制台编码（供输出 goroutine 每行调用）。
func (i *Instance) consoleEnc() string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.ConsoleEncoding
}

// MemSnapshot 加锁快照 UpdateSettings 会改写的三个字段（供 API 摘要读取，杜绝跨包裸读数据竞争）。
func (i *Instance) MemSnapshot() (xmxMB, xmsMB int, enc string) {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.XmxMB, i.XmsMB, i.ConsoleEncoding
}

// PoliciesSnapshot 加锁快照运维策略（供 API 读取）。
func (i *Instance) PoliciesSnapshot() Policies {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	p := i.Policies
	p.Schedules = append([]Schedule{}, i.Policies.Schedules...)
	return p
}

// OnlineCount 返回当前在线玩家数（加锁读）。
func (i *Instance) OnlineCount() int {
	i.onlineMu.Lock()
	defer i.onlineMu.Unlock()
	return len(i.online)
}

// UptimeSec 返回运行时长（秒）；非运行态返回 0。
func (i *Instance) UptimeSec() int {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.state == "running" && !i.startedAt.IsZero() {
		return int(time.Since(i.startedAt).Seconds())
	}
	return 0
}

// ExtraJVMSnapshot 加锁快照自定义 JVM 参数（供 API 读取）。
func (i *Instance) ExtraJVMSnapshot() []string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return append([]string{}, i.ExtraJVM...)
}

var invalidNameChars = `\/:*?"<>|`

// validateJVMArgs 查这次新加或改过的 JVM 参数：不能有换行这类控制字符，也不能有双引号。
// 以前它们原样写进 run.bat，引号会把外面加的引号提前关掉、换行会另起一行，手动双击 run.bat 时 cmd 就会多执行
// 别的命令（v3.1.0 正式版前检查 B-7）。界面按空白拆参数，带不进换行，加了引号也拆不成一个参数；
// 控制字符拦的是 AI、MCP 和直接调接口传进来的。
// keep 是现在存着的参数，里面原样有的不查：界面每次保存都把整张参数表发过来，只改内存也一样，
// 要是连老参数一起查，带双引号的老参数就让什么都存不了。老参数写进 run.bat 时照规矩转义（见 batQuoteArg），
// 不会多执行命令。
func validateJVMArgs(args, keep []string) error {
	old := make(map[string]bool, len(keep))
	for _, a := range keep {
		old[a] = true
	}
	for _, a := range args {
		if old[a] {
			continue
		}
		if strings.IndexFunc(a, unicode.IsControl) >= 0 {
			return fmt.Errorf("JVM 参数里不能有换行、制表符这类控制字符")
		}
		if strings.Contains(a, `"`) {
			return fmt.Errorf("JVM 参数里不能有双引号")
		}
	}
	return nil
}

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 40 {
		return fmt.Errorf("服务器名称需为 1~40 个字符")
	}
	// 名字会写进 run.bat 的 title 那一行：换行能让手动双击 run.bat 时多执行一行命令（v3.1.0 正式版前检查 B-7）
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("服务器名称不能包含换行、制表符这类控制字符")
	}
	if strings.ContainsAny(name, invalidNameChars) {
		return fmt.Errorf(`服务器名称不能包含字符 %s`, invalidNameChars)
	}
	if name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("服务器名称不合法")
	}
	return nil
}
