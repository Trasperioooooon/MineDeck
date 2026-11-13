// Package worldimport 地图导入：认出一份 Java 版世界，按目标服务器的核心和版本摆好，
// 换掉服务器正在用的默认世界（或在新建服务器时放进去），出错和事后都能回到原来的地图。
//
// 两个入口（新建服务器、服务器详情里的「导入并替换当前地图」）共用这里的识别、兼容检查、暂存、提交和恢复。
// 设计和取舍见 docs/地图导入-设计.md。
package worldimport

import "time"

// InputKind 来源里认出来的是什么东西。只有 KindJavaWorld 能导入，其余的说清它是什么、该去哪里。
type InputKind string

const (
	KindJavaWorld    InputKind = "java-world"    // 能导入的 Java 版世界（至少一个候选）
	KindDamagedWorld InputKind = "damaged-world" // 像世界但缺关键元数据（只有 level.dat_old、level.dat 读不出、只有 .mca）
	KindBedrock      InputKind = "bedrock"       // 基岩版世界（.mcworld，或 db/ + levelname.txt）
	KindStructure    InputKind = "structure"     // 建筑蓝图（.schem / .schematic / .litematic / 结构 .nbt）
	KindResourcePack InputKind = "resource-pack" // 资源包（pack.mcmeta + assets/）
	KindDatapack     InputKind = "datapack"      // 数据包（pack.mcmeta + data/）
	KindModpack      InputKind = "modpack"       // 整合包（CurseForge manifest.json、Modrinth .mrpack、MultiMC 等）
	KindServer       InputKind = "server"        // 完整服务端文件夹或压缩包（server.properties、核心 jar 等）
	KindMdpack       InputKind = "mdpack"        // MineDeck 交接包
	KindArchive      InputKind = "archive"       // 读不了的压缩包：RAR、7z、加密、分卷
	KindUnknown      InputKind = "unknown"       // 什么都没认出来
)

// Layout 世界的目录布局。
type Layout string

const (
	// LayoutLegacy 26.1 以前的原版布局：主世界在世界根，下界 DIM-1、末地 DIM1 在世界根下面。
	LayoutLegacy Layout = "legacy"
	// LayoutLegacyBukkit 26.1 以前的 Bukkit / Spigot / Paper 布局：<名>、<名>_nether/DIM-1、<名>_the_end/DIM1 三个目录。
	LayoutLegacyBukkit Layout = "legacy-bukkit"
	// LayoutModern 26.1 起的原版布局：dimensions/minecraft/{overworld,the_nether,the_end}，玩家在 players/。
	LayoutModern Layout = "modern"
	// LayoutModernPaper 26.1 起的 Paper 布局：同 LayoutModern，但五个共用数据文件在 overworld/data/minecraft/ 下。
	LayoutModernPaper Layout = "modern-paper"
)

// Severity 一条检查结果有多严重。
type Severity string

const (
	SevBlock   Severity = "block"   // 不能导入（或不能导入到这台服务器）
	SevConfirm Severity = "confirm" // 要用户明确确认才能继续（升级、版本不明、模组线索）
	SevWarn    Severity = "warn"    // 要看到的提醒（缺维度、单人进度接不上、资源包要另配）
	SevInfo    Severity = "info"    // 说明
)

// Issue 一条检查结果。Msg 是给玩家看的一句话（结论在前），Detail 是技术细节和原文，放进「详情」。
type Issue struct {
	Sev    Severity `json:"sev"`
	Code   string   `json:"code"`
	Msg    string   `json:"msg"`
	Detail string   `json:"detail,omitempty"`
}

// LevelMeta 从 level.dat 读到的东西。只读，缺的字段保持零值并由 Has* 标出，不补值。
type LevelMeta struct {
	HasVersion  bool   `json:"hasVersion"`            // 有 Data.Version（15w32a 起才有）
	VersionName string `json:"versionName,omitempty"` // Data.Version.Name，如 1.20.4、26.1
	VersionID   int32  `json:"versionId,omitempty"`   // Data.Version.Id
	DataVersion int32  `json:"dataVersion,omitempty"` // Data.DataVersion
	Snapshot    bool   `json:"snapshot,omitempty"`    // Data.Version.Snapshot
	Series      string `json:"series,omitempty"`      // Data.Version.Series（main 以外的是特殊分支）
	LegacyNBT   int32  `json:"legacyNbt,omitempty"`   // Data.version（19133 = Anvil），很老的存档只有这个

	LevelName    string   `json:"levelName,omitempty"` // Data.LevelName：地图作者起的名字（不可信文本）
	ServerBrands []string `json:"serverBrands,omitempty"`
	WasModded    bool     `json:"wasModded,omitempty"`

	GameType      int    `json:"gameType"`
	HasGameType   bool   `json:"hasGameType"`
	Hardcore      bool   `json:"hardcore,omitempty"`
	Difficulty    int    `json:"difficulty"`
	HasDifficulty bool   `json:"hasDifficulty"`
	AllowCommands bool   `json:"allowCommands,omitempty"`
	Seed          string `json:"seed,omitempty"`

	HasPlayerTag     bool   `json:"hasPlayerTag,omitempty"`     // 旧版单人存档的 Data.Player
	PlayerUUID       string `json:"playerUuid,omitempty"`       // Data.Player 里的 UUID（如果有）
	SingleplayerUUID string `json:"singleplayerUuid,omitempty"` // 26.1 起的 Data.singleplayer_uuid

	VersionHistory []string          `json:"versionHistory,omitempty"` // 26.3 起的 version_history（只当线索）
	EnabledPacks   []string          `json:"enabledPacks,omitempty"`
	DisabledPacks  []string          `json:"disabledPacks,omitempty"`
	GameRules      map[string]string `json:"gameRules,omitempty"`
	Dimensions     []string          `json:"dimensions,omitempty"` // WorldGenSettings.dimensions 的键
	ModMarkers     []string          `json:"modMarkers,omitempty"` // level.dat 里加载器留下的标记（fml、forge 等）
}

// Dimension 候选世界里的一个维度。
type Dimension struct {
	ID      string `json:"id"`      // minecraft:overworld、minecraft:the_nether、mymod:foo
	Rel     string `json:"rel"`     // 相对候选根（斜杠分隔）
	Regions int    `json:"regions"` // region/*.mca 的个数（0 = 还没生成过区块，不算坏）
}

// Candidate 来源里的一张地图。
type Candidate struct {
	ID         string      `json:"id"`
	Root       string      `json:"root"`            // 相对来源根的世界目录（斜杠分隔），"" 是来源根自己
	Group      []string    `json:"group,omitempty"` // 旧 Bukkit 布局同组的另外两个目录（相对来源根）
	FolderName string      `json:"folderName"`      // 世界目录的名字
	Layout     Layout      `json:"layout"`
	Meta       LevelMeta   `json:"meta"`
	Dimensions []Dimension `json:"dimensions"`

	PlayerFiles int      `json:"playerFiles"`           // playerdata / players/data 里的玩家文件数
	Datapacks   []string `json:"datapacks,omitempty"`   // 世界里 datapacks/ 下的名字
	ResourcePak bool     `json:"resourcePack"`          // 世界根有 resources.zip
	HasIcon     bool     `json:"hasIcon"`               // icon.png
	Readme      []string `json:"readme,omitempty"`      // 来源里和世界放在一起的说明文件（相对来源根）
	Files       int      `json:"files"`                 // 这张地图的文件数
	Bytes       int64    `json:"bytes"`                 // 这张地图的总大小
	Issues      []Issue  `json:"issues,omitempty"`      // 只和这张地图有关的检查结果
	SourceHints []string `json:"sourceHints,omitempty"` // 来源包里和地图并列、不会导入的东西（mods/、plugins/、server.jar…）

	players []string // 玩家文件的 UUID（文件名），不发给前端
}

// SourceRef 来源在哪里。Path 是主机上的路径，只在后端用，不发给前端和 AI。
type SourceRef struct {
	Kind     string `json:"kind"` // folder | zip
	Path     string `json:"-"`
	Name     string `json:"name"` // 给人看的名字（文件夹名或上传的文件名）
	Uploaded bool   `json:"uploaded"`
}

// Scan 一次扫描的结果（后端保管，按 ID 取）。
type Scan struct {
	ID          string      `json:"id"`
	Owner       string      `json:"-"` // 发起扫描的人（来源 + 设备），只有他能用这个结果
	Source      SourceRef   `json:"source"`
	Kind        InputKind   `json:"kind"`
	Candidates  []Candidate `json:"candidates"`
	Issues      []Issue     `json:"issues,omitempty"` // 整个来源的检查结果（不是世界、压缩包读不了…）
	Fingerprint string      `json:"-"`                // 来源内容的指纹：提交前再算一遍比对
	Files       int         `json:"files"`
	Bytes       int64       `json:"bytes"`
	CreatedAt   time.Time   `json:"createdAt"`
	ExpiresAt   time.Time   `json:"expiresAt"`
}

// Relation 地图保存版本和目标服务器版本的关系。
type Relation string

const (
	RelSame    Relation = "same"    // 同一个版本
	RelOlder   Relation = "older"   // 地图更旧：第一次启动时游戏会升级这份副本
	RelNewer   Relation = "newer"   // 地图更新：当前服务器不能安全读取
	RelUnknown Relation = "unknown" // 判断不了（版本名不在清单里、快照、字段缺失或互相矛盾）
)

// Move 暂存时的一步摆放：来源里的 From（相对来源根）放到目标服务器文件夹里的 To（相对实例目录）。
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SettingChange 一项建议改的服务器设置（用户勾选了才改）。
type SettingChange struct {
	Key    string `json:"key"`
	Before string `json:"before"`
	After  string `json:"after"`
	Why    string `json:"why"`
}

// Plan 一次导入的计划：用户在预览里看到并确认的就是它。
type Plan struct {
	ID          string `json:"id"`
	ScanID      string `json:"scanId"`
	CandidateID string `json:"candidateId"`
	Owner       string `json:"-"`

	Inst      string `json:"inst"`      // 目标服务器（新建时是将要用的名字）
	NewInst   bool   `json:"newInst"`   // 新建服务器的入口
	Core      string `json:"core"`      // 目标核心
	MC        string `json:"mc"`        // 目标 Minecraft 版本
	LevelName string `json:"levelName"` // 目标实际生效的 level-name

	Relation Relation        `json:"relation"`
	Layout   Layout          `json:"layout"`  // 摆放后的布局
	Moves    []Move          `json:"moves"`   // 暂存摆放
	Replace  []string        `json:"replace"` // 目标里会被整体移进恢复点的目录（相对实例目录）
	Missing  []string        `json:"missing,omitempty"`
	Settings []SettingChange `json:"settings,omitempty"`
	Issues   []Issue         `json:"issues"`

	NeedBytes   int64 `json:"needBytes"`   // 暂存要多少空间
	FreeBytes   int64 `json:"freeBytes"`   // 目标盘还剩多少
	MustStop    bool  `json:"mustStop"`    // 目标现在没停
	Blocked     bool  `json:"blocked"`     // 有 SevBlock
	NeedConfirm bool  `json:"needConfirm"` // 有 SevConfirm（提交时要带上 acknowledge）

	Fingerprint string    `json:"-"` // 来源指纹 + 候选 + 目标现状的摘要：提交前重算比对
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// 事务记录（journal）的状态。
const (
	TxStaging     = "staging"      // 正在往暂存区复制（没碰目标世界）
	TxStaged      = "staged"       // 暂存完成，等提交
	TxCommitting  = "committing"   // 正在切换（旧图移出、新图移入、改设置）
	TxCommitted   = "committed"    // 新图已就位；恢复点保留
	TxRollingBack = "rolling-back" // 正在退回旧图
	TxRolledBack  = "rolled-back"  // 已经退回旧图（导入没有生效）
	TxRestoring   = "restoring"    // 用户要求换回导入前的地图，正在换
	TxRestored    = "restored"     // 已换回导入前的地图
	TxStuck       = "stuck"        // 自动退不回去：保留全部数据，服务器暂停启动，等人处理
	// TxUnreadable 不是记录里会写的状态：记录文件读不出来或内容对不上时，「地图 / 世界」页这样标它
	TxUnreadable = "unreadable"
)

// Step 一步改名。From、To 都相对实例目录（斜杠分隔）。是否已经做完不看记录，看磁盘：From 不在、To 在就是做完了。
type Step struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Journal 一次导入（或换回旧图）的持久记录，放在 <实例>/.minedeck/world-import/<ID>/journal.json。
type Journal struct {
	ID      string    `json:"id"`
	Inst    string    `json:"inst"`
	State   string    `json:"state"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	Source    string `json:"source"`    // 来源给人看的名字
	Candidate string `json:"candidate"` // 地图名
	LevelName string `json:"levelName"`

	OldOut []Step `json:"oldOut"` // 旧世界的目录：实例里的原位 → 恢复点
	NewIn  []Step `json:"newIn"`  // 新世界的目录：暂存区 → 实例里的原位
	// 换回导入前的地图时：当前的世界目录（导入的那份，可能已经被游戏改过）移到 after-restore/，恢复点里的旧世界搬回原位
	CurOut []Step `json:"curOut,omitempty"`
	OldIn  []Step `json:"oldIn,omitempty"`

	PropsBefore map[string]string `json:"propsBefore,omitempty"` // 改了的设置原来的值（键不存在记为 nil 语义见实现）
	PropsAfter  map[string]string `json:"propsAfter,omitempty"`

	OldLevelSHA string   `json:"oldLevelSha,omitempty"` // 旧主世界 level.dat 的 SHA-256：换回后核对
	NewLevelSHA string   `json:"newLevelSha,omitempty"`
	OldBytes    int64    `json:"oldBytes,omitempty"` // 恢复点占的空间（移出时数的）
	NewBytes    int64    `json:"newBytes,omitempty"`
	LastExit    string   `json:"lastExit,omitempty"` // 换图前服务器上一次是怎么停的（killed 时旧图最后几分钟可能没存上）
	Err         string   `json:"error,omitempty"`
	Stuck       []string `json:"stuck,omitempty"` // 自动退不回去时，要人看的具体路径
}
