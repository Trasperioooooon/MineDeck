package toolbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modcart"
	"minedeck/internal/modinfo"
	"minedeck/internal/modpack"
	"minedeck/internal/winname"
)

// voiceBridgeJarPrefix MineDeck 音乐共享装进服务器的语音桥模组的文件名开头（同 internal/bridge 的 jarPrefix）。
const voiceBridgeJarPrefix = "minedeck-voicebridge"

const (
	maxMrpackFiles         = 4000
	maxMrpackOverrideBytes = 256 << 20
	maxSingleOverrideFile  = 64 << 20
)

// MrpackReq 预览或生成 .mrpack 整合包的请求。
type MrpackReq struct {
	Source        string `json:"source"` // instance | draft
	Inst          string `json:"inst"`   // source="instance" 必填
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Summary       string `json:"summary"`
	Core          string `json:"core"`          // source="draft" 时必填：fabric | forge | neoforge
	MC            string `json:"mc"`            // source="draft" 时必填：如 1.20.1
	LoaderVersion string `json:"loaderVersion"` // 可选；留空时尽量用实例或官方推荐构建
	IncludeConfig bool   `json:"includeConfig"` // 仅 instance 模式有效：把 config/ 打包进 overrides/config/
	WithDeps      *bool  `json:"withDeps"`      // draft 模式默认 true，自动带出 ClosureWith 算出的必装前置
	DestDir       string `json:"destDir"`       // 仅本机调用可指定导出文件夹
}

// MrpackItemView 预览或导出清单中的一条模组或覆盖文件。
type MrpackItemView struct {
	Title       string `json:"title"`
	FileName    string `json:"fileName"`
	PackPath    string `json:"packPath"`  // .mrpack 内部路径，如 mods/xxx.jar 或 overrides/mods/xxx.jar
	Mode        string `json:"mode"`      // download（走 Modrinth CDN 等白名单下载）| override（整包打进 overrides）| skipped
	ClientEnv   string `json:"clientEnv"` // required | optional | unsupported | ""
	ServerEnv   string `json:"serverEnv"` // required | optional | unsupported | ""
	FromDep     bool   `json:"fromDep,omitempty"`
	DepOf       string `json:"depOf,omitempty"`
	URL         string `json:"url,omitempty"`
	SHA1        string `json:"sha1,omitempty"`
	SHA512      string `json:"sha512,omitempty"`
	Size        int64  `json:"size"`
	Note        string `json:"note,omitempty"`
	srcDiskPath string // 仅内部生成时用，不输出到 JSON
}

// MrpackPreviewResp 整合包导出的预览结果（列出下载项、本地 overrides、环境标记、前置提示与告警）。
type MrpackPreviewResp struct {
	Source        string            `json:"source"`
	Name          string            `json:"name"`
	VersionID     string            `json:"versionId"`
	Summary       string            `json:"summary,omitempty"`
	Core          string            `json:"core"` // fabric | forge | neoforge
	MC            string            `json:"mc"`
	LoaderKey     string            `json:"loaderKey"` // fabric-loader | forge | neoforge
	LoaderVersion string            `json:"loaderVersion"`
	Dependencies  map[string]string `json:"dependencies"`
	Files         []MrpackItemView  `json:"files"`
	Overrides     []MrpackItemView  `json:"overrides"`
	DownloadCount int               `json:"downloadCount"`
	OverrideCount int               `json:"overrideCount"`
	ClientOnlyCnt int               `json:"clientOnlyCount"`
	ConfigCount   int               `json:"configCount"`
	Warnings      []string          `json:"warnings"`
	MissingDeps   []string          `json:"missingDeps,omitempty"`
	Conflicts     []string          `json:"conflicts,omitempty"`
	Unresolved    []string          `json:"unresolved,omitempty"`
}

// MrpackBuildResp .mrpack 生成结果。
type MrpackBuildResp struct {
	DownloadID    string            `json:"downloadId"`
	FileName      string            `json:"fileName"`
	Size          int64             `json:"size"`
	SHA1          string            `json:"sha1"`
	SavedPath     string            `json:"savedPath,omitempty"`
	DownloadCount int               `json:"downloadCount"`
	OverrideCount int               `json:"overrideCount"`
	ClientOnlyCnt int               `json:"clientOnlyCount"`
	ConfigCount   int               `json:"configCount"`
	Dependencies  map[string]string `json:"dependencies"`
	Warnings      []string          `json:"warnings"`
}

// modrinthIndex Modrinth .mrpack 规范定义的 modrinth.index.json（https://docs.modrinth.com/modpacks/format_definition/）。
type modrinthIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Dependencies  map[string]string `json:"dependencies"`
	Files         []mrpackIndexFile `json:"files"`
}

type mrpackIndexFile struct {
	Path      string           `json:"path"`
	Hashes    mrpackFileHashes `json:"hashes"`
	Env       mrpackFileEnv    `json:"env"`
	Downloads []string         `json:"downloads"`
	FileSize  int64            `json:"fileSize"`
}

type mrpackFileHashes struct {
	SHA1   string `json:"sha1"`
	SHA512 string `json:"sha512"`
}

type mrpackFileEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// resolveLoaderBuild 查推荐的加载器版本号；留成包变量方便离线单元测试注入或兜底。
var resolveLoaderBuild = func(ctx context.Context, core mcsrc.Core, mc string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	builds, err := mcsrc.ListBuilds(cctx, core, mc)
	if err != nil {
		return "", err
	}
	for _, b := range builds {
		if b.Recommended && b.ID != "" {
			return cleanLoaderBuild(core, mc, b.ID), nil
		}
	}
	if len(builds) > 0 && builds[0].ID != "" {
		return cleanLoaderBuild(core, mc, builds[0].ID), nil
	}
	return "", fmt.Errorf("查不到 %s %s 的加载器版本", core, mc)
}

func mrpackCoreSpec(core mcsrc.Core) (exportCore mcsrc.Core, mrLoader string, modrinthLoader string, depKey string, note string, ok bool) {
	switch core {
	case mcsrc.CoreFabric:
		return mcsrc.CoreFabric, "fabric", "fabric", "fabric-loader", "", true
	case mcsrc.CoreForge:
		return mcsrc.CoreForge, "forge", "forge", "forge", "", true
	case mcsrc.CoreNeoForge:
		return mcsrc.CoreNeoForge, "neoforge", "neoforge", "neoforge", "", true
	case mcsrc.CoreMohist:
		return mcsrc.CoreForge, "forge", "forge", "forge", "这台是 Mohist（混端），整合包将按 Forge 模组端导出，仅含 mods 里的模组，不含服务端插件。", true
	case mcsrc.CoreBanner:
		return mcsrc.CoreFabric, "fabric", "fabric", "fabric-loader", "这台是 Banner（混端），整合包将按 Fabric 模组端导出，仅含 mods 里的模组，不含服务端插件。", true
	}
	return "", "", "", "", "", false
}

func cleanLoaderBuild(core mcsrc.Core, mc, build string) string {
	b := strings.TrimSpace(build)
	if core == mcsrc.CoreForge && mc != "" {
		b = strings.TrimPrefix(b, mc+"-")
	}
	return b
}

// sideEnv 把 Modrinth 项目的 client_side / server_side 换成 .mrpack 里 env 的写法；不知道的（unknown、空）按要装算。
func sideEnv(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "unsupported":
		return "unsupported"
	case "optional":
		return "optional"
	default:
		return "required"
	}
}

// PreviewMrpack 计算从实例或待装草稿导出的 .mrpack 预览清单，但不实际写入压缩包。
func (s *Service) PreviewMrpack(ctx context.Context, req MrpackReq) (MrpackPreviewResp, error) {
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		if strings.TrimSpace(req.Inst) != "" {
			source = "instance"
		} else {
			source = "draft"
		}
	}
	switch source {
	case "instance":
		return s.previewFromInstance(ctx, req)
	case "draft":
		return s.previewFromDraft(ctx, req)
	default:
		return MrpackPreviewResp{}, ErrBadRequest("导出方式只能选「已有服务器」或「工具箱草稿清单」")
	}
}

// BuildMrpack 校验并生成 .mrpack 压缩包（仅从当前实例或草稿清单打包，文件名为程序安全拼接）。
func (s *Service) BuildMrpack(ctx context.Context, req MrpackReq) (MrpackBuildResp, error) {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	prev, err := s.PreviewMrpack(ctx, req)
	if err != nil {
		return MrpackBuildResp{}, err
	}
	if prev.DownloadCount == 0 && prev.OverrideCount == 0 {
		return MrpackBuildResp{}, ErrBadRequest("没有可打包进去的模组或配置文件，请先在服务器里放好模组或往草稿清单挑几个模组")
	}
	// 如果选择了把结果存在数据目录里（仅限本机请求），先准备文件列表再一次性写入，不留半份包。
	idx := modrinthIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionID:     prev.VersionID,
		Name:          prev.Name,
		Summary:       prev.Summary,
		Dependencies:  prev.Dependencies,
		Files:         make([]mrpackIndexFile, 0, len(prev.Files)),
	}
	seenZipPath := map[string]bool{}
	for _, f := range prev.Files {
		lower := strings.ToLower(f.PackPath)
		if seenZipPath[lower] {
			continue
		}
		seenZipPath[lower] = true
		idx.Files = append(idx.Files, mrpackIndexFile{
			Path: f.PackPath,
			Hashes: mrpackFileHashes{
				SHA1:   f.SHA1,
				SHA512: f.SHA512,
			},
			Env: mrpackFileEnv{
				Client: f.ClientEnv,
				Server: f.ServerEnv,
			},
			Downloads: []string{f.URL},
			FileSize:  f.Size,
		})
	}
	idxBytes, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return MrpackBuildResp{}, fmt.Errorf("生成整合包索引失败：%w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipEntry(zw, "modrinth.index.json", idxBytes); err != nil {
		return MrpackBuildResp{}, err
	}

	var totalOverrideBytes int64
	writtenOverrides := 0
	for _, ov := range prev.Overrides {
		zipEntryPath := ov.PackPath
		if !strings.HasPrefix(zipEntryPath, "overrides/") || !winname.PathOK(strings.TrimPrefix(zipEntryPath, "overrides/")) {
			return MrpackBuildResp{}, ErrBadRequest("整合包内文件路径不安全：" + zipEntryPath)
		}
		lower := strings.ToLower(zipEntryPath)
		if seenZipPath[lower] {
			continue
		}
		seenZipPath[lower] = true
		data, err := readRegularLimited(ov.srcDiskPath, maxSingleOverrideFile)
		if err != nil {
			return MrpackBuildResp{}, ErrBadRequest(fmt.Sprintf("读取「%s」失败（%v）。预览以后它可能被移走、改大了或者正被别的程序占着，重新预览一次再生成", ov.PackPath, err))
		}
		totalOverrideBytes += int64(len(data))
		if totalOverrideBytes > maxMrpackOverrideBytes {
			return MrpackBuildResp{}, ErrBadRequest("要整包打进 overrides 的文件合计超过了 256 MB 上限")
		}
		if err := writeZipEntry(zw, zipEntryPath, data); err != nil {
			return MrpackBuildResp{}, err
		}
		writtenOverrides++
	}
	if err := zw.Close(); err != nil {
		return MrpackBuildResp{}, fmt.Errorf("压缩整合包失败：%w", err)
	}

	zipBytes := buf.Bytes()
	sum := sha1.Sum(zipBytes)
	sha1Hex := hex.EncodeToString(sum[:])

	stem := safeFileStem(prev.Name, "整合包")
	verStem := safeFileStem(prev.VersionID, "1.0.0")
	fileName := fmt.Sprintf("%s-%s-%s.mrpack", stem, verStem, time.Now().Format("20060102-150405"))

	savedPath := ""
	if strings.TrimSpace(req.DestDir) != "" {
		savedPath, err = copyToHostDir(req.DestDir, fileName, zipBytes)
		if err != nil {
			return MrpackBuildResp{}, err
		}
	}
	dlEntry, err := s.saveArtifact(fileName, zipBytes, sha1Hex)
	if err != nil {
		return MrpackBuildResp{}, err
	}
	return MrpackBuildResp{
		DownloadID:    dlEntry.ID,
		FileName:      fileName,
		Size:          dlEntry.Size,
		SHA1:          sha1Hex,
		SavedPath:     savedPath,
		DownloadCount: len(idx.Files),
		OverrideCount: writtenOverrides,
		ClientOnlyCnt: prev.ClientOnlyCnt,
		ConfigCount:   prev.ConfigCount,
		Dependencies:  prev.Dependencies,
		Warnings:      prev.Warnings,
	}, nil
}

func (s *Service) previewFromInstance(ctx context.Context, req MrpackReq) (MrpackPreviewResp, error) {
	if s.mgr == nil {
		return MrpackPreviewResp{}, ErrBadRequest("实例管理器还没准备好")
	}
	i, err := s.mgr.Get(strings.TrimSpace(req.Inst))
	if err != nil {
		return MrpackPreviewResp{}, ErrNotFound("找不到这台服务器：" + req.Inst)
	}
	exportCore, _, loaderName, depKey, coreNote, ok := mrpackCoreSpec(i.Core)
	if !ok {
		return MrpackPreviewResp{}, ErrBadRequest(fmt.Sprintf("这台服务器是「%s」核心，不是模组端；Modrinth 整合包（.mrpack）用于导出 Fabric / Forge / NeoForge 模组端（Mohist、Banner 也可导出其中的模组）", i.Core))
	}
	if i.MC == "" {
		return MrpackPreviewResp{}, ErrBadRequest("这台服务器没有记录 Minecraft 版本，导出不了整合包")
	}
	loaderVer := strings.TrimSpace(req.LoaderVersion)
	if loaderVer == "" && (i.Core == mcsrc.CoreFabric || i.Core == mcsrc.CoreForge || i.Core == mcsrc.CoreNeoForge) {
		loaderVer = cleanLoaderBuild(i.Core, i.MC, i.Build)
	}
	guessedLoader := false
	if loaderVer == "" {
		resolved, err := resolveLoaderBuild(ctx, exportCore, i.MC)
		if err != nil {
			return MrpackPreviewResp{}, ErrBadRequest(fmt.Sprintf("识别不到加载器版本，请在表单里手填一个 %s 加载器版本", loaderName))
		}
		loaderVer, guessedLoader = resolved, true
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = i.Name + " 整合包"
	}
	verID := strings.TrimSpace(req.VersionID)
	if verID == "" {
		verID = "1.0.0"
	}

	modsDir := filepath.Join(i.Dir, "mods")
	ents, err := os.ReadDir(modsDir)
	if err != nil && !os.IsNotExist(err) {
		return MrpackPreviewResp{}, fmt.Errorf("读取 mods 文件夹失败：%w", err)
	}

	type diskJar struct {
		name   string
		path   string
		sha1   string
		sha512 string
		size   int64
	}
	var jars []diskJar
	var sha1List []string
	var warnings []string
	if coreNote != "" {
		warnings = append(warnings, coreNote)
	}
	if guessedLoader {
		// 接管、导入来的服务器常常没记加载器版本，Mohist、Banner 记的是它们自己的构建号
		warnings = append(warnings, fmt.Sprintf("这台服务器没记下 %s 加载器的版本，包里按官方推荐的 %s 填，和服务器实际用的可能不一样；"+
			"朋友装好以后进不了服的话，先对一下加载器版本", loaderName, loaderVer))
	}

	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") || !strings.HasSuffix(strings.ToLower(n), ".jar") {
			continue
		}
		if !winname.OK(n) || inst.IsControlFile("mods/"+n) {
			warnings = append(warnings, fmt.Sprintf("跳过不符合文件名安全规则的文件：%s", n))
			continue
		}
		if strings.HasPrefix(strings.ToLower(n), voiceBridgeJarPrefix) {
			// MineDeck 自己的语音桥模组只在服务端用（音乐共享），客户端装了没用；AI 伙伴模组客户端要装，照常导出
			warnings = append(warnings, fmt.Sprintf("「%s」是 MineDeck 音乐共享用的服务端模组，朋友的客户端用不上，没有打进包", n))
			continue
		}
		p := filepath.Join(modsDir, n)
		lst, err := os.Lstat(p)
		if err != nil || !lst.Mode().IsRegular() {
			warnings = append(warnings, fmt.Sprintf("跳过不是普通文件的项目（例如符号链接）：%s", n))
			continue
		}
		h1, h512, sz, err := hashFileBoth(p)
		if err != nil {
			return MrpackPreviewResp{}, fmt.Errorf("校验 %s 失败：%w", n, err)
		}
		jars = append(jars, diskJar{name: n, path: p, sha1: h1, sha512: h512, size: sz})
		sha1List = append(sha1List, h1)
	}

	matches := map[string]modcart.SHA1Match{}
	if len(sha1List) > 0 && s.cart != nil {
		m, err := s.cart.LookupSHA1(ctx, sha1List)
		if err != nil {
			warnings = append(warnings, "连不上 Modrinth 核对已有模组，这几个本地 jar 将全部打进 overrides/mods（导出的包照样能直接玩，只是体积大一些）")
		} else {
			matches = m
		}
	}

	resp := MrpackPreviewResp{
		Source:        "instance",
		Name:          name,
		VersionID:     verID,
		Summary:       strings.TrimSpace(req.Summary),
		Core:          string(exportCore),
		MC:            i.MC,
		LoaderKey:     depKey,
		LoaderVersion: loaderVer,
		Dependencies: map[string]string{
			"minecraft": i.MC,
			depKey:      loaderVer,
		},
		Files:     []MrpackItemView{},
		Overrides: []MrpackItemView{},
		Warnings:  warnings,
	}

	seenRel := map[string]bool{}
	for _, j := range jars {
		m, found := matches[j.sha1]
		_, urlAllowed := modpack.AllowedURL(m.URL, nil)
		// 包里用服务器 mods 文件夹里的原名：同一个文件夹里的名字本来就不重复，在线下载项和 overrides 里的
		// 文件不会落到客户端的同一个位置互相盖掉（用 Modrinth 上的原名的话，可能和另一个本地文件撞名）
		fileName := j.name

		if found && urlAllowed && len(m.SHA1) == 40 && len(m.SHA512) == 128 {
			seenRel[strings.ToLower(fileName)] = true
			srvEnv := sideEnv(m.ServerSide)
			if srvEnv == "unsupported" {
				resp.ClientOnlyCnt++
			}
			title := m.Title
			if title == "" {
				title = fileName
			}
			resp.Files = append(resp.Files, MrpackItemView{
				Title:     title,
				FileName:  fileName,
				PackPath:  "mods/" + fileName,
				Mode:      "download",
				ClientEnv: sideEnv(m.ClientSide),
				ServerEnv: srvEnv,
				URL:       m.URL,
				SHA1:      m.SHA1,
				SHA512:    m.SHA512,
				Size:      j.size,
			})
			continue
		}

		note := "Modrinth 未收录（自定义、修改版或仅发布于其它平台），整包放进 overrides/mods"
		if found && !urlAllowed {
			note = "该版本下载地址不在 .mrpack 允许的白名单域名内（仅允许 cdn.modrinth.com / github / gitlab），改放进 overrides/mods"
		} else if found && len(m.SHA512) != 128 {
			note = "Modrinth 返回缺少 SHA-512 校验值，改放进 overrides/mods"
		}
		if j.size > maxSingleOverrideFile {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("「%s」有 %s，超过了单个文件 64 MB 的上限，又不能让朋友从 Modrinth 下载，没有打进包。"+
				"请把这个文件单独发给朋友", j.name, mib(j.size)))
			continue
		}
		meta := modinfo.Read(j.path)
		title := meta.Name
		if title == "" {
			title = j.name
		}
		resp.Overrides = append(resp.Overrides, MrpackItemView{
			Title:       title,
			FileName:    j.name,
			PackPath:    "overrides/mods/" + j.name,
			Mode:        "override",
			Size:        j.size,
			SHA1:        j.sha1,
			SHA512:      j.sha512,
			Note:        note,
			srcDiskPath: j.path,
		})
	}

	if req.IncludeConfig {
		cfgFiles, cfgWarns, err := collectSafeConfigOverrides(i.Dir)
		if err != nil {
			return MrpackPreviewResp{}, err
		}
		resp.Warnings = append(resp.Warnings, cfgWarns...)
		resp.ConfigCount = len(cfgFiles)
		resp.Overrides = append(resp.Overrides, cfgFiles...)
	}

	resp.DownloadCount = len(resp.Files)
	resp.OverrideCount = len(resp.Overrides)
	if len(resp.Overrides) > 0 {
		resp.Warnings = append(resp.Warnings, "提示：放进 overrides/mods 的本地模组会完整打进压缩包，分享给多人前请留意原模组作者的分发许可协议。")
	}
	if resp.DownloadCount+resp.OverrideCount > maxMrpackFiles {
		return MrpackPreviewResp{}, ErrBadRequest(fmt.Sprintf("要打包的文件总数超过上限（%d 个）", maxMrpackFiles))
	}
	if err := checkOverrideTotal(resp.Overrides); err != nil {
		return MrpackPreviewResp{}, err
	}
	return resp, nil
}

// checkOverrideTotal 要整个打进包的文件合计不能超过上限：预览时就说清楚，不等到生成才失败。
func checkOverrideTotal(ovs []MrpackItemView) error {
	var total int64
	for _, o := range ovs {
		total += o.Size
	}
	if total > maxMrpackOverrideBytes {
		return ErrBadRequest(fmt.Sprintf("要整个打进包的文件合计 %s，超过了 256 MB 的上限。Modrinth 上找不到的大模组请单独发给朋友，"+
			"或者不带 config 再试", mib(total)))
	}
	return nil
}

func mib(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

func (s *Service) previewFromDraft(ctx context.Context, req MrpackReq) (MrpackPreviewResp, error) {
	if s.cart == nil {
		return MrpackPreviewResp{}, ErrBadRequest("待装清单服务还没准备好")
	}
	draftCfg := s.GetMrpackDraft()
	core := strings.TrimSpace(req.Core)
	if core == "" {
		core = draftCfg.Core
	}
	mc := strings.TrimSpace(req.MC)
	if mc == "" {
		mc = draftCfg.MC
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = draftCfg.Name
	}
	verID := strings.TrimSpace(req.VersionID)
	if verID == "" {
		verID = draftCfg.VersionID
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = draftCfg.Summary
	}
	loaderVer := strings.TrimSpace(req.LoaderVersion)
	if loaderVer == "" {
		loaderVer = draftCfg.LoaderVersion
	}

	exportCore, modLoader, _, depKey, _, ok := mrpackCoreSpec(mcsrc.Core(strings.ToLower(core)))
	if !ok || exportCore == "" {
		return MrpackPreviewResp{}, ErrBadRequest("工具箱草稿整合包需要选 Fabric、Forge 或 NeoForge 加载器")
	}
	if _, _, _, verOK := parseMCVersion(mc); !verOK {
		return MrpackPreviewResp{}, ErrBadRequest("请填正确的 Minecraft 版本，例如 1.20.1")
	}
	if loaderVer == "" {
		resolved, err := resolveLoaderBuild(ctx, exportCore, mc)
		if err != nil {
			return MrpackPreviewResp{}, ErrBadRequest(fmt.Sprintf("取不到 %s %s 的推荐加载器版本，请手动填写一个加载器版本号", exportCore, mc))
		}
		loaderVer = resolved
	}

	items := s.cart.List(modcart.Toolbox)
	locals := s.cart.Locals(modcart.Toolbox)
	if len(items) == 0 && len(locals) == 0 {
		return MrpackPreviewResp{}, ErrBadRequest("工具箱的模组清单还是空的，先在下面挑几个模组或从电脑添加 jar 再导出")
	}

	plan, err := s.cart.ClosureWith(ctx, items, locals, modcart.Local{}, []string{modLoader}, mc)
	if err != nil {
		return MrpackPreviewResp{}, fmt.Errorf("计算模组版本和前置依赖失败：%w", err)
	}

	withDeps := req.WithDeps == nil || *req.WithDeps
	resp := MrpackPreviewResp{
		Source:        "draft",
		Name:          name,
		VersionID:     verID,
		Summary:       summary,
		Core:          string(exportCore),
		MC:            mc,
		LoaderKey:     depKey,
		LoaderVersion: loaderVer,
		Dependencies: map[string]string{
			"minecraft": mc,
			depKey:      loaderVer,
		},
		Files:     []MrpackItemView{},
		Overrides: []MrpackItemView{},
		Warnings:  []string{},
	}

	seenRel := map[string]bool{}
	for _, f := range plan.Files {
		if f.FromDep && !withDeps {
			continue
		}
		_, urlAllowed := modpack.AllowedURL(f.URL, nil)
		if !urlAllowed || len(f.SHA1) != 40 || len(f.SHA512) != 128 {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("「%s」的下载地址不在 .mrpack 允许的白名单域名内或缺少 SHA-512 校验，已跳过在线下载项", f.Title))
			continue
		}
		if !winname.OK(f.FileName) || !strings.HasSuffix(strings.ToLower(f.FileName), ".jar") || inst.IsControlFile("mods/"+f.FileName) {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("跳过文件名不安全的项目：%s", f.FileName))
			continue
		}
		if seenRel[strings.ToLower(f.FileName)] {
			continue
		}
		seenRel[strings.ToLower(f.FileName)] = true
		srvEnv := sideEnv(f.ServerSide)
		if srvEnv == "unsupported" {
			resp.ClientOnlyCnt++
		}
		resp.Files = append(resp.Files, MrpackItemView{
			Title:     f.Title,
			FileName:  f.FileName,
			PackPath:  "mods/" + f.FileName,
			Mode:      "download",
			ClientEnv: sideEnv(f.ClientSide),
			ServerEnv: srvEnv,
			FromDep:   f.FromDep,
			DepOf:     f.DependencyOf,
			URL:       f.URL,
			SHA1:      f.SHA1,
			SHA512:    f.SHA512,
			Size:      f.Size,
		})
	}

	// 本地暂存的 jar 试着按 SHA-1 在 Modrinth 认出来：认得到且在白名单就走在线下载，否则打进 overrides/mods
	var localHashes []string
	for _, lf := range locals {
		localHashes = append(localHashes, lf.SHA1)
	}
	localMatches := map[string]modcart.SHA1Match{}
	if len(localHashes) > 0 {
		if m, err := s.cart.LookupSHA1(ctx, localHashes); err == nil {
			localMatches = m
		}
	}
	for _, lf := range locals {
		diskPath, ok := s.cart.StagedPath(modcart.Toolbox, lf.SHA1)
		if !ok {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("本地暂存的「%s」已经找不到了，导出里没有这一项", lf.FileName))
			continue
		}
		if m, hit := localMatches[lf.SHA1]; hit {
			if _, ok := modpack.AllowedURL(m.URL, nil); ok && len(m.SHA1) == 40 && len(m.SHA512) == 128 &&
				winname.OK(lf.FileName) && !inst.IsControlFile("mods/"+lf.FileName) && !seenRel[strings.ToLower(lf.FileName)] {
				seenRel[strings.ToLower(lf.FileName)] = true
				srvEnv := sideEnv(m.ServerSide)
				if srvEnv == "unsupported" {
					resp.ClientOnlyCnt++
				}
				resp.Files = append(resp.Files, MrpackItemView{
					Title:     lf.Title,
					FileName:  lf.FileName,
					PackPath:  "mods/" + lf.FileName,
					Mode:      "download",
					ClientEnv: sideEnv(m.ClientSide),
					ServerEnv: srvEnv,
					URL:       m.URL,
					SHA1:      m.SHA1,
					SHA512:    m.SHA512,
					Size:      lf.Size,
				})
				continue
			}
		}
		if !winname.OK(lf.FileName) || inst.IsControlFile("mods/"+lf.FileName) {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("跳过文件名不安全的本地文件：%s", lf.FileName))
			continue
		}
		if seenRel[strings.ToLower(lf.FileName)] {
			// 和要从网上下载的某个文件同名：两份会落到客户端的同一个位置，后解的盖掉先下的
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("本地文件「%s」和要下载的一个文件同名，没有打进包；要用本地这份的话，先把清单里那一项拿掉", lf.FileName))
			continue
		}
		if lf.Size > maxSingleOverrideFile {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("「%s」有 %s，超过了单个文件 64 MB 的上限，没有打进包。请把这个文件单独发给朋友", lf.FileName, mib(lf.Size)))
			continue
		}
		seenRel[strings.ToLower(lf.FileName)] = true
		resp.Overrides = append(resp.Overrides, MrpackItemView{
			Title:       lf.Title,
			FileName:    lf.FileName,
			PackPath:    "overrides/mods/" + lf.FileName,
			Mode:        "override",
			Size:        lf.Size,
			SHA1:        lf.SHA1,
			Note:        "用户添加的本地 jar，整包放进 overrides/mods",
			srcDiskPath: diskPath,
		})
	}

	for _, u := range plan.Unresolved {
		resp.Unresolved = append(resp.Unresolved, u.Title+"："+u.Reason)
	}
	for _, dep := range plan.Missing {
		if !withDeps {
			resp.MissingDeps = append(resp.MissingDeps, fmt.Sprintf("%s（%s 需要）", dep.Project.Title, dep.DependencyOf))
		}
	}
	for _, m := range plan.Manual {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("本地模组「%s」声明需要前置「%s」，请确认已手工加入", m.File, m.ID))
	}
	resp.Conflicts = slices.Clone(plan.Conflicts)
	resp.DownloadCount = len(resp.Files)
	resp.OverrideCount = len(resp.Overrides)
	if len(resp.Overrides) > 0 {
		resp.Warnings = append(resp.Warnings, "提示：放进 overrides/mods 的本地模组会完整打进压缩包，分享给多人前请留意原模组作者的分发许可协议。")
	}
	if err := checkOverrideTotal(resp.Overrides); err != nil {
		return MrpackPreviewResp{}, err
	}
	return resp, nil
}

func collectSafeConfigOverrides(instDir string) ([]MrpackItemView, []string, error) {
	cfgRoot := filepath.Join(instDir, "config")
	lst, err := os.Lstat(cfgRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("检查 config 文件夹失败：%w", err)
	}
	if lst.Mode()&os.ModeSymlink != 0 || !lst.IsDir() {
		return nil, []string{"config 不是普通文件夹（可能是联接/符号链接），已跳过配置文件打包"}, nil
	}
	var out []MrpackItemView
	var warnings, secrets []string
	var totalBytes int64
	err = filepath.WalkDir(cfgRoot, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		relFS, err := filepath.Rel(cfgRoot, p)
		if err != nil {
			return nil
		}
		rel := path.Clean(filepath.ToSlash(relFS))
		if rel == "." || strings.HasPrefix(rel, "..") || inst.IsControlFile("config/"+rel) || !winname.PathOK("config/"+rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxSingleOverrideFile {
			warnings = append(warnings, fmt.Sprintf("配置文件太大已跳过（超过 64 MB）：config/%s", rel))
			return nil
		}
		if looksSecret(rel, p, info.Size()) {
			secrets = append(secrets, "config/"+rel)
			return nil
		}
		totalBytes += info.Size()
		if totalBytes > maxMrpackOverrideBytes || len(out) >= maxMrpackFiles {
			return ErrBadRequest("config 目录下的文件过多或总大小超过 256 MB，无法全部打包")
		}
		out = append(out, MrpackItemView{
			Title:       "config/" + rel,
			FileName:    path.Base(rel),
			PackPath:    "overrides/config/" + rel,
			Mode:        "override",
			Size:        info.Size(),
			Note:        "服务端 config 配置文件",
			srcDiskPath: p,
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(secrets) > 0 {
		list := secrets
		more := ""
		if len(list) > 8 {
			list, more = list[:8], fmt.Sprintf(" 等 %d 个", len(secrets))
		}
		warnings = append(warnings, fmt.Sprintf("这些配置文件看起来带着密码、令牌、密钥、数据库，或者是备份副本，没有打进包：%s%s。"+
			"它们是服主自己的，发给朋友的包里不该有；确实要的话，请自己检查过内容再单独发。"+
			"这只是尽量挑出来，打进包的配置文件都列在下面，发之前请看一眼", strings.Join(list, "、"), more))
	}
	return out, warnings, nil
}

// 配置文件里可能放着服主自己的密码、令牌、密钥、数据库：机器人令牌、Webhook、数据库连接、权限插件的存储设置之类。
// 导出给朋友的整合包不带这些。按文件名、扩展名和内容各认一遍，宁可多跳过几个（跳过的都写在提醒里）。
// 这只是尽量挑出来：认不出的写法总会有，界面上请人发之前看一眼预览里列出的配置文件。
var (
	secretNameWords = []string{"password", "passwd", "secret", "token", "credential", "apikey", "api_key", "api-key",
		"webhook", "discord", "mysql", "mariadb", "postgres", "jdbc", "luckperms", "oauth"}
	secretExts = map[string]bool{".db": true, ".sqlite": true, ".sqlite3": true, ".pem": true, ".key": true,
		".p12": true, ".pfx": true, ".jks": true, ".keystore": true, ".crt": true, ".cer": true, ".der": true, ".env": true, ".log": true}
	// 备份、临时、停用的副本：内容和正本一样，正本要是被挑出来了，副本也不该漏出去
	backupSuffixes = []string{".bak", ".old", ".orig", ".backup", ".tmp", ".disabled", "~"}
	// 键 = 值、"键": "值"、键: 值。键名里带这些词的都看；password 一类不管值多短、是不是纯数字都算
	secretKV = regexp.MustCompile(`(?i)([a-z0-9_.-]*?(?:password|passwd|pwd|passphrase|secret|token|api[_-]?key|webhook|private[_-]?key|access[_-]?key|license[_-]?key|auth[_-]?(?:key|token|code)|credential)[a-z0-9_.-]*)["']?\s*[:=]\s*["']?([^\s"',;}\]<>]+)`)
	// 单独一个 pass 当键名（不连着别的字母，免得认成 passive 之类）
	secretPass = regexp.MustCompile(`(?im)(?:^|[\s{,"'])pass["']?\s*[:=]\s*["']?([^\s"',;}\]<>]+)`)
	// XML：<password>…</password>、<botToken>…</botToken>
	secretXML = regexp.MustCompile(`(?i)<[a-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|webhook)[a-z0-9_.-]*(?:\s[^>]*)?>\s*[^<\s]`)
	// 连接串里带着口令：mongodb://用户:口令@主机、jdbc:mysql://用户:口令@主机
	secretURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s/:@"']+:[^\s/@"']+@`)
	// YAML 把值写在下一行：token:⏎  - xxxx
	secretYAMLNext = regexp.MustCompile(`(?im)^\s*[a-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|webhook)[a-z0-9_.-]*\s*:\s*\r?\n\s*-?\s*["']?[^\s"']{4,}`)
)

// maxSecretScan 内容最多读这么多来认；更大的配置文件认不全，干脆不打进包（列在提醒里）。
const maxSecretScan = 8 << 20

// looksSecret rel（相对 config/，正斜杠）这个配置文件是不是像放着密码、令牌、密钥的，或者是它们的备份副本。
func looksSecret(rel, diskPath string, size int64) bool {
	low := strings.ToLower(rel)
	base := path.Base(low)
	for _, w := range secretNameWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	if secretExts[path.Ext(base)] {
		return true
	}
	for _, suf := range backupSuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if size > maxSecretScan {
		return true
	}
	b, err := os.ReadFile(diskPath)
	if err != nil {
		return true // 读不了就别打进去：打进去的那一步也会读不了
	}
	return secretContent(b)
}

// secretContent 一份配置的内容里有没有像密码、令牌、密钥的东西（不看扩展名，什么文件都认一遍）。
func secretContent(b []byte) bool {
	for _, m := range secretKV.FindAllSubmatch(b, -1) {
		key, v := strings.ToLower(string(m[1])), strings.ToLower(string(m[2]))
		if strings.Contains(key, "passw") || strings.Contains(key, "pwd") || strings.Contains(key, "passphrase") {
			return true // 口令再短、全是数字也是口令
		}
		if len(v) < 6 || v == "true" || v == "false" || strings.Trim(v, "0123456789.-") == "" {
			continue
		}
		return true
	}
	return secretPass.Match(b) || secretXML.Match(b) || secretURL.Match(b) || secretYAMLNext.Match(b)
}

func hashFileBoth(path string) (sha1Hex string, sha512Hex string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", 0, err
	}
	defer f.Close()
	h1 := sha1.New()
	h512 := sha512.New()
	n, err := io.Copy(io.MultiWriter(h1, h512), f)
	if err != nil {
		return "", "", 0, err
	}
	return hex.EncodeToString(h1.Sum(nil)), hex.EncodeToString(h512.Sum(nil)), n, nil
}

func readRegularLimited(path string, maxBytes int64) ([]byte, error) {
	lst, err := os.Lstat(path)
	if err != nil || !lst.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readLimited(f, maxBytes, "单个文件超过大小限制")
}
