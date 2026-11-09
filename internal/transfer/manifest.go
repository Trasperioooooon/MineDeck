package transfer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"minedeck/internal/modinfo"
)

// Manifest 包内的 mdpack.json：打包方与收包方共享的契约（字段见包注释）。
type Manifest struct {
	FormatVersion   int             `json:"formatVersion"`
	Mode            string          `json:"mode"`
	ExportedAt      time.Time       `json:"exportedAt"`
	ExporterVersion string          `json:"exporterVersion"` // 导出方 MineDeck 的版本号
	Instance        json.RawMessage `json:"instance"`        // instance.json 的原文
	Mods            []modinfo.Mod   `json:"mods"`            // 模组清单（导入端不需要可以只看条数）
	WorldSize       int64           `json:"worldSize"`       // 世界存档的总大小（字节）
	Excluded        []string        `json:"excluded"`        // 实际剔除的条目
}

// InstanceBrief 包里 instance.json 的摘要，向导清点屏展示用。
type InstanceBrief struct {
	Name      string `json:"name"`
	Core      string `json:"core"`
	MC        string `json:"mc"`
	Build     string `json:"build,omitempty"`
	JavaMajor int    `json:"javaMajor"`
	XmxMB     int    `json:"xmxMb,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// Summary 不解压地清点一个 .mdpack：接收向导第一屏要给用户看的全部事实。
type Summary struct {
	FormatVersion int           `json:"formatVersion"`
	Mode          string        `json:"mode"`
	ExportedAt    time.Time     `json:"exportedAt"`
	Exporter      string        `json:"exporter"` // 导出方的 MineDeck 版本
	Instance      InstanceBrief `json:"instance"`
	Port          int           `json:"port"`       // 包内 server.properties 的 server-port；读不到按 25565
	EULAAgreed    bool          `json:"eulaAgreed"` // 包内 eula.txt 已经同意过
	ModCount      int           `json:"modCount"`
	Mods          []modinfo.Mod `json:"mods"`
	WorldSize     int64         `json:"worldSize"` // 字节
	Excluded      []string      `json:"excluded"`
	Entries       int           `json:"entries"`  // 包里的文件条目数
	PackSize      int64         `json:"packSize"` // 包文件本身的大小，字节
}

// errFormat 包本身打不开、或不是 MineDeck 认识的 .mdpack。
var errFormat = errors.New("这不是有效的 .mdpack")

// Inspect 只读地清点一个 .mdpack：解析并校验 manifest，顺带从包里读 server.properties（端口）
// 和 eula.txt（同意与否）。不产生任何写盘。文件不存在时错误里写明。
func Inspect(path string) (*Summary, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("找不到包文件：%s", path)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, errFormat
	}
	defer zr.Close()
	mf := -1
	var props, eula *zip.File
	for i, f := range zr.File {
		switch strings.ToLower(f.Name) {
		case ManifestFile:
			mf = i
		case "server.properties":
			props = f
		case "eula.txt":
			eula = f
		}
	}
	if mf < 0 {
		return nil, fmt.Errorf("%w：包里没有 %s", errFormat, ManifestFile)
	}
	rc, err := zr.File[mf].Open()
	if err != nil {
		return nil, errFormat
	}
	b, err := io.ReadAll(io.LimitReader(rc, 4<<20)) // manifest 只是清单，不会大
	rc.Close()
	if err != nil {
		return nil, errFormat
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%w：manifest 解析不了", errFormat)
	}
	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("包是格式版本 %d 的 .mdpack，这个版本的 MineDeck 只认版本 %d（收包的人或服主先把 MineDeck 升到新版再试）", m.FormatVersion, FormatVersion)
	}
	if !ValidMode(m.Mode) {
		return nil, fmt.Errorf("%w：manifest 里的 mode 不认识（%q）", errFormat, m.Mode)
	}
	if len(m.Instance) == 0 {
		return nil, fmt.Errorf("%w：manifest 里没有 instance.json 原文", errFormat)
	}
	sum := &Summary{
		FormatVersion: m.FormatVersion, Mode: m.Mode, ExportedAt: m.ExportedAt, Exporter: m.ExporterVersion,
		Mods: m.Mods, WorldSize: m.WorldSize, Excluded: m.Excluded,
		PackSize: st.Size(),
	}
	sum.Mods = append([]modinfo.Mod{}, sum.Mods...) // nil → 空切片，前端不太用处理 null
	sum.Excluded = append([]string{}, sum.Excluded...)
	if sum.Mods == nil {
		sum.Mods = []modinfo.Mod{}
	}
	if sum.Excluded == nil {
		sum.Excluded = []string{}
	}
	sum.ModCount = len(sum.Mods)
	sum.Entries = len(zr.File) - 1 // 除掉 manifest 本身
	readBrief(&m, sum)
	if props != nil { // 端口：包里 server.properties 的 server-port（向导预填、检查占用用）
		sum.Port = 25565
		if b, err := readZipFile(props, 1<<20); err == nil {
			if v := propsValue(b, "server-port"); v != "" {
				if n := parseIntSafe(v); n > 0 {
					sum.Port = n
				}
			}
		}
	} else {
		sum.Port = 25565
	}
	if eula != nil {
		if b, err := readZipFile(eula, 64<<10); err == nil && eulaAgreed(b) {
			sum.EULAAgreed = true
		}
	}
	return sum, nil
}

// readBrief 从 manifest 的 instance.json 原文里解出摘要字段。原文解析不了时名字留空，
// 由调用方报「instance.json 读不出来」；这里不强求字段全（旧版导出的包字段可能更多）。
func readBrief(m *Manifest, sum *Summary) {
	var s struct {
		Name      string    `json:"name"`
		Core      string    `json:"core"`
		MC        string    `json:"mc"`
		Build     string    `json:"build"`
		JavaMajor int       `json:"javaMajor"`
		XmxMB     int       `json:"xmxMb"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if err := json.Unmarshal(m.Instance, &s); err != nil {
		return
	}
	sum.Instance = InstanceBrief{
		Name: s.Name, Core: s.Core, MC: s.MC, Build: s.Build,
		JavaMajor: s.JavaMajor, XmxMB: s.XmxMB,
	}
	if !s.CreatedAt.IsZero() {
		sum.Instance.CreatedAt = s.CreatedAt.Format("2006-01-02 15:04")
	}
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}
