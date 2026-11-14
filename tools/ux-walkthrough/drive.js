// 像用户一样操作测试副本里的 MineDeck 窗口：看屏幕（截图）、点、悬停、打字、按键、滚动、拖动、选文件。
// 每做一步都截一张图、在证据文件夹的 actions.jsonl 里记一行。这个脚本不给页面地址，也不调程序接口。
// 用法：node D:\MineDeck-ux\rig\drive.js <first|return> <命令> [参数…]   （命令清单见文件末尾的 HELP，或运行 help）
const { chromium } = require('E:/Archive_files/MineDeck/tools/ui-dev/node_modules/playwright');
const fs = require('fs'), path = require('path'), cp = require('child_process');

const HELP = `命令（坐标都是截图里的像素位置，截图和窗口内容一比一）：
  shot [备注]                 只截图，看现在屏幕上是什么
  click X Y [备注]            左键单击；点到下拉框时不会真点开，而是列出下拉框里的选项（见 choose）
  dbl X Y                     双击
  hover X Y                   把鼠标停在那里约 1.5 秒；有系统悬停提示（title）时把提示文字报出来
  type 文字                   往当前有输入焦点的地方打字（先 click 输入框）
  key 键                      按键：Enter、Escape、Tab、Backspace、Delete、ArrowDown、Control+a、Shift+Tab……
  scroll X Y 量               在 (X,Y) 处滚动鼠标滚轮，量为像素，正数往下、负数往上，例如 scroll 600 400 500
  drag X1 Y1 X2 Y2            按住鼠标从一处拖到另一处（滑块之类）
  choose X Y [选项文字]       (X,Y) 处是下拉框：不带选项文字时列出它的所有选项（相当于点开看）；带上就选中那一项
  upload X Y 文件路径 [...]   点 (X,Y) 处会弹出「打开文件」对话框的按钮/框，并在对话框里选中这些文件
  find 文字                   在当前屏幕上能看见的文字里找，报出位置（只找看得见的字，帮你点准）
  wait 秒                     等几秒再截图（下载、启动这类要等的事）
  oswin                       程序有没有开着系统窗口（比如「选择文件夹」对话框）；有就给它截图
  osdlg pick 文件夹路径       在系统「选择文件夹」对话框里填上这个文件夹并点确定
  osdlg cancel                在系统对话框里点取消
  popups                      列出程序另外弹出的小窗口（网页小窗）；closepopups 把它们关掉
  showwindow                  主窗口缩到托盘以后，相当于点一下托盘图标把它叫回来
  note 文字                   在记录里写一句话（比如「打算去哪找」「这里犹豫了」），不操作界面
备注写在最后，可以省略。打字、按键、滚动之后也会自动截图。`;

const copy = process.argv[2];
const cmd = process.argv[3];
const args = process.argv.slice(4);
if (!copy || !cmd || cmd === 'help') { console.log(HELP); process.exit(0); }
const rig = __dirname;
const st = JSON.parse(fs.readFileSync(path.join(rig, `state-${copy}.json`), 'utf8').replace(/^\uFEFF/, ''));
const OUT = st.out, SHOTS = path.join(OUT, 'shots');
fs.mkdirSync(SHOTS, { recursive: true });
const ctrFile = path.join(OUT, 'counter.txt');
const n = (fs.existsSync(ctrFile) ? parseInt(fs.readFileSync(ctrFile, 'utf8'), 10) || 0 : 0) + 1;
fs.writeFileSync(ctrFile, String(n));
const tag = String(n).padStart(4, '0');
const sleep = ms => new Promise(r => setTimeout(r, ms));
const logRow = row => fs.appendFileSync(path.join(OUT, 'actions.jsonl'), JSON.stringify({ n, at: new Date().toISOString(), cmd, args, ...row }) + '\n');
const ps = (a) => {
  const r = cp.spawnSync('pwsh', ['-NoProfile', '-File', path.join(rig, 'osdlg.ps1'), ...a], { encoding: 'utf8', timeout: 60000 });
  return ((r.stdout || '') + (r.stderr || '')).trim();
};
const num = (v, name) => { const x = Number(v); if (!Number.isFinite(x)) throw new Error(`${name} 不是数字：${v}`); return x; };

function osWindows() {
  try {
    const raw = fs.readFileSync(path.join(rig, `state-${copy}-oswin.json`), 'utf8').replace(/^\uFEFF/, '');
    const rows = JSON.parse(raw || '[]');
    return (Array.isArray(rows) ? rows : [rows]).filter(w => w && w.cls === '#32770');
  } catch { return []; }
}

(async () => {
  if (cmd === 'note') { const text = args.join(' '); logRow({ note: text }); console.log(`#${tag} 已记下：${text}`); return; }
  if (cmd === 'osdlg') {
    const sub = args[0];
    let res;
    if (sub === 'pick') res = ps(['-Action', 'pick', '-Copy', copy, '-Path', args.slice(1).join(' ')]);
    else if (sub === 'cancel') res = ps(['-Action', 'cancel', '-Copy', copy]);
    else throw new Error('osdlg 后面要跟 pick 文件夹路径 或 cancel');
    await sleep(1500);
    console.log(`#${tag} ${res}`);
    // fall through to a screenshot of the main window below
    await shotMain(res);
    return;
  }
  if (cmd === 'showwindow') { const res = ps(['-Action', 'showmain', '-Copy', copy]); await sleep(1200); console.log(`#${tag} ${res}`); await shotMain(res); return; }
  if (cmd === 'oswin') {
    const ws = osWindows();
    if (!ws.length) { logRow({ result: 'no system dialog' }); console.log(`#${tag} 程序现在没有开着系统对话框。`); return; }
    const lines = [];
    for (const [i, w] of ws.entries()) {
      const f = path.join(SHOTS, `${tag}-oswin-${i + 1}.png`);
      const r = ps(['-Action', 'shot', '-Copy', copy, '-Hwnd', String(w.hwnd), '-File', f]);
      lines.push(`系统窗口 ${i + 1}：标题「${w.title}」，大小 ${w.size}，截图 ${fs.existsSync(f) ? f : '（没截成：' + r + '）'}`);
    }
    logRow({ result: lines });
    console.log(`#${tag}\n` + lines.join('\n'));
    return;
  }
  await shotMain(null);
})().catch(e => { logRow({ error: String(e && e.message || e) }); console.log(`#${tag} 出错：${String(e && e.message || e)}`); process.exitCode = 1; });

async function shotMain(prefix) {
  const browser = await chromium.connectOverCDP(`http://127.0.0.1:${st.cdpPort}`, { timeout: 15000 });
  try {
    const ctx = browser.contexts()[0];
    const app = `http://127.0.0.1:${st.appPort}`;
    let page = null;
    for (let i = 0; i < 40 && !page; i++) { page = ctx.pages().find(p => p.url().startsWith(app)); if (!page) await sleep(250); }
    if (!page) throw new Error('找不到 MineDeck 的主窗口（程序是不是关了？可以运行 start.ps1 重新打开）');
    const pagesBefore = ctx.pages().length;
    const extra = [];
    let settle = 900;
    const at = async (x, y) => page.evaluate(([x, y]) => {
      const el = document.elementFromPoint(x, y);
      if (!el) return null;
      const sel = el.closest('select');
      let title = '';
      for (let e = el, k = 0; e && k < 4; e = e.parentElement, k++) if (e.getAttribute && e.getAttribute('title')) { title = e.getAttribute('title'); break; }
      return { tag: el.tagName, isSelect: !!sel, title };
    }, [x, y]);
    const listOptions = async (x, y) => page.evaluate(([x, y]) => {
      const el = document.elementFromPoint(x, y); const s = el && el.closest('select');
      if (!s) return null;
      return { disabled: s.disabled, current: s.selectedOptions[0] ? s.selectedOptions[0].textContent.trim() : '', options: [...s.options].map(o => (o.disabled ? '（不可选）' : '') + o.textContent.trim()) };
    }, [x, y]);

    switch (cmd) {
      case 'shot': settle = 200; break;
      case 'click': case 'dbl': {
        const x = num(args[0], 'X'), y = num(args[1], 'Y');
        const info = await at(x, y);
        if (info && info.isSelect && cmd === 'click') {
          const o = await listOptions(x, y);
          extra.push(o.disabled ? '这是一个下拉框，现在是灰的（不能选）。' : `这是一个下拉框，现在选的是「${o.current}」。点开能看到这些选项：\n  ` + o.options.join('\n  ') + '\n要选其中一项用：choose X Y 选项文字');
          settle = 200; break;
        }
        if (cmd === 'click') await page.mouse.click(x, y); else await page.mouse.dblclick(x, y);
        break;
      }
      case 'hover': {
        const x = num(args[0], 'X'), y = num(args[1], 'Y');
        await page.mouse.move(x, y, { steps: 4 });
        await sleep(1500);
        const info = await at(x, y);
        if (info && info.title) extra.push(`悬停提示（系统小黄条，截图里看不到）：「${info.title}」`); else extra.push('没有系统悬停提示。');
        settle = 200; break;
      }
      case 'type': await page.keyboard.type(args.join(' '), { delay: 20 }); settle = 600; break;
      case 'key': await page.keyboard.press(args[0]); break;
      case 'scroll': {
        const x = num(args[0], 'X'), y = num(args[1], 'Y'), dy = num(args[2] || '400', '量');
        await page.mouse.move(x, y); await page.mouse.wheel(0, dy); settle = 700; break;
      }
      case 'drag': {
        const [x1, y1, x2, y2] = args.slice(0, 4).map((v, i) => num(v, '坐标' + (i + 1)));
        await page.mouse.move(x1, y1); await page.mouse.down(); await page.mouse.move(x2, y2, { steps: 12 }); await page.mouse.up(); break;
      }
      case 'choose': {
        const x = num(args[0], 'X'), y = num(args[1], 'Y'); const want = args.slice(2).join(' ').trim();
        const o = await listOptions(x, y);
        if (!o) { extra.push('这个位置不是下拉框。'); settle = 200; break; }
        if (!want) { extra.push(o.disabled ? '下拉框是灰的（不能选）。' : `下拉框现在选的是「${o.current}」。选项：\n  ` + o.options.join('\n  ')); settle = 200; break; }
        if (o.disabled) { extra.push('下拉框是灰的（不能选），没选成。'); settle = 200; break; }
        const handle = await page.evaluateHandle(([x, y]) => document.elementFromPoint(x, y).closest('select'), [x, y]);
        const opts = await handle.evaluate(s => [...s.options].map(o => ({ v: o.value, t: o.textContent.trim(), d: o.disabled })));
        const hit = opts.find(o => o.t === want) || opts.find(o => o.t.includes(want));
        if (!hit) { extra.push(`没有叫「${want}」的选项。现有：` + opts.map(o => o.t).join(' / ')); settle = 200; break; }
        if (hit.d) { extra.push(`「${hit.t}」是不可选的。`); settle = 200; break; }
        await handle.asElement().selectOption(hit.v);
        extra.push(`选中了「${hit.t}」`);
        break;
      }
      case 'upload': {
        const x = num(args[0], 'X'), y = num(args[1], 'Y'); const files = args.slice(2);
        if (!files.length) throw new Error('upload 后面要给文件路径');
        for (const f of files) if (!fs.existsSync(f)) throw new Error('文件不存在：' + f);
        const wait = page.waitForEvent('filechooser', { timeout: 4000 }).catch(() => null);
        await sleep(600); // 2026-11-14 第二轮：不等拦截生效就点，系统「打开」对话框会真弹出来（归 WebView2 进程），驱动看不到
        await page.mouse.click(x, y);
        const fc = await wait;
        if (!fc) extra.push('点了以后没有弹出「打开文件」对话框。');
        else { await fc.setFiles(files); extra.push(`在「打开文件」对话框里选中了：${files.join('、')}`); }
        settle = 1200; break;
      }
      case 'find': {
        const q = args.join(' ').trim();
        const hits = await page.evaluate(q => {
          const vis = el => { for (let e = el; e && e !== document.documentElement; e = e.parentElement) { const s = getComputedStyle(e); if (s.display === 'none' || s.visibility === 'hidden' || Number(s.opacity) < 0.05) return false; } return true; };
          const out = []; const W = innerWidth, H = innerHeight;
          const tw = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
          for (let t; (t = tw.nextNode());) {
            const txt = t.textContent.replace(/\s+/g, ' ').trim(); if (!txt || !txt.includes(q)) continue;
            const el = t.parentElement; if (!el || !vis(el)) continue;
            const r = document.createRange(); r.selectNodeContents(t); const b = r.getBoundingClientRect();
            if (b.width < 1 || b.height < 1 || b.right < 0 || b.bottom < 0 || b.left > W || b.top > H) continue;
            const cx = Math.round(Math.min(Math.max(b.left + b.width / 2, 0), W - 1)), cy = Math.round(Math.min(Math.max(b.top + b.height / 2, 0), H - 1));
            const top = document.elementFromPoint(cx, cy); if (top && !(top === el || el.contains(top) || top.contains(el))) continue;
            out.push({ text: txt.slice(0, 60), x: cx, y: cy });
          }
          for (const inp of document.querySelectorAll('input,textarea')) {
            const txt = (inp.value || inp.placeholder || ''); if (!txt.includes(q) || !vis(inp)) continue;
            const b = inp.getBoundingClientRect(); if (b.width < 1 || b.bottom < 0 || b.top > H) continue;
            out.push({ text: (inp.value ? '输入框里填着：' : '输入框的灰字提示：') + txt.slice(0, 50), x: Math.round(b.left + b.width / 2), y: Math.round(b.top + b.height / 2) });
          }
          return out.slice(0, 20);
        }, q);
        extra.push(hits.length ? hits.map(h => `「${h.text}」在 (${h.x}, ${h.y})`).join('\n') : `屏幕上看不到「${q}」这几个字（可能要滚动，或者在别的页面）。`);
        settle = 150; break;
      }
      case 'wait': await sleep(num(args[0] || '3', '秒') * 1000); settle = 100; break;
      case 'popups': case 'closepopups': {
        const pops = ctx.pages().filter(p => p !== page);
        if (!pops.length) extra.push('没有另外弹出的小窗口。');
        for (const [i, p] of pops.entries()) {
          const f = path.join(SHOTS, `${tag}-popup-${i + 1}.png`);
          let t = ''; try { t = await p.title(); } catch {}
          try { await p.screenshot({ path: f, scale: 'css' }); } catch {}
          extra.push(`小窗口 ${i + 1}：标题「${t}」，截图 ${f}`);
          if (cmd === 'closepopups') { await p.close().catch(() => {}); extra.push('  已关掉'); }
        }
        settle = 300; break;
      }
      case 'osdlg': case 'showwindow': settle = 200; break;
      default: throw new Error('不认识的命令：' + cmd + '\n' + HELP);
    }
    await sleep(settle);
    const file = path.join(SHOTS, `${tag}-${cmd}.png`);
    await page.screenshot({ path: file, scale: 'css' });
    const vp = page.viewportSize() || await page.evaluate(() => ({ width: innerWidth, height: innerHeight }));
    const pagesAfter = ctx.pages().length;
    if (pagesAfter > pagesBefore) extra.push(`注意：程序另外弹出了 ${pagesAfter - pagesBefore} 个小窗口（运行 popups 看）。`);
    const ws = osWindows();
    if (ws.length) extra.push(`注意：程序开着系统对话框「${ws.map(w => w.title).join('」「')}」，主窗口的截图里看不到它。运行 oswin 给它截图，osdlg pick/cancel 操作它。`);
    const note = (cmd === 'click' || cmd === 'dbl' || cmd === 'shot') ? args.slice(cmd === 'shot' ? 0 : 2).join(' ') : '';
    logRow({ shot: file, viewport: vp, extra, note: note || undefined, prefix: prefix || undefined });
    console.log(`#${tag} 截图 ${file}（${vp.width}x${vp.height}）` + (extra.length ? '\n' + extra.join('\n') : ''));
  } finally {
    await browser.close().catch(() => {});
  }
}
