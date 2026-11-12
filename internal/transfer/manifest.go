// Package transfer 实例交接包（.mdpack）：把一台不再想开的服务器完整地交给别人。
//
// 包格式是 zip：根目录一份 manifest.json，实例文件全部放在 instance/ 前缀下，
// 解包只认这个前缀，包里的路径落不到实例目录之外。manifest 里记有导出时的 instance.json 原文、
// 模组 / 插件清单（sha1 + 大小）、世界大小与剔除清单。
//
// mode 有两种："handover"（服主交接，A 开的服玩不下去了交给 B）和 "distributable"（发给朋友，让他照着开一台）。
// 两种包的内容和布局一样，只是对方导入时看到的说法不同。PR #8（feat-instance-distribute）原来打算另做一套，
// 写出来的包格式和这里不兼容，没有采用，它的几样功能并进了这一套（见交接说明）。
package transfer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
)

// FormatVersion manifest 的格式版本。只认自己会读的这一版；更大的版本交给升级以后的程序。
const FormatVersion = 1

// ModeHandover 交接：A 的服务器连同世界、配置、模组交给 B 续服。
const ModeHandover = "handover"

// ModeDistributable 发给朋友：把服务器打一份发给成员，让他照着开一台。包里的东西和交接一样，
// 区别只在给谁——接手的人在导入向导和日志里看到的是「发给朋友的一份」而不是「交接」。
// （这个模式原来是给 PR #8 留的占位；#8 另写了一套不兼容的包格式，没有采用，功能并到了这一套里。）
const ModeDistributable = "distributable"

// ValidMode mode 是不是认识的两种之一。
func ValidMode(mode string) bool { return mode == ModeHandover || mode == ModeDistributable }

// ModeName 模式的中文叫法（界面和任务日志用，不把英文枚举露给用户）。
func ModeName(mode string) string {
	if mode == ModeDistributable {
		return "发给朋友的一份"
	}
	return "服主交接"
}

// manifestName 包根下的清单文件名。
const manifestName = "manifest.json"

// PayloadPrefix 实例文件在包里的前缀。
const PayloadPrefix = "instance/"

// ModEntry 模组 / 插件清单的一项。id / name / version / loader 从 jar 的元数据读出；
// 读不出来的（魔改 jar、私服插件）这几项留空，但 file / sha1 / size 一定有——线上没有对应文件的，导入时按这个留着。
type ModEntry struct {
	File    string `json:"file"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Loader  string `json:"loader,omitempty"`
	SHA1    string `json:"sha1"`
	Size    int64  `json:"size"`
}

// Manifest 交接包的清单。
type Manifest struct {
	FormatVersion   int           `json:"formatVersion"`
	Mode            string        `json:"mode"`
	MineDeckVersion string        `json:"minedeckVersion"`
	ExportedAt      time.Time     `json:"exportedAt"`
	Instance        inst.Settings `json:"instance"`
	Core            CoreRef       `json:"core"`
	Mods            []ModEntry    `json:"mods"`
	Plugins         []ModEntry    `json:"plugins"`
	WorldBytes      int64         `json:"worldBytes"`
	Excluded        []string      `json:"excluded"`
	Full            bool          `json:"full"` // 完整模式：包含核心启动文件
	Counts          PackCounts    `json:"counts"`
}

// CoreRef 打包时认定的核心与版本（和 instance.json 里的相同字段一致，单列出好读）。
type CoreRef struct {
	Core  mcsrc.Core `json:"core"`
	MC    string     `json:"mc"`
	Build string     `json:"build"`
	Kind  string     `json:"kind"` // game | hybrid | proxy
}

// PackCounts 包内文件数与字节总数的合计。
type PackCounts struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// addMod 打包 mods / plugins 目录里的 jar 时由 writePack 调用，把清单项补齐（元数据在 pf.mod 里，sha1 打包时算的）。
// 读不出元数据的（魔改 jar、解析不了的插件）id / name / version / loader 留空——这样的文件线上多半没有对应，
// 导入时按文件原样保留。
func (m *Manifest) addMod(pf packFile, sha1 string) {
	e := ModEntry{File: filepath.Base(pf.rel), SHA1: sha1, Size: pf.size}
	if pf.mod != nil {
		e.ID, e.Name, e.Version, e.Loader = pf.mod.ID, pf.mod.Name, pf.mod.Version, pf.mod.Loader
	}
	switch firstSeg(pf.rel) {
	case "mods":
		m.Mods = append(m.Mods, e)
	case "plugins":
		m.Plugins = append(m.Plugins, e)
	}
}

// ReadManifest 只读包根下的 manifest.json（不解包别的东西——几十 MB 的包也要能秒出摘要）。
func ReadManifest(path string) (*Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("打不开这个交接包（%w）", err)
	}
	defer zr.Close()
	var mf *zip.File
	for _, f := range zr.File {
		if f.Name == manifestName {
			mf = f
			break
		}
	}
	if mf == nil {
		return nil, errors.New("交接包里没有 manifest.json，可能是普通 zip 或损坏的交接包")
	}
	if mf.UncompressedSize64 > 8<<20 {
		return nil, errors.New("manifest.json 大小异常，这可能不是 MineDeck 的交接包")
	}
	rc, err := mf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest.json 解析失败（%w）", err)
	}
	return &m, nil
}

// check 校验 manifest 是不是本功能能处理的包。
func (m *Manifest) check() error {
	if m.FormatVersion > FormatVersion {
		return fmt.Errorf("这是用更新版本的 MineDeck 导出的交接包（清单版本 %d，本程序支持 %d），请先升级 MineDeck", m.FormatVersion, FormatVersion)
	}
	if m.FormatVersion < 1 || m.Mode == "" {
		return errors.New("manifest.json 缺少版本或模式信息，这可能不是 MineDeck 的交接包")
	}
	if !ValidMode(m.Mode) {
		return fmt.Errorf("不认识的交接包类型：%s", m.Mode)
	}
	if !mcsrc.ValidCore(m.Core.Core) {
		return fmt.Errorf("交接包里的服务端类型 %q 不被这个版本支持", m.Core.Core)
	}
	if strings.TrimSpace(m.Instance.Name) == "" {
		return errors.New("manifest 里没有服务器信息")
	}
	// 版本号和构建号会拼进下载地址和本机的文件名：只认版本号该有的字符，路径分隔符、冒号、连着的两个点一律不要
	for what, v := range map[string]string{"Minecraft 版本": m.Core.MC, "构建版本": m.Core.Build} {
		if !versionLike(v) {
			return fmt.Errorf("交接包里的%s写法不对：%q", what, v)
		}
	}
	return nil
}

// versionLike 像不像一个版本号：字母、数字、点、横线、下划线、加号，中间可以有空格（老快照叫 "1.14 Pre-Release 5" 这样），
// 最长 64 个字符，不能有连着的两个点，不能以点或空格结尾。空串算（没写）。
// 2026-11-10 拿 12 种核心的全部版本号和抽查的约 1900 个构建号对过，都在这个范围里。
func versionLike(v string) bool {
	if len(v) > 64 || strings.Contains(v, "..") || v != strings.Trim(v, " .") {
		return false
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '_', r == '+', r == ' ':
		default:
			return false
		}
	}
	return true
}

// packPort 读包内 server.properties 里的端口（摘要和导入预填用），读不到返回 0。
func packPort(path string) int {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != PayloadPrefix+"server.properties" || f.UncompressedSize64 > 1<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return 0
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return 0
		}
		p := inst.ParseProps(b)
		if v, ok := p.Get("server-port"); ok {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// Summary 导入向导上给用户看的包摘要。
type Summary struct {
	Name       string `json:"name"`
	Core       string `json:"core"`
	MC         string `json:"mc"`
	Build      string `json:"build,omitempty"`
	Kind       string `json:"kind"`
	Mode       string `json:"mode"`
	Full       bool   `json:"full"`
	Port       int    `json:"port"` // 包内 server.properties 的端口（向导里预填，改了端口冲突时前端调 port.check 提示）
	WorldBytes int64  `json:"worldBytes"`
	Mods       int    `json:"mods"`
	Plugins    int    `json:"plugins"`
	XmxMB      int    `json:"xmxMb"`
	JavaMajor  int    `json:"javaMajor"`
	ExportedAt string `json:"exportedAt"`
	MineDeck   string `json:"minedeckVersion"`
	FileBytes  int64  `json:"fileBytes"`
}

// Summarize 校验并摘出包的关键信息（导入向导的「这个包里是什么」）。
func Summarize(path string) (*Summary, error) {
	m, err := ReadManifest(path)
	if err != nil {
		return nil, err
	}
	if err := m.check(); err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &Summary{
		Name: m.Instance.Name, Core: string(m.Core.Core), MC: m.Core.MC, Build: m.Core.Build, Kind: m.Core.Kind,
		Mode: m.Mode, Full: m.Full, Port: packPort(path),
		WorldBytes: m.WorldBytes, Mods: len(m.Mods), Plugins: len(m.Plugins),
		XmxMB: m.Instance.XmxMB, JavaMajor: m.Instance.JavaMajor,
		ExportedAt: m.ExportedAt.Local().Format("2006-01-02 15:04"),
		MineDeck:   m.MineDeckVersion, FileBytes: st.Size(),
	}, nil
}

// fileSafeName 把实例名转成可以当文件名用的形式（交接包自身的文件名）。
func fileSafeName(name string) string {
	repl := strings.NewReplacer(`\`, "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	return strings.TrimSpace(strings.Trim(repl.Replace(name), " ."))
}
