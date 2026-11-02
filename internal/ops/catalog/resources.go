package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
)

// resourceSpec 按核心决定可安装的资源类型、Modrinth loader 过滤与安装目录。
func resourceSpec(core mcsrc.Core) (rtype string, loaders []string, dir string, ok bool) {
	switch core {
	case mcsrc.CoreFabric:
		return "mod", []string{"fabric"}, "mods", true
	case mcsrc.CoreForge:
		return "mod", []string{"forge"}, "mods", true
	case mcsrc.CoreNeoForge:
		return "mod", []string{"neoforge"}, "mods", true
	case mcsrc.CoreMohist:
		return "mod", []string{"forge"}, "mods", true
	case mcsrc.CoreBanner:
		return "mod", []string{"fabric"}, "mods", true
	case mcsrc.CorePaper, mcsrc.CorePurpur, mcsrc.CoreLeaves, mcsrc.CoreFolia:
		return "plugin", []string{"paper", "spigot", "bukkit"}, "plugins", true
	case mcsrc.CoreVelocity:
		return "plugin", []string{"velocity"}, "plugins", true
	case mcsrc.CoreWaterfall:
		return "plugin", []string{"waterfall", "bungeecord"}, "plugins", true
	}
	return "", nil, "", false
}

const modrinthAPI = "https://api.modrinth.com/v2"

type resourceArgs struct {
	Inst      string `json:"inst"`
	Q         string `json:"q"`
	ProjectID string `json:"projectId"`
}

func (c *cat) resourceOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.resources.search", Title: "在 Modrinth 搜索模组或插件", Level: ops.Read,
		Desc:   "按实例的核心和版本过滤（要联网），按下载量排序取前 20 个。",
		Params: withInst(map[string]*ops.Schema{"q": ops.String("搜索词")}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[resourceArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			rtype, loaders, _, ok := resourceSpec(i.Core)
			if !ok {
				return nil, fmt.Errorf("该核心不支持安装模组/插件")
			}
			// facets: 项目类型 + 加载器（同组 OR，跨组 AND）；插件不强卡 MC 版本（兼容面广）
			loaderFacet := make([]string, 0, len(loaders))
			for _, l := range loaders {
				loaderFacet = append(loaderFacet, "categories:"+l)
			}
			facets := []any{[]string{"project_type:" + rtype}, loaderFacet}
			if rtype == "mod" {
				facets = append(facets, []string{"versions:" + i.MC})
			}
			fb, _ := json.Marshal(facets)
			u := fmt.Sprintf("%s/search?limit=20&index=downloads&query=%s&facets=%s",
				modrinthAPI, url.QueryEscape(p.Q), url.QueryEscape(string(fb)))
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var resp struct {
				Hits []struct {
					ProjectID   string `json:"project_id"`
					Slug        string `json:"slug"`
					Title       string `json:"title"`
					Description string `json:"description"`
					Downloads   int64  `json:"downloads"`
					IconURL     string `json:"icon_url"`
				} `json:"hits"`
			}
			if err := dl.FetchJSON(ctx, []string{u}, &resp); err != nil {
				return nil, ops.Errorf(502, "Modrinth 搜索失败: %w", err)
			}
			return map[string]any{"type": rtype, "hits": resp.Hits}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.resources.install", Title: "从 Modrinth 安装模组或插件", Level: ops.Write,
		Desc:   "下载这个项目兼容当前核心和版本的最新文件（校验 SHA-1），放进 mods 或 plugins。",
		Params: withInst(map[string]*ops.Schema{"projectId": ops.String("Modrinth 项目 ID（见搜索结果的 project_id）")}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[resourceArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: fmt.Sprintf("给「%s」（%s %s）安装 Modrinth 项目 %s", p.Inst, i.Core, i.MC, p.ProjectID)}, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[resourceArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			if p.ProjectID == "" {
				return nil, fmt.Errorf("缺少项目 ID")
			}
			rtype, loaders, destDir, ok := resourceSpec(i.Core)
			if !ok {
				return nil, fmt.Errorf("该核心不支持安装模组/插件")
			}
			lj, _ := json.Marshal(loaders)
			u := fmt.Sprintf("%s/project/%s/version?loaders=%s",
				modrinthAPI, url.PathEscape(p.ProjectID), url.QueryEscape(string(lj)))
			if rtype == "mod" {
				gj, _ := json.Marshal([]string{i.MC})
				u += "&game_versions=" + url.QueryEscape(string(gj))
			}
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			var versions []struct {
				Name  string `json:"name"`
				Files []struct {
					URL      string `json:"url"`
					Filename string `json:"filename"`
					Primary  bool   `json:"primary"`
					Hashes   struct {
						SHA1 string `json:"sha1"`
					} `json:"hashes"`
				} `json:"files"`
			}
			if err := dl.FetchJSON(ctx, []string{u}, &versions); err != nil {
				return nil, ops.Errorf(502, "获取版本信息失败: %w", err)
			}
			if len(versions) == 0 {
				return nil, ops.Errorf(404, "该资源没有兼容 %s 的版本", i.MC)
			}
			v := versions[0]
			if len(v.Files) == 0 {
				return nil, ops.Errorf(404, "该版本没有可下载文件")
			}
			file := v.Files[0]
			for _, f := range v.Files {
				if f.Primary {
					file = f
					break
				}
			}
			fname := filepath.Base(file.Filename)
			dest := filepath.Join(i.Dir, destDir, fname)
			if err := dl.Fetch(ctx, dl.Request{
				URLs: []string{file.URL}, Dest: dest, SHA1: file.Hashes.SHA1, MinSize: 1024,
			}, nil); err != nil {
				return nil, ops.Errorf(502, "下载失败: %w", err)
			}
			return map[string]string{"file": destDir + "/" + fname, "version": v.Name}, nil
		},
	})
}
