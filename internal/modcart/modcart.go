// Package modcart 自选模组的购物车：从 Modrinth 挑模组或插件放进购物车，算出还缺哪些前置，一次装进服务器。
//
// 叫法：界面和给人看的文字里这个东西叫「待装清单」（2026-11 改的，原来叫「购物车」）；代码里的标识符、
// 接口路径 /api/modcart、存盘文件 cart.json 和下面的注释都还沿用 cart / 购物车 / 车，指的是同一个东西。
//
// 购物车存在后端（<程序目录>/data/modcart/cart.json），不放浏览器里：应用内 AI 助手和 MCP 也要能看、能加，
// 新建服务器时实例还不存在，挑好的东西要先放进「草稿车」（键是空串），等实例建好再装。每辆车最多 50 项。
//
// 车里有两样东西：Modrinth 上的项目（Item），和人从自己电脑上添加的 jar（LocalFile，见 staged.go）。
// 本地文件暂存在 <程序目录>/data/modcart/local/ 下面，每辆车一个文件夹，登记在同一份 cart.json 里；两样合计不超过 50 项。
//
// 这个包只管购物车、Modrinth 查询、前置闭包、下载、暂存本地文件和本地依赖核对，不认识实例和任务；
// 操作层（ops/catalog/modcart.go）把它接到实例、后台任务和权限上。
package modcart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ModrinthAPI 默认的 Modrinth 接口地址（公开接口，不需要 Key）。
const ModrinthAPI = "https://api.modrinth.com/v2"

// MaxItems 一辆购物车最多放几项：AI 一次能看完整辆车（工具结果有 16KB 上限），装一次也不会太久。
const MaxItems = 50

// Draft 草稿车的键：新建服务器向导、AI 帮人开服时实例还不存在，先放这里。
const Draft = ""

// Toolbox 工具箱「导出整合包」用的那份清单的键。和草稿车分开：草稿车里的东西建服时会一起装进新服务器，
// 工具箱挑的是发给朋友客户端的模组，不该被装进去。带尖括号，不会和实例名（Windows 文件夹名）撞上。
// 这份清单是给客户端的，所以只在客户端用的模组也能放（见 Add、SearchQuery.ClientOK）。
const Toolbox = "<toolbox>"

// Item 购物车里的一项。展示用的字段（名称、图标、许可证）是加入时向 Modrinth 查来的，不信调用方给的。
type Item struct {
	ProjectID string `json:"projectId"` // Modrinth 的项目 ID（标准写法，不是 slug）
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`
	Kind      string `json:"kind"`              // Modrinth 的 project_type：mod / plugin
	License   string `json:"license,omitempty"` // 许可证的 SPDX 写法，如 MIT、LGPL-3.0-only；Modrinth 自定义的是 LicenseRef-…
	// ServerSide、ClientSide Modrinth 标的服务端、客户端要不要装（required / optional / unsupported / unknown）。
	// 导出整合包时写进 env；老清单里没有，按要装算
	ServerSide string    `json:"serverSide,omitempty"`
	ClientSide string    `json:"clientSide,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
}

// Skipped 加入时没放进去的项目和原因（查不到、只在客户端用的模组）。
type Skipped struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title,omitempty"`
	Reason    string `json:"reason"`
}

// Error 带状态码的错误（沿用 HTTP 状态码的含义，操作层原样转成 ops 的错误）。
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func errorf(code int, format string, a ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, a...)}
}

// Service 购物车和 Modrinth 客户端。并发安全。
type Service struct {
	dir      string // cart.json 所在的目录
	api      string // Modrinth 接口地址，测试时换成本机的假服务
	official bool   // 用的是真的 Modrinth：下载地址只认它的 CDN

	mu     sync.Mutex
	carts  map[string][]Item
	locals map[string][]LocalFile // 人从电脑上添加的 jar（见 staged.go），键同 carts
	// loadErr cart.json 在、却没读成（见 LoadError）。不为空时清单是空的，一律不写 cart.json、不清暂存的本地文件；
	// 有人要清单、要改清单时再读一次（retryLoadLocked），读成了就清掉
	loadErr *LoadError

	installMu  sync.Mutex
	installing map[string]bool
}

// 启动时 cart.json 读不出来（多半是杀毒、同步盘这类程序刚好占着）隔多久再读、再读几次（同 tunnel 包）。做成变量是为了测试能调小。
// 坏文件再读也是坏的，不等。
var (
	loadRetryWait = 200 * time.Millisecond
	loadRetries   = 3
)

// New 建好购物车，读回上次存下的。endpoint 空 = 真的 Modrinth。
// cart.json 在却没读成时不当空清单（见 LoadError）：读成之前不写 cart.json、不清暂存的本地文件。
func New(dir, endpoint string) *Service {
	s := &Service{dir: dir, api: strings.TrimRight(endpoint, "/"), carts: map[string][]Item{}, locals: map[string][]LocalFile{},
		installing: map[string]bool{}}
	if s.api == "" {
		s.api = ModrinthAPI
	}
	s.official = s.api == ModrinthAPI
	s.load()
	for i := 0; i < loadRetries && s.loadErr != nil && !s.loadErr.Corrupt; i++ {
		time.Sleep(loadRetryWait)
		s.load()
	}
	if s.loadErr != nil {
		log.Printf("待装清单没读出来，读成之前不许保存、不清暂存的本地文件: %v", s.loadErr)
		return s
	}
	s.sweepStaged() // 只在清单真读成了、或者确实是新装时清：没读成时清单是空的，按它清会把暂存的本地文件全删了
	return s
}

// LoadError cart.json 在，却没读成：读不出来（被别的程序占着、没有读取权限、同名的文件夹顶着、程序文件夹访问不了），
// 或者读出来了解析不了（文件坏了、是空的）。
//
// 以前读不出来就从空清单开始，启动时还按空清单把暂存的本地文件清掉，之后随便加一项就把原来的 cart.json 盖掉；
// 解析不了时改名成 cart.json.bad，暂存的本地文件照样清掉。现在这期间清单是空的，加入、移除、清空、添加和移除本地文件
// 一律拒绝，cart.json 和暂存的文件一个不动；读成了（占着的程序放开了，或者用户修好、挪走了文件）就恢复正常，不用重启。
// 解析不了时不再另存 .bad、从空清单开始：清单里登记着人从电脑上添加的文件，从空清单开始等于把它们丢了。同 tunnel.LoadError。
type LoadError struct {
	Corrupt bool  // true：读出来了但解析不了；false：读不出来
	Err     error // 系统或 JSON 解析给的原因（不带路径）
}

func (e *LoadError) Error() string {
	if e.Corrupt {
		return "待装清单的存档 cart.json（在 MineDeck 文件夹的 data\\modcart 里）内容坏了，读不出清单里的东西。为了不丢掉清单和你从电脑添加的文件，" +
			"修好之前不能往清单里加、移除或安装：" + e.Err.Error()
	}
	return "待装清单的存档 cart.json（在 MineDeck 文件夹的 data\\modcart 里）读不出来（可能被别的程序占着，或者没有读取权限）。" +
		"为了不丢掉清单和你从电脑添加的文件，读出来之前不能往清单里加、移除或安装：" + e.Err.Error()
}

func (e *LoadError) Unwrap() error { return e.Err }

// Hint 界面报错正文接的那句话（60 字以内，不带路径和英文句子，见 app.js 的 errSummary）。两种情况下一步不一样，分开说。
func (e *LoadError) Hint() string {
	if e.Corrupt {
		return "清单的存档 cart.json 内容坏了。用记事本修好它，或者改名挪走后从空清单开始"
	}
	return "清单的存档 cart.json 读不出来，可能被别的程序占着。关掉占用它的程序后点「重试」"
}

// load 读 cart.json，分三种情况：
//   - 文件不在：新装，或者从没往清单里放过东西，空清单是对的。要程序文件夹看得到才算（见 reallyMissing）；
//   - 文件在但读不出来（被占着、没权限、同名文件夹顶着）：记进 s.loadErr；
//   - 读出来了但解析不了（坏 JSON、空文件、没有 carts 这一项）：记进 s.loadErr（Corrupt）。MineDeck 写的文件总带着 carts。
//
// s.loadErr 不为空时 s.carts、s.locals 不动。调用方持有 s.mu；New 里别人还拿不到 s，不用锁。
func (s *Service) load() {
	b, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		if gone := s.reallyMissing(); gone != nil {
			s.loadErr = &LoadError{Err: gone}
			return
		}
		s.loadErr = nil
		return
	}
	if err != nil {
		s.loadErr = &LoadError{Err: noPath(err)}
		return
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // 记事本存的 UTF-8 可能带 BOM：提示里让人用记事本修，修完不能因为这个又读不出来
	var f cartFile
	switch err := json.Unmarshal(b, &f); {
	case len(bytes.TrimSpace(b)) == 0:
		s.loadErr = &LoadError{Corrupt: true, Err: errors.New("文件是空的")}
		return
	case err != nil:
		s.loadErr = &LoadError{Corrupt: true, Err: jsonErrAt(b, err)}
		return
	case f.Carts == nil:
		s.loadErr = &LoadError{Corrupt: true, Err: errors.New(`文件里找不到 "carts" 这一项`)}
		return
	}
	s.loadErr = nil
	s.carts, s.locals = f.Carts, s.checkedLocals(f.Locals)
}

// reallyMissing 读 cart.json 报「不存在」以后再确认一次，是真的新装才返回 nil，否则返回读不出来的原因。
// Windows 上路径里某一级不在（网络盘断了、U 盘拔了、程序文件夹暂时访问不了）也报「不存在」，当成新装的话，
// 路径一恢复，下一次保存就把原文件盖掉了（同 tunnel 包的 reallyMissing，2026-11-15 Codex 复审 P1）。
// data\modcart 在第一次存清单时才建，新装时它和 data 都可能还不在：所以从程序文件夹（cart.json 往上第三级）往下逐级看，
// 程序文件夹要看得到；在的那几级必须是文件夹；第一个不在的那一级必须是「上一级在、它不在」，就算新装。
func (s *Service) reallyMissing() error {
	root := filepath.Dir(filepath.Dir(s.dir))
	fi, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("程序文件夹访问不了：%w", noPath(err))
	}
	if !fi.IsDir() {
		return errors.New("程序文件夹的位置上不是文件夹")
	}
	rel, err := filepath.Rel(root, s.path())
	if err != nil {
		return err
	}
	cur := root
	parts := strings.Split(rel, string(filepath.Separator))
	for k, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		switch {
		case err == nil && k == len(parts)-1:
			return errors.New("刚才读的时候说不存在，再看又在了，这次先不读")
		case err == nil && !fi.IsDir():
			return fmt.Errorf("%s 的位置上不是文件夹", part)
		case err == nil:
		case fileNotFound(err):
			return nil
		default:
			return noPath(err)
		}
	}
	return nil
}

// jsonErrAt 解析报错带上大概在第几行：提示里让人用记事本修，得知道去哪儿找（同 tunnel 包）。
func jsonErrAt(b []byte, err error) error {
	var off int64 = -1
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	}
	if off < 0 || off > int64(len(b)) {
		return err
	}
	return fmt.Errorf("第 %d 行附近：%w", bytes.Count(b[:off], []byte("\n"))+1, err)
}

// retryLoadLocked 上次没读成时再读一次，返回现在的状态（读成了、或者本来就读成了，是 nil）。调用方持有 s.mu。
// 没读成期间清单一直是空的、什么也存不进去，所以读成了直接换上，不会冲掉谁的改动。只读一次、不等：它在锁里。
func (s *Service) retryLoadLocked() error {
	if s.loadErr == nil {
		return nil
	}
	prev := s.loadErr.Error()
	s.load()
	if s.loadErr == nil {
		log.Printf("待装清单读出来了，恢复正常")
		return nil
	}
	if cur := s.loadErr.Error(); cur != prev {
		log.Printf("待装清单还是没读出来: %s", cur)
	}
	return s.loadErr
}

// LoadErr cart.json 在却没读成时返回 *LoadError（先再读一次）；读成了、或者文件本来就不在，返回 nil。
// 操作层查清单、装清单之前先问它：没读成时报出来，不拿一张空清单冒充「清单是空的」。
func (s *Service) LoadErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retryLoadLocked()
}

type cartFile struct {
	Version int                    `json:"version"`
	Carts   map[string][]Item      `json:"carts"`            // 键：实例名；空串 = 草稿车
	Locals  map[string][]LocalFile `json:"locals,omitempty"` // 暂存的本地文件，键同上
}

func (s *Service) path() string { return filepath.Join(s.dir, "cart.json") }

// putLocked 把 inst 的车换成 cart：先写盘，写成了才改内存（写盘失败时内存和磁盘还是同一个样子）。调用方持有 s.mu。
func (s *Service) putLocked(inst string, cart []Item) error {
	next := maps.Clone(s.carts)
	if len(cart) == 0 {
		delete(next, inst)
	} else {
		next[inst] = cart
	}
	if err := s.write(next, s.locals); err != nil {
		return err
	}
	s.carts = next
	return nil
}

// putLocalsLocked 把 inst 的本地文件登记换成 list：同 putLocked，先写盘再改内存。调用方持有 s.mu。
func (s *Service) putLocalsLocked(inst string, list []LocalFile) error {
	next := maps.Clone(s.locals)
	if len(list) == 0 {
		delete(next, inst)
	} else {
		next[inst] = list
	}
	if err := s.write(s.carts, next); err != nil {
		return err
	}
	s.locals = next
	return nil
}

// write 写 cart.json（先写临时文件再改名）。报错里不带路径：这些话会原样给到界面和 AI。
// cart.json 没读成（s.loadErr）时一律不写：内存里是空清单，写下去就把原文件盖掉了。各个改清单的入口在改之前就已经
// 用 retryLoadLocked 挡下；这里再挡一道，兜住以后新加的写盘点。
func (s *Service) write(carts map[string][]Item, locals map[string][]LocalFile) error {
	if s.loadErr != nil {
		return s.loadErr
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return errorf(500, "保存待装清单失败：%v", noPath(err))
	}
	b, _ := json.MarshalIndent(cartFile{Version: 1, Carts: carts, Locals: locals}, "", "  ")
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return errorf(500, "保存待装清单失败：%v", noPath(err))
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		os.Remove(tmp)
		return errorf(500, "保存待装清单失败：%v", noPath(err))
	}
	return nil
}

// List 一辆车里的东西（按加入的先后）。
func (s *Service) List(inst string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.carts[inst])
}

// Add 把项目加进购物车（ids 可以是项目 ID 或 slug）。先向 Modrinth 查一遍：查不到的、只在客户端用的模组不放进去，
// 在 skipped 里说明原因；已经在车里的不重复放。全都放不进去时报错。返回整辆车。
func (s *Service) Add(ctx context.Context, inst string, ids []string) ([]Item, []Skipped, error) {
	want := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(want, id) {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return nil, nil, errorf(400, "没有给要加入的项目")
	}
	if len(want) > MaxItems {
		return nil, nil, errorf(400, "一次最多加 %d 项", MaxItems)
	}
	if err := s.LoadErr(); err != nil { // 存不进去就不用先去问 Modrinth 了
		return nil, nil, err
	}
	projects, err := s.projects(ctx, want)
	if err != nil {
		return nil, nil, err
	}
	var add []Item
	var skipped []Skipped
	for _, id := range want {
		p, ok := projects.find(id)
		switch {
		case !ok:
			skipped = append(skipped, Skipped{ProjectID: id, Reason: notFound})
		case p.ProjectType != "mod" && p.ProjectType != "plugin":
			skipped = append(skipped, Skipped{ProjectID: p.ID, Title: p.Title, Reason: "这是" + kindName(p.ProjectType) + "，不是模组或插件"})
		case p.ProjectType == "mod" && p.ServerSide == "unsupported" && inst != Toolbox:
			skipped = append(skipped, Skipped{ProjectID: p.ID, Title: p.Title, Reason: "只在客户端用的模组，装到服务器上没有用"})
		case slices.ContainsFunc(add, func(it Item) bool { return it.ProjectID == p.ID }):
			// 同一个项目给了 ID 又给了 slug
		default:
			add = append(add, Item{ProjectID: p.ID, Slug: p.Slug, Title: p.Title, Icon: p.IconURL, Kind: p.ProjectType,
				License: p.License.ID, ServerSide: p.ServerSide, ClientSide: p.ClientSide, AddedAt: time.Now().UTC()})
		}
	}
	if len(add) == 0 {
		why := make([]string, 0, len(skipped))
		code := 404 // 全都查不到才是 404
		for _, x := range skipped {
			why = append(why, fmt.Sprintf("%s：%s", orID(x.Title, x.ProjectID), x.Reason))
			if x.Reason != notFound {
				code = 400
			}
		}
		return nil, skipped, errorf(code, "没有加入任何项目（%s）", strings.Join(why, "；"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.retryLoadLocked(); err != nil {
		return nil, nil, err
	}
	cart := slices.Clone(s.carts[inst])
	n := len(cart)
	for _, it := range add {
		if !slices.ContainsFunc(cart, func(x Item) bool { return x.ProjectID == it.ProjectID }) {
			cart = append(cart, it)
		}
	}
	if nl := len(s.locals[inst]); len(cart)+nl > MaxItems { // 本地文件和 Modrinth 项目合起来算
		return nil, nil, errorf(400, "待装清单最多放 %d 项，现在已经有 %d 项", MaxItems, n+nl)
	}
	if len(cart) > n { // 都已经在车里了就不用写盘
		if err := s.putLocked(inst, cart); err != nil {
			return nil, nil, err
		}
	}
	return slices.Clone(cart), skipped, nil
}

const notFound = "Modrinth 上找不到这个项目"

// kindName Modrinth 项目类型的中文说法。
func kindName(t string) string {
	switch t {
	case "modpack":
		return "整合包"
	case "resourcepack":
		return "资源包"
	case "shader":
		return "光影"
	case "datapack":
		return "数据包"
	}
	return "「" + t + "」类型的项目"
}

func orID(title, id string) string {
	if title != "" {
		return title
	}
	return id
}

// Remove 从车里拿掉一项（项目 ID 或 slug）。车里没有它也不报错。返回整辆车。
func (s *Service) Remove(inst, id string) ([]Item, error) {
	return s.RemoveMany(inst, []string{id})
}

// RemoveMany 拿掉几项。装完以后只拿掉装上的那些：下载的那几分钟里新加进来的不能跟着清掉。
func (s *Service) RemoveMany(inst string, ids []string) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.retryLoadLocked(); err != nil {
		return nil, err
	}
	cart := slices.DeleteFunc(slices.Clone(s.carts[inst]), func(it Item) bool {
		return slices.Contains(ids, it.ProjectID) || slices.Contains(ids, it.Slug)
	})
	if len(cart) == len(s.carts[inst]) {
		return slices.Clone(cart), nil
	}
	if err := s.putLocked(inst, cart); err != nil {
		return nil, err
	}
	return slices.Clone(cart), nil
}

// Clear 清空一辆车里的 Modrinth 项目。人从电脑上添加的本地文件不动（AI 也能调清空，不能把人放的文件清掉），
// 连本地文件一起清用 ClearAll。
func (s *Service) Clear(inst string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.retryLoadLocked(); err != nil {
		return err
	}
	if len(s.carts[inst]) == 0 {
		return nil
	}
	return s.putLocked(inst, nil)
}

// Fingerprint 车里有哪些项目的指纹（不看顺序）。AI 的安装提案拿它当前置条件：确认时车里的东西变了，提案作废。
func Fingerprint(items []Item) string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ProjectID)
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:8])
}

// Merge 合并几辆车（去掉重复的项目，保留先出现的）：装实例的车时可以把草稿车一起带上。
func Merge(carts ...[]Item) []Item {
	var out []Item
	for _, c := range carts {
		for _, it := range c {
			if !slices.ContainsFunc(out, func(x Item) bool { return x.ProjectID == it.ProjectID }) {
				out = append(out, it)
			}
		}
	}
	return out
}

// BeginInstall 占住一台实例的安装：同一台同时只装一批（两批一起下会抢同一个临时文件、重复补同一个前置）。
// 返回的函数在装完时调用。
func (s *Service) BeginInstall(inst string) (func(), error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if s.installing[inst] {
		return nil, errorf(409, "正在给「%s」装模组，等这一批装完再装", inst)
	}
	s.installing[inst] = true
	return func() {
		s.installMu.Lock()
		delete(s.installing, inst)
		s.installMu.Unlock()
	}, nil
}

// IsNotFound 是不是「Modrinth 上没有这个」。
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == 404
}
