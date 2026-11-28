// 全局设置 →「AI（可选）」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」，
// 又说里面和 AI 有关的说法作者可能在意：这里的字都照程序的原话，只把长说明收进悬停，没有加程序没有的功能）。样式在 settings-ai.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6200–6795 行 setAI / drawAI，加 assistant.js 的 settings()、mcp.js 的 settings()），从上到下一长页：
//   「AI 能做的三件事」（AI 助手、游戏内 AI、AI 伙伴各一行说明，加「去服务器列表」）；「AI 服务商（可选）」一句说明、已启用 / 未启用（还差：…）；
//   服务商表单：服务商（下拉，预设只是模板）、协议 / 鉴权 / 思考回传（只有「自定义」才出现）、接口地址（有可选地址的预设下面一排「换成…」）、
//   API Key（只写不读：已设置时框空着、占位「已设置（留空不修改）」，旁边「清除」）、模型（手填 + 箭头拉列表、可搜，「刷新」）、思考深度（只有 Claude 协议才出现）；
//   吸底保存条「保存」（换了服务商或地址换了网站时，确认框「保存后要重新填 Key」）；测试连接（一行结果）；
//   每日额度（三个功能各一行：今天 / 本月用量，「万 token / 天」框 +「保存」）；费用估算（价格来源 +「刷新」、当前模型的单价、自填单价列表 + 一行五个框的添加）；
//   AI 助手（局域网可用 AI 助手 开关、会话记录「清空」）；MCP 接口（开关；开着时接口地址「复制」、访问令牌「重置令牌」、「复制配置…」菜单 → 配置弹窗，连过的 AI 工具）；
//   停用 AI（危险区）；最后一段隐私说明。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：左右两块，左边 484 宽、右边 360 宽（圆角 18 = 26 − 8）。
//   · 左栏是服务商表单（多字段，同详情页「启动与内存」照向导第三步：字 18 + 6 + 框 40，两列、行距 16），下面一条保存条只管这一栏（52 高、圆角 18，
//     同「全部配置」「定时任务」：干净时只有灰着的「保存」，有改动时淡进来「N 项修改未保存」「放弃修改」，「保存」亮起来）。
//     顶上一行：「AI 服务商」、状态（已启用 / 未启用 · 还差：…）、隐私（锁形图标，全文在悬停）、停用 AI（电源图标，贴栏的右上角 8、圆角 10）。
//     五行：服务商 ｜ 模型；接口地址（框里右边一颗 ▾，有可选地址的预设才有，点开是一张地址菜单）；API Key（框里：Key 前缀和地址对不上时一个黄色警告，已存的 Key 旁边一颗清除）；
//     协议 ｜ 鉴权；思考回传 ｜ 思考深度。程序里协议、鉴权、思考回传只给「自定义」、思考深度只给 Claude 协议，是藏起来的；这里一直摆着、用不上时灰掉，悬停说原因
//     （规范第九节「暂时不能用的灰掉」，也免得换服务商时整栏跳）。灰着的也写着这家实际用的值（MiMo 的鉴权是「都发」、DeepSeek 的思考回传是「总是回传」）。
//     最下面一块「测试连接」（贴栏边 8、圆角 10）：表单有改动时灰着（同程序：只测已保存的），结果写在键的右边。
//   · 右边一块是其余的，带细滚动条在块里滚：每日额度（三个功能，名字的悬停是程序「AI 能做的三件事」那段，右边一行今天 / 本月的用量，框里一颗保存）、
//     费用估算（价格来源和刷新；当前模型有自填单价时就在那一行标「当前」，不再另写一遍；自填单价的增删，添加是一个表单弹窗）、
//     AI 助手（开关、清空会话记录）、MCP 接口（开关；开着时接口地址、访问令牌、复制配置、连过的 AI 工具）。
//   · 单字段、开关照程序即时保存（额度框里一颗保存，存好了换成勾 1.2 秒；开关点了就是）；确认框照共用件页的危险确认框。
//   · 离开前拦（作者 11-28 定，外壳统一拦，这里只登记 dirty / save / discard）：算没存的有两处——左栏表单改了的几项、右栏额度框里改了没点保存的；
//     开关、自填单价、清空、重置这些点了就生效，不在里面。选「保存」时额度和表单都存，有一处被拦下（额度填错、没选服务商、清旧 Key 的确认点了取消）就留在这页；
//     选「不保存」时两处的草稿都丢掉。清旧 Key 的确认为了能等它的结果，换成了共用的 KIT.confirm（样子一样）。
// 和程序不一样的字：AI 助手的说明「标题栏右上角的「AI 助手」」改成「底部导航里的「AI 助手」」（胶囊里那一颗）；「去服务器列表」那一行没画（悬停里写了在哪）。
// 示例数据：配的是 DeepSeek（预设地址、预设的建议模型 deepseek-v4-flash），Key 已设置；模型列表只用 presets.go 里写着的（DeepSeek 的两个取自它的说明），
//   没写的（Kimi、智谱、OpenAI、Ollama）当作「没有拉到」；用量、费用、测试连接的延迟和 token、价格表的更新日期和条数、自填单价 ¥1 / ¥2 都是草图编的；
//   公开价格没画（不编具体模型的价格）：当前模型没有自填单价时照程序写「查不到…的公开价格」。MCP 打开着，连过 Claude Desktop 和 Cherry Studio；
//   配置预览里的令牌照程序换成占位，MineDeck.exe 的路径是编的。
// 停用 AI 以后胶囊里的 AI 助手跟着藏起来，重新配好又出来（作者 11-29 定；外观里「在底部导航显示 AI 助手」关着时照旧不显示，外壳 setAIOff 管）。
// 程序里停用是把服务商配置清掉，和「从来没配过」看起来一样，要另记一个「停用过」，下次打开才知道还要藏着；从来没配过的照旧显示，点了是助手面板里引着去设置的那一页。
// 草图专用：&ai=off 看还没配置（服务商「未选择」、停用和测试灰着）。没画的：从局域网打开（只能看）、读取失败、测试连接失败。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }], { duration: 360, easing: SLIDE });
  const rise = (el, i = 0) => el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" });
  const setTip = (el, t) => { if (!el || (el.getAttribute("title") ?? el.dataset.tip ?? "") === (t || "")) return; if (t) el.title = t; else el.removeAttribute("title"); delete el.dataset.tip; };
  const setText = (el, t) => { if (el && el.textContent !== t) el.textContent = t; };
  const setHTML = (el, h) => { if (el && el._h !== h) { el._h = h; el.innerHTML = h; C.KIT.hydrate(el); return true; } return false; };

  // ---- 预设：照 internal/ai/presets.go（名字、地址、建议模型、可选地址、说明原话） ----
  const PRESETS = [
    { id: "deepseek", name: "DeepSeek", url: "https://api.deepseek.com", model: "deepseek-v4-flash", models: ["deepseek-v4-flash", "deepseek-v4-pro"], echo: "on",
      note: "官方已于 2026-07-24 下线 deepseek-chat、deepseek-reasoner，替换为 deepseek-v4-flash、deepseek-v4-pro。思考模式进行工具调用时需回传 reasoning_content，否则会返回 400 错误；本预设已默认开启「总是回传」。" },
    { id: "qwen", name: "通义千问（阿里云百炼）", url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus", models: ["qwen-plus"],
      alts: [{ l: "新加坡", u: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1" }],
      note: "默认使用北京地域，API Key 需与所选地域地址匹配；如需使用工作空间专属域名，可在接口地址中修改。思考内容按需自动回传。" },
    { id: "kimi", name: "Kimi（月之暗面）", url: "https://api.moonshot.cn/v1", alts: [{ l: "国际站", u: "https://api.moonshot.ai/v1" }],
      note: "国内站与国际站的 API Key 不通用，请匹配对应地址。流式用量可能位于 choices[0].usage，两处均会读取。多轮对话与工具调用时将自动回传思考内容。" },
    { id: "zhipu", name: "智谱", url: "https://open.bigmodel.cn/api/paas/v4/",
      note: "接口地址末尾的斜杠请原样保留。兼容 OpenAI 协议，部分场景可能存在行为差异；启用保留思考时将自动回传思考内容。" },
    { id: "mimo", name: "小米 MiMo", url: "https://api.xiaomimimo.com/v1", model: "mimo-v2.6-pro", models: ["mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed"], auth: "both", echo: "on",
      alts: [{ l: "按量（Claude）", u: "https://api.xiaomimimo.com/anthropic", p: "anthropic" }, { l: "Token Plan · 中国", u: "https://token-plan-cn.xiaomimimo.com/v1" },
        { l: "Token Plan · 中国（Claude）", u: "https://token-plan-cn.xiaomimimo.com/anthropic", p: "anthropic" }, { l: "Token Plan · 新加坡", u: "https://token-plan-sgp.xiaomimimo.com/v1" },
        { l: "Token Plan · 新加坡（Claude）", u: "https://token-plan-sgp.xiaomimimo.com/anthropic", p: "anthropic" }, { l: "Token Plan · 欧洲", u: "https://token-plan-ams.xiaomimimo.com/v1" },
        { l: "Token Plan · 欧洲（Claude）", u: "https://token-plan-ams.xiaomimimo.com/anthropic", p: "anthropic" }],
      hints: [{ px: ["sk-"], hosts: ["api.xiaomimimo.com"], note: "sk- 前缀为按量付费 Key，仅适用于 api.xiaomimimo.com，无法用于 Token Plan 地址。" },
        { px: ["ttp-", "tp-"], hosts: ["token-plan-cn.xiaomimimo.com", "token-plan-sgp.xiaomimimo.com", "token-plan-ams.xiaomimimo.com"], note: "tp- / ttp- 前缀为 Token Plan Key，仅适用于 token-plan 系列地址，无法用于按量付费地址。" }],
      note: "按量 Key（sk-）需搭配 api.xiaomimimo.com，Token Plan Key（tp- / ttp-）需搭配 token-plan-cn / -sgp / -ams，二者不可混用。默认同时发送 Bearer 与 api-key 鉴权并总是回传思考内容（多轮对话不回传 reasoning_content 会返回 400 错误）；Token Plan 域名的文档中没有 /models 接口，可直接输入模型名或从建议中选择。" },
    { id: "openai", name: "OpenAI", url: "https://api.openai.com/v1", note: "中国大陆网络环境通常需通过代理访问；MineDeck 支持读取系统环境变量 HTTPS_PROXY 中的代理设置。" },
    { id: "ollama", name: "Ollama（本机）", url: "http://localhost:11434/v1/", noKey: true, local: true, note: "无需 API Key，数据仅在本机处理且不计费；可从本机已下载的模型中选择。" },
    { id: "claude", name: "Claude", proto: "anthropic", url: "https://api.anthropic.com", model: "claude-opus-5-5", models: ["claude-opus-5-5"],
      note: "官方支持地区不含中国大陆，国内网络通常无法直连。默认开启服务端回退（拒答时自动切换模型，Beta）。" },
    { id: "custom", name: "自定义", url: "", note: "支持任意 OpenAI 兼容接口或 Claude 协议中转地址，可自定义鉴权与思考回传策略。注意：中转服务商可查看全部对话内容与 API Key。" },
  ];
  const PRE = Object.fromEntries(PRESETS.map(p => [p.id, p]));
  const PROTOS = [["openai", "OpenAI 兼容"], ["anthropic", "Claude"]];
  const AUTHS = [["", "默认"], ["bearer", "仅 Bearer"], ["api-key", "仅 api-key"], ["both", "同时发送"]];
  const ECHOS = [["auto", "自动"], ["on", "总是回传"], ["off", "不回传"]];
  const EFFORTS = [["", "默认（由模型决定）"], ["low", "低（最快、最省）"], ["medium", "中"], ["high", "高"], ["xhigh", "很高"], ["max", "最高（最慢、最贵）"]];
  const ECHO_TIP = { auto: "收到思考内容时原样传回；若服务端不支持该字段，将自动移除并重试，后续请求不再携带。", on: "每次请求均回传思考内容；工具调用轮次若未返回则补空串，避免思考模式返回 400 错误。", off: "不回传思考内容；部分思考模型在多轮工具调用时可能因此返回 400 错误。" };
  const FEATS = [
    { id: "assistant", name: "AI 助手", tip: "底部导航中的「AI 助手」：查询日志与配置后解答 MineDeck 相关问题；涉及修改操作时会先生成确认卡片，经确认后执行。本机与局域网登录设备共用此额度。" },
    { id: "gamechat", name: "游戏内 AI", tip: "各服务器的「扩展 → 游戏内 AI」：玩家在游戏聊天中触发后，由 AI 在聊天栏回复。代理端不支持。" },
    { id: "companion", name: "AI 伙伴", tip: "NeoForge、Forge 服务器的「扩展 → AI 伙伴」：游戏内可交互的陪玩角色，可与玩家对话并协助操作。" },
  ];
  const MCP_URL = "http://127.0.0.1:27333/mcp", MASK = "<令牌，复制时自动附带>", EXE = "D:\\MineDeck\\MineDeck.exe"; // 路径是编的
  const MCP_HEAD = "允许本机其他 AI 工具（Claude Desktop、Cherry Studio、Cursor、VS Code）通过 MCP 接入 MineDeck：可直接查询服务器、日志与配置；涉及修改操作时将在 MineDeck 中弹出确认卡片，确认后执行。默认关闭，仅接受本机连接。"
    + "\n发送至外部 AI 的内容会先脱敏：路径中的 Windows 用户名与程序文件夹、局域网 IP、计算机名及 API Key 均会被替换，玩家名保留。删除服务器与修改设置不会开放给外部 AI。访问令牌仅供本机程序使用，请勿泄露。";
  const PRIVACY = "隐私说明：使用 AI 时，提问内容与查询结果（服务器信息、配置、日志片段等）会发送至所选服务商，各服务商的数据保留策略不同；如有顾虑可使用本机 Ollama。"
    + "\nAPI Key 加密存储于本机，不会回传至界面，也不会写入日志与操作记录。获取公开价格仅下载公开价格表（经 GitHub 加速镜像），不发送任何用户数据。";
  const SNIPS = (() => { // 同 internal/mcp/config.go：名字、粘贴到哪儿、配置原文（令牌换成占位）
    const auth = { Authorization: "Bearer " + MASK }, j = (k, v) => JSON.stringify({ [k]: { minedeck: v } }, null, 2);
    return [
      { id: "claude", name: "Claude Desktop", t: "stdio", where: "Claude Desktop：「设置 → 开发者 → 编辑配置」打开 claude_desktop_config.json，将 minedeck 合并至 mcpServers，保存后完全退出并重启 Claude Desktop。stdio 方式无需填写令牌，也不受 MineDeck 端口变更影响。", text: j("mcpServers", { command: EXE, args: ["-mcp"] }) },
      { id: "cherry", name: "Cherry Studio", t: "http", where: "Cherry Studio：「设置 → MCP 服务器 → 添加 → 从 JSON 导入」，粘贴后确定。", text: j("mcpServers", { type: "streamableHttp", url: MCP_URL, headers: auth }) },
      { id: "cursor", name: "Cursor", t: "http", where: "Cursor：在 Cursor 设置中打开 MCP，添加服务器后将打开 mcp.json（全局配置位于用户文件夹下的 .cursor\\mcp.json），将 minedeck 合并至 mcpServers。", text: j("mcpServers", { url: MCP_URL, headers: auth }) },
      { id: "vscode", name: "VS Code", t: "http", where: "VS Code：在命令面板中运行「MCP: Open User Configuration」打开 mcp.json，将 minedeck 合并至 servers。", text: j("servers", { type: "http", url: MCP_URL, headers: auth }) },
      { id: "http", name: "其他（HTTP）", t: "http", where: "其他支持 Streamable HTTP 的客户端：按下文填写地址与请求头。", text: `地址：${MCP_URL}\n请求头：Authorization: Bearer ${MASK}` },
      { id: "stdio", name: "其他（stdio）", t: "stdio", where: "其他支持 stdio 的客户端：命令填写 MineDeck.exe 完整路径，参数填写 -mcp。", text: `命令：${EXE}\n参数：-mcp` },
    ];
  })();
  const mcpTip = () => (V.mcp ? "已开启。写操作与危险操作需在 MineDeck 中确认后执行。" : "关闭时外部 AI 无法连接 MineDeck；开启时将自动生成访问令牌。"); // 程序还写着工具数，这里没画（不编数）
  const snipLabel = s => /（(HTTP|stdio)）$/.test(s.name) ? s.name : s.name + (s.t === "stdio" ? "（stdio）" : "（HTTP）"); // 同 mcp.js menuLabel

  // ---- 数据（换分类回来不丢） ----
  let V = null, C = null;
  function seed(ctx) {
    if (V) return;
    const off = ctx.QS.get("ai") === "off";
    const s = off ? { preset: "", url: "", model: "", proto: "openai", auth: "", echo: "auto", effort: "" }
      : { preset: "deepseek", url: "https://api.deepseek.com", model: "deepseek-v4-flash", proto: "openai", auth: "", echo: "auto", effort: "" };
    V = { saved: s, cur: { ...s, key: "" }, keySet: !off, saving: false, test: null, testing: false, models: {},
      quota: { assistant: "5", gamechat: "", companion: "" }, qd: {},
      use: { assistant: ["1.3 万", "28.6 万", "约 ¥0.02", "约 ¥0.41"], gamechat: ["4,210", "9.8 万", "约 ¥0.01", "约 ¥0.14"], companion: ["0", "0", "", ""] },
      prices: [{ model: "deepseek-v4-flash", cur: "CNY", in: 1, out: 2, cache: 0.2 }], priceAt: "2026-11-20", priceN: 1862, priceBusy: false,
      lan: true, sess: 7, mcp: true, clients: [{ name: "Claude Desktop", t: "stdio", proto: "2025-06-18", calls: 12, last: "10:42" }, { name: "Cherry Studio", t: "http", proto: "2025-06-18", calls: 3, last: "09:15" }] };
  }
  const pre = id => PRE[id ?? V.cur.preset];
  const isCustom = () => V.cur.preset === "custom";
  const host = u => { try { return new URL(u.trim()).hostname.toLowerCase(); } catch { return ""; } };
  const site = u => { try { const x = new URL(u.trim()); return /^https?:$/.test(x.protocol) ? x.origin : ""; } catch { return ""; } };
  const sameUrl = (a, b) => a.trim().replace(/\/+$/, "") === b.trim().replace(/\/+$/, "");
  function protoOf(f) { // 同程序 protoNow：自定义看分段；预设先看地址是不是某一条声明了另一种协议的可选地址
    const p = PRE[f.preset]; if (!p) return "";
    if (p.id === "custom") return f.proto;
    const alt = (p.alts || []).find(a => a.p && sameUrl(a.u, f.url));
    return alt ? alt.p : (p.proto || "openai");
  }
  const authOf = f => (f.preset === "custom" ? f.auth : (PRE[f.preset]?.auth || ""));
  const echoOf = f => (f.preset === "custom" ? f.echo : (PRE[f.preset]?.echo || "auto"));
  const keyGone = () => V.cur.preset !== V.saved.preset || !site(V.cur.url) || site(V.cur.url) !== site(V.saved.url);
  const keyWarn = () => { // 同程序 keyWarn：前缀对不上当前地址就提醒，不拦
    const p = pre(), k = V.cur.key.trim(); if (!p?.hints || !k) return "";
    let best = null, n = 0;
    p.hints.forEach(h => h.px.forEach(x => { if (k.startsWith(x) && x.length > n) { best = h; n = x.length; } }));
    return best && !best.hosts.includes(host(V.cur.url)) ? best.note : "";
  };
  const enabled = () => { const p = PRE[V.saved.preset]; return !!(p && V.saved.model && (V.keySet || p.noKey)); };
  const missing = () => { const p = PRE[V.saved.preset]; return [!p && "服务商", !(p?.noKey || V.keySet) && "API Key", !V.saved.model && "模型"].filter(Boolean).join("、"); };
  // 一项算不算改了：同程序 readForm（自定义才比鉴权和思考回传，Claude 协议才比思考深度；Key 框有字就算）
  function modOf(k) {
    const c = V.cur, s = V.saved;
    if (k === "key") return !!c.key.trim();
    if (k === "proto") return isCustom() && s.preset === "custom" && c.proto !== s.proto; // 预设的协议跟着地址走，算在地址那一项里
    if (k === "auth") return isCustom() && c.auth !== s.auth;
    if (k === "echo") return isCustom() && c.echo !== s.echo;
    if (k === "effort") return protoOf(c) === "anthropic" && c.effort !== s.effort;
    return c[k].trim() !== s[k].trim();
  }
  const FIELDS = ["preset", "model", "url", "key", "proto", "auth", "echo", "effort"];
  const dirtyN = () => FIELDS.filter(modOf).length;

  // =====================================================================================================
  // 画
  // =====================================================================================================
  const F = (k, label, ctl, span) => `<div class="sai-f" data-k="${k}"${span ? ' style="grid-column: span 2"' : ""}><div class="sai-lb"><span class="sai-lt">${label}</span><i class="sai-dot"></i></div>${ctl}</div>`;
  const selHTML = k => `<button type="button" class="sai-sel" data-sai="sel" data-sel="${k}" aria-haspopup="menu" aria-expanded="false"><span class="sai-selv"></span><i data-ic="chevronDown" class="sai-chev"></i></button>`;
  const segHTML = (k, opts) => `<div class="sai-seg" data-seg="${k}" role="radiogroup"><i class="sai-sgl"></i>${opts.map(([v, l]) => `<button type="button" role="radio" data-v="${v}">${l}</button>`).join("")}</div>`;
  const swHTML = (on, act, label, tip) => `<button type="button" class="sai-swr${on ? " on" : ""}" data-sai="${act}" role="switch" aria-checked="${on}" title="${esc(tip)}"><span class="sai-sw"></span><span>${label}</span></button>`;
  function leftHTML() {
    return `<section class="sai-col sai-pv" aria-label="AI 服务商"><div class="sai-hd"><span class="sai-t" title="配置服务商、API Key（本机 Ollama 除外）与模型后启用。未配置时 MineDeck 不连接任何 AI 服务，也不下载价格数据。预设仅为配置模板，不代表推荐。">AI 服务商</span>`
      + `<span class="sai-stat"></span><span class="sai-sp"></span><span class="sai-hi" title="${esc(PRIVACY)}"><i data-ic="lock"></i></span>`
      + `<button type="button" class="sai-hb" data-sai="off"><i data-ic="power"></i></button></div>`
      + `<div class="sai-grid">`
      + F("preset", "服务商", selHTML("preset"))
      + F("model", "模型", `<label class="sai-in mono sai-cb"><input data-in="model" placeholder="模型名" spellcheck="false" autocomplete="off" aria-label="模型"><button type="button" class="sai-ib" data-sai="models" title="模型列表" aria-haspopup="listbox"><i data-ic="chevronDown"></i></button></label>`)
      + F("url", "接口地址", `<label class="sai-in mono"><input data-in="url" placeholder="https://…" spellcheck="false" autocomplete="off" aria-label="接口地址"><button type="button" class="sai-ib" data-sai="alts" title="切换可选地址" aria-haspopup="menu"><i data-ic="chevronDown"></i></button></label>`, 1)
      + F("key", "API Key", `<label class="sai-in mono"><input type="password" data-in="key" autocomplete="off" spellcheck="false" aria-label="API Key"><span class="sai-kw"><i data-ic="warn"></i></span><button type="button" class="sai-ib" data-sai="clearkey" title="清除 API Key"><i data-ic="trash"></i></button></label>`, 1)
      + F("proto", "协议", segHTML("proto", PROTOS)) + F("auth", "鉴权", selHTML("auth"))
      + F("echo", "思考回传", segHTML("echo", ECHOS)) + F("effort", "思考深度", selHTML("effort"))
      + `</div><div class="sai-test"><button type="button" class="sai-btn" data-sai="test"><i data-ic="bolt"></i>测试连接</button><div class="sai-tr" aria-live="polite"></div></div></section>`
      + `<div class="sai-bar"><span class="sai-note"></span><span class="sai-sp"></span><span class="sai-n"><i></i><span></span></span>`
      + `<button type="button" class="sai-discard" data-sai="discard">放弃修改</button><button type="button" class="sai-save" data-sai="save">保存</button></div>`;
  }
  function quotaHTML(f) {
    const u = V.use[f.id], d = V.qd[f.id], v = d ?? V.quota[f.id], dirty = d !== undefined && d !== V.quota[f.id];
    return `<div class="sai-f" data-q="${f.id}"><div class="sai-lb"><span class="sai-lt" title="${esc(f.tip)}">${f.name}</span><span class="sai-use" title="${esc(`今日已用 ${u[0]} token${u[2] ? `（${u[2]}）` : ""} · 本月 ${u[1]} token${u[3] ? `（${u[3]}）` : ""}`)}">今日 ${u[0]} · 本月 ${u[1]}</span></div>`
      + `<label class="sai-in${dirty ? " dirty" : ""}"><input data-in="q" value="${esc(v)}" placeholder="不限" inputmode="decimal" autocomplete="off" aria-label="${f.name} 每日额度（万 token）">`
      + `<span class="sai-unit">万 token / 天</span><span class="sai-msg"></span><button type="button" class="sai-ib sv" data-sai="q" title="保存"${dirty ? "" : " disabled"}><i data-ic="save"></i></button></label></div>`;
  }
  const money = (x, c) => (c === "CNY" ? "¥" : "$") + (Math.round(x * 10000) / 10000);
  const priceTxt = p => `输入 ${money(p.in, p.cur)} · 输出 ${money(p.out, p.cur)}` + (p.cache != null ? ` · 缓存命中 ${money(p.cache, p.cur)}` : "");
  const priceRow = p => `<div class="sai-pr${p.model === V.saved.model ? " cur" : ""}" data-m="${esc(p.model)}" title="${esc(priceTxt(p) + "（每百万 token）")}"><span class="sai-pm">${esc(p.model)}</span>`
    + `<span class="sai-ptag">当前</span><span class="sai-pp">${esc(priceTxt(p))}</span><button type="button" class="sai-px" data-sai="delprice" title="删除" aria-label="删除 ${esc(p.model)} 的自填单价"><i data-ic="x"></i></button></div>`;
  const clientRow = c => {
    const mode = c.t === "stdio" ? "stdio" : "HTTP";
    const full = [c.name, mode, "协议 " + c.proto, c.calls ? `调用工具 ${c.calls} 次` : "", c.last].filter(Boolean).join(" · ");
    const brief = [mode, c.calls ? `${c.calls} 次` : "", c.last].filter(Boolean).join(" · ");
    return `<div class="sai-cl" title="${esc(full)}"><i data-ic="${c.t === "stdio" ? "monitor" : "globe"}"></i><span class="sai-cn">${esc(c.name)}</span><span class="sai-cm">${esc(brief)}</span></div>`;
  };
  function rightHTML() {
    return `<section class="sai-col sai-rt" aria-label="额度、费用、AI 助手、MCP 接口"><div class="sai-lw"><div class="list">`
      + `<div class="sai-sec"><div class="sai-sh"><span title="三项功能共用左侧配置的服务商与 API Key，每日额度分别计算。按功能设置每日 token 上限（仅计入输入与输出，缓存不计入），超额后该功能暂停至次日 0 点。留空或填 0 表示不限。">每日额度</span></div>`
      + `<div class="sai-qs">${FEATS.map(quotaHTML).join("")}</div></div>`
      + `<div class="sai-sec"><div class="sai-sh"><span title="仅供参考：按公开价格或自填单价估算，不做汇率换算（人民币与美元分别合计）；未匹配到价格时仅显示 token 用量。">费用估算</span><span class="sai-sp"></span>`
      + `<button type="button" class="sai-hb sm" data-sai="prices" title="刷新价格数据"><i data-ic="refresh"></i></button></div>`
      + `<div class="sai-line sai-psrc"></div><div class="sai-line sai-mp"></div><div class="sai-pl">${V.prices.map(priceRow).join("")}</div>`
      + `<button type="button" class="sai-add" data-sai="addprice" title="为指定模型设置每百万 token 单价（人民币或美元），优先级高于公开价格，也可用于补充未收录模型"><i data-ic="plus"></i>自填单价</button></div>`
      + `<div class="sai-sec"><div class="sai-sh"><span title="AI 助手会先查询日志与配置再作答；涉及修改操作时会先弹出确认卡片，经确认后执行。">AI 助手</span></div>`
      + swHTML(V.lan, "lan", "局域网可用 AI 助手", "允许局域网登录的设备（如手机）使用 AI 助手，与本机共用每日额度；确认卡片由对应设备用户自行确认。关闭后，局域网界面不显示 AI 助手入口。")
      + `<div class="sai-kv"><span title="本机保留最近 20 个会话记录；清空后无法恢复。">会话记录</span><span class="sai-sp"></span><button type="button" class="sai-pill dgr" data-sai="sess"${V.sess ? "" : ' disabled title="暂无会话记录"'}>清空</button></div></div>`
      + `<div class="sai-sec"><div class="sai-sh"><span title="${esc(MCP_HEAD)}">MCP 接口</span></div>`
      + swHTML(V.mcp, "mcp", "开启 MCP 接口", mcpTip())
      + `<div class="sai-mcp"${V.mcp ? "" : " hidden"}>`
      + `<div class="sai-kv"><span title="HTTP 方式连接此地址；stdio 方式（MineDeck.exe -mcp）不受端口变化影响。">接口地址</span><span class="sai-sp"></span><span class="sai-mono">${MCP_URL}</span><button type="button" class="sai-ib" data-sai="copyurl" title="复制接口地址"><i data-ic="copy"></i></button></div>`
      + `<div class="sai-kv"><span title="已生成并加密存储于本机，仅在复制配置时附带。若令牌泄露请立即重置；重置后需更新 HTTP 客户端配置，stdio 方式无需修改。">访问令牌</span><span class="sai-sp"></span><span class="sai-mono faint">••••••••</span>`
      + `<button type="button" class="sai-pill" data-sai="token">重置令牌</button></div>`
      + `<div class="sai-kv"><span title="选择目标 AI 工具，复制配置片段并按说明粘贴至其 MCP 配置文件">客户端配置</span><span class="sai-sp"></span><button type="button" class="sai-pill" data-sai="snips" aria-haspopup="menu"><i data-ic="copy"></i>复制配置…</button></div>`
      + `<div class="sai-sub">连接过的 AI 工具（本次启动以来）</div>${V.clients.map(clientRow).join("")}</div></div>`
      + `</div><span class="thumb"></span></div></section>`;
  }
  function html(ctx) { C = ctx; seed(ctx); return `<div class="sai">${leftHTML()}${rightHTML()}</div>`; }
  const root = () => document.querySelector(".cp > .sai:not(.fade-out)");
  const fld = k => root()?.querySelector(`.sai-f[data-k="${k}"]`);

  // ---- 只换变了的：每一处都先比一下，一样就不动 ----
  function placeSeg(box, animate) {
    const hl = box.querySelector(".sai-sgl"), on = box.querySelector("button.on"); if (!hl) return;
    hl.style.opacity = on ? "" : "0"; if (!on || !box.offsetWidth) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function paintSeg(k, v, animate) {
    const box = fld(k)?.querySelector(".sai-seg"); if (!box) return;
    const cur = box.querySelector("button.on")?.dataset.v;
    if (cur === v && box._placed) return;
    box.querySelectorAll("button").forEach(b => { const on = b.dataset.v === v; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); });
    placeSeg(box, animate && box._placed); box._placed = true;
  }
  const optLabel = (opts, v) => (opts.find(o => o[0] === v) || ["", ""])[1];
  function paintField(k, animate = true) {
    const el = fld(k); if (!el) return; const c = V.cur, p = pre();
    let off = "", tip = "";
    switch (k) {
      case "preset": setHTML(el.querySelector(".sai-selv"), p ? esc(p.name) : `<span class="sai-ph">未选择</span>`); tip = p ? p.note : "请选择 AI 服务商；预设仅为配置模板，不代表推荐"; setTip(el.querySelector(".sai-lt"), tip); tip = ""; break;
      case "model": { const i = el.querySelector("input"); if (i.value !== c.model) i.value = c.model; break; }
      case "url": {
        const i = el.querySelector("input"); if (i.value !== c.url) i.value = c.url;
        el.querySelector('[data-sai="alts"]').hidden = !p?.alts;
        setTip(el.querySelector(".sai-lt"), p && p.id !== "custom" ? `预设地址：${p.url}` : "填写任意 OpenAI 兼容接口地址，或 Claude 协议中转地址");
        break;
      }
      case "key": {
        const i = el.querySelector("input"), keep = V.keySet && !keyGone();
        i.disabled = !!p?.noKey; if (p?.noKey && c.key) { c.key = ""; i.value = ""; }
        const ph = p?.noKey ? "无需 API Key" : keep ? "已设置（留空不修改）" : "粘贴 API Key"; if (i.placeholder !== ph) i.placeholder = ph;
        el.querySelector('[data-sai="clearkey"]').hidden = !keep;
        const kw = keyWarn(), w = el.querySelector(".sai-kw"); w.hidden = !kw; setTip(w, kw);
        setTip(el.querySelector(".sai-lt"), p?.noKey ? "本机模型无需 API Key" : "加密存储于本机，更换电脑或 Windows 用户后需重新填写" + (isCustom() ? "。注意：中转服务商可查看全部对话内容与 API Key" : ""));
        break;
      }
      case "proto": paintSeg("proto", protoOf(c), animate); if (!isCustom()) off = p ? "仅「自定义」服务商可修改，预设已自动匹配协议" : "请先选择服务商"; tip = "自定义接口使用的协议类型"; break;
      case "auth": setText(el.querySelector(".sai-selv"), optLabel(AUTHS, authOf(c))); if (!isCustom()) off = p ? "仅「自定义」服务商可修改" : "请先选择服务商";
        tip = protoOf(c) === "anthropic" ? "指定 API Key 所在的请求头。默认发送 x-api-key，中转地址会额外附加 Authorization: Bearer。" : "指定 API Key 所在的请求头。默认发送 Authorization: Bearer；部分服务商（如小米 MiMo）同时支持 api-key。"; break;
      case "echo": paintSeg("echo", echoOf(c), animate); if (!isCustom()) off = p ? "仅「自定义」服务商可修改" : "请先选择服务商"; tip = ECHO_TIP[echoOf(c)] || ECHO_TIP.auto; break;
      case "effort": setText(el.querySelector(".sai-selv"), optLabel(EFFORTS, c.effort)); if (protoOf(c) !== "anthropic") off = "仅对 Claude 协议生效；深度越高响应越慢、消耗越高（claude-opus-5-5 默认为「中」）";
        tip = "仅对 Claude 协议生效；深度越高响应越慢、消耗越高（claude-opus-5-5 默认为「中」）"; break;
    }
    if (["proto", "auth", "echo", "effort"].includes(k)) {
      el.classList.toggle("off", !!off); setTip(el, off); setTip(el.querySelector(".sai-lt"), off ? "" : tip);
      el.querySelectorAll("button").forEach(b => { b.tabIndex = off ? -1 : 0; });
    }
    el.classList.toggle("mod", modOf(k));
  }
  const paintForm = (animate = true) => FIELDS.forEach(k => paintField(k, animate));
  function paintStat() {
    const r = root(); if (!r) return; const on = enabled();
    const h = on ? `<span class="sai-pill2 ok"><i></i>已启用</span>` : `<span class="sai-pill2 warn"><i></i>未启用${missing() ? ` · 缺少：${missing()}` : ""}</span>`;
    const el = r.querySelector(".sai-stat");
    if (el._h !== h) { const first = !el._h; setHTML(el, h); if (!first) [...el.children].forEach(rise); }
    const off = r.querySelector('[data-sai="off"]'); off.disabled = !V.saved.preset;
    setTip(off, V.saved.preset ? "停用 AI：清除服务商配置与 API Key；用量记录、每日额度与自填单价将保留。" : "尚未配置 AI");
  }
  function paintTest() {
    const r = root(); if (!r) return; const b = r.querySelector('[data-sai="test"]'), n = dirtyN();
    const why = !enabled() ? "请先填写并保存服务商、API Key 与模型" : n ? "请先保存服务商修改，再测试连接" : "按已保存的配置发送轻量测试请求，检测延迟及工具调用能力（消耗少量 token，不计入每日额度）";
    b.disabled = V.testing || !enabled() || n > 0; setTip(b, why);
    const sp = b.querySelector(".ico"); sp.classList.toggle("sai-spin", V.testing);
    const t = r.querySelector(".sai-tr"), h = V.testing ? `<span class="sai-tf">测试中…</span>` : V.test || "";
    if (t._h !== h) { setHTML(t, h); [...t.children].forEach(rise); }
  }
  function paintBar() {
    const r = root(); if (!r) return; const n = dirtyN(), on = n > 0;
    const cnt = r.querySelector(".sai-n"), x = r.querySelector(".sai-discard"), s = r.querySelector(".sai-save");
    if (on) setText(cnt.lastElementChild, `${n} 项修改未保存`);
    cnt.classList.toggle("on", on); x.classList.toggle("on", on); x.tabIndex = on ? 0 : -1;
    if (!V.saving) { s.classList.toggle("on", on); s.setAttribute("aria-disabled", !on); setTip(s, on ? "" : "暂无未保存的修改"); }
    // 左边那句：会改变结果的才写（换了服务商或网站，已存的 Key 会清掉）
    const gone = V.keySet && keyGone() && !V.cur.key.trim() && !pre()?.noKey && !!V.cur.preset;
    const h = gone ? `<i data-ic="warn"></i><span title="${esc(V.cur.preset !== V.saved.preset ? "已更换服务商：保存后原有 API Key 将被清除（不会发送至新地址），需重新填写；也可先在上方填入新 API Key 后保存。" : "接口地址已切换至其他域名：保存后原有 API Key 将被清除（不会发送至新地址），需重新填写。")}">保存将清除旧 API Key</span>` : "";
    const note = r.querySelector(".sai-note"); if (note._h !== h) { setHTML(note, h); [...note.children].forEach(rise); }
    paintTest();
  }
  function paintPrices() {
    const r = root(); if (!r) return; const p = PRE[V.saved.preset], need = !!p && !p.local;
    const src = !need ? (p ? "本机模型不计费，不下载公开价格" : "配置付费服务商后自动下载公开价格（仅配置 Ollama 时不下载）")
      : V.priceBusy ? "正在下载公开价格…" : `公开价格 · ${V.priceAt} 更新 · ${V.priceN.toLocaleString("en-US")} 个模型`;
    const el = r.querySelector(".sai-psrc"); if (setHTML(el, `<span title="${need ? "按公开价格估算，每 7 天自动刷新一次" : ""}">${esc(src)}</span>`)) {}
    const b = r.querySelector('[data-sai="prices"]'); b.disabled = !need || V.priceBusy; b.querySelector(".ico").classList.toggle("sai-spin", V.priceBusy);
    // 当前模型：有自填单价时就在下面那一行标「当前」（一个值只说一次），这里不写
    const m = V.saved.model, has = V.prices.some(x => x.model === m);
    const mp = !p || has ? "" : p.local ? "当前模型：本机模型，不计费" : `未找到 ${m || "当前模型"} 的公开价格，仅统计 token 用量，可在下方自填单价`;
    const mpe = r.querySelector(".sai-mp"); mpe.hidden = !mp; setHTML(mpe, mp ? `<span>${esc(mp)}</span>` : "");
    r.querySelectorAll(".sai-pr").forEach(row => row.classList.toggle("cur", row.dataset.m === m));
  }
  const paintAll = animate => { paintForm(animate); paintStat(); paintBar(); paintPrices(); };

  // =====================================================================================================
  // 菜单、模型列表、弹窗：挂在 body 上；点外面、Esc、块里一滚就收
  // =====================================================================================================
  let menu = null, pop = null, mdl = null;
  function place(el, anchor, alignRight) {
    const r = anchor.getBoundingClientRect(), w = el.offsetWidth, h = el.offsetHeight, up = r.bottom + 4 + h > innerHeight - 8;
    el.classList.toggle("up", up);
    el.style.left = Math.round(Math.max(8, Math.min(alignRight ? r.right - w : r.left, innerWidth - w - 8))) + "px";
    el.style.top = Math.round(up ? r.top - h - 4 : r.bottom + 4) + "px";
  }
  function openMenu(anchor, items, pick, opt = {}) {
    if (menu && menu._a === anchor) { closeMenu(); return; }
    closeMenu(); closePop();
    const m = document.createElement("div"); m.className = "sai-dm enter"; m.setAttribute("role", "menu");
    m.innerHTML = (opt.header ? `<div class="sai-dh">${esc(opt.header)}</div>` : "") + items.map(it => `<div class="sai-it${it.on ? " here" : ""}" role="menuitem" data-v="${esc(it.v)}"${it.tip ? ` title="${esc(it.tip)}"` : ""}>`
      + (it.ic ? `<i data-ic="${it.ic}"></i>` : "") + `<span class="sai-im">${esc(it.l)}</span>${it.s ? `<span class="sai-is">${esc(it.s)}</span>` : ""}${it.on ? `<i data-ic="check" class="sai-ck"></i>` : ""}</div>`).join("");
    document.body.appendChild(m); C.KIT.hydrate(m);
    if (opt.match) m.style.minWidth = anchor.getBoundingClientRect().width + "px";
    place(m, anchor, opt.right);
    m.addEventListener("click", e => { const it = e.target.closest(".sai-it"); if (!it) return; closeMenu(); if (!it.classList.contains("here")) pick(it.dataset.v); });
    anchor.classList.add("open"); anchor.setAttribute("aria-expanded", "true"); m._a = anchor; menu = m;
  }
  function closeMenu() {
    if (!menu) return; const m = menu; menu = null;
    m._a.classList.remove("open"); m._a.setAttribute("aria-expanded", "false"); m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  // 模型列表：搜索框（32 高胶囊）、重新获取（28 见方），下面一行一个（32 高、圆角 10），现在填的那个打勾
  function openModels(anchor) {
    if (pop && pop._a === anchor) { closePop(); return; }
    closePop(); closeMenu();
    if (!pre()) { const f = fld("preset"); f.classList.add("bad"); shake(f.querySelector(".sai-sel")); return; }
    const box = anchor.closest(".sai-in"), p = document.createElement("div"); p.className = "sai-pop enter";
    p.innerHTML = `<div class="sai-ph2"><label class="sai-qq"><i data-ic="search"></i><input placeholder="搜索模型" autocomplete="off" spellcheck="false" aria-label="搜索模型"></label>`
      + `<button type="button" class="sai-ib" data-p="reload" title="重新获取模型列表"><i data-ic="refresh"></i></button></div><div class="sai-ml" role="listbox"></div><div class="sai-mmsg"></div>`;
    document.body.appendChild(p); C.KIT.hydrate(p);
    p.style.width = Math.max(box.offsetWidth, 280) + "px"; p._a = anchor; pop = p; anchor.classList.add("open");
    p.addEventListener("pointerdown", e => { if (e.target.closest(".sai-mo")) e.preventDefault(); });
    p.addEventListener("click", e => { const o = e.target.closest(".sai-mo"); if (o) { pickModel(o.dataset.id); return; } if (e.target.closest('[data-p="reload"]')) loadModels(true); });
    p.querySelector("input").addEventListener("input", () => listModels());
    p.querySelector("input").addEventListener("keydown", e => { if (e.key === "Enter") { const o = p.querySelector(".sai-mo"); if (o) pickModel(o.dataset.id); } });
    loadModels(false); place(p, box); p.querySelector("input").focus();
  }
  function loadModels(force) { // 草图：装作去拉了一下；拉到的就是 presets.go 里写着的那几个
    const p = pop; if (!p) return; const id = V.cur.preset, cached = V.models[id];
    if (cached && !force) { listModels(); return; }
    p.querySelector(".sai-ml").innerHTML = ""; p.querySelector(".sai-mmsg").textContent = "正在获取模型列表…";
    const ic = p.querySelector('[data-p="reload"] .ico'); ic.classList.add("sai-spin");
    setTimeout(() => { V.models[id] = pre()?.models || []; if (pop !== p) return; ic.classList.remove("sai-spin"); listModels(true); }, 600);
  }
  function listModels(animate) {
    const p = pop; if (!p) return; const all = V.models[V.cur.preset] || [], q = p.querySelector("input").value.trim().toLowerCase();
    const rows = all.filter(m => !q || m.toLowerCase().includes(q)), list = p.querySelector(".sai-ml");
    list.innerHTML = rows.map(m => `<button type="button" class="sai-mo${m === V.cur.model.trim() ? " on" : ""}" role="option" data-id="${esc(m)}"><i data-ic="check"></i><span>${esc(m)}</span></button>`).join("");
    C.KIT.hydrate(list); if (animate) [...list.children].forEach((x, i) => rise(x, i));
    p.querySelector(".sai-mmsg").textContent = !all.length ? "未获取到模型列表，请直接输入模型名" : !rows.length ? "无匹配模型，可直接在输入框填写" : `共 ${all.length} 个模型，也可直接输入`;
    place(p, p._a.closest(".sai-in"));
  }
  function pickModel(id) { V.cur.model = id; closePop(); paintField("model"); paintBar(); fld("model").querySelector("input").focus(); }
  function closePop() {
    if (!pop) return; const p = pop; pop = null;
    p._a.classList.remove("open"); p.classList.remove("enter"); p.classList.add("leave"); setTimeout(() => p.remove(), 90);
  }
  // 弹窗：同共用件页（外 26 / 内边距 8，里面一栏 18；底下一排 36 高胶囊等宽铺满）。危险的标题前一个危险色图标块、默认聚焦「取消」；别的落在主键
  function modal({ ic, danger, title, body, ok, cancel = "取消", w = 400, onOk, wire }) {
    closeModal(true); closeMenu(); closePop();
    const mk = mdl = Object.assign(document.createElement("div"), { className: "sai-mask enter" });
    mk.innerHTML = `<div class="sai-md" role="dialog" aria-modal="true" style="width:${w}px"><div class="sai-mc"><div class="sai-mh${danger ? "" : " plain"}">${danger ? `<span class="sai-mi"><i data-ic="${ic}"></i></span>` : ""}<h3>${title}</h3></div>`
      + `<div class="sai-mbd">${body}</div></div><div class="sai-ma"><button type="button" class="sai-b${danger ? " fk" : ""}" data-x="no">${cancel}</button><button type="button" class="sai-b ${danger ? "dgr" : "pri fk"}" data-x="ok">${ok}</button></div></div>`;
    document.body.appendChild(mk); C.KIT.hydrate(mk);
    const fk = mk.querySelector(".fk"); wire?.(mk); if (!mk.querySelector(":focus")) fk.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) fk.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mdl !== mk) return;
      if (e.target.closest("[data-x=ok]")) { if (onOk(mk) !== false) closeModal(); return; }
      if (e.target.closest("[data-x=no]") || e.target === mk) closeModal();
    });
  }
  function closeModal(now) {
    if (!mdl) return; const mk = mdl; mdl = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("click", e => {
    if (menu && !e.composedPath().some(n => n === menu || n === menu._a)) closeMenu();
    if (pop && !e.composedPath().some(n => n === pop || n === pop._a)) closePop();
  });
  document.addEventListener("keydown", e => { if (e.key !== "Escape") return; if (mdl) closeModal(); else { closeMenu(); closePop(); } });
  addEventListener("resize", () => { closeMenu(); closePop(); const r = root(); if (r) r.querySelectorAll(".sai-seg").forEach(s => placeSeg(s, false)); });

  // 存好、复制好：图标换成勾 1.2 秒
  function done(b) {
    if (b._done) return; b._done = true; const old = b.innerHTML;
    b.classList.add("done"); b.innerHTML = b.classList.contains("sai-pill") ? C.KIT.icon("check") + b.textContent : C.KIT.icon("check");
    setTimeout(() => { b.classList.remove("done"); b.innerHTML = old; b._done = false; }, 1200);
  }

  // =====================================================================================================
  // 动作
  // =====================================================================================================
  function pickPreset(id) { // 同程序 f.preset.onchange：填好地址和建议模型（都可以改）
    const p = PRE[id], c = V.cur, s = V.saved;
    c.preset = id; c.url = id === "custom" ? (s.preset === "custom" ? s.url : "") : p.url;
    c.model = id === s.preset ? s.model : (p.model || "");
    if (id === "custom" && s.preset !== "custom") { c.proto = "openai"; c.auth = ""; c.echo = "auto"; }
    fld("preset").classList.remove("bad");
    paintForm(); paintBar();
  }
  // 存左栏的表单。返回存成了没有（布尔或 Promise）：外壳「离开前拦」选了保存时要等它，没选服务商被拦下、清旧 Key 的确认点了取消，都算没存成
  let saveP = null;
  function save() {
    if (V.saving) return saveP;
    if (!dirtyN()) return true;
    if (!pre()) { const f = fld("preset"); f.classList.add("bad"); shake(f.querySelector(".sai-sel")); return false; } // 同程序「先选择服务商」
    const commit = clearKey => saveP = new Promise(ok => {
      V.saving = true; const s = root().querySelector(".sai-save"); s.innerHTML = C.KIT.icon("spinner", "sai-spin") + "保存";
      setTimeout(() => { // 草图里装作等了一下请求
        V.saving = false;
        const typed = V.cur.key.trim();
        if (typed) V.keySet = true; else if (clearKey) V.keySet = false;
        V.saved = { preset: V.cur.preset, url: V.cur.url.trim(), model: V.cur.model.trim(), proto: V.cur.proto, auth: V.cur.auth, echo: V.cur.echo, effort: V.cur.effort };
        V.cur = { ...V.saved, key: "" }; V.test = null;
        if (V.saved.preset) C.setAIOff?.(false); // 重新配好：胶囊里的 AI 助手回来（外观里关着的话照旧不显示）
        const r = root();
        if (r) {
          r.querySelector(".sai-save").textContent = "保存";
          const ki = fld("key").querySelector("input"); ki.value = "";
          paintAll(true);
        }
        ok(true);
      }, 450);
    });
    if (V.keySet && keyGone() && !V.cur.key.trim() && !pre().noKey) { // 会清掉已存的 Key：先说清楚，确认了才存（同程序）。用共用的确认框，点了取消能告诉外壳没存成
      closeMenu(); closePop();
      return C.KIT.confirm({ ic: "key", tone: "dgr", title: "清除旧 API Key 并保存？",
        body: V.cur.preset !== V.saved.preset ? `服务商已切换为「${esc(pre().name)}」，原有 API Key 不会迁移，保存后将被清除。可先在上方输入新 API Key 后再保存。`
          : "接口地址已切换至新域名，原有 API Key 不会发送至新地址，保存后将被清除。可先在上方输入新 API Key 后再保存。",
        btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: "清除 API Key 并保存", tone: "dgr" }] }).then(k => (k === "ok" ? commit(true) : false));
    }
    return commit(false);
  }
  function discard() { V.cur = { ...V.saved, key: "" }; fld("key").querySelector("input").value = ""; fld("preset").classList.remove("bad"); paintForm(); paintBar(); }
  function test() {
    if (V.testing || dirtyN() || !enabled()) return;
    V.testing = true; V.test = null; paintTest();
    setTimeout(() => { // 草图编的结果
      V.testing = false;
      const local = PRE[V.saved.preset]?.local;
      V.test = `<span class="sai-tok" title="测试包含一轮工具调用"><i data-ic="check"></i>连接正常 · 首次响应 ${local ? "1,240" : "820"} ms · 支持工具调用 · 消耗 1,204 token${local ? "（本机不计费）" : "（约 ¥0.0015）"}</span>`;
      paintTest();
    }, 900);
  }
  function stopAI() {
    modal({ ic: "power", danger: true, title: "停用 AI？", ok: "停用", body: "将清除服务商配置与 API Key；用量记录、每日额度与自填单价会保留，后续可随时重新配置。",
      onOk: () => { V.saved = { preset: "", url: "", model: "", proto: "openai", auth: "", echo: "auto", effort: "" }; V.cur = { ...V.saved, key: "" }; V.keySet = false; V.test = null; V.models = {};
        fld("key").querySelector("input").value = ""; paintAll(true); C.setAIOff?.(true); } }); // 停用以后胶囊里的 AI 助手跟着藏起来（作者 11-29 定）
  }
  function clearKey() {
    modal({ ic: "trash", danger: true, title: "清除 API Key？", ok: "清除", body: "清除后 AI 功能将停用，需要时可重新填写。",
      onOk: () => { V.keySet = false; V.test = null; paintAll(true); } });
  }
  function saveQuota(row) { // 返回存成了没有（填错被拦下是 false）
    const id = row.dataset.q, box = row.querySelector(".sai-in"), inp = box.querySelector("input"), b = box.querySelector(".sai-ib"), raw = inp.value.trim();
    if (V.qd[id] === undefined || V.qd[id] === V.quota[id]) return true;
    const n = raw === "" ? 0 : +raw;
    if (!(n >= 0) || n > 1e8) { box.classList.add("bad"); const m = box.querySelector(".sai-msg"); m.textContent = "需大于等于 0"; m.title = "请填写不小于 0 的数值（单位：万 token）"; shake(box); inp.focus(); return false; }
    V.quota[id] = n ? String(n) : ""; delete V.qd[id]; if (inp.value !== V.quota[id]) inp.value = V.quota[id]; // 0 就是不限：框空着，占位写着「不限」
    box.classList.remove("dirty", "bad"); b.disabled = true; done(b);
    return true;
  }
  // ---- 离开前拦（kit.js 第 6 条）：左栏表单改了的几项，加右栏额度框里改了没存的 ----
  const quotaMod = () => Object.keys(V.qd).filter(id => V.qd[id] !== V.quota[id]);
  const dirtyAll = () => (V ? (V.saving ? 0 : dirtyN()) + quotaMod().length : 0); // 表单正在存的那一下不算没存；还没画过这一页时 V 是空的，算 0
  async function saveAll() { // 额度一个一个存（填错的就地拦下），再存表单；有一处没存成就留在这页
    for (const id of quotaMod()) if (!saveQuota(root().querySelector(`.sai-f[data-q="${id}"]`))) return false;
    return dirtyN() ? await save() : true;
  }
  function discardAll() { // 表单走「放弃修改」；额度框的草稿丢掉、框里换回存过的
    discard(); V.qd = {};
    root()?.querySelectorAll(".sai-f[data-q]").forEach(row => {
      const box = row.querySelector(".sai-in"), inp = box.querySelector("input"), b = box.querySelector(".sai-ib"), v = V.quota[row.dataset.q];
      if (inp.value !== v) inp.value = v; box.classList.remove("dirty", "bad"); if (!b._done) b.disabled = true;
    });
  }
  function delPrice(row) { // 往右划 32 像素淡掉（0.18 秒）再收起（规范第七节「清单加减」）
    const m = row.dataset.m; V.prices = V.prices.filter(x => x.model !== m);
    row.style.pointerEvents = "none";
    row.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(32px)" }], { duration: 180, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
      const h = row.offsetHeight;
      row.animate([{ height: h + "px", marginTop: "0px" }, { height: "0px", marginTop: "-2px" }], { duration: 180, easing: SLIDE, fill: "forwards" }).onfinish = () => { row.remove(); paintPrices(); root()?.querySelector(".sai-lw")._paint?.(); };
    };
  }
  function addPrice() { // 表单弹窗：模型名、币种、输入 / 输出 / 缓存命中（每百万 token）；不对的就地拦下（同程序的三句）
    const ib = (k, ph, v = "", mono) => `<label class="sai-in${mono ? " mono" : ""}" data-pf="${k}"><input value="${esc(v)}" placeholder="${ph}" autocomplete="off" spellcheck="false"${k === "model" ? "" : ' inputmode="decimal"'}><span class="sai-msg"></span></label>`;
    modal({ title: "自填单价", ok: "添加", w: 460,
      body: `<div class="sai-pform"><div class="sai-f"><div class="sai-lb"><span>模型名</span></div>${ib("model", "模型名", V.saved.model, 1)}</div>`
        + `<div class="sai-f"><div class="sai-lb"><span title="不做汇率换算，人民币与美元分别合计">币种</span></div>${segHTML("pcur", [["CNY", "人民币（¥）"], ["USD", "美元（$）"]])}</div>`
        + `<div class="sai-p3"><div class="sai-f"><div class="sai-lb"><span title="每百万 token">输入</span></div>${ib("in", "每百万 token")}</div><div class="sai-f"><div class="sai-lb"><span title="每百万 token">输出</span></div>${ib("out", "每百万 token")}</div>`
        + `<div class="sai-f"><div class="sai-lb"><span title="每百万 token；选填">缓存命中</span></div>${ib("cache", "选填")}</div></div></div>`,
      wire: mk => {
        const seg = mk.querySelector(".sai-seg"); seg.querySelector('[data-v="CNY"]').classList.add("on"); requestAnimationFrame(() => placeSeg(seg, false));
        seg.addEventListener("click", e => { const b = e.target.closest("button"); if (!b) return; seg.querySelectorAll("button").forEach(x => x.classList.toggle("on", x === b)); placeSeg(seg, true); });
        mk.addEventListener("input", e => e.target.closest(".sai-in")?.classList.remove("bad"));
        mk.querySelector('[data-pf="in"] input').focus();
      },
      onOk: mk => {
        const g = k => mk.querySelector(`[data-pf="${k}"]`), val = k => g(k).querySelector("input").value.trim();
        const bad = (k, short, full) => { const b = g(k), m = b.querySelector(".sai-msg"); b.classList.add("bad"); m.textContent = short; m.title = full; shake(b); b.querySelector("input").focus(); return false; };
        const num = x => x !== "" && +x >= 0;
        if (!val("model")) return bad("model", "必填", "请填写模型名");
        if (!num(val("in"))) return bad("in", "必填", "请填写不小于 0 的输入单价");
        if (!num(val("out"))) return bad("out", "必填", "请填写不小于 0 的输出单价");
        if (val("cache") !== "" && !num(val("cache"))) return bad("cache", "无效", "缓存命中单价需为不小于 0 的数值");
        const it = { model: val("model"), cur: mk.querySelector(".sai-seg button.on").dataset.v, in: +val("in"), out: +val("out"), cache: val("cache") === "" ? null : +val("cache") };
        const list = root().querySelector(".sai-pl"), old = list.querySelector(`.sai-pr[data-m="${CSS.escape(it.model)}"]`);
        const i = V.prices.findIndex(x => x.model === it.model); if (i >= 0) V.prices[i] = it; else V.prices.push(it); // 同一个模型已经有的，换成这一份（同后端）
        const row = document.createElement("template"); row.innerHTML = priceRow(it); const el = row.content.firstElementChild; C.KIT.hydrate(el);
        if (old) old.replaceWith(el); else list.appendChild(el);
        rise(el); paintPrices(); root().querySelector(".sai-lw")._paint?.();
      } });
  }
  function toggleMcp(b) {
    V.mcp = !V.mcp; b.classList.toggle("on", V.mcp); b.setAttribute("aria-checked", V.mcp);
    setTip(b, mcpTip());
    const box = root().querySelector(".sai-mcp"), lw = root().querySelector(".sai-lw");
    box.getAnimations({ subtree: true }).forEach(a => a.cancel());
    if (V.mcp) { box.hidden = false; [...box.children].forEach((x, i) => rise(x, i)); lw._paint?.(); }
    else box.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN }).onfinish = () => { if (!V.mcp) { box.hidden = true; lw._paint?.(); } };
  }
  function snips(b) {
    openMenu(b, SNIPS.map(s => ({ v: s.id, l: snipLabel(s), ic: s.t === "stdio" ? "monitor" : "globe" })), id => {
      const s = SNIPS.find(x => x.id === id);
      modal({ title: `${esc(s.name)}${s.name.endsWith("）") ? "" : " "}配置`, ok: "复制配置", cancel: "关闭", w: 520, body: `${esc(s.where)}<pre class="sai-pre">${esc(s.text)}</pre>`, onOk: () => done(b) });
    }, { header: "选择目标 AI 工具", right: true });
  }

  // =====================================================================================================
  // 挂上去
  // =====================================================================================================
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    ctx.thinBar(r.querySelector(".sai-lw"));
    paintAll(false);
    r.querySelectorAll(".sai-seg").forEach(s => placeSeg(s, false));
    document.fonts?.ready.then(() => { if (r.isConnected) r.querySelectorAll(".sai-seg").forEach(s => placeSeg(s, false)); });
    if (V.top) r.querySelector(".sai-lw .list").scrollTop = V.top;
    r.querySelector(".sai-lw .list").addEventListener("scroll", e => { V.top = e.target.scrollTop; closeMenu(); closePop(); }, { passive: true });
    r.addEventListener("click", e => {
      const sb = e.target.closest(".sai-f:not(.off) .sai-seg button");
      if (sb && !sb.classList.contains("on")) { const k = sb.closest(".sai-seg").dataset.seg; V.cur[k] = sb.dataset.v; paintField(k); if (k === "proto") { paintField("auth"); paintField("effort"); } paintBar(); return; }
      const b = e.target.closest("[data-sai]"); if (!b || b.disabled || b.getAttribute("aria-disabled") === "true" || b.closest(".sai-f.off")) return;
      const a = b.dataset.sai;
      if (a === "sel") {
        const k = b.dataset.sel, c = V.cur;
        if (k === "preset") openMenu(b, PRESETS.map(p => ({ v: p.id, l: p.name, on: p.id === c.preset })), pickPreset, { match: 1 });
        else if (k === "auth") openMenu(b, AUTHS.map(([v, l]) => ({ v, l, on: v === c.auth })), v => { c.auth = v; paintField("auth"); paintBar(); }, { match: 1 });
        else openMenu(b, EFFORTS.map(([v, l]) => ({ v, l, on: v === c.effort })), v => { c.effort = v; paintField("effort"); paintBar(); }, { match: 1, right: 1 });
        return;
      }
      switch (a) {
        case "models": e.preventDefault(); openModels(b); break;
        case "alts": { e.preventDefault(); const p = pre(); openMenu(b, [{ l: "默认", u: p.url }, ...p.alts].map(x => ({ v: x.u, l: x.l, s: x.u.replace(/^https?:\/\//, ""), tip: x.u, on: sameUrl(x.u, V.cur.url) })),
          u => { V.cur.url = u; paintForm(); paintBar(); }, { right: 1 }); break; }
        case "clearkey": e.preventDefault(); clearKey(); break;
        case "off": stopAI(); break;
        case "test": test(); break;
        case "save": save(); break;
        case "discard": discard(); break;
        case "q": e.preventDefault(); saveQuota(b.closest(".sai-f")); break;
        case "prices": V.priceBusy = true; paintPrices(); setTimeout(() => { V.priceBusy = false; V.priceAt = "刚刚"; paintPrices(); }, 900); break;
        case "delprice": delPrice(b.closest(".sai-pr")); break;
        case "addprice": addPrice(); break;
        case "lan": V.lan = !V.lan; b.classList.toggle("on", V.lan); b.setAttribute("aria-checked", V.lan); break;
        case "sess": modal({ ic: "trash", danger: true, title: "清空全部会话？", ok: "清空", body: "将删除本机保存的全部 AI 助手会话记录（含局域网设备创建的会话），删除后无法恢复。",
          onOk: () => { V.sess = 0; b.disabled = true; setTip(b, "暂无会话记录"); } }); break;
        case "mcp": toggleMcp(b); break;
        case "copyurl": done(b); break;
        case "token": modal({ ic: "key", danger: true, title: "重置 MCP 访问令牌？", ok: "重置",
          body: "重置后旧令牌立即失效。通过 HTTP 连接的 AI 工具（Cherry Studio、Cursor、VS Code 等）需更新为新令牌；stdio 方式（Claude Desktop）无需修改。", onOk: () => done(b) }); break;
        case "snips": snips(b); break;
      }
    });
    r.addEventListener("input", e => {
      const i = e.target, k = i.dataset.in; if (!k) return;
      if (k === "q") { const row = i.closest(".sai-f"), id = row.dataset.q, box = i.closest(".sai-in"); V.qd[id] = i.value; const d = i.value !== V.quota[id];
        box.classList.toggle("dirty", d); box.classList.remove("bad"); const b = box.querySelector(".sai-ib"); if (!b._done) b.disabled = !d; return; }
      V.cur[k] = i.value;
      if (k === "url") { paintField("url"); paintField("key"); paintField("proto"); paintField("auth"); paintField("effort"); }
      else paintField(k);
      paintBar();
    });
    r.addEventListener("keydown", e => {
      if (e.key !== "Enter" || e.isComposing) return;
      if (e.target.dataset.in === "q") { e.preventDefault(); saveQuota(e.target.closest(".sai-f")); }
      else if (e.target.dataset.in) { e.preventDefault(); e.target.blur(); } // 回车只是改完这一格；保存在下面
    });
  }

  window.SETTINGS_SECS.ai = { html, mount, dirty: dirtyAll, save: saveAll, discard: discardAll };
})();
