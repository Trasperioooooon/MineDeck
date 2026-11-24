package mcsrc

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"minedeck/internal/dl"
)

// LeavesMC API（Paper v2 风格）：Leaves 是修复原版特性的 Paper 分支，国人维护。
const leavesRoot = "https://api.leavesmc.org/v2/projects/leaves"

type leavesBuild struct {
	Build     int64  `json:"build"`
	Channel   string `json:"channel"`
	Downloads struct {
		Application struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"application"`
	} `json:"downloads"`
}

func LeavesVersions(ctx context.Context) ([]string, error) {
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := dl.FetchJSON(ctx, []string{leavesRoot}, &resp); err != nil {
		return nil, fmt.Errorf("获取 Leaves 版本列表失败: %w", err)
	}
	return orderAndFloor(ctx, resp.Versions)
}

func leavesBuildList(ctx context.Context, mc string) ([]leavesBuild, error) {
	var resp struct {
		Builds []leavesBuild `json:"builds"`
	}
	u := leavesRoot + "/versions/" + url.PathEscape(mc) + "/builds"
	if err := dl.FetchJSON(ctx, []string{u}, &resp); err != nil {
		return nil, fmt.Errorf("获取 Leaves 构建列表失败: %w", err)
	}
	if len(resp.Builds) == 0 {
		return nil, fmt.Errorf("MC %s 暂无 Leaves 构建", mc)
	}
	sort.Slice(resp.Builds, func(a, b int) bool { return resp.Builds[a].Build > resp.Builds[b].Build })
	return resp.Builds, nil
}

func LeavesBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	builds, err := leavesBuildList(ctx, mc)
	if err != nil {
		return nil, err
	}
	out := make([]BuildInfo, 0, len(builds))
	for _, b := range builds {
		out = append(out, BuildInfo{
			ID:          strconv.FormatInt(b.Build, 10),
			Label:       b.Channel,
			Recommended: len(out) == 0,
		})
	}
	return out, nil
}

func LeavesArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	builds, err := leavesBuildList(ctx, mc)
	if err != nil {
		return Artifact{}, err
	}
	var chosen *leavesBuild
	if build == "" || build == "latest" {
		chosen = &builds[0]
	} else {
		for i := range builds {
			if strconv.FormatInt(builds[i].Build, 10) == build {
				chosen = &builds[i]
				break
			}
		}
	}
	if chosen == nil {
		return Artifact{}, fmt.Errorf("未找到 Leaves 构建 #%s", build)
	}
	app := chosen.Downloads.Application
	if app.Name == "" {
		return Artifact{}, fmt.Errorf("Leaves 构建 #%d 缺少下载信息", chosen.Build)
	}
	return Artifact{
		URLs: []string{fmt.Sprintf("%s/versions/%s/builds/%d/downloads/%s",
			leavesRoot, url.PathEscape(mc), chosen.Build, url.PathEscape(app.Name))},
		SHA256:   app.SHA256,
		FileName: app.Name,
		MinSize:  1 << 20,
	}, nil
}
