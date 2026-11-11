package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	// OpenFrp 人机验证：控制台前端加载的 CaptchaSDK（禁止自动签到，只能由用户手动点）。
	openFrpCaptchaAppID = "app_a287037f741b44eea03ced98452dc7cc"
	openFrpCaptchaSDK   = "https://cv.naids.com/sdk/load.min.js"
	openFrpPremiumURL   = "https://console.openfrp.net/premium"
	openFrpAccountURL   = "https://account.naids.com/dashboard"
	openFrpSignURL      = "https://console.openfrp.net/dashboard?action=sign"
	sakuraUserURL       = "https://www.natfrp.com/user/"
	sakuraProfileURL    = "https://www.natfrp.com/user/profile"
	sakuraShopURL       = "https://www.natfrp.com/shop"
	sakuraTunnelShop    = "https://www.natfrp.com/tunnel/shop"
	sakuraPaymentFAQ    = "https://doc.natfrp.com/faq/payment.html"
	// 面板→OpenID OAuth 入口（未登录 GET 会 302 到 openid.13a.com/oauth/authorize?...）
	sakuraWebLoginURL = "https://www.natfrp.com/cgi/user/login"
	sakuraOpenIDAuth  = "https://openid.13a.com/oauth/authorize?response_type=code&client_id=PYoPownjQEAJYNbgwn7EZ2goa3kMaxQjBsuvTUmE"
)

type Accounts struct {
	tunnels   *Manager
	keys      *secret.Store
	client    *http.Client
	openAPI   string
	openLogin string
	sakuraAPI string
	userAgent string
	now       func() time.Time

	mu      sync.Mutex
	pending map[string]*openFrpLoginState
}

type openFrpLoginState struct {
	requestUUID string
	privateKey  *[32]byte
	expires     time.Time
	lastPoll    time.Time
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
	}
}

type AccountStatus struct {
	OpenFrp bool `json:"openfrp"`
	Sakura  bool `json:"natfrp"`
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

// WebLoginInfo 引导用户完成网页授权/登录。Sakura 目前只能打开面板 CGI→OpenID，登录后仍需粘贴访问密钥。
type WebLoginInfo struct {
	Provider  string `json:"provider"`
	URL       string `json:"url"`
	Authorize string `json:"authorize,omitempty"` // OpenID authorize 直链（对照用）
	Message   string `json:"message,omitempty"`
	NeedToken bool   `json:"needToken,omitempty"`
}

// CaptchaInfo 前端拉起人机验证所需的配置。OpenFrp 用 CaptchaSDK.open() 拿 token；Sakura 暂无密钥可用的验证码签到。
type CaptchaInfo struct {
	Provider string `json:"provider"`
	Vendor   string `json:"vendor,omitempty"` // OpenFrp：naids
	AppID    string `json:"appId,omitempty"`
	SDKURL   string `json:"sdkUrl,omitempty"`
	Mode     string `json:"mode"`              // token：前端把隔离 iframe 返回的 token 交给 Sign；session：需浏览器 SESSION
	OpenURL  string `json:"openURL,omitempty"` // SDK 失败时打开官网手动签到
}

// SignPayload 签到请求。OpenFrp 必填 Token（人机验证结果）；Sakura 目前访问密钥无法签到。
type SignPayload struct {
	Token string `json:"token,omitempty"`
}

// SignResult 签到结果。禁止自动签到：本接口只在用户手动操作后由前端调用。
type SignResult struct {
	OK              bool   `json:"ok"`
	Message         string `json:"message,omitempty"`
	NeedSession     bool   `json:"needSession,omitempty"`
	OpenURL         string `json:"openURL,omitempty"`
	SignedToday     *bool  `json:"signedToday,omitempty"`
	ConsecutiveDays int    `json:"consecutiveDays,omitempty"`
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

func (a *Accounts) Status() AccountStatus {
	open, _ := a.keys.State(openFrpAuthKey)
	sakura, _ := a.keys.State(sakuraAuthKey)
	return AccountStatus{OpenFrp: open, Sakura: sakura}
}

func (a *Accounts) BeginOpenFrpLogin(ctx context.Context) (OpenFrpLogin, error) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return OpenFrpLogin{}, fmt.Errorf("生成 OpenFrp 授权密钥失败: %w", err)
	}
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
	clear(privateKey[:])
	if err != nil {
		return OpenFrpLoginPoll{}, fmt.Errorf("解密 OpenFrp 授权信息失败: %w", err)
	}
	authorization := strings.TrimSpace(string(plain))
	clear(plain)
	if len(authorization) < 8 || len(authorization) > 4096 || strings.ContainsAny(authorization, "\r\n") {
		return OpenFrpLoginPoll{}, fmt.Errorf("OpenFrp 返回的授权信息无效")
	}
	if err := a.keys.Set(openFrpAuthKey, authorization); err != nil {
		return OpenFrpLoginPoll{}, fmt.Errorf("保存 OpenFrp 授权失败: %w", err)
	}
	a.mu.Lock()
	if current := a.pending[id]; current == state {
		clear(state.privateKey[:])
		delete(a.pending, id)
	}
	a.mu.Unlock()
	return OpenFrpLoginPoll{Pending: false}, nil
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
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\r\n") {
		return AccountInfo{}, fmt.Errorf("请填写有效的 SakuraFrp 访问密钥")
	}
	info, err := a.sakuraUserInfo(ctx, token)
	if err != nil {
		return AccountInfo{}, err
	}
	if info.Token == "" || info.Name == "" {
		return AccountInfo{}, fmt.Errorf("SakuraFrp 未返回有效的访问账户信息")
	}
	if err := a.keys.Set(sakuraAuthKey, token); err != nil {
		return AccountInfo{}, fmt.Errorf("保存 SakuraFrp 访问密钥失败: %w", err)
	}
	return sakuraAccount(info), nil
}

func (a *Accounts) Disconnect(provider string) error {
	switch provider {
	case "openfrp":
		if err := a.keys.Set(openFrpAuthKey, ""); err != nil {
			return err
		}
		a.mu.Lock()
		for id, state := range a.pending {
			clear(state.privateKey[:])
			delete(a.pending, id)
		}
		a.mu.Unlock()
	case "natfrp":
		if err := a.keys.Set(sakuraAuthKey, ""); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知穿透平台: %s", provider)
	}
	return nil
}

func (a *Accounts) Overview(ctx context.Context, provider string) (AccountOverview, error) {
	switch provider {
	case "openfrp":
		return a.openFrpOverview(ctx)
	case "natfrp":
		return a.sakuraOverview(ctx)
	default:
		return AccountOverview{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

// CaptchaInfo 返回前端拉起签到人机验证所需配置。禁止自动签到。
func (a *Accounts) CaptchaInfo(provider string) (CaptchaInfo, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openfrp":
		return CaptchaInfo{
			Provider: "openfrp",
			Vendor:   "naids",
			AppID:    openFrpCaptchaAppID,
			SDKURL:   openFrpCaptchaSDK,
			Mode:     "token",
			OpenURL:  "https://console.openfrp.net/dashboard?action=sign",
		}, nil
	case "natfrp":
		// Sakura /user/sign 仅支持 SESSION；访问密钥无法完成验证码签到。
		return CaptchaInfo{Provider: "natfrp", Mode: "session", OpenURL: sakuraUserURL}, nil
	default:
		return CaptchaInfo{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

// WebLogin 返回网页登录/授权入口。Sakura：面板 /cgi/user/login → OpenID OAuth；成功后暂需用户粘贴访问密钥。
func (a *Accounts) WebLogin(provider string) (WebLoginInfo, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "natfrp":
		return WebLoginInfo{
			Provider:  "natfrp",
			URL:       sakuraWebLoginURL,
			Authorize: sakuraOpenIDAuth,
			NeedToken: true,
			Message:   "将打开樱花面板登录入口（跳转 Nyatwork OpenID）。登录成功后请回到 MineDeck，从用户信息页复制访问密钥并粘贴连接。自动换票尚需抓包补齐。",
		}, nil
	default:
		return WebLoginInfo{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

// Sign 手动签到。OpenFrp 用前端 CaptchaSDK 的 token 调 userSign；Sakura 访问密钥不可用时引导去官网。
// 禁止自动签到（违反 OpenFrp 服务条款）。
func (a *Accounts) Sign(ctx context.Context, provider string, payload SignPayload) (SignResult, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openfrp":
		return a.signOpenFrp(ctx, payload)
	case "natfrp":
		return a.signSakura(ctx)
	default:
		return SignResult{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

func (a *Accounts) signOpenFrp(ctx context.Context, payload SignPayload) (SignResult, error) {
	token := strings.TrimSpace(payload.Token)
	if token == "" || len(token) > 8192 || strings.ContainsAny(token, "\r\n") {
		return SignResult{}, fmt.Errorf("请先完成人机验证后再签到")
	}
	var raw json.RawMessage
	if err := a.openFrpPost(ctx, "/frp/api/userSign", map[string]any{"token": token}, &raw); err != nil {
		return SignResult{}, err
	}
	msg := "签到成功"
	var s string
	if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
		msg = strings.TrimSpace(s)
	}
	result := SignResult{OK: true, Message: msg}
	if info, err := a.openFrpSignInfo(ctx); err == nil {
		result.SignedToday = &info.signed
		result.ConsecutiveDays = info.total
	}
	return result, nil
}

type openFrpSignState struct {
	signed bool
	total  int
}

func (a *Accounts) openFrpSignInfo(ctx context.Context) (openFrpSignState, error) {
	var raw map[string]any
	if err := a.openFrpPost(ctx, "/frp/api/getSignInfo", map[string]any{}, &raw); err != nil {
		return openFrpSignState{}, err
	}
	st := openFrpSignState{}
	if v, ok := raw["totalsign"]; ok {
		switch n := v.(type) {
		case float64:
			st.total = int(n)
		case json.Number:
			i, _ := n.Int64()
			st.total = int(i)
		}
	}
	signdate, _ := raw["signdate"].(string)
	if signdate != "" {
		if t, err := time.ParseInLocation("2006-01-02", signdate[:min(len(signdate), 10)], time.Local); err == nil {
			now := a.now()
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			st.signed = !t.Before(start)
		}
	}
	return st, nil
}

func (a *Accounts) signSakura(ctx context.Context) (SignResult, error) {
	if _, err := a.sakuraToken(); err != nil {
		return SignResult{}, err
	}
	// TODO(frp-api-research): Sakura /v4/user/sign 明确「仅支持 SESSION 鉴权」，访问密钥会 401。
	// 等调研补齐 SESSION cookie / 请求体后，可在此用浏览器登录态完成签到；当前稳定返回引导。
	_ = ctx
	return SignResult{
		OK:          false,
		NeedSession: true,
		OpenURL:     sakuraUserURL,
		Message:     "SakuraFrp 签到需要官网登录会话，访问密钥无法完成；请在浏览器打开用户中心手动签到",
	}, nil
}

// PurchaseOptions 返回购买/充值跳转入口。公开 OPENAPI 无支付接口时只给官方页 URL。
func (a *Accounts) PurchaseOptions(ctx context.Context, provider string) (PurchaseOptionsResult, error) {
	_ = ctx
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
				{Title: "获取访问密钥", URL: "https://www.natfrp.com/user/profile", Kind: "token"},
				{Title: "付款说明", URL: "https://doc.natfrp.com/faq/payment.html", Kind: "docs"},
			},
		}, nil
	default:
		return PurchaseOptionsResult{}, fmt.Errorf("未知穿透平台: %s", provider)
	}
}

func (a *Accounts) openFrpOverview(ctx context.Context) (AccountOverview, error) {
	var user openFrpUser
	if err := a.openFrpPost(ctx, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
		return AccountOverview{}, err
	}
	var proxies openFrpProxyList
	if err := a.openFrpPost(ctx, "/frp/api/getUserProxies", map[string]any{}, &proxies); err != nil {
		return AccountOverview{}, err
	}
	var nodes openFrpNodeList
	if err := a.openFrpPost(ctx, "/frp/api/getNodeList", map[string]any{}, &nodes); err != nil {
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
	if info, err := a.openFrpSignInfo(ctx); err == nil {
		signed := info.signed
		overview.Account.SignedToday = &signed
		overview.Account.ConsecutiveDays = info.total
	}
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

func (a *Accounts) sakuraOverview(ctx context.Context) (AccountOverview, error) {
	token, err := a.sakuraToken()
	if err != nil {
		return AccountOverview{}, err
	}
	info, err := a.sakuraUserInfo(ctx, token)
	if err != nil {
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
		publicGameAllowed := node.Flag&(1<<11) != 0 || strings.Contains(node.Name, "多线")
		overview.Nodes = append(overview.Nodes, AccountNode{
			ID: id, Name: node.Name, Description: node.Description,
			CanCreate: node.Flag&(1<<2) != 0,
			TCP:       node.Flag&(1<<2) != 0, UDP: canUDP,
			PublicGameAllowed: publicGameAllowed,
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
	if o.Provider == "openfrp" {
		if o.RemotePort < 1 || o.RemotePort > 65535 {
			return Status{}, fmt.Errorf("OpenFrp 需要填写有效的公网端口")
		}
		return a.createOpenFrpTunnel(ctx, o)
	}
	if o.Provider == "natfrp" {
		if o.RemotePort < 0 || o.RemotePort > 65535 {
			return Status{}, fmt.Errorf("公网端口无效")
		}
		return a.createSakuraTunnel(ctx, o)
	}
	return Status{}, fmt.Errorf("未知穿透平台: %s", o.Provider)
}

func (a *Accounts) createOpenFrpTunnel(ctx context.Context, o AccountTunnelOptions) (Status, error) {
	var nodes openFrpNodeList
	if err := a.openFrpPost(ctx, "/frp/api/getNodeList", map[string]any{}, &nodes); err != nil {
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
	if err := a.openFrpPost(ctx, "/frp/api/getUserProxies", map[string]any{}, &before); err != nil {
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
	if err := a.openFrpPost(ctx, "/frp/api/newProxy", request, nil); err != nil {
		return Status{}, err
	}
	var after openFrpProxyList
	if err := a.openFrpPost(ctx, "/frp/api/getUserProxies", map[string]any{}, &after); err != nil {
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
	if err := a.openFrpPost(ctx, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
		return Status{}, fmt.Errorf("OpenFrp 隧道已创建，但读取客户端密钥失败；请刷新后手动导入：%w", err)
	}
	return a.addLocalTunnel("openfrp", strconv.Itoa(created[0].ID), created[0].ProxyName, user.Token, o)
}

func (a *Accounts) createSakuraTunnel(ctx context.Context, o AccountTunnelOptions) (Status, error) {
	token, err := a.sakuraToken()
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
	if o.PublicServer && node.Flag&(1<<11) == 0 && !strings.Contains(node.Name, "多线") {
		return Status{}, fmt.Errorf("SakuraFrp 公开游戏服务器只能使用标记为公开服专用或名称含「多线」的节点")
	}
	request := map[string]any{
		"name": o.Name, "type": o.Type, "node": o.NodeID,
		"local_ip": "127.0.0.1", "local_port": o.LocalPort,
	}
	if o.RemotePort > 0 {
		request["remote"] = strconv.Itoa(o.RemotePort)
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
	return a.addLocalTunnel("natfrp", strconv.Itoa(created.ID), created.Name, info.Token, o)
}

func (a *Accounts) ImportTunnel(ctx context.Context, o AccountTunnelImport) (Status, error) {
	if a.tunnels == nil {
		return Status{}, fmt.Errorf("隧道管理器不可用")
	}
	o.Provider = strings.ToLower(strings.TrimSpace(o.Provider))
	o.ProxyID = strings.TrimSpace(o.ProxyID)
	if o.ProxyID == "" || len(o.ProxyID) > 32 {
		return Status{}, fmt.Errorf("隧道编号无效")
	}
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
		if err := a.openFrpPost(ctx, "/frp/api/getUserProxies", map[string]any{}, &list); err != nil {
			return Status{}, err
		}
		id, _ := strconv.Atoi(o.ProxyID)
		for _, proxy := range list.List {
			if proxy.ID == id {
				remote = AccountTunnel{ID: o.ProxyID, Name: proxy.ProxyName, Type: strings.ToLower(proxy.ProxyType), NodeName: proxy.FriendlyNode}
				break
			}
		}
		if remote.ID == "" {
			return Status{}, fmt.Errorf("OpenFrp 账户中找不到这条隧道")
		}
		var user openFrpUser
		if err := a.openFrpPost(ctx, "/frp/api/getUserInfo", map[string]any{}, &user); err != nil {
			return Status{}, err
		}
		credential = user.Token
	case "natfrp":
		token, err := a.sakuraToken()
		if err != nil {
			return Status{}, err
		}
		var list []sakuraTunnel
		if err := a.sakuraGet(ctx, "/tunnels", token, &list); err != nil {
			return Status{}, err
		}
		id, _ := strconv.Atoi(o.ProxyID)
		for _, proxy := range list {
			if proxy.ID == id {
				remote = AccountTunnel{ID: o.ProxyID, Name: proxy.Name, Type: strings.ToLower(proxy.Type), LocalIP: proxy.LocalIP, LocalPort: proxy.LocalPort}
				break
			}
		}
		if remote.ID == "" {
			return Status{}, fmt.Errorf("SakuraFrp 账户中找不到这条隧道")
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
	return a.addLocalTunnel(o.Provider, o.ProxyID, remote.Name, credential, options)
}

func (a *Accounts) addLocalTunnel(provider, proxyID, name, credential string, o AccountTunnelOptions) (Status, error) {
	if credential == "" {
		return Status{}, fmt.Errorf("平台没有返回 frpc 访问密钥")
	}
	for _, local := range a.tunnels.List() {
		if local.Provider == provider && local.ProxyID == proxyID {
			return local, nil
		}
	}
	added, err := a.tunnels.Add(Tunnel{
		Provider: provider, Name: name, Credential: credential, ProxyID: proxyID,
		Bound: o.Bound, AutoStart: o.AutoStart, Purpose: o.Purpose,
	})
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

func (a *Accounts) openFrpPost(ctx context.Context, path string, body any, out any) error {
	authorization, err := a.keys.Get(openFrpAuthKey)
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
	if rotated := strings.TrimSpace(headers.Get("Authorization")); rotated != "" && rotated != authorization {
		if err := a.keys.Set(openFrpAuthKey, rotated); err != nil {
			return fmt.Errorf("更新 OpenFrp 授权失败: %w", err)
		}
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("OpenFrp 登录已失效，请重新连接账户")
	}
	if status < 200 || status >= 300 {
		return responseError(status, data)
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
		return fmt.Errorf("OpenFrp API 请求失败: %s", safeMessage(envelope.Msg))
	}
	if out != nil && len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("OpenFrp API 数据格式无效")
		}
	}
	return nil
}

func (a *Accounts) sakuraToken() (string, error) {
	token, err := a.keys.Get(sakuraAuthKey)
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
		return nil, responseError(status, data)
	}
	var apiErr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(data, &apiErr) == nil && apiErr.Code >= 400 {
		return nil, fmt.Errorf("SakuraFrp API 请求失败: %s", safeMessage(apiErr.Msg))
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

func responseError(status int, data []byte) error {
	var resp struct {
		Msg  string `json:"msg"`
		Data string `json:"data"`
	}
	_ = json.Unmarshal(data, &resp)
	if msg := safeMessage(resp.Msg); msg != "" {
		return fmt.Errorf("穿透平台返回 HTTP %d: %s", status, msg)
	}
	return fmt.Errorf("穿透平台返回 HTTP %d", status)
}

func safeMessage(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		s = s[:240]
	}
	return s
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
