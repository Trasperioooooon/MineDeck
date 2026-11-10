// 版本相关组件：下载按钮、日志条目、发布日历。
import { esc, fmt, zhAttr } from './util.mjs';
import { versionBadge } from './ui.mjs';

/** 某个版本的下载按钮：镜像（主）+ GitHub（备用） */
export function dlButtons(ctx, r, small) {
  const { L } = ctx;
  const exe = r.exe;
  if (!exe) return `<a class="btn sm" href="${esc(r.html_url)}">${esc(L.log.onGithub)}</a>`;
  const cls = small ? 'btn sm' : 'btn';
  return (exe.mirror_url ? `<a class="${cls} primary" href="${esc(exe.mirror_url)}">${esc(L.log.mirror)} · ${esc(exe.name)}</a>` : '') +
    `<a class="${cls}" href="${esc(exe.github_url)}">${esc(L.log.github)} · ${esc(exe.size_label)}</a>`;
}

/** 版本元信息行：版本号、徽章、发布时间、体积 */
export function releaseMeta(ctx, r, verHref) {
  return `<div class="meta"><a class="ver" href="${esc(verHref)}">${esc(r.tag)}</a>${versionBadge(ctx.L, r)}<time datetime="${esc(r.published_at)}">${esc(r.date_local)} UTC+${ctx.cfg.timezoneOffsetHours}</time>${r.exe ? `<span>${esc(r.exe.size_label)}</span>` : ''}</div>`;
}

/**
 * 时间线上的一个版本。
 * @param {{full:boolean}} o full=true 显示完整说明与下载（旧版日志页），否则只显示摘要
 */
export function entry(ctx, r, { full }) {
  const { L } = ctx;
  const others = r.assets.filter((x) => x !== r.exe);
  const lead = `<p class="summary"${zhAttr(ctx, !!r.summary)}>${esc(r.summary || L.log.noNotes)}</p>`;
  const notes = r.body_html
    ? lead + `<div class="notes"${zhAttr(ctx)}>${r.body_html}</div><button type="button" class="toggle" aria-expanded="false" data-more="${esc(L.log.expand)}" data-less="${esc(L.log.collapse)}">${esc(L.log.expand)}</button>`
    : lead;
  const verHref = full ? '#' + r.tag : ctx.href('changelog') + '#' + r.tag;
  return `<article class="entry" id="${esc(r.tag)}" data-type="${r.prerelease ? 'beta' : 'stable'}">
 <div class="av" aria-hidden="true"><img src="${ctx.a('logo-144.png')}" alt="" width="34" height="34"></div>
 <div class="card">
  ${releaseMeta(ctx, r, verHref)}
  ${full ? notes : lead}
  ${full ? `<div class="dl">${dlButtons(ctx, r, true)}<a class="gh" href="${esc(r.html_url)}">${esc(L.log.onGithub)} →</a></div>
  ${others.length ? `<details class="files"><summary>${esc(L.log.otherFiles)} (${others.length})</summary><ul>${others.map((x) => `<li><a href="${esc(x.github_url)}">${esc(x.name)}</a> · ${esc(x.size_label)}${x.mirror_url ? ` · <a href="${esc(x.mirror_url)}">${esc(L.log.mirror)}</a>` : ''}</li>`).join('')}</ul></details>` : ''}
  ${r.exe && r.exe.sha256 ? `<div class="sha">SHA-256 ${esc(r.exe.name)}: ${esc(r.exe.sha256)}</div>` : ''}` : ''}
 </div>
</article>`;
}

/** 发布日历：最近最多 52 周，按周排成列；有发布的日子可点 */
export function releaseCalendar(ctx) {
  const { data, L, cfg } = ctx;
  const off = cfg.timezoneOffsetHours * 3600e3;
  const day = (t) => new Date(t + off).toISOString().slice(0, 10);
  const map = {};
  for (const r of data.releases) (map[day(Date.parse(r.published_at))] ||= []).push(r);
  const buildT = Date.parse(data.generated_at);
  const first = data.releases.length ? Math.min(...data.releases.map((r) => Date.parse(r.published_at))) : buildT;
  let start = Math.min(first - 14 * 86400e3, buildT - 26 * 7 * 86400e3);
  start = Math.max(start, buildT - 52 * 7 * 86400e3);
  // 对齐到周日（本地时区）
  let d = new Date(day(start) + 'T00:00:00Z');
  d = new Date(d.getTime() - d.getUTCDay() * 86400e3);
  const end = new Date(day(buildT) + 'T00:00:00Z');
  const endWeek = new Date(end.getTime() + (6 - end.getUTCDay()) * 86400e3);
  const cells = [];
  const from = d.toISOString().slice(0, 10);
  for (; d <= endWeek; d = new Date(d.getTime() + 86400e3)) {
    const k = d.toISOString().slice(0, 10);
    const rs = map[k];
    if (d > end) cells.push('<span class="x"></span>');
    else if (rs) {
      const cls = rs.some((r) => !r.prerelease) ? 's' : 'b';
      const t = `${k}：${rs.map((r) => r.tag).join('、')}`;
      cells.push(`<a class="${cls}" href="#${esc(rs[0].tag)}" title="${esc(t)}" aria-label="${esc(t)}"></a>`);
    } else cells.push(`<span title="${k}"></span>`);
  }
  return `<div class="card cal"><div class="cal-scroll"><div class="cal-grid" role="img" aria-label="${esc(L.log.calendar)}">${cells.join('')}</div></div>
 <div class="legend"><span><i style="background:var(--color-grass)"></i>${esc(L.log.stable)}</span><span><i style="background:var(--color-beta)"></i>${esc(L.log.betaF)}</span><span>${esc(fmt(L.log.calendarRange, { from, to: end.toISOString().slice(0, 10) }))}</span></div></div>`;
}
