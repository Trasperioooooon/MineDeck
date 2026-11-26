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
  const isPageErrCheck = name === "全程无页面报错";
  if (ONLY && !ONLY.test(name) && !(isPageErrCheck && results.length > 0)) return;
  let rec;
  try { await fn(); rec = ["PASS", name]; }
  catch (e) { rec = ["FAIL", name, e.message.split("\n")[0]]; }
  results.push(rec);
  console.log(rec.join("  "));
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
    await page.goto(url("#/create"));
    await page.waitForFunction(() => !!document.querySelector("#mode-new") && !!(document.getElementById("app-ver") && document.getElementById("app-ver").textContent));
    errors.length = 0;
    // 从新建页连切 6 次到隧道页：每次切走都发生在上一视图的首个请求返回之前（mock 很快，故用 evaluate 同步连切）
    await page.evaluate(async () => {
      const hs = ["#/instances", "#/settings/java", "#/inst/" + encodeURIComponent("生存服 · 朋友") + "/console", "#/instances", "#/task/task-1", "#/tunnels"];
      for (const h of hs) { location.hash = h; await new Promise(r => setTimeout(r, 5)); }
    });
    // 等隧道页自己的首次渲染（含首轮 /api/instances、/api/tunnels 拉取和 startPoll 挂好）落定后，再开始 6 秒静默计时
    await page.waitForFunction(() => {
      const add = document.querySelector("#tn-add");
      return !!document.querySelector("#tun-list [data-tun]") && !!(add && typeof add.onclick === "function");
    });
    await settle(150);
    const t0 = Date.now();
    await page.waitForTimeout(6000);
    const t1 = Date.now();
    const instReqs = reqs.filter(r => r.t >= t0 && r.u === "/api/instances").length;
    const taskReqs = reqs.filter(r => r.t >= t0 && r.u.startsWith("/api/tasks/")).length;
    const tunReqs = reqs.filter(r => r.t >= t0 && r.t <= t1 + 200 && r.u === "/api/tunnels").length;
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

  // beta.3 R05：取消键叫「取消」，不再叫「保留」（会被读成「保留文件夹」）
  await run("删除确认框默认聚焦「取消」，回车不会删除", async () => {
    await page.goto(url("#/instances")); await settle();
    await page.click('.card[data-name="机械动力整合包"] [data-act="more"]');
    await page.click(".dropmenu button.danger");
    await settle(200);
    const focused = await page.evaluate(() => document.activeElement && document.activeElement.textContent.trim());
    assert(focused === "取消", "默认焦点在：" + focused);
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

  // 保存条：保存请求还没回来时又改了表单（2026-11-16 Codex 复审 beta.3）。以前请求回来后按「那时的表单」重取基线，
  // 后台只收到改之前的那份，界面却把途中改的也标成已保存，离开时也不拦。现在基线只认点「保存」那一刻发出去的那份。
  // gatePut：拦住 pattern 的 PUT，等测试放行（GET 照常过），fn(requested, release)
  const gatePut = async (pattern, fn) => {
    let release, seen;
    const gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    // 用例断言失败时 finally 里会先放行再撤掉拦截：这时请求可能已经被放过去了，continue 报「已处理」不当回事，免得整个测试进程崩掉
    const hold = async route => {
      if (route.request().method() !== "PUT") return route.continue().catch(() => {});
      seen(); await gate; await route.continue().catch(() => {});
    };
    await page.route(pattern, hold);
    try { await fn(requested, release); } finally { release(); await page.unroute(pattern, hold); }
  };
  const lastPut = (since, suffix) => { const r = reqs.slice(since).filter(x => x.m === "PUT" && x.u.endsWith(suffix)).pop(); return r ? JSON.parse(r.b || "{}") : null; };

  await run("保存条：保存途中又加的定时任务仍标「未保存」，保存条算一项、离开时照样拦（Codex 复审 beta.3）", async () => {
    await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
    await page.fill("#sc-at", "05:00"); await page.click("#sc-add");
    const since = reqs.length;
    let offMid = false; // 保存途中「保存」键是不是一直置灰（放到最后断言：先看基线对不对）
    await gatePut("**/policies", async (requested, release) => {
      await page.click('[data-sb="save"]'); await requested;
      await page.fill("#sc-at", "06:30"); await page.click("#sc-add");
      offMid = await page.$eval('[data-sb="save"]', b => b.disabled);
      release();
      await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("定时任务已保存")));
    });
    await settle(200);
    // 只发这一页加了的（按条加，不再整张表发过去，见下面两个窗口的那条用例）
    const body = lastPut(since, "/policies") || {}, sent = body.addSchedules || [];
    assert(sent.some(s => s.at === "05:00") && !sent.some(s => s.at === "06:30") && !("schedules" in body), "发出去的应只有点保存时加的那条：" + JSON.stringify(body));
    const v = await page.evaluate(() => ({ bar: $(".sb-text").textContent, off: $('[data-sb="save"]').disabled,
      drafts: [...document.querySelectorAll("#sched-list .java-row.draft")].map(r => r.textContent.replace(/\s+/g, " ").trim()),
      saved: [...document.querySelectorAll("#sched-list .java-row:not(.draft)")].map(r => r.textContent.replace(/\s+/g, " ").trim()) }));
    assert(v.bar === "1 项修改未保存" && !v.off, "途中加的那条应算一项未保存：" + JSON.stringify(v));
    assert(v.drafts.length === 1 && v.drafts[0].includes("06:30") && v.drafts[0].includes("未保存") && v.saved.some(x => x.includes("05:00")),
      "05:00 已保存、06:30 仍是草稿：" + JSON.stringify(v));
    await page.click('#sidebar a[data-nav="settings"]'); await settle(200);
    assert((await page.$(".modal")) !== null, "还有没保存的定时任务，离开应拦下");
    await page.click('[data-x="ok"]'); await settle(300);
    assert(offMid, "保存途中「保存」键应一直置灰，不叠着再存一次");
  });

  await run("保存条：常用设置保存途中又改的一项仍算未保存，后台只收到点保存时的值（Codex 复审 beta.3）", async () => {
    await page.goto(url(`#/inst/${inst}/common`)); await settle();
    await page.click('.prop-item [data-k="hardcore"]');
    const since = reqs.length;
    let offMid = false;
    await gatePut("**/properties", async (requested, release) => {
      await page.click('[data-sb="save"]'); await requested;
      await page.fill('[data-k="max-players"]', "37");
      offMid = await page.$eval('[data-sb="save"]', b => b.disabled);
      release();
      await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已保存 server.properties")));
    });
    await settle(200);
    const pairs = (lastPut(since, "/properties") || {}).pairs || [];
    const val = k => (pairs.find(p => p.key === k) || {}).value;
    assert(val("hardcore") === "true" && val("max-players") !== "37", "发出去的应是点保存时的值：" + JSON.stringify({ hardcore: val("hardcore"), max: val("max-players") }));
    const v = await page.evaluate(() => ({ bar: $(".sb-text").textContent, off: $('[data-sb="save"]').disabled, max: $('[data-k="max-players"]').value }));
    assert(v.bar === "1 项修改未保存" && !v.off && v.max === "37", "途中改的最大人数应仍算未保存：" + JSON.stringify(v));
    await page.click('[data-sb="discard"]'); await settle(300);
    assert(offMid, "保存途中「保存」键应一直置灰");
  });

  await run("保存条：JVM 参数保存途中又改了，回读的参数不盖掉新填的，新填的仍算未保存（Codex 复审 beta.3）", async () => {
    const FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml"];
    const mock = b => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), b);
    await mock({ legacy: { name: "机械动力整合包", extraJvm: ["-XX:+UseG1GC", ...FIX], forceFlags: FIX } }); // 后端保存时会补回缓解参数
    try {
      await page.goto(url(`#/inst/${encodeURIComponent("机械动力整合包")}/jvm`)); await settle();
      await page.fill("#jm-jvm", "-XX:+UseZGC");
      const since = reqs.length;
      let offMid = false;
      await gatePut("**/settings", async (requested, release) => {
        await page.click('[data-sb="save"]'); await requested;
        await page.fill("#jm-jvm", "-XX:+UseShenandoahGC");
        offMid = await page.$eval('[data-sb="save"]', b => b.disabled);
        release();
        await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已保存内存与 JVM 参数")));
      });
      await settle(200);
      const put = lastPut(since, "/settings") || {};
      assert(JSON.stringify(put.extraJvm) === JSON.stringify(["-XX:+UseZGC"]), "发出去的应是点保存时的参数：" + JSON.stringify(put));
      const v = await page.evaluate(() => ({ jvm: $("#jm-jvm").value, bar: $(".sb-text").textContent, off: $('[data-sb="save"]').disabled }));
      assert(v.jvm === "-XX:+UseShenandoahGC", "回读的参数盖掉了途中新填的：" + JSON.stringify(v.jvm));
      assert(v.bar === "1 项修改未保存" && !v.off, "途中新填的参数应仍算未保存：" + JSON.stringify(v));
      await page.click('[data-sb="discard"]'); await settle(300);
      assert(offMid, "保存途中「保存」键应一直置灰");
    } finally { await mock({ instReset: true }); }
  });

  await run("保存条：选的 Java 要先下载时，保存途中这一页的输入都锁着，保存完去任务页（Codex 复审 beta.3 第二、三轮）", async () => {
    const mock = b => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), b);
    await mock({ instReset: true });
    try {
      await page.goto(url(`#/inst/${encodeURIComponent("机械动力整合包")}/jvm`)); await settle(900);
      await page.selectOption("#jm-java", { index: await page.$eval("#jm-java", s => [...s.options].findIndex(x => x.textContent.startsWith("Java 25（"))) });
      await page.fill("#jm-jvm", "-XX:+UseZGC");
      const since = reqs.length;
      let mid = null;
      await gatePut("**/java", async (requested, release) => {
        await page.click('[data-sb="save"]'); await requested;
        mid = await page.evaluate(() => ({ jvm: $("#jm-jvm").disabled, java: $("#jm-java").disabled, mem: $("#jm-mem").disabled, aikar: $("#jm-aikar").disabled }));
        release();
        await page.waitForFunction(() => location.hash === "#/task/task-java");
      });
      assert(mid && mid.jvm && mid.java && mid.mem && mid.aikar, "保存途中输入应锁着：" + JSON.stringify(mid));
      assert((lastPut(since, "/settings") || {}).extraJvm?.includes("-XX:+UseZGC"), "参数应先存上");
    } finally { await mock({ instReset: true }); }
  });

  await run("保存条：不用下载的 Java 和普通保存不锁输入；保存失败以后锁着的输入放开（Codex 复审 beta.3 第三轮）", async () => {
    const mock = b => page.evaluate(b => fetch("/__mock/state", { method: "POST", body: JSON.stringify(b) }), b);
    await mock({ instReset: true });
    const fail = route => route.request().method() === "PUT" ? route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ ok: false, error: "换不了" }) }) : route.continue();
    try {
      await page.goto(url(`#/inst/${encodeURIComponent("机械动力整合包")}/jvm`)); await settle(900);
      await page.fill("#jm-jvm", "-XX:+UseZGC");
      let mid = null;
      await gatePut("**/settings", async (requested, release) => {
        await page.click('[data-sb="save"]'); await requested;
        mid = await page.$eval("#jm-jvm", e => e.disabled);
        release();
        await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已保存内存与 JVM 参数")));
      });
      assert(mid === false, "普通保存途中不该锁输入");
      await page.route("**/java", fail);
      try {
        await page.selectOption("#jm-java", { index: await page.$eval("#jm-java", s => [...s.options].findIndex(x => x.textContent.startsWith("Java 25（"))) });
        await page.click('[data-sb="save"]');
        await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("保存失败")));
        await settle(200);
        const v = await page.evaluate(() => ({ jvm: $("#jm-jvm").disabled, java: $("#jm-java").disabled, url: location.hash }));
        assert(!v.jvm && !v.java && v.url.endsWith("/settings/jvm"), "保存失败以后应放开输入、留在本页：" + JSON.stringify(v));
      } finally { await page.unroute("**/java", fail); }
    } finally { await mock({ instReset: true }); }
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

  await run("创建任务取消：可取消时显示按钮，发出取消后等清理；提交阶段不显示", async () => {
    let cancelled = false, committing = false, posts = 0;
    const pattern = u => u.pathname === "/api/tasks/task-cancellable" || u.pathname === "/api/tasks/task-cancellable/cancel";
    await page.route(pattern, async route => {
      const req = route.request();
      if (new URL(req.url()).pathname.endsWith("/cancel")) {
        posts++; cancelled = true;
        await route.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: "正在取消" }) });
      } else {
        await route.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: {
          id: "task-cancellable", title: "创建测试服", ended: false, canCancel: !cancelled && !committing,
          steps: [{ name: "复制地图", status: "running" }], log: [], done: 1, total: 10
        } }) });
      }
    });
    try {
      await page.goto(url("#/task/task-cancellable"));
      await page.locator("#t-cancel").waitFor({ state: "visible" });
      // 在取消按钮正显示时切换到提交阶段，确认按钮随状态更新消失
      committing = true;
      await page.waitForFunction(() => !document.querySelector("#t-cancel"));
      assert(await page.locator("#t-cancel").count() === 0, "开始提交以后不应提供取消按钮");
      // 回到可取消阶段，按钮恢复后再点击取消
      committing = false;
      await page.locator("#t-cancel").waitFor({ state: "visible" });
      await page.click("#t-cancel");
      await page.waitForFunction(() => !document.querySelector("#t-cancel"));
      assert(posts === 1, "应当只发一次取消请求");
      assert((await page.textContent("#t-foot")).includes("请勿关闭"), "清理期间任务应仍在进行");
    } finally { await page.unroute(pattern); }
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

  // beta.3 R10：原来的「资源」标签合进了「插件 / 模组」，它自己的那套搜索没有了；同一个搜索框的竞态由「切页竞态：待装清单的搜索」那条管，
  // 这里原来那条拿掉，在下面的列表里加上插件 / 模组页（「已安装」的慢响应）
  for (const [label, hash, endpoint, target] of [
    ["玩家", `#/inst/${inst}/players`, "**/players", `#/inst/${inst}/console`],
    ["备份", `#/inst/${inst}/backups`, "**/backups", `#/inst/${inst}/console`],
    ["定时任务", `#/inst/${inst}/tasks`, "**/policies", `#/inst/${inst}/console`],
    ["常用设置", `#/inst/${inst}/common`, "**/properties", `#/inst/${inst}/console`],
    ["全部配置", `#/inst/${inst}/raw`, "**/properties", `#/inst/${inst}/console`],
    ["内存启动", `#/inst/${inst}/jvm`, "**/api/app", `#/inst/${inst}/console`],
    ["插件 / 模组", `#/inst/${inst}/addons`, "**/mods", `#/inst/${inst}/console`],
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
    // 单独跑本条时不靠上一条留下的 localStorage：显式从深色 + nord 方案 + 跟随方案起步
    await page.evaluate(() => {
      localStorage.setItem("amh_theme", "dark");
      localStorage.setItem("amh_scheme", "nord");
      localStorage.setItem("amh_accent", "auto");
    });
    await page.goto(url("#/settings/appearance"));
    await page.waitForSelector('[data-accent-pick="violet"]');
    const [theme, scheme] = await page.evaluate(() => [document.documentElement.dataset.theme, document.documentElement.dataset.scheme]);
    assert(scheme === "nord", "初始配色方案应为 nord：" + scheme);
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

  await run("AI：模型列表的请求乱序回来时只认最后一次（晚到的旧结果不盖缓存）；拉的时候改了地址，旧结果不显示、弹层收起、箭头能点", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" });
    let n = 0;
    const fake = async r => {
      const k = ++n; // 第一次慢、第二次快：第二次先回来
      await new Promise(x => setTimeout(x, k === 1 ? 600 : 50));
      await r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { models: [{ id: k === 1 ? "old-model" : "new-model" }], source: "live" } }) });
    };
    try {
      await page.goto(url("#/settings/ai")); await settle();
      await page.route("**/api/ai/models", fake);
      await page.click("#ai-list"); await settle(20);
      await page.click("#ai-list"); await settle(300);
      let opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.join() === "new-model", "第二次先回来，应显示第二次的：" + opts);
      await settle(600); // 第一次（旧的）这时才回来
      opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.join() === "new-model", "晚到的旧结果不该显示：" + opts);
      await page.keyboard.press("Escape"); await settle(100);
      await page.click("#ai-model-btn"); await settle(200);
      opts = await page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id));
      assert(opts.join() === "new-model" && n === 2, "再展开用缓存，缓存不该被旧结果盖掉：" + opts + "（请求 " + n + " 次）");
      await page.keyboard.press("Escape"); await settle(100);
      // 拉的时候改了地址：在途的那次作废
      n = 0;
      await page.click("#ai-list"); await settle(50);
      await page.fill("#ai-url", "https://api.deepseek.com/v1"); await settle(50);
      assert(await page.$eval("#ai-model-pop", e => e.hidden) && !(await page.$eval("#ai-model-btn", b => b.disabled)), "改了地址弹层收起、箭头能点");
      await settle(700);
      assert(await page.$eval("#ai-model-pop", e => e.hidden), "旧地址的结果回来后不该再弹出来");
    } finally { await page.unroute("**/api/ai/models", fake); await aiState({ reset: true }); }
  });

  await run("AI：模型列表拉的时候搜索不翻旧列表、回来后按打的字筛；拉的时候换了服务商，箭头能点、报错不留；清除 Key 后不再用缓存（Codex 第二轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" });
    let n = 0, mode = "fast";
    const fake = async r => {
      n++;
      if (mode === "fast") return r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { models: [{ id: "old-a" }, { id: "old-b" }], source: "live" } }) });
      await new Promise(x => setTimeout(x, 600));
      if (mode === "slow-err") return r.fulfill({ status: 502, contentType: "application/json", body: JSON.stringify({ ok: false, error: "服务商出错（旧地址）" }) }).catch(() => {});
      await r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { models: [{ id: "new-a" }, { id: "new-old" }, { id: "new-b" }], source: "live" } }) }).catch(() => {});
    };
    const ids = () => page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id).join());
    try {
      await page.goto(url("#/settings/ai")); await settle();
      await page.route("**/api/ai/models", fake);
      await page.click("#ai-model-btn"); await settle(200);
      assert((await ids()) === "old-a,old-b", "先拉到一次：" + await ids());
      await page.keyboard.press("Escape"); await settle(100);
      // 1. 刷新：拉的时候在搜索框里打字，不拿旧列表搜；回来以后按打的字筛
      mode = "slow";
      await page.click("#ai-list"); await settle(100);
      await page.fill("#ai-model-q", "old"); await settle(100);
      assert((await ids()) === "", "拉的时候不该翻出旧列表：" + await ids());
      await settle(700);
      assert((await ids()) === "new-old", "回来以后按打的字筛：" + await ids());
      await page.keyboard.press("Escape"); await settle(100);
      // 2. 拉的时候换了服务商：弹层收起、箭头能点；旧服务商的报错回来也不显示
      mode = "slow-err";
      await page.click("#ai-list"); await settle(100);
      await page.selectOption("#ai-preset", "kimi"); await settle(100);
      assert(await page.$eval("#ai-model-pop", e => e.hidden) && !(await page.$eval("#ai-model-btn", b => b.disabled)), "换了服务商：弹层收起、箭头能点");
      await settle(700);
      assert((await page.textContent("#ai-model-err")) === "" && await page.$eval("#ai-model-pop", e => e.hidden), "旧服务商的报错不该显示：" + await page.textContent("#ai-model-err"));
      // 3. 换回来（不保存）再展开：缓存还在，不发请求；清除 Key 以后缓存作废，再展开要重新拉
      await page.selectOption("#ai-preset", "deepseek"); await settle(100);
      mode = "fast"; n = 0;
      await page.click("#ai-model-btn"); await settle(200);
      assert(n === 0 && (await ids()).startsWith("new-"), `换回来走缓存（请求 ${n} 次）：` + await ids());
      await page.keyboard.press("Escape"); await settle(100);
      await page.click("#ai-key-clear"); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(300);
      await page.click("#ai-model-btn"); await settle(200);
      assert(n === 1 && (await ids()) === "old-a,old-b", `清除 Key 后应重新拉（请求 ${n} 次）：` + await ids());
    } finally { await page.unroute("**/api/ai/models", fake); await aiState({ reset: true }); }
  });

  await run("AI：框里临时填的 Key 拉到的模型列表不进缓存，「放弃修改」以后再展开按已存的 Key 重新拉；用已存的 Key 拉到的照常缓存（Codex 第三轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" });
    let n = 0;
    const fake = async r => {
      n++;
      const temp = !!(r.request().postDataJSON() || {}).key;
      await r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { models: [{ id: temp ? "temp-key-model" : "saved-key-model" }], source: "live" } }) });
    };
    const ids = () => page.$$eval("#ai-model-list .ai-opt", os => os.map(o => o.dataset.id).join());
    try {
      await page.goto(url("#/settings/ai")); await settle();
      await page.route("**/api/ai/models", fake);
      await page.fill("#ai-key", "sk-temp-0123456789"); await settle(100);
      await page.click("#ai-model-btn"); await settle(200);
      assert((await ids()) === "temp-key-model" && n === 1, "临时 Key 拉到的：" + await ids());
      await page.keyboard.press("Escape"); await settle(100);
      await page.click('[data-sb="discard"]'); await settle(300);
      assert((await page.$eval("#ai-key", e => e.value)) === "", "放弃修改后 Key 框清空");
      await page.click("#ai-model-btn"); await settle(200);
      assert((await ids()) === "saved-key-model" && n === 2, `放弃修改后应按已存的 Key 重新拉（请求 ${n} 次）：` + await ids());
      await page.keyboard.press("Escape"); await settle(100);
      await page.click("#ai-model-btn"); await settle(200);
      assert((await ids()) === "saved-key-model" && n === 2, `已存 Key 的列表照常缓存（请求 ${n} 次）：` + await ids());
    } finally { await page.unroute("**/api/ai/models", fake); await aiState({ reset: true }); }
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
      // 按条增删（不再整张表发回去，见下面两个窗口的那条用例）
      const pb = putBody(t0, "/api/ai/prices");
      assert(pb && JSON.stringify(pb) === JSON.stringify({ addPrices: [{ model: "claude-opus-5-5", currency: "CNY", input: 30, output: 150 }] }), "自填单价：" + JSON.stringify(pb));
      assert((await page.textContent("#ai-model-price")).startsWith("自填：输入 ¥30 · 输出 ¥150"), "自填的应优先：" + await page.textContent("#ai-model-price"));
      t0 = reqs.length;
      await page.click('[data-del-price="0"]'); await settle(300);
      assert(JSON.stringify(putBody(t0, "/api/ai/prices")) === JSON.stringify({ removePrices: ["claude-opus-5-5"] }) && !(await page.$("[data-del-price]")), "删除自填单价");
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

  await run("AI 助手：思考到一半点停止，只有思考的这一条撤掉、提问撤回可放回输入框（同后端 fail()），不留下停在「思考中…」的消息", async () => {
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
      assert(s.msgs.length === 0 && !body.includes("思考中…"), "只有思考的那一条应撤掉：" + J(s));
      assert(body.includes("已停止") && body.includes("这句已从对话里撤回") && await page.$("#ap-body [data-reuse]"), "应提示已停止、提问已撤回：" + body.slice(0, 200));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：请求失败的提示正文只写结论，带状态码和英文原话的原文用「复制原文」拿（名字里带着这条提示）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await page.fill("#ap-in", "模拟报错"); await page.press("#ap-in", "Enter");
      await page.waitForSelector("#ap-body .ap-note.error", { timeout: 5000 }); await idle();
      const note = await page.textContent("#ap-body .ap-note.error");
      assert(note.includes("请求 AI 失败：Key 不对或已失效") && !note.includes("HTTP") && !note.includes("invalid"), "正文不该有原文：" + note);
      const btn = await page.$("#ap-body .ap-note.error [data-copy-raw]");
      assert(btn && (await btn.getAttribute("aria-label")) === "复制报错原文：请求 AI 失败：Key 不对或已失效", "应有「复制原文」");
      await page.evaluate(() => { window.__copied = []; Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied.push(t); } } }); });
      await btn.click(); await settle(200);
      const copied = await page.evaluate(() => window.__copied);
      assert(await toastHas("已复制报错原文"), "点了应复制原文");
      assert(copied.length === 1 && copied[0].includes("HTTP 401") && copied[0].includes("invalid x-api-key") && !copied[0].includes("请求 AI 失败"), "复制的应是原文本身：" + JSON.stringify(copied));
      assert((await page.textContent("#ap-body")).includes("这句已从对话里撤回"), "失败时提问撤回");
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：正文说到一半点停止，这一条标成没说完，摘要是「思考」不是「思考中…」", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await asstState({ slow: 120 });
      await page.fill("#ap-in", "服务器有点卡"); await page.press("#ap-in", "Enter");
      await snapWhen(m => m.some(x => x.md), "正文开始");
      await page.click("#ap-stop");
      await idle();
      const s = await asstSnap();
      const body = await page.textContent("#ap-body");
      const last = s.msgs[s.msgs.length - 1];
      assert(last && last.md && last.sum === "思考" && !last.typing && s.msgs.every(m => m.sum !== "思考中…"), "停下之后：" + J(s));
      assert(body.includes("没说完就停了") && body.includes("已停止"), "应标出没说完、已停止：" + body.slice(0, 200));
      // 这是第二次请求（第一次查了控制台）：提问不撤回，可以「继续回复」
      assert(!body.includes("这句已从对话里撤回") && await page.$('#ap-state [data-x="cont"]'), "第二次请求停下不撤回提问、给「继续回复」：" + body.slice(-300) + " | " + await page.textContent("#ap-state"));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：第一次请求说了一半就停，这一条留着标成没说完，提问照样撤回（同后端 conv.UndoUser），没有「继续回复」（Codex 第二轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await asstState({ slow: 250 });
      await page.fill("#ap-in", "最大人数改成 10"); await page.press("#ap-in", "Enter");
      await snapWhen(m => m.some(x => x.md), "正文开始");
      await page.click("#ap-stop");
      await idle();
      const s = await asstSnap();
      const body = await page.textContent("#ap-body");
      const last = s.msgs[s.msgs.length - 1];
      assert(last && last.md && body.includes("没说完就停了") && body.includes("已停止"), "说了一半的留着、标成没说完：" + J(s));
      assert(body.includes("这句已从对话里撤回") && await page.$("#ap-body [data-reuse]") && !(await page.$('#ap-state [data-x="cont"]')), "第一次请求停下应撤回提问、不给继续回复：" + body.slice(-300) + " | " + await page.textContent("#ap-state"));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：流式重画时，原文没变、渲染结果变了的块也重画（宽表格后面的字多了，同一张表从纯文字变回表格）（Codex 第四轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    const look = () => page.evaluate(() => {
      const ms = document.querySelectorAll("#ap-body .ap-msg.assistant .md"), el = ms[ms.length - 1];
      const first = el && el.querySelector(":scope > .md-b");
      return { says: el ? (el.textContent.match(/说/g) || []).length : 0, table: !!(first && first.querySelector("table")), busy: !document.querySelector("#ap-stop").hidden };
    });
    try {
      await newChat();
      await asstState({ slow: 250 });
      await page.fill("#ap-in", "宽表格"); await page.press("#ap-in", "Enter");
      // 第一段正好是整张表：整段输出放大太多，按纯文字显示（不是这样的话这条测不到要测的东西）
      await page.waitForFunction(() => { const ms = document.querySelectorAll("#ap-body .ap-msg.assistant .md"), el = ms[ms.length - 1]; return el && el.textContent.includes("|h|"); }, null, { timeout: 8000 });
      const early = await look();
      assert(!early.table && !early.says && early.busy, "表格刚写完时应按纯文字显示：" + JSON.stringify(early));
      // 后面的字够多了、还在流式输出：同一张表（原文没变）应该已经画成表格（回复结束时整条会重画，所以要在输出途中看）
      await page.waitForFunction(() => { const ms = document.querySelectorAll("#ap-body .ap-msg.assistant .md"), el = ms[ms.length - 1]; return el && (el.textContent.match(/说/g) || []).length >= 2200; }, null, { timeout: 15000 });
      const mid = await look();
      assert(mid.table && mid.busy, "后面的字多了以后、回复还没结束，表格应该已经画出来：" + JSON.stringify(mid));
      await page.waitForFunction(() => document.querySelector("#ap-stop").hidden, null, { timeout: 15000 });
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：确认卡片没处理就发了新消息、这句第一次请求就失败，撤回以后仍给「继续回复」（作废的结果已经交回，对话停在工具结果上；Codex 第四轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    try {
      await newChat();
      await page.fill("#ap-in", "重启"); await page.press("#ap-in", "Enter");
      await page.waitForSelector("#ap-body .ap-card.pending", { timeout: 5000 }); await idle();
      await page.fill("#ap-in", "模拟报错"); await page.press("#ap-in", "Enter");
      await page.waitForSelector("#ap-body .ap-note.error", { timeout: 5000 }); await idle();
      const body = await page.textContent("#ap-body");
      assert(body.includes("这句已从对话里撤回") && await page.$('#ap-state [data-x="cont"]'), "撤回新消息以后应回到作废的工具结果上、给继续回复：" + body.slice(-300) + " | " + await page.textContent("#ap-state"));
    } finally { await page.keyboard.press("Escape"); await asstState({ reset: true }); await aiState({ reset: true }); }
  });

  await run("AI 助手：工具结果交回去以后那次请求失败，提问不撤回、给「继续回复」；停在工具结果上再发一句、第一次请求就停下，撤回这句以后仍给「继续回复」（同后端 NeedsReply，Codex 第三轮）", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await asstState({ reset: true });
    const cont = () => page.$('#ap-state [data-x="cont"]');
    try {
      // 1. 查完控制台再报错
      await newChat();
      await page.fill("#ap-in", "查完再报错"); await page.press("#ap-in", "Enter");
      await page.waitForSelector("#ap-body .ap-note.error", { timeout: 5000 }); await idle();
      let body = await page.textContent("#ap-body");
      assert(!body.includes("这句已从对话里撤回") && await cont(), "工具结果之后失败：提问不撤回、给继续回复：" + body.slice(-300) + " | " + await page.textContent("#ap-state"));
      // 2. 停在工具结果上（第二次请求说到一半停），再发一句、第一次请求就停
      await page.keyboard.press("Escape"); await asstState({ reset: true });
      await newChat();
      await asstState({ slow: 120 });
      await page.fill("#ap-in", "服务器有点卡"); await page.press("#ap-in", "Enter");
      await snapWhen(m => m.some(x => x.md), "第二次请求的正文开始");
      await page.click("#ap-stop"); await idle();
      assert(await cont(), "第二次请求停下：给继续回复");
      await asstState({ slow: 250 });
      await page.fill("#ap-in", "最大人数改成 10"); await page.press("#ap-in", "Enter");
      await page.waitForFunction(() => [...document.querySelectorAll("#ap-body .ap-msg.assistant")].pop()?.textContent.includes("我先看"), null, { timeout: 5000 });
      await page.click("#ap-stop"); await idle();
      body = await page.textContent("#ap-body");
      assert(body.includes("这句已从对话里撤回") && await cont(), "撤回新的这句以后，对话回到工具结果上，仍给继续回复：" + body.slice(-300) + " | " + await page.textContent("#ap-state"));
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
      // 按名字一个一个发（不再整张名单发回去，见下面两个窗口的那条用例）
      assert(JSON.stringify(gcPuts(t0).pop()) === JSON.stringify({ addAllowed: ["Notch_CN"] }), "授权应即时发 PUT：" + JSON.stringify(gcPuts(t0)));
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
      assert(JSON.stringify(gcPuts(t1).pop()) === JSON.stringify({ removeAllowed: ["Notch_CN"] }) && await toastHas("已收回 Notch_CN 的执行权限"), "收回授权");
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
      // 按条增删（不再整张表发回去，见下面两个窗口的那条用例）：添加 = addZones，编辑 = 删掉原来的名字再加，删除 = removeZones
      const zone = { name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] };
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ addZones: [zone] }]) && !(await page.$(".modal")), "添加区域：" + JSON.stringify(cpPuts(t0)));
      assert((await page.textContent('#cp-zones [data-zone="base"]')).includes("主世界 · (-120, -64, -80) → (-40, 320, 10)"), "区域行");
      // 编辑：改名、换成模组的维度
      await page.click('[data-zedit="base"]'); await page.waitForSelector(".modal");
      await page.fill("#cz-name", "home"); await page.selectOption("#cz-dim", "");
      assert(!(await page.$eval("#cz-dim-row", e => e.hidden)), "选「其他」时要填维度 ID");
      await page.fill("#cz-dim-custom", "twilightforest:twilight_forest");
      t0 = reqs.length;
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ removeZones: ["base"], addZones: [{ ...zone, name: "home", dim: "twilightforest:twilight_forest" }] }]), "编辑区域：" + JSON.stringify(cpPuts(t0)));
      t0 = reqs.length;
      await page.click('[data-zdel="home"]'); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(JSON.stringify(cpPuts(t0)) === JSON.stringify([{ removeZones: ["home"] }]) && (await page.textContent("#cp-zones")).includes("还没有禁挖区域"), "删除区域");
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
          assert(r.navs.length === 5 && r.navs.every(Boolean), "底栏有标签被盖住、点不到" + where);
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
      // beta.3（可用性走查 R12）起停着的平台隧道一直显示上次拿到的地址、标「上次」，失败原因照样写在卡片上（以前这里断言不显示地址）
      assert(r.includes("上次出错") && r.includes("跟随启动失败：首次使用需手动放置樱花 frpc") && r.includes("natfrp 目录后重试") &&
        r.includes("cn-sz-bgp-1.openfrp.top:23456") && (await page.textContent('[data-tun="t1"] .tun-last')) === "上次", "跟随启动失败：" + r);
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

  await run("隧道：存着凭据的自定义 frps 有「清除凭据」，确认后只发 clearCredential、其余配置在后端原样留着；服务商的隧道没有这个按钮", async () => {
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
      // 修改接口只改传了的项：清除凭据只发这一项，别的配置不拿列表里的（可能是几秒前的）带回去（Codex 复审 v3.2.0 第二轮）
      const b = await lastPut();
      assert(JSON.stringify(b) === JSON.stringify({ clearCredential: true }), "请求体：" + JSON.stringify(b));
      const t2 = await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data.tunnels.find(t => t.id === "t2")));
      assert(t2 && t2.provider === "custom" && t2.name === "自建备用线" && t2.serverAddr === "frp.example.com" && t2.serverPort === 7000 && t2.remotePort === 25566 &&
        t2.localPort === 25565 && t2.autoStart === false && !t2.hasCredential, "别的配置原样留着、凭据没了：" + JSON.stringify(t2));
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

  // ==================== 平台账户 ====================
  // 仿真后端：/__mock/state 的 tunnelAccounts（reset / openfrp / natfrp / local / openfrpUnreadable / natfrpUnreadable /
  // openfrpLoginDoneAtPoll / overviews）；响应里有 lastSign / lastImport / lastCreate
  const taState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data.tunnelAccounts), b);
  const resetTA = () => taState({ tunnelAccounts: { reset: true }, tunnelsReset: true });
  const hookOpens = () => page.evaluate(() => {
    window.__opens = [];
    window.open = u => { window.__opens.push(String(u || "")); return null; };
  });
  const opened = () => page.evaluate(() => window.__opens || []);
  const TA_API = "/api/tunnel-accounts";
  const taReqs = since => reqs.slice(since).filter(r => r.u.startsWith(TA_API)).map(r => r.m + " " + r.u);
  // 等仿真后端记下某次请求（lastSign / lastImport / lastCreate），拿不到就返回当时的值，由后面的断言报错
  const taWait = async (key, okFn, timeout = 4000) => {
    const end = Date.now() + timeout;
    for (;;) {
      const v = (await taState({}))[key];
      if ((v && okFn(v)) || Date.now() > end) return v;
      await settle(100);
    }
  };
  const modalGone = () => page.waitForFunction(() => !document.querySelector("#modal-root .modal-mask"), null, { timeout: 3000 }).catch(() => {});
  // 打开隧道页并展开某一家的概览（要先把那一家设成已连接）
  const openOverview = async provider => {
    await page.goto(url("#/tunnels")); await settle(600);
    await page.click(`[data-acc-overview="${provider}"]`);
    await page.waitForSelector(`#tn-acc-panel [data-acc-create="${provider}"]`);
  };

  await run("平台账户：OpenFrp 默认使用远程授权，樱花保留密钥输入", async () => {
    await resetTA();
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('#tn-accounts [data-acc="openfrp"] [data-acc-connect="openfrp"]');
      const ui = await page.evaluate(() => {
        const of = document.querySelector('#tn-accounts [data-acc="openfrp"]');
        const sk = document.querySelector('#tn-accounts [data-acc="natfrp"]');
        return {
          title: (document.querySelector("#tn-accounts h3") || {}).textContent || "",
          declaration: (document.querySelector("#tn-accounts .tun-accounts-head .hint") || {}).textContent || "",
          ofText: (of && of.textContent || "").replace(/\s+/g, " ").trim(),
          ofInputs: !!(of && of.querySelector("input")),
          ofConnect: !!(of && of.querySelector('[data-acc-connect="openfrp"]')),
          ofBrowser: !!(of && of.querySelector('[data-acc-browser-login="openfrp"]')),
          skToken: !!(sk && sk.querySelector("#tn-sakura-token")),
          skConnect: !!(sk && sk.querySelector('[data-acc-connect="natfrp"]')),
          skWeb: !!(sk && sk.querySelector('[data-acc-web-login="natfrp"]')),
          skGetToken: !!(sk && sk.querySelector('[data-acc-get-token="natfrp"]')),
          skText: (sk && sk.textContent || "").replace(/\s+/g, " ").trim(),
          roTip: !!document.querySelector("#tn-acc-ro:not([hidden])"),
        };
      });
      assert(ui.title.includes("平台账户") && ui.declaration.includes("OPENAPI") && ui.declaration.includes("社区开发") &&
        ui.declaration.includes("节点问题") && ui.ofConnect && !ui.ofInputs && !ui.ofBrowser && ui.ofText.includes("远程授权"),
        "OpenFrp 未连接态：" + JSON.stringify(ui));
      assert(ui.skToken && ui.skConnect && ui.skGetToken && !ui.skWeb && ui.skText.includes("访问密钥"),
        "樱花密钥输入 / 获取访问密钥：" + JSON.stringify(ui));
      assert(!ui.roTip, "本机打开时不该有局域网只读的提示");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：樱花未连接时「获取访问密钥」打开官网用户信息页，不再有网页登录", async () => {
    await resetTA();
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('[data-acc-get-token="natfrp"]');
      await hookOpens();
      const before = reqs.length;
      const btn = await page.$eval('[data-acc-get-token="natfrp"]', e => e.textContent.trim());
      assert(btn === "获取访问密钥", "按钮文字：" + btn);
      await page.click('[data-acc-get-token="natfrp"]'); await settle(300);
      const urls = await opened();
      assert(urls.length === 1 && urls[0] === "https://www.natfrp.com/user/profile", "应打开樱花的用户信息页：" + JSON.stringify(urls));
      assert(taReqs(before).length === 0, "点「获取访问密钥」不该请求后端：" + taReqs(before).join(", "));
      const text = await page.evaluate(() => document.body.innerText);
      const card = await page.$eval('#tn-accounts [data-acc="natfrp"]', e => e.textContent.replace(/\s+/g, " ").trim());
      assert(card.includes("在樱花官网的用户信息页复制访问密钥，粘贴到这里连接。"), "卡片说明：" + card);
      for (const w of ["OpenID", "自动换票", "抓包", "网页登录"]) assert(!text.includes(w), `页面上不该再出现「${w}」`);
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：OpenFrp 默认远程授权并轮询到已连接", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrpLoginDoneAtPoll: 1 }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.evaluate(() => { window.__MD_TUNNEL_POLL_MS = 300; });
      await hookOpens();
      const before = reqs.length;
      await page.click('[data-acc-connect="openfrp"]');
      await page.waitForFunction(() => (window.__opens || []).some(u => /openfrp\.net\/oauth/.test(u)), null, { timeout: 3000 }).catch(() => {});
      assert(reqs.slice(before).some(r => r.m === "POST" && r.u === "/api/tunnel-accounts/openfrp/login"),
        "应 POST /api/tunnel-accounts/openfrp/login：" + reqs.slice(before).map(r => r.m + " " + r.u).join(", "));
      const urls = await opened();
      assert(urls.some(u => u.includes("console.openfrp.net/oauth")), "应打开授权链接：" + JSON.stringify(urls));
      await page.waitForFunction(() => {
        const el = document.querySelector('#tn-accounts [data-acc="openfrp"]');
        return el && el.textContent.includes("已连接") && !!el.querySelector('[data-acc-overview="openfrp"]');
      }, null, { timeout: 4000 }).catch(() => {});
      const card = await page.$eval('#tn-accounts [data-acc="openfrp"]', e => e.textContent.replace(/\s+/g, " ").trim());
      assert(card.includes("已连接") && !card.includes("未连接"), "轮询成功后应变已连接：" + card);
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：概览里导入平台隧道时 POST import 带 proxyId", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.click('[data-acc-overview="openfrp"]');
      await page.waitForSelector('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      await page.waitForFunction(async () => {
        const j = await fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json());
        return !!(j.data.tunnelAccounts && j.data.tunnelAccounts.lastImport);
      }, null, { timeout: 3000 }).catch(() => {});
      const last = (await taState({})).lastImport;
      assert(last && last.provider === "openfrp" && String(last.proxyId) === "1001",
        "import 应带 proxyId=1001：" + JSON.stringify(last));
      assert(await toastHas("已导入到 MineDeck"), "应提示已导入");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：导入平台隧道时用途跟着类型走（udp 是语音，tcp 是游戏）", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await openOverview("openfrp");
      const importOne = async id => {
        const sel = `#tn-acc-panel [data-acc-import="openfrp"][data-proxy="${id}"]`;
        await page.waitForSelector(sel);
        await page.click(sel);
        await page.waitForSelector('.modal [data-x="ok"]');
        await page.click('.modal [data-x="ok"]');
        const last = await taWait("lastImport", v => v.proxyId === id);
        await modalGone(); await settle(500); // 等弹窗退场、概览重画完，再点下一条
        return last;
      };
      const types = await page.$$eval("#tn-acc-panel [data-acc-import]", bs => Object.fromEntries(bs.map(b => [b.dataset.proxy, b.dataset.type])));
      assert(types["1003"] === "udp" && types["1001"] === "tcp", "导入按钮上应带着平台隧道的类型：" + JSON.stringify(types));
      const udp = await importOne("1003");
      assert(udp && udp.proxyId === "1003" && udp.purpose === "voice", "udp 隧道应按语音导入：" + JSON.stringify(udp));
      const tcp = await importOne("1001");
      assert(tcp && tcp.proxyId === "1001" && tcp.purpose === "game", "tcp 隧道应按游戏导入：" + JSON.stringify(tcp));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：开了「减少动态」时导入弹窗关掉也不丢绑定实例和跟随启动", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    await page.emulateMedia({ reducedMotion: "reduce" });
    try {
      errors.length = 0;
      await openOverview("openfrp");
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector(".modal #tn-imp-bound");
      const inst = await page.$eval(".modal #tn-imp-bound", s => [...s.options].map(o => o.value).find(v => v) || "");
      assert(inst, "导入弹窗里应能选实例");
      await page.selectOption(".modal #tn-imp-bound", inst);
      await page.check(".modal #tn-imp-auto"); // 默认不勾（A62），这里要验的是勾上的值也带得出去
      await page.click('.modal [data-x="ok"]');
      const last = await taWait("lastImport", v => v.proxyId === "1001");
      assert(last && last.boundInstance === inst && last.autoStart === true,
        "弹窗立刻移除时也要带上选好的实例和跟随启动：" + JSON.stringify(last));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await page.emulateMedia({ reducedMotion: null }); await resetTA(); }
  });

  await run("平台账户：取消等待 OpenFrp 授权时通知后端作废这次授权", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrpLoginDoneAtPoll: 99 }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.evaluate(() => { window.__MD_TUNNEL_POLL_MS = 300; });
      await hookOpens();
      await page.click('[data-acc-connect="openfrp"]');
      await page.waitForSelector('[data-acc-cancel-login="openfrp"]');
      const id = ((await taState({})).openfrpLogin || {}).id;
      assert(id, "应已发起授权");
      await page.click('[data-acc-cancel-login="openfrp"]');
      const cancelled = await taWait("lastCancel", v => v === id);
      assert(cancelled === id, "取消时应 POST cancel 带上这次的登录 ID：" + JSON.stringify(cancelled));
      const st = await taState({});
      assert(!st.openfrpLogin && !st.openfrp, "取消后这次授权应作废、仍未连接：" + JSON.stringify({ login: st.openfrpLogin, openfrp: st.openfrp }));
      const card = await page.$eval('#tn-accounts [data-acc="openfrp"]', e => e.textContent.replace(/\s+/g, " ").trim());
      assert(card.includes("未连接"), "卡片应回到未连接：" + card);
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await page.evaluate(() => { delete window.__MD_TUNNEL_POLL_MS; }); await resetTA(); }
  });

  await run("平台账户：取消等待时授权刚好已经完成，界面以后端为准显示已连接", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrpLoginDoneAtPoll: 99 }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.evaluate(() => { window.__MD_TUNNEL_POLL_MS = 60000; }); // 这条用例里不让前端自己轮询
      await hookOpens();
      await page.click('[data-acc-connect="openfrp"]');
      await page.waitForSelector('[data-acc-cancel-login="openfrp"]');
      // 用户在浏览器里刚好授权完成：后端已连上、这次登录不在等了
      await taState({ tunnelAccounts: { openfrp: true, openfrpLogin: null } });
      await page.click('[data-acc-cancel-login="openfrp"]');
      await page.waitForFunction(() => {
        const el = document.querySelector('#tn-accounts [data-acc="openfrp"]');
        return el && el.textContent.includes("已连接") && !el.textContent.includes("未连接");
      }, null, { timeout: 4000 }).catch(() => {});
      const card = await page.$eval('#tn-accounts [data-acc="openfrp"]', e => e.textContent.replace(/\s+/g, " ").trim());
      assert(card.includes("已连接") && !card.includes("未连接"), "取消时授权已完成，应以后端为准显示已连接：" + card);
      assert(await toastHas("取消前已经完成"), "应说明这次授权在取消前已经完成");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await page.evaluate(() => { delete window.__MD_TUNNEL_POLL_MS; }); await resetTA(); }
  });

  await run("平台账户：取消等待后查不到连接状态时不下「没有连上」的结论", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrpLoginDoneAtPoll: 99 }, tunnelsReset: true });
    const statusFails = u => u.pathname === "/api/tunnel-accounts"; // page.route 的匹配函数拿到的是 URL
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.evaluate(() => { window.__MD_TUNNEL_POLL_MS = 60000; });
      await hookOpens();
      await page.click('[data-acc-connect="openfrp"]');
      await page.waitForSelector('[data-acc-cancel-login="openfrp"]');
      await taState({ tunnelAccounts: { openfrp: true, openfrpLogin: null } }); // 授权刚好已完成
      await page.route(statusFails, route => route.request().method() === "GET"
        ? route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ ok: false, error: "后端暂时连不上" }) })
        : route.continue());
      await page.click('[data-acc-cancel-login="openfrp"]');
      await page.waitForFunction(() => document.body.textContent.includes("没能确认现在的连接状态"), null, { timeout: 4000 }).catch(() => {});
      assert(await toastHas("没能确认现在的连接状态"), "查不到状态时应说没能确认");
      assert(!(await toastHas("没有连上")), "查不到状态时不该断言没有连上");
    } finally {
      await page.unroute(statusFails);
      await page.evaluate(() => { delete window.__MD_TUNNEL_POLL_MS; });
      await resetTA();
    }
  });

  await run("平台账户：导入后的刷新还在等状态时收起概览，状态回来也不再把概览展开", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    const statusURL = u => u.pathname === "/api/tunnel-accounts";
    let release, gotHeld;
    const gate = new Promise(r => { release = r; });
    const held = new Promise(r => { gotHeld = r; });
    try {
      errors.length = 0;
      await openOverview("openfrp");
      // 从现在起连接状态的请求先卡住；卡住第一条时通知测试
      await page.route(statusURL, async route => {
        if (route.request().method() === "GET") { gotHeld(); await gate; }
        return route.continue();
      });
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      const last = await taWait("lastImport", v => v.proxyId === "1001");
      assert(last && last.proxyId === "1001", "导入请求没有到后端");
      // 导入完成，刷新正卡在状态请求上
      await Promise.race([held, settle(4000).then(() => { throw new Error("导入后的状态请求没有被拦下"); })]);
      await page.click('[data-acc-overview="openfrp"]'); // 用户收起概览
      await page.waitForFunction(() => document.querySelector("#tn-acc-panel").hidden, null, { timeout: 3000 });
      const statusBack = page.waitForResponse(r => statusURL(new URL(r.url())) && r.request().method() === "GET", { timeout: 4000 });
      release();
      await statusBack;
      await settle(600); // 状态回来以后旧刷新还要走完（旧代码会在这里再拉一次概览并展开）
      const hidden = await page.$eval("#tn-acc-panel", el => el.hidden);
      assert(hidden, "用户收起以后，迟到的刷新不该把概览重新展开");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { release(); await page.unroute(statusURL); await resetTA(); }
  });

  await run("平台账户：OpenFrp 签到只引导去官方控制台，页面上没有内嵌验证码", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await hookOpens();
      const before = reqs.length;
      await page.click('[data-acc-sign="openfrp"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      const modal = await page.$eval(".modal", m => m.textContent.replace(/\s+/g, " ").trim());
      assert(modal.includes("OpenFrp 签到需要在官方控制台完成人机验证，MineDeck 不代签。") &&
        modal.includes("将打开 https://console.openfrp.net/dashboard?action=sign"), "签到引导：" + modal);
      assert((await opened()).length === 0, "确认之前不该打开任何页面");
      await page.click('.modal [data-x="ok"]'); await settle(300);
      const urls = await opened();
      assert(urls.length === 1 && urls[0] === "https://console.openfrp.net/dashboard?action=sign", "确认后应打开官方控制台的签到页：" + JSON.stringify(urls));
      const frames = await page.evaluate(() => document.querySelectorAll("iframe").length);
      assert(frames === 0 && page.frames().length === 1, "页面上不该有 iframe：" + frames + " / " + page.frames().length);
      const all = reqs.slice(before).map(r => r.m + " " + r.u);
      assert(all.includes("POST /api/tunnel-accounts/openfrp/sign") && !all.some(x => /captcha/i.test(x)),
        "只该请求 sign，不该再取验证码：" + all.join(", "));
      const last = (await taState({})).lastSign;
      assert(last && last.provider === "openfrp" && Object.keys(last).length === 1, "sign 的请求体应为空对象：" + JSON.stringify(last));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：樱花签到确认后打开官网用户中心", async () => {
    await taState({ tunnelAccounts: { reset: true, natfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle(600);
      await hookOpens();
      await page.click('[data-acc-sign="natfrp"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      const modal = await page.$eval(".modal", m => m.textContent.replace(/\s+/g, " ").trim());
      assert(modal.includes("SakuraFrp 签到需要在官网登录后完成，访问密钥不能签到。") &&
        modal.includes("将打开 https://www.natfrp.com/user/"), "签到引导：" + modal);
      await page.click('.modal [data-x="ok"]'); await settle(300);
      const urls = await opened();
      assert(urls.length === 1 && urls[0] === "https://www.natfrp.com/user/", "确认后应打开樱花官网的用户中心：" + JSON.stringify(urls));
      assert(await page.evaluate(() => document.querySelectorAll("iframe").length) === 0, "页面上不该有 iframe");
      // 取消则什么都不打开
      await hookOpens();
      await modalGone();
      await page.click('[data-acc-sign="natfrp"]');
      await page.waitForSelector('.modal [data-x="cancel"]');
      await page.click('.modal [data-x="cancel"]'); await settle(300);
      assert((await opened()).length === 0, "取消后不该打开页面：" + JSON.stringify(await opened()));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：局域网打开时账户区只读，不出按钮也不拉概览", async () => {
    const RO_TIP = "在局域网里打开时只能看连接状态，连接、签到、创建和导入隧道请在运行 MineDeck 的这台电脑上操作。";
    const look = () => page.evaluate(() => {
      const sec = document.querySelector("#tn-accounts");
      const txt = sel => ((sec.querySelector(sel) || {}).textContent || "").replace(/\s+/g, " ").trim();
      const tip = sec.querySelector("#tn-acc-ro");
      return {
        buttons: [...sec.querySelectorAll("button, [role=button], a.btn")].map(b => b.textContent.trim()),
        inputs: sec.querySelectorAll("input, select, textarea").length,
        tip: tip && !tip.hidden ? tip.textContent.trim() : "",
        of: txt('[data-acc="openfrp"]'), sk: txt('[data-acc="natfrp"]'),
        panel: !!sec.querySelector("#tn-acc-panel:not([hidden])"),
      };
    });
    try {
      errors.length = 0;
      // 两家都已连接
      await taState({ tunnelAccounts: { reset: true, openfrp: true, natfrp: true, local: false }, tunnelsReset: true });
      let before = reqs.length;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('#tn-accounts [data-acc="natfrp"]');
      let ui = await look();
      assert(ui.buttons.length === 0 && ui.inputs === 0, "只读时账户区不该有按钮和输入框：" + JSON.stringify(ui));
      assert(ui.tip === RO_TIP, "只读提示：" + JSON.stringify(ui.tip));
      assert(ui.of.includes("已连接") && ui.sk.includes("已连接") && !ui.panel, "只显示连接状态：" + JSON.stringify(ui));
      for (const w of ["概览", "签到", "购买", "断开", "浏览器授权"]) assert(!(ui.of + ui.sk).includes(w), `只读时卡片上不该有「${w}」：` + JSON.stringify(ui));
      assert(!taReqs(before).some(x => x.includes("/overview")), "只读时不该请求 overview：" + taReqs(before).join(", "));
      assert(taReqs(before).join(",") === "GET " + TA_API, "只读时只该查一次连接状态：" + taReqs(before).join(", "));
      // 两家都没连接：也不出输入框和连接按钮
      await taState({ tunnelAccounts: { reset: true, local: false } });
      before = reqs.length;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('#tn-accounts [data-acc="natfrp"]');
      ui = await look();
      assert(ui.buttons.length === 0 && ui.inputs === 0 && ui.tip === RO_TIP && ui.of.includes("未连接") && ui.sk.includes("未连接"),
        "只读且未连接：" + JSON.stringify(ui));
      assert(!taReqs(before).some(x => x.includes("/overview")), "只读时不该请求 overview：" + taReqs(before).join(", "));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：凭据读不出来时按未连接显示，并提示重新连接", async () => {
    const LOST = "之前保存的凭据在这台电脑上读不出来（可能换了电脑或 Windows 用户），请重新连接。";
    const cards = () => page.evaluate(() => Object.fromEntries(["openfrp", "natfrp"].map(p => {
      const c = document.querySelector(`#tn-accounts [data-acc="${p}"]`);
      return [p, {
        text: c.textContent.replace(/\s+/g, " ").trim(),
        connect: !!c.querySelector(`[data-acc-connect="${p}"]`),
        overview: !!c.querySelector(`[data-acc-overview="${p}"]`),
        token: !!c.querySelector("#tn-sakura-token"),
      }];
    })));
    try {
      errors.length = 0;
      await taState({ tunnelAccounts: { reset: true, openfrp: true, openfrpUnreadable: true }, tunnelsReset: true });
      const before = reqs.length;
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('#tn-accounts [data-acc="natfrp"]');
      let c = await cards();
      assert(c.openfrp.text.includes("未连接") && !c.openfrp.text.includes("已连接") && c.openfrp.text.includes(LOST) &&
        c.openfrp.connect && !c.openfrp.overview, "OpenFrp 凭据读不出来：" + JSON.stringify(c.openfrp));
      assert(!c.natfrp.text.includes(LOST), "另一家不该跟着提示：" + JSON.stringify(c.natfrp));
      assert(!taReqs(before).some(x => x.includes("/overview")), "按未连接算，不该请求 overview：" + taReqs(before).join(", "));
      await taState({ tunnelAccounts: { reset: true, natfrp: true, natfrpUnreadable: true } });
      await page.goto(url("#/tunnels")); await settle(600);
      await page.waitForSelector('#tn-accounts [data-acc="natfrp"]');
      c = await cards();
      assert(c.natfrp.text.includes("未连接") && c.natfrp.text.includes(LOST) && c.natfrp.connect && c.natfrp.token && !c.natfrp.overview,
        "樱花凭据读不出来：" + JSON.stringify(c.natfrp));
      assert(!c.openfrp.text.includes(LOST), "另一家不该跟着提示：" + JSON.stringify(c.openfrp));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  // 以前 tunnels.json 写不进去时后端照样报成功，重启以后隧道没了；现在报失败，提示接后端另给的那句人话，原文进「详情」
  await run("平台账户：tunnels.json 写不进去时导入、创建报失败，提示说人话、详情是原文、本机列表不多出隧道", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true, saveFail: true }, tunnelsReset: true });
    const localTunnels = () => page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data.tunnels.length));
    try {
      errors.length = 0;
      const before = await localTunnels();
      await openOverview("openfrp");
      // 导入
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      await taWait("lastImport", v => v.proxyId === "1001");
      const HINT = "隧道配置写不进磁盘，这次没有保存。请检查磁盘是否已满、MineDeck 所在的文件夹是否只读";
      assert(await toastHas(HINT), "导入失败应提示后端给的那句人话");
      assert(!(await toastHas("已导入到 MineDeck", 300)), "写不进去时不该提示已导入");
      const imp = await toastDetail(HINT);
      assert(imp.includes("保存到 MineDeck 失败") && imp.includes("Access is denied"), "「详情」里应是完整原文：" + imp);
      assert(await page.$eval(".toast.error", t => !t.textContent.includes("Access is denied")), "提示正文不该是系统的英文原文");
      assert(await localTunnels() === before, "导入失败后本机列表不该多出隧道");
      await modalGone(); await settle(400);
      // 创建：平台上建好了、本机存不上，要说去导入、别再创建。原因写在对话框里（A57 起不再只弹提示）：正文接后端那句人话，
      // 复制键里是完整原文
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector("#tn-cr-name");
      await page.fill("#tn-cr-name", "mc_savefail");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.name === "mc_savefail");
      const CHINT = "平台上的隧道已建好，但没能存进 MineDeck。检查磁盘后在平台隧道列表里导入它，不要再创建";
      const crErr = await page.waitForFunction(h => { const e = document.querySelector(".modal #tn-cr-err"); return !!e && e.textContent.includes(h); }, CHINT, { timeout: 3000 }).then(() => true, () => false);
      assert(crErr, "创建失败应在对话框里说平台上已建好、去导入：" + await page.$eval(".modal #tn-cr-err", e => e.textContent).catch(() => "（对话框没了）"));
      assert(!(await toastHas("已创建并导入", 300)), "写不进去时不该提示已创建并导入");
      const cr = await page.$eval(".modal #tn-cr-err [data-err-raw]", b => b.getAttribute("data-err-raw"));
      assert(cr.includes("平台上的隧道已经建好") && cr.includes("Access is denied"), "复制键里应是完整原文：" + cr);
      assert(!(await page.$eval("#tn-cr-err", e => e.textContent.includes("Access is denied"))), "正文不该是系统的英文原文");
      assert(await localTunnels() === before, "创建失败后本机列表不该多出隧道");
      // 磁盘恢复以后：再建一条照常成功，刚才平台上建好、本机没存上的那条也能导入（两条编号不能撞上，Codex 复审发现仿真后端撞过）
      const failedId = String(((await taState({})).overviews.openfrp.tunnels.find(t => t.name === "mc_savefail") || {}).id || "");
      assert(failedId, "平台概览里应有刚才建好的那条");
      await page.click('.modal [data-x="cancel"]');
      await modalGone();
      await taState({ tunnelAccounts: { saveFail: false } });
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector("#tn-cr-name");
      await page.fill("#tn-cr-name", "mc_after");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.name === "mc_after");
      assert(await toastHas("已创建并导入"), "磁盘恢复以后创建应成功");
      await modalGone();
      await openOverview("openfrp");
      const sel = `#tn-acc-panel [data-acc-import="openfrp"][data-proxy="${failedId}"]`;
      await page.waitForSelector(sel);
      await page.click(sel);
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      await taWait("lastImport", v => v.proxyId === failedId);
      assert(await toastHas("已导入到 MineDeck"), "刚才没存上的那条应能导入（编号 " + failedId + "）");
      assert(await localTunnels() === before + 2, "恢复以后本机应多出两条");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  // 创建隧道失败（A57）：对话框留着、写明原因（后端给的话）、填的东西都在，后面的概览重新拉。平台上没建成的，按钮恢复、改了再试；
  // 平台上已经建好、本机没导入成的（后端回 created），再点一次平台上就多一条：「创建并导入」锁住，换成「关闭，去导入」，
  // 关掉以后去概览里导入那一条。成功时对话框关掉、概览里有新隧道
  await run("平台账户：创建隧道失败时原因写在对话框里、概览重新拉；平台上没建成的按钮恢复，建成了的锁住、换成「关闭，去导入」；成功后关掉对话框、概览里有新隧道", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    const errText = () => page.$eval(".modal #tn-cr-err", e => e.textContent.trim()).catch(() => "（对话框没了）");
    const createPosts = since => reqs.slice(since).filter(r => r.m === "POST" && r.u === "/api/tunnel-accounts/openfrp/tunnels").length;
    const btns = () => page.evaluate(() => {
      const b = x => document.querySelector(`.modal [data-x="${x}"]`) || {};
      return { okHidden: b("ok").hidden, okDisabled: b("ok").disabled, goHidden: b("go-import").hidden, goDisabled: b("go-import").disabled, goText: (b("go-import").textContent || "").trim() };
    });
    try {
      errors.length = 0;
      await openOverview("openfrp");
      // 一：平台不接受（公网端口填成 0）：平台上什么都没建，按钮恢复，改了再试
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector("#tn-cr-name");
      await page.fill("#tn-cr-name", "mc_retry");
      await page.fill("#tn-cr-rport", "0");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.name === "mc_retry");
      await page.waitForFunction(() => /\S/.test((document.querySelector(".modal #tn-cr-err") || {}).textContent || ""), null, { timeout: 3000 }).catch(() => {});
      assert((await errText()).includes("没能创建隧道：OpenFrp 需要填写有效的公网端口"), "对话框里应写明原因：" + await errText());
      assert(await page.$eval(".modal #tn-cr-name", e => e.value) === "mc_retry", "填的东西不该丢");
      let b = await btns();
      assert(!b.okHidden && !b.okDisabled && b.goHidden, "平台上没建成：「创建并导入」应恢复可点，不出「关闭，去导入」：" + JSON.stringify(b));
      // 二：平台上建好了、本机写不进磁盘：原因照样在对话框里；「创建并导入」锁住，换成「关闭，去导入」，硬点也不再发创建
      await taState({ tunnelAccounts: { saveFail: true } });
      await page.fill("#tn-cr-rport", "25566");
      const before = reqs.length;
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.name === "mc_retry" && v.remotePort === 25566);
      await page.waitForFunction(() => ((document.querySelector(".modal #tn-cr-err") || {}).textContent || "").includes("平台上的隧道已建好"), null, { timeout: 3000 }).catch(() => {});
      assert((await errText()).includes("平台上的隧道已建好，但没能存进 MineDeck"), "写不进磁盘时对话框里应说平台上已建好、去导入：" + await errText());
      b = await btns();
      assert(b.okHidden && b.okDisabled && !b.goHidden && !b.goDisabled && b.goText === "关闭，去导入", "平台上建好了：「创建并导入」应锁住、换成「关闭，去导入」：" + JSON.stringify(b));
      await page.$eval('.modal [data-x="ok"]', x => x.click());
      await settle(500);
      assert(createPosts(before) === 1, "锁住以后再点也不该发创建：" + createPosts(before));
      const made = (await taState({})).overviews.openfrp.tunnels.find(t => t.name === "mc_retry");
      assert(made, "平台概览里应有刚建好的那条");
      const impSel = `#tn-acc-panel [data-acc-import="openfrp"][data-proxy="${made.id}"]`;
      await page.waitForSelector(impSel, { timeout: 3000 }).catch(() => {});
      assert(await page.$(impSel), "对话框后面的概览应已重新拉过、能导入刚建好的那条");
      // 三：点「关闭，去导入」：对话框关掉，焦点落在那一条的「导入到 MineDeck」上；磁盘恢复以后导入成功，不用再建
      await page.click('.modal [data-x="go-import"]');
      await modalGone();
      assert(await page.$("#tn-cr-name") === null, "点「关闭，去导入」后对话框应关闭");
      await page.waitForFunction(s => document.activeElement && document.activeElement.matches(s), impSel, { timeout: 3000 }).catch(() => {});
      assert(await page.evaluate(s => !!document.activeElement && document.activeElement.matches(s), impSel), "焦点应落在刚建好那条的「导入到 MineDeck」上");
      assert(createPosts(before) === 1, "关掉对话框也不该再发创建：" + createPosts(before));
      await taState({ tunnelAccounts: { saveFail: false } });
      await page.click(impSel);
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      await taWait("lastImport", v => v.proxyId === String(made.id));
      assert(await toastHas("已导入到 MineDeck"), "刚才建好、没存上的那条应能导入");
      await modalGone();
      await page.waitForFunction(() => document.querySelector("#tun-list") && document.querySelector("#tun-list").textContent.includes("mc_retry"), null, { timeout: 3000 }).catch(() => {});
      assert(await page.$eval("#tun-list", e => e.textContent.includes("mc_retry")), "本机隧道列表里应有导入的那条");
      // 四：再建一条：成功，对话框关掉，概览和本机列表里都有新隧道
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector("#tn-cr-name");
      await page.fill("#tn-cr-name", "mc_ok");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.name === "mc_ok");
      assert(await toastHas("已创建并导入"), "应提示已创建并导入");
      await modalGone();
      assert(await page.$("#tn-cr-name") === null, "成功后对话框应关闭");
      await page.waitForFunction(() => [...document.querySelectorAll("#tn-acc-panel [data-plat-tun]")].some(r => r.textContent.includes("mc_ok")), null, { timeout: 3000 }).catch(() => {});
      assert(await page.evaluate(() => [...document.querySelectorAll("#tn-acc-panel [data-plat-tun]")].some(r => r.textContent.includes("mc_ok"))), "概览里应有新隧道");
      await page.waitForFunction(() => document.querySelector("#tun-list") && document.querySelector("#tun-list").textContent.includes("mc_ok"), null, { timeout: 3000 }).catch(() => {});
      assert(await page.$eval("#tun-list", e => e.textContent.includes("mc_ok")), "本机隧道列表里应有新隧道");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  // 樱花创建框请求途中（A57）：以前切一下「这是公开服务器」节点列表重画，「创建并导入」跟着被放开，能发第二次创建，
  // 平台上多出一条。现在请求途中开关和按钮都锁着，硬点也只发一次
  await run("平台账户：樱花创建框请求途中「这是公开服务器」和「创建并导入」都锁着，硬点也只发一次创建", async () => {
    const pattern = "**/api/tunnel-accounts/natfrp/tunnels";
    const slow = async route => { await new Promise(r => setTimeout(r, 1500)); await route.continue(); };
    const locked = () => page.evaluate(() => {
      const ok = document.querySelector('.modal [data-x="ok"]'), pub = document.querySelector(".modal #tn-cr-public");
      return { ok: ok.disabled, pub: pub.disabled, checked: pub.checked };
    });
    await taState({ tunnelAccounts: { reset: true, natfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await openOverview("natfrp");
      await page.click('#tn-acc-panel [data-acc-create="natfrp"]');
      await page.waitForSelector("#tn-cr-public");
      const n0 = (await taState({})).overviews.natfrp.tunnels.length;
      await page.route(pattern, slow);
      const before = reqs.length;
      await page.click('.modal [data-x="ok"]');
      let l = await locked();
      assert(l.ok && l.pub && !l.checked, "请求途中按钮和「这是公开服务器」都应锁住：" + JSON.stringify(l));
      await page.$eval(".modal #tn-cr-public", x => x.click());
      l = await locked();
      assert(l.ok && l.pub && !l.checked, "请求途中硬点开关也不该切过去、放开按钮：" + JSON.stringify(l));
      await page.$eval('.modal [data-x="ok"]', x => x.click());
      assert(await toastHas("已创建并导入", 4000), "这一次创建应成功");
      await modalGone(); await settle(300);
      const posts = reqs.slice(before).filter(r => r.m === "POST" && r.u === "/api/tunnel-accounts/natfrp/tunnels").length;
      const n1 = (await taState({})).overviews.natfrp.tunnels.length;
      assert(posts === 1 && n1 === n0 + 1, `只该发一次创建：发了 ${posts} 次，平台隧道 ${n0} → ${n1}`);
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await page.unroute(pattern, slow); await resetTA(); }
  });

  // 樱花创建框：平台上没建成的，「这是公开服务器」跟着按钮放开；平台上已经建好的（created），开关也锁着。以前建好以后还能切，
  // 没有公开服节点的账户切过去，「平台上的隧道已建好……」那句会被「没有可用的公开服专用节点」盖掉，底下却还是「关闭，去导入」（A57）
  await run("平台账户：樱花创建框没建成时「这是公开服务器」放开，平台上已建好以后锁着，「平台上的隧道已建好」那句不被盖掉", async () => {
    await taState({ tunnelAccounts: { reset: true, natfrp: true, saveFail: true, overviews: { natfrp: { nodes: [
      { id: 11, name: "北京联通", description: "仅 TCP", canCreate: true, tcp: true, udp: false, publicGameAllowed: false },
    ] } } }, tunnelsReset: true });
    const dlg = () => page.evaluate(() => {
      const q = s => document.querySelector(".modal " + s) || {};
      return { ok: q('[data-x="ok"]').disabled, pub: q("#tn-cr-public").disabled, checked: q("#tn-cr-public").checked,
        err: (q("#tn-cr-err").textContent || "").trim(), goHidden: q('[data-x="go-import"]').hidden };
    });
    try {
      errors.length = 0;
      await openOverview("natfrp");
      await page.click('#tn-acc-panel [data-acc-create="natfrp"]');
      await page.waitForSelector("#tn-cr-public");
      // 一：本地端口填 0，平台上没建成：按钮和开关都放开，改了再试
      await page.fill("#tn-cr-lport", "0");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.provider === "natfrp" && v.localPort === 0);
      await page.waitForFunction(() => /\S/.test((document.querySelector(".modal #tn-cr-err") || {}).textContent || ""), null, { timeout: 3000 }).catch(() => {});
      let d = await dlg();
      assert(!d.ok && !d.pub && d.goHidden, "平台上没建成：按钮和「这是公开服务器」都应放开：" + JSON.stringify(d));
      // 二：平台上建好了、本机写不进磁盘：开关锁住，硬点也不切过去，报错那句还在
      await page.fill("#tn-cr-lport", "25565");
      await page.click('.modal [data-x="ok"]');
      await taWait("lastCreate", v => v.provider === "natfrp" && v.localPort === 25565);
      await page.waitForFunction(() => ((document.querySelector(".modal #tn-cr-err") || {}).textContent || "").includes("平台上的隧道已建好"), null, { timeout: 3000 }).catch(() => {});
      d = await dlg();
      assert(d.pub && !d.checked && !d.goHidden && d.err.includes("平台上的隧道已建好"), "平台上建好了：「这是公开服务器」应锁住、报错那句在：" + JSON.stringify(d));
      await page.$eval(".modal #tn-cr-public", x => x.click());
      await settle(200);
      d = await dlg();
      assert(d.pub && !d.checked && !d.goHidden && d.err.includes("平台上的隧道已建好") && !d.err.includes("没有可用的公开服专用节点"),
        "硬点开关也不该切过去、盖掉「平台上的隧道已建好」：" + JSON.stringify(d));
      await page.click('.modal [data-x="go-import"]');
      await modalGone();
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  // 「跟随服务器启动」默认不勾（A62，以前三处都默认勾着）：勾上以后每次开服都自动映射到公网，要人自己决定。
  // 手动添加、导入平台隧道、在平台上创建三处一样，不勾时请求里是 false
  await run("隧道：添加、导入、平台上创建三处的「跟随服务器启动」默认都不勾，不勾时请求里是 false", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await musicState({ music: { reset: true } });
      await openOverview("openfrp");
      // 手动添加
      await page.click("#tn-toggle");
      assert(await page.$eval("#tn-auto", e => e.checked) === false, "添加表单里默认应不勾");
      await page.selectOption("#tn-provider", "natfrp");
      await page.fill("#tn-name", "默认不跟随"); await page.fill("#tn-cred", "fake-key-123"); await page.fill("#tn-proxy", "123456");
      await page.click("#tn-add");
      assert(await toastHas("隧道已添加"), "应添加成功");
      const body = (await musicState({})).music.tunnelBody;
      assert(body && body.autoStart === false, "添加的请求体：" + JSON.stringify(body));
      // 导入平台隧道
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector(".modal #tn-imp-auto");
      assert(await page.$eval(".modal #tn-imp-auto", e => e.checked) === false, "导入对话框里默认应不勾");
      await page.click('.modal [data-x="ok"]');
      const imp = await taWait("lastImport", v => v.proxyId === "1001");
      assert(imp && imp.autoStart === false, "导入的请求体：" + JSON.stringify(imp));
      await modalGone(); await settle(300);
      // 在平台上创建
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector(".modal #tn-cr-auto");
      assert(await page.$eval(".modal #tn-cr-auto", e => e.checked) === false, "创建对话框里默认应不勾");
      await page.fill("#tn-cr-name", "mc_nofollow");
      await page.click('.modal [data-x="ok"]');
      const cr = await taWait("lastCreate", v => v.name === "mc_nofollow");
      assert(cr && cr.autoStart === false, "创建的请求体：" + JSON.stringify(cr));
      await modalGone();
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); await resetMusic(); }
  });

  // 手动添加隧道请求途中不许再点（A57）：以前连点两下会发两次，加出两条一样的隧道
  await run("隧道：手动添加的按钮请求途中不能再点，连点两下只发一次", async () => {
    await taState({ tunnelAccounts: { reset: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await musicState({ music: { reset: true } });
      await page.goto(url("#/tunnels")); await settle();
      await page.click("#tn-toggle");
      await page.selectOption("#tn-provider", "natfrp");
      await page.fill("#tn-name", "连点测试"); await page.fill("#tn-cred", "fake-key-123"); await page.fill("#tn-proxy", "123456");
      const before = reqs.length;
      await page.evaluate(() => { const b = document.querySelector("#tn-add"); b.click(); b.click(); });
      assert(await toastHas("隧道已添加"), "应添加成功");
      await settle(300);
      const posts = reqs.slice(before).filter(r => r.m === "POST" && r.u === "/api/tunnels").length;
      assert(posts === 1, "连点两下只该发一次：" + posts);
      assert(await page.$eval("#tn-add", b => !b.disabled), "请求结束后按钮应恢复");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); await resetMusic(); }
  });

  // 手动新增走 toastErr → errSummary：后端给的 hint 要被接进正文（这一条也就验了 hint 守 errSummary 的规矩）
  await run("隧道：tunnels.json 写不进去时手动新增报「无法添加隧道」接后端那句人话，详情是原文", async () => {
    await taState({ tunnelAccounts: { reset: true, saveFail: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await musicState({ music: { reset: true } });
      await page.goto(url("#/tunnels")); await settle();
      await page.click("#tn-toggle");
      await page.selectOption("#tn-provider", "natfrp");
      await page.fill("#tn-name", "樱花游戏"); await page.fill("#tn-cred", "k"); await page.fill("#tn-proxy", "123456");
      await page.click("#tn-add");
      const HINT = "隧道配置写不进磁盘，这次没有保存。请检查磁盘是否已满、MineDeck 所在的文件夹是否只读";
      assert(await toastHas("无法添加隧道：" + HINT), "提示正文应是「无法添加隧道：」加后端那句人话");
      assert(!(await toastHas("隧道已添加", 300)), "写不进去时不该提示已添加");
      const raw = await toastDetail("无法添加隧道");
      assert(raw.includes("没能写进磁盘") && raw.includes("Access is denied"), "「详情」里应是完整原文：" + raw);
      assert(await page.$("#tn-name") && await page.$eval("#tn-name", e => e.value) === "樱花游戏", "失败时表单应留着，填的东西不丢");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); await resetMusic(); }
  });

  // tunnels.json 在却没读出来（被别的程序占着、内容坏了）：以前当成空列表，再加一条就把原文件连同隧道和凭据整份盖掉。
  // 现在列表那里报出来（正文接后端那句，分两种情况），下一行说清现在不能保存，表单收起；读出来以后自己恢复
  await run("隧道：tunnels.json 没读出来时报原因、说清现在不能保存、收起表单，读出来以后自动恢复", async () => {
    const setLoad = v => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ tunnelsLoadFail: x }) }), v);
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await setLoad("locked");
      await page.goto(url("#/tunnels")); await settle();
      await page.waitForSelector("#tun-load-err");
      const box = await page.evaluate(() => {
        const b = document.querySelector("#tun-load-err");
        return { text: b.textContent.replace(/\s+/g, " "), raw: b.querySelector("[data-err-raw]").getAttribute("data-err-raw"),
          form: document.querySelector("#tn-form").hidden, toggle: document.querySelector("#tn-toggle").hidden,
          rows: document.querySelectorAll("#tun-list [data-tun]").length, empty: !!document.querySelector(".tun-empty"),
          summary: document.querySelector("#tun-summary").textContent };
      });
      const LOCKED = "MineDeck 文件夹里的 tunnels.json 读不出来，可能被别的程序占着。关掉占用它的程序后会自动重读";
      assert(box.text.includes("无法显示隧道：" + LOCKED), "正文应是结论接后端那句：" + box.text);
      assert(box.text.includes("读出来之前不能添加、修改或删除隧道"), "要说清现在不能保存：" + box.text);
      assert(!box.text.includes("another process") && box.raw.includes("being used by another process"), "正文不该是英文原文，复制的应是完整原文：" + box.raw);
      assert(box.form && box.toggle, "没读出来时表单和「添加隧道」都该收起来：" + JSON.stringify(box));
      assert(!box.rows && !box.empty && !box.summary, "不该显示成「还没有隧道」或「0 条隧道」：" + JSON.stringify(box));
      // 平台那边的导入也被拒，提示接后端那句（后端在找平台之前就拦下）
      await page.click('[data-acc-overview="openfrp"]');
      await page.waitForSelector('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.click('#tn-acc-panel [data-acc-import="openfrp"][data-proxy="1001"]');
      await page.waitForSelector('.modal [data-x="ok"]');
      await page.click('.modal [data-x="ok"]');
      assert(await toastHas(LOCKED), "没读出来时导入应提示后端那句");
      assert(!(await toastHas("已导入到 MineDeck", 300)), "没读出来时不该提示已导入");
      const cancel = await page.$('.modal [data-x="cancel"]');
      if (cancel) await cancel.click();
      await modalGone();
      // 每 4 秒的刷新：原因没变就不重画（重试键还是原来那个，点到一半不会被换掉）
      await page.evaluate(() => { document.querySelector("#tun-load-retry").dataset.mark = "1"; });
      await page.waitForTimeout(4600);
      assert(await page.$eval("#tun-load-retry", b => b.dataset.mark === "1"), "原因没变时每次刷新都重画了报错框");
      // 点「重试」还是没读出来：提示一句，按钮恢复可点
      await page.click("#tun-load-retry");
      assert(await toastHas("隧道配置还是没读出来"), "重试仍没读出来应提示一句");
      assert(await page.$eval("#tun-load-retry", b => !b.disabled), "重试键应恢复可点");
      // 原因变了（被占着 → 内容坏了）：换成那种情况的说法
      await setLoad("corrupt");
      await page.click("#tun-load-retry");
      const CORRUPT = "MineDeck 文件夹里的 tunnels.json 内容坏了。用记事本修好它，或者改名挪走后从空列表开始";
      await page.waitForFunction(t => ((document.querySelector("#tun-load-err") || {}).textContent || "").includes("无法显示隧道：" + t), CORRUPT, { timeout: 3000 });
      // 读出来了：不点任何东西，4 秒一次的刷新就画出列表，「添加隧道」回来
      await setLoad("");
      await page.waitForSelector("#tun-list [data-tun]", { timeout: 6000 });
      const after = await page.evaluate(() => ({ err: !!document.querySelector("#tun-load-err"), toggle: document.querySelector("#tn-toggle").hidden,
        summary: document.querySelector("#tun-summary").textContent }));
      assert(!after.err && !after.toggle && after.summary.includes("条隧道"), "读出来以后应照常显示列表和「添加隧道」：" + JSON.stringify(after));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await setLoad(""); await resetTA(); }
  });

  await run("平台账户：OpenFrp 创建隧道默认给英文名，名称不合规时在对话框里提示且不发请求", async () => {
    await taState({ tunnelAccounts: { reset: true, openfrp: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await openOverview("openfrp");
      await page.click('#tn-acc-panel [data-acc-create="openfrp"]');
      await page.waitForSelector("#tn-cr-name");
      const dlg = await page.evaluate(() => ({
        name: document.querySelector("#tn-cr-name").value,
        hint: (document.querySelector("#tn-cr-name-hint") || {}).textContent || "",
        pub: !!document.querySelector("#tn-cr-public"),
        rportShown: !document.querySelector("#tn-cr-rport").closest(".field").hidden,
      }));
      assert(/^minedeck_[a-z0-9]{4}$/.test(dlg.name), "默认隧道名应是 minedeck_ 加 4 位小写字母数字：" + dlg.name);
      assert(dlg.hint === "OpenFrp 的隧道名只能用英文字母、数字、下划线和连字符" && !dlg.pub && dlg.rportShown, "对话框：" + JSON.stringify(dlg));
      // 中文名：对话框里提示，不发请求
      const before = reqs.length;
      await page.fill("#tn-cr-name", "我的隧道");
      await page.click('.modal [data-x="ok"]'); await settle(400);
      const bad = await page.evaluate(() => ({
        open: !!document.querySelector("#tn-cr-name"),
        err: (document.querySelector("#tn-cr-err") || {}).textContent || "",
      }));
      assert(bad.open && bad.err.includes("OpenFrp 的隧道名只能用英文字母、数字、下划线和连字符"), "应在对话框里提示名称不合规：" + JSON.stringify(bad));
      assert(taReqs(before).length === 0, "名称不合规时不该发请求：" + taReqs(before).join(", "));
      assert((await taState({})).lastCreate === null, "仿真后端不该收到创建请求：" + JSON.stringify((await taState({})).lastCreate));
      // 改回英文：提示清掉，创建成功
      await page.fill("#tn-cr-name", "mc_friends-1");
      assert(await page.$eval("#tn-cr-err", e => e.textContent) === "", "改了名称后提示应清掉");
      await page.click('.modal [data-x="ok"]');
      const last = await taWait("lastCreate", v => !!v.name);
      assert(last && last.provider === "openfrp" && last.name === "mc_friends-1" && last.nodeId === 1 && last.type === "tcp" &&
        last.localPort === 25565 && last.remotePort === 25566 && last.purpose === "game" && !("publicServer" in last),
        "创建请求：" + JSON.stringify(last));
      assert(await toastHas("已创建并导入"), "应提示已创建并导入");
      await modalGone();
      assert(await page.$("#tn-cr-name") === null, "创建成功后对话框应关闭");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：樱花创建隧道勾了「这是公开服务器」只列公开服专用节点，请求带 publicServer", async () => {
    const nodeNames = () => page.$$eval("#tn-cr-node option", os => os.map(o => o.textContent.trim()));
    const openCreate = async () => {
      await openOverview("natfrp");
      await page.click('#tn-acc-panel [data-acc-create="natfrp"]');
      await page.waitForSelector("#tn-cr-public");
    };
    try {
      errors.length = 0;
      await taState({ tunnelAccounts: { reset: true, natfrp: true }, tunnelsReset: true });
      await openCreate();
      const dlg = await page.evaluate(() => ({
        name: document.querySelector("#tn-cr-name").value,
        ofHint: !!document.querySelector("#tn-cr-name-hint"),
        label: document.querySelector("#tn-cr-public").closest(".switch").textContent.trim(),
        desc: (document.querySelector("#tn-cr-public-desc") || {}).textContent || "",
        checked: document.querySelector("#tn-cr-public").checked,
        rportHidden: document.querySelector("#tn-cr-rport").closest(".field").hidden,
      }));
      assert(/^minedeck_[a-z0-9]{4}$/.test(dlg.name) && !dlg.ofHint && dlg.rportHidden, "樱花的对话框：" + JSON.stringify(dlg));
      assert(dlg.label === "这是公开服务器" && !dlg.checked && dlg.desc.includes("用错节点会被封号且不退款") && dlg.desc.includes("只和熟人小范围联机可以不勾"),
        "公开服务器开关和说明：" + JSON.stringify(dlg));
      const allNodes = await nodeNames();
      assert(allNodes.length === 2 && allNodes[0].startsWith("上海多线") && allNodes[1].startsWith("北京联通"), "没勾时列出全部可创建的节点：" + allNodes.join(" | "));
      await page.click("#tn-cr-public"); await settle(200);
      const pubNodes = await nodeNames();
      assert(pubNodes.length === 1 && pubNodes[0].startsWith("上海多线"), "勾了以后只剩公开服专用节点：" + pubNodes.join(" | "));
      await page.click('.modal [data-x="ok"]');
      const last = await taWait("lastCreate", v => !!v.name);
      assert(last && last.provider === "natfrp" && last.publicServer === true && last.nodeId === 10 && /^minedeck_[a-z0-9]{4}$/.test(last.name),
        "创建请求应带 publicServer: true 和公开服节点：" + JSON.stringify(last));
      assert(last.remotePort === 0, "樱花没有公网端口输入，请求里公网端口应为 0、不带隐藏框的默认值：" + JSON.stringify(last));
      assert(await toastHas("已创建并导入"), "应提示已创建并导入");
      await modalGone();

      // 没勾：请求里 publicServer 为 false，普通节点也能选
      await taState({ tunnelAccounts: { reset: true, natfrp: true }, tunnelsReset: true });
      await openCreate();
      await page.selectOption("#tn-cr-node", "11");
      await page.click('.modal [data-x="ok"]');
      const plain = await taWait("lastCreate", v => !!v.name);
      assert(plain && plain.publicServer === false && plain.nodeId === 11, "没勾时的创建请求：" + JSON.stringify(plain));
      await modalGone();

      // 一个公开服专用节点都没有：勾上后提示没有可用节点，「创建并导入」点不了；取消勾选恢复
      await taState({ tunnelAccounts: { reset: true, natfrp: true, overviews: { natfrp: { nodes: [
        { id: 11, name: "北京联通", description: "仅 TCP", canCreate: true, tcp: true, udp: false, publicGameAllowed: false },
      ] } } }, tunnelsReset: true });
      await openCreate();
      await page.click("#tn-cr-public"); await settle(200);
      const none = await page.evaluate(() => ({
        options: document.querySelectorAll("#tn-cr-node option").length,
        okDisabled: document.querySelector('.modal [data-x="ok"]').disabled,
        err: document.querySelector("#tn-cr-err").textContent,
      }));
      assert(none.options === 0 && none.okDisabled && none.err.includes("没有可用的公开服专用节点"), "没有公开服节点时：" + JSON.stringify(none));
      await page.click("#tn-cr-public"); await settle(200);
      const back = await page.evaluate(() => ({
        options: document.querySelectorAll("#tn-cr-node option").length,
        okDisabled: document.querySelector('.modal [data-x="ok"]').disabled,
        err: document.querySelector("#tn-cr-err").textContent,
      }));
      assert(back.options === 1 && !back.okDisabled && back.err === "", "取消勾选后恢复：" + JSON.stringify(back));
      assert((await taState({})).lastCreate === null, "这一段没有点创建，不该有创建请求");
      await page.click('.modal [data-x="cancel"]'); await modalGone();
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });

  await run("平台账户：断开樱花时说明已导入的隧道里还留着访问密钥", async () => {
    try {
      errors.length = 0;
      await taState({ tunnelAccounts: { reset: true, openfrp: true, natfrp: true }, tunnelsReset: true });
      await page.goto(url("#/tunnels")); await settle(600);
      const ask = async provider => {
        await page.click(`[data-acc-disconnect="${provider}"]`);
        await page.waitForSelector('.modal [data-x="cancel"]');
        const body = await page.$eval(".modal .m-body", e => e.textContent.replace(/\s+/g, " ").trim());
        await page.click('.modal [data-x="cancel"]'); await modalGone();
        return body;
      };
      const sk = await ask("natfrp");
      assert(sk === "会清除本机保存的樱花访问密钥。已导入到 MineDeck 的樱花隧道里仍保存着同一把访问密钥，要彻底清除请删除这些隧道，必要时到樱花官网重置访问密钥。", "樱花的断开文案：" + sk);
      const of = await ask("openfrp");
      assert(of === "会清除本机保存的平台凭据。已导入到 MineDeck 的隧道不会删除。", "OpenFrp 的断开文案：" + of);
      const st = await taState({});
      assert(st.openfrp === true && st.natfrp === true, "取消后不该真的断开：" + JSON.stringify({ openfrp: st.openfrp, natfrp: st.natfrp }));
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); }
  });
  // ==================== 平台账户结束 ====================

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
      assert((await page.$("#lan-restart")) === null && (await lastToast()) === "已关闭手机远程管理", "关闭后：" + await lastToast()); // beta.3：开关按新口径改叫「手机远程管理」
      // 已有密码：直接开
      t0 = reqs.length;
      await flip();
      ps = lanPuts(t0);
      assert(ps.length === 1 && ps[0].listenLan === true && !("lanPassword" in ps[0]) && (await sw()), "有密码时应直接保存：" + JSON.stringify(ps));
      assert((await page.$("#lan-restart")) !== null, "开启后应提示重启");
      // 只改密码：只发密码，不带页面上的开关（另一个窗口刚关掉的话，带上就又打开了，Codex 复审 v3.2.0 第二轮）
      await page.fill("#lan-pw", "pw-654321");
      t0 = reqs.length;
      await page.press("#lan-pw", "Enter"); await settle(400);
      ps = lanPuts(t0);
      assert(ps.length === 1 && !("listenLan" in ps[0]) && ps[0].lanPassword === "pw-654321" && (await lastToast()) === "访问密码已保存" && (await sw()), "回车保存密码：" + JSON.stringify(ps) + " " + await lastToast());
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
      assert(!(await sw()) && (await lastToast()).includes("无法开启手机远程管理：请先设置访问密码"), "后端拒绝时应拨回：" + await lastToast());

      // 这次启动已在局域网上：关闭即时生效（不再放行局域网），再开也不用重启
      await mockState({ listenLan: true });
      await page.goto(url("#/settings/remote")); await settle();
      assert((await page.$("#qr-box svg.qr")) !== null && (await page.$("#lan-restart")) === null, "已生效时应显示二维码、没有重启提示");
      await flip();
      assert((await lastToast()).includes("现在起打不开") && (await page.$("#qr-box")) === null, "关闭已生效的局域网访问：" + await lastToast());
      await flip();
      assert((await lastToast()) === "已开启手机远程管理" && (await page.$("#qr-box svg.qr")) !== null, "重新开启（这次启动本就在监听）：" + await lastToast());
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
        ["工具箱", "#/toolbox"], ["工具箱 → 离线皮肤包", "#/toolbox/skin-pack"], ["工具箱 → 导出整合包", "#/toolbox/mrpack-export"],
        ...["appearance", "download", "storage", "java", "remote", "notify", "ai", "startup", "about"].map(s => ["设置 → " + s, "#/settings/" + s]),
        // beta.3 R06：按新的标签和子页逐页查（旧地址会跳到这些地方）
        ...["console", "players", "world/backups", "world/map", "addons", "tasks", "ext/gameai", "settings/common", "settings/raw", "settings/jvm"].map(t => ["生存服 → " + t, "#/inst/" + inst + "/" + t]),
        ...["console", "players", "world/backups", "world/map", "addons", "tasks", "ext/music", "ext/companion", "settings/common", "settings/jvm"].map(t => ["整合包 → " + t, PACK_I + t])];
      for (const [label, h] of pages) { await page.goto(url(h)); await settle(700); await check(label); }
      await page.goto(url("#/inst/" + inst + "/addons")); await settle(400);
      await page.click("#mc-go"); await settle(500); await check("插件页搜索结果");
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
      // 插件页（beta.3 R10：「资源」标签合进了插件 / 模组页，它的「安装」换成待装清单的「加入清单」）：
      // 已安装那几行的停用 / 启用、删除带文件名；热门里每个按钮带插件名，装过的叫「已安装 X」，加进清单以后跟着变成「已在清单 X」
      await page.goto(url("#/inst/" + inst + "/addons")); await page.waitForSelector("#mc-hits .mc-hit"); await settle(600);
      const files = await page.$$eval(".ad-item", rs => rs.map(r => r.dataset.file));
      for (const f of files) assert(await byName("button", "删除 " + f).count() === 1, "删除按钮的名字没带文件名：" + f);
      const titles = await page.$$eval(".mc-add", bs => bs.map(b => b.dataset.t));
      const adds = (await axTree()).filter(n => n.role === "button" && /^(加入清单|已安装|已在清单) /.test(n.name)).map(n => n.name);
      assert(titles.length > 3 && adds.length === titles.length && new Set(adds).size === adds.length && adds.includes("已安装 LuckPerms"), "热门里按钮的名字：" + adds.slice(0, 6).join("、"));
      await byName("button", "加入清单 Vault").click(); await settle(500);
      assert(await byName("button", "已在清单 Vault").count() === 1, "加进清单以后名字应跟着按钮上的字变成「已在清单 Vault」");
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
      assert(t.h1 === "服务器已接管" && t.label === "接管完成" && t.foot.includes("进入控制台") && t.href === "#/inst/" + encodeURIComponent("朋友的整合包"), "任务页：" + JSON.stringify(t));
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
      assert(v.what.includes("曾由 MineDeck 管理") && !v.noWorldHidden && v.noWorld.includes("没看到世界"), "说明：" + JSON.stringify(v));
      // 代理端本来就没有存档：不提
      await page.selectOption("#ad-core", "velocity");
      assert(await page.$eval("#ad-noworld", x => x.hidden), "代理端不该提示没看到存档");
      await page.selectOption("#ad-core", "paper");
      assert(v.mem === "4096", "没读到原来的内存时默认 4 GB：" + v.mem);
      await page.type("#ad-dir", "x"); await settle(200);
      assert(await page.$eval("#ad-form", f => f.innerHTML === "") && await page.$eval("#ad-go", b => b.disabled), "改了文件夹，旧的识别结果还留着");
      await mockState({ adoptScan: null });
    });

    // beta.3 R16：Java 的选择进了保存条，不再单独有「更换 Java」按钮；这条的点法跟着改成点保存条的「保存」
    await run("设置 → 启动与内存：能给服务器换 Java——列出 MineDeck 下载的、本机装的和可以现下的，标出现在用的；已有的直接换，本机没有的去任务页下载", async () => {
      await mockState({ instReset: true });
      const pack = encodeURIComponent("机械动力整合包");
      await page.goto(url(`#/inst/${pack}/jvm`)); await settle(900);
      let o = await page.evaluate(() => ({ opts: [...$("#jm-java").options].map(x => x.textContent), sel: $("#jm-java").selectedOptions[0].textContent, off: $('[data-sb="save"]').disabled }));
      assert(o.sel === "Java 17（MineDeck 下载的） · 现在用的" && o.off, "现在用的没标出来，或者没改就能点保存：" + JSON.stringify(o));
      assert(o.opts.some(x => x.startsWith("Java 8（本机装的 1.8.0_421：C:\\Program Files\\Java\\")) && o.opts.includes("Java 25（本机没有，下载一份，约 50 MB）") &&
        !o.opts.some(x => x.startsWith("Java 21（本机没有")), "选项：" + JSON.stringify(o.opts));
      const pick = async text => page.selectOption("#jm-java", { index: await page.$eval("#jm-java", (s, t) => [...s.options].findIndex(x => x.textContent.startsWith(t)), text) });
      await pick("Java 8（");
      assert(!(await page.$eval('[data-sb="save"]', b => b.disabled)) && (await page.textContent(".sb-text")).includes("1 项修改未保存"), "选了别的 Java，保存条没亮");
      await page.click('[data-sb="save"]'); await settle(500);
      let st = await mockState({});
      assert(st.lastJava && st.lastJava.inst === "机械动力整合包" && st.lastJava.major === 8 && st.lastJava.path === "C:\\Program Files\\Java\\jre1.8.0_421\\bin\\java.exe", "换 Java 的请求：" + JSON.stringify(st.lastJava));
      o = await page.evaluate(() => ({ sel: $("#jm-java").selectedOptions[0].textContent, off: $('[data-sb="save"]').disabled, badge: $("#d-java").textContent,
        marked: [...$("#jm-java").options].filter(x => x.textContent.includes("现在用的")).length, toasts: [...document.querySelectorAll(".toast")].map(x => x.textContent) }));
      assert(o.sel.startsWith("Java 8（") && o.sel.endsWith("现在用的") && o.marked === 1 && o.off && o.badge === "Java 8", "换完之后：" + JSON.stringify(o));
      assert(o.toasts.some(x => x.includes("已换成 Java 8") && x.includes("下次启动生效")), "提示：" + JSON.stringify(o.toasts));
      // 本机没有的版本：去任务页，说的是「更换 Java」，完成后回到这台服务器的「内存 / 启动」
      await pick("Java 25（");
      await page.click('[data-sb="save"]'); await settle(1200);
      st = await mockState({});
      assert(st.lastJava.major === 25 && st.lastJava.path === "", "下载的请求不带路径：" + JSON.stringify(st.lastJava));
      const t = await page.evaluate(() => ({ url: location.hash, h1: $(".page-head h1").textContent, label: $("#t-label").textContent, link: $("#t-foot a").textContent.trim(), href: $("#t-foot a").getAttribute("href") }));
      assert(t.url === "#/task/task-java" && t.h1 === "Java 已换好" && t.label === "Java 已换好" && t.link === "完成，回到服务器" && t.href === `#/inst/${pack}/settings/jvm`, "任务页：" + JSON.stringify(t));
      await mockState({ instReset: true });
    });

    // beta.3 R05 起所有服务器默认都只移出列表（以前自己建的默认连文件夹一起删），取消键叫「取消」，打开开关后按钮写「删除服务器和文件夹」；
    // U28：移出后的提示写出文件夹在哪。这条用例的期望按新的做法改了，新做法的细节见「beta.3 P0」那一块
    await run("删除：不打开「同时删除文件夹」时按钮写「移出列表」；自己建的和接管进来的都默认不删文件夹，接管进来的写出原来的文件夹，确认后只移出列表", async () => {
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
      assert(!d.checked && d.ok === "移出列表" && d.focused === "取消" && d.hint.includes("接管已有服务器") && !d.hint.includes("下次启动会重新出现"), "自己新建的：" + JSON.stringify(d));
      await page.check("#modal-root input[data-k=files]");
      assert(await page.textContent("#modal-root [data-x=ok]") === "删除服务器和文件夹", "打开开关后按钮应写「删除服务器和文件夹」");
      await page.click('#modal-root [data-x="cancel"]'); await settle(250);
      // 接管进来的：同样默认不删文件
      await mockState({ inst: { name: "机械动力整合包", adopted: true, dir: "E:\\MCServers\\create-pack" } });
      d = await open();
      assert(!d.checked && d.ok === "移出列表" && d.focused === "取消" && d.hint.includes("接管进来的") && d.hint.includes("E:\\MCServers\\create-pack"), "接管进来的：" + JSON.stringify(d));
      await page.click('#modal-root [data-x="ok"]'); await settle(500);
      const st = await mockState({});
      assert(st.lastDelete && st.lastDelete.inst === "机械动力整合包" && st.lastDelete.files === false, "删除的请求：" + JSON.stringify(st.lastDelete));
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("移出列表") && x.includes("文件夹还在：E:\\MCServers\\create-pack")), "提示：" + JSON.stringify(toasts));
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
        { id: "p3", path: "mods/same-host-2.jar", reason: "host", host: "files.example.net", note: "下载地址不在 MineDeck 允许的站点列表里", urls: ["https://files.example.net/same-host-2.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] },
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
      // 只剩跳过的：不再画绿勾说「都处理完了」（盲测 11-16：读着像没事了），写明跳过了几个、没有装上
      assert(/跳过了 \d+ 个文件，它们没有装上/.test(await page.textContent("#t-pending")), "收尾应写跳过了几个、没有装上"); // beta.3：界面上不再说「待决清单」
      // 从没有过待决文件的普通任务页不显示任何待决面板（曾误显示「已全部处理完」卡片）
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [{ id: "task-plain", title: "创建 Paper 服务器", ended: true }] }) }));
      await page.goto(url("#/task/task-plain")); await settle();
      assert(((await page.$eval("#t-pending", e => e.textContent)) || "").trim() === "", "没有待决过的任务不该出现待决面板");
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [], trustedHosts: [] }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("添加模组：项目页只用接口给的类型和 slug", async () => {
    await page.goto(url("#/inst/" + encodeURIComponent("机械动力整合包") + "/addons"));
    await page.waitForSelector("#mc-hits .mc-add", { timeout: 8000 });
    const hrefs = await page.$$eval("#mc-hits a[href]", as => as.map(a => a.getAttribute("href")));
    assert(hrefs.length > 0 && hrefs.every(h => /^https:\/\/modrinth\.com\/mod\/[A-Za-z0-9._-]+$/.test(h)), "项目页：" + JSON.stringify(hrefs));
  });

  await run("没装上的文件：全选只选可下载项且不默认同意，确认风险是另一步，501 项拒绝", async () => {
    const task = {
      id: "task-all", title: "导入整合包 很多外部文件", ended: true, result: "机械动力整合包",
      pending: [
        { id: "p1", path: "mods/a.jar", reason: "host", host: "files.example.net", note: "下载地址不在允许列表", urls: ["https://files.example.net/a.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] },
        { id: "p2", path: "mods/b.jar", reason: "host", host: "cdn.example.org", note: "下载地址不在允许列表", urls: ["https://cdn.example.org/b.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] },
        { id: "p3", path: "mods/c.jar", reason: "nohash", note: "整合包没有提供这个文件的校验值，不能自动下载", options: ["skip", "local"] },
        // 站点不在列表里、又没有校验值：后端给 trust 选项，但真去下载会被拒，全选不该选它
        { id: "p4", path: "mods/d.jar", reason: "host", host: "files.example.net", note: "下载地址不在允许列表", urls: ["https://files.example.net/d.jar"], options: ["local", "skip", "trust"] },
        // 用户已经选了「用本地文件代替」的，全选不替人改
        { id: "p5", path: "mods/e.jar", reason: "host", host: "files.example.net", note: "下载地址不在允许列表", urls: ["https://files.example.net/e.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] },
      ],
    };
    try {
      await page.evaluate(t => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [t], trustedHosts: [] }) }), task);
      await page.goto(url("#/task/task-all")); await settle();
      await page.check('#t-pending .pnd-item[data-id="p5"] input[value="local"]'); await settle(100);
      await page.click("#pnd-trust-all"); await settle();
      const picked = await page.$$eval("#t-pending .pnd-item", els => els.map(e => ({
        id: e.dataset.id,
        act: (e.querySelector("input[type=radio]:checked") || {}).value || "",
        ack: !!(e.querySelector(".pnd-ackbox") || {}).checked,
      })));
      assert(JSON.stringify(picked) === JSON.stringify([
        { id: "p1", act: "trust", ack: false },
        { id: "p2", act: "trust", ack: false },
        { id: "p3", act: "", ack: false },
        { id: "p4", act: "", ack: false },
        { id: "p5", act: "local", ack: false },
      ]), "全选范围（不选没有校验值的、不改已经选好的）：" + JSON.stringify(picked));
      assert((await page.textContent("#t-pending")).includes("不会把外部站点放进默认允许列表"), "应写明不会放进默认允许列表");
      assert((await page.textContent("#t-pending")).includes("没有提供这个文件的校验值"), "缺校验的说明还在");
      await page.click("#pnd-ack-all"); await settle();
      const acked = await page.$$eval("#t-pending .pnd-item", els => els.map(e => ({
        id: e.dataset.id,
        ack: !!(e.querySelector(".pnd-ackbox") || {}).checked,
        shown: !!(e.querySelector("label.pnd-ack") && !e.querySelector("label.pnd-ack").hidden),
      })));
      assert(JSON.stringify(acked) === JSON.stringify([
        { id: "p1", ack: true, shown: true },
        { id: "p2", ack: true, shown: true },
        { id: "p3", ack: false, shown: false },
        { id: "p4", ack: false, shown: false },
        { id: "p5", ack: false, shown: false },
      ]), "确认风险只勾已选的信任项：" + JSON.stringify(acked));
      const tooMany = await page.evaluate(async () => {
        const items = Array.from({ length: 501 }, (_, i) => ({ id: "x" + i, act: "skip" }));
        const r = await fetch("/api/instances/" + encodeURIComponent("机械动力整合包") + "/import-pending", {
          method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ items }),
        });
        return r.status + " " + await r.text();
      });
      assert(tooMany.includes("400") && tooMany.includes("最多 500 项"), "501 应拒绝：" + tooMany);
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [], trustedHosts: [] }) }));
    }
  });

  await run("待决面板：选了用本地文件代替但没给路径时拦下，给路径后成功", async () => {
    const task = {
      id: "task-im2", title: "导入整合包 魔改模组包", ended: true, result: "机械动力整合包",
      pending: [{ id: "p1", path: "mods/modded.jar", reason: "host", host: "files.example.net", urls: ["https://files.example.net/modded.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] }],
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
    // beta.3 R06：「内存 / 启动」成了「设置 → 启动与内存」，地址是 settings/jvm
    assert(hint.includes("Java 25") && hint.includes("Java 17") && hint.includes("「设置 → 启动与内存」"), "应提示模组要求与现状：" + hint);
    await page.click("#d-java-hint a"); await settle();
    assert(page.url().includes("/settings/jvm"), "入口应到「设置 → 启动与内存」：" + page.url());
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
      assert(m.title.startsWith("导出交接包") && m.full === false && m.go.includes("导出交接包") && !m.go.includes("开始打包"), "弹窗状态：" + JSON.stringify(m));
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
      assert(t.h1 === "交接包已导出" && t.go === "#/instances", "导出任务页：" + JSON.stringify(t)); // beta.3 R13：完成后标题换成完成态
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
      assert(t.h1 === "交接包已导入" && t.steps.includes("解包服务器文件") && t.href === "#/inst/" + encodeURIComponent("接手服"), "任务页：" + JSON.stringify(t));
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

  // beta.3 R10：「待装清单」标签合进了「插件 / 模组」（addons，模组服上叫「模组」），旧地址跳过去
  await run("实例详情「模组」标签里的待装清单：vanilla 没有这个标签；加入清单 → 查前置缺项 → 勾选补上 → 移除清空", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url("#/inst/" + encodeURIComponent("原版快照测试"))); await settle(600);
      assert((await page.$('.tabs .tab[data-t="addons"]')) === null, "原版实例不应有插件 / 模组标签");
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      assert((await page.$('.tabs .tab[data-t="addons"].cur')) !== null, "模组实例的旧待装清单地址没落到插件 / 模组标签");
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
      await page.click("#mc-install");
      await page.waitForSelector('.modal [data-x="ok"]', { timeout: 5000 });
      await page.click('.modal [data-x="ok"]');
      await settle(900);
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("无法开始安装：待装清单和你刚才看到的不一样了")), "车变了应拦下并说明：" + JSON.stringify(toasts));
      assert(!page.url().includes("#/task/"), "被拦下时不该跳到任务页：" + page.url());
      assert((await page.textContent("#mc-count")).includes("2") && (await page.textContent("#mc-list")).includes("Create"), "应重新列出现在的清单");
      // 看过现在的清单再点：先确认必装前置，再照常装，请求里带着页面上列着的两项
      await page.click("#mc-install");
      await page.waitForSelector('.modal [data-x="ok"]', { timeout: 5000 });
      await page.click('.modal [data-x="ok"]');
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart.lastInstall;
      assert(li && Array.isArray(li.expect) && li.expect.slice().sort().join() === "create,jei", "安装请求应带上页面上列着的两项：" + JSON.stringify(li));
    } finally { await cartState({ modCart: { reset: true } }); await page.goto(url("#/instances")); await settle(); }
  });

  await run("待装清单的存档读不出来（被占着或内容坏了）：清单的位置画报错框和「重试」，不说「清单是空的」；加入清单被拒以后报错框还在；放开以后点重试照常列出", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.evaluate(i => fetch("/api/modcart", { method: "POST", body: JSON.stringify({ inst: i, projectIds: ["jei"] }) }), PACK);
      await cartState({ modCart: { loadFail: "locked" } });
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      const box = await page.textContent("#mc-load-err");
      assert(box.includes("无法显示待装清单：清单的存档 cart.json 读不出来") && box.includes("都还在"), "读不出来时清单的位置应说清原因：" + box);
      assert((await page.$("#mc-empty")) === null && (await page.$eval("#mc-actions", el => el.hidden)), "读不出来时不该说清单是空的，也不该有清空 / 安装");
      // 加入清单被拒：提示里说同一句；清单的位置仍是报错框（以前加入失败会重画成「清单是空的」）
      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      const toasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(toasts.some(x => x.includes("无法加入清单：清单的存档 cart.json 读不出来")), "加入被拒应说清原因：" + JSON.stringify(toasts));
      assert((await page.$("#mc-load-err")) !== null && (await page.$("#mc-empty")) === null, "加入被拒以后清单的位置应还是报错框");
      // 文件坏了：重试，说法跟着换成「内容坏了」
      await cartState({ modCart: { loadFail: "corrupt" } });
      await page.click("#mc-load-retry"); await settle(600);
      assert((await page.textContent("#mc-load-err")).includes("内容坏了"), "重试以后说法应跟着换");
      // 修好了：重试，原来那项照常列出
      await cartState({ modCart: { loadFail: "" } });
      await page.click("#mc-load-retry"); await settle(600);
      assert((await page.$("#mc-load-err")) === null && (await page.textContent("#mc-list")).includes("JEI"), "读出来以后应照常列出原来的清单");
    } finally { await cartState({ modCart: { reset: true } }); await page.goto(url("#/instances")); await settle(); }
  });

  await run("待装清单从电脑添加时上传回 503（查不到接收磁盘的剩余空间）：不当成清单读不出来，失败提示显示原话，原来的清单还在；关掉开关再添加能加进去", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.evaluate(i => fetch("/api/modcart", { method: "POST", body: JSON.stringify({ inst: i, projectIds: ["jei"] }) }), PACK);
      await cartState({ modCart: { localUploadFail: "unknown-disk" } });
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      assert((await page.textContent("#mc-list")).includes("JEI"), "原来的清单应列着 JEI");
      const jar = { name: "custom-1.0.jar", mimeType: "application/java-archive", buffer: Buffer.from("PKcustom") };
      await page.setInputFiles("#mc-file", [jar]);
      await page.waitForSelector("#mc-local-err", { timeout: 8000 }); await settle(300);
      assert((await page.$("#mc-load-err")) === null && !(await page.textContent("#mc-list")).includes("无法显示待装清单"), "上传 503 不该当成清单读不出来");
      const failText = (await page.textContent("#mc-local-err")).replace(/\s+/g, " ");
      assert(failText.includes("custom-1.0.jar") && failText.includes("暂时查不到接收上传文件所在磁盘的剩余空间，没有接收文件。请稍后重试"), "失败提示应显示原话：" + failText);
      assert((await page.textContent("#mc-list")).includes("JEI") && (await page.textContent("#mc-count")).includes("1"), "原来清单里的东西应还列着");
      await cartState({ modCart: { localUploadFail: "" } });
      await page.setInputFiles("#mc-file", [jar]);
      await page.waitForSelector("#mc-local-ok", { timeout: 8000 }); await settle(300);
      assert((await page.textContent("#mc-list")).includes("custom-1.0.jar") && (await page.textContent("#mc-list")).includes("JEI") && (await page.textContent("#mc-count")).includes("2"), "关掉开关再添加应能加进去");
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

  // ==================== beta.3 更新渠道 ====================
  // 「设置 → 更新与关于」里选「稳定版（只收正式版）」或「最新版（包括测试版）」，改了马上保存（PUT /api/config 的 updateChannel）；
  // 检查更新的结果写明按哪个渠道查的、查到的是正式版还是测试版；稳定渠道上用着比最新正式版还新的测试版时不提示更新。
  // 仿真后端用到的：/__mock/state 的 version、releases、updateChannel、updateFail（见 server.js 的说明和 updateCheck）。
  // 用例名都以「beta.3 更新渠道」开头，node tests.js "beta.3 更新渠道" 只跑这一块。
  {
    const UC = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    // 故意乱序，版本号最大的是草稿
    const RELS = [{ tag: "v3.1.1" }, { tag: "v3.2.0-beta.2", prerelease: true }, { tag: "v3.1.2" }, { tag: "v3.2.0-beta.10", prerelease: true },
      { tag: "v3.2.0-beta.3", prerelease: true }, { tag: "v3.3.0", draft: true }];
    const reset = () => UC({ version: "2.9.0-beta.2", releases: [{ tag: "v2.8.3" }, { tag: "v2.9.0-beta.1", prerelease: true }, { tag: "v2.9.0-beta.2", prerelease: true }], updateChannel: "", updateFail: "", startupUpdate: null });
    const picked = () => page.$$eval('#up-channel [role="radio"]', rs => rs.filter(r => r.getAttribute("aria-checked") === "true").map(r => r.dataset.ch).join(","));
    const result = () => page.$eval("#up-result", e => e.textContent.replace(/\s+/g, " ").trim());
    const check = async () => {
      await page.click("#up-check");
      await page.waitForFunction(() => { const t = (document.querySelector("#up-result") || {}).textContent || ""; return t && t !== "检查中…"; });
      return result();
    };

    await run("beta.3 更新渠道：三种渠道选择（跟随当前版本、稳定版、最新版）会保存，没选过或选回跟随当前版本时按正在用的版本定", async () => {
      try {
        errors.length = 0;
        await UC({ version: "3.2.0-beta.3", releases: RELS, updateChannel: "" });
        await page.goto(url("#/settings/about")); await settle(500);
        assert(await picked() === "auto", "没选过时应选中「跟随当前版本」：" + await picked());
        const head = await page.textContent("#up-ch-sub");
        assert(head.includes("还没选过") && head.includes("「最新版」"), "没选过时的说明：" + head);
        const labels = await page.$$eval('#up-channel [role="radio"]', rs => rs.map(r => r.textContent.replace(/\s+/g, " ").trim()));
        assert(labels.length === 3 && labels[0].includes("跟随当前版本（默认）") && labels[1].includes("稳定版（只收正式版）") && labels[2].includes("最新版（包括测试版）") && labels[2].includes("测试版可能有还没发现的问题"),
          "三个选项：" + labels.join(" | "));
        // 选最新版：存进配置，提示里写明；静默检查关着时提醒还要打开它
        await page.click('label.switch:has(#up-silent)'); await settle(250);
        assert(!(await page.isChecked("#up-silent")), "静默检查应先关上");
        const t00 = reqs.length;
        await page.click('#up-channel [data-ch="latest"]'); await settle(300);
        const put0 = reqs.slice(t00).filter(r => r.m === "PUT" && r.u === "/api/config").map(r => JSON.parse(r.b || "{}"));
        assert(put0.length === 1 && put0[0].updateChannel === "latest", "选最新版要保存：" + JSON.stringify(put0));
        assert(await page.locator(".toast", { hasText: "已改成最新版" }).count() >= 1, "没有「已改成最新版」的提示");
        assert(await page.locator(".toast", { hasText: "启动时静默检查更新" }).count() >= 1, "静默检查关着时应提醒还要打开它");
        assert(!(await page.textContent("#up-ch-sub")).includes("还没选过"), "选定后不该再说还没选过");
        // 再点同一项：不再发请求，只提示现在就是它
        const t01 = reqs.length;
        await page.click('#up-channel [data-ch="latest"]'); await settle(300);
        assert(!reqs.slice(t01).some(r => r.m === "PUT" && r.u === "/api/config") && await page.locator(".toast", { hasText: "现在就是「最新版」" }).count() >= 1, "再点同一项");
        await page.click('label.switch:has(#up-silent)'); await settle(250); // 打开回去，后面的检查照旧
        assert(await page.isChecked("#up-silent"), "静默检查应打开回去");
        // 选稳定版：马上发一个只带 updateChannel 的 PUT，存进配置，给提示
        const t0 = reqs.length;
        await page.click('#up-channel [data-ch="stable"]'); await settle(300);
        const puts = reqs.slice(t0).filter(r => r.m === "PUT" && r.u === "/api/config").map(r => JSON.parse(r.b || "{}"));
        assert(puts.length === 1 && puts[0].updateChannel === "stable" && Object.keys(puts[0]).length === 1, "保存请求：" + JSON.stringify(puts));
        assert((await UC({})).config.updateChannel === "stable", "仿真后端里没存上");
        assert(await picked() === "stable", "点了以后的选中项：" + await picked());
        assert(await page.locator(".toast", { hasText: "已改成稳定版" }).count() >= 1, "没有「已改成稳定版」的提示");
        // 重新进来还是稳定版：用着测试版也以选的为准
        await page.goto(url("#/settings/about")); await settle(500);
        assert(await picked() === "stable", "重新进来：" + await picked());
        // A79：点回「跟随当前版本（默认）」：发 {updateChannel: ""}，配置项清回未选状态，重新按当前版本定渠道
        const tAuto = reqs.length;
        await page.click('#up-channel [data-ch="auto"]'); await settle(300);
        const putAuto = reqs.slice(tAuto).filter(r => r.m === "PUT" && r.u === "/api/config").map(r => JSON.parse(r.b || "{}"));
        assert(putAuto.length === 1 && putAuto[0].updateChannel === "", "选回跟随当前版本要发空串：" + JSON.stringify(putAuto));
        assert(!((await UC({})).config.updateChannel), "仿真后端里 updateChannel 应清回空");
        assert(await picked() === "auto", "选中项应切回 auto：" + await picked());
        assert((await page.textContent("#up-ch-sub")).includes("「最新版」"), "用着测试版切回跟随当前版本，说明应跟随最新版：" + await page.textContent("#up-ch-sub"));
        // 键盘也能选：聚焦「最新版」按空格
        await page.focus('#up-channel [data-ch="latest"]'); await page.keyboard.press(" "); await settle(300);
        assert((await UC({})).config.updateChannel === "latest" && await picked() === "latest", "键盘选最新版：" + await picked());
        // 用着正式版、没选过：按稳定版，选回 auto 后也是稳定版
        await UC({ version: "3.1.2", updateChannel: "" });
        await page.goto(url("#/settings/about")); await settle(500);
        assert(await picked() === "auto", "用着正式版、没选过时选中 auto：" + await picked());
        assert((await page.textContent("#up-ch-sub")).includes("「稳定版」"), "没选过时的说明：" + await page.textContent("#up-ch-sub"));
        assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
      } finally { await reset(); }
    });

    await run("beta.3 更新渠道：检查结果写明按哪个渠道查的、查到的是正式版还是测试版；换了渠道，上一次的结果连同「立即更新」一起清掉", async () => {
      try {
        errors.length = 0;
        await UC({ version: "3.1.1", releases: RELS, updateChannel: "stable" });
        await page.goto(url("#/settings/about")); await settle(500);
        let t = await check();
        assert(t.includes("按「稳定版」查") && t.includes("发现新版本 v3.1.2（正式版）") && t.includes("当前 v3.1.1"), "稳定版有更新：" + t);
        assert(await page.locator("#up-result button", { hasText: "立即更新并重启" }).count() === 1, "有更新时要给「立即更新并重启」");
        await page.click('#up-channel [data-ch="latest"]'); await settle(300);
        assert(await result() === "" && await page.locator("#up-result button").count() === 0, "换了渠道，上一次的结果应清掉：" + await result());
        t = await check();
        assert(t.includes("按「最新版」查") && t.includes("发现新版本 v3.2.0-beta.10（测试版）"), "最新版取版本号最大的测试版（草稿不算）：" + t);
        // 已是最新也写明渠道和查到的是哪种版本
        await UC({ version: "3.2.0-beta.10" });
        t = await check();
        assert(t.includes("按「最新版」查") && t.includes("已是最新") && t.includes("测试版") && await page.locator("#up-result button").count() === 0, "最新版已是最新：" + t);
        await page.click('#up-channel [data-ch="stable"]'); await settle(300);
        await UC({ version: "3.1.2" });
        t = await check();
        assert(t.includes("按「稳定版」查") && t.includes("已是最新") && t.includes("正式版"), "稳定版已是最新：" + t);
        // 查不了：一句结论，原文进复制键（同 00-报错信息.md）
        await UC({ updateFail: "检查更新失败（连不上 GitHub）：dial tcp: i/o timeout" });
        await page.click("#up-check"); await page.waitForSelector("#up-result .err-inline");
        assert((await result()).includes("无法检查更新"), "报错：" + await result());
        assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
      } finally { await reset(); }
    });

    await run("beta.3 更新渠道：稳定渠道上用着比最新正式版还新的测试版时不提示更新，写明正式版出来以后会提示", async () => {
      try {
        await UC({ version: "3.2.0-beta.3", releases: RELS, updateChannel: "stable" });
        await page.goto(url("#/settings/about")); await settle(500);
        const t = await check();
        assert(await page.locator("#up-result button").count() === 0 && !t.includes("发现新版本"), "不该提示更新：" + t);
        assert(t.includes("按「稳定版」查") && t.includes("最新的正式版是 v3.1.2") && t.includes("你用的是测试版 v3.2.0-beta.3") && t.includes("正式版 3.2.0 出来以后会提示"), "说法：" + t);
      } finally { await reset(); }
    });

    await run("启动时查到新版本（A78）与稳定版只有预发布（A80）：不弹窗，侧栏设置和小节显示小圆点，「更新与关于」显示启动通知和「查看」", async () => {
      try {
        errors.length = 0;
        await UC({
          version: "3.2.4",
          updateChannel: "stable",
          releases: [{ tag: "v3.3.0-beta.1", prerelease: true }],
          startupUpdate: { tag: "v3.3.0", channel: "stable", prerelease: false, notes: "修复若干问题" },
        });
        await page.goto(url("#/instances")); await settle(500);
        assert(await page.locator(".modal").count() === 0, "有启动更新通知时不应弹窗");
        assert(!(await page.$eval("#nav-settings-dot", el => el.hidden)), "侧栏「设置」应显示小圆点");
        await page.goto(url("#/settings/about")); await settle(500);
        assert(!(await page.$eval("#set-about-dot", el => el.hidden)), "设置左侧「更新与关于」应显示小圆点");
        assert((await page.textContent("#up-startup")).includes("启动时查到新版本 v3.3.0"), "启动通知文案：" + await page.textContent("#up-startup"));
        await page.click("#up-startup-view"); await settle(200);
        assert(await page.locator("#up-startup").count() === 0, "点「查看」后应收掉启动通知条");
        assert(await page.$eval("#nav-settings-dot", el => el.hidden), "点「查看」后侧栏圆点应收掉");
        assert(await page.$eval("#set-about-dot", el => el.hidden), "点「查看」后设置导航圆点应收掉");
        assert((await result()).includes("v3.3.0") && await page.locator("#up-result button", { hasText: "立即更新并重启" }).count() === 1, "应展开新版本和更新按钮：" + await result());
        // A80：稳定渠道只有预发布版时，检查更新提示暂无正式版、不报错也不出更新按钮
        const noStable = await check();
        assert(noStable.includes("还没有正式发布的版本") && await page.locator("#up-result button").count() === 0, "只有预发布时：" + noStable);
        assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
      } finally { await reset(); }
    });
    await run("启动时查到新版本（A78）：后台检查比界面打开晚查完时，圆点过一会儿自己亮，不用先进设置", async () => {
      try {
        errors.length = 0;
        await UC({ version: "3.2.4", updateChannel: "stable", startupUpdate: null });
        await page.goto(url("#/instances")); await settle(500);
        assert(await page.$eval("#nav-settings-dot", el => el.hidden), "还没查到时不该亮圆点");
        await UC({ startupUpdate: { tag: "v3.3.0", channel: "stable", prerelease: false, notes: "修复若干问题" } }); // 后台检查这时才查完
        await page.waitForFunction(() => !document.getElementById("nav-settings-dot").hidden, null, { timeout: 8000 })
          .catch(() => { throw new Error("后台检查查完以后，侧栏「设置」的圆点没有自己亮"); });
        assert(await page.locator(".modal").count() === 0, "查到新版本时不应弹窗");
        assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
      } finally { await reset(); }
    });
  }

  // ==================== beta.3 小修 ====================
  // 隧道凭据不到 6 个字节时日志里含它的行会整行隐藏（tunnel.credMasker）：不拒绝（自定义 frps 的令牌本来可以很短），
  // 添加时给一句提示。长度按 UTF-8 字节算，同后端；前后的空白不算；空着不提示。提示里也写「字节」（以前写成「字符」，Codex 复审 v3.2.0 第二轮）
  await run("beta.3 小修：添加隧道时凭据不到 6 个字节给一句提示，只提示、照样能添加", async () => {
    await taState({ tunnelAccounts: { reset: true }, tunnelsReset: true });
    try {
      errors.length = 0;
      await page.goto(url("#/tunnels")); await settle();
      await page.click("#tn-toggle");
      await page.selectOption("#tn-provider", "custom");
      const shown = () => page.$eval("#tn-cred-short", e => !e.hidden && e.offsetParent !== null);
      assert(!(await shown()), "凭据空着时不该提示");
      for (const [val, want] of [["abc", true], ["abcdef", false], ["密", true], ["密钥", false], ["  ab  ", true], ["", false], ["abc", true]]) {
        await page.fill("#tn-cred", val);
        assert(await shown() === want, `凭据「${val}」时提示应${want ? "出现" : "不出现"}`);
      }
      const shortText = await page.textContent("#tn-cred-short");
      assert(shortText.includes("日志里含这串凭据的行会整行隐藏，排查问题会不方便，建议换一串更长的") && shortText.includes("6 个字节") && !shortText.includes("字符"),
        "提示的说法（按字节算，不说「字符」）：" + shortText);
      // 只是提示：照样发出添加请求，凭据原样带上
      await page.fill("#tn-name", "短令牌"); await page.fill("#tn-saddr", "frp.example.com");
      const t0 = posts.length;
      await page.click("#tn-add");
      assert(await toastHas("隧道已添加"), "短凭据照样能添加");
      const add = postsTo(t0, "/api/tunnels");
      assert(add.length === 1 && JSON.parse(add[0].b).credential === "abc", "添加请求：" + JSON.stringify(add));
      assert(!(await shown()), "添加以后表单清空，提示也该收起");
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await resetTA(); await resetMusic(); }
  });

  // tunnels.json 在却没读出来时，音乐共享页以前显示「没有语音隧道」并引导去添加，只开着音乐共享页也不会触发隧道重读。
  // 现在说清隧道配置没读出来（正文接后端那句，原文进复制键），给「重试」和「去内网穿透页」；后端取状态时会重读，读出来以后自己恢复
  await run("beta.3 小修：tunnels.json 没读出来时音乐共享页说清楚、给重试和去内网穿透页，不再引导添加，读出来以后恢复；内网穿透页顶上说同一局域网不用隧道", async () => {
    const setLoad = v => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ tunnelsLoadFail: x }) }), v);
    try {
      errors.length = 0;
      // 内网穿透页顶上的指路：同一局域网的朋友用服务器页上的连接地址，不用隧道（tunnels.json 没读出来时也照样在）
      await setLoad("locked");
      await page.goto(url("#/tunnels")); await settle();
      await page.waitForSelector("#tun-load-err");
      const tip = await page.textContent("#tn-lan-tip");
      assert(tip.includes("同一局域网的朋友不用隧道") && tip.includes("「连接地址」"), "页顶应指路：同一局域网用连接地址：" + tip);
      await openMusic({ voiceTunnel: true, inst: { [PACK]: { bridge: "main" } } }, "stopped");
      await page.waitForSelector("#mu-tun-retry");
      const row = await page.evaluate(() => {
        const r = document.querySelector("#mu-tun-retry").closest(".row-item"), copyBtn = r.querySelector("[data-err-raw]");
        return { text: r.textContent.replace(/\s+/g, " "), raw: copyBtn ? copyBtn.getAttribute("data-err-raw") : "",
          link: [...r.querySelectorAll('a[href="#/tunnels"]')].map(a => a.textContent.trim()) };
      });
      const LOCKED = "MineDeck 文件夹里的 tunnels.json 读不出来，可能被别的程序占着。关掉占用它的程序后会自动重读";
      assert(row.text.includes("还不知道有没有语音隧道") && row.text.includes("隧道配置没读出来：" + LOCKED), "应说清隧道配置没读出来，正文接后端那句：" + row.text);
      assert(row.text.includes("不会改 voice_host"), "应说清这期间不改 voice_host：" + row.text);
      assert(!row.text.includes("去添加") && !row.text.includes("添加一条 UDP 隧道"), "没读出来时不该引导去添加：" + row.text);
      assert(row.link.length === 1 && row.link[0] === "去内网穿透页", "应给「去内网穿透页」：" + JSON.stringify(row.link));
      assert(row.raw.includes("being used by another process") && !row.text.includes("another process"), "原文进复制键，不在正文里：" + row.raw);
      // 还没读出来时点「重试」：再取一次状态，提示还是没读出来，按钮还能点
      const r0 = reqs.length;
      await page.click("#mu-tun-retry");
      assert(await toastHas("隧道配置还是没读出来"), "重试仍没读出来应提示一句");
      assert(reqs.slice(r0).some(r => r.m === "GET" && decodeURIComponent(r.u).endsWith(`/instances/${PACK}/music`)), "重试应重新取音乐共享的状态（后端借此重读隧道配置）");
      assert(await page.$eval("#mu-tun-retry", b => !b.disabled), "重试键应恢复可点");
      // 读出来了：点「重试」就换成语音隧道的状态
      await setLoad("");
      await page.click("#mu-tun-retry");
      await page.waitForFunction(() => !document.querySelector("#mu-tun-retry"), null, { timeout: 3000 });
      const check = await page.textContent("#mu-check");
      assert(check.includes("远程好友的语音") && !check.includes("还不知道有没有语音隧道"), "读出来以后应照常显示语音隧道：" + check);
      // 只开着音乐共享页、不点任何东西：隔几秒的刷新也会读出来
      await setLoad("corrupt");
      await page.waitForSelector("#mu-tun-retry", { timeout: 5000 });
      assert((await page.textContent("#mu-check")).includes("内容坏了"), "原因换了，说法跟着换");
      await setLoad("");
      await page.waitForFunction(() => !document.querySelector("#mu-tun-retry"), null, { timeout: 5000 });
      assert(errors.length === 0, "页面报错：" + errors.slice(0, 2).join(" | "));
    } finally { await setLoad(""); await resetMusic(); }
  });

  // ---- 热门清单、从电脑添加本地文件、两栏版面（2026-11） ----
  const cartMock = async () => (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart;
  const toastsNow = () => page.$$eval(".toast", ts => ts.map(t => t.textContent));
  // holdPost：拦住 pattern 的 POST，等测试放行（同一地址的 GET 轮询照常过）
  const holdPost = async (pattern, fn) => {
    let release, seen;
    const gate = new Promise(r => release = r), requested = new Promise(r => seen = r);
    const hold = async route => {
      if (route.request().method() !== "POST") return route.continue().catch(() => {});
      seen(); await gate; await route.continue().catch(() => {});
    };
    await page.route(pattern, hold);
    try { await fn(requested, release); } finally { release(); await page.unroute(pattern, hold); }
  };
  /* 一批「没装上的文件」（同后端 import-pending 的项）：下载地址不在允许列表里，可以用本地文件代替、跳过或信任站点 */
  const pendingItems = (prefix, count) => Array.from({ length: count }, (_, i) => ({
    id: `${prefix}${i}`, path: `mods/${prefix}-${i}.jar`, reason: "host", host: "files.example.net", note: "下载地址不在允许列表",
    urls: [`https://files.example.net/${prefix}-${i}.jar`], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"],
  }));
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
      assert(!(await page.$eval("#mc-side", el => el.classList.contains("mc-drop"))) && decodeURIComponent(page.url()).includes(PACK + "/addons"), "放下以后高亮应收起、页面不该跳走"); // beta.3 R06：旧地址 cart 原地换成 addons
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
      // 往下滚：左边的列表滚走了，右栏还贴在内容区顶上。beta.3 R10 起清单上面多了「已安装」那一块，滚到清单开头再往下 400
      await page.evaluate(() => { $("#main").scrollTop = $(".mc-wrap").getBoundingClientRect().top - $("#main").getBoundingClientRect().top + $("#main").scrollTop + 400; }); await settle(150);
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

  // ==================== beta.3 内网穿透与设置（R12 R17 R18 R19 R20） ====================
  // docs/测试与评审/可用性走查-2026-11-14.md 第八节 R12、R15（设置部分）、R16（设置部分）、R17–R20 和 U29；外加第一层留下的弹窗叠层（内网穿透页、assistant.js、mcp.js）。
  // 用例名都以「beta.3 穿透与设置」开头，node tests.js "beta.3 穿透与设置" 只跑这一块。
  // 仿真后端用到的：/__mock/state 的 tunnel（noLog、localPort……）、tunnelsLoadFail、pickDir、config（serversDir、backupsDir），见 server.js 的说明
  {
    const L2 = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PACK0 = "机械动力整合包", SURV = "生存服 · 朋友", SNAP = "原版快照测试";
    const rowText = id => page.$eval(`#tun-list [data-tun="${id}"]`, e => e.textContent.replace(/\s+/g, " ").trim());
    const clip = () => page.evaluate(() => { window.__copied = null; Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied = t; } } }); });
    const copied = () => page.evaluate(() => window.__copied);
    const layers = () => page.$$eval("#modal-root > .modal-mask:not(.leave)", ms => ms.map(m => m.id || "layer"));
    // 下面先垫一层有输入框的弹窗，看上面叠上来的弹窗关掉以后它还在不在、填的字还在不在
    const lowerLayer = () => page.evaluate(() => { modalLayer(`<div class="modal" role="dialog" aria-label="垫在下面的弹窗"><input id="lower-in" aria-label="下层的输入框" value=""><button class="btn" data-x="ok">下层的确定</button></div>`, "lower-mask"); document.querySelector("#lower-in").value = "下层填的字"; });
    const lowerKept = () => page.evaluate(() => { const m = document.getElementById("lower-mask"); return !!m && !m.dataset.leaving && m.querySelector("#lower-in").value === "下层填的字"; });
    const dropLower = () => page.evaluate(() => { const m = document.getElementById("lower-mask"); if (m) m.remove(); });
    const lastPut = async () => (await L2({})).lastTunnelPut;
    const TUNS = [
      { id: "t1", name: "朋友联机", provider: "openfrp", proxyId: "246810", running: false, boundInstance: SURV, autoStart: true, publicAddr: "cn-sz-bgp-1.openfrp.top:23456", lastError: "" },
      { id: "t2", name: "自建备用线", provider: "custom", running: false, boundInstance: "", autoStart: false, publicAddr: "", lastError: "",
        serverAddr: "frp.example.com", serverPort: 7000, remotePort: 25566, localPort: 25565, proto: "tcp", purpose: "game", hasCredential: true },
      { id: "t3", name: "樱花新线", provider: "natfrp", proxyId: "13579", running: false, boundInstance: PACK0, autoStart: false, publicAddr: "", lastError: "", noLog: true },
      { id: "t4", name: "旧服的线", provider: "custom", running: false, boundInstance: "已经移出的服", autoStart: true, publicAddr: "", lastError: "",
        serverAddr: "frp.example.com", serverPort: 7000, remotePort: 30002, localPort: 25570, proto: "tcp", purpose: "game" },
    ];

    await run("beta.3 穿透与设置 R12/R18：我的隧道排在平台账户上面；停着的自定义 frps 不启动也显示「frps 地址:公网端口」能复制；停着的平台隧道显示上次的地址、标「上次」；没拿到过的写「还没拿到地址」；行上写跟随谁、开还是关；页顶说清添加隧道和平台账户的关系，樱花的 frpc 写成几步", async () => {
      await L2({ tunnels: JSON.parse(JSON.stringify(TUNS)) });
      try {
        await page.goto(url("#/tunnels")); await settle(700);
        const order = await page.evaluate(() => !!(document.querySelector("#tn-mine").compareDocumentPosition(document.querySelector("#tn-accounts")) & Node.DOCUMENT_POSITION_FOLLOWING));
        assert(order && (await page.textContent("#tn-mine h3")).trim() === "我的隧道", "「我的隧道」应排在平台账户上面");
        const r2 = await rowText("t2"), r1 = await rowText("t1"), r3 = await rowText("t3");
        assert(r2.includes("公网地址") && r2.includes("frp.example.com:25566") && !r2.includes("上次") && r2.includes("没绑定服务器"), "停着的自定义 frps：" + r2);
        assert(r1.includes("cn-sz-bgp-1.openfrp.top:23456") && (await page.textContent('[data-tun="t1"] .tun-last')) === "上次" && r1.includes(`跟随「${SURV}」启动：开`), "停着的平台隧道：" + r1);
        assert(r3.includes("还没拿到地址") && !(await page.$('[data-tun="t3"] [data-copy]')) && r3.includes(`跟随「${PACK0}」启动：关`) && !r3.includes("尚未启动"), "没拿到过地址的：" + r3);
        await clip();
        await byName("button", "复制 自建备用线 的公网地址").click(); await settle(150);
        assert(await copied() === "frp.example.com:25566" && await toastHas("已复制公网地址：frp.example.com:25566"), "复制的是 frps 地址:公网端口：" + await copied());
        await byName("button", "复制 朋友联机 的公网地址").click(); await settle(150);
        assert(await copied() === "cn-sz-bgp-1.openfrp.top:23456", "上次的地址也能复制：" + await copied());
        // 「清除凭据」悬停有说明
        assert((await page.getAttribute('[data-tclear="t2"]', "title")).includes("auth token"), "清除凭据的悬停说明");
        // 日志：没有输出时写一句，有输出时照常
        await page.click('[data-tlog="t3"]'); await settle(400);
        assert((await page.textContent("#tlog-t3")).includes("还没有日志"), "空日志：" + await page.textContent("#tlog-t3"));
        await page.click('[data-tlog="t2"]'); await settle(400);
        const log2 = await page.textContent("#tlog-t2");
        assert(log2.includes("start proxy success") && !log2.includes("还没有日志"), "有日志时：" + log2);
        // R18：页顶说清两种加隧道的办法；樱花的 frpc 写成照着做的几步（路径是这台电脑上的）
        const how = await page.textContent("#tn-how");
        assert(how.includes("添加隧道") && how.includes("平台账户") && how.includes("不用自己抄密钥"), "页顶说明：" + how);
        await page.click("#tn-toggle");
        await page.selectOption("#tn-provider", "natfrp"); await settle(150);
        const steps = await page.textContent("#tn-help [data-frpc-steps]");
        assert(steps.includes("软件下载") && steps.includes("D:\\MineDeck\\runtimes\\frpc\\natfrp") && steps.includes("点「启动」") && !(await page.textContent("#tn-help")).includes("按提示把 frpc 放好"), "樱花 frpc 的步骤：" + steps);
      } finally { await L2({ tunnelsReset: true }); }
    });

    await run("beta.3 穿透与设置 R12：「编辑」只改名称、绑定的服务器和跟随启动，只发改了的几项、其余配置在后端原样留着；指向网页端口、tunnels.json 没读出来、运行中时被拒，原因写在弹窗里、填的不丢", async () => {
      const tunOf = id => L2({}).then(s => s.tunnels.find(t => t.id === id));
      await L2({ tunnels: JSON.parse(JSON.stringify(TUNS)) });
      try {
        await page.goto(url("#/tunnels")); await settle(700);
        await byName("button", "编辑 自建备用线").click(); await page.waitForSelector("#tn-ed-name");
        // 弹窗里只有这三样：没有服务商、地址、端口、凭据
        const fields = await page.$$eval("#modal-root .modal input, #modal-root .modal select, #modal-root .modal textarea", es => es.map(e => e.id));
        assert(JSON.stringify(fields) === JSON.stringify(["tn-ed-name", "tn-ed-bound", "tn-ed-auto"]), "编辑弹窗的字段：" + fields);
        assert((await page.textContent("#modal-root .modal")).includes("删掉这条隧道，重新添加"), "要说清换服务商、地址端口、凭据怎么办");
        await page.fill("#tn-ed-name", "自建改名");
        await page.selectOption("#tn-ed-bound", PACK0);
        await page.check("#tn-ed-auto");
        await page.click('#modal-root [data-x="ok"]'); await settle(500);
        assert(await toastHas("已保存「自建改名」的修改") && !(await page.$("#tn-ed-name")), "保存后关掉弹窗、提示");
        // 只发改了的三项（修改接口只改传了的项，Codex 复审 v3.2.0 第二轮）；服务商、地址、端口、凭据在后端原样留着
        let b = await lastPut();
        assert(b && JSON.stringify(b) === JSON.stringify({ name: "自建改名", boundInstance: PACK0, autoStart: true }), "请求体：" + JSON.stringify(b));
        let t = await tunOf("t2");
        assert(t.provider === "custom" && t.serverAddr === "frp.example.com" && t.serverPort === 7000 && t.remotePort === 25566 && t.localPort === 25565 &&
          t.proto === "tcp" && t.purpose === "game" && t.hasCredential === true, "别的配置原样留着：" + JSON.stringify(t));
        await page.waitForFunction(() => (document.querySelector('[data-tun="t2"]') || {}).textContent?.includes("跟随「机械动力整合包」启动：开"), null, { timeout: 3000 }).catch(() => {});
        assert((await rowText("t2")).includes(`跟随「${PACK0}」启动：开`), "行上跟着变：" + await rowText("t2"));
        // 平台隧道：只关了跟随启动，就只发这一项；上次的公网地址、隧道 ID 在后端原样留着（不然一改就丢了「上次」的地址）
        await byName("button", "编辑 朋友联机").click(); await page.waitForSelector("#tn-ed-name");
        await page.uncheck("#tn-ed-auto");
        await page.press("#tn-ed-name", "Enter"); await settle(500);
        b = await lastPut();
        assert(JSON.stringify(b) === JSON.stringify({ autoStart: false }), "平台隧道的请求体：" + JSON.stringify(b));
        t = await tunOf("t1");
        assert(t.provider === "openfrp" && t.proxyId === "246810" && t.publicAddr === "cn-sz-bgp-1.openfrp.top:23456" && t.autoStart === false && t.name === "朋友联机", "平台隧道：" + JSON.stringify(t));
        // 绑着的服务器已经不在列表里（移出列表了）：下拉照样选着它、写明列表里没有，只改名字时绑定不被悄悄清掉
        await byName("button", "编辑 旧服的线").click(); await page.waitForSelector("#tn-ed-name");
        const sel = await page.$eval("#tn-ed-bound", s => ({ v: s.value, t: s.selectedOptions[0].textContent }));
        assert(sel.v === "已经移出的服" && sel.t.includes("列表里没有这台"), "不在列表里的绑定：" + JSON.stringify(sel));
        await page.fill("#tn-ed-name", "旧服的线 2");
        await page.click('#modal-root [data-x="ok"]'); await settle(500);
        b = await lastPut();
        t = await tunOf("t4");
        assert(JSON.stringify(b) === JSON.stringify({ name: "旧服的线 2" }) && t.boundInstance === "已经移出的服" && t.autoStart === true, "只改名字时绑定照旧：" + JSON.stringify({ b, t }));
        // 一样都没改就点保存：不发请求，直接关掉
        const n0 = (await L2({})).lastTunnelPut;
        await byName("button", "编辑 旧服的线 2").click(); await page.waitForSelector("#tn-ed-name");
        await page.click('#modal-root [data-x="ok"]'); await settle(300);
        assert(!(await page.$("#tn-ed-name")) && JSON.stringify((await L2({})).lastTunnelPut) === JSON.stringify(n0), "没改时不该发请求");
        // 指向 MineDeck 网页端口的自定义 frps（以前存下的）：只改名称也被拒，原因写在弹窗里，填的不丢
        await L2({ tunnel: { id: "t2", localPort: PORT } });
        await page.goto(url("#/tunnels")); await settle(700);
        await byName("button", "编辑 自建改名").click(); await page.waitForSelector("#tn-ed-name");
        await page.fill("#tn-ed-name", "再改一次");
        await page.click('#modal-root [data-x="ok"]'); await settle(400);
        let err = await page.textContent("#tn-ed-err");
        assert(err.includes("没能保存修改") && err.includes("MineDeck 自己的网页端口") && await page.inputValue("#tn-ed-name") === "再改一次", "网页端口：" + err);
        await page.keyboard.press("Escape"); await settle(300);
        assert(!(await page.$("#tn-ed-name")), "Esc 关掉编辑弹窗");
        // tunnels.json 没读出来：拒绝，说的是读不出来（不说隧道不存在）
        await byName("button", "编辑 朋友联机").click(); await page.waitForSelector("#tn-ed-name");
        await page.fill("#tn-ed-name", "读不出来时改");
        await L2({ tunnelsLoadFail: "corrupt" });
        await page.click('#modal-root [data-x="ok"]'); await settle(400);
        err = await page.textContent("#tn-ed-err");
        assert(err.includes("没能保存修改") && err.includes("tunnels.json 内容坏了") && await page.inputValue("#tn-ed-name") === "读不出来时改", "没读出来：" + err);
        assert((await lastPut()).name !== "读不出来时改", "没读出来时不该改动");
        await L2({ tunnelsLoadFail: "" });
        await page.keyboard.press("Escape"); await settle(300);
        // 运行中：弹窗里先写明，保存时被拒
        await L2({ tunnel: { id: "t1", running: true, online: true } });
        await page.goto(url("#/tunnels")); await settle(700);
        await byName("button", "编辑 朋友联机").click(); await page.waitForSelector("#tn-ed-name");
        assert(await page.isVisible("#tn-ed-running"), "运行中要先写明");
        await page.fill("#tn-ed-name", "运行中改名"); // 什么都不改不会发请求，改个名字才去后端碰「运行中不能改」
        await page.click('#modal-root [data-x="ok"]'); await settle(400);
        assert((await page.textContent("#tn-ed-err")).includes("请先停止隧道再修改"), "运行中：" + await page.textContent("#tn-ed-err"));
        await page.keyboard.press("Escape"); await settle(300);
      } finally { await L2({ tunnelsLoadFail: "", tunnelsReset: true }); }
    });

    await run("beta.3 穿透与设置 U21：添加隧道时本地端口随绑定的服务器填，自己改过就不再替人改；语音隧道照旧填 SVC 的端口，换回游戏再按服务器填", async () => {
      await L2({ tunnelsReset: true });
      try {
        await page.goto(url("#/tunnels")); await settle(600);
        await page.click("#tn-toggle");
        await page.selectOption("#tn-provider", "custom");
        assert(await page.inputValue("#tn-lport") === "25565", "没绑定时是默认的 25565");
        await page.selectOption("#tn-bound", PACK0);
        assert(await page.inputValue("#tn-lport") === "25566", "绑定机械动力整合包（25566）：" + await page.inputValue("#tn-lport"));
        await page.selectOption("#tn-bound", SNAP);
        assert(await page.inputValue("#tn-lport") === "25567", "换成原版快照测试（25567）：" + await page.inputValue("#tn-lport"));
        await page.selectOption("#tn-bound", "");
        assert(await page.inputValue("#tn-lport") === "25567", "改成不绑定时不动端口");
        await page.fill("#tn-lport", "30001");
        await page.selectOption("#tn-bound", PACK0);
        assert(await page.inputValue("#tn-lport") === "30001", "自己填过就不再替人改");
        // 先选服务器、再换成自定义 frps：也按服务器填
        await page.goto(url("#/tunnels")); await settle(600);
        await page.click("#tn-toggle");
        await page.selectOption("#tn-bound", SNAP);
        await page.selectOption("#tn-provider", "custom");
        assert(await page.inputValue("#tn-lport") === "25567", "先选服务器再换服务商：" + await page.inputValue("#tn-lport"));
        // 语音隧道：music.js 填 SVC 的端口；换回游戏，按绑定的服务器填
        await page.selectOption("#tn-bound", PACK0);
        await page.selectOption("#tn-purpose", "voice"); await settle(300);
        assert(await page.inputValue("#tn-lport") === "24454", "语音隧道填 SVC 的端口：" + await page.inputValue("#tn-lport"));
        await page.selectOption("#tn-purpose", "game"); await settle(100);
        assert(await page.inputValue("#tn-lport") === "25566", "换回游戏按服务器填：" + await page.inputValue("#tn-lport"));
      } finally { await L2({ tunnelsReset: true }); }
    });

    await run("beta.3 穿透与设置：内网穿透页的弹窗（编辑、平台购买、平台创建）叠在已开的弹窗上面；Esc 只关上面那层，下面的弹窗和填的字还在；创建框里看报错详情再回来，填的名称还在", async () => {
      await L2({ tunnelsReset: true, tunnelAccounts: { reset: true, openfrp: true } });
      try {
        await page.goto(url("#/tunnels")); await settle(700);
        const top2 = async what => {
          const ls = await layers();
          assert(ls.length === 2 && ls[0] === "lower-mask", what + " 应叠在上面：" + JSON.stringify(ls));
          await page.keyboard.press("Escape"); await settle(300);
          assert(JSON.stringify(await layers()) === JSON.stringify(["lower-mask"]) && await lowerKept(), what + "：Esc 只该关上面那层，下面的还在");
        };
        await lowerLayer();
        await page.$eval('[data-tedit="t2"]', b => b.click()); await page.waitForSelector("#tn-ed-name");
        await top2("编辑隧道");
        await page.$eval('[data-acc-buy="openfrp"]', b => b.click()); await page.waitForSelector('#modal-root .modal [data-buy]');
        await top2("购买 / 充值");
        await dropLower();
        await page.click('[data-acc-overview="openfrp"]'); await page.waitForSelector("[data-acc-create]");
        await lowerLayer();
        await page.$eval("[data-acc-create]", b => b.click()); await page.waitForSelector("#tn-cr-name");
        await top2("创建并导入");
        await dropLower();
        // 创建框里看一眼报错详情：详情叠在上面，关掉以后创建框和填的名称都在；点遮罩不关（框里有要填的）
        await page.click("[data-acc-create]"); await page.waitForSelector("#tn-cr-name");
        await page.fill("#tn-cr-name", "mc_keep");
        await page.evaluate(() => { showErrDetail("测试用的原文"); });
        await page.waitForSelector(".err-detail");
        await page.keyboard.press("Escape"); await settle(300);
        assert((await layers()).length === 1 && await page.inputValue("#tn-cr-name") === "mc_keep", "详情关掉以后创建框和名称都在");
        await page.mouse.click(20, 820); await settle(300);
        assert(await page.$("#tn-cr-name"), "点遮罩不该关掉有输入的创建框");
        await page.keyboard.press("Escape"); await settle(300);
        assert(!(await page.$("#modal-root > .modal-mask:not(.leave)")), "Esc 关掉创建框");
      } finally { await dropLower(); await L2({ tunnelsReset: true, tunnelAccounts: { reset: true } }); }
    });

    // ---- 交接导出（R20） ----
    const openExport = async () => {
      await page.goto(url("#/instances")); await settle();
      await page.click(`.card[data-name="${PACK0}"] [data-act="more"]`);
      await page.click('.dropmenu button:has-text("导出交接包")'); await page.waitForSelector("#tr-mask");
    };
    await run("beta.3 穿透与设置 R20：导出文件夹不在时给「新建并导出」，点了带 mkdir 再发一次、去任务页；上一级不在、从局域网打开时写明原因、弹窗和填的路径留着；「这个包给谁」两项各写清对方导入时看到什么", async () => {
      await L2({ instReset: true, madeDirs: [], local: true });
      try {
        await openExport();
        const notes = await page.$$eval("#tr-mask .tr-mode-note", ns => ns.map(n => n.textContent));
        assert(notes.length === 2 && notes[0].includes("「服主交接（整台交给你）」") && notes[1].includes("「发给朋友，照着开一台」") &&
          (await page.textContent("#tr-mask .tr-modes .hint")).includes("东西完全一样"), "这个包给谁：" + JSON.stringify(notes));
        const NEW = "E:\\Exports\\新文件夹";
        await page.fill("#tr-dest", NEW);
        await page.click("#tr-go"); await settle(400);
        const why = await page.textContent("#tr-dest-err");
        assert(why.includes("找不到这个文件夹") && why.includes("只建最后一级") && await page.isVisible("#tr-mkdir"), "找不到时：" + why);
        assert(await toastHas("无法导出交接包"), "第一次应有「无法导出交接包」的提示");
        let st = await L2({});
        assert(!st.lastTransferMkdir, "还没点「新建并导出」时不该建");
        await page.click("#tr-mkdir"); await settle(700);
        st = await L2({});
        assert(st.lastTransferMkdir === NEW && st.lastTransferExport && st.lastTransferExport.mkdir === true && st.lastTransferExport.dest === NEW, "新建并导出的请求：" + JSON.stringify(st.lastTransferExport));
        assert(page.url().endsWith("#/task/task-transfer-out") && await toastHas("已新建导出文件夹"), "新建以后去任务页：" + page.url());
        // 上一次的「无法导出交接包」收掉了，不和成功的提示叠在一起（回访走查 11-16）
        assert(await page.locator(".toast.error:not(.leave)", { hasText: "无法导出交接包" }).count() === 0, "重试成功以后上一次的报错提示应收掉");
        // 上一级不在：写明原因，弹窗和填的路径留着
        await openExport();
        await page.fill("#tr-dest", "Z:\\没有的盘\\新文件夹");
        await page.click("#tr-go"); await settle(400);
        await page.click("#tr-mkdir"); await settle(400);
        let err = await page.textContent("#tr-dest-err");
        assert(err.includes("上一级文件夹「Z:\\没有的盘」不在") && await page.isVisible("#tr-mask") && await page.inputValue("#tr-dest") === "Z:\\没有的盘\\新文件夹", "上一级不在：" + err);
        await page.click("#tr-cancel"); await settle(300);
        // 从局域网打开：主机路径只许本机指定，建不了，说清楚
        await L2({ local: false });
        await openExport();
        await page.fill("#tr-dest", "E:\\局域网\\新文件夹");
        await page.click("#tr-go"); await settle(400);
        await page.click("#tr-mkdir"); await settle(400);
        err = await page.textContent("#tr-dest-err");
        assert(err.includes("仅支持在本机操作") && await page.isVisible("#tr-mask"), "局域网：" + err);
        assert(!(await L2({})).madeDirs.includes("E:\\局域网\\新文件夹"), "局域网不该建出文件夹");
        await page.click("#tr-cancel"); await settle(300);
      } finally { await L2({ instReset: true, madeDirs: [], local: true }); }
    });

    // ---- 设置和侧栏（R15、R16、R17、R19、R20 的存储位置、U29） ----
    const putCfg = b => page.evaluate(x => fetch("/api/config", { method: "PUT", headers: { "X-Token": "mock" }, body: JSON.stringify(x) }), b);
    const cfgNow = async () => (await L2({})).config;

    await run("beta.3 穿透与设置 R17：侧栏底部有「退出 MineDeck」，确认后才退出；「设置 → 启动与托盘」的开关叫「点关闭按钮时缩到托盘（不退出）」，写明第一次缩到托盘会提醒；「更新与关于」里的退出按钮写后果，和侧栏的不重名", async () => {
      let shut = 0;
      const stub = route => { shut++; return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ok: true, data: "ok" }) }); };
      await page.route("**/api/shutdown", stub);
      try {
        await page.goto(url("#/instances")); await settle(500);
        const q = byName("button", "退出 MineDeck");
        assert(await q.count() === 1 && await q.isVisible() && (await q.textContent()).trim() === "退出 MineDeck", "侧栏底部的退出");
        assert(await page.$eval("#sb-quit", b => !!b.closest(".sb-foot")), "退出应在侧栏底部");
        await q.click(); await page.waitForSelector(".modal");
        assert((await page.textContent(".modal h3")).includes("退出 MineDeck？"), "要先确认");
        await page.click('.modal [data-x="cancel"]'); await settle(300);
        assert(shut === 0, "取消了不该退出");
        await page.goto(url("#/settings/startup")); await settle(500);
        const sw = await page.evaluate(() => { const i = document.querySelector('[data-cfg="minimizeToTray"]'), row = i.closest(".row-item");
          return { title: row.querySelector(".ri-title").textContent, aria: i.getAttribute("aria-label"), sub: row.querySelector(".ri-sub").textContent }; });
        assert(sw.title === "点关闭按钮时缩到托盘（不退出）" && sw.aria === sw.title && sw.sub.includes("第一次缩到托盘时会弹一条系统通知") && sw.sub.includes("左下角的「退出」"), "开关：" + JSON.stringify(sw));
        await page.goto(url("#/settings/about")); await settle(500);
        assert((await page.textContent("#quit-app")).trim() === "停止服务器并退出" && await byName("button", "退出 MineDeck").count() === 1, "更新与关于里的退出按钮：" + await page.textContent("#quit-app"));
        // 从侧栏真的退出：确认以后请后端退出
        await byName("button", "退出 MineDeck").click(); await page.waitForSelector(".modal");
        await page.click('.modal [data-x="ok"]'); await settle(400);
        assert(shut === 1 && (await page.textContent("body")).includes("正在停止服务器并退出"), "确认以后退出：" + shut);
      } finally { await page.unroute("**/api/shutdown", stub); await page.goto(url("#/instances")); await settle(300); }
    });

    await run("beta.3 穿透与设置 R15/R16/R20/U29：「设置 → 存储位置」（#/settings/storage）里改备份保留份数；改存放文件夹马上生效、提示里能撤销（原来是默认就换回默认）；服务器和备份设成同一个文件夹时提示；备份那一行写所有备份都放在这里", async () => {
      await putCfg({ serversDir: "", backupsDir: "", backupKeep: 10 });
      try {
        await page.goto(url("#/settings/storage")); await settle(500);
        assert(await page.isVisible("#set-backup-keep") && await page.inputValue("#set-bk-keep") === "10", "保留份数在存储位置里");
        const bkRow = await page.$eval('.row-item:has([data-pick="backups"]) .ri-sub', e => e.textContent);
        assert(bkRow.includes("所有备份都放在这里") && !bkRow.includes("服务器开着时做的备份"), "备份那一行：" + bkRow);
        await page.fill("#set-bk-keep", "7");
        await byName("button", "保存备份保留份数").click(); await settle(300);
        assert((await cfgNow()).backupKeep === 7 && await toastHas("保留最近 7 份"), "保留份数存下了：" + (await cfgNow()).backupKeep);
        // 改备份文件夹：马上生效，提示里能撤销；原来是默认，撤销后换回默认（配置里是空串，不是把默认路径写进去）
        await L2({ pickDir: "E:\\MC备份" });
        await byName("button", "更改备份存放文件夹…").click(); await settle(400);
        assert((await cfgNow()).backupsDir === "E:\\MC备份" && (await page.$eval('.row-item:has([data-pick="backups"]) .ri-key', e => e.textContent)) === "E:\\MC备份", "改了马上生效");
        assert(!(await page.$("#st-same")), "不同的文件夹不该提示");
        await byName("button", "撤销：换回原来的备份存放文件夹").last().click(); await settle(400);
        assert((await cfgNow()).backupsDir === "" && (await page.$eval('.row-item:has([data-pick="backups"]) .ri-key', e => e.textContent)) === "D:\\MineDeck\\backups" && await toastHas("已换回原来的备份存放文件夹"),
          "撤销换回默认：" + JSON.stringify((await cfgNow()).backupsDir));
        // 原来是自己选的文件夹：撤销换回那一个
        await L2({ pickDir: "E:\\服务器A" });
        await byName("button", "更改服务器存放文件夹…").click(); await settle(400);
        await L2({ pickDir: "E:\\服务器B" });
        await byName("button", "更改服务器存放文件夹…").click(); await settle(400);
        assert((await cfgNow()).serversDir === "E:\\服务器B", "改成了 B");
        await byName("button", "撤销：换回原来的服务器存放文件夹").last().click(); await settle(400);
        assert((await cfgNow()).serversDir === "E:\\服务器A", "撤销换回 A：" + (await cfgNow()).serversDir);
        // 恢复默认也能撤销
        await byName("button", "恢复默认的服务器存放文件夹").click(); await settle(400);
        assert((await cfgNow()).serversDir === "", "恢复默认");
        await byName("button", "撤销：换回原来的服务器存放文件夹").last().click(); await settle(400);
        assert((await cfgNow()).serversDir === "E:\\服务器A", "撤销恢复默认：" + (await cfgNow()).serversDir);
        // 服务器和备份设成同一个文件夹：提示（大小写、结尾的斜杠不同也算）
        await L2({ pickDir: "e:\\服务器a\\" });
        await byName("button", "更改备份存放文件夹…").click(); await settle(400);
        assert(await page.isVisible("#st-same") && (await page.textContent("#st-same")).includes("同一个文件夹"), "同一个文件夹时要提示");
        await byName("button", "撤销：换回原来的备份存放文件夹").last().click(); await settle(400);
        assert(!(await page.$("#st-same")), "撤销以后提示消失");
      } finally { await L2({ pickDir: null }); await putCfg({ serversDir: "", backupsDir: "", backupKeep: 10 }); }
    });

    await run("beta.3 穿透与设置 R19：「设置 → AI」页顶用三行写清 AI 助手、游戏内 AI、AI 伙伴各在哪儿用，带去服务器列表的链接；额度和设置里都叫「AI 助手」", async () => {
      await page.goto(url("#/settings/ai")); await settle(700);
      const ov = await page.evaluate(() => ({ titles: [...document.querySelectorAll("#ai-overview .ri-title")].map(e => e.textContent),
        text: document.querySelector("#ai-overview").textContent, go: document.querySelector(".ai-overview-go a").getAttribute("href"),
        first: document.querySelector("#set-body").firstElementChild.textContent }));
      assert(JSON.stringify(ov.titles) === JSON.stringify(["AI 助手", "游戏内 AI", "AI 伙伴"]) && ov.text.includes("标题栏右上角") &&
        ov.text.includes("「扩展 → 游戏内 AI」") && ov.text.includes("「扩展 → AI 伙伴」") && ov.go === "#/instances" && ov.first.includes("AI 能做的三件事"), "总览：" + JSON.stringify(ov));
      const body = await page.textContent("#set-body");
      assert(!body.includes("应用内助手") && await byName("spinbutton", "AI 助手每日额度（万 token）").count() === 1 &&
        (await page.textContent("#ai-assist-slot .set-sec-head h2")).trim() === "AI 助手", "统一叫「AI 助手」");
      await page.click(".ai-overview-go a"); await settle(400);
      assert(page.url().endsWith("#/instances"), "链接去服务器列表：" + page.url());
    });

    await run("beta.3 穿透与设置：AI 助手的发送预览和 MCP 的「复制配置」叠在已开的弹窗上面；Esc 只关上面那层，下面的弹窗和填的字还在；MCP 弹窗的读屏名字加在它自己的按钮上", async () => {
      await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-x" }); await L2({ asst: { reset: true }, mcp: { reset: true, enabled: true } });
      try {
        // 发送预览（assistant.js 的 sheet）
        await page.goto(url("#/inst/" + encodeURIComponent(PACK0) + "/console")); await settle(800);
        await lowerLayer();
        await page.$eval('.diag-banner [data-x="ai"]', b => b.click()); await page.waitForSelector(".modal.ap-pv");
        let ls = await layers();
        assert(ls.length === 2 && ls[0] === "lower-mask", "发送预览应叠在上面：" + JSON.stringify(ls));
        await page.mouse.click(20, 820); await settle(300);
        assert((await layers()).length === 2, "预览里有要填的提问，点遮罩不关");
        await page.keyboard.press("Escape"); await settle(400);
        assert(JSON.stringify(await layers()) === JSON.stringify(["lower-mask"]) && await lowerKept(), "Esc 只关发送预览，下面的还在");
        await dropLower();
        // MCP 的复制配置（mcp.js：confirmModal 上的读屏名字）
        await page.goto(url("#/settings/ai")); await settle(800);
        await page.click("#mcp-copy"); await page.waitForSelector(".dropmenu");
        await lowerLayer();
        await page.locator(".dropmenu button", { hasText: "Cherry Studio" }).dispatchEvent("click"); await page.waitForSelector(".modal .mcp-pre");
        ls = await layers();
        const names = await page.evaluate(() => ({ lower: document.querySelector('#lower-mask [data-x="ok"]').getAttribute("aria-label"),
          top: [...document.querySelectorAll('#modal-root > .modal-mask')].pop().querySelector('[data-x="ok"]').getAttribute("aria-label") }));
        assert(ls.length === 2 && ls[0] === "lower-mask" && names.top === "复制这段配置（Cherry Studio）" && names.lower === null, "MCP 弹窗：" + JSON.stringify({ ls, names }));
        await page.keyboard.press("Escape"); await settle(400);
        assert(JSON.stringify(await layers()) === JSON.stringify(["lower-mask"]) && await lowerKept(), "Esc 只关 MCP 弹窗，下面的还在");
      } finally { await dropLower(); await L2({ asst: { reset: true }, mcp: { reset: true } }); await aiState({ reset: true }); }
    });
  }
  // ==================== beta.3 内网穿透与设置结束 ====================

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
      // Mac 上 Ctrl 点是右键（只发 contextmenu，不发 click），对应 Windows Ctrl 点的是 Cmd 点；ControlOrMeta 在 Windows 上就是 Ctrl
      await p.click(EXT_LINK, { modifiers: ["ControlOrMeta"] });
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
      // 页面没拦（没 preventDefault），浏览器才会自己开：在 window 上记下点击（排在页面自己在捕获阶段的处理后面）
      await p.evaluate(() => {
        window.__linkEv = [];
        for (const t of ["click", "auxclick"]) {
          addEventListener(t, e => { if (e.target instanceof Element && e.target.closest("a")) window.__linkEv.push(t + (e.defaultPrevented ? "（被拦）" : "")); });
        }
      });
      for (const opts of [{}, { button: "middle" }]) {
        const [pop] = await Promise.all([c.waitForEvent("page"), p.click(EXT_LINK, opts)]);
        // 中键开的是后台标签页，各平台、各版本的 Chromium 不一样（Mac 上的无头 Chromium 会让它直接崩掉，完整版又绕过拦截真的上网），
        // 所以不等它加载，只看左键开的那个窗口的地址
        if (!opts.button) {
          await pop.waitForLoadState();
          assert(pop.url() === "https://www.openfrp.net/", "新窗口的地址：" + pop.url());
        }
        await pop.close();
      }
      const ev = await p.evaluate(() => window.__linkEv);
      assert(JSON.stringify(ev) === JSON.stringify(["click", "auxclick"]), "局域网浏览器里点链接不该被页面拦下：" + JSON.stringify(ev));
      assert(p.url().endsWith("#/settings/about"), "主窗口跳走了：" + p.url());
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
      await w.p.locator("#mc-side").scrollIntoViewIfNeeded(); // beta.3 R10 起清单上面多了「已安装」，一进来清单可能在可视区下面
      const cart = await w.drag("#mc-side", FILE);
      // 旧地址 cart 原地换成了 addons（R06）
      assert(cart.includes("dragover:true:copy") && cart.some(x => x.startsWith("drop:true")) && decodeURIComponent(w.p.url()).includes(PACK + "/addons"),
        "待装清单应照旧接住拖进来的文件：" + JSON.stringify({ cart, url: w.p.url() }));
    } finally { await w.c.close(); }
    const lan = await open(false);
    try {
      const got = await lan.drag(".set-sec-head h2", { items: LINK });
      assert(got.every(x => !x.startsWith("dragover:true")), "局域网浏览器里不该拦拖放：" + JSON.stringify(got));
    } finally { await lan.c.close(); }
  });

  // ==================== 地图导入（worldimport.js）====================
  // 仿真后端的来源是按名字装样子的（bedrock / multi / newer / 1193 / plain / xss…），见 server.js「地图导入」那一节的说明
  const wState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
  const wReset = () => wState({ instReset: true, local: true, world: { reset: true } });
  const worldTab = name => `#/inst/${encodeURIComponent(name)}/world/map`; // beta.3 R06：「地图」标签成了「世界与备份」的「换地图」子页
  const wZip = name => ({ name, mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mock-world") });
  const wPosts = (since, path) => reqs.slice(since).filter(r => r.m === "POST" && r.u === path).length;
  // 在「选来源」那一步填一个本机路径并检测
  const wScan = async pth => { await page.fill("#wi-path", pth); await page.click("#wi-scan"); };
  // 打开某台服务器的「地图」标签并点「导入并替换当前地图」，停在选来源
  const wOpenFlow = async name => {
    await page.goto(url(worldTab(name))); await page.waitForSelector("#wi-start");
    await page.click("#wi-start"); await page.waitForSelector("#wi-upload");
  };
  const WI_STUCK = { busy: "上次换地图没能自动恢复，服务器已暂停启动。请到「世界与备份 → 换地图」里查看恢复步骤",
    points: [{ id: "w9aa10001", state: "stuck", created: "2026-11-12T20:10:00+08:00", source: "SkyCity.zip", candidate: "天空之城", levelName: "world", hasOld: true, oldBytes: 5e8, keptBytes: 0, canReturn: false,
      error: "rename world: Access is denied.", stuck: ["D:\\MineDeck\\servers\\create-pack\\world", "D:\\MineDeck\\servers\\create-pack\\.minedeck\\world-import\\w9aa10001\\old\\world"] }] };

  // beta.3 R06：「地图」标签成了「世界与备份」标签下的「换地图」子页。代理端没有「世界与备份」，直接打开它的地址落到控制台（不再单独说明）
  await run("地图：游戏服务器的「世界与备份」里有「换地图」，代理端没有这个标签，直接打开地址落到控制台", async () => {
    await wReset();
    for (const [name, want] of [["生存服 · 朋友", true], [PACK, true], ["原版快照测试", true], ["群组代理", false]]) {
      await page.goto(url(`#/inst/${encodeURIComponent(name)}/console`)); await page.waitForSelector(".tabs .tab");
      const has = await page.$$eval('.tabs .tab[data-t="world"]', els => els.map(e => e.textContent.trim()));
      assert(want ? has.length === 1 && has[0] === "世界与备份" : has.length === 0, `${name} 的标签：${JSON.stringify(has)}`);
    }
    await page.goto(url(worldTab("群组代理"))); await page.waitForSelector("#console");
    assert(page.url().endsWith("/console") && !(await page.$("#wi-start")), "代理端的地图地址：" + page.url());
    await page.goto(url("#/inst/" + inst + "/console")); await page.waitForSelector('.tabs .tab[data-t="world"]');
    await page.click('.tabs .tab[data-t="world"]'); await page.waitForSelector('.sub-tabs .tab[data-t="map"]');
    await page.click('.sub-tabs .tab[data-t="map"]');
    await page.waitForSelector("#wi-start");
    assert(page.url().endsWith("/world/map") && (await page.$eval('.tabs .tab[data-t="world"]', a => a.classList.contains("cur"))) &&
      (await page.$eval('.sub-tabs .tab[data-t="map"]', a => a.classList.contains("cur"))), "点子页没有进换地图：" + page.url());
  });

  await run("地图：当前世界和恢复点列表（哪次导入、现在什么状态、占多大），只有最近一次导入有「恢复导入前的地图」", async () => {
    await wReset();
    await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-points [data-wi-point]");
    const v = await page.evaluate(() => ({ level: document.querySelector("#wi-level").textContent, status: document.querySelector("#wi-status").textContent,
      start: !document.querySelector("#wi-start").disabled, busy: !!document.querySelector("#wi-busy"),
      rows: [...document.querySelectorAll("#wi-points [data-wi-point]")].map(r => ({ state: r.dataset.state, text: r.textContent.replace(/\s+/g, " "),
        restore: !!r.querySelector("[data-wi-restore]"), del: !!r.querySelector("[data-wi-del]") })) }));
    assert(v.level === "world" && v.status === "已停止" && v.start && !v.busy, "当前世界：" + JSON.stringify(v));
    assert(v.rows.length === 4, "恢复点条数：" + v.rows.length);
    const [a, old, b, c] = v.rows;
    assert(old.state === "committed" && !old.restore && old.del && old.text.includes("单方块空岛") && old.text.includes("更早的导入") && old.text.includes("要先换回最近那一次"), "更早的那次导入：" + JSON.stringify(old));
    assert(a.state === "committed" && a.restore && a.del && a.text.includes("天空之城") && a.text.includes("已导入") && a.text.includes("SkyCity.zip") && a.text.includes("1.3 GB"), "能换回的那条：" + JSON.stringify(a));
    assert(b.state === "restored" && !b.restore && b.del && b.text.includes("三年老存档") && b.text.includes("已换回") && b.text.includes("640.0 MB"), "已换回的那条：" + JSON.stringify(b));
    assert(c.state === "rolled-back" && !c.restore && c.del && c.text.includes("没有生效") && c.text.includes("已取消，没有替换任何文件"), "没生效的那条：" + JSON.stringify(c));
    await page.goto(url(worldTab("生存服 · 朋友"))); await page.waitForSelector("#wi-points");
    const none = await page.$eval("#wi-points", el => el.textContent);
    assert(none.includes("还没有恢复点") && (await page.$eval("#wi-status", el => el.textContent)) === "运行中", "没有恢复点的服务器：" + none);
  });

  await run("地图导入：本机选文件夹 → 核对页（版本关系、地图没提供的维度、会换掉什么、设置建议默认不勾）→ 确认了才点得动 → 危险确认框 → 任务进度（复制阶段才能取消，之后取消键变灰并写出原因）→ 结果；任务结束前不清检测结果", async () => {
    const NAME = "生存服 · 朋友";
    await wReset();
    try {
      await wOpenFlow(NAME);
      assert((await page.$("#wi-browse")) && (await page.$("#wi-path")), "本机上应有「选择地图文件夹」和路径输入框");
      await page.click("#wi-browse"); await page.waitForSelector("#wi-go");
      let st = await wState({});
      const scanId = st.world.lastScan.id, planId = st.world.lastPlan.id;
      assert(st.world.lastScan.name === "MCServers" && st.world.lastPlan.inst === NAME && !st.world.lastPlan.newInst, "检测和计划的请求：" + JSON.stringify([st.world.lastScan, st.world.lastPlan]));
      const v = await page.evaluate(() => { const q = s => document.querySelector(s), tx = s => (q(s) ? q(s).textContent.replace(/\s+/g, " ") : ""); return {
        name: tx("#wi-map-name"), rel: q("#wi-rel").dataset.rel, relText: tx("#wi-rel"), missing: tx("#wi-missing"), replace: tx("#wi-replace"), kept: tx("#wi-kept"),
        acks: document.querySelectorAll("#wi-stage input[type=checkbox]#wi-ack").length, confirmBoxes: document.querySelectorAll("#wi-confirm input[type=checkbox]").length,
        sets: [...document.querySelectorAll("[data-wi-set]")].map(x => [x.dataset.wiSet, x.checked]), setText: tx("#wi-settings"),
        stop: !!q("#wi-stop") && !q("#wi-stop").checked, warn: tx("#wi-warn"), block: !!q("#wi-block"), space: tx("#wi-space"),
        go: q("#wi-go").disabled, why: tx("#wi-go-why"), details: document.querySelectorAll("#wi-stage details.wi-detail").length,
        open: document.querySelectorAll("#wi-stage details.wi-detail[open]").length }; });
      assert(v.name === "天空之城" && v.rel === "older" && v.relText.includes("1.19.4") && v.relText.includes("Paper 1.21.4") && v.relText.includes("升级"), "版本关系：" + JSON.stringify(v));
      assert(v.missing.includes("末地") && v.missing.includes("不是导丢了"), "没提供的维度：" + v.missing);
      assert(v.replace.includes("world_nether") && v.replace.includes("恢复点") && v.kept.includes("白名单") && v.kept.includes("端口"), "换掉什么、保留什么：" + v.replace + " | " + v.kept);
      assert(v.acks === 1 && v.confirmBoxes === 1, `「我已了解」的勾选框应只有一个：${v.acks} / ${v.confirmBoxes}`);
      assert(JSON.stringify(v.sets) === JSON.stringify([["difficulty", false], ["gamemode", false]]) && v.setText.includes("简单") && v.setText.includes("普通") && v.setText.includes("冒险"), "设置建议应默认不勾：" + JSON.stringify(v.sets) + v.setText);
      assert(v.stop && v.warn.includes("单人世界") && v.warn.includes("资源包") && !v.block && v.space.includes("118"), "停服勾选、提醒、空间：" + JSON.stringify(v));
      assert(v.go && v.why.includes("我已了解") && v.details >= 2 && v.open === 0, "没确认时导入键应点不了，技术细节收在「详情」里：" + JSON.stringify(v));
      const ax = await axProblems();
      assert(!ax.length, "核对页的无障碍：" + ax.join("、"));
      await page.check("#wi-ack");
      assert((await page.$eval("#wi-go", b => b.disabled)) && (await page.$eval("#wi-go-why", el => el.textContent)).includes("停服并导入"), "服务器开着、没勾停服时还应点不了");
      await page.check("#wi-stop");
      assert(!(await page.$eval("#wi-go", b => b.disabled)), "都勾了以后应能点");
      await page.check('[data-wi-set="gamemode"]');
      // 危险确认框：标题点名、默认停在「取消」，取消不发请求
      const t0 = reqs.length;
      await page.click("#wi-go"); await page.waitForSelector(".modal");
      const m = await page.evaluate(() => ({ title: document.querySelector("#modal-title").textContent.trim(), body: document.querySelector(".modal .m-body").textContent,
        focus: document.activeElement.textContent.trim(), danger: !!document.querySelector('.modal [data-x="ok"].danger.solid') }));
      assert(m.title === "导入并替换当前地图" && m.focus === "取消" && m.danger && m.body.includes("world_the_end") && m.body.includes("天空之城") && m.body.includes("默认游戏模式") && m.body.includes("先停止"), "确认框：" + JSON.stringify(m));
      await page.click('.modal [data-x="cancel"]'); await settle(300);
      assert(wPosts(t0, "/api/world/import") === 0, "取消确认框不应发导入请求");
      await page.click("#wi-go"); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector("#wi-cancel");
      st = await wState({});
      const li = st.world.lastImport, taskId = Object.keys(st.world.tasks)[0];
      assert(li.planId === planId && li.confirm === "replace" && li.acknowledge === true && li.stopFirst === true && JSON.stringify(li.settings) === JSON.stringify(["gamemode"]), "导入请求：" + JSON.stringify(li));
      assert((await page.$eval("#wi-task-title", el => el.textContent)).includes(NAME) && st.world.discards.length === 0, "任务标题或提前清了检测结果：" + JSON.stringify(st.world.discards));
      // 切到别的标签再回来：接着看同一个任务，检测结果还在
      await page.click('.tabs .tab[data-t="console"]'); await page.waitForSelector("#console");
      // beta.3 R06：「地图」成了「世界与备份」下的「换地图」子页，点标签进的是「备份」，再点子页
      await page.click('.tabs .tab[data-t="world"]'); await page.click('.sub-tabs .tab[data-t="map"]'); await page.waitForSelector("#wi-task");
      // 开始停服、换地图以后不能取消：取消键变灰，旁边写着后端给的原因（A53，以前是整个键收掉、写一句固定的话）
      await page.waitForSelector("#wi-cancel[disabled]", { timeout: 15000 });
      const late = await page.$eval("#wi-task-acts", el => el.textContent);
      assert((late.includes("不能取消") || late.includes("不能停")) && (await page.$eval("#wi-cancel", b => b.getAttribute("aria-describedby"))) === "wi-cancel-why",
        "换地图阶段取消键应变灰并写出原因：" + late);
      assert((await wState({})).world.discards.length === 0, "任务还在跑就清了检测结果");
      await page.waitForSelector("#wi-task-done-text", { timeout: 15000 });
      const done = await page.$eval("#wi-task-done-text", el => el.textContent);
      assert(done.includes("地图文件已导入，还没开服试过"), "结果应是任务给的那句话：" + done);
      await settle(300);
      st = await wState({});
      assert(st.world.discards.includes(scanId) && st.world.cancels.length === 0, "任务结束后应清掉检测结果：" + JSON.stringify(st.world.discards));
      await page.click("#wi-task-ok"); await page.waitForSelector("#wi-points [data-wi-point]");
      const row = await page.$eval("#wi-points [data-wi-point]", r => ({ state: r.dataset.state, text: r.textContent, restore: !!r.querySelector("[data-wi-restore]") }));
      assert(row.state === "committed" && row.text.includes("天空之城") && row.restore && (await page.$eval("#wi-status", el => el.textContent)) === "已停止", "导入后的恢复点：" + JSON.stringify(row));
      // 同一个任务在任务页：结果是一句话，不是服务器名；「完成」按快照里的 inst 回到这台服务器的「换地图」（A53，以前固定回服务器列表）
      await page.goto(url("#/task/" + taskId)); await page.waitForSelector("#t-foot a.btn.primary");
      const tp = await page.evaluate(() => ({ h1: document.querySelector(".page-head h1").textContent, sub: document.querySelector("#t-sub").textContent,
        href: document.querySelector("#t-foot a.btn.primary").getAttribute("href"), go: document.querySelector("#t-foot a.btn.primary").textContent.trim() }));
      assert(tp.h1 === "地图已处理完" && tp.sub.includes("还没开服试过") && tp.href === worldTab(NAME) && tp.go.includes("回到这台服务器"), "任务页：" + JSON.stringify(tp));
      await page.click("#t-foot a.btn.primary"); await page.waitForSelector("#wi-points [data-wi-point]");
      assert(decodeURIComponent(page.url()).endsWith(`#/inst/${NAME}/world/map`), "点「完成」应回到这台服务器的换地图：" + page.url());
    } finally { await wReset(); }
  });

  await run("地图导入：复制阶段点「取消导入」会停下、没有替换任何文件；没完成时可以回去重新核对同一张地图，放弃时才清掉检测结果", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      await wScan("D:\\Maps\\plain-world"); await page.waitForSelector("#wi-go");
      const v = await page.evaluate(() => ({ rel: document.querySelector("#wi-rel").dataset.rel, ack: !!document.querySelector("#wi-ack"), stop: !!document.querySelector("#wi-stop"),
        go: document.querySelector("#wi-go").disabled, sets: document.querySelectorAll("[data-wi-set]").length, readme: !!document.querySelector("#wi-readmes") }));
      assert(v.rel === "same" && !v.ack && !v.stop && !v.go && !v.readme, "同版本、没有要确认的项时不该有勾选框，导入键直接能点：" + JSON.stringify(v));
      let st = await wState({});
      const scanId = st.world.lastScan.id, plan1 = st.world.lastPlan.id;
      await page.click("#wi-go"); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector("#wi-cancel");
      await page.click("#wi-cancel");
      // beta.3：取消干净结束时是中性的「已取消」说明，不是红色的报错框；步骤画横线（cancelled），不画红叉
      await page.waitForSelector("#wi-task-cancelled", { timeout: 15000 });
      const errText = await page.$eval("#wi-task-cancelled", el => el.textContent);
      const noErr = !(await page.$("#wi-task-err")), dash = await page.$$eval("#wi-steps .task-step.cancelled .ts-dash", els => els.length);
      st = await wState({});
      assert(errText.includes("已取消换地图") && errText.includes("没有替换任何文件") && noErr && dash === 1 && st.world.cancels.length === 1 && st.world.discards.length === 0,
        "取消以后：" + errText + JSON.stringify([noErr, dash, st.world.cancels, st.world.discards]));
      await page.click("#wi-task-again"); await page.waitForSelector("#wi-go");
      st = await wState({});
      assert(st.world.lastPlan.scanId === scanId && st.world.lastPlan.id !== plan1 && (await page.$eval("#wi-map-name", el => el.textContent)) === "新的世界", "重新核对应还是那张地图、一份新计划：" + JSON.stringify(st.world.lastPlan));
      await page.click("#wi-quit"); await page.waitForSelector("#wi-start");
      await settle(300);
      st = await wState({});
      assert(st.world.discards.includes(scanId), "放弃导入应清掉检测结果：" + JSON.stringify(st.world.discards));
      // 没走到导入就离开这一页，也清掉
      await wOpenFlow(PACK);
      await wScan("D:\\Maps\\plain-world"); await page.waitForSelector("#wi-go");
      const scan2 = (await wState({})).world.lastScan.id;
      await page.click('.tabs .tab[data-t="console"]'); await page.waitForSelector("#console"); await settle(400);
      assert((await wState({})).world.discards.includes(scan2), "切走以后应清掉没用上的检测结果");
    } finally { await wReset(); }
  });

  await run("地图导入：地图比服务器新时标红、导入键点不了，也发不出导入请求；磁盘空间不够同样拦下", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      await wScan("D:\\Maps\\newer-map"); await page.waitForSelector("#wi-go");
      const v = await page.evaluate(() => ({ rel: document.querySelector("#wi-rel").dataset.rel, relText: document.querySelector("#wi-rel").textContent, block: (document.querySelector("#wi-block") || {}).textContent || "",
        go: document.querySelector("#wi-go").disabled, why: document.querySelector("#wi-go-why").textContent, bad: document.querySelector("#wi-rel").classList.contains("bad") }));
      assert(v.rel === "newer" && v.bad && v.relText.includes("不能导入") && v.block.includes("26.2") && v.block.includes("现在不能导入") && v.go && v.why.includes("标红"), "被拦下的核对页：" + JSON.stringify(v));
      const t0 = reqs.length;
      await page.$eval("#wi-go", b => b.click()); await settle(400);
      assert(!(await page.$(".modal")) && wPosts(t0, "/api/world/import") === 0, "被拦下时不该弹确认框，也不该发导入请求");
      await wState({ world: { free: 100 * 1024 * 1024 } });
      await page.click("#wi-back"); await page.waitForSelector("#wi-scan");
      await wScan("D:\\Maps\\plain-world"); await page.waitForSelector("#wi-go");
      const d = await page.evaluate(() => ({ block: (document.querySelector("#wi-block") || {}).textContent || "", go: document.querySelector("#wi-go").disabled }));
      assert(d.block.includes("磁盘空间不够") && d.go, "空间不够：" + JSON.stringify(d));
    } finally { await wReset(); }
  });

  await run("地图导入：一个压缩包里有几张地图时列出来让人选，读不出来的那张选不了；选哪张就按哪张核对，可以回去换一张", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      const t0 = reqs.length;
      await wScan("D:\\Maps\\multi-pack.zip"); await page.waitForSelector("[data-wi-pick]");
      const rows = await page.$$eval("[data-wi-cand]", els => els.map(r => ({ id: r.dataset.wiCand, text: r.textContent.replace(/\s+/g, " "), off: r.querySelector("[data-wi-pick]").disabled,
        label: r.querySelector("[data-wi-pick]").getAttribute("aria-label") })));
      assert(rows.length === 3 && wPosts(t0, "/api/world/plan") === 0, "应列出三张地图、还没开始核对：" + JSON.stringify(rows));
      assert(rows[0].text.includes("冒险之旅") && rows[0].text.includes("maps/adventure") && rows[0].text.includes("1.20.4") && rows[0].text.includes("380.0 MB") && !rows[0].off, "第一张：" + JSON.stringify(rows[0]));
      assert(rows[1].text.includes("survival") && rows[1].text.includes("三个文件夹") && rows[1].text.includes("survival_nether") && !rows[1].off, "第二张：" + JSON.stringify(rows[1]));
      assert(rows[2].off && rows[2].text.includes("不能导入") && rows[2].text.includes("level.dat 读不出来"), "读不出来的那张：" + JSON.stringify(rows[2]));
      assert(new Set(rows.map(r => r.label)).size === 3 && rows[0].label.includes("冒险之旅"), "「用这张」的名字应带上是哪张：" + JSON.stringify(rows.map(r => r.label)));
      const ax = await axProblems();
      assert(!ax.length, "候选列表的无障碍：" + ax.join("、"));
      await page.click('[data-wi-pick="c2"]'); await page.waitForSelector("#wi-go");
      let st = await wState({});
      assert(st.world.lastPlan.candidate === "c2" && (await page.$eval("#wi-map-name", el => el.textContent)) === "survival", "应按选的那张核对：" + JSON.stringify(st.world.lastPlan));
      assert((await page.$eval("#wi-back", b => b.textContent)).includes("换一张地图"), "多张地图时返回键应是「换一张地图」");
      await page.click("#wi-back"); await page.waitForSelector("[data-wi-pick]");
      await page.click('[data-wi-pick="c1"]'); await page.waitForSelector("#wi-go");
      st = await wState({});
      assert(st.world.lastPlan.candidate === "c1" && st.world.discards.length === 0 && (await page.$eval("#wi-map-name", el => el.textContent)) === "冒险之旅", "换一张以后：" + JSON.stringify([st.world.lastPlan, st.world.discards]));
    } finally { await wReset(); }
  });

  await run("地图导入：基岩版、RAR 这些不是能导入的地图，原因摆在最前面、到此为止；别的设备上只能上传 ZIP，上传有进度", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      const t0 = reqs.length;
      await wScan("D:\\Maps\\bedrock-world"); await page.waitForSelector("#wi-reject");
      const rej = await page.$eval("#wi-reject", el => el.textContent.replace(/\s+/g, " "));
      const scanId = (await wState({})).world.lastScan.id;
      assert(rej.includes("这是基岩版的世界，不能当地图导入") && rej.includes("改扩展名转不过来") && !(await page.$("#wi-go")) && wPosts(t0, "/api/world/plan") === 0, "基岩版：" + rej);
      await page.click("#wi-again"); await page.waitForSelector("#wi-scan"); await settle(300);
      assert((await wState({})).world.discards.includes(scanId), "换一个来源应清掉上一次的检测结果");
      // 没填路径、填了相对路径：就近说明
      await page.click("#wi-scan");
      assert((await page.$eval("#wi-path-err", el => el.textContent)).includes("完整路径"), "没填路径应在输入框下面提示");
      await wScan("Maps\\SkyCity"); await page.waitForSelector("#wi-src-state .err-box");
      assert((await page.$eval("#wi-src-state .err-box", el => el.textContent)).includes("请选一个完整的路径"), "相对路径应说明原因");
      // RAR 在上传之前就说，不白传
      const t1 = reqs.length;
      await page.setInputFiles("#wi-file", { name: "map.rar", mimeType: "application/octet-stream", buffer: Buffer.from("Rar!") }); await settle(300);
      assert((await page.$eval("#wi-up", el => el.textContent)).includes("RAR") && wPosts(t1, "/api/world/upload") === 0, "RAR 应在上传前拦下");
      // 上传的 .mcworld：后端认出来以后同样摆出原因
      await page.setInputFiles("#wi-file", { name: "MyWorld.mcworld", mimeType: "application/octet-stream", buffer: Buffer.from("PK\u0003\u0004") });
      await page.waitForSelector("#wi-reject");
      assert((await page.$eval("#wi-reject", el => el.textContent)).includes("基岩版") && wPosts(t1, "/api/world/upload") === 1, "上传的基岩版世界应说明原因");
      // 别的设备：没有选文件夹和路径，只有上传
      await wState({ local: false });
      await wOpenFlow(PACK);
      const lan = await page.evaluate(() => ({ browse: !!document.querySelector("#wi-browse"), path: !!document.querySelector("#wi-path"), note: (document.querySelector(".wi-remote") || {}).textContent || "",
        primary: document.querySelector("#wi-upload").classList.contains("primary") }));
      assert(!lan.browse && !lan.path && lan.note.includes("运行 MineDeck 的那台电脑") && lan.primary, "别的设备上的选来源：" + JSON.stringify(lan));
      await page.evaluate(() => { window.__wiUp = []; new MutationObserver(() => { const el = document.querySelector("#wi-up-pct"); if (el) window.__wiUp.push(el.textContent); })
        .observe(document.querySelector("#tab-body"), { subtree: true, childList: true, characterData: true }); });
      await page.setInputFiles("#wi-file", wZip("SkyCity.zip")); await page.waitForSelector("#wi-go");
      const up = await page.evaluate(() => window.__wiUp);
      const st = await wState({});
      assert(st.world.lastScan.uploaded === true && st.world.lastScan.name === "SkyCity.zip" && up.length > 0 && up.includes("100%"), "上传：" + JSON.stringify([st.world.lastScan, up]));
    } finally { await wReset(); }
  });

  await run("地图导入：像世界但一张都不能用的（只有 level.dat_old），拒绝页把候选里写的具体原因和怎么办摆出来", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      const t0 = reqs.length;
      await wScan("D:\\Maps\\damaged-world"); await page.waitForSelector("#wi-reject");
      const rej = await page.$eval("#wi-reject", el => el.textContent.replace(/\s+/g, " "));
      // 后端把这类原因写在候选自己的 issues 里，顶层是空的：以前拒绝页只画顶层，只剩一句「缺了关键文件」
      assert(rej.includes("这份世界缺了关键文件，不能当地图导入") && rej.includes("只有备份用的 level.dat_old") && rej.includes("改名成 level.dat"),
        "拒绝页应带上候选里写的原因和怎么办：" + rej);
      assert(!(await page.$("#wi-go")) && wPosts(t0, "/api/world/plan") === 0, "不能导入的不该有导入键，也不该去做计划");
    } finally { await wReset(); }
  });

  await run("地图导入：地图名和说明书是别人写的，只当文字显示——夹着的 HTML 不变成标签，网址不变成链接，确认框里也一样", async () => {
    await wReset();
    try {
      await wOpenFlow(PACK);
      await wScan("D:\\Maps\\xss-skycity"); await page.waitForSelector("#wi-go");
      const nm = await page.evaluate(() => ({ text: document.querySelector("#wi-map-name").textContent, kids: document.querySelector("#wi-map-name").children.length }));
      assert(nm.text.includes('<img src=x onerror=') && nm.kids === 0, "地图名应原样当文字：" + JSON.stringify(nm));
      const t0 = reqs.length;
      await page.click('[data-wi-readme="0"]'); await page.waitForSelector(".wi-readme-text:not([hidden])");
      const rd = await page.evaluate(() => { const pre = document.querySelector(".wi-readme-text"); return { text: pre.textContent, kids: pre.children.length,
        links: document.querySelectorAll("#wi-readmes a").length, btn: document.querySelector('[data-wi-readme="0"]').textContent }; });
      assert(rd.text.includes('<img src=x onerror="window.__wixss=1">') && rd.text.includes("<b>不该变粗</b>") && rd.text.includes("https://example.com/skycity") && rd.kids === 0 && rd.links === 0 && rd.btn === "收起",
        "说明书应只当文字：" + JSON.stringify(rd));
      await page.click('[data-wi-readme="0"]');
      assert(await page.$eval(".wi-readme-text", el => el.hidden), "再点一次应收起");
      await page.click('[data-wi-readme="0"]'); await page.waitForSelector(".wi-readme-text:not([hidden])");
      assert(wPosts(t0, "/api/world/readme") === 1, "说明书只取一次：" + wPosts(t0, "/api/world/readme"));
      await page.check("#wi-ack");
      await page.click("#wi-go"); await page.waitForSelector(".modal");
      const md = await page.evaluate(() => ({ text: document.querySelector(".modal .m-body").textContent, imgs: document.querySelectorAll(".modal img, #wi-stage img").length, x: window.__wixss }));
      assert(md.text.includes("<img src=x") && md.imgs === 0 && md.x === undefined, "确认框里的地图名：" + JSON.stringify(md));
      await page.click('.modal [data-x="cancel"]'); await settle(200);
      // 恢复点列表里的来源名、地图名同样只当文字
      await wState({ world: { inst: { [PACK]: { points: [{ id: "wxss00001", state: "committed", created: "2026-11-12T20:10:00+08:00", source: '<img src=x onerror="window.__wixss=2">.zip',
        candidate: '<b>加粗</b><img src=x onerror="window.__wixss=3">', levelName: "world", hasOld: true, oldBytes: 1e8, keptBytes: 0, canReturn: true, error: '<i>x</i>' }] } } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("[data-wi-point]");
      const pt = await page.evaluate(() => ({ text: document.querySelector("[data-wi-point]").textContent, bad: document.querySelectorAll("#wi-points img, #wi-points b, #wi-points i").length, x: window.__wixss }));
      assert(pt.text.includes("<b>加粗</b>") && pt.bad === 0 && pt.x === undefined, "恢复点里的名字：" + JSON.stringify(pt));
      await page.click("[data-wi-restore]"); await page.waitForSelector(".modal");
      const md2 = await page.evaluate(() => ({ imgs: document.querySelectorAll(".modal img, .modal .m-body b").length, x: window.__wixss }));
      assert(md2.imgs === 0 && md2.x === undefined, "换回确认框里的地图名：" + JSON.stringify(md2));
      await page.click('.modal [data-x="cancel"]'); await settle(200);
    } finally { await wReset(); }
  });

  await run("地图：恢复导入前的地图、删除恢复点都要过危险确认框（默认停在「取消」），确认词由界面带上；服务器没停时换回键是灰的并说明", async () => {
    await wReset();
    try {
      await page.goto(url(worldTab(PACK))); await page.waitForSelector('[data-wi-restore="w7f3a9c21"]');
      const t0 = reqs.length, rest = `/api/instances/${PACK}/world/restore`, del = `/api/instances/${PACK}/world/delete`;
      const posted = path => reqs.slice(t0).filter(r => r.m === "POST" && decodeURIComponent(r.u) === path).length;
      await page.click('[data-wi-restore="w7f3a9c21"]'); await page.waitForSelector(".modal");
      const m = await page.evaluate(() => ({ title: document.querySelector("#modal-title").textContent.trim(), body: document.querySelector(".modal .m-body").textContent,
        focus: document.activeElement.textContent.trim(), ok: document.querySelector('.modal [data-x="ok"]').textContent.trim(), danger: !!document.querySelector('.modal [data-x="ok"].danger.solid') }));
      assert(m.title === "恢复导入前的地图？" && m.focus === "取消" && m.danger && m.ok === "换回导入前的地图" && m.body.includes("天空之城") && m.body.includes("不会删") && m.body.includes("停止状态"), "换回的确认框：" + JSON.stringify(m));
      await page.keyboard.press("Enter"); await settle(300);
      assert(!(await page.$(".modal:not(.leave)")) && posted(rest) === 0, "回车（焦点在取消上）不该换回");
      await page.click('[data-wi-restore="w7f3a9c21"]'); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector("#wi-task");
      let st = await wState({});
      assert(JSON.stringify(st.world.lastRestore) === JSON.stringify({ inst: PACK, id: "w7f3a9c21", confirm: "restore" }) && posted(rest) === 1, "换回的请求：" + JSON.stringify(st.world.lastRestore));
      await page.waitForSelector("#wi-task-done-text", { timeout: 15000 });
      assert(!(await page.$("#wi-cancel")) && (await page.$eval("#wi-task-done-text", el => el.textContent)).includes("已经换回导入前的地图"), "换回的结果");
      await page.click("#wi-task-ok"); await page.waitForSelector('[data-wi-point="w7f3a9c21"]');
      const row = await page.$eval('[data-wi-point="w7f3a9c21"]', r => ({ state: r.dataset.state, text: r.textContent, restore: !!r.querySelector("[data-wi-restore]") }));
      assert(row.state === "restored" && row.text.includes("已换回") && !row.restore, "换回以后那一条：" + JSON.stringify(row));
      assert((await page.$$("[data-wi-restore]")).length === 1 && (await page.$('[data-wi-restore="w6e2b8d10"]')), "换回最近一次以后，应轮到更早的那一次能换回");
      // 删除恢复点
      await page.click('[data-wi-del="w3c9d1e02"]'); await page.waitForSelector(".modal");
      const d = await page.evaluate(() => ({ title: document.querySelector("#modal-title").textContent.trim(), body: document.querySelector(".modal .m-body").textContent,
        focus: document.activeElement.textContent.trim(), ok: document.querySelector('.modal [data-x="ok"]').textContent.trim() }));
      assert(d.title === "删除恢复点？" && d.focus === "取消" && d.ok === "删除恢复点" && d.body.includes("删除后无法恢复"), "删除的确认框：" + JSON.stringify(d));
      await page.click('.modal [data-x="cancel"]'); await settle(300);
      assert(posted(del) === 0 && (await page.$('[data-wi-point="w3c9d1e02"]')), "取消不该删除");
      await page.click('[data-wi-del="w3c9d1e02"]'); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector('[data-wi-point="w3c9d1e02"]', { state: "detached" });
      st = await wState({});
      assert(JSON.stringify(st.world.lastDelete) === JSON.stringify({ inst: PACK, id: "w3c9d1e02", confirm: "delete" }), "删除的请求：" + JSON.stringify(st.world.lastDelete));
      // 能换回的那条说明删了就换不回
      await wState({ world: { reset: true } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector('[data-wi-del="w7f3a9c21"]');
      await page.click('[data-wi-del="w7f3a9c21"]'); await page.waitForSelector(".modal");
      assert((await page.$eval(".modal .m-body", el => el.textContent)).includes("不能再换回导入前的地图"), "删能换回的恢复点应说明后果");
      await page.click('.modal [data-x="cancel"]'); await settle(200);
      // 服务器没停：换回键是灰的，旁边说明
      await wState({ status: { [PACK]: "running" } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector('[data-wi-restore="w7f3a9c21"]');
      const run = await page.evaluate(() => ({ off: document.querySelector('[data-wi-restore="w7f3a9c21"]').disabled, del: document.querySelector('[data-wi-del="w7f3a9c21"]').disabled,
        note: (document.querySelector(".wi-need-stop") || {}).textContent || "" }));
      assert(run.off && run.del && run.note.includes("先停止服务器"), "服务器开着时换回和删除都是灰的：" + JSON.stringify(run)); // 后端删恢复点也要先占位，开着就拒
    } finally { await wReset(); }
  });

  await run("地图：换地图卡住时页面顶上说明原因、列出要看的文件夹，「重试恢复」成功后提示消失；别的设备上不显示本机路径", async () => {
    await wReset();
    try {
      await wState({ world: { inst: { [PACK]: WI_STUCK } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-busy");
      const b = await page.evaluate(() => ({ text: document.querySelector("#wi-busy").textContent, items: [...document.querySelectorAll("#wi-busy .wi-stuck li")].map(li => li.textContent),
        start: document.querySelector("#wi-start").disabled, tag: document.querySelector("[data-wi-point]").textContent, del: !!document.querySelector("[data-wi-del]") }));
      assert(b.text.includes("上次换地图没能自动恢复") && b.items.length === 2 && b.items[0] === "D:\\MineDeck\\servers\\create-pack\\world" && b.start && b.tag.includes("要人处理") && !b.del, "卡住时的页面：" + JSON.stringify(b));
      // 重试没成功：提示还在
      await wState({ world: { inst: { [PACK]: { retryFail: true } } } });
      await page.click("#wi-retry"); await page.waitForSelector(".toast.error");
      assert(await page.$("#wi-busy"), "重试没成功时提示应还在");
      await page.click(".toast.error .t-msg").catch(() => {});
      await page.keyboard.press("Escape"); await settle(300);
      await wState({ world: { inst: { [PACK]: { retryFail: false } } } });
      await page.click("#wi-retry");
      await page.waitForSelector("#wi-busy", { state: "detached" });
      const st = await wState({});
      const after = await page.evaluate(() => ({ start: document.querySelector("#wi-start").disabled, state: document.querySelector("[data-wi-point]").dataset.state }));
      assert(st.world.retries.length === 2 && st.world.retries[1] === PACK && !after.start && after.state === "rolled-back", "重试成功以后：" + JSON.stringify([st.world.retries, after]));
      // 别的设备：后端只给一句「在这台电脑上查看」，页面照着当文字显示
      await wState({ local: false, world: { inst: { [PACK]: WI_STUCK } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-busy");
      const lan = await page.$eval("#wi-busy", el => el.textContent);
      assert(lan.includes("在这台电脑上查看") && !lan.includes("D:\\MineDeck"), "别的设备上不该出现本机路径：" + lan);
    } finally { await wReset(); }
  });

  await run("地图：导入过几次时只有最近那一次能换回，更早的写明要先换回最近的；导入前没有世界的没有换回键；读不出来的记录写明原因和文件夹、没有换回和删除键", async () => {
    await wReset();
    try {
      const pts = [
        { id: "wc3000003", state: "committed", created: "2026-11-12T20:00:00+08:00", source: "C.zip", candidate: "第三张", levelName: "world", hasOld: true, oldBytes: 3e8, keptBytes: 0 },
        { id: "wc2000002", state: "committed", created: "2026-11-11T20:00:00+08:00", source: "B.zip", candidate: "第二张", levelName: "world", hasOld: true, oldBytes: 2e8, keptBytes: 0 },
        { id: "wc1000001", state: "committed", created: "2026-11-10T20:00:00+08:00", source: "A.zip", candidate: "开服时的地图", levelName: "world", hasOld: false, oldBytes: 0, keptBytes: 0 },
      ];
      const rowsOf = () => page.$$eval("#wi-points [data-wi-point]", els => Object.fromEntries(els.map(r => [r.dataset.wiPoint, { state: r.dataset.state, text: r.textContent.replace(/\s+/g, " "),
        restore: !!r.querySelector("[data-wi-restore]"), del: !!r.querySelector("[data-wi-del]"), warn: !!r.querySelector(".tag.warn") }])));
      await wState({ world: { quick: true, inst: { [PACK]: { points: pts } } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-points [data-wi-point]");
      let r = await rowsOf();
      assert(Object.values(r).filter(x => x.restore).length === 1 && r.wc3000003.restore && r.wc3000003.text.includes("可以换回"), "只有最近那一次能换回：" + JSON.stringify(r));
      assert(!r.wc2000002.restore && r.wc2000002.del && r.wc2000002.text.includes("更早的导入") && r.wc2000002.text.includes("这之后又导入过别的地图") && r.wc2000002.text.includes("要先换回最近那一次"), "更早的那一次：" + JSON.stringify(r.wc2000002));
      assert(!r.wc1000001.restore && r.wc1000001.del && r.wc1000001.text.includes("这次导入之前服务器还没有世界") && !r.wc1000001.text.includes("要先换回"), "导入前没有世界的那一次：" + JSON.stringify(r.wc1000001));
      // 换回最近的那一次以后，才轮到第二张
      await page.click('[data-wi-restore="wc3000003"]'); await page.waitForSelector(".modal");
      await page.click('.modal [data-x="ok"]');
      await page.waitForSelector("#wi-task-done-text", { timeout: 15000 });
      await page.click("#wi-task-ok"); await page.waitForSelector('[data-wi-point="wc3000003"][data-state="restored"]');
      r = await rowsOf();
      assert(Object.values(r).filter(x => x.restore).length === 1 && r.wc2000002.restore && r.wc2000002.text.includes("正在用这张地图") && !r.wc3000003.restore, "换回以后应轮到第二张：" + JSON.stringify(r));
      // 只有「用地图新建」留下的那一条：没有可以换回的旧地图，不出换回键
      await wState({ world: { inst: { [PACK]: { points: [pts[2]] } } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-points [data-wi-point]");
      r = await rowsOf();
      assert(!r.wc1000001.restore && r.wc1000001.del && r.wc1000001.text.includes("正在用这张地图") && r.wc1000001.text.includes("没有可以换回的旧地图"), "用地图新建的服务器：" + JSON.stringify(r));
      // 读不出来的记录：按要人处理显示，写明原因和文件夹，没有换回和删除键；它在的时候「重试恢复」一直失败
      const BAD = "D:\\MineDeck\\servers\\create-pack\\.minedeck\\world-import\\w0bad0001";
      await wState({ world: { inst: { [PACK]: { busy: "换地图的记录读不出来，为了不在半成品的世界上启动，已暂停启动。请到「世界与备份 → 换地图」里查看",
        points: [pts[0], { id: "w0bad0001", state: "unreadable", created: "0001-01-01T00:00:00Z", source: "", candidate: "", levelName: "", hasOld: false, oldBytes: 5e8, keptBytes: 0, error: "unexpected end of JSON input", stuck: [BAD] }] } } } });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector('[data-wi-point="w0bad0001"]');
      r = await rowsOf();
      const bad = r.w0bad0001, banner = await page.evaluate(() => ({ text: document.querySelector("#wi-busy").textContent, items: [...document.querySelectorAll("#wi-busy .wi-stuck li")].map(li => li.textContent),
        start: document.querySelector("#wi-start").disabled, restoreOff: document.querySelector('[data-wi-restore="wc3000003"]').disabled }));
      assert(bad.state === "unreadable" && bad.warn && !bad.restore && !bad.del && bad.text.includes("读不出来") && bad.text.includes("unexpected end of JSON input") && bad.text.includes(BAD) && bad.text.includes("476.8 MB"), "读不出来的记录：" + JSON.stringify(bad));
      assert(!/\d{4}-\d\d-\d\d/.test(bad.text) && !bad.text.includes("NaN") && !bad.text.includes(" 导入 ·") && !bad.text.includes("来源"), "读不出来的记录没有时间和来源，不该硬凑：" + bad.text);
      assert(banner.text.includes("换地图的记录读不出来") && banner.items.length === 1 && banner.items[0] === BAD && banner.start && banner.restoreOff, "读不出来时页面顶上的提示：" + JSON.stringify(banner));
      await page.click("#wi-retry"); await page.waitForSelector(".toast.error");
      await settle(500);
      assert((await page.$("#wi-busy")) && (await page.$('[data-wi-point="w0bad0001"]')) && (await wState({})).world.retries.length === 1, "记录还读不出来时重试应失败、提示还在");
      // 别的设备上：文件夹只给一句话
      await wState({ local: false });
      await page.goto(url(worldTab(PACK))); await page.waitForSelector('[data-wi-point="w0bad0001"]');
      const lan = (await rowsOf()).w0bad0001.text;
      assert(lan.includes("在这台电脑上查看") && !lan.includes("D:\\MineDeck"), "别的设备上不该出现本机路径：" + lan);
    } finally { await wReset(); }
  });

  await run("地图：别的设备发起的换地图，打开换地图页就接上它的进度；取消键是灰的、写着只有发起的那台设备能取消；看完回到首页不会又被接回去", async () => {
    await wReset();
    try {
      await wState({ world: { foreignTask: { inst: PACK, kind: "import" } } });
      const t0 = reqs.length;
      await page.goto(url(worldTab(PACK))); await page.waitForSelector("#wi-task");
      await page.waitForSelector("#wi-cancel[disabled]");
      const v = await page.evaluate(() => ({ title: document.querySelector("#wi-task-title").textContent, why: document.querySelector("#wi-cancel-why").textContent,
        btn: document.querySelector("#wi-cancel").textContent.trim() }));
      assert(v.title.includes("机械动力整合包") && v.why.includes("只有发起这次导入的那台设备能取消") && v.btn === "取消导入", "接上的进度：" + JSON.stringify(v));
      await page.waitForSelector("#wi-task-done-text", { timeout: 15000 });
      const done = await page.$eval("#wi-task-done-text", el => el.textContent);
      assert(done.includes("「机械动力整合包」的地图文件已导入"), "结果里应有服务器名：" + done);
      assert(!(await page.$("#wi-task-again")) && wPosts(t0, "/api/world/cancel") === 0, "别人发起的导入：没有「重新核对」，也没发过取消");
      await page.click("#wi-task-ok"); await page.waitForSelector("#wi-start");
      await settle(3500); // 首页三秒刷新一次：刷过以后还停在首页
      assert((await page.$("#wi-start")) && !(await page.$("#wi-task")), "看完回到首页以后不该又被接回进度页");
      // 别的设备在换回旧地图：接上以后写明正在换回，没有取消键
      await wState({ world: { foreignTask: { inst: PACK, kind: "restore", point: "w7f3a9c21" } } });
      await page.waitForSelector("#wi-task", { timeout: 8000 });
      assert((await page.$eval("#wi-task-title", el => el.textContent)).includes("换回导入前的地图") && !(await page.$("#wi-cancel")), "接上换回旧地图的进度");
      await page.waitForSelector("#wi-task-done-text", { timeout: 15000 });
    } finally { await wReset(); }
  });

  // 新建向导里选一张地图：点「用已有地图开服」→ 填路径检测 → 停在第一步（核心已经按地图预选）
  const wWizPick = async pth => {
    await page.goto(url("#/create/new")); await page.waitForSelector("#wi-wiz-pick");
    await page.click("#wi-wiz-pick"); await page.waitForSelector("#wi-scan");
    await wScan(pth); await page.waitForSelector("#wi-wiz-chosen");
  };
  const wCreates = since => reqs.slice(since).filter(r => r.m === "POST" && r.u === "/api/instances").length;

  await run("新建向导：用已有地图开服——按地图预选核心和版本（不换成最新版），最后一步核对，创建请求带上计划编号；请求被收下以后才清检测结果", async () => {
    await wReset();
    try {
      // 两个入口：向导第一步上方的那一条，和「新建服务器」页卡片下面的那一行（从那儿进来，选地图的面板一上来就开着）
      await page.goto(url("#/create/new")); await page.waitForSelector("#wi-wiz-pick");
      assert((await page.$eval("#wi-wiz-offer", el => el.textContent)).includes("手里有现成的地图"), "第一步应有「用已有地图开服」的入口");
      await page.goto(url("#/create")); await page.waitForSelector("#mode-world");
      // beta.3 R14：「用已有地图开服」从卡片下面那一行小字改成第五张卡片
      assert((await page.$$(".recipe-grid .core-card")).length === 5 && (await page.$eval("#mode-world .cn", a => a.textContent)) === "用已有地图开服", "新建页应是五张卡片，第五张是用已有地图开服");
      await page.click("#mode-world"); await page.waitForSelector("#wi-browse");
      assert(page.url().endsWith("#/create/world") && (await page.$("#wi-wiz-panel")) && (await page.$(".steps .step-pill.cur")), "从新建页的入口进来应是向导第一步、选地图的面板开着：" + page.url());
      await page.click("#wi-browse"); await page.waitForSelector("#wi-wiz-chosen");
      await page.waitForSelector(".core-card.sel");
      const s0 = await page.evaluate(() => ({ name: document.querySelector("#wi-wiz-name").textContent, strip: document.querySelector("#wi-wiz-chosen").textContent,
        core: document.querySelector(".core-card.sel").dataset.id, next: document.querySelector("#wnext").disabled }));
      assert(s0.name === "天空之城" && s0.strip.includes("1.19.4") && s0.strip.includes("预选了原版") && s0.core === "vanilla" && !s0.next, "选好地图以后的第一步：" + JSON.stringify(s0));
      const scanId = (await wState({})).world.lastScan.id;
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      const ver = await page.evaluate(() => ({ sel: document.querySelector(".ver-item.sel").dataset.id, latest: document.querySelector(".ver-item .ver-tag").closest(".ver-item").dataset.id,
        strip: !!document.querySelector("#wi-wiz-chosen"), note: document.querySelector("#wi-wiz-note").hidden }));
      assert(ver.sel === "1.19.4" && ver.latest === "26.2" && ver.strip && ver.note, "版本应预选地图保存的那个，不是最新版：" + JSON.stringify(ver));
      await page.click("#wnext"); await page.waitForSelector("#f-name"); // 原版没有「选构建」「选模组」
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      let st = await wState({});
      const plan = st.world.lastPlan;
      const fin = await page.evaluate(() => ({ rel: document.querySelector("#wi-rel").dataset.rel, relText: document.querySelector("#wi-rel").textContent, ack: !!document.querySelector("#wi-ack"),
        stop: !!document.querySelector("#wi-stop"), sets: !!document.querySelector("#wi-settings"), fill: !document.querySelector("#wi-wiz-fill-go").disabled, diff: document.querySelector("#f-diff").value, mode: document.querySelector("#f-mode").value }));
      assert(plan.newInst === true && plan.core === "vanilla" && plan.mc === "1.19.4" && plan.scanId === scanId && fin.rel === "same" && fin.relText.includes("要建的服务器") && !fin.ack && !fin.stop && !fin.sets,
        "最后一步的核对：" + JSON.stringify([plan, fin]));
      // 难度和默认模式不替人改：提一句，点了「按地图的填」才改
      assert(fin.fill && fin.diff === "easy" && fin.mode === "survival", "难度和模式不该被悄悄改掉：" + JSON.stringify(fin));
      await page.click("#wi-wiz-fill-go");
      assert((await page.$eval("#f-diff", el => el.value)) === "normal" && (await page.$eval("#f-mode", el => el.value)) === "adventure", "点「按地图的填」以后");
      const ax = await axProblems();
      assert(!ax.length, "向导最后一步的无障碍：" + ax.join("、"));
      await page.click("#f-eula");
      // 计划过期了（这里把后端存着的计划作废）：创建被拒，界面自己重新核对一遍，再点就带上新的计划
      await wState({ world: { dropPlans: true } });
      const tx = reqs.length;
      await page.click("#wcreate");
      let plan2 = plan;
      for (let k = 0; k < 30 && plan2.id === plan.id; k++) { await settle(150); plan2 = (await wState({})).world.lastPlan; }
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      assert(plan2.id !== plan.id && wCreates(tx) === 1 && page.url().includes("#/create/world") && !(await page.$eval("#wcreate", b => b.disabled)), "计划过期后应重新核对：" + JSON.stringify(plan2));
      const t0 = reqs.length;
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash.startsWith("#/task/"));
      await settle(500);
      st = await wState({});
      const lc = st.world.lastCreate;
      assert(wCreates(t0) === 1 && lc.worldPlan === plan2.id && lc.worldAck === false && lc.core === "vanilla" && lc.mc === "1.19.4" && lc.difficulty === "normal" && lc.gamemode === "adventure", "创建请求：" + JSON.stringify(lc));
      // 检测结果：创建请求被收下时也先留着（建服任务要是在放地图之前失败，还能接着用这张），建服任务成功了才清，只清一次
      const order = reqs.slice(tx).filter(r => r.m === "POST" && (r.u === "/api/instances" || r.u === "/api/world/discard")).map(r => r.u);
      assert(JSON.stringify(order) === JSON.stringify(["/api/instances", "/api/instances"]) && st.world.discards.length === 0,
        "创建刚被收下时不该清检测结果：" + JSON.stringify([order, st.world.discards]));
      await wState({ taskStage: 4 });
      for (let k = 0; k < 40 && !st.world.discards.length; k++) { await settle(150); st = await wState({}); }
      assert(JSON.stringify(st.world.discards) === JSON.stringify([scanId]), "建服任务成功以后应清检测结果，只清一次：" + JSON.stringify(st.world.discards));
      await wState({ taskStage: 2 });
      await page.goto(url("#/create")); await settle(500);
      await page.goto(url("#/create/new")); await page.waitForSelector("#wi-wiz-pick");
      assert((await wState({})).world.discards.length === 1, "回到新建页不该再清一遍：" + JSON.stringify((await wState({})).world.discards));
    } finally { await wReset(); }
  });

  await run("新建向导带地图：极限模式的地图，最后一步的核对里写明新服务器会打开极限模式；替换已有服务器的地图时不说这句", async () => {
    await wReset();
    try {
      await wWizPick("D:\\Maps\\hardcore-map");
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      await page.click("#wnext"); await page.waitForSelector("#wi-wiz-plan #wi-rel");
      const info = await page.$eval("#wi-wiz-plan", el => el.textContent);
      assert(info.includes("极限模式的地图") && info.includes("hardcore=true"), "核对里应写明会打开极限模式：" + info);
      await wOpenFlow(PACK);
      await wScan("D:\\Maps\\hardcore-map"); await page.waitForSelector("#wi-go");
      assert(!(await page.$eval("#wi-stage", el => el.textContent)).includes("hardcore=true"), "替换已有服务器的地图时不该说新服务器会打开极限模式");
    } finally { await wReset(); }
  });

  await run("新建向导带地图：改了核心或版本会重新核对；要确认的没勾、被拦下的都发不出创建请求；地图的版本不在列表里时不预选；「不用地图」清掉检测结果", async () => {
    await wReset();
    try {
      // 换成 Paper 1.21.4：地图更旧，要确认
      await wWizPick("D:\\Maps\\SkyCity");
      await page.waitForSelector('.core-card[data-id="paper"]');
      await page.click('.core-card[data-id="paper"]');
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      assert((await page.$eval(".ver-item.sel", el => el.dataset.id)) === "1.19.4", "换了核心，版本仍应预选地图的版本");
      await page.click('.ver-item[data-id="1.21.4"]');
      await page.click("#wnext"); await page.waitForSelector("#build-rec b"); // beta.3 R22：构建列表收进「高级」，等推荐的那一行
      await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      let st = await wState({});
      const plan1 = st.world.lastPlan;
      const f1 = await page.evaluate(() => ({ rel: document.querySelector("#wi-rel").dataset.rel, acks: document.querySelectorAll("#wi-wiz-plan input#wi-ack").length, checked: document.querySelector("#wi-ack").checked }));
      assert(plan1.core === "paper" && plan1.mc === "1.21.4" && f1.rel === "older" && f1.acks === 1 && !f1.checked, "换成 Paper 1.21.4 的核对：" + JSON.stringify([plan1, f1]));
      await page.click("#f-eula");
      let t0 = reqs.length;
      await page.click("#wcreate"); await settle(400);
      assert(wCreates(t0) === 0 && (await page.$eval("#wi-wiz-err", el => el.textContent)).includes("我已了解") && page.url().includes("#/create/new"), "没确认不该发创建请求");
      // 回「选版本」换一个：最后一步重新核对，勾选清空
      await page.check("#wi-ack");
      await page.click('.step-pill.done[data-step="1"]'); await page.waitForSelector(".ver-item.sel");
      await page.click('.ver-item[data-id="1.20.6"]');
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      st = await wState({});
      const plan2 = st.world.lastPlan;
      assert(plan2.id !== plan1.id && plan2.mc === "1.20.6" && !(await page.$eval("#wi-ack", el => el.checked)), "改了版本应重新核对、重新确认：" + JSON.stringify(plan2));
      await page.check("#wi-ack");
      await page.click("#f-eula");
      // 创建被别的原因拒了（比如重名）：计划还在后端那儿，不重新核对、勾过的确认留着，检测结果不清；改好再点用的还是同一份
      await wState({ world: { createFail: "已有同名的服务器「地图服」" } }); // 真后端的原话（install.go）
      t0 = reqs.length;
      await page.click("#wcreate"); await page.waitForSelector(".toast.error"); await settle(400);
      st = await wState({});
      assert(wCreates(t0) === 1 && st.world.lastPlan.id === plan2.id && st.world.discards.length === 0 && (await page.$eval("#wi-ack", el => el.checked)) && !(await page.$eval("#wcreate", b => b.disabled)),
        "重名这类失败不该重新核对、不该清检测结果：" + JSON.stringify([st.world.lastPlan, st.world.discards]));
      await wState({ world: { createFail: "" } });
      t0 = reqs.length;
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash.startsWith("#/task/"));
      st = await wState({});
      assert(wCreates(t0) === 1 && st.world.lastCreate.worldPlan === plan2.id && st.world.lastCreate.worldAck === true && st.world.lastCreate.mc === "1.20.6", "创建请求应带最新的计划和确认：" + JSON.stringify(st.world.lastCreate));

      // 地图的版本（1.19.3）不在列表里：不预选，「下一步」点不了，说明原因
      await wWizPick("D:\\Maps\\map-1193");
      const scan3 = (await wState({})).world.lastScan.id;
      await page.click("#wnext"); await page.waitForSelector(".ver-item[data-id]");
      const nv = await page.evaluate(() => ({ sel: document.querySelectorAll(".ver-item.sel").length, next: document.querySelector("#wnext").disabled,
        note: document.querySelector("#wi-wiz-note").hidden ? "" : document.querySelector("#wi-wiz-note").textContent }));
      assert(nv.sel === 0 && nv.next && nv.note.includes("1.19.3") && nv.note.includes("请自己选一个"), "版本不在列表里时：" + JSON.stringify(nv));
      // 「不用地图」：清掉检测结果，向导照常（这时才预选最新版）
      await page.click("#wi-wiz-drop"); await page.waitForSelector(".ver-item.sel");
      await settle(300);
      st = await wState({});
      assert(st.world.discards.includes(scan3) && !(await page.$("#wi-wiz-chosen")) && (await page.$eval(".ver-item.sel", el => el.dataset.id)) === "26.2", "不用地图以后：" + JSON.stringify(st.world.discards));

      // 被拦下：26.2 的地图配 1.21.4 的服务器
      await wWizPick("D:\\Maps\\newer-map");
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      assert((await page.$eval(".ver-item.sel", el => el.dataset.id)) === "26.2", "应预选地图的 26.2");
      await page.click('.ver-item[data-id="1.21.4"]');
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan #wi-block");
      await page.click("#f-eula");
      t0 = reqs.length;
      await page.click("#wcreate"); await settle(400);
      const bl = await page.evaluate(() => ({ block: document.querySelector("#wi-block").textContent, err: document.querySelector("#wi-wiz-err").textContent, rel: document.querySelector("#wi-rel").textContent }));
      assert(wCreates(t0) === 0 && bl.block.includes("不能用这张地图开服") && bl.err.includes("标红") && bl.rel.includes("回到「选版本」"), "被拦下时不该发创建请求：" + JSON.stringify(bl));
      // 离开向导：没用上的检测结果清掉
      const scan4 = (await wState({})).world.lastScan.id;
      await page.click('#sidebar [data-nav="instances"]'); await page.waitForSelector(".card"); await settle(300);
      assert((await wState({})).world.discards.includes(scan4), "离开向导应清掉没用上的检测结果");
      // 代理端不能用地图开服：最后一步说明原因
      await wWizPick("D:\\Maps\\SkyCity");
      await page.waitForSelector('.core-card[data-id="velocity"]');
      await page.click('.core-card[data-id="velocity"]');
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel, .ver-item[data-id]");
      await page.click(".ver-item[data-id]");
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan .err-box");
      t0 = reqs.length;
      await page.click("#wcreate"); await settle(400);
      assert(wCreates(t0) === 0 && (await page.$eval("#wi-wiz-plan", el => el.textContent)).includes("代理端"), "代理端配地图不该发创建请求");
    } finally { await wReset(); }
  });

  await run("新建向导带地图、待装清单里有东西：服务器开始建了以后，地图条上的「换一张」「不用地图」和「上一步」一样点不了，建完照常接着装", async () => {
    await wReset();
    try {
      await cartState({ modCart: { reset: true }, taskStage: 2 }); // 建服任务还在跑：向导留在这一页等它
      await wWizPick("D:\\Maps\\plain-world");
      const scanId = (await wState({})).world.lastScan.id;
      await page.waitForSelector('.core-card[data-id="paper"]');
      await page.click('.core-card[data-id="paper"]');
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      assert((await page.$eval(".ver-item.sel", el => el.dataset.id)) === "1.20.1", "版本应预选地图的 1.20.1");
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#mc-q");
      await page.fill("#mc-q", "LuckPerms"); await page.click("#mc-go");
      await page.click('button[aria-label="加入清单 LuckPerms"]'); await settle(700);
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      assert(!(await page.$eval("#wi-wiz-change", b => b.disabled)) && !(await page.$eval("#wi-wiz-drop", b => b.disabled)), "创建之前地图条上的键应能点");
      await page.fill("#f-name", "带地图的插件服");
      await page.click("#f-eula");
      // 创建请求还没回来的这段：地图条、「上一步」、步骤条都要锁住（复审 Grok 4.7 指出原来要等回来才锁）
      let release; const gate = new Promise(r => { release = r; });
      await page.route("**/api/instances", async route => { if (route.request().method() === "POST") await gate; await route.continue().catch(() => {}); });
      await page.click("#wcreate"); await settle(250);
      const mid = await page.evaluate(() => ({ change: document.querySelector("#wi-wiz-change").disabled, drop: document.querySelector("#wi-wiz-drop").disabled,
        back: document.querySelector("#wback").disabled, pills: [...document.querySelectorAll(".step-pill")].every(p => p.onclick === null) }));
      await page.$eval("#wi-wiz-drop", b => b.click()); await page.$eval("#wi-wiz-change", b => b.click()); await settle(200);
      const still = !!(await page.$("#wi-wiz-chosen")) && !!(await page.$("#f-name"));
      const answered = page.waitForResponse(r => r.url().endsWith("/api/instances") && r.request().method() === "POST");
      release(); await answered; await page.unroute("**/api/instances");
      assert(mid.change && mid.drop && mid.back && mid.pills && still, "请求还没回来时应锁住，硬点也不换地图：" + JSON.stringify([mid, still]));
      await page.waitForFunction(() => document.querySelector("#wback").disabled && /等待|创建|部署|下载/.test(document.querySelector("#wcreate").textContent)); // 向导进了「等建服」
      const w = await page.evaluate(() => ({ change: document.querySelector("#wi-wiz-change").disabled, drop: document.querySelector("#wi-wiz-drop").disabled, back: document.querySelector("#wback").disabled }));
      let st = await wState({});
      assert(w.change && w.drop && w.back && st.world.lastCreate.worldPlan && st.world.discards.length === 0, "等建服时地图条应锁住、检测结果还留着：" + JSON.stringify([w, st.world.discards]));
      await page.$eval("#wi-wiz-drop", b => b.click()); await page.$eval("#wi-wiz-change", b => b.click()); await settle(300);
      assert((await page.$("#f-name")) && (await page.$("#wi-wiz-chosen")) && (await page.$eval("#wcreate", b => b.disabled)) && (await page.$eval("#wback", b => b.disabled)), "硬点地图条不该把向导重画");
      // 建好了：照常接着装待装清单
      await cartState({ taskStage: 4 });
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      st = await wState({});
      for (let k = 0; k < 40 && st.world.discards.length !== 1; k++) { await settle(150); st = await wState({}); }
      assert(st.modcart.lastInstall && st.modcart.lastInstall.inst === "带地图的插件服" && st.modcart.lastInstall.includeDraft === true && JSON.stringify(st.world.discards) === JSON.stringify([scanId]),
        "建完应接着装清单、清掉检测结果：" + JSON.stringify([st.modcart.lastInstall, st.world.discards]));
    } finally { await page.unroute("**/api/instances").catch(() => {}); await cartState({ modCart: { reset: true }, taskStage: 2, instReset: true }); await wReset(); }
  });

  await run("新建向导带地图：建服任务没成功时检测结果留着，回到向导可以「接着用这张」，不用再检测一遍", async () => {
    await wReset();
    try {
      await wState({ taskStage: 2 });
      await wWizPick("D:\\Maps\\plain-world");
      const scanId = (await wState({})).world.lastScan.id;
      await page.waitForSelector(".core-card.sel");
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      await page.click("#wnext"); await page.waitForSelector("#f-name");
      await page.waitForSelector("#wi-wiz-plan #wi-rel");
      if (await page.$("#wi-ack")) await page.check("#wi-ack");
      await page.fill("#f-name", "建不成的服");
      await page.click("#f-eula");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash.startsWith("#/task/"));
      await wState({ taskStage: "error" }); // 建服任务在放地图之前失败（下载核心失败）
      await settle(3600); // 等界面跟着任务看到结果（每 3 秒查一次）
      let st = await wState({});
      assert(st.world.discards.length === 0, "建服任务失败时不该清检测结果：" + JSON.stringify(st.world.discards));
      await page.evaluate(() => { location.hash = "#/create/new"; }); await page.waitForSelector("#wi-wiz-last"); // 在程序里点过去（不重载页面）
      const line = await page.$eval("#wi-wiz-last-line", el => el.textContent);
      assert(line.includes("建服没有成功") && line.includes("接着用这张"), "向导第一步应提出接着用上次那张：" + line);
      const t0 = reqs.length;
      await page.click("#wi-wiz-last"); await page.waitForSelector("#wi-wiz-chosen");
      await page.waitForSelector(".core-card.sel");
      assert(!reqs.slice(t0).some(r => r.u === "/api/world/scan" || r.u === "/api/world/upload") && (await wState({})).world.discards.length === 0,
        "接着用不该重新检测，也不该清掉：" + JSON.stringify(reqs.slice(t0).map(r => r.u)));
      // 换成别的地图：上次留着的那次检测清掉
      await page.click("#wi-wiz-change"); await page.waitForSelector("#wi-browse");
      await wWizPick("D:\\Maps\\map-1193");
      st = await wState({});
      assert(st.world.discards.includes(scanId), "换了别的地图应清掉上次那次检测：" + JSON.stringify(st.world.discards));
    } finally { await wState({ taskStage: 2 }); await wReset(); }
  });

  await run("地图：手机宽度（390）下地图标签、选来源、核对页和向导的地图条都不横向溢出", async () => {
    await wReset();
    const c = await hostCtx({ host: false }, { viewport: { width: 390, height: 844 } });
    try {
      const p = await c.newPage();
      p.on("pageerror", e => errors.push(pageErr(e)));
      const over = async label => {
        await p.waitForTimeout(350);
        const w = await p.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth,
          wide: [...document.querySelectorAll("#tab-body *, #wi-wiz *")].filter(e => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 1).map(e => e.id || e.className || e.tagName).slice(0, 5) }));
        assert(w.sw <= w.cw && w.wide.length === 0, label + " 横向溢出：" + JSON.stringify(w));
      };
      await p.goto(ORIGIN + worldTab(PACK)); await p.waitForSelector("#wi-points [data-wi-point]"); await over("地图标签");
      await p.click("#wi-start"); await p.waitForSelector("#wi-upload"); await over("选来源");
      await p.fill("#wi-path", "D:\\Maps\\multi-pack.zip"); await p.click("#wi-scan"); await p.waitForSelector("[data-wi-pick]"); await over("候选列表");
      await p.click("#wi-again"); await p.waitForSelector("#wi-upload");
      await p.setInputFiles("#wi-file", wZip("SkyCity.zip")); await p.waitForSelector("#wi-go"); await over("核对页");
      await p.click('[data-wi-readme="0"]'); await p.waitForSelector(".wi-readme-text:not([hidden])"); await over("核对页（展开说明书）");
      await p.goto(ORIGIN + "#/create/new"); await p.waitForSelector("#wi-wiz-pick"); await over("向导的入口");
      await p.click("#wi-wiz-pick"); await p.waitForSelector("#wi-scan");
      await p.fill("#wi-path", "D:\\Maps\\SkyCity"); await p.click("#wi-scan"); await p.waitForSelector("#wi-wiz-chosen"); await over("向导里选好的地图");
    } finally { await c.close(); await wReset(); }
  });
  // ==================== beta.3 新建流程与任务页（R09 R13 R14 R22） ====================
  // docs/测试与评审/可用性走查-2026-11-14.md 第八节 R09（后端和接口那一半）、R13、R14、R22。用例名都以「beta.3 流程」开头，node tests.js "beta.3 流程" 只跑这一块。
  // 仿真后端用到的：taskList 里带 canCancel 的任务能取消（先 cancelling，再查两次按「已取消」结束），instPending（按服务器的没装上的文件），见 server.js「/__mock/state」说明
  {
    const fState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const navOn = () => page.evaluate(() => (document.querySelector("#sidebar .nav-item.active") || {}).dataset.nav || "");
    const h1 = () => page.textContent(".page-head h1");
    const PACK = "机械动力整合包";

    await run("beta.3 流程 R14：新建页五种方式分两组，第五张是用已有地图开服；服务器列表空状态的次要链接带上它", async () => {
      try {
        await page.goto(url("#/create")); await page.waitForSelector("#mode-world");
        const g = await page.evaluate(() => ({ head: document.querySelector(".page-head").textContent,
          groups: [...document.querySelectorAll(".recipe-groups .core-group")].map(x => x.querySelector(".cg-title").textContent + ":" + [...x.querySelectorAll(".core-card")].map(c => c.dataset.mode).join(",")),
          oldLink: !!document.querySelector(".wi-home-link"), name: document.querySelector("#mode-world .cn").textContent }));
        assert(g.head.includes("五种方式") && !g.head.includes("四种") && g.groups.join("|") === "新建:new,import,world|搬过来:transfer,adopt" && !g.oldLink && g.name === "用已有地图开服",
          "新建页的五张卡片：" + JSON.stringify(g));
        // 键盘也能进：回车进的是同一个向导，选地图的面板开着
        await page.focus("#mode-world"); await page.keyboard.press("Enter");
        await page.waitForSelector("#wi-wiz-panel");
        assert(page.url().endsWith("#/create/world") && (await page.$(".steps .step-pill.cur")), "第五张卡片应进带地图面板的向导：" + page.url());
        await fState({ empty: true });
        await page.goto(url("#/instances")); await page.waitForSelector("#empty-new");
        const link = await page.textContent(".empty .quiet-link");
        assert(link.includes("用已有地图开服") && link.includes("导入整合包") && link.includes("导入交接包") && link.includes("接管已有服务器"), "空状态的次要链接：" + link);
      } finally { await fState({ empty: false }); }
    });

    await run("beta.3 流程 R13：列表页只摆进行中、有没装上的文件、失败的任务；完成和取消的折成一行不占首屏，失败的能收起", async () => {
      const done = n => ({ id: "task-ok" + n, title: `创建服务器 已建好的第 ${n} 台（paper 1.21.4）`, ended: true, result: "x" + n });
      const failTitle = "创建服务器 R13 折叠测试失败的（fabric 26.2）";
      const taskList = [
        { id: "task-run", title: "创建服务器 还在建的（paper 1.21.4）", ended: false, label: "下载 Paper 核心", done: 10, total: 50 },
        { id: "task-imp", title: "导入整合包 有没装上的（forge 1.20.1）", ended: true, result: PACK, pending: [{ id: "p1", path: "mods/a.jar", reason: "nohash", options: ["skip", "local"] }] },
        { id: "task-bad", title: failTitle, ended: true, error: "下载失败" },
        done(1), done(2), done(3), done(4), done(5),
        { id: "task-cx", title: "创建服务器 取消了的（vanilla 26.2）", ended: true, cancelled: true, error: "已取消，没有创建服务器" },
      ];
      try {
        await fState({ taskList });
        await page.goto(url("#/instances")); await page.waitForSelector("#task-list:not([hidden]) .task-fold");
        const shown = await page.$$eval("#task-list > .task-row", els => els.map(e => e.dataset.task));
        assert(JSON.stringify(shown) === JSON.stringify(["task-run", "task-imp", "task-bad"]), "外面只该摆进行中、有没装上的文件、失败的：" + JSON.stringify(shown));
        const sum = await page.textContent("#task-list .task-fold > summary");
        assert(sum.includes("另外 6 个已结束的任务") && sum.includes("5 个已完成") && sum.includes("1 个已取消"), "折起来的那一行：" + sum);
        assert(!(await page.isVisible('#task-list a[href="#/task/task-ok1"]')), "已完成的不该占地方");
        // 第一张服务器卡片在首屏里
        await page.waitForSelector("#inst-cards .card");
        const top = await page.$eval("#inst-cards .card", el => el.getBoundingClientRect().top);
        assert(top < 850 - 120, "第一张服务器卡片应在首屏：" + top);
        // 失败的那条能收起；收起以后进折叠里，刷新以后照样收着
        await page.click(`#task-list [data-hide-task="${"task-bad|" + failTitle}"]`); await settle(300);
        assert(!(await page.$$eval("#task-list > .task-row", els => els.map(e => e.dataset.task))).includes("task-bad"), "收起以后不该还在外面");
        assert((await page.textContent("#task-list .task-fold > summary")).includes("1 个失败"), "收起的失败任务应算进折叠的那一行");
        await page.reload(); await page.waitForSelector("#task-list .task-fold");
        assert(!(await page.$$eval("#task-list > .task-row", els => els.map(e => e.dataset.task))).includes("task-bad"), "刷新以后照样收着");
        // 点开能看到全部，取消的写「已取消」不是「失败」
        await page.click("#task-list .task-fold > summary"); await settle(200);
        assert(await page.isVisible('#task-list a[href="#/task/task-ok1"]'), "点开以后能看到已完成的");
        const cx = await page.textContent('#task-list .task-row[data-task="task-cx"]');
        assert(cx.includes("已取消") && !cx.includes("失败"), "取消的任务：" + cx);
        // 轮询重画时折叠保持打开
        await settle(3400);
        assert(await page.isVisible('#task-list a[href="#/task/task-ok1"]'), "轮询以后折叠不该自己收回去");
      } finally {
        await page.evaluate(() => sessionStorage.removeItem("md_task_hidden"));
        await fState({ taskList: [] });
      }
    });

    await run("beta.3 流程 R13：向导创建、在任务页取消 → 中性的「已取消」（不是失败、没有英文）；「返回修改」回到基本设置，填过的都在，而且只恢复一次", async () => {
      try {
        await fState({ modCart: { reset: true }, instReset: true, taskList: [{ id: "task-1", title: "创建服务器 返回修改测试服（paper 1.21.4）", ended: false, canCancel: true, label: "下载 Paper 核心", done: 5, total: 50,
          steps: [{ name: "下载 Java 21 运行时", status: "done" }, { name: "下载 Paper 1.21.4 核心", status: "running" }, { name: "同意 EULA 并生成配置", status: "pending" }] }] });
        await page.goto(url("#/create/new")); await page.waitForSelector('.core-card[data-id="paper"]');
        await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click('.ver-item[data-id="1.21.4"]'); await page.click("#wnext"); await page.waitForSelector("#build-rec b");
        await page.click("#build-adv summary"); await page.click('#build-list .ver-item[data-id="230"]');
        await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        await page.fill("#f-name", "返回修改测试服"); await page.fill("#f-port", "25601"); await page.fill("#f-motd", "回来以后还在");
        await page.check("#f-wl"); await page.uncheck("#f-flight"); await page.selectOption("#f-diff", "hard"); await page.selectOption("#f-mode", "creative");
        await page.fill("#mem-mount .mem-num", "6144"); await page.press("#mem-mount .mem-num", "Tab");
        await page.check("#f-eula");
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1");
        assert(await navOn() === "create", "创建任务的侧栏应亮「新建服务器」");
        await page.locator("#t-cancel").waitFor({ state: "visible" });
        await page.click("#t-cancel");
        await page.waitForFunction(() => (document.querySelector(".page-head h1") || {}).textContent === "已取消创建", null, { timeout: 8000 });
        const s = await page.evaluate(() => ({ msgs: document.querySelector("#t-msgs").textContent, errBox: document.querySelectorAll("#t-msgs .err-box").length,
          note: !!document.querySelector("#t-msgs .task-note"), cancelledSteps: document.querySelectorAll(".task-step.cancelled").length, errSteps: document.querySelectorAll(".task-step.error").length,
          eyebrow: document.querySelector("#task-eyebrow").textContent, body: document.querySelector("#main").textContent, back: !!document.querySelector("#t-back-edit") }));
        assert(s.errBox === 0 && s.note && s.msgs.includes("已取消，没有创建服务器") && !s.msgs.includes("失败") && s.eyebrow === "已取消", "取消以后应是中性的「已取消」：" + JSON.stringify(s));
        assert(s.cancelledSteps === 1 && s.errSteps === 0, "取消时正在做的那一步不该画红叉：" + JSON.stringify(s));
        assert(!/context canceled/i.test(s.body) && s.back, "不该出现英文的取消原因，应有「返回修改」：" + JSON.stringify(s));
        assert(!(await page.$$eval(".toast", ts => ts.map(t => t.textContent))).some(t => t.includes("创建成功")), "取消了不该提示创建成功");
        await page.click("#t-back-edit"); await page.waitForSelector("#f-name");
        const f = await page.evaluate(() => ({ hash: location.hash, name: $("#f-name").value, port: $("#f-port").value, motd: $("#f-motd").value, wl: $("#f-wl").checked,
          flight: $("#f-flight").checked, diff: $("#f-diff").value, mode: $("#f-mode").value, eula: $("#f-eula").checked, mem: $("#f-mem").value,
          sum: $("#wf-sum").textContent, step: $(".page-head").textContent }));
        assert(f.hash === "#/create/new" && f.name === "返回修改测试服" && f.port === "25601" && f.motd === "回来以后还在" && f.wl && !f.flight && f.diff === "hard" && f.mode === "creative" && f.eula && f.mem === "6144",
          "返回修改以后表单里的东西：" + JSON.stringify(f));
        assert(f.sum.includes("Paper") && f.sum.includes("1.21.4") && f.sum.includes("构建 230") && f.step.includes("第 5 步"), "核心、版本、构建也应照原样，停在基本设置：" + JSON.stringify(f));
        // 再提交：请求和第一次填的一样
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1");
        const lc = (await fState({})).lastCreate;
        assert(lc.name === "返回修改测试服" && lc.port === 25601 && lc.build === "230" && lc.whitelist === true && lc.difficulty === "hard" && lc.xmxMb === 6144, "再提交的请求：" + JSON.stringify(lc));
        // 只恢复一次：从新建页再进向导是空白的
        await page.goto(url("#/create/new")); await page.waitForSelector("#core-grid .core-card");
        assert(!(await page.$("#core-grid .core-card.sel")) && !(await page.$("#f-name")), "之后再进向导不该带着上次的东西");
        // 任务列表里写「已取消」
        await fState({ taskList: [{ id: "task-1", title: "创建服务器 返回修改测试服（paper 1.21.4）", ended: true, cancelled: true, error: "已取消，没有创建服务器" }] });
        await page.goto(url("#/create")); await page.waitForSelector("#task-list .task-fold");
        await page.click("#task-list .task-fold > summary");
        const row = await page.textContent('#task-list .task-row[data-task="task-1"]');
        assert(row.includes("已取消") && !row.includes("失败"), "任务列表里取消的：" + row);
      } finally { await fState({ taskList: [], instReset: true, modCart: { reset: true } }); }
    });

    await run("beta.3 流程 R13：导入整合包失败以后「返回修改」回到导入表单，选的文件和填的都在", async () => {
      try {
        await fState({ taskList: [{ id: "task-1", title: "导入整合包 测试整合包（forge 1.20.1）", ended: true, error: "下载 mods/a.jar 失败: 连接超时" }] });
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-file");
        await page.setInputFiles("#im-file", { name: "回填测试.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mrpack") });
        await page.fill("#im-name", "导入回填服"); await page.fill("#im-port", "25602"); await page.fill("#im-src", "D:\\老服务器");
        await page.check("#im-online"); await page.uncheck("#im-flight"); await page.check("#im-wl"); await page.check("#im-eula");
        await page.fill("#im-mem-mount .mem-num", "8192"); await page.press("#im-mem-mount .mem-num", "Tab");
        await page.click("#im-go"); await page.waitForFunction(() => location.hash === "#/task/task-1");
        await page.waitForSelector("#t-back-edit");
        const t = await page.evaluate(() => ({ h1: $(".page-head h1").textContent, err: ($("#t-msgs .err-box") || {}).textContent || "" }));
        assert(t.h1 === "服务器没有建好" && t.err.includes("连接超时") && t.err.includes("返回修改"), "失败页：" + JSON.stringify(t));
        await page.click("#t-back-edit"); await page.waitForSelector("#im-name");
        const f = await page.evaluate(() => ({ hash: location.hash, file: ($("#im-file").files[0] || {}).name || "", name: $("#im-name").value, port: $("#im-port").value, src: $("#im-src").value,
          online: $("#im-online").checked, flight: $("#im-flight").checked, wl: $("#im-wl").checked, eula: $("#im-eula").checked, mem: $("#im-mem").value }));
        assert(f.hash === "#/create/import" && f.file === "回填测试.mrpack" && f.name === "导入回填服" && f.port === "25602" && f.src === "D:\\老服务器" && f.online && !f.flight && f.wl && f.eula && f.mem === "8192",
          "返回修改以后的导入表单：" + JSON.stringify(f));
        await page.click("#im-go"); await page.waitForFunction(() => location.hash === "#/task/task-1");
        const li = (await fState({})).lastImport;
        assert(li.name === "导入回填服" && li.port === "25602" && li.whitelist === "true" && li.onlineMode === "true" && li.allowFlight === "false" && li.localSource === "D:\\老服务器",
          "再提交的表单项：" + JSON.stringify(li));
      } finally { await fState({ taskList: [] }); }
    });

    await run("导入交接包失败以后「返回修改」回到导入交接包：选过的包重新上传，填的名称、端口、内存、Java 和 EULA 都在（A49）", async () => {
      const failTransfer = r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { id: "task-transfer-in", kind: "transfer-in",
        title: "导入交接包 A 的服", ended: true, error: "交接包里的 server.properties 把世界文件夹指到了服务器文件夹外面",
        steps: [{ name: "校验交接包", status: "error" }], warnings: [], log: [] } }) });
      await page.route("**/api/tasks/task-transfer-in", failTransfer);
      try {
        await page.goto(url("#/create/transfer")); await page.waitForSelector("#tp-file");
        await page.setInputFiles("#tp-file", { name: "A 的服.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mdpack") });
        await page.waitForSelector("#tp-name");
        await page.fill("#tp-name", "交接回填服"); await page.fill("#tp-port", "25604"); await page.selectOption("#tp-java", "21"); await page.check("#tp-eula");
        await page.fill("#tp-mem-mount .mem-num", "5120"); await page.press("#tp-mem-mount .mem-num", "Tab");
        await page.click("#tp-go"); await page.waitForFunction(() => location.hash === "#/task/task-transfer-in");
        await page.waitForSelector("#t-back-edit");
        assert((await h1()) === "交接包没有导入成", "失败页的标题：" + await h1());
        const uploads = reqs.filter(r => r.u === "/api/transfer/upload").length;
        await page.click("#t-back-edit"); await page.waitForSelector("#tp-name");
        const f = await page.evaluate(() => ({ hash: location.hash, file: ($("#tp-file").files[0] || {}).name || "", name: $("#tp-name").value, port: $("#tp-port").value,
          java: $("#tp-java").value, eula: $("#tp-eula").checked, mem: $("#tp-mem").value }));
        assert(f.hash === "#/create/transfer" && f.file === "A 的服.mdpack" && f.name === "交接回填服" && f.port === "25604" && f.java === "21" && f.eula && f.mem === "5120",
          "返回修改以后的交接包表单：" + JSON.stringify(f));
        assert(reqs.filter(r => r.u === "/api/transfer/upload").length === uploads + 1, "选回来的包应重新上传一遍");
        await page.click("#tp-go"); await page.waitForFunction(() => location.hash === "#/task/task-transfer-in");
        const lt = (await fState({})).lastTransferImport;
        assert(lt.name === "交接回填服" && lt.port === 25604 && lt.javaMajor === 21 && lt.eula === true && lt.xmxMb === 5120, "再提交的请求：" + JSON.stringify(lt));
      } finally { await page.unroute("**/api/tasks/task-transfer-in", failTransfer); }
    });

    await run("建服、导入失败时半成品没删干净（任务带 cleanupFailed）：失败框不说「已经清理干净」，说去「没建完的文件夹」里处理；只认这个字段，error 里有「清理半成品失败」几个字却没带它的照旧（A48）", async () => {
      const err = id => `解压服务端失败（${id}）；清理半成品失败：remove paper.jar: The process cannot access the file`;
      try {
        await fState({ taskList: [
          { id: "task-l1", title: "导入服务端 作者服", kind: "server-in", ended: true, error: err("task-l1"), cleanupFailed: true },
          { id: "task-l2", title: "导入交接包 A 的服", kind: "transfer-in", ended: true, error: err("task-l2"), cleanupFailed: true },
          { id: "task-l3", title: "导入整合包 测试整合包（forge 1.20.1）", ended: true, error: err("task-l3"), cleanupFailed: true },
          { id: "task-l4", title: "导入服务端 作者服", kind: "server-in", ended: true, error: err("task-l4") },
          { id: "task-l5", title: "导入交接包 A 的服", kind: "transfer-in", ended: true, error: "交接包里缺少启动文件（task-l5）" },
        ] });
        const box = async id => {
          await page.goto(url("#/task/" + id));
          await page.waitForFunction(id => (($("#t-msgs .err-box") || {}).textContent || "").includes(id), id);
          return page.textContent("#t-msgs .err-box");
        };
        for (const id of ["task-l1", "task-l2", "task-l3"]) {
          const t = await box(id);
          assert(t.includes("文件夹没删干净") && t.includes("服务器页上方「没建完的文件夹」里") && !t.includes("已经清理干净") && !t.includes("再试一次很快"),
            id + "：半成品没删干净时应说去没建完的文件夹里处理：" + t);
        }
        for (const id of ["task-l4", "task-l5"]) {
          const t = await box(id);
          assert(t.includes("半成品的文件夹已经清理干净，可以重试") && !t.includes("文件夹没删干净"), id + "：没带 cleanupFailed 的照旧接 failHint，不按 error 里的字认：" + t);
        }
      } finally { await fState({ taskList: [] }); }
    });

    await run("导入现成服务端：版本号是从压缩包里的日志认出来的，识别结果的提示里写明（A44）", async () => {
      try {
        await fState({ importPack: { name: "作者服", format: "server", core: "paper", mc: "1.20.1", loader: "", mcFromLog: true },
          taskList: [{ id: "task-1", title: "导入服务端 作者服", kind: "server-in", ended: false }] });
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-file");
        await page.setInputFiles("#im-file", { name: "作者服.zip", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004server") });
        await page.check("#im-eula");
        await page.click("#im-go");
        await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已识别为现成服务端")));
        const t = await page.$$eval(".toast", ts => ts.map(x => x.textContent).join("|"));
        assert(t.includes("paper 1.20.1") && t.includes("版本号是从压缩包里的日志认出来的"), "提示里要写明版本号从哪来：" + t);
        await fState({ importPack: { name: "作者服", format: "server", core: "paper", mc: "1.20.1", loader: "", mcFromLog: false } });
        await page.evaluate(() => document.querySelectorAll(".toast").forEach(x => x.remove()));
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-file");
        await page.setInputFiles("#im-file", { name: "作者服.zip", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004server") });
        await page.check("#im-eula");
        await page.click("#im-go");
        await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已识别为现成服务端")));
        const t2 = await page.$$eval(".toast", ts => ts.map(x => x.textContent).filter(x => x.includes("已识别为现成服务端")).pop());
        assert(!t2.includes("日志"), "jar 里写了版本时不该提日志：" + t2);
      } finally { await fState({ importPack: null, taskList: [] }); }
    });

    await run("beta.3 流程 R13：接管失败以后「返回修改」重新识别那个文件夹，改过的名称、内存和 Java 还在", async () => {
      const failAdopt = r => r.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, data: { id: "task-adopt", kind: "adopt", title: "接管 回填", ended: true,
        error: "文件夹被别的程序占着", steps: [{ name: "检查服务器文件夹", status: "error" }], log: [] } }) });
      await page.route("**/api/tasks/task-adopt", failAdopt);
      try {
        await fState({ instReset: true, adoptScan: null });
        await page.goto(url("#/create/adopt")); await page.waitForSelector("#ad-dir");
        await page.fill("#ad-dir", "E:\\MCServers\\create-pack"); await page.click("#ad-scan"); await page.waitForSelector("#ad-name");
        await page.fill("#ad-name", "接管回填服"); await page.selectOption("#ad-java", "17");
        await page.fill("#ad-mem-mount .mem-num", "3072"); await page.press("#ad-mem-mount .mem-num", "Tab");
        if (await page.isVisible("#ad-eula")) await page.check("#ad-eula");
        await page.click("#ad-go"); await page.waitForFunction(() => location.hash === "#/task/task-adopt");
        await page.waitForSelector("#t-back-edit");
        assert((await h1()) === "服务器没有接管成", "接管失败的标题：" + await h1());
        const scans = reqs.length;
        await page.click("#t-back-edit"); await page.waitForSelector("#ad-name");
        const f = await page.evaluate(() => ({ hash: location.hash, dir: $("#ad-dir").value, name: $("#ad-name").value, java: $("#ad-java").value, mem: $("#ad-mem").value }));
        assert(f.hash === "#/create/adopt" && f.dir === "E:\\MCServers\\create-pack" && f.name === "接管回填服" && f.java === "17" && f.mem === "3072", "返回修改以后的接管表单：" + JSON.stringify(f));
        assert(reqs.slice(scans).some(r => r.m === "POST" && r.u === "/api/adopt/scan"), "回来时应重新识别一遍文件夹");
      } finally { await page.unroute("**/api/tasks/task-adopt", failAdopt); await fState({ instReset: true }); }
    });

    // 「返回修改」的草稿要在发请求之前拍下（2026-11-16 Codex 复审 beta.3）：以前请求回来以后才去读表单，大整合包上传、建服请求还没回来时
    // 切到别的页面，表单已经没了，一读就出错——任务明明在跑，界面却报「无法导入整合包」「无法创建服务器」，草稿也没留下。

    await run("beta.3 流程 R13：导入整合包上传途中切到别的页面，任务照常开始、不误报「无法导入」，失败以后「返回修改」填的都在（Codex 复审 beta.3）", async () => {
      const before = errors.length;
      try {
        await fState({ taskList: [{ id: "task-1", title: "导入整合包 测试整合包（forge 1.20.1）", ended: true, error: "下载 mods/a.jar 失败: 连接超时" }] });
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-file");
        await page.setInputFiles("#im-file", { name: "切页测试.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mrpack") });
        await page.fill("#im-name", "切页导入服"); await page.fill("#im-port", "25603"); await page.check("#im-wl"); await page.check("#im-eula");
        await holdPost("**/api/import/modpack", async (requested, release) => {
          await page.click("#im-go"); await requested;
          await page.click('#sidebar a[data-nav="instances"]'); await page.waitForSelector("#inst-cards");
          release();
          await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => /已识别|无法导入/.test(t.textContent)));
        });
        await settle(300);
        const t = await toastsNow();
        assert(!t.some(x => x.includes("无法导入整合包")) && t.some(x => x.includes("已识别")), "任务已经开始，不该报无法导入：" + JSON.stringify(t));
        assert(errors.length === before, "页面报错：" + errors.slice(before).join(" | "));
        assert(await page.evaluate(() => location.hash) === "#/instances", "切走以后不该被拉去任务页：" + await page.evaluate(() => location.hash));
        // 不刷新页面（草稿只在这一次打开的页面里），从任务页「返回修改」
        await page.evaluate(() => { location.hash = "#/task/task-1"; }); await page.waitForSelector("#t-back-edit");
        await page.click("#t-back-edit"); await page.waitForSelector("#im-name");
        const f = await page.evaluate(() => ({ file: ($("#im-file").files[0] || {}).name || "", name: $("#im-name").value, port: $("#im-port").value, wl: $("#im-wl").checked, eula: $("#im-eula").checked }));
        assert(f.file === "切页测试.mrpack" && f.name === "切页导入服" && f.port === "25603" && f.wl && f.eula, "返回修改以后的导入表单：" + JSON.stringify(f));
      } finally { await fState({ taskList: [] }); }
    });

    await run("beta.3 流程 R13：向导点了「开始部署」、请求还没回来就切走，服务器照常开始建、不误报「无法创建」，「返回修改」填的都在（Codex 复审 beta.3）", async () => {
      const before = errors.length;
      try {
        await fState({ modCart: { reset: true }, instReset: true, taskList: [{ id: "task-1", title: "创建服务器 切页建服（paper 1.21.4）", ended: true, cancelled: true, error: "已取消，没有创建服务器" }] });
        await page.goto(url("#/create/new")); await page.waitForSelector('.core-card[data-id="paper"]');
        await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click('.ver-item[data-id="1.21.4"]'); await page.click("#wnext"); await page.waitForSelector("#build-rec b");
        await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        await page.fill("#f-name", "切页建服"); await page.fill("#f-port", "25604"); await page.fill("#f-motd", "切走也留着");
        await page.check("#f-eula");
        await holdPost("**/api/instances", async (requested, release) => {
          await page.click("#wcreate"); await requested;
          await page.click('#sidebar a[data-nav="instances"]'); await page.waitForSelector("#inst-cards");
          release();
          await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => /已开始创建|无法创建服务器/.test(t.textContent)));
        });
        await settle(300);
        const t = await toastsNow();
        assert(!t.some(x => x.includes("无法创建服务器")) && t.some(x => x.includes("已开始创建")), "服务器已经开始建，不该报无法创建：" + JSON.stringify(t));
        assert(errors.length === before, "页面报错：" + errors.slice(before).join(" | "));
        await page.evaluate(() => { location.hash = "#/task/task-1"; }); await page.waitForSelector("#t-back-edit");
        await page.click("#t-back-edit"); await page.waitForSelector("#f-name");
        const f = await page.evaluate(() => ({ name: $("#f-name").value, port: $("#f-port").value, motd: $("#f-motd").value, eula: $("#f-eula").checked, sum: $("#wf-sum").textContent }));
        assert(f.name === "切页建服" && f.port === "25604" && f.motd === "切走也留着" && f.eula && f.sum.includes("Paper") && f.sum.includes("1.21.4"),
          "返回修改以后的向导：" + JSON.stringify(f));
      } finally { await fState({ taskList: [], instReset: true, modCart: { reset: true } }); }
    });

    await run("beta.3 流程 R13：带地图从向导创建、取消以后「返回修改」：那张地图原样选上、重新核对，检测结果没被清掉", async () => {
      await wReset();
      try {
        await wState({ taskList: [{ id: "task-1", title: "创建服务器 地图回填（vanilla 1.19.4）", ended: false, canCancel: true }] });
        await wWizPick("D:\\Maps\\SkyCity");
        const scanId = (await wState({})).world.lastScan.id;
        await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        await page.waitForSelector("#wi-wiz-plan #wi-rel");
        await page.fill("#f-name", "地图回填服"); await page.check("#f-eula");
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1");
        await page.locator("#t-cancel").waitFor({ state: "visible" }); await page.click("#t-cancel");
        await page.waitForSelector("#t-back-edit", { timeout: 8000 });
        const plans = (await wState({})).world.lastPlan;
        await page.click("#t-back-edit"); await page.waitForSelector("#wi-wiz-chosen"); await page.waitForSelector("#wi-wiz-plan #wi-rel");
        const st = await wState({});
        const f = await page.evaluate(() => ({ map: $("#wi-wiz-name").textContent, name: $("#f-name").value, eula: $("#f-eula").checked }));
        assert(f.map === "天空之城" && f.name === "地图回填服" && f.eula, "返回修改以后的向导：" + JSON.stringify(f));
        assert(!st.world.discards.includes(scanId) && st.world.lastPlan.scanId === scanId && st.world.lastPlan.id !== plans.id, "应拿同一份检测重新核对、不清掉：" + JSON.stringify([st.world.discards, st.world.lastPlan]));
      } finally { await wReset(); await wState({ taskList: [] }); }
    });

    await run("beta.3 流程 R09：任务页的没装上的文件按服务器取、按服务器处理；处理完上面那块「N 个文件待处理」跟着消失；跳过的还能改成用本地文件代替；坏地址不画成链接", async () => {
      const task = { id: "task-imx", title: "导入整合包 R09（forge 1.20.1）", ended: true, result: PACK,
        warnings: [{ kind: "import-pending", title: "2 个文件待处理", note: "这些文件没法自动下载。" }],
        pending: [{ id: "p1", path: "mods/no-hash.jar", reason: "nohash", note: "没有校验值", options: ["skip", "local"] },
          { id: "p2", path: "mods/host.jar", reason: "host", host: "files.example.net", note: "站点不在允许列表里", urls: ["javascript:alert(1)", "https://files.example.net/host.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] }] };
      try {
        await fState({ taskList: [task], trustedHosts: [] });
        const t0 = reqs.length;
        await page.goto(url("#/task/task-imx")); await page.waitForSelector("#t-pending .pnd-item");
        const enc = encodeURIComponent(PACK);
        assert(reqs.slice(t0).some(r => r.m === "GET" && r.u === `/api/instances/${enc}/import-pending`), "应按服务器取清单");
        assert(await page.locator('#t-pending a[href^="javascript"]').count() === 0 && await page.locator('#t-pending a[href^="https://files.example.net"]').count() === 1, "只该把 https 的地址画成链接");
        // 跳过 p1：黄框写 1 个，跳过的收进「已跳过的 1 个」
        await page.check('input[name="pnd-p1"][value="skip"]'); await page.click("#t-pending-apply"); await settle();
        assert((await page.textContent("#t-pending-out")).includes("已跳过"), "跳过应有结果");
        assert((await page.textContent("#t-msgs")).includes("1 个文件待处理") && await page.locator("#t-pending .pnd-skipped").count() === 1, "跳过以后：黄框写 1 个，跳过的收在下面");
        // 本地代替 p2：黄框消失，面板写都处理完了
        await page.check('input[name="pnd-p2"][value="local"]'); await page.fill('.pnd-item[data-id="p2"] input.pnd-local', "D:\\老服务器\\mods\\host.jar");
        await page.click("#t-pending-apply"); await settle();
        const after = await page.evaluate(() => ({ msgs: $("#t-msgs").textContent, pend: $("#t-pending").textContent, items: document.querySelectorAll("#t-pending .pnd-item").length }));
        assert(!after.msgs.includes("个文件待处理") && /跳过了 \d+ 个文件，它们没有装上/.test(after.pend) && after.items === 0, "都处理完以后黄框应消失（只剩跳过的）：" + JSON.stringify(after));
        // 跳过的还能改成用本地文件代替
        // 只剩跳过的时「已跳过的 N 个」默认展开
        await page.check('.pnd-skipped[data-id="p1"] input[value="local"]'); await page.fill('.pnd-skipped[data-id="p1"] input.pnd-local', "D:\\老服务器\\mods\\no-hash.jar");
        await page.click("#t-pending-apply"); await settle();
        assert(await page.locator("#t-pending .pnd-skipped").count() === 0 && (await page.textContent("#t-pending")).includes("没装上的文件都处理完了"), "跳过的处理完以后应清空");
        const posts = reqs.slice(t0).filter(r => r.m === "POST");
        assert(posts.every(r => r.u !== "/api/import/pending/task-imx") && posts.filter(r => r.u === `/api/instances/${enc}/import-pending`).length === 3, "处置应走按服务器的接口：" + JSON.stringify(posts.map(r => r.u)));
        // 程序重开以后（没有任务、只有服务器上的清单）：接口照样给，跳过的也在
        await fState({ taskList: [], instPending: { [PACK]: { pending: [], skipped: [{ id: "p9", path: "config/x.toml", reason: "badurl", options: ["local"] }] } } });
        const v = await page.evaluate(n => fetch(`/api/instances/${encodeURIComponent(n)}/import-pending`, { headers: { "X-Token": "mock" } }).then(r => r.json()), PACK);
        assert(v.ok && v.data.skipped.length === 1 && v.data.pending.length === 0, "按服务器的接口：" + JSON.stringify(v));
      } finally { await fState({ taskList: [], trustedHosts: [] }); }
    });

    await run("导入整合包完成：结果里列出导入时跳过的文件；留下了没装上的文件的清单时，任务页有「打开服务器文件夹」（只在本机，A46）", async () => {
      const task = { id: "task-skip", title: "导入整合包 原版包（vanilla 1.20.1）", ended: true, result: PACK,
        warnings: [{ kind: "import-skipped", title: "2 个文件没有装进来", note: "导入时跳过了这些文件，原因写在每一项后面。",
          items: [{ name: "resourcepacks/光影.zip（整合包清单里写明只给客户端用）" }, { name: "config/CON.txt（名字在 Windows 上写不出来）" }] }],
        pending: [{ id: "p1", path: "datapacks/x.zip", reason: "nohash", options: ["skip", "local"] }] };
      try {
        await fState({ taskList: [task] });
        await page.goto(url("#/task/task-skip")); await page.waitForSelector("#t-open-inst-dir");
        const msgs = await page.textContent("#t-msgs");
        assert(msgs.includes("2 个文件没有装进来") && msgs.includes("resourcepacks/光影.zip（整合包清单里写明只给客户端用）"), "结果里应列出跳过的文件：" + msgs);
        await page.click("#t-open-inst-dir"); await settle(300);
        const st = await fState({});
        assert(st.lastOpenDir && st.lastOpenDir.inst === PACK && !st.lastOpenDir.sub, "应打开这台服务器的文件夹：" + JSON.stringify(st.lastOpenDir));
        await fState({ taskList: [{ ...task, pending: [] }] });
        await page.goto(url("#/task/task-skip")); await page.waitForSelector("#t-foot a.primary"); await settle(300);
        assert(await page.locator("#t-open-inst-dir").count() === 0, "没有留下清单时不该有这个按钮");
        await fState({ taskList: [task], local: false });
        await page.goto(url("#/task/task-skip")); await page.waitForSelector("#t-pending .pnd-item"); await settle(300);
        assert(await page.locator("#t-open-inst-dir").count() === 0, "局域网打开的不该有这个按钮");
      } finally { await fState({ taskList: [], local: true }); }
    });

    await run("beta.3 流程 R13：导出交接包的任务页侧栏亮「服务器」，完成后标题是「交接包已导出」，有「打开所在文件夹」（只在本机）", async () => {
      try {
        await page.goto(url("#/task/task-transfer-out")); await page.waitForSelector("#t-open-dir");
        assert(await navOn() === "instances" && (await h1()) === "交接包已导出", "导出任务页：" + JSON.stringify([await navOn(), await h1()]));
        await page.click("#t-open-dir"); await settle(300);
        const st = await fState({});
        assert(st.lastOpenPath === "E:\\服务器备份", "打开的应是交接包所在的文件夹：" + st.lastOpenPath);
        await fState({ local: false });
        await page.goto(url("#/task/task-transfer-out")); await page.waitForSelector("#t-foot a.btn.primary"); await settle(300);
        assert(!(await page.$("#t-open-dir")), "别的设备上不给「打开所在文件夹」");
        await fState({ local: true });
        // 换 Java 也是从服务器那边发起的；新建、接管亮「新建服务器」
        await page.goto(url("#/task/task-java")); await page.waitForSelector("#t-foot a.btn.primary");
        assert(await navOn() === "instances", "换 Java 的任务页侧栏应亮「服务器」");
        await page.goto(url("#/task/task-adopt")); await page.waitForSelector("#t-foot a.btn.primary");
        assert(await navOn() === "create", "接管的任务页侧栏应亮「新建服务器」");
      } finally { await fState({ local: true }); }
    });

    await run("beta.3 流程 R22：选构建默认收起成「高级」；选插件这一步能「跳过这一步」（清单有东西时先问，建完不装）；EULA 不被吸底栏挡住；中文名下面说清文件夹怎么起名", async () => {
      try {
        await fState({ modCart: { reset: true }, taskStage: 4, instReset: true });
        await page.goto(url("#/create/new")); await page.waitForSelector('.core-card[data-id="paper"]');
        await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click("#wnext"); await page.waitForSelector("#build-rec b");
        const b = await page.evaluate(() => ({ open: $("#build-adv").open, rec: $("#build-rec").textContent, next: $("#wnext").disabled }));
        assert(!b.open && !(await page.isVisible("#build-list")) && b.rec.includes("推荐的构建") && b.rec.includes("232") && !b.next, "构建默认收起、用推荐的：" + JSON.stringify(b));
        await page.click("#build-adv summary"); await page.click('#build-list .ver-item[data-id="231"]');
        assert((await page.textContent("#build-rec")).includes("你选的构建 231") && (await page.textContent("#wf-sum")).includes("构建 231"), "换了构建以后上面那一行跟着变");
        await page.click("#wnext"); await page.waitForSelector("#mc-q");
        assert((await page.textContent("#wskip")).includes("跳过这一步"), "选插件这一步应有「跳过这一步」");
        await page.fill("#mc-q", "LuckPerms"); await page.click("#mc-go");
        await page.click('button[aria-label="加入清单 LuckPerms"]'); await settle(700);
        await page.click("#wskip"); await page.waitForSelector(".modal [data-x=ok]");
        assert((await page.textContent(".modal")).includes("待装清单里已经有 1 项"), "清单里有东西时跳过要先问");
        await page.click(".modal [data-x=cancel]"); await settle(300);
        assert(await page.$("#mc-q") && !(await page.$("#f-name")), "点「取消」留在这一步");
        await page.click("#wskip"); await page.waitForSelector(".modal [data-x=ok]"); await page.click(".modal [data-x=ok]");
        await page.waitForSelector("#f-name");
        // EULA 和吸底栏贴在一起，窗口矮的时候也不被挡住
        await page.setViewportSize({ width: 1280, height: 640 }); await settle(300);
        const g = await page.evaluate(() => { const e = $(".eula-box").getBoundingClientRect(), f = $(".wizard-foot").getBoundingClientRect(); return { eb: e.bottom, et: e.top, ft: f.top, vh: innerHeight }; });
        assert(g.eb <= g.ft + 1 && g.eb <= g.vh && g.et >= 0, "EULA 开关应完整露在吸底栏上面：" + JSON.stringify(g));
        await page.setViewportSize({ width: 1280, height: 850 });
        // 中文名：不再写 server-（自动）
        await page.fill("#f-name", "生存服");
        let p = await page.textContent("#f-path");
        assert(!p.includes("（自动）") && p.includes("server-创建时的日期和时间") && /server-\d{8}-\d{6}/.test(p), "中文名的文件夹说明：" + p);
        await page.fill("#f-name", "Paper生存服");
        p = await page.textContent("#f-path");
        assert(p.includes("\\Paper") && !p.includes("创建时的日期"), "有英文字母时照常写：" + p);
        // 跳过了：清单里有东西也不装，直接去建服任务页
        await page.check("#f-eula");
        const t0 = reqs.length;
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        assert(!reqs.slice(t0).some(r => r.u.endsWith("/modcart/install")), "跳过这一步以后不该装清单");
      } finally { await page.setViewportSize({ width: 1280, height: 850 }); await fState({ modCart: { reset: true }, taskStage: 2, instReset: true }); }
    });
  }
  // ==================== beta.3 端口与没装上的文件（R08 R09） ====================
  // docs/测试与评审/可用性走查-2026-11-14.md 第八节 R08、R09（界面），外加换地图的取消显示「已取消」。用例名都以「beta.3 端口」开头，node tests.js "beta.3 端口" 只跑这一块。
  // 仿真后端用到的：GET /api/ports/suggest、启动时端口和另一台没停着的服务器撞了、改 server-port 时 port 跟着变、restart（模拟程序重开），
  // 见 server.js「/__mock/state」说明里 beta.3 R08 那几行。仿真里四台服务器占着 25565、25566、25567 和代理端的 25577，默认端口应是 25568
  {
    const xState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PACKX = "机械动力整合包", SURVX = "生存服 · 朋友";
    const toWizardFinal = async hash => {
      await page.goto(url(hash)); await page.waitForSelector('.core-card[data-id="paper"]');
      await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
      await page.click("#wnext"); await page.waitForSelector("#f-port");
      await page.waitForFunction(() => document.querySelector("#f-port-note") !== null);
    };
    const portView = (input, note) => page.evaluate(([i, n]) => ({ v: $(i).value, note: $(n).textContent.trim(), warn: $(n).classList.contains("warn"),
      invalid: $(i).classList.contains("invalid"), err: ($(i).closest(".field").querySelector(".field-err") || {}).textContent || "" }), [input, note]);

    await run("beta.3 端口 R08：新建、用已有地图开服、导入整合包、接管四处的默认端口避开别的服务器在用的，并写明换过；「返回修改」填回来的不换", async () => {
      try {
        await xState({ instReset: true, taskStage: 4, modCart: { reset: true } });
        // 从零创建：25565 被生存服用着，默认 25568，写明换过；提交的就是它
        await toWizardFinal("#/create/new");
        let f = await portView("#f-port", "#f-port-note");
        assert(f.v === "25568" && f.note.includes("25565 已被「生存服 · 朋友」使用，已改为 25568") && !f.warn && !f.invalid, "向导的默认端口：" + JSON.stringify(f));
        assert(!(await page.textContent("#main")).includes("端口（默认 25565）"), "端口的标签不该再写「默认 25565」");
        await page.fill("#f-name", "端口默认值测试"); await page.check("#f-eula");
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        let st = await xState({});
        assert(st.lastCreate && st.lastCreate.port === 25568, "新建的请求应带默认端口 25568：" + JSON.stringify(st.lastCreate && st.lastCreate.port));
        await xState({ instReset: true });
        // 用已有地图开服：同一个向导，同样避开
        await toWizardFinal("#/create/world");
        f = await portView("#f-port", "#f-port-note");
        assert(f.v === "25568" && f.note.includes("已改为 25568"), "用已有地图开服的默认端口：" + JSON.stringify(f));
        // 导入整合包
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-port-note");
        await page.waitForFunction(() => $("#im-port").value === "25568", null, { timeout: 3000 }).catch(() => {});
        f = await portView("#im-port", "#im-port-note");
        assert(f.v === "25568" && f.note.includes("25565 已被「生存服 · 朋友」使用，已改为 25568"), "导入整合包的默认端口：" + JSON.stringify(f));
        await page.setInputFiles("#im-file", { name: "端口测试.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mrpack") });
        await page.check("#im-eula"); await page.click("#im-go"); await settle(800);
        st = await xState({});
        assert(st.lastImport && st.lastImport.port === "25568", "导入整合包的请求：" + JSON.stringify(st.lastImport && st.lastImport.port));
        // 「返回修改」：填回来的端口不被默认值盖掉（撞了照样提示）
        await xState({ taskList: [{ id: "task-1", title: "导入整合包 端口测试（forge 1.20.1）", ended: true, error: "下载 mods/a.jar 失败: 连接超时" }] });
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-port-note");
        await page.setInputFiles("#im-file", { name: "端口测试.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mrpack") });
        await page.fill("#im-port", "25566"); await page.check("#im-eula");
        await page.click("#im-go"); await page.waitForSelector("#t-back-edit");
        await page.click("#t-back-edit"); await page.waitForSelector("#im-port-note"); await settle(300);
        f = await portView("#im-port", "#im-port-note");
        assert(f.v === "25566" && f.warn && f.note.includes("机械动力整合包"), "返回修改填回来的端口不该被换掉，撞了照样提示：" + JSON.stringify(f));
        await xState({ taskList: [] });
        // 接管：识别出来的 25565 撞了生存服，默认换成 25568，并写明会改 server.properties；提交带上它
        const scan = { dir: "D:\\old\\S1", name: "S1", core: "paper", mc: "1.21.4", build: "", launchTarget: "jar:paper.jar",
          targets: [{ target: "jar:paper.jar", label: "paper.jar（Paper 1.21.4）", core: "paper", mc: "1.21.4", build: "", javaMajor: 0 }],
          javaMajor: 0, xmxMb: 0, extraJvm: [], port: 25565, eula: true, hasWorld: true, managed: false, notes: [], whitelistOn: false };
        await xState({ adoptScan: scan });
        await page.goto(url("#/create/adopt")); await page.waitForSelector("#ad-dir");
        await page.fill("#ad-dir", "D:\\old\\S1"); await page.keyboard.press("Enter"); await page.waitForSelector("#ad-port");
        f = await portView("#ad-port", "#ad-port-note");
        assert(f.v === "25568" && f.note.includes("25565 已被「生存服 · 朋友」使用，已改为 25568") && f.note.includes("server.properties 里的端口从 25565 改成 25568"), "接管的默认端口：" + JSON.stringify(f));
        await page.click("#ad-go"); await settle(800);
        st = await xState({});
        assert(st.lastAdopt && st.lastAdopt.port === 25568, "接管的请求应带新端口：" + JSON.stringify(st.lastAdopt));
        // 识别出来的端口没撞：沿用，不提示要改配置；代理端没有端口这一项、请求里是 0
        await xState({ adoptScan: { ...scan, port: 25601 } });
        await page.goto(url("#/create/adopt")); await page.waitForSelector("#ad-dir");
        await page.fill("#ad-dir", "D:\\old\\S1"); await page.keyboard.press("Enter"); await page.waitForSelector("#ad-port");
        f = await portView("#ad-port", "#ad-port-note");
        assert(f.v === "25601" && f.note === "", "没撞就沿用原来的端口、不提示：" + JSON.stringify(f));
        await page.selectOption("#ad-core", "velocity");
        assert(await page.$eval("#ad-port-f", e => e.hidden), "代理端不该有端口这一项");
        await page.click("#ad-go"); await settle(800);
        st = await xState({});
        assert(st.lastAdopt && st.lastAdopt.port === 0, "代理端的请求端口应是 0：" + JSON.stringify(st.lastAdopt));
      } finally { await xState({ instReset: true, adoptScan: null, taskList: [], taskStage: 2, modCart: { reset: true } }); }
    });

    await run("新建、导入整合包的默认端口也避开被别的程序占着的，并写明；手填被别的程序占着的当场提示、不拦；接管只提示不换（回访走查 11-16）", async () => {
      try {
        // 生存服挪到 25590：25565 只被别的程序（java.exe）占着；25568 也被一个查不到名字的程序占着
        await xState({ instReset: true, taskStage: 4, modCart: { reset: true }, otherPorts: { 25565: "java.exe", 25568: "" } });
        await xState({ inst: { name: SURVX, port: 25590 } });
        await toWizardFinal("#/create/new");
        let f = await portView("#f-port", "#f-port-note");
        assert(f.v === "25569" && f.note.includes("25565 被别的程序（java.exe）占着，已改为 25569") && !f.warn, "向导的默认端口：" + JSON.stringify(f));
        // 手填被别的程序占着的：停手一下就提示（查不到程序名时不写名字），不拦着；换成空着的提示就没了
        await page.fill("#f-port", "25568"); await settle(700);
        f = await portView("#f-port", "#f-port-note");
        assert(f.warn && f.note.includes("端口 25568 现在被别的程序占着，开服时会报错") && !f.note.includes("（"), "手填被占着的：" + JSON.stringify(f));
        await page.fill("#f-port", "25570"); await settle(700);
        f = await portView("#f-port", "#f-port-note");
        assert(!f.warn && f.note === "", "换成空着的：" + JSON.stringify(f));
        // 手填的还是本软件服务器用着的：照旧说是哪台，不再去问别的程序
        await page.fill("#f-port", "25566"); await settle(700);
        f = await portView("#f-port", "#f-port-note");
        assert(f.warn && f.note.includes("机械动力整合包") && !f.note.includes("别的程序"), "本软件的服务器用着：" + JSON.stringify(f));
        // 导入整合包：同样避开
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-port-note");
        await page.waitForFunction(() => $("#im-port").value === "25569", null, { timeout: 3000 }).catch(() => {});
        f = await portView("#im-port", "#im-port-note");
        assert(f.v === "25569" && f.note.includes("25565 被别的程序（java.exe）占着，已改为 25569"), "导入整合包的默认端口：" + JSON.stringify(f));
        // 接管：识别出来的 25565 只被别的程序占着（多半是原来那台还开着）——不换，只提示
        await xState({ adoptScan: { dir: "D:\\old\\S2", name: "S2", core: "paper", mc: "1.21.4", build: "", launchTarget: "jar:paper.jar",
          targets: [{ target: "jar:paper.jar", label: "paper.jar（Paper 1.21.4）", core: "paper", mc: "1.21.4", build: "", javaMajor: 0 }],
          javaMajor: 0, xmxMb: 0, extraJvm: [], port: 25565, eula: true, hasWorld: true, managed: false, notes: [], whitelistOn: false } });
        await page.goto(url("#/create/adopt")); await page.waitForSelector("#ad-dir");
        await page.fill("#ad-dir", "D:\\old\\S2"); await page.keyboard.press("Enter"); await page.waitForSelector("#ad-port"); await settle(700);
        f = await portView("#ad-port", "#ad-port-note");
        assert(f.v === "25565" && f.warn && f.note.includes("端口 25565 现在被别的程序（java.exe）占着") && f.note.includes("如果是原来的服务器还开着，接管前先把它停掉") &&
          !f.note.includes("server.properties"), "接管只提示不换：" + JSON.stringify(f));
        // 打开表单时 25565 空着（不用再问）；之后别的程序占上了，手填回 25565 照样要问、要提示（Codex 复审 beta.3 之后第二轮：
        // 以前免查对之后的手填也生效）
        await xState({ otherPorts: {} });
        await toWizardFinal("#/create/new");
        f = await portView("#f-port", "#f-port-note");
        assert(f.v === "25565" && f.note === "", "25565 空着时照用、不提示：" + JSON.stringify(f));
        await page.fill("#f-port", "25599"); await settle(700);
        await xState({ otherPorts: { 25565: "java.exe" } });
        await page.fill("#f-port", "25565"); await settle(700);
        f = await portView("#f-port", "#f-port-note");
        assert(f.warn && f.note.includes("端口 25565 现在被别的程序（java.exe）占着"), "手填回默认端口也要查：" + JSON.stringify(f));
      } finally { await xState({ instReset: true, adoptScan: null, otherPorts: {}, taskStage: 2, modCart: { reset: true } }); }
    });

    await run("beta.3 端口 R08：手填的端口和别的服务器撞了当场提示（写明是哪台、开着没有，给一个没人用的），不标红、不拦着创建；改成没人用的提示就没了", async () => {
      try {
        await xState({ instReset: true, taskStage: 4, modCart: { reset: true } });
        await toWizardFinal("#/create/new");
        await page.fill("#f-port", "25566");
        let f = await portView("#f-port", "#f-port-note");
        assert(f.warn && f.note.includes("端口 25566 已被你的服务器「机械动力整合包」使用") && f.note.includes("比如 25568") && !f.note.includes("开着") && !f.invalid && !f.err,
          "撞了停着的服务器：" + JSON.stringify(f));
        await page.fill("#f-port", "25565");
        f = await portView("#f-port", "#f-port-note");
        assert(f.warn && f.note.includes("生存服 · 朋友") && f.note.includes("它现在开着"), "撞了开着的服务器：" + JSON.stringify(f));
        await page.fill("#f-port", "25570");
        f = await portView("#f-port", "#f-port-note");
        assert(f.note === "" && !f.warn, "改成没人用的以后提示应消失：" + JSON.stringify(f));
        // 撞了也能创建（两台从不同时开的服务器可以共用一个端口）
        await page.fill("#f-port", "25566"); await page.fill("#f-name", "撞端口也能建"); await page.check("#f-eula");
        await page.click("#wcreate"); await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        const st = await xState({});
        assert(st.lastCreate && st.lastCreate.port === 25566, "撞了只提示、不拦着：" + JSON.stringify(st.lastCreate && st.lastCreate.port));
        // 导入整合包也一样
        await xState({ instReset: true });
        await page.goto(url("#/create/import")); await page.waitForSelector("#im-port-note");
        await page.fill("#im-port", "25567");
        f = await portView("#im-port", "#im-port-note");
        assert(f.warn && f.note.includes("原版快照测试") && f.note.includes("它现在开着"), "导入整合包手填撞了：" + JSON.stringify(f));
      } finally { await xState({ instReset: true, taskStage: 2, modCart: { reset: true } }); }
    });

    await run("beta.3 端口 R08：启动时端口被自己的另一台服务器占着，报错点它的名字（卡片和页头都是），「改成 N 并启动」一步改端口、保存、启动", async () => {
      try {
        await xState({ instReset: true });
        await xState({ inst: { name: PACKX, port: 25565 }, starts: [] }); // 和开着的生存服撞了
        await page.goto(url(`#/inst/${encodeURIComponent(PACKX)}/players`)); await page.waitForSelector("#d-start");
        await page.click("#d-start"); await page.waitForSelector("#d-start-fail");
        assert(await toastHas("端口 25565 被你的服务器「生存服 · 朋友」占着"), "右上角的提示应点名占着端口的服务器");
        const n = await page.evaluate(() => ({ text: $("#d-start-fail").textContent, fix: ($("#d-start-fail [data-port-fix]") || {}).textContent || "", edit: !!$("#d-start-fail [data-port-edit]") }));
        assert(n.text.includes("端口 25565 被你的服务器「生存服 · 朋友」占着") && !n.text.includes("别的程序") && n.fix.trim() === "改成 25566 并启动" && !n.edit,
          "页头的提示：" + JSON.stringify(n));
        await page.goto(url("#/instances")); await page.waitForSelector(`.card[data-name="${PACKX}"] .inst-fail`);
        const card = await page.$eval(`.card[data-name="${PACKX}"] .inst-fail`, e => e.textContent);
        assert(card.includes("端口 25565 被你的服务器「生存服 · 朋友」占着"), "卡片上也点名：" + card);
        // 一步：改 server-port、保存、启动；起来了提示就没了
        await page.goto(url(`#/inst/${encodeURIComponent(PACKX)}/players`)); await page.waitForSelector("#d-start-fail [data-port-fix]");
        const t0 = reqs.length;
        await page.click("#d-start-fail [data-port-fix]");
        await page.waitForSelector("#d-start-fail", { state: "detached", timeout: 5000 });
        const st = await xState({});
        const seq = reqs.slice(t0).filter(r => r.m === "PUT" || r.m === "POST").map(r => r.m + " " + decodeURIComponent(r.u));
        assert(JSON.stringify(st.lastPropsPut) === JSON.stringify({ inst: PACKX, pairs: [{ key: "server-port", value: "25566" }] }), "应把 server-port 改成 25566：" + JSON.stringify(st.lastPropsPut));
        assert(seq.indexOf(`PUT /api/instances/${PACKX}/properties`) >= 0 && seq.indexOf(`PUT /api/instances/${PACKX}/properties`) < seq.lastIndexOf(`POST /api/instances/${PACKX}/start`),
          "先改端口再启动：" + JSON.stringify(seq));
        assert(JSON.stringify(st.starts.slice(-1)) === JSON.stringify([{ inst: PACKX, port: 25566 }]), "改好以后用新端口启动：" + JSON.stringify(st.starts));
        assert(await toastHas("端口改成了 25566，正在启动"), "应说端口改成了几、正在启动");
      } finally { await xState({ instReset: true, starts: [] }); }
    });

    await run("beta.3 端口 R08：端口被别的程序占着时按钮写「改端口」，点了去「设置 → 常用」里的端口那一项；没占着端口的启动失败没有这个按钮", async () => {
      try {
        await xState({ instReset: true });
        await xState({ inst: { name: PACKX, startFail: "端口 25566 被别的程序占着，换个端口或关掉占用它的程序", startPortBusyFail: { port: 25566 } } });
        await page.goto(url(`#/inst/${encodeURIComponent(PACKX)}/players`)); await page.waitForSelector("#d-start");
        await page.click("#d-start"); await page.waitForSelector("#d-start-fail [data-port-edit]");
        const n = await page.evaluate(() => ({ text: $("#d-start-fail").textContent, edit: $("#d-start-fail [data-port-edit]").textContent.trim(), fix: !!$("#d-start-fail [data-port-fix]") }));
        assert(n.text.includes("被别的程序占着") && n.edit === "改端口" && !n.fix, "别的程序占着：" + JSON.stringify(n));
        await page.click("#d-start-fail [data-port-edit]"); await page.waitForSelector('[data-k="server-port"]'); await settle(300);
        const p = await page.evaluate(() => ({ h: decodeURIComponent(location.hash), focus: document.activeElement && document.activeElement.dataset.k }));
        assert(p.h === `#/inst/${PACKX}/settings/common` && p.focus === "server-port", "去设置里的端口：" + JSON.stringify(p));
        // 在「设置 → 常用」上点「改端口」：不换页，端口那一项照样亮起来
        await page.evaluate(() => document.activeElement.blur());
        await page.click("#d-start-fail [data-port-edit]"); await page.waitForSelector('[data-k="server-port"]'); await settle(400);
        assert(await page.evaluate(() => document.activeElement && document.activeElement.dataset.k) === "server-port", "已经在常用设置里也该聚焦到端口");
        // 别的原因没起来（找不到 Java 这类）：没有改端口的按钮
        await xState({ inst: { name: PACKX, startFail: "找不到这台服务器用的 Java，请到这台服务器详情页的「设置 → 启动与内存」里重新选一个 Java。", startPortBusyFail: null } });
        await page.goto(url(`#/inst/${encodeURIComponent(PACKX)}/players`)); await page.waitForSelector("#d-start");
        await page.click("#d-start");
        await page.waitForFunction(() => { const el = document.querySelector("#d-start-fail"); return el && el.textContent.includes("找不到这台服务器用的 Java"); });
        assert(!(await page.$("#d-start-fail [data-port-edit]")) && !(await page.$("#d-start-fail [data-port-fix]")), "不是端口的问题不该给改端口");
      } finally { await xState({ instReset: true }); }
    });

    await run("beta.3 端口 R09：详情页头常驻「N 个整合包文件没装上」，「处理」打开处理面板，处理完提示消失；跳过的在插件 / 模组页「整合包里没装上的」能再处理；程序重开以后照样找得到", async () => {
      const task = { id: "task-imr", title: "导入整合包 R09 界面（forge 1.20.1）", ended: true, result: PACKX,
        warnings: [{ kind: "import-pending", title: "2 个文件待处理", note: "这些文件没法自动下载。" }],
        pending: [{ id: "p1", path: "mods/no-hash.jar", reason: "nohash", note: "没有校验值", options: ["skip", "local"] },
          { id: "p2", path: "mods/host.jar", reason: "host", host: "files.example.net", note: "站点不在允许列表里", urls: ["https://files.example.net/host.jar"], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"] }] };
      const enc = encodeURIComponent(PACKX);
      try {
        await xState({ instReset: true, taskList: [task], trustedHosts: [] });
        await xState({ restart: true }); // 程序重开：导入任务没了，清单在服务器文件夹里
        let st = await xState({});
        assert(st.taskList.length === 0, "重开以后任务记录应没了");
        await page.goto(url(`#/inst/${enc}/console`)); await page.waitForSelector("#d-import-pending");
        let n = await page.textContent("#d-import-pending");
        assert(n.includes("这台服务器有 2 个整合包文件没装上") && (await page.textContent("#d-import-pending [data-pnd-open]")).trim() === "处理", "页头的提示：" + n);
        // 别的服务器没有这一条
        await page.goto(url(`#/inst/${encodeURIComponent(SURVX)}/console`)); await page.waitForSelector("#d-status"); await settle(400);
        assert(!(await page.$("#d-import-pending")), "没有没装上的文件的服务器不该有这一条");
        // 点「处理」：叠层里就是处理面板，跳过 p1 以后页头写 1 个
        await page.goto(url(`#/inst/${enc}/console`)); await page.waitForSelector("#d-import-pending [data-pnd-open]");
        await page.click("#d-import-pending [data-pnd-open]"); await page.waitForSelector(".pnd-modal #d-pending-box .pnd-item");
        const t0 = reqs.length;
        await page.check('.pnd-modal input[name="pnd-p1"][value="skip"]'); await page.click("#d-pending-apply"); await settle(400);
        assert((await page.textContent("#d-import-pending")).includes("有 1 个整合包文件没装上"), "跳过一个以后页头应写 1 个");
        assert(await page.locator(".pnd-modal .pnd-skipped").count() === 1, "跳过的收在面板下面");
        await page.check('.pnd-modal input[name="pnd-p2"][value="local"]'); await page.fill('.pnd-modal .pnd-item[data-id="p2"] input.pnd-local', "D:\\老服务器\\mods\\host.jar");
        await page.click("#d-pending-apply"); await settle(400);
        assert(!(await page.$("#d-import-pending")), "都处理完（跳过的不算）页头那一条应消失");
        assert(/跳过了 \d+ 个文件，它们没有装上/.test(await page.textContent(".pnd-modal")), "面板里应写跳过了几个、没有装上");
        const posts = reqs.slice(t0).filter(r => r.m === "POST").map(r => decodeURIComponent(r.u));
        assert(posts.length === 2 && posts.every(u => u === `/api/instances/${PACKX}/import-pending`), "处置走按服务器的接口：" + JSON.stringify(posts));
        await page.keyboard.press("Escape"); await settle(300);
        assert(!(await page.$(".pnd-modal")), "Esc 应关掉叠层");
        // 再重开一次：跳过的那个还在服务器上，页头不提示，插件 / 模组页里「整合包里没装上的」能再处理
        await xState({ restart: true });
        await page.goto(url(`#/inst/${enc}/addons`)); await page.waitForSelector("#ad-pending-box .pnd-skipped");
        const blk = await page.evaluate(() => ({ hidden: $("#ad-pending").hidden, head: $("#ad-pnd-h").textContent, skipped: document.querySelectorAll("#ad-pending .pnd-skipped").length,
          notice: !!$("#d-import-pending") }));
        assert(!blk.hidden && blk.head === "整合包里没装上的" && blk.skipped === 1 && !blk.notice, "插件 / 模组页的那一块：" + JSON.stringify(blk));
        await page.check('#ad-pending .pnd-skipped[data-id="p1"] input[value="local"]'); await page.fill('#ad-pending .pnd-skipped[data-id="p1"] input.pnd-local', "D:\\老服务器\\mods\\no-hash.jar");
        await page.click("#ad-pending-apply"); await settle(400);
        assert(await page.locator("#ad-pending .pnd-skipped").count() === 0 && (await page.textContent("#ad-pending")).includes("没装上的文件都处理完了"), "跳过的处理完以后应清空");
        st = await xState({});
        assert(!st.instPending[PACKX].pending.length && !st.instPending[PACKX].skipped.length, "服务器上的清单应清空：" + JSON.stringify(st.instPending[PACKX]));
        // 在插件 / 模组页上点页头的「处理」：滚到页里那一块，不另开叠层（一页只挂一份面板）
        await xState({ instPending: { [PACKX]: { pending: [task.pending[0]], skipped: [] } } });
        await page.goto(url(`#/inst/${enc}/addons`)); await page.waitForSelector("#ad-pending-box .pnd-item"); await page.waitForSelector("#d-import-pending [data-pnd-open]");
        await page.click("#d-import-pending [data-pnd-open]"); await settle(500);
        assert(!(await page.$(".pnd-modal")) && await page.evaluate(() => document.activeElement && document.activeElement.id) === "ad-pnd-h", "插件 / 模组页上应滚到页里那一块");
        // 一个都没有的服务器：插件 / 模组页没有这一块
        await page.goto(url(`#/inst/${encodeURIComponent(SURVX)}/addons`)); await page.waitForSelector("#ad-list"); await settle(500);
        assert(await page.$eval("#ad-pending", e => e.hidden), "没有没装上的文件时不该出现这一块");
      } finally { await xState({ instReset: true, taskList: [], trustedHosts: [] }); }
    });

    await run("beta.3 端口 换地图：复制阶段取消以后，任务页显示中性的「已取消」（页头、步骤是横线、不是红框），不再按失败显示", async () => {
      await wReset();
      try {
        await page.goto(url(worldTab(PACKX))); await page.waitForSelector("#wi-start");
        await page.click("#wi-start"); await page.waitForSelector("#wi-upload");
        await page.fill("#wi-path", "D:\\Maps\\plain-world"); await page.click("#wi-scan"); await page.waitForSelector("#wi-go");
        await page.click("#wi-go"); await page.waitForSelector(".modal");
        await page.click('.modal [data-x="ok"]');
        await page.waitForSelector("#wi-cancel"); await page.click("#wi-cancel");
        await page.waitForSelector("#wi-task-cancelled", { timeout: 15000 }); // 换地图页自己的面板也是中性的「已取消」（beta.3 主会话补）
        const taskId = Object.keys((await wState({})).world.tasks)[0];
        await page.goto(url("#/task/" + taskId)); await page.waitForSelector("#t-foot a.btn");
        const t = await page.evaluate(() => ({ h1: $(".page-head h1").textContent, sub: $(".page-head .sub").textContent, eyebrow: $("#task-eyebrow").textContent,
          note: ($("#t-msgs .task-note") || {}).textContent || "", err: !!$("#t-msgs .err-box"), steps: [...document.querySelectorAll("#t-steps .task-step")].map(s => s.className),
          back: $("#t-foot a.btn").getAttribute("href") }));
        assert(t.h1 === "已取消换地图" && t.sub.includes("没有替换任何文件") && t.eyebrow === "已取消", "任务页的页头：" + JSON.stringify(t));
        assert(t.note.includes("已取消，没有替换任何文件") && !t.err, "应是中性的说明，不是红色的失败框：" + JSON.stringify(t));
        assert(t.steps.some(c => c.includes("cancelled")) && !t.steps.some(c => c.includes("error")), "取消的那一步是横线、不是红叉：" + JSON.stringify(t.steps));
        assert(t.back === "#/instances", "底下回服务器列表：" + t.back);
      } finally { await wReset(); }
    });
  }
  // ==================== 地图导入结束 ====================
  // ==================== beta.3 P0（R01–R05） ====================
  // docs/测试与评审/可用性走查-2026-11-14.md 第八节 R01–R05，外加 U28、U30。用例名都以「beta.3 P0」开头，node tests.js "beta.3 P0" 只跑这一块。
  // 仿真后端用到的字段见 server.js「/__mock/state」说明里 beta.3 P0 那几行（startFail、whitelistOn、mdFiles、connectPort……）
  {
    const pState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PACK0 = "机械动力整合包", SURV = "生存服 · 朋友", PROXY = "群组代理";
    const detail = async (n, t = "console") => { await page.goto(url(`#/inst/${encodeURIComponent(n)}/${t}`)); await page.waitForSelector("#d-status"); await settle(300); };
    const clip = () => page.evaluate(() => { window.__copied = null; Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied = t; } } }); });
    const copied = () => page.evaluate(() => window.__copied);
    // /__mock/state 先改 inst、后做 instReset：两样放在一次请求里，改的字段会被重置掉，所以分两次
    const resetWith = async inst => { await pState({ instReset: true }); await pState({ inst }); };
    const forgetIP = () => page.evaluate(() => { try { localStorage.removeItem("amh_lan_ip"); } catch {} });
    const openDelete = async n => {
      await page.goto(url("#/instances")); await settle();
      await page.click(`.card[data-name="${n}"] [data-act="more"]`);
      await page.click(".dropmenu button.danger"); await page.waitForSelector("#modal-root input[data-k=files]"); await settle(200);
    };

    await run("beta.3 P0 R01：详情页头写出连接地址（排第一的局域网 IP + 端口），能复制、能换 IP；停着时灰字写明开服后连这个；改了端口跟着变；代理端用它自己的端口，没生成配置时说清楚；拿不到 IP 时照常打开", async () => {
      await pState({ instReset: true });
      await forgetIP();
      try {
        await detail(SURV);
        let a = await page.evaluate(() => ({ v: $("#d-addr-v").textContent, off: $("#d-addr").classList.contains("off"), all: $("#d-addr").textContent }));
        assert(a.v === "192.168.1.23:25565" && !a.off && a.all.includes("连接地址") && a.all.includes("添加服务器"), "运行中的地址：" + JSON.stringify(a));
        await clip();
        await byName("button", "复制连接地址").click(); await settle(150);
        assert(await copied() === "192.168.1.23:25565" && await toastHas("已复制连接地址"), "复制的内容：" + await copied());
        // 本机有几个 IP：换一个，记在本机，卡片也跟着用
        await page.click("#d-addr-ip"); await page.waitForSelector(".dropmenu");
        const items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent.trim()));
        assert(JSON.stringify(items) === JSON.stringify(["192.168.1.23", "10.0.0.5", "172.22.48.1"]), "IP 的排序：" + items);
        await page.click('.dropmenu button:has-text("10.0.0.5")'); await settle(200);
        assert(await page.textContent("#d-addr-v") === "10.0.0.5:25565", "换了 IP：" + await page.textContent("#d-addr-v"));
        await page.goto(url("#/instances")); await settle(600); await clip();
        await byName("button", "复制连接地址 " + SURV).click(); await settle(150);
        assert(await copied() === "10.0.0.5:25565", "卡片应用换过的 IP：" + await copied());
        await forgetIP();
        // 在「常用设置」里改了端口：轮询到新端口，地址跟着变
        await detail(SURV);
        await pState({ inst: { name: SURV, port: 25599 } });
        await page.waitForFunction(() => document.querySelector("#d-addr-v").textContent === "192.168.1.23:25599", null, { timeout: 5000 });
        // 停着的：灰字，写明开服后朋友连这个
        await detail(PACK0);
        a = await page.evaluate(() => ({ v: $("#d-addr-v").textContent, off: $("#d-addr").classList.contains("off"), note: $("#d-addr .dh-addr-note").textContent }));
        assert(a.v === "192.168.1.23:25566" && a.off && a.note === "开服后朋友连这个地址", "停着的：" + JSON.stringify(a));
        // 代理端：端口是它自己的；配置文件还没生成（connectPort 为 0）时说清楚，不给一个猜的地址
        await detail(PROXY);
        assert(await page.textContent("#d-addr-v") === "192.168.1.23:25577", "代理端：" + await page.textContent("#d-addr-v"));
        await pState({ inst: { name: PROXY, connectPort: 0 } });
        await detail(PROXY);
        assert(!(await page.$("#d-addr-v")) && (await page.textContent("#d-addr")).includes("第一次启动"), "代理端没生成配置时：" + await page.textContent("#d-addr"));
        // /api/app 拿不到本机 IP：详情页照常打开，地址那一行说没查到、不给复制
        const noIPs = route => route.fetch().then(async r => { const j = await r.json(); j.data.ips = []; return route.fulfill({ response: r, json: j }); });
        await page.route("**/api/app", noIPs);
        try {
          await detail(PACK0);
          assert(!(await page.$("#d-addr-copy")) && (await page.textContent("#d-addr")).includes("没查到这台电脑的局域网 IP"), "没有 IP 时：" + await page.textContent("#d-addr"));
        } finally { await page.unroute("**/api/app", noIPs); }
      } finally { await forgetIP(); await pState({ instReset: true }); }
    });

    await run("beta.3 P0 R01：列表卡片的端口旁能复制连接地址（名字带服务器名，点了不进详情页）；设置里叫「手机远程管理」，页顶指路到服务器页", async () => {
      await pState({ instReset: true });
      await forgetIP();
      await page.goto(url("#/instances")); await settle(600); await clip();
      for (const [n, addr] of [[SURV, "192.168.1.23:25565"], [PACK0, "192.168.1.23:25566"], [PROXY, "192.168.1.23:25577"]]) {
        const b = byName("button", "复制连接地址 " + n);
        assert(await b.count() === 1, "卡片「" + n + "」没有复制连接地址的按钮");
        await b.click(); await settle(150);
        assert(await copied() === addr, n + " 复制的内容：" + await copied());
      }
      assert(page.url().includes("#/instances"), "点复制不该进详情页：" + page.url());
      await page.goto(url("#/settings/remote")); await settle(500);
      const s = await page.evaluate(() => ({ nav: $('.set-nav[data-sec="remote"]').textContent.trim(), h: $(".set-sec-head h2").textContent, note: $("#lan-not-game").textContent,
        sw: $("#lan-on").getAttribute("aria-label"), href: $("#lan-not-game a").getAttribute("href") }));
      assert(s.nav === "手机远程管理" && s.h === "手机远程管理" && s.sw === "开启手机远程管理", "名字：" + JSON.stringify(s));
      assert(s.note.includes("让朋友进游戏的地址在服务器页上") && s.note.includes("不在这里") && s.href === "#/instances", "页顶的指路：" + JSON.stringify(s));
    });

    await run("beta.3 P0 R02：新建向导的基本设置里有白名单开关，默认关并写明关着谁都能进；请求按开关带上 whitelist", async () => {
      await pState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
      try {
        for (const on of [false, true]) {
          await page.goto(url("#/create")); await settle();
          await page.click("#mode-new"); await settle();
          await page.dblclick('.core-card[data-id="paper"]'); await settle();
          await page.click("#wnext"); await settle();
          await page.click("#wnext"); await settle();
          await page.click("#wnext"); await settle();
          const f = await page.evaluate(() => ({ checked: $("#f-wl").checked, text: $(".wl-field").textContent, hint: [...document.querySelectorAll("#wiz-body .hint")].map(h => h.textContent).join("|") }));
          assert(!f.checked && f.text.includes("只允许白名单里的玩家进入") && f.text.includes("关着时任何知道地址的人都能进") && f.text.includes("再到「玩家」里加名字"), "白名单开关：" + JSON.stringify(f));
          assert(!f.hint.includes("白名单等高级参数"), "不该再说白名单要创建后再调：" + f.hint);
          if (on) await page.check("#f-wl");
          await page.fill("#f-name", "白名单" + on);
          await page.click(".eula-box .switch input");
          await page.click("#wcreate"); await settle(700);
          const st = await pState({});
          assert(st.lastCreate && st.lastCreate.name === "白名单" + on && st.lastCreate.whitelist === on, "新建的请求：" + JSON.stringify(st.lastCreate));
        }
        // 用已有地图开服：走的是同一个向导，基本设置里也是这个开关，创建请求同样带上（后端放好地图以后再按它写 white-list）
        await wReset();
        await page.goto(url("#/create")); await page.waitForSelector("#mode-world");
        await page.click("#mode-world"); await page.waitForSelector("#wi-browse");
        await page.click("#wi-browse"); await page.waitForSelector("#wi-wiz-chosen");
        await page.waitForSelector(".core-card.sel");
        await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        await page.waitForSelector("#wi-wiz-plan #wi-rel");
        const w = await page.evaluate(() => ({ checked: $("#f-wl").checked, text: $(".wl-field").textContent }));
        assert(!w.checked && w.text.includes("关着时任何知道地址的人都能进"), "用已有地图开服的基本设置里也该有白名单开关：" + JSON.stringify(w));
        await page.check("#f-wl");
        await page.click("#f-eula");
        await page.click("#wcreate");
        await page.waitForFunction(() => location.hash.startsWith("#/task/"));
        await settle(500);
        const st = await pState({});
        assert(st.lastCreate && st.lastCreate.worldPlan && st.lastCreate.whitelist === true, "用已有地图开服的请求：" + JSON.stringify(st.lastCreate));
      } finally { await pState({ instReset: true, taskStage: 2, world: { reset: true } }); }
    });

    await run("beta.3 P0 R02：导入整合包的表单有白名单开关（默认关，写明整合包自带配置时以它为准），提交时带上；接管表单写出原来的白名单设置（只显示，代理端不显示）", async () => {
      await pState({ instReset: true });
      try {
        await page.goto(url("#/create/import")); await settle(600);
        const f = await page.evaluate(() => ({ checked: $("#im-wl").checked, text: $(".wl-field").textContent }));
        assert(!f.checked && f.text.includes("关着时任何知道地址的人都能进") && f.text.includes("整合包自带 server.properties 的，白名单也以这里为准"), "导入表单：" + JSON.stringify(f));
        await page.setInputFiles("#im-file", { name: "pack.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\x05\x06" + "\0".repeat(18), "latin1") });
        await page.check("#im-wl");
        await page.click("#im-eula");
        await page.click("#im-go"); await settle(800);
        let st = await pState({});
        assert(st.lastImport && st.lastImport.whitelist === "true" && st.lastImport.eula === "true", "导入的表单项：" + JSON.stringify(st.lastImport));
        // 接管：识别出来的白名单开关原样写出来（开 / 关 / 没写）
        const scan = { dir: "D:\\old\\S1", name: "S1", core: "paper", mc: "1.21.4", build: "", launchTarget: "jar:paper.jar",
          targets: [{ target: "jar:paper.jar", label: "paper.jar（Paper 1.21.4）", core: "paper", mc: "1.21.4", build: "", javaMajor: 0 }],
          javaMajor: 0, xmxMb: 0, extraJvm: [], port: 25565, eula: true, hasWorld: true, managed: false, notes: [] };
        for (const [w, want] of [[true, "开（沿用原来的设置）"], [false, "关（沿用原来的设置）"], [null, "没写这一项"]]) {
          await pState({ adoptScan: { ...scan, whitelistOn: w } });
          await page.goto(url("#/create/adopt")); await settle(400);
          await page.fill("#ad-dir", "D:\\old\\S1"); await page.keyboard.press("Enter"); await page.waitForSelector("#ad-wl");
          const t = await page.textContent("#ad-wl");
          assert(t.includes(want) && !(await page.$eval("#ad-wl", e => e.hidden)), `白名单 ${w}：` + t);
        }
        await page.selectOption("#ad-core", "velocity");
        assert(await page.$eval("#ad-wl", e => e.hidden), "代理端没有白名单这一项，不该显示");
      } finally { await pState({ adoptScan: null, instReset: true }); }
    });

    await run("beta.3 P0 R02：玩家标签的白名单块直接显示并能切换总开关（停着只改配置，开着再发 whitelist on/off），不再说要去常用设置打开；常用设置里那行黄字改成说明 MineDeck 新建时的做法", async () => {
      await pState({ instReset: true });
      try {
        await detail(PACK0, "players");
        let p = await page.evaluate(() => ({ on: $("#pl-wl-on").checked, desc: $("#pl-wl-desc").textContent, body: $("#tab-body").textContent }));
        assert(!p.on && p.desc.includes("任何知道地址的人都能进") && !p.body.includes("要在「常用设置」里打开白名单"), "停着、白名单关：" + JSON.stringify(p));
        let st = await pState({ commands: [] });
        await page.click("#pl-wl-on"); await settle(500);
        st = await pState({});
        assert(JSON.stringify(st.lastPropsPut) === JSON.stringify({ inst: PACK0, pairs: [{ key: "white-list", value: "true" }] }) && !st.commands.length, "停着时只写配置：" + JSON.stringify([st.lastPropsPut, st.commands]));
        assert(await page.$eval("#pl-wl-on", e => e.checked) && await toastHas("已打开白名单"), "打开以后开关应是开的");
        // 开着的服务器：写配置以后再发 whitelist on，马上生效
        await detail(SURV, "players");
        await page.click("#pl-wl-on"); await settle(500);
        st = await pState({});
        assert(st.lastPropsPut.inst === SURV && st.lastPropsPut.pairs[0].value === "true" && JSON.stringify(st.commands) === JSON.stringify([{ inst: SURV, cmd: "whitelist on" }]), "运行中：" + JSON.stringify([st.lastPropsPut, st.commands]));
        await page.click("#pl-wl-on"); await settle(500);
        st = await pState({});
        assert(st.commands[1].cmd === "whitelist off" && await toastHas("已关闭白名单"), "再关掉：" + JSON.stringify(st.commands));
        // server.properties 里没写这一项：按服务端的默认值显示，并写明（26.3 起默认开）
        await pState({ inst: { name: PACK0, whitelistOn: null, mc: "26.3" } });
        await detail(PACK0, "players");
        p = await page.evaluate(() => ({ on: $("#pl-wl-on").checked, desc: $("#pl-wl-desc").textContent }));
        assert(p.on && p.desc.includes("还没写这一项"), "没写这一项时：" + JSON.stringify(p));
        // 常用设置：不再是「注意：此版本起白名单默认开启」，而是说明 MineDeck 新建时怎么写
        await detail(PACK0, "common");
        const row = await page.$eval('.prop-item:has([data-k="white-list"])', e => e.textContent);
        assert(row.includes("MineDeck 新建服务器时按向导里的开关写这一项，默认关") && row.includes("服务端自己生成配置时默认是开") && !row.includes("此版本起白名单默认开启"), "常用设置的说明：" + row);
      } finally { await pState({ instReset: true }); }
    });

    await run("beta.3 P0 R03：启动没起来时原因常驻在卡片和详情页头的提示区（#d-notices），控制台有那一行；下次启动成功就消失；模组要 Java 的提示也在提示区", async () => {
      const why = "端口 25566 被别的程序占着，换个端口或关掉占用它的程序";
      await resetWith({ name: PACK0, startFail: why });
      try {
        await page.goto(url("#/instances")); await settle(500);
        await page.click(`.card[data-name="${PACK0}"] [data-act="start"]`); await settle(600);
        assert(await toastHas("无法启动服务器：" + why), "右上角应提示原因");
        await settle(3500); // 过了一轮轮询，卡片上还在
        const card = await page.$eval(`.card[data-name="${PACK0}"] .inst-fail`, e => e.textContent).catch(() => "");
        assert(card.includes("上次没能启动：" + why), "卡片上应常驻原因：" + card);
        await detail(PACK0, "players");
        const n = await page.evaluate(() => ({ hidden: $("#d-notices").hidden, text: $("#d-start-fail").textContent, link: $("#d-start-fail a").getAttribute("href"),
          below: $("#d-notices").getBoundingClientRect().top >= $(".detail-head").getBoundingClientRect().bottom }));
        assert(!n.hidden && n.text.includes("上次没能启动：" + why) && n.link.endsWith("/console") && n.below, "页头下面的提示区：" + JSON.stringify(n));
        await page.click("#d-start-fail a"); await page.waitForFunction(w => [...document.querySelectorAll("#console div")].some(d => d.textContent === "[MineDeck] 没能启动：" + w), why, { timeout: 4000 });
        // 端口放开了：再启动成功，提示区和卡片上的原因都没了
        await pState({ inst: { name: PACK0, startFail: "" } });
        await page.click("#d-start"); await settle(700);
        assert(!(await page.$("#d-start-fail")) && await page.$eval("#d-notices", e => e.hidden), "启动成功后提示应消失");
        await page.goto(url("#/instances")); await settle(500);
        assert(!(await page.$(`.card[data-name="${PACK0}"] .inst-fail`)), "卡片上的原因应消失");
        // 模组要求更高的 Java：也放在提示区里，和启动失败各自独立
        await pState({ modsNeed: { [PACK0]: 25 }, inst: { name: PACK0, startFail: why } });
        await detail(PACK0, "console");
        await page.click("#d-start"); await settle(700);
        const both = await page.evaluate(() => [...document.querySelectorAll("#d-notices > .d-notice")].map(e => e.id));
        assert(JSON.stringify(both.slice().sort()) === JSON.stringify(["d-java-hint", "d-start-fail"]), "提示区里应各有一条：" + both);
        assert(!(await page.$("#d-start-fail a")), "已经在控制台上就不用「看控制台」");
      } finally { await pState({ instReset: true, modsNeed: {} }); }
    });

    await run("beta.3 P0 R03：报错提示至少留 20 秒（不再 8 秒），鼠标停在上面、或者它的「详情」开着时不走；详情页上提示落在页头下面，不盖住启动、停止按钮", async () => {
      await resetWith({ name: PACK0, startFail: "端口 25566 被别的程序占着，换个端口或关掉占用它的程序" });
      const c = await hostCtx({ host: true });
      try {
        const p = await c.newPage();
        p.on("pageerror", e => errors.push(pageErr(e)));
        await p.clock.install();
        await p.goto(ORIGIN + "#/inst/" + encodeURIComponent(PACK0) + "/console"); await p.waitForSelector("#d-start");
        const live = () => p.$$eval(".toast.error:not(.leave)", ts => ts.length);
        const fail = async () => { await p.click("#d-start"); await p.waitForSelector(".toast.error:not(.leave)"); };
        await fail();
        // 不盖住页头的启动、停止、打开文件夹
        const geo = await p.evaluate(() => {
          const t = document.querySelector(".toast.error").getBoundingClientRect();
          const hit = ["#d-start", "#d-stop", "#d-dir"].filter(s => { const b = document.querySelector(s).getBoundingClientRect(); return !(t.right <= b.left || t.left >= b.right || t.bottom <= b.top || t.top >= b.bottom); });
          return { hit, top: t.top, head: document.querySelector(".detail-head").getBoundingClientRect().bottom };
        });
        assert(!geo.hit.length && geo.top >= geo.head, "提示盖住了页头的按钮：" + JSON.stringify(geo));
        await p.clock.runFor(9000);
        assert(await live() === 1, "9 秒后报错提示还应在（以前 8 秒就没了）");
        await p.clock.runFor(12000);
        assert(await live() === 0, "20 秒以后应自己走");
        // 鼠标停在上面：一直留着；移开以后接着倒数
        await fail();
        await p.hover(".toast.error");
        await p.clock.runFor(30000);
        assert(await live() === 1, "鼠标停在上面时不该走");
        await p.mouse.move(20, 700);
        await p.clock.runFor(21000);
        assert(await live() === 0, "移开以后应接着倒数、到点就走");
        // 「详情」开着：一直留着；关掉详情以后接着倒数
        await fail();
        await p.click('.toast.error button[aria-label="查看报错详情"]'); await p.waitForSelector(".err-detail");
        await p.mouse.move(20, 700);
        await p.clock.runFor(30000);
        assert(await live() === 1, "详情开着时提示不该走");
        await p.click('.modal [data-x="close"]');
        await p.mouse.move(20, 700);
        await p.clock.runFor(21000);
        assert(await live() === 0, "关掉详情以后应接着倒数");
      } finally { await c.close(); await pState({ instReset: true }); }
    });

    await run("开服失败的报错提示落在页头提示区下面，不盖住提示区里的修复按钮；提示和「详情」里都有去修的按钮（端口被占 → 改端口，找不到 Java → 去换 Java），别的原因不给", async () => {
      const why = "端口 25566 被别的程序占着，换个端口或关掉占用它的程序";
      await resetWith({ name: PACK0, startFail: why, startPortBusyFail: { port: 25566 } });
      const live = ".toast.error:not(.leave)", vp0 = page.viewportSize();
      try {
        await page.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
        await detail(PACK0, "console");
        await page.click("#d-start"); await page.waitForSelector(live); await settle(400);
        // 提示区（黄条）里的「改端口」「复制报错信息」不被右上角的提示盖住（回访走查 11-16：以前只躲页头）
        const geo = await page.evaluate(sel => {
          const t = document.querySelector(sel).getBoundingClientRect(), n = document.querySelector("#d-notices").getBoundingClientRect();
          const hit = [...document.querySelectorAll("#d-notices button, #d-notices a")].filter(b => { const r = b.getBoundingClientRect(); return !(t.right <= r.left || t.left >= r.right || t.bottom <= r.top || t.top >= r.bottom); }).map(b => b.textContent);
          return { hit, top: t.top, notes: n.bottom, edit: !!document.querySelector("#d-notices [data-port-edit]") };
        }, live);
        assert(geo.edit && !geo.hit.length && geo.top >= geo.notes, "提示盖住了提示区：" + JSON.stringify(geo));
        // 提示上有「改端口」；「详情」里也有一份，点了关掉详情、去「设置 → 常用」的端口那一项，提示也收掉
        assert(await page.$(`${live} button[aria-label="去「${PACK0}」的「设置 → 常用」改端口"]`), "提示上应有改端口");
        await page.click(`${live} button[aria-label="查看报错详情"]`); await page.waitForSelector(".err-detail");
        const btns = await page.$$eval("#modal-root .modal .m-actions button", bs => bs.map(b => b.textContent));
        assert(JSON.stringify(btns) === JSON.stringify(["改端口", "复制报错信息", "关闭"]), "详情里的按钮：" + JSON.stringify(btns));
        await page.click('#modal-root .modal [data-fix="0"]'); await page.waitForSelector('[data-k="server-port"]'); await settle(500);
        const p = await page.evaluate(sel => ({ h: decodeURIComponent(location.hash), focus: document.activeElement && document.activeElement.dataset.k,
          modal: !!document.querySelector("#modal-root .err-detail"), toasts: document.querySelectorAll(sel).length }), live);
        assert(p.h === `#/inst/${PACK0}/settings/common` && p.focus === "server-port" && !p.modal && !p.toasts, "点了以后：" + JSON.stringify(p));
        // 找不到配的 Java：在列表的卡片上点启动，提示上是「去换 Java」，点了去「设置 → 启动与内存」
        await pState({ inst: { name: PACK0, startFail: "找不到这台服务器配的 Java", startPortBusyFail: null, javaMissing: true } });
        await page.goto(url("#/instances")); await settle(500);
        await page.click(`.card[data-name="${PACK0}"] [data-act="start"]`); await page.waitForSelector(live);
        await page.click(`${live} button:text-is("去换 Java")`); await settle(500);
        assert(decodeURIComponent(await page.evaluate(() => location.hash)) === `#/inst/${PACK0}/settings/jvm` && !(await page.$(live)), "应去启动与内存、提示收掉");
        // 别的原因：不给修复按钮，详情里只有复制和关闭
        await pState({ inst: { name: PACK0, startFail: "服务端自己退出了", javaMissing: false } });
        await detail(PACK0, "console");
        await page.click("#d-start"); await page.waitForSelector(live); await settle(300);
        assert((await page.$$eval(`${live} button`, bs => bs.map(b => b.textContent))).join("|") === "详情|", "别的原因不给修复按钮");
        await page.click(`${live} button[aria-label="查看报错详情"]`); await page.waitForSelector(".err-detail");
        assert(!(await page.$("#modal-root .modal [data-fix]")), "详情里也不给");
        await page.click('#modal-root .modal [data-x="close"]'); await settle(300);
        // 矮窗口：提示区下面放不下时退回页头下面，还放不下就回到标题栏下方——整条提示总在屏幕里（Codex 复审 beta.3 之后第一轮：
        // 以前会被推到屏幕外面照样倒数、自己走掉）
        // 报错原文很长时提示加上按钮以后会换行变高：要按加完按钮的高度量（Codex 复审 beta.3 之后第二轮）
        // 原文要在 60 个字以内、提示才会整句接上（见 errSummary），够长才会因为加了按钮多换几行
        await pState({ inst: { name: PACK0, startFail: "模组加载失败缺少前置模组请先检查模组列表和版本是否匹配然后再重新启动一次这台服务器看看还会不会这样" } });
        // 先在大窗口里量：带按钮的报错提示多高（h1）、同样的字不带按钮多高（h0，普通提示没有按钮）、提示区底在哪；
        // 再挑一个「按 h0 放得下、按 h1 放不下」的窗口高度。按 h0 量的老代码会把它放到提示区下面、下半截出屏幕
        await page.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
        await detail(PACK0, "console");
        await page.click("#d-start"); await page.waitForSelector(live); await settle(400);
        const m = await page.evaluate(sel => {
          const e = document.querySelector(sel), h1 = e.getBoundingClientRect().height, text = e.querySelector(".t-msg").textContent;
          const plain = toast(text); const h0 = plain.getBoundingClientRect().height; plain.remove();
          return { h0, h1, nb: document.querySelector("#d-notices").getBoundingClientRect().bottom };
        }, live);
        assert(m.h1 > m.h0, "加了按钮应更高（不然这段测不到要测的）：" + JSON.stringify(m));
        const tight = Math.ceil(m.nb + 8 + m.h0 + 8 + (m.h1 - m.h0) / 2);
        for (const hgt of [tight, 360, 280]) {
          await page.setViewportSize({ width: vp0.width, height: hgt });
          await page.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove()));
          await detail(PACK0, "console");
          await page.click("#d-start"); await page.waitForSelector(live); await settle(400);
          const g = await page.evaluate(sel => { const t = document.querySelector(sel).getBoundingClientRect(); return { top: t.top, bottom: t.bottom, vh: innerHeight,
            notes: document.querySelector("#d-notices").getBoundingClientRect().bottom }; }, live);
          assert(g.top >= 0 && g.bottom <= g.vh, `高 ${hgt} 的窗口里提示应整条看得见：` + JSON.stringify(g));
        }
      } finally {
        await page.setViewportSize(vp0);
        await page.evaluate(() => document.querySelectorAll("#toasts .toast").forEach(t => t.remove())); await pState({ instReset: true });
      }
    });

    await run("beta.3 P0 R04：交接弹窗里报错后点「详情」，报错信息叠在上面；Esc 和「关闭」只关上面那层，交接弹窗和填的路径都在；点遮罩不关；为什么不可用写在弹窗里", async () => {
      await pState({ instReset: true });
      const layers = () => page.$$eval("#modal-root > .modal-mask:not(.leave)", ms => ms.map(m => m.id || (m.querySelector(".err-detail") ? "detail" : "other")));
      await page.goto(url("#/instances")); await settle();
      await page.click(`.card[data-name="${PACK0}"] [data-act="more"]`);
      await page.click('.dropmenu button:has-text("导出交接包")'); await page.waitForSelector("#tr-mask");
      await page.fill("#tr-dest", "Z:\\nope");
      await page.click("#tr-go"); await settle(400);
      const why = await page.textContent("#tr-dest-err");
      // beta.3 R20 起找不到的文件夹可以「新建并导出」（以前这里断言「不会替你新建」）
      assert(why.includes("导出文件夹不可用") && why.includes("找不到") && why.includes("新建并导出") && why.includes("浏览"), "为什么不可用：" + why);
      // 提示还在时点「详情」：报错信息叠在交接弹窗上面
      await page.click('.toast.error button[aria-label="查看报错详情"]'); await page.waitForSelector(".err-detail");
      assert(JSON.stringify(await layers()) === JSON.stringify(["tr-mask", "detail"]), "叠层：" + JSON.stringify(await layers()));
      assert((await page.textContent(".err-detail")).includes("Z:\\nope"), "详情里是原文");
      await page.keyboard.press("Escape"); await settle(300);
      assert(JSON.stringify(await layers()) === JSON.stringify(["tr-mask"]) && await page.inputValue("#tr-dest") === "Z:\\nope", "Esc 只该关上面那层：" + JSON.stringify(await layers()));
      await page.click('.toast.error button[aria-label="查看报错详情"]'); await page.waitForSelector(".err-detail");
      await page.click('.modal [data-x="close"]:visible'); await settle(300);
      assert(JSON.stringify(await layers()) === JSON.stringify(["tr-mask"]) && await page.inputValue("#tr-dest") === "Z:\\nope", "「关闭」只该关上面那层");
      // 点遮罩（以前提示消失后点到的就是这里）：有输入的弹窗不关
      await page.mouse.click(20, 820); await settle(300);
      assert(await page.isVisible("#tr-mask") && await page.inputValue("#tr-dest") === "Z:\\nope", "点遮罩不该关掉有输入的弹窗");
      // 别的原因：写在弹窗里（不只靠右上角的提示），「详情」叠在上面看原文
      const busy = route => route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ ok: false, error: "交接前请先停止服务器（运行中打包会不完整）" }) });
      await page.route("**/transfer/export", busy);
      try {
        await page.fill("#tr-dest", "");
        await page.click("#tr-go"); await settle(400);
        const err = await page.evaluate(() => ({ hidden: $("#tr-err").hidden, text: $("#tr-err").textContent }));
        assert(!err.hidden && err.text.includes("无法导出交接包：交接前请先停止服务器"), "弹窗里的报错：" + JSON.stringify(err));
        await page.click("#tr-err-more"); await page.waitForSelector(".err-detail");
        assert(JSON.stringify(await layers()) === JSON.stringify(["tr-mask", "detail"]), "弹窗里的「详情」也叠在上面");
        await page.keyboard.press("Escape"); await settle(300);
      } finally { await page.unroute("**/transfer/export", busy); }
      // 确认框同样叠在上面：删除框里打开开关、输了名字，看一眼报错详情再回来，开关和输的名字都在
      await page.click("#tr-cancel"); await settle(300);
      await openDelete(PACK0);
      await page.check("#modal-root input[data-k=files]");
      await page.fill("#modal-root input[data-k=confirm-name]", PACK0);
      await page.evaluate(() => { showErrDetail("测试用的原文"); }); // 不能把它返回的 Promise 交回去：它要等详情关掉才落定
      await page.waitForSelector(".err-detail");
      await page.keyboard.press("Escape"); await settle(300);
      const d = await page.evaluate(() => ({ n: document.querySelectorAll("#modal-root > .modal-mask:not(.leave)").length, files: $("#modal-root input[data-k=files]").checked, typed: $("#modal-root input[data-k=confirm-name]").value }));
      assert(d.n === 1 && d.files && d.typed === "机械动力整合包", "删除框应还在、填的都在：" + JSON.stringify(d));
      await page.keyboard.press("Escape"); await settle(300);
      assert(!(await page.$("#modal-root > .modal-mask:not(.leave)")), "再按 Esc 关掉删除框");
    });

    await run("后台任务失败带 errHint 报错提示：任务页失败框紧跟显示中文排查建议，不带 errHint 时照旧（A85）", async () => {
      try {
        await pState({ taskList: [
          { id: "task-hint-yes", title: "创建 Paper 服务器", ended: true, error: "写入 core.jar 失败：permission denied", errHint: "没有读写权限，请检查文件夹权限或关闭占用它的程序", steps: [{ name: "下载核心", status: "error" }], warnings: [], log: [] },
          { id: "task-hint-no", title: "创建 Paper 服务器", ended: true, error: "找不到启动入口", steps: [{ name: "安装服务端", status: "error" }], warnings: [], log: [] },
        ] });
        await page.goto(url("#/task/task-hint-yes"));
        await page.waitForSelector("#t-msgs .err-box");
        const box1 = await page.textContent("#t-msgs .err-box");
        assert(box1.includes("写入 core.jar 失败") && box1.includes("没有读写权限，请检查文件夹权限或关闭占用它的程序"),
          "带 errHint 的失败框应写出建议：" + box1);
        await page.goto(url("#/task/task-hint-no"));
        await page.waitForSelector("#t-msgs .err-box");
        const box2 = await page.textContent("#t-msgs .err-box");
        assert(box2.includes("找不到启动入口") && !box2.includes("undefined"),
          "不带 errHint 时不应显示 undefined：" + box2);
      } finally { await pState({ taskList: [] }); }
    });

    await run("beta.3 P0 R05/U28/U30：删除默认只移出列表；打开开关后按钮写「删除服务器和文件夹」、输对名字才能点；移出后提示写出文件夹、带「打开文件夹」；接管来的写出会留下哪些 MineDeck 的文件", async () => {
      await pState({ instReset: true, local: true });
      try {
        await openDelete(PACK0);
        const m = () => page.evaluate(() => ({ checked: $("#modal-root input[data-k=files]").checked, ok: $("#modal-root [data-x=ok]").textContent, okOff: $("#modal-root [data-x=ok]").disabled,
          cancel: $("#modal-root [data-x=cancel]").textContent, title: $("#modal-root h3").textContent.trim(), confirmHidden: $("#modal-root .del-confirm").hidden, body: $("#modal-root .m-body").textContent }));
        let s = await m();
        assert(!s.checked && s.ok === "移出列表" && !s.okOff && s.cancel === "取消" && s.title === "把「机械动力整合包」移出列表？" && s.confirmHidden, "默认：" + JSON.stringify(s));
        assert(s.body.includes("D:\\MineDeck\\servers\\create-pack") && s.body.includes("接管已有服务器"), "正文写出文件夹：" + s.body);
        await page.check("#modal-root input[data-k=files]");
        s = await m();
        assert(s.ok === "删除服务器和文件夹" && s.okOff && !s.confirmHidden && s.title === "删除「机械动力整合包」和它的文件夹？", "打开开关以后：" + JSON.stringify(s));
        await page.fill("#modal-root input[data-k=confirm-name]", "机械动力");
        assert((await m()).okOff, "名字没对上不能点");
        await page.fill("#modal-root input[data-k=confirm-name]", PACK0);
        assert(!(await m()).okOff, "名字对上了才能点");
        await page.uncheck("#modal-root input[data-k=files]");
        s = await m();
        assert(s.ok === "移出列表" && !s.okOff && s.confirmHidden, "关掉开关又回到移出列表：" + JSON.stringify(s));
        // 点遮罩不关（框里有开关和输入框）
        await page.mouse.click(20, 820); await settle(250);
        assert(await page.$("#modal-root input[data-k=files]"), "点遮罩不该关掉删除框");
        // 真的删：打开开关、输对名字
        await page.check("#modal-root input[data-k=files]");
        await page.fill("#modal-root input[data-k=confirm-name]", PACK0);
        await page.press("#modal-root input[data-k=confirm-name]", "Enter"); await settle(500);
        let st = await pState({});
        assert(st.lastDelete && st.lastDelete.inst === PACK0 && st.lastDelete.files === true && await toastHas("已删除「机械动力整合包」和它的文件夹"), "删除的请求：" + JSON.stringify(st.lastDelete));
        // 接管来的：写出原来的文件夹和会留下的 MineDeck 文件；移出以后提示写出文件夹、能打开
        await resetWith({ name: PACK0, adopted: true, dir: "E:\\MCServers\\create-pack", mdFiles: ["run.bat", "run.before-minedeck.bat", "user_jvm_args.txt", "user_jvm_args.before-minedeck.txt"] });
        await openDelete(PACK0);
        const left = await page.textContent("#modal-root .del-left");
        for (const f of ["instance.json.bak", "run.bat", "run.before-minedeck.bat", "user_jvm_args.txt", "user_jvm_args.before-minedeck.txt"]) assert(left.includes(f), "会留下的文件里应有 " + f + "：" + left);
        assert(left.includes("想换回去") && !(await page.$eval("#modal-root input[data-k=files]", e => e.checked)), "说明：" + left);
        await page.click('#modal-root [data-x="ok"]'); await settle(600);
        st = await pState({});
        assert(st.lastDelete.files === false, "接管来的默认只移出列表：" + JSON.stringify(st.lastDelete));
        const t = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
        assert(t.some(x => x.includes("已把「机械动力整合包」移出列表") && x.includes("文件夹还在：E:\\MCServers\\create-pack")), "提示：" + JSON.stringify(t));
        await byName("button", "打开「机械动力整合包」的文件夹").click(); await settle(300);
        st = await pState({});
        assert(st.lastOpenPath === "E:\\MCServers\\create-pack", "打开的文件夹：" + st.lastOpenPath);
        // 从局域网打开：打不开主机上的文件夹，提示里不给这个按钮
        await pState({ instReset: true, local: false });
        await openDelete(PACK0);
        await page.click('#modal-root [data-x="ok"]'); await settle(600);
        assert(await toastHas("文件夹还在") && await byName("button", "打开「机械动力整合包」的文件夹").count() === 0, "局域网打开时不该有打开文件夹");
      } finally { await pState({ instReset: true, local: true }); }
    });
  }
  // ==================== beta.3 P0 结束 ====================

  // ==================== beta.3 详情页（R06 R07 R10 R11 R15 R16 R21） ====================
  // docs/测试与评审/可用性走查-2026-11-14.md 第八节 R06、R07、R10、R11、R21，R15、R16 里属于详情页的部分；标签和子页的编号见 docs/文案重写/04-标签与术语-beta3.md 第一节。
  // 用例名都以「beta.3 详情」开头，node tests.js "beta.3 详情" 只跑这一块。仿真后端用到的字段见 server.js「/__mock/state」说明里「beta.3 详情页」那几行
  {
    const dState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const SURV = "生存服 · 朋友", PACK0 = "机械动力整合包", VAN = "原版快照测试", PROXY = "群组代理";
    const E = encodeURIComponent;
    const open = async (n, t) => { await page.goto(url(`#/inst/${E(n)}/${t}`)); await page.waitForSelector("#d-status"); await settle(400); };
    const hashNow = () => page.evaluate(() => decodeURIComponent(location.hash));
    const tabsNow = () => page.evaluate(() => ({ tabs: [...document.querySelectorAll(".tabs:not(.sub-tabs) > .tab")].map(a => a.dataset.t + ":" + a.textContent.trim()),
      cur: (document.querySelector(".tabs:not(.sub-tabs) > .tab.cur") || {}).dataset?.t || "", sub: (document.querySelector(".sub-tabs > .tab.cur") || {}).dataset?.t || "",
      subs: [...document.querySelectorAll(".sub-tabs > .tab")].map(a => a.dataset.t + ":" + a.textContent.trim()) }));

    await run("beta.3 详情 R06：七个标签和子页按术语文件排；插件服叫「插件」、模组服叫「模组」、Mohist 叫「插件和模组」，原版没有；代理端只有控制台、插件、定时任务、设置（只有启动与内存）", async () => {
      await dState({ instReset: true });
      try {
        await open(SURV, "console");
        let t = await tabsNow();
        assert(JSON.stringify(t.tabs) === JSON.stringify(["console:控制台", "players:玩家", "world:世界与备份", "addons:插件", "tasks:定时任务", "settings:设置", "ext:扩展"]) && t.cur === "console" && !t.subs.length,
          "Paper 的标签：" + JSON.stringify(t));
        await open(PACK0, "ext");
        t = await tabsNow();
        assert(t.tabs[3] === "addons:模组" && t.cur === "ext" && JSON.stringify(t.subs) === JSON.stringify(["gameai:游戏内 AI", "companion:AI 伙伴", "music:音乐共享"]) && t.sub === "gameai" &&
          await hashNow() === `#/inst/${PACK0}/ext/gameai`, "Forge 的扩展：" + JSON.stringify(t));
        await open(VAN, "settings");
        t = await tabsNow();
        assert(!t.tabs.some(x => x.startsWith("addons")) && t.cur === "settings" && t.sub === "common" && JSON.stringify(t.subs) === JSON.stringify(["common:常用", "jvm:启动与内存", "raw:全部配置"]) &&
          await hashNow() === `#/inst/${VAN}/settings/common`, "原版：" + JSON.stringify(t));
        await open(PROXY, "settings");
        t = await tabsNow();
        assert(JSON.stringify(t.tabs) === JSON.stringify(["console:控制台", "addons:插件", "tasks:定时任务", "settings:设置"]) && t.sub === "jvm" && JSON.stringify(t.subs) === JSON.stringify(["jvm:启动与内存"]) &&
          await hashNow() === `#/inst/${PROXY}/settings/jvm`, "代理端：" + JSON.stringify(t));
        await dState({ inst: { name: PACK0, core: "mohist" } });
        await open(PACK0, "addons");
        t = await tabsNow();
        assert(t.tabs[3] === "addons:插件和模组" && (await page.textContent("#ad-h")) === "已安装的模组" && (await page.textContent("#ad-note")).includes("plugins 文件夹里的插件没有列在这里"),
          "Mohist（标签叫插件和模组，已安装只列 mods 文件夹）：" + JSON.stringify(t) + await page.textContent("#ad-h"));
      } finally { await dState({ instReset: true }); }
    });

    await run("beta.3 详情 R06：每个旧地址都跳到对的新地方，地址原地换掉、不多一条历史记录（后退回到进来之前那一页）；用不上的落到能用的地方", async () => {
      await dState({ instReset: true });
      const cases = [[SURV, "backups", "world/backups"], [SURV, "res", "addons"], [SURV, "cart", "addons"], [SURV, "common", "settings/common"], [SURV, "raw", "settings/raw"],
        [SURV, "jvm", "settings/jvm"], [SURV, "gameai", "ext/gameai"], [PACK0, "companion", "ext/companion"], [PACK0, "music", "ext/music"],
        [SURV, "world", "world/backups"], // 旧的「地图」标签就叫 world：现在就是「世界与备份」，落到「备份」子页，旁边就是「换地图」
        [PROXY, "common", "settings/jvm"], [PROXY, "players", "console"], [VAN, "cart", "console"], [SURV, "music", "ext/gameai"], [SURV, "nope", "console"]];
      for (const [n, old, want] of cases) {
        await page.goto(url("#/instances")); await settle(300);
        await page.evaluate(h => { location.hash = h; }, `#/inst/${E(n)}/${old}`);
        await page.waitForFunction(w => decodeURIComponent(location.hash).endsWith("/" + w) && document.querySelector("#tab-body"), want, { timeout: 4000 }).catch(() => {});
        const h = await hashNow(), t = await tabsNow();
        const [wt, ws = ""] = want.split("/");
        assert(h === `#/inst/${n}/${want}` && t.cur === wt && t.sub === ws, `${n} 的旧地址 ${old}：落到 ${h}，标签 ${t.cur}/${t.sub}，应为 ${want}`);
        if (old === "world") assert(t.subs.includes("map:换地图"), "旧的地图地址落在世界与备份上，应看得到「换地图」：" + JSON.stringify(t.subs));
        await page.goBack(); await settle(300);
        assert(await hashNow() === "#/instances", `${old}：后退应回到列表，实际是 ${await hashNow()}`);
      }
      // 页面上的链接是新地址：页头提示区的「换 Java」
      await dState({ modsNeed: { [PACK0]: 25 } });
      try {
        await open(PACK0, "console");
        const href = await page.$eval("#d-java-hint a", a => decodeURIComponent(a.getAttribute("href"))).catch(() => "");
        assert(href === `#/inst/${PACK0}/settings/jvm` && (await page.textContent("#d-java-hint")).includes("「设置 → 启动与内存」"), "提示区的链接：" + href);
      } finally { await dState({ modsNeed: {} }); }
    });

    await run("beta.3 详情 R06/R07：页头有「重启」和「···」；重启调 restart，有玩家在线先确认（写出人数），没人在线直接重启；停着时重启点不了；「···」里是导出交接包和删除服务器（停着才能点，删除后回到列表）", async () => {
      await dState({ instReset: true, lifecycle: [] });
      try {
        await open(SURV, "console");
        const head = await page.evaluate(() => ({ restart: $("#d-restart").textContent.trim(), dis: $("#d-restart").disabled, more: $("#d-more").getAttribute("aria-label") }));
        assert(head.restart === "重启" && !head.dis && head.more === "更多操作 " + SURV, "页头：" + JSON.stringify(head));
        // 有 3 个人在线：先问，取消就不重启
        await page.click("#d-restart"); await page.waitForSelector("#modal-root .modal");
        let m = await page.evaluate(() => ({ t: $("#modal-root h3").textContent, b: $("#modal-root .m-body").textContent, ok: $('#modal-root [data-x="ok"]').textContent }));
        assert(m.t.includes("重启") && m.b.includes("3") && m.b.includes("掉线") && m.ok === "重启服务器", "确认框：" + JSON.stringify(m));
        await page.click('#modal-root [data-x="cancel"]'); await settle(300);
        let st = await dState({});
        assert(!(st.lifecycle || []).length, "取消了不该发请求：" + JSON.stringify(st.lifecycle));
        await page.click("#d-restart"); await page.waitForSelector("#modal-root .modal");
        await page.click('#modal-root [data-x="ok"]'); await settle(600);
        st = await dState({});
        assert(JSON.stringify(st.lifecycle) === JSON.stringify([{ inst: SURV, act: "restart" }]) && await toastHas("正在重启"), "重启的请求：" + JSON.stringify(st.lifecycle));
        // 停止也一样先问
        await page.click("#d-stop"); await page.waitForSelector("#modal-root .modal");
        m = await page.evaluate(() => ({ t: $("#modal-root h3").textContent, ok: $('#modal-root [data-x="ok"]').textContent }));
        assert(m.t.includes("停止") && m.ok === "停止服务器", "停止的确认框：" + JSON.stringify(m));
        await page.click('#modal-root [data-x="ok"]'); await settle(500);
        st = await dState({});
        assert(st.lifecycle.length === 2 && st.lifecycle[1].act === "stop", "停止的请求：" + JSON.stringify(st.lifecycle));
        // 没人在线：不问，直接重启
        await dState({ inst: { name: SURV, onlineCount: 0 }, lifecycle: [] });
        await open(SURV, "console");
        await page.click("#d-restart"); await settle(600);
        st = await dState({});
        assert(!(await page.$("#modal-root .modal")) && st.lifecycle.length === 1 && st.lifecycle[0].act === "restart", "没人在线时：" + JSON.stringify(st.lifecycle));
        // 运行中：「···」里两项都点不了，写明原因
        await page.click("#d-more"); await page.waitForSelector(".dropmenu");
        let items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent.trim() + (b.disabled ? "(灰)" : "")));
        assert(JSON.stringify(items) === JSON.stringify(["导出交接包…(灰)", "删除服务器…(灰)"]) && (await page.textContent(".dropmenu")).includes("需先停止服务器"), "运行中的菜单：" + items);
        await page.keyboard.press("Escape");
        // 停着的：重启点不了；「···」能导出、能删除，删了回到列表
        await open(PACK0, "settings");
        assert(await page.$eval("#d-restart", b => b.disabled), "停着时重启应点不了");
        await page.click("#d-more"); await page.waitForSelector(".dropmenu");
        items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent.trim() + (b.disabled ? "(灰)" : "")));
        assert(JSON.stringify(items) === JSON.stringify(["导出交接包…", "删除服务器…"]), "停着的菜单：" + items);
        await page.click('.dropmenu button:has-text("导出交接包")'); await page.waitForSelector("#modal-root .modal");
        assert(await page.$("#modal-root #tr-title"), "应弹出导出交接包的弹窗（和卡片「···」里是同一个）");
        await page.keyboard.press("Escape"); await settle(300);
        await page.click("#d-more"); await page.click(".dropmenu button.danger"); await page.waitForSelector("#modal-root input[data-k=files]"); await settle(200);
        await page.click('#modal-root [data-x="ok"]'); await settle(700);
        st = await dState({});
        assert(st.lastDelete && st.lastDelete.inst === PACK0 && !st.lastDelete.files && await hashNow() === "#/instances", "删除以后：" + JSON.stringify(st.lastDelete) + " " + await hashNow());
      } finally { await dState({ instReset: true }); }
    });

    await run("beta.3 详情 R07：常用设置每一项都标「重启生效」（游戏规则开关除外）；保存条按实时状态说，停着不写运行中；运行中保存后给「立即重启」，一步重启；列表卡片的停止也先问", async () => {
      await dState({ instReset: true, lifecycle: [] });
      try {
        const rows = await page.evaluate(() => (window.PROPS_CATALOG || []).filter(p => p.restart !== true).map(p => p.key));
        assert(!rows.length, "目录里还有不标重启的：" + rows);
        // 停着：保存条不写「运行中」
        await open(PACK0, "settings/common");
        assert(await page.$eval(".save-bar .sb-live", e => e.hidden) && await page.$eval('[data-sb="restart"]', b => b.hidden), "停着时不该写运行中");
        const motd = await page.$eval('.prop-item:has([data-k="motd"])', e => e.textContent);
        assert(motd.includes("重启生效"), "motd 应标重启生效：" + motd);
        // 开起来以后（轮询到状态）：保存条马上改口
        await dState({ status: { [PACK0]: "running" } });
        await page.waitForFunction(() => !document.querySelector(".save-bar .sb-live").hidden, null, { timeout: 5000 });
        assert((await page.textContent(".save-bar .sb-live")).includes("服务器运行中：保存后要重启才生效"), "运行中的说法：" + await page.textContent(".save-bar .sb-live"));
        // 改三项、保存 → 「立即重启」→ 没人在线就直接重启（等一轮轮询，页面拿到「没人在线」）
        await dState({ inst: { name: PACK0, onlineCount: 0 } }); await settle(2800);
        await page.click('.prop-item [data-k="hardcore"]');
        await page.fill('[data-k="motd"]', "改过的介绍");
        await page.selectOption('[data-k="difficulty"]', "hard");
        assert((await page.textContent(".sb-text")).includes("3 项修改未保存"), "保存条：" + await page.textContent(".sb-text"));
        await page.click('[data-sb="save"]'); await settle(500);
        assert(await toastHas("重启服务器以后生效") && !(await page.$eval('[data-sb="restart"]', b => b.hidden)) && (await page.textContent(".save-bar .sb-live")).includes("已保存，重启服务器以后生效"),
          "保存以后应给「立即重启」");
        await page.click('[data-sb="restart"]'); await settle(600);
        let st = await dState({});
        assert(JSON.stringify(st.lifecycle) === JSON.stringify([{ inst: PACK0, act: "restart" }]), "立即重启的请求：" + JSON.stringify(st.lifecycle));
        // 停过一次：下次启动就是新设置，「立即重启」不用了
        await dState({ status: { [PACK0]: "stopped" } });
        await page.waitForFunction(() => document.querySelector('[data-sb="restart"]').hidden && document.querySelector(".save-bar .sb-live").hidden, null, { timeout: 5000 });
        // 列表卡片的停止：有人在线先问
        await dState({ lifecycle: [] });
        await page.goto(url("#/instances")); await settle(500);
        await page.click(`.card[data-name="${SURV}"] [data-act="stop"]`); await page.waitForSelector("#modal-root .modal");
        assert((await page.textContent("#modal-root .m-body")).includes("3"), "卡片停止的确认框要写人数");
        await page.click('#modal-root [data-x="cancel"]'); await settle(300);
        st = await dState({});
        assert(!(st.lifecycle || []).length, "取消了不该停");
      } finally { await dState({ instReset: true }); }
    });

    await run("beta.3 详情 R10：插件页上面是已安装（停用的标出来），停着能停用、启用、删除（删除先确认、移进已删除文件夹）；开着时点不了并写明要先停服；热门和搜索里装过的标「已安装」", async () => {
      await dState({ instReset: true, modOps: [], modCart: { reset: true } });
      try {
        await open(SURV, "addons");
        const v = await page.evaluate(() => ({ h: $("#ad-h").textContent, count: $("#ad-count").textContent, rows: [...document.querySelectorAll(".ad-item")].map(r => r.dataset.file + (r.classList.contains("off") ? "(停用)" : "")),
          stop: !$("#ad-need-stop").hidden, dis: [...document.querySelectorAll("[data-ad-toggle], [data-ad-del]")].every(b => b.disabled), add: $(".ad-add-h").textContent }));
        assert(v.h === "已安装的插件" && v.count === "plugins 文件夹里共 3 个，1 个已停用" && v.add === "添加插件" && JSON.stringify(v.rows) === JSON.stringify(["EssentialsX-2.20.1.jar", "LuckPerms-Bukkit-5.4.137.jar", "Chunky-1.4.10.jar.disabled(停用)"]),
          "已安装列表：" + JSON.stringify(v));
        assert(v.stop && v.dis, "运行中应点不了并写明要先停服：" + JSON.stringify(v));
        // 热门清单里：LuckPerms 按模组 ID 认出来，EssentialsX 按文件内容在 Modrinth 上认出来（projects=1）；停用的 Chunky 不算已安装
        await page.waitForSelector("#mc-hits .mc-hit");
        await page.waitForFunction(() => [...document.querySelectorAll(".mc-add")].filter(b => b.textContent.includes("已安装")).length === 2, null, { timeout: 4000 }).catch(() => {});
        const marks = await page.$$eval(".mc-add", bs => bs.filter(b => b.textContent.includes("已安装")).map(b => b.dataset.pid + (b.disabled ? "" : "(可点)")));
        let st = await dState({});
        assert(JSON.stringify(marks.sort()) === JSON.stringify(["essentialsx", "luckperms"]) && st.modsProjects === 1, "热门里的已安装：" + JSON.stringify(marks) + " projects 请求 " + st.modsProjects);
        // 停服以后（轮询到）：马上能点
        await dState({ status: { [SURV]: "stopped" } });
        await page.waitForFunction(() => document.querySelector("#ad-need-stop").hidden && !document.querySelector('[data-ad-toggle="EssentialsX-2.20.1.jar"]').disabled, null, { timeout: 5000 });
        await byName("button", "停用 EssentialsX-2.20.1.jar").click(); await settle(500);
        st = await dState({});
        assert(JSON.stringify(st.modOps) === JSON.stringify([{ inst: SURV, act: "toggle", file: "EssentialsX-2.20.1.jar", enable: false }]) && await toastHas("已停用 EssentialsX-2.20.1.jar"), "停用的请求：" + JSON.stringify(st.modOps));
        assert(await page.$eval('[data-file="EssentialsX-2.20.1.jar.disabled"]', r => r.classList.contains("off")), "停用以后列表里标成停用");
        assert(await page.$eval('.mc-add[data-pid="essentialsx"]', b => !b.disabled && b.textContent.includes("加入清单")), "停用以后热门里不该再标已安装");
        await byName("button", "启用 Chunky-1.4.10.jar.disabled").click(); await settle(500);
        st = await dState({});
        assert(st.modOps[1].file === "Chunky-1.4.10.jar.disabled" && st.modOps[1].enable === true, "启用的请求：" + JSON.stringify(st.modOps[1]));
        // 删除：先确认（说清楚是移进已删除文件夹，能找回），取消不删
        await byName("button", "删除 LuckPerms-Bukkit-5.4.137.jar").click(); await page.waitForSelector("#modal-root .modal");
        const body = await page.textContent("#modal-root .m-body");
        assert(body.includes(".minedeck\\已删除\\plugins") && body.includes("不会直接删掉"), "删除的确认框：" + body);
        await page.click('#modal-root [data-x="cancel"]'); await settle(300);
        assert((await dState({})).modOps.length === 2, "取消了不该删");
        await byName("button", "删除 LuckPerms-Bukkit-5.4.137.jar").click(); await page.waitForSelector("#modal-root .modal");
        await page.click('#modal-root [data-x="ok"]'); await settle(600);
        st = await dState({});
        assert(st.modOps[2].act === "delete" && st.modOps[2].file === "LuckPerms-Bukkit-5.4.137.jar" && !(await page.$('[data-file="LuckPerms-Bukkit-5.4.137.jar"]')) &&
          await toastHas("移进了服务器文件夹里的 .minedeck\\已删除\\plugins\\LuckPerms-Bukkit-5.4.137.jar"), "删除以后：" + JSON.stringify(st.modOps[2]));
        // 模组服叫「模组」；原版打不开这一页
        await open(PACK0, "addons");
        assert((await page.textContent("#ad-h")) === "已安装的模组" && (await page.textContent(".mc-gh")).includes("待装清单"), "模组服的叫法");
      } finally { await dState({ instReset: true, modCart: { reset: true } }); }
    });

    await run("beta.3 详情 R11：运行中「还原」旁写明要先停服；确认框直接给「停止并还原」，一次确认就先停再还原；还原以后页顶写当前世界从哪份备份来；手动备份带标签，列表优先显示标签；说明里有还原的规则", async () => {
      await dState({ instReset: true, lifecycle: [] });
      try {
        await open(SURV, "world/backups");
        const v = await page.evaluate(() => ({ now: $("#bk-now").textContent, titles: [...document.querySelectorAll(".bk-list .ri-title")].map(e => e.textContent), need: document.querySelectorAll(".bk-need-stop").length,
          hint: $("#tab-body").textContent }));
        assert(v.now.includes("没有从备份还原过") && v.titles[1] === "打龙前" && v.titles[0] === "20261026-040000.000000000.zip" && v.need === 3, "备份页：" + JSON.stringify(v));
        assert(v.hint.includes("会先帮你保存世界、停服") && v.hint.includes("还原前自动") && (await page.textContent(".bk-need-stop")) === "会先停服", "说明里应有还原的规则，还原旁写会先停服");
        await byName("button", "还原 20261025-221000.000000000-打龙前.zip").click(); await page.waitForSelector("#modal-root .modal");
        const m = await page.evaluate(() => ({ ok: $('#modal-root [data-x="ok"]').textContent, b: $("#modal-root .m-body").textContent }));
        assert(m.ok === "停止并还原" && m.b.includes("打龙前") && m.b.includes("3") && m.b.includes("先保存世界并停止"), "确认框：" + JSON.stringify(m));
        await page.click('#modal-root [data-x="ok"]'); await settle(900);
        const st = await dState({});
        assert(JSON.stringify(st.lastRestore) === JSON.stringify({ inst: SURV, file: "20261025-221000.000000000-打龙前.zip", stopFirst: true }) &&
          JSON.stringify(st.lifecycle) === JSON.stringify([{ inst: SURV, act: "stop" }]), "一次请求做完停服和还原：" + JSON.stringify([st.lastRestore, st.lifecycle]));
        assert(await toastHas("还原完成") && await toastHas("服务器还停着") && !(await toastHas("还原失败", 300)), "应还原成功，并说服务器还停着");
        await page.waitForFunction(() => document.querySelector("#bk-now").textContent.includes("2026-10-25 22:10"), null, { timeout: 4000 });
        const now = await page.textContent("#bk-now");
        assert(now.includes("当前世界：从 2026-10-25 22:10 的备份还原（打龙前）") && now.includes("还原于 2026-11-15 21:05"), "页顶：" + now);
        assert(!(await page.$(".bk-need-stop")) && (await page.textContent(".bk-list")).includes("现在的世界"), "停下来以后不再写要先停服，列表里标出现在的世界");
        // 重开页面（换个标签再回来）仍在：记在服务器的数据里
        await open(SURV, "console"); await open(SURV, "world/backups");
        assert((await page.textContent("#bk-now")).includes("打龙前"), "重进以后页顶应还在");
        // 停着的直接还原：确认键叫「还原」，不带 stopFirst
        await byName("button", "还原 20261026-040000.000000000.zip").click(); await page.waitForSelector("#modal-root .modal");
        assert(await page.textContent('#modal-root [data-x="ok"]') === "还原", "停着时的确认键");
        await page.click('#modal-root [data-x="ok"]'); await settle(700);
        assert(JSON.stringify((await dState({})).lastRestore) === JSON.stringify({ inst: SURV, file: "20261026-040000.000000000.zip", stopFirst: false }), "停着时的还原请求");
        // 手动备份带标签
        await page.fill("#bk-label", "换地图前"); await page.click("#bk-create"); await settle(600);
        assert(JSON.stringify((await dState({})).lastBackup) === JSON.stringify({ inst: SURV, label: "换地图前" }) && (await page.$$eval(".bk-list .ri-title", e => e[0].textContent)) === "换地图前", "带标签的备份");
      } finally { await dState({ instReset: true }); }
    });

    await run("beta.3 详情 R15：设置 → 启动与内存页顶列出核心、版本、端口、Java、服务器文件夹（能复制、能打开），点端口去常用设置的端口那一项；「打开文件夹」菜单有「备份」；备份页写出备份放在哪、保留份数只读并带路去设置", async () => {
      await dState({ instReset: true });
      try {
        await open(PACK0, "settings/jvm");
        const f = await page.evaluate(() => [...document.querySelectorAll(".jm-fact")].map(e => e.querySelector(".jm-k").textContent + "=" + e.querySelector(".jm-v").textContent.replace(/\s+/g, " ").trim()));
        assert(f[0] === "核心=Forge 47.3.0" && f[1] === "版本=Minecraft 1.20.1" && f[2].startsWith("端口=25566") && f[3] === "Java=Java 17" && f[4].startsWith("服务器文件夹=D:\\MineDeck\\servers\\create-pack"), "页顶：" + JSON.stringify(f));
        await page.evaluate(() => { window.__copied = null; Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied = t; } } }); });
        await page.click("#jm-dir-copy"); await settle(150);
        assert(await page.evaluate(() => window.__copied) === "D:\\MineDeck\\servers\\create-pack", "复制的路径");
        await page.click("#jm-port"); await page.waitForSelector('[data-k="server-port"]'); await settle(300);
        const p = await page.evaluate(() => ({ h: decodeURIComponent(location.hash), focus: document.activeElement && document.activeElement.dataset.k, flash: !!document.querySelector(".prop-item.flash [data-k='server-port']") }));
        assert(p.h === `#/inst/${PACK0}/settings/common` && p.focus === "server-port" && p.flash, "点端口以后：" + JSON.stringify(p));
        // 「打开文件夹」菜单里有「备份」（不在服务器文件夹里，走打开任意文件夹的接口）
        await page.click("#d-dir"); await page.waitForSelector(".dropmenu");
        await page.click('.dropmenu button:has-text("备份")'); await settle(300);
        assert((await dState({})).lastOpenPath === "D:\\MineDeck\\backups\\" + PACK0, "打开的备份文件夹：" + (await dState({})).lastOpenPath);
        // 备份页：写出放在哪、保留几份（只读），带路去「设置 → 存储位置」；没有能改份数的输入框了
        await open(PACK0, "world/backups");
        const w = await page.evaluate(() => ({ where: $("#bk-where").textContent.replace(/\s+/g, " "), href: $("#bk-keep-link").getAttribute("href"), input: !!document.querySelector("#bk-keep input, input#bk-keep") }));
        assert(w.where.includes("D:\\MineDeck\\backups\\" + PACK0) && w.where.includes("保留最近 10 份") && w.href === "#/settings/storage" && !w.input, "备份放在哪：" + JSON.stringify(w));
      } finally { await dState({ instReset: true }); }
    });

    await run("备份页页顶写清现在的世界从哪来（导入的写来源名、换回过、没有记录、从备份还原的优先）；改存放位置以前的备份标出来、能打开原来的文件夹", async () => {
      await dState({ instReset: true, local: true, world: { reset: true } });
      try {
        // 整合包服的恢复点里最新一条是从 SkyCity.zip 导入的（已导入）：页顶写来源名和导入时间，带路去「换地图」
        await open(PACK0, "world/backups");
        let now = await page.textContent("#bk-now");
        assert(now.includes("当前世界：从「SkyCity.zip」导入的地图") && now.includes("导入于 2026-11-10") && !now.includes("服务器自己的世界"), "导入的地图：" + now);
        assert((await page.getAttribute("#bk-now-map", "href")) === `#/inst/${E(PACK0)}/world/map`, "应带路去换地图");
        // 最新一条是没生效的（rolled-back），再往前是换回过的：没生效的不算，按换回过的写
        await dState({ world: { inst: { [PACK0]: { points: [
          { id: "wa", state: "rolled-back", created: "2026-11-12T10:00:00+08:00", source: "没生效.zip", candidate: "world", levelName: "world", hasOld: false, oldBytes: 0, keptBytes: 0 },
          { id: "wb", state: "restored", created: "2026-11-11T10:00:00+08:00", source: "换回的.zip", candidate: "world", levelName: "world", hasOld: false, oldBytes: 0, keptBytes: 0 },
          { id: "wc", state: "committed", created: "2026-11-10T10:00:00+08:00", source: "更早的.zip", candidate: "world", levelName: "world", hasOld: true, oldBytes: 1, keptBytes: 0 },
        ] } } } });
        await open(PACK0, "console"); await open(PACK0, "world/backups");
        now = await page.textContent("#bk-now");
        assert(now.includes("换过一次地图，后来换回了换之前的那张") && !now.includes("没生效") && !now.includes("更早的"), "换回过：" + now);
        // 正在换、要人处理的排在后面也要先说：最新一条已导入，后面跟着一条读不出来的记录（Codex 复审 beta.3 之后第一轮）
        await dState({ world: { inst: { [PACK0]: { points: [
          { id: "we", state: "committed", created: "2026-11-12T10:00:00+08:00", source: "最新的.zip", candidate: "world", levelName: "world", hasOld: true, oldBytes: 1, keptBytes: 0 },
          { id: "wf", state: "unreadable", created: "0001-01-01T00:00:00Z", source: "", candidate: "", levelName: "", hasOld: false, oldBytes: 0, keptBytes: 0 },
        ] } } } });
        await open(PACK0, "console"); await open(PACK0, "world/backups");
        now = await page.textContent("#bk-now");
        assert(now.includes("换地图还没走完，或者要人处理") && !now.includes("最新的"), "排在后面的要人处理：" + now);
        // 世界名对不上现在的 level-name 的那条不算（是另一个世界文件夹）
        await dState({ world: { inst: { [PACK0]: { levelName: "world2", points: [
          { id: "wd", state: "committed", created: "2026-11-12T10:00:00+08:00", source: "别的世界.zip", candidate: "world", levelName: "world", hasOld: true, oldBytes: 1, keptBytes: 0 },
        ] } } } });
        await open(PACK0, "console"); await open(PACK0, "world/backups");
        now = await page.textContent("#bk-now");
        assert(now.includes("没有从备份还原过，「换地图」里也没有导入记录") && !now.includes("别的世界") && !(await page.$("#bk-now-map")), "世界名对不上：" + now);
        // 从备份还原过（worldFrom）优先：换地图时会清掉它，有它就说明还原在后
        await dState({ world: { inst: { [PACK0]: { levelName: "world" } } }, inst: { name: PACK0, worldFrom: { backup: "20261026-040000.000000000.zip", backupTime: "2026-10-26 04:00:00", at: "2026-11-15 21:05:00" } } });
        await open(PACK0, "console"); await open(PACK0, "world/backups");
        now = await page.textContent("#bk-now");
        assert(now.includes("当前世界：从 2026-10-26 04:00 的备份还原") && !now.includes("导入"), "从备份还原的优先：" + now);

        // 生存服没换过地图：不说「服务器自己的世界」，只说没有记录
        await open(SURV, "world/backups");
        now = await page.textContent("#bk-now");
        assert(now.includes("没有从备份还原过，「换地图」里也没有导入记录") && !now.includes("服务器自己的世界"), "没换过地图：" + now);
        assert(!(await page.$("#bk-olds")) && (await page.textContent("#bk-where")).includes("备份放在") && !(await page.textContent("#bk-where")).includes("新的备份放在"), "没有旧文件夹的备份时不提");

        // 改过备份存放文件夹：两份在原来的文件夹，一份在现在的
        const OLD = "E:\\旧备份\\" + SURV;
        await dState({ inst: { name: SURV, backupDir: "F:\\新备份\\" + SURV, backups: [
          { file: "20261116-030000.000000000.zip", sizeMb: 1, time: "2026-11-16 03:00:00" },
          { file: "20261026-040000.000000000.zip", sizeMb: 2, time: "2026-10-26 04:00:00", dir: OLD, elsewhere: true },
          { file: "20261025-221000.000000000-打龙前.zip", sizeMb: 3, time: "2026-10-25 22:10:00", label: "打龙前", dir: OLD, elsewhere: true },
        ] } });
        await open(SURV, "console"); await open(SURV, "world/backups");
        const v = await page.evaluate(() => ({ olds: $("#bk-olds").textContent.replace(/\s+/g, " "), where: $("#bk-where").textContent.replace(/\s+/g, " "),
          tags: [...document.querySelectorAll(".bk-list .row-item")].map(r => !!r.querySelector(".tag.neutral") && r.querySelector(".tag.neutral").textContent) }));
        assert(v.olds.includes("另有 2 份是改存放位置以前做的") && v.olds.includes(OLD) && v.where.includes("新的备份放在 F:\\新备份\\" + SURV), "旧文件夹的说明：" + JSON.stringify(v));
        assert(JSON.stringify(v.tags) === JSON.stringify([false, "在原来的文件夹", "在原来的文件夹"]), "列表里标出在原来文件夹的那几份：" + JSON.stringify(v.tags));
        await byName("button", "打开原来的备份文件夹 " + OLD).click(); await settle(300);
        assert((await dState({})).lastOpenPath === OLD, "打开的是原来的文件夹：" + (await dState({})).lastOpenPath);
        const ax = await axProblems();
        assert(!ax.length, "备份页无障碍检查：" + ax.join("、"));
        // 从局域网打开：没有「打开」，说明照旧
        await dState({ local: false });
        await open(SURV, "console"); await open(SURV, "world/backups");
        assert((await page.textContent("#bk-olds")).includes("另有 2 份") && !(await page.$("[data-bk-open-old]")), "局域网上不给打开");
      } finally { await dState({ instReset: true, local: true, world: { reset: true } }); }
    });

    await run("备份页页顶：随交接包带来的备份来源照实说是原来那台电脑上还原的、那份备份不在这台电脑上；本机列表里同名的那份不标「现在的世界」", async () => {
      await dState({ instReset: true, local: true, world: { reset: true } });
      try {
        const FILE = "20261108-234100.000000000-拆主基地前.zip";
        await dState({ inst: { name: SURV, worldFrom: { backup: FILE, label: "拆主基地前", backupTime: "2026-11-08 23:41:00", at: "2026-11-12 20:05:31", carried: true },
          backups: [{ file: FILE, sizeMb: 1, time: "2026-11-08 23:41:00", label: "拆主基地前" }] } });
        await open(SURV, "console"); await open(SURV, "world/backups");
        const now = await page.textContent("#bk-now");
        assert(now.includes("原来那台电脑上从 2026-11-08 23:41 的备份还原来的") && now.includes("拆主基地前") && now.includes("不在这台电脑上") && !now.includes("还原于"),
          "随交接包带来的：" + now);
        assert(!(await page.$(".bk-list .tag:not(.neutral)")), "本机列表里同名的那份不该标「现在的世界」");
        // 在这台电脑上自己还原的（没有 carried）照旧：写还原时间，列表里标出来
        await dState({ inst: { name: SURV, worldFrom: { backup: FILE, label: "拆主基地前", backupTime: "2026-11-08 23:41:00", at: "2026-11-15 21:05:00" } } });
        await open(SURV, "console"); await open(SURV, "world/backups");
        const mine = await page.textContent("#bk-now");
        assert(mine.includes("当前世界：从 2026-11-08 23:41 的备份还原") && mine.includes("还原于 2026-11-15 21:05") && !mine.includes("原来那台电脑上") &&
          (await page.textContent(".bk-list .tag:not(.neutral)")) === "现在的世界", "本机还原的：" + mine);
      } finally { await dState({ instReset: true, local: true, world: { reset: true } }); }
    });

    await run("beta.3 详情 R16：常用、启动与内存、全部配置、定时任务都用吸底保存条；定时任务点「添加」进草稿（标「未保存」），保存条按条数算", async () => {
      await dState({ instReset: true });
      for (const t of ["settings/common", "settings/jvm", "settings/raw", "tasks"]) {
        await open(SURV, t);
        assert(await page.$(".save-bar.sticky [data-sb=save]"), t + " 没有吸底保存条");
      }
      assert(!(await page.$("#jm-java-set")), "不该再有单独的「更换 Java」");
      await open(SURV, "tasks");
      for (const at of ["05:00", "06:30"]) { await page.fill("#sc-at", at); await page.click("#sc-add"); await settle(100); }
      const v = await page.evaluate(() => ({ bar: $(".sb-text").textContent, drafts: [...document.querySelectorAll("#sched-list .java-row.draft")].map(r => r.textContent.replace(/\s+/g, " ").trim()) }));
      assert(v.bar === "2 项修改未保存" && v.drafts.length === 2 && v.drafts.every(x => x.includes("未保存")), "加两条：" + JSON.stringify(v));
      await page.click('[data-sb="save"]'); await settle(500);
      assert((await page.textContent(".sb-text")) === "所有修改已保存" && !(await page.$("#sched-list .java-row.draft")), "保存以后不再是草稿");
    });

    await run("beta.3 详情 R21：配的 Java 找不到时，卡片和详情页头的提示区都警告并带路去「设置 → 启动与内存」，Java 下拉里那一项标「找不到」；换好以后提示消失", async () => {
      await dState({ instReset: true });
      try {
        await dState({ inst: { name: PACK0, javaMissing: true, javaPath: "C:\\Gone\\jdk-17\\bin\\java.exe" } });
        await page.goto(url("#/instances")); await settle(600);
        const card = await page.$eval(`.card[data-name="${PACK0}"] .inst-java-missing`, a => ({ t: a.textContent, href: a.getAttribute("href") })).catch(() => null);
        assert(card && card.t.includes("找不到这台服务器配的 Java") && decodeURIComponent(card.href) === `#/inst/${PACK0}/settings/jvm`, "卡片：" + JSON.stringify(card));
        await page.click(`.card[data-name="${PACK0}"] .inst-java-missing`); await page.waitForSelector("#jm-java option[value='0']", { state: "attached" });
        assert(await hashNow() === `#/inst/${PACK0}/settings/jvm`, "点卡片上的警告应去启动与内存：" + await hashNow());
        const sel = await page.$eval("#jm-java", s => s.selectedOptions[0].textContent);
        assert(sel === "Java 17（C:\\Gone\\jdk-17\\bin\\java.exe） · 现在用的 · 找不到", "下拉里现在用的那项：" + sel);
        assert(!(await page.$("#d-java-missing a")) && (await page.textContent("#d-java-missing")).includes("C:\\Gone\\jdk-17"), "在启动与内存页上就不用再带路");
        await open(PACK0, "console");
        const n = await page.evaluate(() => ({ t: $("#d-java-missing").textContent, href: $("#d-java-missing a").getAttribute("href") }));
        assert(n.t.includes("找不到这台服务器配的 Java") && decodeURIComponent(n.href) === `#/inst/${PACK0}/settings/jvm`, "页头提示区：" + JSON.stringify(n));
        // 换好了（后端不再报 javaMissing）：轮询到以后提示消失
        await dState({ inst: { name: PACK0, javaMissing: false } });
        await page.waitForFunction(() => !document.querySelector("#d-java-missing"), null, { timeout: 5000 });
      } finally { await dState({ instReset: true }); }
    });

    await run("beta.3 详情：伙伴的弹窗叠在弹窗层里，不把下面开着的弹窗换掉；Esc 只关最上面那层", async () => {
      await dState({ cp: { reset: true, inst: { [PACK0]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true } } } } });
      try {
        await open(PACK0, "ext/companion"); await page.waitForSelector("#cp-zone-add");
        await page.evaluate(() => modalLayer(`<div class="modal" role="dialog" aria-label="下面那层"><input id="keep-in" value="还在"></div>`, "keep-layer"));
        await page.evaluate(() => document.querySelector("#cp-zone-add").click()); await page.waitForSelector(".cp-form");
        let n = await page.evaluate(() => ({ layers: $("#modal-root").childElementCount, keep: $("#keep-in") && $("#keep-in").value }));
        assert(n.layers === 2 && n.keep === "还在", "伙伴的弹窗把下面那层换掉了：" + JSON.stringify(n));
        await page.keyboard.press("Escape"); await settle(300);
        n = await page.evaluate(() => ({ form: !!document.querySelector(".cp-form"), keep: !!$("#keep-layer") }));
        assert(!n.form && n.keep, "Esc 应只关伙伴的弹窗：" + JSON.stringify(n));
      } finally { await page.evaluate(() => { const k = $("#keep-layer"); if (k) k.remove(); }); await dState({ cp: { reset: true } }); }
    });
  }
  // ==================== beta.3 详情页结束 ====================

  // ==================== beta.3 工具箱（#/toolbox） ====================
  {
    const tbZlib = require("zlib");
    const tbCrcTbl = (() => {
      const t = new Uint32Array(256);
      for (let i = 0; i < 256; i++) {
        let c = i;
        for (let k = 0; k < 8; k++) c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1);
        t[i] = c;
      }
      return t;
    })();
    const tbCrc32 = b => { let c = 0xffffffff; for (let i = 0; i < b.length; i++) c = tbCrcTbl[(c ^ b[i]) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
    const tbChunk = (type, data) => {
      const out = Buffer.alloc(12 + data.length);
      out.writeUInt32BE(data.length, 0);
      out.write(type, 4, 4, "ascii");
      data.copy(out, 8);
      out.writeUInt32BE(tbCrc32(out.subarray(4, 8 + data.length)), 8 + data.length);
      return out;
    };
    const makePngBuf = (w, h) => {
      const ihdr = Buffer.alloc(13);
      ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4);
      ihdr[8] = 8; ihdr[9] = 6;
      const raw = Buffer.alloc(h * (1 + w * 4), 160);
      for (let y = 0; y < h; y++) raw[y * (1 + w * 4)] = 0;
      return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), tbChunk("IHDR", ihdr), tbChunk("IDAT", tbZlib.deflateSync(raw)), tbChunk("IEND", Buffer.alloc(0))]);
    };
    const tbState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const clip = () => page.evaluate(() => { window.__copied = null; Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied = t; } } }); });

    await run("工具箱：侧栏导航、快捷键 4 / 5 与首页工具卡片跳转正常，页面无无障碍重名或漏名控件", async () => {
      try {
        await page.goto(url("#/instances")); await settle(300);
        await page.keyboard.press("4"); await settle(300);
        assert((await page.evaluate(() => location.hash)) === "#/toolbox", "快捷键 4 应打开工具箱：" + await page.evaluate(() => location.hash));
        await page.keyboard.press("5"); await settle(300);
        assert((await page.evaluate(() => location.hash)) === "#/settings", "快捷键 5 应打开设置：" + await page.evaluate(() => location.hash));
        await page.click('#sidebar a[data-nav="toolbox"]'); await page.waitForSelector('.tb-card[data-tool="skin-pack"]');
        const axHome = await axProblems();
        assert(!axHome.length, "工具箱首页无障碍检查：" + axHome.join("、"));
        assert(await page.$('.tb-card[data-tool="mrpack-export"]'), "工具箱首页应列出两个核心工具");
        await page.click('.tb-card[data-tool="skin-pack"]'); await page.waitForSelector("#sp-mc");
        assert((await page.evaluate(() => location.hash)) === "#/toolbox/skin-pack", "点皮肤包卡片应跳转");
      } finally { await tbState({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
    });

    await run("工具箱 → 离线皮肤包：读取服务器玩家、1.19.2 撞槽检测与解决、PNG 尺寸校验、64x32 自动扩充提醒、生成与复制 SHA-1、顺位模式与局域网隐藏本机保存", async () => {
      try {
        await tbState({ toolbox: { reset: true }, local: true });
        await page.goto(url("#/toolbox/skin-pack"));
        await page.waitForSelector("#sp-mc"); await settle(400);
        assert((await page.textContent("#sp-ver-badge")).includes("18"), "1.21.4 默认应显示 18 个默认槽位");
        // 版本 26.1 展示 min_format
        await page.fill("#sp-mc", "26.1"); await settle(350);
        assert((await page.textContent("#sp-packfmt-badge")).includes("min_format 84"), "26.1 应展示 min_format：" + await page.textContent("#sp-packfmt-badge"));
        await page.fill("#sp-mc", "1.21.4"); await settle(350);

        // 从已有服务器导入玩家名单（Steve, Alex, Notch_CN）
        await page.waitForSelector("#sp-import-players");
        await page.click("#sp-import-players"); await settle(400);
        assert((await page.$$eval("#sp-rows .tb-skin-row", r => r.length)) === 3, "应导入 3 名玩家");

        // 切到 1.19.2（只有 2 个默认槽位），3 个玩家必撞槽
        await page.fill("#sp-mc", "1.19.2"); await page.waitForSelector("#sp-conflicts");
        assert((await page.textContent("#sp-ver-badge")).includes("2 个默认槽位"), "1.19.2 应显示 2 个默认槽位");
        assert(await page.isDisabled("#sp-build"), "存在未解决撞槽时禁止生成");
        const axConf = await axProblems();
        assert(!axConf.length, "撞槽状态无障碍检查：" + axConf.join("、"));

        // 解决撞槽：保留第一条（其他同槽被跳过）
        await page.click(".sp-resolve-keep"); await settle(350);
        assert(!(await page.$("#sp-conflicts")), "点保留后冲突卡应消失");

        // 传非 64x64 / 64x32 的图片：前端直接拦截并给出原因
        await page.setInputFiles('.sp-file[data-idx="0"]', { name: "bad.png", mimeType: "image/png", buffer: makePngBuf(32, 32) });
        await settle(300);
        assert((await page.textContent('.tb-skin-row[data-idx="0"] .tb-err-line')).includes("64×64 或旧版 64×32"), "非法尺寸应报错");
        assert(await page.isDisabled("#sp-build"), "有图片校验报错时不能生成");

        // 给仍在使用的两行（未被跳过的两条）分别上传 64x64 与旧版 64x32 PNG
        const activeIdxs = await page.$$eval("#sp-rows .tb-skin-row", rows =>
          rows.filter(r => !r.textContent.includes("已跳过")).map(r => r.dataset.idx)
        );
        assert(activeIdxs.length === 2, "1.19.2 解决冲突后应剩 2 条激活皮肤：" + JSON.stringify(activeIdxs));
        await page.setInputFiles(`.sp-file[data-idx="${activeIdxs[0]}"]`, { name: "s0.png", mimeType: "image/png", buffer: makePngBuf(64, 64) });
        await settle(250);
        await page.setInputFiles(`.sp-file[data-idx="${activeIdxs[1]}"]`, { name: "legacy.png", mimeType: "image/png", buffer: makePngBuf(64, 32) });
        await settle(300);
        assert((await page.textContent(`.tb-skin-row[data-idx="${activeIdxs[1]}"]`)).includes("64×32"), "64x32 旧版皮肤应给出镜像扩充说明");

        // 生成并验证 SHA-1 与下载链接
        await clip();
        await page.click("#sp-build");
        await page.waitForSelector("#sp-done-card");
        const sha1 = await page.textContent("#sp-sha1");
        assert(/^[0-9a-f]{40}$/.test(sha1.trim()), "应展示 40 位 SHA-1：" + sha1);
        await page.click("#sp-copy-sha1"); await settle(200);
        assert((await page.evaluate(() => window.__copied)) === sha1.trim(), "复制按钮应复制 SHA-1");
        assert(await page.$("#sp-dl[href*='/api/toolbox/skin-pack/download/']"), "应生成皮肤包下载链接");
        const axDone = await axProblems();
        assert(!axDone.length, "皮肤包生成后无障碍检查：" + axDone.join("、"));

        // 切回 1.21.4（18 槽位）后测试顺位分配模式并验证构建
        await page.fill("#sp-mc", "1.21.4"); await settle(300);
        await page.click('#sp-mode button[data-mode="sequential"]'); await settle(300);
        await page.setInputFiles('.sp-file[data-idx="0"]', { name: "seq0.png", mimeType: "image/png", buffer: makePngBuf(64, 64) });
        await page.setInputFiles('.sp-file[data-idx="1"]', { name: "seq1.png", mimeType: "image/png", buffer: makePngBuf(64, 64) });
        await page.setInputFiles('.sp-file[data-idx="2"]', { name: "seq2.png", mimeType: "image/png", buffer: makePngBuf(64, 64) });
        await settle(300);
        await page.click("#sp-build"); await page.waitForSelector("#sp-done-card");

        // 局域网访问时隐藏「同步保存到本机文件夹」
        await tbState({ local: false });
        await page.goto(url("#/toolbox/skin-pack")); await page.waitForSelector("#sp-mc"); await settle(350);
        assert(await page.$eval("#sp-host-row", e => e.hidden), "非本机请求时应隐藏保存到本机文件夹输入行");
      } finally { await tbState({ toolbox: { reset: true }, local: true }); }
    });

    await run("工具箱 → 导出 Modrinth 整合包：草稿配置持久化、嵌入选模组、preview 显示下载项/仅客户端标 unsupported/overrides，以及已有服务器+config 导出", async () => {
      try {
        await tbState({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
        await page.goto(url("#/toolbox/mrpack-export"));
        await page.waitForSelector("#mp-modcart-host #mc-hits"); await settle(400);

        // 填写名称与版本号（触发草稿持久化）
        await page.fill("#mp-name", "好友周目整合包");
        await page.fill("#mp-ver", "2.1.0");
        await settle(400);
        await page.goto(url("#/toolbox/mrpack-export"));
        await page.waitForSelector("#mp-modcart-host #mc-hits"); await settle(400);
        assert((await page.$eval("#mp-name", e => e.value)) === "好友周目整合包", "重启/重进页面后草稿名称应保留");

        // 草稿还没读回来就改了名称：先不存（不然没动过的版本号会按默认值盖掉存好的 2.1.0），读回来以后存合起来的那份（Codex 复审 beta.3 第二轮）
        {
          let releaseDraft; const gateDraft = new Promise(r => releaseDraft = r);
          const holdDraft = async route => { if (route.request().method() !== "GET") return route.continue().catch(() => {}); await gateDraft; await route.continue().catch(() => {}); };
          await page.route("**/api/toolbox/mrpack/draft", holdDraft);
          try {
            const since = reqs.length;
            await page.goto(url("#/toolbox/mrpack-export"));
            await page.waitForSelector("#mp-name");
            await page.fill("#mp-name", "改过的名字"); await settle(500);
            const draftPuts = () => reqs.slice(since).filter(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft").map(r => JSON.parse(r.b || "{}"));
            assert(!draftPuts().length, "草稿读回来以前不该存：" + JSON.stringify(draftPuts()));
            assert(await page.isDisabled('#mp-src [data-src="instance"]'), "草稿读回来以前来源切换应锁着");
            releaseDraft(); await settle(700);
            // 读回来以后只存人改过的那一格（后端只改传了的字段，没动过的版本号留着存好的 2.1.0）
            const put = draftPuts().pop();
            const stored = (await tbState({})).toolbox.draft;
            assert(put && JSON.stringify(put) === JSON.stringify({ name: "改过的名字" }) && stored.name === "改过的名字" && stored.versionId === "2.1.0",
              "读回来以后只存改过的名称、版本号留着：" + JSON.stringify({ put, stored }));
            assert((await page.$eval("#mp-ver", e => e.value)) === "2.1.0" && (await page.$eval("#mp-name", e => e.value)) === "改过的名字", "没动过的版本号补上，改过的名称留着");
          } finally { releaseDraft(); await page.unroute("**/api/toolbox/mrpack/draft", holdDraft); }
          await page.fill("#mp-name", "好友周目整合包"); await settle(400);
          // 读草稿失败：这一次不自动存，存好的草稿不被默认值盖掉（第三轮）
          const failDraft = route => route.request().method() === "GET" ? route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ ok: false, error: "读不出来" }) }) : route.continue();
          await page.route("**/api/toolbox/mrpack/draft", failDraft);
          try {
            const since = reqs.length;
            await page.goto(url("#/toolbox/mrpack-export"));
            await page.waitForSelector("#mp-name"); await settle(500);
            await page.fill("#mp-name", "读失败时改的"); await settle(600);
            assert(!reqs.slice(since).some(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft"), "读草稿失败时不该自动存");
            // 页面上要说清这一次不自动保存，并给「重新读取」（Codex 复审 beta.3 第五轮：以前什么都不说）
            const errTxt = await page.textContent("#mp-draft-err");
            assert(errTxt.includes("读不出上次存好的整合包信息") && errTxt.includes("不会自动保存") && errTxt.includes("现在改的不保留"), "读失败要常驻说明：" + errTxt);
            assert(await page.$("#mp-draft-msg[role=status] #mp-draft-retry"), "说明在 status 区里，带「重新读取」");
            // 还读不出来：说明留着，接着不存
            await page.click("#mp-draft-retry"); await settle(500);
            assert(await page.$("#mp-draft-err #mp-draft-retry:not([disabled])"), "再读还失败时说明留着，按钮能再点");
            await page.fill("#mp-name", "还是读不出时改的"); await settle(600);
            assert(!reqs.slice(since).some(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft"), "还读不出来时仍不该自动存");
            // 停在「从已有服务器导出」时说明和「重新读取」都不出现：在那边重新读取，切回来一改就会把服务器名存进草稿（Codex 复审 beta.3 之后第一轮）
            await page.click('#mp-src [data-src="instance"]'); await settle(300);
            assert(await page.$eval("#mp-draft-msg", e => e.hidden), "从已有服务器导出时不该有读草稿的说明");
            await page.click('#mp-src [data-src="draft"]'); await settle(300);
            assert(!(await page.$eval("#mp-draft-msg", e => e.hidden)) && await page.$("#mp-draft-retry"), "切回自己挑模组，说明和按钮还在");
          } finally { await page.unroute("**/api/toolbox/mrpack/draft", failDraft); }
          // 读得出来了：点「重新读取」，表单换成存好的那份，说明消失，之后的修改照常自动存
          {
            const since = reqs.length;
            await page.click("#mp-draft-retry"); await settle(500);
            assert(!(await page.$("#mp-draft-err")), "读出来以后说明应消失");
            assert((await page.$eval("#mp-name", e => e.value)) === "好友周目整合包" && (await page.$eval("#mp-ver", e => e.value)) === "2.1.0", "表单应换成存好的那份");
            assert(!reqs.slice(since).some(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft"), "重新读取本身不存（读失败期间改的作废）");
            await page.fill("#mp-summary", "读回来以后写的简介"); await settle(600);
            // 只存改了的简介；名称还是存好的那个（读失败期间改的名字作废了，也没有被存进去）
            const put = reqs.slice(since).filter(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft").map(r => JSON.parse(r.b || "{}")).pop();
            const stored = (await tbState({})).toolbox.draft;
            assert(put && JSON.stringify(put) === JSON.stringify({ summary: "读回来以后写的简介" }) && stored.name === "好友周目整合包" && stored.summary === "读回来以后写的简介",
              "读回来以后应恢复自动保存：" + JSON.stringify({ put, stored }));
            await page.fill("#mp-summary", ""); await settle(500);
          }
          // 改完马上切到「从已有服务器导出」：排着的保存当场存掉，存的是草稿里的名字，不是切过去以后填上的服务器名（第三轮）
          await page.goto(url("#/toolbox/mrpack-export"));
          await page.waitForSelector("#mp-modcart-host #mc-hits"); await settle(400);
          const since2 = reqs.length;
          await page.fill("#mp-name", "切走前的名字");
          await page.click('#mp-src [data-src="instance"]'); await settle(600);
          const put2 = reqs.slice(since2).filter(r => r.m === "PUT" && r.u === "/api/toolbox/mrpack/draft").map(r => JSON.parse(r.b || "{}"));
          assert(put2.length === 1 && put2[0].name === "切走前的名字", "切走时应当场存草稿里的名字：" + JSON.stringify(put2));
          await page.click('#mp-src [data-src="draft"]'); await settle(300);
          await page.fill("#mp-name", "好友周目整合包"); await settle(400);
        }

        // 在嵌入式待装清单中加入 Create（双端）和 JEI（仅客户端），并添加本地 jar
        await page.click('.mc-add[data-pid="create"]'); await settle(200);
        await page.click('.mc-add[data-pid="jei"]'); await settle(200);
        await page.setInputFiles("#mc-file", { name: "custom-local.jar", mimeType: "application/java-archive", buffer: Buffer.from("PK\x03\x04fakejar") });
        await settle(400);
        // 挑的东西进的是工具箱自己的清单（cart=toolbox），新建向导的草稿清单还是空的：建服时不会把这些装进新服务器
        const mcState = (await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data))).modcart;
        assert(!mcState.draft.length && !mcState.localDraft.length && (mcState.insts["<toolbox>"] || []).length === 2 && (mcState.localInsts["<toolbox>"] || []).length === 1,
          "工具箱挑的模组应在自己的清单里、不进向导草稿：" + JSON.stringify({ draft: mcState.draft, toolbox: mcState.insts["<toolbox>"] }));

        // 预览清单
        await page.click("#mp-preview-btn"); await page.waitForSelector("#mp-preview-box .tb-table-wrap");
        const prevTxt = await page.textContent("#mp-preview-box");
        assert(prevTxt.includes("在线下载 2 项") && prevTxt.includes("overrides 1 项") && prevTxt.includes("仅客户端（已标 server: unsupported）"), "预览信息完整：" + prevTxt);
        const axDraft = await axProblems();
        assert(!axDraft.length, "整合包草稿页无障碍检查：" + axDraft.join("、"));

        // 生成 .mrpack
        await clip();
        await page.click("#mp-build-btn"); await page.waitForSelector("#mp-done-card");
        const mpFileTxt = await page.textContent("#mp-done-card");
        assert(mpFileTxt.includes("好友周目整合包-2.1.0.mrpack"), "生成文件名应包含名称与版本号：" + mpFileTxt);
        await page.click("#mp-copy-sha1"); await settle(150);
        assert((await page.evaluate(() => window.__copied)) === (await page.textContent("#mp-sha1")).trim(), "应复制 .mrpack SHA-1");

        // 切到「从已有服务器导出」（机械动力整合包 + config）
        await page.click('#mp-src button[data-src="instance"]'); await settle(300);
        assert((await page.$eval("#mp-inst", e => e.value)) === "机械动力整合包", "应默认选中模组服：" + await page.$eval("#mp-inst", e => e.value));
        // config 默认不带（服主的配置里可能有密码、令牌）；勾上才列进 overrides/config/
        assert(!(await page.$eval("#mp-inc-cfg", e => e.checked)), "「一并打包 config」应默认不勾");
        await page.click("#mp-preview-btn"); await page.waitForSelector("#mp-preview-box .tb-table-wrap");
        assert(!(await page.textContent("#mp-preview-box")).includes("overrides/config/"), "没勾 config 时不该带配置文件");
        await page.click("#mp-inst-row .switch input"); await settle(150);
        await page.click("#mp-preview-btn"); await page.waitForSelector("#mp-preview-box .tb-table-wrap"); await settle(300);
        assert((await page.textContent("#mp-preview-box")).includes("overrides/config/create-common.toml"), "勾选 config 后应列出 overrides/config/");
        await page.click("#mp-build-btn"); await page.waitForSelector("#mp-done-card");
      } finally { await tbState({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
    });
  }
  // ==================== beta.3 工具箱结束 ====================

  const clipOk = () => page.evaluate(() => {
    window.__copied = null;
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async t => { window.__copied = t; } } });
  });
  const copiedNow = () => page.evaluate(() => window.__copied);

  await run("复制：手机远程管理的防火墙命令能一键复制完整原文，不含说明文字；键盘回车也能复制", async () => {
    await page.goto(url("#/settings/remote")); await page.waitForSelector("#lan-fw-copy");
    const shown = (await page.textContent("#lan-fw .copy-block-pre")).trim();
    const attr = await page.getAttribute("#lan-fw-copy", "data-copy-exact");
    assert(shown.startsWith("netsh advfirewall firewall add rule") && shown.includes('name="MineDeck"') && shown.includes("localport=" + PORT), "命令应是完整的 netsh：" + shown);
    assert(attr === shown && !shown.includes("防火墙") && !shown.includes("CMD"), "复制的应是命令本身，不带提示：" + attr);
    const selectable = await page.$eval("#lan-fw .copy-block-pre", el => getComputedStyle(el).userSelect);
    assert(selectable === "all" || selectable === "text", "命令应能选中：" + selectable);
    await clipOk();
    await page.focus("#lan-fw-copy");
    await page.keyboard.press("Enter");
    await page.waitForTimeout(200);
    assert(await copiedNow() === shown, "回车复制的内容：" + await copiedNow());
    assert(await page.evaluate(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("已复制防火墙命令"))), "应提示已复制");
  });

  // 让剪贴板写不进去（http 的手机页面上常见）：没有 clipboard API，execCommand 也返回 false；还原用 clipRestore（同一个页面里）
  const clipFail = (execOk = false) => page.evaluate(ok => {
    if (!window.__clip0) window.__clip0 = { c: Object.getOwnPropertyDescriptor(navigator, "clipboard"), e: document.execCommand };
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    document.execCommand = () => ok;
  }, execOk);
  const clipRestore = () => page.evaluate(() => {
    if (!window.__clip0) return;
    if (window.__clip0.c) Object.defineProperty(navigator, "clipboard", window.__clip0.c); else delete navigator.clipboard;
    document.execCommand = window.__clip0.e; delete window.__clip0;
  });
  const toastNow = s => page.evaluate(s => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes(s)), s); // 现在有没有（不等）

  await run("复制：管理页地址只复制当前 URL，不含访问密码；剪贴板失败时不报成功，选出完整原文，关掉以后焦点回到复制键", async () => {
    await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ listenLan: true }) }));
    try {
      await page.goto(url("#/settings/remote")); await page.waitForSelector("#qr-copy");
      await clipOk();
      await page.click("#qr-copy");
      await page.waitForTimeout(200);
      const urlWant = "http://192.168.1.23:" + PORT;
      assert(await copiedNow() === urlWant, "复制的管理页地址：" + await copiedNow());
      assert(!(String(await copiedNow())).includes("密码"), "地址里不该有密码");
      await page.evaluate(() => { document.querySelectorAll(".toast").forEach(t => t.remove()); });
      await clipFail();
      await page.focus("#lan-fw-copy"); await page.keyboard.press("Enter");
      await page.waitForSelector("#copy-fallback textarea");
      const fb = await page.$eval("#copy-fallback textarea", el => ({ v: el.value, a: el.selectionStart, b: el.selectionEnd }));
      const cmd = (await page.textContent("#lan-fw .copy-block-pre")).trim();
      assert(fb.v === cmd && fb.a === 0 && fb.b === cmd.length, "失败时应选中完整命令：" + JSON.stringify(fb));
      assert(!(await toastNow("已复制")), "失败时不该提示已复制");
      assert(!(await page.textContent("#lan-fw-copy")).includes("已复制"), "失败时复制键不该变成已复制");
      await page.keyboard.press("Escape");
      await page.waitForFunction(() => !document.querySelector("#copy-fallback"));
      const focused = await page.evaluate(() => document.activeElement && (document.activeElement.id || document.activeElement.tagName));
      assert(focused === "lan-fw-copy", "手动复制框关掉以后焦点应回到复制键：" + focused);
      // 走 execCommand 成功的那条路（http 页面上常见）：提示已复制，焦点不丢
      await clipFail(true);
      await page.focus("#lan-fw-copy"); await page.keyboard.press("Enter"); await page.waitForTimeout(200);
      assert(await toastNow("已复制防火墙命令"), "execCommand 成功时应提示已复制");
      const focused2 = await page.evaluate(() => document.activeElement && (document.activeElement.id || document.activeElement.tagName));
      assert(focused2 === "lan-fw-copy", "execCommand 复制以后焦点应还在复制键上：" + focused2);
    } finally {
      await clipRestore().catch(() => {});
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ listenLan: false }) }));
    }
  });

  await run("复制：隧道卡片的公网地址复制失败时，按钮不变成「已复制」，弹手动复制框", async () => {
    await page.goto(url("#/tunnels")); await page.waitForSelector("#tun-list [data-copy]");
    try {
      await clipFail();
      const b = await page.$("#tun-list [data-copy]");
      const addr = await b.getAttribute("data-copy");
      await b.click(); await page.waitForSelector("#copy-fallback textarea");
      assert(await page.$eval("#copy-fallback textarea", el => el.value) === addr, "手动复制框里应是公网地址");
      await page.waitForTimeout(200);
      assert(!(await page.textContent("#tun-list [data-copy]")).includes("已复制") && !(await toastNow("已复制")), "写不进剪贴板时不该显示已复制");
      await page.keyboard.press("Escape");
    } finally { await clipRestore().catch(() => {}); }
  });

  await run("详情页头：运行中主按钮是停止，停着时主按钮是启动；1024×576 里命令框在窗口内，报错横幅展开时整页能滚到命令框", async () => {
    const surv = encodeURIComponent("生存服 · 朋友");
    const pack = encodeURIComponent("机械动力整合包");
    await page.goto(url("#/inst/" + surv + "/console")); await page.waitForSelector("#d-stop");
    const runCls = await page.evaluate(() => ({ stop: $("#d-stop").classList.contains("primary"), start: $("#d-start").classList.contains("primary"), startOff: $("#d-start").disabled }));
    assert(runCls.stop && !runCls.start && runCls.startOff, "运行中：" + JSON.stringify(runCls));
    await page.goto(url("#/inst/" + pack + "/console")); await page.waitForSelector("#d-start");
    const stopCls = await page.evaluate(() => ({ start: $("#d-start").classList.contains("primary"), stop: $("#d-stop").classList.contains("primary") }));
    assert(stopCls.start && !stopCls.stop, "已停止：" + JSON.stringify(stopCls));
    const geo = () => page.evaluate(() => {
      const r = s => document.querySelector(s).getBoundingClientRect(), m = document.querySelector("#main");
      return { it: Math.round(r("#cmd-in").top), ib: Math.round(r("#cmd-in").bottom), vh: innerHeight, ch: Math.round(r("#console").height), ct: Math.round(r("#console").top),
        bb: Math.round(r(".console-bar").bottom), ov: getComputedStyle(m).overflowY, sh: m.scrollHeight, mch: m.clientHeight, st: m.scrollTop };
    });
    await page.setViewportSize({ width: 1024, height: 576 });
    try {
      // 没有提示：不滚就看得见命令框，整页不多出滚动
      await page.goto(url("#/inst/" + surv + "/console")); await page.waitForSelector("#cmd-in"); await page.waitForTimeout(200);
      let g = await geo();
      assert(g.ib <= g.vh + 1 && g.it >= 0 && g.ch >= 64 && g.ct >= g.bb - 1 && g.sh <= g.mch + 1, "最小窗口控制台：" + JSON.stringify(g));
      // 报错横幅展开：放不下，#main 要能滚，滚得到命令框，滚轮也滚得回页头（PR #28 复审：以前 overflow:hidden 把命令框裁掉、滚不到）
      await page.goto(url("#/inst/" + pack + "/console")); await page.waitForSelector("#diag-slot details.db-more summary");
      await page.click("#diag-slot details.db-more summary"); await page.waitForTimeout(300);
      g = await geo();
      assert(g.ov !== "hidden" && g.sh > g.mch, "放不下时 #main 应能滚：" + JSON.stringify(g));
      await page.focus("#cmd-in"); await page.waitForTimeout(200);
      g = await geo();
      assert(g.ib <= g.vh + 1 && g.it >= 0 && g.st > 0, "聚焦命令框以后它应在窗口内：" + JSON.stringify(g));
      const mb = await page.$eval("#main", m => { const b = m.getBoundingClientRect(); return { x: b.left + 40, y: b.top + 40 }; });
      await page.mouse.move(mb.x, mb.y); await page.mouse.wheel(0, -3000); await page.waitForTimeout(400);
      assert((await geo()).st === 0, "滚轮应能滚回页头");
    } finally { await page.setViewportSize({ width: 1280, height: 850 }); }
  });

  await run("AI 助手面板：窗口够宽时主区让出右侧、启动键不被盖住、内容不变窄；窗口窄或面板拖到最宽时照旧浮在上面，主区不被挤坏", async () => {
    const aiGeo = () => page.evaluate(() => {
      const m = document.querySelector("#main"), cs = getComputedStyle(m), b = document.querySelector("#d-start").getBoundingClientRect(), p = document.querySelector("#ai-panel");
      return { open: document.body.classList.contains("ai-open"), push: document.body.classList.contains("ai-push"), mr: cs.marginRight,
        content: Math.round(m.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)), hscroll: m.scrollWidth > m.clientWidth + 1,
        bright: Math.round(b.right), pleft: p ? Math.round(p.getBoundingClientRect().left) : null, pw: p ? Math.round(p.getBoundingClientRect().width) : null };
    });
    const openPanel = async () => { await page.click('button[aria-label^="AI 助手"]'); await page.waitForSelector("#ai-panel"); await page.waitForTimeout(300); };
    await page.evaluate(() => localStorage.removeItem("amh_ai_w"));
    await page.setViewportSize({ width: 1600, height: 900 });
    try {
      await page.goto(url("#/inst/" + encodeURIComponent("生存服 · 朋友") + "/console")); await page.waitForSelector("#d-start");
      const before = await aiGeo();
      await openPanel();
      let g = await aiGeo();
      assert(g.open && g.push && g.mr === g.pw + "px" && g.bright <= g.pleft + 2 && !g.hscroll && g.content >= Math.min(before.content, 700), "宽窗口应让出右侧：" + JSON.stringify({ before, g }));
      // 键盘把面板拖到最宽（720）：1600 宽时主区剩不到 720，改回浮层
      await page.focus("#ai-panel .ap-resize");
      for (let k = 0; k < 8; k++) await page.keyboard.press("Shift+ArrowLeft");
      await page.waitForTimeout(200);
      g = await aiGeo();
      assert(g.pw === 720 && !g.push && g.mr === "0px" && !g.hscroll, "面板拖到最宽时应改回浮层：" + JSON.stringify(g));
      await page.keyboard.press("Escape"); await page.waitForTimeout(300);
      g = await aiGeo();
      assert(!g.open && !g.push && g.mr === "0px", "关掉面板以后：" + JSON.stringify(g));
      // 默认大小的窗口（1280 宽）、默认宽度的面板：主区放不下，浮在上面，主区不变
      await page.evaluate(() => localStorage.removeItem("amh_ai_w"));
      await page.setViewportSize({ width: 1280, height: 850 });
      await page.goto(url("#/inst/" + encodeURIComponent("生存服 · 朋友") + "/console")); await page.waitForSelector("#d-start");
      await openPanel();
      g = await aiGeo();
      assert(g.open && !g.push && g.mr === "0px" && !g.hscroll, "1280 宽时应浮在上面：" + JSON.stringify(g));
      await page.keyboard.press("Escape");
    } finally {
      await page.evaluate(() => localStorage.removeItem("amh_ai_w")).catch(() => {});
      await page.setViewportSize({ width: 1280, height: 850 });
    }
  });

  await run("新建向导选核心：手机竖屏（360、320px）下核心卡片缩成一列，不横向溢出", async () => {
    for (const width of [360, 320]) {
      const c = await hostCtx({ host: false }, { viewport: { width, height: 740 } });
      try {
        const p = await c.newPage();
        p.on("pageerror", e => errors.push(pageErr(e)));
        await p.goto(ORIGIN + "#/create/new"); await p.waitForSelector(".core-grid .core-card"); await p.waitForTimeout(400);
        const w = await p.evaluate(() => {
          const m = document.querySelector("#main");
          return { sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth, msw: m.scrollWidth, mcw: m.clientWidth,
            wide: [...document.querySelectorAll(".core-grid .core-card")].filter(e => e.getBoundingClientRect().right > innerWidth + 1).length };
        });
        assert(w.sw <= w.cw && w.msw <= w.mcw + 1 && w.wide === 0, width + "px 横向溢出：" + JSON.stringify(w));
      } finally { await c.close(); }
    }
  });

  await run("设置 → 常用、全部配置：只提交这一页改过的项，两个窗口同时开着时不拿旧值盖回另一边刚存的", async () => {
    const SURV = "生存服 · 朋友", at = ORIGIN + "#/inst/" + encodeURIComponent(SURV) + "/settings/";
    const c = await hostCtx({ host: false });
    try {
      const pa = await c.newPage(), pb = await c.newPage();
      for (const p of [pa, pb]) {
        p.on("pageerror", e => errors.push(pageErr(e)));
        await p.goto(at + "common"); await p.waitForSelector('[data-k="difficulty"]'); await p.waitForSelector('[data-k="max-players"]');
      }
      const lastPut = p => p.evaluate(() => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data.lastPropsPut));
      const saveNow = async p => {
        await p.click('.save-bar.sticky [data-sb="save"]');
        await p.waitForFunction(() => !document.querySelector(".save-bar.sticky").classList.contains("dirty"), null, { timeout: 5000 });
      };
      // 电脑上（A）改难度、保存：只发难度
      const diff = await pa.$eval('[data-k="difficulty"]', s => {
        const v = [...s.options].map(o => o.value).find(x => x !== s.value);
        s.value = v; s.dispatchEvent(new Event("change", { bubbles: true })); return v;
      });
      await saveNow(pa);
      let put = await lastPut(pa);
      assert(put && put.inst === SURV && JSON.stringify(put.pairs) === JSON.stringify([{ key: "difficulty", value: diff }]), "A 只提交难度：" + JSON.stringify(put));
      // 手机上（B，打开得早，难度还是旧的）只改人数、保存：不带旧的难度
      await pb.fill('[data-k="max-players"]', "7");
      await saveNow(pb);
      put = await lastPut(pb);
      assert(put && JSON.stringify(put.pairs) === JSON.stringify([{ key: "max-players", value: "7" }]), "B 只提交人数、不带旧的难度：" + JSON.stringify(put));
      // 全部配置：同样只提交改过的那一项
      await pa.goto(at + "raw"); await pa.waitForSelector("table.raw input[data-k]");
      const key = await pa.$eval("table.raw input[data-k]", e => e.dataset.k);
      await pa.fill(`table.raw input[data-k="${key}"]`, "只改这一项");
      await saveNow(pa);
      put = await lastPut(pa);
      assert(put && JSON.stringify(put.pairs) === JSON.stringify([{ key, value: "只改这一项" }]), "全部配置只提交改过的：" + JSON.stringify(put));
    } finally { await c.close(); }
  });

  // ==================== 两个窗口同时开着同一处设置（Codex 复审 v3.2.0 第二轮列的十处，2026-11-17 修）====================
  // 每条都是：两个页面打开同一处（电脑上的窗口 A、手机上早就开着的 B），A 改一样存下，B 拿着打开时的样子改另一样存下。
  // B 发出去的只有自己改的那一样，或者按条增删，A 刚存的不被改回去；AI 服务商几项互相牵连，B 存的时候被拦下、请它重新读取。
  // 两个页面各自的 PUT 请求体用 twoWin 记下来；后端最后的样子从仿真后端读
  const twoWin = async (hash, ready, fn) => {
    const c = await hostCtx({ host: false });
    try {
      const pa = await c.newPage(), pb = await c.newPage(), puts = { a: [], b: [] };
      for (const [p, k] of [[pa, "a"], [pb, "b"]]) {
        p.on("pageerror", e => errors.push(pageErr(e)));
        p.on("request", r => { if (r.method() === "PUT") puts[k].push({ u: decodeURIComponent(new URL(r.url()).pathname), b: JSON.parse(r.postData() || "{}") }); });
        await p.goto(ORIGIN + hash); await p.waitForSelector(ready); await p.waitForTimeout(400);
      }
      const last = (k, suffix) => { const x = puts[k].filter(r => r.u.endsWith(suffix)).pop(); return x ? x.b : null; };
      await fn(pa, pb, last);
    } finally { await c.close(); }
  };
  const mockSet = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
  const apiGet = path => page.evaluate(p => fetch(p, { headers: { "X-Token": "mock" } }).then(r => r.json()).then(j => j.data), path);
  const toastIn = (p, s, timeout = 5000) => p.waitForFunction(x => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes(x)), s, { timeout }).then(() => true, () => false);
  const until = async (fn, ms = 5000) => { const t0 = Date.now(); while (!(await fn())) { if (Date.now() - t0 > ms) return false; await settle(50); } return true; };
  const waitHeld = async (count = 1) => { assert(await until(async () => ((await mockSet({})).heldReqs || []).length >= count), `等待仿真后端扣住请求超时（期望 ${count} 个）`); };
  const saveIn = async p => {
    await p.click('.save-bar.sticky [data-sb="save"]');
    await p.waitForFunction(() => !document.querySelector(".save-bar.sticky").classList.contains("dirty"), null, { timeout: 5000 });
  };
  const jstr = x => JSON.stringify(x);

  await run("两个窗口：手机远程管理——一边刚关掉了远程管理，另一边（开关还显示开着）只改访问密码，只发密码、不会把它又打开", async () => {
    await mockSet({ listenLan: true, lanSet: true });
    try {
      await twoWin("#/settings/remote", "#lan-on", async (pa, pb, last) => {
        await pa.click("#lan-on"); await pa.waitForTimeout(400);
        assert(jstr(last("a", "/api/config")) === jstr({ listenLan: false }), "A 关掉远程管理：" + jstr(last("a", "/api/config")));
        assert(await pb.$eval("#lan-on", e => e.checked), "B 还没刷新，开关还显示开着");
        await pb.fill("#lan-pw", "pw-two-win"); await pb.click("#lan-save");
        assert(await toastIn(pb, "访问密码已保存"), "B 存密码要有提示");
        assert(jstr(last("b", "/api/config")) === jstr({ lanPassword: "pw-two-win" }), "B 只发密码，不带页面上的开关：" + jstr(last("b", "/api/config")));
        assert((await mockSet({})).config.listenLan === false, "远程管理还是关着的");
      });
    } finally { await mockSet({ listenLan: false, lanSet: false }); }
  });

  await run("两个窗口：启动与内存——一边改了 JVM 参数，另一边只改内存，只发内存（参数不发、也不回读），参数不被改回", async () => {
    const PK = "机械动力整合包";
    await mockSet({ instReset: true });
    try {
      await twoWin("#/inst/" + encodeURIComponent(PK) + "/jvm", "#jm-jvm", async (pa, pb, last) => {
        await pa.fill("#jm-jvm", "-XX:+UseZGC");
        await saveIn(pa);
        const a = last("a", "/settings");
        assert(a && a.xmxMb === 0 && jstr(a.extraJvm) === jstr(["-XX:+UseZGC"]), "A 只发参数（内存给 0 = 不改）：" + jstr(a));
        await pb.$eval("#jm-mem", el => { el.value = String(+el.value - 1024); el.dispatchEvent(new Event("input", { bubbles: true })); });
        const want = +(await pb.$eval("#jm-mem", el => el.value));
        await saveIn(pb);
        const b = last("b", "/settings");
        assert(b && b.xmxMb === want && !("extraJvm" in b), "B 只发内存：" + jstr(b));
        const bToasts = await pb.$$eval(".toast", els => els.map(e => e.textContent).join(" | "));
        assert(bToasts.includes("已保存内存与 JVM 参数") && !bToasts.includes("补回"), "只改内存时提示不应误报自动补回：" + bToasts);
        const i = (await mockSet({})).instances.find(x => x.name === PK);
        assert(jstr(i.extraJvm) === jstr(["-XX:+UseZGC"]) && i.xmxMb === want, "两边改的都在：" + jstr({ jvm: i.extraJvm, xmx: i.xmxMb }));
        assert((await pb.inputValue("#jm-jvm")) === "-XX:+UseG1GC", "只改内存时不回读，B 没动的参数框不被换掉：" + await pb.inputValue("#jm-jvm"));
      });
    } finally { await mockSet({ instReset: true }); }
  });

  await run("两个窗口：定时任务——一边加了一条，另一边（表还是旧的）删一条、加一条、关掉崩溃重启，只按条发，一边加的那条还在", async () => {
    const SV = "生存服 · 朋友";
    await mockSet({ instReset: true });
    try {
      await twoWin("#/inst/" + encodeURIComponent(SV) + "/tasks", "#sched-list .java-row", async (pa, pb, last) => {
        await pa.selectOption("#sc-type", "backup"); await pa.fill("#sc-at", "05:00"); await pa.click("#sc-add");
        await saveIn(pa);
        assert(jstr(last("a", "/policies")) === jstr({ addSchedules: [{ type: "backup", at: "05:00", args: "" }] }), "A 只发加的那条：" + jstr(last("a", "/policies")));
        // B 打开得早，表里没有 05:00 那条：关掉崩溃重启、删掉 04:00 的定时重启、加一条定时命令
        await pb.click("#pol-crash");
        await pb.click('#sched-list [data-i="0"]');
        await pb.selectOption("#sc-type", "command"); await pb.fill("#sc-at", "21:30"); await pb.fill("#sc-args", "say 晚安"); await pb.click("#sc-add");
        await saveIn(pb);
        const b = last("b", "/policies");
        assert(b && b.crashRestart === false && !("schedules" in b) && jstr(b.removeSchedules) === jstr([{ type: "restart", at: "04:00", args: "" }]) &&
          jstr(b.addSchedules) === jstr([{ type: "command", at: "21:30", args: "say 晚安" }]), "B 只按条发：" + jstr(b));
        const pol = await apiGet("/api/instances/" + encodeURIComponent(SV) + "/policies");
        const keys = pol.schedules.map(s => s.type + " " + s.at).sort();
        assert(pol.crashRestart === false && jstr(keys) === jstr(["backup 05:00", "command 20:00", "command 21:30"]), "A 加的还在、B 删的删了：" + jstr(pol));
        await pb.reload(); await pb.waitForSelector("#sched-list .java-row"); await pb.waitForTimeout(300);
        assert((await pb.$$eval("#sched-list .java-row", r => r.length)) === 3, "B 刷新以后看得到三条");
      });
    } finally { await mockSet({ instReset: true }); }
  });

  // ==================== 定时任务：每隔多久、文件夹、一键添加（2026-11-27 用户反馈：想每小时存档、每小时扫地，扫地和存档各放一个文件夹）====================
  const SCHED_SV = "生存服 · 朋友", SCHED_API = "/api/instances/" + encodeURIComponent(SCHED_SV) + "/policies";
  const schedPut = body => page.evaluate(([p, b]) => fetch(p, { method: "PUT", headers: { "X-Token": "mock", "Content-Type": "application/json" }, body: JSON.stringify(b) }).then(r => r.ok), [SCHED_API, body]);
  // 列表现在的样子：不放文件夹的、各个文件夹里的（文件夹名 → 每行「时间 命令 [未保存]」）
  const schedView = p => p.evaluate(() => {
    const rowText = r => [r.querySelector(".jv").textContent, r.querySelector(".jp").textContent, r.classList.contains("draft") ? "未保存" : ""].filter(Boolean).join(" ");
    const loose = [...document.querySelectorAll("#sched-list > .java-row")].map(rowText);
    const folders = Object.fromEntries([...document.querySelectorAll('#sched-list > [role="group"]')].map(g => [g.getAttribute("aria-label"), [...g.querySelectorAll(".java-row")].map(rowText)]));
    return { loose, folders, bar: document.querySelector(".sb-text").textContent };
  });
  const menuPick = async (p, label, item) => { await p.click(`[aria-label="移到文件夹 ${label}"]`); await p.click(`.dropmenu button:has-text("${item}")`); await p.waitForTimeout(150); };
  const newFolderIn = async (p, label, g) => {
    await menuPick(p, label, "新文件夹…");
    await p.waitForSelector(".modal #sc-new-group");
    await p.fill(".modal #sc-new-group", g); await p.click('.modal [data-x="ok"]'); await p.waitForTimeout(250);
  };

  await run("定时任务：一键添加「每小时存档」「每小时扫地」，各进「存档」「扫地」文件夹；同一条已经有了不再加；悬停写清只留几份、旧的会删；保存只发这两条（带 every、group），刷新以后照样分文件夹", async () => {
    await mockSet({ instReset: true });
    try {
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      const tip = await page.getAttribute("#sc-pre-backup", "title");
      assert(tip.includes("保留最近 10 份") && tip.includes("删掉最旧的") && tip.includes("「存档」") && tip.includes("停着时不备份"), "每小时存档的悬停：" + tip);
      assert((await page.getAttribute("#sc-pre-sweep", "title")).includes("kill @e[type=item]"), "每小时扫地的悬停应写出命令");
      const since = reqs.length;
      await page.click("#sc-pre-backup"); await page.click("#sc-pre-sweep"); await settle(150);
      let v = await schedView(page);
      assert(v.bar === "2 项修改未保存" && v.loose.length === 2 && jstr(v.folders) === jstr({ "文件夹 存档": ["每隔 1 小时 未保存"], "文件夹 扫地": ["每隔 1 小时 kill @e[type=item] 未保存"] }),
        "两条进各自的文件夹、标未保存：" + jstr(v));
      const ax = await axProblems();
      assert(!ax.length, "有文件夹时的无障碍：" + ax.join("、"));
      await page.click("#sc-pre-backup"); await settle(100);
      assert(await toastIn(page, "已经有每小时存档了（在「存档」文件夹里）"), "再点一次应提示已经有了");
      assert((await schedView(page)).bar === "2 项修改未保存", "再点不该多加一条");
      await page.click('[data-sb="save"]'); await page.waitForFunction(() => document.querySelector(".sb-text").textContent === "所有修改已保存");
      const body = lastPut(since, "/policies");
      assert(jstr(body) === jstr({ addSchedules: [{ type: "backup", every: 60, args: "", group: "存档" }, { type: "command", every: 60, args: "kill @e[type=item]", group: "扫地" }] }), "只发加的两条：" + jstr(body));
      const pol = await apiGet(SCHED_API);
      assert(pol.schedules.length === 4 && pol.schedules.filter(s => s.every === 60 && !("at" in s)).length === 2, "后端存的：" + jstr(pol));
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      v = await schedView(page);
      assert(jstr(v.loose) === jstr(["每天 04:00", "每天 20:00 say 今晚 8 点打龙集合！"]) && jstr(v.folders) === jstr({ "文件夹 存档": ["每隔 1 小时"], "文件夹 扫地": ["每隔 1 小时 kill @e[type=item]"] }),
        "刷新以后：" + jstr(v));
      const slot = await page.$eval('#sched-list [aria-label="文件夹 存档"] .jv', e => e.title);
      assert(slot.includes("00:00、01:00、02:00") && slot.includes("停着时不备份"), "每隔多久的悬停写出几点执行：" + slot);
      // 已经存下的也认：刷新以后再点，照样不加
      await page.click("#sc-pre-sweep"); await settle(100);
      assert(await toastIn(page, "已经有每小时扫地了") && (await schedView(page)).bar === "所有修改已保存", "存下的也算已经有了");
    } finally { await mockSet({ instReset: true }); }
  });

  await run("定时任务：选「每隔」换成间隔和单位（快捷时间收起），最短、最长就近提示；同一个文件夹里每天定时和每隔多久都有；挪文件夹只算一项，保存时删原处、加新处；文件夹挪空了就没了", async () => {
    await mockSet({ instReset: true });
    try {
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      assert(!(await page.isVisible("#sc-every")) && await page.isVisible("#sc-at"), "默认是「每天」");
      await page.selectOption("#sc-mode", "every"); await page.selectOption("#sc-type", "backup");
      assert(await page.isVisible("#sc-every") && await page.isVisible("#sc-unit") && !(await page.isVisible("#sc-at")) && !(await page.isVisible('[data-later="10"]')), "「每隔」：间隔和单位，没有时刻和快捷时间");
      const hint = await page.textContent("#sc-hint");
      assert(hint.includes("每个整点") && hint.includes("只留最近 10 份"), "每隔多久的备份提示：" + hint);
      await page.fill("#sc-every", "10"); await page.selectOption("#sc-unit", "1"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("定时备份最短每隔 30 分钟") && await page.$("#sc-every.invalid"), "太短：" + await page.textContent("#sc-err"));
      await page.fill("#sc-every", "13"); await page.selectOption("#sc-unit", "60"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("最长 12 小时"), "太长：" + await page.textContent("#sc-err"));
      await page.fill("#sc-every", "abc"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("整数"), "不是数字：" + await page.textContent("#sc-err"));
      assert((await schedView(page)).bar === "所有修改已保存", "写错的都不该加进去");
      await page.fill("#sc-every", "30"); await page.selectOption("#sc-unit", "1"); await page.fill("#sc-group", "存档"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")) === "" && !(await page.$("#sc-every.invalid")), "加上以后错误消失");
      await page.selectOption("#sc-mode", "at"); await page.fill("#sc-at", "3:00"); await page.click("#sc-add"); // 文件夹框还是「存档」
      let v = await schedView(page);
      assert(jstr(v.folders) === jstr({ "文件夹 存档": ["每隔 30 分钟 未保存", "每天 03:00 未保存"] }) && v.bar === "2 项修改未保存", "同一个文件夹里两种都有：" + jstr(v));
      let since = reqs.length;
      await saveIn(page);
      assert(jstr(lastPut(since, "/policies")) === jstr({ addSchedules: [{ type: "backup", every: 30, args: "", group: "存档" }, { type: "backup", at: "03:00", args: "", group: "存档" }] }), "发的：" + jstr(lastPut(since, "/policies")));
      // 挪进新文件夹「备份」：只算一项；保存发删掉「存档」里那条、在「备份」里加一条
      await newFolderIn(page, "定时备份 每隔 30 分钟（存档）", "备份");
      v = await schedView(page);
      // 文件夹按任务出现的先后排：挪过去的那条在前面，「备份」就排在「存档」前面
      assert(v.bar === "1 项修改未保存" && jstr(v.folders) === jstr({ "文件夹 备份": ["每隔 30 分钟 未保存"], "文件夹 存档": ["每天 03:00"] }), "挪进新文件夹：" + jstr(v));
      since = reqs.length;
      await saveIn(page);
      assert(jstr(lastPut(since, "/policies")) === jstr({ addSchedules: [{ type: "backup", every: 30, args: "", group: "备份" }], removeSchedules: [{ type: "backup", every: 30, args: "", group: "存档" }] }),
        "挪文件夹发的：" + jstr(lastPut(since, "/policies")));
      // 「存档」里剩的那条挪出来（不放文件夹）：「存档」就没了
      await menuPick(page, "定时备份 每天 03:00（存档）", "不放文件夹");
      v = await schedView(page);
      assert(!("文件夹 存档" in v.folders) && v.loose.includes("每天 03:00 未保存"), "挪空的文件夹应消失：" + jstr(v));
      since = reqs.length;
      await saveIn(page);
      assert(jstr(lastPut(since, "/policies")) === jstr({ addSchedules: [{ type: "backup", at: "03:00", args: "" }], removeSchedules: [{ type: "backup", at: "03:00", args: "", group: "存档" }] }),
        "挪出文件夹发的：" + jstr(lastPut(since, "/policies")));
      const pol = await apiGet(SCHED_API);
      assert(jstr(pol.schedules) === jstr([{ type: "restart", at: "04:00", args: "" }, { type: "command", at: "20:00", args: "say 今晚 8 点打龙集合！" },
        { type: "backup", every: 30, args: "", group: "备份" }, { type: "backup", at: "03:00", args: "" }]), "后端最后的样子：" + jstr(pol.schedules));
      // 挪到已有的文件夹：菜单里直接列出来
      await menuPick(page, "定时重启 每天 04:00", "备份");
      assert(jstr((await schedView(page)).folders) === jstr({ "文件夹 备份": ["每天 04:00 未保存", "每隔 30 分钟"] }), "挪进已有的文件夹：" + jstr((await schedView(page)).folders));
      await page.click('[data-sb="discard"]'); await settle(300);
    } finally { await mockSet({ instReset: true }); }
  });

  await run("两个窗口：定时任务——一边把每小时存档挪进别的文件夹，另一边（还是旧的）删掉它、加上每小时扫地：删的找不到不算错，挪过去的那条还在，扫地也加上了", async () => {
    await mockSet({ instReset: true });
    try {
      assert(await schedPut({ addSchedules: [{ type: "backup", every: 60, group: "存档" }] }), "先摆一条每小时存档");
      await twoWin("#/inst/" + encodeURIComponent(SCHED_SV) + "/tasks", "#sched-list .java-row", async (pa, pb, last) => {
        await newFolderIn(pa, "定时备份 每隔 1 小时（存档）", "备份");
        await saveIn(pa);
        assert(jstr(last("a", "/policies")) === jstr({ addSchedules: [{ type: "backup", every: 60, args: "", group: "备份" }], removeSchedules: [{ type: "backup", every: 60, args: "", group: "存档" }] }),
          "A 发删原处、加新处：" + jstr(last("a", "/policies")));
        await pb.click('[aria-label="移除 定时备份 每隔 1 小时（存档）"]');
        await pb.click("#sc-pre-sweep");
        await saveIn(pb);
        assert(jstr(last("b", "/policies")) === jstr({ addSchedules: [{ type: "command", every: 60, args: "kill @e[type=item]", group: "扫地" }], removeSchedules: [{ type: "backup", every: 60, args: "", group: "存档" }] }),
          "B 只按条发：" + jstr(last("b", "/policies")));
        const pol = await apiGet(SCHED_API);
        const got = pol.schedules.filter(s => s.every).map(s => s.type + " " + s.every + " " + (s.group || "")).sort();
        assert(jstr(got) === jstr(["backup 60 备份", "command 60 扫地"]), "A 挪过去的还在、B 加的也在：" + jstr(pol.schedules));
      });
    } finally { await mockSet({ instReset: true }); }
  });

  await run("定时任务：命令框只对定时命令可用（换成备份不带上命令文字）；文件夹名中间有控制字符、超过 32 个字，添加时就地拦下；一样的任务（不管在哪个文件夹）不能再加；每隔 8 小时的悬停列全三次、不加省略号", async () => {
    await mockSet({ instReset: true });
    try {
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      assert(await page.$eval("#sc-args", e => e.disabled), "默认是定时重启，命令框应灰掉");
      await page.selectOption("#sc-type", "command");
      assert(!(await page.$eval("#sc-args", e => e.disabled)), "定时命令时命令框能填");
      await page.fill("#sc-args", "say 不该带上");
      await page.selectOption("#sc-type", "backup");
      assert(await page.$eval("#sc-args", e => e.disabled), "换成备份，命令框又灰掉");
      await page.selectOption("#sc-mode", "every");
      const bar0 = (await schedView(page)).bar;
      await page.fill("#sc-group", "存\t档"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("文件夹名不能有换行之类的控制字符") && await page.$("#sc-group.invalid"), "中间的 Tab：" + await page.textContent("#sc-err"));
      await page.$eval("#sc-group", e => { e.value = "档".repeat(33); e.dispatchEvent(new Event("input", { bubbles: true })); }); // 绕过 maxlength（粘贴、脚本）
      assert(!(await page.$("#sc-group.invalid")), "改了文件夹名，红框先去掉");
      await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("文件夹名最多 32 个字"), "超过 32 个字：" + await page.textContent("#sc-err"));
      assert((await schedView(page)).bar === bar0, "不合格的文件夹名不该加进去");
      await page.fill("#sc-group", "存档"); await page.click("#sc-add");
      let v = await schedView(page);
      assert(jstr(v.folders) === jstr({ "文件夹 存档": ["每隔 1 小时 未保存"] }), "备份不带命令文字：" + jstr(v));
      // 一样的任务：手动再加（换个文件夹也不行）、一键添加都不加
      await page.fill("#sc-group", "别的"); await page.click("#sc-add");
      assert((await page.textContent("#sc-err")).includes("已经有一样的定时任务了（在「存档」文件夹里）"), "手动再加一样的：" + await page.textContent("#sc-err"));
      await page.click("#sc-pre-backup");
      assert(await toastIn(page, "已经有每小时存档了（在「存档」文件夹里）"), "手动加的也算已经有了");
      assert((await schedView(page)).bar === "1 项修改未保存", "一样的都没加进去");
      // 每隔 8 小时：一天就三次，悬停里列全、不加省略号；每隔 1 小时的照旧有省略号
      await page.selectOption("#sc-type", "restart"); await page.fill("#sc-every", "8"); await page.fill("#sc-group", ""); await page.click("#sc-add");
      const tip8 = await page.evaluate(() => [...document.querySelectorAll("#sched-list > .java-row")].pop().querySelector(".jv").title);
      assert(tip8 === "从每天 0 点起算：00:00、08:00、16:00；服务器没开时不执行", "每隔 8 小时的悬停：" + tip8);
      const tip1 = await page.$eval('#sched-list [aria-label="文件夹 存档"] .jv', e => e.title);
      assert(tip1.includes("00:00、01:00、02:00……"), "每隔 1 小时的悬停：" + tip1);
      const since = reqs.length;
      await saveIn(page);
      assert(jstr(lastPut(since, "/policies")) === jstr({ addSchedules: [{ type: "backup", every: 60, args: "", group: "存档" }, { type: "restart", every: 480, args: "" }] }), "发的：" + jstr(lastPut(since, "/policies")));
    } finally { await mockSet({ instReset: true }); }
  });

  await run("定时任务：新文件夹的名字就地检查（空的、中间有控制字符、超过 32 个字都不关弹窗）；文件夹名和命令里的 < & \" 原样显示（菜单、弹窗、列表都不当成 HTML）", async () => {
    await mockSet({ instReset: true });
    try {
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      await page.selectOption("#sc-type", "command"); await page.fill("#sc-at", "21:00"); await page.fill("#sc-args", `say <b>&"hi"`); await page.click("#sc-add");
      const label = `定时命令 每天 21:00 say <b>&"hi"`; // 带双引号，写不进属性选择器：按无障碍名字找
      await page.getByRole("button", { name: "移到文件夹 " + label, exact: true }).click();
      await page.click('.dropmenu button:has-text("新文件夹…")');
      await page.waitForSelector(".modal #sc-new-group");
      const bodyText = await page.$eval(".modal .m-body", e => e.textContent);
      assert(bodyText === label, "弹窗里原样显示任务（不该是 &amp;）：" + bodyText);
      const tryName = async (val, want) => {
        await page.$eval(".modal #sc-new-group", (e, x) => { e.value = x; e.dispatchEvent(new Event("input", { bubbles: true })); }, val);
        await page.click('.modal [data-x="ok"]'); await settle(150);
        const err = await page.$eval(".modal #sc-new-group-err", e => e.textContent).catch(() => "（弹窗已关）");
        assert(err.includes(want) && await page.$(".modal #sc-new-group.invalid"), `「${val}」应就地报「${want}」：${err}`);
      };
      await tryName("", "写个文件夹名");
      await tryName("a\u0001b", "控制字符");
      await tryName("档".repeat(33), "最多 32 个字");
      const evil = `<img src=x onerror=__xss=1>&"夹`; // 28 个字，放得进 32 个字的框
      await page.fill(".modal #sc-new-group", evil);
      assert(!(await page.$(".modal #sc-new-group.invalid")), "改了名字，红框先去掉");
      await page.press(".modal #sc-new-group", "Enter"); await settle(250);
      assert(!(await page.$(".modal")), "合格的名字回车就放进去");
      const v = await schedView(page);
      assert(jstr(v.folders) === jstr({ ["文件夹 " + evil]: [`每天 21:00 say <b>&"hi" 未保存`] }), "列表里原样显示：" + jstr(v));
      assert(await page.$eval("#sched-list", e => !e.querySelector("img") && [...e.querySelectorAll(".jp")].every(x => !x.children.length)), "列表里不该冒出 img，命令里的 <b> 也不该成了元素");
      await page.click('[aria-label="移到文件夹 定时重启 每天 04:00"]'); await settle(150);
      const items = await page.$$eval(".dropmenu button", bs => bs.map(b => b.textContent));
      assert(items.includes(evil) && await page.$eval(".dropmenu", m => !m.querySelector("img")), "菜单里原样显示文件夹名：" + jstr(items));
      await page.keyboard.press("Escape"); await settle(150);
      assert(!(await page.evaluate(() => window.__xss)), "文件夹名不该被当成 HTML 执行");
      await page.click('[data-sb="discard"]'); await settle(300);
    } finally { await mockSet({ instReset: true }); }
  });

  await run("定时任务：已经有两条一样的在不同文件夹（AI、老配置留下的），删掉一条只算一项、另一条不标未保存、保存只删那一条；删了又原样加回来不算修改", async () => {
    await mockSet({ instReset: true });
    try {
      assert(await schedPut({ addSchedules: [{ type: "backup", every: 60, group: "存档" }, { type: "backup", every: 60, group: "备份" }] }), "先摆两条一样的");
      await page.goto(url(`#/inst/${inst}/tasks`)); await settle();
      await page.click('[aria-label="移除 定时备份 每隔 1 小时（存档）"]'); await settle(100);
      let v = await schedView(page);
      assert(v.bar === "1 项修改未保存" && jstr(v.folders) === jstr({ "文件夹 备份": ["每隔 1 小时"] }), "删一条只算一项，另一条没动：" + jstr(v));
      const since = reqs.length;
      await saveIn(page);
      assert(jstr(lastPut(since, "/policies")) === jstr({ removeSchedules: [{ type: "backup", every: 60, args: "", group: "存档" }] }), "只删那一条：" + jstr(lastPut(since, "/policies")));
      const pol = await apiGet(SCHED_API);
      assert(jstr(pol.schedules.filter(s => s.every).map(s => s.group)) === jstr(["备份"]), "后端只剩「备份」里那条：" + jstr(pol.schedules));
      // 删掉定时重启 04:00、又原样加回来：不算修改
      await page.click('[aria-label="移除 定时重启 每天 04:00"]');
      assert((await schedView(page)).bar === "1 项修改未保存", "删了算一项");
      await page.fill("#sc-at", "04:00"); await page.click("#sc-add"); await settle(100);
      v = await schedView(page);
      assert(v.bar === "所有修改已保存" && v.loose.includes("每天 04:00"), "原样加回来不算修改：" + jstr(v));
    } finally { await mockSet({ instReset: true }); }
  });

  await run("两个窗口：AI 服务商——一边存了新模型，另一边（还是旧设置）再存时被拦下、请它重新读取；读回来是另一边存的，再存就能存", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-two-win" });
    try {
      await twoWin("#/settings/ai", "#ai-model", async (pa, pb, last) => {
        await pa.fill("#ai-model", "deepseek-v4-pro");
        await saveIn(pa);
        const a = last("a", "/api/ai/provider");
        assert(a && a.model === "deepseek-v4-pro" && typeof a.rev === "string" && a.rev.length > 0, "A 带着设置的版本存：" + jstr(a));
        await pb.fill("#ai-model", "deepseek-reasoner");
        await pb.click('.save-bar.sticky [data-sb="save"]');
        await pb.waitForSelector(".modal");
        const m = await pb.textContent(".modal");
        assert(m.includes("AI 设置在别的窗口改过了") && m.includes("这次没有保存"), "B 被拦下要说清：" + m);
        assert(last("b", "/api/ai/provider").rev === a.rev, "B 带的是打开时的版本：" + jstr(last("b", "/api/ai/provider")));
        assert((await mockSet({})).ai.model === "deepseek-v4-pro", "A 存的模型没被改回");
        await pb.click('.modal [data-x="ok"]'); await pb.waitForTimeout(800);
        assert((await pb.inputValue("#ai-model")) === "deepseek-v4-pro", "重新读取以后是 A 存的：" + await pb.inputValue("#ai-model"));
        await pb.fill("#ai-model", "deepseek-reasoner");
        await saveIn(pb);
        assert((await mockSet({})).ai.model === "deepseek-reasoner", "读回来以后再存就能存");
      });
    } finally { await aiState({ reset: true }); }
  });

  await run("两个窗口：AI 自填单价——一边加了一条，另一边（表还是旧的）删掉另一条，只按条发，一边加的那条还在", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-two-win", prices: [{ model: "old-model", currency: "CNY", input: 1, output: 2 }] });
    try {
      await twoWin("#/settings/ai", "#cp-add", async (pa, pb, last) => {
        await pa.fill("#cp-model", "new-model"); await pa.fill("#cp-in", "3"); await pa.fill("#cp-out", "4");
        await pa.click("#cp-add");
        assert(await toastIn(pa, "已添加自填单价"), "A 加上");
        assert(jstr(last("a", "/api/ai/prices")) === jstr({ addPrices: [{ model: "new-model", currency: "CNY", input: 3, output: 4 }] }), "A 只发加的那条：" + jstr(last("a", "/api/ai/prices")));
        await pb.click('[data-del-price="0"]');
        assert(await toastIn(pb, "已删除这条自填单价"), "B 删掉");
        assert(jstr(last("b", "/api/ai/prices")) === jstr({ removePrices: ["old-model"] }), "B 只发删的那条：" + jstr(last("b", "/api/ai/prices")));
        assert(jstr((await mockSet({})).ai.prices.map(c => c.model)) === jstr(["new-model"]), "A 加的还在：" + jstr((await mockSet({})).ai.prices));
        assert((await pb.textContent("#ai-prices")).includes("new-model"), "B 按回来的整份设置重画，看得到 A 加的");
      });
    } finally { await aiState({ reset: true }); }
  });

  await run("两个窗口：隧道的编辑和清除凭据——只发改了的几项，另一边刚改的绑定和跟随启动不被弹窗里的旧值改回", async () => {
    await mockSet({ tunnelsReset: true });
    try {
      await twoWin("#/tunnels", "#tun-list [data-tun]", async (pa, pb, last) => {
        // B 先打开 t2 的编辑弹窗（照这一刻的列表填），A 再把 t2 绑上服务器、打开跟随启动
        await pb.click('[data-tedit="t2"]'); await pb.waitForSelector("#tn-ed-name");
        await pa.click('[data-tedit="t2"]'); await pa.waitForSelector("#tn-ed-name");
        await pa.selectOption("#tn-ed-bound", "机械动力整合包"); await pa.check("#tn-ed-auto");
        await pa.click('#modal-root [data-x="ok"]');
        assert(await toastIn(pa, "的修改"), "A 存下");
        assert(jstr(last("a", "/api/tunnels/t2")) === jstr({ boundInstance: "机械动力整合包", autoStart: true }), "A 只发改了的两项：" + jstr(last("a", "/api/tunnels/t2")));
        await pb.fill("#tn-ed-name", "B 改的名字");
        await pb.click('#modal-root [data-x="ok"]');
        assert(await toastIn(pb, "已保存「B 改的名字」的修改"), "B 存下");
        assert(jstr(last("b", "/api/tunnels/t2")) === jstr({ name: "B 改的名字" }), "B 只发名字：" + jstr(last("b", "/api/tunnels/t2")));
        let t2 = (await mockSet({})).tunnels.find(t => t.id === "t2");
        assert(t2.name === "B 改的名字" && t2.boundInstance === "机械动力整合包" && t2.autoStart === true && t2.hasCredential === true, "两边改的都在：" + jstr(t2));
        // 清除凭据：只发 clearCredential
        await pb.click('[data-tclear="t2"]'); await pb.waitForSelector(".modal");
        await pb.click('.modal [data-x="ok"]');
        assert(await toastIn(pb, "保存的凭据"), "B 清掉凭据");
        assert(jstr(last("b", "/api/tunnels/t2")) === jstr({ clearCredential: true }), "清除凭据只发这一项：" + jstr(last("b", "/api/tunnels/t2")));
        t2 = (await mockSet({})).tunnels.find(t => t.id === "t2");
        assert(!t2.hasCredential && t2.name === "B 改的名字" && t2.boundInstance === "机械动力整合包" && t2.autoStart === true, "清凭据不动别的：" + jstr(t2));
      });
    } finally { await mockSet({ tunnelsReset: true }); }
  });

  await run("两个窗口：伙伴禁挖区域——按条增删；一边加的区域，另一边删别的不会把它删掉；另一边弹窗里没看到的重名由后端拦下；编辑是删掉原名再加", async () => {
    const zone = (name, x) => ({ name, dim: "minecraft:overworld", min: [x, -64, 0], max: [x + 10, 320, 10] });
    const zoneNames = async () => (await apiGet("/api/instances/" + encodeURIComponent(PACK) + "/companion")).settings.zones.map(z => z.name);
    await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true, zones: [zone("base", 0)] } } } } });
    try {
      await twoWin("#/inst/" + encodeURIComponent(PACK) + "/companion", "#cp-zone-add", async (pa, pb, last) => {
        const fillZone = async (p, name, x) => {
          await p.fill("#cz-name", name);
          for (const [k, val] of [["ax", x], ["ay", -64], ["az", 0], ["bx", x + 10], ["by", 320], ["bz", 10]]) await p.fill("#cz-" + k, String(val));
          await p.click('.modal [data-x="ok"]');
        };
        // B 先打开「添加」弹窗（重名检查只看这一刻的列表），A 加上 farm
        await pb.click("#cp-zone-add"); await pb.waitForSelector("#cz-name");
        await pa.click("#cp-zone-add"); await pa.waitForSelector("#cz-name");
        await fillZone(pa, "farm", 100);
        assert(await toastIn(pa, "已添加禁挖区域「farm」"), "A 加上 farm");
        assert(jstr(last("a", "/companion/settings")) === jstr({ addZones: [zone("farm", 100)] }), "A 只发加的：" + jstr(last("a", "/companion/settings")));
        // B 加 Farm（大写）：弹窗放行（这一页没看到 farm），后端不分大小写拦下（说的是已经有的 farm），什么都不改
        await fillZone(pb, "Farm", 200);
        assert(await toastIn(pb, "已经有一个叫「farm」的禁挖区域了"), "重名由后端拦下");
        assert(jstr(await zoneNames()) === jstr(["base", "farm"]), "被拦下时什么都没改：" + jstr(await zoneNames()));
        // B 删 base：只发删的，A 加的 farm 还在
        await pb.click('[data-zdel="base"]'); await pb.waitForSelector(".modal");
        await pb.click('.modal [data-x="ok"]');
        assert(await toastIn(pb, "已删除区域「base」"), "B 删掉 base");
        assert(jstr(last("b", "/companion/settings")) === jstr({ removeZones: ["base"] }), "B 只发删的：" + jstr(last("b", "/companion/settings")));
        assert(jstr(await zoneNames()) === jstr(["farm"]), "A 加的 farm 还在：" + jstr(await zoneNames()));
        // 编辑（B 的列表按回来的整份重画了）：farm 改名 orchard = 删掉 farm、加上 orchard
        await pb.click('[data-zedit="farm"]'); await pb.waitForSelector("#cz-name");
        await pb.fill("#cz-name", "orchard"); await pb.click('.modal [data-x="ok"]');
        assert(await toastIn(pb, "已保存区域「orchard」"), "B 改名");
        assert(jstr(last("b", "/companion/settings")) === jstr({ removeZones: ["farm"], addZones: [zone("orchard", 100)] }), "编辑：" + jstr(last("b", "/companion/settings")));
        assert(jstr(await zoneNames()) === jstr(["orchard"]), "改名以后：" + jstr(await zoneNames()));
      });
    } finally { await resetCP(); }
  });

  await run("两个窗口：游戏内 AI 的执行权限名单——一边收回了谁，另一边（名单还是旧的）给别人授权，只按名字发，收回的人不会被授权回来", async () => {
    const SV = "生存服 · 朋友";
    await gcState({ ...gcReady, gc: { reset: true, inst: { [SV]: { settings: { allowed: ["Alex", "Steve"] } } } } });
    try {
      await twoWin("#/inst/" + encodeURIComponent(SV) + "/gameai", '[data-allow="Steve"]', async (pa, pb, last) => {
        await pa.click('[data-allow="Steve"]');
        assert(await toastIn(pa, "已收回 Steve 的执行权限"), "A 收回 Steve");
        assert(jstr(last("a", "/gameai")) === jstr({ removeAllowed: ["Steve"] }), "A 只发收回的：" + jstr(last("a", "/gameai")));
        await pb.fill("#gc-add-name", "Bob_2"); await pb.click("#gc-add");
        assert(await toastIn(pb, "已允许 Bob_2 让 AI 执行命令"), "B 授权 Bob_2");
        assert(jstr(last("b", "/gameai")) === jstr({ addAllowed: ["Bob_2"] }), "B 只发授权的：" + jstr(last("b", "/gameai")));
        const allowed = (await apiGet("/api/instances/" + encodeURIComponent(SV) + "/gameai")).settings.allowed;
        assert(jstr(allowed) === jstr(["Alex", "Bob_2"]), "Steve 没被授权回来：" + jstr(allowed));
      });
    } finally { await resetGC(); }
  });

  await run("两个窗口：工具箱「导出 Modrinth 整合包」的草稿——自动保存只发改了的字段，一边改的名称、另一边改的简介都在", async () => {
    await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa, pb, last) => {
        await pa.fill("#mp-name", "A 改的名字"); await pa.waitForTimeout(700);
        assert(jstr(last("a", "/api/toolbox/mrpack/draft")) === jstr({ name: "A 改的名字" }), "A 只发名称：" + jstr(last("a", "/api/toolbox/mrpack/draft")));
        await pb.fill("#mp-summary", "B 写的简介"); await pb.waitForTimeout(700);
        assert(jstr(last("b", "/api/toolbox/mrpack/draft")) === jstr({ summary: "B 写的简介" }), "B 只发简介，不带页面上旧的名称：" + jstr(last("b", "/api/toolbox/mrpack/draft")));
        const d = (await mockSet({})).toolbox.draft;
        assert(d.name === "A 改的名字" && d.summary === "B 写的简介" && d.versionId === "1.0.0", "两边改的都在：" + jstr(d));
      });
    } finally { await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口：工具箱「导出 Modrinth 整合包」的草稿——同一页里第一次自动保存还没回来时又改回原样，回来以后补发，存着的和表单上的一样（Codex 复审 2026-11-17）", async () => {
    await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa, pb, last) => {
        const orig = await pa.inputValue("#mp-name");
        let release, n = 0;
        const gate = new Promise(r => { release = r; });
        await pa.route("**/api/toolbox/mrpack/draft", async route => { // 只扣住第一次 PUT，放行以前它不回来
          if (route.request().method() === "PUT" && n++ === 0) await gate;
          await route.continue();
        });
        await pa.fill("#mp-name", "改了又改回去"); await pa.waitForTimeout(600);
        assert(jstr(last("a", "/api/toolbox/mrpack/draft")) === jstr({ name: "改了又改回去" }), "第一次发出去了：" + jstr(last("a", "/api/toolbox/mrpack/draft")));
        await pa.fill("#mp-name", orig); await pa.waitForTimeout(600); // 第二次计时到了，第一次还没回来
        release(); await pa.waitForTimeout(900);
        assert(jstr(last("a", "/api/toolbox/mrpack/draft")) === jstr({ name: orig }), "回来以后补发改回去的名称：" + jstr(last("a", "/api/toolbox/mrpack/draft")));
        const d = (await mockSet({})).toolbox.draft;
        assert(d.name === orig && await pa.inputValue("#mp-name") === orig, "存着的应和表单一样：" + jstr(d));
      });
    } finally { await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口：工具箱「导出 Modrinth 整合包」的草稿——第一次自动保存后端存下了、响应还没回来，又改了简介，这时整页刷新，排着的简介也存上（Codex 复审 2026-11-17 第二轮）", async () => {
    await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
    let release = () => {};
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa) => {
        let n = 0;
        const gate = new Promise(r => { release = r; });
        await pa.route("**/api/toolbox/mrpack/draft", async route => {
          if (route.request().method() === "PUT" && n++ === 0) { // 第一次：先送到后端存下，回给页面的那一下扣住
            const resp = await route.fetch();
            await gate;
            try { await route.fulfill({ response: resp }); } catch { /* 页面已经刷新了 */ }
            return;
          }
          await route.continue();
        });
        await pa.fill("#mp-name", "刷新前改的名字"); await pa.waitForTimeout(600);
        await pa.fill("#mp-summary", "刷新前写的简介"); await pa.waitForTimeout(600); // 计时到了，第一次还没回来：排着
        await pa.reload(); await pa.waitForSelector("#mp-name"); await pa.waitForTimeout(600);
        const d = (await mockSet({})).toolbox.draft;
        assert(d.name === "刷新前改的名字" && d.summary === "刷新前写的简介", "刷新以前改的两样都应存上：" + jstr(d));
      });
    } finally { release(); await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口：工具箱整合包草稿（A65-1）——乱序到达时不拿旧序号把新值改回", async () => {
    await mockSet({
      toolbox: { reset: true }, modCart: { reset: true }, local: true,
      holdReq: { method: "PUT", path: "/api/toolbox/mrpack/draft", body: { name: "较早发出的旧名" }, flushHeaders: true },
    });
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa) => {
        await pa.fill("#mp-name", "较早发出的旧名");
        await waitHeld(1); // 先发出的请求已经抵达仿真后端并被扣住
        await pa.fill("#mp-name", "后来改的新名"); // 还在防抖，立刻刷新由 pagehide 送出新名（先落盘）
        await pa.reload(); await pa.waitForSelector("#mp-name");
        assert(await until(async () => (await mockSet({})).toolbox.draft.name === "后来改的新名"), "刷新时送出的新名应先落盘");
        const d = (await mockSet({ releaseHold: true })).toolbox.draft;
        assert(d.name === "后来改的新名", "乱序到达的旧请求不能把新值改回：" + jstr(d));
      });
    } finally { await mockSet({ holdReq: null, toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口：工具箱整合包草稿（A65-2）——一边有请求在途时改别的字段并离开页面，不重发未再改的在途字段去盖掉另一窗口的新值", async () => {
    await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
    let rel = () => {};
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa, pb) => {
        let held = false, respHeld = false;
        const gate = new Promise(r => { rel = r; });
        await pa.route("**/api/toolbox/mrpack/draft", async route => {
          if (route.request().method() === "PUT" && !held) {
            held = true;
            const resp = await route.fetch(); // 第一次已落盘，扣住响应让客户端还以为在途
            respHeld = true;
            await gate;
            try { await route.fulfill({ response: resp }); } catch {}
            return;
          }
          await route.continue();
        });
        await pa.fill("#mp-name", "A 的名称");
        assert(await until(async () => respHeld && (await mockSet({})).toolbox.draft.name === "A 的名称"), "A 的第一次应已存上");
        await pb.fill("#mp-name", "B 覆盖的名称");
        assert(await until(async () => (await mockSet({})).toolbox.draft.name === "B 覆盖的名称"), "B 应已保存成功");
        await pa.fill("#mp-summary", "A 补改的简介"); // A 在途，仅改简介
        await pa.reload(); await pa.waitForSelector("#mp-name");
        assert(await until(async () => (await mockSet({})).toolbox.draft.summary === "A 补改的简介"), "A 离开页面补发的简介应已落盘");
        const d = (await mockSet({})).toolbox.draft;
        assert(d.name === "B 覆盖的名称" && d.summary === "A 补改的简介", "A 补发简介时不能重发没变的名称盖掉 B：" + jstr(d));
      });
    } finally { rel(); await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口：工具箱整合包草稿（A65-3）——第一次保存刚回来后在防抖期内马上刷新，新改动的字段仍能送出存上", async () => {
    await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true });
    try {
      await twoWin("#/toolbox/mrpack-export", "#mp-name", async (pa) => {
        const firstDone = pa.waitForResponse(r => r.url().includes("/api/toolbox/mrpack/draft") && r.request().method() === "PUT");
        await pa.fill("#mp-name", "先存好的名称");
        await firstDone; // 确认第一次已完成且已无在途请求
        await pa.fill("#mp-summary", "防抖期里刷新前的简介");
        await pa.reload(); // 不等 260ms 防抖结束立刻刷新
        await pa.waitForSelector("#mp-name");
        assert(await until(async () => (await mockSet({})).toolbox.draft.summary === "防抖期里刷新前的简介"), "防抖期里刷新也应触发 pagehide 送出新改动");
        const d = (await mockSet({})).toolbox.draft;
        assert(d.name === "先存好的名称" && d.summary === "防抖期里刷新前的简介", "防抖期里刷新也应触发 pagehide 送出新改动：" + jstr(d));
      });
    } finally { await mockSet({ toolbox: { reset: true }, modCart: { reset: true }, local: true }); }
  });

  await run("两个窗口（A63-1）：手机远程管理保存访问密码途中接着打字，响应回来后保留当前输入", async () => {
    await mockSet({ listenLan: true, lanSet: true, holdReq: null });
    try {
      await twoWin("#/settings/remote", "#lan-pw", async (pa) => {
        await pa.fill("#lan-pw", "first-pw-1");
        await mockSet({ holdReq: "PUT /api/config" });
        await pa.click("#lan-save");
        await waitHeld(1);
        await pa.focus("#lan-pw");
        await pa.keyboard.press("End");
        await pa.type("#lan-pw", "-more");
        await mockSet({ releaseHold: true });
        assert(await toastIn(pa, "还有未保存的新改动"), "保存途中改过应提示还有新改动");
        assert((await pa.inputValue("#lan-pw")) === "first-pw-1-more", "接着打的字不能被清掉：" + await pa.inputValue("#lan-pw"));
        assert(!(await pa.$eval("#lan-save", e => e.disabled)), "保存按钮应恢复可用");
        await pa.click("#lan-save");
        assert(await toastIn(pa, "访问密码已保存"), "再点保存应成功");
      });
    } finally { await mockSet({ listenLan: false, lanSet: false, holdReq: null }); }
  });

  await run("两个窗口（A63-2）：AI 自填单价添加途中接着改输入框，响应回来后保留当前输入", async () => {
    await aiState({ reset: true, ...AI_DEEPSEEK, key: "sk-two-win", prices: [] });
    await mockSet({ holdReq: null });
    try {
      await twoWin("#/settings/ai", "#cp-add", async (pa) => {
        await pa.fill("#cp-model", "m1"); await pa.fill("#cp-in", "1"); await pa.fill("#cp-out", "2");
        await mockSet({ holdReq: "PUT /api/ai/prices" });
        await pa.click("#cp-add");
        await waitHeld(1);
        await pa.fill("#cp-model", "m2-next");
        await mockSet({ releaseHold: true });
        assert(await toastIn(pa, "已添加自填单价"), "第一次添加完成要有提示");
        assert((await pa.textContent("#ai-prices")).includes("m1"), "已添加的 m1 应在列表里");
        assert((await pa.inputValue("#cp-model")) === "m2-next", "保存途中接着改的型号名不能被清掉：" + await pa.inputValue("#cp-model"));
        assert(!(await pa.$eval("#cp-add", e => e.disabled)), "添加按钮应恢复可用");
      });
    } finally { await mockSet({ holdReq: null }); await aiState({ reset: true }); }
  });

  await run("两个窗口（A63-3）：隧道编辑弹窗保存途中接着修改名称，弹窗不关且保留后续修改", async () => {
    await mockSet({ tunnelsReset: true, holdReq: null });
    try {
      await twoWin("#/tunnels", "#tun-list [data-tun]", async (pa) => {
        await pa.click('[data-tedit="t2"]'); await pa.waitForSelector("#tn-ed-name");
        await pa.fill("#tn-ed-name", "第一次保存名");
        await mockSet({ holdReq: "PUT /api/tunnels/t2" });
        await pa.click('#modal-root [data-x="ok"]');
        await waitHeld(1);
        await pa.fill("#tn-ed-name", "接着又改的名字");
        await mockSet({ releaseHold: true });
        assert(await toastIn(pa, "有未保存的新改动"), "保存途中改过应提示弹窗保持打开");
        assert((await pa.inputValue("#tn-ed-name")) === "接着又改的名字", "弹窗应保留接着打的字：" + await pa.inputValue("#tn-ed-name"));
        assert(!(await pa.$eval('#modal-root [data-x="ok"]', e => e.disabled)), "确认保存按钮应恢复可用");
        await pa.click('#modal-root [data-x="ok"]');
        assert(await toastIn(pa, "已保存「接着又改的名字」的修改"), "再点保存应保存最新名称");
        const t2 = (await mockSet({})).tunnels.find(t => t.id === "t2");
        assert(t2 && t2.name === "接着又改的名字", "最终保存的名称应是最新的：" + jstr(t2));
      });
    } finally { await mockSet({ tunnelsReset: true, holdReq: null }); }
  });

  await run("两个窗口（A63-4）：伙伴禁挖区域编辑保存途中接着改名，弹窗不关且第二次可正确替换", async () => {
    const zone = (name, x) => ({ name, dim: "minecraft:overworld", min: [x, -64, 0], max: [x + 10, 320, 10] });
    await cpState({ cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, exists: true, zones: [zone("base", 0)] } } } } });
    await mockSet({ holdReq: null });
    try {
      await twoWin("#/inst/" + encodeURIComponent(PACK) + "/companion", "[data-zedit='base']", async (pa) => {
        await pa.click('[data-zedit="base"]'); await pa.waitForSelector("#cz-name");
        await pa.fill("#cz-name", "farm1");
        await mockSet({ holdReq: "PUT /api/instances/" + PACK + "/companion/settings" });
        await pa.click('.modal [data-x="ok"]');
        await waitHeld(1);
        await pa.fill("#cz-name", "farm2");
        await mockSet({ releaseHold: true });
        assert(await toastIn(pa, "有未保存的新改动"), "保存途中改过应保留弹窗");
        assert((await pa.inputValue("#cz-name")) === "farm2", "弹窗里接着改的名称应还在：" + await pa.inputValue("#cz-name"));
        assert(!(await pa.$eval('.modal [data-x="ok"]', e => e.disabled)), "确定按钮应恢复可用");
        await pa.click('.modal [data-x="ok"]');
        assert(await toastIn(pa, "已保存区域「farm2」"), "再点确定应保存 farm2");
        const names = (await apiGet("/api/instances/" + encodeURIComponent(PACK) + "/companion")).settings.zones.map(z => z.name);
        assert(jstr(names) === jstr(["farm2"]), "应只保留最后改好的 farm2：" + jstr(names));
      });
    } finally { await mockSet({ holdReq: null }); await resetCP(); }
  });

  await run("两个窗口（A63-5）：游戏内 AI 授权玩家途中接着输入下一个玩家，响应回来后不清空输入框", async () => {
    const SV = "生存服 · 朋友";
    await gcState({ ...gcReady, gc: { reset: true, inst: { [SV]: { settings: { allowed: ["Alex", "Steve"] } } } } });
    await mockSet({ holdReq: null });
    try {
      await twoWin("#/inst/" + encodeURIComponent(SV) + "/gameai", "#gc-add-name", async (pa) => {
        await pa.fill("#gc-add-name", "Bob_1");
        await mockSet({ holdReq: "PUT /api/instances/" + SV + "/gameai" });
        await pa.click("#gc-add");
        await waitHeld(1);
        await pa.focus("#gc-add-name");
        await pa.fill("#gc-add-name", "Carol_2");
        await mockSet({ releaseHold: true });
        assert(await toastIn(pa, "已允许 Bob_1 让 AI 执行命令"), "第一次授权应成功");
        assert((await pa.inputValue("#gc-add-name")) === "Carol_2", "接着输入的下一个玩家名不能被清空：" + await pa.inputValue("#gc-add-name"));
        assert(!(await pa.$eval("#gc-add", e => e.disabled)), "授权按钮应恢复可用");
        await pa.click("#gc-add");
        assert(await toastIn(pa, "已允许 Carol_2 让 AI 执行命令"), "接着可授权 Carol_2");
        const allowed = (await apiGet("/api/instances/" + encodeURIComponent(SV) + "/gameai")).settings.allowed;
        assert(allowed.includes("Bob_1") && allowed.includes("Carol_2"), "两个人名都要授上权：" + jstr(allowed));
      });
    } finally { await mockSet({ holdReq: null }); await resetGC(); }
  });

  await run("设置 → 常用：原来文件里没有的项，同一页里填上存下、再清空存下，清空的那一下也发出去（以前提示已保存，文件里却还留着）", async () => {
    const SV = "生存服 · 朋友";
    // 让这台服务器的 server.properties 里没有 resource-pack 这一行
    const strip = async route => {
      if (route.request().method() !== "GET") return route.continue();
      const resp = await route.fetch(), j = await resp.json();
      j.data.pairs = j.data.pairs.filter(p => p.key !== "resource-pack");
      await route.fulfill({ response: resp, json: j });
    };
    await page.route("**/properties", strip);
    try {
      await page.goto(url("#/inst/" + encodeURIComponent(SV) + "/common")); await settle();
      const save = async () => { await page.click('.save-bar.sticky [data-sb="save"]'); await page.waitForFunction(() => !document.querySelector(".save-bar.sticky").classList.contains("dirty"), null, { timeout: 5000 }); };
      let since = reqs.length;
      await page.fill('[data-k="resource-pack"]', "https://example.com/pack.zip");
      await save();
      assert(jstr((lastPut(since, "/properties") || {}).pairs) === jstr([{ key: "resource-pack", value: "https://example.com/pack.zip" }]), "填上：" + jstr(lastPut(since, "/properties")));
      since = reqs.length;
      await page.fill('[data-k="resource-pack"]', "");
      await save();
      assert(jstr((lastPut(since, "/properties") || {}).pairs) === jstr([{ key: "resource-pack", value: "" }]), "清空的那一下也要发：" + jstr(lastPut(since, "/properties")));
    } finally { await page.unroute("**/properties", strip); }
  });

  await run("PR #31：半成品文件夹横幅与确认删除、便携 Java 卸载占用拦截与残留清理、超过 500 项分批提交与中途失败保进度、清单项目页白名单校验", async () => {
    try {
      // 1) 半成品文件夹横幅：局域网非本机会话隐藏横幅；本机显示「世界文件」而非「存档」，取消不删，409 冲突弹提示并恢复按钮，提交中按钮禁用防连点，成功后返回 { removed: id } 并隐藏横幅
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        instReset: true,
        local: false,
        creatingLeftovers: [
          { id: "0/half-srv", name: "half-srv", root: "servers", hasWorld: true },
        ],
      }) }));
      await page.goto(url("#/instances")); await settle(500);
      assert(await page.$eval("#creating-left", el => el.hidden), "局域网非本机会话不应显示半成品残留横幅");
      assert(!(await toastHas("仅支持在本机操作", 300)), "局域网拉取半成品 403 时应静默隐藏横幅而不弹报错");

      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ local: true }) }));
      await page.goto(url("#/instances")); await settle(500);
      assert(await page.isVisible("#creating-left"), "本机有半成品时应露出横幅");
      const leftTxt = await page.textContent("#creating-left");
      assert(leftTxt.includes("half-srv") && leftTxt.includes("含世界文件") && leftTxt.includes("位于「servers」") && !leftTxt.includes("存档"), "半成品横幅文案：" + leftTxt);
      await page.click('#creating-left [data-drop-left="0/half-srv"]'); await settle(200);
      const modalBody = await page.textContent(".modal");
      assert(modalBody.includes("世界文件会一起删掉") && !modalBody.includes("存档"), "确认框文案：" + modalBody);
      await page.click('.modal [data-x="cancel"]'); await settle(200);
      assert(await page.isVisible("#creating-left"), "取消确认不该删除");

      // 半成品删除遇 409 冲突（该目录刚开始创建服务器）：提示原因；失败后重新取一次列表，正在创建的不再列出，横幅收起
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        creatingLeftovers: [{ id: "0/half-srv", name: "half-srv", root: "servers", hasWorld: true, creating: true }],
      }) }));
      await page.click('#creating-left [data-drop-left="0/half-srv"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await toastHas("无法删除这个文件夹")) && (await toastHas("正在创建服务器")), "半成品 409 时应弹出原因提示");
      assert(await page.$eval("#creating-left", el => el.hidden), "半成品 409 后应重新取列表：正在创建的不再列出，横幅收起");

      // 半成品正常删除：在途时按钮 disabled 防连点，返回 { removed: "0/half-srv" } 后隐藏横幅
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        creatingLeftovers: [{ id: "0/half-srv", name: "half-srv", root: "servers", hasWorld: true }],
      }) }));
      await page.goto(url("#/instances")); await settle(500);
      let releaseDiscard, discardArrived;
      const discardSeen = new Promise(r => { discardArrived = r; });
      const discardGate = new Promise(r => { releaseDiscard = r; });
      let discardRespData = null;
      const holdDiscard = async route => {
        if (route.request().method() !== "POST") return route.continue();
        discardArrived();
        await discardGate;
        const resp = await route.fetch();
        discardRespData = (await resp.json()).data;
        return route.fulfill({ response: resp });
      };
      await page.route("**/api/creating-leftovers/discard", holdDiscard);
      try {
        await page.click('#creating-left [data-drop-left="0/half-srv"]'); await settle(200);
        const clickOk = page.click('.modal [data-x="ok"]');
        await discardSeen;
        assert(await page.$eval('#creating-left [data-drop-left="0/half-srv"]', b => b.disabled), "删除请求在途时按钮应禁用防连点");
        releaseDiscard();
        await clickOk;
        await settle(400);
      } finally {
        await page.unroute("**/api/creating-leftovers/discard", holdDiscard);
      }
      assert(discardRespData && discardRespData.removed === "0/half-srv" && Object.keys(discardRespData).length === 1, "删除接口返回字段应为 { removed: id }：" + JSON.stringify(discardRespData));
      assert(await page.$eval("#creating-left", el => el.hidden), "删除后横幅应隐藏");

      // 2) 设置 → Java 运行时：服务器占用（409）与正在创建/导入（409）均提示并恢复按钮；卸载在途禁用防连点；未完成残留可清理；局域网隐藏按钮
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        portableJavas: [
          { major: 17, path: "D:\\MineDeck\\runtimes\\java-17\\bin\\java.exe" },
          { major: 8, path: "D:\\MineDeck\\runtimes\\java-8\\bin\\java.exe" },
        ],
        incompleteJavas: [11],
      }) }));
      await page.goto(url("#/settings/java")); await settle(500);
      await page.click('[data-rm-java="17"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await toastHas("无法卸载便携 Java")) && (await toastHas("机械动力整合包")), "被服务器占用 409 时应提示占用它的服务器名");
      assert((await page.locator('[data-rm-java="17"]').count()) === 1 && !(await page.$eval('[data-rm-java="17"]', b => b.disabled)), "被拒绝后按钮应恢复可点且项还在");

      // 正在创建或导入服务器时卸载便携 Java 返回 409
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ creating: true }) }));
      await page.click('[data-rm-java="8"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await toastHas("无法卸载便携 Java")) && (await toastHas("正在创建或导入")), "创建中卸载 Java 409 时应提示等任务结束");
      assert((await page.locator('[data-rm-java="8"]').count()) === 1 && !(await page.$eval('[data-rm-java="8"]', b => b.disabled)), "409 后 Java 8 按钮应恢复可点");
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ creating: false }) }));

      // 正常卸载便携 Java 8：请求在途时按钮禁用防连点
      let releaseRmJava, rmJavaArrived;
      const rmJavaSeen = new Promise(r => { rmJavaArrived = r; });
      const rmJavaGate = new Promise(r => { releaseRmJava = r; });
      const holdRmJava = async route => {
        if (route.request().method() !== "POST") return route.continue();
        rmJavaArrived();
        await rmJavaGate;
        return route.continue();
      };
      await page.route("**/api/javas/remove", holdRmJava);
      try {
        await page.click('[data-rm-java="8"]'); await settle(200);
        const okClick = page.click('.modal [data-x="ok"]');
        await rmJavaSeen;
        assert(await page.$eval('[data-rm-java="8"]', b => b.disabled), "卸载请求在途时按钮应禁用防连点");
        releaseRmJava();
        await okClick;
        await settle(400);
      } finally {
        await page.unroute("**/api/javas/remove", holdRmJava);
      }
      assert((await page.locator('[data-rm-java="8"]').count()) === 0, "无人占用的便携 Java 8 应已卸载");

      assert((await page.textContent("main")).includes("便携 Java 11（残留）"), "未完成残留应在设置里可见");
      await page.click('[data-rm-java="11"][data-inc="1"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert((await page.locator('[data-rm-java="11"]').count()) === 0, "残留清理后应消失");

      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ local: false }) }));
      await page.goto(url("#/settings/java")); await settle(500);
      assert((await page.locator("[data-rm-java]").count()) === 0, "局域网非本机不应显示卸载便携 Java 按钮");
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ local: true }) }));

      // 3) 没装上的文件：505 项分两批（500 + 5），提交期间 #t-pending-apply 禁用防连点；第二批若失败，第一批已处理的 500 项仍同步到界面
      const items505 = Array.from({ length: 505 }, (_, k) => ({
        id: "b" + k, path: `mods/m${k}.jar`, reason: "host", host: "cdn.example.org",
        urls: [`https://cdn.example.org/m${k}.jar`], sha1: "0123456789abcdef0123456789abcdef01234567", options: ["local", "skip", "trust"],
      }));
      await page.evaluate(pnd => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        taskList: [{ id: "task-505", title: "导入整合包 505", ended: true, result: "机械动力整合包", pending: pnd }],
        trustedHosts: [],
      }) }), items505);
      let postSizes = [], failSecond = true, releaseFirstBatch, firstBatchArrived;
      const firstBatchSeen = new Promise(r => { firstBatchArrived = r; });
      const firstBatchGate = new Promise(r => { releaseFirstBatch = r; });
      const batchRoute = async route => {
        if (route.request().method() !== "POST") return route.continue();
        const body = JSON.parse(route.request().postData() || "{}");
        postSizes.push((body.items || []).length);
        if (failSecond && postSizes.length === 1) {
          firstBatchArrived();
          await firstBatchGate;
        }
        if (failSecond && postSizes.length === 2) {
          return route.fulfill({ status: 502, contentType: "application/json", body: JSON.stringify({ ok: false, error: "模拟第二批网络中断" }) });
        }
        return route.continue();
      };
      await page.route("**/import-pending", batchRoute);
      try {
        await page.goto(url("#/task/task-505")); await settle(700);
        await page.click("#pnd-trust-all"); await settle(200);
        await page.click("#pnd-ack-all"); await settle(200);
        const applyClick = page.click("#t-pending-apply");
        await firstBatchSeen;
        assert(await page.$eval("#t-pending-apply", b => b.disabled), "分批提交在途时应用按钮应禁用防连点");
        releaseFirstBatch();
        await applyClick;
        await settle(900);
        assert(JSON.stringify(postSizes) === JSON.stringify([500, 5]), "505 项应按 500+5 分批提交：" + JSON.stringify(postSizes));
        const remainAfterFail = await page.$$eval("#t-pending .pnd-item", els => els.length);
        assert(remainAfterFail === 5, "第二批失败时第一批的 500 项应已从待处理移除，只剩 5 项：" + remainAfterFail);
        assert(!(await page.$eval("#t-pending-apply", b => b.disabled)), "分批提交失败结束后应用按钮应恢复可点以便重试");

        // 重试剩下的 5 项（同域名已在第一批记住，重新选上即可完成）
        failSecond = false; postSizes = [];
        await page.click("#pnd-trust-all"); await settle(150);
        await page.click("#pnd-ack-all"); await settle(150);
        await page.click("#t-pending-apply"); await settle(600);
        assert(JSON.stringify(postSizes) === JSON.stringify([5]) && (await page.textContent("#t-pending")).includes("都处理完了"), "剩余 5 项重试应完成");
      } finally {
        await page.unroute("**/import-pending", batchRoute);
      }

      // 3b) 第二批其实已经在后端处理完、只是回应丢了（连接中途断开）：界面要重新要一次清单，不能还显示那 5 项
      await page.evaluate(pnd => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        taskList: [{ id: "task-505", title: "导入整合包 505", ended: true, result: "机械动力整合包", pending: pnd }],
        trustedHosts: [],
      }) }), items505);
      let lostSizes = [];
      const lostRoute = async route => {
        if (route.request().method() !== "POST") return route.continue();
        lostSizes.push((JSON.parse(route.request().postData() || "{}").items || []).length);
        if (lostSizes.length === 2) {
          await route.fetch(); // 后端照常处理
          return route.fulfill({ status: 502, contentType: "application/json", body: JSON.stringify({ ok: false, error: "模拟回应途中断开" }) });
        }
        return route.continue();
      };
      await page.route("**/import-pending", lostRoute);
      try {
        await page.goto(url("#/task/task-505")); await settle(700);
        await page.click("#pnd-trust-all"); await settle(200);
        await page.click("#pnd-ack-all"); await settle(200);
        await page.click("#t-pending-apply"); await settle(900);
        assert(JSON.stringify(lostSizes) === JSON.stringify([500, 5]), "应分 500+5 两批：" + JSON.stringify(lostSizes));
        const left = await page.$$eval("#t-pending .pnd-item", els => els.length);
        assert(left === 0 && (await page.textContent("#t-pending")).includes("都处理完了"), "回应丢了但后端已处理完时，界面应按最新清单显示处理完了，还剩：" + left);
      } finally {
        await page.unroute("**/import-pending", lostRoute);
      }

      // 4) 待装清单里：合法 slug 给项目页链接，非法 slug（如 .. 或含斜杠）不生成链接
      const badCartRoute = async route => {
        if (route.request().method() !== "GET") return route.continue();
        return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
          ok: true, data: {
            count: 2, total: 2, local: [],
            items: [
              { projectId: "jei", slug: "jei", title: "JEI", kind: "mod", license: "MIT" },
              { projectId: "evil", slug: "../evil", title: "Evil", kind: "mod", license: "MIT" },
            ],
          },
        }) });
      };
      await page.route("**/api/modcart?*", badCartRoute);
      try {
        await page.goto(url("#/inst/" + encodeURIComponent("机械动力整合包") + "/addons")); await settle(600);
        const listLinks = await page.$$eval("#mc-list a[href]", as => as.map(a => a.getAttribute("href")));
        assert(JSON.stringify(listLinks) === JSON.stringify(["https://modrinth.com/mod/jei"]), "非法 slug 不应生成项目页链接：" + JSON.stringify(listLinks));
      } finally {
        await page.unroute("**/api/modcart?*", badCartRoute);
      }
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ instReset: true, taskList: [], trustedHosts: [], local: true, modCart: { reset: true } }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("没建完的文件夹：确认框写明在哪个存放位置，删除失败后重新取一次列表、不留旧的那一行", async () => {
    try {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        instReset: true,
        local: true,
        creatingLeftovers: [
          { id: "11/half-srv", name: "half-srv", root: "移动硬盘服务器", hasWorld: true },
          { id: "22/half-srv", name: "half-srv", root: "servers", hasWorld: false },
        ],
      }) }));
      await page.goto(url("#/instances")); await settle(500);
      await page.click('#creating-left [data-drop-left="11/half-srv"]'); await settle(200);
      const body = await page.textContent(".modal");
      assert(body.includes("位于「移动硬盘服务器」的「half-srv」"), "两个位置里有同名的，确认框要写明删的是哪个位置里的：" + body);
      await page.click('.modal [data-x="cancel"]'); await settle(200);

      // 别处已经把「移动硬盘服务器」里那个删掉了，这里还停着旧列表
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({
        creatingLeftovers: [{ id: "22/half-srv", name: "half-srv", root: "servers", hasWorld: false }],
      }) }));
      await page.click('#creating-left [data-drop-left="11/half-srv"]'); await settle(200);
      await page.click('.modal [data-x="ok"]'); await settle(400);
      assert(await toastHas("无法删除这个文件夹"), "删不掉时应提示");
      assert(!(await page.$('#creating-left [data-drop-left="11/half-srv"]')), "删除失败后应重新取列表，旧的那一行不该还留着");
      const left = await page.textContent("#creating-left");
      assert(left.includes("位于「servers」") && !left.includes("移动硬盘服务器"), "重新取到的列表只剩另一个位置里的那一个：" + left);
      assert(!(await page.$eval('#creating-left [data-drop-left="22/half-srv"]', b => b.disabled)), "重新取到的那一行应能点");
    } finally {
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ instReset: true, local: true }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("A01：常用设置的游戏规则开关优先按世界存档显示；世界未生成时退回 server.properties；运行中切换后立即更新且不等存档落盘", async () => {
    const aState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const VAN = "原版快照测试", enc = encodeURIComponent(VAN);
    try {
      await aState({ instReset: true, commands: [] });
      // 默认仿真数据里「原版快照测试」（26.2，>= 1.21.11）自带世界存档的 gamerules，enable-command-block 在存档里是 true（server.properties 默认是 false）
      await page.goto(url(`#/inst/${enc}/settings/common`));
      await page.waitForSelector('[data-gr="command_blocks_work"]');
      let gr = await page.evaluate(() => ({
        pvp: document.querySelector('[data-gr="pvp"]').checked,
        nether: document.querySelector('[data-gr="allow_entering_nether_using_portals"]').checked,
        cmdBlock: document.querySelector('[data-gr="command_blocks_work"]').checked,
        monsters: document.querySelector('[data-gr="spawn_monsters"]').checked,
        disabled: document.querySelector('[data-gr="pvp"]').disabled,
      }));
      assert(gr.pvp && gr.nether && gr.cmdBlock && gr.monsters && gr.disabled, "有世界存档时应按存档值显示（停机/启动中置灰）：" + JSON.stringify(gr));

      // 存档里关掉了 pvp 和 spawn-monsters，即使 server.properties 里还是 true，也按存档值画成关
      await aState({ inst: { name: VAN, gamerules: { pvp: "false", "allow-nether": "true", "enable-command-block": "true", "spawn-monsters": "false" } } });
      await page.goto(url(`#/inst/${enc}/console`)); await settle(200);
      await page.goto(url(`#/inst/${enc}/settings/common`));
      await page.waitForSelector('[data-gr="pvp"]');
      gr = await page.evaluate(() => ({
        pvp: document.querySelector('[data-gr="pvp"]').checked,
        nether: document.querySelector('[data-gr="allow_entering_nether_using_portals"]').checked,
        cmdBlock: document.querySelector('[data-gr="command_blocks_work"]').checked,
        monsters: document.querySelector('[data-gr="spawn_monsters"]').checked,
      }));
      assert(!gr.pvp && gr.nether && gr.cmdBlock && !gr.monsters, "存档值与 server.properties 不同时应以存档为准：" + JSON.stringify(gr));

      // 世界还没生成（后端不返回 gamerules）：退回 server.properties 的旧值（enable-command-block 默认为 false，其余为 true）
      await aState({ inst: { name: VAN, gamerules: null } });
      await page.goto(url(`#/inst/${enc}/console`)); await settle(200);
      await page.goto(url(`#/inst/${enc}/settings/common`));
      await page.waitForSelector('[data-gr="command_blocks_work"]');
      gr = await page.evaluate(() => ({
        pvp: document.querySelector('[data-gr="pvp"]').checked,
        cmdBlock: document.querySelector('[data-gr="command_blocks_work"]').checked,
      }));
      assert(gr.pvp && !gr.cmdBlock, "读不到存档时应退回 server.properties：" + JSON.stringify(gr));

      // R4：1.21.10 的服务器运行中拨开关，规则名取 1.21.9–1.21.10 正式版名字（commandBlocksEnabled、allowEnteringNetherUsingPortals、spawnMonsters）
      await aState({ inst: { name: VAN, mc: "1.21.10", status: "running", gamerules: { pvp: "true", "allow-nether": "true", "enable-command-block": "false", "spawn-monsters": "true" } }, commands: [] });
      await page.goto(url(`#/inst/${enc}/console`)); await settle(200);
      await page.goto(url(`#/inst/${enc}/settings/common`));
      await page.waitForSelector('[data-gr="commandBlocksEnabled"]:not([disabled])');
      await page.click('.prop-item:has([data-gr="commandBlocksEnabled"]) .switch');
      await settle(250);
      assert(await toastHas("已应用游戏规则 commandBlocksEnabled = true"), "1.21.10 提示应显示实际发出的规则名 commandBlocksEnabled");
      await page.click('.prop-item:has([data-gr="allowEnteringNetherUsingPortals"]) .switch');
      await settle(200);
      await page.click('.prop-item:has([data-gr="spawnMonsters"]) .switch');
      await settle(200);
      let st = await aState({});
      assert(JSON.stringify(st.commands) === JSON.stringify([
        { inst: VAN, cmd: "gamerule commandBlocksEnabled true" },
        { inst: VAN, cmd: "gamerule allowEnteringNetherUsingPortals false" },
        { inst: VAN, cmd: "gamerule spawnMonsters false" },
      ]), "1.21.10 应发正式版驼峰规则名：" + JSON.stringify(st.commands));

      // R4：1.21.11 起的服务器运行中拨开关，规则名改为下划线名字（command_blocks_work、allow_entering_nether_using_portals、spawn_monsters），且切页回来保持刚拨过的状态
      await aState({ inst: { name: VAN, mc: "1.21.11", status: "running", gamerules: { pvp: "true", "allow-nether": "true", "enable-command-block": "false", "spawn-monsters": "true" } }, commands: [] });
      await page.goto(url(`#/inst/${enc}/console`)); await settle(200);
      await page.goto(url(`#/inst/${enc}/settings/common`));
      await page.waitForSelector('[data-gr="command_blocks_work"]:not([disabled])');
      await page.click('.prop-item:has([data-gr="command_blocks_work"]) .switch');
      await settle(250);
      assert(await toastHas("已应用游戏规则 command_blocks_work = true"), "1.21.11 提示应显示实际发出的规则名 command_blocks_work");
      await page.click('.prop-item:has([data-gr="allow_entering_nether_using_portals"]) .switch');
      await settle(200);
      await page.click('.prop-item:has([data-gr="spawn_monsters"]) .switch');
      await settle(200);
      await page.click('.prop-item:has([data-gr="pvp"]) .switch');
      await settle(200);
      st = await aState({});
      assert(JSON.stringify(st.commands) === JSON.stringify([
        { inst: VAN, cmd: "gamerule command_blocks_work true" },
        { inst: VAN, cmd: "gamerule allow_entering_nether_using_portals false" },
        { inst: VAN, cmd: "gamerule spawn_monsters false" },
        { inst: VAN, cmd: "gamerule pvp false" },
      ]), "1.21.11 应发下划线规则名：" + JSON.stringify(st.commands));
      assert((await page.$eval('[data-gr="pvp"]', el => el.checked)) === false, "当页开关状态应立即更新为 false");
      // 模拟存档尚未自动落盘（后端 gamerules 里 pvp 仍是 true），切到控制台再回常用设置，开关仍保持刚改过的 false
      await page.click('.tabs .tab[data-t="console"]'); await settle(200);
      await page.click('.tabs .tab[data-t="settings"]');
      await page.waitForSelector('[data-gr="pvp"]');
      assert((await page.$eval('[data-gr="pvp"]', el => el.checked)) === false, "存档落盘前切页回来，开关也应保持刚改过的状态");
    } finally {
      await aState({ instReset: true, commands: [] });
    }
  });

  await run("A04：端口预查识别系统保留段；自动换出的端口改后再填回不再免查；新建与导入提交时端口被占或保留会回传改端口提示且不建服", async () => {
    const pState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const toWizardFinal = async () => {
      await page.goto(url("#/create/new")); await page.waitForSelector('.core-card[data-id="paper"]');
      await page.click('.core-card[data-id="paper"]'); await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
      await page.click("#wnext"); await page.waitForSelector("#f-port");
      await page.waitForFunction(() => document.querySelector("#f-port-note") !== null);
    };
    const portView = (input, note) => page.evaluate(([i, n]) => ({
      v: $(i).value, note: $(n).textContent.trim(), warn: $(n).classList.contains("warn"),
      useBtn: ($(n).querySelector("[data-port-use]") || {}).textContent || "",
    }), [input, note]);
    try {
      // 1) 25565 落在系统保留段里时，默认端口自动避开并说明是系统保留；自动换出来的端口（25569）改走后被别的程序占上，再填回 25569 不再免查
      await pState({ instReset: true, taskStage: 4, modCart: { reset: true }, reservedPorts: [[25565, 25568]], otherPorts: {} });
      await pState({ inst: { name: "生存服 · 朋友", port: 25590 } });
      await toWizardFinal();
      let f = await portView("#f-port", "#f-port-note");
      assert(f.v === "25569" && f.note.includes("25565 在系统保留的端口范围里，已改为 25569") && !f.warn, "系统保留段默认换端口：" + JSON.stringify(f));

      await page.fill("#f-port", "25575"); await settle(500);
      await pState({ otherPorts: { 25569: "java.exe" } });
      await page.fill("#f-port", "25569"); await settle(500);
      f = await portView("#f-port", "#f-port-note");
      assert(f.warn && f.note.includes("端口 25569 现在被别的程序（java.exe）占着"), "自动换出来的端口改过再填回也应复查：" + JSON.stringify(f));

      // 2) 表单开着期间默认端口（或自动换出的端口）被占上，不改端口直接提交：后端拦下不建服，表单原地显示改端口提示
      await pState({ instReset: true, taskStage: 4, modCart: { reset: true }, reservedPorts: [], otherPorts: {} });
      await pState({ inst: { name: "生存服 · 朋友", port: 25590 } }); // 25565 初始空着
      await toWizardFinal();
      f = await portView("#f-port", "#f-port-note");
      assert(f.v === "25565" && f.note === "", "打开时 25565 空着：" + JSON.stringify(f));
      await pState({ otherPorts: { 25565: "java.exe" } }); // 表单开着期间 25565 被占上
      await page.fill("#f-name", "提交复查服"); await page.check("#f-eula");
      await page.click("#wcreate"); await settle(500);
      assert(page.url().includes("#/create/new"), "端口被占不应跳去任务页：" + page.url());
      let st = await pState({});
      assert(!st.instances.some(i => i.name === "提交复查服"), "端口被占不应创建服务器");
      f = await portView("#f-port", "#f-port-note");
      assert(f.warn && f.note.includes("端口 25565 现在被别的程序（java.exe）占着") && f.useBtn.includes("改成 25568"), "提交被拦后表单应给改端口提示：" + JSON.stringify(f));
      // 点提示上的「改成 25568」再提交即可创建
      await page.click("#f-port-note [data-port-use]"); await settle(200);
      assert((await page.inputValue("#f-port")) === "25568", "点改成 25568 应填入新端口");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 6000 });
      st = await pState({});
      assert(st.lastCreate && st.lastCreate.port === 25568, "换端口后创建成功：" + JSON.stringify(st.lastCreate));

      // 3) 导入整合包表单开着期间，自动换出的端口被系统保留段占上，提交同样拦下回传提示
      await pState({ instReset: true, reservedPorts: [], otherPorts: {} });
      await page.goto(url("#/create/import")); await page.waitForSelector("#im-port-note");
      await page.waitForFunction(() => $("#im-port").value === "25568", null, { timeout: 3000 });
      await pState({ reservedPorts: [[25568, 25570]] });
      await page.setInputFiles("#im-file", { name: "复查测试.mrpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004mrpack") });
      await page.check("#im-eula");
      await page.click("#im-go"); await settle(500);
      assert(page.url().includes("#/create/import"), "导入端口被系统保留应留在表单：" + page.url());
      f = await portView("#im-port", "#im-port-note");
      assert(f.warn && f.note.includes("系统保留") && f.useBtn.includes("改成 25571"), "导入被拦后应提示系统保留与建议端口：" + JSON.stringify(f));

      // 4) R3：另一台本软件的服务器正开着、占着同一个端口时，表单上照旧显示软提示，但提交能建成、不回 409 拦着
      await pState({ instReset: true, taskStage: 4, modCart: { reset: true }, reservedPorts: [], otherPorts: {} });
      await toWizardFinal();
      await page.fill("#f-port", "25565"); // 「生存服 · 朋友」正开着（running），用着 25565
      f = await portView("#f-port", "#f-port-note");
      assert(f.warn && f.note.includes("生存服 · 朋友") && f.note.includes("它现在开着") && f.note.includes("两台不能同时开"),
        "撞了正开着的自家服务器应有软提示：" + JSON.stringify(f));
      await page.fill("#f-name", "共用运行服端口"); await page.check("#f-eula");
      await page.click("#wcreate");
      await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 6000 });
      st = await pState({});
      assert(st.lastCreate && st.lastCreate.name === "共用运行服端口" && st.lastCreate.port === 25565,
        "另一台自家服务器开着时同端口新建应能建成：" + JSON.stringify(st.lastCreate));
    } finally {
      await pState({ instReset: true, reservedPorts: [], otherPorts: {}, taskStage: 2, modCart: { reset: true } });
    }
  });

  await run("A07：上次启动失败原因在程序重开后仍在卡片和详情页头显示；点「关闭提示」后立即清除", async () => {
    const sState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PACK0 = "机械动力整合包", enc = encodeURIComponent(PACK0);
    const why = "端口 25566 被别的程序占着，换个端口或关掉占用它的程序";
    try {
      await sState({ instReset: true });
      await sState({ inst: { name: PACK0, startFail: why, startPortBusyFail: { port: 25566 } } });
      await page.goto(url(`#/inst/${enc}/console`)); await page.waitForSelector("#d-start");
      await page.click("#d-start"); await page.waitForSelector("#d-start-fail");

      // 模拟程序重开：落盘的启动失败原因和改端口入口依然保留在卡片与详情页头
      await sState({ restart: true });
      await page.goto(url("#/instances")); await page.waitForSelector(`.card[data-name="${PACK0}"] .inst-fail`);
      assert((await page.textContent(`.card[data-name="${PACK0}"] .inst-fail`)).includes(why), "重开后列表卡片仍应显示启动失败原因");

      await page.goto(url(`#/inst/${enc}/console`)); await page.waitForSelector("#d-start-fail [data-start-dismiss]");
      const info = await page.evaluate(() => ({
        text: $("#d-start-fail").textContent,
        edit: !!$("#d-start-fail [data-port-edit]"),
      }));
      assert(info.text.includes(why) && info.edit, "重开后详情页头仍应显示启动失败原因与改端口：" + JSON.stringify(info));

      // 用户在界面上关掉提示：详情页头和列表卡片都清除
      await page.click("#d-start-fail [data-start-dismiss]");
      await page.waitForSelector("#d-start-fail", { state: "detached", timeout: 4000 });
      await page.goto(url("#/instances")); await settle(400);
      assert(!(await page.$(`.card[data-name="${PACK0}"] .inst-fail`)), "关闭提示后列表卡片上的失败提示应消失");
    } finally {
      await sState({ instReset: true });
    }
  });

  await run("A08：重名文件夹提示横幅在服务器列表页顶写出文件夹位置、撞上的服务器名与改名处理办法", async () => {
    const mState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    try {
      await mState({
        instReset: true,
        duplicateDirs: [
          { name: "生存服 · 朋友", dir: "F:\\MCBackup\\survival-copy", keptDir: "D:\\OtherDisk\\adopted-survival" },
        ],
      });
      await page.goto(url("#/instances"));
      await page.waitForSelector("#dup-dirs:not([hidden]) .warn-card");
      const text = await page.textContent("#dup-dirs");
      assert(
        text.includes("F:\\MCBackup\\survival-copy") &&
        text.includes("生存服 · 朋友") &&
        text.includes("D:\\OtherDisk\\adopted-survival") &&
        text.includes("instance.json") &&
        text.includes("再用别的名字接管回来，然后重开 MineDeck"),
        "重名提示应写清被跳过的文件夹、撞上的服务器与两种改名后重开办法：" + text,
      );
      await mState({ duplicateDirs: [] });
      await page.goto(url("#/instances")); await settle(300);
      assert(await page.$eval("#dup-dirs", el => el.hidden), "没有重名文件夹时提示横幅应隐藏");
    } finally {
      await mState({ instReset: true });
    }
  });

  await run("A26：接管自带非官方日志配置的服务器时，任务结果卡片与日志写出等于没防及处理办法", async () => {
    const mState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const customTitle = "log4shell 防护：这台 1.12.2 的服务器没有套上 Mojang 的日志配置，等于没防";
    const customNote = "服务器文件夹里已经有一份 log4j2_112-116.xml，内容和官方的不一样（可能是自己改过的），MineDeck 没有覆盖它，也没有替它加上引用。要用官方的那份，把这个文件改名或挪走以后重开 MineDeck；要用自己的，在「启动与内存」里加上 -Dlog4j.configurationFile=log4j2_112-116.xml";
    try {
      await mState({
        instReset: true,
        adoptScan: {
          dir: "D:\\OldServers\\mc112", name: "老112服", core: "vanilla", mc: "1.12.2", build: "",
          launchTarget: "jar:server.jar",
          targets: [{ target: "jar:server.jar", label: "server.jar（Vanilla 原版 1.12.2）", core: "vanilla", mc: "1.12.2", build: "", javaMajor: 8 }],
          javaMajor: 8, xmxMb: 2048, extraJvm: [], port: 25575, eula: true, hasWorld: true, managed: false, notes: [], whitelistOn: false,
        },
        adoptTask: {
          warnings: [{
            kind: "custom-log4j",
            title: customTitle,
            note: customNote,
          }],
          log: [
            "服务器文件夹：D:\\OldServers\\mc112",
            "⚠ " + customTitle + "。" + customNote,
            "接管完成 ✔ 世界、模组、插件和配置文件都没有动",
          ],
        },
      });
      await page.goto(url("#/create/adopt"));
      await page.waitForSelector("#ad-dir");
      await page.fill("#ad-dir", "D:\\OldServers\\mc112");
      await page.click("#ad-scan");
      await page.waitForSelector("#ad-name");
      await page.click("#ad-go");
      await page.waitForSelector('#t-msgs .warn-card[data-warn="custom-log4j"]');
      const warnText = await page.textContent('#t-msgs .warn-card[data-warn="custom-log4j"]');
      const logText = await page.textContent("#task-log");
      assert(warnText.includes("等于没防") && warnText.includes("-Dlog4j.configurationFile=log4j2_112-116.xml"), "接管结果告警应说明等于没防及处理办法：" + warnText);
      assert(logText.includes("等于没防") && logText.includes("-Dlog4j.configurationFile=log4j2_112-116.xml"), "接管任务日志也应包含该说明：" + logText);
    } finally {
      await mState({ instReset: true, adoptScan: null });
    }
  });

  await run("A25 A28：启动与内存预览显示后端自动补齐的 log4shell 参数；同名日志配置非官方时不补引用并说明原因", async () => {
    const mState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PK = "机械动力整合包", enc = encodeURIComponent(PK);
    const FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml"];
    try {
      // 1) 官方日志配置在盘上时：即使在输入框里把缓解参数删掉，预览仍实时展示后端自动补上的两条 log4shell 参数
      await mState({ instReset: true, legacy: { name: PK, mc: "1.16.5", extraJvm: ["-XX:+UseG1GC", ...FIX], forceFlags: FIX } });
      await page.goto(url(`#/inst/${enc}/settings/jvm`));
      await page.waitForSelector("#jm-preview");
      await page.fill("#jm-jvm", "-XX:+UseZGC");
      await page.waitForFunction(() => {
        const t = document.querySelector("#jm-preview")?.textContent || "";
        return t.includes("-XX:+UseZGC") && t.includes("-Dlog4j2.formatMsgNoLookups=true") && t.includes("-Dlog4j.configurationFile=log4j2_112-116.xml");
      });
      assert(await page.$eval("#jm-preview-note", el => el.hidden), "官方日志配置不应显示冲突提示");

      // 2) 文件夹里是同名非官方日志配置时：编辑参数后预览和保存都不强加 -Dlog4j.configurationFile= 引用，且两处都写出等于没防及处理办法
      await mState({ legacy: { name: PK, mc: "1.16.5", extraJvm: ["-Dlog4j2.formatMsgNoLookups=true"], forceFlags: FIX, customLog4jXml: "log4j2_112-116.xml" } });
      await page.goto(url(`#/inst/${enc}/settings/jvm`));
      await page.waitForSelector("#jm-preview");
      await page.fill("#jm-jvm", "-XX:+UseG1GC");
      await page.waitForFunction(() => !document.querySelector("#jm-preview-note")?.hidden && (document.querySelector("#jm-preview")?.textContent || "").includes("-XX:+UseG1GC"));
      const prev = await page.evaluate(() => ({
        cmd: $("#jm-preview").textContent,
        note: $("#jm-preview-note").textContent,
      }));
      assert(prev.cmd.includes("-Dlog4j2.formatMsgNoLookups=true") && !prev.cmd.includes("-Dlog4j.configurationFile=log4j2_112-116.xml"),
        "非官方日志配置时预览不应强加引用：" + JSON.stringify(prev));
      assert(prev.note.includes("等于没防") && prev.note.includes("-Dlog4j.configurationFile=log4j2_112-116.xml"),
        "预览应说明等于没防及处理办法：" + JSON.stringify(prev));

      await page.click('[data-sb="save"]');
      await page.waitForFunction(() => [...document.querySelectorAll(".toast")].some(t => t.textContent.includes("等于没防")));
      assert(!(await page.inputValue("#jm-jvm")).includes("-Dlog4j.configurationFile=log4j2_112-116.xml"),
        "保存后回显的参数框也不应被强加官方引用：" + await page.inputValue("#jm-jvm"));
    } finally {
      await mState({ instReset: true });
    }
  });

  await run("启动与内存：未改最大内存时预览与保存共用请求体，保留原有最小内存", async () => {
    const mState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const PK = "机械动力整合包", enc = encodeURIComponent(PK);
    try {
      await mState({ instReset: true });
      await mState({ inst: { name: PK, xmsMb: 2048 } });
      await page.goto(url(`#/inst/${enc}/settings/jvm`));
      await page.waitForFunction(() => (document.querySelector("#jm-preview")?.textContent || "").includes("-Xms2048M"));
      await page.fill("#jm-jvm", "-XX:+UseZGC");
      await page.waitForFunction(() => {
        const t = document.querySelector("#jm-preview")?.textContent || "";
        return t.includes("-XX:+UseZGC") && t.includes("-Xms2048M");
      });
      const previewBeforeSave = await page.textContent("#jm-preview");
      assert(previewBeforeSave.includes("-Xms2048M") && !previewBeforeSave.includes("-Xms1024M"),
        "只改 JVM 参数时预览不应把 -Xms2048M 重算成 -Xms1024M：" + previewBeforeSave);
      await page.click('[data-sb="save"]');
      await page.waitForFunction(() => !document.querySelector(".save-bar.sticky").classList.contains("dirty"));
      const previewAfterSave = await page.textContent("#jm-preview");
      assert(previewAfterSave === previewBeforeSave, "保存后的启动参数应与保存前预览完全一致：" + previewAfterSave + " vs " + previewBeforeSave);
    } finally {
      await mState({ instReset: true });
    }
  });

  await run("待装清单：全部安装前没查过前置或清单变过时先查一遍，列必装前置确认，检查失败不安装", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);
      assert((await cartMock()).depsQueries === 0, "加入 Modrinth 项目时还没查过前置");

      // 1) 没点过「检查前置」直接点「全部安装」：请求前置期间按钮保持防连点，回来后弹出列出必装前置的确认框
      let releaseDeps;
      const holdDeps = new Promise(r => { releaseDeps = r; });
      await page.route("**/api/modcart/deps*", async route => { await holdDeps; await route.continue().catch(() => {}); });
      await page.click("#mc-install");
      await settle(150);
      assert(await page.$eval("#mc-install", b => b.disabled), "等待前置检查期间「全部安装」按钮应保持禁用防连点");
      releaseDeps();
      await settle(50);
      await page.unroute("**/api/modcart/deps*");

      await page.waitForSelector(".modal", { timeout: 5000 });
      const modalText1 = await page.textContent(".modal");
      assert(modalText1.includes("另外会自动装上 1 项必装前置") && modalText1.includes("Architectury API"),
        "没查过前置直接全部安装应弹出列出必装前置的确认框：" + modalText1);
      await page.click('.modal [data-x="cancel"]'); await settle(200);
      assert((await cartMock()).lastInstall === null, "点「先不装」不应发出安装请求");

      // 2) 清单在上次检查后变过（已过时）：先给无必装前置的模组查过一次，再加进需要前置的 JEI，点全部安装会重新检查
      await page.click("#mc-clear"); await settle(500);
      await page.fill("#mc-q", "Cloth"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 Cloth Config API"]'); await settle(700);
      await page.click("#mc-dep-go"); await settle(500);
      assert((await page.textContent("#mc-dep-box")).includes("不缺前置"), "单放 Cloth Config API 不缺前置");
      const beforeAddJei = (await cartMock()).depsQueries;

      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 JEI"]'); await settle(700);

      // 3) 检查前置失败时：不安装，提示失败原因
      await cartState({ modCart: { depsFail: "Modrinth 暂时连不上" } });
      await page.click("#mc-install"); await settle(600);
      assert((await cartMock()).depsQueries === beforeAddJei + 1, "清单变过后再点安装应重查前置");
      assert((await cartMock()).lastInstall === null && !page.url().includes("#/task/"), "检查前置失败时不应开始安装");
      const failToasts = await page.$$eval(".toast", ts => ts.map(x => x.textContent));
      assert(failToasts.some(x => x.includes("Modrinth 暂时连不上")), "检查前置失败应提示失败原因：" + JSON.stringify(failToasts));

      // 4) 网络恢复后再点「全部安装」：弹出确认框，确认后正常安装
      await cartState({ modCart: { depsFail: "" } });
      await page.click("#mc-install");
      await page.waitForSelector(".modal", { timeout: 5000 });
      assert((await page.textContent(".modal")).includes("Architectury API"), "重查后确认框应列出 Architectury API");
      await page.click('.modal [data-x="ok"]');
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await cartMock()).lastInstall;
      assert(li && li.withDeps === true && li.expect.slice().sort().join() === "cloth-config,jei", "确认后应发起安装：" + JSON.stringify(li));
    } finally {
      await page.unroute("**/api/modcart/deps*").catch(() => {});
      await cartState({ modCart: { reset: true } });
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("待装清单：查前置期间清单变了，自动补查并按最新清单列出必装前置", async () => {
    try {
      await cartState({ modCart: { reset: true } });
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/cart`)); await settle(600);
      await page.fill("#mc-q", "Cloth"); await page.click("#mc-go"); await settle();
      await page.click('button[aria-label="加入清单 Cloth Config API"]'); await settle(700);
      await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();

      // 先让后端按只有 Cloth Config 的清单算出检查结果，再把响应压住；期间把带必装前置的 JEI 加进清单
      let releaseFirst, onFetched;
      const holdFirst = new Promise(r => { releaseFirst = r; });
      const waitFetched = new Promise(r => { onFetched = r; });
      let seenDeps = 0;
      await page.route("**/api/modcart/deps*", async route => {
        seenDeps++;
        if (seenDeps === 1) {
          const resp = await route.fetch();
          onFetched();
          await holdFirst;
          await route.fulfill({ response: resp }).catch(() => {});
          return;
        }
        await route.continue().catch(() => {});
      });

      await page.click("#mc-install");
      await waitFetched;
      await page.click('button[aria-label="加入清单 JEI"]');
      await page.waitForFunction(() => (document.querySelector("#mc-count")?.textContent || "").includes("共 2 项"), null, { timeout: 5000 });
      releaseFirst();

      await page.waitForSelector(".modal", { timeout: 5000 });
      const modalText = await page.textContent(".modal");
      assert(seenDeps >= 2, "查前置期间清单变了应自动重查一遍，实际请求次数：" + seenDeps);
      assert(modalText.includes("另外会自动装上 1 项必装前置") && modalText.includes("Architectury API"),
        "重查后应弹出列出 JEI 必装前置的确认框：" + modalText);
      assert((await cartMock()).lastInstall === null, "确认前不应发出安装请求");

      await page.click('.modal [data-x="ok"]');
      await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
      const li = (await cartMock()).lastInstall;
      assert(li && li.withDeps === true && li.expect.slice().sort().join() === "cloth-config,jei", "确认后应按最新清单安装：" + JSON.stringify(li));
    } finally {
      await page.unroute("**/api/modcart/deps*").catch(() => {});
      await cartState({ modCart: { reset: true } });
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("没装上的文件：超过 500 项分批提交显示批次进度，还有未提交批次时切页先确认", async () => {
    const setTask = items => page.evaluate(t => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [t], trustedHosts: [] }) }), {
      id: "task-1200", title: "导入整合包 大量外部文件", ended: true, result: "机械动力整合包", pending: items,
    });
    try {
      await setTask(pendingItems("p", 1200));
      await page.goto(url("#/task/task-1200")); await settle(600);
      await page.click("#pnd-trust-all"); await settle(300);
      await page.click("#pnd-ack-all"); await settle(300);

      let batchNum = 0;
      const gates = [];
      for (let k = 0; k < 3; k++) {
        let go;
        const p = new Promise(r => { go = r; });
        gates.push({ p, go });
      }
      await page.route("**/import-pending", async route => {
        if (route.request().method() !== "POST") { await route.continue().catch(() => {}); return; }
        const idx = batchNum++;
        if (gates[idx]) await gates[idx].p;
        await route.continue().catch(() => {});
      });

      await page.click("#t-pending-apply");
      await page.waitForFunction(() => (document.querySelector("#t-pending .pnd-prog")?.textContent || "").includes("正在处理第 1 批，共 3 批"), null, { timeout: 5000 });

      // 第 1 批在途、还剩 700 项未提交时切页：弹出确认框，取消则留在当前页继续
      await page.click('#sidebar a[data-nav="instances"]');
      await page.waitForSelector(".modal", { timeout: 5000 });
      const leaveText1 = await page.textContent(".modal");
      assert(leaveText1.includes("还有 700 项还没提交，离开后不会继续；正在处理的这一批会照常做完"), "中途切页应提示剩余未提交项数与在途批次：" + leaveText1);
      await page.click('.modal [data-x="cancel"]'); await settle(150);
      assert(page.url().includes("#/task/task-1200"), "取消离开后应留在任务页：" + page.url());

      // 放行第 1 批，进入第 2 批：看到「正在处理第 2 批，共 3 批」
      gates[0].go();
      await page.waitForFunction(() => (document.querySelector("#t-pending .pnd-prog")?.textContent || "").includes("正在处理第 2 批，共 3 批"), null, { timeout: 5000 });

      // 放行第 2、3 批，全部处理完成
      gates[1].go();
      gates[2].go();
      await page.waitForSelector("#t-pending .pnd-done", { timeout: 10000 });
      assert(batchNum === 3, "1200 项应分 3 批提交，实际：" + batchNum);
      await page.unroute("**/import-pending");

      // 再验证确认离开：第 1 批期间确认离开后跳走，后续批次不再提交
      await setTask(pendingItems("p", 1200));
      await page.goto(url("#/task/task-1200")); await settle(600);
      await page.click("#pnd-trust-all"); await settle(300);
      await page.click("#pnd-ack-all"); await settle(300);

      let sentBatches = 0, releaseFirst, onFirstSent;
      const firstHeld = new Promise(r => { releaseFirst = r; });
      const waitFirstSent = new Promise(r => { onFirstSent = r; });
      await page.route("**/import-pending", async route => {
        if (route.request().method() !== "POST") { await route.continue().catch(() => {}); return; }
        sentBatches++;
        if (sentBatches === 1) {
          onFirstSent();
          await firstHeld;
        }
        await route.continue().catch(() => {});
      });

      await page.click("#t-pending-apply");
      await waitFirstSent;
      await page.click('#sidebar a[data-nav="instances"]');
      await page.waitForSelector(".modal", { timeout: 5000 });
      assert((await page.textContent(".modal")).includes("还有 700 项还没提交，离开后不会继续；正在处理的这一批会照常做完"), "确认框文案不对");
      await page.click('.modal [data-x="ok"]');
      await page.waitForFunction(() => location.hash === "#/instances", null, { timeout: 5000 });
      releaseFirst();
      await settle(400);
      assert(sentBatches === 1, "确认离开后剩下的第 2、3 批不应再提交，实际提交批数：" + sentBatches);
    } finally {
      await page.unroute("**/import-pending").catch(() => {});
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [], trustedHosts: [] }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  await run("没装上的文件：详情页叠层分批提交点关闭确认、两处同时分批互不抹掉离开确认、未保存修改一起确认", async () => {
    const setInstPending = items => page.evaluate(([name, list]) =>
      fetch("/__mock/state", { method: "POST", body: JSON.stringify({ taskList: [], trustedHosts: [], instPending: { [name]: { pending: list, skipped: [] } } }) }),
      [PACK, items]);
    const topConfirmSel = "#modal-root > .modal-mask:not([data-leaving]) .modal:not(.pnd-modal)";

    try {
      // 1) 详情页头叠层里分批提交尚未发完时点「关闭」：弹出确认框，取消留在叠层，确认关闭后后续批次不再发
      await setInstPending(pendingItems("m", 1200));
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/console`)); await settle(600);
      await page.click("[data-pnd-open]");
      await page.waitForSelector(".pnd-modal #d-pending-apply", { timeout: 5000 });
      await page.click(".pnd-modal #pnd-trust-all"); await settle(250);
      await page.click(".pnd-modal #pnd-ack-all"); await settle(250);

      let sentModal = 0, releaseModalBatch1, onModalBatch1;
      const holdModalBatch1 = new Promise(r => { releaseModalBatch1 = r; });
      const waitModalBatch1 = new Promise(r => { onModalBatch1 = r; });
      await page.route("**/import-pending", async route => {
        if (route.request().method() !== "POST") { await route.continue().catch(() => {}); return; }
        sentModal++;
        if (sentModal === 1) { onModalBatch1(); await holdModalBatch1; }
        await route.continue().catch(() => {});
      });

      await page.click("#d-pending-apply");
      await waitModalBatch1;
      await page.click('.pnd-modal [data-x="close"]');
      await page.waitForSelector(topConfirmSel, { timeout: 5000 });
      const closePromptText = await page.textContent(topConfirmSel);
      assert(closePromptText.includes("还有 700 项还没提交，离开后不会继续；正在处理的这一批会照常做完"),
        "叠层关闭时应提示未提交项数与在途批次：" + closePromptText);

      // 点「留在这里」取消关闭：叠层仍在
      await page.click(`${topConfirmSel} [data-x="cancel"]`); await settle(200);
      assert(!!(await page.$(".pnd-modal:not([data-leaving])")), "取消关闭后叠层应继续保留");

      // 再点「关闭」并确认「离开」：叠层关掉，放行在途批次后第 2、3 批不再发
      await page.click('.pnd-modal [data-x="close"]');
      await page.waitForSelector(topConfirmSel, { timeout: 5000 });
      await page.click(`${topConfirmSel} [data-x="ok"]`);
      await page.waitForFunction(() => !document.querySelector(".pnd-modal") || document.querySelector(".pnd-modal").closest("[data-leaving]"), null, { timeout: 5000 });
      releaseModalBatch1();
      await settle(400);
      assert(sentModal === 1, "叠层确认关闭后剩下的批次不应再发，实际批数：" + sentModal);
      await page.unroute("**/import-pending");

      // 2) 两处同时分批（模组页内嵌面板先开始、页头叠层后开始），先开始的先结束，后一处的离开确认仍在
      await setInstPending(pendingItems("a", 600));
      await page.goto(url(`#/inst/${encodeURIComponent(PACK)}/addons`)); await settle(700);
      await page.waitForSelector("#ad-pending-apply", { timeout: 5000 });
      await page.click("#ad-pending-box #pnd-trust-all"); await settle(250);
      await page.click("#ad-pending-box #pnd-ack-all"); await settle(250);

      const aGates = [null, null].map(() => { let go; const p = new Promise(r => { go = r; }); return { p, go }; });
      let aCount = 0, bCount = 0, releaseB1, onA1, onB1;
      const holdB1 = new Promise(r => { releaseB1 = r; });
      const waitA1 = new Promise(r => { onA1 = r; });
      const waitB1 = new Promise(r => { onB1 = r; });
      await page.route("**/import-pending", async route => {
        if (route.request().method() !== "POST") { await route.continue().catch(() => {}); return; }
        const body = JSON.parse(route.request().postData() || "{}");
        const firstId = ((body.items || [])[0] || {}).id || "";
        if (firstId.startsWith("a")) {
          const idx = aCount++;
          if (idx === 0) onA1();
          if (aGates[idx]) await aGates[idx].p;
        } else if (firstId.startsWith("b")) {
          bCount++;
          if (bCount === 1) { onB1(); await holdB1; }
        }
        await route.continue().catch(() => {});
      });

      // 先启动内嵌面板（600 项 = 2 批）
      await page.click("#ad-pending-apply");
      await waitA1;

      // 再准备另一组 1200 项并打开叠层启动分批（3 批，剩 700 项未提交）
      await setInstPending(pendingItems("b", 1200));
      await page.evaluate(n => pendingModal(n, viewGen, () => {}), PACK);
      await page.waitForSelector(".pnd-modal #d-pending-apply", { timeout: 5000 });
      await page.click(".pnd-modal #pnd-trust-all"); await settle(250);
      await page.click(".pnd-modal #pnd-ack-all"); await settle(250);
      await page.click("#d-pending-apply");
      await waitB1;

      // 让先开始的 #ad-pending-box 两批全部跑完（它的注销函数已执行）
      aGates[0].go();
      aGates[1].go();
      await page.waitForFunction(() => {
        const b = document.querySelector("#ad-pending-apply");
        return !b || !b.disabled;
      }, null, { timeout: 8000 });
      await settle(200);

      // 此时触发切页：后开始的叠层仍在第 1 批，离开确认不能被先结束的那一处抹掉
      await page.evaluate(() => { location.hash = "#/instances"; });
      await page.waitForSelector(topConfirmSel, { timeout: 5000 });
      const secondStillGuarded = await page.textContent(topConfirmSel);
      assert(secondStillGuarded.includes("还有 700 项还没提交"), "先开始的面板结束后，另一处的离开确认仍应在：" + secondStillGuarded);
      await page.click(`${topConfirmSel} [data-x="ok"]`);
      await page.waitForFunction(() => location.hash === "#/instances", null, { timeout: 5000 });
      releaseB1();
      await settle(300);
      await page.unroute("**/import-pending");

      // 3) 设置页有未保存修改、同时叠层正在分批提交，浏览器后退时两件事依次都问到
      await setInstPending(pendingItems("c", 1200));
      await page.goto(url("#/tunnels")); await settle(250);
      await page.evaluate(() => { location.hash = "#/instances"; }); await settle(250);
      await page.evaluate(h => { location.hash = h; }, `#/inst/${encodeURIComponent(PACK)}/settings/common`);
      await page.waitForSelector("#motd-in", { timeout: 5000 });
      await page.fill("#motd-in", "改过但还没保存的介绍");
      await page.waitForFunction(() => document.querySelector(".save-bar")?.classList.contains("dirty"), null, { timeout: 5000 });

      await page.click("[data-pnd-open]");
      await page.waitForSelector(".pnd-modal #d-pending-apply", { timeout: 5000 });
      await page.click(".pnd-modal #pnd-trust-all"); await settle(250);
      await page.click(".pnd-modal #pnd-ack-all"); await settle(250);

      let cCount = 0, releaseC1, onC1;
      const holdC1 = new Promise(r => { releaseC1 = r; });
      const waitC1 = new Promise(r => { onC1 = r; });
      await page.route("**/import-pending", async route => {
        if (route.request().method() !== "POST") { await route.continue().catch(() => {}); return; }
        cCount++;
        if (cCount === 1) { onC1(); await holdC1; }
        await route.continue().catch(() => {});
      });

      await page.click("#d-pending-apply");
      await waitC1;

      // 触发浏览器后退：第一道问未提交批次，点「离开」后第二道接着问未保存修改
      await page.goBack();
      await page.waitForSelector(topConfirmSel, { timeout: 5000 });
      const ask1 = await page.textContent(topConfirmSel);
      assert(ask1.includes("还有 700 项还没提交"), "第一道确认应问未提交的分批项：" + ask1);
      await page.click(`${topConfirmSel} [data-x="ok"]`);

      await page.waitForFunction(sel => {
        const el = document.querySelector(sel);
        return el && el.textContent.includes("放弃未保存的修改");
      }, topConfirmSel, { timeout: 5000 });
      const ask2 = await page.textContent(topConfirmSel);
      assert(ask2.includes("没有保存") && ask2.includes("修改会丢失"), "第二道确认应问未保存的设置修改：" + ask2);

      // 在第二道点「继续编辑」取消：仍留在设置页，修改没丢
      await page.click(`${topConfirmSel} [data-x="cancel"]`); await settle(200);
      assert(page.url().includes("/settings/common"), "第二道取消后应留在设置页：" + page.url());
      assert((await page.inputValue("#motd-in")) === "改过但还没保存的介绍", "取消离开后未保存内容应还在");

      // 再退一次，两道都点确认：成功离开设置页
      await page.goBack();
      await page.waitForSelector(topConfirmSel, { timeout: 5000 });
      await page.click(`${topConfirmSel} [data-x="ok"]`);
      await page.waitForFunction(sel => {
        const el = document.querySelector(sel);
        return el && el.textContent.includes("放弃未保存的修改");
      }, topConfirmSel, { timeout: 5000 });
      await page.click(`${topConfirmSel} [data-x="ok"]`);
      // 只断言离开了设置页：拦下后退又取消以后，下一次后退会多跳过一页（历史栈的老毛病，不在这里定死退到哪一页）
      await page.waitForFunction(() => !location.hash.includes("/settings/"), null, { timeout: 5000 });
      releaseC1();
      await settle(300);
    } finally {
      await page.unroute("**/import-pending").catch(() => {});
      await page.evaluate(() => fetch("/__mock/state", { method: "POST", body: JSON.stringify({ instReset: true, taskList: [], trustedHosts: [] }) }));
      await page.goto(url("#/instances")); await settle();
    }
  });

  {
    // 草稿清单（A31）：向导「选模组」那一步挑的东西、建服时切走以后留在草稿清单里的，详情页的横幅怎么提示、怎么处理
    const until = async (fn, ms = 5000) => { const t0 = Date.now(); while (!(await fn())) { if (Date.now() - t0 > ms) return false; await settle(100); } return true; };
    /* 直接摆进仿真后端的清单项（同后端 modcart.Item 的形状）；pickedCore / pickedMc 是挑的时候记下的核心和版本 */
    const item = (projectId, title, kind, pickedCore = "", pickedMc = "") => ({ projectId, slug: projectId, title, icon: "", kind, license: "MIT",
      ...(pickedCore ? { pickedCore } : {}), ...(pickedMc ? { pickedMc } : {}), addedAt: "2026-11-23T10:00:00Z" });
    /* 向导从选核心走到「选模组 / 选插件」那一步；ver 给了就换成那个版本。返回选中的版本 */
    const wizToMods = async (core, ver) => {
      await page.click(`.core-card[data-id="${core}"]`);
      await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
      if (ver) await page.click(`.ver-item[data-id="${ver}"]`);
      const sel = await page.$eval(".ver-item.sel", el => el.dataset.id);
      await page.click("#wnext"); await page.waitForSelector("#build-rec b");
      await page.click("#wnext"); await page.waitForSelector("#mc-q");
      return sel;
    };
    const wizBackToCore = async () => { await page.click('.step-pill.done[data-step="0"]'); await page.waitForSelector('.core-card[data-id="paper"]'); };
    const pick = async (q, title) => {
      await page.fill("#mc-q", q); await page.click("#mc-go"); await settle();
      await page.click(`button[aria-label="加入清单 ${title}"]`); await settle(600);
    };
    /* 填好基本设置点「创建」（从「选模组」那一步或基本设置那一步都行） */
    const wizCreate = async name => {
      if (!(await page.$("#f-name"))) { await page.click("#wnext"); await page.waitForSelector("#f-name"); }
      await page.fill("#f-name", name); await page.click("#f-eula");
      await page.click("#wcreate");
    };
    const waitBuilding = () => page.waitForFunction(() => document.querySelector("#wback")?.disabled && /等待|下载/.test(document.querySelector("#wcreate")?.textContent || ""), null, { timeout: 5000 });
    const leaveToList = async () => {
      await page.click('#sidebar a[data-nav="instances"]');
      await page.waitForFunction(() => location.hash === "#/instances", null, { timeout: 5000 });
    };
    const openAddons = async name => { await page.goto(url(`#/inst/${encodeURIComponent(name)}/addons`)); await page.waitForSelector("#mc-list .mc-item, #mc-empty", { timeout: 5000 }); };
    const freshWizard = async () => { await page.goto(url("#/create/new")); await page.waitForSelector('.core-card[data-id="fabric"]'); };
    const cleanup = async () => {
      await page.unroute("**/api/modcart/deps*").catch(() => {});
      await page.unroute("**/api/instances").catch(() => {});
      await page.unroute("**/api/modcart/draft/target").catch(() => {});
      await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
      await page.goto(url("#/instances")); await settle();
    };

    await run("草稿清单：向导回头把 Fabric 换成 Paper，回到选插件这一步不联网就提示清单里的项可能装不上，点「清空草稿清单」清掉", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true } });
        await freshWizard();
        await wizToMods("fabric");
        await pick("Cloth", "Cloth Config API");
        await wizBackToCore();
        await wizToMods("paper");
        await page.waitForSelector("#mc-wiz-mismatch-card", { timeout: 5000 });
        const text = await page.textContent("#mc-wiz-mismatch-card");
        assert(text.includes("清单里的 1 项是给 Fabric 挑的，换成 Paper 以后可能装不上"), "回头换成 Paper 应提示核心对不上：" + text);
        assert((await cartMock()).depsQueries === 0, "这条提示不联网查前置");
        await page.click("#mc-wiz-mismatch-clear");
        await page.waitForSelector("#mc-empty", { timeout: 5000 });
        assert(!(await page.$("#mc-wiz-mismatch-card")), "清空以后提示应消失");
        assert((await cartMock()).draft.length === 0, "点「清空草稿清单」应清掉草稿清单");
      } finally { await cleanup(); }
    });

    await run("草稿清单：向导回头换核心后点「留着」，提示收起、草稿项留着，再回到这一步不重复提示", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true } });
        await freshWizard();
        await wizToMods("fabric");
        await pick("JEI", "JEI");
        await wizBackToCore();
        await wizToMods("paper");
        await page.waitForSelector("#mc-wiz-mismatch-card", { timeout: 5000 });
        await page.click("#mc-wiz-mismatch-keep"); await settle(200);
        assert(!(await page.$("#mc-wiz-mismatch-card")) && (await page.textContent("#mc-count")).includes("共 1 项"), "点「留着」后提示收起、草稿项还在");
        await wizBackToCore();
        await wizToMods("paper"); await settle(300);
        assert(!(await page.$("#mc-wiz-mismatch-card")), "同一组已经点过「留着」，再回到这一步不该再提示");
        assert((await cartMock()).draft.length === 1, "点「留着」不动草稿清单");
      } finally { await cleanup(); }
    });

    await run("草稿清单：向导里只换了游戏版本，回到选模组这一步也提示清单里的项是给原来那个版本挑的", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true } });
        await freshWizard();
        const v1 = await wizToMods("fabric");
        await pick("JEI", "JEI");
        await page.click('.step-pill.done[data-step="1"]'); await page.waitForSelector(".ver-item.sel");
        const v2 = await page.$$eval("#ver-list .ver-item[data-id]", (els, v) => (els.map(e => e.dataset.id).find(id => id !== v) || ""), v1);
        assert(v2, "仿真后端的版本列表里应有第二个版本");
        await page.click(`.ver-item[data-id="${v2}"]`);
        await page.click("#wnext"); await page.waitForSelector("#build-rec b");
        await page.click("#wnext"); await page.waitForSelector("#mc-wiz-mismatch-card", { timeout: 5000 });
        const text = await page.textContent("#mc-wiz-mismatch-card");
        assert(text.includes(`清单里的 1 项是给 Fabric ${v1} 挑的，换成 Fabric ${v2} 以后可能装不上`), "只换版本也应提示：" + text);
        assert((await page.textContent("#mc-list")).includes(`给 Fabric ${v1} 挑的，版本不一样`), "清单那一行也标出版本不一样");
      } finally { await cleanup(); }
    });

    await run("草稿清单：从电脑添加的 Fabric 模组文件，向导换成 Paper 以后也提示可能装不上", async () => {
      try {
        const lf = { sha1: "a1".repeat(20), fileName: "minimap-fabric-1.0.jar", size: 2048, title: "Minimap", modId: "minimap", version: "1.0", loader: "fabric", addedAt: "2026-11-23T10:00:00Z" };
        await cartState({ instReset: true, modCart: { reset: true, localDraft: [lf] } });
        await freshWizard();
        await wizToMods("fabric"); await settle(300);
        assert(!(await page.$("#mc-wiz-mismatch-card")), "Fabric 的文件放在 Fabric 下不该提示");
        await wizBackToCore();
        await wizToMods("paper");
        await page.waitForSelector("#mc-wiz-mismatch-card", { timeout: 5000 });
        const text = await page.textContent("#mc-wiz-mismatch-card");
        assert(text.includes("清单里的 1 项是给 Fabric 挑的，换成 Paper 以后可能装不上"), "本地文件的加载器对不上也应提示：" + text);
        assert((await page.textContent("#mc-list")).includes("插件服"), "本地文件那一行写明这台是插件服、放进去不会生效");
      } finally { await cleanup(); }
    });

    await run("草稿清单：向导点了「跳过这一步」，建好以后不记目标服务器、不装草稿清单", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
        await freshWizard();
        await wizToMods("fabric");
        await pick("JEI", "JEI");
        await page.click("#wskip"); await page.waitForSelector(".modal [data-x=ok]"); await page.click(".modal [data-x=ok]");
        await page.waitForSelector("#f-name");
        await wizCreate("跳过选模组的服");
        await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        await settle(300);
        const mc = await cartMock();
        assert(mc.draftInst === "" && mc.lastInstall === null, "跳过选模组时不该记目标、不该装：" + JSON.stringify({ draftInst: mc.draftInst, lastInstall: mc.lastInstall }));
        assert(mc.draft.length === 1, "草稿清单留着，以后还能用");
      } finally { await cleanup(); }
    });

    await run("草稿清单：向导回头换成原版，没有选模组这一步，不提示，建好以后不装草稿清单、不记目标服务器", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
        await freshWizard();
        await wizToMods("fabric");
        await pick("JEI", "JEI");
        await wizBackToCore();
        await page.click('.core-card[data-id="vanilla"]');
        await page.click("#wnext"); await page.waitForSelector(".ver-item.sel");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        assert(!(await page.$("#mc-wiz-mismatch-card")), "原版没有选模组这一步，不该提示");
        await wizCreate("换成原版的服");
        await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        await settle(300);
        const mc = await cartMock();
        assert(mc.draftInst === "" && mc.lastInstall === null, "原版服不该记目标、不该装：" + JSON.stringify({ draftInst: mc.draftInst, lastInstall: mc.lastInstall }));
      } finally { await cleanup(); }
    });

    await run("草稿清单：向导发出建服请求以后切走，记下目标服务器；建好以后只有那台的详情页显示「新建时挑的 N 项还没装」", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 2 });
        await freshWizard();
        await wizToMods("paper");
        await pick("Vault", "Vault");
        await wizCreate("切走新建服");
        await waitBuilding();
        assert((await cartMock()).draftInst === "切走新建服", "发出建服请求后草稿清单应记下目标服务器");
        await leaveToList();
        await cartState({ taskStage: 4 }); // 建服在后台跑完
        await openAddons(PACK);
        await settle(300);
        assert(!(await page.$("#mc-draft-leftover")), "别的服务器的详情页不该有这条横幅");
        await openAddons("切走新建服");
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        assert((await page.textContent("#mc-draft-leftover")).includes("新建时挑的 1 项还没装"), "目标服务器的详情页应显示横幅");
      } finally { await cleanup(); }
    });

    /* 记目标服务器是仅人工的写操作，每记一次进一条操作记录：草稿清单是空的时向导不发，免得每次建服都多一条 */
    await run("草稿清单：向导「选模组」这一步什么都没挑就建服，不发记目标服务器的请求", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
        await freshWizard();
        await wizToMods("paper");
        const since = reqs.length;
        await wizCreate("空清单新建服");
        await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        await settle(300);
        const sent = reqs.slice(since).filter(r => r.m === "POST").map(r => r.u);
        assert(sent.includes("/api/instances") && !sent.includes("/api/modcart/draft/target"), "只该发建服请求：" + JSON.stringify(sent));
      } finally { await cleanup(); }
    });

    /* B08：删服务器时仿真后端也清掉这台的清单（同真后端 instance.delete） */
    await run("草稿清单：删掉一台服务器再建一台同名的，看不到上一台留在待装清单里的东西和新建时挑的横幅", async () => {
      try {
        const lf = { sha1: "e5".repeat(20), fileName: "old-own-1.0.jar", size: 2048, title: "Old Own", modId: "oldown", version: "1.0", loader: "forge", addedAt: "2026-11-23T10:00:00Z" };
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("jei", "JEI", "mod")] }, localInsts: { [PACK]: [lf] },
          draft: [item("cloth-config", "Cloth Config API", "mod", "forge", "1.20.1")], draftInst: PACK } });
        await page.goto(url("#/instances")); await settle();
        await page.click(`.card[data-name="${PACK}"] [data-act="more"]`);
        await page.click(".dropmenu button.danger"); await settle(250);
        await page.click('#modal-root [data-x="ok"]');
        await page.waitForFunction(n => !document.querySelector(`.card[data-name="${n}"]`), PACK, { timeout: 5000 });
        const mc = await cartMock();
        assert(!(mc.insts[PACK] || []).length && !(mc.localInsts[PACK] || []).length && mc.draftInst === "", "删掉以后这台的清单和记号都应清掉：" + JSON.stringify(mc));
        assert(mc.draft.length === 1, "草稿清单本身留着");
        await page.evaluate(n => fetch("/api/instances", { method: "POST", headers: { "X-Token": "mock" },
          body: JSON.stringify({ name: n, core: "forge", mc: "1.20.1", port: 25570, eula: true }) }), PACK);
        await openAddons(PACK);
        assert(!!(await page.$("#mc-empty")), "同名新建的那台清单应是空的：" + await page.textContent("#mc-list"));
        assert(!(await page.$("#mc-draft-leftover")), "同名新建的那台不该冒出新建时挑的横幅");
      } finally { await cleanup(); }
    });

    /* A30：cart.json 读不出来时删服，仿真后端也先记下、读出来以后头一次用清单时补清（同真后端 modcart.ForgetInstance） */
    await run("草稿清单：cart.json 读不出来时删掉一台服务器，读出来以后再建同名的，也看不到上一台留下的东西", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("jei", "JEI", "mod")] },
          draft: [item("cloth-config", "Cloth Config API", "mod", "forge", "1.20.1")], draftInst: PACK, loadFail: "corrupt" } });
        const del = await page.evaluate(n => fetch(`/api/instances/${encodeURIComponent(n)}?files=0`, { method: "DELETE", headers: { "X-Token": "mock" } }).then(r => r.status), PACK);
        assert(del === 200, "读不出来时照样能删服：" + del);
        let mc = await cartMock();
        assert((mc.insts[PACK] || []).length === 1 && mc.pendingRemovals.join() === PACK, "读不出来时清不了，先记下：" + JSON.stringify(mc));
        await cartState({ modCart: { loadFail: "" } });
        await page.evaluate(n => fetch("/api/instances", { method: "POST", headers: { "X-Token": "mock" },
          body: JSON.stringify({ name: n, core: "forge", mc: "1.20.1", port: 25570, eula: true }) }), PACK);
        await openAddons(PACK);
        assert(!!(await page.$("#mc-empty")), "同名新建的那台清单应是空的：" + await page.textContent("#mc-list"));
        assert(!(await page.$("#mc-draft-leftover")), "同名新建的那台不该冒出新建时挑的横幅");
        mc = await cartMock();
        assert(!mc.pendingRemovals.length && mc.draftInst === "" && mc.draft.length === 1, "补清以后记录清空、记号清掉、草稿清单留着：" + JSON.stringify(mc));
      } finally { await cleanup(); }
    });

    /* A30：删服时这台的清单清不掉、也记不下来，后端照样删、结果里带 note（同 instance.delete）；界面要让人看得到 */
    await run("删除：待装清单清不掉时删服照常算成功，另给一条带「详情」的提示说这台的东西没清掉", async () => {
      try {
        const why = "待装清单读不出来，也没能记下来等读出来以后再清（拒绝访问）";
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("jei", "JEI", "mod")] }, forgetFail: why } });
        await page.goto(url("#/instances")); await settle();
        await page.click(`.card[data-name="${PACK}"] [data-act="more"]`);
        await page.click(".dropmenu button.danger"); await settle(250);
        await page.click('#modal-root [data-x="ok"]');
        await page.waitForFunction(n => !document.querySelector(`.card[data-name="${n}"]`), PACK, { timeout: 5000 });
        await settle(300);
        const ts = await page.$$eval(".toast", els => els.map(e => ({ err: e.classList.contains("error"), text: e.textContent })));
        assert(ts.some(x => !x.err && x.text.includes("移出列表")), "删服照常算成功：" + JSON.stringify(ts));
        assert(ts.some(x => x.err && x.text.includes(`「${PACK}」待装清单里的东西没清掉`)), "另给一条提示：" + JSON.stringify(ts));
        await page.locator(".toast.error", { hasText: "待装清单里的东西没清掉" }).last().locator('button[aria-label="查看报错详情"]').click();
        await page.waitForSelector(".err-detail");
        assert((await page.textContent(".err-detail")).includes(why), "「详情」里是后端给的原话：" + await page.textContent(".err-detail"));
        await page.click('.modal [data-x="close"]'); await settle(200);
      } finally { await cleanup(); }
    });

    /* 向导建服时 GET /api/modcart 偶发失败：不知道草稿清单空不空，照样记目标（后端在草稿清单真是空的时不记） */
    await run("草稿清单：向导建服时读草稿清单偶发失败，照样记下目标服务器，再去建服任务页", async () => {
      const bareCart = u => u.pathname === "/api/modcart" && !u.search;
      const failGet = route => route.request().method() === "GET"
        ? route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ ok: false, error: "读取待装清单失败" }) })
        : route.continue().catch(() => {});
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
        await freshWizard();
        await wizToMods("paper");
        await pick("Vault", "Vault");
        await page.click("#wnext"); await page.waitForSelector("#f-name");
        await page.route(bareCart, failGet);
        await wizCreate("读清单失败的服");
        await page.waitForFunction(() => location.hash === "#/task/task-1", null, { timeout: 8000 });
        assert(await until(async () => (await cartMock()).draftInst === "读清单失败的服"), "读草稿清单失败时也应记下目标服务器：" + JSON.stringify((await cartMock()).draftInst));
      } finally { await page.unroute(bareCart, failGet).catch(() => {}); await cleanup(); }
    });

    await run("草稿清单：建服请求还没回来就切走，目标服务器照样记上，不报「无法创建」", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 2 });
        await freshWizard();
        await wizToMods("paper");
        await pick("Vault", "Vault");
        await holdPost("**/api/instances", async (requested, release) => {
          await wizCreate("在途切走服");
          await requested;
          await leaveToList();
          release();
          assert(await until(async () => (await cartMock()).draftInst === "在途切走服"), "建服请求回来以后应照样记下目标服务器");
        });
        assert(!(await toastsNow()).some(x => x.includes("无法创建")), "服务器在建，不该报无法创建");
      } finally { await cleanup(); }
    });

    await run("草稿清单：记目标服务器的请求失败，建服照常，建好以后照样装草稿清单，不报「无法创建」", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true }, taskStage: 4 });
        await page.route("**/api/modcart/draft/target", route => route.fulfill({ status: 500, contentType: "application/json",
          body: JSON.stringify({ ok: false, error: "保存待装清单失败：磁盘满了" }) }));
        await freshWizard();
        await wizToMods("paper");
        await pick("Vault", "Vault");
        await wizCreate("记号失败的服");
        await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
        const mc = await cartMock();
        assert(mc.lastInstall && mc.lastInstall.inst === "记号失败的服" && mc.lastInstall.includeDraft === true, "建好以后应照常带着草稿清单装：" + JSON.stringify(mc.lastInstall));
        assert(!(await toastsNow()).some(x => x.includes("无法创建")), "记号没记上不该报无法创建");
      } finally { await cleanup(); }
    });

    await run("草稿清单：详情页横幅先按记下的核心离线标出对不上的项，查过前置以后按 unresolved 标出装不上的；本地文件按这台的核心提醒", async () => {
      try {
        const lf = { sha1: "b2".repeat(20), fileName: "minimap-fabric-1.0.jar", size: 2048, title: "Minimap", modId: "minimap", version: "1.0", loader: "fabric", addedAt: "2026-11-23T10:00:00Z" };
        await cartState({ instReset: true, modCart: { reset: true, draft: [item("jei", "JEI", "mod", "fabric", "1.20.1"), item("vault", "Vault", "plugin")], localDraft: [lf], draftInst: PACK } });
        let releaseDeps;
        const holdDeps = new Promise(r => { releaseDeps = r; });
        await page.route("**/api/modcart/deps*", async route => {
          if (route.request().url().includes("includeDraft=true")) await holdDeps;
          await route.continue().catch(() => {});
        });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        const offline = await page.textContent("#mc-draft-leftover");
        assert(offline.includes("新建时挑的 3 项还没装") && offline.includes("给 Fabric 挑的，核心对不上") && offline.includes("这是插件，核心对不上"),
          "联网结果回来之前应先按记下的核心标出对不上的项：" + offline);
        assert(offline.includes("这个文件是给 Fabric 用的"), "本地文件那一行应按这台的核心提醒：" + offline);
        assert(!(await page.$("#mc-draft-leftover .mc-draft-unres")), "前置检查回来之前不标装不上");
        releaseDeps();
        await page.waitForSelector("#mc-draft-leftover .mc-draft-unres", { timeout: 5000 });
        assert((await page.textContent("#mc-draft-leftover")).includes("装不上：没有支持 forge 的版本"), "前置检查回来以后按 unresolved 标出装不上的");
      } finally { await cleanup(); }
    });

    await run("草稿清单：详情页横幅上点「一起装上」，发出带 includeDraft 的安装，expect 是这台和草稿清单的并集，装完记号清掉", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("cloth-config", "Cloth Config API", "mod")] },
          draft: [item("jei", "JEI", "mod", "forge", "1.20.1")], draftInst: PACK } });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        await page.click("#mc-draft-install");
        await page.waitForSelector(".modal", { timeout: 5000 });
        assert((await page.textContent(".modal")).includes("Architectury API"), "草稿清单里 JEI 的必装前置要先说一声");
        await page.click('.modal [data-x="ok"]');
        await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
        const mc = await cartMock();
        assert(mc.lastInstall && mc.lastInstall.inst === PACK && mc.lastInstall.includeDraft === true && mc.lastInstall.expect.slice().sort().join() === "cloth-config,jei",
          "「一起装上」应带 includeDraft 和两份清单的并集：" + JSON.stringify(mc.lastInstall));
        assert(mc.draftInst === "" && mc.draft.length === 0, "装完草稿清单和记号都应清掉");
      } finally { await cleanup(); }
    });

    /* 同「全部安装」那条（A29）：查前置那几秒里左边的「加入清单」还能点，加进来的那项不在检查结果里，
       检查回来要比出清单变了、重查，确认框列出它的必装前置；不能带着 withDeps 直接装，把它的前置不声不响一起装了（A31） */
    await run("草稿清单：横幅上点「一起装上」、查前置期间往这台的清单里加了带必装前置的项，放行后先列出它的前置再确认，确认前不发安装", async () => {
      try {
        const lf = (sha1, fileName, title) => ({ sha1, fileName, size: 2048, title, modId: title.toLowerCase(), version: "1.0", loader: "forge", addedAt: "2026-11-23T10:00:00Z" });
        const draftLf = lf("c3".repeat(20), "draft-pick-1.0.jar", "Draftpick"), instLf = lf("d4".repeat(20), "own-pick-1.0.jar", "Ownpick");
        await cartState({ instReset: true, modCart: { reset: true, draft: [item("cloth-config", "Cloth Config API", "mod", "forge", "1.20.1")],
          localDraft: [draftLf], localInsts: { [PACK]: [instLf] }, draftInst: PACK } });
        /* 压住带草稿的检查：后端按旧清单算完以后才压，放行时交回的是旧清单的结果 */
        let release, held = 0, released = false;
        const gate = new Promise(r => { release = r; });
        await page.route("**/api/modcart/deps*", async route => {
          if (released || !route.request().url().includes("includeDraft=true")) { await route.continue().catch(() => {}); return; }
          const resp = await route.fetch(); held++; await gate; await route.fulfill({ response: resp }).catch(() => {});
        });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        await page.fill("#mc-q", "JEI"); await page.click("#mc-go"); await settle();
        assert(await until(async () => held >= 1), "打开时那次带草稿的检查应已发出");
        await page.click("#mc-draft-install");
        assert(await until(async () => held >= 2), "点「一起装上」应再发一次带草稿的检查");
        await page.click('button[aria-label="加入清单 JEI"]');
        await page.waitForFunction(() => (document.querySelector("#mc-count")?.textContent || "").includes("共 2 项"), null, { timeout: 5000 });
        released = true; release();
        await page.waitForFunction(() => document.querySelector(".modal") || location.hash.startsWith("#/task/"), null, { timeout: 8000 });
        assert((await cartMock()).lastInstall === null, "确认前不应发出安装请求：" + JSON.stringify((await cartMock()).lastInstall));
        const text = await page.textContent(".modal");
        assert(text.includes("Architectury API"), "放行后应弹出列了 JEI 必装前置的确认框：" + text);
        await page.click('.modal [data-x="ok"]');
        await page.waitForFunction(() => location.hash === "#/task/task-cart", null, { timeout: 8000 });
        const li = (await cartMock()).lastInstall, sorted = a => (a || []).slice().sort().join();
        assert(li && li.includeDraft === true && sorted(li.expect) === "cloth-config,jei", "确认后应按两份清单最新的并集安装：" + JSON.stringify(li));
        assert(sorted(li.expectLocal) === sorted([draftLf.sha1, instLf.sha1]), "expectLocal 应是两份清单里本地文件的并集：" + JSON.stringify(li));
      } finally { await cleanup(); }
    });

    await run("草稿清单：详情页横幅上点「不要了」只清草稿清单，这台自己清单里原有的项还在", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("cloth-config", "Cloth Config API", "mod")] },
          draft: [item("jei", "JEI", "mod", "forge", "1.20.1")], draftInst: PACK } });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        await page.click("#mc-draft-discard");
        await page.waitForFunction(() => !document.querySelector("#mc-draft-leftover"), null, { timeout: 5000 });
        const mc = await cartMock();
        assert(mc.draft.length === 0 && mc.draftInst === "", "草稿清单和记号都应清掉：" + JSON.stringify({ draft: mc.draft, draftInst: mc.draftInst }));
        assert((mc.insts[PACK] || []).map(x => x.projectId).join() === "cloth-config", "这台自己的清单不该动：" + JSON.stringify(mc.insts[PACK]));
        assert((await page.textContent("#mc-list")).includes("Cloth Config API") && (await page.textContent("#mc-count")).includes("共 1 项"), "页面上这台的清单照常列着");
      } finally { await cleanup(); }
    });

    /* 横幅和「全部安装」共用一个「正在安装」：在哪一处的确认框里点「先不装」，三个按钮都要恢复可点（A29、A31） */
    const buttonsEnabled = () => page.evaluate(() => ["#mc-install", "#mc-draft-install", "#mc-draft-discard"]
      .map(s => [s, !!document.querySelector(s) && !document.querySelector(s).disabled]));
    await run("待装清单：横幅在场时点「全部安装」、确认框里点「先不装」，横幅上的两个按钮和「全部安装」都恢复可点", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("jei", "JEI", "mod")] },
          draft: [item("cloth-config", "Cloth Config API", "mod", "forge", "1.20.1")], draftInst: PACK } });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        assert(await until(async () => (await cartMock()).depsQueries >= 1), "横幅那一路的前置检查应先查完");
        const before = (await cartMock()).depsQueries;
        await page.click("#mc-install");
        await page.waitForSelector(".modal", { timeout: 5000 });
        assert((await cartMock()).depsQueries > before, "这台的清单还没查过前置，点「全部安装」应先查一遍");
        await page.click('.modal [data-x="cancel"]'); await settle(300);
        const st = await buttonsEnabled();
        assert(st.every(([, on]) => on), "点「先不装」以后三个按钮都应能点：" + JSON.stringify(st));
        assert((await cartMock()).lastInstall === null, "点「先不装」不该发安装请求");
      } finally { await cleanup(); }
    });

    /* A124：安装请求发出去以后要跳任务页，这一下被离开确认拦下（同一页上「整合包里没装上的」正在分批提交）、人点了「留在这里」，
       还留在这一页：三个按钮不能一直停在禁用 */
    await run("待装清单：点了「全部安装」、跳任务页被离开确认拦下，选「留在这里」以后三个按钮都恢复可点", async () => {
      try {
        await cartState({ instReset: true, taskList: [], trustedHosts: [], instPending: { [PACK]: { pending: pendingItems("q", 1200), skipped: [] } },
          modCart: { reset: true, insts: { [PACK]: [item("cloth-config", "Cloth Config API", "mod")] },
            draft: [item("vault", "Vault", "plugin")], draftInst: PACK } });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        await page.waitForSelector("#ad-pending-apply", { timeout: 5000 });
        await page.click("#ad-pending-box #pnd-trust-all"); await settle(250);
        await page.click("#ad-pending-box #pnd-ack-all"); await settle(250);
        await holdPost("**/import-pending", async requested => {
          await page.click("#ad-pending-apply");
          await requested; // 第 1 批在途，还有 700 项没提交：离开确认登记着
          await page.click("#mc-install");
          await page.waitForSelector(".modal", { timeout: 8000 });
          assert((await page.textContent(".modal")).includes("还有 700 项还没提交"), "跳任务页之前应先问还没提交的那些：" + await page.textContent(".modal"));
          assert((await cartMock()).lastInstall, "安装请求应已发出");
          await page.click('.modal [data-x="cancel"]'); await settle(300);
          assert(await page.evaluate(() => location.hash) === `#/inst/${encodeURIComponent(PACK)}/addons`, "选「留在这里」应还在这一页：" + await page.evaluate(() => location.hash));
          const st = await buttonsEnabled();
          assert(st.every(([, on]) => on), "留在这一页时三个按钮都应能点：" + JSON.stringify(st));
        });
      } finally {
        // holdPost 出来时已经放行：剩下的两批照常发完，离开确认撤掉以后再切页，别把确认框留给下一条用例
        await page.waitForFunction(() => { const b = document.querySelector("#ad-pending-apply"); return !b || !b.disabled; }, null, { timeout: 8000 }).catch(() => {});
        await cartState({ instPending: { [PACK]: { pending: [], skipped: [] } }, taskList: [], trustedHosts: [] });
        await cleanup();
      }
    });

    await run("待装清单：横幅上点「一起装上」、确认框里点「先不装」，「全部安装」和横幅上的按钮都恢复可点", async () => {
      try {
        await cartState({ instReset: true, modCart: { reset: true, insts: { [PACK]: [item("cloth-config", "Cloth Config API", "mod")] },
          draft: [item("jei", "JEI", "mod", "forge", "1.20.1")], draftInst: PACK } });
        await openAddons(PACK);
        await page.waitForSelector("#mc-draft-leftover", { timeout: 5000 });
        await page.click("#mc-draft-install");
        await page.waitForSelector(".modal", { timeout: 5000 });
        await page.click('.modal [data-x="cancel"]'); await settle(300);
        const st = await buttonsEnabled();
        assert(st.every(([, on]) => on), "点「先不装」以后三个按钮都应能点：" + JSON.stringify(st));
        assert((await cartMock()).lastInstall === null, "点「先不装」不该发安装请求");
      } finally { await cleanup(); }
    });
  }

  await run("A104：instance.json 损坏的服务器在列表显示只读提示卡、点卡不跳转，本机可打开文件夹而局域网隐藏", async () => {
    const mState = b => page.evaluate(x => fetch("/__mock/state", { method: "POST", body: JSON.stringify(x) }).then(r => r.json()).then(j => j.data), b);
    const brokenID = "__broken__:badjson1";
    try {
      await mState({
        instReset: true,
        local: true,
        brokenInstances: [
          { id: brokenID, folder: "broken-pack", reason: "设置文件 JSON 格式有误（可能是括号、引号或逗号没写对）" },
        ],
      });
      await page.goto(url("#/instances"));
      await page.waitForSelector(`.card[data-broken="${brokenID}"]`);
      const cardInfo = await page.$eval(`.card[data-broken="${brokenID}"]`, c => ({
        text: c.textContent,
        hasOpen: !!c.querySelector("[data-open-broken]"),
        hasStart: !!c.querySelector('[data-act="start"], [data-act="stop"]'),
        hasMore: !!c.querySelector('[data-act="more"]'),
        hasLink: !!c.querySelector("a"),
        hasName: "name" in c.dataset,
      }));
      assert(
        cardInfo.text.includes("broken-pack") &&
        cardInfo.text.includes("无法读取") &&
        cardInfo.text.includes("这台服务器的设置文件读不出来：设置文件 JSON 格式有误") &&
        cardInfo.hasOpen && !cardInfo.hasStart && !cardInfo.hasMore && !cardInfo.hasLink && !cardInfo.hasName,
        "损坏卡片应显示文件夹名、错误原因与「打开文件夹」，不应有启停/管理/更多操作：" + JSON.stringify(cardInfo),
      );
      await page.click(`.card[data-broken="${brokenID}"] .inst-fail`);
      await settle(200);
      assert(await page.evaluate(() => location.hash) === "#/instances", "点损坏卡片空白处应留在列表页：" + await page.evaluate(() => location.hash));

      await page.click(`.card[data-broken="${brokenID}"] [data-open-broken]`);
      await page.waitForFunction(id => fetch("/__mock/state", { method: "POST", body: "{}" }).then(r => r.json()).then(j => j.data.lastOpenDir?.inst === id), brokenID);

      // 局域网访客：隐藏「打开文件夹」，改提示去主机处理
      await mState({ local: false });
      await page.goto(url("#/instances"));
      await page.waitForSelector(`.card[data-broken="${brokenID}"] .hint`);
      assert(!(await page.$(`.card[data-broken="${brokenID}"] [data-open-broken]`)), "局域网打开时不应显示「打开文件夹」按钮");
      assert((await page.textContent(`.card[data-broken="${brokenID}"]`)).includes("请到主机上打开这个文件夹"), "局域网打开时应提示去主机操作");

      // 只有损坏目录、没有正常服务器时：不显示「还没有服务器」空态
      await mState({ local: true, instances: [] });
      await page.goto(url("#/instances"));
      await page.waitForSelector(`.card[data-broken="${brokenID}"]`);
      assert(!(await page.$("#inst-cards .empty")), "存在损坏目录时不应落到「还没有服务器」空态");
      assert((await page.textContent("#inst-summary")).includes("1 个文件夹配置无法读取"), "页顶摘要应列出配置无法读取的个数");
    } finally {
      await mState({ instReset: true, local: true });
    }
  });

  await run("全程无页面报错", async () => { assert(errors.length === 0, errors.slice(0, 3).join(" | ")); });

  await browser.close();
  await mock.close();
  const fails = results.filter(r => r[0] === "FAIL").length;
  if (fails) {
    console.log("\n--- 失败汇总 ---");
    for (const r of results) if (r[0] === "FAIL") console.log(r.join("  "));
  }
  console.log(`\n${results.length - fails}/${results.length} passed`);
  process.exit(fails ? 1 : 0);
})().catch(e => { console.error(e); process.exit(2); });
