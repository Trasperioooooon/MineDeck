package mcsrc

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/errhint"
)

const (
	forgeBMCL          = "https://bmclapi2.bangbang93.com/forge"
	forgeMavenOfficial = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	forgePromosURL     = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
)

type forgeBuild struct {
	Build     int64  `json:"build"`
	Version   string `json:"version"`
	McVersion string `json:"mcversion"`
	Files     []struct {
		Format   string `json:"format"`
		Category string `json:"category"`
		Hash     string `json:"hash"`
	} `json:"files"`
}

// forgePromos 获取官方推荐/最新版本映射（best-effort，失败返回空表）。
func forgePromos(ctx context.Context) map[string]string {
	var p struct {
		Promos map[string]string `json:"promos"`
	}
	if err := dl.FetchJSON(ctx, []string{forgePromosURL}, &p); err != nil {
		return map[string]string{}
	}
	return p.Promos
}

// ForgeMCVersions 列出 Forge 支持的 MC 版本（镜像优先，官方 promotions 兜底）。
func ForgeMCVersions(ctx context.Context) ([]string, error) {
	var ids []string
	if !app.OfficialOnly() {
		if err := dl.FetchJSON(ctx, []string{forgeBMCL + "/minecraft"}, &ids); err != nil {
			ids = nil
		}
	}
	if len(ids) == 0 {
		promos := forgePromos(ctx)
		if len(promos) == 0 {
			return nil, errhint.Wrap(fmt.Errorf("无法获取 Forge 支持版本列表（镜像与官方源均失败）"), mcsrcFetchHint)
		}
		seen := map[string]bool{}
		for k := range promos {
			id := strings.TrimSuffix(strings.TrimSuffix(k, "-latest"), "-recommended")
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return orderAndFloor(ctx, ids)
}

// ForgeBuilds 列出某 MC 版本的全部 Forge 构建（新在前）。
func ForgeBuilds(ctx context.Context, mc string) ([]BuildInfo, error) {
	recommended := forgePromos(ctx)[mc+"-recommended"]
	builds, err := forgeBuildList(ctx, mc)
	if err == nil && len(builds) > 0 {
		sort.Slice(builds, func(a, b int) bool { return builds[a].Build > builds[b].Build })
		out := make([]BuildInfo, 0, len(builds))
		for _, b := range builds {
			out = append(out, BuildInfo{ID: b.Version, Recommended: b.Version == recommended})
		}
		return out, nil
	}
	// 官方 maven-metadata.xml 兜底
	vers, merr := forgeMavenVersions(ctx, mc)
	if merr != nil {
		if err == nil {
			err = merr
		}
		return nil, errhint.Wrap(fmt.Errorf("获取 Forge 构建列表失败: %w", err), mcsrcFetchHint)
	}
	out := make([]BuildInfo, 0, len(vers))
	for i := len(vers) - 1; i >= 0; i-- { // metadata 为旧在前
		out = append(out, BuildInfo{ID: vers[i], Recommended: vers[i] == recommended})
	}
	return out, nil
}

func forgeBuildList(ctx context.Context, mc string) ([]forgeBuild, error) {
	if app.OfficialOnly() {
		return nil, fmt.Errorf("已配置仅官方源")
	}
	var builds []forgeBuild
	err := dl.FetchJSON(ctx, []string{forgeBMCL + "/minecraft/" + url.PathEscape(mc)}, &builds)
	return builds, err
}

func forgeMavenVersions(ctx context.Context, mc string) ([]string, error) {
	b, err := dl.FetchBytes(ctx, []string{forgeMavenOfficial + "/maven-metadata.xml"})
	if err != nil {
		return nil, err
	}
	var meta struct {
		Versioning struct {
			Versions struct {
				Version []string `xml:"version"`
			} `xml:"versions"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal(b, &meta); err != nil {
		return nil, err
	}
	prefix := mc + "-"
	var out []string
	for _, v := range meta.Versioning.Versions.Version {
		if strings.HasPrefix(v, prefix) {
			out = append(out, strings.TrimPrefix(v, prefix))
		}
	}
	return out, nil
}

// ForgeInstallerArtifact 解析 Forge 安装器下载信息（含 SHA-1，如镜像列表可用）。
func ForgeInstallerArtifact(ctx context.Context, mc, ver string) Artifact {
	sha1 := ""
	if builds, err := forgeBuildList(ctx, mc); err == nil {
		for _, b := range builds {
			if b.Version != ver {
				continue
			}
			for _, f := range b.Files {
				if f.Category == "installer" && f.Format == "jar" {
					sha1 = f.Hash
				}
			}
		}
	}
	fname := fmt.Sprintf("forge-%s-%s-installer.jar", mc, ver)
	official := fmt.Sprintf("%s/%s-%s/%s", forgeMavenOfficial, mc, ver, fname)
	mirror := fmt.Sprintf("%s/download?mcversion=%s&version=%s&category=installer&format=jar",
		forgeBMCL, url.QueryEscape(mc), url.QueryEscape(ver))
	return Artifact{
		URLs:     Order2(mirror, official),
		SHA1:     sha1,
		FileName: fname,
		MinSize:  512 * 1024,
	}
}
