package mcsrc

import (
	"context"
	"fmt"
	"net/url"

	"minedeck/internal/dl"
	"minedeck/internal/errhint"
)

const (
	fabricMetaOfficial = "https://meta.fabricmc.net"
	fabricMetaMirror   = "https://bmclapi2.bangbang93.com/fabric-meta"
)

func fabricURLs(path string) []string {
	return Order2(fabricMetaMirror+path, fabricMetaOfficial+path)
}

// FabricMCVersions 列出 Fabric 支持的稳定版 MC 版本。
func FabricMCVersions(ctx context.Context) ([]string, error) {
	var games []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := dl.FetchJSON(ctx, fabricURLs("/v2/versions/game"), &games); err != nil {
		return nil, errhint.Wrap(fmt.Errorf("获取 Fabric 版本列表失败: %w", err), mcsrcFetchHint)
	}
	var ids []string
	for _, g := range games {
		if g.Stable {
			ids = append(ids, g.Version)
		}
	}
	return orderAndFloor(ctx, ids)
}

// FabricLoaders 列出某 MC 版本可用的 Fabric Loader（新在前）。
func FabricLoaders(ctx context.Context, mc string) ([]BuildInfo, error) {
	var list []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := dl.FetchJSON(ctx, fabricURLs("/v2/versions/loader/"+url.PathEscape(mc)), &list); err != nil {
		return nil, errhint.Wrap(fmt.Errorf("获取 Fabric Loader 列表失败: %w", err), mcsrcFetchHint)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("MC %s 暂无 Fabric 支持", mc)
	}
	out := make([]BuildInfo, 0, len(list))
	markedRec := false
	for _, l := range list {
		rec := false
		if l.Loader.Stable && !markedRec {
			rec, markedRec = true, true
		}
		out = append(out, BuildInfo{ID: l.Loader.Version, Recommended: rec})
	}
	return out, nil
}

// FabricInstallerVersion 获取最新稳定版安装器版本号。
func FabricInstallerVersion(ctx context.Context) (string, error) {
	var list []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := dl.FetchJSON(ctx, fabricURLs("/v2/versions/installer"), &list); err != nil {
		return "", errhint.Wrap(fmt.Errorf("获取 Fabric 安装器版本失败: %w", err), mcsrcFetchHint)
	}
	for _, i := range list {
		if i.Stable {
			return i.Version, nil
		}
	}
	if len(list) > 0 {
		return list[0].Version, nil
	}
	return "", fmt.Errorf("Fabric 安装器版本列表为空")
}

// FabricServerArtifact 一体化服务端 launcher jar 的下载描述。
func FabricServerArtifact(mc, loader, installer string) Artifact {
	path := fmt.Sprintf("/v2/versions/loader/%s/%s/%s/server/jar",
		url.PathEscape(mc), url.PathEscape(loader), url.PathEscape(installer))
	return Artifact{
		URLs:     fabricURLs(path),
		FileName: fmt.Sprintf("fabric-server-mc.%s-loader.%s-launcher.%s.jar", mc, loader, installer),
		MinSize:  40 * 1024,
	}
}
