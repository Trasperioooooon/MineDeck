package mcsrc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
)

const (
	officialManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	bmclapiHost         = "bmclapi2.bangbang93.com"
	// FloorVersion 为本工具支持的最老 MC 版本
	FloorVersion = "1.12.2"
)

// MirrorSwap 将 Mojang 官方域名替换为 BMCLAPI 镜像域名。
func MirrorSwap(u string) string {
	for _, h := range []string{
		"piston-meta.mojang.com",
		"piston-data.mojang.com",
		"launchermeta.mojang.com",
		"launcher.mojang.com",
	} {
		u = strings.Replace(u, h, bmclapiHost, 1)
	}
	return u
}

// URLPair 依据下载源策略给出官方 URL 与其镜像的尝试顺序。
func URLPair(official string) []string {
	mirror := MirrorSwap(official)
	if mirror == official {
		return []string{official}
	}
	return Order2(mirror, official)
}

// Order2 依据下载源策略排序（mirror 为镜像地址，official 为官方地址）。
func Order2(mirror, official string) []string {
	switch {
	case app.OfficialOnly():
		return []string{official}
	case app.MirrorOnly():
		return []string{mirror}
	case app.MirrorFirst():
		return []string{mirror, official}
	default:
		return []string{official, mirror}
	}
}

type ManifestVersion struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	SHA1        string `json:"sha1"`
	ReleaseTime string `json:"releaseTime"` // 如 "2023-06-12T13:25:51+00:00"（另有 time 字段是清单条目的修改时间，勿混用）
}

type manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []ManifestVersion `json:"versions"`
}

var (
	maniMu   sync.Mutex
	maniData *manifest
	maniIdx  map[string]int
	maniAt   time.Time
	// maniSnap 为最近一次加载的清单索引（建成后只读），供 NewerOrEqualCached 无锁读取：
	// getManifest 联网刷新期间一直持有 maniMu，离线判定不应排在它后面
	maniSnap atomic.Pointer[map[string]int]
)

func manifestCachePath() string { return filepath.Join(app.CacheDir, "version_manifest_v2.json") }

// indexVersions 建立 版本号 → 清单位置 的索引（清单新版本在前）。
func indexVersions(m *manifest) map[string]int {
	idx := make(map[string]int, len(m.Versions))
	for i, v := range m.Versions {
		idx[v.ID] = i
	}
	return idx
}

// getManifest 获取版本清单（内存缓存 10 分钟；网络全挂时回退磁盘缓存）。
func getManifest(ctx context.Context) (*manifest, map[string]int, error) {
	maniMu.Lock()
	defer maniMu.Unlock()
	if maniData != nil && time.Since(maniAt) < 10*time.Minute {
		return maniData, maniIdx, nil
	}
	var m manifest
	cachePath := manifestCachePath()
	b, err := dl.FetchBytes(ctx, URLPair(officialManifestURL))
	if err == nil {
		if jerr := json.Unmarshal(b, &m); jerr == nil && len(m.Versions) > 0 {
			_ = os.WriteFile(cachePath, b, 0o644)
		} else {
			err = fmt.Errorf("清单解析失败")
		}
	}
	if err != nil {
		if cb, cerr := os.ReadFile(cachePath); cerr == nil &&
			json.Unmarshal(cb, &m) == nil && len(m.Versions) > 0 {
			err = nil
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("获取 Minecraft 版本清单失败: %w", err)
	}
	idx := indexVersions(&m)
	maniData, maniIdx, maniAt = &m, idx, time.Now()
	maniSnap.Store(&idx)
	return maniData, maniIdx, nil
}

// newerOrEqualIdx 基于清单顺序（新版本在前）判断 a 是否不老于 b；未知版本视为较旧。
func newerOrEqualIdx(idx map[string]int, a, b string) bool {
	ia, oka := idx[a]
	ib, okb := idx[b]
	if !oka || !okb {
		return false
	}
	return ia <= ib
}

// NewerOrEqual 判断版本 a 是否不老于版本 b（依据官方清单发布顺序，兼容 26.x 新版本号）。
func NewerOrEqual(ctx context.Context, a, b string) (bool, error) {
	_, idx, err := getManifest(ctx)
	if err != nil {
		return false, err
	}
	return newerOrEqualIdx(idx, a, b), nil
}

// KnownVersion 官方清单里有没有这个版本号。接管已有服务器时版本是从文件里认出来或用户填的，
// 不一定在清单里（NewerOrEqual 会把不认识的版本当成较旧，不能拿它判断）。
func KnownVersion(ctx context.Context, id string) (bool, error) {
	_, idx, err := getManifest(ctx)
	if err != nil {
		return false, err
	}
	_, ok := idx[id]
	return ok, nil
}

// NewerOrEqualCached 同 NewerOrEqual，但只用本地已有的清单（本次运行已加载的，否则读磁盘缓存）、绝不联网，
// 供保存设置等须即时返回的路径——联网刷新最坏要等约两分钟，而历史版本的先后顺序不会变，旧清单足以判定。
// 无本地清单或任一版本不在其中时 ok=false（不像 NewerOrEqual 那样把未知版本当作较旧），由调用方兜底。
func NewerOrEqualCached(a, b string) (newer, ok bool) {
	var idx map[string]int
	if p := maniSnap.Load(); p != nil {
		idx = *p
	} else {
		var m manifest
		cb, err := os.ReadFile(manifestCachePath())
		if err != nil || json.Unmarshal(cb, &m) != nil {
			return false, false
		}
		idx = indexVersions(&m)
		maniSnap.CompareAndSwap(nil, &idx) // 仅省去后续调用重复解析；不碰 maniData，联网刷新节奏不受影响
	}
	_, oka := idx[a]
	_, okb := idx[b]
	if !oka || !okb {
		return false, false
	}
	return newerOrEqualIdx(idx, a, b), true
}

// vanillaVersions 返回不老于 FloorVersion 的版本（正式版，可选快照），附官方发布时间。
func vanillaVersions(ctx context.Context, snapshots bool) ([]MCVersion, error) {
	m, idx, err := getManifest(ctx)
	if err != nil {
		return nil, err
	}
	floor, hasFloor := idx[FloorVersion]
	var out []MCVersion
	for i, v := range m.Versions {
		if hasFloor && i > floor {
			break // 清单新版本在前，之后全是更老的版本
		}
		if v.Type == "release" || (snapshots && v.Type == "snapshot") {
			out = append(out, MCVersion{
				ID:          v.ID,
				Type:        v.Type,
				Latest:      v.ID == m.Latest.Release || v.ID == m.Latest.Snapshot,
				ReleaseTime: v.ReleaseTime,
			})
		}
	}
	return out, nil
}

// orderAndFloor 将任意来源的版本号集合按官方清单顺序排序（新在前），
// 并过滤掉低于支持下限或清单中不存在的版本。
func orderAndFloor(ctx context.Context, ids []string) ([]string, error) {
	_, idx, err := getManifest(ctx)
	if err != nil {
		return nil, err
	}
	floor, hasFloor := idx[FloorVersion]
	type pair struct {
		id string
		i  int
	}
	var ps []pair
	seen := map[string]bool{}
	for _, id := range ids {
		i, ok := idx[id]
		if !ok || seen[id] {
			continue
		}
		if hasFloor && i > floor {
			continue
		}
		seen[id] = true
		ps = append(ps, pair{id, i})
	}
	sort.Slice(ps, func(a, b int) bool { return ps[a].i < ps[b].i })
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.id
	}
	return out, nil
}

// VersionMeta 单个 MC 版本的关键元数据。
type VersionMeta struct {
	ID         string
	JavaMajor  int // 来自官方 javaVersion.majorVersion 字段（权威）
	ServerURL  string
	ServerSHA1 string
}

// GetVersionMeta 获取指定版本的 Java 需求与官方服务端下载信息。
func GetVersionMeta(ctx context.Context, id string) (*VersionMeta, error) {
	m, idx, err := getManifest(ctx)
	if err != nil {
		return nil, err
	}
	i, ok := idx[id]
	if !ok {
		return nil, fmt.Errorf("未知的 Minecraft 版本: %s", id)
	}
	entry := m.Versions[i]
	var vj struct {
		JavaVersion struct {
			MajorVersion int `json:"majorVersion"`
		} `json:"javaVersion"`
		Downloads struct {
			Server struct {
				SHA1 string `json:"sha1"`
				URL  string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := dl.FetchJSON(ctx, URLPair(entry.URL), &vj); err != nil {
		return nil, fmt.Errorf("获取版本 %s 元数据失败: %w", id, err)
	}
	major := vj.JavaVersion.MajorVersion
	if major == 0 {
		major = staticJavaFor(idx, id)
	}
	if vj.Downloads.Server.URL == "" {
		return nil, fmt.Errorf("版本 %s 没有官方服务端", id)
	}
	return &VersionMeta{
		ID:         id,
		JavaMajor:  major,
		ServerURL:  vj.Downloads.Server.URL,
		ServerSHA1: vj.Downloads.Server.SHA1,
	}, nil
}

// staticJavaFor 静态兜底映射（仅在官方元数据缺失 javaVersion 字段时使用）。
func staticJavaFor(idx map[string]int, id string) int {
	after := func(b string) bool { return newerOrEqualIdx(idx, id, b) }
	switch {
	case after("26.1"):
		return 25
	case after("1.20.5"):
		return 21
	case after("1.18"):
		return 17
	case after("1.17"):
		return 16
	default:
		return 8
	}
}

// VanillaServerArtifact 原版服务端下载描述。
func VanillaServerArtifact(meta *VersionMeta) Artifact {
	return Artifact{
		URLs:     URLPair(meta.ServerURL),
		SHA1:     meta.ServerSHA1,
		FileName: "server.jar",
		MinSize:  1 << 20,
	}
}

// LatestRelease 返回当前最新正式版号。
func LatestRelease(ctx context.Context) (string, error) {
	m, _, err := getManifest(ctx)
	if err != nil {
		return "", err
	}
	return m.Latest.Release, nil
}
