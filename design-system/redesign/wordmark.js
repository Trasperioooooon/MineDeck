/* MineDeck 像素字标（界面重构草图用）：自己画的块状像素字，往下挤出两格厚、外面一圈深色描边，有「立起来的方块字」的感觉。
   字形是 10 行高、笔画 2 格粗的方块字，一个一个手排，不是照着 Minecraft 的标志描的：Mojang 不许第三方做得像官方，
   所以不用它的字形、不用它那种石头纹理和透视，字面是平的，配色取 MineDeck 自己的图标（草绿 + 石板灰）。
   用法：MDWordmark.svg({ cell: 5, tone: "duo" | "mono" }) → SVG 字符串。duo：MINE 草绿、DECK 白；mono：全白。
   另有一颗像素心「♥」（首页字标右边的赞赏，作者 11-26 要的）：MDWordmark.svg({ text: "♥", cell: 5, label: "赞赏" })，同样的描边和挤出，固定玫红。 */
(function (root) {
  "use strict";
  const G = {
    M: ["##......##", "###....###", "####..####", "##.####.##", "##..##..##", "##......##", "##......##", "##......##", "##......##", "##......##"],
    I: ["######", "######", "..##..", "..##..", "..##..", "..##..", "..##..", "..##..", "######", "######"],
    N: ["##.....##", "####...##", "####...##", "##.##..##", "##.##..##", "##..##.##", "##..##.##", "##...####", "##...####", "##.....##"],
    E: ["#######", "#######", "##.....", "##.....", "######.", "######.", "##.....", "##.....", "#######", "#######"],
    D: ["######.", "#######", "##...##", "##...##", "##...##", "##...##", "##...##", "##...##", "#######", "######."],
    C: [".######", "#######", "##.....", "##.....", "##.....", "##.....", "##.....", "##.....", "#######", ".######"],
    K: ["##....##", "##...##.", "##..##..", "##.##...", "####....", "####....", "##.##...", "##..##..", "##...##.", "##....##"],
    // 心：9 宽 8 高，上下各空一行，和字一样按 10 行排，放在字旁边时上下居中
    "♥": [".........", ".##...##.", "####.####", "#########", "#########", ".#######.", "..#####..", "...###...", "....#....", "........."],
  };
  const PAL = {
    white: { hi: "#ffffff", face: "#e9edf2", e1: "#8b94a3", e2: "#5c6474" },
    grass: { hi: "#b4ec72", face: "#7ccf48", e1: "#4c9a2c", e2: "#346f1e" },
    rose: { hi: "#ffb4c2", face: "#f2607c", e1: "#b53a55", e2: "#80263b" },
    o: "#14171c",
  };
  const H = 10, DEPTH = 2;
  function svg({ text = "MINEDECK", cell = 5, tone = "duo", label = "MineDeck" } = {}) {
    // 字面：key "x,y" → 色板名；先排字，左右上下各留出描边和挤出的位置
    const face = new Map();
    let x0 = 1;
    [...text].forEach((ch, i) => {
      const g = G[ch];
      const pal = ch === "♥" ? "rose" : tone === "duo" && i < 4 ? "grass" : "white";
      g.forEach((row, y) => [...row].forEach((c, x) => { if (c === "#") face.set(`${x0 + x},${y + 1}`, pal); }));
      x0 += g[0].length + 1;
    });
    const W = x0, Ht = H + 2 + DEPTH;
    const cells = new Map();
    const has = (x, y) => face.has(`${x},${y}`);
    // 挤出：每个字面格往下挪 1、2 格，没被字面占的地方涂侧面色（近的浅、远的深）。往右下斜着挤，字和字之间的缝会被填死，认不出
    for (let k = DEPTH; k >= 1; k--) for (const [key, pal] of face) {
      const [x, y] = key.split(",").map(Number), nx = x, ny = y + k;
      if (!has(nx, ny)) cells.set(`${nx},${ny}`, PAL[pal][k === 1 ? "e1" : "e2"]);
    }
    // 字面：一笔的最上面一行提亮
    for (const [key, pal] of face) {
      const [x, y] = key.split(",").map(Number);
      cells.set(key, has(x, y - 1) ? PAL[pal].face : PAL[pal].hi);
    }
    // 描边：字面和挤出外面一圈（八个方向）
    const solid = new Set(cells.keys());
    for (let y = 0; y < Ht; y++) for (let x = 0; x < W; x++) {
      if (solid.has(`${x},${y}`)) continue;
      for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) if (solid.has(`${x + dx},${y + dy}`)) { cells.set(`${x},${y}`, PAL.o); dx = dy = 2; }
    }
    let out = "";
    for (let y = 0; y < Ht; y++) {
      let x = 0;
      while (x < W) {
        const c = cells.get(`${x},${y}`);
        if (!c) { x++; continue; }
        let w = 1;
        while (x + w < W && cells.get(`${x + w},${y}`) === c) w++;
        out += `<rect x="${x}" y="${y}" width="${w}" height="1" fill="${c}"/>`;
        x += w;
      }
    }
    return `<svg viewBox="0 0 ${W} ${Ht}" width="${W * cell}" height="${Ht * cell}" shape-rendering="crispEdges" role="img" aria-label="${label}">${out}</svg>`;
  }
  root.MDWordmark = { svg };
})(window);
