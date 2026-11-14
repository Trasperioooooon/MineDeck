// 更新日志列表（ARCH 4.4）：统计、发布日历、渠道筛选（?channel=）、按大版本分组的摘要卡片。全文在版本详情页。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, stat, extLink } from '../partials/ui.mjs';
import { entry, releaseCalendar } from '../partials/release.mjs';
import { icon } from '../partials/svg.mjs';

export default function releases(ctx) {
  const { L, data, cfg } = ctx;
  const G = L.log;
  const st = data.latest_stable, bt = data.latest_beta, last = data.releases[0];
  const groups = data.groups.map((g) => `<section data-group>
  <h2 class="group-h">${esc(g.legacy ? G.groupLegacy : fmt(G.groupMajor, { v: g.key }))}</h2>${g.legacy ? `<p class="group-note">${esc(G.legacyNote)}</p>` : ''}
  <div class="log">${g.releases.map((r) => entry(ctx, r)).join('\n')}</div></section>`).join('\n');
  const lead = esc(fmt(G.sub, { n: data.releases.length, date: last ? last.day_local : '' })) + (G.notesLang ? ' ' + esc(G.notesLang) : '') +
    ` <a class="feed-link" href="${ctx.root}feed.xml">${icon('feed')}${esc(G.feed)}</a>`;
  const main = `${pageHead(ctx, { h: G.h, lead, crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('releases', G.h)] })}
<section class="sec page-top">
 <div class="stats">
  ${st ? stat({ label: G.latestStable, value: st.tag, href: ctx.href('release', { tag: st.tag }), cls: 's1' }) : ''}
  ${bt ? stat({ label: G.latestBeta, value: bt.tag, href: ctx.href('release', { tag: bt.tag }), cls: 's2' }) : ''}
  ${stat({ label: G.total, value: String(data.releases.length), cls: 's3' })}
  ${last ? `<div class="card stat s4"><small>${esc(G.lastRelease)}</small><b data-ago="${esc(last.published_at)}" data-tpl="${esc(G.daysAgo)}" data-today="${esc(G.today)}">${esc(last.day_local)}</b></div>` : ''}
 </div>
 ${releaseCalendar(ctx)}
 <div class="filters" role="group" aria-label="${esc(G.filterAria)}">
  <button type="button" class="chip" data-type="all" aria-pressed="true">${esc(G.all)}</button>
  <button type="button" class="chip" data-type="stable" aria-pressed="false">${esc(G.stable)}</button>
  <button type="button" class="chip" data-type="beta" aria-pressed="false">${esc(G.betaF)}</button>
 </div>
 <div id="releases">${groups}</div>
 <p class="more">${extLink(ctx, `https://github.com/${cfg.repo}/releases`, G.allOnGithub)}</p>
</section>`;
  return layout(ctx, { title: G.title, desc: G.desc, main });
}
