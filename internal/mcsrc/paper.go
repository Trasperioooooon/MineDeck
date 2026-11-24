package mcsrc

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"minedeck/internal/dl"
)

// PaperMC 已于 2025 年下线 api.papermc.io v2（HTTP 410），现行为 Fill v3。
// Fill v3 同时托管 paper / folia / velocity / waterfall 等项目，这里做统一适配。
const fillRoot = "https://fill.papermc.io/v3/projects/"

type fillDownload struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Checksums struct {
		SHA256 string `json:"sha256"`
	} `json:"checksums"`
}

type fillBuild struct {
	ID        int64                   `json:"id"`
	Channel   string                  `json:"channel"`
	Downloads map[string]fillDownload `json:"downloads"`
}

// fillVersions 列出某 Fill 项目的版本。game=true 时按 MC 版本过滤排序，
// 代理端（velocity 等）的版本号是自有体系，原样返回（Fill 为新在前）。
func fillVersions(ctx context.Context, project string, game bool) ([]string, error) {
	var resp struct {
		Versions []struct {
			Version struct {
				ID string `json:"id"`
			} `json:"version"`
		} `json:"versions"`
	}
	if err := dl.FetchJSON(ctx, []string{fillRoot + project + "/versions"}, &resp); err != nil {
		return nil, fmt.Errorf("获取 %s 版本列表失败: %w", project, err)
	}
	var ids []string
	for _, v := range resp.Versions {
		id := v.Version.ID
		if game && (strings.Contains(id, "-rc") || strings.Contains(id, "-pre") || strings.Contains(id, "snapshot")) {
			continue
		}
		ids = append(ids, id)
	}
	if game {
		return orderAndFloor(ctx, ids)
	}
	return ids, nil
}

func fillBuilds(ctx context.Context, project, mc string) ([]BuildInfo, error) {
	builds, err := fillBuildList(ctx, project, mc)
	if err != nil {
		return nil, err
	}
	out := make([]BuildInfo, 0, len(builds))
	for _, b := range builds {
		out = append(out, BuildInfo{
			ID:          strconv.FormatInt(b.ID, 10),
			Label:       b.Channel,
			Recommended: len(out) == 0,
		})
	}
	return out, nil
}

func fillBuildList(ctx context.Context, project, mc string) ([]fillBuild, error) {
	var builds []fillBuild
	u := fillRoot + project + "/versions/" + url.PathEscape(mc) + "/builds"
	if err := dl.FetchJSON(ctx, []string{u}, &builds); err != nil {
		return nil, fmt.Errorf("获取 %s 构建列表失败: %w", project, err)
	}
	sort.Slice(builds, func(a, b int) bool { return builds[a].ID > builds[b].ID })
	return builds, nil
}

// fillArtifact 解析下载描述；build 为空或 "latest" 取最新。
func fillArtifact(ctx context.Context, project, mc, build string) (Artifact, error) {
	var chosen *fillBuild
	if build == "" || build == "latest" {
		var b fillBuild
		u := fillRoot + project + "/versions/" + url.PathEscape(mc) + "/builds/latest"
		if err := dl.FetchJSON(ctx, []string{u}, &b); err != nil {
			return Artifact{}, fmt.Errorf("获取 %s 最新构建失败: %w", project, err)
		}
		chosen = &b
	} else {
		builds, err := fillBuildList(ctx, project, mc)
		if err != nil {
			return Artifact{}, err
		}
		for i := range builds {
			if strconv.FormatInt(builds[i].ID, 10) == build {
				chosen = &builds[i]
				break
			}
		}
		if chosen == nil {
			return Artifact{}, fmt.Errorf("未找到 %s 构建 #%s", project, build)
		}
	}
	d, ok := chosen.Downloads["server:default"]
	if !ok || d.URL == "" {
		return Artifact{}, fmt.Errorf("%s 构建 #%d 缺少服务端下载信息", project, chosen.ID)
	}
	return Artifact{
		URLs:     []string{d.URL},
		SHA256:   d.Checksums.SHA256,
		FileName: d.Name,
		MinSize:  256 * 1024, // 代理端 jar 仅数 MB，游戏端数十 MB
	}, nil
}

// ---- 各项目的具名包装（供 registry 注册） ----

func PaperVersions(ctx context.Context) ([]string, error) { return fillVersions(ctx, "paper", true) }
func PaperBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return fillBuilds(ctx, "paper", mc)
}
func PaperArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return fillArtifact(ctx, "paper", mc, build)
}

func FoliaVersions(ctx context.Context) ([]string, error) { return fillVersions(ctx, "folia", true) }
func FoliaBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return fillBuilds(ctx, "folia", mc)
}
func FoliaArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return fillArtifact(ctx, "folia", mc, build)
}

func VelocityVersions(ctx context.Context) ([]string, error) {
	return fillVersions(ctx, "velocity", false)
}
func VelocityBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return fillBuilds(ctx, "velocity", mc)
}
func VelocityArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return fillArtifact(ctx, "velocity", mc, build)
}

func WaterfallVersions(ctx context.Context) ([]string, error) {
	return fillVersions(ctx, "waterfall", false)
}
func WaterfallBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return fillBuilds(ctx, "waterfall", mc)
}
func WaterfallArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return fillArtifact(ctx, "waterfall", mc, build)
}
