// Package toolbox 提供 MineDeck 工具箱的后端实现：
//   - 离线皮肤材质包生成（skinpack.go）：计算离线 UUID 与默认皮肤槽位、校验/转换皮肤 PNG 并打包为客户端资源包；
//   - Modrinth 整合包导出（mrpack.go）：将草稿待装清单或已有模组服导出为标准 .mrpack（modrinth.index.json + overrides/），并可用 internal/modpack 读回验证。
package toolbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/inst"
	"minedeck/internal/modcart"
	"minedeck/internal/winname"
)

const (
	// maxDownloads、maxDownloadBytes 缓存目录里最多保留最近多少份、合计多大的生成结果；超了从最旧的删
	// （最新的一份总是留着，哪怕它自己就超了合计上限）。
	maxDownloads     = 24
	maxDownloadBytes = 1 << 30
	// draftFile 整合包草稿设置保存在数据目录的文件名，重开程序仍保留。
	draftFile = "mrpack-draft.json"
)

// Options 初始化工具箱服务的可选目录配置；测试可传入临时目录，不传则使用 app 默认目录。
type Options struct {
	Mgr      *inst.Manager
	Cart     *modcart.Service
	DataDir  string
	CacheDir string
}

// Service 工具箱服务，负责草稿元数据持久化、临时产物管理及两个工具的业务执行。
type Service struct {
	mgr      *inst.Manager
	cart     *modcart.Service
	dataDir  string
	cacheDir string

	draftMu       sync.Mutex
	draftSeq      map[string]map[string]int64 // clientId -> field -> 已接受的最大序号（A65 防乱序）
	draftSeqOrder []string
	// buildMu 生成一次只做一份：整包在内存里拼，局域网的人连点几次不至于同时占好几份几百 MB 的内存
	buildMu sync.Mutex

	dlMu    sync.Mutex
	dlOrder []string
	dlMap   map[string]DownloadEntry
}

// DownloadEntry 一份已生成的可下载压缩包记录。
type DownloadEntry struct {
	ID       string    `json:"id"`
	FileName string    `json:"fileName"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	SHA1     string    `json:"sha1"`
	At       time.Time `json:"at"`
}

// New 创建工具箱服务；DataDir/CacheDir 为空时回退到 app.Base/data/toolbox 与 app.CacheDir/toolbox。
func New(opt Options) *Service {
	dataDir := opt.DataDir
	if dataDir == "" && app.Base != "" {
		dataDir = filepath.Join(app.Base, "data", "toolbox")
	}
	cacheDir := opt.CacheDir
	if cacheDir == "" {
		base := app.CacheDir
		if base == "" && app.Base != "" {
			base = filepath.Join(app.Base, "cache")
		}
		if base != "" {
			cacheDir = filepath.Join(base, "toolbox")
		}
	}
	s := &Service{
		mgr:      opt.Mgr,
		cart:     opt.Cart,
		dataDir:  dataDir,
		cacheDir: cacheDir,
		dlMap:    map[string]DownloadEntry{},
	}
	s.loadCachedDownloads()
	return s
}

// SkinUploadDir 网页层接收上传皮肤 PNG 时暂存的专用目录；操作层只允许从这个目录读取上传文件。
func (s *Service) SkinUploadDir() string {
	return filepath.Join(s.cacheDir, "skin-upload")
}

func (s *Service) downloadsDir() string {
	return filepath.Join(s.cacheDir, "downloads")
}

// loadCachedDownloads 程序或服务创建时扫一遍缓存下载目录里的元数据（若磁盘上文件还在就接上）。
func (s *Service) loadCachedDownloads() {
	dir := s.downloadsDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var entry DownloadEntry
		if json.Unmarshal(b, &entry) != nil || !validDownloadID(entry.ID) {
			continue
		}
		binPath := filepath.Join(dir, entry.ID+".bin")
		st, err := os.Stat(binPath)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		entry.Path = binPath
		entry.Size = st.Size()
		s.dlMap[entry.ID] = entry
		s.dlOrder = append(s.dlOrder, entry.ID)
	}
	// 目录是按编号（随机）列出来的：按生成时间排好，超出上限的从最旧的删
	slices.SortFunc(s.dlOrder, func(a, b string) int { return s.dlMap[a].At.Compare(s.dlMap[b].At) })
	s.evictLocked()
	// 没有 meta 的 .bin（写 meta 之前程序退了）认不回来，删掉
	for _, e := range ents {
		if id, ok := strings.CutSuffix(e.Name(), ".bin"); ok && !e.IsDir() {
			if _, known := s.dlMap[id]; !known {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}

// evictLocked 超出份数或合计大小上限时从最旧的删，最新的一份总是留着。调用方持有 dlMu（或还没有并发）。
func (s *Service) evictLocked() {
	var total int64
	for _, id := range s.dlOrder {
		total += s.dlMap[id].Size
	}
	dir := s.downloadsDir()
	for len(s.dlOrder) > 1 && (len(s.dlOrder) > maxDownloads || total > maxDownloadBytes) {
		oldest := s.dlOrder[0]
		s.dlOrder = s.dlOrder[1:]
		total -= s.dlMap[oldest].Size
		delete(s.dlMap, oldest)
		_ = os.Remove(filepath.Join(dir, oldest+".bin"))
		_ = os.Remove(filepath.Join(dir, oldest+".meta.json"))
	}
}

func validDownloadID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func newDownloadID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// saveArtifact 把生成好的压缩包字节存入下载缓存区并返回下载记录；超过保留上限时清理最旧的记录。
func (s *Service) saveArtifact(fileName string, data []byte, sha1Hex string) (DownloadEntry, error) {
	dir := s.downloadsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DownloadEntry{}, fmt.Errorf("创建缓存目录失败：%w", err)
	}
	id, err := newDownloadID()
	if err != nil {
		return DownloadEntry{}, err
	}
	binPath := filepath.Join(dir, id+".bin")
	if err := atomicfile.Write(binPath, data, 0o644); err != nil {
		return DownloadEntry{}, fmt.Errorf("写入缓存文件失败：%w", err)
	}
	entry := DownloadEntry{
		ID:       id,
		FileName: fileName,
		Path:     binPath,
		Size:     int64(len(data)),
		SHA1:     sha1Hex,
		At:       time.Now(),
	}
	metaBytes, _ := json.Marshal(entry)
	if err := atomicfile.Write(filepath.Join(dir, id+".meta.json"), metaBytes, 0o644); err != nil {
		_ = os.Remove(binPath) // 没有 meta 的 .bin 重启以后认不回来，留着只占地方
		return DownloadEntry{}, fmt.Errorf("写入缓存文件失败：%w", err)
	}

	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	s.dlMap[id] = entry
	s.dlOrder = append(s.dlOrder, id)
	s.evictLocked()
	return entry, nil
}

// GetDownload 根据编号取得已生成的压缩包文件信息；找不到返回错误。
func (s *Service) GetDownload(id string) (DownloadEntry, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validDownloadID(id) {
		return DownloadEntry{}, ErrNotFound("下载编号不对")
	}
	s.dlMu.Lock()
	entry, ok := s.dlMap[id]
	s.dlMu.Unlock()
	if !ok {
		return DownloadEntry{}, ErrNotFound("这个下载已经过期或不存在，请重新生成")
	}
	st, err := os.Stat(entry.Path)
	if err != nil || !st.Mode().IsRegular() {
		return DownloadEntry{}, ErrNotFound("缓存里的生成文件已经不在了，请重新生成")
	}
	return entry, nil
}

// copyToHostDir 当本机用户指定了保存目录时，把压缩包写到目标目录（由调用方保证已校验 caller.Local）。
func copyToHostDir(destDir, fileName string, data []byte) (string, error) {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return "", nil
	}
	if !filepath.IsAbs(destDir) {
		return "", ErrBadRequest("保存文件夹要写完整路径（比如 D:\\整合包），或者点「浏览…」选")
	}
	clean := filepath.Clean(destDir)
	st, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrBadRequest("指定的保存文件夹不存在")
		}
		return "", ErrBadRequest("指定的保存文件夹打不开")
	}
	if !st.IsDir() {
		return "", ErrBadRequest("指定的保存位置不是文件夹")
	}
	if !winname.OK(fileName) {
		return "", ErrBadRequest("生成文件名不合规")
	}
	target := filepath.Join(clean, fileName)
	if err := atomicfile.Write(target, data, 0o644); err != nil {
		return "", fmt.Errorf("保存到指定文件夹失败：%w", err)
	}
	return target, nil
}

// safeFileStem 把用户输入名称清理成适合当文件名的前缀（只留安全字符，防目录穿越和 Windows 设备名）。
func safeFileStem(raw, fallback string) string {
	s := strings.TrimSpace(raw)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s)
	s = strings.Trim(s, ". ")
	if s == "" || !winname.OK(s) || !winname.OK(s+".zip") {
		return fallback
	}
	if len([]rune(s)) > 64 {
		s = strings.TrimRight(string([]rune(s)[:64]), ". ")     // 截断以后末尾又可能是点或空格，Windows 会把它们吃掉
		if s == "" || !winname.OK(s) || !winname.OK(s+".zip") { // 截出来的可能正好是 NUL 这类设备名
			return fallback
		}
	}
	return s
}

// Error 带 HTTP 状态码的操作失败。
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func ErrBadRequest(msg string) error { return &Error{Code: 400, Err: errors.New(msg)} }
func ErrNotFound(msg string) error   { return &Error{Code: 404, Err: errors.New(msg)} }

// DraftConfig 用户在工具箱里填的整合包草稿设置（存到 <data>/toolbox/mrpack-draft.json，重开程序不丢）。
type DraftConfig struct {
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Summary       string `json:"summary"`
	Core          string `json:"core"`
	MC            string `json:"mc"`
	LoaderVersion string `json:"loaderVersion"`
	IncludeConfig bool   `json:"includeConfig"`
}

// GetMrpackDraft 读取持久化的整合包草稿设置；尚未保存过、读不出来都返回合理的默认值（生成整合包时用）。
func (s *Service) GetMrpackDraft() DraftConfig {
	cfg, _ := s.ReadMrpackDraft()
	return cfg
}

// ReadMrpackDraft 同 GetMrpackDraft，但文件在却读不出来（被别的程序占着、没有权限）时还返回错误：界面据此这一次
// 不自动保存，免得用默认值盖掉存好的草稿（Codex 复审 beta.3 第四轮）。内容坏了（不是 JSON）按没有草稿算，
// 坏的内容留着也用不上。
func (s *Service) ReadMrpackDraft() (DraftConfig, error) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	return s.readDraft()
}

// readDraft ReadMrpackDraft 读文件的那一段，不拿锁：调用方持有 draftMu（PatchMrpackDraft 在锁里读-合-写，
// 再调 ReadMrpackDraft 会自己锁住自己）。
func (s *Service) readDraft() (DraftConfig, error) {
	def := DraftConfig{
		Name:      "我的整合包",
		VersionID: "1.0.0",
		Core:      "fabric",
		MC:        "1.20.1",
	}
	if s.dataDir == "" {
		return def, nil
	}
	b, err := os.ReadFile(filepath.Join(s.dataDir, draftFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return def, nil
		}
		return def, fmt.Errorf("读不出存好的整合包草稿（%w）", err)
	}
	var cfg DraftConfig
	if json.Unmarshal(b, &cfg) != nil {
		return def, nil
	}
	if cfg.Name == "" {
		cfg.Name = def.Name
	}
	if cfg.VersionID == "" {
		cfg.VersionID = def.VersionID
	}
	if cfg.Core == "" {
		cfg.Core = def.Core
	}
	if cfg.MC == "" {
		cfg.MC = def.MC
	}
	return cfg, nil
}

// normDraft 去掉各项前后的空白，查长度（太长报 400）。整份保存和按字段改共用。
func normDraft(cfg *DraftConfig) error {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.VersionID = strings.TrimSpace(cfg.VersionID)
	cfg.Summary = strings.TrimSpace(cfg.Summary)
	cfg.Core = strings.TrimSpace(cfg.Core)
	cfg.MC = strings.TrimSpace(cfg.MC)
	cfg.LoaderVersion = strings.TrimSpace(cfg.LoaderVersion)
	if len([]rune(cfg.Name)) > 100 || len([]rune(cfg.VersionID)) > 50 || len([]rune(cfg.Summary)) > 300 {
		return ErrBadRequest("整合包名称、版本号或简介太长")
	}
	return nil
}

// DraftPatch 只带要改的草稿字段：nil 表示不改，沿用存好的那份（JSON 名同 DraftConfig）。
// 两个窗口同时开着工具箱时，以前每次保存都把整页的草稿写回去，没刷新的那一边就把另一边刚改的名字、简介改回原样；
// 现在界面只发和这一页基线不一样的字段（2026-11-17）。另可带 ClientID 与递增 Seq，乱序到达的旧请求逐字段忽略（A65）。
type DraftPatch struct {
	Name          *string `json:"name"`
	VersionID     *string `json:"versionId"`
	Summary       *string `json:"summary"`
	Core          *string `json:"core"`
	MC            *string `json:"mc"`
	LoaderVersion *string `json:"loaderVersion"`
	IncludeConfig *bool   `json:"includeConfig"`
	ClientID      string  `json:"clientId,omitempty"`
	Seq           int64   `json:"seq,omitempty"`
}

const maxDraftSeqClients = 64

// filterBySeqLocked 按 clientId 与字段各自记录的最大序号过滤掉乱序晚到的旧字段。调用方持有 draftMu。
func (s *Service) filterBySeqLocked(p DraftPatch) (DraftPatch, []string, bool) {
	hasAny := p.Name != nil || p.VersionID != nil || p.Summary != nil || p.Core != nil || p.MC != nil || p.LoaderVersion != nil || p.IncludeConfig != nil
	if p.ClientID == "" || p.Seq <= 0 {
		return p, nil, hasAny
	}
	seen := s.draftSeq[p.ClientID]
	accept := func(field string, set bool) bool {
		return set && p.Seq > seen[field]
	}
	var applied []string
	if !accept("name", p.Name != nil) {
		p.Name = nil
	} else {
		applied = append(applied, "name")
	}
	if !accept("versionId", p.VersionID != nil) {
		p.VersionID = nil
	} else {
		applied = append(applied, "versionId")
	}
	if !accept("summary", p.Summary != nil) {
		p.Summary = nil
	} else {
		applied = append(applied, "summary")
	}
	if !accept("core", p.Core != nil) {
		p.Core = nil
	} else {
		applied = append(applied, "core")
	}
	if !accept("mc", p.MC != nil) {
		p.MC = nil
	} else {
		applied = append(applied, "mc")
	}
	if !accept("loaderVersion", p.LoaderVersion != nil) {
		p.LoaderVersion = nil
	} else {
		applied = append(applied, "loaderVersion")
	}
	if !accept("includeConfig", p.IncludeConfig != nil) {
		p.IncludeConfig = nil
	} else {
		applied = append(applied, "includeConfig")
	}
	return p, applied, len(applied) > 0
}

func (s *Service) advanceSeqLocked(clientID string, seq int64, fields []string) {
	if clientID == "" || seq <= 0 || len(fields) == 0 {
		return
	}
	if s.draftSeq == nil {
		s.draftSeq = map[string]map[string]int64{}
	}
	m := s.draftSeq[clientID]
	if m == nil {
		m = map[string]int64{}
		s.draftSeq[clientID] = m
		s.draftSeqOrder = append(s.draftSeqOrder, clientID)
		for len(s.draftSeqOrder) > maxDraftSeqClients {
			evict := s.draftSeqOrder[0]
			s.draftSeqOrder = s.draftSeqOrder[1:]
			delete(s.draftSeq, evict)
		}
	} else if idx := slices.Index(s.draftSeqOrder, clientID); idx >= 0 {
		s.draftSeqOrder = append(append(s.draftSeqOrder[:idx], s.draftSeqOrder[idx+1:]...), clientID)
	}
	for _, f := range fields {
		if seq > m[f] {
			m[f] = seq
		}
	}
}

// mergeInto 把给了的字段合到 cfg 上。
func (p DraftPatch) mergeInto(cfg DraftConfig) DraftConfig {
	set := func(dst, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&cfg.Name, p.Name)
	set(&cfg.VersionID, p.VersionID)
	set(&cfg.Summary, p.Summary)
	set(&cfg.Core, p.Core)
	set(&cfg.MC, p.MC)
	set(&cfg.LoaderVersion, p.LoaderVersion)
	if p.IncludeConfig != nil {
		cfg.IncludeConfig = *p.IncludeConfig
	}
	return cfg
}

// PatchMrpackDraft 只改 p 里给了的字段，返回合起来的整份草稿。读存好的、合、查长度、写盘都在 draftMu 里一次做完，
// 两个窗口先后保存，后一个合在前一个存下的结果上。带 ClientID 和 Seq 时忽略序号不大于已存值的字段。
// 存好的文件在却读不出来时报错（*Error，500）、不写：拿默认值合上去再写，就把存好的草稿盖掉了。内容坏了（不是 JSON）照 ReadMrpackDraft 按默认值算。
// 没有数据目录时不写盘，只返回合好的。
func (s *Service) PatchMrpackDraft(p DraftPatch) (DraftConfig, error) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	cur, err := s.readDraft()
	if err != nil {
		return DraftConfig{}, &Error{Code: 500, Err: fmt.Errorf("%w，这次的改动没有保存", err)}
	}
	eff, applied, anyNew := s.filterBySeqLocked(p)
	if !anyNew {
		_ = normDraft(&cur)
		return cur, nil
	}
	cfg := eff.mergeInto(cur)
	if err := normDraft(&cfg); err != nil {
		return cfg, err
	}
	if s.dataDir != "" {
		if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
			return cfg, fmt.Errorf("创建草稿目录失败：%w", err)
		}
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return cfg, err
		}
		if err := atomicfile.Write(filepath.Join(s.dataDir, draftFile), b, 0o644); err != nil {
			return cfg, fmt.Errorf("保存草稿失败：%w", err)
		}
	}
	s.advanceSeqLocked(p.ClientID, p.Seq, applied)
	return cfg, nil
}

// SaveMrpackDraft 保存用户的整合包草稿设置（原子写盘），整份替换。界面和操作层现在走 PatchMrpackDraft。
func (s *Service) SaveMrpackDraft(cfg DraftConfig) (DraftConfig, error) {
	if err := normDraft(&cfg); err != nil {
		return cfg, err
	}
	if s.dataDir == "" {
		return cfg, nil
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return cfg, fmt.Errorf("创建草稿目录失败：%w", err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return cfg, err
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if err := atomicfile.Write(filepath.Join(s.dataDir, draftFile), b, 0o644); err != nil {
		return cfg, fmt.Errorf("保存草稿失败：%w", err)
	}
	return cfg, nil
}

// readLimited 从 r 读取最多 maxBytes；超出时报错。
func readLimited(r io.Reader, maxBytes int64, tooBigMsg string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrBadRequest(tooBigMsg)
	}
	return b, nil
}
