package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/tasks"
)

// 自选模组 + 购物车。
//
// 风险等级：搜索、看车、加进车、拿出来、清空、查前置都是读操作——购物车只是一张暂存的清单，不动服务器里的任何文件，
// AI 可以直接帮人挑；真正往服务器里放文件的 modcart.install 是写操作，AI 发起时生成提案卡片，卡片上逐个列出要下载的文件。
// 购物车按实例分开存；inst 不填是「草稿车」：新建服务器时实例还不存在，先挑好放这里，建好以后 install 带上 includeDraft 一起装。

type modcartArgs struct {
	Inst         string   `json:"inst"`
	Q            string   `json:"q"`
	Core         string   `json:"core"`
	MC           string   `json:"mc"`
	ProjectIDs   []string `json:"projectIds"`
	ProjectID    string   `json:"projectId"`
	WithDeps     *bool    `json:"withDeps"`
	IncludeDraft bool     `json:"includeDraft"`
	// Expect 发起安装的人当时看到的购物车（项目 ID）。给了就核对：和现在要装的不一样就不装。
	// 往车里加东西是读操作，AI 不用确认就能做；人点「安装」时装的必须是他看见的那些。
	Expect *[]string `json:"expect"`
}

var (
	cartInst  = ops.String("实例名称（实例列表里的 name）；不填 = 草稿购物车：服务器还没建好时先放这里，建好后 modcart_install 带上 includeDraft 一起装").Len(0, 100)
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

// cartView 列表类结果的形状（给 AI 的结果有 16KB 上限，车最多 50 项，放得下）。
func cartView(items []modcart.Item) map[string]any {
	if items == nil {
		items = []modcart.Item{}
	}
	return map[string]any{"count": len(items), "items": items}
}

// installItems 这次要装的：实例的车，includeDraft 时再加上草稿车。
func (c *cat) installItems(p modcartArgs) []modcart.Item {
	items := c.ModCart.List(p.Inst)
	if p.IncludeDraft {
		items = modcart.Merge(items, c.ModCart.List(modcart.Draft))
	}
	return items
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
		Name: "modcart.search", Title: "在 Modrinth 搜索模组或插件（购物车）", Level: ops.Read,
		Desc: "按核心对应的加载器和 MC 版本搜 Modrinth（要联网），按下载量排序取前 20 个，只在客户端用的模组不列出来。" +
			"给 inst 按实例的核心和版本搜；服务器还没建好时给 core 和 mc。结果的 project_id 用于 modcart_add。",
		Params: ops.Object(map[string]*ops.Schema{"q": ops.String("搜索词（英文名效果更好）").Len(0, 100),
			"inst": cartInst, "core": cartCore, "mc": cartMC}),
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
			hits, err := c.ModCart.Search(ctx, p.Q, t.rtype, t.loaders, t.mc)
			if err != nil {
				return nil, cartErr(err)
			}
			return map[string]any{"type": t.rtype, "hits": hits}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.list", Title: "查看模组购物车", Level: ops.Read,
		Desc:   "购物车里有什么（按加入的先后，最多 50 项）。不给 inst 看草稿购物车。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			return cartView(c.ModCart.List(p.Inst)), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.add", Title: "把模组加进购物车", Level: ops.Read,
		Desc: "把 Modrinth 项目加进购物车（一次可以加多个），只是暂存，不下载、不改服务器。加入时会向 Modrinth 核对：" +
			"查不到的、只在客户端用的模组不放进去，在 skipped 里说明原因。返回整辆车。每辆车最多 50 项。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst,
			"projectIds": ops.Array("要加入的 Modrinth 项目 ID 或 slug", projectID).MaxLen(modcart.MaxItems)}, "projectIds"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
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
			items, skipped, err := c.ModCart.Add(ctx, p.Inst, p.ProjectIDs)
			if err != nil {
				return nil, cartErr(err)
			}
			v := cartView(items)
			if len(skipped) > 0 {
				v["skipped"] = skipped
			}
			return v, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.remove", Title: "从购物车拿掉一项", Level: ops.Read,
		Desc:   "把一个项目从购物车里拿掉（车里没有它也不报错）。返回整辆车。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "projectId": projectID}, "projectId"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			items, err := c.ModCart.Remove(p.Inst, p.ProjectID)
			if err != nil {
				return nil, cartErr(err)
			}
			return cartView(items), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.clear", Title: "清空购物车", Level: ops.Read,
		Desc:   "清空购物车（只清清单，不动服务器里已经装上的文件）。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.ModCart.Clear(p.Inst); err != nil {
				return nil, cartErr(err)
			}
			return cartView(nil), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.deps", Title: "检查购物车的前置", Level: ops.Read,
		Desc: "联网算出购物车要装的全部内容：files 是会下载的文件（fromDep 的是自动补的必装前置），missing 是购物车之外还需要的必装前置" +
			"（modcart_install 默认会一起装，也可以先 modcart_add 放进车里），optional 是可选前置（不自动装），conflicts 是不兼容的组合，" +
			"localHits 是服务器里已经有、安装时跳过的，unresolved 是找不到能装的版本的。给 inst 时会对照服务器里已有的文件；" +
			"includeDraft=true 把草稿购物车也算进来（和 modcart_install 带 includeDraft 时装的一样）。车很满时 files 会少列，note 里说明。",
		Params: ops.Object(map[string]*ops.Schema{"inst": cartInst, "core": cartCore, "mc": cartMC,
			"includeDraft": ops.Boolean("给 inst 时把草稿购物车也算进来（默认 false）")}),
		Run: func(ctx context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			items := c.installItems(p)
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			var local modcart.Local
			if t.inst != nil {
				local = c.ModCart.ScanLocal(ctx, filepath.Join(t.inst.Dir, t.sub))
			}
			plan, err := c.ModCart.Closure(ctx, items, local, t.loaders, t.mc)
			if err != nil {
				return nil, cartErr(err)
			}
			return depsView(len(items), plan, caller.Source.AI()), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "modcart.install", Title: "安装购物车里的模组", Level: ops.Write,
		Desc: "把实例购物车里的模组或插件装进服务器的 mods / plugins：挑兼容的版本、自动补上必装前置（withDeps=false 只装车里的），" +
			"在后台下载（核对 SHA-1），返回任务编号（进度见 task_get）。服务器里已经有的跳过；装完核对本地依赖，还缺的写在任务的提示里。" +
			"includeDraft=true 把草稿购物车一起装进这台实例（新建服务器后用）。确认卡片上会列出要下载的每个文件。",
		Params: withInst(map[string]*ops.Schema{
			"withDeps":     ops.Boolean("自动补上必装前置（默认 true）"),
			"includeDraft": ops.Boolean("把草稿购物车也一起装进这台实例（默认 false）"),
			"expect":       ops.Array("发起安装的人当时看到的购物车里的项目 ID（界面会带上）；给了就核对，和现在要装的不一样就不装", ops.String("项目 ID")),
		}),
		Preview: func(ctx context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[modcartArgs](a)
			if err != nil {
				return nil, err
			}
			t, err := c.cartTarget(p)
			if err != nil {
				return nil, err
			}
			items := c.installItems(p)
			if len(items) == 0 {
				return nil, ops.Errorf(400, "购物车是空的")
			}
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			local := c.ModCart.ScanLocal(ctx, filepath.Join(t.inst.Dir, t.sub))
			plan, err := c.ModCart.Closure(ctx, items, local, t.loaders, t.mc)
			if err != nil {
				return nil, cartErr(err)
			}
			files := toInstall(plan, withDeps(p))
			if len(files) == 0 { // 没有要下载的：不出一张什么都不做的卡片，直接说清楚
				var why []string
				if len(plan.Unresolved) > 0 {
					why = append(why, "找不到能装的版本："+unresolvedText(plan.Unresolved))
				}
				if len(plan.LocalHits) > 0 {
					why = append(why, "服务器里已经有了："+strings.Join(plan.LocalHits, "、")+"（可以用 modcart_remove 从车里拿掉）")
				}
				return nil, ops.Errorf(400, "没有要下载的文件。%s", strings.Join(why, "；"))
			}
			deps, size := 0, int64(0)
			// 前置条件：车里有哪些项目，和这张卡片列出的确切文件。确认前车变了、或者 Modrinth 上发了新版本，卡片作废
			pl := &ops.Plan{Pre: map[string]string{"购物车": modcart.Fingerprint(items), "要装的文件": filesPrint(files, plan.LocalHits)}}
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
			pl.Summary = fmt.Sprintf("把购物车里的 %d 项装进「%s」", len(items), t.inst.Name)
			if deps > 0 {
				pl.Summary += fmt.Sprintf("（另补 %d 个必装前置）", deps)
			}
			pl.Summary += fmt.Sprintf("，共下载 %d 个文件约 %s", len(files), mib(size))
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
				warn = append(warn, "不兼容："+strings.Join(plan.Conflicts, "；"))
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
			items := c.installItems(p)
			if len(items) == 0 {
				done()
				return nil, ops.Errorf(400, "购物车是空的")
			}
			// 装的必须是人看过、确认过的那些。往车里加东西不用确认（AI 助手、别的窗口都能加），
			// 所以这里读到的车要和发起的人看到的对一遍：
			//   - 人在界面上点的：请求里带着他当时看到的项目（expect）；
			//   - AI 提的、人在卡片上确认的：确认那一刻的购物车指纹（卡片上列的文件在后台算完版本以后再对，见 installCart）。
			if p.Expect != nil && !sameProjects(items, *p.Expect) {
				done()
				return nil, ops.Errorf(409, "购物车里的东西和你刚才看到的不一样了（AI 助手或者别的窗口可能改过）。先看一眼现在的购物车，再点安装")
			}
			wantFiles := ""
			if pre := ops.ConfirmedPre(ctx); pre != nil {
				if modcart.Fingerprint(items) != pre["购物车"] {
					done()
					return nil, ops.Errorf(409, "确认的时候购物车又变了，这次没有装。请让 AI 重新提一次")
				}
				wantFiles = pre["要装的文件"]
			}
			task, tctx := c.Mgr.Tasks.New(fmt.Sprintf("给「%s」安装购物车里的 %d 项", t.inst.Name, len(items)),
				[]string{"挑版本、补前置", "下载文件", "核对本地依赖"})
			task.SetKind("mods")
			go func() {
				defer done()
				c.installCart(tctx, task, t, p, items, wantFiles)
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
	v := map[string]any{"count": n, "conflicts": plan.Conflicts, "localHits": plan.LocalHits, "unresolved": plan.Unresolved}
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
	fits := func() bool { b, _ := json.Marshal(v); return len(b) <= aiBudget }
	if !fits() {
		deps := slices.DeleteFunc(slices.Clone(files), func(f aiFile) bool { return !f.FromDep })
		v["files"] = deps
		v["note"] = fmt.Sprintf("结果太长：files 只列了自动补的 %d 个前置，购物车里要装的 %d 项见 modcart_list", len(deps), len(files)-len(deps))
		if !fits() {
			v["files"] = []aiFile{}
			v["note"] = fmt.Sprintf("结果太长：一共要下载 %d 个文件（其中 %d 个是自动补的前置），没有逐个列出；要补的前置见 missing", len(files), len(deps))
		}
	}
	return v
}

// filesPrint 要装的确切文件（项目@版本）加上要跳过的已有文件的指纹，不看顺序。
func filesPrint(files []modcart.File, hits []string) string {
	keys := make([]string, 0, len(files)+len(hits))
	for _, f := range files {
		keys = append(keys, f.ProjectID+"@"+f.VersionID)
	}
	for _, h := range hits {
		keys = append(keys, "已有:"+h)
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:8])
}

func unresolvedText(us []modcart.Unresolved) string {
	parts := make([]string, 0, len(us))
	for _, u := range us {
		parts = append(parts, u.Title+"："+u.Reason)
	}
	return strings.Join(parts, "；")
}

// installCart 后台任务：挑版本、补前置 → 下载 → 核对本地依赖；装上的从购物车里拿掉（下载期间新加进车的不动）。
// wantFiles 非空时是人在提案卡片上确认的那份文件清单的指纹：这里重新挑完版本、补完前置以后要和它对上，
// 对不上（确认之后 Modrinth 上发了新版本、前置变了）就一个文件都不下载。
func (c *cat) installCart(ctx context.Context, task *tasks.Task, t cartTarget, p modcartArgs, items []modcart.Item, wantFiles string) {
	dir := filepath.Join(t.inst.Dir, t.sub)
	task.StartStep(0)
	task.Logf("购物车共 %d 项：%s", len(items), itemTitles(items))
	rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	local := c.ModCart.ScanLocal(rctx, dir)
	plan, err := c.ModCart.Closure(rctx, items, local, t.loaders, t.mc)
	cancel()
	if err != nil {
		task.Fail(err)
		return
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
		w := tasks.Warning{Kind: "conflicts", Title: "有不兼容的组合", Note: "照样装上了；服务器起不来时先拿掉其中一个。"}
		for _, s := range plan.Conflicts {
			w.Items = append(w.Items, tasks.WarnItem{Name: s})
		}
		task.AddWarning(w)
	}
	for _, h := range plan.LocalHits {
		task.Logf("服务器里已经有 %s，跳过", h)
	}
	files := toInstall(plan, withDeps(p))
	if wantFiles != "" && filesPrint(files, plan.LocalHits) != wantFiles {
		task.Fail(errors.New("要装的文件和确认时卡片上列的不一样了（Modrinth 上可能刚发了新版本）。一个文件都没有下载，请让 AI 重新提一次"))
		return
	}
	if len(files) == 0 && len(plan.Unresolved) > 0 {
		task.Fail(fmt.Errorf("没有能装的文件：%s", unresolvedText(plan.Unresolved)))
		return
	}
	if len(files) == 0 {
		task.Logf("车里的都已经在服务器里了，不用下载")
	}

	task.StartStep(1)
	installed := map[string]bool{} // 装上了（或者本来就有）的购物车项目
	for _, it := range items {
		if !slices.ContainsFunc(plan.Files, func(f modcart.File) bool { return f.ProjectID == it.ProjectID }) &&
			!slices.ContainsFunc(plan.Unresolved, func(u modcart.Unresolved) bool { return u.ProjectID == it.ProjectID }) {
			installed[it.ProjectID] = true // 不在要下载的里、也不在找不到的里：服务器里已经有了
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
			Note: "购物车先不动：检查网络或代理后再装一次，已经装上的会自动跳过，只补没装上的。", Items: failed})
	}

	task.StartStep(2)
	missing, stale, n, err := modcart.CheckLocal(dir)
	if err != nil {
		task.Logf("核对本地依赖失败：%v", err)
	} else {
		task.Logf("读了 %d 个模组的依赖声明", n)
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
			task.Logf("从购物车里拿掉装好的项目失败：%v", err)
		}
		if p.IncludeDraft {
			if _, err := c.ModCart.RemoveMany(modcart.Draft, done); err != nil {
				task.Logf("从草稿购物车里拿掉装好的项目失败：%v", err)
			}
		}
	}
	if t.inst.Status() != "stopped" {
		task.Logf("服务器正在运行：重启后新装的才生效")
	}
	task.Logf("装好了 %d 个文件", len(files)-len(failed))
	task.Finish(t.inst.Name)
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
