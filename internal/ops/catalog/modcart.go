package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/tasks"
)

// 自选模组 + 购物车（界面和给人看的文字里叫「待装清单」，代码和注释沿用 cart / 购物车 / 车，见 internal/modcart 的包说明）。
//
// 风险等级：搜索、看车、加进车、拿出来、清空、查前置都是读操作——购物车只是一张暂存的清单，不动服务器里的任何文件，
// AI 可以直接帮人挑；真正往服务器里放文件的 modcart.install 是写操作，AI 发起时生成提案卡片，卡片上逐个列出要下载的文件。
// 购物车按实例分开存；inst 不填是「草稿车」：新建服务器时实例还不存在，先挑好放这里，建好以后 install 带上 includeDraft 一起装。
//
// 车里还能放人从自己电脑上添加的 jar（本地文件，见 internal/modcart/staged.go）。这部分的规矩：
//   - 添加（modcart.local.add）、移除（modcart.local.remove）是仅人工：文件是人给的，AI 助手和 MCP 客户端不能往里放，也不能拿掉。
//     不限本机（同整合包上传 instance.import）：局域网里用手机打开界面也能加。
//   - AI 看得到清单里有哪些本地文件（modcart.list，只有文件名、大小和读出来的模组信息，没有路径），能把它们算进前置检查；
//     AI 调 modcart.clear 只清 Modrinth 项目，本地文件留着。
//   - 安装时本地文件和 Modrinth 项目一样要「人看过」：界面带上 expectLocal（它当时列着的文件的 SHA-1），车里有本地文件
//     而请求没带、或者对不上，就不装；AI 发起的走提案，本地文件算进前置条件的指纹，卡片上逐个列出要拷进去的文件。
//   - 校验在这一层做（不信网页层和调用方）：文件名、扩展名、是不是 zip、大小、inst.IsControlFile。

type modcartArgs struct {
	Inst         string   `json:"inst"`
	Q            string   `json:"q"`
	Core         string   `json:"core"`
	MC           string   `json:"mc"`
	Category     string   `json:"category"`
	Offset       int      `json:"offset"`
	ProjectIDs   []string `json:"projectIds"`
	ProjectID    string   `json:"projectId"`
	WithDeps     *bool    `json:"withDeps"`
	IncludeDraft bool     `json:"includeDraft"`
	// Expect 发起安装的人当时看到的购物车（项目 ID）。给了就核对：和现在要装的不一样就不装。
	// 往车里加东西是读操作，AI 不用确认就能做；人点「安装」时装的必须是他看见的那些。
	Expect *[]string `json:"expect"`
	// ExpectLocal 发起安装的人当时看到的本地文件（SHA-1）。车里有本地文件时必须给，而且要和现在的对得上：
	// 上次向导半途放弃留下的文件、别的窗口加的文件，不能在人没看过的情况下装进服务器。
	ExpectLocal *[]string `json:"expectLocal"`
	// 本地文件：File 是网页层刚存下的上传临时文件，FileName 是人给的文件名，SHA1 是要移除的那个文件
	File     string `json:"file"`
	FileName string `json:"fileName"`
	SHA1     string `json:"sha1"`
}

// LocalUploadDir 网页层把上传的 jar 先存在这里（<程序目录>/cache/modcart-upload），再把路径交给 modcart.local.add。
// 那个操作只认这个目录里的文件：不能拿它去读主机上别的文件。
func LocalUploadDir() string { return filepath.Join(app.CacheDir, "modcart-upload") }

var (
	cartInst  = ops.String("实例名称（实例列表里的 name）；不填 = 草稿清单：服务器还没建好时先放这里，建好后 modcart_install 带上 includeDraft 一起装").Len(0, 100)
	cartCore  = ops.String("服务器核心，如 fabric、forge、neoforge、paper（不给 inst 时必填；给了 inst 就用实例的）").Len(0, 20)
	cartMC    = ops.String("Minecraft 版本，如 1.20.1（不给 inst 时装模组必填；给了 inst 就用实例的）").Len(0, 40)
	projectID = ops.String("Modrinth 项目 ID 或 slug（见 modcart_search 结果的 project_id）").Len(1, 100)
)

// cartErr 带上 modcart 给的状态码。
func cartErr(err error) error {
	var e *modcart.Error
	if errors.As(err, &e) {
		return ops.Wrap(e.Code, e.Err)
	}
	return err
}

// cartTarget 一次购物车操作针对什么：资源类型（mod / plugin）、Modrinth 加载器、按哪个 MC 版本过滤（插件不卡版本，同资源页）、
// 装到哪个目录。给了 inst 用实例的核心和版本，没给就用参数里的 core、mc。
type cartTarget struct {
	rtype   string
	loaders []string
	mc      string // 过滤用的版本（插件是空串）
	sub     string // mods / plugins
	inst    *inst.Instance
}

func (c *cat) cartTarget(p modcartArgs) (cartTarget, error) {
	core, mc := mcsrc.Core(p.Core), p.MC
	var t cartTarget
	if p.Inst != "" {
		i, err := c.get(p.Inst)
		if err != nil {
			return t, err
		}
		t.inst, core, mc = i, i.Core, i.MC
	} else if core == "" {
		return t, ops.Errorf(400, "要给实例名 inst，或者给核心 core 和 MC 版本 mc")
	}
	rtype, loaders, sub, ok := resourceSpec(core)
	if !ok {
		return t, ops.Errorf(400, "%s 核心装不了模组或插件", orEmpty(string(core)))
	}
	t.rtype, t.loaders, t.sub = rtype, loaders, sub
	if rtype == "mod" {
		if mc == "" {
			return t, ops.Errorf(400, "装模组要给 MC 版本 mc")
		}
		t.mc = mc
	}
	return t, nil
}

// localView 清单里一个本地文件给外面看的样子：没有路径。warn 是按这台服务器的核心看出来的提醒（加载器不对、读不出信息）。
type localView struct {
	SHA1     string    `json:"sha1,omitempty"` // 给 AI 的不带：移除、安装时的核对都只有人能做
	FileName string    `json:"fileName"`
	Size     int64     `json:"size"`
	Title    string    `json:"title"`
	ModID    string    `json:"modId,omitempty"`
	Version  string    `json:"version,omitempty"`
	Loader   string    `json:"loader,omitempty"`
	Warn     string    `json:"warn,omitempty"`
	AddedAt  time.Time `json:"addedAt"`
}

// aiLocalBudget 给 AI 的清单里，本地文件那一段最多多大（整个工具结果有 16KB 上限，Modrinth 项目那一段还要地方）。
const aiLocalBudget = 6 << 10

// cartView 列表类结果的形状：items 是 Modrinth 项目，local 是本地文件，count 是 items 的个数，total 是两样合计。
// 两个切片都不是 nil（JSON 里是 []）。t 给了就按那台服务器的加载器给本地文件写提醒。
// 给 AI 的（ai=true）本地文件不带 SHA-1，文件名很长、放不下时少列几个并在 localNote 里说明。
func cartView(items []modcart.Item, locals []modcart.LocalFile, t *cartTarget, ai bool) map[string]any {
	if items == nil {
		items = []modcart.Item{}
	}
	lv := make([]localView, 0, len(locals))
	size := 0
	for _, lf := range locals {
		v := localView{SHA1: lf.SHA1, FileName: lf.FileName, Size: lf.Size, Title: lf.Title, ModID: lf.ModID, Version: lf.Version,
			Loader: lf.Loader, AddedAt: lf.AddedAt}
		if t != nil {
			v.Warn = modcart.LoaderWarn(lf.Loader, t.loaders, t.mc)
		}
		if ai {
			v.SHA1 = ""
			b, _ := json.Marshal(v)
			if size += len(b) + 1; size > aiLocalBudget {
				break
			}
		}
		lv = append(lv, v)
	}
	out := map[string]any{"count": len(items), "items": items, "local": lv, "total": len(items) + len(locals)}
	if len(lv) < len(locals) {
		out["localNote"] = fmt.Sprintf("本地文件一共 %d 个，这里只列了前 %d 个", len(locals), len(lv))
	}
	return out
}

// view 一辆车现在的样子（见 cartView）。p 里给了 inst 或 core 就带上加载器提醒；给得不对（实例不存在之类）不报错，只是没有提醒。
func (c *cat) view(p modcartArgs, caller ops.Caller) map[string]any {
	var t *cartTarget
	if p.Inst != "" || p.Core != "" {
		if ct, err := c.cartTarget(p); err == nil {
			t = &ct
		}
	}
	return cartView(c.ModCart.List(p.Inst), c.ModCart.Locals(p.Inst), t, caller.Source.AI())
}

// installItems 这次要装的：实例的车，includeDraft 时再加上草稿车。
func (c *cat) installItems(p modcartArgs) []modcart.Item {
	items := c.ModCart.List(p.Inst)
	if p.IncludeDraft {
		items = modcart.Merge(items, c.ModCart.List(modcart.Draft))
	}
	return items
}

// installLocals 这次要装的本地文件：实例的车里的，includeDraft 时再加上草稿车里的。
func (c *cat) installLocals(p modcartArgs) []modcart.LocalFile {
	locals := c.ModCart.Locals(p.Inst)
	if p.IncludeDraft {
		locals = modcart.MergeLocal(locals, c.ModCart.Locals(modcart.Draft))
	}
	return locals
}

// toCopy 这次真要拷进服务器的本地文件：服务器里已经有一模一样的（plan.Local 里标了 Skip）不用拷。
func toCopy(plan *modcart.Plan, locals []modcart.LocalFile) []modcart.LocalFile {
	return slices.DeleteFunc(slices.Clone(locals), func(lf modcart.LocalFile) bool {
		return slices.ContainsFunc(plan.Local, func(lc modcart.LocalCheck) bool { return lc.SHA1 == lf.SHA1 && lc.Skip != "" })
	})
}

// sameLocals 要装的这些本地文件和 want（SHA-1）是不是同一批（不看顺序）。
func sameLocals(locals []modcart.LocalFile, want []string) bool {
	if len(locals) != len(want) {
		return false
	}
	have := make(map[string]bool, len(locals))
	for _, lf := range locals {
		have[lf.SHA1] = true
	}
	for _, h := range want {
		if !have[strings.ToLower(h)] {
			return false
		}
		delete(have, strings.ToLower(h))
	}
	return len(have) == 0
}

// checkLocalDest 本地文件落进实例目录之前过的关：文件名再查一遍，再过 inst.IsControlFile（别人给的文件落进实例目录前都要过它）。
// sub 给了就只查那个目录（mods 或 plugins）；没给（还不知道装进哪种服务器）两个都查。
func checkLocalDest(name string, sub ...string) error {
	if err := modcart.CheckLocalName(name); err != nil {
		return cartErr(err)
	}
	if len(sub) == 0 {
		sub = []string{"mods", "plugins"}
	}
	for _, d := range append([]string{""}, sub...) {
		if inst.IsControlFile(filepath.Join(d, name)) {
			return ops.Errorf(400, "「%s」这个名字是 MineDeck 自己要用的，不能当模组或插件放进服务器。改个名字再添加", name)
		}
	}
	return nil
}

// toInstall 计划里真要下载的文件（withDeps=false 时只装购物车里的）。
func toInstall(plan *modcart.Plan, withDeps bool) []modcart.File {
	return slices.DeleteFunc(slices.Clone(plan.Files), func(f modcart.File) bool { return f.FromDep && !withDeps })
}

func withDeps(p modcartArgs) bool { return p.WithDeps == nil || *p.WithDeps }

// sameProjects 要装的这些项目和 want（项目 ID）是不是同一批（不看顺序）。
func sameProjects(items []modcart.Item, want []string) bool {
	if len(items) != len(want) {
		return false
	}
	have := make(map[string]bool, len(items))
	for _, it := range items {
		have[it.ProjectID] = true
	}
	for _, id := range want {
		if !have[id] {
			return false
		}
		delete(have, id)
	}
	return len(have) == 0
}

// fileLabel 「Fabric API 0.92.2+1.20.1」，前置写明谁需要它。
func fileLabel(f modcart.File) string {
	s := f.Title + " " + f.VersionNumber
	if f.VersionType != "" {
		s += "（" + f.VersionType + " 版）"
	}
	if f.FromDep {
		s += "（前置，" + f.DependencyOf + " 需要）"
	}
	return s
}

func mib(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

func (c *cat) modcartOps() {
	c.reg.Register(&ops.Op{
		Name: "modcart.search", Title: "在 Modrinth 搜索模组或插件（待装清单）", Level: ops.Read,
		Desc: "按核心对应的加载器和 MC 版本搜 Modrinth（要联网），按下载量排序，一次 20 个，只在客户端用的模组不列出来。" +
			"q 留空就是这个核心和版本下最热门的；category 按类别筛；offset 往后翻（结果里 more=true 才有下一页，最多到第 5 页）。" +
			"给 inst 按实例的核心和版本搜；服务器还没建好时给 core 和 mc。结果的 project_id 用于 modcart_add。",
		Params: ops.Object(map[string]*ops.Schema{"q": ops.String("搜索词（英文名效果更好）；留空 = 最热门的").Len(0, 100),
			"inst": cartInst, "core": cartCore, "mc": cartMC,
			"category": ops.String("只看这个类别（可选）。模组能用：" + strings.Join(modcart.Categories("mod"), "、") +
				"；插件能用：" + strings.Join(modcart.Categories("plugin"), "、")).OneOf(modcart.AllCategories()...),
			"offset": ops.Integer("从第几条开始（可选，默认 0）：0、20、40、60、80，每次返回 20 条以内").Range(0, modcart.MaxOffset)}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if p.Category != "" && !slices.Contains(modcart.Categories(t.rtype), p.Category) {
				return nil, ops.Errorf(400, "%s没有「%s」这个类别，能用的有：%s", map[string]string{"mod": "模组", "plugin": "插件"}[t.rtype],
					p.Category, strings.Join(modcart.Categories(t.rtype), "、"))
			}
			res, err := c.ModCart.Search(ctx, modcart.SearchQuery{Q: p.Q, Type: t.rtype, Loaders: t.loaders, MC: t.mc,
				Category: p.Category, Offset: p.Offset})
			if err != nil {
				return nil, cartErr(err)
			}
			return map[string]any{"type": t.rtype, "hits": res.Hits, "offset": res.Offset, "total": res.Total, "more": res.More}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.list", Title: "查看待装清单", Level: ops.Read,
		Desc: "待装清单里有什么（按加入的先后，两样合计最多 50 项）：items 是 Modrinth 项目，local 是用户自己从电脑上添加的 jar" +
			"（只有文件名、大小和读出来的模组信息；这部分只有用户能加、能移除）。不给 inst 看草稿清单。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "core": cartCore, "mc": cartMC}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			return c.view(p, caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.add", Title: "把模组或插件加进待装清单", Level: ops.Read,
		Desc: "把 Modrinth 项目加进待装清单（一次可以加多个），只是暂存，不下载、不改服务器。加入时会向 Modrinth 核对：" +
			"查不到的、只在客户端用的模组不放进去，在 skipped 里说明原因。返回整份清单。每份清单最多 50 项（连本地文件一起算）。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst,
			"projectIds": ops.Array("要加入的 Modrinth 项目 ID 或 slug", projectID).MaxLen(modcart.MaxItems)}, "projectIds"),
		Run: func(ctx context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			if len(p.ProjectIDs) == 0 {
				return nil, ops.Errorf(400, "没有给要加入的项目")
			}
			if p.Inst != "" {
				if _, err := c.cartTarget(p); err != nil {
					return nil, err
				}
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			_, skipped, err := c.ModCart.Add(ctx, p.Inst, p.ProjectIDs)
			if err != nil {
				return nil, cartErr(err)
			}
			v := c.view(p, caller)
			if len(skipped) > 0 {
				v["skipped"] = skipped
			}
			return v, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.remove", Title: "从待装清单拿掉一项", Level: ops.Read,
		Desc:   "把一个 Modrinth 项目从待装清单里拿掉（清单里没有它也不报错）。用户自己添加的本地文件拿不掉。返回整份清单。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "projectId": projectID}, "projectId"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.ModCart.Remove(p.Inst, p.ProjectID); err != nil {
				return nil, cartErr(err)
			}
			return c.view(p, caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.clear", Title: "清空待装清单", Level: ops.Read,
		Desc: "清空待装清单里的 Modrinth 项目（只清这份清单，不动服务器里已经装上的文件）。" +
			"用户自己从电脑上添加的本地文件不会被清掉，那部分只有用户在界面上能清。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			// 人在界面上点「清空」：Modrinth 项目和本地文件都清。别的来源（AI 助手、MCP、定时任务）只清 Modrinth 项目：
			// 本地文件是人放的，放和拿都只许人
			if caller.Source.Human() {
				err = c.ModCart.ClearAll(p.Inst)
			} else {
				err = c.ModCart.Clear(p.Inst)
			}
			if err != nil {
				return nil, cartErr(err)
			}
			v := c.view(p, caller)
			if n := len(c.ModCart.Locals(p.Inst)); n > 0 {
				v["note"] = fmt.Sprintf("清单里还留着 %d 个本地文件：那是用户自己从电脑上添加的，只有用户能在界面上移除", n)
			}
			return v, nil
		},
	})

	// 本地文件：人把自己电脑上的 jar 放进清单。存文件的那半步在网页层（multipart 存成临时文件，见 web/modcart.go），
	// 校验和入库在这里。仅人工、不限本机，同整合包上传（instance.import）。
	c.reg.Register(&ops.Op{
		Name: "modcart.local.add", Title: "从电脑添加模组或插件文件", Level: ops.HumanOnly,
		Desc: "把一个已经上传好的 jar 放进待装清单（只是暂存，不动服务器）。文件名必须是单独一段、以 .jar 结尾，文件必须是能打开的 zip，" +
			"最大 256 MB；同一个文件再加一次不重复存，文件名相同内容不同的拒绝。返回整份清单，added 是这次加的那一个。",
		// 参数说明故意不写长度上限：参数不合规时 Run 不会被调到，上传的临时文件就没人删了；名字、大小都在 Run 里查
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例名称；不填 = 草稿清单"), "core": cartCore, "mc": cartMC,
			"file": ops.String("上传后存下的临时文件"), "fileName": ops.String("文件名")}, "file", "fileName"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			// 只认网页接口刚存进上传目录的文件：不能拿这个操作去读（或者删）主机上别的文件
			if p.File == "" || filepath.Dir(filepath.Clean(p.File)) != LocalUploadDir() {
				return nil, ops.Errorf(400, "文件不在上传目录里")
			}
			p.File = filepath.Clean(p.File)
			// 必须是个普通文件：下面的 os.Remove 连空文件夹也删，不能让它碰到上传目录里的别的东西
			if st, err := os.Lstat(p.File); err != nil || !st.Mode().IsRegular() {
				return nil, ops.Errorf(400, "没有收到文件，再添加一次")
			}
			defer os.Remove(p.File) // 放进清单时已经被挪走了；没放进去的不留
			if utf8Len(p.Inst) > 100 {
				return nil, ops.Errorf(400, "实例名太长")
			}
			if p.Inst != "" {
				if _, err := c.cartTarget(p); err != nil {
					return nil, err
				}
			}
			if err := checkLocalDest(p.FileName); err != nil {
				return nil, err
			}
			lf, dup, err := c.ModCart.AddLocal(p.Inst, p.File, p.FileName)
			if err != nil {
				return nil, cartErr(err)
			}
			v := c.view(p, caller)
			v["added"] = map[string]any{"sha1": lf.SHA1, "fileName": lf.FileName, "title": lf.Title, "size": lf.Size}
			v["duplicate"] = dup
			return v, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.local.remove", Title: "从待装清单移除本地文件", Level: ops.HumanOnly,
		Desc: "把一个本地文件从待装清单里拿掉（暂存的那份一起删，不动服务器里的文件）。清单里没有它也不报错。返回整份清单。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "core": cartCore, "mc": cartMC,
			"sha1": ops.String("要移除的文件的 SHA-1（modcart.list 的 local[].sha1）").Match("^[0-9a-fA-F]{40}$")}, "sha1"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.ModCart.RemoveLocal(p.Inst, strings.ToLower(p.SHA1)); err != nil {
				return nil, cartErr(err)
			}
			return c.view(p, caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.deps", Title: "检查待装清单的前置", Level: ops.Read,
		Desc: "联网算出待装清单要装的全部内容：files 是会下载的文件（fromDep 的是自动补的必装前置），missing 是清单之外还需要的必装前置" +
			"（modcart_install 默认会一起装，也可以先 modcart_add 放进清单里），optional 是可选前置（不自动装），conflicts 是不兼容的组合，" +
			"localHits 是服务器里已经有、安装时跳过的，unresolved 是找不到能装的版本的。清单里有用户添加的本地文件时：local 逐个列出" +
			"（recognized 表示在 Modrinth 上认出来了，它的前置已经一起算进去；warn 是加载器或版本对不上的提醒），" +
			"manual 是本地文件自己声明、哪儿都没找到的前置（只知道模组 ID，没法代为下载，要用户自己去找）。给 inst 时会对照服务器里已有的文件；" +
			"includeDraft=true 把草稿清单也算进来（和 modcart_install 带 includeDraft 时装的一样）。清单很满时 files 会少列，note 里说明。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "core": cartCore, "mc": cartMC,
			"includeDraft": ops.Boolean("给 inst 时把草稿清单也算进来（默认 false）")}),
		Run: func(ctx context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			items, locals := c.installItems(p), c.installLocals(p)
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			var local modcart.Local
			if t.inst != nil {
				local = c.ModCart.ScanLocal(ctx, filepath.Join(t.inst.Dir, t.sub))
			}
			plan, err := c.ModCart.ClosureWith(ctx, items, locals, local, t.loaders, t.mc)
			if err != nil {
				return nil, cartErr(err)
			}
			return depsView(len(items), plan, caller.Source.AI()), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.install", Title: "安装待装清单里的模组或插件", Level: ops.Write,
		Desc: "把这台实例待装清单里的模组或插件装进服务器的 mods / plugins：挑兼容的版本、自动补上必装前置（withDeps=false 只装清单里的），" +
			"在后台下载（核对 SHA-1），返回任务编号（进度见 task_get）。服务器里已经有的跳过；装完核对本地依赖，还缺的写在任务的提示里。" +
			"includeDraft=true 把草稿清单一起装进这台实例（新建服务器后用）。清单里用户自己添加的本地文件会一起拷进去" +
			"（目标位置已有同名文件：内容一样跳过，不一样不覆盖）。确认卡片上会列出要下载、要拷进去的每个文件。",
		Params: withInst(map[string]*ops.Schema{
			"withDeps":     ops.Boolean("自动补上必装前置（默认 true）"),
			"includeDraft": ops.Boolean("把草稿清单也一起装进这台实例（默认 false）"),
			"expect":       ops.Array("发起安装的人当时看到的待装清单里的项目 ID（界面会带上）；给了就核对，和现在要装的不一样就不装", ops.String("项目 ID")),
			"expectLocal": ops.Array("发起安装的人当时看到的本地文件的 SHA-1（界面会带上）。清单里有本地文件时必须给，和现在要装的不一样就不装",
				ops.String("SHA-1").Len(40, 40)).MaxLen(modcart.MaxItems),
		}),
		// expectLocal 是「人看过这些文件」的凭据，只能由人的界面给；AI 发起的安装走提案卡片，卡片上列着这些文件
		HumanParams: []string{"expectLocal"},
		Preview: func(ctx context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			items, locals := c.installItems(p), c.installLocals(p)
			if len(items) == 0 && len(locals) == 0 {
				return nil, ops.Errorf(400, "待装清单是空的")
			}
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			local := c.ModCart.ScanLocal(ctx, filepath.Join(t.inst.Dir, t.sub))
			plan, err := c.ModCart.ClosureWith(ctx, items, locals, local, t.loaders, t.mc)
			if err != nil {
				return nil, cartErr(err)
			}
			files, copies := toInstall(plan, withDeps(p)), toCopy(plan, locals)
			if len(files) == 0 && len(copies) == 0 { // 没有要下载、要拷的：不出一张什么都不做的卡片，直接说清楚
				var why []string
				if len(plan.Unresolved) > 0 {
					why = append(why, "找不到能装的版本："+unresolvedText(plan.Unresolved))
				}
				if len(plan.LocalHits) > 0 {
					why = append(why, "服务器里已经有了："+strings.Join(plan.LocalHits, "、")+"（可以用 modcart_remove 从清单里拿掉）")
				}
				if len(locals) > 0 { // 清单里的本地文件服务器上都有了：AI 拿不掉它们，告诉它该怎么跟用户说
					why = append(why, "清单里的本地文件是用户自己添加的，要由用户在界面上移除（或者让用户自己点「全部安装」：已经有的会自动跳过并从清单里拿掉）")
				}
				return nil, ops.Errorf(400, "没有要下载的文件。%s", strings.Join(why, "；"))
			}
			deps, size := 0, int64(0)
			// 前置条件：车里有哪些项目、有哪些本地文件，和这张卡片列出的确切文件。确认前车变了（多了、少了、换了本地文件），
			// 或者 Modrinth 上发了新版本，卡片作废
			pl := &ops.Plan{Pre: map[string]string{"待装清单": modcart.Fingerprint(items), "本地文件": modcart.LocalFingerprint(locals),
				"要装的文件": filesPrint(files, plan.LocalHits, copies)}}
			var localWarn []string
			for _, lf := range copies {
				after := t.sub + "/" + lf.FileName + "（本地文件，" + mib(lf.Size) + "）"
				if lc, ok := localCheck(plan, lf); ok {
					if lc.NameTaken {
						after = "不覆盖：" + t.sub + " 里已经有同名文件，内容不一样"
					}
					if lc.Warn != "" {
						localWarn = append(localWarn, lf.FileName+"："+lc.Warn)
					}
				}
				pl.Changes = append(pl.Changes, ops.Change{What: "放入本地文件 " + lf.FileName, Before: "未安装", After: after})
			}
			for _, f := range files {
				if f.FromDep {
					deps++
				}
				size += f.Size
				after := t.sub + "/" + f.FileName
				if f.License != "" {
					after += "（许可证 " + f.License + "）"
				}
				pl.Changes = append(pl.Changes, ops.Change{What: "安装 " + fileLabel(f), Before: "未安装", After: after})
			}
			pl.Summary = fmt.Sprintf("把待装清单里的 %d 项装进「%s」", len(items)+len(locals), t.inst.Name)
			if deps > 0 {
				pl.Summary += fmt.Sprintf("（另补 %d 个必装前置）", deps)
			}
			if len(files) > 0 {
				pl.Summary += fmt.Sprintf("，共下载 %d 个文件约 %s", len(files), mib(size))
			}
			if len(copies) > 0 {
				pl.Summary += fmt.Sprintf("，放入 %d 个用户从电脑上添加的本地文件", len(copies))
			}
			var warn []string
			if t.inst.Status() != "stopped" {
				warn = append(warn, "服务器正在运行：装完要重启才生效")
			}
			if len(plan.LocalHits) > 0 {
				warn = append(warn, "服务器里已经有、会跳过："+strings.Join(plan.LocalHits, "、"))
			}
			if len(plan.Unresolved) > 0 {
				warn = append(warn, "找不到能装的版本，不装："+unresolvedText(plan.Unresolved))
			}
			if len(plan.Conflicts) > 0 {
				warn = append(warn, "有冲突："+strings.Join(plan.Conflicts, "；"))
			}
			if len(localWarn) > 0 {
				warn = append(warn, "本地文件要留意："+strings.Join(localWarn, "；"))
			}
			if len(plan.Manual) > 0 {
				warn = append(warn, "本地文件还需要这些前置，没法代为下载，要自己去找："+manualText(plan.Manual))
			}
			if plan.LocalNote != "" {
				warn = append(warn, strings.TrimSuffix(plan.LocalNote, "。"))
			}
			pl.Warning = strings.Join(warn, "。")
			return pl, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			// 先占住这台实例再读车：上一批刚装完、正从车里拿掉装好的那几项时，这里读到的是拿掉以后的
			done, err := c.ModCart.BeginInstall(t.inst.Name)
			if err != nil {
				return nil, cartErr(err)
			}
			items, locals := c.installItems(p), c.installLocals(p)
			if len(items) == 0 && len(locals) == 0 {
				done()
				return nil, ops.Errorf(400, "待装清单是空的")
			}
			// 装的必须是人看过、确认过的那些。往车里加东西不用确认（AI 助手、别的窗口都能加），
			// 所以这里读到的车要和发起的人看到的对一遍：
			//   - 人在界面上点的：请求里带着他当时看到的项目（expect）；
			//   - AI 提的、人在卡片上确认的：确认那一刻的购物车指纹（卡片上列的文件在后台算完版本以后再对，见 installCart）。
			if p.Expect != nil && !sameProjects(items, *p.Expect) {
				done()
				return nil, ops.Errorf(409, "待装清单和你刚才看到的不一样了（AI 助手或者别的窗口可能改过）。先看一眼现在的清单，再点安装")
			}
			wantFiles := ""
			pre := ops.ConfirmedPre(ctx)
			if pre != nil {
				if modcart.Fingerprint(items) != pre["待装清单"] || modcart.LocalFingerprint(locals) != pre["本地文件"] {
					done()
					return nil, ops.Errorf(409, "确认的时候待装清单又变了，这次没有装。请让 AI 重新提一次")
				}
				wantFiles = pre["要装的文件"]
			}
			// 本地文件比 Modrinth 项目管得更严：expect 没给时不查（老的调用方），本地文件没给就不装。
			// 上次向导半途放弃留在草稿清单里的文件、别的窗口加的文件，没有哪个人看过，不能就这么装进服务器。
			// 走提案确认的那条路不看 expectLocal：人确认的是卡片上列的文件，上面已经按指纹核过了。
			if pre == nil && (len(locals) > 0 || p.ExpectLocal != nil) && (p.ExpectLocal == nil || !sameLocals(locals, *p.ExpectLocal)) {
				done()
				return nil, ops.Errorf(409, "清单里的本地文件和你刚才看到的不一样了（可能是上次没装完留下的，或者别的窗口加的）。先看一眼现在的清单，再点安装")
			}
			task, tctx := c.Mgr.Tasks.New(fmt.Sprintf("给「%s」安装待装清单里的 %d 项", t.inst.Name, len(items)+len(locals)),
				[]string{"挑版本、补前置", "下载文件", "放入本地文件", "核对本地依赖"})
			task.SetKind("mods")
			go func() {
				defer done()
				c.installCart(tctx, task, t, p, items, locals, wantFiles)
			}()
			return map[string]string{"taskId": task.ID()}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.mods.deps", Title: "核对已装模组的依赖", Level: ops.Read,
		Desc: "不联网：读 mods（插件服读 plugins）里每个模组自己声明的必需依赖，对照目录里实际有的模组 ID。" +
			"missing 是一个都没有的，stale 是有但版本不在要求范围里的，checked 是读了几个模组。手动放进来的、不在 Modrinth 上的模组按模组 ID 也算满足。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			missing, stale, n, err := modcart.CheckLocal(filepath.Join(t.inst.Dir, t.sub))
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			return map[string]any{"count": len(missing), "missing": missing, "stale": stale, "checked": n}, nil
		},
	})
}

// aiBudget 给 AI 的 modcart.deps 结果最多多大：工具结果超过 16 KB 会被截断，留出脱敏和边界标记的余量。
const aiBudget = 14 << 10

// depsView modcart.deps 的结果。界面要文件名、大小来画清单；给 AI 的只留它用得上的（项目 ID、名称、版本、许可证、谁需要它），
// 满满一车（50 项，每项再带一两个前置）时还放不下，就先不逐个列车里的（modcart_list 能看），再不行只给个数，
// 保证结果完整、不被截成半截 JSON。
func depsView(n int, plan *modcart.Plan, ai bool) map[string]any {
	locals, manual := plan.Local, plan.Manual
	if locals == nil { // 界面直接 .map：不能是 null
		locals = []modcart.LocalCheck{}
	}
	if manual == nil {
		manual = []modcart.ManualDep{}
	}
	v := map[string]any{"count": n, "conflicts": plan.Conflicts, "localHits": plan.LocalHits, "unresolved": plan.Unresolved,
		"local": locals, "manual": manual}
	if plan.LocalNote != "" {
		v["localNote"] = plan.LocalNote
	}
	if !ai {
		files := make([]modcart.File, 0, len(plan.Files))
		for _, f := range plan.Files { // 下载地址、哈希和原始依赖界面用不着
			f.URL, f.SHA1, f.Deps = "", "", nil
			files = append(files, f)
		}
		v["files"], v["missing"], v["optional"] = files, plan.Missing, plan.Optional
		return v
	}
	type aiFile struct {
		ProjectID    string `json:"projectId"`
		Title        string `json:"title"`
		Version      string `json:"version"`
		License      string `json:"license,omitempty"`
		FromDep      bool   `json:"fromDep,omitempty"`
		DependencyOf string `json:"dependencyOf,omitempty"`
	}
	type aiDep struct {
		ProjectID    string `json:"projectId"`
		Title        string `json:"title"`
		DependencyOf string `json:"dependencyOf,omitempty"`
	}
	files := make([]aiFile, 0, len(plan.Files))
	for _, f := range plan.Files {
		files = append(files, aiFile{f.ProjectID, f.Title, f.VersionNumber, f.License, f.FromDep, f.DependencyOf})
	}
	short := func(ds []modcart.DepInfo) []aiDep {
		out := make([]aiDep, 0, len(ds))
		for _, d := range ds {
			out = append(out, aiDep{d.Project.ProjectID, d.Project.Title, d.DependencyOf})
		}
		return out
	}
	v["files"], v["missing"], v["optional"] = files, short(plan.Missing), short(plan.Optional)
	// 本地文件给 AI 的只留文件名、名称、认没认出来和提醒（没有 SHA-1）
	type aiLocal struct {
		FileName   string `json:"fileName"`
		Title      string `json:"title"`
		Recognized bool   `json:"recognized"`
		ProjectID  string `json:"projectId,omitempty"`
		Warn       string `json:"warn,omitempty"`
		Covers     string `json:"covers,omitempty"`
		Skip       string `json:"skip,omitempty"`
	}
	al := make([]aiLocal, 0, len(locals))
	for _, lc := range locals {
		al = append(al, aiLocal{lc.FileName, lc.Title, lc.Recognized, lc.ProjectID, lc.Warn, lc.Covers, lc.Skip})
	}
	v["local"] = al
	fits := func() bool { b, _ := json.Marshal(v); return len(b) <= aiBudget }
	if !fits() && len(al) > 0 { // 文件名都很长的一车本地文件：先不逐个列（modcart_list 能看），要自己去找的前置留着
		v["local"] = []aiLocal{}
		v["localNote"] = strings.TrimSpace(plan.LocalNote + fmt.Sprintf(" 本地文件 %d 个没有逐个列出，见 modcart_list。", len(al)))
	}
	if !fits() {
		deps := slices.DeleteFunc(slices.Clone(files), func(f aiFile) bool { return !f.FromDep })
		v["files"] = deps
		v["note"] = fmt.Sprintf("结果太长：files 只列了自动补的 %d 个前置，待装清单里要装的 %d 项见 modcart_list", len(deps), len(files)-len(deps))
		if !fits() {
			v["files"] = []aiFile{}
			v["note"] = fmt.Sprintf("结果太长：一共要下载 %d 个文件（其中 %d 个是自动补的前置），没有逐个列出；要补的前置见 missing", len(files), len(deps))
		}
	}
	return v
}

// filesPrint 要装的确切文件（项目@版本）、要拷进去的本地文件（按内容）加上要跳过的已有文件的指纹，不看顺序。
func filesPrint(files []modcart.File, hits []string, copies []modcart.LocalFile) string {
	keys := make([]string, 0, len(files)+len(hits)+len(copies))
	for _, f := range files {
		keys = append(keys, f.ProjectID+"@"+f.VersionID)
	}
	for _, h := range hits {
		keys = append(keys, "已有:"+h)
	}
	for _, lf := range copies {
		keys = append(keys, "本地:"+lf.SHA1)
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:8])
}

// localCheck 这个本地文件在计划里的那一行。
func localCheck(plan *modcart.Plan, lf modcart.LocalFile) (modcart.LocalCheck, bool) {
	i := slices.IndexFunc(plan.Local, func(lc modcart.LocalCheck) bool { return lc.SHA1 == lf.SHA1 })
	if i < 0 {
		return modcart.LocalCheck{}, false
	}
	return plan.Local[i], true
}

// manualText 「ghost（Needy 需要）、…」。
func manualText(ms []modcart.ManualDep) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, m.ID+"（"+m.RequiredBy+" 需要）")
	}
	return strings.Join(parts, "、")
}

func utf8Len(v string) int { return len([]rune(v)) }

func unresolvedText(us []modcart.Unresolved) string {
	parts := make([]string, 0, len(us))
	for _, u := range us {
		parts = append(parts, u.Title+"："+u.Reason)
	}
	return strings.Join(parts, "；")
}

// installCart 后台任务：挑版本、补前置 → 下载 → 放入本地文件 → 核对本地依赖；装上的从购物车里拿掉（下载期间新加进车的不动）。
// locals 是这次要装的本地文件（发起时读到、核对过的那一批）。
// wantFiles 非空时是人在提案卡片上确认的那份文件清单的指纹：这里重新挑完版本、补完前置以后要和它对上，
// 对不上（确认之后 Modrinth 上发了新版本、前置变了）就一个文件都不下载、不拷。
func (c *cat) installCart(ctx context.Context, task *tasks.Task, t cartTarget, p modcartArgs, items []modcart.Item, locals []modcart.LocalFile, wantFiles string) {
	dir := filepath.Join(t.inst.Dir, t.sub)
	task.StartStep(0)
	if len(items) > 0 {
		task.Logf("待装清单里有 %d 个 Modrinth 项目：%s", len(items), itemTitles(items))
	}
	if len(locals) > 0 {
		task.Logf("待装清单里有 %d 个从电脑上添加的本地文件：%s", len(locals), localNames(locals))
	}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	local := c.ModCart.ScanLocal(rctx, dir)
	plan, err := c.ModCart.ClosureWith(rctx, items, locals, local, t.loaders, t.mc)
	cancel()
	if err != nil {
		task.Fail(err)
		return
	}
	if plan.LocalNote != "" {
		task.Logf("%s", plan.LocalNote)
	}
	if len(plan.Unresolved) > 0 {
		w := tasks.Warning{Kind: "unresolved", Title: fmt.Sprintf("%d 项找不到能装的版本", len(plan.Unresolved)),
			Note: "没有给这台服务器的核心和版本发布的文件，这次没有装。可以换别的模组，或者到 Modrinth 上看看有没有别的版本。"}
		for _, u := range plan.Unresolved {
			it := tasks.WarnItem{Name: u.Title + "：" + u.Reason}
			if u.ProjectID != "" {
				it.URL = "https://modrinth.com/project/" + u.ProjectID
			}
			w.Items = append(w.Items, it)
		}
		task.AddWarning(w)
	}
	if len(plan.Conflicts) > 0 {
		w := tasks.Warning{Kind: "conflicts", Title: "有冲突的组合", Note: "能装的照样装上了；服务器起不来时先拿掉其中一个。"}
		for _, s := range plan.Conflicts {
			w.Items = append(w.Items, tasks.WarnItem{Name: s})
		}
		task.AddWarning(w)
	}
	if len(plan.Manual) > 0 {
		w := tasks.Warning{Kind: "manual-deps", Title: fmt.Sprintf("有 %d 个前置要自己去找", len(plan.Manual)),
			Note: "这些前置是你添加的本地文件自己写明要的，我们只知道它的名字（模组 ID），没法替你下载。找到对应的 jar 以后，用「从电脑添加…」放进清单再装一次。"}
		for _, m := range plan.Manual {
			w.Items = append(w.Items, tasks.WarnItem{Name: m.ID + "（" + m.RequiredBy + " 需要）"})
		}
		task.AddWarning(w)
	}
	var noted []tasks.WarnItem
	for _, lc := range plan.Local {
		if lc.Warn != "" {
			noted = append(noted, tasks.WarnItem{Name: lc.FileName + "：" + lc.Warn})
		}
		if lc.Covers != "" {
			task.Logf("清单里的 %s 和本地文件 %s 是同一个，只装本地这一份", lc.Covers, lc.FileName)
		}
	}
	if len(noted) > 0 {
		task.AddWarning(tasks.Warning{Kind: "local-warn", Title: fmt.Sprintf("%d 个本地文件可能不适合这台服务器", len(noted)),
			Note: "照样放进去了。服务器起不来、或者它没生效时，先把它从服务器的文件夹里拿掉。", Items: noted})
	}
	for _, h := range plan.LocalHits {
		task.Logf("服务器里已经有 %s，跳过", h)
	}
	files, copies := toInstall(plan, withDeps(p)), toCopy(plan, locals)
	if wantFiles != "" && filesPrint(files, plan.LocalHits, copies) != wantFiles {
		task.Fail(errors.New("要装的文件和确认时卡片上列的不一样了（Modrinth 上可能刚发了新版本）。一个文件都没有下载，请让 AI 重新提一次"))
		return
	}
	if len(files) == 0 && len(copies) == 0 && len(plan.Unresolved) > 0 {
		task.Fail(fmt.Errorf("没有能装的文件：%s", unresolvedText(plan.Unresolved)))
		return
	}
	if len(files) == 0 && len(items) > 0 {
		task.Logf("清单里的 Modrinth 项目都已经在服务器里了，不用下载")
	}

	task.StartStep(1)
	installed := map[string]bool{} // 装上了（或者本来就有）的购物车项目
	for _, it := range items {
		if !slices.ContainsFunc(plan.Files, func(f modcart.File) bool { return f.ProjectID == it.ProjectID }) &&
			!slices.ContainsFunc(plan.Unresolved, func(u modcart.Unresolved) bool { return u.ProjectID == it.ProjectID }) {
			installed[it.ProjectID] = true // 不在要下载的里、也不在找不到的里：服务器里已经有了，或者有本地文件顶上
		}
	}
	var failed []tasks.WarnItem
	var firstErr error
	for i, f := range files {
		task.Logf("[%d/%d] %s：%s（%s）", i+1, len(files), fileLabel(f), f.FileName, mib(f.Size))
		_, skipped, err := c.ModCart.Download(ctx, f, dir, task.ProgressFn("下载 "+f.FileName))
		switch {
		case err != nil:
			task.Logf("  失败：%v", err)
			failed = append(failed, tasks.WarnItem{Name: f.FileName + "：" + err.Error()})
			if firstErr == nil {
				firstErr = err
			}
			continue
		case skipped:
			task.Logf("  已经有同名文件，跳过")
		}
		if !f.FromDep {
			installed[f.ProjectID] = true
		}
	}
	if len(failed) == len(files) && len(files) > 0 {
		task.Fail(firstErr)
		return
	}
	if len(failed) > 0 {
		task.AddWarning(tasks.Warning{Kind: "download-failed", Title: fmt.Sprintf("%d 个文件没下载成功", len(failed)),
			Note: "待装清单先不动：检查网络或代理后再装一次，已经装上的会自动跳过，只补没装上的。", Items: failed})
	}

	// 放入本地文件：一个个拷进 mods / plugins。装好的（拷进去了、或者服务器里本来就有一模一样的）从暂存里删掉；
	// 没装上的留在清单里，原因写进提示。同名但内容不同的文件不覆盖。
	task.StartStep(2)
	copied := 0
	var kept, broken []tasks.WarnItem
	for i, lf := range locals {
		if ctx.Err() != nil {
			break
		}
		lc, _ := localCheck(plan, lf)
		if lc.Skip != "" {
			task.Logf("[本地 %d/%d] %s：服务器里已经有一模一样的文件（%s），跳过", i+1, len(locals), lf.FileName, lc.Skip)
			c.dropLocal(task, lf)
			continue
		}
		task.Logf("[本地 %d/%d] %s（%s）", i+1, len(locals), lf.FileName, mib(lf.Size))
		status, err := "", checkLocalDest(lf.FileName, t.sub)
		if err == nil {
			status, err = c.ModCart.InstallLocal(lf, dir)
		}
		// 这个本地文件顶替了清单里的某个 Modrinth 项目（同一个模组，只装本地这一份）却没放进去：
		// 那个项目其实没装上，不能当成装好了从清单里拿掉
		notInstalled := func() {
			if lc.ProjectID != "" {
				delete(installed, lc.ProjectID)
			}
		}
		switch {
		case err != nil:
			task.Logf("  没装上：%v", err)
			broken = append(broken, tasks.WarnItem{Name: lf.FileName + "：" + err.Error()})
			notInstalled()
		case status == modcart.LocalDiffers:
			task.Logf("  %s 里已经有同名文件，内容和这一份不一样，没有覆盖", t.sub)
			kept = append(kept, tasks.WarnItem{Name: lf.FileName})
			notInstalled()
		case status == modcart.LocalSame:
			task.Logf("  %s 里已经有一模一样的文件，跳过", t.sub)
			c.dropLocal(task, lf)
		default:
			task.Logf("  已放进 %s", t.sub)
			copied++
			c.dropLocal(task, lf)
		}
	}
	if len(kept) > 0 {
		task.AddWarning(tasks.Warning{Kind: "local-not-overwritten", Title: fmt.Sprintf("%d 个本地文件没有放进去：服务器里有同名文件", len(kept)),
			Note: "同名文件的内容和你添加的不一样，没有覆盖，免得把正在用的换掉。想换成你添加的这一份：先在服务器的 " + t.sub +
				" 文件夹里删掉旧的，再装一次。这几个文件还留在待装清单里。", Items: kept})
	}
	if len(broken) > 0 {
		task.AddWarning(tasks.Warning{Kind: "local-failed", Title: fmt.Sprintf("%d 个本地文件没装上", len(broken)),
			Note: "这几个文件还留在待装清单里，看看原因，处理好以后再装一次。", Items: broken})
	}

	task.StartStep(3)
	missing, stale, n, err := modcart.CheckLocal(dir)
	if err != nil {
		task.Logf("核对本地依赖失败：%v", err)
	} else {
		task.Logf("读了 %d 个模组的依赖声明", n)
		// 前面「要自己去找」里已经说过的那几个不再报一遍（同一个模组 ID、同一个需要它的文件）
		missing = slices.DeleteFunc(missing, func(m modcart.MissingMod) bool {
			return slices.ContainsFunc(plan.Manual, func(x modcart.ManualDep) bool { return strings.EqualFold(x.ID, m.ID) && x.File == m.File })
		})
		if len(missing) > 0 {
			w := tasks.Warning{Kind: "still-missing-deps", Title: fmt.Sprintf("还缺 %d 个前置", len(missing)),
				Note: "这些前置是模组自己声明的，Modrinth 上没写，或者自动补装没成功；缺了它们服务器可能起不来。"}
			for _, m := range missing {
				w.Items = append(w.Items, tasks.WarnItem{Name: depText(m)})
			}
			task.AddWarning(w)
		}
		if len(stale) > 0 {
			w := tasks.Warning{Kind: "stale-deps", Title: fmt.Sprintf("%d 个前置的版本不对", len(stale)),
				Note: "服务器里有这个模组，但版本不在要求的范围里。"}
			for _, m := range stale {
				w.Items = append(w.Items, tasks.WarnItem{Name: depText(m) + "，现在是 " + m.Have})
			}
			task.AddWarning(w)
		}
	}
	// 有文件没下载成功时整辆车都不动：没下成的可能是车里某一项的前置，这时把那一项拿掉，
	// 下次再装就不会再去补它的前置了
	if len(failed) == 0 {
		var done []string
		for id := range installed {
			done = append(done, id)
		}
		if _, err := c.ModCart.RemoveMany(t.inst.Name, done); err != nil {
			task.Logf("从待装清单里拿掉装好的项目失败：%v", err)
		}
		if p.IncludeDraft {
			if _, err := c.ModCart.RemoveMany(modcart.Draft, done); err != nil {
				task.Logf("从草稿清单里拿掉装好的项目失败：%v", err)
			}
		}
	}
	// 这次没有下载任何文件，本地文件又一个都没放进去：算失败，不能写「安装完成」（原因已经列在上面的提示里）。
	// 放在收尾之后：本来就在服务器里的那些项目照样从清单里拿掉了，没放进去的本地文件和被它顶替的项目都还留着。
	if len(files) == 0 && len(locals) > 0 && len(kept)+len(broken) == len(locals) {
		task.Fail(fmt.Errorf("%d 个本地文件一个都没放进服务器，原因见上面的提示。这几个文件还留在待装清单里", len(locals)))
		return
	}
	if t.inst.Status() != "stopped" {
		task.Logf("服务器正在运行：重启后新装的才生效")
	}
	if copied > 0 {
		task.Logf("装好了 %d 个文件（其中 %d 个是从电脑上添加的本地文件）", len(files)-len(failed)+copied, copied)
	} else {
		task.Logf("装好了 %d 个文件", len(files)-len(failed))
	}
	task.Finish(t.inst.Name)
}

// dropLocal 装好的本地文件从待装清单里拿掉（暂存的那份一起删）。
func (c *cat) dropLocal(task *tasks.Task, lf modcart.LocalFile) {
	if err := c.ModCart.DropLocal(lf); err != nil {
		task.Logf("  从待装清单里拿掉 %s 失败：%v", lf.FileName, err)
	}
}

func localNames(locals []modcart.LocalFile) string {
	ns := make([]string, 0, len(locals))
	for _, lf := range locals {
		ns = append(ns, lf.FileName)
	}
	return strings.Join(ns, "、")
}

func itemTitles(items []modcart.Item) string {
	ts := make([]string, 0, len(items))
	for _, it := range items {
		ts = append(ts, it.Title)
	}
	return strings.Join(ts, "、")
}

func depText(m modcart.MissingMod) string {
	s := m.ID
	if m.Range != "" && m.Range != "*" {
		s += " " + m.Range
	}
	return s + "（" + m.RequiredBy + " 需要）"
}
