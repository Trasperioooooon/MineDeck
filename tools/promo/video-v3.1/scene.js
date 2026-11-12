/* MineDeck v3.1.0 宣传片的全部画面。window.renderAt(t) 只依赖 t（秒），同一个 t 永远画出同一帧。
   时间轴写在 captions.js 的节拍网格上：T(小节, 拍)，小节从 1 数、拍从 0 数（123.99 BPM，跟着配乐）。
   截图视图（ShotView）用截图本身的 CSS 像素（1280×800）当坐标：镜头 = 把截图上的哪一点放到画面中心、放大多少倍；
   光标、高亮框写在截图坐标里，由镜头换算到画面上，所以线宽不随镜头缩放；叠在截图上的小部件（下拉列表、飞进清单的小标签、
   打字的遮罩）放在截图那一层里，跟着截图一起缩放。
   画面左下角是字幕，镜头都把要看的东西放在字幕上面（画面 y < 1050）或者右边。 */
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
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
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
  const GREENS = ["#6cbf3a", "#5aaa33", "#4c9a2c", "#3e8f2a", "#367d24", "#5b7b31"];

  // ---------- 素材 ----------
  let RECTS = {}, ICON = null;
  const shotURL = n => `/out/shots/${n}.png`;

  // ---------- 场景 ----------
  const scenes = [];
  function scene(t0, t1, build, opts = {}) {
    const root = el("div", "scene", sceneLayer);
    const s = { t0, t1, root, opts, render: () => {} };
    s.render = build(root, s) || s.render;
    scenes.push(s);
    return s;
  }

  // 截图视图
  function ShotView(parent, o) {
    const size = o.size || [1280, 800];
    const vp = { x: 0, y: 0, w: W, h: H };
    const box = el("div", "sv", parent);
    css(box, { left: "0px", top: "0px", width: vp.w + "px", height: vp.h + "px" });
    const win = el("div", "win", box);
    css(win, { width: size[0] + "px", height: size[1] + "px" });
    const names = [...new Set(o.shots.map(s => s[1]))];
    const im = {};
    for (const n of names) { const i = el("img", "", win); i.decoding = "sync"; i.src = shotURL(n); i.alt = ""; im[n] = i; }
    // 叠在截图上的小部件（截图坐标，跟着镜头缩放）
    const ovs = (o.overlays || []).map(ov => { const e = el("div", "ov " + (ov.cls || ""), win, ov.html || ""); css(e, { left: ov.x + "px", top: ov.y + "px" }); if (ov.w) e.style.width = ov.w + "px"; if (ov.h) e.style.height = ov.h + "px"; return { ...ov, e }; });
    const cam = track(o.cam);
    const marks = (o.marks || []).map(m => ({ ...m, spotEl: m.spot ? el("div", "spot", box) : null, el: el("div", "mark", box) }));
    const cur = o.cursor ? track(o.cursor) : null;
    const cursorEl = cur ? el("div", "cursor", box, CURSOR_SVG) : null;
    const ripples = (o.clicks || []).map(tc => ({ tc, el: el("div", "ripple", box) }));
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
        for (const ov of ovs) {
          const a = vis(t, ov.t0, ov.t1, ov.din ?? .16, ov.dout ?? .12);
          show(ov.e, a > 0.001);
          if (a > 0.001) { ov.e.style.opacity = (ov.solid ? 1 : a).toFixed(3); if (ov.render) ov.render(t, ov.e, a); }
        }
        for (const m of marks) {
          const a = vis(t, m.t0, m.t1, .26, .2);
          const on = a > 0.001;
          show(m.el, on);
          if (m.spotEl) show(m.spotEl, on);
          if (!on) continue;
          const padX = (m.padX ?? m.pad ?? 8) * view.z / 1.8, padY = (m.padY ?? m.pad ?? 8) * view.z / 1.8;
          const [x0, y0] = view.toStage(m.r.x, m.r.y);
          const w = m.r.w * view.z + 2 * padX, h = m.r.h * view.z + 2 * padY;
          const sc = 1 + (1 - prog(t, m.t0, .3, E.out)) * .04;
          const tr = `translate(${(x0 - padX).toFixed(1)}px, ${(y0 - padY).toFixed(1)}px)`;
          const geo = { width: w.toFixed(1) + "px", height: h.toFixed(1) + "px", borderRadius: (m.radius ?? 18) + "px" };
          css(m.el, { ...geo, transform: tr + ` scale(${sc.toFixed(4)})`, transformOrigin: "50% 50%", opacity: a.toFixed(3) });
          if (m.spotEl) css(m.spotEl, { ...geo, transform: tr, opacity: (a * (m.spotA ?? .5)).toFixed(3) });
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
          const d = t - r.tc, on = d >= 0 && d < .5;
          show(r.el, on);
          if (!on) continue;
          const p = cur ? cur(r.tc) : { x: 0, y: 0 };
          const [sx, sy] = view.toStage(p.x, p.y);
          const u = E.out(d / .5), rad = 16 + 64 * u;
          css(r.el, { width: rad * 2 + "px", height: rad * 2 + "px", transform: `translate(${(sx - rad).toFixed(1)}px, ${(sy - rad).toFixed(1)}px)`, opacity: (1 - E.in(d / .5)).toFixed(3) });
        }
      },
    };
    return view;
  }
  const R = (shot, key) => { const r = RECTS[shot] && RECTS[shot][key]; if (!r) throw new Error(`rect ${shot}.${key} missing`); return r; };
  const C = r => ({ x: r.x + r.w / 2, y: r.y + r.h / 2 });
  // 一个截图镜头的简写：shots / cam / marks / cursor / clicks / overlays 原样交给 ShotView
  const shotScene = (t0, t1, o, opts) => scene(t0, t1, root => { const v = ShotView(root, o); return t => v.render(t); }, opts);

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
  // 落定时向外弹出的小方块
  function Burst(parent, seed, n, cx, cy, d0, d1) {
    const r = rng(seed), bs = [];
    for (let i = 0; i < n; i++) { const a = (i / n) * Math.PI * 2 + r() * .3; bs.push({ a, d: d0 + r() * d1, s: 14 + Math.floor(r() * 2) * 8, c: GREENS[i % GREENS.length], e: el("i", "px-deco", parent) }); }
    return (t, t0, dur = .7) => {
      for (const b of bs) {
        const u = clamp((t - t0) / dur), on = t >= t0 && u < 1;
        show(b.e, on);
        if (on) { const rr = 100 + b.d * E.out(u); css(b.e, { width: b.s + "px", height: b.s + "px", background: b.c, opacity: (1 - u).toFixed(3), transform: `translate(${(cx + Math.cos(b.a) * rr * 1.25).toFixed(1)}px, ${(cy + Math.sin(b.a) * rr).toFixed(1)}px)` }); }
      }
    };
  }
  // 逐字进场的一行字
  function Chars(parent, cls, text) {
    const e = el("div", cls, parent);
    const cs = [...text].map(ch => { const s = el("span", "", e); s.textContent = ch; return s; });
    return { e, render(t, t0, step, tOut) {
      const out = tOut != null ? prog(t, tOut, .22, E.in) : 0;
      cs.forEach((s, i) => { const u = prog(t, t0 + i * step, .32, E.out); css(s, { opacity: (u * (1 - out)).toFixed(3), transform: `translateY(${((1 - u) * 40 - out * 24).toFixed(1)}px)` }); });
    } };
  }

  // ---------- 显示器（交接那几段用，自己画的扁平图形） ----------
  function Monitor(parent, cx, top, label) {
    const m = el("div", "mon", parent);
    css(m, { left: (cx - 350) + "px", top: top + "px" });
    const scr = el("div", "scr", m); const inner = el("div", "in", scr);
    el("div", "neck", m); el("div", "base", m);
    const lb = el("div", "lbl", m); lb.textContent = label; lb.dataset.text = "pc";
    return { m, inner, cx, cy: top + 22 + 198 }; // 屏幕中心（画面坐标）
  }
  function MiniCard(parent, on) {
    const c = el("div", "mini-card", parent);
    const b = el("b", "", c); b.textContent = TEXT.srvName;
    const m = el("div", "m", c); m.textContent = TEXT.srvMeta;
    const st = el("div", "st " + (on ? "on" : "off"), c, "<i></i><span></span>"); st.lastChild.textContent = TEXT.srvState[on ? 1 : 0];
    c.dataset.text = "card";
    return c;
  }
  // 交接包文件：图标 + 文件名 + 大小
  function FileTile(parent) {
    const f = el("div", "file-tile", parent);
    el("img", "", f).src = "/out/assets/icon-256.png";
    const n = el("div", "fn", f); n.textContent = TEXT.packFile;
    const s = el("div", "sz", f); s.textContent = TEXT.packSize;
    f.dataset.text = "file";
    return f;
  }

  // ---------- 字幕 ----------
  const capLayer = el("div", "layer", stage);
  const caps = CAPTIONS.map(c => {
    const e = el("div", "cap", capLayer);
    if (c.eb) el("div", "eb", e, `<i></i><span>${c.eb}</span>`);
    el("div", "main", e).textContent = c.main;
    if (c.sub) el("div", "sub", e).textContent = c.sub;
    e.dataset.text = "cap";
    return { ...c, e };
  });
  function renderCaps(t) {
    for (const c of caps) {
      const a = vis(t, c.t0, c.t1, .3, .18);
      show(c.e, a > 0.001);
      if (a <= 0.001) continue;
      const inU = prog(t, c.t0, .34, E.out), outU = prog(t, c.t1 - .18, .18, E.in);
      css(c.e, { opacity: a.toFixed(3), transform: `translateY(${((1 - inU) * 28 - outU * 10).toFixed(1)}px)` });
    }
  }

  // ---------- 像素擦除（分段之间，落在小节线上） ----------
  const wipeLayer = el("div", "layer", stage);
  const COLS = 16, ROWS = 9, BS = 160;
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
  // 时间轴（画面第 b 小节 = 曲子第 b + 2 小节）
  // =====================================================================
  function build() {
    // ---- 开场（第 1–3 小节）：一句话 → 方块拼出图标，第 3 小节低音进来时落定 ----
    scene(0, T(4), root => {
      const q = Chars(root, "hook-q", TEXT.hookQ); q.e.dataset.text = "hook";
      const icon = IconBlocks(root, 26, W / 2, 236);
      const word = el("div", "wordmark", root); word.textContent = TEXT.word; css(word, { top: "690px", fontSize: "200px" }); word.dataset.text = "hook";
      const badge = el("div", "badge", root, "<i></i><span></span>"); badge.lastChild.textContent = TEXT.badge; css(badge, { top: "930px" }); badge.dataset.text = "hook";
      const tag = el("div", "tagline", root); css(tag, { top: "1086px", fontSize: "70px" });
      tag.innerHTML = TEXT.hookTag.replace("Windows 一键开服工具", "Windows <b>一键开服</b>工具"); tag.dataset.text = "hook";
      const burst = Burst(root, 21, 18, W / 2, 236 + 208, 260, 140);
      return t => {
        show(q.e, t < T(2, 1));
        q.render(t, .05, .05, T(1, 3.4));
        show(icon.box, t >= T(2) - .01);
        icon.render(t, T(2), .085, .44, 640);
        const pulse = t >= T(3) ? 1 + .055 * Math.sin(clamp((t - T(3)) / .3) * Math.PI) : 1;
        css(icon.box, { transform: `scale(${pulse.toFixed(4)})`, transformOrigin: "50% 60%" });
        burst(t, T(3));
        const wu = prog(t, T(3), .42, E.out), bu = prog(t, T(3, .5), .45, E.spring), bf = prog(t, T(3, .5), .15), tu = prog(t, T(3, 1.5), .42, E.out);
        css(word, { opacity: wu.toFixed(3), transform: `translateY(${((1 - wu) * 40).toFixed(1)}px)` });
        css(badge, { opacity: bf.toFixed(3), transform: `translateX(-50%) scale(${(.7 + .3 * bu).toFixed(4)})` });
        css(tag, { opacity: tu.toFixed(3), transform: `translateY(${((1 - tu) * 30).toFixed(1)}px)` });
      };
    });

    // ---- 四种方式（第 4–5 小节）----
    { const rs = ["new", "import", "transfer", "adopt"].map(k => R("create-home", k));
      shotScene(T(4), T(6), {
        shots: [[T(4), "create-home"]],
        cam: [[T(4), { x: 690, y: 360, z: 1.78 }], [T(5, 2.5), { x: 650, y: 380, z: 2.0 }, E.io], [T(6), { x: C(rs[3]).x - 30, y: C(rs[3]).y + 60, z: 2.7 }, E.in]],
        marks: rs.map((r, i) => ({ t0: T(5, i), t1: i < 3 ? T(5, i + 1) + .06 : T(6), r, spot: true, spotA: .4, radius: 22 })),
      });
    }

    // ---- 第一章：接管已有服务器（第 6–11 小节）----
    { // 选中以前的服务器文件夹（路径一个字一个字打出来），点「识别」
      const dir = R("adopt-filled", "dir"), scan = R("adopt-filled", "scan");
      const tx0 = 278, tx1 = 450; // 输入框里文字的起止（截图坐标；从 adopt-filled.png 量的，文字的深色像素在 278.5–448）
      const typed = u => tx0 + (tx1 - tx0) * u;
      const tu = t => prog(t, T(6, .25), T(6, 1.75) - T(6, .25), E.lin);
      shotScene(T(6), T(7), {
        shots: [[T(6), "adopt-filled"]],
        cam: [[T(6), { x: 640, y: 300, z: 2.0 }], [T(7), { x: 700, y: 280, z: 2.25 }, E.io]],
        overlays: [{ t0: T(6) - 1, t1: T(6, 2), solid: true, din: 0, dout: 0, cls: "type-mask", x: tx0, y: dir.y + 5, h: dir.h - 10,
          render: (t, e) => { const x = typed(tu(t)); css(e, { left: x.toFixed(1) + "px", width: (dir.x + dir.w - 8 - x).toFixed(1) + "px" }); e.classList.toggle("caret-on", t < T(6, 1.9) && (Math.floor(t / .26) % 2 === 0 || tu(t) < 1)); } }],
        cursor: [[T(6), { x: 920, y: 420 }], [T(6, 1.75), { x: 920, y: 420 }], [T(6, 2.4), { x: C(scan).x - 6, y: C(scan).y + 3 }, E.io], [T(7), { x: C(scan).x - 6, y: C(scan).y + 3 }]],
        cursorOn: [T(6), T(7) + 1], clicks: [T(6, 2.6)],
        marks: [{ t0: T(6, .25), t1: T(6, 2.2), r: dir, pad: 8 }, { t0: T(6, 2.2), t1: T(7), r: scan, spot: true, spotA: .4, pad: 8 }],
      });
    }
    { // 识别出来了
      const core = R("adopt-scan", "core");
      const box = { x: 265, y: core.y - 4, w: 760, h: 210 }; // 名称、类型、版本、启动文件、内存
      shotScene(T(7), T(8), {
        shots: [[T(7), "adopt-scan"]],
        cam: [[T(7), { x: 660, y: 260, z: 1.9 }], [T(8), { x: 650, y: 270, z: 2.1 }, E.io]],
        marks: [{ t0: T(7, .5), t1: T(8), r: box, spot: true, spotA: .45, radius: 20 }],
      });
    }
    { // 点「接管这台服务器」→ 接管完成
      const go = R("adopt-scan", "go"), log = R("adopt-task", "log");
      shotScene(T(8), T(8, 1.25), {
        shots: [[T(8), "adopt-scan"]],
        cam: [[T(8), { x: 760, y: 640, z: 2.2 }], [T(8, 1.25), { x: 780, y: 650, z: 2.3 }, E.io]],
        cursor: [[T(8), { x: 640, y: 700 }], [T(8, .6), { x: C(go).x - 10, y: C(go).y + 3 }, E.io], [T(8, 1.25), { x: C(go).x - 10, y: C(go).y + 3 }]],
        cursorOn: [T(8), T(8, 1.25) + 1], clicks: [T(8, .75)],
        marks: [{ t0: T(8) - .01, t1: T(8, 1.25) + .3, r: go, pad: 8 }],
      });
      shotScene(T(8, 1.25), T(9), {
        shots: [[T(8, 1.25), "adopt-task"]],
        cam: [[T(8, 1.25), { x: 660, y: 400, z: 1.8 }], [T(9), { x: 660, y: 420, z: 1.95 }, E.io]],
        marks: [{ t0: T(8, 1.75), t1: T(9), r: { x: log.x, y: log.y, w: log.w, h: 100 }, spot: true, spotA: .4, radius: 16 }],
      });
    }
    { // 列表里多了这一台
      const card = R("servers-adopted", "card");
      shotScene(T(9), T(10), {
        shots: [[T(9), "servers-adopted"]],
        cam: [[T(9), { x: 640, y: 600, z: 1.75 }], [T(10), { x: 600, y: 640, z: 1.95 }, E.io]],
        marks: [{ t0: T(9, .5), t1: T(10), r: card, spot: true, spotA: .5, radius: 26 }],
      });
    }
    { // 换 Java：打开下拉列表，选本机装的 Java 21
      const sel = R("jvm-java-before", "sel"), set = R("jvm-java", "set");
      const ROW = 24, PAD = 4, pick = 2;
      const dd = `<div class="dd">${TEXT.javaOpts.map((s, i) => `<div class="dd-i${i === 0 ? " cur" : ""}">${esc(s)}</div>`).join("")}</div>`;
      const ddY = sel.y + sel.h + 3, rowY = i => ddY + PAD + i * ROW + ROW / 2;
      shotScene(T(10), T(11), {
        shots: [[T(10), "jvm-java-before"], [T(10, 2.6), "jvm-java"]],
        cam: [[T(10), { x: 600, y: 640, z: 2.15 }], [T(11), { x: 620, y: 655, z: 2.3 }, E.io]],
        overlays: [{ t0: T(10, .75), t1: T(10, 2.6), x: sel.x, y: ddY, w: 640, html: dd,
          render: (t, e, a) => {
            const u = prog(t, T(10, .75), .2, E.out);
            e.firstChild.style.transform = `translateY(${((1 - u) * -8).toFixed(1)}px) scaleY(${(.94 + .06 * u).toFixed(3)})`;
            const hov = t < T(10, 1.4) ? 0 : pick;
            [...e.firstChild.children].forEach((r, i) => r.classList.toggle("hov", i === hov));
          } }],
        cursor: [[T(10), { x: 720, y: 720 }], [T(10, .45), { x: sel.x + 300, y: C(sel).y + 2 }, E.io], [T(10, 1), { x: sel.x + 300, y: C(sel).y + 2 }],
          [T(10, 1.6), { x: sel.x + 260, y: rowY(pick) + 2 }, E.io], [T(10, 2.5), { x: sel.x + 260, y: rowY(pick) + 2 }], [T(10, 3.4), { x: C(set).x - 4, y: C(set).y + 3 }, E.io], [T(11), { x: C(set).x - 4, y: C(set).y + 3 }]],
        cursorOn: [T(10), T(11) + 1], clicks: [T(10, .6), T(10, 2.45)],
        marks: [{ t0: T(10, .1), t1: T(10, .75), r: sel, pad: 6 }, { t0: T(10, 2.75), t1: T(11), r: set, spot: true, spotA: .35, pad: 8 }],
      });
    }
    { // 删除时只移出列表
      const sw = R("delete-adopted", "sw"), ok = R("delete-adopted", "ok");
      shotScene(T(11), T(12), {
        shots: [[T(11), "delete-adopted"]],
        cam: [[T(11), { x: 640, y: 420, z: 2.1 }], [T(12), { x: 645, y: 430, z: 2.3 }, E.io]],
        marks: [{ t0: T(11, .5), t1: T(11, 2.3), r: { x: sw.x, y: sw.y - 6, w: sw.w, h: 112 }, spot: true, spotA: .4, radius: 16 },
          { t0: T(11, 2.3), t1: T(12), r: ok, pad: 8 }],
      });
    }

    // ---- 第二章：自己挑模组（第 12–17 小节）----
    { const chips = R("wiz-mods", "chips");
      shotScene(T(12), T(13), {
        shots: [[T(12), "wiz-mods"]],
        cam: [[T(12), { x: 700, y: 420, z: 1.8 }], [T(13), { x: 640, y: 470, z: 2.0 }, E.io]],
        marks: [{ t0: T(12, 1), t1: T(13), r: { x: chips.x, y: 396, w: chips.w, h: 200 }, spot: true, spotA: .4, radius: 18 }],
      });
    }
    { const chips = R("wiz-mods-search", "q"), cat = R("wiz-mods", "chips");
      shotScene(T(13), T(14), {
        shots: [[T(13), "wiz-mods-search"]],
        cam: [[T(13), { x: 560, y: 400, z: 2.2 }], [T(14), { x: 580, y: 410, z: 2.35 }, E.io]],
        marks: [{ t0: T(13, .25), t1: T(13, 1.8), r: cat, pad: 6 }, { t0: T(13, 1.8), t1: T(14), r: chips, pad: 6 }],
      });
    }
    { // 加入清单：一个小标签从按钮飞进右边的待装清单
      const jei = R("wiz-mods-search", "jei"), list = R("wiz-mods-added", "list");
      const a = { x: jei.x + 8, y: jei.y }, b = { x: list.x + 40, y: list.y + 18 };
      shotScene(T(14), T(15), {
        shots: [[T(14), "wiz-mods-search"], [T(14, 1.15), "wiz-mods-added"]],
        cam: [[T(14), { x: 760, y: 400, z: 2.0 }], [T(15), { x: 800, y: 380, z: 2.15 }, E.io]],
        overlays: [{ t0: T(14, .9), t1: T(14, 1.3), x: 0, y: 0, cls: "fly-chip", html: "JEI", din: .05, dout: .08,
          render: (t, e) => { const u = prog(t, T(14, .9), T(14, 1.3) - T(14, .9), E.io); const x = lerp(a.x, b.x, u), y = lerp(a.y, b.y, u) - Math.sin(u * Math.PI) * 60; css(e, { transform: `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px) scale(${(1 + .25 * Math.sin(u * Math.PI)).toFixed(3)})` }); } }],
        cursor: [[T(14), { x: 600, y: 560 }], [T(14, .7), { x: C(jei).x, y: C(jei).y + 2 }, E.io], [T(15), { x: C(jei).x + 10, y: C(jei).y + 30 }, E.io]],
        cursorOn: [T(14), T(15) + 1], clicks: [T(14, .85)],
        marks: [{ t0: T(14, 1.4), t1: T(15), r: list, spot: true, spotA: .4, pad: 8 }],
      });
    }
    { // 建好的服务器的待装清单：清单里是 REI，检查前置列出它真实的两个必装前置；框住「还缺的前置」整段（标题、说明和两行）
      const sec = R("cart-deps", "depsec");
      shotScene(T(15), T(16), {
        shots: [[T(15), "cart-deps"]],
        cam: [[T(15), { x: 920, y: 420, z: 2.0 }], [T(16), { x: 1000, y: 500, z: 2.3 }, E.io]],
        marks: [{ t0: T(15, .5), t1: T(16), r: { x: sec.x, y: sec.y - 4, w: sec.w, h: sec.h + 8 }, spot: true, spotA: .45, radius: 18 }],
      });
    }
    { const list = R("wiz-mods-local", "list"), wr = R("wiz-mods-local", "warnrow");
      shotScene(T(16), T(17), {
        shots: [[T(16), "wiz-mods-local"]],
        cam: [[T(16), { x: 900, y: 420, z: 2.1 }], [T(17), { x: 960, y: 450, z: 2.35 }, E.io]],
        marks: [{ t0: T(16, .5), t1: T(16, 2), r: { x: list.x, y: list.y + 90, w: list.w, h: list.h - 90 }, pad: 6 },
          { t0: T(16, 2), t1: T(17), r: wr, spot: true, spotA: .45, pad: 8 }],
      });
    }
    { const pg = R("cart-task-0", "prog"), done = R("cart-task-2", "done");
      shotScene(T(17), T(18), {
        shots: [[T(17), "cart-task-0"], [T(17, 1), "cart-task-1"], [T(17, 2.25), "cart-task-2"]],
        cam: [[T(17), { x: 660, y: 380, z: 1.85 }], [T(17, 2), { x: 620, y: 360, z: 2.1 }, E.io], [T(18), { x: 640, y: 520, z: 2.0 }, E.out]],
        marks: [{ t0: T(17, .25), t1: T(17, 2.3), r: { x: pg.x, y: pg.y - 4, w: pg.w, h: pg.h + 8 } }, { t0: T(17, 2.5), t1: T(18), r: done, spot: true, spotA: .4, pad: 10 }],
      });
    }

    // ---- 第三章：导入整合包（第 18–21 小节）----
    { const file = R("import-cf", "file"), src = R("import-cf", "src");
      shotScene(T(18), T(19), {
        shots: [[T(18), "import-cf"]],
        cam: [[T(18), { x: 650, y: 300, z: 2.0 }], [T(18, 1.25), { x: 650, y: 320, z: 2.0 }], [T(18, 2), { x: 660, y: 560, z: 2.1 }, E.io], [T(19), { x: 670, y: 570, z: 2.2 }]],
        marks: [{ t0: T(18, .25), t1: T(18, 1.4), r: file, pad: 6 }, { t0: T(18, 2), t1: T(19), r: src, spot: true, spotA: .45, pad: 8 }],
      });
    }
    { const box = R("pending", "box");
      shotScene(T(19), T(20), {
        shots: [[T(19), "pending"]],
        cam: [[T(19), { x: 760, y: 330, z: 1.9 }], [T(20), { x: 740, y: 300, z: 2.15 }, E.io]],
        marks: [{ t0: T(19, .25), t1: T(19, 1.5), r: { x: box.x, y: box.y, w: box.w, h: 70 }, pad: 4 },
          { t0: T(19, 1.5), t1: T(20), r: { x: 282, y: 150, w: 939, h: 290 }, spot: true, spotA: .4, pad: 6 }],
      });
    }
    { const rl = R("pending", "rLocal"), p1 = R("pending-local", "p1");
      shotScene(T(20), T(21), {
        shots: [[T(20), "pending"], [T(20, 1.1), "pending-local"]],
        cam: [[T(20), { x: 700, y: 260, z: 2.3 }], [T(21), { x: 720, y: 280, z: 2.45 }, E.io]],
        cursor: [[T(20), { x: 900, y: 400 }], [T(20, .75), { x: rl.x + 8, y: C(rl).y + 2 }, E.io], [T(20, 1.4), { x: rl.x + 8, y: C(rl).y + 2 }], [T(20, 2.4), { x: 300, y: 330 }, E.io], [T(21), { x: 300, y: 330 }]],
        cursorOn: [T(20), T(21) + 1], clicks: [T(20, 1)],
        marks: [{ t0: T(20, 1.2), t1: T(20, 2.4), r: p1, spot: true, spotA: .4, pad: 6 }, { t0: T(20, 2.4), t1: T(21), r: { x: 282, y: 288, w: 939, h: 80 }, pad: 6 }],
      });
    }
    { const log = R("server-in", "log");
      shotScene(T(21), T(22), {
        shots: [[T(21), "server-in"]],
        cam: [[T(21), { x: 660, y: 380, z: 1.85 }], [T(22), { x: 640, y: 400, z: 2.0 }, E.io]],
        marks: [{ t0: T(21, .5), t1: T(22), r: { x: log.x, y: log.y, w: log.w, h: 100 }, spot: true, spotA: .4, radius: 16 }],
      });
    }

    // ---- 转折（第 22–23 小节，曲子安静下来）----
    scene(T(22), T(24), root => {
      const l1 = Chars(root, "turn-l big", TEXT.turn[0]); css(l1.e, { top: "500px" }); l1.e.dataset.text = "turn";
      const l2 = el("div", "turn-l small", root); l2.textContent = TEXT.turn[1]; css(l2, { top: "770px" }); l2.dataset.text = "turn";
      return t => {
        l1.render(t, T(22, .25), .06, T(24) - .3);
        const u = prog(t, T(23), .5, E.out), out = prog(t, T(24) - .3, .25, E.in);
        css(l2, { opacity: (u * (1 - out)).toFixed(3), transform: `translateY(${((1 - u) * 30).toFixed(1)}px)` });
      };
    });

    // ---- 第四章：交接包（第 24–31 小节；第 26 小节主段落落下）----
    { const item = R("card-menu", "item");
      shotScene(T(24) - .25, T(25), {
        shots: [[T(24), "card-menu"]],
        cam: [[T(24), { x: 980, y: 430, z: 2.0 }], [T(25), { x: 1000, y: 480, z: 2.25 }, E.io]],
        cursor: [[T(24), { x: 880, y: 640 }], [T(24, 2), { x: C(item).x - 20, y: C(item).y + 3 }, E.io], [T(25), { x: C(item).x - 20, y: C(item).y + 3 }]],
        cursorOn: [T(24, .2), T(25) + 1], clicks: [T(24, 3)],
        marks: [{ t0: T(24, 1), t1: T(25), r: item, spot: true, spotA: .4, pad: 6 }],
      }, { fadeIn: .3 }); // 提前 0.25 秒开始淡入，和「转折」那两行字的淡出叠在一起，中间不留空的沙色底
    }
    { const who = R("transfer-dialog", "who"), go = R("transfer-dialog", "go");
      shotScene(T(25), T(26), {
        shots: [[T(25), "transfer-dialog"]],
        cam: [[T(25), { x: 640, y: 400, z: 1.85 }], [T(25, 2.5), { x: 660, y: 470, z: 2.1 }, E.io], [T(26), { x: 700, y: 560, z: 2.3 }, E.in]],
        cursor: [[T(25), { x: 900, y: 520 }], [T(25, 2.5), { x: C(go).x - 8, y: C(go).y + 3 }, E.io], [T(26), { x: C(go).x - 8, y: C(go).y + 3 }]],
        cursorOn: [T(25), T(26) + 1], clicks: [T(25, 3)],
        marks: [{ t0: T(25, .25), t1: T(25, 2.3), r: who, spot: true, spotA: .4, pad: 6 }, { t0: T(25, 2.3), t1: T(26), r: go, pad: 8 }],
      });
    }
    // 打包：几样东西飞进箱子，箱子变成一个 .mdpack 文件（第 26 小节，主段落落下）
    scene(T(26), T(27), root => {
      const cx = 1560, cy = 600;
      const crate = el("div", "crate", root, '<i class="rv"></i><i class="rh"></i><i class="bow l"></i><i class="bow r"></i>');
      css(crate, { left: (cx - 220) + "px", top: (cy - 220) + "px" });
      const file = FileTile(root); css(file, { left: (cx - 330) + "px", top: (cy - 330) + "px" });
      const COLORS = ["#6cbf3a", "#93613a", "#4c9a2c", "#c9a227", "#3e8f2a", "#5d584a"];
      const START = [[520, 250], [430, 560], [620, 860], [1560, 140], [2330, 300], [2360, 880]];
      const chips = TEXT.packItems.map((s, i) => { const c = el("div", "pk-chip", root, `<i style="background:${COLORS[i]}"></i><span></span>`); c.lastChild.textContent = s; c.dataset.text = "pack"; return { e: c, sx: START[i][0], sy: START[i][1], i }; });
      const burst = Burst(root, 33, 16, cx, cy, 200, 120);
      return t => {
        const cu = prog(t, T(26), .45, E.spring), cf = prog(t, T(26), .12);
        const morph = prog(t, T(26, 2.5), .18, E.in);
        const squish = t >= T(26, 2.1) && t < T(26, 2.5) ? 1 - .08 * Math.sin(clamp((t - T(26, 2.1)) / (T(26, 2.5) - T(26, 2.1))) * Math.PI) : 1;
        show(crate, morph < 1);
        css(crate, { opacity: (cf * (1 - morph)).toFixed(3), transform: `scale(${((.6 + .4 * cu) * (1 + .1 * morph)).toFixed(4)}, ${((.6 + .4 * cu) * squish * (1 + .1 * morph)).toFixed(4)})` });
        for (const c of chips) {
          const tIn = T(26) + c.i * .05, tFly = T(26, .6) + c.i * .1, dur = .42;
          const a = prog(t, tIn, .2, E.out), f = prog(t, tFly, dur, E.in);
          const on = t >= tIn && f < 1;
          show(c.e, on);
          if (!on) continue;
          const x = lerp(c.sx, cx, f), y = lerp(c.sy, cy, f) - Math.sin(f * Math.PI) * 40;
          css(c.e, { opacity: (a * (1 - prog(t, tFly + dur * .75, dur * .25))).toFixed(3), transform: `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px) translate(-50%, -50%) scale(${(1 - .7 * f).toFixed(4)})` });
        }
        const fu = prog(t, T(26, 2.6), .5, E.spring), ff = prog(t, T(26, 2.55), .15);
        show(file, t >= T(26, 2.55));
        css(file, { opacity: ff.toFixed(3), transform: `scale(${(.7 + .3 * fu).toFixed(4)})` });
        burst(t, T(26, 2.6), .6);
      };
    });
    // 发给朋友：文件从左边的电脑飞到右边的电脑（第 27 小节）
    scene(T(27), T(28), root => {
      const A = Monitor(root, 760, 300, TEXT.pcs[0]), B = Monitor(root, 1840, 300, TEXT.pcs[1]);
      const trail = []; for (let i = 0; i < 22; i++) trail.push(el("i", "trail-dot", root));
      const file = FileTile(root); file.classList.add("mini");
      const badge = el("div", "check-badge", root, CHECK_SVG); css(badge, { left: (B.cx + 150) + "px", top: (B.cy - 190) + "px" });
      const path = u => ({ x: lerp(A.cx, B.cx, u), y: lerp(A.cy, B.cy, u) - Math.sin(u * Math.PI) * 330 });
      return t => {
        const mi = prog(t, T(27), .3, E.out);
        for (const M of [A, B]) css(M.m, { opacity: mi.toFixed(3), transform: `translateY(${((1 - mi) * 30).toFixed(1)}px)` });
        const u = prog(t, T(27, .5), T(27, 2.25) - T(27, .5), E.io);
        const p = path(u), land = t >= T(27, 2.25) ? 1 - .1 * Math.sin(clamp((t - T(27, 2.25)) / .25) * Math.PI) : 1;
        css(file, { opacity: prog(t, T(27), .2).toFixed(3), transform: `translate(${p.x.toFixed(1)}px, ${p.y.toFixed(1)}px) translate(-50%, -50%) scale(${((.42 + .14 * Math.sin(u * Math.PI)) * land).toFixed(4)})` });
        trail.forEach((d, i) => {
          const k = (i + 1) / (trail.length + 1), on = u > k + .03;
          show(d, on);
          if (on) { const q = path(k); css(d, { opacity: (.85 - .5 * prog(t, T(27, 2.6), .4)).toFixed(3), transform: `translate(${(q.x - 11).toFixed(1)}px, ${(q.y - 11).toFixed(1)}px)` }); }
        });
        const bu = prog(t, T(27, 2.75), .4, E.spring);
        show(badge, t >= T(27, 2.75));
        css(badge, { opacity: clamp(bu * 3).toFixed(3), transform: `scale(${(.5 + .5 * bu).toFixed(4)})` });
      };
    });
    { const sum = R("transfer-import", "summary"), warn = R("transfer-import", "warn");
      shotScene(T(28), T(29), {
        shots: [[T(28), "transfer-import"]],
        cam: [[T(28), { x: 650, y: 360, z: 2.0 }], [T(29), { x: 660, y: 420, z: 2.2 }, E.io]],
        marks: [{ t0: T(28, .4), t1: T(28, 2.1), r: sum, spot: true, spotA: .4, pad: 6 }, { t0: T(28, 2.1), t1: T(29), r: warn, spot: true, spotA: .4, pad: 6 }],
      });
    }
    { const steps = R("transfer-task", "steps");
      shotScene(T(29), T(30), {
        shots: [[T(29), "transfer-task"]],
        cam: [[T(29), { x: 660, y: 380, z: 1.8 }], [T(30), { x: 640, y: 360, z: 2.0 }, E.io]],
        marks: [{ t0: T(29, .4), t1: T(30), r: steps, spot: true, spotA: .4, pad: 6 }],
      });
    }
    { const pill = R("friend-console", "pill"), done = R("friend-console", "done");
      shotScene(T(30), T(31), {
        shots: [[T(30), "friend-console"]],
        cam: [[T(30), { x: 700, y: 360, z: 2.2 }], [T(31), { x: 720, y: 380, z: 2.4 }, E.io]],
        marks: [{ t0: T(30, .25), t1: T(30, 1.25), r: pill, pad: 6 },
          { t0: T(30, 1.25), t1: T(30, 2.5), r: { x: done.x - 6, y: done.y - 1, w: 520, h: done.h + 2 }, spot: true, spotA: .45, padY: 2, padX: 8, radius: 12 },
          { t0: T(30, 2.5), t1: T(31), r: { x: done.x - 6, y: done.y + 19, w: 520, h: 64 }, spot: true, spotA: .45, padY: 2, padX: 8, radius: 12 }],
      });
    }
    // 收束：两台电脑上是同一台服务器，换了电脑接着开（第 31 小节）
    scene(T(31), T(32), root => {
      const A = Monitor(root, 760, 300, TEXT.pcs[0]), B = Monitor(root, 1840, 300, TEXT.pcs[1]);
      const ca = MiniCard(A.inner, false), cb = MiniCard(B.inner, true);
      const trail = []; for (let i = 0; i < 22; i++) trail.push(el("i", "trail-dot", root));
      const badge = el("div", "check-badge", root, CHECK_SVG); css(badge, { left: (B.cx + 150) + "px", top: (B.cy - 190) + "px" });
      const path = u => ({ x: lerp(A.cx, B.cx, u), y: lerp(A.cy, B.cy, u) - Math.sin(u * Math.PI) * 330 });
      return t => {
        const au = prog(t, T(31), .35, E.out), bu = prog(t, T(31, .5), .5, E.spring), ku = prog(t, T(31, 1.25), .4, E.spring);
        css(ca, { opacity: (.55 * au).toFixed(3) });
        css(cb, { opacity: prog(t, T(31, .5), .2).toFixed(3), transform: `translate(-50%, -50%) scale(${(.8 + .2 * bu).toFixed(4)})` });
        trail.forEach((d, i) => { const q = path((i + 1) / (trail.length + 1)); css(d, { opacity: (.35 * au).toFixed(3), transform: `translate(${(q.x - 11).toFixed(1)}px, ${(q.y - 11).toFixed(1)}px)` }); });
        show(badge, t >= T(31, 1.25));
        css(badge, { opacity: clamp(ku * 3).toFixed(3), transform: `scale(${(.5 + .5 * ku).toFixed(4)})` });
      };
    });

    // ---- 还有这些（第 32–35 小节）----
    { const toast = R("toast-err", "toast"), det = R("toast-err", "det"), pre = R("err-detail", "pre");
      shotScene(T(32), T(32, 1.75), {
        shots: [[T(32), "toast-err"]],
        cam: [[T(32), { x: 1000, y: 260, z: 2.1 }], [T(32, 1.75), { x: 1060, y: 230, z: 2.3 }, E.io]],
        cursor: [[T(32), { x: 1000, y: 360 }], [T(32, 1.2), { x: C(det).x - 4, y: C(det).y + 3 }, E.io], [T(32, 1.75), { x: C(det).x - 4, y: C(det).y + 3 }]],
        cursorOn: [T(32), T(32, 1.75) + 1], clicks: [T(32, 1.45)],
        marks: [{ t0: T(32, .25), t1: T(32, 1.75) + .3, r: toast, spot: true, spotA: .4, pad: 6 }],
      });
      shotScene(T(32, 1.75), T(33), {
        shots: [[T(32, 1.75), "err-detail"]],
        cam: [[T(32, 1.75), { x: 640, y: 400, z: 2.2 }], [T(33), { x: 640, y: 410, z: 2.35 }, E.io]],
        marks: [{ t0: T(32, 2.1), t1: T(33), r: pre, spot: true, spotA: .35, pad: 6 }],
      });
    }
    { const st = R("tunnel-blocked", "state"), err = R("tunnel-blocked", "err");
      shotScene(T(33), T(34), {
        shots: [[T(33), "tunnel-blocked"]],
        cam: [[T(33), { x: 752, y: 284, z: 2.3 }], [T(34), { x: 752, y: 290, z: 2.6 }, E.io]],
        marks: [{ t0: T(33, .25), t1: T(33, 1.25), r: st, pad: 8 }, { t0: T(33, 1.25), t1: T(34), r: err, spot: true, spotA: .4, pad: 6 }],
      });
    }
    { const hint = R("java-hint", "hint");
      shotScene(T(34), T(35), {
        shots: [[T(34), "java-hint"]],
        cam: [[T(34), { x: 620, y: 230, z: 2.4 }], [T(35), { x: 600, y: 214, z: 2.85 }, E.io]],
        marks: [{ t0: T(34, .5), t1: T(35), r: hint, spot: true, spotA: .4, pad: 8 }],
      });
    }
    { const list = R("create-tasks", "list");
      shotScene(T(35), T(36), {
        shots: [[T(35), "create-tasks"]],
        cam: [[T(35), { x: 680, y: 330, z: 1.85 }], [T(36), { x: 660, y: 320, z: 2.0 }, E.io]],
        marks: [{ t0: T(35, .4), t1: T(36), r: list, spot: true, spotA: .4, pad: 6 }],
      });
    }

    // ---- 正式版、升级（第 36–37 小节）----
    scene(T(36), T(37), root => {
      const title = Chars(root, "rel-title", TEXT.relTitle); css(title.e, { top: "430px" }); title.e.dataset.text = "rel";
      const row = el("div", "rel-chips", root); css(row, { top: "720px" });
      const chips = TEXT.relChips.map(s => { const c = el("div", "rel-chip", row, `<span class="ck">${CHECK_SVG}</span><span></span>`); c.lastChild.textContent = s; c.dataset.text = "rel"; return c; });
      return t => {
        title.render(t, T(36), .04, null);
        chips.forEach((c, i) => { const u = prog(t, T(36, 1 + i * .8), .4, E.spring), f = prog(t, T(36, 1 + i * .8), .15); css(c, { opacity: f.toFixed(3), transform: `translateY(${((1 - u) * 40).toFixed(1)}px) scale(${(.85 + .15 * u).toFixed(4)})` }); });
      };
    });
    { const chk = R("update", "check"), res = R("update", "result");
      shotScene(T(37), T(38), {
        shots: [[T(37), "update-before"], [T(37, 1), "update"]],
        cam: [[T(37), { x: 900, y: 440, z: 2.1 }], [T(38), { x: 880, y: 470, z: 2.3 }, E.io]],
        cursor: [[T(37), { x: 900, y: 560 }], [T(37, .6), { x: C(chk).x - 6, y: C(chk).y + 3 }, E.io], [T(38), { x: C(chk).x - 6, y: C(chk).y + 3 }]],
        cursorOn: [T(37), T(38) + 1], clicks: [T(37, .85)],
        marks: [{ t0: T(37, .2), t1: T(37, 1), r: chk, pad: 8 }, { t0: T(37, 1.2), t1: T(38), r: res, spot: true, spotA: .4, pad: 8 }],
      });
    }

    // ---- 第一次用（第 38–39 小节）：一个文件就够了 ----
    scene(T(38), T(40), root => {
      const tile = el("div", "exe-tile", root);
      el("img", "", tile).src = "/out/assets/icon-256.png";
      el("div", "fn", tile).textContent = TEXT.exeFile;
      el("div", "sz", tile).textContent = TEXT.exeSize;
      tile.dataset.text = "exe";
      const title = el("div", "big-title", root); title.textContent = TEXT.exeTitle; css(title, { left: "240px", top: "300px" }); title.dataset.text = "exe";
      const rows = TEXT.exeRows.map((s, i) => { const r = el("div", "row-check", root, `<span class="ck">${CHECK_SVG}</span><span></span>`); r.lastChild.textContent = s; css(r, { left: "250px", top: 580 + i * 140 + "px" }); r.dataset.text = "exe"; return r; });
      return t => {
        const a = prog(t, T(38), .5, E.spring), fade = prog(t, T(38), .2);
        css(tile, { left: "1600px", top: "300px", opacity: fade.toFixed(3), transform: `translateY(${(Math.sin((t - T(38)) * 1.6) * 8).toFixed(1)}px) scale(${(0.86 + .14 * a).toFixed(4)})` });
        const tu = prog(t, T(38, .5), .4, E.out);
        css(title, { opacity: tu.toFixed(3), transform: `translateX(${((1 - tu) * -40).toFixed(1)}px)` });
        rows.forEach((r, i) => {
          const u = prog(t, T(38, 1.5 + i), .4, E.out), cu = prog(t, T(38, 1.5 + i) + .12, .3, E.spring);
          css(r, { opacity: u.toFixed(3), transform: `translateX(${((1 - u) * -40).toFixed(1)}px)` });
          r.firstChild.style.transform = `scale(${(.6 + .4 * cu).toFixed(4)})`;
        });
      };
    }, { fadeIn: .25 });

    // ---- 回顾（第 40 小节）----
    scene(T(40), T(41), root => {
      const names = ["create-home", "adopt-scan", "wiz-mods-added", "pending-local", "transfer-dialog", "friend-console"];
      const tiles = names.map((n, i) => { const e = el("div", "tile", root); el("img", "", e).src = shotURL(n); return { e, i, x: 140 + (i % 3) * 780, y: 170 + Math.floor(i / 3) * 520 }; });
      const dim = el("div", "recap-dim", root);
      const words = el("div", "recap-words", root);
      const ws = [];
      TEXT.recap.forEach((s, i) => { if (i) ws.push(el("i", "", words)); const sp = el("span", "", words); sp.textContent = s; sp.dataset.text = "recap"; ws.push(sp); });
      return t => {
        const out = prog(t, T(41) - .22, .2, E.in);
        for (const ti of tiles) {
          const u = prog(t, T(40) + ti.i * .06, .34, E.out);
          css(ti.e, { left: ti.x + "px", top: ti.y + "px", opacity: (u * (1 - out)).toFixed(3), transform: `translateY(${((1 - u) * 60).toFixed(1)}px) scale(${(.92 + .08 * u).toFixed(4)}) rotate(${((ti.i % 2 ? 1 : -1) * (1 - u) * 3).toFixed(2)}deg)` });
        }
        dim.style.opacity = (prog(t, T(40, .6), .25, E.out) * (1 - out)).toFixed(3);
        let k = 0;
        ws.forEach(e => {
          const t0 = T(40, 1 + .5 * k);
          if (e.tagName === "SPAN") { const u = prog(t, t0, .3, E.spring); k++; css(e, { opacity: (clamp(u * 2) * (1 - out)).toFixed(3), transform: `scale(${(.7 + .3 * u).toFixed(4)})` }); }
          else { const u = prog(t, t0, .25, E.out); css(e, { opacity: (u * (1 - out)).toFixed(3) }); }
        });
      };
    });

    // ---- 片尾（第 41 小节起；第 42 小节曲子收尾时文字已到位）----
    scene(T(41), DURATION + 1, root => {
      const icon = IconBlocks(root, 24, W / 2, 150);
      const word = el("div", "wordmark", root); word.textContent = TEXT.word; css(word, { top: "580px", fontSize: "170px" }); word.dataset.text = "end";
      const l1 = el("div", "end-line", root); css(l1, { top: "800px", fontSize: "60px", fontWeight: "500" });
      TEXT.endLine.forEach((s, i) => { if (i) el("i", "sep", l1); const sp = el("span", "", l1); sp.textContent = s; });
      l1.dataset.text = "end";
      const url = el("div", "end-url", root, `${GLOBE_SVG}<span></span>`); url.lastChild.textContent = TEXT.endUrl; url.dataset.text = "end"; url.style.top = "920px";
      const l3 = el("div", "end-line", root, `<span style="display:flex;color:var(--muted)">${SEARCH_SVG}</span><span></span>`); l3.lastChild.textContent = TEXT.endSearch;
      css(l3, { top: "1080px", fontSize: "56px", fontWeight: "500" }); l3.dataset.text = "end";
      const note = el("div", "end-note", root); note.textContent = TEXT.endNote; css(note, { top: "1250px" }); note.dataset.text = "end";
      return t => {
        icon.render(t, T(41), .032, .34, 420);
        const pulse = 1 + .04 * Math.sin(clamp((t - T(41, 1.7)) / .3) * Math.PI);
        css(icon.box, { transform: `scale(${pulse.toFixed(4)})`, transformOrigin: "50% 60%" });
        [[word, T(41, 1.5)], [l1, T(41, 2.5)], [url, T(41, 3.25)], [l3, T(42)], [note, T(42, .5)]].forEach(([e, t0]) => {
          const u = prog(t, t0, .45, E.out);
          const base = e === url ? "translateX(-50%) " : "";
          css(e, { opacity: u.toFixed(3), transform: `${base}translateY(${((1 - u) * 34).toFixed(1)}px)` });
        });
      };
    });
  }

  // ---------- 主渲染 ----------
  let dbg = null;
  function renderAt(t) {
    // 背景装饰方块只在纯文字和自己画的场景出现
    // 第 24 小节没有方块擦除转场，装饰方块跟着卡片菜单那一镜的淡入一起淡出，不突然消失
    const decoOn = t < T(4) || (t >= T(26) && t < T(28)) || (t >= T(31) && t < T(32)) || (t >= T(36) && t < T(37)) || (t >= T(38) && t < T(40)) || t >= T(41) ? 1
      : t >= T(22) && t < T(24) + .05 ? 1 - prog(t, T(24) - .25, .3, E.out) : 0;
    renderBg(t, decoOn);
    for (const s of scenes) {
      const on = t >= s.t0 && t < s.t1;
      show(s.root, on);
      if (!on) continue;
      const fi = s.opts.fadeIn ? prog(t, s.t0, s.opts.fadeIn, E.out) : 1;
      s.root.style.opacity = fi < 1 ? fi.toFixed(3) : "";
      s.render(t);
    }
    renderCaps(t);
    renderWipe(t);
    if (dbg) { const bar = Math.floor(t / P.BAR) + 1, beat = (t - (bar - 1) * P.BAR) / P.BEAT; dbg.textContent = `t=${t.toFixed(3)}s  f=${Math.round(t * 60)}  bar ${bar} beat ${beat.toFixed(2)}`; }
  }

  async function init() {
    [RECTS, ICON] = await Promise.all([fetch("/out/shots/rects.json").then(r => r.json()), fetch("/out/assets/pixel-icon.json").then(r => r.json())]);
    build();
    if (qs.has("debug")) { dbg = el("div", "", stage); dbg.id = "dbg"; }
    // 字体：把所有上屏文字按用到的字重各加载一遍
    const allText = [...document.querySelectorAll("#stage *")].map(e => e.childElementCount ? "" : e.textContent).join("") + CAPTIONS.map(c => [c.eb, c.main, c.sub].join("")).join("");
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
