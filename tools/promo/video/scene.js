/* MineDeck 宣传视频的全部画面。window.renderAt(t) 只依赖 t（秒），同一个 t 永远画出同一帧。
   时间轴写在 captions.js 的节拍网格上：T(小节, 拍)，小节从 1 数、拍从 0 数。
   截图视图（ShotView）用截图本身的 CSS 像素（桌面 1280×800、手机 390×844）当坐标：镜头 = 把截图上的哪一点放到视图中心、放大多少倍；
   光标、高亮框也写在截图坐标里，由镜头换算到画面上，所以线宽不随镜头缩放。 */
"use strict";
(function () {
  const P = window.PROMO;
  const { T, TEXT, CAPTIONS, CUTS, DURATION } = P;
  const W = 2560, H = 1440;
  const qs = new URLSearchParams(location.search);
  const stage = document.getElementById("stage");

  // ---------- 小工具 ----------
  function bez(x1, y1, x2, y2) { // CSS cubic-bezier
    const cx = 3 * x1, bx = 3 * (x2 - x1) - cx, ax = 1 - cx - bx, cy = 3 * y1, by = 3 * (y2 - y1) - cy, ay = 1 - cy - by;
    const sx = u => ((ax * u + bx) * u + cx) * u, sy = u => ((ay * u + by) * u + cy) * u, dx = u => (3 * ax * u + 2 * bx) * u + cx;
    return x => {
      if (x <= 0) return 0; if (x >= 1) return 1;
      let u = x;
      for (let i = 0; i < 8; i++) { const e = sx(u) - x, d = dx(u); if (Math.abs(e) < 1e-6 || Math.abs(d) < 1e-6) break; u -= e / d; }
      let lo = 0, hi = 1;
      for (let i = 0; i < 30 && Math.abs(sx(u) - x) > 1e-6; i++) { if (sx(u) < x) lo = u; else hi = u; u = (lo + hi) / 2; }
      return sy(u);
    };
  }
  // 缓动令牌：design-system/MASTER.md §5.1
  const E = { out: bez(.22, 1, .36, 1), in: bez(.55, 0, .75, .2), io: bez(.4, 0, .2, 1), spring: bez(.34, 1.36, .64, 1), lin: x => x,
    cam: bez(.45, 0, .25, 1) };
  const clamp = (x, a = 0, b = 1) => Math.min(b, Math.max(a, x));
  const lerp = (a, b, u) => a + (b - a) * u;
  const prog = (t, t0, dur, ease = E.lin) => ease(clamp((t - t0) / dur));
  // 进场 / 离场合成的可见度：t0 起 din 秒进，t1 前 dout 秒出
  const vis = (t, t0, t1, din = .3, dout = .2) => t < t0 || t >= t1 ? 0 : Math.min(prog(t, t0, din, E.out), 1 - prog(t, t1 - dout, dout, E.in));
  function el(tag, cls, parent, html) { const e = document.createElement(tag); if (cls) e.className = cls; if (html != null) e.innerHTML = html; if (parent) parent.appendChild(e); return e; }
  const css = (e, o) => { for (const k in o) e.style[k] = o[k]; return e; };
  const show = (e, on) => { const v = on ? "" : "none"; if (e.style.display !== v) e.style.display = v; };
  // 关键帧：[[t, {值}, 缓动], …]；z 按对数插值（推拉镜头更匀）
  function track(keys) {
    return t => {
      if (t <= keys[0][0]) return keys[0][1];
      for (let i = 1; i < keys.length; i++) {
        const [t1, v1, e] = keys[i];
        if (t < t1) {
          const [t0, v0] = keys[i - 1], u = (e || E.cam)((t - t0) / (t1 - t0)), o = {};
          for (const k in v1) o[k] = k === "z" ? v0[k] * Math.pow(v1[k] / v0[k], u) : lerp(v0[k], v1[k], u);
          return o;
        }
      }
      return keys[keys.length - 1][1];
    };
  }
  function rng(seed) { let s = seed >>> 0 || 1; return () => { s ^= s << 13; s >>>= 0; s ^= s >> 17; s ^= s << 5; s >>>= 0; return s / 4294967296; }; }
  const hash = (a, b, c = 0) => { let h = (a * 374761393 + b * 668265263 + c * 2147483647) >>> 0; h = ((h ^ (h >> 13)) * 1274126177) >>> 0; return ((h ^ (h >> 16)) >>> 0) / 4294967296; };
  const CHECK_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="3.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>';
  const CURSOR_SVG = '<svg viewBox="0 0 24 24" width="60" height="60"><path d="M4 2.5v17.2l4.4-4.1 3.1 6.8 3.2-1.4-3.1-6.7h6.1z" fill="#fff" stroke="#151515" stroke-width="1.5" stroke-linejoin="round"/></svg>';
  const SEARCH_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5L21 21"/></svg>';
  const GLOBE_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.6 2.6 3.8 5.6 3.8 9s-1.2 6.4-3.8 9c-2.6-2.6-3.8-5.6-3.8-9S9.4 5.6 12 3z"/></svg>';

  // ---------- 素材 ----------
  let RECTS = {}, ICON = null;
  const imgs = {};
  const shotURL = n => `/out/shots/${n}.png`;
  function useShot(name) { if (!imgs[name]) imgs[name] = shotURL(name); return imgs[name]; }

  // ---------- 场景 ----------
  const scenes = [];
  function scene(t0, t1, build) {
    const root = el("div", "scene", sceneLayer);
    const s = { t0, t1, root, render: () => {} };
    s.render = build(root, s) || s.render;
    scenes.push(s);
    return s;
  }

  // 截图视图
  function ShotView(parent, o) {
    const size = o.size || [1280, 800];
    const vp = o.vp || { x: 0, y: 0, w: W, h: H };
    const box = el("div", "sv" + (o.vp ? " clip" : ""), parent);
    css(box, { left: vp.x + "px", top: vp.y + "px", width: vp.w + "px", height: vp.h + "px", borderRadius: (vp.r || 0) + "px" });
    const win = el("div", "win", box);
    css(win, { width: size[0] + "px", height: size[1] + "px" });
    if (o.vp) win.style.boxShadow = "none";
    const names = [...new Set(o.shots.map(s => s[1]))];
    const im = {};
    for (const n of names) { const i = el("img", "", win); i.decoding = "sync"; i.src = useShot(n); i.alt = ""; im[n] = i; }
    const cam = track(o.cam);
    const marks = (o.marks || []).map(m => ({ ...m, spotEl: m.spot ? el("div", "spot", box) : null, el: m.noRing ? null : el("div", "mark", box) }));
    const cur = o.cursor ? track(o.cursor) : null;
    const cursorEl = cur ? el("div", "cursor", box, CURSOR_SVG) : null;
    const ripples = (o.clicks || []).map(tc => ({ tc, el: el("div", o.tap ? "tap" : "ripple", box) }));
    const view = {
      box, win, z: 1, tx: 0, ty: 0,
      toStage: (x, y) => [view.tx + x * view.z, view.ty + y * view.z],
      render(t) {
        const c = cam(t);
        view.z = c.z; view.tx = vp.w / 2 - c.x * c.z; view.ty = vp.h / 2 - c.y * c.z;
        win.style.transform = `translate(${view.tx.toFixed(2)}px, ${view.ty.toFixed(2)}px) scale(${c.z.toFixed(5)})`;
        let active = o.shots[0][1];
        for (const [ts, n] of o.shots) if (t >= ts) active = n;
        for (const n of names) show(im[n], n === active);
        for (const m of marks) {
          const a = vis(t, m.t0, m.t1, .28, .22);
          const on = a > 0.001;
          if (m.el) show(m.el, on);
          if (m.spotEl) show(m.spotEl, on);
          if (!on) continue;
          const padX = (m.padX ?? m.pad ?? 8) * view.z / 1.8, padY = (m.padY ?? m.pad ?? 8) * view.z / 1.8;
          const [x0, y0] = view.toStage(m.r.x, m.r.y);
          const w = m.r.w * view.z + 2 * padX, h = m.r.h * view.z + 2 * padY;
          const sc = 1 + (1 - prog(t, m.t0, .32, E.out)) * .04;
          const tr = `translate(${(x0 - padX).toFixed(1)}px, ${(y0 - padY).toFixed(1)}px)`;
          const geo = { width: w.toFixed(1) + "px", height: h.toFixed(1) + "px", borderRadius: (m.radius ?? 18) + "px" };
          if (m.el) css(m.el, { ...geo, transform: tr + ` scale(${sc.toFixed(4)})`, transformOrigin: "50% 50%", opacity: a.toFixed(3) });
          if (m.spotEl) css(m.spotEl, { ...geo, transform: tr, opacity: (a * (m.spotA ?? 1)).toFixed(3) });
        }
        if (cursorEl) {
          const [ci, co] = o.cursorOn || [o.cursor[0][0], o.cursor[o.cursor.length - 1][0] + 1];
          const a = vis(t, ci, co, .2, .2);
          show(cursorEl, a > 0.001);
          if (a > 0.001) {
            const p = cur(t), [sx, sy] = view.toStage(p.x, p.y);
            let sq = 1;
            for (const r of ripples) { const d = t - r.tc; if (d > -0.06 && d < 0.22) sq = Math.min(sq, 1 - 0.14 * Math.sin(clamp((d + 0.06) / 0.28) * Math.PI)); }
            css(cursorEl, { transform: `translate(${(sx - 9).toFixed(1)}px, ${(sy - 5).toFixed(1)}px) scale(${sq.toFixed(3)})`, opacity: a.toFixed(3) });
          }
        }
        for (const r of ripples) {
          const d = t - r.tc, on = d >= 0 && d < .55;
          show(r.el, on);
          if (!on) continue;
          const p = (o.tapAt && o.tapAt[r.tc]) || (cur ? cur(r.tc) : { x: 0, y: 0 });
          const [sx, sy] = view.toStage(p.x, p.y);
          const u = E.out(d / .55), rad = 18 + 70 * u;
          css(r.el, { width: rad * 2 + "px", height: rad * 2 + "px", transform: `translate(${(sx - rad).toFixed(1)}px, ${(sy - rad).toFixed(1)}px)`, opacity: (1 - E.in(d / .55)).toFixed(3) });
        }
      },
    };
    return view;
  }
  const R = (shot, key) => { const r = RECTS[shot] && RECTS[shot][key]; if (!r) throw new Error(`rect ${shot}.${key} missing`); return r; };
  const C = r => ({ x: r.x + r.w / 2, y: r.y + r.h / 2 });

  // ---------- 背景 ----------
  const bg = el("div", "layer", stage);
  const bgGrid = el("div", "", bg); bgGrid.id = "bg-grid";
  el("div", "", bg).id = "bg-glow";
  el("div", "", bg).id = "bg-mask";
  const decoLayer = el("div", "layer", bg);
  const deco = [];
  { const r = rng(7); for (let i = 0; i < 26; i++) deco.push({ x: r() * W, y: r() * H, s: 14 + Math.floor(r() * 3) * 10, sp: 10 + r() * 22, c: ["#6cbf3a", "#4c9a2c", "#93613a", "#c9bf9f"][Math.floor(r() * 4)], a: .18 + r() * .22, el: el("div", "px-deco", decoLayer) }); }
  function renderBg(t, decoOn) {
    bgGrid.style.transform = `translate(${(-(t * 10) % 80).toFixed(2)}px, ${(-(t * 6) % 80).toFixed(2)}px)`;
    show(decoLayer, decoOn > 0);
    if (decoOn > 0) for (const d of deco) {
      const y = ((d.y - t * d.sp) % (H + 100) + H + 100) % (H + 100) - 50;
      css(d.el, { width: d.s + "px", height: d.s + "px", background: d.c, opacity: (d.a * decoOn).toFixed(3), transform: `translate(${d.x.toFixed(1)}px, ${y.toFixed(1)}px)` });
    }
  }

  const sceneLayer = el("div", "layer", stage); // 场景在背景之上、字幕和转场之下

  // ---------- 图标方块 ----------
  function IconBlocks(parent, cell, cx, top) {
    const box = el("div", "icon-box", parent);
    css(box, { left: (cx - cell * 8) + "px", top: top + "px", width: cell * 16 + "px", height: cell * 16 + "px" });
    const bl = ICON.cells.map(c => { const e = el("i", "", box); css(e, { width: cell + 0.6 + "px", height: cell + 0.6 + "px", background: c.c }); return { ...c, e }; });
    return {
      box, cell,
      render(t, t0, rowGap, fall, drop) {
        for (const b of bl) {
          const d = t0 + (15 - b.y) * rowGap + hash(b.x, b.y) * rowGap * .9;
          const u = clamp((t - d) / fall);
          const yy = (1 - E.spring(u)) * -drop;
          css(b.e, { transform: `translate(${b.x * cell}px, ${(b.y * cell + yy).toFixed(1)}px)`, opacity: clamp(u * 3).toFixed(3) });
        }
      },
    };
  }

  // ---------- 字幕 ----------
  const capLayer = el("div", "layer", stage);
  const caps = CAPTIONS.map(c => {
    const e = el("div", "cap", capLayer);
    if (c.eb) el("div", "eb", e, `<i></i><span>${c.eb}</span>`);
    el("div", "main", e).textContent = c.main;
    if (c.sub) el("div", "sub", e).textContent = c.sub;
    if (c.chips) { const ch = el("div", "chips", e); for (const s of c.chips) el("span", "", ch).textContent = s; }
    e.dataset.text = "cap";
    return { ...c, e };
  });
  function renderCaps(t) {
    for (const c of caps) {
      const a = vis(t, c.t0, c.t1, .32, .2);
      show(c.e, a > 0.001);
      if (a <= 0.001) continue;
      const inU = prog(t, c.t0, .36, E.out), outU = prog(t, c.t1 - .2, .2, E.in);
      css(c.e, { opacity: a.toFixed(3), transform: `translateY(${((1 - inU) * 28 - outU * 10).toFixed(1)}px)` });
    }
  }

  // ---------- 像素擦除（分段之间，落在小节线上） ----------
  const wipeLayer = el("div", "layer", stage);
  const COLS = 16, ROWS = 9, BS = 160;
  const GREENS = ["#6cbf3a", "#5aaa33", "#4c9a2c", "#3e8f2a", "#367d24", "#5b7b31"];
  const wblocks = [];
  for (let r = 0; r < ROWS; r++) for (let c = 0; c < COLS; c++) {
    const e = el("div", "wb", wipeLayer);
    e.style.background = GREENS[Math.floor(hash(c, r, 3) * GREENS.length)];
    wblocks.push({ c, r, e, j: hash(c, r, 11) });
  }
  function renderWipe(t) {
    const cut = CUTS.find(c => t >= c - .42 && t < c + .42);
    show(wipeLayer, cut != null);
    if (cut == null) return;
    for (const b of wblocks) {
      const d = .22 * (b.c / (COLS - 1)) + .05 * b.j + .03 * (b.r / (ROWS - 1));
      let s;
      if (t < cut) s = E.out(clamp((t - (cut - .4 + d)) / .14));
      else s = 1 - E.in(clamp((t - (cut + .02 + d)) / .14));
      show(b.e, s > 0.001);
      if (s > 0.001) b.e.style.transform = `translate(${b.c * BS - 1}px, ${b.r * BS - 1}px) scale(${s.toFixed(4)})`;
    }
  }

  // =====================================================================
  // 时间轴
  // =====================================================================
  function build() {
    // ---- A 开场（第 1–3 小节，0–6 秒）----
    scene(0, T(4), (root) => {
      const q = el("div", "hook-q", root);
      const chars = [...TEXT.hookQ].map(ch => { const s = el("span", "", q); s.textContent = ch; return s; });
      q.dataset.text = "hook";
      const icon = IconBlocks(root, 26, W / 2, 286);
      const word = el("div", "wordmark", root); word.textContent = TEXT.word; css(word, { top: "750px", fontSize: "210px" });
      const tag = el("div", "tagline", root); css(tag, { top: "1020px" });
      tag.innerHTML = TEXT.hookTag.replace("Windows 一键开服工具", "Windows <b>一键开服</b>工具");
      tag.dataset.text = "tag";
      // 落定时向外弹出的小方块
      const burst = []; { const r = rng(21); for (let i = 0; i < 18; i++) { const a = (i / 18) * Math.PI * 2 + r() * .3; burst.push({ a, d: 260 + r() * 140, s: 14 + Math.floor(r() * 2) * 8, c: GREENS[i % GREENS.length], e: el("i", "px-deco", root) }); } }
      return t => {
        // 提问：逐字进场，第 1 小节末离场
        const qOut = prog(t, T(1, 3.5), .25, E.in);
        show(q, t < T(2));
        chars.forEach((s, i) => { const u = prog(t, .05 + i * .045, .34, E.out); css(s, { opacity: (u * (1 - qOut)).toFixed(3), transform: `translateY(${((1 - u) * 40 - qOut * 30).toFixed(1)}px)` }); });
        // 方块自下而上落下（第 2 小节），第 3 小节重拍拼好
        show(icon.box, t >= T(2) - .01);
        icon.render(t, T(2), .088, .46, 640);
        const pulse = t >= T(3) ? 1 + .055 * Math.sin(clamp((t - T(3)) / .32) * Math.PI) : 1;
        icon.box.style.transform = `scale(${pulse.toFixed(4)})`;
        icon.box.style.transformOrigin = "50% 60%";
        for (const b of burst) {
          const u = clamp((t - T(3)) / .7), on = t >= T(3) && u < 1;
          show(b.e, on);
          if (on) { const r = 120 + b.d * E.out(u); css(b.e, { width: b.s + "px", height: b.s + "px", background: b.c, opacity: (1 - u).toFixed(3), transform: `translate(${(W / 2 + Math.cos(b.a) * r * 1.25).toFixed(1)}px, ${(494 + Math.sin(b.a) * r).toFixed(1)}px)` }); }
        }
        const wu = prog(t, T(3), .45, E.out), tu = prog(t, T(3, 1), .45, E.out);
        css(word, { opacity: wu.toFixed(3), transform: `translateY(${((1 - wu) * 40).toFixed(1)}px)` });
        css(tag, { opacity: tu.toFixed(3), transform: `translateY(${((1 - tu) * 30).toFixed(1)}px)` });
      };
    });

    // ---- B 新建服务器（第 4–10 小节，6–20 秒）----
    { // 选核心
      const paper = R("wiz-core", "paper"), next = R("wiz-core", "next");
      scene(T(4), T(5), root => {
        const v = ShotView(root, {
          shots: [[T(4), "wiz-core"], [T(4, 2), "wiz-core-sel"]],
          cam: [[T(4), { x: 720, y: 430, z: 1.62 }], [T(5), { x: 640, y: 470, z: 1.86 }, E.io]],
          cursor: [[T(4), { x: 1000, y: 690 }], [T(4, 1.75), { x: C(paper).x - 40, y: C(paper).y + 6 }, E.io], [T(4, 2.2), { x: C(paper).x - 40, y: C(paper).y + 6 }], [T(4, 3.2), C(next), E.io], [T(5), C(next)]],
          cursorOn: [T(4), T(5) + 1], clicks: [T(4, 2), T(4, 3.5)],
          marks: [{ t0: T(4, 1), t1: T(4, 3.25), r: paper, spot: true }],
        });
        return t => v.render(t);
      });
    }
    { // 选版本
      const ver = R("wiz-version", "v"), next = R("wiz-version", "next");
      scene(T(5), T(6), root => {
        const v = ShotView(root, {
          shots: [[T(5), "wiz-version"], [T(5, 2), "wiz-version-sel"]],
          cam: [[T(5), { x: 680, y: 430, z: 1.7 }], [T(6), { x: 650, y: 470, z: 1.9 }, E.io]],
          cursor: [[T(5), { x: 1060, y: 720 }], [T(5, 1.75), { x: ver.x + 70, y: C(ver).y + 4 }, E.io], [T(5, 2.2), { x: ver.x + 70, y: C(ver).y + 4 }], [T(5, 3.2), C(next), E.io], [T(6), C(next)]],
          cursorOn: [T(5), T(6) + 1], clicks: [T(5, 2), T(5, 3.5)],
          marks: [{ t0: T(5, 1), t1: T(5, 3.25), r: ver, spot: true }],
        });
        return t => v.render(t);
      });
    }
    { // 基本设置 → 开始部署
      const nm = R("wiz-settings-eula", "name"), eula = R("wiz-settings-eula", "eula"), go = R("wiz-settings-eula", "go");
      scene(T(6), T(7), root => {
        const v = ShotView(root, {
          shots: [[T(6), "wiz-settings-eula"]],
          cam: [[T(6), { x: 660, y: 330, z: 1.78 }], [T(7), { x: 700, y: 560, z: 1.86 }, E.io]],
          cursor: [[T(6), { x: 760, y: 300 }], [T(6, 1), { x: C(nm).x + 60, y: C(nm).y + 4 }, E.io], [T(6, 2.6), { x: C(go).x, y: C(go).y + 2 }, E.io], [T(7), { x: C(go).x, y: C(go).y + 2 }]],
          cursorOn: [T(6), T(7) + 1], clicks: [T(6, 3)],
          marks: [{ t0: T(6, .5), t1: T(6, 1.75), r: nm }, { t0: T(6, 1.75), t1: T(6, 3.9), r: go, spot: true, pad: 10 }],
        });
        return t => v.render(t);
      });
    }
    { // 部署进度：每半拍换一张（最后两张换得快些）
      const prog_ = R("deploy-0", "prog"), done = R("deploy-8", "done");
      const shots = [[T(7), "deploy-0"], [T(7, 1), "deploy-1"], [T(7, 2), "deploy-2"], [T(7, 3), "deploy-3"], [T(8), "deploy-4"], [T(8, 1), "deploy-5"], [T(8, 2), "deploy-6"], [T(8, 2.5), "deploy-7"], [T(8, 3), "deploy-8"]];
      scene(T(7), T(9), root => {
        const v = ShotView(root, {
          shots,
          cam: [[T(7), { x: 690, y: 400, z: 1.62 }], [T(8, 2.8), { x: 640, y: 380, z: 2.0 }, E.io], [T(8, 3.6), { x: 690, y: 470, z: 1.78 }, E.out], [T(9), { x: 690, y: 470, z: 1.78 }]],
          marks: [{ t0: T(7, .5), t1: T(8, 2.9), r: { x: prog_.x, y: prog_.y - 4, w: prog_.w, h: prog_.h + 8 } }, { t0: T(8, 3), t1: T(9), r: done, spot: true, spotA: .6, pad: 10 }],
        });
        return t => v.render(t);
      });
    }
    { // 一个 exe
      scene(T(9), T(11), root => {
        const tile = el("div", "exe-tile", root);
        el("img", "", tile).src = "/out/assets/icon-256.png";
        el("div", "fn", tile).textContent = TEXT.exeFile;
        el("div", "sz", tile).textContent = TEXT.exeSize;
        tile.dataset.text = "exe";
        const title = el("div", "big-title", root); title.textContent = TEXT.exeTitle; css(title, { left: "240px", top: "330px" }); title.dataset.text = "exe";
        const rows = TEXT.exeRows.map((s, i) => { const r = el("div", "row-check", root, `<span class="ck">${CHECK_SVG}</span><span></span>`); r.lastChild.textContent = s; css(r, { left: "250px", top: 620 + i * 150 + "px" }); r.dataset.text = "exe"; return r; });
        return t => {
          const a = prog(t, T(9), .5, E.spring), fade = prog(t, T(9), .2);
          css(tile, { left: "1600px", top: "340px", opacity: fade.toFixed(3), transform: `translateY(${(Math.sin((t - T(9)) * 1.6) * 8).toFixed(1)}px) scale(${(0.86 + .14 * a).toFixed(4)})` });
          const tu = prog(t, T(9, 1), .4, E.out);
          css(title, { opacity: tu.toFixed(3), transform: `translateX(${((1 - tu) * -40).toFixed(1)}px)` });
          rows.forEach((r, i) => {
            const u = prog(t, T(9, 2 + i), .4, E.out), cu = prog(t, T(9, 2 + i) + .12, .3, E.spring);
            css(r, { opacity: u.toFixed(3), transform: `translateX(${((1 - u) * -40).toFixed(1)}px)` });
            r.firstChild.style.transform = `scale(${(.6 + .4 * cu).toFixed(4)})`;
          });
        };
      });
    }

    // ---- C 运行与管理（第 11–16 小节，20–32 秒）----
    { // 点启动 → 运行中 → Done
      const start = R("console-stopped", "start"), pill = R("console-running", "pill"), doneLine = R("console-running", "done");
      const doneText = { x: doneLine.x - 6, y: doneLine.y - 1, w: 412, h: doneLine.h + 2 };
      scene(T(11), T(13), root => {
        const v = ShotView(root, {
          shots: [[T(11), "console-stopped"], [T(11, 2), "console-running"]],
          cam: [[T(11), { x: 760, y: 300, z: 1.8 }], [T(11, 2), { x: 720, y: 270, z: 1.95 }, E.io], [T(12), { x: 720, y: 270, z: 1.95 }], [T(12, 1), { x: 640, y: 400, z: 2.15 }, E.io], [T(13), { x: 620, y: 410, z: 2.25 }]],
          cursor: [[T(11), { x: 1150, y: 420 }], [T(11, 1.75), { x: C(start).x - 8, y: C(start).y + 3 }, E.io], [T(11, 3), { x: C(start).x - 8, y: C(start).y + 3 }], [T(12), { x: 1180, y: 300 }, E.io]],
          cursorOn: [T(11), T(12)], clicks: [T(11, 2)],
          marks: [{ t0: T(11, 2.25), t1: T(11, 3.9), r: pill, pad: 8 }, { t0: T(12, .5), t1: T(13), r: doneText, spot: true, padY: 1, padX: 8, radius: 12 }],
        });
        return t => v.render(t);
      });
    }
    { // 服务器列表
      const card = R("servers", "card");
      scene(T(13), T(14), root => {
        const v = ShotView(root, {
          shots: [[T(13), "servers"]],
          cam: [[T(13), { x: 700, y: 360, z: 1.7 }], [T(14), { x: 620, y: 330, z: 1.9 }, E.io]],
          marks: [{ t0: T(13, 1), t1: T(14), r: card, spot: true, spotA: .6, radius: 26 }],
        });
        return t => v.render(t);
      });
    }
    { // 报错横幅 + 常见原因
      const banner = R("banner", "banner");
      const local = { x: 314, y: 688, w: 266, h: 26 }; // 「以上是 MineDeck 本地规则的分析，没有联网。」
      const fix = { x: 312, y: 441, w: 360, h: 64 };    // 第一个「怎么修」和下面两行
      scene(T(14), T(17), root => {
        const v = ShotView(root, {
          shots: [[T(14), "banner"]],
          cam: [[T(14), { x: 720, y: 430, z: 1.68 }], [T(15), { x: 680, y: 440, z: 1.85 }, E.io], [T(16), { x: 690, y: 450, z: 1.85 }], [T(16, 1), { x: 600, y: 640, z: 2.1 }, E.io], [T(17), { x: 590, y: 650, z: 2.18 }]],
          marks: [{ t0: T(14, 1), t1: T(15), r: banner, radius: 26, pad: 6 }, { t0: T(16, 1), t1: T(16, 2.5), r: fix }, { t0: T(16, 2.5), t1: T(17), r: local, spot: true, spotA: .6 }],
        });
        const card = el("div", "causes", root);
        el("h3", "", card).textContent = TEXT.causesTitle;
        const items = TEXT.causes.map(s => { const it = el("div", "it", card, "<i><b></b></i><span></span>"); it.lastChild.textContent = s; return it; });
        card.dataset.text = "causes";
        const dim = el("div", "spot", root); css(dim, { width: "0px", height: "0px", left: "0px", top: "0px" });
        return t => {
          v.render(t);
          const a = vis(t, T(15), T(16), .35, .25);
          show(card, a > 0.001); show(dim, a > 0.001);
          if (a > 0.001) {
            const u = prog(t, T(15), .45, E.out);
            css(card, { left: "1300px", top: "250px", opacity: a.toFixed(3), transform: `translateX(${((1 - u) * 80).toFixed(1)}px)` });
            dim.style.opacity = (a * .55).toFixed(3);
            items.forEach((it, i) => { const iu = prog(t, T(15, i), .35, E.out); css(it, { opacity: iu.toFixed(3), transform: `translateY(${((1 - iu) * 24).toFixed(1)}px)` }); });
          }
        };
      });
    }

    // ---- D 整合包与联机（第 17–22 小节，32–44 秒）----
    { // 拖入整合包
      const file = R("import-filled", "file"), go = R("import-filled", "go");
      scene(T(17), T(19), root => {
        const v = ShotView(root, {
          shots: [[T(17), "import"], [T(17, 3), "import-filled"]],
          cam: [[T(17), { x: 720, y: 330, z: 1.78 }], [T(18), { x: 700, y: 360, z: 1.86 }, E.io], [T(18, 1), { x: 700, y: 360, z: 1.86 }], [T(18, 3.5), { x: 680, y: 470, z: 1.74 }, E.io], [T(19), { x: 680, y: 470, z: 1.74 }]],
          cursor: [[T(17), { x: 120, y: 690 }], [T(17, 2.6), { x: C(file).x + 40, y: C(file).y + 10 }, E.io], [T(18, 1.5), { x: C(file).x + 40, y: C(file).y + 10 }], [T(18, 3), { x: C(go).x, y: C(go).y + 2 }, E.io], [T(19), { x: C(go).x, y: C(go).y + 2 }]],
          cursorOn: [T(17), T(19) + 1], clicks: [T(17, 3)],
          marks: [{ t0: T(17, 3), t1: T(18, 1.5), r: file, spot: true, spotA: .55 }, { t0: T(18, 3), t1: T(19), r: go, pad: 10 }],
        });
        const chip = el("div", "file-chip", root, `<span class="doc"></span><span></span>`);
        chip.lastChild.textContent = TEXT.mrpack; chip.dataset.text = "chip";
        const cur = track([[T(17), { x: 120, y: 690 }], [T(17, 2.6), { x: C(file).x + 40, y: C(file).y + 10 }, E.io]]);
        return t => {
          v.render(t);
          const on = t < T(17, 3.3);
          show(chip, on);
          if (on) {
            const p = cur(t), [sx, sy] = v.toStage(p.x, p.y);
            const drop = prog(t, T(17, 2.75), .3, E.in);
            css(chip, { opacity: (prog(t, T(17), .2) * (1 - drop)).toFixed(3), transform: `translate(${(sx - 60).toFixed(1)}px, ${(sy - 40).toFixed(1)}px) scale(${(1 - .25 * drop).toFixed(3)}) rotate(${(-3 + 3 * drop).toFixed(2)}deg)`, transformOrigin: "30px 30px" });
          }
        };
      });
    }
    { // 内网穿透：复制公网地址
      const copy = R("tunnels", "copy"), addr = R("tunnels", "addr"), t1row = R("tunnels", "t1"), t2row = R("tunnels", "t2");
      scene(T(19), T(21), root => {
        const v = ShotView(root, {
          shots: [[T(19), "tunnels"], [T(19, 3), "tunnels-copied"]],
          cam: [[T(19), { x: 740, y: 255, z: 2.1 }], [T(19, 3), { x: 760, y: 245, z: 2.3 }, E.io], [T(21), { x: 770, y: 240, z: 2.38 }]],
          cursor: [[T(19), { x: 1200, y: 470 }], [T(19, 2.5), { x: C(copy).x - 6, y: C(copy).y + 3 }, E.io], [T(20, 1), { x: C(copy).x - 6, y: C(copy).y + 3 }], [T(20, 2.5), { x: 1100, y: 420 }, E.io]],
          cursorOn: [T(19), T(20, 2.5)], clicks: [T(19, 3)],
          marks: [{ t0: T(19, 1), t1: T(19, 2.9), r: { x: t1row.x, y: t1row.y, w: t1row.w, h: t2row.y + t2row.h - t1row.y }, radius: 26, pad: 6 },
            { t0: T(20), t1: T(21), r: addr, spot: true, spotA: .55, pad: 10 }],
        });
        return t => v.render(t);
      });
    }
    { // 手机管理
      const qr = R("remote", "card");
      scene(T(21), T(23), root => {
        const v = ShotView(root, {
          vp: { x: 128, y: 150, w: 1300, h: 880, r: 34 },
          shots: [[T(21), "remote"]],
          cam: [[T(21), { x: 876, y: C(qr).y - 40, z: 1.6 }], [T(23), { x: 862, y: C(qr).y - 20, z: 1.66 }, E.io]],
          marks: [{ t0: T(21, 1), t1: T(22, 1), r: R("remote", "qr"), pad: 10 }],
        });
        // 手机外框 + 两张手机截图（服务器列表 → 点「管理」→ 控制台）
        const S = 1.3, sw = 390 * S, sh = 844 * S, bz = 22;
        const phone = el("div", "phone", root);
        css(phone, { width: sw + 2 * bz + "px", height: sh + 2 * bz + "px" });
        const scr = el("div", "scr", phone); css(scr, { left: bz + "px", top: bz + "px", width: sw + "px", height: sh + "px" });
        const a = el("img", "", scr); a.src = useShot("phone-servers"); css(a, { width: sw + "px", height: sh + "px" });
        const b = el("img", "", scr); b.src = useShot("phone-console"); css(b, { width: sw + "px", height: sh + "px" });
        const camDot = el("div", "cam", phone); css(camDot, { left: (sw / 2 + bz - 17) + "px", top: (bz + 16) + "px" });
        const tap = el("div", "tap", scr);
        const tapAt = { x: 128, y: 445 }, tTap = T(22, 1.5);
        return t => {
          v.render(t);
          const u = prog(t, T(21, 1), .55, E.spring), f = prog(t, T(21, 1), .2);
          css(phone, { left: "1680px", top: "130px", opacity: f.toFixed(3), transform: `translateY(${((1 - u) * 160).toFixed(1)}px)` });
          const sl = prog(t, T(22, 2), .32, E.out);
          css(a, { transform: `translateX(${(-sl * sw * .3).toFixed(1)}px)`, opacity: (1 - sl * .6).toFixed(3) });
          css(b, { transform: `translateX(${((1 - sl) * sw).toFixed(1)}px)` });
          show(b, t >= T(22, 2));
          const d = t - tTap, on = d >= 0 && d < .5;
          show(tap, on);
          if (on) { const r = 26 + 34 * E.out(d / .5); css(tap, { width: 2 * r + "px", height: 2 * r + "px", transform: `translate(${(tapAt.x * S - r).toFixed(1)}px, ${(tapAt.y * S - r).toFixed(1)}px)`, opacity: (1 - E.in(d / .5)).toFixed(3) }); }
        };
      });
    }

    // ---- E 可选的 AI（第 23–31 小节，44–62 秒）----
    scene(T(23), T(24, 2), root => {
      const [l1, l2, l3] = TEXT.aiTitle.map((s, i) => { const e = el("div", "ai-l", root); e.textContent = s; e.dataset.text = "ai"; return e; });
      css(l1, { top: "440px", fontSize: "150px", fontWeight: "600", letterSpacing: "-.01em" });
      css(l2, { top: "690px", fontSize: "80px", fontWeight: "500" });
      css(l3, { top: "830px", fontSize: "64px", fontWeight: "400", color: "var(--muted)" });
      return t => {
        [[l1, T(23)], [l2, T(23, 1.5)], [l3, T(24)]].forEach(([e, t0]) => { const u = prog(t, t0, .45, E.out); css(e, { opacity: u.toFixed(3), transform: `translateY(${((1 - u) * 36).toFixed(1)}px)` }); });
      };
    });
    { // AI 开服助手
      const card = R("assistant", "card"), ok = R("assistant", "ok"), user = R("assistant", "user");
      scene(T(24, 2), T(26, 2), root => {
        const v = ShotView(root, {
          shots: [[T(24, 2), "assistant"]],
          cam: [[T(24, 2), { x: 760, y: 360, z: 1.62 }], [T(25), { x: 930, y: 300, z: 1.95 }, E.io], [T(25, 2), { x: 950, y: 470, z: 2.1 }, E.io], [T(26, 2), { x: 960, y: 480, z: 2.2 }]],
          cursor: [[T(25, 2), { x: 1150, y: 700 }], [T(25, 3.5), { x: C(ok).x - 4, y: C(ok).y + 3 }, E.io], [T(26, 2), { x: C(ok).x - 4, y: C(ok).y + 3 }]],
          cursorOn: [T(25, 2), T(26, 2) + 1], clicks: [T(26)],
          marks: [{ t0: T(24, 3), t1: T(25, 1.75), r: user, pad: 8 }, { t0: T(25, 2), t1: T(26, 2), r: card, spot: true, spotA: .55, radius: 24 }],
        });
        return t => v.render(t);
      });
    }
    { // 游戏内 AI：自己画的方块风景 + 聊天栏（没有用任何游戏素材）
      scene(T(26, 2), T(28, 2), root => {
        const g = el("div", "game", root); css(g, { left: "128px", top: "96px", width: "2304px", height: "1248px" });
        const cv = el("canvas", "", g); cv.width = 2304; cv.height = 1248;
        const ctx = cv.getContext("2d");
        const chat = el("div", "chat", g); css(chat, { left: "64px", top: "250px" });
        const lines = TEXT.chatAI.map(m => { const ln = el("div", "ln", chat); ln.innerHTML = m.who ? `<span class="who"></span><span></span>` : `<span class="h"></span><span></span>`; ln.firstChild.textContent = m.who ? `<${m.who}> ` : m.head; ln.lastChild.textContent = m.text; ln.dataset.text = "chat"; return ln; });
        const at = [T(26, 2.5), T(26, 3.5), T(27, 1.5), T(27, 2.5)];
        const tag = el("div", "game-tag", g, "<i></i><span>游戏内 AI 已打开</span>"); css(tag, { right: "48px", top: "44px" }); tag.dataset.text = "chat";
        const land = Landscape(2304, 1248, 5);
        return t => {
          const clearU = prog(t, T(27, 2.5), .6, E.io);
          land.draw(ctx, t, 1 - clearU);
          lines.forEach((ln, i) => { const u = prog(t, at[i], .3, E.out); show(ln, t >= at[i]); css(ln, { opacity: u.toFixed(3), transform: `translateX(${((1 - u) * -30).toFixed(1)}px)` }); });
          const tu = prog(t, T(26, 2), .4, E.out); css(tag, { opacity: tu.toFixed(3) });
        };
      });
    }
    { // AI 伙伴
      const c1 = R("companion", "c1"), list = R("companion", "list");
      scene(T(28, 2), T(29, 3), root => {
        const v = ShotView(root, {
          shots: [[T(28, 2), "companion"]],
          cam: [[T(28, 2), { x: 720, y: 380, z: 1.72 }], [T(29, 3), { x: 690, y: 365, z: 1.95 }, E.io]],
          marks: [{ t0: T(28, 3), t1: T(29, 3), r: c1, spot: true, spotA: .6 }],
        });
        const chat = el("div", "chat", root); css(chat, { right: "128px", bottom: "150px", alignItems: "flex-end" });
        const lines = TEXT.chatCompanion.map(m => { const ln = el("div", "ln", chat); ln.innerHTML = m.who ? `<span class="who"></span><span></span>` : `<span class="h"></span><span></span>`; ln.firstChild.textContent = m.who ? `<${m.who}> ` : m.head; ln.lastChild.textContent = m.text; ln.dataset.text = "chat"; return ln; });
        const at = [T(28, 3), T(29)];
        return t => {
          v.render(t);
          lines.forEach((ln, i) => { const u = prog(t, at[i], .3, E.out); show(ln, t >= at[i]); css(ln, { opacity: u.toFixed(3), transform: `translateY(${((1 - u) * -16).toFixed(1)}px)` }); });
        };
      });
    }
    { // 音乐共享
      const src = R("music", "src");
      scene(T(29, 3), T(31), root => {
        const v = ShotView(root, {
          shots: [[T(29, 3), "music"]],
          cam: [[T(29, 3), { x: 720, y: 400, z: 1.7 }], [T(31), { x: 660, y: 370, z: 1.92 }, E.io]],
          marks: [{ t0: T(30), t1: T(31), r: { x: src.x, y: src.y, w: src.w, h: 70 }, spot: true, spotA: .5 }],
        });
        return t => v.render(t);
      });
    }
    { // Key 与额度
      const key = R("settings-ai", "key"), quota = R("settings-ai", "quota");
      scene(T(31), T(32), root => {
        const v = ShotView(root, {
          shots: [[T(31), "settings-ai"]],
          cam: [[T(31), { x: 900, y: 290, z: 1.8 }], [T(31, 1.5), { x: 910, y: 320, z: 1.85 }, E.io], [T(31, 2.6), { x: 930, y: 430, z: 1.9 }, E.io], [T(32), { x: 935, y: 440, z: 1.92 }]],
          marks: [{ t0: T(31, .5), t1: T(31, 2), r: { x: key.x - 70, y: key.y - 34, w: 760, h: 112 }, radius: 22 },
            { t0: T(31, 2), t1: T(32), r: { x: quota.x - 8, y: quota.y - 42, w: quota.w + 16, h: 68 }, radius: 18, pad: 6 }],
        });
        return t => v.render(t);
      });
    }

    // ---- F 回顾（第 32 小节）与片尾（第 33–38 小节）----
    scene(T(32), T(33), root => {
      const names = ["wiz-core-sel", "deploy-8", "console-running", "banner", "tunnels-copied", "assistant"];
      const tiles = names.map((n, i) => { const e = el("div", "tile", root); el("img", "", e).src = useShot(n); return { e, i, x: 140 + (i % 3) * 780, y: 170 + Math.floor(i / 3) * 520 }; });
      const dim = el("div", "recap-dim", root); dim.style.background = "rgba(240,236,225,.62)";
      const words = el("div", "recap-words", root);
      const ws = [];
      TEXT.recap.forEach((s, i) => { if (i) ws.push(el("i", "", words)); const sp = el("span", "", words); sp.textContent = s; sp.dataset.text = "recap"; ws.push(sp); });
      return t => {
        for (const ti of tiles) {
          const u = prog(t, T(32) + ti.i * .0625, .36, E.out);
          css(ti.e, { left: ti.x + "px", top: ti.y + "px", opacity: u.toFixed(3), transform: `translateY(${((1 - u) * 60).toFixed(1)}px) scale(${(.92 + .08 * u).toFixed(4)}) rotate(${((ti.i % 2 ? 1 : -1) * (1 - u) * 3).toFixed(2)}deg)` });
        }
        dim.style.opacity = (prog(t, T(32, .75), .25, E.out)).toFixed(3);
        // 三个词落在第 2、3、4 拍（分隔方块和后一个词一起出来）
        let k = 0;
        ws.forEach(e => {
          const t0 = T(32, 1 + k);
          if (e.tagName === "SPAN") { const u = prog(t, t0, .3, E.spring); k++; css(e, { opacity: clamp(u * 2).toFixed(3), transform: `scale(${(.7 + .3 * u).toFixed(4)})` }); }
          else { const u = prog(t, t0, .25, E.out); css(e, { opacity: u.toFixed(3) }); }
        });
      };
    });
    scene(T(33), DURATION + 1, root => {
      const icon = IconBlocks(root, 24, W / 2, 200);
      const word = el("div", "wordmark", root); word.textContent = TEXT.word; css(word, { top: "640px", fontSize: "176px" }); word.dataset.text = "end";
      const l1 = el("div", "end-line", root); css(l1, { top: "870px", fontSize: "64px", fontWeight: "500" });
      TEXT.endLine.forEach((s, i) => { if (i) el("i", "sep", l1); const sp = el("span", "", l1); sp.textContent = s; });
      l1.dataset.text = "end";
      const url = el("div", "end-url", root, `${GLOBE_SVG}<span></span>`); url.lastChild.textContent = TEXT.endUrl; url.dataset.text = "end";
      const l3 = el("div", "end-line", root, `<span style="display:flex;color:var(--muted)">${SEARCH_SVG}</span><span></span>`); l3.lastChild.textContent = TEXT.endSearch;
      css(l3, { top: "1160px", fontSize: "60px", fontWeight: "500" }); l3.dataset.text = "end";
      return t => {
        icon.render(t, T(33), .034, .34, 420);
        const pulse = 1 + .04 * Math.sin(clamp((t - T(33, 1.6)) / .3) * Math.PI);
        css(icon.box, { transform: `scale(${pulse.toFixed(4)})`, transformOrigin: "50% 60%" });
        [[word, T(33, 1.5)], [l1, T(33, 2.5)], [url, T(33, 3.5)], [l3, T(34, .5)]].forEach(([e, t0]) => {
          const u = prog(t, t0, .45, E.out);
          const base = e === url ? "translateX(-50%) " : "";
          css(e, { opacity: u.toFixed(3), transform: `${base}translateY(${((1 - u) * 34).toFixed(1)}px)` });
        });
        url.style.top = "1004px";
      };
    });
  }

  // 游戏画面的插画（自己画的：圆角云、平滑远山、台阶状近山、圆树冠、雨），不用任何游戏素材和贴图
  function Landscape(w, h, seed) {
    const r = rng(seed);
    const clouds = []; for (let i = 0; i < 6; i++) clouds.push({ x: r() * (w + 900), y: 70 + r() * 230, s: .7 + r() * .6, sp: 14 + r() * 12 });
    const rain = []; for (let i = 0; i < 240; i++) rain.push({ x: r() * (w + 400), y: r() * h, l: 30 + r() * 40, sp: 900 + r() * 500 });
    const trees = []; for (let x = 120; x < w * 2; x += 260 + r() * 340) trees.push({ x, s: .8 + r() * .5, k: r() });
    const mix = (a, b, u) => { const p = c => [1, 3, 5].map(i => parseInt(c.slice(i, i + 2), 16)); const A = p(a), Bc = p(b); return `rgb(${A.map((v, i) => Math.round(v + (Bc[i] - v) * u)).join(",")})`; };
    const far = x => h * .56 + Math.sin(x * .0021 + .7) * 70 + Math.sin(x * .0053) * 34;
    const STEP = 64;
    const near = x => { const sx = Math.floor(x / STEP) * STEP; return Math.round((h * .72 + Math.sin(sx * .0026 + 2.1) * 90 + Math.sin(sx * .0071) * 30) / 32) * 32; };
    return {
      draw(ctx, t, rainy) {
        const sky = ctx.createLinearGradient(0, 0, 0, h);
        sky.addColorStop(0, mix("#86c3ec", "#6c7f8e", rainy)); sky.addColorStop(1, mix("#e2f1f8", "#a8b3ba", rainy));
        ctx.fillStyle = sky; ctx.fillRect(0, 0, w, h);
        // 太阳（雨停后才露出来）
        ctx.globalAlpha = 1 - rainy; ctx.fillStyle = "#fff4c8";
        ctx.beginPath(); ctx.arc(w * .78, 250, 86, 0, Math.PI * 2); ctx.fill();
        ctx.globalAlpha = (1 - rainy) * .35; ctx.beginPath(); ctx.arc(w * .78, 250, 130, 0, Math.PI * 2); ctx.fill();
        ctx.globalAlpha = 1;
        // 圆角云
        for (const c of clouds) {
          const x = ((c.x - t * c.sp) % (w + 900) + w + 900) % (w + 900) - 450, s = c.s;
          ctx.fillStyle = mix("#ffffff", "#c3ccd2", rainy);
          ctx.beginPath(); ctx.roundRect(x, c.y, 360 * s, 70 * s, 35 * s); ctx.fill();
          ctx.beginPath(); ctx.roundRect(x + 70 * s, c.y - 44 * s, 190 * s, 80 * s, 40 * s); ctx.fill();
        }
        const pan = t * 26;
        // 远山（平滑）
        ctx.fillStyle = mix("#b5dca6", "#9fb3a3", rainy);
        ctx.beginPath(); ctx.moveTo(0, h);
        for (let x = 0; x <= w; x += 16) ctx.lineTo(x, far(x + pan * .35));
        ctx.lineTo(w, h); ctx.closePath(); ctx.fill();
        // 近山（台阶状，像素风的一点点意思）
        ctx.fillStyle = mix("#6dbb4a", "#5f8f55", rainy * .7);
        ctx.beginPath(); ctx.moveTo(0, h);
        const off = pan % STEP;
        for (let x = -off; x <= w + STEP; x += STEP) { const y = near(x + pan); ctx.lineTo(x, y); ctx.lineTo(x + STEP, y); }
        ctx.lineTo(w + STEP, h); ctx.closePath(); ctx.fill();
        const grd = ctx.createLinearGradient(0, h * .6, 0, h);
        grd.addColorStop(0, "rgba(40,80,20,0)"); grd.addColorStop(1, "rgba(40,80,20,.35)");
        ctx.fillStyle = grd; ctx.fillRect(0, h * .6, w, h * .4);
        // 树：细树干 + 圆树冠
        for (const tr of trees) {
          const x = tr.x - pan; if (x < -200 || x > w + 200) continue;
          const gy = near(x + pan), s = tr.s;
          ctx.fillStyle = "#7a5233"; ctx.fillRect(x - 10 * s, gy - 150 * s, 20 * s, 150 * s);
          ctx.fillStyle = tr.k < .5 ? "#3f9a35" : "#4aa83c";
          ctx.beginPath(); ctx.arc(x, gy - 190 * s, 78 * s, 0, Math.PI * 2); ctx.fill();
          ctx.beginPath(); ctx.arc(x - 56 * s, gy - 150 * s, 54 * s, 0, Math.PI * 2); ctx.fill();
          ctx.beginPath(); ctx.arc(x + 58 * s, gy - 150 * s, 56 * s, 0, Math.PI * 2); ctx.fill();
        }
        // 雨
        if (rainy > 0.01) {
          ctx.strokeStyle = `rgba(232,241,248,${(.55 * rainy).toFixed(3)})`; ctx.lineWidth = 4; ctx.lineCap = "round";
          ctx.beginPath();
          for (const d of rain) { const y = (d.y + t * d.sp) % (h + 100) - 50, x = ((d.x - (t * d.sp) * .18) % (w + 400) + w + 400) % (w + 400) - 100; ctx.moveTo(x, y); ctx.lineTo(x - d.l * .18, y + d.l); }
          ctx.stroke();
        }
      },
    };
  }

  // ---------- 主渲染 ----------
  let dbg = null;
  function renderAt(t) {
    // 背景装饰方块只在纯文字的场景出现
    const decoOn = t < T(4) || (t >= T(9) && t < T(11)) || (t >= T(23) && t < T(24, 2)) || t >= T(33) ? 1 : 0;
    renderBg(t, decoOn);
    for (const s of scenes) {
      const on = t >= s.t0 && t < s.t1;
      show(s.root, on);
      if (on) s.render(t);
    }
    renderCaps(t);
    renderWipe(t);
    if (dbg) { const bar = Math.floor(t / 2) + 1, beat = (t % 2) / .5; dbg.textContent = `t=${t.toFixed(3)}s  f=${Math.round(t * 60)}  bar ${bar} beat ${beat.toFixed(2)}`; }
  }

  async function init() {
    [RECTS, ICON] = await Promise.all([fetch("/out/shots/rects.json").then(r => r.json()), fetch("/out/assets/pixel-icon.json").then(r => r.json())]);
    build();
    if (qs.has("debug")) { dbg = el("div", "", stage); dbg.id = "dbg"; }
    // 字体：把所有上屏文字按用到的字重各加载一遍
    const allText = [...document.querySelectorAll("#stage *")].map(e => e.childElementCount ? "" : e.textContent).join("") + CAPTIONS.map(c => [c.eb, c.main, c.sub, ...(c.chips || [])].join("")).join("");
    const loads = [];
    for (const fam of ["MD Geist", "MD Noto Sans SC", "MD Geist Mono"]) for (const wgt of [400, 500, 600]) loads.push(document.fonts.load(`${wgt} 64px "${fam}"`, allText));
    await Promise.all(loads);
    await document.fonts.ready;
    // 图片全部解码
    const all = [...document.images];
    await Promise.all(all.map(i => i.decode().catch(() => { throw new Error("image failed: " + i.src); })));
    renderAt(qs.has("t") ? +qs.get("t") : 0);
    if (qs.has("play")) { const t0 = performance.now() - (+qs.get("t") || 0) * 1000; const loop = () => { renderAt(((performance.now() - t0) / 1000) % DURATION); requestAnimationFrame(loop); }; requestAnimationFrame(loop); }
    return { duration: DURATION, images: all.length };
  }

  window.renderAt = renderAt;
  window.__ready = init();
})();
