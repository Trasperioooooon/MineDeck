package modcart

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// ---- 前置闭包 ----
//
// 购物车里每一项挑出要装的版本，再顺着 Modrinth 版本对象里的 dependencies[] 往下找：
//   - required（必装）：购物车里没有、服务器上也没有的，挑好版本一起下载（FromDep），并列进 Missing，
//     界面上可以勾选后正式放进购物车；往下最多追 3 层，visited 防环。
//   - embedded（已内嵌）：按 Modrinth 的说明已经打包在模组里了，算满足，不单独装。
//   - optional（可选）：只列出来（只看购物车里的模组直接依赖的那一层），不自动加。
//   - incompatible（不兼容）：和购物车里的、服务器上已有的撞上了，写进 Conflicts。
// 「服务器上已有」按三样认：文件名一样；文件的 SHA-1 在 Modrinth 上属于同一个项目（换过版本的旧文件也认得出，
// 不会再下一份，同一个模组两份 jar 服务器起不来）；modinfo 读出来的模组 ID 和项目的 slug 一样。

// maxDepth 必装前置往下追几层。
const maxDepth = 3

// workers 同时查几个项目的版本。
const workers = 6

// MRDep Modrinth 版本对象里的一条依赖。
type MRDep struct {
	VersionID      string `json:"versionId,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	FileName       string `json:"fileName,omitempty"`
	DependencyType string `json:"dependencyType"`
}

// File 要下载的一个文件（挑中的版本的主文件）。
type File struct {
	ProjectID     string  `json:"projectId"`
	Slug          string  `json:"slug,omitempty"`
	Title         string  `json:"title"`
	VersionID     string  `json:"versionId"`
	VersionNumber string  `json:"versionNumber"`
	VersionType   string  `json:"versionType,omitempty"` // 不是正式版时：beta / alpha
	FileName      string  `json:"fileName"`
	URL           string  `json:"url,omitempty"`
	SHA1          string  `json:"sha1,omitempty"`
	Size          int64   `json:"size"`
	License       string  `json:"license,omitempty"`
	FromDep       bool    `json:"fromDep,omitempty"`      // 不是购物车里的，是自动补的必装前置
	DependencyOf  string  `json:"dependencyOf,omitempty"` // 前置：谁需要它（名称）
	Deps          []MRDep `json:"deps,omitempty"`
}

// Unresolved 找不到能装的版本的项目。
type Unresolved struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Reason    string `json:"reason"`
}

// DepInfo 一项前置（必装或可选）。
type DepInfo struct {
	Project        File   `json:"project"`
	DependencyType string `json:"dependencyType"` // required / optional
	DependencyOf   string `json:"dependencyOf"`   // 谁需要它（名称）
}

// Local 服务器目录（mods 或 plugins）里已经有的东西，见 ScanLocal。
type Local struct {
	Files    []string          // jar 文件名（停用的不算）
	Projects map[string]string // 按 SHA-1 在 Modrinth 上认出来的：项目 ID → 文件名
	IDs      []string          // modinfo 读出来的模组 ID（含 provides）
}

// has 这个项目服务器上已经有了没有；有的话返回那个文件名（按 modinfo 的 ID 认出来的返回名称）。
func (l Local) has(p mrProject, fileName string) (string, bool) {
	if name, ok := l.Projects[p.ID]; ok {
		return name, true
	}
	if i := slices.IndexFunc(l.Files, func(f string) bool { return strings.EqualFold(f, fileName) }); fileName != "" && i >= 0 {
		return l.Files[i], true // Windows 上文件名不分大小写
	}
	if p.Slug != "" && slices.ContainsFunc(l.IDs, func(id string) bool { return strings.EqualFold(id, p.Slug) }) {
		return orID(p.Title, p.Slug), true
	}
	return "", false
}

// Plan 一辆车要装的全部内容。切片都不是 nil：JSON 里是 []，界面直接 .map。
type Plan struct {
	Files      []File       `json:"files"`      // 要下载的：购物车里的，加上自动补的必装前置（FromDep）
	Missing    []DepInfo    `json:"missing"`    // 必装前置里购物车没有、服务器上也没有的（安装时会自动一起装；也可以先放进购物车）
	Optional   []DepInfo    `json:"optional"`   // 可选前置（不自动装）
	Conflicts  []string     `json:"conflicts"`  // 不兼容的组合
	LocalHits  []string     `json:"localHits"`  // 服务器目录里已经有的（购物车里的或前置），安装时跳过
	Unresolved []Unresolved `json:"unresolved"` // 找不到能装的版本的
}

// resolver 一次闭包计算：缓存查过的项目和版本，查多个项目时并发。
type resolver struct {
	s       *Service
	loaders []string
	mc      string

	mu        sync.Mutex
	projects  map[string]mrProject // 项目 ID → 项目
	byVersion map[string]mrVersion // 版本 ID → 版本（只在主流程里用）
}

type resolved struct {
	file   File
	reason string // 没有能装的版本时的原因
	err    error  // 网络之类的问题：整次计算失败
}

// Closure 算出一辆车要装的全部内容（见本节开头）。loaders、mc 同 Search；网络失败时报错（502），
// 某一项找不到能装的版本不算失败，写进 Unresolved。
func (s *Service) Closure(ctx context.Context, items []Item, local Local, loaders []string, mc string) (*Plan, error) {
	r := &resolver{s: s, loaders: loaders, mc: mc, projects: map[string]mrProject{}, byVersion: map[string]mrVersion{}}
	plan := &Plan{Files: []File{}, Missing: []DepInfo{}, Optional: []DepInfo{}, Conflicts: []string{}, LocalHits: []string{}, Unresolved: []Unresolved{}}
	inCart := map[string]bool{}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		inCart[it.ProjectID] = true
		ids = append(ids, it.ProjectID)
		p := mrProject{ID: it.ProjectID, Slug: it.Slug, Title: it.Title, ProjectType: it.Kind}
		p.License.ID = it.License
		r.projects[it.ProjectID] = p
	}
	planned := map[string]bool{}   // 已经处理过的项目（要装、已有、找不到）
	satisfied := map[string]bool{} // 已经内嵌在要装（或已有）的模组里的
	var level []File
	res := r.resolveMany(ctx, ids)
	for _, it := range items {
		x := res[it.ProjectID]
		if x.err != nil {
			return nil, x.err
		}
		planned[it.ProjectID] = true
		if x.reason != "" {
			plan.Unresolved = append(plan.Unresolved, Unresolved{ProjectID: it.ProjectID, Title: it.Title, Reason: x.reason})
			continue
		}
		f := x.file
		level = append(level, f) // 已经装着的也顺着它的依赖查一遍：上次没装全的前置这次补上
		if name, ok := local.has(r.projects[it.ProjectID], f.FileName); ok {
			plan.addHit(name)
			continue
		}
		plan.Files = append(plan.Files, f)
	}

	type need struct {
		dep  MRDep
		of   string
		skip bool // 已经记过找不到
	}
	var incompat []need
	optional := map[string]DepInfo{}
	var optOrder []string
	for depth := 1; len(level) > 0 && depth <= maxDepth; depth++ {
		var req, opt, embedded []need
		for _, f := range level {
			for _, d := range f.Deps {
				n := need{dep: d, of: f.Title}
				switch d.DependencyType {
				case "required":
					req = append(req, n)
				case "optional":
					if depth == 1 {
						opt = append(opt, n)
					}
				case "incompatible":
					incompat = append(incompat, n)
				case "embedded":
					embedded = append(embedded, n)
				}
			}
		}
		// 只写了版本 ID 的依赖：先查出是哪个项目。必装的还要看钉死的那个版本能不能用，能用就装它
		// （可选和不兼容的钉死版本不能拿来当必装前置的版本）。
		pinned := map[string]File{}
		for i := range req {
			d := &req[i].dep
			if d.VersionID == "" || (d.ProjectID != "" && (inCart[d.ProjectID] || planned[d.ProjectID])) {
				continue
			}
			v, err := r.version(ctx, d.VersionID)
			if IsNotFound(err) {
				if d.ProjectID == "" {
					plan.Unresolved = append(plan.Unresolved, Unresolved{Title: orID(d.FileName, "未知的前置"),
						Reason: req[i].of + " 需要的那个版本在 Modrinth 上已经没有了"})
					req[i].skip = true
				}
				continue
			}
			if err != nil {
				return nil, err
			}
			if d.ProjectID == "" {
				d.ProjectID = v.ProjectID
			}
			if compatible(v, loaders, mc) {
				pinned[v.ProjectID] = fileOf(v)
			}
		}
		for _, list := range [][]need{opt, embedded} {
			for i := range list {
				if err := r.fillProject(ctx, &list[i].dep); err != nil {
					return nil, err
				}
			}
		}
		for _, n := range embedded {
			if n.dep.ProjectID != "" {
				satisfied[n.dep.ProjectID] = true
			}
		}
		var all []string
		for _, list := range [][]need{req, opt} {
			for _, n := range list {
				if n.dep.ProjectID != "" {
					all = append(all, n.dep.ProjectID)
				}
			}
		}
		if err := r.loadProjects(ctx, all); err != nil {
			return nil, err
		}

		// 必装：这一层要补的先挑版本（并发），再按出现的顺序登记
		var todo []string
		of := map[string]string{}
		for _, n := range req {
			pid := n.dep.ProjectID
			if n.skip {
				continue
			}
			if pid == "" {
				plan.Unresolved = append(plan.Unresolved, Unresolved{Title: orID(n.dep.FileName, "未知的前置"),
					Reason: n.of + " 需要它，但 Modrinth 上没写明是哪个项目"})
				continue
			}
			if inCart[pid] || planned[pid] || satisfied[pid] || slices.Contains(todo, pid) {
				continue
			}
			p := r.project(pid)
			if name, ok := local.has(p, ""); ok {
				plan.addHit(name)
				planned[pid] = true
				continue
			}
			todo = append(todo, pid)
			of[pid] = n.of
		}
		got := r.resolveMany(ctx, slices.DeleteFunc(slices.Clone(todo), func(pid string) bool { _, ok := pinned[pid]; return ok }))
		var next []File
		for _, pid := range todo {
			planned[pid] = true
			p := r.project(pid)
			var f File
			if pf, ok := pinned[pid]; ok {
				f = r.decorate(pf, p)
			} else {
				x := got[pid]
				if x.err != nil {
					return nil, x.err
				}
				if x.reason != "" {
					plan.Unresolved = append(plan.Unresolved, Unresolved{ProjectID: pid, Title: orID(p.Title, pid), Reason: of[pid] + " 需要它，但" + x.reason})
					continue
				}
				f = x.file
			}
			if name, ok := local.has(p, f.FileName); ok {
				plan.addHit(name)
				continue
			}
			f.FromDep, f.DependencyOf = true, of[pid]
			plan.Files = append(plan.Files, f)
			plan.Missing = append(plan.Missing, DepInfo{Project: brief(f), DependencyType: "required", DependencyOf: of[pid]})
			next = append(next, f)
		}

		// 可选：只列出来
		var optTodo []string
		for _, n := range opt {
			pid := n.dep.ProjectID
			if pid == "" || inCart[pid] || planned[pid] || satisfied[pid] || slices.Contains(optOrder, pid) || slices.Contains(optTodo, pid) {
				continue
			}
			if _, ok := local.has(r.project(pid), ""); ok {
				continue
			}
			optTodo = append(optTodo, pid)
			of[pid] = n.of
		}
		gotOpt := r.resolveMany(ctx, optTodo)
		for _, pid := range optTodo {
			x := gotOpt[pid]
			if x.err != nil {
				return nil, x.err
			}
			if x.reason != "" { // 可选的装不了就不提了
				continue
			}
			optional[pid] = DepInfo{Project: brief(x.file), DependencyType: "optional", DependencyOf: of[pid]}
			optOrder = append(optOrder, pid)
		}
		level = next
	}
	for _, pid := range optOrder {
		if !planned[pid] && !inCart[pid] && !satisfied[pid] {
			plan.Optional = append(plan.Optional, optional[pid])
		}
	}

	// 不兼容：各层都收齐以后，对照购物车、要装的全部文件（含自动补的前置）和服务器上已有的统一看。
	// 依赖只点名了某个版本时，只有要装的正是那个版本才算。
	toInstall := map[string]string{} // 项目 → 要装的版本
	for _, f := range plan.Files {
		toInstall[f.ProjectID] = f.VersionID
	}
	var ipids []string
	for i := range incompat {
		if err := r.fillProject(ctx, &incompat[i].dep); err != nil {
			return nil, err
		}
		if pid := incompat[i].dep.ProjectID; pid != "" {
			ipids = append(ipids, pid)
		}
	}
	if err := r.loadProjects(ctx, ipids); err != nil {
		return nil, err
	}
	for _, n := range incompat {
		pid := n.dep.ProjectID
		if pid == "" {
			continue
		}
		p := r.project(pid)
		var msg string
		switch {
		case n.dep.VersionID != "":
			if v, ok := toInstall[pid]; ok && v == n.dep.VersionID {
				msg = fmt.Sprintf("%s 和 %s 的这个版本不兼容，不能一起装", n.of, orID(p.Title, pid))
			}
		case inCart[pid]:
			msg = fmt.Sprintf("%s 和 %s 不兼容，不能一起装", n.of, orID(p.Title, pid))
		case toInstall[pid] != "":
			msg = fmt.Sprintf("%s 和 %s（自动补的前置）不兼容", n.of, orID(p.Title, pid))
		default:
			if name, ok := local.has(p, n.dep.FileName); ok {
				msg = fmt.Sprintf("%s 和服务器里已有的 %s 不兼容", n.of, name)
			}
		}
		if msg != "" && !slices.Contains(plan.Conflicts, msg) {
			plan.Conflicts = append(plan.Conflicts, msg)
		}
	}
	return plan, nil
}

// addHit 记一条「服务器里已经有」（不重复）。
func (p *Plan) addHit(name string) {
	if !slices.Contains(p.LocalHits, name) {
		p.LocalHits = append(p.LocalHits, name)
	}
}

// fillProject 只写了版本 ID 的依赖：查出是哪个项目。版本已经没有了就留空。
func (r *resolver) fillProject(ctx context.Context, d *MRDep) error {
	if d.ProjectID != "" || d.VersionID == "" {
		return nil
	}
	v, err := r.version(ctx, d.VersionID)
	if IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	d.ProjectID = v.ProjectID
	return nil
}

// version 查单个版本（同一次计算里查过的不再查）。只在 Closure 的主流程里调，不用加锁。
func (r *resolver) version(ctx context.Context, id string) (mrVersion, error) {
	if v, ok := r.byVersion[id]; ok {
		return v, nil
	}
	v, err := r.s.version(ctx, id)
	if err == nil {
		r.byVersion[id] = v
	}
	return v, err
}

// brief 列进 Missing / Optional 时去掉下载地址、哈希和依赖：给 AI 看的结果要短（16KB 上限）。
func brief(f File) File {
	f.URL, f.SHA1, f.Deps = "", "", nil
	return f
}

// project 查过的项目；没查到的只有 ID。
func (r *resolver) project(pid string) mrProject {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.projects[pid]; ok {
		return p
	}
	return mrProject{ID: pid}
}

// loadProjects 批量查还不认识的项目（名称、slug、许可证）。
func (r *resolver) loadProjects(ctx context.Context, ids []string) error {
	r.mu.Lock()
	var need []string
	for _, id := range ids {
		if _, ok := r.projects[id]; !ok && !slices.Contains(need, id) {
			need = append(need, id)
		}
	}
	r.mu.Unlock()
	if len(need) == 0 {
		return nil
	}
	ps, err := r.s.projects(ctx, need)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range ps {
		r.projects[p.ID] = p
	}
	return nil
}

// resolveMany 并发给几个项目挑版本。
func (r *resolver) resolveMany(ctx context.Context, pids []string) map[string]resolved {
	out := make(map[string]resolved, len(pids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for _, pid := range pids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			x := r.resolve(ctx, pid)
			mu.Lock()
			out[pid] = x
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// resolve 给一个项目挑版本。
func (r *resolver) resolve(ctx context.Context, pid string) resolved {
	vs, err := r.s.versions(ctx, pid, r.loaders, r.mc)
	if IsNotFound(err) {
		return resolved{reason: "Modrinth 上找不到这个项目"}
	}
	if err != nil {
		return resolved{err: err}
	}
	v, ok := pickVersion(vs, r.loaders, r.mc)
	if !ok {
		return resolved{reason: "没有 " + describe(r.loaders, r.mc) + " 能用的版本"}
	}
	return resolved{file: r.decorate(fileOf(v), r.project(pid))}
}

// decorate 补上项目的名称、slug 和许可证。
func (r *resolver) decorate(f File, p mrProject) File {
	f.Title, f.Slug, f.License = orID(p.Title, f.ProjectID), p.Slug, p.License.ID
	return f
}

// fileOf 版本的主文件和依赖。
func fileOf(v mrVersion) File {
	pf := primaryFile(v)
	f := File{ProjectID: v.ProjectID, VersionID: v.ID, VersionNumber: v.VersionNumber, FileName: pf.Filename,
		URL: pf.URL, SHA1: pf.Hashes.SHA1, Size: pf.Size}
	if v.VersionType != "release" {
		f.VersionType = v.VersionType
	}
	for _, d := range v.Dependencies {
		f.Deps = append(f.Deps, MRDep{VersionID: d.VersionID, ProjectID: d.ProjectID, FileName: d.FileName, DependencyType: d.DependencyType})
	}
	return f
}
