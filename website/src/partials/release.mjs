// 版本相关组件：下载按钮、版本元信息、摘要卡片、下载格（文件表）、发布日历。
import { esc, fmt, zhAttr } from './util.mjs';
import { versionBadge, button, extLink, copyable, go } from './ui.mjs';
import { icon } from './svg.mjs';

/** 正文里的站内占位链接 md-release:<tag> 换成当前页面的相对地址 */
export const localBody = (ctx, r) => r.body_html.replace(/href="md-release:([^"]+)"/g, (m, t) => `href="${ctx.href('release', { tag: t })}"`);

/**
 * exe 的主下载按钮：镜像（主）+ GitHub（备用，外链）。
 * quiet=true 时两个按钮都用次要样式（测试版用，免得和稳定版一样显眼）。
 */
export function dlButtons(ctx, r, { size, big, quiet } = {}) {
  const { L } = ctx;
  const exe = r.exe;
  if (!exe) return extLink(ctx, r.html_url, L.release.viewGithub, { cls: 'btn sm' });
  const primary = exe.mirror_url
    ? button({ href: exe.mirror_url, label: big ? fmt(L.home.download, { version: r.tag }) : L.download.mirrorBtn, sub: big ? fmt(L.home.downloadSub, { file: exe.name, size: exe.size_label, date: r.day_local }) : `${exe.name} · ${exe.size_label}`, primary: !quiet, pix: big, size, icon: 'arrow' })
    : '';
  const gh = button({ href: exe.github_url, label: L.download.githubBtn, sub: primary ? L.fallback : `${exe.name} · ${exe.size_label}`, primary: !primary && !quiet, size, ext: true, ctx });
  return primary + gh;
}

/**
 * 手机上的提示：MineDeck 是 Windows 程序，给出下载页地址和复制按钮。
 * 默认隐藏，只在窄屏的触屏设备上显示（pages.css 的 .on-phone）；紧跟其后的 .cta 加 has-phone-note，届时主按钮降为次要样式。
 */
export function phoneNote(ctx) {
  const { L, cfg } = ctx;
  const url = cfg.siteUrl.replace(/\/$/, '') + ctx.crumb('download', '').url;
  return `<div class="card on-phone"><div><b>${esc(L.phone.h)}</b>${esc(L.phone.p)} <code>${esc(url.replace(/^https?:\/\//, ''))}</code></div><button type="button" class="btn sm js-only" data-copy="${esc(url)}">${icon('copy', { cls: 'i-copy' })}${icon('check', { cls: 'i-check' })}<span>${esc(L.phone.copy)}</span></button></div>`;
}

/** 版本元信息行：版本号、徽章、发布时间、体积 */
export function releaseMeta(ctx, r, { link = true } = {}) {
  const ver = link ? `<a class="ver" href="${ctx.href('release', { tag: r.tag })}">${esc(r.tag)}</a>` : `<span class="ver">${esc(r.tag)}</span>`;
  return `<div class="meta">${ver}${versionBadge(ctx.L, r)}<time datetime="${esc(r.published_at)}">${esc(r.date_local)} ${esc(ctx.L.tzLabel)}</time>${r.exe ? `<span>${esc(r.exe.size_label)}</span>` : ''}</div>`;
}

/** 时间线上的摘要卡片（更新日志列表页）：左列日期 + 渠道，右列版本名、摘要、详情 */
export function entry(ctx, r) {
  const { L } = ctx;
  return `<article class="entry" id="${esc(r.tag)}" data-type="${r.channel}">
 <div class="av" aria-hidden="true"><img src="${ctx.a('logo-144.png')}" alt="" width="34" height="34"></div>
 <div class="card">
  ${releaseMeta(ctx, r)}
  <p class="summary"${zhAttr(ctx, !!r.summary)}>${esc(r.summary || L.log.noNotes)}</p>
  ${go(ctx.href('release', { tag: r.tag }), L.log.details)}
 </div>
</article>`;
}

/** 首页 / 下载页的小版本卡 */
export function miniRelease(ctx, r, label) {
  return `<div class="card mini-rel"><small>${esc(label)}</small>${releaseMeta(ctx, r)}<p class="summary"${zhAttr(ctx, !!r.summary)}>${esc(r.summary || ctx.L.log.noNotes)}</p>${go(ctx.href('release', { tag: r.tag }), ctx.L.home.details)}</div>`;
}

/** 下载格：每个文件一行，文件名、说明、大小、SHA-256（短显示，可复制）、镜像下载 + GitHub 备用 */
export function fileTable(ctx, r) {
  const { L } = ctx;
  if (!r.assets.length) return `<p class="muted">${esc(L.release.noFiles)}</p>`;
  const rows = r.assets.map((f) => `<tr>
  <td><b class="fname">${esc(f.name)}</b><small>${esc(L.release.fileDesc[f.kind] || '')}</small></td>
  <td>${esc(f.size_label)}</td>
  <td>${f.sha256 ? copyable(ctx, f.sha256, { short: true, label: L.release.copySha }) : '—'}</td>
  <td class="acts">${f.mirror_url ? button({ href: f.mirror_url, label: L.release.mirror, size: 'sm', primary: f.kind === 'exe', icon: 'arrow' }) : ''}${extLink(ctx, f.github_url, 'GitHub', { cls: 'gh-alt' })}</td>
 </tr>`).join('');
  return `<div class="tbl-wrap"><table class="files-table"><thead><tr><th>${esc(L.release.file)}</th><th>${esc(L.release.size)}</th><th>${esc(L.release.sha)}</th><th>${esc(L.release.actions)}</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

/** 发布日历：最近最多 52 周，按周排成列；有发布的日子可点（跳到列表里的卡片） */
export function releaseCalendar(ctx) {
  const { data, L } = ctx;
  const day = data.localDay;
  const map = {};
  for (const r of data.releases) (map[r.day_local] ||= []).push(r);
  const buildT = Date.parse(data.generated_at);
  const first = data.releases.length ? Math.min(...data.releases.map((r) => Date.parse(r.published_at))) : buildT;
  let start = Math.min(first - 14 * 86400e3, buildT - 26 * 7 * 86400e3);
  start = Math.max(start, buildT - 52 * 7 * 86400e3);
  let d = new Date(day(start) + 'T00:00:00Z');
  d = new Date(d.getTime() - d.getUTCDay() * 86400e3);
  const end = new Date(day(buildT) + 'T00:00:00Z');
  const endWeek = new Date(end.getTime() + (6 - end.getUTCDay()) * 86400e3);
  const cells = [];
  const from = d.toISOString().slice(0, 10);
  for (let i = 0; d <= endWeek; d = new Date(d.getTime() + 86400e3), i++) {
    const k = d.toISOString().slice(0, 10);
    const rs = map[k];
    const w = ` style="--i:${Math.floor(i / 7)}"`;
    if (d > end) cells.push(`<span class="x"${w}></span>`);
    else if (rs) {
      const cls = rs.some((r) => !r.prerelease) ? 's' : 'b';
      const t = `${k}：${rs.map((r) => r.tag).join('、')}`;
      cells.push(`<a class="${cls}" href="#${esc(rs[0].tag)}" title="${esc(t)}" aria-label="${esc(t)}"${w}></a>`);
    } else cells.push(`<span title="${k}"${w}></span>`);
  }
  return `<div class="card cal" data-anim="drop"><div class="cal-scroll"><div class="cal-grid" role="img" aria-label="${esc(L.log.calendar)}">${cells.join('')}</div></div>
 <div class="legend"><span><i class="lg-s"></i>${esc(L.log.stable)}</span><span><i class="lg-b"></i>${esc(L.log.betaF)}</span><span>${esc(fmt(L.log.calendarRange, { from, to: end.toISOString().slice(0, 10) }))}</span></div></div>`;
}
