// 活动热力图（A4）：每天提交数分 5 级，发布日加描边。数据来自构建时的 data/activity.json，浏览器不请求 GitHub。
import { esc, fmt } from './util.mjs';

/**
 * @param ctx
 * @param {{weeks?:number, linkReleases?:boolean}} o weeks 显示最近多少周；linkReleases 发布日链接到版本详情
 */
export function heatmap(ctx, { weeks = 52, linkReleases = true, summary = false } = {}) {
  const { data, L } = ctx;
  const { days, releases } = data.activity;
  const buildT = Date.parse(data.generated_at);
  const end = new Date(data.localDay(buildT) + 'T00:00:00Z');
  let d = new Date(end.getTime() - (weeks * 7 - 1) * 86400e3);
  d = new Date(d.getTime() - d.getUTCDay() * 86400e3);
  const endWeek = new Date(end.getTime() + (6 - end.getUTCDay()) * 86400e3);
  const counts = Object.values(days).filter(Boolean).sort((a, b) => a - b);
  // 分级阈值按非零天数的四分位，避免一天几十次把其他天都压成最浅
  const q = (p) => counts.length ? counts[Math.min(counts.length - 1, Math.floor(p * counts.length))] : 1;
  const th = [q(0.25), q(0.5), q(0.75)];
  const level = (n) => (!n ? 0 : n <= th[0] ? 1 : n <= th[1] ? 2 : n <= th[2] ? 3 : 4);
  const cells = [];
  let total = 0, active = 0, relN = 0;
  for (let i = 0; d <= endWeek; d = new Date(d.getTime() + 86400e3), i++) {
    const k = d.toISOString().slice(0, 10);
    const w = ` style="--i:${Math.floor(i / 7)}"`;
    if (d > end) { cells.push(`<span class="x"${w}></span>`); continue; }
    const n = days[k] || 0;
    total += n;
    if (n) active++;
    const rel = releases[k];
    if (rel) relN += rel.length;
    const t = rel ? fmt(L.heat.cellRel, { date: k, n, tags: rel.join('、') }) : fmt(L.heat.cell, { date: k, n });
    const cls = `h${level(n)}${rel ? ' r' : ''}`;
    cells.push(rel && linkReleases
      ? `<a class="${cls}" href="${ctx.href('release', { tag: rel[0] })}" title="${esc(t)}" aria-label="${esc(t)}"${w}></a>`
      : `<span class="${cls}" title="${esc(t)}"${w}></span>`);
  }
  const src = data.activity.source ? fmt(L.heat.source, { source: data.activity.source.replace(/ origin\/\S+| HEAD/, ''), date: ctx.fmtDate(data.activity.fetched_at || data.generated_at) }) : '';
  const sum = summary ? `<dl class="heat-sum">${[[total, L.heat.sumCommits], [relN, L.heat.sumReleases], [active, L.heat.sumDays]].map(([n, t]) => `<div><dt>${esc(t)}</dt><dd class="pix" data-count="${n}">${n}</dd></div>`).join('')}</dl>` : '';
  return `<div class="card heat${summary ? ' has-sum' : ''}" data-anim="drop">
 <div class="cal-scroll"><div class="cal-grid heat-grid" role="img" aria-label="${esc(fmt(L.heat.aria, { weeks }))}">${cells.join('')}</div></div>
 <div class="legend"><span>${esc(L.heat.less)}</span><span class="scale"><i class="h0"></i><i class="h1"></i><i class="h2"></i><i class="h3"></i><i class="h4"></i></span><span>${esc(L.heat.more)}</span><span><i class="h2 r"></i>${esc(L.heat.release)}</span>${src ? `<span class="heat-src">${esc(src)}</span>` : ''}</div>
${sum}</div>`;
}

/** 每月发布数小柱图（关于页） */
export function monthlyBars(ctx) {
  const { data, L } = ctx;
  const m = {};
  for (const r of data.releases) m[r.day_local.slice(0, 7)] = (m[r.day_local.slice(0, 7)] || 0) + 1;
  const keys = Object.keys(m).sort();
  if (!keys.length) return '';
  // 补齐中间没有发布的月份
  const all = [];
  for (let [y, mo] = keys[0].split('-').map(Number); `${y}-${String(mo).padStart(2, '0')}` <= keys[keys.length - 1]; mo === 12 ? (y++, mo = 1) : mo++) all.push(`${y}-${String(mo).padStart(2, '0')}`);
  const max = Math.max(...Object.values(m));
  return `<div class="card bars" role="img" aria-label="${esc(L.about.monthlyAria)}">${all.map((k) => `<div class="bar"><span style="--h:${((m[k] || 0) / max).toFixed(3)}"><b>${m[k] || ''}</b></span><small>${k.slice(2)}</small></div>`).join('')}</div>`;
}
