// Package modpack 解析整合包（Modrinth .mrpack 与 CurseForge zip）并解析服务端文件下载清单。
package modpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
)

// File 一个待下载的整合包文件。
type File struct {
	Path string // 相对实例目录（如 mods/xxx.jar）
	SHA1 string
	URLs []string
	Size int64
}

// CFRef CurseForge 文件引用（需 API 解析成直链）。
type CFRef struct {
	ProjectID int64
	FileID    int64
	Required  bool
}

type Pack struct {
	Format       string // mrpack | curseforge
	Name         string
	MC           string
	Core         mcsrc.Core
	Loader       string // 加载器/核心构建版本
	Files        []File
	Pending      []PendingFile // 解析时就无法自动下载的文件，逐项交给用户处置（不再整包拒绝）
	CFRefs       []CFRef
	ZipPath      string
	OverrideDirs []string // 依序应用
}

// Parse 识别并解析整合包 zip。
func Parse(zipPath string) (*Pack, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开整合包（需为 .mrpack 或 zip）: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		switch f.Name {
		case "modrinth.index.json":
			return parseMrpack(zipPath, f)
		case "manifest.json":
			return parseCurseForge(zipPath, f)
		}
	}
	return nil, fmt.Errorf("无法识别整合包格式：缺少 modrinth.index.json 或 manifest.json")
}

func readZipFile(f *zip.File, out any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// safeRelPath 校验整合包内文件路径，防目录穿越。
func safeRelPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) ||
		strings.Contains(clean, ":") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("整合包内含非法路径: %s", p)
	}
	return clean, nil
}

// ---------- Modrinth .mrpack ----------

func parseMrpack(zipPath string, f *zip.File) (*Pack, error) {
	var idx struct {
		FormatVersion int               `json:"formatVersion"`
		Name          string            `json:"name"`
		Dependencies  map[string]string `json:"dependencies"`
		Files         []struct {
			Path   string `json:"path"`
			Hashes struct {
				SHA1 string `json:"sha1"`
			} `json:"hashes"`
			Env *struct {
				Server string `json:"server"`
			} `json:"env"`
			Downloads []string `json:"downloads"`
			FileSize  int64    `json:"fileSize"`
		} `json:"files"`
	}
	if err := readZipFile(f, &idx); err != nil {
		return nil, fmt.Errorf("解析 modrinth.index.json 失败: %w", err)
	}
	p := &Pack{Format: "mrpack", Name: idx.Name, ZipPath: zipPath,
		OverrideDirs: []string{"overrides", "server-overrides"}}
	p.MC = idx.Dependencies["minecraft"]
	if p.MC == "" {
		return nil, fmt.Errorf("整合包未声明 Minecraft 版本")
	}
	switch {
	case idx.Dependencies["fabric-loader"] != "":
		p.Core, p.Loader = mcsrc.CoreFabric, idx.Dependencies["fabric-loader"]
	case idx.Dependencies["neoforge"] != "":
		p.Core, p.Loader = mcsrc.CoreNeoForge, idx.Dependencies["neoforge"]
	case idx.Dependencies["forge"] != "":
		p.Core, p.Loader = mcsrc.CoreForge, idx.Dependencies["forge"]
	case idx.Dependencies["quilt-loader"] != "":
		return nil, fmt.Errorf("暂不支持 Quilt 加载器的整合包")
	default:
		p.Core = mcsrc.CoreVanilla
	}
	for _, fl := range idx.Files {
		if fl.Env != nil && fl.Env.Server == "unsupported" {
			continue // 仅客户端文件（材质、光影等）
		}
		rel, err := safeRelPath(fl.Path)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			continue
		}
		// 逐文件分类：能自动下载的进 Files，不能的进 Pending（不再因为一个文件拒绝整个整合包）。
		// 分类规则（顺序重要）：缺 SHA-1 的文件任何处置都不能下载（下载下来没法核对，信任站点也一样）；
		// 有 SHA-1 且有白名单地址才能自动下载；只有自定义 https 地址的留待用户信任站点后下载。
		allowed, pendingURLs := classifyURLs(fl.Downloads, nil)
		switch {
		case len(allowed) > 0 && fl.Hashes.SHA1 == "":
			// 有可下载地址但没有校验值：下载下来没法核对，任何处置都不自动下载（保留地址没有意义）
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonNoHash})
		case len(allowed) > 0:
			p.Files = append(p.Files, File{Path: rel, SHA1: fl.Hashes.SHA1, URLs: allowed, Size: fl.FileSize})
		case len(pendingURLs) > 0 && fl.Hashes.SHA1 == "":
			// 自定义站点的直链、但缺校验值：trusted 下载也过不了校验，同上不给下载资格
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonNoHash})
		case len(pendingURLs) > 0:
			// https 的自定义主机：交给用户确认（可选「信任这个站点」）后再下载；SHA-1 带上，trust 下载必须过校验
			p.Pending = append(p.Pending, PendingFile{Path: rel, SHA1: fl.Hashes.SHA1, URLs: pendingURLs, Size: fl.FileSize, Reason: ReasonHost})
		default:
			// 一条能用的地址都没有（非 https 或没写地址）：只能保留本地文件 / 手动指定 / 跳过
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonBadURL})
		}
	}
	return p, nil
}

// ---------- CurseForge ----------

func parseCurseForge(zipPath string, f *zip.File) (*Pack, error) {
	var man struct {
		Name      string `json:"name"`
		Overrides string `json:"overrides"`
		Minecraft struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
		Files []struct {
			ProjectID int64 `json:"projectID"`
			FileID    int64 `json:"fileID"`
			Required  bool  `json:"required"`
		} `json:"files"`
	}
	if err := readZipFile(f, &man); err != nil {
		return nil, fmt.Errorf("解析 manifest.json 失败: %w", err)
	}
	p := &Pack{Format: "curseforge", Name: man.Name, ZipPath: zipPath, MC: man.Minecraft.Version}
	ov := man.Overrides
	if ov == "" {
		ov = "overrides"
	}
	p.OverrideDirs = []string{ov}
	loaderID := ""
	for _, l := range man.Minecraft.ModLoaders {
		if l.Primary || loaderID == "" {
			loaderID = l.ID
		}
	}
	kind, ver, _ := strings.Cut(loaderID, "-")
	switch kind {
	case "forge":
		p.Core, p.Loader = mcsrc.CoreForge, ver
	case "neoforge":
		p.Core, p.Loader = mcsrc.CoreNeoForge, ver
	case "fabric":
		p.Core, p.Loader = mcsrc.CoreFabric, ver
	case "quilt":
		return nil, fmt.Errorf("暂不支持 Quilt 加载器的整合包")
	default:
		return nil, fmt.Errorf("无法识别整合包加载器: %s", loaderID)
	}
	for _, fl := range man.Files {
		p.CFRefs = append(p.CFRefs, CFRef{ProjectID: fl.ProjectID, FileID: fl.FileID, Required: fl.Required})
	}
	return p, nil
}

// ResolveCF 通过 CurseForge 官方 API（需用户 API key）把文件引用解析为直链。
// 返回可下载文件与未能解析的引用（作者禁止分发或 API 失败）。
func ResolveCF(ctx context.Context, refs []CFRef, apiKey string) (files []File, unresolved []CFRef, err error) {
	if apiKey == "" {
		return nil, refs, fmt.Errorf("未配置 CurseForge API Key（可在全局设置填写，或改用 Modrinth 格式整合包）")
	}
	ids := make([]int64, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.FileID)
	}
	body, _ := json.Marshal(map[string]any{"fileIds": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.curseforge.com/v1/mods/files", strings.NewReader(string(body)))
	if err != nil {
		return nil, refs, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", dl.UA)
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := dl.Client.Do(req.WithContext(cctx))
	if err != nil {
		return nil, refs, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, refs, fmt.Errorf("CurseForge API HTTP %d（请检查 API Key）", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID          int64  `json:"id"`
			FileName    string `json:"fileName"`
			DownloadURL string `json:"downloadUrl"`
			FileLength  int64  `json:"fileLength"`
			Hashes      []struct {
				Value string `json:"value"`
				Algo  int    `json:"algo"` // 1=SHA1 2=MD5
			} `json:"hashes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, refs, err
	}
	byID := map[int64]int{}
	for i, d := range out.Data {
		byID[d.ID] = i
	}
	for _, r := range refs {
		i, ok := byID[r.FileID]
		if !ok || out.Data[i].DownloadURL == "" {
			unresolved = append(unresolved, r)
			continue
		}
		d := out.Data[i]
		sha1 := ""
		for _, h := range d.Hashes {
			if h.Algo == 1 {
				sha1 = h.Value
			}
		}
		rel, err := safeRelPath("mods/" + d.FileName)
		if err != nil {
			unresolved = append(unresolved, r)
			continue
		}
		files = append(files, File{Path: rel, SHA1: sha1, URLs: []string{d.DownloadURL}, Size: d.FileLength})
	}
	return files, unresolved, nil
}

// ExtractOverrides 将整合包内的覆盖目录依序解压到实例目录。
func ExtractOverrides(p *Pack, destDir string, writeFile func(rel string, r io.Reader) error) error {
	zr, err := zip.OpenReader(p.ZipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, prefix := range p.OverrideDirs {
		for _, f := range zr.File {
			name := strings.ReplaceAll(f.Name, "\\", "/")
			if !strings.HasPrefix(name, prefix+"/") || f.FileInfo().IsDir() {
				continue
			}
			rel, err := safeRelPath(strings.TrimPrefix(name, prefix+"/"))
			if err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			werr := writeFile(rel, rc)
			rc.Close()
			if werr != nil {
				return werr
			}
		}
	}
	return nil
}
