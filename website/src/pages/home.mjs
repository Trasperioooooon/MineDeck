// 首页（ARCH 4.1）：这是什么、从哪下载、最近有没有在更新、出了问题去哪。其余内容只给入口。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { featCard, secHead, stat, go, button, extLink } from '../partials/ui.mjs';
import { dlButtons, miniRelease, phoneNote } from '../partials/release.mjs';
import { heatmap } from '../partials/heatmap.mjs';
import { peopleSplit } from '../partials/people.mjs';
import { illustration, icon } from '../partials/svg.mjs';
import { showcase } from '../partials/showcase.mjs';
import { repo } from '../routes.mjs';

export default function home(ctx) {
  const { L, data, cfg } = ctx;
  const H = L.home;
  const st = data.latest_stable, bt = data.latest_beta, last = data.releases[0];
  const stars = cfg.stats.showStars && data.repoInfo;
  const stats = [
    st && stat({ label: L.log.latestStable, value: st.tag, href: ctx.href('release', { tag: st.tag }), cls: 's1' }),
    data.show_beta && stat({ label: L.log.latestBeta, value: bt.tag, href: ctx.href('release', { tag: bt.tag }), cls: 's2' }),
    stat({ label: L.log.total, value: String(data.releases.length), href: ctx.href('releases'), cls: 's3' }),
    last && `<div class="card stat s4"><small>${esc(L.log.lastRelease)}</small><b data-ago="${esc(last.published_at)}" data-tpl="${esc(L.log.daysAgo)}" data-today="${esc(L.log.today)}">${esc(last.day_local)}</b></div>`,
    stars && stat({ label: H.stars, value: String(data.repoInfo.stargazers_count), cls: 's5', note: fmt(H.starsAsOf, { date: ctx.fmtDate(data.repoInfo.fetched_at) }) }),
  ].filter(Boolean);
  const main = `<section class="hero">
 <div>
  <span class="eyebrow">${esc(H.eyebrow)}</span>
  <h1>${H.h1}</h1>
  <p class="lead">${esc(H.lead)}</p>
  ${st ? phoneNote(ctx) : ''}
  <div class="cta has-phone-note">${st ? dlButtons(ctx, st, { big: true }) : ''}</div>
  <div class="sub-links">${go(ctx.href('download'), H.otherWays)}</div>
  <p class="note">${esc(H.tagline)}</p>
 </div>
 <div class="hero-art">
  <div class="card shot"><div class="grass-strip" aria-hidden="true"></div><img src="${ctx.a('01-servers.png')}" alt="${esc(H.shotAlt)}" width="1440" height="900"></div>
  <div class="card hero-anim">${illustration('hero', { label: H.heroAnimAria })}</div>
 </div>
</section>

<section class="sec" id="highlights">
 ${secHead({ h: H.show.h })}
 ${showcase(ctx)}
</section>

<section class="sec" id="features">
 ${secHead({ h: H.coreH, p: esc(H.coreSub), more: go(ctx.href('features'), H.allFeatures) })}
 <div class="grid cols-4">${H.core.map((it, i) => featCard(it, { i: [0, 6, 3, 2][i] })).join('\n')}</div>
</section>

<section class="sec" id="activity">
 ${secHead({ h: H.activityH, p: esc(H.activitySub), more: go(ctx.href('releases'), H.allReleases) })}
 <div class="stats${stats.length === 5 ? ' stats-5' : ''}" role="group" aria-label="${esc(H.statsAria)}">${stats.join('')}</div>
 <div class="act">
  ${heatmap(ctx, { weeks: cfg.homeHeatWeeks || 26, summary: true })}
  <div class="act-r">${st ? miniRelease(ctx, st, L.log.latestStable) : ''}${data.show_beta ? miniRelease(ctx, bt, L.log.latestBeta) : ''}</div>
 </div>
</section>

<section class="sec">
 <div class="card oss">
  <div><h2>${esc(H.ossH)}</h2><p>${esc(H.ossP)}</p>${peopleSplit(ctx, { max: cfg.contributors.max || 12 })}</div>
  <div class="row">${button({ href: repo(cfg), label: H.star, primary: true, ext: true, ctx })}${button({ href: ctx.href('about') + '#contribute', label: H.contribute })}</div>
 </div>
</section>

<section class="sec" id="help">
 ${secHead({ h: H.helpH })}
 <div class="grid cols-2">
  <a class="card help-card" href="${ctx.href('feedback')}">${icon('alert', { cls: 'feat-ico' })}<span><b>${esc(H.reportT)} →</b><small>${esc(H.reportD)}</small></span></a>
  <a class="card help-card" href="${ctx.href('help')}#faq">${icon('terminal', { cls: 'feat-ico' })}<span><b>${esc(H.faqT)} →</b><small>${esc(H.faqD)}</small></span></a>
 </div>
 <p class="note">${esc(H.sensitive)}</p>
</section>`;
  return layout(ctx, { title: L.site.title, desc: L.site.description, main });
}
