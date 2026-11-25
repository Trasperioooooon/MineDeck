/* 旧的 12×12 核心头像（11-26 以前真程序 internal/web/ui/pixel.js 里的那套），原样冻结一份，只给对比页「现在」那一列和历史草图用；
   全局名改成 MDPixelOld，免得和换成 16×16 以后的 MDPixel 撞名。新画法在 pixel16.js，真程序换完以后以 internal/web/ui/pixel.js 为准。 */
/* MineDeck 像素素材（核心头像 / 空状态插画），全部生成内联 SVG，零外部资源。
   像素画只放在连续曲率的方块瓦片「里面」（像 App 图标），不被圆角裁切；瓦片外形仍是 squircle（见 app.css .px-avatar）。
   用法：MDPixel.avatar(coreId, "lg") → HTML 字符串；MDPixel.emptySVG(240) → 空状态插画；MDPixel.CORE_COLORS[coreId] → 核心色。 */
(function (root) {
  "use strict";
  const mix = (a, b, t) => {
    const p = h => [1, 3, 5].map(i => parseInt(h.slice(i, i + 2), 16));
    const A = p(a), B = p(b);
    return "#" + A.map((v, i) => Math.round(v + (B[i] - v) * t).toString(16).padStart(2, "0")).join("");
  };
  /* 固定色（草、泥土、金属、纸、水……），与核心色派生的 a / b / c / d 四档配合 */
  const FIXED = {
    g: "#6cbf3a", G: "#a6e05a", h: "#4c9a2c", e: "#93613a", E: "#6f4727", y: "#ffcc4d", o: "#ff8a3d", l: "#8dff6a",
    s: "#c3cad3", S: "#8a94a1", T: "#59616d", K: "#363c46", w: "#fbfaf6", W: "#dcd8cc", k: "#2a2622",
    u: "#4fa3e0", U: "#a9dcff", v: "#2f6fae", r: "#e0604f",
  };
  const roles = c => ({ a: c, b: mix(c, "#ffffff", 0.45), c: mix(c, "#000000", 0.3), d: mix(c, "#000000", 0.55), ...FIXED });
  /* 核心色：只作辨识（头像瓦片底、核心徽标前的小方块），不铺大色块 */
  const CORE_COLORS = {
    vanilla: "#5b8c3a", paper: "#6e7189", purpur: "#9b6bc7", leaves: "#3f9e57", folia: "#2f9e94", fabric: "#a08a5a",
    neoforge: "#d0743a", forge: "#4f6a8f", mohist: "#b05f36", banner: "#c4942f", velocity: "#2f9fb8", waterfall: "#3f7fc7",
  };
  /* 12×12 像素图案，'.' 透明。按核心名字的意象画：原版 = 草方块、Paper = 纸、Purpur = 紫珀块、Leaves = 树叶、
     Folia = 树苗、Fabric = 织物、Forge = 铁砧、NeoForge = 带火星的铁砧、Mohist = 齿轮、Banner = 旗帜、
     Velocity = 加速箭头、Waterfall = 瀑布；未知核心 = 圆石 */
  const quad = ["bbbbbd", "baaaad", "baccad", "baccad", "baaaad", "dddddd"];
  const weave = Array.from({ length: 12 }, (_, y) => Array.from({ length: 12 }, (_, x) =>
    ((Math.floor(x / 3) + Math.floor(y / 3)) % 2 === 0) ? (y % 3 === 1 ? "c" : "b") : (x % 3 === 1 ? "c" : "a")).join(""));
  const MOTIF = {
    vanilla: [
      "GGGGGGGGGGGG", "gggggggggggg", "gggggggggggg", "ghggghgghggh", "eehgeeeeehee", "eEeeeeEeeeee",
      "eeeeeeeeeEee", "eeEeeeeeeeee", "eeeeeEeeeeEe", "eEeeeeeeeeee", "eeeeeeeEeeee", "eeeEeeeeeeEe"],
    paper: [
      ".kkkkkkkk...", ".kwwwwwwkk..", ".kwwwwwwkWk.", ".kwwwwwwkkkk", ".kwwwwwwwwwk", ".kwaaaaaawwk",
      ".kwwwwwwwwwk", ".kwaaaaaaawk", ".kwwwwwwwwwk", ".kwaaaawwwwk", ".kwwwwwwwwwk", ".kkkkkkkkkkk"],
    purpur: [...quad.map(r => r + r), ...quad.map(r => r + r)],
    leaves: [
      "........dddd", "......ddbbbd", ".....dbbbaad", "....dbbaacad", "...dbbaacaad", "...dbaacaaad",
      "..dbaacaaad.", "..dbacaaadd.", "..dacaaadd..", "..dcaaddd...", ".dcdddd.....", "dc.........."],
    folia: [
      "............", ".ddd....ddd.", "dbbbd..dbbbd", "dbbaad.daabd", ".daaacdcaaad", "..dddacadd..",
      ".....dcd....", ".....dcd....", ".....dcd....", "...ddddddd..", "..dEEEEEEEd.", "..ddddddddd."],
    fabric: weave,
    forge: [
      "............", "............", ".SSSSSSSSSS.", "SsssssssssST", ".TSSSSSSSST.", "...TSSSST...",
      "....SSSS....", "....TSST....", "...TSSSST...", "..KKKKKKKK..", "..KKKKKKKK..", "............"],
    neoforge: [
      "........y...", ".......yoy..", "....y..yoy..", "........y...", "..SSSSSSSS..", ".SssssssssST",
      "..TSSSSSST..", "....SSSS....", "....TSST....", "...TSSSST...", "..KKKKKKKK..", "..KKKKKKKK.."],
    mohist: [
      ".....aa.....", "..a..aa..a..", "...aaaaaa...", "..aabbbbaa..", "aaab....caaa", "aaab....caaa",
      "aaab....caaa", "aaab....caaa", "..aaccccaa..", "...aaaaaa...", "..a..aa..a..", ".....aa....."],
    banner: [
      "kkkkkkkkkkkk", ".k.bbbbbbb..", ".k.baaaaab..", ".k.baccab...", ".k.baaaaab..", ".k.baaaaab..",
      ".k.baccab...", ".k.baaaaab..", ".k.baa.aab..", ".k.ba...ab..", ".k.b.....b..", ".k.........."],
    velocity: [
      "............", ".bb...bb....", ".abb..abb...", "..abb..abb..", "...abb..abb.", "....aab..aab",
      "....aac..aac", "...acc..acc.", "..acc..acc..", ".acc..acc...", ".cc...cc....", "............"],
    waterfall: [
      "KKKKKKKKKKKK", "TTTTTTTTTTTT", "..uUuuUuuU..", "..uuUuuUuu..", "..Uuuvuuvu..", "..uuUuuUuu..",
      "..uUuuvuuU..", "..uuuUuuvu..", "..Uuvuuuuu..", ".wuUwuuUwuw.", "wwwUwwwwuwww", ".wwwwwwwwww."],
    unknown: [
      "SSSSSSSSSSSS", "SssSSSSSSsSS", "SSSSTSSSSSSS", "SSSSSSSsSSTS", "STSSSSSSSSSS", "SSSSsSSSTSSS",
      "SSSSSSSSSSSS", "SsSSSSTSSSsS", "SSSSSSSSSSSS", "SSTSSsSSSSSS", "SSSSSSSSSTSS", "SSSsSSSSSSSS"],
  };
  /* 图案 → SVG：同色横向相邻像素合并成一个 rect；crispEdges 保证像素锐利。
     colors 的值以 "var(" 开头时写成 style（随主题变化的颜色，如强调色），否则写 fill 属性 */
  function rectsOf(rows, colors) {
    let out = "";
    for (let y = 0; y < rows.length; y++) {
      const row = rows[y];
      let x = 0;
      while (x < row.length) {
        const ch = row[x];
        if (ch === "." || !colors[ch]) { x++; continue; }
        let w = 1;
        while (x + w < row.length && row[x + w] === ch) w++;
        const c = colors[ch];
        out += `<rect x="${x}" y="${y}" width="${w}" height="1" ${c.startsWith("var(") ? `style="fill:${c}"` : `fill="${c}"`}/>`;
        x += w;
      }
    }
    return out;
  }
  /* 核心头像：squircle 瓦片（核心色浅底 + 方块倒角）里放 12×12 像素图案 */
  function avatar(core, cls = "") {
    const c = CORE_COLORS[core] || "#7a8aa0";
    const rows = MOTIF[core] || MOTIF.unknown;
    return `<span class="px-avatar${cls ? " " + cls : ""}" style="--c:${c}" aria-hidden="true">` +
      `<svg viewBox="0 0 12 12" width="24" height="24" shape-rendering="crispEdges">${rectsOf(rows, roles(c))}</svg></span>`;
  }
  /* 空状态插画：漂浮的草方块小岛 + 一台像素服务器 + 云 + 加号牌子（牌子取当前强调色）。48×30 网格 */
  const ISLAND = (() => {
    const W = 48, H = 30;
    const g = Array.from({ length: H }, () => Array(W).fill("."));
    const put = (x, y, ch) => { if (x >= 0 && x < W && y >= 0 && y < H) g[y][x] = ch; };
    const rect = (x0, y0, x1, y1, ch) => { for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) put(x, y, ch); };
    const cloud = (x, y) => { rect(x + 2, y, x + 7, y, "w"); rect(x, y + 1, x + 10, y + 2, "w"); rect(x + 1, y + 3, x + 9, y + 3, "W"); };
    cloud(2, 3); cloud(35, 1);
    const top = 17; // 小岛：草 2 行、泥土逐行收窄、石头尖
    rect(10, top, 37, top, "G"); rect(10, top + 1, 37, top + 1, "g");
    [[10, 37], [11, 36], [12, 35], [14, 33], [16, 31], [18, 29], [21, 26], [23, 24]].forEach(([a, b], i) => rect(a, top + 2 + i, b, top + 2 + i, i < 3 ? "e" : i < 6 ? "S" : "T"));
    for (let x = 10; x <= 37; x += 3) put(x, top + 2, "h");
    [[13, 20], [19, 21], [27, 20], [31, 19], [16, 22], [24, 23]].forEach(([x, y]) => put(x, y, "E"));
    const sx = 19, sy = 7; // 服务器方块（站在岛上）
    rect(sx, sy, sx + 9, sy + 9, "K"); rect(sx + 1, sy + 1, sx + 8, sy + 8, "T");
    rect(sx + 1, sy + 1, sx + 8, sy + 1, "S");
    rect(sx + 2, sy + 3, sx + 7, sy + 3, "k"); put(sx + 7, sy + 3, "l");
    rect(sx + 2, sy + 6, sx + 7, sy + 6, "k"); put(sx + 7, sy + 6, "y");
    rect(31, 11, 35, 15, "a"); rect(33, 12, 33, 14, "w"); rect(32, 13, 34, 13, "w"); rect(33, 16, 33, 16, "E"); // 加号牌子
    [[12, 16], [14, 16], [36, 16], [26, 16]].forEach(([x, y]) => put(x, y, "g")); // 草丛
    return { rows: g.map(r => r.join("")), W, H };
  })();
  function emptySVG(size = 240) {
    const { rows, W, H } = ISLAND;
    return `<svg class="px-art" viewBox="0 0 ${W} ${H}" width="${size}" height="${Math.round(size * H / W)}" shape-rendering="crispEdges" aria-hidden="true">` +
      `${rectsOf(rows, { ...FIXED, a: "var(--accent)" })}</svg>`;
  }
  root.MDPixelOld = { avatar, emptySVG, CORE_COLORS };
})(window);
