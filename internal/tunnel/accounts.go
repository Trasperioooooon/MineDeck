package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/box"

	"minedeck/internal/app"
	"minedeck/internal/secret"
)

const (
	openFrpAuthKey = "tunnel.openfrp.authorization"
	sakuraAuthKey  = "tunnel.sakura.token"
	openFrpAPI     = "https://api.openfrp.net"
	openFrpLogin   = "https://access.openfrp.net"
	sakuraAPI      = "https://api.natfrp.com/v4"

	// 只给浏览器打开的官方页面。签到和支付都在官网完成，MineDeck 不代签、不代下单。
	// OpenFrp 的签到加了人机验证，官方 OPENAPI 写明这部分「暂不可供参考」，所以只跳控制台的签到深链，
	// 控制台打开后会自己拉起验证码。
	openFrpPremiumURL = "https://console.openfrp.net/premium"
	openFrpAccountURL = "https://account.naids.com/dashboard"
	openFrpSignURL    = "https://console.openfrp.net/dashboard?action=sign"
	sakuraUserURL     = "https://www.natfrp.com/user/"
	sakuraProfileURL  = "https://www.natfrp.com/user/profile"
	sakuraShopURL     = "https://www.natfrp.com/shop"
	sakuraTunnelShop  = "https://www.natfrp.com/tunnel/shop"
	sakuraPaymentFAQ  = "https://doc.natfrp.com/faq/payment.html"

	// overviewTTL 概览缓存多久：一次概览要向平台连发好几个请求，OpenFrp 超出阈值会一律回 429。
	overviewTTL = 30 * time.Second
)

// errAccountChanged 一次多步操作（概览、创建、导入）途中账户被断开或换了：后面的步骤不再用另一个账户的凭据继续。
var errAccountChanged = errors.New("操作途中账户被断开或换了，后面的步骤没有继续；平台上可能已经做了一部分，请重新连接后在概览里看看")

// openFrpTunnelName OpenFrp 的 newProxy 文档写明隧道名不支持中文；这里只放行最稳的字符。
var openFrpTunnelName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,30}$`)

type Accounts struct {
	tunnels   *Manager
	keys      *secret.Store
	client    *http.Client
	openAPI   string
	openLogin string
	sakuraAPI string
	userAgent string
	now       func() time.Time

	// mu 管 pending、两家的连接代数和概览缓存。凭据的写入也在 mu 里做：
	// 断开会把代数加一，在途的登录、连接、授权轮换回来时代数对不上就不写，断开的账户不会被悄悄连回去。
	mu        sync.Mutex
	pending   map[string]*openFrpLoginState
	openGen   uint64
	sakuraGen uint64
	overviews map[string]cachedOverview
	ovVer     map[string]uint64 // 平台上的隧道变了就加一：在途的旧概览回来时不再写进缓存

	// guardPort MineDeck 自己的网页端口：不许建成或导入指向它的隧道（映射到公网后经 frpc 进来的请求都来自本机地址）
	guardPort int
}

type cachedOverview struct {
	at  time.Time
	gen uint64
	ver uint64
	ov  AccountOverview
}

type openFrpLoginState struct {
	requestUUID string
	privateKey  *[32]byte
	expires     time.Time
	lastPoll    time.Time
	gen         uint64 // 发起时的 OpenFrp 连接代数：期间断开过或别的登录先成功了，这次就作废
}

func NewAccounts(tunnels *Manager, keys *secret.Store) *Accounts {
	if keys == nil {
		keys = app.Secrets()
	}
	return &Accounts{
		tunnels: tunnels,
		keys:    keys,
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		openAPI:   openFrpAPI,
		openLogin: openFrpLogin,
		sakuraAPI: sakuraAPI,
		userAgent: fmt.Sprintf("MineDeck/%s", app.Version),
		now:       time.Now,
		pending:   map[string]*openFrpLoginState{},
		overviews: map[string]cachedOverview{},
		ovVer:     map[string]uint64{},
	}
}

// SetGuardPort 告诉账户层 MineDeck 自己的网页端口，创建和导入时拒绝指向它的隧道。
func (a *Accounts) SetGuardPort(port int) {
	a.mu.Lock()
	a.guardPort = port
	a.mu.Unlock()
}

func (a *Accounts) checkGuardPort(port int) error {
	a.mu.Lock()
	guard := a.guardPort
	a.mu.Unlock()
	if guard > 0 && port == guard {
		return fmt.Errorf("本地端口 %d 是 MineDeck 自己的网页端口，不能映射到公网", port)
	}
	return nil
}

// AccountStatus 两家是否已连接。Local 是调用方是否在本机：局域网打开时界面只读。
// *Unreadable 表示存过凭据但解不开（多半换了电脑或 Windows 用户），界面提示重新连接。
type AccountStatus struct {
	OpenFrp           bool `json:"openfrp"`
	Sakura            bool `json:"natfrp"`
	Local             bool `json:"local"`
	OpenFrpUnreadable bool `json:"openfrpUnreadable,omitempty"`
	SakuraUnreadable  bool `json:"natfrpUnreadable,omitempty"`
}

type AccountInfo struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	Group           string `json:"group,omitempty"`
	Realname        bool   `json:"realname,omitempty"`
	TunnelCount     int    `json:"tunnelCount,omitempty"`
	TunnelLimit     int    `json:"tunnelLimit,omitempty"`
	TrafficSummary  string `json:"trafficSummary,omitempty"`
	SignedToday     *bool  `json:"signedToday,omitempty"`
	ConsecutiveDays int    `json:"consecutiveDays,omitempty"`
}

type AccountNode struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	CanCreate         bool   `json:"canCreate"`
	TCP               bool   `json:"tcp"`
	UDP               bool   `json:"udp"`
	PortRange         string `json:"portRange,omitempty"`
	PublicGameAllowed bool   `json:"publicGameAllowed"`
}

type AccountTunnel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	NodeName  string `json:"nodeName"`
	LocalIP   string `json:"localIP,omitempty"`
	LocalPort int    `json:"localPort,omitempty"`
	Remote    string `json:"remote,omitempty"`
	Online    bool   `json:"online"`
	Enabled   bool   `json:"enabled"`
	CanImport bool   `json:"canImport"`
}

type AccountOverview struct {
	Provider string          `json:"provider"`
	Account  AccountInfo     `json:"account"`
	Nodes    []AccountNode   `json:"nodes"`
	Tunnels  []AccountTunnel `json:"tunnels"`
}

type AccountTunnelOptions struct {
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	NodeID       int    `json:"nodeId"`
	Type         string `json:"type"`
	LocalPort    int    `json:"localPort"`
	RemotePort   int    `json:"remotePort"`
	Bound        string `json:"boundInstance"`
	AutoStart    bool   `json:"autoStart"`
	Purpose      string `json:"purpose"`
	PublicServer bool   `json:"publicServer"`
}

type AccountTunnelImport struct {
	Provider  string `json:"provider"`
	ProxyID   string `json:"proxyId"`
	Bound     string `json:"boundInstance"`
	AutoStart bool   `json:"autoStart"`
	Purpose   string `json:"purpose"`
}

type OpenFrpLogin struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type OpenFrpLoginPoll struct {
	Pending bool `json:"pending"`
}

// SignResult 签到引导。两家的签到都要在官网完成（OpenFrp 有人机验证，樱花只认官网登录会话），
// MineDeck 不发任何签到请求，只告诉前端打开哪个官方页面。OK 恒为 false：这里没有签成。
type SignResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	OpenURL string `json:"openURL"`
}

// PurchaseOption 购买/充值跳转入口（公开 OPENAPI 无支付接口时返回官方页）。
type PurchaseOption struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"` // shop / premium / account / tunnel
}

// PurchaseOptions 某平台的购买入口列表。
type PurchaseOptionsResult struct {
	Provider string           `json:"provider"`
	Options  []PurchaseOption `json:"options"`
}

func (a *Accounts) Status(local bool) AccountStatus {
	open, openBad := a.keys.State(openFrpAuthKey)
	sakura, sakuraBad := a.keys.State(sakuraAuthKey)
	return AccountStatus{OpenFrp: open, Sakura: sakura, Local: local, OpenFrpUnreadable: openBad, SakuraUnreadable: sakuraBad}
}

// validAuthorization 平台给的 Authorization / 访问密钥是否像样：不空、不超长、不含换行（会被当成请求头注入）。
func validAuthorization(s string) bool {
	return len(s) >= 8 && len(s) <= 4096 && !strings.ContainsAny(s, "\r\n")
}

// generation 取某家当前的连接代数（调用方不持锁）。
func (a *Accounts) generation(provider string) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generationLocked(provider)
}

// generationLocked 同 generation，调用方已持有 a.mu。
func (a *Accounts) generationLocked(provider string) uint64 {
	if provider == "openfrp" {
		return a.openGen
	}
	return a.sakuraGen
}

// bumpLocked 某家的连接变了（新连上、断开）：代数加一，概览缓存作废。调用方持有 a.mu。
func (a *Accounts) bumpLocked(provider string) {
	if provider == "openfrp" {
		a.openGen++
	} else {
		a.sakuraGen++
	}
	delete(a.overviews, provider)
}

// forgetOverview 平台上的隧道变了（创建、导入）：下次概览重新拉，在途的旧概览也不再写进缓存。
func (a *Accounts) forgetOverview(provider string) {
	a.mu.Lock()
	delete(a.overviews, provider)
	a.ovVer[provider]++
	a.mu.Unlock()
}

func (a *Accounts) BeginOpenFrpLogin(ctx context.Context) (OpenFrpLogin, error) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return OpenFrpLogin{}, fmt.Errorf("生成 OpenFrp 授权密钥失败: %w", err)
	}
	gen := a.generation("openfrp")
	body := map[string]string{"public_key": base64.URLEncoding.EncodeToString(public[:])}
	data, _, status, err := a.request(ctx, http.MethodPost, a.openLogin, "/argoAccess/requestLogin", "", body, true)
	if err != nil {
		clear(private[:])
		return OpenFrpLogin{}, err
	}
	if status < 200 || status >= 300 {
		clear(private[:])
		return OpenFrpLogin{}, responseError(status, data)
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AuthorizationURL string `json:"authorization_url"`
			RequestUUID      string `json:"request_uuid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		clear(private[:])
		return OpenFrpLogin{}, fmt.Errorf("OpenFrp 登录请求响应格式无效")
	}
	if resp.Code != 200 || resp.Data.RequestUUID == "" {
		clear(private[:])
		return OpenFrpLogin{}, fmt.Errorf("OpenFrp 登录请求失败: %s", safeMessage(resp.Msg))
	}
	if !validOpenFrpAuthorizationURL(resp.Data.AuthorizationURL) {
		clear(private[:])
		return OpenFrpLogin{}, fmt.Errorf("OpenFrp 返回了无效的授权地址")
	}
	now := a.now()
	a.mu.Lock()
	if a.openGen != gen {
		// 发起授权期间账户被断开或别的登录先成功了：这次不算
		a.mu.Unlock()
		clear(private[:])
		return OpenFrpLogin{}, fmt.Errorf("发起授权期间 OpenFrp 账户状态变了，请重新连接")
	}
	for id, state := range a.pending {
		if now.After(state.expires) {
			clear(state.privateKey[:])
			delete(a.pending, id)
		}
	}
	a.pending[resp.Data.RequestUUID] = &openFrpLoginState{
		requestUUID: resp.Data.RequestUUID,
		privateKey:  private,
		expires:     now.Add(5 * time.Minute),
		gen:         gen,
	}
	a.mu.Unlock()
	return OpenFrpLogin{ID: resp.Data.RequestUUID, URL: resp.Data.AuthorizationURL}, nil
}

func validOpenFrpAuthorizationURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() == "console.openfrp.net" && u.User == nil
}

func (a *Accounts) PollOpenFrpLogin(ctx context.Context, id string) (OpenFrpLoginPoll, error) {
	a.mu.Lock()
	state := a.pending[id]
	if state == nil {
		a.mu.Unlock()
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 登录请求不存在或已完成")
	}
	now := a.now()
	if now.After(state.expires) {
		clear(state.privateKey[:])
		delete(a.pending, id)
		a.mu.Unlock()
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 登录请求已过期，请重新连接")
	}
	if !state.lastPoll.IsZero() && now.Sub(state.lastPoll) < 5*time.Second {
		a.mu.Unlock()
		return OpenFrpLoginPoll{Pending: true}, nil
	}
	state.lastPoll = now
	privateKey := *state.privateKey
	a.mu.Unlock()
	defer clear(privateKey[:])

	data, headers, status, err := a.request(ctx, http.MethodGet, a.openLogin, "/argoAccess/pollLogin", "", map[string]string{"request_uuid": state.requestUUID}, true)
	if err != nil {
		return OpenFrpLoginPoll{}, err
	}
	if status == http.StatusNoContent {
		return OpenFrpLoginPoll{Pending: true}, nil
	}
	if status < 200 || status >= 300 {
		return OpenFrpLoginPoll{}, responseError(status, data)
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AuthorizationData string `json:"authorization_data"`
			RequestUUID       string `json:"request_uuid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 登录轮询响应格式无效")
	}
	if resp.Code == http.StatusNoContent || resp.Data.AuthorizationData == "" {
		if resp.Code == 0 || resp.Code == http.StatusNoContent || resp.Code == http.StatusOK {
			return OpenFrpLoginPoll{Pending: true}, nil
		}
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 登录轮询失败: %s", safeMessage(resp.Msg))
	}
	if resp.Code != http.StatusOK {
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 登录轮询失败: %s", safeMessage(resp.Msg))
	}
	plain, err := decryptOpenFrpAuthorization(resp.Data.AuthorizationData, headers.Get("X-Request-Public-Key"), &privateKey)
	if err != nil {
		return OpenFrpLoginPoll{}, fmt.Errorf("解密 OpenFrp 授权信息失败: %w", err)
	}
	authorization := strings.TrimSpace(string(plain))
	clear(plain)
	if !validAuthorization(authorization) {
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 返回的授权信息无效")
	}
	// 在锁里确认这次登录还在等：用户在授权途中点了「断开」或「取消等待」重来，就不能再把授权写进去。
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending[id] != state || a.openGen != state.gen {
		return OpenFrpLoginPoll{}, fmt.Errorf("这次 OpenFrp 授权已经取消，请重新连接")
	}
	if err := a.keys.Set(openFrpAuthKey, authorization); err != nil {
		return OpenFrpLoginPoll{}, fmt.Errorf("保存 OpenFrp 授权失败: %w", err)
	}
	// 连上了：这次和其余还在等的登录全部作废，较早发起的登录不能再回来覆盖这次的授权
	for pid, ps := range a.pending {
		clear(ps.privateKey[:])
		delete(a.pending, pid)
	}
	a.bumpLocked("openfrp")
	return OpenFrpLoginPoll{Pending: false}, nil
}

// CancelOpenFrpLogin 用户点了「取消等待」：这次登录作废，此后即使有在途的轮询拿到授权也不会写进去。
// 返回 false 表示这次登录已经不在等了（多半是刚刚授权成功，也可能过期了），界面要重新查一次连接状态。
func (a *Accounts) CancelOpenFrpLogin(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.pending[id]
	if state == nil {
		return false
	}
	clear(state.privateKey[:])
	delete(a.pending, id)
	return true
}

func decryptOpenFrpAuthorization(ciphertext, serverPublic string, private *[32]byte) ([]byte, error) {
	var encrypted []byte
	var err error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		encrypted, err = enc.DecodeString(ciphertext)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("密文不是有效的 base64")
	}
	if len(encrypted) < 24 {
		return nil, fmt.Errorf("密文长度不正确")
	}
	var public [32]byte
	var decoded []byte
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
		decoded, err = enc.DecodeString(serverPublic)
		if err == nil {
			break
		}
	}
	if err != nil || len(decoded) != len(public) {
		return nil, fmt.Errorf("服务端公钥格式不正确")
	}
	copy(public[:], decoded)
	var nonce [24]byte
	copy(nonce[:], encrypted[:24])
	plain, ok := box.Open(nil, encrypted[24:], &nonce, &public, private)
	if !ok {
		return nil, fmt.Errorf("密文认证失败")
	}
	return plain, nil
}

func (a *Accounts) ConnectSakura(ctx context.Context, token string) (AccountInfo, error) {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	if !validAuthorization(token) {
		return AccountInfo{}, fmt.Errorf("请填写有效的 SakuraFrp 访问密钥")
	}
	gen := a.generation("natfrp")
	info, err := a.sakuraUserInfo(ctx, token)
	if err != nil {
		return AccountInfo{}, err
	}
	if info.Token == "" || info.Name == "" {
		return AccountInfo{}, fmt.Errorf("SakuraFrp 未返回有效的访问账户信息")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sakuraGen != gen {
		return AccountInfo{}, fmt.Errorf("连接期间 SakuraFrp 账户被断开或换了密钥，请重新连接")
	}
	if err := a.keys.Set(sakuraAuthKey, token); err != nil {
		return AccountInfo{}, fmt.Errorf("保存 SakuraFrp 访问密钥失败: %w", err)
	}
	a.bumpLocked("natfrp")
	return sakuraAccount(info), nil
}

// Disconnect 清掉本机保存的平台凭据。只管 secrets.json 里这一项：已导入的隧道各自存着 frpc 用的密钥，不动；
// 平台那边的授权也不会作废（OpenFrp 的 Authorization 最长 30 天后失效，樱花的访问密钥要用户去官网重置）。
func (a *Accounts) Disconnect(provider string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch provider {
	case "openfrp":
		if err := a.keys.Set(openFrpAuthKey, ""); err != nil {
			return err
		}
		for id, state := range a.pending {
			clear(state.privateKey[:])
			delete(a.pending, id)
		}
	case "natfrp":
		if err := a.keys.Set(sakuraAuthKey, ""); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知穿透平台: %s", provider)
	}
	a.bumpLocked(provider)
	return nil
}

// Overview 账户、节点和平台隧道。结果按平台缓存 overviewTTL：界面上来回点「概览」不会每次都向平台连发请求。
func (a *Accounts) Overview(ctx context.Context, provider string) (AccountOverview, error) {
	if provider != "openfrp" && provider != "natfrp" {
		return AccountOverview{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
	a.mu.Lock()
	gen, ver := a.generationLocked(provider), a.ovVer[provider]
	if c, ok := a.overviews[provider]; ok && c.gen == gen && c.ver == ver && a.now().Sub(c.at) < overviewTTL {
		a.mu.Unlock()
		return c.ov, nil
	}
	a.mu.Unlock()
	var ov AccountOverview
	var err error
	if provider == "openfrp" {
		ov, err = a.openFrpOverview(ctx, gen)
	} else {
		ov, err = a.sakuraOverview(ctx, gen)
	}
	if errors.Is(err, errAccountChanged) {
		return AccountOverview{}, fmt.Errorf("加载概览时账户被断开或换了，请重新打开概览")
	}
	if err != nil {
		return AccountOverview{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.generationLocked(provider) != gen {
		return AccountOverview{}, fmt.Errorf("加载概览时账户被断开或换了，请重新打开概览")
	}
	// 平台上的隧道在这期间变了（创建、导入）：这份结果照样给，但不进缓存，下次重新拉
	if a.ovVer[provider] == ver {
		a.overviews[provider] = cachedOverview{at: a.now(), gen: gen, ver: ver, ov: ov}
	}
	return ov, nil
}

// Sign 签到引导。不发任何签到请求：OpenFrp 的签到要过控制台的人机验证（官方 OPENAPI 写明这部分暂不可供参考，
// 也明令禁止自动签到），樱花的签到只认官网登录会话、访问密钥签不了。前端拿 OpenURL 让用户自己去官网点。
func (a *Accounts) Sign(provider string) (SignResult, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openfrp":
		return SignResult{OpenURL: openFrpSignURL, Message: "OpenFrp 签到需要在官方控制台完成人机验证，MineDeck 不代签。"}, nil
	case "natfrp":
		return SignResult{OpenURL: sakuraUserURL, Message: "SakuraFrp 签到需要在官网登录后完成，访问密钥不能签到。"}, nil
	default:
		return SignResult{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

// PurchaseOptions 返回购买/充值跳转入口。两家的公开 API 都没有支付接口，只给官方页面。
func (a *Accounts) PurchaseOptions(provider string) (PurchaseOptionsResult, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openfrp":
		return PurchaseOptionsResult{
			Provider: "openfrp",
			Options: []PurchaseOption{
				{Title: "增值服务 / 会员与加量", URL: openFrpPremiumURL, Kind: "premium"},
				{Title: "账户中心", URL: openFrpAccountURL, Kind: "account"},
			},
		}, nil
	case "natfrp":
		return PurchaseOptionsResult{
			Provider: "natfrp",
			Options: []PurchaseOption{
				{Title: "SakuraFrp 商店", URL: sakuraShopURL, Kind: "shop"},
				{Title: "隧道套餐", URL: sakuraTunnelShop, Kind: "tunnel"},
				{Title: "用户中心", URL: sakuraUserURL, Kind: "account"},
				{Title: "获取访问密钥", URL: sakuraProfileURL, Kind: "token"},
				{Title: "付款说明", URL: sakuraPaymentFAQ, Kind: "docs"},
			},
		}, nil
	default:
		return PurchaseOptionsResult{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

func (a *Accounts) openFrpOverview(ctx context.Context, gen uint64) (AccountOverview, error) {
	var user openFrpUser
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
		return AccountOverview{}, err
	}
	var proxies openFrpProxyList
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserProxies", map[string]any{}, &proxies); err != nil {
		return AccountOverview{}, err
	}
	var nodes openFrpNodeList
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getNodeList", map[string]any{}, &nodes); err != nil {
		return AccountOverview{}, err
	}
	if user.Token == "" {
		return AccountOverview{}, fmt.Errorf("OpenFrp 账户没有可用于 frpc 的用户密钥")
	}
	overview := AccountOverview{
		Provider: "openfrp",
		Account: AccountInfo{
			Provider:       "openfrp",
			Name:           user.Username,
			Group:          user.FriendlyGroup,
			Realname:       user.Realname,
			TunnelCount:    user.Used,
			TunnelLimit:    user.Proxies,
			TrafficSummary: fmt.Sprintf("剩余 %d MiB", user.Traffic),
		},
		Nodes:   make([]AccountNode, 0, len(nodes.List)),
		Tunnels: make([]AccountTunnel, 0, len(proxies.List)),
	}
	// 不查签到状态：getSignInfo 不在 OpenFrp 的公开 OPENAPI 里，签到也只跳控制台。
	for _, node := range nodes.List {
		overview.Nodes = append(overview.Nodes, AccountNode{
			ID: node.ID, Name: node.Name, Description: node.Description,
			CanCreate: node.Status == http.StatusOK && !node.FullyLoaded,
			TCP:       node.ProtocolSupport["tcp"], UDP: node.ProtocolSupport["udp"],
			PublicGameAllowed: true,
		})
	}
	for _, proxy := range proxies.List {
		proxyID := strconv.Itoa(proxy.ID)
		typeName := strings.ToLower(proxy.ProxyType)
		overview.Tunnels = append(overview.Tunnels, AccountTunnel{
			ID: proxyID, Name: proxy.ProxyName, Type: typeName, NodeName: proxy.FriendlyNode,
			LocalIP: proxy.LocalIP, LocalPort: proxy.LocalPort, Remote: proxy.ConnectAddress,
			Online: proxy.Online, Enabled: proxy.Status,
			CanImport: typeName == "tcp" || typeName == "udp",
		})
	}
	return overview, nil
}

func (a *Accounts) sakuraOverview(ctx context.Context, gen uint64) (AccountOverview, error) {
	token, err := a.sakuraTokenAt(gen)
	if err != nil {
		return AccountOverview{}, err
	}
	info, err := a.sakuraUserInfo(ctx, token)
	if err != nil {
		return AccountOverview{}, err
	}
	if err := a.checkGen("natfrp", gen); err != nil {
		return AccountOverview{}, err
	}
	nodesRaw, err := a.sakuraRequest(ctx, http.MethodGet, "/nodes", token, nil)
	if err != nil {
		return AccountOverview{}, err
	}
	var nodesMap map[string]sakuraNode
	if err := json.Unmarshal(nodesRaw, &nodesMap); err != nil {
		return AccountOverview{}, fmt.Errorf("SakuraFrp 节点列表响应格式无效")
	}
	if err := a.checkGen("natfrp", gen); err != nil {
		return AccountOverview{}, err
	}
	tunnelsRaw, err := a.sakuraRequest(ctx, http.MethodGet, "/tunnels", token, nil)
	if err != nil {
		return AccountOverview{}, err
	}
	var proxies []sakuraTunnel
	if err := json.Unmarshal(tunnelsRaw, &proxies); err != nil {
		return AccountOverview{}, fmt.Errorf("SakuraFrp 隧道列表响应格式无效")
	}
	nodeNames := make(map[int]string, len(nodesMap))
	overview := AccountOverview{
		Provider: "natfrp", Account: sakuraAccount(info),
		Nodes: make([]AccountNode, 0, len(nodesMap)), Tunnels: make([]AccountTunnel, 0, len(proxies)),
	}
	for key, node := range nodesMap {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		nodeNames[id] = node.Name
		canUDP := node.Flag&(1<<5) != 0
		overview.Nodes = append(overview.Nodes, AccountNode{
			ID: id, Name: node.Name, Description: node.Description,
			CanCreate: node.Flag&(1<<2) != 0,
			TCP:       node.Flag&(1<<2) != 0, UDP: canUDP,
			PublicGameAllowed: sakuraPublicGameNode(node),
		})
	}
	for _, proxy := range proxies {
		typeName := strings.ToLower(proxy.Type)
		remote := proxy.Remote
		if remote != "" && proxy.Node > 0 && nodeNames[proxy.Node] != "" {
			if _, err := strconv.Atoi(remote); err == nil {
				remote = nodeNames[proxy.Node] + ":" + remote
			}
		}
		overview.Tunnels = append(overview.Tunnels, AccountTunnel{
			ID: strconv.Itoa(proxy.ID), Name: proxy.Name, Type: typeName,
			NodeName: nodeNames[proxy.Node], LocalIP: proxy.LocalIP, LocalPort: proxy.LocalPort,
			Remote: remote, Online: proxy.Online, Enabled: proxy.Status == 0,
			CanImport: typeName == "tcp" || typeName == "udp",
		})
	}
	return overview, nil
}

func (a *Accounts) CreateTunnel(ctx context.Context, o AccountTunnelOptions) (Status, error) {
	if a.tunnels == nil {
		return Status{}, fmt.Errorf("隧道管理器不可用")
	}
	o.Provider = strings.ToLower(strings.TrimSpace(o.Provider))
	o.Name = strings.TrimSpace(o.Name)
	o.Type = strings.ToLower(strings.TrimSpace(o.Type))
	if o.Name == "" || len([]rune(o.Name)) > 30 {
		return Status{}, fmt.Errorf("隧道名不能为空，且不能超过 30 个字符")
	}
	if o.NodeID <= 0 || o.LocalPort < 1 || o.LocalPort > 65535 {
		return Status{}, fmt.Errorf("节点编号或本地端口无效")
	}
	if err := a.checkGuardPort(o.LocalPort); err != nil {
		return Status{}, err
	}
	if o.Type != "tcp" && o.Type != "udp" {
		return Status{}, fmt.Errorf("目前只支持创建 TCP 或 UDP 隧道")
	}
	if o.Purpose == "" {
		if o.Type == "udp" {
			o.Purpose = PurposeVoice
		} else {
			o.Purpose = PurposeGame
		}
	}
	if o.Purpose != PurposeGame && o.Purpose != PurposeVoice {
		return Status{}, fmt.Errorf("隧道用途无效")
	}
	if o.Purpose == PurposeVoice && o.Type != "udp" {
		return Status{}, fmt.Errorf("语音隧道必须使用 UDP")
	}
	var st Status
	var err error
	gen := a.generation(o.Provider)
	switch o.Provider {
	case "openfrp":
		if !openFrpTunnelName.MatchString(o.Name) {
			return Status{}, fmt.Errorf("OpenFrp 的隧道名只能用英文字母、数字、下划线和连字符，最长 30 个字符")
		}
		if o.RemotePort < 1 || o.RemotePort > 65535 {
			return Status{}, fmt.Errorf("OpenFrp 需要填写有效的公网端口")
		}
		st, err = a.createOpenFrpTunnel(ctx, o, gen)
	case "natfrp":
		if o.RemotePort < 0 || o.RemotePort > 65535 {
			return Status{}, fmt.Errorf("公网端口无效")
		}
		st, err = a.createSakuraTunnel(ctx, o, gen)
	default:
		return Status{}, fmt.Errorf("未知穿透平台: %s", o.Provider)
	}
	// 平台上可能已经建成（哪怕后面导入失败），概览一律重新拉
	a.forgetOverview(o.Provider)
	return st, err
}

func (a *Accounts) createOpenFrpTunnel(ctx context.Context, o AccountTunnelOptions, gen uint64) (Status, error) {
	var nodes openFrpNodeList
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getNodeList", map[string]any{}, &nodes); err != nil {
		return Status{}, err
	}
	var node *openFrpNode
	for i := range nodes.List {
		if nodes.List[i].ID == o.NodeID {
			node = &nodes.List[i]
			break
		}
	}
	if node == nil || node.Status != http.StatusOK || node.FullyLoaded || !node.ProtocolSupport[o.Type] {
		return Status{}, fmt.Errorf("所选 OpenFrp 节点不可用或不支持 %s", strings.ToUpper(o.Type))
	}
	var before openFrpProxyList
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserProxies", map[string]any{}, &before); err != nil {
		return Status{}, err
	}
	old := make(map[int]bool, len(before.List))
	for _, proxy := range before.List {
		old[proxy.ID] = true
	}
	request := map[string]any{
		"autoTls": "false", "custom": "", "dataEncrypt": false, "dataGzip": false,
		"domain_bind": "", "forceHttps": false, "local_addr": "127.0.0.1",
		"local_port": strconv.Itoa(o.LocalPort), "name": o.Name, "node_id": o.NodeID,
		"proxyProtocolVersion": false, "remote_port": o.RemotePort, "type": o.Type,
	}
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/newProxy", request, nil); err != nil {
		return Status{}, err
	}
	var after openFrpProxyList
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserProxies", map[string]any{}, &after); err != nil {
		return Status{}, fmt.Errorf("OpenFrp 已接受创建请求，但刷新隧道列表失败：%w", err)
	}
	var created []openFrpProxy
	for _, proxy := range after.List {
		if !old[proxy.ID] && proxy.ProxyName == o.Name && proxy.NID == o.NodeID && proxy.LocalPort == o.LocalPort && strings.EqualFold(proxy.ProxyType, o.Type) {
			created = append(created, proxy)
		}
	}
	if len(created) != 1 {
		return Status{}, fmt.Errorf("OpenFrp 已创建隧道，但无法唯一识别新隧道；请刷新平台隧道列表后手动导入")
	}
	var user openFrpUser
	if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
		return Status{}, fmt.Errorf("OpenFrp 隧道已创建，但读取客户端密钥失败；请刷新后手动导入：%w", err)
	}
	return a.addLocalTunnel("openfrp", strconv.Itoa(created[0].ID), created[0].ProxyName, user.Token, o, gen)
}

func (a *Accounts) createSakuraTunnel(ctx context.Context, o AccountTunnelOptions, gen uint64) (Status, error) {
	token, err := a.sakuraTokenAt(gen)
	if err != nil {
		return Status{}, err
	}
	var info sakuraUserInfo
	if err := a.sakuraGet(ctx, "/user/info", token, &info); err != nil {
		return Status{}, err
	}
	var nodes map[string]sakuraNode
	if err := a.sakuraGet(ctx, "/nodes", token, &nodes); err != nil {
		return Status{}, err
	}
	node, ok := nodes[strconv.Itoa(o.NodeID)]
	if !ok || node.Flag&(1<<2) == 0 {
		return Status{}, fmt.Errorf("所选 SakuraFrp 节点不可创建隧道")
	}
	if o.Type == "udp" && node.Flag&(1<<5) == 0 {
		return Status{}, fmt.Errorf("所选 SakuraFrp 节点不支持 UDP")
	}
	// 樱花官方 API 文档的规定：公开游戏服务器只能用 flag&2048 != 0 或名称含「多线」的节点（「三线」不算），
	// 违反会被封号且不退款。
	if o.PublicServer && !sakuraPublicGameNode(node) {
		return Status{}, fmt.Errorf("公开游戏服务器只能用樱花的公开服专用节点（名称含「多线」或标有公开游戏标记的节点）")
	}
	request := map[string]any{
		"name": o.Name, "type": o.Type, "node": o.NodeID,
		"local_ip": "127.0.0.1", "local_port": o.LocalPort,
	}
	if o.RemotePort > 0 {
		request["remote"] = strconv.Itoa(o.RemotePort)
	}
	if err := a.checkGen("natfrp", gen); err != nil {
		return Status{}, err
	}
	var created sakuraTunnel
	if err := a.sakuraRequestJSON(ctx, http.MethodPost, "/tunnels", token, request, &created); err != nil {
		return Status{}, err
	}
	if created.ID <= 0 {
		return Status{}, fmt.Errorf("SakuraFrp 已返回成功，但没有返回新隧道编号；请刷新平台隧道列表后手动导入")
	}
	if info.Token == "" {
		return Status{}, fmt.Errorf("SakuraFrp 隧道已创建，但账户没有可用于 frpc 的访问密钥")
	}
	return a.addLocalTunnel("natfrp", strconv.Itoa(created.ID), created.Name, info.Token, o, gen)
}

func (a *Accounts) ImportTunnel(ctx context.Context, o AccountTunnelImport) (Status, error) {
	if a.tunnels == nil {
		return Status{}, fmt.Errorf("隧道管理器不可用")
	}
	o.Provider = strings.ToLower(strings.TrimSpace(o.Provider))
	// 编号统一写成平台的十进制形式：「007」「+7」都能被 Atoi 认成 7，原样存下会绕过下面的重复检查，还会原样进 frpc 的参数
	id, err := strconv.Atoi(strings.TrimSpace(o.ProxyID))
	if err != nil || id <= 0 || strings.ContainsAny(strings.TrimSpace(o.ProxyID), "+-") {
		return Status{}, fmt.Errorf("隧道编号无效")
	}
	o.ProxyID = strconv.Itoa(id)
	gen := a.generation(o.Provider)
	for _, local := range a.tunnels.List() {
		if local.Provider == o.Provider && local.ProxyID == o.ProxyID {
			return Status{}, fmt.Errorf("这条平台隧道已经添加到 MineDeck")
		}
	}
	var remote AccountTunnel
	var credential string
	switch o.Provider {
	case "openfrp":
		var list openFrpProxyList
		if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserProxies", map[string]any{}, &list); err != nil {
			return Status{}, err
		}
		for _, proxy := range list.List {
			if proxy.ID == id {
				remote = AccountTunnel{ID: o.ProxyID, Name: proxy.ProxyName, Type: strings.ToLower(proxy.ProxyType), NodeName: proxy.FriendlyNode, LocalPort: proxy.LocalPort}
				break
			}
		}
		if remote.ID == "" {
			return Status{}, fmt.Errorf("OpenFrp 账户中找不到这条隧道")
		}
		var user openFrpUser
		if err := a.openFrpPostAt(ctx, &gen, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
			return Status{}, err
		}
		credential = user.Token
	case "natfrp":
		token, err := a.sakuraTokenAt(gen)
		if err != nil {
			return Status{}, err
		}
		var list []sakuraTunnel
		if err := a.sakuraGet(ctx, "/tunnels", token, &list); err != nil {
			return Status{}, err
		}
		for _, proxy := range list {
			if proxy.ID == id {
				remote = AccountTunnel{ID: o.ProxyID, Name: proxy.Name, Type: strings.ToLower(proxy.Type), LocalIP: proxy.LocalIP, LocalPort: proxy.LocalPort}
				break
			}
		}
		if remote.ID == "" {
			return Status{}, fmt.Errorf("SakuraFrp 账户中找不到这条隧道")
		}
		if err := a.checkGen("natfrp", gen); err != nil {
			return Status{}, err
		}
		var info sakuraUserInfo
		if err := a.sakuraGet(ctx, "/user/info", token, &info); err != nil {
			return Status{}, err
		}
		credential = info.Token
	default:
		return Status{}, fmt.Errorf("未知穿透平台: %s", o.Provider)
	}
	if remote.Type != "tcp" && remote.Type != "udp" {
		return Status{}, fmt.Errorf("MineDeck 只导入 TCP 或 UDP 隧道")
	}
	if a.checkGuardPort(remote.LocalPort) != nil {
		return Status{}, fmt.Errorf("这条平台隧道的本地端口 %d 是 MineDeck 自己的网页端口，不能导入：映射到公网后管理界面会暴露出去", remote.LocalPort)
	}
	if credential == "" {
		return Status{}, fmt.Errorf("平台没有返回 frpc 访问密钥")
	}
	options := AccountTunnelOptions{Provider: o.Provider, Name: remote.Name, Bound: o.Bound, AutoStart: o.AutoStart, Purpose: o.Purpose}
	if options.Purpose == "" {
		if remote.Type == "udp" {
			options.Purpose = PurposeVoice
		} else {
			options.Purpose = PurposeGame
		}
	}
	st, err := a.addLocalTunnel(o.Provider, o.ProxyID, remote.Name, credential, options, gen)
	if err == nil {
		a.forgetOverview(o.Provider)
	}
	return st, err
}

// addLocalTunnel 把平台隧道写进本机。gen 是整个创建/导入开始时的连接代数：途中账户被断开或换了，
// 前后几步可能用的不是同一个账户，不再写进本机。
func (a *Accounts) addLocalTunnel(provider, proxyID, name, credential string, o AccountTunnelOptions, gen uint64) (Status, error) {
	if credential == "" {
		return Status{}, fmt.Errorf("平台没有返回 frpc 访问密钥")
	}
	// 代数检查、查重和写入在同一把锁里：检查通过以后才断开的，或者两个导入同时进来的，都不会漏过去
	a.mu.Lock()
	if a.generationLocked(provider) != gen {
		a.mu.Unlock()
		return Status{}, fmt.Errorf("操作途中账户被断开或换了，没有写进 MineDeck；平台上的隧道可能已经建好，请重新连接后在概览里导入")
	}
	for _, local := range a.tunnels.List() {
		if local.Provider == provider && local.ProxyID == proxyID {
			a.mu.Unlock()
			return local, nil
		}
	}
	added, err := a.tunnels.Add(Tunnel{
		Provider: provider, Name: name, Credential: credential, ProxyID: proxyID,
		Bound: o.Bound, AutoStart: o.AutoStart, Purpose: o.Purpose,
	})
	a.mu.Unlock()
	if err != nil {
		return Status{}, fmt.Errorf("保存到 MineDeck 失败: %w", err)
	}
	for _, local := range a.tunnels.List() {
		if local.ID == added.ID {
			return local, nil
		}
	}
	return Status{Tunnel: *added}, nil
}

// openFrpPostAt 调 OpenFrp 的接口。want 非空时，这次请求必须用 *want 那一代的账户：
// 一次多步操作途中账户被断开或换了，就不再用新账户的凭据发后面的请求。
func (a *Accounts) openFrpPostAt(ctx context.Context, want *uint64, path string, body any, out any) error {
	// 凭据和代数在同一把锁里读：读完凭据才断开的话，代数也已经对不上，换发的新值不会把账户连回去
	a.mu.Lock()
	gen := a.openGen
	if want != nil && *want != gen {
		a.mu.Unlock()
		return errAccountChanged
	}
	authorization, err := a.keys.Get(openFrpAuthKey)
	a.mu.Unlock()
	if err != nil {
		return fmt.Errorf("读取 OpenFrp 授权失败: %w", err)
	}
	if authorization == "" {
		return fmt.Errorf("请先连接 OpenFrp 账户")
	}
	data, headers, status, err := a.request(ctx, http.MethodPost, a.openAPI, path, authorization, body, true)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("OpenFrp 登录已失效，请重新连接账户")
	}
	if status < 200 || status >= 300 {
		return responseError(status, data, authorization)
	}
	// OpenFrp 文档说请求任意接口时「有概率返回新值」，但没写放在哪；这里按响应头 Authorization 收。
	// 只认成功响应里的、像样的新值，并且这期间账户没被断开或重新登录过，否则宁可不收。
	if rotated := strings.TrimSpace(headers.Get("Authorization")); rotated != "" && rotated != authorization && validAuthorization(rotated) {
		a.mu.Lock()
		if a.openGen == gen {
			err = a.keys.Set(openFrpAuthKey, rotated)
		}
		a.mu.Unlock()
		if err != nil {
			return fmt.Errorf("更新 OpenFrp 授权失败: %w", err)
		}
	}
	var envelope struct {
		Flag bool            `json:"flag"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("OpenFrp API 响应格式无效")
	}
	if !envelope.Flag {
		return fmt.Errorf("OpenFrp API 请求失败: %s", safeMessage(scrub(envelope.Msg, authorization)))
	}
	if out != nil && len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("OpenFrp API 数据格式无效")
		}
	}
	return nil
}

// checkGen 一次多步操作的下一步之前确认账户还是开始时那一个。
func (a *Accounts) checkGen(provider string, gen uint64) error {
	if a.generation(provider) != gen {
		return errAccountChanged
	}
	return nil
}

// sakuraTokenAt 取第 gen 代的樱花访问密钥：代数和密钥在同一把锁里读，账户已经换了就不给。
func (a *Accounts) sakuraTokenAt(gen uint64) (string, error) {
	a.mu.Lock()
	if a.sakuraGen != gen {
		a.mu.Unlock()
		return "", errAccountChanged
	}
	token, err := a.keys.Get(sakuraAuthKey)
	a.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("读取 SakuraFrp 访问密钥失败: %w", err)
	}
	if token == "" {
		return "", fmt.Errorf("请先连接 SakuraFrp 账户")
	}
	return token, nil
}

func (a *Accounts) sakuraUserInfo(ctx context.Context, token string) (sakuraUserInfo, error) {
	var info sakuraUserInfo
	if err := a.sakuraGet(ctx, "/user/info", token, &info); err != nil {
		return sakuraUserInfo{}, err
	}
	return info, nil
}

func (a *Accounts) sakuraGet(ctx context.Context, path, token string, out any) error {
	data, err := a.sakuraRequest(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("SakuraFrp API 响应格式无效")
	}
	return nil
}

func (a *Accounts) sakuraRequestJSON(ctx context.Context, method, path, token string, body, out any) error {
	data, err := a.sakuraRequest(ctx, method, path, token, body)
	if err != nil {
		return err
	}
	if out != nil && len(data) != 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("SakuraFrp API 响应格式无效")
		}
	}
	return nil
}

func (a *Accounts) sakuraRequest(ctx context.Context, method, path, token string, body any) ([]byte, error) {
	data, _, status, err := a.request(ctx, method, a.sakuraAPI, path, "Bearer "+token, body, false)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, responseError(status, data, token)
	}
	var apiErr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(data, &apiErr) == nil && apiErr.Code >= 400 {
		return nil, fmt.Errorf("SakuraFrp API 请求失败: %s", safeMessage(scrub(apiErr.Msg, token)))
	}
	return data, nil
}

func (a *Accounts) request(ctx context.Context, method, base, path, authorization string, body any, openFrp bool) ([]byte, http.Header, int, error) {
	data, headers, status, err := a.doJSON(ctx, a.client, method, base, path, authorization, body)
	if err != nil {
		if openFrp {
			return nil, nil, 0, fmt.Errorf("连接 OpenFrp 失败: %w", err)
		}
		return nil, nil, 0, fmt.Errorf("连接 SakuraFrp 失败: %w", err)
	}
	return data, headers, status, nil
}

// doJSON 用指定 client 发 JSON 请求。
func (a *Accounts) doJSON(ctx context.Context, client *http.Client, method, base, path, authorization string, body any) ([]byte, http.Header, int, error) {
	if client == nil {
		client = a.client
	}
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, 0, err
		}
		r = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, r)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("构造穿透平台请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", a.userAgent)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("读取穿透平台响应失败: %w", err)
	}
	return data, resp.Header, resp.StatusCode, nil
}

// responseError 平台的错误响应。secrets 是这次请求带的凭据：平台万一在报错里回显，先抹掉再给人看、进记录。
func responseError(status int, data []byte, secrets ...string) error {
	var resp struct {
		Msg  string `json:"msg"`
		Data string `json:"data"`
	}
	_ = json.Unmarshal(data, &resp)
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("穿透平台说请求太频繁（HTTP 429），稍等一会儿再试")
	}
	if msg := safeMessage(scrub(resp.Msg, secrets...)); msg != "" {
		return fmt.Errorf("穿透平台返回 HTTP %d: %s", status, msg)
	}
	return fmt.Errorf("穿透平台返回 HTTP %d", status)
}

// scrub 把文本里出现的凭据换成 ***。
func scrub(s string, secrets ...string) string {
	for _, v := range secrets {
		if v = strings.TrimSpace(strings.TrimPrefix(v, "Bearer ")); len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

// safeMessage 平台报错原文去空白、截到 120 个字符（按字符截，不把中文切成半个字）。
func safeMessage(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return s
}

// sakuraPublicGameNode 樱花官方 API 文档对公开游戏服务器节点的判定：flag&2048 != 0，或名称含「多线」（「三线」不算）。
func sakuraPublicGameNode(n sakuraNode) bool {
	return n.Flag&(1<<11) != 0 || strings.Contains(n.Name, "多线")
}

type openFrpUser struct {
	Token         string `json:"token"`
	Username      string `json:"username"`
	FriendlyGroup string `json:"friendlyGroup"`
	Realname      bool   `json:"realname"`
	Traffic       int64  `json:"traffic"`
	Used          int    `json:"used"`
	Proxies       int    `json:"proxies"`
}

type openFrpProxyList struct {
	List []openFrpProxy `json:"list"`
}

type openFrpProxy struct {
	ID             int    `json:"id"`
	ProxyName      string `json:"proxyName"`
	ProxyType      string `json:"proxyType"`
	FriendlyNode   string `json:"friendlyNode"`
	LocalIP        string `json:"localIp"`
	LocalPort      int    `json:"localPort"`
	ConnectAddress string `json:"connectAddress"`
	NID            int    `json:"nid"`
	Online         bool   `json:"online"`
	Status         bool   `json:"status"`
}

type openFrpNodeList struct {
	List []openFrpNode `json:"list"`
}

type openFrpNode struct {
	ID              int             `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Status          int             `json:"status"`
	ProtocolSupport map[string]bool `json:"protocolSupport"`
	FullyLoaded     bool            `json:"fullyLoaded"`
}

type sakuraUserInfo struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	Realname int    `json:"realname"`
	Tunnels  int    `json:"tunnels"`
	Group    struct {
		Name string `json:"name"`
	} `json:"group"`
	Traffic []int64 `json:"traffic"`
	Sign    struct {
		Signed bool `json:"signed"`
		Days   int  `json:"days"`
	} `json:"sign"`
}

type sakuraNode struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Description string `json:"description"`
	Flag        int    `json:"flag"`
}

type sakuraTunnel struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Node      int    `json:"node"`
	Type      string `json:"type"`
	Status    int    `json:"status"`
	Online    bool   `json:"online"`
	LocalIP   string `json:"local_ip"`
	LocalPort int    `json:"local_port"`
	Remote    string `json:"remote"`
}

func sakuraAccount(info sakuraUserInfo) AccountInfo {
	traffic := ""
	if len(info.Traffic) >= 2 {
		traffic = fmt.Sprintf("今日已用 %d · 剩余 %d", info.Traffic[0], info.Traffic[1])
	}
	signed := info.Sign.Signed
	return AccountInfo{
		Provider: "natfrp", Name: info.Name, Group: info.Group.Name,
		Realname: info.Realname != 0, TunnelLimit: info.Tunnels,
		TrafficSummary: traffic, SignedToday: &signed, ConsecutiveDays: info.Sign.Days,
	}
}
