package mcsrc

import (
	"context"
	"fmt"
	"net/url"

	"minedeck/internal/dl"
)

const purpurBase = "https://api.purpurmc.org/v2/purpur"

// PurpurVersions 列出 Purpur 支持的 MC 版本。
func PurpurVersions(ctx context.Context) ([]string, error) {
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := dl.FetchJSON(ctx, []string{purpurBase}, &resp); err != nil {
		return nil, fmt.Errorf("获取 Purpur 版本列表失败: %w", err)
	}
	return orderAndFloor(ctx, resp.Versions)
}

// PurpurBuilds 列出某 MC 版本的 Purpur 构建（新在前）。
func PurpurBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	var resp struct {
		Builds struct {
			Latest string   `json:"latest"`
			All    []string `json:"all"`
		} `json:"builds"`
	}
	if err := dl.FetchJSON(ctx, []string{purpurBase + "/" + url.PathEscape(mc)}, &resp); err != nil {
		return nil, fmt.Errorf("获取 Purpur 构建列表失败: %w", err)
	}
	out := make([]BuildInfo, 0, len(resp.Builds.All))
	for i := len(resp.Builds.All) - 1; i >= 0; i-- { // all 为旧在前
		b := resp.Builds.All[i]
		out = append(out, BuildInfo{ID: b, Recommended: b == resp.Builds.Latest})
	}
	return out, nil
}

// PurpurArtifact 解析 Purpur 服务端 jar 的下载描述；build 为空或 "latest" 取最新。
func PurpurArtifact(ctx context.Context, mc, build string) (Artifact, error) {
	if build == "" || build == "latest" {
		var resp struct {
			Builds struct {
				Latest string `json:"latest"`
			} `json:"builds"`
		}
		if err := dl.FetchJSON(ctx, []string{purpurBase + "/" + url.PathEscape(mc)}, &resp); err != nil {
			return Artifact{}, fmt.Errorf("获取 Purpur 最新构建失败: %w", err)
		}
		build = resp.Builds.Latest
	}
	var info struct {
		MD5 string `json:"md5"`
	}
	if err := dl.FetchJSON(ctx, []string{purpurBase + "/" + url.PathEscape(mc) + "/" + url.PathEscape(build)}, &info); err != nil {
		return Artifact{}, fmt.Errorf("获取 Purpur 构建信息失败: %w", err)
	}
	return Artifact{
		URLs:     []string{fmt.Sprintf("%s/%s/%s/download", purpurBase, url.PathEscape(mc), url.PathEscape(build))},
		MD5:      info.MD5,
		FileName: fmt.Sprintf("purpur-%s-%s.jar", mc, build),
		MinSize:  1 << 20,
	}, nil
}
