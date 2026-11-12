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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// Item 购物车里的一项。展示用的字段（名称、图标、许可证）是加入时向 Modrinth 查来的，不信调用方给的。
type Item struct {
	ProjectID string    `json:"projectId"` // Modrinth 的项目 ID（标准写法，不是 slug）
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Icon      string    `json:"icon,omitempty"`
	Kind      string    `json:"kind"`              // Modrinth 的 project_type：mod / plugin
	License   string    `json:"license,omitempty"` // 许可证的 SPDX 写法，如 MIT、LGPL-3.0-only；Modrinth 自定义的是 LicenseRef-…
	AddedAt   time.Time `json:"addedAt"`
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

	installMu  sync.Mutex
	installing map[string]bool
}

// New 建好购物车，读回上次存下的。endpoint 空 = 真的 Modrinth。
// cart.json 读不懂时另存成 cart.json.bad，从空车开始（不让一个坏文件挡住整个功能）。
func New(dir, endpoint string) *Service {
	s := &Service{dir: dir, api: strings.TrimRight(endpoint, "/"), carts: map[string][]Item{}, locals: map[string][]LocalFile{},
		installing: map[string]bool{}}
	if s.api == "" {
		s.api = ModrinthAPI
	}
	s.official = s.api == ModrinthAPI
	b, err := os.ReadFile(s.path())
	if err == nil {
		var f cartFile
		if json.Unmarshal(b, &f) == nil && f.Carts != nil {
			s.carts = f.Carts
			s.locals = s.checkedLocals(f.Locals)
		} else {
			_ = os.Rename(s.path(), s.path()+".bad")
		}
	}
	s.sweepStaged()
	return s
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
func (s *Service) write(carts map[string][]Item, locals map[string][]LocalFile) error {
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
		case p.ProjectType == "mod" && p.ServerSide == "unsupported":
			skipped = append(skipped, Skipped{ProjectID: p.ID, Title: p.Title, Reason: "只在客户端用的模组，装到服务器上没有用"})
		case slices.ContainsFunc(add, func(it Item) bool { return it.ProjectID == p.ID }):
			// 同一个项目给了 ID 又给了 slug
		default:
			add = append(add, Item{ProjectID: p.ID, Slug: p.Slug, Title: p.Title, Icon: p.IconURL, Kind: p.ProjectType,
				License: p.License.ID, AddedAt: time.Now().UTC()})
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
