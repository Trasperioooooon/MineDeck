// Package inst 管理服务器实例：创建流水线、配置读写、进程启停与控制台。
package inst

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
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
}

type Instance struct {
	Settings
	Dir     string
	Console *Console

	procMu     sync.Mutex
	proc       *proc
	state      string // stopped | starting | running | stopping
	userStop   bool   // 本次退出是否用户主动触发（区分崩溃）
	startedAt  time.Time
	crashCount int
	runGen     int64 // 每次 Start 自增，用于让崩溃重启只作用于其对应的那次运行

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
	Tasks    *tasks.Manager

	autoMu    sync.Mutex
	autoHook  func(AutoAction)     // 定时任务、崩溃自动重启执行后的回调（操作层用它写操作记录）
	startHook func(string)         // 起服务器进程之前的回调（音乐共享在这里写好桥接模组的配置、voice_host），见 hooks.go
	lineHook  func(string, string) // 服务器每输出一行的回调（游戏内 AI 认聊天），见 hooks.go

	pendMu      sync.Mutex // 整合包导入待决的任务 → 实例（处置时按任务找实例目录）
	pendingInst map[string]pendingRef
	resolveMu   sync.Mutex // 待决文件的处置一次只做一批，见 ResolveImportPending
}

func NewManager() (*Manager, error) {
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}, Tasks: tasks.NewManager()}
	seen := map[string]bool{} // 已经登记过的目录（真实路径）：同一个文件夹从两条路都能找到时只算一次
	load := func(dir string) {
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
			return
		}
		seen[key] = true
		if _, dup := m.insts[s.Name]; dup {
			log.Printf("实例名冲突，已跳过重复目录：%s（%s）", s.Name, dir)
			return
		}
		m.insts[s.Name] = &Instance{Settings: s, Dir: dir, Console: newConsole()}
	}
	// 多根扫描：内置默认根 + 配置默认根 + 历史自定义根（支持实例散落于不同盘符/目录）
	for _, root := range app.InstanceRoots() {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue // 根目录不存在/不可读则跳过，不再因默认根缺失整体失败
		}
		for _, e := range ents {
			if e.IsDir() {
				load(filepath.Join(root, e.Name()))
			}
		}
	}
	// 单独登记的文件夹（接管进来的服务器）：只读它自己，不看它的上一层。
	// 文件夹现在不在（比如在没插上的移动硬盘里）就跳过，登记留着，回来了照样认；
	// 登记只在这台服务器移出列表或被删掉时去掉（见 Delete）。
	for _, dir := range app.InstanceDirs() {
		load(dir)
	}
	m.startScheduler()
	return m, nil
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
	if i.Status() != "stopped" {
		return fmt.Errorf("请先停止服务器再删除")
	}
	// 这台服务器的单独登记（接管进来的才有）。登记的写法可能和 i.Dir 不一样——比如登记的是一个目录联接，
	// 而启动时是从某个存放根下面先扫到它的——所以按真实路径找；要赶在删文件夹之前找，删了就解不出真实路径了
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
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除文件失败: %w", err)
	}
	return nil
}

// 打包任务的占位：打包时读着实例目录里的世界和配置，这期间启动、删除都会打出坏包。
// map 惰性初始化，测试里手工构造的 Manager 也能用。
func (m *Manager) isPacking(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.packing[name]
}

// tryMarkPacking 占上打包的位；已经有人占着时返回 false。
func (m *Manager) tryMarkPacking(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.packing[name] {
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

// UpdateSettings 调整实例运行参数（内存、控制台编码、自定义 JVM 参数）并重新生成启动脚本。
// xmxMB<=0 表示不修改内存；consoleEnc 为空表示不修改编码；extraJVM 为 nil 表示不修改 JVM 参数。
func (m *Manager) UpdateSettings(name string, xmxMB, xmsMB int, consoleEnc string, extraJVM *[]string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	switch consoleEnc {
	case "auto", "utf-8", "gbk", "":
	default:
		return fmt.Errorf("不支持的控制台编码: %s", consoleEnc)
	}
	// 运行中控制台 goroutine 会并发读这些字段，写入必须持锁；
	// 落盘（saveInstance 序列化 i.Settings、writeRunBat 读取 i 字段）也在锁内完成，
	// 避免与并发的 UpdateSettings 竞争地读写 i.Settings
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if xmxMB > 0 {
		if xmxMB < 512 {
			return fmt.Errorf("最大内存不能低于 512MB")
		}
		if xmsMB < 128 || xmsMB > xmxMB {
			xmsMB = min(1024, xmxMB)
		}
		i.XmxMB, i.XmsMB = xmxMB, xmsMB
	}
	if consoleEnc != "" {
		i.ConsoleEncoding = consoleEnc
	}
	if extraJVM != nil {
		// 老版本的 log4shell 缓解参数强制保留：套用预设、清空或手动删掉后保存都会补回
		i.ExtraJVM = withJVMFlags(*extraJVM, log4jFlags(i))
	}
	if err := m.saveInstance(i); err != nil {
		return err
	}
	return writeRunBat(i)
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

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 40 {
		return fmt.Errorf("服务器名称需为 1~40 个字符")
	}
	if strings.ContainsAny(name, invalidNameChars) {
		return fmt.Errorf(`服务器名称不能包含字符 %s`, invalidNameChars)
	}
	if name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("服务器名称不合法")
	}
	return nil
}
