// 服务器详情页 →「扩展」的三个子页：游戏内 AI（ext/gameai）、AI 伙伴（ext/companion，只有 Forge / NeoForge）、音乐共享（ext/music，只有 Forge / NeoForge / Mohist）。
// 11-27 夜里派子代理照临时规范画的第一稿；计划书把这三页归在「主会话照规范先做出大概、作者整体扫一眼」。样式在 detail-ext.css，选择器、图标名都带 ex 前缀。
//
// 程序现在的样子：internal/web/ui/gamechat.js、companion.js、music.js（app.js 约 3582 行登记标签、约 4632 行分派）。三页在程序里都是一长串「分组 + 一行一项」，
// 每行一个图标瓦片、一句标题、一两句说明，单字段的框旁边一颗「保存」。这里照临时规范重排：说明进悬停，只留会拦住下一步或会改变结果的那句；
// 系统控件（数字框、range、select、datalist、勾选框、单选）全换成自己画的步进、条、菜单、勾、行加滑动选中框；页面本身不滚，长的在栏里滚（细滚动条）。
//
// 三页的骨架：
//   · 游戏内 AI、AI 伙伴有总开关：内容面板最上面一排 32 高——左边开关（在字的左边）和状态，右边一个 32 高胶囊分段「换着看什么」（同首页、联机页、插件页）；
//     下面一块按分段换内容，里面并排两三栏（圆角 18 = 26 − 8），每栏自己滚。换分段时旧的几栏往反方向挪 5 像素淡掉、新的一栏一栏淡进来（规范第七节）。
//   · 音乐共享没有总开关，不分段：左边一栏「广播」（音源、采集方式、音量、开始广播），右边上「检查清单」、下「听众」。
//
// ---- 游戏内 AI ----
// 程序（gamechat.js）从上到下：AI 没配置 / 局域网只能看的提示；打开开关、触发前缀、别名、「提问会发给 X（模型）」和今天的用量；能让 AI 执行命令的玩家（没开白名单时的提醒，
// 一键开启白名单）、按名字授权；命令模板十个开关；限流和额度四个数字框、今天谁问了几次、所有服务器的每日总额度；最近的问答和执行记录；识别测试；高级识别（折叠）。
// 这里：顶上一排「游戏内 AI」开关 +（没配 AI 时）一颗警告胶囊「AI 还没配置 · 去设置」，右边分段「设置 · 问答记录 · 识别测试」。
//   · 设置：三栏。左「提问」：触发前缀、别名（并排两格，40 高、圆角 10，框里右边一颗保存，没改时灰着、回车也能存）、「提问会发给」（点了去「设置 → AI」，草图里不跳）；
//     下面「限流和额度」：四项用步进（一下一档，档位见「替作者定的」），「全服每天」「全服每天的 token」下面一行淡字是今天用了多少（谁问了几次在悬停里），
//     最后一行是所有服务器合计的每日总额度。中「能执行命令的玩家」：一行一人，开关在名字左边，在线的名字后面一个成功色小方点，管理员一个王冠，
//     「白名单」「最近来过」是小标签；没开白名单又授了权时，栏顶一条警告「没有开启白名单」带「开启白名单」（确认框里勾要加进白名单的人）；底下按名字授权的框。
//     右「命令模板」：一行一个，整行是开关，第二行等宽字是命令的写法，限制和「默认关着」进悬停。
//   · 问答记录：一栏，记录之间用分隔线：谁、类型、选项、「可执行命令」、用量和时间一行；问、答、执行的命令（✓ / ✗ 加原因）、命令输出、报错往下排，全文不截。
//   · 识别测试：上一栏贴一行日志、「填入示例」、「识别」，下面认出的几项和一句结论（照 internal/chatai 的 describe 原话，草图里用 JS 照着认）；
//     下一栏「高级识别」：放宽名字规则、不看线程名两个开关，自定义正则（框里保存）。改了这几项，上面有结果时马上按新规则重认。
//   · 程序的「局域网打开时只能看」草图没画（草图是本机窗口）。
//
// ---- AI 伙伴 ----
// 程序（companion.js）从上到下：加载器版本不对 / AI 没配置 / 游戏内 AI 没开的提示；安装（三个模组的版本和状态、安装或更新）；好友的客户端（要装 / 不用装、
// 复制给好友的说明）；设置（打开伙伴、伙伴上限、攻击时不许打的生物、AI 伙伴的每日额度）；禁挖区域（弹窗编辑）；和伙伴模组的连接 + 伙伴列表（刷出、停下、移除、全部停下）；最近的事件。
// 这里：顶上一排「AI 伙伴」开关、连接状态胶囊（连上时悬停是模组版本和心跳）、需要时一颗提示胶囊（AI 没配置 / 游戏内 AI 没开 · 去打开），右边分段「伙伴 · 设置 · 安装」，
// 要装、要换模组时「安装」后面一个警告色小点，打开这页先停在「安装」。
//   · 伙伴：左「伙伴」一行一个（名字、状态、主人；血量、维度和坐标；正在做的或上次的结果；「停下」和移除），底下「刷出伙伴」「全部停下」（40 高、圆角 10）；右「最近的事件」。
//   · 设置：左「设置」：伙伴上限（步进 1–16）、攻击时不许打的生物（小胶囊一个一个列，× 拿掉，下面框里加，标题旁「恢复默认」）、AI 伙伴的每日额度；
//     改了以后栏标题旁转 2 秒的小圈（伙伴模组每 2 秒读一次设置）。右「禁挖区域」：一行一个，点行编辑，× 删除（危险确认框），右上角 ＋ 添加（表单弹窗）。
//   · 安装：左「安装」三个模组（状态标签、装的或要装的版本，依赖带去 Modrinth 的外链图标），底下一条说现在该做什么（安装 / 先停服再安装 / 装好了要重启 / 都装好了）；
//     右「好友的客户端」：要装、不用装，伙伴模组旁边「打开 mods 文件夹」，依赖旁边去下载页；右上角复制给好友的说明；最底下一句「整合包要装同一个版本」（会改变结果，留在页上）。
//   · 刷出伙伴、编辑禁挖区域照共用件页的表单弹窗画；维度、出生在用自己画的菜单（程序是 select），「主人」底下列在线的人点一下填进去（程序是 datalist）。
//
// ---- 音乐共享 ----
// 程序（music.js）从上到下：广播（选正在出声的程序 → 开始；正在播时音源、听众数、网络、停止）；检查清单（核心、SVC、桥接模组、语音端口、远程语音 + 手动填 voice_host）；
// 听众（谁连上了语音、谁没装、版本不兼容，发给好友的说明）；设置（我在游戏里的名字、音量）；高级选项（采集方式、整机录音的播放设备、桥接模组的加载方式）。
// 这里：左「广播」：标题行后面淡字是上次广播为什么停的；中间一行一个正在出声的程序（电平原地跳，选中框滑过去）、整机录音时换成提醒和「播放设备」菜单、正在广播时换成广播的样子；
// 下面「采集方式」（40 高分段，程序在高级选项里）、「音量」（自己画的条，把手是竖着的小胶囊，0–200%），最底下一行左边是为什么点不了（会拦住下一步，留在页上）、右边「开始广播」。
// 右上「检查清单」五行：✓ / ! / 图标、名字、值、要做的事（安装、去联机）；桥接模组和远程语音的「···」里是「换成备用加载方式」「手动填写 voice_host」（后者弹一块小浮层）。
// 右下「听众」：会听到音乐 / 没连上语音 / 不再发给，一人一颗小胶囊；右上角复制发给好友的说明（悬停看全文）；底下「我在游戏里的名字」。
// 语音隧道不在这里管（作者 11-26 定：一台最多游戏、语音各一条，在「联机」里建）；这里没有语音隧道时给一颗「去联机」（ctx.goTab("net")）。
//
// 草图里的数据：都是编的。游戏内 AI 的名单和「玩家」页一样（白名单、管理员照 detail-players.js 的 SAMPLE，原版那台白名单关着）；用量、费用估价、问答记录是假的；
// 服务商写的是程序预设里 DeepSeek 的默认模型。Forge 那台在「插件 / 模组」里没装 Simple Voice Chat、PlayerEngine、Architectury，在「联机」里也没装 SVC，
// 所以打开时伙伴、音乐两页都是「还没装」的样子（装的过程能点）；地址后面加 &mods=1 看三个模组、SVC 和桥接模组都装好、正在用的样子（草图专用）。
// &ai=0（外壳左下角「没配 AI」）看 AI 没配置的样子。在这里装上 SVC、开了白名单，改不到「联机」「玩家」那两页的数据（各页的数据各自一份）。
(() => {
  // 程序图标里没有的：伙伴、禁挖区域的镐、血量、音乐、耳机、白名单的盾，照搬程序 companion.js、music.js、gamechat.js 里的画法；王冠同「玩家」页那顶；铅笔同 import.html
  const CROWN = "M2.6 5.3l2.8 2.6L8 3.6l2.6 4.3 2.8-2.6-1.1 6.2H3.7z";
  Object.assign(window.EXTRA_ICONS = window.EXTRA_ICONS || {}, {
    exBot: '<rect x="3" y="4.6" width="10" height="8.2" rx="2"/><path d="M8 2.2v2.4M6.1 8.3h.01M9.9 8.3h.01M6.3 10.7h3.4"/>',
    exPick: '<path d="M3.2 12.8l6.4-6.4"/><path d="M5.6 3.6c2.4-1 5-.4 6.8 1.4 1.8 1.8 2.4 4.4 1.4 6.8"/>',
    exHeart: '<path d="M8 13.2S2.6 9.9 2.6 6.1A2.8 2.8 0 018 4.7a2.8 2.8 0 015.4 1.4C13.4 9.9 8 13.2 8 13.2z"/>',
    exMusic: '<path d="M6 12V3.7l7-1.5v8.3"/><circle cx="4.3" cy="12" r="1.8"/><circle cx="11.3" cy="10.5" r="1.8"/>',
    exHeadphones: '<path d="M2.8 10.2V8.3a5.2 5.2 0 0110.4 0v1.9"/><rect x="2.3" y="9.4" width="2.8" height="4.1" rx="1"/><rect x="10.9" y="9.4" width="2.8" height="4.1" rx="1"/>',
    exShield: '<path d="M8 2.2l5 1.8v3.7c0 2.9-2.1 5.2-5 6.1-2.9-.9-5-3.2-5-6.1V4z"/><path d="M5.9 8.1l1.5 1.5 2.8-3"/>',
    exCrown: `<path d="${CROWN}" fill="currentColor"/><path d="M3.7 13.4h8.6"/>`,
    exPencil: '<path d="M10.6 2.8l2.6 2.6-7.6 7.6-3.2.6.6-3.2z"/><path d="M9.2 4.2l2.6 2.6"/>',
  });
  window.DETAIL_TABS = window.DETAIL_TABS || {};

  // ================= 共用的小件 =================
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const E_IN = "cubic-bezier(.4, 0, 1, 1)", E_OUT = "cubic-bezier(.2, .7, .2, 1)", E_SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const hy = el => { window.KIT.hydrate(el); return el; };
  const ico = n => window.KIT.icon(n);
  const same = (a, b) => String(a).toLowerCase() === String(b).toLowerCase();
  const has = (arr, n) => arr.some(x => same(x, n));
  const fmtTokens = n => { n = +n || 0; return n >= 10000 ? (Math.round(n / 1000) / 10) + " 万" : n.toLocaleString("en-US"); }; // 同 app.js
  const pad = n => String(n).padStart(2, "0");
  const nowHM = () => { const d = new Date(); return `${pad(d.getHours())}:${pad(d.getMinutes())}`; };
  let C = null; // 外壳最近一次给的 ctx
  const running = () => C.st === "running";
  const MODS = () => C.QS.get("mods") === "1"; // 草图专用：三个模组、SVC 和桥接模组都装好了
  const AI_ON = () => C.QS.get("ai") !== "0";  // 外壳左下角「没配 AI」
  const rootOf = cls => document.querySelector(`.cp > .${cls}:not(.fade-out)`);
  // 悬停提示：kit.js 悬停时把 title 换成自己的浮层并删掉 title；改提示要重新写 title、清掉旧的 data-tip（同「玩家」页）
  const setTip = (el, t) => { if (!el) return; delete el.dataset.tip; if (t) el.setAttribute("title", t); else el.removeAttribute("title"); };
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: E_SLIDE });
  const tag = (t, tone = "") => `<span class="ex-tag${tone ? " " + tone : ""}">${esc(t)}</span>`;
  const pill = (tone, t, tip = "") => `<span class="ex-pill ${tone}"${tip ? ` title="${esc(tip)}"` : ""}><i></i>${esc(t)}</span>`;
  const fld = (label, tip, inner, cls = "") => `<div class="ex-fld${cls ? " " + cls : ""}"><span class="ex-lb"${tip ? ` title="${esc(tip)}"` : ""}>${label}</span>${inner}</div>`;

  // 逐项换（规范第七节）：一样东西的内容变了，旧的 0.09 秒往反方向挪 5 像素淡掉，新的 0.22 秒从另一边挪 5 像素进来；连着点时上一次没换完的作废
  function swapIn(el, html, dir = 1, axis = "Y", after) {
    if (!el) return;
    const tf = v => `translate${axis}(${v}px)`, tok = el._exTok = (el._exTok || 0) + 1;
    el.getAnimations().forEach(a => a.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, easing: E_IN, fill: "forwards" }).onfinish = () => {
      if (el._exTok !== tok) return;
      el.innerHTML = html; hy(el); after?.();
      el.getAnimations().forEach(a => a.cancel());
      el.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, easing: E_OUT });
    };
  }
  // 一批换（换分段、整份列表换掉）：旧的一起往反方向挪 5 像素淡掉，新的一样一样淡进来（每样 0.22 秒、错开 22 毫秒）
  function swapKids(box, html, dir = 1, axis = "X", after) {
    if (!box) return;
    const tf = v => `translate${axis}(${v}px)`, tok = box._exTok = (box._exTok || 0) + 1;
    [...box.children].forEach(o => { if (o._exOut) return; o._exOut = true; o.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, easing: E_IN, fill: "forwards" }); });
    setTimeout(() => {
      if (box._exTok !== tok || !box.isConnected) return;
      box.innerHTML = html; hy(box); after?.();
      [...box.children].slice(0, 12).forEach((c, i) => c.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: E_OUT, fill: "backwards" }));
    }, 90);
  }
  // 清单加减（规范第七节）：新加的从下面淡进来；拿掉一个是那一行往右划 32 像素淡掉（0.18 秒）再收起
  const rowIn = row => row?.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: E_OUT, fill: "backwards" });
  function rowOut(row, done) {
    if (!row || row.classList.contains("ex-out")) return;
    row.classList.add("ex-out");
    const h = row.offsetHeight + "px";
    row.animate([{ opacity: 1, transform: "none", height: h, easing: E_IN }, { opacity: 0, transform: "translateX(32px)", height: h, offset: .56, easing: E_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginTop: "-2px", paddingTop: "0px", paddingBottom: "0px" }], { duration: 320, fill: "forwards" }).onfinish = () => { row.remove(); done?.(); };
  }
  const flash = row => { if (!row) return; row.scrollIntoView({ block: "nearest", behavior: "smooth" }); row.classList.remove("ex-flash"); void row.offsetWidth; row.classList.add("ex-flash"); };
  // 复制、保存以后图标换成勾 1.2 秒；刷新的图标转 0.6 秒（规范第七节）
  function doneTick(b) {
    if (!b || b._exDone) return; b._exDone = true;
    const old = b.innerHTML; b.classList.add("done"); b.innerHTML = ico("check");
    setTimeout(() => { b.classList.remove("done"); b.innerHTML = old; b._exDone = false; }, 1200);
  }
  const spinOnce = b => { if (!b || b.classList.contains("ex-spin1")) return; b.classList.add("ex-spin1"); setTimeout(() => b.classList.remove("ex-spin1"), 600); };

  // 开关：36 × 20，在字的左边（同向导第三步 EULA、「玩家」页白名单）；说明进悬停
  const tgl = ({ key, on, label = "", tip = "", dis = false, cls = "", attrs = "" }) =>
    `<button type="button" class="ex-tg${on ? " on" : ""}${cls ? " " + cls : ""}" role="switch" aria-checked="${on}" data-ex-tg="${key}"${dis ? ' aria-disabled="true"' : ""}${tip ? ` title="${esc(tip)}"` : ""}${attrs ? " " + attrs : ""}>`
    + `<span class="ex-sw"></span>${label ? `<span class="ex-tl">${label}</span>` : ""}</button>`;
  const setTgl = (b, on) => { if (!b) return; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); };

  // 分段：32 高胶囊「换着看什么」，选中块也是胶囊、滑过去；big 是表单里取值的 40 高、圆角 10（同向导第三步）
  const segHTML = (key, items, cur, big = false, dis = "") => `<div class="ex-seg${big ? " big" : ""}" data-ex-seg="${key}" role="${big ? "radiogroup" : "tablist"}"${dis ? ` aria-disabled="true" title="${esc(dis)}"` : ""}><i class="ex-sgl"></i>`
    + items.map(([v, l, extra = ""]) => `<span data-v="${v}" role="${big ? "radio" : "tab"}" aria-${big ? "checked" : "selected"}="${v === cur}" class="${v === cur ? "on" : ""}">${l}${extra}</span>`).join("") + `</div>`;
  function placeSeg(s, instant) {
    const hl = s && $(".ex-sgl", s), on = s && $("span.on", s); if (!hl || !on) return;
    if (instant) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (instant) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function pickSeg(s, v) { $$("span[data-v]", s).forEach(x => { const on = x.dataset.v === v; x.classList.toggle("on", on); x.setAttribute(s.classList.contains("big") ? "aria-checked" : "aria-selected", on); }); placeSeg(s); }

  // 步进：40 高、圆角 10，两颗 28 见方、圆角 4 的按钮离框外沿 6；一下一档，到头那边灰掉（规范第九节）
  const stpHTML = (key, text, lo, hi) => `<div class="ex-stp" data-ex-stp="${key}"><button type="button" data-d="-1" aria-label="减少一档"${lo ? " disabled" : ""}><i data-ic="minus"></i></button>`
    + `<b>${esc(text)}</b><button type="button" data-d="1" aria-label="增加一档"${hi ? " disabled" : ""}><i data-ic="plus"></i></button></div>`;
  function paintStp(el, text, lo, hi, dir) {
    if (!el) return;
    const b = $("b", el); if (b.textContent !== text) swapIn(b, esc(text), dir);
    $("[data-d='-1']", el).disabled = lo; $("[data-d='1']", el).disabled = hi;
  }
  // 一下一档；不在档上的值（以前在程序里手填的）往那个方向找最近的一档
  const ladder = (L, v, d) => d > 0 ? (L.find(x => x > v) ?? v) : ([...L].reverse().find(x => x < v) ?? v);

  // 单字段的框：40 高、圆角 10，框里右边一颗保存（28 见方、离框外沿 6、圆角 4），没改时灰着，回车也能存（程序 bindSaveInput 的保存语义）
  const saveIn = ({ key, val = "", ph = "", mono = false, max = 64, label }) => `<label class="ex-in${mono ? " mono" : ""}" data-ex-in="${key}" data-saved="${esc(val)}">`
    + `<input value="${esc(val)}" placeholder="${esc(ph)}" maxlength="${max}" spellcheck="false" autocomplete="off" aria-label="${esc(label)}"><span class="ex-msg"></span>`
    + `<button type="button" class="ex-ib r4 ex-save" title="保存" disabled><i data-ic="save"></i></button></label>`;
  // 加名字、加生物的框：同「玩家」页的输入框，回车或点框里的 ＋
  const addBox = (key, ph, plusTip, max, label) => `<label class="ex-in ex-add" data-ex-in="${key}"><input maxlength="${max}" placeholder="${esc(ph)}" spellcheck="false" autocomplete="off" aria-label="${esc(label)}">`
    + `<span class="ex-msg"></span><button type="button" class="ex-ib r4 ex-plus" title="${esc(plusTip)}"><i data-ic="plus"></i></button></label>`;
  // 拦下来：框标红、晃一下，框里右边换成一句短的红字（全文进悬停），光标落过去（规范第七节「反馈」）
  // 弹窗里的框不写在框里，栏底一行红字说全文（同共用件页「就地拦下」）
  function bad(box, msg, tip = "") {
    if (!box) return;
    box.classList.add("bad");
    const fe = box.closest(".ex-md")?.querySelector(".ex-ferr"), m = $(".ex-msg", box);
    if (fe) { fe.textContent = tip || msg; fe.classList.add("on"); if (m) m.textContent = ""; }
    else if (m) { m.textContent = msg; setTip(m, tip); }
    shake(box); $("input", box)?.focus();
  }
  function savedOK(box) {
    const inp = $("input", box), b = $(".ex-save", box);
    box.dataset.saved = inp.value; box.classList.remove("dirty", "bad"); if (b) { b.disabled = true; doneTick(b); }
  }
  // 框里打字：去掉红框；改过了保存亮起来
  function onInput(e) {
    const box = e.target.closest?.(".ex-in"); if (!box) return;
    box.classList.remove("bad");
    const fe = box.closest(".ex-md")?.querySelector(".ex-ferr"); if (fe && !box.closest(".ex-md").querySelector(".ex-in.bad")) fe.classList.remove("on");
    const b = $(".ex-save", box); if (b) { const dirty = e.target.value !== box.dataset.saved; box.classList.toggle("dirty", dirty); b.disabled = !dirty; }
    if (box.classList.contains("ex-add")) box.classList.toggle("has", !!e.target.value.trim());
  }
  // 名字放不下被截断时，全名进悬停
  const fitTitle = el => { if (!el) return; if (el.scrollWidth > el.clientWidth + 1) el.title = el.textContent; else el.removeAttribute("title"); };
  const fitNames = r => { r.querySelectorAll(".ex-nm").forEach(fitTitle); document.fonts?.ready.then(() => r.isConnected && r.querySelectorAll(".ex-nm").forEach(fitTitle)); };
  const bars = r => r.querySelectorAll(".ex-lw").forEach(w => C.thinBar(w));
  const repaint = w => { w?._paint?.(); };
  const noneOf = w => { if (!w) return; w.classList.toggle("is-none", !w.querySelector(".list > :not(.ex-out)")); repaint(w); };

  // ---- 确认框、表单弹窗：同共用件页（commons.html）、「世界与备份」的 .wd-mask 那一套（深浅两套遮罩和投影在 css 里），挂在 body 上 ----
  let mask = null;
  function ask({ ic, tone = "dgr", title, tip = "", body = "", ok, okTip = "", no = "取消", w = 400, onOk, setup, focusInput = false }) {
    closeAsk(true); closeDD(); closePop();
    const mk = mask = Object.assign(document.createElement("div"), { className: "ex-mask enter" });
    mk.innerHTML = `<div class="ex-md" role="dialog" aria-modal="true" style="--w:${w}px"><div class="ex-mc"><div class="ex-mh"><span class="ex-mi ${tone}"><i data-ic="${ic}"></i></span><h3>${title}</h3>`
      + (tip ? `<span class="ex-info" title="${esc(tip)}"><i data-ic="info"></i></span>` : "") + `</div>${body}<div class="ex-ferr" role="alert"></div></div>`
      + `<div class="ex-ma"><button type="button" class="ex-b" data-x="no">${no}</button><button type="button" class="ex-b ${tone}" data-x="ok"${okTip ? ` title="${esc(okTip)}"` : ""}>${ok}</button></div></div>`;
    document.body.appendChild(hy(mk));
    setup?.(mk);
    // 默认聚焦（同共用件页）：危险的落在「取消」，别的落在主键，有要填的落在第一个框；鼠标点开时浏览器不画焦点框，所以画一圈细框，一动别的就收
    const first = focusInput && $("input", mk), fk = first || $(tone === "dgr" ? "[data-x=no]" : "[data-x=ok]", mk);
    if (!first) fk.classList.add("fk");
    fk.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) $$(".fk", mk).forEach(x => x.classList.remove("fk")); });
    mk.addEventListener("keydown", e => {
      if (e.key === "Tab") $$(".fk", mk).forEach(x => x.classList.remove("fk"));
      if (e.key === "Enter" && e.target.tagName === "INPUT") { e.preventDefault(); $("[data-x=ok]", mk).click(); }
    });
    mk.addEventListener("input", onInput);
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      if (e.target.closest("[data-x=ok]")) { if (onOk?.(mk) !== false) closeAsk(); return; }
      // 有要填的东西时点外面不关（同程序 confirmModal：点空了填的就丢了）
      if (e.target.closest("[data-x=no]") || (e.target === mk && !$("input", mk))) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null; closeDD();
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  // ---- 菜单（代替程序的 select、datalist、「···」）：同共用件页的菜单，外圆角 16、内边距 5，一项 36 高、圆角 10 ----
  let dd = null;
  function openDD(anchor, items, cur, onPick) {
    if (dd && dd._a === anchor) { closeDD(); return; }
    closeDD(); closePop();
    const m = dd = Object.assign(document.createElement("div"), { className: "ex-dd enter" });
    m.setAttribute("role", "menu");
    m.innerHTML = items.map(it => it.sep ? `<div class="ex-dsep"></div>`
      : `<div class="ex-di${it.v === cur ? " on" : ""}${it.dgr ? " dgr" : ""}" data-v="${esc(it.v)}" role="menuitem"${it.dis ? ` aria-disabled="true" title="${esc(it.dis)}"` : ""}>`
      + `${it.ic ? `<i data-ic="${it.ic}"></i>` : ""}<span>${esc(it.l)}</span>${it.v === cur ? `<i data-ic="check" class="ex-dck"></i>` : ""}</div>`).join("");
    document.body.appendChild(hy(m));
    const r = anchor.getBoundingClientRect();
    m.style.minWidth = Math.round(Math.max(r.width, 180)) + "px";
    const below = r.bottom + 6 + m.offsetHeight < innerHeight - 8;
    m.style.left = Math.round(Math.max(8, Math.min(r.right - m.offsetWidth > r.left ? r.left : (anchor.classList.contains("ex-ddf") ? r.left : r.right - m.offsetWidth), innerWidth - m.offsetWidth - 8))) + "px";
    m.style.top = Math.round(below ? r.bottom + 6 : r.top - 6 - m.offsetHeight) + "px";
    if (!below) m.classList.add("up");
    anchor.classList.add("open"); m._a = anchor;
    m.addEventListener("click", e => { const it = e.target.closest(".ex-di"); if (!it || it.getAttribute("aria-disabled") === "true") return; closeDD(); onPick(it.dataset.v); });
  }
  function closeDD() { if (!dd) return; const m = dd; dd = null; m._a?.classList.remove("open"); m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90); }
  // 自己画的下拉：40 高、圆角 10，同输入框的底和边，右边一个 ▾
  const ddField = (key, text, label) => `<button type="button" class="ex-ddf" data-ex-dd="${key}" aria-haspopup="menu" aria-label="${esc(label)}"><span>${esc(text)}</span><i data-ic="chevronDown"></i></button>`;
  // ---- 小浮层（手动填 voice_host）：同控制台「Aa」的浮层，外圆角 16、内边距 6，里面 40 高的框圆角 10 ----
  let pop = null;
  function openPop(anchor, html, setup) {
    if (pop && pop._a === anchor) { closePop(); return; }
    closePop(); closeDD();
    const p = pop = Object.assign(document.createElement("div"), { className: "ex-pop enter", innerHTML: html });
    p.setAttribute("role", "dialog");
    document.body.appendChild(hy(p));
    const r = anchor.getBoundingClientRect();
    p.style.left = Math.round(Math.max(8, Math.min(r.right - p.offsetWidth, innerWidth - p.offsetWidth - 8))) + "px";
    p.style.top = Math.round(r.bottom + 6) + "px";
    anchor.classList.add("open"); p._a = anchor;
    p.addEventListener("input", onInput);
    setup?.(p);
  }
  function closePop() { if (!pop) return; const p = pop; pop = null; p._a?.classList.remove("open"); p.classList.remove("enter"); p.classList.add("leave"); setTimeout(() => p.remove(), 90); }
  const closeAll = () => { closeAsk(true); closeDD(); closePop(); };
  document.addEventListener("pointerdown", e => {
    if (dd && !e.target.closest(".ex-dd") && !(dd._a && dd._a.contains(e.target))) closeDD();
    if (pop && !e.target.closest(".ex-pop") && !(pop._a && pop._a.contains(e.target)) && !e.target.closest(".ex-dd")) closePop();
  }, true);
  document.addEventListener("keydown", e => {
    if (e.key !== "Escape") return;
    if (dd) { closeDD(); e.stopPropagation(); return; }
    if (pop) { closePop(); e.stopPropagation(); return; }
    if (mask) { closeAsk(); e.stopPropagation(); }
  }, true);
  addEventListener("scroll", e => { if (dd && !e.target.closest?.(".ex-dd")) closeDD(); }, true);

  // 换分段：同一个面板里换内容，选中块滑过去；左右换，旧的往反方向挪、新的从点的那一侧进来
  function goSeg(r, D, v, view, mountPane) {
    if (D.seg === v) return;
    const s = $(".ex-top .ex-seg", r), keys = $$("span[data-v]", s).map(x => x.dataset.v), dir = keys.indexOf(v) > keys.indexOf(D.seg) ? 1 : -1;
    D.seg = v; pickSeg(s, v); closeAll();
    const pane = $(".ex-pane", r), nv = view(D);
    swapKids(pane, nv.html, dir, "X", () => { pane.className = "ex-pane " + nv.cls; mountPane(r); });
  }
  // 总开关关着（作者 11-28 定）：下面这一段设置整块淡到 0.7（规范第六节「开关关着、内容先给人看的」），
  // 按钮、输入框点不动、键盘也进不去；列表照样能滚、标题照样能悬停看说明，空白处悬停说为什么。
  // 只管「设置」这一段：问答记录、识别测试、伙伴的安装照常能用（不开也要能装模组、能先测日志）
  function paintOff(r, off, tip) {
    const p = $(".ex-pane", r); if (!p) return;
    p.classList.toggle("off", off); setTip(p, off ? tip : "");
    $$("button, input, textarea, [tabindex]", p).forEach(el => { el.inert = off; });
  }

  // ======================================================================
  // ================= 游戏内 AI ==========================================
  // ======================================================================
  // 命令模板：照 internal/chatai/command.go 的 Templates（标题、写法、限制、默认开关都是原话）
  const TPL = [
    { id: "time", title: "修改与查询时间", usage: "time set day|night|noon|midnight；time query daytime|gametime|day", limit: "仅允许上述取值", def: true },
    { id: "weather", title: "修改天气", usage: "weather clear|rain|thunder [秒]", limit: "秒数 ≤ 6000", def: true },
    { id: "tp", title: "传送自己", usage: "tp <自己> <在线玩家>；tp <自己> x y z", limit: "仅可传送自己", def: true },
    { id: "effect", title: "设置自身状态效果", usage: "effect give <自己> <效果> [秒] [等级]；effect clear <自己>", limit: "秒数 ≤ 600，等级 ≤ 4", def: true },
    { id: "spawnpoint", title: "设置出生点", usage: "spawnpoint <自己>", limit: "仅可设置自身出生点，位置为当前所在坐标", def: true },
    { id: "locate", title: "定位结构/群系与查询在线", usage: "locate structure|biome <ID>；list", limit: "只读", def: true },
    { id: "gamemode", title: "切换自身游戏模式", usage: "gamemode survival|creative|adventure|spectator <自己>", limit: "仅可修改自身模式（含创造模式）", def: false },
    { id: "give", title: "给予自身物品", usage: "give <自己> <物品> [数量]", limit: "数量 ≤ 64；模组物品 ID 无法提前校验，由服务端判定", def: false },
    { id: "difficulty", title: "修改游戏难度", usage: "difficulty peaceful|easy|normal|hard", limit: "影响所有玩家", def: false },
    { id: "seed", title: "查看世界种子", usage: "seed", limit: "只读", def: false },
  ];
  // 四项限流：默认值照 internal/chatai/settings.go；档位是草图定的（见「替作者定的」）。0 = 不限：冷却的 0 是「0 秒」在最下面，次数和 token 的「不限」在最上面
  const LIMS = [
    { k: "cooldown", title: "每人冷却", L: [0, 3, 5, 8, 10, 15, 20, 30, 60, 120], def: 8, tip: "同一玩家两次提问的最小间隔（游戏内 AI 与 AI 伙伴共用），默认 8 秒" },
    { k: "perPlayer", title: "每人每天", L: [10, 20, 30, 60, 100, 200, 500, Infinity], def: 60, tip: "每位玩家每日最多提问次数（使用「@伙伴名」呼叫 AI 伙伴单独计次，上限相同），默认 60 次" },
    { k: "perServer", title: "全服每天", L: [50, 100, 200, 300, 500, 1000, 2000, Infinity], def: 300, tip: "本服务器所有玩家每日合计最多提问次数（含游戏内 AI 与 AI 伙伴），默认 300 次" },
    { k: "budget", title: "全服每天 token", L: [5, 10, 20, 50, 100, 200, 500, Infinity].map(x => x * 1e4), def: 5e5, tip: "本服务器每日 token 消耗上限（含输入与输出），默认 50 万" },
  ];
  const limNum = v => v === 0 ? Infinity : v; // 存的 0 是「不限」
  const limText = (l, v) => l.k === "cooldown" ? `${v} 秒` : v === 0 ? "不限" : l.k === "budget" ? fmtTokens(v) : `${v} 次`;
  const limEnds = (l, v) => { const n = l.k === "cooldown" ? v : limNum(v); return [ladder(l.L, n, -1) === n, ladder(l.L, n, 1) === n]; };
  const PRICE = 0.28; // 草图编的单价：每 100 万 token 0.28 美元（程序按 LiteLLM 的价格文件估，查不到时不写）
  const money = n => "$" + (n > 0 && n < 0.01 ? n.toFixed(4) : n.toFixed(2));
  const LIM_TIP = "设为「不限」即不设上限。不调用 AI 的回复不计次（/帮助、/额度、选项错误）。\n同一玩家 1 分钟内提问超过 5 次后将静默 3 分钟，并在游戏内私聊提示剩余冷却时间（冷却中或超额拦截不重复计次）。\n同时仅处理 1 个提问，最多排队 3 个。";
  const AUTH_TIP = "仅已授权玩家可让 AI 执行右侧已启用的命令，其他玩家仅可提问。\n在线玩家、白名单成员、管理员及最近来过的玩家会自动列出；未进过服的玩家可在下方按名称授权。\n注意：离线模式下按名称授权存在冒名风险。";
  const TPL_TIP = "AI 仅可执行此处已启用的命令类型，所有参数均按模板校验，其中「自己」会自动替换为提问玩家。\nop、deop、ban、stop、execute 等高危命令永久禁止，无法启用。";
  const LOG_TIP = "游戏内单次回复最多拆分 3 条消息，超出部分会截断；此处保留完整记录。\n仅保留最近 100 条，保存在该服务器文件夹中。";
  const TEST_TIP = "粘贴一行服务器日志，测试 MineDeck 对玩家名、正文、选项的解析结果及处理方式。\n测试将采用「设置」中的前缀、别名及下方高级识别规则（含未保存修改），不会发送任何请求。";
  const SAMPLE = "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <Steve> @AI /私 今晚怎么打末影龙"; // 程序的示例
  const PATTERN_EXAMPLE = "^\\[\\d{2}:\\d{2}:\\d{2} INFO\\]: \\[\\w+\\] (?P<name>\\w+): (?P<msg>.*)$";
  const FORMAT = { forge: "Forge / NeoForge", vanilla: "原版 / Fabric / Quilt", paper: "Paper / Purpur", custom: "自定义正则", "": "无法识别" };
  const KIND = { ask: "提问", help: "帮助", quota: "额度", unknown: "未知选项", nospace: "选项后缺空格", misplaced: "选项位置有误",
    cooldown: "冷却中", limit: "次数或额度已用完", busy: "队列已满", silenced: "刷屏静默", disabled: "AI 未配置", failed: "回复失败" }; // 同 gamechat.js

  // 名单：和「玩家」页（detail-players.js 的 SAMPLE）对得上；「最近来过」是这里加的几个，外加平常在线、现在不在线的人
  const PL = {
    paper: { wlOn: true, wl: ["Notch_CN", "Steve", "Alex", "xiaoming_2008", "LittleFox", "RedstoneGuo", "Wang_Lei", "Kiki", "BlockBuster99", "moonlight_cat", "Dawei", "Chen_Yu", "NightOwl", "Pumpkin_Pie", "xX_DragonSlayerX"],
      ops: ["Notch_CN", "Steve"], seen: ["LittleFox", "RedstoneGuo", "xiaoming_2008"], usual: ["Steve", "Alex", "Notch_CN"], allowed: ["Steve", "Notch_CN"] },
    forge: { wlOn: true, wl: ["Steve", "Alex", "GearHead", "Create_Fan", "BrassTank", "Wang_Lei", "Kiki", "Dawei", "Mech_Lin", "Cogwheel_77", "Andesite_Ann", "Zhou_Mo"],
      ops: ["Steve"], seen: ["GearHead", "Create_Fan"], usual: ["Steve", "Alex"], allowed: ["Steve", "GearHead"] },
    vanilla: { wlOn: false, wl: ["Steve", "Alex", "SnapshotTester"], ops: ["Steve"], seen: ["SnapshotTester", "PixelCat", "Herobrine_fan"], usual: ["Steve"], allowed: ["Steve", "SnapshotTester"] },
  };
  const GA = {};
  function ga() {
    const k = C.kind;
    if (!GA[k]) {
      const s = PL[k] || PL.paper, on = C.online || [], names = [];
      [...on, ...s.wl, ...s.ops, ...s.seen, ...s.allowed].forEach(n => { if (!has(names, n)) names.push(n); });
      const players = names.map(n => ({ name: n, online: has(on, n), wl: has(s.wl, n), op: has(s.ops, n), seen: !has(on, n) && (has(s.seen, n) || has(s.usual, n)) }))
        .sort((a, b) => (b.online - a.online) || a.name.localeCompare(b.name, "en", { sensitivity: "base" }));
      const comp = k === "forge" && MODS();
      const today = { paper: { count: 37, tokens: 124000, top: [["Steve", 12, 0], ["Notch_CN", 9, 0], ["Alex", 8, 0], ["xiaoming_2008", 5, 0], ["LittleFox", 3, 0]] },
        forge: { count: comp ? 25 : 21, tokens: comp ? 81500 : 68000, top: [["Steve", 9, comp ? 3 : 0], ["GearHead", 7, 0], ["Alex", 5, comp ? 1 : 0]] },
        vanilla: { count: 4, tokens: 9600, top: [["Steve", 3, 0], ["SnapshotTester", 1, 0]] } }[k] || { count: 0, tokens: 0, top: [] };
      GA[k] = { on: true, prefix: "@AI", aliases: k === "paper" ? ["@小助手"] : [], players, allowed: [...s.allowed], wlOn: s.wlOn, wlFirst: "",
        tpl: Object.fromEntries(TPL.map(t => [t.id, t.def])), lim: { cooldown: 8, perPlayer: 60, perServer: 300, budget: 500000 }, today,
        quota: { used: 186000, limit: 2000000 }, log: logOf(k), line: "", res: null, adv: { loose: false, thread: false, pattern: "" }, seg: "set" };
    }
    return GA[k];
  }
  // 最近的问答（编的）：谁问、问了什么、AI 答了什么、执行了哪些命令；类型、选项、用量的说法照 gamechat.js
  function logOf(k) {
    const [a, b, c, d, e] = { paper: ["Steve", "Notch_CN", "Alex", "xiaoming_2008", "LittleFox"], forge: ["Steve", "GearHead", "Alex", "Create_Fan", "Kiki"],
      vanilla: ["Steve", "SnapshotTester", "Alex", "Steve", "PixelCat"] }[k] || ["Steve", "Alex", "Alex", "Steve", "Alex"];
    return [
      { t: "21:42", who: a, kind: "ask", opts: ["私"], exec: true, q: "今晚怎么打末影龙", priv: true, tok: 1840, cost: "约 $0.0005",
        a: "先带够方块、弓箭和几瓶缓降药水。到了末地先把黑曜石柱顶上的末影水晶射掉（有铁栏杆罩着的要爬上去敲掉），龙停到中间的基岩上时近战砍它。别盯着末影人看。" },
      { t: "21:38", who: b, kind: "ask", exec: true, q: "天气改成晴天，下雨太吵了", tok: 1262, cost: "约 $0.0004", a: "好了，已经换成晴天。",
        cmds: [{ ok: true, run: "weather clear 6000", out: "Set the weather to clear" }] },
      { t: "21:31", who: c, kind: "ask", q: "给我一把钻石剑", tok: 986, cost: "约 $0.0003",
        a: "你还不能让我执行命令，可以找服主授权。钻石剑自己做也不难：工作台上竖着摆两颗钻石，下面一根木棍。" },
      { t: "21:12", who: a, kind: "ask", exec: true, q: "切成创造模式，我要搭个桥", tok: 1318, cost: "约 $0.0004", a: "「切换自身游戏模式」这类命令服主没有打开，我做不了。",
        cmds: [{ ok: false, run: `gamemode creative ${a}`, why: "「切换自身游戏模式」没有打开" }] },
      { t: "21:05", who: d, kind: "cooldown", reply: "问得太快了，6 秒后再问", priv: true },
      { t: "20:58", who: e, kind: "help", opts: ["帮助"], priv: true,
        reply: "@AI 问题：全服都能看到回答；@AI /私 问题：只回你；@AI /问 问题：只回答、不执行命令；@AI /额度：看今天还能问几次" },
      { t: "11 月 26 日 23:10", who: c, kind: "failed", q: "附近哪里有村庄", err: "AI 服务商没有回应（等了 60 秒）" },
    ];
  }
  const whoAsked = G => { const top = G.today.top.filter(x => x[1] || x[2]); return top.length ? "今日提问统计：" + top.map(([n, c, cp]) => c ? `${n} ${c} 次${cp ? `（AI 伙伴 ${cp} 次）` : ""}` : `${n} AI 伙伴 ${cp} 次`).join("、") : "今日暂无提问记录"; };
  const limTip = (G, l) => l.k !== "budget" ? l.tip : l.tip + (G.lim.budget ? `\n按当前模型单价估算，每日 ${fmtTokens(G.lim.budget)} token 约 ${money(PRICE * G.lim.budget / 1e6)}` : `\n按当前模型单价估算，每 100 万 token 约 ${money(PRICE)}`);
  const QUOTA_TIP = "「设置 → AI」中为游戏内 AI 配置的全局每日额度（所有服务器合计），耗尽后将暂停至次日恢复。\n点击右侧前往「设置 → AI」修改。";
  const swTip = G => `玩家在聊天中使用「${G.prefix} 问题」提问，回复默认全服可见；加 /私 仅回复提问者，加 /帮助 查看全部用法。\n关闭后 MineDeck 不监听聊天内容，也不会自动回复。`;
  const PROVIDER = "DeepSeek（deepseek-v4-flash）"; // 程序预设里 DeepSeek 的默认模型；说法照程序「提问会发给 X（模型）」

  // ---- 画 ----
  function gaHTML(ctx) {
    C = ctx; closeAll();
    const G = ga(), v = gaView(G);
    return `<div class="ex ex-gai"><div class="ex-top">${tgl({ key: "gaOn", on: G.on, label: "游戏内 AI", tip: swTip(G), cls: "big" })}`
      + (AI_ON() ? "" : gateHTML("AI 未配置", "去设置", "aiset", "请先前往「设置 → AI」选择服务商并填写 Key 与模型。未配置时，玩家发送 @AI 仅会收到「主机还没配置 AI」提示。\n缺少：Key、模型"))
      + `<span class="ex-sp"></span>${segHTML("ga", [["set", "设置"], ["log", "问答记录"], ["test", "识别测试"]], G.seg)}</div>`
      + `<div class="ex-pane ${v.cls}">${v.html}</div></div>`;
  }
  // 顶上一排的提示胶囊：32 高、警告色，里面一颗 24 高的小胶囊按钮（同页头常驻提示的做法）
  const gateHTML = (t, btn, act, tip, tone = "warn") => `<span class="ex-gate ${tone}" title="${esc(tip)}"><i data-ic="${tone === "warn" ? "warn" : "info"}"></i><span>${esc(t)}</span>`
    + `<button type="button" class="ex-gbtn" data-ex-act="${act}">${esc(btn)}</button></span>`;
  const gaView = G => G.seg === "log" ? { cls: "one", html: gaLog(G) } : G.seg === "test" ? { cls: "stack", html: gaTest(G) } : { cls: "g3", html: gaSet(G) };
  function gaSet(G) {
    const ask = `<section class="ex-col"><div class="ex-lw ex-fill"><div class="list ex-form">`
      + `<div class="ex-hd"><span class="ex-t">提问</span></div>`
      + `<div class="ex-pair">${fld("触发前缀", "聊天消息以此开头时触发（如「@AI 今晚怎么打末影龙」），不区分大小写，兼容全角 ＠。", saveIn({ key: "prefix", val: G.prefix, max: 16, label: "触发前缀" }))}`
      + fld("别名", "与触发前缀等效（如 @小助手），多项以逗号分隔，选填。", saveIn({ key: "alias", val: G.aliases.join("，"), ph: "如 @小助手", max: 200, label: "别名" })) + `</div>`
      + fld("提问会发给", "本次启动后首次有人提问时，会在游戏内公告所使用的 AI 服务商与模型。\n点击前往「设置 → AI」切换服务商与模型。",
        `<button type="button" class="ex-box" data-ex-act="aiset"><span class="${AI_ON() ? "" : "faint"}">${AI_ON() ? PROVIDER : "未配置"}</span><i data-ic="chevronRight"></i></button>`)
      + `<div class="ex-hd ex-hd2"><span class="ex-t" title="${esc(LIM_TIP)}">限流和额度</span></div>`
      + LIMS.map(l => limRow(G, l)).join("")
      + `<div class="ex-lim"><span class="ex-ll"><b title="${esc(QUOTA_TIP)}">每日总额度</b><small>所有服务器合计</small></span>`
      + `<span class="ex-val">${fmtTokens(G.quota.used)} / ${fmtTokens(G.quota.limit)}</span><button type="button" class="ex-ib" data-ex-act="aiset" title="前往「设置 → AI」修改"><i data-ic="chevronRight"></i></button></div>`
      + `</div><span class="thumb"></span></div></section>`;
    const auth = `<section class="ex-col" data-ex-col="auth"><div class="ex-hd"><span class="ex-t" title="${esc(AUTH_TIP)}">能执行命令的玩家</span><span class="ex-n" data-k="nAllow">${G.allowed.length || ""}</span></div>`
      + `<div class="ex-wslot" data-k="wl">${wlWarn(G)}</div>`
      + `<div class="ex-lw" data-lw="auth"><div class="list">${G.players.map(p => authRow(G, p)).join("")}</div><span class="thumb"></span><div class="ex-none">暂无可授权的玩家</div></div>`
      + addBox("allow", "玩家名，如 Steve", "授权", 16, "按名字授权") + `</section>`;
    const tpl = `<section class="ex-col" data-ex-col="tpl"><div class="ex-hd"><span class="ex-t" title="${esc(TPL_TIP)}">命令模板</span><span class="ex-n" data-k="nTpl">${nTpl(G)}</span></div>`
      + `<div class="ex-lw" data-lw="tpl"><div class="list">${TPL.map(t => tplRow(G, t)).join("")}</div><span class="thumb"></span></div></section>`;
    return ask + auth + tpl;
  }
  function limRow(G, l) {
    const v = G.lim[l.k], [lo, hi] = limEnds(l, v);
    const today = l.k === "perServer" ? `<small title="${esc(whoAsked(G))}">今天 ${G.today.count} 次</small>` : l.k === "budget" ? `<small>今天 ${fmtTokens(G.today.tokens)}</small>` : "";
    return `<div class="ex-lim" data-lim="${l.k}"><span class="ex-ll"><b title="${esc(limTip(G, l))}">${l.title}</b>${today}</span>${stpHTML("lim-" + l.k, limText(l, v), lo, hi)}</div>`;
  }
  const nTpl = G => `${TPL.filter(t => G.tpl[t.id]).length} / ${TPL.length}`;
  const tplRow = (G, t) => `<button type="button" class="ex-tg ex-tr${G.tpl[t.id] ? " on" : ""}" role="switch" aria-checked="${!!G.tpl[t.id]}" data-ex-tg="tpl" data-id="${t.id}" `
    + `title="${esc(`${t.usage}\n限制：${t.limit}${t.def ? "" : "\n默认关闭"}`)}"><span class="ex-sw"></span><span class="ex-tx"><b>${esc(t.title)}</b><span>${esc(t.usage)}</span></span></button>`;
  function authRow(G, p) {
    const on = has(G.allowed, p.name);
    return `<div class="ex-pr" data-name="${esc(p.name)}">${tgl({ key: "allow", on, label: `<span class="ex-nm">${esc(p.name)}</span>`, tip: `允许 ${p.name} 让 AI 执行命令`, cls: "ex-prt" })}`
      + (p.online ? `<i class="ex-dotok" title="在线"></i>` : "") + (p.op ? `<span class="ex-op" title="管理员">${ico("exCrown")}</span>` : "")
      + `<span class="ex-sp"></span><span class="ex-tags">${p.wl ? tag("白名单") : ""}${!p.online && p.seen ? tag("最近来过", "faint") : ""}</span></div>`;
  }
  // 授了权却没开白名单：栏顶一条提醒（会改变结果：离线服谁都能冒名让 AI 执行命令），带「开启白名单」
  const wlTip = G => `离线模式下，任何人使用 ${has(G.allowed, G.wlFirst) ? G.wlFirst : G.allowed[0]} 的名称进服均可让 AI 执行已启用的命令。开启白名单可拦截未知玩家，不开启亦可正常授权。`;
  function wlWarn(G) {
    if (G.wlOn || !G.allowed.length) return "";
    return `<div class="ex-warn" title="${esc(wlTip(G))}">`
      + `<i data-ic="warn"></i><span>未开启白名单</span><button type="button" class="ex-gbtn" data-ex-act="wlOn">开启白名单</button></div>`;
  }
  function gaLog(G) {
    return `<section class="ex-col ex-logc"><div class="ex-hd"><span class="ex-t" title="${esc(LOG_TIP)}">问答与执行记录</span><span class="ex-n">${G.log.length || ""}</span></div>`
      + `<div class="ex-lw${G.log.length ? "" : " is-none"}" data-lw="log"><div class="list ex-recs">${G.log.map(recHTML).join("")}</div><span class="thumb"></span>`
      + `<div class="ex-none" title="${esc(`开启游戏内 AI 后，在游戏聊天中发送「${G.prefix} 你好」即可触发。`)}">暂无问答记录</div></div></section>`;
  }
  function recHTML(r) {
    const tone = r.kind === "failed" || r.kind === "silenced" ? "warn" : r.kind === "ask" ? "acc" : "";
    let h = `<div class="ex-rec"><div class="ex-rh"><b class="ex-who">${esc(r.who)}</b>${tag(KIND[r.kind] || r.kind, tone)}${(r.opts || []).map(o => tag("/" + o, "mono")).join("")}`
      + `${r.exec ? tag("可执行命令") : ""}<span class="ex-sp"></span>${r.tok ? `<span class="ex-use">消耗 ${fmtTokens(r.tok)} token${r.cost ? " · " + r.cost : ""}</span>` : ""}<span class="ex-when">${esc(r.t)}</span></div>`;
    if (r.q) h += `<p><b>问</b><span>${esc(r.q)}</span></p>`;
    const rep = r.a || r.reply;
    if (rep) h += `<p><b>${r.kind === "ask" ? "答" : "回"}</b><span>${esc(rep)}${r.priv ? `<em>（仅私聊 ${esc(r.who)}）</em>` : ""}</span></p>`;
    (r.cmds || []).forEach(c => {
      h += `<p class="ex-cmd ${c.ok ? "ok" : "bad"}">${ico(c.ok ? "check" : "x")}<code>/${esc(c.run)}</code>${c.ok ? "" : `<span>未执行：${esc(c.why)}</span>`}</p>`;
      if (c.ok && c.out) h += `<pre class="ex-cout">${esc(c.out)}</pre>`;
    });
    if (r.err) h += `<p class="ex-err">${ico("warn")}<span>${esc(r.err)}</span></p>`;
    return h + `</div>`;
  }
  function gaTest(G) {
    return `<section class="ex-col ex-testc"><div class="ex-hd"><span class="ex-t" title="${esc(TEST_TIP)}">识别测试</span></div>`
      + `<div class="ex-trow"><label class="ex-in mono" data-ex-in="line"><input value="${esc(G.line)}" placeholder="${esc(SAMPLE)}" spellcheck="false" autocomplete="off" aria-label="一行服务器日志"><span class="ex-msg"></span></label>`
      + `<button type="button" class="ex-btn big" data-ex-act="sample" title="填入一行 Forge 格式的玩家聊天日志">填入示例</button><button type="button" class="ex-btn big pri" data-ex-act="run"><i data-ic="search"></i>识别</button></div>`
      + `<div class="ex-lw" data-lw="res"><div class="list ex-res" data-k="res">${G.res ? resHTML(G.res) : ""}</div><span class="thumb"></span></div></section>`
      // 高级识别：两个开关放在栏的标题行右边（同「玩家」页白名单的开关在栏头），下面自定义正则
      + `<section class="ex-col ex-advc"><div class="ex-hd"><span class="ex-t" title="无法识别聊天日志时可调整以下选项；修改后上方已测试的日志会立即按新规则重新识别">高级识别</span><span class="ex-sp"></span>`
      + `<span class="ex-advrow">${tgl({ key: "loose", on: G.adv.loose, label: "放宽名字规则", tip: "默认仅识别 1～16 位字母、数字及下划线；离线服玩家名含 - 或 . 时可开启此项。\n以 @ 开头、含 \" 或 \\、或包含中文的名称仍不识别，以防命令注入风险。" })}`
      + tgl({ key: "thread", on: G.adv.thread, label: "忽略线程名", tip: "默认仅识别服务器主线程输出的聊天日志；若模组修改了线程名导致无法识别，可开启此项（仍需由 MinecraftServer 输出）。" }) + `</span></div>`
      + fld("自定义正则", "适用于安装了聊天格式插件或模组的服务器。请使用 (?P<name>…) 捕获玩家名、(?P<msg>…) 捕获聊天内容，按整行匹配；留空则使用内置规则。\n修改前建议先在上方识别测试中验证。",
        saveIn({ key: "pattern", val: G.adv.pattern, ph: "如 " + PATTERN_EXAMPLE, mono: true, max: 500, label: "自定义正则" }))
      + `</section>`;
  }
  function resHTML(r) {
    const kv = [["日志格式", esc(FORMAT[r.format] ?? r.format)]];
    if (r.thread) kv.push(["线程", esc(r.thread)]);
    if (r.chat) {
      kv.push(["玩家", `<span class="ex-mono">${esc(r.player)}</span>${tag(r.online ? "在线" : "不在线", r.online ? "ok" : "")}${r.authorized ? tag("能执行命令") : ""}`]);
      kv.push(["正文", esc(r.message)]);
    }
    if (r.ask) {
      kv.push(["前缀", `<span class="ex-mono">${esc(r.ask.prefix)}</span>`]);
      kv.push(["选项", r.ask.opts.length ? `<span class="ex-mono">${esc(r.ask.opts.map(o => "/" + o).join(" "))}</span>` : "无"]);
      if (r.ask.question) kv.push(["问题", esc(r.ask.question)]);
    }
    return `<dl class="ex-kv">${kv.map(([k, v]) => `<dt>${k}</dt><dd>${v}</dd>`).join("")}</dl>`
      + `<div class="ex-verdict ${r.ask ? "hit" : "miss"}">${ico(r.ask ? "check" : "info")}<span>${esc(r.result)}</span></div>`;
  }

  // ---- 识别：照 internal/chatai 的 recognize.go、ask.go、view.go 的 describe（原话），草图里用 JS 认，不发请求 ----
  const NAME_OK = /^[A-Za-z0-9_]{1,16}$/;
  function parseLine(line) {
    let m;
    if (/^\s*(\[MineDeck\]|>)/.test(line)) return { md: true };
    if ((m = line.match(/^\[(\d\d:\d\d:\d\d)\] \[([^\]]+)\/([A-Z]+)\] \[([^\]]+)\]: (.*)$/))) return { format: "forge", thread: m[2], level: m[3], logger: m[4], body: m[5] };
    if ((m = line.match(/^\[(\d\d:\d\d:\d\d)\] \[([^\]]+)\/([A-Z]+)\]: (.*)$/))) return { format: "vanilla", thread: m[2], level: m[3], body: m[4] };
    if ((m = line.match(/^\[(\d\d:\d\d:\d\d) ([A-Z]+)\]: (.*)$/))) return { format: "paper", thread: "", level: m[2], body: m[3] };
    return { format: "", body: line };
  }
  function validSender(n, loose) {
    if (!loose) return NAME_OK.test(n) ? "" : "仅支持 1～16 位字母、数字及下划线；若离线服玩家名包含其他字符，可开启「放宽名字规则」";
    if (![...n].length || [...n].length > 16) return "玩家名长度须为 1～16 个字符";
    if (n.startsWith("@")) return "不识别以 @ 开头的名称（写入命令会被解析为目标选择器）";
    if (/["\\]/.test(n)) return "不识别包含 \" 或 \\ 的名称（会破坏命令格式）";
    if (/[^\x21-\x7e]|[<>]/.test(n)) return "名称包含中文、空格或其他非 ASCII 字符，无法安全写入命令，不予识别";
    return "";
  }
  function parseAsk(msg, prefixes) {
    const t = msg.trim(), norm = s => s.replace(/^＠/, "@").toLowerCase();
    const p = prefixes.find(x => { const a = norm(t), b = norm(x); return a.startsWith(b) && (a.length === b.length || /\s/.test(a[b.length])); });
    if (!p) return null;
    let rest = t.slice(p.length).trim(), act = "ask", unknown = "";
    const opts = [], KNOWN = ["帮助", "额度", "私", "问"];
    while (/^[/／]/.test(rest)) {
      const tok = rest.slice(1).split(/\s+/)[0];
      if (KNOWN.includes(tok)) { opts.push(tok); rest = rest.slice(1 + tok.length).trim(); continue; }
      if (KNOWN.some(n => tok.startsWith(n))) act = "nospace"; else { act = "unknown"; unknown = tok; }
      break;
    }
    if (act === "ask") {
      if (/\s[/／](私|帮助|额度|问)$/.test(rest)) act = "misplaced";
      else if (opts.includes("帮助") || (!rest && !opts.includes("额度"))) act = "help";
      else if (opts.includes("额度")) act = "quota";
    }
    return { prefix: t.slice(0, p.length), opts, act, unknown, question: act === "ask" ? rest : "", priv: opts.includes("私"), noExec: opts.includes("问") };
  }
  function recognize(line, G) {
    const prefixes = [G.prefix, ...G.aliases], l = parseLine(line), h = { format: l.format || "", thread: l.thread || "" };
    if (l.md) { h.reason = "MineDeck 写入控制台的系统输出（命令回显、提示），已忽略"; return h; }
    let player, msg;
    if (G.adv.pattern) {
      let m = null; try { m = line.match(new RegExp(G.adv.pattern.replace(/\(\?P</g, "(?<"))); } catch { /* 保存时已经查过 */ }
      h.format = "custom"; h.thread = "";
      if (!m || !m.groups) { h.reason = "自定义正则未匹配该行日志"; return h; }
      player = m.groups.name; msg = m.groups.msg;
    } else {
      const cm = (l.body || "").match(/^(?:\[Not Secure\] )?<([^>]*)> (.*)$/);
      let why = "";
      if (!l.format) why = "无法识别该行日志格式（不匹配 Forge / NeoForge、原版 / Fabric 或 Paper 格式）；若服务器安装了聊天格式插件或模组，请配置自定义正则";
      else if (l.level !== "INFO") why = "非 INFO 级别日志，不属于玩家聊天";
      else if (l.format !== "paper" && !G.adv.thread && l.thread !== "Server thread" && !l.thread.startsWith("Async Chat Thread - #")) why = `线程「${l.thread}」不属于聊天记录线程；若线程名不匹配，可开启「忽略线程名」`;
      else if (l.format === "forge" && l.logger !== "minecraft/MinecraftServer" && l.logger !== "minecraft/DedicatedServer") why = `记录器「${l.logger}」并非用于记录聊天的 MinecraftServer`;
      else if (!cm) why = "不属于玩家聊天（标准格式为 <玩家名> 内容）";
      if (why) { h.reason = why; return h; }
      player = cm[1]; msg = cm[2];
    }
    h.chat = true; h.player = player; h.message = msg;
    const why = validSender(player, G.adv.loose);
    if (why) { h.reason = why; return h; }
    h.ask = parseAsk(msg, prefixes);
    if (!h.ask) h.reason = `正文未以触发前缀（${prefixes.join("、")}）开头，或前缀后缺少空格`;
    return h;
  }
  function describe(t, G) {
    if (!t.ask) return t.chat ? `${t.player} 的聊天，未触发：${t.reason}` : "未触发：" + t.reason;
    const a = t.ask, who = t.player;
    let out;
    switch (a.act) {
      case "help": out = `私聊发送帮助给 ${who}，不调用 AI`; break;
      case "quota": out = `私聊发送 ${who} 今日剩余提问次数，不调用 AI`; break;
      case "unknown": out = `未知选项 /${a.unknown}：私聊提示 ${who}，不调用 AI`; break;
      case "nospace": out = `选项与问题之间缺少空格：私聊提示 ${who}，不调用 AI`; break;
      case "misplaced": out = `选项位于问题之后：私聊提示 ${who}，不调用 AI（避免私聊回复公开发送）`; break;
      default:
        out = `${who} 提问「${a.question}」：回复${a.priv ? "仅私聊 " + who : "全服可见"}`;
        out += t.authorized && a.noExec ? "；已附加 /问，本次不启用命令执行" : t.authorized ? `；${who} 拥有执行权限，可让 AI 执行已启用的命令` : `；${who} 无执行权限，仅回复文本`;
    }
    if (!t.online) out += `。注意：${who} 当前离线，实际运行时不会处理离线玩家的发言`;
    if (!G.on) out += "。游戏内 AI 当前未开启";
    return out;
  }
  function runTest(r, quiet) {
    const G = ga(), box = $("[data-ex-in=line]", r); if (!box) return;
    G.line = $("input", box).value;
    const line = G.line.trim(), out = $("[data-k=res]", r), w = $("[data-lw=res]", r), fit = () => repaint(w);
    if (!line) { if (!quiet) bad(box, "请粘贴一行日志"); if (G.res) { G.res = null; swapIn(out, "", 1, "Y", fit); } return; }
    const h = recognize(line, G);
    h.online = !!h.player && has(C.online || [], h.player); h.authorized = !!h.player && has(G.allowed, h.player); h.result = describe(h, G);
    const first = !G.res; G.res = h;
    if (first) { out.innerHTML = resHTML(h); hy(out); fit(); [...out.children].forEach((c, i) => c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: E_OUT, fill: "backwards" })); }
    else swapIn(out, resHTML(h), 1, "Y", fit);
  }

  // 名字的检查：同 gamechat.js 的 nameErr（短的进框里，原话进悬停）
  const nameErr = n => {
    if (!n) return ["请输入玩家名", "请输入玩家名"];
    if ([...n].length > 16) return ["最多 16 个字符", "玩家名最多 16 个字符"];
    if (n.startsWith("@")) return ["不可 @ 开头", "玩家名不可使用 @ 开头（写入命令会被解析为目标选择器）"];
    if (/["\\]/.test(n)) return ["含非法字符", "玩家名不可包含 \" 或 \\"];
    if (/[^\x21-\x7e]|[<>]/.test(n)) return ["格式无效", "玩家名仅支持英文字母、数字及半角符号（不含中文、空格和 < >）"];
    return null;
  };

  // ---- 挂上去 ----
  function gaMount(ctx) {
    C = ctx;
    const r = rootOf("ex-gai"); if (!r) return;
    placeSeg($(".ex-top .ex-seg", r), true);
    gaPane(r);
    r.addEventListener("click", gaClick); r.addEventListener("keydown", gaKey); r.addEventListener("input", onInput);
  }
  const gaPane = r => { bars(r); fitNames(r); gaOff(r); };
  const gaOff = r => { const G = ga(); paintOff(r, !G.on && G.seg === "set", "游戏内 AI 未开启，开启后方可修改"); };
  const paintCount = (r, k, text) => { const el = $(`[data-k=${k}]`, r); if (el && el.textContent !== text) swapIn(el, esc(text), 1); };
  function gaClick(e) {
    if (e.target.closest(".ex-pane.off")) return; // 总开关关着：设置这一段什么都不接（按钮已 inert，这里兜底）
    const r = e.currentTarget, G = ga();
    const sg = e.target.closest(".ex-top .ex-seg span[data-v]"); if (sg) { goSeg(r, G, sg.dataset.v, gaView, gaPane); return; }
    const t = e.target.closest("[data-ex-tg]");
    if (t) {
      if (t.getAttribute("aria-disabled") === "true") return;
      const k = t.dataset.exTg;
      if (k === "gaOn") { G.on = !G.on; setTgl(t, G.on); gaOff(r); if (G.res) runTest(r, true); return; }
      if (k === "tpl") { const id = t.dataset.id; G.tpl[id] = !G.tpl[id]; setTgl(t, G.tpl[id]); paintCount(r, "nTpl", nTpl(G)); return; }
      if (k === "allow") { const n = t.closest(".ex-pr").dataset.name; setAllowed(r, n, !has(G.allowed, n)); return; }
      if (k === "loose" || k === "thread") { G.adv[k] = !G.adv[k]; setTgl(t, G.adv[k]); if (G.res) runTest(r, true); return; }
    }
    const st = e.target.closest(".ex-stp button");
    if (st) {
      const k = st.closest(".ex-stp").dataset.exStp.slice(4), l = LIMS.find(x => x.k === k), d = +st.dataset.d;
      const cur = k === "cooldown" ? G.lim[k] : limNum(G.lim[k]), nx = ladder(l.L, cur, d); if (nx === cur) return;
      G.lim[k] = nx === Infinity ? 0 : nx; // 即时生效（程序是改了数字再点保存）
      const [lo, hi] = limEnds(l, G.lim[k]); paintStp(st.closest(".ex-stp"), limText(l, G.lim[k]), lo, hi, d);
      if (k === "budget") setTip($(`[data-lim=budget] .ex-ll b`, r), limTip(G, l));
      return;
    }
    if (e.target.closest(".ex-save")) { e.preventDefault(); commitGa(r, e.target.closest(".ex-in")); return; }
    if (e.target.closest(".ex-plus")) { e.preventDefault(); addAllowed(r, e.target.closest(".ex-in")); return; }
    const a = e.target.closest("[data-ex-act]");
    if (a) {
      const act = a.dataset.exAct;
      if (act === "wlOn") wlDialog(r);
      else if (act === "sample") { const inp = $("[data-ex-in=line] input", r); inp.value = SAMPLE; inp.closest(".ex-in").classList.remove("bad"); inp.focus(); }
      else if (act === "run") runTest(r);
      // aiset：去「设置 → AI」，草图里没有那一页，不跳
    }
  }
  function gaKey(e) {
    if (e.target.closest(".ex-pane.off")) return;
    if (e.key !== "Enter" || e.target.tagName !== "INPUT") return;
    const box = e.target.closest(".ex-in"), r = e.currentTarget; if (!box) return;
    e.preventDefault();
    const k = box.dataset.exIn;
    if (k === "line") runTest(r);
    else if (k === "allow") addAllowed(r, box);
    else if (box.classList.contains("dirty")) commitGa(r, box);
  }
  function commitGa(r, box) {
    const G = ga(), k = box.dataset.exIn, inp = $("input", box), v = inp.value.trim();
    if (k === "prefix") {
      if (!v) return bad(box, "不可为空", "触发前缀不可为空");
      if (/\s/.test(v)) return bad(box, "不可含空格", "触发前缀不可包含空格");
      G.prefix = v; inp.value = v;
      setTip($("[data-ex-tg=gaOn]", r), swTip(G));
    } else if (k === "alias") {
      const list = v.split(/[,，、;；\s]+/).map(x => x.trim()).filter(Boolean);
      if (list.some(x => same(x, G.prefix))) return bad(box, "与前缀重复", `别名「${G.prefix}」与触发前缀重复`);
      G.aliases = list; inp.value = list.join("，");
    } else if (k === "pattern") {
      if (v) {
        let re = null; try { re = new RegExp(v.replace(/\(\?P</g, "(?<")); } catch (err) { return bad(box, "语法有误", "自定义正则语法有误：" + err.message); }
        if (!/\(\?P?<name>/.test(v) || !/\(\?P?<msg>/.test(v) || !re) return bad(box, "缺少命名分组", "自定义正则须包含两个命名分组：(?P<name>…) 表示玩家名，(?P<msg>…) 表示聊天内容");
      }
      G.adv.pattern = v; inp.value = v;
      if (G.res) runTest(r, true);
    }
    savedOK(box);
  }
  // 授权、收回：只换那一行的开关、栏头的人数，没开白名单时栏顶那条提醒跟着出来或收起
  function setAllowed(r, n, on) {
    const G = ga();
    if (on) { if (!has(G.allowed, n)) G.allowed.push(n); G.wlFirst = n; } else G.allowed = G.allowed.filter(x => !same(x, n));
    const row = $$(".ex-pr", r).find(x => same(x.dataset.name, n));
    setTgl($("[data-ex-tg=allow]", row), on);
    paintCount(r, "nAllow", String(G.allowed.length || ""));
    paintWl(r);
    if (G.res && same(G.res.player || "", n)) runTest(r, true);
  }
  function paintWl(r) {
    const G = ga(), slot = $("[data-k=wl]", r); if (!slot) return;
    const want = wlWarn(G), shown = !!slot.firstElementChild && !slot.firstElementChild.classList.contains("ex-out");
    if (!!want === shown) { if (shown) setTip(slot.firstElementChild, wlTip(G)); return; }
    if (want) { slot.innerHTML = want; hy(slot); rowIn(slot.firstElementChild); requestAnimationFrame(() => repaint($("[data-lw=auth]", r))); }
    else rowOut(slot.firstElementChild, () => repaint($("[data-lw=auth]", r)));
  }
  function addAllowed(r, box) {
    const G = ga(), inp = $("input", box), n = inp.value.trim(), err = nameErr(n);
    if (err) return bad(box, err[0], err[1]);
    if (has(G.allowed, n)) { flash($$(".ex-pr", r).find(x => same(x.dataset.name, n))); inp.value = ""; box.classList.remove("has"); return; }
    inp.value = ""; box.classList.remove("has");
    let p = G.players.find(x => same(x.name, n));
    if (!p) { // 列表里没有的人（还没进过服的好友）：加在最后，从下面淡进来
      p = { name: n, online: false, wl: false, op: false, seen: false }; G.players.push(p);
      const list = $("[data-lw=auth] .list", r); list.insertAdjacentHTML("beforeend", authRow(G, p));
      const row = list.lastElementChild; hy(row); fitTitle($(".ex-nm", row)); rowIn(row); noneOf($("[data-lw=auth]", r));
      list.scrollTo({ top: list.scrollHeight, behavior: "smooth" });
      setAllowed(r, n, true);
    } else { setAllowed(r, p.name, true); flash($$(".ex-pr", r).find(x => same(x.dataset.name, p.name))); }
  }
  // 开启白名单：照程序的确认框，勾要先加进白名单的人（在线的、最近来过的、授了权的、还不在名单里的），名字不合法的加不进去
  function wlDialog(r) {
    const G = ga(), cand = G.players.filter(p => !p.wl && (p.online || p.seen || has(G.allowed, p.name)));
    const ok = cand.filter(p => NAME_OK.test(p.name)), badN = cand.filter(p => !NAME_OK.test(p.name)), listed = G.players.filter(p => p.wl).map(p => p.name);
    const body = `<div class="ex-mb">将先把已勾选玩家加入白名单，再开启白名单，避免已有玩家无法进入。</div>`
      + (ok.length ? `<div class="ex-cks">${ok.map(p => `<button type="button" class="ex-ck on" role="checkbox" aria-checked="true" data-n="${esc(p.name)}"><span class="ex-bx">${ico("check")}</span>`
        + `<span class="ex-cn">${esc(p.name)}</span>${p.online ? tag("在线", "ok") : ""}${has(G.allowed, p.name) ? tag("已授权") : ""}${!p.online && p.seen ? tag("最近来过", "faint") : ""}</button>`).join("")}</div>`
        : `<div class="ex-mb faint">所有在线、最近来过及已授权玩家均已在白名单中。</div>`)
      + (badN.length ? `<div class="ex-mb warn">${ico("warn")}<span>${esc(badN.map(p => p.name).join("、"))} 的名称不符合 1～16 位字母、数字及下划线规则，无法加入白名单。</span></div>` : "")
      + (listed.length ? `<div class="ex-mb faint" title="${esc(listed.join("、"))}">已有 ${listed.length} 名玩家在白名单中，无需重复添加</div>` : "");
    ask({ ic: "exShield", tone: "acc", title: "开启白名单", body, ok: "加入白名单并开启", w: 420,
      okTip: running() ? "服务器运行中：将发送 whitelist add 与 whitelist on 命令" : "服务器已停止：将直接修改白名单文件并将 white-list 设为 true",
      setup: mk => mk.addEventListener("click", e => { const c = e.target.closest(".ex-ck"); if (!c) return; const on = !c.classList.contains("on"); c.classList.toggle("on", on); c.setAttribute("aria-checked", on); }),
      onOk: mk => {
        const picked = $$(".ex-ck.on", mk).map(c => c.dataset.n);
        G.wlOn = true;
        picked.forEach(n => {
          const p = G.players.find(x => same(x.name, n)); if (!p) return; p.wl = true;
          const row = $$(".ex-pr", r).find(x => same(x.dataset.name, n)), tg = row && $(".ex-tags", row);
          if (tg) { tg.insertAdjacentHTML("afterbegin", tag("白名单")); rowIn(tg.firstElementChild); }
        });
        paintWl(r);
      } });
  }
  window.DETAIL_TABS["ext/gameai"] = { html: gaHTML, mount: gaMount };

  // ======================================================================
  // ================= AI 伙伴 ============================================
  // ======================================================================
  // 三个模组：照 internal/companion/jar/manifest.json 里 Forge 1.20.1 那一份（实测过的版本、要求的范围、文件名、大小都是真的）
  const MODDEF = [
    { id: "minedeck_companion", name: "MineDeck Companion", kind: "companion", want: "0.2.1", file: "minedeck-companion-forge-1.20.1-0.2.1.jar", license: "MIT" },
    { id: "playerengine", name: "PlayerEngine", kind: "dep", want: "1.20.1-1.4.0", range: "[1.20.1-1.4.0,1.20.1-1.5)", file: "playerengine-forge-1.20.1-1.4.0.jar", size: 4488694, license: "LGPL-3.0" },
    { id: "architectury", name: "Architectury API", kind: "dep", want: "9.2.14", range: "[9.2.14,)", file: "architectury-9.2.14-forge.jar", size: 580602, license: "LGPL-3.0" },
  ];
  const MOD_STATE = { ok: ["ok", "已安装"], inrange: ["", "可用（非实测版本）"], outdated: ["warn", "需更新"], missing: ["warn", "未安装"] }; // 同 companion.js
  const DEF_DENY = ["minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:allay"]; // 同 companion.DefaultAttackDeny
  const DIMS = [["minecraft:overworld", "主世界"], ["minecraft:the_nether", "下界"], ["minecraft:the_end", "末地"]];
  const dimName = d => (DIMS.find(x => x[0] === d) || [d, String(d || "").replace(/^minecraft:/, "")])[1];
  const shortID = id => String(id).replace(/^minecraft:/, "");
  const RESULT = { done: "已完成", incomplete: "未达目标", failed: "失败", cancelled: "已停止", superseded: "已替换为新任务", timeout: "已超时" };
  const COMP_STATE = { starting: ["warm", "准备中"], idle: ["ok", "空闲"], busy: ["warm", "执行中"] };
  const taskTitle = t => !t ? "任务" : t.op === "collect" ? `采集 ${shortID(t.item)}${t.count != null ? " ×" + t.count : ""}` : t.op === "follow" ? (t.player ? `跟随 ${t.player}` : "跟随主人") : t.op;
  const mb = n => (n / 1048576).toFixed(1) + " MB";
  const PACK_NOTE = "若服务器使用整合包，好友客户端须安装同一版本的整合包，仅安装上述模组无法进入。"; // 程序原话
  const CP = {};
  function cp() {
    const k = C.kind;
    if (!CP[k]) {
      const ready = MODS(), run = running();
      const evs = ready ? [
        ["21:43", "「小方」开始采集 oak_log ×32"], ["21:40", "「阿铁」已停止跟随 Alex"],
        ["21:31", "「小方」卡住了：要拆或放禁挖区域里的方块，绕不过去", "warn"], ["21:20", "伙伴模组已读取新配置"], ["21:12", "「阿铁」开始跟随 Alex"],
        ["21:05", "已刷出伙伴「阿铁」"], ["20:58", "已连接伙伴模组 0.2.1（PlayerEngine 1.20.1-1.4.0），现有 1 个伙伴"]].map(([t, x, lv]) => ({ t, x, lv: lv || "info" })) : [];
      if (ready && !run) evs.unshift({ t: "21:50", x: "服务器已停止，与伙伴模组的连接已断开（伙伴保留在存档中，下次启动后自动接管）", lv: "info" });
      CP[k] = {
        mods: MODDEF.map(m => ({ ...m, state: ready ? "ok" : "missing", version: ready ? m.want : "" })), restart: false, installing: false,
        on: ready, linking: false, max: 4, deny: [...DEF_DENY], pend: 0,
        zones: ready ? [{ name: "base", dim: "minecraft:overworld", min: [-120, -64, 240], max: [-60, 320, 310] }, { name: "farm", dim: "minecraft:overworld", min: [-40, 60, 180], max: [10, 80, 230] }] : [],
        comps: ready ? [
          { id: 1, name: "小方", owner: "Steve", state: "busy", hp: 20, max: 20, dim: "minecraft:overworld", xyz: [-88, 71, 262], task: { id: 7, op: "collect", item: "minecraft:oak_log", count: 32 }, last: null },
          { id: 2, name: "阿铁", owner: "Alex", state: "idle", hp: 17.5, max: 20, dim: "minecraft:overworld", xyz: [12, 64, 198], task: null, last: { title: "跟随 Alex", result: "cancelled", at: "21:40" } }] : [],
        events: evs, nextId: 3, nextTask: 8, quota: { used: 32000, limit: 500000 },
        seg: ready ? "comp" : "inst",
      };
    }
    return CP[k];
  }
  const needInstall = D => D.mods.some(m => m.state === "missing" || m.state === "outdated");
  const modMissing = D => D.mods[0].state === "missing";
  // 和伙伴模组的连接（companion.js 的 LINK）：草图按开关和服务器的状态推出来
  function linkOf(D) {
    if (!D.on) return ["off", "未开启", "关闭时伙伴模组不开放控制端口，MineDeck 无法指挥伙伴。"];
    if (C.st !== "running") return ["off", "等待启动", C.st === "stopped" ? "服务器启动完成（出现 Done）后伙伴模组将开放端口，MineDeck 会自动连接。" : "服务器正在启动，完成后将自动连接。"];
    if (D.restart) return ["bad", "连接失败", "伙伴模组未开放控制端口：服务器运行期间安装的模组需重启服务器后方可加载。"]; // 草图写的说法，程序这里显示后端的报错
    if (D.linking) return ["warm", "连接中", ""];
    return ["ok", "已连接", "伙伴模组 0.2.1 · PlayerEngine 1.20.1-1.4.0 · 心跳 3 毫秒\n仅接受本机 MineDeck 连接，请勿外传连接令牌。"];
  }
  const isLive = D => linkOf(D)[0] === "ok";
  const cpSwTip = D => modMissing(D) && !D.on ? "请先安装伙伴模组后再开启。" : "伙伴是服务器中由 AI 驱动的类玩家角色：PlayerEngine 提供实体，MineDeck 提供决策。\n玩家可在聊天中使用「@伙伴名 指令」指挥（经游戏内 AI）。关闭后 MineDeck 不再指挥伙伴，伙伴仍保留在存档中。";
  function cpGate(D) {
    if (!AI_ON()) return gateHTML("AI 未配置", "去设置", "aiset", "伙伴需依赖 AI 理解玩家指令，请先前往「设置 → AI」选择服务商并填写 Key 与模型。\n未配置时仍可刷出或移除伙伴，但无法在游戏内通过聊天指挥。");
    if (D.on && !(GA[C.kind] ? GA[C.kind].on : true)) return gateHTML("游戏内 AI 未开启", "去开启", "togai", "玩家在游戏聊天中呼叫伙伴前，须先在「扩展 → 游戏内 AI」中开启该功能。", "info");
    return "";
  }

  function cpHTML(ctx) {
    C = ctx; closeAll();
    const D = cp(), v = cpView(D), [tone, t, tip] = linkOf(D);
    return `<div class="ex ex-cpn"><div class="ex-top">${tgl({ key: "cpOn", on: D.on, label: "AI 伙伴", tip: cpSwTip(D), cls: "big", dis: modMissing(D) && !D.on })}`
      + `<span class="ex-pslot" data-k="link">${pill(tone, t, tip)}</span><span class="ex-gslot" data-k="gate">${cpGate(D)}</span><span class="ex-sp"></span>`
      + segHTML("cp", [["comp", "伙伴"], ["set", "设置"], ["inst", "安装", `<i class="ex-sdot"${needInstall(D) ? "" : " hidden"} title="有待安装或待更新的模组"></i>`]], D.seg) + `</div>`
      + `<div class="ex-pane ${v.cls}">${v.html}</div></div>`;
  }
  const cpView = D => ({ cls: D.seg === "comp" ? "g2 wide" : "g2", html: D.seg === "set" ? cpSet(D) : D.seg === "inst" ? cpInst(D) : cpComps(D) });
  // ---- 伙伴 ----
  function cpComps(D) {
    const live = isLive(D), list = live ? D.comps : [];
    return `<section class="ex-col" data-ex-col="comps"><div class="ex-hd"><span class="ex-t" title="玩家可在游戏聊天中使用「@伙伴名 指令」下达任务。仅在「游戏内 AI」中已授权的玩家可指挥伙伴，任务完成后会私聊通知发起者。">伙伴</span>`
      + `<span class="ex-n" data-k="nComp">${live ? `${list.length} / ${D.max}` : ""}</span></div>`
      + `<div class="ex-lw${list.length ? "" : " is-none"}" data-lw="comps"><div class="list">${list.map(compRow).join("")}</div><span class="thumb"></span>`
      + `<div class="ex-none" data-k="cnone"${live ? "" : ` title="伙伴数据保存在存档中，停止服务器或断开连接均不会使其消失。"`}>${live ? "暂无伙伴" : "连接伙伴模组后将在此显示伙伴列表"}</div></div>`
      + `<div class="ex-acts">${spawnBtn(D)}<button type="button" class="ex-btn big" data-ex-act="stopall"${live && list.some(c => c.task) ? "" : " disabled"}><i data-ic="stop"></i>全部停止</button></div></section>`
      + `<section class="ex-col" data-ex-col="events"><div class="ex-hd"><span class="ex-t" title="本次启动以来伙伴模组上报的事件记录，按时间倒序保留最多 100 条；仅存内存，退出程序后清空。">事件记录</span></div>`
      + `<div class="ex-lw${D.events.length ? "" : " is-none"}" data-lw="events"><div class="list">${D.events.map(evRow).join("")}</div><span class="thumb"></span>`
      + `<div class="ex-none" title="连接伙伴模组、刷出伙伴或任务状态变更时，将在此记录。">暂无事件</div></div></section>`;
  }
  function spawnBtn(D) {
    const live = isLive(D), full = D.comps.length >= D.max;
    const dis = !live ? "请先启动服务器、开启 AI 伙伴并等待连接模组。" : full ? `已达伙伴上限（${D.max} 个），请先移除现有伙伴或在「设置」中调高上限。` : "";
    return `<button type="button" class="ex-btn big pri" data-ex-act="spawn"${dis ? ` aria-disabled="true" title="${esc(dis)}"` : ` title="伙伴不占用在线玩家名额，亦不显示在 Tab 列表中。"`}><i data-ic="plus"></i>刷出伙伴</button>`;
  }
  const num1 = x => Number.isInteger(+x) ? String(+x) : (+x).toFixed(1);
  function compRow(c) {
    const [tone, st] = COMP_STATE[c.state] || COMP_STATE.busy;
    const line3 = c.task ? `<span>正在${esc(taskTitle(c.task))}</span><em>（任务 ${c.task.id}）</em>`
      : c.last ? `<em>上次：${esc(c.last.title)} ${esc(RESULT[c.last.result] || RESULT.failed)} · ${esc(c.last.at)}</em>` : `<em>尚无任务记录</em>`;
    return `<div class="ex-cr" data-id="${c.id}"><span class="ex-av"><i data-ic="exBot"></i></span><div class="ex-cm">`
      + `<div class="ex-c1"><b class="ex-nm">${esc(c.name)}</b><span data-k="cst">${pill(tone, st)}</span>${c.owner ? `<span class="ex-own">主人 ${esc(c.owner)}</span>` : ""}</div>`
      + `<div class="ex-c2">${ico("exHeart")}<span>${num1(c.hp)}/${num1(c.max)}</span><span>·</span><span>${esc(dimName(c.dim))}</span><span class="ex-mono">${c.xyz.join(", ")}</span></div>`
      + `<div class="ex-c3" data-k="ctask">${line3}</div></div>`
      + `<div class="ex-ca"><span data-k="cstop">${c.task ? `<button type="button" class="ex-btn" data-ex-act="stop">${ico("stop")}停止</button>` : ""}</span>`
      + `<button type="button" class="ex-ib" data-ex-act="despawn" title="移除伙伴"><i data-ic="trash"></i></button></div></div>`;
  }
  const evRow = e => `<div class="ex-ev${e.lv === "warn" ? " warn" : ""}">${ico(e.lv === "warn" ? "warn" : "info")}<span class="ex-evt">${esc(e.x)}</span><span class="ex-when">${esc(e.t)}</span></div>`;
  // ---- 设置 ----
  function cpSet(D) {
    return `<section class="ex-col" data-ex-col="cset"><div class="ex-hd"><span class="ex-t" title="修改后约 2 秒内自动生效，无需重启服务器。">设置</span>`
      + `<span class="ex-pend" data-k="pend" hidden title="正在同步配置至伙伴模组（每 2 秒读取一次）">${ico("spinner")}</span></div>`
      + `<div class="ex-lim"><span class="ex-ll"><b title="同时存在的最大伙伴数量（1～16）。调低上限不会移除现有伙伴，仅限制新增。">伙伴上限</b></span>${stpHTML("max", `${D.max} 个`, D.max <= 1, D.max >= 16)}</div>`
      + `<div class="ex-fld ex-denyf"><span class="ex-lb"><span title="执行攻击（attack）任务时跳过以下生物 ID。\n玩家、伙伴及已驯服、已命名或拴绳的生物默认受保护，不受此列表影响。">禁止攻击的生物</span>`
      + `<button type="button" class="ex-ib sm" data-ex-act="denyDef" title="恢复默认：不打村民、流浪商人、铁傀儡、雪傀儡、悦灵"><i data-ic="restore"></i></button></span>`
      + `<div class="ex-lw ex-chipw" data-lw="deny"><div class="list ex-chips" data-k="deny">${D.deny.map(denyChip).join("")}</div><span class="thumb"></span><div class="ex-none" title="清空表示无额外限制">暂无额外限制</div></div>`
      + addBox("deny", "生物 ID，如 minecraft:villager", "添加", 100, "添加禁止攻击的生物") + `</div>`
      + `<div class="ex-lim"><span class="ex-ll"><b title="玩家呼叫 AI 伙伴时消耗的额度统计；任务完成通知不调用 AI，不计入 token。\n点击右侧前往「设置 → AI」修改。">AI 伙伴每日额度</b><small>今日已用 / 每日上限</small></span>`
      + `<span class="ex-val">${fmtTokens(D.quota.used)} / ${D.quota.limit ? fmtTokens(D.quota.limit) : "不限"}</span><button type="button" class="ex-ib" data-ex-act="aiset" title="前往「设置 → AI」修改"><i data-ic="chevronRight"></i></button></div></section>`
      + `<section class="ex-col" data-ex-col="zones"><div class="ex-hd"><span class="ex-t" title="伙伴不会破坏或放置该区域内的方块（倒水、点火、锄地同样不做）；伙伴可穿行，受阻时将报告「卡住了」。\n采用方块坐标（含端点），两角顺序不限。">禁挖区域</span>`
      + `<span class="ex-n" data-k="nZone">${D.zones.length || ""}</span><button type="button" class="ex-ib ex-corner" data-ex-act="zoneAdd" title="添加区域（最多 64 个）"><i data-ic="plus"></i></button></div>`
      + `<div class="ex-lw${D.zones.length ? "" : " is-none"}" data-lw="zones"><div class="list">${D.zones.map(zoneRow).join("")}</div><span class="thumb"></span>`
      + `<div class="ex-none" title="可将基地、农场等需保护区域设为禁挖；游戏内按 F3 可查看当前位置坐标。">暂无禁挖区域</div></div></section>`;
  }
  const denyChip = id => `<span class="ex-chip" data-id="${esc(id)}" title="${esc(id)}">${esc(shortID(id))}<button type="button" class="ex-cx" data-ex-act="denyRm" aria-label="移除 ${esc(id)}">${ico("x")}</button></span>`;
  const zoneRow = z => `<div class="ex-zr" data-name="${esc(z.name)}" role="button" tabindex="0" title="点击编辑"><span class="ex-av sm"><i data-ic="exPick"></i></span><div class="ex-cm">`
    + `<div class="ex-c1"><b class="ex-mono">${esc(z.name)}</b><span class="ex-dim">${esc(dimName(z.dim))}</span></div><div class="ex-c2 ex-mono">(${z.min.join(", ")}) → (${z.max.join(", ")})</div></div>`
    + `<button type="button" class="ex-ib ex-rx" data-ex-act="zoneDel" title="删除区域"><i data-ic="trash"></i></button></div>`;
  // ---- 安装 ----
  function cpInst(D) {
    return `<section class="ex-col" data-ex-col="inst"><div class="ex-hd"><span class="ex-t" title="服务端需安装以下三个模组，其中 MineDeck Companion 由 MineDeck 内置。\nPlayerEngine 与 Architectury API 需点击「安装」从 Modrinth 下载已验证版本，不会自动升级。">安装</span></div>`
      + `<div class="ex-mods">${D.mods.map(modRow).join("")}</div><span class="ex-grow"></span><div class="ex-strip-slot" data-k="strip">${stripHTML(D)}</div></section>`
      + `<section class="ex-col" data-ex-col="client"><div class="ex-hd"><span class="ex-t" title="AI 伙伴包含模组新增实体，好友客户端须安装标注「必需」的同版本模组至 mods 文件夹。">好友的客户端</span>`
      + `<button type="button" class="ex-ib ex-corner" data-ex-act="copyGuide" title="${esc("复制给好友的说明：\n" + guideCP())}"><i data-ic="copy"></i></button></div>`
      + `<div class="ex-mods">${clientRows()}</div><span class="ex-grow"></span><div class="ex-note">${ico("info")}<span>${PACK_NOTE}</span></div></section>`;
  }
  function modRow(m) {
    const [tone, label] = MOD_STATE[m.state] || MOD_STATE.missing;
    const line = m.version ? `已安装 ${m.version}` : m.kind === "companion" ? `MineDeck 内置 ${m.want}` : `验证版本 ${m.want}`;
    const tip = [m.file, m.range ? `要求 ${m.range}` : "MineDeck 内置，无需联网下载", m.license].filter(Boolean).join("\n");
    return `<div class="ex-mr" data-mod="${m.id}" title="${esc(tip)}"><span class="ex-av"><i data-ic="${m.kind === "companion" ? "exBot" : "package"}"></i></span><div class="ex-cm">`
      + `<div class="ex-c1"><b>${esc(m.name)}</b><span data-k="mst">${tag(label, tone)}</span></div><div class="ex-c2 ex-mono" data-k="mver">${esc(line)}</div></div>`
      + (m.kind === "dep" ? `<button type="button" class="ex-ib" data-ex-act="ext" title="Modrinth 上的 ${esc(m.name)}"><i data-ic="share"></i></button>` : "") + `</div>`;
  }
  // 现在该做什么（companion.js 的 drawInstall 最后那一行）：一条 52 高、圆角 10（离栏边 8，18 − 8）
  function stripHTML(D) {
    const dl = D.mods.filter(m => m.kind === "dep" && m.state !== "ok" && m.state !== "inrange").reduce((s, m) => s + m.size, 0);
    const replace = D.mods.some(m => m.state === "outdated");
    if (D.installing) return `<div class="ex-strip"><span class="ex-si spin">${ico("spinner")}</span><span class="ex-stx"><b>正在安装…</b><small>${dl ? `正在从 Modrinth 下载约 ${mb(dl)}，请稍候` : "正在写入 mods 文件夹"}</small></span></div>`;
    if (!needInstall(D)) return D.restart
      ? `<div class="ex-strip warn" title="服务器运行期间安装的模组，需重启服务器后加载"><span class="ex-si">${ico("warn")}</span><span class="ex-stx"><b>安装完成，重启服务器后生效</b></span></div>`
      : `<div class="ex-strip ok" title="后续 MineDeck 更新伙伴模组时，此处会提示「需更新」"><span class="ex-si">${ico("check")}</span><span class="ex-stx"><b>所需模组均已安装</b></span></div>`;
    if (C.st !== "stopped" && replace) return `<div class="ex-strip warn"><span class="ex-si">${ico("warn")}</span><span class="ex-stx"><b>请先停止服务器再更新</b><small>服务器运行中无法替换已占用的旧版文件</small></span>`
      + `<button type="button" class="ex-btn big pri" aria-disabled="true" title="服务器运行中无法替换已占用的旧版文件，请先停止服务器。">${ico("download")}更新</button></div>`;
    return `<div class="ex-strip"><span class="ex-si">${ico("download")}</span><span class="ex-stx"><b>安装缺少的模组</b><small>${dl ? `需从 Modrinth 下载约 ${mb(dl)}` : "无需下载"}</small></span>`
      + `<button type="button" class="ex-btn big pri" data-ex-act="install"${C.st !== "stopped" ? ` title="服务器运行中，安装完成后需重启服务器生效。"` : ""}>${ico("download")}${replace ? "更新" : "安装"}</button></div>`;
  }
  // 好友的客户端：伙伴模组 client = required 要装（不在 Modrinth 上，从服务器的 mods 里拷），两个依赖 side = server 不用装（同 install.go 的 clientMods）
  function clientRows() {
    return MODDEF.map(m => {
      const need = m.kind === "companion";
      return `<div class="ex-mr sm"><div class="ex-cm"><div class="ex-c1"><b>${esc(m.name)}</b><span class="ex-mono ex-ver">${esc(m.want)}</span>${tag(need ? "必需" : "无需安装", need ? "acc" : "")}</div></div>`
        + (need ? `<button type="button" class="ex-ib" data-ex-act="modsDir" title="MineDeck 内置（未上架 Modrinth），请从服务器 mods 文件夹中复制此文件发送给好友\n${esc(m.file)}"><i data-ic="folder"></i></button>`
          : `<button type="button" class="ex-ib" data-ex-act="ext" title="${esc(`${m.license} · 从 Modrinth 下载这个版本`)}"><i data-ic="share"></i></button>`) + `</div>`;
    }).join("");
  }
  const guideCP = () => `服务器「${C.S.name}」开了 MineDeck 的 AI 伙伴，客户端要装这几个模组（版本和服务器的一样），放进 .minecraft 的 mods 文件夹：\n`
    + `1. MineDeck Companion 0.2.1：找房主要文件 minedeck-companion-forge-1.20.1-0.2.1.jar\n如果服务器是整合包：先装同一个整合包（版本要和服务器一致），再把上面的模组放进那个整合包的 mods 文件夹。\n进服后在聊天里说「@伙伴的名字 话」就能叫它。`;

  function cpMount(ctx) {
    C = ctx;
    const r = rootOf("ex-cpn"); if (!r) return;
    placeSeg($(".ex-top .ex-seg", r), true);
    cpPane(r);
    r.addEventListener("click", cpClick); r.addEventListener("keydown", cpKey); r.addEventListener("input", onInput);
  }
  const cpPane = r => { bars(r); fitNames(r); cpOff(r); };
  const cpOff = r => { const D = cp(); paintOff(r, !D.on && D.seg === "set", modMissing(D) ? "需先安装伙伴模组并开启 AI 伙伴，方可修改" : "AI 伙伴未开启，开启后方可修改"); };
  const addEv = (r, x, lv = "info") => {
    const D = cp(), e = { t: nowHM(), x, lv }; D.events.unshift(e);
    const w = $("[data-lw=events]", r); if (!w) return;
    const list = $(".list", w); list.insertAdjacentHTML("afterbegin", evRow(e)); rowIn(list.firstElementChild); list.scrollTo({ top: 0, behavior: "smooth" }); noneOf(w);
  };
  function paintLink(r) {
    const D = cp(), [tone, t, tip] = linkOf(D), slot = $("[data-k=link]", r), cur = $(".ex-pill", slot);
    if (cur && cur.textContent === t) { setTip(cur, tip); return; }
    swapIn(slot, pill(tone, t, tip), 1);
  }
  function paintGate(r) {
    const slot = $("[data-k=gate]", r), html = cpGate(cp());
    if ((slot.textContent || "") === (html ? hy(Object.assign(document.createElement("div"), { innerHTML: html })).textContent : "")) return;
    swapIn(slot, html, 1);
  }
  // 连上、断开时伙伴那一栏整批换（连上：接管世界里的伙伴；断开：列表空了，正中一句淡字）
  function paintComps(r) {
    const D = cp(), w = $("[data-lw=comps]", r);
    const spawn = $("[data-ex-act=spawn]", r), stopAll = $("[data-ex-act=stopall]", r);
    if (spawn) { spawn.outerHTML = spawnBtn(D); hy($(".ex-acts", r)); }
    if (stopAll) stopAll.disabled = !(isLive(D) && D.comps.some(c => c.task));
    paintCount(r, "nComp", isLive(D) ? `${D.comps.length} / ${D.max}` : "");
    if (!w) return;
    const live = isLive(D), list = $(".list", w), none = $("[data-k=cnone]", w);
    none.textContent = live ? "暂无伙伴" : "连接伙伴模组后将在此显示伙伴列表"; setTip(none, live ? "" : "伙伴数据保存在存档中，停止服务器或断开连接均不会使其消失。");
    const want = live ? D.comps : [];
    if (want.length === list.children.length) return;
    if (!want.length) { w.classList.add("is-none"); swapKids(list, "", 1, "Y", () => repaint(w)); }
    else swapKids(list, want.map(compRow).join(""), 1, "Y", () => { noneOf(w); fitNames(w); });
  }
  function cpToggle(r, t) {
    const D = cp();
    D.on = !D.on; setTgl(t, D.on); setTip(t, cpSwTip(D));
    if (D.on && running() && !D.restart) {
      D.linking = true; paintLink(r);
      setTimeout(() => {
        D.linking = false; if (!r.isConnected) return;
        paintLink(r); paintComps(r);
        addEv(r, `已连接伙伴模组 0.2.1（PlayerEngine 1.20.1-1.4.0），现有 ${D.comps.length} 个伙伴`);
      }, 900);
    } else {
      paintLink(r); paintComps(r);
      if (!D.on && running()) addEv(r, "已关闭 AI 伙伴，与伙伴模组的连接已断开（伙伴仍保留在存档中）");
    }
    paintGate(r); cpOff(r);
  }
  function cpClick(e) {
    if (e.target.closest(".ex-pane.off")) return; // 同 gaClick
    const r = e.currentTarget, D = cp();
    const sg = e.target.closest(".ex-top .ex-seg span[data-v]"); if (sg) { goSeg(r, D, sg.dataset.v, cpView, cpPane); return; }
    const t = e.target.closest("[data-ex-tg]");
    if (t && t.dataset.exTg === "cpOn") { if (t.getAttribute("aria-disabled") !== "true") cpToggle(r, t); return; }
    const st = e.target.closest(".ex-stp button");
    if (st) {
      const d = +st.dataset.d, n = Math.max(1, Math.min(16, D.max + d)); if (n === D.max) return;
      D.max = n; paintStp(st.closest(".ex-stp"), `${n} 个`, n <= 1, n >= 16, d); pending(r); return;
    }
    if (e.target.closest(".ex-plus")) { e.preventDefault(); addDeny(r, e.target.closest(".ex-in")); return; }
    const zr = e.target.closest(".ex-zr");
    const a = e.target.closest("[data-ex-act]");
    if (!a) { if (zr) zoneDialog(r, D.zones.find(z => z.name === zr.dataset.name)); return; }
    if (a.getAttribute("aria-disabled") === "true") return; // 灰着的（刷出伙伴、更新）：不用 disabled，悬停才出得来原因
    const act = a.dataset.exAct, cr = a.closest(".ex-cr"), c = cr && D.comps.find(x => x.id === +cr.dataset.id);
    switch (act) {
      case "togai": $('#snav [data-s="gameai"]')?.click(); break; // 去「扩展 → 游戏内 AI」：点外壳左边的子页
      case "spawn": spawnDialog(r); break;
      case "stopall": D.comps.filter(x => x.task).forEach(x => stopComp(r, x)); break;
      case "stop": if (c) stopComp(r, c); break;
      case "despawn": if (c) despawnDialog(r, c); break;
      case "denyRm": { const chip = a.closest(".ex-chip"), id = chip.dataset.id; D.deny = D.deny.filter(x => x !== id); chipOut(chip, () => noneOf($("[data-lw=deny]", r))); pending(r); break; }
      case "denyDef": resetDeny(r); break;
      case "zoneAdd": zoneDialog(r, null); break;
      case "zoneDel": if (zr) zoneDel(r, zr.dataset.name); break;
      case "install": install(r); break;
      case "copyGuide": doneTick(a); break;
      // ext（去 Modrinth）、modsDir（打开 mods 文件夹）、aiset（去「设置 → AI」）：草图里不跳、不开
    }
  }
  function cpKey(e) {
    if (e.target.closest(".ex-pane.off")) return;
    const r = e.currentTarget;
    if (e.key === "Enter" && e.target.tagName === "INPUT" && e.target.closest(".ex-in")?.dataset.exIn === "deny") { e.preventDefault(); addDeny(r, e.target.closest(".ex-in")); return; }
    if ((e.key === "Enter" || e.key === " ") && e.target.classList?.contains("ex-zr")) { e.preventDefault(); zoneDialog(r, cp().zones.find(z => z.name === e.target.dataset.name)); }
  }
  // 设置改了：栏标题旁转 2 秒的小圈（伙伴模组每 2 秒读一次）
  function pending(r) {
    const D = cp(), p = $("[data-k=pend]", r); if (!p) return;
    p.hidden = false; const tok = ++D.pend;
    setTimeout(() => { if (D.pend === tok && p.isConnected) p.hidden = true; }, 2000);
  }
  function chipOut(chip, done) {
    chip.classList.add("ex-out");
    chip.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(16px)" }], { duration: 180, easing: E_IN, fill: "forwards" }).onfinish = () => { chip.remove(); done?.(); };
  }
  const entityOK = id => id.length <= 100 && /^(?:[a-z0-9_.-]+:)?[a-z0-9_./-]+$/.test(id); // 同 companion.js
  function addDeny(r, box) {
    const D = cp(), inp = $("input", box), raw = inp.value.trim(); if (!raw) return bad(box, "请填写生物 ID");
    if (!entityOK(raw)) return bad(box, "格式无效", `「${raw}」不是有效的生物 ID（仅支持小写字母、数字及 _ . - / :，如 minecraft:villager）`);
    const id = raw.includes(":") ? raw : "minecraft:" + raw; // 没写命名空间的按原版的算（NormalizeEntityID）
    inp.value = ""; box.classList.remove("has");
    const w = $("[data-lw=deny]", r), list = $(".list", w);
    if (D.deny.includes(id)) { flash($(`.ex-chip[data-id="${CSS.escape(id)}"]`, list)); return; }
    D.deny.push(id); list.insertAdjacentHTML("beforeend", denyChip(id)); rowIn(list.lastElementChild); noneOf(w);
    list.scrollTo({ top: list.scrollHeight, behavior: "smooth" }); pending(r);
  }
  function resetDeny(r) {
    const D = cp(); if (D.deny.join() === DEF_DENY.join()) return;
    D.deny = [...DEF_DENY]; const w = $("[data-lw=deny]", r);
    swapKids($(".list", w), D.deny.map(denyChip).join(""), 1, "Y", () => noneOf(w)); pending(r);
  }
  // 停下：那一行的状态、第三行、「停下」三处换掉；事件一栏顶上多一条
  function stopComp(r, c) {
    if (!c.task) return;
    c.last = { title: taskTitle(c.task), result: "cancelled", at: nowHM() }; const title = c.last.title; c.task = null; c.state = "idle";
    const row = $(`.ex-cr[data-id="${c.id}"]`, r);
    if (row) {
      swapIn($("[data-k=cst]", row), pill(...COMP_STATE.idle), 1);
      swapIn($("[data-k=ctask]", row), `<em>上次：${esc(title)} 已停止 · ${esc(c.last.at)}</em>`, 1);
      const sb = $("[data-k=cstop]", row); sb.firstElementChild && sb.firstElementChild.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: E_IN, fill: "forwards" }).finished.then(() => { sb.innerHTML = ""; });
    }
    const sa = $("[data-ex-act=stopall]", r); if (sa) sa.disabled = !cp().comps.some(x => x.task);
    addEv(r, `「${c.name}」已停止${title}`);
  }
  // 名字、主人的检查：同 companion.js 的 nameErr、playerOK
  const compNameErr = n => {
    if (!n || [...n].length > 16) return ["需 1～16 个字符", "伙伴名称长度须为 1～16 个字符"];
    if (/[\s@＠/／、:：,，<>"\\§]/.test(n) || /[\u0000-\u001f]/.test(n)) return ["含禁用字符", "名称不可包含空格、@、/、顿号、冒号、逗号、< > \" \\ §（聊天中需通过「@伙伴名」呼叫）"];
    return null;
  };
  function spawnDialog(r) {
    const D = cp(), on = C.online || [];
    let near = "";
    const body = `<div class="ex-fgrid">`
      + fld("名字", "玩家在聊天中通过「@伙伴名」呼叫该伙伴", `<label class="ex-in" data-ex-in="sname"><input maxlength="16" placeholder="如 小方" spellcheck="false" autocomplete="off" aria-label="名字"><span class="ex-msg"></span></label>`)
      + fld("主人", "选填。伙伴将默认出生在主人身旁（主人离线时出生在世界出生点）", `<label class="ex-in" data-ex-in="owner"><input maxlength="20" placeholder="玩家名（选填）" value="${on.length === 1 ? esc(on[0]) : ""}" spellcheck="false" autocomplete="off" aria-label="主人"><span class="ex-msg"></span></label>`
        + (on.length ? `<div class="ex-picks">${on.map(n => `<button type="button" class="ex-pk" data-pk="${esc(n)}" title="填入「主人」">${esc(n)}</button>`).join("")}</div>` : ""))
      + fld("出生位置", "也可指定出生在某位在线玩家身旁", ddField("near", "主人身旁", "出生位置")) + `</div>`;
    ask({ ic: "exBot", tone: "acc", title: "刷出伙伴", tip: "伙伴默认出生在主人身旁（主人离线时出生在世界出生点），亦可指定出生在某位在线玩家身旁。\n伙伴不占用在线玩家名额，亦不显示在 Tab 列表中。", body, ok: "刷出", w: 420, focusInput: true,
      setup: mk => mk.addEventListener("click", e => {
        const pk = e.target.closest("[data-pk]"); if (pk) { const i = $("[data-ex-in=owner] input", mk); i.value = pk.dataset.pk; i.closest(".ex-in").classList.remove("bad"); i.focus(); return; }
        const f = e.target.closest("[data-ex-dd=near]");
        if (f) openDD(f, [{ v: "", l: "主人身旁" }, ...on.map(n => ({ v: n, l: `${n} 身旁` }))], near, v => { near = v; swapIn($("span", f), esc(v ? `${v} 身旁` : "主人身旁"), 1); });
      }),
      onOk: mk => {
        const nb = $("[data-ex-in=sname]", mk), ob = $("[data-ex-in=owner]", mk), n = $("input", nb).value.trim(), owner = $("input", ob).value.trim();
        const err = compNameErr(n); if (err) { bad(nb, err[0], err[1]); return false; }
        if (D.comps.some(c => same(c.name, n))) { bad(nb, "名称重复", `已存在名为「${n}」的伙伴`); return false; }
        if (owner && !/^[A-Za-z0-9_.]{1,20}$/.test(owner)) { bad(ob, "格式无效", "主人游戏名仅支持 1～20 个字母、数字、_ 及 ."); return false; }
        const c = { id: D.nextId++, name: n, owner, state: "starting", hp: 20, max: 20, dim: "minecraft:overworld", xyz: near === "Alex" ? [14, 64, 201] : [-86, 71, 259], task: null, last: null };
        D.comps.push(c);
        const w = $("[data-lw=comps]", r), list = w && $(".list", w);
        if (list) { list.insertAdjacentHTML("beforeend", compRow(c)); const row = list.lastElementChild; hy(row); rowIn(row); noneOf(w); list.scrollTo({ top: list.scrollHeight, behavior: "smooth" }); }
        paintComps(r); addEv(r, `已刷出伙伴「${n}」`);
        setTimeout(() => { c.state = "idle"; const row = $(`.ex-cr[data-id="${c.id}"]`, r); if (row) swapIn($("[data-k=cst]", row), pill(...COMP_STATE.idle), 1); }, 1400);
      } });
  }
  function despawnDialog(r, c) {
    let drop = false;
    ask({ ic: "trash", tone: "dgr", title: `移除伙伴「${esc(c.name)}」？`, w: 380,
      body: `<div class="ex-mb">伙伴将从当前存档中移除。</div><div class="ex-mrow">${tgl({ key: "drop", on: false, label: "将背包物品掉落至原地", tip: "关闭时背包物品将随伙伴一同清除" })}</div>`,
      ok: "移除伙伴",
      setup: mk => mk.addEventListener("click", e => { const t = e.target.closest("[data-ex-tg=drop]"); if (t) { drop = !drop; setTgl(t, drop); } }),
      onOk: () => {
        const D = cp(); D.comps = D.comps.filter(x => x.id !== c.id);
        const row = $(`.ex-cr[data-id="${c.id}"]`, r);
        rowOut(row, () => noneOf($("[data-lw=comps]", r)));
        paintComps(r); addEv(r, `已移除伙伴「${c.name}」${drop ? "，背包物品已掉落至原地" : ""}`);
      } });
  }
  // 禁挖区域：表单弹窗（同程序的字段：名字、维度、维度 ID、两个角的 x y z），字段就地拦下
  const coordErr = (k, s) => { if (!/^-?\d+$/.test(s.trim())) return "坐标须为整数"; const n = +s; if (k === "y") return n < -2048 || n > 2048 ? "y 坐标范围为 -2048 至 2048" : ""; return Math.abs(n) > 30000000 ? "x、z 坐标须在 ±30000000 以内" : ""; };
  function zoneDialog(r, z) {
    const D = cp(), cur = z || { name: "", dim: "minecraft:overworld", min: ["", "", ""], max: ["", "", ""] };
    let dim = DIMS.some(d => d[0] === cur.dim) ? cur.dim : "";
    const xyz = (id, c) => `<div class="ex-xyz">${["x", "y", "z"].map((k, i) => `<label class="ex-in" data-ex-in="${id}${k}"><span class="ex-ax">${k}</span><input inputmode="numeric" value="${esc(c[i])}" spellcheck="false" autocomplete="off" aria-label="${id === "a" ? "角 1" : "角 2"} 的 ${k}"><span class="ex-msg"></span></label>`).join("")}</div>`;
    const dimLabel = v => v ? DIMS.find(d => d[0] === v)[1] : "其他（模组维度）";
    const body = `<div class="ex-fgrid two">`
      + fld("名字", "1～32 个字母、数字、_、-", `<label class="ex-in" data-ex-in="zname"><input maxlength="32" value="${esc(cur.name)}" placeholder="如 base、farm" spellcheck="false" autocomplete="off" aria-label="名字"><span class="ex-msg"></span></label>`)
      + fld("维度", "", ddField("dim", dimLabel(dim), "维度"))
      + `<div class="ex-dimid${dim ? "" : " on"}">${fld("维度 ID", "模组维度须填写完整 ID", `<label class="ex-in mono" data-ex-in="dimid"><input value="${dim ? "" : esc(cur.dim)}" placeholder="如 twilightforest:twilight_forest" spellcheck="false" autocomplete="off" aria-label="维度 ID"><span class="ex-msg"></span></label>`)}</div>`
      + `<div class="ex-full">${fld("角 1", "", xyz("a", cur.min))}</div><div class="ex-full">${fld("角 2", "", xyz("b", cur.max))}</div></div>`;
    ask({ ic: "exPick", tone: "acc", title: z ? `编辑禁挖区域「${esc(z.name)}」` : "添加禁挖区域", ok: "保存", w: 460, focusInput: true,
      tip: "填写区域对角两端的方块坐标（含端点），例如基地东北角与西南角。若不确定 y 范围，可填 -64 至 320 覆盖全部高度。", body,
      setup: mk => mk.addEventListener("click", e => {
        const f = e.target.closest("[data-ex-dd=dim]"); if (!f) return;
        openDD(f, [...DIMS.map(([v, l]) => ({ v, l: `${l}（${v}）` })), { v: "", l: "其他（模组维度）" }], dim, v => {
          dim = v; swapIn($("span", f), esc(dimLabel(v)), 1);
          $(".ex-dimid", mk).classList.toggle("on", !v); if (!v) setTimeout(() => $("[data-ex-in=dimid] input", mk).focus(), 60);
        });
      }),
      onOk: mk => {
        const q = k => $(`[data-ex-in=${k}]`, mk), n = $("input", q("zname")).value.trim();
        if (!/^[A-Za-z0-9_-]{1,32}$/.test(n)) { bad(q("zname"), "格式无效", "名称仅支持 1～32 个字母、数字、_ 及 -"); return false; }
        if (D.zones.some(x => same(x.name, n) && (!z || x.name !== z.name))) { bad(q("zname"), "名称重复", `已存在名为「${n}」的区域`); return false; }
        const d = dim || $("input", q("dimid")).value.trim();
        if (!(d.length <= 100 && /^[a-z0-9_.-]+:[a-z0-9_./-]+$/.test(d))) { bad(q("dimid"), "需填写完整 ID", "维度须填写完整 ID，如 minecraft:overworld"); return false; }
        const pts = [];
        for (const id of ["a", "b"]) {
          const c = [];
          for (const k of ["x", "y", "z"]) { const b = q(id + k), v = $("input", b).value, er = coordErr(k, v); if (er) { bad(b, "", `${id === "a" ? "角 1" : "角 2"} 的 ${k}：${er}`); return false; } c.push(+v.trim()); }
          pts.push(c);
        }
        const nz = { name: n, dim: d, min: pts[0], max: pts[1] }, w = $("[data-lw=zones]", r), list = w && $(".list", w);
        if (z) { // 编辑：只换那一行
          D.zones = D.zones.map(x => x === z ? nz : x);
          const row = list && $$(".ex-zr", list).find(x => x.dataset.name === z.name);
          if (row) { row.dataset.name = n; swapIn(row, zoneRow(nz).replace(/^<div[^>]*>|<\/div>$/g, ""), 1); }
        } else {
          D.zones.push(nz);
          if (list) { list.insertAdjacentHTML("beforeend", zoneRow(nz)); const row = list.lastElementChild; hy(row); rowIn(row); noneOf(w); list.scrollTo({ top: list.scrollHeight, behavior: "smooth" }); }
          paintCount(r, "nZone", String(D.zones.length));
        }
        pending(r);
      } });
  }
  function zoneDel(r, name) {
    ask({ ic: "trash", tone: "dgr", title: `删除禁挖区域「${esc(name)}」？`, body: `<div class="ex-mb">删除后，伙伴将可以在该区域内破坏或放置方块。</div>`, ok: "删除区域", w: 380,
      onOk: () => {
        const D = cp(); D.zones = D.zones.filter(z => z.name !== name);
        const w = $("[data-lw=zones]", r), row = w && $$(".ex-zr", w).find(x => x.dataset.name === name);
        rowOut(row, () => noneOf(w)); paintCount(r, "nZone", String(D.zones.length || "")); pending(r);
      } });
  }
  // 安装：草图里转 1.6 秒就算装好；开着服装的要重启才加载（程序同样提示）
  function install(r) {
    const D = cp(); if (D.installing) return;
    D.installing = true; swapIn($("[data-k=strip]", r), stripHTML(D), 1);
    setTimeout(() => {
      D.installing = false;
      D.mods.forEach(m => { m.state = "ok"; m.version = m.want; });
      if (C.st !== "stopped") D.restart = true;
      if (!r.isConnected) return;
      swapIn($("[data-k=strip]", r), stripHTML(D), 1);
      $$(".ex-mr[data-mod]", r).forEach(row => {
        const m = D.mods.find(x => x.id === row.dataset.mod);
        swapIn($("[data-k=mst]", row), tag(...[MOD_STATE.ok[1], MOD_STATE.ok[0]]), 1); swapIn($("[data-k=mver]", row), esc(`已安装 ${m.version}`), 1);
      });
      const dot = $(".ex-sdot", r); if (dot) dot.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "scale(.4)" }], { duration: 180, easing: E_IN, fill: "forwards" }).finished.then(() => { dot.hidden = true; dot.getAnimations().forEach(a => a.cancel()); });
      const sw = $("[data-ex-tg=cpOn]", r); sw.removeAttribute("aria-disabled"); setTip(sw, cpSwTip(D));
      paintLink(r);
    }, 1600);
  }
  window.DETAIL_TABS["ext/companion"] = { html: cpHTML, mount: cpMount };

  // ======================================================================
  // ================= 音乐共享 ===========================================
  // ======================================================================
  // 正在出声的程序（编的；字段同 /api/music/sources）：上次用的是网易云音乐，打开时选它；java（游戏本身）不列
  const SRC = [
    { pid: 11840, exe: "cloudmusic.exe", name: "网易云音乐", playing: true, dev: "扬声器 (Realtek(R) Audio)" },
    { pid: 15020, exe: "msedge.exe", name: "Microsoft Edge", playing: true, dev: "扬声器 (Realtek(R) Audio)" },
    { pid: 7724, exe: "QQMusic.exe", name: "QQ音乐", playing: false, dev: "" },
    { pid: 9312, exe: "Spotify.exe", name: "Spotify", playing: false, dev: "" },
  ];
  const DEVS = [["", "系统默认（扬声器 (Realtek(R) Audio)）"], ["cable", "CABLE Input (VB-Audio Virtual Cable)"], ["hdmi", "DELL U2723QE (NVIDIA High Definition Audio)"]];
  const SVC_VER = "1.20.1-2.5.26", SVC_FILE = "voicechat-forge-1.20.1-2.5.26.jar", BRIDGE_VER = "0.2.0"; // 桥接模组的版本同 internal/bridge/jar.go
  const SYS_WARN = "整机录音会采集本机全部输出音频（含游戏音效与好友语音），可能导致好友听到自己的回声。";
  const MU = {};
  function mu() {
    const k = C.kind;
    if (!MU[k]) {
      const ready = MODS(), run = running();
      MU[k] = {
        svc: { installed: ready, busy: false }, bridge: { installed: ready, variant: "main", busy: false }, voice: { found: ready, port: 24454, host: "" }, restart: [],
        mode: "process", dev: "", sel: 11840, vol: 100, exclude: "", sysOK: false, live: null,
        srcs: SRC.map(s => ({ ...s })),
        last: run ? { at: "20:15", reason: "用户停止" } : { at: "11 月 26 日 22:40", reason: "服务器已停止" },
      };
    }
    return MU[k];
  }
  const svcReady = M => M.svc.installed && !M.restart.includes("Simple Voice Chat");
  // 听众：连上语音的人（装了 SVC 的服务器上，Steve 装了 SVC、Alex 没装）；「我在游戏里的名字」里的不再发给
  function listeners(M) {
    if (!running()) return null;
    const on = C.online || [], ex = M.exclude.split(/[,，;；\s]+/).filter(Boolean);
    const voice = svcReady(M) && M.bridge.installed ? on.filter(n => same(n, "Steve")) : [];
    return { voice: voice.filter(n => !has(ex, n)), excluded: voice.filter(n => has(ex, n)), noVoice: on.filter(n => !has(voice, n)) };
  }
  // 为什么点不了「开始广播」（music.js 的 blockers 原话，只露第一条）
  function blockers(M) {
    const out = [];
    if (!M.svc.installed) out.push("请先安装 Simple Voice Chat"); else if (M.restart.includes("Simple Voice Chat")) out.push("Simple Voice Chat 需重启服务器后生效");
    if (!M.bridge.installed) out.push("请先安装桥接模组"); else if (M.restart.includes("桥接模组")) out.push("桥接模组需重启服务器后生效");
    if (C.st !== "running") out.push(C.st === "starting" ? "服务器启动中，请等待完成" : "请先启动服务器");
    if (M.mode === "process" && !M.sel) out.push("请先在上方选择音频源程序");
    return out;
  }
  const guideMU = M => "服务器开了「MineDeck 音乐共享」：房主电脑上放的歌，会通过 Simple Voice Chat（简单语音聊天）播给大家。\n"
    + (M.svc.installed ? `1. 客户端装 Simple Voice Chat，和服务器同一个版本最稳妥：服务器装的是 ${SVC_VER}。\n` : "1. 客户端装 Simple Voice Chat，版本和服务器的一样（问房主要）。\n")
    + "2. 进服后按 V 打开语音菜单，点「调整音量」，找到「MineDeck 音乐共享」：拉到 0 就只静音音乐，不影响别人说话。"
    + (M.voice.host ? `\n3. 远程连服的朋友：语音走 ${M.voice.host}，不用另外设置；连不上语音时告诉房主。` : ""); // 同 musicshare/status.go 的 guide

  function muHTML(ctx) {
    C = ctx; closeAll();
    const M = mu();
    return `<div class="ex ex-mus"><section class="ex-col ex-cast" data-ex-col="cast">`
      + `<div class="ex-hd"><span class="ex-t">广播</span><span class="ex-pslot" data-k="lpill">${M.live ? livePill(M) : ""}</span><span class="ex-last" data-k="last">${lastText(M)}</span>`
      + `<button type="button" class="ex-ib ex-corner" data-ex-act="srcRefresh" title="刷新列表"${M.mode === "process" && !M.live ? "" : " hidden"}><i data-ic="refresh"></i></button></div>`
      + `<div class="ex-mid" data-k="mid">${midHTML(M)}</div>`
      + `<div class="ex-lim"><span class="ex-ll"><b title="默认仅采集单个程序音频；切换为整机录音会一并采集游戏音效与语音，可能产生回声。">采集方式</b></span>`
      + segHTML("mode", [["process", "单程序采集"], ["system", "整机录音"]], M.mode, true, M.live ? "广播进行中不可切换，请先停止广播" : "") + `</div>`
      + `<div class="ex-lim"><span class="ex-ll"><b title="100% 为原始音量，调整后即时生效。">音量</b></span>${slider(M.vol)}</div>`
      + `<div class="ex-act"><span class="ex-why" data-k="why">${whyHTML(M)}</span>${castBtn(M)}</div></section>`
      + `<div class="ex-side"><section class="ex-col" data-ex-col="check"><div class="ex-hd"><span class="ex-t" title="所有必需项就绪后即可广播。已安装 Simple Voice Chat（SVC）的玩家均可收听，亦可在游戏内 SVC 菜单单独静音。">检查清单</span>`
      + `<span class="ex-hslot" data-k="rs">${restartTag(M)}</span></div><div class="ex-cks2">${checkRows(M)}</div></section>`
      + `<section class="ex-col" data-ex-col="lis"><div class="ex-hd"><span class="ex-t">听众</span>`
      + `<button type="button" class="ex-ib ex-corner" data-ex-act="copyGuide" title="${esc("复制发给好友的说明：\n" + guideMU(M))}"><i data-ic="copy"></i></button></div>`
      + `<div class="ex-lw${listeners(M) ? "" : " is-none"}" data-lw="lis"><div class="list" data-k="groups">${groupsHTML(M)}</div><span class="thumb"></span>`
      + `<div class="ex-none" title="已连接语音的玩家均可收听音乐；未连接通常因未安装 SVC 或版本与服务端不一致。">服务器运行后将显示已连接语音的玩家</div></div>`
      + fld("我在游戏里的名字", "填写后不再向你推送音乐：本机已能直接听到原声，再经 SVC 播放会听到两遍。\n多项以逗号分隔，选填。",
        saveIn({ key: "exclude", val: M.exclude, ph: "如 Steve", max: 400, label: "我在游戏里的名字" })) + `</section></div></div>`;
  }
  const lastText = M => !M.live && M.last ? esc(`上次广播于 ${M.last.at} 停止：${M.last.reason}`) : "";
  const livePill = M => M.live.phase === "live" ? pill("ok", "广播中") : pill("warm", "连接中…");
  function midHTML(M) {
    if (M.live) return liveHTML(M);
    if (M.mode === "system") return `<div class="ex-sys"><div class="ex-warn tall" title="${esc(SYS_WARN + "\n降低回声建议：主机避免在本机运行客户端，或调低游戏与语音音量。")}">${ico("warn")}<span>整机录音会广播本机全部声音</span></div>`
      + fld("播放设备", "选择要采集音频的播放设备。在 Windows「应用音量和设备首选项」中将音乐软件指定至独立设备（如虚拟声卡）并在此选用，可避免采集游戏声与回声。",
        ddField("dev", DEVS.find(d => d[0] === M.dev)[1], "播放设备")) + `</div>`;
    const list = [...M.srcs].sort((a, b) => a.exe.localeCompare(b.exe));
    return `<div class="ex-lw${list.length ? "" : " is-none"}" data-lw="srcs"><div class="list ex-srcs" role="radiogroup" aria-label="音源"><i class="ex-shl"></i>${list.map(s => srcRow(M, s)).join("")}</div><span class="thumb"></span>`
      + `<div class="ex-none" title="请先在音乐软件中播放音频，再点击刷新。Minecraft 进程（java）已自动排除，避免产生回声。">暂无出声程序</div></div>`;
  }
  const meter = (n, on = 0) => `<span class="ex-meter${n > 10 ? " big" : ""}" title="当前电平">${Array.from({ length: n }, (_, i) => `<i${i < on ? ' class="on"' : ""}></i>`).join("")}</span>`;
  const srcRow = (M, s) => `<div class="ex-src${s.pid === M.sel ? " on" : ""}" data-pid="${s.pid}" role="radio" tabindex="0" aria-checked="${s.pid === M.sel}">`
    + `<span class="ex-av"><i data-ic="exMusic"></i></span><div class="ex-cm"><b class="ex-nm">${esc(s.name && s.name !== s.exe ? `${s.name}（${s.exe}）` : s.exe)}</b>`
    + `<span class="ex-c2"><span data-k="play">${s.playing ? "正在出声" : "未出声"}</span>${s.dev ? " · " + esc(s.dev) : ""}</span></div>${meter(10)}</div>`;
  function liveHTML(M) {
    const s = M.srcs.find(x => x.pid === M.sel), src = M.mode === "system" ? "整机声音" : s ? s.exe : "";
    return `<div class="ex-live"><span class="ex-av big"><i data-ic="exMusic"></i></span><div class="ex-lvt"><b>本机 · ${esc(src)}</b><span class="ex-mono" data-k="dur">已播 ${dur(M)}</span></div>`
      + meter(24) + `<div class="ex-lvl"><i data-ic="exHeadphones"></i><span data-k="lis">${lisText(M)}</span></div>`
      + `<div class="ex-lvn ex-mono" data-k="net">${netText(M)}</div>`
      + (M.mode === "system" ? `<div class="ex-warn">${ico("warn")}<span>整机录音中：游戏音效与语音聊天将一并广播</span></div>` : "") + `</div>`;
  }
  const dur = M => { const s = Math.max(0, Math.floor((Date.now() - M.live.since) / 1000)); return `${Math.floor(s / 60)}:${pad(s % 60)}`; };
  const lisText = M => { if (M.live.phase !== "live") return "连接桥接模组后显示听众数"; const n = listeners(M)?.voice.length || 0; return n ? `${n} 人在听` : "暂无听众"; };
  const netText = M => M.live.phase === "live" ? `缓冲 ${M.live.buf} 帧 · 往返 ${M.live.rtt} ms · 已发 ${M.live.sent.toLocaleString("en-US")} 帧` : "正在连接桥接模组…";
  function whyHTML(M) { if (M.live) return ""; const w = blockers(M)[0]; return w ? `${ico("warn")}<span>${esc(w)}</span>` : ""; }
  const castBtn = M => M.live ? `<button type="button" class="ex-btn big stop" data-ex-act="cast"><i data-ic="stop"></i>停止广播</button>`
    : `<button type="button" class="ex-btn big pri" data-ex-act="cast"${blockers(M).length ? " aria-disabled=\"true\"" : ""}><i data-ic="play"></i>${M.mode === "system" ? "开始广播（整机录音）" : "开始广播"}</button>`;
  // 音量：自己画的条（规范第九节：把手是竖着的小胶囊），0–200、一下 5，100% 那里一道刻度
  const slider = v => `<div class="ex-sl" data-ex-sl="vol" role="slider" tabindex="0" aria-label="音量" aria-valuemin="0" aria-valuemax="200" aria-valuenow="${v}">`
    + `<span class="ex-slt"><i class="ex-slf" style="width:${v / 2}%"></i><i class="ex-slm"></i></span><i class="ex-slh" style="left:${v / 2}%"></i></div><b class="ex-slv ex-mono">${v}%</b>`;
  const restartTag = M => M.restart.length ? `<span class="ex-tag warn" title="${esc(`${M.restart.join("、")} 是服务器运行中装上或改过的：重启服务器以后才生效。`)}">重启服务器后生效</span>` : "";
  // 检查清单五行：核心、SVC、桥接模组、语音端口、远程好友的语音（music.js 的 drawCheck；说明进悬停）
  function checkRows(M) {
    const st = (ok, icn) => `<span class="ex-cki ${ok === true ? "ok" : ok === false ? "bad" : ""}">${ico(ok === true ? "check" : ok === false ? "warn" : icn)}</span>`;
    const runNote = C.st !== "stopped" ? "服务器运行中，安装完成后需重启服务器生效。" : "";
    const svc = M.svc.installed
      ? row("svc", st(true), "Simple Voice Chat", `<span class="ex-mono">${SVC_VER}</span>${M.restart.includes("Simple Voice Chat") ? tag("重启生效", "warn") : ""}`, "", `已安装 ${SVC_VER}。玩家客户端也需安装 SVC，与服务器同版本最稳妥。\n${SVC_FILE}`)
      : row("svc", st(false), "Simple Voice Chat", `<span class="faint">未安装</span>`, M.svc.busy ? `<span class="ex-busy">${ico("spinner")}下载中…</span>` : `<button type="button" class="ex-btn" data-ex-act="svcInstall">${ico("download")}安装</button>`,
        `音乐通过此模组推送给玩家。点击「安装」从 Modrinth 下载适配 Forge ${C.S.mc} 的最新正式版至 mods 文件夹（需联网）。${runNote}玩家客户端亦须安装同一版本。`);
    const br = M.bridge.installed
      ? row("bridge", st(true), "桥接模组", `<span class="ex-mono">${BRIDGE_VER}${M.bridge.variant === "javafml" ? "（备用）" : ""}</span>${M.restart.includes("桥接模组") ? tag("重启生效", "warn") : ""}`,
        `<button type="button" class="ex-ib" data-ex-act="brMenu" title="更多"><i data-ic="more"></i></button>`, `已安装 MineDeck 桥接模组 ${BRIDGE_VER}${M.bridge.variant === "javafml" ? "（备用加载方式）" : ""}${running() && !M.restart.includes("桥接模组") ? "，SVC 已加载" : ""}。`)
      : row("bridge", st(false), "桥接模组", `<span class="faint">未安装</span>`, M.bridge.busy ? `<span class="ex-busy">${ico("spinner")}</span>`
        : `<button type="button" class="ex-btn" data-ex-act="brInstall"${M.svc.installed ? "" : ` aria-disabled="true" title="请先安装 Simple Voice Chat：桥接模组依赖该模组，缺少会导致服务器无法启动。"`}>${ico("download")}安装</button>`,
        M.svc.installed ? `MineDeck 内置 ${BRIDGE_VER}，安装至 mods 文件夹即可，无需联网。${runNote}` : "");
    const port = M.voice.found
      ? row("port", st(null, "plug"), "语音端口", `<span class="ex-mono">UDP ${M.voice.port}</span>`, "", "局域网玩家：请在 Windows 防火墙放行此 UDP 端口以连接语音。\nconfig/voicechat/voicechat-server.properties")
      : row("port", st(null, "plug"), "语音端口", `<span class="faint">未生成</span>`, "", "Simple Voice Chat 首次启动服务器后将生成配置（默认 UDP 24454），请启动一次服务器后再查看。");
    const rm = M.voice.host
      ? row("remote", st(null, "globe"), "远程语音", `<span class="ex-mono ex-host">${esc(M.voice.host)}</span>`, `<button type="button" class="ex-ib" data-ex-act="rmMenu" title="更多"><i data-ic="more"></i></button>`,
        "voice_host 为手动配置：所有玩家（含局域网）均通过该地址连接语音，MineDeck 不会自动覆盖。")
      : row("remote", st(null, "globe"), "远程语音", `<span class="faint">未配置隧道</span>`,
        `<button type="button" class="ex-btn" data-ex-act="toNet" title="远程玩家收听语音需在「联机」页创建语音隧道（建议与游戏隧道同节点），MineDeck 将自动写入 voice_host。仅局域网联机时无需配置。">去联机</button>`
        + `<button type="button" class="ex-ib" data-ex-act="rmMenu" title="更多"><i data-ic="more"></i></button>`, "voice_host = （未设置）");
    return row("core", st(true), "核心", `<span class="ex-mono">${esc(C.S.label)} ${esc(C.S.mc)}</span>`, "", "支持音乐共享。") + svc + br + port + rm;
    function row(k, icon, label, val, actHTML, tip) {
      return `<div class="ex-ck2" data-ck="${k}"${tip ? ` title="${esc(tip)}"` : ""}>${icon}<span class="ex-ckl">${label}</span><span class="ex-ckv">${val}</span><span class="ex-cka">${actHTML}</span></div>`;
    }
  }
  function groupsHTML(M) {
    const L = listeners(M); if (!L) return "";
    const chips = (arr, tone = "") => `<div class="ex-chips">${arr.map(n => `<span class="ex-chip ro${tone ? " " + tone : ""}" data-n="${esc(n)}">${esc(n)}</span>`).join("")}</div>`;
    let h = `<div class="ex-grp" data-g="voice"><span class="ex-gl">${L.voice.length ? `可收听音乐 · ${L.voice.length} 人` : L.excluded.length ? "暂无其他听众" : "暂无玩家连接语音"}</span>${L.voice.length ? chips(L.voice, "ok") : ""}</div>`;
    if (L.excluded.length) h += `<div class="ex-grp" data-g="ex"><span class="ex-gl" title="在「我在游戏里的名字」里：不再发给他">不再发给</span>${chips(L.excluded)}</div>`;
    if (L.noVoice.length) h += `<div class="ex-grp" data-g="no"><span class="ex-gl warn" title="可能未安装 Simple Voice Chat，可点击右上角复制安装说明发送给对方。">在线未连语音 · ${L.noVoice.length} 人</span>${chips(L.noVoice, "warn")}</div>`;
    return h;
  }

  function muMount(ctx) {
    C = ctx;
    const r = rootOf("ex-mus"); if (!r) return;
    $$(".ex-seg", r).forEach(s => placeSeg(s, true));
    bars(r); fitNames(r); placeSrc(r, true);
    r.addEventListener("click", muClick); r.addEventListener("keydown", muKey); r.addEventListener("input", onInput); r.addEventListener("pointerdown", muDown);
    tickStart(r);
  }
  // 音源的选中框：单独一块，滑到选中的那一行（规范第七节）
  function placeSrc(r, instant) {
    const list = $(".ex-srcs", r), hl = list && $(".ex-shl", list), on = list && $(".ex-src.on", list); if (!hl) return;
    if (!on) { hl.style.opacity = "0"; return; }
    if (instant || hl.style.opacity === "0") hl.style.transition = "none";
    hl.style.opacity = "1"; hl.style.transform = `translateY(${on.offsetTop}px)`; hl.style.height = on.offsetHeight + "px";
    if (instant || hl.style.transition === "none") { void hl.offsetWidth; hl.style.transition = ""; }
  }
  // 电平原地跳：正在出声的那几行、正在广播的大条；每 0.11 秒换一下哪几格亮（草图编的随机电平）
  let ticker = 0, secT = 0;
  function tickStart(r) {
    clearInterval(ticker); clearInterval(secT);
    const lv = {};
    ticker = setInterval(() => {
      if (!r.isConnected) { clearInterval(ticker); return; }
      const M = mu();
      $$(".ex-src", r).forEach(row => {
        const s = M.srcs.find(x => x.pid === +row.dataset.pid); if (!s) return;
        const prev = lv[s.pid] ?? 4, n = s.playing ? Math.max(1, Math.min(10, Math.round(prev + (Math.random() - .45) * 4))) : 0; lv[s.pid] = n;
        $$(".ex-meter i", row).forEach((c, i) => c.classList.toggle("on", i < n));
      });
      const big = $(".ex-live .ex-meter", r);
      if (big) { const n = M.live && M.live.phase === "live" ? Math.max(3, Math.min(24, Math.round((lv.big ?? 12) + (Math.random() - .5) * 8))) : 0; lv.big = n || 12; $$("i", big).forEach((c, i) => c.classList.toggle("on", i < n)); }
    }, 110);
    secT = setInterval(() => {
      if (!r.isConnected) { clearInterval(secT); return; }
      const M = mu(); if (!M.live) return;
      if (M.live.phase === "live") { M.live.sent += 50; M.live.buf = 5 + Math.round(Math.random() * 2); M.live.rtt = 3 + Math.round(Math.random() * 3); const n = $("[data-k=net]", r); if (n) n.textContent = netText(M); }
      const d = $("[data-k=dur]", r); if (d) d.textContent = `已播 ${dur(M)}`;
    }, 1000);
  }
  function paintWhy(r) {
    const M = mu(), w = $("[data-k=why]", r), html = whyHTML(M);
    if (w.textContent !== hy(Object.assign(document.createElement("div"), { innerHTML: html })).textContent) swapIn(w, html, 1);
    const b = $("[data-ex-act=cast]", r);
    if (!M.live) { if (blockers(M).length) b.setAttribute("aria-disabled", "true"); else b.removeAttribute("aria-disabled"); }
  }
  function paintCheck(r, k) {
    const M = mu(), tmp = document.createElement("div"); tmp.innerHTML = checkRows(M); hy(tmp);
    const nw = $(`[data-ck=${k}]`, tmp), old = $(`[data-ck=${k}]`, r); if (!nw || !old) return;
    swapIn(old, nw.innerHTML, 1); setTip(old, nw.getAttribute("title") || "");
  }
  function paintGroups(r) {
    const M = mu(), w = $("[data-lw=lis]", r), box = $("[data-k=groups]", r); if (!box) return;
    const html = groupsHTML(M); w.classList.toggle("is-none", !html);
    swapKids(box, html, 1, "Y", () => repaint(w));
    setTip($("[data-ex-act=copyGuide]", r), "复制发给好友的说明：\n" + guideMU(M));
  }
  function muClick(e) {
    const r = e.currentTarget, M = mu();
    const ms = e.target.closest("[data-ex-seg=mode] span[data-v]");
    if (ms) { if (ms.closest(".ex-seg").getAttribute("aria-disabled") === "true" || ms.dataset.v === M.mode) return; setMode(r, ms.dataset.v); return; }
    const src = e.target.closest(".ex-src"); if (src) { pickSrc(r, +src.dataset.pid); return; }
    if (e.target.closest(".ex-save")) { e.preventDefault(); saveExclude(r, e.target.closest(".ex-in")); return; }
    const f = e.target.closest("[data-ex-dd=dev]");
    if (f) { openDD(f, DEVS.map(([v, l]) => ({ v, l })), M.dev, v => { M.dev = v; swapIn($("span", f), esc(DEVS.find(d => d[0] === v)[1]), 1); }); return; }
    const a = e.target.closest("[data-ex-act]"); if (!a) return;
    if (a.getAttribute("aria-disabled") === "true" && a.dataset.exAct !== "cast") return; // 「开始广播」灰着时点了要晃一下、指着原因
    switch (a.dataset.exAct) {
      case "cast": M.live ? stopCast(r) : startCast(r, a); break;
      case "srcRefresh": spinOnce(a); refreshSrc(r); break;
      case "svcInstall": svcInstall(r); break;
      case "brInstall": brInstall(r); break;
      case "brMenu": openDD(a, [{ v: "alt", ic: "package", l: M.bridge.variant === "javafml" ? "切换为默认加载方式" : "切换为备用加载方式", dis: C.st !== "stopped" ? "请先停止服务器" : "" }], null, () => {
        M.bridge.variant = M.bridge.variant === "javafml" ? "main" : "javafml"; paintCheck(r, "bridge"); }); break;
      case "rmMenu": openDD(a, [{ v: "vh", ic: "exPencil", l: "手动填写 voice_host…" }], null, () => vhPop(r, a)); break;
      case "toNet": C.goTab("net"); break;
      case "copyGuide": doneTick(a); break;
    }
  }
  function muKey(e) {
    const r = e.currentTarget, M = mu();
    if (e.target.classList?.contains("ex-src") && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); pickSrc(r, +e.target.dataset.pid); return; }
    if (e.target.closest?.("[data-ex-sl]")) {
      const d = { ArrowRight: 5, ArrowUp: 5, ArrowLeft: -5, ArrowDown: -5, PageUp: 25, PageDown: -25 }[e.key];
      if (d) { e.preventDefault(); setVol(r, M.vol + d); } else if (e.key === "Home") setVol(r, 0); else if (e.key === "End") setVol(r, 200);
      return;
    }
    if (e.key === "Enter" && e.target.tagName === "INPUT") { const box = e.target.closest(".ex-in"); if (box?.dataset.exIn === "exclude") { e.preventDefault(); if (box.classList.contains("dirty")) saveExclude(r, box); } }
  }
  // 拖音量：按住把手或条上任意一处都行
  function muDown(e) {
    const sl = e.target.closest("[data-ex-sl]"); if (!sl) return;
    e.preventDefault(); sl.focus();
    const r = e.currentTarget, t = $(".ex-slt", sl), at = x => { const b = t.getBoundingClientRect(); setVol(r, Math.round(Math.max(0, Math.min(1, (x - b.left) / b.width)) * 40) * 5); };
    at(e.clientX); sl.classList.add("drag"); sl.setPointerCapture(e.pointerId);
    const mv = ev => at(ev.clientX), up = () => { sl.classList.remove("drag"); sl.removeEventListener("pointermove", mv); sl.removeEventListener("pointerup", up); };
    sl.addEventListener("pointermove", mv); sl.addEventListener("pointerup", up);
  }
  function setVol(r, v) {
    const M = mu(); v = Math.max(0, Math.min(200, v)); if (v === M.vol) return; M.vol = v; // 即时生效（程序是松手时存）
    const sl = $("[data-ex-sl=vol]", r); sl.setAttribute("aria-valuenow", v);
    $(".ex-slf", sl).style.width = v / 2 + "%"; $(".ex-slh", sl).style.left = v / 2 + "%"; sl.nextElementSibling.textContent = v + "%";
  }
  function pickSrc(r, pid) {
    const M = mu(); if (M.live || M.sel === pid) return;
    M.sel = pid;
    $$(".ex-src", r).forEach(x => { const on = +x.dataset.pid === pid; x.classList.toggle("on", on); x.setAttribute("aria-checked", on); });
    placeSrc(r); paintWhy(r);
  }
  // 刷新列表：草图里 Spotify 开始出声，其余不动；只换变了的那一行的「正在出声」
  function refreshSrc(r) {
    const M = mu(), s = M.srcs.find(x => x.exe === "Spotify.exe"); if (!s || s.playing) return;
    s.playing = true; s.dev = "扬声器 (Realtek(R) Audio)";
    const row = $(`.ex-src[data-pid="${s.pid}"]`, r); if (row) swapIn($(".ex-c2", row), `<span data-k="play">正在出声</span> · ${esc(s.dev)}`, 1);
  }
  function setMode(r, v) {
    const M = mu(), dir = v === "system" ? 1 : -1; M.mode = v; closeDD();
    pickSeg($("[data-ex-seg=mode]", r), v);
    swapKids($("[data-k=mid]", r), midHTML(M), dir, "X", () => { bars(r); fitNames(r); placeSrc(r, true); });
    $("[data-ex-act=srcRefresh]", r).hidden = v !== "process";
    const b = $("[data-ex-act=cast]", r); swapIn(b, `${ico("play")}${v === "system" ? "开始广播（整机录音）" : "开始广播"}`, 1, "Y");
    paintWhy(r);
  }
  function startCast(r, btn) {
    const M = mu();
    if (blockers(M).length) { shake($("[data-k=why]", r)); shake(btn); return; }
    // 整机录音：开始前确认一次（每台电脑一次，确认过就记住；同 music.js）
    if (M.mode === "system" && !M.sysOK) {
      ask({ ic: "warn", tone: "dgr", title: "确认使用整机录音？", no: "取消", ok: "继续广播", w: 420,
        body: `<div class="ex-mb">${esc(SYS_WARN)}</div><div class="ex-mb faint">降低回声建议：主机避免在本机运行客户端，或调低游戏与语音音量。本机确认一次后将不再提示。</div>`,
        onOk: () => { M.sysOK = true; setTimeout(() => go(), 150); } });
      return;
    }
    go();
    function go() {
      M.live = { phase: "connecting", since: Date.now(), sent: 0, buf: 6, rtt: 4 };
      swapIn($("[data-k=last]", r), "", 1); swapIn($("[data-k=lpill]", r), livePill(M), 1);
      swapKids($("[data-k=mid]", r), liveHTML(M), 1, "Y");
      $("[data-ex-act=srcRefresh]", r).hidden = true;
      const ms = $("[data-ex-seg=mode]", r); ms.setAttribute("aria-disabled", "true"); setTip(ms, "广播进行中不可切换，请先停止广播");
      const b = $("[data-ex-act=cast]", r); b.classList.replace("pri", "stop"); b.removeAttribute("aria-disabled"); swapIn(b, `${ico("stop")}停止广播`, 1);
      paintWhy(r);
      setTimeout(() => {
        if (!M.live || M.live.phase !== "connecting") return;
        M.live.phase = "live"; if (!r.isConnected) return;
        swapIn($("[data-k=lpill]", r), livePill(M), 1); swapIn($("[data-k=lis]", r), esc(lisText(M)), 1); swapIn($("[data-k=net]", r), esc(netText(M)), 1);
      }, 1200);
    }
  }
  function stopCast(r) {
    const M = mu(); M.live = null; M.last = { at: nowHM(), reason: "用户停止" };
    swapIn($("[data-k=lpill]", r), "", 1); swapIn($("[data-k=last]", r), lastText(M), 1);
    swapKids($("[data-k=mid]", r), midHTML(M), -1, "Y", () => { bars(r); fitNames(r); placeSrc(r, true); });
    $("[data-ex-act=srcRefresh]", r).hidden = M.mode !== "process";
    const ms = $("[data-ex-seg=mode]", r); ms.removeAttribute("aria-disabled"); setTip(ms, "");
    const b = $("[data-ex-act=cast]", r); b.classList.replace("stop", "pri"); swapIn(b, `${ico("play")}${M.mode === "system" ? "开始广播（整机录音）" : "开始广播"}`, 1);
    paintWhy(r);
  }
  // 装 SVC：草图里转 1.4 秒（程序从 Modrinth 下）；开着服装的要重启（清单顶上一个「重启服务器后生效」、那一行一个「重启生效」）
  function svcInstall(r) {
    const M = mu(); if (M.svc.busy) return;
    M.svc.busy = true; paintCheck(r, "svc");
    setTimeout(() => {
      M.svc.busy = false; M.svc.installed = true; if (C.st !== "stopped") M.restart.push("Simple Voice Chat");
      if (!r.isConnected) return;
      paintCheck(r, "svc"); paintCheck(r, "bridge"); paintRs(r); paintWhy(r); paintGroups(r);
    }, 1400);
  }
  function brInstall(r) {
    const M = mu(); if (M.bridge.busy || !M.svc.installed) return;
    M.bridge.busy = true; paintCheck(r, "bridge");
    setTimeout(() => {
      M.bridge.busy = false; M.bridge.installed = true; if (C.st !== "stopped") M.restart.push("桥接模组");
      if (!r.isConnected) return;
      paintCheck(r, "bridge"); paintRs(r); paintWhy(r);
    }, 700);
  }
  function paintRs(r) {
    const M = mu(), s = $("[data-k=rs]", r), html = restartTag(M), had = !!s.firstElementChild;
    if (had && html) setTip(s.firstElementChild, `${M.restart.join("、")} 是服务器运行中装上或改过的：重启服务器以后才生效。`);
    else if (had !== !!html) swapIn(s, html, 1);
  }
  // 手动填 voice_host：弹一块小浮层（程序是行下面展开一个框）
  function vhPop(r, anchor) {
    const M = mu();
    openPop(anchor, `<div class="ex-ph" title="填写后所有玩家（含局域网）均通过该地址连接语音，MineDeck 不会自动覆盖。${running() ? "\nSVC 仅在启动时读取该项，重启服务器后生效。" : ""}">voice_host</div>`
      + saveIn({ key: "vh", val: M.voice.host, ph: "如 frp-abc.example.com:34567（留空清除）", mono: true, max: 200, label: "voice_host" }), p => {
      const inp = $("input", p); inp.focus(); inp.select();
      const save = () => {
        const box = $(".ex-in", p), v = inp.value.trim();
        if (v && !/^[A-Za-z0-9.-]+(:\d{1,5})?$/.test(v)) return bad(box, "格式无效", "voice_host 须为「主机名」或「主机名:端口」格式");
        M.voice.host = v; savedOK(box); paintCheck(r, "remote");
        setTip($("[data-ex-act=copyGuide]", r), "复制发给好友的说明：\n" + guideMU(M));
        setTimeout(closePop, 500);
      };
      p.addEventListener("click", e => { if (e.target.closest(".ex-save")) save(); });
      p.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); if ($(".ex-in", p).classList.contains("dirty")) save(); } });
    });
  }
  // 我在游戏里的名字：同 music.js 的 checkNames；存了以后听众那一栏跟着变
  function saveExclude(r, box) {
    const M = mu(), v = $("input", box).value;
    for (const n of v.split(/[,，;；\s]+/).filter(Boolean)) {
      if ([...n].length > 36) return bad(box, "名称过长", `「${n.slice(0, 12)}…」超出限制，每个名称最多 36 个字符`);
      if (/[\\=#!:\u0000-\u001f]/.test(n)) return bad(box, "含禁用字符", `「${n}」包含不支持的字符（\\ = # ! :）`);
    }
    M.exclude = v.trim(); $("input", box).value = M.exclude; savedOK(box);
    if (listeners(M)) paintGroups(r);
    if (M.live && M.live.phase === "live") swapIn($("[data-k=lis]", r), esc(lisText(M)), 1);
  }
  window.DETAIL_TABS["ext/music"] = { html: muHTML, mount: muMount };
})();
