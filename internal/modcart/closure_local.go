package modcart

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"minedeck/internal/modinfo"
)

// ---- 前置闭包里的本地文件 ----
//
// 车里的本地文件（人从电脑上添加的 jar）在算前置时这样处理：
//   - 先拿 SHA-1 去 Modrinth 认（/version_files）。认得出来的：知道它是哪个项目的哪个版本，它声明的必装前置
//     并进闭包（缺的照常列进 Missing、可以自动一起下载）；车里别的项目依赖它时算已经满足，不再下载一份；
//     车里要是正好也有这个项目，只装本地这一份（Covers）。
//   - 认不出来的（CurseForge 独占的、魔改的、这次没连上 Modrinth）：用 modinfo 读 jar 自己声明的依赖，按模组 ID 对照
//     「车里别的本地文件 + 服务器目录里已有的 + 这次要下载的」。还缺的列进 Manual：只知道模组 ID，没法代为下载。
//   - 加载器（认出来的还看 MC 版本）对不上只提醒（Warn），不拦。
//   - 服务器目录里已经有一模一样的文件：安装时跳过（Skip）。有同一个模组的另一份、或者同名但内容不同的文件：
//     写进 Conflicts 提醒，不自动删任何东西。
//
// 「这次要下载的」只知道项目的 slug，不知道模组 ID，所以按 slug 宽松地对（去掉符号、结尾的 api）：对上了就不报缺。
// 这一步只是装之前的提醒，报少了不要紧——装完还有 CheckLocal 按真实的模组 ID 再核一遍。

// LocalCheck 一个本地文件的检查结果。
type LocalCheck struct {
	SHA1       string `json:"sha1"`
	FileName   string `json:"fileName"`
	Title      string `json:"title"`
	Size       int64  `json:"size"`
	Recognized bool   `json:"recognized"`          // 在 Modrinth 上认出来了
	ProjectID  string `json:"projectId,omitempty"` // 认出来的项目
	VersionID  string `json:"versionId,omitempty"`
	Version    string `json:"version,omitempty"` // 版本号：认出来的用 Modrinth 的，否则是 jar 自己写的
	Warn       string `json:"warn,omitempty"`    // 加载器或 MC 版本对不上、读不出信息时的提醒
	Covers     string `json:"covers,omitempty"`  // 清单里的这个 Modrinth 项目和它是同一个：只装本地这一份，不再下载
	Skip       string `json:"skip,omitempty"`    // 服务器目录里已经有一模一样的文件（那个文件的名字）：安装时跳过
	NameTaken  bool   `json:"nameTaken,omitempty"`
}

// ManualDep 本地文件自己声明、哪儿都没找到的必需前置：要人自己去找。
type ManualDep struct {
	ID         string `json:"id"`              // 缺的模组 ID（插件是插件名）
	Range      string `json:"range,omitempty"` // 要求的版本
	RequiredBy string `json:"requiredBy"`      // 谁需要它（名称）
	File       string `json:"file"`            // 需要它的那个本地文件
}

type stagedFile struct {
	lf     LocalFile
	mod    modinfo.Mod
	ver    *mrVersion // 在 Modrinth 上认出来的版本；没认出来是 nil
	follow bool       // 顺着它在 Modrinth 上声明的依赖往下找（认出来了，而且加载器对得上）
}

// stagedSet 一次闭包计算里的本地文件。空的也能用（方法都按「没有本地文件」回答）。
type stagedSet struct {
	files     []stagedFile
	byProject map[string]int // 认出来的项目 ID → files 的下标
	byID      map[string]int // 模组 ID（小写，含 provides）→ 下标
	byName    map[string]int // 文件名（小写）→ 下标
}

// inspectStaged 逐个看车里的本地文件：去 Modrinth 认、读 jar 自己的描述，把每个文件的那一行先填进 plan.Local。
func (s *Service) inspectStaged(ctx context.Context, staged []LocalFile, loaders []string, mc string, plan *Plan) *stagedSet {
	st := &stagedSet{byProject: map[string]int{}, byID: map[string]int{}, byName: map[string]int{}}
	if len(staged) == 0 {
		return st
	}
	hashes := make([]string, 0, len(staged))
	for _, lf := range staged {
		hashes = append(hashes, lf.SHA1)
	}
	vs, err := s.versionFiles(ctx, hashes)
	if err != nil {
		vs = nil
		plan.LocalNote = "这次没连上 Modrinth，认不出本地文件是哪个项目，只按文件自己写明的前置检查。联网以后再查一次会更准。"
	}
	for i, lf := range staged {
		f := stagedFile{lf: lf, mod: s.readStaged(lf)}
		c := LocalCheck{SHA1: lf.SHA1, FileName: lf.FileName, Title: lf.Title, Size: lf.Size, Version: lf.Version}
		if v, ok := vs[lf.SHA1]; ok && validID(v.ProjectID) {
			f.ver = &v
			c.Recognized, c.ProjectID, c.VersionID = true, v.ProjectID, v.ID
			if v.VersionNumber != "" {
				c.Version = v.VersionNumber
			}
			fits := slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(loaders, l) })
			f.follow = fits
			switch {
			case !fits && len(v.Loaders) > 0 && len(loaders) > 0:
				c.Warn = fmt.Sprintf("这个文件是给 %s 用的，这台服务器是 %s，装上多半不能用。", loaderList(v.Loaders), serverName(loaders))
			case mc != "" && len(v.GameVersions) > 0 && !slices.Contains(v.GameVersions, mc):
				c.Warn = fmt.Sprintf("这个文件是给 Minecraft %s 用的，这台服务器是 %s，装上可能不能用。", versionList(v.GameVersions), mc)
			}
			if _, dup := st.byProject[v.ProjectID]; !dup {
				st.byProject[v.ProjectID] = i
			}
		} else {
			c.Warn = LoaderWarn(f.mod.Loader, loaders, mc)
		}
		for _, id := range append([]string{f.mod.ID}, f.mod.Also...) {
			if id = strings.ToLower(id); id != "" {
				if _, dup := st.byID[id]; !dup {
					st.byID[id] = i
				}
			}
		}
		st.byName[strings.ToLower(lf.FileName)] = i
		st.files = append(st.files, f)
		plan.Local = append(plan.Local, c)
	}
	return st
}

// loaderList 「Fabric、Quilt」：最多列三个。
func loaderList(ls []string) string {
	names := make([]string, 0, 3)
	for _, l := range ls {
		if len(names) == 3 {
			break
		}
		names = append(names, loaderName(l))
	}
	return strings.Join(names, "、")
}

// versionList 「1.19.2、1.19.3 等」：最多列三个。
func versionList(vs []string) string {
	if len(vs) > 3 {
		return strings.Join(vs[:3], "、") + " 等"
	}
	return strings.Join(vs, "、")
}

// has 这个项目是不是车里某个认出来的本地文件。
func (st *stagedSet) has(pid string) bool {
	_, ok := st.byProject[pid]
	return ok
}

// covers 车里有没有本地文件就是这个项目（返回它的下标）：在 Modrinth 上认出来是它；文件名一样；
// 或者 jar 自己写的模组 ID 和项目的 slug 一样（同 Local.has 认服务器上已有文件的办法）。
func (st *stagedSet) covers(p mrProject, fileName string) (int, bool) {
	if i, ok := st.byProject[p.ID]; ok && p.ID != "" {
		return i, true
	}
	if i, ok := st.byName[strings.ToLower(fileName)]; ok && fileName != "" {
		return i, true
	}
	if i, ok := st.byID[strings.ToLower(p.Slug)]; ok && p.Slug != "" {
		return i, true
	}
	return 0, false
}

// strictKey 只留字母数字、转小写：cloth-config 和 cloth_config 算同一个。
func strictKey(v string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			return r
		}
		return -1
	}, v)
}

// looseKey 在 strictKey 的基础上去掉结尾的 api：模组 ID 是 architectury、slug 是 architectury-api 这种也对得上。
func looseKey(v string) string {
	k := strictKey(v)
	if t := strings.TrimSuffix(k, "api"); t != "" {
		return t
	}
	return k
}

// check 主流程算完以后，把本地文件对照服务器目录、互相对照、对照这次要下载的：填 Skip、Conflicts 和 Manual。
func (st *stagedSet) check(plan *Plan, local Local, loaders []string) {
	if len(st.files) == 0 {
		return
	}
	noun := "插件"
	if isModLoaders(loaders) {
		noun = "模组"
	}
	conflict := func(msg string) {
		if !slices.Contains(plan.Conflicts, msg) {
			plan.Conflicts = append(plan.Conflicts, msg)
		}
	}
	have := map[string]bool{} // 已经有的、马上会有的模组 ID（小写）
	for _, id := range local.IDs {
		have[strings.ToLower(id)] = true
	}
	for id := range st.byID {
		have[id] = true
	}
	coming := map[string]bool{} // 这次要下载的项目（宽松的 slug）
	for _, f := range plan.Files {
		if f.Slug != "" {
			coming[looseKey(f.Slug)] = true
		}
	}
	for i, f := range st.files {
		c := &plan.Local[i]
		name := f.lf.FileName
		id := strings.ToLower(f.mod.ID)
		// 和车里排在前面的本地文件是同一个模组
		for j := 0; j < i; j++ {
			o := st.files[j]
			sameProject := f.ver != nil && o.ver != nil && f.ver.ProjectID == o.ver.ProjectID
			if sameProject || (id != "" && id == strings.ToLower(o.mod.ID)) {
				conflict(fmt.Sprintf("本地文件 %s 和 %s 是同一个%s的两份，只留一份：两份都装服务器会起不来", o.lf.FileName, name, noun))
			}
		}
		// 服务器目录里已经有的
		if same, ok := local.Hashes[f.lf.SHA1]; ok {
			c.Skip = same
			plan.addHit(same)
			continue // 文件已经在服务器上了，它的前置由装完以后的本地核对去看
		}
		if k := slices.IndexFunc(local.Files, func(x string) bool { return strings.EqualFold(x, name) }); k >= 0 {
			c.NameTaken = true
			conflict(fmt.Sprintf("服务器里已经有一个也叫 %s 的文件，内容和你添加的不一样。安装时不会覆盖它；想换成你添加的这一份，先把服务器里旧的删掉", local.Files[k]))
		} else {
			other := ""
			if f.ver != nil {
				other = local.Projects[f.ver.ProjectID]
			}
			if other == "" && id != "" {
				other = local.IDFile[id]
			}
			if other != "" {
				conflict(fmt.Sprintf("本地文件 %s 和服务器里已有的 %s 是同一个%s，两份都在的话服务器会起不来。先把旧的那份停用或删掉，或者把这个文件从清单里移除", name, other, noun))
			}
		}
		if f.ver != nil {
			continue // 认出来的：前置已经按 Modrinth 上写的算过了
		}
		// 和这次要下载的看起来是同一个
		if id != "" {
			for _, pf := range plan.Files {
				if pf.Slug != "" && strictKey(pf.Slug) == strictKey(id) {
					conflict(fmt.Sprintf("本地文件 %s 和要下载的 %s 看起来是同一个%s，只留一份就够了", name, pf.Title, noun))
				}
			}
		}
		// jar 自己声明的必需前置
		for _, d := range f.mod.Deps {
			did := strings.ToLower(d.ID)
			if !mustHave(d) || have[did] || coming[looseKey(did)] {
				continue
			}
			if !slices.ContainsFunc(plan.Manual, func(m ManualDep) bool { return strings.EqualFold(m.ID, d.ID) && m.File == name }) {
				plan.Manual = append(plan.Manual, ManualDep{ID: d.ID, Range: d.Range, RequiredBy: f.lf.Title, File: name})
			}
		}
	}
}
