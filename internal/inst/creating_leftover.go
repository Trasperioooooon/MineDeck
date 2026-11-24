package inst

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/linkmode"
	"minedeck/internal/tasks"
	"minedeck/internal/winname"
)

// ErrCreatingConflict 半成品目录当前正在创建或已是登记服务器，拒绝删除时包装此错误（操作层映射为 409）。
var ErrCreatingConflict = errors.New("creating leftover conflict")

// WriteCreatingMarker 在还没登记的新目录里写下「这次创建还没完成」。新建、导入整合包、导入现成服务端和交接包都经这里。
// 只写我们自己的记号文件，不碰世界、配置。已经登记的服务器不靠这个文件判断。
// 写之前先把 dir 所在的存放位置记进「正在建的位置」（A38）：第一次往一个新位置里建服、记号放下了还没建完程序就崩了，
// 下次启动找没建完的文件夹时也扫得到那里。不记成扫描根：建不成的话，那个位置里别的带 instance.json 的文件夹
// 不该因此进服务器列表。建成了由登记处记成扫描根（markCreated）。
func WriteCreatingMarker(dir, name string) error {
	app.RememberCreatingRoot(filepath.Dir(dir))
	return writeFileNoHardlink(filepath.Join(dir, CreatingMarker), []byte(strings.TrimSpace(name)+"\n"), 0o644)
}

// markCreated 登记的收尾，instance.json 写好以后调（completeCreate、FinalizeCreate）：先把所在的存放位置记成扫描根，再去掉创建记号。
// 先后不能反：两步之间程序没了，文件夹里带着记号又有 instance.json，下次启动时会把位置补记成扫描根（promoteCreatedRoots），
// 这台照常进列表；先去记号的话，这台既不在列表里，也不算没建完的，什么线索都不留。
// 返回的只是记号去没去掉：去不掉不算登记失败，有 instance.json 的不会被当成半成品，下次启动时去掉记号。
func markCreated(dir string) error {
	app.RememberRoot(filepath.Dir(dir))
	afterRootRemembered(dir)
	return ClearCreatingMarker(dir)
}

// discardCreatingDir 建服、导入失败时清掉 dir（discardCreating 已经核对过是这次任务预留的、不是链接）。
// 只有这次任务放过创建记号的才整个删：放记号之前失败的，预留的路径上要是冒出了别人的文件夹（人自己、别的程序放的），
// 里面的东西不是这次建的，只删得掉空文件夹，删不掉照实报错（A48）。四条路（新建、整合包、现成服务端、交接包）都是
// 建好文件夹紧接着放记号，中间不写别的。
// 整个删到一半（有文件被别的程序占着）时记号多半已经先没了，剩下的文件夹既不在列表里、也不算没建完的，没处可管：
// 还是真文件夹、又没有 instance.json 的，把记号放回去，由「没建完的文件夹」那里统一处理。
func discardCreatingDir(dir, name string) error {
	if !creatingMarkerOwned(dir) {
		if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("文件夹里有不是这次建的东西，没有删（%w）", err)
		}
		return nil
	}
	err := os.RemoveAll(dir)
	if err != nil && plainCreatingDir(dir) && !creatingMarkerOwned(dir) {
		if registered, _ := checkNoInstanceFiles(dir); !registered {
			_ = WriteCreatingMarker(dir, name)
		}
	}
	return err
}

// DiscardCreate 交接包导入失败（cause 是失败原因）时清掉 ReserveCreate 预留、还没登记的文件夹，和新建、导入整合包失败时
// 同一套（discardFailed）。返回的错误要照实告诉人：文件夹里还留着东西。
func (m *Manager) DiscardCreate(cause error, name, dir string) error {
	return m.discardFailed(cause, name, dir)
}

// FailLeftover 建服、导入失败，半成品又没清干净（cleanupErr）时结束任务：失败原因后面接上清理失败的那句，任务记上
// cleanupFailed，界面按它说去「没建完的文件夹」里处理（不按这几个字认）。新建、整合包、现成服务端（failCreate）和交接包都经这里。
func FailLeftover(t *tasks.Task, err, cleanupErr error) {
	t.MarkCleanupFailed()
	t.Fail(fmt.Errorf("%w；清理半成品失败：%v", err, cleanupErr))
}

// errCreateDirTaken 预留（reserveInstanceDir）以后、建文件夹之前，那个位置上已经有了一个文件夹（人刚放进来的、别的程序建的）。
var errCreateDirTaken = errors.New("这个位置已经有一个同名文件夹（可能是刚放进来的），没有动它")

// MkdirCreating 建这次创建任务的服务器文件夹，放上创建记号（新建、导入整合包、导入现成服务端、交接包都经这里）。
// 本层用 os.Mkdir 而不是 MkdirAll：预留以后、建文件夹之前别人在同一个位置放了文件夹的话，MkdirAll 照样成功、紧接着放上
// 记号，之后失败就会按「自己建的」整个删掉（A48）。已经有了就回 errCreateDirTaken，失败时不进清理（见 discardFailed）。
// 记号在登记成功之前一直留着：进程中途没了，下次可以认出来删；已经有 instance.json 的不靠它清理。
func MkdirCreating(dir, name string) error {
	beforeCreateDir(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w：%s", errCreateDirTaken, dir)
		}
		return err
	}
	return WriteCreatingMarker(dir, name)
}

// discardFailed 建服、导入失败（cause 是失败原因）以后清半成品，四条路都经这里。失败在建文件夹那一步、那个位置上已经有
// 别人的文件夹的（errCreateDirTaken）不进清理：那不是这次建的，一样不动，也不再报一句「文件夹里有不是这次建的东西」。
func (m *Manager) discardFailed(cause error, name, dir string) error {
	if errors.Is(cause, errCreateDirTaken) {
		return nil
	}
	return m.discardCreating(name, dir)
}

// beforeCreateDir MkdirCreating 建文件夹之前的那一刻，默认什么都不做。测试里换成往那个位置放东西的函数，
// 模拟预留以后、建文件夹之前别人放进来的文件夹。
var beforeCreateDir = func(dir string) {}

// afterRootRemembered markCreated 里「已经记成扫描根、还没去记号」的那一刻，默认什么都不做。
// 测试里换成查看盘上状态的函数，钉住两步的先后。
var afterRootRemembered = func(dir string) {}

// leftoverRoots 找没建完的文件夹要扫的位置：扫描根（app.InstanceRoots）加上正在建的位置（app.CreatingRoots）。
// 按 foldRoot 去重：Windows 上只差大小写的两条是同一个文件夹，留着两条的话里面的半成品会列两遍、编号还一样。
func leftoverRoots() []string {
	var roots []string
	seen := map[string]bool{}
	for _, r := range append(app.InstanceRoots(), app.CreatingRoots()...) {
		if k := foldRoot(r); !seen[k] {
			seen[k] = true
			roots = append(roots, filepath.Clean(r))
		}
	}
	return roots
}

// foldRoot 比较存放位置用的写法：化简，只在 Windows 上再转小写（那里只差大小写的是同一个文件夹）。
// 别的系统上原样比：区分大小写的卷上（Linux、Mac 的区分大小写 APFS）只差大小写的是两个文件夹，转小写去重会只留先出现的
// 那一条，同一个编号指谁就取决于先后了。默认不区分大小写的 APFS 上，同一个文件夹会列两遍、编号不同，删哪个都是删它，无害。
func foldRoot(root string) string {
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		return strings.ToLower(root)
	}
	return root
}

// leftoverID 半成品的编号：「位置的短哈希/文件夹名」，短哈希是 foldRoot 以后的 FNV-32a，写成十进制。
// 不用位置在 leftoverRoots 里的序号：改默认存放位置、正在建的位置升成扫描根以后序号会挪，
// 界面上停着的旧编号就指到另一个位置里的同名文件夹去了（A38）。
func leftoverID(root, name string) string {
	return leftoverRootKey(root) + "/" + name
}

func leftoverRootKey(root string) string {
	h := fnv.New32a()
	h.Write([]byte(foldRoot(root)))
	return strconv.FormatUint(uint64(h.Sum32()), 10)
}

// promoteCreatedRoots 启动时、扫服务器之前调：正在建的位置里有文件夹带着创建记号、又已经有 instance.json，说明上次登记
// 写完 instance.json、还没把位置记成扫描根程序就没了（见 markCreated），这里补记成扫描根，这台照常进列表。
// 只认 instance.json 本身，移出列表留下的 .bak 不算。导入整合包、现成服务端和交接包都不让包里带的 instance.json 落到盘上
// （IsControlFile），带记号的文件夹里有 instance.json，除了下面跳过的，只会是登记时写的。
// 跳过单独登记过的文件夹：接管只认它自己，把它的上一层升成扫描根的话，那里别的带 instance.json 的文件夹会不声不响地进列表。
// 接管现在写文件之前就去掉创建记号（runAdopt）；以前的版本接管进来的没建完的文件夹记号还在，靠这一条挡住。
func promoteCreatedRoots() {
	adopted := app.InstanceDirs()
	isAdopted := func(dir string) bool {
		real := realPath(dir)
		return slices.ContainsFunc(adopted, func(d string) bool { return sameDir(real, realPath(d)) })
	}
	for _, r := range app.CreatingRoots() {
		names, _ := creatingChildren(r)
		for _, name := range names {
			dir := filepath.Join(r, name)
			if st, err := os.Lstat(filepath.Join(dir, "instance.json")); err == nil && st.Mode().IsRegular() && !isAdopted(dir) {
				app.RememberRoot(r)
				break
			}
		}
	}
}

// pruneCreatingRoots 启动时整理「正在建的位置」：已经成了扫描根的，和底下没有带创建记号的文件夹的，去掉。
// 只在启动时做，那时没有正在建的任务。位置本身现在读不到（比如在没插上的移动硬盘里）的留着，回来了照样找得到半成品。
func pruneCreatingRoots() {
	scan := app.InstanceRoots()
	var drop []string
	for _, r := range app.CreatingRoots() {
		names, readable := creatingChildren(r)
		if slices.ContainsFunc(scan, func(s string) bool { return app.SamePath(s, r) }) || readable && len(names) == 0 {
			drop = append(drop, r)
		}
	}
	if len(drop) > 0 {
		app.ForgetCreatingRoots(drop...)
	}
}

// creatingChildren root 下面找半成品时要看的子文件夹的名字：真文件夹（不是符号链接、目录联接），名字在 Windows 上能用，
// 里面有我们的创建记号。列半成品、启动时补记扫描根和整理「正在建的位置」都按这一份算。
// root 读不出来时 readable 为 false；root 本身是链接或目录联接时不扫它，算读得出、没有。
func creatingChildren(root string) (names []string, readable bool) {
	st, err := os.Lstat(root)
	if err != nil {
		return nil, false
	}
	if !linkmode.IsPlainDirInfo(st) {
		return nil, true
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, false
	}
	for _, e := range ents {
		if linkmode.IsPlainDirEntry(e) && winname.OK(e.Name()) &&
			creatingMarkerOwned(filepath.Join(root, e.Name())) {
			names = append(names, e.Name())
		}
	}
	return names, true
}

// ClearCreatingMarker 登记成功以后去掉记号。文件本来就没有不算失败。
func ClearCreatingMarker(dir string) error {
	err := os.Remove(filepath.Join(dir, CreatingMarker))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CreatingLeftover 一次创建没走完、进程重启以后还留在磁盘上的目录。
// 只包括「在某个服务器根目录的直接子目录里、有创建记号、没有 instance.json（及 .bak/.tmp）」的真文件夹。
// 接管进来的目录、用户自己放的目录没有这个记号，不会出现在这里。
type CreatingLeftover struct {
	ID       string `json:"id"`   // 交给删除接口的编号，不是任意路径
	Name     string `json:"name"` // 文件夹名
	RootName string `json:"root"` // 它所在的服务器根的文件夹名，给人看
	HasWorld bool   `json:"hasWorld"`
}

// CreatingLeftovers 列出可以手动删的半成品。只读，不改盘上的东西：有 instance.json（或移出列表留下的 .bak）的不列入，
// 它里面残留的记号留到下次启动时去掉（clearStaleCreatingMarkers），或者有人点删的时候去掉（DiscardCreatingLeftover）。
func (m *Manager) CreatingLeftovers() []CreatingLeftover {
	return m.scanCreatingLeftovers(nil)
}

// clearStaleCreatingMarkers 启动时（NewManager）去掉已经登记成服务器的文件夹里残留的创建记号：登记成功以后去记号那一下失败了
// （被占着），或者程序正好崩在那中间。留着的话，以后这台的 instance.json 要是没了，它会被当成半成品列出来、能被删掉。
// 启动时还没有正在创建的任务，去掉的只会是残留的。
func (m *Manager) clearStaleCreatingMarkers() {
	m.scanCreatingLeftovers(func(dir string) {
		if err := ClearCreatingMarker(dir); err != nil {
			log.Printf("已登记的服务器文件夹里残留的创建记号去不掉，下次启动再试：%s（%v）", dir, err)
		}
	})
}

// scanCreatingLeftovers 扫各个存放位置（leftoverRoots）下带创建记号的文件夹，列出没登记的半成品。
// 已经登记成服务器的不列，交给 registered（可以是 nil）。
func (m *Manager) scanCreatingLeftovers(registered func(dir string)) []CreatingLeftover {
	var out []CreatingLeftover
	for _, root := range leftoverRoots() {
		names, _ := creatingChildren(root)
		for _, name := range names {
			dir := filepath.Join(root, name)
			isServer, checkErr := checkNoInstanceFiles(dir)
			if isServer {
				if registered != nil {
					registered(dir)
				}
				continue
			}
			if checkErr != nil {
				continue // 读不清是不是已有服务器时不列出来删
			}
			if m.creatingOrRegistered(dir) {
				continue // 正在创建，或已在内存/接管登记里
			}
			out = append(out, CreatingLeftover{
				ID:       leftoverID(root, name),
				Name:     name,
				RootName: filepath.Base(root),
				HasWorld: dirHasWorld(dir),
			})
		}
	}
	if out == nil {
		out = []CreatingLeftover{}
	}
	return out
}

// DiscardCreatingLeftover 删掉 id 对应的半成品目录。confirm 必须为 true。
// 占住目录的「创建中」锁后再查一遍记号、instance.json（含 .bak/.tmp）、符号链接与目录联接。任何一条不满足都不删。
func (m *Manager) DiscardCreatingLeftover(id string, confirm bool) error {
	if !confirm {
		return fmt.Errorf("删除没建完的文件夹需要明确确认")
	}
	dir, err := m.leftoverDir(id)
	if err != nil {
		return err
	}
	release, err := m.acquireLeftoverDelete(dir)
	if err != nil {
		return err
	}
	defer release()

	st, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("找不到这个文件夹")
		}
		return fmt.Errorf("检查这个文件夹失败：%w", err)
	}
	if !linkmode.IsPlainDirInfo(st) {
		return fmt.Errorf("这个文件夹是指向别处的链接，没有删除")
	}
	if !creatingMarkerOwned(dir) {
		return fmt.Errorf("这个文件夹没有创建记号，没有删除")
	}
	registered, checkErr := checkNoInstanceFiles(dir)
	if registered {
		_ = ClearCreatingMarker(dir)
		return fmt.Errorf("%w：这个文件夹已经是服务器，没有删除", ErrCreatingConflict)
	}
	if checkErr != nil {
		return checkErr
	}
	if hasNestedCreatingLink(dir) {
		return fmt.Errorf("这个文件夹里面含有链接或目录联接，没有删除")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("未能删干净：%w。其他服务器和文件夹没有动，可稍后再试", err)
	}
	return nil
}

// leftoverDir 按编号（见 leftoverID）找回半成品的路径。斜杠前那段要正好对上 leftoverRoots 里的一个位置：
// 对不上（这个位置已经不扫了），或者对上不止一个（哈希撞了），都不去猜，回「没有这一项」。
func (m *Manager) leftoverDir(id string) (string, error) {
	key, name, ok := strings.Cut(id, "/")
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || !winname.OK(name) {
		return "", fmt.Errorf("没有这一项")
	}
	var root string
	matched := 0
	for _, r := range leftoverRoots() {
		if leftoverRootKey(r) == key {
			root, matched = r, matched+1
		}
	}
	if matched != 1 || !plainCreatingDir(root) {
		return "", fmt.Errorf("没有这一项")
	}
	clean := filepath.Clean(filepath.Join(root, name))
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("没有这一项")
	}
	return clean, nil
}

func plainCreatingDir(p string) bool {
	return linkmode.IsPlainDir(p)
}

// creatingMarkerOwned 记号是普通文件，目录本身是真文件夹（不是符号链接或目录联接）。
func creatingMarkerOwned(dir string) bool {
	if !plainCreatingDir(dir) {
		return false
	}
	st, err := os.Lstat(filepath.Join(dir, CreatingMarker))
	return err == nil && st.Mode().IsRegular()
}

// checkNoInstanceFiles 确认目录里没有 instance.json、移出列表留底的 instance.json.bak 或正在落盘的 instance.json.tmp。
// 遇到权限不足、共享冲突等非「不存在」错误时返回 checkErr，绝不当成「没有配置文件」放行删除。
func checkNoInstanceFiles(dir string) (registered bool, checkErr error) {
	for _, name := range []string{"instance.json", settingsBackup} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("没法确认这个文件夹是不是已有服务器（%w），没有删除", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "instance.json.tmp")); err == nil {
		return false, fmt.Errorf("%w：这个文件夹正在写入服务器配置，暂时不能删除", ErrCreatingConflict)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("没法确认这个文件夹是不是已有服务器（%w），没有删除", err)
	}
	return false, nil
}

func hasNestedCreatingLink(root string) bool {
	found := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			found = true
			return fs.SkipAll
		}
		if p != root && linkmode.IsLink(d.Type()) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found || err != nil
}

func (m *Manager) creatingOrRegistered(dir string) bool {
	real := realPath(dir)
	key := dirKeyPrefix + strings.ToLower(real)
	m.mu.Lock()
	if m.creating[key] {
		m.mu.Unlock()
		return true
	}
	for _, inst := range m.insts {
		ir := realPath(inst.Dir)
		if sameDir(real, ir) || within(real, ir) || within(ir, real) {
			m.mu.Unlock()
			return true
		}
	}
	m.mu.Unlock()
	for _, d := range app.InstanceDirs() {
		dr := realPath(d)
		if sameDir(real, dr) || within(real, dr) || within(dr, real) {
			return true
		}
	}
	return false
}

func (m *Manager) acquireLeftoverDelete(dir string) (func(), error) {
	real := realPath(dir)
	key := dirKeyPrefix + strings.ToLower(real)
	m.mu.Lock()
	if m.creating[key] {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w：这个文件夹正在创建服务器，暂时不能删除", ErrCreatingConflict)
	}
	for _, inst := range m.insts {
		ir := realPath(inst.Dir)
		if sameDir(real, ir) || within(real, ir) || within(ir, real) {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w：这个文件夹已经是列表里的服务器，没有删除", ErrCreatingConflict)
		}
	}
	if m.creating == nil {
		m.creating = map[string]bool{}
	}
	m.creating[key] = true
	m.mu.Unlock()

	for _, d := range app.InstanceDirs() {
		dr := realPath(d)
		if sameDir(real, dr) || within(real, dr) || within(dr, real) {
			m.releaseCreating(key)
			return nil, fmt.Errorf("%w：这个文件夹已经是接管登记的服务器，没有删除", ErrCreatingConflict)
		}
	}
	return func() { m.releaseCreating(key) }, nil
}

func dirHasWorld(dir string) bool {
	names := []string{"world", "world_nether", "world_the_end"}
	if lv := LevelName(dir); lv != "" && lv != "world" {
		names = append(names, lv, lv+"_nether", lv+"_the_end")
	}
	for _, name := range names {
		if st, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name), "level.dat")); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}
