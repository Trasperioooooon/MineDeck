/* 核心头像第二版（界面重构草图用，定稿后替换 internal/web/ui/pixel.js 的 MOTIF）：16×16 网格，按同一套规矩画——
   · 每个物体最外一圈是描边（物体自己颜色里最深的那一档），描边画在形状里面，所以形状的外框就是图标的外框；
   · 光从左上来：描边里面一圈，左上亮、右下暗，中间本色（obj() 自动算，不手点）；
   · 方块类（原版的草方块、Purpur 的紫珀块、没认出来的核心用圆石）一律画成背包里那种斜看的立方体；
   · 图案尽量占满 16 格，左右上下最多留一格空，十二个核心看上去一样大。
   用法：MDPixel16.svg(核心, 像素尺寸) → SVG 字符串；MDPixel16.avatar(核心, 类名, 像素尺寸) → 带核心色底的瓦片。
   像素尺寸取 16 的整数倍（16 / 32 / 48）才锐利。 */
(function (root) {
  "use strict";
  const N = 16;
  const inb = (x, y) => x >= 0 && y >= 0 && x < N && y < N;
  const pip = (px, py, pts) => {
    let inside = false;
    for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
      const [xi, yi] = pts[i], [xj, yj] = pts[j];
      if ((yi > py) !== (yj > py) && px < (xj - xi) * (py - yi) / (yj - yi) + xi) inside = !inside;
    }
    return inside;
  };
  // 形状：给像素 (x, y) 判断在不在里面（按像素中心算）
  const S = {
    rect: (x0, y0, x1, y1) => (x, y) => x >= x0 && x <= x1 && y >= y0 && y <= y1,
    circle: (cx, cy, r) => (x, y) => (x + .5 - cx) ** 2 + (y + .5 - cy) ** 2 <= r * r,
    poly: pts => (x, y) => pip(x + .5, y + .5, pts),
    or: (...f) => (x, y) => f.some(g => g(x, y)),
    and: (...f) => (x, y) => f.every(g => g(x, y)),
    not: f => (x, y) => !f(x, y),
    // 逐行给出 [起, 止]（含两端），小图形手排比算出来的干净；null 表示这一行空
    rows: t => (x, y) => !!t[y] && x >= t[y][0] && x <= t[y][1],
    // 两头尖的叶片：中心 (cx, cy)，朝 (dx, dy) 方向，半长 a、最宽处半宽 b
    leaf: (cx, cy, dx, dy, a, b, p = 0.8) => {
      const l = Math.hypot(dx, dy), ux = dx / l, uy = dy / l;
      return (x, y) => {
        const px = x + .5 - cx, py = y + .5 - cy, u = px * ux + py * uy, v = -px * uy + py * ux;
        return Math.abs(u) <= a && Math.abs(v) <= b * Math.pow(Math.max(0, 1 - (u / a) ** 2), p);
      };
    },
  };
  const hash = (x, y, s = 0) => {
    let h = (x * 374761393 + y * 668265263 + s * 982451653) >>> 0;
    h = Math.imul(h ^ (h >>> 13), 1274126177) >>> 0;
    return (h ^ (h >>> 16)) >>> 0;
  };
  const pick = (arr, x, y, s) => arr[hash(x, y, s) % arr.length];
  const grid = () => Array.from({ length: N }, () => Array(N).fill(null));

  // 实心物体：最外一圈描边，里面一圈左上亮、右下暗，中间本色。c = { o, light, base, dark, tex? }
  function obj(g, f, c) {
    const In = (x, y) => inb(x, y) && f(x, y);
    const edge = (x, y) => In(x, y) && (!In(x - 1, y) || !In(x + 1, y) || !In(x, y - 1) || !In(x, y + 1));
    for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) {
      if (!In(x, y)) continue;
      let col = c.base, ring = "base";
      if (edge(x, y)) { col = c.o; ring = "o"; }
      else if (c.light && (edge(x, y - 1) || edge(x - 1, y))) { col = c.light; ring = "light"; }
      else if (c.dark && (edge(x, y + 1) || edge(x + 1, y))) { col = c.dark; ring = "dark"; }
      if (c.tex && ring !== "o") { const t = c.tex(x, y, ring); if (t) col = t; }
      g[y][x] = col;
    }
  }
  const fill = (g, f, col) => { for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) if (f(x, y)) g[y][x] = typeof col === "function" ? col(x, y) : col; };
  const dot = (g, list, col) => { for (const [x, y] of list) if (inb(x, y)) g[y][x] = col; };

  // 斜看的立方体：顶面菱形 8 行、两个侧面各 7 行，占满 16 宽。faces.top(x, y)、faces.left(x, v)、faces.right(x, v)，v 是侧面从上往下第几行
  function cube(g, faces, o) {
    const dx = x => (x < 8 ? 7 - x : x - 8), topRow = x => Math.floor(dx(x) / 2), botRow = x => 7 - Math.floor(dx(x) / 2);
    const top = (x, y) => inb(x, y) && y >= topRow(x) && y <= botRow(x);
    const side = (x, y) => inb(x, y) && y > botRow(x) && y <= botRow(x) + 7;
    const all = (x, y) => top(x, y) || side(x, y);
    for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) {
      if (!all(x, y)) continue;
      const v = y - botRow(x) - 1;
      let col;
      if (!all(x - 1, y) || !all(x + 1, y) || !all(x, y - 1) || !all(x, y + 1)) col = o;
      else if (top(x, y)) col = side(x, y + 1) && faces.topEdge ? faces.topEdge : faces.top(x, y);
      else col = x < 8 ? faces.left(x, v) : faces.right(x, v);
      g[y][x] = col;
    }
  }

  const METAL = { o: "#1e232a", light: "#cfd6de", base: "#8f99a6", dark: "#5c6470" };
  function anvil(g, dy) {
    const m = S.or(
      S.poly([[0.5, 3 + dy], [15.5, 3 + dy], [15.5, 6 + dy], [4, 6 + dy], [2, 5 + dy]]),
      S.rect(6, 6 + dy, 10, 8 + dy), S.rect(5, 9 + dy, 11, 9 + dy),
      S.rect(3, 10 + dy, 13, 11 + dy), S.rect(2, 12 + dy, 14, 13 + dy));
    obj(g, m, METAL);
  }

  const DRAW = {
    // 原版：草方块
    vanilla: g => cube(g, {
      top: (x, y) => pick(["#6cbf3a", "#6cbf3a", "#6cbf3a", "#7fcd48", "#5aa832", "#8fd65a"], x, y, 1),
      topEdge: "#8fd65a",
      left: (x, v) => v <= 1 || (v === 2 && hash(x, 0, 3) % 2 === 0) ? pick(["#5eaa34", "#5eaa34", "#4c9a2c"], x, v, 2) : pick(["#93613a", "#93613a", "#93613a", "#a8754a", "#6f4727"], x, v, 4),
      right: (x, v) => v <= 1 || (v === 2 && hash(x, 0, 5) % 2 === 0) ? pick(["#4a922b", "#4a922b", "#3f8524"], x, v, 6) : pick(["#7a5030", "#7a5030", "#7a5030", "#8d6038", "#5a3a20"], x, v, 7),
    }, "#1f1a12"),
    // Paper：纸飞机（三块分开上色，写在下面 DRAW.paper）
    // Purpur：紫珀块
    // 三个面拉开明暗（顶最亮、左中、右最暗），侧面的方格缝也加深，不然看上去是一团紫色
    purpur: g => cube(g, {
      // 顶面也画上方格缝：两条顺着棱的斜线从中间穿过，和侧面一样是四块
      top: (x, y) => Math.abs((y + .5 - 4) - (x + .5 - 8) / 2) < .5 || Math.abs((y + .5 - 4) + (x + .5 - 8) / 2) < .5 ? "#b48fd6"
        : pick(["#dcc4f1", "#dcc4f1", "#dcc4f1", "#e6d3f6"], x, y, 8),
      topEdge: "#f1e5fb",
      left: (x, v) => (v === 3 || x === 3) ? "#6b4590" : pick(["#9b6cc0", "#9b6cc0", "#a476c7"], x, v, 9),
      right: (x, v) => (v === 3 || x === 12) ? "#3f2459" : pick(["#5e3a80", "#5e3a80", "#664287"], x, v, 10),
    }, "#24162f"),
    // Leaves：一片叶子
    leaves: g => {
      const lf = S.leaf(8.3, 7.5, 1, -1, 7.4, 3.4, 0.75);
      obj(g, lf, { o: "#1c4a24", light: "#8fdc7e", base: "#55b55c", dark: "#3b8f45",
        // 叶脉：中间一道深色主脉，两边每隔三格一道斜的侧脉
        tex: (x, y, ring) => { const px = x + .5 - 8.3, py = y + .5 - 7.5, u = (px - py) / Math.SQRT2, v = (px + py) / Math.SQRT2;
          if (Math.abs(v) < .6 && u > -6.2 && u < 5.8) return "#2f7a38";
          if (ring === "base" && Math.abs(v) >= 1 && Math.abs(v) <= 2.3 && Math.abs((((u - Math.abs(v)) % 3) + 3) % 3 - 1.5) < .55) return "#46a24f";
          return null; } });
      dot(g, [[2, 13], [1, 14]], "#1c4a24");
    },
    // Folia：花盆里的树苗
    folia: g => {
      obj(g, S.poly([[3.6, 11], [12.4, 11], [11.2, 15.6], [4.8, 15.6]]), { o: "#4a2416", light: "#e0905f", base: "#b9683e", dark: "#8a4628" });
      dot(g, [[5, 12], [6, 12], [7, 12], [8, 12], [9, 12], [10, 12]], "#5a3a20");
      fill(g, S.rect(7, 7, 7, 11), "#6aa83f"); fill(g, S.rect(8, 7, 8, 11), "#3f7a2a");
      const L = { o: "#134a40", light: "#86e3c9", base: "#41b79b", dark: "#2a8a75" };
      obj(g, S.leaf(4.5, 6.4, -1, -0.5, 4.1, 2.3, 0.8), L);
      obj(g, S.leaf(11.5, 4.7, 1, -0.72, 4.3, 2.4, 0.8), L);
    },
    // Fabric：线轴
    fabric: g => {
      const T = { o: "#5b4a2a", light: "#fff2cf", base: "#ead7a6", dark: "#c8b07c", tex: (x, y, r) => (r === "base" && y % 2 === 0 ? "#d9c28e" : null) };
      obj(g, S.rect(4, 3, 11, 12), T);
      const W = { o: "#3d2814", light: "#e8bd84", base: "#c4925a", dark: "#946635" };
      obj(g, S.rect(2, 1, 13, 3), W); obj(g, S.rect(2, 12, 13, 14), W);
      dot(g, [[12, 7], [13, 8], [13, 9], [14, 10], [14, 11]], "#b89a5e");
    },
    // Forge：铁砧
    forge: g => anvil(g, 1),
    // NeoForge：铁砧 + 烧红的锭 + 火星
    neoforge: g => {
      anvil(g, 2);
      obj(g, S.rect(5, 2, 10, 4), { o: "#6b1f08", light: "#ffe08a", base: "#ffa53a", dark: "#e0701e" });
      dot(g, [[3, 1], [12, 0], [13, 2], [2, 3]], "#ffd35a");
    },
    // Mohist：齿轮
    // 齿是 4 格宽的方块（斜向的齿是转了 45° 的方块），齿根埋进轮身，不是一根根尖刺
    mohist: g => {
      const dia = (cx, cy, r) => S.poly([[cx, cy - r], [cx + r, cy], [cx, cy + r], [cx - r, cy]]);
      obj(g, S.and(S.or(S.circle(8, 8, 5.3),
        S.rect(6, 0, 9, 3), S.rect(6, 12, 9, 15), S.rect(0, 6, 3, 9), S.rect(12, 6, 15, 9),
        dia(3.4, 3.4, 2.6), dia(12.6, 3.4, 2.6), dia(3.4, 12.6, 2.6), dia(12.6, 12.6, 2.6)),
        S.not(S.circle(8, 8, 2.3))), { o: "#3f1a0b", light: "#f0aa78", base: "#c97a44", dark: "#8f4c24" });
    },
    // Taiyitist（原 Banner）：挂着的旗帜
    banner: g => {
      obj(g, S.rect(1, 1, 14, 3), { o: "#3a240f", light: "#c48a52", base: "#8f5d30", dark: "#6a4220" });
      obj(g, S.and(S.rect(3, 3, 12, 15), (x, y) => !(y >= 13 && Math.abs(x + .5 - 8) < y - 11.5)),
        { o: "#4a3410", light: "#f8de8a", base: "#e2b84e", dark: "#b88d28" });
      fill(g, (x, y) => Math.abs(x + .5 - 8) + Math.abs(y + .5 - 8.5) <= 2.6, "#b8432f");
      dot(g, [[7, 8], [8, 8]], "#e8735a");
    },
    // Velocity：闪电（快）。两道箭头在 16 格里要么太细、要么像括号，换成闪电。
    // 逐行手排：上半截 6~7 格宽往左下斜，中间一道横带，下半截从横带右边接出来收成尖；用多边形算的话尾巴只剩一条描边
    velocity: g => obj(g, S.rows([null, [6, 12], [6, 11], [5, 11], [5, 10], [4, 10], [4, 13], [3, 13], [3, 12],
      [7, 11], [7, 10], [6, 10], [6, 9], [5, 8], [5, 7], [4, 5]]),
      { o: "#4a2f04", light: "#fff2b0", base: "#ffd04a", dark: "#e09a1a" }),
    // Waterfall：一滴水（石崖加水柱在 16 格里怎么画都像窗户或水龙头，换成最好认的水滴）。
    // 逐行给宽度，尖头 2 格、每行往外放一格，算出来的圆加三角在尖头下面会留一截「瓶颈」
    waterfall: g => {
      obj(g, S.rows([null, [7, 8], [6, 9], [6, 9], [5, 10], [4, 11], [4, 11], [3, 12], [3, 12], [2, 13], [2, 13], [2, 13], [3, 12], [3, 12], [4, 11], [6, 9]]),
        { o: "#123a63", light: "#9fdcff", base: "#3f8fd6", dark: "#2a66a6" });
      dot(g, [[5, 7], [4, 9], [4, 10]], "#f2faff");
    },
    // 没认出来的核心：圆石
    unknown: g => cube(g, {
      top: (x, y) => pick(["#a3abb4", "#a3abb4", "#8a929b", "#bcc3ca", "#6f7780"], x, y, 11),
      topEdge: "#c3c9d0",
      left: (x, v) => pick(["#8a929b", "#8a929b", "#737b84", "#9aa2ab", "#5d646c"], x, v, 12),
      right: (x, v) => pick(["#6b737b", "#6b737b", "#575e66", "#7b838c", "#474d54"], x, v, 13),
    }, "#22262b"),
  };

  // 纸飞机：上翼、下翼、机腹三块各自上色，外圈统一描边
  DRAW.paper = g => {
    const up = S.poly([[0.3, 7.4], [15.7, 0.3], [6.5, 10.2]]), low = S.poly([[6.5, 10.2], [15.7, 0.3], [9.2, 15.7]]), under = S.poly([[0.3, 7.4], [6.5, 10.2], [4.2, 13.2]]);
    const all = (x, y) => inb(x, y) && (up(x, y) || low(x, y) || under(x, y));
    const edge = (x, y) => all(x, y) && (!all(x - 1, y) || !all(x + 1, y) || !all(x, y - 1) || !all(x, y + 1));
    for (let y = 0; y < N; y++) for (let x = 0; x < N; x++) {
      if (!all(x, y)) continue;
      g[y][x] = edge(x, y) ? "#2c3346" : up(x, y) ? ((edge(x, y - 1) || edge(x - 1, y)) ? "#ffffff" : "#eef2f9") : low(x, y) ? "#bfc9dc" : "#8793ad";
    }
  };

  const CORE_COLORS = (root.MDPixel && root.MDPixel.CORE_COLORS) || {
    vanilla: "#5b8c3a", paper: "#6e7189", purpur: "#9b6bc7", leaves: "#3f9e57", folia: "#2f9e94", fabric: "#a08a5a",
    neoforge: "#d0743a", forge: "#4f6a8f", mohist: "#b05f36", banner: "#c4942f", velocity: "#2f9fb8", waterfall: "#3f7fc7",
  };
  const cache = {};
  function pixels(core) {
    if (!cache[core]) { const g = grid(); (DRAW[core] || DRAW.unknown)(g); cache[core] = g; }
    return cache[core];
  }
  function svg(core, size = 32) {
    const g = pixels(core);
    let out = "";
    for (let y = 0; y < N; y++) {
      let x = 0;
      while (x < N) {
        const c = g[y][x];
        if (!c) { x++; continue; }
        let w = 1;
        while (x + w < N && g[y][x + w] === c) w++;
        out += `<rect x="${x}" y="${y}" width="${w}" height="1" fill="${c}"/>`;
        x += w;
      }
    }
    return `<svg viewBox="0 0 16 16" width="${size}" height="${size}" shape-rendering="crispEdges" aria-hidden="true">${out}</svg>`;
  }
  function avatar(core, cls = "", size = 32) {
    const c = CORE_COLORS[core] || "#7a8aa0";
    return `<span class="px-avatar${cls ? " " + cls : ""}" style="--c:${c}" aria-hidden="true">${svg(core, size)}</span>`;
  }
  root.MDPixel16 = { svg, avatar, CORE_COLORS, CORES: ["vanilla", "paper", "purpur", "leaves", "folia", "fabric", "forge", "neoforge", "mohist", "banner", "velocity", "waterfall", "unknown"] };
})(window);
