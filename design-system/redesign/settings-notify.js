// 全局设置 →「通知与集成」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-notify.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6213 行 setNotify）：页顶标题和一句说明（「服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件，可以推送到群机器人等外部服务。」），
// 一组一行：「Webhook 事件推送（选填）」，说明「有事件时，会向这个地址 POST 一条 JSON。留空就是关闭。」，地址框（占位「https://example.com/hook（留空关闭）」）+「保存」。
// 保存是 bindSaveInput：改过了「保存」才能点，回车也能存；存好了提示「Webhook 已保存，即时生效」，存空的提示「已关闭 Webhook 推送」。
// 后端（ops/catalog/appops.go）不收：不是 http(s):// 开头的（「Webhook 地址需以 http(s):// 开头」）、没有主机名的（「Webhook 地址的格式不对，要写成 https://主机名/路径 这样」）。
// 从局域网打开时只给地址的开头，后面换成 ***（maskWebhook），说明里多一句「从局域网打开时只显示地址的开头……」，点进框就全选；原样存回去算没改，
// 改了一半还带着 *** 的不收（「Webhook 地址里的 *** 是藏起来的部分，要改就把整个地址重新填一遍」）。
// 这一分类程序里就这一项：没有按事件分的开关，也没有「发一条测试」。这里照程序，不加。
//
// 这里：内容面板里一栏占满（内边距、字的样子同设置里「外观」「下载与网络」「存储位置」）。最上面一格：名字「Webhook 事件推送」（页顶那句、那一行的说明进它的悬停），
// 下面地址框占满栏宽（等宽字），
// 框里右边一颗保存（同详情页「扩展」单字段的框：没改时灰着，改过了亮起来，回车也能存，存好了换成勾 1.2 秒，不弹提示）。
// 后端不收的照规范第七节「反馈」就地拦下：框标红、晃一下、光标落过去，保存的位置换成一句短的红字，后端的原话进悬停。
// 离开前拦（作者 11-28 定，外壳统一拦，这里只登记 dirty / save / discard）：Webhook 地址改了没存就离开先问；选「保存」时后端不收的照样就地拦下、留在这页。
// 示例数据：存着 https://example.com/hook（任务单：画成示例地址，不写真实的服务地址）。
// 草图专用：&from=lan 看从局域网打开时（地址是 https://example.com/***，点进框全选；说明多那一句，也进悬停）。
// 没画的：读取失败。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s);
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });
  const setTip = (el, text) => {
    if (!el || (el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };

  const MASK = "***"; // 同后端 webhookMask
  const TIP = "支持将服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件推送至群机器人等外部服务。\n触发事件时将向该地址 POST 一条 JSON；留空即关闭推送。";
  const TIP_LAN = "\n通过局域网访问时仅显示地址前缀（路径与参数已隐藏），如需修改请重新填写完整地址。";
  // 同后端 maskWebhook：只留协议和主机，后面换成 ***
  const maskOf = u => { if (!u) return ""; try { const p = new URL(u); return p.protocol + "//" + p.host + "/" + MASK; } catch { return MASK; } };
  let N = null;
  function seed(ctx) {
    if (N) return;
    const lan = ctx.QS.get("from") === "lan", real = "https://example.com/hook";
    N = { lan, saved: lan ? maskOf(real) : real, draft: null }; // saved：框里该有的（局域网那边就是遮掉的样子）；draft：改了还没存的
  }

  function html(ctx) {
    seed(ctx);
    const v = N.draft ?? N.saved, dirty = N.draft !== null && N.draft !== N.saved;
    return `<div class="sn"><section class="sn-col" aria-label="通知与集成"><div class="sn-f"><span class="sn-lb"><span title="${esc(TIP + (N.lan ? TIP_LAN : ""))}">Webhook 事件推送</span></span>`
      + `<label class="sn-in${dirty ? " dirty" : ""}"><input value="${esc(v)}" placeholder="https://example.com/hook（留空关闭）" spellcheck="false" autocomplete="off" aria-label="Webhook 地址">`
      + `<span class="sn-msg"></span><button type="button" class="sn-ib" data-sn-act="save" title="保存"${dirty ? "" : " disabled"}><i data-ic="save"></i></button></label></div></section></div>`;
  }
  const root = () => document.querySelector(".cp > .sn:not(.fade-out)");
  function mount() {
    const r = root(); if (!r) return;
    r.addEventListener("click", e => { if (e.target.closest('[data-sn-act="save"]')) { e.preventDefault(); save(); } });
    r.addEventListener("keydown", e => { if (e.key === "Enter" && e.target.closest?.(".sn-in")) { e.preventDefault(); save(); } });
    r.addEventListener("input", e => { if (e.target.closest?.(".sn-in")) { N.draft = e.target.value; paint(); } });
    // 局域网打开：点进去就全选，输入就是整个换掉（同程序）
    r.addEventListener("focusin", e => { if (N.lan && e.target.matches?.(".sn-in input")) e.target.select(); });
  }
  function paint() {
    const box = root()?.querySelector(".sn-in"); if (!box) return;
    const dirty = N.draft !== null && N.draft !== N.saved, b = $(".sn-ib", box);
    box.classList.remove("bad"); box.classList.toggle("dirty", dirty);
    if (dirty) untick(b); // 勾还没收、又改了：马上换回保存
    b.disabled = !dirty;
  }
  // 存好了：保存图标换成勾 1.2 秒（规范第七节）
  function tick(b) {
    untick(b);
    b._snOld = b.innerHTML; b.classList.add("done"); b.innerHTML = window.KIT.icon("check");
    b._snT = setTimeout(() => untick(b), 1200);
  }
  function untick(b) {
    if (!b._snT) return;
    clearTimeout(b._snT); b._snT = 0; b.classList.remove("done"); b.innerHTML = b._snOld;
  }
  // 后端 url.Parse 以后要有主机名（webhookURLOK）。浏览器的 URL 会把 https:///x 认成主机 x，所以照 Go 的读法自己取：// 后面到第一个 / ? # 为止，去掉账号和端口
  function hostOf(u) {
    const m = u.match(/^https?:\/\/([^/?#]*)/i); if (!m) return "";
    const hp = m[1].split("@").pop();
    return hp.startsWith("[") ? hp.slice(1, hp.indexOf("]") > 0 ? hp.indexOf("]") : undefined) : hp.split(":")[0];
  }
  function bad(box, short, full) {
    box.classList.add("bad");
    const m = $(".sn-msg", box); m.textContent = short; setTip(m, full);
    shake(box); $("input", box)?.focus();
  }
  // 照后端 appops.go 那几道：草图里不发请求，改这里的数据、换框的样子。返回存成了没有（被拦下是 false）：外壳「离开前拦」选了保存时看它
  function save() {
    const box = root()?.querySelector(".sn-in"); if (!box) return true;
    const inp = $("input", box), u = inp.value.trim();
    if (N.draft === null || inp.value === N.saved) return true; // 没改：保存灰着，回车也不存（同 bindSaveInput）
    if (N.lan && u.includes(MASK)) { bad(box, "请填写完整地址", "地址中的 *** 为隐藏部分，修改时请重新填写完整地址"); return false; }
    if (u && !/^https?:\/\//.test(u)) { bad(box, "需以 http(s):// 开头", "Webhook 地址需以 http(s):// 开头"); return false; }
    if (u && (!hostOf(u) || /\s/.test(hostOf(u)))) { bad(box, "地址格式无效", "Webhook 地址格式无效，请按 https://主机名/路径 格式填写"); return false; }
    N.saved = N.lan ? maskOf(u) : u; N.draft = null; // 存空的就是关掉推送（程序提示「已关闭 Webhook 推送」，这里框空着、占位字写着「留空关闭」）
    if (inp.value !== N.saved) inp.value = N.saved;
    const b = $(".sn-ib", box);
    box.classList.remove("dirty", "bad"); b.disabled = true;
    tick(b);
    return true;
  }
  // 放弃没存的地址：草稿丢掉，框里换回存过的
  function discard() {
    if (!N) return; N.draft = null;
    const inp = root()?.querySelector(".sn-in input"); if (inp) inp.value = N.saved;
    paint();
  }

  // 离开前拦（kit.js 第 6 条）：这一页只有 Webhook 地址一项
  window.SETTINGS_SECS.notify = { html, mount, dirty: () => (N && N.draft !== null && N.draft !== N.saved ? 1 : 0), save, discard };
})();
