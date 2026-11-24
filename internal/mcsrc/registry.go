package mcsrc

import (
	"context"
	"fmt"
)

// CoreKind 核心类别：决定创建流水线与 UI 表单的差异。
type CoreKind string

const (
	KindGame   CoreKind = "game"   // 普通游戏服（eula + server.properties）
	KindHybrid CoreKind = "hybrid" // Forge+Bukkit 混合服（流程同 game）
	KindProxy  CoreKind = "proxy"  // 群组代理端（无 eula/server.properties，Java 版本固定）
)

type CoreInfo struct {
	ID   Core     `json:"id"`
	Name string   `json:"name"`
	Tag  string   `json:"tag"`
	Desc string   `json:"desc"`
	Kind CoreKind `json:"kind"`
}

// coreImpl 一个核心的完整实现。Artifact 为空表示该核心走 install.go 中的专用流程
// （vanilla/fabric/forge/neoforge），否则走"下载 jar 即服务端"的通用流程。
type coreImpl struct {
	Info      CoreInfo
	FixedJava int // KindProxy 使用的固定 Java major；0 = 按 MC 版本元数据
	Versions  func(ctx context.Context, snapshots bool) ([]MCVersion, error)
	Builds    func(ctx context.Context, mc string) ([]BuildInfo, error)
	Artifact  func(ctx context.Context, mc, build string) (Artifact, error)
}

var (
	registry   []*coreImpl
	registryIx = map[Core]*coreImpl{}
)

func register(c *coreImpl) {
	registry = append(registry, c)
	registryIx[c.Info.ID] = c
}

func impl(c Core) (*coreImpl, error) {
	if i, ok := registryIx[c]; ok {
		return i, nil
	}
	return nil, fmt.Errorf("未知核心类型: %s", c)
}

const mcsrcFetchHint = "获取版本列表失败，请检查网络或在「设置 → 下载与网络」切换下载源"

// Cores 返回全部核心的元数据（UI 展示顺序）。
func Cores() []CoreInfo {
	out := make([]CoreInfo, 0, len(registry))
	for _, c := range registry {
		out = append(out, c.Info)
	}
	return out
}

// ValidCore 判断核心是否已注册。
func ValidCore(c Core) bool {
	_, ok := registryIx[c]
	return ok
}

// KindOf 返回核心类别（未知核心按 game 处理）。
func KindOf(c Core) CoreKind {
	if i, ok := registryIx[c]; ok {
		return i.Info.Kind
	}
	return KindGame
}

// FixedJavaOf 返回代理类核心的固定 Java major（非代理类返回 0）。
func FixedJavaOf(c Core) int {
	if i, ok := registryIx[c]; ok {
		return i.FixedJava
	}
	return 0
}

// GenericArtifact 返回通用 jar 型核心的下载描述；专用流程核心返回错误。
func GenericArtifact(ctx context.Context, c Core, mc, build string) (Artifact, error) {
	i, err := impl(c)
	if err != nil {
		return Artifact{}, err
	}
	if i.Artifact == nil {
		return Artifact{}, fmt.Errorf("核心 %s 不走通用下载流程", c)
	}
	return i.Artifact(ctx, mc, build)
}

// ListVersions 按核心列出可选 MC/核心版本。
func ListVersions(ctx context.Context, core Core, snapshots bool) ([]MCVersion, error) {
	i, err := impl(core)
	if err != nil {
		return nil, err
	}
	return i.Versions(ctx, snapshots)
}

// ListBuilds 按核心列出某版本的可选构建。
func ListBuilds(ctx context.Context, core Core, mc string) ([]BuildInfo, error) {
	i, err := impl(core)
	if err != nil {
		return nil, err
	}
	if i.Builds == nil {
		return []BuildInfo{}, nil
	}
	return i.Builds(ctx, mc)
}

func init() {
	// ---- 专用流程核心（install.go 内有各自的安装分支） ----
	register(&coreImpl{
		Info: CoreInfo{CoreVanilla, "Vanilla 原版", "纯净", "Mojang 官方服务端，原汁原味", KindGame},
		Versions: func(ctx context.Context, snapshots bool) ([]MCVersion, error) {
			return vanillaVersions(ctx, snapshots)
		},
	})
	register(&coreImpl{
		Info:     CoreInfo{CorePaper, "Paper", "插件", "最流行的高性能插件服务端", KindGame},
		Versions: wrapIDVersions(PaperVersions),
		Builds:   PaperBuilds,
		Artifact: func(ctx context.Context, mc, build string) (Artifact, error) {
			return PaperArtifact(ctx, mc, build)
		},
	})
	register(&coreImpl{
		Info:     CoreInfo{CorePurpur, "Purpur", "插件+", "Paper 分支，更多可调项与特性", KindGame},
		Versions: wrapIDVersions(PurpurVersions),
		Builds:   PurpurBuilds,
		Artifact: func(ctx context.Context, mc, build string) (Artifact, error) {
			return PurpurArtifact(ctx, mc, build)
		},
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreLeaves, "Leaves", "插件·仿原版", "Paper 分支，修复原版特性（刷铁轨等），国人维护", KindGame},
		Versions: wrapIDVersions(LeavesVersions),
		Builds:   LeavesBuilds,
		Artifact: LeavesArtifact,
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreFolia, "Folia", "插件·多线程", "Paper 官方多线程分支，适合大量玩家分散场景", KindGame},
		Versions: wrapIDVersions(FoliaVersions),
		Builds:   FoliaBuilds,
		Artifact: FoliaArtifact,
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreFabric, "Fabric", "模组·轻量", "轻量模组加载器，启动快、更新快", KindGame},
		Versions: wrapIDVersions(FabricMCVersions),
		Builds:   FabricLoaders,
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreNeoForge, "NeoForge", "模组·主流", "1.20.2+ 模组生态主流（Forge 的社区继任者）", KindGame},
		Versions: func(ctx context.Context, _ bool) ([]MCVersion, error) { return NeoMCVersions(ctx) },
		Builds: func(ctx context.Context, mc string) ([]BuildInfo, error) {
			b, _, err := NeoBuilds(ctx, mc)
			return b, err
		},
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreForge, "Forge", "模组·经典", "经典模组加载器，适合 1.20.1 及更早的整合包", KindGame},
		Versions: wrapIDVersions(ForgeMCVersions),
		Builds:   ForgeBuilds,
	})
	// ---- 混合服（Forge 模组 + Bukkit 插件同服） ----
	register(&coreImpl{
		Info:     CoreInfo{CoreMohist, "Mohist", "混合·Forge+插件", "Forge 模组与 Bukkit 插件共存，整合包服首选之一", KindHybrid},
		Versions: wrapIDVersions(MohistVersions),
		Builds:   MohistBuilds,
		Artifact: MohistArtifact,
	})
	register(&coreImpl{
		Info:     CoreInfo{CoreBanner, "Taiyitist（原 Banner）", "混合·Fabric+插件", "Fabric 模组与 Bukkit 插件共存（已改名 Taiyitist，下载仍用原 banner 构建）", KindHybrid},
		Versions: wrapIDVersions(BannerVersions),
		Builds:   BannerBuilds,
		Artifact: BannerArtifact,
	})
	// ---- 代理端（群组服入口，转发到后端多个服务器） ----
	register(&coreImpl{
		Info:      CoreInfo{CoreVelocity, "Velocity", "代理·主流", "PaperMC 出品的现代群组代理端，搭配多后端服使用", KindProxy},
		FixedJava: 21,
		Versions:  wrapIDVersions(VelocityVersions),
		Builds:    VelocityBuilds,
		Artifact:  VelocityArtifact,
	})
	register(&coreImpl{
		Info:      CoreInfo{CoreWaterfall, "Waterfall", "代理·经典", "BungeeCord 分支（已停更但仍可用），老群组服兼容", KindProxy},
		FixedJava: 17,
		Versions:  wrapIDVersions(WaterfallVersions),
		Builds:    WaterfallBuilds,
		Artifact:  WaterfallArtifact,
	})
}

// wrapIDVersions 将 []string 版本列表适配为 []MCVersion。
func wrapIDVersions(fn func(ctx context.Context) ([]string, error)) func(context.Context, bool) ([]MCVersion, error) {
	return func(ctx context.Context, _ bool) ([]MCVersion, error) {
		ids, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return wrapIDs(ctx, ids), nil
	}
}

func wrapIDs(ctx context.Context, ids []string) []MCVersion {
	latest, _ := LatestRelease(ctx)
	out := make([]MCVersion, 0, len(ids))
	for _, id := range ids {
		out = append(out, MCVersion{ID: id, Type: "release", Latest: id == latest})
	}
	return out
}
