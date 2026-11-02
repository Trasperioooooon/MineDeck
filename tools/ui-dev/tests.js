// 交互回归测试：node tests.js [正则]（只跑名字匹配的用例，如 node tests.js 外观）
// 自带一个全新的仿真后端（随机端口），在无头 Chromium 里操作界面并断言。全部通过时退出码为 0。
"use strict";
const { chromium } = require("playwright");
const { start } = require("./server");
const ONLY = process.argv[2] ? new RegExp(process.argv[2]) : null;
let ORIGIN, PORT; // 仿真后端启动后赋值
let n = 0;
const url = hash => ORIGIN + "?n=" + (++n) + hash;
const results = [];

async function run(name, fn) {
  if (ONLY && !ONLY.test(name)) return;
  try { await fn(); results.push(["PASS", name]); }
  catch (e) { results.push(["FAIL", name, e.message.split("\n")[0]]); }
}
const assert = (c, msg) => { if (!c) throw new Error(msg); };

(async () => {
  const mock = await start();
  ({ url: ORIGIN, port: PORT } = mock);
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 850 }, locale: "zh-CN" });
  await ctx.addInitScript(() => {
    localStorage.setItem("amh_token", "mock");
    localStorage.setItem("amh_theme", "dark");
    window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
    window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
  });
  const page = await ctx.newPage();
  const errors = [];
  // 页面报错带上第一处出错位置（文件:行:列），偶发的报错不用复现也能直接定位
  const pageErr = e => e.message + (((e.stack || "").split("\n").find(l => /^\s*at /.test(l)) || "").replace(/^\s*at\s*/, "  @ ").replace(/https?:\/\/[^/]+\//, ""));
  page.on("pageerror", e => errors.push(pageErr(e)));
  const reqs = [];
  page.on("request", r => reqs.push({ t: Date.now(), m: r.method(), u: new URL(r.url()).pathname, b: r.method() === "PUT" ? r.postData() || "" : "" }));
  // 某次之后有没有改过某个配置键（PUT /api/config 的请求体里带这个键）——原生窗口还会顺带上报界面底色，不能只看路径
  const putConfig = (since, key) => reqs.slice(since).some(r => r.m === "PUT" && r.u === "/api/config" && key in JSON.parse(r.b || "{}"));
  const settle = (ms = 400) => page.waitForTimeout(ms);
  const inst = encodeURIComponent("生存服 · 朋友");
  await page.goto(ORIGIN);
  await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ empty: false, onboarded: true, taskStage: 2 }) }));

  await run("切页后不残留轮询（快速切换 6 次后，停在隧道页 6 秒内不再请求 /api/instances）", async () => {
    await page.goto(url("#/instances")); await settle(600);
    errors.length = 0;
    // 每次切走都发生在上一视图的首个请求返回之前（mock 很快，故用 evaluate 同步连切）
    await page.evaluate(async () => {
      const hs = ["#/instances", "#/settings/java", "#/inst/" + encodeURIComponent("生存服 · 朋友") + "/console", "#/instances", "#/task/task-1", "#/tunnels"];
      for (const h of hs) { location.hash = h; await new Promise(r => setTimeout(r, 5)); }
    });
    await settle(800);
    const t0 = Date.now();
    await page.waitForTimeout(6000);
    const inWin = reqs.filter(r => r.t >= t0);
    const instReqs = inWin.filter(r => r.u === "/api/instances").length;
    const taskReqs = inWin.filter(r => r.u.startsWith("/api/tasks/")).length;
    const tunReqs = inWin.filter(r => r.u === "/api/tunnels").length;
    assert(instReqs === 0 && taskReqs === 0, `残留轮询：/api/instances ${instReqs} 次、/api/tasks ${taskReqs} 次（隧道页本应只轮询 /api/tunnels）`);
    assert(tunReqs >= 1 && tunReqs <= 2, `/api/tunnels 轮询次数异常：${tunReqs}`);
    assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
  });

  await run("toast 落在标题栏下方，不遮挡窗口控制键", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="机械动力整合包"] [data-act="start"]');
    await page.waitForSelector(".toast");
    const tb = await page.$eval(".toast", el => el.getBoundingClientRect().top);
    const hb = await page.$eval("#titlebar", el => el.getBoundingClientRect().bottom);
    assert(tb >= hb, `toast 顶 ${tb} < 标题栏底 ${hb}`);
  });

  await run("点击卡片空白处进入详情；点「···」不跳转并弹出菜单", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
    await settle(150);
    assert((await page.$(".dropmenu")) !== null, "菜单未弹出");
    assert(page.url().includes("#/instances"), "点「···」后发生了跳转");
    await page.keyboard.press("Escape");
    // 菜单离场有 120ms 动画：按下 Esc 立即进入离场（不再可点），动画结束后从页面移除
    assert((await page.$(".dropmenu:not(.leave)")) === null, "Esc 未关闭菜单");
    await page.waitForSelector(".dropmenu", { state: "detached", timeout: 1000 }).catch(() => {});
    assert((await page.$(".dropmenu")) === null, "菜单离场后未移除");
    await page.click('.card[data-name="机械动力整合包"] .inst-config');
    await settle();
    assert(decodeURIComponent(page.url()).includes("#/inst/机械动力整合包"), "点卡片未进入详情：" + page.url());
  });

  await run("运行中的服务器：菜单里的删除不可点并说明原因", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="生存服 · 朋友"] [data-act="more"]');
    await settle(150);
    const dis = await page.$eval(".dropmenu button.danger", b => b.disabled);
    const note = await page.$eval(".dropmenu .dm-note", el => el.textContent);
    assert(dis && note.includes("停止"), `disabled=${dis} note=${note}`);
    await page.keyboard.press("Escape");
  });

  await run("删除确认框默认聚焦「保留」，回车不会删除", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
    await page.click(".dropmenu button.danger");
    await settle(200);
    const focused = await page.evaluate(() => document.activeElement && document.activeElement.textContent.trim());
    assert(focused === "保留", "默认焦点在：" + focused);
    const before = reqs.length;
    await page.keyboard.press("Enter");
    await settle(200);
    assert(!reqs.slice(before).some(r => r.m === "DELETE"), "回车触发了 DELETE");
  });

  await run("弹窗打开时按 1-4 不会在底下切页", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
    await page.click(".dropmenu button.danger");
    await settle(150);
    await page.keyboard.press("3");
    await settle(150);
    assert(page.url().includes("#/instances"), "弹窗下切页到了：" + page.url());
    await page.keyboard.press("Escape");
  });

  await run("常用设置：改动后保存条显示未保存项数，切 tab 被拦下；「继续编辑」保留改动与地址", async () => {
    await page.goto(url(`#/inst/${inst}/common`)); await settle();
    assert(await page.$eval('[data-sb="save"]', b => b.disabled), "未改动时保存键应置灰");
    await page.click('.prop-item:has([data-k="hardcore"]) .sw');
    const st = await page.$eval(".sb-state", el => el.textContent);
    assert(st.includes("1 项修改未保存"), "保存条状态：" + st);
    await page.click('.tab[data-t="players"]');
    await settle(200);
    assert((await page.$(".modal")) !== null, "未弹出离开确认");
    await page.click('[data-x="cancel"]');
    await settle(200);
    assert(page.url().endsWith("/common"), "取消后地址变了：" + page.url());
    assert(await page.$eval('[data-k="hardcore"]', el => el.checked), "取消后改动丢失");
    // 再次离开并确认放弃 → 进入玩家页
    await page.click('.tab[data-t="players"]');
    await settle(200);
    await page.click('[data-x="ok"]');
    await settle(400);
    assert(page.url().endsWith("/players"), "确认放弃后未进入玩家页：" + page.url());
    assert((await page.$(".players-grid")) !== null, "玩家页未渲染");
  });

  await run("常用设置：保存后恢复干净状态，再切页不再拦截", async () => {
    await page.goto(url(`#/inst/${inst}/common`)); await settle();
    await page.click('.prop-item:has([data-k="hardcore"]) .sw');
    const before = reqs.length;
    await page.click('[data-sb="save"]');
    await settle(300);
    assert(reqs.slice(before).some(r => r.m === "PUT" && r.u.endsWith("/properties")), "未发送 PUT properties");
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("已保存"), "保存后状态未复位");
    await page.click('.tab[data-t="raw"]');
    await settle(300);
    assert((await page.$(".modal")) === null, "已保存仍被拦截");
    assert(page.url().endsWith("/raw"), "未切到全部配置");
  });

  await run("「放弃修改」按服务器值重绘", async () => {
    await page.goto(url(`#/inst/${inst}/jvm`)); await settle();
    await page.click("#jm-aikar");
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("未保存"), "填入 Aikar 后未标记未保存");
    await page.click('[data-sb="discard"]');
    await settle(400);
    assert((await page.$eval("#jm-jvm", el => el.value)) === "", "放弃后参数未还原");
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("已保存"), "放弃后状态未复位");
  });

  await run("定时任务：增删条目计入未保存，并在离开时拦截", async () => {
    await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
    await page.click('#sched-list [data-i="0"]');
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("1 项"), "删除定时任务后未标记");
    await page.click('#sidebar a[data-nav="settings"]');
    await settle(200);
    assert((await page.$(".modal")) !== null, "离开未拦截");
    await page.click('[data-x="ok"]');
    await settle(300);
  });

  await run("向导中途再点「新建服务器」/按 2 回到入口", async () => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await settle();
    await page.click('#sidebar a[data-nav="create"]'); await settle();
    assert((await page.$("#mode-new")) !== null, "点「新建服务器」未回到入口");
    await page.click("#mode-new"); await settle();
    await page.keyboard.press("2"); await settle();
    assert((await page.$("#mode-new")) !== null, "按 2 未回到入口");
  });

  await run("向导：双击核心直达选版本；默认预选最新正式版；完成步骤可点回", async () => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.dblclick('.core-card[data-id="fabric"]'); await settle();
    assert((await page.$("#ver-list")) !== null, "双击未进入版本步");
    const sel = await page.$eval("#ver-list .ver-item.sel", el => el.dataset.id);
    assert(sel === "26.2", "默认选中：" + sel);
    assert(!(await page.$eval("#wnext", b => b.disabled)), "预选后「下一步」应可点");
    assert((await page.$eval("#wf-sum", el => el.textContent)).includes("Fabric · 26.2"), "底栏回显不对");
    await page.click('.step-pill.done[data-step="0"]'); await settle();
    assert((await page.$("#core-grid .core-card.sel")) !== null, "点回第 1 步后选择丢失");
  });

  await run("向导：名称为空 / 端口非法 / 未同意 EULA 时就地报错，不发请求", async () => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.dblclick('.core-card[data-id="paper"]'); await settle();
    await page.click("#wnext"); await settle();
    await page.click("#wnext"); await settle();
    await page.fill("#f-name", "");
    await page.fill("#f-port", "70000");
    const before = reqs.length;
    await page.click("#wcreate"); await settle(200);
    assert(!reqs.slice(before).some(r => r.m === "POST" && r.u === "/api/instances"), "非法输入仍提交了");
    const errs = await page.$$eval(".field-err", els => els.map(e => e.textContent.trim()).filter(Boolean));
    assert(errs.length === 3, "错误条数：" + JSON.stringify(errs));
    assert((await page.$(".toast")) === null || !(await page.$eval(".toast", t => t.classList.contains("error"))), "仍弹了错误 toast");
    await page.fill("#f-name", "测试服");
    await page.fill("#f-port", "25570");
    await page.click(".eula-box .sw");
    await settle(100);
    const left = await page.$$eval(".field-err", els => els.map(e => e.textContent.trim()).filter(Boolean));
    assert(left.length === 0, "改正后错误未消失：" + JSON.stringify(left));
  });

  await run("部署任务首轮即结束时只提示一次成功", async () => {
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskStage: 4 }) }));
    await page.goto(url("#/task/task-9")); await page.waitForTimeout(2500);
    const toasts = await page.$$eval(".toast", ts => ts.map(t => t.textContent));
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskStage: 2 }) }));
    assert(toasts.filter(t => t.includes("创建成功")).length === 1, "成功提示次数：" + JSON.stringify(toasts));
  });

  await run("设置单字段：没改时保存键置灰，回车即保存", async () => {
    await page.goto(url("#/settings/download")); await settle();
    assert(await page.$eval("#cf-save", b => b.disabled), "未改动时保存键应置灰");
    await page.fill("#cf-key", "key-" + Date.now());
    assert(!(await page.$eval("#cf-save", b => b.disabled)), "改动后保存键应可点");
    const before = reqs.length;
    await page.press("#cf-key", "Enter"); await settle(300);
    assert(putConfig(before, "cfApiKey"), "回车未保存");
    assert(await page.$eval("#cf-save", b => b.disabled), "保存后应恢复置灰");
  });

  // ---- 外观 ----
  const rootVar = name => page.evaluate(n => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);
  const cssVal = async (sel, name) => {
    const css = await page.evaluate(() => fetch("/themes.css").then(r => r.text()));
    const block = css.split(sel)[1];
    return block && (block.match(new RegExp(name + ":\\s*([^;]+);")) || [])[1];
  };

  await run("外观：切换配色方案即时生效，刷新后保留", async () => {
    await page.goto(url("#/settings/appearance")); await settle();
    await page.click('[data-scheme-pick="nord"]'); await settle(200);
    const ds = await page.evaluate(() => [document.documentElement.dataset.scheme, localStorage.getItem("amh_scheme")]);
    assert(ds[0] === "nord" && ds[1] === "nord", "未生效 / 未持久化：" + ds);
    const theme = await page.evaluate(() => document.documentElement.dataset.theme);
    const want = await cssVal(`[data-scheme="nord"][data-theme="${theme}"]`, "--bg");
    assert((await rootVar("--bg")) === want, `--bg=${await rootVar("--bg")}，应为 ${want}`);
    assert((await page.$$eval(".scheme-card.on", els => els.map(e => e.dataset.schemePick))).join() === "nord", "选中卡不对");
    await page.reload(); await settle();
    assert((await page.evaluate(() => document.documentElement.dataset.scheme)) === "nord", "刷新后丢失");
  });

  await run("外观：强调色覆盖方案自带的强调色，「跟随方案」可还原", async () => {
    await page.goto(url("#/settings/appearance")); await settle();
    const theme = await page.evaluate(() => document.documentElement.dataset.theme);
    await page.click('[data-accent-pick="violet"]'); await settle(200);
    const v = await cssVal(`[data-accent="violet"][data-theme="${theme}"]`, "--accent-solid");
    assert((await rootVar("--accent-solid")) === v, `覆盖未生效：${await rootVar("--accent-solid")} ≠ ${v}`);
    await page.click('[data-accent-pick="auto"]'); await settle(200);
    const s = await cssVal(`[data-scheme="nord"][data-theme="${theme}"]`, "--accent-solid");
    assert((await rootVar("--accent-solid")) === s, `还原失败：${await rootVar("--accent-solid")} ≠ ${s}`);
  });

  await run("外观：侧栏快捷切换模式时，外观页选中态同步", async () => {
    await page.goto(url("#/settings/appearance")); await settle();
    const before = await page.evaluate(() => document.documentElement.dataset.themePref);
    await page.click("#theme-btn"); await settle(200);
    const after = await page.evaluate(() => document.documentElement.dataset.themePref);
    const on = await page.$eval(".seg button.on", b => b.dataset.mode);
    assert(before !== after && on === after, `before=${before} after=${after} 选中=${on}`);
  });

  await run("外观：存储了无效方案名时回退到默认「方块」", async () => {
    await page.evaluate(() => localStorage.setItem("amh_scheme", "bogus"));
    await page.goto(url("#/instances")); await settle();
    assert((await page.evaluate(() => document.documentElement.dataset.scheme)) === "craft", "未回退");
    await page.evaluate(() => { localStorage.removeItem("amh_scheme"); localStorage.setItem("amh_accent", "auto"); });
  });

  await run("外观：没有记忆时默认方案是「方块」，外观页第一张卡即「方块」且为选中态", async () => {
    await page.evaluate(() => localStorage.removeItem("amh_scheme"));
    await page.goto(url("#/settings/appearance")); await settle();
    assert((await page.evaluate(() => document.documentElement.dataset.scheme)) === "craft", "默认方案不是 craft");
    const first = await page.$eval(".scheme-card", b => [b.dataset.schemePick, b.classList.contains("on"), b.querySelector(".sc-name").textContent]);
    assert(first[0] === "craft" && first[1] && first[2] === "方块", "第一张方案卡：" + first);
    assert((await page.$$(".scheme-card")).length === 7, "方案卡数量不是 7");
    const theme = await page.evaluate(() => document.documentElement.dataset.theme);
    const want = await cssVal(`[data-scheme="craft"][data-theme="${theme}"]`, "--bg");
    assert((await rootVar("--bg")) === want, `--bg=${await rootVar("--bg")}，应为 ${want}`);
  });

  await run("局域网未登录：密码框保留，不被「无法连接」错误页覆盖；真故障仍显示错误页", async () => {
    const reply401 = route => route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ ok: false, error: "需要登录" }) });
    await page.route("**/api/app", reply401);
    try {
      await page.goto(url("#/instances")); await settle(600);
      assert((await page.$("#login-mask #login-pw")) !== null, "密码框不在");
      assert(!(await page.textContent("body")).includes("无法连接"), "被错误页覆盖");
    } finally { await page.unroute("**/api/app", reply401); }
    const reply502 = route => route.fulfill({ status: 502, contentType: "text/html", body: "<h1>Bad Gateway</h1>" });
    await page.route("**/api/app", reply502);
    try {
      await page.goto(url("#/instances")); await settle(600);
      assert((await page.textContent("body")).includes("无法连接"), "后端真故障时未显示错误页");
    } finally { await page.unroute("**/api/app", reply502); }
  });

  await run("远程访问：开启后显示二维码，默认家用网段地址，可切换其它地址", async () => {
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ listenLan: true }) }));
    try {
      await page.goto(url("#/settings/remote")); await settle();
      assert((await page.$("#qr-box svg.qr")) !== null, "没有二维码");
      const want = "http://192.168.1.23:" + PORT;
      assert((await page.textContent("#qr-url")) === want, "默认地址应为 192.168.x.x：" + await page.textContent("#qr-url"));
      assert((await page.getAttribute("#qr-box", "aria-label")).endsWith(want), "二维码缺少无障碍标签");
      const ips = await page.$$eval(".qr-ip", bs => bs.map(b => b.textContent));
      assert(ips.join() === "192.168.1.23,10.0.0.5,172.22.48.1", "地址排序不对：" + ips);
      await page.click('.qr-ip[data-i="1"]'); await settle(150);
      assert((await page.textContent("#qr-url")) === "http://10.0.0.5:" + PORT, "切换地址无效");
      assert((await page.$eval(".qr-ip.cur", b => b.textContent + "|" + b.getAttribute("aria-pressed"))) === "10.0.0.5|true", "当前地址按钮状态不对");
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ listenLan: false }) }));
    }
  });

  await run("向导：原版的版本列表带发布日期副行，其它核心不显示", async () => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.dblclick('.core-card[data-id="vanilla"]'); await settle();
    const first = await page.$eval("#ver-list .ver-item[data-id]", el => [el.dataset.id, el.querySelector(".ver-date")?.textContent]);
    assert(first[0] === "26.2" && first[1] === "2026-10-01", "日期副行不对：" + first);
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.dblclick('.core-card[data-id="paper"]'); await settle();
    assert((await page.$("#ver-list .ver-date")) === null, "无发布时间的核心不应显示日期");
  });

  await run("JVM 参数：老版本的 log4shell 缓解参数在套用预设 / 清空时保留，删掉后保存会被补回并回显", async () => {
    const FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml"];
    await page.evaluate(fix => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ legacy: { name: "机械动力整合包", extraJvm: ["-XX:+UseG1GC", ...fix], forceFlags: fix } }) }), FIX);
    const pk = encodeURIComponent("机械动力整合包");
    await page.goto(url(`#/inst/${pk}/jvm`)); await settle();
    const args = () => page.$eval("#jm-jvm", el => el.value.split(/\s+/).filter(Boolean));
    await page.click("#jm-aikar"); await settle(100);
    let a = await args();
    assert(a[0] === "-XX:+UseG1GC" && a.length > 10 && a.slice(-2).join() === FIX.join(), "套用预设后应为 Aikar 参数 + 缓解参数：" + a.slice(-3));
    await page.click("#jm-clear"); await settle(100);
    assert((await args()).join() === FIX.join(), "清空应只剩缓解参数：" + await args());
    // 手动删光再保存：后端补回，页面回显补回后的参数，且不再标记未保存
    await page.fill("#jm-jvm", "-XX:+UseZGC");
    await page.click('[data-sb="save"]'); await settle(600);
    a = await args();
    assert(a.join() === ["-XX:+UseZGC", ...FIX].join(), "未回显补回后的参数：" + a);
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("已保存"), "回显后仍标记未保存");
    const toasts = await page.$$eval(".toast", ts => ts.map(t => t.textContent).join("|"));
    assert(toasts.includes("已自动补回"), "缺少补回提示：" + toasts);
  });

  // ---- v5：启动画面 / 设置分组列表 / 减少动态 ----
  // 模拟原生 WebView2 宿主：窗口桥接函数 + 每次进程启动生成的 ID（真机由 main.go 用 WebView2 的文档创建脚本注入）
  const hostCtx = async (boot, opts = {}) => {
    const c = await browser.newContext({ viewport: { width: 1280, height: 850 }, locale: "zh-CN", ...opts });
    await c.addInitScript(b => {
      localStorage.setItem("amh_token", "mock");
      if (b.host) { window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {}; window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false; }
      if (b.boot) window.__MD_BOOT = b.boot;
    }, boot);
    return c;
  };
  const splashState = p => p.evaluate(() => ({ attr: document.documentElement.dataset.splash || "", el: !!document.getElementById("splash") }));

  await run("启动画面：原生窗口冷启动时播一次、约 1 秒内自动淡出；刷新不再播；局域网浏览器与「减少动态」不播", async () => {
    const c = await hostCtx({ host: true, boot: "boot-1" });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/instances");
      let st = await splashState(p);
      assert(st.attr === "play" && st.el, "冷启动未播放启动画面：" + JSON.stringify(st));
      assert((await p.$$eval("#splash-logo i[style]", els => els.length)) > 100, "像素方块没有画出来");
      await p.waitForFunction(() => !document.getElementById("splash"), null, { timeout: 2000 });
      assert((await p.evaluate(() => document.documentElement.dataset.splash)) === undefined, "淡出后未清除 data-splash");
      await p.reload(); await p.waitForTimeout(150);
      st = await splashState(p);
      assert(!st.attr && !st.el, "刷新后又播了启动画面：" + JSON.stringify(st));
    } finally { await c.close(); }
    for (const [what, boot, opts] of [["局域网浏览器（无宿主函数）", { host: false, boot: "boot-2" }, {}], ["减少动态", { host: true, boot: "boot-3" }, { reducedMotion: "reduce" }]]) {
      const c2 = await hostCtx(boot, opts);
      try {
        const p = await c2.newPage();
        await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(100);
        const st = await splashState(p);
        assert(!st.attr && !st.el, `${what}不应播放启动画面：` + JSON.stringify(st));
      } finally { await c2.close(); }
    }
  });

  await run("启动画面：点击或按任意键立即跳过，按键不会触发 1–4 快捷键", async () => {
    for (const how of ["click", "key"]) {
      const c = await hostCtx({ host: true, boot: "boot-skip-" + how });
      try {
        const p = await c.newPage();
        await p.goto(ORIGIN + "#/instances");
        await p.waitForSelector("#splash");
        const t0 = Date.now();
        if (how === "click") await p.click("#splash", { force: true }); else await p.keyboard.press("2");
        await p.waitForFunction(() => !document.getElementById("splash"), null, { timeout: 2000 });
        const ms = Date.now() - t0;
        assert(ms < 500, `${how} 跳过太慢：${ms}ms`);
        assert(p.url().endsWith("#/instances"), "跳过用的按键触发了切页：" + p.url());
      } finally { await c.close(); }
    }
  });

  await run("设置：分组列表——下载源单选行点选即时生效（打勾跟随、发 PUT），键盘可选；存储位置为带图标的分组", async () => {
    await page.goto(url("#/settings/download")); await settle();
    const rows = () => page.$$eval("#src-cards .row-item", els => els.map(e => e.dataset.v + ":" + e.getAttribute("aria-checked") + ":" + e.querySelector(".ri-check").classList.contains("on")));
    assert((await rows()).join() === "auto:true:true,mirror:false:false,official:false:false", "初始选中不对：" + await rows());
    let before = reqs.length;
    await page.click('#src-cards .row-item[data-v="mirror"]'); await settle(250);
    assert(putConfig(before, "source"), "点选未保存");
    assert((await rows()).join() === "auto:false:false,mirror:true:true,official:false:false", "点选后选中不对：" + await rows());
    await page.focus('#src-cards .row-item[data-v="official"]');
    before = reqs.length;
    await page.keyboard.press("Enter"); await settle(250);
    assert(putConfig(before, "source") && (await rows())[2] === "official:true:true", "键盘未选中：" + await rows());
    await page.click('#src-cards .row-item[data-v="auto"]'); await settle(250); // 复原
    await page.click('.set-nav[data-sec="storage"]'); await settle();
    const g = await page.$eval("#set-body .group", el => [el.classList.contains("has-ico"), el.querySelectorAll(".row-item").length, el.querySelectorAll(".ri-ico").length]);
    assert(g.join() === "true,3,3", "存储位置分组不对：" + g);
    assert((await page.$eval('.set-nav.cur', a => a.dataset.sec)) === "storage" && (await page.$eval("#set-eyebrow", el => el.textContent)) === "设置/存储位置", "导航当前项或眉标未更新");
    await page.click('.set-nav[data-sec="startup"]'); await settle();
    before = reqs.length;
    await page.click('.row-item:has([data-cfg="minimizeToTray"]) .sw'); await settle(250);
    assert(putConfig(before, "minimizeToTray"), "开关未即时保存");
    await page.click('.row-item:has([data-cfg="minimizeToTray"]) .sw'); await settle(250); // 复原
  });

  await run("状态切换：卡片胶囊换成新状态并播换场，状态行随之展开 / 收起，操作按钮随状态切换", async () => {
    const setStatus = st => page.evaluate(s => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ status: { "机械动力整合包": s } }) }), st);
    try {
      await page.goto(url("#/instances")); await settle(600);
      const card = '.card[data-name="机械动力整合包"]';
      assert(await page.$eval(card + " [data-stat]", el => el.classList.contains("closed")), "已停止的卡片不应显示状态行");
      await setStatus("starting");
      await page.dispatchEvent("#inst-q", "input"); // 触发重绘（等同下一轮轮询）
      await page.waitForSelector(card + " .st-pill.starting");
      const mid = await page.$eval(card, c => ({ out: !!c.querySelector(".st-label.out"), inl: c.querySelector(".st-label:not(.out)").classList.contains("in"), stop: !!c.querySelector('[data-act="stop"]') }));
      assert(mid.out && mid.inl, "胶囊文字没有换场：" + JSON.stringify(mid));
      assert(mid.stop, "启动中应显示「停止」");
      await page.waitForTimeout(400);
      const open = await page.$eval(card + " [data-stat]", el => [el.classList.contains("closed"), !!el.querySelector(".xp.warn.run")]);
      assert(!open[0] && open[1], "启动中状态行未展开：" + open);
      await setStatus("stopped");
      await page.dispatchEvent("#inst-q", "input");
      await page.waitForSelector(card + " .st-pill.stopped");
      await page.waitForTimeout(400);
      assert(await page.$eval(card + " [data-stat]", el => el.classList.contains("closed")), "停止后状态行未收起");
      assert((await page.$(card + ' [data-act="start"]')) !== null, "停止后应显示「启动」");
    } finally { await setStatus("stopped"); }
  });

  await run("冷启动白闪：原生窗口把标题栏底色上报存进配置，换配色后跟着更新；局域网浏览器不上报", async () => {
    const chromeOf = (p, scheme, theme) => p.evaluate(([s, t]) => fetch("/themes.css").then(r => r.text()).then(css =>
      (css.split(`[data-scheme="${s}"][data-theme="${t}"]`)[1].match(/--chrome:\s*([^;]+);/) || [])[1].trim()), [scheme, theme]);
    const cfg = p => p.evaluate(() => fetch("/api/app", { headers: { "X-Token": "mock" } }).then(r => r.json()).then(j => j.data.config.chromeColor));
    await page.evaluate(() => fetch("/api/config", { method: "PUT", headers: { "X-Token": "mock" }, body: JSON.stringify({ chromeColor: "" }) }));
    const c = await hostCtx({ host: true });
    try {
      const p = await c.newPage();
      await p.goto(ORIGIN + "#/settings/appearance"); await p.waitForTimeout(900);
      const theme = await p.evaluate(() => document.documentElement.dataset.theme);
      assert((await cfg(p)) === await chromeOf(p, "craft", theme), `启动后未上报默认方案的标题栏色：${await cfg(p)}`);
      await p.click('[data-scheme-pick="nord"]'); await p.waitForTimeout(800);
      assert((await cfg(p)) === await chromeOf(p, "nord", theme), `换配色后未更新：${await cfg(p)}`);
      const bad = await p.evaluate(() => fetch("/api/config", { method: "PUT", headers: { "X-Token": "mock" }, body: JSON.stringify({ chromeColor: "red" }) }).then(r => r.status));
      assert(bad === 400, "非法颜色应返回 400：" + bad);
      await p.click('[data-scheme-pick="craft"]'); await p.waitForTimeout(800);
    } finally { await c.close(); }
    const lan = await hostCtx({ host: false });
    try {
      const p = await lan.newPage();
      const puts = [];
      p.on("request", r => { if (r.method() === "PUT" && r.url().endsWith("/api/config")) puts.push(r.postData()); });
      await p.goto(ORIGIN + "?scheme=github#/instances"); await p.waitForTimeout(900);
      assert(!puts.some(b => b.includes("chromeColor")), "局域网浏览器不应上报界面底色：" + puts);
    } finally { await lan.close(); }
  });

  await run("减少动态：切页、菜单、弹窗、提示都不播动画（状态直接切换）", async () => {
    const c = await hostCtx({ host: true }, { reducedMotion: "reduce" });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      const running = () => p.evaluate(() => document.getAnimations().filter(a => a.playState === "running").length);
      await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(400);
      await p.click('.card[data-name="机械动力整合包"] [data-act="more"]');
      assert((await running()) === 0, "菜单进场仍有动画");
      await p.click(".dropmenu button.danger");
      assert((await running()) === 0, "弹窗进场仍有动画");
      await p.keyboard.press("Escape");
      assert((await p.$(".modal")) === null, "减少动态下弹窗应立即移除");
      await p.click('#sidebar a[data-nav="settings"]'); await p.waitForTimeout(300);
      await p.click('.set-nav[data-sec="appearance"]'); await p.waitForTimeout(300);
      await p.click('[data-mode="light"]');
      assert((await running()) === 0, "切页 / 键帽滑动仍有动画");
    } finally { await c.close(); }
  });

  await run("CurseForge Key 只写不读：已设置时输入框留空、占位写「已设置」，可清除；解不开时提示重填；导入页提示跟随 cfKeySet", async () => {
    const mockState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }), b);
    const appText = () => page.evaluate(() => fetch("/api/app", { headers: { "X-Token": "mock" } }).then(r => r.text()));
    const importHint = async () => {
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-import"); await settle();
      return page.evaluate(() => [...document.querySelectorAll(".hint")].some(h => h.textContent.includes("需要 API Key")));
    };
    const field = () => page.evaluate(() => ({ v: document.querySelector("#cf-key").value, ph: document.querySelector("#cf-key").placeholder,
      clear: !document.querySelector("#cf-clear").hidden, save: !document.querySelector("#cf-save").disabled, sub: document.querySelector("#cf-sub").textContent }));
    await mockState({ cfKey: "", cfKeyBroken: false });
    try {
      assert(await importHint(), "没设置 Key 时导入页应提示需要 API Key");
      await page.goto(url("#/settings/download")); await settle();
      let f = await field();
      assert(f.v === "" && f.ph === "粘贴 API Key" && !f.clear && !f.save, "未设置时：" + JSON.stringify(f));
      const key = "cf-test-key-" + Date.now();
      await page.fill("#cf-key", key);
      const before = reqs.length;
      await page.press("#cf-key", "Enter"); await settle(300);
      assert(putConfig(before, "cfApiKey"), "回车未保存");
      f = await field();
      assert(f.v === "" && f.ph.includes("已设置") && f.clear && !f.save, "保存后输入框应清空、占位写「已设置」、出现「清除」：" + JSON.stringify(f));
      const info = await appText();
      assert(!info.includes(key) && JSON.parse(info).data.cfKeySet === true, "/api/app 不应下发 Key，应给 cfKeySet：" + info);
      await page.reload(); await settle();
      f = await field();
      assert(f.v === "" && f.ph.includes("已设置") && f.clear, "重新打开后：" + JSON.stringify(f));
      assert(!(await importHint()), "已设置 Key 时导入页不应再提示");
      // 清除：危险操作多一步，确认框默认聚焦「取消」
      await page.goto(url("#/settings/download")); await settle();
      await page.click("#cf-clear"); await settle(200);
      assert((await page.evaluate(() => document.activeElement && document.activeElement.textContent.trim())) === "取消", "清除确认框应默认聚焦「取消」");
      const b2 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(300);
      assert(reqs.slice(b2).some(r => r.m === "PUT" && r.u === "/api/config" && JSON.parse(r.b || "{}").cfApiKey === ""), "清除应发 cfApiKey 空串");
      f = await field();
      assert(f.ph === "粘贴 API Key" && !f.clear && JSON.parse(await appText()).data.cfKeySet === false, "清除后：" + JSON.stringify(f));
      // 存着的 Key 解不开（换了电脑或 Windows 用户）
      await mockState({ cfKey: "old", cfKeyBroken: true });
      await page.goto(url("#/settings/download")); await settle();
      f = await field();
      assert(f.sub.includes("无法解开") && f.ph === "粘贴 API Key" && f.clear, "解不开时应提示重填：" + JSON.stringify(f));
    } finally { await mockState({ cfKey: "", cfKeyBroken: false }); }
  });

  await run("隧道：凭据解不开（换了电脑或 Windows 用户）时，这一行说明原因，优先于上次的公网地址", async () => {
    const mockState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }), b);
    await mockState({ credBroken: "t1" });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      const rows = await page.$$eval("#tun-list .java-row", els => els.map(e => e.textContent));
      assert(rows[0].includes("凭据无法解开") && !rows[0].includes("openfrp.top"), "解不开的隧道应显示原因：" + rows[0]);
      assert(!rows[1].includes("凭据无法解开"), "别的隧道不受影响：" + rows[1]);
    } finally { await mockState({ credBroken: "" }); }
  });

  // ---- 设置 → AI（设计文档 §4）：仿真后端的 /api/ai/* 与 ai.Service 同一套规则 ----
  const aiState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ ai: x }) }), b);
  const toastHas = (s, timeout = 2000) => page.waitForFunction(x => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes(x)), s, { timeout }).then(() => true, () => false);
  const aiForm = () => page.evaluate(() => {
    const q = s => document.querySelector(s);
    return { preset: q("#ai-preset").value, opts: [...q("#ai-preset").options].map(o => o.textContent), url: q("#ai-url").value, model: q("#ai-model").value,
      key: q("#ai-key").value, keyPh: q("#ai-key").placeholder, keySub: q("#ai-key-sub").textContent, clear: !q("#ai-key-clear").hidden,
      proto: !q(".ai-proto-row").hidden, effort: !q(".ai-effort-row").hidden, status: q("#ai-status").textContent,
      dirty: !!q(".save-bar.dirty"), test: !q("#ai-test").disabled, priceSrc: q("#ai-price-src").textContent };
  });
  const putBody = (since, path) => { const r = reqs.slice(since).filter(x => x.m === "PUT" && x.u === path).pop(); return r ? JSON.parse(r.b || "{}") : null; };
  const AI_DEEPSEEK = { preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash" };
  const AI_CLAUDE = { preset: "claude", protocol: "anthropic", baseUrl: "https://api.anthropic.com", model: "claude-opus-5-5" };

  await run("AI：没配置时不预选、不推荐，打开页面只读设置不联网；选预设填好地址和建议模型；Key 只写不读（保存后留空、占位写「已设置」）", async () => {
    await aiState({ reset: true });
    try {
      const t0 = reqs.length;
      await page.goto(url("#/settings/ai")); await settle();
      let f = await aiForm();
      assert(f.preset === "" && f.opts[0] === "未选择" && !f.opts.some(o => o.includes("推荐")), "没配置时不应预选或推荐：" + f.opts);
      assert(f.status.includes("未启用") && !f.test && !f.dirty && f.priceSrc.includes("才会下载"), "没配置时：" + JSON.stringify(f));
      const aiReqs = reqs.slice(t0).filter(r => r.u.startsWith("/api/ai/")).map(r => r.m + " " + r.u);
      assert(aiReqs.length > 0 && aiReqs.every(r => r === "GET /api/ai/settings"), "打开页面只应读设置：" + aiReqs);
      await page.selectOption("#ai-preset", "deepseek"); await settle(150);
      f = await aiForm();
      assert(f.url === "https://api.deepseek.com" && f.model === "deepseek-v4-flash" && !f.proto && !f.effort && f.dirty, "选 DeepSeek 后：" + JSON.stringify(f));
      const key = "sk-ai-test-" + Date.now();
      await page.fill("#ai-key", key);
      const t1 = reqs.length;
      await page.click('[data-sb="save"]'); await settle(300);
      const b = putBody(t1, "/api/ai/provider");
      assert(b && b.key === key && b.preset === "deepseek" && b.baseUrl === "https://api.deepseek.com", "保存应带上新 Key：" + JSON.stringify(b));
      f = await aiForm();
      assert(f.key === "" && f.keyPh.includes("已设置") && f.clear && !f.dirty && f.status.includes("已启用") && f.test, "保存后：" + JSON.stringify(f));
      assert(!f.opts.includes("未选择"), "配过之后不应再有「未选择」（停用走「停用 AI」）");
      const view = await page.evaluate(() => fetch("/api/ai/settings", { headers: { "X-Token": "mock" } }).then(r => r.text()));
      assert(!view.includes(key) && JSON.parse(view).data.keySet === true, "设置接口不应下发 Key：" + view.slice(0, 160));
      await page.reload(); await settle();
      f = await aiForm();
      assert(f.key === "" && f.keyPh.includes("已设置") && f.preset === "deepseek" && !(await page.content()).includes(key), "重新打开后：" + JSON.stringify(f));
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：同一网站只改路径 Key 照旧；换到别的网站先提示、保存要确认（取消不存），确认后明着清 Key；清除 Key 要确认；停用 AI 清掉服务商设置", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-old" });
    try {
      await page.goto(url("#/settings/ai")); await settle();
      let f = await aiForm();
      assert(f.keyPh.includes("已设置") && f.clear, "已设置 Key：" + JSON.stringify(f));
      // 同一个网站只改路径（测试报告问题 1）：不提示、不确认，保存后 Key 还在
      await page.fill("#ai-url", "https://api.deepseek.com/nonexistent-path"); await settle(100);
      f = await aiForm();
      assert(!f.keySub.includes("原来的 Key 会清掉") && f.keyPh.includes("已设置") && f.dirty, "同一网站改路径：" + JSON.stringify(f));
      let t1 = reqs.length;
      await page.click('[data-sb="save"]'); await settle(300);
      assert(!(await page.$(".modal")) && !("key" in (putBody(t1, "/api/ai/provider") || {})), "同一网站改路径不该确认、不该带 key");
      f = await aiForm();
      assert(f.status.includes("已启用") && f.keyPh.includes("已设置"), "同一网站改路径保存后 Key 还在：" + JSON.stringify(f));
      // 换到别的网站：提示；保存先确认，取消就不存
      await page.fill("#ai-url", "https://relay.example/v1"); await settle(100);
      f = await aiForm();
      assert(f.keySub.includes("原来的 Key 会清掉") && !f.clear && f.keyPh === "粘贴 API Key" && f.dirty, "换网站后应提示：" + JSON.stringify(f));
      t1 = reqs.length;
      await page.click('[data-sb="save"]'); await settle(200);
      assert(await page.$(".modal"), "会清掉 Key 时保存要先确认");
      await page.click('.modal [data-x="cancel"]'); await settle(200);
      f = await aiForm();
      assert(!putBody(t1, "/api/ai/provider") && f.dirty && f.status.includes("已启用"), "取消后不该保存：" + JSON.stringify(f));
      await page.click('[data-sb="save"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(300);
      assert((putBody(t1, "/api/ai/provider") || {}).key === "", "确认后应明着发 key 空串（操作记录里看得出清过）");
      f = await aiForm();
      assert(f.status.includes("未启用") && f.status.includes("填写 Key") && f.keyPh === "粘贴 API Key" && !f.clear && !f.test, "保存后旧 Key 应清掉：" + JSON.stringify(f));
      assert(await toastHas("还差：填写 Key"), "应提示还差什么");
      await page.fill("#ai-key", "sk-new"); await page.click('[data-sb="save"]'); await settle(300);
      f = await aiForm();
      assert(f.status.includes("已启用") && f.keyPh.includes("已设置"), "重填后：" + JSON.stringify(f));
      // 清除 Key：危险操作多一步，确认框默认聚焦「取消」
      await page.click("#ai-key-clear"); await settle(200);
      assert((await page.evaluate(() => document.activeElement && document.activeElement.textContent.trim())) === "取消", "清除确认框应默认聚焦「取消」");
      t1 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(300);
      assert((putBody(t1, "/api/ai/provider") || {}).key === "", "清除应发 key 空串");
      f = await aiForm();
      assert(f.keyPh === "粘贴 API Key" && !f.clear && f.status.includes("未启用"), "清除后：" + JSON.stringify(f));
      // 停用 AI：清掉服务商设置，回到没配置
      await page.click("#ai-off"); await settle(200);
      t1 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(500);
      assert((putBody(t1, "/api/ai/provider") || {}).preset === "", "停用应发 preset 空串");
      f = await aiForm();
      assert(f.preset === "" && f.url === "" && !(await page.$eval("#ai-off-zone", z => !z.hidden)), "停用后：" + JSON.stringify(f));
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：获取模型列表填进可选列表；测试连接只认保存过的设置，显示延迟和能否调用工具；思考深度只对 Claude、协议只对自定义", async () => {
    await aiState({ reset: true });
    try {
      await page.goto(url("#/settings/ai")); await settle();
      await page.selectOption("#ai-preset", "custom"); await settle(100);
      let f = await aiForm();
      assert(f.proto && !f.effort && f.url === "" && f.keySub.includes("中转方能看到"), "自定义：" + JSON.stringify(f));
      await page.selectOption("#ai-proto", "anthropic"); await settle(100);
      assert((await aiForm()).effort, "自定义走 Claude 协议时应显示思考深度");
      await page.selectOption("#ai-preset", "claude"); await settle(100);
      f = await aiForm();
      assert(!f.proto && f.effort && f.url === "https://api.anthropic.com" && f.model === "claude-opus-5-5", "选 Claude 后：" + JSON.stringify(f));
      await page.click("#ai-list");
      assert(await toastHas("先填写 Key"), "没填 Key 时获取模型列表应提示");
      await page.fill("#ai-key", "sk-ant-test");
      await page.click("#ai-list"); await settle(300);
      const opts = await page.$$eval("#ai-models option", os => os.map(o => o.value));
      assert(opts.length === 3 && opts.includes("claude-sonnet-5-5"), "模型列表：" + opts);
      assert((await page.textContent("#ai-model-sub")).includes("取到 3 个模型"), "应提示取到几个模型");
      await page.selectOption("#ai-effort", "high");
      let t0 = reqs.length;
      await page.click('[data-sb="save"]'); await settle(300);
      const b = putBody(t0, "/api/ai/provider");
      assert(b && b.preset === "claude" && b.effort === "high" && b.key === "sk-ant-test" && !("protocol" in b), "保存 Claude：" + JSON.stringify(b));
      // 改了还没保存：不发测试请求，提示先保存
      await page.fill("#ai-model", "claude-sonnet-5-5");
      t0 = reqs.length;
      await page.click("#ai-test");
      assert(await toastHas("先保存服务商设置"), "未保存时应提示先保存");
      assert(!reqs.slice(t0).some(r => r.u === "/api/ai/test"), "未保存时不应发测试请求");
      await page.click('[data-sb="save"]'); await settle(300);
      await page.click("#ai-test"); await settle(300);
      let sub = await page.textContent("#ai-test-sub");
      assert(sub.includes("连接正常") && sub.includes("820 ms") && sub.includes("能调用工具") && sub.includes("168 token"), "测试结果：" + sub);
      await aiState({ testFail: true });
      await page.click("#ai-test"); await settle(300);
      sub = await page.textContent("#ai-test-sub");
      assert(sub.includes("连接失败") && sub.includes("Key 不对"), "测试失败时：" + sub);
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：每日额度按万 token 保存，填错就近提示；自填单价可添加、删除，优先于公开价格；只配本机模型时不取价格", async () => {
    await aiState({ reset: true, ...AI_CLAUDE, key: "sk-ant" });
    try {
      await page.goto(url("#/settings/ai")); await settle();
      const qsub = await page.textContent("#q-sub-assistant");
      assert(qsub.includes("今天已用 12.3 万 token（约 $0.03）") && qsub.includes("约 $0.42 + ¥1.30") && qsub.includes("1 次查不到价格"), "用量：" + qsub);
      let t0 = reqs.length;
      await page.fill("#q-assistant", "5"); await page.press("#q-assistant", "Enter"); await settle(300);
      assert((putBody(t0, "/api/ai/quota") || {}).assistant === 50000, "额度应按万 token 换算");
      assert(await toastHas("每天最多 5 万 token"), "应提示新额度");
      t0 = reqs.length;
      await page.fill("#q-assistant", "-1"); await page.press("#q-assistant", "Enter"); await settle(200);
      assert((await page.textContent("#q-err-assistant")).includes("不小于 0") && await page.$eval("#q-assistant", e => e.classList.contains("invalid")), "填错应就近提示");
      assert(!putBody(t0, "/api/ai/quota"), "填错不应保存");
      await page.fill("#q-assistant", ""); await page.press("#q-assistant", "Enter"); await settle(300);
      assert((putBody(t0, "/api/ai/quota") || {}).assistant === 0 && (await page.textContent("#q-err-assistant")) === "", "清空即不限");
      // 公开价格：手动刷新后显示当前模型的单价
      assert((await page.textContent("#ai-price-src")).includes("还没取到"), "还没取价格时：" + await page.textContent("#ai-price-src"));
      await page.click("#ai-price-refresh"); await settle(300);
      assert((await page.textContent("#ai-price-src")).includes("2026-10-28 更新"), "刷新后：" + await page.textContent("#ai-price-src"));
      assert((await page.textContent("#ai-model-price")).startsWith("公开价格：输入 $4 · 输出 $20"), "公开价格：" + await page.textContent("#ai-model-price"));
      // 自填单价（人民币，不做汇率换算）：缺输出单价时就近提示；填好后优先于公开价格
      await page.selectOption("#cp-cur", "CNY");
      await page.fill("#cp-in", "30");
      await page.click("#cp-add"); await settle(150);
      assert((await page.textContent("#cp-err")).includes("输入和输出单价都要填"), "缺输出单价应就近提示");
      await page.fill("#cp-out", "150");
      t0 = reqs.length;
      await page.click("#cp-add"); await settle(300);
      const pb = putBody(t0, "/api/ai/prices");
      assert(pb && JSON.stringify(pb.prices) === JSON.stringify([{ model: "claude-opus-5-5", currency: "CNY", input: 30, output: 150 }]), "自填单价：" + JSON.stringify(pb));
      assert((await page.textContent("#ai-model-price")).startsWith("自填：输入 ¥30 · 输出 ¥150"), "自填的应优先：" + await page.textContent("#ai-model-price"));
      t0 = reqs.length;
      await page.click('[data-del-price="0"]'); await settle(300);
      assert((putBody(t0, "/api/ai/prices") || {}).prices.length === 0 && !(await page.$("[data-del-price]")), "删除自填单价");
      assert((await page.textContent("#ai-model-price")).startsWith("公开价格"), "删掉后回到公开价格");
      // 只配了 Ollama：不花钱，不取价格，不用 Key
      await aiState({ preset: "ollama", protocol: "openai", baseUrl: "http://localhost:11434/v1/", model: "qwen3:8b", key: "" });
      await page.goto(url("#/settings/ai")); await settle();
      const f = await aiForm();
      assert(f.priceSrc.includes("本机模型不花钱") && await page.$eval("#ai-price-refresh", e => e.disabled), "本机模型不应取价格：" + f.priceSrc);
      assert((await page.textContent("#ai-model-price")).includes("本机模型，不计费") && f.keyPh === "不用 Key" && f.status.includes("已启用"), "本机模型：" + JSON.stringify(f));
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：局域网打开只能看——提示仅本机修改，控件全部禁用，没有保存条和「停用 AI」", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x", local: false });
    try {
      const t0 = reqs.length;
      await page.goto(url("#/settings/ai")); await settle();
      const r = await page.evaluate(() => ({
        warn: [...document.querySelectorAll("#set-body .warn-card")].map(w => w.textContent).join(""),
        live: [...document.querySelectorAll("#set-body input, #set-body select, #set-body button")].filter(e => !e.disabled && !e.hidden).map(e => e.id || e.outerHTML.slice(0, 40)),
        bar: !!document.querySelector("#set-body .save-bar"), off: !!document.querySelector("#ai-off-zone"),
        ph: document.querySelector("#ai-key").placeholder, clear: !document.querySelector("#ai-key-clear").hidden }));
      assert(r.warn.includes("仅支持在本机修改"), "应提示仅本机修改：" + r.warn);
      assert(r.live.length === 0, "局域网下仍能操作：" + r.live);
      assert(!r.bar && !r.off && r.ph === "已设置" && !r.clear, "局域网下：" + JSON.stringify(r));
      assert(!reqs.slice(t0).some(x => x.u.startsWith("/api/ai/") && x.m !== "GET"), "局域网下不应发修改请求");
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：手机宽度（390px）下分区不横向溢出", async () => {
    await aiState({ reset: true, preset: "custom", protocol: "anthropic", baseUrl: "https://relay.example.com", model: "claude-opus-5-5", key: "sk",
      prices: [{ model: "claude-opus-5-5", currency: "CNY", input: 30, output: 150, cacheRead: 3 }] });
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/settings/ai"); await p.waitForTimeout(900);
      const w = await p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
        wide: [...document.querySelectorAll("#set-body *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.id || e.className).slice(0, 5) }));
      assert(w.sw <= w.cw && w.wide.length === 0, "横向溢出：" + JSON.stringify(w));
    } finally { await c.close(); await aiState({ reset: true }); }
  });

  // ==================== 第五阶段：音乐共享（music.js）====================
  // 仿真后端的场景见 server.js 的 musicMockState；POST 的请求体单独记一份（上面的 reqs 只记 PUT 的）
  const musicState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
  const posts = [];
  page.on("request", r => { if (r.method() === "POST") posts.push({ u: decodeURIComponent(new URL(r.url()).pathname), b: r.postData() || "" }); });
  const postsTo = (since, suffix) => posts.slice(since).filter(x => x.u.endsWith(suffix));
  const PACK = "机械动力整合包";
  const openMusic = async (state, status = "running") => {
    await musicState({ music: { reset: true, ...state }, status: { [PACK]: status } });
    await page.goto(url("#/inst/" + encodeURIComponent(PACK) + "/music")); await settle(900);
  };
  const resetMusic = () => musicState({ music: { reset: true }, status: { [PACK]: "stopped" } });

  await run("音乐共享：只有 Forge 系的服务器有这个标签；检查清单列出 SVC、桥接模组、语音端口、远程语音；点「安装」装上桥接模组", async () => {
    try {
      await page.goto(url("#/inst/" + inst + "/console")); await settle();
      assert(!(await page.$('.tabs [data-t="music"]')), "Paper 服不该有音乐共享标签");
      await openMusic({}, "stopped");
      assert(await page.$('.tabs [data-t="music"].cur'), "Forge 服应有音乐共享标签并选中");
      const check = await page.textContent("#mu-check");
      for (const s of ["核心：Forge 1.20.1", "已装 1.20.1-2.5.24", "桥接模组：还没装", "语音端口：UDP 24454", "没有语音隧道", "voice_host = （空）"])
        assert(check.includes(s), `检查清单里没有「${s}」：${check}`);
      assert(await page.$eval("#mu-start", b => b.disabled) && (await page.textContent("#mu-why")).includes("先装桥接模组"), "没装桥接模组时不能开始");
      const t0 = posts.length;
      await page.click('[data-install="main"]'); await settle(500);
      const ins = postsTo(t0, "/music/install");
      assert(ins.length === 1 && JSON.parse(ins[0].b).variant === "main", "安装请求：" + JSON.stringify(ins));
      assert(await toastHas("已装好桥接模组"), "应提示装好了");
      assert((await page.textContent("#mu-check")).includes("已装 0.2.0 版"), "装好后检查清单没更新：" + await page.textContent("#mu-check"));
      assert((await page.textContent("#mu-why")).includes("先开服"), "装好后还差开服：" + await page.textContent("#mu-why"));
    } finally { await resetMusic(); }
  });

  await run("音乐共享：没装 SVC 时检查清单直接给「安装」按钮（不再让人去资源页认），装好后清单刷新；运行中装的提示重启后生效、不能开始广播；下载失败报原因、按钮恢复（测试报告观察 13）", async () => {
    try {
      await openMusic({ inst: { [PACK]: { svc: false } } }, "stopped");
      let check = await page.textContent("#mu-check");
      assert(check.includes("Simple Voice Chat：mods 里没有") && check.includes("Forge 1.20.1 版") && !(await page.$('#mu-check a[href$="/res"]')), "没装 SVC 时：" + check);
      assert((await page.getAttribute("#mu-svc-install", "aria-label")) === "安装 Simple Voice Chat", "按钮的名字应带上模组名");
      assert(await page.$eval('[data-install="main"]', b => b.disabled), "没装 SVC 时桥接模组不能装");
      let t0 = posts.length;
      await page.click("#mu-svc-install"); await settle(500);
      assert(postsTo(t0, "/music/svc").length === 1, "安装请求：" + JSON.stringify(posts.slice(t0)));
      assert(await toastHas("已装好 Simple Voice Chat 1.20.1-2.6.27"), "应提示装好了");
      check = await page.textContent("#mu-check");
      assert(check.includes("已装 1.20.1-2.6.27") && !(await page.$("#mu-svc-install")) && !(await page.$eval('[data-install="main"]', b => b.disabled)), "装好后清单没刷新：" + check);
      assert(!(await page.textContent("#mu-restart")).includes("Simple Voice Chat"), "停着时装的不用重启");

      await openMusic({ inst: { [PACK]: { svc: false } } }, "running");
      await page.click("#mu-svc-install"); await settle(500);
      assert(await toastHas("重启服务器后生效"), "运行中装的应提示重启后生效");
      assert((await page.textContent("#mu-restart")).includes("Simple Voice Chat") && (await page.textContent("#mu-check")).includes("重启生效"), "顶上应提示重启：" + await page.textContent("#mu-restart"));
      assert((await page.textContent("#mu-why")).includes("装好 Simple Voice Chat 后要重启服务器"), "没重启时不能开始：" + await page.textContent("#mu-why"));

      await openMusic({ inst: { [PACK]: { svc: false, svcFail: "在 Modrinth 上查 Simple Voice Chat 的版本失败：连接超时（检查网络或代理后再试）" } } }, "stopped");
      t0 = posts.length;
      await page.click("#mu-svc-install"); await settle(500);
      assert(await toastHas("版本失败：连接超时"), "下载失败应报原因");
      assert(!(await page.$eval("#mu-svc-install", b => b.disabled)) && (await page.textContent("#mu-svc-install")).includes("安装"), "失败后按钮应恢复");
    } finally { await resetMusic(); }
  });

  await run("音乐共享：Win11 选一个正在出声的程序开始广播，选音源时电平跟着刷新；广播中显示听众、不再列音源；停止后回到选音源", async () => {
    try {
      await openMusic({ inst: { [PACK]: { bridge: "main" } } });
      const rows = await page.$$eval("#mu-src [data-pid]", rs => rs.map(r => ({ exe: r.dataset.exe, on: r.getAttribute("aria-checked") === "true", lit: r.querySelectorAll(".xp i.on").length })));
      assert(rows.length === 3 && rows[0].exe === "cloudmusic.exe" && rows[0].on && rows[0].lit > 0, "音源列表：" + JSON.stringify(rows));
      assert(!rows.find(r => r.exe === "QQMusic.exe").lit, "没声音的程序电平应为空");
      const s0 = reqs.length;
      await page.waitForTimeout(2200);
      assert(reqs.slice(s0).filter(r => r.u === "/api/music/sources").length >= 2, "选音源时应每秒刷新电平");
      await page.click('#mu-src [data-exe="msedge.exe"]'); await settle(150);
      assert(await page.$eval('#mu-src [data-exe="msedge.exe"]', r => r.getAttribute("aria-checked") === "true"), "点选后应选中 msedge.exe");
      assert(!(await page.$eval("#mu-start", b => b.disabled)), "都就绪了应能开始");
      const t0 = posts.length;
      await page.click("#mu-start"); await settle(600);
      const st = postsTo(t0, "/music/start");
      assert(st.length === 1 && JSON.stringify(JSON.parse(st[0].b)) === JSON.stringify({ mode: "process", pid: 7788, exe: "msedge.exe" }), "开始广播的请求：" + JSON.stringify(st));
      const live = await page.textContent("#mu-cast");
      assert(live.includes("正在广播") && live.includes("msedge.exe") && live.includes("2 人在听"), "广播中：" + live);
      const s1 = reqs.length;
      await page.waitForTimeout(2200);
      assert(!reqs.slice(s1).some(r => r.u === "/api/music/sources"), "广播中不该再列音源");
      await page.click("#mu-stop"); await settle(600);
      assert(postsTo(t0, "/music/stop").length === 1, "应发停止请求");
      assert(await page.$("#mu-src") && (await page.textContent("#mu-last")).includes("用户停止"), "停止后应回到选音源，并写明上次怎么停的");
    } finally { await resetMusic(); }
  });

  await run("音乐共享：只能录整机时先警告，点「仍然录整机」才开始（默认聚焦取消，取消不开始）；确认过就记住", async () => {
    try {
      await openMusic({ cap: "win10", inst: { [PACK]: { bridge: "main" } } });
      assert((await page.textContent("#mu-cast")).includes("这台电脑只能录整机的声音"), "Win10 应先写明只能录整机");
      assert(!(await page.$("#mu-src")), "整机录音不列程序");
      let t0 = posts.length;
      await page.click("#mu-start"); await settle(250);
      assert((await page.textContent(".modal")).includes("游戏声音、语音聊天"), "应弹出整机录音的警告");
      assert(await page.evaluate(() => document.activeElement.textContent.trim()) === "先不录", "默认应聚焦取消");
      await page.click('.modal [data-x="cancel"]'); await settle(250);
      assert(!postsTo(t0, "/music/start").length && !postsTo(t0, "/confirm-system").length, "取消后不该开始，也不该记下确认");
      await page.click("#mu-start"); await settle(250);
      await page.click('.modal [data-x="ok"]'); await settle(700);
      const cf = postsTo(t0, "/api/music/confirm-system"), st = postsTo(t0, "/music/start");
      assert(cf.length === 1 && st.length === 1 && JSON.parse(st[0].b).mode === "system", "确认后应先记下确认再开始：" + JSON.stringify(st));
      assert((await page.textContent("#mu-cast")).includes("正在录整机声音"), "广播中应一直提示正在录整机");
      await page.click("#mu-stop"); await settle(600);
      t0 = posts.length;
      await page.click("#mu-start"); await settle(600);
      assert(!(await page.$(".modal")) && postsTo(t0, "/music/start").length === 1, "确认过以后不该再问");
      await page.click("#mu-stop"); await settle(400);
    } finally { await resetMusic(); }
  });

  await run("音乐共享：语音隧道停着时不说「远程好友连服后就能听到」；没勾跟随启动的说下次开服前清回 voice_host，勾了的说开服时会跟着启动（测试报告 G-07）", async () => {
    try {
      const inst = { [PACK]: { bridge: "main", voiceHost: "frp-abc.natfrp.cloud:34567" } };
      await openMusic({ voiceTunnel: { running: false, autoStart: false }, inst });
      let check = await page.textContent("#mu-check");
      assert(check.includes("语音隧道没开") && check.includes("下次开服前 MineDeck 会把它清回空") && !check.includes("远程好友连服后就能听到"), "隧道停着、没勾跟随启动：" + check);
      await openMusic({ voiceTunnel: { running: false, autoStart: true }, inst }); // 服务器开着，跟随启动的隧道被手动停了
      check = await page.textContent("#mu-check");
      assert(check.includes("语音隧道没开") && check.includes("下次开服时它也会跟着启动") && !check.includes("清回空"), "隧道停着、勾了跟随启动：" + check);
    } finally { await resetMusic(); }
  });

  await run("音乐共享：voice_host 和隧道地址不一样时不覆盖，可以改用隧道地址；手动填写、「我在游戏里的名字」写错就近提示；音量即时保存", async () => {
    try {
      await openMusic({ voiceTunnel: true, inst: { [PACK]: { bridge: "main", voiceHost: "my.domain.com:24454" } } });
      let check = await page.textContent("#mu-check");
      assert(check.includes("voice_host 和隧道地址不一样") && check.includes("my.domain.com:24454") && check.includes("frp-abc.natfrp.cloud:34567"), "两个地址不一样时：" + check);
      let t0 = reqs.length;
      await page.click("#mu-vh-tunnel"); await settle(500);
      assert(JSON.stringify(putBody(t0, "/api/instances/" + encodeURIComponent(PACK) + "/music/voicehost")) === JSON.stringify({ fromTunnel: true }), "改用隧道地址的请求");
      assert(await toastHas("重启服务器后生效"), "运行中改的应提示重启后生效");
      check = await page.textContent("#mu-check");
      assert(check.includes("voice_host = frp-abc.natfrp.cloud:34567") && (await page.textContent("#mu-restart")).includes("voice_host"), "改完：" + check);
      // 手动填写：格式不对时报错写在输入框下面
      await page.click("#mu-vh-toggle"); await settle(150);
      await page.fill("#mu-vh", "不是地址!"); await page.press("#mu-vh", "Enter"); await settle(400);
      assert((await page.textContent("#mu-vh-err")).includes("语音地址不对") && await page.$eval("#mu-vh", e => e.classList.contains("invalid")), "手动填写写错应就近提示");
      // 我在游戏里的名字：有不能用的字符时不发请求
      t0 = reqs.length;
      await page.fill("#mu-ex", "Steve:1"); await page.press("#mu-ex", "Enter"); await settle(200);
      assert((await page.textContent("#mu-ex-err")).includes("不能用的字符") && !putBody(t0, "/api/instances/" + encodeURIComponent(PACK) + "/music/settings"), "名字写错应就近提示、不保存");
      await page.fill("#mu-ex", "Steve"); await page.press("#mu-ex", "Enter"); await settle(300);
      assert(JSON.stringify(putBody(t0, "/api/instances/" + encodeURIComponent(PACK) + "/music/settings")) === JSON.stringify({ exclude: "Steve" }) && (await page.textContent("#mu-ex-err")) === "", "名字应保存");
      await page.waitForTimeout(2200);
      assert((await page.textContent("#mu-players")).includes("不再发给他"), "排除名单里的人应单独列出：" + await page.textContent("#mu-players"));
      t0 = reqs.length;
      await page.$eval("#mu-vol", e => { e.value = "150"; e.dispatchEvent(new Event("input")); e.dispatchEvent(new Event("change")); });
      await settle(300);
      assert((putBody(t0, "/api/instances/" + encodeURIComponent(PACK) + "/music/settings") || {}).volume === 150 && (await page.textContent("#mu-vol-v")) === "150%", "音量应即时保存");
    } finally { await resetMusic(); }
  });

  await run("音乐共享：局域网打开不列音源、不给开始（录的是主机的声音），能停止主机上开始的广播", async () => {
    try {
      await openMusic({ inst: { [PACK]: { bridge: "main" } } });
      await page.click("#mu-start"); await settle(500);
      await musicState({ music: { local: false } });
      const s0 = reqs.length;
      await page.goto(url("#/inst/" + encodeURIComponent(PACK) + "/music")); await settle(900);
      assert((await page.textContent("#mu-cast")).includes("正在广播") && await page.$("#mu-stop"), "局域网也能看到正在广播、能停止");
      await page.click("#mu-stop"); await settle(600);
      assert((await page.textContent("#mu-cast")).includes("开始广播只能在主机上操作") && !(await page.$("#mu-start")), "局域网不给开始");
      await page.waitForTimeout(1500);
      assert(!reqs.slice(s0).some(r => r.u === "/api/music/sources"), "局域网不该列音源");
    } finally { await resetMusic(); }
  });

  await run("隧道：用途选「语音」时写明怎么建（SVC 端口取自绑定的服务器），自定义 frps 默认 UDP；请求体带上用途和协议；列表标出语音隧道", async () => {
    try {
      await musicState({ music: { reset: true, voiceTunnel: true } });
      await page.goto(url("#/tunnels")); await settle();
      assert((await page.textContent("#tun-list")).includes("语音"), "列表应标出语音隧道");
      await page.click("#tn-toggle");
      await page.selectOption("#tn-provider", "custom");
      assert(await page.$eval("#tn-voice-tip", e => e.hidden) && await page.$eval("#tn-proto", e => e.value) === "tcp", "默认是游戏隧道、TCP");
      await page.selectOption("#tn-bound", PACK);
      await page.selectOption("#tn-purpose", "voice"); await settle(300);
      const tip = await page.textContent("#tn-voice-tip");
      assert(tip.includes("24454") && tip.includes(PACK) && tip.includes("UDP"), "语音隧道的说明：" + tip);
      assert(await page.$eval("#tn-proto", e => e.value) === "udp" && await page.$eval("#tn-lport", e => e.value) === "24454", "自定义 frps 选语音时应默认 UDP、本地端口填 SVC 的");
      await page.fill("#tn-name", "语音线 2"); await page.fill("#tn-saddr", "1.2.3.4"); await page.fill("#tn-rport", "34567");
      await page.click("#tn-add"); await settle(400);
      const body = (await musicState({})).music.tunnelBody;
      assert(body && body.purpose === "voice" && body.proto === "udp" && body.localPort === 24454 && body.boundInstance === PACK, "添加隧道的请求体：" + JSON.stringify(body));
    } finally { await resetMusic(); }
  });

  await run("音乐共享：手机宽度（390px）下不横向溢出", async () => {
    await musicState({ music: { reset: true, voiceTunnel: true, inst: { [PACK]: { bridge: "main", voiceHost: "my.domain.com:24454" } } }, status: { [PACK]: "running" } });
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/inst/" + encodeURIComponent(PACK) + "/music"); await p.waitForTimeout(1200);
      await p.click("#mu-adv-head"); await p.waitForTimeout(200);
      const w = await p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
        wide: [...document.querySelectorAll("#tab-body *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.id || e.className).slice(0, 5) }));
      assert(w.sw <= w.cw && w.wide.length === 0, "横向溢出：" + JSON.stringify(w));
    } finally { await c.close(); await resetMusic(); }
  });
  // ==================== 第五阶段结束 ====================
  // ================================ 第三阶段：应用内 AI 助手 ================================
  // 仿真后端里假的 AI 按用户的话挑回复（见 server.js 的「第三阶段」）；asstState 返回 asst 的状态（executed = 确认后执行过的操作）
  const asstState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ asst: x }) }).then(r => r.json()).then(j => j.data.asst), b);
  const conPage = name => url("#/inst/" + encodeURIComponent(name) + "/console");
  const reply = (s, timeout = 5000) => page.waitForFunction(x => [...document.querySelectorAll("#ap-body .ap-msg.assistant .md")].some(e => e.textContent.includes(x)), s, { timeout });
  const idle = () => page.waitForFunction(() => { const b = document.querySelector("#ap-stop"); return b && b.hidden; }, null, { timeout: 5000 });

  await run("AI 助手：没配置 AI 时报错横幅照样出现（本地规则的结论和怎么修），不带「让 AI 分析」；关掉后这一次不再提示；入口引导去设置，Esc 关闭", async () => {
    await aiState({ reset: true }); await asstState({ reset: true });
    await page.goto(conPage("机械动力整合包")); await settle(700);
    const b = await page.$eval(".diag-banner", e => ({ text: e.textContent, err: e.classList.contains("err") })).catch(() => null);
    assert(b && b.err && b.text.includes("缺少前置模组") && b.text.includes("一共认出 2 个问题"), "横幅：" + JSON.stringify(b));
    assert(!(await page.$('.diag-banner [data-x="ai"]')), "没配置 AI 时不应有「让 AI 分析」");
    await page.click(".db-more summary");
    assert((await page.textContent(".db-more")).includes("怎么修"), "展开后应有修复建议");
    await page.click('.diag-banner [data-x="close"]');
    assert(!(await page.$(".diag-banner")), "关闭后横幅应消失");
    await page.goto(conPage("机械动力整合包")); await settle(700);
    assert(!(await page.$(".diag-banner")), "同一次的问题关掉后不应再出现");
    await page.evaluate(() => sessionStorage.clear());
    await page.goto(conPage("生存服 · 朋友")); await settle(700);
    assert(!(await page.$(".diag-banner")), "没有问题时不应有横幅");
    await page.click("#ai-btn"); await settle(300);
    assert((await page.textContent("#ap-body")).includes("AI 还没配置") && !!(await page.$('#ap-body [data-x="setup"]')), "没配置 AI 时面板应引导去设置");
    await page.keyboard.press("Escape"); await settle(300);
    assert(!(await page.$("#ai-panel")), "Esc 应关闭面板");
  });

  await run("AI 助手：「让 AI 分析」先给发送预览（字数、可展开、隐私信息是占位符，取消就不发），发送后流式回复；读操作不弹确认，写操作确认后按提案参数原样执行", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await page.goto(conPage("机械动力整合包")); await settle(700);
      await page.click('.diag-banner [data-x="ai"]');
      await page.waitForSelector(".modal.ap-pv");
      const pv = await page.evaluate(() => ({ body: document.querySelector(".ap-pv .m-body").textContent, q: document.querySelector(".ap-pv textarea").value,
        pre: document.querySelector(".ap-pv .ap-pre").textContent, open: document.querySelector(".ap-pv-more").open }));
      assert(/将发送约 \d+ 字/.test(pv.body) && pv.q.includes("分析") && !pv.open, "预览：" + JSON.stringify(pv).slice(0, 200));
      assert(pv.pre.includes("<用户目录>") && pv.pre.includes("<局域网IP>") && !/C:\\Users\\|192\.168\./.test(pv.pre), "占位符应原样显示：" + pv.pre.slice(0, 200));
      await page.click(".ap-pv-more summary");
      assert(await page.$eval(".ap-pv-more", d => d.open), "应能展开查看全文");
      await page.click('.modal [data-x="cancel"]'); await settle(300);
      let a = await asstState({});
      assert(a.sessions.length === 0 && !(await page.$("#ai-panel")), "取消后不发送，也不留下空会话：" + a.sessions.length);
      await page.click('.diag-banner [data-x="ai"]');
      await page.waitForSelector(".modal.ap-pv");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector("#ai-panel .ap-card.pending", { timeout: 5000 });
      const v = await page.evaluate(() => ({
        title: document.querySelector("#ap-title").textContent,
        chip: (document.querySelector(".ap-msg.user .ap-chip") || {}).textContent || "",
        tools: [...document.querySelectorAll(".ap-tool")].map(t => t.className + "|" + t.textContent.replace(/\s+/g, " ").trim()),
        card: document.querySelector(".ap-card.pending").textContent.replace(/\s+/g, " "),
        usage: document.querySelectorAll(".ap-usage").length,
        sendPrimary: document.querySelector("#ap-send").classList.contains("primary") }));
      assert(v.title.includes("机械动力整合包") && v.chip.includes("附上了") && v.chip.includes("排错资料"), "发送后：" + JSON.stringify(v));
      assert(v.tools.length === 1 && v.tools[0].includes("done") && v.tools[0].includes("查看模组清单"), "读操作应直接执行、不弹确认：" + v.tools);
      assert(v.card.includes("写操作") && v.card.includes("xaerominimap.jar") && v.usage >= 2 && !v.sendPrimary, "确认卡片：" + JSON.stringify(v));
      a = await asstState({});
      assert(a.executed.length === 0, "没确认的写操作不应执行");
      await page.click('.ap-card.pending [data-x="ok"]');
      await reply("已经禁用了");
      a = await asstState({});
      assert(a.executed.length === 1 && JSON.stringify(a.executed[0].args) === JSON.stringify({ inst: "机械动力整合包", file: "xaerominimap.jar", enable: false }), "执行的参数应和提案一致：" + JSON.stringify(a.executed));
      assert((await page.textContent(".ap-card .pc-out")).includes("已确认"), "卡片应显示已确认");
      // 实际发给 AI 的内容可以点开看
      await page.click(".ap-msg.user .ap-chip");
      await page.waitForSelector(".modal .ap-sent");
      assert((await page.textContent(".modal .ap-sent")).includes("<程序目录>"), "应能看到实际发送的内容");
      await page.keyboard.press("Escape"); await settle(250);
      assert(!!(await page.$("#ai-panel")), "Esc 先关弹窗，面板还在");
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：新会话可选关联的服务器；拒绝的理由交还给 AI；危险操作要多勾一次才能确认；AI 回复里的 HTML 原样显示", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await page.goto(url("#/inst/" + inst + "/console")); await settle(500);
      await page.evaluate(() => localStorage.removeItem("amh_ai_sid"));
      await page.click("#ai-btn"); await settle(300);
      await page.click('#ap-body [data-x="new"]'); await settle(200);
      const items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent.trim()));
      assert(items[0] === "生存服 · 朋友（当前）" && items.includes("不关联服务器"), "新会话的菜单：" + items);
      await page.click(".dropmenu button >> nth=0"); await settle(300);
      assert((await page.textContent("#ap-sub")).includes("关联：生存服 · 朋友"), "应关联当前服务器");
      await page.fill("#ap-in", "把人数改成 10"); await page.press("#ap-in", "Enter");
      await page.waitForSelector(".ap-card.pending", { timeout: 5000 });
      assert((await page.$$(".ap-tool.done")).length === 1, "读 server.properties 应直接执行");
      await page.click('.ap-card.pending [data-x="reject"]');
      await page.fill(".ap-card.pending .pc-reason input", "周末人多");
      await page.press(".ap-card.pending .pc-reason input", "Enter");
      await reply("周末人多");
      assert((await page.textContent(".ap-card .pc-out")).includes("已拒绝"), "卡片应显示已拒绝");
      assert((await asstState({})).executed.length === 0, "拒绝的不应执行");
      await idle();
      await page.fill("#ap-in", "重启一下"); await page.press("#ap-in", "Enter");
      await page.waitForSelector(".ap-card.danger.pending", { timeout: 5000 });
      const ok = await page.$eval('.ap-card.danger.pending [data-x="ok"]', b => ({ dis: b.disabled, cls: b.className }));
      assert(ok.dis && ok.cls.includes("danger solid"), "危险操作没勾选时：" + JSON.stringify(ok));
      await page.check('.ap-card.danger.pending [data-k="ack"]');
      assert(!(await page.$eval('.ap-card.danger.pending [data-x="ok"]', b => b.disabled)), "勾选后应能确认");
      await page.click('.ap-card.danger.pending [data-x="ok"]');
      await reply("已经在重启了");
      const ex = (await asstState({})).executed;
      assert(ex.length === 1 && ex[0].op === "instance.restart" && ex[0].ack === true, "危险操作应带上第二次确认：" + JSON.stringify(ex));
      await idle();
      await page.fill("#ap-in", "你好"); await page.press("#ap-in", "Enter");
      await reply("应该原样显示"); await idle();
      const m = await page.evaluate(() => {
        const md = [...document.querySelectorAll(".ap-msg.assistant .md")].pop();
        return { h: !!md.querySelector("h4"), nest: !!md.querySelector("ol li ul li"), table: !!md.querySelector("table td"), pre: !!md.querySelector("pre code"),
          img: md.querySelectorAll("img").length, link: (md.querySelector("a") || {}).href, xss: window.__xss, text: md.textContent };
      });
      assert(m.h && m.nest && m.table && m.pre && m.img === 0 && !m.xss && m.text.includes("<img src=x") && m.link === "https://docs.papermc.io/", "Markdown：" + JSON.stringify(m).slice(0, 300));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：设置里的「局域网可用 AI 助手」即时生效，局域网打开时关掉就没有入口；会话记录可删除、清空", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await page.goto(url("#/settings/ai")); await settle();
      assert(await page.$eval("#ai-lan", e => e.checked && !e.disabled), "开关默认开");
      const t0 = reqs.length;
      await page.click("#ai-assist-slot .switch .sw"); await settle(300);
      assert((putBody(t0, "/api/ai/assistant") || {}).lan === false, "关掉应发 lan: false");
      assert(await toastHas("局域网登录的设备不能用"), "应提示已关闭");
      await aiState({ local: false }); // 模拟局域网打开
      await page.goto(url("#/instances")); await settle();
      assert(await page.$eval("#ai-btn", b => b.hidden), "关掉后局域网打开的界面里不应有助手入口");
      await asstState({ lan: true });
      await page.goto(url("#/instances")); await settle();
      assert(!(await page.$eval("#ai-btn", b => b.hidden)), "打开后局域网也有入口");
      await aiState({ local: true });
      await page.evaluate(async () => {
        for (const t of ["第一个问题", "第二个问题"]) {
          const r = await fetch("/api/assistant/sessions", { method: "POST", body: JSON.stringify({ inst: "" }) }).then(r => r.json());
          await fetch(`/api/assistant/sessions/${r.data.id}/messages`, { method: "POST", body: JSON.stringify({ text: t }) });
        }
        localStorage.removeItem("amh_ai_sid");
      });
      await page.goto(url("#/instances")); await settle(1500);
      await page.click("#ai-btn"); await settle(400);
      assert((await page.$$(".ap-row")).length === 2, "应列出 2 个会话");
      await page.click(".ap-row [data-del] >> nth=0"); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await page.$$(".ap-row")).length === 1, "删除后剩 1 个");
      await page.click('#ap-body [data-x="clear"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await page.textContent("#ap-body")).includes("还没有会话") && await toastHas("已清空 1 个会话"), "清空后应是空状态");
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：手机宽度（390px）下面板占满、不横向溢出；在输入框里按数字键不会切页", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(800);
      await p.click("#ai-btn"); await p.waitForTimeout(400);
      await p.click('#ap-body [data-x="new"]'); await p.waitForTimeout(200);
      await p.click(".dropmenu button >> text=不关联服务器"); await p.waitForTimeout(400);
      await p.fill("#ap-in", "你好"); await p.press("#ap-in", "Enter");
      await p.waitForFunction(() => [...document.querySelectorAll(".ap-msg.assistant .md")].some(e => e.textContent.includes("原样显示")), null, { timeout: 5000 });
      const w = await p.evaluate(() => {
        const r = document.querySelector("#ai-panel").getBoundingClientRect();
        return { left: r.left, width: r.width, sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
          wide: [...document.querySelectorAll("#ai-panel *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1 && !e.closest(".md-table, .md-pre, .ap-kv"))
            .map(e => e.className || e.tagName).slice(0, 5) };
      });
      assert(w.left === 0 && w.width === 390 && w.sw <= w.cw && w.wide.length === 0, "手机宽度：" + JSON.stringify(w));
      await p.focus("#ap-in"); await p.keyboard.press("2"); await p.waitForTimeout(200);
      assert(p.url().endsWith("#/instances"), "输入框里按 2 不应切页：" + p.url());
    } finally { await c.close(); await asstState({ reset: true }); await aiState({ reset: true }); }
  });
  // ============================== 第三阶段结束 ==============================

  // ==================== 第四阶段：游戏内 AI（gamechat.js）====================
  // 仿真后端的场景见 server.js 的 gcMockState；改设置的 PUT 记在 reqs 里（路径要解码），一键开白名单的 POST 记在 posts 里（第五阶段那块登记的）
  const gcState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
  const gcPuts = since => reqs.slice(since).filter(x => x.m === "PUT" && decodeURIComponent(x.u).endsWith("/gameai")).map(x => JSON.parse(x.b || "{}"));
  const openGC = async (name = "生存服 · 朋友") => { await page.goto(url("#/inst/" + encodeURIComponent(name) + "/gameai")); await settle(900); };
  const gcReady = { gc: { reset: true }, ai: { reset: true, ...AI_DEEPSEEK, key: "sk-x" } };
  const resetGC = () => gcState({ gc: { reset: true }, ai: { reset: true } });

  await run("游戏内 AI：代理端没有这个标签；没配置 AI 时先提示去设置；开关即时生效", async () => {
    try {
      await resetGC();
      await page.goto(url("#/inst/" + encodeURIComponent("群组代理") + "/console")); await settle();
      assert(!(await page.$('.tabs [data-t="gameai"]')), "代理端不该有游戏内 AI 标签");
      await openGC();
      assert(await page.$('.tabs [data-t="gameai"].cur'), "应有游戏内 AI 标签并选中");
      const gate = await page.textContent("#gc-gate");
      assert(gate.includes("AI 还没配置") && await page.$('#gc-gate a[href="#/settings/ai"]'), "没配置 AI 时应提示去设置：" + gate);
      await gcState(gcReady);
      await openGC();
      assert((await page.textContent("#gc-gate")).trim() === "" && (await page.textContent("#gc-prov")).includes("提问会发给 DeepSeek"), "配置了 AI 后不再提示");
      const t0 = reqs.length;
      await page.click("#gc-on + .sw"); await settle(300);
      assert(JSON.stringify(gcPuts(t0)) === JSON.stringify([{ enabled: true }]), "开关应即时发 PUT：" + JSON.stringify(gcPuts(t0)));
      assert(await toastHas("已打开游戏内 AI"), "应提示已打开");
    } finally { await resetGC(); }
  });

  await run("游戏内 AI：授权玩家时没开白名单就提醒；一键开启白名单先列出要加的人（默认全选，已在白名单的、名字不合规的另外说明），确认后提醒消失", async () => {
    try {
      await gcState(gcReady);
      await openGC();
      assert(!(await page.$("#gc-wl-warn")), "还没授权时不提醒");
      const t0 = reqs.length;
      await page.click('[data-allow="Notch_CN"] + .sw'); await settle(400);
      assert(JSON.stringify(gcPuts(t0).pop()) === JSON.stringify({ allowed: ["Notch_CN"] }), "授权应即时发 PUT：" + JSON.stringify(gcPuts(t0)));
      const warn = await page.textContent("#gc-wl-warn");
      assert(warn.includes("这个服没开白名单") && warn.includes("Notch_CN 的名字进服"), "没开白名单应提醒：" + warn);
      await page.click("#gc-wl"); await page.waitForSelector(".modal");
      const items = await page.$$eval(".gc-wl-item", ls => ls.map(l => l.querySelector("span").textContent + (l.querySelector("input").checked ? "✓" : "")));
      assert(JSON.stringify(items) === JSON.stringify(["Notch_CN✓", "Herobrine✓"]), "要加进白名单的人：" + items);
      const body = (await page.textContent(".modal")).replace(/\s+/g, " ");
      assert(body.includes("Mr-X 的名字") && body.includes("已经在白名单里：Alex、Steve") && body.includes("whitelist on"), "说明：" + body);
      await page.uncheck('.gc-wl-item input[data-k="Herobrine"]');
      const p0 = posts.length;
      await page.click('.modal [data-x="ok"]'); await settle(500);
      const wl = postsTo(p0, "/gameai/whitelist");
      assert(wl.length === 1 && JSON.stringify(JSON.parse(wl[0].b)) === JSON.stringify({ players: ["Notch_CN"] }), "一键开白名单的请求：" + JSON.stringify(wl));
      assert(!(await page.$("#gc-wl-warn")) && await toastHas("已把 Notch_CN 加进白名单"), "开启后提醒消失");
      const t1 = reqs.length;
      await page.click('[data-allow="Notch_CN"] + .sw'); await settle(300);
      assert(JSON.stringify(gcPuts(t1).pop()) === JSON.stringify({ allowed: [] }) && await toastHas("已收回 Notch_CN 的执行权限"), "收回授权");
      await page.fill("#gc-add-name", "@a"); await page.press("#gc-add-name", "Enter"); await settle(200);
      assert((await page.textContent("#gc-add-err")).includes("@ 开头"), "以 @ 开头的名字应就近提示");
      await page.fill("#gc-add-name", "Bob_2"); await page.click("#gc-add"); await settle(400);
      assert(await page.$('[data-allow="Bob_2"]:checked') && (await page.inputValue("#gc-add-name")) === "", "按名字授权后列出并勾上");
    } finally { await resetGC(); }
  });

  await run("游戏内 AI：命令模板即时保存（gamemode、give 默认关）；限流和前缀单字段保存，填错就近提示", async () => {
    try {
      await gcState(gcReady);
      await openGC();
      const tpl = await page.$$eval("#gc-tpl [data-tpl]", cs => cs.map(c => c.dataset.tpl + (c.checked ? "+" : "-")));
      assert(tpl.join(",") === "time+,weather+,tp+,effect+,spawnpoint+,locate+,gamemode-,give-,difficulty-,seed-", "模板的默认开关：" + tpl);
      assert((await page.$$("#gc-tpl .tag")).length === 4, "默认关的四个要标出来");
      let t0 = reqs.length;
      await page.click('[data-tpl="gamemode"] + .sw'); await settle(300);
      assert(JSON.stringify(gcPuts(t0).pop()) === JSON.stringify({ templates: { gamemode: true } }), "模板开关即时保存");
      assert(await page.$eval("#gc-l-save-cooldown", b => b.disabled), "没改时保存键置灰");
      await page.fill("#gc-l-cooldown", "-1"); await page.press("#gc-l-cooldown", "Enter"); await settle(200);
      assert((await page.textContent("#gc-l-err-cooldown")).includes("不小于 0") && await page.$("#gc-l-cooldown.invalid"), "填错应就近提示");
      t0 = reqs.length;
      await page.fill("#gc-l-cooldown", "20"); await page.press("#gc-l-cooldown", "Enter"); await settle(300);
      await page.fill("#gc-l-budget", "80"); await page.click("#gc-l-save-budget"); await settle(300);
      assert(JSON.stringify(gcPuts(t0)) === JSON.stringify([{ cooldown: 20 }, { budget: 800000 }]), "限流的请求：" + JSON.stringify(gcPuts(t0)));
      assert((await page.textContent("#gc-l-err-cooldown")).trim() === "" && await toastHas("全服每天的 token：80 万 token"), "保存后错误消失");
      await page.fill("#gc-prefix", "/ai"); await page.press("#gc-prefix", "Enter"); await settle(300);
      assert((await page.textContent("#gc-prefix-err")).includes("不能用的字符"), "不合规的前缀应就近提示");
      await page.fill("#gc-prefix", "@小助手"); await page.press("#gc-prefix", "Enter"); await settle(300);
      assert(await toastHas("触发前缀改成了 @小助手") && (await page.textContent("#gc-prefix-err")).trim() === "", "前缀保存");
    } finally { await resetGC(); }
  });

  await run("游戏内 AI：识别测试显示认出的玩家、正文、选项和怎么处理；还没保存的别名也能试", async () => {
    try {
      await gcState(gcReady);
      await openGC();
      await page.click("#gc-run"); await settle(200);
      assert((await page.textContent("#gc-test-err")).includes("先贴一行日志"), "空的时候就近提示");
      await page.click("#gc-sample"); await page.click("#gc-run"); await settle(400);
      const kv = await page.$$eval(".gc-kv dt", ds => ds.map(d => d.textContent + "=" + d.nextElementSibling.textContent.replace(/\s+/g, " ").trim()));
      assert(kv.includes("日志格式=Forge / NeoForge") && kv.includes("玩家=Steve 在线") && kv.includes("选项=/私") && kv.includes("问题=今晚怎么打末影龙"), "识别结果：" + kv);
      assert((await page.textContent(".gc-result")).includes("只发给 Steve") && await page.$(".gc-result.hit"), "结论");
      await page.fill("#gc-alias", "@小助手");
      await page.fill("#gc-line", "[20:15:31] [Server thread/INFO]: <Alex> @小助手 你好"); await page.press("#gc-line", "Enter"); await settle(400);
      assert((await page.textContent(".gc-result")).includes("Alex 提问「你好」"), "还没保存的别名也能试：" + await page.textContent(".gc-result"));
      await page.fill("#gc-line", "[20:15:31] [Server thread/INFO]: <Alex> @AIbot 你好"); await page.press("#gc-line", "Enter"); await settle(400);
      assert((await page.textContent(".gc-result")).includes("不触发") && await page.$(".gc-result.miss"), "@AIbot 不触发");
      await page.fill("#gc-alias", ""); // 还原，免得离开时被未保存守卫拦下
    } finally { await resetGC(); }
  });

  await run("游戏内 AI：最近的问答显示完整的回答、执行的命令（没执行的写明原因）和用量；没有记录时给出怎么试", async () => {
    try {
      await gcState(gcReady);
      await openGC();
      const recs = await page.$$eval("#gc-log .gc-rec", rs => rs.map(r => r.textContent.replace(/\s+/g, " ")));
      assert(recs.length === 6, "应列出 6 条：" + recs.length);
      assert(recs[0].includes("把天气改晴") && recs[0].includes("/weather clear") && recs[0].includes("Set the weather to clear") && recs[0].includes("约 $"), "第一条：" + recs[0]);
      assert(recs[1].includes("/op Steve") && recs[1].includes("没执行：op 命令永远不允许"), "没执行的命令写明原因：" + recs[1]);
      assert(recs[2].includes("（只发给 Alex）") && recs[2].includes("（游戏里截断了）") && recs[2].includes("龙蛋"), "完整的回答：" + recs[2]);
      assert(await page.$("#gc-log .gc-cmd.bad") && await page.$("#gc-log .gc-cmd.ok"), "命令的成败要标出来");
      await openGC("机械动力整合包");
      assert((await page.textContent("#gc-log")).includes("还没有人问过"), "空状态");
    } finally { await resetGC(); }
  });

  await run("游戏内 AI：局域网打开只能看——控件禁用、写明要在主机上改，识别测试照样能用；手机宽度不横向溢出", async () => {
    try {
      await gcState({ gc: { reset: true }, ai: { reset: true, ...AI_DEEPSEEK, key: "sk-x", local: false } });
      await openGC();
      const d = await page.evaluate(() => ({ note: document.querySelector("#gc-gate").textContent,
        enabled: [...document.querySelectorAll("#gc input, #gc button")].filter(e => !e.disabled).map(e => e.id).sort() }));
      assert(d.note.includes("局域网打开") && d.note.includes("那台电脑上"), "应写明在主机上改：" + d.note);
      assert(JSON.stringify(d.enabled) === JSON.stringify(["gc-line", "gc-run", "gc-sample"]), "局域网只有识别测试能用：" + d.enabled);
      await page.click("#gc-sample"); await page.click("#gc-run"); await settle(400);
      assert(await page.$(".gc-result.hit"), "局域网也能做识别测试");
    } finally { await resetGC(); }
    await gcState(gcReady);
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/inst/" + encodeURIComponent("生存服 · 朋友") + "/gameai"); await p.waitForTimeout(1200);
      await p.click('[data-allow="Notch_CN"] + .sw'); await p.waitForTimeout(400);
      await p.click("#gc-sample"); await p.click("#gc-run"); await p.click("#gc-adv-head"); await p.waitForTimeout(400);
      const w = await p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
        wide: [...document.querySelectorAll("#tab-body *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.id || e.className).slice(0, 5) }));
      assert(w.sw <= w.cw && w.wide.length === 0, "横向溢出：" + JSON.stringify(w));
    } finally { await c.close(); await resetGC(); }
  });
  // ==================== 第四阶段结束 ====================

  // ==================== 第七阶段：AI 伙伴（companion.js）====================
  // 仿真后端的场景见 server.js 的 cpMockState；改设置的 PUT 记在 reqs 里，安装、刷出、停下、移除的 POST 记在 posts 里（第五阶段那块登记的）
  const cpState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
  const cpPuts = since => reqs.slice(since).filter(x => x.m === "PUT" && decodeURIComponent(x.u).endsWith("/companion/settings")).map(x => JSON.parse(x.b || "{}"));
  const openCP = async () => { await page.goto(url("#/inst/" + encodeURIComponent(PACK) + "/companion")); await settle(900); };
  const CP_INSTALLED = { minedeck_companion: { state: "ok", version: "0.2.0", files: [{ file: "minedeck-companion-forge-1.20.1-0.2.0.jar", version: "0.2.0", keep: true }] },
    playerengine: { state: "ok", version: "1.20.1-1.4.0", files: [{ file: "playerengine-forge-1.20.1-1.4.0.jar", version: "1.20.1-1.4.0", keep: true }] } };
  const resetCP = () => cpState({ cp: { reset: true }, ai: { reset: true }, status: { [PACK]: "stopped" } });

  await run("伙伴：只有 NeoForge / Forge 的服务器有这个标签；安装状态列出三个模组，一键安装后都装好；好友的客户端按清单显示，依赖改成只装服务端时不再标「要装」", async () => {
    try {
      await resetCP();
      await page.goto(url("#/inst/" + inst + "/console")); await settle();
      assert(!(await page.$('.tabs [data-t="companion"]')), "Paper 服不该有伙伴标签");
      await openCP();
      assert(await page.$('.tabs [data-t="companion"].cur'), "Forge 服应有伙伴标签并选中");
      const mods = await page.$$eval("#cp-install [data-mod]", rs => rs.map(r => r.dataset.mod + ":" + r.querySelector(".tag").textContent));
      assert(JSON.stringify(mods) === JSON.stringify(["minedeck_companion:没装", "playerengine:没装", "architectury:已装好"]), "三个模组的状态：" + mods);
      const act = await page.textContent("#cp-install");
      assert(act.includes("要从 Modrinth 下载约 4.3 MB") && act.includes("实测过的是 1.20.1-1.4.0"), "安装行：" + act);
      assert(await page.$('#cp-install [data-mod="playerengine"] a[href="https://modrinth.com/mod/playerengine/version/wFlD9yTq"]'), "依赖附 Modrinth 链接");
      const gate = await page.textContent("#cp-gate");
      assert(gate.includes("AI 还没配置") && await page.$('#cp-gate a[href="#/settings/ai"]'), "没配置 AI 时提示去设置：" + gate);
      const t0 = posts.length;
      await page.click("#cp-install-btn"); await settle(500);
      assert(postsTo(t0, "/companion/install").length === 1, "应发安装请求");
      assert(await toastHas("已安装：minedeck-companion-forge-1.20.1-0.2.0.jar、playerengine-forge-1.20.1-1.4.0.jar"), "应提示装了什么");
      assert((await page.textContent("#cp-install")).includes("三个都装好了") && !(await page.$("#cp-install-btn")), "装好以后不再给安装按钮");
      // 好友的客户端：0.2.0 起两个依赖只装服务端，好友只装伙伴模组（随 MineDeck 内置，打开 mods 目录发给好友）
      const cl = await page.$$eval("#cp-client [data-client]", rs => rs.map(r => r.dataset.client + ":" + r.querySelector(".tag").textContent));
      assert(JSON.stringify(cl) === JSON.stringify(["minedeck_companion:要装", "playerengine:不用装", "architectury:不用装"]), "side 是 server 的依赖不用装：" + cl);
      assert(await page.$("#cp-mods-dir"), "打开 mods 目录");
      // 清单里依赖的 side 是 both 时（老装法），好友三个都要装，两个依赖附 Modrinth 链接
      await cpState({ cp: { clientSide: "both" } });
      await openCP();
      const cl2 = await page.$$eval("#cp-client [data-client]", rs => rs.map(r => r.dataset.client + ":" + r.querySelector(".tag").textContent));
      assert(JSON.stringify(cl2) === JSON.stringify(["minedeck_companion:要装", "playerengine:要装", "architectury:要装"]), "好友要装的：" + cl2);
      assert(await page.$('#cp-client a[href="https://modrinth.com/mod/architectury-api/version/1MKTLiiG"]'), "依赖附下载页");
      // 运行中要替换旧版本：先停服
      await cpState({ cp: { reset: true, inst: { [PACK]: { mods: { minedeck_companion: { state: "outdated", version: "0.0.9", files: [{ file: "minedeck-companion-forge-1.20.1-0.0.9.jar", version: "0.0.9", keep: false }] } } } } }, status: { [PACK]: "running" } });
      await openCP();
      assert((await page.textContent("#cp-install")).includes("先停服再安装") && await page.$eval("#cp-install-btn", b => b.disabled), "运行中不能替换旧版本");
      assert((await page.textContent('#cp-install [data-mod="minedeck_companion"]')).includes("安装时删掉：minedeck-companion-forge-1.20.1-0.0.9.jar"), "列出要删掉的旧版本");
    } finally { await resetCP(); }
  });

  await run("伙伴：开关即时生效（没装伙伴模组时不能打开）；伙伴上限、不许打的生物单字段保存，填错就近提示；禁挖区域在弹窗里添加（填错就近提示）、编辑、删除", async () => {
    try {
      await resetCP();
      await openCP();
      assert(await page.$eval("#cp-on", e => e.disabled) && (await page.textContent("#cp-on-sub")).includes("先装伙伴模组"), "没装时不能打开");
      await cpState({ cp: { inst: { [PACK]: { mods: CP_INSTALLED } } } });
      await openCP();
      let t0 = reqs.length;
      await page.click("#cp-on + .sw"); await settle(300);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ enabled: true }]) && await toastHas("已打开 AI 伙伴"), "开关应即时发 PUT：" + JSON.stringify(cpPuts(t0)));
      assert(await page.$eval("#cp-max-save", b => b.disabled), "没改时保存键置灰");
      await page.fill("#cp-max", "0"); await page.press("#cp-max", "Enter"); await settle(200);
      assert((await page.textContent("#cp-max-err")).includes("1 到 16") && await page.$("#cp-max.invalid"), "上限填错就近提示");
      t0 = reqs.length;
      await page.fill("#cp-max", "2"); await page.press("#cp-max", "Enter"); await settle(300);
      await page.fill("#cp-deny", "Villager!"); await page.click("#cp-deny-save"); await settle(200);
      assert((await page.textContent("#cp-deny-err")).includes("不是生物 ID"), "生物 ID 填错就近提示");
      await page.fill("#cp-deny", "villager，minecraft:cat"); await page.click("#cp-deny-save"); await settle(300);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ maxCompanions: 2 }, { attackDeny: ["villager", "minecraft:cat"] }]), "单字段保存：" + JSON.stringify(cpPuts(t0)));
      assert((await page.inputValue("#cp-deny")) === "minecraft:villager, minecraft:cat" && await toastHas("不许打：villager、cat"), "保存后按规范写法显示");
      t0 = reqs.length;
      await page.click("#cp-deny-default"); await settle(300);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ denyDefault: true }]) && (await page.inputValue("#cp-deny")).includes("minecraft:wandering_trader"), "恢复默认");
      // 禁挖区域：弹窗里填，填错就近提示
      assert((await page.textContent("#cp-zones")).includes("还没有禁挖区域"), "没有区域时说明");
      await page.click("#cp-zone-add"); await page.waitForSelector(".modal");
      await page.fill("#cz-name", "我的家"); await page.click('.modal [data-x="ok"]'); await settle(150);
      assert((await page.textContent("#cz-err")).includes("字母、数字") && await page.$("#cz-name.invalid"), "区域名填错就近提示");
      await page.fill("#cz-name", "base");
      for (const [k, val] of [["ax", "-120"], ["ay", "-64"], ["az", "-80"], ["bx", "-40"], ["by", "3200"], ["bz", "10"]]) await page.fill("#cz-" + k, val);
      await page.click('.modal [data-x="ok"]'); await settle(150);
      assert((await page.textContent("#cz-err")).includes("角 2 的 y") && await page.$("#cz-by.invalid"), "坐标超范围就近提示");
      await page.fill("#cz-by", "320");
      t0 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(400);
      const zone = { name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] };
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ zones: [zone] }]) && !(await page.$(".modal")), "添加区域：" + JSON.stringify(cpPuts(t0)));
      assert((await page.textContent('#cp-zones [data-zone="base"]')).includes("主世界 · (-120, -64, -80) → (-40, 320, 10)"), "区域行");
      // 编辑：改名、换成模组的维度
      await page.click('[data-zedit="base"]'); await page.waitForSelector(".modal");
      await page.fill("#cz-name", "home"); await page.selectOption("#cz-dim", "");
      assert(!(await page.$eval("#cz-dim-row", e => e.hidden)), "选「其他」时要填维度 ID");
      await page.fill("#cz-dim-custom", "twilightforest:twilight_forest");
      t0 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ zones: [{ ...zone, name: "home", dim: "twilightforest:twilight_forest" }] }]), "编辑区域：" + JSON.stringify(cpPuts(t0)));
      t0 = reqs.length;
      await page.click('[data-zdel="home"]'); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ zones: [] }]) && (await page.textContent("#cp-zones")).includes("还没有禁挖区域"), "删除区域");
    } finally { await resetCP(); }
  });

  await run("伙伴：连上伙伴模组才能刷出；刷出（弹窗校验名字）、停下、全部停下、移除（确认，可选掉落背包）；「设置 → AI」有 AI 伙伴的每日额度", async () => {
    try {
      await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true } } } }, ai: { reset: true, ...AI_DEEPSEEK, key: "sk-x" } });
      await openCP();
      assert((await page.textContent("#cp-link")).includes("等服务器开好") && await page.$eval("#cp-spawn", b => b.disabled), "服务器没开时不能刷出");
      assert((await page.textContent("#cp-gate")).includes("游戏内 AI"), "游戏内 AI 没开时提醒");
      // 关服时模组先发 END 断开，MineDeck 不再重连（两半联调）：连接是 waiting，说明「正在关闭」，不说「还在启动」
      await cpState({ status: { [PACK]: "stopping" } });
      await openCP();
      assert((await page.textContent("#cp-link")).includes("服务器正在关闭"), "关服时：" + await page.textContent("#cp-link"));
      await cpState({ status: { [PACK]: "running" } });
      await openCP();
      assert((await page.textContent("#cp-link")).includes("已连上") && (await page.textContent("#cp-list")).includes("还没有伙伴"), "连上以后：" + await page.textContent("#cp-link"));
      await page.click("#cp-spawn"); await page.waitForSelector(".modal");
      await page.fill("#cs-name", "小 方"); await page.click('.modal [data-x="ok"]'); await settle(150);
      assert((await page.textContent("#cs-err")).includes("空格") && await page.$("#cs-name.invalid"), "名字不合规就近提示");
      await page.fill("#cs-name", "小方"); await page.fill("#cs-owner", "Steve"); await page.selectOption("#cs-near", "Alex");
      const p0 = posts.length;
      await page.click('.modal [data-x="ok"]'); await settle(500);
      const sp = postsTo(p0, "/companion/spawn");
      assert(sp.length === 1 && JSON.stringify(JSON.parse(sp[0].b)) === JSON.stringify({ name: "小方", owner: "Steve", near: "Alex" }), "刷出的请求：" + JSON.stringify(sp));
      assert(await toastHas("刷出了伙伴「小方」") && (await page.textContent('#cp-list [data-comp="1"]')).includes("主人 Steve"), "列表里有小方");
      assert((await page.textContent("#cp-events")).includes("刷出了伙伴「小方」（主人 Steve）"), "最近的事件");
      await page.click("#cp-spawn"); await page.waitForSelector(".modal");
      await page.fill("#cs-name", "小方"); await page.click('.modal [data-x="ok"]'); await settle(150);
      assert((await page.textContent("#cs-err")).includes("已经有一个叫「小方」"), "重名就近提示");
      await page.keyboard.press("Escape"); await settle(200);
      // 派个任务：能停下、全部停下
      await cpState({ cp: { busy: true } });
      await openCP();
      assert((await page.textContent('#cp-list [data-comp="1"]')).includes("正在采集 oak_log ×3"), "显示当前任务");
      let p1 = posts.length;
      await page.click('[data-cstop="1"]'); await settle(400);
      assert(JSON.stringify(JSON.parse(postsTo(p1, "/companion/stop")[0].b)) === JSON.stringify({ companion: 1 }) && await toastHas("已停下"), "停下一个");
      assert((await page.textContent('#cp-list [data-comp="1"]')).includes("上次：采集 oak_log ×3 停下了") && await page.$eval("#cp-stopall", b => b.disabled), "停下后显示上次的结果");
      await cpState({ cp: { busy: true } });
      await openCP();
      p1 = posts.length;
      await page.click("#cp-stopall"); await settle(400);
      assert(JSON.stringify(JSON.parse(postsTo(p1, "/companion/stop")[0].b)) === JSON.stringify({ all: true }), "全部停下");
      // 移除：确认，默认聚焦取消；勾上掉落背包
      await page.click('[data-cdel="1"]'); await page.waitForSelector(".modal");
      const focused = await page.evaluate(() => document.activeElement && document.activeElement.textContent.trim());
      assert(focused === "取消", "移除的确认框默认聚焦取消：" + focused);
      await page.click('.modal .switch .sw');
      p1 = posts.length;
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(JSON.stringify(JSON.parse(postsTo(p1, "/companion/despawn")[0].b)) === JSON.stringify({ companion: 1, drop: true }) && await toastHas("已移除伙伴「小方」"), "移除的请求");
      assert(!(await page.$('#cp-list [data-comp="1"]')), "移除后不在列表里");
      // 「设置 → AI」：AI 伙伴的每日额度
      await page.goto(url("#/settings/ai")); await settle();
      assert((await page.textContent("#q-sub-companion")).includes("今天已用 2,400 token"), "AI 伙伴的用量：" + await page.textContent("#q-sub-companion"));
      const t0 = reqs.length;
      await page.fill("#q-companion", "3"); await page.press("#q-companion", "Enter"); await settle(300);
      assert(JSON.stringify(putBody(t0, "/api/ai/quota")) === JSON.stringify({ companion: 30000 }) && await toastHas("AI 伙伴每天最多 3 万 token"), "AI 伙伴的额度按万 token 保存");
      await openCP();
      assert((await page.textContent("#cp-quota")).includes("每天最多 3 万"), "伙伴页显示额度：" + await page.textContent("#cp-quota"));
    } finally { await resetCP(); }
  });

  await run("伙伴：手机宽度（390px）下不横向溢出，弹窗里的坐标一列一个", async () => {
    await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true, zones: [{ name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] }] } } } }, status: { [PACK]: "running" } });
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/inst/" + encodeURIComponent(PACK) + "/companion"); await p.waitForTimeout(1200);
      const overflow = () => p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
        wide: [...document.querySelectorAll("#tab-body *, .modal *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.id || e.className).slice(0, 5) }));
      let w = await overflow();
      assert(w.sw <= w.cw && w.wide.length === 0, "横向溢出：" + JSON.stringify(w));
      await p.click("#cp-zone-add"); await p.waitForSelector(".modal"); await p.waitForTimeout(300);
      w = await overflow();
      assert(w.sw <= w.cw && w.wide.length === 0, "弹窗横向溢出：" + JSON.stringify(w));
    } finally { await c.close(); await resetCP(); }
  });
  // ==================== 第七阶段结束 ====================

  // ==================== MCP 接口（mcp.js，助手面板顶上的「外部 AI 的请求」在 assistant.js）====================
  // 仿真后端的场景见 server.js 的 mcpMockState：enabled、portChanged、clients、propose（模拟外部 AI 提出一个写操作）
  const mcpState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ mcp: x }) }).then(r => r.json()).then(j => j.data.mcp), b);
  const MCP_PROPS = { op: "instance.props.set", title: "修改 server.properties", level: "W", actor: "Claude Desktop",
    args: { inst: "生存服 · 朋友", pairs: [{ key: "max-players", value: "10" }] },
    plan: { summary: "把「生存服 · 朋友」的 max-players 从 20 改成 10", changes: [{ what: "max-players", before: "20", after: "10" }] } };
  const MCP_RESTART = { op: "instance.restart", title: "重启服务器", level: "D", actor: "Cursor", args: { inst: "生存服 · 朋友" },
    plan: { summary: "重启「生存服 · 朋友」", warning: "重启：在线的 3 人会掉线" } };
  const stubClipboard = p => p.evaluate(() => {
    window.__copied = [];
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied.push(t); } } });
  });

  await run("MCP 接口：默认关着；打开后有地址、令牌和复制配置（预览里令牌是占位，复制的是完整的）；重置令牌要确认；关掉后不再显示", async () => {
    await mcpState({ reset: true }); await aiState({ reset: true });
    try {
      await page.goto(url("#/settings/ai")); await settle(700);
      const off = await page.evaluate(() => ({ sw: !!document.querySelector("#mcp-on") && !document.querySelector("#mcp-on").checked,
        url: !!document.querySelector("#mcp-url"), head: (document.querySelector("#ai-mcp-slot") || {}).textContent || "" }));
      assert(off.sw && !off.url && off.head.includes("MCP 接口") && off.head.includes("默认关闭"), "默认：" + JSON.stringify(off).slice(0, 200));
      let t0 = reqs.length;
      await page.click("#ai-mcp-slot .switch .sw"); await settle(400);
      assert((putBody(t0, "/api/mcp") || {}).enabled === true, "打开应发 enabled: true");
      assert(await toastHas("MCP 接口已打开"), "应提示已打开");
      const on = await page.evaluate(() => ({ key: (document.querySelector("#mcp-group .ri-key") || {}).textContent || "", reset: !!document.querySelector("#mcp-reset"),
        copy: !!document.querySelector("#mcp-copy"), clients: (document.querySelector("#mcp-clients") || {}).textContent || "" }));
      assert(on.key === "http://127.0.0.1:27333/mcp" && on.reset && on.copy && on.clients.includes("还没有 AI 工具连过"), "打开后：" + JSON.stringify(on));
      const tok = (await mcpState({})).token;
      await stubClipboard(page);
      await page.click("#mcp-copy"); await settle(250);
      const items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent.trim()));
      assert(items.length === 6 && items[0] === "Claude Desktop（stdio）" && items.includes("Cherry Studio（HTTP）") && items.includes("VS Code（HTTP）"), "复制给谁：" + items);
      await page.click('.dropmenu button:has-text("Cherry Studio")'); await page.waitForSelector(".modal .mcp-pre");
      const pv = await page.evaluate(() => ({ pre: document.querySelector(".modal .mcp-pre").textContent, body: document.querySelector(".modal .m-body").textContent }));
      assert(pv.pre.includes('"streamableHttp"') && pv.pre.includes("<令牌，复制时自动带上>") && !pv.pre.includes(tok) && pv.body.includes("从 JSON 导入"), "预览：" + JSON.stringify(pv).slice(0, 300));
      await page.click('.modal [data-x="ok"]'); await settle(300);
      const copied = await page.evaluate(() => window.__copied.pop() || "");
      assert(copied.includes("Bearer " + tok) && JSON.parse(copied).mcpServers.minedeck.url === "http://127.0.0.1:27333/mcp", "复制的内容：" + copied);
      assert(await toastHas("已复制 Cherry Studio 的配置"), "应提示已复制");
      await page.click("#mcp-copy"); await settle(250);
      await page.click('.dropmenu button:has-text("Claude Desktop")'); await page.waitForSelector(".modal .mcp-pre");
      const cl = await page.textContent(".modal .mcp-pre");
      assert(cl.includes('"-mcp"') && cl.includes("MineDeck.exe") && !cl.includes("令牌"), "Claude Desktop 用 stdio、不带令牌：" + cl);
      await page.keyboard.press("Escape"); await settle(250);
      t0 = reqs.length;
      await page.click("#mcp-reset"); await settle(250);
      assert((await page.textContent(".modal")).includes("旧令牌马上失效"), "重置前要确认");
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(reqs.slice(t0).some(r => r.m === "POST" && r.u === "/api/mcp/token") && (await mcpState({})).token !== tok, "应换了令牌");
      assert(await toastHas("已重置令牌"), "应提示已重置");
      t0 = reqs.length;
      await page.click("#ai-mcp-slot .switch .sw"); await settle(400);
      assert((putBody(t0, "/api/mcp") || {}).enabled === false && !(await page.$("#mcp-url")) && !(await page.$("#mcp-copy")), "关掉后不再显示地址和复制配置");
    } finally { await page.keyboard.press("Escape"); await mcpState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("MCP 接口：外部 AI 提出写操作——提示、标题栏「AI 助手」亮小方块；面板顶上是同样的确认卡片（没配置 AI 也能处理），确认后按提案执行；拒绝的理由交回；危险操作要多勾一次", async () => {
    await mcpState({ reset: true, enabled: true }); await aiState({ reset: true }); await asstState({ reset: true });
    try {
      await page.goto(url("#/instances")); await settle(700);
      assert(await page.$eval("#ai-btn .tb-dot", d => d.hidden), "没有请求时不亮");
      await mcpState({ propose: MCP_PROPS });
      assert(await toastHas("外部 AI「Claude Desktop」请求：修改 server.properties"), "来了新请求要提示");
      const btn = await page.$eval("#ai-btn", b => ({ dot: !b.querySelector(".tb-dot").hidden, title: b.title }));
      assert(btn.dot && btn.title.includes("外部 AI 有 1 个操作等你确认"), "标题栏入口：" + JSON.stringify(btn));
      await page.click('.toast.ext'); await page.waitForSelector("#ai-panel #ap-ext .ap-card.pending");
      await page.waitForFunction(() => document.querySelector("#ap-body").textContent.includes("AI 还没配置"), null, { timeout: 3000 }).catch(() => {});
      const v = await page.evaluate(() => ({ ext: document.querySelector("#ap-ext").textContent.replace(/\s+/g, " "), gate: document.querySelector("#ap-body").textContent,
        n: document.querySelectorAll("#ap-ext .ax-item").length, dot: !document.querySelector("#ai-btn .tb-dot").hidden }));
      assert(v.n === 1 && v.ext.includes("外部 AI 的请求") && v.ext.includes("Claude Desktop") && v.ext.includes("max-players") && v.ext.includes("写操作") && !v.dot,
        "面板顶上的卡片：" + JSON.stringify(v).slice(0, 300));
      assert(v.gate.includes("AI 还没配置"), "没配置 AI 时下面照样是引导，外部 AI 的请求也能处理");
      assert((await mcpState({})).executed.length === 0, "没确认不能执行");
      await page.click('#ap-ext [data-x="ok"]'); await settle(400);
      let st = await mcpState({});
      assert(st.executed.length === 1 && JSON.stringify(st.executed[0].args) === JSON.stringify(MCP_PROPS.args) && st.executed[0].actor === "Claude Desktop", "确认后按提案执行：" + JSON.stringify(st.executed));
      assert(await toastHas("已确认，已经执行") && await page.$eval("#ap-ext", e => e.hidden), "确认后卡片拿掉");
      // 危险操作：要多勾一次
      await mcpState({ propose: MCP_RESTART }); await page.waitForSelector("#ap-ext .ap-card.danger.pending");
      const ok = await page.$eval('#ap-ext .ap-card.danger [data-x="ok"]', b => ({ dis: b.disabled, cls: b.className }));
      assert(ok.dis && ok.cls.includes("danger solid") && (await page.textContent("#ap-ext")).includes("在线的 3 人会掉线"), "危险操作没勾时：" + JSON.stringify(ok));
      await page.check('#ap-ext .ap-card.danger [data-k="ack"]');
      await page.click('#ap-ext .ap-card.danger [data-x="ok"]'); await settle(400);
      st = await mcpState({});
      assert(st.executed.length === 2 && st.executed[1].op === "instance.restart" && st.executed[1].ack === true, "危险操作带上第二次确认：" + JSON.stringify(st.executed));
      // 拒绝：理由交回给外部 AI；两张卡片时只处理那一张，另一张勾的框、写的理由不受影响
      await mcpState({ propose: [MCP_PROPS, { ...MCP_PROPS, actor: "VS Code" }] }); await page.waitForFunction(() => document.querySelectorAll("#ap-ext .ax-item").length === 2);
      await page.click('#ap-ext .ax-item >> nth=1 >> [data-x="reject"]');
      await page.fill("#ap-ext .ax-item >> nth=1 >> .pc-reason input", "周末人多");
      await page.click('#ap-ext .ax-item >> nth=0 >> [data-x="reject"]');
      await page.fill("#ap-ext .ax-item >> nth=0 >> .pc-reason input", "先别动");
      await page.press("#ap-ext .ax-item >> nth=0 >> .pc-reason input", "Enter"); await settle(400);
      st = await mcpState({});
      assert(st.rejected.length === 1 && st.rejected[0].reason === "先别动", "拒绝的理由：" + JSON.stringify(st.rejected));
      assert((await page.inputValue("#ap-ext .ax-item >> nth=0 >> .pc-reason input")) === "周末人多", "另一张卡片写了一半的理由要留着");
      await page.keyboard.press("Escape"); await settle(300);
      assert(!(await page.$("#ai-panel")) && !(await page.$eval("#ai-btn .tb-dot", d => d.hidden)), "关掉面板后还有一个在等：小方块亮着");
    } finally { await page.keyboard.press("Escape"); await mcpState({ reset: true }); await aiState({ reset: true }); await asstState({ reset: true }); }
  });

  await run("MCP 接口：局域网打开只能看——开关禁用，没有令牌和复制配置，不连事件流，也收不到外部 AI 的请求", async () => {
    await mcpState({ reset: true, enabled: true, clients: [{ name: "claude-ai", title: "Claude", version: "0.14.2", transport: "stdio", protocol: "2025-06-18",
      first: new Date().toISOString(), last: new Date().toISOString(), calls: 3 }] });
    await aiState({ reset: true, local: false });
    try {
      const t0 = reqs.length;
      await page.goto(url("#/settings/ai")); await settle(800);
      const v = await page.evaluate(() => ({ sw: document.querySelector("#mcp-on").disabled && document.querySelector("#mcp-on").checked,
        copy: !!document.querySelector("#mcp-copy"), reset: !!document.querySelector("#mcp-reset"), text: document.querySelector("#ai-mcp-slot").textContent }));
      assert(v.sw && !v.copy && !v.reset && v.text.includes("MCP 接口的设置仅支持在本机修改") && v.text.includes("Claude"), "局域网：" + JSON.stringify(v).slice(0, 300));
      await mcpState({ propose: MCP_PROPS }); await settle(500);
      assert(!reqs.slice(t0).some(r => r.u === "/api/mcp/events"), "局域网不该连外部 AI 请求的事件流");
      assert(!(await toastHas("外部 AI", 300)), "局域网不提示外部 AI 的请求");
    } finally { await aiState({ reset: true }); await mcpState({ reset: true }); }
  });

  await run("MCP 接口：端口被占用时写明 HTTP 方式的配置要改；连过的 AI 工具列出名字、方式和协议；手机宽度（390px）下不横向溢出", async () => {
    const now = new Date().toISOString();
    await mcpState({ reset: true, enabled: true, portChanged: true, clients: [
      { name: "claude-ai", title: "Claude", version: "0.14.2", transport: "stdio", protocol: "2025-06-18", first: now, last: now, calls: 3 },
      { name: "cursor-vscode", version: "2.3.1", transport: "http", protocol: "2026-07-28", first: now, last: now, calls: 0 }] });
    await aiState({ reset: true });
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      await page.goto(url("#/settings/ai")); await settle(800);
      const d = await page.evaluate(() => ({ warn: (document.querySelector("#mcp-group .warn-text") || {}).textContent || "",
        key: document.querySelector("#mcp-group .ri-key").textContent, rows: [...document.querySelectorAll("#mcp-clients .row-item")].map(r => r.textContent.replace(/\s+/g, " ").trim()) }));
      assert(d.warn.includes("想用的端口被占用") && d.key.includes(":51234/mcp"), "端口被占用：" + JSON.stringify(d));
      assert(d.rows.length === 2 && d.rows[0].includes("Claude") && d.rows[0].includes("stdio") && d.rows[0].includes("2025-06-18") && d.rows[0].includes("调用了 3 次工具") &&
        d.rows[1].includes("cursor-vscode") && d.rows[1].includes("HTTP"), "连过的 AI 工具：" + JSON.stringify(d.rows));
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/settings/ai"); await p.waitForTimeout(900);
      await p.evaluate(() => document.querySelector("#ai-mcp-slot").scrollIntoView());
      const w = await p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
        wide: [...document.querySelectorAll("#ai-mcp-slot *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.className || e.tagName).slice(0, 5) }));
      assert(w.sw <= w.cw && w.wide.length === 0, "手机宽度的设置：" + JSON.stringify(w));
      await mcpState({ propose: MCP_RESTART }); await p.waitForTimeout(500);
      await p.click("#ai-btn"); await p.waitForSelector("#ap-ext .ap-card.danger.pending");
      const pw = await p.evaluate(() => ({ wide: [...document.querySelectorAll("#ap-ext *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1 && !e.closest(".ap-kv")).map(e => e.className || e.tagName).slice(0, 5) }));
      assert(pw.wide.length === 0, "手机宽度的外部 AI 卡片：" + JSON.stringify(pw));
    } finally { await c.close(); await mcpState({ reset: true }); await aiState({ reset: true }); }
  });
  // ==================== MCP 接口结束 ====================

  await run("控制台：快捷键说明常驻在命令框下方，打字后还在；placeholder 只留示例（测试报告观察 7）", async () => {
    await page.goto(url(`#/inst/${inst}/console`)); await settle();
    const ph = await page.getAttribute("#cmd-in", "placeholder");
    assert(!/回车|Tab|翻历史/.test(ph) && ph.includes("say"), "placeholder 应只留示例：" + ph);
    await page.fill("#cmd-in", "say 你好");
    const keys = await page.textContent("#cmd-keys");
    assert(await page.isVisible("#cmd-keys") && ["回车", "发送", "Tab", "补全", "翻历史"].every(s => keys.includes(s)), "打字后快捷键说明应还在：" + keys);
    assert((await page.getAttribute("#cmd-in", "aria-describedby")) === "cmd-keys", "命令框应关联说明（读屏）");
    await page.fill("#cmd-in", "");
  });

  await run("全部配置：搜索按键名和中文名过滤（横线、空格不算），Esc 清空，搜不到时说明；搜索不算未保存的修改（测试报告观察 8）", async () => {
    await page.goto(url(`#/inst/${inst}/raw`)); await settle();
    const rows = () => page.$$eval("table.raw tr", rs => rs.filter(r => r.style.display !== "none").map(r => r.dataset.search));
    const total = (await rows()).length;
    await page.fill("#raw-search", "rcon");
    let vis = await rows();
    assert(vis.length >= 4 && vis.length < total && vis.every(s => s.toLowerCase().includes("rcon")), "搜 rcon：" + JSON.stringify(vis));
    assert((await page.textContent("#raw-count")) === `找到 ${vis.length} 项（共 ${total} 项）`, "计数：" + await page.textContent("#raw-count"));
    for (const q of ["视距", "view distance", "VIEW-DISTANCE"]) {
      await page.fill("#raw-search", q);
      vis = await rows();
      assert(vis.some(s => s.startsWith("view-distance ")) && vis.length < 5, `搜「${q}」：` + JSON.stringify(vis));
    }
    await page.fill("#raw-search", "没有这个键");
    assert((await rows()).length === 0 && await page.isVisible("#raw-none"), "搜不到时应说明");
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("所有修改已保存"), "搜索不该算修改");
    await page.press("#raw-search", "Escape");
    assert((await rows()).length === total && (await page.$eval("#raw-search", e => e.value)) === "" && !(await page.isVisible("#raw-none")), "Esc 应清空搜索");
    assert((await page.textContent("#raw-count")) === `共 ${total} 项`, "清空后计数");
  });

  await run("常用设置：搜索同样忽略横线和空格，整组都不匹配时整组隐藏", async () => {
    await page.goto(url(`#/inst/${inst}/common`)); await settle();
    await page.fill("#cp-search", "view distance");
    const vis = await page.$$eval(".prop-item", els => els.filter(e => e.offsetParent).map(e => e.dataset.search));
    assert(vis.length === 1 && vis[0].includes("view-distance"), "搜 view distance：" + JSON.stringify(vis));
    const groups = await page.$$eval(".cp-group", gs => gs.filter(g => g.style.display !== "none").length);
    assert(groups === 1, "只剩一组：" + groups);
    await page.press("#cp-search", "Escape");
    assert((await page.$$eval(".cp-group", gs => gs.filter(g => g.style.display === "none").length)) === 0, "Esc 后全部回来");
  });

  await run("定时任务：时间是「时:分」文本框，快捷项填成几分钟后的时刻并说明每天执行；写错就近提示，4:5、2130 这类写法补齐成 HH:MM（测试报告观察 1）", async () => {
    await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
    assert((await page.$eval("#sc-at", e => e.type)) === "text", "时间框应是文本框");
    assert((await page.textContent("#sc-hint")).includes("每天到这个时间执行一次"), "旁边应说明每天执行");
    await page.click('[data-later="10"]');
    const [got, a, b] = await page.evaluate(() => {
      const f = d => String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
      return [document.querySelector("#sc-at").value, f(new Date(Date.now() + 10 * 60000)), f(new Date(Date.now() + 9 * 60000))];
    });
    assert(got === a || got === b, `「10 分钟后」填成了 ${got}，应为 ${a}`);
    assert((await page.textContent("#sc-hint")).includes(`已填成 ${got}`) && (await page.textContent("#sc-hint")).includes("之后每天这个时间都执行"), "快捷项后的说明：" + await page.textContent("#sc-hint"));
    const n0 = await page.$$eval("#sched-list .java-row", r => r.length);
    await page.fill("#sc-at", "25:00"); await page.click("#sc-add");
    assert((await page.textContent("#sc-err")).includes("时:分") && await page.$("#sc-at.invalid"), "写错应就近提示：" + await page.textContent("#sc-err"));
    assert((await page.$$eval("#sched-list .java-row", r => r.length)) === n0, "写错不该添加");
    await page.fill("#sc-at", "4:5"); await page.press("#sc-at", "Enter");
    let list = await page.$$eval("#sched-list .java-row .jv", r => r.map(x => x.textContent));
    assert(list.length === n0 + 1 && list[n0] === "每天 04:05", "4:5 应补齐成 04:05：" + JSON.stringify(list));
    assert((await page.$eval("#sc-at", e => e.value)) === "04:05" && !(await page.$("#sc-at.invalid")) && (await page.textContent("#sc-err")) === "", "改对后错误消失");
    await page.fill("#sc-at", "2130"); await page.click("#sc-add");
    list = await page.$$eval("#sched-list .java-row .jv", r => r.map(x => x.textContent));
    assert(list[n0 + 1] === "每天 21:30", "2130 应认成 21:30：" + JSON.stringify(list));
    assert((await page.$eval(".sb-state", el => el.textContent)).includes("2 项修改未保存") || (await page.$eval(".sb-state", el => el.textContent)).includes("未保存"), "添加后计入未保存");
    await page.click('[data-sb="discard"]'); await settle(300);
  });

  await run("部署任务：进度和日志长时间不动时在进度旁提示「下载慢或者在重试」，有进展就收起；后端报了超时时写出哪个地址、第几次（测试报告 B-01）", async () => {
    try {
      await page.goto(url("#/instances")); await settle();
      await page.evaluate(() => { window.__MD_STALL_MS = 1500; location.hash = "#/task/task-7"; });
      await settle(600);
      assert(await page.$eval("#t-slow", e => e.hidden), "刚打开不该提示");
      await page.waitForFunction(() => !document.querySelector("#t-slow").hidden, null, { timeout: 4000 }).catch(() => {});
      const slow = await page.textContent("#t-slow");
      assert(!(await page.$eval("#t-slow", e => e.hidden)) && slow.includes("没有新进度") && slow.includes("检查网络或代理"), "进度不动应提示：" + slow);
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskStage: 3 }) }));
      await page.waitForFunction(() => document.querySelector("#t-slow").hidden, null, { timeout: 1500 }).catch(() => {});
      assert(await page.$eval("#t-slow", e => e.hidden), "有进展后应收起");
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskNet: { count: 3, host: "maven.neoforged.net", what: "连接超时", ago: 4 } }) }));
      await page.waitForFunction(() => !document.querySelector("#t-slow").hidden, null, { timeout: 1200 }).catch(() => {});
      assert((await page.textContent("#t-slow-title")) === "maven.neoforged.net 连接超时，已经 3 次", "后端报的超时：" + await page.textContent("#t-slow-title"));
    } finally {
      await page.evaluate(() => { delete window.__MD_STALL_MS; return fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskStage: 2, taskNet: null }) }); });
    }
  });

  await run("全程无页面报错", async () => { assert(errors.length === 0, errors.slice(0, 3).join(" | ")); });

  await browser.close();
  await mock.close();
  for (const r of results) console.log(r.join("  "));
  const fails = results.filter(r => r[0] === "FAIL").length;
  console.log(`\n${results.length - fails}/${results.length} passed`);
  process.exit(fails ? 1 : 0);
})().catch(e => { console.error(e); process.exit(2); });
