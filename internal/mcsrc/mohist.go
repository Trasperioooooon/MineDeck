package mcsrc

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"minedeck/internal/dl"
)

// MohistMC 新版 API（api.mohistmc.com）：托管 Mohist（Forge+Bukkit 混合服）
// 与 Banner（Fabric+Bukkit 混合服）。
// 注意：旧域名 mohistmc.com/api/v2 已是僵尸接口（返回旧数据且下载损坏），勿用。
const mohistAPI = "https://api.mohistmc.com/project/"

type mohistBuild struct {
	ID         int64     `json:"id"`
	FileSHA256 string    `json:"file_sha256"`
	BuildDate  time.Time `json:"build_date"`
}

func mohistVersions(ctx context.Context, project string) ([]string, error) {
	var list []struct {
		Name string `json:"name"`
	}
	if err := dl.FetchJSON(ctx, []string{mohistAPI + project + "/versions"}, &list); err != nil {
		return nil, fmt.Errorf("获取 %s 版本列表失败: %w", project, err)
	}
	ids := make([]string, 0, len(list))
	for _, v := range list {
		ids = append(ids, v.Name)
	}
	return orderAndFloor(ctx, ids)
}

func mohistBuildList(ctx context.Context, project, mc string) ([]mohistBuild, error) {
	var builds []mohistBuild
	u := mohistAPI + project + "/" + url.PathEscape(mc) + "/builds"
	if err := dl.FetchJSON(ctx, []string{u}, &builds); err != nil {
		return nil, fmt.Errorf("获取 %s 构建列表失败: %w", project, err)
	}
	if len(builds) == 0 {
		return nil, fmt.Errorf("MC %s 暂无 %s 构建", mc, project)
	}
	sort.Slice(builds, func(a, b int) bool { return builds[a].ID > builds[b].ID })
	return builds, nil
}

func mohistBuilds(ctx context.Context, project, mc string) ([]BuildInfo, error) {
	builds, err := mohistBuildList(ctx, project, mc)
	if err != nil {
		return nil, err
	}
	out := make([]BuildInfo, 0, len(builds))
	for _, b := range builds {
		out = append(out, BuildInfo{
			ID:          strconv.FormatInt(b.ID, 10),
			Label:       b.BuildDate.Format("2006-01-02"),
			Recommended: len(out) == 0,
		})
	}
	return out, nil
}

func mohistArtifact(ctx context.Context, project, mc, build string) (Artifact, error) {
	builds, err := mohistBuildList(ctx, project, mc)
	if err != nil {
		return Artifact{}, err
	}
	var chosen *mohistBuild
	if build == "" || build == "latest" {
		chosen = &builds[0]
	} else {
		for i := range builds {
			if strconv.FormatInt(builds[i].ID, 10) == build {
				chosen = &builds[i]
				break
			}
		}
	}
	if chosen == nil {
		return Artifact{}, fmt.Errorf("未找到 %s 构建 #%s", project, build)
	}
	return Artifact{
		URLs: []string{fmt.Sprintf("%s%s/%s/builds/%d/download",
			mohistAPI, project, url.PathEscape(mc), chosen.ID)},
		SHA256:   chosen.FileSHA256,
		FileName: fmt.Sprintf("%s-%s-%d.jar", project, mc, chosen.ID),
		MinSize:  1 << 20,
	}, nil
}

func MohistVersions(ctx context.Context) ([]string, error) { return mohistVersions(ctx, "mohist") }
func MohistBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return mohistBuilds(ctx, "mohist", mc)
}
func MohistArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return mohistArtifact(ctx, "mohist", mc, build)
}

func BannerVersions(ctx context.Context) ([]string, error) { return mohistVersions(ctx, "banner") }
func BannerBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	return mohistBuilds(ctx, "banner", mc)
}
func BannerArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	return mohistArtifact(ctx, "banner", mc, build)
}
