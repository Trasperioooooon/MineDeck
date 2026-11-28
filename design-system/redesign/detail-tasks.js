// 服务器详情页 →「定时任务」（11-27 第一稿，子代理照临时规范画的；计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 detail-tasks.css。
//
// 程序：internal/web/ui/app.js 的 view === "tasks"。11-27 夜里另一个分支给它加了「每隔多久」、文件夹和一键添加（作者转述的用户原话：
// 「想一键设置1个小时存一次档，1个小时扫一次地，再搞个任务分类文件夹，把扫地的指令单独放一个，存档的单独放一个。」），这一稿照那份画。
// 那一页从上到下：崩溃自动重启（开关加一段说明）；「一键添加」两颗；列表（不放文件夹的在上，文件夹按出现先后排在下面；每行类型徽标、
// 「每天 HH:MM」或「每隔 N」、命令、「未保存」、「移到…」「移除」）；添加一行（类型下拉、每天 / 每隔下拉、时:分框或间隔数字框加单位下拉、命令框、
// 文件夹框带候选、「添加」）；「5 分钟后」「10 分钟后」「30 分钟后」；吸底保存条「保存策略」。
// 后端 internal/inst/policy.go：类型 restart / command / backup；at（HH:MM）和 every（分钟）二选一，every 从每天 0 点起按钟点对齐
// （每隔 60 就是每个整点），最短命令 5、备份 30、重启 60，最长 720；group 是文件夹名（最多 32 字），只管分组显示，挪空了文件夹就没了。
// 到点时：命令、重启只在服务器开着时执行；每隔多久的备份停着时不做（世界没变），每天定时的备份照做。
// 定时备份只留最近 N 份（全局设置，默认 24；作者 11-29 定和手动备份分开计数，互不挤占）。
//
// 排法（内容面板 868 × 526，里面 852 × 510）：左边一栏任务列表、右边 300 宽一栏添加任务（都是圆角 18 = 26 − 8），最下面横跨两栏
// 一条 52 高、圆角 18 的保存条（同向导第三步 EULA 那条：开关在字的左边）。三块都不动，多了在列表里滚（细滚动条）。
//   · 列表：一行 40 高、圆角 10（离栏边 8）：类型图标、「每天 04:00」/「每隔 1 小时」、做什么（重启、备份，命令是等宽的原文），
//     右边一个未保存的小点和「···」（悬停那一行才出来，同「玩家」名单里的 ×）。不放文件夹的在上面（同程序），下面一个个文件夹：
//     文件夹那一行是图标、名字、条数，里面的任务缩进到和文件夹名对齐。下次什么时候执行、每隔多久的从 0 点起怎么算、备份只留几份，都进那一行的悬停。
//     服务器停着时到点不会执行的那几行（命令、重启、每隔多久的备份）淡下去，悬停说原因（程序的原话）；每天定时的备份照常。
//   · 「···」：移到文件夹（列出所有文件夹、不放文件夹、新文件夹…，现在在哪个打勾），删除。「新文件夹…」就地变成一个输入框，回车挪进去
//     （程序是弹一个确认框起名字）。挪走的那一行往右划掉、在新地方从下面淡进来；文件夹挪空了，那一行也划掉。
//   · 添加任务：最上面两颗预设（28 高胶囊）「每小时存档」「每小时扫地」，点一下就加进列表（同程序：先进草稿，保存以后生效）；
//     同样的已经有了（类型、间隔、命令一样，不管在哪个文件夹）就不再加，胶囊上的 ＋ 换成勾，点它是那一行闪一下（同「玩家」加重复名字）。
//     下面是表单（同向导第三步：字 18 + 6 + 框 40）：类型（分段：重启、命令、备份）、时间（分段：每天、每隔；每天是「时:分」框，
//     每隔是步进，一下一档）、命令（不是「命令」类型时灰着、悬停说只有命令要填）、文件夹（自己画的下拉，同「···」那张菜单），右下「添加」。
//   · 保存条：左边「崩溃自动重启」（说明进悬停）；右边有改动时淡进来「N 项修改未保存」「放弃修改」，「保存」亮起来。干净时只有灰着的「保存」，
//     保存条和列表的大小一像素不变。改动的那几行有个强调色小点（程序是「未保存」标签）。
// 程序的「5 分钟后」那三颗（作者以前的清单 A72：意思不清）这里拿掉了：它们只是把「每天」的时刻填成几分钟后，加出来的是一条天天执行的任务。
// 「每隔多久」和两颗预设管了反复做的事；要「过几分钟只做一次」得另加功能，写在汇报里等作者定。
// 作者 11-28 审完以后加的三样（程序都没有，改程序时照这里做）：
//   · 文件夹能收起、重命名、整个删除。文件夹那一行左边一大块是按钮，点了收起 / 展开（末尾的尖角跟着转；里面的行一起往上淡掉再收起高度，
//     展开是一行一行从下面淡进来）。收起只是看的样子，不算修改，保存、放弃都不动它；收起着、里面有没保存的任务时文件夹那一行亮小点；
//     往收起的文件夹里加、挪任务，或者加的那条已经在里面，先把它展开。右边「···」（悬停才出来，同任务行）：重命名、删除文件夹。
//     重命名是名字就地变成框（同「新建文件夹…」）：回车、点别处确定，Esc 不改；空着、和别的文件夹同名时回车拦下（框标红、晃一下、右边一句红字），
//     点别处就照原样放回去。后端的文件夹只是每条任务上的一个名字，所以重命名算「里面每条任务各挪了一次」（几条就是几项修改），改回原名就不算。
//     删除文件夹连同里面的任务一起删，先弹共用的确认框（KIT.confirm，危险），和别的改动一样进草稿、点保存才生效。
//   · 离开前拦未保存：登记 dirty / save / discard（见文件末尾），外壳在换标签、刷新、跳页前问「有未保存的修改」。
//   · 「每隔」步进中间的数点进去就地打字（KIT.numEdit）：框里带着单位（「1 小时」「90 分钟」），只填数字时照框里原来的单位算；
//     分钟数不在档位上的也收（后端收任意整数），之后点加减往那个方向落到最近一档。
// 示例数据：Paper 照任务单给的那份（每天 4 点重启、「存档」里每隔 1 小时和每天 3 点的备份、「扫地」里每隔 1 小时 kill @e[type=item]）；
// Forge 三条不放文件夹的（一条很长的 tellraw 看截断）；原版没有任务（看空的样子）；代理端一条每天重启。外壳的接口见 detail.html 的 content()。
(() => {
  // 程序图标里没有的：铅笔（重命名），画法同 AI 助手面板里那支（assistant.html）
  Object.assign(window.EXTRA_ICONS = window.EXTRA_ICONS || {}, { pencil: '<path d="M3 13l.5-2.6 7.7-7.7a1.2 1.2 0 011.7 0l.4.4a1.2 1.2 0 010 1.7L5.6 12.5z"/><path d="M9.9 4l2.1 2.1"/>' });
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  // 定时备份保留份数（作者 11-29 定和手动备份分开算）：全局设置、所有服务器共用，默认 24，没设过时按 24 和手动份数（默认 10）里大的算
  // （程序 app.ScheduledBackupKeepOf）；手动备份另有自己的份数，不占这里。草图参数 &keep= &skeep= 同「设置 → 存储位置」那页，随时读
  const qsN = k => { const n = parseInt(C.QS.get(k), 10); return n >= 1 && n <= 1000 ? n : 0; };
  const skeepN = () => qsN("skeep") || Math.max(24, qsN("keep") || 10);
  const TYPES = { restart: { name: "重启", ic: "refresh" }, command: { name: "命令", ic: "console" }, backup: { name: "备份", ic: "camera" } };
  const MIN_EVERY = { command: 5, backup: 30, restart: 60 }, MAX_EVERY = 720; // 同后端 everyMin、everyMax
  // 步进一下一档：只取能整除一天的（从 0 点起对齐，一天里每一段一样长）；数据里不在这几档上的（比如别处设的 100 分钟）照样能显示
  const STEPS = [5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240, 360, 480, MAX_EVERY];
  const SWEEP = "kill @e[type=item]";
  const PRESETS = [ // 字照程序（一键添加的两样，悬停的话也是程序的）
    { id: "save", label: "每小时存档", task: { type: "backup", every: 60, group: "存档" }, proxy: "代理端没有存档，不支持备份",
      tip: () => `每隔 1 小时备份一次存档，放入「存档」文件夹；服务器未运行时不执行。定时备份保留最近 ${skeepN()} 份（约为最近 ${span(skeepN() * 60)}，所有服务器共用，`
        + "可在「设置 → 存储位置」中修改），超出时自动删除最旧的定时备份；手动备份单独计数，不受影响" },
    { id: "sweep", label: "每小时扫地", task: { type: "command", every: 60, args: SWEEP, group: "扫地" }, proxy: "代理端不支持 kill 命令",
      tip: () => `每隔 1 小时执行 ${SWEEP}，清理地面掉落物，放入「扫地」文件夹；服务器未运行时不执行` },
  ];
  const SAMPLE = {
    // 和「世界与备份 → 备份」对得上（主会话 11-27 改）：Paper 只有每天 04:00 一个定时备份（备份列表里标「定时」的都是 04:00），Forge 没有定时备份
    paper: { crash: true, list: [{ type: "restart", at: "05:00" }, { type: "backup", at: "04:00", group: "存档" },
      { type: "command", every: 60, args: SWEEP, group: "扫地" }] },
    forge: { crash: false, list: [{ type: "command", at: "05:25", args: 'tellraw @a {"text":"服务器 5 分钟后自动重启，请先找个安全的地方下线","color":"yellow"}' },
      { type: "restart", at: "05:30" }] },
    vanilla: { crash: false, list: [] },
    proxy: { crash: true, list: [{ type: "restart", at: "05:00" }] },
  };
  const CRASH_TIP = "异常退出后依次间隔 10 秒、30 秒、60 秒自动重启；连续崩溃 3 次后停止重试并提示";
  const AT_TIP = "每天在指定时间执行一次（24 小时制）";
  const EVERY_TIP = "从每日 00:00 起对齐周期（如每隔 1 小时即整点执行）\n最短间隔：命令 5 分钟、备份 30 分钟、重启 1 小时；最长 12 小时，更长周期请选「每天」";

  // ---- 小工具 ----
  const pad2 = n => String(n).padStart(2, "0");
  const hm = m => pad2(Math.floor(m / 60)) + ":" + pad2(m % 60);
  const dur = m => (m % 60 ? `${m} 分钟` : `${m / 60} 小时`);
  // 步进里打的字：认「90」「90 分钟」「1.5 小时」「2h」；只填数字时照框里原来的单位算（unit：1 是分钟、60 是小时）。返回分钟数，认不出返回 NaN
  const parseDur = (v, unit) => {
    const m = String(v).replace(/\s/g, "").match(/^(\d+(?:\.\d+)?)(分钟?|小时|时|min|m|h)?$/i); if (!m) return NaN;
    return Math.round(+m[1] * (m[2] ? (/^(小时|时|h)$/i.test(m[2]) ? 60 : 1) : unit) * 1e6) / 1e6; // 1.1 小时是 66 分钟，别让浮点误差弄成不是整数
  };
  const whenText = t => (t.every ? `每隔 ${dur(t.every)}` : `每天 ${t.at}`);
  // 份数 × 间隔大约是多久（同 detail-world.js、settings-storage.js）：整天的写天，两天以内写小时（最多一位小数），再长四舍五入到天
  const span = m => m % 1440 === 0 ? `${m / 1440} 天` : m < 2880 ? `${+(m / 60).toFixed(1)} 小时` : `${Math.round(m / 1440)} 天`;
  // 认 4:00、04:00、0400、21：30 这几种写法，一律补齐成 HH:MM（同程序 parseHM）
  const parseHM = v => {
    const m = String(v).trim().match(/^(\d{1,2})[:：.](\d{1,2})$/) || String(v).trim().match(/^(\d{1,2})(\d{2})$/);
    return !m || +m[1] > 23 || +m[2] > 59 ? "" : pad2(+m[1]) + ":" + pad2(+m[2]);
  };
  // 同一条：类型、时刻、间隔、命令都一样，不管在哪个文件夹（同程序一键添加的判重）
  const what = t => JSON.stringify([t.type, t.at || "", t.every || 0, t.args || ""]);
  const same = (a, b) => what(a) === what(b);
  const keyOf = t => `t:${t.id}|${t.group || ""}`; // 列表里一行的键：挪了文件夹就是另一行，原处划掉、新地方淡进来
  const setTip = (el, text) => { if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip; };
  const shake = el => el.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });

  // ---- 数据：每台服务器一份（草图里切核心是换一台），换标签回来还在 ----
  let C = null, seq = 0; // C：外壳最近一次给的 ctx
  const DATA = {};
  function data() {
    const k = C.kind;
    if (!DATA[k]) {
      const s = SAMPLE[k] || SAMPLE.paper;
      const D = { crash: s.crash, list: s.list.map(t => ({ ...t, id: ++seq })), order: [], shut: new Set(), saving: false, // shut：收起着的文件夹（只是看的样子，不进基线）
        form: { type: "restart", mode: "at", at: "04:00", every: 60, args: "", group: "" } };
      fixOrder(D); commit(D);
      DATA[k] = D;
    }
    return DATA[k];
  }
  // 文件夹的先后：第一次出现时排到最后，之后不跟着任务挪来挪去变（免得一挪整个文件夹跳位置）；挪空了的去掉
  function fixOrder(D) {
    D.order = D.order.filter(g => D.list.some(t => t.group === g));
    D.list.forEach(t => { if (t.group && !D.order.includes(t.group)) D.order.push(t.group); });
    D.shut.forEach(g => { if (!D.order.includes(g)) D.shut.delete(g); }); // 文件夹没了就忘掉它收没收起（删掉以后又「放弃修改」找回来的，是展开着的）
  }
  // 存下来的样子（基线）：保存、放弃都照它。算改了几项同程序：一条任务的键是「类型、时刻、间隔、命令」加第几条一样的，值是在哪个文件夹——
  // 加一条、删一条各算一项，挪了文件夹算一项，删掉又加回一模一样的不算
  const keyed = list => { const n = {}; return list.map(t => { const k = what(t); n[k] = (n[k] || 0) + 1; return [k + "#" + n[k], t.group || ""]; }); };
  function commit(D) { D.base = { crash: D.crash, list: D.list.map(t => ({ ...t })), order: [...D.order], map: new Map(keyed(D.list)) }; }
  const drafts = D => new Set(keyed(D.list).flatMap(([k, g], i) => (D.base.map.get(k) !== g ? [D.list[i].id] : []))); // 还没保存的那几条（id）
  function dirtyCount(D) {
    const now = new Map(keyed(D.list));
    let n = D.crash !== D.base.crash ? 1 : 0;
    new Set([...D.base.map.keys(), ...now.keys()]).forEach(k => { if (D.base.map.get(k) !== now.get(k)) n++; });
    return n;
  }
  const stopped = () => C.st === "stopped";
  const idle = t => stopped() && (t.type !== "backup" || !!t.every); // 停着时到点也不会执行的
  function nextRun(t) {
    const d = new Date(), now = d.getHours() * 60 + d.getMinutes();
    if (t.at) { const [h, m] = t.at.split(":").map(Number); return `${h * 60 + m > now ? "今天" : "明天"} ${t.at}`; }
    const n = (Math.floor(now / t.every) + 1) * t.every;
    return n >= 1440 ? "明天 00:00" : `今天 ${hm(n)}`;
  }
  const slots = m => [0, m, 2 * m].filter(x => x < 1440).map(hm).join("、") + "……"; // 同程序
  // 定时备份那一行的悬停：只留几份、大约多久（每隔多久的才算得出；同程序 everyHint），手动备份另算
  const keepText = t => `定时备份保留最近 ${skeepN()} 份${t.every ? `（按此间隔约为最近 ${span(t.every * skeepN())}）` : ""}，超出时自动删除最旧的定时备份；手动备份单独计数，不受影响`;
  function rowTip(t) {
    const out = [idle(t) ? (t.type === "backup" ? "服务器未运行时不执行定时备份" : "服务器未运行时不执行") : `下次 ${nextRun(t)}`];
    if (t.every) out.push(`从每日 00:00 起算：${slots(t.every)}`);
    if (t.type === "backup") out.push(keepText(t));
    return out.join("\n");
  }

  // ---- 画 ----
  const taskRow = (t, draft) => {
    const cmd = t.type === "command", ty = TYPES[t.type];
    return `<div class="tk-row${t.group ? " in" : ""}${draft ? " draft" : ""}${idle(t) ? " idle" : ""}" role="listitem" data-key="${esc(keyOf(t))}" data-id="${t.id}" title="${esc(rowTip(t))}">`
      + `<i data-ic="${ty.ic}" class="tk-ti"></i><span class="tk-when">${whenText(t)}</span>`
      + `<span class="tk-what${cmd ? " mono" : ""}">${cmd ? esc(t.args) : ty.name}</span><span class="tk-dot" title="未保存"></span>`
      + `<button type="button" class="tk-more" data-tk="more" title="更多" aria-label="${esc(`${whenText(t)} ${cmd ? t.args : ty.name}`)}：移至文件夹、删除"><i data-ic="more"></i></button></div>`;
  };
  // 文件夹那一行：左边一大块是按钮（点了收起 / 展开，末尾的尖角跟着转），右边小点和「···」（重命名、删除文件夹；悬停才出来，同任务行）
  const folderInner = (g, n, open) => `<button type="button" class="tk-fh" data-tk="fold" aria-expanded="${open}"><i data-ic="folder" class="tk-fi"></i><span class="tk-fn">${esc(g)}</span>`
    + `<span class="tk-fc">${n}</span><i data-ic="chevronDown" class="tk-fv"></i></button><span class="tk-dot" title="未保存"></span>`
    + `<button type="button" class="tk-more" data-tk="fmore" title="更多" aria-label="${esc(`文件夹「${g}」`)}：重命名、删除"><i data-ic="more"></i></button>`;
  const folderRow = (g, n, shut, draft) => `<div class="tk-fd${shut ? " shut" : ""}${draft ? " draft" : ""}" role="listitem" data-key="${esc("f:" + g)}">${folderInner(g, n, !shut)}</div>`;
  // 列表里该有的每一行（键、怎么画、已经在的怎么只改变了的那几处）：不放文件夹的在上（同程序），文件夹按 D.order 排在下面
  function want(D) {
    const W = [], dr = drafts(D), task = t => W.push({ key: keyOf(t), make: () => taskRow(t, dr.has(t.id)), patch: el => patchTask(el, t, dr.has(t.id)) });
    D.list.filter(t => !t.group).forEach(task);
    D.order.forEach(g => {
      const ts = D.list.filter(t => t.group === g), shut = D.shut.has(g), draft = ts.some(t => dr.has(t.id));
      W.push({ key: "f:" + g, make: () => folderRow(g, ts.length, shut, draft), patch: el => patchFolder(el, ts.length, shut, draft) });
      if (!shut) ts.forEach(task); // 收起着的：里面的行不画
    });
    return W;
  }
  function patchFolder(el, n, shut, draft) {
    el.classList.toggle("shut", shut); el.classList.toggle("draft", draft); // 小点只在收起着时亮（样式里管），展开时各行自己有
    const c = el.querySelector(".tk-fc"), h = el.querySelector(".tk-fh"); if (!h) return; // 正在重命名：那一行里只有框，改完会整行重写
    if (c.textContent !== String(n)) c.textContent = n;
    if (h.getAttribute("aria-expanded") !== String(!shut)) h.setAttribute("aria-expanded", !shut);
  }
  function patchTask(el, t, draft) {
    el.classList.toggle("draft", draft);
    el.classList.toggle("idle", idle(t));
    setTip(el, rowTip(t));
  }
  function presetState(D, p) {
    if (C.S.proxy) return { off: true, tip: p.proxy };
    const had = D.list.find(t => same(t, p.task));
    return had ? { has: true, tip: `已存在该任务${had.group ? `（位于「${had.group}」文件夹）` : ""}` } : { tip: p.tip() };
  }
  const chip = (D, p) => {
    const s = presetState(D, p);
    return `<button type="button" class="tk-chip${s.has ? " has" : ""}" data-tk="preset" data-pre="${p.id}"${s.off ? ' aria-disabled="true"' : ""} title="${esc(s.tip)}"><i data-ic="${s.has ? "check" : "plus"}"></i>${p.label}</button>`;
  };
  // 分段选（同向导第三步：40 高、圆角 10，选中块离外框 4、圆角 6，滑过去）
  const seg = (name, opts, on) => `<div class="tk-seg" data-seg="${name}" role="radiogroup"><i class="tk-sgl"></i>${opts.map(([v, l, tip, off]) =>
    `<button type="button" role="radio" data-v="${v}" aria-checked="${v === on}" class="${v === on ? "on" : ""}"${off ? ' aria-disabled="true"' : ""}${tip ? ` title="${esc(tip)}"` : ""}>${l}</button>`).join("")}</div>`;
  const fld = (label, ctl) => `<div class="tk-fld"><span class="tk-lb">${label}</span>${ctl}</div>`;
  function whenCtl(D) {
    const F = D.form;
    if (F.mode === "at") return `<label class="tk-iw tk-at" title="24 小时制，如 04:00、21:30"><input class="tk-in" value="${esc(F.at)}" maxlength="5" inputmode="numeric" placeholder="时:分" autocomplete="off" spellcheck="false" aria-label="每日执行时间（时:分）">`
      + `<span class="tk-msg" title="请按 24 小时制填写时:分，如 04:00 或 21:30">时间无效</span></label>`;
    return `<div class="tk-stp"><button type="button" data-tk="ev-"><i data-ic="minus"></i></button><b class="tk-sv">${dur(F.every)}</b><button type="button" data-tk="ev+"><i data-ic="plus"></i></button></div>`;
  }
  const cmdOff = D => D.form.type !== "command";
  const cmdBox = D => `<label class="tk-iw tk-cmd${cmdOff(D) ? " off" : ""}"${cmdOff(D) ? ' title="仅「命令」类型需要填写"' : ""}>`
    + `<input class="tk-in mono" value="${esc(D.form.args)}" placeholder="${C.S.proxy ? "如 velocity reload" : "如 say 大家好"}" autocomplete="off" spellcheck="false" aria-label="命令内容"${cmdOff(D) ? " disabled" : ""}>`
    + `<span class="tk-msg" title="定时命令需要填写命令内容">请填写命令</span></label>`;
  const folderBtn = D => `<button type="button" class="tk-sel" data-tk="fsel" aria-haspopup="menu"><i data-ic="folder"></i>`
    + `<span class="tk-selv${D.form.group ? "" : " ph"}">${D.form.group ? esc(D.form.group) : "无文件夹"}</span><i data-ic="chevronDown" class="tk-chev"></i></button>`;
  function addForm(D) {
    const F = D.form, noBackup = C.S.proxy ? PRESETS[0].proxy : "";
    return `<section class="tk-add" aria-label="添加定时任务"><div class="tk-pre">${PRESETS.map(p => chip(D, p)).join("")}</div><div class="tk-hr"></div>`
      + fld("类型", seg("type", [["restart", "重启"], ["command", "命令"], ["backup", "备份", noBackup, !!noBackup]], F.type))
      + fld("时间", `<div class="tk-wrow">${seg("mode", [["at", "每天", AT_TIP], ["every", "每隔", EVERY_TIP]], F.mode)}<div class="tk-whenv">${whenCtl(D)}</div></div>`)
      + fld("命令", cmdBox(D)) + fld("文件夹", folderBtn(D))
      + `<div class="tk-foot"><button type="button" class="tk-addb" data-tk="add"><i data-ic="plus"></i>添加</button></div></section>`;
  }
  function bar(D) {
    const n = dirtyCount(D), on = n ? " on" : "";
    return `<div class="tk-bar"><button type="button" class="tk-tg${D.crash ? " on" : ""}" data-tk="crash" role="switch" aria-checked="${D.crash}" title="${CRASH_TIP}"><span class="tk-sw"></span>崩溃自动重启</button>`
      + `<span class="tk-sp"></span><span class="tk-n${on}" aria-live="polite"><i></i><span>${n} 项修改未保存</span></span>`
      + `<button type="button" class="tk-discard${on}" data-tk="discard"${n ? "" : ' tabindex="-1"'}>放弃修改</button>`
      + `<button type="button" class="tk-save${on}" data-tk="save"${n ? "" : ' aria-disabled="true" title="暂无未保存的修改"'}>保存</button></div>`;
  }
  function html(ctx) {
    C = ctx; const D = data();
    return `<div class="tk"><section class="tk-box" aria-label="定时任务"><div class="tk-lw${D.list.length ? "" : " is-none"}"><div class="list" role="list">`
      + want(D).map(w => w.make()).join("") + `</div><span class="thumb"></span><div class="tk-none">暂无定时任务</div></div></section>`
      + addForm(D) + bar(D) + `</div>`;
  }

  // ---- 挂上去 ----
  const root = () => document.querySelector(".cp > .tk:not(.fade-out)");
  const listEl = () => root()?.querySelector(".tk-lw .list");
  const paintList = () => root()?.querySelector(".tk-lw")?._paint?.();
  // 命令放不下被截断时，全文进悬停
  const fitWhat = row => { const w = row.querySelector(".tk-what.mono"); if (w) setTip(w, w.scrollWidth > w.clientWidth + 1 ? w.textContent : ""); };
  function placeSeg(box, animate) {
    const hl = box.querySelector(".tk-sgl"), on = box.querySelector("button.on"); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return; const D = data();
    ctx.thinBar(r.querySelector(".tk-lw"));
    r.querySelectorAll(".tk-seg").forEach(s => placeSeg(s, false));
    paintStepper(D);
    r.querySelectorAll(".tk-row").forEach(fitWhat);
    document.fonts?.ready.then(() => { if (!r.isConnected) return; r.querySelectorAll(".tk-row").forEach(fitWhat); r.querySelectorAll(".tk-seg").forEach(s => placeSeg(s, false)); });
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("keydown", onKey);
    r.addEventListener("focusout", onBlur);
    r.querySelector(".tk-lw .list").addEventListener("scroll", () => { if (menu && r.contains(menu._a)) closeMenu(); }, { passive: true });
  }

  // ---- 只换变了的那几行：按键对上已经在的行，只改它的几处；没有的新建、从下面淡进来；多出来的往右划掉 ----
  function sync(D, reveal) {
    const list = listEl(); if (!list) return;
    const live = [...list.children].filter(el => !el.classList.contains("tk-out"));
    const have = new Map(live.map(el => [el.dataset.key, el]));
    const W = want(D), keys = new Set(W.map(w => w.key));
    live.forEach(el => { if (!keys.has(el.dataset.key)) (inShut(D, el) ? fold : drop)(el); });
    let prev = null; const born = [];
    for (const w of W) {
      let el = have.get(w.key);
      if (el) {
        w.patch(el);
        if (prev && !(prev.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING)) prev.after(el); // 留下来的先后本来不会乱，保险
      } else {
        list.querySelectorAll(".tk-out").forEach(o => { if (o.dataset.key === w.key) o.remove(); }); // 同一行还在往外走（刚收起又展开）：旧的那个作废
        const tpl = document.createElement("template"); tpl.innerHTML = w.make(); el = tpl.content.firstElementChild;
        if (prev) prev.after(el); else list.prepend(el);
        C.KIT.hydrate(el); born.push(el);
      }
      prev = el;
    }
    born.forEach((el, i) => { grow(el, i); fitWhat(el); });
    paintNone(D); paintList();
    if (reveal) setTimeout(() => revealRow(reveal), 200);
  }
  // 新的一行：先撑开高度，再从下面 5 像素淡进来（0.22 秒），几行之间错开 22 毫秒
  function grow(el, i) {
    const h = el.offsetHeight + "px";
    el.animate([
      { height: "0px", marginTop: "-2px", opacity: 0, transform: "translateY(5px)", easing: SLIDE },
      { height: h, marginTop: "0px", opacity: 0, transform: "translateY(5px)", offset: .4, easing: EASE_OUT },
      { height: h, marginTop: "0px", opacity: 1, transform: "none" },
    ], { duration: 360, delay: i * 22, fill: "backwards" }).onfinish = paintList;
  }
  // 拿掉一行：往右划 32 像素淡掉（0.18 秒），再收起，下面的补上来（同「玩家」名单）
  function drop(el) {
    el.classList.add("tk-out");
    const h = el.offsetHeight + "px";
    el.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: SLIDE },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, fill: "forwards" }).onfinish = () => { el.remove(); paintList(); };
  }
  // 收起文件夹：里面的行一起往上 5 像素淡掉（0.09 秒），再收起高度；不往右划（那是「拿掉」）。展开时走上面的 grow
  const inShut = (D, el) => { const t = D.list.find(x => x.id === +el.dataset.id); return !!t && D.shut.has(t.group) && keyOf(t) === el.dataset.key; };
  function fold(el) {
    el.classList.add("tk-out");
    const h = el.offsetHeight + "px", o = getComputedStyle(el).opacity; // 刚展开还没淡完又收起：从眼下的样子接着走
    el.getAnimations().forEach(a => a.cancel());
    el.animate([
      { opacity: o, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateY(-5px)", height: h, marginBottom: "0px", offset: .3, easing: SLIDE },
      { opacity: 0, transform: "translateY(-5px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 300, fill: "forwards" }).onfinish = () => { el.remove(); paintList(); };
  }
  const rowOf = id => listEl()?.querySelector(`.tk-row[data-id="${id}"]:not(.tk-out)`);
  function revealRow(id) {
    const list = listEl(), el = rowOf(id); if (!el) return;
    const lr = list.getBoundingClientRect(), r = el.getBoundingClientRect();
    if (r.bottom > lr.bottom - 8) list.scrollBy({ top: r.bottom - lr.bottom + 8, behavior: "smooth" });
    else if (r.top < lr.top + 8) list.scrollBy({ top: r.top - lr.top - 8, behavior: "smooth" });
  }
  function flash(id) {
    const el = rowOf(id); if (!el) return;
    revealRow(id); el.classList.remove("tk-flash"); void el.offsetWidth; el.classList.add("tk-flash");
  }
  // 「暂无定时任务」：最后一行划掉以后才淡进来
  let noneT = 0;
  function paintNone(D) {
    const w = root()?.querySelector(".tk-lw"); if (!w) return;
    clearTimeout(noneT);
    if (D.list.length) w.classList.remove("is-none"); else noneT = setTimeout(() => { if (!data().list.length) w.classList.add("is-none"); }, 300);
  }
  function paintPresets(D) {
    root()?.querySelectorAll(".tk-chip").forEach(b => {
      const s = presetState(D, PRESETS.find(p => p.id === b.dataset.pre));
      setTip(b, s.tip);
      if (b.classList.contains("has") === !!s.has) return;
      b.classList.toggle("has", !!s.has); b.querySelector("svg").outerHTML = C.KIT.icon(s.has ? "check" : "plus");
    });
  }
  // 保存条：数字、两样淡进淡出、「保存」亮不亮；保存条本身的大小不变
  function paintBar(D) {
    const r = root(); if (!r) return;
    const n = dirtyCount(D), on = n > 0, t = r.querySelector(".tk-n"), x = r.querySelector(".tk-discard"), s = r.querySelector(".tk-save");
    if (on) t.lastElementChild.textContent = `${n} 项修改未保存`; // 变回 0 时旧的数跟着淡掉，不先换成 0
    t.classList.toggle("on", on); x.classList.toggle("on", on); x.tabIndex = on ? 0 : -1;
    if (D.saving) return;
    s.classList.toggle("on", on);
    if (on) { s.removeAttribute("aria-disabled"); setTip(s, ""); } else { s.setAttribute("aria-disabled", "true"); setTip(s, "暂无未保存的修改"); }
  }
  function paintCrash(D) { const b = root()?.querySelector(".tk-tg"); if (b) { b.classList.toggle("on", D.crash); b.setAttribute("aria-checked", D.crash); } }
  function paintCmd(D) {
    const w = root()?.querySelector(".tk-cmd"); if (!w) return;
    const off = cmdOff(D);
    w.classList.toggle("off", off); w.querySelector("input").disabled = off; setTip(w, off ? "仅「命令」类型需要填写" : "");
    if (off) w.classList.remove("bad");
  }
  function paintStepper(D) {
    const s = root()?.querySelector(".tk-stp"); if (!s) return;
    const F = D.form, lo = MIN_EVERY[F.type], v = s.querySelector(".tk-sv"), [mi, pl] = s.querySelectorAll("button");
    if (v.textContent !== dur(F.every)) v.textContent = dur(F.every);
    const down = STEPS.some(x => x < F.every && x >= lo), up = STEPS.some(x => x > F.every);
    mi.setAttribute("aria-disabled", !down); setTip(mi, down ? "缩短间隔" : `${TYPES[F.type].name}最短间隔为 ${dur(lo)}`);
    pl.setAttribute("aria-disabled", !up); setTip(pl, up ? "延长间隔" : `最长间隔为 ${dur(MAX_EVERY)}，更长周期请选「每天」`);
    // 中间的数点进去就地打字（kit.js 第 7 条）。最短间隔跟着类型走、步进又会整个重画（每天 ↔ 每隔），所以每次画都重新交一遍
    setTip(v, "点击输入间隔，如「90 分钟」「1.5 小时」");
    C.KIT.numEdit(v, { get: () => F.every, set: n => { F.every = n; paintStepper(D); }, min: lo, max: MAX_EVERY, int: true, label: "间隔",
      text: dur, parse: s => parseDur(s, F.every % 60 ? 1 : 60), hint: `请输入 ${dur(lo)}～${dur(MAX_EVERY)}之间的间隔，如「90 分钟」「1.5 小时」` });
  }
  function paintFolderBtn(D) {
    const v = root()?.querySelector(".tk-selv"); if (!v) return;
    v.textContent = D.form.group || "无文件夹"; v.classList.toggle("ph", !D.form.group);
  }
  const changed = (D, reveal) => { fixOrder(D); sync(D, reveal); paintBar(D); paintPresets(D); };

  // ---- 做事：草图里不发请求，改这里的数据、换变了的那几处 ----
  function insert(D, t) {
    const had = D.list.find(x => same(x, t));
    if (had) { // 同样的已经有了：不重复加，那一行闪一下（在收起的文件夹里就先展开，等那一行长出来再闪）
      if (D.shut.delete(had.group)) { sync(D); setTimeout(() => flash(had.id), 200); } else flash(had.id);
      return false;
    }
    t.id = ++seq; D.list.push(t); D.shut.delete(t.group); changed(D, t.id); // 加进收起的文件夹：先展开，不然看不见加到哪了
    return true;
  }
  function addFromForm(D) {
    const r = root(), F = D.form, t = { type: F.type };
    if (F.mode === "at") {
      const inp = r.querySelector(".tk-at input"), v = parseHM(inp.value);
      if (!v) { const w = inp.closest(".tk-iw"); w.classList.add("bad"); shake(w); inp.focus(); return; }
      inp.value = F.at = t.at = v;
    } else t.every = Math.max(F.every, MIN_EVERY[F.type]);
    if (F.type === "command") {
      const inp = r.querySelector(".tk-cmd input"), v = inp.value.trim();
      if (!v) { const w = inp.closest(".tk-iw"); w.classList.add("bad"); shake(w); inp.focus(); return; }
      t.args = v;
    }
    if (F.group) t.group = F.group;
    if (insert(D, t) && t.args) { r.querySelector(".tk-cmd input").value = F.args = ""; } // 加好以后命令框清空（同程序），别的留着方便接着加
  }
  function move(D, t, g) {
    if ((t.group || "") === g) return;
    D.list = D.list.filter(x => x !== t); // 挪到那个文件夹的最后
    if (g) t.group = g; else delete t.group;
    D.list.push(t); D.shut.delete(g); changed(D, t.id);
  }
  function remove(D, t) { D.list = D.list.filter(x => x !== t); changed(D); }
  // ---- 文件夹：收起 / 展开、重命名、整个删除（作者 11-28 定要；程序没有） ----
  const folderOf = el => el.closest(".tk-fd").dataset.key.slice(2);
  const folderEl = g => [...(listEl()?.children || [])].find(el => el.dataset.key === "f:" + g && !el.classList.contains("tk-out"));
  function toggleFold(D, g) { if (!D.shut.delete(g)) D.shut.add(g); sync(D); }
  // 重命名：名字就地变成框（同菜单里「新建文件夹…」那个框，最多 32 个字），图标和字的位置不动
  function renameFolder(D, g) {
    const fd = folderEl(g); if (!fd) return;
    fd.classList.add("edit"); listEl().classList.add("renaming"); // renaming：别的行先不接鼠标，见样式
    fd.innerHTML = `<label class="tk-fe"><i data-ic="folder"></i><input maxlength="32" value="${esc(g)}" autocomplete="off" spellcheck="false" aria-label="文件夹名称"><span class="tk-msg"></span></label>`;
    C.KIT.hydrate(fd); const inp = fd.querySelector("input"); inp.focus(); inp.select();
  }
  // how："enter" 回车、"blur" 点了别处、"esc"。空着、和别的文件夹同名：回车时拦下（框标红、晃一下、右边一句红字），点别处就照原样放回去（同步进打字）。
  // 同名不当成合并：新建时同名是「挪进那一个」，重命名撞上多半是手误，两个文件夹悄悄并成一个不好再分开
  function renameEnd(D, inp, how) {
    const box = inp.closest(".tk-fe"), fd = box.closest(".tk-fd"); if (box._done) return;
    const g = folderOf(fd), v = inp.value.trim();
    const bad = how === "esc" || v === g ? "" : !v ? "请填写名称" : D.order.includes(v) ? "已存在同名文件夹" : "";
    if (bad && how === "enter") { box.classList.add("bad"); box.querySelector(".tk-msg").textContent = bad; shake(box); return; }
    box._done = true; // 下面换掉这个框时还会来一次 blur
    const to = how === "esc" || bad ? g : v;
    if (to !== g) { // 行都还是原来那几行，只把键换成新名字的：不然 sync 会当成旧的全拿掉、新的全加上，整个文件夹划走再长出来
      D.list.forEach(t => { if (t.group !== g) return; const el = rowOf(t.id); t.group = to; if (el) el.dataset.key = keyOf(t); });
      D.order[D.order.indexOf(g)] = to; if (D.shut.delete(g)) D.shut.add(to);
      if (D.form.group === g) { D.form.group = to; paintFolderBtn(D); }
      fd.dataset.key = "f:" + to;
    }
    fd.classList.remove("edit"); listEl()?.classList.remove("renaming"); fd.innerHTML = folderInner(to, D.list.filter(t => t.group === to).length, !D.shut.has(to)); C.KIT.hydrate(fd);
    if (how !== "blur") fd.querySelector(".tk-fh").focus({ preventScroll: true });
    if (to !== g) changed(D);
  }
  // 整个删除：连同里面的任务。先问一句（危险，默认落在「取消」）；删了也只是进草稿，点保存才生效、「放弃修改」能找回来
  async function removeFolder(D, g) {
    const n = D.list.filter(t => t.group === g).length;
    const k = await C.KIT.confirm({ ic: "trash", tone: "dgr", title: `删除文件夹「${esc(g)}」？`, body: `将删除该文件夹及其中的 ${n} 项任务，保存后生效。`,
      btns: [{ k: "cancel", label: "取消" }, { k: "del", label: "删除", tone: "dgr" }] });
    if (k !== "del") return;
    D.list = D.list.filter(t => t.group !== g); changed(D);
  }
  function stepEvery(D, d) {
    const F = D.form, lo = MIN_EVERY[F.type];
    const next = d > 0 ? STEPS.find(x => x > F.every) : [...STEPS].reverse().find(x => x < F.every && x >= lo);
    if (next) { F.every = next; paintStepper(D); }
  }
  function pickSeg(D, btn) {
    if (btn.classList.contains("on") || btn.getAttribute("aria-disabled") === "true") return;
    const box = btn.parentElement, all = [...box.querySelectorAll("button")], dir = all.indexOf(btn) > all.findIndex(x => x.classList.contains("on")) ? 1 : -1;
    all.forEach(x => { x.classList.toggle("on", x === btn); x.setAttribute("aria-checked", x === btn); });
    placeSeg(box, true);
    const F = D.form;
    if (box.dataset.seg === "type") { F.type = btn.dataset.v; F.every = Math.max(F.every, MIN_EVERY[F.type]); paintCmd(D); paintStepper(D); }
    else { F.mode = btn.dataset.v; swapWhen(D, dir); }
  }
  // 每天 ↔ 每隔：时刻框和步进逐项换（旧的往反方向挪 5 像素淡掉 0.09 秒，新的从点的那一侧挪进来 0.22 秒）；连着点时上一次的作废
  function swapWhen(D, dir) {
    const box = root()?.querySelector(".tk-whenv"); if (!box) return;
    const tok = box._tok = (box._tok || 0) + 1, old = box.firstElementChild;
    const show = () => {
      if (box._tok !== tok) return;
      box.innerHTML = whenCtl(D); C.KIT.hydrate(box); paintStepper(D);
      box.firstElementChild.animate([{ opacity: 0, transform: `translateX(${5 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
    };
    const o = +getComputedStyle(old).opacity; old.getAnimations().forEach(a => a.cancel());
    old.animate([{ opacity: o }, { opacity: 0, transform: `translateX(${-5 * dir}px)` }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = show;
  }
  // 返回 Promise<存成了没有>：外壳离开前选「保存」时要等它（kit.js 第 6 条）。D.saving 平时是 false，正在存时是那个 Promise，再调给同一个
  function save(D) {
    if (D.saving) return D.saving;
    if (!dirtyCount(D)) return Promise.resolve(true);
    const s = root().querySelector(".tk-save"); s.innerHTML = `${C.KIT.icon("spinner", "tk-spin")}保存`;
    return D.saving = new Promise(done => setTimeout(() => { // 草图里装作等了一下请求
      D.saving = false; commit(D);
      const b = root()?.querySelector(".tk-save"); if (b) b.textContent = "保存";
      sync(D); paintBar(D); done(true);
    }, 450));
  }
  function discard(D) {
    if (!dirtyCount(D)) return;
    D.list = D.base.list.map(t => ({ ...t })); D.crash = D.base.crash; D.order = [...D.base.order];
    paintCrash(D); changed(D);
  }

  // ---- 菜单：同共用件页那张（外圆角 16、细边加内边距 5，项 36 高、圆角 10）。行上的「···」和表单的文件夹下拉共用 ----
  let menu = null;
  const menuItem = (attrs, ic, label, here) => `<div class="tk-it${here ? " here" : ""}" role="menuitem" ${attrs}><i data-ic="${ic}"></i><span>${esc(label)}</span>`
    + (here ? `<i data-ic="check" class="tk-ck"></i>` : "") + `</div>`;
  const newItem = `<div class="tk-it" role="menuitem" data-new><i data-ic="plus"></i><span>新建文件夹…</span></div>`;
  const folderItems = (names, cur) => names.map(g => menuItem(`data-mv="${esc(g)}"`, "folder", g, g === cur)).join("") + menuItem(`data-mv=""`, "doc", "无文件夹", !cur);
  function openMenu(anchor, inner, pick, under) {
    if (menu && menu._a === anchor) { closeMenu(); return; }
    closeMenu();
    const m = document.createElement("div"); m.className = "tk-dm enter"; m.setAttribute("role", "menu"); m.innerHTML = inner;
    document.body.appendChild(m); C.KIT.hydrate(m);
    const r = anchor.getBoundingClientRect();
    if (under) m.style.width = r.width + "px"; // 表单的下拉：和框一样宽、左边对齐
    const w = m.offsetWidth, h = m.offsetHeight, up = r.bottom + 4 + h > innerHeight - 8;
    m.classList.toggle("up", up);
    m.style.left = Math.round(Math.max(8, Math.min(under ? r.left : r.right - w, innerWidth - w - 8))) + "px";
    m.style.top = Math.round(up ? r.top - h - 4 : r.bottom + 4) + "px";
    anchor.classList.add("open"); m._a = anchor; m._pick = pick;
    m.addEventListener("click", onMenuClick); m.addEventListener("keydown", onMenuKey);
    menu = m;
  }
  function closeMenu() {
    if (!menu) return; const m = menu; menu = null;
    m._a.classList.remove("open"); m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  function onMenuClick(e) {
    const it = e.target.closest(".tk-it"); if (!it || !menu) return;
    if (it.hasAttribute("data-new")) { newFolderBox(it); return; }
    const pick = menu._pick; closeMenu();
    if (!it.classList.contains("here")) pick(it.dataset);
  }
  // 「新文件夹…」就地变成输入框（最多 32 个字）：回车挪进去，空着回车晃一下；和已有的同名就是挪进那一个
  function newFolderBox(it) {
    const box = document.createElement("label"); box.className = "tk-nf";
    box.innerHTML = `<i data-ic="folder"></i><input maxlength="32" placeholder="如 存档、扫地、活动提醒" autocomplete="off" spellcheck="false" aria-label="新建文件夹名称">`;
    it.replaceWith(box); C.KIT.hydrate(box); box.querySelector("input").focus();
  }
  function onMenuKey(e) {
    const inp = e.target.closest(".tk-nf input"); if (!inp || e.key !== "Enter" || !menu) return;
    e.preventDefault();
    const v = inp.value.trim(); if (!v) { const b = inp.closest(".tk-nf"); b.classList.add("bad"); shake(b); return; }
    const pick = menu._pick; closeMenu(); pick({ mv: v });
  }
  // 点在菜单和打开它的按钮以外就收起。用 composedPath：点「新文件夹…」时那一项已经换成输入框、不在菜单里了，路径里还有菜单
  document.addEventListener("click", e => { if (menu && !e.composedPath().some(n => n === menu || n === menu._a)) closeMenu(); });
  document.addEventListener("keydown", e => { if (e.key === "Escape") closeMenu(); });
  addEventListener("resize", () => { closeMenu(); root()?.querySelectorAll(".tk-seg").forEach(s => placeSeg(s, false)); });

  // ---- 事件（绑在这一页的根上，换标签时跟着内容一起扔掉） ----
  function onClick(e) {
    const D = data();
    const sb = e.target.closest(".tk-seg button"); if (sb) { pickSeg(D, sb); return; }
    const b = e.target.closest("[data-tk]"); if (!b || b.getAttribute("aria-disabled") === "true") return;
    switch (b.dataset.tk) {
      case "preset": { const p = PRESETS.find(x => x.id === b.dataset.pre); insert(D, { ...p.task }); break; }
      case "add": addFromForm(D); break;
      case "ev-": stepEvery(D, -1); break;
      case "ev+": stepEvery(D, 1); break;
      case "crash": D.crash = !D.crash; paintCrash(D); paintBar(D); break;
      case "discard": discard(D); break;
      case "save": save(D); break;
      case "fsel": {
        const names = [...D.order]; if (D.form.group && !names.includes(D.form.group)) names.push(D.form.group);
        openMenu(b, folderItems(names, D.form.group) + newItem, d => { D.form.group = d.mv; paintFolderBtn(D); }, true);
        break;
      }
      case "fold": toggleFold(D, folderOf(b)); break;
      case "fmore": {
        const g = folderOf(b);
        openMenu(b, menuItem("data-ren", "pencil", "重命名") + `<div class="tk-sep"></div><div class="tk-it tk-dgr" role="menuitem" data-del><i data-ic="trash"></i><span>删除文件夹</span></div>`,
          d => { if ("del" in d) removeFolder(D, g); else renameFolder(D, g); });
        break;
      }
      case "more": {
        const t = D.list.find(x => x.id === +b.closest(".tk-row").dataset.id); if (!t) return;
        openMenu(b, `<div class="tk-hd">移至文件夹</div>${folderItems(D.order, t.group || "")}${newItem}<div class="tk-sep"></div>`
          + `<div class="tk-it tk-dgr" role="menuitem" data-del><i data-ic="trash"></i><span>删除</span></div>`,
          d => { if ("del" in d) remove(D, t); else move(D, t, d.mv); });
        break;
      }
    }
  }
  function onInput(e) {
    const fe = e.target.closest(".tk-fe"); if (fe) { fe.classList.remove("bad"); return; }
    const w = e.target.closest(".tk-iw"); if (!w) return;
    const F = data().form; w.classList.remove("bad");
    if (w.classList.contains("tk-at")) F.at = e.target.value; else if (w.classList.contains("tk-cmd")) F.args = e.target.value;
  }
  function onKey(e) {
    if (e.target.closest(".tk-fe")) { // 文件夹重命名：回车确定（输入法选字的回车不算），Esc 不改
      if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); renameEnd(data(), e.target, "enter"); }
      else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); renameEnd(data(), e.target, "esc"); }
      return;
    }
    if (e.key === "Enter" && e.target.closest(".tk-iw")) { e.preventDefault(); addFromForm(data()); }
  }
  function onBlur(e) { // 离开时刻框时补齐成 HH:MM（同程序）；文件夹重命名的框点了别处算确定
    if (e.target.closest?.(".tk-fe")) { renameEnd(data(), e.target, "blur"); return; }
    if (!e.target.closest(".tk-at")) return;
    const v = parseHM(e.target.value); if (v) e.target.value = data().form.at = v;
  }

  // dirty / save / discard：外壳离开这一页之前用（kit.js 第 6 条）。项数就是保存条上的那个数；还没画过（C 是空的）算干净
  window.DETAIL_TABS.tasks = { html, mount, dirty: () => (C ? dirtyCount(data()) : 0), save: () => save(data()), discard: () => discard(data()) };
})();
