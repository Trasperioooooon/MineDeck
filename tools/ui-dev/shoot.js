// 截图：node shoot.js [pages|schemes|all]（默认 all）[--only=正则]
//   pages  ：逐页截图，深色 / 浅色各一套（列表、菜单与弹窗、空状态、向导各步、部署任务、详情各页、
//            未保存守卫、隧道、设置各分区（远程访问含二维码）、首启引导）
//   schemes：7 套配色 × 浅色 / 深色，各拍「服务器」与「设置 → 外观」两页
//   --only ：只拍名字匹配的，如 --only=wiz 或 --only="nord-dark"
// 图片存到本目录的 shots/<组名>/（已被 .gitignore 忽略），每次运行自带一个全新的仿真后端。
"use strict";
const { launch } = require("./browser");
const fs = require("fs");
const path = require("path");
const { start } = require("./server");

const args = process.argv.slice(2);
const GROUP = args.find(a => !a.startsWith("--")) || "all";
const onlyArg = args.find(a => a.startsWith("--only="));
const ONLY = onlyArg ? new RegExp(onlyArg.slice("--only=".length)) : null;
const OUT = path.join(__dirname, "shots");
const W = 1280, H = 850;
const SCHEMES = ["craft", "fluent", "github", "graphite", "nord", "catppuccin", "solarized"];
const INST = encodeURIComponent("生存服 · 朋友");

if (!["pages", "schemes", "all"].includes(GROUP)) {
  console.error("用法：node shoot.js [pages|schemes|all] [--only=正则]");
  process.exit(1);
}

// 模拟 WebView2 无边框宿主注入的窗口桥接函数（有它们，标题栏才显示窗口三键）
function hostStubs() {
  window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
  window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
}

async function shootPages(browser, mock) {
  const dir = path.join(OUT, "pages");
  fs.mkdirSync(dir, { recursive: true });
  let n = 0;
  const url = hash => mock.url + "?n=" + (++n) + hash;
  for (const theme of ["dark", "light"]) {
    const ctx = await browser.newContext({ viewport: { width: W, height: H }, locale: "zh-CN" });
    await ctx.addInitScript(t => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", t); }, theme);
    await ctx.addInitScript(hostStubs);
    const page = await ctx.newPage();
    page.on("pageerror", e => console.log(`[${theme}] 页面报错：`, e.message));
    const settle = (ms = 450) => page.waitForTimeout(ms);
    const want = name => !ONLY || ONLY.test(name);
    const shot = async (name, opts = {}) => {
      if (!want(name)) return;
      await settle();
      await page.screenshot({ path: path.join(dir, `${theme}-${name}.png`), ...opts });
    };
    const go = async (hash, name) => { if (want(name)) { await page.goto(url(hash)); await shot(name); } };
    const setState = s => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), s);

    await page.goto(mock.url);
    await setState({ empty: false, onboarded: true, taskStage: 2, listenLan: false });

    await go("#/instances", "01-instances");
    if (want("02-more-menu") || want("02-delete-modal")) {
      await page.goto(url("#/instances")); await settle();
      await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
      await shot("02-more-menu");
      await page.click(".dropmenu button.danger");
      await shot("02-delete-modal");
      await page.keyboard.press("Escape");
    }
    if (want("03-toast")) {
      await page.goto(url("#/instances")); await settle();
      await page.click('.card[data-name="机械动力整合包"] [data-act="start"]');
      await page.waitForTimeout(150);
      await page.screenshot({ path: path.join(dir, `${theme}-03-toast.png`), clip: { x: W - 520, y: 0, width: 520, height: 160 } });
    }
    await setState({ empty: true });
    await go("#/instances", "04-instances-empty");
    await setState({ empty: false });

    await go("#/create", "05-create");
    if (want("06-wiz")) {
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-new"); await shot("06-wiz-core");
      await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await shot("06-wiz-version");
      await page.click('.ver-item[data-id="1.21.4"]'); await page.click("#wnext"); await shot("06-wiz-build");
      await page.click("#wnext"); await shot("06-wiz-settings");
      await page.click("#wcreate"); await page.waitForTimeout(150); await shot("06-wiz-eula-error");
      // 原版的版本表来自 Mojang 清单，带发布日期副行
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-new"); await settle(250);
      await page.dblclick('.core-card[data-id="vanilla"]'); await shot("06-wiz-version-vanilla");
    }
    if (want("07-import")) {
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-import"); await shot("07-import");
    }
    await go("#/task/task-1", "08-task-running");
    await setState({ taskStage: "error" });
    await go("#/task/task-2", "08-task-error");
    await setState({ taskStage: 2 });

    for (const t of ["console", "players", "backups", "tasks", "res", "common", "raw", "jvm"]) {
      await go(`#/inst/${INST}/${t}`, `10-detail-${t}`);
    }
    if (want("10-detail-common-scrolled")) {
      await page.goto(url(`#/inst/${INST}/common`)); await settle();
      await page.evaluate(() => { const m = document.querySelector("#main"); m.scrollTop = m.scrollHeight; });
      await shot("10-detail-common-scrolled");
    }
    await go(`#/inst/${encodeURIComponent("机械动力整合包")}/console`, "11-detail-stopped");
    // 未保存修改：改一个开关 → 吸底保存栏高亮；再点别的标签 → 离开前拦截
    if (want("12-dirty")) {
      await page.goto(url(`#/inst/${INST}/common`)); await settle();
      await page.click('.prop-item:has([data-k="hardcore"]) .sw');
      await page.fill('[data-k="motd"]', "§b改过的 MOTD");
      await shot("12-dirty-savebar");
      await page.click('.tab[data-t="players"]');
      await shot("12-dirty-guard");
      await page.keyboard.press("Escape");
    }

    await go("#/tunnels", "20-tunnels");
    await setState({ listenLan: true }); // 远程访问开启后才显示二维码卡
    for (const s of ["appearance", "download", "storage", "java", "remote", "notify", "startup", "about"]) {
      await go(`#/settings/${s}`, `30-settings-${s}`);
    }
    await setState({ listenLan: false, onboarded: false });
    await go("#/instances", "40-onboarding");
    await setState({ onboarded: true });
    await ctx.close();
  }
}

async function shootSchemes(browser, mock) {
  const dir = path.join(OUT, "schemes");
  fs.mkdirSync(dir, { recursive: true });
  const ctx = await browser.newContext({ viewport: { width: W, height: H }, locale: "zh-CN" });
  await ctx.addInitScript(() => localStorage.setItem("amh_token", "mock"));
  await ctx.addInitScript(hostStubs);
  const page = await ctx.newPage();
  page.on("pageerror", e => console.log("页面报错：", e.message));
  let n = 0;
  for (const s of SCHEMES) for (const mode of ["light", "dark"]) for (const [pg, hash] of [["instances", "#/instances"], ["appearance", "#/settings/appearance"]]) {
    const name = `${s}-${mode}-${pg}`;
    if (ONLY && !ONLY.test(name)) continue;
    // URL 参数优先于本机记住的外观偏好（见 index.html），不会改写 localStorage
    await page.goto(`${mock.url}?n=${++n}&scheme=${s}&theme=${mode}&accent=auto${hash}`);
    await page.waitForTimeout(500);
    await page.screenshot({ path: path.join(dir, name + ".png") });
  }
  await ctx.close();
}

(async () => {
  const mock = await start();
  const browser = await launch();
  try {
    if (GROUP !== "schemes") await shootPages(browser, mock);
    if (GROUP !== "pages") await shootSchemes(browser, mock);
  } finally {
    await browser.close();
    await mock.close();
  }
  console.log(`截图已保存到 ${OUT}`);
})().catch(e => { console.error(e); process.exit(1); });
