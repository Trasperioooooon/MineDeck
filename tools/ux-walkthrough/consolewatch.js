// 后台记录测试窗口页面里的报错（console error / warning、未捕获的异常），写进证据文件夹的 console.log。
// 只给第二轮查原因用，走查的人不用看它。
const { chromium } = require('E:/Archive_files/MineDeck/tools/ui-dev/node_modules/playwright');
const fs = require('fs'), path = require('path');
const copy = process.argv[2];
const st = JSON.parse(fs.readFileSync(path.join(__dirname, `state-${copy}.json`), 'utf8').replace(/^\uFEFF/, ''));
const out = path.join(st.out, 'console.log');
const w = s => fs.appendFileSync(out, `[${new Date().toISOString()}] ${s}\n`);
(async () => {
  const browser = await chromium.connectOverCDP(`http://127.0.0.1:${st.cdpPort}`);
  const ctx = browser.contexts()[0];
  const hook = p => {
    p.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') w(`console.${m.type()}: ${m.text().replace(/token=[\w-]+/g, 'token=<hidden>')}`); });
    p.on('pageerror', e => w(`pageerror: ${String(e && e.stack || e).replace(/token=[\w-]+/g, 'token=<hidden>')}`));
  };
  ctx.pages().forEach(hook);
  ctx.on('page', p => { w('new window opened'); hook(p); });
  browser.on('disconnected', () => process.exit(0));
  w('watching');
})().catch(e => { w('watcher failed: ' + e); process.exit(1); });
