// 全局设置 →「启动与托盘」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-startup.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 5856 行 setStartup）：页顶标题「启动与托盘」和一句「关闭按钮的行为，以及开机是否自动启动。」，
// 一组两行（行首图标瓦片 monitor、power，标题、说明，右边开关）：「点关闭按钮时缩到托盘（不退出）」（config.minimizeToTray）、「开机自动启动」（info.autoStart，写注册表）。
// 两个开关都是点了即时保存（PUT /api/config），成功提示「已开启 / 已关闭」，失败提示「无法开启「…」」并把开关拨回去。
//
// 这里：内容面板里一栏（圆角 18 = 26 − 8，内边距 8），高度随内容（同「通知与集成」：拉满面板就是一大块空栏）。两行各 44 高、圆角 10（18 − 8），
// 开关 36 × 20 在字的左边，字只写程序的标题，说明进整行的悬停（规范第一节、第九节）。页顶那句说明不要了（左边分类已经写着「启动与托盘」）。
// 点了马上拨过去（同程序：即时保存），不弹提示（开关自己就是结果）。没写成功时开关拨回去、那一行晃一下，行尾一句短的红字，整句进悬停（规范第七节「反馈」）。
// 和程序不一样的字：托盘那一行的说明里「要退出，点左下角的「退出」」——侧栏没有了（计划书 U04），改成「右键托盘图标，或者在「更新与关于」最下面点「退出 MineDeck」」。
// 示例数据：缩到托盘开着、开机自启关着（任意挑的）。
// 草图专用：&fail=1 让「开机自动启动」打开时写注册表失败（看拦下来的样子）。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });

  const ROWS = [
    { k: "tray", label: "关闭窗口时最小化到托盘（不退出）",
      tip: "开启后，点击关闭按钮将最小化至系统托盘，程序与服务器继续运行；点击托盘图标可恢复窗口（首次最小化时将发送一次系统通知）。"
        + "如需退出，可右键托盘图标，或在「更新与关于」底部点击「退出 MineDeck」。关闭后，点击关闭按钮将停止服务器并退出程序（有运行中的服务器时会先确认）。" },
    { k: "auto", label: "开机自动启动",
      tip: "登录 Windows 后自动在系统托盘静默启动，无需管理员权限（若移动过程序文件夹，需重新切换此开关以更新启动路径）。" },
  ];
  let S = null; // 换分类回来不丢
  const seed = ctx => { if (!S) S = { tray: true, auto: false, fail: ctx.QS.get("fail") === "1" }; };

  const rowHTML = r => `<button type="button" class="su-row${S[r.k] ? " on" : ""}" data-k="${r.k}" role="switch" aria-checked="${S[r.k]}" title="${esc(r.tip)}">`
    + `<span class="su-sw"></span><span class="su-l">${esc(r.label)}</span><span class="su-msg"></span></button>`;
  function html(ctx) {
    seed(ctx);
    return `<div class="su"><section class="su-col" aria-label="启动与托盘">${ROWS.map(rowHTML).join("")}</section></div>`;
  }
  const root = () => document.querySelector(".cp > .su:not(.fade-out)");
  const paint = el => { const on = S[el.dataset.k]; el.classList.toggle("on", on); el.setAttribute("aria-checked", on); };

  function toggle(el) {
    const k = el.dataset.k, want = !S[k], label = ROWS.find(r => r.k === k).label;
    el.classList.remove("bad"); el.querySelector(".su-msg").removeAttribute("title");
    S[k] = want; paint(el); // 同程序：先拨过去，写不成再拨回来
    if (S.fail && k === "auto" && want) {
      setTimeout(() => {
        S[k] = false;
        const cur = root()?.querySelector(`[data-k="${k}"]`); if (!cur) return;
        paint(cur);
        const m = cur.querySelector(".su-msg"); m.textContent = "开启失败"; m.title = `无法开启「${label}」：写入开机启动项失败`;
        cur.classList.add("bad"); shake(cur);
      }, 420); // 装作等了一下请求
    }
  }
  function mount() {
    const r = root(); if (!r) return;
    r.addEventListener("click", e => { const el = e.target.closest(".su-row"); if (el) toggle(el); });
  }

  window.SETTINGS_SECS.startup = { html, mount };
})();
