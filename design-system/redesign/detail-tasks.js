// 服务器详情页 →「定时任务」（11-27 第一稿，子代理照临时规范画的；计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 detail-tasks.css。
//
// 程序：internal/web/ui/app.js 的 view === "tasks"。11-27 夜里另一个分支给它加了「每隔多久」、文件夹和一键添加（作者转述的用户原话：
// 「想一键设置1个小时存一次档，1个小时扫一次地，再搞个任务分类文件夹，把扫地的指令单独放一个，存档的单独放一个。」），这一稿照那份画。
// 那一页从上到下：崩溃自动重启（开关加一段说明）；「一键添加」两颗；列表（不放文件夹的在上，文件夹按出现先后排在下面；每行类型徽标、
// 「每天 HH:MM」或「每隔 N」、命令、「未保存」、「移到…」「移除」）；添加一行（类型下拉、每天 / 每隔下拉、时:分框或间隔数字框加单位下拉、命令框、
// 文件夹框带候选、「添加」）；「5 分钟后」「10 分钟后」「30 分钟后」；吸底保存条「保存策略」。
// 后端 internal/inst/policy.go：类型 restart / command / backup；at（HH:MM）和 every（分钟）二选一，every 从每天 0 点起按钟点对齐
// （每隔 60 就是每个整点），最短命令 5、备份 30、重启 60，最长 720；group 是文件夹名（最多 32 字），只管分组显示，挪空了文件夹就没了。
// 到点时：命令、重启只在服务器开着时执行；每隔多久的备份停着时不做（世界没变），每天定时的备份照做。备份只留最近 N 份（全局设置，默认 10，手动的也算）。
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
// 示例数据：Paper 照任务单给的那份（每天 4 点重启、「存档」里每隔 1 小时和每天 3 点的备份、「扫地」里每隔 1 小时 kill @e[type=item]）；
// Forge 三条不放文件夹的（一条很长的 tellraw 看截断）；原版没有任务（看空的样子）；代理端一条每天重启。外壳的接口见 detail.html 的 content()。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const KEEP = 10; // 备份保留份数：全局设置，默认 10（internal/app/app.go DefaultBackupKeep），所有服务器共用，手动备份也算
  const TYPES = { restart: { name: "重启", ic: "refresh" }, command: { name: "命令", ic: "console" }, backup: { name: "备份", ic: "camera" } };
  const MIN_EVERY = { command: 5, backup: 30, restart: 60 }, MAX_EVERY = 720; // 同后端 everyMin、everyMax
  // 步进一下一档：只取能整除一天的（从 0 点起对齐，一天里每一段一样长）；数据里不在这几档上的（比如别处设的 100 分钟）照样能显示
  const STEPS = [5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240, 360, 480, MAX_EVERY];
  const SWEEP = "kill @e[type=item]";
  const PRESETS = [ // 字照程序（一键添加的两样，悬停的话也是程序的）
    { id: "save", label: "每小时存档", task: { type: "backup", every: 60, group: "存档" }, proxy: "代理端没有存档，不支持备份",
      tip: () => `每隔 1 小时备份一次存档，放入「存档」文件夹；服务器未运行时不执行。每台服务器保留最近 ${KEEP} 份（含手动备份，可在「设置 → 存储位置」中修改），超出时自动清理最旧的备份` },
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
  const AT_TIP = "每天到这个时刻执行一次（24 小时制）";
  const EVERY_TIP = "从每日 00:00 起对齐周期（如每隔 1 小时即整点执行）\n最短间隔：命令 5 分钟、备份 30 分钟、重启 1 小时；最长 12 小时，更长周期请选「每天」";

  // ---- 小工具 ----
  const pad2 = n => String(n).padStart(2, "0");
  const hm = m => pad2(Math.floor(m / 60)) + ":" + pad2(m % 60);
  const dur = m => (m % 60 ? `${m} 分钟` : `${m / 60} 小时`);
  const whenText = t => (t.every ? `每隔 ${dur(t.every)}` : `每天 ${t.at}`);
  const ago = m => (m < 2880 ? `${+(m / 60).toFixed(1)} 小时` : `${Math.round(m / 1440)} 天`);
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
      const D = { crash: s.crash, list: s.list.map(t => ({ ...t, id: ++seq })), order: [], saving: false,
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
  const keepText = t => `每台服务器保留最近 ${KEEP} 份（含手动备份）` + (t.every ? `，约 ${ago(t.every * KEEP)}前的备份将被自动清理` : "");
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
  const folderRow = (g, n) => `<div class="tk-fd" role="listitem" data-key="${esc("f:" + g)}"><i data-ic="folder" class="tk-fi"></i><span class="tk-fn">${esc(g)}</span><span class="tk-fc">${n}</span></div>`;
  // 列表里该有的每一行（键、怎么画、已经在的怎么只改变了的那几处）：不放文件夹的在上（同程序），文件夹按 D.order 排在下面
  function want(D) {
    const W = [], dr = drafts(D), task = t => W.push({ key: keyOf(t), make: () => taskRow(t, dr.has(t.id)), patch: el => patchTask(el, t, dr.has(t.id)) });
    D.list.filter(t => !t.group).forEach(task);
    D.order.forEach(g => {
      const ts = D.list.filter(t => t.group === g);
      W.push({ key: "f:" + g, make: () => folderRow(g, ts.length), patch: el => { const c = el.querySelector(".tk-fc"); if (c.textContent !== String(ts.length)) c.textContent = ts.length; } });
      ts.forEach(task);
    });
    return W;
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
    live.forEach(el => { if (!keys.has(el.dataset.key)) drop(el); });
    let prev = null; const born = [];
    for (const w of W) {
      let el = have.get(w.key);
      if (el) {
        w.patch(el);
        if (prev && !(prev.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING)) prev.after(el); // 留下来的先后本来不会乱，保险
      } else {
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
  }
  function paintFolderBtn(D) {
    const v = root()?.querySelector(".tk-selv"); if (!v) return;
    v.textContent = D.form.group || "无文件夹"; v.classList.toggle("ph", !D.form.group);
  }
  const changed = (D, reveal) => { fixOrder(D); sync(D, reveal); paintBar(D); paintPresets(D); };

  // ---- 做事：草图里不发请求，改这里的数据、换变了的那几处 ----
  function insert(D, t) {
    const had = D.list.find(x => same(x, t));
    if (had) { flash(had.id); return false; } // 同样的已经有了：不重复加，那一行闪一下
    t.id = ++seq; D.list.push(t); changed(D, t.id);
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
    D.list.push(t); changed(D, t.id);
  }
  function remove(D, t) { D.list = D.list.filter(x => x !== t); changed(D); }
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
  function save(D) {
    if (D.saving || !dirtyCount(D)) return;
    D.saving = true;
    const s = root().querySelector(".tk-save"); s.innerHTML = `${C.KIT.icon("spinner", "tk-spin")}保存`;
    setTimeout(() => { // 草图里装作等了一下请求
      D.saving = false; commit(D);
      const b = root()?.querySelector(".tk-save"); if (b) b.textContent = "保存";
      sync(D); paintBar(D);
    }, 450);
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
    const w = e.target.closest(".tk-iw"); if (!w) return;
    const F = data().form; w.classList.remove("bad");
    if (w.classList.contains("tk-at")) F.at = e.target.value; else if (w.classList.contains("tk-cmd")) F.args = e.target.value;
  }
  function onKey(e) { if (e.key === "Enter" && e.target.closest(".tk-iw")) { e.preventDefault(); addFromForm(data()); } }
  function onBlur(e) { // 离开时刻框时补齐成 HH:MM（同程序）
    if (!e.target.closest(".tk-at")) return;
    const v = parseHM(e.target.value); if (v) e.target.value = data().form.at = v;
  }

  window.DETAIL_TABS.tasks = { html, mount };
})();
