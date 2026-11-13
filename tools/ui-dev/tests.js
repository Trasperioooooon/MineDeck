// 交互回归测试：node tests.js [正则]（只跑名字匹配的用例，如 node tests.js 外观）
// 自带一个全新的仿真后端（随机端口），在无头 Chromium 里操作界面并断言。全部通过时退出码为 0。
"use strict";
const { launch } = require("./browser");
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
  const browser = await launch();
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
    await page.click('.prop-item [data-k="hardcore"]');
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
    await page.click('.prop-item [data-k="hardcore"]');
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
    await page.click("#wnext"); await settle(); // 非原版多了「选模组」一步（可选，直接下一步）
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
    await page.click(".eula-box .switch input");
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

  await run("任务恢复：切页返回、刷新后能找回并行部署、导入结果和失败日志", async () => {
    const taskList = [
      { id: "task-pack", title: "导入整合包 测试包", ended: false, label: "下载模组", done: 50, total: 100 },
      { id: "task-create", title: "创建 Paper 测试服", ended: false },
      { id: "task-failed", title: "创建失败的服务器", ended: true, error: "下载失败" },
    ];
    try {
      await page.evaluate(taskList => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList }) }), taskList);
      await page.goto(url("#/task/task-pack")); await settle();
      await page.click('#sidebar a[data-nav="settings"]'); await settle();
      await page.click('#sidebar a[data-nav="create"]'); await settle();
      assert(await page.locator('#task-list a[href="#/task/task-pack"]').count() === 1, "切回来后导入任务入口丢失");
      assert(await page.locator('#task-list a[href="#/task/task-create"]').count() === 1, "并行创建任务入口丢失");
      await page.reload(); await settle();
      await page.click('#task-list a[href="#/task/task-pack"]'); await settle();
      assert((await page.textContent("#t-label")).includes("下载模组"), "没有恢复真实进度");
      taskList[0] = { ...taskList[0], ended: true, warnings: [{ title: "1 个模组需手动补装" }] };
      await page.evaluate(taskList => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList }) }), taskList);
      await page.click('#sidebar a[data-nav="instances"]'); await settle();
      assert(/已完成，\d+ 条提示/.test(await page.textContent("#task-list")), "列表里看不出导入完成后有告警（摘要的 warningCount）");
      await page.click('#task-list a[href="#/task/task-pack"]'); await settle();
      assert((await page.textContent("#t-msgs")).includes("需手动补装"), "离开期间完成的导入告警丢失");
      await page.click('#sidebar a[data-nav="create"]'); await settle();
      await page.click('#task-list a[href="#/task/task-failed"]'); await settle();
      assert((await page.textContent("#t-msgs")).includes("下载失败"), "失败任务无法回看");
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [] }) }));
    }
  });

  await run("切页竞态：导入页的慢响应不会覆盖服务器列表", async () => {
    await page.goto(url("#/create")); await settle();
    let release, seen;
    const gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    await page.route("**/api/app", async route => { seen(); await gate; await route.continue(); });
    try {
      await page.click("#mode-import"); await requested;
      await page.click('#sidebar a[data-nav="instances"]'); await settle();
      release(); await settle(600);
      assert(await page.locator("#inst-cards").count() === 1 && await page.locator("#im-file").count() === 0, "旧导入页覆盖了当前页面");
    } finally { release(); await page.unroute("**/api/app"); }
  });

  await run("切页竞态：重进向导后旧核心的版本响应不得覆盖新核心", async () => {
    await page.goto(url("#/create")); await settle();
    let release, seen;
    const gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    await page.route("**/api/mcversions?core=paper&*", async route => {
      seen(); await gate;
      await route.fulfill({ json: { ok: true, data: [{ id: "1.0-old", type: "release", latest: true }] } });
    });
    try {
      await page.click("#mode-new"); await settle();
      await page.dblclick('.core-card[data-id="paper"]'); await requested;
      await page.click('#sidebar a[data-nav="create"]'); await settle();
      await page.click("#mode-new"); await settle();
      await page.dblclick('.core-card[data-id="fabric"]'); await settle();
      release(); await settle(600);
      assert(!(await page.textContent("#ver-list")).includes("1.0-old"), "旧核心的版本响应覆盖了新向导");
      assert((await page.textContent("#wf-sum")).includes("Fabric · 26.2"), "新核心的选择被改写");
    } finally { release(); await page.unroute("**/api/mcversions?core=paper&*"); }
  });

  await run("切页竞态：资源搜索只显示最后一次查询，离开后不再写页面", async () => {
    let release, seen;
    let gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    const pattern = "**/resources/search?*";
    await page.route(pattern, async route => {
      const q = new URL(route.request().url()).searchParams.get("q");
      if (q === "old" || q === "leave") { seen(); await gate; }
      await route.fulfill({ json: { ok: true, data: { hits: [{ project_id: q, title: q, description: "测试资源", downloads: 1000 }] } } });
    });
    try {
      await page.goto(url(`#/inst/${inst}/res`)); await settle();
      await page.fill("#res-q", "old"); await page.click("#res-go"); await requested;
      await page.fill("#res-q", "new"); await page.click("#res-go"); await settle();
      release(); await settle(600);
      assert((await page.textContent("#res-list")).includes("new") && !(await page.textContent("#res-list")).includes("old"), "旧查询覆盖了新结果");
      gate = new Promise(r => release = r); requested = new Promise(r => seen = r);
      const beforeErrors = errors.length;
      await page.fill("#res-q", "leave");
      await page.click("#res-go"); await requested;
      await page.click('#sidebar a[data-nav="instances"]'); await settle();
      release(); await settle(600);
      assert(await page.locator("#inst-cards").count() === 1 && errors.length === beforeErrors, "离开后的资源响应引发报错或覆盖页面");
    } finally { if (release) release(); await page.unroute(pattern); }
  });

  for (const [label, hash, endpoint, target] of [
    ["玩家", `#/inst/${inst}/players`, "**/players", `#/inst/${inst}/console`],
    ["备份", `#/inst/${inst}/backups`, "**/backups", `#/inst/${inst}/console`],
    ["定时任务", `#/inst/${inst}/tasks`, "**/policies", `#/inst/${inst}/console`],
    ["常用设置", `#/inst/${inst}/common`, "**/properties", `#/inst/${inst}/console`],
    ["全部配置", `#/inst/${inst}/raw`, "**/properties", `#/inst/${inst}/console`],
    ["内存启动", `#/inst/${inst}/jvm`, "**/api/app", `#/inst/${inst}/console`],
    ["Java 设置", "#/settings/java", "**/api/javas", "#/settings/download"],
  ]) {
    await run(`切页竞态：${label}慢响应不覆盖新标签、不报错`, async () => {
      await page.goto(url("#/instances")); await settle();
      const before = errors.length;
      let release, seen;
      const gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
      await page.route(endpoint, async route => { seen(); await gate; await route.continue(); });
      try {
        await page.evaluate(hash => { location.hash = hash; }, hash); await requested;
        await page.evaluate(hash => { location.hash = hash; }, target); await settle();
        const expected = await page.textContent(target.startsWith("#/settings") ? "#set-body" : "#tab-body");
        release(); await settle(500);
        const actual = await page.textContent(target.startsWith("#/settings") ? "#set-body" : "#tab-body");
        assert(actual === expected && errors.length === before, "旧响应改写了新标签或产生报错：" + errors.slice(before).join(" | "));
      } finally { release(); await page.unroute(endpoint); }
    });
  }

  await run("任务恢复：进度接口暂时失败后自动恢复，保留上次进度", async () => {
    let fail = false, calls = 0;
    await page.route("**/api/tasks/task-retry", async route => {
      calls++;
      if (fail) return route.fulfill({ status: 503, json: { ok: false, error: "临时不可用" } });
      await route.continue();
    });
    try {
      await page.goto(url("#/task/task-retry")); await settle();
      fail = true; await settle(1000);
      assert(await page.locator("#task-log").count() === 1, "一次刷新失败就清掉了进度和日志");
      fail = false; const before = calls; await settle(1800);
      assert(calls > before && await page.locator("#t-fetch-error:not([hidden])").count() === 0, "失败后永久停止轮询");
    } finally { await page.unroute("**/api/tasks/task-retry"); }
  });

  await run("任务恢复：不存在的任务停止轮询并提供返回入口", async () => {
    let calls = 0;
    await page.route("**/api/tasks/task-missing", async route => {
      calls++;
      await route.fulfill({ status: 404, json: { ok: false, error: "任务不存在" } });
    });
    try {
      await page.goto(url("#/task/task-missing")); await settle(1800);
      assert(calls === 1, "不存在的任务仍在反复轮询：" + calls);
      assert(await page.locator('#task-box a[href="#/create"]').count() === 1 && !(await page.textContent("#task-box")).includes("加载中"), "缺少返回入口或仍停在加载中");
    } finally { await page.unroute("**/api/tasks/task-missing"); }
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
    await page.click('[data-cfg="minimizeToTray"]'); await settle(250);
    assert(putConfig(before, "minimizeToTray"), "开关未即时保存");
    await page.click('[data-cfg="minimizeToTray"]'); await settle(250); // 复原
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
      return page.evaluate(() => [...document.querySelectorAll(".hint")].some(h => h.textContent.includes("需配置 API Key")));
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
  // 报错提示「详情」里的原文：点开正文里含 s 的那条报错提示的「详情」，读完按 Esc 关掉
  const toastDetail = async s => {
    await page.locator(".toast.error", { hasText: s }).last().locator('button[aria-label="查看报错详情"]').click();
    const d = await page.textContent(".err-detail");
    await page.keyboard.press("Escape"); await settle(250);
    return d;
  };
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
      // 模型框：手填 + 自己的箭头。没填 Key 时拉不到：错误写在这一行下面（结论接上后端的原因），弹层里给预设的建议
      await page.click("#ai-model-btn"); await settle(300);
      const modelErr = await page.textContent("#ai-model-err");
      assert(modelErr.includes("无法获取模型列表") && modelErr.includes("先填写 Key"), "没填 Key 时获取模型列表应就近提示：" + modelErr);
      let opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.length === 1 && opts[0] === "claude-opus-5-5", "拉不到时给预设里的建议：" + opts);
      await page.keyboard.press("Escape"); await settle(100);
      assert(await page.$eval("#ai-model-pop", e => e.hidden) && !(await page.$eval("#ai-model-btn", b => b.disabled)), "Esc 关掉弹层，箭头还能点");
      await page.fill("#ai-key", "sk-ant-test");
      await page.click("#ai-model-btn"); await settle(300);
      opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.length === 3 && opts.includes("claude-sonnet-5-5"), "模型列表：" + opts);
      assert((await page.textContent("#ai-model-sub")).includes("已有 3 个模型") && (await page.textContent("#ai-model-err")) === "", "应提示有几个模型、清掉上次的错误");
      assert(await page.$eval("#ai-model-list .ai-opt[aria-selected=true]", o => o.dataset.id) === "claude-opus-5-5", "当前模型应打勾");
      // 搜索 + 方向键 + 回车选中；焦点在搜索框时当前项挂在搜索框上（读屏读得到）
      await page.fill("#ai-model-q", "haiku"); await settle(100);
      opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.length === 1 && opts[0] === "claude-haiku-5-5", "搜索：" + opts);
      await page.fill("#ai-model-q", ""); await settle(100);
      await page.keyboard.press("ArrowDown"); await settle(50);
      assert((await page.getAttribute("#ai-model-q", "aria-activedescendant")) && !(await page.getAttribute("#ai-model", "aria-activedescendant")), "当前项应挂在有焦点的搜索框上");
      await page.keyboard.press("Enter"); await settle(100);
      assert((await aiForm()).model === "claude-sonnet-5-5" && await page.$eval("#ai-model-pop", e => e.hidden), "回车选中第二个并关掉弹层：" + (await aiForm()).model);
      // 拉列表的时候点了别处（弹层关了）：箭头不能一直灰着（复审第 9 条）
      await page.route("**/api/ai/models", async r => { await new Promise(x => setTimeout(x, 400)); await r.continue(); });
      await page.click("#ai-list"); await settle(50);
      await page.mouse.click(5, 5); await settle(700);
      await page.unroute("**/api/ai/models");
      assert(await page.$eval("#ai-model-pop", e => e.hidden) && !(await page.$eval("#ai-model-btn", b => b.disabled)), "加载中关掉弹层后箭头应恢复");
      await page.fill("#ai-model", "claude-opus-5-5");
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
      assert(sub.includes("连接正常") && sub.includes("820 ms") && sub.includes("能调用工具") && sub.includes("含一轮工具调用") && sub.includes("168 token"), "测试结果：" + sub);
      await aiState({ testFail: true });
      await page.click("#ai-test"); await settle(300);
      sub = await page.textContent("#ai-test-sub");
      // 原文里带着 HTTP 状态码和服务商的英文原话，整句接不上；接的是后端另给的那句人话（hint），原文收进只有图标的「复制报错信息」
      const raw = await page.getAttribute("#ai-test-sub [data-err-raw]", "data-err-raw");
      assert(sub.trim() === "连接失败：Key 不对或已失效" && (raw || "").includes("HTTP 401") && (await page.getAttribute("#ai-test-sub [data-err-raw]", "aria-label")) === "复制报错信息 AI 测试连接",
        "测试失败时：" + sub + " / 原文：" + raw);
    } finally { await aiState({ reset: true }); }
  });

  await run("AI：小米 MiMo 换到 Claude 协议的地址时按 Claude 处理（思考深度）、Token Plan 地址不拉模型列表给建议；Key 前缀和地址对不上只提醒（只比域名）；自定义的鉴权默认按协议、可改", async () => {
    await aiState({ reset: true });
    try {
      await page.goto(url("#/settings/ai")); await settle();
      await page.selectOption("#ai-preset", "mimo"); await settle(150);
      let f = await aiForm();
      assert(f.url === "https://api.xiaomimimo.com/v1" && f.model === "mimo-v2.6-pro" && !f.effort && !f.proto, "选 MiMo：" + JSON.stringify(f));
      assert(await page.$eval(".ai-auth-row", r => r.hidden) && await page.$eval(".ai-echo-row", r => r.hidden), "预设不显示鉴权和思考回传");
      await page.locator("#ai-alts button", { hasText: "Token Plan · 中国 · Claude 协议" }).click(); await settle(150);
      f = await aiForm();
      assert(f.url === "https://token-plan-cn.xiaomimimo.com/anthropic" && f.effort, "换到 Claude 协议的地址应显示思考深度：" + JSON.stringify(f));
      await page.fill("#ai-key", "sk-payg-0123456789"); await settle(100);
      assert((await aiForm()).keySub.includes("按量付费 Key"), "按量 Key 配 Token Plan 地址应提醒：" + (await aiForm()).keySub);
      await page.fill("#ai-key", "tp-plan-0123456789"); await page.fill("#ai-url", "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages"); await settle(100);
      assert(!(await aiForm()).keySub.includes("Token Plan Key"), "同一个域名多带一截路径不该提醒：" + (await aiForm()).keySub);
      await page.fill("#ai-url", "https://token-plan-cn.xiaomimimo.com/anthropic"); await settle(100);
      const t0 = reqs.length;
      await page.click("#ai-model-btn"); await settle(300);
      const opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts[0] === "mimo-v2.6-pro" && opts.length >= 3 && (await page.textContent("#ai-model-msg")).includes("官方文档没有模型列表"), "Token Plan 给建议：" + opts);
      assert(reqs.slice(t0).filter(r => r.u === "/api/ai/models").length === 1, "只问一次后端（后端不出网）");
      await page.keyboard.press("Escape"); await settle(100);
      await page.click('[data-sb="save"]'); await settle(300);
      let b = putBody(t0, "/api/ai/provider");
      assert(b && b.preset === "mimo" && !("protocol" in b) && !("auth" in b), "预设不传协议和鉴权（后端按地址定）：" + JSON.stringify(b));
      const view = await page.evaluate(() => fetch("/api/ai/settings", { headers: { "X-Token": "mock" } }).then(r => r.json()));
      assert(view.data.provider.protocol === "anthropic", "保存后应按 Claude 协议：" + JSON.stringify(view.data.provider));
      // 自定义：鉴权默认留空（按协议的默认），说明跟着协议变
      await page.selectOption("#ai-preset", "custom"); await settle(150);
      assert(await page.$eval("#ai-auth", s => s.value) === "" && (await page.textContent("#ai-auth-sub")).includes("Authorization: Bearer"), "自定义 OpenAI 兼容：默认 Bearer");
      await page.selectOption("#ai-proto", "anthropic"); await settle(100);
      assert((await page.textContent("#ai-auth-sub")).includes("x-api-key"), "自定义 Claude 协议：默认 x-api-key：" + await page.textContent("#ai-auth-sub"));
      await page.fill("#ai-url", "https://relay.example/v1"); await page.fill("#ai-model", "claude-x");
      await page.selectOption("#ai-auth", "api-key");
      const t1 = reqs.length;
      await page.click('[data-sb="save"]'); await settle(200);
      if (await page.$(".modal")) { await page.click('.modal [data-x="ok"]'); await settle(300); }
      b = putBody(t1, "/api/ai/provider");
      assert(b && b.preset === "custom" && b.protocol === "anthropic" && b.auth === "api-key" && b.echo === "auto", "保存自定义：" + JSON.stringify(b));
      await page.reload(); await settle();
      assert(await page.$eval("#ai-auth", s => s.value) === "api-key", "重新打开后鉴权还在");
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
      // 后端那句自己带着全角冒号：用句号接在结论后面
      assert(await toastHas("无法安装 Simple Voice Chat。在 Modrinth 上查 Simple Voice Chat 的版本失败：连接超时"), "下载失败应报原因");
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
        return { h: !!md.querySelector("h3.md-h.md-h3"), nest: !!md.querySelector("ol li ul li"), table: !!md.querySelector("table td"), pre: !!md.querySelector("pre code"),
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
      await page.click("#ai-assist-slot .switch input"); await settle(300);
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
      assert(await p.$eval(".ap-resize", e => getComputedStyle(e).display === "none"), "手机宽度下面板占满，拖宽手柄应隐藏（也就不会被 Tab 到）");
      await p.focus("#ap-in"); await p.keyboard.press("2"); await p.waitForTimeout(200);
      assert(p.url().endsWith("#/instances"), "输入框里按 2 不应切页：" + p.url());
    } finally { await c.close(); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  // 思考、流式重画、拖宽手柄。假 AI 听到「卡」：第一轮只有思考和工具调用，第二轮先思考再出正文，最后给一张确认卡片（见 server.js 的 asstScript）
  // 开一个不关联服务器的新会话，面板停在对话里
  const newChat = async () => {
    await page.goto(url("#/instances")); await settle(500);
    await page.evaluate(() => localStorage.removeItem("amh_ai_sid"));
    await page.click("#ai-btn"); await settle(300);
    await page.click('#ap-body [data-x="new"]'); await settle(200);
    await page.click(".dropmenu button >> text=不关联服务器"); await settle(300);
  };
  // 一次取下所有助手消息的样子（同一时刻的）：思考的摘要、是否展开、思考有多少字，有没有正文，「正在…」那一行，工具的状态；mark 是测试自己打在元素上的记号，整条重画后就没了
  const asstSnap = () => page.evaluate(() => ({
    busy: !(document.querySelector("#ap-stop") || { hidden: true }).hidden,
    msgs: [...document.querySelectorAll("#ap-body .ap-msg.assistant")].map(m => {
      const d = m.querySelector(":scope > .ap-think"), ty = m.querySelector(":scope > .ap-typing");
      return { sum: d ? d.querySelector("summary").textContent : "", open: !!(d && d.open), think: d ? d.querySelector(".ap-think-body").textContent.length : 0,
        md: !!m.querySelector(":scope > .md"), typing: ty ? ty.textContent.trim() : "",
        tools: [...m.querySelectorAll(".ap-tool")].map(t => t.className.replace("ap-tool ", "")), mark: m.dataset.mark || "" };
    }),
  }));
  // 等到某个中间状态出现，返回那一刻的样子（条件和断言看的是同一份，不会等到了再去取时已经变了）
  const snapWhen = async (cond, what, timeout = 6000) => {
    const t0 = Date.now();
    for (;;) {
      const s = await asstSnap();
      if (cond(s.msgs, s)) return s;
      if (Date.now() - t0 > timeout) throw new Error("没等到「" + what + "」，最后看到的是 " + JSON.stringify(s).slice(0, 500));
      await page.waitForTimeout(15);
    }
  };
  const J = x => JSON.stringify(x).slice(0, 500);

  await run("AI 助手：思考默认折叠，进行中摘要写「思考中…」，开始调工具或有了正文就写「思考」；下一轮开始后上一条不停在「思考中…」；展开的思考在流式更新和整条重画后保持展开", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await asstState({ slow: 250 }); // 每一小段隔 250 毫秒，中间状态才看得见
      await page.fill("#ap-in", "服务器有点卡"); await page.press("#ap-in", "Enter");
      // 第一轮：只有思考和工具调用，一个字的正文都没有
      let s = await snapWhen(m => m.length === 1 && !!m[0].sum, "第一条开始思考");
      assert(s.msgs[0].sum === "思考中…" && !s.msgs[0].open && !s.msgs[0].md && s.busy, "思考进行中、还没正文：默认折叠，摘要写「思考中…」：" + J(s));
      const len0 = s.msgs[0].think;
      await page.click("#ap-body .ap-msg.assistant .ap-think > summary");
      await page.evaluate(() => { document.querySelector("#ap-body .ap-msg.assistant").dataset.mark = "1"; });
      s = await snapWhen(m => m[0].think > len0, "思考的文字接着增加");
      assert(s.msgs[0].open && s.msgs[0].mark === "1" && s.msgs[0].sum === "思考中…", "流式更新时展开的思考应保持展开（元素没换）：" + J(s));
      // 模型开始写工具调用（工具还没进这一条）：思考已经结束了
      s = await snapWhen(m => m[0].typing.startsWith("正在调用"), "开始调用工具");
      assert(s.msgs[0].sum === "思考" && s.msgs[0].open, "开始调工具后摘要不该还是「思考中…」：" + J(s));
      // 工具执行中：这一条整条重画过（记号没了），思考仍然展开
      s = await snapWhen(m => m[0].tools.some(t => t.includes("running")), "工具执行中");
      assert(s.msgs[0].sum === "思考" && s.msgs[0].open && !s.msgs[0].mark && !s.msgs[0].md && s.busy, "工具执行期间：" + J(s));
      // 第二轮开始：上一条不再是最后一条
      s = await snapWhen(m => m.length === 2 && !!m[1].sum, "第二条开始思考");
      assert(s.msgs[0].sum === "思考" && s.msgs[0].open && !s.msgs[0].typing && s.msgs[0].tools.join() === "done", "下一轮开始后，上一条的摘要不该停在「思考中…」：" + J(s));
      assert(s.msgs[1].sum === "思考中…" && !s.msgs[1].open && !s.msgs[1].md && s.busy, "第二条思考进行中：" + J(s));
      // 正文来了，回复还没结束
      s = await snapWhen(m => m[1].md, "第二条开始出正文");
      assert(s.msgs[1].sum === "思考" && !s.msgs[1].open && s.busy, "有了正文之后摘要应是「思考」：" + J(s));
      await asstState({ slow: 0 });
      await page.waitForSelector("#ap-body .ap-card.pending", { timeout: 8000 }); await idle();
      s = await asstSnap();
      assert(s.msgs.length === 2 && s.msgs.every(x => x.sum === "思考" && !x.typing && x.think > 20) && s.msgs[0].open && !s.msgs[1].open && !s.busy, "整个回复结束后：" + J(s));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：思考到一半点停止，这一条标成没说完，摘要不停在「思考中…」", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await asstState({ slow: 250 });
      await page.fill("#ap-in", "服务器有点卡"); await page.press("#ap-in", "Enter");
      let s = await snapWhen(m => m.length === 1 && m[0].sum === "思考中…", "开始思考");
      await page.click("#ap-stop");
      await idle();
      s = await asstSnap();
      const body = await page.textContent("#ap-body");
      assert(s.msgs.length === 1 && s.msgs[0].sum === "思考" && !s.msgs[0].typing && !s.msgs[0].md && !s.msgs[0].tools.length, "停下之后摘要不该还是「思考中…」：" + J(s));
      assert(body.includes("没说完就停了") && body.includes("已停止"), "应标出没说完、已停止：" + body.slice(0, 200));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：确认卡片处理后这一条整条重画，正文里代码块的横向滚动位置和展开的思考都留着", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await page.fill("#ap-in", "服务器有点卡"); await page.press("#ap-in", "Enter");
      await page.waitForSelector("#ap-body .ap-card.pending", { timeout: 5000 }); await idle();
      await page.click("#ap-body .ap-msg.assistant:has(.ap-card) .ap-think > summary");
      const before = await page.evaluate(() => {
        const m = document.querySelector("#ap-body .ap-card").closest(".ap-msg");
        m.dataset.mark = "1";
        const pre = m.querySelector(".md-pre");
        pre.scrollLeft = 90;
        return { left: pre.scrollLeft, room: pre.scrollWidth - pre.clientWidth, open: m.querySelector(".ap-think").open };
      });
      assert(before.left === 90 && before.room > 120 && before.open, "那行长代码应该能横向滚动：" + J(before));
      await page.click('.ap-card.pending [data-x="ok"]');
      await reply("改好了"); await idle();
      const after = await page.evaluate(() => {
        const m = document.querySelector("#ap-body .ap-card").closest(".ap-msg");
        return { mark: m.dataset.mark || "", left: m.querySelector(".md-pre").scrollLeft, open: m.querySelector(".ap-think").open, out: m.querySelector(".pc-out").textContent };
      });
      assert(!after.mark && after.out.includes("已确认"), "确认后这一条应整条重画：" + J(after));
      assert(after.left === 90 && after.open, "重画后滚动位置和展开的思考应留着：" + J(after));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：拖宽手柄能用键盘（← 变宽、→ 变窄，按住 Shift 步子大），松键后才记下，到头不乱跳，刷新后还在；拖到一半被打断也会收尾", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    const W = () => page.evaluate(() => {
      const h = document.querySelector(".ap-resize");
      return { v: document.documentElement.style.getPropertyValue("--ai-w"), panel: Math.round(document.querySelector("#ai-panel").getBoundingClientRect().width),
        saved: localStorage.getItem("amh_ai_w"), now: h.getAttribute("aria-valuenow"), min: h.getAttribute("aria-valuemin"), max: h.getAttribute("aria-valuemax"),
        drag: h.classList.contains("drag"), focus: document.activeElement === h };
    });
    const openPanel = async () => { await page.goto(url("#/instances")); await settle(500); await page.click("#ai-btn"); await page.waitForSelector("#ai-panel"); await settle(300); };
    const grip = () => page.$eval(".ap-resize", e => { const r = e.getBoundingClientRect(); return { x: r.left + r.width / 2, y: r.top + 240 }; });
    try {
      await page.goto(url("#/instances")); await settle(300);
      await page.evaluate(() => localStorage.removeItem("amh_ai_w"));
      await openPanel();
      const h = await page.$eval(".ap-resize", e => ({ tab: e.tabIndex, role: e.getAttribute("role"), ori: e.getAttribute("aria-orientation"), label: e.getAttribute("aria-label") }));
      assert(h.tab === 0 && h.role === "separator" && h.ori === "vertical" && !!h.label, "手柄应能被 Tab 到、有名字：" + J(h));
      // 用键盘走到手柄上（它排在面板里第一个按钮的前面）：脚本直接设的焦点不算键盘焦点，不出焦点高亮
      await page.focus("#ap-back"); await page.keyboard.press("Shift+Tab");
      let w = await W();
      assert(w.focus && w.panel === 440 && w.now === "440" && w.min === "320" && w.max === "720" && w.saved === null, "一开始：" + J(w));
      const ring = await page.$eval(".ap-resize", e => { const c = getComputedStyle(e); return { bg: c.backgroundColor, outline: c.outlineStyle }; });
      assert(!/rgba\(0, 0, 0, 0\)|transparent/.test(ring.bg), "键盘焦点落在手柄上时应有高亮：" + J(ring));
      await page.keyboard.down("ArrowLeft");
      w = await W();
      assert(w.v === "456px" && w.panel === 456 && w.now === "456" && w.saved === null, "按下 ←：宽 16，松键之前不写：" + J(w));
      await page.keyboard.up("ArrowLeft");
      assert((await W()).saved === "456", "松键后记下：" + J(await W()));
      await page.keyboard.press("Shift+ArrowLeft");
      w = await W();
      assert(w.panel === 520 && w.saved === "520" && w.now === "520", "Shift + ←：宽 64：" + J(w));
      await page.keyboard.press("ArrowRight");
      w = await W();
      assert(w.panel === 504 && w.saved === "504", "→：窄 16：" + J(w));
      for (let i = 0; i < 4; i++) await page.keyboard.press("Shift+ArrowRight"); // 504 → 440 → 376 → 320（到头）→ 320
      w = await W();
      assert(w.v === "320px" && w.panel === 320 && w.now === "320" && w.saved === "320", "最窄 320，到头再按不该跳回默认宽度：" + J(w));
      for (let i = 0; i < 8; i++) await page.keyboard.press("Shift+ArrowLeft"); // 到 720 为止
      w = await W();
      assert(w.v === "720px" && w.panel === 720 && w.now === "720" && w.saved === "720", "最宽 720：" + J(w));
      await page.keyboard.press("ArrowRight");
      await openPanel(); // 刷新
      w = await W();
      assert(w.v === "704px" && w.panel === 704 && w.now === "704" && w.saved === "704", "刷新后宽度还在：" + J(w));
      // 鼠标拖：往右拖变窄，松手后记下、高亮去掉
      let g = await grip();
      await page.mouse.move(g.x, g.y); await page.mouse.down();
      await page.mouse.move(g.x + 104, g.y, { steps: 4 });
      w = await W();
      assert(w.drag && w.panel === 600 && w.now === "600" && w.saved === "704", "拖动中：" + J(w));
      await page.mouse.up();
      w = await W();
      assert(!w.drag && w.panel === 600 && w.saved === "600", "松手后：" + J(w));
      // 拖到一半被打断（没有松手这一步，只是失去了指针捕获）：照样收尾，之后鼠标再动也不跟着变
      await page.evaluate(() => document.querySelector(".ap-resize").addEventListener("pointerdown", e => { window.__pid = e.pointerId; }, { once: true }));
      g = await grip();
      await page.mouse.move(g.x, g.y); await page.mouse.down();
      await page.mouse.move(g.x - 40, g.y, { steps: 2 });
      assert((await W()).panel === 640, "第二次拖动中：" + J(await W()));
      await page.evaluate(() => document.querySelector(".ap-resize").releasePointerCapture(window.__pid));
      await page.mouse.move(g.x - 41, g.y); await page.mouse.move(g.x - 42, g.y);
      w = await W();
      assert(!w.drag && w.panel === 640 && w.saved === "640", "被打断后应去掉高亮、记下当时的宽度：" + J(w));
      await page.mouse.move(g.x - 42, g.y + 30, { steps: 2 });
      await page.mouse.up();
      assert((await W()).panel === 640, "打断之后不该还跟着鼠标变：" + J(await W()));
      // 再拖一次：只有这一次的监听在起作用
      g = await grip();
      await page.mouse.move(g.x, g.y); await page.mouse.down();
      await page.mouse.move(g.x + 20, g.y, { steps: 2 });
      await page.mouse.up();
      w = await W();
      assert(!w.drag && w.panel === 620 && w.saved === "620" && w.now === "620", "打断之后再拖一次：" + J(w));
    } finally { await page.mouse.up(); await page.keyboard.press("Escape"); await page.evaluate(() => localStorage.removeItem("amh_ai_w")); await asstState({ reset: true }); await aiState({ reset: true }); }
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
      await page.click("#gc-on"); await settle(300);
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
      await page.click('[data-allow="Notch_CN"]'); await settle(400);
      assert(JSON.stringify(gcPuts(t0).pop()) === JSON.stringify({ allowed: ["Notch_CN"] }), "授权应即时发 PUT：" + JSON.stringify(gcPuts(t0)));
      const warn = await page.textContent("#gc-wl-warn");
      assert(warn.includes("没有开启白名单") && warn.includes("Notch_CN 的名字进服"), "没开白名单应提醒：" + warn);
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
      await page.click('[data-allow="Notch_CN"]'); await settle(300);
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
      await page.click('[data-tpl="gamemode"]'); await settle(300);
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

  await run("游戏内 AI：限流的默认值（冷却 8 秒、每人每天 60 次）；全服每天的 token 写明约合多少钱；列出今天谁问了几次；「/tp 怎么用」当提问", async () => {
    try {
      await gcState({ gc: { reset: true }, ai: { reset: true, ...AI_DEEPSEEK, key: "sk-x", priceUpdated: "2026-10-28" } });
      await openGC();
      const vals = [await page.inputValue("#gc-l-cooldown"), await page.inputValue("#gc-l-perPlayer")];
      assert(JSON.stringify(vals) === JSON.stringify(["8", "60"]), "出厂默认值：" + vals);
      const lim = (await page.textContent("#gc-lim")).replace(/\s+/g, " ");
      assert(lim.includes("默认 8 秒") && lim.includes("默认 60 次") && lim.includes("叫伙伴另算"), "限流的说明：" + lim);
      assert((await page.textContent("#gc")).includes("接下来 3 分钟不再回答"), "说明里的静默时长");
      let cost = await page.textContent("#gc-budget-cost");
      assert(cost.includes("按当前模型的单价估，每天 50 万 token 约 $0.") && cost.includes("国际站"), "全服每天的 token 约合多少钱：" + cost);
      assert((await page.textContent("#gc-top")) === "Steve 4 次（叫伙伴 2 次）、Alex 3 次。", "今天谁问了几次：" + await page.textContent("#gc-top"));
      await page.fill("#gc-l-budget", "100"); await page.press("#gc-l-budget", "Enter"); await settle(300);
      cost = await page.textContent("#gc-budget-cost");
      assert(cost.includes("每天 100 万 token 约 $0."), "改了上限，估价跟着变：" + cost);
      // 识别测试：问命令用法的写法当提问，别的不认识的选项照旧
      await page.fill("#gc-line", "[20:15:31] [Server thread/INFO]: <Alex> @AI /tp 怎么用"); await page.press("#gc-line", "Enter"); await settle(400);
      assert((await page.textContent(".gc-result")).includes("Alex 提问「/tp 怎么用」"), "「/tp 怎么用」当提问：" + await page.textContent(".gc-result"));
      await page.fill("#gc-line", "[20:15:31] [Server thread/INFO]: <Alex> @AI /tp Steve"); await page.press("#gc-line", "Enter"); await settle(400);
      assert((await page.textContent(".gc-result")).includes("未知选项 /tp"), "别的照旧是未知选项：" + await page.textContent(".gc-result"));
      // 查不到单价时不写；今天没人问过时写明
      await gcState({ ai: { priceUpdated: "" }, gc: { inst: { "机械动力整合包": { today: { count: 0, tokens: 0, players: {}, companion: {}, top: [] } } } } });
      await openGC("机械动力整合包");
      assert((await page.textContent("#gc-budget-cost")) === "", "查不到单价时不写：" + await page.textContent("#gc-budget-cost"));
      assert((await page.textContent("#gc-top")) === "今天还没有人问过。", "没人问过：" + await page.textContent("#gc-top"));
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
      await p.click('[data-allow="Notch_CN"]'); await p.waitForTimeout(400);
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
      // 好友的客户端：0.2.0 起两个依赖只装服务端，好友只装伙伴模组（随 MineDeck 内置，打开 mods 文件夹发给好友）
      const cl = await page.$$eval("#cp-client [data-client]", rs => rs.map(r => r.dataset.client + ":" + r.querySelector(".tag").textContent));
      assert(JSON.stringify(cl) === JSON.stringify(["minedeck_companion:要装", "playerengine:不用装", "architectury:不用装"]), "side 是 server 的依赖不用装：" + cl);
      assert(await page.$("#cp-mods-dir"), "应有「打开 mods 文件夹」");
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
      await page.click("#cp-on"); await settle(300);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ enabled: true }]) && await toastHas("已打开伙伴"), "开关应即时发 PUT：" + JSON.stringify(cpPuts(t0)));
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
      await page.click('.modal .switch input');
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
      await page.click("#ai-mcp-slot .switch input"); await settle(400);
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
      // 名字里已经带了连接方式的不再标一次（测试报告第五轮：「其他（HTTP）（HTTP）」）
      assert(items.includes("其他（HTTP）") && items.includes("其他（stdio）") && !items.some(x => /（(HTTP|stdio)）（(HTTP|stdio)）/.test(x)), "不重复标连接方式：" + items);
      await page.click('.dropmenu button:has-text("Cherry Studio")'); await page.waitForSelector(".modal .mcp-pre");
      const pv = await page.evaluate(() => ({ pre: document.querySelector(".modal .mcp-pre").textContent, body: document.querySelector(".modal .m-body").textContent }));
      assert(pv.pre.includes('"streamableHttp"') && pv.pre.includes("<令牌，复制时自动带上>") && !pv.pre.includes(tok) && pv.body.includes("从 JSON 导入"), "预览：" + JSON.stringify(pv).slice(0, 300));
      // 弹窗里的复制按钮和页面上复制接口地址的「复制」名字分得开（读屏、自动化靠名字找按钮）
      const names = await page.evaluate(() => { const ok = document.querySelector('.modal [data-x="ok"]'), u = document.querySelector("#mcp-url");
        return { ok: ok.textContent.trim(), okAria: ok.getAttribute("aria-label") || "", url: u.getAttribute("aria-label") || "" }; });
      assert(names.ok === "复制这段配置" && names.okAria.startsWith("复制这段配置") && names.okAria.includes("Cherry Studio") && names.url === "复制接口地址", "两个复制按钮的名字：" + JSON.stringify(names));
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
      await page.click("#ai-mcp-slot .switch input"); await settle(400);
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
  await run("MCP 接口：「连过的 AI 工具」不用刷新页面，有新的 AI 工具来过就自己更新，上面的开关不重画（测试报告第五轮）", async () => {
    await mcpState({ reset: true, enabled: true }); await aiState({ reset: true });
    try {
      await page.goto(url("#/settings/ai")); await settle(700);
      assert((await page.textContent("#mcp-clients")).includes("还没有 AI 工具连过"), "一开始没有连过的");
      await page.$eval("#mcp-on", e => { e.dataset.mark = "1"; }); // 认出开关是不是原来那个
      const now = new Date().toISOString();
      await mcpState({ clients: [{ name: "cherry-studio", title: "Cherry Studio", version: "1.5.0", transport: "http", protocol: "2025-06-18", first: now, last: now, calls: 2 }] });
      await page.waitForFunction(() => (document.querySelector("#mcp-clients") || {}).textContent?.includes("Cherry Studio"), null, { timeout: 5000 }).catch(() => {});
      const v = await page.evaluate(() => ({ text: (document.querySelector("#mcp-clients") || {}).textContent || "", same: (document.querySelector("#mcp-on") || { dataset: {} }).dataset.mark === "1" }));
      assert(v.text.includes("Cherry Studio") && v.text.includes("调用了 2 次工具") && !v.text.includes("还没有 AI 工具连过") && v.same, "列表自己更新、开关没重画：" + JSON.stringify(v));
    } finally { await mcpState({ reset: true }); await aiState({ reset: true }); }
  });
  // ==================== MCP 接口结束 ====================

  // ==================== 手机布局（测试报告第 15 条）====================
  // 模拟真手机：isMobile + hasTouch（pointer: coarse）。Playwright 模拟不了地址栏伸缩，这里只验证布局本身：
  // 底栏在纵向 flex 里排在内容层下面（不是 fixed 盖在内容上），内容层滚到底时最后一行露在底栏上方
  await run("手机布局：竖屏 390×844、横屏 844×390（触屏）下底部标签栏在视口内且点得到，#main 能滚到最底、整页不滚动、不横向溢出；鼠标的矮窗口仍是桌面布局", async () => {
    const pages = ["#/instances", `#/inst/${inst}/common`, `#/inst/${inst}/console`, "#/settings/remote", "#/create"];
    for (const vp of [{ width: 390, height: 844 }, { width: 844, height: 390 }]) {
      const tag = vp.width + "×" + vp.height;
      const c = await hostCtx({ host: false }, { viewport: vp, isMobile: true, hasTouch: true });
      try {
        const p = await c.newPage();
        p.on("pageerror", e => errors.push(pageErr(e)));
        await p.goto(url("#/instances")); await p.waitForTimeout(600);
        // 先确认真的模拟出了触屏，否则横屏那半条会走桌面布局、白白通过
        assert(await p.evaluate(() => matchMedia("(pointer: coarse)").matches), tag + "：没模拟出触屏（pointer: coarse）");
        for (const hash of pages) {
          await p.goto(url(hash)); await p.waitForTimeout(700);
          const r = await p.evaluate(() => {
            const sb = document.querySelector("#sidebar"), m = document.querySelector("#main");
            const s = sb.getBoundingClientRect();
            const navs = [...sb.querySelectorAll(".nav-item")].map(a => {
              const b = a.getBoundingClientRect(), hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
              return !!hit && hit.closest(".nav-item") === a;
            });
            m.scrollTop = m.scrollHeight;
            const mr = m.getBoundingClientRect();
            const last = Math.max(...[...m.children].filter(e => e.getClientRects().length).map(e => e.getBoundingClientRect().bottom));
            return { vh: innerHeight, dir: getComputedStyle(document.querySelector("#shell")).flexDirection,
              sb: [s.top, s.bottom, s.left, s.right], mainBottom: mr.bottom, last, rest: m.scrollHeight - m.scrollTop - m.clientHeight, navs,
              docH: document.documentElement.scrollHeight, docW: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
              mainW: [m.scrollWidth, m.clientWidth] };
          });
          const where = `${tag} ${hash}：${JSON.stringify(r)}`;
          assert(r.dir === "column", "没走手机布局（底栏应在内容层下面）" + where);
          assert(r.sb[0] >= 0 && Math.abs(r.sb[1] - r.vh) < 1 && r.sb[2] === 0 && Math.abs(r.sb[3] - vp.width) < 1, "底栏不在视口最下方" + where);
          assert(r.navs.length === 4 && r.navs.every(Boolean), "底栏有标签被盖住、点不到" + where);
          assert(r.mainBottom <= r.sb[0] + .5, "内容层伸到了底栏下面" + where);
          assert(r.rest <= 1 && r.last <= r.mainBottom + .5, "内容层滚不到最底，最后一行被挡" + where);
          assert(r.docH <= r.vh && r.docW <= r.cw, "整页能滚动或横向溢出" + where);
          assert(r.mainW[0] <= r.mainW[1], "内容层横向溢出" + where);
        }
        // 点底栏切页（手指点按）
        await p.tap('#sidebar a[data-nav="tunnels"]'); await p.waitForTimeout(500);
        assert(p.url().endsWith("#/tunnels"), tag + "：点底栏「内网穿透」没切页：" + p.url());
      } finally { await c.close(); }
    }
    // 鼠标操作的矮窗口（不是触屏）：仍是桌面的左侧图标栏，不被「矮」这一条带进手机布局
    const d = await hostCtx({ host: true }, { viewport: { width: 844, height: 390 } });
    try {
      const p = await d.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(url("#/instances")); await p.waitForTimeout(600);
      const r = await p.evaluate(() => ({ dir: getComputedStyle(document.querySelector("#shell")).flexDirection, w: document.querySelector("#sidebar").getBoundingClientRect().width }));
      assert(r.dir === "row" && r.w === 64, "鼠标的矮窗口不该变成底栏：" + JSON.stringify(r));
    } finally { await d.close(); }
    // 手机上开着「减少动态」：切页仍然不播任何动画（手机块里的动效覆盖不带 !important，压不过减少动态）
    const rm = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, reducedMotion: "reduce" });
    try {
      const p = await rm.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(url("#/instances")); await p.waitForTimeout(600);
      await p.tap('#sidebar a[data-nav="settings"]');
      const running = await p.evaluate(() => document.getAnimations().filter(a => a.playState === "running").length);
      assert(running === 0, "减少动态下手机切页仍有动画：" + running);
    } finally { await rm.close(); }
  });

  // ==================== 内网穿透的隧道卡片（测试报告第五轮）====================
  // 仿真后端：/__mock/state 的 tunnel 改某条隧道（startError：点「启动」时报的错；nextAddr / addrDelay：启动后多久拿到公网地址），tunnelsReset 还原
  const tunState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data.tunnels), b);
  const tunRowText = id => page.$eval(`#tun-list [data-tun="${id}"]`, e => e.textContent.replace(/\s+/g, " ").trim());

  await run("隧道：点「启动」后卡片马上变「运行中」，拿到公网地址前每秒刷新；展开着日志也照常刷新、日志框不断；停止后马上变回", async () => {
    await tunState({ tunnelsReset: true, tunnel: { id: "t2", lastError: "", nextAddr: "frp.example.com:34567", addrDelay: 1500 } });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      assert((await page.textContent("#tun-summary")).includes("运行中 1"), "一开始 1 条在运行");
      await page.click('[data-tlog="t2"]'); await settle(400);
      assert(await page.$eval("#tlog-t2", e => e.style.display !== "none" && e.textContent.includes("start proxy success")), "日志展开了");
      await page.$eval("#tlog-t2", e => { e.dataset.mark = "1"; }); // 认出是不是同一个日志框
      await page.click('[data-tstart="t2"]');
      await page.waitForFunction(() => document.querySelector("#tun-summary").textContent.includes("运行中 2"), null, { timeout: 1500 }).catch(() => {});
      const r1 = await tunRowText("t2");
      assert((await page.textContent("#tun-summary")).includes("运行中 2") && r1.includes("连接中") && r1.includes("停止"), "启动后马上是运行中、连接中（日志开着也刷新）：" + r1);
      // 公网地址 1.5 秒后才有：每秒刷新一次，3 秒内一定显示出来（4 秒一次的轮询不一定赶得上）
      await page.waitForFunction(() => (document.querySelector('[data-tun="t2"]') || {}).textContent?.includes("frp.example.com:34567"), null, { timeout: 3000 }).catch(() => {});
      assert((await tunRowText("t2")).includes("frp.example.com:34567"), "拿到公网地址后很快显示：" + await tunRowText("t2"));
      const log = await page.$eval("#tlog-t2", e => ({ same: e.dataset.mark === "1", shown: e.style.display !== "none", text: e.textContent.includes("start proxy success") }));
      assert(log.same && log.shown && log.text, "列表重画时日志框还是原来那个：" + JSON.stringify(log));
      assert(await page.$eval('[data-tlog="t2"]', b => b.getAttribute("aria-expanded")) === "true", "日志按钮标着展开");
      await page.click('[data-tstop="t2"]');
      await page.waitForFunction(() => document.querySelector("#tun-summary").textContent.includes("运行中 1") && !!document.querySelector('[data-tstart="t2"]'), null, { timeout: 1500 }).catch(() => {});
      assert((await page.textContent("#tun-summary")).includes("运行中 1") && await page.$('[data-tstart="t2"]'), "停止后马上变回：" + await tunRowText("t2"));
      await page.click('[data-tlog="t2"]'); await settle(200);
      assert(await page.$eval("#tlog-t2", e => e.style.display === "none") && await page.$eval('[data-tlog="t2"]', b => b.getAttribute("aria-expanded")) === "false", "再点一下日志收起");
    } finally { await tunState({ tunnelsReset: true }); }
  });

  await run("隧道：启动失败、跟随启动失败的原因直接写在卡片上（停着时排在上次的公网地址前面），跑起来后不再显示", async () => {
    const MSG = "首次使用需手动放置樱花 frpc：登录 SakuraFrp 管理面板 → 软件下载 → 下载「frpc」Windows amd64 版，把 exe 放入 D:\\MineDeck\\runtimes\\frpc\\natfrp 目录后重试（仅需一次）";
    await tunState({ tunnelsReset: true, tunnel: { id: "t1", provider: "natfrp", running: false, lastError: "跟随启动失败：" + MSG } });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      const r = await tunRowText("t1");
      assert(r.includes("上次出错") && r.includes("跟随启动失败：首次使用需手动放置樱花 frpc") && r.includes("natfrp 目录后重试") && !r.includes("openfrp.top"), "跟随启动失败：" + r);
      // 手动启动也失败：提示里有原因，卡片上换成「启动失败：…」，按钮还能再点
      await tunState({ tunnel: { id: "t1", startError: MSG } });
      await page.click('[data-tstart="t1"]');
      // 这句又长又带路径：提示里只写结论，原文在「详情」里；卡片上照样整句显示（下面断言 .tun-err）
      assert(await toastHas("无法启动隧道"), "启动失败要提示");
      assert(await toastDetail("无法启动隧道") === MSG, "提示的「详情」里应是完整原文");
      await page.waitForFunction(() => (document.querySelector('[data-tun="t1"] .tun-err') || {}).textContent?.startsWith("启动失败："), null, { timeout: 2000 }).catch(() => {});
      assert((await page.textContent('[data-tun="t1"] .tun-err')).startsWith("启动失败：首次使用"), "手动启动失败的原因：" + await tunRowText("t1"));
      assert(!(await page.$eval('[data-tstart="t1"]', b => b.disabled)), "失败后启动按钮能再点");
      // 放好 frpc 再启动：跑起来就不再显示失败原因
      await tunState({ tunnel: { id: "t1", startError: "", nextAddr: "", publicAddr: "" } });
      await page.click('[data-tstart="t1"]');
      await page.waitForFunction(() => !document.querySelector('[data-tun="t1"] .tun-err') && document.querySelector('[data-tun="t1"]').textContent.includes("连接中"), null, { timeout: 2000 }).catch(() => {});
      const r2 = await tunRowText("t1");
      assert(!r2.includes("首次使用") && r2.includes("连接中"), "跑起来后不再显示失败原因：" + r2);
      // 自建备用线（初始数据）：停着、frpc 报过错，错误整句显示在卡片上
      assert((await tunRowText("t2")).includes("authorization failed"), "停着的隧道显示上次的错误：" + await tunRowText("t2"));
    } finally { await tunState({ tunnelsReset: true }); }
  });

  await run("隧道：新一次运行还没连上就报错，卡片显示这次的错误，不拿上次的公网地址充数；没报错时是「连接中」，连上以后照常显示地址（Codex 复审）", async () => {
    await tunState({ tunnelsReset: true, tunnel: { id: "t1", running: true, online: false, lastError: "login to the server failed: dial tcp: i/o timeout" } });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      const r = await tunRowText("t1");
      assert(r.includes("连接出错") && r.includes("i/o timeout") && !r.includes("openfrp.top"), "没连上就报错：" + r);
      assert(!(await page.$('[data-tun="t1"] [data-copy]')), "没连上时不给复制上次的地址");
      await tunState({ tunnel: { id: "t1", lastError: "" } });
      await page.goto(url("#/tunnels")); await settle(600);
      const r2 = await tunRowText("t1");
      assert(r2.includes("连接中") && !r2.includes("openfrp.top"), "没连上、也没报错：" + r2);
      await tunState({ tunnel: { id: "t1", online: true } });
      await page.goto(url("#/tunnels")); await settle(600);
      const r3 = await tunRowText("t1");
      assert(r3.includes("cn-sz-bgp-1.openfrp.top:23456") && !r3.includes("连接中"), "连上以后：" + r3);
    } finally { await tunState({ tunnelsReset: true }); }
  });

  await run("隧道：列表偶尔刷新失败时，展开着的日志不关、列表留着，摘要提示一句；恢复后照常重画、日志框还是原来那个（独立评审）", async () => {
    const fail = route => route.fulfill({ status: 502, contentType: "text/html", body: "<h1>Bad Gateway</h1>" });
    await tunState({ tunnelsReset: true });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      await page.click('[data-tlog="t1"]'); await settle(400);
      await page.$eval("#tlog-t1", e => { e.dataset.mark = "1"; });
      await page.route("**/api/tunnels", fail);
      await page.waitForFunction(() => document.querySelector("#tun-summary").textContent.includes("无法刷新隧道列表"), null, { timeout: 6000 }).catch(() => {});
      // 回的不是 JSON（Bad Gateway），原文是「服务异常 (HTTP 502)」：只写结论，原文收进行内的复制键
      assert((await page.textContent("#tun-summary")).trim() === "无法刷新隧道列表" && (await page.getAttribute("#tun-summary [data-err-raw]", "data-err-raw")) === "服务异常 (HTTP 502)",
        "摘要应提示刷新失败：" + await page.textContent("#tun-summary"));
      const during = await page.$eval("#tlog-t1", e => ({ same: e.dataset.mark === "1", shown: e.style.display !== "none" }));
      assert(during.same && during.shown && (await page.$$("#tun-list [data-tun]")).length === 2, "刷新失败时列表和日志应留着：" + JSON.stringify(during));
      await page.unroute("**/api/tunnels", fail);
      await page.waitForFunction(() => document.querySelector("#tun-summary").textContent.includes("条隧道"), null, { timeout: 6000 }).catch(() => {});
      assert((await page.textContent("#tun-summary")).includes("条隧道"), "恢复后摘要照常：" + await page.textContent("#tun-summary"));
      const after = await page.$eval("#tlog-t1", e => ({ same: e.dataset.mark === "1", shown: e.style.display !== "none" }));
      assert(after.same && after.shown, "恢复后日志框还是原来那个：" + JSON.stringify(after));
    } finally { await page.unroute("**/api/tunnels", fail).catch(() => {}); await tunState({ tunnelsReset: true }); }
  });

  await run("隧道：存着凭据的自定义 frps 有「清除凭据」，确认后带 clearCredential 整体提交、其余配置原样带回；服务商的隧道没有这个按钮", async () => {
    const lastPut = () => page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data.lastTunnelPut));
    await tunState({ tunnelsReset: true });
    try {
      await page.goto(url("#/tunnels")); await settle(600);
      assert(await page.$('[data-tclear="t2"]') && !(await page.$('[data-tclear="t1"]')), "只有存着凭据的自定义 frps 有「清除凭据」");
      assert(await page.getAttribute('[data-tclear="t2"]', "aria-label") === "清除 自建备用线 保存的凭据", "按钮的名字带上隧道名");
      // 先点取消：什么也不发
      await page.click('[data-tclear="t2"]'); await settle(300);
      assert((await page.textContent(".modal")).includes("清除「自建备用线」保存的凭据？"), "要先确认");
      await page.click('.modal [data-x="cancel"]'); await settle(300);
      assert(await lastPut() === null, "取消了不该发请求");
      await page.click('[data-tclear="t2"]'); await settle(300);
      await page.click('.modal [data-x="ok"]'); await settle(300);
      assert(await toastHas("已清除「自建备用线」保存的凭据"), "清除后要提示");
      const b = await lastPut();
      assert(b && b.clearCredential === true && b.credential === "" && b.provider === "custom" && b.name === "自建备用线" &&
        b.serverAddr === "frp.example.com" && b.serverPort === 7000 && b.remotePort === 25566 && b.localPort === 25565 &&
        b.proto === "tcp" && b.purpose === "game" && b.autoStart === false && b.boundInstance === "", "请求体：" + JSON.stringify(b));
      await page.waitForFunction(() => !document.querySelector('[data-tclear="t2"]'), null, { timeout: 2000 }).catch(() => {});
      assert(!(await page.$('[data-tclear="t2"]')), "清掉以后不再显示这个按钮");
      // 运行中不能改：后端的原因接在结论后面
      await tunState({ tunnelsReset: true, tunnel: { id: "t2", running: true } });
      await page.goto(url("#/tunnels")); await settle(600);
      await page.click('[data-tclear="t2"]'); await settle(300);
      await page.click('.modal [data-x="ok"]'); await settle(300);
      assert(await toastHas("无法清除凭据：请先停止隧道再修改"), "运行中清除要说明原因");
    } finally { await tunState({ tunnelsReset: true }); }
  });

  await run("隧道：樱花、OpenFrp 的隧道不带 frps 地址和端口（以前把隐藏着的 25565 / 25566 / 7000 也存了进去）", async () => {
    try {
      await musicState({ music: { reset: true } });
      await page.goto(url("#/tunnels")); await settle();
      await page.click("#tn-toggle");
      await page.selectOption("#tn-provider", "natfrp");
      await page.fill("#tn-name", "樱花游戏"); await page.fill("#tn-cred", "k"); await page.fill("#tn-proxy", "123456");
      await page.click("#tn-add"); await settle(400);
      const body = (await musicState({})).music.tunnelBody;
      assert(body && body.provider === "natfrp" && body.proxyId === "123456" && body.localPort === 0 && body.remotePort === 0 && body.serverPort === 0 && body.serverAddr === "",
        "请求体：" + JSON.stringify(body));
    } finally { await resetMusic(); }
  });
  // ==================== 内网穿透的隧道卡片结束 ====================

  await run("伙伴：「好友的客户端」写明整合包服务端还要装同一个整合包，复制给好友的说明里也有（测试报告第五轮）", async () => {
    try {
      await resetCP();
      await openCP();
      const sub = await page.$eval("#cp-client", e => e.previousElementSibling.textContent);
      assert(sub.includes("整合包") && sub.includes("版本和服务器的一样"), "好友的客户端那段说明：" + sub);
      await stubClipboard(page);
      await page.click("#cp-copy-guide"); await settle(250);
      const g = await page.evaluate(() => (window.__copied || []).pop() || "");
      assert(g.includes("如果服务器是整合包") && g.includes("先装同一个整合包") && g.includes("@伙伴的名字"), "复制给好友的说明：" + g);
    } finally { await resetCP(); }
  });

  // ==================== 窗口与设置（测试报告 A-02、观察 3、观察 16）====================
  // 模拟宿主时把窗口函数换成记账的，看前端调了哪个
  const winCtx = async (extra = {}) => {
    const c = await browser.newContext({ viewport: { width: 1280, height: 850 }, locale: "zh-CN" });
    await c.addInitScript(x => {
      localStorage.setItem("amh_token", "mock");
      localStorage.setItem("amh_theme", "dark");
      window.__win = [];
      for (const fn of ["hostWinClose", "hostWinMin", "hostWinMaxToggle", "hostWinDrag", "hostCloseAck", "hostWinHide", "hostWinQuit"]) window[fn] = () => { window.__win.push(fn); };
      window.hostWinIsMax = async () => false;
      if (x.dbl) window.hostDblClickTime = async () => x.dbl;
    }, extra);
    return c;
  };
  const winCalls = p => p.evaluate(() => window.__win.filter(f => f !== "hostWinIsMax").join(","));

  await run("标题栏：空白处按下发起原生拖动；同一处快速按两下第二下改为最大化 / 还原、不再拖动；间隔超过系统双击时间或位置挪开都不算；窗口按钮上不触发", async () => {
    const c = await winCtx({ dbl: 300 }); // 宿主告知系统双击间隔 300ms
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(500);
      const r = await p.$eval("#titlebar .grow", el => { const b = el.getBoundingClientRect(); return { x: b.left + b.width / 2, y: b.top + b.height / 2 }; });
      const reset = () => p.evaluate(() => { window.__win.length = 0; });
      await p.mouse.click(r.x, r.y);
      assert((await winCalls(p)) === "hostWinDrag", "单击应只发起拖动：" + await winCalls(p));
      await p.waitForTimeout(400); await reset();
      await p.mouse.dblclick(r.x, r.y);
      assert((await winCalls(p)) === "hostWinDrag,hostWinMaxToggle", "双击应第一下拖动、第二下最大化：" + await winCalls(p));
      await p.waitForTimeout(400); await reset();
      await p.mouse.click(r.x, r.y); await p.waitForTimeout(400); await p.mouse.click(r.x, r.y); // 间隔超过宿主告知的 300ms
      assert((await winCalls(p)) === "hostWinDrag,hostWinDrag", "间隔超过双击时间不算双击：" + await winCalls(p));
      await p.waitForTimeout(400); await reset();
      await p.mouse.click(r.x, r.y); await p.mouse.click(r.x + 30, r.y); // 拖动过窗口：屏幕位置变了
      assert((await winCalls(p)) === "hostWinDrag,hostWinDrag", "位置挪开不算双击：" + await winCalls(p));
      await p.waitForTimeout(400); await reset();
      await p.mouse.click(r.x, r.y); await p.mouse.click(r.x, r.y); await p.mouse.click(r.x, r.y); // 三连击：第三下重新算起
      assert((await winCalls(p)) === "hostWinDrag,hostWinMaxToggle,hostWinDrag", "三连击：" + await winCalls(p));
      await p.waitForTimeout(400); await reset();
      await p.dblclick("#wc-min");
      assert(!(await winCalls(p)).includes("hostWinDrag") && !(await winCalls(p)).includes("hostWinMaxToggle"), "窗口按钮上不该拖动或最大化：" + await winCalls(p));
    } finally { await c.close(); }
  });

  await run("关窗确认：宿主请求时弹出三选一（默认聚焦「最小化到托盘」），先回应宿主；不叠第二个；取消 / 停止服务器并退出 / 最小化到托盘（勾「以后都这样」才改设置）；托盘不可用时只剩退出和取消", async () => {
    const c = await winCtx();
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      const puts = []; // 改了「最小化到托盘」的 PUT（原生窗口还会顺带上报界面底色，不算）
      p.on("request", rq => {
        if (rq.method() !== "PUT" || new URL(rq.url()).pathname !== "/api/config") return;
        const b = JSON.parse(rq.postData() || "{}");
        if ("minimizeToTray" in b) puts.push(b);
      });
      await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(500);
      const ask = o => p.evaluate(o => { window.__win.length = 0; window.__mdCloseAsk(o); }, o);
      const btns = () => p.$$eval("#close-ask:not(.leave) .m-actions .btn", bs => bs.map(b => b.textContent.trim()).join("|"));
      await ask({ tray: true, running: 2 });
      await p.waitForSelector("#close-ask");
      assert((await p.textContent("#close-ask h3")).includes("2 台服务器"), "标题没写几台服务器：" + await p.textContent("#close-ask h3"));
      assert((await btns()) === "取消|停止服务器并退出|最小化到托盘", "按钮不对：" + await btns());
      assert((await p.evaluate(() => document.activeElement.textContent.trim())) === "最小化到托盘", "默认焦点不对");
      assert((await winCalls(p)) === "hostCloseAck", "没先回应宿主：" + await winCalls(p));
      await p.evaluate(() => window.__mdCloseAsk({ tray: true, running: 2 })); // Alt+F4 再按一次
      assert((await p.$$("#close-ask")).length === 1, "叠出了第二个确认框");
      await p.keyboard.press("3"); await p.waitForTimeout(100);
      assert(p.url().endsWith("#/instances"), "确认框开着时按 3 切了页：" + p.url());
      await p.keyboard.press("Escape"); await p.waitForTimeout(250);
      assert((await p.$("#close-ask")) === null && !/Hide|Quit/.test(await winCalls(p)), "Esc 应只关掉确认框：" + await winCalls(p));

      await ask({ tray: true, running: 1 });
      await p.click('#close-ask [data-x="quit"]'); await p.waitForTimeout(250);
      assert((await winCalls(p)) === "hostCloseAck,hostWinQuit" && puts.length === 0, "停止服务器并退出：" + await winCalls(p) + " " + JSON.stringify(puts));

      await ask({ tray: true, running: 1 });
      await p.click('#close-ask [data-x="tray"]'); await p.waitForTimeout(250);
      assert((await winCalls(p)) === "hostCloseAck,hostWinHide" && puts.length === 0, "最小化到托盘（不记住）不该改设置：" + await winCalls(p) + " " + JSON.stringify(puts));

      await ask({ tray: true, running: 1 });
      await p.click('#close-ask .switch input'); // 勾上「以后关闭窗口都最小化到托盘」
      await p.click('#close-ask [data-x="tray"]'); await p.waitForTimeout(300);
      assert((await winCalls(p)) === "hostCloseAck,hostWinHide", "记住后没收进托盘：" + await winCalls(p));
      assert(puts.length === 1 && puts[0].minimizeToTray === true && Object.keys(puts[0]).length === 1, "记住选择应只保存 minimizeToTray：" + JSON.stringify(puts));

      await ask({ tray: false, running: 3 });
      await p.waitForSelector("#close-ask:not(.leave)");
      assert((await btns()) === "取消|停止服务器并退出" && (await p.$("#close-ask:not(.leave) .switch")) === null, "托盘不可用时：" + await btns());
      assert((await p.evaluate(() => document.activeElement.textContent.trim())) === "取消", "托盘不可用时默认焦点应在取消");
      await p.evaluate(() => { document.activeElement.blur(); window.__mdCloseAsk({ tray: false, running: 3 }); }); // 再按一次关闭
      assert((await p.evaluate(() => document.activeElement.textContent.trim())) === "取消", "再次关闭时焦点不该落到「停止服务器并退出」");
      await p.evaluate(() => { window.__win.length = 0; window.__win.push("hostCloseAck"); }); // 下面只看取消这一下
      await p.click('#close-ask:not(.leave) [data-x="cancel"]'); await p.waitForTimeout(250);
      assert((await p.$("#close-ask")) === null && (await winCalls(p)) === "hostCloseAck", "取消：" + await winCalls(p));
    } finally { await c.close(); }
    // 局域网浏览器（没有宿主函数）不接关窗请求
    const lanCtx = await browser.newContext({ locale: "zh-CN" });
    try {
      const p = await lanCtx.newPage();
      await p.addInitScript(() => localStorage.setItem("amh_token", "mock"));
      await p.goto(ORIGIN + "#/instances"); await p.waitForTimeout(300);
      assert((await p.evaluate(() => typeof window.__mdCloseAsk)) === "undefined", "局域网浏览器不该有关窗确认");
    } finally { await lanCtx.close(); }
  });

  await run("启动与托盘：说明写清不开托盘时点关闭会退出、有服务器在运行先询问（不再说「始终显示」）", async () => {
    await page.goto(url("#/settings/startup")); await settle();
    const head = await page.textContent("#set-body .set-sec-head .sub");
    const sub = await page.$eval('.row-item:has([data-cfg="minimizeToTray"]) .ri-sub', el => el.textContent);
    assert(!head.includes("始终显示") && head.includes("关闭按钮的行为"), "分区说明：" + head);
    assert(sub.includes("会退出程序并停掉服务器") && sub.includes("先确认"), "开关说明：" + sub);
  });

  await run("远程访问：开关即时生效——没设密码时就地提示并拨回、不发请求；有密码直接保存、提示重启后生效（常驻提示，不显示二维码）；关闭即时生效；密码和开关一起保存；后端拒绝时拨回", async () => {
    const mockState = b => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), b);
    const lanPuts = since => reqs.slice(since).filter(r => r.m === "PUT" && r.u === "/api/config").map(r => JSON.parse(r.b || "{}")).filter(b => "listenLan" in b || "lanPassword" in b);
    const sw = () => page.$eval("#lan-on", el => el.checked);
    const lastToast = () => page.$$eval(".toast", ts => ts.length ? ts[ts.length - 1].textContent : "");
    const flip = async () => { await page.click("#lan-on"); await settle(300); };
    try {
      await mockState({ listenLan: false, lanSet: false });
      await page.goto(url("#/settings/remote")); await settle();
      let t0 = reqs.length;
      await flip();
      assert(!(await sw()) && lanPuts(t0).length === 0, "没设密码时开关应拨回、不发请求：" + JSON.stringify(lanPuts(t0)));
      assert((await page.textContent("#lan-pw-err")).includes("先设置访问密码"), "没有就地提示：" + await page.textContent("#lan-pw-err"));
      assert((await page.evaluate(() => document.activeElement.id)) === "lan-pw", "焦点应到密码框");
      // 填好密码点「保存」：和刚才想开的开关一起保存
      await page.fill("#lan-pw", "pw-123456");
      assert((await page.textContent("#lan-pw-err")) === "", "输入后提示应消失");
      t0 = reqs.length;
      await page.click("#lan-save"); await settle(400);
      let ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && ps[0].lanPassword === "pw-123456", "密码和开关应一起保存：" + JSON.stringify(ps));
      assert(await sw(), "保存后开关应为开");
      assert((await page.textContent("#lan-restart")).includes("重启 MineDeck 后生效") && (await page.$("#qr-box")) === null, "还没重启：应常驻提示、不显示二维码");
      assert((await lastToast()).includes("重启 MineDeck 后生效"), "提示没写重启后生效：" + await lastToast());
      assert((await page.getAttribute("#lan-pw", "placeholder")).includes("已设置"), "密码框应改为已设置");
      // 关掉：即时保存；这次启动本来就没监听局域网，不用提重启
      t0 = reqs.length;
      await flip();
      ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === false && !("lanPassword" in ps[0]) && !(await sw()), "关闭应即时保存：" + JSON.stringify(ps));
      assert((await page.$("#lan-restart")) === null && (await lastToast()) === "已关闭局域网访问", "关闭后：" + await lastToast());
      // 已有密码：直接开
      t0 = reqs.length;
      await flip();
      ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && !("lanPassword" in ps[0]) && (await sw()), "有密码时应直接保存：" + JSON.stringify(ps));
      assert((await page.$("#lan-restart")) !== null, "开启后应提示重启");
      // 只改密码
      await page.fill("#lan-pw", "pw-654321");
      t0 = reqs.length;
      await page.press("#lan-pw", "Enter"); await settle(400);
      ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && ps[0].lanPassword === "pw-654321" && (await lastToast()) === "访问密码已保存", "回车保存密码：" + JSON.stringify(ps) + " " + await lastToast());
      // 密码框为空时点保存：就地提示，不发请求
      t0 = reqs.length;
      await page.click("#lan-save"); await settle(200);
      assert(lanPuts(t0).length === 0 && (await page.textContent("#lan-pw-err")).includes("输入新密码"), "空密码保存：" + await page.textContent("#lan-pw-err"));

      // 先填密码再拨开关：一起保存
      await mockState({ listenLan: false, lanSet: false });
      await page.goto(url("#/settings/remote")); await settle();
      await page.fill("#lan-pw", "pw-abcdef");
      t0 = reqs.length;
      await flip();
      ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && ps[0].lanPassword === "pw-abcdef" && (await sw()), "先填密码再拨开关：" + JSON.stringify(ps));

      // 后端拒绝（页面以为有密码，其实没有）：开关拨回并报错
      await mockState({ listenLan: false, lanSet: true });
      await page.goto(url("#/settings/remote")); await settle();
      await mockState({ lanSet: false });
      await flip();
      assert(!(await sw()) && (await lastToast()).includes("无法开启局域网访问：请先设置访问密码"), "后端拒绝时应拨回：" + await lastToast());

      // 这次启动已在局域网上：关闭即时生效（不再放行局域网），再开也不用重启
      await mockState({ listenLan: true });
      await page.goto(url("#/settings/remote")); await settle();
      assert((await page.$("#qr-box svg.qr")) !== null && (await page.$("#lan-restart")) === null, "已生效时应显示二维码、没有重启提示");
      await flip();
      assert((await lastToast()).includes("现在起打不开") && (await page.$("#qr-box")) === null, "关闭已生效的局域网访问：" + await lastToast());
      await flip();
      assert((await lastToast()) === "已开启局域网访问" && (await page.$("#qr-box svg.qr")) !== null, "重新开启（这次启动本就在监听）：" + await lastToast());
    } finally { await mockState({ listenLan: false, lanSet: false }); }
  });
  await run("远程访问：已有密码时在框里填了新密码再拨开关，新密码一起保存、提示里写明（以前只存了开关，重画清空了框，看着像改好了）（独立评审）", async () => {
    const mockState = b => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), b);
    const lanPuts = since => reqs.slice(since).filter(r => r.m === "PUT" && r.u === "/api/config").map(r => JSON.parse(r.b || "{}"));
    await mockState({ listenLan: false, lanSet: true });
    try {
      await page.goto(url("#/settings/remote")); await settle();
      await page.fill("#lan-pw", "pw-new-777");
      const t0 = reqs.length;
      await page.click("#lan-on"); await settle(400);
      const ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && ps[0].lanPassword === "pw-new-777", "开关和新密码应一起保存：" + JSON.stringify(ps));
      assert(await toastHas("新的访问密码已保存"), "提示里应写明新密码也保存了");
    } finally { await mockState({ listenLan: false, lanSet: false }); }
  });
  await run("通知与集成：从局域网打开时 Webhook 地址只显示开头，原样传回不会盖掉真地址，接着改了一半的不收，填新的照常存；本机看完整的（v3.1.0 正式版前检查 B-2）", async () => {
    const mockState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const real = "https://hook.example.com/robot/send?access_token=tk-secret-123";
    const sub = () => page.$eval(".row-item:has(#wh-url) .ri-sub", el => el.textContent);
    try {
      await mockState({ local: true, webhookUrl: real });
      await page.goto(url("#/settings/notify")); await settle();
      assert((await page.inputValue("#wh-url")) === real && !(await sub()).includes("藏起来"), "本机应看到完整地址：" + await page.inputValue("#wh-url"));

      await mockState({ local: false });
      await page.goto(url("#/settings/notify")); await settle();
      const shown = await page.inputValue("#wh-url");
      assert(shown === "https://hook.example.com/***", "局域网应只看到开头：" + shown);
      assert((await sub()).includes("藏起来了"), "应说明后面藏起来了：" + await sub());
      assert(await page.$eval("#wh-save", b => b.disabled), "没改时保存键应置灰");
      // 点进去就全选：直接输入就是整个换掉
      await page.click("#wh-url");
      const sel = await page.$eval("#wh-url", el => [el.selectionStart, el.selectionEnd, el.value.length]);
      assert(sel[0] === 0 && sel[1] === sel[2], "点进遮掉的地址应全选：" + sel);
      // 原样传回（比如别的程序拿这份配置整份写回）：真地址不变
      await page.evaluate(v => fetch("/api/config", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ webhookUrl: v }) }), shown);
      let st = await mockState({});
      assert(st.config.webhookUrl === real, "原样传回不该盖掉真地址：" + st.config.webhookUrl);
      // 在遮掉的地址后面接着改：不收，真地址不变，提示说清楚
      await page.fill("#wh-url", shown + "x");
      await page.click("#wh-save"); await settle(300);
      st = await mockState({});
      assert(st.config.webhookUrl === real, "改了一半的不该存：" + st.config.webhookUrl);
      assert(await toastHas("藏起来的部分"), "提示里应说明 *** 是藏起来的部分");
      // 填全新的地址：存进去
      await page.fill("#wh-url", "https://bot.example.com/new?key=k2");
      await page.click("#wh-save"); await settle(300);
      st = await mockState({});
      assert(st.config.webhookUrl === "https://bot.example.com/new?key=k2" && await toastHas("Webhook 已保存"), "填新地址应存进去：" + st.config.webhookUrl);
    } finally { await mockState({ local: true, webhookUrl: "" }); }
  });
  // ==================== 窗口与设置结束 ====================

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
    assert((await page.textContent("#sc-hint")).includes(`已设为 ${got}`) && (await page.textContent("#sc-hint")).includes("之后每天该时间执行"), "快捷项后的说明：" + await page.textContent("#sc-hint"));
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
      assert((await page.textContent("#t-slow-title")) === "maven.neoforged.net 连接超时，已重试 3 次", "后端报的超时：" + await page.textContent("#t-slow-title"));
    } finally {
      await page.evaluate(() => { delete window.__MD_STALL_MS; return fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskStage: 2, taskNet: null }) }); });
    }
  });
  // ==================== 无障碍（测试报告「交互与可用性观察」5、6、9–12：测试员用无障碍接口操作界面时撞上的） ====================
  // 读 Chromium 自己算出的无障碍树：WebView2 交给 Windows UI Automation 的就是这一份（角色、可访问名称、描述、链接地址）
  const cdp = await ctx.newCDPSession(page);
  const axProp = (n, k) => ((n.properties || []).find(p => p.name === k) || {}).value;
  const axTree = async () => (await cdp.send("Accessibility.getFullAXTree")).nodes.filter(n => !n.ignored && n.role).map(n => ({
    role: n.role.value, name: ((n.name && n.name.value) || "").trim(), desc: ((n.description && n.description.value) || "").trim(),
    url: (axProp(n, "url") || {}).value || "" }));
  const AX_CTRL = new Set(["button", "link", "textbox", "searchbox", "combobox", "spinbutton", "checkbox", "switch", "slider"]);
  // 同一页里可访问名称相同的控件、没有名字的控件。
  // 例外：同名而且指向同一个地址的链接。侧栏的「服务器」和详情页面包屑的「服务器」、侧栏的「新建服务器」和服务器列表页头的「新建服务器」、
  // 游戏内 AI 没配置时的两处「去设置」，去的都是同一个地方，名字一样才对
  const axProblems = async () => {
    const seen = new Map(), out = [];
    for (const n of await axTree()) {
      if (!AX_CTRL.has(n.role)) continue;
      if (!n.name) { out.push("没有名字的 " + n.role); continue; }
      const k = n.role + "「" + n.name + "」", urls = seen.get(k) || [];
      urls.push(n.url); seen.set(k, urls);
    }
    for (const [k, urls] of seen) if (urls.length > 1 && !(k.startsWith("link") && urls.every(u => u && u === urls[0]))) out.push(k + "×" + urls.length);
    return out;
  };
  const byName = (role, name) => page.getByRole(role, { name, exact: true });

  await run("无障碍：各主要页面、弹窗和 AI 助手面板里没有同名的按钮 / 链接 / 输入框，也没有读不到名字的控件（同名链接指向同一处的除外）", async () => {
    const bad = [];
    const check = async label => { const p = await axProblems(); if (p.length) bad.push(label + "：" + p.join("、")); };
    const PACK_I = "#/inst/" + encodeURIComponent(PACK) + "/";
    try {
      await gcState(gcReady); // AI 配好：设置 → AI、游戏内 AI 的控件都画出来
      await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true } } } }, status: { [PACK]: "running" } });
      await musicState({ music: { reset: true, inst: { [PACK]: { bridge: "main" } } } });
      const pages = [["服务器列表", "#/instances"], ["新建服务器", "#/create"], ["隧道", "#/tunnels"],
        ...["appearance", "download", "storage", "java", "remote", "notify", "ai", "startup", "about"].map(s => ["设置 → " + s, "#/settings/" + s]),
        ...["console", "players", "backups", "tasks", "res", "gameai", "common", "raw", "jvm"].map(t => ["生存服 → " + t, "#/inst/" + inst + "/" + t]),
        ...["console", "players", "backups", "tasks", "music", "companion", "common", "jvm"].map(t => ["整合包 → " + t, PACK_I + t])];
      for (const [label, h] of pages) { await page.goto(url(h)); await settle(700); await check(label); }
      await page.goto(url("#/inst/" + inst + "/res")); await settle(400);
      await page.click("#res-go"); await settle(500); await check("资源页搜索结果");
      await page.goto(url("#/tunnels")); await settle(500);
      await page.click("#tn-toggle"); await settle(200); await check("隧道的添加表单");
      await page.goto(url(PACK_I + "companion")); await settle(900);
      await page.click("#cp-spawn"); await page.waitForSelector(".modal"); await check("刷出伙伴弹窗");
      await page.keyboard.press("Escape"); await settle(200);
      await page.goto(url("#/instances")); await settle(600);
      await page.click("#ai-btn"); await page.waitForSelector("#ai-panel"); await settle(300); await check("服务器列表 + AI 助手面板");
      await page.keyboard.press("Escape"); await settle(200);
    } finally { await resetGC(); await resetCP(); await resetMusic(); }
    assert(!bad.length, bad.join(" | "));
  });

  await run("无障碍：同名按钮带上对象——卡片上的启动 / 停止 / 管理 / 更多操作带服务器名；游戏内 AI 的 7 个「保存」各不相同；资源页的「安装」带模组名，装好后跟着变", async () => {
    try {
      await page.goto(url("#/instances")); await settle(700);
      const cards = await page.$$eval(".card[data-name]", cs => cs.map(c => ({ n: c.dataset.name, act: c.querySelector('[data-act="start"]') ? "启动" : "停止" })));
      assert(cards.length === 4, "卡片数：" + cards.length);
      for (const c of cards)
        assert(await byName("button", c.act + " " + c.n).count() === 1 && await byName("link", "管理 " + c.n).count() === 1 && await byName("button", "更多操作 " + c.n).count() === 1,
          `卡片「${c.n}」的按钮名字没带服务器名`);
      assert(await byName("link", "管理").count() === 0 && await byName("button", "启动").count() === 0 && await byName("button", "停止").count() === 0, "不该再有光秃秃的「管理」「启动」「停止」");
      // 游戏内 AI：触发前缀、别名、四项限流、自定义正则（展开高级识别后才露出来）
      await gcState(gcReady); await openGC();
      await page.click("#gc-adv-head"); await settle(200);
      const saves = (await axTree()).filter(n => n.role === "button" && n.name.startsWith("保存")).map(n => n.name).sort();
      const want = ["保存触发前缀", "保存别名", "保存每人冷却", "保存每人每天", "保存全服每天", "保存全服每天的 token", "保存自定义正则"].sort();
      assert(JSON.stringify(saves) === JSON.stringify(want), "游戏内 AI 的保存按钮：" + saves.join("、"));
      // 资源页
      await page.goto(url("#/inst/" + inst + "/res")); await settle(400);
      await page.click("#res-go"); await settle(500);
      const titles = await page.$$eval("[data-rt]", bs => bs.map(b => b.dataset.rt));
      const installs = (await axTree()).filter(n => n.role === "button" && n.name.startsWith("安装")).map(n => n.name);
      assert(titles.length === 3 && JSON.stringify(installs) === JSON.stringify(titles.map(t => "安装 " + t)), "安装按钮的名字：" + installs.join("、"));
      await byName("button", "安装 Chunky").click(); await settle(400);
      assert(await byName("button", "已安装 Chunky").count() === 1, "装好后名字应跟着按钮上的字变成「已安装 Chunky」");
    } finally { await resetGC(); }
  });

  await run("无障碍：折叠行「高级识别」「高级选项」的名字就是标题、副标题是描述；伙伴连接那一行名字不带版本号；刷出伙伴弹窗的「主人」按名字找得到；AI 面板的关闭按钮叫「收起 AI 助手」", async () => {
    try {
      await gcState(gcReady); await openGC();
      const adv = (await axTree()).find(n => n.role === "button" && n.name.startsWith("高级识别"));
      assert(adv && adv.name === "高级识别" && adv.desc.startsWith("认不出聊天时再来改"), "高级识别：" + JSON.stringify(adv));
      assert(await byName("button", "高级识别").getAttribute("aria-expanded") === "false", "收起时 aria-expanded 应是 false");
      await byName("button", "高级识别").click(); await settle(200);
      assert(await byName("button", "高级识别").getAttribute("aria-expanded") === "true", "展开后 aria-expanded 应是 true");
      await openMusic({ inst: { [PACK]: { bridge: "main" } } });
      const mu = (await axTree()).find(n => n.role === "button" && n.name.startsWith("高级选项"));
      assert(mu && mu.name === "高级选项" && mu.desc.includes("桥接模组的加载方式"), "高级选项：" + JSON.stringify(mu));
      await resetMusic();
      await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true } } } }, ai: { reset: true, ...AI_DEEPSEEK, key: "sk-x" }, status: { [PACK]: "running" } });
      await openCP();
      const link = (await axTree()).find(n => n.name.startsWith("和伙伴模组的连接"));
      assert(link && link.role === "group" && link.name === "和伙伴模组的连接" && link.desc.includes("已连上") && link.desc.includes("0.2.0"), "连接那一行：" + JSON.stringify(link));
      await page.click("#cp-spawn"); await page.waitForSelector(".modal");
      const owner = byName("combobox", "主人");
      assert(await owner.count() === 1 && await byName("textbox", "名字").count() === 1 && await byName("combobox", "出生在").count() === 1, "弹窗里的名字、主人、出生在应能按名字找到");
      await owner.fill("Steve");
      assert(await page.$eval("#cs-owner", i => i.value) === "Steve", "按名字找到的应是「主人」输入框");
      await page.keyboard.press("Escape"); await settle(200);
    } finally { await resetGC(); await resetCP(); await resetMusic(); }
    await page.goto(url("#/instances")); await settle(500);
    await page.click("#ai-btn"); await page.waitForSelector("#ai-panel");
    const fold = byName("button", "收起 AI 助手");
    assert(await fold.count() === 1 && await fold.getAttribute("title") === "收起（Esc）", "面板的关闭按钮应叫「收起 AI 助手」，提示同步");
    assert(await byName("button", "关闭 AI 助手").count() === 0 && await byName("button", "关闭").count() === 1, "窗口三键的「关闭」照旧、只此一个");
    await fold.click(); await settle(300);
    assert(!(await page.$("#ai-panel")), "点「收起 AI 助手」应收起面板");
  });

  await run("无障碍：开关的 input 叠在胶囊上、尺寸相当（不再是 1×1），点胶囊点中的是 input 并即时生效；键盘焦点环画在胶囊上；带说明文字的开关也对得齐", async () => {
    await page.goto(url("#/settings/startup")); await settle(500);
    const geo = sel => page.evaluate(s => {
      const i = document.querySelector(s), a = i.getBoundingClientRect(), b = i.nextElementSibling.getBoundingClientRect();
      const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
      return { w: a.width, h: a.height, dx: Math.abs(a.left + a.width / 2 - b.left - b.width / 2), dy: Math.abs(a.top + a.height / 2 - b.top - b.height / 2), hit: hit === i };
    }, sel);
    const sel = '[data-cfg="minimizeToTray"]';
    const g = await geo(sel), box = await page.locator(sel).boundingBox();
    assert(box.width >= 20 && box.height >= 20 && g.dx <= 1 && g.dy <= 1 && g.hit, "开关 input 的位置：" + JSON.stringify({ box, g }));
    const was = await page.$eval(sel, i => i.checked), before = reqs.length;
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2); await settle(250);
    assert(await page.$eval(sel, i => i.checked) === !was && putConfig(before, "minimizeToTray"), "点胶囊正中应切换开关并即时保存");
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2); await settle(250); // 复原
    await page.focus(sel); await page.keyboard.press("Shift+Tab"); await page.keyboard.press("Tab");
    const ring = await page.$eval(sel, i => document.activeElement === i && getComputedStyle(i.nextElementSibling).outlineStyle);
    assert(ring === "solid", "Tab 到开关时焦点环应画在胶囊上：" + ring);
    // 隧道表单里的开关：label 里还有两行说明，胶囊垂直居中，input 也要跟着居中
    await page.goto(url("#/tunnels")); await settle(500);
    await page.click("#tn-toggle"); await settle(200);
    const t = await geo("#tn-auto");
    assert(t.w >= 20 && t.h >= 20 && t.dx <= 1 && t.dy <= 1 && t.hit, "带说明的开关：" + JSON.stringify(t));
  });

  await run("无障碍：玩家页的 OP、踢、移除是按钮（键盘 Tab 得到、读屏读得出对象）；新建向导的核心卡是一组单选，方向键换、空格选、回车下一步", async () => {
    await page.goto(url(`#/inst/${inst}/players`)); await settle(600);
    const xs = await page.$$eval(".p-x", bs => bs.map(b => ({ tag: b.tagName, name: b.getAttribute("aria-label"), tab: b.tabIndex })));
    assert(xs.length > 0 && xs.every(x => x.tag === "BUTTON" && x.name && x.tab === 0), "玩家页的小按钮：" + JSON.stringify(xs.slice(0, 3)));
    assert(xs.some(x => x.name.startsWith("OP ")) && xs.some(x => x.name.startsWith("踢 ")) && xs.some(x => x.name.startsWith("移除 ")), "OP、踢、移除都带上玩家名：" + JSON.stringify(xs.map(x => x.name)));
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    const grid = await page.$eval("#core-grid", g => ({ role: g.getAttribute("role"), name: g.getAttribute("aria-label") }));
    assert(grid.role === "radiogroup" && grid.name, "核心列表应是一组单选：" + JSON.stringify(grid));
    const state = () => page.$$eval("#core-grid [role=radio]", cs => cs.map(c => ({ id: c.dataset.id, on: c.getAttribute("aria-checked"), tab: c.tabIndex })));
    let s = await state();
    assert(s.filter(c => c.tab === 0).length === 1, "Tab 顺序里只有一张卡：" + JSON.stringify(s));
    await page.focus("#core-grid [tabindex='0']");
    const first = await page.evaluate(() => document.activeElement.dataset.id);
    await page.keyboard.press("ArrowRight"); await settle(100);
    s = await state();
    const focused = await page.evaluate(() => document.activeElement.dataset.id);
    const sel = s.find(c => c.on === "true");
    assert(sel && sel.id === focused && sel.id !== first && sel.tab === 0 && s.filter(c => c.tab === 0).length === 1, "方向键选中下一张：" + JSON.stringify({ first, focused, sel }));
    assert(!(await page.$eval("#wnext", b => b.disabled)), "选中后「下一步」可点");
    await page.keyboard.press("Enter"); await settle();
    assert((await page.$("#core-grid")) === null, "回车应进入下一步");
    await page.goto(url("#/instances")); await settle();
  });
  // ==================== 无障碍结束 ====================

  // ==================== 接管已有服务器、换 Java ====================
  {
    const mockState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const neoArgs = "args:libraries/net/neoforged/neoforge/21.1.265/win_args.txt";

    await run("接管已有服务器：新建页第三张卡片进入；文件夹不对就地提示、不出表单；识别后预填名称、类型、版本、内存，换启动文件时类型跟着换；没同意 EULA 不发请求；接管后任务页说的是「接管」", async () => {
      await mockState({ instReset: true, adoptScan: null });
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-adopt"); await settle(500);
      assert(await page.textContent(".page-head h1") === "接管已有服务器", "没进入接管页");
      assert(await page.$eval("#ad-go", b => b.disabled), "还没识别就能点「接管」");
      // 没填、选了整个磁盘、选到了上一层：错误写在文件夹那一栏下面，表单不出现
      await page.click("#ad-scan"); await settle(200);
      assert((await page.textContent("#ad-dir-err")).includes("请先选择"), "没填文件夹时没提示");
      for (const [dir, want] of [["D:\\", "整个磁盘"], ["E:\\空文件夹", "请选到具体那一个文件夹"]]) {
        await page.fill("#ad-dir", dir); await page.click("#ad-scan"); await settle(400);
        assert((await page.textContent("#ad-dir-err")).includes(want), `${dir} 应提示「${want}」：` + await page.textContent("#ad-dir-err"));
        assert(await page.$eval("#ad-form", f => f.innerHTML === "") && await page.$eval("#ad-go", b => b.disabled), dir + " 识别失败还出了表单");
      }
      // 「浏览」选完直接识别（仿真后端选的是 E:\MCServers）
      await page.click("#ad-browse"); await settle(700);
      const v = await page.evaluate(() => ({
        dir: $("#ad-dir").value, err: $("#ad-dir-err").textContent, name: $("#ad-name").value, core: $("#ad-core").value, mc: $("#ad-mc").value,
        mem: $("#ad-mem").value, targets: $("#ad-target").options.length, java: $("#ad-java").value, notes: $("#ad-notes").textContent,
        eulaHidden: $("#ad-eula-box").hidden, what: $("#ad-what").textContent, go: $("#ad-go").disabled, noWorldHidden: $("#ad-noworld").hidden,
      }));
      assert(v.dir === "E:\\MCServers" && v.err === "", "文件夹：" + JSON.stringify(v));
      assert(v.name === "create-pack" && v.core === "neoforge" && v.mc === "1.21.1" && v.mem === "6144" && v.targets === 2 && v.java === "0", "预填不对：" + JSON.stringify(v));
      assert(v.notes.includes("2 个能启动的文件") && !v.eulaHidden && !v.go && v.noWorldHidden, "提醒 / EULA / 按钮 / 有存档时不该说没看到存档：" + JSON.stringify(v));
      assert(v.what.includes("run.before-minedeck.bat") && v.what.includes("先停止原来的服务器"), "没写明会往文件夹里写什么：" + v.what);
      // 换启动文件：类型和版本换成那个文件认出来的
      await page.selectOption("#ad-target", "jar:server.jar");
      assert(await page.$eval("#ad-core", s => s.value) === "vanilla", "换了启动文件，类型没跟着换");
      await page.selectOption("#ad-target", neoArgs);
      assert(await page.$eval("#ad-core", s => s.value) === "neoforge", "换回来后类型没跟着换");
      // 代理端没有 EULA
      await page.selectOption("#ad-core", "velocity");
      assert(await page.$eval("#ad-eula-box", b => b.hidden) && await page.textContent("#ad-mc-label") === "代理端版本", "代理端不该要 EULA");
      await page.selectOption("#ad-core", "neoforge");
      // 没同意 EULA、名称为空：就地报错，不发请求
      await page.fill("#ad-name", "");
      await page.click("#ad-go"); await settle(300);
      let st = await mockState({});
      assert(st.lastAdopt === null, "没同意 EULA 就发了请求");
      assert((await page.textContent("#ad-name-err")).includes("起个名字") && (await page.textContent("#ad-eula-err")).includes("EULA"), "没有就地提示");
      await page.fill("#ad-name", "朋友的整合包");
      await page.check("#ad-eula");
      await page.selectOption("#ad-java", "25");
      await page.click("#ad-go"); await settle(1200);
      st = await mockState({});
      const a = st.lastAdopt || {};
      assert(a.dir === "E:\\MCServers" && a.name === "朋友的整合包" && a.core === "neoforge" && a.mc === "1.21.1" && a.build === "21.1.265" &&
        a.launchTarget === neoArgs && a.xmxMb === 6144 && a.javaMajor === 25 && a.eula === true, "接管的请求：" + JSON.stringify(a));
      assert(page.url().endsWith("#/task/task-adopt"), "没去任务页：" + page.url());
      const t = await page.evaluate(() => ({ h1: $(".page-head h1").textContent, label: $("#t-label").textContent, foot: $("#t-foot").textContent, href: $("#t-foot a").getAttribute("href"),
        toasts: [...document.querySelectorAll(".toast")].map(x => x.textContent) }));
      assert(t.h1 === "正在接管服务器" && t.label === "接管完成" && t.foot.includes("进入控制台") && t.href === "#/inst/" + encodeURIComponent("朋友的整合包"), "任务页：" + JSON.stringify(t));
      assert(t.toasts.some(x => x.includes("接管成功")) && !t.toasts.some(x => x.includes("创建成功")), "提示应是接管成功：" + JSON.stringify(t.toasts));
    });

    await run("接管已有服务器：改了文件夹没重新识别就不能接管；已经同意过 EULA 的不用再勾；MineDeck 管过的文件夹说明设置会带回来", async () => {
      await mockState({ adoptScan: { dir: "D:\\old\\S1", name: "S1", core: "paper", mc: "1.21.4", build: "", launchTarget: "jar:paper.jar",
        targets: [{ target: "jar:paper.jar", label: "paper.jar（Paper 1.21.4）", core: "paper", mc: "1.21.4", build: "", javaMajor: 0 }],
        javaMajor: 0, xmxMb: 0, extraJvm: [], port: 25565, eula: true, hasWorld: false, managed: true, notes: [] } });
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-adopt"); await settle(500);
      await page.fill("#ad-dir", "D:\\old\\S1"); await page.keyboard.press("Enter"); await settle(600);
      const v = await page.evaluate(() => ({ eulaHidden: $("#ad-eula-box").hidden, what: $("#ad-what").textContent, notes: !!$("#ad-notes"), mem: $("#ad-mem").value, go: $("#ad-go").disabled,
        noWorldHidden: $("#ad-noworld").hidden, noWorld: $("#ad-noworld").textContent }));
      assert(v.eulaHidden && !v.notes && !v.go, "已经同意过 EULA 不用再勾、没有提醒时不画提醒框：" + JSON.stringify(v));
      assert(v.what.includes("曾由 MineDeck 管理") && !v.noWorldHidden && v.noWorld.includes("没看到存档"), "说明：" + JSON.stringify(v));
      // 代理端本来就没有存档：不提
      await page.selectOption("#ad-core", "velocity");
      assert(await page.$eval("#ad-noworld", x => x.hidden), "代理端不该提示没看到存档");
      await page.selectOption("#ad-core", "paper");
      assert(v.mem === "4096", "没读到原来的内存时默认 4 GB：" + v.mem);
      await page.type("#ad-dir", "x"); await settle(200);
      assert(await page.$eval("#ad-form", f => f.innerHTML === "") && await page.$eval("#ad-go", b => b.disabled), "改了文件夹，旧的识别结果还留着");
      await mockState({ adoptScan: null });
    });

    await run("内存 / 启动：能给服务器换 Java——列出 MineDeck 下载的、本机装的和可以现下的，标出现在用的；已有的直接换，本机没有的去任务页下载", async () => {
      await mockState({ instReset: true });
      const pack = encodeURIComponent("机械动力整合包");
      await page.goto(url(`#/inst/${pack}/jvm`)); await settle(900);
      let o = await page.evaluate(() => ({ opts: [...$("#jm-java").options].map(x => x.textContent), sel: $("#jm-java").selectedOptions[0].textContent, off: $("#jm-java-set").disabled }));
      assert(o.sel === "Java 17（MineDeck 下载的） · 现在用的" && o.off, "现在用的没标出来，或者没改就能点更换：" + JSON.stringify(o));
      assert(o.opts.some(x => x.startsWith("Java 8（本机装的 1.8.0_421：C:\\Program Files\\Java\\")) && o.opts.includes("Java 25（本机没有，下载一份，约 50 MB）") &&
        !o.opts.some(x => x.startsWith("Java 21（本机没有")), "选项：" + JSON.stringify(o.opts));
      const pick = async text => page.selectOption("#jm-java", { index: await page.$eval("#jm-java", (s, t) => [...s.options].findIndex(x => x.textContent.startsWith(t)), text) });
      await pick("Java 8（");
      assert(!(await page.$eval("#jm-java-set", b => b.disabled)), "选了别的 Java，按钮没亮");
      await page.click("#jm-java-set"); await settle(500);
      let st = await mockState({});
      assert(st.lastJava && st.lastJava.inst === "机械动力整合包" && st.lastJava.major === 8 && st.lastJava.path === "C:\\Program Files\\Java\\jre1.8.0_421\\bin\\java.exe", "换 Java 的请求：" + JSON.stringify(st.lastJava));
      o = await page.evaluate(() => ({ sel: $("#jm-java").selectedOptions[0].textContent, off: $("#jm-java-set").disabled, badge: $("#d-java").textContent,
        marked: [...$("#jm-java").options].filter(x => x.textContent.includes("现在用的")).length, toasts: [...document.querySelectorAll(".toast")].map(x => x.textContent) }));
      assert(o.sel.startsWith("Java 8（") && o.sel.endsWith("现在用的") && o.marked === 1 && o.off && o.badge === "Java 8", "换完之后：" + JSON.stringify(o));
      assert(o.toasts.some(x => x.includes("已换成 Java 8") && x.includes("下次启动生效")), "提示：" + JSON.stringify(o.toasts));
      // 本机没有的版本：去任务页，说的是「更换 Java」，完成后回到这台服务器的「内存 / 启动」
      await pick("Java 25（");
      await page.click("#jm-java-set"); await settle(1200);
      st = await mockState({});
      assert(st.lastJava.major === 25 && st.lastJava.path === "", "下载的请求不带路径：" + JSON.stringify(st.lastJava));
      const t = await page.evaluate(() => ({ url: location.hash, h1: $(".page-head h1").textContent, label: $("#t-label").textContent, link: $("#t-foot a").textContent.trim(), href: $("#t-foot a").getAttribute("href") }));
      assert(t.url === "#/task/task-java" && t.h1 === "正在更换 Java" && t.label === "Java 已换好" && t.link === "完成，回到服务器" && t.href === `#/inst/${pack}/jvm`, "任务页：" + JSON.stringify(t));
      await mockState({ instReset: true });
    });

    await run("删除：不勾「同时删除文件夹」时按钮写「移出列表」；接管进来的服务器默认不勾、写出原来的文件夹，确认后只移出列表", async () => {
      await mockState({ instReset: true });
      const open = async () => {
        await page.goto(url("#/instances")); await settle();
        await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
        await page.click(".dropmenu button.danger");
        await settle(250);
        return page.evaluate(() => ({ checked: $("#modal-root input[data-k=files]").checked, ok: $("#modal-root [data-x=ok]").textContent, hint: $("#modal-root .hint").textContent,
          focused: document.activeElement.textContent.trim() }));
      };
      let d = await open();
      assert(d.checked && d.ok === "删除服务器" && d.focused === "保留" && d.hint.includes("接管已有服务器") && !d.hint.includes("下次启动会重新出现"), "自己新建的：" + JSON.stringify(d));
      await page.uncheck("#modal-root input[data-k=files]");
      assert(await page.textContent("#modal-root [data-x=ok]") === "移出列表", "不删文件时按钮应写「移出列表」");
      await page.click('#modal-root [data-x="cancel"]'); await settle(250);
      // 接管进来的：默认不删文件
      await mockState({ inst: { name: "机械动力整合包", adopted: true, dir: "E:\\MCServers\\create-pack" } });
      d = await open();
      assert(!d.checked && d.ok === "移出列表" && d.focused === "保留" && d.hint.includes("接管进来的") && d.hint.includes("E:\\MCServers\\create-pack"), "接管进来的：" + JSON.stringify(d));
      await page.click('#modal-root [data-x="ok"]'); await settle(500);
      const st = await mockState({});
      assert(st.lastDelete && st.lastDelete.inst === "机械动力整合包" && st.lastDelete.files === false, "删除的请求：" + JSON.stringify(st.lastDelete));
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("移出列表") && x.includes("文件还在原处")), "提示：" + JSON.stringify(toasts));
      await mockState({ instReset: true });
      await page.goto(url("#/instances")); await settle();
    });
  }

  // ---- 导入逐文件处置 + Java 提示（fix-custom-mod-choice）----
  await run("导入的待决文件面板：逐项列出；跳过、本地代替、未确认的信任各自结算；同站点第二次不用再确认", async () => {
    const mkInst = encodeURIComponent("机械动力整合包");
    const task = {
      id: "task-im", title: "导入整合包 魔改模组包", ended: true, result: "机械动力整合包",
      warnings: [
        { kind: "import-kept", title: "按原样保留 1 个文件", note: "这些文件没有从网上下载，用的是整合包覆盖文件夹或本地来源文件夹里已有的那份。", items: [{ name: "mods/inherited.jar（来自本地来源文件夹，未校验）" }] },
        { kind: "import-pending", title: "3 个文件待处理", note: "这些文件没法自动下载。" },
        { kind: "java-req", title: "有模组要求 Java 25，这台服务器现在是 Java 17", note: "这只是提醒，不影响导入结果。", items: [{ name: "modded-fabric.jar（modded，要 Java 25）" }] },
      ],
      pending: [
        { id: "p1", path: "mods/modded-custom-host.jar", reason: "host", host: "files.example.net", note: "下载地址不在 MineDeck 允许的站点列表里（只允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载", urls: ["https://files.example.net/modded-custom-host.jar"], sha1: "aa", size: 12345, options: ["local", "skip", "trust"] },
        { id: "p2", path: "mods/no-hash.jar", reason: "nohash", note: "整合包没有提供这个文件的校验值，不能自动下载", options: ["skip", "local"] },
        { id: "p3", path: "mods/same-host-2.jar", reason: "host", host: "files.example.net", note: "下载地址不在 MineDeck 允许的站点列表里", urls: ["https://files.example.net/same-host-2.jar"], options: ["local", "skip", "trust"] },
      ],
    };
    try {
      await page.evaluate(t => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [t], trustedHosts: [] }) }), task);
      // 任务列表摘要：待决条数用 pendingCount 显示，入口写着「去处理」
      await page.goto(url("#/create")); await settle();
      assert((await page.textContent("#task-list")).includes("3 个文件待处理"), "列表摘要应写待决条数");
      assert((await page.textContent("#task-list")).includes("去处理"), "入口文字应为「去处理」");
      await page.click('#task-list a[href="#/task/task-im"]'); await settle();
      // 面板逐项列出；原因、站点、地址、大小都在；处置选项按原因给
      const rows = await page.$$eval("#t-pending .pnd-item", els => els.map(e => e.textContent));
      assert(rows.length === 3, "应逐项列出 3 个：" + JSON.stringify(rows));
      const hostRow = rows.find(r => r.includes("mods/modded-custom-host.jar"));
      assert(hostRow && hostRow.includes("files.example.net"), "host 项应写出站点");
      const p1Opts = await page.$$eval('#t-pending .pnd-item[data-id="p1"] input[type=radio]', els => els.map(e => e.value));
      const p2Opts = await page.$$eval('#t-pending .pnd-item[data-id="p2"] input[type=radio]', els => els.map(e => e.value));
      assert(JSON.stringify(p1Opts) === JSON.stringify(["local", "skip", "trust"]), "host 项应有三种处置：" + JSON.stringify(p1Opts));
      assert(JSON.stringify(p2Opts) === JSON.stringify(["skip", "local"]), "缺哈希项不该能信任下载：" + JSON.stringify(p2Opts));
      // Java 提醒在告警区，带「内存 / 启动」入口
      const msgs = await page.textContent("#t-msgs");
      assert(msgs.includes("Java 25") && msgs.includes("Java 17"), "Java 提醒应在告警区：" + msgs);
      assert(await page.locator('#t-msgs a[href*="/jvm"]').count() === 1, "Java 提醒应有换 Java 入口");
      assert((await page.getAttribute('#t-msgs a[href*="/jvm"]', "href")).includes(mkInst), "入口应指向这台实例");
      // 未确认的信任：留在清单，原因是「没有确认过」
      await page.check('input[name="pnd-p1"][value="trust"]');
      assert(!(await page.$eval('.pnd-item[data-id="p1"] label.pnd-ack', e => e.hidden)), "选信任应露出风险确认");
      await page.click("#t-pending-apply"); await settle();
      let out = await page.textContent("#t-pending-out");
      assert(out.includes("处理失败") && out.includes("没有确认过"), "未确认信任不应下载：" + out);
      assert((await page.$$eval("#t-pending .pnd-item", els => els.length)) === 3, "没处理成的应留在清单");
      // 确认信任（勾风险）后成功；同站点的另一项不再要求确认
      await page.check('.pnd-item[data-id="p1"] .pnd-ackbox');
      await page.check('input[name="pnd-p3"][value="trust"]');
      await page.click("#t-pending-apply"); await settle();
      out = await page.textContent("#t-pending-out");
      assert(out.includes("已处理") && out.includes("校验通过") && out.includes("之前确认过"), "信任应下载成功、同站点免确认：" + out);
      assert((await page.$$eval("#t-pending .pnd-item", els => els.length)) === 1, "成功项应移出清单");
      const st = await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data));
      assert((st.trustedHosts || []).includes("files.example.net"), "确认过的站点应记下来：" + JSON.stringify(st.trustedHosts));
      // 跳过：成功并记录
      await page.check('input[name="pnd-p2"][value="skip"]');
      await page.click("#t-pending-apply"); await settle();
      assert((await page.textContent("#t-pending-out")).includes("已跳过"), "跳过应记录");
      assert(!(await page.locator("#t-pending .pnd-item").count()), "全部处理完后面板应收起");
      assert((await page.textContent("#t-pending")).includes("待决清单已全部处理完"), "收尾应写已处理完");
      // 从没有过待决文件的普通任务页不显示任何待决面板（曾误显示「已全部处理完」卡片）
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [{ id: "task-plain", title: "创建 Paper 服务器", ended: true }] }) }));
      await page.goto(url("#/task/task-plain")); await settle();
      assert(((await page.$eval("#t-pending", e => e.textContent)) || "").trim() === "", "没有待决过的任务不该出现待决面板");
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [], trustedHosts: [] }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("待决面板：选了用本地文件代替但没给路径时拦下，给路径后成功", async () => {
    const task = {
      id: "task-im2", title: "导入整合包 魔改模组包", ended: true, result: "机械动力整合包",
      pending: [{ id: "p1", path: "mods/modded.jar", reason: "host", host: "files.example.net", urls: ["https://files.example.net/modded.jar"], options: ["local", "skip", "trust"] }],
    };
    try {
      await page.evaluate(t => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [t], trustedHosts: [] }) }), task);
      await page.goto(url("#/task/task-im2")); await settle();
      await page.check('input[name="pnd-p1"][value="local"]');
      assert(!(await page.$eval('.pnd-item[data-id="p1"] input.pnd-local', e => e.hidden)), "选本地代替应露出路径输入");
      await page.click("#t-pending-apply"); await settle();
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("但没给路径")), "没给路径应拦下：" + JSON.stringify(toasts));
      assert((await page.$$eval("#t-pending .pnd-item", els => els.length)) === 1, "清单不该变");
      await page.fill('.pnd-item[data-id="p1"] input.pnd-local', "D:\\老服务器\\mods\\modded.jar");
      await page.click("#t-pending-apply"); await settle();
      const out = await page.textContent("#t-pending-out");
      assert(out.includes("已处理") && out.includes("modded.jar"), "给了路径应成功：" + out);
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [] }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("实例详情：模组要求比实例高的 Java 时提示换 Java 并给入口；导入表单有本地来源文件夹", async () => {
    // 1.20.1（Java 17）的机械动力整合包里有要 Java 25 的魔改模组（用户反馈的主场景）
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ modsNeed: { "机械动力整合包": 25 } }) }));
    await page.goto(url("#/inst/" + encodeURIComponent("机械动力整合包"))); await settle(600);
    const hint = await page.textContent("#d-java-hint").catch(() => "");
    assert(hint.includes("Java 25") && hint.includes("Java 17") && hint.includes("内存 / 启动"), "应提示模组要求与现状：" + hint);
    await page.click("#d-java-hint a"); await settle();
    assert(page.url().includes("/jvm"), "入口应到「内存 / 启动」：" + page.url());
    // 没有声明要求的实例不显示
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ modsNeed: {} }) }));
    await page.goto(url("#/inst/" + encodeURIComponent("生存服 · 朋友"))); await settle(600);
    assert(!(await page.locator("#d-java-hint").count()), "没有要求就不该有提示");
    // 导入表单：本地来源文件夹字段在，浏览走选文件夹（从「新建服务器」的第二张卡片进入）
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-import"); await settle(600);
    assert((await page.locator("#im-src").count()) === 1, "导入表单应有本地来源文件夹输入框");
    await page.click("#im-src-browse"); await settle();
    assert((await page.inputValue("#im-src")).includes("E:\\MCServers"), "浏览应回填 pickdir 的路径");
  });

  // ---- 实例交接包（.mdpack） ----
  {
    const mockState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    await run("导出交接包：卡片菜单进入、隐私提醒可见、确认后去任务页", async () => {
      await mockState({ instReset: true });
      await page.goto(url("#/instances")); await settle();
      await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
      await settle(200);
      const item = await page.evaluateHandle(() => [...document.querySelectorAll(".dropmenu button")].find(b => b.textContent.includes("导出交接包")));
      assert(item, "菜单里没有「导出交接包」");
      assert(await page.evaluate(el => !el.disabled, item), "「导出交接包」被禁用了");
      await item.asElement().click(); await settle(300);
      assert(await page.isVisible("#tr-mask"), "确认弹窗没弹出来");
      const m = await page.evaluate(() => ({ title: $("#tr-title").textContent, privacy: $("#tr-mask .wc-note").textContent, full: $("#tr-full").checked, go: $("#tr-go").textContent }));
      assert(m.title.includes("机械动力整合包"), "标题里没有实例名：" + m.title);
      assert(m.privacy.includes("白名单") && m.privacy.includes("密码会原样打包"), "隐私提醒：" + m.privacy);
      assert(m.full === false && m.go.includes("开始打包"), "弹窗状态：" + JSON.stringify(m));
      // 导出位置不存在：后端报「导出文件夹不可用: 路径」，输入框标红、下面写原因，弹窗留着；提示只写结论（原文带路径）
      await page.fill("#tr-dest", "Z:\\nope");
      await page.click("#tr-go"); await settle(400);
      const bad = await page.evaluate(() => ({ invalid: $("#tr-dest").classList.contains("invalid"), err: $("#tr-dest-err").textContent }));
      assert(bad.invalid && bad.err.includes("导出文件夹不可用") && await page.isVisible("#tr-mask"), "导出文件夹不可用时：" + JSON.stringify(bad));
      assert(await toastHas("无法导出交接包"), "应提示无法导出交接包");
      assert(!(await page.evaluate(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("Z:")))), "提示正文里不该有路径");
      await page.fill("#tr-dest", "E:\\交接包");
      // 这个包给谁：默认是交接，这里改成发给朋友
      assert(await page.isChecked('input[name="tr-mode"][value="handover"]'), "默认应是交给下一任服主");
      await page.check('input[name="tr-mode"][value="distributable"]');
      await page.click("#tr-go"); await settle(700);
      const st = await mockState({});
      assert(st.lastTransferExport && st.lastTransferExport.inst === "机械动力整合包" && st.lastTransferExport.full === false && st.lastTransferExport.dest === "E:\\交接包"
        && st.lastTransferExport.mode === "distributable", "导出请求：" + JSON.stringify(st.lastTransferExport));
      assert(page.url().endsWith("#/task/task-transfer-out"), "没去任务页：" + page.url());
      const t = await page.evaluate(() => ({ h1: $(".page-head h1").textContent, log: $("#task-log").textContent, go: $("#t-foot a").getAttribute("href") }));
      assert(t.h1 === "正在导出交接包" && t.go === "#/instances", "导出任务页：");
      await page.click("#t-foot a"); await settle(300);
      assert(page.url().endsWith("#/instances"), "完成按钮没回列表：" + page.url());
    });

    await run("交接包和整合包超过上限：上传之前就拦下，说清楚是太大了", async () => {
      await mockState({ instReset: true });
      await page.goto(url("#/create")); await settle();
      // 造不出几个 GB 的文件：把名字以 huge 开头的文件说成 9 GB。页面重新加载以后要再设一次
      const fakeHuge = () => page.evaluate(() => {
        const real = Object.getOwnPropertyDescriptor(Blob.prototype, "size").get;
        Object.defineProperty(File.prototype, "size", { configurable: true, get() { return this.name.startsWith("huge") ? 9 * 1024 ** 3 : real.call(this); } });
      });
      await fakeHuge();
      try {
        await page.click("#mode-transfer"); await settle(300);
        let since = reqs.length;
        await page.setInputFiles("#tp-file", { name: "huge.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK") });
        await settle(600);
        const tp = await page.textContent("#tp-file-err");
        assert(tp.includes("9.00 GB") && tp.includes("8 GB 导入上限") && tp.includes("接管已有服务器"), "交接包太大的提示：" + tp);
        assert(!reqs.slice(since).some(r => r.u === "/api/transfer/upload"), "太大的交接包不该开始上传");
        assert(await page.$eval("#tp-go", b => b.disabled), "太大的包不该能点「导入」");
        // 换一个正常大小的：提示消掉，照常上传
        await page.setInputFiles("#tp-file", { name: "pack.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK") });
        await settle(700);
        assert((await page.textContent("#tp-file-err")).trim() === "" && await page.$("#tp-name"), "换成正常的包以后应照常出摘要");
        assert(!(await page.$eval("#tp-go", b => b.disabled)) || await page.$("#tp-eula"), "正常的包应能继续往下填");
        // 已经传好一个包以后再选一个太大的：上一个包的摘要要清掉、按钮要灰掉，不然点「导入」导的是上一个包
        await page.setInputFiles("#tp-file", { name: "huge-2.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK") });
        await settle(500);
        assert((await page.textContent("#tp-file-err")).includes("8 GB 导入上限"), "第二次选太大的包也要提示");
        assert(!(await page.$("#tp-name")) && (await page.textContent("#tp-form")).trim() === "", "上一个包的摘要应清掉：" + await page.textContent("#tp-form"));
        assert(await page.$eval("#tp-go", b => b.disabled), "换成太大的包以后「导入」应灰掉");

        await page.goto(url("#/create")); await settle();
        await fakeHuge();
        await page.click("#mode-import"); await settle(400);
        since = reqs.length;
        await page.setInputFiles("#im-file", { name: "huge.zip", mimeType: "application/zip", buffer: Buffer.from("PK") });
        await page.click("#im-go"); await settle(400);
        const im = await page.textContent("#im-file-err");
        assert(im.includes("9.00 GB") && im.includes("4 GB 导入上限"), "整合包太大的提示：" + im);
        assert(!reqs.slice(since).some(r => r.u === "/api/import/modpack"), "太大的整合包不该开始上传");
      } finally {
        await page.evaluate(() => { delete File.prototype.size; });
      }
    });

    await run("导入交接包：新建页第四张卡片、上传出摘要、端口占用提示、提交后任务页说「交接」", async () => {
      await mockState({ instReset: true });
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-transfer"); await settle(300);
      assert(await page.textContent(".page-head h1") === "导入交接包", "没进入交接包向导");
      assert(await page.$eval("#tp-go", b => b.disabled), "没有包就能点「导入」");
      await page.setInputFiles("#tp-file", { name: "pack.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK") });
      await settle(700);
      const s = await page.evaluate(() => ({ name: $("#tp-name") && $("#tp-name").value, port: $("#tp-port") && $("#tp-port").value, kv: $("#tp-form") ? $("#tp-form").textContent : "" }));
      assert(s.name === "A 的服" && s.port === "25570", "摘要没带出名称与端口：" + JSON.stringify(s));
      assert(s.kv.includes("Paper 1.20.1") && s.kv.includes("36 个模组") && s.kv.includes("2.0 GB") && s.kv.includes("服主交接"), "包摘要展示：" + JSON.stringify(s.kv));
      // Java：默认跟原来一样，可以改
      assert((await page.$eval("#tp-java", e => e.options[0].textContent)).includes("跟原来一样：Java 17"), "Java 默认应是包里原来的");
      await page.selectOption("#tp-java", "21");
      // 包里的模组、插件是别人电脑上带来的程序：导入前要提醒确认来源，并说明 JVM 参数和定时任务不带过来
      // 来源提醒和「哪些不带过来」分成了两段：前一段是警告卡，后一段是紧跟着的一行说明
      const trust = await page.textContent("#tp-trust");
      const carry = await page.$eval("#tp-trust", e => e.nextElementSibling.textContent);
      assert(trust.includes("信得过") && trust.includes("在你的电脑上运行") && carry.includes("定时任务不会带过来"), "来源提醒：" + trust + " / " + carry);
      // 端口改成一个被占用的：给出行内提示（不拦提交）
      await mockState({ portBusy: 25599 });
      await page.fill("#tp-port", "25599");
      await settle(500);
      assert((await page.textContent("#tp-port-err")).includes("占用"), "端口占用没提示：" + await page.textContent("#tp-port-err"));
      await page.fill("#tp-name", "接手服");
      await page.fill("#tp-port", "25566");
      await page.check("#tp-eula");
      await page.click("#tp-go"); await settle(800);
      const st = await mockState({});
      assert(st.lastTransferImport && st.lastTransferImport.name === "接手服" && st.lastTransferImport.eula === true && +st.lastTransferImport.port === 25566 && +st.lastTransferImport.xmxMb >= 2048
        && st.lastTransferImport.javaMajor === 21,
        "导入请求：" + JSON.stringify(st.lastTransferImport));
      assert(page.url().endsWith("#/task/task-transfer-in"), "没去任务页：" + page.url());
      const t = await page.evaluate(() => ({ h1: $(".page-head h1").textContent, steps: $("#t-steps").textContent, go: $("#t-foot a"), href: $("#t-foot a").getAttribute("href") }));
      assert(t.h1 === "正在导入交接包" && t.steps.includes("解包服务器文件") && t.href === "#/inst/" + encodeURIComponent("接手服"), "任务页：" + JSON.stringify(t));
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("交接包导入成功")), "成功提示：" + JSON.stringify(toasts));
    });
  }

  // ==================== 自选模组 + 购物车 ====================
  const cartState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }), b);

  await run("向导「选模组 / 选插件」步（非 vanilla 第 5 步）：搜索后加进草稿清单；提交后建服完成即自动装模组，跳到模组安装任务页", async () => {
    // taskStage 4 = 建服任务一查就是已完成：提交后向导会等它结束，再带上草稿车发安装（安装任务是 kind "mods"）
    try {
      await cartState({ modCart: { reset: true }, taskStage: 4 });
      await page.goto(url("#/create")); await settle();
      await page.click("#mode-new"); await settle();
      await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await settle();
      await page.click("#wnext"); await settle();
      assert((await page.$("#build-list")) !== null, "没到构建步");
      await page.click("#wnext"); await settle();
      const pills = await page.$$eval(".step-pill", els => els.map(e => e.textContent));
      assert(pills.length === 5 && pills.some(t => t.includes("选插件")), "非 vanilla 应是 5 步、Paper 这一步应叫「选插件」：" + JSON.stringify(pills));
      assert(!(await page.$eval("#wnext", b => b.disabled)), "选模组是可选步，「下一步」应可直接点");
      // 搜索 → 假 hits → 加购一项：正是因为按钮带对象名，这里才点得到 LuckPerms 那一个（Paper 是插件端，搜出来的是插件）
      await page.fill("#mc-q", "LuckPerms"); await page.click("#mc-go"); await settle();
      assert((await page.textContent("#mc-hits")).includes("LuckPerms") && (await page.textContent("#mc-hits-title")).includes("LuckPerms"), "搜索结果没出现");
      await page.click('button[aria-label="加入清单 LuckPerms"]'); await settle(700);
      assert((await page.$$eval("#mc-list .mc-item", els => els.length)) === 1, "草稿车清单不是 1 项");
      assert((await page.$eval("#mc-count", el => el.textContent)).includes("1"), "草稿车计数不对");
      // 提交建服：草稿车非空时留在向导页等建服，建完自动把车装上再跳模组安装任务
      await page.click("#wnext"); await settle();
      assert((await page.$("#f-name")) !== null, "没到基本设置步");
      await page.fill("#f-name", "测试的模组服");
      await page.click(".eula-box .switch input");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      await settle(300);
      const h1 = await page.textContent(".page-head h1");
      assert(h1.includes("待装清单"), "任务页说法没有改成安装待装清单：" + h1);
      // 带草稿车安装的请求里要有「选模组」那一步上画出来的那些：后端拿它核对，草稿车之后被改过就不装
      const li = (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart.lastInstall;
      assert(li && li.includeDraft === true && Array.isArray(li.expect) && li.expect.join() === "luckperms" && Array.isArray(li.expectLocal) && li.expectLocal.length === 0,
        "安装请求应带上向导上看到的那一项（没有本地文件时 expectLocal 是空数组）：" + JSON.stringify(li));
      await page.waitForSelector("#t-foot a.btn.primary", { timeout: 6000 }); // 第 3 次查询后任务结束
      assert((await page.getAttribute("#t-foot a.btn.primary", "href")) === "#/inst/" + encodeURIComponent("测试的模组服"), "完成的去处在详情页才能看到");
    } finally { await cartState({ modCart: { reset: true }, taskStage: 2, instReset: true }); }
  });

  await run("实例详情「待装清单」标签：vanilla 没有；加入清单 → 查前置缺项 → 勾选补上 → 移除清空", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url("#/inst/" + encodeURIComponent("原版快照测试"))); await settle(600);
      assert((await page.$('.tabs .tab[data-t="cart"]')) === null, "原版实例不应有待装清单标签");
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      assert((await page.$('.tabs .tab[data-t="cart"]')) !== null, "模组实例缺待装清单标签");
      // 提示条（本地依赖核对）：已装的模组缺依赖时先说一声
      assert((await page.textContent("#mc-local-tip")).includes("缺 1 项前置"), "本地前置提示条没出现");
      // 搜索 → 加购（这台是 Forge 的模组服：搜出来的是模组）
      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
      assert((await page.$$eval("#mc-hits .mc-hit", els => els.length)) === 1, "搜 JEI 应只剩一条");
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      assert((await page.textContent("#mc-list")).includes("JEI") && (await page.textContent("#mc-count")).includes("1"), "加购之后的清单不对");
      assert(!(await page.$eval("#mc-actions", el => el.hidden)), "清空 / 安装按钮在车非空时应出现");
      // 检查依赖：列缺的必装项（默认勾上），勾选后一并入车
      await page.click("#mc-dep-go"); await settle();
      const depText = await page.textContent("#mc-dep-box");
      assert(depText.includes("Architectury") && depText.includes("必装"), "缺的依赖没有列出来：" + depText.slice(0, 80));
      assert((await page.$eval(".mc-miss input", el => el.checked)) === true, "缺依赖项应默认勾选");
      await page.click("#mc-dep-add"); await settle(900);
      assert((await page.textContent("#mc-count")).includes("2") && (await page.textContent("#mc-list")).includes("Architectury"), "勾选补齐后清单不对");
      assert((await page.textContent("#mc-dep-box")).includes("不缺前置"), "补齐后复查应显示不缺前置");
      // 可选依赖按单个加
      await page.click('button[aria-label="加入清单（可选前置）Cloth Config API"]'); await settle(900);
      assert((await page.textContent("#mc-count")).includes("3"), "可选依赖加不进来");
      // 移除一项，再把余下的清空
      await page.click('button[aria-label="移除 JEI"]'); await settle(700);
      assert(!(await page.textContent("#mc-list")).includes("JEI") && (await page.textContent("#mc-count")).includes("2"), "移除后清单不对");
      await page.click("#mc-clear"); await settle(700);
      assert((await page.$$eval("#mc-list .mc-item", els => els.length)) === 0 && (await page.$eval("#mc-actions", el => el.hidden)), "清空后清单与按钮没复位");
    } finally { await cartState({ modCart: { reset: true } }); }
  });

  await run("待装清单在打开之后被别处改过：点安装会被拦下并重新列出现在的清单，看过以后再点才装", async () => {
    // 往车里加东西不用确认（AI 助手、别的窗口都能加）：装的必须是页面上列着的那些
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      assert((await page.textContent("#mc-count")).includes("1"), "清单应是 1 项");
      // 页面没动的时候，别处又往这台服务器的车里加了一项
      await page.evaluate(i => fetch("/api/modcart", { method: "POST", body: JSON.stringify({ inst: i, projectIds: ["create"] }) }), PACK);
      await page.click("#mc-install"); await settle(900);
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("无法开始安装：待装清单和你刚才看到的不一样了")), "车变了应拦下并说明：" + JSON.stringify(toasts));
      assert(!page.url().includes("#/task/"), "被拦下时不该跳到任务页：" + page.url());
      assert((await page.textContent("#mc-count")).includes("2") && (await page.textContent("#mc-list")).includes("Create"), "应重新列出现在的清单");
      // 看过现在的清单再点：照常装，请求里带着页面上列着的两项
      await page.click("#mc-install");
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart.lastInstall;
      assert(li && Array.isArray(li.expect) && li.expect.slice().sort().join() === "create,jei", "安装请求应带上页面上列着的两项：" + JSON.stringify(li));
    } finally { await cartState({ modCart: { reset: true } }); await page.goto(url("#/instances")); await settle(); }
  });

  await run("vanilla 向导保持 4 步：步骤条无「选模组」，构建步照旧跳过直达基本设置", async () => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.click('.core-card[data-id="vanilla"]'); await page.click("#wnext"); await settle();
    await page.click("#wnext"); await settle(); // 构建步被跳过，直达基本设置
    const pills = await page.$$eval(".step-pill", els => els.map(e => e.textContent));
    assert(pills.length === 4 && !pills.some(t => /选模组|选插件/.test(t)), "vanilla 步骤条应为 4 步且无「选模组」：" + JSON.stringify(pills));
    assert((await page.$("#f-name")) !== null, "没从版本页直达基本设置");
    const titles = await page.$$eval(".step-pill", els => els.map(e => e.getAttribute("title")));
    assert(titles.includes("原版无需选构建"), "构建步的跳过提示没了：" + JSON.stringify(titles));
    await page.click("#wback"); await settle();
    assert((await page.$("#ver-list")) !== null, "基本设置「上一步」应回到版本页");
  });

  await run("切页竞态：待装清单的搜索只显示最后一次查询，离开后不再写页面", async () => {
    let release, seen;
    let gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    const pattern = "**/api/modcart/search*";
    await page.route(pattern, async route => {
      const q = new URL(route.request().url()).searchParams.get("q");
      if (q === "old" || q === "leave") { seen(); await gate; }
      await route.fulfill({ json: { ok: true, data: { type: "mod", hits: [{ project_id: q, slug: q, title: q, description: "测试", downloads: 1000, icon_url: "", license: "MIT" }] } } });
    });
    try {
      await page.goto(url(`#/inst/${inst}/cart`)); await settle(600);
      await page.fill("#mc-q", "old"); await page.click("#mc-go"); await requested;
      await page.fill("#mc-q", "new"); await page.click("#mc-go"); await settle();
      release(); await settle(600);
      const text = await page.textContent("#mc-hits");
      assert(text.includes("new") && !text.includes("old"), "旧查询覆盖了新结果：" + text.slice(0, 60));
      gate = new Promise(r => release = r); requested = new Promise(r => seen = r);
      const beforeErrors = errors.length;
      await page.fill("#mc-q", "leave"); await page.click("#mc-go"); await requested;
      await page.click('#sidebar a[data-nav="instances"]'); await settle();
      release(); await settle(600);
      assert(await page.locator("#inst-cards").count() === 1 && errors.length === beforeErrors, "离开后的响应引发报错或覆盖页面");
    } finally { if (release) release(); await page.unroute(pattern); }
  });

  // ---- 热门清单、从电脑添加本地文件、两栏版面（2026-11） ----
  const cartMock = async () => (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart;
  const packCart = `#/inst/${encodeURIComponent(PACK)}/cart`; // Forge 1.20.1 的模组服
  /* 一个「jar」：仿真后端只看开头是不是 PK，内容不同 SHA-1 就不同 */
  const jarFile = (name, body = name) => ({ name, mimeType: "application/java-archive", buffer: Buffer.from("PK" + body) });
  /* 走到向导的「选模组 / 选插件」那一步 */
  const gotoWizMods = async core => {
    await page.goto(url("#/create")); await settle();
    await page.click("#mode-new"); await settle();
    await page.click(`.core-card[data-id="${core}"]`); await page.click("#wnext"); await settle();
    await page.click("#wnext"); await settle();
    assert((await page.$("#build-list")) !== null, "没到构建步");
    await page.click("#wnext"); await settle();
    assert((await page.$("#wiz-mod-body .mc-wrap")) !== null, "没到选模组这一步");
  };

  await run("待装清单的热门清单：一打开就列出当前核心下最热门的；加载更多翻到第 5 页为止；类别筛选和关键词搜索，标题行跟着变；插件端叫插件", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(packCart)); await settle(700);
      assert((await page.textContent("#mc-hits-title")) === "热门模组 · 按下载量", "标题行：" + await page.textContent("#mc-hits-title"));
      assert((await page.textContent(".mc-hits-head")).includes("按当前核心与版本过滤") && (await page.textContent(".mc-hits-head")).includes("Modrinth"), "标题行旁边的说明没了");
      let rows = await page.$$eval("#mc-hits .mc-hit b", els => els.map(e => e.textContent));
      assert(rows.length === 20 && rows[0] === "Create" && rows[1] === "JEI", "没搜就应列出下载量最高的 20 个：" + rows.slice(0, 3).join());
      assert((await page.inputValue("#mc-q")) === "", "关键词应是空的");
      // 加载更多：一页 20 条接在后面，翻到第 5 页（100 个）就不给翻了，说清楚去哪儿找别的
      for (let i = 0; i < 4; i++) {
        assert((await page.textContent("#mc-more")).includes("加载更多"), "第 " + (i + 1) + " 页后面应有「加载更多」");
        await page.click("#mc-more"); await settle(350);
      }
      rows = await page.$$eval("#mc-hits .mc-hit b", els => els.map(e => e.textContent));
      assert(rows.length === 100 && new Set(rows).size === 100, "翻了 4 次应有 100 个不重复的：" + rows.length);
      assert((await page.$("#mc-more")) === null && (await page.textContent("#mc-more-end")).includes("最热门的 100 个"), "第 5 页之后不该再有「加载更多」");
      let mc = await cartMock();
      assert(mc.searches.map(x => x.offset).join() === "0,20,40,60,80" && mc.searches.every(x => x.q === "" && x.category === ""), "请求的翻页不对：" + JSON.stringify(mc.searches));
      // 类别筛选片：单选，默认「全部」
      assert((await page.getAttribute('.mc-chips .cp-chip[data-cat=""]', "aria-pressed")) === "true", "默认应选中「全部」");
      const chips = await page.$$eval(".mc-chips .cp-chip", els => els.map(e => e.textContent));
      assert(chips.join() === "全部,优化,实用,管理,世界生成,冒险,科技,魔法,生物,装备,存储,支持库", "模组的类别：" + chips.join());
      await page.click('.mc-chips .cp-chip[data-cat="library"]'); await settle(400);
      assert((await page.textContent("#mc-hits-title")) === "支持库类模组 · 按下载量", "选了类别以后的标题：" + await page.textContent("#mc-hits-title"));
      const pressed = await page.$$eval(".mc-chips .cp-chip", els => els.filter(e => e.getAttribute("aria-pressed") === "true").map(e => e.textContent));
      assert(pressed.join() === "支持库", "类别应是单选：" + pressed.join());
      rows = await page.$$eval("#mc-hits .mc-hit b", els => els.map(e => e.textContent));
      assert(rows[0] === "Cloth Config API" && rows[1] === "Architectury API" && !rows.includes("Create"), "按类别筛出来的不对：" + rows.slice(0, 4).join());
      mc = await cartMock();
      assert(mc.searches.at(-1).category === "library" && mc.searches.at(-1).offset === "0", "换类别应从头查：" + JSON.stringify(mc.searches.at(-1)));
      // 关键词搜索保留：和类别一起算；没结果给一句话
      await page.fill("#mc-q", "jei"); await page.press("#mc-q", "Enter"); await settle(400);
      assert((await page.textContent("#mc-hits-title")) === "“jei” 的搜索结果 · 支持库", "带关键词的标题：" + await page.textContent("#mc-hits-title"));
      assert((await page.textContent("#mc-hits-empty")).includes("换个关键词或类别"), "空结果应给一句话");
      await page.click('.mc-chips .cp-chip[data-cat=""]'); await settle(400);
      assert((await page.textContent("#mc-hits-title")) === "“jei” 的搜索结果", "回到全部以后的标题：" + await page.textContent("#mc-hits-title"));
      rows = await page.$$eval("#mc-hits .mc-hit b", els => els.map(e => e.textContent));
      assert(rows.join() === "JEI" && (await page.$("#mc-more")) === null, "搜 jei 应只有 JEI、没有加载更多：" + rows.join());
      // 同一页里没有同名、没名字的控件（筛选片、加入清单、加载更多、从电脑添加）
      await page.fill("#mc-q", ""); await page.click("#mc-go"); await settle(400);
      const bad = await axProblems();
      assert(!bad.length, "热门清单里有同名或没名字的控件：" + bad.join("、"));
      // 插件端：名词和类别跟着核心走
      await page.goto(url(`#/inst/${inst}/cart`)); await settle(700);
      assert((await page.textContent("#mc-hits-title")) === "热门插件 · 按下载量", "插件端的标题：" + await page.textContent("#mc-hits-title"));
      const pchips = await page.$$eval(".mc-chips .cp-chip", els => els.map(e => e.textContent));
      assert(pchips.includes("经济") && pchips.includes("聊天社交") && !pchips.includes("魔法"), "插件的类别：" + pchips.join());
      assert((await page.$$eval("#mc-hits .mc-hit b", els => els[0].textContent)) === "EssentialsX", "插件端应列出最热门的插件");
      assert((await page.getAttribute("#mc-q", "aria-label")) === "搜索插件", "搜索框的名字应跟着叫插件");
    } finally { await cartState({ modCart: { reset: true } }); }
  });

  await run("待装清单的热门清单加载失败或很慢：只在列表区说明并给「重试」，不弹提示；向导「选模组」这一步的「下一步」照常能点", async () => {
    try {
      await cartState({ modCart: { reset: true, searchFail: "连不上 Modrinth：等了太久没有回应" } });
      await gotoWizMods("fabric"); await settle(500);
      const errText = await page.textContent("#mc-hits-err");
      assert(errText.includes("连不上 Modrinth") && errText.includes("重试") && errText.includes("可以先跳过"), "列表区应说明出了什么事、下一步怎么办：" + errText);
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(!toasts.some(x => x.includes("Modrinth")), "自动加载失败不该弹提示：" + JSON.stringify(toasts));
      assert(!(await page.$eval("#wnext", b => b.disabled)), "加载失败不该挡住「下一步」");
      assert((await page.$("#mc-local-pick")) !== null && !(await page.$eval("#mc-local-pick", b => b.disabled)), "热门清单坏了，从电脑添加照常能用");
      // 重试：好了就列出来
      await cartState({ modCart: { searchFail: "" } });
      await page.click("#mc-retry"); await settle(500);
      assert((await page.$$eval("#mc-hits .mc-hit", els => els.length)) === 20 && (await page.$("#mc-hits-err")) === null, "重试以后应列出热门模组");
      // 很慢：骨架还在转的时候点「下一步」，马上就到下一步；晚到的结果不写页面、不报错
      await cartState({ modCart: { searchDelay: 1500 } });
      await page.click("#wback"); await settle(200);
      await page.click("#wnext"); await settle(200);
      assert((await page.getAttribute("#mc-hits", "aria-busy")) === "true" && (await page.$("#mc-hits .sk-row")) !== null, "加载中应是骨架");
      assert(!(await page.$eval("#wnext", b => b.disabled)), "加载很慢也不该挡住「下一步」");
      const before = errors.length;
      await page.click("#wnext"); await settle(300);
      assert((await page.$("#f-name")) !== null, "骨架还在转的时候点「下一步」应直接到基本设置");
      await settle(1600);
      assert((await page.$("#f-name")) !== null && errors.length === before, "晚到的热门清单不该写页面或报错");
    } finally { await cartState({ modCart: { reset: true } }); }
  });

  await run("待装清单从电脑添加本地文件：逐个上传，没加上的在清单下面逐个说明（不弹提示）；行上写明来源和提醒；加完自动检查前置；计数和「全部安装（N）」把本地文件算进去；移除、清空", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(packCart)); await settle(700);
      assert((await page.textContent("#mc-local-pick")).includes("从电脑添加…"), "缺「从电脑添加…」按钮");
      assert((await page.getAttribute("#mc-file", "accept")) === ".jar" && (await page.$eval("#mc-file", el => el.multiple && el.type === "file")), "应是能多选 .jar 的文件输入框");
      // 一次选 4 个：两个能加，一个不是 jar，一个打不开
      await page.setInputFiles("#mc-file", [jarFile("needs-geckolib-1.0.jar"), jarFile("sodium-fabric-0.5.8.jar"),
        { name: "readme.txt", mimeType: "text/plain", buffer: Buffer.from("hi") }, { name: "broken.jar", mimeType: "application/java-archive", buffer: Buffer.from("not a zip") }]);
      await page.waitForSelector("#mc-local-ok", { timeout: 8000 }); await settle(500); // 这一句只在全部传完以后才出现
      const rows = await page.$$eval("#mc-list .mc-local", els => els.map(e => e.textContent.replace(/\s+/g, " ").trim()));
      assert(rows.length === 2 && rows[0].includes("本地文件 · needs-geckolib-1.0.jar") && rows[1].includes("本地文件 · sodium-fabric-0.5.8.jar"), "清单里的本地文件：" + JSON.stringify(rows));
      assert(/本地文件 · needs-geckolib-1\.0\.jar · \d+ B/.test(rows[0]), "副行应是「本地文件 · 文件名 · 大小」：" + rows[0]);
      assert(rows[1].includes("给 Fabric 用的") && (await page.$$eval("#mc-list .mc-local .mc-local-warn", els => els.length)) === 1, "加载器不对的那一行应有提醒：" + rows[1]);
      const failText = (await page.textContent("#mc-local-err")).replace(/\s+/g, " ");
      assert(failText.includes("2 个文件没有加进清单") && failText.includes("readme.txt") && failText.includes("不是 .jar 文件") && failText.includes("broken.jar") && failText.includes("打不开"),
        "没加上的应逐个说明原因：" + failText);
      assert((await page.textContent("#mc-local-ok")).includes("已添加 2 个文件"), "应说明加上了几个：" + await page.textContent("#mc-local-ok"));
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(!toasts.some(x => x.includes("readme") || x.includes("broken")), "添加失败不该用提示框堆一串：" + JSON.stringify(toasts));
      // 计数、安装按钮都把本地文件算进去
      assert((await page.textContent("#mc-count")).includes("共 2 项"), "计数：" + await page.textContent("#mc-count"));
      assert((await page.textContent("#mc-install")).includes("全部安装（2）") && !(await page.$eval("#mc-actions", el => el.hidden)), "安装按钮：" + await page.textContent("#mc-install"));
      // 加完自动查了一遍前置：认出来的说明一下，认不出来的把要自己找的前置单独列出来
      const dep = (await page.textContent("#mc-dep-box")).replace(/\s+/g, " ");
      assert(dep.includes("清单里有 2 个本地文件") && dep.includes("1 个在 Modrinth 上认出来了"), "前置里应说明本地文件的情况：" + dep.slice(0, 120));
      const manual = (await page.textContent("#mc-dep-manual")).replace(/\s+/g, " ");
      assert(manual.includes("有 1 个前置要自己去找") && manual.includes("geckolib") && manual.includes("没法替你下载"), "要自己去找的前置：" + manual);
      assert((await page.$("#mc-dep-ok")) === null, "有要自己找的前置时不该说「不缺前置」");
      // 再加一个 Modrinth 项目：两样列在同一张清单里
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      assert((await page.$$eval("#mc-list .mc-item", els => els.length)) === 3 && (await page.textContent("#mc-count")).includes("共 3 项") &&
        (await page.textContent("#mc-install")).includes("全部安装（3）"), "本地文件和 Modrinth 项目应列在同一张清单里");
      const bad = await axProblems();
      assert(!bad.length, "清单里有同名或没名字的控件：" + bad.join("、"));
      // 「知道了」收起失败说明；同一个文件再加一次不重复
      await page.click("#mc-local-dismiss");
      assert((await page.$("#mc-local-err")) === null, "「知道了」应收起失败说明");
      await page.setInputFiles("#mc-file", [jarFile("needs-geckolib-1.0.jar")]);
      await page.waitForFunction(() => (document.querySelector("#mc-local-ok") || {}).textContent?.includes("已经在清单里"), null, { timeout: 8000 }); await settle(300);
      assert((await page.textContent("#mc-local-ok")).includes("已经在清单里") && (await page.$$eval("#mc-list .mc-local", els => els.length)) === 2, "同一个文件再加不该重复");
      // 移除一个本地文件：前置跟着重查（要自己找的那个还在，认出来的没了）
      await page.click('button[aria-label="移除本地文件 sodium-fabric-0.5.8.jar"]'); await settle(900);
      assert((await page.$$eval("#mc-list .mc-local", els => els.length)) === 1 && (await page.textContent("#mc-count")).includes("共 2 项"), "移除本地文件以后的清单不对");
      assert(!(await page.textContent("#mc-dep-box")).includes("认出来了") && (await page.$("#mc-dep-manual")) !== null, "移除以后前置应重查");
      // 把文件拖进来也行，拖偏到左边的列表上也算数（不接的话浏览器会把这个 jar 当网页打开）：清单那一栏高亮，放下就加进清单，页面不跳走
      const dt = await page.evaluateHandle(() => { const d = new DataTransfer(); d.items.add(new File(["PKdropped"], "dropped-1.0.jar")); return d; });
      await page.dispatchEvent("#mc-hits", "dragenter", { dataTransfer: dt });
      assert(await page.$eval("#mc-side", el => el.classList.contains("mc-drop")), "拖着文件经过时清单这一栏应高亮");
      await page.dispatchEvent("#mc-hits", "dragover", { dataTransfer: dt });
      await page.dispatchEvent("#mc-hits", "drop", { dataTransfer: dt });
      await page.waitForFunction(() => document.querySelector("#mc-list").textContent.includes("dropped-1.0.jar"), null, { timeout: 5000 });
      assert(!(await page.$eval("#mc-side", el => el.classList.contains("mc-drop"))) && decodeURIComponent(page.url()).includes(PACK + "/cart"), "放下以后高亮应收起、页面不该跳走");
      await settle(600); // 等这次添加之后自动跑的那遍前置检查回来
      // 清空：两样都清
      await page.click("#mc-clear"); await settle(700);
      assert((await page.$$eval("#mc-list .mc-item", els => els.length)) === 0 && (await page.$eval("#mc-actions", el => el.hidden)) && (await page.textContent("#mc-dep-box")) === "", "清空后清单、按钮、前置没复位");
      const mc = await cartMock();
      assert(mc.localInsts[PACK].length === 0 && mc.insts[PACK].length === 0, "清空应把本地文件一起清掉：" + JSON.stringify(mc.localInsts));
    } finally { await cartState({ modCart: { reset: true } }); }
  });

  await run("向导「选模组」这一步只从电脑添加了本地文件、没挑 Modrinth 项目：这一步放宽成两栏；建好服务器以后照样会装，安装请求带着页面上列着的文件", async () => {
    try {
      await cartState({ modCart: { reset: true }, taskStage: 4 });
      await gotoWizMods("fabric"); await settle(500);
      // 这一步用数据面的宽度：吸底栏不再套阅读宽度，左边逛、右边清单并排
      const lay = await page.evaluate(() => { const b = $(".mc-browse").getBoundingClientRect(), s = $(".mc-side").getBoundingClientRect(), f = $(".wizard-foot");
        return { side: s.left >= b.right, footRead: f.classList.contains("w-read"), footW: f.getBoundingClientRect().width, wrapW: $(".mc-wrap").getBoundingClientRect().width }; });
      assert(lay.side && !lay.footRead && Math.abs(lay.footW - lay.wrapW) < 2, "向导这一步应放宽成两栏、吸底栏同宽：" + JSON.stringify(lay));
      assert((await page.$("#mc-install")) === null, "向导里没有「全部安装」（建好服务器后自动装）");
      // 文件还在传的那几秒先按住「下一步」：不然后加进去的那个不在这一步看到的清单里，建好服务器以后整张清单都装不上。传完立刻放开
      let letGo;
      const held = new Promise(r => letGo = r);
      await page.route("**/api/modcart/local", async route => { await held; await route.continue(); });
      await page.setInputFiles("#mc-file", [jarFile("my-own-mod-1.0.jar")]);
      await page.waitForFunction(() => document.querySelector("#wnext").disabled, null, { timeout: 5000 });
      assert((await page.textContent("#wnext")).includes("正在添加文件") && (await page.textContent("#mc-local-state")).includes("正在添加 1 / 1") &&
        (await page.$eval("#mc-local-pick", b => b.disabled)), "上传的时候应按住「下一步」并写明正在添加第几个：" + await page.textContent("#wnext"));
      assert(!(await page.$eval("#wback", b => b.disabled)), "「上一步」不用按住（回去再过来，清单会重新列一遍）");
      letGo();
      await page.waitForSelector("#mc-list .mc-local", { timeout: 5000 }); await settle(500);
      await page.unroute("**/api/modcart/local");
      assert((await page.textContent("#mc-count")).includes("共 1 项") && (await page.textContent("#mc-list")).includes("本地文件 · my-own-mod-1.0.jar"), "草稿清单里应有这个本地文件");
      assert(!(await page.$eval("#wnext", b => b.disabled)) && (await page.textContent("#wnext")).includes("下一步"), "加完以后「下一步」应放开");
      await page.click("#wnext"); await settle();
      await page.fill("#f-name", "只带本地文件的服");
      await page.click(".eula-box .switch input");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await cartMock()).lastInstall;
      assert(li && li.inst === "只带本地文件的服" && li.includeDraft === true && Array.isArray(li.expect) && li.expect.length === 0 &&
        Array.isArray(li.expectLocal) && li.expectLocal.length === 1 && /^[0-9a-f]{40}$/.test(li.expectLocal[0]) && li.copied === 1,
        "只有本地文件也应发安装，并带上页面上列着的那个文件：" + JSON.stringify(li));
    } finally { await page.unroute("**/api/modcart/local").catch(() => {}); await cartState({ modCart: { reset: true }, taskStage: 2, instReset: true }); }
  });

  await run("待装清单里的本地文件在打开之后被别处动过：点安装会被拦下并重新列出现在的清单；向导「选模组」那一步上没列出来过的本地文件不会跟着装进新服务器", async () => {
    /* 别的窗口往某辆车里加一个本地文件 */
    const sneak = (instName, name) => page.evaluate(async ([i, n]) => {
      const fd = new FormData();
      fd.append("file", new Blob(["PK" + n]), n);
      if (i) fd.append("inst", i);
      await fetch("/api/modcart/local", { method: "POST", body: fd });
    }, [instName, name]);
    try {
      await cartState({ modCart: { reset: true }, taskStage: 4 });
      await page.goto(url(packCart)); await settle(700);
      await page.setInputFiles("#mc-file", [jarFile("mine-1.0.jar")]);
      await page.waitForSelector("#mc-list .mc-local", { timeout: 5000 }); await settle(400);
      await sneak(PACK, "not-seen-2.0.jar"); // 页面没动的时候，别处又加了一个
      await page.click("#mc-install"); await settle(900);
      let toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("无法开始安装：清单里的本地文件和你刚才看到的不一样了")), "本地文件变了应拦下并说明：" + JSON.stringify(toasts));
      assert(!page.url().includes("#/task/") && (await cartMock()).lastInstall === null, "被拦下时不该发出安装、不该跳到任务页");
      assert((await page.$$eval("#mc-list .mc-local", els => els.length)) === 2 && (await page.textContent("#mc-list")).includes("not-seen-2.0.jar"), "应重新列出现在的清单");
      await page.click("#mc-install"); // 看过现在的清单再点：照常装，带着页面上列着的两个文件
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await cartMock()).lastInstall;
      assert(li && li.expectLocal.length === 2 && li.copied === 2, "安装请求应带上页面上列着的两个文件：" + JSON.stringify(li));

      // 向导：在「选模组」这一步挑了一个 Modrinth 项目，走到下一步以后草稿清单里多出一个本地文件（别的窗口加的、没人看过）
      await cartState({ modCart: { reset: true }, taskStage: 4, instReset: true });
      await gotoWizMods("fabric"); await settle(500);
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      await page.click("#wnext"); await settle();
      await sneak("", "leftover-from-last-time.jar");
      await page.fill("#f-name", "不该带上陌生文件的服");
      await page.click(".eula-box .switch input");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 }); // 装不成：回到建服任务页
      toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("无法安装清单里的模组：清单里的本地文件和你刚才看到的不一样了")), "应说明为什么没装：" + JSON.stringify(toasts));
      const mc = await cartMock();
      assert(mc.lastInstall === null && mc.localDraft.length === 1 && mc.draft.length === 1, "没人看过的本地文件不该被装上，清单原样留着：" + JSON.stringify(mc.lastInstall));
    } finally { await cartState({ modCart: { reset: true }, taskStage: 2, instReset: true }); await page.goto(url("#/instances")); await settle(); }
  });

  await run("待装清单的两栏版面：宽窗口左边逛、右边清单（360px，往下滚时吸顶）；程序窗口最小的 1024×576 放不下就叠成一栏；手机宽度也没有横向滚动", async () => {
    const geo = () => page.evaluate(() => {
      const b = $(".mc-browse").getBoundingClientRect(), s = $(".mc-side").getBoundingClientRect(), m = $("#main");
      return { browseRight: b.right, browseBottom: b.bottom, browseLeft: b.left, sideLeft: s.left, sideTop: s.top, sideW: Math.round(s.width),
        pos: getComputedStyle($(".mc-side")).position, mainTop: m.getBoundingClientRect().top, overflowX: m.scrollWidth - m.clientWidth,
        pageOverflowX: document.documentElement.scrollWidth - window.innerWidth };
    });
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(packCart)); await settle(700);
      let g = await geo();
      assert(g.sideLeft >= g.browseRight && g.sideW === 360 && g.pos === "sticky", "1280 宽应是两栏、右栏 360px：" + JSON.stringify(g));
      assert(g.overflowX <= 0 && g.pageOverflowX <= 0, "1280 宽不该有横向滚动：" + JSON.stringify(g));
      // 往下滚：左边的列表滚走了，右栏还贴在内容区顶上
      await page.evaluate(() => { $("#main").scrollTop = 600; }); await settle(150);
      g = await geo();
      assert(g.sideTop >= g.mainTop && g.sideTop <= g.mainTop + 40, "往下滚的时候右栏应吸顶：" + JSON.stringify(g));
      const visible = await page.evaluate(() => { const r = $("#mc-local-pick").getBoundingClientRect(); return r.top >= 0 && r.bottom <= window.innerHeight; });
      assert(visible, "滚下去以后清单这一栏应还看得到");
      // 程序窗口最小 1024×576：内容区放不下两栏，叠成一栏（浏览 → 清单 → 检查前置）
      await page.setViewportSize({ width: 1024, height: 576 }); await settle(300);
      await page.evaluate(() => { $("#main").scrollTop = 0; }); await settle(100);
      g = await geo();
      assert(g.sideTop >= g.browseBottom - 1 && Math.abs(g.sideLeft - g.browseLeft) < 2 && g.pos !== "sticky", "1024 宽应叠成一栏：" + JSON.stringify(g));
      assert(g.overflowX <= 0 && g.pageOverflowX <= 0, "1024 宽不该有横向滚动：" + JSON.stringify(g));
      const order = await page.$$eval(".mc-wrap h3", els => els.map(e => e.textContent.trim().replace(/\s+.*$/, "")));
      assert(order.join() === "热门模组,待装清单,检查前置", "一栏时的顺序应是浏览、清单、检查前置：" + order.join());
      for (const w of [900, 390]) {
        await page.setViewportSize({ width: w, height: 800 }); await settle(300);
        g = await geo();
        assert(g.overflowX <= 0 && g.pageOverflowX <= 0, w + " 宽不该有横向滚动：" + JSON.stringify(g));
      }
    } finally { await page.setViewportSize({ width: 1280, height: 850 }); await cartState({ modCart: { reset: true } }); }
  });

  await run("报错的正文：结论在前，看得懂的后端原句整句接上（自带冒号时用句号接）；带技术痕迹、超过 60 字、前端兜底词只写结论；连不上时说连不上；「详情」和复制键里是完整原文", async () => {
    await page.goto(url("#/instances")); await settle();
    const got = await page.evaluate(() => {
      const E = (m, x = {}) => Object.assign(new Error(m), x);
      return [
        ["无法删除服务器", E("请先停止服务器再删除", { status: 400 })],
        ["发送失败", E("AI 未启用：先在「设置 → AI」里选择服务商，填好 Key 和模型", { status: 400 })],
        ["保存失败", E("需要登录", { status: 401 })],
        ["无法启动服务器", E("启动失败: fork/exec D:\\MineDeck\\runtimes\\java17\\bin\\java.exe: The system cannot find the file specified.", { status: 400 })],
        ["无法读取会话记录", E("拒绝访问：unexpected EOF")],
        ["无法检查更新", E("服务异常 (HTTP 502)")],
        ["无法导入整合包", E("导入失败")],
        ["保存失败", E("未知错误")],
        ["无法启动服务器", new TypeError("Failed to fetch")],
        ["无法启动服务器", new TypeError("Cannot read properties of undefined (reading 'pending')")],
        ["无法启动隧道", E("这".repeat(61))],
        ["无法启动隧道", E("这".repeat(60))],
        ["无法获取模型列表", E("Key 不对或已失效（HTTP 401，authentication_error）：invalid x-api-key", { status: 502, hint: "Key 不对或已失效" })],
        ["无法获取模型列表", E("服务商返回错误（HTTP 418）：teapot", { status: 502, hint: "" })],
        // Codex 复审构造的：给人看的话别误杀，路径和英文报错别漏放
        ["无法创建服务器", E("服务器名称不能包含「\\」")],
        ["无法创建服务器", E("仅支持 Minecraft 1.20/1.21")],
        ["保存失败", E("接口地址必须以「https://」开头")],
        ["无法读取配置", E("无法读取：/usr/local/config")],
        ["无法读取配置", E("无法读取「配置/服务器.json」")],
        ["无法读取配置", E("读不了 D:\\MineDeck\\世界\\level.dat")],
        ["连接失败", E("连接失败：Bad Gateway")],
        ["连接失败", E("连接失败：ACCESS DENIED")],
        ["无法开启局域网访问", E("请先设置访问密码")],
        ["无法安装 Simple Voice Chat", E("Simple Voice Chat 要求 Fabric API")],
        ["无法安装", E("支持原版/Fabric/Forge 三种核心")],
        ["无法安装", E("可以启用/禁用/删除这个模组")],
        ["无法添加模组", E("「Forbidden.jar」是空文件（可能没下载完整），换一份再添加")],
        ["无法检查 Java", E("[1.20.1]\\java.exe 不是已知的 Java 17（见 java.list）")],
        ["无法连接", E("无法连接：https://镜像.中国")],
      ].map(([what, e]) => errSummary(what, e).text);
    });
    const want = [
      "无法删除服务器：请先停止服务器再删除",
      "发送失败。AI 未启用：先在「设置 → AI」里选择服务商，填好 Key 和模型",
      "保存失败：需要登录",
      "无法启动服务器",
      "无法读取会话记录",
      "无法检查更新",
      "无法导入整合包",
      "保存失败",
      "无法启动服务器：连不上 MineDeck",
      "无法启动服务器",
      "无法启动隧道",
      "无法启动隧道：" + "这".repeat(60),
      "无法获取模型列表：Key 不对或已失效",
      "无法获取模型列表",
      "无法创建服务器：服务器名称不能包含「\\」",
      "无法创建服务器：仅支持 Minecraft 1.20/1.21",
      "保存失败：接口地址必须以「https://」开头",
      "无法读取配置",
      "无法读取配置",
      "无法读取配置",
      "连接失败",
      "连接失败",
      "无法开启局域网访问：请先设置访问密码",
      "无法安装 Simple Voice Chat：Simple Voice Chat 要求 Fabric API",
      "无法安装：支持原版/Fabric/Forge 三种核心",
      "无法安装：可以启用/禁用/删除这个模组",
      "无法添加模组：「Forbidden.jar」是空文件（可能没下载完整），换一份再添加",
      "无法检查 Java",
      "无法连接",
    ];
    for (let i = 0; i < want.length; i++) assert(got[i] === want[i], `第 ${i + 1} 条：${got[i]}（应为 ${want[i]}）`);
    // 提示：正文只写结论，「详情」里是原文
    const raw = "启动失败: fork/exec D:\\MineDeck\\runtimes\\java17\\bin\\java.exe: The system cannot find the file specified.";
    await page.evaluate(x => toastErr("无法启动服务器", new Error(x)), raw);
    assert(await toastHas("无法启动服务器"), "应弹出报错提示");
    assert(await toastDetail("无法启动服务器") === raw, "「详情」里应是完整原文");
    // 报错框、行内报错：复制键带着完整原文（回车换行也原样），名字是「复制报错信息」，同页有几处时带上是哪一处
    const crlf = "第一行\r\n第二行: open C:\\x: Access is denied.";
    const b = await page.evaluate(x => {
      const d = document.createElement("div");
      d.innerHTML = errBox("无法获取版本清单", new Error(x), { hint: "可在「设置 → 下载与网络」换一个下载源后重试。" }) + errInline("无法刷新隧道列表", x, { label: "隧道" });
      const box = d.querySelector(".err-box"), btn = box.querySelector("[data-err-raw]"), inl = d.querySelector(".err-inline [data-err-raw]");
      return { text: box.firstChild.textContent.trim(), hint: box.children[0].textContent, raw: btn.getAttribute("data-err-raw"), name: btn.textContent.trim(),
        inlineText: d.querySelector(".err-inline").textContent.trim(), inlineRaw: inl.getAttribute("data-err-raw"), inlineName: inl.getAttribute("aria-label"), inlineTitle: inl.title };
    }, crlf);
    assert(b.text === "无法获取版本清单" && b.hint.startsWith("可在") && b.raw === crlf && b.name === "复制报错信息", "报错框：" + JSON.stringify(b));
    assert(b.inlineText === "无法刷新隧道列表" && b.inlineRaw === crlf && b.inlineName === "复制报错信息 隧道" && b.inlineTitle === "复制报错信息 隧道", "行内报错：" + JSON.stringify(b));
  });

  // ==================== 外部链接交给默认浏览器（3.1.1；测试报告「外部链接没有用默认浏览器打开」）====================
  // host=true 模拟原生窗口：宿主函数 hostOpenURL 只记下交过来的地址，__openFail 不为空时像宿主拒绝那样让 Promise 落空。
  // 别的网站的请求一律拦下、给一个空页面：测试不真的上网，万一没拦住弹了窗口，打开的也只是这个空页面
  const extCtx = async host => {
    const c = await browser.newContext({ viewport: { width: 1280, height: 850 }, locale: "zh-CN" });
    const origin = new URL(ORIGIN).origin;
    await c.route(u => u.origin !== origin, r => r.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: "<title>别的网站</title>" }));
    await c.addInitScript(h => {
      localStorage.setItem("amh_token", "mock");
      localStorage.setItem("amh_theme", "dark");
      if (!h) return;
      for (const fn of ["hostWinClose", "hostWinMin", "hostWinMaxToggle", "hostWinDrag"]) window[fn] = () => {};
      window.hostWinIsMax = async () => false;
      window.__opened = []; window.__openFail = "";
      window.hostOpenURL = async u => { window.__opened.push(u); if (window.__openFail) throw window.__openFail; };
    }, host);
    return c;
  };
  const EXT_LINK = 'a[href="https://www.openfrp.net"]'; // 「设置 → 更新与关于」里的 openfrp.net，带 target="_blank"

  await run("外部链接：原生窗口里左键、Ctrl / Shift 点、中键、回车、window.open 都交给宿主用默认浏览器打开，不弹 WebView2 的窗口、主窗口不跳走；右键不算；站内链接照旧开窗口；宿主拒绝或打不开时弹报错提示", async () => {
    const c = await extCtx(true);
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/settings/about");
      await p.waitForSelector(EXT_LINK);
      const opened = () => p.evaluate(() => window.__opened.splice(0));
      await p.click(EXT_LINK);
      await p.click(EXT_LINK, { modifiers: ["Control"] });
      await p.click(EXT_LINK, { modifiers: ["Shift"] });
      await p.click(EXT_LINK, { button: "middle" });
      await p.focus(EXT_LINK); await p.keyboard.press("Enter");
      await p.click(EXT_LINK, { button: "right" });
      await p.waitForTimeout(1500); // 不拦的话 Ctrl 点开的窗口要过半秒多才出来，等够了再数
      let got = await opened();
      // 交给宿主的是浏览器整理过的地址（补上了末尾的斜杠）
      assert(got.length === 5 && got.every(u => u === "https://www.openfrp.net/"), "交给宿主的地址不对：" + JSON.stringify(got));
      assert(c.pages().length === 1, "弹出了别的窗口：" + c.pages().map(x => x.url()).join(", "));
      assert(p.url().endsWith("#/settings/about"), "主窗口跳走了：" + p.url());
      // 脚本里的 window.open：外部地址交给宿主（中文、空格转成 %XX），返回 null；_self 也不让主窗口跳过去
      const nulls = await p.evaluate(() => [window.open("https://example.com/a b?q=我的世界", "_blank"), window.open("http://example.com/x", "_self")].map(w => w === null));
      got = await opened();
      assert(nulls.every(Boolean) && JSON.stringify(got) === JSON.stringify(["https://example.com/a%20b?q=%E6%88%91%E7%9A%84%E4%B8%96%E7%95%8C", "http://example.com/x"]),
        "window.open：" + JSON.stringify({ nulls, got }));
      await p.waitForTimeout(200);
      assert(c.pages().length === 1 && p.url().endsWith("#/settings/about"), "window.open 开了窗口或让主窗口跳走了：" + p.url());
      // SVG 里的链接（href 是 SVGAnimatedString，不是字符串）也交给宿主，主窗口不跳走
      await p.evaluate(() => {
        const ns = "http://www.w3.org/2000/svg", mk = n => document.createElementNS(ns, n);
        const svg = mk("svg"), a = mk("a"), r = mk("rect");
        svg.id = "svg-link-test"; svg.setAttribute("width", "40"); svg.setAttribute("height", "40");
        svg.style.cssText = "position:fixed;left:200px;top:200px;z-index:99999";
        a.setAttribute("href", "https://example.com/svg?x=1");
        r.setAttribute("width", "40"); r.setAttribute("height", "40");
        a.appendChild(r); svg.appendChild(a); document.body.appendChild(svg);
      });
      await p.click("#svg-link-test rect");
      await p.waitForTimeout(300);
      got = await opened();
      assert(JSON.stringify(got) === JSON.stringify(["https://example.com/svg?x=1"]) && p.url().endsWith("#/settings/about"),
        "SVG 链接：" + JSON.stringify({ got, url: p.url() }));
      await p.evaluate(() => document.getElementById("svg-link-test").remove());
      // 站内地址（字体许可原文）照旧开窗口，不交给宿主
      const [pop] = await Promise.all([c.waitForEvent("page"), p.click('a[href="fonts/OFL-Geist.txt"]')]);
      await pop.close();
      const [pop2] = await Promise.all([c.waitForEvent("page"), p.evaluate(() => { window.open("fonts/OFL-NotoSansSC.txt"); })]);
      await pop2.close();
      assert((await opened()).length === 0, "站内地址不该交给宿主");
      // 宿主拒绝（Promise 落空，原话是一句中文）：结论接上原话
      await p.evaluate(() => { window.__openFail = "只能打开 http 或 https 开头的网址"; });
      await p.click(EXT_LINK);
      const msgs = () => p.$$eval(".toast.error .t-msg", ts => ts.map(t => t.textContent));
      await p.waitForFunction(() => document.querySelectorAll(".toast.error").length >= 1, null, { timeout: 2000 });
      assert((await msgs()).includes("没能用浏览器打开链接：只能打开 http 或 https 开头的网址"), "宿主拒绝时的提示：" + JSON.stringify(await msgs()));
      // 宿主在后台打不开（回调 __mdOpenURLFailed，原话带着网址和系统的英文原文）：只写结论，「详情」里是原文
      const raw = "用默认浏览器打开 https://www.openfrp.net/ 失败：No application is associated with the specified file for this operation.";
      await p.evaluate(x => window.__mdOpenURLFailed(x), raw);
      await p.waitForFunction(() => document.querySelectorAll(".toast.error").length >= 2, null, { timeout: 2000 });
      assert((await msgs()).includes("没能用浏览器打开链接"), "打不开时的提示：" + JSON.stringify(await msgs()));
      await p.locator(".toast.error").last().locator('button[aria-label="查看报错详情"]').click();
      assert((await p.textContent(".err-detail")) === raw, "「详情」里应是完整原文");
      await p.keyboard.press("Escape");
    } finally { await c.close(); }
  });

  await run("外部链接：局域网浏览器里（没有 hostOpenURL）照浏览器自己的规矩开，window.open 不动", async () => {
    const c = await extCtx(false);
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/settings/about");
      await p.waitForSelector(EXT_LINK);
      for (const opts of [{}, { button: "middle" }]) {
        const [pop] = await Promise.all([c.waitForEvent("page"), p.click(EXT_LINK, opts)]);
        await pop.waitForLoadState();
        assert(pop.url() === "https://www.openfrp.net/", "新窗口的地址：" + pop.url());
        await pop.close();
      }
      const st = await p.evaluate(() => ({ native: /\[native code\]/.test(Function.prototype.toString.call(window.open)), hook: typeof window.__mdOpenURLFailed }));
      assert(st.native && st.hook === "undefined", "局域网浏览器里不该改 window.open：" + JSON.stringify(st));
    } finally { await c.close(); }
  });

  // 用 CDP 的 Input.dispatchDragEvent 走浏览器真正的拖放流程。无头 Chromium 松手后什么都不开，看不出拦没拦住，
  // 所以在 window 上（排在页面自己的处理后面）记下 dragover 有没有被页面接下、dropEffect 是什么、drop 有没有发出来：
  // dropEffect 是 none 时浏览器不发 drop，WebView2 也就不会另开小窗（没人接的拖放，它是另开一个窗口打开的，主窗口不动；
  // 3.1.1 发版前拿正式版 3.1.0 在真窗口里对照过）
  await run("拖进窗口：原生窗口里没地方接的链接和文件，页面接下来、标成不能放（WebView2 就不会另开小窗把它打开）；输入框照旧能拖进文字；局域网浏览器里不管", async () => {
    const LINK = [{ mimeType: "text/uri-list", data: "https://example.com/dropped" }, { mimeType: "text/plain", data: "https://example.com/dropped" }];
    const FILE = { items: [], files: [require("path").join(__dirname, "README.md")] };
    const open = async host => {
      const c = await extCtx(host);
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      await p.goto(ORIGIN + "#/settings/notify");
      await p.waitForSelector("#wh-url");
      const cdp = await c.newCDPSession(p);
      const drag = async (sel, data) => {
        const b = await p.locator(sel).first().boundingBox();
        const at = { x: Math.round(b.x + b.width / 2), y: Math.round(b.y + b.height / 2) };
        await p.evaluate(() => {
          if (!window.__dragOn) { // 换过页面（整页重新加载）以后再挂一次
            window.__dragOn = true;
            for (const t of ["dragover", "drop"]) window.addEventListener(t, e => window.__drag.push(`${t}:${e.defaultPrevented}:${e.dataTransfer.dropEffect}`));
          }
          window.__drag = [];
        });
        for (const type of ["dragEnter", "dragOver", "drop"]) await cdp.send("Input.dispatchDragEvent", { type, ...at, data: { dragOperationsMask: 1 | 2 | 16, ...data } });
        await p.waitForTimeout(300);
        return [...new Set(await p.evaluate(() => window.__drag))];
      };
      return { c, p, drag };
    };
    const w = await open(true);
    try {
      for (const [what, data] of [["链接", { items: LINK }], ["文件", FILE]]) {
        const got = await w.drag(".set-sec-head h2", data);
        assert(JSON.stringify(got) === JSON.stringify(["dragover:true:none"]) && w.p.url().endsWith("#/settings/notify"),
          `拖进来的${what}应被页面接下、标成不能放，不发 drop：` + JSON.stringify({ got, url: w.p.url() }));
      }
      await w.p.fill("#wh-url", "");
      const got = await w.drag("#wh-url", { items: [{ mimeType: "text/plain", data: "https://example.com/hook" }] });
      assert((await w.p.inputValue("#wh-url")) === "https://example.com/hook" && !got.some(x => x.startsWith("dragover:true")),
        "输入框应能拖进文字：" + JSON.stringify({ v: await w.p.inputValue("#wh-url"), got }));
      const intoInput = await w.drag("#wh-url", FILE); // 文件拖到输入框上也不让打开
      assert(JSON.stringify(intoInput) === JSON.stringify(["dragover:true:none"]), "文件拖到输入框上：" + JSON.stringify(intoInput));
      // 待装清单接文件的地方照旧接得住：那边自己接下（copy），这里不改成不能放，drop 照常发给它
      await w.p.goto(url(packCart));
      await w.p.waitForSelector("#mc-local-pick");
      const cart = await w.drag("#mc-side", FILE);
      assert(cart.includes("dragover:true:copy") && cart.some(x => x.startsWith("drop:true")) && decodeURIComponent(w.p.url()).includes(PACK + "/cart"),
        "待装清单应照旧接住拖进来的文件：" + JSON.stringify({ cart, url: w.p.url() }));
    } finally { await w.c.close(); }
    const lan = await open(false);
    try {
      const got = await lan.drag(".set-sec-head h2", { items: LINK });
      assert(got.every(x => !x.startsWith("dragover:true")), "局域网浏览器里不该拦拖放：" + JSON.stringify(got));
    } finally { await lan.c.close(); }
  });

  await run("全程无页面报错", async () => { assert(errors.length === 0, errors.slice(0, 3).join(" | ")); });

  await browser.close();
  await mock.close();
  for (const r of results) console.log(r.join("  "));
  const fails = results.filter(r => r[0] === "FAIL").length;
  console.log(`\n${results.length - fails}/${results.length} passed`);
  process.exit(fails ? 1 : 0);
})().catch(e => { console.error(e); process.exit(2); });
