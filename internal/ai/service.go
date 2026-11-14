package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// KeyStore Key 的存放处（Windows 上是 secret.Store：DPAPI 加密存进 secrets.json）。Key 不落盘明文，
// 不进 settings.json，接口也不下发，只告诉「是否已设置」。
type KeyStore interface {
	Get(name string) (string, error)
	Set(name, plain string) error
	State(name string) (set, unreadable bool)
}

// KeyName AI 的 Key 在 secrets.json 里的名字（cf.key 是 CurseForge 的）。
const KeyName = "ai.key"

// ErrDisabled 没配置 AI：AI 部分不初始化，不联网，不取价格（§4.5）。
var ErrDisabled = errors.New("AI 未启用：先在「设置 → AI」里选择服务商，填好 Key 和模型")

// Options Service 的依赖。
type Options struct {
	Dir      string                // 数据目录，一般是 <程序目录>/data/ai
	Keys     KeyStore              // Key 的存放处
	HTTP     *http.Client          // nil 用 DefaultHTTPClient（测试里换成一拨号就报错的）
	Mirrors  func(string) []string // GitHub 加速镜像（dl.GHMirrors）；nil 只直连
	PriceURL string                // 价格文件地址，默认 PriceURL
	Now      func() time.Time
}

// Settings 存在 <数据目录>/settings.json 的 AI 设置（不含 Key）。
type Settings struct {
	Preset    string   `json:"preset,omitempty"` // 空 = 没配置
	Protocol  Protocol `json:"protocol,omitempty"`
	BaseURL   string   `json:"baseUrl,omitempty"`
	Model     string   `json:"model,omitempty"`
	MaxOutput int      `json:"maxOutput,omitempty"` // 模型列表给的输出上限（Claude 的 max_tokens 不超过它）
	Effort    string   `json:"effort,omitempty"`    // Claude 的思考深度；空 = 模型默认（claude-opus-5-5 是 medium）
	// Claude 走中转时这两项默认不开，测试连接时试过、能用才打开；官方地址默认开
	Eager    *bool `json:"eager,omitempty"`
	Fallback *bool `json:"fallback,omitempty"`

	Quota  map[Feature]int64 `json:"quota,omitempty"` // 每日 token 上限，0 或没有 = 不限
	Prices []CustomPrice     `json:"prices,omitempty"`

	// LANAssistant 局域网登录的设备能不能用应用内助手（§10.3）；没有 = 默认开
	LANAssistant *bool `json:"lanAssistant,omitempty"`

	// 只对自定义服务商有意义：鉴权头、思考内容回传。空 = 默认（鉴权按协议的默认，回传是自动）
	Auth string `json:"auth,omitempty"`
	Echo string `json:"echo,omitempty"`
}

// CustomPrice 用户自填的单价（每百万 token），优先于公开数据，也能补上查不到的模型。
type CustomPrice struct {
	Model      string   `json:"model"`
	Currency   string   `json:"currency"` // CNY / USD
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cacheRead,omitempty"`
	CacheWrite *float64 `json:"cacheWrite,omitempty"`
}

// Service AI 接入层的门面：设置、Key、客户端、测试连接、模型列表、价格、记账和额度。
type Service struct {
	dir    string
	keys   KeyStore
	http   *http.Client
	now    func() time.Time
	prices *PriceBook
	ledger *Ledger

	mu sync.Mutex
	st Settings

	// noEcho 已经知道不认 reasoning_content 的「预设 + 地址 + 模型」（auto 被拒过一次）。助手每一轮都新建客户端，
	// 记在客户端上的结论只活一轮；记在这里，之后的请求直接不带，不用每一轮都先吃一个 400。只在内存里，重启后重新试一次。
	echoMu sync.Mutex
	noEcho map[string]bool

	retryDelay time.Duration // 测试里调小
}

// NewService 读设置，不联网。
func NewService(o Options) *Service {
	if o.HTTP == nil {
		o.HTTP = DefaultHTTPClient()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PriceURL == "" {
		o.PriceURL = PriceURL
	}
	urls := []string{o.PriceURL}
	if o.Mirrors != nil {
		urls = o.Mirrors(o.PriceURL)
	}
	s := &Service{dir: o.Dir, keys: o.Keys, http: o.HTTP, now: o.Now,
		prices: newPriceBook(filepath.Join(o.Dir, "prices.json"), o.HTTP, urls, o.Now),
		ledger: newLedger(filepath.Join(o.Dir, "usage.json"), o.Now)}
	if b, err := os.ReadFile(s.settingsPath()); err == nil {
		var st Settings
		if json.Unmarshal(b, &st) == nil {
			s.st = sanitize(st)
		}
	}
	return s
}

func (s *Service) settingsPath() string { return filepath.Join(s.dir, "settings.json") }

// protocolAllowed 非自定义预设：协议要么是默认的，要么是当前地址那一条变体声明的（MiMo 的 /anthropic）。
func protocolAllowed(p Preset, st Settings) bool {
	if st.Protocol == p.Protocol {
		return true
	}
	ep, ok := p.matchURL(st.BaseURL)
	return ok && ep.Protocol == st.Protocol
}

// sanitize 读回来的设置不合规的部分当作没配置（文件可能被手改过）。
func sanitize(st Settings) Settings {
	p, ok := PresetByID(st.Preset)
	if !ok || !st.Protocol.valid() || (p.ID != CustomID && !protocolAllowed(p, st)) {
		st.Preset, st.Protocol, st.BaseURL, st.Model = "", "", "", ""
	}
	if !AuthMode(st.Auth).valid() {
		st.Auth = ""
	}
	if !EchoMode(st.Echo).valid() {
		st.Echo = ""
	}
	if st.Preset != CustomID {
		st.Auth, st.Echo = "", ""
	}
	if _, err := ParseBaseURL(st.BaseURL); err != nil {
		st.BaseURL = ""
	}
	if !validEffort(st.Effort) {
		st.Effort = ""
	}
	q := map[Feature]int64{}
	for _, f := range QuotaFeatures {
		if v := st.Quota[f]; v > 0 {
			q[f] = v
		}
	}
	st.Quota = q
	var prices []CustomPrice
	for _, cp := range st.Prices {
		if checkCustomPrice(cp) == nil {
			prices = append(prices, cp)
		}
	}
	st.Prices = prices
	return st
}

func (s *Service) saveLocked() error { return s.save(s.st) }

// save 把 st 写进 settings.json（先写临时文件再改名），不动内存里的设置。调用时持有 s.mu。
func (s *Service) save(st Settings) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(st); err != nil {
		return err
	}
	return writeFileAtomic(s.settingsPath(), buf.Bytes())
}

func (s *Service) keyState() (set, broken bool) {
	if s.keys == nil {
		return false, false
	}
	return s.keys.State(KeyName)
}

// missing 还差什么没配好；空串表示已启用。
func (s *Service) missing(st Settings) string { return missingWith(st, s.keyState) }

// missingWithKey 同 missing，Key 用调用方手里那个来判断：Client、Test 用 current 和设置一起取到的 Key，
// 不另去问 Key 现在的状态——换服务商的保存可能正好夹在中间，问到的是换过以后的那个，和手里的设置配不上对。
func missingWithKey(st Settings, key string) string {
	return missingWith(st, func() (bool, bool) { return key != "", false }) // 解不开的 Key 取出来就是空的，按没填算
}

func missingWith(st Settings, keyState func() (set, broken bool)) string {
	p, ok := PresetByID(st.Preset)
	switch {
	case !ok:
		return "选择服务商"
	case st.BaseURL == "":
		return "填写接口地址"
	}
	if !p.NoKey && p.ID != CustomID {
		if set, broken := keyState(); broken {
			return "重新填写 Key（原来的 Key 解不开，可能换了电脑或 Windows 用户）"
		} else if !set {
			return "填写 Key"
		}
	}
	if st.Model == "" {
		return "选择模型"
	}
	return ""
}

func (s *Service) snapshot() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// current 当前的设置和与它配对的 Key，在锁里一起取：换服务商的那次保存也一直拿着这把锁（见 commitLocked），
// 所以不会先拿到旧地址、等那边换完了再拿到新 Key（或者反过来）。要发请求的都从这里取。
func (s *Service) current() (Settings, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(), s.key()
}

func (s *Service) snapshotLocked() Settings {
	st := s.st
	st.Quota = map[Feature]int64{}
	for k, v := range s.st.Quota {
		st.Quota[k] = v
	}
	st.Prices = append([]CustomPrice(nil), s.st.Prices...)
	return st
}

// Enabled 是否已配置好（选了服务商、填了 Key 和模型）。
func (s *Service) Enabled() bool { return s.missing(s.snapshot()) == "" }

// LANAssistant 局域网登录的设备能不能用应用内助手（设置里的开关，默认开）。
func (s *Service) LANAssistant() bool { return boolOr(s.snapshot().LANAssistant, true) }

// SetLANAssistant 打开或关闭「局域网可用 AI 助手」。
func (s *Service) SetLANAssistant(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.LANAssistant = &on
	return s.saveLocked()
}

// Current 当前的服务商、协议、地址和模型（会话据此判断设置是不是改过了：改了就要开新会话）。
type Current struct {
	Preset   string   `json:"preset"`
	Protocol Protocol `json:"protocol"`
	BaseURL  string   `json:"baseUrl"`
	Model    string   `json:"model"`
}

// Current 见 Current 类型；没配置时为空。
func (s *Service) Current() Current {
	st := s.snapshot()
	if s.missing(st) != "" {
		return Current{}
	}
	return Current{Preset: st.Preset, Protocol: st.Protocol, BaseURL: st.BaseURL, Model: st.Model}
}

// Usage 某项功能今天的用量和每日额度（0 表示不限）。不联网。
func (s *Service) Usage(f Feature) (today Totals, limit int64) {
	return s.ledger.Today(f), s.snapshot().Quota[f]
}

// paid 配置好了、而且不是本机模型：只有这时才去取价格数据（只配了 Ollama 的不取）。
func (s *Service) paid(st Settings) bool {
	p, _ := PresetByID(st.Preset)
	return s.missing(st) == "" && !p.Local
}

// Start 程序启动时调用：配置了要花钱的服务商，才在后台检查价格数据（缓存超过 7 天才真的去下载）。
// 没配置 AI 时什么也不做，不联网。
func (s *Service) Start() {
	if s.paid(s.snapshot()) {
		s.prices.MaybeRefresh()
	}
}

func validEffort(e string) bool {
	switch e {
	case "", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

func boolOr(p *bool, d bool) bool {
	if p == nil {
		return d
	}
	return *p
}

// config 按设置和预设生成客户端配置。
func (s *Service) config(st Settings, key string, stream bool) Config {
	p, _ := PresetByID(st.Preset)
	// 鉴权留空就是「这个协议的默认」，不要在这里补成 bearer：Claude 协议的默认是 x-api-key（中转另带 Bearer），
	// 补成 bearer 会让官方预设和 Claude 中转都只发 Bearer。
	auth, echo := p.Auth, p.Echo
	if p.ID == CustomID {
		if AuthMode(st.Auth).valid() {
			auth = AuthMode(st.Auth)
		}
		if EchoMode(st.Echo).valid() && st.Echo != "" {
			echo = EchoMode(st.Echo)
		}
	}
	if echo == "" {
		echo = EchoOff
	}
	cfg := Config{Protocol: st.Protocol, BaseURL: st.BaseURL, Key: key, Model: st.Model, Stream: stream,
		HTTP: s.http, RetryDelay: s.retryDelay, Auth: auth, Echo: echo, EchoPadAll: p.echoPadAll, ToolResultName: p.toolResultName}
	if echo == EchoAuto {
		k := strings.Join([]string{st.Preset, string(st.Protocol), strings.TrimRight(strings.TrimSpace(st.BaseURL), "/"), st.Model}, "\n")
		s.echoMu.Lock()
		cfg.DropReasoning = s.noEcho[k]
		s.echoMu.Unlock()
		cfg.OnDropReasoning = func() {
			s.echoMu.Lock()
			if s.noEcho == nil {
				s.noEcho = map[string]bool{}
			}
			s.noEcho[k] = true
			s.echoMu.Unlock()
		}
	}
	if st.Protocol == Anthropic {
		off := officialClaude(st.BaseURL)
		cfg.Official = off
		cfg.Eager = boolOr(st.Eager, off)
		cfg.Fallback = boolOr(st.Fallback, off)
		cfg.Effort = st.Effort
		cfg.MaxTokens = 16000
		if st.MaxOutput > 0 && st.MaxOutput < cfg.MaxTokens {
			cfg.MaxTokens = st.MaxOutput
		}
	}
	return cfg
}

func (s *Service) key() string {
	if s.keys == nil {
		return ""
	}
	k, err := s.keys.Get(KeyName)
	if err != nil {
		return ""
	}
	return k
}

// Client 按当前设置建一个客户端（第三、四阶段用）。没配置 AI 返回 ErrDisabled。
func (s *Service) Client(stream bool) (*Client, error) {
	st, key := s.current()
	if missingWithKey(st, key) != "" {
		return nil, ErrDisabled
	}
	return NewClient(s.config(st, key, stream))
}

// Begin 用 AI 之前先调：没配置报 ErrDisabled，今天的额度用完了报 *QuotaError。
func (s *Service) Begin(f Feature) error {
	st := s.snapshot()
	if s.missing(st) != "" {
		return ErrDisabled
	}
	return s.ledger.Check(f, st.Quota[f])
}

// Record 记一次请求的用量（不管成没成功，花了的都要记），返回估算的费用（查不到价格时为 nil，界面上只显示 token）。
func (s *Service) Record(f Feature, r *Result) *Cost {
	if r == nil || !f.valid() {
		return nil
	}
	st := s.snapshot()
	c := s.estimate(st, r)
	s.ledger.Add(f, r.Usage, c)
	if s.paid(st) {
		s.prices.MaybeRefresh() // 程序开着超过 7 天时在这里刷新
	}
	return c
}

// estimate 估算一次请求的费用。Claude 发生服务端回退时，每次尝试按各自模型的价格算。
func (s *Service) estimate(st Settings, r *Result) *Cost {
	p, ok := PresetByID(st.Preset)
	if !ok {
		return nil
	}
	if p.Local {
		return &Cost{Currency: "USD", Source: "local"} // 本机模型不花钱
	}
	if len(r.Attempts) > 0 {
		var total *Cost
		for _, a := range r.Attempts {
			c := s.costFor(st, p, []string{a.Model}, a.Usage)
			if c == nil || (total != nil && total.Currency != c.Currency) {
				return nil
			}
			if total == nil {
				total = c
			} else {
				total.Amount += c.Amount
				if c.Source != total.Source {
					total.Source = "mixed"
				}
			}
		}
		return total
	}
	return s.costFor(st, p, []string{st.Model, r.Model}, r.Usage)
}

// costFor 依次用候选的模型名找单价：自填的优先，然后是公开数据。
func (s *Service) costFor(st Settings, p Preset, models []string, u Usage) *Cost {
	for _, m := range models {
		if m == "" {
			continue
		}
		for _, cp := range st.Prices {
			if cp.Model == m {
				return &Cost{Amount: cp.price().Cost(u), Currency: cp.Currency, Source: "custom"}
			}
		}
	}
	for _, m := range models {
		if m == "" {
			continue
		}
		if pr, ok := s.prices.Lookup(p.litellm, m); ok {
			return &Cost{Amount: pr.Cost(u), Currency: "USD", Source: "litellm"}
		}
	}
	return nil
}

func (cp CustomPrice) price() Price {
	per := func(v float64) float64 { return v / 1e6 }
	p := Price{Input: per(cp.Input), Output: per(cp.Output)}
	if cp.CacheRead != nil {
		v := per(*cp.CacheRead)
		p.CacheRead = &v
	}
	if cp.CacheWrite != nil {
		v := per(*cp.CacheWrite)
		p.CacheWrite = &v
	}
	return p
}

func checkCustomPrice(cp CustomPrice) error {
	if m := strings.TrimSpace(cp.Model); m == "" || m != cp.Model || len([]rune(m)) > 200 || hasControl(m) {
		return errors.New("模型名不对（不能为空，前后不能有空格，最长 200 个字）")
	}
	if cp.Currency != "CNY" && cp.Currency != "USD" {
		return errors.New("币种只能是 CNY（元）或 USD（美元）")
	}
	ok := func(v float64) bool { return v >= 0 && v <= 100000 }
	if !ok(cp.Input) || !ok(cp.Output) || (cp.CacheRead != nil && !ok(*cp.CacheRead)) || (cp.CacheWrite != nil && !ok(*cp.CacheWrite)) {
		return errors.New("单价要在 0 到 100000 之间（每百万 token）")
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// ---------- 设置接口 ----------

// ProviderPatch 修改服务商设置：只改传了的字段（指针为 nil 表示没传）。
// Key 只写：非空保存，空串清除，不传不动。换了服务商、或者接口地址换到了别的网站，旧 Key 自动清掉、必须重填（§4.5：
// 防止有人把地址改成自己的服务器，把 Key 骗过去）；同一个网站只改路径或协议，Key 照旧（见 SameSite）。
type ProviderPatch struct {
	Preset    *string   `json:"preset"`
	Protocol  *Protocol `json:"protocol"`
	BaseURL   *string   `json:"baseUrl"`
	Model     *string   `json:"model"`
	MaxOutput *int      `json:"maxOutput"`
	Effort    *string   `json:"effort"`
	Key       *string   `json:"key"`
	Auth      *string   `json:"auth"`
	Echo      *string   `json:"echo"`
}

// SetProvider 保存服务商设置。preset 传空串表示停用 AI（同时清掉 Key）。
// 新设置和与它配对的 Key 都存好了才生效，中途写盘失败就退回原样（见 commitLocked）。
func (s *Service) SetProvider(pt ProviderPatch) error {
	if err := s.setProvider(pt); err != nil {
		return err
	}
	if s.paid(s.snapshot()) {
		s.prices.MaybeRefresh()
	}
	return nil
}

func (s *Service) setProvider(pt ProviderPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.st
	st := s.st
	if pt.Preset != nil {
		st.Preset = *pt.Preset
	} else if st.Preset == "" {
		return errors.New("先选择服务商")
	}
	if st.Preset == "" { // 停用：设置和 Key 一起清掉
		st.Preset, st.Protocol, st.BaseURL, st.Model, st.MaxOutput, st.Effort, st.Eager, st.Fallback = "", "", "", "", 0, "", nil, nil
		st.Auth, st.Echo = "", ""
		empty := ""
		return s.commitLocked(st, &empty)
	}
	p, ok := PresetByID(st.Preset)
	if !ok {
		return fmt.Errorf("未知的服务商：%q", st.Preset)
	}
	changedPreset := st.Preset != old.Preset
	switch {
	case p.ID != CustomID:
		// 协议等地址定下来再定：同一预设的不同地址可能是另一种协议（MiMo 的 /anthropic）
	case pt.Protocol != nil:
		st.Protocol = *pt.Protocol
	case !st.Protocol.valid() || changedPreset:
		st.Protocol = OpenAI
	}
	if pt.BaseURL != nil {
		st.BaseURL = strings.TrimSpace(*pt.BaseURL)
	} else if changedPreset {
		st.BaseURL = p.BaseURL
	}
	if st.BaseURL == "" && p.ID != CustomID {
		st.BaseURL = p.BaseURL
	}
	if _, err := ParseBaseURL(st.BaseURL); err != nil {
		return err
	}
	if p.ID != CustomID {
		if ep, ok := p.matchURL(st.BaseURL); ok {
			st.Protocol = ep.Protocol
		} else {
			st.Protocol = p.Protocol
		}
		st.Auth, st.Echo = "", ""
	} else {
		if pt.Auth != nil {
			st.Auth = *pt.Auth
		} else if changedPreset {
			st.Auth = string(p.Auth)
		}
		if pt.Echo != nil {
			st.Echo = *pt.Echo
		} else if changedPreset {
			st.Echo = string(p.Echo)
		}
		// 鉴权留空 = 按协议的默认（OpenAI 兼容发 Bearer，Claude 协议发 x-api-key、中转另带 Bearer）
		if st.Echo == "" {
			st.Echo = string(EchoAuto)
		}
		if !AuthMode(st.Auth).valid() {
			return errors.New("鉴权只能是默认（留空）、bearer、api-key 或 both")
		}
		if !EchoMode(st.Echo).valid() || st.Echo == "" {
			return errors.New("思考回传只能是 auto、on 或 off")
		}
	}
	if !st.Protocol.valid() {
		return errors.New("协议只能是 openai 或 anthropic")
	}
	if pt.Model != nil {
		st.Model = strings.TrimSpace(*pt.Model)
	} else if changedPreset {
		st.Model = p.Model
	}
	if len([]rune(st.Model)) > 200 || hasControl(st.Model) {
		return errors.New("模型名不对（最长 200 个字）")
	}
	if pt.MaxOutput != nil {
		st.MaxOutput = max(0, *pt.MaxOutput)
	}
	if pt.Effort != nil {
		st.Effort = *pt.Effort
	}
	if !validEffort(st.Effort) {
		return errors.New("思考深度只能是 low、medium、high、xhigh、max，或留空用模型默认值")
	}
	if st.Protocol != Anthropic {
		st.Effort, st.MaxOutput = "", 0
	}
	var newKey *string
	if pt.Key != nil {
		k := strings.TrimSpace(*pt.Key)
		if len(k) > 4096 || hasControl(k) || strings.ContainsAny(k, " \t") {
			return errors.New("Key 的格式不对（不能有空格，最长 4096 个字符）")
		}
		newKey = &k
	}
	// 换了服务商、协议或地址：Claude 中转的功能探测要重来
	moved := changedPreset || st.Protocol != old.Protocol || st.BaseURL != old.BaseURL
	if moved {
		st.Eager, st.Fallback = nil, nil
		if pt.MaxOutput == nil {
			st.MaxOutput = 0
		}
	}
	// 换了服务商，或者地址换到了别的网站：旧 Key 不能带过去（免得发给别人），清掉。
	// 同一个网站只改路径或接口协议，Key 照旧（界面上写着「留空不修改」）。
	if newKey == nil && (changedPreset || !SameSite(st.BaseURL, old.BaseURL)) {
		empty := ""
		newKey = &empty
	}
	return s.commitLocked(st, newKey)
}

// commitLocked 把新设置 st 和与它配对的 Key 一起存好，都存好了才换掉内存里的设置；newKey 为 nil 表示 Key 不动。
// 调用时持有 s.mu，存完才放开：要发请求的从 current 一起取设置和 Key，也要这把锁，看不到存到一半的样子。
//
// 中途写盘失败就退回原样（v3.1.0 正式版前检查 B-10）。以前先改内存再写盘：写设置失败的话，内存里已经是新地址、
// Key 还是旧的，后面的请求会把旧 Key 发到新地址；设置存好了、新 Key 没存上的话，盘上也是这样，重启以后照旧。
// 现在 Key 要变时先清掉旧 Key，再写设置，最后存新 Key；退回时先写回旧设置，写回成功了才放回旧 Key。
// 哪一步失败、退回时又失败，最坏也只是「某个地址 + 没有 Key」（要重填），不会是「新地址 + 旧 Key」。
func (s *Service) commitLocked(st Settings, newKey *string) error {
	if newKey == nil {
		if err := s.save(st); err != nil {
			return fmt.Errorf("保存设置失败：%w", err)
		}
		s.st = st
		return nil
	}
	old, oldKey := s.st, s.key() // 旧 Key 解不开的按没有算：退回以后就是没填 Key
	if err := s.setKey(""); err != nil {
		return err
	}
	if err := s.save(st); err != nil {
		_ = s.setKey(oldKey) // 设置没写进去：放回旧 Key，还是原来那一对（放不回去就是没填 Key，不会配错）
		return fmt.Errorf("保存设置失败：%w", err)
	}
	if *newKey != "" {
		if err := s.setKey(*newKey); err != nil {
			if s.save(old) == nil {
				_ = s.setKey(oldKey)
			} else {
				s.st = st // 旧设置也写不回去：内存跟着盘上的新设置走，Key 空着，要重填
			}
			return err
		}
	}
	s.st = st
	return nil
}

func (s *Service) setKey(k string) error {
	if s.keys == nil {
		if k == "" {
			return nil
		}
		return errors.New("没有地方存 Key")
	}
	if err := s.keys.Set(KeyName, k); err != nil {
		return fmt.Errorf("保存 Key 失败：%w", err)
	}
	return nil
}

// SetQuota 设每日额度（token）。只改传了的功能；0 表示不限。
func (s *Service) SetQuota(q map[Feature]int64) error {
	for f, v := range q {
		ok := false
		for _, x := range QuotaFeatures {
			ok = ok || x == f
		}
		if !ok {
			return fmt.Errorf("不能给 %q 设额度", f)
		}
		if v < 0 || v > 1e12 {
			return errors.New("额度要在 0 到 1 万亿 token 之间（0 表示不限）")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Quota == nil {
		s.st.Quota = map[Feature]int64{}
	}
	for f, v := range q {
		if v == 0 {
			delete(s.st.Quota, f)
		} else {
			s.st.Quota[f] = v
		}
	}
	return s.saveLocked()
}

// SetPrices 整体替换自填的单价。
func (s *Service) SetPrices(ps []CustomPrice) error {
	if len(ps) > 100 {
		return errors.New("自填单价最多 100 条")
	}
	seen := map[string]bool{}
	for _, cp := range ps {
		if err := checkCustomPrice(cp); err != nil {
			return fmt.Errorf("%s：%w", cp.Model, err)
		}
		if seen[cp.Model] {
			return fmt.Errorf("模型 %s 填了两次", cp.Model)
		}
		seen[cp.Model] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Prices = append([]CustomPrice(nil), ps...)
	return s.saveLocked()
}

// RefreshPrices 手动刷新公开价格。没配置 AI，或者只配了本机模型时不取。
func (s *Service) RefreshPrices(ctx context.Context) error {
	st := s.snapshot()
	if s.missing(st) != "" {
		return ErrDisabled
	}
	if !s.paid(st) {
		return errors.New("本机模型不花钱，不需要价格数据")
	}
	return s.prices.Refresh(ctx)
}

// ---------- 给界面看的状态 ----------

// ProviderView 服务商设置（不含 Key）。
type ProviderView struct {
	Preset    string   `json:"preset"`
	Protocol  Protocol `json:"protocol"`
	BaseURL   string   `json:"baseUrl"`
	Model     string   `json:"model"`
	MaxOutput int      `json:"maxOutput,omitempty"`
	Effort    string   `json:"effort"`
	Eager     bool     `json:"eager"`    // Claude：工具参数边生成边推（中转默认不开，测试连接后确定）
	Fallback  bool     `json:"fallback"` // Claude：服务端回退
	Auth      string   `json:"auth,omitempty"`
	Echo      string   `json:"echo,omitempty"`
}

// ModelPrice 当前模型的单价（每百万 token），给设置页显示。
type ModelPrice struct {
	Found      bool     `json:"found"`
	Local      bool     `json:"local,omitempty"` // 本机模型不计费
	Source     string   `json:"source,omitempty"`
	Currency   string   `json:"currency,omitempty"`
	Input      float64  `json:"input,omitempty"`
	Output     float64  `json:"output,omitempty"`
	CacheRead  *float64 `json:"cacheRead,omitempty"`
	CacheWrite *float64 `json:"cacheWrite,omitempty"`
	Tiered     bool     `json:"tiered,omitempty"` // 按输入长度分档（显示的是最低一档）
	Note       string   `json:"note,omitempty"`
}

// View GET /api/ai/settings 的内容。
type View struct {
	Presets      []Preset           `json:"presets"`
	Provider     ProviderView       `json:"provider"`
	KeySet       bool               `json:"keySet"`
	KeyBroken    bool               `json:"keyBroken"`
	Enabled      bool               `json:"enabled"`
	Missing      string             `json:"missing,omitempty"`
	Quota        map[Feature]int64  `json:"quota"`
	Today        map[Feature]Totals `json:"today"`
	Month        map[Feature]Totals `json:"month"`
	PriceNeeded  bool               `json:"priceNeeded"` // 配置了要花钱的服务商（才会去取价格数据）
	Prices       PriceStatus        `json:"prices"`
	ModelPrice   *ModelPrice        `json:"modelPrice,omitempty"`
	CustomPrices []CustomPrice      `json:"customPrices"`
	LANAssistant bool               `json:"lanAssistant"` // 局域网登录的设备能不能用应用内助手
	Local        bool               `json:"local"`        // 请求来自本机（只有本机能改 AI 设置），由调用方填
}

// DomesticNote 国内四家的费用提示（§4.4）。
const DomesticNote = "按国际站美元价估算，国内站按人民币计费，会有出入"

// View 当前的设置和状态，不含 Key、不联网。
func (s *Service) View() View {
	st := s.snapshot()
	set, broken := s.keyState()
	v := View{Presets: Presets(), KeySet: set, KeyBroken: broken, Quota: st.Quota, Prices: s.prices.Status(),
		CustomPrices: st.Prices, Today: map[Feature]Totals{}, Month: map[Feature]Totals{}, LANAssistant: boolOr(st.LANAssistant, true)}
	if v.CustomPrices == nil {
		v.CustomPrices = []CustomPrice{}
	}
	v.Missing = s.missing(st)
	v.Enabled = v.Missing == ""
	v.PriceNeeded = s.paid(st)
	cfg := s.config(st, "", true)
	v.Provider = ProviderView{Preset: st.Preset, Protocol: st.Protocol, BaseURL: st.BaseURL, Model: st.Model, MaxOutput: st.MaxOutput,
		Effort: st.Effort, Eager: cfg.Eager, Fallback: cfg.Fallback, Auth: string(cfg.Auth), Echo: string(cfg.Echo)}
	for _, f := range []Feature{FeatAssistant, FeatGameChat, FeatCompanion} {
		v.Today[f] = s.ledger.Today(f)
		v.Month[f] = s.ledger.Month(f)
	}
	if p, ok := PresetByID(st.Preset); ok && st.Model != "" {
		v.ModelPrice = s.modelPrice(st, p)
	}
	return v
}

func (s *Service) modelPrice(st Settings, p Preset) *ModelPrice {
	if p.Local {
		return &ModelPrice{Found: true, Local: true}
	}
	for _, cp := range st.Prices {
		if cp.Model == st.Model {
			return &ModelPrice{Found: true, Source: "custom", Currency: cp.Currency, Input: cp.Input, Output: cp.Output, CacheRead: cp.CacheRead, CacheWrite: cp.CacheWrite}
		}
	}
	pr, ok := s.prices.Lookup(p.litellm, st.Model)
	if !ok {
		return &ModelPrice{}
	}
	m := func(v float64) float64 { return v * 1e6 }
	mp := &ModelPrice{Found: true, Source: "litellm", Currency: "USD", Input: m(pr.Input), Output: m(pr.Output), Tiered: len(pr.Tiers) > 0}
	if pr.CacheRead != nil {
		v := m(*pr.CacheRead)
		mp.CacheRead = &v
	}
	if pr.CacheWrite != nil {
		v := m(*pr.CacheWrite)
		mp.CacheWrite = &v
	}
	if p.Domestic {
		mp.Note = DomesticNote
	}
	return mp
}

// ---------- 模型列表、测试连接 ----------

// ModelsRequest 取模型列表：可以用设置页里还没保存的服务商和地址。Key 不传时，只有服务商没换、地址还是同一个网站
// 才用已存的 Key（不把已存的 Key 发到别的网站去）。
type ModelsRequest struct {
	Preset   string   `json:"preset"`
	Protocol Protocol `json:"protocol"`
	BaseURL  string   `json:"baseUrl"`
	Key      string   `json:"key"`
	// Auth 没传（nil）才沿用已存的；传了空串就是「按协议的默认」（界面上选了「默认」还没保存，不能退回旧值）
	Auth *string `json:"auth,omitempty"`
	Echo string  `json:"echo,omitempty"`
}

// ModelList 模型列表。Source=live 是服务商返回的；preset 是预设里的建议（对方没有 /models，或列表接口 404）。
type ModelList struct {
	Models  []Model `json:"models"`
	Source  string  `json:"source"`
	Warning string  `json:"warning,omitempty"`
}

// Models 取模型列表。这是用户在设置页点了按钮才发的请求。
// 文档写明没有 /models 的地址不发请求，直接给预设里的建议。404/405/501 同样退回建议，仍允许手填。
// 401/403 不兜底：那是 Key 或权限的问题，要让用户看到。
func (s *Service) Models(ctx context.Context, req ModelsRequest) (ModelList, error) {
	p, ok := PresetByID(req.Preset)
	if !ok {
		return ModelList{}, errors.New("先选择服务商")
	}
	proto := p.Protocol
	if p.ID == CustomID && req.Protocol.valid() {
		proto = req.Protocol
	} else if ep, ok := p.matchURL(strings.TrimSpace(req.BaseURL)); ok && req.BaseURL != "" {
		proto = ep.Protocol
	} else if ep, ok := p.matchURL(p.BaseURL); ok && strings.TrimSpace(req.BaseURL) == "" {
		proto = ep.Protocol
	}
	base := strings.TrimSpace(req.BaseURL)
	if base == "" {
		base = p.BaseURL
	}
	if _, err := ParseBaseURL(base); err != nil {
		return ModelList{}, err
	}
	if ep, ok := p.matchURL(base); ok && ep.NoModels {
		return ModelList{Models: suggestedModels(p), Source: "preset",
			Warning: "这个地址的官方文档没有模型列表，下面是预设里的建议，也可以直接填写模型名。"}, nil
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		if st, saved := s.current(); st.Preset == p.ID && SameSite(st.BaseURL, base) {
			key = saved
		}
	}
	if key == "" && !p.NoKey && p.ID != CustomID {
		return ModelList{}, errors.New("先填写 Key")
	}
	saved := s.snapshot()
	auth, echo := saved.Auth, req.Echo
	if req.Auth != nil {
		auth = *req.Auth
	}
	if echo == "" {
		echo = saved.Echo
	}
	st := Settings{Preset: p.ID, Protocol: proto, BaseURL: base, Auth: auth, Echo: echo}
	cl, err := NewClient(s.config(st, key, false))
	if err != nil {
		return ModelList{}, err
	}
	ms, err := cl.ListModels(ctx)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && (ae.Status == 404 || ae.Status == 405 || ae.Status == 501) {
			if sug := suggestedModels(p); len(sug) > 0 {
				return ModelList{Models: sug, Source: "preset",
					Warning: "这个地址没有模型列表，下面是预设里的建议，也可以直接填写模型名。"}, nil
			}
		}
		return ModelList{}, err
	}
	if len(ms) == 0 {
		if sug := suggestedModels(p); len(sug) > 0 {
			return ModelList{Models: sug, Source: "preset",
				Warning: "服务商没有返回模型，下面是预设里的建议，也可以直接填写模型名。"}, nil
		}
	}
	return ModelList{Models: ms, Source: "live"}, nil
}

// TestResult 测试连接的结果。
type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"` // 发出请求到收到第一个事件
	TotalMs   int64  `json:"totalMs"`
	Model     string `json:"model,omitempty"`
	Tools     string `json:"tools"` // yes：能调用工具；no：服务商说不支持；unknown：模型没调用
	Reply     string `json:"reply,omitempty"`
	Usage     Usage  `json:"usage"`
	Cost      *Cost  `json:"cost,omitempty"`
	Eager     *bool  `json:"eager,omitempty"`    // Claude：eager_input_streaming 能不能用
	Fallback  *bool  `json:"fallback,omitempty"` // Claude：服务端回退能不能用
	Error     string `json:"error,omitempty"`
	Hint      string `json:"hint,omitempty"` // 失败原因里给人看的那一句（见 APIError.Hint），界面的正文接它；完整原文是 Error
	Stop      string `json:"stop,omitempty"`
	Warn      string `json:"warn,omitempty"`   // Key 前缀和地址对不上（不拦截，测试照样发）
	Rounds    int    `json:"rounds,omitempty"` // 实际请求了几轮（调用了工具就会有第二轮）
}

var pingTool = Tool{Name: "minedeck_ping", Description: "连接测试用的工具。收到连接测试的请求时调用它。",
	Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"固定填 true"}},"required":["ok"]}`)}

// Test 测试连接：发一个最小的流式请求（带一个测试用的工具），看延迟、能不能调用工具。
// Claude 的中转：先打开 eager_input_streaming 和服务端回退试，不认的自动关掉，结果存进设置（§4.3）。
// 测试花的 token 不计入每日额度。
func (s *Service) Test(ctx context.Context) (*TestResult, error) {
	st, key := s.current()
	if missingWithKey(st, key) != "" {
		return nil, ErrDisabled
	}
	p, _ := PresetByID(st.Preset)
	limit := 90 * time.Second
	if p.Local {
		limit = 2 * time.Minute // 本机模型可能要先加载进内存，再跑两轮
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cfg := s.config(st, key, true)
	if cfg.Protocol == Anthropic {
		cfg.Eager, cfg.Fallback = true, true
		cfg.MaxTokens = min(cfg.MaxTokens, 4096)
	}
	warn := KeyWarning(p, st.BaseURL, key)
	res, err := s.probe(ctx, cfg, []Tool{pingTool})
	tools := "yes"
	var ae *APIError
	var r2 *secondRoundError
	if err != nil && !errors.As(err, &r2) && errors.As(err, &ae) && ae.Status == 400 && strings.Contains(strings.ToLower(ae.Message), "tool") {
		// 第一轮就说不支持工具（服务商或本机模型）：去掉工具再试一次，看基本的对话通不通。
		// 第二轮（工具结果交回之后）失败不走这里：那时已经证明能调用工具，要把第二轮的问题照实报出来。
		res, err = s.probe(ctx, cfg, nil)
		tools = "no"
	}
	if err != nil {
		tr := &TestResult{Error: err.Error(), Hint: hintOf(err), Tools: "unknown", Warn: warn}
		if errors.As(err, &r2) {
			tr.Tools, tr.Rounds = "yes", 2
			if p.ID == CustomID && cfg.Echo != EchoOn && r2.rejected() {
				tr.Hint += "。可以把「思考回传」改成「总是回传」"
			}
		}
		if errors.As(err, &ae) && ae.Status == 401 && warn != "" {
			// Key 前缀和地址对不上：这比「Key 不对」更说到点子上。界面正文只接 60 字以内的一句，
			// 长的那段说明（warn）另外显示在这一行下面
			tr.Hint = "Key 和地址对不上：按量付费和套餐的 Key 不能混用"
		}
		return tr, nil
	}
	out := res.out
	out.OK, out.Tools, out.Warn = true, tools, warn
	if tools == "yes" && !res.called {
		out.Tools = "unknown"
	}
	if cfg.Protocol == Anthropic {
		eager, fb := res.client.Features()
		out.Eager, out.Fallback = &eager, &fb
		// 中转：试出来能用的才打开，存进设置。官方地址不存：每个会话的第一次请求被拒时客户端自己会关掉
		if !cfg.Official {
			s.mu.Lock()
			if s.st.Preset == st.Preset && s.st.BaseURL == st.BaseURL {
				s.st.Eager, s.st.Fallback = &eager, &fb
				_ = s.saveLocked()
			}
			s.mu.Unlock()
		}
	}
	out.Cost = s.estimate(st, res.result)
	return &out, nil
}

type probeResult struct {
	out    TestResult
	result *Result
	called bool
	client *Client
}

// secondRoundError 测试连接的第二轮（工具结果交回之后）失败了。第一轮已经证明能调用工具，
// Test 不能再把它当成「不支持工具」去掉工具重试，那样会把真正的问题（多半是思考内容没带回）说成连接正常。
type secondRoundError struct{ err error }

func (e *secondRoundError) Error() string {
	return "工具结果交回后的第二轮失败：" + e.err.Error()
}
func (e *secondRoundError) Unwrap() error { return e.err }

// rejected 第二轮是被服务商拒绝的（400），不是网络或额度之类的问题。
func (e *secondRoundError) rejected() bool {
	var ae *APIError
	return errors.As(e.err, &ae) && ae.Status == 400
}

// Hint 见 APIError.Hint。
func (e *secondRoundError) Hint() string {
	var ae *APIError
	if errors.As(e.err, &ae) && ae.Status == 400 {
		if reasoningField.MatchString(ae.lowerText()) {
			return "工具结果交回之后被拒：要把思考内容原样传回，或者服务商不认思考字段"
		}
		return "工具结果交回之后被拒：多半是思考内容没有原样传回"
	}
	if h := hintOf(e.err); h != "" {
		return "工具结果交回之后失败：" + h
	}
	return "工具结果交回之后失败"
}

func (s *Service) probe(ctx context.Context, cfg Config, tools []Tool) (*probeResult, error) {
	cl, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	conv, err := cl.NewConversation("这是 MineDeck 的连接测试，请按用户的要求做。", tools)
	if err != nil {
		return nil, err
	}
	q := "这是连接测试。请回复「连接正常」四个字。"
	if len(tools) > 0 {
		q = "这是连接测试。请调用 minedeck_ping 工具，参数 ok 设为 true，不用回复其他内容。"
	}
	_ = conv.AddUser(q)
	start := time.Now()
	var first time.Duration
	res, err := cl.Chat(ctx, conv, func(e Event) {
		if first == 0 && e.Type != EvRetry {
			first = time.Since(start)
		}
	})
	if err != nil {
		return nil, err
	}
	total := time.Since(start)
	if first == 0 {
		first = total
	}
	pr := &probeResult{result: res, client: cl, out: TestResult{LatencyMs: first.Milliseconds(), TotalMs: total.Milliseconds(),
		Model: res.Model, Reply: truncate(strings.TrimSpace(res.Text), 200), Usage: res.Usage, Stop: string(res.Stop), Rounds: 1}}
	for _, tc := range res.ToolCalls {
		pr.called = pr.called || tc.Name == pingTool.Name
	}
	if pr.called {
		// 交回一个无害结果再发一轮：有的服务商第一轮能过，第二轮才因为没带回思考内容而 400。
		if err := conv.AddResult(res); err != nil {
			return nil, fmt.Errorf("测试的工具调用无法记入对话：%w", err)
		}
		rs := make([]ToolResult, len(res.ToolCalls))
		for i, tc := range res.ToolCalls {
			body := "（测试连接，没有执行）"
			if tc.Name == pingTool.Name {
				body = `{"ok":true}`
			}
			rs[i] = ToolResult{ID: tc.ID, Content: body}
		}
		if err := conv.AddToolResults(rs); err != nil {
			return nil, err
		}
		res2, err := cl.Chat(ctx, conv, func(Event) {})
		if err != nil {
			return nil, &secondRoundError{err: err}
		}
		pr.out.Rounds = 2
		u := res.Usage
		u.Add(res2.Usage)
		pr.out.Usage = u
		cost := *res
		cost.Usage = u
		if res2.Model != "" {
			cost.Model = res2.Model
		}
		pr.result = &cost
		if t := strings.TrimSpace(res2.Text); t != "" {
			pr.out.Reply = truncate(t, 200)
		}
		pr.out.TotalMs = time.Since(start).Milliseconds()
	}
	if pr.out.Model == "" {
		pr.out.Model = cfg.Model
	}
	return pr, nil
}
