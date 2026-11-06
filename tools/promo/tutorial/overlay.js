// 注入页面的东西。这几个函数都在浏览器里跑，不能引用外面的变量。
//   fakeEventSource：控制台日志流换成假流，由录制脚本决定每行什么时候出现（真的日志流按真实时间推，录不出稳定的节奏）
//   instantScroll：平滑滚动改成立刻到位（平滑滚动按真实时间走，要滚动的镜头由脚本逐帧推 #main.scrollTop）
//   installSeek：每帧暂停所有动画和过渡，拨到虚拟时间该在的位置；走到头的调 finish()，animationend 照常触发
//   installOverlay：画面上层的光标、输入光标、字幕卡、高亮框、章节卡。这一层用输出画面的坐标（1280×720），
//                   镜头推近时反向缩放抵消掉，所以字幕和线宽不随镜头变。不用 CSS 动画，每帧由脚本直接设样式。
"use strict";

function fakeEventSource(seed) {
  const Real = window.EventSource;
  const open = new Map();
  window.__log = seed || {};
  window.__emit = (name, line) => {
    (window.__log[name] = window.__log[name] || []).push(line);
    for (const f of open.get(name) || []) if (f.onmessage) f.onmessage({ data: JSON.stringify(line) });
  };
  window.EventSource = function (url, opts) {
    const m = /\/api\/instances\/([^/]+)\/console/.exec(String(url));
    if (!m) return new Real(url, opts);
    const name = decodeURIComponent(m[1]);
    const fake = { readyState: 1, url: String(url), onmessage: null, onerror: null, onopen: null, addEventListener() {}, removeEventListener() {},
      close() { this.readyState = 2; open.get(name).delete(this); } };
    if (!open.has(name)) open.set(name, new Set());
    open.get(name).add(fake);
    setTimeout(() => { for (const l of window.__log[name] || []) if (fake.readyState === 1 && fake.onmessage) fake.onmessage({ data: JSON.stringify(l) }); }, 20);
    return fake;
  };
}

function instantScroll() {
  const orig = Element.prototype.scrollIntoView;
  Element.prototype.scrollIntoView = function (o) { orig.call(this, o && typeof o === "object" ? { ...o, behavior: "auto" } : o); };
}

function installSeek() {
  const born = new WeakMap();
  window.__seek = now => {
    for (const a of document.getAnimations()) {
      if (!born.has(a)) born.set(a, now);
      if (a.playState === "finished") continue;
      const t = now - born.get(a);
      const end = a.effect ? a.effect.getComputedTiming().endTime : 0;
      if (end !== Infinity && t >= end) { try { a.finish(); } catch { /* 无限循环的走不到这里 */ } continue; }
      if (a.playState !== "paused") a.pause();
      a.currentTime = t;
    }
  };
}

function installOverlay({ icon }) {
  const css = `
  #__ov { position: fixed; left: 0; top: 0; width: 1280px; height: 720px; z-index: 2147483647; pointer-events: none; overflow: hidden; transform-origin: 0 0;
    font-family: var(--font); -webkit-font-smoothing: antialiased; }
  #__ov * { box-sizing: border-box; }
  input, textarea { caret-color: transparent !important; }
  #__cur { position: absolute; left: 0; top: 0; width: 28px; height: 28px; filter: drop-shadow(0 2px 3px rgba(0,0,0,.35)); }
  #__ring { position: absolute; left: 0; top: 0; width: 40px; height: 40px; margin: -20px 0 0 -20px; border-radius: 50%; border: 2px solid rgba(255,255,255,.95); box-shadow: 0 0 0 2px rgba(63,125,32,.85); opacity: 0; }
  #__caret { position: absolute; left: 0; top: 0; width: 1.6px; background: #221f18; display: none; }
  .__mark { position: absolute; left: 0; top: 0; border-radius: 10px; border: 2.5px solid #fff; outline: 2.5px solid #4c9a2a; box-shadow: 0 0 16px 2px rgba(92,190,60,.55); opacity: 0; }
  .__mark.dim { box-shadow: 0 0 16px 2px rgba(92,190,60,.55), 0 0 0 4000px rgba(28,24,14,.34); }
  #__cap { position: absolute; bottom: 44px; max-width: 900px; padding: 15px 24px 18px; border-radius: 15px; background: rgba(22,27,24,.95); color: #f7f4ec;
    box-shadow: 0 12px 30px -9px rgba(0,0,0,.45), 0 0 0 1px rgba(255,255,255,.06); opacity: 0; }
  #__cap .eb { display: flex; align-items: center; gap: 8px; font-size: 21px; line-height: 1.15; font-weight: 600; color: #8fe06c; letter-spacing: .02em; margin-bottom: 5px; }
  #__cap .eb i { width: 9px; height: 9px; background: #8fe06c; box-shadow: 3px 3px 0 0 rgba(143,224,108,.45); display: block; }
  #__cap .main { font-size: 35px; line-height: 1.24; font-weight: 600; letter-spacing: .01em; white-space: nowrap; }
  #__cap .sub { font-size: 23px; line-height: 1.3; font-weight: 400; color: #ddd8c9; margin-top: 5px; white-space: nowrap; }
  #__badge { position: absolute; right: 28px; top: 44px; padding: 7px 14px 8px; border-radius: 999px; background: rgba(22,27,24,.92); color: #f7f4ec; font-size: 18px; font-weight: 600; opacity: 0; }
  #__sand { position: absolute; inset: 0; background: #efece2; opacity: 0; }
  #__chap { position: absolute; inset: 0; display: flex; flex-direction: column; justify-content: center; padding-left: 116px; color: #221f18; opacity: 0; }
  #__chap .no { display: flex; align-items: center; gap: 12px; font-size: 26px; font-weight: 600; color: #3f7d20; letter-spacing: .04em; margin-bottom: 12px; }
  #__chap .no i { width: 12px; height: 12px; background: #4c9a2a; box-shadow: 4px 4px 0 0 rgba(76,154,42,.35); display: block; }
  #__chap .ti { font-size: 66px; line-height: 1.2; font-weight: 600; letter-spacing: .01em; }
  #__chap .su { font-size: 28px; line-height: 1.4; color: #625d4e; margin-top: 12px; }
  #__chap img { position: absolute; right: 116px; top: 50%; width: 170px; height: 170px; margin-top: -85px; image-rendering: pixelated; }
  #__card { position: absolute; inset: 0; background: #efece2; color: #221f18; opacity: 0; }
  #__card .wrap { position: absolute; left: 96px; right: 96px; top: 0; bottom: 0; display: flex; flex-direction: column; justify-content: center; }
  #__card .eb { display: flex; align-items: center; gap: 10px; font-size: 24px; font-weight: 600; color: #3f7d20; letter-spacing: .03em; margin-bottom: 10px; }
  #__card .eb i { width: 11px; height: 11px; background: #4c9a2a; box-shadow: 4px 4px 0 0 rgba(76,154,42,.35); display: block; }
  #__card .h { font-size: 50px; line-height: 1.22; font-weight: 600; letter-spacing: .01em; }
  #__card .s { font-size: 27px; line-height: 1.45; color: #625d4e; margin-top: 10px; }
  #__card .row { display: flex; align-items: stretch; gap: 22px; margin-top: 42px; }
  #__card .tile { flex: 1; min-width: 0; padding: 24px 24px 22px; border-radius: 16px; background: #fbfaf5; box-shadow: 0 2px 0 rgba(60,50,30,.06), 0 10px 24px -14px rgba(60,50,30,.35); }
  #__card .tile img, #__card .tile svg { width: 60px; height: 60px; display: block; margin-bottom: 14px; image-rendering: pixelated; }
  #__card .tile b { display: block; font-size: 27px; font-weight: 600; line-height: 1.3; }
  #__card .tile span { display: block; font-size: 22px; line-height: 1.4; color: #625d4e; margin-top: 5px; }
  #__card .arrow { align-self: center; font-size: 34px; color: #4c9a2a; font-weight: 600; }
  #__card .pill { padding: 16px 30px 18px; border-radius: 12px; background: #fbfaf5; font-size: 32px; font-weight: 600; box-shadow: 0 0 0 2px rgba(76,154,42,.55), 0 10px 24px -14px rgba(60,50,30,.35); }
  #__card .addr { font-family: var(--mono); font-size: 40px; font-weight: 600; color: #2f6a14; margin-top: 34px; }
  #__card ol { list-style: none; margin: 34px 0 0; padding: 0; }
  #__card li { display: flex; gap: 18px; align-items: baseline; font-size: 31px; line-height: 1.3; margin-bottom: 22px; font-weight: 500; }
  #__card li i { font-style: normal; flex: none; width: 42px; height: 42px; border-radius: 10px; background: #3f7d20; color: #fff; font-size: 24px; font-weight: 600; display: grid; place-items: center; }
  #__card .phone { position: absolute; right: 120px; top: 34px; width: 300px; height: 652px; padding: 10px; border-radius: 40px; background: #1d211e; box-shadow: 0 24px 50px -22px rgba(0,0,0,.6); }
  #__card .phone img { width: 100%; height: 100%; display: block; border-radius: 31px; object-fit: cover; object-position: top; }
  #__card .bar { position: absolute; left: 0; right: 0; bottom: 0; height: 58px; background: #e3dfd2; display: flex; align-items: center; justify-content: flex-end; gap: 16px; padding-right: 150px; font-size: 19px; color: #625d4e; }
  #__card .bar img { width: 30px; height: 30px; image-rendering: pixelated; }
  #__card .menu { position: absolute; right: 120px; bottom: 76px; width: 330px; padding: 8px; border-radius: 12px; background: #fbfaf5; box-shadow: 0 16px 40px -14px rgba(60,50,30,.5), 0 0 0 1px rgba(60,50,30,.1); }
  #__card .menu div { padding: 12px 16px; font-size: 25px; border-radius: 8px; }
  #__card .menu div.on { background: #e7efe0; color: #2f6a14; font-weight: 600; }
  #__card .end { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; }
  #__card .end img { width: 190px; height: 190px; image-rendering: pixelated; margin-bottom: 22px; }
  #__card .end .w { font-size: 74px; font-weight: 600; letter-spacing: .01em; }
  #__card .end .l { display: flex; align-items: center; gap: 14px; font-size: 27px; color: #625d4e; margin-top: 14px; }
  #__card .end .l i { width: 8px; height: 8px; background: #4c9a2a; display: block; }
  #__card .end .u { font-family: var(--mono); font-size: 29px; color: #2f6a14; margin-top: 26px; }
  #__card .end .g { font-size: 26px; color: #625d4e; margin-top: 8px; }
  #__chip { position: absolute; left: 0; top: 0; padding: 10px 16px 11px; border-radius: 10px; background: #fbfaf5; color: #221f18; font-size: 19px; font-weight: 600; white-space: nowrap; opacity: 0;
    box-shadow: 0 12px 26px -10px rgba(40,30,10,.55), 0 0 0 1.5px rgba(76,154,42,.7); }
  `;
  const st = document.createElement("style");
  st.textContent = css;
  document.documentElement.appendChild(st);
  const ov = document.createElement("div");
  ov.id = "__ov";
  ov.innerHTML = `<div id="__card"></div>
    <div id="__marks"></div><div id="__caret"></div>
    <div id="__cap"><div class="eb"><i></i><span></span></div><div class="main"></div><div class="sub"></div></div>
    <div id="__badge"></div>
    <div id="__ring"></div>
    <svg id="__cur" viewBox="0 0 24 24"><path d="M4 2.5v17.2l4.4-4.1 3.1 6.8 3.2-1.4-3.1-6.7h6.1z" fill="#fff" stroke="#151515" stroke-width="1.5" stroke-linejoin="round"/></svg>
    <div id="__chip"></div>
    <div id="__sand"></div>
    <div id="__chap"><div class="no"><i></i><span></span></div><div class="ti"></div><div class="su"></div><img src="${icon}" alt=""></div>`;
  document.documentElement.appendChild(ov);
  const $ = id => document.getElementById(id);
  const cap = $("__cap"), cur = $("__cur"), ring = $("__ring"), caret = $("__caret"), marksBox = $("__marks"), sand = $("__sand"), chap = $("__chap"), badge = $("__badge"), card = $("__card"), chip = $("__chip");
  const cv = document.createElement("canvas").getContext("2d");
  const marks = new Map();

  // 「选择器」或「选择器|包含的文字」，后一种取符合条件、看得见的元素里面积最小的
  const pick = spec => {
    const i = spec.indexOf("|"), sel = i < 0 ? spec : spec.slice(0, i), txt = i < 0 ? "" : spec.slice(i + 1);
    const els = [...document.querySelectorAll(sel)].filter(e => e.getClientRects().length && (!txt || e.textContent.includes(txt)));
    if (!els.length) return null;
    if (!txt) return els[0];
    return els.map(e => ({ e, b: e.getBoundingClientRect() })).sort((a, b) => a.b.width * a.b.height - b.b.width * b.b.height)[0].e;
  };
  window.__rect = spec => {
    const e = pick(spec);
    if (!e) return null;
    const b = e.getBoundingClientRect();
    return { x: b.left, y: b.top, w: b.width, h: b.height };
  };
  window.__exists = spec => !!pick(spec);
  // 目标在 #main 里、却不在能点到的范围（顶上是标题栏，底下留出吸底保存条）：要先滚过去
  window.__needScroll = spec => {
    const e = pick(spec), m = document.querySelector("#main");
    if (!e || !m.contains(e) || e.closest(".save-bar, .wizard-foot")) return false;
    const b = e.getBoundingClientRect();
    return b.top < 48 || b.bottom > 640;
  };
  window.__scrollMain = (y, sel) => { const m = sel ? document.querySelector(sel) : document.querySelector("#main"); m.scrollTop = y; };
  window.__mainTop = (spec, pad, sel) => {
    const m = sel ? document.querySelector(sel) : document.querySelector("#main"), e = pick(spec);
    if (!e) return null;
    const y = e.getBoundingClientRect().top - m.getBoundingClientRect().top + m.scrollTop - pad;
    return { from: m.scrollTop, to: Math.max(0, Math.min(y, m.scrollHeight - m.clientHeight)) };
  };
  window.__capText = ({ eb, main, sub, pos }) => {
    cap.querySelector(".eb").style.display = eb ? "" : "none";
    cap.querySelector(".eb span").textContent = eb || "";
    cap.querySelector(".main").textContent = main || "";
    const s = cap.querySelector(".sub");
    s.style.display = sub ? "" : "none";
    s.textContent = sub || "";
    const right = pos === "right" || pos === "top"; // top：右上角，下半屏有要看的东西时用
    cap.style.left = right ? "auto" : "64px";
    cap.style.right = right ? "64px" : "auto";
    cap.style.top = pos === "top" ? "60px" : "auto";
    cap.style.bottom = pos === "top" ? "auto" : "44px";
  };
  window.__chapText = ({ no, title, sub }) => {
    chap.querySelector(".no span").textContent = no || "";
    chap.querySelector(".no").style.display = no ? "" : "none";
    chap.querySelector(".ti").textContent = title || "";
    chap.querySelector(".su").textContent = sub || "";
  };
  window.__card = html => { card.innerHTML = html || ""; };
  window.__badgeText = t => { badge.textContent = t; };
  window.__chipText = t => { chip.textContent = t; };

  function drawCaret(s, k, cam) {
    const a = document.activeElement;
    const ok = a && (a.tagName === "TEXTAREA" || (a.tagName === "INPUT" && /^(text|search|password|number|)$/.test(a.type)));
    const on = ok && !a.readOnly && !a.disabled && s.caret && ((s.t - s.caretT) % 1.06) < 0.53;
    if (!on) { caret.style.display = "none"; return; }
    const cs = getComputedStyle(a), b = a.getBoundingClientRect();
    cv.font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
    let pos = a.value.length;
    try { if (a.selectionStart != null) pos = a.selectionStart; } catch { /* number 输入框没有 selectionStart */ }
    const txt = a.type === "password" ? "•".repeat(pos) : a.value.slice(0, pos);
    const x = b.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) + cv.measureText(txt).width - a.scrollLeft;
    const fs = parseFloat(cs.fontSize), h = fs * 1.3;
    const y = a.tagName === "TEXTAREA" ? b.top + parseFloat(cs.paddingTop) + 1 : b.top + (b.height - h) / 2;
    caret.style.display = "block";
    caret.style.height = h * k + "px";
    caret.style.background = cs.color;
    caret.style.transform = `translate(${(x - cam.x) * k}px,${(y - cam.y) * k}px)`;
  }

  window.__ov = s => {
    const cam = s.cam, k = 1280 / cam.w;
    ov.style.transform = `translate(${cam.x}px,${cam.y}px) scale(${1 / k})`;
    cur.style.display = s.cur.show ? "" : "none";
    const cx = (s.cur.x - cam.x) * k, cy = (s.cur.y - cam.y) * k;
    cur.style.transform = `translate(${cx - 3.4}px,${cy - 1.8}px)`;
    ring.style.opacity = s.ring > 0 ? String(0.9 * (1 - s.ring)) : "0";
    ring.style.transform = `translate(${cx}px,${cy}px) scale(${0.45 + s.ring * 0.9})`;
    drawCaret(s, k, cam);
    cap.style.opacity = String(s.cap.op);
    cap.style.transform = `translateY(${s.cap.dy}px)`;
    badge.style.opacity = String(s.badge);
    chip.style.opacity = String(s.chip.op);
    chip.style.transform = `translate(${(s.chip.x - cam.x) * k}px,${(s.chip.y - cam.y) * k}px)`;
    sand.style.opacity = String(s.sand);
    chap.style.opacity = String(s.chap);
    card.style.opacity = String(s.card.op);
    if (s.card.op > 0) card.querySelectorAll("[data-in]").forEach(e => {
      const a = Math.max(0, Math.min(1, (s.t - s.card.t0 - +e.dataset.in) / 0.32)), u = 1 - (1 - a) * (1 - a);
      e.style.opacity = String(u);
      e.style.transform = (e.dataset.tf || "") + ` translateY(${(1 - u) * 10}px)`;
    });
    const seen = new Set();
    for (const m of s.marks) {
      seen.add(m.id);
      let el = marks.get(m.id);
      if (!el) { el = document.createElement("div"); el.className = "__mark" + (m.dim ? " dim" : ""); marksBox.appendChild(el); marks.set(m.id, el); }
      const t = pick(m.spec);
      if (t) m.last = t.getBoundingClientRect();
      const r = t ? t.getBoundingClientRect() : null;
      if (!r) { el.style.opacity = "0"; continue; }
      const p = m.pad;
      el.style.opacity = String(m.op);
      el.style.width = (r.width + p * 2 / k) * k + "px";
      el.style.height = (r.height + p * 2 / k) * k + "px";
      el.style.borderRadius = (m.radius ?? 10) + "px";
      el.style.transform = `translate(${(r.left - cam.x) * k - p}px,${(r.top - cam.y) * k - p}px)`;
    }
    for (const [id, el] of marks) if (!seen.has(id)) { el.remove(); marks.delete(id); }
  };
}

module.exports = { fakeEventSource, instantScroll, installSeek, installOverlay };
