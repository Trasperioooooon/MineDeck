package worldimport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// Service 地图导入的入口：扫描结果、导入计划由它保管（绑定发起的人，过期清理），导入、换回旧图、启动时的恢复都从这里走。
type Service struct {
	M *inst.Manager

	mu      sync.Mutex
	scans   map[string]*scanResult
	plans   map[string]*Plan
	cancels map[string]cancelEntry  // 导入任务编号 → 取消（只在暂存阶段生效）
	claims  map[string]*createClaim // 建服请求占着的计划编号 → 计划和扫描结果
	// active 正在进行的导入的事务目录（小写的绝对路径）。暂存在占位之外进行，「重试恢复」不能把它们当成
	// 上次没走完的暂存清掉：清掉以后复制还在往里写，最后会把半份地图当成整份换上去
	active map[string]bool

	order versionOrder
	now   func() time.Time
	// StopTimeout「停服并导入」等服务器自己退出的最长时间
	StopTimeout time.Duration
}

// 扫描结果和计划的有效期。claimTTL 是建服请求占着计划的最长时间：新建流水线自己最多跑 90 分钟，
// 结束时会放掉；这里只是兜底，万一没放掉也不会一直占着上传的文件。
const (
	scanTTL  = 2 * time.Hour
	planTTL  = 30 * time.Minute
	claimTTL = 3 * time.Hour
)

type cancelEntry struct {
	owner string
	fn    context.CancelFunc
}

// createClaim 新建服务器的请求收下了一份计划：计划从可用的计划里拿走（同一份不能再用），
// 扫描结果钉住（过期、放弃都不删上传的文件），直到新建任务结束。
type createClaim struct {
	owner string
	p     *Plan
	r     *scanResult
	until time.Time
	used  bool // 已经开始往新服务器里放地图了（放过就不能再用）
}

// NewService 建服务，并把「新建服务器时放地图」的钩子挂到实例管理器上。
func NewService(m *inst.Manager) *Service {
	s := &Service{M: m, scans: map[string]*scanResult{}, plans: map[string]*Plan{}, cancels: map[string]cancelEntry{},
		claims: map[string]*createClaim{}, active: map[string]bool{}, order: manifestOrder, now: time.Now, StopTimeout: 2 * time.Minute}
	m.SetWorldPlacer(s.placeForCreate, s.ReleaseCreate)
	return s
}

// setActive 登记一个正在进行的导入的事务目录，返回撤销登记的函数。
func (s *Service) setActive(txDir string) func() {
	k := strings.ToLower(filepath.Clean(txDir))
	s.mu.Lock()
	s.active[k] = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.active, k)
		s.mu.Unlock()
	}
}

func (s *Service) isActive(txDir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[strings.ToLower(filepath.Clean(txDir))]
}

// unpin 一个任务用完了扫描结果；它已经过期或被放弃、又没有别的任务在用时，删掉上传的文件。
func (s *Service) unpin(r *scanResult) {
	s.mu.Lock()
	r.pins--
	drop := r.pins <= 0 && r.dropped && r.Source.Uploaded
	s.mu.Unlock()
	if drop {
		_ = os.Remove(r.Source.Path)
	}
}

// dropScanLocked 把扫描结果标成不要了；有任务在用时不删上传的文件（用完再删），返回现在就该删的文件。
func dropScanLocked(r *scanResult) string {
	r.dropped = true
	if r.pins > 0 || !r.Source.Uploaded {
		return ""
	}
	return r.Source.Path
}

func newID(prefix string) string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// ---------- 扫描 ----------

// ScanPath 扫描本机路径（文件夹或文件）。只给本机的人用：调用方负责确认。
func (s *Service) ScanPath(owner, p string) (*Scan, error) {
	p = filepath.Clean(strings.TrimSpace(p))
	if !filepath.IsAbs(p) {
		return nil, fmt.Errorf("请选一个完整的路径")
	}
	return s.scan(owner, p, filepath.Base(p), false)
}

// ScanUpload 扫描已经上传到缓存目录的文件（display 是上传时的文件名，只用来显示和认扩展名）。
func (s *Service) ScanUpload(owner, file, display string) (*Scan, error) {
	if filepath.Dir(filepath.Clean(file)) != UploadDir() {
		return nil, fmt.Errorf("文件不在上传文件夹里")
	}
	return s.scan(owner, file, display, true)
}

// UploadDir 局域网上传的地图放在哪。
func UploadDir() string { return filepath.Join(app.CacheDir, "worldimport", "uploads") }

func (s *Service) scan(owner, p, display string, uploaded bool) (*Scan, error) {
	s.sweep()
	r, err := scanPath(p, cleanText(display, 120), uploaded)
	if err != nil {
		return nil, err
	}
	if r.src != nil {
		r.src.Close() // 只留结果；暂存时重新打开，顺便核对来源有没有变
	}
	r.ID = newID("s")
	r.Owner = owner
	r.CreatedAt = s.now()
	r.ExpiresAt = r.CreatedAt.Add(scanTTL)
	if r.Candidates == nil {
		r.Candidates = []Candidate{}
	}
	s.mu.Lock()
	s.scans[r.ID] = r
	s.mu.Unlock()
	out := r.Scan
	return &out, nil
}

func (s *Service) getScan(owner, id string) (*scanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.scans[id]
	if r == nil || r.Owner != owner {
		return nil, fmt.Errorf("找不到这次检测的结果，可能已经过期，请重新选择地图")
	}
	if s.now().After(r.ExpiresAt) {
		return nil, fmt.Errorf("这次检测的结果已经过期，请重新选择地图")
	}
	return r, nil
}

// Readme 读来源里的一份说明书（按不可信的纯文本处理：去掉控制字符，限长）。
func (s *Service) Readme(owner, scanID, candID, rel string) (string, error) {
	r, err := s.getScan(owner, scanID)
	if err != nil {
		return "", err
	}
	info := r.cands[candID]
	if info == nil || !containsFold(info.c.Readme, rel) {
		return "", fmt.Errorf("没有这份说明")
	}
	src, err := reopen(r)
	if err != nil {
		return "", err
	}
	defer src.Close()
	e := src.find(rel)
	if e == nil {
		return "", fmt.Errorf("说明文件不见了")
	}
	b, err := src.readSmall(e, 256<<10)
	if err != nil {
		return "", err
	}
	text := strings.ToValidUTF8(string(b), "�")
	var sb strings.Builder
	for _, line := range strings.Split(text, "\n") {
		sb.WriteString(cleanText(strings.TrimRight(line, "\r"), 400))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String()), nil
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// reopen 重新打开一份来源（暂存、读说明书、核对指纹时用）。
func reopen(r *scanResult) (*source, error) {
	if r.Source.Kind == "folder" {
		return openFolder(r.Source.Path)
	}
	return openZip(r.Source.Path)
}

// sweep 清掉过期的扫描结果和计划，以及过期的上传文件。
func (s *Service) sweep() {
	now := s.now()
	s.mu.Lock()
	var drop []string
	for id, r := range s.scans {
		if now.After(r.ExpiresAt) {
			if p := dropScanLocked(r); p != "" {
				drop = append(drop, p)
			}
			delete(s.scans, id)
		}
	}
	for id, p := range s.plans {
		if now.After(p.ExpiresAt) {
			delete(s.plans, id)
		}
	}
	var stale []*createClaim
	for id, c := range s.claims {
		if now.After(c.until) {
			stale = append(stale, c)
			delete(s.claims, id)
		}
	}
	inUse := map[string]bool{} // 正在用的上传文件：放久了也不删
	for _, r := range s.scans {
		if r.pins > 0 {
			inUse[strings.ToLower(filepath.Base(r.Source.Path))] = true
		}
	}
	for _, c := range s.claims {
		inUse[strings.ToLower(filepath.Base(c.r.Source.Path))] = true
	}
	s.mu.Unlock()
	for _, c := range stale {
		s.unpin(c.r)
	}
	for _, p := range drop {
		_ = os.Remove(p)
	}
	if ents, err := os.ReadDir(UploadDir()); err == nil {
		for _, e := range ents {
			if inUse[strings.ToLower(e.Name())] {
				continue
			}
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > scanTTL+time.Hour {
				_ = os.Remove(filepath.Join(UploadDir(), e.Name()))
			}
		}
	}
}

// Discard 用户放弃这次检测：删掉结果和上传的文件。
func (s *Service) Discard(owner, scanID string) {
	s.mu.Lock()
	r := s.scans[scanID]
	if r == nil || r.Owner != owner {
		s.mu.Unlock()
		return
	}
	delete(s.scans, scanID)
	for id, p := range s.plans {
		if p.ScanID == scanID {
			delete(s.plans, id)
		}
	}
	drop := dropScanLocked(r) // 已经开始的导入、建服任务不受影响：上传的文件等它们用完再删
	s.mu.Unlock()
	if drop != "" {
		_ = os.Remove(drop)
	}
}

// ---------- 计划 ----------

// PlanReq 生成计划的请求：替换已有服务器的地图（Inst），或者新建服务器（NewInst + Core + MC）。
type PlanReq struct {
	ScanID    string     `json:"scanId"`
	Candidate string     `json:"candidate"`
	Inst      string     `json:"inst"`
	NewInst   bool       `json:"newInst"`
	Core      mcsrc.Core `json:"core"`
	MC        string     `json:"mc"`
}

// targetOf 已有服务器的现状。
func (s *Service) targetOf(name string) (*Target, *inst.Instance, error) {
	i, err := s.M.Get(name)
	if err != nil {
		return nil, nil, err
	}
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return nil, nil, fmt.Errorf("「%s」是代理端（Velocity / Waterfall），它本身没有世界，不能导入地图。请把地图导入到它后面的游戏服务器", name)
	}
	t := &Target{Inst: name, Dir: i.Dir, Core: i.Core, MC: i.MC, LevelName: inst.LevelName(i.Dir), Props: map[string]string{}, launch: i.LaunchTarget}
	if b, err := os.ReadFile(i.PropsPath()); err == nil {
		t.Props = inst.JavaProps(b)
	}
	t.jarWV = jarWorldVersion(i.Dir, i.LaunchTarget)
	return t, i, nil
}

// targetFP 目标现状的指纹：核心、版本、level-name，以及各世界目录在不在、level.dat 的大小和修改时间。
func targetFP(t *Target) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|", t.Core, t.MC, t.LevelName)
	if t.Dir != "" {
		for _, n := range worldDirNames(t.LevelName) {
			p := filepath.Join(t.Dir, filepath.FromSlash(n))
			if st, err := os.Lstat(p); err == nil {
				fmt.Fprintf(h, "%s:dir=%v|", n, st.IsDir())
				if l, err := os.Stat(filepath.Join(p, "level.dat")); err == nil {
					fmt.Fprintf(h, "%d:%d|", l.Size(), l.ModTime().UnixNano())
				}
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MakePlan 生成导入计划（不改任何东西）。
func (s *Service) MakePlan(ctx context.Context, owner string, req PlanReq) (*Plan, error) {
	r, err := s.getScan(owner, req.ScanID)
	if err != nil {
		return nil, err
	}
	info := r.cands[req.Candidate]
	if info == nil {
		return nil, fmt.Errorf("请选择要导入的地图")
	}
	var t *Target
	var i *inst.Instance
	if req.NewInst {
		if !mcsrc.ValidCore(req.Core) || req.MC == "" {
			return nil, fmt.Errorf("请选择核心和版本")
		}
		if mcsrc.KindOf(req.Core) == mcsrc.KindProxy {
			return nil, fmt.Errorf("代理端（Velocity / Waterfall）没有世界，不能用地图开服")
		}
		t = &Target{New: true, Core: req.Core, MC: req.MC, LevelName: "world",
			Props: map[string]string{"online-mode": "true"}}
	} else {
		t, i, err = s.targetOf(req.Inst)
		if err != nil {
			return nil, err
		}
	}
	p := s.evaluate(ctx, r, info, t, i)
	p.ID = newID("p")
	p.Owner = owner
	p.CreatedAt = s.now()
	p.ExpiresAt = p.CreatedAt.Add(planTTL)
	s.mu.Lock()
	s.plans[p.ID] = p
	s.mu.Unlock()
	out := *p
	return &out, nil
}

// evaluate 按地图和目标算出计划内容。
func (s *Service) evaluate(ctx context.Context, r *scanResult, info *candInfo, t *Target, i *inst.Instance) *Plan {
	c := info.c
	p := &Plan{ScanID: r.ID, CandidateID: c.ID, Inst: t.Inst, NewInst: t.New, Core: string(t.Core), MC: t.MC, LevelName: t.LevelName}
	p.Issues = append(p.Issues, r.Issues...)
	p.Issues = append(p.Issues, c.Issues...)
	if r.Kind != KindJavaWorld && r.Kind != KindDamagedWorld {
		p.Issues = append(p.Issues, Issue{Sev: SevBlock, Code: "not-world", Msg: "这不是能导入的 Java 版世界"})
	}
	rel, is := relation(ctx, s.order, c.Meta, t)
	p.Relation = rel
	p.Issues = append(p.Issues, is...)
	fam := coreFamily(t.Core)
	modern, ok := isModern(ctx, s.order, t.MC)
	if !ok {
		p.Issues = append(p.Issues, Issue{Sev: SevBlock, Code: "target-version-unknown",
			Msg: "认不出服务器的 Minecraft 版本「" + t.MC + "」，没法确定世界该怎么摆。联网后再试一次"})
	}
	mp, is := planMapping(c, fam, modern, t.LevelName)
	p.Issues = append(p.Issues, is...)
	p.Issues = append(p.Issues, mp.notes...)
	p.Moves = mp.moves
	p.Layout = mp.layout
	p.Issues = append(p.Issues, modChecks(c, fam)...)
	p.Issues = append(p.Issues, worldRootChecks(t)...)
	p.Issues = append(p.Issues, playerNotes(c, t)...)
	p.Issues = append(p.Issues, playNotes(c)...)
	// 维度：地图没提供的分开说，不能让人以为是导丢了
	for _, d := range []struct{ id, name string }{{"minecraft:the_nether", "下界"}, {"minecraft:the_end", "末地"}} {
		if !hasDim(c, d.id) {
			p.Missing = append(p.Missing, d.name)
		}
	}
	if len(p.Missing) > 0 {
		p.Issues = append(p.Issues, Issue{Sev: SevInfo, Code: "dims-not-provided",
			Msg: "地图没有提供" + strings.Join(p.Missing, "、") + "。这不是导入丢了：服务器第一次用到时会生成全新的" + strings.Join(p.Missing, "、") + "，不会沿用旧地图的"})
	}
	// 目标的旧世界：level-name 的主目录和 _nether、_the_end，有哪个移哪个
	if t.Dir != "" {
		for _, n := range worldDirNames(t.LevelName) {
			if exists(filepath.Join(t.Dir, filepath.FromSlash(n))) {
				p.Replace = append(p.Replace, n)
			}
		}
		p.Settings = settingSuggestions(c.Meta, t.Props)
		if i.Status() != "stopped" {
			p.MustStop = true
		}
		if i.LastExit() == "killed" && len(p.Replace) > 0 {
			p.Issues = append(p.Issues, Issue{Sev: SevWarn, Code: "last-killed",
				Msg: "这台服务器上次是被强制结束的，旧地图最后几分钟的进度可能没存上。恢复点保存的是现在磁盘上的样子"})
		}
		if others := otherWorlds(t.Dir, t.LevelName); len(others) > 0 {
			p.Issues = append(p.Issues, Issue{Sev: SevInfo, Code: "other-worlds",
				Msg:    "服务器文件夹里还有别的世界（多世界插件建的），它们不会被改动",
				Detail: strings.Join(firstN(others, 8), "、")})
		}
	}
	// 来源和要替换的世界是同一个位置（选了这台服务器自己的世界文件夹）：换的时候会把来源移进恢复点
	if t.Dir != "" && r.Source.Kind == "folder" {
		for _, root := range info.roots {
			src := filepath.Join(r.Source.Path, filepath.FromSlash(root))
			for _, n := range worldDirNames(t.LevelName) {
				dst := filepath.Join(t.Dir, filepath.FromSlash(n))
				if sameOrInside(src, dst) || sameOrInside(dst, src) {
					p.Issues = append(p.Issues, Issue{Sev: SevBlock, Code: "source-is-target",
						Msg: "选的地图就是这台服务器正在用的世界（或者在它里面），不能拿它替换它自己"})
				}
			}
		}
	}
	// 空间：暂存一份新地图，加 256 MB 余量；旧世界是改名移走的，不再占一份
	p.NeedBytes = c.Bytes + 256<<20
	root := t.Dir
	if root == "" {
		root = app.ServersRoot()
	}
	if free, err := app.DiskFree(root); err == nil {
		p.FreeBytes = int64(free)
		if p.NeedBytes > p.FreeBytes {
			p.Issues = append(p.Issues, Issue{Sev: SevBlock, Code: "disk-full",
				Msg: fmt.Sprintf("磁盘空间不够：这张地图要 %s，服务器所在的盘只剩 %s", fmtBytes(p.NeedBytes), fmtBytes(p.FreeBytes))})
		}
	}
	for _, is := range p.Issues {
		switch is.Sev {
		case SevBlock:
			p.Blocked = true
		case SevConfirm:
			p.NeedConfirm = true
		}
	}
	p.Fingerprint = r.Fingerprint + "|" + info.fp + "|" + targetFP(t)
	sortIssues(p.Issues)
	return p
}

// sortIssues 严重的在前，同级保持原来的顺序。
func sortIssues(is []Issue) {
	rank := map[Severity]int{SevBlock: 0, SevConfirm: 1, SevWarn: 2, SevInfo: 3}
	sort.SliceStable(is, func(a, b int) bool { return rank[is[a].Sev] < rank[is[b].Sev] })
}

// otherWorlds 服务器根目录下别的世界（带 level.dat 的文件夹，level-name 那三个除外）。
func otherWorlds(dir, level string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	mine := map[string]bool{}
	for _, n := range worldDirNames(level) {
		mine[strings.ToLower(n)] = true
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() || mine[strings.ToLower(e.Name())] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "level.dat")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

var (
	velocityOnRe = regexp.MustCompile(`(?m)^\s*velocity:\s*\n(?:\s+.*\n)*?\s+enabled:\s*true`)
	bungeeOnRe   = regexp.MustCompile(`(?m)^\s*bungeecord:\s*true`)
)

// behindProxy 这台服务器是不是在代理后面（玩家的 UUID 由代理转发过来）。
func behindProxy(dir string) bool {
	if dir == "" {
		return false
	}
	for _, f := range []string{filepath.Join("config", "paper-global.yml"), "paper.yml"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && velocityOnRe.Match(b) {
			return true
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "spigot.yml")); err == nil && bungeeOnRe.Match(b) {
		return true
	}
	return false
}

// playerNotes 玩家数据的说明：默认保留来源的玩家文件；单人存档主人的进度能不能接上，看账号和正版验证。
func playerNotes(c *Candidate, t *Target) []Issue {
	var is []Issue
	m := c.Meta
	online := strings.ToLower(strings.TrimSpace(t.Props["online-mode"])) != "false"
	proxy := behindProxy(t.Dir)
	owner, file := "", ""
	switch {
	case m.SingleplayerUUID != "":
		owner, file = m.SingleplayerUUID, "players/data/"+m.SingleplayerUUID+".dat"
	case m.HasPlayerTag:
		owner = m.PlayerUUID
		if owner != "" {
			file = "playerdata/" + owner + ".dat"
		}
	}
	if owner == "00000000-0000-0000-0000-000000000000" {
		owner, file = "", "" // 迁移时找不到主人 UUID 会写成占位的 UUID，对应不上任何账号
	}
	if m.HasPlayerTag || m.SingleplayerUUID != "" {
		base := "这是单人存档。世界和建筑会保留；单人角色身上的背包、经验、末影箱、位置能不能接上，要看主人用哪个账号进服。"
		var how string
		switch {
		case owner == "":
			how = "存档里没认出主人的 UUID，服务器上没法自动对应。"
		case proxy:
			how = "这台服务器在代理（Velocity / BungeeCord）后面，进服的 UUID 由代理决定：代理开着正版验证时，主人用同一个正版账号进服，一般会读到存档里自己的那份（" + file + "）。"
		case online:
			how = "主人用玩单人时的同一个正版账号进服，服务器会读存档里的 " + file + "（单人最后一次存档时写的），一般就接上了。"
		default:
			how = "这台服务器关着正版验证（离线模式），进服的 UUID 是按名字算出来的，和正版账号的不一样，接不上。想接上：让主人先进一次服再退出，停服后把世界里 " + file + " 复制一份，改名成新生成的那个玩家文件的名字（先把新生成的那个备份），再启动。"
		}
		if m.HasPlayerTag && m.SingleplayerUUID == "" && owner != "" && !c.hasPlayerFile(owner) {
			how = "主人的进度只存在 level.dat 里（存档里没有对应的玩家文件），服务器不会读 level.dat 里的这一份，首版也不会自动转换，所以接不上。"
		}
		is = append(is, Issue{Sev: SevWarn, Code: "singleplayer-owner", Msg: base + how,
			Detail: "主人 UUID：" + owner})
	}
	if c.PlayerFiles > 0 {
		is = append(is, Issue{Sev: SevInfo, Code: "players-kept",
			Msg: fmt.Sprintf("地图带了 %d 个玩家的进度文件，会原样保留，不会和这台服务器原来的玩家文件混在一起；同一个账号进服会接上自己的进度。箱子里的东西属于世界，跟着地图走", c.PlayerFiles)})
	}
	return is
}

// hasPlayerFile 来源里有没有这个 UUID 的玩家文件（扫描时记下的清单里查）。
func (c *Candidate) hasPlayerFile(uuid string) bool {
	for _, u := range c.players {
		if strings.EqualFold(u, uuid) {
			return true
		}
	}
	return false
}

// playNotes 冒险 / 小游戏地图的玩法提示：数据包、资源包、命令。
func playNotes(c *Candidate) []Issue {
	var is []Issue
	if len(c.Datapacks) > 0 {
		is = append(is, Issue{Sev: SevInfo, Code: "datapacks",
			Msg:    fmt.Sprintf("地图自带 %d 个数据包，导入后由游戏按存档里的设置加载，会参与玩法。导入本身不运行包里的任何程序", len(c.Datapacks)),
			Detail: strings.Join(firstN(c.Datapacks, 10), "、")})
	}
	if c.ResourcePak {
		is = append(is, Issue{Sev: SevWarn, Code: "resources-zip",
			Msg: "地图带了资源包（resources.zip）。专用服务器不会把它自动发给玩家：要大家看到正确的材质，请把它放到一个大家都能下载的地址，在设置里填资源包地址，或者让每个人自己安装"})
	}
	v, ok := c.Meta.gameRule("commandBlocksEnabled", "command_blocks_work")
	if !ok {
		v, ok = c.Meta.gameRule("commandBlocksWork", "command_blocks_work")
	}
	if ok && (v == "0" || v == "false") {
		is = append(is, Issue{Sev: SevWarn, Code: "command-blocks-off",
			Msg: "这张地图的游戏规则里关掉了命令方块（command_blocks_work）。如果地图靠命令方块运作，要在游戏里用 /gamerule 打开"})
	} else if c.Meta.AllowCommands {
		is = append(is, Issue{Sev: SevInfo, Code: "commands",
			Msg: "地图允许使用命令。如果地图用到命令方块，要确认服务器允许命令方块运行：老版本在 server.properties 的 enable-command-block，新版本 server.properties 里没有这一项，改由游戏规则管（1.21.11 起叫 command_blocks_work）。MineDeck 不会自动改"})
	}
	if len(c.Readme) > 0 {
		is = append(is, Issue{Sev: SevInfo, Code: "readme",
			Msg: "地图带了说明书，里面可能写着推荐的版本、人数和需要的设置，导入前看一眼"})
	}
	return is
}

func (s *Service) getPlan(owner, id string) (*Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
	if p == nil || p.Owner != owner {
		return nil, fmt.Errorf("找不到这份导入计划，可能已经过期，请重新检测")
	}
	if s.now().After(p.ExpiresAt) {
		return nil, fmt.Errorf("这份导入计划已经过期，请重新检测")
	}
	return p, nil
}

// ---------- 执行：替换已有服务器的地图 ----------

// ExecReq 执行导入的请求。Confirm 必须是 "replace"（界面上点了「导入并替换」）；有要确认的项时 Acknowledge 要为真。
type ExecReq struct {
	PlanID      string   `json:"planId"`
	Confirm     string   `json:"confirm"`
	Acknowledge bool     `json:"acknowledge"`
	Settings    []string `json:"settings"` // 用户勾选要改的设置（计划里 Settings 的 Key）
	StopFirst   bool     `json:"stopFirst"`
}

var importSteps = []string{"核对地图和服务器", "复制地图到暂存区", "停服并占用这台服务器", "换上新地图", "完成"}

// Execute 替换已有服务器的地图，返回任务编号。服务器端在这里核对确认，不靠前端弹窗。
func (s *Service) Execute(owner string, req ExecReq) (string, error) {
	p, err := s.getPlan(owner, req.PlanID)
	if err != nil {
		return "", err
	}
	if p.NewInst {
		return "", fmt.Errorf("这份计划是给新建服务器用的")
	}
	if req.Confirm != "replace" {
		return "", fmt.Errorf("替换地图需要明确确认")
	}
	if p.Blocked {
		return "", fmt.Errorf("这张地图现在不能导入到这台服务器，先看预览里标红的原因")
	}
	if p.NeedConfirm && !req.Acknowledge {
		return "", fmt.Errorf("预览里有需要你确认的项（版本、模组等），确认后再导入")
	}
	if p.MustStop && !req.StopFirst {
		return "", fmt.Errorf("服务器正在运行。请先停止服务器，或者选「停服并导入」")
	}
	props := map[string]string{}
	for _, k := range req.Settings {
		found := false
		for _, sc := range p.Settings {
			if sc.Key == k {
				props[k], found = sc.After, true
			}
		}
		if !found {
			return "", fmt.Errorf("计划里没有「%s」这项设置", k)
		}
	}
	if why := s.M.WorldBusy(p.Inst); why != "" {
		return "", errors.New(why)
	}
	r, err := s.getScan(owner, p.ScanID)
	if err != nil {
		return "", err
	}
	// 计划一次只用一回：再点一次要重新检测。扫描结果钉住：任务进行中放弃检测、过期都不删上传的文件
	s.mu.Lock()
	if s.plans[p.ID] != p {
		s.mu.Unlock()
		return "", fmt.Errorf("这份导入计划已经用过了，请重新检测")
	}
	if r.dropped { // 刚刚过期或被放弃，上传的文件可能已经删了
		s.mu.Unlock()
		return "", fmt.Errorf("这次检测的结果已经过期，请重新选择地图")
	}
	delete(s.plans, p.ID)
	r.pins++
	s.mu.Unlock()
	t, tctx := s.M.Tasks.New(fmt.Sprintf("导入地图到 %s", p.Inst), importSteps)
	t.SetKind("world")
	ctx, cancel := context.WithCancel(tctx)
	s.mu.Lock()
	s.cancels[t.ID()] = cancelEntry{owner: owner, fn: cancel}
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.cancels, t.ID())
			s.mu.Unlock()
			cancel()
			s.unpin(r)
		}()
		res, err := s.runImport(ctx, t, r, p, props, req.StopFirst)
		if err != nil {
			t.Fail(err)
			return
		}
		t.Finish(res)
	}()
	return t.ID(), nil
}

// Cancel 取消一个导入任务：只在复制到暂存区的阶段生效，开始换地图以后会做完（或退回）再结束。
// 只有发起导入的人能取消。
func (s *Service) Cancel(owner, taskID string) bool {
	s.mu.Lock()
	c, ok := s.cancels[taskID]
	s.mu.Unlock()
	if !ok || c.owner != owner {
		return false
	}
	c.fn()
	return true
}

func (s *Service) runImport(ctx context.Context, t *tasks.Task, r *scanResult, p *Plan, props map[string]string, stopFirst bool) (string, error) {
	// 1. 核对：来源、目标都还是预览时的样子
	t.StartStep(0)
	tg, i, err := s.targetOf(p.Inst)
	if err != nil {
		return "", err
	}
	src, err := reopen(r)
	if err != nil {
		return "", fmt.Errorf("来源打不开了：%w", err)
	}
	defer src.Close()
	info, err := rebind(r, src, p.CandidateID)
	if err != nil {
		return "", err
	}
	if fp := r.Fingerprint + "|" + info.fp + "|" + targetFP(tg); fp != p.Fingerprint {
		return "", fmt.Errorf("地图或服务器在预览以后变过了（文件被改动、level-name 或版本改了），没有替换任何文件。请重新检测")
	}
	if tg.LevelName != p.LevelName || string(tg.Core) != p.Core || tg.MC != p.MC {
		return "", fmt.Errorf("服务器的设置在预览以后变过了，没有替换任何文件。请重新检测")
	}
	mp, _ := planMapping(info.c, coreFamily(tg.Core), mustModern(ctx, s.order, tg.MC), tg.LevelName)
	files, dropped, err := resolveMoves(info, mp)
	if err != nil {
		return "", err
	}
	if len(dropped) > 0 {
		t.Logf("这些文件属于旧 Bukkit 布局的服务器专用数据，不带过去：%s", strings.Join(firstN(dropped, 10), "、"))
	}

	// 2. 暂存：服务器照常运行
	t.StartStep(1)
	id := newID("w")
	j := &Journal{ID: id, Inst: p.Inst, State: TxStaging, Created: s.now(), Source: r.Source.Name,
		Candidate: candName(info.c), LevelName: tg.LevelName}
	if err := saveJournal(tg.Dir, j); err != nil {
		return "", fmt.Errorf("没能在服务器文件夹里建暂存区：%w", err)
	}
	defer s.setActive(txAbs(tg.Dir, id))()
	abandon := func(cause error) (string, error) {
		_ = os.RemoveAll(filepath.Join(txAbs(tg.Dir, id), stageSub))
		j.State, j.Err = TxRolledBack, cause.Error()
		_ = saveJournal(tg.Dir, j)
		return "", cause
	}
	stageRoot := filepath.Join(txAbs(tg.Dir, id), stageSub)
	if err := stageFiles(ctx, src, files, stageRoot, t.ProgressFn("复制地图")); err != nil {
		if errors.Is(err, context.Canceled) {
			return abandon(fmt.Errorf("已取消，没有替换任何文件"))
		}
		return abandon(err)
	}
	if after, err := rescan(src, info); err != nil || fingerprint(after) != info.fp {
		return abandon(fmt.Errorf("来源在复制期间变了，没有替换任何文件。请先关掉正在使用这张地图的程序，再重新检测"))
	}
	j.NewLevelSHA = fileSHA(filepath.Join(stageRoot, filepath.FromSlash(tg.LevelName), "level.dat"))
	j.NewBytes = dirBytes(stageRoot)
	for _, n := range worldDirNames(tg.LevelName) {
		if exists(filepath.Join(stageRoot, filepath.FromSlash(n))) {
			j.NewIn = append(j.NewIn, Step{From: txRel(id) + "/" + stageSub + "/" + n, To: n})
		}
	}
	j.State = TxStaged
	if err := saveJournal(tg.Dir, j); err != nil {
		return abandon(err)
	}
	t.Logf("地图已复制到暂存区（%s）", fmtBytes(j.NewBytes))

	// 3. 停服、占位。从这里开始不再响应取消：要么换完，要么退回
	t.StartStep(2)
	if i.Status() != "stopped" {
		if !stopFirst {
			return abandon(fmt.Errorf("服务器正在运行，没有替换任何文件"))
		}
		t.Logf("正在停止服务器（等它自己保存退出，不会强制结束）…")
		if err := s.M.StopAndWait(p.Inst, s.StopTimeout); err != nil {
			if errors.Is(err, inst.ErrStopTimeout) {
				return abandon(fmt.Errorf("服务器在 %v 内没有停下，导入已取消，没有替换任何文件。服务器会继续自己关闭，可以在控制台看情况", s.StopTimeout))
			}
			return abandon(fmt.Errorf("没能停下服务器，导入已取消，没有替换任何文件：%w", err))
		}
	}
	release, err := s.M.BeginWorldChange(p.Inst, "换地图")
	if err != nil {
		return abandon(err)
	}
	defer release()
	// 占上位以后再核对一遍：停服、占位这段时间里没人改过
	tg2, i2, err := s.targetOf(p.Inst)
	if err != nil {
		return abandon(err)
	}
	if tg2.LevelName != tg.LevelName || tg2.Core != tg.Core || tg2.MC != tg.MC {
		return abandon(fmt.Errorf("服务器的设置在导入过程中变了，没有替换任何文件。请重新检测"))
	}
	j.LastExit = i2.LastExit()
	for _, n := range worldDirNames(tg.LevelName) {
		wp := filepath.Join(tg.Dir, filepath.FromSlash(n))
		if !exists(wp) {
			continue
		}
		if held, err := sessionLockHeld(wp); err == nil && held {
			return abandon(fmt.Errorf("「%s」正被别的程序使用（可能是在 MineDeck 之外手动启动的服务器），没有替换任何文件。先关掉它再试", n))
		}
		j.OldOut = append(j.OldOut, Step{From: n, To: txRel(id) + "/" + oldSub + "/" + n})
	}
	j.OldLevelSHA = fileSHA(filepath.Join(tg.Dir, filepath.FromSlash(tg.LevelName), "level.dat"))
	for _, s := range j.OldOut {
		j.OldBytes += dirBytes(filepath.Join(tg.Dir, filepath.FromSlash(s.From)))
	}

	// 4. 换
	t.StartStep(3)
	stuck, err := commitTx(tg.Dir, j, props)
	if err != nil {
		if stuck {
			s.M.SetWorldBlock(p.Inst, "换地图没有完成，自动恢复也失败了，服务器已暂停启动。请到「地图 / 世界」里查看恢复步骤")
			return "", fmt.Errorf("恢复未完成，服务器已暂停启动：%v。所有文件都还在，查看恢复步骤：%s", err, txAbs(tg.Dir, id))
		}
		return "", fmt.Errorf("导入中断，已恢复到导入前的世界：%v", err)
	}
	t.StartStep(4)
	if len(j.OldOut) > 0 {
		t.Logf("导入前的世界留在恢复点里（%s），随时可以在「地图 / 世界」里换回来", fmtBytes(j.OldBytes))
	}
	return "地图文件已导入，尚未启动检查。服务器保持停止，启动后进服看看是不是这张地图", nil
}

func candName(c *Candidate) string {
	if c.Meta.LevelName != "" {
		return c.Meta.LevelName
	}
	return c.FolderName
}

func mustModern(ctx context.Context, order versionOrder, mc string) bool {
	m, _ := isModern(ctx, order, mc)
	return m
}

// rebind 在重新打开的来源上找回候选的文件清单，并重算它的指纹。
func rebind(r *scanResult, src *source, candID string) (*candInfo, error) {
	old := r.cands[candID]
	if old == nil {
		return nil, fmt.Errorf("请选择要导入的地图")
	}
	info := &candInfo{c: old.c, roots: old.roots}
	files, err := rescan(src, info)
	if err != nil {
		return nil, fmt.Errorf("地图来源读不全了，没有替换任何文件。请重新检测：%w", err)
	}
	info.files = files
	info.fp = fingerprint(info.files)
	if info.fp != old.fp {
		return nil, fmt.Errorf("地图在预览以后变过了（文件被改动），没有替换任何文件。请重新检测")
	}
	return info, nil
}

// rescan 按候选的目录重新列一遍文件（文件夹来源会重新读盘上的大小和时间）。重新读不了（文件夹被删、
// 多了联接点、文件太多）就报错：不能拿打开时的旧清单去比，那样永远对得上。
func rescan(src *source, info *candInfo) ([]entry, error) {
	if src.kind == "folder" {
		fresh, err := openFolder(src.root)
		if err != nil {
			return nil, err
		}
		src = fresh
	}
	if len(info.roots) == 1 && info.roots[0] == "" {
		return src.files, nil
	}
	var out []entry
	for _, root := range info.roots {
		out = append(out, src.under(root)...)
	}
	return out, nil
}

// ---------- 新建服务器时放地图 ----------

// CheckCreatePlan 新建服务器的请求带着计划编号时，核对计划是这个人的、是给新建用的、核心和版本对得上、没有被拦下，
// 核对通过就收下这份计划（见 createClaim）：之后它不会过期、不能再用第二次，上传的文件也不会被删，
// 直到新建任务结束时 ReleaseCreate。建服请求没能开始时（比如重名），调用方也要 ReleaseCreate。
func (s *Service) CheckCreatePlan(owner, planID string, core mcsrc.Core, mc string, ack bool) error {
	p, err := s.getPlan(owner, planID)
	if err != nil {
		return err
	}
	if err := checkCreate(p, core, mc, ack); err != nil {
		return err
	}
	r, err := s.getScan(owner, p.ScanID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans[planID] != p || s.claims[planID] != nil {
		return fmt.Errorf("这份导入计划已经用过了，请重新检测")
	}
	if r.dropped { // 刚刚过期或被放弃，上传的文件可能已经删了
		return fmt.Errorf("这次检测的结果已经过期，请重新选择地图")
	}
	delete(s.plans, planID)
	r.pins++
	s.claims[planID] = &createClaim{owner: owner, p: p, r: r, until: s.now().Add(claimTTL)}
	return nil
}

// ReleaseCreate 新建任务结束（或没能开始）：放掉收下的计划和钉住的扫描结果。还没开始放地图就结束的
// （重名、下载失败这类），计划没过期就放回去，改一改再建不用重新检测。
func (s *Service) ReleaseCreate(owner, planID string) {
	s.mu.Lock()
	c := s.claims[planID]
	if c == nil || c.owner != owner {
		s.mu.Unlock()
		return
	}
	delete(s.claims, planID)
	if !c.used && !s.now().After(c.p.ExpiresAt) && !c.r.dropped {
		s.plans[planID] = c.p
	}
	s.mu.Unlock()
	s.unpin(c.r)
}

func checkCreate(p *Plan, core mcsrc.Core, mc string, ack bool) error {
	if !p.NewInst {
		return fmt.Errorf("这份计划不是给新建服务器用的")
	}
	if p.Core != string(core) || p.MC != mc {
		return fmt.Errorf("选的核心或版本和检测地图时不一样了，请重新检测")
	}
	if p.Blocked {
		return fmt.Errorf("这张地图不能用这个核心和版本开服，先看预览里标红的原因")
	}
	if p.NeedConfirm && !ack {
		return fmt.Errorf("预览里有需要你确认的项（版本、模组等），确认后再开服")
	}
	return nil
}

// placeForCreate 新建流水线在登记实例之前调：把计划里的地图放进新服务器的目录（level-name 是 world）。
// 失败时整台服务器都不建（流水线会删掉目录），不会留下一台启动后随机生成新世界的实例。
func (s *Service) placeForCreate(ctx context.Context, t *tasks.Task, owner, planID, dir string) error {
	s.mu.Lock()
	c := s.claims[planID]
	if c != nil && c.owner == owner {
		c.used = true
	}
	s.mu.Unlock()
	if c == nil || c.owner != owner {
		return fmt.Errorf("找不到这份导入计划，请重新检测")
	}
	p, r := c.p, c.r
	src, err := reopen(r)
	if err != nil {
		return fmt.Errorf("地图来源打不开了：%w", err)
	}
	defer src.Close()
	info, err := rebind(r, src, p.CandidateID)
	if err != nil {
		return err
	}
	level := inst.LevelName(dir)
	mp, _ := planMapping(info.c, coreFamily(mcsrc.Core(p.Core)), mustModern(ctx, s.order, p.MC), level)
	files, _, err := resolveMoves(info, mp)
	if err != nil {
		return err
	}
	id := newID("w")
	j := &Journal{ID: id, Inst: p.Inst, State: TxStaging, Created: s.now(), Source: r.Source.Name, Candidate: candName(info.c), LevelName: level}
	if err := saveJournal(dir, j); err != nil {
		return err
	}
	stageRoot := filepath.Join(txAbs(dir, id), stageSub)
	t.Logf("复制地图「%s」…", j.Candidate)
	if err := stageFiles(ctx, src, files, stageRoot, t.ProgressFn("复制地图")); err != nil {
		return err
	}
	if after, err := rescan(src, info); err != nil || fingerprint(after) != info.fp {
		return fmt.Errorf("地图来源在复制期间变了。请先关掉正在使用这张地图的程序，再重新检测")
	}
	j.NewLevelSHA = fileSHA(filepath.Join(stageRoot, filepath.FromSlash(level), "level.dat"))
	for _, n := range worldDirNames(level) {
		if exists(filepath.Join(stageRoot, filepath.FromSlash(n))) {
			if exists(filepath.Join(dir, filepath.FromSlash(n))) {
				return fmt.Errorf("新服务器的文件夹里已经有「%s」了", n)
			}
			j.NewIn = append(j.NewIn, Step{From: txRel(id) + "/" + stageSub + "/" + n, To: n})
		}
	}
	if _, err := commitTx(dir, j, nil); err != nil {
		return err
	}
	t.Logf("地图已放进新服务器")
	return nil
}

// ---------- 恢复点、换回旧图、启动时恢复 ----------

// RestorePoint 一次导入留下的记录（界面「地图 / 世界」页列出）。
type RestorePoint struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Created   time.Time `json:"created"`
	Source    string    `json:"source"`
	Candidate string    `json:"candidate"`
	LevelName string    `json:"levelName"`
	HasOld    bool      `json:"hasOld"`   // 恢复点里有导入前的世界（能换回）
	OldBytes  int64     `json:"oldBytes"` // 恢复点占的空间
	KeptBytes int64     `json:"keptBytes"`
	Err       string    `json:"error,omitempty"`
	Stuck     []string  `json:"stuck,omitempty"`
	// CanReturn 现在能点「恢复导入前的地图」：恢复点里有旧世界，世界名和现在一样，
	// 并且是这个世界最近的一次导入（先换回最近的，才能再往前换；乱序换回会把中间那张图困在恢复点里）
	CanReturn bool `json:"canReturn"`
}

// WorldStatus 「地图 / 世界」页要的东西。
type WorldStatus struct {
	LevelName string         `json:"levelName"`
	Status    string         `json:"status"`
	Busy      string         `json:"busy,omitempty"`
	Points    []RestorePoint `json:"points"`
	Proxy     bool           `json:"proxy"`
}

// Status 一台服务器的世界状态和恢复点。
func (s *Service) Status(name string) (*WorldStatus, error) {
	i, err := s.M.Get(name)
	if err != nil {
		return nil, err
	}
	ws := &WorldStatus{LevelName: inst.LevelName(i.Dir), Status: i.Status(), Busy: s.M.WorldBusy(name), Points: []RestorePoint{},
		Proxy: mcsrc.KindOf(i.Core) == mcsrc.KindProxy}
	good, bad := listJournals(i.Dir)
	latest := latestCommitted(good, ws.LevelName)
	for _, j := range good {
		rp := RestorePoint{ID: j.ID, State: j.State, Created: j.Created, Source: j.Source, Candidate: j.Candidate, LevelName: j.LevelName,
			Err: j.Err, Stuck: j.Stuck}
		root := txAbs(i.Dir, j.ID)
		rp.OldBytes = dirBytes(filepath.Join(root, oldSub))
		rp.KeptBytes = dirBytes(filepath.Join(root, restoreSub))
		rp.HasOld = j.State == TxCommitted && len(j.OldOut) > 0 && rp.OldBytes > 0
		rp.CanReturn = rp.HasOld && j == latest
		ws.Points = append(ws.Points, rp)
	}
	// 读不出来的记录也列出来：它可能管着一份移到一半的世界，不能当它不存在
	for _, b := range bad {
		ws.Points = append(ws.Points, RestorePoint{ID: b.id, State: TxUnreadable, Err: noPath(b.err),
			Stuck:    []string{txAbs(i.Dir, b.id)},
			OldBytes: dirBytes(filepath.Join(txAbs(i.Dir, b.id), oldSub))})
	}
	return ws, nil
}

// latestCommitted 这个世界名最近一次已提交的导入（只有它能换回）。js 新的在前。
func latestCommitted(js []*Journal, level string) *Journal {
	for _, j := range js {
		if j.State == TxCommitted && strings.EqualFold(j.LevelName, level) {
			return j
		}
	}
	return nil
}

type badJournal struct {
	id  string
	err error
}

// listJournals 一个实例的全部换地图记录（新的在前），以及读不出来的记录（文件坏了、内容对不上）。
// 没有 journal.json 的目录不算：记录是建暂存区之前头一个写下的，没有它就还没开始。
func listJournals(dir string) ([]*Journal, []badJournal) {
	ents, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(inst.WorldTxDir)))
	if err != nil {
		return nil, nil
	}
	var out []*Journal
	var bad []badJournal
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		j, err := loadJournal(dir, e.Name())
		switch {
		case err == nil:
			out = append(out, j)
		case errors.Is(err, os.ErrNotExist):
		default:
			bad = append(bad, badJournal{id: e.Name(), err: err})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	sort.Slice(bad, func(a, b int) bool { return bad[a].id < bad[b].id })
	return out, bad
}

// Restore 换回导入前的地图，返回任务编号。导入以后服务器可能已经启动过、改写过导入的世界：那一份会留在旁边，不删。
func (s *Service) Restore(name, txID, confirm string) (string, error) {
	if confirm != "restore" {
		return "", fmt.Errorf("换回旧地图需要明确确认")
	}
	i, err := s.M.Get(name)
	if err != nil {
		return "", err
	}
	j, err := loadJournal(i.Dir, txID)
	if err != nil {
		return "", fmt.Errorf("找不到这次导入的记录")
	}
	if err := restorable(i.Dir, j); err != nil {
		return "", err
	}
	t, _ := s.M.Tasks.New(fmt.Sprintf("把 %s 换回导入前的地图", name), []string{"占用这台服务器", "换回旧地图", "核对"})
	t.SetKind("world")
	go func() {
		t.StartStep(0)
		release, err := s.M.BeginWorldChange(name, "换回旧地图")
		if err != nil {
			t.Fail(err)
			return
		}
		defer release()
		// 占上位以后再看一遍：等位的这段时间里，别的换回可能刚做完或者退回了
		if j, err = loadJournal(i.Dir, txID); err == nil {
			err = restorable(i.Dir, j)
		}
		if err != nil {
			t.Fail(err)
			return
		}
		t.StartStep(1)
		stuck, err := restoreTx(i.Dir, j)
		if err != nil {
			if stuck {
				s.M.SetWorldBlock(name, "换回旧地图没有完成，自动恢复也失败了，服务器已暂停启动。请到「地图 / 世界」里查看恢复步骤")
				t.Fail(fmt.Errorf("恢复未完成，服务器已暂停启动：%v", err))
				return
			}
			t.Fail(fmt.Errorf("没能换回旧地图，导入的地图还在原位：%v", err))
			return
		}
		t.StartStep(2)
		checked := "" // 只有导入时旧世界有 level.dat 才核对过
		if j.OldLevelSHA != "" {
			checked = "，旧地图的 level.dat 核对一致"
		}
		t.Finish("已经换回导入前的地图" + checked + "。导入的那份（包括启动后的改动）留在旁边，确认不要了可以删掉")
	}()
	return t.ID(), nil
}

// restorable 这次导入现在能不能换回：已提交、导入前有旧世界、世界名没改过，并且是这个世界最近的一次导入。
func restorable(dir string, j *Journal) error {
	if j.State != TxCommitted {
		return fmt.Errorf("这次导入现在不能换回（状态：%s）", j.State)
	}
	if len(j.OldOut) == 0 {
		return fmt.Errorf("这次导入之前服务器还没有世界，没有可以换回的旧地图")
	}
	if cur := inst.LevelName(dir); !strings.EqualFold(cur, j.LevelName) {
		return fmt.Errorf("导入以后 level-name 改过了（现在是 %s，导入时是 %s），没法自动换回。先把 level-name 改回去", cur, j.LevelName)
	}
	js, _ := listJournals(dir)
	if l := latestCommitted(js, j.LevelName); l == nil || l.ID != j.ID {
		return fmt.Errorf("这之后又导入过别的地图。请先换回最近那一次导入前的地图，再往前换")
	}
	return nil
}

// DeletePoint 删掉一次导入留下的东西（恢复点、换回时留在旁边的世界）。删了就不能再换回。
func (s *Service) DeletePoint(name, txID, confirm string) error {
	if confirm != "delete" {
		return fmt.Errorf("删除恢复点需要明确确认")
	}
	i, err := s.M.Get(name)
	if err != nil {
		return err
	}
	if why := s.M.WorldBusy(name); why != "" {
		return errors.New(why)
	}
	j, err := loadJournal(i.Dir, txID)
	if err != nil {
		return fmt.Errorf("找不到这次导入的记录")
	}
	switch j.State {
	case TxCommitted, TxRolledBack, TxRestored:
	default:
		return fmt.Errorf("这次导入还没处理完（状态：%s），现在不能删", j.State)
	}
	release, err := s.M.BeginWorldChange(name, "删除恢复点")
	if err != nil {
		return err
	}
	defer release()
	return os.RemoveAll(txAbs(i.Dir, txID))
}

// RecoverAll 程序启动时调（在任何导入开始之前）：处理每台服务器没走完的换地图记录，处理好了解除启动封锁；
// 暂存到一半就退出的（没碰目标世界，也没封锁启动），顺手把暂存区清掉。
func (s *Service) RecoverAll() {
	blocked := s.M.WorldBlocked()
	for _, i := range s.M.List() {
		_, isBlocked := blocked[i.Name]
		s.recoverInstance(i, isBlocked)
	}
}

// recoverInstance 处理一台服务器的记录。withCommit 为假时只清暂存到一半的（这台没有封锁，说明没有切换到一半的）。
func (s *Service) recoverInstance(i *inst.Instance, withCommit bool) {
	ok := true
	js, bad := listJournals(i.Dir)
	for _, b := range bad {
		if withCommit {
			i.Console.Append("[MineDeck] 有一份换地图的记录读不出来（" + b.err.Error() + "），为了不在半成品的世界上启动，服务器已暂停启动。所有文件都还在：" + txAbs(i.Dir, b.id))
		}
	}
	for _, j := range js {
		switch j.State {
		case TxCommitted, TxRolledBack, TxRestored:
			continue
		case TxStaging, TxStaged:
			if s.isActive(txAbs(i.Dir, j.ID)) { // 正在进行的导入（重试恢复时可能碰上），不是上次留下的
				continue
			}
		default:
			if !withCommit {
				continue
			}
		}
		if err := recoverTx(i.Dir, j); err != nil || j.State == TxStuck {
			ok = false
			i.Console.Append("[MineDeck] 上次换地图没有完成，自动恢复失败：" + errString(err) + "。服务器已暂停启动，所有文件都还在：" + txAbs(i.Dir, j.ID))
			continue
		}
		if j.State == TxRolledBack && withCommit {
			i.Console.Append("[MineDeck] 上次换地图没有完成，已经恢复到之前的世界")
		}
	}
	if !withCommit {
		return
	}
	if !ok {
		s.M.SetWorldBlock(i.Name, "上次换地图没能自动恢复，服务器已暂停启动。请到「地图 / 世界」里查看恢复步骤")
		return
	}
	// 处理完再按盘上的记录看一遍（和加载实例时设封锁的是同一个判断）：读不出来的、还没走完的都还在，就不解除
	s.M.SetWorldBlock(i.Name, inst.PendingWorldTx(i.Dir))
}

// Retry 对卡住的记录再试一次自动恢复（界面「重试恢复」）。只处理这一台：别的服务器上可能正有导入在暂存。
func (s *Service) Retry(name string) error {
	i, err := s.M.Get(name)
	if err != nil {
		return err
	}
	if i.Status() != "stopped" {
		return fmt.Errorf("请先停止服务器")
	}
	release, err := s.M.BeginWorldChange(name, "恢复地图")
	if err != nil {
		return err
	}
	s.recoverInstance(i, true)
	release()
	if why := s.M.WorldBusy(name); why != "" {
		return errors.New(why)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return "状态异常"
	}
	return err.Error()
}

// SaveUpload 把上传流写进上传目录（流式，不过系统临时目录），超过 limit 就停。返回落盘的路径。
func SaveUpload(r io.Reader, limit int64) (string, error) {
	dir := UploadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, newID("u")+".upload")
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	n, cerr := io.Copy(f, io.LimitReader(r, limit+1))
	if werr := f.Close(); cerr == nil {
		cerr = werr
	}
	if cerr == nil && n > limit {
		cerr = ErrUploadTooLarge
	}
	if cerr != nil {
		os.Remove(dest)
		return "", diskErr(cerr)
	}
	return dest, nil
}

// ErrUploadTooLarge 上传超过上限。
var ErrUploadTooLarge = errors.New("上传的文件太大")

// sameOrInside a 是不是 b 本身或者在 b 里面（按真实路径、不分大小写比）。
func sameOrInside(a, b string) bool {
	ra, rb := realOr(a), realOr(b)
	rel, err := filepath.Rel(rb, ra)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func realOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return strings.ToLower(filepath.Clean(p))
}

// noPath 错误说明里不带本机路径（读文件的错误会带上完整路径；「地图 / 世界」页也给局域网里的人看）。
func noPath(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return "记录文件打不开：" + pe.Err.Error()
	}
	return err.Error()
}
