package mcsrc

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/errhint"
)

const (
	neoBMCL  = "https://bmclapi2.bangbang93.com/neoforge"
	neoMaven = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	// neoForgeCDN 和官方 maven 是同一套 /releases 路径（Reposilite）。
	// 2026-11-20 核对过：maven-metadata.xml 能取到，26.3.0.115 的 installer.jar.sha1
	// 和 maven.neoforged.net 上的 40 位校验值相同。安装器 jar 只有在拿到官方这份校验值时才把 CDN 加进候选。
	// https://maven.fastmirror.net/ 当时返回的是一段设置 cookie 再刷新的 HTML，不是 maven-metadata.xml，没有收进下载源。
	neoForgeCDN = "https://neoforged.forgecdn.net/releases/net/neoforged/neoforge"
	// NeoForge 与 Forge 在 1.20.1 时代纠缠不清，本工具从 1.20.2 起提供 NeoForge
	neoFloor = "1.20.2"
)

type neoEntry struct {
	Version       string `json:"version"`
	RawVersion    string `json:"rawVersion"`
	McVersion     string `json:"mcversion"`
	InstallerPath string `json:"installerPath"`
}

// NeoMCVersions 列出可用 NeoForge 的 MC 版本（>= 1.20.2 的正式版）。
func NeoMCVersions(ctx context.Context) ([]MCVersion, error) {
	all, err := vanillaVersions(ctx, false)
	if err != nil {
		return nil, err
	}
	_, idx, err := getManifest(ctx)
	if err != nil {
		return nil, err
	}
	var out []MCVersion
	for _, v := range all {
		if newerOrEqualIdx(idx, v.ID, neoFloor) {
			out = append(out, v)
		}
	}
	return out, nil
}

// NeoBuilds 列出某 MC 版本的全部 NeoForge 构建（新在前），并返回 version→installerPath 映射。
func NeoBuilds(ctx context.Context, mc string) ([]BuildInfo, map[string]string, error) {
	paths := map[string]string{}
	if !app.OfficialOnly() {
		var list []neoEntry
		if err := dl.FetchJSON(ctx, []string{neoBMCL + "/list/" + url.PathEscape(mc)}, &list); err == nil && len(list) > 0 {
			out := make([]BuildInfo, 0, len(list))
			for i := len(list) - 1; i >= 0; i-- { // 列表为旧在前
				e := list[i]
				out = append(out, BuildInfo{
					ID:          e.Version,
					Recommended: len(out) == 0 && !strings.Contains(e.Version, "beta"),
				})
				paths[e.Version] = e.InstallerPath
			}
			return out, paths, nil
		}
	}
	// 官方 maven-metadata.xml 兜底
	vers, err := neoMavenVersions(ctx, mc)
	if err != nil || len(vers) == 0 {
		if err == nil {
			err = fmt.Errorf("该版本暂无 NeoForge 构建")
		}
		return nil, nil, errhint.Wrap(fmt.Errorf("获取 NeoForge 构建列表失败: %w", err), mcsrcFetchHint)
	}
	out := make([]BuildInfo, 0, len(vers))
	for i := len(vers) - 1; i >= 0; i-- {
		out = append(out, BuildInfo{ID: vers[i], Recommended: len(out) == 0})
	}
	return out, paths, nil
}

// mcToNeoPrefixes MC 版本号 → NeoForge 版本前缀的启发式映射
// （如 1.21.1 → 21.1.，26.2 → 26.2. 或 26.2-）。
func mcToNeoPrefixes(mc string) []string {
	if strings.HasPrefix(mc, "1.") {
		rest := strings.TrimPrefix(mc, "1.")
		parts := strings.SplitN(rest, ".", 2)
		minor := parts[0]
		patch := "0"
		if len(parts) == 2 {
			patch = parts[1]
		}
		return []string{minor + "." + patch + "."}
	}
	return []string{mc + ".", mc + "-"}
}

func neoMavenVersions(ctx context.Context, mc string) ([]string, error) {
	b, err := dl.FetchBytes(ctx, []string{neoMaven + "/maven-metadata.xml"})
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
	prefixes := mcToNeoPrefixes(mc)
	var out []string
	for _, v := range meta.Versioning.Versions.Version {
		for _, p := range prefixes {
			if strings.HasPrefix(v, p) {
				out = append(out, v)
				break
			}
		}
	}
	return out, nil
}

var (
	neoMavenBase    = neoMaven
	neoForgeCDNBase = neoForgeCDN
	neoSidecarWait  = 10 * time.Second
)

// NeoInstallerArtifact 解析 NeoForge 安装器下载信息。
// 会向官方 maven 要 installer.jar.sha1（小文件，不下载 jar）。要到了才把 neoforged.forgecdn.net 加进候选，
// 并且三个地址都按这份官方校验值核对。要不到校验值时不使用 CDN，地址和以前一样（镜像策略决定 BMCLAPI / 官方）。
// 安装器自己再用 --mirror 拉依赖的行为没有改：那一步信任的是 BMCLAPI，不是这次没逐个核对过的 CDN。
func NeoInstallerArtifact(ctx context.Context, ver, installerPath string) Artifact {
	ver = strings.TrimSpace(ver)
	escVer := url.PathEscape(ver)
	if installerPath == "" {
		installerPath = fmt.Sprintf("/maven/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", escVer, escVer)
	}
	mirror := "https://bmclapi2.bangbang93.com" + installerPath
	official := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", strings.TrimRight(neoMavenBase, "/"), escVer, escVer)
	urls := Order2(mirror, official)
	sha := ""
	if got, ok := neoOfficialInstallerSHA1(ctx, ver); ok {
		sha = got
		if !app.OfficialOnly() {
			// 排在原来的首选后面：forgecdn 在国内的连通性没实测过，排第一的话连不上或很慢时每次都先在它上面耗着。
			// 首选（BMCLAPI 或官方，看下载源设置）失败了再试它，仍按官方校验值核对。
			cdn := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", strings.TrimRight(neoForgeCDNBase, "/"), escVer, escVer)
			urls = append([]string{urls[0], cdn}, urls[1:]...)
		}
	}
	fileName := "neoforge-installer.jar"
	if validNeoVersion(ver) {
		fileName = fmt.Sprintf("neoforge-%s-installer.jar", ver)
	}
	return Artifact{
		URLs:     urls,
		SHA1:     sha,
		FileName: fileName,
		MinSize:  512 * 1024,
	}
}

func validNeoVersion(ver string) bool {
	if ver == "" || ver == "." || ver == ".." || len(ver) > 80 {
		return false
	}
	for _, c := range ver {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '.' || c == '-' || c == '_' || c == '+') {
			return false
		}
	}
	return true
}

// neoOfficialInstallerSHA1 只从官方 maven 读 .sha1。内容必须是 40 位十六进制，HTML 或别的正文不算。
// 单独设短超时且跳转只跟 https：官方源不通时不让创建流程白等一分钟，也不允许降级跳转篡改校验值。
func neoOfficialInstallerSHA1(ctx context.Context, ver string) (string, bool) {
	if !validNeoVersion(ver) {
		return "", false
	}
	escVer := url.PathEscape(ver)
	u := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar.sha1", strings.TrimRight(neoMavenBase, "/"), escVer, escVer)
	sctx, cancel := context.WithTimeout(ctx, neoSidecarWait)
	defer cancel()
	b, err := dl.FetchBytesHTTPS(sctx, []string{u})
	if err != nil {
		return "", false
	}
	return parseSHA1Sidecar(b)
}

// parseSHA1Sidecar 认出 maven 的 .sha1 文件。只认单行、第一段恰好 40 位十六进制（可选跟一个文件名）；HTML 或多行正文拒绝。
func parseSHA1Sidecar(b []byte) (string, bool) {
	if len(b) == 0 || len(b) > 512 || bytes.ContainsAny(b, "<>") {
		return "", false
	}
	trimmed := strings.TrimSpace(string(b))
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || len(fields) > 2 {
		return "", false
	}
	s := strings.ToLower(fields[0])
	if len(s) != 40 {
		return "", false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", false
		}
	}
	return s, true
}
