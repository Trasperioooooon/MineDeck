// 版本详情（ARCH 4.5）：/releases/<tag>/。左侧版本导航，右侧版本头、状态提示、下载格、发布说明全文、上一版/下一版、底部操作。
import { esc, fmt, zhAttr } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { crumbs, versionBadge, extLink, toc, go } from '../partials/ui.mjs';
import { fileTable, localBody } from '../partials/release.mjs';

export default function release(ctx) {
  const { L, data, cfg, page } = ctx;
  const R = L.release;
  const r = data.byTag[page.params.tag];
  const by = (t) => t && data.byTag[t];
  const channel = (x) => (x.prerelease ? L.log.betaF : L.log.stable);
  // 状态提示条：只出现一条
  const notice = r.newer_same
    ? `<p class="notice"><a href="${ctx.href('release', { tag: r.newer_same })}">${esc(fmt(R.newer, { channel: channel(r), tag: r.newer_same }))} →</a></p>`
    : r.prerelease ? `<p class="notice">${esc(R.betaNote)}</p>`
    : r.legacy ? `<p class="notice">${esc(R.legacyNote)}</p>` : '';
  const since = r.days_since_prev == null ? R.first : r.days_since_prev === 0 ? R.sameDay : fmt(R.sincePrev, { n: r.days_since_prev });
  const pagerItem = (t, label, cls) => {
    const x = by(t);
    return x ? `<a class="card ${cls}" href="${ctx.href('release', { tag: x.tag })}"><small>${esc(label)}</small><b>${esc(x.tag)}</b></a>` : '<span></span>';
  };
  const side = `<nav class="vnav card" aria-label="${esc(R.versions)}"><b>${esc(R.versions)}</b><ol>${data.releases.map((x) =>
    `<li><a href="${ctx.href('release', { tag: x.tag })}"${x.tag === r.tag ? ' aria-current="page"' : ''}><span>${esc(x.tag)}</span><i class="dot ${x.prerelease ? 'b' : 's'}" title="${esc(channel(x))}"></i></a></li>`).join('')}</ol></nav>`;
  const jump = `<form class="vjump js-only" action="${ctx.href('releases')}" data-jump><label><span>${esc(R.jump)}</span><select name="v">${data.releases.map((x) =>
    `<option value="${ctx.href('release', { tag: x.tag })}"${x.tag === r.tag ? ' selected' : ''}>${esc(x.tag)}${x.prerelease ? ' · ' + esc(L.log.betaF) : ''}</option>`).join('')}</select></label></form>`;
  const notesToc = r.toc.length > 3 ? toc(R.toc, r.toc) : '';
  const main = `${crumbs(ctx, [ctx.crumb('home', L.crumbHome), ctx.crumb('releases', L.log.h), ctx.crumb('release', r.tag, { tag: r.tag })])}
<div class="rel-layout">
 <aside class="rel-side">${side}</aside>
 <article class="rel-main">
  <header class="rel-head">
   <h1 class="pix page-title">${esc(r.tag)}</h1>
   <div class="meta">${versionBadge(L, r)}<time datetime="${esc(r.published_at)}">${esc(r.date_local)} ${esc(L.tzLabel)}</time><span>${esc(since)}</span>${r.exe ? `<span>${esc(r.exe.name)} · ${esc(r.exe.size_label)}</span>` : ''}</div>
   ${jump}
  </header>
  ${notice}
  <section class="sec-s" id="files"><h2 class="sub-h">${esc(R.files)}</h2>${fileTable(ctx, r)}</section>
  <section class="sec-s" id="notes"><h2 class="sub-h">${esc(R.notes)}</h2>
   ${R.notesLang ? `<p class="muted">${esc(R.notesLang)}</p>` : ''}
   ${notesToc}
   ${r.body_html ? `<div class="notes card"${zhAttr(ctx)}>${localBody(ctx, r)}</div>` : `<p class="muted">${esc(L.log.noNotes)}</p>`}
  </section>
  <nav class="pager" aria-label="${esc(R.versions)}">${pagerItem(r.prev, R.prev, 'prev')}${pagerItem(r.next, R.next, 'next')}</nav>
  ${r.prev_same && r.prev_same !== r.prev ? `<p class="more">${go(ctx.href('release', { tag: r.prev_same }), `${R.prevSame}：${r.prev_same}`)}</p>` : ''}
  <div class="rel-acts">
   <a class="btn" href="${ctx.href('feedback')}?version=${encodeURIComponent(r.tag)}${r.prerelease ? '&amp;kind=beta' : ''}"><span>${esc(R.feedback)} →</span></a>
   ${extLink(ctx, r.html_url, R.viewGithub)}
   ${r.compare_url ? extLink(ctx, r.compare_url, R.compare) : ''}
  </div>
 </article>
</div>`;
  return layout(ctx, { title: fmt(R.title, { tag: r.tag }), desc: fmt(R.desc, { tag: r.tag }), main });
}
