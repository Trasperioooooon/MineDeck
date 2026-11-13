package ai

import (
	"net/url"
	"strings"
)

// AuthMode 鉴权头。空 = 这个协议的默认：OpenAI 兼容发 Authorization: Bearer；Claude 协议发 x-api-key，
// 中转另带 Bearer（见 claudeHeader）。有的服务商（小米 MiMo）认 api-key，两种都写进文档的用 both。
type AuthMode string

const (
	AuthBearer AuthMode = "bearer" // Authorization: Bearer
	AuthAPIKey AuthMode = "api-key"
	AuthBoth   AuthMode = "both" // Bearer 和 api-key 都带
)

func (a AuthMode) valid() bool { return a == "" || a == AuthBearer || a == AuthAPIKey || a == AuthBoth }

// EchoMode 多轮时要不要把 reasoning_content 带回。
// off 不带；on 带上，没收到的补一个空串（补哪些见 Preset.echoPadAll；DeepSeek / MiMo 思考模式不带会 400）；
// auto 收到过就带，若对方报「不认识这个字段」再去掉重发一次（这个结论在 Service 里记住，之后的请求直接不带）。
type EchoMode string

const (
	EchoOff  EchoMode = "off"
	EchoOn   EchoMode = "on"
	EchoAuto EchoMode = "auto"
)

func (e EchoMode) valid() bool { return e == "" || e == EchoOff || e == EchoOn || e == EchoAuto }

// KeyHint 某种 Key 前缀该配哪些地址。对不上时只提醒，不拦截保存。
// 只比主机名：Key 绑定的是域名，地址后面多一截路径（/v1/chat/completions）不算对不上。
type KeyHint struct {
	Prefixes []string `json:"prefixes"`
	URLs     []string `json:"urls"`
	Note     string   `json:"note"`
}

// Preset 服务商预设：只是方便填写的模板（§4.2，已定）。选一个预设就填好协议、地址和建议模型，用户都可以改；
// 第一次打开时不预选任何一家，界面上也不推荐哪一家。列表顺序照设计文档，不代表推荐。
//
// 地址和要点按设计文档 §2.1、§4.2 的调研结论填，Verified 照抄那里的核实情况：
// 「核实」读过官方文档或源码原文；「摘要」只读到了搜索摘要或第三方文档，要用户用真实 Key 实测。
type Preset struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Protocol Protocol  `json:"protocol"`
	BaseURL  string    `json:"baseUrl"`
	AltURLs  []AltURL  `json:"altUrls,omitempty"` // 可以切换的其他地址（国际站、套餐地址、另一种协议）
	Model    string    `json:"model,omitempty"`   // 建议模型；空 = 从模型列表里选
	Models   []string  `json:"models,omitempty"`  // /models 失败或没有这个接口时的静态建议
	NoKey    bool      `json:"noKey,omitempty"`   // 不用 Key
	Local    bool      `json:"local,omitempty"`   // 本机模型：不花钱，不取价格数据
	Domestic bool      `json:"domestic,omitempty"`
	Auth     AuthMode  `json:"auth,omitempty"`
	Echo     EchoMode  `json:"echo,omitempty"`
	KeyHints []KeyHint `json:"keyHints,omitempty"`
	Note     string    `json:"note"`
	Verified string    `json:"verified"`

	litellm        string // LiteLLM 价格文件里的服务商名（§4.4 的对照表）
	toolResultName bool
	echoPadAll     bool // Echo 为 on 时，没有思考内容的助手消息全都补空串，而不只是带工具调用的（DeepSeek）
}

// AltURL 一个可选地址。Protocol 为空表示跟预设的默认协议一样。
// NoModels：官方文档没有这个地址的模型列表（例如 MiMo Token Plan），不要去请求 /models。
type AltURL struct {
	Label    string   `json:"label"`
	URL      string   `json:"url"`
	Protocol Protocol `json:"protocol,omitempty"`
	NoModels bool     `json:"noModels,omitempty"`
}

// CustomID 自定义（任意 OpenAI 兼容的地址或 Claude 协议的中转）。
const CustomID = "custom"

var presets = []Preset{
	{
		ID: "deepseek", Name: "DeepSeek", Protocol: OpenAI, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Domestic: true,
		Note: "官方文档写明 deepseek-chat、deepseek-reasoner 已于 2026-07-24 下线，换成 deepseek-v4-flash、deepseek-v4-pro" +
			"（摘要）。思考模式下请求带工具时，每条助手消息都要把 reasoning_content 原样传回，没调用工具的那几轮也要，否则会 400" +
			"（核实：官方思考模式文档）；这里总是传回，没有的补空串（空串是否被接受未核实，需实测）。",
		Verified: "摘要", litellm: "deepseek", Echo: EchoOn, echoPadAll: true,
	},
	{
		ID: "qwen", Name: "通义千问（阿里云百炼）", Protocol: OpenAI, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Domestic: true,
		AltURLs: []AltURL{{Label: "新加坡", URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"}},
		Note: "默认是北京地域；Key 只能配对应地域的地址（摘要）。官方建议改用工作空间专属域名，可以在地址里改。" +
			"思考模型由 preserve_thinking 决定读不读历史里的思考内容，缺了不报错（核实），这里收到过就传回。",
		Verified: "摘要", litellm: "dashscope", Echo: EchoAuto,
	},
	{
		ID: "kimi", Name: "Kimi（月之暗面）", Protocol: OpenAI, BaseURL: "https://api.moonshot.cn/v1", Domestic: true,
		AltURLs: []AltURL{{Label: "国际站", URL: "https://api.moonshot.ai/v1"}},
		Note: "国内站和国际站的 Key 不通用（摘要：第三方文档，官方页面未能抓取）。流式用量可能在 choices[0].usage，两处都读。" +
			"多轮和工具调用要把完整的助手消息原样传回，包括 reasoning_content（核实：官方 K3 文档），这里收到过就传回。",
		Verified: "摘要", litellm: "moonshot", toolResultName: true, Echo: EchoAuto,
	},
	{
		ID: "zhipu", Name: "智谱", Protocol: OpenAI, BaseURL: "https://open.bigmodel.cn/api/paas/v4/", Domestic: true,
		Note: "地址末尾的斜杠原样保留。官方说兼容 OpenAI，但「部分场景仍有差异」（摘要）。" +
			"开了「保留思考」（Coding Plan 的地址默认开）时要把思考内容原样传回（核实），这里收到过就传回。",
		Verified: "摘要", litellm: "zai", Echo: EchoAuto,
	},
	{
		ID: "mimo", Name: "小米 MiMo", Protocol: OpenAI, BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.6-pro", Domestic: true,
		Models: []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed"},
		Auth:   AuthBoth, Echo: EchoOn,
		AltURLs: []AltURL{
			{Label: "按量 · Claude 协议", URL: "https://api.xiaomimimo.com/anthropic", Protocol: Anthropic},
			{Label: "Token Plan · 中国", URL: "https://token-plan-cn.xiaomimimo.com/v1", NoModels: true},
			{Label: "Token Plan · 中国 · Claude 协议", URL: "https://token-plan-cn.xiaomimimo.com/anthropic", Protocol: Anthropic, NoModels: true},
			{Label: "Token Plan · 新加坡", URL: "https://token-plan-sgp.xiaomimimo.com/v1", NoModels: true},
			{Label: "Token Plan · 新加坡 · Claude 协议", URL: "https://token-plan-sgp.xiaomimimo.com/anthropic", Protocol: Anthropic, NoModels: true},
			{Label: "Token Plan · 欧洲", URL: "https://token-plan-ams.xiaomimimo.com/v1", NoModels: true},
			{Label: "Token Plan · 欧洲 · Claude 协议", URL: "https://token-plan-ams.xiaomimimo.com/anthropic", Protocol: Anthropic, NoModels: true},
		},
		KeyHints: []KeyHint{
			{Prefixes: []string{"sk-"}, URLs: []string{"https://api.xiaomimimo.com/v1", "https://api.xiaomimimo.com/anthropic"},
				Note: "sk- 开头的是按量付费 Key，只能用在 api.xiaomimimo.com。填到 Token Plan 的地址上会 401。"},
			{Prefixes: []string{"ttp-", "tp-"}, URLs: []string{
				"https://token-plan-cn.xiaomimimo.com/v1", "https://token-plan-cn.xiaomimimo.com/anthropic",
				"https://token-plan-sgp.xiaomimimo.com/v1", "https://token-plan-sgp.xiaomimimo.com/anthropic",
				"https://token-plan-ams.xiaomimimo.com/v1", "https://token-plan-ams.xiaomimimo.com/anthropic",
			}, Note: "tp- / ttp- 开头的是 Token Plan Key，只能用在 token-plan 的地址上，不能用在按量地址。"},
		},
		Note: "按量 Key（sk-）用 api.xiaomimimo.com；Token Plan Key（tp- / ttp-）用 token-plan-cn / -sgp / -ams，两种不能混用。" +
			"鉴权认 api-key 或 Bearer。思考默认开着，多轮（尤其是调用工具之后）要把 reasoning_content 原样传回，否则会 400。" +
			"Token Plan 的域名文档里没有 /models，模型可以手填，或从建议里选。",
		Verified: "核实",
	},
	{
		ID: "openai", Name: "OpenAI", Protocol: OpenAI, BaseURL: "https://api.openai.com/v1",
		Note:     "国内一般要代理才能连上（推断）。MineDeck 走环境变量 HTTPS_PROXY 里的代理。",
		Verified: "摘要", litellm: "openai",
	},
	{
		ID: "ollama", Name: "Ollama（本机）", Protocol: OpenAI, BaseURL: "http://localhost:11434/v1/", NoKey: true, Local: true,
		Note:     "不用 Key，数据不出本机；模型从本机已下载的里面选。不估算费用。",
		Verified: "核实",
	},
	{
		ID: "claude", Name: "Claude", Protocol: Anthropic, BaseURL: "https://api.anthropic.com", Model: "claude-opus-5-5",
		Note:     "支持地区不含中国大陆（摘要），国内一般也连不上（推断）。默认开启服务端回退（拒答时自动换模型，beta）。",
		Verified: "核实", litellm: "anthropic",
	},
	{
		ID: CustomID, Name: "自定义", Protocol: OpenAI, Echo: EchoAuto,
		Note:     "任意 OpenAI 兼容的地址，或者走 Claude 协议的中转。鉴权和思考回传可以改。注意：中转方能看到全部对话和 Key。",
		Verified: "—",
	},
}

// Presets 全部预设（副本）。
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// PresetByID 按编号取预设。
func PresetByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// matchURL 这个地址是不是预设自己的，或某一个可选地址。路径也要比（同一主机上 /v1 和 /anthropic 不是同一个接口）。
func (p Preset) matchURL(base string) (AltURL, bool) {
	if sameEndpoint(base, p.BaseURL) {
		return AltURL{Label: "默认", URL: p.BaseURL, Protocol: p.Protocol}, true
	}
	for _, a := range p.AltURLs {
		if sameEndpoint(base, a.URL) {
			ep := a
			if !ep.Protocol.valid() {
				ep.Protocol = p.Protocol
			}
			return ep, true
		}
	}
	return AltURL{}, false
}

// sameEndpoint http/https、主机（含端口）和路径都一样。末尾斜杠不算差别。
func sameEndpoint(a, b string) bool {
	ua, err1 := ParseBaseURL(a)
	ub, err2 := ParseBaseURL(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host) &&
		strings.TrimRight(ua.Path, "/") == strings.TrimRight(ub.Path, "/")
}

// KeyWarning Key 前缀和当前地址对不上时的提醒。对得上、认不出前缀、或这个预设没有提示时返回空串。不拦截。
func KeyWarning(p Preset, baseURL, key string) string {
	key = strings.TrimSpace(key)
	if key == "" || len(p.KeyHints) == 0 {
		return ""
	}
	var best *KeyHint
	bestLen := 0
	for i := range p.KeyHints {
		h := &p.KeyHints[i]
		for _, px := range h.Prefixes {
			if px != "" && strings.HasPrefix(key, px) && len(px) > bestLen {
				best, bestLen = h, len(px)
			}
		}
	}
	if best == nil {
		return ""
	}
	for _, u := range best.URLs {
		if sameHost(baseURL, u) {
			return ""
		}
	}
	return best.Note
}

// sameHost 主机名一样（不分大小写，不看协议、端口和路径）。前端 app.js 的 keyWarn 是同一条规则。
func sameHost(a, b string) bool {
	ua, err1 := ParseBaseURL(a)
	ub, err2 := ParseBaseURL(b)
	return err1 == nil && err2 == nil && strings.EqualFold(ua.Hostname(), ub.Hostname())
}

// suggestedModels 预设上写死的建议模型（默认模型在最前）。/models 没有或失败时给界面兜底。
func suggestedModels(p Preset) []Model {
	seen := map[string]bool{}
	var out []Model
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, Model{ID: id})
		}
	}
	add(p.Model)
	for _, id := range p.Models {
		add(id)
	}
	return out
}

// officialClaude 是不是 Anthropic 官方的地址（中转不认 eager_input_streaming 和服务端回退，默认不开）。
func officialClaude(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.anthropic.com")
}
