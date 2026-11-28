// 全局设置 →「存储位置」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。
// 样式在 settings-storage.css，选择器都带 ss 前缀。
//
// 程序现在的样子（internal/web/ui/app.js setStorage，约 5965 行）：标题「存储位置」加一句长说明（改了马上生效，只对之后新建的服务器和新做的备份起作用，已经有的不会搬过去）；
// 一组三行：服务器存放文件夹、备份存放文件夹、程序数据文件夹，每行图标瓦片、名字、一句说明、等宽的路径，右边「打开」，前两行还有「更改…」和「恢复默认」（刷新图标）；
// 改了马上生效，右上角弹「已更改……，马上生效」带「撤销」（撤销换回改之前配置里的值，原来是默认就换回默认）。服务器和备份设成同一个文件夹时下面一块黄色提醒；
// 一句「更改存放文件夹只能在这台电脑上操作；用手机远程管理时，需要到主机上更改」。再一组「备份保留份数」：数字框（1~1000）、「份」、「保存」，存好弹一条提示。
// 11-27 夜里另一处程序改动（分支 worktree-agent-a07cf5b50bb05615d，还没合进 main）在第一组最后加了一行「Windows 安全中心排除项」：
// 状态（没加入 / 等系统确认… / 已加入 · 时间 / 加入的是以前的位置，有服务器放在程序数据文件夹外面时后面加「放在别处的服务器不在里面」）和「加入…」或「撤销…」，
// 点了由宿主以管理员身份跑 PowerShell（弹 Windows 的确认窗口），结果用提示说（点了「否」、组策略不让跑、Defender 没在工作、文件夹太大被拒……，字照 cmd/minedeck/defender_windows.go）。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：一栏占满面板（圆角 18 = 26 − 8，内边距 16 / 20），表单两列（各 398 宽、列距 16），照向导第三步「字 18 + 6 + 控件 40」，行距 24：
//   · 第一行：服务器存放文件夹 ｜ 备份存放文件夹。第二行：程序数据文件夹 ｜ Windows 安全中心排除项。一道细线。第三行左边：每台服务器保留几份备份。
//   · 路径框照向导「存储路径」：40 高、圆角 10，文件夹图标加等宽路径（放不下先省上级目录，文件夹名留着），整个框点了就是「更改…」（草图里不弹选文件夹的窗口，
//     假装选了一个）；框里右边两颗图标按钮（28 见方、离框边 6、圆角 4）：打开（外链图标，在资源管理器里打开）、恢复默认（刷新图标，已经是默认的时候灰着）。
//     程序数据文件夹不能改，框不能点，只有「打开」。各行的说明、「只能在这台电脑上操作」那句进悬停。
//   · 改了马上生效（同程序），右上角一条提示带「撤销」（同共用件页 commons.html 的那条：标题栏下面 12、离右边 16、360 宽，带按钮的停 20 秒，鼠标停在上面不走），
//     撤销换回改之前的值（原来是默认就换回默认），提示收掉，路径框换回去，不再弹第二条。
//   · 服务器和备份是同一个文件夹时，「备份存放文件夹」那一行右边一句黄字「服务器和备份放在了同一个文件夹」（会改变结果，留在页上；程序那段建议进悬停）。
//   · 排除项：一个和路径框一样的框（锁图标、状态、框里右边一颗 28 高的胶囊「加入…」或「撤销…」）；有服务器放在程序数据文件夹外面时，名字那一行右边一句淡字
//     「放在别处的服务器不在里面」（悬停列出是哪几台、哪几个文件夹）。点了以后框里换成「等系统确认…」、按钮转圈灰着，草图里等一秒多就成了。
//     程序里做完的结果是一条提示；这里写进框里的状态（「没有加入：在系统的确认窗口里点了「否」」「没能加入：组策略不让运行 PowerShell」……，原话全文和手动做的路径进悬停），
//     下一次点之前一直在，不会一闪就没。
//   · 每台服务器保留几份备份：步进（132 宽，一下一档，到头那边灰掉；档位 1~10 一档一个，再往上 12、15、20、24、30、48、72、100、168、200、300、500、1000，
//     现有值不在档位上时照原样写、一动落到相邻的档，同详情页「常用」；不带快捷预设，同详情页那些步进），后面一颗「保存」（同保存条那颗：没改时灰着，改了亮起来）；
//     改了没存时名字后面一个强调色小点。存好不弹提示（数字就在那儿）。说明进悬停，加了一句：定时任务每小时备份一次时，保留 N 份只够留最近 N 个小时（手动的也算）。
//     步进中间的数点进去就地打字（作者 11-28 定凡是步进都这样，共用 kit.js 的 KIT.numEdit）：1～1000 的整数，不在档位上的也收，之后点加减往那个方向落到最近一档。
//   · 离开前拦（作者 11-28 定，外壳统一拦，这里只登记 dirty / save / discard）：这一页只有保留份数是先改、点「保存」才生效的；
//     存放文件夹、排除项都是点了就生效，不在里面。
// 换分类回来：改过的都还在（数据在这个文件里）；正在等的「加入…」、挂着的「撤销」提示回来照样有效。
// 草图专用（左上角的切换条加不进，用地址参数）：
//   &def=added   已加入 · 2026-11-27 03:10，能「撤销…」
//   &def=moved   程序数据文件夹搬过：记着的是以前的位置（C:\Games\MineDeck），只给「撤销…」，撤的是旧位置
//   &def=broad   程序数据文件夹是磁盘根目录（D:\）：「加入…」灰着，悬停说为什么（程序里是点了才被宿主拒，事先灰掉要宿主多给一个字段，见返回的清单）
//   &def=denied  刚在 Windows 的确认窗口里点了「否」；&def=policy 组策略不让运行 PowerShell；&def=off Windows 安全中心没在工作
//                （这三种一打开就是「刚试过一次」的样子，再点「加入…」还是同样的结果）
//   &same=1      备份存放文件夹设成了和服务器同一个文件夹；&keep=13 保留份数一开始是 13（不在档位上）
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => {
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  const setText = (el, t) => { if (el.textContent !== t) el.textContent = t; };
  const setAttr = (el, k, v) => { if (el.getAttribute(k) !== String(v)) el.setAttribute(k, v); };

  const NAME = { servers: "服务器存放文件夹", backups: "备份存放文件夹" };
  const SUB = { servers: "servers", backups: "backups" };
  // 程序原话（setStorage 的各行说明、页头那句、手机远程那句）
  const DESC = {
    servers: "新建的服务器将保存在此文件夹中，每台对应一个子文件夹（中文名称不会直接用作文件夹名）。\n更改后立即生效，仅影响之后新建的服务器，已有服务器不会移动且可正常使用。",
    backups: "全部备份（含运行中、已停止及手动、定时备份）均保存在此文件夹中，每台服务器对应一个子文件夹。\n更改后立即生效，仅影响之后创建的备份，已有备份不会移动且可正常使用。",
    base: "存放程序本体、已下载的 Java 及缓存文件，可整个文件夹直接复制迁移。",
  };
  const REMOTE = "仅限本机操作；通过手机远程管理时需在主机上更改";
  const SAME_TIP = "虽可正常使用，但备份会与服务器文件混杂而不易区分；若磁盘损坏，服务器与备份将同时丢失。建议为备份另选独立文件夹，最好位于其他磁盘。";
  // 草图里「选文件夹」假装选到的（不弹系统的窗口）
  const PICKS = { servers: ["E:\\MC-Servers", "F:\\游戏\\开服"], backups: ["E:\\MineDeck-备份", "F:\\备份\\MineDeck"] };
  // 保留份数的档位（草图定的，见开头）
  const LAD = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 15, 20, 24, 30, 48, 72, 100, 168, 200, 300, 500, 1000];

  // 排除项的几句话（照 cmd/minedeck/defender_windows.go 和分支里 app.js 的原话）
  const DEF_TIP = base => "实时防护会扫这个文件夹里读写的每个文件，开服、存档会慢一些，frpc 这类穿透程序还常被误报隔离。"
    + `加入以后 Windows 安全中心不再扫描 ${base}，要管理员权限，会弹出 Windows PowerShell 的确认窗口。`
    + "放在别处的服务器和备份不在里面。装了别的杀毒软件的话，要在那个软件里另外设置。"
    + "MineDeck 只记得自己加过没有，在 Windows 安全中心里手动删掉的话，这里看不出来。";
  const manual = verb => `可在 Windows 安全中心手动${verb}：病毒和威胁防护 → 管理设置 → 排除项`;
  const RESULT = { // 点了以后没做成的几种：框里那句（短）、悬停（程序原话全文）
    cancelled: { warn: false, line: add => `${add ? "未加入" : "未撤销"}：在系统确认窗口中选择了「否」`, tip: () => "" },
    blocked: { warn: true, line: add => `${add ? "加入失败" : "撤销失败"}：组策略禁止运行 PowerShell`, tip: add => `本机组策略禁止运行 PowerShell，请在 Windows 安全中心手动${add ? "添加" : "删除"}。\n${manual(add ? "添加" : "删除")}` },
    unavailable: { warn: true, line: add => `${add ? "加入失败" : "撤销失败"}：Windows 安全中心未运行`, tip: () => "Windows 安全中心当前未运行，可能已安装其他杀毒软件，可在对应软件中将此文件夹设为信任。" },
  };
  const BROAD_TIP = "程序数据文件夹位于磁盘根目录或「下载」等系统文件夹中，整体排除存在安全风险。请先将 MineDeck 移至独立文件夹。";
  const P2 = n => String(n).padStart(2, "0");
  const fmtAt = d => `${d.getFullYear()}-${P2(d.getMonth() + 1)}-${P2(d.getDate())} ${P2(d.getHours())}:${P2(d.getMinutes())}`;

  let C = null, S = null;
  const norm = p => String(p || "").replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
  const sameDir = (a, b) => !!a && !!b && norm(a) === norm(b);
  const under = (p, base) => { const a = norm(p), b = norm(base); return a === b || a.startsWith(b + "\\"); };
  const join = (base, sub) => (base.endsWith("\\") ? base : base + "\\") + sub;
  function data() {
    if (S) return S;
    const q = C.QS, def = q.get("def") || "", broad = def === "broad", base = broad ? "D:\\" : "D:\\MineDeck";
    const k = parseInt(q.get("keep"), 10), keep = k >= 1 && k <= 1000 ? k : 10;
    const res = { denied: "cancelled", policy: "blocked", off: "unavailable" }[def];
    S = { base, broad, servers: "", backups: q.get("same") === "1" ? join(base, "servers") : "", keep, keepBase: keep, keepSaving: false,
      rec: def === "added" ? { path: base, at: "2026-11-27 03:10" } : def === "moved" ? { path: "C:\\Games\\MineDeck", at: "2026-11-02 21:47" } : null,
      pending: "", result: res ? { kind: res, action: "add" } : null, pt: 0 };
    return S;
  }
  const rootOf = (s, w) => s[w] || join(s.base, SUB[w]);
  const root = () => document.querySelector(".cp > .ss:not(.fade-out)");

  // ---- 路径框 ----
  function pathParts(p) { // 放不下时先省上级目录：前面是上级（淡），后面是文件夹名
    const i = p.replace(/\\$/, "").lastIndexOf("\\");
    return i <= 0 ? ["", p] : [p.slice(0, i), p.slice(i)];
  }
  const pathInner = p => { const [hd, tl] = pathParts(p); return `<span class="hd">${esc(hd)}</span><span class="tl">${esc(tl)}</span>`; };
  const pathTip = (s, w) => w === "base" ? s.base : `${rootOf(s, w)}\n点击更改文件夹（${REMOTE}）`;
  const resetTip = (s, w) => (s[w] ? `恢复默认文件夹（${join(s.base, SUB[w])}）` : "当前已是默认文件夹");
  function pathBox(s, w) {
    const pick = w !== "base", p = w === "base" ? s.base : rootOf(s, w);
    return `<div class="ss-pb${pick ? " pick" : ""}" data-w="${w}"${pick ? ` role="button" tabindex="0" data-ss="pick" aria-label="更改${NAME[w]}…"` : ""} title="${esc(pathTip(s, w))}">`
      + `<i data-ic="folder"></i><b class="ss-pt">${pathInner(p)}</b>`
      + `<button type="button" class="ss-ib" data-ss="open" data-w="${w}" title="打开文件夹" aria-label="打开${w === "base" ? "程序数据文件夹" : NAME[w]}"><i data-ic="share"></i></button>`
      + (pick ? `<button type="button" class="ss-ib" data-ss="reset" data-w="${w}" aria-disabled="${!s[w]}" title="${esc(resetTip(s, w))}" aria-label="恢复默认${NAME[w]}"><i data-ic="refresh"></i></button>` : "")
      + `</div>`;
  }

  // ---- 排除项 ----
  // 放在程序数据文件夹外面的：存放位置、放在别处的服务器（同分支里的 storageOutsideBase）
  function outside(s) {
    const list = [];
    ["servers", "backups"].forEach(w => { if (!under(rootOf(s, w), s.base)) list.push(`${NAME[w]}　${rootOf(s, w)}`); });
    (C.servers || []).forEach(v => { if (v.elsewhere && !under(v.elsewhere, s.base)) list.push(`${v.name}　${v.elsewhere}`); });
    return list;
  }
  function defView(s) {
    const here = s.rec && sameDir(s.rec.path, s.base);
    const v = { line: "未加入", warn: false, tip: "", btn: s.rec ? "撤销…" : "加入…", act: s.rec ? "remove" : "add", off: false,
      btip: s.rec ? "从排除项中移除（将弹出系统确认窗口）" : "以管理员身份加入（将弹出系统确认窗口）" };
    if (s.pending) return { ...v, line: "等待系统确认…", btn: s.pending === "add" ? "加入…" : "撤销…", busy: true, btip: "" };
    if (here) v.line = `已加入 · ${s.rec.at}`;
    else if (s.rec) { v.line = "当前排除项为旧位置"; v.tip = `此前加入的是 ${s.rec.path}，撤销的是它。`; }
    else if (s.broad) { v.line = "不能加入：程序数据文件夹是磁盘根目录"; v.warn = true; v.tip = BROAD_TIP; v.off = true; v.btip = BROAD_TIP; }
    if (s.result && !s.broad) { const R = RESULT[s.result.kind], add = s.result.action === "add"; v.line = R.line(add); v.warn = R.warn; v.tip = R.tip(add); }
    return v;
  }
  function defBox(s) {
    const v = defView(s);
    return `<div class="ss-db"><i data-ic="lock"></i><span class="ss-ds${v.warn ? " warn" : ""}"${v.tip ? ` title="${esc(v.tip)}"` : ""}>${esc(v.line)}</span>`
      + `<button type="button" class="ss-btn" data-ss="def" data-act="${v.act}" aria-disabled="${!!(v.off || v.busy)}"${v.btip ? ` title="${esc(v.btip)}"` : ""}>`
      + `${v.busy ? `<i data-ic="spinner" class="ss-spin"></i>` : ""}<span>${v.btn}</span></button></div>`;
  }
  const outTip = list => `以下文件夹不在排除项范围内：\n${list.join("\n")}`;

  // ---- 保留份数 ----
  const kTxt = n => `${n} 份`;
  const stepTo = (v, d) => (d > 0 ? LAD.find(x => x > v) : [...LAD].reverse().find(x => x < v));
  const keepTip = n => "创建新备份时，超出上限的最旧备份将自动删除（「还原前自动」备份计入上限）。此上限对所有服务器生效，修改后于下次备份时起效。\n"
    + `若定时任务设为每隔 1 小时备份一次，保留 ${n} 份约覆盖最近 ${n} 小时（手动备份同样计入其中）。`;
  function keepHTML(s) {
    const dn = stepTo(s.keep, -1) !== undefined, up = stepTo(s.keep, 1) !== undefined, dirty = s.keep !== s.keepBase;
    return `<div class="ss-f ss-keep${dirty ? " mod" : ""}"><div class="ss-lb"><span title="${esc(keepTip(s.keep))}">每台服务器备份保留份数</span><i class="ss-dot"></i></div><div class="ss-kr">`
      + `<div class="ss-stp"><button type="button" data-ss="dec" aria-label="少一档" aria-disabled="${!dn}" title="${dn ? "少一档" : "最少 1 份"}"><i data-ic="minus"></i></button>`
      + `<b class="ss-sv" aria-live="polite">${kTxt(s.keep)}</b><button type="button" data-ss="inc" aria-label="多一档" aria-disabled="${!up}" title="${up ? "多一档" : "最多 1000 份"}"><i data-ic="plus"></i></button></div>`      + `<button type="button" class="ss-save${dirty ? " on" : ""}" data-ss="ksave" aria-disabled="${!dirty}"${dirty ? "" : ' title="暂无未保存的修改"'}>保存</button></div></div>`;
  }

  function html(ctx) {
    C = ctx; const s = data(), out = outside(s), same = sameDir(rootOf(s, "servers"), rootOf(s, "backups"));
    return `<div class="ss"><section class="ss-col" aria-label="存储位置"><div class="ss-grid">`
      + `<div class="ss-f"><div class="ss-lb"><span title="${esc(DESC.servers)}">服务器存放文件夹</span></div>${pathBox(s, "servers")}</div>`
      + `<div class="ss-f"><div class="ss-lb"><span title="${esc(DESC.backups)}">备份存放文件夹</span><span class="ss-sp"></span>`
      + `<span class="ss-same"${same ? "" : " hidden"} title="${esc(SAME_TIP)}"><i data-ic="warn"></i>服务器与备份共用同一文件夹</span></div>${pathBox(s, "backups")}</div>`
      + `<div class="ss-f"><div class="ss-lb"><span title="${esc(DESC.base)}">程序数据文件夹</span></div>${pathBox(s, "base")}</div>`
      + `<div class="ss-f ss-def"><div class="ss-lb"><span title="${esc(DEF_TIP(s.base))}">Windows 安全中心排除项</span><span class="ss-sp"></span>`
      + `<span class="ss-out"${out.length ? "" : " hidden"} title="${esc(outTip(out))}">别处的服务器不在排除项内</span></div>${defBox(s)}</div>`
      + `<i class="ss-hr"></i>${keepHTML(s)}</div></section></div>`;
  }

  // =====================================================================================================
  // 只换动了的那一处
  // =====================================================================================================
  function paintPath(w) {
    const r = root(); if (!r) return; const s = data(), box = r.querySelector(`.ss-pb[data-w="${w}"]`); if (!box) return;
    const b = box.querySelector(".ss-pt"), html = pathInner(rootOf(s, w));
    if (b.innerHTML !== html) { // 路径逐项换：旧的往上 5 像素淡掉，新的从下面淡进来
      const tok = b._tok = (b._tok || 0) + 1;
      b.getAnimations().forEach(a => a.cancel());
      b.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
        if (b._tok !== tok) return;
        b.innerHTML = html; b.getAnimations().forEach(a => a.cancel());
        b.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: "cubic-bezier(.2, .7, .2, 1)" });
      };
    }
    setTip(box, pathTip(s, w));
    const rs = box.querySelector('[data-ss="reset"]'); if (rs) { setAttr(rs, "aria-disabled", !s[w]); setTip(rs, resetTip(s, w)); }
  }
  function paintSame() {
    const r = root(); if (!r) return; const s = data();
    r.querySelector(".ss-same").hidden = !sameDir(rootOf(s, "servers"), rootOf(s, "backups"));
  }
  function paintOut() {
    const r = root(); if (!r) return; const el = r.querySelector(".ss-out"), list = outside(data());
    el.hidden = !list.length; setTip(el, list.length ? outTip(list) : "");
  }
  function paintDef() {
    const r = root(); if (!r) return; const s = data(), v = defView(s), box = r.querySelector(".ss-db");
    const ln = box.querySelector(".ss-ds"), bt = box.querySelector(".ss-btn");
    if (ln.textContent !== v.line) { // 状态那句逐项换
      ln.getAnimations().forEach(a => a.cancel());
      ln.textContent = v.line; ln.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: "cubic-bezier(.2, .7, .2, 1)" });
    }
    ln.classList.toggle("warn", v.warn); setTip(ln, v.tip);
    setAttr(bt, "aria-disabled", !!(v.off || v.busy)); setAttr(bt, "data-act", v.act); setTip(bt, v.btip);
    const want = `${v.busy ? C.KIT.icon("spinner", "ss-spin") : ""}<span>${v.btn}</span>`;
    if (bt._h !== want) { bt._h = want; bt.innerHTML = want; }
  }
  function paintKeep() {
    const r = root(); if (!r) return; const s = data(), f = r.querySelector(".ss-keep");
    const [mi, pl] = f.querySelectorAll(".ss-stp button"), dn = stepTo(s.keep, -1) !== undefined, up = stepTo(s.keep, 1) !== undefined, dirty = s.keep !== s.keepBase;
    const num = f.querySelector(".ss-sv"); if (!num.querySelector(".kit-num")) setText(num, kTxt(s.keep)); // 正在框里打字时不动它（打完由 numEdit 放回去，再重画）
    setAttr(mi, "aria-disabled", !dn); setTip(mi, dn ? "少一档" : "最少 1 份");
    setAttr(pl, "aria-disabled", !up); setTip(pl, up ? "多一档" : "最多 1000 份");
    f.classList.toggle("mod", dirty);
    setTip(f.querySelector(".ss-lb > span"), keepTip(s.keep));
    const sv = f.querySelector(".ss-save");
    if (!s.keepSaving) { sv.classList.toggle("on", dirty); setAttr(sv, "aria-disabled", !dirty); setTip(sv, dirty ? "" : "暂无未保存的修改"); }
  }

  // 步进中间的数点进去就地打字（kit.js 第 7 条）：1～1000 的整数；不在档位上的也收，之后点加减由 stepTo 往那个方向落到最近一档
  const keepEdit = el => C.KIT.numEdit(el, { get: () => data().keep, set: n => { data().keep = n; paintKeep(); }, min: 1, max: 1000, int: true, label: "每台服务器备份保留份数" });
  // 存保留份数：草图里装作等了一下请求；存好不弹提示（同详情页「常用」的保存）。返回 Promise：外壳「离开前拦」选了保存时等它落定再走
  let keepP = null;
  function saveKeep() {
    const s = data();
    if (s.keepSaving) return keepP;
    if (s.keep === s.keepBase) return Promise.resolve(true);
    s.keepSaving = true; const sv = root()?.querySelector(".ss-save"); if (sv) sv.innerHTML = `${C.KIT.icon("spinner", "ss-spin")}保存`;
    return keepP = new Promise(ok => setTimeout(() => {
      s.keepSaving = false; s.keepBase = s.keep; const rr = root(); if (rr) rr.querySelector(".ss-save").textContent = "保存"; paintKeep(); ok(true);
    }, 450));
  }
  // 放弃没存的保留份数：回到存过的那个数
  function discardKeep() { if (!S) return; S.keep = S.keepBase; paintKeep(); }

  // =====================================================================================================
  // 改存放文件夹：马上生效，右上角一条带「撤销」的提示（同共用件页那条）
  // =====================================================================================================
  function change(w, value, msg) {
    const s = data(), prev = s[w];
    s[w] = value; paintPath(w); paintSame(); paintOut();
    toast(msg, { label: "撤销", aria: `撤销更改，恢复原${NAME[w]}`, run: () => { s[w] = prev; paintPath(w); paintSame(); paintOut(); } });
  }
  let tbox = null;
  function toast(msg, act) {
    if (!tbox || !tbox.isConnected) { tbox = Object.assign(document.createElement("div"), { id: "ss-toasts" }); tbox.setAttribute("aria-live", "polite"); document.body.appendChild(tbox); }
    const d = document.createElement("div"); d.className = "ss-tst enter";
    d.innerHTML = `<span class="ss-ti"><i data-ic="check"></i></span><span class="ss-tm">${esc(msg)}</span><button type="button" class="ss-tx" data-tx aria-label="关闭提示"><i data-ic="x"></i></button>`
      + `<span class="ss-ta"><button type="button" class="ss-tb" data-ta aria-label="${esc(act.aria)}">${esc(act.label)}</button></span>`;
    C.KIT.hydrate(d); tbox.appendChild(d);
    // 带按钮的停 20 秒（同程序 TOAST_LINGER_MS）；鼠标停在上面时不走，移开以后至少再留 4 秒
    let left = 20000, timer = 0, since = 0, gone = false;
    const kill = () => { if (gone) return; gone = true; clearTimeout(timer); leaveToast(d); };
    const run = () => { if (gone) return; since = Date.now(); timer = setTimeout(kill, left); };
    d.addEventListener("mouseenter", () => { clearTimeout(timer); left = Math.max(0, left - (Date.now() - since)); });
    d.addEventListener("mouseleave", () => { left = Math.max(left, 4000); run(); });
    d.addEventListener("click", e => {
      if (e.target.closest("[data-tx]")) { kill(); return; }
      if (e.target.closest("[data-ta]")) { act.run(); kill(); }
    });
    run();
  }
  // 走的时候往右 16 像素淡掉（0.14 秒），下面的几条补上去（0.32 秒，同共用件页）
  function leaveToast(d) {
    d.classList.remove("enter"); d.classList.add("leave");
    setTimeout(() => {
      const sibs = [...tbox.children].filter(x => x !== d), before = new Map(sibs.map(x => [x, x.getBoundingClientRect().top]));
      d.remove();
      sibs.forEach(x => { const dy = before.get(x) - x.getBoundingClientRect().top; if (Math.abs(dy) > .5) x.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: 320, easing: SLIDE }); });
    }, 140);
  }

  // ---- 排除项：草图里装作等系统确认了一会儿 ----
  function runDef(act) {
    const s = data(); if (s.pending) return;
    s.pending = act; paintDef();
    clearTimeout(s.pt);
    s.pt = setTimeout(() => {
      const kind = (C.QS.get("def") && { denied: "cancelled", policy: "blocked", off: "unavailable" }[C.QS.get("def")]) || "";
      s.pending = "";
      if (kind) s.result = { kind, action: act };   // 草图参数说的那种失败：每次都一样
      else {
        s.result = null;
        if (act === "add") s.rec = { path: s.base, at: fmtAt(new Date()) }; else s.rec = null;
      }
      paintDef();
    }, 1300);
  }

  // =====================================================================================================
  // 事件（绑在这一页的根上）
  // =====================================================================================================
  function onClick(e) {
    const r = root(), s = data(); if (!r) return;
    const b = e.target.closest("[data-ss]"); if (!b) return;
    if (b.getAttribute("aria-disabled") === "true") return;
    const w = b.dataset.w;
    switch (b.dataset.ss) {
      case "open": break; // 草图里不打开资源管理器
      case "reset": change(w, "", `已恢复默认${NAME[w]}`); break;
      case "pick": { // 草图里不弹选文件夹的窗口：假装选了下一个
        const cur = rootOf(s, w), next = PICKS[w].find(p => !sameDir(p, cur)) || PICKS[w][0];
        change(w, next, `已更改${NAME[w]}，立即生效`); break;
      }
      case "def": runDef(b.dataset.act); break;
      case "dec": case "inc": { const v = stepTo(s.keep, b.dataset.ss === "inc" ? 1 : -1); if (v !== undefined) { s.keep = v; paintKeep(); } break; }
      case "ksave": saveKeep(); break;
    }
  }
  function onKey(e) {
    if ((e.key === "Enter" || e.key === " ") && e.target.matches?.(".ss-pb.pick")) { e.preventDefault(); e.target.click(); }
  }
  // 框里的按钮点了不算点了框（不触发「更改…」）：上面 closest 先找到的是按钮本身，已经分开了

  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    r.addEventListener("click", onClick);
    r.addEventListener("keydown", onKey);
    const s = data();
    if (s.keepSaving) { const sv = r.querySelector(".ss-save"); sv.innerHTML = `${C.KIT.icon("spinner", "ss-spin")}保存`; } // 换回来时还在存
    keepEdit(r.querySelector(".ss-sv"));
  }

  // 离开前拦（kit.js 第 6 条）：只有保留份数是要点「保存」的；正在存的那一下不算没存（还没画过这一页时 S 是空的，算 0）
  window.SETTINGS_SECS.storage = { html, mount, dirty: () => (S && !S.keepSaving && S.keep !== S.keepBase ? 1 : 0), save: saveKeep, discard: discardKeep };
})();
