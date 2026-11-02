package ai

import (
	"net/url"
	"strings"
)

// Preset 服务商预设：只是方便填写的模板（§4.2，已定）。选一个预设就填好协议、地址和建议模型，用户都可以改；
// 第一次打开时不预选任何一家，界面上也不推荐哪一家。列表顺序照设计文档，不代表推荐。
//
// 地址和要点按设计文档 §2.1、§4.2 的调研结论填，Verified 照抄那里的核实情况：
// 「核实」读过官方文档或源码原文；「摘要」只读到了搜索摘要或第三方文档，要用户用真实 Key 实测。
type Preset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol Protocol `json:"protocol"`
	BaseURL  string   `json:"baseUrl"`
	AltURLs  []AltURL `json:"altUrls,omitempty"` // 可以切换的其他地址（国际站、其他地域）
	Model    string   `json:"model,omitempty"`   // 建议模型；空 = 从模型列表里选
	NoKey    bool     `json:"noKey,omitempty"`   // 不用 Key
	Local    bool     `json:"local,omitempty"`   // 本机模型：不花钱，不取价格数据
	Domestic bool     `json:"domestic,omitempty"`
	Note     string   `json:"note"`
	Verified string   `json:"verified"`

	litellm        string // LiteLLM 价格文件里的服务商名（§4.4 的对照表）
	echoReasoning  bool
	toolResultName bool
}

// AltURL 一个可选地址。
type AltURL struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// CustomID 自定义（任意 OpenAI 兼容的地址或 Claude 协议的中转）。
const CustomID = "custom"

var presets = []Preset{
	{
		ID: "deepseek", Name: "DeepSeek", Protocol: OpenAI, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Domestic: true,
		Note: "官方文档写明 deepseek-chat、deepseek-reasoner 已于 2026-07-24 下线，换成 deepseek-v4-flash、deepseek-v4-pro" +
			"（摘要）。思考模式下工具循环可能要把 reasoning_content 原样传回，这里默认传回（摘要，需实测）。",
		Verified: "摘要", litellm: "deepseek", echoReasoning: true,
	},
	{
		ID: "qwen", Name: "通义千问（阿里云百炼）", Protocol: OpenAI, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Domestic: true,
		AltURLs:  []AltURL{{Label: "新加坡", URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"}},
		Note:     "默认是北京地域；Key 只能配对应地域的地址（摘要）。官方建议改用工作空间专属域名，可以在地址里改。",
		Verified: "摘要", litellm: "dashscope",
	},
	{
		ID: "kimi", Name: "Kimi（月之暗面）", Protocol: OpenAI, BaseURL: "https://api.moonshot.cn/v1", Domestic: true,
		AltURLs:  []AltURL{{Label: "国际站", URL: "https://api.moonshot.ai/v1"}},
		Note:     "国内站和国际站的 Key 不通用（摘要：第三方文档，官方页面未能抓取）。流式用量可能在 choices[0].usage，两处都读。",
		Verified: "摘要", litellm: "moonshot", toolResultName: true,
	},
	{
		ID: "zhipu", Name: "智谱", Protocol: OpenAI, BaseURL: "https://open.bigmodel.cn/api/paas/v4/", Domestic: true,
		Note:     "地址末尾的斜杠原样保留。官方说兼容 OpenAI，但「部分场景仍有差异」（摘要）。",
		Verified: "摘要", litellm: "zai",
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
		ID: CustomID, Name: "自定义", Protocol: OpenAI,
		Note:     "任意 OpenAI 兼容的地址，或者走 Claude 协议的中转。注意：中转方能看到全部对话和 Key。",
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

// officialClaude 是不是 Anthropic 官方的地址（中转不认 eager_input_streaming 和服务端回退，默认不开）。
func officialClaude(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.anthropic.com")
}
