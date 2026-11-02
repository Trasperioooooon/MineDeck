// Package companionjar 是打进 MineDeck exe 的 AI 伙伴模组：companion/ 下两个加载器的工程可复现构建出的 jar，
// 和描述它们的 manifest.json（每个 jar 的加载器、MC 版本、模组版本、sha256，以及依赖的 PlayerEngine、
// Architectury API 的版本要求、Modrinth 项目 ID 和版本选择规则）。
//
// 这个目录只放 jar、清单和读取它们的代码。模组源码改了以后，用 companion/update-embedded.sh 重新构建、
// 把 jar 复制进来并改好清单；检查入库的是不是最新的用 update-embedded.sh --check。别的包只 import 这里，不改这里。
//
// jar 里只有我们自己的代码（MIT），不含 PlayerEngine、Architectury 的任何文件；那两个由 MineDeck 按清单从 Modrinth 下载。
package companionjar

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//go:embed manifest.json minedeck-companion-neoforge-1.21.1.jar minedeck-companion-forge-1.20.1.jar
var files embed.FS

// FilePrefix 本模组的 jar 装进 mods 时的文件名都以它开头（各版本、各加载器），安装时据此找出要替换的旧版本。
const FilePrefix = "minedeck-companion-"

// Jar.Loader 的取值。
const (
	LoaderNeoForge = "neoforge"
	LoaderForge    = "forge"
)

// Dependency.Side 的取值：依赖要装在哪一侧。
const (
	SideBoth   = "both"   // 服务端和每个玩家的客户端都要装
	SideServer = "server" // 只有服务端要装
)

// PolicyTestedFirst 版本选择规则（Modrinth.Policy）：
//  1. 先装 Tested 里那个版本（实测过的），下载后核对 Size 和 SHA512；
//  2. Tested 下载不到（比如作者删了这个版本）时，才去 Modrinth 上找：项目 ProjectID，加载器在 Loaders 里，
//     游戏版本在 GameVersions 里，version_type 是 release，version_number 匹配 VersionPattern，取发布时间最晚的一个；
//     下载后还要确认 jar 里 mods.toml 写的版本在 Dependency.VersionRange 之内，不在就不装。
const PolicyTestedFirst = "tested-first"

// Manifest manifest.json 的内容。
type Manifest struct {
	Schema int     `json:"schema"` // 清单格式的版本，现在是 1
	Mod    ModInfo `json:"mod"`
	Jars   []Jar   `json:"jars"`
}

// ModInfo 本模组。
type ModInfo struct {
	ID       string `json:"id"`       // 模组 ID：minedeck_companion
	Name     string `json:"name"`     // 显示名
	Version  string `json:"version"`  // 模组版本（两个加载器的 jar 相同）
	License  string `json:"license"`  // MIT
	Protocol int    `json:"protocol"` // 控制连接的协议版本（companion/README.md 的「协议 v1」）
}

// Jar 一个加载器、一个 MC 版本对应的 jar。
type Jar struct {
	Loader         string       `json:"loader"`         // LoaderNeoForge / LoaderForge
	Minecraft      string       `json:"minecraft"`      // MC 版本，如 "1.21.1"（只支持这一个版本）
	LoaderVersions string       `json:"loaderVersions"` // 加载器版本要求，mods.toml 的写法，如 "[21.1,)"
	TestedLoader   string       `json:"testedLoader"`   // 实测过的加载器版本
	Java           int          `json:"java"`           // 运行服务端要的 Java 大版本
	File           string       `json:"file"`           // 内置的文件名
	InstallName    string       `json:"installName"`    // 装进 mods 时用的文件名（带版本号，以 FilePrefix 开头）
	Size           int64        `json:"size"`           // 字节数
	SHA256         string       `json:"sha256"`         // 小写十六进制
	Client         string       `json:"client"`         // 玩家的客户端要不要装本模组："required"
	Dependencies   []Dependency `json:"dependencies"`   // 要另外装的模组（PlayerEngine、Architectury API）
}

// Dependency 本模组依赖、但不打包的模组。
type Dependency struct {
	ModID        string   `json:"modId"`        // 模组 ID：playerengine / architectury
	Name         string   `json:"name"`         // 显示名
	VersionRange string   `json:"versionRange"` // 本模组 mods.toml 里写的版本范围（Maven 写法），不在范围内加载器拒绝启动
	Side         string   `json:"side"`         // SideBoth / SideServer
	License      string   `json:"license"`      // 它的许可证
	Modrinth     Modrinth `json:"modrinth"`     // 去哪下、下哪个版本
}

// Modrinth 依赖在 Modrinth 上的位置和版本选择规则。
type Modrinth struct {
	ProjectID      string   `json:"projectId"`      // 项目 ID（不会变；slug 可能改）
	Slug           string   `json:"slug"`           // 项目的 slug，拼网页链接用
	Loaders        []string `json:"loaders"`        // 查版本时的 loaders 条件
	GameVersions   []string `json:"gameVersions"`   // 查版本时的 game_versions 条件
	VersionPattern string   `json:"versionPattern"` // 正则，匹配 Modrinth 版本的 version_number
	Policy         string   `json:"policy"`         // 版本选择规则，现在只有 PolicyTestedFirst
	Tested         Pinned   `json:"tested"`         // 实测过的那个版本
}

// Pinned Modrinth 上一个具体的版本和它的主文件。
type Pinned struct {
	VersionID     string `json:"versionId"`     // Modrinth 的版本 ID
	VersionNumber string `json:"versionNumber"` // Modrinth 的 version_number
	ModVersion    string `json:"modVersion"`    // 这个 jar 的 mods.toml 里写的版本
	File          string `json:"file"`          // 文件名
	Size          int64  `json:"size"`          // 字节数
	SHA1          string `json:"sha1"`          // 小写十六进制
	SHA512        string `json:"sha512"`        // 小写十六进制，下载后用它核对
	URL           string `json:"url"`           // 下载地址（Modrinth 的 CDN）
}

// RawManifest 内置的 manifest.json 原文。
func RawManifest() []byte {
	b, err := files.ReadFile("manifest.json")
	if err != nil {
		panic(err) // go:embed 了这个文件，读不到只能是编译出了问题
	}
	return b
}

// Load 读出并检查内置的清单：格式对，每个 jar 都在、大小和 sha256 都对得上（exe 被改坏了会报错）。
func Load() (*Manifest, error) {
	return parse(RawManifest(), files.ReadFile)
}

// Find 按加载器和 MC 版本找 jar；没有对应的 jar（不支持这个组合）时 ok 为 false。
func (m *Manifest) Find(loader, minecraft string) (j Jar, ok bool) {
	for _, j := range m.Jars {
		if j.Loader == loader && j.Minecraft == minecraft {
			return j, true
		}
	}
	return Jar{}, false
}

// Bytes jar 的内容，校验过大小和 sha256。
func (j Jar) Bytes() ([]byte, error) {
	return j.read(files.ReadFile)
}

func (j Jar) read(readFile func(string) ([]byte, error)) ([]byte, error) {
	b, err := readFile(j.File)
	if err != nil {
		return nil, fmt.Errorf("内置的伙伴模组 %s 读不出来：%w", j.File, err)
	}
	if int64(len(b)) != j.Size || sha256Hex(b) != j.SHA256 {
		return nil, fmt.Errorf("内置的伙伴模组 %s 校验失败（大小或 sha256 对不上），这个 MineDeck 程序可能已损坏，请重新下载", j.File)
	}
	return b, nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

var (
	hexRe     = regexp.MustCompile(`^[0-9a-f]+$`)
	modIDRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	fileRe    = regexp.MustCompile(`^[A-Za-z0-9._+-]+\.jar$`)
	versionRe = regexp.MustCompile(`^[0-9A-Za-z.+-]+$`)
)

func isHex(s string, n int) bool { return len(s) == n && hexRe.MatchString(s) }

// parse 解析清单并逐项检查；readFile 读内置文件（测试时换成假的）。
func parse(raw []byte, readFile func(string) ([]byte, error)) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("伙伴模组的清单不是合法的 JSON：%w", err)
	}
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if m.Schema != 1 {
		bad("清单格式版本是 %d，只认 1", m.Schema)
	}
	if !modIDRe.MatchString(m.Mod.ID) || m.Mod.Name == "" || !versionRe.MatchString(m.Mod.Version) || m.Mod.License == "" {
		bad("清单的 mod 不完整：%+v", m.Mod)
	}
	if m.Mod.Protocol < 1 {
		bad("清单的协议版本不对：%d", m.Mod.Protocol)
	}
	if len(m.Jars) == 0 {
		bad("清单里没有 jar")
	}
	seen := map[string]bool{}
	for i, j := range m.Jars {
		where := fmt.Sprintf("jars[%d]（%s %s）", i, j.Loader, j.Minecraft)
		if j.Loader != LoaderNeoForge && j.Loader != LoaderForge {
			bad("%s：不认识的加载器 %q", where, j.Loader)
		}
		if j.Minecraft == "" || j.LoaderVersions == "" || j.TestedLoader == "" || j.Java < 8 || j.Client == "" {
			bad("%s：缺少 minecraft、loaderVersions、testedLoader、java 或 client", where)
		}
		key := j.Loader + " " + j.Minecraft
		if seen[key] {
			bad("%s：同一个加载器和 MC 版本出现了两次", where)
		}
		seen[key] = true
		if !fileRe.MatchString(j.File) || !fileRe.MatchString(j.InstallName) ||
			!strings.HasPrefix(j.InstallName, FilePrefix) || !strings.Contains(j.InstallName, "-"+m.Mod.Version+".jar") {
			bad("%s：文件名不对（file=%q installName=%q）", where, j.File, j.InstallName)
		}
		if !isHex(j.SHA256, 64) || j.Size <= 0 {
			bad("%s：sha256 或 size 不对", where)
		} else if _, err := j.read(readFile); err != nil {
			errs = append(errs, err)
		}
		if len(j.Dependencies) == 0 {
			bad("%s：没有列出依赖", where)
		}
		for _, d := range j.Dependencies {
			if err := checkDependency(d); err != nil {
				bad("%s 的依赖 %s：%v", where, d.ModID, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &m, nil
}

func checkDependency(d Dependency) error {
	r := d.Modrinth
	t := r.Tested
	switch {
	case !modIDRe.MatchString(d.ModID) || d.Name == "" || d.VersionRange == "" || d.License == "":
		return errors.New("缺少 modId、name、versionRange 或 license")
	case d.Side != SideBoth && d.Side != SideServer:
		return fmt.Errorf("side 只能是 %s 或 %s，现在是 %q", SideBoth, SideServer, d.Side)
	case r.ProjectID == "" || r.Slug == "" || len(r.Loaders) == 0 || len(r.GameVersions) == 0:
		return errors.New("modrinth 缺少 projectId、slug、loaders 或 gameVersions")
	case r.Policy != PolicyTestedFirst:
		return fmt.Errorf("不认识的版本选择规则 %q", r.Policy)
	case t.VersionID == "" || t.VersionNumber == "" || t.ModVersion == "" || !fileRe.MatchString(t.File) || t.Size <= 0:
		return errors.New("tested 不完整")
	case !isHex(t.SHA1, 40) || !isHex(t.SHA512, 128):
		return errors.New("tested 的 sha1 或 sha512 不对")
	case !strings.HasPrefix(t.URL, "https://cdn.modrinth.com/data/"+r.ProjectID+"/versions/"+t.VersionID+"/"):
		return fmt.Errorf("tested 的下载地址不是这个项目、这个版本的：%s", t.URL)
	}
	re, err := regexp.Compile(r.VersionPattern)
	if err != nil {
		return fmt.Errorf("versionPattern 不是合法的正则：%w", err)
	}
	if !re.MatchString(t.VersionNumber) {
		return fmt.Errorf("tested 的版本 %s 不符合 versionPattern %s", t.VersionNumber, r.VersionPattern)
	}
	return nil
}
